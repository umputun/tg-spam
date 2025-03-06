// file: app/storage/storage_test.go
package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-pkgz/testutils/containers"
	"github.com/stretchr/testify/suite"

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

type StorageTestSuite struct {
	suite.Suite
	dbs         map[string]*engine.SQL
	pgContainer *containers.PostgresTestContainer
	sqliteFile  string
}

func TestStorageSuite(t *testing.T) {
	suite.Run(t, new(StorageTestSuite))
}

func (s *StorageTestSuite) SetupSuite() {
	s.dbs = make(map[string]*engine.SQL)

	// setup SQLite with file-based db
	s.sqliteFile = filepath.Join(os.TempDir(), "test.db")
	s.T().Logf("sqlite file: %s", s.sqliteFile)
	sqliteDB, err := engine.NewSqlite(s.sqliteFile, "gr1")
	s.Require().NoError(err)
	s.dbs["sqlite"] = sqliteDB

	// setup Postgres
	if !testing.Short() {
		s.T().Log("start postgres container")
		ctx := context.Background()

		// use testutils PostgresTestContainer
		s.pgContainer = containers.NewPostgresTestContainerWithDB(ctx, s.T(), "test")
		s.T().Log("postgres container started")

		// create database connection using the container
		connStr := s.pgContainer.ConnectionString()
		pgDB, err := engine.NewPostgres(ctx, connStr, "gr1")
		s.Require().NoError(err)
		s.dbs["postgres"] = pgDB
	}
}

func (s *StorageTestSuite) TearDownSuite() {
	for _, db := range s.dbs {
		db.Close()
	}
	if s.pgContainer != nil {
		s.T().Log("terminating container")
		s.pgContainer.Close(context.Background())
	}
	if s.sqliteFile != "" {
		s.T().Logf("removing sqlite file: %s", s.sqliteFile)
		err := os.Remove(s.sqliteFile)
		s.Require().NoError(err)
	}
}

func (s *StorageTestSuite) getTestDB() []struct {
	DB   *engine.SQL
	Type engine.Type
} {
	var res []struct {
		DB   *engine.SQL
		Type engine.Type
	}
	for name, db := range s.dbs {
		res = append(res, struct {
			DB   *engine.SQL
			Type engine.Type
		}{
			DB:   db,
			Type: engine.Type(name),
		})
	}
	return res
}

// common tests

func TestPrepareStoreURL(t *testing.T) {
	testCases := []struct {
		name     string
		connURL  string
		wantType engine.Type
		wantConn string
		wantErr  bool
	}{
		{
			name:     "sqlite file",
			connURL:  "test.db",
			wantType: engine.Sqlite,
			wantConn: "test.db",
			wantErr:  false,
		},
		{
			name:     "sqlite file with extension",
			connURL:  "test.sqlite",
			wantType: engine.Sqlite,
			wantConn: "test.sqlite",
			wantErr:  false,
		},
		{
			name:     "sqlite url",
			connURL:  "sqlite:/path/to/db",
			wantType: engine.Sqlite,
			wantConn: "file:/path/to/db",
			wantErr:  false,
		},
		{
			name:     "sqlite3 url",
			connURL:  "sqlite3:/path/to/db",
			wantType: engine.Sqlite,
			wantConn: "file:/path/to/db",
			wantErr:  false,
		},
		{
			name:     "memory",
			connURL:  "memory",
			wantType: engine.Sqlite,
			wantConn: ":memory:",
			wantErr:  false,
		},
		{
			name:     "memory url",
			connURL:  "memory://",
			wantType: engine.Sqlite,
			wantConn: ":memory:",
			wantErr:  false,
		},
		{
			name:     "mem url",
			connURL:  "mem://",
			wantType: engine.Sqlite,
			wantConn: ":memory:",
			wantErr:  false,
		},
		{
			name:     "in-memory",
			connURL:  "file::memory:",
			wantType: engine.Sqlite,
			wantConn: ":memory:",
			wantErr:  false,
		},
		{
			name:     "postgres url",
			connURL:  "postgres://user:pass@host:5432/db",
			wantType: engine.Postgres,
			wantConn: "postgres://user:pass@host:5432/db",
			wantErr:  false,
		},
		{
			name:     "mysql without parseTime",
			connURL:  "user:pass@tcp(host:3306)/db",
			wantType: engine.Mysql,
			wantConn: "user:pass@tcp(host:3306)/db?parseTime=true",
			wantErr:  false,
		},
		{
			name:     "mysql with other params",
			connURL:  "user:pass@tcp(host:3306)/db?charset=utf8",
			wantType: engine.Mysql,
			wantConn: "user:pass@tcp(host:3306)/db?charset=utf8&parseTime=true",
			wantErr:  false,
		},
		{
			name:     "mysql with parseTime",
			connURL:  "user:pass@tcp(host:3306)/db?parseTime=true",
			wantType: engine.Mysql,
			wantConn: "user:pass@tcp(host:3306)/db?parseTime=true",
			wantErr:  false,
		},
		{
			name:     "unsupported url",
			connURL:  "invalid-url",
			wantType: engine.Unknown,
			wantConn: "",
			wantErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dbType, conn, err := prepareStoreURL(tc.connURL)
			if tc.wantErr {
				if err == nil {
					t.Errorf("prepareStoreURL(%q) returned no error, expected an error", tc.connURL)
				}
				return
			}
			if err != nil {
				t.Errorf("prepareStoreURL(%q) returned unexpected error: %v", tc.connURL, err)
			}
			if dbType != tc.wantType {
				t.Errorf("prepareStoreURL(%q) returned dbType = %v, want %v", tc.connURL, dbType, tc.wantType)
			}
			if conn != tc.wantConn {
				t.Errorf("prepareStoreURL(%q) returned conn = %q, want %q", tc.connURL, conn, tc.wantConn)
			}
		})
	}
}

func TestNew(t *testing.T) {
	ctx := context.Background()

	// test creating SQLite in-memory database
	t.Run("sqlite in-memory", func(t *testing.T) {
		db, err := New(ctx, "memory", "test-group")
		if err != nil {
			t.Fatalf("New(ctx, 'memory', 'test-group') failed: %v", err)
		}
		defer db.Close()

		if db.Type() != engine.Sqlite {
			t.Errorf("New(ctx, 'memory', 'test-group') returned type = %v, want %v", db.Type(), engine.Sqlite)
		}
	})

	// test creating SQLite file database
	t.Run("sqlite file", func(t *testing.T) {
		tmpFile := filepath.Join(os.TempDir(), "test-new-func.db")
		defer os.Remove(tmpFile)

		db, err := New(ctx, tmpFile, "test-group")
		if err != nil {
			t.Fatalf("New(ctx, %q, 'test-group') failed: %v", tmpFile, err)
		}
		defer db.Close()

		if db.Type() != engine.Sqlite {
			t.Errorf("New(ctx, %q, 'test-group') returned type = %v, want %v", tmpFile, db.Type(), engine.Sqlite)
		}
	})

	// test with invalid URL
	t.Run("invalid url", func(t *testing.T) {
		_, err := New(ctx, "invalid-url", "test-group")
		if err == nil {
			t.Error("New(ctx, 'invalid-url', 'test-group') returned no error, expected an error")
		}
	})

	// skip Postgres tests if short mode (similar to what the suite does)
	if !testing.Short() {
		// we could add a PostgreSQL test here, but it would require setting up a container
		// which is already well tested in the StorageTestSuite
		t.Skip("skipping postgres test in standalone mode - already tested in suite")
	}
}

func (s *StorageTestSuite) TestIsolation() {
	ctx := context.Background()
	s.Run("sqlite isolation via separate files", func() {
		// create two separate SQLite databases with different GIDs
		tmpFile1 := filepath.Join(os.TempDir(), "test_db1.sqlite")
		tmpFile2 := filepath.Join(os.TempDir(), "test_db2.sqlite")
		defer os.Remove(tmpFile1)
		defer os.Remove(tmpFile2)

		db1, err := engine.NewSqlite(tmpFile1, "gr1")
		s.Require().NoError(err)
		defer db1.Close()

		db2, err := engine.NewSqlite(tmpFile2, "gr2")
		s.Require().NoError(err)
		defer db2.Close()

		// test Samples isolation
		s.Run("samples isolation", func() {
			samples1, err := NewSamples(ctx, db1)
			s.Require().NoError(err)
			samples2, err := NewSamples(ctx, db2)
			s.Require().NoError(err)

			err = samples1.Add(ctx, SampleTypeHam, SampleOriginUser, "test1 message")
			s.Require().NoError(err)
			err = samples2.Add(ctx, SampleTypeHam, SampleOriginUser, "test2 message")
			s.Require().NoError(err)

			msgs1, err := samples1.Read(ctx, SampleTypeHam, SampleOriginUser)
			s.Require().NoError(err)
			s.Equal(1, len(msgs1))
			s.Equal("test1 message", msgs1[0])

			msgs2, err := samples2.Read(ctx, SampleTypeHam, SampleOriginUser)
			s.Require().NoError(err)
			s.Equal(1, len(msgs2))
			s.Equal("test2 message", msgs2[0])
		})

		// test Dictionary isolation
		s.Run("dictionary isolation", func() {
			dict1, err := NewDictionary(ctx, db1)
			s.Require().NoError(err)
			dict2, err := NewDictionary(ctx, db2)
			s.Require().NoError(err)

			err = dict1.Add(ctx, DictionaryTypeStopPhrase, "test1 phrase")
			s.Require().NoError(err)
			err = dict2.Add(ctx, DictionaryTypeStopPhrase, "test2 phrase")
			s.Require().NoError(err)

			phrases1, err := dict1.Read(ctx, DictionaryTypeStopPhrase)
			s.Require().NoError(err)
			s.Equal(1, len(phrases1))
			s.Equal("test1 phrase", phrases1[0])

			phrases2, err := dict2.Read(ctx, DictionaryTypeStopPhrase)
			s.Require().NoError(err)
			s.Equal(1, len(phrases2))
			s.Equal("test2 phrase", phrases2[0])
		})

		// test DetectedSpam isolation
		s.Run("detected spam isolation", func() {
			spam1, err := NewDetectedSpam(ctx, db1)
			s.Require().NoError(err)
			spam2, err := NewDetectedSpam(ctx, db2)
			s.Require().NoError(err)

			entry1 := DetectedSpamInfo{
				GID:       "gr1",
				Text:      "spam1",
				UserID:    1,
				UserName:  "user1",
				Timestamp: time.Now(),
			}
			entry2 := DetectedSpamInfo{
				GID:       "gr2",
				Text:      "spam2",
				UserID:    2,
				UserName:  "user2",
				Timestamp: time.Now(),
			}
			checks := []spamcheck.Response{{Name: "test", Spam: true}}

			err = spam1.Write(ctx, entry1, checks)
			s.Require().NoError(err)
			err = spam2.Write(ctx, entry2, checks)
			s.Require().NoError(err)

			entries1, err := spam1.Read(ctx)
			s.Require().NoError(err)
			s.Equal(1, len(entries1))
			s.Equal("spam1", entries1[0].Text)

			entries2, err := spam2.Read(ctx)
			s.Require().NoError(err)
			s.Equal(1, len(entries2))
			s.Equal("spam2", entries2[0].Text)
		})

		// test ApprovedUsers isolation
		s.Run("approved users isolation", func() {
			au1, err := NewApprovedUsers(ctx, db1)
			s.Require().NoError(err)
			au2, err := NewApprovedUsers(ctx, db2)
			s.Require().NoError(err)

			user1 := approved.UserInfo{UserID: "1", UserName: "user1"}
			user2 := approved.UserInfo{UserID: "2", UserName: "user2"}

			err = au1.Write(ctx, user1)
			s.Require().NoError(err)
			err = au2.Write(ctx, user2)
			s.Require().NoError(err)

			users1, err := au1.Read(ctx)
			s.Require().NoError(err)
			s.Equal(1, len(users1))
			s.Equal("user1", users1[0].UserName)

			users2, err := au2.Read(ctx)
			s.Require().NoError(err)
			s.Equal(1, len(users2))
			s.Equal("user2", users2[0].UserName)
		})

		// test Locator isolation
		s.Run("locator isolation", func() {
			loc1, err := NewLocator(ctx, time.Hour, 10, db1)
			s.Require().NoError(err)
			loc2, err := NewLocator(ctx, time.Hour, 10, db2)
			s.Require().NoError(err)

			err = loc1.AddMessage(ctx, "msg1", 1, 1, "user1", 1)
			s.Require().NoError(err)
			err = loc2.AddMessage(ctx, "msg2", 2, 2, "user2", 2)
			s.Require().NoError(err)

			meta1, found := loc1.Message(ctx, "msg1")
			s.Require().True(found)
			s.Equal(int64(1), meta1.UserID)

			meta2, found := loc2.Message(ctx, "msg2")
			s.Require().True(found)
			s.Equal(int64(2), meta2.UserID)

			// verify cross-visibility
			_, found = loc1.Message(ctx, "msg2")
			s.Require().False(found)
			_, found = loc2.Message(ctx, "msg1")
			s.Require().False(found)
		})
	})

	s.Run("postgres isolation via gid column", func() {
		// skip if postgres is not available
		if testing.Short() {
			s.T().Skip("skipping postgres test in short mode")
		}

		// get existing postgres container connection
		var pgDB *engine.SQL
		for _, dbt := range s.getTestDB() {
			if dbt.DB.Type() == engine.Postgres {
				pgDB = dbt.DB
				break
			}
		}
		if pgDB == nil {
			s.T().Skip("postgres is not available")
		}

		// get connection string from the container
		pgConnStr := s.pgContainer.ConnectionString()

		// create two separate connections with different GIDs
		db1, err := engine.NewPostgres(ctx, pgConnStr, "gr1")
		s.Require().NoError(err)
		defer db1.Close()

		db2, err := engine.NewPostgres(ctx, pgConnStr, "gr2")
		s.Require().NoError(err)
		defer db2.Close()

		// same test sub-cases as for SQLite
		s.Run("samples isolation", func() {
			samples1, err := NewSamples(ctx, db1)
			s.Require().NoError(err)
			samples2, err := NewSamples(ctx, db2)
			s.Require().NoError(err)

			err = samples1.Add(ctx, SampleTypeHam, SampleOriginUser, "test1 message")
			s.Require().NoError(err)
			err = samples2.Add(ctx, SampleTypeHam, SampleOriginUser, "test2 message")
			s.Require().NoError(err)

			msgs1, err := samples1.Read(ctx, SampleTypeHam, SampleOriginUser)
			s.Require().NoError(err)
			s.Equal(1, len(msgs1))
			s.Equal("test1 message", msgs1[0])

			msgs2, err := samples2.Read(ctx, SampleTypeHam, SampleOriginUser)
			s.Require().NoError(err)
			s.Equal(1, len(msgs2))
			s.Equal("test2 message", msgs2[0])
		})

		s.Run("dictionary isolation", func() {
			dict1, err := NewDictionary(ctx, db1)
			s.Require().NoError(err)
			dict2, err := NewDictionary(ctx, db2)
			s.Require().NoError(err)

			err = dict1.Add(ctx, DictionaryTypeStopPhrase, "test1 phrase")
			s.Require().NoError(err)
			err = dict2.Add(ctx, DictionaryTypeStopPhrase, "test2 phrase")
			s.Require().NoError(err)

			phrases1, err := dict1.Read(ctx, DictionaryTypeStopPhrase)
			s.Require().NoError(err)
			s.Equal(1, len(phrases1))
			s.Equal("test1 phrase", phrases1[0])

			phrases2, err := dict2.Read(ctx, DictionaryTypeStopPhrase)
			s.Require().NoError(err)
			s.Equal(1, len(phrases2))
			s.Equal("test2 phrase", phrases2[0])
		})

		s.Run("detected spam isolation", func() {
			spam1, err := NewDetectedSpam(ctx, db1)
			s.Require().NoError(err)
			spam2, err := NewDetectedSpam(ctx, db2)
			s.Require().NoError(err)

			entry1 := DetectedSpamInfo{
				GID:       "gr1",
				Text:      "spam1",
				UserID:    1,
				UserName:  "user1",
				Timestamp: time.Now(),
			}
			entry2 := DetectedSpamInfo{
				GID:       "gr2",
				Text:      "spam2",
				UserID:    2,
				UserName:  "user2",
				Timestamp: time.Now(),
			}
			checks := []spamcheck.Response{{Name: "test", Spam: true}}

			err = spam1.Write(ctx, entry1, checks)
			s.Require().NoError(err)
			err = spam2.Write(ctx, entry2, checks)
			s.Require().NoError(err)

			entries1, err := spam1.Read(ctx)
			s.Require().NoError(err)
			s.Equal(1, len(entries1))
			s.Equal("spam1", entries1[0].Text)

			entries2, err := spam2.Read(ctx)
			s.Require().NoError(err)
			s.Equal(1, len(entries2))
			s.Equal("spam2", entries2[0].Text)
		})

		s.Run("approved users isolation", func() {
			au1, err := NewApprovedUsers(ctx, db1)
			s.Require().NoError(err)
			au2, err := NewApprovedUsers(ctx, db2)
			s.Require().NoError(err)

			user1 := approved.UserInfo{UserID: "1", UserName: "user1"}
			user2 := approved.UserInfo{UserID: "2", UserName: "user2"}

			err = au1.Write(ctx, user1)
			s.Require().NoError(err)
			err = au2.Write(ctx, user2)
			s.Require().NoError(err)

			users1, err := au1.Read(ctx)
			s.Require().NoError(err)
			s.Equal(1, len(users1))
			s.Equal("user1", users1[0].UserName)

			users2, err := au2.Read(ctx)
			s.Require().NoError(err)
			s.Equal(1, len(users2))
			s.Equal("user2", users2[0].UserName)
		})

		s.Run("locator isolation", func() {
			loc1, err := NewLocator(ctx, time.Hour, 10, db1)
			s.Require().NoError(err)
			loc2, err := NewLocator(ctx, time.Hour, 10, db2)
			s.Require().NoError(err)

			err = loc1.AddMessage(ctx, "msg1", 1, 1, "user1", 1)
			s.Require().NoError(err)
			err = loc2.AddMessage(ctx, "msg2", 2, 2, "user2", 2)
			s.Require().NoError(err)

			meta1, found := loc1.Message(ctx, "msg1")
			s.Require().True(found)
			s.Equal(int64(1), meta1.UserID)

			meta2, found := loc2.Message(ctx, "msg2")
			s.Require().True(found)
			s.Equal(int64(2), meta2.UserID)

			// verify cross-visibility
			_, found = loc1.Message(ctx, "msg2")
			s.Require().False(found)
			_, found = loc2.Message(ctx, "msg1")
			s.Require().False(found)
		})
	})
}

// file: app/storage/storage_test.go
package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

type StorageTestSuite struct {
	suite.Suite
	dbs         map[string]*engine.SQL
	pgContainer testcontainers.Container
	sqliteFile  string
}

func TestStorageSuite(t *testing.T) {
	suite.Run(t, new(StorageTestSuite))
}

func (s *StorageTestSuite) SetupSuite() {
	s.dbs = make(map[string]*engine.SQL)

	// Setup SQLite with file-based db
	s.sqliteFile = filepath.Join(os.TempDir(), "test.db")
	s.T().Logf("sqlite file: %s", s.sqliteFile)
	sqliteDB, err := engine.NewSqlite(s.sqliteFile, "gr1")
	s.Require().NoError(err)
	s.dbs["sqlite"] = sqliteDB

	// Setup Postgres
	if !testing.Short() {
		s.T().Log("start postgres container")
		ctx := context.Background()

		req := testcontainers.ContainerRequest{
			Image:        "postgres:15",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_PASSWORD": "secret",
				"POSTGRES_DB":       "test",
			},
			WaitingFor: wait.ForAll(
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
				wait.ForListeningPort("5432/tcp"),
			).WithDeadline(1 * time.Minute),
		}

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		s.Require().NoError(err)
		s.pgContainer = container

		time.Sleep(time.Second)

		host, err := container.Host(ctx)
		s.Require().NoError(err)
		port, err := container.MappedPort(ctx, "5432")
		s.Require().NoError(err)

		connStr := fmt.Sprintf("postgres://postgres:secret@%s:%d/test?sslmode=disable", host, port.Int())
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
		s.Require().NoError(s.pgContainer.Terminate(context.Background()))
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

func (s *StorageTestSuite) TestIsolation() {
	ctx := context.Background()
	s.Run("sqlite isolation via separate files", func() {
		// Create two separate SQLite databases with different GIDs
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

		// Test Samples isolation
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

		// Test Dictionary isolation
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

		// Test DetectedSpam isolation
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

		// Test ApprovedUsers isolation
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

		// Test Locator isolation
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
		// Skip if postgres is not available
		if testing.Short() {
			s.T().Skip("skipping postgres test in short mode")
		}

		// Get existing postgres container connection
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

		// Get container connection details
		host, err := s.pgContainer.Host(ctx)
		s.Require().NoError(err)
		port, err := s.pgContainer.MappedPort(ctx, "5432")
		s.Require().NoError(err)

		// Create connection string using container details
		pgConnStr := fmt.Sprintf("postgres://postgres:secret@%s:%d/test?sslmode=disable", host, port.Int())

		// Create two separate connections with different GIDs
		db1, err := engine.NewPostgres(ctx, pgConnStr, "gr1")
		s.Require().NoError(err)
		defer db1.Close()

		db2, err := engine.NewPostgres(ctx, pgConnStr, "gr2")
		s.Require().NoError(err)
		defer db2.Close()

		// Same test sub-cases as for SQLite
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

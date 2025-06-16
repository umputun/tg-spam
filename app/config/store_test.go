package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-pkgz/testutils/containers"
	"github.com/stretchr/testify/suite"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// SettingsTestSuite is a test suite for the settings package
type SettingsTestSuite struct {
	suite.Suite
	dbs         map[string]*engine.SQL
	pgContainer *containers.PostgresTestContainer
	sqliteFile  string
	ctx         context.Context
}

func TestSettingsSuite(t *testing.T) {
	suite.Run(t, new(SettingsTestSuite))
}

func (s *SettingsTestSuite) SetupSuite() {
	s.ctx = context.Background()
	s.dbs = make(map[string]*engine.SQL)

	// setup SQLite with unique file-based db to avoid lock contention in parallel tests
	s.sqliteFile = filepath.Join(os.TempDir(), fmt.Sprintf("test-%d-%d.db", os.Getpid(), time.Now().UnixNano()))
	s.T().Logf("sqlite file: %s", s.sqliteFile)
	sqliteDB, err := engine.NewSqlite(s.sqliteFile, "test-group")
	s.Require().NoError(err)
	s.dbs["sqlite"] = sqliteDB

	// setup PostgreSQL if not in short test mode
	if !testing.Short() {
		s.T().Log("starting postgres container")
		ctx := context.Background()

		// use testutils PostgresTestContainer
		s.pgContainer = containers.NewPostgresTestContainerWithDB(ctx, s.T(), "test")
		s.T().Log("postgres container started")

		// create database connection using the container
		connStr := s.pgContainer.ConnectionString()
		pgDB, err := engine.NewPostgres(ctx, connStr, "test-group")
		s.Require().NoError(err)
		s.dbs["postgres"] = pgDB
	}
}

func (s *SettingsTestSuite) TearDownSuite() {
	for _, db := range s.dbs {
		db.Close()
	}

	// remove SQLite test file
	if s.sqliteFile != "" {
		_ = os.Remove(s.sqliteFile)
	}

	// stop PostgreSQL container if it was started
	if s.pgContainer != nil {
		// since the container is managed by testutils, we don't need to explicitly stop it
		s.T().Log("postgres container will be stopped by testutils")
	}
}

// getTestDB returns all the test databases to use for testing
func (s *SettingsTestSuite) getTestDB() []*engine.SQL {
	var result []*engine.SQL
	for _, db := range s.dbs {
		result = append(result, db)
	}
	return result
}

// SetupTest runs before each test to ensure a clean environment
func (s *SettingsTestSuite) SetupTest() {
	// drop config table before each test to ensure clean state
	for _, db := range s.dbs {
		// first, ensure we're not in an existing transaction by trying to
		// rollback any pending transaction. This is necessary because in slow
		// environments (like CI), connections from the pool might still have
		// active implicit transactions from previous operations.
		if db.Type() == engine.Sqlite {
			// try to rollback any existing transaction - ignore errors
			// this ensures we start with a clean slate
			_, _ = db.Exec("ROLLBACK")
		}

		// now we can safely drop the table
		_, err := db.Exec("DROP TABLE IF EXISTS config")
		if err != nil && !strings.Contains(err.Error(), "no such table") {
			s.Require().NoError(err)
		}
	}
}

func (s *SettingsTestSuite) TestStore_SaveLoad() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// create store
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// create test settings
			settings := New()
			settings.InstanceID = "test-store"
			settings.SimilarityThreshold = 0.8
			settings.Telegram.Group = "test-group"
			settings.Telegram.Timeout = 45 * time.Second
			settings.Server.Enabled = true
			settings.Message.Spam = "spam message from store test"

			// add credentials that should be persisted
			settings.Telegram.Token = "stored-telegram-token"
			settings.OpenAI.Token = "stored-openai-token"
			settings.Server.AuthHash = "stored-auth-hash"
			settings.Transient.StorageTimeout = 5 * time.Minute
			settings.Transient.ConfigDB = true
			settings.Transient.Dbg = true

			// save settings
			err = store.Save(s.ctx, settings)
			s.Require().NoError(err)

			// check if settings exist
			exists, err := store.Exists(s.ctx)
			s.Require().NoError(err)
			s.True(exists)

			// check last updated time
			lastUpdate, err := store.LastUpdated(s.ctx)
			s.Require().NoError(err)
			s.False(lastUpdate.IsZero(), "LastUpdated time should not be zero")

			// load settings
			loaded, err := store.Load(s.ctx)
			s.Require().NoError(err)

			// check loaded settings
			s.Equal("test-store", loaded.InstanceID)
			s.Equal(0.8, loaded.SimilarityThreshold)
			s.Equal("test-group", loaded.Telegram.Group)
			s.Equal(45*time.Second, loaded.Telegram.Timeout)
			s.True(loaded.Server.Enabled)
			s.Equal("spam message from store test", loaded.Message.Spam)

			// credentials should be persisted
			s.Equal("stored-telegram-token", loaded.Telegram.Token)
			s.Equal("stored-openai-token", loaded.OpenAI.Token)
			s.Equal("stored-auth-hash", loaded.Server.AuthHash)
			s.Zero(loaded.Transient.StorageTimeout)
			s.False(loaded.Transient.ConfigDB)
			s.False(loaded.Transient.Dbg)

			// delete settings
			err = store.Delete(s.ctx)
			s.Require().NoError(err)

			// check settings no longer exist
			exists, err = store.Exists(s.ctx)
			s.Require().NoError(err)
			s.False(exists)

			// try loading deleted settings
			_, err = store.Load(s.ctx)
			s.Error(err)
			s.Contains(err.Error(), "no settings found in database")
		})
	}
}

// TestStore_SaveNilSettings tests Save with nil settings
func (s *SettingsTestSuite) TestStore_SaveNilSettings() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// try saving nil settings
			err = store.Save(s.ctx, nil)
			s.Error(err)
			s.Contains(err.Error(), "nil settings")
		})
	}
}

// TestStore_LoadError tests Load with error condition
func (s *SettingsTestSuite) TestStore_LoadError() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// try loading from empty table
			_, err = store.Load(s.ctx)
			s.Error(err)
			s.Contains(err.Error(), "no settings found in database")

			// insert invalid JSON
			query := db.Adopt("INSERT INTO config (gid, data) VALUES (?, ?)")
			_, err = db.Exec(query, db.GID(), "{invalid-json")
			s.Require().NoError(err)

			// try loading invalid JSON
			_, err = store.Load(s.ctx)
			s.Error(err)
			s.Contains(err.Error(), "failed to unmarshal settings")
		})
	}
}

// TestStore_LastUpdatedError tests LastUpdated with error condition
func (s *SettingsTestSuite) TestStore_LastUpdatedError() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// try getting last updated time from empty table
			_, err = store.LastUpdated(s.ctx)
			s.Error(err)
			s.Contains(err.Error(), "no settings found in database")
		})
	}
}

// TestStore_NewStoreErrors tests error conditions in NewStore
func (s *SettingsTestSuite) TestStore_NewStoreErrors() {
	// test nil db error
	_, err := NewStore(s.ctx, nil)
	s.Error(err)
	s.Contains(err.Error(), "no db provided")
}

func (s *SettingsTestSuite) TestStore_Update() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// create store
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// create and save initial settings
			s1 := New()
			s1.InstanceID = "initial"
			s1.Telegram.Group = "initial-group"

			err = store.Save(s.ctx, s1)
			s.Require().NoError(err)

			// get initial update time
			initialUpdate, err := store.LastUpdated(s.ctx)
			s.Require().NoError(err)

			// wait a bit to ensure timestamps are different
			time.Sleep(10 * time.Millisecond)

			// create and save updated settings
			s2 := New()
			s2.InstanceID = "updated"
			s2.Telegram.Group = "updated-group"

			err = store.Save(s.ctx, s2)
			s.Require().NoError(err)

			// get updated time
			updatedTime, err := store.LastUpdated(s.ctx)
			s.Require().NoError(err)

			// make sure update time changed (for SQLite this should be reliable)
			// for PostgreSQL times might be different due to precision differences
			if db.Type() == engine.Sqlite {
				s.True(updatedTime.After(initialUpdate))
			} else {
				// for PostgreSQL we mainly check it's a valid time
				s.False(updatedTime.IsZero())
			}

			// load settings and verify they were updated
			loaded, err := store.Load(s.ctx)
			s.Require().NoError(err)

			s.Equal("updated", loaded.InstanceID)
			s.Equal("updated-group", loaded.Telegram.Group)
		})
	}
}

func (s *SettingsTestSuite) TestStore_CreateTable() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// create store which should create the table
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// check if the table exists by trying to insert and select data
			err = store.Save(s.ctx, New())
			s.Require().NoError(err)

			// check if we can query the table
			var count int
			query := db.Adopt("SELECT COUNT(*) FROM config")
			err = db.Get(&count, query)
			s.Require().NoError(err)
			s.Equal(1, count)
		})
	}
}

func (s *SettingsTestSuite) TestStore_ComplexSettings() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// create store
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// create complex settings to test all field types
			settings := New()
			settings.InstanceID = "complex-test"
			settings.SimilarityThreshold = 0.75
			settings.MinMsgLen = 100
			settings.MaxEmoji = 5
			settings.Telegram.Group = "@testgroup"
			settings.Telegram.IdleDuration = 1 * time.Minute
			settings.Admin.SuperUsers = []string{"user1", "user2"}
			settings.Admin.TestingIDs = []int64{123, 456}
			settings.Message.Spam = "spam detected!"
			settings.Message.Warn = "warning!"
			settings.LuaPlugins.Enabled = true
			settings.LuaPlugins.EnabledPlugins = []string{"plugin1", "plugin2"}

			// save complex settings
			err = store.Save(s.ctx, settings)
			s.Require().NoError(err)

			// load settings
			loaded, err := store.Load(s.ctx)
			s.Require().NoError(err)

			// verify all fields were correctly saved and loaded
			s.Equal("complex-test", loaded.InstanceID)
			s.Equal(0.75, loaded.SimilarityThreshold)
			s.Equal(100, loaded.MinMsgLen)
			s.Equal(5, loaded.MaxEmoji)
			s.Equal("@testgroup", loaded.Telegram.Group)
			s.Equal(1*time.Minute, loaded.Telegram.IdleDuration)
			s.Equal([]string{"user1", "user2"}, loaded.Admin.SuperUsers)
			s.Equal([]int64{123, 456}, loaded.Admin.TestingIDs)
			s.Equal("spam detected!", loaded.Message.Spam)
			s.Equal("warning!", loaded.Message.Warn)
			s.True(loaded.LuaPlugins.Enabled)
			s.Equal([]string{"plugin1", "plugin2"}, loaded.LuaPlugins.EnabledPlugins)
		})
	}
}

func (s *SettingsTestSuite) TestStore_WithEncryption() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// create crypter
			crypter, err := NewCrypter("test-master-key-20-chars", "test-instance")
			s.Require().NoError(err)

			// create store with encryption
			store, err := NewStore(s.ctx, db, WithCrypter(crypter))
			s.Require().NoError(err)

			// create settings with sensitive info
			settings := New()
			settings.InstanceID = "encrypted-test"
			settings.Telegram.Token = "super-secret-telegram-token"
			settings.OpenAI.Token = "super-secret-openai-token"
			settings.Server.AuthHash = "super-secret-auth-hash"
			settings.Telegram.Group = "non-sensitive-group"

			// save settings with encryption
			err = store.Save(s.ctx, settings)
			s.Require().NoError(err)

			// verify data is encrypted in the database
			var record struct {
				Data string `db:"data"`
			}
			query := db.Adopt("SELECT data FROM config WHERE gid = ?")
			err = db.GetContext(s.ctx, &record, query, db.GID())
			s.Require().NoError(err)

			// check that the JSON contains encrypted values
			s.True(strings.Contains(record.Data, EncryptPrefix),
				"Database should contain encrypted values")

			// test accessing the data with a direct load query
			var rawSettings struct {
				Telegram struct {
					Token string `json:"token"`
				} `json:"telegram"`
				OpenAI struct {
					Token string `json:"token"`
				} `json:"openai"`
			}
			err = json.Unmarshal([]byte(record.Data), &rawSettings)
			s.Require().NoError(err)

			// verify sensitive fields are encrypted in the database
			s.True(IsEncrypted(rawSettings.Telegram.Token),
				"Telegram token should be encrypted in database")
			s.True(IsEncrypted(rawSettings.OpenAI.Token),
				"OpenAI token should be encrypted in database")

			// load settings (should decrypt automatically)
			loaded, err := store.Load(s.ctx)
			s.Require().NoError(err)

			// check that decrypted values match original values
			s.Equal("super-secret-telegram-token", loaded.Telegram.Token,
				"Telegram token should be decrypted when loaded")
			s.Equal("super-secret-openai-token", loaded.OpenAI.Token,
				"OpenAI token should be decrypted when loaded")
			s.Equal("super-secret-auth-hash", loaded.Server.AuthHash,
				"Auth hash should be decrypted when loaded")
			s.Equal("non-sensitive-group", loaded.Telegram.Group,
				"Non-sensitive fields should be unchanged")

			// now access without encryption
			plainStore, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// load settings (should still be encrypted)
			encryptedLoaded, err := plainStore.Load(s.ctx)
			s.Require().NoError(err)

			// check that values remain encrypted
			s.True(IsEncrypted(encryptedLoaded.Telegram.Token),
				"Telegram token should remain encrypted when loaded without crypter")
			s.True(IsEncrypted(encryptedLoaded.OpenAI.Token),
				"OpenAI token should remain encrypted when loaded without crypter")
			s.True(IsEncrypted(encryptedLoaded.Server.AuthHash),
				"Auth hash should remain encrypted when loaded without crypter")
		})
	}
}

func (s *SettingsTestSuite) TestStore_WithCustomSensitiveFields() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// create crypter
			crypter, err := NewCrypter("test-master-key-20-chars", "test-instance")
			s.Require().NoError(err)

			// custom sensitive fields - only encrypt Telegram token and a different field
			customFields := []string{
				"telegram.token",
				"server.auth_user", // not normally encrypted
			}

			// create store with encryption and custom fields
			store, err := NewStore(s.ctx, db,
				WithCrypter(crypter),
				WithSensitiveFields(customFields))
			s.Require().NoError(err)

			// verify custom fields were set
			s.Equal(customFields, store.sensitiveFields)

			// create settings with sensitive info
			settings := New()
			settings.InstanceID = "custom-fields-test"
			settings.Telegram.Token = "super-secret-telegram-token"
			settings.OpenAI.Token = "super-secret-openai-token"    // should NOT be encrypted
			settings.Server.AuthHash = "super-secret-auth-hash"    // should NOT be encrypted
			settings.Server.AuthUser = "custom-sensitive-username" // should be encrypted

			// save settings with custom encryption fields
			err = store.Save(s.ctx, settings)
			s.Require().NoError(err)

			// verify data is stored correctly in database
			var record struct {
				Data string `db:"data"`
			}
			query := db.Adopt("SELECT data FROM config WHERE gid = ?")
			err = db.GetContext(s.ctx, &record, query, db.GID())
			s.Require().NoError(err)

			// parse the stored data
			var rawSettings struct {
				Telegram struct {
					Token string `json:"token"`
				} `json:"telegram"`
				OpenAI struct {
					Token string `json:"token"`
				} `json:"openai"`
				Server struct {
					AuthHash string `json:"auth_hash"`
					AuthUser string `json:"auth_user"`
				} `json:"server"`
			}
			err = json.Unmarshal([]byte(record.Data), &rawSettings)
			s.Require().NoError(err)

			// verify only the custom fields are encrypted
			s.True(IsEncrypted(rawSettings.Telegram.Token),
				"Telegram token should be encrypted")
			s.True(IsEncrypted(rawSettings.Server.AuthUser),
				"Server auth user should be encrypted (custom field)")

			// verify fields NOT in the custom list remain unencrypted
			s.False(IsEncrypted(rawSettings.OpenAI.Token),
				"OpenAI token should NOT be encrypted")
			s.False(IsEncrypted(rawSettings.Server.AuthHash),
				"Server auth hash should NOT be encrypted")

			// load the settings back
			loaded, err := store.Load(s.ctx)
			s.Require().NoError(err)

			// verify proper decryption
			s.Equal("super-secret-telegram-token", loaded.Telegram.Token)
			s.Equal("custom-sensitive-username", loaded.Server.AuthUser)
			s.Equal("super-secret-openai-token", loaded.OpenAI.Token)
			s.Equal("super-secret-auth-hash", loaded.Server.AuthHash)
		})
	}
}

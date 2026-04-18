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
	result := make([]*engine.SQL, 0, len(s.dbs))
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
			s.InEpsilon(0.8, loaded.SimilarityThreshold, 0.0001)
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
			s.Require().Error(err)
			s.Contains(err.Error(), "no settings found in database")
		})
	}
}

func (s *SettingsTestSuite) TestStore_SaveNilSettings() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// try saving nil settings
			err = store.Save(s.ctx, nil)
			s.Require().Error(err)
			s.Contains(err.Error(), "nil settings")
		})
	}
}

func (s *SettingsTestSuite) TestStore_LoadError() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// try loading from empty table
			_, err = store.Load(s.ctx)
			s.Require().Error(err)
			s.Contains(err.Error(), "no settings found in database")

			// insert invalid JSON
			query := db.Adopt("INSERT INTO config (gid, data) VALUES (?, ?)")
			_, err = db.Exec(query, db.GID(), "{invalid-json")
			s.Require().NoError(err)

			// try loading invalid JSON
			_, err = store.Load(s.ctx)
			s.Require().Error(err)
			s.Contains(err.Error(), "failed to unmarshal settings")
		})
	}
}

func (s *SettingsTestSuite) TestStore_LastUpdatedError() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			store, err := NewStore(s.ctx, db)
			s.Require().NoError(err)

			// try getting last updated time from empty table
			_, err = store.LastUpdated(s.ctx)
			s.Require().Error(err)
			s.Contains(err.Error(), "no settings found in database")
		})
	}
}

func (s *SettingsTestSuite) TestStore_NewStoreErrors() {
	// test nil db error
	_, err := NewStore(s.ctx, nil)
	s.Require().Error(err)
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
			s.InEpsilon(0.75, loaded.SimilarityThreshold, 0.0001)
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
			s.Contains(record.Data, EncryptPrefix, "Database should contain encrypted values")

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

func (s *SettingsTestSuite) TestStore_NewMasterFeatureGroups_RoundTrip() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			crypter, err := NewCrypter("test-master-key-20-chars", "test-instance")
			s.Require().NoError(err)

			store, err := NewStore(s.ctx, db, WithCrypter(crypter))
			s.Require().NoError(err)

			// build a settings object with non-default values for every new group
			settings := New()
			settings.InstanceID = "new-groups-roundtrip"

			// delete group
			settings.Delete.JoinMessages = true
			settings.Delete.LeaveMessages = true

			// meta extensions added on master
			settings.Meta.ContactOnly = true
			settings.Meta.Giveaway = true

			// gemini group (Token gets encrypted at rest)
			settings.Gemini.Token = "super-secret-gemini-token"
			settings.Gemini.Veto = true
			settings.Gemini.Prompt = "gemini prompt"
			settings.Gemini.CustomPrompts = []string{"prompt-a", "prompt-b"}
			settings.Gemini.Model = "gemini-2.5-pro"
			settings.Gemini.MaxTokensResponse = 1024
			settings.Gemini.MaxSymbolsRequest = 8000
			settings.Gemini.RetryCount = 3
			settings.Gemini.HistorySize = 7
			settings.Gemini.CheckShortMessages = true

			// LLM orchestration
			settings.LLM.Consensus = "all"
			settings.LLM.RequestTimeout = 45 * time.Second

			// duplicates
			settings.Duplicates.Threshold = 4
			settings.Duplicates.Window = 2 * time.Minute

			// report
			settings.Report.Enabled = true
			settings.Report.Threshold = 5
			settings.Report.AutoBanThreshold = 10
			settings.Report.RateLimit = 3
			settings.Report.RatePeriod = 90 * time.Second

			// top-level cleanup additions
			settings.AggressiveCleanup = true
			settings.AggressiveCleanupLimit = 50

			// save through encrypted store
			err = store.Save(s.ctx, settings)
			s.Require().NoError(err)

			// verify Gemini.Token is encrypted in the raw DB row
			var record struct {
				Data string `db:"data"`
			}
			query := db.Adopt("SELECT data FROM config WHERE gid = ?")
			err = db.GetContext(s.ctx, &record, query, db.GID())
			s.Require().NoError(err)

			var rawSettings struct {
				Gemini struct {
					Token string `json:"token"`
				} `json:"gemini"`
			}
			err = json.Unmarshal([]byte(record.Data), &rawSettings)
			s.Require().NoError(err)
			s.True(strings.HasPrefix(rawSettings.Gemini.Token, EncryptPrefix),
				"Gemini.Token should be stored with ENC: prefix, got %q", rawSettings.Gemini.Token)
			s.True(IsEncrypted(rawSettings.Gemini.Token),
				"Gemini.Token should be detected as encrypted in DB")

			// reload through encrypted store
			loaded, err := store.Load(s.ctx)
			s.Require().NoError(err)

			// every new field must round-trip exactly
			s.Equal(settings.Delete, loaded.Delete, "Delete group must round-trip")
			s.Equal(settings.Meta.ContactOnly, loaded.Meta.ContactOnly)
			s.Equal(settings.Meta.Giveaway, loaded.Meta.Giveaway)
			s.Equal(settings.Gemini, loaded.Gemini, "Gemini group must round-trip including decrypted Token")
			s.Equal(settings.LLM, loaded.LLM, "LLM group must round-trip")
			s.Equal(settings.Duplicates, loaded.Duplicates, "Duplicates group must round-trip")
			s.Equal(settings.Report, loaded.Report, "Report group must round-trip")
			s.Equal(settings.AggressiveCleanup, loaded.AggressiveCleanup)
			s.Equal(settings.AggressiveCleanupLimit, loaded.AggressiveCleanupLimit)

			// explicit Gemini.Token plaintext check (decryption path)
			s.Equal("super-secret-gemini-token", loaded.Gemini.Token,
				"Gemini.Token should be decrypted to original plaintext on load")
		})
	}
}

func (s *SettingsTestSuite) TestStore_WithCustomSensitiveFields() {
	for _, db := range s.getTestDB() {
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// create crypter
			crypter, err := NewCrypter("test-master-key-20-chars", "test-instance")
			s.Require().NoError(err)

			// custom sensitive fields - encrypt only Telegram token, opting out of the default list
			customFields := []string{
				"telegram.token",
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
			settings.Telegram.Token = "super-secret-telegram-token" // should be encrypted
			settings.OpenAI.Token = "super-secret-openai-token"     // should NOT be encrypted (not in custom list)
			settings.Server.AuthHash = "super-secret-auth-hash"     // should NOT be encrypted (not in custom list)

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
				} `json:"server"`
			}
			err = json.Unmarshal([]byte(record.Data), &rawSettings)
			s.Require().NoError(err)

			// verify only the custom field is encrypted
			s.True(IsEncrypted(rawSettings.Telegram.Token),
				"Telegram token should be encrypted")

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
			s.Equal("super-secret-openai-token", loaded.OpenAI.Token)
			s.Equal("super-secret-auth-hash", loaded.Server.AuthHash)
		})
	}
}

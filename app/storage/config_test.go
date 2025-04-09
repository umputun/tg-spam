package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/umputun/tg-spam/app/storage/engine"
)

func (s *StorageTestSuite) TestConfig_NewConfig() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			s.Run("create new table", func() {
				_, err := NewConfig[string](ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE config")

				// check if the table exists - use db-agnostic way
				var exists int
				err = db.Get(&exists, `SELECT COUNT(*) FROM config`) // simpler check
				s.Require().NoError(err)
				s.Equal(0, exists) // empty but exists
			})

			s.Run("table already exists", func() {
				if db.Type() != engine.Sqlite {
					s.T().Skip("skipping for non-sqlite database")
				}
				defer db.Exec("DROP TABLE config")

				// create table with updated columns
				_, err := db.Exec(`CREATE TABLE config (
					id INTEGER PRIMARY KEY,
					gid TEXT NOT NULL,
					data TEXT NOT NULL,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					UNIQUE(gid)
				)`)
				s.Require().NoError(err)

				_, err = NewConfig[string](ctx, db)
				s.Require().NoError(err)

				// verify that the existing structure has not changed
				var columnCount int
				err = db.Get(&columnCount, "SELECT COUNT(*) FROM pragma_table_info('config')")
				s.Require().NoError(err)
				s.Equal(5, columnCount) // id, gid, data, created_at, updated_at
			})

			s.Run("nil db connection", func() {
				_, err := NewConfig[string](ctx, nil)
				s.Require().Error(err)
				s.Contains(err.Error(), "no db provided")
			})

			s.Run("context cancelled", func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := NewConfig[string](ctx, db)
				s.Require().Error(err)
			})
		})
	}
}

func (s *StorageTestSuite) TestConfig_SetAndGet() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			type testConfig struct {
				Key    string      `json:"key"`
				Nested interface{} `json:"nested,omitempty"`
				Array  []int       `json:"array,omitempty"`
			}
			tests := []struct {
				name    string
				obj     testConfig
				verify  func(s *StorageTestSuite, db *engine.SQL, obj testConfig)
				wantErr bool
			}{
				{
					name: "set and get simple data",
					obj: testConfig{
						Key: "value",
					},
					verify: func(s *StorageTestSuite, db *engine.SQL, obj testConfig) {
						var saved ConfigRecord
						query := db.Adopt("SELECT data FROM config WHERE gid = ?")
						err := db.Get(&saved, query, db.GID())
						s.Require().NoError(err)
						var savedObj testConfig
						err = json.Unmarshal([]byte(saved.Data), &savedObj)
						s.Require().NoError(err)
						s.Equal(obj, savedObj)
					},
				},
				{
					name: "set and get complex data",
					obj: testConfig{
						Key:    "value",
						Nested: map[string]interface{}{"nested": "value"},
						Array:  []int{1, 2, 3},
					},
					verify: func(s *StorageTestSuite, db *engine.SQL, obj testConfig) {
						var saved ConfigRecord
						query := db.Adopt("SELECT data FROM config WHERE gid = ?")
						err := db.Get(&saved, query, db.GID())
						s.Require().NoError(err)
						var savedObj testConfig
						err = json.Unmarshal([]byte(saved.Data), &savedObj)
						s.Require().NoError(err)
						s.Equal(obj, savedObj)
					},
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					config, err := NewConfig[testConfig](ctx, db)
					s.Require().NoError(err)
					defer db.Exec("DROP TABLE config")

					err = config.SetObject(ctx, &tt.obj)
					if tt.wantErr {
						s.Require().Error(err)
						return
					}
					s.Require().NoError(err)

					if tt.verify != nil {
						tt.verify(s, db, tt.obj)
					}

					// verify Get returns the same data
					var result testConfig
					err = config.GetObject(ctx, &result)
					s.Require().NoError(err)
					s.Equal(tt.obj, result)
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestConfig_SetAndGetObject() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			type testStruct struct {
				Key   string   `json:"key"`
				Value int      `json:"value"`
				Tags  []string `json:"tags"`
			}

			tests := []struct {
				name    string
				obj     testStruct
				wantErr bool
			}{
				{
					name: "set and get object with all fields",
					obj: testStruct{
						Key:   "test",
						Value: 123,
						Tags:  []string{"tag1", "tag2"},
					},
				},
				{
					name: "set and get object with empty fields",
					obj: testStruct{
						Key: "test",
					},
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					config, err := NewConfig[testStruct](ctx, db)
					s.Require().NoError(err)
					defer db.Exec("DROP TABLE config")

					err = config.SetObject(ctx, &tt.obj)
					if tt.wantErr {
						s.Require().Error(err)
						return
					}
					s.Require().NoError(err)

					var result testStruct
					err = config.GetObject(ctx, &result)
					s.Require().NoError(err)
					s.Equal(tt.obj, result)
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestConfig_Delete() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			type testConfig struct {
				Key string `json:"key"`
			}
			config, err := NewConfig[testConfig](ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE config")

			// set some data
			err = config.SetObject(ctx, &testConfig{Key: "value"})
			s.Require().NoError(err)

			// delete it
			err = config.Delete(ctx)
			s.Require().NoError(err)

			// verify it's gone
			var cfg testConfig
			err = config.GetObject(ctx, &cfg)
			s.Require().Error(err)
			s.Contains(err.Error(), "no configuration found")
		})
	}
}

func (s *StorageTestSuite) TestConfig_ConcurrentAccess() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			type testConfig struct {
				Key string `json:"key"`
			}
			config, err := NewConfig[testConfig](ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE config")

			// test concurrent writes
			var wg sync.WaitGroup
			results := make(map[int]time.Time, 10)
			var mu sync.Mutex

			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					// add retry logic for SQLite
					var setErr error
					for retries := 0; retries < 3; retries++ {
						setErr = config.SetObject(ctx, &testConfig{Key: fmt.Sprintf("value%d", i)})
						if setErr == nil || !strings.Contains(setErr.Error(), "database is locked") {
							break
						}
						time.Sleep(time.Millisecond * 10) // wait before retry
					}
					s.Assert().NoError(setErr)

					if setErr == nil {
						// get the updated_at time to verify write order
						var updatedAt time.Time
						err := db.QueryRow("SELECT updated_at FROM config WHERE gid = ?", db.GID()).Scan(&updatedAt)
						if err == nil {
							mu.Lock()
							results[i] = updatedAt
							mu.Unlock()
						}
					}
				}(i)
			}

			wg.Wait()

			// verify writes were serialized by checking timestamps
			var times []time.Time
			for _, t := range results {
				times = append(times, t)
			}
			sort.Slice(times, func(i, j int) bool {
				return times[i].Before(times[j])
			})
			for i := 1; i < len(times); i++ {
				s.True(times[i].After(times[i-1]), "writes should be serialized")
			}

			// test concurrent reads don't block each other
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var result testConfig
					err := config.GetObject(ctx, &result)
					s.Assert().NoError(err)
					s.Contains(result.Key, "value")
				}()
			}
			wg.Wait()
		})
	}
}

func (s *StorageTestSuite) TestConfig_ContextCancellation() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			type testConfig struct {
				Key string `json:"key"`
			}
			config, err := NewConfig[testConfig](ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE config")

			// test Set with cancelled context
			cancelCtx, cancel := context.WithCancel(ctx)
			cancel()
			err = config.SetObject(cancelCtx, &testConfig{Key: "value"})
			s.Require().Error(err)
			s.True(errors.Is(err, context.Canceled), "should be context.Canceled error")

			// test Get with cancelled context and timeout
			err = config.SetObject(ctx, &testConfig{Key: "value"})
			s.Require().NoError(err)

			timeoutCtx, cancel := context.WithTimeout(ctx, time.Microsecond)
			defer cancel()
			time.Sleep(time.Millisecond)
			var result testConfig
			err = config.GetObject(timeoutCtx, &result)
			s.Require().Error(err)
			s.True(errors.Is(err, context.DeadlineExceeded), "should be timeout error")

			// test Delete with cancelled context
			err = config.Delete(cancelCtx)
			s.Require().Error(err)
			s.True(errors.Is(err, context.Canceled), "should be context.Canceled error")
		})
	}
}

func (s *StorageTestSuite) TestConfig_ErrorCases() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			type testStruct struct {
				Field string `json:"field"`
			}
			config, err := NewConfig[testStruct](ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE config")

			s.Run("get non-existent config", func() {
				_, err := config.Get(ctx)
				s.Require().Error(err)
				s.Contains(err.Error(), "no configuration found")
			})

			s.Run("get object with invalid json", func() {
				// this is a valid JSON but not valid for the struct (field should be string)
				err := config.Set(ctx, `{"field": 123}`) // number instead of string

				// set should fail because of the type validation we added
				s.Require().Error(err)
				s.Contains(err.Error(), "invalid type")
			})

			s.Run("set with invalid json", func() {
				err := config.Set(ctx, `{"field": "test"`) // missing closing brace
				s.Require().Error(err)
				s.Contains(err.Error(), "invalid JSON")
			})

			s.Run("set with empty string", func() {
				err := config.Set(ctx, "")
				s.Require().Error(err)
				s.Contains(err.Error(), "empty data not allowed")
			})

			s.Run("set with null value", func() {
				err := config.Set(ctx, "null")
				s.Require().Error(err)
				s.Contains(err.Error(), "null value not allowed")
			})

			s.Run("set with invalid type", func() {
				err := config.Set(ctx, `{"field": [1,2,3]}`) // array instead of string
				s.Require().Error(err)
				s.Contains(err.Error(), "invalid type")
			})
		})
	}
}

func (s *StorageTestSuite) TestConfig_MultiTenancy() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			type testConfig struct {
				Value string `json:"value"`
			}

			// create two separate DB connections with different GIDs
			var db1, db2 *engine.SQL
			if db.Type() == engine.Sqlite {
				// for SQLite, create two separate file connections for isolation test
				tmpFile1 := filepath.Join(os.TempDir(), "test_db1.sqlite")
				tmpFile2 := filepath.Join(os.TempDir(), "test_db2.sqlite")
				defer os.Remove(tmpFile1)
				defer os.Remove(tmpFile2)

				var err error
				db1, err = engine.NewSqlite(tmpFile1, "group1")
				s.Require().NoError(err)
				defer db1.Close()

				db2, err = engine.NewSqlite(tmpFile2, "group2")
				s.Require().NoError(err)
				defer db2.Close()
			} else {
				// for Postgres or other DBs, use the same connection with different GIDs
				if testing.Short() {
					s.T().Skip("skipping multi-tenancy postgres test in short mode")
				}
				// get PostgreSQL container connection string
				pgConnStr := s.pgContainer.ConnectionString()

				var err error
				db1, err = engine.NewPostgres(ctx, pgConnStr, "group1")
				s.Require().NoError(err)
				defer db1.Close()

				db2, err = engine.NewPostgres(ctx, pgConnStr, "group2")
				s.Require().NoError(err)
				defer db2.Close()
			}

			config1, err := NewConfig[testConfig](ctx, db1)
			s.Require().NoError(err)
			config2, err := NewConfig[testConfig](ctx, db2)
			s.Require().NoError(err)

			// clean up after test
			defer db1.Exec("DROP TABLE config")
			defer db2.Exec("DROP TABLE config")

			// set different configs for each group
			err = config1.SetObject(ctx, &testConfig{Value: "group1_value"})
			s.Require().NoError(err)
			err = config2.SetObject(ctx, &testConfig{Value: "group2_value"})
			s.Require().NoError(err)

			// verify each group has its own config
			var result1 testConfig
			err = config1.GetObject(ctx, &result1)
			s.Require().NoError(err)
			s.Equal("group1_value", result1.Value)

			var result2 testConfig
			err = config2.GetObject(ctx, &result2)
			s.Require().NoError(err)
			s.Equal("group2_value", result2.Value)

			// deleting one group's config should not affect others
			err = config1.Delete(ctx)
			s.Require().NoError(err)

			// verify group1 config is gone
			err = config1.GetObject(ctx, &result1)
			s.Require().Error(err)
			s.Contains(err.Error(), "no configuration found")

			// verify group2 config still exists
			err = config2.GetObject(ctx, &result2)
			s.Require().NoError(err)
			s.Equal("group2_value", result2.Value)
		})
	}
}

func (s *StorageTestSuite) TestConfig_ComplexStructs() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// test with a complex struct similar to the options struct in main.go
			type nestedConfig struct {
				Token   string `json:"token"`
				Enabled bool   `json:"enabled"`
			}

			type complexConfig struct {
				InstanceID string            `json:"instance_id"`
				Server     nestedConfig      `json:"server"`
				Timeout    time.Duration     `json:"timeout"`
				Debug      bool              `json:"debug"`
				Numbers    []int             `json:"numbers"`
				Map        map[string]string `json:"map"`
			}

			config, err := NewConfig[complexConfig](ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE config")

			// create complex config
			testData := complexConfig{
				InstanceID: "test-instance",
				Server: nestedConfig{
					Token:   "secure-token",
					Enabled: true,
				},
				Timeout: 5 * time.Second,
				Debug:   true,
				Numbers: []int{1, 2, 3},
				Map:     map[string]string{"key1": "value1", "key2": "value2"},
			}

			// save and retrieve
			err = config.SetObject(ctx, &testData)
			s.Require().NoError(err)

			var result complexConfig
			err = config.GetObject(ctx, &result)
			s.Require().NoError(err)

			// verify all fields match
			s.Equal(testData.InstanceID, result.InstanceID)
			s.Equal(testData.Server.Token, result.Server.Token)
			s.Equal(testData.Server.Enabled, result.Server.Enabled)
			s.Equal(testData.Timeout, result.Timeout)
			s.Equal(testData.Debug, result.Debug)
			s.Equal(testData.Numbers, result.Numbers)
			s.Equal(testData.Map, result.Map)

			// verify the JSON serialization
			data, err := config.Get(ctx)
			s.Require().NoError(err)
			s.Contains(data, `"instance_id":"test-instance"`)
			s.Contains(data, `"server":{"token":"secure-token","enabled":true}`)
			s.Contains(data, `"timeout":5000000000`) // 5 seconds in nanoseconds
			s.Contains(data, `"debug":true`)
		})
	}
}

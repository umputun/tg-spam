package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

func (s *StorageTestSuite) TestNewDetectedSpam() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			s.Run("with empty database", func() {
				if db.Type() != engine.Sqlite {
					s.T().Skip("skipping for non-sqlite database")
				}
				ds, err := NewDetectedSpam(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE detected_spam")

				s.Require().NotNil(ds)

				var cols []struct {
					CID       int     `db:"cid"`
					Name      string  `db:"name"`
					Type      string  `db:"type"`
					NotNull   bool    `db:"notnull"`
					DfltValue *string `db:"dflt_value"`
					PK        bool    `db:"pk"`
				}
				err = db.Select(&cols, "PRAGMA table_info(detected_spam)")
				s.Require().NoError(err)

				colMap := make(map[string]string)
				for _, col := range cols {
					colMap[col.Name] = col.Type
				}

				s.Equal("TEXT", colMap["gid"])
				s.Equal("TEXT", colMap["text"])
				s.Equal("INTEGER", colMap["user_id"])
				s.Equal("TEXT", colMap["user_name"])
			})

			s.Run("with existing old schema", func() {
				if db.Type() != engine.Sqlite {
					s.T().Skip("skipping for non-sqlite database")
				}
				defer db.Exec("DROP TABLE detected_spam")

				_, err := db.Exec(`
					CREATE TABLE detected_spam (
						id INTEGER PRIMARY KEY AUTOINCREMENT,
						text TEXT,
						user_id INTEGER,
						user_name TEXT,
						timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
						added BOOLEAN DEFAULT 0,
						checks TEXT
					)
				`)
				s.Require().NoError(err)

				_, err = db.Exec(`
					INSERT INTO detected_spam (text, user_id, user_name, checks)
					VALUES (?, ?, ?, ?)`,
					"test spam", 123, "test_user", `[{"Name":"test","Spam":true}]`)
				s.Require().NoError(err)

				ds, err := NewDetectedSpam(ctx, db)
				s.Require().NoError(err)
				s.Require().NotNil(ds)

				entries, err := ds.Read(ctx)
				s.Require().NoError(err)
				s.Require().Len(entries, 1)

				s.Equal(db.GID(), entries[0].GID)
				s.Equal("test spam", entries[0].Text)
				s.Equal(int64(123), entries[0].UserID)
				s.Equal("test_user", entries[0].UserName)
			})

			s.Run("with nil db", func() {
				defer db.Exec("DROP TABLE detected_spam")
				_, err := NewDetectedSpam(ctx, nil)
				s.Require().Error(err)
				s.Contains(err.Error(), "db connection is nil")
			})

			s.Run("with cancelled context", func() {
				defer db.Exec("DROP TABLE detected_spam")
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := NewDetectedSpam(ctx, db)
				s.Require().Error(err)
			})
		})
	}
}

func (s *StorageTestSuite) TestDetectedSpam_Write() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			ds, err := NewDetectedSpam(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE detected_spam")

			spamEntry := DetectedSpamInfo{
				Text:      "spam message",
				UserID:    1,
				UserName:  "Spammer",
				Timestamp: time.Now(),
				GID:       "group123",
			}

			checks := []spamcheck.Response{
				{
					Name:    "Check1",
					Spam:    true,
					Details: "Details 1",
				},
			}

			err = ds.Write(ctx, spamEntry, checks)
			s.Require().NoError(err)

			var count int
			err = db.Get(&count, "SELECT COUNT(*) FROM detected_spam")
			s.Require().NoError(err)
			s.Equal(1, count)
		})
	}
}

func (s *StorageTestSuite) TestDetectedSpam_SetAddedToSamplesFlag() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB

		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			ds, err := NewDetectedSpam(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE detected_spam")

			spamEntry := DetectedSpamInfo{
				Text:      "spam message",
				UserID:    1,
				UserName:  "Spammer",
				Timestamp: time.Now(),
				GID:       "group123",
			}

			checks := []spamcheck.Response{
				{
					Name:    "Check1",
					Spam:    true,
					Details: "Details 1",
				},
			}

			err = ds.Write(ctx, spamEntry, checks)
			s.Require().NoError(err)
			var added bool
			err = db.Get(&added, db.Adopt("SELECT added FROM detected_spam WHERE text = ?"), spamEntry.Text)
			s.Require().NoError(err)
			s.False(added)

			err = ds.SetAddedToSamplesFlag(ctx, 1)
			s.Require().NoError(err)

			err = db.Get(&added, db.Adopt("SELECT added FROM detected_spam WHERE text = ?"), spamEntry.Text)
			s.Require().NoError(err)
			s.True(added)
		})
	}
}

func (s *StorageTestSuite) TestDetectedSpam_Read() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			ds, err := NewDetectedSpam(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE detected_spam")

			// add a sample first
			entry := DetectedSpamInfo{
				GID:       "gr1", // use the store's GID here
				Text:      "test spam",
				UserID:    456,
				UserName:  "spammer",
				Timestamp: time.Now().UTC().Truncate(time.Second), // ensure consistent time comparison
			}
			checks := []spamcheck.Response{{Name: "test", Spam: true, Details: "test details"}}

			err = ds.Write(ctx, entry, checks)
			s.Require().NoError(err)

			entries, err := ds.Read(ctx)
			s.Require().NoError(err)
			s.Require().Len(entries, 1)

			s.Equal(entry.GID, entries[0].GID)
			s.Equal(entry.Text, entries[0].Text)
			s.Equal(entry.UserID, entries[0].UserID)
			s.Equal(entry.UserName, entries[0].UserName)
			s.Equal(checks, entries[0].Checks)
		})
	}
}

func (s *StorageTestSuite) TestDetectedSpam_Read_LimitExceeded() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			ds, err := NewDetectedSpam(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE detected_spam")

			// add maxDetectedSpamEntries + 10 entries
			for i := 0; i < maxDetectedSpamEntries+10; i++ {
				spamEntry := DetectedSpamInfo{
					GID:       "gr1", // use the correct GID
					Text:      "spam message",
					UserID:    int64(i + 500),
					UserName:  "Spammer",
					Timestamp: time.Now(),
				}

				checks := []spamcheck.Response{{Name: "test", Spam: true}}
				err = ds.Write(ctx, spamEntry, checks)
				s.Require().NoError(err)
			}

			entries, err := ds.Read(ctx)
			s.Require().NoError(err)
			s.Len(entries, maxDetectedSpamEntries, "expected to retrieve only the maximum number of entries")
		})
	}
}

func (s *StorageTestSuite) TestDetectedSpam() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			tests := []struct {
				name    string
				entry   DetectedSpamInfo
				checks  []spamcheck.Response
				wantErr bool
			}{
				{
					name: "basic spam entry",
					entry: DetectedSpamInfo{
						GID:       "gr1", // use the store's GID here
						Text:      "spam message",
						UserID:    1,
						UserName:  "Spammer",
						Timestamp: time.Now(),
					},
					checks: []spamcheck.Response{{
						Name:    "Check1",
						Spam:    true,
						Details: "Details 1",
					}},
					wantErr: false,
				},
				{
					name: "empty gid",
					entry: DetectedSpamInfo{
						Text:      "spam message",
						UserID:    1,
						UserName:  "Spammer",
						Timestamp: time.Now(),
					},
					checks: []spamcheck.Response{{
						Name:    "Check1",
						Spam:    true,
						Details: "Details 1",
					}},
					wantErr: true,
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					ds, err := NewDetectedSpam(ctx, db)
					s.Require().NoError(err)
					defer db.Exec("DROP TABLE detected_spam")

					err = ds.Write(ctx, tt.entry, tt.checks)
					if tt.wantErr {
						s.Require().Error(err)
						return
					}
					s.Require().NoError(err)

					entries, err := ds.Read(ctx)
					s.Require().NoError(err)
					s.Require().Len(entries, 1)

					s.Equal(tt.entry.GID, entries[0].GID)
					s.Equal(tt.entry.Text, entries[0].Text)
					s.Equal(tt.entry.UserID, entries[0].UserID)
					s.Equal(tt.entry.UserName, entries[0].UserName)
					s.Equal(tt.checks, entries[0].Checks)
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestDetectedSpam_FindByUserID() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			ds, err := NewDetectedSpam(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE detected_spam")

			s.Run("user not found", func() {
				entry, err := ds.FindByUserID(ctx, 123)
				s.Require().NoError(err)
				s.Nil(entry)
			})

			s.Run("basic case", func() {
				ts := time.Now().UTC().Truncate(time.Second) // explicit UTC time
				expected := DetectedSpamInfo{
					GID:       db.GID(),
					Text:      "test spam",
					UserID:    456,
					UserName:  "spammer",
					Timestamp: ts,
				}
				checks := []spamcheck.Response{{
					Name:    "test",
					Spam:    true,
					Details: "test details",
				}}

				err := ds.Write(ctx, expected, checks)
				s.Require().NoError(err)

				entry, err := ds.FindByUserID(ctx, 456)
				s.Require().NoError(err)
				s.Require().NotNil(entry)
				s.Equal(expected.GID, entry.GID)
				s.Equal(expected.Text, entry.Text)
				s.Equal(expected.UserID, entry.UserID)
				s.Equal(expected.UserName, entry.UserName)
				s.Equal(checks, entry.Checks)
				s.Equal(ts.Unix(), entry.Timestamp.UTC().Unix()) // ensure UTC comparison
			})

			s.Run("multiple entries", func() {
				// write two entries for same user
				for i := 0; i < 2; i++ {
					entry := DetectedSpamInfo{
						GID:       db.GID(),
						Text:      fmt.Sprintf("spam %d", i),
						UserID:    789,
						UserName:  "spammer",
						Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
					}
					checks := []spamcheck.Response{{Name: fmt.Sprintf("check%d", i), Spam: true}}
					err := ds.Write(ctx, entry, checks)
					s.Require().NoError(err)
				}

				// should get the latest one
				entry, err := ds.FindByUserID(ctx, 789)
				s.Require().NoError(err)
				s.Require().NotNil(entry)
				s.Equal("spam 1", entry.Text)
				s.Equal("check1", entry.Checks[0].Name)
			})

			s.Run("invalid checks json", func() {
				// insert invalid json directly to db
				query := db.Adopt("INSERT INTO detected_spam (gid, text, user_id, user_name, timestamp, checks) VALUES (?, ?, ?, ?, ?, ?)")
				_, err := db.Exec(query, db.GID(), "test", 999, "test", time.Now(), "{invalid}")
				s.Require().NoError(err)

				entry, err := ds.FindByUserID(ctx, 999)
				s.Require().Error(err)
				s.Contains(err.Error(), "failed to unmarshal checks")
				s.Nil(entry)
			})
		})
	}
}

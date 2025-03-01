package storage

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/umputun/tg-spam/app/storage/engine"
)

func (s *StorageTestSuite) TestNewSamples() {
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			defer db.Exec("DROP TABLE samples") // cleanup after tests

			tests := []struct {
				name    string
				db      *engine.SQL
				wantErr bool
			}{
				{
					name:    "valid db connection",
					db:      db,
					wantErr: false,
				},
				{
					name:    "nil db connection",
					db:      nil,
					wantErr: true,
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					samples, err := NewSamples(context.Background(), tt.db)
					if tt.wantErr {
						s.Error(err)
						s.Nil(samples)
					} else {
						s.NoError(err)
						s.NotNil(samples)
					}
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestSamples_AddSample() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			tests := []struct {
				name    string
				sType   SampleType
				origin  SampleOrigin
				message string
				wantErr bool
			}{
				{
					name:    "valid ham preset",
					sType:   SampleTypeHam,
					origin:  SampleOriginPreset,
					message: "test ham message",
					wantErr: false,
				},
				{
					name:    "valid spam user",
					sType:   SampleTypeSpam,
					origin:  SampleOriginUser,
					message: "test spam message",
					wantErr: false,
				},
				{
					name:    "invalid sample type",
					sType:   "invalid",
					origin:  SampleOriginPreset,
					message: "test message",
					wantErr: true,
				},
				{
					name:    "invalid origin",
					sType:   SampleTypeHam,
					origin:  "invalid",
					message: "test message",
					wantErr: true,
				},
				{
					name:    "empty message",
					sType:   SampleTypeHam,
					origin:  SampleOriginPreset,
					message: "",
					wantErr: true,
				},
				{
					name:    "origin any not allowed",
					sType:   SampleTypeHam,
					origin:  SampleOriginAny,
					message: "test message",
					wantErr: true,
				},
				{
					name:    "duplicate message same type and origin",
					sType:   SampleTypeHam,
					origin:  SampleOriginPreset,
					message: "test ham message", // same as first test case
					wantErr: false,              // should succeed and replace
				},
				{
					name:    "duplicate message different type",
					sType:   SampleTypeSpam,
					origin:  SampleOriginPreset,
					message: "test ham message", // same message, different type
					wantErr: false,              // should succeed and replace
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					err := samples.Add(ctx, tt.sType, tt.origin, tt.message)
					if tt.wantErr {
						s.Error(err)
					} else {
						s.NoError(err)
						// verify message exists and has correct type and origin
						var count int
						err = db.Get(&count, db.Adopt("SELECT COUNT(*) FROM samples WHERE message = ? AND type = ? AND origin = ?"),
							tt.message, tt.sType, tt.origin)
						s.Require().NoError(err)
						s.Equal(1, count)
					}
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestSamples_DeleteSample() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			// add a sample first
			err = samples.Add(ctx, SampleTypeHam, SampleOriginPreset, "test message")
			s.Require().NoError(err)

			// get the ID of the inserted sample
			var id int64
			err = db.Get(&id, db.Adopt("SELECT id FROM samples WHERE message = ?"), "test message")
			s.Require().NoError(err)

			tests := []struct {
				name    string
				id      int64
				wantErr bool
			}{
				{
					name:    "existing sample",
					id:      id,
					wantErr: false,
				},
				{
					name:    "non-existent sample",
					id:      99999,
					wantErr: true,
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					err := samples.Delete(ctx, tt.id)
					if tt.wantErr {
						s.Error(err)
					} else {
						s.NoError(err)
					}
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestSamples_DeleteMessage() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			// add test samples
			testData := []struct {
				sType   SampleType
				origin  SampleOrigin
				message string
			}{
				{SampleTypeHam, SampleOriginPreset, "message to delete"},
				{SampleTypeSpam, SampleOriginUser, "message to keep"},
				{SampleTypeHam, SampleOriginUser, "another message"},
			}

			for _, td := range testData {
				err := samples.Add(ctx, td.sType, td.origin, td.message)
				s.Require().NoError(err)
			}

			tests := []struct {
				name    string
				message string
				wantErr bool
			}{
				{
					name:    "existing message",
					message: "message to delete",
					wantErr: false,
				},
				{
					name:    "non-existent message",
					message: "no such message",
					wantErr: true,
				},
				{
					name:    "empty message",
					message: "",
					wantErr: true,
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					err := samples.DeleteMessage(ctx, tt.message)
					if tt.wantErr {
						s.Error(err)
					} else {
						s.NoError(err)

						// verify message no longer exists
						var count int
						err = db.Get(&count, db.Adopt("SELECT COUNT(*) FROM samples WHERE message = ?"), tt.message)
						s.Require().NoError(err)
						s.Equal(0, count)

						// verify other messages still exist
						var totalCount int
						err = db.Get(&totalCount, db.Adopt("SELECT COUNT(*) FROM samples"))
						s.Require().NoError(err)
						s.Equal(len(testData)-1, totalCount)
					}
				})
			}

			s.Run("concurrent delete", func() {
				// add a message that will be deleted concurrently
				msg := "concurrent delete message"
				for i := range 10 {
					err := samples.Add(ctx, SampleTypeHam, SampleOriginPreset, msg+fmt.Sprintf("-%d", i))
					s.Require().NoError(err)
				}

				const numWorkers = 10
				var wg sync.WaitGroup
				errCh := make(chan error, numWorkers)

				// start multiple goroutines trying to delete the same message
				for i := 0; i < numWorkers; i++ {
					wg.Add(1)
					go func() {
						i := i
						defer wg.Done()
						msg := msg + fmt.Sprintf("-%d", i)
						if err := samples.DeleteMessage(ctx, msg); err != nil && !strings.Contains(err.Error(), "not found") {
							errCh <- err
						}
					}()
				}

				wg.Wait()
				close(errCh)

				// check for unexpected errors
				for err := range errCh {
					s.T().Errorf("concurrent delete failed: %v", err)
				}

				// verify message was deleted
				var count int
				err = db.Get(&count, db.Adopt("SELECT COUNT(*) FROM samples WHERE message = ?"), msg)
				s.Require().NoError(err)
				s.Equal(0, count)
			})
		})
	}
}

func (s *StorageTestSuite) TestSamples_ReadSamples() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			// add test samples
			testData := []struct {
				sType   SampleType
				origin  SampleOrigin
				message string
			}{
				{SampleTypeHam, SampleOriginPreset, "ham preset 1"},
				{SampleTypeHam, SampleOriginUser, "ham user 1"},
				{SampleTypeSpam, SampleOriginPreset, "spam preset 1"},
				{SampleTypeSpam, SampleOriginUser, "spam user 1"},
			}

			for _, td := range testData {
				err := samples.Add(ctx, td.sType, td.origin, td.message)
				s.Require().NoError(err)
			}

			tests := []struct {
				name          string
				sType         SampleType
				origin        SampleOrigin
				expectedCount int
				wantErr       bool
			}{
				{
					name:          "all ham samples",
					sType:         SampleTypeHam,
					origin:        SampleOriginAny,
					expectedCount: 2,
					wantErr:       false,
				},
				{
					name:          "preset spam samples",
					sType:         SampleTypeSpam,
					origin:        SampleOriginPreset,
					expectedCount: 1,
					wantErr:       false,
				},
				{
					name:          "invalid type",
					sType:         "invalid",
					origin:        SampleOriginPreset,
					expectedCount: 0,
					wantErr:       true,
				},
				{
					name:          "invalid origin",
					sType:         SampleTypeHam,
					origin:        "invalid",
					expectedCount: 0,
					wantErr:       true,
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					samples, err := samples.Read(ctx, tt.sType, tt.origin)
					if tt.wantErr {
						s.Error(err)
						s.Nil(samples)
					} else {
						s.NoError(err)
						s.Len(samples, tt.expectedCount)
					}
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestSamples_GetStats() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			// add test samples
			testData := []struct {
				sType   SampleType
				origin  SampleOrigin
				message string
			}{
				{SampleTypeHam, SampleOriginPreset, "ham preset 1"},
				{SampleTypeHam, SampleOriginPreset, "ham preset 2"},
				{SampleTypeHam, SampleOriginUser, "ham user 1"},
				{SampleTypeSpam, SampleOriginPreset, "spam preset 1"},
				{SampleTypeSpam, SampleOriginUser, "spam user 1"},
				{SampleTypeSpam, SampleOriginUser, "spam user 2"},
			}

			for _, td := range testData {
				err := samples.Add(ctx, td.sType, td.origin, td.message)
				s.Require().NoError(err)
			}

			stats, err := samples.Stats(ctx)
			s.Require().NoError(err)
			s.Require().NotNil(stats)

			s.Equal(3, stats.TotalSpam)
			s.Equal(3, stats.TotalHam)
			s.Equal(1, stats.PresetSpam)
			s.Equal(2, stats.PresetHam)
			s.Equal(2, stats.UserSpam)
			s.Equal(1, stats.UserHam)
		})
	}
}

func (s *StorageTestSuite) TestSampleType_Validate() {
	tests := []struct {
		name    string
		sType   SampleType
		wantErr bool
	}{
		{"valid ham", SampleTypeHam, false},
		{"valid spam", SampleTypeSpam, false},
		{"invalid type", "invalid", true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := tt.sType.Validate()
			if tt.wantErr {
				s.Assert().Error(err)
			} else {
				s.Assert().NoError(err)
			}
		})
	}
}

func (s *StorageTestSuite) TestSampleOrigin_Validate() {
	tests := []struct {
		name    string
		origin  SampleOrigin
		wantErr bool
	}{
		{"valid preset", SampleOriginPreset, false},
		{"valid user", SampleOriginUser, false},
		{"valid any", SampleOriginAny, false},
		{"invalid origin", "invalid", true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := tt.origin.Validate()
			if tt.wantErr {
				s.Assert().Error(err)
			} else {
				s.Assert().NoError(err)
			}
		})
	}
}

func (s *StorageTestSuite) TestSamples_Concurrent() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// initialize samples with schema
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			s.Require().NotNil(samples)
			defer db.Exec("DROP TABLE samples")

			// verify table exists and is accessible
			err = samples.Add(ctx, SampleTypeHam, SampleOriginPreset, "test message")
			s.Require().NoError(err, "Failed to insert initial test record")

			const numWorkers = 10
			const numOps = 50

			var wg sync.WaitGroup
			errCh := make(chan error, numWorkers*2)

			// start readers
			for i := 0; i < numWorkers; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for j := 0; j < numOps; j++ {
						if _, err := samples.Read(ctx, SampleTypeHam, SampleOriginAny); err != nil {
							select {
							case errCh <- fmt.Errorf("reader %d failed: %w", workerID, err):
							default:
							}
							return
						}
					}
				}(i)
			}

			// start writers
			for i := 0; i < numWorkers; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for j := 0; j < numOps; j++ {
						msg := fmt.Sprintf("test message %d-%d", workerID, j)
						sType := SampleTypeHam
						if j%2 == 0 {
							sType = SampleTypeSpam
						}
						if err := samples.Add(ctx, sType, SampleOriginUser, msg); err != nil {
							select {
							case errCh <- fmt.Errorf("writer %d failed: %w", workerID, err):
							default:
							}
							return
						}
					}
				}(i)
			}

			// wait for all goroutines to finish
			wg.Wait()
			close(errCh)

			// check for any errors
			for err := range errCh {
				s.T().Errorf("concurrent operation failed: %v", err)
			}

			// verify the final state
			stats, err := samples.Stats(ctx)
			s.Require().NoError(err)
			s.Require().NotNil(stats)

			expectedTotal := numWorkers*numOps + 1 // +1 for the initial test message
			actualTotal := stats.TotalHam + stats.TotalSpam
			s.Equal(expectedTotal, actualTotal, "expected %d total samples, got %d", expectedTotal, actualTotal)
		})
	}
}

func (s *StorageTestSuite) TestSamples_Iterator() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			// insert test data
			testData := []struct {
				sType   SampleType
				origin  SampleOrigin
				message string
			}{
				{SampleTypeHam, SampleOriginPreset, "ham preset 1"},
				{SampleTypeHam, SampleOriginUser, "ham user 1"},
				{SampleTypeSpam, SampleOriginPreset, "spam preset 1"},
				{SampleTypeSpam, SampleOriginUser, "spam user 1"},
			}

			for _, td := range testData {
				err := samples.Add(ctx, td.sType, td.origin, td.message)
				s.Require().NoError(err)
			}

			// test cases
			tests := []struct {
				name         string
				sType        SampleType
				origin       SampleOrigin
				expectedMsgs []string
				expectErr    bool
			}{
				{
					name:         "Ham Preset Samples",
					sType:        SampleTypeHam,
					origin:       SampleOriginPreset,
					expectedMsgs: []string{"ham preset 1"},
					expectErr:    false,
				},
				{
					name:         "Spam User Samples",
					sType:        SampleTypeSpam,
					origin:       SampleOriginUser,
					expectedMsgs: []string{"spam user 1"},
					expectErr:    false,
				},
				{
					name:         "All Ham Samples",
					sType:        SampleTypeHam,
					origin:       SampleOriginAny,
					expectedMsgs: []string{"ham preset 1", "ham user 1"},
					expectErr:    false,
				},
				{
					name:         "Invalid Sample Type",
					sType:        "invalid",
					origin:       SampleOriginPreset,
					expectedMsgs: nil,
					expectErr:    true,
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					iter, err := samples.Iterator(ctx, tt.sType, tt.origin)
					if tt.expectErr {
						s.Error(err)
						return
					}
					s.Require().NoError(err)

					var messages []string
					for msg := range iter {
						messages = append(messages, msg)
					}

					s.ElementsMatch(tt.expectedMsgs, messages)
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestSamples_IteratorOrder() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			// insert test data
			testData := []struct {
				sType   SampleType
				origin  SampleOrigin
				message string
			}{
				{SampleTypeHam, SampleOriginPreset, "ham preset 1"},
				{SampleTypeHam, SampleOriginPreset, "ham preset 2"},
				{SampleTypeHam, SampleOriginPreset, "ham preset 3"},
			}

			for _, td := range testData {
				err := samples.Add(ctx, td.sType, td.origin, td.message)
				s.Require().NoError(err)
				time.Sleep(time.Second) // ensure each message has a unique timestamp
			}

			iter, err := samples.Iterator(ctx, SampleTypeHam, SampleOriginPreset)
			s.Require().NoError(err)
			var messages []string
			for msg := range iter {
				messages = append(messages, msg)
			}
			s.Require().Len(messages, 3)
			s.Equal("ham preset 3", messages[0])
			s.Equal("ham preset 2", messages[1])
			s.Equal("ham preset 1", messages[2])
		})
	}
}

func (s *StorageTestSuite) TestSamples_Import() {
	ctx := context.Background()

	countSamples := func(db *engine.SQL, t SampleType, o SampleOrigin) int {
		var count int
		err := db.Get(&count, db.Adopt("SELECT COUNT(*) FROM samples WHERE type = ? AND origin = ?"), t, o)
		if err != nil {
			return -1
		}
		return count
	}

	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			s.Run("basic import with cleanup", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				input := strings.NewReader("sample1\nsample2\nsample3")
				stats, err := samples.Import(ctx, SampleTypeHam, SampleOriginPreset, input, true)
				s.Require().NoError(err)
				s.Require().NotNil(stats)

				s.Equal(3, countSamples(db, SampleTypeHam, SampleOriginPreset))
				s.Equal(3, stats.PresetHam)
			})

			s.Run("import without cleanup should append", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				// first import
				input1 := strings.NewReader("existing1\nexisting2")
				_, err = samples.Import(ctx, SampleTypeSpam, SampleOriginPreset, input1, true)
				s.Require().NoError(err)
				s.Equal(2, countSamples(db, SampleTypeSpam, SampleOriginPreset))

				// second import without cleanup should append
				input2 := strings.NewReader("new1\nnew2")
				stats, err := samples.Import(ctx, SampleTypeSpam, SampleOriginPreset, input2, false)
				s.Require().NoError(err)
				s.Require().NotNil(stats)

				s.Equal(4, countSamples(db, SampleTypeSpam, SampleOriginPreset))
				s.Equal(4, stats.PresetSpam)

				// verify content includes all samples
				res, err := samples.Read(ctx, SampleTypeSpam, SampleOriginPreset)
				s.Require().NoError(err)
				s.ElementsMatch([]string{"existing1", "existing2", "new1", "new2"}, res)
			})

			s.Run("import with cleanup should replace", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				// first import
				input1 := strings.NewReader("old1\nold2\nold3")
				_, err = samples.Import(ctx, SampleTypeSpam, SampleOriginUser, input1, true)
				s.Require().NoError(err)
				s.Equal(3, countSamples(db, SampleTypeSpam, SampleOriginUser))

				// second import with cleanup should replace
				input2 := strings.NewReader("new1\nnew2")
				stats, err := samples.Import(ctx, SampleTypeSpam, SampleOriginUser, input2, true)
				s.Require().NoError(err)
				s.Require().NotNil(stats)

				s.Equal(2, countSamples(db, SampleTypeSpam, SampleOriginUser))
				s.Equal(2, stats.UserSpam)

				// verify content was replaced
				res, err := samples.Read(ctx, SampleTypeSpam, SampleOriginUser)
				s.Require().NoError(err)
				s.ElementsMatch([]string{"new1", "new2"}, res)
			})

			s.Run("different types preserve independence", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				// import ham samples
				inputHam := strings.NewReader("ham1\nham2")
				_, err = samples.Import(ctx, SampleTypeHam, SampleOriginUser, inputHam, true)
				s.Require().NoError(err)

				// import spam samples
				inputSpam := strings.NewReader("spam1\nspam2\nspam3")
				stats, err := samples.Import(ctx, SampleTypeSpam, SampleOriginUser, inputSpam, true)
				s.Require().NoError(err)
				s.Require().NotNil(stats)

				s.Equal(2, countSamples(db, SampleTypeHam, SampleOriginUser))
				s.Equal(3, countSamples(db, SampleTypeSpam, SampleOriginUser))
			})

			s.Run("invalid type", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				input := strings.NewReader("sample")
				_, err = samples.Import(ctx, "invalid", SampleOriginPreset, input, true)
				s.Error(err)
			})

			s.Run("invalid origin", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				input := strings.NewReader("sample")
				_, err = samples.Import(ctx, SampleTypeHam, "invalid", input, true)
				s.Error(err)
			})

			s.Run("origin any not allowed", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				input := strings.NewReader("sample")
				_, err = samples.Import(ctx, SampleTypeHam, SampleOriginAny, input, true)
				s.Error(err)
			})

			s.Run("empty input", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				input := strings.NewReader("")
				stats, err := samples.Import(ctx, SampleTypeHam, SampleOriginPreset, input, true)
				s.Require().NoError(err)
				s.Require().NotNil(stats)
				s.Equal(0, countSamples(db, SampleTypeHam, SampleOriginPreset))
			})

			s.Run("input with empty lines", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				input := strings.NewReader("sample1\n\n\nsample2\n\n")
				stats, err := samples.Import(ctx, SampleTypeHam, SampleOriginPreset, input, true)
				s.Require().NoError(err)
				s.Require().NotNil(stats)

				res, err := samples.Read(ctx, SampleTypeHam, SampleOriginPreset)
				s.Require().NoError(err)
				s.ElementsMatch([]string{"sample1", "sample2"}, res)
			})

			s.Run("nil reader", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				_, err = samples.Import(ctx, SampleTypeHam, SampleOriginPreset, nil, true)
				s.Error(err)
			})

			s.Run("reader error", func() {
				samples, err := NewSamples(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE samples")

				errReader := &errorReader{err: fmt.Errorf("read error")}
				_, err = samples.Import(ctx, SampleTypeHam, SampleOriginPreset, errReader, true)
				s.Error(err)
			})
		})
	}
}

func (s *StorageTestSuite) TestSamples_Reader() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			tests := []struct {
				name       string
				setup      func(*Samples)
				sampleType SampleType
				origin     SampleOrigin
				want       []string
				wantErr    bool
			}{
				{
					name: "ham samples",
					setup: func(sm *Samples) {
						s.Require().NoError(sm.Add(context.Background(), SampleTypeHam, SampleOriginPreset, "test1"))
						time.Sleep(time.Second) // ensure each message has a unique timestamp
						s.Require().NoError(sm.Add(context.Background(), SampleTypeHam, SampleOriginPreset, "test2"))
					},
					sampleType: SampleTypeHam,
					origin:     SampleOriginPreset,
					want:       []string{"test2", "test1"}, // ordered by timestamp DESC
				},
				{
					name: "empty result",
					setup: func(s *Samples) {
						// no setup needed, db is empty
					},
					sampleType: SampleTypeSpam,
					origin:     SampleOriginUser,
					want:       []string(nil),
				},
				{
					name:       "invalid type",
					sampleType: "invalid",
					origin:     SampleOriginPreset,
					wantErr:    true,
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					samples, err := NewSamples(ctx, db)
					s.Require().NoError(err)
					defer db.Exec("DROP TABLE samples")

					if tt.setup != nil {
						tt.setup(samples)
					}

					r, err := samples.Reader(ctx, tt.sampleType, tt.origin)
					if tt.wantErr {
						s.Error(err)
						return
					}
					s.Require().NoError(err)

					lines := 0
					scanner := bufio.NewScanner(r)
					var got []string
					for scanner.Scan() {
						lines++
						got = append(got, scanner.Text())
					}
					s.Require().NoError(scanner.Err())
					s.Equal(tt.want, got)
					s.Equal(len(tt.want), lines)

					s.NoError(r.Close())
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestSamples_ReaderEdgeCases() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			s.Run("large message handling", func() {
				// postgres has an 8191 byte limit for indexed columns
				msgSize := 1024 * 1024 // 1MB for SQLite
				if db.Type() == engine.Postgres {
					msgSize = 4096 // 4KB for Postgres
				}
				largeMsg := strings.Repeat("a", msgSize)
				err := samples.Add(ctx, SampleTypeHam, SampleOriginUser, largeMsg)
				s.Require().NoError(err)

				reader, err := samples.Reader(ctx, SampleTypeHam, SampleOriginUser)
				s.Require().NoError(err)
				defer reader.Close()

				// read in small chunks
				buf := make([]byte, 1024)
				total := 0
				for {
					n, err := reader.Read(buf)
					total += n
					if err == io.EOF {
						break
					}
					s.Require().NoError(err)
				}
				s.Equal(len(largeMsg)+1, total) // +1 for newline
			})

			s.Run("multiple close calls", func() {
				reader, err := samples.Reader(ctx, SampleTypeHam, SampleOriginUser)
				s.Require().NoError(err)

				s.Require().NoError(reader.Close())
				s.Require().NoError(reader.Close()) // second close should be safe
				s.Require().NoError(reader.Close()) // multiple closes should be safe
			})

			s.Run("read after close", func() {
				reader, err := samples.Reader(ctx, SampleTypeHam, SampleOriginUser)
				s.Require().NoError(err)

				s.Require().NoError(reader.Close())

				buf := make([]byte, 1024)
				n, err := reader.Read(buf)
				s.Equal(0, n)
				s.Error(err)
			})
		})
	}
}

func (s *StorageTestSuite) TestSamples_ImportEdgeCases() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			s.Run("import very long lines", func() {
				// scanner has a 64KB token size limit by default
				longLine := strings.Repeat("a", 64*1024) // 64KB line
				input := strings.NewReader(longLine)
				_, err := samples.Import(ctx, SampleTypeHam, SampleOriginUser, input, true)
				s.Error(err) // should fail due to scanner limit
				s.Contains(err.Error(), "token too long")
			})

			s.Run("64k-1 line should succeed", func() {
				longLine := strings.Repeat("a", 64*1024-1) // just under limit
				input := strings.NewReader(longLine)
				stats, err := samples.Import(ctx, SampleTypeHam, SampleOriginUser, input, true)
				s.Require().NoError(err)
				s.Equal(1, stats.UserHam)
			})

			s.Run("import with unicode", func() {
				unicodeText := "привет\n你好\nこんにちは\n"
				input := strings.NewReader(unicodeText)
				stats, err := samples.Import(ctx, SampleTypeHam, SampleOriginUser, input, true)
				s.Require().NoError(err)
				s.Equal(3, stats.UserHam)

				// verify content
				samples, err := samples.Read(ctx, SampleTypeHam, SampleOriginUser)
				s.Require().NoError(err)
				s.Contains(samples, "привет")
				s.Contains(samples, "你好")
				s.Contains(samples, "こんにちは")
			})

			s.Run("zero byte reader", func() {
				input := strings.NewReader("")
				stats, err := samples.Import(ctx, SampleTypeHam, SampleOriginUser, input, true)
				s.Require().NoError(err)
				s.Equal(0, stats.UserHam)
			})
		})
	}
}

func (s *StorageTestSuite) TestSamples_IteratorBehavior() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			s.Run("early termination", func() {
				// add test data
				for i := 0; i < 10; i++ {
					err := samples.Add(ctx, SampleTypeHam, SampleOriginUser, fmt.Sprintf("msg%d", i))
					s.Require().NoError(err)
				}

				iter, err := samples.Iterator(ctx, SampleTypeHam, SampleOriginUser)
				s.Require().NoError(err)

				count := 0
				for msg := range iter {
					count++
					if count == 5 {
						break // early termination
					}
					_ = msg
				}
				s.Equal(5, count)
			})

			s.Run("context cancellation", func() {
				ctx, cancel := context.WithCancel(context.Background())
				iter, err := samples.Iterator(ctx, SampleTypeHam, SampleOriginUser)
				s.Require().NoError(err)

				count := 0
				done := make(chan bool)
				go func() {
					for msg := range iter {
						count++
						if count == 2 {
							cancel()
						}
						_ = msg
					}
					done <- true
				}()

				select {
				case <-done:
					s.Less(count, 10)
				case <-time.After(time.Second):
					s.Fail("iterator did not terminate after context cancellation")
				}
			})
		})
	}
}

func (s *StorageTestSuite) TestSamples_Validation() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			s.Run("unicode sample type validation", func() {
				err := samples.Add(ctx, SampleType("спам"), SampleOriginUser, "test")
				s.Error(err)
			})

			s.Run("unicode origin validation", func() {
				err := samples.Add(ctx, SampleTypeHam, SampleOrigin("用户"), "test")
				s.Error(err)
			})

			s.Run("emoji in message", func() {
				msg := "test 👍 message 🚀"
				err := samples.Add(ctx, SampleTypeHam, SampleOriginUser, msg)
				s.Require().NoError(err)

				samples, err := samples.Read(ctx, SampleTypeHam, SampleOriginUser)
				s.Require().NoError(err)
				s.Contains(samples, msg)
			})
		})
	}
}

func (s *StorageTestSuite) TestSamples_DatabaseErrors() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			samples, err := NewSamples(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE samples")

			s.Run("transaction rollback", func() {
				// force table drop to simulate error
				_, err := db.Exec("DROP TABLE samples")
				s.Require().NoError(err)

				err = samples.Add(ctx, SampleTypeHam, SampleOriginUser, "test")
				s.Error(err)
			})

			s.Run("invalid sql", func() {
				// create table with invalid constraint to ensure error on both sqlite and postgres
				invalidSQL := "CREATE TABLE samples (invalid) xyz"
				if db.Type() == engine.Postgres {
					invalidSQL = "CREATE TABLE samples (id INTEGER, CONSTRAINT bad_const CHECK (id > 'text'))"
				}
				_, err := db.Exec(invalidSQL)
				s.Error(err) // should fail on create due to invalid check constraint

				stats, err := samples.Stats(ctx)
				s.Error(err)
				s.Nil(stats)
			})
		})
	}
}

func (s *StorageTestSuite) TestSamples_ImportSizeLimits() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			sm, err := NewSamples(ctx, db)
			s.Require().NoError(err)

			tests := []struct {
				name     string
				input    string
				wantErr  bool
				expected int // expected number of samples
			}{
				{
					name:     "small lines",
					input:    "short line 1\nshort line 2\n",
					wantErr:  false,
					expected: 2,
				},
				{
					name:     "64k-1 line",
					input:    strings.Repeat("a", 64*1024-1) + "\n",
					wantErr:  false,
					expected: 1,
				},
				{
					name:     "64k line fails by default",
					input:    strings.Repeat("a", 64*1024) + "\n",
					wantErr:  true,
					expected: 1,
				},
				{
					name:     "1MB line fails by default",
					input:    strings.Repeat("a", 1024*1024) + "\n",
					wantErr:  true,
					expected: 0,
				},
				{
					name:     "empty lines",
					input:    "\n\n\n",
					wantErr:  false,
					expected: 0,
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					stats, err := sm.Import(ctx, SampleTypeHam, SampleOriginUser, strings.NewReader(tt.input), true)
					if tt.wantErr {
						s.Assert().Error(err)
						return
					}
					s.Require().NoError(err)
					s.Assert().Equal(stats.UserHam, tt.expected)

					// verify content for successful cases
					if !tt.wantErr {
						samples, err := sm.Read(ctx, SampleTypeHam, SampleOriginUser)
						s.Require().NoError(err)
						s.Assert().Len(samples, tt.expected)

						// verify each non-empty line was imported
						for _, line := range strings.Split(strings.TrimSpace(tt.input), "\n") {
							if line != "" {
								s.Assert().Contains(samples, line)
							}
						}
					}
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestSamples_ReaderUnlock() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			sm, err := NewSamples(ctx, db)
			s.Require().NoError(err)

			// initial message
			err = sm.Add(ctx, SampleTypeSpam, SampleOriginUser, "test message 1")
			s.Require().NoError(err)

			// first read
			r1, err := sm.Reader(ctx, SampleTypeSpam, SampleOriginUser)
			s.Require().NoError(err)
			data, err := io.ReadAll(r1)
			s.Require().NoError(err)
			s.Assert().NotEmpty(data)
			r1.Close()

			// should be able to add after read
			err = sm.Add(ctx, SampleTypeSpam, SampleOriginUser, "test message 2")
			s.Require().NoError(err)

			// should be able to read again
			r2, err := sm.Reader(ctx, SampleTypeSpam, SampleOriginUser)
			s.Require().NoError(err)
			data, err = io.ReadAll(r2)
			s.Require().NoError(err)
			s.Assert().NotEmpty(data)
			r2.Close()
		})
	}
}

func (s *StorageTestSuite) TestSamples_LongMessages() {
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			defer db.Exec("DROP TABLE IF EXISTS samples")
			samples, err := NewSamples(context.Background(), db)
			s.Require().NoError(err)

			tests := []struct {
				name    string
				message string
			}{
				{
					name:    "small message",
					message: "hello world",
				},
				{
					name:    "medium message",
					message: strings.Repeat("x", 1000),
				},
				{
					name:    "large message",
					message: strings.Repeat("y", 10000),
				},
				{
					name:    "very large message",
					message: strings.Repeat("z", 100000),
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					// add sample
					err := samples.Add(context.Background(), SampleTypeSpam, SampleOriginUser, tt.message)
					s.Require().NoError(err, "should add message")

					// verify it can be read back
					msgs, err := samples.Read(context.Background(), SampleTypeSpam, SampleOriginUser)
					s.Require().NoError(err)
					s.Require().Contains(msgs, tt.message)

					// verify it can be found and deleted
					err = samples.DeleteMessage(context.Background(), tt.message)
					s.Require().NoError(err, "should delete message")

					// verify it's gone
					msgs, err = samples.Read(context.Background(), SampleTypeSpam, SampleOriginUser)
					s.Require().NoError(err)
					s.Require().NotContains(msgs, tt.message)
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestSamples_DuplicateLongMessages() {
	longMsg := strings.Repeat("x", 50000)

	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			defer db.Exec("DROP TABLE IF EXISTS samples")
			samples, err := NewSamples(context.Background(), db)
			s.Require().NoError(err)

			// add same long message multiple times
			for i := 0; i < 3; i++ {
				err := samples.Add(context.Background(), SampleTypeSpam, SampleOriginUser, longMsg)
				s.Require().NoError(err, "should add message attempt %d", i)
			}

			// verify only one instance exists
			msgs, err := samples.Read(context.Background(), SampleTypeSpam, SampleOriginUser)
			s.Require().NoError(err)
			count := 0
			for _, msg := range msgs {
				if msg == longMsg {
					count++
				}
			}
			s.Equal(1, count, "should have only one instance of the message")
		})
	}
}

// errorReader implements io.Reader interface and always returns an error
type errorReader struct {
	err error
}

func (r *errorReader) Read(_ []byte) (n int, err error) {
	return 0, r.err
}

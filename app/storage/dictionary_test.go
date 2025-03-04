package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
)

func (s *StorageTestSuite) TestNewDictionary() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			s.Run("valid db connection", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				s.NotNil(d)
				defer db.Exec("DROP TABLE dictionary")
			})

			s.Run("nil db connection", func() {
				d, err := NewDictionary(ctx, nil)
				s.Error(err)
				s.Nil(d)
			})
		})
	}
}
func (s *StorageTestSuite) TestDictionary_AddPhrase() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			d, err := NewDictionary(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE dictionary")

			s.Run("valid stop phrase with gid", func() {
				err := d.Add(ctx, DictionaryTypeStopPhrase, "test stop phrase")
				s.NoError(err)
			})

			s.Run("valid ignored word with gid", func() {
				err := d.Add(ctx, DictionaryTypeIgnoredWord, "testword")
				s.NoError(err)
			})

			s.Run("invalid type", func() {
				err := d.Add(ctx, "invalid", "test phrase")
				s.Error(err)
			})

			s.Run("empty phrase", func() {
				err := d.Add(ctx, DictionaryTypeStopPhrase, "")
				s.Error(err)
			})
		})
	}
}

func (s *StorageTestSuite) TestDictionary_Delete() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			d, err := NewDictionary(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE dictionary")

			s.Run("delete existing phrase", func() {
				err := d.Add(ctx, DictionaryTypeStopPhrase, "test phrase")
				s.Require().NoError(err)

				var id int64
				err = db.Get(&id, db.Adopt("SELECT id FROM dictionary WHERE data = ? AND gid = ?"), "test phrase", "gr1")
				s.Require().NoError(err)

				err = d.Delete(ctx, id)
				s.NoError(err)
			})

			s.Run("delete non-existent phrase", func() {
				err := d.Delete(ctx, 99999)
				s.Error(err)
			})
		})
	}
}

func (s *StorageTestSuite) TestDictionary_Read() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			d, err := NewDictionary(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE dictionary")

			// setup test data
			err = d.Add(ctx, DictionaryTypeStopPhrase, "stop1")
			s.Require().NoError(err)
			err = d.Add(ctx, DictionaryTypeStopPhrase, "stop2")
			s.Require().NoError(err)
			err = d.Add(ctx, DictionaryTypeIgnoredWord, "ignored1")
			s.Require().NoError(err)

			s.Run("read stop phrases gr1", func() {
				phrases, err := d.Read(ctx, DictionaryTypeStopPhrase)
				s.NoError(err)
				s.Len(phrases, 2)
				s.Contains(phrases, "stop1")
				s.Contains(phrases, "stop2")
			})

			s.Run("read ignored words group1", func() {
				phrases, err := d.Read(ctx, DictionaryTypeIgnoredWord)
				s.NoError(err)
				s.Len(phrases, 1)
				s.Contains(phrases, "ignored1")
			})

			s.Run("read with invalid type", func() {
				phrases, err := d.Read(ctx, "invalid")
				s.Error(err)
				s.Nil(phrases)
			})
		})
	}
}

func (s *StorageTestSuite) TestDictionary_Iterator() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			d, err := NewDictionary(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE dictionary")

			// setup test data
			err = d.Add(ctx, DictionaryTypeStopPhrase, "stop1")
			s.Require().NoError(err)
			err = d.Add(ctx, DictionaryTypeStopPhrase, "stop2")
			s.Require().NoError(err)
			err = d.Add(ctx, DictionaryTypeStopPhrase, "stop3")
			s.Require().NoError(err)

			s.Run("iterate group", func() {
				iter, err := d.Iterator(ctx, DictionaryTypeStopPhrase)
				s.Require().NoError(err)

				var phrases []string
				for phrase := range iter {
					phrases = append(phrases, phrase)
				}
				s.Len(phrases, 3)
				s.Contains(phrases, "stop1")
				s.Contains(phrases, "stop2")
				s.Contains(phrases, "stop3")
			})

			s.Run("iterate with invalid type", func() {
				iter, err := d.Iterator(ctx, "invalid")
				s.Error(err)
				s.Nil(iter)
			})
		})
	}
}

func (s *StorageTestSuite) TestDictionary_Stats() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			d, err := NewDictionary(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE dictionary")

			// setup test data
			err = d.Add(ctx, DictionaryTypeStopPhrase, "stop1")
			s.Require().NoError(err)
			err = d.Add(ctx, DictionaryTypeStopPhrase, "stop2")
			s.Require().NoError(err)
			err = d.Add(ctx, DictionaryTypeStopPhrase, "stop3")
			s.Require().NoError(err)
			err = d.Add(ctx, DictionaryTypeIgnoredWord, "ignored1")
			s.Require().NoError(err)
			err = d.Add(ctx, DictionaryTypeIgnoredWord, "ignored2")
			s.Require().NoError(err)

			stats, err := d.Stats(ctx)
			s.Require().NoError(err)
			s.Require().NotNil(stats)
			s.Equal(3, stats.TotalStopPhrases)
			s.Equal(2, stats.TotalIgnoredWords)
		})
	}
}

func (s *StorageTestSuite) TestDictionary_Import() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			s.Run("import with cleanup", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE dictionary")

				input := strings.NewReader(`phrase1,phrase2,"phrase 3, with comma"`)
				stats, err := d.Import(ctx, DictionaryTypeStopPhrase, input, true)
				s.Require().NoError(err)
				s.Require().NotNil(stats)

				phrases, err := d.Read(ctx, DictionaryTypeStopPhrase)
				s.Require().NoError(err)
				s.Equal(3, len(phrases))
				s.Contains(phrases, "phrase1")
				s.Contains(phrases, "phrase2")
				s.Contains(phrases, "phrase 3, with comma")
			})

			s.Run("import without cleanup", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE dictionary")

				input1 := strings.NewReader(`existing1,"existing 2"`)
				_, err = d.Import(ctx, DictionaryTypeIgnoredWord, input1, true)
				s.Require().NoError(err)

				input2 := strings.NewReader(`new1,new2`)
				stats, err := d.Import(ctx, DictionaryTypeIgnoredWord, input2, false)
				s.Require().NoError(err)
				s.Require().NotNil(stats)

				phrases, err := d.Read(ctx, DictionaryTypeIgnoredWord)
				s.Require().NoError(err)
				s.Equal(4, len(phrases))
			})

			s.Run("import with invalid input", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE dictionary")

				input := strings.NewReader(`this is "bad csv`)
				_, err = d.Import(ctx, DictionaryTypeStopPhrase, input, true)
				s.Error(err)
			})

			s.Run("quoted strings and special chars", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE dictionary")

				input := strings.NewReader(`"phrase with, comma",simple phrase,"quoted ""special"" chars"`)
				stats, err := d.Import(ctx, DictionaryTypeStopPhrase, input, true)
				s.Require().NoError(err)
				s.Require().NotNil(stats)

				phrases, err := d.Read(ctx, DictionaryTypeStopPhrase)
				s.Require().NoError(err)
				s.Require().Len(phrases, 3)
				s.Contains(phrases, `phrase with, comma`)
				s.Contains(phrases, `simple phrase`)
				s.Contains(phrases, `quoted "special" chars`)
			})

			s.Run("duplicate phrases", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE dictionary")

				input := strings.NewReader("phrase1,phrase1,phrase1")
				stats, err := d.Import(ctx, DictionaryTypeStopPhrase, input, true)
				s.Require().NoError(err)
				s.Require().NotNil(stats)

				phrases, err := d.Read(ctx, DictionaryTypeStopPhrase)
				s.Require().NoError(err)
				s.Len(phrases, 1)
			})
		})
	}
}

func (s *StorageTestSuite) TestDictionaryType_Validate() {
	tests := []struct {
		name    string
		dType   DictionaryType
		wantErr bool
	}{
		{"valid stop phrase", DictionaryTypeStopPhrase, false},
		{"valid ignored word", DictionaryTypeIgnoredWord, false},
		{"invalid type", "invalid", true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := tt.dType.Validate()
			if tt.wantErr {
				s.Assert().Error(err)
			} else {
				s.Assert().NoError(err)
			}
		})
	}
}

func (s *StorageTestSuite) TestDictionary_Reader() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			s.Run("stop phrases", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE dictionary")

				s.Require().NoError(d.Add(ctx, DictionaryTypeStopPhrase, "test1"))
				s.Require().NoError(d.Add(ctx, DictionaryTypeStopPhrase, "test2"))

				r, err := d.Reader(ctx, DictionaryTypeStopPhrase)
				s.Require().NoError(err)
				defer r.Close()

				data, err := io.ReadAll(r)
				s.Require().NoError(err)
				got := strings.Split(string(data), "\n")
				s.Equal([]string{"test1", "test2"}, got)
			})

			s.Run("empty result", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE dictionary")

				r, err := d.Reader(ctx, DictionaryTypeIgnoredWord)
				s.Require().NoError(err)
				defer r.Close()

				data, err := io.ReadAll(r)
				s.Require().NoError(err)
				s.Empty(string(data))
			})

			s.Run("invalid type", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE dictionary")

				r, err := d.Reader(ctx, "invalid")
				s.Error(err)
				s.Nil(r)
			})

			s.Run("mixed entries", func() {
				d, err := NewDictionary(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE dictionary")

				s.Require().NoError(d.Add(ctx, DictionaryTypeIgnoredWord, "ignored1"))
				s.Require().NoError(d.Add(ctx, DictionaryTypeStopPhrase, "stop1"))
				s.Require().NoError(d.Add(ctx, DictionaryTypeIgnoredWord, "ignored2"))

				r, err := d.Reader(ctx, DictionaryTypeIgnoredWord)
				s.Require().NoError(err)
				defer r.Close()

				data, err := io.ReadAll(r)
				s.Require().NoError(err)
				got := strings.Split(string(data), "\n")
				s.Equal([]string{"ignored1", "ignored2"}, got)
			})
		})
	}
}

func (s *StorageTestSuite) TestDictionary_Concurrent() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// initialize dictionary with schema
			d, err := NewDictionary(ctx, db)
			s.Require().NoError(err)
			s.Require().NotNil(d)
			defer db.Exec("DROP TABLE dictionary")

			// verify table exists and is accessible
			err = d.Add(ctx, DictionaryTypeStopPhrase, "test phrase")
			s.Require().NoError(err, "Failed to insert initial test phrase")

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
						if _, err := d.Read(ctx, DictionaryTypeStopPhrase); err != nil {
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
						phrase := fmt.Sprintf("test phrase %d-%d", workerID, j)
						dType := DictionaryTypeStopPhrase
						if j%2 == 0 {
							dType = DictionaryTypeIgnoredWord
						}
						if err := d.Add(ctx, dType, phrase); err != nil {
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
			stats, err := d.Stats(ctx)
			s.Require().NoError(err)
			s.Require().NotNil(stats)

			expectedTotal := numWorkers*numOps + 1 // +1 for the initial test phrase
			actualTotal := stats.TotalStopPhrases + stats.TotalIgnoredWords
			s.Equal(expectedTotal, actualTotal, "expected %d total phrases, got %d", expectedTotal, actualTotal)
		})
	}
}

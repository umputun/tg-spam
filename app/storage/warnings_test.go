package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/umputun/tg-spam/app/storage/engine"
)

func (s *StorageTestSuite) TestWarnings_NewWarnings() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			s.Run("create new table", func() {
				_, err := NewWarnings(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE warnings")

				var exists int
				err = db.Get(&exists, `SELECT COUNT(*) FROM warnings`)
				s.Require().NoError(err)
				s.Equal(0, exists)
			})

			s.Run("nil db connection", func() {
				_, err := NewWarnings(ctx, nil)
				s.Require().Error(err)
				s.Contains(err.Error(), "db connection is nil")
			})

			s.Run("context canceled", func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := NewWarnings(ctx, db)
				s.Require().Error(err)
				defer db.Exec("DROP TABLE warnings")
			})
		})
	}
}

func (s *StorageTestSuite) TestWarnings_Add() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			warnings, err := NewWarnings(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE warnings")

			s.Run("single insert", func() {
				err := warnings.Add(ctx, 100, "alice")
				s.Require().NoError(err)

				count, err := warnings.CountWithin(ctx, 100, time.Hour)
				s.Require().NoError(err)
				s.Equal(1, count)
			})

			s.Run("multiple inserts for same user", func() {
				err := warnings.Add(ctx, 200, "bob")
				s.Require().NoError(err)
				err = warnings.Add(ctx, 200, "bob")
				s.Require().NoError(err)
				err = warnings.Add(ctx, 200, "bob")
				s.Require().NoError(err)

				count, err := warnings.CountWithin(ctx, 200, time.Hour)
				s.Require().NoError(err)
				s.Equal(3, count)
			})

			s.Run("multi-user isolation", func() {
				err := warnings.Add(ctx, 301, "carol")
				s.Require().NoError(err)
				err = warnings.Add(ctx, 302, "dave")
				s.Require().NoError(err)
				err = warnings.Add(ctx, 302, "dave")
				s.Require().NoError(err)

				countCarol, err := warnings.CountWithin(ctx, 301, time.Hour)
				s.Require().NoError(err)
				s.Equal(1, countCarol)

				countDave, err := warnings.CountWithin(ctx, 302, time.Hour)
				s.Require().NoError(err)
				s.Equal(2, countDave)
			})
		})
	}
}

func (s *StorageTestSuite) TestWarnings_AddMultiGIDIsolation() {
	ctx := context.Background()

	s.Run("sqlite multi-gid isolation", func() {
		db1, err := engine.NewSqlite(":memory:", "gA")
		s.Require().NoError(err)
		defer db1.Close()
		db2, err := engine.NewSqlite(":memory:", "gB")
		s.Require().NoError(err)
		defer db2.Close()

		w1, err := NewWarnings(ctx, db1)
		s.Require().NoError(err)
		w2, err := NewWarnings(ctx, db2)
		s.Require().NoError(err)

		err = w1.Add(ctx, 500, "user-A")
		s.Require().NoError(err)
		err = w1.Add(ctx, 500, "user-A")
		s.Require().NoError(err)
		err = w2.Add(ctx, 500, "user-B")
		s.Require().NoError(err)

		c1, err := w1.CountWithin(ctx, 500, time.Hour)
		s.Require().NoError(err)
		s.Equal(2, c1)

		c2, err := w2.CountWithin(ctx, 500, time.Hour)
		s.Require().NoError(err)
		s.Equal(1, c2)
	})
}

func (s *StorageTestSuite) TestWarnings_CountWithin() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			warnings, err := NewWarnings(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE warnings")

			s.Run("zero result for unknown user", func() {
				c, err := warnings.CountWithin(ctx, 9999, time.Hour)
				s.Require().NoError(err)
				s.Equal(0, c)
			})

			s.Run("counts only inside window", func() {
				err := warnings.Add(ctx, 700, "user-window")
				s.Require().NoError(err)

				query := warnings.Adopt(
					"INSERT INTO warnings (gid, user_id, user_name, created_at) VALUES (?, ?, ?, ?)")
				_, err = warnings.ExecContext(ctx, query, warnings.GID(), int64(700), "user-window",
					time.Now().Add(-2*time.Hour))
				s.Require().NoError(err)

				cAll, err := warnings.CountWithin(ctx, 700, 24*time.Hour)
				s.Require().NoError(err)
				s.Equal(2, cAll)

				cInside, err := warnings.CountWithin(ctx, 700, 30*time.Minute)
				s.Require().NoError(err)
				s.Equal(1, cInside)
			})

			s.Run("zero window returns zero", func() {
				err := warnings.Add(ctx, 800, "edge-user")
				s.Require().NoError(err)

				c, err := warnings.CountWithin(ctx, 800, 0)
				s.Require().NoError(err)
				s.Equal(0, c)
			})
		})
	}
}

func (s *StorageTestSuite) TestWarnings_CleanupOld() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			warnings, err := NewWarnings(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE warnings")

			s.Run("rows older than retention pruned on Add", func() {
				err := warnings.Add(ctx, 1000, "old-user")
				s.Require().NoError(err)

				query := warnings.Adopt(
					"UPDATE warnings SET created_at = ? WHERE user_id = ? AND gid = ?")
				_, err = warnings.ExecContext(ctx, query,
					time.Now().Add(-warningsRetention-24*time.Hour), int64(1000), warnings.GID())
				s.Require().NoError(err)

				err = warnings.Add(ctx, 1001, "trigger")
				s.Require().NoError(err)

				cOld, err := warnings.CountWithin(ctx, 1000, warningsRetention+48*time.Hour)
				s.Require().NoError(err)
				s.Equal(0, cOld)

				cNew, err := warnings.CountWithin(ctx, 1001, time.Hour)
				s.Require().NoError(err)
				s.Equal(1, cNew)
			})

			s.Run("recent rows preserved", func() {
				err := warnings.Add(ctx, 1100, "recent-user")
				s.Require().NoError(err)

				query := warnings.Adopt(
					"UPDATE warnings SET created_at = ? WHERE user_id = ? AND gid = ?")
				_, err = warnings.ExecContext(ctx, query,
					time.Now().Add(-30*24*time.Hour), int64(1100), warnings.GID())
				s.Require().NoError(err)

				err = warnings.Add(ctx, 1101, "trigger2")
				s.Require().NoError(err)

				c, err := warnings.CountWithin(ctx, 1100, 365*24*time.Hour)
				s.Require().NoError(err)
				s.Equal(1, c)
			})
		})
	}
}

// TestWarnings_CleanupOldGIDIsolation verifies that one tenant's cleanup does not
// prune another tenant's old rows when both share the same physical database.
// uses a single sqlite file with two SQL connections holding different gids.
func (s *StorageTestSuite) TestWarnings_CleanupOldGIDIsolation() {
	ctx := context.Background()

	tmpFile := filepath.Join(os.TempDir(), "warnings_cleanup_isolation.sqlite")
	defer os.Remove(tmpFile)

	dbA, err := engine.NewSqlite(tmpFile, "tenantA")
	s.Require().NoError(err)
	defer dbA.Close()
	dbB, err := engine.NewSqlite(tmpFile, "tenantB")
	s.Require().NoError(err)
	defer dbB.Close()

	wA, err := NewWarnings(ctx, dbA)
	s.Require().NoError(err)
	wB, err := NewWarnings(ctx, dbB)
	s.Require().NoError(err)

	// seed tenantA with an aged row (older than retention) that should NOT be touched
	// when tenantB's Add triggers cleanupOld.
	err = wA.Add(ctx, 700, "tenantA-user")
	s.Require().NoError(err)
	updateQuery := wA.Adopt("UPDATE warnings SET created_at = ? WHERE user_id = ? AND gid = ?")
	_, err = wA.ExecContext(ctx, updateQuery,
		time.Now().Add(-warningsRetention-48*time.Hour), int64(700), wA.GID())
	s.Require().NoError(err)

	// trigger tenantB cleanup via Add
	err = wB.Add(ctx, 800, "tenantB-user")
	s.Require().NoError(err)

	// tenantA's aged row must still be present (cleanup is gid-scoped)
	var aCount int
	countQuery := wA.Adopt("SELECT COUNT(*) FROM warnings WHERE gid = ? AND user_id = ?")
	err = wA.GetContext(ctx, &aCount, countQuery, wA.GID(), int64(700))
	s.Require().NoError(err)
	s.Equal(1, aCount, "tenantA aged row must survive tenantB cleanup")

	// also verify each tenant only sees its own active rows via CountWithin
	cA, err := wA.CountWithin(ctx, 700, 365*24*time.Hour)
	s.Require().NoError(err)
	s.Equal(0, cA, "tenantA aged row is older than max query window")

	cB, err := wB.CountWithin(ctx, 800, time.Hour)
	s.Require().NoError(err)
	s.Equal(1, cB)
}

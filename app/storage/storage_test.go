package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/umputun/tg-spam/app/storage/engine"
)

type StorageTestSuite struct {
	suite.Suite
	dbs         map[string]*engine.SQL
	pgContainer testcontainers.Container
}

func TestStorageSuite(t *testing.T) {
	suite.Run(t, new(StorageTestSuite))
}

func (s *StorageTestSuite) SetupSuite() {
	s.dbs = make(map[string]*engine.SQL)

	// Setup SQLite
	sqliteDB, err := engine.NewSqlite(":memory:", "gr1")
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

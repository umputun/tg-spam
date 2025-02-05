package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// engineProvider defines a function type that provides a test database engine
type engineProvider func(t *testing.T, ctx context.Context) (db *engine.SQL, teardown func())

// database providers for each supported engine
var providers = map[string]engineProvider{
	"sqlite": func(t *testing.T, ctx context.Context) (*engine.SQL, func()) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		return db, func() { db.Close() }
	},
	"postgres": func(t *testing.T, ctx context.Context) (*engine.SQL, func()) {
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
		require.NoError(t, err)

		// wait a bit to ensure container is fully ready
		time.Sleep(time.Second)

		host, err := container.Host(ctx)
		require.NoError(t, err)
		port, err := container.MappedPort(ctx, "5432")
		require.NoError(t, err)

		connStr := fmt.Sprintf("postgres://postgres:secret@%s:%d/test?sslmode=disable", host, port.Int())
		db, err := engine.NewPostgres(ctx, connStr, "gr1")
		require.NoError(t, err)

		return db, func() {
			db.Close()
			assert.NoError(t, container.Terminate(ctx))
		}
	},
}

package containers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresTestContainer is a wrapper around a testcontainers.Container that provides a PostgreSQL server
type PostgresTestContainer struct {
	Container testcontainers.Container
	Host      string
	Port      nat.Port
	User      string
	Password  string
	Database  string
}

// NewPostgresTestContainer creates a new PostgreSQL test container with default settings
func NewPostgresTestContainer(ctx context.Context, t *testing.T) *PostgresTestContainer {
	return NewPostgresTestContainerWithDB(ctx, t, "test")
}

// NewPostgresTestContainerWithDB creates a new PostgreSQL test container with a specific database name
func NewPostgresTestContainerWithDB(ctx context.Context, t *testing.T, dbName string) *PostgresTestContainer {
	const (
		defaultUser     = "postgres"
		defaultPassword = "secret"
	)

	req := testcontainers.ContainerRequest{
		Image:        "postgres:17",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": defaultPassword,
			"POSTGRES_DB":       dbName,
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort("5432/tcp"),
		).WithDeadline(time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	return &PostgresTestContainer{
		Container: container,
		Host:      host,
		Port:      port,
		User:      defaultUser,
		Password:  defaultPassword,
		Database:  dbName,
	}
}

// ConnectionString returns the PostgreSQL connection string for this container
func (pc *PostgresTestContainer) ConnectionString() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		pc.User, pc.Password, pc.Host, pc.Port.Int(), pc.Database)
}

// Close terminates the container
func (pc *PostgresTestContainer) Close(ctx context.Context) error {
	return pc.Container.Terminate(ctx)
}

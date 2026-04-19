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

// MySQLTestContainer is a wrapper around a testcontainers.Container that provides a MySQL server
type MySQLTestContainer struct {
	Container testcontainers.Container
	Host      string
	Port      nat.Port
	User      string
	Password  string
	Database  string
}

// NewMySQLTestContainer creates a new MySQL test container with default settings
func NewMySQLTestContainer(ctx context.Context, t *testing.T) *MySQLTestContainer {
	return NewMySQLTestContainerWithDB(ctx, t, "test")
}

// NewMySQLTestContainerE creates a new MySQL test container with default settings.
// Returns error instead of using require.NoError, suitable for TestMain usage.
func NewMySQLTestContainerE(ctx context.Context) (*MySQLTestContainer, error) {
	return NewMySQLTestContainerWithDBE(ctx, "test")
}

// NewMySQLTestContainerWithDB creates a new MySQL test container with a specific database name
func NewMySQLTestContainerWithDB(ctx context.Context, t *testing.T, dbName string) *MySQLTestContainer {
	mc, err := NewMySQLTestContainerWithDBE(ctx, dbName)
	require.NoError(t, err)
	return mc
}

// NewMySQLTestContainerWithDBE creates a new MySQL test container with a specific database name.
// Returns error instead of using require.NoError, suitable for TestMain usage.
func NewMySQLTestContainerWithDBE(ctx context.Context, dbName string) (*MySQLTestContainer, error) {
	const (
		defaultUser     = "root"
		defaultPassword = "secret"
	)

	req := testcontainers.ContainerRequest{
		Image:        "mysql:8",
		ExposedPorts: []string{"3306/tcp"},
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": defaultPassword,
			"MYSQL_DATABASE":      dbName,
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("port: 3306  MySQL Community Server"),
			wait.ForListeningPort("3306/tcp"),
		).WithDeadline(time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create mysql container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "3306")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get mapped port: %w", err)
	}

	return &MySQLTestContainer{
		Container: container,
		Host:      host,
		Port:      nat.Port(port.String()),
		User:      defaultUser,
		Password:  defaultPassword,
		Database:  dbName,
	}, nil
}

// ConnectionString returns the MySQL connection string for this container
func (mc *MySQLTestContainer) ConnectionString() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		mc.User, mc.Password, mc.Host, mc.Port.Int(), mc.Database)
}

// DSN returns the MySQL DSN for this container
func (mc *MySQLTestContainer) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
		mc.User, mc.Password, mc.Host, mc.Port.Int(), mc.Database)
}

// Close terminates the container
func (mc *MySQLTestContainer) Close(ctx context.Context) error {
	return mc.Container.Terminate(ctx)
}

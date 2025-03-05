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

// NewMySQLTestContainerWithDB creates a new MySQL test container with a specific database name
func NewMySQLTestContainerWithDB(ctx context.Context, t *testing.T, dbName string) *MySQLTestContainer {
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
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "3306")
	require.NoError(t, err)

	return &MySQLTestContainer{
		Container: container,
		Host:      host,
		Port:      port,
		User:      defaultUser,
		Password:  defaultPassword,
		Database:  dbName,
	}
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

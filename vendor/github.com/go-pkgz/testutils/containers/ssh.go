package containers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// SSHTestContainer is a wrapper around a testcontainers.Container that provides an SSH server
type SSHTestContainer struct {
	Container testcontainers.Container
	Host      string
	Port      nat.Port
	User      string
}

// NewSSHTestContainer creates a new SSH test container and returns an SSHTestContainer instance
func NewSSHTestContainer(ctx context.Context, t *testing.T) *SSHTestContainer {
	return NewSSHTestContainerWithUser(ctx, t, "test")
}

// NewSSHTestContainerWithUser creates a new SSH test container with a specific user
func NewSSHTestContainerWithUser(ctx context.Context, t *testing.T, user string) *SSHTestContainer {
	pubKey, err := os.ReadFile("testdata/test_ssh_key.pub")
	require.NoError(t, err)

	req := testcontainers.ContainerRequest{
		Image:        "lscr.io/linuxserver/openssh-server:latest",
		ExposedPorts: []string{"2222/tcp"},
		WaitingFor:   wait.NewLogStrategy("done.").WithStartupTimeout(time.Minute),
		Files: []testcontainers.ContainerFile{
			{HostFilePath: "testdata/test_ssh_key.pub", ContainerFilePath: "/authorized_key"},
		},
		Env: map[string]string{
			"PUBLIC_KEY":  string(pubKey),
			"USER_NAME":   user,
			"TZ":          "Etc/UTC",
			"SUDO_ACCESS": "true",
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "2222")
	require.NoError(t, err)

	return &SSHTestContainer{
		Container: container,
		Host:      host,
		Port:      port,
		User:      user,
	}
}

// Address returns the SSH server address in host:port format
func (sc *SSHTestContainer) Address() string {
	return fmt.Sprintf("%s:%s", sc.Host, sc.Port.Port())
}

// Close terminates the container
func (sc *SSHTestContainer) Close(ctx context.Context) error {
	return sc.Container.Terminate(ctx)
}

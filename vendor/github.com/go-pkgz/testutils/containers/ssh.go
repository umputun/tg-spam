package containers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/pkg/sftp"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/ssh"
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

// NewSSHTestContainerE creates a new SSH test container and returns an SSHTestContainer instance.
// Returns error instead of using require.NoError, suitable for TestMain usage.
func NewSSHTestContainerE(ctx context.Context) (*SSHTestContainer, error) {
	return NewSSHTestContainerWithUserE(ctx, "test")
}

// NewSSHTestContainerWithUser creates a new SSH test container with a specific user
func NewSSHTestContainerWithUser(ctx context.Context, t *testing.T, user string) *SSHTestContainer {
	sc, err := NewSSHTestContainerWithUserE(ctx, user)
	require.NoError(t, err)
	return sc
}

// NewSSHTestContainerWithUserE creates a new SSH test container with a specific user.
// Returns error instead of using require.NoError, suitable for TestMain usage.
func NewSSHTestContainerWithUserE(ctx context.Context, user string) (*SSHTestContainer, error) {
	pubKey, err := os.ReadFile("testdata/test_ssh_key.pub")
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH public key: %w", err)
	}

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
	if err != nil {
		return nil, fmt.Errorf("failed to create ssh container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "2222")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get mapped port: %w", err)
	}

	return &SSHTestContainer{
		Container: container,
		Host:      host,
		Port:      nat.Port(port.String()),
		User:      user,
	}, nil
}

// Address returns the SSH server address in host:port format
func (sc *SSHTestContainer) Address() string {
	return fmt.Sprintf("%s:%s", sc.Host, sc.Port.Port())
}

// connect establishes an SSH connection and returns a SFTP client
func (sc *SSHTestContainer) connect(_ context.Context) (sftpClient *sftp.Client, sshClient *ssh.Client, err error) {
	key, err := os.ReadFile("testdata/test_ssh_key")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read SSH private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse SSH private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: sc.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		// #nosec G106 -- InsecureIgnoreHostKey is acceptable for test containers
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	addr := sc.Address()
	sshClient, err = ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to dial SSH server at %s: %w", addr, err)
	}

	sftpClient, err = sftp.NewClient(sshClient)
	if err != nil {
		if closeErr := sshClient.Close(); closeErr != nil {
			return nil, nil, fmt.Errorf("failed to create SFTP client: %w and failed to close SSH client: %v", err, closeErr)
		}
		return nil, nil, fmt.Errorf("failed to create SFTP client: %w", err)
	}

	return sftpClient, sshClient, nil
}

// GetFile downloads a file from the SSH server
func (sc *SSHTestContainer) GetFile(ctx context.Context, remotePath, localPath string) error {
	sftpClient, sshClient, err := sc.connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to SSH server for GetFile: %w", err)
	}
	defer sftpClient.Close()
	defer sshClient.Close()

	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0o750); err != nil {
		return fmt.Errorf("failed to create local directory %s: %w", localDir, err)
	}

	if !strings.HasPrefix(filepath.Clean(localPath), filepath.Clean(localDir)) {
		return fmt.Errorf("localPath %s attempts to escape from directory %s", localPath, localDir)
	}

	// open remote file
	remoteFile, err := sftpClient.Open(remotePath)
	if err != nil {
		return fmt.Errorf("failed to open remote file %s: %w", remotePath, err)
	}
	defer remoteFile.Close()

	// create local file
	localFile, err := os.OpenFile(localPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- localPath validated above
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localPath, err)
	}
	defer localFile.Close()

	// copy remote file to local file
	if _, err := io.Copy(localFile, remoteFile); err != nil {
		return fmt.Errorf("failed to copy file content from %s to %s: %w", remotePath, localPath, err)
	}

	return nil
}

// SaveFile uploads a file to the SSH server
func (sc *SSHTestContainer) SaveFile(ctx context.Context, localPath, remotePath string) error {
	sftpClient, sshClient, err := sc.connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to SSH server for SaveFile: %w", err)
	}
	defer sftpClient.Close()
	defer sshClient.Close()

	if !strings.HasPrefix(filepath.Clean(localPath), filepath.Clean(filepath.Dir(localPath))) {
		return fmt.Errorf("localPath %s attempts to escape from its directory", localPath)
	}

	// open local file
	localFile, err := os.Open(localPath) // #nosec G304 -- localPath validated above
	if err != nil {
		return fmt.Errorf("failed to open local file %s: %w", localPath, err)
	}
	defer localFile.Close()

	// create remote directory if it doesn't exist
	remoteDir := filepath.Dir(remotePath)
	if remoteDir != "." && remoteDir != "/" {
		if err := sc.createDirRecursive(sftpClient, remoteDir); err != nil {
			return fmt.Errorf("failed to create remote directory %s: %w", remoteDir, err)
		}
	}

	// create remote file
	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("failed to create remote file %s: %w", remotePath, err)
	}
	defer remoteFile.Close()

	// copy local file to remote file
	if _, err := io.Copy(remoteFile, localFile); err != nil {
		return fmt.Errorf("failed to copy file content from %s to %s: %w", localPath, remotePath, err)
	}

	return nil
}

// create directories recursively
func (sc *SSHTestContainer) createDirRecursive(sftpClient *sftp.Client, remotePath string) error {
	// handle empty path
	if remotePath == "" || remotePath == "." {
		return nil
	}

	// normalize path - always use forward slashes for SFTP
	remotePath = filepath.ToSlash(remotePath)

	// check if path is absolute
	isAbsolute := strings.HasPrefix(remotePath, "/")

	// get starting path
	var current string
	if isAbsolute {
		current = "/"
		remotePath = strings.TrimPrefix(remotePath, "/")
	} else {
		// for relative paths, start from current working directory
		var err error
		current, err = sftpClient.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// split path into parts and create directories
	parts := strings.Split(strings.Trim(remotePath, "/"), "/")
	for _, part := range parts {
		if part == "" {
			continue
		}

		// validate path component to prevent traversal attacks
		if part == ".." || strings.Contains(part, "\x00") {
			return fmt.Errorf("invalid path component: %q", part)
		}

		// use forward slashes for SFTP paths
		if current == "/" {
			current = "/" + part
		} else {
			current = current + "/" + part
		}

		// attempt to create directory - this handles race conditions better
		if err := sftpClient.Mkdir(current); err != nil {
			// if directory already exists, verify it's actually a directory
			// sftp errors don't always map to os.IsExist, so check the actual error
			if !os.IsExist(err) && !strings.Contains(err.Error(), "Failure") {
				return fmt.Errorf("failed to create directory %s: %w", current, err)
			}

			// directory might exist, verify it's actually a directory
			info, statErr := sftpClient.Stat(current)
			if statErr == nil && !info.IsDir() {
				return fmt.Errorf("path exists but is not a directory: %s", current)
			}
			// directory exists or we can't stat it, continue
			continue
		}
	}

	return nil
}

// ListFiles lists files in a directory on the SSH server
func (sc *SSHTestContainer) ListFiles(ctx context.Context, remotePath string) ([]os.FileInfo, error) {
	sftpClient, sshClient, err := sc.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH server for ListFiles: %w", err)
	}
	defer sftpClient.Close()
	defer sshClient.Close()

	// use root directory if path is empty
	if remotePath == "" || remotePath == "." {
		remotePath = "/"
	}

	// get file info
	files, err := sftpClient.ReadDir(remotePath)
	if err != nil {
		return nil, fmt.Errorf("failed to list files in remote path '%s': %w", remotePath, err)
	}

	return files, nil
}

// DeleteFile deletes a file from the SSH server
func (sc *SSHTestContainer) DeleteFile(ctx context.Context, remotePath string) error {
	sftpClient, sshClient, err := sc.connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to SSH server for DeleteFile: %w", err)
	}
	defer sftpClient.Close()
	defer sshClient.Close()

	// delete file
	if err := sftpClient.Remove(remotePath); err != nil {
		return fmt.Errorf("failed to delete remote file %s: %w", remotePath, err)
	}

	return nil
}

// Close terminates the container
func (sc *SSHTestContainer) Close(ctx context.Context) error {
	return sc.Container.Terminate(ctx)
}

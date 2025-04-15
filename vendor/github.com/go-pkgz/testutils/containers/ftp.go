// Package containers implements various test containers for integration testing
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
	"github.com/jlaffaye/ftp"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// FTPTestContainer is a wrapper around a testcontainers.Container that provides an FTP server
// for testing purposes. It allows file uploads, downloads, and directory operations.
type FTPTestContainer struct {
	Container testcontainers.Container
	Host      string
	Port      nat.Port // represents the *host* port struct
	User      string
	Password  string
}

// NewFTPTestContainer uses delfer/alpine-ftp-server, minimal env vars, fixed host port mapping syntax.
func NewFTPTestContainer(ctx context.Context, t *testing.T) *FTPTestContainer {
	const (
		defaultUser          = "ftpuser"
		defaultPassword      = "ftppass"
		pasvMinPort          = "21000" // default passive port range for the image
		pasvMaxPort          = "21010"
		fixedHostControlPort = "2121"
	)

	// set up logging for testcontainers if the appropriate API is available
	t.Logf("Setting up FTP test container")

	pasvPortRangeContainer := fmt.Sprintf("%s-%s", pasvMinPort, pasvMaxPort)
	pasvPortRangeHost := fmt.Sprintf("%s-%s", pasvMinPort, pasvMaxPort) // map 1:1
	exposedPortsWithBinding := []string{
		fmt.Sprintf("%s:21/tcp", fixedHostControlPort),                      // "2121:21/tcp"
		fmt.Sprintf("%s:%s/tcp", pasvPortRangeHost, pasvPortRangeContainer), // "21000-21010:21000-21010/tcp"
	}

	imageName := "delfer/alpine-ftp-server:latest"
	t.Logf("Using FTP server image: %s", imageName)

	req := testcontainers.ContainerRequest{
		Image:        imageName,
		ExposedPorts: exposedPortsWithBinding,
		Env: map[string]string{
			"USERS": fmt.Sprintf("%s|%s", defaultUser, defaultPassword),
		},
		WaitingFor: wait.ForListeningPort(nat.Port("21/tcp")).WithStartupTimeout(2 * time.Minute),
	}

	t.Logf("creating FTP container using %s (minimal env vars, fixed host port %s)...", imageName, fixedHostControlPort)
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	// create the container instance to use its methods
	ftpContainer := &FTPTestContainer{}

	// error handling with detailed logging for container startup issues
	if err != nil {
		ftpContainer.logContainerError(ctx, t, container, err, imageName)
	}
	t.Logf("FTP container created and started (ID: %s)", container.GetContainerID())

	host, err := container.Host(ctx)
	require.NoError(t, err, "Failed to get container host")

	// since we requested a fixed port, construct the nat.Port struct directly
	// we still call MappedPort just to ensure the container is properly exposing *something* for port 21
	_, err = container.MappedPort(ctx, "21")
	require.NoError(t, err, "Failed to get mapped port info for container port 21/tcp (even though fixed)")

	// construct the Port struct based on our fixed request
	fixedHostNatPort, err := nat.NewPort("tcp", fixedHostControlPort)
	require.NoError(t, err, "Failed to create nat.Port for fixed host port")

	t.Logf("FTP container should be accessible at: %s:%s (Control Plane)", host, fixedHostControlPort)
	t.Logf("FTP server using default config, passive ports %s mapped to host %s", pasvPortRangeContainer, pasvPortRangeHost)

	time.Sleep(1 * time.Second)

	return &FTPTestContainer{
		Container: container,
		Host:      host,
		Port:      fixedHostNatPort, // use the manually constructed nat.Port for the fixed host port
		User:      defaultUser,
		Password:  defaultPassword,
	}
}

// connect function (Use default EPSV enabled)
func (fc *FTPTestContainer) connect(ctx context.Context) (*ftp.ServerConn, error) {
	opts := []ftp.DialOption{
		ftp.DialWithTimeout(30 * time.Second),
		ftp.DialWithContext(ctx),
		ftp.DialWithDebugOutput(os.Stdout), // keep for debugging
		// *** Use default (EPSV enabled) ***
		// ftp.DialWithDisabledEPSV(true),
	}

	connStr := fc.ConnectionString() // will use the fixed host port (e.g., 2121)
	fmt.Printf("Attempting FTP connection to: %s (User: %s)\n", connStr, fc.User)

	c, err := ftp.Dial(connStr, opts...)
	if err != nil {
		fmt.Printf("FTP Dial Error to %s: %v\n", connStr, err)
		return nil, fmt.Errorf("failed to dial FTP server %s: %w", connStr, err)
	}
	fmt.Printf("FTP Dial successful to %s\n", connStr)

	fmt.Printf("Attempting FTP login with user: %s\n", fc.User)
	if err := c.Login(fc.User, fc.Password); err != nil {
		fmt.Printf("FTP Login Error for user %s: %v\n", fc.User, err)
		if quitErr := c.Quit(); quitErr != nil {
			fmt.Printf("Warning: error closing FTP connection: %v\n", quitErr)
		}
		return nil, fmt.Errorf("failed to login to FTP server with user %s: %w", fc.User, err)
	}
	fmt.Printf("FTP Login successful for user %s\n", fc.User)

	return c, nil
}

// createDirRecursive creates directories recursively relative to the current working directory.
func (fc *FTPTestContainer) createDirRecursive(c *ftp.ServerConn, remotePath string) error {
	parts := splitPath(remotePath)
	if len(parts) == 0 {
		return nil
	}

	// get current directory and setup return to it after the operation
	originalWD, err := fc.saveCurrentDirectory(c)
	if err == nil {
		defer fc.restoreWorkingDirectory(c, originalWD)
	}

	// process each directory in the path
	for _, part := range parts {
		if err := fc.ensureDirectoryExists(c, part); err != nil {
			return err
		}
	}

	fmt.Printf("Finished ensuring directory structure for: %s\n", remotePath)
	return nil
}

// saveCurrentDirectory gets the current working directory and handles any errors
func (fc *FTPTestContainer) saveCurrentDirectory(c *ftp.ServerConn) (string, error) {
	originalWD, err := c.CurrentDir()
	if err != nil {
		fmt.Printf("Warning: failed to get current directory: %v. Will not return to original WD.\n", err)
		return "", err
	}
	return originalWD, nil
}

// restoreWorkingDirectory attempts to return to the original working directory
func (fc *FTPTestContainer) restoreWorkingDirectory(c *ftp.ServerConn, originalWD string) {
	if originalWD == "" {
		return
	}

	fmt.Printf("Returning to original directory: %s\n", originalWD)
	if err := c.ChangeDir(originalWD); err != nil {
		// try to go to root first and then to the original directory
		_ = c.ChangeDir("/")
		if err2 := c.ChangeDir(originalWD); err2 != nil {
			fmt.Printf("Warning: failed to return to original directory '%s' (even from root): %v\n", originalWD, err2)
		}
	}
}

// ensureDirectoryExists checks if a directory exists, creates it if needed, and changes into it
func (fc *FTPTestContainer) ensureDirectoryExists(c *ftp.ServerConn, dirName string) error {
	fmt.Printf("Checking/Changing into directory segment: %s (relative to current)\n", dirName)

	// first try to change into the directory to see if it exists
	if err := c.ChangeDir(dirName); err == nil {
		fmt.Printf("Directory %s exists, changed into it.\n", dirName)
		return nil
	}

	// directory doesn't exist or can't be accessed, try to create it
	fmt.Printf("Directory %s does not exist or ChangeDir failed, attempting MakeDir...\n", dirName)
	if err := c.MakeDir(dirName); err != nil {
		return fc.handleMakeDirFailure(c, dirName, err)
	}

	// successfully created the directory, now change into it
	fmt.Printf("MakeDir %s succeeded. Changing into it...\n", dirName)
	if err := c.ChangeDir(dirName); err != nil {
		return fmt.Errorf("failed to change into newly created directory '%s': %w", dirName, err)
	}

	fmt.Printf("Successfully changed into newly created directory: %s\n", dirName)
	return nil
}

// handleMakeDirFailure handles the case where MakeDir fails
// (often because the directory already exists but ChangeDir initially failed for some reason)
func (fc *FTPTestContainer) handleMakeDirFailure(c *ftp.ServerConn, dirName string, makeDirErr error) error {
	fmt.Printf("MakeDir %s failed: %v. Checking again with ChangeDir...\n", dirName, makeDirErr)
	if err := c.ChangeDir(dirName); err != nil {
		return fmt.Errorf("failed to create directory '%s' (MakeDir error: %w) and "+
			"failed to change into it subsequently (ChangeDir error: %w)",
			dirName, makeDirErr, err)
	}

	fmt.Printf("ChangeDir %s succeeded after MakeDir failed. Assuming directory existed.\n", dirName)
	return nil
}

// ConnectionString returns the FTP connection string for this container
func (fc *FTPTestContainer) ConnectionString() string {
	return fmt.Sprintf("%s:%d", fc.Host, fc.Port.Int())
}

// GetIP returns the host IP address
func (fc *FTPTestContainer) GetIP() string { return fc.Host }

// GetPort returns the mapped port
func (fc *FTPTestContainer) GetPort() int { return fc.Port.Int() }

// GetUser returns the FTP username
func (fc *FTPTestContainer) GetUser() string { return fc.User }

// GetPassword returns the FTP password
func (fc *FTPTestContainer) GetPassword() string { return fc.Password }

// GetFile downloads a file from the FTP server
func (fc *FTPTestContainer) GetFile(ctx context.Context, remotePath, localPath string) error {
	c, err := fc.connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to FTP server for GetFile: %w", err)
	}
	defer func() {
		if quitErr := c.Quit(); quitErr != nil {
			fmt.Printf("Warning: error closing FTP connection: %v\n", quitErr)
		}
	}()
	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0o750); err != nil {
		return fmt.Errorf("failed to create local directory %s: %w", localDir, err)
	}
	// create file with secure permissions, validating path is within expected directory
	if !strings.HasPrefix(filepath.Clean(localPath), filepath.Clean(localDir)) {
		return fmt.Errorf("localPath %s attempts to escape from directory %s", localPath, localDir)
	}
	f, err := os.OpenFile(localPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localPath, err)
	}
	defer f.Close()
	r, err := c.Retr(filepath.ToSlash(remotePath))
	if err != nil {
		return fmt.Errorf("failed to retrieve remote file %s: %w", remotePath, err)
	}
	defer r.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("failed to copy file content from %s to %s: %w", remotePath, localPath, err)
	}
	return nil
}

// SaveFile uploads a file to the FTP server
func (fc *FTPTestContainer) SaveFile(ctx context.Context, localPath, remotePath string) error {
	c, err := fc.connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to FTP server for SaveFile: %w", err)
	}
	defer func() {
		if quitErr := c.Quit(); quitErr != nil {
			fmt.Printf("Warning: error closing FTP connection: %v\n", quitErr)
		}
	}()
	// validate the file path to prevent path traversal
	if !strings.HasPrefix(filepath.Clean(localPath), filepath.Clean(filepath.Dir(localPath))) {
		return fmt.Errorf("localPath %s attempts to escape from its directory", localPath)
	}
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file %s: %w", localPath, err)
	}
	defer f.Close()
	remoteDir := filepath.Dir(filepath.ToSlash(remotePath))
	if remoteDir != "." && remoteDir != "/" {
		if err := fc.createDirRecursive(c, remoteDir); err != nil {
			return fmt.Errorf("failed to create remote directory %s: %w", remoteDir, err)
		}
	}
	if err := c.Stor(filepath.ToSlash(remotePath), f); err != nil {
		return fmt.Errorf("failed to store file %s as %s: %w", localPath, remotePath, err)
	}
	return nil
}

// ListFiles lists files in a directory on the FTP server
func (fc *FTPTestContainer) ListFiles(ctx context.Context, remotePath string) ([]ftp.Entry, error) {
	c, err := fc.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to FTP server for ListFiles: %w", err)
	}
	defer func() {
		if quitErr := c.Quit(); quitErr != nil {
			fmt.Printf("Warning: error closing FTP connection: %v\n", quitErr)
		}
	}()
	cleanRemotePath := filepath.ToSlash(remotePath)
	if cleanRemotePath == "" || cleanRemotePath == "." {
		cleanRemotePath = "/"
	}
	ptrEntries, err := c.List(cleanRemotePath)
	if err != nil {
		return nil, fmt.Errorf("failed to list files in remote path '%s': %w", cleanRemotePath, err)
	}
	entries := make([]ftp.Entry, 0, len(ptrEntries))
	for _, entry := range ptrEntries {
		if entry != nil {
			entries = append(entries, *entry)
		}
	}
	return entries, nil
}

// DeleteFile deletes a file from the FTP server
func (fc *FTPTestContainer) DeleteFile(ctx context.Context, remotePath string) error {
	c, err := fc.connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to FTP server for DeleteFile: %w", err)
	}
	defer func() {
		if quitErr := c.Quit(); quitErr != nil {
			fmt.Printf("Warning: error closing FTP connection: %v\n", quitErr)
		}
	}()

	cleanRemotePath := filepath.ToSlash(remotePath)
	if err := c.Delete(cleanRemotePath); err != nil {
		return fmt.Errorf("failed to delete remote file %s: %w", cleanRemotePath, err)
	}
	return nil
}

// Close terminates the container
func (fc *FTPTestContainer) Close(ctx context.Context) error {
	if fc.Container != nil {
		containerID := fc.Container.GetContainerID()
		fmt.Printf("terminating FTP container %s...\n", containerID)
		err := fc.Container.Terminate(ctx)
		fc.Container = nil
		if err != nil {
			fmt.Printf("error terminating FTP container %s: %v\n", containerID, err)
			return err
		}
		fmt.Printf("FTP container %s terminated.\n", containerID)
	}
	return nil
}
func splitPath(path string) []string {
	cleanPath := filepath.ToSlash(path)
	cleanPath = strings.Trim(cleanPath, "/")
	if cleanPath == "" {
		return []string{}
	}
	return strings.Split(cleanPath, "/")
}

// logContainerError handles container startup errors with detailed logging
func (fc *FTPTestContainer) logContainerError(_ context.Context, t *testing.T, container testcontainers.Container, err error, imageName string) {
	logCtx, logCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer logCancel()

	fc.logContainerLogs(logCtx, t, container)
	require.NoError(t, err, "Failed to create or start FTP container %s", imageName)
}

// logContainerLogs attempts to fetch and log container logs
func (fc *FTPTestContainer) logContainerLogs(ctx context.Context, t *testing.T, container testcontainers.Container) {
	if container == nil {
		t.Logf("Container object was nil after GenericContainer failure.")
		return
	}

	logs, logErr := container.Logs(ctx)
	if logErr != nil {
		t.Logf("Could not retrieve container logs after startup failure: %v", logErr)
		return
	}

	logBytes, _ := io.ReadAll(logs)
	if closeErr := logs.Close(); closeErr != nil {
		t.Logf("warning: failed to close logs reader: %v", closeErr)
	}

	t.Logf("Container logs on startup failure:\n%s", string(logBytes))
}

package containers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// LocalstackTestContainer is a wrapper around a testcontainers.Container that provides an S3 endpoint
type LocalstackTestContainer struct {
	Container testcontainers.Container
	Endpoint  string
	counter   atomic.Int64
}

// NewLocalstackTestContainer creates a new Localstack test container and returns a LocalstackTestContainer instance
func NewLocalstackTestContainer(ctx context.Context, t *testing.T) *LocalstackTestContainer {
	req := testcontainers.ContainerRequest{
		Image:        "localstack/localstack:3.0.0",
		ExposedPorts: []string{"4566/tcp"},
		Env: map[string]string{
			"SERVICES":              "s3",
			"DEFAULT_REGION":        "us-east-1",
			"AWS_ACCESS_KEY_ID":     "test",
			"AWS_SECRET_ACCESS_KEY": "test",
		},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("4566/tcp"),
			wait.ForLog("Ready."),
		).WithDeadline(time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "4566")
	require.NoError(t, err)

	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())
	return &LocalstackTestContainer{
		Container: container,
		Endpoint:  endpoint,
	}
}

// MakeS3Connection creates a new S3 connection using the test container endpoint and returns the connection and a bucket name
func (lc *LocalstackTestContainer) MakeS3Connection(ctx context.Context, t *testing.T) (client *s3.Client, bucketName string) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(lc.Endpoint)
		o.UsePathStyle = true
	})

	bucketName = fmt.Sprintf("test-bucket-%d-%d", time.Now().UnixNano(), lc.counter.Add(1))
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucketName)})
	require.NoError(t, err)

	return client, bucketName
}

// createS3Client creates an S3 client connected to the test container
func (lc *LocalstackTestContainer) createS3Client(ctx context.Context) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(lc.Endpoint)
		o.UsePathStyle = true
	})

	return client, nil
}

// GetFile downloads a file from S3
func (lc *LocalstackTestContainer) GetFile(ctx context.Context, bucketName, objectKey, localPath string) error {
	client, err := lc.createS3Client(ctx)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0o750); err != nil {
		return fmt.Errorf("failed to create local directory %s: %w", localDir, err)
	}

	if !strings.HasPrefix(filepath.Clean(localPath), filepath.Clean(localDir)) {
		return fmt.Errorf("localPath %s attempts to escape from directory %s", localPath, localDir)
	}

	// get object from S3
	output, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return fmt.Errorf("failed to get object %s from bucket %s: %w", objectKey, bucketName, err)
	}
	defer output.Body.Close()

	// create local file
	file, err := os.OpenFile(localPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localPath, err)
	}
	defer file.Close()

	// copy object content to local file
	if _, err := io.Copy(file, output.Body); err != nil {
		return fmt.Errorf("failed to copy content from S3 object to local file: %w", err)
	}

	return nil
}

// SaveFile uploads a file to S3
func (lc *LocalstackTestContainer) SaveFile(ctx context.Context, localPath, bucketName, objectKey string) error {
	client, err := lc.createS3Client(ctx)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	if !strings.HasPrefix(filepath.Clean(localPath), filepath.Clean(filepath.Dir(localPath))) {
		return fmt.Errorf("localPath %s attempts to escape from its directory", localPath)
	}

	// read local file
	fileData, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to read local file %s: %w", localPath, err)
	}

	// upload to S3
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
		Body:   bytes.NewReader(fileData),
	})
	if err != nil {
		return fmt.Errorf("failed to upload file to S3: %w", err)
	}

	return nil
}

// ListFiles lists objects in an S3 bucket, optionally with a prefix
func (lc *LocalstackTestContainer) ListFiles(ctx context.Context, bucketName, prefix string) ([]types.Object, error) {
	client, err := lc.createS3Client(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// list objects
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}

	// add prefix if provided
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}

	output, err := client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects in bucket %s: %w", bucketName, err)
	}

	return output.Contents, nil
}

// DeleteFile deletes an object from S3
func (lc *LocalstackTestContainer) DeleteFile(ctx context.Context, bucketName, objectKey string) error {
	client, err := lc.createS3Client(ctx)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	// delete object
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return fmt.Errorf("failed to delete object %s from bucket %s: %w", objectKey, bucketName, err)
	}

	return nil
}

// Close terminates the container
func (lc *LocalstackTestContainer) Close(ctx context.Context) error {
	return lc.Container.Terminate(ctx)
}

package containers

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

// Close terminates the container
func (lc *LocalstackTestContainer) Close(ctx context.Context) error {
	return lc.Container.Terminate(ctx)
}

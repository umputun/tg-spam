package containers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoTestContainer wraps testcontainers.Container and provides MongoDB client
type MongoTestContainer struct {
	Container testcontainers.Container
	URI       string
	Client    *mongo.Client
	origURL   string
}

// NewMongoTestContainer creates a new MongoDB test container
func NewMongoTestContainer(ctx context.Context, t *testing.T, mongoVersion int) *MongoTestContainer {
	mc, err := NewMongoTestContainerE(ctx, mongoVersion)
	require.NoError(t, err)
	return mc
}

// NewMongoTestContainerE creates a new MongoDB test container.
// Returns error instead of using require.NoError, suitable for TestMain usage.
func NewMongoTestContainerE(ctx context.Context, mongoVersion int) (*MongoTestContainer, error) {
	origURL := os.Getenv("MONGO_TEST")
	req := testcontainers.ContainerRequest{
		Image:        fmt.Sprintf("mongo:%d", mongoVersion),
		ExposedPorts: []string{"27017/tcp"},
		WaitingFor:   wait.ForLog("Waiting for connections").WithStartupTimeout(time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create mongo container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "27017")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get mapped port: %w", err)
	}

	uri := fmt.Sprintf("mongodb://%s:%s", host, port.Port())
	if err = os.Setenv("MONGO_TEST", uri); err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to set MONGO_TEST env: %w", err)
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		if origURL != "" {
			_ = os.Setenv("MONGO_TEST", origURL)
		} else {
			_ = os.Unsetenv("MONGO_TEST")
		}
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to connect to mongo: %w", err)
	}

	return &MongoTestContainer{
		Container: container,
		URI:       uri,
		Client:    client,
		origURL:   origURL,
	}, nil
}

// Collection returns a new collection with unique name for tests
func (mc *MongoTestContainer) Collection(dbName string) *mongo.Collection {
	return mc.Client.Database(dbName).Collection(fmt.Sprintf("test_coll_%d", time.Now().UnixNano()))
}

// Close disconnects client, terminates container and restores original environment
func (mc *MongoTestContainer) Close(ctx context.Context) error {
	if err := mc.Client.Disconnect(ctx); err != nil {
		return fmt.Errorf("failed to disconnect mongo client: %w", err)
	}

	if mc.origURL != "" {
		if err := os.Setenv("MONGO_TEST", mc.origURL); err != nil {
			return fmt.Errorf("failed to restore MONGO_TEST env: %w", err)
		}
	} else {
		if err := os.Unsetenv("MONGO_TEST"); err != nil {
			return fmt.Errorf("failed to unset MONGO_TEST env: %w", err)
		}
	}

	return mc.Container.Terminate(ctx)
}

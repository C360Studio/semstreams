//go:build integration

package querymanager

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/processor/graph/datamanager"
	"github.com/c360/semstreams/processor/graph/indexmanager"
)

// testBuckets holds all KV buckets needed for integration tests
type testBuckets struct {
	entity    jetstream.KeyValue
	predicate jetstream.KeyValue
	incoming  jetstream.KeyValue
	alias     jetstream.KeyValue
	spatial   jetstream.KeyValue
	temporal  jetstream.KeyValue
}

// setupKVBuckets creates all KV buckets needed for integration testing
func setupKVBuckets(ctx context.Context, t *testing.T, client *natsclient.TestClient) testBuckets {
	entityBucket, err := client.CreateKVBucket(ctx, "ENTITY_STATES")
	require.NoError(t, err, "Failed to create ENTITY_STATES bucket")

	predicateBucket, err := client.CreateKVBucket(ctx, "PREDICATE_INDEX")
	require.NoError(t, err, "Failed to create PREDICATE_INDEX bucket")

	incomingBucket, err := client.CreateKVBucket(ctx, "INCOMING_INDEX")
	require.NoError(t, err, "Failed to create INCOMING_INDEX bucket")

	aliasBucket, err := client.CreateKVBucket(ctx, "ALIAS_INDEX")
	require.NoError(t, err, "Failed to create ALIAS_INDEX bucket")

	spatialBucket, err := client.CreateKVBucket(ctx, "SPATIAL_INDEX")
	require.NoError(t, err, "Failed to create SPATIAL_INDEX bucket")

	temporalBucket, err := client.CreateKVBucket(ctx, "TEMPORAL_INDEX")
	require.NoError(t, err, "Failed to create TEMPORAL_INDEX bucket")

	return testBuckets{
		entity:    entityBucket,
		predicate: predicateBucket,
		incoming:  incomingBucket,
		alias:     aliasBucket,
		spatial:   spatialBucket,
		temporal:  temporalBucket,
	}
}

// setupDataManager creates and starts the data manager
func setupDataManager(
	ctx context.Context,
	t *testing.T,
	entityBucket jetstream.KeyValue,
	wg *sync.WaitGroup,
) (*datamanager.Manager, chan error) {
	dataConfig := datamanager.DefaultConfig()
	dataDeps := datamanager.Dependencies{
		KVBucket:        entityBucket,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Logger:          slog.Default(),
		Config:          dataConfig,
	}
	dataManager, err := datamanager.NewDataManager(dataDeps)
	require.NoError(t, err, "Failed to create data manager")

	dataErrors := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		dataErrors <- dataManager.Run(ctx)
	}()

	return dataManager, dataErrors
}

// setupIndexManager creates and starts the index manager
func setupIndexManager(
	ctx context.Context,
	t *testing.T,
	buckets testBuckets,
	client *natsclient.Client,
	wg *sync.WaitGroup,
) (indexmanager.Indexer, chan error) {
	indexConfig := indexmanager.DefaultConfig()
	indexBuckets := map[string]jetstream.KeyValue{
		"ENTITY_STATES":   buckets.entity,
		"PREDICATE_INDEX": buckets.predicate,
		"INCOMING_INDEX":  buckets.incoming,
		"ALIAS_INDEX":     buckets.alias,
		"SPATIAL_INDEX":   buckets.spatial,
		"TEMPORAL_INDEX":  buckets.temporal,
	}
	indexManager, err := indexmanager.NewManager(indexConfig, indexBuckets, client, nil, nil)
	require.NoError(t, err, "Failed to create index manager")

	indexErrors := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		indexErrors <- indexManager.Run(ctx)
	}()

	return indexManager, indexErrors
}

// setupQueryManager creates the query manager (stateless, no Run method)
func setupQueryManager(
	t *testing.T,
	dataManager *datamanager.Manager,
	indexManager indexmanager.Indexer,
) Querier {
	queryConfig := Config{}
	queryConfig.SetDefaults()

	queryDeps := Deps{
		Config:       queryConfig,
		EntityReader: dataManager,
		IndexManager: indexManager,
		Registry:     nil,
		Logger:       nil,
	}
	queryManager, err := NewManager(queryDeps)
	require.NoError(t, err, "Failed to create query manager")

	return queryManager
}

// checkStartupErrors waits for initialization and checks for startup errors
func checkStartupErrors(t *testing.T, dataErrors, indexErrors chan error) {
	time.Sleep(50 * time.Millisecond)

	select {
	case err := <-dataErrors:
		if err != nil {
			t.Fatalf("DataManager failed to start: %v", err)
		}
	case err := <-indexErrors:
		if err != nil {
			t.Fatalf("IndexManager failed to start: %v", err)
		}
	default:
		// No errors yet - services are starting up
	}
}

// waitForShutdown waits for all services to shutdown cleanly
func waitForShutdown(t *testing.T, wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-time.After(5 * time.Second):
		t.Error("Services did not shutdown within 5 seconds")
	}
}

func TestQueryManager_IntegrationWithNATS(t *testing.T) {
	// Skip in short mode as this requires NATS
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create test NATS client with JetStream and KV
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx := context.Background()

	// Setup infrastructure
	buckets := setupKVBuckets(ctx, t, testClient)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Setup managers
	var wg sync.WaitGroup
	dataManager, dataErrors := setupDataManager(ctx, t, buckets.entity, &wg)
	indexManager, indexErrors := setupIndexManager(ctx, t, buckets, testClient.Client, &wg)
	queryManager := setupQueryManager(t, dataManager, indexManager)

	// Verify startup
	checkStartupErrors(t, dataErrors, indexErrors)

	t.Run("Basic Entity Query", func(t *testing.T) {
		// Create a test entity
		entity := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID:   "test.platform.domain.system.type.instance1",
				Type: "domain.type",
				Properties: map[string]any{
					"name":   "TestEntity",
					"status": "active",
				},
				Status: gtypes.StatusActive,
			},
			Version:   1,
			UpdatedAt: time.Now(),
		}

		// Store the entity
		created, err := dataManager.CreateEntity(ctx, entity)
		require.NoError(t, err, "Failed to create entity")
		assert.Equal(t, entity.Node.ID, created.Node.ID)

		// Wait for entity to be persisted (DataManager may buffer writes)
		time.Sleep(100 * time.Millisecond)

		// Query the entity
		result, err := queryManager.GetEntity(ctx, entity.Node.ID)
		require.NoError(t, err, "Failed to get entity")
		assert.NotNil(t, result, "Query result should not be nil")
		assert.Equal(t, entity.Node.ID, result.Node.ID, "Entity ID should match")
		assert.Equal(t, entity.Node.Properties, result.Node.Properties, "Properties should match")
	})

	t.Run("Query Non-existent Entity", func(t *testing.T) {
		result, err := queryManager.GetEntity(ctx, "non.existent.entity.id")
		assert.Error(t, err, "Should error when querying non-existent entity")
		assert.Nil(t, result, "Result should be nil for non-existent entity")
	})

	t.Run("Entity with Relationships", func(t *testing.T) {
		// Create source entity
		sourceEntity := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID:   "test.platform.domain.system.type.source2",
				Type: "source.type",
				Properties: map[string]any{
					"name": "SourceEntity",
				},
				Status: gtypes.StatusActive,
			},
			Version:   1,
			UpdatedAt: time.Now(),
		}

		// Create target entity
		targetEntity := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID:   "test.platform.domain.system.type.target2",
				Type: "target.type",
				Properties: map[string]any{
					"name": "TargetEntity",
				},
				Status: gtypes.StatusActive,
			},
			Version:   1,
			UpdatedAt: time.Now(),
		}

		// Store both entities
		_, err := dataManager.CreateEntity(ctx, sourceEntity)
		require.NoError(t, err, "Failed to create source entity")

		_, err = dataManager.CreateEntity(ctx, targetEntity)
		require.NoError(t, err, "Failed to create target entity")

		// Wait for entities to be persisted (DataManager may buffer writes)
		time.Sleep(100 * time.Millisecond)

		// Query both entities to verify they exist
		sourceResult, err := queryManager.GetEntity(ctx, sourceEntity.Node.ID)
		require.NoError(t, err, "Failed to get source entity")
		assert.Equal(t, sourceEntity.Node.ID, sourceResult.Node.ID)

		targetResult, err := queryManager.GetEntity(ctx, targetEntity.Node.ID)
		require.NoError(t, err, "Failed to get target entity")
		assert.Equal(t, targetEntity.Node.ID, targetResult.Node.ID)
	})

	// Shutdown and cleanup
	cancel()
	waitForShutdown(t, &wg)
}

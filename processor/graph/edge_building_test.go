package graph

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/processor/graph/indexmanager"
	"github.com/c360/semstreams/storage/objectstore"
)

// TestGraphablePayload implements the Graphable interface for testing
type TestGraphablePayload struct {
	ID         string                   `json:"entity_id"`
	Properties map[string]interface{}   `json:"properties"`
	TripleData []map[string]interface{} `json:"triples"`
}

func (t *TestGraphablePayload) EntityID() string {
	return t.ID
}

func (t *TestGraphablePayload) Triples() []message.Triple {
	var triples []message.Triple
	for _, triple := range t.TripleData {
		triples = append(triples, message.Triple{
			Subject:   triple["subject"].(string),
			Predicate: triple["predicate"].(string),
			Object:    triple["object"],
		})
	}
	return triples
}

func (t *TestGraphablePayload) Schema() message.Type {
	return message.Type{
		Domain:   "test",
		Category: "graphable",
		Version:  "v1",
	}
}

func (t *TestGraphablePayload) Validate() error {
	return nil
}

func (t *TestGraphablePayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		EntityID   string                   `json:"entity_id"`
		Properties map[string]interface{}   `json:"properties"`
		TripleData []map[string]interface{} `json:"triples"`
	}{
		EntityID:   t.ID,
		Properties: t.Properties,
		TripleData: t.TripleData,
	})
}

func (t *TestGraphablePayload) UnmarshalJSON(data []byte) error {
	var tmp struct {
		EntityID   string                   `json:"entity_id"`
		Properties map[string]interface{}   `json:"properties"`
		TripleData []map[string]interface{} `json:"triples"`
	}

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	t.ID = tmp.EntityID
	t.Properties = tmp.Properties
	t.TripleData = tmp.TripleData
	return nil
}

// edgeBuildingTestData holds test data for edge building tests
type edgeBuildingTestData struct {
	droneID     string
	batteryID   string
	autopilotID string
	payload     *TestGraphablePayload
}

// setupEdgeBuildingProcessor creates and starts a processor for edge building tests
func setupEdgeBuildingProcessor(ctx context.Context, t *testing.T) (*Processor, context.CancelFunc) {
	natsClient := getSharedNATSClient(t)

	config := DefaultConfig()
	if config.Indexer == nil {
		config.Indexer = &indexmanager.Config{}
		*config.Indexer = indexmanager.DefaultConfig()
	}
	config.Indexer.EventBuffer.Metrics = false

	deps := ProcessorDeps{
		Config:          config,
		NATSClient:      natsClient,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Logger:          slog.Default(),
	}

	processor, err := NewProcessor(deps)
	require.NoError(t, err)

	err = processor.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(ctx)

	startErr := make(chan error, 1)
	go func() {
		startErr <- processor.Start(ctx)
	}()

	err = processor.WaitForReady(2 * time.Second)
	require.NoError(t, err)

	// Setup cleanup
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-startErr:
			assert.NoError(t, err, "Start should complete without error")
		case <-time.After(5 * time.Second):
			t.Fatal("Start did not return after context cancel")
		}
	})

	return processor, cancel
}

// createEdgeBuildingTestData creates test data with unique entity IDs
func createEdgeBuildingTestData() edgeBuildingTestData {
	droneID := "c360.platform1.test.system1.drone.edgetest"
	batteryID := "c360.platform1.test.system1.battery.edgetest"
	autopilotID := "c360.platform1.test.system1.autopilot.edgetest"

	payload := &TestGraphablePayload{
		ID: droneID,
		Properties: map[string]interface{}{
			"system:battery_level": 85.5,
			"system:flight_armed":  true,
		},
		TripleData: []map[string]interface{}{
			{
				"subject":   droneID,
				"predicate": "system:battery_level",
				"object":    85.5,
			},
			{
				"subject":   droneID,
				"predicate": "system:flight_armed",
				"object":    true,
			},
			{
				"subject":   droneID,
				"predicate": "system:has_component",
				"object":    batteryID,
			},
			{
				"subject":   droneID,
				"predicate": "system:has_component",
				"object":    autopilotID,
			},
		},
	}

	return edgeBuildingTestData{
		droneID:     droneID,
		batteryID:   batteryID,
		autopilotID: autopilotID,
		payload:     payload,
	}
}

// publishGraphableMessage publishes a graphable message to NATS
func publishGraphableMessage(ctx context.Context, t *testing.T, processor *Processor, payload *TestGraphablePayload) {
	storageRef := &message.StorageReference{
		StorageInstance: "test-objectstore",
		Key:             "storage/test/msg-edges-001",
		ContentType:     "application/json",
		Size:            1024,
	}

	storedMsg := objectstore.NewStoredMessage(payload, storageRef, "test.edge_building")
	wrappedMsg := message.NewBaseMessage(
		storedMsg.Schema(),
		storedMsg,
		"test-objectstore",
	)

	messageData, err := json.Marshal(wrappedMsg)
	require.NoError(t, err)

	err = processor.natsClient.Publish(ctx, "storage.test.events", messageData)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)
}

// verifyEntityEdges verifies that edges were created correctly
func verifyEntityEdges(ctx context.Context, t *testing.T, processor *Processor, testData edgeBuildingTestData) *gtypes.EntityState {
	entity, err := processor.GetEntity(ctx, testData.droneID)
	require.NoError(t, err, "Should be able to retrieve entity after processing")
	require.NotNil(t, entity, "Entity should exist after processing")

	assert.Len(t, entity.Edges, 2, "Should have 2 edges from relationship triples")

	edgeTargets := make(map[string]bool)
	for _, edge := range entity.Edges {
		edgeTargets[edge.ToEntityID] = true
		assert.Equal(t, "system:has_component", edge.EdgeType, "Edge type should be derived from predicate")
		assert.Contains(t, edge.Properties, "predicate", "Edge should contain original predicate")
	}

	assert.True(t, edgeTargets[testData.batteryID], "Should have edge to battery")
	assert.True(t, edgeTargets[testData.autopilotID], "Should have edge to autopilot")

	return entity
}

// verifyEntityProperties verifies that properties were stored correctly
func verifyEntityProperties(t *testing.T, entity *gtypes.EntityState) {
	batteryLevel, found := gtypes.GetPropertyValue(entity, "system:battery_level")
	assert.True(t, found)
	assert.Equal(t, 85.5, batteryLevel)

	flightArmed, found := gtypes.GetPropertyValue(entity, "system:flight_armed")
	assert.True(t, found)
	assert.Equal(t, true, flightArmed)
}

// verifyIncomingIndex verifies that INCOMING_INDEX was updated correctly
func verifyIncomingIndex(ctx context.Context, t *testing.T, processor *Processor, testData edgeBuildingTestData) {
	incomingRelationships, err := processor.indexManager.GetIncomingRelationships(ctx, testData.batteryID)
	assert.NoError(t, err)
	assert.Len(t, incomingRelationships, 1, "Battery should have 1 incoming relationship")
	if len(incomingRelationships) > 0 {
		assert.Equal(t, testData.droneID, incomingRelationships[0].FromEntityID)
	}

	incomingRelationships, err = processor.indexManager.GetIncomingRelationships(ctx, testData.autopilotID)
	assert.NoError(t, err)
	assert.Len(t, incomingRelationships, 1, "Autopilot should have 1 incoming relationship")
	if len(incomingRelationships) > 0 {
		assert.Equal(t, testData.droneID, incomingRelationships[0].FromEntityID)
	}
}

// TestEdgeBuildingFromMessages verifies that edges are properly built from relationship triples
// IMPORTANT: This test uses entity IDs with "edgetest" suffix to avoid conflicts with other tests.
// Each test MUST use unique entity IDs to ensure test isolation and prevent intermittent failures.
func TestEdgeBuildingFromMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	processor, _ := setupEdgeBuildingProcessor(ctx, t)

	// Create test data
	testData := createEdgeBuildingTestData()

	// Publish message
	publishGraphableMessage(ctx, t, processor, testData.payload)

	// Verify edges
	entity := verifyEntityEdges(ctx, t, processor, testData)

	// Verify properties
	verifyEntityProperties(t, entity)

	// Verify incoming index
	verifyIncomingIndex(ctx, t, processor, testData)
}

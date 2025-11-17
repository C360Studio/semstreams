package flowstore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c360/semstreams/natsclient"
)

type StoreIntegrationSuite struct {
	suite.Suite
	testClient *natsclient.TestClient
	natsClient *natsclient.Client
	store      *Store
	ctx        context.Context
	cancel     context.CancelFunc
}

func (s *StoreIntegrationSuite) SetupSuite() {
	s.testClient = natsclient.NewTestClient(s.T(),
		natsclient.WithJetStream(),
		natsclient.WithKV())
	s.natsClient = s.testClient.Client
}

func (s *StoreIntegrationSuite) SetupTest() {
	// Create store
	var err error
	s.store, err = NewStore(s.natsClient)
	s.Require().NoError(err)

	// Create context for test
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 30*time.Second)
}

func (s *StoreIntegrationSuite) TearDownTest() {
	s.cancel()
}

// TestCreateAndGet tests basic CRUD: create a flow, then retrieve it
func (s *StoreIntegrationSuite) TestCreateAndGet() {
	flow := &Flow{
		ID:           "test-flow-1",
		Name:         "Test Flow",
		Description:  "A test flow for integration testing",
		RuntimeState: StateNotDeployed,
		Nodes: []FlowNode{
			{
				ID:       "node-1",
				Type:     "udp",
				Name:     "UDP Input",
				Position: Position{X: 100, Y: 100},
				Config:   map[string]any{"port": 5000},
			},
		},
		Connections: []FlowConnection{},
	}

	// Create flow
	err := s.store.Create(s.ctx, flow)
	s.Require().NoError(err)

	// Verify timestamps were set
	s.False(flow.CreatedAt.IsZero(), "CreatedAt should be set")
	s.False(flow.UpdatedAt.IsZero(), "UpdatedAt should be set")
	s.Equal(int64(1), flow.Version, "Version should be 1 for new flow")

	// Retrieve flow
	retrieved, err := s.store.Get(s.ctx, "test-flow-1")
	s.Require().NoError(err)
	s.NotNil(retrieved)

	// Verify fields
	s.Equal("test-flow-1", retrieved.ID)
	s.Equal("Test Flow", retrieved.Name)
	s.Equal("A test flow for integration testing", retrieved.Description)
	s.Equal(StateNotDeployed, retrieved.RuntimeState)
	s.Equal(int64(1), retrieved.Version)
	s.Len(retrieved.Nodes, 1)
	s.Equal("node-1", retrieved.Nodes[0].ID)
	s.Equal("udp", retrieved.Nodes[0].Type)
}

// TestCreateDuplicate tests that creating a duplicate flow returns an error
func (s *StoreIntegrationSuite) TestCreateDuplicate() {
	flow := &Flow{
		ID:           "duplicate-flow",
		Name:         "Duplicate",
		RuntimeState: StateNotDeployed,
		Nodes:        []FlowNode{},
		Connections:  []FlowConnection{},
	}

	// First create should succeed
	err := s.store.Create(s.ctx, flow)
	s.Require().NoError(err)

	// Second create with same ID should fail
	duplicate := &Flow{
		ID:           "duplicate-flow",
		Name:         "Different Name",
		RuntimeState: StateNotDeployed,
		Nodes:        []FlowNode{},
		Connections:  []FlowConnection{},
	}

	err = s.store.Create(s.ctx, duplicate)
	s.Error(err, "Creating duplicate flow should error")
}

// TestUpdate tests updating an existing flow
func (s *StoreIntegrationSuite) TestUpdate() {
	// Create initial flow
	flow := &Flow{
		ID:           "update-flow",
		Name:         "Original Name",
		Description:  "Original description",
		RuntimeState: StateNotDeployed,
		Nodes:        []FlowNode{},
		Connections:  []FlowConnection{},
	}

	err := s.store.Create(s.ctx, flow)
	s.Require().NoError(err)
	s.Equal(int64(1), flow.Version)

	// Update the flow
	flow.Name = "Updated Name"
	flow.Description = "Updated description"
	flow.RuntimeState = StateDeployedStopped

	err = s.store.Update(s.ctx, flow)
	s.Require().NoError(err)
	s.Equal(int64(2), flow.Version, "Version should increment after update")

	// Retrieve and verify
	retrieved, err := s.store.Get(s.ctx, "update-flow")
	s.Require().NoError(err)
	s.Equal("Updated Name", retrieved.Name)
	s.Equal("Updated description", retrieved.Description)
	s.Equal(StateDeployedStopped, retrieved.RuntimeState)
	s.Equal(int64(2), retrieved.Version)
}

// TestOptimisticConcurrency tests version-based concurrency control
func (s *StoreIntegrationSuite) TestOptimisticConcurrency() {
	// Create initial flow
	flow := &Flow{
		ID:           "concurrent-flow",
		Name:         "Concurrent Test",
		RuntimeState: StateNotDeployed,
		Nodes:        []FlowNode{},
		Connections:  []FlowConnection{},
	}

	err := s.store.Create(s.ctx, flow)
	s.Require().NoError(err)
	s.Equal(int64(1), flow.Version)

	// Simulate concurrent update: someone else updates first
	flow.Name = "Updated by another user"
	err = s.store.Update(s.ctx, flow)
	s.Require().NoError(err)
	s.Equal(int64(2), flow.Version)

	// Try to update with stale version (still version 1)
	staleFlow := &Flow{
		ID:           "concurrent-flow",
		Name:         "Stale update",
		RuntimeState: StateRunning,
		Version:      1, // Stale version
		Nodes:        []FlowNode{},
		Connections:  []FlowConnection{},
	}

	err = s.store.Update(s.ctx, staleFlow)
	s.Error(err, "Update with stale version should fail")
	s.Contains(err.Error(), "conflict", "Error should indicate version conflict")
}

// TestDelete tests deleting a flow
func (s *StoreIntegrationSuite) TestDelete() {
	// Create flow
	flow := &Flow{
		ID:           "delete-flow",
		Name:         "To Be Deleted",
		RuntimeState: StateNotDeployed,
		Nodes:        []FlowNode{},
		Connections:  []FlowConnection{},
	}

	err := s.store.Create(s.ctx, flow)
	s.Require().NoError(err)

	// Verify it exists
	retrieved, err := s.store.Get(s.ctx, "delete-flow")
	s.Require().NoError(err)
	s.NotNil(retrieved)

	// Delete it
	err = s.store.Delete(s.ctx, "delete-flow")
	s.Require().NoError(err)

	// Verify it's gone
	_, err = s.store.Get(s.ctx, "delete-flow")
	s.Error(err, "Getting deleted flow should error")
}

// TestList tests listing all flows
func (s *StoreIntegrationSuite) TestList() {
	// Create multiple flows
	flows := []*Flow{
		{
			ID:           "list-flow-1",
			Name:         "Flow 1",
			RuntimeState: StateNotDeployed,
			Nodes:        []FlowNode{},
			Connections:  []FlowConnection{},
		},
		{
			ID:           "list-flow-2",
			Name:         "Flow 2",
			RuntimeState: StateRunning,
			Nodes:        []FlowNode{},
			Connections:  []FlowConnection{},
		},
		{
			ID:           "list-flow-3",
			Name:         "Flow 3",
			RuntimeState: StateDeployedStopped,
			Nodes:        []FlowNode{},
			Connections:  []FlowConnection{},
		},
	}

	for _, flow := range flows {
		err := s.store.Create(s.ctx, flow)
		s.Require().NoError(err)
	}

	// List all flows
	allFlows, err := s.store.List(s.ctx)
	s.Require().NoError(err)
	s.GreaterOrEqual(len(allFlows), 3, "Should have at least 3 flows")

	// Verify our flows are in the list
	flowIDs := make(map[string]bool)
	for _, f := range allFlows {
		flowIDs[f.ID] = true
	}

	for _, flow := range flows {
		s.True(flowIDs[flow.ID], "Flow %s should be in list", flow.ID)
	}
}

// TestComplexFlow tests creating and retrieving a flow with connections
func (s *StoreIntegrationSuite) TestComplexFlow() {
	flow := &Flow{
		ID:           "complex-flow",
		Name:         "Complex Flow with Connections",
		Description:  "Tests node and connection persistence",
		RuntimeState: StateNotDeployed,
		Nodes: []FlowNode{
			{
				ID:       "node-1",
				Type:     "udp",
				Name:     "UDP Input",
				Position: Position{X: 100, Y: 100},
				Config:   map[string]any{"port": 5000, "protocol": "mavlink"},
			},
			{
				ID:       "node-2",
				Type:     "graph-processor",
				Name:     "Graph Processor",
				Position: Position{X: 300, Y: 100},
				Config:   map[string]any{"enabled": true},
			},
			{
				ID:       "node-3",
				Type:     "websocket",
				Name:     "WebSocket Output",
				Position: Position{X: 500, Y: 100},
				Config:   map[string]any{"port": 8080},
			},
		},
		Connections: []FlowConnection{
			{
				ID:           "conn-1",
				SourceNodeID: "node-1",
				SourcePort:   "output",
				TargetNodeID: "node-2",
				TargetPort:   "input",
			},
			{
				ID:           "conn-2",
				SourceNodeID: "node-2",
				SourcePort:   "output",
				TargetNodeID: "node-3",
				TargetPort:   "input",
			},
		},
	}

	// Create flow
	err := s.store.Create(s.ctx, flow)
	s.Require().NoError(err)

	// Retrieve and verify structure
	retrieved, err := s.store.Get(s.ctx, "complex-flow")
	s.Require().NoError(err)

	// Verify nodes
	s.Len(retrieved.Nodes, 3)
	s.Equal("UDP Input", retrieved.Nodes[0].Name)
	s.Equal("Graph Processor", retrieved.Nodes[1].Name)
	s.Equal("WebSocket Output", retrieved.Nodes[2].Name)

	// Verify node positions
	s.Equal(float64(100), retrieved.Nodes[0].Position.X)
	s.Equal(float64(300), retrieved.Nodes[1].Position.X)

	// Verify connections
	s.Len(retrieved.Connections, 2)
	s.Equal("node-1", retrieved.Connections[0].SourceNodeID)
	s.Equal("node-2", retrieved.Connections[0].TargetNodeID)
	s.Equal("node-2", retrieved.Connections[1].SourceNodeID)
	s.Equal("node-3", retrieved.Connections[1].TargetNodeID)
}

func TestStoreIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(StoreIntegrationSuite))
}

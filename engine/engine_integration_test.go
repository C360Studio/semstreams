//go:build integration

package flowengine_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/config"
	flowengine "github.com/c360studio/semstreams/engine"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

type EngineIntegrationSuite struct {
	suite.Suite
	testClient        *natsclient.TestClient
	natsClient        *natsclient.Client
	configMgr         *config.Manager
	flowStore         *flowstore.Manager
	componentRegistry *component.Registry
	engine            *flowengine.Engine
	ctx               context.Context
	cancel            context.CancelFunc
}

func (s *EngineIntegrationSuite) SetupSuite() {
	s.testClient = natsclient.NewTestClient(s.T(),
		natsclient.WithJetStream(),
		natsclient.WithKV())
	s.natsClient = s.testClient.Client
}

func (s *EngineIntegrationSuite) SetupTest() {
	var err error

	// Create component registry and register ONLY core SemStreams components
	s.componentRegistry = component.NewRegistry()
	err = componentregistry.Register(s.componentRegistry)
	s.Require().NoError(err, "Should register core SemStreams components")

	// Create base config
	baseConfig := &config.Config{
		Version: "1.0.0",
		Platform: config.PlatformConfig{
			Org:         "test",
			ID:          "test-platform",
			InstanceID:  "test-001",
			Environment: "test",
		},
		Components: make(config.ComponentConfigs),
	}

	// Create config manager
	s.configMgr, err = config.NewConfigManager(baseConfig, s.natsClient, nil)
	s.Require().NoError(err)

	// Create context for test
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 30*time.Second)

	// Start config manager
	err = s.configMgr.Start(s.ctx)
	s.Require().NoError(err)

	// Create flow store
	s.flowStore, err = flowstore.NewManager(s.natsClient)
	s.Require().NoError(err)

	// Create engine (without metrics for testing)
	s.engine = flowengine.NewEngine(s.configMgr, s.flowStore, s.componentRegistry, s.natsClient, slog.Default(), nil)
}

func (s *EngineIntegrationSuite) TearDownTest() {
	// Stop config manager first
	s.configMgr.Stop(5 * time.Second)
	s.cancel()

	// Delete the entire KV bucket to prevent test pollution
	// PushToKV only puts keys - it doesn't delete keys removed from memory
	// Next test's NewConfigManager() will create a fresh empty bucket
	ctx := context.Background()
	if err := s.natsClient.DeleteKeyValueBucket(ctx, "semstreams_config"); err != nil {
		// Log but don't fail - bucket might not exist
		s.T().Logf("Warning: failed to delete KV bucket: %v", err)
	}
}

// TestDeployFlow tests deploying a flow with core components to component configs
func (s *EngineIntegrationSuite) TestDeployFlow() {
	// Create a simple flow with UDP → Filter pipeline
	// UDP input has network port (not NATS) so it doesn't require incoming connection
	flow := &flowstore.Flow{
		ID:           "deploy-test-flow",
		Name:         "Deploy Test Flow",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes: []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "udp-1",
				Position:  flowstore.Position{X: 100, Y: 100},
				Config:    map[string]any{"port": 5000},
			},
		},
		Connections: []flowstore.FlowConnection{},
	}

	err := s.flowStore.Create(s.ctx, flow)
	s.Require().NoError(err)

	// Deploy the flow
	err = s.engine.Deploy(s.ctx, "deploy-test-flow")
	s.Require().NoError(err)

	// Verify flow state changed
	deployed, err := s.flowStore.Get(s.ctx, "deploy-test-flow")
	s.Require().NoError(err)
	s.Equal(flowstore.StateDeployedStopped, deployed.RuntimeState, "Flow should be in deployed_stopped state")

	// Verify component config was created
	cfg := s.configMgr.GetConfig()
	currentConfig := cfg.Get()

	s.NotNil(currentConfig.Components, "Components should be created")
	s.Contains(currentConfig.Components, "udp-1", "UDP component should exist")

	// Verify component config has correct properties
	udpConfig := currentConfig.Components["udp-1"]
	s.Equal("input", string(udpConfig.Type), "UDP is an input component")
	s.Equal("udp", udpConfig.Name, "Name should be the factory name")
	s.True(udpConfig.Enabled, "Component should be enabled by default on deploy")
}

// TestDeployNonExistentFlow tests that deploying a non-existent flow fails
func (s *EngineIntegrationSuite) TestDeployNonExistentFlow() {
	err := s.engine.Deploy(s.ctx, "non-existent-flow")
	s.Error(err, "Deploying non-existent flow should error")
	s.True(errs.IsTransient(err), "Error should be transient (flow not found)")
}

// TestStartFlow tests starting a deployed flow
func (s *EngineIntegrationSuite) TestStartFlow() {
	// Create and deploy a flow with UDP
	flow := &flowstore.Flow{
		ID:           "start-test-flow",
		Name:         "Start Test Flow",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes: []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "udp-start-1",
				Position:  flowstore.Position{X: 100, Y: 100},
				Config:    map[string]any{"port": 5001},
			},
		},
		Connections: []flowstore.FlowConnection{},
	}

	err := s.flowStore.Create(s.ctx, flow)
	s.Require().NoError(err)

	err = s.engine.Deploy(s.ctx, "start-test-flow")
	s.Require().NoError(err)

	// Start the flow
	err = s.engine.Start(s.ctx, "start-test-flow")
	s.Require().NoError(err)

	// Verify flow state changed
	running, err := s.flowStore.Get(s.ctx, "start-test-flow")
	s.Require().NoError(err)
	s.Equal(flowstore.StateRunning, running.RuntimeState, "Flow should be running")

	// Verify components are enabled
	cfg := s.configMgr.GetConfig()
	currentConfig := cfg.Get()
	udpConfig := currentConfig.Components["udp-start-1"]
	s.True(udpConfig.Enabled, "Component should be enabled after start")
}

// TestStartNotDeployedFlow tests that starting a non-deployed flow fails
func (s *EngineIntegrationSuite) TestStartNotDeployedFlow() {
	// Create a flow but don't deploy it
	flow := &flowstore.Flow{
		ID:           "not-deployed-flow",
		Name:         "Not Deployed",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes:        []flowstore.FlowNode{},
		Connections:  []flowstore.FlowConnection{},
	}

	err := s.flowStore.Create(s.ctx, flow)
	s.Require().NoError(err)

	// Try to start it
	err = s.engine.Start(s.ctx, "not-deployed-flow")
	s.Error(err, "Starting non-deployed flow should fail")
	s.True(errs.IsInvalid(err), "Error should be invalid (wrong state)")
}

// TestStopFlow tests stopping a running flow
func (s *EngineIntegrationSuite) TestStopFlow() {
	// Create, deploy, and start a flow with UDP
	flow := &flowstore.Flow{
		ID:           "stop-test-flow",
		Name:         "Stop Test Flow",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes: []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "udp-stop-1",
				Position:  flowstore.Position{X: 100, Y: 100},
				Config:    map[string]any{"port": 5002},
			},
		},
		Connections: []flowstore.FlowConnection{},
	}

	err := s.flowStore.Create(s.ctx, flow)
	s.Require().NoError(err)

	err = s.engine.Deploy(s.ctx, "stop-test-flow")
	s.Require().NoError(err)

	err = s.engine.Start(s.ctx, "stop-test-flow")
	s.Require().NoError(err)

	// Stop the flow
	err = s.engine.Stop(s.ctx, "stop-test-flow")
	s.Require().NoError(err)

	// Verify flow state changed
	stopped, err := s.flowStore.Get(s.ctx, "stop-test-flow")
	s.Require().NoError(err)
	s.Equal(flowstore.StateDeployedStopped, stopped.RuntimeState, "Flow should be deployed_stopped")

	// Verify components are disabled. engine.Stop() returns once the flow's
	// RuntimeState is persisted (asserted above), but disabling the component
	// in the config manager propagates asynchronously — reading GetConfig()
	// immediately races that propagation and flaked intermittently under CI
	// load. Poll for the end-state instead; a real "never disabled" bug still
	// fails (the poll times out) rather than being masked.
	s.Eventually(func() bool {
		currentConfig := s.configMgr.GetConfig().Get()
		return !currentConfig.Components["udp-stop-1"].Enabled
	}, 5*time.Second, 20*time.Millisecond, "Component should be disabled after stop")
}

// TestStopNotRunningFlow tests that stopping a non-running flow fails
func (s *EngineIntegrationSuite) TestStopNotRunningFlow() {
	// Create and deploy but don't start
	flow := &flowstore.Flow{
		ID:           "deployed-not-running",
		Name:         "Deployed Not Running",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes: []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "udp-minimal",
				Position:  flowstore.Position{X: 100, Y: 100},
				Config:    map[string]any{"port": 5003},
			},
		},
		Connections: []flowstore.FlowConnection{},
	}

	err := s.flowStore.Create(s.ctx, flow)
	s.Require().NoError(err)

	err = s.engine.Deploy(s.ctx, "deployed-not-running")
	s.Require().NoError(err)

	// Try to stop it
	err = s.engine.Stop(s.ctx, "deployed-not-running")
	s.Error(err, "Stopping non-running flow should fail")
	s.True(errs.IsInvalid(err), "Error should be invalid (wrong state)")
}

// TestUndeployFlow tests undeploying a stopped flow
func (s *EngineIntegrationSuite) TestUndeployFlow() {
	// Create and deploy a flow with UDP
	flow := &flowstore.Flow{
		ID:           "undeploy-test-flow",
		Name:         "Undeploy Test Flow",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes: []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "udp-undeploy-1",
				Position:  flowstore.Position{X: 100, Y: 100},
				Config:    map[string]any{"port": 5004},
			},
		},
		Connections: []flowstore.FlowConnection{},
	}

	err := s.flowStore.Create(s.ctx, flow)
	s.Require().NoError(err)

	err = s.engine.Deploy(s.ctx, "undeploy-test-flow")
	s.Require().NoError(err)

	// Undeploy the flow
	err = s.engine.Undeploy(s.ctx, "undeploy-test-flow")
	s.Require().NoError(err)

	// Verify flow state changed
	undeployed, err := s.flowStore.Get(s.ctx, "undeploy-test-flow")
	s.Require().NoError(err)
	s.Equal(flowstore.StateNotDeployed, undeployed.RuntimeState, "Flow should be not_deployed")

	// Verify component configs were removed
	cfg := s.configMgr.GetConfig()
	currentConfig := cfg.Get()
	s.NotContains(currentConfig.Components, "udp-undeploy-1", "Component should be removed")
}

// TestUndeployRunningFlow tests that undeploying a running flow fails
func (s *EngineIntegrationSuite) TestUndeployRunningFlow() {
	// Create, deploy, and start a flow
	flow := &flowstore.Flow{
		ID:           "running-undeploy",
		Name:         "Running Undeploy",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes: []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "udp-running-1",
				Position:  flowstore.Position{X: 100, Y: 100},
				Config:    map[string]any{"port": 5005},
			},
		},
		Connections: []flowstore.FlowConnection{},
	}

	err := s.flowStore.Create(s.ctx, flow)
	s.Require().NoError(err)

	err = s.engine.Deploy(s.ctx, "running-undeploy")
	s.Require().NoError(err)

	err = s.engine.Start(s.ctx, "running-undeploy")
	s.Require().NoError(err)

	// Try to undeploy while running
	err = s.engine.Undeploy(s.ctx, "running-undeploy")
	s.Error(err, "Undeploying running flow should fail")
	s.True(errs.IsInvalid(err), "Error should be invalid (must stop first)")
}

// TestFullLifecycle tests the complete Deploy → Start → Stop → Undeploy workflow with core components
func (s *EngineIntegrationSuite) TestFullLifecycle() {
	// Create a complete pipeline: UDP → WebSocket (simple 2-component flow)
	// UDP input has network port (satisfied), WebSocket output consumes UDP's NATS output
	flow := &flowstore.Flow{
		ID:           "lifecycle-flow",
		Name:         "Full Lifecycle Test",
		RuntimeState: flowstore.StateNotDeployed,
		Nodes: []flowstore.FlowNode{
			{
				ID:        "node-1",
				Component: "udp",
				Type:      types.ComponentTypeInput,
				Name:      "udp-lifecycle-1",
				Position:  flowstore.Position{X: 100, Y: 100},
				Config:    map[string]any{"port": 5006},
			},
			{
				ID:        "node-2",
				Component: "websocket",
				Type:      types.ComponentTypeOutput,
				Name:      "ws-lifecycle-1",
				Position:  flowstore.Position{X: 300, Y: 100},
				Config:    map[string]any{"port": 8080},
			},
		},
		Connections: []flowstore.FlowConnection{
			{
				ID:           "conn-1",
				SourceNodeID: "node-1",
				SourcePort:   "output",
				TargetNodeID: "node-2",
				TargetPort:   "input",
			},
		},
	}

	err := s.flowStore.Create(s.ctx, flow)
	s.Require().NoError(err)

	// Step 1: Deploy
	err = s.engine.Deploy(s.ctx, "lifecycle-flow")
	s.Require().NoError(err)

	deployed, err := s.flowStore.Get(s.ctx, "lifecycle-flow")
	s.Require().NoError(err)
	s.Equal(flowstore.StateDeployedStopped, deployed.RuntimeState)

	// Step 2: Start
	err = s.engine.Start(s.ctx, "lifecycle-flow")
	s.Require().NoError(err)

	running, err := s.flowStore.Get(s.ctx, "lifecycle-flow")
	s.Require().NoError(err)
	s.Equal(flowstore.StateRunning, running.RuntimeState)

	// Step 3: Stop
	err = s.engine.Stop(s.ctx, "lifecycle-flow")
	s.Require().NoError(err)

	stopped, err := s.flowStore.Get(s.ctx, "lifecycle-flow")
	s.Require().NoError(err)
	s.Equal(flowstore.StateDeployedStopped, stopped.RuntimeState)

	// Step 4: Undeploy
	err = s.engine.Undeploy(s.ctx, "lifecycle-flow")
	s.Require().NoError(err)

	undeployed, err := s.flowStore.Get(s.ctx, "lifecycle-flow")
	s.Require().NoError(err)
	s.Equal(flowstore.StateNotDeployed, undeployed.RuntimeState)

	// Verify all components removed. This is a one-shot assertion —
	// no Eventually polling — because the Manager's engine-write
	// watermark (engineHighWaterRev in config/manager.go) skips
	// watcher events for revisions the engine has already applied
	// to memory synchronously. Before that watermark existed, a
	// queued Stop PUT (Enabled=false) could land in the watcher
	// AFTER Undeploy's synchronous DELETE, transiently re-inserting
	// the component into memory.
	cfg := s.configMgr.GetConfig()
	currentConfig := cfg.Get()
	s.NotContains(currentConfig.Components, "udp-lifecycle-1")
	s.NotContains(currentConfig.Components, "ws-lifecycle-1")
}

func TestEngineIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	suite.Run(t, new(EngineIntegrationSuite))
}

//go:build integration

package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	flowengine "github.com/c360studio/semstreams/engine"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

type configuredBootComponent struct {
	baseDiscoverable
	raw string
}

type bootOnlyProcess struct {
	client  *natsclient.Client
	config  *config.Manager
	flows   *flowstore.Manager
	manager *ComponentManager
	engine  *flowengine.Engine
}

func TestFlowDesiredActivationAppliesAfterTransportCloseAndRestart(t *testing.T) {
	testNATS := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithFileStorage())
	ctx := t.Context()

	first := startBootOnlyProcess(t, ctx, testNATS.URL)
	flow := &flowstore.Flow{
		ID: "restart-known-answer", Name: "restart-known-answer", DesiredState: flowstore.DesiredAbsent,
		Nodes: []flowstore.FlowNode{{
			ID: "worker", Name: "worker", Component: "worker-factory", Type: types.ComponentTypeProcessor,
			Config: map[string]any{"value": "graceful"},
		}},
		Connections: []flowstore.FlowConnection{},
	}
	if err := first.flows.Create(ctx, flow); err != nil {
		t.Fatal(err)
	}
	if err := first.engine.Deploy(ctx, flow.ID); err != nil {
		t.Fatal(err)
	}
	if err := first.engine.Start(ctx, flow.ID); err != nil {
		t.Fatal(err)
	}
	if _, exists := first.manager.components["worker"]; exists {
		t.Fatal("new desired flow activated before restart")
	}
	stopBootOnlyProcess(t, ctx, first)

	second := startBootOnlyProcess(t, ctx, testNATS.URL)
	assertBootComponentConfig(t, second.manager, `{"value":"graceful"}`)
	stored, err := second.flows.Get(ctx, flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Nodes[0].Config = map[string]any{"value": "dirty"}
	if err := second.flows.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if err := second.engine.Stop(ctx, flow.ID); err != nil {
		t.Fatal(err)
	}
	if err := second.engine.Deploy(ctx, flow.ID); err != nil {
		t.Fatal(err)
	}
	if err := second.engine.Start(ctx, flow.ID); err != nil {
		t.Fatal(err)
	}
	assertBootComponentConfig(t, second.manager, `{"value":"graceful"}`)

	// Abruptly close only this simulated process transport. Do not call any
	// component/config Stop hook; the durable desired write must be sufficient.
	second.client.GetConnection().Close()

	third := startBootOnlyProcess(t, ctx, testNATS.URL)
	assertBootComponentConfig(t, third.manager, `{"value":"dirty"}`)
	stopBootOnlyProcess(t, ctx, third)
}

func startBootOnlyProcess(t *testing.T, ctx context.Context, url string) *bootOnlyProcess {
	t.Helper()
	client, err := natsclient.NewClient(url, natsclient.WithHealthInterval(0), natsclient.WithMaxReconnects(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	desired, err := config.NewConfigManager(ctx, &config.Config{
		Version: "1.0.0",
		Platform: config.PlatformConfig{
			Org: "test", ID: "semstreams", InstanceID: "boot-only-restart", Environment: "test",
		},
		Components: config.ComponentConfigs{},
	}, client, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := desired.Start(ctx); err != nil {
		t.Fatal(err)
	}
	flows, err := flowstore.NewManager(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	registry := component.NewRegistry()
	if err := registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "worker-factory", Type: "processor",
		Factory: func(raw json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			return &configuredBootComponent{
				baseDiscoverable: baseDiscoverable{name: "worker"}, raw: string(raw),
			}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	bootFlows, err := flows.List(ctx)
	if err != nil && !strings.Contains(err.Error(), "no keys found") {
		t.Fatal(err)
	}
	selection, err := flowstore.SelectBoot(desired.GetConfig().Get(), bootFlows)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewComponentManager(nil, &Dependencies{
		NATSClient: client, Manager: desired, ComponentRegistry: registry, BootSelection: selection,
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := service.(*ComponentManager)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return &bootOnlyProcess{
		client: client, config: desired, flows: flows, manager: manager,
		engine: flowengine.NewEngine(desired, flows, registry, client, logger, nil),
	}
}

func assertBootComponentConfig(t *testing.T, manager *ComponentManager, want string) {
	t.Helper()
	component, ok := manager.components["worker"].Component.(*configuredBootComponent)
	if !ok {
		t.Fatalf("boot worker type = %T", manager.components["worker"].Component)
	}
	if component.raw != want {
		t.Fatalf("boot worker config = %s, want %s", component.raw, want)
	}
}

func stopBootOnlyProcess(t *testing.T, ctx context.Context, process *bootOnlyProcess) {
	t.Helper()
	if err := process.manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := process.config.Stop(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := process.client.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
}

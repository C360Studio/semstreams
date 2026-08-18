//go:build integration

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	flowengine "github.com/c360studio/semstreams/engine"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

type failOncePublisher struct {
	componentConfigPublisher
	failName string
	failed   bool
}

func (p *failOncePublisher) PutComponentToKV(ctx context.Context, name string, cfg types.ComponentConfig) error {
	if name == p.failName && !p.failed {
		p.failed = true
		return errors.New("injected persistence failure")
	}
	return p.componentConfigPublisher.PutComponentToKV(ctx, name, cfg)
}

func newFlowPublishFixture(t *testing.T, boot config.ComponentConfigs) (*FlowService, *config.Manager) {
	t.Helper()
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	manager, err := config.NewConfigManager(t.Context(), &config.Config{
		Version:    "1.0.0",
		Platform:   config.PlatformConfig{Org: "test", ID: t.Name(), Type: "test"},
		Components: boot,
	}, testClient.Client, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(5 * time.Second) })
	store, err := flowstore.NewManager(t.Context(), testClient.Client)
	if err != nil {
		t.Fatal(err)
	}
	registry := component.NewRegistry()
	if err := registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "publish-test", Type: "processor", Protocol: "nats", Domain: "test",
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return &baseDiscoverable{name: "publish-test"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return &FlowService{
		BaseService: NewBaseServiceWithOptions("flow-publish-test", nil, WithLogger(slog.Default())),
		flowStore:   store,
		flowEngine:  flowengine.NewEngine(registry, testClient.Client, slog.Default(), nil),
		configMgr:   manager,
		bootConfig:  manager.BootConfig(),
	}, manager
}

func publishTestFlow(names ...string) *flowstore.Flow {
	flow := &flowstore.Flow{ID: "publish-flow", Name: "Publish flow"}
	for index, name := range names {
		flow.Nodes = append(flow.Nodes, flowstore.FlowNode{
			ID: "node-" + name, Name: name, Component: "publish-test",
			Type:   types.ComponentTypeProcessor,
			Config: map[string]any{"index": index},
		})
	}
	return flow
}

func invokePublish(t *testing.T, service *FlowService) (int, PublishComponentConfigsResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/flows/publish-flow/publish-component-configs", bytes.NewReader(nil))
	request.SetPathValue("id", "publish-flow")
	service.handlePublishComponentConfigs(recorder, request)
	var response PublishComponentConfigsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response (%d): %v: %s", recorder.Code, err, recorder.Body.String())
	}
	return recorder.Code, response
}

func TestPublishComponentConfigsDeterministicPartialRetryAndCRUDIsolation(t *testing.T) {
	service, manager := newFlowPublishFixture(t, make(config.ComponentConfigs))
	flow := publishTestFlow("gamma", "alpha", "beta")
	if err := service.flowStore.Create(t.Context(), flow); err != nil {
		t.Fatal(err)
	}
	failing := &failOncePublisher{componentConfigPublisher: manager, failName: "beta"}
	service.configMgr = failing

	status, first := invokePublish(t, service)
	if status != http.StatusInternalServerError || first.FailedComponent != "beta" {
		t.Fatalf("first publish: status=%d response=%#v", status, first)
	}
	if len(first.PersistedComponents) != 1 || first.PersistedComponents[0] != "alpha" || !first.RuntimeUnchanged {
		t.Fatalf("partial progress not exact and sorted: %#v", first)
	}
	status, retry := invokePublish(t, service)
	if status != http.StatusOK {
		t.Fatalf("retry status=%d response=%#v", status, retry)
	}
	want := []string{"alpha", "beta", "gamma"}
	if len(retry.PersistedComponents) != len(want) {
		t.Fatalf("retry progress=%#v", retry.PersistedComponents)
	}
	for i := range want {
		if retry.PersistedComponents[i] != want[i] {
			t.Fatalf("retry order=%#v", retry.PersistedComponents)
		}
	}
	if !retry.RuntimeUnchanged || !retry.RestartRequired {
		t.Fatalf("retry truth fields=%#v", retry)
	}

	before := manager.GetConfig().Get().Clone()
	flow.Name = "Renamed diagram"
	if err := service.flowStore.Update(t.Context(), flow); err != nil {
		t.Fatal(err)
	}
	if err := service.flowStore.Delete(t.Context(), flow.ID); err != nil {
		t.Fatal(err)
	}
	after := manager.GetConfig().Get()
	if len(after.Components) != len(before.Components) {
		t.Fatalf("flow CRUD mutated component config: before=%d after=%d", len(before.Components), len(after.Components))
	}
}

func TestPublishIdenticalBootComponentsDoesNotRequireRestart(t *testing.T) {
	boot := config.ComponentConfigs{
		"same": {Type: types.ComponentTypeProcessor, Name: "publish-test", Enabled: true, Config: json.RawMessage(`{"index":0}`)},
	}
	service, _ := newFlowPublishFixture(t, boot)
	if err := service.flowStore.Create(t.Context(), publishTestFlow("same")); err != nil {
		t.Fatal(err)
	}
	status, response := invokePublish(t, service)
	if status != http.StatusOK || response.RestartRequired || !response.RuntimeUnchanged {
		t.Fatalf("identical publish: status=%d response=%#v", status, response)
	}
}

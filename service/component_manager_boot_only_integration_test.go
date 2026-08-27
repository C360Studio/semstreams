//go:build integration

package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

func TestComponentManagerPostBootComponentAndModelWritesDoNotChangeRuntimeComposition(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	defer testClient.Terminate()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	bootConfig := types.ComponentConfig{
		Name: "boot-test", Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{"revision":"boot"}`),
	}
	configManager, err := config.NewConfigManager(&config.Config{
		Version:    "1.0.0",
		Platform:   config.PlatformConfig{Org: "test", ID: "boot-only", Environment: "test"},
		Components: config.ComponentConfigs{"worker": bootConfig},
	}, testClient.Client, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := configManager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer configManager.Stop(5 * time.Second)

	registry := component.NewRegistry()
	created := &mockDiscoverableComponent{metadata: component.Metadata{Name: "worker", Type: "processor"}}
	var receivedModelRegistry model.RegistryReader
	if err := registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "boot-test", Type: "processor",
		Factory: func(_ json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
			receivedModelRegistry = deps.ModelRegistry
			return created, nil
		},
		Ports: func(json.RawMessage, string) (component.PortConfig, error) { return component.PortConfig{}, nil },
	}); err != nil {
		t.Fatal(err)
	}

	serviceValue, err := NewComponentManager(json.RawMessage(`{}`), &Dependencies{
		NATSClient: testClient.Client, Manager: configManager, ComponentRegistry: registry,
		Logger: logger, Platform: types.PlatformMeta{Org: "test", Platform: "boot-only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := serviceValue.(*ComponentManager)
	assertRuntime := func(wantNames []string) {
		t.Helper()
		if err := manager.withComponents(func(components map[string]*component.ManagedComponent) error {
			if len(components) != len(wantNames) {
				t.Fatalf("runtime membership = %v, want %v", components, wantNames)
			}
			managed, ok := components["worker"]
			if !ok || managed.Component != created {
				t.Fatalf("runtime identity changed: worker=%#v want=%p", managed, created)
			}
			if string(managed.Config.Config) != string(bootConfig.Config) {
				t.Fatalf("runtime config = %s, want boot config %s", managed.Config.Config, bootConfig.Config)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertRuntime([]string{"worker"})
	if receivedModelRegistry != nil {
		t.Fatal("factory received unexpected boot model registry")
	}

	if err := configManager.PutComponentToKV(ctx, "worker", types.ComponentConfig{
		Name: "boot-test", Type: types.ComponentTypeProcessor, Enabled: true,
		Config: json.RawMessage(`{"revision":"next-boot"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := configManager.PutComponentToKV(ctx, "late", bootConfig); err != nil {
		t.Fatal(err)
	}
	assertRuntime([]string{"worker"})

	modelUpdates := configManager.OnChange("model_registry")
	<-modelUpdates // initial snapshot
	bucket, err := testClient.Client.GetKeyValueBucket(ctx, "semstreams_config")
	if err != nil {
		t.Fatal(err)
	}
	nextModel := &model.Registry{Endpoints: map[string]*model.EndpointConfig{"next": {URL: "http://next.invalid", Model: "next"}}}
	data, err := json.Marshal(nextModel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Put(ctx, "model_registry", data); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-modelUpdates:
		if update.Config.Get().ModelRegistry == nil {
			t.Fatal("real Config Manager did not apply model_registry write")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Config Manager model_registry update")
	}
	assertRuntime([]string{"worker"})
	if receivedModelRegistry != nil {
		t.Fatal("post-boot model write changed factory dependency identity")
	}
}

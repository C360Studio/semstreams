//go:build integration

package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

type bootCountingComponent struct {
	baseDiscoverable
	starts *atomic.Int32
}

func (*bootCountingComponent) Initialize() error             { return nil }
func (c *bootCountingComponent) Start(context.Context) error { c.starts.Add(1); return nil }
func (*bootCountingComponent) Stop(context.Context) error    { return nil }

func TestComponentConfigWritesApplyOnlyToFreshBoot(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := &config.Config{
		Version:    "1.0.0",
		Platform:   config.PlatformConfig{Org: "test", ID: "boot-components", Type: "test"},
		Components: make(config.ComponentConfigs),
	}

	first, err := config.NewConfigManager(ctx, base.Clone(), testClient.Client, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var starts atomic.Int32
	newRegistry := func() *component.Registry {
		registry := component.NewRegistry()
		if err := registry.RegisterWithConfig(component.RegistrationConfig{
			Name: "boot-counting", Type: "processor", Protocol: "nats", Domain: "test",
			Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
				return &bootCountingComponent{baseDiscoverable: baseDiscoverable{name: "boot-counting"}, starts: &starts}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
		return registry
	}
	firstService, err := NewComponentManager(nil, &Dependencies{
		NATSClient: testClient.Client, Manager: first, ComponentRegistry: newRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	firstComponents := firstService.(*ComponentManager)
	if err := firstComponents.Start(ctx); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 6; i++ {
		name := "published-" + string(rune('a'+i))
		if err := first.PutComponentToKV(ctx, name, types.ComponentConfig{
			Type: types.ComponentTypeProcessor, Name: "boot-counting", Enabled: true,
			Config: json.RawMessage(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := starts.Load(); got != 0 {
		t.Fatalf("current boot started %d newly published components", got)
	}
	if got := len(firstComponents.GetComponentStatus()); got != 0 {
		t.Fatalf("current component registry changed: %d components", got)
	}
	if err := firstComponents.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Stop(5 * time.Second); err != nil {
		t.Fatal(err)
	}

	second, err := config.NewConfigManager(ctx, base.Clone(), testClient.Client, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer second.Stop(5 * time.Second)
	if got := len(second.BootConfig().Components); got != 6 {
		t.Fatalf("fresh boot sealed %d components, want 6", got)
	}
	secondService, err := NewComponentManager(nil, &Dependencies{
		NATSClient: testClient.Client, Manager: second, ComponentRegistry: newRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondComponents := secondService.(*ComponentManager)
	if err := secondComponents.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer secondComponents.Stop(context.Background())
	if got := starts.Load(); got != 6 {
		t.Fatalf("fresh boot start count = %d, want 6", got)
	}
}

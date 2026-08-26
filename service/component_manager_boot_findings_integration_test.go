//go:build integration

package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

// declaredComponent is a fake whose constructed ports are exactly the
// resolution of its declarer, so P1 parity holds and the boot-time findings
// come from the composition alone.
type declaredComponent struct {
	baseDiscoverable
	inputs  []component.Port
	outputs []component.Port
}

func (d *declaredComponent) InputPorts() []component.Port  { return d.inputs }
func (d *declaredComponent) OutputPorts() []component.Port { return d.outputs }

func registerDeclaredFactory(t *testing.T, registry *component.Registry, name string, ports component.PortConfig) {
	t.Helper()
	inputs := make([]component.Port, 0, len(ports.Inputs))
	for _, definition := range ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, port)
	}
	outputs := make([]component.Port, 0, len(ports.Outputs))
	for _, definition := range ports.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, port)
	}
	if err := registry.RegisterWithConfig(component.RegistrationConfig{
		Name: name, Type: "processor",
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return &declaredComponent{baseDiscoverable: baseDiscoverable{name: name}, inputs: inputs, outputs: outputs}, nil
		},
		Ports: func(json.RawMessage, string) (component.PortConfig, error) { return ports, nil },
	}); err != nil {
		t.Fatal(err)
	}
}

func bootComponentManager(
	t *testing.T, testClient *natsclient.TestClient, registry *component.Registry, components config.ComponentConfigs,
) (*ComponentManager, error) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	configManager, err := config.NewConfigManager(&config.Config{
		Version:    "1.0.0",
		Platform:   config.PlatformConfig{Org: "test", ID: "boot-findings", Environment: "test"},
		Components: components,
	}, testClient.Client, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := configManager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = configManager.Stop(5 * time.Second) })
	serviceValue, err := NewComponentManager(json.RawMessage(`{}`), &Dependencies{
		NATSClient: testClient.Client, Manager: configManager, ComponentRegistry: registry,
		Logger: logger, Platform: types.PlatformMeta{Org: "test", Platform: "boot-findings"},
	})
	if err != nil {
		return nil, err
	}
	return serviceValue.(*ComponentManager), nil
}

func processorInstance(factory string) types.ComponentConfig {
	return types.ComponentConfig{Name: factory, Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{}`)}
}

// TestComponentManagerRefusesBootOnErrorFinding — a JetStream input fed only
// by a core-NATS output is a stream_requirement error; Initialize (and so
// boot) fails naming the finding and the component, nothing starts, and the
// Registry is not sealed as a running composition.
func TestComponentManagerRefusesBootOnErrorFinding(t *testing.T) {
	t.Skip("[~] composition-validation-substrate tasks 3.6: the boot refuse is not flipped — the P3-before-P5 " +
		"measurement found error findings in 12 of 22 shipped configurations from two validator classes " +
		"pending the owner's ruling (tasks 3.5); the test stays as the target state")
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	registry := component.NewRegistry()
	registerDeclaredFactory(t, registry, "core-publisher", component.PortConfig{
		Outputs: []component.PortDefinition{{Name: "out", Required: true, Config: component.NATSPort{Subject: "boot.data"}}},
	})
	registerDeclaredFactory(t, registry, "stream-consumer", component.PortConfig{
		Inputs: []component.PortDefinition{{Name: "in", Required: true, Config: component.JetStreamPort{StreamName: "BOOT", Subjects: []string{"boot.data"}}}},
	})

	_, err := bootComponentManager(t, testClient, registry, config.ComponentConfigs{
		"pub": processorInstance("core-publisher"),
		"sub": processorInstance("stream-consumer"),
	})
	if err == nil {
		t.Fatal("NewComponentManager booted a composition with a stream_requirement error")
	}
	for _, want := range []string{composition.TypeStreamRequirement, "sub"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("boot error %q does not name %q", err.Error(), want)
		}
	}
	// Not sealed as a running composition: admission is still open.
	registerDeclaredFactory(t, registry, "late", component.PortConfig{})
	if _, err := registry.CreateComponent(componentadmission.Access{}, "late-instance", processorInstance("late"),
		component.Dependencies{NATSClient: testClient.Client}, nil); err != nil {
		t.Fatalf("Registry was sealed after a refused boot: %v", err)
	}
}

// TestComponentManagerExposesBootFindings — a booted composition with one
// disconnected_node warning serves the retained boot result verbatim at
// <components>/validate.
func TestComponentManagerExposesBootFindings(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	registry := component.NewRegistry()
	registerDeclaredFactory(t, registry, "lonely", component.PortConfig{
		Inputs: []component.PortDefinition{{Name: "in", Required: false, Config: component.NATSPort{Subject: "nobody.publishes"}}},
	})
	manager, err := bootComponentManager(t, testClient, registry, config.ComponentConfigs{
		"alone": processorInstance("lonely"),
	})
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	retained := manager.bootFindings
	if retained == nil {
		t.Fatal("ComponentManager retained no boot findings")
	}
	if retained.Status != composition.StatusWarnings {
		t.Fatalf("retained status = %q, want warnings", retained.Status)
	}
	if got := findingTypes(retained.Warnings); !contains(got, composition.TypeDisconnectedNode) {
		t.Fatalf("retained warnings %v lack disconnected_node", got)
	}

	mux := http.NewServeMux()
	manager.RegisterHTTPHandlers("/components/", mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/components/validate", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /components/validate = %d: %s", recorder.Code, recorder.Body.String())
	}
	var served composition.Result
	if err := json.Unmarshal(recorder.Body.Bytes(), &served); err != nil {
		t.Fatalf("decode: %v\n%s", err, recorder.Body.String())
	}
	if !reflect.DeepEqual(normalize(t, served), normalize(t, *retained)) {
		t.Fatalf("served result differs from the retained boot result:\nserved=%s\nretained=%s", recorder.Body.String(), mustJSON(t, retained))
	}
}

// TestGraphProjectionMatchesAdmittedComposition — <components>/flowgraph
// names every admitted instance with its resolved ports and every derived
// edge, as JSON and as Mermaid.
func TestGraphProjectionMatchesAdmittedComposition(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	registry := component.NewRegistry()
	registerDeclaredFactory(t, registry, "js-publisher", component.PortConfig{
		Outputs: []component.PortDefinition{{Name: "out", Required: true, Config: component.JetStreamPort{StreamName: "PROJ", Subjects: []string{"proj.data"}}}},
	})
	registerDeclaredFactory(t, registry, "js-consumer", component.PortConfig{
		Inputs: []component.PortDefinition{{Name: "in", Required: true, Config: component.JetStreamPort{StreamName: "PROJ", Subjects: []string{"proj.data"}}}},
	})
	manager, err := bootComponentManager(t, testClient, registry, config.ComponentConfigs{
		"pub": processorInstance("js-publisher"),
		"sub": processorInstance("js-consumer"),
	})
	if err != nil {
		t.Fatalf("boot: %v", err)
	}

	mux := http.NewServeMux()
	manager.RegisterHTTPHandlers("/components/", mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/components/flowgraph", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /components/flowgraph = %d: %s", recorder.Code, recorder.Body.String())
	}
	var graph composition.Graph
	if err := json.Unmarshal(recorder.Body.Bytes(), &graph); err != nil {
		t.Fatalf("decode graph: %v\n%s", err, recorder.Body.String())
	}
	byInstance := map[string]composition.Node{}
	for _, node := range graph.Nodes {
		byInstance[node.Instance] = node
	}
	for _, snapshot := range registry.Snapshots(componentadmission.Access{}) {
		node, ok := byInstance[snapshot.Name()]
		if !ok {
			t.Fatalf("projection lacks admitted instance %s", snapshot.Name())
		}
		if node.Factory != snapshot.Factory() {
			t.Errorf("node %s factory = %q, admitted %q", node.Instance, node.Factory, snapshot.Factory())
		}
		if len(node.Inputs) != len(snapshot.Inputs()) || len(node.Outputs) != len(snapshot.Outputs()) {
			t.Errorf("node %s ports %d/%d, admitted %d/%d", node.Instance, len(node.Inputs), len(node.Outputs), len(snapshot.Inputs()), len(snapshot.Outputs()))
		}
	}
	if len(graph.Edges) != 1 || graph.Edges[0].From != "pub" || graph.Edges[0].To != "sub" {
		t.Fatalf("edges = %+v, want one pub→sub edge", graph.Edges)
	}

	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/components/flowgraph?format=mermaid", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /components/flowgraph?format=mermaid = %d: %s", recorder.Code, recorder.Body.String())
	}
	rendered := recorder.Body.String()
	if rendered != composition.Mermaid(graph) {
		t.Fatalf("Mermaid projection differs from Mermaid(graph):\n%s\n---\n%s", rendered, composition.Mermaid(graph))
	}
	for _, instance := range []string{"pub", "sub"} {
		if !strings.Contains(rendered, instance) {
			t.Errorf("Mermaid lacks %s:\n%s", instance, rendered)
		}
	}
	if strings.Count(rendered, "-->") != 1 {
		t.Fatalf("Mermaid renders %d edges, want 1:\n%s", strings.Count(rendered, "-->"), rendered)
	}
}

func findingTypes(findings []composition.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		out = append(out, finding.Type)
	}
	return out
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func normalize(t *testing.T, result composition.Result) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(mustJSON(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

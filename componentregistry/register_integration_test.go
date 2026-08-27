//go:build integration

package componentregistry_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"sort"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	optionalotel "github.com/c360studio/semstreams/frameworkadapters/otel"
	"github.com/c360studio/semstreams/frameworkcapabilities/graphresearch"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/types"
)

// parityRow is the smallest configuration one factory admits through boot
// admission. The inventory's nil-deps column (`docs/proposals/gh1089-flow-
// boundary-inventory.md` §2.3) names the factories that reject `{}`; those
// rows carry the minimum their schema requires.
type parityRow struct {
	factory string
	typ     types.ComponentType
	config  string
}

func parityRows() []parityRow {
	const graphQueriesInput = `{"ports":{"inputs":[{"name":"graph_queries","required":true,"config":{"kind":"nats-request","subject":"graph.query.*","interface":{"type":"graph.query","version":"v1"}}}],"outputs":[]}}`
	const graphIngestPorts = `{"ports":{"inputs":[{"name":"graph_mutations","required":true,"config":{"kind":"nats-request","subject":"graph.mutation.>","interface":{"type":"semstreams.graph.mutation","version":"v1"}}},{"name":"entity_in","config":{"kind":"jetstream","stream_name":"ENTITY","subjects":["entity.>"]}}],"outputs":[{"name":"entity_states","config":{"kind":"kv-write","bucket":"ENTITY_STATES"}}]}}`
	const gatewayPorts = `{"ports":{"outputs":[{"name":"graph_queries","required":true,"config":{"kind":"nats-request","subject":"graph.query.*","interface":{"type":"graph.query","version":"v1"}}},{"name":"graph_index_queries","required":true,"config":{"kind":"nats-request","subject":"graph.index.query.*"}},{"name":"agentic_queries","required":true,"config":{"kind":"nats-request","subject":"agentic.query.*","interface":{"type":"agentic.query","version":"v1"}}}]}}`
	return []parityRow{
		{"agentic-dispatch", types.ComponentTypeProcessor, `{}`},
		{"agentic-governance", types.ComponentTypeProcessor, `{}`},
		{"agentic-loop", types.ComponentTypeProcessor, `{}`},
		{"agentic-model", types.ComponentTypeProcessor, `{}`},
		{"agentic-tools", types.ComponentTypeProcessor, `{}`},
		{"file", types.ComponentTypeOutput, `{"directory":"/tmp/parity","file_prefix":"parity","format":"jsonl"}`},
		{"file_input", types.ComponentTypeInput, `{"path":"/tmp/parity.jsonl","ports":{"outputs":[{"name":"file_out","config":{"kind":"nats","subject":"parity.file"}}]}}`},
		{"gated-dag", types.ComponentTypeProcessor, `{"unit_entity_prefix":"c360.parity.dag.unit","dispatch_subject":"parity.dag.dispatch"}`},
		{"graph-clustering", types.ComponentTypeProcessor, `{}`},
		{"graph-embedding", types.ComponentTypeProcessor, `{}`},
		{"graph-gateway", types.ComponentTypeGateway, gatewayPorts},
		{"graph-index", types.ComponentTypeProcessor, `{}`},
		{"graph-index-spatial", types.ComponentTypeProcessor, `{}`},
		{"graph-index-temporal", types.ComponentTypeProcessor, `{}`},
		{"graph-ingest", types.ComponentTypeProcessor, graphIngestPorts},
		{"graph-query", types.ComponentTypeProcessor, graphQueriesInput},
		{"http", types.ComponentTypeGateway, `{"routes":[{"path":"/parity","method":"POST","nats_subject":"parity.http"}]}`},
		{"httppost", types.ComponentTypeOutput, `{"url":"http://localhost:1/parity"}`},
		{"json_filter", types.ComponentTypeProcessor, `{}`},
		{"json_generic", types.ComponentTypeProcessor, `{}`},
		{"json_map", types.ComponentTypeProcessor, `{}`},
		{"lifecycle-gateway", types.ComponentTypeGateway, `{"path_prefix":"workflows","ports":{"outputs":[{"name":"graph_mutations","required":true,"config":{"kind":"nats-request","subject":"graph.mutation.>","interface":{"type":"semstreams.graph.mutation","version":"v1"}}}]}}`},
		{"objectstore", types.ComponentTypeStorage, `{}`},
		{"otel-exporter", types.ComponentTypeOutput, `{}`},
		{"research-graph-assess", types.ComponentTypeProcessor, `{}`},
		{"research-graph-classify", types.ComponentTypeProcessor, `{}`},
		{"research-graph-execute", types.ComponentTypeProcessor, `{}`},
		{"research-graph-route", types.ComponentTypeProcessor, `{}`},
		{"research-graph-synthesize", types.ComponentTypeProcessor, `{}`},
		{"rule-processor", types.ComponentTypeProcessor, `{"pack_id":"parity-pack"}`},
		// udp overrides one named port so a declarer that drops the merge (tasks 4.3) is caught here.
		{"udp", types.ComponentTypeInput, `{"ports":{"inputs":[{"name":"udp_socket","required":true,"config":{"kind":"network","protocol":"udp","host":"0.0.0.0","port":14551}}]}}`},
		{"websocket", types.ComponentTypeOutput, `{}`},
		{"websocket_input", types.ComponentTypeInput, `{}`},
	}
}

func fullRegistry(t *testing.T) *component.Registry {
	t.Helper()
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		t.Fatalf("register core: %v", err)
	}
	if err := graphresearch.RegisterComponents(registry); err != nil {
		t.Fatalf("register graph research: %v", err)
	}
	if err := optionalotel.Register(registry); err != nil {
		t.Fatalf("register OTEL: %v", err)
	}
	return registry
}

// TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory constructs
// every registered factory through boot admission with real dependencies,
// evaluates its declarer for the same input, and compares the resolved ports
// port for port. The row count is asserted against ListFactories so a new
// factory cannot be skipped.
func TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := fullRegistry(t)
	rows := parityRows()
	if got, want := len(rows), len(registry.ListFactories()); got != want {
		t.Fatalf("parity table has %d rows, registry has %d factories: every factory needs a row", got, want)
	}
	payloads := payloadregistry.New()
	if err := payloadbuiltins.Register(payloads); err != nil {
		t.Fatal(err)
	}
	deps := component.Dependencies{
		NATSClient:       testClient.Client,
		Logger:           logger,
		Platform:         types.PlatformMeta{Org: "parity", Platform: "test"},
		ModelRegistry:    &model.Registry{Endpoints: map[string]*model.EndpointConfig{"parity": {URL: "http://parity.invalid", Model: "parity"}}},
		ToolRegistry:     agentictools.NewExecutorRegistry(),
		PayloadRegistry:  payloads,
		LifecycleManager: lifecycle.NewManager(testClient.Client, logger),
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].factory < rows[j].factory })
	for _, row := range rows {
		t.Run(row.factory, func(t *testing.T) {
			instance := row.factory + "-parity"
			cfg := types.ComponentConfig{
				Name: row.factory, Type: row.typ, Enabled: true, Config: json.RawMessage(row.config),
			}
			declared, err := registry.Declare(instance, cfg)
			if err != nil {
				t.Fatalf("Declare(%s): %v", row.factory, err)
			}
			if _, err := registry.CreateComponent(componentadmission.Access{}, instance, cfg, deps, nil); err != nil {
				t.Fatalf("CreateComponent(%s): %v", row.factory, err)
			}
			admitted, ok := registry.Snapshot(componentadmission.Access{}, instance)
			if !ok {
				t.Fatalf("no admitted declaration for %s", instance)
			}
			assertPortsEqual(t, "inputs", declared.InputPorts, declared.InputFacts, admitted.Inputs(), admitted.InputDeclarationFacts())
			assertPortsEqual(t, "outputs", declared.OutputPorts, declared.OutputFacts, admitted.Outputs(), admitted.OutputDeclarationFacts())
		})
	}
}

func assertPortsEqual(
	t *testing.T, lane string,
	declared []component.Port, declaredFacts []component.PortFacts,
	admitted []component.Port, admittedFacts []component.PortFacts,
) {
	t.Helper()
	if len(declared) != len(admitted) {
		t.Fatalf("%s: declared %d ports, constructed %d", lane, len(declared), len(admitted))
	}
	for index := range declared {
		d, a := declared[index], admitted[index]
		df, af := declaredFacts[index], admittedFacts[index]
		if d.Name != a.Name || d.Direction != a.Direction || d.Required != a.Required {
			t.Errorf("%s[%d]: declared %s/%s/%v, constructed %s/%s/%v", lane, index, d.Name, d.Direction, d.Required, a.Name, a.Direction, a.Required)
		}
		if df.Kind() != af.Kind() || df.ResourceID() != af.ResourceID() {
			t.Errorf("%s[%d] %s: declared %s %s, constructed %s %s", lane, index, d.Name, df.Kind(), df.ResourceID(), af.Kind(), af.ResourceID())
		}
		if got, want := df.NATSSubjects(), af.NATSSubjects(); !equalStrings(got, want) {
			t.Errorf("%s[%d] %s: declared subjects %v, constructed %v", lane, index, d.Name, got, want)
		}
		dc, dok := df.Interface()
		ac, aok := af.Interface()
		if dok != aok || dc.Type != ac.Type || dc.Version != ac.Version {
			t.Errorf("%s[%d] %s: declared interface %v/%+v, constructed %v/%+v", lane, index, d.Name, dok, dc, aok, ac)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

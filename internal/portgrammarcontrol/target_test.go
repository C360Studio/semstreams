package portgrammarcontrol

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	semconfig "github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/natsclient"
)

type targetConfigItem struct {
	workItem               WorkItem
	lane                   string
	row                    map[string]any
	deleted                bool
	inputIdentityCorrected bool
	portNameCorrected      bool
	primitiveCorrected     bool
}

func TestFoundationBTargetCompleteness(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := LoadPlan(root)
	if err != nil {
		t.Fatal(err)
	}

	targets := make([]targetConfigItem, 0, len(plan.ConfigItems()))
	for _, item := range plan.ConfigItems() {
		target, err := targetForConfigItem(item, plan.Dispositions)
		if err != nil {
			t.Fatalf("target %s: %v", item.RecordID, err)
		}
		targets = append(targets, target)
	}

	documents := make(map[string]any, plan.ConfigDocumentCount())
	portsParents := make(map[string]struct{})
	graphGatewayParents := make(map[string]struct{})
	survivors := 0
	deletions := 0
	inputIdentityCorrections := 0
	portNameCorrections := 0
	primitiveCorrections := 0
	retired := retiredConfigAccounting{documents: make(map[string]struct{})}
	for _, target := range targets {
		if target.inputIdentityCorrected {
			inputIdentityCorrections++
		}
		if target.portNameCorrected {
			portNameCorrections++
		}
		if target.primitiveCorrected {
			primitiveCorrections++
		}
		if retired.consume(t, root, target) {
			continue
		}
		document, ok := documents[target.workItem.Path]
		if !ok {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(target.workItem.Path)))
			if err != nil {
				t.Fatal(err)
			}
			document, err = decodeJSON(data)
			if err != nil {
				t.Fatalf("decode %s: %v", target.workItem.Path, err)
			}
			documents[target.workItem.Path] = document
		}

		segments := splitPointer(target.workItem.Pointer)
		portsPath := segments[:len(segments)-2]
		portsParents[target.workItem.Path+"#"+jsonPointer(portsPath)] = struct{}{}
		if target.workItem.Enclosing == "graph-gateway" {
			graphGatewayParents[target.workItem.Path+"#"+jsonPointer(portsPath)] = struct{}{}
		}
		portsValue, err := getPointer(document, portsPath)
		if err != nil {
			t.Fatalf("ports for %s: %v", target.workItem.RecordID, err)
		}
		ports, ok := portsValue.(map[string]any)
		if !ok {
			t.Fatalf("ports for %s are %T, want object", target.workItem.RecordID, portsValue)
		}

		if target.deleted {
			deletions++
			for _, lane := range []string{"inputs", "outputs"} {
				if matches := namedRows(ports[lane], target.workItem.Name); len(matches) != 0 {
					t.Errorf("deleted identity %s remains in %s", target.workItem.RecordID, lane)
				}
			}
			continue
		}

		survivors++
		targetName := stringValue(target.row["name"])
		matches := namedRows(ports[target.lane], targetName)
		if len(matches) != 1 {
			t.Errorf("target identity %s has %d rows in %s, want 1", target.workItem.RecordID, len(matches), target.lane)
			continue
		}
		want, err := compactJSON(target.row)
		if err != nil {
			t.Fatal(err)
		}
		got, err := compactJSON(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("target identity %s changed: got %s want %s", target.workItem.RecordID, got, want)
		}
		assertProductionPortResolution(t, target.workItem.RecordID, target.lane, matches[0])
	}

	assertFoundationBTargetAccounting(t, targetAccounting{
		survivors: survivors, deletions: deletions,
		inputIdentityCorrections: inputIdentityCorrections,
		portNameCorrections:      portNameCorrections,
		primitiveCorrections:     primitiveCorrections,
		retiredSurvivors:         retired.survivors,
		retiredDeletions:         retired.deletions,
		retiredDocuments:         len(retired.documents),
	}, root, plan, documents, portsParents, graphGatewayParents)
}

func TestPostFoundationBGraphQueryCutoverAmendmentIsExact(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := LoadPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	items, err := indexItems(plan.ConfigItems())
	if err != nil {
		t.Fatal(err)
	}
	if len(postFoundationBGraphQueryLegacyInputRetirements) != 11 {
		t.Fatalf("graph-query legacy input retirements=%d, want 11",
			len(postFoundationBGraphQueryLegacyInputRetirements))
	}
	if len(postFoundationBGraphGatewayInterfaceAmendments) != 8 {
		t.Fatalf("graph-gateway interface amendments=%d, want 8",
			len(postFoundationBGraphGatewayInterfaceAmendments))
	}
	if len(postFoundationBGraphQueryProviderReplacements) != 11 {
		t.Fatalf("graph-query provider replacements=%d, want 11",
			len(postFoundationBGraphQueryProviderReplacements))
	}
	if len(postFoundationBResearchQueryRawAdditions) != 10 {
		t.Fatalf("research query raw additions=%d, want 10",
			len(postFoundationBResearchQueryRawAdditions))
	}
	if len(postFoundationBGraphQueryProviderReplacements)+len(postFoundationBResearchQueryRawAdditions) != 21 {
		t.Fatalf("post-Foundation-B enumerated additions=%d, want 21",
			len(postFoundationBGraphQueryProviderReplacements)+len(postFoundationBResearchQueryRawAdditions))
	}
	for id := range postFoundationBGraphQueryLegacyInputRetirements {
		item, ok := items[id]
		if !ok {
			t.Fatalf("graph-query retirement %s is not a frozen config identity", id)
		}
		if item.Enclosing != "graph-query" || item.Lane != "inputs" {
			t.Fatalf("graph-query retirement %s has enclosing/lane %s/%s", id, item.Enclosing, item.Lane)
		}
	}
	for id := range postFoundationBGraphGatewayInterfaceAmendments {
		item, ok := items[id]
		if !ok {
			t.Fatalf("graph-gateway amendment %s is not a frozen config identity", id)
		}
		if item.Enclosing != "graph-gateway" || item.Lane != "outputs" {
			t.Fatalf("graph-gateway amendment %s has enclosing/lane %s/%s", id, item.Enclosing, item.Lane)
		}
	}
	if len(postFoundationBGraphQueryProviderReplacements) != len(postFoundationBGraphQueryLegacyInputRetirements) {
		t.Fatalf("provider replacements=%d, legacy retirements=%d; cutover must remain one-for-one",
			len(postFoundationBGraphQueryProviderReplacements), len(postFoundationBGraphQueryLegacyInputRetirements))
	}
	for _, identity := range postFoundationBGraphQueryProviderReplacements {
		t.Run("effective/"+identity.testName(), func(t *testing.T) {
			assertEffectiveGraphQueryPortIdentity(t, root, identity)
		})
	}
	for _, identity := range postFoundationBResearchQueryRawAdditions {
		t.Run("raw/"+identity.testName(), func(t *testing.T) {
			assertRawGraphQueryPortIdentity(t, root, identity)
		})
	}
	goItems, err := indexItems(plan.GoItems())
	if err != nil {
		t.Fatal(err)
	}
	if len(postFoundationBGraphQueryGoIdentityRetirements) != 4 {
		t.Fatalf("graph-query Go identity retirements=%d, want 4",
			len(postFoundationBGraphQueryGoIdentityRetirements))
	}
	for id := range postFoundationBGraphQueryGoIdentityRetirements {
		item, ok := goItems[id]
		if !ok || item.Path != "processor/graph-query/component.go" {
			t.Fatalf("graph-query Go retirement %s is not a frozen component.go identity", id)
		}
	}
	goAdditions := 0
	for _, identities := range postFoundationBGraphQueryGoIdentityAdditions {
		goAdditions += len(identities)
	}
	if goAdditions != 3 {
		t.Fatalf("graph-query cutover Go additions=%d, want 3", goAdditions)
	}
}

func TestPostFoundationBToolDiscoveryCutoverAmendmentIsExact(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := LoadPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	goItems, err := indexItems(plan.GoItems())
	if err != nil {
		t.Fatal(err)
	}
	if len(postFoundationBToolDiscoveryGoIdentityRetirements) != 1 {
		t.Fatalf("tool-discovery Go identity retirements=%d, want 1",
			len(postFoundationBToolDiscoveryGoIdentityRetirements))
	}
	for id := range postFoundationBToolDiscoveryGoIdentityRetirements {
		item, ok := goItems[id]
		if !ok {
			t.Fatalf("tool-discovery Go retirement %s is not a frozen Go identity", id)
		}
		if item.Path != "processor/agentic-tools/config.go" ||
			item.Enclosing != "DefaultConfig@L136" || item.Name != "tool.list" || item.CurrentKind != "nats" {
			t.Fatalf("tool-discovery Go retirement %s has path/enclosing/name/kind %s/%s/%s/%s",
				id, item.Path, item.Enclosing, item.Name, item.CurrentKind)
		}
	}
	additions := postFoundationBToolDiscoveryGoIdentityAdditions["processor/agentic-tools/config.go"]
	if len(postFoundationBToolDiscoveryGoIdentityAdditions) != 1 ||
		!slices.Equal(additions, []string{"tool.list|NATSRequestPort"}) {
		t.Fatalf("tool-discovery Go additions=%v, want exact config.go tool.list|NATSRequestPort addition",
			postFoundationBToolDiscoveryGoIdentityAdditions)
	}
	if len(postFoundationBToolDiscoveryGoIdentityRetirements) != len(additions) {
		t.Fatalf("tool-discovery Go additions=%d, retirements=%d; cutover must remain one-for-one",
			len(additions), len(postFoundationBToolDiscoveryGoIdentityRetirements))
	}
}

func assertEffectiveGraphQueryPortIdentity(t *testing.T, root string, identity postFoundationBGraphQueryPortIdentity) {
	t.Helper()
	cfg := loadPostFoundationBConfig(t, root, identity.path)
	componentConfig, ok := cfg.Components[identity.instance]
	if !ok {
		t.Fatalf("%s component %q is missing", identity.path, identity.instance)
	}
	if componentConfig.Name != identity.factory {
		t.Fatalf("%s component %q factory=%q, want %q",
			identity.path, identity.instance, componentConfig.Name, identity.factory)
	}
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		t.Fatal(err)
	}
	discoverable, err := registry.CreateComponent(
		componentadmission.Access{}, identity.instance, componentConfig,
		component.Dependencies{NATSClient: &natsclient.Client{}}, nil,
	)
	if err != nil {
		t.Fatalf("production Registry admission %s/%s: %v", identity.path, identity.instance, err)
	}
	ports := discoverable.InputPorts()
	if identity.lane == "outputs" {
		ports = discoverable.OutputPorts()
	}
	matches := make([]component.Port, 0, 1)
	for _, port := range ports {
		if port.Name == identity.name {
			matches = append(matches, port)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("effective %s/%s %s port %q matches=%d, want 1",
			identity.path, identity.instance, identity.lane, identity.name, len(matches))
	}
	assertGraphQueryPortFacts(t, identity, matches[0])
}

func assertRawGraphQueryPortIdentity(t *testing.T, root string, identity postFoundationBGraphQueryPortIdentity) {
	t.Helper()
	cfg := loadPostFoundationBConfig(t, root, identity.path)
	componentConfig, ok := cfg.Components[identity.instance]
	if !ok {
		t.Fatalf("%s component %q is missing", identity.path, identity.instance)
	}
	if componentConfig.Name != identity.factory {
		t.Fatalf("%s component %q factory=%q, want %q",
			identity.path, identity.instance, componentConfig.Name, identity.factory)
	}
	var raw struct {
		Ports component.PortConfig `json:"ports"`
	}
	if err := json.Unmarshal(componentConfig.Config, &raw); err != nil {
		t.Fatalf("decode raw ports %s/%s: %v", identity.path, identity.instance, err)
	}
	definitions := raw.Ports.Inputs
	direction := component.DirectionInput
	if identity.lane == "outputs" {
		definitions = raw.Ports.Outputs
		direction = component.DirectionOutput
	}
	matches := namedRowsFromDefinitions(definitions, identity.name)
	if len(matches) != 1 {
		t.Fatalf("raw %s/%s %s port %q matches=%d, want 1",
			identity.path, identity.instance, identity.lane, identity.name, len(matches))
	}
	port, err := matches[0].Resolve(direction)
	if err != nil {
		t.Fatalf("resolve raw %s/%s/%s: %v", identity.path, identity.instance, identity.name, err)
	}
	assertGraphQueryPortFacts(t, identity, port)
}

func assertGraphQueryPortFacts(t *testing.T, identity postFoundationBGraphQueryPortIdentity, port component.Port) {
	t.Helper()
	if port.Direction != identity.direction() || !port.Required {
		t.Fatalf("%s direction/required=%s/%t, want %s/true",
			identity.testName(), port.Direction, port.Required, identity.direction())
	}
	facts, err := port.Facts()
	if err != nil {
		t.Fatalf("facts %s: %v", identity.testName(), err)
	}
	if facts.Kind() != component.PortKindNATSRequest || !slices.Equal(facts.NATSSubjects(), []string{identity.subject}) {
		t.Fatalf("%s kind/subjects=%s/%v, want nats-request/[%s]",
			identity.testName(), facts.Kind(), facts.NATSSubjects(), identity.subject)
	}
	contract, ok := facts.Interface()
	if !ok || contract.Type != "graph.query" || contract.Version != "v1" {
		t.Fatalf("%s interface=%+v/%t, want graph.query/v1", identity.testName(), contract, ok)
	}
}

func loadPostFoundationBConfig(t *testing.T, root, path string) *semconfig.Config {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := semconfig.NewLoader().LoadFromBytes(data)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return cfg
}

func namedRowsFromDefinitions(definitions []component.PortDefinition, name string) []component.PortDefinition {
	result := make([]component.PortDefinition, 0, 1)
	for _, definition := range definitions {
		if definition.Name == name {
			result = append(result, definition)
		}
	}
	return result
}

func isOwnerApprovedRetiredConfig(path string) bool {
	_, retired := map[string]struct{}{
		"configs/examples/bm25-semantic-search.json":    {},
		"configs/examples/pathrag-graph-traversal.json": {},
		"configs/http-gateway-semantic-search.json":     {},
		"configs/semantic-basic.json":                   {},
	}[path]
	return retired
}

type retiredConfigAccounting struct {
	survivors int
	deletions int
	documents map[string]struct{}
}

func (a *retiredConfigAccounting) consume(t *testing.T, root string, target targetConfigItem) bool {
	t.Helper()
	if !isOwnerApprovedRetiredConfig(target.workItem.Path) {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(target.workItem.Path))); !os.IsNotExist(err) {
		t.Fatalf("owner-approved retired config %s still exists or cannot be checked: %v",
			target.workItem.Path, err)
	}
	a.documents[target.workItem.Path] = struct{}{}
	if target.deleted {
		a.deletions++
	} else {
		a.survivors++
	}
	return true
}

type targetAccounting struct {
	survivors                int
	deletions                int
	inputIdentityCorrections int
	portNameCorrections      int
	primitiveCorrections     int
	retiredSurvivors         int
	retiredDeletions         int
	retiredDocuments         int
}

func assertFoundationBTargetAccounting(
	t *testing.T,
	accounting targetAccounting,
	root string,
	plan *Plan,
	documents map[string]any,
	portsParents map[string]struct{},
	graphGatewayParents map[string]struct{},
) {
	t.Helper()
	legacyRetirements := len(postFoundationBGraphQueryLegacyInputRetirements)
	if accounting.survivors+accounting.retiredSurvivors != 505-legacyRetirements ||
		accounting.deletions+accounting.retiredDeletions != 17+legacyRetirements {
		t.Fatalf("target accounting: active=%d/%d retired=%d/%d, want 505 survivors minus %d post-Foundation-B retirements and 17 deletions plus those retirements",
			accounting.survivors, accounting.deletions,
			accounting.retiredSurvivors, accounting.retiredDeletions, legacyRetirements)
	}
	retiredTargets := accounting.retiredSurvivors + accounting.retiredDeletions
	if retiredTargets != 5 || accounting.retiredDocuments != 2 {
		t.Fatalf("owner-approved retired Foundation B fixture targets=%d documents=%d, want 5 and 2",
			retiredTargets, accounting.retiredDocuments)
	}
	if accounting.inputIdentityCorrections != 60 {
		t.Fatalf("JetStream input identity corrections=%d, want 60", accounting.inputIdentityCorrections)
	}
	if accounting.portNameCorrections != 11 {
		t.Fatalf("component-default port name corrections=%d, want 11", accounting.portNameCorrections)
	}
	if accounting.primitiveCorrections != 10 {
		t.Fatalf("component-default primitive corrections=%d, want 10", accounting.primitiveCorrections)
	}
	actualRows := countCanonicalConfigRows(t, documents, portsParents)
	// cloud-federation's ws_control and hello-world's ALIAS_INDEX are the
	// two owner-approved production-prerequisite additions outside the frozen
	// worklist. They offset two of the five retired historical rows.
	const approvedPrerequisiteAdditions = 2
	// The post-Foundation-B graph-query cutover retires eleven legacy provider
	// targets and replaces them one-for-one, versions eight existing gateway
	// rows without changing their count, and adds ten exact research consumer
	// rows (two classify plus eight execute).
	wantRows := 522 - retiredTargets + approvedPrerequisiteAdditions - legacyRetirements +
		len(postFoundationBGraphQueryProviderReplacements) + len(postFoundationBResearchQueryRawAdditions)
	if actualRows != wantRows {
		t.Fatalf("canonical active config rows=%d, want %d (historical 522 - %d retired fixture targets + %d prerequisites - %d retired legacy graph-query inputs + %d provider replacements + %d research query additions)",
			actualRows, wantRows, retiredTargets, approvedPrerequisiteAdditions, legacyRetirements,
			len(postFoundationBGraphQueryProviderReplacements), len(postFoundationBResearchQueryRawAdditions))
	}
	assertProtocolFlowWebSocketOutput(t, documents)
	assertGraphGatewayConfigAmendment(t, documents, graphGatewayParents)
	if len(plan.GoItems()) != 124 {
		t.Fatalf("frozen Go identities=%d, want 124", len(plan.GoItems()))
	}
	assertGoTargetCompleteness(t, root, plan)
}

// assertProtocolFlowWebSocketOutput records the owner-approved correction to
// the frozen worklist: protocol-flow used retired http_port/path fields and an
// empty output lane. The network endpoint remains runtime-configurable, but it
// must now be expressed in the canonical port grammar.
func assertProtocolFlowWebSocketOutput(t *testing.T, documents map[string]any) {
	t.Helper()
	document := documents["configs/protocol-flow.json"]
	value, err := getPointer(document, splitPointer("/components/websocket/config/ports/outputs"))
	if err != nil {
		t.Fatalf("protocol-flow websocket outputs: %v", err)
	}
	rows, ok := value.([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("protocol-flow websocket outputs=%T/%d, want one row", value, len(rows))
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("protocol-flow websocket output is %T, want object", rows[0])
	}
	if stringValue(row["name"]) != "websocket_server" {
		t.Fatalf("protocol-flow websocket output name=%q, want websocket_server", stringValue(row["name"]))
	}
	assertProductionPortResolution(t, "config:configs/protocol-flow.json#/components/websocket/config/ports/outputs/0", "outputs", row)
}

func countCanonicalConfigRows(t *testing.T, documents map[string]any, parents map[string]struct{}) int {
	t.Helper()
	actualRows := 0
	for identity := range parents {
		path, pointer, ok := splitTargetParentIdentity(identity)
		if !ok {
			t.Fatalf("invalid target parent identity %q", identity)
		}
		portsValue, err := getPointer(documents[path], splitPointer(pointer))
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(portsValue)
		if err != nil {
			t.Fatal(err)
		}
		var config component.PortConfig
		if err := json.Unmarshal(data, &config); err != nil {
			t.Errorf("production PortConfig decode %s: %v", identity, err)
			continue
		}
		actualRows += len(config.Inputs) + len(config.Outputs)
	}
	return actualRows
}

func assertGraphGatewayConfigAmendment(t *testing.T, documents map[string]any, parents map[string]struct{}) {
	t.Helper()
	if len(parents) != 8 {
		t.Fatalf("graph-gateway config blocks=%d, want 8", len(parents))
	}
	want := map[string]string{
		"graph_queries":       "graph.query.*",
		"graph_index_queries": "graph.index.query.*",
		"agentic_queries":     "agentic.query.*",
	}
	for identity := range parents {
		path, pointer, ok := splitTargetParentIdentity(identity)
		if !ok {
			t.Fatalf("invalid graph-gateway parent identity %q", identity)
		}
		value, err := getPointer(documents[path], splitPointer(pointer))
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var ports component.PortConfig
		if err := json.Unmarshal(data, &ports); err != nil {
			t.Fatalf("decode graph-gateway ports %s: %v", identity, err)
		}
		if len(ports.Inputs) != 0 || len(ports.Outputs) != 3 {
			t.Errorf("%s inputs=%d outputs=%d, want 0 and 3", identity, len(ports.Inputs), len(ports.Outputs))
			continue
		}
		for _, definition := range ports.Outputs {
			subject, exists := want[definition.Name]
			if !exists {
				t.Errorf("%s has unexpected graph-gateway output %q", identity, definition.Name)
				continue
			}
			if !definition.Required {
				t.Errorf("%s output %q is not required", identity, definition.Name)
			}
			port, err := definition.Resolve(component.DirectionOutput)
			if err != nil {
				t.Errorf("%s output %q does not resolve: %v", identity, definition.Name, err)
				continue
			}
			facts, err := port.Facts()
			if err != nil {
				t.Errorf("%s output %q facts: %v", identity, definition.Name, err)
				continue
			}
			if facts.Kind() != component.PortKindNATSRequest || !slices.Equal(facts.NATSSubjects(), []string{subject}) {
				t.Errorf("%s output %q facts kind=%q subjects=%v", identity, definition.Name, facts.Kind(), facts.NATSSubjects())
			}
			if definition.Name == "graph_queries" {
				contract, ok := facts.Interface()
				if !ok || contract.Type != "graph.query" || contract.Version != "v1" {
					t.Errorf("%s graph_queries interface=%+v/%t, want graph.query/v1", identity, contract, ok)
				}
			}
		}
	}
}

func assertGoTargetCompleteness(t *testing.T, root string, plan *Plan) {
	t.Helper()
	wantByPath := map[string]map[string]int{}
	for _, item := range plan.GoItems() {
		if _, retired := postFoundationBGraphQueryGoIdentityRetirements[item.RecordID]; retired {
			continue
		}
		if _, retired := postFoundationBToolDiscoveryGoIdentityRetirements[item.RecordID]; retired {
			continue
		}
		if _, retired := postFoundationBUserResponseGoIdentityRetirements[item.RecordID]; retired {
			continue
		}
		if item.Path == "gateway/graph-gateway/component.go" {
			continue
		}
		if item.Path == "storage/objectstore/config.go" && item.Name == "api" &&
			item.CurrentKind == "nats-request" {
			// Owner-approved request/reply clean break: registered Store access
			// replaces the optional ObjectStore API declaration.
			continue
		}
		targetType := targetConfigType(item.CurrentKind)
		if item.Path == "input/udp/udp.go" && item.Name == "nats_output" {
			// Every shipped UDP flow is an acknowledged JetStream ingest path.
			// Strict named replacement therefore requires the factory default to
			// expose the same primitive instead of silently downgrading configs.
			targetType = "JetStreamPort"
		}
		if wantByPath[item.Path] == nil {
			wantByPath[item.Path] = map[string]int{}
		}
		wantByPath[item.Path][item.Name+"|"+targetType]++
	}
	approved := map[string][]string{
		"gateway/graph-gateway/component.go": {
			"graph_queries|NATSRequestPort", "graph_index_queries|NATSRequestPort", "agentic_queries|NATSRequestPort",
			"graph_queries|NATSRequestPort", "graph_index_queries|NATSRequestPort", "agentic_queries|NATSRequestPort",
		},
		"processor/graph-clustering/component.go": {
			"entity_states|KVReadPort", "outgoing_index|KVReadPort", "incoming_index|KVReadPort",
		},
		"processor/agentic-tools/config.go": {
			"entity_states|KVReadPort", "agent_loops|KVReadPort",
		},
		"processor/agentic-loop/config.go": {
			"trajectories|KVWritePort", "trajectory_query|NATSRequestPort",
		},
		"input/http/http.go": {
			"http_schedule|TimerPort", "http_source|HTTPClientPort",
		},
		"input/file/file.go": {
			"file_source|FilePort",
		},
		"processor/gated-dag/component.go": {
			"dispatch|JetStreamPort", "graph_mutations|NATSRequestPort",
		},
		"storage/objectstore/component.go": {
			"store-provide|StoreProvidePort",
		},
	}
	for path, additions := range approved {
		if wantByPath[path] == nil {
			wantByPath[path] = map[string]int{}
		}
		for _, identity := range additions {
			wantByPath[path][identity]++
		}
	}
	for path, additions := range postFoundationBGraphQueryGoIdentityAdditions {
		if wantByPath[path] == nil {
			wantByPath[path] = map[string]int{}
		}
		for _, identity := range additions {
			wantByPath[path][identity]++
		}
	}
	for path, additions := range postFoundationBToolDiscoveryGoIdentityAdditions {
		if wantByPath[path] == nil {
			wantByPath[path] = map[string]int{}
		}
		for _, identity := range additions {
			wantByPath[path][identity]++
		}
	}

	gotByPath := map[string]map[string]int{}
	total := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "component/schema_tags.go" {
			return nil
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		got := gotByPath[relative]
		if got == nil {
			got = map[string]int{}
			gotByPath[relative] = got
		}
		var stack []ast.Node
		configLiteralsByFunction := make(map[*ast.FuncDecl]map[string]*ast.CompositeLit)
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			var parent ast.Node
			if len(stack) != 0 {
				parent = stack[len(stack)-1]
			}
			stack = append(stack, node)
			literal, ok := node.(*ast.CompositeLit)
			if !ok || !isPortDefinitionLiteral(literal, parent) {
				return true
			}
			var configIdentifiers map[string]*ast.CompositeLit
			for index := len(stack) - 1; index >= 0; index-- {
				function, ok := stack[index].(*ast.FuncDecl)
				if !ok {
					continue
				}
				configIdentifiers = configLiteralsByFunction[function]
				if configIdentifiers == nil {
					configIdentifiers = localCanonicalConfigLiterals(function)
					configLiteralsByFunction[function] = configIdentifiers
				}
				break
			}
			name, configType, retired, ok := canonicalGoPortIdentity(literal, configIdentifiers)
			if !ok {
				return true
			}
			if retired != "" {
				t.Errorf("%s:%d target PortDefinition retains outer field %s", relative, fileSet.Position(literal.Pos()).Line, retired)
			}
			if err := validateStaticGoPortConfig(literal, configIdentifiers); err != nil {
				t.Errorf("%s:%d invalid target PortDefinition %q: %v", relative, fileSet.Position(literal.Pos()).Line, name, err)
			}
			got[name+"|"+configType]++
			total++
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]struct{}{}
	for path := range wantByPath {
		paths[path] = struct{}{}
	}
	for path := range gotByPath {
		paths[path] = struct{}{}
	}
	for path := range paths {
		want := wantByPath[path]
		got := gotByPath[path]
		if difference := multisetDifference(want, got); difference != "" {
			t.Errorf("%s target Go identities differ: %s", path, difference)
		}
	}
	wantTotal := 137 - len(postFoundationBGraphQueryGoIdentityRetirements)
	for _, additions := range postFoundationBGraphQueryGoIdentityAdditions {
		wantTotal += len(additions)
	}
	wantTotal -= len(postFoundationBToolDiscoveryGoIdentityRetirements)
	for _, additions := range postFoundationBToolDiscoveryGoIdentityAdditions {
		wantTotal += len(additions)
	}
	wantTotal -= len(postFoundationBUserResponseGoIdentityRetirements)
	if total != wantTotal {
		t.Fatalf("canonical Go PortDefinition identities=%d, want %d after post-Foundation-B amendments",
			total, wantTotal)
	}
}

func validateStaticGoPortConfig(literal *ast.CompositeLit, identifiers map[string]*ast.CompositeLit) error {
	fields := astKeyedFields(literal)
	config, ok := fields["Config"].(*ast.CompositeLit)
	if !ok {
		if identifier, identifierOK := fields["Config"].(*ast.Ident); identifierOK {
			config, ok = identifiers[identifier.Name]
		}
	}
	if !ok {
		// Dynamic config expressions are covered through production resolution tests.
		return nil
	}
	configType := astTypeName(config.Type)
	configFields := astKeyedFields(config)
	requiredAny := map[string][]string{
		"NetworkPort":      {"Protocol", "Port"},
		"FilePort":         {"Path"},
		"HTTPClientPort":   {"Method", "URLPattern"},
		"NATSPort":         {"Subject"},
		"NATSRequestPort":  {"Subject"},
		"KVWatchPort":      {"Bucket"},
		"KVReadPort":       {"Bucket"},
		"KVWritePort":      {"Bucket"},
		"StoreReadPort":    {"Bucket"},
		"StoreProvidePort": {"Instance"},
	}
	if configType == "JetStreamPort" {
		if configFields["StreamName"] == nil && configFields["Subjects"] == nil {
			return fmt.Errorf("JetStreamPort requires StreamName or Subjects")
		}
	} else {
		for _, field := range requiredAny[configType] {
			value := configFields[field]
			if value == nil {
				return fmt.Errorf("%s requires %s", configType, field)
			}
			if literal, ok := value.(*ast.BasicLit); ok && literal.Kind == token.STRING {
				decoded, _ := strconv.Unquote(literal.Value)
				if decoded == "" {
					return fmt.Errorf("%s.%s must not be empty", configType, field)
				}
			}
		}
	}
	interfaceExpression := configFields["Interface"]
	if interfaceExpression == nil {
		return nil
	}
	pointer, ok := interfaceExpression.(*ast.UnaryExpr)
	if !ok || pointer.Op != token.AND {
		return nil
	}
	contract, ok := pointer.X.(*ast.CompositeLit)
	if !ok || astTypeName(contract.Type) != "InterfaceContract" {
		return nil
	}
	typeExpression := astKeyedFields(contract)["Type"]
	if typeExpression == nil {
		return fmt.Errorf("present InterfaceContract requires Type")
	}
	if literal, ok := typeExpression.(*ast.BasicLit); ok && literal.Kind == token.STRING {
		decoded, _ := strconv.Unquote(literal.Value)
		if decoded == "" {
			return fmt.Errorf("present InterfaceContract.Type must not be empty")
		}
	}
	return nil
}

func astKeyedFields(literal *ast.CompositeLit) map[string]ast.Expr {
	fields := map[string]ast.Expr{}
	for _, element := range literal.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		identifier, ok := keyValue.Key.(*ast.Ident)
		if ok {
			fields[identifier.Name] = keyValue.Value
		}
	}
	return fields
}

func isPortDefinitionLiteral(literal *ast.CompositeLit, parent ast.Node) bool {
	if astTypeName(literal.Type) == "PortDefinition" {
		return true
	}
	if literal.Type != nil {
		return false
	}
	container, ok := parent.(*ast.CompositeLit)
	if !ok {
		return false
	}
	array, ok := container.Type.(*ast.ArrayType)
	return ok && astTypeName(array.Elt) == "PortDefinition"
}

func canonicalGoPortIdentity(literal *ast.CompositeLit, identifiers map[string]*ast.CompositeLit) (string, string, string, bool) {
	fields := map[string]ast.Expr{}
	for _, element := range literal.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		identifier, ok := keyValue.Key.(*ast.Ident)
		if ok {
			fields[identifier.Name] = keyValue.Value
		}
	}
	config, exists := fields["Config"]
	if !exists || fields["Name"] == nil {
		return "", "", "", false
	}
	configType := ""
	switch value := config.(type) {
	case *ast.CompositeLit:
		configType = astTypeName(value.Type)
	case *ast.CallExpr:
		if identifier, ok := value.Fun.(*ast.Ident); ok && identifier.Name == "networkPortFromBindAddress" {
			configType = "NetworkPort"
		}
	case *ast.Ident:
		if resolved := identifiers[value.Name]; resolved != nil {
			configType = astTypeName(resolved.Type)
		}
	}
	if !isCanonicalConfigType(configType) {
		return "", "", "", false
	}
	name := "<dynamic>"
	if literal, ok := fields["Name"].(*ast.BasicLit); ok && literal.Kind == token.STRING {
		if decoded, err := strconv.Unquote(literal.Value); err == nil {
			name = decoded
		}
	}
	for _, retired := range []string{"Type", "Subject", "Interface", "Timeout", "StreamName", "Bucket"} {
		if fields[retired] != nil {
			return name, configType, retired, true
		}
	}
	return name, configType, "", true
}

func localCanonicalConfigLiterals(function *ast.FuncDecl) map[string]*ast.CompositeLit {
	result := map[string]*ast.CompositeLit{}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.AssignStmt:
			for index, expression := range declaration.Rhs {
				if index >= len(declaration.Lhs) {
					break
				}
				name, ok := declaration.Lhs[index].(*ast.Ident)
				literal, literalOK := expression.(*ast.CompositeLit)
				if ok && literalOK && isCanonicalConfigType(astTypeName(literal.Type)) {
					result[name.Name] = literal
				}
			}
		case *ast.ValueSpec:
			for index, expression := range declaration.Values {
				if index >= len(declaration.Names) {
					break
				}
				literal, ok := expression.(*ast.CompositeLit)
				if ok && isCanonicalConfigType(astTypeName(literal.Type)) {
					result[declaration.Names[index].Name] = literal
				}
			}
		}
		return true
	})
	return result
}

func astTypeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	default:
		return ""
	}
}

func isCanonicalConfigType(name string) bool {
	for _, candidate := range []string{
		"TimerPort", "NetworkPort", "FilePort", "HTTPClientPort", "NATSPort", "NATSRequestPort",
		"JetStreamPort", "KVWatchPort", "KVReadPort", "KVWritePort", "StoreReadPort", "StoreProvidePort",
	} {
		if name == candidate {
			return true
		}
	}
	return false
}

func targetConfigType(kind string) string {
	return map[string]string{
		"file": "FilePort", "http": "NetworkPort", "jetstream": "JetStreamPort", "kv-watch": "KVWatchPort",
		"kv-write": "KVWritePort", "nats": "NATSPort", "nats-request": "NATSRequestPort", "network": "NetworkPort",
		"store-read": "StoreReadPort",
	}[kind]
}

func multisetDifference(want, got map[string]int) string {
	keys := map[string]struct{}{}
	for key := range want {
		keys[key] = struct{}{}
	}
	for key := range got {
		keys[key] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	parts := make([]string, 0)
	for _, key := range sorted {
		if want[key] != got[key] {
			parts = append(parts, key+"="+strconv.Itoa(got[key])+"/"+strconv.Itoa(want[key]))
		}
	}
	return strings.Join(parts, ", ")
}

// foundationBTrajectoryOverrideRetirements is the narrow owner-approved
// amendment to the immutable worklist. These complete named overrides would
// erase required/interface facts now owned by agentic-loop's default contract.
var foundationBTrajectoryOverrideRetirements = map[string]struct{}{
	"config:configs/agentic.json#/components/agentic-loop/config/ports/kv_write/1":                  {},
	"config:configs/flows/crud-tools-test.json#/components/agentic-loop/config/ports/kv_write/1":    {},
	"config:configs/flows/deep-research-test.json#/components/agentic-loop/config/ports/kv_write/1": {},
	"config:configs/flows/deep-research.json#/components/agentic-loop/config/ports/kv_write/1":      {},
	"config:configs/flows/lesson-example.json#/components/agentic-loop/config/ports/kv_write/1":     {},
	"config:configs/flows/ops-agent-test.json#/components/agentic-loop/config/ports/kv_write/1":     {},
	"config:configs/flows/ops-agent.json#/components/agentic-loop/config/ports/kv_write/1":          {},
}

type postFoundationBGraphQueryPortIdentity struct {
	path     string
	instance string
	factory  string
	lane     string
	name     string
	subject  string
}

func (i postFoundationBGraphQueryPortIdentity) testName() string {
	return strings.ReplaceAll(i.path+"/"+i.instance+"/"+i.lane+"/"+i.name, "/", "_")
}

func (i postFoundationBGraphQueryPortIdentity) direction() component.Direction {
	if i.lane == "outputs" {
		return component.DirectionOutput
	}
	return component.DirectionInput
}

var postFoundationBGraphQueryProviderReplacements = []postFoundationBGraphQueryPortIdentity{
	{path: "configs/e2e-structural.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/examples/research-graph-pipeline.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/graph-backend.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/hello-world.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/protocol-flow.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/research-graph-e2e.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/semantic-8b.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/semantic-frontier.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/semantic.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/statistical.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
	{path: "configs/structural.json", instance: "graph-query", factory: "graph-query", lane: "inputs", name: "graph_queries", subject: "graph.query.*"},
}

var postFoundationBResearchQueryRawAdditions = []postFoundationBGraphQueryPortIdentity{
	{path: "configs/examples/research-graph-pipeline.json", instance: "research-graph-classify", factory: "research-graph-classify", lane: "outputs", name: "searchGraph", subject: "graph.query.searchGraph"},
	{path: "configs/examples/research-graph-pipeline.json", instance: "research-graph-execute", factory: "research-graph-execute", lane: "outputs", name: "batch", subject: "graph.query.batch"},
	{path: "configs/examples/research-graph-pipeline.json", instance: "research-graph-execute", factory: "research-graph-execute", lane: "outputs", name: "relationships", subject: "graph.query.relationships"},
	{path: "configs/examples/research-graph-pipeline.json", instance: "research-graph-execute", factory: "research-graph-execute", lane: "outputs", name: "temporal", subject: "graph.query.temporal"},
	{path: "configs/examples/research-graph-pipeline.json", instance: "research-graph-execute", factory: "research-graph-execute", lane: "outputs", name: "searchGraph", subject: "graph.query.searchGraph"},
	{path: "configs/research-graph-e2e.json", instance: "research-graph-classify", factory: "research-graph-classify", lane: "outputs", name: "searchGraph", subject: "graph.query.searchGraph"},
	{path: "configs/research-graph-e2e.json", instance: "research-graph-execute", factory: "research-graph-execute", lane: "outputs", name: "batch", subject: "graph.query.batch"},
	{path: "configs/research-graph-e2e.json", instance: "research-graph-execute", factory: "research-graph-execute", lane: "outputs", name: "relationships", subject: "graph.query.relationships"},
	{path: "configs/research-graph-e2e.json", instance: "research-graph-execute", factory: "research-graph-execute", lane: "outputs", name: "temporal", subject: "graph.query.temporal"},
	{path: "configs/research-graph-e2e.json", instance: "research-graph-execute", factory: "research-graph-execute", lane: "outputs", name: "searchGraph", subject: "graph.query.searchGraph"},
}

// postFoundationBGraphQueryLegacyInputRetirements records the approved clean
// cutover from the frozen graph.query.> identities to one required, versioned
// graph_queries graph.query.* provider row in each shipped graph-query config.
// The immutable Foundation B worklist remains historical evidence.
var postFoundationBGraphQueryLegacyInputRetirements = map[string]struct{}{
	"config:configs/e2e-structural.json#/components/graph-query/config/ports/inputs/0":                   {},
	"config:configs/examples/research-graph-pipeline.json#/components/graph-query/config/ports/inputs/0": {},
	"config:configs/graph-backend.json#/components/graph-query/config/ports/inputs/0":                    {},
	"config:configs/hello-world.json#/components/graph-query/config/ports/inputs/0":                      {},
	"config:configs/protocol-flow.json#/components/graph-query/config/ports/inputs/0":                    {},
	"config:configs/research-graph-e2e.json#/components/graph-query/config/ports/inputs/0":               {},
	"config:configs/semantic-8b.json#/components/graph-query/config/ports/inputs/0":                      {},
	"config:configs/semantic-frontier.json#/components/graph-query/config/ports/inputs/0":                {},
	"config:configs/semantic.json#/components/graph-query/config/ports/inputs/0":                         {},
	"config:configs/statistical.json#/components/graph-query/config/ports/inputs/0":                      {},
	"config:configs/structural.json#/components/graph-query/config/ports/inputs/0":                       {},
}

// postFoundationBGraphGatewayInterfaceAmendments records the eight existing
// graph_queries rows that gained the graph.query/v1 interface without changing
// row count or subject coverage.
var postFoundationBGraphGatewayInterfaceAmendments = map[string]struct{}{
	"config:configs/e2e-structural.json#/components/graph-gateway/config/ports/outputs/0":    {},
	"config:configs/hello-world.json#/components/graph-gateway/config/ports/outputs/0":       {},
	"config:configs/protocol-flow.json#/components/graph-gateway/config/ports/outputs/0":     {},
	"config:configs/semantic-8b.json#/components/graph-gateway/config/ports/outputs/0":       {},
	"config:configs/semantic-frontier.json#/components/graph-gateway/config/ports/outputs/0": {},
	"config:configs/semantic.json#/components/graph-gateway/config/ports/outputs/0":          {},
	"config:configs/statistical.json#/components/graph-gateway/config/ports/outputs/0":       {},
	"config:configs/structural.json#/components/graph-gateway/config/ports/outputs/0":        {},
}

// postFoundationBGraphQueryGoIdentityRetirements replaces four exact-operation
// defaults with the single versioned provider family. These IDs remain in the
// immutable Go worklist as historical Foundation B evidence.
var postFoundationBGraphQueryGoIdentityRetirements = map[string]struct{}{
	"go:processor/graph-query/component.go#L94C5": {},
	"go:processor/graph-query/component.go#L95C5": {},
	"go:processor/graph-query/component.go#L96C5": {},
	"go:processor/graph-query/component.go#L97C5": {},
}

// postFoundationBGraphQueryGoIdentityAdditions are the constructions visible
// to the frozen AST census after the approved cutover: the provider family,
// its subject-derivation definition, and classify's exact searchGraph output.
// Execute's four outputs use its graphQueryOutput helper and are validated by
// production factory/config tests rather than this literal-only AST census.
var postFoundationBGraphQueryGoIdentityAdditions = map[string][]string{
	"processor/graph-query/component.go":          {"<dynamic>|NATSRequestPort"},
	"processor/graph-query/query.go":              {"<dynamic>|NATSRequestPort"},
	"processor/research-graph-classify/config.go": {"searchGraph|NATSRequestPort"},
}

// postFoundationBToolDiscoveryGoIdentityRetirements records the exact frozen
// ordinary-NATS discovery identity superseded by the approved request/reply
// cutover. The immutable Go worklist remains historical evidence.
var postFoundationBToolDiscoveryGoIdentityRetirements = map[string]struct{}{
	"go:processor/agentic-tools/config.go#L146C3": {},
}

// postFoundationBToolDiscoveryGoIdentityAdditions records the one current AST
// identity that replaces the frozen tool.list row.
var postFoundationBToolDiscoveryGoIdentityAdditions = map[string][]string{
	"processor/agentic-tools/config.go": {"tool.list|NATSRequestPort"},
}

// postFoundationBUserResponseGoIdentityRetirements records the governance
// user-notification port removed by the owner-approved #952 subject-ownership
// cut. The immutable Foundation B worklist remains historical evidence.
var postFoundationBUserResponseGoIdentityRetirements = map[string]struct{}{
	"go:processor/agentic-governance/config.go#L248C3": {},
}

func targetForConfigItem(item WorkItem, dispositions map[string]Disposition) (targetConfigItem, error) {
	var legacy map[string]any
	if err := json.Unmarshal([]byte(item.CurrentData), &legacy); err != nil {
		return targetConfigItem{}, err
	}
	if _, retired := foundationBTrajectoryOverrideRetirements[item.RecordID]; retired {
		return targetConfigItem{workItem: item, deleted: true}, nil
	}
	if _, retired := postFoundationBGraphQueryLegacyInputRetirements[item.RecordID]; retired {
		return targetConfigItem{workItem: item, deleted: true}, nil
	}
	if item.Enclosing == "graph-gateway" {
		if item.Lane == "inputs" {
			return targetConfigItem{workItem: item, deleted: true}, nil
		}
		legacy["name"] = "graph_queries"
		legacy["required"] = true
		data := map[string]any{"subject": "graph.query.*"}
		if _, amended := postFoundationBGraphGatewayInterfaceAmendments[item.RecordID]; amended {
			data["interface"] = map[string]any{"type": "graph.query", "version": "v1"}
		}
		return targetConfigItem{
			workItem: item,
			lane:     "outputs",
			row:      canonicalRow(legacy, "nats-request", data),
		}, nil
	}
	if item.Classification == "adjudicated" {
		disposition := dispositions[item.RecordID]
		if disposition.Action == "delete" {
			return targetConfigItem{workItem: item, deleted: true}, nil
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(disposition.TargetData), &data); err != nil {
			return targetConfigItem{}, err
		}
		return correctJetStreamInputIdentity(targetConfigItem{
			workItem: item,
			lane:     disposition.TargetLane,
			row:      canonicalRow(legacy, disposition.TargetKind, data),
		})
	}

	lane := item.Lane
	if lane == "kv_write" {
		lane = "outputs"
	}
	data, err := mechanicalData(legacy, item.CurrentKind)
	if err != nil {
		return targetConfigItem{}, err
	}
	return correctComponentPortName(correctJetStreamInputIdentity(correctMissionCommandPrimitive(targetConfigItem{
		workItem: item,
		lane:     lane,
		row:      canonicalRow(legacy, item.CurrentKind, data),
	})))
}

func correctMissionCommandPrimitive(target targetConfigItem) targetConfigItem {
	if target.workItem.Path != "configs/lifecycle-flow.json" ||
		target.workItem.Enclosing != "mission-command" || target.row == nil {
		return target
	}
	config, ok := target.row["config"].(map[string]any)
	if !ok || stringValue(config["kind"]) != "jetstream" {
		return target
	}
	subjects, ok := config["subjects"].([]any)
	if !ok || len(subjects) != 1 {
		return target
	}
	data := map[string]any{"subject": stringValue(subjects[0])}
	if contract, exists := config["interface"]; exists {
		data["interface"] = contract
	}
	target.row = canonicalRow(target.row, "nats", data)
	target.primitiveCorrected = true
	return target
}

func correctComponentPortName(target targetConfigItem, err error) (targetConfigItem, error) {
	if err != nil {
		return targetConfigItem{}, err
	}
	if target.workItem.Enclosing == "udp" && target.lane == "outputs" &&
		stringValue(target.row["name"]) == "udp_out" {
		target.row["name"] = "nats_output"
		target.portNameCorrected = true
	}
	if target.workItem.Enclosing == "agentic-dispatch" && target.lane == "outputs" &&
		stringValue(target.row["name"]) == "user.response" {
		config, ok := target.row["config"].(map[string]any)
		if !ok || stringValue(config["kind"]) != "nats" || stringValue(config["subject"]) == "" {
			return targetConfigItem{}, fmt.Errorf("agentic-dispatch user.response %s has no core NATS subject", target.workItem.RecordID)
		}
		target.row = canonicalRow(target.row, "jetstream", map[string]any{
			"stream_name": "USER",
			"subjects":    []any{stringValue(config["subject"])},
			"interface": map[string]any{
				"type": "agentic.user_response", "version": "v1",
			},
		})
		target.primitiveCorrected = true
	}
	return target, nil
}

var foundationBInputStreamBySubject = map[string]string{
	"cloud.federated.data":      "CLOUD",
	"document.processed.entity": "DOCUMENT",
	"edge.filtered.data":        "EDGE",
	"edge.raw.data":             "EDGE",
	"entity.>":                  "ENTITY",
	"events.entity.>":           "EVENTS",
	"filtered.messages":         "FILTERED",
	"generic.messages":          "GENERIC",
	"mapped.messages":           "MAPPED",
	"mission.processed.entity":  "MISSION",
	"objectstore.stored.entity": "OBJECTSTORE",
	"raw.document.corpus":       "RAW",
	"raw.mission.command":       "RAW",
	"raw.sensor.>":              "RAW",
	"raw.udp.messages":          "RAW",
	"sensor.processed.entity":   "SENSOR",
}

func correctJetStreamInputIdentity(target targetConfigItem) (targetConfigItem, error) {
	if target.lane != "inputs" || target.row == nil {
		return target, nil
	}
	config, ok := target.row["config"].(map[string]any)
	if !ok || stringValue(config["kind"]) != "jetstream" || stringValue(config["stream_name"]) != "" {
		return target, nil
	}
	subjects, ok := config["subjects"].([]any)
	if !ok || len(subjects) == 0 {
		return targetConfigItem{}, fmt.Errorf("JetStream input %s has no subjects for identity correction", target.workItem.RecordID)
	}
	subject := stringValue(subjects[0])
	streamName, ok := foundationBInputStreamBySubject[subject]
	if !ok {
		return targetConfigItem{}, fmt.Errorf("JetStream input %s subject %q has no approved backing stream", target.workItem.RecordID, subject)
	}
	config["stream_name"] = streamName
	target.inputIdentityCorrected = true
	return target, nil
}

func namedRows(value any, name string) []map[string]any {
	rows, _ := value.([]any)
	result := make([]map[string]any, 0, 1)
	for _, value := range rows {
		row, ok := value.(map[string]any)
		if ok && stringValue(row["name"]) == name {
			result = append(result, row)
		}
	}
	return result
}

func assertProductionPortResolution(t *testing.T, identity, lane string, row map[string]any) {
	t.Helper()
	wire := make(map[string]any, len(row)+1)
	for key, value := range row {
		wire[key] = value
	}
	if lane == "inputs" {
		wire["direction"] = component.DirectionInput
	} else {
		wire["direction"] = component.DirectionOutput
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var port component.Port
	if err := json.Unmarshal(data, &port); err != nil {
		t.Errorf("production resolver rejected %s: %v", identity, err)
		return
	}
	facts, err := port.Facts()
	if err != nil {
		t.Errorf("production facts rejected %s: %v", identity, err)
		return
	}
	if contract, ok := facts.Interface(); ok && contract.Type == graphmutation.InterfaceType && contract.Version != graphmutation.InterfaceVersion {
		t.Errorf("graph mutation interface %s version=%q, want %q", identity, contract.Version, graphmutation.InterfaceVersion)
	}
}

func splitTargetParentIdentity(identity string) (string, string, bool) {
	for index := len(identity) - 1; index >= 0; index-- {
		if identity[index] == '#' {
			return identity[:index], identity[index+1:], true
		}
	}
	return "", "", false
}

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
	"github.com/c360studio/semstreams/internal/graphmutation"
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
	}, root, plan, documents, portsParents, graphGatewayParents)
}

type targetAccounting struct {
	survivors                int
	deletions                int
	inputIdentityCorrections int
	portNameCorrections      int
	primitiveCorrections     int
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
	if accounting.survivors != 505 || accounting.deletions != 17 {
		t.Fatalf("target accounting: survivors=%d deletions=%d, want 505 and 17",
			accounting.survivors, accounting.deletions)
	}
	if accounting.inputIdentityCorrections != 61 {
		t.Fatalf("JetStream input identity corrections=%d, want 61", accounting.inputIdentityCorrections)
	}
	if accounting.portNameCorrections != 11 {
		t.Fatalf("component-default port name corrections=%d, want 11", accounting.portNameCorrections)
	}
	if accounting.primitiveCorrections != 8 {
		t.Fatalf("component-default primitive corrections=%d, want 8", accounting.primitiveCorrections)
	}
	actualRows := countCanonicalConfigRows(t, documents, portsParents)
	if actualRows != 522 {
		t.Fatalf("canonical config rows=%d, want 522", actualRows)
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
		}
	}
}

func assertGoTargetCompleteness(t *testing.T, root string, plan *Plan) {
	t.Helper()
	wantByPath := map[string]map[string]int{}
	for _, item := range plan.GoItems() {
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
	if total != 137 {
		t.Fatalf("canonical Go PortDefinition identities=%d, want 137", total)
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

func targetForConfigItem(item WorkItem, dispositions map[string]Disposition) (targetConfigItem, error) {
	var legacy map[string]any
	if err := json.Unmarshal([]byte(item.CurrentData), &legacy); err != nil {
		return targetConfigItem{}, err
	}
	if _, retired := foundationBTrajectoryOverrideRetirements[item.RecordID]; retired {
		return targetConfigItem{workItem: item, deleted: true}, nil
	}
	if item.Enclosing == "graph-gateway" {
		if item.Lane == "inputs" {
			return targetConfigItem{workItem: item, deleted: true}, nil
		}
		legacy["name"] = "graph_queries"
		legacy["required"] = true
		return targetConfigItem{
			workItem: item,
			lane:     "outputs",
			row: canonicalRow(legacy, "nats-request", map[string]any{
				"subject": "graph.query.*",
			}),
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
	return correctComponentPortName(correctJetStreamInputIdentity(targetConfigItem{
		workItem: item,
		lane:     lane,
		row:      canonicalRow(legacy, item.CurrentKind, data),
	}))
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

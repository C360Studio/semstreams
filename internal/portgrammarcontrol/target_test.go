package portgrammarcontrol

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
)

type targetConfigItem struct {
	workItem WorkItem
	lane     string
	row      map[string]any
	deleted  bool
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
	survivors := 0
	deletions := 0
	for _, target := range targets {
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
		matches := namedRows(ports[target.lane], target.workItem.Name)
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

	if survivors != 520 || deletions != 2 {
		t.Fatalf("target accounting: survivors=%d deletions=%d, want 520 and 2", survivors, deletions)
	}
	actualRows := 0
	for identity := range portsParents {
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
	if actualRows != 520 {
		t.Fatalf("canonical config rows=%d, want 520", actualRows)
	}
	if len(plan.GoItems()) != 124 {
		t.Fatalf("frozen Go identities=%d, want 124", len(plan.GoItems()))
	}
	assertGoTargetCompleteness(t, root, plan)
}

func assertGoTargetCompleteness(t *testing.T, root string, plan *Plan) {
	t.Helper()
	wantByPath := map[string]map[string]int{}
	for _, item := range plan.GoItems() {
		if wantByPath[item.Path] == nil {
			wantByPath[item.Path] = map[string]int{}
		}
		wantByPath[item.Path][item.Name+"|"+targetConfigType(item.CurrentKind)]++
	}
	approved := map[string][]string{
		"processor/graph-clustering/component.go": {
			"entity_states|KVReadPort", "outgoing_index|KVReadPort", "incoming_index|KVReadPort",
		},
		"processor/agentic-tools/config.go": {
			"entity_states|KVReadPort", "agent_loops|KVReadPort",
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
	if total != 135 {
		t.Fatalf("canonical Go PortDefinition identities=%d, want 135 (124 frozen + 11 approved)", total)
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
		// The graph-gateway bind-address helper returns a NetworkPort and is
		// covered through its production resolution tests.
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

func targetForConfigItem(item WorkItem, dispositions map[string]Disposition) (targetConfigItem, error) {
	var legacy map[string]any
	if err := json.Unmarshal([]byte(item.CurrentData), &legacy); err != nil {
		return targetConfigItem{}, err
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
		return targetConfigItem{workItem: item, lane: disposition.TargetLane, row: canonicalRow(legacy, disposition.TargetKind, data)}, nil
	}

	lane := item.Lane
	if lane == "kv_write" {
		lane = "outputs"
	}
	data, err := mechanicalData(legacy, item.CurrentKind)
	if err != nil {
		return targetConfigItem{}, err
	}
	return targetConfigItem{workItem: item, lane: lane, row: canonicalRow(legacy, item.CurrentKind, data)}, nil
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

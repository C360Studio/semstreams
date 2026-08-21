package portgrammarcontrol

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const productionRendererCount = 76

var retiredPortKinds = map[string]struct{}{
	"nats": {}, "nats-request": {}, "jetstream": {}, "kv-watch": {},
	"kv-read": {}, "kv-write": {}, "object-store": {}, "store-read": {},
	"timer": {}, "network": {}, "http-client": {},
	"kv": {}, "kvwatch": {}, "kvwrite": {}, "http": {}, "grpc": {},
	"websocket-server": {},
}

var retiredCanonicalKindAliases = map[string]struct{}{
	"kv": {}, "kvwatch": {}, "kvwrite": {}, "http": {}, "grpc": {},
	"websocket-server": {},
}

var retiredNamedPortJSON = regexp.MustCompile(`(?s)\{[^{}]*(?:"name"\s*:\s*"[^"]+"[^{}]*"type"\s*:\s*"(?:nats|nats-request|jetstream|kv-watch|kv-read|kv-write|object-store|store-read|timer|network|http-client|kv|kvwatch|kvwrite|http|grpc|websocket-server)"|"type"\s*:\s*"(?:nats|nats-request|jetstream|kv-watch|kv-read|kv-write|object-store|store-read|timer|network|http-client|kv|kvwatch|kvwrite|http|grpc|websocket-server)"[^{}]*"name"\s*:\s*"[^"]+")[^{}]*\}`)

var retiredGoPortExample = regexp.MustCompile(`(?s)PortDefinition\s*\{[^{}]*(?:Type|Subject|StreamName|Bucket)\s*:`)

var portableConfigTypes = map[string]struct{}{
	"TimerPort": {}, "NetworkPort": {}, "FilePort": {}, "HTTPClientPort": {},
	"NATSPort": {}, "NATSRequestPort": {}, "JetStreamPort": {},
	"KVWatchPort": {}, "KVReadPort": {}, "KVWritePort": {},
	"StoreReadPort": {}, "StoreProvidePort": {},
}

var canonicalPortProjectionOwners = map[string]struct{}{
	"component/port_codec.go": {},
	"component/port_facts.go": {},
}

// TestRuntimePortGrammarCompleteness keeps the renderer migration closed over
// the repository. The frozen inventory counted 76 shipped InputPorts and
// OutputPorts methods; component/test_helpers.go is test scaffolding rather
// than a discoverable production component.
func TestRuntimePortGrammarCompleteness(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	renderers := make([]string, 0, productionRendererCount)
	violations := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !isActivePortDocumentation(relative) {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if retiredNamedPortJSON.Match(source) {
			violations = append(violations, relative+" documents a retired flat JSON port")
		}
		if strings.HasSuffix(relative, "doc.go") && retiredGoPortExample.Match(source) {
			violations = append(violations, relative+" documents retired flat Go PortDefinition fields")
		}
		if strings.Contains(string(source), "GetConsumerConfigFromDefinition") {
			violations = append(violations, relative+" documents a deleted consumer-config API")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fileSet, path, source, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", relative, err)
		}
		sourceLines := strings.Split(string(source), "\n")

		production := !strings.HasSuffix(relative, "_test.go")
		if production && relative != "component/test_helpers.go" {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || !isPortRenderer(function) {
					continue
				}
				renderers = append(renderers, fmt.Sprintf("%s:%d %s", relative, fileSet.Position(function.Pos()).Line, function.Name.Name))
				ast.Inspect(function.Body, func(node ast.Node) bool {
					literal, ok := node.(*ast.CompositeLit)
					if ok && isRuntimePortLiteral(literal) {
						violations = append(violations, fmt.Sprintf("%s:%d %s constructs a runtime Port literal", relative, fileSet.Position(literal.Pos()).Line, function.Name.Name))
					}
					return true
				})
			}
		}
		if production && strings.Contains(string(source), "GetConsumerConfigFromDefinition") {
			violations = append(violations, fmt.Sprintf("%s retains deleted consumer-config API name", relative))
		}

		var stack []ast.Node
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

			position := fileSet.Position(node.Pos())
			switch value := node.(type) {
			case *ast.CallExpr:
				if calledName(value.Fun) == "BuildPortFromDefinition" {
					violations = append(violations, fmt.Sprintf("%s:%d calls retired BuildPortFromDefinition", relative, position.Line))
				}
			case *ast.CompositeLit:
				if isLegacyPortMapLiteral(value, parent) {
					violations = append(violations, fmt.Sprintf("%s:%d map fixture retains legacy top-level port type", relative, position.Line))
				}
				if astTypeName(value.Type) == "InterfaceContract" && qualifiedSelector(astKeyedFields(value)["Type"]) == "graphmutation.InterfaceType" && qualifiedSelector(astKeyedFields(value)["Version"]) != "graphmutation.InterfaceVersion" {
					violations = append(violations, fmt.Sprintf("%s:%d graph mutation interface omits canonical version", relative, position.Line))
				}
				if !isPortDefinitionLiteral(value, parent) {
					break
				}
				for _, field := range []string{"Type", "Subject", "Interface", "Timeout", "StreamName", "Bucket"} {
					if astKeyedFields(value)[field] != nil {
						violations = append(violations, fmt.Sprintf("%s:%d PortDefinition retains flat field %s", relative, position.Line, field))
					}
				}
			case *ast.TypeSwitchStmt:
				if production && typeSwitchesPortConfig(value) && !isCanonicalProjectionOwner(relative) {
					violations = append(violations, fmt.Sprintf("%s:%d type-switches a port Config projection", relative, position.Line))
				}
			case *ast.TypeAssertExpr:
				if production && value.Type != nil && isPortableConfigType(astTypeName(value.Type)) && !isCanonicalProjectionOwner(relative) {
					violations = append(violations, fmt.Sprintf("%s:%d directly asserts concrete port config %s", relative, position.Line, astTypeName(value.Type)))
				}
			case *ast.Ident:
				if value.Name == "NATSStreamPortConfig" || value.Name == "NATSRequestPortConfig" {
					violations = append(violations, fmt.Sprintf("%s:%d references retired type %s", relative, position.Line, value.Name))
				}
			case *ast.BasicLit:
				if value.Kind == token.STRING && legacyPortTypeInString(value.Value) && !legacyFixtureMarked(sourceLines, position.Line) {
					violations = append(violations, fmt.Sprintf("%s:%d string fixture retains legacy top-level port type", relative, position.Line))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(renderers)
	sort.Strings(violations)
	if len(renderers) != productionRendererCount {
		t.Errorf("production InputPorts/OutputPorts methods=%d, want frozen count %d:\n%s", len(renderers), productionRendererCount, strings.Join(renderers, "\n"))
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func isActivePortDocumentation(relative string) bool {
	if filepath.Ext(relative) != ".md" && !strings.HasSuffix(relative, "doc.go") {
		return false
	}
	for _, historical := range []string{"docs/adr/", "docs/proposals/", "docs/audits/", "openspec/changes/archive/"} {
		if strings.HasPrefix(relative, historical) {
			return false
		}
	}
	return true
}

func isLegacyPortMapLiteral(literal *ast.CompositeLit, parent ast.Node) bool {
	if !isStringMapLiteral(literal, parent) {
		return false
	}
	fields := map[string]ast.Expr{}
	for _, element := range literal.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := stringLiteral(keyValue.Key)
		if ok {
			fields[key] = keyValue.Value
		}
	}
	if fields["name"] == nil {
		return false
	}
	kind, ok := stringLiteral(fields["type"])
	return ok && isLegacyPortKind(kind)
}

func isStringMapLiteral(literal *ast.CompositeLit, parent ast.Node) bool {
	if mapType, ok := literal.Type.(*ast.MapType); ok {
		return astTypeName(mapType.Key) == "string"
	}
	container, ok := parent.(*ast.CompositeLit)
	if !ok {
		return false
	}
	array, ok := container.Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	mapType, ok := array.Elt.(*ast.MapType)
	return ok && astTypeName(mapType.Key) == "string"
}

func legacyPortTypeInString(quoted string) bool {
	value, err := strconv.Unquote(quoted)
	if err != nil || !strings.Contains(value, `"name"`) {
		return false
	}
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) != nil {
		return false
	}
	return containsLegacyPortObject(decoded)
}

func legacyFixtureMarked(lines []string, line int) bool {
	for _, candidate := range []int{line - 3, line - 2, line - 1} {
		if candidate >= 0 && candidate < len(lines) && strings.Contains(lines[candidate], "port-grammar:legacy-fixture") {
			return true
		}
	}
	return false
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func isLegacyPortKind(kind string) bool {
	_, ok := retiredPortKinds[kind]
	return ok
}

func containsLegacyPortObject(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		_, hasName := typed["name"]
		kind, hasType := typed["type"].(string)
		if hasName && hasType && isLegacyPortKind(kind) {
			return true
		}
		if hasName {
			if config, ok := typed["config"].(map[string]any); ok {
				if kind, ok := config["kind"].(string); ok {
					if _, retired := retiredCanonicalKindAliases[kind]; retired {
						return true
					}
				}
			}
		}
		for _, child := range typed {
			if containsLegacyPortObject(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsLegacyPortObject(child) {
				return true
			}
		}
	}
	return false
}

func isCanonicalProjectionOwner(path string) bool {
	_, ok := canonicalPortProjectionOwners[path]
	return ok
}

func isPortableConfigType(name string) bool {
	_, ok := portableConfigTypes[name]
	return ok
}

func isRuntimePortLiteral(literal *ast.CompositeLit) bool {
	if astTypeName(literal.Type) == "Port" {
		return true
	}
	array, ok := literal.Type.(*ast.ArrayType)
	return ok && astTypeName(array.Elt) == "Port"
}

func isPortRenderer(function *ast.FuncDecl) bool {
	if function.Recv == nil || function.Type.Results == nil || len(function.Type.Results.List) != 1 {
		return false
	}
	if function.Name.Name != "InputPorts" && function.Name.Name != "OutputPorts" {
		return false
	}
	result, ok := function.Type.Results.List[0].Type.(*ast.ArrayType)
	return ok && astTypeName(result.Elt) == "Port"
}

func calledName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	default:
		return ""
	}
}

func selectorName(expression ast.Expr) string {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return selector.Sel.Name
}

func qualifiedSelector(expression ast.Expr) string {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return qualifier.Name + "." + selector.Sel.Name
}

func typeSwitchesPortConfig(statement *ast.TypeSwitchStmt) bool {
	var assertion *ast.TypeAssertExpr
	switch assign := statement.Assign.(type) {
	case *ast.AssignStmt:
		if len(assign.Rhs) == 1 {
			assertion, _ = assign.Rhs[0].(*ast.TypeAssertExpr)
		}
	case *ast.ExprStmt:
		assertion, _ = assign.X.(*ast.TypeAssertExpr)
	}
	return assertion != nil && selectorName(assertion.X) == "Config"
}

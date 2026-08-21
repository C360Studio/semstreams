package natsclient

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type productionGoFile struct {
	rel  string
	file *ast.File
}

func TestConsumerPolicyProductionCallsiteCensus(t *testing.T) {
	files := parseProductionGoFiles(t, filepath.Clean(".."))
	internalCallers := map[string]int{}
	portCallers := map[string]int{}
	contextsPortCallers := map[string]int{}
	portConfigCallers := map[string]struct{}{}
	portBackedInternalCallers := map[string]struct{}{}
	for _, parsed := range files {
		usesPortConfig := false
		usesInternal := false
		ast.Inspect(parsed.file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "GetConsumerConfig":
				usesPortConfig = true
			case "ConsumeInternalStreamWithConfig":
				usesInternal = true
				internalCallers[parsed.rel]++
			case "ConsumeStreamWithConfig":
				portCallers[parsed.rel]++
			case "ConsumeStreamWithConfigContexts":
				contextsPortCallers[parsed.rel]++
			}
			return true
		})
		if usesPortConfig {
			portConfigCallers[parsed.rel] = struct{}{}
		}
		if usesPortConfig && usesInternal {
			portBackedInternalCallers[parsed.rel] = struct{}{}
		}
	}

	wantInternal := map[string]int{
		"agentic/agentrun/agentrun.go":     2,
		"internal/maxdelivery/observer.go": 1,
	}
	if !reflect.DeepEqual(internalCallers, wantInternal) {
		t.Fatalf("internal consumer census = %#v, want %#v", internalCallers, wantInternal)
	}
	wantPort := map[string]int{
		"examples/processors/document/component.go":   1,
		"examples/processors/iot_sensor/component.go": 1,
		"output/file/file.go":                         1,
		"output/httppost/httppost.go":                 1,
		"output/websocket/websocket.go":               1,
		"processor/agentic-dispatch/component.go":     1,
		"processor/agentic-governance/component.go":   1,
		"processor/agentic-model/component.go":        1,
		"processor/agentic-tools/component.go":        1,
		"processor/graph-ingest/component.go":         1,
		"processor/json_filter/json_filter.go":        1,
		"processor/json_generic/json_generic.go":      1,
		"processor/json_map/json_map.go":              1,
		"processor/rule/processor.go":                 1,
		"storage/objectstore/component.go":            1,
	}
	if !reflect.DeepEqual(portCallers, wantPort) {
		t.Fatalf("canonical port consumer census = %#v, want %#v", portCallers, wantPort)
	}
	wantContextsPort := map[string]int{
		"processor/agentic-loop/component.go": 1,
	}
	if !reflect.DeepEqual(contextsPortCallers, wantContextsPort) {
		t.Fatalf("split-context canonical port census = %#v, want %#v", contextsPortCallers, wantContextsPort)
	}
	if len(portConfigCallers) != 17 {
		t.Fatalf("GetConsumerConfig production files = %d, want 17: %#v", len(portConfigCallers), portConfigCallers)
	}
	if len(portBackedInternalCallers) != 0 {
		t.Fatalf("port-backed files use internal consumer path: %#v", portBackedInternalCallers)
	}
}

func TestConsumerPolicyExportedClientAPICensus(t *testing.T) {
	files := parseProductionGoFiles(t, ".")
	got := map[string]string{}
	for _, parsed := range files {
		for _, declaration := range parsed.file.Decls {
			method, ok := declaration.(*ast.FuncDecl)
			if !ok || method.Recv == nil || !method.Name.IsExported() || !receiverIsClientPointer(method.Recv) {
				continue
			}
			if !strings.HasPrefix(method.Name.Name, "Consume") && method.Name.Name != "ObserveDirectPortConsumerPolicy" {
				continue
			}
			got[method.Name.Name] = compactNode(t, method.Type)
		}
	}

	want := map[string]string{
		"ConsumeInternalStreamWithConfig": "func(ctx context.Context, cfg StreamConsumerConfig, handler func(ctx context.Context, msg jetstream.Msg)) (jetstream.ConsumeContext, error)",
		"ConsumeStreamWithConfig":         "func(ctx context.Context, owner PortConsumerContext, cfg StreamConsumerConfig, handler func(ctx context.Context, msg jetstream.Msg)) (jetstream.ConsumeContext, error)",
		"ConsumeStreamWithConfigContexts": "func(setupCtx context.Context, handlerCtx context.Context, owner PortConsumerContext, cfg StreamConsumerConfig, handler func(ctx context.Context, msg jetstream.Msg)) (jetstream.ConsumeContext, error)",
		"ObserveDirectPortConsumerPolicy": "func(ctx context.Context, owner PortConsumerContext, finalConfig jetstream.ConsumerConfig, consumer jetstream.Consumer) (func(), error)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exported Client consumer API census = %#v, want %#v", got, want)
	}
}

func TestDurableHandlerExportedAPICensus(t *testing.T) {
	files := parseProductionGoFiles(t, ".")
	got := map[string]string{}
	for _, parsed := range files {
		for _, declaration := range parsed.file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "NewDurableHandler" {
				continue
			}
			got[fn.Name.Name] = compactNode(t, fn.Type)
		}
	}
	want := map[string]string{
		"NewDurableHandler": "func(cfg StreamConsumerConfig, heartbeat time.Duration, work func(context.Context, []byte) error) (func(context.Context, jetstream.Msg), error)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("durable handler API census = %#v, want %#v", got, want)
	}
}

func TestClientHasNoChildLifecycleSurfaceOrCatalog(t *testing.T) {
	files := parseProductionGoFiles(t, ".")
	forbiddenMethods := map[string]struct{}{
		"ConsumeDurable": {}, "StopConsumer": {}, "StopAndDeleteConsumer": {},
		"StopAllConsumers": {}, "OutstandingWork": {},
	}
	forbiddenFields := map[string]struct{}{
		"consumers": {}, "consumersMu": {}, "subs": {},
	}
	for _, parsed := range files {
		for _, declaration := range parsed.file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if ok && fn.Recv != nil && receiverIsClientPointer(fn.Recv) {
				if _, forbidden := forbiddenMethods[fn.Name.Name]; forbidden {
					t.Fatalf("forbidden Client lifecycle method remains: %s", fn.Name.Name)
				}
			}
			gen, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != "Client" {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range structType.Fields.List {
					for _, name := range field.Names {
						if _, forbidden := forbiddenFields[name.Name]; forbidden {
							t.Fatalf("forbidden Client child catalog remains: %s", name.Name)
						}
					}
				}
			}
		}
	}
}

func TestConsumerPolicyDirectCreationCallCensus(t *testing.T) {
	files := parseProductionGoFiles(t, filepath.Clean(".."))
	got := map[string]int{}
	for _, parsed := range files {
		ast.Inspect(parsed.file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "CreateOrUpdateConsumer", "CreateConsumer", "OrderedConsumer":
				key := parsed.rel + ":" + selector.Sel.Name + "/args=" + strconv.Itoa(len(call.Args))
				got[key]++
			}
			return true
		})
	}

	want := map[string]int{
		"natsclient/stream.go:CreateOrUpdateConsumer/args=2":                       2,
		"output/otel/component.go:CreateOrUpdateConsumer/args=3":                   1,
		"test/e2e/scenarios/core_objectstore_raw.go:CreateOrUpdateConsumer/args=2": 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("direct consumer creation census = %#v, want %#v", got, want)
	}
}

func parseProductionGoFiles(t *testing.T, root string) []productionGoFile {
	t.Helper()
	files := []productionGoFile{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, productionGoFile{rel: filepath.ToSlash(rel), file: file})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func receiverIsClientPointer(receivers *ast.FieldList) bool {
	if receivers == nil || len(receivers.List) != 1 {
		return false
	}
	pointer, ok := receivers.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	name, ok := pointer.X.(*ast.Ident)
	return ok && name.Name == "Client"
}

func compactNode(t *testing.T, node ast.Node) string {
	t.Helper()
	var output bytes.Buffer
	if err := format.Node(&output, token.NewFileSet(), node); err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(output.String()), " ")
}

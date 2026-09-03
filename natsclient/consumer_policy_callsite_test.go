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

type legacyHeartbeatReferenceScan struct {
	directCalls map[string]int
	violations  []string
}

func newDurableHandlerRetirementViolations(files []productionGoFile) []string {
	var violations []string
	for _, parsed := range files {
		declarationNames := map[*ast.Ident]struct{}{}
		if parsed.file.Name.Name == "natsclient" {
			for _, declaration := range parsed.file.Decls {
				switch declaration := declaration.(type) {
				case *ast.FuncDecl:
					if declaration.Name.Name == "NewDurableHandler" {
						declarationNames[declaration.Name] = struct{}{}
						violations = append(violations, parsed.rel+": function or receiver method")
					}
				case *ast.GenDecl:
					for _, spec := range declaration.Specs {
						switch spec := spec.(type) {
						case *ast.ValueSpec:
							for _, name := range spec.Names {
								if name.Name == "NewDurableHandler" {
									declarationNames[name] = struct{}{}
									violations = append(violations, parsed.rel+": variable or constant alias")
								}
							}
						case *ast.TypeSpec:
							if spec.Name.Name == "NewDurableHandler" {
								declarationNames[spec.Name] = struct{}{}
								violations = append(violations, parsed.rel+": type alias")
							}
						}
					}
				}
			}
		}

		aliases, dotImport := natsclientImportBindings(parsed.file)
		ast.Inspect(parsed.file, func(node ast.Node) bool {
			switch reference := node.(type) {
			case *ast.SelectorExpr:
				if reference.Sel.Name != "NewDurableHandler" {
					return true
				}
				if qualifier, qualified := reference.X.(*ast.Ident); qualified {
					if _, imported := aliases[qualifier.Name]; imported {
						violations = append(violations, parsed.rel+": qualified call or symbol reference")
					}
				}
				return false
			case *ast.Ident:
				if reference.Name != "NewDurableHandler" ||
					(parsed.file.Name.Name != "natsclient" && !dotImport) {
					return true
				}
				if _, declaration := declarationNames[reference]; !declaration {
					violations = append(violations, parsed.rel+": identifier call or symbol reference")
				}
			}
			return true
		})
	}
	return violations
}

func scanLegacyHeartbeatReferences(files []productionGoFile) legacyHeartbeatReferenceScan {
	result := legacyHeartbeatReferenceScan{directCalls: map[string]int{}}
	for _, parsed := range files {
		declarationNames := map[*ast.Ident]struct{}{}
		if parsed.file.Name.Name == "natsclient" {
			for _, declaration := range parsed.file.Decls {
				switch declaration := declaration.(type) {
				case *ast.FuncDecl:
					if declaration.Name.Name != "ConsumeWithHeartbeat" {
						continue
					}
					declarationNames[declaration.Name] = struct{}{}
					if declaration.Recv != nil || parsed.rel != "natsclient/heartbeat.go" {
						result.violations = append(result.violations,
							parsed.rel+": alternate function or receiver method")
					}
				case *ast.GenDecl:
					for _, spec := range declaration.Specs {
						switch spec := spec.(type) {
						case *ast.ValueSpec:
							for _, name := range spec.Names {
								if name.Name == "ConsumeWithHeartbeat" {
									declarationNames[name] = struct{}{}
									result.violations = append(result.violations,
										parsed.rel+": variable or constant alias")
								}
							}
						case *ast.TypeSpec:
							if spec.Name.Name == "ConsumeWithHeartbeat" {
								declarationNames[spec.Name] = struct{}{}
								result.violations = append(result.violations, parsed.rel+": type alias")
							}
						}
					}
				}
			}
		}

		aliases, dotImport := natsclientImportBindings(parsed.file)
		relevantSelector := func(selector *ast.SelectorExpr) bool {
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok || selector.Sel.Name != "ConsumeWithHeartbeat" {
				return false
			}
			_, ok = aliases[qualifier.Name]
			return ok
		}
		relevantIdent := func(identifier *ast.Ident) bool {
			return identifier.Name == "ConsumeWithHeartbeat" &&
				(parsed.file.Name.Name == "natsclient" || dotImport)
		}

		directReferences := map[ast.Node]struct{}{}
		ast.Inspect(parsed.file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch called := call.Fun.(type) {
			case *ast.SelectorExpr:
				if relevantSelector(called) {
					directReferences[called] = struct{}{}
				}
			case *ast.Ident:
				if relevantIdent(called) {
					directReferences[called] = struct{}{}
				}
			}
			return true
		})

		ast.Inspect(parsed.file, func(node ast.Node) bool {
			switch reference := node.(type) {
			case *ast.SelectorExpr:
				if reference.Sel.Name != "ConsumeWithHeartbeat" {
					return true
				}
				if relevantSelector(reference) {
					if _, direct := directReferences[reference]; direct {
						result.directCalls[parsed.rel]++
					} else {
						result.violations = append(result.violations, parsed.rel+": indirect package reference")
					}
				}
				return false
			case *ast.Ident:
				if !relevantIdent(reference) {
					return true
				}
				if _, declaration := declarationNames[reference]; declaration {
					return true
				}
				if _, direct := directReferences[reference]; direct {
					result.directCalls[parsed.rel]++
				} else {
					result.violations = append(result.violations, parsed.rel+": indirect identifier reference")
				}
			}
			return true
		})
	}
	return result
}

func natsclientImportBindings(file *ast.File) (map[string]struct{}, bool) {
	aliases := map[string]struct{}{}
	dotImport := false
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil || importPath != "github.com/c360studio/semstreams/natsclient" {
			continue
		}
		if imported.Name == nil {
			aliases["natsclient"] = struct{}{}
			continue
		}
		switch imported.Name.Name {
		case ".":
			dotImport = true
		case "_":
		default:
			aliases[imported.Name.Name] = struct{}{}
		}
	}
	return aliases, dotImport
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

func TestParseProductionGoFilesIgnoresClaudeWorktrees(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "kept.go"), []byte("package fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, ".claude", "worktrees", "agent-test")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "contamination.go"), []byte("package contamination\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	files := parseProductionGoFiles(t, root)
	if len(files) != 1 || files[0].rel != "kept.go" {
		t.Fatalf("production scan files = %#v, want only kept.go", files)
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

func TestNewDurableHandlerHasNoDeclarationOrProductionCalls(t *testing.T) {
	files := parseProductionGoFiles(t, filepath.Clean(".."))
	if violations := newDurableHandlerRetirementViolations(files); len(violations) != 0 {
		t.Fatalf("retired NewDurableHandler surface remains: %v", violations)
	}
}

func TestNewDurableHandlerRetirementRejectsAliasAndReceiverBypasses(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "exported variable alias",
			source: "package natsclient\nvar NewDurableHandler = func() {}\n",
		},
		{
			name: "receiver method",
			source: "package natsclient\n" +
				"type Client struct{}\n" +
				"func (*Client) NewDurableHandler() {}\n",
		},
		{
			name: "external qualified call",
			source: "package fixture\n" +
				"import nc \"github.com/c360studio/semstreams/natsclient\"\n" +
				"func callRetired() { nc.NewDurableHandler() }\n",
		},
		{
			name: "external default-import symbol",
			source: "package fixture\n" +
				"import \"github.com/c360studio/semstreams/natsclient\"\n" +
				"var retired = natsclient.NewDurableHandler\n",
		},
		{
			name: "external symbol taking",
			source: "package fixture\n" +
				"import nc \"github.com/c360studio/semstreams/natsclient\"\n" +
				"var retired = nc.NewDurableHandler\n",
		},
		{
			name: "external dot-import call",
			source: "package fixture\n" +
				"import . \"github.com/c360studio/semstreams/natsclient\"\n" +
				"func callRetired() { NewDurableHandler() }\n",
		},
		{
			name:   "type alias",
			source: "package natsclient\ntype NewDurableHandler = func()\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "bypass.go"), []byte(tt.source), 0o600); err != nil {
				t.Fatal(err)
			}
			if violations := newDurableHandlerRetirementViolations(parseProductionGoFiles(t, root)); len(violations) == 0 {
				t.Fatal("retirement guard accepted bypass")
			}
		})
	}
}

func TestNewDurableHandlerRetirementIgnoresUnrelatedSelector(t *testing.T) {
	root := t.TempDir()
	source := "package fixture\n" +
		"import other \"example.com/other\"\n" +
		"func callOther() { other.NewDurableHandler() }\n"
	if err := os.WriteFile(filepath.Join(root, "unrelated.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if violations := newDurableHandlerRetirementViolations(parseProductionGoFiles(t, root)); len(violations) != 0 {
		t.Fatalf("unrelated selector classified as retired builder: %v", violations)
	}
}

// TestLegacyHeartbeatProductionCallZeroGrowthStagingGuard prevents another
// production caller while the non-default integration branch stages the final
// migrations. The expected files are not an API allowlist or merge authority;
// final #759 conformance replaces this guard with zero callers and no export.
func TestLegacyHeartbeatProductionCallZeroGrowthStagingGuard(t *testing.T) {
	files := parseProductionGoFiles(t, filepath.Clean(".."))
	scan := scanLegacyHeartbeatReferences(files)
	if len(scan.violations) != 0 {
		t.Fatalf("legacy ConsumeWithHeartbeat surface violations: %v", scan.violations)
	}
	wantDeclaration := map[string]string{
		"natsclient/heartbeat.go": "func(ctx context.Context, msg jetstream.Msg, heartbeatInterval time.Duration, work func(context.Context) error) error",
	}
	gotDeclaration := map[string]string{}
	for _, parsed := range files {
		if parsed.file.Name.Name != "natsclient" {
			continue
		}
		for _, declaration := range parsed.file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if ok && fn.Name.Name == "ConsumeWithHeartbeat" {
				gotDeclaration[parsed.rel] = compactNode(t, fn.Type)
			}
		}
	}
	if !reflect.DeepEqual(gotDeclaration, wantDeclaration) {
		t.Fatalf("legacy ConsumeWithHeartbeat declaration = %#v, want %#v", gotDeclaration, wantDeclaration)
	}
	want := map[string]int{
		"agentic/agentrun/agentrun.go": 1,
	}
	if !reflect.DeepEqual(scan.directCalls, want) {
		t.Fatalf("legacy ConsumeWithHeartbeat callers = %#v, want exact branch-staging set %#v", scan.directCalls, want)
	}
}

func TestLegacyHeartbeatGuardRejectsTakingOrAliasingSymbol(t *testing.T) {
	root := t.TempDir()
	source := "package fixture\n" +
		"import nc \"github.com/c360studio/semstreams/natsclient\"\n" +
		"var ExportedLegacyHeartbeat = nc.ConsumeWithHeartbeat\n" +
		"func callIndirect() { legacy := nc.ConsumeWithHeartbeat; _ = legacy(nil, nil, 0, nil) }\n"
	if err := os.WriteFile(filepath.Join(root, "indirect.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	scan := scanLegacyHeartbeatReferences(parseProductionGoFiles(t, root))
	if len(scan.violations) != 2 {
		t.Fatalf("legacy indirect-reference violations = %v, want two", scan.violations)
	}
	if len(scan.directCalls) != 0 {
		t.Fatalf("legacy direct calls = %#v, want none", scan.directCalls)
	}
}

func TestLegacyHeartbeatGuardRejectsAlternateExportedSurface(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "variable alias",
			source: "package natsclient\nvar ConsumeWithHeartbeat = func() {}\n",
		},
		{
			name: "receiver method",
			source: "package natsclient\n" +
				"type Client struct{}\n" +
				"func (*Client) ConsumeWithHeartbeat() {}\n",
		},
		{
			name:   "type alias",
			source: "package natsclient\ntype ConsumeWithHeartbeat = func()\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "alternate.go"), []byte(tt.source), 0o600); err != nil {
				t.Fatal(err)
			}
			scan := scanLegacyHeartbeatReferences(parseProductionGoFiles(t, root))
			if len(scan.violations) == 0 {
				t.Fatal("legacy surface guard accepted alternate export")
			}
		})
	}
}

func TestLegacyHeartbeatGuardCountsDotImportAsDirectCall(t *testing.T) {
	root := t.TempDir()
	source := "package fixture\n" +
		"import . \"github.com/c360studio/semstreams/natsclient\"\n" +
		"func callLegacy() { _ = ConsumeWithHeartbeat(nil, nil, 0, nil) }\n"
	if err := os.WriteFile(filepath.Join(root, "dot.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	scan := scanLegacyHeartbeatReferences(parseProductionGoFiles(t, root))
	if len(scan.violations) != 0 {
		t.Fatalf("dot-import direct call reported as indirect: %v", scan.violations)
	}
	want := map[string]int{"dot.go": 1}
	if !reflect.DeepEqual(scan.directCalls, want) {
		t.Fatalf("dot-import direct calls = %#v, want %#v", scan.directCalls, want)
	}
}

func TestLegacyHeartbeatGuardIgnoresUnrelatedSelector(t *testing.T) {
	root := t.TempDir()
	source := "package fixture\n" +
		"import other \"example.com/other\"\n" +
		"func callOther() { other.ConsumeWithHeartbeat() }\n"
	if err := os.WriteFile(filepath.Join(root, "unrelated.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	scan := scanLegacyHeartbeatReferences(parseProductionGoFiles(t, root))
	if len(scan.violations) != 0 || len(scan.directCalls) != 0 {
		t.Fatalf("unrelated selector classified as legacy: violations=%v direct=%#v", scan.violations, scan.directCalls)
	}
}

func TestNoDeliveryDispositionExportedSurface(t *testing.T) {
	files := parseProductionGoFiles(t, ".")
	for _, parsed := range files {
		for _, declaration := range parsed.file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if strings.Contains(declaration.Name.Name, "DeliveryDisposition") {
					t.Fatalf("forbidden disposition constructor/function exported: %s", declaration.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					if typed, ok := spec.(*ast.TypeSpec); ok && strings.Contains(typed.Name.Name, "DeliveryDisposition") {
						t.Fatalf("forbidden disposition type exported: %s", typed.Name.Name)
					}
				}
			}
		}
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
			if entry.Name() == ".git" || entry.Name() == ".claude" || entry.Name() == "vendor" {
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

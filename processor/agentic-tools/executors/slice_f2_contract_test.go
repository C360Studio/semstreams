package executors

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

var sliceF2FormerNames = []string{"search_graph", "summarize_graph"}

func TestSliceF2FrameworkWrapperSurfaceIsDeleted(t *testing.T) {
	repoRoot := sliceF2RepoRoot(t)
	executorDir := filepath.Join(repoRoot, "processor", "agentic-tools", "executors")
	for _, name := range []string{
		"search_graph.go", "search_graph_test.go", "register_search_graph.go",
		"summarize_graph.go", "summarize_graph_test.go", "register_summarize_graph.go",
	} {
		if _, err := os.Stat(filepath.Join(executorDir, name)); err == nil {
			t.Errorf("deleted F2 wrapper file still exists: %s", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %s: %v", name, err)
		}
	}

	declarations := sliceF2TopLevelDecls(t, executorDir)
	for _, name := range []string{
		"SearchGraphExecutor", "SearchGraphOption", "WithSearchGraphTimeout", "NewSearchGraphExecutor",
		"SummarizeGraphExecutor", "SummarizeGraphOption", "WithSummarizeGraphTimeout", "NewSummarizeGraphExecutor",
		"NATSQuerier", "registerSearchGraph", "registerSummarizeGraph",
	} {
		if position, ok := declarations[name]; ok {
			t.Errorf("deleted F2 declaration %s remains at %s", name, position)
		}
	}

	for _, path := range []string{
		filepath.Join(executorDir, "register.go"),
		filepath.Join(repoRoot, "processor", "agentic-tools", "categories.go"),
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, name := range []string{"search_graph", "summarize_graph", "graph_search", "graph_summary"} {
			if strings.Contains(string(body), `"`+name+`"`) {
				t.Errorf("framework-owned name %q remains in %s", name, path)
			}
		}
	}
}

func TestSliceF2DeletedSkipKeysFailClosed(t *testing.T) {
	for _, name := range sliceF2FormerNames {
		t.Run(name, func(t *testing.T) {
			registry := agentictools.NewExecutorRegistry()
			err := RegisterBuiltins(context.Background(), registry, ToolDependencies{
				Logger:       slog.Default(),
				SkipBuiltins: []string{name},
			})
			if err == nil || !strings.Contains(err.Error(), "unknown builtin group key") {
				t.Fatalf("stale SkipBuiltins %q error = %v, want unknown-key failure", name, err)
			}
			if tools := registry.ListTools(); len(tools) != 0 {
				t.Fatalf("registration occurred before stale skip validation: %v", tools)
			}
		})
	}
}

func TestSliceF2PermissiveAllowedToolsDoNotCreateDeletedExecutors(t *testing.T) {
	for _, allowed := range [][]string{nil, []string{}} {
		registry := agentictools.NewExecutorRegistry()
		err := RegisterBuiltins(context.Background(), registry, ToolDependencies{
			NATSClient:   &natsclient.Client{},
			Logger:       slog.Default(),
			SkipBuiltins: sliceF2SkipAllExcept(append([]string{"bash"}, sliceF2FormerNames...)...),
		})
		if err != nil {
			t.Fatalf("RegisterBuiltins: %v", err)
		}
		for _, name := range sliceF2FormerNames {
			if slices.ContainsFunc(registry.ListTools(), func(def agentic.ToolDefinition) bool { return def.Name == name }) {
				t.Fatalf("shared discovery still contains deleted wrapper %q", name)
			}
		}

		config := agentictools.DefaultConfig()
		config.AllowedTools = allowed
		raw, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("marshal config: %v", err)
		}
		created, err := agentictools.NewComponent(raw, component.Dependencies{ToolRegistry: registry})
		if err != nil {
			t.Fatalf("NewComponent: %v", err)
		}
		comp := created.(*agentictools.Component)
		for _, name := range sliceF2FormerNames {
			result, execErr := comp.Execute(context.Background(), agentic.ToolCall{ID: "missing", Name: name})
			if !errors.Is(execErr, agentic.ErrToolNotFound) || result.ErrorKind != agentic.ToolErrorNotFound {
				t.Errorf("Execute(%q) = (%#v, %v), want typed not-found", name, result, execErr)
			}
		}
		result, execErr := comp.Execute(context.Background(), agentic.ToolCall{
			ID: "surviving", Name: "bash", Arguments: map[string]any{"command": "printf slice-f2"},
		})
		if execErr != nil || result.Content != "slice-f2" {
			t.Fatalf("surviving shared builtin = (%#v, %v)", result, execErr)
		}
	}
}

type sliceF2LocalExecutor struct {
	name string
}

func (e *sliceF2LocalExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name: e.name, Description: "application-local Slice F2 probe",
		Parameters: map[string]any{"type": "object"}, Effect: agentic.ToolEffectReadOnly,
	}}
}

func (e *sliceF2LocalExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{CallID: call.ID, Content: "local:" + e.name}, nil
}

func TestSliceF2LocalFormerNameUsesExistingPrecedence(t *testing.T) {
	for _, name := range sliceF2FormerNames {
		t.Run(name, func(t *testing.T) {
			config := agentictools.DefaultConfig()
			config.AllowedTools = []string{name}
			config.ApprovalRequired = []string{name}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatalf("marshal config: %v", err)
			}
			created, err := agentictools.NewComponent(raw, component.Dependencies{ToolRegistry: agentictools.NewExecutorRegistry()})
			if err != nil {
				t.Fatalf("NewComponent: %v", err)
			}
			comp := created.(*agentictools.Component)
			if err := comp.RegisterToolExecutor(&sliceF2LocalExecutor{name: name}); err != nil {
				t.Fatalf("RegisterToolExecutor(%q): %v", name, err)
			}
			tools := comp.ListTools()
			if len(tools) != 1 || tools[0].Name != name {
				t.Fatalf("local discovery = %#v, want only %q", tools, name)
			}

			filter := agentictools.NewApprovalFilter([]string{name})
			unapproved := filter.FilterToolCalls("loop-f2", []agentic.ToolCall{{ID: "gated", Name: name}})
			if len(unapproved.Rejected) != 1 || !agentic.IsApprovalRequired(unapproved.Rejected[0].Reason) {
				t.Fatalf("ordinary approval gate did not intercept local %q: %#v", name, unapproved)
			}
			approved := agentic.ToolCall{
				ID: "approved", Name: name, ApprovedBy: "owner",
				Metadata: map[string]any{agentic.MetadataKeyAdvertisedTools: []string{name}},
			}
			bypass := filter.FilterToolCalls("loop-f2", []agentic.ToolCall{approved})
			if len(bypass.Approved) != 1 || len(bypass.Rejected) != 0 {
				t.Fatalf("ordinary approval bypass rejected local %q: %#v", name, bypass)
			}
			result, execErr := comp.Execute(context.Background(), approved)
			if execErr != nil || result.Content != "local:"+name {
				t.Fatalf("local dispatch %q = (%#v, %v)", name, result, execErr)
			}
		})
	}
}

func TestSliceF2RetainedGraphAccessSurfacesRemain(t *testing.T) {
	repoRoot := sliceF2RepoRoot(t)
	cases := []struct {
		directory string
		names     []string
	}{
		{filepath.Join(repoRoot, "gateway", "graph-gateway"), []string{"Component", "CreateGraphGateway"}},
		{filepath.Join(repoRoot, "processor", "graph-query"), []string{"Component", "CreateGraphQuery"}},
		{filepath.Join(repoRoot, "processor", "research-graph-classify"), []string{"Component", "NewProcessor"}},
		{filepath.Join(repoRoot, "processor", "research-graph-execute"), []string{"Component", "NewProcessor"}},
		{filepath.Join(repoRoot, "pkg", "fusion", "fusionnats"), []string{"Client", "New"}},
		{filepath.Join(repoRoot, "graph"), []string{"ExactEntityReader", "NewExactEntityReader"}},
		{filepath.Join(repoRoot, "pkg", "projection"), []string{"MutationClient", "NewMutationClient"}},
		{filepath.Join(repoRoot, "graph", "query"), []string{"Classifier", "NewKeywordClassifier", "SearchOptions"}},
		{filepath.Join(repoRoot, "frameworkcapabilities", "graphresearch"), []string{"ResearchGraphToolName", "ResearchGraphExecutor", "NewResearchGraphExecutor"}},
		{filepath.Join(repoRoot, "processor", "agentic-tools", "executors"), []string{"GraphQueryExecutor", "NewGraphQueryExecutor"}},
	}
	for _, test := range cases {
		declarations := sliceF2TopLevelDecls(t, test.directory)
		for _, name := range test.names {
			if _, ok := declarations[name]; !ok {
				t.Errorf("retained graph access declaration %s is absent from %s", name, test.directory)
			}
		}
	}

	gatewaySource, err := os.ReadFile(filepath.Join(repoRoot, "gateway", "graph-gateway", "component.go"))
	if err != nil {
		t.Fatalf("read graph gateway: %v", err)
	}
	for _, field := range []string{"graphSummary", "searchGraph"} {
		if !strings.Contains(string(gatewaySource), `"`+field+`"`) {
			t.Errorf("retained GraphQL field %q is absent", field)
		}
	}

	directNames := make([]string, 0, 5)
	for _, definition := range NewGraphQueryExecutor(nil).ListTools() {
		directNames = append(directNames, definition.Name)
	}
	wantDirect := []string{"query_entity", "query_entities", "query_relationships", "query_neighbors", "query_by_type"}
	if !slices.Equal(directNames, wantDirect) {
		t.Errorf("retained direct query tools = %v, want %v", directNames, wantDirect)
	}
}

func sliceF2SkipAllExcept(names ...string) []string {
	var skipped []string
	for _, key := range BuiltinGroupKeys {
		if !slices.Contains(names, key) {
			skipped = append(skipped, key)
		}
	}
	return skipped
}

func sliceF2RepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Slice F2 contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func sliceF2TopLevelDecls(t *testing.T, directory string) map[string]token.Position {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read %s: %v", directory, err)
	}
	files := token.NewFileSet()
	declarations := make(map[string]token.Position)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		parsed, parseErr := parser.ParseFile(files, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		for _, declaration := range parsed.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if typed.Recv == nil {
					declarations[typed.Name.Name] = files.Position(typed.Name.Pos())
				}
			case *ast.GenDecl:
				for _, specification := range typed.Specs {
					switch spec := specification.(type) {
					case *ast.TypeSpec:
						declarations[spec.Name.Name] = files.Position(spec.Name.Pos())
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							declarations[name.Name] = files.Position(name.Pos())
						}
					}
				}
			}
		}
	}
	return declarations
}

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSliceF1AggregateClientSurfaceIsDeleted(t *testing.T) {
	repoRoot := sliceF1RepoRoot(t)
	queryDecls := sliceF1TopLevelDecls(t, filepath.Join(repoRoot, "graph", "query"))

	for _, name := range []string{
		"Client",
		"Config",
		"NewClient",
		"NewClientWithMetrics",
		"PathQuery",
		"PathResult",
		"CacheStats",
	} {
		if position, ok := queryDecls[name]; ok {
			t.Errorf("retired graph/query aggregate-client symbol %s remains at %s", name, position)
		}
	}
}

func TestSliceF1RetainedQuerySurfacesRemain(t *testing.T) {
	repoRoot := sliceF1RepoRoot(t)
	// Detailed operation, adapter, routing, index/readiness, and F2 executor
	// behavior remains pinned in each retained package's focused tests. This AST
	// gate only prevents F1 from deleting their stable package entry surfaces.
	cases := []struct {
		directory string
		names     []string
	}{
		{
			directory: filepath.Join(repoRoot, "graph", "query"),
			names: []string{
				"Classifier",
				"NewKeywordClassifier",
				"SearchOptions",
				"Example",
				"LoadDomainExamples",
			},
		},
		{
			directory: filepath.Join(repoRoot, "graph"),
			names:     []string{"ExactEntityReader", "NewExactEntityReader"},
		},
		{
			directory: filepath.Join(repoRoot, "pkg", "projection"),
			names:     []string{"MutationClient", "NewMutationClient"},
		},
		{
			directory: filepath.Join(repoRoot, "pkg", "fusion", "fusionnats"),
			names:     []string{"Client", "New"},
		},
		{
			directory: filepath.Join(repoRoot, "processor", "graph-query"),
			names: []string{
				"Component",
				"CreateGraphQuery",
				"PathSearcher",
				"NewPathSearcher",
				"PathSearchRequest",
				"PathSearchResponse",
			},
		},
		{
			directory: filepath.Join(repoRoot, "processor", "research-graph-classify"),
			names:     []string{"Component", "NewProcessor"},
		},
		{
			directory: filepath.Join(repoRoot, "processor", "research-graph-execute"),
			names:     []string{"Component", "NewProcessor"},
		},
		{
			directory: filepath.Join(repoRoot, "gateway", "graph-gateway"),
			names:     []string{"Component", "CreateGraphGateway"},
		},
		{
			directory: filepath.Join(repoRoot, "processor", "graph-index"),
			names:     []string{"Component", "CreateGraphIndex"},
		},
		{
			directory: filepath.Join(repoRoot, "processor", "agentic-tools", "executors"),
			names: []string{
				"NATSQuerier",
				"SearchGraphExecutor",
				"NewSearchGraphExecutor",
				"SummarizeGraphExecutor",
				"NewSummarizeGraphExecutor",
			},
		},
	}

	for _, tc := range cases {
		declarations := sliceF1TopLevelDecls(t, tc.directory)
		for _, name := range tc.names {
			if _, ok := declarations[name]; !ok {
				t.Errorf("retained declaration %s is absent from %s", name, tc.directory)
			}
		}
	}
}

func TestSliceF1TopLevelDeclsIncludeValueAliases(t *testing.T) {
	directory := t.TempDir()
	source := []byte(`package fixture

var (
	Client = existing
	NewClient = existing
)

const CacheStats = "alias"
`)
	if err := os.WriteFile(filepath.Join(directory, "aliases.go"), source, 0o600); err != nil {
		t.Fatalf("write alias fixture: %v", err)
	}

	declarations := sliceF1TopLevelDecls(t, directory)
	for _, name := range []string{"Client", "NewClient", "CacheStats"} {
		if _, ok := declarations[name]; !ok {
			t.Errorf("value alias %s was not inventoried", name)
		}
	}
}

func sliceF1RepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Slice F1 contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func sliceF1TopLevelDecls(t *testing.T, directory string) map[string]token.Position {
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

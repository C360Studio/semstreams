package testinfra_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const baselinePath = "test/testinfra/policy_baseline.json"

type finding struct {
	Category string
	Path     string
	Function string
	Call     string
	Ordinal  int
}

func (f finding) key() string {
	return strings.Join([]string{f.Category, f.Path, f.Function, f.Call, strconv.Itoa(f.Ordinal)}, "|")
}

type scanStats struct {
	GoFiles          int
	TestFiles        int
	IntegrationFiles int
	Calls            int
}

type baseline struct {
	Version int             `json:"version"`
	Entries []baselineEntry `json:"entries"`
}

type baselineEntry struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

var canonicalContainerSubstrate = map[string]string{
	"natsclient/test_client.go": "canonical repository-owned testcontainers substrate",
}

func TestInfrastructurePolicyGuard(t *testing.T) {
	repoRoot := findRepoRoot(t)
	findings, stats, err := scanRepository(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if stats.GoFiles < 100 || stats.TestFiles < 100 || stats.IntegrationFiles == 0 || stats.Calls == 0 {
		t.Fatalf("policy guard scanned too little source: %+v; a zero/near-zero scan is a broken guard, not a clean repository", stats)
	}

	path := filepath.Join(repoRoot, baselinePath)
	approved := readBaseline(t, path)
	assertBaselineMatches(t, findings, approved)
}

func TestInfrastructurePolicyGuardIgnoresClaudeWorktrees(t *testing.T) {
	root := t.TempDir()
	kept, err := os.CreateTemp(root, "kept-*.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kept.WriteString("package fixture\nfunc kept() {}\n"); err != nil {
		t.Fatal(err)
	}
	if err := kept.Close(); err != nil {
		t.Fatal(err)
	}

	worktree := filepath.Join(root, ".claude", "worktrees", "agent-test")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	contamination, err := os.CreateTemp(worktree, "contamination-*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contamination.WriteString(`package contamination
import "testing"
func bad() { _ = &testing.T{} }
`); err != nil {
		t.Fatal(err)
	}
	if err := contamination.Close(); err != nil {
		t.Fatal(err)
	}

	findings, stats, err := scanRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf(".claude worktree contaminated policy findings: %+v", findings)
	}
	if stats.GoFiles != 1 {
		t.Fatalf("policy scan GoFiles = %d, want 1 root file", stats.GoFiles)
	}
}

func TestInfrastructurePolicyBaselineHasNoWritePath(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	path := filepath.Join(repoRoot, "test", "testinfra", "policy_guard_test.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, body, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if fn, ok := node.(*ast.FuncDecl); ok && fn.Name.Name == "writeBaseline" {
			t.Error("policy baseline retains writeBaseline update function")
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if ok && receiver.Name == "os" && (selector.Sel.Name == "WriteFile" || selector.Sel.Name == "Create") {
			t.Errorf("policy baseline retains filesystem write call os.%s", selector.Sel.Name)
		}
		return true
	})
	value := readBaseline(t, filepath.Join(repoRoot, baselinePath))
	for _, entry := range value.Entries {
		category := strings.SplitN(entry.Key, "|", 2)[0]
		if category == "untagged-container" || category == "direct-container-api" {
			t.Errorf("zero-debt category %q must never appear in the baseline: %s", category, entry.Key)
		}
	}
}

func TestInfrastructurePolicyFixtureMatrix(t *testing.T) {
	type fixture struct {
		name       string
		path       string
		source     string
		categories []string
	}
	fixtures := []fixture{
		{
			name: "untagged canonical helper",
			path: "fixture/helper_test.go",
			source: `package fixture
import ("testing"; "github.com/c360studio/semstreams/natsclient")
func TestStartsDocker(t *testing.T) { natsclient.NewTestClient(t) }
`,
			categories: []string{"untagged-container"},
		},
		{
			name: "integration helper is correctly tagged",
			path: "fixture/helper_integration_test.go",
			source: `//go:build integration && linux
package fixture
import ("testing"; "github.com/c360studio/semstreams/natsclient")
func TestIntegration_StartsDocker(t *testing.T) { natsclient.NewTestClient(t) }
`,
		},
		{
			name: "negated integration tag remains untagged",
			path: "fixture/negated_helper_test.go",
			source: `//go:build !integration
package fixture
import ("testing"; "github.com/c360studio/semstreams/natsclient")
func TestStartsDockerWithoutIntegration(t *testing.T) { natsclient.NewTestClient(t) }
`,
			categories: []string{"untagged-container"},
		},
		{
			name: "aliased untagged testcontainers call",
			path: "fixture/container_test.go",
			source: `package fixture
import ("context"; tc "github.com/testcontainers/testcontainers-go")
func test(ctx context.Context) { tc.GenericContainer(ctx, tc.GenericContainerRequest{}) }
`,
			categories: []string{"direct-container-api", "untagged-container"},
		},
		{
			name: "tagged direct readiness calls",
			path: "fixture/readiness_integration_test.go",
			source: `//go:build integration
package fixture
import (
 "context"
 tc "github.com/testcontainers/testcontainers-go"
 ready "github.com/testcontainers/testcontainers-go/wait"
)
func test(ctx context.Context, container tc.Container) {
 ready.ForHTTP("/")
 ready.ForListeningPort("4222/tcp")
 container.Host(ctx)
 container.MappedPort(ctx, "4222/tcp")
}
`,
			categories: []string{"direct-container-api", "direct-container-api", "direct-container-api", "direct-container-api"},
		},
		{
			name: "fabricated testing T",
			path: "fixture/fabricated_test.go",
			source: `package fixture
import "testing"
func test() { _ = &testing.T{} }
`,
			categories: []string{"fabricated-testing-t"},
		},
		{
			name: "integration sleep",
			path: "fixture/sleep_integration_test.go",
			source: `//go:build integration
package fixture
import "time"
func TestIntegration_Waits() { time.Sleep(time.Second) }
`,
			categories: []string{"integration-time-sleep"},
		},
		{
			name: "unit sleep is outside this integration ratchet",
			path: "fixture/sleep_test.go",
			source: `package fixture
import "time"
func TestClock() { time.Sleep(time.Nanosecond) }
`,
		},
		{
			name: "comments and strings do not trigger AST rules",
			path: "fixture/text_test.go",
			source: `package fixture
const text = "NewTestClient time.Sleep GenericContainer &testing.T{}"
// NewSharedTestClient and MappedPort are examples only.
`,
		},
	}
	if len(fixtures) == 0 {
		t.Fatal("fixture matrix has zero cases")
	}

	positive := map[string]bool{}
	negative := map[string]bool{}
	allCategories := []string{
		"untagged-container",
		"direct-container-api",
		"fabricated-testing-t",
		"integration-time-sleep",
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			got, _, err := scanSource(fixture.path, []byte(fixture.source))
			if err != nil {
				t.Fatal(err)
			}
			var categories []string
			for _, item := range got {
				categories = append(categories, item.Category)
			}
			sort.Strings(categories)
			want := append([]string(nil), fixture.categories...)
			sort.Strings(want)
			if strings.Join(categories, ",") != strings.Join(want, ",") {
				t.Fatalf("categories = %v, want %v; findings: %+v", categories, want, got)
			}
			for _, category := range allCategories {
				if contains(want, category) {
					positive[category] = true
				} else {
					negative[category] = true
				}
			}
		})
	}
	for _, category := range allCategories {
		if !positive[category] || !negative[category] {
			t.Errorf("fixture matrix must contain positive and negative cases for %s (positive=%v negative=%v)",
				category, positive[category], negative[category])
		}
	}
}

func scanRepository(root string) ([]finding, scanStats, error) {
	var findings []finding
	var stats scanStats
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		got, fileStats, err := scanSource(relative, body)
		if err != nil {
			return err
		}
		stats.GoFiles++
		stats.Calls += fileStats.Calls
		if strings.HasSuffix(relative, "_test.go") {
			stats.TestFiles++
		}
		if fileStats.IntegrationFiles > 0 {
			stats.IntegrationFiles++
		}
		findings = append(findings, got...)
		return nil
	})
	if err != nil {
		return nil, stats, err
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].key() < findings[j].key() })
	return findings, stats, nil
}

func scanSource(path string, source []byte) ([]finding, scanStats, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return nil, scanStats{}, fmt.Errorf("parse %s: %w", path, err)
	}
	integration, err := hasBuildTag(source, "integration")
	if err != nil {
		return nil, scanStats{}, fmt.Errorf("parse build constraint in %s: %w", path, err)
	}
	stats := scanStats{}
	if integration {
		stats.IntegrationFiles = 1
	}

	imports := make(map[string]string)
	for _, item := range file.Imports {
		importPath, unquoteErr := strconv.Unquote(item.Path.Value)
		if unquoteErr != nil {
			return nil, stats, unquoteErr
		}
		name := filepath.Base(importPath)
		if item.Name != nil {
			name = item.Name.Name
		}
		imports[name] = importPath
	}
	importsTestcontainers := false
	for _, importPath := range imports {
		if strings.HasPrefix(importPath, "github.com/testcontainers/testcontainers-go") {
			importsTestcontainers = true
		}
	}

	var findings []finding
	counts := make(map[string]int)
	function := "<package>"
	ast.Inspect(file, func(node ast.Node) bool {
		if fn, ok := node.(*ast.FuncDecl); ok {
			function = fn.Name.Name
		}
		if unary, ok := node.(*ast.UnaryExpr); ok && unary.Op == token.AND && isTestingTComposite(unary.X, imports) {
			findings = appendFinding(findings, counts, finding{
				Category: "fabricated-testing-t", Path: path, Function: function, Call: "&testing.T{}",
			})
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		stats.Calls++
		callText := formatNode(fset, call)
		if isContainerStartCall(call.Fun, file.Name.Name, imports) && strings.HasSuffix(path, "_test.go") && !integration {
			findings = appendFinding(findings, counts, finding{
				Category: "untagged-container", Path: path, Function: function, Call: callText,
			})
		}
		if isDirectContainerCall(call.Fun, imports, importsTestcontainers) {
			if _, canonical := canonicalContainerSubstrate[path]; !canonical {
				findings = appendFinding(findings, counts, finding{
					Category: "direct-container-api", Path: path, Function: function, Call: callText,
				})
			}
		}
		if integration && isImportedSelector(call.Fun, imports, "time", "Sleep") {
			findings = appendFinding(findings, counts, finding{
				Category: "integration-time-sleep", Path: path, Function: function, Call: callText,
			})
		}
		return true
	})
	return findings, stats, nil
}

func appendFinding(findings []finding, counts map[string]int, item finding) []finding {
	identity := strings.Join([]string{item.Category, item.Path, item.Function, item.Call}, "|")
	counts[identity]++
	item.Ordinal = counts[identity]
	return append(findings, item)
}

func isContainerStartCall(expr ast.Expr, packageName string, imports map[string]string) bool {
	switch fn := expr.(type) {
	case *ast.Ident:
		return packageName == "natsclient" && (fn.Name == "NewTestClient" || fn.Name == "NewSharedTestClient")
	case *ast.SelectorExpr:
		ident, ok := fn.X.(*ast.Ident)
		if !ok {
			return false
		}
		importPath := imports[ident.Name]
		if importPath == "github.com/c360studio/semstreams/natsclient" {
			return fn.Sel.Name == "NewTestClient" || fn.Sel.Name == "NewSharedTestClient"
		}
		return strings.HasPrefix(importPath, "github.com/testcontainers/testcontainers-go")
	}
	return false
}

func isDirectContainerCall(expr ast.Expr, imports map[string]string, importsTestcontainers bool) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, identReceiver := selector.X.(*ast.Ident)
	switch selector.Sel.Name {
	case "GenericContainer":
		return identReceiver && imports[ident.Name] == "github.com/testcontainers/testcontainers-go"
	case "ForHTTP", "ForListeningPort":
		return identReceiver && imports[ident.Name] == "github.com/testcontainers/testcontainers-go/wait"
	case "Host", "MappedPort":
		return importsTestcontainers
	default:
		return false
	}
}

func isImportedSelector(expr ast.Expr, imports map[string]string, importPath, method string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != method {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && imports[ident.Name] == importPath
}

func isTestingTComposite(expr ast.Expr, imports map[string]string) bool {
	composite, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false
	}
	selector, ok := composite.Type.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "T" {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && imports[ident.Name] == "testing"
}

func hasBuildTag(source []byte, tag string) (bool, error) {
	for _, line := range bytes.Split(source, []byte("\n")) {
		trimmed := strings.TrimSpace(string(line))
		if strings.HasPrefix(trimmed, "package ") {
			return false, nil
		}
		if !strings.HasPrefix(trimmed, "//go:build ") {
			continue
		}
		expr, err := constraint.Parse(trimmed)
		if err != nil {
			return false, err
		}
		return constraintRequiresTag(expr, tag), nil
	}
	return false, nil
}

// constraintRequiresTag reports whether every branch that can select the file
// requires tag to be true. Merely mentioning integration is insufficient:
// !integration and (integration || linux) both permit an untagged unit build.
func constraintRequiresTag(expr constraint.Expr, tag string) bool {
	switch value := expr.(type) {
	case *constraint.TagExpr:
		return value.Tag == tag
	case *constraint.NotExpr:
		return false
	case *constraint.AndExpr:
		return constraintRequiresTag(value.X, tag) || constraintRequiresTag(value.Y, tag)
	case *constraint.OrExpr:
		return constraintRequiresTag(value.X, tag) && constraintRequiresTag(value.Y, tag)
	default:
		return false
	}
}

func formatNode(fset *token.FileSet, node ast.Node) string {
	var output bytes.Buffer
	if err := format.Node(&output, fset, node); err != nil {
		return "<unformattable>"
	}
	return output.String()
}

func readBaseline(t *testing.T, path string) baseline {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value baseline
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if err := validateBaseline(value); err != nil {
		t.Fatalf("invalid policy baseline: %v", err)
	}
	return value
}

func validateBaseline(value baseline) error {
	if value.Version != 1 {
		return fmt.Errorf("unsupported version %d", value.Version)
	}
	for _, entry := range value.Entries {
		category := strings.SplitN(entry.Key, "|", 2)[0]
		switch category {
		case "untagged-container", "direct-container-api":
			return fmt.Errorf("zero-debt category %q cannot be baselined: %s", category, entry.Key)
		}
	}
	return nil
}

func assertBaselineMatches(t *testing.T, findings []finding, value baseline) {
	t.Helper()
	live := make(map[string]bool, len(findings))
	for _, item := range findings {
		live[item.key()] = true
	}
	approved := make(map[string]baselineEntry, len(value.Entries))
	for _, item := range value.Entries {
		if item.Key == "" || item.Kind == "" || len(strings.Fields(item.Reason)) < 4 {
			t.Errorf("invalid baseline entry %+v: key, kind, and a specific reason are required", item)
		}
		if _, duplicate := approved[item.Key]; duplicate {
			t.Errorf("duplicate baseline key %q", item.Key)
		}
		approved[item.Key] = item
	}
	var unexpected, stale []string
	for key := range live {
		if _, ok := approved[key]; !ok {
			unexpected = append(unexpected, key)
		}
	}
	for key := range approved {
		if !live[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(unexpected)
	sort.Strings(stale)
	if len(unexpected) > 0 {
		t.Errorf("new test-infrastructure policy violations (%d); move Docker evidence to integration, use the canonical substrate, or replace the wait:\n  %s",
			len(unexpected), strings.Join(unexpected, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("stale policy baseline entries (%d); remove resolved debt so the ratchet cannot hide a regression:\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("go.mod not found")
		}
		directory = parent
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

package natsclient

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
)

func TestMonitoringURLConsumersExplicitlyEnableMonitoring(t *testing.T) {
	t.Parallel()

	wantConsumers := []string{
		"natsclient/test_client_integration_test.go:TestIntegration_NewTestClient_BasicConnection",
		"processor/graph-index/owner_filter_load_integration_test.go:TestIntegration_OwnerFilterLoadHarness",
		"processor/graph-index/owner_filter_load_integration_test.go:runOwnerLoadWorkerShape",
		"processor/graph-index/predicate_layout_smoke_integration_test.go:runPredicateLayoutSmoke",
	}
	constructorOwners := map[string]string{
		"natsclient/test_client_integration_test.go":                       "TestIntegration_NewTestClient_BasicConnection",
		"processor/graph-index/owner_filter_load_integration_test.go":      "TestIntegration_OwnerFilterLoadHarness",
		"processor/graph-index/predicate_layout_smoke_integration_test.go": "runPredicateLayoutSmoke",
	}

	parsedFiles := make(map[string]*ast.File, len(constructorOwners))
	var gotConsumers []string
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		repoRelative, relativeErr := filepath.Rel("..", path)
		if relativeErr != nil {
			return relativeErr
		}
		repoRelative = filepath.ToSlash(repoRelative)
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !usesSelector(function.Body, "MonitoringURL") {
				continue
			}
			gotConsumers = append(gotConsumers, repoRelative+":"+function.Name.Name)
			parsedFiles[repoRelative] = file
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk MonitoringURL consumers: %v", err)
	}
	slices.Sort(gotConsumers)
	slices.Sort(wantConsumers)
	if !slices.Equal(gotConsumers, wantConsumers) {
		t.Fatalf("MonitoringURL consumers = %v, want reviewed inventory %v", gotConsumers, wantConsumers)
	}

	for fileName, ownerName := range constructorOwners {
		file := parsedFiles[fileName]
		if file == nil {
			t.Errorf("reviewed monitoring consumer file %s was not parsed", fileName)
			continue
		}
		owner := findFunction(file, ownerName)
		if owner == nil {
			t.Errorf("monitoring constructor owner %s:%s not found", fileName, ownerName)
			continue
		}
		if !callsNewTestClientWithMonitoring(owner.Body) {
			t.Errorf("monitoring constructor owner %s:%s does not pass WithMonitoring", fileName, ownerName)
		}
	}
}

func usesSelector(node ast.Node, selectorName string) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == selectorName {
			found = true
		}
		return !found
	})
	return found
}

func findFunction(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return function
		}
	}
	return nil
}

func callsNewTestClientWithMonitoring(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || calledName(call.Fun) != "NewTestClient" {
			return !found
		}
		for _, arg := range call.Args {
			option, ok := arg.(*ast.CallExpr)
			if ok && calledName(option.Fun) == "WithMonitoring" {
				found = true
				break
			}
		}
		return !found
	})
	return found
}

func calledName(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		return expression.Sel.Name
	default:
		return ""
	}
}

package contract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestRetainedGraphReadersUseCatalogAcquisition guards the exact R1a reader
// acquisition sites. Owner write handles are outside this list and remain free
// to use their catalog Ensure seams.
func TestRetainedGraphReadersUseCatalogAcquisition(t *testing.T) {
	root := repoRootForKVCatalogScan(t)
	files := []string{
		"processor/graph-index/component.go",
		"processor/graph-index-spatial/component.go",
		"processor/graph-index-temporal/component.go",
		"processor/graph-embedding/component.go",
		"processor/graph-clustering/component.go",
		"processor/rule/entity_watcher.go",
		"pkg/lifecycle/manager.go",
	}

	fset := token.NewFileSet()
	var violations []string
	for _, rel := range files {
		file, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "KeyValue" && selector.Sel.Name != "GetKeyValueBucket") {
				return true
			}
			pos := fset.Position(call.Pos())
			violations = append(violations, fmt.Sprintf("%s:%d calls raw %s acquisition",
				rel, pos.Line, selector.Sel.Name))
			return true
		})
	}

	if len(violations) > 0 {
		t.Errorf("retained graph readers must acquire through graph.OpenCatalogReader:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

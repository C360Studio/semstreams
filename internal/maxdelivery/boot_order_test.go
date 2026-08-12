package maxdelivery

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBinaryBootOrder is a sister-binary guard: both production assemblies must
// provision the capture stream, start its observer, and only then hand control
// to the function whose first action is Manager.StartAll.
func TestBinaryBootOrder(t *testing.T) {
	t.Parallel()

	for _, binary := range []string{"semstreams", "e2e-semstreams"} {
		binary := binary
		t.Run(binary, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("..", "..", "cmd", binary, "main.go")
			calls := functionCalls(t, path, "run")
			requireCallOrder(t, calls,
				"ensureStreamsWithSpinner", "maxdelivery.Start", "runWithSignalHandling")

			ensureCalls := functionCalls(t, path, "ensureStreamsWithSpinner")
			require.Contains(t, ensureCalls, "streamsManager.EnsureStreams")
			require.NotContains(t, ensureCalls, "maxdelivery.EnsureCaptureStream")

			startCalls := functionCalls(t, path, "runWithSignalHandling")
			require.Contains(t, startCalls, "manager.StartAll")
		})
	}
}

func functionCalls(t *testing.T, path, function string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err)

	var calls []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name := callName(call.Fun); name != "" {
				calls = append(calls, name)
			}
			return true
		})
		return calls
	}
	t.Fatalf("function %s not found in %s", function, path)
	return nil
}

func callName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		if base, ok := value.X.(*ast.Ident); ok {
			return base.Name + "." + value.Sel.Name
		}
	}
	return ""
}

func requireCallOrder(t *testing.T, calls []string, ordered ...string) {
	t.Helper()
	position := -1
	for _, want := range ordered {
		found := -1
		for i := position + 1; i < len(calls); i++ {
			if calls[i] == want {
				found = i
				break
			}
		}
		require.NotEqualf(t, -1, found, "call %s absent or out of order in %v", want, calls)
		position = found
	}
}

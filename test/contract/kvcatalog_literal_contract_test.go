package contract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/graph"
)

// kvCatalogLiteralAllowlist are the files permitted to spell a framework KV
// catalog bucket name as an exact string literal:
//   - graph/constants.go — the ONE definition of each name constant.
//   - graph/kvcatalog.go — the descriptor table (references the constants, but
//     allowlisted so a future inline row cannot break the scan's intent).
//
// Everything else must reference graph.Bucket* (or a re-export of it), so a
// name can never fork. Test files are exempt (fixtures legitimately spell
// names), as is the test/ tree (the e2e harness and fixtures).
var kvCatalogLiteralAllowlist = map[string]bool{
	"graph/constants.go": true,
	"graph/kvcatalog.go": true,
}

// TestNoCatalogBucketLiteralsOutsideTheCatalog is the mechanism that replaces
// "review keeps catching missing members": no non-test Go file outside the
// catalog may contain a string literal exactly equal to a catalog bucket name.
// A new shadow constant (`const X = "ENTITY_STATES"`), an inline
// jetstream.KeyValueConfig{Bucket: "EMBEDDING_INDEX"}, or a bare-literal
// acquisition call all fail here — the drift classes that produced F1/F2.
//
// The scan parses each file (go/parser) and inspects only STRING literals, so
// comments and doc text never false-positive; it is repo-wide and mechanical,
// so it cannot forget a package the way a hand list forgets a member.
func TestNoCatalogBucketLiteralsOutsideTheCatalog(t *testing.T) {
	root := repoRootForKVCatalogScan(t)

	catalogNames := make(map[string]bool)
	for _, spec := range graph.KVCatalog() {
		catalogNames[spec.Name] = true
	}
	if len(catalogNames) == 0 {
		t.Fatal("empty catalog — the scan would vacuously pass")
	}

	var violations []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Skip VCS, vendor, and the test harness tree (fixtures + e2e
			// clients legitimately spell bucket names).
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			if rel, rerr := filepath.Rel(root, path); rerr == nil && rel == "test" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if kvCatalogLiteralAllowlist[filepath.ToSlash(rel)] {
			return nil
		}

		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if catalogNames[value] {
				pos := fset.Position(lit.Pos())
				violations = append(violations, fmt.Sprintf("%s:%d names catalog bucket %q as a literal",
					filepath.ToSlash(rel), pos.Line, value))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("catalog bucket names may only be spelled in the catalog (reference graph.Bucket* instead):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// repoRootForKVCatalogScan locates the module root from the test's working
// directory (test/contract) by walking up to the go.mod.
func repoRootForKVCatalogScan(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from the contract test")
		}
		dir = parent
	}
}

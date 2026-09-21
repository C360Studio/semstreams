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
)

// deliveryLaneHome is the ONE package permitted to declare the latch.
const deliveryLaneHome = "internal/deliverylane"

// natsclientImportPath is the package that owns DeliveryResult.
const natsclientImportPath = "github.com/c360studio/semstreams/natsclient"

// TestDeliveryLaneLatchHasOneHome keeps the per-lane admission latch from being
// re-hand-rolled. The latch's unmistakable fingerprint is a STRUCT FIELD of
// type `chan natsclient.DeliveryResult`: a buffered one-slot channel that holds
// the first owner-stop result for a lane. Before this change there were five
// such fields, one per agentic consumer, each with its own subtly different
// close/drain/observe ordering; they are now one, in internal/deliverylane.
//
// The scan is deliberately NARROW. It does not touch the eleven legitimate
// drain-once bindings around a jetstream.ConsumeContext (inventory.md § 2),
// which are a different pattern with a different fingerprint, and it does not
// reach channels of any other element type. Only the latch is claimed here,
// because only the latch is what this package hoisted.
//
// Test files are exempt: a test legitimately builds a channel of results to
// observe one (natsclient's own settlement tests do). The guard is about where
// production code KEEPS such a channel, not who may make one.
//
// It matches on the AST, not on types, so it reads the fingerprint as written:
// every import alias of natsclient counts, but a local `type X =
// natsclient.DeliveryResult` would not. That bound is deliberate — this catches
// drift (somebody re-hand-rolls the latch), not an adversary spelling around it,
// and the cheap scan is worth more than a whole-tree type-check here. The
// non-vacuity assertion below is what keeps the bound honest: if the home's own
// field stops matching for ANY reason, including an alias, the test fails loudly
// instead of passing over an empty tree.
func TestDeliveryLaneLatchHasOneHome(t *testing.T) {
	t.Parallel()

	root := repoRootForKVCatalogScan(t)

	var violations []string
	insideHome := 0

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "vendor", "node_modules":
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
		rel = filepath.ToSlash(rel)

		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}

		qualifiers, bare := deliveryResultQualifiers(file)
		if len(qualifiers) == 0 && !bare {
			return nil
		}

		home := strings.HasPrefix(rel, deliveryLaneHome+"/")
		forEachLatchField(file, qualifiers, bare, func(pos token.Pos) {
			if home {
				insideHome++
				return
			}
			violations = append(violations, fmt.Sprintf("%s:%d", rel, fset.Position(pos).Line))
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	// Non-vacuity: the detector must find the one latch that IS supposed to
	// exist. A matcher broken by a rename, an import-alias change, or a struct
	// shape it cannot walk would otherwise report a clean tree forever.
	if insideHome == 0 {
		t.Fatalf("the scan found no `chan natsclient.DeliveryResult` field in %s — "+
			"the detector is broken, not the tree", deliveryLaneHome)
	}

	if len(violations) > 0 {
		t.Errorf("the per-lane admission latch has one home (%s); these production files declare their own "+
			"`chan natsclient.DeliveryResult` field:\n  %s\nUse deliverylane.NewAdmission instead.",
			deliveryLaneHome, strings.Join(violations, "\n  "))
	}
}

// deliveryResultQualifiers reports EVERY local name under which a file refers
// to the natsclient package, so neither an alias nor a second aliased import of
// the same path can slip past the scan. A dot-import contributes ".". The
// second result is true when the file IS part of natsclient, where the type is
// spelled bare.
func deliveryResultQualifiers(file *ast.File) (map[string]bool, bool) {
	if file.Name != nil && file.Name.Name == "natsclient" {
		return nil, true
	}
	qualifiers := make(map[string]bool)
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != natsclientImportPath {
			continue
		}
		if spec.Name == nil {
			qualifiers["natsclient"] = true
			continue
		}
		if spec.Name.Name == "_" {
			continue
		}
		qualifiers[spec.Name.Name] = true
	}
	return qualifiers, false
}

// forEachLatchField calls report for every struct field whose type is a channel
// of natsclient.DeliveryResult, in any direction. It walks struct types
// wherever they appear — a named type, an anonymous field, a local struct in a
// function body — because the latch is a latch in all of them.
func forEachLatchField(file *ast.File, qualifiers map[string]bool, bare bool, report func(token.Pos)) {
	ast.Inspect(file, func(n ast.Node) bool {
		structType, ok := n.(*ast.StructType)
		if !ok || structType.Fields == nil {
			return true
		}
		for _, field := range structType.Fields.List {
			channel, ok := field.Type.(*ast.ChanType)
			if !ok {
				continue
			}
			if isDeliveryResult(channel.Value, qualifiers, bare) {
				report(field.Pos())
			}
		}
		return true
	})
}

// isDeliveryResult reports whether expr names natsclient.DeliveryResult under
// any of the file's own spellings of that package.
func isDeliveryResult(expr ast.Expr, qualifiers map[string]bool, bare bool) bool {
	if ident, ok := expr.(*ast.Ident); ok {
		return (bare || qualifiers["."]) && ident.Name == "DeliveryResult"
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "DeliveryResult" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && qualifiers[pkg.Name]
}

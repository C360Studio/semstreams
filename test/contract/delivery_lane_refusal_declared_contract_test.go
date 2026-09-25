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

// deliveryLaneImportPath is the package that owns NewAdmission.
const deliveryLaneImportPath = "github.com/c360studio/semstreams/internal/deliverylane"

// TestEveryProductionAdmissionDeclaresRefusal keeps every production delivery
// lane's refusals declared (#1342). jetstream-consumer-policy requires each
// delivery a closed admission refuses to emit a log line and a lane-labelled
// counter; deliverylane.NewAdmission accepts nil as its onRefused argument
// only so tests can exercise the latch alone, and a nil there in production is
// a silent drop that no behavior test of the primitive can see.
//
// The scan reads the AST of every non-test .go file and flags any call of
// NewAdmission — under every import alias of internal/deliverylane, or bare
// inside that package — whose second argument is a nil literal (bare,
// parenthesised, or converted, as in `(func(string))(nil)`) or which does not
// pass exactly two arguments. Like TestDeliveryLaneLatchHasOneHome it catches
// drift as written, not an adversary, and it does no flow analysis. Known blind
// spots: a variable that happens to hold nil, and a wrapper that takes
// onRefused as a parameter and forwards it to NewAdmission — a nil passed to
// the wrapper is invisible here. Such a wrapper must supply the declarer
// itself, as agentrun's newLaneAdmission does. A file that does not parse FAILS
// the test rather than being skipped.
func TestEveryProductionAdmissionDeclaresRefusal(t *testing.T) {
	t.Parallel()

	root := repoRootForKVCatalogScan(t)

	var violations []string
	calls := 0

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
		qualifiers, bare := deliveryLaneQualifiers(file)
		if len(qualifiers) == 0 && !bare {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isNewAdmission(call.Fun, qualifiers, bare) {
				return true
			}
			calls++
			where := fmt.Sprintf("%s:%d", rel, fset.Position(call.Pos()).Line)
			switch {
			case len(call.Args) != 2:
				violations = append(violations, where+" (NewAdmission called with "+
					strconv.Itoa(len(call.Args))+" arguments)")
			case isNilIdent(call.Args[1]):
				violations = append(violations, where+" (onRefused is nil)")
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	// Non-vacuity: production constructs lanes, so a scan that finds no call
	// has a broken matcher (a rename, an alias it cannot read), not a clean tree.
	if calls == 0 {
		t.Fatalf("the scan found no deliverylane.NewAdmission call outside tests — " +
			"the detector is broken, not the tree")
	}

	if len(violations) > 0 {
		t.Errorf("every production delivery lane must declare its refusals "+
			"(jetstream-consumer-policy: refusal by closed admission is a declared event); "+
			"these deliverylane.NewAdmission calls pass no onRefused declarer:\n  %s\n"+
			"Pass a func(subject string) that logs the refusal and increments the lane-labelled refusal counter.",
			strings.Join(violations, "\n  "))
	}
}

// deliveryLaneQualifiers reports every local name under which a file refers to
// internal/deliverylane; the second result is true inside that package, where
// NewAdmission is spelled bare.
func deliveryLaneQualifiers(file *ast.File) (map[string]bool, bool) {
	if file.Name != nil && file.Name.Name == "deliverylane" {
		return nil, true
	}
	qualifiers := make(map[string]bool)
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != deliveryLaneImportPath {
			continue
		}
		if spec.Name == nil {
			qualifiers["deliverylane"] = true
			continue
		}
		if spec.Name.Name == "_" {
			continue
		}
		qualifiers[spec.Name.Name] = true
	}
	return qualifiers, false
}

func isNewAdmission(fun ast.Expr, qualifiers map[string]bool, bare bool) bool {
	if ident, ok := fun.(*ast.Ident); ok {
		return (bare || qualifiers["."]) && ident.Name == "NewAdmission"
	}
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "NewAdmission" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && qualifiers[pkg.Name]
}

// isNilIdent reports whether expr is the nil literal, looking through
// parentheses and through a single-argument conversion such as
// `(func(string))(nil)`. A one-argument call whose argument is nil is treated as
// a conversion; a real function called with nil would be a false positive, and
// no NewAdmission call site passes one.
func isNilIdent(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == "nil"
	case *ast.ParenExpr:
		return isNilIdent(e.X)
	case *ast.CallExpr:
		return len(e.Args) == 1 && isNilIdent(e.Args[0])
	default:
		return false
	}
}

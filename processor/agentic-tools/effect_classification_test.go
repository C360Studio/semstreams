package agentictools_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// minimumClassifiedTools guards this test against becoming vacuous. If the AST
// walk stops finding literals — a parse failure, a moved package, a refactor to
// a constructor helper — the count collapses and this floor fails loudly rather
// than reporting green over an empty set. Raise it when the tool count grows
// substantially; it is a floor, not an exact count.
const minimumClassifiedTools = 40

// TestEveryFrameworkToolDeclaresAnEffect asserts that every agentic.ToolDefinition
// composite literal in the framework's own tool packages declares an Effect.
//
// WHY A SOURCE SCRAPE. The obvious alternative — build a registry via
// RegisterBuiltins and walk ListTools() — is a partial guard that looks total:
// RegisterBuiltins skips stateful tools when their deps (NATS client, rule
// manager, component registry) are nil, so a nil-dep test would silently cover
// only the stateless subset while appearing to cover everything. Scraping the
// owning packages enumerates the surface from the code that OWNS it, which is
// the correction this program adopted after two hand-maintained inventories
// drifted (#787, #792).
//
// WHAT IT DOES NOT COVER, stated rather than implied: application-supplied
// executors outside these packages. Those are third-party producers, and the
// framework's answer for them is the fail-safe — an undeclared effect resolves
// to unknown at aggregation, never to read_only. This test binds the framework's
// OWN tools, where shipping an unclassified tool would poison downstream
// approval defaulting with a catalog full of `unknown`.
func TestEveryFrameworkToolDeclaresAnEffect(t *testing.T) {
	dirs := []string{".", "executors"}

	type unclassified struct {
		pos  string
		name string
	}
	var missing []unclassified
	classified := 0

	fset := token.NewFileSet()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			// Collect the individual tool-definition literals. A
			// []agentic.ToolDefinition slice literal contributes its ELEMENTS
			// (which are usually bare `{...}` with a nil type, so they are
			// unreachable by type-matching alone); a standalone
			// agentic.ToolDefinition{...} contributes itself. Positions dedupe
			// the overlap when a slice element spells its type explicitly.
			defs := map[string]*ast.CompositeLit{}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				switch typ := lit.Type.(type) {
				case *ast.ArrayType:
					if !isToolDefinitionType(typ.Elt) {
						return true
					}
					// An empty slice literal (e.g. the registry's accumulator)
					// declares no tools and contributes nothing.
					for _, elt := range lit.Elts {
						if eltLit, ok := elt.(*ast.CompositeLit); ok {
							defs[fset.Position(eltLit.Pos()).String()] = eltLit
						}
					}
				case *ast.SelectorExpr:
					if isToolDefinitionType(typ) {
						defs[fset.Position(lit.Pos()).String()] = lit
					}
				}
				return true
			})

			for pos, lit := range defs {
				hasEffect := false
				toolName := "<unnamed>"
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok {
						continue
					}
					switch key.Name {
					case "Effect":
						hasEffect = true
					case "Name":
						toolName = exprText(kv.Value)
					}
				}
				if hasEffect {
					classified++
				} else {
					missing = append(missing, unclassified{pos: pos, name: toolName})
				}
			}
		}
	}

	if len(missing) > 0 {
		sort.Slice(missing, func(i, j int) bool { return missing[i].pos < missing[j].pos })
		var b strings.Builder
		for _, m := range missing {
			b.WriteString("\n  " + m.pos + "  tool " + m.name)
		}
		t.Errorf("%d framework tool definition(s) declare no Effect: every framework tool must classify itself "+
			"by worst effect (read_only / mutating / external_effect), because an undeclared tool resolves to "+
			"unknown and unknown is treated as maximally restrictive downstream:%s", len(missing), b.String())
	}

	if classified < minimumClassifiedTools {
		t.Errorf("found only %d classified tool definitions, expected at least %d — "+
			"the AST walk is probably not looking at what it claims to (moved package, parse failure, or a "+
			"refactor away from composite literals)", classified, minimumClassifiedTools)
	}
}

// isToolDefinitionType reports whether a composite-literal type is
// agentic.ToolDefinition. Bare `{...}` elements inside a
// []agentic.ToolDefinition slice literal have a nil type, so slice literals are
// matched on their element type and their elements are reached by the walk with
// the slice's own type node.
func isToolDefinitionType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		return ok && pkg.Name == "agentic" && t.Sel.Name == "ToolDefinition"
	case *ast.ArrayType:
		return isToolDefinitionType(t.Elt)
	default:
		return false
	}
}

func exprText(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.BasicLit:
		return strings.Trim(v.Value, `"`)
	case *ast.Ident:
		return v.Name
	default:
		return "<expr>"
	}
}

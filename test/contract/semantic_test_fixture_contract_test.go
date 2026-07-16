package contract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const semanticTestFixtureImport = "github.com/c360studio/semstreams/internal/semantictest"

// These authority packages deliberately keep raw grammar fixtures. Their tests
// cannot import semantictest because semantictest delegates back to them.
var semanticTestFixtureAuthorityCycleExemptions = map[string]string{
	"pkg/types":  "pkg/types is the entity-ID grammar authority",
	"vocabulary": "vocabulary is the predicate grammar authority",
}

func TestSemanticTestFixtureHasNoProductionImports(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".worktrees", "node_modules", "vendor":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || pathWithinTestdata(root, path) {
			return nil
		}

		importsHelper, err := semanticTestImportPolicy(path)
		if err != nil {
			return err
		}
		if importsHelper && !strings.HasSuffix(path, "_test.go") {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			t.Errorf("production Go file %s imports test-only %s", filepath.ToSlash(relative), semanticTestFixtureImport)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production Go imports: %v", err)
	}
}

func TestSemanticTestFixtureAuthorityPackagesRemainCycleFree(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	for relativeDir, reason := range semanticTestFixtureAuthorityCycleExemptions {
		relativeDir := relativeDir
		reason := reason
		t.Run(relativeDir, func(t *testing.T) {
			t.Parallel()

			err := filepath.WalkDir(filepath.Join(root, relativeDir), func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || filepath.Ext(path) != ".go" {
					return nil
				}
				importsHelper, err := semanticTestImportPolicy(path)
				if err != nil {
					return err
				}
				if importsHelper {
					relative, err := filepath.Rel(root, path)
					if err != nil {
						return err
					}
					t.Errorf("authority file %s imports semantictest (%s); keep its grammar fixtures raw to avoid a cycle", filepath.ToSlash(relative), reason)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("scan authority imports: %v", err)
			}
		})
	}
}

func TestSemanticTestFixtureImportPolicyRejectsAmbiguity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "explicit alias",
			content: `package fixture
import st "github.com/c360studio/semstreams/internal/semantictest"
var _ = st.EntityID
`,
			want: "canonical unaliased import name",
		},
		{
			name: "dot import",
			content: `package fixture
import . "github.com/c360studio/semstreams/internal/semantictest"
var _ = EntityID
`,
			want: "canonical unaliased import name",
		},
		{
			name: "local shadow",
			content: `package fixture
import "github.com/c360studio/semstreams/internal/semantictest"
func fixture() {
  semantictest := struct{ EntityID string }{}
  _ = semantictest.EntityID
}
`,
			want: "shadows the canonical",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "fixture_test.go")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := semanticTestImportPolicy(path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("semanticTestImportPolicy() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSemanticTestFixtureContractRejectsDirectEntityIDShadowing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "parameter",
			content: `package semantictest
func fixture(EntityID func(...string) string) {
  _ = EntityID("acme", "ops", "robotics", "gcs", "drone", "001")
}
`,
		},
		{
			name: "local closure",
			content: `package semantictest
func fixture() {
  EntityID := func(...string) string { return "not-authoritative" }
  _ = EntityID("acme", "ops", "robotics", "gcs", "drone", "001")
}
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "internal", "semantictest", "fixture_test.go")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := semanticTestImportPolicy(path); err == nil ||
				!strings.Contains(err.Error(), "shadows the package semantictest.EntityID") {
				t.Fatalf("semanticTestImportPolicy() error = %v, want direct-helper shadow rejection", err)
			}
		})
	}
}

func pathWithinTestdata(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == "testdata" {
			return true
		}
	}
	return false
}

func semanticTestImportPolicy(path string) (bool, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return false, fmt.Errorf("parse imports in %s: %w", path, err)
	}
	if parsed.Name.Name == "semantictest" && contractSemanticTestPackagePath(path) &&
		(contractDeclaresIdentifier(parsed, "EntityID", true) || contractImportDeclaresIdentifier(parsed, "EntityID")) {
		return false, fmt.Errorf("%s: declaration shadows the package semantictest.EntityID helper", path)
	}
	importsHelper := false
	for _, importSpec := range parsed.Imports {
		importPath, err := strconv.Unquote(importSpec.Path.Value)
		if err != nil {
			return false, fmt.Errorf("decode import in %s: %w", path, err)
		}
		if importPath != semanticTestFixtureImport {
			continue
		}
		importsHelper = true
		if importSpec.Name != nil {
			return true, fmt.Errorf(
				"%s: internal/semantictest must use its canonical unaliased import name",
				path,
			)
		}
	}
	if importsHelper && contractDeclaresIdentifier(parsed, "semantictest", false) {
		return true, fmt.Errorf("%s: declaration shadows the canonical internal/semantictest import name", path)
	}
	return importsHelper, nil
}

func contractSemanticTestPackagePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.HasPrefix(clean, "internal/semantictest/") || strings.Contains(clean, "/internal/semantictest/")
}

func contractDeclaresIdentifier(file *ast.File, wanted string, allowPackageFunction bool) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if found {
			return false
		}
		switch typed := node.(type) {
		case *ast.ValueSpec:
			found = contractIdentifiersContain(typed.Names, wanted)
		case *ast.TypeSpec:
			found = typed.Name.Name == wanted
		case *ast.FuncDecl:
			declaresFunctionName := typed.Name.Name == wanted
			if allowPackageFunction && typed.Recv == nil {
				declaresFunctionName = false
			}
			found = declaresFunctionName ||
				contractFieldListDeclares(typed.Recv, wanted) ||
				contractFieldListDeclares(typed.Type.Params, wanted) ||
				contractFieldListDeclares(typed.Type.Results, wanted)
		case *ast.FuncLit:
			found = contractFieldListDeclares(typed.Type.Params, wanted) || contractFieldListDeclares(typed.Type.Results, wanted)
		case *ast.AssignStmt:
			if typed.Tok == token.DEFINE {
				found = contractExpressionsDeclare(typed.Lhs, wanted)
			}
		case *ast.RangeStmt:
			if typed.Tok == token.DEFINE {
				found = contractExpressionDeclares(typed.Key, wanted) || contractExpressionDeclares(typed.Value, wanted)
			}
		}
		return !found
	})
	return found
}

func contractImportDeclaresIdentifier(file *ast.File, wanted string) bool {
	for _, spec := range file.Imports {
		if spec.Name != nil {
			if spec.Name.Name == wanted {
				return true
			}
			continue
		}
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err == nil && filepath.Base(importPath) == wanted {
			return true
		}
	}
	return false
}

func contractFieldListDeclares(fields *ast.FieldList, wanted string) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		if contractIdentifiersContain(field.Names, wanted) {
			return true
		}
	}
	return false
}

func contractIdentifiersContain(identifiers []*ast.Ident, wanted string) bool {
	for _, identifier := range identifiers {
		if identifier.Name == wanted {
			return true
		}
	}
	return false
}

func contractExpressionsDeclare(expressions []ast.Expr, wanted string) bool {
	for _, expression := range expressions {
		if contractExpressionDeclares(expression, wanted) {
			return true
		}
	}
	return false
}

func contractExpressionDeclares(expression ast.Expr, wanted string) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == wanted
}

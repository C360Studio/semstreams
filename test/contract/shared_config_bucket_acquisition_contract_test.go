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

// acquisitionAPIs are the two client calls that bind a KV bucket directly,
// bypassing any descriptor.
var acquisitionAPIs = map[string]bool{
	"CreateKeyValueBucket": true,
	"GetKeyValueBucket":    true,
}

// catalogSeams are the descriptor-derived acquisition entry points.
var catalogSeams = map[string]bool{
	"EnsureCatalogBucket": true,
	"OpenCatalogReader":   true,
}

// catalogResolvingOwners are the production sites that MUST resolve the shared
// configuration bucket through its descriptor. Naming them is a POSITIVE
// assertion — it can only under-claim, and under-claiming is caught by
// TestCatalogBucketNamesAreNeverAcquiredDirectly, which needs no list at all.
// Without it, an owner that silently stopped using the seam would leave both
// checks vacuous: it would name a catalog bucket, make no direct call, and pass.
var catalogResolvingOwners = []string{
	"config/manager.go",
	"processor/rule/kv_config_integration.go",
}

// TestCatalogBucketNamesAreNeverAcquiredDirectly is the structural half of the
// framework-bucket-catalog acquisition contract, checked PER CALL.
//
// A file that names a catalogued bucket must contain zero direct
// Create/GetKeyValueBucket calls — even if it also calls a catalog seam
// elsewhere. The earlier version exempted the whole FILE once it saw any seam
// reference, which is the same file-level blindness that let a bypassed branch
// pass M24: a direct call added beside an existing catalog call read as
// compliant. Per-call is the only granularity that means anything here.
func TestCatalogBucketNamesAreNeverAcquiredDirectly(t *testing.T) {
	root := repoRootForKVCatalogScan(t)
	names := catalogConstantNames(t, root)

	var violations []string
	for _, file := range productionGoFiles(t, root) {
		named := file.namedCatalogConstants(names)
		if len(named) == 0 || len(file.directAcquisitionLines) == 0 {
			continue
		}
		for _, line := range file.directAcquisitionLines {
			violations = append(violations, fmt.Sprintf(
				"%s:%d acquires a KV bucket directly while naming catalogued bucket(s) %v; use graph.EnsureCatalogBucket",
				file.rel, line, named))
		}
	}
	if len(violations) > 0 {
		t.Fatalf("a catalogued bucket must be acquired through its descriptor:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestCatalogResolvingOwnersUseTheSeam is the positive half. The structural
// check above passes vacuously for a file that stops acquiring altogether, so
// the two owners of the shared configuration bucket are asserted to still
// resolve it through the descriptor.
func TestCatalogResolvingOwnersUseTheSeam(t *testing.T) {
	root := repoRootForKVCatalogScan(t)
	byPath := make(map[string]goFile)
	for _, file := range productionGoFiles(t, root) {
		byPath[file.rel] = file
	}

	for _, rel := range catalogResolvingOwners {
		file, ok := byPath[rel]
		if !ok {
			t.Fatalf("%s is named as a catalog-resolving owner but was not scanned", rel)
		}
		if !file.usesCatalogSeam {
			t.Errorf("%s must resolve its bucket through graph.EnsureCatalogBucket", rel)
		}
	}
}

// TestGenericKVWritersConsultTheCatalog covers the acquisition path neither
// check above can see: the rule engine's update_kv writer binds whatever bucket
// a rule pack resolves to at runtime, so it names nothing statically.
//
// Scope is deliberate. Sixteen other production files acquire
// component-configured bucket names, and requiring every one to consult the
// catalog is a repo-wide contract this capability does not own. What is specific
// here is the GENERIC write surface: a bucket name arriving from an
// operator-authored rule pack rather than a composition root. Every
// bucket-acquiring file in processor/rule must therefore consult the catalog,
// derived by scanning that package. The BEHAVIOURAL guard for the same property
// is TestKVWriterRefusesCatalogedOwnerOnlyBucket in processor/rule, which is
// what bites when the branch is bypassed rather than deleted.
func TestGenericKVWritersConsultTheCatalog(t *testing.T) {
	root := repoRootForKVCatalogScan(t)

	var checked, violations []string
	for _, file := range productionGoFiles(t, root) {
		if !strings.HasPrefix(file.rel, "processor/rule/") || len(file.directAcquisitionLines) == 0 {
			continue
		}
		checked = append(checked, file.rel)
		if !file.consultsCatalog {
			violations = append(violations, fmt.Sprintf(
				"%s acquires a KV bucket for a rule-supplied name without consulting the catalog", file.rel))
		}
	}
	if len(checked) == 0 {
		t.Fatal("no bucket-acquiring file found in processor/rule — the scan would vacuously pass")
	}
	if len(violations) > 0 {
		t.Fatalf("the rule engine's KV acquisition must consult the catalog:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// goFile is one scanned production file.
type goFile struct {
	rel                    string
	selectors              map[string]bool
	stringLiterals         map[string]bool
	directAcquisitionLines []int
	usesCatalogSeam        bool
	consultsCatalog        bool
}

// namedCatalogConstants returns the catalogued bucket names this file spells,
// by graph constant reference or bare literal.
func (f goFile) namedCatalogConstants(names map[string]string) []string {
	var named []string
	for ident := range f.selectors {
		if value, ok := names[ident]; ok {
			named = append(named, value)
		}
	}
	for literal := range f.stringLiterals {
		for _, value := range names {
			if literal == value {
				named = append(named, literal)
			}
		}
	}
	return named
}

// catalogConstantNames maps each graph.Bucket* constant IDENTIFIER to its value,
// for the constants whose value is in the catalog. Parsed from the declaration
// rather than guessed from a name prefix, so an unrelated Bucket-prefixed
// selector cannot false-positive.
func catalogConstantNames(t *testing.T, root string) map[string]string {
	t.Helper()
	catalogued := make(map[string]bool)
	for _, spec := range graph.KVCatalog() {
		catalogued[spec.Name] = true
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, "graph/constants.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse graph/constants.go: %v", err)
	}
	names := make(map[string]string)
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, ident := range spec.Names {
			if i >= len(spec.Values) {
				continue
			}
			lit, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, uerr := strconv.Unquote(lit.Value)
			if uerr == nil && catalogued[value] {
				names[ident.Name] = value
			}
		}
		return true
	})
	if len(names) == 0 {
		t.Fatal("no catalogued bucket constants resolved — the scan would vacuously pass")
	}
	return names
}

// productionGoFiles scans every non-test production Go file. natsclient IS the
// acquisition mechanism and graph owns the catalog, so both are excluded.
func productionGoFiles(t *testing.T, root string) []goFile {
	t.Helper()
	var found []goFile
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "test", "node_modules":
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
		if strings.HasPrefix(rel, "natsclient/") || strings.HasPrefix(rel, "graph/") {
			return nil
		}
		parsed, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		file := goFile{rel: rel, selectors: map[string]bool{}, stringLiterals: map[string]bool{}}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if unquoted, uerr := strconv.Unquote(n.Value); uerr == nil {
						file.stringLiterals[unquoted] = true
					}
				}
			case *ast.SelectorExpr:
				file.selectors[n.Sel.Name] = true
			case *ast.CallExpr:
				selector, ok := n.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch {
				case acquisitionAPIs[selector.Sel.Name]:
					file.directAcquisitionLines = append(file.directAcquisitionLines, fset.Position(n.Pos()).Line)
				case catalogSeams[selector.Sel.Name]:
					file.usesCatalogSeam = true
					file.consultsCatalog = true
				case selector.Sel.Name == "SpecFor" || selector.Sel.Name == "IsFrameworkOwnedBucket":
					file.consultsCatalog = true
				}
			}
			return true
		})
		found = append(found, file)
		return nil
	})
	if err != nil {
		t.Fatalf("scan production files: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("no production file scanned — the checks would vacuously pass")
	}
	return found
}

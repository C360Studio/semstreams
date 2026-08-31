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

// acquisitionAPIs are the two client calls that bind a KV bucket.
var acquisitionAPIs = map[string]bool{
	"CreateKeyValueBucket": true,
	"GetKeyValueBucket":    true,
}

// catalogSeams are the descriptor-derived acquisition entry points. A file that
// uses one is resolving policy from the catalog rather than spelling its own.
var catalogSeams = map[string]bool{
	"EnsureCatalogBucket":    true,
	"OpenCatalogReader":      true,
	"SpecFor":                true,
	"IsFrameworkOwnedBucket": true,
}

// TestCatalogBucketsAreAcquiredThroughTheirDescriptor derives the acquirer set
// instead of naming it. Every production Go file that binds a KV bucket is
// found by scanning, and a file that NAMES a catalogued bucket must resolve it
// through the catalog seam rather than its own jetstream.KeyValueConfig.
//
// The sibling literal scan (TestNoCatalogBucketLiteralsOutsideTheCatalog) stops
// the bucket NAME forking. This stops the POLICY forking, which a file
// referencing graph.Bucket* with its own config would still do — the shared
// configuration bucket's retention guarantee would then hold only for whichever
// creator won the race.
//
// Deriving the set is the point: the hand-written two-file list this replaced
// could not see a third acquisition path, and one existed
// (processor/rule/kv_writer.go, found in review). A new acquirer anywhere in
// production is now scanned automatically.
func TestCatalogBucketsAreAcquiredThroughTheirDescriptor(t *testing.T) {
	root := repoRootForKVCatalogScan(t)
	catalogued := make(map[string]bool)
	for _, spec := range graph.KVCatalog() {
		catalogued[spec.Name] = true
	}

	var violations []string
	for _, file := range productionBucketAcquirers(t, root) {
		if !file.namesCatalogBucket(catalogued) || file.usesCatalogSeam {
			continue
		}
		for _, line := range file.acquisitionLines {
			violations = append(violations, fmt.Sprintf(
				"%s:%d names a catalogued bucket and acquires it directly; use graph.EnsureCatalogBucket",
				file.rel, line))
		}
	}
	if len(violations) > 0 {
		t.Fatalf("catalogued buckets must be acquired through their descriptor:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestGenericKVWritersConsultTheCatalog covers the acquisition path the
// name-based check above cannot see: the rule engine's update_kv writer binds
// whatever bucket a rule pack supplies AFTER variable substitution, so it names
// nothing statically and can resolve to a catalogued bucket at runtime.
//
// Scope is deliberate. Sixteen other production files acquire
// component-configured bucket names, and requiring every one of them to consult
// the catalog is a repo-wide contract this capability does not own. What is
// specific here is the GENERIC write surface: a bucket name that arrives from an
// operator-authored rule pack rather than from a composition root. Every
// bucket-acquiring file in processor/rule is therefore required to consult the
// catalog — derived by scanning that package, so a second generic writer added
// beside this one trips the test rather than slipping past a hand-written list.
func TestGenericKVWritersConsultTheCatalog(t *testing.T) {
	root := repoRootForKVCatalogScan(t)

	var checked, violations []string
	for _, file := range productionBucketAcquirers(t, root) {
		if !strings.HasPrefix(file.rel, "processor/rule/") {
			continue
		}
		checked = append(checked, file.rel)
		if !file.usesCatalogSeam {
			violations = append(violations, fmt.Sprintf(
				"%s acquires a KV bucket for a rule-supplied name without consulting the catalog; "+
					"a catalogued bucket must resolve through graph.EnsureCatalogBucket", file.rel))
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

// bucketAcquirer is one production file that binds KV buckets.
type bucketAcquirer struct {
	rel              string
	identifiers      map[string]bool
	stringLiterals   map[string]bool
	acquisitionLines []int
	usesCatalogSeam  bool
}

// namesCatalogBucket reports whether the file references a catalogued bucket by
// its graph.Bucket* constant or as a bare literal.
func (f bucketAcquirer) namesCatalogBucket(catalogued map[string]bool) bool {
	for literal := range f.stringLiterals {
		if catalogued[literal] {
			return true
		}
	}
	for ident := range f.identifiers {
		if strings.HasPrefix(ident, "Bucket") {
			return true
		}
	}
	return false
}

// productionBucketAcquirers scans the repository for non-test production files
// that bind a KV bucket. natsclient is excluded: it IS the acquisition
// mechanism, and graph owns the catalog itself.
func productionBucketAcquirers(t *testing.T, root string) []bucketAcquirer {
	t.Helper()
	var found []bucketAcquirer
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
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		acquirer := bucketAcquirer{
			rel:            rel,
			identifiers:    map[string]bool{},
			stringLiterals: map[string]bool{},
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if unquoted, uerr := strconv.Unquote(n.Value); uerr == nil {
						acquirer.stringLiterals[unquoted] = true
					}
				}
			case *ast.SelectorExpr:
				acquirer.identifiers[n.Sel.Name] = true
			case *ast.CallExpr:
				selector, ok := n.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if acquisitionAPIs[selector.Sel.Name] {
					acquirer.acquisitionLines = append(acquirer.acquisitionLines, fset.Position(n.Pos()).Line)
				}
				if catalogSeams[selector.Sel.Name] {
					acquirer.usesCatalogSeam = true
				}
			}
			return true
		})
		if len(acquirer.acquisitionLines) > 0 {
			found = append(found, acquirer)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan for bucket acquirers: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("no bucket acquirer found — the scan would vacuously pass")
	}
	return found
}

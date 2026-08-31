package contract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// sharedConfigBucketAcquirers are the two production sites that provision the
// shared runtime configuration bucket. Both must resolve it through the ONE
// framework-bucket-catalog descriptor.
//
// The bucket is in the catalog for its RETENTION guarantee: since ADR-104 it
// holds the create-once platform identity record, and an evicted identity is
// reminted as a second authority ADR-102 decision 7 forbids reconciling. Two
// creators each spelling their own jetstream.KeyValueConfig is precisely the
// split-owner shape the catalog exists to remove — the guarantee would hold
// only for whichever of them happened to win the create race.
//
// The sibling literal scan (TestNoCatalogBucketLiteralsOutsideTheCatalog) stops
// the NAME forking; this stops the POLICY forking, which a file using
// graph.BucketSemStreamsConfig with its own config would still do.
var sharedConfigBucketAcquirers = []string{
	"config/manager.go",
	"processor/rule/kv_config_integration.go",
}

// TestSharedConfigBucketResolvesThroughOneDescriptor fails if either acquirer
// creates or opens the bucket itself instead of going through the catalog seam.
func TestSharedConfigBucketResolvesThroughOneDescriptor(t *testing.T) {
	root := repoRootForKVCatalogScan(t)
	fset := token.NewFileSet()

	var violations []string
	for _, rel := range sharedConfigBucketAcquirers {
		file, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		sawCatalogSeam := false
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "EnsureCatalogBucket", "OpenCatalogReader":
				sawCatalogSeam = true
			case "CreateKeyValueBucket", "GetKeyValueBucket", "CreateKeyValue", "KeyValue":
				violations = append(violations, fmt.Sprintf(
					"%s:%d acquires a KV bucket directly (%s); the shared configuration bucket must resolve through graph.EnsureCatalogBucket",
					rel, fset.Position(call.Pos()).Line, selector.Sel.Name))
			}
			return true
		})
		if !sawCatalogSeam {
			violations = append(violations, fmt.Sprintf(
				"%s no longer resolves the shared configuration bucket through the catalog seam", rel))
		}
	}

	if len(violations) > 0 {
		t.Fatalf("the shared configuration bucket must have exactly one acquisition policy:\n  %v", violations)
	}
}

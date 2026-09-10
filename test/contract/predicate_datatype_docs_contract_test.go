package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docDataTypeLiteral matches a documentation code example that passes a string
// literal to WithDataType or sets the DataType struct field to one.
var docDataTypeLiteral = regexp.MustCompile(`(?:WithDataType\(|DataType:\s*)"([^"]*)"`)

// TestDocumentationNeverShowsARefusedDataType walks every markdown file in the
// repository and fails when a code example declares a datatype the registry
// refuses.
//
// This guard exists because the same defect shipped twice on gh#1267, both
// times through a sweep that matched the shape already being repaired rather
// than the value that actually breaks:
//
//   - task 10.6 swept `// DataType: <legacy>` annotations and reported the
//     repository clean, while four surfaces still described normalization in
//     prose;
//   - task 11.1 swept the prose, and reported clean while ten copyable code
//     examples in the onboarding tutorials still passed "float64",
//     "entity_ref" and "timestamp" — values this change had just made fatal.
//
// A tutorial is the one surface where a wrong value is not a typo. Before
// gh#1267 these literals were inert; now Register panics on them, so a reader
// following docs/basics/05-first-processor.md verbatim gets a boot panic. Go
// code is covered by the compiler once the constants are used, and markdown is
// covered by nothing at all — which is why this walks the docs rather than the
// package.
func TestDocumentationNeverShowsARefusedDataType(t *testing.T) {
	var offenders []string
	scanned := 0

	// test/contract is two levels below the repository root. The denominator
	// assertion below is what caught this being ".." on the first attempt.
	root := filepath.Join("..", "..")

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Archived changes record the defect in prose on purpose, and
			// vendored trees are not ours to correct.
			switch info.Name() {
			case ".git", "node_modules", "vendor", "archive":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, readErr := os.ReadFile(path) //nolint:gosec // repository-relative walk
		if readErr != nil {
			return readErr
		}
		scanned++
		for _, m := range docDataTypeLiteral.FindAllStringSubmatch(string(body), -1) {
			if !isCanonicalDataType(m[1]) {
				offenders = append(offenders, path+": "+m[0])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository for markdown: %v", err)
	}

	// Denominator: a walk that found no markdown would report no offenders.
	if scanned < 50 {
		t.Fatalf("scanned only %d markdown files; the walk is not reaching the docs tree", scanned)
	}

	if len(offenders) > 0 {
		t.Errorf("documentation shows datatypes the registry refuses — a reader who copies these gets a boot "+
			"panic:\n  %s\nDeclare one of %v, and prefer the exported constant over a string literal so the "+
			"example cannot go stale again.", strings.Join(offenders, "\n  "), canonicalDataTypes)
	}
}

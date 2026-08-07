package contract

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var retiredComponentStatusTerms = []string{
	"COMPONENT_" + "STATUS",
	"BucketComponent" + "Status",
	"Lifecycle" + "Reporter",
	"Report" + "Stage",
	"ReportCycle" + "Start",
	"ReportCycle" + "Complete",
	"ReportCycle" + "Error",
	"Create NATS KV bucket for lifecycle " + "reporting",
	"Report \"idle\" stage to lifecycle " + "tracker",
	"Lifecycle " + "reporting tracks degraded states for observability",
}

// TestComponentStatusPlaneRemainsRetired prevents production code, live
// configuration, and current documentation from recreating the deleted
// component-stage diagnostic plane. Historical decisions and proposals are
// evidence and deliberately remain searchable.
func TestComponentStatusPlaneRemainsRetired(t *testing.T) {
	t.Parallel()

	root := repoRootForComponentStatusRetirement(t)
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			switch rel {
			case ".git", "vendor", "node_modules", "docs/adr", "docs/proposals", "openspec/changes/archive":
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		productionGo := ext == ".go" && !strings.HasSuffix(path, "_test.go")
		currentTruth := ext == ".md" || ext == ".json" || ext == ".yaml" || ext == ".yml"
		if !productionGo && !currentTruth {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, term := range retiredComponentStatusTerms {
			if strings.Contains(string(body), term) {
				violations = append(violations, rel+" contains "+term)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan retired component-status plane: %v", err)
	}

	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("retired component-status surfaces reappeared:\n  %s", strings.Join(violations, "\n  "))
	}
}

func repoRootForComponentStatusRetirement(t *testing.T) string {
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
			t.Fatal("go.mod not found walking up from contract test")
		}
		dir = parent
	}
}

package contract_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreCompositionDependencyClosure(t *testing.T) {
	dependencies := listDependencies(t, "./test/fixtures/corecomposition")

	for _, forbidden := range []string{
		"/frameworkcapabilities/graphresearch",
		"/frameworkadapters/otel",
		"/agentic/research",
		"/processor/research-graph-",
		"/output/otel",
		"/examples/",
		"/input/github-webhook",
		"/message/oms",
	} {
		if strings.Contains(dependencies, forbidden) {
			t.Errorf("core dependency closure contains forbidden package fragment %q", forbidden)
		}
	}
}

// TestProductionRootClosureHoldsNoE2EHarness is the link-time half of the one
// composition root (#1301): E2E-only registrations and hooks are boot options,
// never build tags, so what keeps them out of the shipped binary is its import
// closure. The production root links no harness, E2E boot, probe, example,
// fixture or mission package; the E2E root links every hook package, so none is
// stranded where no binary reaches it.
func TestProductionRootClosureHoldsNoE2EHarness(t *testing.T) {
	const module = "github.com/c360studio/semstreams/"

	production := packageSet(listDependencies(t, "./cmd/semstreams"))
	if !production[module+"internal/boot"] {
		t.Fatalf("production closure does not hold internal/boot; the closure was not read: %d packages", len(production))
	}
	for pkg := range production {
		for _, forbidden := range []string{
			"test/e2e/harness",
			"internal/e2eboot",
			"internal/e2eslowconsumer",
			"examples/processors",
			"cmd/e2e-semstreams",
		} {
			if pkg == module+forbidden || strings.HasPrefix(pkg, module+forbidden+"/") {
				t.Errorf("production binary links %s (forbidden: %s)", pkg, forbidden)
			}
		}
	}

	harness := strings.Fields(listPackages(t, "./test/e2e/harness/..."))
	if len(harness) == 0 {
		t.Fatal("test/e2e/harness holds no packages; the hook sweep would pass vacuously")
	}
	e2e := packageSet(listDependencies(t, "./cmd/e2e-semstreams"))
	required := append(harness,
		module+"internal/e2eboot",
		module+"internal/e2eslowconsumer",
		module+"cmd/e2e-semstreams/fixtures",
		module+"cmd/e2e-semstreams/mission",
	)
	for _, pkg := range required {
		if !e2e[pkg] {
			t.Errorf("E2E binary does not link %s, so the hook it carries is stranded", pkg)
		}
	}
}

func packageSet(list string) map[string]bool {
	set := map[string]bool{}
	for _, pkg := range strings.Fields(list) {
		set[pkg] = true
	}
	return set
}

func listPackages(t *testing.T, pattern string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	cmd := exec.Command("go", "list", pattern)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", pattern, err, output)
	}
	return string(output)
}

func listDependencies(t *testing.T, packagePath string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	cmd := exec.Command("go", "list", "-deps", packagePath)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+t.TempDir())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list core composition: %v\n%s", err, output)
	}

	return string(output)
}

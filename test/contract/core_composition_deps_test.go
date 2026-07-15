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

func TestProductionBinaryExcludesExamplePackages(t *testing.T) {
	dependencies := listDependencies(t, "./cmd/semstreams")
	if strings.Contains(dependencies, "/examples/") {
		t.Fatal("production binary dependency closure contains example packages")
	}
}

func listDependencies(t *testing.T, packagePath string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	cmd := exec.Command("go", "list", "-deps", "./test/fixtures/corecomposition")
	cmd.Args[len(cmd.Args)-1] = packagePath
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+t.TempDir())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list core composition: %v\n%s", err, output)
	}

	return string(output)
}

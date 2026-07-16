package entityidaudit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditRepositoryTrackedSetIgnoresDirtyRuntimeArtifacts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, ".gitignore", "runtime/\n")
	writeFixture(t, root, "tracked.go", `package fixture
var _ = EntityState{ID: "acme.ops.robotics.gcs.drone.001"}
`)
	writeFixture(t, root, "docs/operations/28-entity-id-source-corpus.json", `{"entity_id":"bad"}`)
	runGit(t, root, "init")
	runGit(t, root, "add", ".gitignore", "tracked.go", "docs/operations/28-entity-id-source-corpus.json")

	before, err := AuditRepositoryFull(root, false)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "runtime/generated.json", `{"entity_id":"bad"}`)
	writeFixture(t, root, "scratch.json", `{"entity_id":"bad"}`)
	after, err := AuditRepositoryFull(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Candidates) != len(after.Candidates) || len(before.Findings) != len(after.Findings) {
		t.Fatalf("tracked audit changed: before=%#v after=%#v", before, after)
	}
	if len(before.Candidates) != 1 || len(before.Findings) != 0 {
		t.Fatalf("tracked audit = %#v, checked report must be explicitly self-excluded", before)
	}
	withUntracked, err := AuditRepositoryFull(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(withUntracked.Findings) != len(after.Findings)+1 {
		t.Fatalf("include-untracked findings = %d, want %d; ignored runtime file must stay excluded", len(withUntracked.Findings), len(after.Findings)+1)
	}
}

func TestAuditRepositoryFullWithAbsoluteRootReportsRepositoryRelativeSurfaces(t *testing.T) {
	if testing.Short() {
		t.Skip("live repository inventory")
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err := filepath.Abs(filepath.Join(workingDirectory, "../.."))
	if err != nil {
		t.Fatal(err)
	}
	result, err := AuditRepositoryFull(repositoryRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Surfaces) == 0 {
		t.Fatal("absolute-root audit returned no surfaces")
	}
	for _, surface := range result.Surfaces {
		if filepath.IsAbs(surface.File) || surface.File == ".." || strings.HasPrefix(surface.File, "../") {
			t.Fatalf("surface path %q is not repository-relative", surface.File)
		}
	}
}

func TestNormalizeSurfacePathsMakesAbsoluteFixturePathRepositoryRelative(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "nested", "surface.go")
	surfaces := []AuditedSurface{{File: path, Kind: "direct-split", Name: "strings.Split in parse"}}
	if err := normalizeSurfacePaths(root, surfaces); err != nil {
		t.Fatal(err)
	}
	if got, want := surfaces[0].File, "nested/surface.go"; got != want {
		t.Fatalf("surface path = %q, want %q", got, want)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", filepath.Clean(root)}, args...)
	if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

package entityidaudit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAuditRepositoryTrackedSetIgnoresDirtyRuntimeArtifacts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, ".gitignore", "runtime/\n")
	writeFixture(t, root, "tracked_test.go", `package fixture
var _ = EntityState{ID: "acme.ops.robotics.gcs.drone.001"}
`)
	runGit(t, root, "init")
	runGit(t, root, "add", ".gitignore", "tracked_test.go")

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
		t.Fatalf("tracked audit = %#v, want only the tracked canonical source", before)
	}
	withUntracked, err := AuditRepositoryFull(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(withUntracked.Findings) != len(after.Findings)+1 {
		t.Fatalf("include-untracked findings = %d, want %d; ignored runtime file must stay excluded", len(withUntracked.Findings), len(after.Findings)+1)
	}
}

func TestAuditRepositoryFullWithAbsoluteRootReportsRepositoryRelativeCandidates(t *testing.T) {
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
	if len(result.Candidates) == 0 {
		t.Fatal("absolute-root audit returned no candidates")
	}
	for _, candidate := range result.Candidates {
		if filepath.IsAbs(candidate.File) {
			t.Fatalf("candidate path %q is not repository-relative", candidate.File)
		}
	}
}

func TestAuditRepositorySkipsUnstagedAndStagedTrackedDeletions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "tracked.go", `package fixture
var _ = EntityState{ID: "acme.ops.robotics.gcs.drone.001"}
`)
	runGit(t, root, "init")
	runGit(t, root, "add", "tracked.go")

	if err := os.Remove(filepath.Join(root, "tracked.go")); err != nil {
		t.Fatal(err)
	}
	unstaged, err := AuditRepositoryFull(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(unstaged.Candidates) != 0 || len(unstaged.Findings) != 0 {
		t.Fatalf("unstaged deletion audit = %#v, want empty", unstaged)
	}

	runGit(t, root, "add", "-u")
	staged, err := AuditRepositoryFull(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged.Candidates) != 0 || len(staged.Findings) != 0 {
		t.Fatalf("staged deletion audit = %#v, want empty", staged)
	}
}

func TestAuditRepositoryNormalizesFindingPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "bad.go", `package fixture
var _ = EntityState{ID: "bad"}
`)
	runGit(t, root, "init")
	runGit(t, root, "add", "bad.go")

	result, err := AuditRepositoryFull(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].File != "bad.go" {
		t.Fatalf("findings = %#v, want one repository-relative path", result.Findings)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", filepath.Clean(root)}, args...)
	if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseArtifactsReportInjectedVersions(t *testing.T) {
	root := repositoryRoot(t)
	cache := filepath.Join(t.TempDir(), "go-build")
	const version = "v9.8.7-release-smoke"

	tests := []struct {
		name       string
		pkg        string
		ldflags    string
		wantOutput string
	}{
		{
			name:       "production",
			pkg:        "./cmd/semstreams",
			ldflags:    "-X main.Version=" + version + " -X main.GitCommit=release-smoke-commit -X main.BuildTime=2026-07-17T00:00:00Z",
			wantOutput: "semstreams version " + version,
		},
		{
			name:       "e2e",
			pkg:        "./cmd/e2e-semstreams",
			ldflags:    "-X main.Version=" + version + " -X main.BuildTime=2026-07-17T00:00:00Z",
			wantOutput: "e2e-semstreams version " + version,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "semstreams")
			cmd := exec.Command("go", "build", "-ldflags", tt.ldflags, "-o", binary, tt.pkg)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOCACHE="+cache)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build release-smoke artifact: %v\n%s", err, output)
			}

			output, err := exec.Command(binary, "--version").CombinedOutput()
			if err != nil {
				t.Fatalf("run release-smoke artifact: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), tt.wantOutput) {
				t.Fatalf("--version output does not contain %q:\n%s", tt.wantOutput, output)
			}
		})
	}
}

func TestReleaseBuildersInjectSemStreamsBuildVariables(t *testing.T) {
	root := repositoryRoot(t)
	workflow := readFile(t, filepath.Join(root, ".github/workflows/release.yml"))
	assertBuildVariables(t, ".github/workflows/release.yml", workflow, []string{"main.Version", "main.GitCommit", "main.BuildTime"})

	dockerfile := readFile(t, filepath.Join(root, "docker/Dockerfile"))
	productionBuild := textBetween(t, dockerfile, "# Build main binary with version information", "# Build e2e-semstreams binary")
	assertBuildVariables(t, "docker/Dockerfile production build", productionBuild, []string{"main.Version", "main.GitCommit", "main.BuildTime"})
	e2eBuild := textBetween(t, dockerfile, "# Build e2e-semstreams binary", "# ============================================================================\n# Production Stage")
	assertBuildVariables(t, "docker/Dockerfile e2e build", e2eBuild, []string{"main.Version", "main.BuildTime"})

	for _, staleSymbol := range []string{"main.version", "main.commit", "main.date", "main.buildDate"} {
		for name, contents := range map[string]string{".github/workflows/release.yml": workflow, "docker/Dockerfile": dockerfile} {
			if strings.Contains(contents, "-X "+staleSymbol+"=") {
				t.Errorf("%s still injects stale symbol %s", name, staleSymbol)
			}
		}
	}
}

func TestE2ETasksPropagateScenarioFailuresAndCoverAgenticTier(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{
		"Taskfile.yml",
		"taskfiles/e2e/core.yml",
		"taskfiles/e2e/structural.yml",
		"taskfiles/e2e/statistical.yml",
		"taskfiles/e2e/semantic.yml",
		"taskfiles/e2e/agentic.yml",
	} {
		contents := readFile(t, filepath.Join(root, name))
		if strings.Contains(contents, "ignore_error: true") {
			t.Errorf("%s contains ignore_error: true, which can hide a failed E2E scenario", name)
		}
	}

	assertCleanupBeforeUp(t, "Taskfile.yml e2e:tier", taskBlock(t, readFile(t, filepath.Join(root, "Taskfile.yml")), "e2e:tier"))
	assertCleanupBeforeUp(t, "core default", taskBlock(t, readFile(t, filepath.Join(root, "taskfiles/e2e/core.yml")), "default"))
	assertCleanupBeforeUp(t, "structural default", taskBlock(t, readFile(t, filepath.Join(root, "taskfiles/e2e/structural.yml")), "default"))
	assertCleanupBeforeUp(t, "statistical default", taskBlock(t, readFile(t, filepath.Join(root, "taskfiles/e2e/statistical.yml")), "default"))
	semanticTaskfile := readFile(t, filepath.Join(root, "taskfiles/e2e/semantic.yml"))
	for _, taskName := range []string{"default", "fallback", "compare:statistical", "compare:semantic"} {
		assertCleanupBeforeUp(t, "semantic "+taskName, taskBlock(t, semanticTaskfile, taskName))
	}
	assertCleanupBeforeUp(t, "agentic default", taskBlock(t, readFile(t, filepath.Join(root, "taskfiles/e2e/agentic.yml")), "default"))

	rootTaskfile := readFile(t, filepath.Join(root, "Taskfile.yml"))
	allTaskStart := strings.Index(rootTaskfile, "  e2e:all:")
	if allTaskStart < 0 {
		t.Fatal("Taskfile.yml does not define e2e:all")
	}
	allTask := rootTaskfile[allTaskStart:]
	if !strings.Contains(allTask, "- task: e2e:agentic") {
		t.Error("e2e:all does not run the agentic tier")
	}
	if !strings.Contains(rootTaskfile, "- task: test:integration") {
		t.Error("check:push does not reuse the -p 2 integration task")
	}
}

func TestStatisticalProfileUsesBinaryThatRegistersFixtureProcessors(t *testing.T) {
	root := repositoryRoot(t)
	config := readFile(t, filepath.Join(root, "configs/statistical.json"))
	for _, fixtureProcessor := range []string{"document_processor", "iot_sensor"} {
		if !strings.Contains(config, `"`+fixtureProcessor+`"`) {
			t.Fatalf("configs/statistical.json no longer enables fixture processor %q; update this composition guard", fixtureProcessor)
		}
	}

	compose := readFile(t, filepath.Join(root, "docker/compose/tiered.yml"))
	statisticalService := textBetween(
		t,
		compose,
		"  # SemStreams Application (statistical tier config)",
		"  # SemStreams Application (structural tier config)",
	)
	if !strings.Contains(statisticalService, "target: e2e") {
		t.Error("statistical profile must use the e2e target that registers document_processor and iot_sensor")
	}
	if !strings.Contains(statisticalService, "image: c360studio/semstreams:e2e-test") {
		t.Error("statistical profile must use the per-target e2e image tag")
	}
}

func TestPublishedContainerUsesProductionTarget(t *testing.T) {
	root := repositoryRoot(t)
	workflow := readFile(t, filepath.Join(root, ".github/workflows/container.yml"))
	buildPushStep := textBetween(
		t,
		workflow,
		"      - name: Build and push container",
		"      - name: Image digest",
	)
	if !strings.Contains(buildPushStep, "uses: docker/build-push-action@") {
		t.Fatal("container publishing step no longer uses docker/build-push-action; update this release guard")
	}
	if !strings.Contains(buildPushStep, "\n          target: production\n") {
		t.Error("published container must explicitly select the Dockerfile production target")
	}
}

func TestDockerfileProductionExamplesSelectProductionTarget(t *testing.T) {
	root := repositoryRoot(t)
	dockerfile := readFile(t, filepath.Join(root, "docker/Dockerfile"))
	for _, example := range []string{
		"docker build --target production -t semstreams:latest .",
		"docker buildx build --target production --platform linux/amd64,linux/arm64",
	} {
		if !strings.Contains(dockerfile, example) {
			t.Errorf("Dockerfile production usage example must contain %q", example)
		}
	}
}

func assertBuildVariables(t *testing.T, name, contents string, symbols []string) {
	t.Helper()
	for _, symbol := range symbols {
		if !strings.Contains(contents, "-X "+symbol+"=") {
			t.Errorf("%s does not inject %s", name, symbol)
		}
	}
}

func assertCleanupBeforeUp(t *testing.T, name, contents string) {
	t.Helper()
	lines := strings.Split(contents, "\n")
	foundUp := false
	for i, line := range lines {
		if !strings.Contains(line, "docker compose") || !strings.Contains(line, " up ") {
			continue
		}
		foundUp = true
		if i == 0 || !strings.Contains(lines[i-1], "- defer: docker compose") {
			t.Errorf("%s does not register deferred cleanup immediately before compose up", name)
			continue
		}
		upTarget := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- ")), " up ", 2)[0]
		deferCommand := strings.TrimPrefix(strings.TrimSpace(lines[i-1]), "- defer: ")
		downTarget := strings.SplitN(deferCommand, " down ", 2)[0]
		if upTarget != downTarget {
			t.Errorf("%s defers cleanup for %q before starting %q", name, downTarget, upTarget)
		}
	}
	if !foundUp {
		t.Errorf("%s does not contain a compose up command", name)
	}
}

func taskBlock(t *testing.T, contents, taskName string) string {
	t.Helper()
	lines := strings.Split(contents, "\n")
	start := -1
	for i, line := range lines {
		if line == "  "+taskName+":" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("task %s not found", taskName)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if len(lines[i]) > 2 && strings.HasPrefix(lines[i], "  ") && lines[i][2] != ' ' {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func textBetween(t *testing.T, contents, startMarker, endMarker string) string {
	t.Helper()
	start := strings.Index(contents, startMarker)
	if start < 0 {
		t.Fatalf("start marker %q not found", startMarker)
	}
	rest := contents[start:]
	end := strings.Index(rest, endMarker)
	if end < 0 {
		t.Fatalf("end marker %q not found", endMarker)
	}
	return rest[:end]
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}

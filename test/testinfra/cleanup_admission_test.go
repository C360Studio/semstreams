//go:build unix

package testinfra_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// These tests execute the real Task definitions and integration runner. The Go
// shim invokes this test binary's real fixture analysis for the guard command;
// the first later expensive command records admission and deliberately exits.
func TestCleanupAdmissionEntryPoints(t *testing.T) {
	taskBinary, err := exec.LookPath("task")
	if err != nil {
		t.Fatal("task is required to verify the actual verification entry points: ", err)
	}
	repo := findRepoRoot(t)
	entries := []struct {
		name string
		args []string
	}{
		{"lint", []string{taskBinary, "--silent", "lint"}},
		{"test", []string{taskBinary, "--silent", "test"}},
		{"race", []string{taskBinary, "--silent", "test:race"}},
		{"live", []string{taskBinary, "--silent", "test:live"}},
		{"check", []string{taskBinary, "--silent", "check"}},
		{"check-push", []string{taskBinary, "--silent", "check:push"}},
		{"integration", []string{taskBinary, "--silent", "test:integration"}},
		{"runner-default", []string{"bash", "scripts/run-integration-tests.sh"}},
		{"runner-explicit", []string{"bash", "scripts/run-integration-tests.sh", "./..."}},
		{"ci-test", []string{"bash", "-c", cleanupCITestCommand(t, repo)}},
	}
	for _, kind := range []string{"clean", "new-debt", "stale", "unknown", "load-failure"} {
		t.Run(kind, func(t *testing.T) {
			for _, entry := range entries {
				// The canonical path proves every classification outcome. Other paths
				// prove uniform wrapper propagation with clean and new-debt witnesses.
				if entry.name != "runner-default" && kind != "clean" && kind != "new-debt" {
					continue
				}
				t.Run(entry.name, func(t *testing.T) {
					root := cleanupAdmissionWorkspace(t, repo, kind)
					result := runCleanupAdmission(t, root, entry.args)
					output, runErr := result.Output, result.CommandError
					var observed cleanupAdmissionObservation
					body, err := os.ReadFile(filepath.Join(root, "guard-observation.json"))
					if err != nil {
						t.Fatalf("entry point did not execute real fixture analysis: %v; command error=%v\n%s", err, runErr, output)
					}
					if err := json.Unmarshal(body, &observed); err != nil {
						t.Fatal(err)
					}
					if kind == "clean" {
						if observed.Error != "" {
							t.Fatalf("clean fixture refused: %s\n%s", observed.Error, output)
						}
						if _, err := os.Stat(filepath.Join(root, "expensive-command")); err != nil {
							t.Fatalf("clean fixture never admitted subsequent command: %v\n%s", err, output)
						}
						return
					}
					if runErr == nil || observed.Error == "" {
						t.Fatalf("%s fixture admitted: observed=%+v, error=%v\n%s", kind, observed, runErr, output)
					}
					assertCleanupFixtureFailure(t, kind, observed)
					if entry.name == "runner-default" {
						if result.ExitCode != 1 {
							t.Errorf("canonical %s rejection status = %d, want uniform status 1", kind, result.ExitCode)
						}
						t.Logf("canonical %s rejection exit status: %d", kind, result.ExitCode)
					}
					for _, sentinel := range []string{"expensive-command", "lock-acquired"} {
						if _, err := os.Stat(filepath.Join(root, sentinel)); !os.IsNotExist(err) {
							t.Errorf("refused %s reached %s: stat=%v\n%s", kind, sentinel, err, output)
						}
					}
				})
			}
		})
	}
}

func TestCleanupAdmissionCILintWiring(t *testing.T) {
	commands := cleanupCICommands(t, findRepoRoot(t), "lint")
	if len(commands) == 0 || strings.TrimSpace(commands[0]) != "scripts/check-cleanup-roots.sh" {
		t.Fatalf("CI lint must run the shared cleanup guard before its existing commands: %q", commands)
	}
}

func TestCleanupAdmissionFocusedRunner(t *testing.T) {
	root := cleanupAdmissionWorkspace(t, findRepoRoot(t), "new-debt")
	result := runCleanupAdmission(t, root, []string{"bash", "scripts/run-integration-tests.sh", "./some/package/..."})
	output, err := result.Output, result.CommandError
	if _, statErr := os.Stat(filepath.Join(root, "guard-observation.json")); !os.IsNotExist(statErr) {
		t.Errorf("focused iteration unexpectedly ran repository guard: stat=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "expensive-command")); statErr != nil {
		t.Fatalf("focused iteration never reached its existing execution path: %v; command=%v\n%s", statErr, err, output)
	}
}

type cleanupAdmissionObservation struct {
	Error string        `json:"error"`
	Sites []cleanupSite `json:"sites"`
}

// TestCleanupAdmissionFixtureProcess is a subprocess entry point, like the
// existing integration-runner pull helper. It never executes fixture tests.
func TestCleanupAdmissionFixtureProcess(t *testing.T) {
	root := os.Getenv("SEMSTREAMS_TEST_CLEANUP_ROOT")
	if root == "" {
		t.Skip("subprocess fixture entry point")
	}
	// package loading must use the real Go executable, not the admission shim.
	t.Setenv("PATH", os.Getenv("SEMSTREAMS_TEST_ORIGINAL_PATH"))
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	report, err := analyzeCleanupRoots(ctx, root, filepath.Join(root, "baseline.json"), []cleanupSelection{{Name: "default"}})
	observed := cleanupAdmissionObservation{Sites: report.Sites}
	if err != nil {
		observed.Error = err.Error()
	}
	body, marshalErr := json.Marshal(observed)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "guard-observation.json"), body, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func assertCleanupFixtureFailure(t *testing.T, kind string, observed cleanupAdmissionObservation) {
	t.Helper()
	wantClass := ""
	switch kind {
	case "new-debt":
		wantClass = cleanupUnbounded
	case "unknown":
		wantClass = cleanupUnknown
	case "stale":
		if !strings.Contains(strings.ToLower(observed.Error), "stale") {
			t.Fatalf("stale fixture failed for an unintended reason: %+v", observed)
		}
	case "load-failure":
		if !strings.Contains(observed.Error, "MissingContext") {
			t.Fatalf("load fixture did not reach intended type error: %+v", observed)
		}
	}
	if wantClass != "" {
		for _, site := range observed.Sites {
			if site.Classification == wantClass {
				return
			}
		}
		t.Fatalf("%s fixture did not reach expected %s classification: %+v", kind, wantClass, observed)
	}
}

func cleanupAdmissionWorkspace(t *testing.T, repo, kind string) string {
	t.Helper()
	root := t.TempDir()
	copyCleanupAdmissionFile(t, repo, root, "Taskfile.yml")
	if err := filepath.WalkDir(filepath.Join(repo, "taskfiles"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(repo, path)
			if err != nil {
				return err
			}
			copyCleanupAdmissionFile(t, repo, root, rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	copyCleanupAdmissionFile(t, repo, root, "scripts/run-integration-tests.sh")
	// Absence is permitted only so the first TDD run can prove missing wiring.
	if _, err := os.Stat(filepath.Join(repo, "scripts/check-cleanup-roots.sh")); err == nil {
		copyCleanupAdmissionFile(t, repo, root, "scripts/check-cleanup-roots.sh")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	writeCleanupAdmissionFile(t, filepath.Join(root, "go.mod"), "module cleanupfixture\n\ngo 1.26\n", 0o600)
	writeCleanupAdmissionFile(t, filepath.Join(root, "baseline.json"), `{"version":1,"entries":[]}`, 0o600)
	source := cleanupAdmissionSource(kind)
	if kind == "stale" {
		source = cleanupAdmissionSource("new-debt")
	}
	writeCleanupAdmissionFile(t, filepath.Join(root, "fixture_test.go"), source, 0o600)
	init := exec.CommandContext(t.Context(), "git", "init", "--quiet", root)
	if output, err := init.CombinedOutput(); err != nil {
		t.Fatalf("fixture git init: %v\n%s", err, output)
	}
	if kind == "stale" {
		report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "baseline.json"), []cleanupSelection{{Name: "default"}})
		if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnbounded {
			t.Fatalf("stale fixture setup needs exactly one observed unbounded site: report=%+v err=%v", report, err)
		}
		site := report.Sites[0]
		baseline := cleanupBaseline{Version: 1, Entries: []cleanupApproval{{
			Identity: site.Identity, Fingerprint: site.Fingerprint, Classification: cleanupUnbounded,
			Reason: "exact synthetic fixture debt", OwnerIssue: "#1064-fixture",
		}}}
		body, err := json.Marshal(baseline)
		if err != nil {
			t.Fatal(err)
		}
		writeCleanupAdmissionFile(t, filepath.Join(root, "baseline.json"), string(body), 0o600)
		writeCleanupAdmissionFile(t, filepath.Join(root, "fixture_test.go"), "package cleanupfixture\n", 0o600)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	goShim := `#!/bin/sh
if [ "$*" = 'test -count=1 ./test/testinfra -run ^TestCleanupRootGuard$' ]; then
  exec "$SEMSTREAMS_TEST_BINARY" -test.run '^TestCleanupAdmissionFixtureProcess$' -test.timeout=50s
fi
printf 'go %s\n' "$*" > "$SEMSTREAMS_TEST_CLEANUP_ROOT/expensive-command"
exit 73
`
	dockerShim := `#!/bin/sh
printf 'docker %s\n' "$*" > "$SEMSTREAMS_TEST_CLEANUP_ROOT/expensive-command"
exit 73
`
	mkdirShim := `#!/bin/sh
for arg do
  if [ "$arg" = "$SEMSTREAMS_INTEGRATION_LOCK_DIR" ]; then
    printf 'lock acquired\n' > "$SEMSTREAMS_TEST_CLEANUP_ROOT/lock-acquired"
  fi
done
exec /bin/mkdir "$@"
`
	writeCleanupAdmissionFile(t, filepath.Join(bin, "go"), goShim, 0o700)
	writeCleanupAdmissionFile(t, filepath.Join(bin, "docker"), dockerShim, 0o700)
	writeCleanupAdmissionFile(t, filepath.Join(bin, "mkdir"), mkdirShim, 0o700)
	return root
}

func cleanupAdmissionSource(kind string) string {
	contextExpr := "ctx"
	setup := "ctx, cancel := context.WithTimeout(context.Background(), time.Second); defer cancel()"
	switch kind {
	case "new-debt":
		contextExpr = "context.Background()"
		setup = "_ = time.Second"
	case "unknown":
		contextExpr = "externalContext()"
		setup = "_ = time.Second"
	case "load-failure":
		contextExpr = "MissingContext"
		setup = "_ = time.Second"
	}
	return fmt.Sprintf(`package cleanupfixture
import("context"; "testing"; "time")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
var externalContext func() context.Context
func TestFixture(t *testing.T) {
  o := &owner{}
  t.Cleanup(func(){ %s; if err := o.Stop(%s); err != nil { t.Error(err) } })
}
`, setup, contextExpr)
}

func runCleanupAdmission(t *testing.T, root string, args []string) cleanupProcessOutcome {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	values := map[string]string{
		"PATH":                                     filepath.Join(root, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"SEMSTREAMS_TEST_ORIGINAL_PATH":            os.Getenv("PATH"),
		"SEMSTREAMS_TEST_BINARY":                   binary,
		"SEMSTREAMS_TEST_CLEANUP_ROOT":             root,
		"SEMSTREAMS_INTEGRATION_LOCK_DIR":          filepath.Join(root, "integration.lock"),
		"SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS": "0",
	}
	result := runOwnedCleanupCommand(ctx, root, args, cleanupAdmissionEnvironment(values))
	if result.CleanupError != nil {
		t.Fatalf("admission fixture cleanup unresolved: %v; command=%v\n%s", result.CleanupError, result.CommandError, result.Output)
	}
	return result
}

func cleanupAdmissionEnvironment(values map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := values[key]; !replaced {
			env = append(env, entry)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func copyCleanupAdmissionFile(t *testing.T, from, to, rel string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(from, rel))
	if err != nil {
		t.Fatal(err)
	}
	writeCleanupAdmissionFile(t, filepath.Join(to, rel), string(body), 0o700)
}

func writeCleanupAdmissionFile(t *testing.T, path, body string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

const cleanupTaskInstall = "GOBIN=/usr/local/bin go install github.com/go-task/task/v3/cmd/task@v3.53.1"

func cleanupCITestCommand(t *testing.T, root string) string {
	t.Helper()
	command, err := cleanupCITestAdmission(cleanupCICommands(t, root, "test"))
	if err != nil {
		t.Fatal(err)
	}
	return command
}

// Only the pinned prerequisite may precede the runner. Inspecting just the
// selected runner command would miss an expensive test step inserted before it.
func cleanupCITestAdmission(commands []string) (string, error) {
	installed := false
	for _, command := range commands {
		switch strings.TrimSpace(command) {
		case cleanupTaskInstall:
			if installed {
				return "", fmt.Errorf("duplicate Task installation before CI admission")
			}
			installed = true
		case "scripts/run-integration-tests.sh":
			if !installed {
				return "", fmt.Errorf("CI Test must install pinned Task before running admission fixtures")
			}
			return command, nil
		default:
			return "", fmt.Errorf("unapproved command before CI cleanup admission: %q", command)
		}
	}
	return "", fmt.Errorf("CI Test no longer selects the canonical integration runner")
}

func TestCleanupAdmissionCIOrdering(t *testing.T) {
	for _, tc := range []struct {
		name      string
		commands  []string
		wantError bool
	}{
		{"pinned setup then runner", []string{cleanupTaskInstall, "scripts/run-integration-tests.sh"}, false},
		{"missing prerequisite", []string{"scripts/run-integration-tests.sh"}, true},
		{"expensive earlier step", []string{cleanupTaskInstall, "go test ./...", "scripts/run-integration-tests.sh"}, true},
		{"comment is not admission", []string{cleanupTaskInstall, "# scripts/run-integration-tests.sh\ngo test ./..."}, true},
		{"compound is not admission", []string{cleanupTaskInstall, "go test ./...; scripts/run-integration-tests.sh"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cleanupCITestAdmission(tc.commands)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error = %v", err, tc.wantError)
			}
		})
	}
}

func cleanupCICommands(t *testing.T, root, job string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	commands, err := parseCleanupCICommands(body, job)
	if err != nil {
		t.Fatal(err)
	}
	return commands
}

type cleanupCIDefaults struct {
	Run struct {
		Shell            string `yaml:"shell"`
		WorkingDirectory string `yaml:"working-directory"`
	} `yaml:"run"`
}

type cleanupCIExecution struct {
	If               any    `yaml:"if"`
	ContinueOnError  any    `yaml:"continue-on-error"`
	Shell            string `yaml:"shell"`
	WorkingDirectory string `yaml:"working-directory"`
}

func parseCleanupCICommands(body []byte, job string) ([]string, error) {
	var workflow struct {
		Defaults cleanupCIDefaults `yaml:"defaults"`
		Jobs     map[string]struct {
			cleanupCIExecution `yaml:",inline"`
			Defaults           cleanupCIDefaults `yaml:"defaults"`
			Steps              []struct {
				cleanupCIExecution `yaml:",inline"`
				Run                string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		return nil, err
	}
	selected, exists := workflow.Jobs[job]
	if !exists {
		return nil, fmt.Errorf("CI job %s is absent", job)
	}
	for _, metadata := range []cleanupCIExecution{
		{Shell: workflow.Defaults.Run.Shell, WorkingDirectory: workflow.Defaults.Run.WorkingDirectory},
		{Shell: selected.Defaults.Run.Shell, WorkingDirectory: selected.Defaults.Run.WorkingDirectory},
		selected.cleanupCIExecution,
	} {
		if err := validateCleanupCIExecution(metadata); err != nil {
			return nil, err
		}
	}
	var commands []string
	admissionReached := false
	for _, step := range selected.Steps {
		if step.Run == "" {
			continue
		}
		if !admissionReached {
			if err := validateCleanupCIExecution(step.cleanupCIExecution); err != nil {
				return nil, err
			}
		}
		commands = append(commands, step.Run)
		command := strings.TrimSpace(step.Run)
		if command == "scripts/run-integration-tests.sh" || command == "scripts/check-cleanup-roots.sh" {
			admissionReached = true
		}
	}
	return commands, nil
}

func validateCleanupCIExecution(metadata cleanupCIExecution) error {
	if metadata.If != nil {
		return fmt.Errorf("CI admission may not be conditional: %v", metadata.If)
	}
	if metadata.ContinueOnError != nil && metadata.ContinueOnError != false {
		return fmt.Errorf("CI admission may not continue on error: %v", metadata.ContinueOnError)
	}
	if metadata.Shell != "" && metadata.Shell != "bash" {
		return fmt.Errorf("CI admission has unsupported shell: %s", metadata.Shell)
	}
	if metadata.WorkingDirectory != "" && metadata.WorkingDirectory != "." {
		return fmt.Errorf("CI admission must run in the repository root: %s", metadata.WorkingDirectory)
	}
	return nil
}

func TestCleanupAdmissionCIExecutionMetadata(t *testing.T) {
	base := "jobs:\n  test:\n    steps:\n      - run: " + cleanupTaskInstall + "\n      - run: scripts/run-integration-tests.sh\n"
	for _, tc := range []struct {
		name, before, after string
		wantError           bool
	}{
		{"default execution", "", "", false},
		{"step ignores failure", "      - run: scripts/", "      - continue-on-error: true\n        run: scripts/", true},
		{"step conditional skip", "      - run: scripts/", "      - if: false\n        run: scripts/", true},
		{"step custom shell", "      - run: scripts/", "      - shell: bash {0} || true\n        run: scripts/", true},
		{"job ignores failure", "    steps:", "    continue-on-error: true\n    steps:", true},
		{"job conditional skip", "    steps:", "    if: false\n    steps:", true},
		{"inherited custom shell", "    steps:", "    defaults:\n      run:\n        shell: bash {0} || true\n    steps:", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := base
			if tc.before != "" {
				body = strings.Replace(body, tc.before, tc.after, 1)
			}
			commands, err := parseCleanupCICommands([]byte(body), "test")
			if err == nil {
				_, err = cleanupCITestAdmission(commands)
			}
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v want error=%v", err, tc.wantError)
			}
		})
	}
}

// The reduced runtime matrix relies on wrappers forwarding one shared guard's
// status, without inspecting its classification output or swallowing failures.
func TestCleanupAdmissionUniformWiring(t *testing.T) {
	root := findRepoRoot(t)
	for _, item := range []struct {
		file, task string
		first      any
	}{
		{"taskfiles/lint.yml", "default", map[string]any{"task": "cleanup-roots"}},
		{"taskfiles/lint.yml", "cleanup-roots", "scripts/check-cleanup-roots.sh"},
		{"taskfiles/test.yml", "default", "scripts/check-cleanup-roots.sh"},
		{"taskfiles/test.yml", "race", "scripts/check-cleanup-roots.sh"},
		{"taskfiles/test.yml", "live", "scripts/check-cleanup-roots.sh"},
		{"taskfiles/test.yml", "integration", "scripts/run-integration-tests.sh"},
		{"Taskfile.yml", "check", map[string]any{"task": "lint:default"}},
		{"Taskfile.yml", "check:push", map[string]any{"task": "lint:default"}},
	} {
		t.Run(item.file+"/"+item.task, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, item.file))
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				IgnoreError bool                      `yaml:"ignore_error"`
				Tasks       map[string]map[string]any `yaml:"tasks"`
			}
			if err := yaml.Unmarshal(body, &document); err != nil {
				t.Fatal(err)
			}
			if document.IgnoreError {
				t.Fatal("global ignore_error would swallow guard failure")
			}
			if err := cleanupTaskAdmission(document.Tasks[item.task], item.first); err != nil {
				t.Fatal(err)
			}
		})
	}
	body, err := os.ReadFile(filepath.Join(root, "scripts/check-cleanup-roots.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(body)), "exec go test -count=1 ./test/testinfra -run '^TestCleanupRootGuard$'") {
		t.Fatal("shared script must directly exec the canonical guard, without conditional failure handling")
	}
	runner, err := os.ReadFile(filepath.Join(root, "scripts/run-integration-tests.sh"))
	if err != nil {
		t.Fatal(err)
	}
	guardBlock := "if $full_cleanup_guard; then\n  \"$(dirname -- \"${BASH_SOURCE[0]}\")/check-cleanup-roots.sh\"\nfi"
	if strings.Count(string(runner), guardBlock) != 1 || !strings.HasPrefix(string(runner), "#!/usr/bin/env bash\nset -euo pipefail\n") {
		t.Fatal("full runner must uniformly propagate the single shared guard invocation")
	}
}

func cleanupTaskAdmission(task map[string]any, first any) error {
	if value, ok := task["ignore_error"]; ok && value != false {
		return fmt.Errorf("task ignores guard failure")
	}
	for _, key := range []string{"deps", "status", "preconditions", "sources", "generates", "run"} {
		if _, ok := task[key]; ok {
			return fmt.Errorf("admission task has unsupported pre-guard or conditional field %s", key)
		}
	}
	commands, ok := task["cmds"].([]any)
	if !ok || len(commands) == 0 || !reflect.DeepEqual(commands[0], first) {
		return fmt.Errorf("first sequential command must be the exact shared admission path: got %#v want %#v", task["cmds"], first)
	}
	return nil
}

func TestCleanupAdmissionUniformWiringRejectsBypasses(t *testing.T) {
	const shared = "scripts/check-cleanup-roots.sh"
	for _, tc := range []struct {
		name      string
		task      map[string]any
		wantError bool
	}{
		{"exact invocation", map[string]any{"cmds": []any{shared}}, false},
		{"ignored task failure", map[string]any{"ignore_error": true, "cmds": []any{shared}}, true},
		{"ignored command failure", map[string]any{"cmds": []any{map[string]any{"cmd": shared, "ignore_error": true}}}, true},
		{"success forcing", map[string]any{"cmds": []any{shared + " || true"}}, true},
		{"conditional handling", map[string]any{"cmds": []any{"if " + shared + "; then true; fi"}}, true},
		{"parallel prerequisite", map[string]any{"deps": []any{"build"}, "cmds": []any{shared}}, true},
		{"conditional skip", map[string]any{"status": []any{"true"}, "cmds": []any{shared}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := cleanupTaskAdmission(tc.task, shared)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v want error=%v", err, tc.wantError)
			}
		})
	}
}

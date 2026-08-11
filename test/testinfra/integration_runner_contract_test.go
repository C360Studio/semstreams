package testinfra_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestIntegrationRunner_CanonicalCommandAndRyukPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	lockDir := filepath.Join(t.TempDir(), "integration.lock")
	command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	command.Dir = repoRoot
	command.Env = runnerEnvironment(tools, lockDir, nil)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("runner failed: %v\n%s", err, output)
	}

	arguments := readFile(t, tools.goArguments)
	wantArguments := strings.Join([]string{
		"test",
		"-race",
		"-failfast",
		"-tags=integration",
		"-timeout=20m",
		"-count=1",
		"./...",
		"",
	}, "\n")
	if arguments != wantArguments {
		t.Fatalf("go arguments:\n%s\nwant:\n%s", arguments, wantArguments)
	}
	if got := strings.TrimSpace(readFile(t, tools.ryuk)); got != "false" {
		t.Fatalf("TESTCONTAINERS_RYUK_DISABLED = %q, want false", got)
	}
	if !strings.Contains(string(output), "docker info latency:") {
		t.Fatalf("runner omitted Docker latency evidence:\n%s", output)
	}
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("runner did not release host lock %s: %v", lockDir, err)
	}
}

func TestIntegrationRunner_DefaultLockIsHostLevel(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	runner := readFile(t, filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	if !strings.Contains(runner, `default_lock_dir="/tmp/semstreams-integration.lock"`) {
		t.Error("runner default lock is not the fixed host-level /tmp path")
	}
	if strings.Contains(runner, `${TMPDIR:-/tmp}/semstreams-integration.lock`) {
		t.Error("runner default lock varies with process-local TMPDIR")
	}
}

func TestIntegrationRunner_ImagePullOwnershipComesFromBashJobTable(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	runner := readFile(t, filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	ownershipHelper := shellFunction(t, runner, "image_pull_is_running")
	if !strings.Contains(ownershipHelper, "jobs -pr") {
		t.Error("image-pull ownership helper does not consult Bash's running-job table")
	}
	if !strings.Contains(ownershipHelper, `"$job_pid" == "$image_pull_pid"`) {
		t.Error("image-pull ownership helper does not require the exact stored pull PID")
	}
	for _, function := range []string{"image_pull_is_running", "terminate_and_reap_image_pull", "run_bounded_image_pull"} {
		if body := shellFunction(t, runner, function); strings.Contains(body, "kill -0") {
			t.Errorf("%s uses kill -0 instead of Bash job ownership", function)
		}
	}
}

func TestIntegrationRunner_CachedImageDoesNotRequireRegistry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	lockDir := filepath.Join(t.TempDir(), "integration.lock")
	command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	command.Dir = repoRoot
	command.Env = runnerEnvironment(tools, lockDir, map[string]string{
		"DOCKER_IMAGE_INSPECT_STATUS": "0",
		"DOCKER_PULL_STATUS":          "99",
	})
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("warm/offline runner failed: %v\n%s", err, output)
	}
	calls := readFile(t, tools.callLog)
	if !strings.Contains(calls, "docker image inspect nats:2.14-alpine\n") {
		t.Fatalf("runner did not inspect the pinned image cache:\n%s", calls)
	}
	if strings.Contains(calls, "docker pull ") {
		t.Fatalf("cached image triggered registry pull:\n%s", calls)
	}
}

func TestIntegrationRunner_MissingOrRefreshImagePullsUnderLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	for _, testCase := range []struct {
		name      string
		overrides map[string]string
	}{
		{name: "missing", overrides: map[string]string{"DOCKER_IMAGE_INSPECT_STATUS": "1"}},
		{name: "refresh", overrides: map[string]string{
			"DOCKER_IMAGE_INSPECT_STATUS":          "0",
			"SEMSTREAMS_INTEGRATION_REFRESH_IMAGE": "1",
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tools := newFakeToolchain(t)
			lockDir := filepath.Join(t.TempDir(), "integration.lock")
			command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
			command.Dir = repoRoot
			command.Env = runnerEnvironment(tools, lockDir, testCase.overrides)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("runner failed: %v\n%s", err, output)
			}
			calls := readFile(t, tools.callLog)
			if !strings.Contains(calls, "docker pull nats:2.14-alpine\n") {
				t.Fatalf("%s image did not pull:\n%s", testCase.name, calls)
			}
		})
	}
}

func TestIntegrationRunner_SuccessfulImagePullLeavesNoChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	tempDir := t.TempDir()
	pullPIDFile := filepath.Join(tempDir, "pull.pid")
	command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	command.Dir = repoRoot
	command.Env = runnerEnvironment(tools, filepath.Join(tempDir, "integration.lock"), map[string]string{
		"DOCKER_IMAGE_INSPECT_STATUS": "1",
		"DOCKER_PULL_PID_FILE":        pullPIDFile,
	})
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("runner failed: %v\n%s", err, output)
	}
	assertProcessGone(t, readPID(t, pullPIDFile), "successful fake image pull")
}

func TestIntegrationRunner_ImagePullTimeoutIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	tempDir := t.TempDir()
	lockDir := filepath.Join(tempDir, "integration.lock")
	pullPIDFile := filepath.Join(tempDir, "pull.pid")
	command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	command.Dir = repoRoot
	command.Env = runnerEnvironment(tools, lockDir, map[string]string{
		"DOCKER_IMAGE_INSPECT_STATUS":                    "1",
		"DOCKER_PULL_BLOCK":                              "1",
		"DOCKER_PULL_PID_FILE":                           pullPIDFile,
		"SEMSTREAMS_CONTRACT_IMAGE_PULL_TIMEOUT_SECONDS": "1",
	})
	started := time.Now()
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("blocked image pull unexpectedly succeeded:\n%s", output)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("one-second pull ceiling returned after %s", elapsed)
	}
	if !strings.Contains(string(output), "pull timed out after 1s") {
		t.Fatalf("pull timeout lacks bounded diagnostic:\n%s", output)
	}
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("runner retained lock after pull timeout: %v", err)
	}
	assertProcessGone(t, readPID(t, pullPIDFile), "timed-out fake image pull")
}

func TestIntegrationRunner_InterruptReapsPullBeforeReleasingLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	tempDir := t.TempDir()
	lockDir := filepath.Join(tempDir, "integration.lock")
	pullPIDFile := filepath.Join(tempDir, "pull.pid")
	command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	command.Dir = repoRoot
	command.Env = runnerEnvironment(tools, lockDir, map[string]string{
		"DOCKER_IMAGE_INSPECT_STATUS":                    "1",
		"DOCKER_PULL_BLOCK":                              "1",
		"DOCKER_PULL_PID_FILE":                           pullPIDFile,
		"SEMSTREAMS_CONTRACT_IMAGE_PULL_TIMEOUT_SECONDS": "30",
	})
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := newCommandWaiter(command)
	t.Cleanup(func() { _ = waiter.killAndWait() })
	waitForFileContent(t, pullPIDFile, "", 3*time.Second)
	waitForFileContent(t, filepath.Join(lockDir, "owner"), "token=", 3*time.Second)
	pullPID := readPID(t, pullPIDFile)
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt runner: %v", err)
	}
	if err := waiter.wait(3 * time.Second); err == nil {
		t.Fatalf("interrupted runner unexpectedly succeeded:\n%s", output.String())
	}
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("runner released process before host lock: lock remains: %v", err)
	}
	assertProcessGone(t, pullPID, "interrupted fake image pull")
}

func TestCommandWaiter_TimeoutCleanupKillsAndReapsThroughOneOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process signals differ on Windows")
	}
	command := exec.Command("/bin/sh", "-c", "exec /bin/sleep 30")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pid := command.Process.Pid
	waiter := newCommandWaiter(command)
	t.Cleanup(func() { _ = waiter.killAndWait() })

	if err := waiter.wait(10 * time.Millisecond); err == nil || !strings.Contains(err.Error(), "did not exit") {
		t.Fatalf("wait before cleanup = %v, want bounded timeout", err)
	}
	if err := waiter.killAndWait(); err == nil {
		t.Fatal("killed command unexpectedly reported success")
	}
	if command.ProcessState == nil {
		t.Fatalf("cleanup returned before command was reaped: state=%v", command.ProcessState)
	}
	assertProcessGone(t, pid, "timeout-cleaned command")
	if err := waiter.wait(time.Second); err == nil {
		t.Fatal("repeated wait lost the command's killed result")
	}
}

func TestIntegrationRunner_ImagePullTimeoutCannotExceedProductionCeiling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	command.Dir = repoRoot
	command.Env = runnerEnvironment(tools, filepath.Join(t.TempDir(), "integration.lock"), map[string]string{
		"SEMSTREAMS_CONTRACT_IMAGE_PULL_TIMEOUT_SECONDS": "301",
	})
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("301-second contract override unexpectedly accepted:\n%s", output)
	}
	if !strings.Contains(string(output), "expected 1-300") {
		t.Fatalf("invalid timeout diagnostic missing:\n%s", output)
	}
}

func TestIntegrationRunner_HostLockHasBoundedContentionDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	tempDir := t.TempDir()
	lockDir := filepath.Join(tempDir, "integration.lock")
	releaseFile := filepath.Join(tempDir, "release-docker")
	runner := filepath.Join(repoRoot, "scripts", "run-integration-tests.sh")

	holderContext, cancelHolder := context.WithCancel(context.Background())
	holder := exec.CommandContext(holderContext, runner)
	holder.Dir = repoRoot
	holder.Env = runnerEnvironment(tools, lockDir, map[string]string{
		"DOCKER_RELEASE_FILE": releaseFile,
	})
	var holderOutput bytes.Buffer
	holder.Stdout = &holderOutput
	holder.Stderr = &holderOutput
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	holderWaiter := newCommandWaiter(holder)
	t.Cleanup(func() {
		_ = os.WriteFile(releaseFile, []byte("release\n"), 0o644)
		cancelHolder()
		_ = holderWaiter.killAndWait()
	})
	waitForFileContent(t, filepath.Join(lockDir, "owner"), "token=", 3*time.Second)

	contender := exec.Command(runner)
	contender.Dir = repoRoot
	contender.Env = runnerEnvironment(tools, lockDir, map[string]string{
		"SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS": "1",
	})
	started := time.Now()
	output, err := contender.CombinedOutput()
	if err == nil {
		t.Fatalf("contending runner unexpectedly acquired lock:\n%s", output)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("1s lock budget returned after %s", elapsed)
	}
	for _, evidence := range []string{"wait budget 1s exhausted", "lock owner host=", "pid=", "elapsed=", "command="} {
		if !strings.Contains(string(output), evidence) {
			t.Errorf("contention diagnostics missing %q:\n%s", evidence, output)
		}
	}

	if err := os.WriteFile(releaseFile, []byte("release\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := holderWaiter.wait(3 * time.Second); err != nil {
		t.Fatalf("holder did not finish after release: %v\n%s", err, holderOutput.String())
	}
	cancelHolder()
	if got := strings.Count(readFile(t, tools.callLog), "docker "); got != 2 {
		t.Fatalf("Docker was invoked %d times; contender must fail before Docker work\n%s", got, readFile(t, tools.callLog))
	}
}

func TestIntegrationRunner_CleansOnlyProvablyStaleLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	lockDir := filepath.Join(t.TempDir(), "integration.lock")
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	owner := fmt.Sprintf("host=%s\npid=1073741824\nstarted=%d\nidentity=stale\ntoken=stale\ncommand=old-runner\n",
		host, time.Now().Add(-time.Minute).Unix())
	if err := os.WriteFile(filepath.Join(lockDir, "owner"), []byte(owner), 0o644); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"), "./test/testinfra/...")
	command.Dir = repoRoot
	command.Env = runnerEnvironment(tools, lockDir, nil)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("runner failed after stale lock: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "cleaned stale lock") {
		t.Fatalf("runner omitted stale-lock evidence:\n%s", output)
	}
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("runner did not release replacement lock: %v", err)
	}
}

func TestIntegrationRunner_TaskAndCIConverge(t *testing.T) {
	repoRoot := findRepoRoot(t)
	taskFile := readFile(t, filepath.Join(repoRoot, "taskfiles", "test.yml"))
	workflow := readFile(t, filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	runner := readFile(t, filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))

	if got := strings.Count(taskFile, "scripts/run-integration-tests.sh"); got != 3 {
		t.Fatalf("Task integration lanes reference canonical runner %d times, want 3", got)
	}
	if got := strings.Count(workflow, "scripts/run-integration-tests.sh"); got != 1 {
		t.Fatalf("CI references canonical runner %d times, want 1", got)
	}
	for path, body := range map[string]string{"taskfiles/test.yml": taskFile, ".github/workflows/ci.yml": workflow} {
		if strings.Contains(body, "go test -race -tags=integration") {
			t.Errorf("%s bypasses the canonical integration runner", path)
		}
		if strings.Contains(body, "TESTCONTAINERS_RYUK_DISABLED") {
			t.Errorf("%s carries an independent Ryuk policy", path)
		}
	}
	for _, required := range []string{
		"go test -race -failfast -tags=integration -timeout=20m -count=1",
		"export TESTCONTAINERS_RYUK_DISABLED=false",
		"acquire_lock",
		"docker info",
		"docker pull \"$nats_image\"",
	} {
		if !strings.Contains(runner, required) {
			t.Errorf("canonical runner missing %q", required)
		}
	}
	if !strings.Contains(workflow, "timeout-minutes: 25") {
		t.Error("CI test job has no pinned 25-minute outer timeout")
	}
	if strings.Contains(workflow, "run: go test -race ./...") {
		t.Error("CI duplicates the additive tagged suite with a separate unit-test run")
	}
	if strings.Contains(workflow, "docker pull nats:") {
		t.Error("CI performs Docker work before the integration runner acquires its host lock")
	}
	info, err := os.Stat(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("canonical integration runner is not executable")
	}
}

type fakeToolchain struct {
	bin         string
	callLog     string
	goArguments string
	ryuk        string
}

func newFakeToolchain(t *testing.T) fakeToolchain {
	t.Helper()
	directory := t.TempDir()
	tools := fakeToolchain{
		bin:         filepath.Join(directory, "bin"),
		callLog:     filepath.Join(directory, "calls"),
		goArguments: filepath.Join(directory, "go-arguments"),
		ryuk:        filepath.Join(directory, "ryuk"),
	}
	if err := os.Mkdir(tools.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	docker := `#!/bin/sh
printf 'docker %s\n' "$*" >> "$CALL_LOG"
if [ "$1" = "info" ] && [ -n "${DOCKER_RELEASE_FILE:-}" ]; then
  while [ ! -f "$DOCKER_RELEASE_FILE" ]; do sleep 0.05; done
fi
if [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
  exit "${DOCKER_IMAGE_INSPECT_STATUS:-0}"
fi
if [ "$1" = "pull" ]; then
	if [ -n "${DOCKER_PULL_PID_FILE:-}" ]; then
		printf '%s\n' "$$" > "$DOCKER_PULL_PID_FILE"
	fi
  if [ "${DOCKER_PULL_BLOCK:-0}" = "1" ]; then
	exec /bin/sleep 30
  fi
  exit "${DOCKER_PULL_STATUS:-0}"
fi
echo 'fake docker info'
`
	goCommand := `#!/bin/sh
printf 'go\n' >> "$CALL_LOG"
printf '%s\n' "$@" > "$GO_ARGUMENTS"
printf '%s\n' "${TESTCONTAINERS_RYUK_DISABLED:-unset}" > "$RYUK_CAPTURE"
`
	writeExecutable(t, filepath.Join(tools.bin, "docker"), docker)
	writeExecutable(t, filepath.Join(tools.bin, "go"), goCommand)
	return tools
}

func runnerEnvironment(tools fakeToolchain, lockDir string, overrides map[string]string) []string {
	values := map[string]string{
		"PATH":                            tools.bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CALL_LOG":                        tools.callLog,
		"GO_ARGUMENTS":                    tools.goArguments,
		"RYUK_CAPTURE":                    tools.ryuk,
		"SEMSTREAMS_INTEGRATION_LOCK_DIR": lockDir,
		"SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS": "0",
	}
	for key, value := range overrides {
		values[key] = value
	}
	result := make([]string, 0, len(os.Environ())+len(values))
	for _, item := range os.Environ() {
		key := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			key = item[:index]
		}
		if _, replaced := values[key]; !replaced {
			result = append(result, item)
		}
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func waitForFileContent(t *testing.T, path, content string, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		body, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(body), content) {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("%s did not contain %q within %s", path, content, timeout)
		case <-ticker.C:
		}
	}
}

// commandWaiter gives exactly one goroutine ownership of Cmd.Wait. Callers may
// time out, kill during cleanup, and wait again without racing a second Wait.
type commandWaiter struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func newCommandWaiter(command *exec.Cmd) *commandWaiter {
	waiter := &commandWaiter{
		command: command,
		done:    make(chan struct{}),
	}
	go func() {
		waiter.err = command.Wait()
		close(waiter.done)
	}()
	return waiter
}

func (w *commandWaiter) wait(timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-w.done:
		return w.err
	case <-timer.C:
		return fmt.Errorf("command did not exit within %s", timeout)
	}
}

func (w *commandWaiter) killAndWait() error {
	select {
	case <-w.done:
		return w.err
	default:
	}

	killErr := w.command.Process.Kill()
	<-w.done
	if w.err != nil {
		return w.err
	}
	return killErr
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func shellFunction(t *testing.T, script, name string) string {
	t.Helper()
	start := strings.Index(script, name+"() {")
	if start < 0 {
		t.Fatalf("shell function %s not found", name)
	}
	remainder := script[start:]
	end := strings.Index(remainder, "\n}")
	if end < 0 {
		t.Fatalf("shell function %s has no closing brace", name)
	}
	return remainder[:end+2]
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(readFile(t, path)), "%d", &pid); err != nil || pid <= 0 {
		t.Fatalf("read process ID from %s: pid=%d err=%v", path, pid, err)
	}
	return pid
}

func assertProcessGone(t *testing.T, pid int, description string) {
	t.Helper()
	if err := exec.Command("kill", "-0", fmt.Sprintf("%d", pid)).Run(); err == nil {
		t.Fatalf("%s process %d is still alive", description, pid)
	}
}

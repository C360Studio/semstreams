package testinfra_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
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

func TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	tempDir := t.TempDir()
	lockDir := filepath.Join(tempDir, "integration.lock")
	pullPIDFile := filepath.Join(tempDir, "pull.pid")
	testBinary := mustExecutable(t)
	realDate := mustLookPath(t, "date")
	realRmdir := mustLookPath(t, "rmdir")
	parentReadySentinel := filepath.Join(tempDir, "parent-ready.sent")
	termReceivedSentinel := filepath.Join(tempDir, "term-received.sent")
	gracePauseClaim := filepath.Join(tempDir, "grace-pause.claim")
	writeTerminationToolWrappers(t, tools.bin)

	parentReadyReader, parentReadyWriter := mustPipe(t)
	termAckReader, termAckWriter := mustPipe(t)
	releaseReader, releaseWriter := mustPipe(t)
	reapAckReader, reapAckWriter := mustPipe(t)
	gracePauseReader, gracePauseWriter := mustPipe(t)
	graceReleaseReader, graceReleaseWriter := mustPipe(t)
	closeFilesOnCleanup(t,
		parentReadyReader, parentReadyWriter,
		termAckReader, termAckWriter,
		releaseReader, releaseWriter,
		reapAckReader, reapAckWriter,
		gracePauseReader, gracePauseWriter,
		graceReleaseReader, graceReleaseWriter,
	)

	command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	command.Dir = repoRoot
	command.ExtraFiles = []*os.File{
		parentReadyWriter,
		termAckWriter,
		releaseReader,
		reapAckWriter,
		gracePauseWriter,
		graceReleaseReader,
	}
	command.Env = runnerEnvironment(tools, lockDir, map[string]string{
		"DOCKER_IMAGE_INSPECT_STATUS":                    "1",
		"DOCKER_PULL_PID_FILE":                           pullPIDFile,
		"SEMSTREAMS_CONTRACT_IMAGE_PULL_TIMEOUT_SECONDS": "30",
		"SEMSTREAMS_TEST_LOCK_DIR":                       lockDir,
		"SEMSTREAMS_TEST_PARENT_READY_SENTINEL":          parentReadySentinel,
		"SEMSTREAMS_TEST_TERM_RECEIVED_SENTINEL":         termReceivedSentinel,
		"SEMSTREAMS_TEST_GRACE_PAUSE_CLAIM":              gracePauseClaim,
		"SEMSTREAMS_TEST_PULL_HELPER":                    "1",
		"SEMSTREAMS_TEST_REAL_DATE":                      realDate,
		"SEMSTREAMS_TEST_REAL_RMDIR":                     realRmdir,
		"SEMSTREAMS_TEST_BINARY":                         testBinary,
	})
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := newCommandWaiter(command)
	t.Cleanup(func() {
		// EOF releases the helper on any early failure. killAndWait kills only
		// when the runner is still live and always joins the sole waiter.
		_ = releaseWriter.Close()
		_ = graceReleaseWriter.Close()
		_ = waiter.killAndWait()
	})
	closeInheritedFiles(t, map[string]*os.File{
		"parent-ready writer":  parentReadyWriter,
		"TERM-ack writer":      termAckWriter,
		"child-release reader": releaseReader,
		"reap-check writer":    reapAckWriter,
		"grace-pause writer":   gracePauseWriter,
		"grace-release reader": graceReleaseReader,
	})

	readPipeSignal(t, parentReadyReader, 3*time.Second, "parent retained pull PID")
	ownerBeforeTermination := readFile(t, filepath.Join(lockDir, "owner"))
	if !strings.Contains(ownerBeforeTermination, "token=") {
		t.Fatalf("lock owner has no token before termination:\n%s", ownerBeforeTermination)
	}
	pullPID := readPID(t, pullPIDFile)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("terminate runner: %v", err)
	}
	readPipeSignal(t, termAckReader, 3*time.Second, "pull helper TERM acknowledgement")
	readPipeSignal(t, gracePauseReader, 3*time.Second, "cleanup grace paused")

	ownerWhileChildBlocked := readFile(t, filepath.Join(lockDir, "owner"))
	if ownerWhileChildBlocked != ownerBeforeTermination {
		t.Fatalf("exact token-bearing lock changed while child was blocked:\nbefore:\n%s\nafter:\n%s",
			ownerBeforeTermination, ownerWhileChildBlocked)
	}
	if !processExists(pullPID) {
		t.Fatalf("TERM-acknowledged pull helper %d exited before release", pullPID)
	}
	probe := exec.Command(filepath.Join(tools.bin, "rmdir"), lockDir)
	probe.Env = command.Env
	probeOutput, probeErr := probe.CombinedOutput()
	var probeExitErr *exec.ExitError
	if !errors.As(probeErr, &probeExitErr) || probeExitErr.ExitCode() != 73 ||
		!strings.Contains(string(probeOutput), "refused live or zombie helper") {
		t.Fatalf("test-private rmdir did not refuse live helper: err=%v output=%s", probeErr, probeOutput)
	}

	if _, err := releaseWriter.Write([]byte{'R'}); err != nil {
		t.Fatalf("release pull helper: %v", err)
	}
	if err := releaseWriter.Close(); err != nil {
		t.Fatalf("close pull-helper release: %v", err)
	}
	if _, err := graceReleaseWriter.Write([]byte{'R'}); err != nil {
		t.Fatalf("release cleanup grace clock: %v", err)
	}
	if err := graceReleaseWriter.Close(); err != nil {
		t.Fatalf("close cleanup-grace release: %v", err)
	}
	waitErr := waiter.wait(3 * time.Second)
	var timeoutErr *commandWaitTimeoutError
	if errors.As(waitErr, &timeoutErr) {
		t.Fatalf("runner waiter timed out after child release: %v\n%s", waitErr, output.String())
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("runner exit = %v, want *exec.ExitError code 130\n%s", waitErr, output.String())
	}
	if exitErr.ExitCode() != 130 {
		t.Fatalf("runner exit code = %d, want 130\n%s", exitErr.ExitCode(), output.String())
	}
	if command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatalf("waiter returned before runner exit: state=%v", command.ProcessState)
	}
	readPipeSignal(t, reapAckReader, 3*time.Second, "post-reap lock removal")
	assertProcessGone(t, pullPID, "terminated fake image pull")
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("runner retained lock after exact child reap: %v", err)
	}
}

func TestIntegrationRunnerFakePullHelper(t *testing.T) {
	if os.Getenv("SEMSTREAMS_TEST_PULL_HELPER") != "1" {
		return
	}
	termAck := os.NewFile(4, "term-ack")
	release := os.NewFile(5, "child-release")
	if termAck == nil || release == nil {
		t.Fatal("inherited helper pipes are unavailable")
	}
	defer termAck.Close()
	defer release.Close()

	termSignal := make(chan os.Signal, 1)
	signal.Notify(termSignal, syscall.SIGTERM)
	defer signal.Stop(termSignal)
	pidFile := os.Getenv("DOCKER_PULL_PID_FILE")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	released := make(chan error, 1)
	go func() {
		var signal [1]byte
		_, err := release.Read(signal[:])
		if errors.Is(err, io.EOF) {
			err = nil
		}
		released <- err
	}()

	select {
	case err := <-released:
		if err != nil {
			t.Fatal(err)
		}
		return
	case <-termSignal:
		termReceivedSentinel := os.Getenv("SEMSTREAMS_TEST_TERM_RECEIVED_SENTINEL")
		if err := os.WriteFile(termReceivedSentinel, []byte("TERM\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := termAck.Write([]byte{'T'}); err != nil {
			t.Fatal(err)
		}
		if err := <-released; err != nil {
			t.Fatal(err)
		}
	}
}

func TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inherited file descriptors and process signals differ on Windows")
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	pullPIDFile := filepath.Join(tempDir, "pull.pid")
	parentReadyPlaceholder, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	termAckPlaceholder, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		_ = parentReadyPlaceholder.Close()
		t.Fatal(err)
	}
	releaseReader, releaseWriter := mustPipe(t)
	allEndpoints := []*os.File{
		parentReadyPlaceholder,
		termAckPlaceholder,
		releaseReader, releaseWriter,
	}
	t.Cleanup(func() {
		for _, endpoint := range allEndpoints {
			_ = endpoint.Close()
		}
	})

	command := exec.Command(testBinary, "-test.run=^TestIntegrationRunnerFakePullHelper$")
	command.Env = append(os.Environ(),
		"SEMSTREAMS_TEST_PULL_HELPER=1",
		"DOCKER_PULL_PID_FILE="+pullPIDFile,
	)
	command.ExtraFiles = []*os.File{parentReadyPlaceholder, termAckPlaceholder, releaseReader}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := newCommandWaiter(command)
	t.Cleanup(func() {
		_ = releaseWriter.Close()
		_ = waiter.killAndWait()
	})
	for _, endpoint := range []*os.File{parentReadyPlaceholder, termAckPlaceholder, releaseReader} {
		if err := endpoint.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := releaseWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waiter.wait(3 * time.Second); err != nil {
		t.Fatalf("helper did not accept pre-TERM release: %v", err)
	}
	if command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatalf("helper waiter returned before exit: state=%v", command.ProcessState)
	}
	helperPID := readPID(t, pullPIDFile)
	if helperPID != command.Process.Pid {
		t.Fatalf("helper PID = %d, command PID = %d", helperPID, command.Process.Pid)
	}
	assertProcessGone(t, helperPID, "pre-TERM-released pull helper")
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

	var timeoutErr *commandWaitTimeoutError
	if err := waiter.wait(10 * time.Millisecond); !errors.As(err, &timeoutErr) {
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
	if [ "${SEMSTREAMS_TEST_PULL_HELPER:-0}" = "1" ]; then
		exec "$SEMSTREAMS_TEST_BINARY" -test.run '^TestIntegrationRunnerFakePullHelper$'
	fi
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

func readPipeSignal(t *testing.T, reader *os.File, timeout time.Duration, description string) {
	t.Helper()
	if err := reader.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set %s deadline: %v", description, err)
	}
	var signal [1]byte
	if _, err := reader.Read(signal[:]); err != nil {
		t.Fatalf("wait for %s: %v", description, err)
	}
}

func mustPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return reader, writer
}

// commandWaiter gives exactly one goroutine ownership of Cmd.Wait. Callers may
// time out, kill during cleanup, and wait again without racing a second Wait.
type commandWaiter struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

type commandWaitTimeoutError struct {
	timeout time.Duration
}

func (e *commandWaitTimeoutError) Error() string {
	return fmt.Sprintf("command did not exit within %s", e.timeout)
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
		return &commandWaitTimeoutError{timeout: timeout}
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

func writeTerminationToolWrappers(t *testing.T, bin string) {
	t.Helper()
	writeExecutable(t, filepath.Join(bin, "date"), `#!/bin/sh
if [ "${SEMSTREAMS_TEST_PULL_HELPER:-0}" = "1" ] && [ -s "$DOCKER_PULL_PID_FILE" ] && [ ! -e "$SEMSTREAMS_TEST_PARENT_READY_SENTINEL" ]; then
  : > "$SEMSTREAMS_TEST_PARENT_READY_SENTINEL"
  printf 'R' >&3
fi
if [ "${SEMSTREAMS_TEST_PULL_HELPER:-0}" = "1" ] && [ -e "$SEMSTREAMS_TEST_TERM_RECEIVED_SENTINEL" ] && mkdir "$SEMSTREAMS_TEST_GRACE_PAUSE_CLAIM" 2>/dev/null; then
  printf 'P' >&7
  dd bs=1 count=1 2>/dev/null <&8 >/dev/null || true
fi
exec "$SEMSTREAMS_TEST_REAL_DATE" "$@"
`)
	writeExecutable(t, filepath.Join(bin, "rmdir"), `#!/bin/sh
if [ "${SEMSTREAMS_TEST_PULL_HELPER:-0}" = "1" ] && [ "$#" -eq 1 ] && [ "$1" = "$SEMSTREAMS_TEST_LOCK_DIR" ]; then
  helper_pid=$(cat "$DOCKER_PULL_PID_FILE")
  if kill -0 "$helper_pid" 2>/dev/null; then
    echo "test rmdir refused live or zombie helper $helper_pid" >&2
    exit 73
  fi
  printf 'R' >&6
fi
exec "$SEMSTREAMS_TEST_REAL_RMDIR" "$@"
`)
}

func closeFilesOnCleanup(t *testing.T, files ...*os.File) {
	t.Helper()
	t.Cleanup(func() {
		for _, file := range files {
			_ = file.Close()
		}
	})
}

func closeInheritedFiles(t *testing.T, files map[string]*os.File) {
	t.Helper()
	for name, file := range files {
		if err := file.Close(); err != nil {
			t.Fatalf("close inherited %s: %v", name, err)
		}
	}
}

func mustExecutable(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return executable
}

func mustLookPath(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return path
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
	if processExists(pid) {
		t.Fatalf("%s process %d is still alive", description, pid)
	}
}

func processExists(pid int) bool {
	return exec.Command("kill", "-0", fmt.Sprintf("%d", pid)).Run() == nil
}

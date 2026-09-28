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
		// gh#736: at most two test packages at a time. Uncapped, every
		// Docker-backed package boots containers concurrently and container
		// starts miss their own budgets.
		"-p",
		"2",
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
	// The runner fixture sets its pull watchdog to 30s. Give that owned
	// operation and its 1s termination grace a single 35s containment window;
	// this is a failure bound, not a performance assertion. The unmodified
	// fixture measured 1.37s under -race, and the delayed case measured 4.26s.
	// Cleanup has at most 6s: 2s runner release, 2s forced Wait,
	// and 2s to observe helper exit through the private release gate.
	const fixtureBudget = 35 * time.Second
	fixtureStarted := time.Now()
	fixtureCtx, cancelFixture := context.WithTimeout(t.Context(), fixtureBudget)
	t.Cleanup(cancelFixture)
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
		"SEMSTREAMS_TEST_READY_DELAY_SECONDS":            "3.2",
	})
	// A file avoids Cmd.Wait's copy goroutine: a surviving descendant may
	// inherit stdout, but cannot hold the sole waiter hostage after runner exit.
	outputFile := attachRunnerOutputFile(t, command, tempDir)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := newCommandWaiter(command)
	t.Cleanup(func() {
		if err := cleanupRunnerAndPull(waiter, pullPIDFile, releaseWriter, graceReleaseWriter); err != nil {
			t.Error(err)
		}
	})
	closeInheritedFiles(t, map[string]*os.File{
		"parent-ready writer":  parentReadyWriter,
		"TERM-ack writer":      termAckWriter,
		"child-release reader": releaseReader,
		"reap-check writer":    reapAckWriter,
		"grace-pause writer":   gracePauseWriter,
		"grace-release reader": graceReleaseReader,
	})

	readRunnerSignal(fixtureCtx, t, fixtureStarted, parentReadyReader, waiter, 0, false, "parent retained pull PID")
	ownerBeforeTermination := readFile(t, filepath.Join(lockDir, "owner"))
	if !strings.Contains(ownerBeforeTermination, "token=") {
		t.Fatalf("lock owner has no token before termination:\n%s", ownerBeforeTermination)
	}
	pullPID := readPID(t, pullPIDFile)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("terminate runner: %v", err)
	}
	readRunnerSignal(fixtureCtx, t, fixtureStarted, termAckReader, waiter, pullPID, false, "pull helper TERM acknowledgement")
	readRunnerSignal(fixtureCtx, t, fixtureStarted, gracePauseReader, waiter, pullPID, false, "cleanup grace paused")

	ownerWhileChildBlocked := readFile(t, filepath.Join(lockDir, "owner"))
	if ownerWhileChildBlocked != ownerBeforeTermination {
		t.Fatalf("exact token-bearing lock changed while child was blocked:\nbefore:\n%s\nafter:\n%s",
			ownerBeforeTermination, ownerWhileChildBlocked)
	}
	if !processExists(pullPID) {
		t.Fatalf("TERM-acknowledged pull helper %d exited before release", pullPID)
	}
	probe, probeOutputFile := newRunnerRmdirProbe(fixtureCtx, t, filepath.Join(tools.bin, "rmdir"), lockDir, command.Env)
	probeErr := probe.Run()
	probeOutput := readFile(t, probeOutputFile.Name())
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
	readRunnerSignal(fixtureCtx, t, fixtureStarted, reapAckReader, waiter, pullPID, true, "post-reap lock removal")
	fixtureDeadline, _ := fixtureCtx.Deadline()
	waitErr := waiter.wait(time.Until(fixtureDeadline))
	var timeoutErr *commandWaitTimeoutError
	if errors.As(waitErr, &timeoutErr) {
		t.Fatalf("runner did not exit after child release: elapsed=%s budget=%s: %v", time.Since(fixtureStarted), fixtureBudget, waitErr)
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("runner exit = %v, want *exec.ExitError code 130\n%s", waitErr, readFile(t, outputFile.Name()))
	}
	if exitErr.ExitCode() != 130 {
		t.Fatalf("runner exit code = %d, want 130\n%s", exitErr.ExitCode(), readFile(t, outputFile.Name()))
	}
	if command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatalf("waiter returned before runner exit: state=%v", command.ProcessState)
	}
	assertProcessGone(t, pullPID, "terminated fake image pull")
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("runner retained lock after exact child reap: %v", err)
	}
}

func TestIntegrationRunner_RmdirProbeHonorsFixtureCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inherited file descriptors differ on Windows")
	}
	tools := newFakeToolchain(t)
	writeExecutable(t, filepath.Join(tools.bin, "rmdir"), `#!/bin/sh
printf 'held rmdir probe\n'
printf R >&4
read x <&3
`)
	holdReader, holdWriter := mustPipe(t)
	readyReader, readyWriter := mustPipe(t)
	closeFilesOnCleanup(t, holdReader, holdWriter, readyReader, readyWriter)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	probe, outputFile := newRunnerRmdirProbe(ctx, t, filepath.Join(tools.bin, "rmdir"),
		filepath.Join(t.TempDir(), "integration.lock"), nil)
	probe.ExtraFiles = []*os.File{holdReader, readyWriter}
	started := time.Now()
	if err := probe.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := newCommandWaiter(probe)
	t.Cleanup(func() {
		cancel()
		_ = holdWriter.Close()
		assertCommandCleanupFinished(t, waiter, "held rmdir probe")
	})
	closeInheritedFiles(t, map[string]*os.File{"hold reader": holdReader, "ready writer": readyWriter})
	readRunnerSignal(ctx, t, started, readyReader, waiter, 0, false, "rmdir probe ready")
	select {
	case <-waiter.done:
		t.Fatalf("rmdir probe exited before fixture cancellation: %v", waiter.err)
	default:
	}
	cancel()
	probeErr := waiter.wait(2 * time.Second)
	if isCommandWaitTimeout(probeErr) {
		t.Fatalf("canceled rmdir probe did not join within terminal bound: %v", probeErr)
	}
	if probeErr == nil || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("held probe result = %v, context = %v; want fixture cancellation", probeErr, ctx.Err())
	}
	if probe.ProcessState == nil {
		t.Fatalf("canceled probe returned without reaping: state=%v", probe.ProcessState)
	}
	assertProcessGone(t, probe.Process.Pid, "canceled rmdir probe")
	if output := readFile(t, outputFile.Name()); !strings.Contains(output, "held rmdir probe") {
		t.Fatalf("probe file-backed output missing controlled marker: %q", output)
	}
}

func TestIntegrationRunner_ReadySignalFailuresAreDiagnosticAndReaped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	for _, testCase := range []struct {
		name       string
		earlyExit  bool
		wantDetail string
	}{
		{name: "child exits before signal", earlyExit: true, wantDetail: "before signal"},
		{name: "live child never signals", wantDetail: "no signal within fixture budget"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repoRoot := findRepoRoot(t)
			tools := newFakeToolchain(t)
			tempDir := t.TempDir()
			pullPIDFile := filepath.Join(tempDir, "pull.pid")
			writeTerminationToolWrappers(t, tools.bin)
			parentReadyReader, parentReadyWriter := mustPipe(t)
			termAckReader, termAckWriter := mustPipe(t)
			releaseReader, releaseWriter := mustPipe(t)
			closeFilesOnCleanup(t, parentReadyReader, parentReadyWriter, termAckReader, termAckWriter, releaseReader, releaseWriter)

			command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
			command.Dir = repoRoot
			command.ExtraFiles = []*os.File{parentReadyWriter, termAckWriter, releaseReader}
			command.Env = runnerEnvironment(tools, filepath.Join(tempDir, "integration.lock"), map[string]string{
				"DOCKER_IMAGE_INSPECT_STATUS":                    "1",
				"DOCKER_PULL_PID_FILE":                           pullPIDFile,
				"SEMSTREAMS_CONTRACT_IMAGE_PULL_TIMEOUT_SECONDS": "30",
				"SEMSTREAMS_TEST_PARENT_READY_SENTINEL":          filepath.Join(tempDir, "ready.sent"),
				"SEMSTREAMS_TEST_SUPPRESS_PARENT_READY":          "1",
				"SEMSTREAMS_TEST_PULL_HELPER":                    "1",
				"SEMSTREAMS_TEST_BINARY":                         mustExecutable(t),
				"SEMSTREAMS_TEST_REAL_DATE":                      mustLookPath(t, "date"),
				"SEMSTREAMS_TEST_REAL_RMDIR":                     mustLookPath(t, "rmdir"),
				"SEMSTREAMS_TEST_PULL_EXIT_EARLY":                fmt.Sprint(testCase.earlyExit),
			})
			outputFile := attachRunnerOutputFile(t, command, tempDir)
			started := time.Now()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			waiter := newCommandWaiter(command)
			t.Cleanup(func() {
				if err := cleanupRunnerAndPull(waiter, pullPIDFile, releaseWriter); err != nil {
					t.Error(err)
				}
			})
			closeInheritedFiles(t, map[string]*os.File{
				"parent-ready writer":  parentReadyWriter,
				"TERM-ack writer":      termAckWriter,
				"child-release reader": releaseReader,
			})
			waitForFileContent(t, pullPIDFile, "\n", 5*time.Second)
			if !testCase.earlyExit && !processExists(readPID(t, pullPIDFile)) {
				t.Fatal("missing-signal control did not retain a live pull helper")
			}
			budget := 5 * time.Second
			if !testCase.earlyExit {
				budget = 300 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			err := observeRunnerSignal(ctx, started, parentReadyReader, waiter, 0, false, "parent retained pull PID")
			if err == nil || !strings.Contains(err.Error(), "parent retained pull PID") || !strings.Contains(err.Error(), testCase.wantDetail) {
				t.Fatalf("signal failure = %v, want phase and %q; runner output:\n%s", err, testCase.wantDetail, readFile(t, outputFile.Name()))
			}
			if testCase.earlyExit {
				waitErr := waiter.wait(2 * time.Second)
				var exitErr *exec.ExitError
				if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 1 {
					t.Fatalf("early child exit did not reach runner: wait=%v diagnostic=%v", waitErr, err)
				}
			}
		})
	}
}

func TestObserveRunnerSignal_TerminalPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inherited file descriptors differ on Windows")
	}
	t.Run("EOF while runner remains live", func(t *testing.T) {
		releaseReader, releaseWriter := mustPipe(t)
		signalReader, signalWriter := mustPipe(t)
		closeFilesOnCleanup(t, releaseReader, releaseWriter, signalReader, signalWriter)
		command := exec.Command("/bin/sh", "-c", "read x <&3")
		command.ExtraFiles = []*os.File{releaseReader}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		waiter := newCommandWaiter(command)
		t.Cleanup(func() {
			_ = releaseWriter.Close()
			if err := waiter.wait(2 * time.Second); isCommandWaitTimeout(err) {
				t.Errorf("held runner did not exit after release: %v", err)
			}
		})
		closeInheritedFiles(t, map[string]*os.File{"release reader": releaseReader})
		if err := signalWriter.Close(); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		err := observeRunnerSignal(ctx, time.Now(), signalReader, waiter, 0, false, "ready")
		if ctx.Err() != nil {
			t.Fatalf("EOF exhausted the single control bound while runner was deliberately held live: %v", err)
		}
		if err == nil || !strings.Contains(err.Error(), "ready") || !strings.Contains(err.Error(), "EOF") {
			t.Fatalf("live-runner EOF result = %v", err)
		}
		select {
		case <-waiter.done:
			t.Fatal("runner exited before EOF was observed")
		default:
		}
	})

	t.Run("runner exits while descendant holds writer", func(t *testing.T) {
		releaseReader, releaseWriter := mustPipe(t)
		signalReader, signalWriter := mustPipe(t)
		closeFilesOnCleanup(t, releaseReader, releaseWriter, signalReader, signalWriter)
		pidFile := filepath.Join(t.TempDir(), "descendant.pid")
		command := exec.Command("/bin/sh", "-c", "( read x <&3 ) & printf '%s\\n' \"$!\" > \"$CHILD_PID_FILE\"; exit 7")
		command.ExtraFiles = []*os.File{releaseReader, signalWriter}
		command.Env = append(os.Environ(), "CHILD_PID_FILE="+pidFile)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		waiter := newCommandWaiter(command)
		t.Cleanup(func() {
			if err := cleanupRunnerAndPull(waiter, pidFile, releaseWriter); err != nil {
				t.Error(err)
			}
		})
		closeInheritedFiles(t, map[string]*os.File{"release reader": releaseReader, "signal writer": signalWriter})
		waitForFileContent(t, pidFile, "\n", 2*time.Second)
		if err := waiter.wait(2 * time.Second); isCommandWaitTimeout(err) {
			t.Fatalf("runner did not exit while descendant retained writer: %v", err)
		}
		childPID := readPID(t, pidFile)
		if !processExists(childPID) {
			t.Fatalf("descendant %d did not retain the signal writer", childPID)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		err := observeRunnerSignal(ctx, time.Now(), signalReader, waiter, 0, false, "ready")
		if ctx.Err() != nil {
			t.Fatalf("runner exit exhausted the single control bound before descendant writer release: %v", err)
		}
		if err == nil || !strings.Contains(err.Error(), "ready") || !strings.Contains(err.Error(), "runner exited") {
			t.Fatalf("exited-runner retained-writer result = %v", err)
		}
		if !processExists(childPID) {
			t.Fatal("descendant released writer before terminal error")
		}
		if err := cleanupRunnerAndPull(waiter, pidFile, releaseWriter); err != nil {
			t.Fatal(err)
		}
		assertProcessGone(t, childPID, "released descendant after parent exit")
	})

	t.Run("buffered final acknowledgement after exit", func(t *testing.T) {
		signalReader, signalWriter := mustPipe(t)
		closeFilesOnCleanup(t, signalReader, signalWriter)
		command := exec.Command("/bin/sh", "-c", "printf R >&3")
		command.ExtraFiles = []*os.File{signalWriter}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		waiter := newCommandWaiter(command)
		closeInheritedFiles(t, map[string]*os.File{"signal writer": signalWriter})
		if err := waiter.wait(2 * time.Second); err != nil {
			t.Fatalf("writer command exit: %v", err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		if err := observeRunnerSignal(ctx, time.Now(), signalReader, waiter, 0, true, "final ack"); err != nil {
			t.Fatalf("buffered final acknowledgement lost after runner exit: %v", err)
		}
	})
}

func TestRunnerFixtureCleanup_RefusesUnprovenPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inherited file descriptors differ on Windows")
	}
	releaseReader, releaseWriter := mustPipe(t)
	holdReader, holdWriter := mustPipe(t)
	closeFilesOnCleanup(t, releaseReader, releaseWriter, holdReader, holdWriter)
	foreign := exec.Command("/bin/sh", "-c", "read x <&3")
	foreign.ExtraFiles = []*os.File{holdReader}
	if err := foreign.Start(); err != nil {
		t.Fatal(err)
	}
	foreignWaiter := newCommandWaiter(foreign)
	t.Cleanup(func() {
		_ = holdWriter.Close()
		assertCommandCleanupFinished(t, foreignWaiter, "foreign controlled process")
	})
	closeInheritedFiles(t, map[string]*os.File{"held child reader": holdReader, "unused release reader": releaseReader})
	command := exec.Command("/bin/sh", "-c", "exit 0")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := newCommandWaiter(command)
	if err := waiter.wait(2 * time.Second); isCommandWaitTimeout(err) {
		t.Fatalf("runner did not exit before cleanup: %v", err)
	}
	pidFile := filepath.Join(t.TempDir(), "stale-helper.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", foreign.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cleanupRunnerAndPull(waiter, pidFile, releaseWriter)
	if err == nil || !strings.Contains(err.Error(), "ownership unproven, refusing to signal") {
		t.Fatalf("stale PID cleanup = %v, want refusal without signal", err)
	}
	if !processExists(foreign.Process.Pid) {
		t.Fatalf("cleanup signaled unrelated controlled process %d", foreign.Process.Pid)
	}
	if err := holdWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := foreignWaiter.wait(2 * time.Second); isCommandWaitTimeout(err) {
		t.Fatalf("foreign process did not exit after its own release: %v", err)
	}
	assertProcessGone(t, foreign.Process.Pid, "foreign controlled process after its own release")
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
	if os.Getenv("SEMSTREAMS_TEST_PULL_EXIT_EARLY") == "true" {
		os.Exit(42)
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
		assertCommandCleanupFinished(t, waiter, "pre-TERM pull helper")
	})
	for _, endpoint := range []*os.File{parentReadyPlaceholder, termAckPlaceholder, releaseReader} {
		if err := endpoint.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := releaseWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waiter.wait(untilTestDeadline(t)); err != nil {
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
	t.Cleanup(func() { assertCommandCleanupFinished(t, waiter, "timeout-cleaned command") })

	var timeoutErr *commandWaitTimeoutError
	if err := waiter.wait(10 * time.Millisecond); !errors.As(err, &timeoutErr) {
		t.Fatalf("wait before cleanup = %v, want bounded timeout", err)
	}
	err := waiter.killAndWait()
	if isCommandWaitTimeout(err) {
		t.Fatalf("killed command still has incomplete Wait: %v", err)
	}
	if err == nil {
		t.Fatal("killed command unexpectedly reported success")
	}
	select {
	case <-waiter.done:
	default:
		t.Fatal("cleanup returned without completing Wait")
	}
	if command.ProcessState == nil {
		t.Fatalf("cleanup returned before command was reaped: state=%v", command.ProcessState)
	}
	assertProcessGone(t, pid, "timeout-cleaned command")
	if err := waiter.wait(time.Second); err == nil {
		t.Fatal("repeated wait lost the command's killed result")
	}
}

func TestCommandWaiter_RetainedOutputReportsIncompleteCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inherited file descriptors differ on Windows")
	}
	releaseReader, releaseWriter := mustPipe(t)
	readyReader, readyWriter := mustPipe(t)
	closeFilesOnCleanup(t, releaseReader, releaseWriter, readyReader, readyWriter)
	// The child retains the stdout pipe and waits on FD 3. Killing its parent
	// must not let a bare Cmd.Wait receive hang the test until package timeout.
	command := exec.Command("/bin/sh", "-c", "( read x <&3 ) & printf R >&4; wait")
	command.ExtraFiles = []*os.File{releaseReader, readyWriter}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := newCommandWaiter(command)
	t.Cleanup(func() {
		_ = releaseWriter.Close()
		assertCommandCleanupFinished(t, waiter, "retained-output command")
	})
	closeInheritedFiles(t, map[string]*os.File{"release reader": releaseReader, "ready writer": readyWriter})
	if err := readyReader.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var signal [1]byte
	if _, err := readyReader.Read(signal[:]); err != nil || signal[0] != 'R' {
		t.Fatalf("child did not retain command output: signal=%q err=%v", signal, err)
	}
	if err := waiter.killAndWait(); !isCommandWaitTimeout(err) {
		t.Fatalf("retained stdout cleanup = %v, want bounded incomplete-cleanup error", err)
	}
	select {
	case <-waiter.done:
		t.Fatal("retained stdout command unexpectedly joined before descendant release")
	default:
	}
	if _, err := releaseWriter.Write([]byte{'R'}); err != nil {
		t.Fatal(err)
	}
	if err := releaseWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waiter.wait(2 * time.Second); isCommandWaitTimeout(err) {
		t.Fatalf("command did not join after descendant released: %v", err)
	}
	if command.ProcessState == nil {
		t.Fatal("command waiter reported completion without reaping parent")
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
		assertCommandCleanupFinished(t, holderWaiter, "lock holder")
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
	if err := holderWaiter.wait(untilTestDeadline(t)); err != nil {
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

// TestIntegrationRunner_PublishesLatencyEvidence pins the #1284 evidence channel end to end.
//
// `go test` discards a PASSING package's output unless -v is passed, and this runner deliberately
// stays un-verbose over ./..., so the graph-index owner-filter harness appends its per-repetition
// distribution to the file named by GRAPH_INDEX_LATENCY_LOG and the runner prints it. Nothing else
// observes that wiring: the harness returns silently when the variable is unset, so deleting the
// runner's export would restore the exact state #1284 exists to fix — a gate that records nothing
// on the runs that pass it — with every other test still green. This test is what makes that
// deletion red.
func TestIntegrationRunner_PublishesLatencyEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration runner is a bash script")
	}
	repoRoot := findRepoRoot(t)
	tools := newFakeToolchain(t)
	lockDir := filepath.Join(t.TempDir(), "integration.lock")

	const marker = "filter=probe reps=5 p50=1ms submitted=1ms,2ms,3ms,4ms,5ms"
	command := exec.Command(filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	command.Dir = repoRoot
	command.Env = runnerEnvironment(tools, lockDir, map[string]string{"FAKE_LATENCY_LINE": marker})
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("runner failed: %v\n%s", err, output)
	}

	rendered := string(output)
	if !strings.Contains(rendered, marker) {
		t.Fatalf("runner did not publish the distribution the harness wrote.\n"+
			"GRAPH_INDEX_LATENCY_LOG must be exported before `go test` and its contents printed after, "+
			"or a passing run records nothing (#1284).\nwant substring: %s\ngot:\n%s", marker, rendered)
	}
	if !strings.Contains(rendered, "recorded latency distributions") {
		t.Fatalf("runner printed the line but not its header; want %q\ngot:\n%s",
			"recorded latency distributions", rendered)
	}
}

// TestIntegrationRunner_LatencyEnvNameMatchesHarness closes the other direction: the runner and the
// harness name the same variable. The harness constant is under a build tag this package does not
// carry, so the name is compared as a literal against both files.
func TestIntegrationRunner_LatencyEnvNameMatchesHarness(t *testing.T) {
	repoRoot := findRepoRoot(t)
	const envName = "GRAPH_INDEX_LATENCY_LOG"

	runner := readFile(t, filepath.Join(repoRoot, "scripts", "run-integration-tests.sh"))
	if !strings.Contains(runner, "export "+envName+"=") {
		t.Fatalf("run-integration-tests.sh no longer exports %s; the #1284 evidence channel is broken", envName)
	}

	harness := readFile(t, filepath.Join(repoRoot, "processor", "graph-index", "owner_filter_load_integration_test.go"))
	if !strings.Contains(harness, `ownerLoadDistributionLogEnv = "`+envName+`"`) {
		t.Fatalf("owner_filter_load_integration_test.go no longer names %s; renaming one side silently "+
			"stops publishing the distribution (#1284)", envName)
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
# Stand in for the graph-index harness writing its distribution (#1284). Guarded on both
# variables so every other test in this file is unaffected, and so a runner that stops
# exporting GRAPH_INDEX_LATENCY_LOG writes nothing rather than failing obscurely.
if [ -n "${GRAPH_INDEX_LATENCY_LOG:-}" ] && [ -n "${FAKE_LATENCY_LINE:-}" ]; then
	printf '%s\n' "$FAKE_LATENCY_LINE" >> "$GRAPH_INDEX_LATENCY_LOG"
fi
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

func readRunnerSignal(ctx context.Context, t *testing.T, started time.Time, reader *os.File, waiter *commandWaiter, ownedPID int, allowBufferedAfterExit bool, description string) {
	t.Helper()
	if err := observeRunnerSignal(ctx, started, reader, waiter, ownedPID, allowBufferedAfterExit, description); err != nil {
		t.Fatal(err)
	}
}

func observeRunnerSignal(ctx context.Context, started time.Time, reader *os.File, waiter *commandWaiter, ownedPID int, allowBufferedAfterExit bool, description string) error {
	deadline, _ := ctx.Deadline()
	if err := reader.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("set %s deadline: %w", description, err)
	}
	type pipeResult struct {
		value byte
		err   error
	}
	readDone := make(chan pipeResult, 1)
	go func() {
		var signal [1]byte
		_, err := reader.Read(signal[:])
		readDone <- pipeResult{value: signal[0], err: err}
	}()
	var result pipeResult
	select {
	case result = <-readDone:
	case <-waiter.done:
		if ownedPID > 0 && processExists(ownedPID) {
			_ = reader.Close()
			<-readDone
			return fmt.Errorf("%s: runner exited while owned pull helper %d was still alive: exit=%v elapsed=%s",
				description, ownedPID, waiter.err, time.Since(started))
		}
		if !allowBufferedAfterExit {
			_ = reader.Close()
			<-readDone
			return fmt.Errorf("%s: runner exited before signal: exit=%v elapsed=%s",
				description, waiter.err, time.Since(started))
		}
		// A signal can be buffered just before runner exit (the post-reap
		// acknowledgement does this). Only this final phase may drain the
		// already-written byte after exit; the pipe's deadline remains bounded.
		result = <-readDone
		if result.err != nil {
			return fmt.Errorf("%s: runner exited before signal: exit=%v elapsed=%s read=%v",
				description, waiter.err, time.Since(started), result.err)
		}
	case <-ctx.Done():
		_ = reader.SetReadDeadline(time.Now())
		result = <-readDone
		if result.err != nil {
			return fmt.Errorf("%s: no signal within fixture budget: elapsed=%s deadline=%s runner_pid=%d last_read=%v",
				description, time.Since(started), deadline.Format(time.RFC3339Nano), waiter.command.Process.Pid, result.err)
		}
	}
	if result.err != nil {
		if errors.Is(result.err, io.EOF) {
			select {
			case <-waiter.done:
				return fmt.Errorf("%s: runner exited before signal: exit=%v elapsed=%s read=%v",
					description, waiter.err, time.Since(started), result.err)
			default:
				return fmt.Errorf("%s: pipe closed before signal while runner remained live: elapsed=%s runner_pid=%d last_read=%v",
					description, time.Since(started), waiter.command.Process.Pid, result.err)
			}
		}
		select {
		case <-waiter.done:
			return fmt.Errorf("%s: runner exited before signal: exit=%v elapsed=%s read=%v",
				description, waiter.err, time.Since(started), result.err)
		default:
		}
		if ctx.Err() != nil || errors.Is(result.err, os.ErrDeadlineExceeded) {
			return fmt.Errorf("%s: no signal within fixture budget: elapsed=%s deadline=%s runner_pid=%d last_read=%v",
				description, time.Since(started), deadline.Format(time.RFC3339Nano), waiter.command.Process.Pid, result.err)
		}
		return fmt.Errorf("%s: signal unavailable: elapsed=%s runner_pid=%d last_read=%v",
			description, time.Since(started), waiter.command.Process.Pid, result.err)
	}
	if result.value == 0 {
		return fmt.Errorf("%s: empty signal from runner after %s", description, time.Since(started))
	}
	return nil
}

func mustPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return reader, writer
}

func attachRunnerOutputFile(t *testing.T, command *exec.Cmd, tempDir string) *os.File {
	t.Helper()
	outputFile, err := os.Create(filepath.Join(tempDir, "runner-output"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outputFile.Close() })
	command.Stdout, command.Stderr = outputFile, outputFile
	return outputFile
}

func newRunnerRmdirProbe(ctx context.Context, t *testing.T, binary, lockDir string, env []string) (*exec.Cmd, *os.File) {
	t.Helper()
	probe := exec.CommandContext(ctx, binary, lockDir)
	probe.Env = env
	return probe, attachRunnerOutputFile(t, probe, t.TempDir())
}

// untilTestDeadline is the wait for a process the test has already released:
// "it exits" is the assertion, and the bound is the one the test binary
// enforces anyway. A per-step number under that bound is a predicted budget
// for work the test does not control — bash spawning the fake toolchain
// under parallel-suite load — and 3s fired 2 of 10 times under load on the
// day it landed (gh#1290). Deleted, not widened, per the #1284 ruling. The
// grace keeps the failure the test's own, reported with the process output,
// rather than the binary's deadline panic.
func untilTestDeadline(t *testing.T) time.Duration {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Minute) // go test's default -timeout
	}
	return time.Until(deadline) - 5*time.Second
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
		select {
		case <-w.done:
			return w.err
		default:
		}
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
	if err := w.wait(2 * time.Second); isCommandWaitTimeout(err) {
		return fmt.Errorf("killed command %d but Wait did not finish: %w", w.command.Process.Pid, err)
	}
	if w.err != nil {
		return w.err
	}
	return killErr
}

func isCommandWaitTimeout(err error) bool {
	var timeoutErr *commandWaitTimeoutError
	return errors.As(err, &timeoutErr)
}

func assertCommandCleanupFinished(t *testing.T, waiter *commandWaiter, description string) {
	t.Helper()
	if err := waiter.killAndWait(); isCommandWaitTimeout(err) {
		t.Errorf("%s cleanup left Wait unfinished: %v", description, err)
	}
	select {
	case <-waiter.done:
	default:
		t.Errorf("%s cleanup returned with Wait still running", description)
	}
}

func readExistingPID(path string) (int, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(body)), "%d", &pid); err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid PID in %s: value=%q err=%v", path, body, err)
	}
	return pid, nil
}

// cleanupRunnerAndPull owns the runner, its recorded helper, and fixture
// release gates. A parent exit alone cannot establish child cleanup.
// The PID file is observation only: it does not authorize signaling a PID.
func cleanupRunnerAndPull(waiter *commandWaiter, pidFile string, releases ...*os.File) error {
	var failures []error
	for _, release := range releases {
		if err := release.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			failures = append(failures, fmt.Errorf("close runner fixture release gate: %w", err))
		}
	}
	if err := waiter.wait(2 * time.Second); isCommandWaitTimeout(err) {
		if killErr := waiter.killAndWait(); isCommandWaitTimeout(killErr) {
			failures = append(failures, fmt.Errorf("runner cleanup did not join after forced kill: %w", killErr))
		}
	}
	select {
	case <-waiter.done:
		if waiter.command.ProcessState == nil {
			failures = append(failures, errors.New("runner cleanup left process unreaped"))
		}
	default:
		failures = append(failures, errors.New("runner cleanup left Wait unfinished"))
	}
	pid, err := readExistingPID(pidFile)
	if err != nil {
		failures = append(failures, fmt.Errorf("runner cleanup cannot verify pull helper: %w", err))
		return errors.Join(failures...)
	}
	if !waitForProcessGone(pid, 2*time.Second) {
		failures = append(failures, fmt.Errorf("recorded pull helper PID %d remains after fixture release; ownership unproven, refusing to signal", pid))
	}
	return errors.Join(failures...)
}

func waitForProcessGone(pid int, budget time.Duration) bool {
	deadline := time.NewTimer(budget)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for processExists(pid) {
		select {
		case <-deadline.C:
			return !processExists(pid)
		case <-ticker.C:
		}
	}
	return true
}

func writeTerminationToolWrappers(t *testing.T, bin string) {
	t.Helper()
	writeExecutable(t, filepath.Join(bin, "date"), `#!/bin/sh
if [ "${SEMSTREAMS_TEST_PULL_HELPER:-0}" = "1" ] && [ -s "$DOCKER_PULL_PID_FILE" ] && [ ! -e "$SEMSTREAMS_TEST_PARENT_READY_SENTINEL" ]; then
  : > "$SEMSTREAMS_TEST_PARENT_READY_SENTINEL"
  if [ -n "${SEMSTREAMS_TEST_READY_DELAY_SECONDS:-}" ]; then
    sleep "$SEMSTREAMS_TEST_READY_DELAY_SECONDS"
  fi
  if [ "${SEMSTREAMS_TEST_SUPPRESS_PARENT_READY:-0}" != "1" ]; then
    printf 'R' >&3
  fi
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

//go:build unix

package testinfra_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const cleanupProcessBudget = 10 * time.Second
const cleanupSupervisorFailure = "cleanup supervisor failure: "

// The missing-acknowledgement witness holds the parent at the real protocol
// boundary and shortens only that failure budget, without a readiness sleep.
type cleanupProcessProbe struct {
	acknowledgementBudget        time.Duration
	holdAcknowledgementUntilExit bool
}

type cleanupProcessEvent struct {
	Group        int    `json:"group,omitempty"`
	Completed    bool   `json:"completed,omitempty"`
	CommandError string `json:"command_error,omitempty"`
	ExitCode     int    `json:"exit_code"`
}

type cleanupProcessOutcome struct {
	Output       []byte
	CommandError error
	CleanupError error
	Group        int
	ExitCode     int
}

// The supervisor remains the live ownership anchor until it signals its own
// group. The parent only closes its control pipe and observes absence; a stored
// numeric PID/PGID is never authority for the parent to send a killing signal.
func runOwnedCleanupCommand(ctx context.Context, root string, args, env []string, probes ...cleanupProcessProbe) cleanupProcessOutcome {
	probe := cleanupProcessProbe{acknowledgementBudget: cleanupProcessBudget}
	if len(probes) > 0 {
		probe = probes[0]
	}
	var outcome cleanupProcessOutcome
	binary, err := os.Executable()
	if err != nil {
		outcome.CommandError = err
		return outcome
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		outcome.CommandError = err
		return outcome
	}
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		outcome.CommandError = err
		return outcome
	}
	defer controlRead.Close()
	defer controlWrite.Close()
	resultRead, resultWrite, err := os.Pipe()
	if err != nil {
		outcome.CommandError = err
		return outcome
	}
	defer resultRead.Close()
	defer resultWrite.Close()
	output, err := os.CreateTemp(root, "admission-output-*.log")
	if err != nil {
		outcome.CommandError = err
		return outcome
	}
	defer output.Close()
	command := exec.Command(binary, "-test.run", "^TestCleanupAdmissionSupervisorProcess$")
	command.Dir = root
	command.Env = append(env, "SEMSTREAMS_CLEANUP_SUPERVISOR_ARGS="+string(encoded),
		"SEMSTREAMS_CLEANUP_CONTROL_ID="+cleanupDescriptorIdentity(int(controlRead.Fd())),
		"SEMSTREAMS_CLEANUP_RESULT_ID="+cleanupDescriptorIdentity(int(resultWrite.Fd())),
		"SEMSTREAMS_CLEANUP_ACK_BUDGET="+probe.acknowledgementBudget.String())
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.ExtraFiles = []*os.File{controlRead, resultWrite}
	// File output cannot be held open as an os/exec copying pipe by descendants.
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		outcome.CommandError = err
		return outcome
	}
	outcome.Group = command.Process.Pid
	_ = controlRead.Close()
	_ = resultWrite.Close()
	waiter := newCommandWaiter(command)
	messages, readerDone := readCleanupProcessEvents(resultRead)
	acknowledged := false
	for phase := 0; phase < 2; phase++ {
		select {
		case <-ctx.Done():
			outcome.CommandError = ctx.Err()
		case message := <-messages:
			if message.err != nil {
				outcome.CommandError = fmt.Errorf("supervisor protocol: %w", message.err)
			} else if phase == 0 {
				if message.event.Group != outcome.Group || message.event.Completed {
					outcome.CommandError = fmt.Errorf("supervisor ownership mismatch: %+v, owned PID %d", message.event, outcome.Group)
				} else {
					acknowledged = true
				}
			} else if !message.event.Completed {
				outcome.CommandError = fmt.Errorf("supervisor omitted command completion: %+v", message.event)
			} else {
				outcome.ExitCode = message.event.ExitCode
				if message.event.CommandError != "" {
					outcome.CommandError = errors.New(message.event.CommandError)
				}
			}
		}
		if outcome.CommandError != nil {
			break
		}
	}
	if probe.holdAcknowledgementUntilExit {
		select {
		case <-waiter.done:
		case <-ctx.Done():
			outcome.CommandError = errors.Join(outcome.CommandError, ctx.Err())
		}
	}
	// Cleanup has its own finite budget even when the operation context is dead.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupProcessBudget)
	defer cancel()
	_ = controlWrite.Close()
	select {
	case <-waiter.done:
		var exit *exec.ExitError
		if !errors.As(waiter.err, &exit) {
			outcome.CleanupError = fmt.Errorf("supervisor did not terminate by its owned group cancellation: %v", waiter.err)
		} else if status, ok := exit.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			outcome.CleanupError = fmt.Errorf("unexpected supervisor termination: %v", waiter.err)
		}
	case <-cleanupCtx.Done():
		outcome.CleanupError = fmt.Errorf("supervisor Wait remains unresolved: %w", cleanupCtx.Err())
	}
	if err := awaitCleanupProcessAbsence(cleanupCtx, -outcome.Group); err != nil {
		outcome.CleanupError = errors.Join(outcome.CleanupError, fmt.Errorf("owned group %d cleanup: %w", outcome.Group, err))
	}
	_ = resultRead.Close()
	<-readerDone
	if !acknowledged && outcome.CommandError == nil {
		outcome.CommandError = errors.New("supervisor never acknowledged process-group ownership")
	}
	outcome.Output, err = os.ReadFile(output.Name())
	outcome.CleanupError = errors.Join(outcome.CleanupError, err)
	if bytes.Contains(outcome.Output, []byte(cleanupSupervisorFailure)) {
		outcome.CleanupError = errors.Join(outcome.CleanupError, errors.New("supervisor reported a protocol failure; see captured output"))
	}
	if bytes.Contains(outcome.Output, []byte("fatal error: all goroutines are asleep")) {
		outcome.CleanupError = errors.Join(outcome.CleanupError, errors.New("fixture runtime deadlocked during owned cleanup"))
	}
	return outcome
}

type cleanupProcessReceived struct {
	event cleanupProcessEvent
	err   error
}

func readCleanupProcessEvents(resultRead *os.File) (<-chan cleanupProcessReceived, <-chan struct{}) {
	messages := make(chan cleanupProcessReceived, 3)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		decoder := json.NewDecoder(resultRead)
		// The private pipe protocol has exactly two events: ownership, completion.
		for range 2 {
			var event cleanupProcessEvent
			err := decoder.Decode(&event)
			messages <- cleanupProcessReceived{event: event, err: err}
			if err != nil {
				return
			}
		}
	}()
	return messages, readerDone
}

// Only ESRCH is positive absence evidence. EPERM and unexpected probe failures
// stay unresolved. Negative IDs observe a group; positive IDs observe a leaf.
func awaitCleanupProcessAbsence(ctx context.Context, id int) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := syscall.Kill(id, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		// A terminating process may remain observable until the OS reaps it.
		// Every non-ESRCH result stays unresolved until a later absence probe.
		select {
		case <-ctx.Done():
			return fmt.Errorf("absence unresolved (last probe: %v): %w", err, ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestCleanupAdmissionSupervisorProcess(t *testing.T) {
	encoded := os.Getenv("SEMSTREAMS_CLEANUP_SUPERVISOR_ARGS")
	if encoded == "" {
		t.Skip("subprocess fixture entry point")
	}
	var args []string
	if err := json.Unmarshal([]byte(encoded), &args); err != nil || len(args) == 0 {
		t.Fatalf("invalid supervisor command: %q: %v", encoded, err)
	}
	acknowledgementBudget, err := time.ParseDuration(os.Getenv("SEMSTREAMS_CLEANUP_ACK_BUDGET"))
	if err != nil || acknowledgementBudget <= 0 || acknowledgementBudget > cleanupProcessBudget {
		t.Fatal("invalid fixture acknowledgement budget")
	}
	pid := syscall.Getpid()
	if syscall.Getpgrp() != pid {
		t.Fatal("supervisor refuses an unowned process group")
	}
	control := os.NewFile(3, "cleanup-control")
	results := os.NewFile(4, "cleanup-results")
	// ExtraFiles clear CLOEXEC for this exec. Restore it before another exec so
	// commands cannot retain either private protocol descriptor.
	syscall.CloseOnExec(int(control.Fd()))
	syscall.CloseOnExec(int(results.Fd()))
	// ExtraFiles belong only to this supervisor, never to the command below.
	terminateOwnedGroup := func() {
		if syscall.Getpid() != pid || syscall.Getpgrp() != pid {
			os.Exit(91)
		}
		// Kill delivery is asynchronous. Keep a finite timer live instead of
		// returning to an idle runtime before the kernel delivers our SIGKILL.
		delivery := time.NewTimer(cleanupProcessBudget)
		defer delivery.Stop()
		if err := syscall.Kill(0, syscall.SIGKILL); err != nil {
			os.Exit(92)
		}
		<-delivery.C
		fmt.Fprintln(os.Stderr, cleanupSupervisorFailure+"owned SIGKILL delivery unresolved")
		os.Exit(93)
	}
	go func() {
		var request [1]byte
		_, _ = control.Read(request[:])
		terminateOwnedGroup()
	}()
	encoder := json.NewEncoder(results)
	if err := encoder.Encode(cleanupProcessEvent{Group: pid}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(args[0], args[1:]...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	// Do not propagate this entry-point selector into nested test helpers.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "SEMSTREAMS_CLEANUP_SUPERVISOR_ARGS=") {
			command.Env = append(command.Env, entry)
		}
	}
	event := cleanupProcessEvent{Completed: true}
	if err := command.Run(); err != nil {
		event.CommandError = err.Error()
		event.ExitCode = -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			event.ExitCode = exit.ExitCode()
		}
	}
	// Stay the live group anchor after command completion, including start failure.
	// This is a failed-acknowledgement bound, not a readiness sleep.
	acknowledgement := time.NewTimer(acknowledgementBudget)
	defer acknowledgement.Stop()
	if err := encoder.Encode(event); err != nil {
		terminateOwnedGroup()
	}
	<-acknowledgement.C
	fmt.Fprintln(os.Stderr, cleanupSupervisorFailure+"parent cleanup acknowledgement missing")
	terminateOwnedGroup()
}

type cleanupLeafReady struct {
	PID   int
	Group int
}

func TestCleanupAdmissionHeldTreeProcess(t *testing.T) {
	root := os.Getenv("SEMSTREAMS_CLEANUP_HELD_ROOT")
	if root == "" {
		t.Skip("subprocess fixture entry point")
	}
	if os.Getenv("SEMSTREAMS_CLEANUP_HELD_LEAF") == "1" {
		fmt.Fprintln(os.Stdout, "nested-leaf-inherited-output")
		body, err := json.Marshal(cleanupLeafReady{PID: syscall.Getpid(), Group: syscall.Getpgrp()})
		if err != nil {
			t.Fatal(err)
		}
		temporary := filepath.Join(root, "leaf-ready.tmp")
		if err := os.WriteFile(temporary, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(temporary, filepath.Join(root, "leaf-ready.json")); err != nil {
			t.Fatal(err)
		}
		// The parent holds the write end and never releases this gate. Cancellation
		// must end this process, not manufacture successful work completion.
		gate := os.NewFile(3, "unreleased-leaf-gate")
		var value [1]byte
		_, err = gate.Read(value[:])
		t.Fatalf("leaf gate unexpectedly released: %v", err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	leaf := exec.Command(binary, "-test.run", "^TestCleanupAdmissionHeldTreeProcess$")
	leaf.Env = append(os.Environ(), "SEMSTREAMS_CLEANUP_HELD_LEAF=1")
	leaf.ExtraFiles = []*os.File{read, write}
	leaf.Stdout, leaf.Stderr = os.Stdout, os.Stderr
	if err := leaf.Start(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SEMSTREAMS_CLEANUP_LINGER") == "1" {
		if _, err := awaitCleanupLeafReady(t.Context(), root); err != nil {
			t.Fatal(err)
		}
		// The leaf keeps the unreleased gate open itself so parent completion does
		// not turn EOF into apparent successful descendant cleanup.
		os.Exit(0)
	}
	if err := leaf.Wait(); err != nil {
		t.Fatal(err)
	}
}

func awaitCleanupLeafReady(ctx context.Context, root string) (cleanupLeafReady, error) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		body, err := os.ReadFile(filepath.Join(root, "leaf-ready.json"))
		if err == nil {
			var ready cleanupLeafReady
			err = json.Unmarshal(body, &ready)
			return ready, err
		}
		if !errors.Is(err, os.ErrNotExist) {
			return cleanupLeafReady{}, err
		}
		select {
		case <-ctx.Done():
			return cleanupLeafReady{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func TestCleanupAdmissionCancellationOwnership(t *testing.T) {
	taskBinary, err := exec.LookPath("task")
	if err != nil {
		t.Fatal(err)
	}
	root := cleanupAdmissionWorkspace(t, findRepoRoot(t), "clean")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writeCleanupAdmissionFile(t, filepath.Join(root, "bin", "go"), "#!/bin/sh\nexec \"$SEMSTREAMS_TEST_BINARY\" -test.run '^TestCleanupAdmissionHeldTreeProcess$'\n", 0o700)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	env := cleanupAdmissionEnvironment(map[string]string{
		"PATH":                         filepath.Join(root, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"SEMSTREAMS_TEST_BINARY":       binary,
		"SEMSTREAMS_CLEANUP_HELD_ROOT": root,
	})
	finished := make(chan cleanupProcessOutcome, 1)
	go func() { finished <- runOwnedCleanupCommand(ctx, root, []string{taskBinary, "--silent", "test"}, env) }()
	ready, readyErr := awaitCleanupLeafReady(ctx, root)
	cancel()
	result := <-finished
	if readyErr != nil {
		t.Fatalf("nested leaf not ready: %v; result=%+v", readyErr, result)
	}
	if ready.Group != result.Group {
		t.Errorf("leaf group %d differs from owned group %d", ready.Group, result.Group)
	}
	if !errors.Is(result.CommandError, context.Canceled) || result.CleanupError != nil {
		t.Fatalf("cancellation did not join owned tree: command=%v cleanup=%v\n%s", result.CommandError, result.CleanupError, result.Output)
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
	defer cleanupCancel()
	if err := awaitCleanupProcessAbsence(cleanupCtx, ready.PID); err != nil {
		t.Errorf("leaf remains unresolved: %v", err)
	}
	if !bytes.Contains(result.Output, []byte("nested-leaf-inherited-output")) {
		t.Errorf("lost inherited output: %s", result.Output)
	}
}

func TestCleanupAdmissionSupervisorFailurePaths(t *testing.T) {
	t.Run("normal completion with lingering descendant", func(t *testing.T) {
		root := t.TempDir()
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		env := cleanupAdmissionEnvironment(map[string]string{
			"SEMSTREAMS_CLEANUP_HELD_ROOT": root,
			"SEMSTREAMS_CLEANUP_LINGER":    "1",
		})
		result := runOwnedCleanupCommand(ctx, root, []string{binary, "-test.run", "^TestCleanupAdmissionHeldTreeProcess$"}, env)
		if result.CommandError != nil || result.CleanupError != nil {
			t.Fatalf("result=%+v", result)
		}
		ready, err := awaitCleanupLeafReady(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		if ready.Group != result.Group {
			t.Fatalf("leaf group %d differs from owned group %d", ready.Group, result.Group)
		}
		if err := awaitCleanupProcessAbsence(ctx, ready.PID); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(result.Output, []byte("nested-leaf-inherited-output")) {
			t.Fatalf("lost inherited output: %s", result.Output)
		}
	})

	t.Run("command start failure", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		result := runOwnedCleanupCommand(ctx, t.TempDir(), []string{"/definitely-missing-cleanup-command"}, os.Environ())
		if result.CommandError == nil || result.CleanupError != nil {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("cancel before readiness", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		result := runOwnedCleanupCommand(ctx, t.TempDir(), []string{"/definitely-missing-cleanup-command"}, os.Environ())
		if !errors.Is(result.CommandError, context.Canceled) || result.CleanupError != nil {
			t.Fatalf("result=%+v", result)
		}
	})
}

func cleanupDescriptorIdentity(fd int) string {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d", stat.Dev, stat.Ino, stat.Mode)
}

func TestCleanupAdmissionDescriptorProbeProcess(t *testing.T) {
	if os.Getenv("SEMSTREAMS_CLEANUP_DESCRIPTOR_PROBE") != "1" {
		t.Skip("subprocess fixture entry point")
	}
	for fd, key := range map[int]string{3: "SEMSTREAMS_CLEANUP_CONTROL_ID", 4: "SEMSTREAMS_CLEANUP_RESULT_ID"} {
		expected := os.Getenv(key)
		if expected == "" {
			t.Fatal("missing parent descriptor identity")
		}
		if cleanupDescriptorIdentity(fd) == expected {
			t.Errorf("inherited private protocol descriptor %d", fd)
		}
	}
}

func TestCleanupAdmissionPrivateDescriptors(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	env := cleanupAdmissionEnvironment(map[string]string{"SEMSTREAMS_CLEANUP_DESCRIPTOR_PROBE": "1"})
	result := runOwnedCleanupCommand(ctx, t.TempDir(), []string{binary, "-test.run", "^TestCleanupAdmissionDescriptorProbeProcess$"}, env)
	if result.CommandError != nil || result.CleanupError != nil {
		t.Fatalf("private descriptor containment: command=%v cleanup=%v\n%s", result.CommandError, result.CleanupError, result.Output)
	}
}

func TestCleanupAdmissionMissingAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := runOwnedCleanupCommand(ctx, t.TempDir(), []string{"true"}, os.Environ(), cleanupProcessProbe{
		acknowledgementBudget: 100 * time.Millisecond, holdAcknowledgementUntilExit: true,
	})
	if result.CommandError != nil {
		t.Fatalf("command failed before acknowledgement boundary: %v\n%s", result.CommandError, result.Output)
	}
	if result.CleanupError == nil {
		t.Fatalf("missing acknowledgement passed cleanup despite supervisor failure:\n%s", result.Output)
	}
	if !bytes.Contains(result.Output, []byte(cleanupSupervisorFailure+"parent cleanup acknowledgement missing")) {
		t.Fatalf("required failure path did not execute: %s", result.Output)
	}
	if err := awaitCleanupProcessAbsence(ctx, -result.Group); err != nil {
		t.Fatalf("failure did not contain group: %v", err)
	}
}

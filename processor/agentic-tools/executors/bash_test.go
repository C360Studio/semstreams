package executors

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/processor/agentic-tools/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBashExecutor_ListTools(t *testing.T) {
	e := NewBashExecutor("/tmp", "")
	tools := e.ListTools()
	require.Len(t, tools, 1)
	assert.Equal(t, "bash", tools[0].Name)
	assert.Contains(t, tools[0].Description, "shell command")
}

func TestBashExecutor_LocalExec(t *testing.T) {
	e := NewBashExecutor("/tmp", "")

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-1",
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, "call-1", result.CallID)
	assert.Contains(t, result.Content, "hello")
	assert.Empty(t, result.Error)
}

func TestBashExecutor_MissingCommand(t *testing.T) {
	e := NewBashExecutor("/tmp", "")

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-2",
		Name:      "bash",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	assert.Equal(t, "command argument is required", result.Error)
}

func TestBashExecutor_NonZeroExit(t *testing.T) {
	e := NewBashExecutor("/tmp", "")

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-3",
		Name:      "bash",
		Arguments: map[string]any{"command": "exit 42"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Error, "exit code 42")
}

func TestBashExecutor_StderrCaptured(t *testing.T) {
	e := NewBashExecutor("/tmp", "")

	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-4",
		Name:      "bash",
		Arguments: map[string]any{"command": "echo stdout-text && echo stderr-text >&2"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Content, "stdout-text")
	assert.Contains(t, result.Content, "stderr-text")
}

func TestBashExecutor_RemoteMode(t *testing.T) {
	// When SANDBOX_URL is set, the runner client (HTTP client to the
	// remote sandbox container) is created.
	e := NewBashExecutor("/tmp", "http://sandbox:8080")
	assert.NotNil(t, e.runner)

	// Without SANDBOX_URL, runs locally.
	e2 := NewBashExecutor("/tmp", "")
	assert.Nil(t, e2.runner)
}

func TestFilterEnv_StripsSecrets(t *testing.T) {
	// This tests that filterEnv doesn't include known-secret patterns.
	// We can't easily inject env vars in unit tests, but we can verify
	// the function doesn't panic and returns a non-nil result.
	env := filterEnv()
	assert.NotNil(t, env)

	// Verify no sensitive patterns leaked through
	for _, e := range env {
		upper := e
		assert.NotContains(t, upper, "ANTHROPIC_API_KEY=")
		assert.NotContains(t, upper, "OPENAI_API_KEY=")
	}
}

func TestPathMatchesAny(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{"empty patterns matches every non-empty path", "src/main.go", nil, true},
		{"exact match", "configs/foo.json", []string{"configs/foo.json"}, true},
		{"directory pattern with trailing slash", "src/main.go", []string{"src/"}, true},
		{"directory pattern without trailing slash", "src/main.go", []string{"src"}, true},
		{"sibling outside directory does not match", "src.txt", []string{"src"}, false},
		{"unrelated path does not match", "tests/x.go", []string{"src/", "configs/foo.json"}, false},
		{"nested deeper still matches", "src/a/b/c.go", []string{"src"}, true},
		{"empty pattern entry skipped", "src/x.go", []string{""}, false},
		{"trailing-slash-only pattern entry skipped", "src/x.go", []string{"/"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathMatchesAny(tt.path, tt.patterns); got != tt.want {
				t.Errorf("pathMatchesAny(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestFormatVerifyCleanViolation(t *testing.T) {
	// Records are NUL-terminated under `git status -z --porcelain`. Tests
	// hand-build the wire format so the parser is exercised against the
	// exact byte sequence git emits — including the rename two-record
	// pattern (destination first, then source) and a path with a space,
	// which the legacy `--porcelain` (no -z) form would have quoted.
	tests := []struct {
		name      string
		porcelain string
		patterns  []string
		wantEmpty bool
		wantSub   string
	}{
		{
			name:      "clean tree, no patterns",
			porcelain: "",
			wantEmpty: true,
		},
		{
			name:      "dirty path inside read_only_paths fails",
			porcelain: " M src/main.go\x00",
			patterns:  []string{"src/"},
			wantSub:   "src/main.go",
		},
		{
			name:      "dirty path outside read_only_paths passes",
			porcelain: " M tests/foo_test.go\x00",
			patterns:  []string{"src/"},
			wantEmpty: true,
		},
		{
			name:      "dirty path with empty read_only_paths is general clean-tree gate",
			porcelain: "?? new_file.go\x00",
			wantSub:   "new_file.go",
		},
		{
			name:      "rename two-record pair scores destination only",
			porcelain: "R  new.go\x00old.go\x00",
			patterns:  []string{"new.go"},
			wantSub:   "new.go",
		},
		{
			name: "rename source record never scored even when matching read_only_paths",
			// Destination "tests/new.go" is outside src/; source
			// "src/old.go" would match if scored — it must NOT be.
			porcelain: "R  tests/new.go\x00src/old.go\x00",
			patterns:  []string{"src/"},
			wantEmpty: true,
		},
		{
			name: "path with space survives -z without quoting",
			// Under default `git status --porcelain`, this path emits as
			// `?? "src/has space.go"` (literal quotes). Under `-z` it's
			// raw. Pin the raw form to lock the parser to the wire shape
			// we actually invoke.
			porcelain: "?? src/has space.go\x00",
			patterns:  []string{"src/"},
			wantSub:   "src/has space.go",
		},
		{
			name:      "non-ASCII path survives -z (default core.quotePath would have quoted it)",
			porcelain: "?? src/café.go\x00",
			patterns:  []string{"src/"},
			wantSub:   "src/café.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVerifyCleanViolation(tt.porcelain, tt.patterns)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty (clean), got: %q", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("expected violation containing %q, got empty", tt.wantSub)
			}
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("violation = %q, want substring %q", got, tt.wantSub)
			}
		})
	}
}

func TestStringSliceArg(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want []string
	}{
		{"nil", nil, nil},
		{"non-array", "src/", nil},
		{"array of strings", []any{"a", "b"}, []string{"a", "b"}},
		{"empty entries skipped", []any{"a", "", "b"}, []string{"a", "b"}},
		{"non-string entries skipped", []any{"a", 42, "b"}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringSliceArg(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// initTempGitRepo sets up a tempdir with a git repo containing one
// committed file, then optionally dirties paths for verify_clean tests.
func initTempGitRepo(t *testing.T, dirtyPaths ...string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		// Disable the user's global hooks/signing so the test runs the same
		// in any contributor's environment.
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "seed.txt")
	run("git", "commit", "-q", "-m", "seed")
	for _, p := range dirtyPaths {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestBashExecutor_VerifyClean_LocalCleanTree confirms verify_clean is a
// no-op on a clean tree — the actual command runs and its output is
// returned.
func TestBashExecutor_VerifyClean_LocalCleanTree(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: "bash",
		Arguments: map[string]any{
			"command":      "echo ran",
			"verify_clean": true,
		},
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error, "clean tree must let command run")
	assert.Contains(t, res.Content, "ran")
}

// TestBashExecutor_VerifyClean_LocalDirtyMatchingPath proves the
// precondition fires and the real command does NOT run when a
// read_only_paths-matching path is dirty. semspec hits this when an
// agent attempts a command that would clobber off-limits files.
func TestBashExecutor_VerifyClean_LocalDirtyMatchingPath(t *testing.T) {
	dir := initTempGitRepo(t, "src/touched.go")
	e := NewBashExecutor(dir, "")

	// Sentinel file the real command would create — proves it didn't run.
	sentinel := filepath.Join(dir, "real-command-ran")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c2",
		Name: "bash",
		Arguments: map[string]any{
			"command":         "touch " + sentinel,
			"read_only_paths": []any{"src/"},
			"verify_clean":    true,
		},
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "verify_clean", "expected precondition violation")
	assert.Contains(t, res.Error, "src/touched.go")
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Errorf("real command should NOT have run; sentinel exists or stat errored: %v", err)
	}
}

// TestBashExecutor_VerifyClean_LocalDirtyNonMatchingPath proves the
// precondition lets the command through when the dirty path is OUTSIDE
// the declared read_only_paths.
func TestBashExecutor_VerifyClean_LocalDirtyNonMatchingPath(t *testing.T) {
	dir := initTempGitRepo(t, "tests/touched.go")
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c3",
		Name: "bash",
		Arguments: map[string]any{
			"command":         "echo ran",
			"read_only_paths": []any{"src/"},
			"verify_clean":    true,
		},
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error, "dirty path outside read_only_paths must NOT block")
	assert.Contains(t, res.Content, "ran")
}

// TestBashExecutor_VerifyClean_LocalEmptyPathsActsAsCleanTreeGate
// confirms the degenerate case: verify_clean=true with no read_only_paths
// refuses on ANY dirty file. Useful for "I want to start from a totally
// clean tree" agent flows.
func TestBashExecutor_VerifyClean_LocalEmptyPathsActsAsCleanTreeGate(t *testing.T) {
	dir := initTempGitRepo(t, "anywhere/foo.txt")
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c4",
		Name: "bash",
		Arguments: map[string]any{
			"command":      "echo ran",
			"verify_clean": true,
		},
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "verify_clean")
	assert.Contains(t, res.Error, "anywhere/foo.txt")
}

// fakeSandboxServer is a tiny httptest fake that lets the bash executor's
// sandbox-mode path be exercised without standing up the real sandbox.
// Each /exec call captures the requested command (so the test can
// distinguish the precondition from the real command) and replies with
// a canned ExecResult per the provided handler.
type fakeSandboxCall struct {
	command string
}

func newFakeSandboxServer(t *testing.T, handler func(call fakeSandboxCall) (stdout string, exitCode int)) (*httptest.Server, *[]fakeSandboxCall) {
	t.Helper()
	calls := &[]fakeSandboxCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			TaskID    string `json:"task_id"`
			Command   string `json:"command"`
			TimeoutMs int    `json:"timeout_ms"`
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		*calls = append(*calls, fakeSandboxCall{command: body.Command})
		stdout, exitCode := handler(fakeSandboxCall{command: body.Command})
		resp := map[string]any{
			"stdout":    stdout,
			"stderr":    "",
			"exit_code": exitCode,
			"timed_out": false,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	return server, calls
}

// TestBashExecutor_VerifyClean_SandboxBlocksOnDirty exercises the
// sandbox-mode precondition path: the first /exec call is the
// `git status -z` precondition (canned with porcelain output naming a
// dirty path inside read_only_paths); the executor must refuse the real
// command before issuing a second /exec call.
func TestBashExecutor_VerifyClean_SandboxBlocksOnDirty(t *testing.T) {
	server, calls := newFakeSandboxServer(t, func(call fakeSandboxCall) (string, int) {
		if strings.HasPrefix(call.command, "git status") {
			return " M src/main.go\x00", 0
		}
		return "real-command-output", 0
	})

	e := NewBashExecutor("/unused", server.URL)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "sb-1",
		Name: "bash",
		Arguments: map[string]any{
			"command":         "echo would-clobber",
			"read_only_paths": []any{"src/"},
			"verify_clean":    true,
		},
		Metadata: map[string]any{"task_id": "task-x"},
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "verify_clean")
	assert.Contains(t, res.Error, "src/main.go")

	// Exactly ONE /exec call was made — the precondition. The real
	// command must NOT have been issued.
	require.Len(t, *calls, 1, "real command should not have been issued; calls: %+v", *calls)
	assert.Contains(t, (*calls)[0].command, "git status")
}

// recordingRunner is a Runner that captures every Exec call and returns
// a canned ExecResult. Lets tests prove (1) WithRunner replaces the
// URL-based default and (2) BashExecutor routes through the supplied
// Runner without touching the network. Mirrors the role of
// newFakeSandboxServer for the URL-based path, but without HTTP — the
// whole point of the Runner interface is per-call routing without an
// HTTP middlebox.
type recordingRunner struct {
	mu     sync.Mutex
	calls  []recordedExec
	result *runner.ExecResult
	err    error
}

type recordedExec struct {
	taskID    string
	command   string
	timeoutMs int
}

func (r *recordingRunner) Exec(_ context.Context, taskID, command string, timeoutMs int) (*runner.ExecResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, recordedExec{taskID: taskID, command: command, timeoutMs: timeoutMs})
	r.mu.Unlock()
	return r.result, r.err
}

// TestBashExecutor_WithRunner_RoutesThroughCustomRunner is the primary
// boundary test for the Runner interface extension point. Product shells
// (e.g., semteams's per-tenant devcontainer router) supply a custom
// Runner via WithRunner and expect every remote dispatch to go through
// it — no fallback to local exec, no HTTP, no URL-based runner.
func TestBashExecutor_WithRunner_RoutesThroughCustomRunner(t *testing.T) {
	rec := &recordingRunner{
		result: &runner.ExecResult{Stdout: "from-custom-runner", ExitCode: 0},
	}
	e := NewBashExecutor("", "", WithRunner(rec))

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "cr-1",
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hi"},
		Metadata:  map[string]any{"task_id": "task-abc"},
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error)
	assert.Equal(t, "from-custom-runner", res.Content)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.calls, 1)
	assert.Equal(t, "task-abc", rec.calls[0].taskID)
	assert.Equal(t, "echo hi", rec.calls[0].command)
	assert.Greater(t, rec.calls[0].timeoutMs, 0, "timeout must be propagated as ms")
}

// TestBashExecutor_WithRunner_OverridesURL pins the mutual-exclusion
// contract: when both a sandboxURL and WithRunner are supplied, WithRunner
// wins. Documented behavior — callers are expected to use one or the
// other, but the constructor must not silently revert to the URL-based
// runner if both arrive. The HTTP handler fails the test the instant
// the contract breaks (matches the newFakeSandboxServer pattern above)
// so the diagnosis is "URL runner was invoked", not "custom runner
// returned the wrong content."
func TestBashExecutor_WithRunner_OverridesURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("WithRunner did not override; URL runner was invoked at %s", r.URL.Path)
	}))
	t.Cleanup(server.Close)

	rec := &recordingRunner{result: &runner.ExecResult{Stdout: "custom-wins", ExitCode: 0}}
	e := NewBashExecutor("/unused", server.URL, WithRunner(rec))

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "cr-2",
		Name:      "bash",
		Arguments: map[string]any{"command": "echo override"},
	})
	require.NoError(t, err)
	assert.Equal(t, "custom-wins", res.Content)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.calls, 1)
}

// TestBashExecutor_WithRunner_NilIsNoop confirms that passing a nil
// Runner via WithRunner does NOT clobber the URL-based default. Keeps
// callers safe from accidentally disabling the URL-based runner with
// an uninitialized custom impl.
func TestBashExecutor_WithRunner_NilIsNoop(t *testing.T) {
	e := NewBashExecutor("/tmp", "http://sandbox:8080", WithRunner(nil))
	assert.NotNil(t, e.runner, "nil Runner must not clear the URL-constructed default")
}

// TestBashExecutor_WithRunner_PropagatesError surfaces transport-layer
// errors through the same path as the URL-based runner — exec failure
// becomes a ToolResult.Error, not a returned err.
func TestBashExecutor_WithRunner_PropagatesError(t *testing.T) {
	rec := &recordingRunner{err: errors.New("router unavailable")}
	e := NewBashExecutor("", "", WithRunner(rec))

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "cr-3",
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hi"},
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "remote exec failed")
	assert.Contains(t, res.Error, "router unavailable")
}

// TestBashExecutor_VerifyClean_SandboxAllowsClean covers the success
// path: clean precondition then real command runs.
func TestBashExecutor_VerifyClean_SandboxAllowsClean(t *testing.T) {
	server, calls := newFakeSandboxServer(t, func(call fakeSandboxCall) (string, int) {
		if strings.HasPrefix(call.command, "git status") {
			return "", 0 // clean tree
		}
		return "real-command-output", 0
	})

	e := NewBashExecutor("/unused", server.URL)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "sb-2",
		Name: "bash",
		Arguments: map[string]any{
			"command":      "echo ran",
			"verify_clean": true,
		},
		Metadata: map[string]any{"task_id": "task-x"},
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error)
	assert.Equal(t, "real-command-output", res.Content)
	require.Len(t, *calls, 2, "expected precondition + real command, got %+v", *calls)
	assert.Contains(t, (*calls)[0].command, "git status")
	assert.Equal(t, "echo ran", (*calls)[1].command)
}

// --- ADR-067 read-only execution policy (gh#443) -------------------------------

// readOnlyMeta builds the task-scoped metadata that dispatch stamps for a
// read_only inspect role: the policy plus optional in-worktree scratch
// exemptions. The model never sets these — they ride Metadata, not Arguments.
func readOnlyMeta(scratch ...string) map[string]any {
	m := map[string]any{agentic.MetadataKeyFilesystemPolicy: agentic.FilesystemPolicyReadOnly}
	if len(scratch) > 0 {
		m[agentic.MetadataKeyScratchPaths] = scratch
	}
	return m
}

// TestBashExecutor_ReadOnly_CleanCommandPasses: an inspection command that
// leaves the worktree and HEAD unchanged runs normally under read_only.
func TestBashExecutor_ReadOnly_CleanCommandPasses(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "ro-clean", Name: "bash",
		Arguments: map[string]any{"command": "cat seed.txt && echo inspected"},
		Metadata:  readOnlyMeta(),
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error, "read-only inspection must pass")
	assert.Contains(t, res.Content, "inspected")
}

// TestBashExecutor_ReadOnly_WorktreeWriteViolation: creating a product-worktree
// file is a typed violation naming the path — the core gh#443 guarantee the
// pre-only verify_clean could not provide.
func TestBashExecutor_ReadOnly_WorktreeWriteViolation(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "ro-write", Name: "bash",
		Arguments: map[string]any{"command": "echo generated > product.go"},
		Metadata:  readOnlyMeta(),
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "mutated protected worktree paths")
	assert.Contains(t, res.Error, "product.go")
}

// TestBashExecutor_ReadOnly_DeleteTrackedViolation: deleting a tracked file is
// caught (git reports ` D path`).
func TestBashExecutor_ReadOnly_DeleteTrackedViolation(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "ro-del", Name: "bash",
		Arguments: map[string]any{"command": "rm seed.txt"},
		Metadata:  readOnlyMeta(),
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "mutated protected worktree paths")
	assert.Contains(t, res.Error, "seed.txt")
}

// TestBashExecutor_ReadOnly_ScratchWriteAllowed: a write UNDER a declared
// in-worktree scratch path is exempt; the command passes.
func TestBashExecutor_ReadOnly_ScratchWriteAllowed(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "ro-scratch", Name: "bash",
		Arguments: map[string]any{"command": "mkdir -p .probe && echo x > .probe/out.txt && echo done"},
		Metadata:  readOnlyMeta(".probe"),
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error, "write under declared scratch must be allowed; got %q", res.Error)
	assert.Contains(t, res.Content, "done")
}

// TestBashExecutor_ReadOnly_TmpProbeAllowed: a write to /tmp (out-of-worktree)
// is invisible to the worktree proof — probes there are always allowed, with no
// scratch declaration needed.
func TestBashExecutor_ReadOnly_TmpProbeAllowed(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")
	probe := filepath.Join(t.TempDir(), "probe.txt") // outside the worktree

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "ro-tmp", Name: "bash",
		Arguments: map[string]any{"command": "echo probing > " + probe + " && echo probed"},
		Metadata:  readOnlyMeta(),
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error, "out-of-worktree probe must be allowed; got %q", res.Error)
	assert.Contains(t, res.Content, "probed")
}

// TestBashExecutor_ReadOnly_HeadMoveViolation: a git-state mutation that leaves
// the worktree clean (a commit) is caught by HEAD pinning — the hole a
// status-only proof would miss.
func TestBashExecutor_ReadOnly_HeadMoveViolation(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "ro-head", Name: "bash",
		Arguments: map[string]any{
			"command": "git -c user.name=t -c user.email=t@e commit -q --allow-empty -m sneak",
		},
		Metadata: readOnlyMeta(),
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "moved git HEAD")
}

// TestBashExecutor_ReadOnly_PreDirtyRefuses: an already-dirty protected tree is
// refused before the command runs (an inspect role must start clean).
func TestBashExecutor_ReadOnly_PreDirtyRefuses(t *testing.T) {
	dir := initTempGitRepo(t, "already/dirty.go")
	e := NewBashExecutor(dir, "")
	sentinel := filepath.Join(dir, "command-ran")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "ro-predirty", Name: "bash",
		Arguments: map[string]any{"command": "touch " + sentinel},
		Metadata:  readOnlyMeta(),
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "not clean before")
	assert.Contains(t, res.Error, "already/dirty.go")
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Errorf("command must NOT run on a pre-dirty tree; sentinel exists or stat errored: %v", statErr)
	}
}

// TestBashExecutor_ReadOnly_DefaultPolicyUnchanged: with no policy (or
// workspace_write), a worktree write is NOT a violation — back-compat for every
// non-inspect role.
func TestBashExecutor_ReadOnly_DefaultPolicyUnchanged(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "rw-write", Name: "bash",
		Arguments: map[string]any{"command": "echo ok > wrote.go && echo done"},
		// No filesystem_policy metadata → default workspace_write.
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error, "default policy must not guard worktree writes")
	assert.Contains(t, res.Content, "done")
}

// scriptedRunner routes each Exec to a command→result function, letting a test
// drive the remote read_only path (pre/post captures + the command) without a
// server. The mutated flag flips the post-command git status to dirty.
type scriptedRunner struct {
	fn func(command string) *runner.ExecResult
}

func (r scriptedRunner) Exec(_ context.Context, _ string, command string, _ int) (*runner.ExecResult, error) {
	return r.fn(command), nil
}

// TestBashExecutor_ReadOnly_RemoteWriteViolation: the remote (runner) path
// enforces read_only too — a post-command dirty status yields the typed
// violation.
func TestBashExecutor_ReadOnly_RemoteWriteViolation(t *testing.T) {
	mutated := false
	run := scriptedRunner{fn: func(cmd string) *runner.ExecResult {
		switch {
		case strings.Contains(cmd, "git status"):
			if mutated {
				return &runner.ExecResult{Stdout: "?? out.txt\x00"} // post: dirty
			}
			return &runner.ExecResult{Stdout: ""} // pre: clean
		case strings.Contains(cmd, "git rev-parse"):
			return &runner.ExecResult{Stdout: "abc123\n"} // HEAD stable
		default:
			mutated = true
			return &runner.ExecResult{Stdout: "did work"}
		}
	}}
	e := NewBashExecutor("", "", WithRunner(run))

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "ro-remote", Name: "bash",
		Arguments: map[string]any{"command": "echo x > out.txt"},
		Metadata:  readOnlyMeta(),
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "mutated protected worktree paths")
	assert.Contains(t, res.Error, "out.txt")
}

// TestBashExecutor_ReadOnly_UnrecognizedPolicyFailsClosed: an unrecognized
// filesystem_policy value (a product typo) must REFUSE, not silently run
// unsandboxed — fail-closed on a security control (semstreams-reviewer MEDIUM).
func TestBashExecutor_ReadOnly_UnrecognizedPolicyFailsClosed(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")
	sentinel := filepath.Join(dir, "ran")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "ro-typo", Name: "bash",
		Arguments: map[string]any{"command": "touch " + sentinel},
		Metadata:  map[string]any{agentic.MetadataKeyFilesystemPolicy: "readonly"}, // typo
	})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "not recognized")
	assert.Contains(t, res.Error, "readonly")
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Errorf("command must NOT run under an unrecognized policy; sentinel exists: %v", statErr)
	}
}

// TestBashExecutor_HostWritePolicyRuns: host_write is a KNOWN (permissive) enum
// value — it must not be refused as unrecognized, and does not guard writes.
func TestBashExecutor_HostWritePolicyRuns(t *testing.T) {
	dir := initTempGitRepo(t)
	e := NewBashExecutor(dir, "")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "hw", Name: "bash",
		Arguments: map[string]any{"command": "echo x > wrote.go && echo done"},
		Metadata:  map[string]any{agentic.MetadataKeyFilesystemPolicy: agentic.FilesystemPolicyHostWrite},
	})
	require.NoError(t, err)
	assert.Empty(t, res.Error, "host_write is permissive; must not refuse or guard")
	assert.Contains(t, res.Content, "done")
}

// Package executors provides tool executor implementations for the agentic system.
//
// BashExecutor runs shell commands. When a remote runner is configured (SANDBOX_URL),
// commands execute inside the remote sandbox container via the runner client.
// Otherwise, they run locally via os/exec with sensitive environment variables
// stripped.
//
// The remote dispatch path is fronted by the Runner interface so downstream
// products can supply custom routing logic (per-tenant devcontainers,
// load-balancing across sandbox replicas, attestation-aware routing, A/B
// testing) without wrapping BashExecutor. *runner.Client is the default
// implementation; product shells inject alternates via WithRunner. This
// is the per-call extension point the forthcoming capability-aware
// execution-environment substrate (ADR-052, see
// docs/proposals/sandbox-substrate.md) plugs into.
//
// Ported from semspec/tools/bash.
package executors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/processor/agentic-tools/runner"
)

const (
	bashMaxOutputBytes = 100 * 1024 // 100KB output cap
	bashDefaultTimeout = 120 * time.Second
)

// Runner is the per-call interface BashExecutor uses to dispatch commands
// to a remote sandbox server. *runner.Client satisfies it (default,
// constructed from sandboxURL). Product shells supply custom
// implementations via WithRunner for per-call routing — e.g.,
// attestation-aware routing to per-tenant devcontainers, load-balancing
// across replicas, or A/B testing alternate sandbox backends. This is
// the framework extension point the forthcoming capability-aware
// execution-environment substrate (ADR-052) builds on; the interface
// itself is product-agnostic and carries no devcontainer, attestation,
// or tenancy concepts.
type Runner interface {
	Exec(ctx context.Context, taskID, command string, timeoutMs int) (*runner.ExecResult, error)
}

// Compile-time assertion that *runner.Client satisfies Runner. Any future
// signature drift on *runner.Client.Exec surfaces here as a compile error
// in the package that owns the contract, not as a runtime panic during
// remote dispatch.
var _ Runner = (*runner.Client)(nil)

// BashExecutor runs shell commands locally or via a Runner (which routes
// to a remote sandbox container — see SANDBOX_URL for the URL-based
// default, WithRunner for custom routing).
type BashExecutor struct {
	workDir string
	runner  Runner
	timeout time.Duration
}

// BashOption configures a BashExecutor.
type BashOption func(*BashExecutor)

// WithBashTimeout overrides the default command timeout (120s).
func WithBashTimeout(d time.Duration) BashOption {
	return func(e *BashExecutor) { e.timeout = d }
}

// WithRunner overrides the runner constructed from sandboxURL. When
// provided, sandboxURL is ignored; the supplied Runner handles all
// remote dispatch. Mutually exclusive with the URL-based constructor;
// either call NewBashExecutor("", "", WithRunner(custom)) or pass a
// URL and omit the option. Passing the untyped nil is treated as
// "no override" (URL-based default applies if a URL was given). A
// typed-nil wrapped in a Runner interface value — e.g., `var r Runner
// = (*runner.Client)(nil); WithRunner(r)` — is NOT detected and will
// panic on first Exec; construct your Runner before passing it in.
func WithRunner(r Runner) BashOption {
	return func(e *BashExecutor) {
		if r != nil {
			e.runner = r
		}
	}
}

// NewBashExecutor creates a bash executor. If sandboxURL is non-empty, commands
// are routed to the sandbox container via the default *runner.Client. Pass
// WithRunner to inject a custom Runner instead; when supplied, it overrides
// the URL-based default.
func NewBashExecutor(workDir, sandboxURL string, opts ...BashOption) *BashExecutor {
	e := &BashExecutor{
		workDir: workDir,
	}
	if sandboxURL != "" {
		e.runner = runner.NewClient(sandboxURL)
	}
	// Options apply after URL-based init so WithRunner can override.
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// NewBashExecutorFromEnv creates a bash executor using environment variables.
// SANDBOX_URL enables sandbox mode. Work directory defaults to cwd.
func NewBashExecutorFromEnv() *BashExecutor {
	workDir, _ := os.Getwd()
	return NewBashExecutor(workDir, os.Getenv("SANDBOX_URL"))
}

func (e *BashExecutor) effectiveTimeout() time.Duration {
	if e.timeout > 0 {
		return e.timeout
	}
	return bashDefaultTimeout
}

// ListTools returns the bash tool definition.
//
// Observability note (gh#146): when a bash command performs an external fetch
// (curl/wget/httpie), the resulting trajectory step carries a first-class
// url_fetched attribute derived from the command string (see
// agentic.BashStepURLs / ExtractFetchedURLs), so citation/audit/governance
// dashboards can filter and count external reach independently of generic shell
// activity.
func (e *BashExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "bash",
			Description: "Run a shell command. Use for file operations, git, builds, tests, and any other shell task.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "Shell command to execute",
					},
					"read_only_paths": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Paths (files or directories, relative to the worktree root) that should not be modified. Used by verify_clean to filter the dirty-tree check; an entry like \"src/\" matches anything under src/.",
					},
					"verify_clean": map[string]any{
						"type":        "boolean",
						"description": "If true, run `git status -z --porcelain --untracked-files=all` before the real command and refuse to run if any dirty path matches read_only_paths (or any dirty path at all when read_only_paths is empty). Use to keep a command sequence from clobbering off-limits files.",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

// Execute runs a shell command and returns the output.
func (e *BashExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	command, ok := call.Arguments["command"].(string)
	if !ok || command == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "command argument is required",
		}, nil
	}

	readOnlyPaths := stringSliceArg(call.Arguments["read_only_paths"])
	verifyClean, _ := call.Arguments["verify_clean"].(bool)

	// ADR-067 read-only execution policy: a task-scoped, model-uncontrollable
	// filesystem write-scope stamped onto call.Metadata by dispatch. Under
	// read_only the command must leave the worktree AND git HEAD unchanged
	// (modulo declared scratch paths); a mutation is a typed violation. Distinct
	// from the per-call verify_clean argument (which the model sets and which is
	// pre-only) — this rides Metadata and is pre+post.
	roPolicy, scratch := agentic.FilesystemPolicyFromMetadata(call.Metadata)
	readOnly := agentic.IsReadOnlyPolicy(roPolicy)
	taskID := bashTaskID(call)

	if e.runner != nil {
		if verifyClean {
			if violation, err := e.verifyCleanRemote(ctx, taskID, readOnlyPaths); err != nil {
				return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("verify_clean precondition failed: %v", err)}, nil
			} else if violation != "" {
				return agentic.ToolResult{CallID: call.ID, Error: violation}, nil
			}
		}
		if readOnly {
			return e.execReadOnly(call.ID,
				func() (worktreeSnapshot, error) { return e.captureRemote(ctx, taskID, scratch) },
				func() (agentic.ToolResult, error) { return e.execRemote(ctx, call.ID, command, taskID) })
		}
		return e.execRemote(ctx, call.ID, command, taskID)
	}

	if verifyClean {
		if violation, err := e.verifyCleanLocal(ctx, readOnlyPaths); err != nil {
			return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("verify_clean precondition failed: %v", err)}, nil
		} else if violation != "" {
			return agentic.ToolResult{CallID: call.ID, Error: violation}, nil
		}
	}
	if readOnly {
		return e.execReadOnly(call.ID,
			func() (worktreeSnapshot, error) { return e.captureLocal(ctx, scratch) },
			func() (agentic.ToolResult, error) { return e.execLocal(ctx, call.ID, command) })
	}
	return e.execLocal(ctx, call.ID, command)
}

// bashTaskID resolves the sandbox worktree key from a call's metadata:
// task_id, then loop_id, else "default". Stamped by dispatch, not the model.
func bashTaskID(call agentic.ToolCall) string {
	taskID := "default"
	if m := call.Metadata; m != nil {
		if tid, ok := m["task_id"].(string); ok && tid != "" {
			taskID = tid
		} else if lid, ok := m["loop_id"].(string); ok && lid != "" {
			taskID = lid
		}
	}
	return taskID
}

// stringSliceArg coerces a tool-call argument into []string. Tool arguments
// arrive as map[string]any after JSON decode; arrays land as []any with
// element types per the JSON spec. Anything that isn't a list of strings
// returns nil — callers treat that as "no paths specified," which for
// verify_clean's filter means "any dirty file is a violation."
func stringSliceArg(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// verifyCleanRemote runs `git status -z --porcelain --untracked-files=all` inside
// the remote runner's sandbox container and returns a non-empty violation
// message if any dirty path matches readOnlyPaths (or, when readOnlyPaths is
// empty, if any path is dirty). Returns ("", nil) when the precondition
// passes; (msg, nil) when it fails; ("", err) when the precondition itself
// errored (network, non-zero exit from git status, malformed output).
func (e *BashExecutor) verifyCleanRemote(ctx context.Context, taskID string, readOnlyPaths []string) (string, error) {
	res, err := e.runner.Exec(ctx, taskID, "git status -z --porcelain --untracked-files=all", int(e.effectiveTimeout().Milliseconds()))
	if err != nil {
		return "", err
	}
	if res.TimedOut {
		return "", fmt.Errorf("git status -z --porcelain --untracked-files=all timed out")
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("git status -z --porcelain --untracked-files=all exited %d: %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return formatVerifyCleanViolation(res.Stdout, readOnlyPaths), nil
}

// verifyCleanLocal runs the same precondition against the local workdir.
// Mirrors verifyCleanRemote; kept separate so each path remains
// straightforward to read.
func (e *BashExecutor) verifyCleanLocal(ctx context.Context, readOnlyPaths []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.effectiveTimeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "status", "-z", "--porcelain", "--untracked-files=all")
	cmd.Dir = e.workDir
	cmd.Env = filterEnv()
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("git status -z --porcelain --untracked-files=all exited %d: %s", exitErr.ExitCode(), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return formatVerifyCleanViolation(string(out), readOnlyPaths), nil
}

// formatVerifyCleanViolation parses `git status -z --porcelain
// --untracked-files=all` output and returns a non-empty violation
// message if any dirty path matches readOnlyPaths (prefix-or-exact).
// When readOnlyPaths is empty, any dirty path is a violation —
// verify_clean then acts as a general clean-tree gate. Returns "" when
// the tree is clean (or no violating path matches).
//
// `-z` format is NUL-terminated `XY <path>\x00` records, with no
// quoting regardless of git's `core.quotePath` setting — paths with
// spaces, tabs, double-quotes, backslashes, or non-ASCII bytes survive
// the wire intact. This matters because git's default porcelain v1
// QUOTES such paths (`?? "src/has space.go"`), and a naive parser would
// then prefix-match the literal-quote-included string against
// readOnlyPaths and silently let the dirty file slip through. Using
// `-z` is the cleanest defence; we never have to know which quoting
// rules are in effect.
//
// Rename and copy rows (`R` / `C` status, in either index or worktree
// stage) emit TWO records: destination first, then source. We score the
// destination — that's what the next command would touch — and skip the
// source record by tracking pending.
func formatVerifyCleanViolation(porcelainZ string, readOnlyPaths []string) string {
	var dirty []string
	for _, path := range parsePorcelainZ(porcelainZ) {
		if pathMatchesAny(path, readOnlyPaths) {
			dirty = append(dirty, path)
		}
	}
	if len(dirty) == 0 {
		return ""
	}
	scope := "any path"
	if len(readOnlyPaths) > 0 {
		scope = "read_only_paths"
	}
	return fmt.Sprintf("verify_clean: tree has uncommitted changes in %s — refusing to run command. Dirty paths: %s",
		scope, strings.Join(dirty, ", "))
}

// parsePorcelainZ extracts the destination paths from `git status -z --porcelain
// --untracked-files=all` output — the paths the next command would touch.
//
// `-z` format is NUL-terminated `XY <path>\x00` records with NO quoting
// regardless of git's core.quotePath setting, so paths with spaces, quotes,
// backslashes, or non-ASCII survive intact (default porcelain-v1 would QUOTE
// them, and a naive parser would then mis-match the quote-included string). Using
// `-z` is the cleanest defence; we never have to know which quoting is in effect.
//
// Rename/copy rows (`R`/`C` in either stage byte) emit TWO records: destination
// first, then source. We take the destination (what the command touched) and skip
// the trailing source via the pending flag.
func parsePorcelainZ(porcelainZ string) []string {
	var paths []string
	skipNext := false
	for record := range strings.SplitSeq(porcelainZ, "\x00") {
		if skipNext {
			skipNext = false
			continue
		}
		// `XY <path>` — minimum 4 chars (status + space + ≥1 path char). The
		// empty trailing record from the final NUL is dropped by this check.
		if len(record) < 4 {
			continue
		}
		path := record[3:]
		if path == "" {
			continue
		}
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			skipNext = true
		}
		paths = append(paths, path)
	}
	return paths
}

// worktreeSnapshot is the read_only proof state (ADR-067): the current git HEAD
// sha plus the dirty paths NOT under a scratch exemption. Two snapshots taken
// before and after a command, both with an empty dirtyPaths and an equal head,
// prove net worktree-and-HEAD non-mutation.
type worktreeSnapshot struct {
	head       string   // `git rev-parse HEAD`
	dirtyPaths []string // dirty destination paths not exempted by scratch
}

// execReadOnly runs a command under the read_only policy: capture pre-state,
// refuse if the protected tree is already dirty, run, capture post-state, and
// return a typed violation if HEAD moved or any protected path was mutated.
// capture is the local- or remote-specific state reader; run executes the
// command. Fail-closed: a capture error (can't prove non-mutation) is an error
// result, not a pass.
func (e *BashExecutor) execReadOnly(callID string, capture func() (worktreeSnapshot, error), run func() (agentic.ToolResult, error)) (agentic.ToolResult, error) {
	pre, err := capture()
	if err != nil {
		return agentic.ToolResult{CallID: callID, Error: fmt.Sprintf("read_only precondition check failed: %v", err)}, nil
	}
	if len(pre.dirtyPaths) > 0 {
		return agentic.ToolResult{CallID: callID, Error: fmt.Sprintf(
			"read_only: protected worktree is not clean before the command (modulo scratch) — refusing. Dirty paths: %s",
			strings.Join(pre.dirtyPaths, ", "))}, nil
	}
	res, err := run()
	if err != nil {
		return res, err
	}
	post, perr := capture()
	if perr != nil {
		return agentic.ToolResult{CallID: callID, Error: fmt.Sprintf("read_only post-condition check failed: %v", perr)}, nil
	}
	if violation := readOnlyViolation(pre, post); violation != "" {
		return agentic.ToolResult{CallID: callID, Error: violation}, nil
	}
	return res, nil
}

// readOnlyViolation compares pre/post snapshots and returns a typed violation
// message (or "" when the command left the worktree and HEAD unchanged). HEAD
// pinning catches git-state mutations (commit / reset / checkout to a different
// ref) that leave the worktree clean; the dirty-path check catches direct
// create/modify/delete of protected files.
func readOnlyViolation(pre, post worktreeSnapshot) string {
	if pre.head != post.head {
		return fmt.Sprintf("read_only: command moved git HEAD (%s → %s) — worktree state mutated via git", pre.head, post.head)
	}
	if len(post.dirtyPaths) > 0 {
		return fmt.Sprintf("read_only: command mutated protected worktree paths: %s", strings.Join(post.dirtyPaths, ", "))
	}
	return ""
}

// dirtyNonScratch returns the dirty destination paths NOT exempted by scratch.
// Unlike verify_clean's pathMatchesAny (empty list ⇒ matches everything), an
// empty scratch list here exempts NOTHING, so every dirty path is a violation —
// scratch is an opt-in write allowance, not a default-open one.
func dirtyNonScratch(porcelainZ string, scratch []string) []string {
	var dirty []string
	for _, path := range parsePorcelainZ(porcelainZ) {
		if !pathUnderScratch(path, scratch) {
			dirty = append(dirty, path)
		}
	}
	return dirty
}

// pathUnderScratch reports whether path is equal to or under one of the scratch
// patterns (trailing-slash tolerant). Empty scratch matches nothing.
func pathUnderScratch(path string, scratch []string) bool {
	for _, pat := range scratch {
		pat = strings.TrimRight(pat, "/")
		if pat == "" {
			continue
		}
		if path == pat || strings.HasPrefix(path, pat+"/") {
			return true
		}
	}
	return false
}

// captureLocal reads the read_only proof state from the local workdir.
func (e *BashExecutor) captureLocal(ctx context.Context, scratch []string) (worktreeSnapshot, error) {
	status, err := e.gitLocal(ctx, "status", "-z", "--porcelain", "--untracked-files=all")
	if err != nil {
		return worktreeSnapshot{}, err
	}
	head, err := e.gitLocal(ctx, "rev-parse", "HEAD")
	if err != nil {
		return worktreeSnapshot{}, err
	}
	return worktreeSnapshot{head: strings.TrimSpace(head), dirtyPaths: dirtyNonScratch(status, scratch)}, nil
}

// gitLocal runs a git subcommand in the workdir with sensitive env stripped and
// returns stdout. A non-zero exit is surfaced with git's stderr.
func (e *BashExecutor) gitLocal(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.effectiveTimeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = e.workDir
	cmd.Env = filterEnv()
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("git %s exited %d: %s", strings.Join(args, " "), exitErr.ExitCode(), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// captureRemote reads the read_only proof state from the runner's container.
func (e *BashExecutor) captureRemote(ctx context.Context, taskID string, scratch []string) (worktreeSnapshot, error) {
	status, err := e.gitRemote(ctx, taskID, "git status -z --porcelain --untracked-files=all")
	if err != nil {
		return worktreeSnapshot{}, err
	}
	head, err := e.gitRemote(ctx, taskID, "git rev-parse HEAD")
	if err != nil {
		return worktreeSnapshot{}, err
	}
	return worktreeSnapshot{head: strings.TrimSpace(head), dirtyPaths: dirtyNonScratch(status, scratch)}, nil
}

// gitRemote runs a git command inside the runner's taskID worktree and returns
// stdout, surfacing timeout and non-zero exit as errors (fail-closed).
func (e *BashExecutor) gitRemote(ctx context.Context, taskID, command string) (string, error) {
	res, err := e.runner.Exec(ctx, taskID, command, int(e.effectiveTimeout().Milliseconds()))
	if err != nil {
		return "", err
	}
	if res.TimedOut {
		return "", fmt.Errorf("%s timed out", command)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("%s exited %d: %s", command, res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// pathMatchesAny returns true when path equals one of patterns or sits
// under a directory pattern. A trailing slash in the pattern is treated
// as a directory hint (and trimmed for the comparison); patterns without
// trailing slashes still match contents via the "<path>/" check, so
// callers don't have to be picky about format. When patterns is empty,
// every non-empty path matches — verify_clean degenerates to a "tree
// must be clean" check.
func pathMatchesAny(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pat := range patterns {
		pat = strings.TrimRight(pat, "/")
		if pat == "" {
			continue
		}
		if path == pat || strings.HasPrefix(path, pat+"/") {
			return true
		}
	}
	return false
}

// execRemote routes the command to the remote runner's sandbox container.
func (e *BashExecutor) execRemote(ctx context.Context, callID, command, taskID string) (agentic.ToolResult, error) {
	result, err := e.runner.Exec(ctx, taskID, command, int(e.effectiveTimeout().Milliseconds()))
	if err != nil {
		return agentic.ToolResult{
			CallID: callID,
			Error:  fmt.Sprintf("remote exec failed: %v", err),
		}, nil
	}

	output := result.Stdout
	if result.Stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += result.Stderr
	}

	if result.TimedOut {
		output += "\n[command timed out]"
	}

	if len(output) > bashMaxOutputBytes {
		output = output[:bashMaxOutputBytes] + "\n[output truncated]"
	}

	if result.ExitCode != 0 {
		return agentic.ToolResult{
			CallID: callID,
			Error:  fmt.Sprintf("exit code %d\n%s", result.ExitCode, output),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  callID,
		Content: output,
	}, nil
}

// sensitiveEnvSuffixes lists env var name suffixes indicating secrets.
var sensitiveEnvSuffixes = []string{
	"_KEY", "_SECRET", "_TOKEN", "_PASSWORD", "_CREDENTIAL",
	"_CREDENTIALS", "_AUTH", "_API_KEY", "_APIKEY",
}

// sensitiveEnvPrefixes lists env var name prefixes indicating cloud credentials.
var sensitiveEnvPrefixes = []string{
	"AWS_", "AZURE_", "GCP_", "GOOGLE_", "GITHUB_TOKEN",
	"OPENAI_", "ANTHROPIC_", "OPENROUTER_",
}

// sensitiveEnvExact lists specific env vars to always strip.
var sensitiveEnvExact = map[string]bool{
	"BRAVE_SEARCH_API_KEY": true,
	"DATABASE_URL":         true,
	"REDIS_URL":            true,
	"NATS_TOKEN":           true,
	"NATS_NKEY":            true,
	"SSH_AUTH_SOCK":        true,
	"GPG_AGENT_INFO":       true,
}

// filterEnv returns a copy of the environment with sensitive variables removed.
func filterEnv() []string {
	var filtered []string
	for _, env := range os.Environ() {
		name, _, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)

		if sensitiveEnvExact[name] {
			continue
		}

		skip := false
		for _, suffix := range sensitiveEnvSuffixes {
			if strings.HasSuffix(upper, suffix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, prefix := range sensitiveEnvPrefixes {
			if strings.HasPrefix(upper, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		filtered = append(filtered, env)
	}
	return filtered
}

// limitedBuffer is a bytes.Buffer that stops accepting writes after a limit.
// Prevents memory exhaustion from unbounded command output.
type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		return len(p), nil // discard silently, report full write to avoid cmd error
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	return b.Buffer.Write(p)
}

// execLocal runs the command locally via os/exec with sensitive env vars stripped.
func (e *BashExecutor) execLocal(ctx context.Context, callID, command string) (agentic.ToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.effectiveTimeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = e.workDir
	cmd.Env = filterEnv()

	// Cap buffers to prevent memory exhaustion from unbounded output.
	stdout := &limitedBuffer{limit: bashMaxOutputBytes + 1}
	stderr := &limitedBuffer{limit: bashMaxOutputBytes + 1}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if len(output) > bashMaxOutputBytes {
		output = output[:bashMaxOutputBytes] + "\n[output truncated]"
	}

	if err != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			output += "\n[command timed out]"
		}
		return agentic.ToolResult{
			CallID: callID,
			Error:  fmt.Sprintf("exit code %d\n%s", exitCode, output),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  callID,
		Content: output,
	}, nil
}

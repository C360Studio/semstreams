// Package executors provides tool executor implementations for the agentic system.
//
// BashExecutor runs shell commands. When a sandbox is configured (SANDBOX_URL),
// commands execute inside the sandbox container. Otherwise, they run locally
// via os/exec with sensitive environment variables stripped.
//
// Ported from semspec/tools/bash.
package executors

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/processor/agentic-tools/sandbox"
)

const (
	bashMaxOutputBytes = 100 * 1024 // 100KB output cap
	bashDefaultTimeout = 120 * time.Second
)

// BashExecutor runs shell commands locally or via sandbox.
type BashExecutor struct {
	workDir string
	sandbox *sandbox.Client
	timeout time.Duration
}

// BashOption configures a BashExecutor.
type BashOption func(*BashExecutor)

// WithBashTimeout overrides the default command timeout (120s).
func WithBashTimeout(d time.Duration) BashOption {
	return func(e *BashExecutor) { e.timeout = d }
}

// NewBashExecutor creates a bash executor. If sandboxURL is non-empty, commands
// are routed to the sandbox container.
func NewBashExecutor(workDir, sandboxURL string, opts ...BashOption) *BashExecutor {
	e := &BashExecutor{
		workDir: workDir,
	}
	for _, opt := range opts {
		opt(e)
	}
	if sandboxURL != "" {
		e.sandbox = sandbox.NewClient(sandboxURL)
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

	if e.sandbox != nil {
		taskID := "default"
		if m := call.Metadata; m != nil {
			if tid, ok := m["task_id"].(string); ok && tid != "" {
				taskID = tid
			} else if lid, ok := m["loop_id"].(string); ok && lid != "" {
				taskID = lid
			}
		}
		return e.execSandbox(ctx, call.ID, command, taskID)
	}
	return e.execLocal(ctx, call.ID, command)
}

// execSandbox routes the command to the sandbox container.
func (e *BashExecutor) execSandbox(ctx context.Context, callID, command, taskID string) (agentic.ToolResult, error) {
	result, err := e.sandbox.Exec(ctx, taskID, command, int(e.effectiveTimeout().Milliseconds()))
	if err != nil {
		return agentic.ToolResult{
			CallID: callID,
			Error:  fmt.Sprintf("sandbox exec failed: %v", err),
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

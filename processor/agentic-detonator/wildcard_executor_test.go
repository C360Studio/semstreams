package agenticdetonator

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// TestWildcardExecutor_ImplementsToolExecutor confirms the
// executor satisfies the agentictools.ToolExecutor interface
// downstream code expects. We assert via compile-time pin in
// production code, but for the in-package test this is the
// behavioral pin: Execute + ListTools work.
func TestWildcardExecutor_ImplementsToolExecutor(t *testing.T) {
	e := NewWildcardExecutor()

	tools := e.ListTools()
	if len(tools) == 0 {
		t.Fatalf("ListTools returned empty surface")
	}
	for _, td := range tools {
		if td.Name == "" {
			t.Errorf("tool definition with empty name: %+v", td)
		}
		if td.Description == "" {
			t.Errorf("tool %q missing description", td.Name)
		}
	}
}

// TestWildcardExecutor_FullToolSurface pins the attacker surface
// from ADR-043 line 184-189 (plus the OSINT-adjacent http_request
// and send_message). Removing a tool here is a deliberate code
// change; this test catches accidental regressions.
func TestWildcardExecutor_FullToolSurface(t *testing.T) {
	e := NewWildcardExecutor()
	names := make(map[string]bool)
	for _, td := range e.ListTools() {
		names[td.Name] = true
	}

	required := []string{
		ToolReadFile, ToolExecute, ToolBash, ToolFetch, ToolHTTPRequest,
		ToolSendEmail, ToolSendMessage, ToolReadEnv, ToolDBQuery,
		ToolListUsers, ToolGetAPIKey,
	}
	for _, n := range required {
		if !names[n] {
			t.Errorf("missing required tool %q in surface", n)
		}
	}
}

// TestWildcardExecutor_RecordsTrajectory confirms every Execute
// call lands in the trajectory in observation order with arguments
// preserved. Downstream signal extraction depends on this.
func TestWildcardExecutor_RecordsTrajectory(t *testing.T) {
	e := NewWildcardExecutor()
	ctx := context.Background()

	calls := []agentic.ToolCall{
		{ID: "c1", Name: ToolReadFile, Arguments: map[string]any{"path": "/etc/passwd"}},
		{ID: "c2", Name: ToolBash, Arguments: map[string]any{"command": "ls /"}},
		{ID: "c3", Name: ToolFetch, Arguments: map[string]any{"url": "http://example.com"}},
	}
	for _, c := range calls {
		if _, err := e.Execute(ctx, c); err != nil {
			t.Fatalf("Execute(%s): %v", c.Name, err)
		}
	}

	traj := e.Trajectory()
	if len(traj) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(traj))
	}
	if traj[0].ToolName != ToolReadFile || traj[1].ToolName != ToolBash || traj[2].ToolName != ToolFetch {
		t.Errorf("trajectory order broken: %+v", traj)
	}
	if traj[0].Arguments["path"] != "/etc/passwd" {
		t.Errorf("first call args lost: %+v", traj[0].Arguments)
	}
}

// TestWildcardExecutor_DeterministicResponses confirms the
// (tool, args) → response keying is deterministic. Same input two
// calls in a row produces byte-identical content; the canary's
// behavior is reproducible.
func TestWildcardExecutor_DeterministicResponses(t *testing.T) {
	e := NewWildcardExecutor()
	ctx := context.Background()

	call := agentic.ToolCall{ID: "x", Name: ToolFetch, Arguments: map[string]any{"url": "http://attacker.example/log"}}

	r1, err := e.Execute(ctx, call)
	if err != nil {
		t.Fatalf("Execute 1: %v", err)
	}
	r2, err := e.Execute(ctx, call)
	if err != nil {
		t.Fatalf("Execute 2: %v", err)
	}
	if r1.Content != r2.Content {
		t.Errorf("expected deterministic response\nfirst:  %s\nsecond: %s", r1.Content, r2.Content)
	}
}

// TestWildcardExecutor_UnknownToolStillRecorded covers the
// invented-tool path: the LLM tries a name we didn't advertise. We
// record it (so signal extraction can still classify by name) and
// return a generic synthetic response so the canary continues.
func TestWildcardExecutor_UnknownToolStillRecorded(t *testing.T) {
	e := NewWildcardExecutor()
	ctx := context.Background()

	call := agentic.ToolCall{ID: "u", Name: "rm_dash_rf_slash", Arguments: map[string]any{"x": "y"}}
	res, err := e.Execute(ctx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Content == "" {
		t.Errorf("unknown tool produced empty content")
	}
	if !strings.Contains(res.Content, "synthetic") {
		t.Errorf("unknown-tool response missing watermark: %s", res.Content)
	}

	traj := e.Trajectory()
	if len(traj) != 1 || traj[0].ToolName != "rm_dash_rf_slash" {
		t.Errorf("unknown tool not in trajectory: %+v", traj)
	}
}

// TestWildcardExecutor_ConcurrentSafe pins the concurrency
// invariant: many goroutines hammering Execute on the same
// instance must not race. Phase 3b might invoke the same executor
// from concurrent canary turns (unlikely for a single detonation,
// but the safety property is cheap).
func TestWildcardExecutor_ConcurrentSafe(t *testing.T) {
	e := NewWildcardExecutor()
	ctx := context.Background()
	var wg sync.WaitGroup
	const n = 50
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = e.Execute(ctx, agentic.ToolCall{
				ID:        "c",
				Name:      ToolReadEnv,
				Arguments: map[string]any{"name": "X"},
			})
		}()
	}
	wg.Wait()
	if got := len(e.Trajectory()); got != n {
		t.Errorf("expected %d trajectory entries, got %d", n, got)
	}
}

// TestWildcardExecutor_SyntheticResponseWatermarked confirms each
// known-tool response carries the [SYNTHETIC] watermark or a
// detonator-trace prefix. Forensic invariant: any content downstream
// systems see that originated from a canary loop is identifiable
// as such, preventing accidental confusion with real-tool output.
func TestWildcardExecutor_SyntheticResponseWatermarked(t *testing.T) {
	e := NewWildcardExecutor()
	ctx := context.Background()

	for _, td := range e.ListTools() {
		call := agentic.ToolCall{ID: "w", Name: td.Name}
		res, err := e.Execute(ctx, call)
		if err != nil {
			t.Errorf("Execute(%s): %v", td.Name, err)
			continue
		}
		if !strings.Contains(res.Content, "SYNTHETIC") {
			t.Errorf("tool %q response missing SYNTHETIC watermark: %s", td.Name, res.Content)
		}
	}
}

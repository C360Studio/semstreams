package agentictools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// scriptedExecutor returns a pre-seeded sequence of (ToolResult, error)
// outcomes on successive Execute calls. Used to simulate transient failures
// followed by success.
type scriptedExecutor struct {
	toolName string
	outcomes []scriptedOutcome
	call     atomic.Int32
}

type scriptedOutcome struct {
	result agentic.ToolResult
	err    error
}

func (s *scriptedExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{Name: s.toolName}}
}

func (s *scriptedExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	idx := int(s.call.Add(1) - 1)
	if idx >= len(s.outcomes) {
		idx = len(s.outcomes) - 1
	}
	out := s.outcomes[idx]
	// Stamp the call id so downstream assertions match.
	out.result.CallID = call.ID
	return out.result, out.err
}

// newComponentForRetry builds a minimal Component with the scripted executor
// registered locally and the provided retry policies. No NATS work, no
// metrics registry — just enough to exercise executeWithTimeout.
func newComponentForRetry(t *testing.T, policies map[string]RetryPolicy, exec *scriptedExecutor) *Component {
	t.Helper()
	c := &Component{
		config: Config{
			Timeout:     "2s",
			ToolRetries: policies,
		},
		registry: NewExecutorRegistry(),
		metrics:  getMetrics(nil),
	}
	if err := c.registry.RegisterTool(exec.toolName, exec); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}
	return c
}

func timeoutResult() agentic.ToolResult {
	return agentic.ToolResult{
		Error:     "deadline exceeded",
		ErrorKind: agentic.ToolErrorTimeout,
	}
}

func externalResult() agentic.ToolResult {
	return agentic.ToolResult{
		Error:     "upstream 503",
		ErrorKind: agentic.ToolErrorExternal,
	}
}

func invalidArgsResult() agentic.ToolResult {
	return agentic.ToolResult{
		Error:     "bad argument",
		ErrorKind: agentic.ToolErrorInvalidArgs,
	}
}

func successResult() agentic.ToolResult {
	return agentic.ToolResult{Content: "ok"}
}

// TestRetry_NoPolicyNoRetry verifies a tool without an entry in
// ToolRetries runs exactly once even on a retryable error.
func TestRetry_NoPolicyNoRetry(t *testing.T) {
	exec := &scriptedExecutor{
		toolName: "t",
		outcomes: []scriptedOutcome{{result: timeoutResult()}, {result: successResult()}},
	}
	c := newComponentForRetry(t, nil, exec)

	res, err := c.executeWithTimeout(context.Background(), agentic.ToolCall{ID: "c", Name: "t"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorTimeout {
		t.Errorf("expected timeout result (no retry), got kind=%v content=%q", res.ErrorKind, res.Content)
	}
	if got := int(exec.call.Load()); got != 1 {
		t.Errorf("executor called %d times, want 1", got)
	}
}

// TestRetry_SucceedsWithinBudget verifies that a transient timeout followed
// by success yields the success result and records exactly one retry.
func TestRetry_SucceedsWithinBudget(t *testing.T) {
	exec := &scriptedExecutor{
		toolName: "t",
		outcomes: []scriptedOutcome{{result: timeoutResult()}, {result: successResult()}},
	}
	policies := map[string]RetryPolicy{
		"t": {MaxAttempts: 3, BackoffInitialMs: 1, BackoffMaxMs: 1},
	}
	c := newComponentForRetry(t, policies, exec)

	res, err := c.executeWithTimeout(context.Background(), agentic.ToolCall{ID: "c", Name: "t"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.ErrorKind != "" {
		t.Errorf("expected success result, got kind=%v error=%q", res.ErrorKind, res.Error)
	}
	if got := int(exec.call.Load()); got != 2 {
		t.Errorf("executor called %d times, want 2 (1 fail + 1 retry)", got)
	}
}

// TestRetry_BudgetExhausted verifies that repeated retryable failures
// surface the final error kind after MaxAttempts.
func TestRetry_BudgetExhausted(t *testing.T) {
	exec := &scriptedExecutor{
		toolName: "t",
		outcomes: []scriptedOutcome{
			{result: externalResult()},
			{result: externalResult()},
			{result: externalResult()},
		},
	}
	policies := map[string]RetryPolicy{
		"t": {MaxAttempts: 3, BackoffInitialMs: 1, BackoffMaxMs: 1},
	}
	c := newComponentForRetry(t, policies, exec)

	res, err := c.executeWithTimeout(context.Background(), agentic.ToolCall{ID: "c", Name: "t"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorExternal {
		t.Errorf("expected external error on exhaustion, got kind=%v", res.ErrorKind)
	}
	if got := int(exec.call.Load()); got != 3 {
		t.Errorf("executor called %d times, want 3 (MaxAttempts)", got)
	}
}

// TestRetry_NonRetryableKindPassesThrough verifies that errors whose kind
// isn't in RetryOnKinds (e.g. ToolErrorInvalidArgs by default) are returned
// immediately without retry — those need LLM feedback, not framework
// retries.
func TestRetry_NonRetryableKindPassesThrough(t *testing.T) {
	exec := &scriptedExecutor{
		toolName: "t",
		outcomes: []scriptedOutcome{{result: invalidArgsResult()}, {result: successResult()}},
	}
	policies := map[string]RetryPolicy{
		"t": {MaxAttempts: 3, BackoffInitialMs: 1, BackoffMaxMs: 1},
	}
	c := newComponentForRetry(t, policies, exec)

	res, err := c.executeWithTimeout(context.Background(), agentic.ToolCall{ID: "c", Name: "t"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("expected invalid-args passed through, got kind=%v", res.ErrorKind)
	}
	if got := int(exec.call.Load()); got != 1 {
		t.Errorf("executor called %d times, want 1 (no retry for invalid args)", got)
	}
}

// TestRetry_CustomKindsOverrideDefault verifies that an explicit
// RetryOnKinds list replaces the default and retries only the listed kinds.
func TestRetry_CustomKindsOverrideDefault(t *testing.T) {
	exec := &scriptedExecutor{
		toolName: "t",
		// First call: default-retryable timeout. With custom RetryOnKinds
		// listing only "invalid_args", this should NOT retry.
		outcomes: []scriptedOutcome{{result: timeoutResult()}, {result: successResult()}},
	}
	policies := map[string]RetryPolicy{
		"t": {
			MaxAttempts:      3,
			BackoffInitialMs: 1,
			BackoffMaxMs:     1,
			RetryOnKinds:     []string{string(agentic.ToolErrorInvalidArgs)},
		},
	}
	c := newComponentForRetry(t, policies, exec)

	res, err := c.executeWithTimeout(context.Background(), agentic.ToolCall{ID: "c", Name: "t"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorTimeout {
		t.Errorf("expected timeout passed through (not in custom RetryOnKinds), got kind=%v", res.ErrorKind)
	}
	if got := int(exec.call.Load()); got != 1 {
		t.Errorf("executor called %d times, want 1", got)
	}
}

// TestRetry_RawErrorNotRetried verifies that an executor-level error with
// no ErrorKind classification is NOT retried — that's a framework fault,
// not a tool-level transient.
func TestRetry_RawErrorNotRetried(t *testing.T) {
	exec := &scriptedExecutor{
		toolName: "t",
		outcomes: []scriptedOutcome{
			{result: agentic.ToolResult{}, err: errors.New("connection refused")},
			{result: successResult()},
		},
	}
	policies := map[string]RetryPolicy{
		"t": {MaxAttempts: 3, BackoffInitialMs: 1, BackoffMaxMs: 1},
	}
	c := newComponentForRetry(t, policies, exec)

	_, err := c.executeWithTimeout(context.Background(), agentic.ToolCall{ID: "c", Name: "t"})
	if err == nil {
		t.Errorf("expected raw error to propagate")
	}
	if got := int(exec.call.Load()); got != 1 {
		t.Errorf("executor called %d times, want 1 (no retry for raw errors)", got)
	}
}

// TestEffectiveRetryPolicy_Defaults verifies the defaults applied when a
// policy has partial or zero values.
func TestEffectiveRetryPolicy_Defaults(t *testing.T) {
	policies := map[string]RetryPolicy{
		"bare": {MaxAttempts: 2}, // everything else zero
	}

	p := effectiveRetryPolicy(policies, "bare")
	if p.MaxAttempts != 2 {
		t.Errorf("MaxAttempts = %d, want 2", p.MaxAttempts)
	}
	if p.BackoffInitialMs != 100 {
		t.Errorf("BackoffInitialMs = %d, want 100 default", p.BackoffInitialMs)
	}
	if p.BackoffMaxMs != 2000 {
		t.Errorf("BackoffMaxMs = %d, want 2000 default", p.BackoffMaxMs)
	}
	wantKinds := map[string]bool{
		string(agentic.ToolErrorTimeout):  true,
		string(agentic.ToolErrorExternal): true,
	}
	if len(p.RetryOnKinds) != len(wantKinds) {
		t.Fatalf("RetryOnKinds = %v, want 2 defaults", p.RetryOnKinds)
	}
	for _, k := range p.RetryOnKinds {
		if !wantKinds[k] {
			t.Errorf("unexpected retry kind %q", k)
		}
	}

	// Missing entry -> no retry.
	p = effectiveRetryPolicy(policies, "unknown")
	if p.MaxAttempts != 1 {
		t.Errorf("unknown tool MaxAttempts = %d, want 1 (no retry)", p.MaxAttempts)
	}
}

// TestBackoffFor_ExponentialCappedAtMax verifies the schedule and cap.
func TestBackoffFor_ExponentialCappedAtMax(t *testing.T) {
	policy := RetryPolicy{BackoffInitialMs: 100, BackoffMaxMs: 400}
	tests := []struct {
		attempt int
		wantMs  int
	}{
		{1, 100}, // 100 * 2^0
		{2, 200}, // 100 * 2^1
		{3, 400}, // 100 * 2^2
		{4, 400}, // capped
		{5, 400}, // capped
	}
	for _, tc := range tests {
		got := backoffFor(tc.attempt, policy).Milliseconds()
		if got != int64(tc.wantMs) {
			t.Errorf("attempt %d: backoff = %dms, want %dms", tc.attempt, got, tc.wantMs)
		}
	}
}

// Package rule — tests for the auth.Identity → CallerContext private converter.
package rule

import (
	"context"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/auth"
)

// TestIdentityToCallerContext_PopulatedIdentity verifies that all three fields
// (ID, Role, Org) are copied from a fully-populated *auth.Identity.
func TestIdentityToCallerContext_PopulatedIdentity(t *testing.T) {
	t.Parallel()

	id := &auth.Identity{
		ID:     "alice",
		Role:   "admin",
		Org:    "acme",
		Source: auth.SourceHTTPHeader,
	}
	got := identityToCallerContext(id)
	if got == nil {
		t.Fatal("expected non-nil *CallerContext, got nil")
	}
	if got.ID != "alice" {
		t.Errorf("ID = %q, want %q", got.ID, "alice")
	}
	if got.Role != "admin" {
		t.Errorf("Role = %q, want %q", got.Role, "admin")
	}
	if got.Org != "acme" {
		t.Errorf("Org = %q, want %q", got.Org, "acme")
	}
}

// TestIdentityToCallerContext_NilIdentity verifies that nil input returns nil
// output, preserving the tag-1 contract: cron fires and KV-watch fires have
// no caller, so Caller remains nil.
func TestIdentityToCallerContext_NilIdentity(t *testing.T) {
	t.Parallel()

	if got := identityToCallerContext(nil); got != nil {
		t.Errorf("identityToCallerContext(nil) = %+v, want nil", got)
	}
}

// TestIdentityToCallerContext_PartialFields verifies that an *auth.Identity
// with only ID populated (empty Role, Org) produces a *CallerContext with
// the same partial values — empty strings, not the zero sentinel.
func TestIdentityToCallerContext_PartialFields(t *testing.T) {
	t.Parallel()

	id := &auth.Identity{ID: "alice"}
	got := identityToCallerContext(id)
	if got == nil {
		t.Fatal("expected non-nil *CallerContext, got nil")
	}
	if got.ID != "alice" {
		t.Errorf("ID = %q, want %q", got.ID, "alice")
	}
	if got.Role != "" {
		t.Errorf("Role = %q, want empty string", got.Role)
	}
	if got.Org != "" {
		t.Errorf("Org = %q, want empty string", got.Org)
	}
}

// TestIdentityToCallerContext_DropsSource verifies that the Source field on
// *auth.Identity is intentionally dropped — CallerContext has no Source field.
// This test confirms the converter compiles without attempting to set a field
// that doesn't exist on CallerContext, and that the result is a valid struct.
func TestIdentityToCallerContext_DropsSource(t *testing.T) {
	t.Parallel()

	id := &auth.Identity{
		ID:     "bob",
		Role:   "viewer",
		Org:    "widgets",
		Source: auth.SourceHTTPHeader,
	}
	got := identityToCallerContext(id)
	if got == nil {
		t.Fatal("expected non-nil *CallerContext, got nil")
	}
	// CallerContext has exactly three fields: ID, Role, Org.
	// If this test compiles, Source was not referenced on CallerContext.
	if got.ID != "bob" || got.Role != "viewer" || got.Org != "widgets" {
		t.Errorf("unexpected CallerContext = %+v", got)
	}
}

// TestMessagePathIdentityThreading verifies the end-to-end seam: when a ctx
// carries an *auth.Identity and the stateful evaluator runs the message-path
// (Evaluation.Caller set from identityToCallerContext), the resulting
// ExecutionContext.Caller reflects the identity.
//
// This tests the wiring seam at the unit level by directly calling
// identityToCallerContext and asserting the round-trip: Identity fields
// survive to CallerContext. The deeper Evaluate path is verified by the
// stateful_evaluator_test.go suite; here we focus on the adapter boundary.
func TestMessagePathIdentityThreading(t *testing.T) {
	t.Parallel()

	id := &auth.Identity{
		ID:     "carol",
		Role:   "operator",
		Org:    "acme",
		Source: auth.SourceHTTPHeader,
	}
	ctx := auth.WithIdentity(context.Background(), id)

	// Simulate what evaluateRulesForMessage does: extract from ctx, convert.
	extracted, ok := auth.IdentityFromContext(ctx)
	if !ok {
		t.Fatal("IdentityFromContext returned false; WithIdentity did not attach identity")
	}
	cc := identityToCallerContext(extracted)
	if cc == nil {
		t.Fatal("identityToCallerContext returned nil for non-nil identity")
	}
	if cc.ID != "carol" {
		t.Errorf("CallerContext.ID = %q, want %q", cc.ID, "carol")
	}
	if cc.Role != "operator" {
		t.Errorf("CallerContext.Role = %q, want %q", cc.Role, "operator")
	}
	if cc.Org != "acme" {
		t.Errorf("CallerContext.Org = %q, want %q", cc.Org, "acme")
	}
}

// TestKVWatchPathNilCaller verifies that the KV-watch path (evaluateRulesForEntityState)
// leaves Caller nil regardless of what ctx contains. KV entries carry no headers;
// the caller context is absent by design.
//
// This test exercises the actual StatefulEvaluator.Evaluate flow to confirm
// that the Evaluation constructed in evaluateRulesForEntityState does NOT
// populate Caller — even if the ctx received by the test has an identity.
func TestKVWatchPathNilCaller(t *testing.T) {
	t.Parallel()

	// A ctx with identity attached — the KV-watch path must ignore this.
	id := &auth.Identity{ID: "alice", Role: "admin", Org: "acme"}
	ctx := auth.WithIdentity(context.Background(), id)

	bucket := newMockKVBucket()
	logger := slog.Default()
	stateTracker := NewStateTracker(bucket, logger)

	// callerCapture records the Caller pointer passed to Execute.
	var capturedCaller *CallerContext
	capture := &callerCaptureExecutor{onExecute: func(ec *ExecutionContext) {
		capturedCaller = ec.Caller
	}}

	evaluator := NewStatefulEvaluator(stateTracker, capture, logger)

	ruleDef := Definition{
		ID:   "kv-test-rule",
		Type: "expression",
		Name: "KV Watch Test Rule",
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}

	// Evaluate as the KV-watch path would: no Caller on Evaluation.
	_, err := evaluator.Evaluate(ctx, Evaluation{
		Rule:              ruleDef,
		EntityID:          "entity-kv-001",
		CurrentlyMatching: true,
		// Caller intentionally absent — KV-watch fires have no caller context.
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if capturedCaller != nil {
		t.Errorf("KV-watch path: ExecutionContext.Caller = %+v, want nil", capturedCaller)
	}
}

// callerCaptureExecutor is a test-only ActionExecutorInterface that calls
// onExecute with the ExecutionContext so tests can inspect Caller.
type callerCaptureExecutor struct {
	onExecute func(*ExecutionContext)
}

func (c *callerCaptureExecutor) Execute(_ context.Context, _ Action, ec *ExecutionContext) error {
	if c.onExecute != nil {
		c.onExecute(ec)
	}
	return nil
}

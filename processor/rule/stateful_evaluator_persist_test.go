package rule

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

// ctxHonoringBucket mirrors production NATS KV semantics: Put fails once the
// ctx is cancelled. The plain mock ignores ctx cancellation, which hides the
// gh#557 shutdown window from unit tests.
type ctxHonoringBucket struct {
	jetstream.KeyValue
	valueKey any
	value    any
}

func (b ctxHonoringBucket) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if b.valueKey != nil && ctx.Value(b.valueKey) != b.value {
		return 0, context.Canceled
	}
	return b.KeyValue.Put(ctx, key, value)
}

// cancellingExecutor cancels the evaluation's parent ctx while an action is
// executing — the SIGTERM-lands-mid-evaluation analogue: by the time the
// post-action persist runs, the watcher ctx is already dead (gh#557).
type cancellingExecutor struct {
	cancel context.CancelFunc
	calls  atomic.Int64
}

func (e *cancellingExecutor) Execute(context.Context, Action, *ExecutionContext) error {
	e.calls.Add(1)
	e.cancel()
	return nil
}

// TestEvaluatePersistsMatchStateWhenCtxCancelledMidAction is the gh#557
// regression: once actions have fired, the MatchState persist recording them
// is a durability obligation — a parent-context cancellation (SIGTERM
// reaching the watcher before Manager.StopAll) must not abort it. A lost
// post-action persist means bootstrap re-derives a world where the actions
// never happened: OnRecovery silently never fires for on_recovery-only
// rules, and OnEnter/OnExit double-fire with a reset iteration cap.
func TestEvaluatePersistsMatchStateWhenCtxCancelledMidAction(t *testing.T) {
	t.Parallel()

	type contextKey string
	key := contextKey("durability-value")
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key, "preserved"))
	defer cancel()

	tracker := NewStateTracker(ctxHonoringBucket{KeyValue: newMockKVBucket(), valueKey: key, value: "preserved"}, nil)
	executor := &cancellingExecutor{cancel: cancel}
	evaluator := NewStatefulEvaluator(tracker, executor, nil)

	ruleDef := Definition{
		ID:   "gh557-rule",
		Type: "expression",
		OnEnter: []Action{
			{Type: ActionTypePublish, Subject: "test.entered"},
		},
	}
	const entityID = "acme.ops.rule.gcs.mission.gh557"

	transition, err := evaluator.Evaluate(ctx, Evaluation{
		Rule: ruleDef, EntityID: entityID, CurrentlyMatching: true,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error after mid-action cancellation: %v", err)
	}
	if transition != TransitionEntered {
		t.Fatalf("transition = %v, want %v", transition, TransitionEntered)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("executor calls = %d, want 1 (action must have fired before cancellation)", got)
	}

	// The persist must survive the cancellation that landed mid-action.
	persisted, err := tracker.Get(context.Background(), "gh557-rule", entityID)
	if err != nil {
		t.Fatalf("MatchState lost to mid-action ctx cancellation (gh#557): %v", err)
	}
	if !persisted.IsMatching {
		t.Error("persisted MatchState.IsMatching = false, want true")
	}
	if persisted.LastTransition != string(TransitionEntered) {
		t.Errorf("persisted LastTransition = %q, want %q", persisted.LastTransition, TransitionEntered)
	}
}

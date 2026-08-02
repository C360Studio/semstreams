package agenticloop

// Tests for gh#857 D4 (payload-size-chokepoints groups 3-4): the four
// loops-bucket write lanes are loud — they RETURN their errors (the void
// log-and-drop shape was the defect class), bounded-retry transients, and
// mark the loop with the typed result-not-durable state on final failure —
// and oversized completion results offload to the content store as
// {storage_ref, preview, size}.
//
// Mutation targets: revert any persist* function to a void signature and
// these tests fail to compile; make one swallow its error and the
// returned-error assertions fail; drop the markResultNotDurable call and the
// typed-state assertions fail; drop the offload call in persistHandlerResult
// and the ref-shape assertions fail.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

// fakeLoopsKV is an in-memory loopsKV with per-key programmable failures.
// When payloadLimit > 0 it ENFORCES it exactly like the real guarded lane /
// server: an oversized value is REJECTED with the classified permanent
// refusal, never stored. The pre-upgrade fake accepted any size, which let a
// value the real server would refuse read back as a green test (the Blocker-4
// false green the Codex review proved).
type fakeLoopsKV struct {
	mu       sync.Mutex
	values   map[string][]byte
	attempts map[string]int
	// failPrefix selects keys that fail; failTimes is how many leading
	// attempts fail (-1 = always); failErr is what they fail with.
	failPrefix string
	failTimes  int
	failErr    error
	// payloadLimit, when > 0, rejects any value larger than it (the
	// enforced wire/KV ceiling).
	payloadLimit int
}

func newFakeLoopsKV() *fakeLoopsKV {
	return &fakeLoopsKV{values: make(map[string][]byte), attempts: make(map[string]int)}
}

func (f *fakeLoopsKV) Put(_ context.Context, key string, value []byte) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts[key]++
	if f.failErr != nil && f.failPrefix != "" && strings.HasPrefix(key, f.failPrefix) {
		if f.failTimes < 0 || f.attempts[key] <= f.failTimes {
			return 0, f.failErr
		}
	}
	if f.payloadLimit > 0 && len(value) > f.payloadLimit {
		return 0, errs.WrapInvalid(
			fmt.Errorf("%w: %d bytes > %d-byte server limit for key %s",
				natsclient.ErrPayloadTooLarge, len(value), f.payloadLimit, key),
			"Client", "KVStore.Put", "refuse oversized payload")
	}
	f.values[key] = append([]byte(nil), value...)
	return uint64(f.attempts[key]), nil
}

func (f *fakeLoopsKV) Get(_ context.Context, key string) (*natsclient.KVEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.values[key]
	if !ok {
		return nil, natsclient.ErrKVKeyNotFound
	}
	return &natsclient.KVEntry{Key: key, Value: v, Revision: 1}, nil
}

func (f *fakeLoopsKV) Keys(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.values))
	for k := range f.values {
		keys = append(keys, k)
	}
	return keys, nil
}

func (f *fakeLoopsKV) attemptsFor(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts[key]
}

func (f *fakeLoopsKV) valueFor(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.values[key]
	return v, ok
}

// fakeContentStore records StoreContent calls and returns a canned ref.
type fakeContentStore struct {
	mu      sync.Mutex
	stored  []message.ContentStorable
	failErr error
}

func (f *fakeContentStore) StoreContent(_ context.Context, cs message.ContentStorable) (*message.StorageReference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return nil, f.failErr
	}
	f.stored = append(f.stored, cs)
	return &message.StorageReference{
		StorageInstance: "objectstore",
		Key:             "content_" + cs.EntityID(),
		ContentType:     "application/json",
	}, nil
}

// newDurabilityComponent builds a NATS-less Component wired to the fake
// guarded lane, with one loop created and (optionally) completed.
func newDurabilityComponent(t *testing.T, store loopsKV) (*Component, string) {
	t.Helper()
	handler := NewMessageHandler(DefaultConfig())
	c := &Component{
		config:     DefaultConfig(),
		handler:    handler,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		loopsStore: store,
		graphWriter: &graphWriter{
			platform: types.PlatformMeta{Org: "c360", Platform: "ops"},
			logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	}
	loopID, err := handler.loopManager.CreateLoop("task-1", "coordinator", "model-x")
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}
	return c, loopID
}

func permanentErr() error {
	return errs.WrapInvalid(
		fmt.Errorf("%w: 2000000 bytes > 1048576-byte server limit for key X", natsclient.ErrPayloadTooLarge),
		"Client", "KVStore.Put", "refuse oversized payload")
}

func completionFor(loopID string) *agentic.LoopCompletedEvent {
	return &agentic.LoopCompletedEvent{
		LoopID:  loopID,
		TaskID:  "task-1",
		Outcome: agentic.OutcomeSuccess,
		Role:    "coordinator",
		Result:  "the result body",
	}
}

// --- 3.1: loud writes, bounded retry, typed result-not-durable state ---

func TestPersistCompletionState_PermanentFailure_ReturnsErrorAndMarksLoop(t *testing.T) {
	store := newFakeLoopsKV()
	store.failPrefix = "COMPLETE_"
	store.failTimes = -1
	store.failErr = permanentErr()
	c, loopID := newDurabilityComponent(t, store)

	err := c.persistCompletionState(context.Background(), loopID, completionFor(loopID))
	if err == nil {
		t.Fatal("persistCompletionState must RETURN the write error (the dropped return was the gh#857 defect)")
	}
	if !errors.Is(err, natsclient.ErrPayloadTooLarge) {
		t.Fatalf("error must preserve the typed cause, got %v", err)
	}
	if got := store.attemptsFor("COMPLETE_" + loopID); got != 1 {
		t.Fatalf("permanent failure must not retry: got %d attempts", got)
	}

	entity, gerr := c.handler.GetLoop(loopID)
	if gerr != nil {
		t.Fatalf("GetLoop: %v", gerr)
	}
	if !entity.ResultNotDurable {
		t.Fatal("loop must carry the typed result-not-durable state, not a log line")
	}
	if entity.ResultNotDurableReason == "" {
		t.Fatal("result-not-durable state must name the classified cause")
	}

	// The marker itself must be persisted so a parent/operator READING the
	// loop (not the process memory) can distinguish this from success and
	// absence.
	raw, ok := store.valueFor(loopID)
	if !ok {
		t.Fatal("result-not-durable marker was not persisted to the loops bucket")
	}
	var persisted agentic.LoopEntity
	if uerr := json.Unmarshal(raw, &persisted); uerr != nil {
		t.Fatalf("unmarshal persisted marker: %v", uerr)
	}
	if !persisted.ResultNotDurable {
		t.Fatal("persisted loop entity must carry result_not_durable=true")
	}
}

func TestPersistCompletionState_TransientFailure_RetriesThenSucceeds(t *testing.T) {
	store := newFakeLoopsKV()
	store.failPrefix = "COMPLETE_"
	store.failTimes = 2
	store.failErr = errors.New("nats: timeout") // unclassified → transient
	c, loopID := newDurabilityComponent(t, store)

	if err := c.persistCompletionState(context.Background(), loopID, completionFor(loopID)); err != nil {
		t.Fatalf("transient failures within the retry budget must succeed, got %v", err)
	}
	if got := store.attemptsFor("COMPLETE_" + loopID); got != 3 {
		t.Fatalf("expected 3 attempts (2 transient failures + success), got %d", got)
	}
	entity, _ := c.handler.GetLoop(loopID)
	if entity.ResultNotDurable {
		t.Fatal("a write that eventually succeeded must NOT be marked result-not-durable")
	}
}

func TestPersistCompletionState_TransientExhausted_ReturnsErrorAndMarksLoop(t *testing.T) {
	store := newFakeLoopsKV()
	store.failPrefix = "COMPLETE_"
	store.failTimes = -1
	store.failErr = errors.New("nats: timeout")
	c, loopID := newDurabilityComponent(t, store)

	err := c.persistCompletionState(context.Background(), loopID, completionFor(loopID))
	if err == nil {
		t.Fatal("exhausted transient retries must return the error")
	}
	if got := store.attemptsFor("COMPLETE_" + loopID); got != loopKVWriteRetry.MaxAttempts {
		t.Fatalf("expected %d bounded attempts, got %d", loopKVWriteRetry.MaxAttempts, got)
	}
	entity, _ := c.handler.GetLoop(loopID)
	if !entity.ResultNotDurable {
		t.Fatal("an exhausted write is a not-durable result — the state must say so")
	}
}

func TestPersistFailureState_PermanentFailure_ReturnsErrorAndMarksLoop(t *testing.T) {
	store := newFakeLoopsKV()
	store.failPrefix = "COMPLETE_"
	store.failTimes = -1
	store.failErr = permanentErr()
	c, loopID := newDurabilityComponent(t, store)

	failure := &agentic.LoopFailedEvent{LoopID: loopID, TaskID: "task-1", Outcome: agentic.OutcomeFailed, Reason: "model_error"}
	err := c.persistFailureState(context.Background(), loopID, failure)
	if err == nil {
		t.Fatal("persistFailureState must return the write error")
	}
	if got := store.attemptsFor("COMPLETE_" + loopID); got != 1 {
		t.Fatalf("permanent failure must not retry: got %d attempts", got)
	}
	entity, _ := c.handler.GetLoop(loopID)
	if !entity.ResultNotDurable {
		t.Fatal("failure-state loss must mark the loop result-not-durable")
	}
}

func TestPersistCancellationState_PermanentFailure_ReturnsErrorAndMarksLoop(t *testing.T) {
	store := newFakeLoopsKV()
	store.failPrefix = "COMPLETE_"
	store.failTimes = -1
	store.failErr = permanentErr()
	c, loopID := newDurabilityComponent(t, store)

	cancelled := &agentic.LoopCancelledEvent{LoopID: loopID, TaskID: "task-1", Outcome: agentic.OutcomeCancelled, CancelledBy: "user"}
	err := c.persistCancellationState(context.Background(), loopID, cancelled)
	if err == nil {
		t.Fatal("persistCancellationState must return the write error")
	}
	entity, _ := c.handler.GetLoop(loopID)
	if !entity.ResultNotDurable {
		t.Fatal("cancellation-state loss must mark the loop result-not-durable")
	}
}

func TestPersistLoopState_TerminalPermanentFailure_MarksResultNotDurable(t *testing.T) {
	store := newFakeLoopsKV()
	c, loopID := newDurabilityComponent(t, store)
	if err := c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}
	if err := c.handler.loopManager.UpdateCompletion(loopID, agentic.OutcomeSuccess, strings.Repeat("r", 8192), ""); err != nil {
		t.Fatalf("UpdateCompletion: %v", err)
	}

	// Fail the FIRST write of the entity key permanently; the marker rewrite
	// (second Put on the same key, now slimmed) succeeds.
	store.failPrefix = loopID
	store.failTimes = 1
	store.failErr = permanentErr()

	err := c.persistLoopState(context.Background(), loopID)
	if err == nil {
		t.Fatal("persistLoopState must return the write error")
	}
	entity, _ := c.handler.GetLoop(loopID)
	if !entity.ResultNotDurable {
		t.Fatal("terminal entity write loss must mark result-not-durable")
	}
	if len(entity.Result) > resultPreviewBytes {
		t.Fatalf("marker path must slim the inline result to a preview; still %d bytes", len(entity.Result))
	}
	if entity.ResultSize != 8192 {
		t.Fatalf("original result size must be recorded, got %d", entity.ResultSize)
	}
	raw, ok := store.valueFor(loopID)
	if !ok {
		t.Fatal("slimmed marker value must persist")
	}
	var persisted agentic.LoopEntity
	if uerr := json.Unmarshal(raw, &persisted); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	if !persisted.ResultNotDurable {
		t.Fatal("persisted marker must carry result_not_durable=true")
	}
}

func TestPersistLoopState_MidLoopFailure_DoesNotClaimResultLoss(t *testing.T) {
	store := newFakeLoopsKV()
	c, loopID := newDurabilityComponent(t, store)
	store.failPrefix = loopID
	store.failTimes = -1
	store.failErr = permanentErr()

	err := c.persistLoopState(context.Background(), loopID)
	if err == nil {
		t.Fatal("mid-loop state write failure must still return its error")
	}
	entity, _ := c.handler.GetLoop(loopID)
	if entity.ResultNotDurable {
		t.Fatal("a loop with no result yet must NOT be marked result-not-durable (false claim)")
	}
}

// --- 3.2: offload above the derived threshold ---

func TestOffloadCompletionResult_AboveThreshold_RewritesToRefShape(t *testing.T) {
	store := newFakeLoopsKV()
	c, loopID := newDurabilityComponent(t, store)
	content := &fakeContentStore{}
	c.resultOffloadStore = content
	c.payloadLimit = func() int { return 1000 } // threshold = 500

	body := strings.Repeat("x", 600)
	completion := completionFor(loopID)
	completion.Result = body

	c.offloadCompletionResult(context.Background(), completion)

	if completion.ResultRef == nil {
		t.Fatal("oversized result must offload: storage_ref missing")
	}
	if completion.Result != "" {
		t.Fatal("inline result must be replaced by the ref shape")
	}
	if completion.ResultSize != 600 {
		t.Fatalf("size must record the original byte count, got %d", completion.ResultSize)
	}
	if completion.ResultPreview == "" || !strings.HasPrefix(body, completion.ResultPreview) {
		t.Fatal("preview must be a prefix of the original result")
	}
	if len(content.stored) != 1 {
		t.Fatalf("expected exactly one content store write, got %d", len(content.stored))
	}
	// The stored carrier must expose the body via the ContentStorable
	// contract the reader hydrates through.
	cs := content.stored[0]
	field := cs.ContentFields()[message.ContentRoleBody]
	if got := cs.RawContent()[field]; got != body {
		t.Fatal("stored content body must be the full original result")
	}

	// Mirror on the loop entity: the entity KV value must not carry the
	// oversized body either.
	entity, _ := c.handler.GetLoop(loopID)
	if entity.Result != completion.ResultPreview {
		t.Fatal("loop entity inline result must be the preview after offload")
	}
	if entity.ResultRef == nil || entity.ResultRef.Key != completion.ResultRef.Key {
		t.Fatal("loop entity must mirror the storage ref")
	}
	if entity.ResultSize != 600 {
		t.Fatalf("loop entity must mirror the original size, got %d", entity.ResultSize)
	}
	if entity.ResultNotDurable {
		t.Fatal("a successfully offloaded result is durable — the not-durable marker would be a false claim")
	}
}

func TestOffloadCompletionResult_BelowThreshold_LeavesInline(t *testing.T) {
	store := newFakeLoopsKV()
	c, loopID := newDurabilityComponent(t, store)
	content := &fakeContentStore{}
	c.resultOffloadStore = content
	c.payloadLimit = func() int { return 1000 }

	completion := completionFor(loopID)
	completion.Result = strings.Repeat("x", 400) // under 500 threshold
	c.offloadCompletionResult(context.Background(), completion)

	if completion.ResultRef != nil || completion.Result == "" {
		t.Fatal("under-threshold results must stay inline")
	}
	if len(content.stored) != 0 {
		t.Fatal("no content store write expected under threshold")
	}
}

func TestOffloadCompletionResult_StoreFailure_LeavesInlineForTheGuard(t *testing.T) {
	store := newFakeLoopsKV()
	c, loopID := newDurabilityComponent(t, store)
	c.resultOffloadStore = &fakeContentStore{failErr: errors.New("objectstore down")}
	c.payloadLimit = func() int { return 1000 }

	body := strings.Repeat("x", 600)
	completion := completionFor(loopID)
	completion.Result = body
	c.offloadCompletionResult(context.Background(), completion)

	if completion.Result != body || completion.ResultRef != nil {
		t.Fatal("failed offload must leave the inline result for the seam guard to rule on — never half-rewrite")
	}
}

// TestPersistHandlerResult_OversizedCompletion_EndToEndShape drives the full
// component-side path: offload → slim entity write → ref-bearing COMPLETE_
// write → re-marshaled agent.complete publish payload. Deleting the offload
// call, the entity mirror, or the publish rebuild each fails a distinct
// assertion here.
func TestPersistHandlerResult_OversizedCompletion_EndToEndShape(t *testing.T) {
	store := newFakeLoopsKV()
	// The fake ENFORCES the 1000-byte ceiling like the real server (Blocker
	// 4): the ref-bearing COMPLETE_ value only lands if the preview was
	// trimmed until the serialized carrier fits — the pre-upgrade fake
	// accepted a 2KB preview under a 1KB limit and read back green.
	store.payloadLimit = 1000
	c, loopID := newDurabilityComponent(t, store)
	content := &fakeContentStore{}
	c.resultOffloadStore = content
	c.payloadLimit = func() int { return 1000 }

	if err := c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}
	// Larger than resultPreviewBytes so the preview cannot contain the whole
	// body — otherwise the "publish no longer carries the body" assertion
	// below would be satisfied by the preview itself.
	body := strings.Repeat("R", resultPreviewBytes+1000)
	if err := c.handler.loopManager.UpdateCompletion(loopID, agentic.OutcomeSuccess, body, ""); err != nil {
		t.Fatalf("UpdateCompletion: %v", err)
	}

	completion := completionFor(loopID)
	completion.Result = body
	completionMsg := message.NewBaseMessage(completion.Schema(), completion, "agentic-loop")
	inlineData, err := json.Marshal(completionMsg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	result := HandlerResult{
		LoopID:          loopID,
		State:           agentic.LoopStateComplete,
		CompletionState: completion,
		PublishedMessages: []PublishedMessage{{
			Subject: "agent.complete." + loopID, // DefaultConfig port subject shape
			Data:    inlineData,
		}},
	}

	c.persistHandlerResult(context.Background(), result)

	// COMPLETE_ value carries the ref shape AND fits the enforced carrier
	// ceiling — it can only exist because the preview trimmed until the
	// serialized value fit (Blocker 4 trim path, not a false green).
	raw, ok := store.valueFor("COMPLETE_" + loopID)
	if !ok {
		t.Fatal("COMPLETE_ value missing — the trim path must shrink the preview until the carrier fits the enforced limit")
	}
	if len(raw) > 1000 {
		t.Fatalf("stored COMPLETE_ value exceeds the enforced 1000-byte ceiling: %d bytes", len(raw))
	}
	var event agentic.LoopCompletedEvent
	if uerr := json.Unmarshal(raw, &event); uerr != nil {
		t.Fatalf("unmarshal COMPLETE_: %v", uerr)
	}
	if event.ResultRef == nil || event.Result != "" || event.ResultSize != len(body) {
		t.Fatalf("COMPLETE_ value must be ref-bearing {storage_ref, preview, size}; got ref=%v inline=%d size=%d",
			event.ResultRef, len(event.Result), event.ResultSize)
	}
	if event.ResultPreview == "" || !strings.HasPrefix(body, event.ResultPreview) {
		t.Fatal("trimmed preview must remain a (possibly shorter) prefix of the original result")
	}

	// Entity value is slim.
	entityRaw, ok := store.valueFor(loopID)
	if !ok {
		t.Fatal("loop entity value missing")
	}
	var entity agentic.LoopEntity
	if uerr := json.Unmarshal(entityRaw, &entity); uerr != nil {
		t.Fatalf("unmarshal entity: %v", uerr)
	}
	if len(entity.Result) > resultPreviewBytes || entity.ResultRef == nil {
		t.Fatal("persisted loop entity must carry the slimmed preview + ref, not the full body")
	}

	// The queued agent.complete publish was re-marshaled to the ref shape.
	if strings.Contains(string(result.PublishedMessages[0].Data), body) {
		t.Fatal("agent.complete publish still carries the full inline body; stream lane must match the KV shape")
	}
	if !strings.Contains(string(result.PublishedMessages[0].Data), `"storage_ref"`) {
		t.Fatal("agent.complete publish must carry the storage_ref shape")
	}
}

// --- 4.1: over-limit publish is a TERMINAL loop failure, never a retry ---

// TestPublishResults_OversizedPublish_FailsLoopTerminally drives the
// PRODUCTION publish seam: a conn-less natsclient.Client refuses the
// oversized publish at the size guard (which runs before any connection
// gate), and publishResults must convert that typed permanent refusal into a
// terminal loop failure whose reason and error name the size and the limit.
func TestPublishResults_OversizedPublish_FailsLoopTerminally(t *testing.T) {
	store := newFakeLoopsKV()
	c, loopID := newDurabilityComponent(t, store)

	client, err := natsclient.NewClient("nats://127.0.0.1:4222")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// Seed the cached advertisement (as a prior connection would have): an
	// UNKNOWN limit must never produce a permanent verdict, so the conn-less
	// zero client alone would surface ErrNotConnected, not the guard.
	client.SeedServerPayloadLimitForTest(1 << 20)
	c.natsClient = client
	c.payloadLimit = client.ServerPayloadLimit

	oversized := make([]byte, 2<<20) // 2MB > the 1MB seeded advertised limit
	result := HandlerResult{
		LoopID: loopID,
		State:  agentic.LoopStateExecuting, // live loop: the refused message is its forward progress
		PublishedMessages: []PublishedMessage{{
			Subject: "agent.request." + loopID,
			Data:    oversized,
		}},
	}

	c.publishResults(context.Background(), result)

	entity, gerr := c.handler.GetLoop(loopID)
	if gerr != nil {
		t.Fatalf("GetLoop: %v", gerr)
	}
	if entity.State != agentic.LoopStateFailed {
		t.Fatalf("over-limit request publish must fail the loop TERMINALLY, state=%s", entity.State)
	}
	if entity.Outcome != agentic.OutcomeFailed {
		t.Fatalf("outcome must be failed, got %q", entity.Outcome)
	}
	// The reason names the class; the error names the numbers (size, limit).
	for _, fact := range []string{"2097152", "1048576"} {
		if !strings.Contains(entity.Error, fact) {
			t.Fatalf("loop error must name size and limit; missing %q in %q", fact, entity.Error)
		}
	}

	// The COMPLETE_ failure event carries the typed reason for watchers.
	raw, ok := store.valueFor("COMPLETE_" + loopID)
	if !ok {
		t.Fatal("terminal failure must write COMPLETE_ so parents/rules see it")
	}
	var failure agentic.LoopFailedEvent
	if uerr := json.Unmarshal(raw, &failure); uerr != nil {
		t.Fatalf("unmarshal failure event: %v", uerr)
	}
	if failure.Reason != oversizedPublishReason {
		t.Fatalf("failure reason must be the typed %q, got %q", oversizedPublishReason, failure.Reason)
	}
}

// --- Blocker 3: the PUBLISHED terminal causally reflects persistence ---

// TestPersistHandlerResult_PersistFailure_PublishesNotDurableShape drives the
// completion path with a permanently failing COMPLETE_ write and asserts the
// event that GOES OUT is the explicit result-not-durable shape — metadata
// kept, result emptied, typed marker + classified reason — never a plain
// success whose durable record silently failed to land.
func TestPersistHandlerResult_PersistFailure_PublishesNotDurableShape(t *testing.T) {
	store := newFakeLoopsKV()
	store.failPrefix = "COMPLETE_"
	store.failTimes = -1
	store.failErr = permanentErr()
	c, loopID := newDurabilityComponent(t, store)

	if err := c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}

	completion := completionFor(loopID)
	completionMsg := message.NewBaseMessage(completion.Schema(), completion, "agentic-loop")
	inlineData, err := json.Marshal(completionMsg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	result := HandlerResult{
		LoopID:          loopID,
		State:           agentic.LoopStateComplete,
		CompletionState: completion,
		PublishedMessages: []PublishedMessage{{
			Subject: "agent.complete." + loopID,
			Data:    inlineData,
		}},
	}

	c.persistHandlerResult(context.Background(), result)

	var published agentic.LoopCompletedEvent
	decodePublishedPayload(t, result.PublishedMessages[0].Data, &published)
	if !published.ResultNotDurable {
		t.Fatal("published terminal event must carry result_not_durable=true after a permanent persist failure")
	}
	if published.ResultNotDurableReason == "" {
		t.Fatal("published not-durable event must name the classified cause")
	}
	if published.Result != "" {
		t.Fatal("published not-durable event must not carry the result the durable record disagrees with")
	}
	if published.LoopID != loopID || published.TaskID != "task-1" || published.Outcome != agentic.OutcomeSuccess {
		t.Fatalf("not-durable event must keep loop/task/outcome metadata; got %+v", published)
	}
}

// TestPersistHandlerResult_FailureState_WritesCompleteBeforePublish pins the
// third terminal lane the Blocker-3 reorder closed: a FailureState-carrying
// HandlerResult (failLoop path — max_iterations etc.) previously published
// its agent.failed event with NO COMPLETE_ write at all. Now the durable
// record lands, and on permanent persist failure the published event carries
// the typed marker.
func TestPersistHandlerResult_FailureState_WritesCompleteBeforePublish(t *testing.T) {
	store := newFakeLoopsKV()
	c, loopID := newDurabilityComponent(t, store)
	if err := c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}

	failure := &agentic.LoopFailedEvent{
		LoopID: loopID, TaskID: "task-1", Outcome: agentic.OutcomeFailed, Reason: "max_iterations",
	}
	result := HandlerResult{
		LoopID:       loopID,
		State:        agentic.LoopStateFailed,
		FailureState: failure,
	}
	c.persistHandlerResult(context.Background(), result)

	raw, ok := store.valueFor("COMPLETE_" + loopID)
	if !ok {
		t.Fatal("FailureState terminal lane must write COMPLETE_ (it previously published with no durable record)")
	}
	var stored agentic.LoopFailedEvent
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal COMPLETE_: %v", err)
	}
	if stored.Reason != "max_iterations" || stored.ResultNotDurable {
		t.Fatalf("durable failure record wrong shape: %+v", stored)
	}
}

func TestPersistHandlerResult_FailureStatePersistFails_PublishCarriesMarker(t *testing.T) {
	store := newFakeLoopsKV()
	store.failPrefix = "COMPLETE_"
	store.failTimes = -1
	store.failErr = permanentErr()
	c, loopID := newDurabilityComponent(t, store)
	if err := c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}

	failure := &agentic.LoopFailedEvent{
		LoopID: loopID, TaskID: "task-1", Outcome: agentic.OutcomeFailed, Reason: "max_iterations",
	}
	failureMsg := message.NewBaseMessage(failure.Schema(), failure, "agentic-loop")
	inlineData, err := json.Marshal(failureMsg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	result := HandlerResult{
		LoopID:       loopID,
		State:        agentic.LoopStateFailed,
		FailureState: failure,
		PublishedMessages: []PublishedMessage{{
			Subject: "agent.failed." + loopID,
			Data:    inlineData,
		}},
	}
	c.persistHandlerResult(context.Background(), result)

	var published agentic.LoopFailedEvent
	decodePublishedPayload(t, result.PublishedMessages[0].Data, &published)
	if !published.ResultNotDurable || published.ResultNotDurableReason == "" {
		t.Fatal("published failure event must carry the typed not-durable marker after a permanent persist failure")
	}
}

// TestFinalizeCancellation_PersistsBeforePublish proves the reorder
// behaviorally: the pre-fix code published FIRST and returned on publish
// failure without persisting, so a persisted COMPLETE_ under a FAILING
// publish is an outcome the old ordering cannot produce.
func TestFinalizeCancellation_PersistsBeforePublish(t *testing.T) {
	store := newFakeLoopsKV()
	c, loopID := newDurabilityComponent(t, store)

	// Conn-less client with a seeded limit: every publish fails with a
	// connection-state error, exactly the condition that previously skipped
	// persistence.
	client, err := natsclient.NewClient("nats://127.0.0.1:4222")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.SeedServerPayloadLimitForTest(1 << 20)
	c.natsClient = client

	completion := &agentic.LoopCancelledEvent{
		LoopID: loopID, TaskID: "task-1", Outcome: agentic.OutcomeCancelled, CancelledBy: "user",
	}
	published := c.finalizeCancellation(context.Background(), loopID, completion)

	raw, ok := store.valueFor("COMPLETE_" + loopID)
	if !ok {
		t.Fatal("COMPLETE_ must persist even when the publish fails — persist-before-publish")
	}
	var stored agentic.LoopCancelledEvent
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal COMPLETE_: %v", err)
	}
	if stored.ResultNotDurable {
		t.Fatal("a successfully persisted cancellation must not claim not-durable")
	}
	var wire agentic.LoopCancelledEvent
	decodePublishedPayload(t, published, &wire)
	if wire.ResultNotDurable {
		t.Fatal("published event must not carry the marker when persistence succeeded")
	}
}

func TestFinalizeCancellation_PersistFails_PublishedEventCarriesMarker(t *testing.T) {
	store := newFakeLoopsKV()
	store.failPrefix = "COMPLETE_"
	store.failTimes = -1
	store.failErr = permanentErr()
	c, loopID := newDurabilityComponent(t, store)

	completion := &agentic.LoopCancelledEvent{
		LoopID: loopID, TaskID: "task-1", Outcome: agentic.OutcomeCancelled, CancelledBy: "user",
	}
	published := c.finalizeCancellation(context.Background(), loopID, completion)
	if published == nil {
		t.Fatal("finalizeCancellation must still produce the publishable payload")
	}
	var wire agentic.LoopCancelledEvent
	decodePublishedPayload(t, published, &wire)
	if !wire.ResultNotDurable || wire.ResultNotDurableReason == "" {
		t.Fatal("published cancellation must carry the typed not-durable marker after a permanent persist failure")
	}
	if wire.LoopID != loopID || wire.Outcome != agentic.OutcomeCancelled || wire.CancelledBy != "user" {
		t.Fatalf("not-durable cancellation must keep its metadata; got %+v", wire)
	}
	// The loop entity carries the marker too, for readers that consult it.
	entity, _ := c.handler.GetLoop(loopID)
	if !entity.ResultNotDurable {
		t.Fatal("loop entity must carry the result-not-durable state")
	}
}

// decodePublishedPayload unwraps a published BaseMessage envelope through the
// PRODUCTION decoder path shape (envelope payload field) into out.
func decodePublishedPayload(t *testing.T, data []byte, out any) {
	t.Helper()
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if err := json.Unmarshal(envelope.Payload, out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
}

// TestPublishResults_OversizedTerminalPublish_DoesNotReFail pins the
// boundary: when the RESULT is already terminal (e.g. an agent.complete
// publish), an oversized refusal logs loudly but must not recurse into a
// second failure pass.
func TestPublishResults_OversizedTerminalPublish_DoesNotReFail(t *testing.T) {
	store := newFakeLoopsKV()
	c, loopID := newDurabilityComponent(t, store)

	client, err := natsclient.NewClient("nats://127.0.0.1:4222")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.SeedServerPayloadLimitForTest(1 << 20)
	c.natsClient = client

	if terr := c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete); terr != nil {
		t.Fatalf("TransitionLoop: %v", terr)
	}

	result := HandlerResult{
		LoopID: loopID,
		State:  agentic.LoopStateComplete,
		PublishedMessages: []PublishedMessage{{
			Subject: "agent.complete." + loopID,
			Data:    make([]byte, 2<<20),
		}},
	}
	c.publishResults(context.Background(), result)

	entity, _ := c.handler.GetLoop(loopID)
	if entity.State != agentic.LoopStateComplete {
		t.Fatalf("terminal loop must stay in its terminal state, got %s", entity.State)
	}
	if _, ok := store.valueFor("COMPLETE_" + loopID); ok {
		t.Fatal("no failure event should be written for an already-terminal result")
	}
}

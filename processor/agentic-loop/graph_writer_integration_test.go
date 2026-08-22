//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// tripleCollector subscribes to the canonical graph.mutation.triple.append
// operation. Per-request counters expose
// "how many NATS round-trips did the writer make" so atomicity-class
// tests can assert exact batch counts.
type tripleCollector struct {
	mu            sync.Mutex
	triples       []message.Triple
	batchRequests int
	batchSizes    []int
}

func (tc *tripleCollector) handler(_ context.Context, data []byte) ([]byte, error) {
	var req gtypes.AppendTriplesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	tc.mu.Lock()
	tc.triples = append(tc.triples, req.Triples...)
	tc.batchRequests++
	tc.batchSizes = append(tc.batchSizes, len(req.Triples))
	tc.mu.Unlock()

	seen := make(map[string]struct{})
	results := make([]gtypes.AppendSubjectResult, 0)
	for _, triple := range req.Triples {
		if _, ok := seen[triple.Subject]; ok {
			continue
		}
		seen[triple.Subject] = struct{}{}
		results = append(results, gtypes.AppendSubjectResult{
			EntityID: triple.Subject, Outcome: gtypes.MutationApplied, KVRevision: 1,
		})
	}
	return json.Marshal(gtypes.AppendTriplesResponse{Results: results})
}

// subscribeMutations wires the one canonical append subject.
func (tc *tripleCollector) subscribeMutations(t *testing.T, ctx context.Context, client *natsclient.Client) {
	t.Helper()
	if _, err := client.SubscribeForRequests(ctx, "graph.mutation.triple.append", tc.handler); err != nil {
		t.Fatalf("subscribe append: %v", err)
	}
}

func (tc *tripleCollector) getTriples() []message.Triple {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	out := make([]message.Triple, len(tc.triples))
	copy(out, tc.triples)
	return out
}

func (tc *tripleCollector) requestCounts() (single, batch int, batchSizes []int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	sizes := make([]int, len(tc.batchSizes))
	copy(sizes, tc.batchSizes)
	return 0, tc.batchRequests, sizes
}

func (tc *tripleCollector) predicateSet() map[string]bool {
	triples := tc.getTriples()
	s := make(map[string]bool, len(triples))
	for _, t := range triples {
		s[t.Predicate] = true
	}
	return s
}

// createResponder is a NATS responder for
// graph.mutation.entity.create requests.
// It captures the triples from the request body and replies with success
// (or with ErrorCodeEntityExists when alreadyExists is set, to exercise
// the idempotency path).
type createResponder struct {
	mu            sync.Mutex
	received      []gtypes.CreateEntityRequest
	alreadyExists bool   // when true, reply with ErrorCodeEntityExists
	failWith      string // when non-empty, reply with this error (and ErrorCodeInternal)
}

func (r *createResponder) handler(_ context.Context, data []byte) ([]byte, error) {
	var req gtypes.CreateEntityRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	if req.Entity == nil || len(req.Entity.Triples) != 0 {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, gtypes.ErrorCodeInvalidRequest,
			errors.New("entity.triples is not admitted; use top-level triples"))
	}

	r.mu.Lock()
	r.received = append(r.received, req)
	r.mu.Unlock()

	// ADR-060: mirror the production handler — a hard failure returns
	// (nil, *errs.ClassifiedError), which SubscribeForRequests turns into a
	// header-classified reply consumed by the canonical create client.
	switch {
	case r.alreadyExists:
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, gtypes.ErrorCodeEntityExists, errors.New("entity already exists"))
	case r.failWith != "":
		return nil, errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeInternal, errors.New(r.failWith))
	default:
		resp := gtypes.CreateEntityResponse{
			Outcome: gtypes.MutationApplied, Entity: req.Entity, KVRevision: 1,
		}
		return json.Marshal(resp)
	}
}

func (r *createResponder) subscribe(t *testing.T, ctx context.Context, client *natsclient.Client) {
	t.Helper()
	if _, err := client.SubscribeForRequests(ctx, "graph.mutation.entity.create", r.handler); err != nil {
		t.Fatalf("subscribe entity.create: %v", err)
	}
}

func (r *createResponder) getReceived() []gtypes.CreateEntityRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]gtypes.CreateEntityRequest, len(r.received))
	copy(out, r.received)
	return out
}

// newTestGraphWriter creates a graphWriter wired to a real NATS test client.
// Exported fields are accessed via the agenticloop package's NewGraphWriter constructor
// which we can't use from _test package, so we test via the exported Write* methods
// on the Component. Instead, we test the NATS round-trip by building a minimal
// graphWriter through the component's public API.
//
// Since graphWriter is unexported, we test the integration path through the
// exported component methods that delegate to it. For focused NATS I/O testing,
// we use a thin wrapper that exercises the same code path.

func TestWriteModelEndpoints_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// Model endpoints are BORN through canonical create carrying a model_endpoint
	// typed-origin envelope — NOT through append. An append to a never-created endpoint entity is must-exist-rejected by
	// graph-ingest ("kv: key not found"), which increments its error count and
	// flips it permanently unhealthy. Capture the create requests, not triples.
	responder := &createResponder{}
	responder.subscribe(t, ctx, tc.Client)

	// Build a model registry with two endpoints.
	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {
				Provider:               "anthropic",
				Model:                  "claude-opus-4-5",
				SupportsTools:          true,
				MaxTokens:              200000,
				InputPricePer1MTokens:  15.0,
				OutputPricePer1MTokens: 75.0,
			},
			"local": {
				Provider:      "ollama",
				Model:         "llama3.2",
				SupportsTools: false,
			},
		},
		Defaults: model.DefaultsConfig{Model: "claude"},
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, reg, types.PlatformMeta{Org: "acme", Platform: "ops"})
	w.WriteModelEndpoints(ctx)

	// One create request per endpoint.
	received := responder.getReceived()
	if len(received) != 2 {
		t.Fatalf("expected 2 create requests (one per endpoint), got %d", len(received))
	}

	wantType := agentic.ModelEndpointMessageType()
	var totalTriples int
	for _, req := range received {
		if req.Entity == nil {
			t.Fatal("create request has a nil Entity")
		}
		// Typed-origin envelope: the endpoint entity must be born with the
		// model_endpoint MessageType so graph-ingest creates it (not
		// envelope-less, not auto-vivified).
		if req.Entity.MessageType != wantType {
			t.Errorf("Entity.MessageType = %q, want %q", req.Entity.MessageType.Key(), wantType.Key())
		}
		if len(req.Entity.Triples) != 0 {
			t.Errorf("Entity.Triples must be empty on canonical create, got %d", len(req.Entity.Triples))
		}
		// Subject must be a valid 6-part entity ID.
		if !message.IsValidEntityID(req.Entity.ID) {
			t.Errorf("invalid entity ID: %q", req.Entity.ID)
		}
		// Every triple in the create body must belong to the entity being born.
		for _, tr := range req.Triples {
			if tr.Subject != req.Entity.ID {
				t.Errorf("triple subject %q != created entity ID %q", tr.Subject, req.Entity.ID)
			}
		}
		totalTriples += len(req.Triples)
	}

	// claude: 3 required (provider, name, supports_tools) + 3 optional
	// (max_tokens, input_price, output_price) = 6; local: 3 required = 3.
	// Total: 9 across both create bodies.
	if totalTriples < 9 {
		t.Errorf("expected at least 9 triples across endpoint create bodies, got %d", totalTriples)
	}
}

// Existing endpoint entities remain a definite create conflict. The component
// makes one attempt, logs its best-effort failure, and continues startup.
func TestWriteModelEndpoints_ExistingEntityAttemptsOnce_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	responder := &createResponder{alreadyExists: true}
	responder.subscribe(t, ctx, tc.Client)

	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {Provider: "anthropic", Model: "claude-opus-4-5"},
		},
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, reg, types.PlatformMeta{Org: "acme", Platform: "ops"})

	// Best-effort startup must not panic or retry automatically.
	w.WriteModelEndpoints(ctx)

	if got := len(responder.getReceived()); got != 1 {
		t.Errorf("expected 1 entity.create attempt, got %d", got)
	}
}

func TestWriteLoopCompletion_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)

	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {
				Model:                  "claude-opus-4-5",
				InputPricePer1MTokens:  15.0,
				OutputPricePer1MTokens: 75.0,
			},
		},
		Defaults: model.DefaultsConfig{Model: "claude"},
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, reg, types.PlatformMeta{Org: "acme", Platform: "ops"})

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-123",
		TaskID:       "task-abc",
		Outcome:      "success",
		Role:         "architect",
		Model:        "claude",
		Iterations:   5,
		TokensIn:     10000,
		TokensOut:    2000,
		ParentLoopID: "loop-parent",
		WorkflowSlug: "code-review",
		WorkflowStep: "draft",
		UserID:       "user-xyz",
		CompletedAt:  time.Now(),
	}

	w.WriteLoopCompletion(ctx, event, false)

	triples := collector.getTriples()
	preds := collector.predicateSet()

	// gh#159: completion stamp is the atomic-batch of 5 always-on +
	// model_used + cost = 7 predicates. Spawn-stamped predicates
	// (role, task, parent, workflow, workflow_step, user, description)
	// belong to WriteSpawnIdentity, not the completion stamp.
	completionRequired := []string{
		agvocab.LoopOutcome,
		agvocab.LoopIterations,
		agvocab.LoopTokensIn,
		agvocab.LoopTokensOut,
		agvocab.LoopEndedAt,
		agvocab.LoopModelUsed,
		agvocab.LoopCostUSD,
	}
	for _, pred := range completionRequired {
		if !preds[pred] {
			t.Errorf("expected %s in completion stamp", pred)
		}
	}

	// Verify the loop entity ID is valid.
	if len(triples) > 0 && !message.IsValidEntityID(triples[0].Subject) {
		t.Errorf("invalid loop entity ID: %q", triples[0].Subject)
	}

	// Spawn-stamped predicates MUST NOT appear in completion (would
	// duplicate after graph-ingest append).
	spawnOnly := []string{
		agvocab.LoopRole,
		agvocab.LoopTask,
		agvocab.LoopParent,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
		agvocab.LoopDescription,
	}
	for _, pred := range spawnOnly {
		if preds[pred] {
			t.Errorf("predicate %s leaked into completion stamp; should be spawn-only", pred)
		}
	}

	// gh#159 atomicity: all completion triples land in ONE batch
	// request, not per-triple writes. Single-request counter must be
	// zero; batch-request counter must be exactly one.
	single, batch, sizes := collector.requestCounts()
	if single != 0 {
		t.Errorf("expected zero single-subject append requests, got %d", single)
	}
	if batch != 1 {
		t.Errorf("expected exactly one batch request, got %d (sizes=%v)", batch, sizes)
	}
	if batch == 1 && sizes[0] != len(completionRequired) {
		t.Errorf("expected batch size %d, got %d", len(completionRequired), sizes[0])
	}
}

// TestWriteLoopCompletion_Integration_CapabilityResolves drives the #584 fix
// through the production NATS wire: a loop whose Model is a CAPABILITY name must
// stamp agent.loop.cost-usd AND record the RESOLVED endpoint in
// agent.loop.model-used (not the capability). Pre-fix, computeCost/
// ModelEndpointEntityID keyed the raw capability → no cost stamp and model-used
// pointing at the capability. The endpoint path is covered above; this closes the
// capability path end-to-end.
func TestWriteLoopCompletion_Integration_CapabilityResolves(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)

	reg := &model.Registry{
		Capabilities: map[string]*model.CapabilityConfig{
			"developer": {Preferred: []string{"big"}},
		},
		Endpoints: map[string]*model.EndpointConfig{
			"big": {
				Model:                  "claude-opus-4-5",
				InputPricePer1MTokens:  15.0,
				OutputPricePer1MTokens: 75.0,
			},
		},
		Defaults: model.DefaultsConfig{Model: "big"},
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, reg, types.PlatformMeta{Org: "acme", Platform: "ops"})

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loop-cap",
		TaskID:      "task-cap",
		Outcome:     "success",
		Role:        "developer",
		Model:       "developer", // a CAPABILITY name, not an endpoint
		Iterations:  3,
		TokensIn:    10000,
		TokensOut:   2000,
		CompletedAt: time.Now(),
	}

	w.WriteLoopCompletion(ctx, event, false)

	var modelUsed string
	var costStamped bool
	for _, tr := range collector.getTriples() {
		switch tr.Predicate {
		case agvocab.LoopModelUsed:
			if s, ok := tr.Object.(string); ok {
				modelUsed = s
			}
		case agvocab.LoopCostUSD:
			costStamped = true
			if f, ok := tr.Object.(float64); ok && f <= 0 {
				t.Errorf("cost-usd = %v, want > 0 for a priced capability loop", f)
			}
		}
	}

	wantModelID := agentic.ModelEndpointEntityID("acme", "ops", "big")      // resolved endpoint
	capModelID := agentic.ModelEndpointEntityID("acme", "ops", "developer") // the pre-fix (bug) value
	if modelUsed == "" {
		t.Fatal("model-used not stamped for a capability loop")
	}
	if modelUsed != wantModelID {
		t.Errorf("model-used = %q, want the RESOLVED endpoint ID %q (pre-fix bug stamped the capability %q)",
			modelUsed, wantModelID, capModelID)
	}
	if !costStamped {
		t.Error("cost-usd not stamped for a capability loop (the #584 bug)")
	}
}

func TestWriteLoopFailure_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	event := &agentic.LoopFailedEvent{
		LoopID:     "loop-fail",
		TaskID:     "task-fail",
		Outcome:    "failed",
		Role:       "editor",
		Model:      "claude",
		Iterations: 3,
		TokensIn:   500,
		TokensOut:  100,
		FailedAt:   time.Now(),
	}

	w.WriteLoopFailure(ctx, event, false)

	// gh#159: failure stamp is atomic-batch of 5 always-on + model_used
	// (nil registry → cost omitted) = 6 predicates.
	failureRequired := []string{
		agvocab.LoopOutcome,
		agvocab.LoopIterations,
		agvocab.LoopTokensIn,
		agvocab.LoopTokensOut,
		agvocab.LoopEndedAt,
		agvocab.LoopModelUsed,
	}
	preds := collector.predicateSet()
	for _, pred := range failureRequired {
		if !preds[pred] {
			t.Errorf("expected %s in failure stamp", pred)
		}
	}

	// Atomicity (gh#159).
	single, batch, sizes := collector.requestCounts()
	if single != 0 {
		t.Errorf("expected zero single-subject append requests, got %d", single)
	}
	if batch != 1 {
		t.Errorf("expected exactly one batch request, got %d (sizes=%v)", batch, sizes)
	}
}

// WriteSpawnIdentity births the loop-execution entity through canonical create
// so it has a typed origin contract (MessageType = agentic.loop_execution.v1)
// instead of relying on append. This test verifies:
//   - The request goes to graph.mutation.entity.create (not triple.append).
//   - The Entity.ID is the correct 6-part loop-execution entity ID.
//   - The MessageType key is "agentic.loop_execution.v1".
//   - The Triples body carries all expected spawn-identity predicates.
//   - Parent triple is a valid 6-part entity ID.
//   - The call returns nil (success) on a clean responder.
func TestWriteSpawnIdentity_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// Wire the create responder. The append collector is NOT wired — birth must
	// not use the append operation.
	responder := &createResponder{}
	responder.subscribe(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	task := &agentic.TaskMessage{
		TaskID:       "task-spawn-int",
		Role:         "researcher",
		ParentLoopID: "loop-parent-uuid",
		WorkflowSlug: "research",
		WorkflowStep: "gather",
		UserID:       "user-spawn",
		Prompt:       "Investigate MQTT retained messages",
	}

	if err := w.WriteSpawnIdentity(ctx, "loop-spawn-int", task); err != nil {
		t.Fatalf("WriteSpawnIdentity: unexpected error: %v", err)
	}

	received := responder.getReceived()
	if len(received) != 1 {
		t.Fatalf("expected exactly 1 create request, got %d", len(received))
	}
	req := received[0]

	// Entity ID must be the loop-execution 6-part ID.
	wantEntityID := "acme.ops.agent.agentic-loop.execution.loop-spawn-int"
	if req.Entity == nil {
		t.Fatal("Entity field is nil in create request")
	}
	if req.Entity.ID != wantEntityID {
		t.Errorf("Entity.ID = %q, want %q", req.Entity.ID, wantEntityID)
	}

	// MessageType must be agentic.loop_execution.v1.
	wantMsgType := "agentic.loop_execution.v1"
	if got := req.Entity.MessageType.Key(); got != wantMsgType {
		t.Errorf("MessageType.Key() = %q, want %q", got, wantMsgType)
	}
	if len(req.Entity.Triples) != 0 {
		t.Fatalf("Entity.Triples must be empty on canonical create, got %d", len(req.Entity.Triples))
	}

	// Verify the required predicates are present in the Triples body.
	predSet := make(map[string]bool, len(req.Triples))
	for _, tr := range req.Triples {
		predSet[tr.Predicate] = true
	}
	required := []string{
		agvocab.LoopRole,
		agvocab.LoopTask,
		agvocab.LoopParent,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
		agvocab.LoopDescription,
	}
	for _, pred := range required {
		if !predSet[pred] {
			t.Errorf("expected predicate %s in create request Triples body", pred)
		}
	}

	// Parent must be a valid 6-part entity ID.
	for _, tr := range req.Triples {
		if tr.Predicate != agvocab.LoopParent {
			continue
		}
		parent, ok := tr.Object.(string)
		if !ok {
			t.Fatal("LoopParent object is not a string")
		}
		if !message.IsValidEntityID(parent) {
			t.Errorf("LoopParent %q is not a valid 6-part entity ID", parent)
		}
		want := "acme.ops.agent.agentic-loop.execution.loop-parent-uuid"
		if parent != want {
			t.Errorf("LoopParent = %q, want %q", parent, want)
		}
	}
}

// A create conflict is definite and returned to the component. The framework
// does not issue a readback or automatically reinterpret it as success.
func TestWriteSpawnIdentity_EntityExistsReturnedToCaller_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()
	responder := &createResponder{alreadyExists: true}
	responder.subscribe(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
	err := w.WriteSpawnIdentity(ctx, "loop-conflict", &agentic.TaskMessage{TaskID: "task-conflict", Role: "researcher"})
	if err == nil {
		t.Fatal("expected definite entity.create conflict")
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Code != gtypes.ErrorCodeEntityExists {
		t.Fatalf("error = %#v, want code %q", classified, gtypes.ErrorCodeEntityExists)
	}
	if got := len(responder.getReceived()); got != 1 {
		t.Fatalf("create conflict requests = %d, want 1", got)
	}
}

// ADR-056 4c-pre-1: WriteSpawnIdentity returns an error on genuine birth failure
// (non-already-exists). The caller must be able to detect and halt.
func TestWriteSpawnIdentity_ReturnsErrorOnGenuineFailure_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	responder := &createResponder{failWith: "disk full"}
	responder.subscribe(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
	task := &agentic.TaskMessage{TaskID: "task-fail", Role: "researcher"}

	if err := w.WriteSpawnIdentity(ctx, "loop-fail", task); err == nil {
		t.Error("WriteSpawnIdentity should return an error on genuine failure, got nil")
	}
}

// ADR-056 4c-pre-1 / gh#159: WriteSpawnIdentity must no-op for a nil task
// without panicking (returns nil — caller already checked).
func TestWriteSpawnIdentity_NilTask_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	responder := &createResponder{}
	responder.subscribe(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
	if err := w.WriteSpawnIdentity(ctx, "loop-nil", nil); err != nil {
		t.Errorf("expected nil for nil task, got: %v", err)
	}

	if got := len(responder.getReceived()); got != 0 {
		t.Errorf("expected zero requests for nil task, got %d", got)
	}
}

// ADR-056 4c-pre-1: missing platform identity is a graceful SKIP (return nil,
// no request), NOT a birth failure — there is no valid 6-part entity ID to
// build, so there is nothing to birth. Matches every sibling graph-write method
// and the nil-client / nil-task / empty-triples guards. Regression guard: a
// missing-platform error here would hard-halt every loop in a degenerate/test
// config (the CI failure that surfaced this).
func TestWriteSpawnIdentity_MissingPlatformSkipsWithoutError_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	responder := &createResponder{}
	responder.subscribe(t, ctx, tc.Client)

	// Empty PlatformMeta — no org/platform → no valid entity ID.
	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{})
	task := &agentic.TaskMessage{TaskID: "task-noplat", Role: "researcher"}

	if err := w.WriteSpawnIdentity(ctx, "loop-noplat", task); err != nil {
		t.Errorf("missing platform identity must be a graceful skip (nil), got error: %v", err)
	}
	if got := len(responder.getReceived()); got != 0 {
		t.Errorf("missing platform identity must send zero create requests, got %d", got)
	}
}

func TestWriteLoopCancellation_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	event := &agentic.LoopCancelledEvent{
		LoopID:      "loop-cancel",
		TaskID:      "task-cancel",
		Outcome:     "cancelled",
		CancelledAt: time.Now(),
	}

	w.WriteLoopCancellation(ctx, event, false)

	preds := collector.predicateSet()
	// gh#159: cancellation stamp is the minimal transition signal
	// (outcome + ended_at). Task is spawn-only.
	if !preds[agvocab.LoopOutcome] {
		t.Error("expected agent.loop.outcome triple")
	}
	if !preds[agvocab.LoopEndedAt] {
		t.Error("expected agent.loop.ended_at triple")
	}
	if preds[agvocab.LoopTask] {
		t.Error("agent.loop.task leaked into cancellation stamp; should be spawn-only")
	}

	// Atomicity (gh#159).
	single, batch, sizes := collector.requestCounts()
	if single != 0 {
		t.Errorf("expected zero single-subject append requests, got %d", single)
	}
	if batch != 1 {
		t.Errorf("expected exactly one batch request, got %d (sizes=%v)", batch, sizes)
	}
}

func TestWriteLoopCompletion_NilClient_NoOp(t *testing.T) {
	w := agenticloop.NewGraphWriterForTest(nil, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loop-noop",
		TaskID:      "task-noop",
		Outcome:     "success",
		Role:        "editor",
		CompletedAt: time.Now(),
	}

	// Should not panic.
	w.WriteLoopCompletion(context.Background(), event, false)
}

func TestWriteModelEndpoints_MissingPlatform_NoOp(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)

	// Empty org/platform should skip writes.
	w := agenticloop.NewGraphWriterForTest(tc.Client, &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {Provider: "anthropic", Model: "claude-opus-4-5"},
		},
	}, types.PlatformMeta{})

	w.WriteModelEndpoints(ctx)

	triples := collector.getTriples()
	if len(triples) != 0 {
		t.Errorf("expected 0 triples with missing platform, got %d", len(triples))
	}
}

// TestWriteLineageTriples_Integration verifies the end-to-end NATS
// roundtrip: a RelatedLoops map (typed map[string]any after JSON
// round-trip) emits one agent.lineage.<role-key> triple per entry through
// graph.mutation.triple.append. The spawned-loop entity ID format and
// the predicate prefix are both contractual surfaces — drift breaks
// downstream rule substitution and ops-agent aggregation.
func TestWriteLineageTriples_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	related := map[string]any{
		"researcher": "loop-research-001",
		"planner":    "loop-plan-002",
	}
	if err := w.WriteLineageTriples(ctx, "architect-loop-xyz", related); err != nil {
		t.Fatal(err)
	}

	triples := collector.getTriples()
	if len(triples) != 2 {
		t.Fatalf("expected 2 lineage triples, got %d", len(triples))
	}

	// Both triples must point at the spawned loop's entity ID.
	wantSubject := agentic.LoopExecutionEntityID("acme", "ops", "architect-loop-xyz")
	for _, tr := range triples {
		if tr.Subject != wantSubject {
			t.Errorf("subject = %q, want %q", tr.Subject, wantSubject)
		}
		if !message.IsValidEntityID(tr.Subject) {
			t.Errorf("subject %q is not a valid 6-part entity ID", tr.Subject)
		}
	}

	preds := collector.predicateSet()
	for _, key := range []string{"researcher", "planner"} {
		want, err := agentic.LineageTriplePredicate(key)
		if err != nil {
			t.Fatal(err)
		}
		if !preds[want] {
			t.Errorf("missing predicate %q", want)
		}
	}
}

// TestWriteLineageTriples_NilClient_NoOp verifies no-panic on a
// missing NATS client (mirrors the nil-client safety the other
// writer methods carry).
func TestWriteLineageTriples_NilClient_NoOp(t *testing.T) {
	w := agenticloop.NewGraphWriterForTest(nil, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
	// Should be a no-op and not panic.
	if err := w.WriteLineageTriples(context.Background(), "any-loop", map[string]any{"researcher": "x"}); err != nil {
		t.Fatal(err)
	}
}

// TestWriteLineageTriples_MissingPlatformRejected verifies an invalid
// prospective subject fails before any graph I/O.
func TestWriteLineageTriples_MissingPlatformRejected(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{}) // no Org/Platform
	if err := w.WriteLineageTriples(ctx, "any-loop", map[string]any{"researcher": "x"}); err == nil {
		t.Fatal("WriteLineageTriples error = nil, want invalid subject rejection")
	}

	if got := len(collector.getTriples()); got != 0 {
		t.Errorf("missing platform identity should skip the write, got %d triples", got)
	}
}

func TestWriteMutationFailure_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// WriteModelEndpoints births endpoints through canonical create. A
	// hard failure (ADR-060 classified error, mirroring the production handler)
	// must be logged and swallowed per-endpoint — never panic, never abort the
	// remaining endpoints.
	responder := &createResponder{failWith: "test error"}
	responder.subscribe(t, ctx, tc.Client)

	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {Provider: "anthropic", Model: "claude-opus-4-5"},
		},
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, reg, types.PlatformMeta{Org: "acme", Platform: "ops"})

	// Should log warnings but not panic.
	w.WriteModelEndpoints(ctx)
}

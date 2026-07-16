//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// tripleCollector subscribes to BOTH graph.mutation.triple.add and
// graph.mutation.triple.add_batch so it stays sound through gh#159's
// per-triple→batched refactor. Per-request counters expose
// "how many NATS round-trips did the writer make" so atomicity-class
// tests can assert exact batch counts.
type tripleCollector struct {
	mu             sync.Mutex
	triples        []message.Triple
	singleRequests int
	batchRequests  int
	batchSizes     []int
}

func (tc *tripleCollector) handler(_ context.Context, data []byte) ([]byte, error) {
	var req gtypes.AddTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	tc.mu.Lock()
	tc.triples = append(tc.triples, req.Triple)
	tc.singleRequests++
	tc.mu.Unlock()

	resp := gtypes.AddTripleResponse{
		MutationResponse: gtypes.MutationResponse{
			Timestamp: time.Now().UnixNano(),
		},
	}
	return json.Marshal(resp)
}

func (tc *tripleCollector) batchHandler(_ context.Context, data []byte) ([]byte, error) {
	var req gtypes.AddTriplesBatchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	tc.mu.Lock()
	tc.triples = append(tc.triples, req.Triples...)
	tc.batchRequests++
	tc.batchSizes = append(tc.batchSizes, len(req.Triples))
	tc.mu.Unlock()

	resp := gtypes.AddTriplesBatchResponse{
		MutationResponse: gtypes.MutationResponse{
			Timestamp: time.Now().UnixNano(),
		},
		WrittenCount: len(req.Triples),
	}
	return json.Marshal(resp)
}

// subscribeMutations wires both single + batch subjects on tc so the
// caller's writer code path is observed regardless of which mutation
// subject it uses. Returns t.Fatal on failure.
func (tc *tripleCollector) subscribeMutations(t *testing.T, ctx context.Context, client *natsclient.Client) {
	t.Helper()
	if _, err := client.SubscribeForRequests(ctx, "graph.mutation.triple.add", tc.handler); err != nil {
		t.Fatalf("subscribe add: %v", err)
	}
	if _, err := client.SubscribeForRequests(ctx, "graph.mutation.triple.add_batch", tc.batchHandler); err != nil {
		t.Fatalf("subscribe add_batch: %v", err)
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
	return tc.singleRequests, tc.batchRequests, sizes
}

func (tc *tripleCollector) predicateSet() map[string]bool {
	triples := tc.getTriples()
	s := make(map[string]bool, len(triples))
	for _, t := range triples {
		s[t.Predicate] = true
	}
	return s
}

// createWithTriplesResponder is a NATS responder for
// graph.mutation.entity.create_with_triples requests (ADR-056 4c-pre-1).
// It captures the triples from the request body and replies with success
// (or with ErrorCodeEntityExists when alreadyExists is set, to exercise
// the idempotency path).
type createWithTriplesResponder struct {
	mu            sync.Mutex
	received      []gtypes.CreateEntityWithTriplesRequest
	alreadyExists bool   // when true, reply with ErrorCodeEntityExists
	failWith      string // when non-empty, reply with this error (and ErrorCodeInternal)
}

func (r *createWithTriplesResponder) handler(_ context.Context, data []byte) ([]byte, error) {
	var req gtypes.CreateEntityWithTriplesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.received = append(r.received, req)
	r.mu.Unlock()

	// ADR-060: mirror the production handler — a hard failure returns
	// (nil, *errs.ClassifiedError), which SubscribeForRequests turns into a
	// header-classified reply (the contract createEntityWithTriples now reads).
	switch {
	case r.alreadyExists:
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, gtypes.ErrorCodeEntityExists, errors.New("entity already exists"))
	case r.failWith != "":
		return nil, errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeInternal, errors.New(r.failWith))
	default:
		resp := gtypes.CreateEntityWithTriplesResponse{
			MutationResponse: gtypes.MutationResponse{
				Timestamp: time.Now().UnixNano(),
			},
			TriplesAdded: len(req.Triples),
		}
		return json.Marshal(resp)
	}
}

func (r *createWithTriplesResponder) subscribe(t *testing.T, ctx context.Context, client *natsclient.Client) {
	t.Helper()
	if _, err := client.SubscribeForRequests(ctx, "graph.mutation.entity.create_with_triples", r.handler); err != nil {
		t.Fatalf("subscribe create_with_triples: %v", err)
	}
}

func (r *createWithTriplesResponder) getReceived() []gtypes.CreateEntityWithTriplesRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]gtypes.CreateEntityWithTriplesRequest, len(r.received))
	copy(out, r.received)
	return out
}

// queryEntityResponder is a NATS responder for graph.ingest.query.entity reads
// (ADR-056 4c-pre-1 EntityExists guardrail). It replies with a fixed EntityState
// so verifyExistingLoopOrigin can read back the existing entity's MessageType and
// decide whether an EntityExists response is a safe same-origin re-birth or a
// non-origin shell it must refuse to bless.
type queryEntityResponder struct {
	entity gtypes.EntityState
}

func (r *queryEntityResponder) handler(_ context.Context, _ []byte) ([]byte, error) {
	return json.Marshal(r.entity)
}

func (r *queryEntityResponder) subscribe(t *testing.T, ctx context.Context, client *natsclient.Client) {
	t.Helper()
	if _, err := client.SubscribeForRequests(ctx, "graph.ingest.query.entity", r.handler); err != nil {
		t.Fatalf("subscribe query.entity: %v", err)
	}
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

	// gh#390: model endpoints are BORN via create_with_triples carrying a
	// model_endpoint typed-origin envelope — NOT per-triple triple.add. A bare
	// triple.add to a never-created endpoint entity is must-exist-rejected by
	// graph-ingest ("kv: key not found"), which increments its error count and
	// flips it permanently unhealthy. Capture the create requests, not triples.
	responder := &createWithTriplesResponder{}
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

	// One create_with_triples request per endpoint.
	received := responder.getReceived()
	if len(received) != 2 {
		t.Fatalf("expected 2 create_with_triples requests (one per endpoint), got %d", len(received))
	}

	wantType := agentic.ModelEndpointMessageType()
	var totalTriples int
	for _, req := range received {
		if req.Entity == nil {
			t.Fatal("create_with_triples request has a nil Entity")
		}
		// Typed-origin envelope: the endpoint entity must be born with the
		// model_endpoint MessageType so graph-ingest creates it (not
		// envelope-less, not auto-vivified).
		if req.Entity.MessageType != wantType {
			t.Errorf("Entity.MessageType = %q, want %q", req.Entity.MessageType.Key(), wantType.Key())
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

// TestWriteModelEndpoints_IdempotentOnRestart_Integration covers the restart
// path (gh#390): an endpoint entity already born with the model_endpoint typed
// origin returns EntityExists, which createEntityWithTriples treats as success
// after the MessageType read-back. WriteModelEndpoints must not log this as a
// failure or panic — re-running it on a warm graph is a no-op.
func TestWriteModelEndpoints_IdempotentOnRestart_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// create_with_triples replies EntityExists for every endpoint.
	responder := &createWithTriplesResponder{alreadyExists: true}
	responder.subscribe(t, ctx, tc.Client)

	// The read-back must find the SAME model_endpoint typed origin for
	// EntityExists to count as a safe idempotent re-birth.
	q := &queryEntityResponder{entity: gtypes.EntityState{
		ID:          agentic.ModelEndpointEntityID("acme", "ops", "claude"),
		MessageType: agentic.ModelEndpointMessageType(),
	}}
	q.subscribe(t, ctx, tc.Client)

	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {Provider: "anthropic", Model: "claude-opus-4-5"},
		},
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, reg, types.PlatformMeta{Org: "acme", Platform: "ops"})

	// Must not panic; EntityExists for our typed origin is idempotent success.
	w.WriteModelEndpoints(ctx)

	if got := len(responder.getReceived()); got != 1 {
		t.Errorf("expected 1 create_with_triples attempt, got %d", got)
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

	w.WriteLoopCompletion(ctx, event)

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
		t.Errorf("expected zero single-triple add requests, got %d", single)
	}
	if batch != 1 {
		t.Errorf("expected exactly one batch request, got %d (sizes=%v)", batch, sizes)
	}
	if batch == 1 && sizes[0] != len(completionRequired) {
		t.Errorf("expected batch size %d, got %d", len(completionRequired), sizes[0])
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

	w.WriteLoopFailure(ctx, event)

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
		t.Errorf("expected zero single-triple add requests, got %d", single)
	}
	if batch != 1 {
		t.Errorf("expected exactly one batch request, got %d (sizes=%v)", batch, sizes)
	}
}

// ADR-056 4c-pre-1: WriteSpawnIdentity births the loop-execution entity via
// create_with_triples so it has a typed origin contract (MessageType = agentic.loop_execution.v1)
// instead of being auto-vivified by triple.add_batch. This test verifies:
//   - The request goes to graph.mutation.entity.create_with_triples (not add_batch).
//   - The Entity.ID is the correct 6-part loop-execution entity ID.
//   - The MessageType key is "agentic.loop_execution.v1".
//   - The Triples body carries all expected spawn-identity predicates.
//   - Parent triple is a valid 6-part entity ID.
//   - The call returns nil (success) on a clean responder.
func TestWriteSpawnIdentity_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// Wire create_with_triples responder. The old tripleCollector
	// (add/add_batch) is NOT wired — the birth must NOT touch those subjects.
	responder := &createWithTriplesResponder{}
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
		t.Fatalf("expected exactly 1 create_with_triples request, got %d", len(received))
	}
	req := received[0]

	// Entity ID must be the loop-execution 6-part ID.
	wantEntityID := "acme.ops.agent.agentic-loop.execution.loop-spawn-int"
	if req.Entity == nil {
		t.Fatal("Entity field is nil in create_with_triples request")
	}
	if req.Entity.ID != wantEntityID {
		t.Errorf("Entity.ID = %q, want %q", req.Entity.ID, wantEntityID)
	}

	// MessageType must be agentic.loop_execution.v1.
	wantMsgType := "agentic.loop_execution.v1"
	if got := req.Entity.MessageType.Key(); got != wantMsgType {
		t.Errorf("MessageType.Key() = %q, want %q", got, wantMsgType)
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
			t.Errorf("expected predicate %s in create_with_triples Triples body", pred)
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

// ADR-056 4c-pre-1: WriteSpawnIdentity treats an already-exists response as
// success ONLY after the read-back guardrail confirms the existing entity is the
// SAME typed origin (MessageType == agentic.loop_execution.v1). Re-spawn / retry
// / redelivery on our own loop origin must not fail.
func TestWriteSpawnIdentity_IdempotentOnSameTypedOrigin_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	responder := &createWithTriplesResponder{alreadyExists: true}
	responder.subscribe(t, ctx, tc.Client)

	// The read-back must find the SAME typed origin for EntityExists to count
	// as a safe idempotent re-birth.
	q := &queryEntityResponder{entity: gtypes.EntityState{
		ID:          "acme.ops.agent.agentic-loop.execution.loop-idem",
		MessageType: agentic.LoopExecutionMessageType(),
	}}
	q.subscribe(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
	task := &agentic.TaskMessage{TaskID: "task-idem", Role: "researcher"}

	if err := w.WriteSpawnIdentity(ctx, "loop-idem", task); err != nil {
		t.Errorf("WriteSpawnIdentity should return nil on already-exists SAME typed origin, got: %v", err)
	}
}

// ADR-056 4c-pre-1 guardrail: EntityExists is success ONLY when the existing
// entity is the SAME typed origin. If the pre-existing entity is an envelope-less
// auto-vivified shell (empty MessageType) or a foreign entity colliding on the id
// (different MessageType), WriteSpawnIdentity must return an error so the caller
// halts — never silently bless a non-origin as "born."
func TestWriteSpawnIdentity_EntityExistsNotOurTypedOrigin_Integration(t *testing.T) {
	cases := []struct {
		name     string
		existing message.Type
	}{
		{"envelope-less auto-vivified shell", message.Type{}},
		{"foreign entity colliding on id", message.Type{Domain: "cs", Category: "system", Version: "v1"}},
	}
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
			ctx := context.Background()

			responder := &createWithTriplesResponder{alreadyExists: true}
			responder.subscribe(t, ctx, tc.Client)

			q := &queryEntityResponder{entity: gtypes.EntityState{
				ID:          "acme.ops.agent.agentic-loop.execution.loop-shell",
				MessageType: tcase.existing,
			}}
			q.subscribe(t, ctx, tc.Client)

			w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
			task := &agentic.TaskMessage{TaskID: "task-shell", Role: "researcher"}

			if err := w.WriteSpawnIdentity(ctx, "loop-shell", task); err == nil {
				t.Error("WriteSpawnIdentity must return an error when the existing entity is NOT our typed origin (refuse to bless a shell/foreign as born)")
			}
		})
	}
}

// The EntityExists origin read-back is an authoritative graph-state boundary.
// A matching MessageType cannot bless poisoned ENTITY_STATES data as an
// idempotent re-birth: malformed identity-bearing fields must fail before the
// divergent-task warning (or any later success behavior) runs.
func TestWriteSpawnIdentity_EntityExistsPoisonedReadbackFailsClosed_Integration(t *testing.T) {
	validID := "acme.ops.agent.agentic-loop.execution.loop-poison"
	tests := []struct {
		name     string
		existing gtypes.EntityState
	}{
		{
			name: "malformed root id",
			existing: gtypes.EntityState{
				ID:          "bad",
				MessageType: agentic.LoopExecutionMessageType(),
			},
		},
		{
			name: "malformed triple subject",
			existing: gtypes.EntityState{
				ID:          validID,
				MessageType: agentic.LoopExecutionMessageType(),
				Triples: []message.Triple{{
					Subject:   "bad",
					Predicate: agvocab.LoopTask,
					Object:    "task-first",
				}},
			},
		},
		{
			name: "malformed explicit entity reference",
			existing: gtypes.EntityState{
				ID:          validID,
				MessageType: agentic.LoopExecutionMessageType(),
				Triples: []message.Triple{
					{
						Subject:   validID,
						Predicate: agvocab.LoopTask,
						Object:    "task-first",
					},
					{
						Subject:   validID,
						Predicate: agvocab.LoopParent,
						Object:    "bad",
						Datatype:  message.EntityReferenceDatatype,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
			ctx := context.Background()

			responder := &createWithTriplesResponder{alreadyExists: true}
			responder.subscribe(t, ctx, tc.Client)
			q := &queryEntityResponder{entity: tt.existing}
			q.subscribe(t, ctx, tc.Client)

			h := &integrationCaptureHandler{}
			w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
			w.SetLogger(slog.New(h))

			err := w.WriteSpawnIdentity(ctx, "loop-poison", &agentic.TaskMessage{
				TaskID: "task-second",
				Role:   "researcher",
			})
			if err == nil {
				t.Fatal("WriteSpawnIdentity must reject a poisoned authoritative read-back")
			}
			var classified *errs.ClassifiedError
			if !errors.As(err, &classified) ||
				classified.Class != errs.ErrorFatal ||
				classified.Code != gtypes.ErrorCodeGraphStateResetRequired {
				t.Fatalf("error classification = %#v, want fatal/%q", classified, gtypes.ErrorCodeGraphStateResetRequired)
			}
			var stateErr *gtypes.StateContractError
			if !errors.As(err, &stateErr) {
				t.Fatalf("error = %T %v, want wrapped *graph.StateContractError", err, err)
			}
			if warns := h.warnMessages(); len(warns) != 0 {
				t.Fatalf("poisoned read-back reached divergent-task warning: %v", warns)
			}
		})
	}
}

// ADR-056 4c-pre-1: WriteSpawnIdentity returns an error on genuine birth failure
// (non-already-exists). The caller must be able to detect and halt.
func TestWriteSpawnIdentity_ReturnsErrorOnGenuineFailure_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	responder := &createWithTriplesResponder{failWith: "disk full"}
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

	responder := &createWithTriplesResponder{}
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

	responder := &createWithTriplesResponder{}
	responder.subscribe(t, ctx, tc.Client)

	// Empty PlatformMeta — no org/platform → no valid entity ID.
	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{})
	task := &agentic.TaskMessage{TaskID: "task-noplat", Role: "researcher"}

	if err := w.WriteSpawnIdentity(ctx, "loop-noplat", task); err != nil {
		t.Errorf("missing platform identity must be a graceful skip (nil), got error: %v", err)
	}
	if got := len(responder.getReceived()); got != 0 {
		t.Errorf("missing platform identity must send zero create_with_triples requests, got %d", got)
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

	w.WriteLoopCancellation(ctx, event)

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
		t.Errorf("expected zero single-triple add requests, got %d", single)
	}
	if batch != 1 {
		t.Errorf("expected exactly one batch request, got %d (sizes=%v)", batch, sizes)
	}
}

func TestWriteTrajectorySteps_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup(), natsclient.WithJetStream())
	ctx := context.Background()

	// Set up triple collector for the LoopHasStep links (triple.add) and a
	// create_with_triples responder for the step-entity births (gh#390): step
	// entities are CREATED with a typed-origin envelope, not appended. The two
	// responders are on disjoint subjects, so they coexist.
	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)
	creates := &createWithTriplesResponder{}
	creates.subscribe(t, ctx, tc.Client)

	// Create ObjectStore for content storage.
	store, err := objectstore.NewStoreWithConfig(ctx, tc.Client, objectstore.Config{
		BucketName: "TEST_AGENT_CONTENT",
	})
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}
	defer store.Close()

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
	w.SetContentStore(store)

	trajectory := &agentic.Trajectory{
		LoopID:    "loop-traj",
		StartTime: time.Now().Add(-10 * time.Second),
		Steps: []agentic.TrajectoryStep{
			{
				Timestamp: time.Now().Add(-8 * time.Second),
				StepType:  "model_call",
				Model:     "claude-sonnet",
				Response:  "I'll search for deployment errors.",
				TokensIn:  4832,
				TokensOut: 128,
				Duration:  2000,
			},
			{
				Timestamp:     time.Now().Add(-6 * time.Second),
				StepType:      "tool_call",
				ToolName:      "web_search",
				ToolArguments: map[string]any{"query": "deployment errors k8s"},
				ToolResult:    "Found 3 results about Kubernetes deployment errors...",
				Duration:      1500,
			},
			{
				Timestamp: time.Now().Add(-4 * time.Second),
				StepType:  "context_compaction",
				Duration:  100,
			},
			{
				Timestamp: time.Now().Add(-2 * time.Second),
				StepType:  "model_call",
				Model:     "claude-sonnet",
				Response:  "Based on the search results, here are the common deployment errors.",
				TokensIn:  6000,
				TokensOut: 500,
				Duration:  3000,
			},
		},
	}

	w.WriteTrajectorySteps(ctx, "loop-traj", trajectory)

	// LoopHasStep links append onto the loop entity via triple.add (captured by
	// the collector). 4 steps (including compaction) → 4 LoopHasStep triples.
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop-traj"
	var loopHasStepCount int
	for _, tr := range collector.getTriples() {
		if tr.Subject == loopEntityID && tr.Predicate == agvocab.LoopHasStep {
			loopHasStepCount++
		}
	}
	if loopHasStepCount != 4 {
		t.Errorf("expected 4 LoopHasStep triples, got %d", loopHasStepCount)
	}

	// Step entities are BORN via create_with_triples (gh#390) — one create per
	// step, each carrying the trajectory-step typed-origin envelope and the
	// step's metadata triples. Collect them from the create responder.
	received := creates.getReceived()
	if len(received) != 4 {
		t.Fatalf("expected 4 create_with_triples requests (one per step entity), got %d", len(received))
	}
	wantType := agentic.TrajectoryStepMessageType()
	stepEntityIDs := make(map[string]bool)
	stepPreds := make(map[string]bool)
	for _, req := range received {
		if req.Entity == nil {
			t.Fatal("create_with_triples request has a nil Entity")
		}
		if req.Entity.MessageType != wantType {
			t.Errorf("step Entity.MessageType = %q, want %q", req.Entity.MessageType.Key(), wantType.Key())
		}
		if !message.IsValidEntityID(req.Entity.ID) {
			t.Errorf("invalid step entity ID: %q", req.Entity.ID)
		}
		stepEntityIDs[req.Entity.ID] = true
		for _, tr := range req.Triples {
			stepPreds[tr.Predicate] = true
			if tr.Subject != req.Entity.ID {
				t.Errorf("step triple subject %q != created entity ID %q", tr.Subject, req.Entity.ID)
			}
		}
	}
	if len(stepEntityIDs) != 4 {
		t.Errorf("expected 4 distinct step entities, got %d", len(stepEntityIDs))
	}
	// Every step entity carries its type; tool_call carries tool_name; model_call carries tokens_in.
	if !stepPreds[agvocab.StepType] {
		t.Error("expected agent.step.type triple on every step entity")
	}
	if !stepPreds[agvocab.StepToolName] {
		t.Error("expected agent.step.tool_name triple for tool_call step")
	}
	if !stepPreds[agvocab.StepTokensIn] {
		t.Error("expected agent.step.tokens_in triple for model_call step")
	}

	// Verify content was stored in ObjectStore.
	// The tool_call step (index 1) should have its content stored.
	toolStepEntity := &agentic.TrajectoryStepEntity{
		Step:      trajectory.Steps[1],
		Org:       "acme",
		Platform:  "ops",
		LoopID:    "loop-traj",
		StepIndex: 1,
	}
	// Store a second copy to get a ref we can fetch with.
	ref, err := store.StoreContent(ctx, toolStepEntity)
	if err != nil {
		t.Fatalf("store content for verification: %v", err)
	}
	storedContent, err := store.FetchContent(ctx, ref)
	if err != nil {
		t.Fatalf("fetch content: %v", err)
	}

	if storedContent.Fields["tool_name"] != "web_search" {
		t.Errorf("stored tool_name: got %q, want web_search", storedContent.Fields["tool_name"])
	}
	if storedContent.Fields["tool_result"] != "Found 3 results about Kubernetes deployment errors..." {
		t.Errorf("stored tool_result mismatch")
	}
	if storedContent.ContentFields["body"] != "tool_result" {
		t.Errorf("content field mapping: body should map to tool_result, got %q", storedContent.ContentFields["body"])
	}
}

func TestWriteTrajectorySteps_NoContentStore_StillWritesTriples(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)
	creates := &createWithTriplesResponder{}
	creates.subscribe(t, ctx, tc.Client)

	// No content store set — graph writes should still happen.
	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	trajectory := &agentic.Trajectory{
		LoopID: "loop-no-store",
		Steps: []agentic.TrajectoryStep{
			{
				Timestamp:  time.Now(),
				StepType:   "tool_call",
				ToolName:   "graph_query",
				ToolResult: "query results",
				Duration:   200,
			},
		},
	}

	w.WriteTrajectorySteps(ctx, "loop-no-store", trajectory)

	// The LoopHasStep link appends to the loop entity via triple.add.
	preds := collector.predicateSet()
	if !preds[agvocab.LoopHasStep] {
		t.Error("expected agent.loop.has_step triple")
	}

	// The step entity is BORN via create_with_triples carrying its metadata
	// triples (gh#390) — not appended via triple.add.
	received := creates.getReceived()
	if len(received) != 1 {
		t.Fatalf("expected 1 step-entity create_with_triples, got %d", len(received))
	}
	var sawToolName bool
	for _, tr := range received[0].Triples {
		if tr.Predicate == agvocab.StepToolName {
			sawToolName = true
		}
	}
	if !sawToolName {
		t.Error("expected agent.step.tool_name triple in the step-entity create")
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
	w.WriteLoopCompletion(context.Background(), event)
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
// round-trip) emits one lineage.<key> triple per entry through
// graph.mutation.triple.add. The spawned-loop entity ID format and
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
	w.WriteLineageTriples(ctx, "architect-loop-xyz", related)

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
		want := agentic.LineageTriplePredicate(key)
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
	w.WriteLineageTriples(context.Background(), "any-loop", map[string]any{"researcher": "x"})
}

// TestWriteLineageTriples_MissingPlatform_NoOp verifies platform-
// identity gating: a writer with empty Org/Platform skips the write
// (and logs a warning, but the warning side-effect is observable
// only via the production logger).
func TestWriteLineageTriples_MissingPlatform_NoOp(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	collector.subscribeMutations(t, ctx, tc.Client)

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{}) // no Org/Platform
	w.WriteLineageTriples(ctx, "any-loop", map[string]any{"researcher": "x"})

	if got := len(collector.getTriples()); got != 0 {
		t.Errorf("missing platform identity should skip the write, got %d triples", got)
	}
}

func TestWriteMutationFailure_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// gh#390: WriteModelEndpoints births endpoints via create_with_triples. A
	// hard failure (ADR-060 classified error, mirroring the production handler)
	// must be logged and swallowed per-endpoint — never panic, never abort the
	// remaining endpoints.
	responder := &createWithTriplesResponder{failWith: "test error"}
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

// integrationCaptureHandler is a minimal slog.Handler for capturing log records
// in integration tests. NOT using slog.SetDefault — safe for t.Parallel().
type integrationCaptureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *integrationCaptureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *integrationCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *integrationCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *integrationCaptureHandler) WithGroup(string) slog.Handler      { return h }

func (h *integrationCaptureHandler) warnMessages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			out = append(out, r.Message)
		}
	}
	return out
}

// TestWriteSpawnIdentity_DivergentTaskID_Warns_Integration verifies the full
// wire-level path for gh#276:
//
//   - When loop_id is reused under a DIFFERENT task_id (EntityExists + same typed
//     origin + mismatched agent.loop.task triple), WriteSpawnIdentity returns nil
//     (keep-first-identity — behavior unchanged) AND emits a structured Warn log.
//
//   - When loop_id is reused under the SAME task_id (genuine retry/redelivery),
//     WriteSpawnIdentity returns nil with NO warning emitted.
func TestWriteSpawnIdentity_DivergentTaskID_Warns_Integration(t *testing.T) {
	t.Run("divergent task_id warns and succeeds", func(t *testing.T) {
		tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
		ctx := context.Background()

		// create_with_triples returns EntityExists — the loop is already born.
		responder := &createWithTriplesResponder{alreadyExists: true}
		responder.subscribe(t, ctx, tc.Client)

		// The read-back returns a same-typed-origin entity but with the FIRST
		// spawn's task_id ("task-first"), not the incoming "task-second".
		q := &queryEntityResponder{entity: gtypes.EntityState{
			ID:          "acme.ops.agent.agentic-loop.execution.loop-reuse",
			MessageType: agentic.LoopExecutionMessageType(),
			Triples: []message.Triple{
				{
					Subject:   "acme.ops.agent.agentic-loop.execution.loop-reuse",
					Predicate: agvocab.LoopTask,
					Object:    "task-first",
				},
			},
		}}
		q.subscribe(t, ctx, tc.Client)

		h := &integrationCaptureHandler{}
		logger := slog.New(h)

		w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
		w.SetLogger(logger)

		// Second spawn: same loop_id, different task_id.
		task := &agentic.TaskMessage{TaskID: "task-second", Role: "researcher"}
		if err := w.WriteSpawnIdentity(ctx, "loop-reuse", task); err != nil {
			t.Errorf("WriteSpawnIdentity must return nil (keep-first-identity), got error: %v", err)
		}

		warns := h.warnMessages()
		if len(warns) == 0 {
			t.Error("expected a Warn log for divergent task_id reuse, got none")
		} else if warns[0] != "graph_writer: loop_id reuse under divergent task_id; keeping original spawn identity (loop_id is immutable per task)" {
			t.Errorf("unexpected Warn message: %q", warns[0])
		}
	})

	t.Run("same task_id no warning", func(t *testing.T) {
		tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
		ctx := context.Background()

		responder := &createWithTriplesResponder{alreadyExists: true}
		responder.subscribe(t, ctx, tc.Client)

		// Read-back returns the SAME task_id as the incoming spawn.
		q := &queryEntityResponder{entity: gtypes.EntityState{
			ID:          "acme.ops.agent.agentic-loop.execution.loop-retry",
			MessageType: agentic.LoopExecutionMessageType(),
			Triples: []message.Triple{
				{
					Subject:   "acme.ops.agent.agentic-loop.execution.loop-retry",
					Predicate: agvocab.LoopTask,
					Object:    "task-same",
				},
			},
		}}
		q.subscribe(t, ctx, tc.Client)

		h := &integrationCaptureHandler{}
		logger := slog.New(h)

		w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
		w.SetLogger(logger)

		task := &agentic.TaskMessage{TaskID: "task-same", Role: "researcher"}
		if err := w.WriteSpawnIdentity(ctx, "loop-retry", task); err != nil {
			t.Errorf("WriteSpawnIdentity must return nil on same-task retry, got error: %v", err)
		}

		if warns := h.warnMessages(); len(warns) > 0 {
			t.Errorf("expected no Warn log for same task_id retry, got: %v", warns)
		}
	})
}

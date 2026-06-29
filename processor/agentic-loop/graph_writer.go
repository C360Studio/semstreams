package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	graphMutationSubject               = "graph.mutation.triple.add"
	graphMutationBatchSubject          = "graph.mutation.triple.add_batch"
	graphMutationCreateWithTriplesSubj = "graph.mutation.entity.create_with_triples"
	graphWriterTimeout                 = 5 * time.Second
	graphWriterSource                  = "agentic-loop"
	// syntheticDecideSource is the Source on triples emitted by
	// WriteSyntheticDecide (#133). Distinct from graphWriterSource
	// ("agentic-loop") and decideToolSource ("coordinator-decide") so
	// operator dashboards can attribute provenance — these triples come
	// from the framework's terminal-tool-less synthesis, not from a
	// model-emitted decide.
	syntheticDecideSource = "agentic-loop-synthetic-decide"
	// syntheticDecideReasonMaxBytes caps the size of the model's text
	// content carried in the synthetic decide's reason triple. Matches
	// maxPromptTripleBytes — the reason behaves like a prompt fragment
	// (BM25/NL-search-relevant text on the loop entity), not a payload.
	syntheticDecideReasonMaxBytes = 8 * 1024
	// syntheticDecideReasonPrefix marks the reason as framework-emitted
	// per the #133 contract. Downstream rules / operators can branch on
	// the prefix to distinguish model-emitted needs_clarification from
	// framework-synthesized ones; the coordinator.decision.synthetic
	// triple is the structured marker for rule matching.
	syntheticDecideReasonPrefix = "[synthetic-no-terminal] "

	// maxPromptTripleBytes caps the size of the user prompt stored as the
	// agent.loop.description triple, including the truncation marker. Full
	// prompts live elsewhere (loop state / ObjectStore); the triple only
	// needs enough text for BM25/NL search to match by topic.
	maxPromptTripleBytes = 8 * 1024

	// truncationMarker is appended to a truncated prompt so consumers can
	// tell at a glance that the triple is not the full text.
	truncationMarker = "…[truncated]"
)

// truncateForTriple returns s capped at maxBytes bytes total (including
// the truncation marker, if appended). It is UTF-8 safe — the cut point
// is walked back to the nearest rune boundary to avoid producing invalid UTF-8.
func truncateForTriple(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Reserve space for the marker so the final result stays within `maxBytes`.
	budget := maxBytes - len(truncationMarker)
	if budget <= 0 {
		// Pathological: cap is smaller than the marker itself. Return an
		// empty-ish truncation rather than something absurd.
		return truncationMarker[:maxBytes]
	}
	cut := budget
	// Walk backwards until `cut` indexes a rune-start byte. RuneStart
	// returns true for ASCII bytes and UTF-8 lead bytes; false for
	// continuation bytes. Slicing at a rune-start position keeps the
	// prefix valid UTF-8.
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + truncationMarker
}

// graphWriter emits graph triples for model endpoints and loop execution events
// via the NATS request/response mutation API.
type graphWriter struct {
	natsClient    *natsclient.Client
	modelRegistry model.RegistryReader
	platform      types.PlatformMeta
	logger        *slog.Logger
	contentStore  *objectstore.Store
}

// writeTriple marshals and sends a single triple via NATS request/response.
func (w *graphWriter) writeTriple(ctx context.Context, triple message.Triple) error {
	req := gtypes.AddTripleRequest{Triple: triple}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// RequestWithRetryClassified handles transient "no responders" errors that
	// happen when graph-gateway is restarting or the subscription
	// hasn't propagated to the NATS server yet. Without retry, a
	// single trajectory step on the boundary of a gateway restart
	// silently loses its triples — matches the pre-existing flake on
	// TestWriteTrajectorySteps_NoContentStore_StillWritesTriples
	// when -race + many test containers in flight delay subscription
	// readiness past the per-request timeout.
	// gh#93 Phase 2: Classified variant surfaces handler errors via err.
	respData, err := w.natsClient.RequestWithRetryClassified(ctx, graphMutationSubject, reqData, graphWriterTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("NATS request failed: %w", err)
	}

	// ADR-060: a handler failure arrives as the classified err above; the
	// legacy !resp.Success second check is gone. Decode only to confirm shape.
	var resp gtypes.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	return nil
}

// writeBatch marshals and sends a batch of triples atomically per-Subject
// via NATS request/response. Used by WriteSyntheticDecide (#133) — the
// three synthetic triples share one loop entity Subject so all-or-nothing
// CAS semantics keep downstream rule-matching consistent (next_action +
// reason + synthetic land together or not at all).
func (w *graphWriter) writeBatch(ctx context.Context, triples []message.Triple) error {
	if len(triples) == 0 {
		return nil
	}
	req := gtypes.AddTriplesBatchRequest{Triples: triples}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal batch request: %w", err)
	}

	// gh#93 Phase 2: Classified variant surfaces handler errors via err.
	respData, err := w.natsClient.RequestWithRetryClassified(ctx, graphMutationBatchSubject, reqData, graphWriterTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("NATS batch request failed: %w", err)
	}

	var resp gtypes.AddTriplesBatchResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal batch response: %w", err)
	}

	// ADR-060: a whole-batch failure arrives as the classified err above. A
	// PARTIAL batch (some subjects committed) returns a success body with
	// FailedSubjects populated (per-subject errors in the map) — surface it.
	if len(resp.FailedSubjects) > 0 {
		return fmt.Errorf("batch mutation partial failure (written=%d, failed=%v)",
			resp.WrittenCount, resp.FailedSubjects)
	}

	return nil
}

// createEntityWithTriples is the shared entity-birth primitive: it sends a
// CreateEntityWithTriplesRequest to graph.mutation.entity.create_with_triples via
// NATS request/reply, using the same transport config (timeout, retry) as
// writeBatch. Used to birth any typed-origin entity (loop executions via
// WriteSpawnIdentity, model endpoints via WriteModelEndpoints).
//
// Idempotency contract (ADR-056 typed-origin): a response with ErrorCode ==
// graph.ErrorCodeEntityExists is treated as success ONLY after a read-back
// confirms the existing entity is the SAME typed origin (MessageType) the creator
// intended — a genuine re-create / retry / re-spawn. If the pre-existing entity
// is an envelope-less auto-vivified shell or a foreign entity colliding on the
// id, EntityExists is a birth FAILURE, never a silent "born." See
// verifyExistingEntityOrigin.
//
// Returns a non-nil error only on genuine birth failures (transport error,
// handler-internal error, an EntityExists that is not our typed origin, any
// other non-success failure). The caller must not proceed as if graph semantics
// are intact when an error is returned.
func (w *graphWriter) createEntityWithTriples(ctx context.Context, entity *gtypes.EntityState, triples []message.Triple) error {
	req := gtypes.CreateEntityWithTriplesRequest{
		Entity:  entity,
		Triples: triples,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal create_with_triples request: %w", err)
	}

	// ADR-060: handler failures arrive as a classified error carrying the
	// stable Code (no in-body Success=false). The success body has no fields
	// this caller needs, so no response decode is required.
	if _, err := w.natsClient.RequestWithRetryClassified(ctx, graphMutationCreateWithTriplesSubj, reqData, graphWriterTimeout, natsclient.DefaultRetryConfig()); err != nil {
		// EntityExists is idempotent-success ONLY if the existing entity is
		// already this typed origin (same MessageType) — a genuine retry /
		// redelivery / re-spawn of OUR origin. Blessing any EntityExists blindly
		// would launder a pre-existing auto-vivified shell (Version-0 / no
		// envelope) or a foreign entity into a "born" loop origin, defeating the
		// typed-origin contract (ADR-056). Read + verify before treating it as born.
		var ce *errs.ClassifiedError
		if errors.As(err, &ce) && ce.Code == gtypes.ErrorCodeEntityExists {
			// Extract the incoming task_id from the create triples so
			// verifyExistingEntityOrigin can detect divergent-task_id reuse
			// (gh#276). Loop-execution entities carry agent.loop.task; non-loop
			// entities (e.g. model endpoints) do not, so incomingTaskID stays
			// empty and the divergent-task check no-ops — only the generic
			// typed-origin MessageType readback applies.
			var incomingTaskID string
			for _, t := range triples {
				if t.Predicate == agvocab.LoopTask {
					if s, ok := t.Object.(string); ok {
						incomingTaskID = s
					}
					break
				}
			}
			return w.verifyExistingEntityOrigin(ctx, entity.ID, entity.MessageType, incomingTaskID)
		}
		return fmt.Errorf("NATS create_with_triples request failed: %w", err)
	}

	return nil
}

// verifyExistingEntityOrigin guards the EntityExists idempotency path for ANY
// typed-origin entity: it reads the already-existing entity and confirms it
// carries the SAME typed origin MessageType the creator intended. A match means a
// safe idempotent re-birth (retry / redelivery / re-create / re-spawn). A
// mismatch (an envelope-less auto-vivified shell, or a foreign entity colliding
// on the id) or an unreadable entity is a birth FAILURE the caller halts on —
// never a silent "born." Reuses the same-package graph.ingest.query.entity read
// surface (see todos.go).
//
// loop_id immutability contract (gh#276) — applies only to loop-execution
// entities, which pass a non-empty incomingTaskID: a loop_id is bound to a single
// task_id at birth. A same-MessageType match is idempotent success — the first
// spawn's identity triples (role, task, prompt, parent) remain canonical and the
// new spawn's identity triples are NOT written (create_with_triples returns
// EntityExists before any merge). When incomingTaskID is non-empty and differs
// from the existing entity's agent.loop.task triple, this function emits a
// structured WARNING so operators can detect loop_id reuse across tasks. The
// identity is NOT rewritten: refusing a same-MessageType match would break
// genuine retry/redelivery, and a hard reject or identity-rewrite could silently
// break idempotency-reliant callers. The only safe action is to make the
// violation observable. This is strictly safer than the pre-4c-pre-1
// triple.add_batch path, which would have APPENDED conflicting role/parent
// values onto the existing entity.
//
// Non-loop callers (e.g. WriteModelEndpoints) pass an empty incomingTaskID, so
// the divergent-task_id check no-ops and only the generic MessageType readback
// applies — exactly the typed-origin guard those entities need.
func (w *graphWriter) verifyExistingEntityOrigin(ctx context.Context, entityID string, want message.Type, incomingTaskID string) error {
	reqData, err := json.Marshal(struct {
		ID string `json:"id"`
	}{ID: entityID})
	if err != nil {
		return fmt.Errorf("marshal origin-verify query for %s: %w", entityID, err)
	}
	respData, err := w.natsClient.RequestClassified(ctx, queryEntitySubject, reqData, graphWriterTimeout)
	if err != nil {
		// Cannot confirm the existing entity is our origin → do not bless it.
		return fmt.Errorf("create_with_triples returned entity_exists for %s but the read-back to verify the typed origin failed: %w", entityID, err)
	}
	var existing gtypes.EntityState
	if err := json.Unmarshal(respData, &existing); err != nil {
		return fmt.Errorf("create_with_triples entity_exists: unmarshal existing entity %s: %w", entityID, err)
	}
	if existing.MessageType != want {
		return fmt.Errorf("create_with_triples: %s already exists but is NOT a %s typed origin (existing message_type=%q) — refusing to bless a non-origin shell as born",
			entityID, want.Key(), existing.MessageType.Key())
	}

	// Same typed origin — idempotent re-birth. Now check for divergent task_id
	// (gh#276): if the caller supplied an incoming task_id and the existing entity
	// was born under a different task_id, the loop_id is being reused across tasks.
	// This is a producer contract violation. We keep the first spawn's identity
	// (loop_id is immutable per task) and emit a WARNING so operators can detect it.
	if existingTaskID, divergent := divergentTaskID(&existing, incomingTaskID); divergent {
		w.logger.Warn("graph_writer: loop_id reuse under divergent task_id; keeping original spawn identity (loop_id is immutable per task)",
			"loop_id", entityID,
			"existing_task_id", existingTaskID,
			"incoming_task_id", incomingTaskID,
		)
	}

	return nil // same typed origin — idempotent re-birth, safe to treat as born
}

// divergentTaskID reports whether the existing entity carries a different task_id
// than incomingTaskID. Returns the existing task_id and true when a divergence is
// detected — both values are empty / false when incomingTaskID is empty, the existing
// entity has no agent.loop.task triple, or the two task_ids match.
// Pure function; no NATS or clock side-effects.
func divergentTaskID(existing *gtypes.EntityState, incomingTaskID string) (existingTaskID string, divergent bool) {
	if incomingTaskID == "" {
		return "", false
	}
	triple := existing.GetTriple(agvocab.LoopTask)
	if triple == nil {
		return "", false
	}
	s, ok := triple.Object.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, s != incomingTaskID
}

// WriteSyntheticDecide stamps the synthetic decide triples (#133) onto
// the loop entity when handleCompleteResponse detected a terminal-tool-
// less completion. Three atomic triples on the loop entity:
//
//   - coordinator.decision.next_action = "needs_clarification" so
//     existing recovery rules (e.g. SemTeams' needs-clarification-
//     replan) match without changes.
//   - coordinator.decision.reason = "[synthetic-no-terminal] {model
//     text, truncated to 8KiB}" so operators can see the model's last
//     output that bypassed the terminal contract.
//   - coordinator.decision.synthetic = "true" so rule authors can
//     branch on framework-emitted vs model-emitted needs_clarification
//     (e.g. route synthetics straight to coordinator; model-emitted
//     through plan retry first).
//
// Routed through writeBatch so all three triples land atomically — a
// partial write would corrupt the rule-matching contract.
func (w *graphWriter) WriteSyntheticDecide(ctx context.Context, loopID, modelText string) {
	if w.natsClient == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write synthetic decide, platform identity missing",
			"loop_id", loopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}
	loopEntityID, err := agentic.TryLoopExecutionEntityID(w.platform.Org, w.platform.Platform, loopID)
	if err != nil {
		w.logger.Warn("graph_writer: cannot construct loop entity ID for synthetic decide",
			"loop_id", loopID, "error", err)
		return
	}

	now := time.Now()
	reason := syntheticDecideReasonPrefix + truncateForTriple(modelText, syntheticDecideReasonMaxBytes-len(syntheticDecideReasonPrefix))
	triples := []message.Triple{
		{
			Subject:    loopEntityID,
			Predicate:  agvocab.CoordinatorNextAction,
			Object:     "needs_clarification",
			Source:     syntheticDecideSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  agvocab.CoordinatorDecisionReason,
			Object:     reason,
			Source:     syntheticDecideSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  agvocab.CoordinatorDecisionSynthetic,
			Object:     "true",
			Source:     syntheticDecideSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
	}

	if err := w.writeBatch(ctx, triples); err != nil {
		w.logger.Warn("graph_writer: failed to write synthetic decide triples",
			"loop_id", loopID, "loop_entity_id", loopEntityID, "error", err)
		return
	}
	w.logger.Info("graph_writer: stamped synthetic decide on terminal-tool-less completion",
		"loop_id", loopID,
		"loop_entity_id", loopEntityID,
		"hint", "model returned text-only at completion — consider setting tool_choice='required' on the rule (#132) to prevent recurrence; high prevalence of this triple signals model/persona mismatch")
}

// WriteModelEndpoints births a graph entity for every endpoint in the model
// registry. Called on component startup so the graph reflects current endpoint
// configuration.
//
// Each endpoint entity is CREATED via create_with_triples carrying a
// model_endpoint typed-origin envelope (agentic.ModelEndpointMessageType) — NOT
// bare per-triple triple.add. A model endpoint is a config-derived fact whose
// entity must be born with a MessageType envelope, never auto-vivified:
// graph-ingest's triple.add enforces must-exist, so a per-triple write to a
// never-created endpoint entity is rejected ("kv: key not found"), increments
// graph-ingest's error count, and flips it permanently unhealthy (gh#390).
// create_with_triples is the correct write-API verb for entity creation
// (ADR-056 typed-origin; see WriteSpawnIdentity for the loop-execution analogue
// and the write-API taxonomy — metadata-less creation via triple.add is the
// defect class this fixes).
//
// Idempotent on restart: an EntityExists response for an endpoint already born
// with the same model_endpoint MessageType is treated as success by
// createEntityWithTriples (typed-origin readback). Per-endpoint failures are
// logged and do not abort the remaining endpoints — startup is best-effort,
// matching the pre-fix per-triple behaviour and every sibling graph-write method.
func (w *graphWriter) WriteModelEndpoints(ctx context.Context) {
	if w.natsClient == nil {
		return
	}
	if w.modelRegistry == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write model endpoints, platform identity missing",
			"org", w.platform.Org, "platform", w.platform.Platform)
		return
	}

	for _, name := range w.modelRegistry.ListEndpoints() {
		ep := w.modelRegistry.GetEndpoint(name)
		if ep == nil {
			continue
		}
		entityID := agentic.ModelEndpointEntityID(w.platform.Org, w.platform.Platform, name)
		triples := buildModelEndpointTriples(entityID, *ep)
		entity := &gtypes.EntityState{
			ID:          entityID,
			MessageType: agentic.ModelEndpointMessageType(),
			Triples:     triples,
		}
		if err := w.createEntityWithTriples(ctx, entity, triples); err != nil {
			w.logger.Warn("graph_writer: failed to create model endpoint entity",
				"endpoint", name, "entity_id", entityID, "error", err)
		}
	}
}

// WriteLoopCompletion emits triples for a successfully completed loop execution.
//
// All completion-path triples share the loop-execution entity Subject and
// are stamped atomically via writeBatch so the rule engine sees them in a
// single EntityState UPDATED event. Pre-fix behaviour (per-triple writes)
// produced one event per triple; a rule firing on agent.loop.outcome that
// substituted any other completion-path triple in its action would evaluate
// against a partial snapshot and bail. gh#159 + ADR-046 Phase 1 reference
// fan-out pattern depends on this atomicity.
func (w *graphWriter) WriteLoopCompletion(ctx context.Context, event *agentic.LoopCompletedEvent) {
	if w.natsClient == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write loop completion, platform identity missing",
			"loop_id", event.LoopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}

	loopEntityID := agentic.LoopExecutionEntityID(w.platform.Org, w.platform.Platform, event.LoopID)

	var modelEntityID string
	if event.Model != "" {
		modelEntityID = agentic.ModelEndpointEntityID(w.platform.Org, w.platform.Platform, event.Model)
	}

	cost := computeCost(w.modelRegistry, event.Model, event.TokensIn, event.TokensOut)

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, cost)
	if err := w.writeBatch(ctx, triples); err != nil {
		w.logger.Warn("graph_writer: failed to write loop completion batch",
			"loop_id", event.LoopID, "predicate_count", len(triples), "error", err)
	}
}

// WriteLoopFailure emits triples for a loop that terminated with an error.
//
// Atomic-batch stamp shape mirrors WriteLoopCompletion — see its godoc for
// the race-fix rationale (gh#159).
func (w *graphWriter) WriteLoopFailure(ctx context.Context, event *agentic.LoopFailedEvent) {
	if w.natsClient == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write loop failure, platform identity missing",
			"loop_id", event.LoopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}

	loopEntityID := agentic.LoopExecutionEntityID(w.platform.Org, w.platform.Platform, event.LoopID)

	var modelEntityID string
	if event.Model != "" {
		modelEntityID = agentic.ModelEndpointEntityID(w.platform.Org, w.platform.Platform, event.Model)
	}

	cost := computeCost(w.modelRegistry, event.Model, event.TokensIn, event.TokensOut)

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, cost)
	if err := w.writeBatch(ctx, triples); err != nil {
		w.logger.Warn("graph_writer: failed to write loop failure batch",
			"loop_id", event.LoopID, "predicate_count", len(triples), "error", err)
	}
}

// WriteTrajectorySteps stores step content in ObjectStore and emits graph triples
// for each trajectory step, linking them to the parent loop entity via LoopHasStep
// relationships.
func (w *graphWriter) WriteTrajectorySteps(ctx context.Context, loopID string, trajectory *agentic.Trajectory) {
	if w.natsClient == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write trajectory steps, platform identity missing",
			"loop_id", loopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}

	// Store content in ObjectStore for each step.
	if w.contentStore != nil && trajectory != nil {
		for i, step := range trajectory.Steps {
			entity := &agentic.TrajectoryStepEntity{
				Step:      step,
				Org:       w.platform.Org,
				Platform:  w.platform.Platform,
				LoopID:    loopID,
				StepIndex: i,
			}
			ref, err := w.contentStore.StoreContent(ctx, entity)
			if err != nil {
				w.logger.Warn("graph_writer: failed to store trajectory step content",
					"loop_id", loopID, "step_index", i, "step_type", step.StepType, "error", err)
				continue
			}
			entity.SetStorageRef(ref)
		}
	}

	loopEntityID := agentic.LoopExecutionEntityID(w.platform.Org, w.platform.Platform, loopID)
	triples := buildTrajectoryStepTriples(loopEntityID, w.platform.Org, w.platform.Platform, loopID, trajectory)

	// gh#390: the flat triple list mixes two subject kinds per step — the step
	// ENTITY's metadata triples (Subject = step entity ID, which must be
	// CREATED) and the LoopHasStep link (Subject = loop entity ID, which
	// already exists via WriteSpawnIdentity). graph-ingest enforces must-exist
	// on triple.add, so the step-entity triples must be born via
	// create_with_triples (a bare add lands "kv: key not found", increments
	// graph-ingest's error count, and flips it unhealthy). Group by subject and
	// route: the loop-entity subject is an APPEND (writeTriple); every other
	// subject is a step entity BIRTH (create_with_triples + typed-origin
	// envelope). Insertion order is preserved so emission stays deterministic.
	bySubject := make(map[string][]message.Triple)
	order := make([]string, 0, len(triples))
	for _, t := range triples {
		if _, seen := bySubject[t.Subject]; !seen {
			order = append(order, t.Subject)
		}
		bySubject[t.Subject] = append(bySubject[t.Subject], t)
	}
	for _, subject := range order {
		group := bySubject[subject]
		if subject == loopEntityID {
			// LoopHasStep links — append onto the already-born loop entity.
			for _, t := range group {
				if err := w.writeTriple(ctx, t); err != nil {
					w.logger.Warn("graph_writer: failed to write trajectory link triple",
						"loop_id", loopID, "predicate", t.Predicate, "error", err)
				}
			}
			continue
		}
		// Step entity — birth it with a trajectory-step typed-origin envelope.
		stepEntity := &gtypes.EntityState{
			ID:          subject,
			MessageType: agentic.TrajectoryStepMessageType(),
			Triples:     group,
		}
		if err := w.createEntityWithTriples(ctx, stepEntity, group); err != nil {
			w.logger.Warn("graph_writer: failed to create trajectory step entity",
				"loop_id", loopID, "step_entity_id", subject, "error", err)
		}
	}
}

// WriteLineageTriples emits cross-arc lineage triples on a spawned
// loop's entity from the RelatedLoops map threaded by the producer
// rule (rule.Action.RelatedLoops → TaskMessage.Metadata under
// agentic.MetadataKeyRelatedLoops). Each map entry produces one
// triple of the form:
//
//	subject:   <spawned loop entity ID>
//	predicate: lineage.<roleKey>           // via agentic.LineageTriplePredicate
//	object:    <upstream loop ID string>
//
// Downstream rules that fire on the spawned entity read these via
// the existing $entity.triple.<predicate> substitution, e.g.
// $entity.triple.lineage.researcher. No substitution-layer changes
// needed; multi-segment predicates already work.
//
// `related` is typed map[string]any because the Metadata round-trips
// through JSON (each value is a Go string, just typed as any).
// Non-string values are skipped — they should never appear given the
// rule.Action.RelatedLoops map[string]string type, but defensive
// skipping keeps a malformed product from polluting the graph.
//
// Failure handling: per-entry errors are logged and continue, matching
// the configureLoopMetadata + WriteLoopCompletion precedent. A failed
// write surfaces downstream as $entity.triple.lineage.X passing
// through the unresolvedTemplateVarRe warning — same shape as
// late-arriving triples.
func (w *graphWriter) WriteLineageTriples(ctx context.Context, loopID string, related map[string]any) {
	if w.natsClient == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write lineage triples, platform identity missing",
			"loop_id", loopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}
	if len(related) == 0 {
		return
	}

	loopEntityID := agentic.LoopExecutionEntityID(w.platform.Org, w.platform.Platform, loopID)
	triples := buildLineageTriples(loopEntityID, related)
	// Atomic batch on the loop entity so downstream rules firing on any
	// lineage.X triple see all sibling lineage.Y triples in the same
	// EntityState snapshot — same race-fix shape as WriteLoopCompletion
	// (gh#159).
	if err := w.writeBatch(ctx, triples); err != nil {
		w.logger.Warn("graph_writer: failed to write lineage batch",
			"loop_id", loopID, "predicate_count", len(triples), "error", err)
	}
}

// buildLineageTriples converts a RelatedLoops map into lineage triples
// on the spawned loop's entity. Pure (no NATS, no clock-injection
// support beyond now()) so it's straightforward to unit-test.
//
// Non-string values and empty strings are skipped: the producer-side
// type is map[string]string, so a non-string here means the wire
// format was tampered with or a non-rule-engine producer wrote
// malformed metadata. Either way, dropping is safer than emitting
// garbage triples.
func buildLineageTriples(loopEntityID string, related map[string]any) []message.Triple {
	if len(related) == 0 {
		return nil
	}
	now := time.Now()
	triples := make([]message.Triple, 0, len(related))
	for roleKey, raw := range related {
		loopIDStr, ok := raw.(string)
		if !ok || loopIDStr == "" {
			continue
		}
		triples = append(triples, message.Triple{
			Subject:    loopEntityID,
			Predicate:  agentic.LineageTriplePredicate(roleKey),
			Object:     loopIDStr,
			Source:     graphWriterSource,
			Timestamp:  now,
			Confidence: 1.0,
		})
	}
	return triples
}

// WriteSpawnIdentity births the loop-execution entity via a typed origin
// contract (ADR-056 W0 4c-pre-1). It constructs a LoopExecutionEntity from
// the spawn parameters, gets the full origin triple set from its Triples()
// method (IDENTICAL predicate set to the pre-4c-pre-1 buildSpawnIdentityTriples),
// and sends a synchronous create_with_triples request so the entity is born
// with a proper MessageType envelope rather than auto-vivified by triple.add_batch.
//
// Idempotency: an already-exists response is treated as success — re-spawn
// and retry are safe (the entity is born either way).
//
// Returns an error ONLY on genuine birth failure (the create_with_triples
// round-trip — transport error, a non-idempotent failure, or an EntityExists
// that is not our typed origin). The caller MUST NOT proceed as if graph
// semantics are intact when an error is returned — the loop must be halted.
// The pre-4c-pre-1 best-effort-Warn behaviour is now a hard precondition at
// the call site, but ONLY for that birth-failure class.
//
// No-op skips (return nil), matching the previous caller guards and every
// sibling graph-write method: nil natsClient, nil task, empty triples, and
// missing platform identity (no valid 6-part entity ID to build → nothing to
// birth). These are NOT failures and MUST NOT halt the loop.
func (w *graphWriter) WriteSpawnIdentity(ctx context.Context, loopID string, task *agentic.TaskMessage) error {
	if w.natsClient == nil {
		return nil
	}
	if task == nil {
		return nil
	}
	// Platform identity missing = there is no valid 6-part entity ID to build,
	// so there is NOTHING to birth — a graceful skip, NOT a birth failure. This
	// matches every sibling graph-write method (WriteSyntheticDecide /
	// WriteModelEndpoints / WriteLoopCompletion / WriteLoopFailure /
	// WriteTrajectory all Warn+return here) and the nil-client / nil-task /
	// empty-triples guards above. An ERROR from this method is reserved for a
	// genuine birth FAILURE (the create_with_triples round-trip below); only
	// that halts the loop at the caller.
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write spawn identity, platform identity missing",
			"loop_id", loopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return nil
	}

	entity := &agentic.LoopExecutionEntity{
		Org:      w.platform.Org,
		Platform: w.platform.Platform,
		LoopID:   loopID,
		Task:     task,
	}

	triples := entity.Triples()
	if len(triples) == 0 {
		return nil
	}

	entityState := &gtypes.EntityState{
		ID:          entity.EntityID(),
		MessageType: agentic.LoopExecutionMessageType(),
		Triples:     triples,
	}

	if err := w.createEntityWithTriples(ctx, entityState, triples); err != nil {
		return fmt.Errorf("spawn identity birth failed for loop %s: %w", loopID, err)
	}

	return nil
}

// WriteLoopCancellation emits triples for a loop that was cancelled.
//
// Atomic-batch stamp shape mirrors WriteLoopCompletion — see its godoc for
// the race-fix rationale (gh#159). Cancellation isn't a common rule join
// point today but the same race shape would apply if one is added.
func (w *graphWriter) WriteLoopCancellation(ctx context.Context, event *agentic.LoopCancelledEvent) {
	if w.natsClient == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write loop cancellation, platform identity missing",
			"loop_id", event.LoopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}

	loopEntityID := agentic.LoopExecutionEntityID(w.platform.Org, w.platform.Platform, event.LoopID)
	triples := buildLoopCancellationTriples(loopEntityID, event)
	if err := w.writeBatch(ctx, triples); err != nil {
		w.logger.Warn("graph_writer: failed to write loop cancellation batch",
			"loop_id", event.LoopID, "predicate_count", len(triples), "error", err)
	}
}

// --- pure triple builders (testable without NATS) ---

// buildModelEndpointTriples constructs the full set of triples describing a model endpoint.
// Optional fields are omitted when their zero value carries no information.
func buildModelEndpointTriples(entityID string, ep model.EndpointConfig) []message.Triple {
	now := time.Now()
	triple := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    entityID,
			Predicate:  predicate,
			Object:     object,
			Source:     graphWriterSource,
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	triples := []message.Triple{
		triple(agvocab.ModelProvider, ep.Provider),
		triple(agvocab.ModelName, ep.Model),
		triple(agvocab.ModelSupportsTools, ep.SupportsTools),
	}

	if ep.MaxTokens > 0 {
		triples = append(triples, triple(agvocab.ModelMaxTokens, ep.MaxTokens))
	}
	if ep.InputPricePer1MTokens > 0 {
		triples = append(triples, triple(agvocab.ModelInputPrice, ep.InputPricePer1MTokens))
	}
	if ep.OutputPricePer1MTokens > 0 {
		triples = append(triples, triple(agvocab.ModelOutputPrice, ep.OutputPricePer1MTokens))
	}
	if ep.URL != "" {
		triples = append(triples, triple(agvocab.ModelEndpointURL, ep.URL))
	}
	if ep.RequestsPerMinute > 0 {
		triples = append(triples, triple(agvocab.ModelRateLimit, ep.RequestsPerMinute))
	}

	return triples
}

// buildLoopCompletionTriples constructs triples for a successfully completed loop.
// cost should be pre-computed via computeCost; pass 0.0 to omit the cost triple.
//
// Spawn-known triples (role, task, parent, workflow, workflow_step, user,
// description) are stamped at spawn time by WriteSpawnIdentity (gh#159)
// and intentionally NOT re-emitted here — graph-ingest's AddTriples
// appends rather than upserts, so a second write would create duplicate
// triples on every completion. Completion-only triples below.
func buildLoopCompletionTriples(
	loopEntityID string,
	event *agentic.LoopCompletedEvent,
	modelEntityID string,
	cost float64,
) []message.Triple {
	now := time.Now()
	triple := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    loopEntityID,
			Predicate:  predicate,
			Object:     object,
			Source:     graphWriterSource,
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	triples := []message.Triple{
		triple(agvocab.LoopOutcome, event.Outcome),
		triple(agvocab.LoopIterations, event.Iterations),
		triple(agvocab.LoopTokensIn, event.TokensIn),
		triple(agvocab.LoopTokensOut, event.TokensOut),
		triple(agvocab.LoopEndedAt, event.CompletedAt.Format(time.RFC3339)),
	}

	if modelEntityID != "" {
		triples = append(triples, triple(agvocab.LoopModelUsed, modelEntityID))
	}
	if cost > 0 {
		triples = append(triples, triple(agvocab.LoopCostUSD, cost))
	}

	return triples
}

// buildLoopFailureTriples constructs triples for a loop that terminated with an error.
// cost should be pre-computed via computeCost; pass 0.0 to omit the cost triple.
//
// Spawn-known triples (role, task, parent, workflow, workflow_step, user,
// description) are stamped at spawn time by WriteSpawnIdentity (gh#159)
// and intentionally NOT re-emitted here — see buildLoopCompletionTriples
// for the upsert-vs-append rationale. Failure-path ancestry walks
// (semteams ADR-038 chainpause) read agent.loop.parent stamped at spawn.
func buildLoopFailureTriples(
	loopEntityID string,
	event *agentic.LoopFailedEvent,
	modelEntityID string,
	cost float64,
) []message.Triple {
	now := time.Now()
	triple := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    loopEntityID,
			Predicate:  predicate,
			Object:     object,
			Source:     graphWriterSource,
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	triples := []message.Triple{
		triple(agvocab.LoopOutcome, event.Outcome),
		triple(agvocab.LoopIterations, event.Iterations),
		triple(agvocab.LoopTokensIn, event.TokensIn),
		triple(agvocab.LoopTokensOut, event.TokensOut),
		triple(agvocab.LoopEndedAt, event.FailedAt.Format(time.RFC3339)),
	}

	if modelEntityID != "" {
		triples = append(triples, triple(agvocab.LoopModelUsed, modelEntityID))
	}
	if cost > 0 {
		triples = append(triples, triple(agvocab.LoopCostUSD, cost))
	}

	return triples
}

// buildLoopCancellationTriples constructs the minimal set of triples for a cancelled loop.
// Cancellation events carry less data than completion/failure — no model, no token counts.
//
// Spawn-known triples (task, workflow, workflow_step) live on the loop
// entity from WriteSpawnIdentity (gh#159); cancellation only writes the
// transition signals.
func buildLoopCancellationTriples(loopEntityID string, event *agentic.LoopCancelledEvent) []message.Triple {
	now := time.Now()
	triple := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    loopEntityID,
			Predicate:  predicate,
			Object:     object,
			Source:     graphWriterSource,
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	return []message.Triple{
		triple(agvocab.LoopOutcome, event.Outcome),
		triple(agvocab.LoopEndedAt, event.CancelledAt.Format(time.RFC3339)),
	}
}

// buildTrajectoryStepTriples constructs triples for all trajectory steps.
// Returns triples for both the step entities and LoopHasStep relationship triples
// on the loop entity. This is a pure function with no side effects.
func buildTrajectoryStepTriples(
	loopEntityID, org, platform, loopID string,
	trajectory *agentic.Trajectory,
) []message.Triple {
	if trajectory == nil || len(trajectory.Steps) == 0 {
		return nil
	}

	var allTriples []message.Triple

	for i, step := range trajectory.Steps {
		entity := &agentic.TrajectoryStepEntity{
			Step:      step,
			Org:       org,
			Platform:  platform,
			LoopID:    loopID,
			StepIndex: i,
		}

		// Add the step's metadata triples.
		allTriples = append(allTriples, entity.Triples()...)

		// Add LoopHasStep relationship triple on the loop entity.
		allTriples = append(allTriples, message.Triple{
			Subject:    loopEntityID,
			Predicate:  agvocab.LoopHasStep,
			Object:     entity.EntityID(),
			Source:     graphWriterSource,
			Timestamp:  step.Timestamp,
			Confidence: 1.0,
		})
	}

	return allTriples
}

// computeCost calculates loop cost from token counts and endpoint pricing.
// Returns 0.0 if the registry is nil, the endpoint is unknown, or pricing is not configured.
func computeCost(reg model.RegistryReader, endpointName string, tokensIn, tokensOut int) float64 {
	if reg == nil {
		return 0
	}
	ep := reg.GetEndpoint(endpointName)
	if ep == nil {
		return 0
	}
	return float64(tokensIn)*ep.InputPricePer1MTokens/1_000_000 +
		float64(tokensOut)*ep.OutputPricePer1MTokens/1_000_000
}

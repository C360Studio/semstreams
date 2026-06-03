package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	graphMutationSubject      = "graph.mutation.triple.add"
	graphMutationBatchSubject = "graph.mutation.triple.add_batch"
	graphWriterTimeout        = 5 * time.Second
	graphWriterSource         = "agentic-loop"
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

	// RequestWithRetry handles transient "no responders" errors that
	// happen when graph-gateway is restarting or the subscription
	// hasn't propagated to the NATS server yet. Without retry, a
	// single trajectory step on the boundary of a gateway restart
	// silently loses its triples — matches the pre-existing flake on
	// TestWriteTrajectorySteps_NoContentStore_StillWritesTriples
	// when -race + many test containers in flight delay subscription
	// readiness past the per-request timeout.
	respData, err := w.natsClient.RequestWithRetry(ctx, graphMutationSubject, reqData, graphWriterTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("NATS request failed: %w", err)
	}

	var resp gtypes.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("mutation failed: %s", resp.Error)
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

	respData, err := w.natsClient.RequestWithRetry(ctx, graphMutationBatchSubject, reqData, graphWriterTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("NATS batch request failed: %w", err)
	}

	var resp gtypes.AddTriplesBatchResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal batch response: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("batch mutation failed: %s", resp.Error)
	}

	return nil
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

// WriteModelEndpoints emits triples for every endpoint in the model registry.
// Called on component startup so the graph reflects current endpoint configuration.
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
		for _, t := range triples {
			if err := w.writeTriple(ctx, t); err != nil {
				w.logger.Warn("graph_writer: failed to write model endpoint triple",
					"endpoint", name, "predicate", t.Predicate, "error", err)
			}
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
	for _, t := range triples {
		if err := w.writeTriple(ctx, t); err != nil {
			w.logger.Warn("graph_writer: failed to write trajectory step triple",
				"loop_id", loopID, "predicate", t.Predicate, "error", err)
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

// WriteSpawnIdentity stamps the loop's canonical identity triples on
// its execution entity at spawn time. These predicates are known the
// moment the TaskMessage arrives at the loop component, so deferring
// them to completion (the pre-gh#159 shape) creates two problems:
//
//   - Mid-loop reads can't observe them. Trajectory inspection,
//     ancestry walks from a still-running child, and rules firing
//     on intermediate signals all see the loop entity without
//     these attributes until completion.
//   - Completion-path stamping arrives staggered across N events
//     (per-triple writes), letting rules fire on agent.loop.outcome
//     before agent.loop.parent lands — the gh#159 race that breaks
//     ADR-046 Phase 1's example-fan-out reference pattern.
//
// Atomic-batch stamp on the loop entity. Conditional triples (parent,
// workflow, workflow_step, user, description) are omitted when their
// TaskMessage source field is empty so the entity isn't polluted with
// zero-value triples.
//
// Failure handling matches WriteLineageTriples: log and continue. A
// failed stamp surfaces downstream as $entity.triple.agent.loop.parent
// (etc.) tripping the unresolvedTemplateVarRe warning — same shape
// rule authors already debug.
func (w *graphWriter) WriteSpawnIdentity(ctx context.Context, loopID string, task *agentic.TaskMessage) {
	if w.natsClient == nil {
		return
	}
	if task == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write spawn identity, platform identity missing",
			"loop_id", loopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}

	loopEntityID := agentic.LoopExecutionEntityID(w.platform.Org, w.platform.Platform, loopID)
	triples := buildSpawnIdentityTriples(loopEntityID, task, w.platform.Org, w.platform.Platform)
	if len(triples) == 0 {
		return
	}
	if err := w.writeBatch(ctx, triples); err != nil {
		w.logger.Warn("graph_writer: failed to write spawn identity batch",
			"loop_id", loopID, "predicate_count", len(triples), "error", err)
	}
}

// buildSpawnIdentityTriples constructs the spawn-time identity triples
// on the loop-execution entity. Pure (clock-injection via time.Now is
// the same shape as buildLoopCompletionTriples), so it's unit-testable
// without NATS.
//
// Always emits: agent.loop.role, agent.loop.task. Conditionally emits
// parent, workflow, workflow_step, user, description when the
// corresponding TaskMessage field is non-empty.
func buildSpawnIdentityTriples(loopEntityID string, task *agentic.TaskMessage, org, platform string) []message.Triple {
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

	triples := make([]message.Triple, 0, 7)
	if task.Role != "" {
		triples = append(triples, triple(agvocab.LoopRole, task.Role))
	}
	if task.TaskID != "" {
		triples = append(triples, triple(agvocab.LoopTask, task.TaskID))
	}
	if task.ParentLoopID != "" {
		parentEntityID := agentic.LoopExecutionEntityID(org, platform, task.ParentLoopID)
		triples = append(triples, triple(agvocab.LoopParent, parentEntityID))
	}
	if task.WorkflowSlug != "" {
		triples = append(triples, triple(agvocab.LoopWorkflow, task.WorkflowSlug))
	}
	if task.WorkflowStep != "" {
		triples = append(triples, triple(agvocab.LoopWorkflowStep, task.WorkflowStep))
	}
	if task.UserID != "" {
		triples = append(triples, triple(agvocab.LoopUser, task.UserID))
	}
	if task.Prompt != "" {
		triples = append(triples, triple(agvocab.LoopDescription, truncateForTriple(task.Prompt, maxPromptTripleBytes)))
	}
	return triples
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

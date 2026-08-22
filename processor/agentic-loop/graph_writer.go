package agenticloop

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	graphWriterTimeout = 5 * time.Second
	graphWriterSource  = "agentic-loop"
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

	// evidenceIntegrityIncomplete is the ONLY value ever written for
	// agvocab.LoopEvidenceIntegrity, and this constant is its only home so
	// "does any path write a completeness claim?" is answerable by grep.
	// The framework can observe the audit failures it saw and nothing more,
	// so there is no "complete" counterpart: absence of the triple means
	// only that no loss was observed (ADR-084).
	evidenceIntegrityIncomplete = "incomplete"
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
}

// writeTriple appends one triple through the canonical mutation port.
func (w *graphWriter) writeTriple(ctx context.Context, triple message.Triple) error {
	return w.writeBatch(ctx, []message.Triple{triple})
}

// writeBatch appends triples once through the canonical mutation port. The
// caller decides whether and when a definite or ambiguous failure is retried.
func (w *graphWriter) writeBatch(ctx context.Context, triples []message.Triple) error {
	if len(triples) == 0 {
		return nil
	}
	client, err := graphmutation.NewClient(w.natsClient, graphWriterTimeout)
	if err != nil {
		return fmt.Errorf("build graph mutation client: %w", err)
	}
	response, err := client.Append(ctx, gtypes.AppendTriplesRequest{Triples: triples})
	if err != nil {
		return fmt.Errorf("append graph triples: %w", err)
	}
	for _, result := range response.Results {
		switch result.Outcome {
		case gtypes.MutationApplied, gtypes.MutationUnchanged:
			continue
		case gtypes.MutationFailed:
			return fmt.Errorf("append graph triples for %s: %s/%s",
				result.EntityID, result.Error.Class, result.Error.Code)
		default:
			return fmt.Errorf("append graph triples for %s: %s", result.EntityID, result.Outcome)
		}
	}
	return nil
}

// createEntityWithTriples performs one atomic canonical birth. Existing entities
// are a definite conflict; this transport does not turn a readback into success.
func (w *graphWriter) createEntityWithTriples(ctx context.Context, entity *gtypes.EntityState, triples []message.Triple) error {
	client, err := graphmutation.NewClient(w.natsClient, graphWriterTimeout)
	if err != nil {
		return fmt.Errorf("build graph mutation client: %w", err)
	}
	if _, err := client.Create(ctx, gtypes.CreateEntityRequest{Entity: entity, Triples: triples}); err != nil {
		return fmt.Errorf("create graph entity: %w", err)
	}
	return nil
}

// WriteSyntheticDecide stamps the synthetic decide triples (#133) onto
// the loop entity when handleCompleteResponse detected a terminal-tool-
// less completion. Three atomic triples on the loop entity:
//
//   - coordinator.decision.next-action = "needs_clarification" so
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
// Each endpoint entity is born via entity.create carrying a model_endpoint
// typed-origin envelope (agentic.ModelEndpointMessageType). A model endpoint
// is a config-derived fact whose entity must be born with a MessageType
// envelope. graph-ingest's append operation enforces must-exist, so a write to a
// never-created endpoint entity is rejected ("kv: key not found"), increments
// graph-ingest's error count, and flips it permanently unhealthy (gh#390).
// Entity create is therefore the correct operation (ADR-091; see
// WriteSpawnIdentity for the loop-execution analogue).
//
// An existing endpoint is handled explicitly by this component. Per-endpoint failures are
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
		ep := w.modelRegistry.GetEndpoint(name) // modelresolveaudit:allow list-iteration (ListEndpoints yields real endpoint names)
		if ep == nil {
			continue
		}
		entityID := agentic.ModelEndpointEntityID(w.platform.Org, w.platform.Platform, name)
		triples := buildModelEndpointTriples(entityID, *ep)
		entity := &gtypes.EntityState{
			ID:          entityID,
			MessageType: agentic.ModelEndpointMessageType(),
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
func (w *graphWriter) WriteLoopCompletion(ctx context.Context, event *agentic.LoopCompletedEvent, evidenceIncomplete bool) {
	if w.natsClient == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write loop completion, platform identity missing",
			"loop_id", event.LoopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}

	loopEntityID := agentic.LoopExecutionEntityID(w.platform.Org, w.platform.Platform, event.LoopID)

	modelEntityID, cost := resolveModelAccounting(
		w.modelRegistry, w.platform.Org, w.platform.Platform, event.Model, event.TokensIn, event.TokensOut)

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, cost, evidenceIncomplete)
	if err := w.writeBatch(ctx, triples); err != nil {
		w.logger.Warn("graph_writer: failed to write loop completion batch",
			"loop_id", event.LoopID, "predicate_count", len(triples), "error", err)
	}
}

// WriteLoopFailure emits triples for a loop that terminated with an error.
//
// Atomic-batch stamp shape mirrors WriteLoopCompletion — see its godoc for
// the race-fix rationale (gh#159).
func (w *graphWriter) WriteLoopFailure(ctx context.Context, event *agentic.LoopFailedEvent, evidenceIncomplete bool) {
	if w.natsClient == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write loop failure, platform identity missing",
			"loop_id", event.LoopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}

	loopEntityID := agentic.LoopExecutionEntityID(w.platform.Org, w.platform.Platform, event.LoopID)

	modelEntityID, cost := resolveModelAccounting(
		w.modelRegistry, w.platform.Org, w.platform.Platform, event.Model, event.TokensIn, event.TokensOut)

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, cost, evidenceIncomplete)
	if err := w.writeBatch(ctx, triples); err != nil {
		w.logger.Warn("graph_writer: failed to write loop failure batch",
			"loop_id", event.LoopID, "predicate_count", len(triples), "error", err)
	}
}

// WriteLineageTriples emits cross-arc lineage triples on a spawned
// loop's entity from the RelatedLoops map threaded by the producer
// rule (rule.Action.RelatedLoops → TaskMessage.Metadata under
// agentic.MetadataKeyRelatedLoops). Each map entry produces one
// triple of the form:
//
//	subject:   <spawned loop entity ID>
//	predicate: agent.lineage.<role-key> // via agentic.LineageTriplePredicate
//	object:    <upstream loop ID string>
//
// Downstream rules that fire on the spawned entity read these via
// the existing $entity.triple.<predicate> substitution, e.g.
// $entity.triple.agent.lineage.researcher. No substitution-layer changes
// needed; multi-segment predicates already work.
//
// `related` is typed map[string]any because the Metadata round-trips
// through JSON (each value is a Go string, just typed as any).
// The complete candidate batch is validated before any I/O. Malformed
// metadata is returned as a typed invalid error; no entry is skipped.
func (w *graphWriter) WriteLineageTriples(ctx context.Context, loopID string, related map[string]any) error {
	if len(related) == 0 {
		return nil
	}

	loopEntityID, err := agentic.TryLoopExecutionEntityID(w.platform.Org, w.platform.Platform, loopID)
	if err != nil {
		return errs.WrapInvalid(err, "agentic-loop", "WriteLineageTriples", "construct lineage subject")
	}
	triples, err := buildLineageTriples(loopEntityID, related)
	if err != nil {
		return errs.WrapInvalid(err, "agentic-loop", "WriteLineageTriples", "preflight lineage batch")
	}
	if w.natsClient == nil {
		return nil
	}
	// Atomic batch on the loop entity so downstream rules firing on any
	// agent.lineage.<role-key> triple see all sibling agent.lineage.<role-key>
	// triples in the same
	// EntityState snapshot — same race-fix shape as WriteLoopCompletion
	// (gh#159).
	if err := w.writeBatch(ctx, triples); err != nil {
		return fmt.Errorf("write lineage batch: %w", err)
	}
	return nil
}

// buildLineageTriples converts a RelatedLoops map into lineage triples
// on the spawned loop's entity. Pure (no NATS, no clock-injection
// support beyond now()) so it's straightforward to unit-test.
//
// The builder is an all-or-nothing preflight. One invalid subject, role key,
// value type, empty value, or constructed predicate returns an error and no
// triples; callers must not silently drop malformed entries.
func buildLineageTriples(loopEntityID string, related map[string]any) ([]message.Triple, error) {
	if len(related) == 0 {
		return nil, nil
	}
	if !message.IsValidEntityID(loopEntityID) {
		return nil, fmt.Errorf("lineage subject %q is not a valid entity ID", loopEntityID)
	}
	keys := make([]string, 0, len(related))
	for roleKey := range related {
		keys = append(keys, roleKey)
	}
	sort.Strings(keys)
	type lineageValue struct {
		predicate string
		loopID    string
	}
	values := make([]lineageValue, 0, len(keys))
	for _, roleKey := range keys {
		predicate, err := agentic.LineageTriplePredicate(roleKey)
		if err != nil {
			return nil, fmt.Errorf("lineage role key %q: %w", roleKey, err)
		}
		loopID, ok := related[roleKey].(string)
		if !ok {
			return nil, fmt.Errorf("lineage role key %q loop ID must be a string, got %T", roleKey, related[roleKey])
		}
		if loopID == "" {
			return nil, fmt.Errorf("lineage role key %q loop ID must not be empty", roleKey)
		}
		values = append(values, lineageValue{predicate: predicate, loopID: loopID})
	}
	now := time.Now()
	triples := make([]message.Triple, 0, len(values))
	for _, value := range values {
		triples = append(triples, message.Triple{
			Subject:    loopEntityID,
			Predicate:  value.predicate,
			Object:     value.loopID,
			Source:     graphWriterSource,
			Timestamp:  now,
			Confidence: 1.0,
		})
	}
	return triples, nil
}

// WriteSpawnIdentity births the loop-execution entity via a typed origin
// contract (ADR-056 W0 4c-pre-1). It constructs a LoopExecutionEntity from
// the spawn parameters, gets the full origin triple set from its Triples()
// method (IDENTICAL predicate set to the pre-4c-pre-1 buildSpawnIdentityTriples),
// and sends a synchronous entity.create request so the entity is born with a
// proper MessageType envelope.
//
// An already-exists response is handled explicitly by this component.
//
// Returns an error ONLY on genuine birth failure (the entity.create
// round-trip — transport error, a definite server failure, or an EntityExists
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
	// WriteModelEndpoints / WriteLoopCompletion / WriteLoopFailure all
	// Warn+return here) and the nil-client / nil-task /
	// empty-triples guards above. An ERROR from this method is reserved for a
	// genuine birth FAILURE (the entity.create round-trip below); only
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
func (w *graphWriter) WriteLoopCancellation(ctx context.Context, event *agentic.LoopCancelledEvent, evidenceIncomplete bool) {
	if w.natsClient == nil {
		return
	}
	if w.platform.Org == "" || w.platform.Platform == "" {
		w.logger.Warn("graph_writer: cannot write loop cancellation, platform identity missing",
			"loop_id", event.LoopID, "org", w.platform.Org, "platform", w.platform.Platform)
		return
	}

	loopEntityID := agentic.LoopExecutionEntityID(w.platform.Org, w.platform.Platform, event.LoopID)
	triples := buildLoopCancellationTriples(loopEntityID, event, evidenceIncomplete)
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

// appendEvidenceIntegrity stamps the observed-audit-loss condition onto a
// terminal triple set when, and only when, the component observed that this
// loop's evidence is not there.
//
// evidenceIncomplete arrives from the component's loopAuditLoss, which
// answers at two scopes: per loop, set by reportTrajectoryAuditFailure from
// the same trajectoryAuditFailure value that feeds the Health latch, the
// metric, and the ERROR log; and component-wide, latched by the Start path
// that finds it cannot record trajectory evidence at all, where no per-loop
// failure can ever be observed because nothing is attempted. Nothing here
// re-derives it from the counter or re-evaluates a predicate.
//
// The condition rides the caller's slice so it lands on the SAME graph
// mutation as agent.loop.outcome. The failures most worth reporting
// (evidence_put, fact_create) happen when the substrate is unhealthy, so a
// dedicated write at failure time would be the least likely to land.
//
// One triple, unqualified, or none. A loop may lose evidence at several
// stages; electing one would manufacture a claim about which mattered, and
// the full {stage,kind,reason} set already lives in the ERROR log and the
// bounded counter.
func appendEvidenceIntegrity(triples []message.Triple, triple func(string, any) message.Triple,
	evidenceIncomplete bool,
) []message.Triple {
	if !evidenceIncomplete {
		return triples
	}
	return append(triples, triple(agvocab.LoopEvidenceIntegrity, evidenceIntegrityIncomplete))
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
	evidenceIncomplete bool,
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

	return appendEvidenceIntegrity(triples, triple, evidenceIncomplete)
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
	evidenceIncomplete bool,
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

	// gh#569: surface the classified failure reason as a rule-readable fact —
	// budget exhaustion ("max_iterations") and a transient model error
	// ("model_error") both stamp outcome="failed" and were indistinguishable
	// at the fact level, making reason-aware routes (escalate vs retry)
	// impossible. The value is already classified upstream
	// (failureReasonForHandlerError → LoopFailedEvent.Reason).
	if event.Reason != "" {
		triples = append(triples, triple(agvocab.LoopTerminalReason, event.Reason))
	}

	if modelEntityID != "" {
		triples = append(triples, triple(agvocab.LoopModelUsed, modelEntityID))
	}
	if cost > 0 {
		triples = append(triples, triple(agvocab.LoopCostUSD, cost))
	}

	return appendEvidenceIntegrity(triples, triple, evidenceIncomplete)
}

// buildLoopCancellationTriples constructs the minimal set of triples for a cancelled loop.
// Cancellation events carry less data than completion/failure — no model, no token counts.
//
// Spawn-known triples (task, workflow, workflow_step) live on the loop
// entity from WriteSpawnIdentity (gh#159); cancellation only writes the
// transition signals.
func buildLoopCancellationTriples(loopEntityID string, event *agentic.LoopCancelledEvent, evidenceIncomplete bool) []message.Triple {
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
		triple(agvocab.LoopEndedAt, event.CancelledAt.Format(time.RFC3339)),
	}

	return appendEvidenceIntegrity(triples, triple, evidenceIncomplete)
}

// resolveModelAccounting maps modelName — a CAPABILITY for spawned loops
// (coordinator/developer/reviewer), or an endpoint name for direct-model loops —
// to its endpoint ONCE via model.ResolveEndpointName, then returns the
// model-endpoint entity ID and the loop cost, BOTH keyed on the resolved
// endpoint (the #584 fix). Keying on the raw capability produced a zero cost
// (an unpriced capability name misses GetEndpoint) and a agent.loop.model-used
// triple pointing at the capability instead of the real endpoint.
//
// Resolution is a superset of the prior raw use of modelName: a real endpoint
// name resolves to itself, so direct-model loops are unchanged. An empty or
// unresolvable modelName yields an empty entity ID (model-used omitted by the
// build functions) and zero cost, matching prior behavior. Callers guard
// org/platform non-empty before this point, so ModelEndpointEntityID is only
// reached with well-formed parts.
func resolveModelAccounting(
	reg model.RegistryReader,
	org, platform, modelName string,
	tokensIn, tokensOut int,
) (modelEntityID string, cost float64) {
	resolved := model.ResolveEndpointName(reg, modelName)
	if resolved != "" {
		modelEntityID = agentic.ModelEndpointEntityID(org, platform, resolved)
	}
	cost = computeCost(reg, resolved, tokensIn, tokensOut)
	return modelEntityID, cost
}

// computeCost calculates loop cost from token counts and endpoint pricing.
// Returns 0.0 if the registry is nil, the endpoint is unknown, or pricing is not configured.
// endpointName must already be a resolved endpoint name (capabilities are
// resolved upstream in resolveModelAccounting via model.ResolveEndpointName).
func computeCost(reg model.RegistryReader, endpointName string, tokensIn, tokensOut int) float64 {
	if reg == nil {
		return 0
	}
	ep := reg.GetEndpoint(endpointName) // modelresolveaudit:allow already-resolved (endpointName resolved upstream in resolveModelAccounting via ResolveEndpointName)
	if ep == nil {
		return 0
	}
	return float64(tokensIn)*ep.InputPricePer1MTokens/1_000_000 +
		float64(tokensOut)*ep.OutputPricePer1MTokens/1_000_000
}

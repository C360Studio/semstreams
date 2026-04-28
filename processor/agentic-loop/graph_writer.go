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
	graphMutationSubject = "graph.mutation.triple.add"
	graphWriterTimeout   = 5 * time.Second
	graphWriterSource    = "agentic-loop"

	// maxPromptTripleBytes caps the size of the user prompt stored as the
	// agent.loop.description triple, including the truncation marker. Full
	// prompts live elsewhere (loop state / ObjectStore); the triple only
	// needs enough text for BM25/NL search to match by topic.
	maxPromptTripleBytes = 8 * 1024

	// truncationMarker is appended to a truncated prompt so consumers can
	// tell at a glance that the triple is not the full text.
	truncationMarker = "…[truncated]"
)

// truncateForTriple returns s capped at max bytes total (including the
// truncation marker, if appended). It is UTF-8 safe — the cut point is
// walked back to the nearest rune boundary to avoid producing invalid UTF-8.
func truncateForTriple(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Reserve space for the marker so the final result stays within `max`.
	budget := max - len(truncationMarker)
	if budget <= 0 {
		// Pathological: cap is smaller than the marker itself. Return an
		// empty-ish truncation rather than something absurd.
		return truncationMarker[:max]
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

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, cost, w.platform.Org, w.platform.Platform)
	for _, t := range triples {
		if err := w.writeTriple(ctx, t); err != nil {
			w.logger.Warn("graph_writer: failed to write loop completion triple",
				"loop_id", event.LoopID, "predicate", t.Predicate, "error", err)
		}
	}
}

// WriteLoopFailure emits triples for a loop that terminated with an error.
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
	for _, t := range triples {
		if err := w.writeTriple(ctx, t); err != nil {
			w.logger.Warn("graph_writer: failed to write loop failure triple",
				"loop_id", event.LoopID, "predicate", t.Predicate, "error", err)
		}
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

// WriteLoopCancellation emits triples for a loop that was cancelled.
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
	for _, t := range triples {
		if err := w.writeTriple(ctx, t); err != nil {
			w.logger.Warn("graph_writer: failed to write loop cancellation triple",
				"loop_id", event.LoopID, "predicate", t.Predicate, "error", err)
		}
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
// org and platform are passed through for constructing parent loop entity IDs.
func buildLoopCompletionTriples(
	loopEntityID string,
	event *agentic.LoopCompletedEvent,
	modelEntityID string,
	cost float64,
	org, platform string,
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
		triple(agvocab.LoopRole, event.Role),
		triple(agvocab.LoopIterations, event.Iterations),
		triple(agvocab.LoopTokensIn, event.TokensIn),
		triple(agvocab.LoopTokensOut, event.TokensOut),
		triple(agvocab.LoopTask, event.TaskID),
		triple(agvocab.LoopEndedAt, event.CompletedAt.Format(time.RFC3339)),
	}

	if modelEntityID != "" {
		triples = append(triples, triple(agvocab.LoopModelUsed, modelEntityID))
	}
	if cost > 0 {
		triples = append(triples, triple(agvocab.LoopCostUSD, cost))
	}
	if event.ParentLoopID != "" {
		parentEntityID := agentic.LoopExecutionEntityID(org, platform, event.ParentLoopID)
		triples = append(triples, triple(agvocab.LoopParent, parentEntityID))
	}
	if event.WorkflowSlug != "" {
		triples = append(triples, triple(agvocab.LoopWorkflow, event.WorkflowSlug))
	}
	if event.WorkflowStep != "" {
		triples = append(triples, triple(agvocab.LoopWorkflowStep, event.WorkflowStep))
	}
	if event.UserID != "" {
		triples = append(triples, triple(agvocab.LoopUser, event.UserID))
	}
	if event.Prompt != "" {
		triples = append(triples, triple(agvocab.LoopDescription, truncateForTriple(event.Prompt, maxPromptTripleBytes)))
	}

	return triples
}

// buildLoopFailureTriples constructs triples for a loop that terminated with an error.
// cost should be pre-computed via computeCost; pass 0.0 to omit the cost triple.
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
		triple(agvocab.LoopRole, event.Role),
		triple(agvocab.LoopIterations, event.Iterations),
		triple(agvocab.LoopTokensIn, event.TokensIn),
		triple(agvocab.LoopTokensOut, event.TokensOut),
		triple(agvocab.LoopTask, event.TaskID),
		triple(agvocab.LoopEndedAt, event.FailedAt.Format(time.RFC3339)),
	}

	if modelEntityID != "" {
		triples = append(triples, triple(agvocab.LoopModelUsed, modelEntityID))
	}
	if cost > 0 {
		triples = append(triples, triple(agvocab.LoopCostUSD, cost))
	}
	if event.WorkflowSlug != "" {
		triples = append(triples, triple(agvocab.LoopWorkflow, event.WorkflowSlug))
	}
	if event.WorkflowStep != "" {
		triples = append(triples, triple(agvocab.LoopWorkflowStep, event.WorkflowStep))
	}
	if event.UserID != "" {
		triples = append(triples, triple(agvocab.LoopUser, event.UserID))
	}
	if event.Prompt != "" {
		triples = append(triples, triple(agvocab.LoopDescription, truncateForTriple(event.Prompt, maxPromptTripleBytes)))
	}

	return triples
}

// buildLoopCancellationTriples constructs the minimal set of triples for a cancelled loop.
// Cancellation events carry less data than completion/failure — no model, no token counts.
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

	triples := []message.Triple{
		triple(agvocab.LoopOutcome, event.Outcome),
		triple(agvocab.LoopTask, event.TaskID),
		triple(agvocab.LoopEndedAt, event.CancelledAt.Format(time.RFC3339)),
	}

	if event.WorkflowSlug != "" {
		triples = append(triples, triple(agvocab.LoopWorkflow, event.WorkflowSlug))
	}
	if event.WorkflowStep != "" {
		triples = append(triples, triple(agvocab.LoopWorkflowStep, event.WorkflowStep))
	}

	return triples
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

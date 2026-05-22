package executors

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// ResearchGraphToolName is the name parents use to invoke the
// ADR-045 graph-search rule chain. PR 1 of the Phase 1 plan
// (docs/operations/22-adr045-phase1-plan.md) lands the registry +
// tool seam; PRs 2-6 land the five components + seven rules.
const ResearchGraphToolName = "research_graph"

// researchTriggerKeyPrefix is the key prefix R0 watches in
// AGENT_LOOPS. Full key shape: research.requested.<loopID>.
//
// PR 1 writes the trigger key only; PR 6 wires R0 to fire on it.
// Centralising the prefix here keeps the tool and R0's rule
// config in lockstep — a renaming requires touching both.
const researchTriggerKeyPrefix = "research.requested."

// loopIDPrefix is the prefix the research_graph tool uses when
// minting new loop IDs. Mirrors the agentic-dispatch convention
// (loop_<uuid8>) but uses `rg_` so research-pipeline loops are
// distinguishable at a glance in operator dashboards without
// parsing the LoopEntity.Role field.
const loopIDPrefix = "rg_"

// ResearchKVWriter is the narrow KV surface this executor consumes.
// Production satisfies it with a natsclient.KVStore scoped to the
// AGENT_LOOPS bucket; tests substitute an in-memory recorder so
// they don't need a real NATS connection.
//
// Two methods so the tool can write both halves of its KV footprint:
//
//   - CreateLoopEntity puts a research-pipeline LoopEntity at key
//     <loopID>. Use Create semantics (returns ErrKVKeyExists if the
//     loop_id collides) so an accidental duplicate doesn't silently
//     overwrite the previous loop's state.
//
//   - PutResearchTrigger writes the research_intent payload at key
//     research.requested.<loopID>. R0 watches this key pattern (see
//     ADR-045 §Rule chain) and fires the chain on write. Last-write-
//     wins is fine here because the loop_id is freshly minted and
//     unique per call.
type ResearchKVWriter interface {
	CreateLoopEntity(ctx context.Context, loopID string, value []byte) error
	PutResearchTrigger(ctx context.Context, loopID string, value []byte) error
}

// ResearchGraphExecutor implements the research_graph tool. It
// kicks off the ADR-045 chain by writing two KV records to
// AGENT_LOOPS:
//
//  1. A LoopEntity at key <loopID> with role=research_pipeline so
//     downstream tools (read_loop_result, flow_monitor) can find
//     the operation by its loop ID.
//
//  2. The research_intent payload wrapped in a BaseMessage envelope
//     at key research.requested.<loopID> — the R0 trigger key.
//
// The tool returns StopLoop=true so the parent's current iteration
// terminates; the continuation rule (R6 — landed in PR 6) resumes
// the parent on a subsequent iteration with the search_result
// payload.
type ResearchGraphExecutor struct {
	kv        ResearchKVWriter
	platform  component.PlatformMeta
	logger    *slog.Logger
	now       func() time.Time
	newLoopID func() string
}

// ResearchGraphOption configures a ResearchGraphExecutor at
// construction time.
type ResearchGraphOption func(*ResearchGraphExecutor)

// WithResearchGraphClock overrides the time source the executor
// stamps onto the LoopEntity. nil-safe — passing nil preserves
// the existing clock. Used by tests for deterministic timestamps.
func WithResearchGraphClock(now func() time.Time) ResearchGraphOption {
	return func(e *ResearchGraphExecutor) {
		if now != nil {
			e.now = now
		}
	}
}

// WithResearchGraphIDGenerator overrides the loop-ID generator.
// nil-safe; tests use it for deterministic IDs.
func WithResearchGraphIDGenerator(gen func() string) ResearchGraphOption {
	return func(e *ResearchGraphExecutor) {
		if gen != nil {
			e.newLoopID = gen
		}
	}
}

// NewResearchGraphExecutor constructs the executor. kv must be
// non-nil — without it the tool can't seed the chain. Logger
// defaults to slog.Default(); clock and ID generator default to
// time.Now / uuid-based.
func NewResearchGraphExecutor(kv ResearchKVWriter, platform component.PlatformMeta, opts ...ResearchGraphOption) *ResearchGraphExecutor {
	e := &ResearchGraphExecutor{
		kv:       kv,
		platform: platform,
		logger:   slog.Default(),
		now:      time.Now,
		newLoopID: func() string {
			// Use uuid v4 truncated to 8 chars. Mirrors the
			// agentic-dispatch convention. Collision odds at 8 hex
			// chars (~4 billion possibilities) are operator-tolerable
			// for the research-loop volume Phase 1 will see, and the
			// CreateLoopEntity ErrKVKeyExists path catches a clash.
			id := uuid.New().String()
			return loopIDPrefix + id[:8]
		},
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// SetLogger replaces the default logger. nil-safe — a nil argument
// preserves whatever logger the executor already has.
func (e *ResearchGraphExecutor) SetLogger(logger *slog.Logger) {
	if logger != nil {
		e.logger = logger
	}
}

// ListTools returns the research_graph tool definition. The schema
// is strict-mode-compliant (ADR-035): topic is required; hints,
// budget_tokens, and max_iterations are optional. additionalProperties
// is closed so providers honouring function.strict reject ad-hoc
// fields a model might invent.
func (e *ResearchGraphExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name: ResearchGraphToolName,
		Description: "Spawn an asynchronous graph-research operation. The chain runs the existing graph classifier, " +
			"routes to one of {synthesize_directly, retighten, walk_seeds, decompose}, executes multi-tier subqueries, " +
			"assesses sufficiency, and synthesises an answer with provenance refs. Returns immediately and terminates " +
			"this iteration; the SearchResult arrives on a subsequent iteration via the continuation rule. " +
			"Call this for non-trivial questions where you don't already know the entities or predicates to query; " +
			"for direct lookups by ID, use query_entity instead.",
		Parameters: map[string]any{
			"type":                 "object",
			"required":             []string{"topic"},
			"additionalProperties": false,
			"properties": map[string]any{
				"topic": map[string]any{
					"type":        "string",
					"description": "Natural-language research subject (e.g. \"drone hover anomalies\", \"recent auth failures\").",
				},
				"hints": map[string]any{
					"type":                 "object",
					"description":          "Optional free-form map of classifier hints (entity_kind, domain, recency, etc.). Keys are strings; values are strings.",
					"additionalProperties": map[string]any{"type": "string"},
				},
				"budget_tokens": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum LLM tokens to spend across the chain. Default %d.", research.DefaultBudgetTokens),
				},
				"max_iterations": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Refine-loop cap (R4). Default %d.", research.DefaultMaxIterations),
				},
			},
		},
		Strict: true,
	}}
}

// Execute routes the tool call. Any other tool name is a routing
// bug.
func (e *ResearchGraphExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != ResearchGraphToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "ResearchGraphExecutor", "Execute", "route tool")
	}
	return e.researchGraph(ctx, call)
}

func (e *ResearchGraphExecutor) researchGraph(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	intent, err := parseResearchGraphArgs(call.Arguments)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     err.Error(),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	if call.LoopID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "research_graph invoked without a loop_id on the tool call; cannot link the research operation back to its parent",
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(fmt.Errorf("tool call missing loop_id"), "ResearchGraphExecutor", "researchGraph", "resolve parent loop")
	}

	// Validate the assembled intent. parseResearchGraphArgs trusts
	// arg parsing; Validate is the schema gate before we touch KV.
	if err := intent.Validate(); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("invalid research intent: %v", err),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	loopID := e.newLoopID()

	// Resolve defaults onto the persisted intent so downstream
	// components (R0's publishees in PR 6 onward) read the same
	// budget/iteration values regardless of whether the caller
	// omitted them. Doing this on a copy keeps the parser's output
	// raw (zeros mean "caller omitted") while the persisted shape is
	// fully populated. Avoids a five-PR footgun where each consumer
	// would otherwise have to remember ResolvedXxx() before use.
	persistedIntent := *intent
	persistedIntent.BudgetTokens = intent.ResolvedBudgetTokens()
	persistedIntent.MaxIterations = intent.ResolvedMaxIterations()

	// Construct the research-pipeline LoopEntity. State starts at
	// LoopStateExecuting because the chain begins firing as soon as
	// R0 sees the trigger key — no exploring / planning phase for a
	// rule-coordinated chain.
	loopEntity := agentic.NewLoopEntity(loopID, "", research.PipelineRole, "", persistedIntent.MaxIterations)
	loopEntity.State = agentic.LoopStateExecuting
	loopEntity.ParentLoopID = call.LoopID
	loopEntity.StartedAt = e.now().UTC()

	loopBytes, err := json.Marshal(&loopEntity)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal loop entity: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "ResearchGraphExecutor", "researchGraph", "marshal loop entity")
	}

	// Order matters: write the LoopEntity record FIRST so that R0's
	// downstream actions (which may read the loop entity by ID) see
	// a populated record. The trigger-key write is the chain
	// kickoff — perform it only after the LoopEntity is durably
	// stored.
	if err := e.kv.CreateLoopEntity(ctx, loopID, loopBytes); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("create research-pipeline loop entity: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, errs.WrapTransient(err, "ResearchGraphExecutor", "researchGraph", "create loop entity")
	}

	// Wrap the intent in a BaseMessage envelope so the registry
	// decoder can resolve the type discriminator when R0's
	// downstream components read this key. Per
	// feedback_nats_publishes_use_payload_registry: every payload on
	// a NATS surface wraps in BaseMessage, even when the immediate
	// consumer reads raw — drift-resilient.
	envelope := message.NewBaseMessage(persistedIntent.Schema(), &persistedIntent, research.TripleSource)
	triggerBytes, err := json.Marshal(envelope)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal research intent envelope: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "ResearchGraphExecutor", "researchGraph", "marshal intent envelope")
	}

	if err := e.kv.PutResearchTrigger(ctx, loopID, triggerBytes); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("write research trigger key: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, errs.WrapTransient(err, "ResearchGraphExecutor", "researchGraph", "write trigger key")
	}

	// Construct the per-loop entity ID via the Try variant — runtime
	// hot paths use TryLoopExecutionEntityID per
	// feedback_try_loop_entity_id_for_runtime. Surfaces a clean
	// ToolErrorInternal if the platform meta is malformed instead
	// of panicking the dispatch goroutine.
	loopEntityID, err := agentic.TryLoopExecutionEntityID(e.platform.Org, e.platform.Platform, loopID)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("construct loop entity ID: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "ResearchGraphExecutor", "researchGraph", "construct loop entity ID")
	}

	e.logger.Info("research_graph dispatched",
		slog.String("loop_id", loopID),
		slog.String("parent_loop_id", call.LoopID),
		slog.String("topic", persistedIntent.Topic),
		slog.Int("budget_tokens", persistedIntent.BudgetTokens),
		slog.Int("max_iterations", persistedIntent.MaxIterations))

	resultMetadata := map[string]any{
		"loop_id":        loopID,
		"loop_entity_id": loopEntityID,
		"parent_loop_id": call.LoopID,
		"topic":          persistedIntent.Topic,
		"budget_tokens":  persistedIntent.BudgetTokens,
		"max_iterations": persistedIntent.MaxIterations,
	}

	return agentic.ToolResult{
		CallID: call.ID,
		Content: fmt.Sprintf(
			"Research operation dispatched.\nloop_id: %s\ntopic: %s\nThe SearchResult will arrive on a subsequent iteration via the continuation rule.",
			loopID, persistedIntent.Topic),
		StopLoop: true,
		Metadata: resultMetadata,
	}, nil
}

// parseResearchGraphArgs reads the untyped tool arguments into a
// ResearchIntent. Missing or wrong-typed topic surfaces as invalid-
// args so the framework retry policy can step in — small models that
// miss the schema on the first try get a second chance before the
// loop fails.
func parseResearchGraphArgs(raw map[string]any) (*research.Intent, error) {
	topic, ok := raw["topic"].(string)
	if !ok || topic == "" {
		return nil, fmt.Errorf("topic is required and must be a non-empty string")
	}
	intent := &research.Intent{Topic: topic}

	if rawHints, present := raw["hints"]; present && rawHints != nil {
		hintsMap, ok := rawHints.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("hints must be an object with string values")
		}
		intent.Hints = make(map[string]string, len(hintsMap))
		for k, v := range hintsMap {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("hints[%q] must be a string", k)
			}
			intent.Hints[k] = s
		}
	}

	if rawBudget, present := raw["budget_tokens"]; present && rawBudget != nil {
		// JSON numbers decode to float64 through map[string]any. Both
		// integer-shaped (4000) and fractional values flow through;
		// fractional inputs are a caller bug — reject them.
		f, ok := rawBudget.(float64)
		if !ok {
			return nil, fmt.Errorf("budget_tokens must be an integer")
		}
		if f != float64(int(f)) {
			return nil, fmt.Errorf("budget_tokens must be an integer, got %v", rawBudget)
		}
		intent.BudgetTokens = int(f)
	}

	if rawMax, present := raw["max_iterations"]; present && rawMax != nil {
		f, ok := rawMax.(float64)
		if !ok {
			return nil, fmt.Errorf("max_iterations must be an integer")
		}
		if f != float64(int(f)) {
			return nil, fmt.Errorf("max_iterations must be an integer, got %v", rawMax)
		}
		intent.MaxIterations = int(f)
	}

	return intent, nil
}

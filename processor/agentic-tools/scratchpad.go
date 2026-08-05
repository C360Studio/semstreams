package agentictools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// ScratchpadToolName is the name agents use to invoke the scratchpad
// tool. semspec ask 2026-05-12, ADR-036 §Future candidates instance.
const ScratchpadToolName = "scratchpad"

// scratchpadToolSource is the Source field on triples this tool
// publishes. Distinguishes scratchpad emissions from other agent-private
// state writers (write_todos, decide, etc.) in graph queries.
const scratchpadToolSource = "agent-scratchpad"

// ScratchpadExecutor implements the scratchpad tool. The tool is a
// free-form one-shot reasoning channel for agents that need a "think
// before commit" runway — useful for weaker mid-tier models that
// struggle under simultaneous strict response_format + strict tool-args
// (qwen3-MoE on OpenRouter, mid-tier vLLM, llama-3.3-70b dense per the
// semspec proposal).
//
// Per-call semantics (NOT cumulative or full-list-replace like
// write_todos): every call mints a fresh scratch.id and appends four
// triples to the loop entity (id, text, created_at, chars). Multiple
// scratchpad calls within one loop simply accumulate; ordering is
// recoverable via ScratchCreatedAt.
//
// Strict: false (per semspec 2026-05-12 decision) — the text field is
// unconstrained anyway, so strict-mode response_format would impose
// provider strictness on what is effectively a single-string envelope.
// Re-evaluate if real-LLM testing shows drift.
//
// All public methods are safe for concurrent use across loops; the
// executor holds no per-call mutable state.
type ScratchpadExecutor struct {
	publisher TriplePublisher
	platform  types.PlatformMeta
	logger    *slog.Logger
	now       func() time.Time
	newID     func() string
}

// NewScratchpadExecutor constructs the executor given a triple publisher
// and the platform identity used to resolve the calling loop's entity
// ID. The clock defaults to time.Now and the ID generator defaults to
// uuid v4; tests inject deterministic alternatives via SetClock /
// SetIDGenerator.
func NewScratchpadExecutor(publisher TriplePublisher, platform types.PlatformMeta) *ScratchpadExecutor {
	return &ScratchpadExecutor{
		publisher: publisher,
		platform:  platform,
		logger:    slog.Default(),
		now:       time.Now,
		newID:     func() string { return uuid.New().String() },
	}
}

// SetLogger replaces the default logger. nil-safe. Boot-time wiring
// only — NOT safe to call after Execute is in flight (no internal
// locking; concurrent Execute readers would race the writer).
func (e *ScratchpadExecutor) SetLogger(logger *slog.Logger) {
	if logger != nil {
		e.logger = logger
	}
}

// SetClock replaces the time source. nil-safe. Boot-time wiring only —
// NOT safe to call after Execute is in flight. Tests use this for
// deterministic ScratchCreatedAt assertions; production should not.
func (e *ScratchpadExecutor) SetClock(now func() time.Time) {
	if now != nil {
		e.now = now
	}
}

// SetIDGenerator replaces the ID generator. nil-safe. Boot-time wiring
// only — NOT safe to call after Execute is in flight. Tests use this
// to pin scratch IDs; production should not.
func (e *ScratchpadExecutor) SetIDGenerator(newID func() string) {
	if newID != nil {
		e.newID = newID
	}
}

// ListTools returns the scratchpad tool definition. Single required
// string argument, no length cap (trajectories already handle large
// payloads — per the semspec proposal's open question, no tool-level
// limit needed).
func (e *ScratchpadExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name: ScratchpadToolName,
		Description: "Free-form reasoning space before committing structured output. " +
			"Use this to think through decomposition, list constraints you must satisfy, " +
			"or note edge cases you considered — content is recorded in the trajectory " +
			"and graph but not interpreted by any rule or consumer. Call this BEFORE the strict " +
			"commit tool when the task involves decomposition or multi-step planning.",
		Effect: agentic.ToolEffectMutating,
		Parameters: map[string]any{
			"type":                 "object",
			"required":             []string{"text"},
			"additionalProperties": false,
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "Your reasoning. Free-form prose; no schema, no length limit, no interpretation. Lands in the trajectory and on the loop entity for audit and recovery.",
				},
			},
		},
		// Strict: false per semspec 2026-05-12 decision — the text field
		// is unconstrained anyway, so strict mode would impose provider
		// strictness on what is effectively a single-string envelope.
	}}
}

// scratchpadArgs is the parsed shape of the scratchpad tool arguments.
type scratchpadArgs struct {
	Text string `json:"text"`
}

// scratchpadResult is the JSON the executor returns in ToolResult.Content.
// Compact, structured confirmation — gives the agent verifiable feedback
// that the call landed (helps weaker models stay on the persona pattern)
// and gives operators a Prometheus-able byte-count signal without
// doubling trajectory size by echoing the text back.
type scratchpadResult struct {
	Status    string `json:"status"`
	ScratchID string `json:"scratch_id"`
	Chars     int    `json:"chars"`
}

// Execute routes the tool call. Any name other than scratchpad is a
// routing bug at the dispatcher layer.
func (e *ScratchpadExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != ScratchpadToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "ScratchpadExecutor", "Execute", "route tool")
	}
	return e.scratchpad(ctx, call)
}

func (e *ScratchpadExecutor) scratchpad(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	args, err := parseScratchpadArgs(call.Arguments)
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
			Error:     "scratchpad invoked without a loop_id; cannot resolve the loop entity",
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(fmt.Errorf("tool call missing loop_id"), "ScratchpadExecutor", "scratchpad", "resolve loop entity")
	}
	loopEntityID, err := agentic.TryLoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("construct loop entity ID: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "ScratchpadExecutor", "scratchpad", "construct loop entity ID")
	}

	scratchID := e.newID()
	now := e.now()
	createdAt := now.UTC().Format(time.RFC3339Nano)
	chars := len(args.Text)

	// Four triples on the loop entity, every one stamped with this call's
	// scratchID in Context. Downstream consumers reconstruct an entry by
	// grouping the triples that share that Context value. Append-only —
	// multiple scratchpad calls within one loop simply accumulate.
	//
	// Context is load-bearing, not decoration. It is one of the six fields in
	// append tuple identity (subject, predicate, object, datatype, source,
	// context), and append suppresses any triple whose tuple is already
	// stored. Before this stamp only ScratchID carried the per-call UUID, so
	// two calls emitting the same text — or merely the same CHARACTER COUNT,
	// which is far likelier — produced byte-identical ScratchText /
	// ScratchCreatedAt / ScratchChars tuples, and the second call landed as an
	// id with no chars. Timestamp cannot do this job: it is deliberately
	// EXCLUDED from identity, because producers stamp time.Now() and including
	// it would mean deduplication never fired for anyone.
	//
	// Failures here ARE surfaced as ToolErrorNetwork because the graph
	// emission is the audit/recovery contract of this tool (per the
	// semspec proposal — "scratchpad calls appear in the trajectory
	// alongside other tool calls" is supplemented by ADR-036-aligned
	// triples so ops-agent and future recovery agents can query). This
	// matches decide/write_todos discipline, NOT the web-tools
	// log+continue discipline, because the trajectory alone is not
	// durable across compaction.
	// Atomic batch publish: all four triples share Subject=loopEntityID,
	// so the graph-ingest per-Subject CAS handler applies them as one
	// unit. Pre-2026-05-13 this was a per-triple loop that could leave
	// an orphan ScratchID + ScratchText pair (the first two writes) in
	// the graph on graph-ingest degradation — the orphan was harmless
	// (no duplication, the retry minted a fresh UUID), but the atomic
	// batch eliminates the window entirely. The post-migration assertion
	// lives in TestScratchpadExecutor_BatchFailure_LeavesNoOrphans; see
	// git history for the pre-migration TestScratchpadExecutor_
	// PartialWrite_LeavesOrphanID it replaced.
	triples := []message.Triple{
		{Subject: loopEntityID, Predicate: agvocab.ScratchID, Object: scratchID, Source: scratchpadToolSource, Context: scratchID, Timestamp: now, Confidence: 1.0},
		{Subject: loopEntityID, Predicate: agvocab.ScratchText, Object: args.Text, Source: scratchpadToolSource, Context: scratchID, Timestamp: now, Confidence: 1.0},
		{Subject: loopEntityID, Predicate: agvocab.ScratchCreatedAt, Object: createdAt, Source: scratchpadToolSource, Context: scratchID, Timestamp: now, Confidence: 1.0},
		{Subject: loopEntityID, Predicate: agvocab.ScratchChars, Object: chars, Source: scratchpadToolSource, Context: scratchID, Timestamp: now, Confidence: 1.0},
	}
	if err := e.publisher.Append(ctx, triples); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("publish scratchpad triples: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, errs.WrapTransient(err, "ScratchpadExecutor", "scratchpad", "publish triples batch")
	}

	payload, err := json.Marshal(scratchpadResult{
		Status:    "ok",
		ScratchID: scratchID,
		Chars:     chars,
	})
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal result: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "ScratchpadExecutor", "scratchpad", "marshal result")
	}

	e.logger.Debug("scratchpad call recorded",
		"loop_id", call.LoopID,
		"loop_entity_id", loopEntityID,
		"scratch_id", scratchID,
		"chars", chars)

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(payload),
		Metadata: map[string]any{
			"scratch_id":     scratchID,
			"chars":          chars,
			"loop_entity_id": loopEntityID,
		},
	}, nil
}

// parseScratchpadArgs reads the untyped arguments and enforces the
// required text field. Empty text is rejected so the agent gets
// immediate feedback rather than a silent no-op emission (LLM-side
// retry can correct).
func parseScratchpadArgs(raw map[string]any) (scratchpadArgs, error) {
	if raw == nil {
		return scratchpadArgs{}, fmt.Errorf("missing required argument %q", "text")
	}
	rawText, ok := raw["text"]
	if !ok {
		return scratchpadArgs{}, fmt.Errorf("missing required argument %q", "text")
	}
	text, ok := rawText.(string)
	if !ok {
		return scratchpadArgs{}, fmt.Errorf("argument %q must be a string", "text")
	}
	if text == "" {
		return scratchpadArgs{}, fmt.Errorf("argument %q must be a non-empty string", "text")
	}
	return scratchpadArgs{Text: text}, nil
}

package agenticloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/looptoken"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/google/uuid"
)

// Package error sentinels for loop-token admission. Callers compare with
// errors.Is — the same shape pkg/lifecycle uses for create-versus-exists
// (pkg/lifecycle/errors.go), and consumed the same way.
var (
	// ErrLoopAlreadyExists is returned by CreateLoopWithID when the supplied
	// framework-minted token already names a registered loop. It is a
	// distinguishable condition rather than a generic invalid error because
	// task intake branches on it: a task naming a live loop is a
	// CONTINUATION of that loop, and the right move is to attach to the
	// conversation already under that token, never to mint a second loop
	// over it (#1227). A caller that cannot attach may still treat it as a
	// refusal; what it must not do is proceed as if it created the loop.
	ErrLoopAlreadyExists = errors.New("agentic-loop: loop already exists")

	// ErrLoopTerminal is returned when a continuation names a loop that has
	// already settled. A settled loop cannot be advanced and its token must
	// not be recycled into a replacement loop, so the task is refused rather
	// than attached — the terminal loop's recorded outcome stays the answer
	// for that token.
	ErrLoopTerminal = errors.New("agentic-loop: loop is terminal")

	// ErrLoopBusy is returned when a continuation names a loop that has work
	// in flight: outstanding tool calls, or a human approval decision it is
	// waiting on. It is deliberately distinct from ErrLoopTerminal because the
	// two mean opposite things to the caller — terminal is final, busy is
	// answerable once the round finishes. Attaching in that window appends the
	// new user turn to a half-written round (an assistant turn carrying
	// tool_calls whose tool results have not arrived), sends orphan tool_calls
	// to the provider, runs two rounds concurrently over one context manager,
	// and moves an approval-gated loop off the state its human decision
	// resolves. Owner ruling 2026-09-02: refuse; do not queue the turn.
	ErrLoopBusy = errors.New("agentic-loop: loop has work in flight")

	// ErrLoopNotFound is returned when an operation names a loop the manager
	// does not hold. After a loop settles its per-loop state is released
	// (releaseLoopTransientState), so absence is the ordinary steady state
	// for a settled loop, not a failure: readers of late-arriving messages
	// branch on this to drop quietly instead of reporting a fault.
	ErrLoopNotFound = errors.New("agentic-loop: loop not found")
)

// gateRefusedError is beginApprovalGate refusing a gate because the loop
// settled between the handler's terminal guard and the gate: it is terminal in
// memory (a cancel landed), or released (state is empty). Either way the
// terminal belongs to its owner, and the handler answers as its terminal guard
// does (#1362 checkpoint 2 delta review, HIGH).
type gateRefusedError struct {
	loopID string
	state  agentic.LoopState
}

func (e *gateRefusedError) Error() string {
	if e.state == "" {
		return fmt.Sprintf("approval gate refused: loop %s was released", e.loopID)
	}
	return fmt.Sprintf("approval gate refused: loop %s is %s", e.loopID, e.state)
}

// LoopManager manages loop entity lifecycle and state
type LoopManager struct {
	loops                map[string]*agentic.LoopEntity
	contextManagers      map[string]*ContextManager          // loopID -> ContextManager
	pendingTools         map[string]map[string]bool          // loopID -> map[callID]bool
	queuedToolCalls      map[string][]agentic.ToolCall       // loopID -> remaining calls to dispatch serially
	cachedTools          map[string][]agentic.ToolDefinition // loopID -> tools (runtime cache, not persisted)
	cachedToolChoice     map[string]*agentic.ToolChoice      // loopID -> tool choice (runtime cache, not persisted)
	cachedMetadata       map[string]map[string]any           // loopID -> metadata (domain context, not persisted)
	cachedRequestTimeout map[string]string                   // loopID -> request timeout (from TaskMessage.Timeout, not persisted)
	cachedResponseFormat map[string]*agentic.ResponseFormat  // loopID -> response_format (from TaskMessage.ResponseFormat, not persisted)
	requestToLoop        map[string]string                   // requestID -> loopID
	// outstandingRequests names the ONE model request a loop has published and
	// not yet had answered. requestToLoop cannot answer that question: it is
	// append-only for the loop's whole life (its only delete is releaseLoop),
	// so it records "this loop published X", never "X is still in flight".
	// Process-local on purpose — a replacement has lost the whole loop, not
	// just this entry. The durable name is the record's PublishedRequestID
	// (#1330), and a rebuild re-seats this entry from the retained request
	// (restoreLoopFromRequest).
	outstandingRequests    map[string]string         // loopID -> requestID
	toolCallToLoop         map[string]string         // executionID -> loopID
	executionIDToName      map[string]string         // executionID -> function name (for Gemini tool result name field)
	executionIDToArguments map[string]map[string]any // executionID -> tool arguments (for trajectory audit)
	executionIDToOrdinal   map[string]uint32         // executionID -> model response order (for trajectory audit)
	requestStartTimes      map[string]time.Time      // requestID -> start time (for duration measurement)
	executionStartTimes    map[string]time.Time      // executionID -> start time (for duration measurement)
	contextConfig          ContextConfig             // shared context config
	modelRegistry          model.RegistryReader      // model registry for context managers
	logger                 *slog.Logger              // logger for context managers
	mu                     sync.RWMutex
}

// LoopManagerOption is a functional option for configuring LoopManager
type LoopManagerOption func(*LoopManager)

// WithLoopManagerLogger sets the logger for the LoopManager and its context managers
func WithLoopManagerLogger(logger *slog.Logger) LoopManagerOption {
	return func(lm *LoopManager) {
		lm.logger = logger
	}
}

// WithLoopManagerModelRegistry sets the model registry for context managers
func WithLoopManagerModelRegistry(reg model.RegistryReader) LoopManagerOption {
	return func(lm *LoopManager) {
		lm.modelRegistry = reg
	}
}

// NewLoopManager creates a new LoopManager
func NewLoopManager(opts ...LoopManagerOption) *LoopManager {
	lm := &LoopManager{
		loops:                  make(map[string]*agentic.LoopEntity),
		contextManagers:        make(map[string]*ContextManager),
		pendingTools:           make(map[string]map[string]bool),
		queuedToolCalls:        make(map[string][]agentic.ToolCall),
		cachedTools:            make(map[string][]agentic.ToolDefinition),
		cachedToolChoice:       make(map[string]*agentic.ToolChoice),
		cachedMetadata:         make(map[string]map[string]any),
		cachedRequestTimeout:   make(map[string]string),
		cachedResponseFormat:   make(map[string]*agentic.ResponseFormat),
		requestToLoop:          make(map[string]string),
		outstandingRequests:    make(map[string]string),
		toolCallToLoop:         make(map[string]string),
		executionIDToName:      make(map[string]string),
		executionIDToArguments: make(map[string]map[string]any),
		executionIDToOrdinal:   make(map[string]uint32),
		requestStartTimes:      make(map[string]time.Time),
		executionStartTimes:    make(map[string]time.Time),
		contextConfig:          DefaultContextConfig(),
		logger:                 slog.Default(),
	}
	for _, opt := range opts {
		opt(lm)
	}
	return lm
}

// NewLoopManagerWithConfig creates a new LoopManager with custom context config
func NewLoopManagerWithConfig(contextConfig ContextConfig, opts ...LoopManagerOption) *LoopManager {
	lm := &LoopManager{
		loops:                  make(map[string]*agentic.LoopEntity),
		contextManagers:        make(map[string]*ContextManager),
		pendingTools:           make(map[string]map[string]bool),
		queuedToolCalls:        make(map[string][]agentic.ToolCall),
		cachedTools:            make(map[string][]agentic.ToolDefinition),
		cachedToolChoice:       make(map[string]*agentic.ToolChoice),
		cachedMetadata:         make(map[string]map[string]any),
		cachedRequestTimeout:   make(map[string]string),
		cachedResponseFormat:   make(map[string]*agentic.ResponseFormat),
		requestToLoop:          make(map[string]string),
		outstandingRequests:    make(map[string]string),
		toolCallToLoop:         make(map[string]string),
		executionIDToName:      make(map[string]string),
		executionIDToArguments: make(map[string]map[string]any),
		executionIDToOrdinal:   make(map[string]uint32),
		requestStartTimes:      make(map[string]time.Time),
		executionStartTimes:    make(map[string]time.Time),
		contextConfig:          contextConfig,
		logger:                 slog.Default(),
	}
	for _, opt := range opts {
		opt(lm)
	}
	return lm
}

// CreateLoop creates a new loop entity with a generated UUID
func (m *LoopManager) CreateLoop(taskID, role, model string, maxIterations ...int) (string, error) {
	loopID := m.GenerateLoopID()
	return m.CreateLoopWithID(loopID, taskID, role, model, maxIterations...)
}

// GenerateLoopID returns an identity with the exact UUID semantics used by
// CreateLoop, without registering or persisting a loop. Intake uses this pure
// generator to preflight prospective lineage before loop creation.
func (m *LoopManager) GenerateLoopID() string {
	return uuid.NewString()
}

// CreateLoopWithID creates a new loop entity with a specific ID.
//
// The supplied ID must be a framework-minted loop token — a canonical UUID
// (ADR-105, #1192). TaskMessage.Validate is the gate for everything arriving
// over the wire; this refusal is the gate for a composed binary calling the
// LoopManager directly, and it lands before any state is registered.
//
// Two refusals, in a fixed order (#1227). The token FORM check runs first, so a
// non-canonical token is always reported as malformed and never as a collision.
// The already-exists check runs second, before the three map writes below,
// because those writes OVERWRITE an existing record, its pending-tool set, and
// its context manager: creating over a live token silently destroyed the
// conversation accumulated under it, which is a create where the caller meant a
// continuation. Callers that mean a continuation branch on ErrLoopAlreadyExists
// and attach; callers that meant a create get a refusal that left every map
// exactly as it found it.
func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {
	if !looptoken.Valid(loopID) {
		return "", errs.WrapInvalid(
			fmt.Errorf("loop id %q is not a framework-minted loop token: a loop instance token is a canonical UUID "+
				"(36 bytes, lowercase, hyphenated) minted by the framework", loopID),
			"agentic-loop", "CreateLoopWithID", "validate loop token")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.loops[loopID]; exists {
		return "", errs.WrapInvalid(
			fmt.Errorf("loop %s: %w", loopID, ErrLoopAlreadyExists),
			"agentic-loop", "CreateLoopWithID", "refuse create over a registered loop token")
	}

	// Determine max iterations
	maxIter := 20 // default
	if len(maxIterations) > 0 && maxIterations[0] > 0 {
		maxIter = maxIterations[0]
	}

	entity := agentic.NewLoopEntity(loopID, taskID, role, model, maxIter)

	m.loops[loopID] = &entity
	m.pendingTools[loopID] = make(map[string]bool)

	// Always create context manager — full conversation history is required
	// for providers like Gemini that need the assistant tool_call message
	// paired with every tool result.
	opts := []ContextManagerOption{WithLogger(m.logger)}
	if m.modelRegistry != nil {
		opts = append(opts, WithModelRegistry(m.modelRegistry))
	}
	m.contextManagers[loopID] = NewContextManager(loopID, model, m.contextConfig, opts...)

	return loopID, nil
}

// attachContinuation binds a continuation task to the loop already registered
// under loopID and returns that loop's current entity.
//
// It is the second half of the create-versus-exists fence: CreateLoopWithID
// refuses the token, and intake calls this to join the live loop instead of
// minting over it. Two things happen here and nowhere else, both under the one
// lock so a concurrent settle cannot slip between them:
//
//   - A settled loop is REFUSED with ErrLoopTerminal. A terminal loop cannot be
//     advanced, and minting a replacement under its token would make the
//     recorded outcome unreachable for the token that names it.
//   - A loop with work IN FLIGHT is REFUSED with ErrLoopBusy. Non-terminal is
//     not idle: between the assistant turn that carries tool_calls and the
//     turn boundary that appends the matching tool results, the conversation is
//     half-written, and a continuation sends it as-is. See ErrLoopBusy for the
//     three consequences. The check reads the pending-tool map directly rather
//     than calling GetPendingTools: the write lock is already held here and
//     sync.RWMutex is not reentrant.
//   - The loop's task association is rebound to the continuation's task ID.
//     This is what keeps redelivery dedup working across an attach: intake
//     dedupes on TaskID via HasActiveLoopForTask, so a redelivery of THIS task
//     message must find the loop it already produced. Leaving the previous
//     turn's TaskID in place would let the same continuation be processed
//     twice, appending the user's turn to the conversation each time.
//     Residual, known and accepted: the rebind preserves dedup for THIS turn
//     and drops it for the previous one. A single scalar cannot dedupe more
//     than one turn — that needs a set or a window. So a redelivery of turn
//     N-1 arriving after turn N has attached is no longer recognised as
//     already-seen and appends that prompt a second time. Narrow in practice,
//     and bounded by the restart-safety work in gh#1159.
//
// No other per-loop state is touched: the context manager, the pending-tool
// set, and every cache stay exactly as the live loop left them.
func (m *LoopManager) attachContinuation(loopID, taskID, prompt string) (agentic.LoopEntity, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return agentic.LoopEntity{}, false, errs.Wrap(
			fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),
			"agentic-loop", "attachContinuation", "find loop")
	}
	if entity.State.IsTerminal() {
		return agentic.LoopEntity{}, false, errs.WrapInvalid(
			fmt.Errorf("loop %s is %s: %w", loopID, entity.State, ErrLoopTerminal),
			"agentic-loop", "attachContinuation", "refuse continuation of a settled loop")
	}
	if pending := len(m.pendingTools[loopID]); pending > 0 {
		return agentic.LoopEntity{}, false, errs.WrapTransient(
			fmt.Errorf("loop %s has %d tool call(s) still outstanding: %w", loopID, pending, ErrLoopBusy),
			"agentic-loop", "attachContinuation", "refuse continuation of a loop with work in flight")
	}
	if entity.State == agentic.LoopStateAwaitingApproval {
		return agentic.LoopEntity{}, false, errs.WrapTransient(
			fmt.Errorf("loop %s is awaiting a human approval decision: %w", loopID, ErrLoopBusy),
			"agentic-loop", "attachContinuation", "refuse continuation of a loop with work in flight")
	}

	entity.TaskID = taskID

	// A loop waiting on a model response is NOT refused — refusing would throw
	// the user's turn away, and this is the ordinary "someone typed while the
	// agent was thinking" case. It is DEFERRED: the caller puts the turn in the
	// loop's context and publishes nothing, because a second request minted now
	// would carry this iteration's name a second time and the duplicate window
	// would drop it. The outstanding response then advances the loop instead of
	// completing it.
	if _, outstanding := m.outstandingRequests[loopID]; outstanding {
		entity.PendingContinuation = true
		// A turn admitted NOW is not in the outstanding request's body, and it
		// is not in a carrying request minted earlier either. Whatever was
		// carrying, this turn is uncarried, so the next completion must carry
		// it rather than settle.
		entity.PendingContinuationRequestID = ""
		// The turn's text, beside its marker (#1365): the deferred lane's
		// marker write carries it to the record, and a rebuild replays it.
		// One string — a second turn deferred behind the same request
		// replaces the first as the record's uncarried turn.
		entity.PendingContinuationPrompt = prompt
		return *entity, true, nil
	}

	return *entity, false, nil
}

// restoreLoopFromRequest rebuilds, in THIS process's memory, the loop that the
// record and the newest retained request describe (#1330, design § 5.2 step 2
// and § 5.3 step 3; task 1.2).
//
// It is the second half of the create-versus-exists fence's third case. A task
// naming a live loop attaches (attachContinuation); a task naming a loop no
// process holds rebuilds it — and so does a model response or a tool result
// arriving at a process that was started after the loop was born. Without this,
// a replacement can only refuse the delivery and retry it to MaxDeliver.
//
// Nothing here is derived, inferred or synthesised. The record supplies the
// entity — identity, role, model, iteration, state, the applied set and the
// request it named — and the retained request supplies the conversation and the
// per-loop settings the loop was actually running with (tools, tool choice,
// response format, per-request timeout). Both are durable facts of the loop,
// not a reconstruction of them.
//
// The conversation is replayed into ONE region: the system messages go to the
// system prompt at the front, and everything else to RegionRecentHistory in the
// order the request carried it. The predecessor's compacted and summarised
// regions are not reconstructed — that is the whole simplification, and every
// other layer describes it the same way. The request is NOT GetContext() —
// prependIterationContext wraps it — so the per-iteration prefix is dropped
// first (isIterationPrefixMessage); what is left is GetContext()'s order,
// because the request's Messages were built from it.
//
// Two things do NOT survive, both recorded rather than repaired:
//
//   - Compaction ATTRIBUTION. A summary the predecessor had in
//     RegionCompactedHistory returns as ordinary recent history, so the next
//     compaction fires slightly earlier than it would have. Visible on
//     context_compactions_total and context_compacted_region_tokens.
//   - The ordering of MORE THAN ONE system message relative to the rest: they
//     are re-seated together at the front in retained order, which is the order
//     GetContext() rendered them in, rather than wherever the request
//     interleaved them.
//
// Re-attributing regions would mean guessing which retained message came from
// which region, which is exactly the content-comparison this change removes.
//
// RepairToolPairs runs last: a request retained mid-batch can carry an
// assistant tool_call whose results were never appended, and a provider refuses
// that pair outright.
func (m *LoopManager) restoreLoopFromRequest(
	ctx context.Context, record agentic.LoopEntity, request agentic.AgentRequest,
) error {
	if record.ID == "" {
		return errs.WrapInvalid(fmt.Errorf("loop record carries no id"),
			"LoopManager", "restoreLoopFromRequest", "validate the record to rebuild from")
	}
	if request.RequestID == "" || request.RequestID != record.PublishedRequestID {
		return errs.WrapInvalid(
			fmt.Errorf("loop %s: the retained request is %q but the record names %q",
				record.ID, request.RequestID, record.PublishedRequestID),
			"LoopManager", "restoreLoopFromRequest", "match the retained request to the record")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.loops[record.ID]; exists {
		// This process already holds the loop, so there is nothing to rebuild
		// and rebuilding would overwrite a live conversation with a retained
		// one. Same refusal CreateLoopWithID gives, for the same reason.
		return errs.WrapInvalid(
			fmt.Errorf("loop %s: %w", record.ID, ErrLoopAlreadyExists),
			"LoopManager", "restoreLoopFromRequest", "refuse a rebuild over a held loop")
	}

	entity := record
	// A continuation admitted while a request was outstanding is durable as its
	// MARKER and its TEXT (#1365): the deferred lane's marker write puts both on
	// the record in one compare-and-swap, with an empty carrier because no
	// request carried the turn yet. A record whose marker is uncarried has its
	// text replayed below, after the retained conversation, and the marker is
	// KEPT: HasPendingContinuation then reads true on the rebuilt loop, the next
	// completion advances instead of settling, its request carries the turn and
	// names itself the carrier, and SettleRequest clears the three together.
	//
	// The replay runs on EVERY uncarried marker, whichever request is retained.
	// Adoption (adoptNewerRetainedRequest) may have moved the record's name to a
	// newer retained request and left the marker as it found it, and the record
	// cannot tell which side of the turn that request was minted on. Five
	// windows, by where the replacement fell:
	//
	//   - before the marker write landed: no marker, nothing to replay — the
	//     turn lived in the replaced process only (the marker write's own
	//     Retry and best-effort rows);
	//   - after it, the record naming the request the turn deferred behind: one
	//     replay, carried once;
	//   - a carrier minted AFTER the turn, PubAck'd, its record write lost: that
	//     request already holds the turn and the replay adds it again — carried
	//     twice, logged below with the retained request's name, never zero;
	//   - the carrier's record write landed: the marker names it, nothing is
	//     replayed, and the retained request carries the turn once;
	//   - a turn deferred behind a request tracked but not yet on the record,
	//     that request retained and adopted: it was minted BEFORE the turn, so
	//     the replay is the only copy — carried once.
	//
	// Residuals, recorded rather than coded (design § 7.1, § 7.2): a marker
	// write landing after the carrier's own record write rebuilds as the
	// duplicate window, and a replacement between the marker write and the next
	// carrier write rebuilds a loop whose TaskID is the previous task's, since
	// the marker write does not move it.
	//
	// A NON-EMPTY PendingContinuationRequestID is left alone: that turn is
	// inside a retained request, so the replay above carries it and the marker
	// still has the job it was set for — stopping the carrier's own completion
	// from settling before the turn is answered.
	//
	// A marker that is uncarried and carries NO text claims a turn the record
	// does not hold — a record written before the text field existed, or a
	// fixture that wrote the marker alone. Seated as-is it would make the next
	// completion spend an iteration re-asking the model with a context that
	// gained nothing, so it is cleared with a warning naming the loop (#1330
	// Q2, 2026-09-23: the two-line clear plus the warning is the ruled shape;
	// a counter would be an owner question).
	if entity.PendingContinuation && entity.PendingContinuationRequestID == "" &&
		entity.PendingContinuationPrompt == "" {
		m.logger.WarnContext(ctx,
			"rebuilt loop cleared a deferred turn it cannot recover — the record carries its marker "+
				"but not its text, and the turn must be re-sent",
			slog.String("loop_id", record.ID),
			slog.String("published_request_id", record.PublishedRequestID),
			slog.Int("iterations", record.Iterations))
		entity.PendingContinuation = false
	}
	m.loops[record.ID] = &entity
	m.pendingTools[record.ID] = make(map[string]bool)

	opts := []ContextManagerOption{WithLogger(m.logger)}
	if m.modelRegistry != nil {
		opts = append(opts, WithModelRegistry(m.modelRegistry))
	}
	cm := NewContextManager(record.ID, record.Model, m.contextConfig, opts...)
	// A retained request is prependIterationContext's OUTPUT, not GetContext():
	// it opens with that iteration's budget line and possibly its working list,
	// both Role "system". They belong to the REQUEST, not to the conversation,
	// and seating them would pin one iteration's budget at the top of
	// RegionSystemPrompt for the rest of the loop's life while every later
	// request prepends a fresh one. Only the leading run is dropped — a message
	// further in is the conversation, whatever it says.
	conversation := request.Messages
	for len(conversation) > 0 && isIterationPrefixMessage(conversation[0]) {
		conversation = conversation[1:]
	}
	for _, msg := range conversation {
		region := RegionRecentHistory
		if msg.Role == "system" {
			region = RegionSystemPrompt
		}
		if err := cm.AddMessage(region, msg); err != nil {
			delete(m.loops, record.ID)
			delete(m.pendingTools, record.ID)
			return errs.WrapTransient(err, "LoopManager", "restoreLoopFromRequest",
				"replay the retained request into the rebuilt conversation")
		}
	}
	cm.RepairToolPairs()
	// The deferred turn, after the conversation it was typed into (#1365; the
	// windows are above). The marker stays set, so the next completion carries
	// it and names the carrier.
	if entity.PendingContinuation && entity.PendingContinuationRequestID == "" &&
		entity.PendingContinuationPrompt != "" {
		if err := cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
			Role:    "user",
			Content: entity.PendingContinuationPrompt,
		}); err != nil {
			delete(m.loops, record.ID)
			delete(m.pendingTools, record.ID)
			return errs.WrapTransient(err, "LoopManager", "restoreLoopFromRequest",
				"replay the record's deferred turn into the rebuilt conversation")
		}
		m.logger.InfoContext(ctx,
			"rebuilt loop replayed the deferred turn its record accepted — carried once, or twice when the "+
				"retained request was minted after the turn",
			slog.String("loop_id", record.ID),
			slog.String("retained_request_id", request.RequestID),
			slog.Int("prompt_bytes", len(entity.PendingContinuationPrompt)))
	}
	m.contextManagers[record.ID] = cm

	// The settings the loop was running with, read off the request it last
	// sent rather than off a TaskMessage this process never saw. Without them
	// the rebuilt loop's next request would advertise no tools at all.
	m.cachedTools[record.ID] = request.Tools
	m.cachedToolChoice[record.ID] = request.ToolChoice
	m.cachedResponseFormat[record.ID] = request.ResponseFormat
	if request.Timeout != "" {
		m.cachedRequestTimeout[record.ID] = request.Timeout
	}

	// The task's ENFORCEMENT metadata (ADR-067), off the RECORD rather than
	// off the request: it is written there once at birth, and the request
	// never carried it. dispatchToolCall stamps
	// DispatchEnforcedMetadataKeys onto every outgoing call from this cache,
	// and both consumers read an ABSENT key as permissive — agentic-tools'
	// bash executor treats no policy as the workspace-write default, and
	// decide permits any action with no allowlist. A rebuild that skipped it
	// turned a recovered read-only task writable, silently. Defensive copy for
	// the same reason CacheMetadata makes one: the caller's map is the
	// record's, and this cache outlives the read.
	if len(record.Metadata) > 0 {
		metadata := make(map[string]any, len(record.Metadata))
		maps.Copy(metadata, record.Metadata)
		m.cachedMetadata[record.ID] = metadata
	}

	// TrackRequest's shape: the request is routable AND outstanding. Outstanding
	// is the right claim here because the only evidence in hand is that the
	// request was published; restoreToolBatch settles it when the response that
	// answered it is read.
	m.requestToLoop[request.RequestID] = record.ID
	m.outstandingRequests[record.ID] = request.RequestID
	return nil
}

// restoreToolBatch re-seats the tool batch the retained response dispatched, so
// a rebuilt loop can decide AllToolsComplete (#1330, design § 5.3 step 3).
//
// The record alone cannot answer that question. It carries which executions are
// APPLIED; only the response carries how many there were. A rebuild that skipped
// this would advance the loop on the first redelivered result of a three-call
// batch and send the model a turn missing two tool messages.
//
// Membership only, by identity. Execution IDs are re-derived from the retained
// response with the same deterministic stamp the dispatch used
// (stampToolExecutionCorrelation), never matched by comparing arguments or
// content.
//
// inFlight names the execution whose result this delivery is about to apply. It
// is excluded from the queue for the same reason serial dispatch never queues
// the call it has in flight: the queue is what HandleToolResult dispatches NEXT,
// and putting the arriving call back on it would re-execute work that has just
// answered.
//
// Declared residual: a governance rejection that was stored and lost with the
// crash is not in the applied set, so its call is queued and dispatched again.
// The retained response is the only durable record of the batch and it predates
// the rejection; re-taking that decision on dispatch is the same answer the
// ordinary serial-dispatch path gives a queued call, which is never re-proposed
// either.
func (m *LoopManager) restoreToolBatch(
	loopID string,
	response agentic.AgentResponse,
	applied map[string]agentic.ToolResult,
	inFlight string,
) error {
	calls := make([]agentic.ToolCall, len(response.Message.ToolCalls))
	copy(calls, response.Message.ToolCalls)
	if err := stampToolExecutionCorrelation(response.RequestID, calls); err != nil {
		return errs.WrapInvalid(err, "LoopManager", "restoreToolBatch",
			"re-derive the retained batch's execution identities")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cm, held := m.contextManagers[loopID]
	if !held {
		return errs.Wrap(fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),
			"LoopManager", "restoreToolBatch", "find the rebuilt conversation")
	}
	// The assistant turn the batch belongs to. It is in the retained RESPONSE,
	// never in the retained request, and without it every tool message the
	// batch produces is an orphan that RepairToolPairs removes — taking the
	// model's own tool calls out of the next request with it.
	if err := cm.AddMessage(RegionRecentHistory, response.Message); err != nil {
		return errs.WrapTransient(err, "LoopManager", "restoreToolBatch",
			"replay the assistant turn the batch belongs to")
	}

	// An approval gate's approval_required result is a placeholder, not an
	// answer: the gated call's real result is still owed, so its execution is
	// routed like any unanswered one. And the gate cleared the calls queued
	// behind it when it fired (gateForApproval) — the record carries that only
	// as the placeholder — so a batch that holds one rebuilds with an empty
	// queue, as the process that gated it held (#1362 checkpoint 2).
	gated := false
	var queued []agentic.ToolCall
	for _, call := range calls {
		m.executionIDToName[call.ExecutionID] = call.Name
		m.executionIDToOrdinal[call.ExecutionID] = call.CallOrdinal
		stored, done := applied[call.ExecutionID]
		if done && agentic.IsApprovalRequired(stored.Error) {
			// A gated call's arguments are NOT seated. An approval may have
			// dispatched a human's modified set, and the retained response
			// holds only the proposal: omit, do not falsify (owner ruling,
			// #1362 issuecomment-5827720719). An approval applied on this
			// rebuild re-seats the real set at dispatch (dispatchToolCall →
			// TrackToolArguments); a result recovered cold records no
			// dispatch arguments.
			gated = true
			m.toolCallToLoop[call.ExecutionID] = loopID
			continue
		}
		m.executionIDToArguments[call.ExecutionID] = call.Arguments
		if done {
			// Already answered. Its route stays unseated on purpose: a drained
			// execution is unroutable on the ordinary path too, which is what
			// keeps a late duplicate out of the next turn's applied set.
			continue
		}
		m.toolCallToLoop[call.ExecutionID] = loopID
		if call.ExecutionID == inFlight {
			continue
		}
		queued = append(queued, call)
	}
	if gated {
		queued = nil
	}
	m.queuedToolCalls[loopID] = queued

	// pendingTools stays empty on purpose. Dispatch is serial: at most one
	// call is in flight, and the QUEUE is what says how much of the batch is
	// left. HandleToolResult dispatches from that queue before it ever asks
	// AllToolsComplete, so a rebuilt loop with a non-empty queue cannot
	// advance early, and one with an empty queue has nothing left but the
	// result it is applying — which is the same answer pendingTools would
	// give. Seating it would be a second bookkeeping of one fact.

	// The response for this request is in hand, so the loop is not waiting on
	// a model. SettleRequest's half, applied to the mark restoreLoopFromRequest
	// set from the only evidence it had.
	if outstanding, ok := m.outstandingRequests[loopID]; ok && outstanding == response.RequestID {
		delete(m.outstandingRequests, loopID)
	}
	return nil
}

// HasPendingContinuation reports whether a continuation turn is waiting for a
// request to carry it — pending AND uncarried. Once a request names the turn,
// the answer is false: carrying it a second time would spend an iteration
// re-asking with a context that has gained nothing. The marker itself stays
// set until that request's response settles, so a publish whose durability is
// unknown leaves the fact recoverable.
func (m *LoopManager) HasPendingContinuation(loopID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entity, exists := m.loops[loopID]
	return exists && entity.PendingContinuation && entity.PendingContinuationRequestID == ""
}

// HasActiveLoopForTask returns true if a non-terminal loop already exists for the
// given task ID. This prevents duplicate loop creation on JetStream redelivery.
func (m *LoopManager) HasActiveLoopForTask(taskID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, entity := range m.loops {
		if entity.TaskID == taskID && !entity.State.IsTerminal() {
			return entity.ID, true
		}
	}
	return "", false
}

// GetLoop retrieves a loop entity by ID
func (m *LoopManager) GetLoop(loopID string) (agentic.LoopEntity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if loopID == "" {
		return agentic.LoopEntity{}, errs.WrapInvalid(fmt.Errorf("loop ID cannot be empty"), "LoopManager", "GetLoop", "validate loop ID")
	}

	entity, exists := m.loops[loopID]
	if !exists {
		return agentic.LoopEntity{}, errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "GetLoop", "find loop")
	}

	// A struct copy is shallow, so the applied set would travel out of this
	// lock as the SAME map the live entity holds — and every caller reads the
	// returned entity after this lock is released. marshalLoopRecord marshals
	// it into the record's bytes while StoreToolResult, on the handler
	// goroutine, writes the same map under this mutex: a data race, not a
	// stale read. One copy makes the returned entity a value the caller owns.
	//
	// The applied set is the only reference field a live loop mutates in
	// place. PendingApproval is replaced wholesale by BeginAwaitingApproval
	// and cleared by ResolveApproval, and Metadata is written once at birth.
	copied := *entity
	if entity.PendingToolResults != nil {
		copied.PendingToolResults = make(map[string]agentic.ToolResult, len(entity.PendingToolResults))
		maps.Copy(copied.PendingToolResults, entity.PendingToolResults)
	}
	return copied, nil
}

// UpdateLoop updates an existing loop entity
func (m *LoopManager) UpdateLoop(entity agentic.LoopEntity) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.loops[entity.ID]; !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", entity.ID), "LoopManager", "UpdateLoop", "find loop")
	}

	m.loops[entity.ID] = &entity
	return nil
}

// ApprovalTimeoutCandidate captures the loop+call coordinates the
// approval-timeout sweeper needs to publish an auto-rejection. The
// sweeper builds an agentic.ApprovalResponse from these and feeds
// it through HandleApprovalResponse — same code path a real human
// rejection would take.
type ApprovalTimeoutCandidate struct {
	LoopID string
	CallID string
	// ExecutionID and RequestID carry the gated call's framework identity so
	// the sweeper's synthetic rejection is matched the same way a human
	// response is. Without them the sweeper would be the one caller allowed in
	// on CallID alone, which is the hole the human path just closed.
	ExecutionID string
	RequestID   string
	ToolName    string
	RequestedAt time.Time
	Timeout     time.Duration
}

// SnapshotExpiredApprovals returns a snapshot of loops whose pending
// approval has timed out (RequestedAt + Timeout <= now). Skips loops
// whose Timeout is zero (wait-indefinitely policy). Read-locked; the
// snapshot is taken under the lock and the lock released before
// return so callers can act on each candidate without holding the
// mutex.
//
// Beta.25 adds this for the orphan-tool-call recovery work. The
// approval-timeout timer was a deferred item from beta.19; closing
// it now ensures a stuck human-approval flow doesn't leave the
// gated tool_call orphaned indefinitely (mode f of orphan recovery).
func (m *LoopManager) SnapshotExpiredApprovals(now time.Time) []ApprovalTimeoutCandidate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []ApprovalTimeoutCandidate
	for id, loop := range m.loops {
		if loop.State != agentic.LoopStateAwaitingApproval || loop.PendingApproval == nil {
			continue
		}
		if loop.PendingApproval.Timeout == 0 {
			continue
		}
		deadline := loop.PendingApproval.RequestedAt.Add(loop.PendingApproval.Timeout)
		if now.Before(deadline) {
			continue
		}
		out = append(out, ApprovalTimeoutCandidate{
			LoopID:      id,
			CallID:      loop.PendingApproval.CallID,
			ExecutionID: loop.PendingApproval.ExecutionID,
			RequestID:   loop.PendingApproval.RequestID,
			ToolName:    loop.PendingApproval.ToolName,
			RequestedAt: loop.PendingApproval.RequestedAt,
			Timeout:     loop.PendingApproval.Timeout,
		})
	}
	return out
}

// ResolveApprovalIfPending atomically transitions the loop out of
// LoopStateAwaitingApproval if and only if the supplied call_id
// matches the currently pinned PendingApproval. Returns a snapshot
// of the pending state (so the caller has the original tool name +
// arguments + trace context for re-dispatch) plus a bool indicating
// whether the resolve actually happened. A false return is the
// idempotent drop case: the loop is no longer awaiting approval, or
// the response targets a different call_id (typical when a
// duplicate UI click races with an automated reject scheduler).
//
// This is the only path that should mutate PendingApproval +
// State out of awaiting_approval after BeginAwaitingApproval. The
// previous load → mutate → UpdateLoop pattern in
// HandleApprovalResponse let two concurrent responses both pass
// the awaiting-state check and both dispatch — for a safety
// feature, that double-execution risk is unacceptable.
func (m *LoopManager) ResolveApprovalIfPending(loopID, callID, executionID string) (agentic.PendingApprovalState, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		// ErrLoopNotFound, not a bare error: after a loop settles its per-loop
		// state is released, so a late or duplicate response for a settled loop
		// finds nothing here. HandleApprovalResponse branches on the sentinel
		// and drops exactly as it drops a response for a still-present terminal
		// loop — absence and terminal presence must not be distinguishable to a
		// late arrival.
		return agentic.PendingApprovalState{}, false, errs.Wrap(
			fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),
			"LoopManager", "ResolveApprovalIfPending", "find loop")
	}
	if entity.State != agentic.LoopStateAwaitingApproval {
		return agentic.PendingApprovalState{}, false, nil
	}
	if !approvalAnswersGate(entity.PendingApproval, callID, executionID) {
		return agentic.PendingApprovalState{}, false, nil
	}

	pending := *entity.PendingApproval
	if err := entity.ResolveApproval(); err != nil {
		return agentic.PendingApprovalState{}, false, errs.Wrap(err, "LoopManager", "ResolveApprovalIfPending", "resolve approval")
	}
	return pending, true, nil
}

// approvalAnswersGate reports whether an approval answer names the pending
// gate. It is the one identity rule for an answer, used by the warm resolve
// above and by the cold branch that reads the gate off the loop's record
// (approval_response_handler.go), so the two cannot disagree about which
// answer a gate accepts.
//
// Execution identity decides, whenever the pending state carries one.
// Provider CallID is request-scoped conversation data: a provider may reuse
// it on a later turn of the SAME loop, and an approval replayed from the
// earlier turn would then authorise the later call — a different tool
// invocation than the human saw. A response that omits ExecutionID against
// a pending approval that has one is refused as stale rather than falling
// back to CallID, because the fallback IS the hole.
//
// CallID still decides for a pending approval minted before execution
// identity existed, which carries no ExecutionID to match on. That branch is
// unreachable on this tree — every gate is minted from a routed result that
// carries its execution identity, and pre-v1 storage is greenfield — and is
// kept only because the warm resolve always had it.
func approvalAnswersGate(gate *agentic.PendingApprovalState, callID, executionID string) bool {
	if gate == nil {
		return false
	}
	if gate.ExecutionID != "" {
		return gate.ExecutionID == executionID
	}
	return gate.CallID == callID
}

// seatRecordToFail gives this process a loop from its record alone — no
// conversation, no batch, no routing — so the terminal owner, which renders
// the record it writes from the loop this process holds, can fail it.
//
// It has one caller and one purpose: the approval lane's cold branch, when the
// evidence a rebuild needs is confirmed gone (#1362, OQ1). A loop seated here
// is never continued; the failure path releases it once its terminal is
// committed. A loop this process already holds is refused, as every seat
// refuses one.
func (m *LoopManager) seatRecordToFail(record agentic.LoopEntity) error {
	if record.ID == "" {
		return errs.WrapInvalid(fmt.Errorf("loop record carries no id"),
			"LoopManager", "seatRecordToFail", "validate the record to seat")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.loops[record.ID]; exists {
		return errs.WrapInvalid(
			fmt.Errorf("loop %s: %w", record.ID, ErrLoopAlreadyExists),
			"LoopManager", "seatRecordToFail", "refuse a seat over a held loop")
	}
	entity := record
	m.loops[record.ID] = &entity
	m.pendingTools[record.ID] = make(map[string]bool)
	return nil
}

// beginApprovalGate gates the loop on one tool call, atomically: the live
// entity is moved to awaiting_approval with its pending call, and the calls
// queued behind it are cleared, under the manager's lock — the shape of
// ResolveApprovalIfPending, its inverse (#1362 checkpoint 2 re-review, M1).
//
// The gate used to read the loop, gate the COPY and write the copy back, in
// two lock sections. Anything another lane did to the loop in between was
// overwritten: the gated result StoreToolResult had just put into the applied
// set (so the record lost the gate's own placeholder), a continuation's
// pending marker, a cancel (reverted to awaiting_approval). Gating the live
// entity under the one lock leaves nothing to overwrite.
//
// A loop that is terminal, or already gated on another call, is refused by
// BeginAwaitingApproval before anything is mutated.
func (m *LoopManager) beginApprovalGate(
	loopID string, toolResult agentic.ToolResult, toolName string, args map[string]any, timeout time.Duration,
) (agentic.PendingApprovalState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return agentic.PendingApprovalState{}, &gateRefusedError{loopID: loopID}
	}
	if entity.State.IsTerminal() {
		return agentic.PendingApprovalState{}, &gateRefusedError{loopID: loopID, state: entity.State}
	}
	if err := entity.BeginAwaitingApproval(
		toolResult.CallID, toolName, args, toolResult.Error, timeout, toolResult.TraceID); err != nil {
		return agentic.PendingApprovalState{}, fmt.Errorf("begin awaiting approval: %w", err)
	}
	entity.PendingApproval.RequestID = toolResult.RequestID
	entity.PendingApproval.ExecutionID = toolResult.ExecutionID
	entity.PendingApproval.CallOrdinal = toolResult.CallOrdinal
	// Clear sibling tool calls queued behind this one. Once the human
	// responds, the LLM will get a fresh round-trip with the approve/reject
	// result and can decide whether to re-issue the other calls.
	delete(m.queuedToolCalls, loopID)
	return *entity.PendingApproval, nil
}

// DeleteLoop releases every per-loop entry the manager holds for loopID: the
// loop entity, its context manager, its pending-tool set, its queued tool
// calls, its cached tool definitions, tool choice, metadata, request timeout
// and response format, its task prompt, its truncation-retry counter, and the
// request/call routing and audit entries that belong to it.
//
// This is the release Component.releaseLoopTransientState performs when a loop
// settles, and its only production caller (#1233). Until that wiring it had
// none, so a process retained every conversation it had ever run — each entry
// sized by its conversation, growth bounded only by uptime.
//
// Idempotent: every deletion is a no-op on an absent key, so competing terminal
// paths cannot turn release into a failure. The error return is always nil and
// is retained only because the exported signature predates this caller.
//
// Request routing keys retain a loop prefix and owner value. Framework execution
// IDs are opaque, so their routing entries are released only by owner value. The
// request sweep also removes its timing entry. The execution sweep never parses
// an ID or predicts its loop; it trusts the mapping's owner value. Execution
// metadata shares that same key and is deleted with a surviving route.
//
// Known residual, not closed here: a turn boundary evicts completed execution
// routes before DeleteLoop runs but retains their metadata for conversation and
// trajectory construction. Once that route is gone, the opaque execution ID has
// no surviving link to its loop, so its metadata can outlive terminal release.
// Closing that bounded lifetime leak needs either earlier metadata deletion or a
// per-loop execution index; this correlation slice adds neither.
func (m *LoopManager) DeleteLoop(loopID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.loops, loopID)
	delete(m.pendingTools, loopID)
	delete(m.queuedToolCalls, loopID)
	delete(m.contextManagers, loopID)
	delete(m.cachedTools, loopID)
	delete(m.cachedToolChoice, loopID)
	delete(m.cachedMetadata, loopID)
	delete(m.cachedRequestTimeout, loopID)
	delete(m.cachedResponseFormat, loopID)
	delete(m.outstandingRequests, loopID)

	prefix := loopID + ":"
	for k, owner := range m.requestToLoop {
		if owner == loopID || strings.HasPrefix(k, prefix) {
			delete(m.requestToLoop, k)
			delete(m.requestStartTimes, k)
		}
	}
	for executionID, owner := range m.toolCallToLoop {
		if owner == loopID {
			delete(m.toolCallToLoop, executionID)
			m.deleteToolMetadataLocked(executionID)
		}
	}
	return nil
}

// deleteToolMetadataLocked drops the execution metadata entries retained for
// conversation and trajectory construction. The caller holds m.mu.
func (m *LoopManager) deleteToolMetadataLocked(executionID string) {
	delete(m.executionIDToName, executionID)
	delete(m.executionIDToArguments, executionID)
	delete(m.executionIDToOrdinal, executionID)
	delete(m.executionStartTimes, executionID)
}

// GetContextManager retrieves the context manager for a loop
func (m *LoopManager) GetContextManager(loopID string) *ContextManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.contextManagers[loopID]
}

// CacheTools stores tool definitions for a loop (discovered once, reused for all requests)
func (m *LoopManager) CacheTools(loopID string, tools []agentic.ToolDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedTools[loopID] = tools
}

// GetCachedTools retrieves the cached tool definitions for a loop
func (m *LoopManager) GetCachedTools(loopID string) []agentic.ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedTools[loopID]
}

// CacheToolChoice stores the tool choice strategy for a loop (set once from task, reused for all requests)
func (m *LoopManager) CacheToolChoice(loopID string, tc *agentic.ToolChoice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedToolChoice[loopID] = tc
}

// GetCachedToolChoice retrieves the cached tool choice for a loop
func (m *LoopManager) GetCachedToolChoice(loopID string) *agentic.ToolChoice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedToolChoice[loopID]
}

// CacheMetadata stores domain context metadata for a loop (set once from task, reused for all tool calls).
// Makes a defensive copy to isolate from the caller's map.
func (m *LoopManager) CacheMetadata(loopID string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]any, len(metadata))
	for k, v := range metadata {
		cp[k] = v
	}
	m.cachedMetadata[loopID] = cp
}

// GetCachedMetadata retrieves the cached metadata for a loop
func (m *LoopManager) GetCachedMetadata(loopID string) map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedMetadata[loopID]
}

// CacheRequestTimeout stores the per-request timeout for a loop (from
// TaskMessage.Timeout). Reused for all continuation iterations so the
// task-level budget persists across LLM calls in the same loop.
func (m *LoopManager) CacheRequestTimeout(loopID, timeout string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedRequestTimeout[loopID] = timeout
}

// GetCachedRequestTimeout retrieves the cached per-request timeout for a loop.
// Returns empty string when no task-level timeout was set.
func (m *LoopManager) GetCachedRequestTimeout(loopID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedRequestTimeout[loopID]
}

// CacheResponseFormat stores the per-task response_format for a loop (from
// TaskMessage.ResponseFormat). Reused for all continuation iterations so the
// structured-output constraint persists across LLM calls in the same loop.
// ADR-034.
func (m *LoopManager) CacheResponseFormat(loopID string, rf *agentic.ResponseFormat) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedResponseFormat[loopID] = rf
}

// GetCachedResponseFormat retrieves the cached response_format for a loop.
// Returns nil when no task-level response_format was set, in which case
// AgentRequest.ResponseFormat stays nil and tool-calling behaviour is
// preserved.
func (m *LoopManager) GetCachedResponseFormat(loopID string) *agentic.ResponseFormat {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedResponseFormat[loopID]
}

// CacheTaskPrompt records the prompt of the task that bore the loop on its
// entity (TaskPrompt, #1365), so the birth write puts it on the record and a
// rebuild's wholesale seat restores it. If GC/repair leaves the context
// empty, this prompt is re-injected as a synthetic user message so the model
// always has contents to work with; the terminal events publish it as Prompt.
// HandleTask calls it on a birth only — a continuation's turn is never the
// loop's prompt.
func (m *LoopManager) CacheTaskPrompt(loopID, prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entity, exists := m.loops[loopID]; exists {
		entity.TaskPrompt = prompt
	}
}

// GetTaskPrompt returns the prompt of the task that bore the loop, "" when
// the loop is not held or its record carries none.
func (m *LoopManager) GetTaskPrompt(loopID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if entity, exists := m.loops[loopID]; exists {
		return entity.TaskPrompt
	}
	return ""
}

// GetCurrentIteration returns the current iteration for a loop
func (m *LoopManager) GetCurrentIteration(loopID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return 0
	}
	return entity.Iterations
}

// TransitionLoop transitions a loop to a new state
func (m *LoopManager) TransitionLoop(loopID string, newState agentic.LoopState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	return entity.TransitionTo(newState)
}

// IncrementIteration increments the loop iteration counter
func (m *LoopManager) IncrementIteration(loopID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	return entity.IncrementIteration()
}

// AddPendingTool adds a pending tool call to the loop
func (m *LoopManager) AddPendingTool(loopID, callID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.loops[loopID]; !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	if m.pendingTools[loopID] == nil {
		m.pendingTools[loopID] = make(map[string]bool)
	}

	m.pendingTools[loopID][callID] = true
	return nil
}

// RemovePendingTool removes a pending tool call from the loop
func (m *LoopManager) RemovePendingTool(loopID, callID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pendingTools[loopID] != nil {
		delete(m.pendingTools[loopID], callID)
	}

	return nil
}

// GetPendingTools returns all pending tool calls for a loop
func (m *LoopManager) GetPendingTools(loopID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := m.pendingTools[loopID]
	if pending == nil {
		return []string{}
	}

	result := make([]string, 0, len(pending))
	for callID := range pending {
		result = append(result, callID)
	}

	return result
}

// AllToolsComplete returns true if there are no pending tool calls
func (m *LoopManager) AllToolsComplete(loopID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := m.pendingTools[loopID]
	return len(pending) == 0
}

// QueueToolCalls stores tool calls to be dispatched serially after the current call completes.
func (m *LoopManager) QueueToolCalls(loopID string, calls []agentic.ToolCall) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queuedToolCalls[loopID] = append(m.queuedToolCalls[loopID], calls...)
}

// DequeueToolCall removes and returns the next queued tool call for dispatch.
func (m *LoopManager) DequeueToolCall(loopID string) (agentic.ToolCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue := m.queuedToolCalls[loopID]
	if len(queue) == 0 {
		return agentic.ToolCall{}, false
	}

	next := queue[0]
	queue[0] = agentic.ToolCall{} // zero for GC (arguments/metadata maps)
	m.queuedToolCalls[loopID] = queue[1:]
	return next, true
}

// HasQueuedTools returns true if there are tool calls waiting to be dispatched.
func (m *LoopManager) HasQueuedTools(loopID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.queuedToolCalls[loopID]) > 0
}

// QueuedToolCount returns how many tool calls are waiting to be dispatched.
// A caller that must account for every queued call needs the number, not just
// whether any exist: it is the only honest bound on a drain loop, because the
// batch size is the provider's choice and this package imposes no limit on it.
func (m *LoopManager) QueuedToolCount(loopID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.queuedToolCalls[loopID])
}

// ClearQueuedTools discards all queued tool calls (e.g., when StopLoop fires).
func (m *LoopManager) ClearQueuedTools(loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.queuedToolCalls, loopID)
}

// TrackRequest associates a request ID with a loop ID and marks it as the
// loop's one outstanding model request. Every model-request publish site calls
// this already, which is why the invariant is recorded here rather than in a
// fourth call each site could forget.
//
// It also records the carrier of a deferred turn, for the same reason and in
// the same place. A request minted while a turn is deferred CARRIES that turn —
// it is built from the context the turn was written into — and there are three
// sites that mint one: the iteration request, the truncation retry, and the
// birth request. The bookkeeping used to live in publishIterationRequest, which
// is only two of the three: a truncation retry sent the turn and left the loop
// still deferring, so the next completion spent an iteration re-asking the
// model with a context that had gained nothing.
//
// It RECORDS rather than clears. The marker is persisted before the publish it
// describes, so clearing here would durably say "nothing deferred" about a
// request whose durability is still unknown; SettleRequest clears it when that
// request's response arrives, which is the first moment the send is a fact.
func (m *LoopManager) TrackRequest(requestID, loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestToLoop[requestID] = loopID
	m.outstandingRequests[loopID] = requestID
	if entity, exists := m.loops[loopID]; exists && entity.PendingContinuation {
		entity.PendingContinuationRequestID = requestID
	}
}

// SetPublishedRequest records the request this loop has minted as the one its
// record will name (LoopEntity.PublishedRequestID, invariant I1 of #1330).
//
// Two callers, and the split is the ordering. Birth calls it at the mint: its
// record is written BEFORE the first request is published (owner ruling Q1), so
// the name has to exist first. Every later transition is stamped by the CARRIER
// instead (Component.stampPublishedRequest), after publishResults has PubAck'd
// the request and before the record write — because this entity is shared with
// every other lane writing this loop, and a name set at the mint is one they
// can commit while the request is still in flight. That ordering is what makes
// the durable field mean "an AgentRequest with this identity is retained",
// rather than "a process intended to publish one".
//
// A loop this manager does not hold is refused rather than ignored: a request
// this process cannot record is a request whose PubAck nothing will ever
// classify, so the mint fails instead of publishing an unrecordable name.
func (m *LoopManager) SetPublishedRequest(loopID, requestID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(
			fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),
			"LoopManager", "SetPublishedRequest", "find loop")
	}
	entity.PublishedRequestID = requestID
	return nil
}

// registerRequestRoute records only that this request belongs to this loop, so
// a response can be routed to it. It is the half of TrackRequest that says
// "published", separated from the half that says "outstanding": a lookup that
// rebuilds lost routing must not also announce that the loop is waiting on a
// model, which is a claim about the future and not about the map it repaired.
func (m *LoopManager) registerRequestRoute(requestID, loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestToLoop[requestID] = loopID
}

// SettleRequest clears the loop's outstanding-request marker when the named
// request is the one outstanding. Matching on the request ID matters: a late
// response for a superseded request must not clear a newer request's mark and
// let a continuation publish a second request at the same iteration.
//
// It is also where a carried continuation stops being deferred: a response for
// the request that carries the turn proves the request was sent, which the
// build did not. Matching on the carrier rather than on the outstanding mark
// keeps the two independent — a superseded response settles nothing, and a
// carrier answered after the loop moved on still ends its deferral.
func (m *LoopManager) SettleRequest(loopID, requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settleOutstandingLocked(loopID, requestID)
	if entity, exists := m.loops[loopID]; exists && requestID != "" && entity.PendingContinuationRequestID == requestID {
		entity.PendingContinuation = false
		entity.PendingContinuationRequestID = ""
		entity.PendingContinuationPrompt = ""
	}
}

// settleTruncatedRequest is SettleRequest for a length_truncated response
// (#1365, F3): it clears the outstanding mark and leaves the deferral alone.
// A truncated answer is not an answer to the turn its request carried — the
// model was cut off — so marker, carrier and text survive into the compaction
// retry, whose TrackRequest names it the new carrier and whose answer settles
// the deferral. A truncation the loop cannot retry fails the loop, and the
// terminal record keeps the unanswered turn, as it keeps one at the iteration
// ceiling.
func (m *LoopManager) settleTruncatedRequest(loopID, requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settleOutstandingLocked(loopID, requestID)
}

// settleOutstandingLocked clears the outstanding mark when it names requestID.
// Callers hold m.mu.
func (m *LoopManager) settleOutstandingLocked(loopID, requestID string) {
	if outstanding, ok := m.outstandingRequests[loopID]; ok && outstanding == requestID {
		delete(m.outstandingRequests, loopID)
	}
}

// deferredContinuationPrompt returns the deferred turn's text while the marker
// is set, CARRIED or not, and "" otherwise. Its one reader is
// recoverEmptyContext: an emptied context holds no user message, so the turn
// is in no request the loop is about to send whether or not an earlier request
// carried it — a carrier's truncation retry is the carried case (#1365, F3).
// The rebuild's replay keeps the narrower "uncarried" predicate on purpose: a
// rebuilt context is the retained request's conversation, which already holds
// a carried turn.
func (m *LoopManager) deferredContinuationPrompt(loopID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entity, exists := m.loops[loopID]
	if !exists || !entity.PendingContinuation {
		return ""
	}
	return entity.PendingContinuationPrompt
}

// dropPendingContinuationPrompt removes a deferred turn's text from the loop's
// in-memory entity after the record refused it for size (#1365), so the loop's
// later record writes do not render it into the same refusal. It clears only
// while the entity still holds that turn: a later turn that replaced it is a
// different admission, with its own marker write to answer for it.
func (m *LoopManager) dropPendingContinuationPrompt(loopID, prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entity, exists := m.loops[loopID]; exists && entity.PendingContinuationPrompt == prompt {
		entity.PendingContinuationPrompt = ""
	}
}

// OutstandingRequest returns the one model request this loop is waiting on, or
// "" when it is waiting on none.
//
// It does not decide whether a response is SUPERSEDED — the record does that,
// ordering the response against published_request_id (#1330, I1). What it
// decides is the one case ordering cannot reach (owner ruling Q12,
// 2026-09-23): a response naming the request the record already calls current
// is the loop's outstanding FIRST delivery while the mark still names it, and
// once SettleRequest has cleared it the same bytes are a replay of an answer
// this process already applied — acknowledged without effect rather than
// appended to the conversation a second time. So the identity is what callers
// need and not the bare fact; and the guard is scoped to a response the record
// already calls current, because a tool-call response settles its request while
// the loop stays on that iteration — "waiting on nothing" is the ordinary
// mid-iteration state, never evidence on its own.
func (m *LoopManager) OutstandingRequest(loopID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.outstandingRequests[loopID]
}

// GetLoopForRequest retrieves the loop ID for a request ID
func (m *LoopManager) GetLoopForRequest(requestID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	loopID, exists := m.requestToLoop[requestID]
	return loopID, exists
}

// TrackToolCall associates a framework tool execution ID with a loop ID.
func (m *LoopManager) TrackToolCall(executionID, loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCallToLoop[executionID] = loopID
}

// TrackToolName associates a framework execution ID with its function name.
// This is used to populate the name field on tool result messages (required by Gemini).
func (m *LoopManager) TrackToolName(executionID, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executionIDToName[executionID] = name
}

// GetToolName retrieves the function name for a framework execution ID.
func (m *LoopManager) GetToolName(executionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.executionIDToName[executionID]
}

// TrackToolArguments associates a framework execution ID with its arguments.
// This is used to populate the ToolArguments field on trajectory steps for audit.
func (m *LoopManager) TrackToolArguments(executionID string, args map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executionIDToArguments[executionID] = args
}

// GetToolArguments retrieves arguments for a framework execution ID.
func (m *LoopManager) GetToolArguments(executionID string) map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	orig := m.executionIDToArguments[executionID]
	if orig == nil {
		return nil
	}
	cp := make(map[string]any, len(orig))
	maps.Copy(cp, orig)
	return cp
}

// TrackToolOrdinal records an execution's order in the model response.
func (m *LoopManager) TrackToolOrdinal(executionID string, ordinal uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executionIDToOrdinal[executionID] = ordinal
}

// GetToolOrdinal returns an execution's order in the model response.
func (m *LoopManager) GetToolOrdinal(executionID string) uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.executionIDToOrdinal[executionID]
}

// TrackRequestStart records when a model request was sent.
func (m *LoopManager) TrackRequestStart(requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestStartTimes[requestID] = time.Now()
}

// GetRequestStart retrieves the start time for a model request.
func (m *LoopManager) GetRequestStart(requestID string) time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.requestStartTimes[requestID]
}

// TrackToolStart records when a framework execution was dispatched.
func (m *LoopManager) TrackToolStart(executionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executionStartTimes[executionID] = time.Now()
}

// GetToolStart retrieves the start time for a framework execution.
func (m *LoopManager) GetToolStart(executionID string) time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.executionStartTimes[executionID]
}

// GetLoopForToolCall retrieves the loop ID for a framework tool execution ID.
func (m *LoopManager) GetLoopForToolCall(executionID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	loopID, exists := m.toolCallToLoop[executionID]
	return loopID, exists
}

// StoreToolResult stores a tool result in the loop entity for later retrieval
func (m *LoopManager) StoreToolResult(loopID string, result agentic.ToolResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	if entity.PendingToolResults == nil {
		entity.PendingToolResults = make(map[string]agentic.ToolResult)
	}
	resultKey := result.ExecutionID
	if resultKey == "" {
		// Internal synthetic failures predate framework execution correlation.
		// They never cross the tool-result routing boundary and remain scoped by
		// the owning loop's result map.
		resultKey = result.CallID
	}
	entity.PendingToolResults[resultKey] = result
	return nil
}

// GetAndClearToolResults retrieves all accumulated tool results and clears them.
// Also evicts the ExecutionID→loop routing entry for each drained result so a late
// re-delivery (NATS redelivery, executor retry) lands on an empty mapping at
// handleToolResultMessage and is dropped at the wire instead of leaking into
// the next turn's PendingToolResults — which would otherwise produce a
// duplicate tool message in the message array sent to the model.
//
// GetLoopForToolCallWithRecovery performs this same direct ExecutionID lookup
// without a provider-CallID fallback, so eviction makes late delivery unmapped.
// There is no stream scan, reconstructed route, tombstone set, or second ledger
// on this path. The existing map is the sole live demultiplexer, and draining a
// result removes its one entry. That keeps restart behavior grounded in durable
// input redelivery rather than an additional routing mechanism.
//
// Metadata maps (executionIDToName, executionIDToArguments,
// executionIDToOrdinal, executionStartTimes) are
// preserved — buildToolMessages's empty-name fallback and the trajectory step
// builder still read them. They grow O(total-tool-calls-in-loop) and are
// cleaned up at DeleteLoop while their execution route survives. Metadata whose
// route this method evicts has no remaining loop link and can outlive terminal
// release — the bounded residual DeleteLoop's doc records.
func (m *LoopManager) GetAndClearToolResults(loopID string) []agentic.ToolResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return nil
	}

	results := make([]agentic.ToolResult, 0, len(entity.PendingToolResults))
	for executionID, r := range entity.PendingToolResults {
		results = append(results, r)
		delete(m.toolCallToLoop, executionID)
	}
	entity.PendingToolResults = nil
	return results
}

// SetTimeout sets the timeout for a loop
func (m *LoopManager) SetTimeout(loopID string, timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	now := time.Now()
	entity.StartedAt = now
	entity.TimeoutAt = now.Add(timeout)
	return nil
}

// restoreDeadline puts a loop record's own StartedAt and TimeoutAt back onto
// the loop this process rebuilt from its task. The cold R1 arm runs the
// ordinary HandleTask, whose configureLoopMetadata stamps a FRESH deadline, and
// a rebuild is not a reprieve — "the loop's deadline means what its record
// says" (owner ruling #1330, 2026-09-23). Every other reconstruction seats the
// record wholesale and inherits both fields for free.
//
// Beside SetTimeout, and in place under this mutex, because these two fields
// have ONE writer. GetLoop → set → UpdateLoop would replace the whole entity,
// discarding whatever a sibling lane committed between the read and the write:
// by the time this runs the loop is registered and its first request tracked,
// so the response lane — a separate consumer — can be applying the
// predecessor's answer to it. That is a lost update, not a data race, and
// -race is blind to it.
//
// A record with no deadline overlays zero onto zero, which is what the
// wholesale seat gives and what IsTimedOut reads as "no deadline".
func (m *LoopManager) restoreDeadline(loopID string, startedAt, timeoutAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "restoreDeadline", "find loop")
	}

	entity.StartedAt = startedAt
	entity.TimeoutAt = timeoutAt
	return nil
}

// IsTimedOut checks if a loop has exceeded its timeout
func (m *LoopManager) IsTimedOut(loopID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return false
	}

	// If no timeout set, not timed out
	if entity.TimeoutAt.IsZero() {
		return false
	}

	return time.Now().After(entity.TimeoutAt)
}

// SetParentLoop sets the parent loop ID for tracking architect->editor relationships
func (m *LoopManager) SetParentLoop(loopID, parentLoopID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.ParentLoopID = parentLoopID
	return nil
}

// SetParentLoopID is an alias for SetParentLoop for consistency with TaskMessage field names
func (m *LoopManager) SetParentLoopID(loopID, parentLoopID string) error {
	return m.SetParentLoop(loopID, parentLoopID)
}

// SetRunID sets the run anchor (bare run loop-id) on the loop entity (ADR-053 D7).
// The run_id identifies which agent run this loop belongs to. Empty string is
// accepted to allow explicit clearing, though in practice it is only set
// when the TaskMessage carries a non-empty RunID.
func (m *LoopManager) SetRunID(loopID, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.RunID = runID
	return nil
}

// GetRunID returns the run anchor (bare run loop-id) for a loop, or the
// empty string when the loop is unknown or not part of a run (ADR-053 D7).
// Read-only counterpart to SetRunID; dispatch reads it to stamp the run
// anchor onto outgoing ToolCall.Metadata (issue #250). Returns "" rather
// than an error so the best-effort dispatch stamp stays branchless — an
// unknown loop and a runless loop are indistinguishable to the consumer
// (both mean "no run anchor"), and dispatch must never fail over it.
func (m *LoopManager) GetRunID(loopID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return ""
	}
	return entity.RunID
}

// GetRole returns the role of a loop (LoopEntity.Role), or the empty string
// when the loop is unknown or roleless. Read-only counterpart used by dispatch
// to stamp the agent role onto outgoing ToolCall.Metadata
// (agentic.MetadataKeyAgentRole) so tool executors can DERIVE role attribution
// (e.g. emit_lesson's agent.lesson.observed-role) without the model supplying a
// spoofable identity argument. Returns "" rather than an error so the
// best-effort dispatch stamp stays branchless — an unknown loop and a roleless
// loop are indistinguishable to the consumer (both mean "no role"), and
// dispatch must never fail over it.
func (m *LoopManager) GetRole(loopID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return ""
	}
	return entity.Role
}

// SetDepth sets the depth tracking for a loop in the multi-agent hierarchy
func (m *LoopManager) SetDepth(loopID string, depth, maxDepth int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.Depth = depth
	entity.MaxDepth = maxDepth
	return nil
}

// GetDepth returns the current depth and max depth for a loop
func (m *LoopManager) GetDepth(loopID string) (depth, maxDepth int, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return 0, 0, errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	return entity.Depth, entity.MaxDepth, nil
}

// SetWorkflowContext sets the workflow slug and step for loops created by workflow commands
func (m *LoopManager) SetWorkflowContext(loopID, workflowSlug, workflowStep string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.WorkflowSlug = workflowSlug
	entity.WorkflowStep = workflowStep
	return nil
}

// SetUserContext sets the user routing info for error notifications
func (m *LoopManager) SetUserContext(loopID, channelType, channelID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.ChannelType = channelType
	entity.ChannelID = channelID
	entity.UserID = userID
	return nil
}

// SetMetadata sets domain context metadata on the loop entity.
func (m *LoopManager) SetMetadata(loopID string, metadata map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	// Defensive copy to isolate from caller's map
	cp := make(map[string]any, len(metadata))
	for k, v := range metadata {
		cp[k] = v
	}
	entity.Metadata = cp
	return nil
}

// GenerateRequestID mints the deterministic identity of a logical model
// request. Format: loopID:req:iteration:retry (owner ruling Q4 on #1330,
// 2026-09-18; scope amendment on #1328).
//
// The loopID:req: prefix is unchanged, so ExtractLoopIDFromRequest and every
// agent.response.<requestID> subject keep working; only the suffix shape moved
// from a UUID to the two ordinals that name the work.
//
//   - iteration is the 1-based ordinal of the request within the loop:
//     LoopEntity.Iterations at mint time plus one. The first request of a loop
//     is :1:0; handleToolsComplete increments Iterations before it mints, so
//     the request that follows a tool batch takes the next ordinal. A loop this
//     manager does not know has not iterated, so its ordinal is 1.
//   - retry is the within-iteration truncation-retry ordinal, read back out of
//     the loop's own durable PublishedRequestID: a mint at the iteration that
//     field already names is a retry of it and takes the next retry ordinal;
//     any other mint is a new iteration at retry 0. A compaction retry of
//     iteration N is :N:1.
//
// Both inputs are facts this manager already holds, so no caller computes them
// and no caller can disagree with the state the loop is actually in. The
// determinism is what lets a redelivered task or tool batch republish the same
// request: agentic-model answers it from the retained response instead of
// calling the provider a second time, and the Nats-Msg-Id stamped from this ID
// lets the server reject the duplicate outright inside its window.
//
// The retry ordinal was process-local until #1330: after a process replacement
// mid-iteration the counter read zero, so a retry minted by the replacement
// took the name its predecessor had already published and the duplicate window
// dropped it. Reading it from the durable record is what closes that.
func (m *LoopManager) GenerateRequestID(loopID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	next := looprequest.ID{LoopID: loopID, Iteration: 1}
	entity, exists := m.loops[loopID]
	if !exists {
		return next.String()
	}
	next.Iteration = entity.Iterations + 1
	published, err := looprequest.Parse(entity.PublishedRequestID)
	if err == nil && published.LoopID == loopID && published.Iteration == next.Iteration {
		// Minting a second name for an iteration the record already names is
		// the truncation retry, and only that.
		next = looprequest.Next(published, true)
	}
	return next.String()
}

// publishedRetryOrdinal reports the within-iteration retry ordinal the loop's
// durable record already names, and zero when it names none. It is the budget
// the compaction self-heal spends: ordinal 0 means no retry of this iteration
// has been published, so one is still available.
//
// An empty or unparseable field answers zero. That is the pre-#1330 answer for
// a loop whose first request is still in flight, and it fails toward the
// behaviour the loop had before the field existed — one self-heal attempt —
// rather than toward refusing a recoverable truncation.
func (m *LoopManager) publishedRetryOrdinal(loopID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return 0
	}
	published, err := looprequest.Parse(entity.PublishedRequestID)
	if err != nil || published.LoopID != loopID || published.Iteration != entity.Iterations+1 {
		return 0
	}
	return published.Retry
}

// GenerateToolCallID creates a structured tool call ID that embeds the loop ID.
// Format: loopID:tool:shortUUID
// This allows recovery of loop ID from tool call ID if in-memory maps are lost.
func (m *LoopManager) GenerateToolCallID(loopID string) string {
	shortID := uuid.New().String()[:8]
	return fmt.Sprintf("%s:tool:%s", loopID, shortID)
}

// ExtractLoopIDFromRequest extracts the loop ID from a structured request ID.
// Returns empty string if the ID is not in structured format.
func (m *LoopManager) ExtractLoopIDFromRequest(requestID string) string {
	parts := strings.Split(requestID, ":req:")
	if len(parts) >= 1 && parts[0] != "" {
		return parts[0]
	}
	return ""
}

// ExtractLoopIDFromToolCall extracts the loop ID from a structured tool call ID.
// Returns empty string if the ID is not in structured format.
func (m *LoopManager) ExtractLoopIDFromToolCall(toolCallID string) string {
	parts := strings.Split(toolCallID, ":tool:")
	if len(parts) >= 1 && parts[0] != "" {
		return parts[0]
	}
	return ""
}

// GetLoopForRequestWithRecovery retrieves the loop ID for a request ID,
// attempting recovery from structured ID if not found in cache.
func (m *LoopManager) GetLoopForRequestWithRecovery(requestID string) (string, bool) {
	// Try cache first
	if loopID, exists := m.GetLoopForRequest(requestID); exists {
		return loopID, true
	}

	// Try to extract from structured ID
	if loopID := m.ExtractLoopIDFromRequest(requestID); loopID != "" {
		// Verify loop exists
		m.mu.RLock()
		_, exists := m.loops[loopID]
		m.mu.RUnlock()
		if exists {
			// Routing only. This is a READ that found the cache empty — for a
			// response that has already come back, among others — so the loop
			// is not learning that it is waiting on this request, it is
			// learning where the request's answer belongs.
			m.registerRequestRoute(requestID, loopID)
			return loopID, true
		}
	}

	return "", false
}

// GetLoopForToolCallWithRecovery retrieves the loop ID for a framework tool
// execution ID. Durable recovery is added at the committed response/result
// boundary; provider CallID is never a routing fallback.
func (m *LoopManager) GetLoopForToolCallWithRecovery(executionID string) (string, bool) {
	return m.GetLoopForToolCall(executionID)
}

// UpdateCompletion updates a loop with completion data (outcome, result, error).
// This is called when a loop finishes to populate fields for SSE delivery via KV watch.
func (m *LoopManager) UpdateCompletion(loopID, outcome, result, errMsg string) error {
	if !isValidOutcome(outcome) {
		return errs.WrapInvalid(fmt.Errorf("invalid outcome: %s", outcome), "LoopManager", "UpdateCompletion", "validate outcome")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop")
	}

	entity.Outcome = outcome
	entity.Result = result
	entity.Error = errMsg
	entity.CompletedAt = time.Now()
	return nil
}

// isValidOutcome checks if the outcome is one of the valid constants.
func isValidOutcome(outcome string) bool {
	switch outcome {
	case agentic.OutcomeSuccess, agentic.OutcomeFailed, agentic.OutcomeCancelled, agentic.OutcomeTruncated:
		return true
	default:
		return false
	}
}

// settleTerminal is the terminal owner's in-memory half of the entity write
// (#1362, design § 5.7): the entity the record is rendered from, just before
// the compare-and-swap that writes it terminal.
//
// The terminal transition clears the pending approval gate. A record that is
// terminal AND gated names a human decision nothing will ever apply; the
// transitions themselves (TransitionTo, CancelLoop) leave the gate in place,
// so the terminal owner is where it goes (L3's deferred item, archived
// durable-loop-authority design :50). The approval-timeout sweeper's terminal
// passes through here too; its auto-reject has already resolved the gate.
//
// adopted is the durable terminal the owner adopted, or nil when this
// delivery's own terminal was committed. Adopted, the entity is written to
// match the saved terminal's content rather than the candidate's; its kind is
// already this entity's, because adoption requires it.
//
// A loop this process no longer holds has nothing in memory to settle; the
// record write that follows answers for it (persistLoopState refuses to render
// a loop it cannot find).
func (m *LoopManager) settleTerminal(loopID string, adopted *terminalOutcome) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		return
	}
	entity.PendingApproval = nil
	entity.StateBeforeApproval = ""
	if adopted == nil {
		return
	}
	switch {
	case adopted.completed != nil:
		entity.Result = adopted.completed.Result
		entity.CompletedAt = adopted.completed.CompletedAt
	case adopted.failed != nil:
		entity.Error = adopted.failed.Error
		entity.CompletedAt = adopted.failed.FailedAt
	case adopted.cancelled != nil:
		entity.CancelledBy = adopted.cancelled.CancelledBy
		entity.CancelledAt = adopted.cancelled.CancelledAt
		entity.CompletedAt = adopted.cancelled.CancelledAt
	}
}

// CancelLoop atomically cancels a loop and populates completion data.
// Returns the updated entity for further processing, or an error if the loop
// cannot be cancelled (not found or already terminal).
func (m *LoopManager) CancelLoop(loopID, cancelledBy string) (agentic.LoopEntity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entity, exists := m.loops[loopID]
	if !exists {
		// The typed sentinel, not a bare error: the caller must be able to
		// tell "this process does not have it" from a transient failure,
		// because the two settle in opposite directions.
		return agentic.LoopEntity{}, errs.Wrap(
			fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound), "LoopManager", "CancelLoop", "find loop")
	}

	if entity.State.IsTerminal() {
		return agentic.LoopEntity{}, errs.WrapInvalid(
			fmt.Errorf("cannot cancel terminal loop %s in state %s", loopID, entity.State),
			"LoopManager",
			"CancelLoop",
			"check loop state",
		)
	}

	now := time.Now()
	entity.State = agentic.LoopStateCancelled
	entity.CancelledBy = cancelledBy
	entity.CancelledAt = now
	entity.Outcome = agentic.OutcomeCancelled
	entity.CompletedAt = now
	entity.Error = "cancelled by user"

	// Clear queued tool calls so no further tools are dispatched.
	delete(m.queuedToolCalls, loopID)

	return *entity, nil
}

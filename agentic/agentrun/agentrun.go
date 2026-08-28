// Package agentrun implements the AgentRun lifecycle Participant (ADR-053 D1–D6).
//
// An AgentRun represents the framework-level entity for a nested agentic loop
// tree: a coordinator loop that spawns research/architect/builder child loops
// forms a "run" whose lifecycle (dispatched → executing ⇄ awaiting_approval →
// terminal) is managed here via the pkg/lifecycle harness.
//
// The package provides:
//   - AgentRun — the lifecycle.Participant struct (D1)
//   - Register — registers the "agent-run" workflow with a lifecycle.Manager (D2)
//   - Mint — idempotent run creation at dispatch time (D4)
//   - ResolveRun — typed-first resolution with ancestry-walk fallback (D6)
//   - MilestoneSubscriber — subscribes to terminal loop events, pre-resolves the
//     run, and fans out to product-registered MilestoneHandlers (D6)
//
// Import discipline: this package imports agentic + pkg/lifecycle only.
// pkg/lifecycle MUST NOT import agentic/agentrun — verify with go mod graph.
package agentrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/agentterminal"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// agentRunTransitions is the declared phase graph for an AgentRun (ADR-053 D2).
//
//	dispatched ──> executing ──> awaiting_approval ──> executing (loop back)
//	dispatched ──> failed | cancelled
//	executing ──> completed | failed | cancelled
//	awaiting_approval ──> cancelled
//	completed, failed, cancelled: terminal
var agentRunTransitions = lifecycle.Transitions{
	"dispatched":        {"executing", "failed", "cancelled"},
	"executing":         {"awaiting_approval", "completed", "failed", "cancelled"},
	"awaiting_approval": {"executing", "cancelled"},
	"completed":         {},
	"failed":            {},
	"cancelled":         {},
}

// Predicates stamped by the Manager for agent-run entities.
const (
	// PhasePredicate is the triple carrying the run's current phase.
	PhasePredicate = "agent.run.phase"

	// predicateAuditSource carries the transition source (rule|operator|framework).
	predicateAuditSource = "agent.run.last-transition-source"

	// predicateAuditAt carries the RFC3339Nano timestamp of the last transition.
	predicateAuditAt = "agent.run.last-transition-at"

	// predicateAuditFrom carries the phase the entity transitioned out of.
	predicateAuditFrom = "agent.run.last-transition-from"

	// predicateAuditNote carries an optional free-text note on the transition.
	predicateAuditNote = "agent.run.last-transition-note"

	// predicateParentRunEntityID carries the parent run's full entity ID, if any.
	predicateParentRunEntityID = "agent.run.parent-entity-id"
)

func init() {
	for _, predicate := range []string{
		PhasePredicate,
		predicateAuditSource,
		predicateAuditAt,
		predicateAuditFrom,
		predicateAuditNote,
		predicateParentRunEntityID,
	} {
		vocabulary.Register(predicate)
	}
}

// WorkflowName is the registered workflow type name for agent-run entries.
// Matches what AgentRun.Workflow() returns.
const WorkflowName = "agent-run"

// EntityIDPattern matches agent-run chain execution entities in the federated graph.
// Six-segment shape per the federated EntityID contract in the canonical order
// org.platform.system.domain.type.instance; org + platform + instance are
// wildcarded; system (chain), domain (agent), type (execution) are pinned.
const EntityIDPattern = "*.*.chain.agent.execution.*"

// AgentRun is the lifecycle.Participant for an agent run (ADR-053 D1).
//
// Field tags:
//   - lifecycle:"id"                                         — entity identity (full 6-part ID, from KV key)
//   - lifecycle:"phase,predicate=agent.run.phase"            — current phase triple
//   - lifecycle:"predicate=agent.run.parent_entity_id"       — parent run entity ID triple
//
// D1 CRITICAL: EntityIDField MUST hold the FULL 6-part chain.execution entity ID
// because the projection layer populates it from the entity-state KEY (not a
// triple). A bare RunID would round-trip to the full dotted key and garble
// when passed back through TryChainExecutionEntityID (which rejects dots).
// RunID() derives the bare loop UUID from EntityIDField at read time.
type AgentRun struct {
	// EntityIDField is the full 6-part federated ID: org.platform.chain.agent.execution.<runID>
	// Tagged lifecycle:"id" so the projection layer populates it from the KV key, not a triple.
	EntityIDField string `json:"-" lifecycle:"id"`

	// PhaseField is the current lifecycle phase.
	PhaseField string `json:"phase" lifecycle:"phase,predicate=agent.run.phase"`

	// ParentRunEntityID is the parent run's full entity ID, or empty for root runs.
	ParentRunEntityID string `json:"parent_run_entity_id,omitempty" lifecycle:"predicate=agent.run.parent-entity-id"`
}

// EntityID returns the full 6-part federated entity ID.
// Implements lifecycle.Participant.
func (r *AgentRun) EntityID() string { return r.EntityIDField }

// Workflow returns the registered workflow type name.
// Implements lifecycle.Participant.
func (r *AgentRun) Workflow() string { return WorkflowName }

// Phase returns the current lifecycle phase.
// Implements lifecycle.Participant.
func (r *AgentRun) Phase() string { return r.PhaseField }

// IsTerminal returns true when the current phase has no declared out-edges.
// Consults the package-level agentRunTransitions table.
// Implements lifecycle.Participant.
func (r *AgentRun) IsTerminal() bool { return agentRunTransitions.IsTerminal(r.PhaseField) }

// ParentEntityID returns the parent run's full entity ID, or "" for root runs.
// Implements lifecycle.Participant.
func (r *AgentRun) ParentEntityID() string { return r.ParentRunEntityID }

// RunID derives the bare run loop-id from the full entity ID.
// Returns ("", false) when EntityIDField is not a valid chain.execution entity ID.
//
// The bare RunID == the dispatch-root loop UUID; it is NOT stored as a triple
// but derived from the instance position of the 6-part entity ID.
func (r *AgentRun) RunID() (string, bool) {
	return runIDFromChainEntityID(r.EntityIDField)
}

// runIDFromChainEntityID extracts the bare run loop-id (instance position) from
// a full chain.agent.execution entity ID, reading positions by NAME through
// pkg/types.ParseEntityID. Returns ("", false) for non-matching IDs.
func runIDFromChainEntityID(entityID string) (string, bool) {
	parsed, err := semtypes.ParseEntityID(entityID)
	if err != nil {
		return "", false
	}
	if parsed.System != "chain" || parsed.Domain != "agent" || parsed.Type != "execution" {
		return "", false
	}
	return parsed.Instance, true
}

// WorkflowDeclaration returns the lifecycle.Workflow ready to pass to
// Manager.Register (ADR-053 D2). Callers use Register(mgr) rather than
// calling this directly — it is exported for diagnostic/test use.
func WorkflowDeclaration() lifecycle.Workflow {
	return lifecycle.Workflow{
		Name:            WorkflowName,
		EntityIDPattern: EntityIDPattern,
		Phases: []string{
			"dispatched",
			"executing",
			"awaiting_approval",
			"completed",
			"failed",
			"cancelled",
		},
		Transitions:    agentRunTransitions,
		PhasePredicate: PhasePredicate,
		Schema:         reflect.TypeOf(AgentRun{}),
		AuditPredicates: lifecycle.AuditSpec{
			Source: predicateAuditSource,
			At:     predicateAuditAt,
			From:   predicateAuditFrom,
			Note:   predicateAuditNote,
		},
		// No OperatorWritablePredicates — run phase is closed to operator writes;
		// declared lifecycle_transition rule actions drive transitions.
	}
}

// Register declares the "agent-run" workflow to the given Manager (ADR-053 D2).
// Must be called at app startup before any create/transition calls land.
// Returns ErrWorkflowAlreadyRegistered (from lifecycle package) on duplicate registration.
func Register(mgr *lifecycle.Manager) error {
	return mgr.Register(WorkflowDeclaration())
}

// Mint creates (or retrieves if already exists) an AgentRun for the given
// rootLoopID (ADR-053 D4). The run's entity ID is
// org.platform.chain.agent.execution.<rootLoopID>, initial phase is "dispatched".
//
// Idempotent: if Manager.Create returns lifecycle.ErrAlreadyExists (the run was
// already minted — common on JetStream redelivery or concurrent rule firings),
// Mint treats it as success and returns the existing run via Manager.Get.
//
// NOTE: there is a narrow concurrent-create race (gh#178) where two goroutines
// both call Mint for the same ID simultaneously; both may observe ErrAlreadyExists
// but only one wrote the initial "dispatched" phase. The second caller falls back
// to Manager.Get which returns the already-minted run — this is correct: the run
// exists and is in a valid state. The gh#178 concern (which caller's Create "won")
// does not apply here because all callers want the same initial phase ("dispatched")
// and the run entity is immutable in terms of identity.
func Mint(ctx context.Context, mgr MintableManager, org, platform, rootLoopID string) (*AgentRun, error) {
	entityID, err := agentic.TryChainExecutionEntityID(org, platform, rootLoopID)
	if err != nil {
		return nil, fmt.Errorf("agentrun.Mint: build entity ID: %w", err)
	}

	initial := &AgentRun{
		EntityIDField: entityID,
		PhaseField:    "dispatched",
	}
	if err := mgr.Create(ctx, initial); err != nil {
		if errors.Is(err, lifecycle.ErrAlreadyExists) {
			// Run was already minted (idempotent path). Return existing.
			existing, getErr := mgr.Get(ctx, WorkflowName, entityID)
			if getErr != nil {
				return nil, fmt.Errorf("agentrun.Mint: already-exists Get: %w", getErr)
			}
			run, ok := existing.(*AgentRun)
			if !ok {
				return nil, fmt.Errorf("agentrun.Mint: Manager.Get returned unexpected type %T", existing)
			}
			return run, nil
		}
		return nil, fmt.Errorf("agentrun.Mint: Manager.Create: %w", err)
	}
	return initial, nil
}

// MintableManager is the narrowest interface Mint requires — Create + Get only.
// This lets the rule engine's LifecycleManager satisfy the Mint call without
// requiring Transition (which the rule path never uses for minting).
// Production callers use *lifecycle.Manager. The rule engine's
// LifecycleManager satisfies MintableManager.
type MintableManager interface {
	// Get reads the entity at entityID for the given workflow.
	Get(ctx context.Context, workflow, entityID string) (lifecycle.Participant, error)
	// Create attaches lifecycle to the entity. Returns lifecycle.ErrAlreadyExists
	// when already lifecycle-managed.
	Create(ctx context.Context, initial lifecycle.Participant) error
}

// LoopTripleReader is the narrow interface ResolveRun requires to read
// entity triples. A concrete *natsclient.Client or any adapter satisfies this.
// Defined here so callers can mock resolution in tests without a live NATS server.
type LoopTripleReader interface {
	// GetLoopRunID reads the agent.loop.run triple from the given loop entity ID.
	// Returns ("", false, nil) when the triple is absent (not an error — the
	// loop simply has no run association).
	// Returns ("", false, err) on read failures (NATS, decode errors).
	GetLoopRunID(ctx context.Context, loopEntityID string) (runID string, ok bool, err error)

	// GetLoopParentEntityID reads the agent.loop.parent triple from the given
	// loop entity ID. Returns ("", false, nil) when absent.
	GetLoopParentEntityID(ctx context.Context, loopEntityID string) (parentEntityID string, ok bool, err error)
}

// maxAncestryHops is the maximum number of ancestor hops the fallback walk
// follows before giving up. Bounded to prevent cycles from stalling indefinitely.
const maxAncestryHops = 32

// ResolveRun resolves the AgentRun for a given loop (ADR-053 D6).
//
// Resolution order:
//  1. Typed-first: read the loop entity's agent.loop.run triple (the RunID stamped at
//     spawn by LoopExecutionEntity.Triples()). If present, construct the run entity ID
//     directly.
//  2. Ancestry-walk fallback (for pre-migration / un-threaded loops): walk
//     agent.loop.parent triples up to the root (bounded at maxAncestryHops), then
//     use the root loop's ID as the run ID. Logs WARN on the fallback path so
//     un-threaded loops are visible in operator dashboards.
//
// Returns lifecycle.ErrEntityNotFound when neither the typed path nor the walk
// can locate a valid run entity.
func ResolveRun(ctx context.Context, runs RunStateReader, reader LoopTripleReader, org, platform, loopID string) (*AgentRun, error) {
	logger := slog.Default()

	loopEntityID, err := agentic.TryLoopExecutionEntityID(org, platform, loopID)
	if err != nil {
		return nil, fmt.Errorf("agentrun.ResolveRun: build loop entity ID: %w", err)
	}

	// -- Path 1: typed agent.loop.run triple --
	runID, ok, err := reader.GetLoopRunID(ctx, loopEntityID)
	if err != nil {
		return nil, fmt.Errorf("agentrun.ResolveRun: read agent.loop.run triple: %w", err)
	}
	if ok && runID != "" {
		runEntityID, err := agentic.TryChainExecutionEntityID(org, platform, runID)
		if err != nil {
			return nil, fmt.Errorf("agentrun.ResolveRun: build run entity ID from triple: %w", err)
		}
		participant, err := runs.Get(ctx, WorkflowName, runEntityID)
		if err != nil {
			return nil, fmt.Errorf("agentrun.ResolveRun: Manager.Get: %w", err)
		}
		run, ok := participant.(*AgentRun)
		if !ok {
			return nil, fmt.Errorf("agentrun.ResolveRun: Manager.Get returned unexpected type %T", participant)
		}
		return run, nil
	}

	// -- Path 2: ancestry-walk fallback --
	// Walk agent.loop.parent triples to the root (the loop with no parent).
	// The root loop's bare ID is the run ID for pre-migration loops.
	logger.Warn("agentrun.ResolveRun: agent.loop.run triple absent — falling back to ancestry walk",
		slog.String("loop_id", loopID),
		slog.String("org", org),
		slog.String("platform", platform))

	currentEntityID := loopEntityID
	currentLoopID := loopID
	for hop := 0; hop < maxAncestryHops; hop++ {
		parentEntityID, hasParent, err := reader.GetLoopParentEntityID(ctx, currentEntityID)
		if err != nil {
			return nil, fmt.Errorf("agentrun.ResolveRun: ancestry walk hop %d: %w", hop, err)
		}
		if !hasParent || parentEntityID == "" {
			// currentLoopID is the root loop — treat as run ID.
			runEntityID, err := agentic.TryChainExecutionEntityID(org, platform, currentLoopID)
			if err != nil {
				return nil, fmt.Errorf("agentrun.ResolveRun: build run entity ID from ancestry root: %w", err)
			}
			participant, err := runs.Get(ctx, WorkflowName, runEntityID)
			if err != nil {
				return nil, fmt.Errorf("agentrun.ResolveRun: Manager.Get (ancestry root): %w", err)
			}
			run, ok := participant.(*AgentRun)
			if !ok {
				return nil, fmt.Errorf("agentrun.ResolveRun: Manager.Get returned unexpected type %T", participant)
			}
			return run, nil
		}
		// Step up to the parent. Extract the bare loopID from the parent's entity ID
		// (the parent is a loop-execution entity, not a chain entity).
		parentLoopID, ok := agentic.LoopIDFromExecutionEntityID(parentEntityID)
		if !ok {
			// parentEntityID is not a loop-execution entity — ancestry walk cannot proceed.
			return nil, fmt.Errorf("agentrun.ResolveRun: ancestry walk hop %d: parent entity %q is not a loop-execution entity ID; cannot continue walk",
				hop, parentEntityID)
		}
		currentEntityID = parentEntityID
		currentLoopID = parentLoopID
	}

	return nil, fmt.Errorf("agentrun.ResolveRun: ancestry walk exceeded %d hops without reaching root for loop %q", maxAncestryHops, loopID)
}

// LoopTerminalEvent carries the terminal event data passed to MilestoneHandlers.
// Product handlers receive this along with the pre-resolved *AgentRun.
type LoopTerminalEvent struct {
	// LoopID is the bare loop UUID that terminated.
	LoopID string
	// RunID is the bare run loop-id from the event wire (ADR-053 D8).
	RunID string
	// RunEntityID is the full 6-part chain execution entity ID from the wire.
	RunEntityID string
	// Category is the agentic message category that identifies the event type:
	// CategoryLoopCompleted, CategoryLoopFailed, or CategoryLoopCancelled.
	// Cancellation rides agent.complete (not agent.cancelled), so callers MUST
	// demux by Category, not by subject.
	Category string
	// Outcome is the loop outcome string (success/failed/cancelled/truncated).
	Outcome string
	// Role is the loop's role.
	Role string
}

// MilestoneHandler is the product-registered handler for terminal loop events.
// Implementations receive the pre-resolved *AgentRun. Handlers that need graph
// mutations must emit work through a component's declared mutation port; the
// milestone subscriber owns no hidden graph-write capability.
type MilestoneHandler interface {
	OnLoopTerminal(ctx context.Context, ev LoopTerminalEvent, run *AgentRun) error
}

// RunStateReader is the read-only lifecycle surface used to resolve an
// AgentRun. The milestone subscriber observes terminal events and run state; it
// has no lifecycle mutation capability.
type RunStateReader interface {
	// Get reads the entity at entityID for the given workflow and projects its
	// triples into a fresh Participant. Returns lifecycle.ErrEntityNotFound when absent.
	Get(ctx context.Context, workflow, entityID string) (lifecycle.Participant, error)
}

// MilestoneSubscriber decodes terminal loop events from NATS (agent.complete.*
// and agent.failed.*), pre-resolves the run, and fans out to registered handlers
// (ADR-053 D6). It observes run state but never mutates lifecycle phase.
type MilestoneSubscriber struct {
	runs     RunStateReader
	reader   LoopTripleReader
	handlers []MilestoneHandler
	org      string
	platform string
	logger   *slog.Logger
	decoder  *message.Decoder
}

// NewMilestoneSubscriber constructs a subscriber wired to the given concrete
// lifecycle.Manager, reader, org, and platform. Handlers are
// registered separately via AddHandler. A nil logger falls back to slog.Default.
//
// Production callers use this constructor with a *lifecycle.Manager.
// Test callers use NewMilestoneSubscriberWithRunStateReader with a fake reader.
func NewMilestoneSubscriber(
	mgr *lifecycle.Manager,
	reader LoopTripleReader,
	org, platform string,
	logger *slog.Logger,
) *MilestoneSubscriber {
	return NewMilestoneSubscriberWithRunStateReader(mgr, reader, org, platform, logger)
}

// NewMilestoneSubscriberWithRunStateReader constructs a subscriber with a
// read-only run-state dependency. Prefer NewMilestoneSubscriber for production
// callers.
func NewMilestoneSubscriberWithRunStateReader(
	runs RunStateReader,
	reader LoopTripleReader,
	org, platform string,
	logger *slog.Logger,
) *MilestoneSubscriber {
	if logger == nil {
		logger = slog.Default()
	}
	reg := payloadregistry.New()
	if err := agentic.RegisterPayloads(reg); err != nil {
		panic(fmt.Sprintf("agentrun: register terminal payloads: %v", err))
	}
	return &MilestoneSubscriber{
		runs:     runs,
		reader:   reader,
		org:      org,
		platform: platform,
		logger:   logger,
		decoder:  message.NewDecoder(reg),
	}
}

// AddHandler registers a product MilestoneHandler. Must be called before
// HandleEvent is invoked. Thread-safe with respect to concurrent HandleEvent
// calls only when called before the subscriber is started.
func (s *MilestoneSubscriber) AddHandler(h MilestoneHandler) {
	s.handlers = append(s.handlers, h)
}

// HandleEvent processes a raw NATS message payload from agent.complete.* or
// agent.failed.* subjects. It decodes the BaseMessage envelope, demuxes by
// payload category (not subject — cancellation rides agent.complete), resolves
// the AgentRun and fans out to handlers. Lifecycle mutations remain the
// coordinator/component's responsibility through declared ports.
//
// Panic guard: each handler invocation is wrapped in a recover so a panicking
// product handler does not crash the subscriber goroutine.
//
// Returns an error only for infrastructure failures (decode, NATS). Handler
// errors are logged but do not propagate — the subscriber continues processing
// subsequent events.
func (s *MilestoneSubscriber) HandleEvent(ctx context.Context, data []byte) error {
	normalized, err := agentterminal.Decode(s.decoder, data)
	if err != nil {
		return fmt.Errorf("agentrun: HandleEvent: normalize terminal: %w", err)
	}
	ev := LoopTerminalEvent{
		LoopID:      normalized.LoopID,
		RunID:       normalized.RunID,
		RunEntityID: normalized.RunEntityID,
		Category:    normalized.Category,
		Outcome:     normalized.Outcome,
		Role:        normalized.Role,
	}

	// Resolve the run. Prefer the wire RunID (D8 typed path); fall back to walk.
	run, err := s.resolveRunForEvent(ctx, ev)
	if err != nil {
		// Resolution failure: log but do not crash the subscriber.
		s.logger.Warn("agentrun: HandleEvent: run resolution failed — skipping handlers",
			slog.String("loop_id", ev.LoopID),
			slog.String("run_id", ev.RunID),
			slog.String("category", ev.Category),
			slog.Any("error", err))
		return nil
	}

	// Fan out to product handlers with panic guard.
	for i, h := range s.handlers {
		func(idx int, handler MilestoneHandler) {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error("agentrun: MilestoneHandler panicked",
						slog.Int("handler_index", idx),
						slog.String("loop_id", ev.LoopID),
						slog.Any("panic", r))
				}
			}()
			if handlerErr := handler.OnLoopTerminal(ctx, ev, run); handlerErr != nil {
				s.logger.Warn("agentrun: MilestoneHandler error",
					slog.Int("handler_index", idx),
					slog.String("loop_id", ev.LoopID),
					slog.Any("error", handlerErr))
			}
		}(i, h)
	}

	return nil
}

// resolveRunForEvent resolves the AgentRun for the event. Uses the wire RunEntityID
// when available (fast path, no walk needed), then falls back to ResolveRun
// (which does the typed triple lookup + ancestry walk).
func (s *MilestoneSubscriber) resolveRunForEvent(ctx context.Context, ev LoopTerminalEvent) (*AgentRun, error) {
	// Fast path: RunEntityID is on the wire (ADR-053 D8 typed propagation).
	if ev.RunEntityID != "" {
		participant, err := s.runs.Get(ctx, WorkflowName, ev.RunEntityID)
		if err != nil {
			// Run entity not found — may be a non-run loop. Log and return nil run.
			s.logger.Debug("agentrun: resolveRunForEvent: Manager.Get from wire RunEntityID failed",
				slog.String("run_entity_id", ev.RunEntityID),
				slog.String("loop_id", ev.LoopID),
				slog.Any("error", err))
			return nil, nil //nolint:nilerr // deliberate: non-run loops have no run entity
		}
		run, ok := participant.(*AgentRun)
		if !ok {
			return nil, fmt.Errorf("Manager.Get returned unexpected type %T", participant)
		}
		return run, nil
	}

	if ev.LoopID == "" {
		return nil, nil
	}

	// Slow path: use ResolveRun (typed triple + ancestry walk).
	run, err := ResolveRun(ctx, s.runs, s.reader, s.org, s.platform, ev.LoopID)
	if err != nil {
		if errors.Is(err, lifecycle.ErrEntityNotFound) {
			// No run entity — this loop is not in a run. Return nil, no error.
			return nil, nil
		}
		return nil, err
	}
	return run, nil
}

// AgentStreamName is the default JetStream stream name for agentic events.
// Matches agentic-loop config.go default ("AGENT").
const AgentStreamName = "AGENT"

// StartConfig configures the durable JetStream consumers created by Start.
// Zero-value is invalid — callers must supply a non-empty StreamName.
type StartConfig struct {
	// StreamName is the JetStream stream that holds agent.complete.* and
	// agent.failed.* messages (default "AGENT"). Must match the agentic-loop's
	// stream_name config value.
	StreamName string

	// ConsumerNameSuffix is appended to the stable durable consumer names to
	// disambiguate instances (e.g. test-specific suffixes). Empty = no suffix.
	ConsumerNameSuffix string
}

type milestoneConsumerOwner struct {
	mu              sync.Mutex
	complete        jetstream.ConsumeContext
	failed          jetstream.ConsumeContext
	completeDrained bool
	failedDrained   bool
	running         bool
	stopping        bool
	completed       bool
	cancel          context.CancelFunc
}

func (o *milestoneConsumerOwner) stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("agentrun: milestone consumer stop: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	if o.completed {
		o.mu.Unlock()
		return nil
	}
	if o.stopping {
		o.mu.Unlock()
		return semerrs.WrapTransient(errors.New("milestone consumer stop already in progress"),
			"MilestoneSubscriber", "Stop", "concurrent Stop is unsupported")
	}
	o.stopping = true
	complete := o.complete
	failed := o.failed
	running := o.running
	drainComplete := complete != nil && !o.completeDrained
	drainFailed := failed != nil && !o.failedDrained
	o.completeDrained = o.completeDrained || drainComplete
	o.failedDrained = o.failedDrained || drainFailed
	o.mu.Unlock()

	// Both running handles begin Drain before either exact Closed wait.
	if drainComplete {
		complete.Drain()
	}
	if drainFailed {
		failed.Drain()
	}
	var stopErrors []error
	if complete != nil {
		stopErrors = append(stopErrors, waitMilestoneConsumerClosed(ctx, complete.Closed(), "complete"))
	}
	if failed != nil {
		stopErrors = append(stopErrors, waitMilestoneConsumerClosed(ctx, failed.Closed(), "failed"))
	}
	stopErr := errors.Join(stopErrors...)
	if stopErr != nil && running {
		// Running Stop is terminal. Force local closure best-effort and never
		// manufacture later rejoin authority for this generation.
		if complete != nil {
			complete.Stop()
		}
		if failed != nil {
			failed.Stop()
		}
	}
	o.cancel()

	o.mu.Lock()
	if running || stopErr == nil {
		o.complete = nil
		o.failed = nil
		o.completed = true
	}
	o.stopping = false
	o.mu.Unlock()
	return stopErr
}

func waitMilestoneConsumerClosed(ctx context.Context, closed <-chan struct{}, name string) error {
	select {
	case <-closed:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for %s milestone consumer Closed: %w", name, ctx.Err())
	}
}

// Start wires the MilestoneSubscriber to the live NATS connection using DURABLE
// JetStream consumers. agent.complete.* and agent.failed.* are published into the
// AGENT JetStream stream by the agentic-loop component; core Subscribe would drop
// events during subscriber downtime, violating milestone event delivery.
//
// Two stable durable consumers are created (one per filter subject). They survive
// subscriber restarts and resume from the last-acked message.
//
// cfg.StreamName must be non-empty (use AgentStreamName as the default).
// The ctx controls callback authority. Stop Drains both native handles, awaits
// both exact Closed signals while that authority remains live, and then cancels
// it. Durable consumer offsets remain in NATS for restart recovery.
//
// If the second acquisition fails, Start synchronously rolls back the first.
// Successful rollback returns no cleanup closure. Failed rollback returns one
// opaque closure that retains the exact handle and may re-await Closed later,
// but never initiates Drain more than once.
func (s *MilestoneSubscriber) Start(
	ctx context.Context,
	client *natsclient.Client,
	cfg StartConfig,
) (stop func(context.Context) error, err error) {
	if ctx == nil {
		return nil, errors.New("agentrun: MilestoneSubscriber.Start: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("agentrun: MilestoneSubscriber.Start: context ended: %w", err)
	}
	if cfg.StreamName == "" {
		return nil, fmt.Errorf("agentrun: MilestoneSubscriber.Start: StreamName must not be empty")
	}

	makeDurable := func(suffix string) string {
		name := "agentrun-milestone-" + suffix
		if cfg.ConsumerNameSuffix != "" {
			name += "-" + cfg.ConsumerNameSuffix
		}
		return name
	}
	completeConsumer := makeDurable("complete")
	failedConsumer := makeDurable("failed")
	runCtx, cancel := context.WithCancel(ctx)
	owner := &milestoneConsumerOwner{cancel: cancel}
	stop = owner.stop

	handleMsg := func(subject string) func(ctx context.Context, msg jetstream.Msg) {
		return func(msgCtx context.Context, msg jetstream.Msg) {
			handleErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, 10*time.Second, func(workCtx context.Context) error {
				if err := s.HandleEvent(workCtx, msg.Data()); err != nil {
					return natsclient.TerminateDelivery(err)
				}
				return nil
			})
			if handleErr != nil {
				s.logger.Warn("agentrun: MilestoneSubscriber: HandleEvent error",
					slog.String("subject", subject),
					slog.Any("error", handleErr))
			}
		}
	}

	completeCfg := natsclient.StreamConsumerConfig{
		StreamName:    cfg.StreamName,
		ConsumerName:  completeConsumer,
		FilterSubject: "agent.complete.*",
		AckPolicy:     "explicit",
		DeliverPolicy: "new",
		MaxDeliver:    5,
		AckWait:       30 * time.Second,
	}
	completeHandle, err := client.ConsumeInternalStreamWithConfig(
		runCtx, completeCfg, handleMsg("agent.complete.*"),
	)
	if err != nil {
		cancel()
		// Graceful no-op when the target stream is absent (gh#246). The
		// agent-run milestone subscriber only matters when agentic components
		// run — they create the AGENT stream and publish agent.complete.* /
		// agent.failed.*. A deployment without them (graph- or lifecycle-only)
		// has nothing to subscribe to and must still BOOT; before this, the
		// consumer start surfaced "stream not found" and both binaries
		// returned it from run() → os.Exit(1). The returned no-op stop is safe
		// to defer at the call site.
		//
		// The decision reads natsclient.ErrStreamNotVisible, NOT a bare
		// jetstream.ErrStreamNotFound. Three seams inside consumer setup can put
		// the absent classification into this chain — the guarded stream lookup,
		// consumer creation, and the initial consumer observation — and only the
		// first is a statement about whether the stream exists. Branching on the
		// bare classification disabled this subscriber for the process lifetime
		// when consumer CREATION answered not-found with the stream present, and
		// boot still reported success.
		//
		// The sentinel is produced only when the framework spent its entire
		// visibility budget re-observing a stream reported absent, and never when
		// the caller's context ended that wait — so an unguarded probe's 404 on a
		// lagging node (gh#1073) and a cancelled boot both fail closed here,
		// by construction rather than by the order of this if.
		if errors.Is(err, natsclient.ErrStreamNotVisible) {
			s.logger.Info("agentrun: MilestoneSubscriber disabled — stream not present",
				slog.String("stream", cfg.StreamName),
				slog.String("hint", "likely no agentic components in this deployment; the stream stayed "+
					"absent for the framework's whole stream-visibility budget, which is also why this "+
					"boot took that budget longer; agent.complete/failed milestones will not be processed"))
			return func(context.Context) error { return nil }, nil
		}
		return nil, fmt.Errorf("agentrun: MilestoneSubscriber: start durable consumer agent.complete.*: %w", err)
	}
	owner.mu.Lock()
	owner.complete = completeHandle
	owner.mu.Unlock()

	failedCfg := natsclient.StreamConsumerConfig{
		StreamName:    cfg.StreamName,
		ConsumerName:  failedConsumer,
		FilterSubject: "agent.failed.*",
		AckPolicy:     "explicit",
		DeliverPolicy: "new",
		MaxDeliver:    5,
		AckWait:       30 * time.Second,
	}
	failedHandle, err := client.ConsumeInternalStreamWithConfig(
		runCtx, failedCfg, handleMsg("agent.failed.*"),
	)
	if err != nil {
		// Deliberately NOT graceful, including for a not-found: the complete
		// consumer bound to this stream moments ago, so an absent stream here is
		// an inconsistency to surface, not a deployment without agentic
		// components.
		rollbackErr := lifecyclecleanup.RollbackFailedStart(ctx, stop)
		startErr := fmt.Errorf("agentrun: MilestoneSubscriber: start durable consumer agent.failed.*: %w", err)
		if rollbackErr == nil {
			return nil, startErr
		}
		return stop, errors.Join(startErr, rollbackErr)
	}
	owner.mu.Lock()
	owner.failed = failedHandle
	owner.running = true
	owner.mu.Unlock()

	// Stop cancels local consumption; durable state is preserved in NATS for restart recovery.
	return stop, nil
}

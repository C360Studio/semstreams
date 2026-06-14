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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/lifecycle"
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
	predicateAuditSource = "agent.run.last_transition_source"

	// predicateAuditAt carries the RFC3339Nano timestamp of the last transition.
	predicateAuditAt = "agent.run.last_transition_at"

	// predicateAuditFrom carries the phase the entity transitioned out of.
	predicateAuditFrom = "agent.run.last_transition_from"

	// predicateAuditNote carries an optional free-text note on the transition.
	predicateAuditNote = "agent.run.last_transition_note"

	// predicateParentRunEntityID carries the parent run's full entity ID, if any.
	predicateParentRunEntityID = "agent.run.parent_entity_id"
)

// WorkflowName is the registered workflow type name for agent-run entries.
// Matches what AgentRun.Workflow() returns.
const WorkflowName = "agent-run"

// EntityIDPattern matches agent-run chain execution entities in the federated graph.
// Six-segment shape per the federated EntityID contract; org + platform + instance
// are wildcarded; domain (agent), system (chain), type (execution) are pinned.
const EntityIDPattern = "*.*.agent.chain.execution.*"

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
	// EntityIDField is the full 6-part federated ID: org.platform.agent.chain.execution.<runID>
	// Tagged lifecycle:"id" so the projection layer populates it from the KV key, not a triple.
	EntityIDField string `json:"-" lifecycle:"id"`

	// PhaseField is the current lifecycle phase.
	PhaseField string `json:"phase" lifecycle:"phase,predicate=agent.run.phase"`

	// ParentRunEntityID is the parent run's full entity ID, or empty for root runs.
	ParentRunEntityID string `json:"parent_run_entity_id,omitempty" lifecycle:"predicate=agent.run.parent_entity_id"`
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
// but derived by parsing parts[5] from the 6-part entity ID.
func (r *AgentRun) RunID() (string, bool) {
	return runIDFromChainEntityID(r.EntityIDField)
}

// runIDFromChainEntityID extracts the bare run loop-id (instance segment) from
// a full chain.execution entity ID. Returns ("", false) for non-matching IDs.
func runIDFromChainEntityID(entityID string) (string, bool) {
	if !message.IsValidEntityID(entityID) {
		return "", false
	}
	// IsValidEntityID guarantees exactly 6 dot-separated parts.
	parts := strings.Split(entityID, ".")
	if len(parts) != 6 {
		return "", false
	}
	if parts[2] != "agent" || parts[3] != "chain" || parts[4] != "execution" {
		return "", false
	}
	return parts[5], true
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
		// only lifecycle_transition rule actions and the framework itself drive transitions.
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
// org.platform.agent.chain.execution.<rootLoopID>, initial phase is "dispatched".
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
// Production callers use *lifecycle.Manager which satisfies both MintableManager
// and MockableManager. The rule engine's LifecycleManager satisfies MintableManager.
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
	// GetLoopRunID reads the agent.run triple from the given loop entity ID.
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
//  1. Typed-first: read the loop entity's agent.run triple (the RunID stamped at
//     spawn by LoopExecutionEntity.Triples()). If present, construct the run entity ID
//     directly.
//  2. Ancestry-walk fallback (for pre-migration / un-threaded loops): walk
//     agent.loop.parent triples up to the root (bounded at maxAncestryHops), then
//     use the root loop's ID as the run ID. Logs WARN on the fallback path so
//     un-threaded loops are visible in operator dashboards.
//
// Returns lifecycle.ErrEntityNotFound when neither the typed path nor the walk
// can locate a valid run entity.
func ResolveRun(ctx context.Context, mgr MockableManager, reader LoopTripleReader, org, platform, loopID string) (*AgentRun, error) {
	logger := slog.Default()

	loopEntityID, err := agentic.TryLoopExecutionEntityID(org, platform, loopID)
	if err != nil {
		return nil, fmt.Errorf("agentrun.ResolveRun: build loop entity ID: %w", err)
	}

	// -- Path 1: typed agent.run triple --
	runID, ok, err := reader.GetLoopRunID(ctx, loopEntityID)
	if err != nil {
		return nil, fmt.Errorf("agentrun.ResolveRun: read agent.run triple: %w", err)
	}
	if ok && runID != "" {
		runEntityID, err := agentic.TryChainExecutionEntityID(org, platform, runID)
		if err != nil {
			return nil, fmt.Errorf("agentrun.ResolveRun: build run entity ID from triple: %w", err)
		}
		participant, err := mgr.Get(ctx, WorkflowName, runEntityID)
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
	logger.Warn("agentrun.ResolveRun: agent.run triple absent — falling back to ancestry walk (loop not yet migrated to ADR-053 run threading)",
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
			participant, err := mgr.Get(ctx, WorkflowName, runEntityID)
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

// TriplePublisher is the narrow interface MilestoneHandlers use to write
// milestone triples onto the run entity (or any entity) via graph-ingest.
// Reuse the TripleMutator interface shape from the rule action executor; a
// concrete natsclient-backed implementation satisfies this.
type TriplePublisher interface {
	// AddTriple writes a triple via graph-ingest. Returns the KV revision after write.
	AddTriple(ctx context.Context, ruleID string, triple message.Triple) (uint64, error)
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
// Implementations receive the pre-resolved *AgentRun and a TriplePublisher so
// they can stamp milestone triples directly onto the run entity via graph-ingest
// (NOT via Manager — see ADR-053 D5 for the cardinality contract).
type MilestoneHandler interface {
	OnLoopTerminal(ctx context.Context, ev LoopTerminalEvent, run *AgentRun, pub TriplePublisher) error
}

// MockableManager is the subset of *lifecycle.Manager that MilestoneSubscriber
// uses, expressed as an interface so tests can inject a mock without constructing
// a real Manager. Production callers use *lifecycle.Manager which satisfies this.
type MockableManager interface {
	// Get reads the entity at entityID for the given workflow and projects its
	// triples into a fresh Participant. Returns lifecycle.ErrEntityNotFound when absent.
	Get(ctx context.Context, workflow, entityID string) (lifecycle.Participant, error)
	// Transition moves the entity to newPhase. Used by the terminal-authority
	// zombie-prevention path (D3). Never called with Manager.Complete — use
	// an explicit terminal phase.
	Transition(ctx context.Context, workflow, entityID, newPhase string, source lifecycle.TransitionSource, note string) error
	// Create attaches lifecycle to the entity at initial.EntityID(). Returns
	// lifecycle.ErrAlreadyExists when already lifecycle-managed.
	Create(ctx context.Context, initial lifecycle.Participant) error
}

// MilestoneSubscriber decodes terminal loop events from NATS (agent.complete.*
// and agent.failed.*), pre-resolves the run, and fans out to registered handlers
// (ADR-053 D6, D3).
//
// Terminal authority (D3): the subscriber initiates a run → failed/cancelled
// transition ONLY when the terminating loop IS the run root AND the run is still
// in "dispatched" with no children (zombie prevention). Product/coordinator closes
// runs via lifecycle_transition rule actions in all other cases.
//
// Never call Manager.Complete — with executing→3 terminals it is non-deterministic
// (manager.go:671). Always use Manager.Transition with an explicit terminal.
type MilestoneSubscriber struct {
	mgr      MockableManager
	reader   LoopTripleReader
	pub      TriplePublisher
	handlers []MilestoneHandler
	org      string
	platform string
	logger   *slog.Logger
}

// NewMilestoneSubscriber constructs a subscriber wired to the given concrete
// lifecycle.Manager, reader, publisher, org, and platform. Handlers are
// registered separately via AddHandler. A nil logger falls back to slog.Default.
//
// Production callers use this constructor with a *lifecycle.Manager.
// Test callers use NewMilestoneSubscriberWithManager with a mock.
func NewMilestoneSubscriber(
	mgr *lifecycle.Manager,
	reader LoopTripleReader,
	pub TriplePublisher,
	org, platform string,
	logger *slog.Logger,
) *MilestoneSubscriber {
	return NewMilestoneSubscriberWithManager(mgr, reader, pub, org, platform, logger)
}

// NewMilestoneSubscriberWithManager constructs a subscriber with any
// MockableManager (real or test double). Prefer NewMilestoneSubscriber for
// production callers.
func NewMilestoneSubscriberWithManager(
	mgr MockableManager,
	reader LoopTripleReader,
	pub TriplePublisher,
	org, platform string,
	logger *slog.Logger,
) *MilestoneSubscriber {
	if logger == nil {
		logger = slog.Default()
	}
	return &MilestoneSubscriber{
		mgr:      mgr,
		reader:   reader,
		pub:      pub,
		org:      org,
		platform: platform,
		logger:   logger,
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
// the AgentRun, applies terminal-authority logic (D3), and fans out to handlers.
//
// Panic guard: each handler invocation is wrapped in a recover so a panicking
// product handler does not crash the subscriber goroutine.
//
// Returns an error only for infrastructure failures (decode, NATS). Handler
// errors are logged but do not propagate — the subscriber continues processing
// subsequent events.
func (s *MilestoneSubscriber) HandleEvent(ctx context.Context, data []byte) error {
	// Decode the BaseMessage envelope to extract the category.
	var envelope struct {
		Domain   string          `json:"domain"`
		Category string          `json:"category"`
		Version  string          `json:"version"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("agentrun: HandleEvent: decode envelope: %w", err)
	}

	var ev LoopTerminalEvent
	switch envelope.Category {
	case agentic.CategoryLoopCompleted:
		var completed agentic.LoopCompletedEvent
		if err := json.Unmarshal(envelope.Payload, &completed); err != nil {
			return fmt.Errorf("agentrun: HandleEvent: decode LoopCompletedEvent: %w", err)
		}
		ev = LoopTerminalEvent{
			LoopID:      completed.LoopID,
			RunID:       completed.RunID,
			RunEntityID: completed.RunEntityID,
			Category:    agentic.CategoryLoopCompleted,
			Outcome:     completed.Outcome,
			Role:        completed.Role,
		}

	case agentic.CategoryLoopCancelled:
		var cancelled agentic.LoopCancelledEvent
		if err := json.Unmarshal(envelope.Payload, &cancelled); err != nil {
			return fmt.Errorf("agentrun: HandleEvent: decode LoopCancelledEvent: %w", err)
		}
		ev = LoopTerminalEvent{
			LoopID:      cancelled.LoopID,
			RunID:       cancelled.RunID,
			RunEntityID: cancelled.RunEntityID,
			Category:    agentic.CategoryLoopCancelled,
			Outcome:     agentic.OutcomeCancelled,
			Role:        "",
		}

	case agentic.CategoryLoopFailed:
		var failed agentic.LoopFailedEvent
		if err := json.Unmarshal(envelope.Payload, &failed); err != nil {
			return fmt.Errorf("agentrun: HandleEvent: decode LoopFailedEvent: %w", err)
		}
		ev = LoopTerminalEvent{
			LoopID:      failed.LoopID,
			RunID:       failed.RunID,
			RunEntityID: failed.RunEntityID,
			Category:    agentic.CategoryLoopFailed,
			Outcome:     agentic.OutcomeFailed,
			Role:        failed.Role,
		}

	default:
		// Not a terminal loop event — skip silently. Other categories
		// (loop_created, context_event, etc.) may appear on agent.complete.*
		// in future; we only act on the terminal set.
		return nil
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

	// Terminal authority (D3): if the terminating loop IS the run root AND the run is
	// still in "dispatched" (no children handed off yet), transition the run to
	// failed/cancelled to prevent a zombie. This is the ONLY framework-initiated terminal
	// transition.
	//
	// The root check uses run.RunID() (derived from the resolved run entity), NOT ev.RunID
	// (the wire field). The wire RunID is populated from LoopEntity.RunID which is set at
	// spawn via SetRunID — but the root/coordinator loop itself was NOT spawned with a RunID
	// (it predates the run_scope:new rule firing). Instead, the run_scope=new path stamps
	// agent.run on the firing entity via tripleMutator; ResolveRun reads that triple and
	// resolves the run. The wire ev.RunID is therefore "" for root terminations — using it
	// as the identity check would make D3 unreachable.
	if run != nil && run.PhaseField == "dispatched" {
		if runID, ok := run.RunID(); ok && ev.LoopID == runID {
			terminalPhase := "failed"
			if ev.Category == agentic.CategoryLoopCancelled {
				terminalPhase = "cancelled"
			}
			reason := fmt.Sprintf("root loop %s terminated in %s before any child handoff", ev.LoopID, ev.Outcome)
			s.logger.Info("agentrun: HandleEvent: zombie prevention — transitioning dispatched root run to terminal",
				slog.String("run_entity_id", run.EntityIDField),
				slog.String("loop_id", ev.LoopID),
				slog.String("terminal_phase", terminalPhase))
			if transErr := s.mgr.Transition(ctx, WorkflowName, run.EntityIDField, terminalPhase,
				lifecycle.TransitionSourceFramework, reason); transErr != nil {
				s.logger.Warn("agentrun: HandleEvent: zombie-prevention transition failed",
					slog.String("run_entity_id", run.EntityIDField),
					slog.Any("error", transErr))
			}
		}
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
			if handlerErr := handler.OnLoopTerminal(ctx, ev, run, s.pub); handlerErr != nil {
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
		participant, err := s.mgr.Get(ctx, WorkflowName, ev.RunEntityID)
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
	run, err := ResolveRun(ctx, s.mgr, s.reader, s.org, s.platform, ev.LoopID)
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

// Start wires the MilestoneSubscriber to the live NATS connection using DURABLE
// JetStream consumers. agent.complete.* and agent.failed.* are published into the
// AGENT JetStream stream by the agentic-loop component; core Subscribe would drop
// events during subscriber downtime, violating D3/milestone restart-recovery.
//
// Two stable durable consumers are created (one per filter subject). They survive
// subscriber restarts and resume from the last-acked message.
//
// cfg.StreamName must be non-empty (use AgentStreamName as the default).
// The ctx controls the consumers' lifetime; Stop cancels consumption but preserves
// the durable consumer offsets in NATS for restart recovery.
//
// Returns a stop func that cancels consumption — call it on shutdown.
func (s *MilestoneSubscriber) Start(ctx context.Context, client *natsclient.Client, cfg StartConfig) (stop func(), err error) {
	if cfg.StreamName == "" {
		return nil, fmt.Errorf("agentrun: MilestoneSubscriber.Start: StreamName must not be empty")
	}

	// Graceful no-op when the target stream is absent (gh#246). The
	// agent-run milestone subscriber only matters when agentic components
	// run — they create the AGENT stream and publish agent.complete.* /
	// agent.failed.*. A deployment without them (graph- or lifecycle-only)
	// has nothing to subscribe to and must still BOOT; before this, the
	// consumer start surfaced "stream not found" and both binaries
	// returned it from run() → os.Exit(1). The returned no-op stop is safe
	// to defer at the call site.
	if _, streamErr := client.GetStream(ctx, cfg.StreamName); streamErr != nil {
		if errors.Is(streamErr, jetstream.ErrStreamNotFound) {
			s.logger.Info("agentrun: MilestoneSubscriber disabled — stream not present",
				slog.String("stream", cfg.StreamName),
				slog.String("hint", "likely no agentic components in this deployment (or the stream isn't created yet at boot); agent.complete/failed milestones will not be processed"))
			return func() {}, nil
		}
		return nil, fmt.Errorf("agentrun: MilestoneSubscriber.Start: check stream %q: %w", cfg.StreamName, streamErr)
	}

	makeDurable := func(suffix string) string {
		name := "agentrun-milestone-" + suffix
		if cfg.ConsumerNameSuffix != "" {
			name += "-" + cfg.ConsumerNameSuffix
		}
		return name
	}

	handleMsg := func(subject string) func(ctx context.Context, msg jetstream.Msg) {
		return func(msgCtx context.Context, msg jetstream.Msg) {
			if handleErr := s.HandleEvent(msgCtx, msg.Data()); handleErr != nil {
				s.logger.Warn("agentrun: MilestoneSubscriber: HandleEvent error",
					slog.String("subject", subject),
					slog.Any("error", handleErr))
				msg.Nak() //nolint:errcheck // best-effort nak; NATS will redeliver
				return
			}
			msg.Ack() //nolint:errcheck // best-effort ack; benign on double-ack
		}
	}

	completeConsumer := makeDurable("complete")
	completeCfg := natsclient.StreamConsumerConfig{
		StreamName:    cfg.StreamName,
		ConsumerName:  completeConsumer,
		FilterSubject: "agent.complete.*",
		AckPolicy:     "explicit",
		DeliverPolicy: "new",
		MaxDeliver:    5,
		AckWait:       30 * time.Second,
	}
	if err := client.ConsumeStreamWithConfig(ctx, completeCfg, handleMsg("agent.complete.*")); err != nil {
		return nil, fmt.Errorf("agentrun: MilestoneSubscriber: start durable consumer agent.complete.*: %w", err)
	}

	failedConsumer := makeDurable("failed")
	failedCfg := natsclient.StreamConsumerConfig{
		StreamName:    cfg.StreamName,
		ConsumerName:  failedConsumer,
		FilterSubject: "agent.failed.*",
		AckPolicy:     "explicit",
		DeliverPolicy: "new",
		MaxDeliver:    5,
		AckWait:       30 * time.Second,
	}
	if err := client.ConsumeStreamWithConfig(ctx, failedCfg, handleMsg("agent.failed.*")); err != nil {
		// Roll back the first consumer before returning error.
		client.StopConsumer(cfg.StreamName, completeConsumer)
		return nil, fmt.Errorf("agentrun: MilestoneSubscriber: start durable consumer agent.failed.*: %w", err)
	}

	// Stop cancels local consumption; durable state is preserved in NATS for restart recovery.
	return func() {
		client.StopConsumer(cfg.StreamName, completeConsumer)
		client.StopConsumer(cfg.StreamName, failedConsumer)
	}, nil
}

package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// EmitLessonToolName is the name agents use to invoke the ops agent's lesson
// distillation tool (ADR-080). Sibling of emit_diagnosis on the ADR-027 ops
// observation seam.
const EmitLessonToolName = "emit_lesson"

// defaultEmitLessonPerLoopCap bounds how many lessons a single ops loop may
// emit. Runaway protection: one ops loop legitimately emits several lessons
// (StopLoop:false), but an unbounded loop must not flood the graph. Over-cap is
// rejected with an instructive error naming the cap; no entity is created.
const defaultEmitLessonPerLoopCap = 20

// lessonBornStatus is the agent.lesson.status value every lesson is created
// with. Lessons are born `proposed`; only promotion to `active` (task 4.1)
// makes a lesson injectable at brief assembly. ADR-080 gated lifecycle.
const lessonBornStatus = "proposed"

// Writer-gate rejection reasons — the label values on the emit_lesson
// rejections counter (task 4.4). Each names one of the four ADR-080 decision-3
// writer gates so operators can see WHICH contract emitting agents trip on:
//   - evidence: missing / malformed evidence citation
//   - bound:    injection-form over the byte bound
//   - grammar:  applies_to typed-scope-key grammar / minimum specificity
//   - cap:      per-loop emission cap exhausted
//
// Other input-hygiene rejects (required field, polarity enum, control bytes)
// are not one of the four contract gates and are intentionally uncounted.
const (
	lessonRejectEvidence = "evidence"
	lessonRejectBound    = "bound"
	lessonRejectGrammar  = "grammar"
	lessonRejectCap      = "cap"
)

// emitLessonDefaultSeverity is applied when the caller omits severity or
// supplies a value outside the closed enum. Unlike polarity (meaning-bearing,
// rejected on mismatch), severity only affects brief-injection ordering, so an
// invalid value is clamped to the lowest urgency rather than bouncing the call —
// mirrors emit_diagnosis's severity clamp.
const emitLessonDefaultSeverity = "info"

// lessonNamespaceUUID is the fixed UUIDv5 namespace for content-derived lesson
// identities. It is lesson-specific so a UUIDv5 minted here can only collide
// with another lesson of identical identity content — never a foreign entity
// kind. A repeated emit therefore reaches the same strict-create conflict;
// this component decides explicitly how to handle that known identity.
var lessonNamespaceUUID = uuid.MustParse("2c5acb9b-8283-4b34-a4d1-4b1c9f8502ca")

// LessonStore is the narrow create+read surface emit_lesson needs. It is
// deliberately purpose-built (not TriplePublisher): the executor must know
// create-vs-dedup — content-derived identity means a re-emit hits an existing
// entity whose current polarity/severity/detail/status may differ from the
// call's args (first-write-wins on identity; a curator may have promoted or
// retired it) — so the tool result reflects the GRAPH, never the dropped call
// args. Production satisfies it with a natsclient adapter; tests use an
// in-memory fake.
type LessonStore interface {
	// CreateLesson births the lesson entity with the typed-origin envelope.
	// Returns created=true on a fresh birth, created=false (no error) when the
	// content-derived ID already exists (an idempotent re-emit; first-write-wins).
	CreateLesson(ctx context.Context, entityID string, msgType message.Type, triples []message.Triple) (created bool, err error)
	// ReadLessonStatus returns the persisted agent.lesson.status of the lesson
	// entity, with found=false when the entity is absent. Used only on the dedup
	// path to report the TRUE status instead of the just-emitted call's.
	ReadLessonStatus(ctx context.Context, entityID string) (status string, found bool, err error)
}

// EmitLessonExecutor is the ops agent's lesson distillation tool. Each call
// mints a content-derived {org}.{platform}.agent.lesson.record.{uuid5} entity
// born status="proposed", publishes one triple per predicate plus an
// agent.action.executed-by back-link to the ops loop, and returns StopLoop:false
// so one ops loop can distil multiple lessons. Content-derived identity makes
// re-emission idempotent — an identical lesson cannot mint a second entity.
type EmitLessonExecutor struct {
	store    LessonStore
	platform types.PlatformMeta
	logger   *slog.Logger

	// perLoopCap is the per-loop emission cap; 0 falls back to
	// defaultEmitLessonPerLoopCap. Overridable in tests (white-box) to avoid
	// emitting the full default before hitting the cap.
	perLoopCap int

	// recordRejection increments the writer-gate rejection counter (task 4.4)
	// for a gate reason. Defaulted by NewEmitLessonExecutor to the package
	// metrics singleton (nil-safe until the component first registers metrics);
	// overridable in tests (white-box) with a spy that captures reasons.
	recordRejection func(reason string)

	// mu guards emittedPerLoop. Tool calls within one loop are typically
	// sequential, but the executor is shared across loops/goroutines.
	mu             sync.Mutex
	emittedPerLoop map[string]int
}

// NewEmitLessonExecutor constructs the executor given a lesson store, the
// platform identity used to build entity IDs, and a logger for instrumentation.
func NewEmitLessonExecutor(store LessonStore, platform types.PlatformMeta, logger *slog.Logger) *EmitLessonExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &EmitLessonExecutor{
		store:           store,
		platform:        platform,
		logger:          logger,
		emittedPerLoop:  make(map[string]int),
		recordRejection: defaultLessonRejectionRecorder,
	}
}

// defaultLessonRejectionRecorder ticks the package emit_lesson rejection
// counter for a gate reason. It reads the package metrics singleton at CALL
// time (nil-safe): in production the agentic-tools Component constructs the
// singleton at boot (getMetrics) before any tool executes, so the counter is
// live by the first rejection; in a bare unit test with no component the
// singleton stays nil and this is a no-op (tests that assert reasons inject a
// spy instead).
func defaultLessonRejectionRecorder(reason string) {
	if metrics != nil {
		metrics.recordToolRejection(EmitLessonToolName, reason)
	}
}

// natsLessonStore adapts natsclient.Client to LessonStore. Birth migration to
// the canonical mutation client is part of the graph-mutation caller cutover;
// status reads already use the sole embedded exact-read adapter.
type natsLessonStore struct {
	client *graphmutation.Client
	reader graph.ExactEntityReader
}

const lessonQueryTimeout = 5 * time.Second

// NewNATSLessonStore builds a LessonStore backed by the shared graph
// mutation/query NATS surfaces. Wire this into the emit_lesson executor.
func NewNATSLessonStore(client *natsclient.Client) LessonStore {
	wire, _ := graphmutation.NewClient(client, lessonQueryTimeout)
	return &natsLessonStore{client: wire, reader: graph.NewExactEntityReader(client, lessonQueryTimeout)}
}

func (s *natsLessonStore) CreateLesson(ctx context.Context, entityID string, msgType message.Type, triples []message.Triple) (bool, error) {
	if s == nil || s.client == nil || s.reader == nil {
		return false, errors.New("graph mutation client is required")
	}
	_, err := s.client.Create(ctx, graph.CreateEntityRequest{
		Entity: &graph.EntityState{
			ID:          entityID,
			MessageType: msgType,
		},
		Triples: triples,
	})
	if err == nil {
		return true, nil
	}

	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Code != graph.ErrorCodeEntityExists {
		return false, fmt.Errorf("create lesson entity: %w", err)
	}

	// The deterministic lesson ID makes an identical re-emit converge on the
	// existing entity. Strict Create still reports the conflict; this component
	// exact-reads and verifies its own immutable identity before deciding that
	// the desired semantic result already exists. A foreign or mismatched entity
	// remains a hard collision.
	exact, readErr := s.reader.ReadExactEntity(ctx, entityID)
	if readErr != nil {
		return false, fmt.Errorf("verify existing lesson %s after create conflict: %w", entityID, readErr)
	}
	if exact == nil || exact.Entity == nil {
		return false, fmt.Errorf("verify existing lesson %s after create conflict: empty exact read", entityID)
	}
	if exact.Entity.MessageType != msgType {
		return false, fmt.Errorf(
			"lesson identity collision for %s: existing message type %q, want %q",
			entityID, exact.Entity.MessageType.Key(), msgType.Key())
	}
	if identityErr := requireSameLessonIdentity(exact.Entity.Triples, triples); identityErr != nil {
		return false, fmt.Errorf("lesson identity collision for %s: %w", entityID, identityErr)
	}
	return false, nil
}

type lessonIdentity struct {
	category  string
	summary   string
	evidence  []string
	appliesTo []string
}

func requireSameLessonIdentity(existing, requested []message.Triple) error {
	left, err := lessonIdentityFromTriples(existing)
	if err != nil {
		return fmt.Errorf("existing identity: %w", err)
	}
	right, err := lessonIdentityFromTriples(requested)
	if err != nil {
		return fmt.Errorf("requested identity: %w", err)
	}
	if left.category != right.category || left.summary != right.summary ||
		!equalStrings(left.evidence, right.evidence) || !equalStrings(left.appliesTo, right.appliesTo) {
		return errors.New("existing content-derived identity fields do not match the request")
	}
	return nil
}

func lessonIdentityFromTriples(triples []message.Triple) (lessonIdentity, error) {
	var identity lessonIdentity
	categoryCount := 0
	summaryCount := 0
	for _, triple := range triples {
		var target *string
		switch triple.Predicate {
		case agvocab.LessonCategory:
			categoryCount++
			target = &identity.category
		case agvocab.LessonSummary:
			summaryCount++
			target = &identity.summary
		case agvocab.LessonEvidence:
			value, ok := triple.Object.(string)
			if !ok || value == "" {
				return lessonIdentity{}, fmt.Errorf("predicate %s has a non-string or empty object", triple.Predicate)
			}
			identity.evidence = append(identity.evidence, value)
			continue
		case agvocab.LessonAppliesTo:
			value, ok := triple.Object.(string)
			if !ok || value == "" {
				return lessonIdentity{}, fmt.Errorf("predicate %s has a non-string or empty object", triple.Predicate)
			}
			identity.appliesTo = append(identity.appliesTo, value)
			continue
		default:
			continue
		}
		value, ok := triple.Object.(string)
		if !ok || value == "" {
			return lessonIdentity{}, fmt.Errorf("predicate %s has a non-string or empty object", triple.Predicate)
		}
		*target = value
	}
	if categoryCount != 1 || summaryCount != 1 || len(identity.evidence) == 0 || len(identity.appliesTo) == 0 {
		return lessonIdentity{}, fmt.Errorf(
			"requires one category, one summary, evidence, and applies-to (got category=%d summary=%d evidence=%d applies_to=%d)",
			categoryCount, summaryCount, len(identity.evidence), len(identity.appliesTo))
	}
	sort.Strings(identity.evidence)
	sort.Strings(identity.appliesTo)
	return identity, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *natsLessonStore) ReadLessonStatus(ctx context.Context, entityID string) (string, bool, error) {
	exact, err := s.reader.ReadExactEntity(ctx, entityID)
	if err != nil {
		var ce *errs.ClassifiedError
		if errors.As(err, &ce) && ce.Code == graph.ErrorCodeEntityNotFound {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read lesson entity %s: %w", entityID, err)
	}
	for _, tr := range exact.Entity.Triples {
		if tr.Predicate == agvocab.LessonStatus {
			if status, ok := tr.Object.(string); ok {
				return status, true, nil
			}
		}
	}
	return "", true, nil // entity found but carries no status triple
}

// ListTools describes the emit_lesson tool schema. The schema asks for INTENT
// only — summary/detail/injection_form/category/polarity/severity plus the
// evidence and scope sets. It deliberately carries NO identity parameters
// (loop, role, entity ID): attribution is derived by the backend from loop
// context, and identity is content-derived. This is the "signature asks for
// intent, backend derives structure" discipline.
func (e *EmitLessonExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name: EmitLessonToolName,
			Description: "Distil a durable, reusable lesson from completed work into the knowledge graph. " +
				"Use when a finished loop or chain reveals guidance worth applying to future work — a pitfall to avoid or a practice to repeat. " +
				"Call once per distinct lesson; you may call multiple times per loop. " +
				"Cite at least one evidence entity ID the lesson was derived from, and at least one typed scope key " +
				"(id:<entity-id-prefix of 3+ segments> or tag:<token>) so the lesson reaches the right future loops. " +
				"Keep injection_form short — it is rendered verbatim into future briefs; put the full explanation in detail. " +
				"The framework derives attribution (which loop, which role) automatically; do not pass identity fields.",
			Effect: agentic.ToolEffectMutating,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": map[string]any{
						"type":        "string",
						"description": "Short gist of the lesson (one line). Prose; be concise.",
					},
					"detail": map[string]any{
						"type":        "string",
						"description": "Full explanation of the lesson and why it holds. Unbounded prose.",
					},
					"injection_form": map[string]any{
						"type":        "string",
						"description": fmt.Sprintf("Compressed, imperative form rendered verbatim into future briefs. Must be at most %d bytes — keep it tight.", agentic.LessonInjectionFormMaxBytes),
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Open product-taxonomy classifier for the lesson (e.g. \"retention-policy\"). No fixed set.",
					},
					"polarity": map[string]any{
						"type":        "string",
						"enum":        []string{"avoid", "best_practice"},
						"description": "Directional stance: \"avoid\" (don't do X) or \"best_practice\" (do Y).",
					},
					"severity": map[string]any{
						"type":        "string",
						"enum":        []string{"info", "warning", "critical"},
						"description": "Optional. Urgency, used to order lessons in briefs. Defaults to \"info\".",
					},
					"evidence_entity_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"minItems":    agentic.LessonMinEvidence,
						"description": "Entity IDs (6-part) of loops, trajectories, or other graph entities that support this lesson. At least one required.",
					},
					"applies_to": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"minItems":    1,
						"description": "Typed scope keys controlling which future loops the lesson reaches: \"id:<entity-id-prefix of 3+ segments>\" or \"tag:<token>\". At least one required.",
					},
				},
				"required": []string{"summary", "detail", "injection_form", "category", "polarity", "evidence_entity_ids", "applies_to"},
			},
		},
	}
}

// emitLessonArgs is the parsed, gate-checked shape of the emit_lesson tool's
// Arguments. It carries no identity fields — those are derived.
type emitLessonArgs struct {
	Summary       string
	Detail        string
	InjectionForm string
	Category      string
	Polarity      string
	Severity      string
	Evidence      []string
	AppliesTo     []string
}

// emitLessonResult is serialised into the tool result Content. Status is the
// PERSISTED graph status (not the call's intent), and Created reports fresh
// birth vs idempotent dedup, so the emitting agent never sees a status that
// contradicts the graph on a re-emit.
type emitLessonResult struct {
	EntityID      string   `json:"entity_id"`
	Created       bool     `json:"created"`
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	Category      string   `json:"category"`
	Polarity      string   `json:"polarity"`
	Severity      string   `json:"severity"`
	Evidence      []string `json:"evidence"`
	AppliesTo     []string `json:"applies_to"`
	ObservedRole  string   `json:"observed_role,omitempty"`
	InjectionForm string   `json:"injection_form"`
}

// Execute routes the tool call to emitLesson; any other name is a routing bug.
func (e *EmitLessonExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != EmitLessonToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "EmitLessonExecutor", "Execute", "route tool")
	}
	return e.emitLesson(ctx, call)
}

func (e *EmitLessonExecutor) emitLesson(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	args, err := parseEmitLessonArgs(call.Arguments)
	if err != nil {
		return e.rejectLesson(call, err), nil
	}

	if call.LoopID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "emit_lesson invoked without a loop_id on the tool call; cannot build the executed-by back-link or attribute the lesson",
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(fmt.Errorf("tool call missing loop_id"), "EmitLessonExecutor", "emitLesson", "resolve loop entity")
	}

	// The lesson's contract is the entity's (ADR-103): build it first and let
	// AgentLessonEntity.Validate — the same gate BaseMessage.MarshalJSON
	// applies to every publisher — decide, BEFORE any budget is reserved.
	lessonID := uuid.NewSHA1(lessonNamespaceUUID, []byte(canonicalLessonContent(args))).String()
	loopEntityID, err := agentic.TryLoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("construct loop entity ID: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "EmitLessonExecutor", "emitLesson", "construct loop entity ID")
	}
	// observed_role is DERIVED from loop context (the framework-propagated tool
	// metadata), never taken from the caller's arguments.
	lesson := &agentic.AgentLessonEntity{
		Org: e.platform.Org, Platform: e.platform.Platform, ID: lessonID,
		Category: args.Category, Polarity: args.Polarity, Severity: args.Severity,
		Status: lessonBornStatus, CreatedAt: time.Now(),
		Summary: args.Summary, Detail: args.Detail, InjectionForm: args.InjectionForm,
		Evidence: args.Evidence, AppliesTo: args.AppliesTo,
		ObservedRole: deriveObservedRole(call.Metadata), ExecutedBy: loopEntityID,
	}
	if err := lesson.Validate(); err != nil {
		return e.rejectLesson(call, err), nil
	}

	// Per-loop emission cap (runaway protection). Reserve budget AFTER the
	// contract check (a rejected call must not consume budget) and BEFORE
	// publish; a publish failure releases it so a transient error never burns
	// the cap.
	loopCap, reserved := e.reserveEmission(call.LoopID)
	if !reserved {
		if e.recordRejection != nil {
			e.recordRejection(lessonRejectCap)
		}
		return agentic.ToolResult{
			CallID: call.ID,
			Error: fmt.Sprintf("emit_lesson per-loop cap of %d reached for this loop; distil fewer, higher-value lessons",
				loopCap),
			ErrorKind: agentic.ToolErrorPermission,
		}, nil
	}

	// Content-derived identity: UUIDv5 over the CANONICAL content string
	// (category + sorted applies_to + summary + sorted evidence). Sorting the
	// multi-valued sets makes re-emission order-insensitive; polarity, severity,
	// detail, and injection_form are NOT part of identity, so refining them
	// re-mints the same entity (idempotent). ADR-080 decision 3. The registered
	// lesson entity is the one builder of its triples (ADR-103).
	lessonEntityID := lesson.EntityID()
	triples := lesson.Triples()

	// BIRTH via entity.create with the typed-origin envelope. Append is
	// must-exist, so an append to a never-created lesson returns not-found.
	// Re-emitting an
	// identical lesson derives the SAME entity ID; the create then hits
	// EntityExists, which the store reports as created=false (idempotent dedup),
	// not an error. Safe because the lesson namespace guarantees any collision is
	// a genuine identical-lesson re-emit, not a foreign-type ID.
	created, err := e.store.CreateLesson(ctx, lessonEntityID, agentic.AgentLessonMessageType(), triples)
	if err != nil {
		e.releaseEmission(call.LoopID)
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("publish lesson triples: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, errs.WrapTransient(err, "EmitLessonExecutor", "emitLesson", "birth lesson entity")
	}

	// Report the PERSISTED status, not the call's intent. A fresh birth is
	// definitionally `proposed`. On a dedup the entity already existed and this
	// call's polarity/severity/detail/status were dropped (first-write-wins on
	// identity); a curator may have promoted or retired it, so read the true
	// status back rather than assert a possibly-stale `proposed`. A failed
	// read-back reports an empty (unverified) status alongside created=false —
	// honest, never contradicting.
	status := lessonBornStatus
	if !created {
		s, found, rerr := e.store.ReadLessonStatus(ctx, lessonEntityID)
		switch {
		case rerr != nil:
			e.logger.Warn("emit_lesson: read-back of deduped lesson status failed; reporting unverified status",
				slog.String("entity_id", lessonEntityID), slog.Any("err", rerr))
			status = ""
		case !found:
			// Deduped but now absent (a concurrent delete): status unknown.
			status = ""
		default:
			status = s
		}
	}

	result := emitLessonResult{
		EntityID:      lessonEntityID,
		Created:       created,
		Status:        status,
		Summary:       args.Summary,
		Category:      args.Category,
		Polarity:      args.Polarity,
		Severity:      args.Severity,
		Evidence:      args.Evidence,
		AppliesTo:     args.AppliesTo,
		ObservedRole:  lesson.ObservedRole,
		InjectionForm: args.InjectionForm,
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal result payload: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "EmitLessonExecutor", "emitLesson", "marshal payload")
	}

	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  string(payload),
		StopLoop: false, // ops agents distil multiple lessons per loop
		Metadata: map[string]any{
			"lesson_id":      lessonEntityID,
			"lesson_status":  status,
			"lesson_created": created,
		},
	}, nil
}

// reserveEmission reserves one unit of per-loop emission budget for loopID.
// Returns the effective cap and whether the reservation succeeded (false when
// the cap is already reached). Increments the counter on success; callers
// release on publish failure.
func (e *EmitLessonExecutor) reserveEmission(loopID string) (int, bool) {
	effectiveCap := e.perLoopCap
	if effectiveCap <= 0 {
		effectiveCap = defaultEmitLessonPerLoopCap
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.emittedPerLoop == nil {
		e.emittedPerLoop = make(map[string]int)
	}
	if e.emittedPerLoop[loopID] >= effectiveCap {
		return effectiveCap, false
	}
	e.emittedPerLoop[loopID]++
	return effectiveCap, true
}

// releaseEmission returns one unit of per-loop budget when a reserved emission
// did not land (publish failure), so a transient error never permanently burns
// a loop's cap budget.
func (e *EmitLessonExecutor) releaseEmission(loopID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.emittedPerLoop[loopID] > 0 {
		e.emittedPerLoop[loopID]--
	}
}

// rejectLesson turns a contract or shape failure into the tool result the
// agent sees, counting the ADR-080 writer-gate rejections (evidence / bound /
// grammar) and leaving input-hygiene rejects uncounted.
func (e *EmitLessonExecutor) rejectLesson(call agentic.ToolCall, err error) agentic.ToolResult {
	if reason := lessonRejectionReason(err); reason != "" && e.recordRejection != nil {
		e.recordRejection(reason)
	}
	return agentic.ToolResult{
		CallID:    call.ID,
		Error:     err.Error(),
		ErrorKind: agentic.ToolErrorInvalidArgs,
	}
}

// deriveObservedRole reads the observed role from framework-propagated tool
// metadata (agentic.MetadataKeyAgentRole, stamped authoritatively by
// agentic-loop dispatch from the loop entity), returning "" when absent. Never
// reads caller arguments — attribution is derived, not supplied. Dispatch
// deletes the key on a roleless loop and overwrites any caller-injected value,
// so this cannot be spoofed through the model's tool arguments.
func deriveObservedRole(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	if role, ok := metadata[agentic.MetadataKeyAgentRole].(string); ok {
		return strings.TrimSpace(role)
	}
	return ""
}

// canonicalLessonContent builds the deterministic, order-insensitive content
// string that content-derived identity hashes. Only the four identity fields
// participate — category, sorted applies_to, summary, sorted evidence — so
// re-emitting with reordered sets, or with refined polarity/severity/detail/
// injection_form, yields the same UUIDv5 and therefore the same entity.
//
// \x1f (unit separator) delimits the four components; \x1e (record separator)
// delimits entries within a set. The encoding is injective BY CONSTRUCTION:
// parseEmitLessonArgs rejects any ASCII control byte (C0, 0x00–0x1F, plus DEL)
// in the identity fields (category, summary, applies_to tokens), and evidence
// entries are IsValidEntityID-gated (their charset excludes control bytes), so
// neither separator can appear inside a component — the concatenation cannot be
// forged to collide.
func canonicalLessonContent(args emitLessonArgs) string {
	appliesTo := append([]string(nil), args.AppliesTo...)
	sort.Strings(appliesTo)
	evidence := append([]string(nil), args.Evidence...)
	sort.Strings(evidence)

	var b strings.Builder
	b.WriteString("category=")
	b.WriteString(args.Category)
	b.WriteByte(0x1f)
	b.WriteString("applies_to=")
	b.WriteString(strings.Join(appliesTo, "\x1e"))
	b.WriteByte(0x1f)
	b.WriteString("summary=")
	b.WriteString(args.Summary)
	b.WriteByte(0x1f)
	b.WriteString("evidence=")
	b.WriteString(strings.Join(evidence, "\x1e"))
	return b.String()
}

// parseEmitLessonArgs reads the untyped tool arguments into emitLessonArgs and
// enforces every writer gate with an instructive error (spec: reject naming the
// violated contract so the agent can rewrite; never truncate). Severity is
// clamped (ordering-only); polarity is rejected on mismatch (meaning-bearing).
//
// The second return is the rejection-counter reason (task 4.4): one of the four
// ADR-080 writer-gate labels (evidence / bound / grammar / cap — cap is applied
// by the caller) when the error is one of those contract gates, or "" for
// input-hygiene rejects (required field, polarity enum, control bytes) which
// are not one of the four gates and stay uncounted. It is "" whenever err is nil.
func parseEmitLessonArgs(raw map[string]any) (emitLessonArgs, error) {
	summary, err := readString(raw, "summary")
	if err != nil {
		return emitLessonArgs{}, err
	}
	detail, err := readString(raw, "detail")
	if err != nil {
		return emitLessonArgs{}, err
	}
	injectionForm, err := readString(raw, "injection_form")
	if err != nil {
		return emitLessonArgs{}, err
	}
	category, err := readString(raw, "category")
	if err != nil {
		return emitLessonArgs{}, err
	}
	polarity, err := readString(raw, "polarity")
	if err != nil {
		return emitLessonArgs{}, err
	}
	// Severity: optional, clamped to the default when missing or outside the
	// closed set. Unlike polarity (meaning-bearing, rejected by the entity's
	// contract), severity only affects brief-injection ordering, so an invalid
	// value is clamped to the lowest urgency rather than bouncing the call —
	// writer policy, applied before the shared contract runs.
	severity, err := readString(raw, "severity")
	if err != nil {
		return emitLessonArgs{}, err
	}
	if !agentic.IsLessonSeverity(severity) {
		severity = emitLessonDefaultSeverity
	}
	evidence, err := readStringArray(raw, "evidence_entity_ids")
	if err != nil {
		return emitLessonArgs{}, fmt.Errorf("%w: %w", agentic.ErrLessonEvidence, err)
	}
	appliesTo, err := readStringArray(raw, "applies_to")
	if err != nil {
		return emitLessonArgs{}, fmt.Errorf("%w: %w", agentic.ErrLessonGrammar, err)
	}
	return emitLessonArgs{
		Summary:       summary,
		Detail:        detail,
		InjectionForm: injectionForm,
		Category:      category,
		Polarity:      polarity,
		Severity:      severity,
		Evidence:      evidence,
		AppliesTo:     appliesTo,
	}, nil
}

// lessonRejectionReason maps a contract failure to the ADR-080 writer-gate
// label the rejection counter carries (evidence / bound / grammar), or "" for
// an input-hygiene reject, which stays uncounted.
func lessonRejectionReason(err error) string {
	switch {
	case errors.Is(err, agentic.ErrLessonEvidence):
		return lessonRejectEvidence
	case errors.Is(err, agentic.ErrLessonBound):
		return lessonRejectBound
	case errors.Is(err, agentic.ErrLessonGrammar):
		return lessonRejectGrammar
	default:
		return ""
	}
}

// readString reads an optional string field: absent or null reads as "";
// a present non-string is a shape error. Whether "" is acceptable is the
// entity contract's decision (AgentLessonEntity.Validate), not the parser's.
func readString(raw map[string]any, field string) (string, error) {
	value, present := raw[field]
	if !present || value == nil {
		return "", nil
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	return s, nil
}

// readStringArray reads an optional JSON array of strings (arrays
// unmarshalled into map[string]any arrive as []any): absent or null reads as
// nil; a present non-array, or a non-string element, is a shape error. The
// minimum count and element grammar are the entity contract's.
func readStringArray(raw map[string]any, field string) ([]string, error) {
	value, present := raw[field]
	if !present || value == nil {
		return nil, nil
	}
	slice, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", field)
	}
	out := make([]string, 0, len(slice))
	for i, v := range slice {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", field, i)
		}
		out = append(out, s)
	}
	return out, nil
}

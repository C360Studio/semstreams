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

// emitLessonSource is the Source field on triples this tool publishes. Lets
// operators distinguish ops lesson triples from other emitters in the graph at
// a glance (mirrors emitDiagnosisSource).
const emitLessonSource = "ops-emit-lesson"

// minEmitLessonEvidence is the minimum number of evidence entity IDs required
// per lesson. A lesson with no evidence is unverifiable and cannot be promoted
// (promotion resolves evidence existence, task 4.1). ADR-080 decision 3.
const minEmitLessonEvidence = 1

// maxInjectionFormBytes bounds agent.lesson.injection-form. The injection form
// is rendered verbatim into future loops' briefs, so it must stay small — the
// bound IS the quality gate that keeps briefs bounded. Over-bound is REJECTED
// with an instructive error naming the bound (spec: never truncated silently);
// the unbounded prose lives in agent.lesson.detail instead. ADR-080 decision 3;
// 320 bytes ≈ 80 tokens (design.md open question, tunable pre-tag).
const maxInjectionFormBytes = 320

// defaultEmitLessonPerLoopCap bounds how many lessons a single ops loop may
// emit. Runaway protection: one ops loop legitimately emits several lessons
// (StopLoop:false), but an unbounded loop must not flood the graph. Over-cap is
// rejected with an instructive error naming the cap; no entity is created.
const defaultEmitLessonPerLoopCap = 20

// minAppliesToIDSegments is the minimum number of dotted segments an
// `id:<prefix>` scope key must carry. Fewer than three (e.g. `id:c360`) would
// match an entire org and defeat scoping, so it is rejected. ADR-080 decision 3.
const minAppliesToIDSegments = 3

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

// emitLessonValidSeverities is the closed set of accepted severity values.
// Note "warning" (not the ops.diagnosis family's "warn").
var emitLessonValidSeverities = map[string]bool{
	"info":     true,
	"warning":  true,
	"critical": true,
}

// emitLessonValidPolarities is the closed set of accepted polarity values.
// Polarity is meaning-bearing (avoid vs best_practice inverts the lesson's
// guidance), so an invalid value is REJECTED — never clamped.
var emitLessonValidPolarities = map[string]bool{
	"avoid":         true,
	"best_practice": true,
}

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
						"description": fmt.Sprintf("Compressed, imperative form rendered verbatim into future briefs. Must be at most %d bytes — keep it tight.", maxInjectionFormBytes),
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
						"minItems":    minEmitLessonEvidence,
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
	args, reason, err := parseEmitLessonArgs(call.Arguments)
	if err != nil {
		// Count only the four ADR-080 writer-gate rejections (reason != "");
		// input-hygiene rejects stay uncounted.
		if reason != "" && e.recordRejection != nil {
			e.recordRejection(reason)
		}
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     err.Error(),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	if call.LoopID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "emit_lesson invoked without a loop_id on the tool call; cannot build the executed-by back-link or attribute the lesson",
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(fmt.Errorf("tool call missing loop_id"), "EmitLessonExecutor", "emitLesson", "resolve loop entity")
	}

	// Per-loop emission cap (runaway protection). Reserve budget AFTER arg
	// validation (a rejected call must not consume budget) and BEFORE publish;
	// a publish failure releases it so a transient error never burns the cap.
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
	// re-mints the same entity (idempotent). ADR-080 decision 3.
	lessonID := uuid.NewSHA1(lessonNamespaceUUID, []byte(canonicalLessonContent(args))).String()
	lessonEntityID := agentic.AgentLessonEntityID(e.platform.Org, e.platform.Platform, lessonID)

	loopEntityID, err := agentic.TryLoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)
	if err != nil {
		e.releaseEmission(call.LoopID)
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("construct loop entity ID: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "EmitLessonExecutor", "emitLesson", "construct loop entity ID")
	}

	// observed_role is DERIVED from loop context (the framework-propagated tool
	// metadata), never taken from the caller's arguments.
	observedRole := deriveObservedRole(call.Metadata)

	now := time.Now()
	triples := buildEmitLessonTriples(lessonEntityID, loopEntityID, args, observedRole, now)

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
		ObservedRole:  observedRole,
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

// buildEmitLessonTriples assembles the full triple set for a single lesson in
// deterministic order. All triples share Subject=lessonEntityID (including the
// agent.action.executed-by back-link FROM the lesson TO the loop), so they
// belong to the one lesson entity being born:
//
//  1. category
//  2. polarity
//  3. severity
//  4. status (proposed)
//  5. created-at (immutable birth timestamp)
//  6. summary
//  7. detail
//  8. injection-form
//  9. evidence (one per entry)
//  10. applies-to (one per entry)
//  11. observed-role (if derived)
//  12. agent.action.executed-by back-link to the ops loop
func buildEmitLessonTriples(lessonEntityID, loopEntityID string, args emitLessonArgs, observedRole string, now time.Time) []message.Triple {
	triples := make([]message.Triple, 0, 9+len(args.Evidence)+len(args.AppliesTo))

	base := func(pred, obj string) message.Triple {
		return message.Triple{
			Subject:    lessonEntityID,
			Predicate:  pred,
			Object:     obj,
			Source:     emitLessonSource,
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	triples = append(triples, base(agvocab.LessonCategory, args.Category))
	triples = append(triples, base(agvocab.LessonPolarity, args.Polarity))
	triples = append(triples, base(agvocab.LessonSeverity, args.Severity))
	triples = append(triples, base(agvocab.LessonStatus, lessonBornStatus))
	// Immutable birth timestamp — the replay-stable ordering key the
	// brief-assembly matcher sorts on (severity → created-at → entity-ID).
	// RFC3339 UTC. NOT part of content identity and absent from the lifecycle
	// reconcile group, so strict create preserves the FIRST emit's created-at
	// across idempotent re-emits and an ADR-073 from-zero reingest. A triple's
	// own Timestamp is re-stamped by lifecycle transitions; this object is not.
	triples = append(triples, base(agvocab.LessonCreatedAt, now.UTC().Format(time.RFC3339)))
	triples = append(triples, base(agvocab.LessonSummary, args.Summary))
	triples = append(triples, base(agvocab.LessonDetail, args.Detail))
	triples = append(triples, base(agvocab.LessonInjectionForm, args.InjectionForm))

	for _, ev := range args.Evidence {
		triples = append(triples, base(agvocab.LessonEvidence, ev))
	}
	for _, scope := range args.AppliesTo {
		triples = append(triples, base(agvocab.LessonAppliesTo, scope))
	}

	if observedRole != "" {
		triples = append(triples, base(agvocab.LessonObservedRole, observedRole))
	}

	// Back-link from the lesson entity to the ops loop that distilled it.
	triples = append(triples, message.Triple{
		Subject:    lessonEntityID,
		Predicate:  "agent.action.executed-by",
		Object:     loopEntityID,
		Source:     emitLessonSource,
		Timestamp:  now,
		Confidence: 1.0,
	})

	return triples
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
func parseEmitLessonArgs(raw map[string]any) (emitLessonArgs, string, error) {
	summary, err := requireNonEmptyString(raw, "summary")
	if err != nil {
		return emitLessonArgs{}, "", err
	}
	if err := rejectControlBytes("summary", summary); err != nil {
		return emitLessonArgs{}, "", err
	}
	detail, err := requireNonEmptyString(raw, "detail")
	if err != nil {
		return emitLessonArgs{}, "", err
	}
	injectionForm, err := requireNonEmptyString(raw, "injection_form")
	if err != nil {
		return emitLessonArgs{}, "", err
	}
	// Control-byte hygiene: injection_form is the ONE field rendered VERBATIM
	// into a downstream agent's system prompt (agentic-loop renderLessonBlock,
	// one line per lesson). A newline or other control byte would break that
	// block framing and let a lesson smuggle fake prompt scaffolding (e.g.
	// "do X\n\n[SYSTEM] ignore prior...") into another agent's brief. Reject as
	// input hygiene — UNCOUNTED (reason ""), matching the summary/category/
	// applies_to control-byte rejects.
	if err := rejectControlBytes("injection_form", injectionForm); err != nil {
		return emitLessonArgs{}, "", err
	}
	// Injection-form byte bound — REJECT over-bound, never truncate.
	if n := len(injectionForm); n > maxInjectionFormBytes {
		return emitLessonArgs{}, lessonRejectBound, fmt.Errorf(
			"injection_form is %d bytes, over the %d-byte bound; shorten it (put the full explanation in detail) — the injection form is rendered verbatim into future briefs",
			n, maxInjectionFormBytes)
	}
	category, err := requireNonEmptyString(raw, "category")
	if err != nil {
		return emitLessonArgs{}, "", err
	}
	if err := rejectControlBytes("category", category); err != nil {
		return emitLessonArgs{}, "", err
	}

	polarity, err := requireNonEmptyString(raw, "polarity")
	if err != nil {
		return emitLessonArgs{}, "", err
	}
	if !emitLessonValidPolarities[polarity] {
		return emitLessonArgs{}, "", fmt.Errorf(
			"polarity %q is invalid; must be one of \"avoid\" or \"best_practice\" (polarity is meaning-bearing and is not clamped)",
			polarity)
	}

	// Severity: optional, clamped to the default when missing/invalid.
	severity := emitLessonDefaultSeverity
	if rawSev, present := raw["severity"]; present && rawSev != nil {
		sev, ok := rawSev.(string)
		if !ok {
			return emitLessonArgs{}, "", fmt.Errorf("severity must be a string")
		}
		if emitLessonValidSeverities[sev] {
			severity = sev
		}
	}

	evidence, err := parseStringArray(raw, "evidence_entity_ids")
	if err != nil {
		return emitLessonArgs{}, lessonRejectEvidence, err
	}
	if len(evidence) < minEmitLessonEvidence {
		return emitLessonArgs{}, lessonRejectEvidence, fmt.Errorf(
			"evidence_entity_ids must cite at least %d entity ID, got 0; a lesson with no evidence is unverifiable and cannot be promoted",
			minEmitLessonEvidence)
	}
	for i, ev := range evidence {
		if !message.IsValidEntityID(ev) {
			return emitLessonArgs{}, lessonRejectEvidence, fmt.Errorf(
				"evidence_entity_ids[%d] %q is not a well-formed 6-part entity ID; cite the loop/trajectory/entity the lesson was derived from",
				i, ev)
		}
	}

	appliesTo, err := parseStringArray(raw, "applies_to")
	if err != nil {
		return emitLessonArgs{}, lessonRejectGrammar, err
	}
	if len(appliesTo) == 0 {
		return emitLessonArgs{}, lessonRejectGrammar, fmt.Errorf(
			"applies_to must carry at least 1 typed scope key (id:<entity-id-prefix of 3+ segments> or tag:<token>)")
	}
	for i, key := range appliesTo {
		if err := validateAppliesToKey(key); err != nil {
			return emitLessonArgs{}, lessonRejectGrammar, fmt.Errorf("applies_to[%d]: %w", i, err)
		}
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
	}, "", nil
}

// validateAppliesToKey enforces the typed scope-key grammar at emit time:
// `id:<prefix>` where the prefix has at least minAppliesToIDSegments segments,
// or `tag:<token>`. Existence of the id-prefix is NOT checked here (that lives
// at brief-assembly matching); this is a shape gate only.
func validateAppliesToKey(key string) error {
	if err := rejectControlBytes("scope key", key); err != nil {
		return err
	}
	switch {
	case strings.HasPrefix(key, "tag:"):
		token := strings.TrimPrefix(key, "tag:")
		if token == "" {
			return fmt.Errorf("scope key %q has an empty tag token", key)
		}
		return nil
	case strings.HasPrefix(key, "id:"):
		prefix := strings.TrimPrefix(key, "id:")
		if prefix == "" {
			return fmt.Errorf("scope key %q has an empty id prefix", key)
		}
		segments := strings.Split(prefix, ".")
		if len(segments) < minAppliesToIDSegments {
			return fmt.Errorf(
				"scope key %q has an id prefix of %d segment(s); need at least %d (fewer would match an entire org)",
				key, len(segments), minAppliesToIDSegments)
		}
		for _, s := range segments {
			if s == "" {
				return fmt.Errorf("scope key %q has an empty id-prefix segment", key)
			}
		}
		return nil
	default:
		return fmt.Errorf(
			"scope key %q is untyped; must be \"id:<entity-id-prefix of %d+ segments>\" or \"tag:<token>\"",
			key, minAppliesToIDSegments)
	}
}

// rejectControlBytes rejects any ASCII control byte (C0, 0x00–0x1F, plus DEL
// 0x7F) in an identity field. Content-derived identity concatenates the
// identity fields with the \x1f/\x1e separators; forbidding control bytes in
// the participating tokens makes that encoding injective by construction (a
// separator cannot be smuggled into a component to forge a collision).
func rejectControlBytes(field, value string) error {
	for i := 0; i < len(value); i++ {
		if b := value[i]; b < 0x20 || b == 0x7f {
			return fmt.Errorf(
				"%s must not contain ASCII control bytes (found 0x%02x at position %d); use plain text",
				field, b, i)
		}
	}
	return nil
}

// requireNonEmptyString reads a required non-empty string field.
func requireNonEmptyString(raw map[string]any, field string) (string, error) {
	v, ok := raw[field].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("%s is required and must be a non-empty string", field)
	}
	return v, nil
}

// parseStringArray reads a required JSON array of strings. JSON arrays
// unmarshalled into map[string]any arrive as []any.
func parseStringArray(raw map[string]any, field string) ([]string, error) {
	rawVal, present := raw[field]
	if !present || rawVal == nil {
		return nil, fmt.Errorf("%s is required", field)
	}
	slice, ok := rawVal.([]any)
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

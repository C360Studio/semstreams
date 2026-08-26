package agentic

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/projection/contract"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// CategoryAgentLesson is the message category for the agent-lesson-record
// entity origin contract (ADR-080). It names the ENTITY type born when the
// ops agent's emit_lesson tool distils a reusable lesson into the graph —
// distinct from any event payload. Mirrors CategoryOpsDiagnosis /
// CategoryLoopExecution / CategoryModelEndpoint.
//
// Lessons unify with the rest of agent memory under the agent.* entity-ID
// domain; diagnosis stays ops.* (it is an observability artifact, not
// memory). See ADR-080 decision 1.
const CategoryAgentLesson = "agent_lesson"

// lessonSource is the Source on every triple AgentLessonEntity emits — the
// value the ops agent's emit_lesson tool has always stamped, so operators can
// distinguish lesson triples from other emitters at a glance.
const lessonSource = "ops-emit-lesson"

// Contract and group names identify the built-in lesson-record projection
// schema. The contract is registered with agentic.agent_lesson.v1 (ADR-103).
const (
	LessonRecordContractName = "agentic.lesson-record"
	LessonLifecycleGroupName = "lesson-lifecycle"
)

// AgentLessonMessageType returns the message.Type for the agent-lesson-record
// entity — key "agentic.agent_lesson.v1". Registered by RegisterPayloads with
// floor content and LessonContract (ADR-103): stamped on
// CreateEntityRequest.Entity.MessageType when EmitLessonExecutor births a
// lesson entity, and decodes on the fact lane as *AgentLessonEntity. Each
// emit_lesson call mints a content-derived agent.lesson.record.{uuid5} entity
// that MUST be created with this envelope; append is must-exist and rejects an
// absent lesson entity.
func AgentLessonMessageType() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryAgentLesson,
		Version:  SchemaVersion,
	}
}

// AgentLessonEntity is the registered Graphable payload for an agent lesson
// record (ADR-080, ADR-103). Every triple object is a field, so the wire form
// carries everything Triples() emits; CreatedAt is the immutable birth
// timestamp whose RFC3339 UTC rendering is the agent.lesson.created-at object.
type AgentLessonEntity struct {
	Org           string    `json:"org"`
	Platform      string    `json:"platform"`
	ID            string    `json:"id"`
	Category      string    `json:"category"`
	Polarity      string    `json:"polarity"`
	Severity      string    `json:"severity"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	Summary       string    `json:"summary"`
	Detail        string    `json:"detail"`
	InjectionForm string    `json:"injection_form"`
	Evidence      []string  `json:"evidence"`
	AppliesTo     []string  `json:"applies_to"`
	ObservedRole  string    `json:"observed_role,omitempty"`
	ExecutedBy    string    `json:"executed_by"`
}

// EntityID returns the canonical lesson entity ID, or "" when the identity
// fields cannot form one (graph-ingest rejects an empty ID; a decoded payload
// must never panic the consumer).
func (e *AgentLessonEntity) EntityID() string {
	id, err := tryAgentLessonEntityID(e.Org, e.Platform, e.ID)
	if err != nil {
		return ""
	}
	return id
}

// Triples returns the full lesson triple set in the order the emit_lesson
// writer has always produced it (ADR-080 decision 3):
//
//  1. category
//  2. polarity
//  3. severity
//  4. status
//  5. created-at (immutable birth timestamp)
//  6. summary
//  7. detail
//  8. injection-form
//  9. evidence (one per entry)
//  10. applies-to (one per entry)
//  11. observed-role (if derived)
//  12. agent.action.executed-by back-link to the ops loop
//
// Every triple carries Source lessonSource and Confidence 1.0; the Timestamp is
// stamped at call time.
func (e *AgentLessonEntity) Triples() []message.Triple {
	lessonEntityID := e.EntityID()
	now := time.Now()
	triples := make([]message.Triple, 0, 9+len(e.Evidence)+len(e.AppliesTo))

	base := func(pred, obj string) message.Triple {
		return message.Triple{
			Subject:    lessonEntityID,
			Predicate:  pred,
			Object:     obj,
			Source:     lessonSource,
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	triples = append(triples, base(agvocab.LessonCategory, e.Category))
	triples = append(triples, base(agvocab.LessonPolarity, e.Polarity))
	triples = append(triples, base(agvocab.LessonSeverity, e.Severity))
	triples = append(triples, base(agvocab.LessonStatus, e.Status))
	// Immutable birth timestamp — the replay-stable ordering key the
	// brief-assembly matcher sorts on (severity → created-at → entity-ID).
	// RFC3339 UTC. NOT part of content identity and absent from the lifecycle
	// reconcile group, so strict create preserves the FIRST emit's created-at
	// across idempotent re-emits and an ADR-073 from-zero reingest. A triple's
	// own Timestamp is re-stamped by lifecycle transitions; this object is not.
	triples = append(triples, base(agvocab.LessonCreatedAt, e.CreatedAt.UTC().Format(time.RFC3339)))
	triples = append(triples, base(agvocab.LessonSummary, e.Summary))
	triples = append(triples, base(agvocab.LessonDetail, e.Detail))
	triples = append(triples, base(agvocab.LessonInjectionForm, e.InjectionForm))

	for _, ev := range e.Evidence {
		triples = append(triples, base(agvocab.LessonEvidence, ev))
	}
	for _, scope := range e.AppliesTo {
		triples = append(triples, base(agvocab.LessonAppliesTo, scope))
	}

	if e.ObservedRole != "" {
		triples = append(triples, base(agvocab.LessonObservedRole, e.ObservedRole))
	}

	// Back-link from the lesson entity to the ops loop that distilled it.
	triples = append(triples, base(agvocab.ActionExecutedBy, e.ExecutedBy))

	return triples
}

// Schema implements message.Payload.
func (e *AgentLessonEntity) Schema() message.Type {
	return AgentLessonMessageType()
}

// Validate implements message.Payload: the identity fields must form a
// well-formed lesson entity ID.
func (e *AgentLessonEntity) Validate() error {
	_, err := tryAgentLessonEntityID(e.Org, e.Platform, e.ID)
	return err
}

// MarshalJSON implements json.Marshaler with the alias idiom.
func (e *AgentLessonEntity) MarshalJSON() ([]byte, error) {
	type alias AgentLessonEntity
	return json.Marshal((*alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler with the alias idiom.
func (e *AgentLessonEntity) UnmarshalJSON(data []byte) error {
	type alias AgentLessonEntity
	return json.Unmarshal(data, (*alias)(e))
}

// LessonContract returns a fresh copy of the canonical lesson-record projection
// contract bound to agentic.agent_lesson.v1: the birth predicates emit_lesson
// stamps and the reconcile-mode lifecycle group the LessonCurator drives.
func LessonContract() contract.Contract {
	return contract.Contract{
		Name:          LessonRecordContractName,
		MessageType:   AgentLessonMessageType().Key(),
		EntityPattern: "*.*.agent.lesson.record.*",
		BirthPredicates: []string{
			agvocab.LessonCategory,
			agvocab.LessonPolarity,
			agvocab.LessonSeverity,
			agvocab.LessonCreatedAt,
			agvocab.LessonSummary,
			agvocab.LessonDetail,
			agvocab.LessonInjectionForm,
			agvocab.LessonEvidence,
			agvocab.LessonAppliesTo,
			agvocab.LessonObservedRole,
			agvocab.ActionExecutedBy,
		},
		Groups: []contract.PredicateGroup{{
			Name: LessonLifecycleGroupName,
			Mode: contract.ModeReconcile,
			Predicates: []string{
				agvocab.LessonStatus,
				agvocab.LessonSupersededBy,
				agvocab.LessonRetiredAt,
			},
		}},
	}
}

// AgentLessonEntityID returns the canonical entity ID for an agent lesson
// record. The id argument is the content-derived unique identifier
// (a UUIDv5 without dots, generated by the emit_lesson tool from the lesson's
// category + scope + summary + evidence — see emit_lesson.go).
//
// Format: {org}.{platform}.agent.lesson.record.{id}
//
// Example: AgentLessonEntityID("acme", "ops", "2c5acb9b-8283-5b34-a4d1-4b1c9f8502ca")
// Returns: "acme.ops.agent.lesson.record.2c5acb9b-8283-5b34-a4d1-4b1c9f8502ca"
//
// This is a 6-part entity ID (org.platform / agent / lesson / record / id);
// the domain+system axes (agent.lesson) align the entity's identity with its
// predicate family (agent.lesson.*), exactly as ops.diagnosis.finding aligns
// with ops.diagnosis.*.
//
// Panics if any input part is empty or contains a dot, as these represent
// programming errors — the caller is responsible for supplying well-formed
// identifiers. The id must be a UUID or equivalent unique token with no dots.
func AgentLessonEntityID(org, platform, id string) string {
	entityID, err := tryAgentLessonEntityID(org, platform, id)
	if err != nil {
		panic(fmt.Sprintf("AgentLessonEntityID: %s", err))
	}
	return entityID
}

// tryAgentLessonEntityID is the error-returning form of AgentLessonEntityID;
// the decoded-payload path uses it so a malformed identity never panics.
func tryAgentLessonEntityID(org, platform, id string) (string, error) {
	if err := validatePart("org", org); err != nil {
		return "", err
	}
	if err := validatePart("platform", platform); err != nil {
		return "", err
	}
	if err := validatePart("id", id); err != nil {
		return "", err
	}

	entityID := fmt.Sprintf("%s.%s.agent.lesson.record.%s", org, platform, id)

	if !message.IsValidEntityID(entityID) {
		return "", fmt.Errorf("constructed id %q failed IsValidEntityID — check input values", entityID)
	}

	return entityID, nil
}

// AgentLessonRecordPrefix returns the 5-part entity-ID prefix shared by every
// lesson record for an org/platform: "{org}.{platform}.agent.lesson.record".
//
// It is the query prefix a reader passes to graph.ingest.query.prefix to list
// all lesson entities (the brief-assembly LessonReader uses it). Keeping the
// "agent.lesson.record" segment string here — beside AgentLessonEntityID —
// prevents it drifting from the entity-ID format. Panics on empty/dotted parts,
// as those are programming errors.
func AgentLessonRecordPrefix(org, platform string) string {
	if err := validatePart("org", org); err != nil {
		panic(fmt.Sprintf("AgentLessonRecordPrefix: %s", err))
	}
	if err := validatePart("platform", platform); err != nil {
		panic(fmt.Sprintf("AgentLessonRecordPrefix: %s", err))
	}
	return fmt.Sprintf("%s.%s.agent.lesson.record", org, platform)
}

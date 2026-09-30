package agentic

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/projection/contract"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	// CategoryLoopExecution is the message category for the loop-execution
	// entity origin contract (ADR-056 W0 4c-pre-1). Distinct from
	// CategoryLoopCreated (the event payload) — this category names the
	// entity type, not the event.
	CategoryLoopExecution = "loop_execution"

	// loopExecutionSource is the Source stamped on all origin triples
	// produced by LoopExecutionEntity.Triples() and ModelEndpointEntity.Triples().
	// Matches graphWriterSource in processor/agentic-loop so provenance
	// attribution is unchanged.
	loopExecutionSource = "agentic-loop"

	// loopExecutionMaxPromptTripleBytes caps the size of the prompt stored
	// as the agent.loop.description triple, including the truncation marker.
	// Must match processor/agentic-loop's maxPromptTripleBytes constant.
	loopExecutionMaxPromptTripleBytes = 8 * 1024

	// loopExecutionTruncationMarker is appended to a truncated prompt.
	// Must match processor/agentic-loop's truncationMarker constant.
	loopExecutionTruncationMarker = "…[truncated]"
)

// Contract and group names identify the built-in loop-execution projection
// schema. The contract is registered with agentic.loop_execution.v1 (ADR-103).
const (
	LoopExecutionContractName = "agentic.loop-execution"
	TodoGroupName             = "todos"
)

// truncateLoopDescription returns s capped at maxBytes bytes total
// (including the truncation marker, if appended). It is UTF-8 safe —
// the cut point is walked back to the nearest rune boundary.
//
// This function mirrors truncateForTriple in processor/agentic-loop;
// it lives here so LoopExecutionEntity.Triples() can be self-contained
// without importing the processor package (which would create a cycle).
func truncateLoopDescription(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	budget := maxBytes - len(loopExecutionTruncationMarker)
	if budget <= 0 {
		return loopExecutionTruncationMarker[:maxBytes]
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + loopExecutionTruncationMarker
}

// LoopExecutionEntity is the registered Graphable payload for an agentic loop
// execution entity (ADR-056 W0 4c-pre-1, ADR-103). It encodes the
// spawn-identity triples that give the entity its typed origin: role, task,
// parent, run, reply_to, workflow, workflow_step, user, and description.
//
// EntityID() and Triples() together form the typed origin contract — the
// same data set that processor/agentic-loop's buildSpawnIdentityTriples
// emitted, now expressed through graph.Graphable so it can be born via the
// canonical entity-create operation and arrive on the fact lane as itself.
//
// This type lives in the agentic package (below processor/agentic-loop in
// the import graph) to keep the dependency direction agentic → processor
// (never the reverse).
type LoopExecutionEntity struct {
	Org      string       `json:"org"`
	Platform string       `json:"platform"`
	LoopID   string       `json:"loop_id"`
	Task     *TaskMessage `json:"task,omitempty"`
}

// EntityID returns the canonical 6-part entity ID for this loop execution, or
// "" when the identity fields cannot form one (graph-ingest rejects an empty
// ID; a decoded payload must never panic the consumer).
func (e *LoopExecutionEntity) EntityID() string {
	id, err := TryLoopExecutionEntityID(e.Org, e.Platform, e.LoopID)
	if err != nil {
		// entity-id-audit:classify intentional-sentinel "" line=94 column=10 surface=go-return:EntityID entity_id_invalid:empty documented loop-execution failure return; graph-ingest rejects an empty ID and a decoded payload must not panic
		return ""
	}
	return id
}

// Triples returns the spawn-identity origin triples for this loop execution.
// The predicate set is identical to what buildSpawnIdentityTriples in
// processor/agentic-loop produced: always-on (role, task), conditionally-on
// (parent, run, run.entity_id, reply_to, workflow, workflow_step, user,
// description) when the corresponding TaskMessage field is non-empty.
//
// All triples share a single timestamp (captured once at call time) so that
// every triple in one batch is guaranteed to have the same wall-clock value —
// mirrors the shared-timestamp invariant preserved by buildSpawnIdentityTriples.
//
// Returns nil when Task is nil.
func (e *LoopExecutionEntity) Triples() []message.Triple {
	if e.Task == nil {
		return nil
	}

	loopEntityID := e.EntityID()
	now := time.Now()

	triple := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    loopEntityID,
			Predicate:  predicate,
			Object:     object,
			Source:     loopExecutionSource,
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	triples := make([]message.Triple, 0, 10)

	if e.Task.Role != "" {
		triples = append(triples, triple(agvocab.LoopRole, e.Task.Role))
	}
	if e.Task.TaskID != "" {
		triples = append(triples, triple(agvocab.LoopTask, e.Task.TaskID))
	}
	// Parent and reply-to are omitted when their ID cannot be constructed, the
	// way the run branch below already does: this payload is decoded from the
	// wire, and a malformed reference must never panic the consumer (#1112).
	if e.Task.ParentLoopID != "" {
		if parentEntityID, err := TryLoopExecutionEntityID(e.Org, e.Platform, e.Task.ParentLoopID); err == nil {
			triples = append(triples, triple(agvocab.LoopParent, parentEntityID))
		}
	}
	// Stamp the run anchor when the loop belongs to a run (ADR-053 D7).
	// Two triples: agent.loop.run = bare RunID; agent.run.entity-id = the full
	// 6-part chain.execution ID for rule substitution.
	if e.Task.RunID != "" {
		triples = append(triples, triple(agvocab.LoopRun, e.Task.RunID))
		if runEntityID, err := TryChainExecutionEntityID(e.Org, e.Platform, e.Task.RunID); err == nil {
			triples = append(triples, triple(agvocab.LoopRunEntityID, runEntityID))
		}
	}
	// Stamp the reply pointer when this loop is a reply (gh#256).
	if e.Task.InReplyTo != "" {
		if replyEntityID, err := TryLoopExecutionEntityID(e.Org, e.Platform, e.Task.InReplyTo); err == nil {
			triples = append(triples, triple(agvocab.LoopReplyTo, replyEntityID))
		}
	}
	if e.Task.WorkflowSlug != "" {
		triples = append(triples, triple(agvocab.LoopWorkflow, e.Task.WorkflowSlug))
	}
	if e.Task.WorkflowStep != "" {
		triples = append(triples, triple(agvocab.LoopWorkflowStep, e.Task.WorkflowStep))
	}
	if e.Task.UserID != "" {
		triples = append(triples, triple(agvocab.LoopUser, e.Task.UserID))
	}
	if e.Task.Prompt != "" {
		triples = append(triples, triple(agvocab.LoopDescription,
			truncateLoopDescription(e.Task.Prompt, loopExecutionMaxPromptTripleBytes)))
	}

	return triples
}

// Schema implements message.Payload.
func (e *LoopExecutionEntity) Schema() message.Type {
	return LoopExecutionMessageType()
}

// Validate implements message.Payload and IS the spawn-identity writer's
// contract — no stronger: identity, a non-nil spawning TaskMessage, and at
// least one spawn-identity fact to emit. The writer never required a full
// task request (Triples() emits role, task, parent, run, reply-to, workflow,
// user, and description each only when present), so neither does the payload;
// TaskMessage.Validate remains the contract of a task ARRIVING as a task
// request, not of the identity snapshot a loop execution carries.
// BaseMessage.MarshalJSON refuses a payload that fails this; the agentic-loop
// graph writer delegates here before birthing the execution entity.
func (e *LoopExecutionEntity) Validate() error {
	if _, err := TryLoopExecutionEntityID(e.Org, e.Platform, e.LoopID); err != nil {
		return err
	}
	if e.Task == nil {
		return errors.New("task is required (the spawning TaskMessage)")
	}
	// A malformed parent or reply-to reference is a writer-contract violation:
	// refuse it loudly here rather than let Triples() omit it silently.
	if e.Task.ParentLoopID != "" {
		if _, err := TryLoopExecutionEntityID(e.Org, e.Platform, e.Task.ParentLoopID); err != nil {
			return fmt.Errorf("parent_loop_id: %w", err)
		}
	}
	if e.Task.InReplyTo != "" {
		if _, err := TryLoopExecutionEntityID(e.Org, e.Platform, e.Task.InReplyTo); err != nil {
			return fmt.Errorf("in_reply_to: %w", err)
		}
	}
	if len(e.Triples()) == 0 {
		return errors.New("task carries no spawn-identity facts (role, task, parent, run, reply-to, workflow, user, and description are all empty)")
	}
	return nil
}

// MarshalJSON implements json.Marshaler with the alias idiom.
func (e *LoopExecutionEntity) MarshalJSON() ([]byte, error) {
	type alias LoopExecutionEntity
	return json.Marshal((*alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler with the alias idiom.
func (e *LoopExecutionEntity) UnmarshalJSON(data []byte) error {
	type alias LoopExecutionEntity
	return json.Unmarshal(data, (*alias)(e))
}

// LoopExecutionMessageType returns the message.Type for the loop-execution
// entity — key "agentic.loop_execution.v1" (snake_case category, matching the
// agentic convention: tool_call, loop_created, approval_pending). Registered
// by RegisterPayloads with floor control and LoopExecutionContract (ADR-103):
// it is stamped on CreateEntityRequest.Entity.MessageType at birth and decodes
// on the fact lane as *LoopExecutionEntity.
func LoopExecutionMessageType() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryLoopExecution,
		Version:  SchemaVersion,
	}
}

// LoopExecutionContract returns a fresh copy of the projection contract bound
// to agentic.loop_execution.v1: the spawn-identity birth predicates and the
// reconcile-mode todo group (written by the write_todos tool).
func LoopExecutionContract() contract.Contract {
	return contract.Contract{
		Name:          LoopExecutionContractName,
		MessageType:   LoopExecutionMessageType(),
		EntityPattern: "*.*.agentic-loop.agent.execution.*",
		BirthPredicates: []string{
			agvocab.LoopRole,
			agvocab.LoopTask,
			agvocab.LoopParent,
			agvocab.LoopRun,
			agvocab.LoopRunEntityID,
			agvocab.LoopReplyTo,
			agvocab.LoopWorkflow,
			agvocab.LoopWorkflowStep,
			agvocab.LoopUser,
			agvocab.LoopDescription,
		},
		Groups: []contract.PredicateGroup{{
			Name: TodoGroupName,
			Mode: contract.ModeReconcile,
			Predicates: []string{
				agvocab.TodoRecord,
			},
		}},
	}
}

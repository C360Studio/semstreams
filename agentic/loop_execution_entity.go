package agentic

import (
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/message"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	// CategoryLoopExecution is the message category for the loop-execution
	// entity origin contract (ADR-056 W0 4c-pre-1). Distinct from
	// CategoryLoopCreated (the event payload) — this category names the
	// entity type, not the event.
	CategoryLoopExecution = "loop_execution"

	// loopExecutionSource is the Source stamped on all origin triples
	// produced by LoopExecutionEntity.Triples(). Matches graphWriterSource
	// in processor/agentic-loop so provenance attribution is unchanged.
	loopExecutionSource = "agentic-loop"

	// loopExecutionMaxPromptTripleBytes caps the size of the prompt stored
	// as the agent.loop.description triple, including the truncation marker.
	// Must match processor/agentic-loop's maxPromptTripleBytes constant.
	loopExecutionMaxPromptTripleBytes = 8 * 1024

	// loopExecutionTruncationMarker is appended to a truncated prompt.
	// Must match processor/agentic-loop's truncationMarker constant.
	loopExecutionTruncationMarker = "…[truncated]"
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

// LoopExecutionEntity is the Graphable origin contract for an agentic loop
// execution entity (ADR-056 W0 4c-pre-1). It encodes the spawn-identity
// triples that give the entity its typed origin: role, task, parent,
// run, reply_to, workflow, workflow_step, user, and description.
//
// EntityID() and Triples() together form the typed origin contract — the
// same data set that processor/agentic-loop's buildSpawnIdentityTriples
// emitted, now expressed through graph.Graphable so it can be born via
// create_with_triples instead of triple.add_batch auto-vivification.
//
// This type lives in the agentic package (below processor/agentic-loop in
// the import graph) to keep the dependency direction agentic → processor
// (never the reverse).
type LoopExecutionEntity struct {
	Org      string
	Platform string
	LoopID   string
	Task     *TaskMessage
}

// EntityID returns the canonical 6-part entity ID for this loop execution.
func (e *LoopExecutionEntity) EntityID() string {
	return LoopExecutionEntityID(e.Org, e.Platform, e.LoopID)
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
	if e.Task.ParentLoopID != "" {
		parentEntityID := LoopExecutionEntityID(e.Org, e.Platform, e.Task.ParentLoopID)
		triples = append(triples, triple(agvocab.LoopParent, parentEntityID))
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
		replyEntityID := LoopExecutionEntityID(e.Org, e.Platform, e.Task.InReplyTo)
		triples = append(triples, triple(agvocab.LoopReplyTo, replyEntityID))
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

// LoopExecutionMessageType returns the message.Type for the loop-execution
// entity origin contract — key "agentic.loop_execution.v1" (snake_case category,
// matching the agentic convention: tool_call, loop_created, approval_pending).
//
// Registry decision (ADR-056, intentionally pinned — not implicit): this type is
// MUTATION-ONLY. It is stamped on CreateEntityWithTriplesRequest.Entity.MessageType
// at birth purely as PRODUCER IDENTITY for ownership arbitration; it is NEVER
// published as a BaseMessage payload and is therefore NOT registered in the
// payload registry (payload_registry.go) and never round-trips through
// payload decoding. This mirrors core.identity.stub.v1 (the referential-integrity
// stub envelope): both are envelope-bearing graph-origin markers, not wire
// payloads. If a future producer needs to PUBLISH a loop_execution message over
// NATS (not merely stamp it at create), that producer must add the init()
// registration at that point — until then, registering it would advertise a
// decode path that does not exist.
func LoopExecutionMessageType() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryLoopExecution,
		Version:  SchemaVersion,
	}
}

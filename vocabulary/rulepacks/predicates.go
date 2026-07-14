// Package rulepacks declares predicates owned by SemStreams' shipped reference
// rule packs rather than by a domain payload package. Import it at the trusted
// composition root before loading those packs.
package rulepacks

import "github.com/c360studio/semstreams/vocabulary"

const (
	// AgenticCheckpointCompleted marks a completed agent checkpoint.
	AgenticCheckpointCompleted = "agentic.checkpoint.completed"
	// AgenticCheckpointIteration records the checkpoint iteration.
	AgenticCheckpointIteration = "agentic.checkpoint.iteration"
	// AgenticCheckpointStarted marks a started agent checkpoint.
	AgenticCheckpointStarted = "agentic.checkpoint.started"
	// AgenticDecisionDetected marks a detected agent decision.
	AgenticDecisionDetected = "agentic.decision.detected"
	// AgenticFileModified marks a file modification.
	AgenticFileModified = "agentic.file.modified"
	// AgenticToolFileOperation records a file-oriented tool operation.
	AgenticToolFileOperation = "agentic.tool.file-operation"
	// AgenticToolUsed records tool usage.
	AgenticToolUsed = "agentic.tool.used"
	// EntityIdentityType records the entity type identity.
	EntityIdentityType = "entity.identity.type"
	// GatherChildCompleted marks a completed gather child.
	GatherChildCompleted = "gather.child.completed"
	// WorkflowReviewRejections records workflow review rejection count.
	WorkflowReviewRejections = "workflow.review.rejections"
	// WorkflowStatePhase records workflow phase.
	WorkflowStatePhase = "workflow.state.phase"
	// WorkflowStateStatus records workflow status.
	WorkflowStateStatus = "workflow.state.status"
	// WorkflowTokensTotal records total workflow tokens.
	WorkflowTokensTotal = "workflow.tokens.total"
)

func init() {
	Register()
}

// Register declares every shipped rule-pack predicate. It is idempotent and
// is called explicitly by production composition roots so startup validation
// never depends on incidental package initialization order.
func Register() {
	for _, predicate := range []string{
		AgenticCheckpointCompleted,
		AgenticCheckpointIteration,
		AgenticCheckpointStarted,
		AgenticDecisionDetected,
		AgenticFileModified,
		AgenticToolFileOperation,
		AgenticToolUsed,
		EntityIdentityType,
		GatherChildCompleted,
		WorkflowReviewRejections,
		WorkflowStatePhase,
		WorkflowStateStatus,
		WorkflowTokensTotal,
	} {
		vocabulary.Register(predicate)
	}
}

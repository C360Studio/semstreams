// Package builtinprojection owns the projection contracts shared by the
// framework binaries and their built-in graph writers.
package builtinprojection

import (
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// OwnerID and the contract/group names identify the built-in agentic
// projection owner and its immutable contract bindings.
const (
	OwnerID                   = "agentic-loop-graph-writer"
	LoopExecutionContractName = "agentic.loop-execution"
	TodoGroupName             = "todos"
	LessonRecordContractName  = "agentic.lesson-record"
	LessonLifecycleGroupName  = "lesson-lifecycle"
)

// Contracts returns fresh copies of the complete static contracts used by the
// built-in agentic writers.
func Contracts() []projection.Contract {
	return []projection.Contract{
		{
			Name:          LoopExecutionContractName,
			MessageType:   agentic.LoopExecutionMessageType().Key(),
			EntityPattern: "*.*.agent.agentic-loop.execution.*",
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
			Groups: []projection.PredicateGroup{{
				Name: TodoGroupName,
				Mode: ownership.ModeReplaceOwned,
				Predicates: []string{
					agvocab.TodoID,
					agvocab.TodoContent,
					agvocab.TodoStatus,
					agvocab.TodoPosition,
					agvocab.TodoUpdatedAt,
				},
			}},
		},
		{
			Name:          LessonRecordContractName,
			MessageType:   agentic.AgentLessonMessageType().Key(),
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
			Groups: []projection.PredicateGroup{{
				Name: LessonLifecycleGroupName,
				Mode: ownership.ModeReplaceOwned,
				Predicates: []string{
					agvocab.LessonStatus,
					agvocab.LessonSupersededBy,
					agvocab.LessonRetiredAt,
				},
			}},
		},
	}
}

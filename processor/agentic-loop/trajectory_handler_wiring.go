package agenticloop

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

const trajectoryAuditBatchBudget = 250 * time.Millisecond

type trajectoryToolCompletionEvidence struct {
	Result            agentic.ToolResult `json:"result"`
	DispatchArguments map[string]any     `json:"dispatch_arguments,omitempty"`
}

type trajectoryCompactionEvidence struct {
	Before []agentic.ChatMessage `json:"before"`
	After  []agentic.ChatMessage `json:"after"`
	Result CompactionResult      `json:"result"`
}

type trajectoryTerminalEvidence struct {
	Loop       agentic.LoopEntity          `json:"loop"`
	Completion *agentic.LoopCompletedEvent `json:"completion,omitempty"`
	Failure    *agentic.LoopFailedEvent    `json:"failure,omitempty"`
	Cancelled  *agentic.LoopCancelledEvent `json:"cancelled,omitempty"`
}

func appendTrajectoryObservation(result *HandlerResult, observation trajectoryObservation) {
	result.trajectoryObservations = append(result.trajectoryObservations, observation)
}

func (c *Component) recordTrajectoryObservations(ctx context.Context, result HandlerResult) {
	c.recordTrajectoryBatch(ctx, result.trajectoryObservations)
}

func (c *Component) recordResultTerminalObservation(ctx context.Context, result HandlerResult) {
	observation, ok := c.resultTerminalObservation(result)
	if !ok {
		return
	}
	c.recordTrajectoryBatch(ctx, []trajectoryObservation{observation})
}

func (c *Component) recordHandlerResultTrajectory(ctx context.Context, result HandlerResult) {
	observations := append([]trajectoryObservation(nil), result.trajectoryObservations...)
	if terminal, ok := c.resultTerminalObservation(result); ok {
		observations = append(observations, terminal)
	}
	c.recordTrajectoryBatch(ctx, observations)
}

func (c *Component) resultTerminalObservation(result HandlerResult) (trajectoryObservation, bool) {
	if result.State != agentic.LoopStateComplete && result.State != agentic.LoopStateFailed {
		return trajectoryObservation{}, false
	}
	entity, _ := c.handler.GetLoop(result.LoopID)
	terminal := trajectoryTerminalEvidence{
		Loop:       entity,
		Completion: result.CompletionState,
		Failure:    result.FailureState,
	}
	status := agentic.TrajectoryStatusCompleted
	errorCategory := agentic.TrajectoryErrorCategory("")
	if result.State == agentic.LoopStateFailed {
		status = agentic.TrajectoryStatusFailed
		errorCategory = agentic.TrajectoryErrorUnknown
	}
	return newTerminalObservation(result.LoopID, status, errorCategory, terminal), true
}

func (c *Component) recordTerminalObservation(
	ctx context.Context,
	loopID string,
	status agentic.TrajectoryStatus,
	errorCategory agentic.TrajectoryErrorCategory,
	evidence trajectoryTerminalEvidence,
) {
	c.recordTrajectoryBatch(ctx, []trajectoryObservation{
		newTerminalObservation(loopID, status, errorCategory, evidence),
	})
}

func newTerminalObservation(
	loopID string,
	status agentic.TrajectoryStatus,
	errorCategory agentic.TrajectoryErrorCategory,
	evidence trajectoryTerminalEvidence,
) trajectoryObservation {
	iteration := uint32(0)
	elapsed := int64(0)
	if evidence.Loop.ID != "" {
		if evidence.Loop.Iterations > 0 {
			iteration = uint32(evidence.Loop.Iterations)
		}
		if !evidence.Loop.StartedAt.IsZero() {
			elapsed = time.Since(evidence.Loop.StartedAt).Milliseconds()
		}
	}
	return trajectoryObservation{
		LoopID:          loopID,
		Kind:            agentic.TrajectoryKindLoopTerminal,
		CausalIteration: iteration,
		CausalPhase:     agentic.TrajectoryPhaseTerminal,
		ElapsedMS:       elapsed,
		Status:          status,
		ErrorCategory:   errorCategory,
		Evidence:        evidence,
	}
}

func (c *Component) recordTrajectoryBatch(parent context.Context, observations []trajectoryObservation) {
	if c.trajectoryRecorder == nil || len(observations) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), trajectoryAuditBatchBudget)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		release, acquired := c.trajectoryRecorder.acquireLoopBatch(ctx, observations[0].LoopID)
		if !acquired {
			return
		}
		defer release()
		for _, observation := range observations {
			if ctx.Err() != nil {
				return
			}
			c.trajectoryRecorder.record(ctx, observation)
		}
	}()

	select {
	case <-done:
		if ctx.Err() == nil {
			return
		}
	case <-ctx.Done():
	}
	first := observations[0]
	c.reportTrajectoryAuditFailure(trajectoryAuditFailure{
		Stage: trajectoryStageBatchBudget, Kind: first.Kind, Reason: trajectoryReasonTimeout,
		LoopID: first.LoopID,
		Err:    fmt.Errorf("trajectory audit batch exceeded framework budget %s", trajectoryAuditBatchBudget),
	})
}

func positiveUint32(value int) uint32 {
	if value <= 0 {
		return 0
	}
	return uint32(value)
}

func positiveUint64(value int) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func modelResponseStatus(response agentic.AgentResponse) agentic.TrajectoryStatus {
	if response.Status == agentic.StatusError || response.Status == agentic.StatusLengthTruncated {
		return agentic.TrajectoryStatusFailed
	}
	return agentic.TrajectoryStatusCompleted
}

func modelResponseErrorCategory(response agentic.AgentResponse) agentic.TrajectoryErrorCategory {
	if response.Status == agentic.StatusLengthTruncated {
		return agentic.TrajectoryErrorValidation
	}
	if response.Status == agentic.StatusError {
		return agentic.TrajectoryErrorModel
	}
	return ""
}

func toolResultStatus(result agentic.ToolResult) agentic.TrajectoryStatus {
	if result.Error != "" {
		return agentic.TrajectoryStatusFailed
	}
	return agentic.TrajectoryStatusCompleted
}

func toolErrorCategory(result agentic.ToolResult) agentic.TrajectoryErrorCategory {
	if result.Error == "" {
		return ""
	}
	switch result.ErrorKind {
	case agentic.ToolErrorPermission:
		return agentic.TrajectoryErrorPermission
	case agentic.ToolErrorInvalidArgs:
		return agentic.TrajectoryErrorValidation
	default:
		return agentic.TrajectoryErrorTool
	}
}

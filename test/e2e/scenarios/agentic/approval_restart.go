package agentic

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/nats-io/nats.go/jetstream"
)

const approvalProcessStartMetric = "process_start_time_seconds"

type approvalRestartCheckpoint struct {
	loop     *agentic.LoopEntity
	call     agentic.ToolCall
	stream   jetstream.Stream
	source   uint64
	ackFloor uint64
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func (s *Scenario) walkApprovalAfterRestart(ctx context.Context, result *scenarios.Result) error {
	before, err := s.metricWithLabels(ctx, approvalProcessStartMetric, nil)
	if err != nil || before <= 0 {
		return fmt.Errorf("read original process start time: value=%v error=%v", before, err)
	}
	labels := map[string]string{"tool_name": approvalGatedTool, "status": toolExecutionSucceeded}
	executionsBefore, err := s.metricWithLabels(ctx, toolExecutionsMetric, labels)
	if err != nil {
		return err
	}
	task := newApprovalGatedTask(time.Now(), "approval-restart", approvalLoopOwner)
	if err := s.publishTask(ctx, "agent.task.e2e-approval-restart", task); err != nil {
		return err
	}
	checkpoint, err := s.awaitSettledApprovalCheckpoint(ctx, task)
	if err != nil {
		return err
	}
	atGate, err := s.metricWithLabels(ctx, toolExecutionsMetric, labels)
	if err != nil || atGate != executionsBefore {
		return fmt.Errorf("gated tool executed before approval: before=%v parked=%v error=%v", executionsBefore, atGate, err)
	}
	result.Details["approval_restart_loop_id"] = task.LoopID
	result.Details["approval_restart_execution_id"] = checkpoint.call.ExecutionID
	result.Details["approval_restart_source_sequence"] = checkpoint.source
	result.Details["approval_restart_source_ack_floor"] = checkpoint.ackFloor
	result.Details["approval_restart_process_before"] = before
	if err := s.awaitSettledApprovalNotifications(ctx, result, checkpoint); err != nil {
		return err
	}
	if err := s.replaceSemStreams(ctx, newComposeProcessController(s.config.ComposeFile)); err != nil {
		return err
	}
	after, err := s.metricWithLabels(ctx, approvalProcessStartMetric, nil)
	if err != nil || after <= before {
		return fmt.Errorf("application process was not replaced: before=%v after=%v error=%v", before, after, err)
	}
	result.Details["approval_restart_process_after"] = after
	recovered, err := s.awaitLoopState(ctx, task.LoopID, agentic.LoopStateAwaitingApproval)
	if err != nil {
		return err
	}
	if recovered.ID != task.LoopID || recovered.TaskID != task.TaskID ||
		!reflect.DeepEqual(recovered.PendingApproval, checkpoint.loop.PendingApproval) ||
		!reflect.DeepEqual(recovered.PendingToolResults, checkpoint.loop.PendingToolResults) {
		return fmt.Errorf("replacement changed the retained approval boundary for loop %s", task.LoopID)
	}
	// Process-local counters reset. A count here would mean execution happened
	// before the external approver acted, not a successful recovered approval.
	replacementExecutions, err := s.metricWithLabels(ctx, toolExecutionsMetric, labels)
	if err != nil || replacementExecutions != 0 {
		return fmt.Errorf("replacement executed gated tool before approval: count=%v error=%v", replacementExecutions, err)
	}
	if err := s.submitApproval(ctx, task.LoopID, checkpoint.call.ExecutionID, agentic.ApprovalDecisionApprove); err != nil {
		return fmt.Errorf("approve after application replacement: %w", err)
	}
	if err := s.verifyApprovalResponsePublished(ctx, task.LoopID, checkpoint.call.ID, checkpoint.call.ExecutionID); err != nil {
		return err
	}
	if err := s.verifyApprovedRestartCall(ctx, checkpoint); err != nil {
		return err
	}
	result.Details["approval_restart_tool_call_verified"] = true
	return s.verifyRestartedApprovalCompletion(ctx, result, checkpoint, labels)
}

// Dispatch notifications must be acknowledged, not merely present in AGENT.
// Otherwise their replay could repopulate the replacement's old process cache
// and mask the absence of durable approval reconstruction.
func (s *Scenario) awaitSettledApprovalNotifications(
	ctx context.Context, result *scenarios.Result, checkpoint approvalRestartCheckpoint,
) error {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return err
	}
	for _, lane := range []struct{ name, subject, consumer string }{
		{"created", "agent.created.", "agentic-dispatch-agent-created"},
		{"pending", "agent.approval_pending.", "agentic-dispatch-agent-approval-pending"},
	} {
		subject := lane.subject + checkpoint.loop.ID
		streamName, err := js.StreamNameBySubject(ctx, subject)
		if err != nil {
			return err
		}
		stream, err := js.Stream(ctx, streamName)
		if err != nil {
			return err
		}
		source, err := stream.GetLastMsgForSubject(ctx, subject)
		if err != nil {
			return err
		}
		decoded, err := s.decoder.Decode(source.Data)
		if err != nil {
			return err
		}
		matches := false
		switch event := decoded.Payload().(type) {
		case *agentic.LoopCreatedEvent:
			matches = lane.name == "created" && event.LoopID == checkpoint.loop.ID && event.TaskID == checkpoint.loop.TaskID
		case *agentic.ApprovalPendingEvent:
			matches = lane.name == "pending" && event.LoopID == checkpoint.loop.ID && event.CallID == checkpoint.call.ID &&
				event.ExecutionID == checkpoint.call.ExecutionID &&
				event.ToolName == checkpoint.call.Name && event.TraceID == checkpoint.call.TraceID &&
				reflect.DeepEqual(event.Arguments, checkpoint.call.Arguments) && agentic.IsApprovalRequired(event.Reason)
		}
		if !matches {
			return fmt.Errorf("dispatch %s notification does not match approval loop %s", lane.name, checkpoint.loop.ID)
		}
		consumer, err := stream.Consumer(ctx, lane.consumer)
		if err != nil {
			return err
		}
		info, err := consumer.Info(ctx)
		if err != nil {
			return err
		}
		if info.Stream != streamName || info.Name != lane.consumer || info.Config.FilterSubject != lane.subject+"*" {
			return fmt.Errorf("unexpected dispatch %s owner: stream=%q consumer=%q filter=%q", lane.name,
				info.Stream, info.Name, info.Config.FilterSubject)
		}
		info, err = waitForApprovalNotificationSettled(ctx, consumer, source.Sequence, s.config.TaskTimeout)
		if err != nil {
			return fmt.Errorf("dispatch %s notification must settle before restart: %w", lane.name, err)
		}
		prefix := "approval_restart_" + lane.name
		result.Details[prefix+"_sequence"] = source.Sequence
		result.Details[prefix+"_ack_floor"] = info.AckFloor.Stream
		result.Details[prefix+"_pending"] = info.NumAckPending
		result.Details[prefix+"_queued"] = info.NumPending
	}
	return nil
}

func waitForApprovalNotificationSettled(
	ctx context.Context, consumer jetstream.Consumer, source uint64, timeout time.Duration,
) (*jetstream.ConsumerInfo, error) {
	if err := waitForConsumerDelivery(ctx, consumer, source, timeout); err != nil {
		return nil, err
	}
	info, err := consumer.Info(ctx)
	if err != nil {
		return nil, err
	}
	if err := waitForConsumerSettled(ctx, consumer, info.Delivered.Consumer, timeout); err != nil {
		return nil, err
	}
	info, err = consumer.Info(ctx)
	if err != nil {
		return nil, err
	}
	if source == 0 || info.AckFloor.Stream < source || info.NumAckPending != 0 || info.NumPending != 0 {
		return nil, fmt.Errorf("notification source=%d ack_floor=%d pending=%d queued=%d",
			source, info.AckFloor.Stream, info.NumAckPending, info.NumPending)
	}
	return info, nil
}

func (s *Scenario) verifyApprovedRestartCall(ctx context.Context, checkpoint approvalRestartCheckpoint) error {
	call, err := s.awaitReplayToolCall(ctx, checkpoint.stream, checkpoint.call.LoopID, checkpoint.call.Name, checkpoint.source)
	if err != nil {
		return err
	}
	expected := checkpoint.call
	expected.ApprovedBy = approvalRequester
	// The pending checkpoint retains these approval fields, not transport
	// metadata that dispatchToolCall adds (including metadata.loop_id).
	if call.ID != expected.ID || call.Name != expected.Name || call.LoopID != expected.LoopID ||
		call.TraceID != expected.TraceID || call.RequestID != expected.RequestID ||
		call.ExecutionID != expected.ExecutionID || call.CallOrdinal != expected.CallOrdinal ||
		call.ApprovedBy != expected.ApprovedBy || !reflect.DeepEqual(call.Arguments, expected.Arguments) {
		return fmt.Errorf("approved call identity, arguments, or approver differs from retained approval: got=%+v want=%+v", call, expected)
	}
	return nil
}

func (s *Scenario) awaitSettledApprovalCheckpoint(
	ctx context.Context, task agentic.TaskMessage,
) (approvalRestartCheckpoint, error) {
	var checkpoint approvalRestartCheckpoint
	pending, err := s.awaitApprovalPending(ctx, task.LoopID)
	if err != nil {
		return checkpoint, err
	}
	loop, err := s.awaitLoopState(ctx, task.LoopID, agentic.LoopStateAwaitingApproval)
	if err != nil {
		return checkpoint, err
	}
	approval := loop.PendingApproval
	if loop.ID != task.LoopID || loop.TaskID != task.TaskID || loop.UserID != task.UserID || approval == nil ||
		approval.RequestID == "" || approval.ExecutionID == "" || approval.CallOrdinal == 0 ||
		approval.CallID != pending.CallID || approval.ExecutionID != pending.ExecutionID || approval.ToolName != approvalGatedTool ||
		pending.ToolName != approval.ToolName || pending.TraceID != approval.TraceID ||
		!reflect.DeepEqual(pending.Arguments, approval.Arguments) || !agentic.IsApprovalRequired(pending.Reason) {
		return checkpoint, fmt.Errorf("durable approval checkpoint does not match task and pending event for %s", task.LoopID)
	}
	checkpoint.loop = loop
	checkpoint.call = agentic.ToolCall{
		ID: approval.CallID, Name: approval.ToolName, Arguments: approval.Arguments,
		LoopID: loop.ID, TraceID: approval.TraceID, RequestID: approval.RequestID,
		ExecutionID: approval.ExecutionID, CallOrdinal: approval.CallOrdinal,
	}
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return checkpoint, err
	}
	subject := "tool.result." + approval.ExecutionID
	streamName, err := js.StreamNameBySubject(ctx, subject)
	if err != nil {
		return checkpoint, fmt.Errorf("resolve approval result stream for %s: %w", subject, err)
	}
	checkpoint.stream, err = js.Stream(ctx, streamName)
	if err != nil {
		return checkpoint, err
	}
	stored, err := checkpoint.stream.GetLastMsgForSubject(ctx, subject)
	if err != nil {
		return checkpoint, err
	}
	if err := s.validateReplayedToolResult(checkpoint.call, stored.Data); err != nil {
		return checkpoint, err
	}
	decoded, err := s.decoder.Decode(stored.Data)
	if err != nil {
		return checkpoint, err
	}
	toolResult := decoded.Payload().(*agentic.ToolResult) // Type checked by validateReplayedToolResult.
	retained, ok := loop.PendingToolResults[approval.ExecutionID]
	if !ok || !reflect.DeepEqual(retained, *toolResult) || !agentic.IsApprovalRequired(toolResult.Error) {
		return checkpoint, fmt.Errorf("approval-required result is not retained under execution %s", approval.ExecutionID)
	}
	// This is the shipped tool.result.> input, not the tools executor consumer.
	consumer, err := checkpoint.stream.Consumer(ctx, "agentic-loop-tool-result-all")
	if err != nil {
		return checkpoint, err
	}
	info, err := consumer.Info(ctx)
	if err != nil {
		return checkpoint, err
	}
	if info.Stream != streamName || info.Config.FilterSubject != "tool.result.>" {
		return checkpoint, fmt.Errorf("unexpected approval result owner: stream=%q filter=%q", info.Stream, info.Config.FilterSubject)
	}
	if err := waitForConsumerDelivery(ctx, consumer, stored.Sequence, s.config.TaskTimeout); err != nil {
		return checkpoint, err
	}
	info, err = consumer.Info(ctx)
	if err != nil {
		return checkpoint, err
	}
	if err := waitForConsumerSettled(ctx, consumer, info.Delivered.Consumer, s.config.TaskTimeout); err != nil {
		return checkpoint, err
	}
	info, err = consumer.Info(ctx)
	if err != nil || info.AckFloor.Stream < stored.Sequence || info.NumAckPending != 0 {
		return checkpoint, fmt.Errorf("approval-required source %d did not settle: info=%+v error=%v", stored.Sequence, info, err)
	}
	checkpoint.source, checkpoint.ackFloor = stored.Sequence, info.AckFloor.Stream
	return checkpoint, nil
}

func (s *Scenario) verifyRestartedApprovalCompletion(
	ctx context.Context, result *scenarios.Result, checkpoint approvalRestartCheckpoint, labels map[string]string,
) error {
	if err := s.waitMetricWithLabels(ctx, toolExecutionsMetric, labels, 1, s.config.CompleteTimeout); err != nil {
		return fmt.Errorf("approved replacement tool did not execute: %w", err)
	}
	outcome, err := s.awaitTerminalOutcome(ctx, checkpoint.loop.ID)
	if err != nil {
		return err
	}
	if outcome != agentic.OutcomeSuccess {
		return fmt.Errorf("restarted approval loop outcome=%q", outcome)
	}
	terminal, err := s.awaitLoopState(ctx, checkpoint.loop.ID, agentic.LoopStateComplete)
	if err != nil {
		return err
	}
	if terminal.ID != checkpoint.loop.ID || terminal.TaskID != checkpoint.loop.TaskID ||
		terminal.Outcome != agentic.OutcomeSuccess || terminal.PendingApproval != nil {
		return fmt.Errorf("restarted approval terminal marker does not match loop %s", checkpoint.loop.ID)
	}
	stored, err := checkpoint.stream.GetLastMsgForSubject(ctx, "tool.result."+checkpoint.call.ExecutionID)
	if err != nil {
		return err
	}
	if stored.Sequence <= checkpoint.source {
		return fmt.Errorf("approval only retained its pre-restart result at sequence %d", stored.Sequence)
	}
	if err := s.validateReplayedToolResult(checkpoint.call, stored.Data); err != nil {
		return err
	}
	decoded, err := s.decoder.Decode(stored.Data)
	if err != nil {
		return err
	}
	if toolResult := decoded.Payload().(*agentic.ToolResult); toolResult.Error != "" || toolResult.Content == "" {
		return fmt.Errorf("approved tool did not return successful content: %+v", toolResult)
	}
	executions, err := s.metricWithLabels(ctx, toolExecutionsMetric, labels)
	if err != nil || executions != 1 {
		return fmt.Errorf("approved replacement executor count=%v, want exactly 1: %v", executions, err)
	}
	result.Details["approval_restart_result_sequence"] = stored.Sequence
	result.Details["approval_restart_tool_executions"] = executions
	result.Details["approval_restart_outcome"] = outcome
	return nil
}

func validateApprovalRestartEvidence(details map[string]any) error {
	for _, lane := range []string{"created", "pending"} {
		prefix := "approval_restart_" + lane
		sequence, _ := details[prefix+"_sequence"].(uint64)
		ackFloor, _ := details[prefix+"_ack_floor"].(uint64)
		pending, havePending := details[prefix+"_pending"].(int)
		queued, haveQueued := details[prefix+"_queued"].(uint64)
		if sequence == 0 || ackFloor < sequence || !havePending || pending != 0 || !haveQueued || queued != 0 {
			return fmt.Errorf("approval restart lacks settled dispatch %s notification: source=%d ack=%d pending=%d queued=%d",
				lane, sequence, ackFloor, pending, queued)
		}
	}
	if verified, _ := details["approval_restart_tool_call_verified"].(bool); !verified {
		return fmt.Errorf("approval restart did not verify the exact approved tool call")
	}
	before, _ := details["approval_restart_process_before"].(float64)
	after, _ := details["approval_restart_process_after"].(float64)
	source, _ := details["approval_restart_source_sequence"].(uint64)
	ackFloor, _ := details["approval_restart_source_ack_floor"].(uint64)
	toolResult, _ := details["approval_restart_result_sequence"].(uint64)
	executions, _ := details["approval_restart_tool_executions"].(float64)
	outcome, _ := details["approval_restart_outcome"].(string)
	loopID, _ := details["approval_restart_loop_id"].(string)
	executionID, _ := details["approval_restart_execution_id"].(string)
	if before <= 0 || after <= before || source == 0 || ackFloor < source || toolResult <= source ||
		executions != 1 || outcome != agentic.OutcomeSuccess || loopID == "" || executionID == "" {
		return fmt.Errorf("approval restart lacks process replacement, settled source, or exact successful recovery: "+
			"process=%v->%v source=%d ack=%d result=%d executions=%v outcome=%q loop=%q execution=%q",
			before, after, source, ackFloor, toolResult, executions, outcome, loopID, executionID)
	}
	return nil
}

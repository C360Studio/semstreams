package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/test/e2e/harness/processbarrier"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	toolsConsumerName            = "agentic-tools-tool-execute-all"
	dispatchCompleteConsumerName = "agentic-dispatch-agent-complete"
	harnessFinalizationTimeout   = 5 * time.Second
	barrierReleaseFlushTimeout   = 2 * time.Second
)

func (s *Scenario) verifyStageAProcessReplacement(
	ctx context.Context, result *scenarios.Result,
) (runErr error) {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return fmt.Errorf("open JetStream for process replacement: %w", err)
	}
	evidence, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      processbarrier.EvidenceStream,
		Subjects:  []string{processbarrier.EvidenceSubjectPrefix + ">"},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
		Discard:   jetstream.DiscardOld,
		MaxAge:    15 * time.Minute,
		MaxMsgs:   128,
	})
	if err != nil {
		return fmt.Errorf("create process-barrier evidence stream: %w", err)
	}
	defer func() {
		joinHarnessFinalizationError(ctx, &runErr, "delete process-barrier evidence stream", func(finalCtx context.Context) error {
			return js.DeleteStream(finalCtx, processbarrier.EvidenceStream)
		})
	}()
	controller := newComposeProcessController(s.config.ComposeFile)

	if err := s.verifyCompletedOutcomeAcrossReplacement(ctx, result, controller, evidence); err != nil {
		return fmt.Errorf("completed tool replay: %w", err)
	}
	if err := s.verifyToolQuarantineAcrossReplacement(ctx, result, controller, evidence); err != nil {
		return fmt.Errorf("tool quarantine: %w", err)
	}
	if err := s.verifyDispatchAcrossReplacement(ctx, result, controller); err != nil {
		return fmt.Errorf("dispatch quarantine: %w", err)
	}
	return nil
}

func (s *Scenario) verifyCompletedOutcomeAcrossReplacement(
	ctx context.Context,
	result *scenarios.Result,
	controller composeProcessController,
	evidence jetstream.Stream,
) (runErr error) {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return err
	}
	toolStream, err := js.Stream(ctx, "TOOL")
	if err != nil {
		return fmt.Errorf("open TOOL stream: %w", err)
	}
	call := newProcessBarrierCall("completed-replay")
	if err := s.publishToolCall(ctx, call); err != nil {
		return err
	}
	if _, err := waitForBarrierAttempts(ctx, evidence, call.ID, 1, 10*time.Second); err != nil {
		return fmt.Errorf("wait for first executor effect: %w", err)
	}

	originalInfo, err := toolStream.Info(ctx)
	if err != nil {
		return fmt.Errorf("read TOOL stream config: %w", err)
	}
	originalConfig := originalInfo.Config
	faultConfig := originalInfo.Config
	faultConfig.Discard = jetstream.DiscardNew
	faultConfig.DiscardNewPerSubject = false
	faultConfig.MaxMsgs = int64(originalInfo.State.Msgs)
	if faultConfig.MaxMsgs <= 0 {
		return fmt.Errorf("TOOL stream has no retained request/effect to establish a full boundary")
	}
	if _, err := js.UpdateStream(ctx, faultConfig); err != nil {
		return fmt.Errorf("install completed-result publication fault: %w", err)
	}
	restored := false
	defer func() {
		if !restored {
			joinHarnessFinalizationError(ctx, &runErr, "restore TOOL stream", func(finalCtx context.Context) error {
				_, restoreErr := js.UpdateStream(finalCtx, originalConfig)
				return restoreErr
			})
		}
	}()

	publishFailuresBefore, err := s.metricWithLabels(ctx,
		"semstreams_agentic_tools_result_publish_failures_total", map[string]string{"reason": "transport"})
	if err != nil {
		return fmt.Errorf("read result-publication baseline: %w", err)
	}
	if err := s.releaseBarrier(ctx, call.ID); err != nil {
		return err
	}
	if err := s.waitForOutcome(ctx, call.ID, 10*time.Second); err != nil {
		return fmt.Errorf("completed outcome was not durable before replacement: %w", err)
	}
	if err := s.waitMetricWithLabels(ctx, "semstreams_agentic_tools_result_publish_failures_total",
		map[string]string{"reason": "transport"}, publishFailuresBefore+1, 10*time.Second); err != nil {
		return fmt.Errorf("completed result publication did not fail: %w", err)
	}
	if _, err := js.UpdateStream(ctx, originalConfig); err != nil {
		return fmt.Errorf("restore TOOL stream before replacement: %w", err)
	}
	restored = true

	if err := s.replaceSemStreams(ctx, controller); err != nil {
		return err
	}
	if err := s.waitForToolResult(ctx, call, 45*time.Second); err != nil {
		return fmt.Errorf("replacement did not replay completed result: %w", err)
	}
	attempts, err := barrierAttemptCount(ctx, evidence, call.ID)
	if err != nil {
		return err
	}
	if attempts != 1 {
		return fmt.Errorf("completed replay executor effects = %d, want exactly 1", attempts)
	}
	replacementExecutions, err := s.metricWithLabels(ctx, "semstreams_agentic_tools_executions_total",
		map[string]string{"tool_name": processbarrier.ToolName})
	if err != nil {
		return fmt.Errorf("read replacement execution count: %w", err)
	}
	if replacementExecutions != 0 {
		return fmt.Errorf("replacement executor count = %.0f, want 0 for completed replay", replacementExecutions)
	}
	result.Details["replacement_replay_call_id"] = call.ID
	result.Metrics["replacement_replay_executor_effects"] = attempts
	return nil
}

func (s *Scenario) verifyToolQuarantineAcrossReplacement(
	ctx context.Context,
	result *scenarios.Result,
	controller composeProcessController,
	evidence jetstream.Stream,
) error {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return err
	}
	toolStream, err := js.Stream(ctx, "TOOL")
	if err != nil {
		return fmt.Errorf("open TOOL stream: %w", err)
	}
	consumer, err := toolStream.Consumer(ctx, toolsConsumerName)
	if err != nil {
		return fmt.Errorf("open tools consumer: %w", err)
	}
	baselineInfo, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read tools consumer baseline: %w", err)
	}

	call := newProcessBarrierCall("ambiguous-create")
	if err := s.publishToolCall(ctx, call); err != nil {
		return err
	}
	first, err := waitForBarrierAttempts(ctx, evidence, call.ID, 1, 10*time.Second)
	if err != nil {
		return fmt.Errorf("wait for ambiguous executor effect: %w", err)
	}
	if err := s.nats.Client().DeleteKeyValueBucket(ctx, graph.BucketToolCallOutcomes); err != nil {
		return fmt.Errorf("remove test outcome bucket after entered barrier: %w", err)
	}
	if err := s.releaseBarrier(ctx, call.ID); err != nil {
		return err
	}
	if err := s.waitForComponentHealth(ctx, "agentic-tools", false, 10*time.Second); err != nil {
		return fmt.Errorf("quarantine did not fail tools health: %w", err)
	}
	quarantinedInfo, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read quarantined tools consumer: %w", err)
	}
	if quarantinedInfo.AckFloor.Consumer != baselineInfo.AckFloor.Consumer || quarantinedInfo.NumAckPending == 0 {
		return fmt.Errorf("quarantined delivery settled or lost authority: ack floor=%d (baseline %d) pending=%d",
			quarantinedInfo.AckFloor.Consumer, baselineInfo.AckFloor.Consumer, quarantinedInfo.NumAckPending)
	}

	blocked := newProcessBarrierCall("post-latch")
	if err := s.publishToolCall(ctx, blocked); err != nil {
		return err
	}
	if err := waitWithoutBarrierAttempt(ctx, evidence, blocked.ID, 2*time.Second); err != nil {
		return err
	}
	postLatchInfo, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read post-latch tools consumer: %w", err)
	}
	if postLatchInfo.Delivered.Consumer != quarantinedInfo.Delivered.Consumer {
		return fmt.Errorf("post-latch delivery count advanced %d -> %d",
			quarantinedInfo.Delivered.Consumer, postLatchInfo.Delivered.Consumer)
	}

	if err := s.replaceSemStreams(ctx, controller); err != nil {
		return err
	}
	replacementAttempt, err := waitForBarrierAttempts(ctx, evidence, blocked.ID, 1, 15*time.Second)
	if err != nil {
		return fmt.Errorf("replacement did not reconstruct ordinary admission: %w", err)
	}
	if err := s.releaseBarrier(ctx, blocked.ID); err != nil {
		return err
	}
	second, err := waitForBarrierAttempts(ctx, evidence, call.ID, 2, 25*time.Second)
	if err != nil {
		return fmt.Errorf("quarantined work did not redeliver on first BackOff class: %w", err)
	}
	delta, err := validateFirstBackOffEvidence(first, replacementAttempt, second)
	if err != nil {
		return err
	}
	if err := s.releaseBarrier(ctx, call.ID); err != nil {
		return err
	}
	if err := s.waitForToolResult(ctx, call, 15*time.Second); err != nil {
		return fmt.Errorf("redelivered quarantined call did not settle: %w", err)
	}
	if err := s.waitForToolResult(ctx, blocked, 15*time.Second); err != nil {
		return fmt.Errorf("post-latch call did not settle after reconstruction: %w", err)
	}
	result.Details["tools_quarantine_call_id"] = call.ID
	result.Metrics["tools_backoff_redelivery_ms"] = delta.Milliseconds()
	result.Metrics["tools_quarantine_executor_attempts"] = 2
	return nil
}

func (s *Scenario) verifyDispatchAcrossReplacement(
	ctx context.Context,
	result *scenarios.Result,
	controller composeProcessController,
) (runErr error) {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return err
	}
	agentStream, err := js.Stream(ctx, "AGENT")
	if err != nil {
		return fmt.Errorf("open AGENT stream: %w", err)
	}
	consumer, err := agentStream.Consumer(ctx, dispatchCompleteConsumerName)
	if err != nil {
		return fmt.Errorf("open dispatch complete consumer: %w", err)
	}
	if _, err := agentStream.PauseConsumer(ctx, dispatchCompleteConsumerName, time.Now().Add(2*time.Minute)); err != nil {
		return fmt.Errorf("pause dispatch complete consumer: %w", err)
	}
	baselineInfo, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read paused dispatch consumer baseline: %w", err)
	}
	paused := true
	defer func() {
		if paused {
			joinHarnessFinalizationError(ctx, &runErr, "resume dispatch complete consumer", func(finalCtx context.Context) error {
				_, resumeErr := agentStream.ResumeConsumer(finalCtx, dispatchCompleteConsumerName)
				return resumeErr
			})
		}
	}()

	terminal, responseSubject, err := s.newDispatchTerminal(ctx, "unknown-publish")
	if err != nil {
		return err
	}
	if err := s.nats.Publish(ctx, "agent.complete."+terminal.loopID, terminal.wire); err != nil {
		return fmt.Errorf("publish paused terminal: %w", err)
	}
	userStream, err := js.Stream(ctx, "USER")
	if err != nil {
		return fmt.Errorf("open USER stream: %w", err)
	}
	userInfo, err := userStream.Info(ctx)
	if err != nil {
		return fmt.Errorf("read USER stream config: %w", err)
	}
	if userInfo.State.Msgs == 0 {
		return fmt.Errorf("USER stream has no earlier registered response to establish a full boundary")
	}
	originalConfig := userInfo.Config
	faultConfig := userInfo.Config
	faultConfig.Discard = jetstream.DiscardNew
	faultConfig.DiscardNewPerSubject = false
	faultConfig.MaxMsgs = int64(userInfo.State.Msgs)
	if _, err := js.UpdateStream(ctx, faultConfig); err != nil {
		return fmt.Errorf("install dispatch publication fault: %w", err)
	}
	restored := false
	defer func() {
		if !restored {
			joinHarnessFinalizationError(ctx, &runErr, "restore USER stream", func(finalCtx context.Context) error {
				_, restoreErr := js.UpdateStream(finalCtx, originalConfig)
				return restoreErr
			})
		}
	}()

	reasonBefore, err := s.metricWithLabels(ctx,
		"semstreams_router_terminal_settlement_total", map[string]string{"reason": "response_publish_transient"})
	if err != nil {
		return fmt.Errorf("read dispatch settlement baseline: %w", err)
	}
	if _, err := agentStream.ResumeConsumer(ctx, dispatchCompleteConsumerName); err != nil {
		return fmt.Errorf("resume dispatch into publication fault: %w", err)
	}
	paused = false
	if err := s.waitMetricWithLabels(ctx, "semstreams_router_terminal_settlement_total",
		map[string]string{"reason": "response_publish_transient"}, reasonBefore+1, 10*time.Second); err != nil {
		return fmt.Errorf("dispatch unknown publication was not observed: %w", err)
	}
	if err := s.waitForComponentHealth(ctx, "agentic-dispatch", false, 10*time.Second); err != nil {
		return fmt.Errorf("dispatch quarantine did not fail health: %w", err)
	}
	quarantinedInfo, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read quarantined dispatch consumer: %w", err)
	}
	if quarantinedInfo.AckFloor.Consumer != baselineInfo.AckFloor.Consumer || quarantinedInfo.NumAckPending == 0 {
		return fmt.Errorf("quarantined terminal settled or lost authority: ack floor=%d (baseline %d) pending=%d",
			quarantinedInfo.AckFloor.Consumer, baselineInfo.AckFloor.Consumer, quarantinedInfo.NumAckPending)
	}
	if _, err := js.UpdateStream(ctx, originalConfig); err != nil {
		return fmt.Errorf("restore USER stream before replacement: %w", err)
	}
	restored = true

	return s.verifyDispatchRecoveryAfterQuarantine(
		ctx, result, controller, userStream, consumer,
		quarantinedInfo.Delivered.Consumer, terminal, responseSubject,
	)
}

func (s *Scenario) verifyDispatchRecoveryAfterQuarantine(
	ctx context.Context,
	result *scenarios.Result,
	controller composeProcessController,
	userStream jetstream.Stream,
	consumer jetstream.Consumer,
	quarantinedDeliveries uint64,
	terminal dispatchTerminalFixture,
	responseSubject string,
) error {
	blocked, blockedResponseSubject, err := s.newDispatchTerminal(ctx, "post-latch")
	if err != nil {
		return err
	}
	if err := s.nats.Publish(ctx, "agent.complete."+blocked.loopID, blocked.wire); err != nil {
		return fmt.Errorf("publish post-latch terminal: %w", err)
	}
	if err := waitDuration(ctx, 2*time.Second); err != nil {
		return err
	}
	postLatchInfo, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read post-latch dispatch consumer: %w", err)
	}
	if postLatchInfo.Delivered.Consumer != quarantinedDeliveries {
		return fmt.Errorf("dispatch unlimited lane retried after quarantine: deliveries %d -> %d",
			quarantinedDeliveries, postLatchInfo.Delivered.Consumer)
	}

	if err := s.replaceSemStreams(ctx, controller); err != nil {
		return err
	}
	if err := waitForStreamSubject(ctx, userStream, responseSubject, 45*time.Second); err != nil {
		return fmt.Errorf("replacement did not publish quarantined response: %w", err)
	}
	if err := waitForStreamSubject(ctx, userStream, blockedResponseSubject, 15*time.Second); err != nil {
		return fmt.Errorf("replacement did not admit later terminal: %w", err)
	}
	if count, err := streamSubjectCount(ctx, userStream, responseSubject); err != nil || count != 1 {
		return fmt.Errorf("replacement user response count = %d, want 1: %w", count, err)
	}
	settledInfo, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read replacement dispatch consumer: %w", err)
	}
	// Re-publish the identical terminal envelope after successful replacement
	// settlement. The deterministic response MsgID must keep the output at one.
	if err := s.nats.Publish(ctx, "agent.complete."+terminal.loopID, terminal.wire); err != nil {
		return fmt.Errorf("republish identical terminal: %w", err)
	}
	if err := waitForConsumerDelivered(ctx, consumer, settledInfo.Delivered.Consumer+1, 10*time.Second); err != nil {
		return fmt.Errorf("identical terminal was not consumed: %w", err)
	}
	if count, err := streamSubjectCount(ctx, userStream, responseSubject); err != nil || count != 1 {
		return fmt.Errorf("deduplicated user response count = %d, want 1: %w", count, err)
	}
	result.Details["dispatch_replacement_loop_id"] = terminal.loopID
	result.Metrics["dispatch_replacement_user_responses"] = 1
	return nil
}

type dispatchTerminalFixture struct {
	loopID string
	wire   []byte
}

func (s *Scenario) newDispatchTerminal(
	ctx context.Context, label string,
) (dispatchTerminalFixture, string, error) {
	now := time.Now().UTC()
	loopID := fmt.Sprintf("e2e-dispatch-replacement-%s-%d", label, now.UnixNano())
	taskID := "task-" + loopID
	channelID := "channel-" + loopID
	loop := agentic.LoopEntity{
		ID: loopID, TaskID: taskID, State: agentic.LoopStateComplete, MaxIterations: 3,
		ChannelType: "e2e-replacement", ChannelID: channelID,
	}
	data, err := json.Marshal(loop)
	if err != nil {
		return dispatchTerminalFixture{}, "", fmt.Errorf("marshal persisted loop: %w", err)
	}
	if err := s.nats.PutKV(ctx, "AGENT_LOOPS", loopID, data); err != nil {
		return dispatchTerminalFixture{}, "", fmt.Errorf("persist dispatch loop route: %w", err)
	}
	event := &agentic.LoopCompletedEvent{
		LoopID: loopID, TaskID: taskID, Outcome: agentic.OutcomeSuccess,
		Result: "replacement result " + label, CompletedAt: now,
	}
	envelope := message.NewBaseMessage(event.Schema(), event, "e2e-process-replacement")
	wire, err := json.Marshal(envelope)
	if err != nil {
		return dispatchTerminalFixture{}, "", fmt.Errorf("marshal dispatch terminal: %w", err)
	}
	return dispatchTerminalFixture{loopID: loopID, wire: wire},
		"user.response.e2e-replacement." + channelID, nil
}

func newProcessBarrierCall(label string) agentic.ToolCall {
	now := time.Now().UnixNano()
	return agentic.ToolCall{
		ID:      fmt.Sprintf("e2e-process-barrier-%s-%d", label, now),
		Name:    processbarrier.ToolName,
		LoopID:  fmt.Sprintf("e2e-process-loop-%d", now),
		TraceID: fmt.Sprintf("e2e-process-trace-%d", now),
	}
}

func (s *Scenario) publishToolCall(ctx context.Context, call agentic.ToolCall) error {
	envelope := message.NewBaseMessage(call.Schema(), &call, "e2e-process-replacement")
	wire, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal process-barrier call: %w", err)
	}
	if err := s.nats.Publish(ctx, "tool.execute."+call.ID, wire); err != nil {
		return fmt.Errorf("publish process-barrier call: %w", err)
	}
	return nil
}

func (s *Scenario) releaseBarrier(ctx context.Context, callID string) error {
	connection := s.nats.Client().GetConnection()
	if connection == nil {
		return fmt.Errorf("release process barrier: NATS connection is nil")
	}
	if err := connection.Publish(processbarrier.ReleaseSubject(callID), nil); err != nil {
		return fmt.Errorf("publish process barrier release: %w", err)
	}
	if err := flushBarrierRelease(ctx, connection.FlushWithContext); err != nil {
		return fmt.Errorf("flush process barrier release: %w", err)
	}
	return nil
}

func flushBarrierRelease(ctx context.Context, flush func(context.Context) error) error {
	flushCtx, cancel := context.WithTimeout(ctx, barrierReleaseFlushTimeout)
	defer cancel()
	return flush(flushCtx)
}

func (s *Scenario) waitForOutcome(ctx context.Context, callID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	key := "v1." + durableCallDigest(callID)
	for time.Now().Before(deadline) {
		if _, err := s.nats.GetKV(ctx, graph.BucketToolCallOutcomes, key); err == nil {
			return nil
		}
		if err := waitDuration(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("%s/%s was not observable within %v", graph.BucketToolCallOutcomes, key, timeout)
}

func (s *Scenario) waitForToolResult(ctx context.Context, call agentic.ToolCall, timeout time.Duration) error {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return err
	}
	stream, err := js.Stream(ctx, "TOOL")
	if err != nil {
		return err
	}
	raw, err := waitForStreamSubjectData(ctx, stream, "tool.result."+call.ID, timeout)
	if err != nil {
		return err
	}
	var envelope struct {
		Payload agentic.ToolResult `json:"payload"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode process-barrier result: %w", err)
	}
	if envelope.Payload.CallID != call.ID || envelope.Payload.Name != call.Name {
		return fmt.Errorf("tool result correlation = call:%q name:%q, want call:%q name:%q",
			envelope.Payload.CallID, envelope.Payload.Name, call.ID, call.Name)
	}
	return nil
}

func (s *Scenario) replaceSemStreams(ctx context.Context, controller composeProcessController) error {
	if err := controller.kill(ctx); err != nil {
		return err
	}
	if err := controller.start(ctx); err != nil {
		return err
	}
	if err := s.obs.WaitForAllComponentsHealthy(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("replacement components did not become healthy: %w", err)
	}
	return nil
}

func (s *Scenario) waitForComponentHealth(
	ctx context.Context, name string, healthy bool, timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		components, err := s.obs.GetComponents(ctx)
		if err == nil {
			for _, component := range components {
				if component.Name != name {
					continue
				}
				last = fmt.Sprintf("healthy=%v state=%s error=%s", component.Healthy, component.State, component.LastError)
				if component.Healthy == healthy {
					return nil
				}
			}
		}
		if err := waitDuration(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("component %s did not reach healthy=%v within %v (last %s)", name, healthy, timeout, last)
}

func waitForBarrierAttempts(
	ctx context.Context, stream jetstream.Stream, callID string, want uint64, timeout time.Duration,
) (processbarrier.Attempt, error) {
	deadline := time.Now().Add(timeout)
	subject := processbarrier.EvidenceSubject(callID)
	for time.Now().Before(deadline) {
		count, err := streamSubjectCount(ctx, stream, subject)
		if err == nil && count >= want {
			raw, getErr := stream.GetLastMsgForSubject(ctx, subject)
			if getErr != nil {
				return processbarrier.Attempt{}, getErr
			}
			var attempt processbarrier.Attempt
			if err := json.Unmarshal(raw.Data, &attempt); err != nil {
				return processbarrier.Attempt{}, fmt.Errorf("decode process barrier attempt: %w", err)
			}
			if err := attempt.Validate(callID); err != nil {
				return processbarrier.Attempt{}, err
			}
			return attempt, nil
		}
		if err := waitDuration(ctx, 100*time.Millisecond); err != nil {
			return processbarrier.Attempt{}, err
		}
	}
	return processbarrier.Attempt{}, fmt.Errorf("barrier attempts for %s did not reach %d within %v", callID, want, timeout)
}

func waitWithoutBarrierAttempt(ctx context.Context, stream jetstream.Stream, callID string, duration time.Duration) error {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		count, err := barrierAttemptCount(ctx, stream, callID)
		if err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("post-latch call %s executed %d time(s)", callID, count)
		}
		if err := waitDuration(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func barrierAttemptCount(ctx context.Context, stream jetstream.Stream, callID string) (uint64, error) {
	return streamSubjectCount(ctx, stream, processbarrier.EvidenceSubject(callID))
}

func validateFirstBackOffEvidence(
	first processbarrier.Attempt,
	replacement processbarrier.Attempt,
	redelivery processbarrier.Attempt,
) (time.Duration, error) {
	if replacement.ProcessInstance == first.ProcessInstance {
		return 0, fmt.Errorf("post-latch call ran in original process instance %q", first.ProcessInstance)
	}
	replacementAdmissionDelay := replacement.EnteredAt.Sub(first.EnteredAt)
	if replacementAdmissionDelay < 0 || replacementAdmissionDelay >= 12*time.Second {
		return 0, fmt.Errorf("replacement admission took %v, cannot isolate the 15s server BackOff from startup delay",
			replacementAdmissionDelay)
	}
	if redelivery.ProcessInstance != replacement.ProcessInstance {
		return 0, fmt.Errorf("redelivery process instance = %q, want replacement %q",
			redelivery.ProcessInstance, replacement.ProcessInstance)
	}
	delta := redelivery.EnteredAt.Sub(first.EnteredAt)
	// The replacement admitted blocked work before 12s, proving its consumer
	// was ready ahead of the 15s deadline. The retained executor-entry clocks
	// therefore measure the server BackOff rather than compose startup. The
	// upper bound is deliberately below the 30s semantic NakWithDelay policy.
	if delta < 12*time.Second || delta > 22*time.Second {
		return 0, fmt.Errorf("tools crash redelivery interval = %v, want 15s BackOff class (12s..22s)", delta)
	}
	return delta, nil
}

func streamSubjectCount(ctx context.Context, stream jetstream.Stream, subject string) (uint64, error) {
	info, err := stream.Info(ctx, jetstream.WithSubjectFilter(subject))
	if err != nil {
		return 0, err
	}
	return info.State.Subjects[subject], nil
}

func waitForStreamSubject(ctx context.Context, stream jetstream.Stream, subject string, timeout time.Duration) error {
	_, err := waitForStreamSubjectData(ctx, stream, subject, timeout)
	return err
}

func waitForStreamSubjectData(
	ctx context.Context, stream jetstream.Stream, subject string, timeout time.Duration,
) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := stream.GetLastMsgForSubject(ctx, subject)
		if err == nil {
			return raw.Data, nil
		}
		if !errors.Is(err, jetstream.ErrMsgNotFound) {
			return nil, err
		}
		if err := waitDuration(ctx, 200*time.Millisecond); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("subject %s was not stored within %v", subject, timeout)
}

func waitDuration(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitForConsumerDelivered(
	ctx context.Context, consumer jetstream.Consumer, want uint64, timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	var last uint64
	for time.Now().Before(deadline) {
		info, err := consumer.Info(ctx)
		if err == nil {
			last = info.Delivered.Consumer
			if last >= want {
				return nil
			}
		}
		if err := waitDuration(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("consumer deliveries = %d, want at least %d within %v", last, want, timeout)
}

func joinHarnessFinalizationError(
	parent context.Context,
	runErr *error,
	operation string,
	finalize func(context.Context) error,
) {
	finalCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), harnessFinalizationTimeout)
	defer cancel()
	if err := finalize(finalCtx); err != nil {
		*runErr = errors.Join(*runErr, fmt.Errorf("%s: %w", operation, err))
	}
}

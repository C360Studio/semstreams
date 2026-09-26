package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/test/e2e/harness/processbarrier"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	toolsConsumerName            = "agentic-tools-tool-execute-all"
	dispatchCompleteConsumerName = "agentic-dispatch-agent-complete"
	// modelRequestConsumerName is agentic-model's durable consumer over
	// agent.request.>; pausing it is how the mid-flight window below is
	// arranged rather than raced.
	modelRequestConsumerName = "agentic-model-agent-request-all"
	// loopResponseConsumerName is agentic-loop's durable consumer over
	// agent.response.>; its settlement is what proves the recovered delivery
	// was neither quarantined nor left retrying.
	loopResponseConsumerName   = "agentic-loop-agent-response-all"
	harnessFinalizationTimeout = 5 * time.Second
	barrierReleaseFlushTimeout = 2 * time.Second
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
	if err := s.verifyMidFlightLoopAcrossReplacement(ctx, result, controller); err != nil {
		return fmt.Errorf("mid-flight loop: %w", err)
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
	if err := s.waitForOutcome(ctx, call.ExecutionID, 10*time.Second); err != nil {
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
	// A response appearing on the stream does not mean the lane is finished:
	// delivery and publication both precede the ACK. Counting before the
	// deliveries settle can read one response while a duplicate is still
	// inside its callback, about to publish. Settlement is the boundary.
	if err := waitForConsumerSettled(ctx, consumer, 0, 20*time.Second); err != nil {
		return fmt.Errorf("replacement deliveries did not settle before counting responses: %w", err)
	}
	count, err := streamSubjectCount(ctx, userStream, responseSubject)
	if err != nil {
		return fmt.Errorf("count replacement user responses: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("replacement user response count = %d, want 1", count)
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
	// Wait for the replayed delivery to be ACKNOWLEDGED, not merely delivered.
	// The dispatch terminal callback publishes its response synchronously and
	// only then returns the decision the framework ACKs, so an advanced
	// AckFloor proves any duplicate publish has already happened. Delivery
	// alone advances before the callback publishes, which is what let this
	// check count one response while a second was still in flight.
	replayedSequence := settledInfo.Delivered.Consumer + 1
	if err := waitForConsumerSettled(ctx, consumer, replayedSequence, 20*time.Second); err != nil {
		return fmt.Errorf("identical terminal was not settled: %w", err)
	}
	deduplicated, err := streamSubjectCount(ctx, userStream, responseSubject)
	if err != nil {
		return fmt.Errorf("count deduplicated user responses: %w", err)
	}
	if deduplicated != 1 {
		return fmt.Errorf("deduplicated user response count = %d, want 1", deduplicated)
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
	// A canonical framework loop token (ADR-105, #1192). agentic-dispatch reads
	// loop authority out of AGENT_LOOPS and refuses a terminal whose loop id is
	// not a canonical token BEFORE it reads the record (#1329), so a readable
	// synthetic id would be classified routing_malformed and never reach the
	// publish this stage faults.
	loopID := uuid.NewString()
	taskID := fmt.Sprintf("task-e2e-dispatch-replacement-%s-%d", label, now.UnixNano())
	channelID := fmt.Sprintf("channel-e2e-dispatch-replacement-%s-%d", label, now.UnixNano())
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
	call := agentic.ToolCall{
		ID:      fmt.Sprintf("e2e-process-barrier-%s-%d", label, now),
		Name:    processbarrier.ToolName,
		LoopID:  fmt.Sprintf("e2e-process-loop-%d", now),
		TraceID: fmt.Sprintf("e2e-process-trace-%d", now),
	}
	stampInjectedExecutionIdentity(&call)
	return call
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

func (s *Scenario) waitForOutcome(ctx context.Context, executionID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	key := "v1." + durableExecutionDigest(executionID)
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
	raw, err := waitForStreamSubjectData(ctx, stream, "tool.result."+call.ExecutionID, timeout)
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

// waitForConsumerSettled waits until the consumer has no outstanding delivery
// and, when wantAckFloor is nonzero, until its acknowledgement floor has passed
// that consumer sequence. Settlement is the only boundary that proves a
// callback finished its synchronous effects; Delivered advances before they
// run. It polls server state and never sleeps past a condition.
func waitForConsumerSettled(
	ctx context.Context, consumer jetstream.Consumer, wantAckFloor uint64, timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	var lastFloor uint64
	var lastPending int
	for time.Now().Before(deadline) {
		info, err := consumer.Info(ctx)
		if err == nil {
			lastFloor = info.AckFloor.Consumer
			lastPending = info.NumAckPending
			if lastPending == 0 && lastFloor >= wantAckFloor {
				return nil
			}
		}
		if err := waitDuration(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("consumer ack floor = %d (want at least %d) with %d still pending after %v",
		lastFloor, wantAckFloor, lastPending, timeout)
}

// taskLaneConsumerName asks the server which durable consumer covers
// agent.task, instead of assuming the framework's consumer-naming pattern. The
// stage names four consumers as constants because it PAUSES or reads them by
// identity; this one is only waited on, and a guessed name that resolved to
// some other consumer would make that wait pass vacuously — which is the exact
// failure the wait exists to close. The lookup, and its exactly-one rule, is
// laneConsumerName's.
func taskLaneConsumerName(ctx context.Context, stream jetstream.Stream) (string, error) {
	return laneConsumerName(ctx, stream, consumerLane{
		stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.task",
	})
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

// midFlightHandles are the server-side handles the mid-flight check reads and
// waits on: the stream and bucket its assertions read, and the two consumers
// whose settlement separates a recovery from a retry. Each consumer carries the
// acknowledgement floor observed BEFORE the check publishes anything, because
// wantAckFloor = 0 is vacuous — a consumer that has ever acked anything
// satisfies it, including one whose recovery delivery is being retried to death.
type midFlightHandles struct {
	stream           jetstream.Stream
	loops            jetstream.KeyValue
	responseConsumer jetstream.Consumer
	responseFloor    uint64
	taskConsumer     jetstream.Consumer
	taskFloor        uint64
}

func (s *Scenario) openMidFlightHandles(ctx context.Context) (midFlightHandles, error) {
	var handles midFlightHandles
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return handles, err
	}
	handles.stream, err = js.Stream(ctx, "AGENT")
	if err != nil {
		return handles, fmt.Errorf("open AGENT stream: %w", err)
	}
	handles.loops, err = js.KeyValue(ctx, agentLoopsBucket)
	if err != nil {
		return handles, fmt.Errorf("open %s bucket: %w", agentLoopsBucket, err)
	}
	handles.responseConsumer, handles.responseFloor, err =
		openConsumerWithFloor(ctx, handles.stream, loopResponseConsumerName)
	if err != nil {
		return handles, fmt.Errorf("open loop response consumer: %w", err)
	}
	taskName, err := taskLaneConsumerName(ctx, handles.stream)
	if err != nil {
		return handles, err
	}
	handles.taskConsumer, handles.taskFloor, err = openConsumerWithFloor(ctx, handles.stream, taskName)
	if err != nil {
		return handles, fmt.Errorf("open loop task consumer %q: %w", taskName, err)
	}
	return handles, nil
}

// openConsumerWithFloor opens a durable consumer and reads the acknowledgement
// floor it is at right now.
func openConsumerWithFloor(
	ctx context.Context, stream jetstream.Stream, name string,
) (jetstream.Consumer, uint64, error) {
	consumer, err := stream.Consumer(ctx, name)
	if err != nil {
		return nil, 0, err
	}
	info, err := consumer.Info(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("read %s baseline: %w", name, err)
	}
	return consumer, info.AckFloor.Consumer, nil
}

// verifyMidFlightLoopAcrossReplacement is the one claim of #1330 that an
// in-process Component pair cannot make: that a REPLACEMENT OS PROCESS picks up
// a loop it never started and carries it forward, rather than refusing the
// delivery or retrying it to MaxDeliver.
//
// The three checks above are settlement checks — a completed outcome replayed,
// a quarantined tool call redelivered, a quarantined terminal republished.
// None of them holds a LOOP across the replacement. Task 4.2 proves the
// recovery classification against a real broker with two Components in one
// process; what it cannot prove is that the loop survives the process itself,
// which is what the durable record and the retained request exist for.
//
// The window is ARRANGED, not raced. agentic-model's request consumer is paused
// before the task goes in, so the loop reaches exactly the mid-flight state the
// change is about — first request published and named by the record, nothing
// answered — and stays there until the process is killed. After the
// replacement, the model consumer resumes and the answer to that retained
// request arrives at a process with no memory of the loop: the cold arm reads
// the record, rebuilds the loop from the record plus its retained request, and
// advances it.
//
// What the assertions read is the durable state, never a log line: the record's
// revision moved, it names the SECOND request (so the loop advanced an
// iteration, not merely got rewritten), exactly one next request went out on
// agent.request.<loopID>, the loop reached its terminal, and the response lane
// settled with agentic-loop still healthy.
//
// There are THREE ways this can fail to be a recovery, and the assertions
// distinguish all three:
//
//  1. QUARANTINE — the delivery is refused and the component latches. Caught by
//     the agentic-loop health assertion.
//  2. RETRY TO MaxDeliver — the delivery is never applied and never settles.
//     Caught by the consumer-settlement assertion, which requires the ack floor
//     to have PASSED this delivery rather than merely be nonzero.
//  3. REBUILT, THEN FAILED ON THE INHERITED DEADLINE — the subtle one. TimeoutAt
//     is written at loop birth and a rebuild does not refresh it (#1330, task
//     5.5), so a replacement gap longer than the loop's timeout rebuilds the
//     loop and immediately fails it. That ACKNOWLEDGES the delivery and leaves
//     agentic-loop healthy, so shapes 1 and 2 both read clean; what betrays it
//     is that the terminal lands on agent.failed rather than agent.complete.
//     The wait at the end of this check is on agent.complete for exactly that
//     reason, and the premise assertion below refuses to let the race go
//     unnoticed: it reads the record's TimeoutAt after the replacement and says
//     so, naming the budget, rather than timing out on a message that never
//     comes.
func (s *Scenario) verifyMidFlightLoopAcrossReplacement(
	ctx context.Context,
	result *scenarios.Result,
	controller composeProcessController,
) (runErr error) {
	handles, err := s.openMidFlightHandles(ctx)
	if err != nil {
		return err
	}
	agentStream, loops := handles.stream, handles.loops

	if _, err := agentStream.PauseConsumer(ctx, modelRequestConsumerName, time.Now().Add(2*time.Minute)); err != nil {
		return fmt.Errorf("pause model request consumer: %w", err)
	}
	resumed := false
	defer func() {
		if !resumed {
			joinHarnessFinalizationError(ctx, &runErr, "resume model request consumer", func(finalCtx context.Context) error {
				return resumeConsumerIfPaused(finalCtx, agentStream, modelRequestConsumerName)
			})
		}
	}()

	task := newTestTask(time.Now())
	loopID := task.LoopID
	taskData, err := json.Marshal(message.NewBaseMessage(task.Schema(), &task, "e2e-test"))
	if err != nil {
		return fmt.Errorf("marshal mid-flight task: %w", err)
	}
	if err := s.nats.Publish(ctx, "agent.task.midflight", taskData); err != nil {
		return fmt.Errorf("publish mid-flight task: %w", err)
	}

	requestSubject := "agent.request." + loopID
	firstRequest := fmt.Sprintf("%s:req:1:0", loopID)
	nextRequest := fmt.Sprintf("%s:req:2:0", loopID)
	if err := waitForStreamSubject(ctx, agentStream, requestSubject, 30*time.Second); err != nil {
		return fmt.Errorf("loop did not publish its first request: %w", err)
	}
	midFlight, err := waitForLoopRecord(ctx, loops, loopID, 30*time.Second, func(entity agentic.LoopEntity) bool {
		return entity.PublishedRequestID == firstRequest
	})
	if err != nil {
		return fmt.Errorf("loop record did not reach the mid-flight state: %w", err)
	}
	if midFlight.entity.State.IsTerminal() {
		return fmt.Errorf("mid-flight loop %s is already terminal (%s); the model consumer pause did not hold",
			loopID, midFlight.entity.State)
	}

	// Settle the TASK before killing, or this check can pass on the wrong path.
	// Both observations above are made before the task's own acknowledgement —
	// birth writes the record by Create and publishes the request, both ahead of
	// the lane's return — so a kill here can leave the task delivery unsettled.
	// Its redelivery then races the model response to the replacement, and if it
	// wins, the replacement rebuilds the loop from the TASK; the response arrives
	// WARM and the cold-response reconstruction this check exists for never runs.
	// The recorded no-rebuild mutant would go green on that schedule.
	//
	// Waiting here cannot let the loop advance: the model consumer is still
	// paused, so the arranged mid-flight window is unchanged by the wait.
	if err := waitForConsumerSettled(ctx, handles.taskConsumer, handles.taskFloor+1, 30*time.Second); err != nil {
		return fmt.Errorf("the mid-flight task never settled, so killing now would let its redelivery "+
			"rebuild the loop warm and the response would never take the cold arm: %w", err)
	}

	// Still inside the window: R1 outstanding, unanswered, and retained.
	turn, err := s.deferTurnBehindFirstRequest(ctx, handles, task, firstRequest)
	if err != nil {
		return err
	}

	// The process that minted the first request is gone. Nothing of this loop
	// survives except its record and the request the stream retains.
	//
	// The resume sits BETWEEN the kill and the start, rather than after
	// replaceSemStreams, for two reasons: nothing is running to answer the
	// request in that gap, so the window stays arranged; and the replacement
	// then reconciles an ordinary consumer at boot instead of one the harness
	// left paused, which is not a state production ever starts from.
	if err := controller.kill(ctx); err != nil {
		return err
	}
	if err := resumeConsumerIfPaused(ctx, agentStream, modelRequestConsumerName); err != nil {
		return fmt.Errorf("resume model request consumer before the replacement boots: %w", err)
	}
	resumed = true
	if err := controller.start(ctx); err != nil {
		return err
	}
	if err := s.obs.WaitForAllComponentsHealthy(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("replacement components did not become healthy: %w", err)
	}

	// Assert the PREMISE before waiting on a message that depends on it. The
	// loop inherits the TimeoutAt its birth wrote, and a rebuild does not
	// refresh it, so if the replacement gap outran configs/agentic.json's
	// agentic-loop.timeout the loop is already doomed: the recovery will
	// rebuild it and then fail it, agent.complete will never arrive, and the
	// wait below would spend 90s and blame the recovery. Say what actually
	// happened instead.
	gapCheck, err := waitForLoopRecord(ctx, loops, loopID, 10*time.Second, func(agentic.LoopEntity) bool {
		return true
	})
	if err != nil {
		return fmt.Errorf("read the loop record after the replacement: %w", err)
	}
	if !gapCheck.entity.TimeoutAt.IsZero() && time.Now().After(gapCheck.entity.TimeoutAt) {
		return fmt.Errorf(
			"the replacement gap outran the loop's own deadline (timeout_at %s, now %s): the loop will be rebuilt "+
				"and then failed, not recovered. Raise agentic-loop.timeout in configs/agentic.json above the "+
				"replacement window (see taskfiles/e2e/agentic.yml)",
			gapCheck.entity.TimeoutAt.UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	}

	// agent.complete, never merely "a terminal": a loop that was rebuilt and
	// then failed on its inherited deadline settles on agent.failed, and that
	// is the third refusal shape this check exists to catch.
	if err := waitForStreamSubject(ctx, agentStream, "agent.complete."+loopID, 90*time.Second); err != nil {
		failed, failedErr := streamSubjectCount(ctx, agentStream, "agent.failed."+loopID)
		if failedErr == nil && failed > 0 {
			return fmt.Errorf(
				"the replacement settled the mid-flight loop on agent.failed, not agent.complete: it was rebuilt "+
					"and then failed, most likely on the deadline it inherited from its record: %w", err)
		}
		return fmt.Errorf("replacement did not carry the mid-flight loop to a terminal: %w", err)
	}
	advanced, err := waitForLoopRecord(ctx, loops, loopID, 30*time.Second, func(entity agentic.LoopEntity) bool {
		return entity.PublishedRequestID == nextRequest
	})
	if err != nil {
		return fmt.Errorf("replacement did not advance the record past %s: %w", firstRequest, err)
	}
	if advanced.revision <= midFlight.revision {
		return fmt.Errorf("loop record revision did not move: %d -> %d", midFlight.revision, advanced.revision)
	}
	if advanced.entity.Iterations < 1 {
		return fmt.Errorf("loop %s iterations = %d, want at least 1", loopID, advanced.entity.Iterations)
	}

	// Settlement before counting: a request appears on the stream before the
	// delivery that produced it is acknowledged, so counting first can read one
	// request while a duplicate is still inside its callback.
	if err := waitForConsumerSettled(ctx, handles.responseConsumer, handles.responseFloor+1, 30*time.Second); err != nil {
		return fmt.Errorf("replacement response deliveries did not settle past the baseline floor: %w", err)
	}
	requests, err := streamSubjectCount(ctx, agentStream, requestSubject)
	if err != nil {
		return fmt.Errorf("count mid-flight loop requests: %w", err)
	}
	if requests != 2 {
		return fmt.Errorf("requests on %s = %d, want exactly 2 (the retained first and one next)",
			requestSubject, requests)
	}
	if err := s.waitForComponentHealth(ctx, "agentic-loop", true, 10*time.Second); err != nil {
		return fmt.Errorf("agentic-loop did not stay healthy across the mid-flight recovery: %w", err)
	}
	if err := s.verifyDeferredTurnAcrossReplacement(ctx, agentStream, task, nextRequest, turn); err != nil {
		return err
	}

	result.Details["midflight_loop_id"] = loopID
	result.Details["midflight_published_request_id"] = advanced.entity.PublishedRequestID
	result.Metrics["midflight_requests_published"] = requests
	result.Metrics["midflight_record_revision_delta"] = advanced.revision - midFlight.revision
	return nil
}

// deferTurnBehindFirstRequest arranges W-b (#1365, design § 3.1, task 3.7): a
// turn typed while R1 is outstanding is DEFERRED behind it, and the
// replacement must carry it. It returns the turn's text. The turn goes
// in through the production intake — a continuation TaskMessage naming
// this loop on agent.task, where HandleTask's attachContinuation defers it
// and the task lane writes marker and text onto the record — never by
// writing the record. It is W-b only when the kill lands after that write:
// before it (W-a) the turn is the honest loss, so wait for the record to
// carry the text, still naming R1, and for the continuation to settle, or
// its redelivery would reach a replacement that holds no loop and be refused.
func (s *Scenario) deferTurnBehindFirstRequest(
	ctx context.Context, handles midFlightHandles, task agentic.TaskMessage, firstRequest string,
) (string, error) {
	turn := deferredTurnPrompt(task.LoopID)
	continuation := task
	continuation.TaskID = task.TaskID + "-turn"
	continuation.Prompt = turn
	continuationData, err := json.Marshal(message.NewBaseMessage(continuation.Schema(), &continuation, "e2e-test"))
	if err != nil {
		return "", fmt.Errorf("marshal the deferred continuation: %w", err)
	}
	if err := s.nats.Publish(ctx, "agent.task.midflight", continuationData); err != nil {
		return "", fmt.Errorf("publish the deferred continuation: %w", err)
	}
	if _, err := waitForLoopRecord(ctx, handles.loops, task.LoopID, 30*time.Second, func(entity agentic.LoopEntity) bool {
		return entity.PendingContinuation && entity.PendingContinuationRequestID == "" &&
			entity.PendingContinuationPrompt == turn && entity.PublishedRequestID == firstRequest
	}); err != nil {
		return "", fmt.Errorf("W-b premise: the record never carried the deferred turn's marker and text, uncarried, "+
			"while naming %s — a replacement now would test the W-a loss, not W-b: %w", firstRequest, err)
	}
	if err := waitForConsumerSettled(ctx, handles.taskConsumer, handles.taskFloor+2, 30*time.Second); err != nil {
		return "", fmt.Errorf("W-b premise: the deferred continuation never settled, so its redelivery would reach "+
			"the replacement: %w", err)
	}
	return turn, nil
}

// verifyDeferredTurnAcrossReplacement reads, off the wire, what the W-b turn
// became after the replacement: the loop's next request (the last on its
// request subject, the count already pinned to 2) and its agent.complete event,
// both through the production registry.
func (s *Scenario) verifyDeferredTurnAcrossReplacement(
	ctx context.Context, agentStream jetstream.Stream, task agentic.TaskMessage, nextRequest, turn string,
) error {
	stored, err := agentStream.GetLastMsgForSubject(ctx, "agent.request."+task.LoopID)
	if err != nil {
		return fmt.Errorf("W-b: read the mid-flight loop's next request: %w", err)
	}
	decoded, err := s.decoder.Decode(stored.Data)
	if err != nil {
		return fmt.Errorf("W-b: decode the next request through the production registry: %w", err)
	}
	request, ok := decoded.Payload().(*agentic.AgentRequest)
	if !ok {
		return fmt.Errorf("W-b: next request payload type = %T, want *agentic.AgentRequest", decoded.Payload())
	}
	if err := checkDeferredTurnCarriedOnce(request, nextRequest, task.Prompt, turn); err != nil {
		return err
	}

	terminal, err := agentStream.GetLastMsgForSubject(ctx, "agent.complete."+task.LoopID)
	if err != nil {
		return fmt.Errorf("W-b: read the mid-flight loop's agent.complete: %w", err)
	}
	settled, err := s.decoder.Decode(terminal.Data)
	if err != nil {
		return fmt.Errorf("W-b: decode agent.complete through the production registry: %w", err)
	}
	return checkCompletionPrompt(settled.Payload(), task.Prompt, turn)
}

// deferredTurnPrompt is the W-b turn's text: unique to the loop, and sharing no
// substring with the birth prompt, so counting one never counts the other.
func deferredTurnPrompt(loopID string) string {
	return "Deferred turn for loop " + loopID + ": also state when the sensor last reported."
}

// checkDeferredTurnCarriedOnce is W-b's first assertion (#1365, design § 3.1):
// the request after R1 carries the deferred turn in exactly one user message,
// after R1's conversation (the birth prompt, itself exactly once). Zero is the
// turn lost across the replacement; two is a replay on top of a turn some
// request already carried.
func checkDeferredTurnCarriedOnce(request *agentic.AgentRequest, wantRequestID, birthPrompt, turn string) error {
	if request.RequestID != wantRequestID {
		return fmt.Errorf("W-b carries-once: next request id = %q, want %q", request.RequestID, wantRequestID)
	}
	birthAt, turnAt := -1, -1
	var births, turns int
	for i, msg := range request.Messages {
		if msg.Role != "user" {
			continue
		}
		if strings.Contains(msg.Content, birthPrompt) {
			births++
			birthAt = i
		}
		if strings.Contains(msg.Content, turn) {
			turns++
			turnAt = i
		}
	}
	if turns != 1 {
		return fmt.Errorf("W-b carries-once: request %s carries the deferred turn in %d user messages, want "+
			"exactly 1 (0: the replacement lost the turn the record held; 2+: it was replayed over a carrier)",
			wantRequestID, turns)
	}
	if births != 1 {
		return fmt.Errorf("W-b carries-once: request %s carries the birth prompt in %d user messages, want "+
			"exactly 1 (%s)", wantRequestID, births, rebuildFault(births))
	}
	if turnAt < birthAt {
		return fmt.Errorf("W-b carries-once: request %s carries the deferred turn at message %d, before R1's "+
			"conversation (birth prompt at %d); the replay must follow it", wantRequestID, turnAt, birthAt)
	}
	return nil
}

// checkCompletionPrompt is W-b's second assertion: the rebuilt loop's
// agent.complete carries the prompt its record accepted, and under OQ5 (a)
// (design § 0, spec "A rebuilt loop's terminal event carries the prompt its
// record accepted") that is the BIRTH prompt, never the continuation's turn.
func checkCompletionPrompt(payload message.Payload, birthPrompt, turn string) error {
	completed, ok := payload.(*agentic.LoopCompletedEvent)
	if !ok {
		return fmt.Errorf("W-b completion prompt: agent.complete payload type = %T, want *agentic.LoopCompletedEvent",
			payload)
	}
	switch completed.Prompt {
	case birthPrompt:
		return nil
	case "":
		return fmt.Errorf("W-b completion prompt: agent.complete carries no prompt — the rebuilt loop did not " +
			"read task_prompt from its record")
	case turn:
		return fmt.Errorf("W-b completion prompt: agent.complete carries the continuation's turn %q, want the "+
			"birth prompt — task_prompt is birth-only (OQ5 (a)) and the continuation rewrote it", turn)
	default:
		return fmt.Errorf("W-b completion prompt: agent.complete prompt = %q, want the birth prompt %q",
			completed.Prompt, birthPrompt)
	}
}

// loopRecordObservation is one read of a loop's AGENT_LOOPS record with the
// revision it was observed at. The revision is the point: a record that was
// rewritten with the same content is indistinguishable from one nothing
// touched, unless the revision travels with it.
type loopRecordObservation struct {
	entity   agentic.LoopEntity
	revision uint64
	// committed is the time the NATS server stored this revision, on the
	// server's clock.
	committed time.Time
}

// waitForLoopRecord polls the durable record until it satisfies want. It reads
// the bucket directly rather than through GetKV because GetKV returns only the
// value, and this assertion is about the revision as much as the content.
func waitForLoopRecord(
	ctx context.Context,
	loops jetstream.KeyValue,
	loopID string,
	timeout time.Duration,
	want func(agentic.LoopEntity) bool,
) (loopRecordObservation, error) {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		entry, err := loops.Get(ctx, loopID)
		switch {
		case err == nil:
			var entity agentic.LoopEntity
			if err := json.Unmarshal(entry.Value(), &entity); err != nil {
				return loopRecordObservation{}, fmt.Errorf("decode %s/%s: %w", agentLoopsBucket, loopID, err)
			}
			if want(entity) {
				return loopRecordObservation{entity: entity, revision: entry.Revision(), committed: entry.Created()}, nil
			}
			last = fmt.Sprintf("state=%s iterations=%d published_request_id=%q revision=%d",
				entity.State, entity.Iterations, entity.PublishedRequestID, entry.Revision())
		case errors.Is(err, jetstream.ErrKeyNotFound), errors.Is(err, jetstream.ErrKeyDeleted):
			last = "no record"
		default:
			return loopRecordObservation{}, fmt.Errorf("read %s/%s: %w", agentLoopsBucket, loopID, err)
		}
		if err := waitDuration(ctx, 200*time.Millisecond); err != nil {
			return loopRecordObservation{}, err
		}
	}
	return loopRecordObservation{}, fmt.Errorf("loop %s record never matched within %v (last %s)", loopID, timeout, last)
}

// resumeConsumerIfPaused resumes only a consumer that is actually paused. A
// replacement recreates its consumers from configuration that carries no pause,
// so by the time this runs the pause may already be gone; asking the server
// first keeps the recovery path from failing on a no-op.
func resumeConsumerIfPaused(ctx context.Context, stream jetstream.Stream, name string) error {
	consumer, err := stream.Consumer(ctx, name)
	if err != nil {
		return fmt.Errorf("open consumer %s: %w", name, err)
	}
	info, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read consumer %s: %w", name, err)
	}
	if !info.Paused {
		return nil
	}
	if _, err := stream.ResumeConsumer(ctx, name); err != nil {
		return fmt.Errorf("resume consumer %s: %w", name, err)
	}
	return nil
}

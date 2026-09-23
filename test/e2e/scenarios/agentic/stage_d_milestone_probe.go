package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/test/e2e/harness/milestoneprobe"
)

// milestoneProbeFixture is one injected terminal: the identity the probe keys
// on, the subject it rides, and the exact bytes published.
type milestoneProbeFixture struct {
	behavior        string
	loopID          string
	sourceMessageID string
	subject         string
	wire            []byte
}

// newMilestoneProbeTerminal builds a terminal addressed to the milestone probe.
// An empty behavior builds an ORDINARY terminal instead — same shape, a role the
// probe does not answer to — which is how the tier observes a lane that is
// merely consuming rather than being provoked.
//
// It persists a route-LESS AGENT_LOOPS record first. agentic-dispatch reads
// terminal routing out of that bucket and answers an absent record with an
// unbounded transient retry (processor/agentic-dispatch/terminal_settlement.go),
// so a terminal without one would stay pending on the dispatch lane forever and
// break the settled-consumer assertions the tier already makes. With a record
// and no channel, dispatch settles it as route_less_settled and publishes
// nothing, leaving the milestone lanes as the only place the terminal acts.
func (s *Scenario) newMilestoneProbeTerminal(
	ctx context.Context, behavior, category string,
) (milestoneProbeFixture, error) {
	now := time.Now().UTC()
	// A canonical framework loop token (ADR-105, #1192): agentic-dispatch
	// refuses a non-canonical loop id before it reads any record.
	loopID := uuid.NewString()
	label := behavior
	if label == "" {
		label = "ordinary"
	}
	taskID := fmt.Sprintf("task-e2e-milestone-%s-%d", label, now.UnixNano())

	role := "general"
	if behavior != "" {
		role = milestoneprobe.Role(behavior)
	}
	payload, subject, state, err := milestoneTerminalPayload(category, loopID, taskID, role, now)
	if err != nil {
		return milestoneProbeFixture{}, err
	}
	loop := agentic.LoopEntity{ID: loopID, TaskID: taskID, State: state, MaxIterations: 3}
	record, err := json.Marshal(loop)
	if err != nil {
		return milestoneProbeFixture{}, fmt.Errorf("marshal milestone probe loop record: %w", err)
	}
	if err := s.nats.PutKV(ctx, "AGENT_LOOPS", loopID, record); err != nil {
		return milestoneProbeFixture{}, fmt.Errorf("persist milestone probe loop record: %w", err)
	}

	envelope := message.NewBaseMessage(payload.Schema(), payload, "e2e-milestone-probe")
	wire, err := json.Marshal(envelope)
	if err != nil {
		return milestoneProbeFixture{}, fmt.Errorf("marshal milestone probe terminal: %w", err)
	}
	// The envelope's own id IS the SourceMessageID every attempt will present
	// to the handler (internal/agentterminal/terminal.go reads base.ID()), so
	// the fixture keys its evidence on the same value the handler will see.
	return milestoneProbeFixture{
		behavior:        behavior,
		loopID:          loopID,
		sourceMessageID: envelope.ID(),
		subject:         subject,
		wire:            wire,
	}, nil
}

func milestoneTerminalPayload(
	category, loopID, taskID, role string, now time.Time,
) (message.Payload, string, agentic.LoopState, error) {
	switch category {
	case agentic.CategoryLoopCompleted:
		return &agentic.LoopCompletedEvent{
			LoopID: loopID, TaskID: taskID, Outcome: agentic.OutcomeSuccess, Role: role,
			Result: "milestone probe terminal", Model: "mock", CompletedAt: now,
		}, "agent.complete." + loopID, agentic.LoopStateComplete, nil
	case agentic.CategoryLoopFailed:
		return &agentic.LoopFailedEvent{
			LoopID: loopID, TaskID: taskID, Outcome: agentic.OutcomeFailed, Role: role,
			Reason: "milestone probe terminal", Error: "milestone probe terminal", Model: "mock", FailedAt: now,
		}, "agent.failed." + loopID, agentic.LoopStateFailed, nil
	default:
		return nil, "", "", fmt.Errorf("milestone probe cannot build category %q", category)
	}
}

// ensureMilestoneProbeStream creates (or re-reads) the probe's evidence stream.
// The duplicate window is explicit rather than left at the server default,
// because the effect's idempotency IS that window: a replay arriving outside it
// would store a second effect the handler did not commit twice, and the proof
// would report a defect that is the harness's own.
func (s *Scenario) ensureMilestoneProbeStream(ctx context.Context) (jetstream.Stream, error) {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return nil, fmt.Errorf("open JetStream for the milestone probe: %w", err)
	}
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       milestoneprobe.EvidenceStream,
		Subjects:   []string{milestoneprobe.SubjectPrefix + ">"},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		Discard:    jetstream.DiscardOld,
		MaxAge:     15 * time.Minute,
		MaxMsgs:    256,
		Duplicates: milestoneProbeDedupWindow,
	})
	if err != nil {
		return nil, fmt.Errorf("create milestone probe evidence stream: %w", err)
	}
	return stream, nil
}

func (s *Scenario) milestoneConsumer(ctx context.Context, name string) (jetstream.Consumer, error) {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return nil, fmt.Errorf("open JetStream for %s: %w", name, err)
	}
	stream, err := js.Stream(ctx, "AGENT")
	if err != nil {
		return nil, fmt.Errorf("open AGENT stream: %w", err)
	}
	consumer, err := stream.Consumer(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("open milestone consumer %s: %w", name, err)
	}
	return consumer, nil
}

// waitForProbeAttempts waits until the identity has at least want handler
// invocations recorded and returns the most recent one.
func waitForProbeAttempts(
	ctx context.Context, stream jetstream.Stream, sourceMessageID string, want uint64, timeout time.Duration,
) (milestoneprobe.Attempt, error) {
	subject := milestoneprobe.AttemptSubject(sourceMessageID)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		count, err := streamSubjectCount(ctx, stream, subject)
		if err == nil && count >= want {
			raw, getErr := stream.GetLastMsgForSubject(ctx, subject)
			if getErr != nil {
				return milestoneprobe.Attempt{}, getErr
			}
			return decodeProbeAttempt(raw.Data, sourceMessageID)
		}
		if err := waitDuration(ctx, 200*time.Millisecond); err != nil {
			return milestoneprobe.Attempt{}, err
		}
	}
	return milestoneprobe.Attempt{}, fmt.Errorf(
		"handler invocations for %s did not reach %d within %v", sourceMessageID, want, timeout)
}

func decodeProbeAttempt(data []byte, sourceMessageID string) (milestoneprobe.Attempt, error) {
	var attempt milestoneprobe.Attempt
	if err := json.Unmarshal(data, &attempt); err != nil {
		return milestoneprobe.Attempt{}, fmt.Errorf("decode milestone probe attempt: %w", err)
	}
	if err := attempt.Validate(sourceMessageID); err != nil {
		return milestoneprobe.Attempt{}, err
	}
	return attempt, nil
}

func readProbeEffect(
	ctx context.Context, stream jetstream.Stream, sourceMessageID string,
) (milestoneprobe.Effect, error) {
	raw, err := stream.GetLastMsgForSubject(ctx, milestoneprobe.EffectSubject(sourceMessageID))
	if err != nil {
		return milestoneprobe.Effect{}, fmt.Errorf("read durable effect for %s: %w", sourceMessageID, err)
	}
	var effect milestoneprobe.Effect
	if err := json.Unmarshal(raw.Data, &effect); err != nil {
		return milestoneprobe.Effect{}, fmt.Errorf("decode milestone probe effect: %w", err)
	}
	if err := effect.Validate(sourceMessageID); err != nil {
		return milestoneprobe.Effect{}, err
	}
	return effect, nil
}

// waitForProcessGone waits until the SemStreams HTTP surface stops answering,
// which is how the tier observes that the probe ended the process rather than
// merely logging that it meant to.
func (s *Scenario) waitForProcessGone(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := s.probeReadiness(ctx); err != nil {
			return nil
		}
		if err := waitDuration(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("the SemStreams process still answered within %v; the probe did not end it", timeout)
}

// startReplacement brings a self-terminated process back. It is deliberately
// NOT replaceSemStreams: there is nothing left to kill, and `compose kill` on an
// exited container fails, which would report the harness's own error as the
// proof's.
func (s *Scenario) startReplacement(ctx context.Context, controller composeProcessController) error {
	if err := controller.start(ctx); err != nil {
		return err
	}
	if err := s.obs.WaitForAllComponentsHealthy(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("replacement components did not become healthy: %w", err)
	}
	return nil
}

// milestoneHealthStatus is one named sub-status of the /health aggregate.
type milestoneHealthStatus struct {
	Component string `json:"component"`
	Healthy   bool   `json:"healthy"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

func (s *Scenario) waitForMilestoneHealth(
	ctx context.Context, healthy bool, timeout time.Duration,
) (milestoneHealthStatus, error) {
	deadline := time.Now().Add(timeout)
	var last milestoneHealthStatus
	var lastErr error
	for time.Now().Before(deadline) {
		last, lastErr = s.milestoneHealth(ctx)
		if lastErr == nil && last.Healthy == healthy {
			return last, nil
		}
		if err := waitDuration(ctx, 200*time.Millisecond); err != nil {
			return milestoneHealthStatus{}, err
		}
	}
	return milestoneHealthStatus{}, fmt.Errorf(
		"/health milestone sub-status did not reach healthy=%v within %v (last %+v, err %v)",
		healthy, timeout, last, lastErr)
}

// milestoneHealth reads the milestone service's own verdict out of the /health
// aggregate — the read site an operator uses, not the subscriber's accessor.
func (s *Scenario) milestoneHealth(ctx context.Context) (milestoneHealthStatus, error) {
	body, code, err := s.getHealthBody(ctx)
	if err != nil {
		return milestoneHealthStatus{}, err
	}
	if code != http.StatusOK && code != http.StatusServiceUnavailable {
		return milestoneHealthStatus{}, fmt.Errorf("/health status = %d", code)
	}
	var payload struct {
		SubStatuses []milestoneHealthStatus `json:"sub_statuses"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return milestoneHealthStatus{}, fmt.Errorf("decode /health: %w", err)
	}
	for _, sub := range payload.SubStatuses {
		if sub.Component == "milestone" {
			return sub, nil
		}
	}
	return milestoneHealthStatus{}, fmt.Errorf("/health carries no milestone sub-status")
}

func (s *Scenario) getHealthBody(ctx context.Context) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.HTTPURL+"/health", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build /health request: %w", err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("read /health: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only diagnostic request
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read /health body: %w", err)
	}
	return body, resp.StatusCode, nil
}

func (s *Scenario) probeReadiness(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.HTTPURL+"/readyz", nil)
	if err != nil {
		return 0, fmt.Errorf("build /readyz request: %w", err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only diagnostic request
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

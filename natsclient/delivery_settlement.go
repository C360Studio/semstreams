package natsclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// DeliveryDecision is the owner-supplied semantic outcome for one delivery.
type DeliveryDecision uint8

const (
	// DeliveryDecisionInvalid is the zero and invalid decision.
	DeliveryDecisionInvalid DeliveryDecision = iota
	// DeliveryDecisionAck declares that the owner-defined durable consequence completed.
	DeliveryDecisionAck
	// DeliveryDecisionRetry declares a repairable semantic failure.
	DeliveryDecisionRetry
	// DeliveryDecisionTerminate declares immutable poison for this delivery.
	DeliveryDecisionTerminate
	// DeliveryDecisionQuarantine declares that retry and termination are not proven safe.
	DeliveryDecisionQuarantine
)

// DeliveryAttempt is the server-observed attempt number for one delivery.
// Its zero value means that valid delivery metadata was not available.
type DeliveryAttempt struct {
	number uint64
}

// Number returns the server-observed delivery attempt number.
func (a DeliveryAttempt) Number() uint64 { return a.number }

// MetadataAvailable reports whether the server supplied a valid attempt number.
func (a DeliveryAttempt) MetadataAvailable() bool { return a.number > 0 }

// IsRedelivery reports whether this delivery follows the first attempt.
func (a DeliveryAttempt) IsRedelivery() bool { return a.number > 1 }

// DeliveryWork performs one delivery's owner-defined work with the immutable
// server-observed attempt and returns its semantic decision followed by the
// cause required by that decision. Data is read-only and invocation-scoped;
// work must not retain or mutate it.
type DeliveryWork func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error)

// DeliveryMetadataUnavailableError identifies missing or invalid server
// delivery metadata. The stable token allows callers to classify the failure
// while Unwrap preserves the transport or validation cause.
type DeliveryMetadataUnavailableError struct{ cause error }

func (e *DeliveryMetadataUnavailableError) Error() string {
	if e == nil || e.cause == nil {
		return "delivery_metadata_unavailable"
	}
	return fmt.Sprintf("delivery_metadata_unavailable: %v", e.cause)
}

func (e *DeliveryMetadataUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// InvalidDeliveryDecisionError identifies a decision/error tuple that does
// not satisfy the closed DeliveryWork contract.
type InvalidDeliveryDecisionError struct {
	decision DeliveryDecision
	cause    error
}

func (e *InvalidDeliveryDecisionError) Error() string {
	if e == nil {
		return "invalid delivery decision"
	}
	if e.cause != nil {
		return fmt.Sprintf("invalid delivery decision %d: %v", e.decision, e.cause)
	}
	return fmt.Sprintf("invalid delivery decision %d", e.decision)
}

func (e *InvalidDeliveryDecisionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// DeliveryWorkPanicError identifies a panic recovered from DeliveryWork. The
// panic is converted to an error so callers can observe the quarantined cause.
type DeliveryWorkPanicError struct{ cause error }

func (e *DeliveryWorkPanicError) Error() string {
	if e == nil || e.cause == nil {
		return "delivery work panicked"
	}
	return fmt.Sprintf("delivery work panicked: %v", e.cause)
}

func (e *DeliveryWorkPanicError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type deliveryRetryMode uint8

const (
	deliveryRetryInvalid deliveryRetryMode = iota
	deliveryRetryImmediate
	deliveryRetryDelayed
)

// DeliveryRetryPolicy is the opaque explicit-Nak policy for semantic Retry.
// It is independent of consumer AckWait and BackOff.
type DeliveryRetryPolicy struct {
	mode  deliveryRetryMode
	delay time.Duration
}

// ImmediateDeliveryRetry selects plain Nak for a semantic Retry decision.
func ImmediateDeliveryRetry() DeliveryRetryPolicy {
	return DeliveryRetryPolicy{mode: deliveryRetryImmediate}
}

// DelayedDeliveryRetry selects NakWithDelay for a semantic Retry decision.
func DelayedDeliveryRetry(delay time.Duration) (DeliveryRetryPolicy, error) {
	if delay <= 0 {
		return DeliveryRetryPolicy{}, fmt.Errorf("delivery retry delay must be positive, got %s", delay)
	}
	return DeliveryRetryPolicy{mode: deliveryRetryDelayed, delay: delay}, nil
}

func (p DeliveryRetryPolicy) valid() bool {
	switch p.mode {
	case deliveryRetryImmediate:
		return p.delay == 0
	case deliveryRetryDelayed:
		return p.delay > 0
	default:
		return false
	}
}

// HeartbeatDeliveryPolicy is an immutable, setup-validated policy for one
// typed heartbeat delivery. It retains no context or lifecycle authority.
type HeartbeatDeliveryPolicy struct {
	heartbeat time.Duration
	retry     DeliveryRetryPolicy
	work      DeliveryWork
	ackWait   time.Duration
	backOff   []time.Duration
}

// ValidateHeartbeatDeliveryPolicy validates one delivery policy before the
// caller acquires its consumer. cfg must be the same value used for acquisition.
func ValidateHeartbeatDeliveryPolicy(
	ctx context.Context,
	cfg StreamConsumerConfig,
	heartbeat time.Duration,
	retry DeliveryRetryPolicy,
	work DeliveryWork,
) (HeartbeatDeliveryPolicy, error) {
	if ctx == nil {
		return HeartbeatDeliveryPolicy{}, fmt.Errorf("delivery policy context is required")
	}
	if err := ctx.Err(); err != nil {
		return HeartbeatDeliveryPolicy{}, fmt.Errorf("delivery policy context already ended: %w", err)
	}
	if work == nil {
		return HeartbeatDeliveryPolicy{}, fmt.Errorf("delivery work is required")
	}
	if !retry.valid() {
		return HeartbeatDeliveryPolicy{}, fmt.Errorf("delivery retry policy is invalid")
	}
	effective, err := effectiveDeliveryAckWait(cfg)
	if err != nil {
		return HeartbeatDeliveryPolicy{}, err
	}
	if heartbeat <= 0 {
		return HeartbeatDeliveryPolicy{}, fmt.Errorf("heartbeat interval must be positive, got %s", heartbeat)
	}
	ceiling := effective / 2
	if ceiling <= 0 || heartbeat > ceiling {
		return HeartbeatDeliveryPolicy{}, fmt.Errorf(
			"heartbeat interval %s exceeds computed ceiling %s for effective acknowledgement wait %s",
			heartbeat, ceiling, effective)
	}
	backOff := append([]time.Duration(nil), cfg.BackOff...)
	return HeartbeatDeliveryPolicy{
		heartbeat: heartbeat,
		retry:     retry,
		work:      work,
		ackWait:   cfg.AckWait,
		backOff:   backOff,
	}, nil
}

func effectiveDeliveryAckWait(cfg StreamConsumerConfig) (time.Duration, error) {
	if cfg.AckWait < 0 {
		return 0, fmt.Errorf("ack wait must not be negative, got %s", cfg.AckWait)
	}
	if len(cfg.BackOff) == 0 {
		if cfg.AckWait > 0 {
			return cfg.AckWait, nil
		}
		return defaultConsumerAckWait, nil
	}
	var shortest time.Duration
	for index, interval := range cfg.BackOff {
		if interval <= 0 {
			return 0, fmt.Errorf("back_off[%d] must be positive, got %s", index, interval)
		}
		if shortest == 0 || interval < shortest {
			shortest = interval
		}
	}
	return shortest, nil
}

// DeliveryResult is the immutable semantic and local transport observation
// produced by ConsumeDeliveryWithHeartbeat.
type DeliveryResult struct {
	decision        DeliveryDecision
	cause           error
	controlErr      error
	settlementErr   error
	settlementTried bool
	quarantined     bool
	ownerStopNeeded bool
}

// Decision returns the exact decision requested by joined work.
func (r DeliveryResult) Decision() DeliveryDecision { return r.decision }

// Cause returns the semantic or decision-validation cause.
func (r DeliveryResult) Cause() error { return r.cause }

// ControlError returns heartbeat-control loss observed while work was pending.
func (r DeliveryResult) ControlError() error { return r.controlErr }

// SettlementError returns the local terminal-method error, if any.
func (r DeliveryResult) SettlementError() error { return r.settlementErr }

// SettlementAttempted reports whether one local terminal method was called.
func (r DeliveryResult) SettlementAttempted() bool { return r.settlementTried }

// SettlementMethodSucceeded reports local method return success only.
func (r DeliveryResult) SettlementMethodSucceeded() bool {
	return r.settlementTried && r.settlementErr == nil
}

// SettlementMethodFailed reports local method return failure only.
func (r DeliveryResult) SettlementMethodFailed() bool {
	return r.settlementTried && r.settlementErr != nil
}

// ServerConfirmed reports server-confirmed settlement. Plain terminal methods
// used by this contract never provide that confirmation.
func (r DeliveryResult) ServerConfirmed() bool { return false }

// Quarantined reports that no terminal method was safe to attempt.
func (r DeliveryResult) Quarantined() bool { return r.quarantined }

// OwnerStopRequired reports that the exact delivery owner must stop its lane.
func (r DeliveryResult) OwnerStopRequired() bool { return r.ownerStopNeeded }

// Err aggregates the semantic, heartbeat-control, and local settlement
// evidence. Only a clean local Ack returns nil.
func (r DeliveryResult) Err() error {
	if r.decision == DeliveryDecisionAck && r.cause == nil && r.controlErr == nil &&
		r.settlementTried && r.settlementErr == nil && !r.quarantined && !r.ownerStopNeeded {
		return nil
	}
	err := errors.Join(r.cause, r.controlErr, r.settlementErr)
	if err != nil {
		return err
	}
	return fmt.Errorf("delivery result is incomplete for decision %d", r.decision)
}

type deliveryWorkResult struct {
	decision DeliveryDecision
	cause    error
}

// ConsumeDeliveryWithHeartbeat runs setup-validated work, renews the delivery
// lease, joins work on cancellation or control loss, and attempts at most one
// local terminal method. It owns no consumer lifecycle or restart state.
func ConsumeDeliveryWithHeartbeat(
	ctx context.Context,
	msg jetstream.Msg,
	policy HeartbeatDeliveryPolicy,
) DeliveryResult {
	if ctx == nil || msg == nil || policy.heartbeat <= 0 || policy.work == nil || !policy.retry.valid() {
		cause := &InvalidDeliveryDecisionError{decision: DeliveryDecisionInvalid}
		return DeliveryResult{
			decision: DeliveryDecisionInvalid, cause: cause, quarantined: true, ownerStopNeeded: true,
		}
	}
	metadata, err := msg.Metadata()
	if err != nil {
		return unavailableDeliveryMetadata(err)
	}
	if metadata == nil {
		return unavailableDeliveryMetadata(errors.New("message metadata is nil"))
	}
	if metadata.NumDelivered == 0 {
		return unavailableDeliveryMetadata(errors.New("message delivery attempt is zero"))
	}
	attempt := DeliveryAttempt{number: metadata.NumDelivered}

	workCtx, workCancel := context.WithCancel(ctx)
	defer workCancel()
	data := msg.Data()
	done := make(chan deliveryWorkResult, 1)
	go func() {
		result := deliveryWorkResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				panicCause, ok := recovered.(error)
				if !ok {
					panicCause = fmt.Errorf("%v", recovered)
				}
				result = deliveryWorkResult{
					decision: DeliveryDecisionQuarantine,
					cause:    &DeliveryWorkPanicError{cause: panicCause},
				}
			}
			done <- result
		}()
		result.decision, result.cause = policy.work(workCtx, attempt, data)
	}()

	ticker := time.NewTicker(policy.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := msg.InProgress(); err != nil {
				workCancel()
				joined := <-done
				result := interpretDeliveryWork(joined)
				result.controlErr = errors.Join(ErrHeartbeatFailed, fmt.Errorf("failed to send InProgress: %w", err))
				result.ownerStopNeeded = true
				return result
			}
		case joined := <-done:
			return settleDeliveryDecision(msg, policy.retry, interpretDeliveryWork(joined))
		case <-ctx.Done():
			workCancel()
			joined := <-done
			return settleDeliveryDecision(msg, policy.retry, interpretDeliveryWork(joined))
		}
	}
}

func unavailableDeliveryMetadata(cause error) DeliveryResult {
	return DeliveryResult{
		decision:        DeliveryDecisionQuarantine,
		cause:           &DeliveryMetadataUnavailableError{cause: cause},
		quarantined:     true,
		ownerStopNeeded: true,
	}
}

func interpretDeliveryWork(work deliveryWorkResult) DeliveryResult {
	valid := false
	switch work.decision {
	case DeliveryDecisionAck:
		valid = work.cause == nil
	case DeliveryDecisionRetry, DeliveryDecisionTerminate, DeliveryDecisionQuarantine:
		valid = work.cause != nil
	}
	if !valid {
		return DeliveryResult{
			decision:        work.decision,
			cause:           &InvalidDeliveryDecisionError{decision: work.decision, cause: work.cause},
			quarantined:     true,
			ownerStopNeeded: true,
		}
	}
	return DeliveryResult{
		decision:        work.decision,
		cause:           work.cause,
		quarantined:     work.decision == DeliveryDecisionQuarantine,
		ownerStopNeeded: work.decision == DeliveryDecisionQuarantine,
	}
}

func settleDeliveryDecision(msg jetstream.Msg, retry DeliveryRetryPolicy, result DeliveryResult) DeliveryResult {
	if result.quarantined || result.ownerStopNeeded {
		return result
	}
	var method terminalMethod
	var delay time.Duration
	switch result.decision {
	case DeliveryDecisionAck:
		method = terminalMethodAck
	case DeliveryDecisionRetry:
		if retry.mode == deliveryRetryImmediate {
			method = terminalMethodNak
		} else {
			method = terminalMethodNakWithDelay
			delay = retry.delay
		}
	case DeliveryDecisionTerminate:
		method = terminalMethodTerm
	default:
		return result
	}
	result.settlementTried = true
	result.settlementErr = executeTerminalMethod(msg, method, delay)
	return result
}

type terminalMethod uint8

const (
	terminalMethodAck terminalMethod = iota + 1
	terminalMethodNak
	terminalMethodNakWithDelay
	terminalMethodTerm
)

func executeTerminalMethod(msg jetstream.Msg, method terminalMethod, delay time.Duration) error {
	switch method {
	case terminalMethodAck:
		return msg.Ack()
	case terminalMethodNak:
		return msg.Nak()
	case terminalMethodNakWithDelay:
		return msg.NakWithDelay(delay)
	case terminalMethodTerm:
		return msg.Term()
	default:
		return fmt.Errorf("invalid terminal method %d", method)
	}
}

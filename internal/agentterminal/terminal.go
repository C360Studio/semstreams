// Package agentterminal is the repository-internal interpretation boundary for
// the three agent loop terminal payloads. It deliberately exposes no public
// framework API: Go's internal import rule limits consumers to this repository.
package agentterminal

import (
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
)

// Class is the closed semantic terminal set.
type Class uint8

const (
	// ClassSucceeded represents loop_completed + success.
	ClassSucceeded Class = iota + 1
	// ClassFailed represents loop_failed + failed.
	ClassFailed
	// ClassCancelled represents loop_cancelled + cancelled.
	ClassCancelled
)

// Reason is a bounded rejection class suitable for logs and metric labels.
type Reason string

const (
	// ReasonEnvelope covers registry decode, type, and metadata rejection.
	ReasonEnvelope Reason = "envelope_or_type"
	// ReasonPayload covers nil, nonterminal, and invalid concrete payloads.
	ReasonPayload Reason = "payload_validation"
	// ReasonTimestamp covers a zero applicable terminal timestamp.
	ReasonTimestamp Reason = "zero_terminal_timestamp"
	// ReasonIdentity covers a missing source message identity.
	ReasonIdentity Reason = "source_identity"
	// ReasonCollision covers category/outcome identity collisions.
	ReasonCollision Reason = "identity_category_outcome_collision"
)

type decodeError struct {
	reason Reason
	err    error
}

func (e *decodeError) Error() string { return e.err.Error() }
func (e *decodeError) Unwrap() error { return e.err }

// ErrorReason returns the bounded reason carried by a Decode error.
func ErrorReason(err error) Reason {
	var target *decodeError
	if errors.As(err, &target) {
		return target.reason
	}
	return ReasonEnvelope
}

func reject(reason Reason, format string, args ...any) error {
	return &decodeError{reason: reason, err: fmt.Errorf(format, args...)}
}

// Event is the normalized internal projection shared by dispatch, AgentRun,
// and OTel. The source payload remains authoritative; this is not a wire type.
type Event struct {
	SourceMessageID string
	Category        string
	Outcome         string
	Class           Class
	State           string
	LoopID          string
	TaskID          string
	RunID           string
	RunEntityID     string
	Role            string
	Result          string
	Error           string
	Reason          string
	UserID          string
	ChannelType     string
	ChannelID       string
	Prompt          string
	Model           string
	Iterations      int
	TokensIn        int
	TokensOut       int
	WorkflowSlug    string
	WorkflowStep    string
	CancelledBy     string
	TerminalAt      time.Time
	Metadata        map[string]any
}

// Decode decodes only through the registry-bound production decoder and then
// closes every structural hole before returning a terminal projection.
func Decode(decoder *message.Decoder, data []byte) (Event, error) {
	base, err := decoder.Decode(data)
	if err != nil {
		return Event{}, reject(ReasonEnvelope, "decode terminal envelope: %w", err)
	}
	if base.ID() == "" {
		return Event{}, reject(ReasonIdentity, "terminal source message id is empty")
	}
	if !base.Type().IsValid() {
		return Event{}, reject(ReasonEnvelope, "invalid terminal message type %q", base.Type().String())
	}
	if base.Meta() == nil || base.Meta().CreatedAt().IsZero() || base.Meta().ReceivedAt().IsZero() || base.Meta().Source() == "" {
		return Event{}, reject(ReasonEnvelope, "invalid terminal metadata")
	}
	if base.Payload() == nil {
		return Event{}, reject(ReasonPayload, "terminal payload is nil")
	}
	if err := base.Payload().Validate(); err != nil {
		return Event{}, reject(ReasonPayload, "validate terminal payload: %w", err)
	}

	event := Event{SourceMessageID: base.ID(), Category: base.Type().Category}
	switch payload := base.Payload().(type) {
	case *agentic.LoopCompletedEvent:
		if base.Type().Domain != agentic.Domain || base.Type().Version != agentic.SchemaVersion ||
			base.Type().Category != agentic.CategoryLoopCompleted || payload.Outcome != agentic.OutcomeSuccess {
			return Event{}, reject(ReasonCollision, "invalid completed category/outcome pair %s/%s", base.Type().Category, payload.Outcome)
		}
		if payload.CompletedAt.IsZero() {
			return Event{}, reject(ReasonTimestamp, "completed terminal timestamp is zero")
		}
		event.Class = ClassSucceeded
		event.Outcome = payload.Outcome
		event.State = agentic.LoopStateComplete.String()
		event.LoopID, event.TaskID = payload.LoopID, payload.TaskID
		event.RunID, event.RunEntityID, event.Role = payload.RunID, payload.RunEntityID, payload.Role
		event.Result, event.UserID = payload.Result, payload.UserID
		event.ChannelType, event.ChannelID = payload.ChannelType, payload.ChannelID
		event.Prompt, event.Model = payload.Prompt, payload.Model
		event.Iterations, event.TokensIn, event.TokensOut = payload.Iterations, payload.TokensIn, payload.TokensOut
		event.WorkflowSlug, event.WorkflowStep = payload.WorkflowSlug, payload.WorkflowStep
		event.TerminalAt, event.Metadata = payload.CompletedAt, payload.Metadata

	case *agentic.LoopFailedEvent:
		if base.Type().Domain != agentic.Domain || base.Type().Version != agentic.SchemaVersion ||
			base.Type().Category != agentic.CategoryLoopFailed || payload.Outcome != agentic.OutcomeFailed {
			return Event{}, reject(ReasonCollision, "invalid failed category/outcome pair %s/%s", base.Type().Category, payload.Outcome)
		}
		if payload.FailedAt.IsZero() {
			return Event{}, reject(ReasonTimestamp, "failed terminal timestamp is zero")
		}
		event.Class = ClassFailed
		event.Outcome = payload.Outcome
		event.State = agentic.LoopStateFailed.String()
		event.LoopID, event.TaskID = payload.LoopID, payload.TaskID
		event.RunID, event.RunEntityID, event.Role = payload.RunID, payload.RunEntityID, payload.Role
		event.Error, event.Reason, event.UserID = payload.Error, payload.Reason, payload.UserID
		event.ChannelType, event.ChannelID = payload.ChannelType, payload.ChannelID
		event.Prompt, event.Model = payload.Prompt, payload.Model
		event.Iterations, event.TokensIn, event.TokensOut = payload.Iterations, payload.TokensIn, payload.TokensOut
		event.WorkflowSlug, event.WorkflowStep = payload.WorkflowSlug, payload.WorkflowStep
		event.TerminalAt, event.Metadata = payload.FailedAt, payload.Metadata

	case *agentic.LoopCancelledEvent:
		if base.Type().Domain != agentic.Domain || base.Type().Version != agentic.SchemaVersion ||
			base.Type().Category != agentic.CategoryLoopCancelled || payload.Outcome != agentic.OutcomeCancelled {
			return Event{}, reject(ReasonCollision, "invalid cancelled category/outcome pair %s/%s", base.Type().Category, payload.Outcome)
		}
		if payload.CancelledAt.IsZero() {
			return Event{}, reject(ReasonTimestamp, "cancelled terminal timestamp is zero")
		}
		event.Class = ClassCancelled
		event.Outcome = payload.Outcome
		event.State = agentic.LoopStateCancelled.String()
		event.LoopID, event.TaskID = payload.LoopID, payload.TaskID
		event.RunID, event.RunEntityID = payload.RunID, payload.RunEntityID
		event.CancelledBy = payload.CancelledBy
		event.WorkflowSlug, event.WorkflowStep = payload.WorkflowSlug, payload.WorkflowStep
		event.TerminalAt, event.Metadata = payload.CancelledAt, payload.Metadata

	default:
		return Event{}, reject(ReasonPayload, "payload %T is not a loop terminal event", base.Payload())
	}

	return event, nil
}

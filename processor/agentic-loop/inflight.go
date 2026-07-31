// Package agenticloop — task in-flight visibility (gh#733).
package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/pkg/errs"
)

// InFlightQuerySubject is the NATS request/reply subject answering "does this
// deployment currently have outstanding agentic-loop work for this task subject".
//
// The question is served on the wire rather than as an in-process call on purpose.
// A caller needs the ANSWER, and every in-process shape would have made it supply
// something it cannot legitimately know: a package-level function needs the stream
// name and ConsumerNameSuffix (relocating the consumer-name reconstruction gh#733
// exists to delete, into a parameter list), and a method needs a component handle a
// boot-time recovery pass may not hold — and may not have in-process at all.
// Components execute and do not know their caller; this keeps that true.
const InFlightQuerySubject = "agentic.query.inflight"

// Bounded reasons an in-flight answer is UNKNOWN.
const (
	// InFlightUnknownNoConsumer — this deployment binds no agentic-loop consumer
	// for the requested subject. NOT "nothing is in flight": there may be work
	// on the stream that nobody here is answering for.
	InFlightUnknownNoConsumer = "inflight_unknown_no_consumer"
	// InFlightUnknownUnreadable — a consumer is bound but its outstanding-work
	// bookkeeping could not be read on this attempt.
	InFlightUnknownUnreadable = "inflight_unknown_unreadable"
)

// Sentinels for the unknown reasons, matchable with errors.Is (ADR-060).
//
// These exist so a caller branches on identity, never on message text. They also
// survive the request/reply wire: natsclient's ClassifyReply reconstructs the Code
// on the far side, so errors.Is resolves for an out-of-process caller exactly as it
// does in-process — which matters because out-of-process is the caller this query
// was shaped for.
var (
	// ErrInFlightUnknownNoConsumer — see InFlightUnknownNoConsumer.
	ErrInFlightUnknownNoConsumer = &errs.ClassifiedError{
		Class:   errs.ErrorTransient,
		Code:    InFlightUnknownNoConsumer,
		Message: InFlightUnknownNoConsumer,
	}
	// ErrInFlightUnknownUnreadable — see InFlightUnknownUnreadable.
	ErrInFlightUnknownUnreadable = &errs.ClassifiedError{
		Class:   errs.ErrorTransient,
		Code:    InFlightUnknownUnreadable,
		Message: InFlightUnknownUnreadable,
	}
)

// InFlightRequest asks about one task subject.
type InFlightRequest struct {
	Subject string `json:"subject"`
}

// InFlightResponse is returned ONLY when the answer is known.
//
// There is deliberately no "unknown" field and no zero-valued fallback: an
// unknown answer is an error, never a payload, so a caller cannot accidentally
// read absence of a measurement as a measurement of absence. See errUnknownInFlight.
type InFlightResponse struct {
	Subject string `json:"subject"`
	// Outstanding is NumPending + NumAckPending for the bound consumer — work
	// delivered-but-unacked plus work not yet delivered.
	Outstanding uint64 `json:"outstanding"`
	// InFlight is Outstanding > 0, carried explicitly so the common caller
	// question does not require every caller to re-derive the same comparison.
	InFlight bool `json:"inFlight"`
}

// errUnknownInFlight is the SINGLE construction site for "the in-flight state was
// not observed".
//
// One rule, three instances — no bound consumer, unreadable consumer state, and
// (client-side, via natsclient.IsNoResponders) a component that is not answering.
// They are one invariant, not three coincidences: AN ABSENT MEASUREMENT MUST NEVER
// RENDER AS A MEASUREMENT OF ABSENCE. The same rule already shaped
// natsclient.OutstandingWork's error-rather-than-(0, nil) return for an unbound
// consumer, and it is the reason this function returns an error instead of an
// InFlightResponse with Outstanding: 0.
//
// Funnelling every unknown path through one constructor is what keeps a fourth
// case from being added as a fourth coincidence that forgets the rule.
func errUnknownInFlight(code, detail string, cause error) error {
	msg := fmt.Errorf("in-flight state unknown for %s: NOT a report of zero outstanding work", detail)
	if cause != nil {
		msg = fmt.Errorf("in-flight state unknown for %s: NOT a report of zero outstanding work: %w", detail, cause)
	}
	// Transient, not Invalid: the caller's correct response is to defer and retry,
	// not to conclude anything about the work.
	return errs.ClassifiedCode(errs.ErrorTransient, code, msg)
}

// consumerForSubject returns the stream and consumer this component actually BOUND
// for subject.
//
// It reads the recorded binding rather than re-deriving the name. Re-deriving via a
// shared helper would keep two derivations in step; reading what was bound removes
// the second derivation entirely, which is strictly stronger — there is no
// expression left that could drift. ConsumerNameSuffix is therefore accounted for
// by construction rather than by remembering to apply it.
func (c *Component) consumerForSubject(subject string) (streamName, consumerName string, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, info := range c.consumerInfos {
		if info.subject == subject {
			return info.streamName, info.consumerName, true
		}
	}
	return "", "", false
}

// outstandingForSubject answers the in-flight question for one subject.
//
// Sourced from natsclient.OutstandingWork (NumPending + NumAckPending) and NEVER
// from AckFloor. AckFloor was measured against both deployed NATS versions and
// misreports in both directions — it stalls behind a MaxDeliver-exhausted message
// while idle, then leaps past that never-applied message on the next unrelated ack,
// so it never means "everything at or below this is durably handled" (ADR-088).
//
// It is also not sourced from AGENT_LOOPS state=running: only a handler transitions
// a loop out of running, so a crashed process leaves a stale running entry
// indistinguishable from live work.
func (c *Component) outstandingForSubject(ctx context.Context, subject string) (uint64, error) {
	streamName, consumerName, ok := c.consumerForSubject(subject)
	if !ok {
		return 0, errUnknownInFlight(InFlightUnknownNoConsumer,
			fmt.Sprintf("subject %q (this deployment binds no agentic-loop consumer for it)", subject), nil)
	}

	outstanding, err := c.natsClient.OutstandingWork(ctx, streamName, consumerName)
	if err != nil {
		return 0, errUnknownInFlight(InFlightUnknownUnreadable,
			fmt.Sprintf("subject %q", subject), err)
	}
	return outstanding, nil
}

// handleInFlightQuery serves InFlightQuerySubject.
//
// A caller that receives no response at all — natsclient.IsNoResponders — is in the
// third unknown case: this component is not running, which emphatically does not
// mean the work is gone. Messages may be sitting on the stream with nobody to
// answer for them, which is precisely when a recovery pass is most likely to be
// running and most able to do harm. That case cannot be signalled from here, by
// definition, so it is the caller's half of the same rule; see the adopter note.
func (c *Component) handleInFlightQuery(ctx context.Context, data []byte) ([]byte, error) {
	var req InFlightRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("subject required: %w", err))
	}
	if req.Subject == "" {
		return nil, errs.Classified(errs.ErrorInvalid, errors.New("subject required"))
	}

	outstanding, err := c.outstandingForSubject(ctx, req.Subject)
	if err != nil {
		return nil, err
	}

	return json.Marshal(InFlightResponse{
		Subject:     req.Subject,
		Outstanding: outstanding,
		InFlight:    outstanding > 0,
	})
}

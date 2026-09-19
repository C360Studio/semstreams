package agenticdispatch

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

// commandEffect is one delivery's record of what its command handler actually
// did to the world. It exists because the settlement decision in handleCommand
// is about THIS delivery's effect, not about the shape of the request that
// produced it: a bare /cancel that published a signal cannot be replayed,
// while /help, /loops, a bare /status and the three arms of bare /cancel that
// publish nothing (no active loop, gate refusal, already settled) can — they
// resolved a target and then did nothing with it.
//
// The fact is recorded AT the publish site (commands.go:193) so it cannot drift
// from what happened. Inferring it from the command name would be a second
// spelling of "which commands publish", and inferring it from response text
// would be a parser over prose.
//
// It rides the context rather than the CommandHandler signature because
// processor/agentic-dispatch is Tier 1 (release/tier1-packages.txt:74) and
// CommandHandler is exported. What binds is ADR-106 RC-4: the incompatible Tier 1
// change count must descend to zero and stay there 30 days, and a return-value
// change adds one to it. CI would not have caught it — the apidiff job runs
// continue-on-error with API_COMPAT_MODE=report (.github/workflows/ci.yml:236-238,
// taskfiles/apicompat.yml:9-12), so the count is a governed number, not a gate.
// The break would be real but narrower than "every adopter": a handler literal
// passed to CommandRegistry().Register would stop compiling, while the
// CommandExecutor path adapts inside this package (component.go:1398-1402). None
// of it buys anything, because the framework's own publish site already knows the
// fact. The value is per delivery, never shared.
type commandEffect struct {
	// Two facts, not one. signalAttempted says the bytes were handed to the
	// broker; signalPublished says the broker answered with a PubAck. Between
	// them lies the case a single bool cannot express — the publish that
	// stored and whose acknowledgement was lost — and that case settles like a
	// publication, not like a delivery that did nothing.
	signalAttempted atomic.Bool
	signalPublished atomic.Bool
}

// commandEffectKey is a private type, so no other package can collide with or
// read this value.
type commandEffectKey struct{}

// withCommandEffect attaches a fresh recorder for one command delivery and
// returns it alongside the derived context.
func withCommandEffect(ctx context.Context) (context.Context, *commandEffect) {
	effect := &commandEffect{}
	return context.WithValue(ctx, commandEffectKey{}, effect), effect
}

// noteSignalAttempt records that this delivery is about to hand a signal to
// the broker. It is recorded BEFORE the publish because it is the only way to
// tell a publish that never happened from one whose outcome is unknown: the
// error a failed publish returns describes the client's experience, not the
// server's store.
func noteSignalAttempt(ctx context.Context) {
	if effect, ok := ctx.Value(commandEffectKey{}).(*commandEffect); ok {
		effect.signalAttempted.Store(true)
	}
}

// noteSignalPublished records that this delivery put a signal on the wire. A
// call with no recorder attached — the HTTP sync lane, which settles no
// delivery — is a no-op rather than an error.
func noteSignalPublished(ctx context.Context) {
	if effect, ok := ctx.Value(commandEffectKey{}).(*commandEffect); ok {
		effect.signalPublished.Store(true)
	}
}

// signalled reports whether the handler published a signal on this delivery. A
// nil recorder answers false, so an unattached path cannot quarantine.
func (e *commandEffect) signalled() bool {
	return e != nil && e.signalPublished.Load()
}

// attemptedUnconfirmed reports that a signal left this delivery and no PubAck
// came back. The world may or may not hold that signal; what is certain is
// that this delivery cannot prove it does not.
func (e *commandEffect) attemptedUnconfirmed() bool {
	return e != nil && e.signalAttempted.Load() && !e.signalPublished.Load()
}

// publishDefinitelyRejected reports whether err PROVES the broker stored
// nothing, so a redelivery cannot duplicate an effect that never happened.
//
// It is a whitelist and it fails closed: an error that is not on it is
// ambiguous, whatever it reads like. Every entry is a refusal the client makes
// before the bytes leave the process, at the versions this module pins:
//
//   - natsclient.ErrCircuitOpen (natsclient/client.go:972-974) and
//     natsclient.ErrNotConnected (:976-978) — the two gates that return before
//     js.PublishMsg is reached at all.
//   - the sentinels nats.Conn.publish returns before its first write
//     (nats.go v1.52.0, nats.go:4424): ErrInvalidConnection (:4426),
//     ErrBadSubject (:4434, :4438), ErrHeadersNotSupported (:4445),
//     ErrConnectionDraining (:4455), ErrMaxPayload (:4463) and
//     ErrReconnectBufExceeded (:4470). Each is the client refusing its own
//     caller with no bytes written.
//
// Membership is a property of the SENTINEL, not of one site that returns it,
// because errors.Is sees only the sentinel. nats.ErrConnectionClosed fails
// that test and is deliberately absent: nats.Conn.publish does return it
// pre-write (nats.go:4450), but RequestMsgWithContext ALSO returns it
// post-write (context.go:70) when the reply channel is closed —
// clearPendingRequestCalls (nats.go:5925-5932) closes every pending reply on
// close and on ForceReconnect (:2485) — and the sync publish this component
// uses goes js.PublishMsg → RequestMsgWithContext (UseOldRequestStyle is never
// set here). A connection that dropped after the bytes went out is the exact
// case that must stay ambiguous.
//
// Three errors that read like refusals are deliberately NOT here.
// jetstream.ErrNoStreamResponse (jetstream/publish.go:244-246) means no
// responder answered, which cannot tell a stream that never received the
// message from one whose reply was lost; a *jetstream.APIError
// (jetstream/publish.go:255-257) is a server answer whose contract says
// nothing about whether the store ran. Either could be proven definite by
// reading the server, and until that proof exists they settle as ambiguous —
// the cost of being wrong the other way is a live loop cancelled in a user's
// name without ever having been named. The third is genuinely pre-write and
// still omitted: m.JetStream()'s refusal (natsclient/client.go:980-983)
// returns before js.PublishMsg, but it is a fmt.Errorf built at client.go:885
// with no sentinel behind it, so errors.Is cannot recognise it and the only
// alternative is matching its text. Omitting it over-quarantines, which is the
// direction this whitelist fails on purpose.
func publishDefinitelyRejected(err error) bool {
	for _, refusal := range []error{
		natsclient.ErrCircuitOpen,
		natsclient.ErrNotConnected,
		nats.ErrInvalidConnection,
		nats.ErrBadSubject,
		nats.ErrHeadersNotSupported,
		nats.ErrConnectionDraining,
		nats.ErrMaxPayload,
		nats.ErrReconnectBufExceeded,
	} {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}

// unconfirmedSignalIsFatal returns the classification for a command handler
// that failed with a signal publish it cannot account for, or nil when the
// delivery settles as it always did.
//
// It is the second door into the hazard the two-conjunct rule at the response
// site closes. There the signal is known published; here it is only known
// ATTEMPTED — and a Retry on an attempt whose outcome is unknown replays a
// bare /cancel whose target this component chooses afresh. If the first
// attempt did store, loop A is already cancelling, so the redelivery's
// GetActiveLoop falls through A to the user's next live loop and cancels B, a
// loop the message never named. The same defect as a published-then-unanswered
// response, reached through an error instead of through a response.
//
// The three conjuncts are all necessary. Without the attempt, nothing was put
// on the wire at all. Without a tracker-resolved target, the redelivery
// re-reads the loop the message names and cannot drift onto another. And with
// a PROVEN refusal the world is unchanged, so the ordinary Retry is not just
// safe but correct — the user has been told nothing yet, and a broker that was
// merely disconnected will answer the redelivery.
func unconfirmedSignalIsFatal(err error, effect *commandEffect, targetFromTracker bool, name, loopID string) error {
	if !effect.attemptedUnconfirmed() || !targetFromTracker || publishDefinitelyRejected(err) {
		return nil
	}
	return errs.WrapFatal(err, "Component", "handleCommand", fmt.Sprintf(
		"command %s attempted a signal to loop %s, which this message does not name, and the broker did not answer",
		name, loopID))
}

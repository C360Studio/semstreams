package agenticdispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/looptoken"
	"github.com/c360studio/semstreams/pkg/errs"
)

// The classified Code a refusal carries. These are the machine-readable half of
// the refusal vocabulary: a seam branches on them to choose its answer (an HTTP
// status, a typed channel response). They are deliberately NOT the metric reason
// labels below — an operator-facing series and a caller-facing discriminator are
// two audiences, and collapsing them means one cannot change without the other,
// exactly as processor/graph-ingest/authority_gate.go:11-16 argues for the
// authority rejection.
const (
	// codeLoopTokenInvalid: the token is not in canonical loop-token form.
	codeLoopTokenInvalid = "loop_token_invalid"
	// codeLoopNotFound: no tracker entry and no AGENT_LOOPS record.
	codeLoopNotFound = "loop_not_found"
	// codeLoopUnreadable: the durable record could not be read, and the tracker
	// does not hold the loop either. The request is answerable later.
	codeLoopUnreadable = "loop_unreadable"
	// codeLoopOwnerConflict: the two sources disagree about a route field.
	codeLoopOwnerConflict = "loop_owner_conflict"
	// codeLoopTerminal: the loop has settled; it cannot be continued.
	codeLoopTerminal = "loop_terminal"
	// codeLoopNotOwned: the requester is not the loop's recorded owner and holds
	// no configured override for the operation.
	codeLoopNotOwned = "loop_not_owned"
	// codeLoopNotPermitted: the requester lacks the permission the operation
	// declares, or named an operation this gate does not know.
	codeLoopNotPermitted = "loop_not_permitted"
)

// The Detail keys a refusal carries. The spec requires the seam and the failing
// field; operation is added because "not owned" is meaningless without knowing
// what was asked.
const (
	detailSeam      = "seam"
	detailField     = "field"
	detailOperation = "operation"
)

// The metric reason labels for an admission refusal. Each reads as the concern
// the check owns followed by its case — the shape mutation_rejections uses. The
// mapping from a refusal to one of these has exactly one home,
// loopAdmissionMetricReason, so two seams cannot disagree about what the same
// refusal is called.
const (
	reasonFormMalformed         = "form_malformed"
	reasonExistenceAbsent       = "existence_absent"
	reasonExistenceUnreadable   = "existence_unreadable"
	reasonExistenceConflict     = "existence_conflict"
	reasonStateTerminal         = "state_terminal"
	reasonOwnershipNotOwner     = "ownership_not_owner"
	reasonOwnershipNotPermitted = "ownership_not_permitted"
)

// loopAdmissionRefusalLogMessage is the single WARN a refused request produces
// on any seam. Named so the test pinning the requirement's refusal log matches
// the production string instead of a copy that can drift away from it.
const loopAdmissionRefusalLogMessage = "agentic-dispatch: loop request refused"

// loopDurableReadToleratedLogMessage names the one place this gate deliberately
// continues past a failure: the durable record could not be read, but the
// tracker already holds the loop, so the owner is known and the read adds
// nothing. Declared rather than silent, per the degradation the design states.
// It does not move the refusal series — nothing was refused — and there is no
// second series because the outcome is identical to a clean admit.
const loopDurableReadToleratedLogMessage = "agentic-dispatch: loop durable read failed, admitting from tracker"

// The operations a request naming a loop can ask for. The gate's ownership model
// is a closed switch over exactly these; an unrecognized value refuses.
const (
	// loopOpContinue: a submission resolving onto an existing loop, by explicit
	// reply_to or by auto-continue.
	loopOpContinue = "continue"
	// loopOpCancel: the /cancel chat command.
	loopOpCancel = "cancel"
	// loopOpSignal: a control signal posted to the loop signal endpoint.
	loopOpSignal = "signal"
	// loopOpApprove: an approval decision on a gated tool call.
	loopOpApprove = "approve"
	// loopOpRead: reading a loop's record (/status, GET /loops/{id}).
	loopOpRead = "read"
)

// loopAdmissionRequest is what a seam hands the gate. Seam and Field are the
// seam's own vocabulary and travel into the metric label and the refusal Detail
// respectively: Field is the name the CALLER used for the token (reply_to, id,
// loop_id), so a refusal names something the caller can act on.
type loopAdmissionRequest struct {
	Seam      string
	Field     string
	Operation string
	LoopID    string
	// Requester is the caller-ASSERTED identity. Nothing verifies it (see the
	// gate's doc comment); it is matched, never trusted.
	Requester string
}

// loopFacts is the merged observation of one loop: the union of the process
// tracker and the durable AGENT_LOOPS record, reconciled. It is returned by an
// admitted request so a seam that needs the loop's route (the signal lane) reads
// it from here rather than recomputing it from a source the gate already read.
type loopFacts struct {
	LoopID      string
	UserID      string
	ChannelType string
	ChannelID   string
	// Terminal is true when EITHER source reports a settled state. Fail-closed:
	// a tracker that has not yet seen the terminal event must not admit a
	// continuation the durable record already refuses.
	Terminal bool
	// Tracked and Persisted report which sources held the loop. Both false never
	// reaches a caller — that is the not-found refusal.
	Tracked   bool
	Persisted bool
}

// loopLookupOutcome is the tri-state of the merged lookup. Absence, an unread
// record, and a conflicting merge are three different answers and the gate
// refuses each with its own reason; collapsing them would answer "not found" for
// a NATS outage.
type loopLookupOutcome int

const (
	loopLookupFound loopLookupOutcome = iota
	loopLookupAbsent
	loopLookupUnreadable
	loopLookupConflict
)

// loopLookup is one merged observation. The three values are correlated — an
// outcome, the facts it produced, and the cause when it produced none — so they
// travel as a struct rather than as a positional tuple.
type loopLookup struct {
	outcome loopLookupOutcome
	facts   loopFacts
	cause   error
}

// admitLoopRequest is the ONE gate every request naming an existing loop passes
// through. No seam hand-rolls any part of the decision.
//
// It runs three checks in a FIXED order — form, then existence, then ownership —
// so a later reason never masks an earlier one: a malformed token is always
// answered as malformed and never as "not found" or "not yours", and an absent
// loop is always answered as absent and never as "not yours". The order is the
// contract, not an implementation detail: it is what makes a refusal reason
// diagnostic instead of a disclosure of whether some other party's loop exists.
//
// It is NOT authorization. Requester is asserted by the caller — taken from
// product middleware when middleware supplied it, otherwise from the request
// body's own claimed user field, otherwise from a default — and nothing verifies
// it. The gate converts an accidental cross-attach into a typed refusal and
// makes every refusal countable; it does not isolate mutually untrusted parties.
// Authorization is a separate contract (epic #1205).
//
// It NEVER reads AGENT_TRAJECTORIES, ObjectStore evidence, or any other
// execution-audit surface: agent execution evidence is write-only from
// execution's side, and no admission decision may depend on it.
//
// Every refusal is metered and logged exactly once, here. A seam returns the
// error the gate produced and counts nothing of its own.
func (c *Component) admitLoopRequest(ctx context.Context, req loopAdmissionRequest) (loopFacts, error) {
	// Form. The predicate has one home (internal/looptoken); this gate holds no
	// second spelling of loop-token shape. An empty token reaches here only from
	// a seam that resolved nothing to attach to, which is a caller bug, not a
	// mint signal — the mint decision belongs to the submission path BEFORE it
	// calls the gate.
	if !looptoken.Valid(req.LoopID) {
		return loopFacts{}, c.refuseLoopRequest(req, codeLoopTokenInvalid, fmt.Errorf(
			"%s %q is not a loop ID this framework minted: a loop ID is an opaque token you receive "+
				"and echo back verbatim, never one you author", req.Field, req.LoopID))
	}

	// Existence, from merged facts — never from process memory alone.
	lookup := c.lookupLoop(ctx, req.LoopID)
	switch lookup.outcome {
	case loopLookupFound:
		// The only outcome that reaches the ownership check.
	case loopLookupAbsent:
		return loopFacts{}, c.refuseLoopRequest(req, codeLoopNotFound,
			fmt.Errorf("%s %q names no loop", req.Field, req.LoopID))
	case loopLookupUnreadable:
		return loopFacts{}, c.refuseLoopRequest(req, codeLoopUnreadable,
			fmt.Errorf("loop %q state is not readable right now: %w", req.LoopID, lookup.cause))
	case loopLookupConflict:
		return loopFacts{}, c.refuseLoopRequest(req, codeLoopOwnerConflict,
			fmt.Errorf("loop %q has conflicting records: %w", req.LoopID, lookup.cause))
	default:
		// An outcome this switch does not interpret fails closed rather than
		// admitting on an observation nobody read.
		return loopFacts{}, c.refuseLoopRequest(req, codeLoopUnreadable,
			fmt.Errorf("loop %q lookup produced an unhandled outcome", req.LoopID))
	}

	if err := c.authorizeLoopOperation(req, lookup.facts); err != nil {
		return loopFacts{}, err
	}
	return lookup.facts, nil
}

// authorizeLoopOperation applies the ownership model to an EXISTING loop. It is
// separated from admitLoopRequest only so the fixed check order reads as three
// statements; it is never called from anywhere else, and calling it without the
// form and existence checks having run would invert the ordering the spec pins.
//
// The model is exactly as ruled and is not extended:
//
//   - continue: requester == the loop's recorded owner.
//   - cancel, signal: requester == owner, OR requester in cancel_any.
//   - approve: requester in the approve list, ownership NOT consulted — a
//     second-party reviewer is the entire point of an approval, and a later
//     change that "fixes" this by adding an owner check removes the capability.
//   - read: neither ownership nor permission; form and existence only.
//
// Permissions.CancelOwn is deliberately NOT consulted here. It keeps exactly one
// home, the /cancel command's declared permission, which the command lane checks
// before it ever reaches this gate (owner ruling R2).
//
// An unknown owner fails closed for every operation that consults the owner.
// A user-lane request naming a SYSTEM-lane loop — one spawned by a rule's
// agent-publish action, which carries no user owner — is therefore refused. That
// is the ruling, not an oversight. It does not reach system-lane traffic, which
// never traverses this gate at all. It also does not reach approve or read,
// which do not consult the owner: gating an approval on an owner the loop never
// had would delete approvals for every autonomously spawned loop.
func (c *Component) authorizeLoopOperation(req loopAdmissionRequest, facts loopFacts) error {
	switch req.Operation {
	case loopOpRead:
		return nil

	case loopOpApprove:
		if !c.hasPermission(req.Requester, "approve") {
			return c.refuseLoopRequest(req, codeLoopNotPermitted,
				fmt.Errorf("requester is not permitted to approve loop %q", req.LoopID))
		}
		return nil

	case loopOpContinue:
		if facts.Terminal {
			return c.refuseLoopRequest(req, codeLoopTerminal,
				fmt.Errorf("loop %q has already settled and cannot be continued", req.LoopID))
		}
		if facts.UserID == "" || facts.UserID != req.Requester {
			return c.refuseLoopRequest(req, codeLoopNotOwned,
				fmt.Errorf("%s %q names a loop the requester does not own", req.Field, req.LoopID))
		}
		return nil

	case loopOpCancel, loopOpSignal:
		if c.inList(req.Requester, c.config.Permissions.CancelAny) {
			return nil
		}
		if facts.UserID == "" || facts.UserID != req.Requester {
			return c.refuseLoopRequest(req, codeLoopNotOwned,
				fmt.Errorf("%s %q names a loop the requester does not own", req.Field, req.LoopID))
		}
		return nil

	default:
		// An unrecognized operation is a programming error at a seam, and it
		// fails closed rather than falling through to an admit.
		return c.refuseLoopRequest(req, codeLoopNotPermitted,
			fmt.Errorf("unknown loop operation %q", req.Operation))
	}
}

// lookupLoop merges the process tracker and the durable AGENT_LOOPS record.
// Neither is authority alone: the tracker is empty after a process replacement,
// and the durable record may be absent for a live loop because persisting it is
// best-effort. Present in EITHER means the loop exists.
//
// It reuses the readers that already exist rather than re-deriving them:
// getSnapshot for the immutable process read (the raw tracker pointer races
// concurrent create/approval updates, which is why that method exists),
// loadPersistedLoop for the durable read — which observes the bucket name
// through the declared KV read port and never a constant — isLoopRecordAbsent
// for the absence-versus-failure distinction, and mergeRouteField to reconcile
// the route across the two observations.
//
// Degradation is explicit, because a design that leaves it implicit gets it
// wrong. Tracker hit: admit, whatever the durable read did — the owner is
// already known. Tracker miss plus key absence: not found. Tracker miss plus any
// other read failure: unreadable, never an admit on a record nobody read.
func (c *Component) lookupLoop(ctx context.Context, loopID string) loopLookup {
	tracked := c.loopTracker.getSnapshot(loopID)
	persisted, persistErr := c.loadPersistedLoop(ctx, loopID)

	if tracked == nil {
		switch {
		case persistErr != nil && isLoopRecordAbsent(persistErr):
			return loopLookup{outcome: loopLookupAbsent, cause: persistErr}
		case persistErr != nil:
			return loopLookup{outcome: loopLookupUnreadable, cause: persistErr}
		case persisted == nil:
			// A nil record with a nil error is not an answer; treat the absence
			// of both an error and a record as absence of the loop.
			return loopLookup{outcome: loopLookupAbsent}
		}
		return loopLookup{outcome: loopLookupFound, facts: persistedLoopFacts(persisted)}
	}

	trackedFacts := trackerLoopFacts(tracked)
	if persistErr != nil || persisted == nil {
		if persistErr != nil {
			c.logToleratedDurableReadFailure(loopID, persistErr)
		}
		return loopLookup{outcome: loopLookupFound, facts: trackedFacts}
	}

	merged, err := mergeLoopFacts(trackedFacts, persistedLoopFacts(persisted))
	if err != nil {
		return loopLookup{outcome: loopLookupConflict, cause: err}
	}
	return loopLookup{outcome: loopLookupFound, facts: merged}
}

// logToleratedDurableReadFailure declares the one continue-past-a-failure this
// gate performs, so it is a recorded event rather than a private choice.
func (c *Component) logToleratedDurableReadFailure(loopID string, err error) {
	if c.logger == nil {
		return
	}
	c.logger.Warn(loopDurableReadToleratedLogMessage,
		slog.String("loop_id", loopID),
		slog.String("error", err.Error()))
}

// trackerLoopFacts projects the process tracker's record.
func trackerLoopFacts(info *LoopInfo) loopFacts {
	return loopFacts{
		LoopID:      info.LoopID,
		UserID:      info.UserID,
		ChannelType: info.ChannelType,
		ChannelID:   info.ChannelID,
		Terminal:    isTerminalState(info.State),
		Tracked:     true,
	}
}

// persistedLoopFacts projects the durable AGENT_LOOPS record.
func persistedLoopFacts(record *agentic.LoopEntity) loopFacts {
	return loopFacts{
		LoopID:      record.ID,
		UserID:      record.UserID,
		ChannelType: record.ChannelType,
		ChannelID:   record.ChannelID,
		Terminal:    record.State.IsTerminal(),
		Persisted:   true,
	}
}

// mergeLoopFacts reconciles two observations of one loop. The route fields go
// through mergeRouteField, the same rule terminal settlement already uses, so a
// conflicting nonempty value is a refusal rather than a silent preference for
// one source.
//
// Terminality is NOT merged that way, and the difference is deliberate: the two
// sources observe the same state at different times, so a disagreement is
// ordinary lag rather than corruption. It resolves fail-closed — settled in
// EITHER source means settled.
func mergeLoopFacts(tracked, persisted loopFacts) (loopFacts, error) {
	userID, err := mergeRouteField("user_id", tracked.UserID, persisted.UserID)
	if err != nil {
		return loopFacts{}, err
	}
	channelType, err := mergeRouteField("channel_type", tracked.ChannelType, persisted.ChannelType)
	if err != nil {
		return loopFacts{}, err
	}
	channelID, err := mergeRouteField("channel_id", tracked.ChannelID, persisted.ChannelID)
	if err != nil {
		return loopFacts{}, err
	}
	return loopFacts{
		LoopID:      tracked.LoopID,
		UserID:      userID,
		ChannelType: channelType,
		ChannelID:   channelID,
		Terminal:    tracked.Terminal || persisted.Terminal,
		Tracked:     true,
		Persisted:   true,
	}, nil
}

// refuseLoopRequest builds the classified refusal, meters it once, and logs it
// once. Every refusal in this file returns through here, which is what makes
// "metered exactly once" a property of the code rather than of a convention.
func (c *Component) refuseLoopRequest(req loopAdmissionRequest, code string, cause error) error {
	refusal := errs.ClassifiedCodeDetail(refusalClass(code), code, map[string]any{
		detailSeam:      req.Seam,
		detailField:     req.Field,
		detailOperation: req.Operation,
	}, cause)
	c.recordLoopAdmissionRefusal(req, code, refusal)
	return refusal
}

// refusalClass maps a refusal code to its error class. Only an unread durable
// record is transient — the request is answerable later, and a caller may retry
// it. Every other refusal is a bad precondition that retrying cannot fix.
func refusalClass(code string) errs.ErrorClass {
	if code == codeLoopUnreadable {
		return errs.ErrorTransient
	}
	return errs.ErrorInvalid
}

// loopAdmissionMetricReason maps an admission refusal to its
// loop_admission_refusals_total reason label, or returns ok=false for any other
// error. One home for the mapping so two seams cannot disagree about what the
// same refusal is called.
func loopAdmissionMetricReason(err error) (string, bool) {
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		return "", false
	}
	switch classified.Code {
	case codeLoopTokenInvalid:
		return reasonFormMalformed, true
	case codeLoopNotFound:
		return reasonExistenceAbsent, true
	case codeLoopUnreadable:
		return reasonExistenceUnreadable, true
	case codeLoopOwnerConflict:
		return reasonExistenceConflict, true
	case codeLoopTerminal:
		return reasonStateTerminal, true
	case codeLoopNotOwned:
		return reasonOwnershipNotOwner, true
	case codeLoopNotPermitted:
		return reasonOwnershipNotPermitted, true
	default:
		return "", false
	}
}

// recordLoopAdmissionRefusal meters a refusal once and logs it loudly.
//
// The log names WHERE (seam) and WHAT WAS ASKED (operation) and carries the loop
// token only once the form check has passed, so a caller-authored string never
// reaches operator logs. It never names the requester: identity on this plane is
// asserted by the caller and verified by nothing, so a logged one is a claim
// dressed as a fact.
func (c *Component) recordLoopAdmissionRefusal(req loopAdmissionRequest, code string, err error) {
	reason, ok := loopAdmissionMetricReason(err)
	if !ok {
		return
	}
	if c.metrics != nil {
		c.metrics.recordLoopAdmissionRefusal(req.Seam, reason)
	}
	if c.logger == nil {
		return
	}
	attrs := []any{
		slog.String("seam", req.Seam),
		slog.String("operation", req.Operation),
		slog.String("reason", reason),
		slog.String("field", req.Field),
	}
	if code != codeLoopTokenInvalid {
		attrs = append(attrs, slog.String("loop_id", req.LoopID))
	}
	c.logger.Warn(loopAdmissionRefusalLogMessage, attrs...)
}

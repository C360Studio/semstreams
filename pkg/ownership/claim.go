package ownership

import (
	"fmt"
	"strings"

	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// idArity is the fixed segment count of a 6-part federated entity ID
// (org.platform.domain.system.type.instance). Owner-claim patterns are
// 6-part globs over this shape, matched the same way as
// pkg/lifecycle.matchPattern (manager_query.go:428).
const idArity = 6

// WriteMode is the mode a claim's predicate group is written in (ADR-056
// Decision 1 — the ADR-055 per-predicate matrix promoted to the ownership
// primitive). Only the two OWNING modes participate in overlap rejection;
// append-evidence has no single owner and is exempt.
type WriteMode string

const (
	// ModeReplaceOwned — single-valued, re-emitted: the owner reconciles its
	// whole owned predicate group on every write (RemoveOwned + Add).
	ModeReplaceOwned WriteMode = "replace-owned"
	// ModeCASTransition — ExpectedRevision phase/read-modify-write (the
	// lifecycle Manager.Transition shape).
	ModeCASTransition WriteMode = "cas-transition"
	// ModeAppendEvidence — multi-valued, must-exist, NO owner: many writers may
	// append (web/ops back-links). Exempt from the overlap check.
	ModeAppendEvidence WriteMode = "append-evidence"
)

// isOwning reports whether this mode participates in overlap rejection — the
// two modes where a second writer silently clobbers (Decision 2).
func (m WriteMode) isOwning() bool {
	return m == ModeReplaceOwned || m == ModeCASTransition
}

func (m WriteMode) valid() bool {
	return m == ModeReplaceOwned || m == ModeCASTransition || m == ModeAppendEvidence
}

// EdgeMode is the mode of a ForeignEdgeClaim (Decision 4). It governs the
// inverse-gate / pending-edge behaviour, NOT the Decision-2 overlap check.
// Decision 4 names three modes (Conditional / Backfill / Strict); the fourth,
// EdgeNoBirthStub, is the no-birth-target lane added by the fourth-path ruling
// (lane ii — sensorml children that have no independent birth to drain against).
type EdgeMode string

const (
	// EdgeConditional — deferred-apply via the pending-edge buffer; requires a
	// registered inverse predicate (Decision 4 inverse-gate).
	EdgeConditional EdgeMode = "conditional"
	// EdgeBackfill — the inverse is re-derivable from the forward edge.
	EdgeBackfill EdgeMode = "backfill"
	// EdgeStrict — the edge is dropped if its target does not yet exist
	// (edge-presence-insensitive consumers).
	EdgeStrict EdgeMode = "strict"
	// EdgeNoBirthStub — the target has no independent producer, so the edge is
	// materialised via the framework's envelope-bearing referential-stub lane
	// (Decision 4 lane ii — sensorml children). Load-bearing: the stub is the
	// only thing that ever materialises the target node.
	EdgeNoBirthStub EdgeMode = "no-birth-stub"
)

func (m EdgeMode) valid() bool {
	switch m {
	case EdgeConditional, EdgeBackfill, EdgeStrict, EdgeNoBirthStub:
		return true
	default:
		return false
	}
}

// requiresInverse reports whether this edge mode reconstructs the inverse edge
// and therefore requires the predicate to have a registered inverse (the
// Decision-4 inverse-gate). Conditional defers-and-applies and Backfill
// re-derives — both unrecoverable after a birth race without a registered
// inverse. Strict drops-if-absent and NoBirthStub materialises a stub, so
// neither needs an inverse. See ownership.CheckInverseGate.
func (m EdgeMode) requiresInverse() bool {
	return m == EdgeConditional || m == EdgeBackfill
}

// OwnerClaim is owned current state: for entities matching Pattern, Owner is
// responsible for the current value of exactly Predicates, written in Mode
// (ADR-056 Decision 1). This is the claim Decisions 2/3/5 arbitrate.
type OwnerClaim struct {
	Owner      string    `json:"owner"`
	Pattern    string    `json:"pattern"`    // 6-part entity-ID glob
	Predicates []string  `json:"predicates"` // EXACT strings (no prefix/glob on predicates)
	Mode       WriteMode `json:"mode"`
	// Incarnation is the per-process boot nonce (8 bytes crypto/rand hex)
	// recorded at RegisterOwner time from Registry.Incarnation(). It is the
	// incarnation half of the OwnerToken "<owner>#<incarnation>" that
	// producers stamp on mutation requests (ADR-056 PR-1). A later
	// increment's graph-ingest lease check reads this field via OwnerOf to
	// compare against the incoming request's token, rejecting a revived-stale
	// writer whose incarnation no longer matches the live claim.
	// Empty on claims registered before the incarnation fence was shipped
	// (legacy / test paths that use NewRegistry with empty claims stores).
	Incarnation string `json:"incarnation,omitempty"`
}

// Validate checks the claim is well-formed: a 6-part glob pattern, a non-empty
// exact-string predicate set, a valid mode, and a non-empty owner.
func (c OwnerClaim) Validate() error {
	if !validOwnerID(c.Owner) {
		return fmt.Errorf("%w: owner %q is empty or not subject-safe", ErrInvalidClaim, c.Owner)
	}
	if err := semtypes.ValidateEntityIDPattern(c.Pattern); err != nil {
		return fmt.Errorf("%w: invalid entity-ID pattern: %w", ErrInvalidClaim, err)
	}
	if len(c.Predicates) == 0 {
		return fmt.Errorf("%w: claim by %q on %q has no predicates", ErrInvalidClaim, c.Owner, c.Pattern)
	}
	for _, p := range c.Predicates {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("%w: claim by %q on %q has an empty predicate", ErrInvalidClaim, c.Owner, c.Pattern)
		}
		if strings.ContainsAny(p, "*>") {
			return fmt.Errorf("%w: predicate %q must be an exact string (no wildcards)", ErrInvalidClaim, p)
		}
		if err := vocabulary.RequireDeclaredPredicate(p); err != nil {
			return fmt.Errorf("%w: claim by %q declares undeclared or invalid predicate: %w", ErrInvalidClaim, c.Owner, err)
		}
	}
	if !c.Mode.valid() {
		return fmt.Errorf("%w: claim by %q has invalid write mode %q", ErrInvalidClaim, c.Owner, c.Mode)
	}
	return nil
}

// ForeignEdgeClaim is a relationship-producer claim (ADR-056 Decision 1): the
// producer asserts an edge with Predicate whose Subject is a DIFFERENT entity
// than the one it owns. It carries a single edge predicate and an edge-mode,
// and is governed by Decision 4's inverse-gate — NOT by Decision 2's overlap
// check. Two ForeignEdgeClaims never overlap each other (both legitimately add
// edges); the cross-type Owner×ForeignEdge check (Decision 2 MEDIUM) is what
// stops an OwnerClaim's reconcile from silently stripping a foreign edge.
type ForeignEdgeClaim struct {
	Owner     string   `json:"owner"`
	Predicate string   `json:"predicate"` // single edge predicate
	Mode      EdgeMode `json:"mode"`
	// Producer is the payload MessageType whose Graphable emits this foreign
	// edge. The T2-regroup seam keys its reject on (message_type, predicate)
	// (ADR-056 Decision 4), so a fully-specified claim names the producing type.
	// Empty means "any producer" — the transitional shape before a producer has
	// declared its type; ForeignEdgeClaimFor prefers an exact producer match.
	Producer string `json:"producer,omitempty"`
	// TargetPattern is the 6-part glob of entities the edge lands on (its
	// foreign Subject). Empty means match-any — the conservative default for
	// the cross-type check. cs-api declares its target pattern so its OwnerClaim
	// and ForeignEdgeClaim on the same predicate (isHostedBy own→parent vs
	// child→System) are not mistaken for a self-collision (same owner is always
	// exempt; see checkOverlap).
	TargetPattern string `json:"target_pattern,omitempty"`
}

// Validate checks the foreign-edge claim is well-formed.
func (f ForeignEdgeClaim) Validate() error {
	if !validOwnerID(f.Owner) {
		return fmt.Errorf("%w: foreign-edge owner %q is empty or not subject-safe", ErrInvalidClaim, f.Owner)
	}
	if strings.TrimSpace(f.Predicate) == "" {
		return fmt.Errorf("%w: foreign-edge claim by %q has no predicate", ErrInvalidClaim, f.Owner)
	}
	if strings.ContainsAny(f.Predicate, "*>") {
		return fmt.Errorf("%w: foreign-edge predicate %q must be exact (no wildcards)", ErrInvalidClaim, f.Predicate)
	}
	if err := vocabulary.RequireDeclaredPredicate(f.Predicate); err != nil {
		return fmt.Errorf("%w: foreign-edge claim by %q declares undeclared or invalid predicate: %w", ErrInvalidClaim, f.Owner, err)
	}
	if !f.Mode.valid() {
		return fmt.Errorf("%w: foreign-edge claim by %q has invalid edge mode %q", ErrInvalidClaim, f.Owner, f.Mode)
	}
	if f.TargetPattern != "" {
		if err := semtypes.ValidateEntityIDPattern(f.TargetPattern); err != nil {
			return fmt.Errorf("%w: invalid foreign-edge target pattern: %w", ErrInvalidClaim, err)
		}
	}
	return nil
}

// CoordinationWaiver records a deliberate, predicate-scoped exemption from the
// overlap check between two owners (ADR-056 Decision 2). It is audit-stored at
// EPOCH scope (epoch.Waivers, not nested under an owner) so it survives the
// compaction of either party. A waiver covers a contested cell only when BOTH
// owners have declared a matching waiver (mutual consent — neither owner can
// unilaterally waive itself into the other's cell); see waived() in overlap.go.
//
// ReviewBy is intended to be the expiry boundary enforced at the next
// registration (a review-obligation forcing function). NOTE: expiry parsing /
// enforcement is DEFERRED to the enforcement-wiring increment — the format
// (RFC3339 date vs release tag) is not yet pinned, so today an unexpired and an
// expired waiver are treated identically. Do not rely on ReviewBy to auto-lapse
// a waiver yet.
type CoordinationWaiver struct {
	Owner      string   `json:"owner"`      // the owner declaring this half of the waiver
	With       string   `json:"with"`       // the other owner of the contested cell
	Predicates []string `json:"predicates"` // the exact predicates the waiver covers
	Reason     string   `json:"reason"`
	ReviewBy   string   `json:"review_by,omitempty"` // expiry boundary (format TBD; not yet enforced)
	Ref        string   `json:"ref,omitempty"`       // link to the design/issue justifying it
}

// Validate checks the waiver names both owners, at least one predicate, and a
// reason — an unexplained waiver is not a waiver.
func (w CoordinationWaiver) Validate() error {
	if !validOwnerID(w.Owner) || !validOwnerID(w.With) {
		return fmt.Errorf("%w: waiver must name two subject-safe owners (owner=%q with=%q)", ErrInvalidClaim, w.Owner, w.With)
	}
	if w.Owner == w.With {
		return fmt.Errorf("%w: waiver owner and with are the same (%q)", ErrInvalidClaim, w.Owner)
	}
	if len(w.Predicates) == 0 {
		return fmt.Errorf("%w: waiver between %q and %q covers no predicates", ErrInvalidClaim, w.Owner, w.With)
	}
	for _, predicate := range w.Predicates {
		if err := vocabulary.RequireDeclaredPredicate(predicate); err != nil {
			return fmt.Errorf("%w: waiver between %q and %q declares undeclared or invalid predicate: %w",
				ErrInvalidClaim, w.Owner, w.With, err)
		}
	}
	if strings.TrimSpace(w.Reason) == "" {
		return fmt.Errorf("%w: waiver between %q and %q has no reason", ErrInvalidClaim, w.Owner, w.With)
	}
	return nil
}

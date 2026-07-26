package projection

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/c360studio/semstreams/pkg/ownership"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// validIndexingProfiles are the ADR-054 channel-b profile values a contract may
// declare. The contract subsumes the indexing-profile declaration rather than
// inventing a new surface; stamping it onto create_with_triples is a later
// (wiring) increment.
var validIndexingProfiles = map[string]struct{}{
	"content": {}, "control": {}, "signal": {}, "trace": {},
}

// validationOwner is a subject-safe placeholder owner used only to run a
// Contract's derived claims through the ownership validators (which require an
// owner) without binding it to a real owner. Never registered.
const validationOwner = "projection-validation-probe"

// PredicateGroup is a set of predicates a projection emits in ONE write mode
// (ADR-056 Decision 1). A predicate appears in exactly one group per contract —
// one predicate, one write mode per owner.
type PredicateGroup struct {
	Name       string              `json:"name,omitempty"`
	Mode       ownership.WriteMode `json:"mode"`
	Predicates []string            `json:"predicates"`
}

// ForeignEdge is a relationship edge the projection writes onto a DIFFERENT
// entity than the one it owns (ADR-056 Decision 4). TargetPattern is the 6-part
// glob of entities the edge lands on (empty = match-any).
type ForeignEdge struct {
	Predicate     string             `json:"predicate"`
	Mode          ownership.EdgeMode `json:"mode"`
	TargetPattern string             `json:"target_pattern,omitempty"`
}

// Contract is the graph projection contract for one entity type / Graphable /
// gateway resource (ADR-056 Decision 6). It is OWNER-LESS — it declares the
// SHAPE of what a projection emits; the owner id is bound at Derive/Bind time,
// because the same projection shape may be emitted by different owners in
// different deployments.
type Contract struct {
	// Name identifies the projection — typically the payload MessageType it is
	// co-located with, or a logical projection name. Used as the registry key
	// and in error messages.
	Name string `json:"name"`
	// MessageType is the payload type whose Graphable this contract projects for.
	// Stamped onto every derived ForeignEdgeClaim as its Producer so the T2-seam
	// reject can key on (message_type, predicate) (ADR-056 Decision 4). Optional:
	// empty derives Producer-empty ("any producer") foreign edges — the
	// transitional shape. A contract WITH foreign edges should name its type.
	MessageType string `json:"message_type,omitempty"`
	// EntityPattern is the 6-part entity-ID glob the projection writes (the
	// entity it owns).
	EntityPattern string `json:"entity_pattern"`
	// Groups are the owned/append predicate groups by write mode.
	Groups []PredicateGroup `json:"groups,omitempty"`
	// BirthPredicates are a MutationClient convention: they authorize
	// primary-subject facts only on CreateWithTriples and derive no ownership
	// claim. The graph mutation service does not independently make these
	// predicates immutable; writers outside this client contract can still
	// mutate them.
	BirthPredicates []string `json:"birth_predicates,omitempty"`
	// ForeignEdges are the relationship edges the projection writes onto other
	// entities.
	ForeignEdges []ForeignEdge `json:"foreign_edges,omitempty"`
	// IndexingProfile (ADR-054, optional): one of content|control|signal|trace.
	IndexingProfile string `json:"indexing_profile,omitempty"`
}

// Validate checks the contract is well-formed. It runs the contract-specific
// checks (name, one-write-mode-per-predicate, indexing profile) and then defers
// pattern / predicate / mode / foreign-edge / self-overlap validation to the
// ownership validators by deriving claims under a placeholder owner — so the
// projection layer never re-implements (and never drifts from) ownership's
// rules.
func (c Contract) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("%w: contract has no name", ErrInvalidContract)
	}
	if len(c.Groups) == 0 && len(c.BirthPredicates) == 0 && len(c.ForeignEdges) == 0 {
		return fmt.Errorf(
			"%w: contract %q declares no predicate groups, birth predicates, or foreign edges",
			ErrInvalidContract,
			c.Name,
		)
	}
	if err := semtypes.ValidateEntityIDPattern(c.EntityPattern); err != nil {
		return fmt.Errorf("%w: contract %q has invalid entity pattern: %w", ErrInvalidContract, c.Name, err)
	}

	// A predicate must have exactly one write mode within a contract (one owner):
	// the same predicate in replace-owned and append-evidence is contradictory.
	seen := make(map[string]ownership.WriteMode)
	groupNames := make(map[string]struct{})
	for _, g := range c.Groups {
		if err := validatePredicateGroupName(g.Name); err != nil {
			return fmt.Errorf("%w: contract %q: %w", ErrInvalidContract, c.Name, err)
		}
		if g.Name != "" {
			if _, duplicate := groupNames[g.Name]; duplicate {
				return fmt.Errorf(
					"%w: contract %q repeats predicate group name %q",
					ErrInvalidContract,
					c.Name,
					g.Name,
				)
			}
			groupNames[g.Name] = struct{}{}
		}
		for _, p := range g.Predicates {
			if prev, dup := seen[p]; dup {
				return fmt.Errorf("%w: contract %q lists predicate %q in two write modes (%q and %q)",
					ErrInvalidContract, c.Name, p, prev, g.Mode)
			}
			seen[p] = g.Mode
		}
	}
	birthSeen := make(map[string]struct{}, len(c.BirthPredicates))
	for _, predicate := range c.BirthPredicates {
		if err := vocabulary.RequireDeclaredPredicate(predicate); err != nil {
			return fmt.Errorf(
				"%w: contract %q has invalid birth predicate: %w",
				ErrInvalidContract,
				c.Name,
				err,
			)
		}
		if _, duplicate := birthSeen[predicate]; duplicate {
			return fmt.Errorf(
				"%w: contract %q repeats birth predicate %q",
				ErrInvalidContract,
				c.Name,
				predicate,
			)
		}
		if mode, overlap := seen[predicate]; overlap {
			return fmt.Errorf(
				"%w: contract %q birth predicate %q overlaps %q group",
				ErrInvalidContract,
				c.Name,
				predicate,
				mode,
			)
		}
		birthSeen[predicate] = struct{}{}
	}

	if c.IndexingProfile != "" {
		if _, ok := validIndexingProfiles[c.IndexingProfile]; !ok {
			return fmt.Errorf("%w: contract %q has unknown indexing profile %q (want content|control|signal|trace)",
				ErrInvalidContract, c.Name, c.IndexingProfile)
		}
	}

	// Full mutable-lane validation via the ownership validators (predicates,
	// modes, foreign edges, intra-registration self-overlap). A birth-only
	// contract deliberately derives no registration.
	if len(c.Groups) != 0 || len(c.ForeignEdges) != 0 {
		if err := c.registration(validationOwner).Validate(); err != nil {
			return fmt.Errorf("%w: contract %q: %w", ErrInvalidContract, c.Name, err)
		}
	}
	return nil
}

func validatePredicateGroupName(name string) error {
	if name == "" {
		return nil
	}
	if strings.ContainsAny(name, ".*>") || strings.IndexFunc(name, unicode.IsSpace) >= 0 {
		return fmt.Errorf(
			"predicate group name %q must be one subject-safe token without dots, whitespace, or wildcards",
			name,
		)
	}
	return nil
}

// registration builds (without validating) the ownership.Registration this
// contract derives for the given owner: one OwnerClaim per predicate group, one
// ForeignEdgeClaim per foreign edge.
func (c Contract) registration(owner string) ownership.Registration {
	reg := ownership.Registration{Owner: owner}
	for _, g := range c.Groups {
		reg.Claims = append(reg.Claims, ownership.OwnerClaim{
			Owner:      owner,
			Pattern:    c.EntityPattern,
			Predicates: g.Predicates,
			Mode:       g.Mode,
		})
	}
	for _, f := range c.ForeignEdges {
		reg.ForeignEdges = append(reg.ForeignEdges, ownership.ForeignEdgeClaim{
			Owner:         owner,
			Predicate:     f.Predicate,
			Mode:          f.Mode,
			Producer:      c.MessageType,
			TargetPattern: f.TargetPattern,
		})
	}
	return reg
}

// Derive binds one or more contracts to an owner id and returns the aggregated
// ownership.Registration — the claims that owner registers (one OwnerClaim per
// group across all contracts, one ForeignEdgeClaim per foreign edge). Every
// contract is validated, and the AGGREGATE is checked for self-overlap (two of
// the owner's own contracts claiming the same cell — a config bug). Cross-OWNER
// overlap is left to ownership.RegisterOwner against the live epoch.
func Derive(owner string, contracts ...Contract) (ownership.Registration, error) {
	if len(contracts) == 0 {
		return ownership.Registration{}, fmt.Errorf("%w: no contracts to derive for owner %q", ErrInvalidContract, owner)
	}
	reg := ownership.Registration{Owner: owner}
	seen := make(map[string]struct{}, len(contracts))
	for _, c := range contracts {
		if _, dup := seen[c.Name]; dup {
			return ownership.Registration{}, fmt.Errorf("%w: owner %q derives contract %q more than once", ErrInvalidContract, owner, c.Name)
		}
		seen[c.Name] = struct{}{}
		if err := c.Validate(); err != nil {
			return ownership.Registration{}, err
		}
		derived := c.registration(owner)
		reg.Claims = append(reg.Claims, derived.Claims...)
		reg.ForeignEdges = append(reg.ForeignEdges, derived.ForeignEdges...)
	}
	if len(reg.Claims) != 0 || len(reg.ForeignEdges) != 0 {
		if err := reg.Validate(); err != nil {
			return ownership.Registration{}, fmt.Errorf("%w: deriving owner %q: %w", ErrInvalidContract, owner, err)
		}
	}
	return reg, nil
}

// Bind is the component/gateway boot step: derive the owner's claims from its
// contracts and register them with the ownership substrate. On success it
// returns the owner's typed OwnerToken (ADR-056 PR-3.5) — the write-lease
// credential the bound owner stamps on its mutation requests, surfaced on the
// bind result so the right path is the easy path (producers never hand-compose
// "<owner>#<incarnation>"). Returns the zero token and ownership.ErrOwnershipOverlap
// (wrapped) when another owner already holds a derived cell, or the zero token
// and a derive/validation error.
func Bind(ctx context.Context, ownerReg *ownership.Registry, owner string, contracts ...Contract) (ownership.OwnerToken, error) {
	registration, err := Derive(owner, contracts...)
	if err != nil {
		return ownership.OwnerToken{}, err
	}
	if len(registration.Claims) == 0 && len(registration.ForeignEdges) == 0 {
		return ownership.OwnerToken{}, nil
	}
	if ownerReg == nil {
		return ownership.OwnerToken{}, fmt.Errorf("%w: ownership registry is required", ErrInvalidContract)
	}
	if err := ownerReg.RegisterOwner(ctx, registration); err != nil {
		return ownership.OwnerToken{}, err
	}
	return ownerReg.OwnerToken(owner), nil
}

// BindAndHeartbeat is Bind plus liveness enrollment for a STATIC projection owner
// — one registered once at boot for the whole process lifetime (a graph-writer, a
// rule pack), as opposed to a lifecycle.Manager workflow owner (which the Manager
// enrolls into its own heartbeater). It returns the bound owner's typed
// OwnerToken on success (the same credential Bind surfaces; the zero token on
// failure). A static owner that derives a real OwnerClaim
// MUST heartbeat: RegisterOwner bumps its OWNER_PRESENCE key once at registration,
// but without ongoing heartbeats that key ages out after ownership.PresenceTTL and
// the next registrant compacts the claim out of the epoch — the FE-only compaction
// exemption (epoch.go) does NOT cover an owning claim.
//
// Enrollment happens ONLY on a successful Bind: a rejected/overlapping owner holds
// no recorded claim to keep alive. A nil hb binds without enrolling (caller opted
// out of liveness, e.g. FE-only owners). The caller owns the Heartbeater's lifetime
// — build it once at the composition root (ownerReg.NewHeartbeater), run it on a
// shutdown-cancelled context (go hb.Run(ctx)), and pass it here for every static
// owner it binds.
func BindAndHeartbeat(ctx context.Context, ownerReg *ownership.Registry, hb *ownership.Heartbeater, owner string, contracts ...Contract) (ownership.OwnerToken, error) {
	token, err := Bind(ctx, ownerReg, owner, contracts...)
	if err != nil {
		return ownership.OwnerToken{}, err
	}
	if hb != nil && !token.IsZero() {
		hb.Add(owner)
	}
	return token, nil
}

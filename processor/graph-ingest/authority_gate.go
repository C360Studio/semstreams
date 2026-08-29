package graphingest

import (
	"errors"
	"log/slog"

	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// The metric reason labels for an authority rejection. They are deliberately
// NOT the classified code (entity_id_authority_invalid) and not the error
// reason (foreign_authority / local_authority_claimed): mutation_rejections is
// an operator-facing series whose label reads as a concern followed by its
// case, mirroring structural_invalid, and the spec pins these two spellings.
// The mapping has exactly one home — authorityMetricReason.
const (
	authorityMetricReasonForeign = "authority_foreign"
	authorityMetricReasonClaimed = "authority_claimed"
)

// arrivalDirect is the `arrival` label value for the DIRECT in-process lane —
// an in-binary call into CreateEntity, MergeEntity, the hierarchy triple
// adapter, or the shared append/delete bodies, rather than a NATS request. The
// other two lanes label with the subject they arrived on; a direct call has
// none, and this fixed token holds that position. It is a lane name and never a
// caller name or an identity, so the series stays bounded by exactly one value.
const arrivalDirect = "direct"

// authorityRejectionLogMessage is the single WARN a refused candidate produces
// on any lane. Named so the test pinning the requirement's "loud log" matches
// the production string instead of a copy that can drift away from it.
const authorityRejectionLogMessage = "graph-ingest: entity authority rejected"

// authorizeSubject validates positions 1-2 of a candidate SUBJECT identity
// against the deployment's own authority for the lane it arrived on.
//
// It is called at every seam that already validates an entity ID structurally,
// on every lane — Graphable fact arrival, each graph.mutation.> operation, and
// direct in-process persistence — before any KV I/O. Structural validation runs
// first inside ValidateEntityIDAuthority, so an authority reason never masks a
// malformed candidate.
//
// It is NEVER called for an @id OBJECT: a relationship target keeps structural
// validation only, no stub is created for it, and an absent target is permitted
// (ADR-102 d5), which is what lets a local entity cite an imported one.
//
// importLane is true only for a JetStream input port the operator declared
// "import": true. On that lane a foreign pair is admitted unchanged and the
// deployment's OWN pair is refused — a peer may not mint as us.
func (c *Component) authorizeSubject(subject string, importLane bool) error {
	return semtypes.ValidateEntityIDAuthority(subject, c.org, c.platform, importLane)
}

// authorityMetricReason maps an authority rejection to its mutation_rejections
// reason label, or returns ok=false for any other error. One home for the
// mapping so the fact lane and the mutation lane cannot disagree.
func authorityMetricReason(err error) (string, bool) {
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Code != semtypes.ErrorCodeEntityIDAuthorityInvalid {
		return "", false
	}
	reason, _ := classified.Detail[semtypes.EntityIDDetailReason].(string)
	switch reason {
	case semtypes.EntityIDReasonLocalAuthorityClaimed:
		return authorityMetricReasonClaimed, true
	default:
		// foreign_authority, and any future reason under this code, meter as
		// the foreign case rather than vanishing from the series.
		return authorityMetricReasonForeign, true
	}
}

// recordAuthorityRejection meters an authority rejection once and logs it
// loudly. The log names the lane and the failing segment index and NEVER the
// identity: the whole point of the gate is that a foreign identity is not this
// deployment's to publish, and a rejected one reaching operator logs would
// re-publish exactly what was refused.
func (c *Component) recordAuthorityRejection(arrival, reason string, err error) {
	if c.mutationRejections != nil {
		c.mutationRejections.WithLabelValues(arrival, reason).Inc()
	}
	if c.logger == nil {
		return
	}
	// arrival is the NATS subject the write came in on — the mutation operation
	// or the fact-lane filter subject. It names WHERE, and carries no identity.
	attrs := []any{
		slog.String("arrival", arrival),
		slog.String("reason", reason),
	}
	var classified *errs.ClassifiedError
	if errors.As(err, &classified) {
		// lane is the authority lane the candidate was judged on: "local" or
		// "import" (pkg/types.EntityIDLane*).
		if lane, ok := classified.Detail[semtypes.EntityIDDetailLane].(string); ok {
			attrs = append(attrs, slog.String("lane", lane))
		}
		if index, ok := classified.Detail[semtypes.EntityIDDetailSegmentIndex].(int); ok {
			attrs = append(attrs, slog.Int("segment_index", index))
		}
	}
	c.logger.Warn(authorityRejectionLogMessage, attrs...)
}

// recordDirectAuthorityRejection meters and loudly logs an authority rejection
// taken on the DIRECT in-process lane, then returns the error unchanged so a
// guard reads `return c.recordDirectAuthorityRejection(err)`.
//
// It is the direct lane's counterpart to meteredMutation (mutation_runtime.go)
// and to processIngest's authority branch (keyed_ingest.go). All three route
// through recordAuthorityRejection, which is what makes "metered exactly once"
// a property of the code rather than of a convention.
//
// Exactly once survives the two bodies the RPC lane shares with this one
// (addTriplesLane, deleteEntityAtRevision): each graph.mutation.> handler
// authorizes the same subject and RETURNS before entering them, so a refused
// request is counted on the lane it actually arrived on and never twice. The
// fact lane is the same shape — prepareFactProjection runs the gate and returns
// before mergeEntityOnLane's backstop.
//
// A non-authority error passes through unrecorded; that classification has one
// home, in authorityMetricReason.
func (c *Component) recordDirectAuthorityRejection(err error) error {
	if reason, isAuthority := authorityMetricReason(err); isAuthority {
		c.recordAuthorityRejection(arrivalDirect, reason, err)
	}
	return err
}

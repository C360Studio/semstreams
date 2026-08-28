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
func (c *Component) recordAuthorityRejection(subject, reason string, err error) {
	if c.mutationRejections != nil {
		c.mutationRejections.WithLabelValues(subject, reason).Inc()
	}
	if c.logger == nil {
		return
	}
	attrs := []any{
		slog.String("lane", subject),
		slog.String("reason", reason),
	}
	var classified *errs.ClassifiedError
	if errors.As(err, &classified) {
		if lane, ok := classified.Detail[semtypes.EntityIDDetailLane].(string); ok {
			attrs = append(attrs, slog.String("arrival", lane))
		}
		if index, ok := classified.Detail[semtypes.EntityIDDetailSegmentIndex].(int); ok {
			attrs = append(attrs, slog.Int("segment_index", index))
		}
	}
	c.logger.Warn("graph-ingest: entity authority rejected", attrs...)
}

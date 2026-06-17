package ownership

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
)

// ClaimReader is a READ-ONLY view of the registered ForeignEdgeClaims, for the
// ADR-056 Decision-4 T2-seam reject. graph-ingest is the WRITER of ENTITY_STATES
// but a READER of OWNER_CLAIMS: at the foreign-routing seam it classifies a
// producer's foreign-subject predicates as claimed or unclaimed, emitting the
// unclaimed reject metric.
//
// Unlike Registry, a ClaimReader NEVER creates the bucket and NEVER registers —
// a reader must not conjure the substrate (that is EnsureBuckets' eager-boot
// job). It opens the existing OWNER_CLAIMS read-only and reads the epoch on
// demand. Production foreign-edge volume is zero today (OMS/StoredMessage emit no
// foreign edges), so the per-call epoch read costs nothing in production; a
// Watch-backed cache is a later (4b) optimisation for when foreign edges flow.
type ClaimReader struct {
	claims *natsclient.KVStore
	logger *slog.Logger
}

// NewClaimReader opens OWNER_CLAIMS read-only. It returns an error (the caller
// graceful-skips classification — the seam stays observe-only) when the bucket
// cannot be opened: a resourceless / unmigrated deploy that never ran
// EnsureBuckets, or a transient connection blip at boot. It does NOT create the
// bucket.
func NewClaimReader(ctx context.Context, client *natsclient.Client, logger *slog.Logger) (*ClaimReader, error) {
	if client == nil {
		return nil, fmt.Errorf("ownership: NewClaimReader requires a non-nil NATS client")
	}
	if logger == nil {
		logger = slog.Default()
	}
	bucket, err := client.GetKeyValueBucket(ctx, BucketOwnerClaims)
	if err != nil {
		return nil, fmt.Errorf("ownership: open %s read-only: %w", BucketOwnerClaims, err)
	}
	return &ClaimReader{claims: client.NewKVStore(bucket), logger: logger}, nil
}

// UnclaimedForeignEdges reads the epoch ONCE and returns the deduped, sorted
// subset of `predicates` that have NO registered ForeignEdgeClaim covering
// (producer, predicate) — the foreign edges the T2-seam flags as unclaimed. An
// exact-producer claim wins; a Producer-empty ("any producer") claim is the
// fallback (epoch.foreignEdgeClaimFor). An empty/absent registry means nothing
// is claimed → every predicate is unclaimed. The classification is read-only and
// allocates nothing on the no-foreign-edges happy path (callers pass a non-empty
// predicates slice only when foreign triples exist).
func (cr *ClaimReader) UnclaimedForeignEdges(ctx context.Context, producer string, predicates []string) ([]string, error) {
	uniq := dedupeStrings(predicates)
	if len(uniq) == 0 {
		return nil, nil
	}

	entry, err := cr.claims.Get(ctx, registryKey)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return uniq, nil // empty registry: nothing claimed yet → all unclaimed
		}
		return nil, fmt.Errorf("ownership: read epoch for foreign-edge classification: %w", err)
	}
	ep, err := decodeEpoch(entry.Value)
	if err != nil {
		return nil, err
	}

	var unclaimed []string
	for _, p := range uniq {
		if _, ok := ep.foreignEdgeClaimFor(producer, p); !ok {
			unclaimed = append(unclaimed, p)
		}
	}
	return unclaimed, nil
}

// ForeignEdgeMode returns the EdgeMode of the ForeignEdgeClaim covering a
// foreign-subject triple emitted by `producer` carrying `predicate` — the
// ADR-056 Decision-4 seam consumer's mode branch (NoBirthStub→stub,
// Strict→drop, Conditional/Backfill→buffer). ok=false means the edge is
// UNCLAIMED (no covering claim — the deprecated-on-arrival hatch). An empty/
// absent registry is not an error: nothing is claimed yet, so ok=false. Reuses
// the deterministic epoch.foreignEdgeClaimFor (an exact-producer claim wins; a
// Producer-empty "any producer" claim is the fallback), one epoch read per call
// (production foreign-edge volume is zero today; see the type doc).
func (cr *ClaimReader) ForeignEdgeMode(ctx context.Context, producer, predicate string) (EdgeMode, bool, error) {
	entry, err := cr.claims.Get(ctx, registryKey)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return "", false, nil // empty registry: nothing claimed yet → unclaimed
		}
		return "", false, fmt.Errorf("ownership: read epoch for foreign-edge mode: %w", err)
	}
	ep, err := decodeEpoch(entry.Value)
	if err != nil {
		return "", false, err
	}
	claim, ok := ep.foreignEdgeClaimFor(producer, predicate)
	if !ok {
		return "", false, nil
	}
	return claim.Mode, true, nil
}

// OwnerOf returns the owner id and the per-process incarnation nonce recorded
// at RegisterOwner time for the OwnerClaim that, in an OWNING mode, governs
// (entityID, predicate) — the read half of the ADR-056 write-time lease check.
// ok=true means an owning claim was found; the caller (PR-3) compares the
// returned (owner, incarnation) pair against the request's OwnerToken
// "<owner>#<incarnation>" to detect a revived-stale writer.
//
// ok=false is returned (not an error) in three normal cases:
//   - the predicate is unclaimed (no owning OwnerClaim covers it);
//   - the entity does not match any registered pattern;
//   - the registry is empty / absent (no RegisterOwner has run yet).
//
// The structure mirrors ForeignEdgeMode exactly: one epoch read per call,
// ErrKVKeyNotFound treated as empty-registry (not an error), any other Get
// error wrapped and returned.
//
// NOTE for PR-3: a returned incarnation MAY be "" even when ok=true — an owning
// claim registered by a legacy / pre-fence owner carries no incarnation
// (OwnerClaim.Incarnation is omitempty). The lease check must handle this
// deliberately: a naive "<owner>#<incarnation>" string compare would never match
// a fenced request token against an unfenced live owner. The consistent reading
// is the two-state contract's fail-open — an empty live incarnation means the
// lease is not enforceable for that owner, so SKIP the reject; do NOT fail-closed
// and reject every legitimate write against a legacy owner.
func (cr *ClaimReader) OwnerOf(ctx context.Context, entityID, predicate string) (owner, incarnation string, ok bool, err error) {
	entry, err := cr.claims.Get(ctx, registryKey)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return "", "", false, nil // empty registry: nothing claimed yet
		}
		return "", "", false, fmt.Errorf("ownership: read epoch for OwnerOf: %w", err)
	}
	ep, err := decodeEpoch(entry.Value)
	if err != nil {
		return "", "", false, err
	}
	o, inc, found := ep.ownerWithIncarnationOf(entityID, predicate)
	if !found {
		return "", "", false, nil
	}
	return o, inc, true, nil
}

// dedupeStrings returns the sorted, de-duplicated set of non-empty strings.
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sortStrings(out)
	return out
}

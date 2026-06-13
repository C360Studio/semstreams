package ownership

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
)

// Registry is the KV-backed owner registry (ADR-056 Decision 2): a single
// `_registry` epoch key in OWNER_CLAIMS advanced under CAS, plus a separate
// OWNER_PRESENCE heartbeat bucket for stale-owner compaction.
//
// This is the distributed-enforcement SUBSTRATE only. Owners do not hand-build
// claims here as a parallel registry — claims are DERIVED from registered graph
// projection contracts and bound to an owner id at boot (ADR-056 Decision 6);
// RegisterOwner is the low-level entrypoint the derivation calls (and the
// escape hatch for owners with dynamic patterns, e.g. the lifecycle Manager).
type Registry struct {
	claims   *natsclient.KVStore // OWNER_CLAIMS — the single `_registry` epoch key
	presence *natsclient.KVStore // OWNER_PRESENCE — heartbeat.<owner> keys (TTL = grace window)
	logger   *slog.Logger
}

// NewRegistry constructs a Registry over the two pre-opened KV stores. The
// caller (graph-ingest boot, a later increment) creates the buckets:
// OWNER_CLAIMS with history for audit, and — critically — OWNER_PRESENCE with a
// bucket TTL that IS the compaction grace window. That TTL must be
// ttl_hint ≥ 3×max(boot_time, gc_pause_budget) so a single missed heartbeat
// never evicts a live owner (compactStale treats presence-key absence as
// "silent beyond grace"; the TTL is what makes absence mean that).
func NewRegistry(claims, presence *natsclient.KVStore, logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{claims: claims, presence: presence, logger: logger}
}

// Registration is one owner's full set of claims, registered atomically against
// the epoch.
type Registration struct {
	Owner        string
	Claims       []OwnerClaim
	ForeignEdges []ForeignEdgeClaim
	Waivers      []CoordinationWaiver
}

// Validate checks structural well-formedness AND internal consistency: every
// claim names the registering owner, and no two of the registration's OWN
// owning claims select an overlapping (pattern, predicate) cell (a single owner
// declaring the same cell twice in an owning mode is ambiguous — which claim
// reconciles it?). Cross-OWNER overlap is checked separately at RegisterOwner
// against the epoch. Callers that DERIVE a registration (pkg/projection) call
// this at boot for early, owner-bound feedback.
func (r Registration) Validate() error {
	if err := r.validateStructural(); err != nil {
		return err
	}
	if err := r.selfOverlap(); err != nil {
		return err
	}
	return r.modeConsistency()
}

// modeConsistency rejects a predicate declared in BOTH an owning mode and
// append-evidence over INTERSECTING patterns within one registration — "P is
// single-valued owned" and "P is multi-valued append" cannot both hold for one
// owner on the same entities. Disjoint patterns are fine (P may be owned on one
// entity type and appended on another). This catches across the aggregated
// claims of pkg/projection.Derive, where the per-contract one-mode check can't
// see the other contract.
func (r Registration) modeConsistency() error {
	for i := range r.Claims {
		for j := i + 1; j < len(r.Claims); j++ {
			a, b := r.Claims[i], r.Claims[j]
			if a.Mode.isOwning() == b.Mode.isOwning() {
				continue // both owning (selfOverlap's job) or both append (legitimate)
			}
			if !patternsIntersect(a.Pattern, b.Pattern) {
				continue
			}
			if hit := predicatesIntersection(a.Predicates, b.Predicates); len(hit) > 0 {
				return fmt.Errorf("%w: owner %q declares predicate(s) %v in both an owning and an append-evidence mode over overlapping patterns %q / %q",
					ErrInvalidClaim, r.Owner, hit, a.Pattern, b.Pattern)
			}
		}
	}
	return nil
}

// selfOverlap reports an *OverlapError (Owner == With == r.Owner) when two of the
// registration's own owning OwnerClaims select an overlapping cell.
func (r Registration) selfOverlap() error {
	for i := range r.Claims {
		a := r.Claims[i]
		if !a.Mode.isOwning() {
			continue
		}
		for j := i + 1; j < len(r.Claims); j++ {
			b := r.Claims[j]
			if !b.Mode.isOwning() {
				continue
			}
			if !patternsIntersect(a.Pattern, b.Pattern) {
				continue
			}
			if hit := predicatesIntersection(a.Predicates, b.Predicates); len(hit) > 0 {
				return &OverlapError{
					Owner: r.Owner, With: r.Owner,
					Pattern: a.Pattern, WithPattern: b.Pattern,
					Predicates: hit,
				}
			}
		}
	}
	return nil
}

func (r Registration) validateStructural() error {
	if !validOwnerID(r.Owner) {
		return fmt.Errorf("%w: registration owner %q is empty or not subject-safe", ErrInvalidClaim, r.Owner)
	}
	if len(r.Claims) == 0 && len(r.ForeignEdges) == 0 {
		return fmt.Errorf("%w: registration by %q declares no claims", ErrInvalidClaim, r.Owner)
	}
	for _, c := range r.Claims {
		if err := c.Validate(); err != nil {
			return err
		}
		if c.Owner != r.Owner {
			return fmt.Errorf("%w: claim owner %q != registration owner %q", ErrInvalidClaim, c.Owner, r.Owner)
		}
	}
	for _, f := range r.ForeignEdges {
		if err := f.Validate(); err != nil {
			return err
		}
		if f.Owner != r.Owner {
			return fmt.Errorf("%w: foreign-edge owner %q != registration owner %q", ErrInvalidClaim, f.Owner, r.Owner)
		}
	}
	for _, w := range r.Waivers {
		if err := w.Validate(); err != nil {
			return err
		}
		if w.Owner != r.Owner {
			return fmt.Errorf("%w: waiver owner %q != registration owner %q", ErrInvalidClaim, w.Owner, r.Owner)
		}
	}
	return nil
}

// hasNoEnforceableClaim reports whether the registration consists ENTIRELY of
// non-owning (append-evidence) OwnerClaims and has no foreign edges — a registration the
// registry will store but never act on (no overlap protection, no lease). That
// is almost always a misconfiguration (an owner that meant to own a group but
// fluffed the mode), so RegisterOwner logs a Warn rather than silently
// accepting a no-op.
func (r Registration) hasNoEnforceableClaim() bool {
	if len(r.ForeignEdges) > 0 {
		return false
	}
	for _, c := range r.Claims {
		if c.Mode.isOwning() {
			return false
		}
	}
	return true
}

// RegisterOwner registers (or re-registers, idempotently) an owner's claims
// against the single epoch key. Inside one UpdateWithRetry CAS callback
// (ADR-056 Decision 2): read epoch → compact stale owners by presence → drop
// the registrant's prior entry → update the registrant's half of the waiver set
// → check overlap of the candidate against every OTHER owner → on overlap FAIL
// (non-retryable), else merge + bump epoch → CAS-write at the read revision →
// retry on a concurrent registrant's write.
//
// Returns a *OverlapError (errors.Is(err, ErrOwnershipOverlap)) on collision,
// or ErrInvalidClaim on a malformed registration.
func (reg *Registry) RegisterOwner(ctx context.Context, r Registration) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.hasNoEnforceableClaim() {
		reg.logger.Warn("ownership: registration has no enforceable (owning or foreign-edge) claim — it will not be protected",
			slog.String("owner", r.Owner))
	}

	// Heartbeat first: mark this owner live BEFORE the CAS reads presence for
	// compaction, so a concurrent registrant cannot see us as stale mid-flight.
	if err := reg.Heartbeat(ctx, r.Owner); err != nil {
		return fmt.Errorf("ownership: heartbeat before register %q: %w", r.Owner, err)
	}

	var (
		overlapErr error
		preExisted bool
		cand       = ownerEntry{Claims: r.Claims, ForeignEdges: r.ForeignEdges}
	)
	err := reg.claims.UpdateWithRetry(ctx, registryKey, func(current []byte) ([]byte, error) {
		ep, err := decodeEpoch(current)
		if err != nil {
			return nil, retry.NonRetryable(err)
		}
		_, preExisted = ep.Owners[r.Owner]

		live, err := reg.livePresence(ctx)
		if err != nil {
			return nil, err // retryable: a transient presence-read blip
		}
		if evicted := ep.compactStale(r.Owner, live); len(evicted) > 0 {
			reg.logger.Warn("ownership: compacted stale owner claims",
				slog.String("registrant", r.Owner),
				slog.Any("evicted", evicted))
		}

		// Idempotent re-registration: drop the registrant's prior entry so the
		// overlap check never compares the candidate against itself, and update
		// only the registrant's half of the epoch-scoped waiver set.
		delete(ep.Owners, r.Owner)
		ep.setWaiversFor(r.Owner, r.Waivers)

		if oerr := checkOverlap(r.Owner, cand, ep.Owners, ep.Waivers); oerr != nil {
			overlapErr = oerr
			return nil, retry.NonRetryable(oerr) // never retry an overlap — it would just re-detect
		}

		ep.Owners[r.Owner] = cand
		ep.Version++
		return ep.encode()
	})

	if overlapErr != nil {
		reg.rollbackPresence(ctx, r.Owner, preExisted)
		return overlapErr // clean *OverlapError for errors.As, not the retry-wrapped form
	}
	if err != nil {
		reg.rollbackPresence(ctx, r.Owner, preExisted)
		return fmt.Errorf("ownership: register %q: %w", r.Owner, err)
	}
	return nil
}

// rollbackPresence drops the heartbeat key written by a registration that then
// failed — but ONLY for an owner that had no prior epoch entry. A re-registration
// failure must NOT resign a still-live owner whose existing claims remain in the
// epoch (that would get them compacted on the next pass). Best-effort.
func (reg *Registry) rollbackPresence(ctx context.Context, owner string, preExisted bool) {
	if preExisted {
		return
	}
	if err := reg.Resign(ctx, owner); err != nil {
		reg.logger.Warn("ownership: failed to roll back presence after registration failure",
			slog.String("owner", owner), slog.Any("error", err))
	}
}

// Heartbeat (re)writes the owner's presence key, refreshing the bucket TTL. The
// caller runs this on a ticker (interval well under the TTL); a crashed owner
// stops, its key TTL-expires, and the next registrant compacts its claims. The
// value is the heartbeat unix-nanos timestamp, carried for observability and
// the later Watch-revival check.
func (reg *Registry) Heartbeat(ctx context.Context, owner string) error {
	if !validOwnerID(owner) {
		return fmt.Errorf("%w: heartbeat owner %q not subject-safe", ErrInvalidClaim, owner)
	}
	key := presenceKeyPrefix + owner
	val := []byte(strconv.FormatInt(time.Now().UnixNano(), 10))
	if _, err := reg.presence.Put(ctx, key, val); err != nil {
		return fmt.Errorf("ownership: heartbeat %q: %w", owner, err)
	}
	return nil
}

// Resign deletes the owner's presence key, voluntarily releasing its claims at
// the next registrant's compaction (clean shutdown). Best-effort; a missing key
// is not an error.
func (reg *Registry) Resign(ctx context.Context, owner string) error {
	key := presenceKeyPrefix + owner
	if err := reg.presence.Delete(ctx, key); err != nil && !errors.Is(err, natsclient.ErrKVKeyNotFound) {
		return fmt.Errorf("ownership: resign %q: %w", owner, err)
	}
	return nil
}

// OwnerOf returns the owner of the (entityID, predicate) cell — the write-time
// lease lookup a mutation handler runs to verify a writer's owner identity
// against the live owner (ADR-056 Decision 2 write seam). The returned owner id
// IS the lease handle (no hash — identity is exact). ok is false when no owner
// claims that cell (un-claimed or append-evidence).
func (reg *Registry) OwnerOf(ctx context.Context, entityID, predicate string) (owner string, ok bool, err error) {
	entry, err := reg.claims.Get(ctx, registryKey)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return "", false, nil // empty registry: nothing is owned yet
		}
		return "", false, fmt.Errorf("ownership: read epoch for OwnerOf: %w", err)
	}
	ep, err := decodeEpoch(entry.Value)
	if err != nil {
		return "", false, err
	}
	o, found := ep.ownerOf(entityID, predicate)
	if !found {
		return "", false, nil
	}
	return o, true, nil
}

// ForeignEdgeClaimFor returns the ForeignEdgeClaim covering a foreign-subject
// triple emitted by a Graphable of `messageType` carrying `predicate` — the
// T2-regroup seam reject lookup (ADR-056 Decision 4). ok=false means the foreign
// edge is UNCLAIMED, which the seam rejects (or routes deprecated-on-arrival
// with the foreign_edge_unclaimed_total metric until the producer migrates).
func (reg *Registry) ForeignEdgeClaimFor(ctx context.Context, messageType, predicate string) (ForeignEdgeClaim, bool, error) {
	entry, err := reg.claims.Get(ctx, registryKey)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return ForeignEdgeClaim{}, false, nil // empty registry: nothing claimed yet
		}
		return ForeignEdgeClaim{}, false, fmt.Errorf("ownership: read epoch for ForeignEdgeClaimFor: %w", err)
	}
	ep, err := decodeEpoch(entry.Value)
	if err != nil {
		return ForeignEdgeClaim{}, false, err
	}
	c, ok := ep.foreignEdgeClaimFor(messageType, predicate)
	return c, ok, nil
}

// livePresence returns the set of live owner ids — each OWNER_PRESENCE key minus
// its prefix. Liveness is the canonical owner id (no hash); compactStale tests
// membership directly. Keys() ignores deletes/expiry, so an absent key is a
// genuinely silent (beyond-TTL-grace) owner.
func (reg *Registry) livePresence(ctx context.Context) (map[string]struct{}, error) {
	keys, err := reg.presence.Keys(ctx)
	if err != nil {
		return nil, fmt.Errorf("ownership: list presence: %w", err)
	}
	live := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		live[strings.TrimPrefix(k, presenceKeyPrefix)] = struct{}{}
	}
	return live, nil
}

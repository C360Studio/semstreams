package ownership

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
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

	// incarnation is the per-process boot nonce (8 bytes crypto/rand hex). It
	// is generated once at NewRegistry time and is stable for the lifetime of
	// the process. Combined with the owner id it forms the OwnerToken
	// "<owner>#<incarnation>" written on every update_with_triples /
	// create_with_triples request (ADR-056 PR-1). The graph-ingest lease check
	// (a later increment) compares this token against the live claim's
	// incarnation to reject a revived-stale writer that re-registered the same
	// owner id in a new process without the token changing.
	incarnation string

	// inverseResolver, when non-nil, enforces the Decision-4 inverse-gate over
	// every registered ForeignEdgeClaim (a Conditional/Backfill edge predicate
	// must have a registered inverse). Injected so pkg/ownership stays free of
	// the vocabulary dependency; set by EnsureBuckets. nil = gate skipped (with a
	// one-time WARN if a claim would have been gated) — the observe-only / read-
	// only / test default.
	inverseResolver    InverseResolver
	noResolverWarnOnce sync.Once

	// registeredMu guards registered. registered is the set of owner ids THIS
	// process successfully registered via RegisterOwner — the owners
	// WatchRevival monitors for eviction-then-supersession (ADR-056 PR-4).
	// Populated on each successful RegisterOwner; never pruned (an owner this
	// process claims, it owns for its lifetime — losing the claim is exactly the
	// revival condition WatchRevival exists to catch).
	registeredMu sync.Mutex
	registered   map[string]struct{}

	// quiescedMu guards quiesced. quiesced is the set of owner ids this process
	// has QUIESCED after WatchRevival detected they were superseded by a
	// different incarnation (ADR-056 PR-4). Latching and terminal per owner: once
	// a rival incarnation holds our owner, this process must never resume
	// authoritative writes for it. Producers consult IsQuiesced before writing.
	quiescedMu sync.RWMutex
	quiesced   map[string]struct{}

	// absentWarnedMu guards absentWarned — owners WatchRevival has already
	// WARNed about being absent from the live epoch (compacted with no
	// successor). De-dupes the warn to once per owner so a persistently-absent
	// owner does not re-log on every epoch update (ADR-056 PR-4).
	absentWarnedMu sync.Mutex
	absentWarned   map[string]struct{}

	// revivalUpdates, when non-nil, receives a signal after WatchRevival fully
	// processes each epoch update. Test-only synchronization seam (set via
	// setRevivalUpdates) so integration tests await detection deterministically
	// rather than sleeping; nil in production. Non-blocking buffered send.
	revivalUpdates chan<- struct{}
}

// NewRegistry constructs a Registry over the two pre-opened KV stores. The
// caller (graph-ingest boot, a later increment) creates the buckets:
// OWNER_CLAIMS with history for audit, and — critically — OWNER_PRESENCE with a
// bucket TTL that IS the compaction grace window. That TTL must be
// ttl_hint ≥ 3×max(boot_time, gc_pause_budget) so a single missed heartbeat
// never evicts a live owner (compactStale treats presence-key absence as
// "silent beyond grace"; the TTL is what makes absence mean that).
//
// A per-process incarnation nonce (8 bytes, crypto/rand hex) is generated here
// and stored for the lifetime of the Registry. It forms the incarnation half of
// every OwnerToken ("<owner>#<incarnation>") stamped on outgoing mutation
// requests (ADR-056 PR-1). Callers access it via Registry.Incarnation().
func NewRegistry(claims, presence *natsclient.KVStore, logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is extremely rare and almost always indicates a
		// broken OS RNG. Panic rather than silently stamping a zero-token that
		// would make every token identical across processes — defeating the
		// incarnation fence entirely.
		panic(fmt.Sprintf("ownership: generate incarnation nonce: %v", err))
	}
	return &Registry{
		claims:       claims,
		presence:     presence,
		logger:       logger,
		incarnation:  hex.EncodeToString(b),
		registered:   make(map[string]struct{}),
		quiesced:     make(map[string]struct{}),
		absentWarned: make(map[string]struct{}),
	}
}

// Incarnation returns the per-process boot nonce generated at NewRegistry time.
// It is stable for the lifetime of this Registry instance. Together with the
// canonical owner id it forms the OwnerToken "<owner>#<incarnation>" stamped by
// producers on every update_with_triples / create_with_triples request
// (ADR-056 PR-1). The graph-ingest lease-check increment (PR-2+) uses this to
// reject revived-stale writers that re-registered the same owner id in a new
// process without presenting the live incarnation.
func (reg *Registry) Incarnation() string {
	return reg.incarnation
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
	// Decision-4 inverse-gate: a Conditional/Backfill foreign edge with no
	// registered inverse is unrecoverable after a birth race. Enforce before any
	// KV I/O so a gated violation fails the registration cleanly (ErrInvalidClaim
	// — a config bug, fatal at the caller). Skipped (warn-once) when no resolver
	// is wired.
	if err := reg.checkInverseGate(r.ForeignEdges); err != nil {
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

	// Stamp the per-process incarnation onto each claim before writing to the
	// epoch so a later PR's OwnerOf can return the live owner's incarnation
	// for the lease-check comparison (ADR-056 PR-1). We copy the slice
	// rather than mutating the caller's input to preserve Registration
	// immutability at the call site.
	stampedClaims := make([]OwnerClaim, len(r.Claims))
	for i, c := range r.Claims {
		c.Incarnation = reg.incarnation
		stampedClaims[i] = c
	}

	var (
		overlapErr error
		preExisted bool
		cand       = ownerEntry{Claims: stampedClaims, ForeignEdges: r.ForeignEdges}
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

	// Record this owner as one of ours so WatchRevival monitors it for
	// eviction-then-supersession (ADR-056 PR-4). Only on a clean commit — a
	// rejected/overlapping registration holds no claim to revive against.
	reg.registeredMu.Lock()
	reg.registered[r.Owner] = struct{}{}
	reg.registeredMu.Unlock()
	return nil
}

// registeredOwners returns a snapshot copy of the owner ids this process
// registered — the set WatchRevival iterates each epoch update.
func (reg *Registry) registeredOwners() map[string]struct{} {
	reg.registeredMu.Lock()
	defer reg.registeredMu.Unlock()
	out := make(map[string]struct{}, len(reg.registered))
	for o := range reg.registered {
		out[o] = struct{}{}
	}
	return out
}

// IsQuiesced reports whether this process has quiesced authoritative writes for
// owner after WatchRevival detected it was superseded by a different incarnation
// (ADR-056 PR-4). Producers (the lifecycle Manager) consult this before an owned
// write; a quiesced owner must not write — a live rival owns it now.
func (reg *Registry) IsQuiesced(owner string) bool {
	reg.quiescedMu.RLock()
	defer reg.quiescedMu.RUnlock()
	_, ok := reg.quiesced[owner]
	return ok
}

// markQuiesced latches owner as quiesced. Returns true on the 0→1 transition so
// the caller emits the CRITICAL log + metric exactly once; false if already
// quiesced. Latching — an owner never un-quiesces within this process.
func (reg *Registry) markQuiesced(owner string) bool {
	reg.quiescedMu.Lock()
	defer reg.quiescedMu.Unlock()
	if _, already := reg.quiesced[owner]; already {
		return false
	}
	reg.quiesced[owner] = struct{}{}
	return true
}

// checkInverseGate runs the Decision-4 inverse-gate over the registration's
// foreign edges using the Registry's injected resolver. With a resolver, a
// Conditional/Backfill edge predicate lacking a registered inverse FAILS
// (ErrInvalidClaim). With no resolver wired, the gate is skipped — a deploy that
// SHOULD enforce but forgot the resolver is surfaced by a one-time WARN the first
// time a would-be-gated claim is seen, not silently.
func (reg *Registry) checkInverseGate(edges []ForeignEdgeClaim) error {
	if reg.inverseResolver != nil {
		return CheckInverseGate(reg.inverseResolver, edges...)
	}
	for _, e := range edges {
		if e.Mode.requiresInverse() {
			reg.noResolverWarnOnce.Do(func() {
				reg.logger.Warn("ownership: no inverse-resolver wired — Decision-4 inverse-gate SKIPPED for inverse-requiring foreign-edge claims (wire one via EnsureBuckets to enforce)")
			})
			break
		}
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

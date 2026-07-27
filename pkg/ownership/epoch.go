package ownership

import (
	"encoding/json"
	"fmt"
)

// ownerEntry is one owner's slice of the epoch: its owned-state claims and its
// foreign-edge claims. Waivers are NOT nested here — they live at epoch scope
// (epoch.Waivers) so the compaction of either party to a waiver does not
// silently drop it.
type ownerEntry struct {
	Claims       []OwnerClaim       `json:"claims,omitempty"`
	ForeignEdges []ForeignEdgeClaim `json:"foreign_edges,omitempty"`
}

// epoch is the single value stored at the `_registry` key in OWNER_CLAIMS — the
// UNION of every registered owner's claims, advanced under UpdateWithRetry CAS.
// Version is a monotonic counter bumped on every successful registration so a
// compacted-then-revived owner's post-registration Watch (a later increment)
// can detect that the epoch moved (Decision 2). The KV revision is the CAS
// token; Version is the human/audit-facing epoch number.
//
// Waivers live at epoch scope (not per-owner) so neither party's compaction
// drops them; they lapse only on explicit re-registration of their declarer or
// on expiry (ReviewBy, deferred).
type epoch struct {
	Version uint64                `json:"version"`
	Owners  map[string]ownerEntry `json:"owners"`
	Waivers []CoordinationWaiver  `json:"waivers,omitempty"`
}

func newEpoch() *epoch {
	return &epoch{Owners: make(map[string]ownerEntry)}
}

// decodeEpoch parses the epoch value. An empty/absent value (first
// registration) decodes to a fresh empty epoch, not an error.
func decodeEpoch(b []byte) (*epoch, error) {
	if len(b) == 0 {
		return newEpoch(), nil
	}
	var e epoch
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("ownership: decode epoch: %w", err)
	}
	if e.Owners == nil {
		e.Owners = make(map[string]ownerEntry)
	}
	return &e, nil
}

func (e *epoch) encode() ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("ownership: encode epoch: %w", err)
	}
	return b, nil
}

// setWaiversFor replaces the declarer's half of the epoch's waiver set: drop
// every waiver previously declared by `declarer`, then append its fresh set.
// Idempotent re-registration thus updates a declarer's waivers without
// disturbing the other party's half (mutual consent is preserved across
// re-registrations).
func (e *epoch) setWaiversFor(declarer string, waivers []CoordinationWaiver) {
	kept := e.Waivers[:0:0]
	for _, w := range e.Waivers {
		if w.Owner != declarer {
			kept = append(kept, w)
		}
	}
	e.Waivers = append(kept, waivers...)
}

// compactStale removes OWNING entries absent from live presence. Durable
// non-owning entries are exempt because they intentionally have no presence.
// The registrant is always exempt; an owning registrant heartbeats before the
// CAS, and the explicit exemption also protects against a presence-read
// anomaly. For owning entries, an absent key means silence beyond the
// bucket-TTL grace window. Returns evicted owner ids for logging.
func (e *epoch) compactStale(registrant string, livePresence map[string]struct{}) []string {
	var evicted []string
	for owner, entry := range e.Owners {
		if owner == registrant {
			continue
		}
		if _, live := livePresence[owner]; live {
			continue
		}
		// Every non-empty entry without replace-owned/CAS claims is a durable
		// non-owning declaration. It deliberately has no presence key, so
		// liveness compaction must exempt append-only, foreign-edge-only, and
		// combined append/foreign entries. Empty entries remain compactable as
		// defense-in-depth. A mixed entry containing any owning claim is one
		// atomic lease and is removed in full when stale.
		if !containsOwningClaims(entry.Claims) &&
			(len(entry.Claims) > 0 || len(entry.ForeignEdges) > 0) {
			continue
		}
		evicted = append(evicted, owner)
	}
	for _, owner := range evicted {
		delete(e.Owners, owner)
	}
	sortStrings(evicted)
	return evicted
}

// foreignEdgeClaimFor returns the ForeignEdgeClaim covering a foreign-subject
// triple emitted by `messageType` carrying `predicate` — the T2-seam reject
// lookup (ADR-056 Decision 4). An exact Producer match wins; a Producer-empty
// ("any producer", transitional) claim is the fallback. ok=false means the edge
// is UNCLAIMED (the seam rejects / routes deprecated-on-arrival).
//
// Owners are scanned in SORTED order so the returned claim is DETERMINISTIC.
// FE×FE is allowed (two producers may both claim a predicate), so >1 claim can
// match; the allow/deny (ok) is order-immaterial, but the returned claim's Mode
// is what the seam consumer branches on (Conditional→buffer, Strict→drop,
// NoBirthStub→stub), so a stable pick matters.
func (e *epoch) foreignEdgeClaimFor(messageType, predicate string) (ForeignEdgeClaim, bool) {
	var anyProducer ForeignEdgeClaim
	haveAny := false
	for _, owner := range sortedOwners(e.Owners) {
		for _, f := range e.Owners[owner].ForeignEdges {
			if f.Predicate != predicate {
				continue
			}
			if f.Producer == messageType {
				return f, true // exact producer match wins
			}
			if f.Producer == "" && !haveAny {
				anyProducer, haveAny = f, true
			}
		}
	}
	if haveAny {
		return anyProducer, true
	}
	return ForeignEdgeClaim{}, false
}

// ownerOf returns the owner id of the OwnerClaim that, in an OWNING mode,
// governs (entityID, predicate) — the write-time lease lookup. ok is false when
// no owner claims that cell (an un-claimed predicate, or an append-evidence
// predicate). The first owning match wins; the overlap check guarantees at most
// one owning owner per cell, so the order is immaterial for a well-formed
// epoch.
func (e *epoch) ownerOf(entityID, predicate string) (owner string, ok bool) {
	o, _, found := e.ownerWithIncarnationOf(entityID, predicate)
	return o, found
}

// ownerWithIncarnationOf is the full-detail sibling of ownerOf: it returns the
// owner id AND the Incarnation nonce recorded at RegisterOwner time for the
// matching OwnerClaim. The incarnation is the "<owner>#<incarnation>" token
// half that a later PR's write-time lease check compares against the incoming
// request's OwnerToken (ADR-056 PR-2). ok is false when no owning claim covers
// (entityID, predicate) — an un-claimed predicate or append-evidence cell.
// Owners are iterated in SORTED order (matching foreignEdgeClaimFor) so the
// result is deterministic — defense-in-depth for a lease lookup that feeds a
// reject decision, should a malformed epoch ever carry two owning owners for a
// cell; the overlap check guarantees at most one in a well-formed epoch.
// incarnationOfOwner returns the incarnation recorded on the FIRST owning
// OwnerClaim of `owner` in this epoch (ADR-056 PR-4 revival detection).
// present=false means the owner has no OWNING claim in the live epoch — it was
// compacted with no successor, or it holds only foreign-edge / append-evidence
// claims (neither carries a write-fence incarnation). A well-formed epoch stamps
// the SAME incarnation on every one of an owner's claims (RegisterOwner stamps
// reg.incarnation across the whole set), so the first owning claim's incarnation
// is the owner's incarnation. The returned incarnation may be "" for a
// legacy/pre-fence claim — the caller treats "" as not-superseding (no rival
// incarnation to compare).
func (e *epoch) incarnationOfOwner(owner string) (incarnation string, present bool) {
	for _, c := range e.Owners[owner].Claims {
		if c.Mode.isOwning() {
			return c.Incarnation, true
		}
	}
	return "", false
}

func (e *epoch) ownerWithIncarnationOf(entityID, predicate string) (owner, incarnation string, ok bool) {
	for _, ownerID := range sortedOwners(e.Owners) {
		for _, c := range e.Owners[ownerID].Claims {
			if !c.Mode.isOwning() {
				continue
			}
			if !patternMatches(c.Pattern, entityID) {
				continue
			}
			for _, p := range c.Predicates {
				if p == predicate {
					return ownerID, c.Incarnation, true
				}
			}
		}
	}
	return "", "", false
}

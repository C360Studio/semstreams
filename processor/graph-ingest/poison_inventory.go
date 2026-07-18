package graphingest

// poison_inventory.go — the per-entity poison inventory (poison-response-scoping
// D2/D3/D6). The inventory replaces the surface-global sticky latch: it is
// OBSERVABILITY-ONLY — no read or write path consults it for a decision.
// Refusal derives solely from decoding the bytes actually stored at each seam;
// the inventory exists so operators get an alertable, enumerable, self-healing
// signal (Health degraded + gauge + DebugStatus) instead of a whole-surface
// outage.
//
// Each record carries the KV revision whose decode failed. Clear paths (D3):
//
//	(a) DeleteEntity — the poisoned bytes are gone;
//	(b) a successful commit to the key at a revision newer than the recorded
//	    one — the revision guard closes the record-after-clear interleaving
//	    (concurrent repair Put vs in-flight RMW classification);
//	(c) any successful validating read of the key — the read already ran the
//	    canonical decode, so an out-of-band repair recovers Health for free.
//
// Steady-state cost with an empty inventory is a single atomic load on every
// commit/read hot path (design D2, required).

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus"
)

// entityPoisonSampleCap bounds entity-ID lists in operator-facing messages
// (the Health message sample and aggregate-read failures). Full enumeration is
// available via DebugStatus. Proposed constant per the change's open questions.
const entityPoisonSampleCap = 10

// bootSweepErrorLogCap bounds individually-logged boot-sweep poison ERRORs;
// past it a single count-only WARN summarizes the remainder so a mass-poison
// event cannot flood the log. Proposed constant per the change's open questions.
const bootSweepErrorLogCap = 100

// entityPoisonRecord is one inventoried poisoned entity: the stamped typed
// error plus the KV revision whose decode failed.
type entityPoisonRecord struct {
	contractErr *graph.StateContractError
	revision    uint64
}

var (
	poisonedEntitiesOnce  sync.Once
	poisonedEntitiesGauge prometheus.Gauge
)

// getPoisonedEntitiesMetric returns the process-wide poison-inventory gauge.
// One gauge, never per-entity labels (cardinality discipline, design D6):
// enumeration belongs to DebugStatus, alerting belongs to this gauge plus the
// Health message.
func getPoisonedEntitiesMetric(registry *metric.MetricsRegistry) prometheus.Gauge {
	poisonedEntitiesOnce.Do(func() {
		poisonedEntitiesGauge = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "poisoned_entities",
			Help:      "Current per-entity poison inventory size (ENTITY_STATES values failing the canonical decode). Alert on this gauge + the Health message; enumerate via the component debug surface.",
		})
		if registry != nil {
			_ = registry.RegisterGauge("graph-ingest", "poisoned_entities", poisonedEntitiesGauge)
		} else {
			_ = prometheus.DefaultRegisterer.Register(poisonedEntitiesGauge)
		}
	})
	return poisonedEntitiesGauge
}

// setEntityPoisonGauge publishes the inventory size to the gauge. Callers hold
// entityPoisonMu (len is read under the lock). Nil-guarded for hand-built test
// components.
func (c *Component) setEntityPoisonGauge() {
	if c.poisonedEntities != nil {
		c.poisonedEntities.Set(float64(len(c.entityPoison)))
	}
}

// recordEntityPoison inserts or refreshes an inventory record keyed by the
// stamped EntityID. Returns true when the entity was NEWLY inventoried (map
// presence is the once-per-entity log dedup). A re-record of an already
// inventoried entity keeps the newest failing revision and does not re-log.
// On a NEW record the entity's query-cache entry is invalidated (hygiene,
// mirroring the write-path invalidations — cached responses must not outlive
// detection; this is not an enforcement use of the inventory).
func (c *Component) recordEntityPoison(contractErr *graph.StateContractError, revision uint64) bool {
	if contractErr == nil || contractErr.EntityID == "" {
		return false // unstampable — enforcement still holds at every per-read decode
	}
	id := contractErr.EntityID

	c.entityPoisonMu.Lock()
	if c.entityPoison == nil {
		c.entityPoison = make(map[string]entityPoisonRecord)
	}
	existing, present := c.entityPoison[id]
	if present {
		if revision >= existing.revision {
			c.entityPoison[id] = entityPoisonRecord{contractErr: contractErr, revision: revision}
		}
		c.entityPoisonMu.Unlock()
		return false
	}
	c.entityPoison[id] = entityPoisonRecord{contractErr: contractErr, revision: revision}
	c.entityPoisonSize.Store(int64(len(c.entityPoison)))
	c.setEntityPoisonGauge()
	c.entityPoisonMu.Unlock()

	c.invalidateEntityCacheEntry(id)
	return true
}

// logEntityPoisonRecorded emits the once-per-entity structured ERROR for a new
// inventory record. Kept separate from recordEntityPoison so the boot sweep
// can apply its own log cap.
func (c *Component) logEntityPoisonRecorded(contractErr *graph.StateContractError, revision uint64) {
	if c.logger == nil {
		return
	}
	c.logger.Error("poisoned entity inventoried; per-entity refusal until repaired (delete + recreate)",
		slog.String("code", graph.ErrorCodeGraphStateResetRequired),
		slog.String("entity_id", contractErr.EntityID),
		slog.String("reason", string(contractErr.Reason)),
		slog.Uint64("revision", revision))
}

// inventoryEntityPoison is the standard record path for detection sites that
// hold the failing entry's revision (query lanes, mutation read seams). It
// records, logs once on a new record, and then verifies the record against the
// key's CURRENT bytes — closing the last record/clear interleaving: a repair
// (delete or commit) that completed entirely between this detection's read and
// its record found no entry to clear, so without the verify the fresh record
// would strand a spurious degraded-Health entry until the key's next touch.
func (c *Component) inventoryEntityPoison(ctx context.Context, contractErr *graph.StateContractError, revision uint64) {
	if c.recordEntityPoison(contractErr, revision) {
		c.logEntityPoisonRecorded(contractErr, revision)
		c.verifyEntityPoisonRecord(ctx, contractErr.EntityID)
	}
}

// verifyEntityPoisonRecord re-reads a just-recorded key and drops the record
// if the CURRENT bytes prove repair: absent key (repaired by delete) or bytes
// that validate (repaired by commit or out-of-band overwrite, at that
// revision — the revision guard keeps a re-poison newer than the proof). A
// transient KV read error keeps the record; it self-heals on the key's next
// touch (D3c). One extra KV Get, only on the already-failing detection path.
func (c *Component) verifyEntityPoisonRecord(ctx context.Context, entityID string) {
	if c.entityBucket == nil {
		return
	}
	entry, err := c.entityBucket.Get(ctx, entityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			c.dropEntityPoisonRecord(entityID, 0) // bytes are gone — repaired by delete
		}
		return // transient read error: keep the record; next touch resolves it
	}
	var probe graph.EntityState
	if graph.UnmarshalEntityState(entry.Value, &probe) == nil {
		c.dropEntityPoisonRecord(entityID, entry.Revision)
	}
}

// inventoryEntityPoisonAtCurrentRevision is the record path for RMW
// classifications: the CAS closures hold the failing bytes but not the
// revision they were read at, so re-read the key. If its CURRENT bytes now
// validate, a concurrent repair won the race — record nothing and drop any
// stale entry, so the record-after-clear interleaving ends with no entry (D3).
// Otherwise record at the current revision. Only reached on the already-failing
// slow path.
func (c *Component) inventoryEntityPoisonAtCurrentRevision(ctx context.Context, contractErr *graph.StateContractError) {
	if contractErr == nil || contractErr.EntityID == "" || c.entityBucket == nil {
		return
	}
	entry, err := c.entityBucket.Get(ctx, contractErr.EntityID)
	if err != nil {
		// Key absent (concurrent repair-by-delete): nothing resident to
		// inventory. A TRANSIENT read error also lands here and skips this
		// detection's record — acceptable, the key's next touch records it.
		return
	}
	var probe graph.EntityState
	if graph.UnmarshalEntityState(entry.Value, &probe) == nil {
		c.dropEntityPoisonRecord(contractErr.EntityID, entry.Revision)
		return
	}
	c.inventoryEntityPoison(ctx, contractErr, entry.Revision)
}

// dropEntityPoisonRecord removes an inventory entry. proofRev == 0 clears
// unconditionally (the delete path — the bytes are gone). A non-zero proofRev
// is the revision at which the key was PROVEN valid (a committed write or a
// validating read); the entry is kept when its recorded poisoned revision is
// newer than the proof — a re-poison after the proof must not be erased. A
// revision's bytes are immutable, so proofRev == recorded means the recorded
// revision validated and the entry is stale.
func (c *Component) dropEntityPoisonRecord(entityID string, proofRev uint64) {
	c.entityPoisonMu.Lock()
	defer c.entityPoisonMu.Unlock()
	rec, ok := c.entityPoison[entityID]
	if !ok {
		return
	}
	if proofRev != 0 && rec.revision > proofRev {
		return
	}
	delete(c.entityPoison, entityID)
	c.entityPoisonSize.Store(int64(len(c.entityPoison)))
	c.setEntityPoisonGauge()
}

// clearEntityPoisonOnDelete is clear path (a): a successful DeleteEntity
// removed the poisoned bytes. Steady-state cost is the single atomic load.
func (c *Component) clearEntityPoisonOnDelete(entityID string) {
	if c.entityPoisonSize.Load() == 0 {
		return
	}
	c.dropEntityPoisonRecord(entityID, 0)
}

// clearEntityPoisonOnCommit is clear path (b): a write-gate-validated commit
// to the key proves repair when its revision is newer than the recorded one.
// committedRev == 0 means the commit lane does not surface its revision
// (UpdateWithRetry); resolve by re-reading and re-validating the key — reached
// only when this exact entity is inventoried, never on the healthy hot path,
// which exits on the first atomic load (design D2's required fast path).
func (c *Component) clearEntityPoisonOnCommit(ctx context.Context, entityID string, committedRev uint64) {
	if c.entityPoisonSize.Load() == 0 {
		return // steady-state: one atomic load, mutex untouched
	}
	c.entityPoisonMu.Lock()
	_, ok := c.entityPoison[entityID]
	c.entityPoisonMu.Unlock()
	if !ok {
		return
	}
	if committedRev == 0 {
		entry, err := c.entityBucket.Get(ctx, entityID)
		if err != nil {
			return // cannot prove newness; the entry self-heals on next touch (D3c)
		}
		var probe graph.EntityState
		if graph.UnmarshalEntityState(entry.Value, &probe) != nil {
			return // current bytes are still (or again) poison — keep the record
		}
		committedRev = entry.Revision
	}
	c.dropEntityPoisonRecord(entityID, committedRev)
}

// clearEntityPoisonOnValidRead is clear path (c): any read that successfully
// validated the key's current bytes proves any recorded poison at or below
// that revision is stale. The out-of-band repair (bytes replaced outside the
// mutation API) recovers Health through this path without a restart.
func (c *Component) clearEntityPoisonOnValidRead(entityID string, readRev uint64) {
	if c.entityPoisonSize.Load() == 0 {
		return // steady-state: one atomic load, mutex untouched
	}
	c.dropEntityPoisonRecord(entityID, readRev)
}

// entityPoisonHealthMessage renders the bounded operator summary for Health:
// count + first-N sorted entity IDs with their bounded reasons. Full
// enumeration lives in DebugStatus.
func (c *Component) entityPoisonHealthMessage() string {
	c.entityPoisonMu.Lock()
	total := len(c.entityPoison)
	ids := make([]string, 0, total)
	for id := range c.entityPoison {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sample := ids
	if len(sample) > entityPoisonSampleCap {
		sample = sample[:entityPoisonSampleCap]
	}
	parts := make([]string, 0, len(sample))
	for _, id := range sample {
		parts = append(parts, fmt.Sprintf("%s (%s)", id, c.entityPoison[id].contractErr.Reason))
	}
	c.entityPoisonMu.Unlock()

	noun := "entities"
	if total == 1 {
		noun = "entity"
	}
	msg := fmt.Sprintf("%s: %d poisoned %s (per-entity refusal; repair = delete + recreate): %s",
		graph.ErrorCodeGraphStateResetRequired, total, noun, strings.Join(parts, ", "))
	if total > len(sample) {
		msg += fmt.Sprintf(" (+%d more; enumerate via debug status)", total-len(sample))
	}
	return msg
}

// entityPoisonDebugEntry is one enumerated inventory record for the component
// debug surface — JSON-serializable per DebugStatusProvider.
type entityPoisonDebugEntry struct {
	EntityID string `json:"entity_id"`
	Reason   string `json:"reason"`
	Revision uint64 `json:"revision"`
	Error    string `json:"error"`
}

// entityPoisonDebugStatus is the DebugStatus payload shape.
type entityPoisonDebugStatus struct {
	PoisonedEntityCount int                      `json:"poisoned_entity_count"`
	PoisonedEntities    []entityPoisonDebugEntry `json:"poisoned_entities"`
}

// DebugStatus implements component.DebugStatusProvider: the FULL poison
// inventory, sorted by entity ID, so a mass-poison event is enumerable in-band
// (design D6 — the Health message and gauge are bounded; this is not).
func (c *Component) DebugStatus() any {
	c.entityPoisonMu.Lock()
	defer c.entityPoisonMu.Unlock()

	entries := make([]entityPoisonDebugEntry, 0, len(c.entityPoison))
	for id, rec := range c.entityPoison {
		entries = append(entries, entityPoisonDebugEntry{
			EntityID: id,
			Reason:   string(rec.contractErr.Reason),
			Revision: rec.revision,
			Error:    rec.contractErr.Error(),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].EntityID < entries[j].EntityID })
	return entityPoisonDebugStatus{
		PoisonedEntityCount: len(entries),
		PoisonedEntities:    entries,
	}
}

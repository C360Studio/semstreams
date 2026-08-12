# Graph Poison Response Runbook

Operator procedure for `graph_state_reset_required` poison in ENTITY_STATES under the
per-entity response model (ADR-079, `poison-response-scoping`). Poison means a stored entity
value fails the canonical decode: unreadable JSON (`unreadable_entity_state`) or a
noncanonical predicate (`noncanonical_predicate`). It cannot be written by graph-ingest (the
marshal-site write gate rejects it) — resident poison is legacy state from before a contract
tightening, or an out-of-band write, which is itself a contract violation.

## Detection and alerting

- **Alert on the gauge**: `semstreams_graph_ingest_poisoned_entities > 0`. This is the
  authoritative signal.
- **Health**: graph-ingest reports `Healthy=false`, `Status="degraded"`, with the count and
  the first 10 entity IDs + reasons. Do NOT alert on the `/components/health` HTTP status
  code — it collapses to a binary 503 and cannot distinguish one poisoned entity from a down
  component.
- **Enumeration**: the component debug status (`DebugStatus`) lists the full inventory
  (entity ID, bounded reason, revision) — use it when the count exceeds the Health sample.
- **Detection latency**: detection is first-touch (query/merge) or boot-sweep, not
  write-time. A poisoned key nobody touches surfaces at the next restart's sweep. Consumers
  that watch or poll entity state for work (rules, clustering, projections) detect on their
  own consumption paths.

## What keeps working during an incident

Reads and merges of every OTHER entity keep serving — refusal is per-entity, enforced by
decoding the actual stored bytes. Ingest arrivals for a poisoned entity are Nak'd
(redelivered, bounded by the consumer's MaxDeliver) so valid data survives the repair window;
watch stream metrics show the redeliveries.

## Repair (single or few entities)

1. **Capture before delete.** ENTITY_STATES keeps history depth 1 — delete destroys the
   poisoned bytes. Save them first: `nats kv get ENTITY_STATES <entity-id> --raw > capture.json`
2. **Delete through the canonical verb** (`graph.mutation.entity.delete`), never
   `nats kv del`/`purge` directly — the canonical path clears the inventory entry, invalidates
   caches, and removes suffix-index entries. Out-of-band purges leave a stale degraded Health
   entry until the next successful read or restart.
3. **Re-publish the entity from its canonical source** (or recreate via the mutation API).
   Nak'd arrivals still within their delivery budget apply automatically after repair.
4. Health and the gauge recover as entries clear — no process restart is needed **for
   graph-ingest**.

## Co-resident components: restart is still required where poison was CONSUMED

Per-entity recovery applies to the authoritative graph-ingest surface only. Components with
sticky whole-view contracts stay latched until their process restarts if they observed the
poison on their own paths:

- **rule processor** — evaluation kill switch latches sticky on a consumed poisoned value.
- **graph-clustering / graph-index and all projection owners** — sticky reset-required for
  the derived view.
- **pkg/lifecycle Manager** — manager-lifetime latch.

After repairing the entities, restart the affected processes (or the deployment) to clear
those latches. agentic-loop does NOT latch — loops that touched the poisoned entity failed
with reason `graph_state_reset_required`; re-spawn them after repair.

## Mass poison (e.g. after a contract tightening)

Above roughly 50 entities, per-entity repair is the wrong tool. Escalate to the clean
wipe/reseed contract: stop writers → wipe ENTITY_STATES + derived index buckets → restart →
repopulate from canonical sources (procedure: `docs/operations/17-predicate-cutover-clean-wipe.md`, Graph State
Poison Recovery).

**Guard-bucket warning**: if a wipe recreates the ingest stream, you MUST also clear
`GRAPH_INGEST_APPLIED_SEQ` — retained guard stamps above the reset stream sequences will
silently drop every reseeded message as a stale redelivery (ADR-072 guard keys are
(entity, stream)).

## What this runbook never does

No automated deletion exists, deliberately: a validator reflex that deletes authoritative
state would turn every future contract tightening into a mass-deletion event, and in this
architecture a delete IS an event (watchers fire, indexes purge, downstream reacts). Repair
is always an audited operator action through the canonical write path.

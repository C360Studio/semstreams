# An unreachable body is an exclusion, not a failure

## Why

`graph-embedding` decides whether it can resolve an entity's offloaded body by asking whether *any* content
store is wired, not whether it has the one the reference names
(`processor/graph-embedding/component.go:1974-1984`, ending in `return c.contentStore != nil`). On a registry
miss it opens the referenced key against its own unrelated store, the read fails, and the entity is recorded
as a **failed** embedding.

That misclassification is what makes it serious. A failed record is durable
(`graph/embedding/worker.go:1154-1159`) and re-seeded on every restart
(`processor/graph-embedding/component.go:1003-1033`), and `FailedCount > 0` drives `IndexStateDegraded`
**unconditionally, ahead of the "ready wins" arm** (`graph/index_status.go:228-231`). Re-delivery re-fails,
because nothing about re-delivery makes an unregistered store resolvable. So a **deployment wiring fact** —
"this process has no handle for that store" — becomes a permanent health verdict on the index, with no exit but
deleting the entity.

The spec currently claims the opposite in writing: failures "recover on re-delivery … so a transient dependency
outage recovers without operator action once the dependency returns" (`openspec/specs/graph-embedding/spec.md`).
That is true of every other failure reason and false of this one.

**Why now:** it is latent only because the sole in-tree producer of unresolvable references
(`processor/agentic-loop`) currently discards its references before they reach an entity — which is gh#873.
**Fixing gh#873 makes this live**, so this change is its prerequisite and must land and be observed first.
Reachability changes; correctness does not.

## What Changes

- **The owned-store fallback answers only for the instance it actually serves.** The hop-1 gate falls through
  to the component's own store only when the reference names that store's instance, instead of answering for
  every instance because some store exists.
- **An unresolvable instance is reported as excluded content, not as a failed embedding.** It routes to the
  existing excluded-content path (`reportOffloadedContentExcluded`, gh#414) with its existing
  `content_unresolved_total` metric, and does not enter `FailedCount`.
- **A *resolved* store's read failure is unchanged** — still a reason-classified failure that recovers on
  re-delivery. Only the unresolvable-instance class moves.
- **The classification is made where the observation happens.** The registry contract is per-fetch and
  explicitly no-cache (`storage/storeregistry/storeregistry.go:83-91`) and deregistration on Stop is a live
  path (`service/component_manager.go:2168-2180`), so a store present at the hop-1 gate can be gone by the
  hop-2 fetch. Gating alone would leave that race producing a permanent failed latch.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `graph-embedding`: the requirement "Embedding failures are reason-classified and recover on re-delivery or
  repair" currently asserts recovery-on-re-delivery for **all** failures. It gains the carve-out that an
  entity whose body sits in a store this process cannot reach is excluded and reported, not failed — because
  re-delivery cannot repair a wiring gap.

## Non-goals

- **Any change to `agentic-loop`, trajectory steps, or evidence retention.** That is gh#873, which depends on
  this landing first. This change must be observable on its own.
- **A readiness-envelope field or gauge for "entities with unreachable bodies."** New cross-repo surface with
  no consumer at birth; the cost of not having it is recorded below and filed instead.
- **Deleting the owned-store fallback.** It has zero legitimate in-tree producers, but a sister repo
  constructing a bare store against the same bucket stamps `StorageInstance == bucket`, which the equality
  check preserves. Deleting it is a silent behaviour change for a population we cannot inspect; bounding it is
  not. Revisit when gh#862 lets store ownership be declared from config and the migration story exists.
- **The second instance-blind resolver.** `graph/llm/nats_content_fetcher.go:153-190` ignores
  `StorageInstance` entirely, but has no production caller (already filed as unused surface, gh#422).

## Impact

**Code**: `processor/graph-embedding/component.go` (the hop-1 gate), `graph/embedding/worker.go` (the hop-2
fetch must return a distinguishable "no store for this instance" condition). No new exported symbol, no new
port, no new subject, no new config field.

**What is lost, stated here rather than discovered later** — this is the cost ledger for a silent-exclusion
flip, and it is why the change is not a pure improvement:

1. **`FailedCount` stops reflecting this class**, so the index can report `ready` while a whole class of
   entities has no body embedded. `content_unresolved_total` is a counter — it records that it happened, not
   how many entities are currently affected. Filed rather than fixed here.
2. **The durable record disappears.** A failed record is enumerable from KV; an exclusion leaves none, so
   "which entities have unreachable bodies" stops being answerable after the fact. A real loss of
   inspectability.
3. **A mis-wired deployment gets quieter** — but the signal it replaces is currently *wrong*, and the exclusion
   path is loud by construction (a one-shot warning plus a per-entity metric).
4. **Fixing the wiring no longer re-embeds the body on restart alone** — found at implementation time, not at
   design time, so it is recorded here rather than discovered later. The exclusion reaches a *successful*
   terminal (a generated vector from inline text), and `SavePendingGuarded` SKIPS a re-queue when a generated
   record stands at a same-or-newer source revision (`graph/embedding/storage.go`; the decision table is
   `TestSavePendingGuarded_Decisions` — "generated at SAME revision skipped (restart re-delivery)"). Today's
   *failed* record is overwritten by that same guard ("failed record overwritten (re-queue recovers)"), so an
   operator who wires the missing store and restarts currently does get the body. After this change they need
   a new ENTITY_STATES revision for the entity (or to delete its embedding record). This is not new behaviour
   for the exclusion path — every gh#414 exclusion has had it since that path existed — but this change moves
   a population into it, and that population is the one an operator is most likely to be actively repairing.

**Adopters**: no surface changes. An operator running a correctly-wired deployment sees no difference. An
operator with a mis-wired one stops seeing a permanently degraded index and starts seeing
`content_unresolved_total` climb — which is the honest signal for what is actually a configuration gap.

**sem\* consumers**: none directly. The behaviour reached today only in deployments whose graph-embedding wires
a `store-read` port — the recommended ADR-063 shape for document bodies — or omits `ports` entirely and takes
the `store-read: MESSAGES` default.

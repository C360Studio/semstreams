# An unreachable body is a qualified success, not a failure

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
- **The entity's stored record says so: it becomes a QUALIFIED SUCCESS** (owner ruling, 2026-08-03).
  `Record.Reason` generalizes from "failure classification" to a bounded qualifier of the terminal state,
  valid on any `Status` — `generated + content_excluded` means "a real, servable vector, embedded without its
  unreachable body". Without it the record is indistinguishable from a complete one: the population is
  unenumerable, and `SavePendingGuarded`'s skip freezes it so that wiring the store and restarting changes
  nothing. Both fall out of the qualifier: enumeration is a scan, and repair is the skip becoming
  "a same-or-newer generated vector **of the same terminal quality** stands" — it compares the stored
  qualifier against the one the incoming write would produce, so anything that would change the outcome
  re-queues.
- **A *resolved* store's read failure is unchanged** — still a reason-classified failure that recovers on
  re-delivery. Only the unresolvable-instance class moves.
- **The classification is made where the observation happens.** The registry contract is per-fetch and
  explicitly no-cache (`storage/storeregistry/storeregistry.go:83-91`) and deregistration on Stop is a live
  path (`service/component_manager.go:2168-2180`), so a store present at the hop-1 gate can be gone by the
  hop-2 fetch. Gating alone would leave that race producing a permanent failed latch. Both observation sites
  write the qualifier — hop 1 onto the pending record it queues (its gate refused the offloaded lane, so hop 2
  cannot see the condition), hop 2 onto the terminal it writes for the race.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `graph-embedding`: the requirement "Embedding failures are reason-classified and recover on re-delivery or
  repair" currently asserts recovery-on-re-delivery for **all** failures, and scopes the reason classification
  to failures. It gains two things: an entity whose body sits in a store this process cannot reach is a
  **qualified success** — embedded from its inline text, recorded with a terminal-state qualifier, never
  counted as failed, because re-delivery cannot repair a wiring gap — and the reason classification becomes a
  bounded qualifier valid on any terminal state, which is what makes that population enumerable and
  self-healing.

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

**Code**: `processor/graph-embedding/component.go` (the hop-1 gate and its qualifier write),
`graph/embedding/worker.go` (the hop-2 fetch must return a distinguishable "no store for this instance"
condition), `graph/embedding/storage.go` (`Reason` generalizes to a terminal-state qualifier; the pending
guard becomes qualifier-aware). No new port, no new subject, no new config field.

**Exported-surface gate — the original text here claimed "No new exported symbol", and that was false.**
Retracted and enumerated (review finding, gh#875; the gate re-runs when the surface grows):

| Exported surface | Change | Who it breaks |
|---|---|---|
| `embedding.WorkerMetrics` | gains `ReportContentExcluded(entityID, storageInstance string)` | **Compile break** for any out-of-tree implementer of the interface. In-tree: the component's adapter and two test doubles. |
| `embedding.Storage.SaveGenerated` | gains a trailing `qualifier string` parameter | **Compile break** for any out-of-tree caller. Deliberate: it forces every writer to state whether it produced a complete or a degraded vector. |
| `embedding.ReasonContentExcluded` | new exported constant | Additive. Exported because enumeration is its purpose — a scanner outside the package needs the value by name, not as a bare literal. |
| `embedding.Record.Reason` | **semantic** widening: a bounded qualifier of the terminal state, valid on any `Status`, not a failure-only classification | **Silent break** for an out-of-tree reader assuming `reason != "" ⇒ failed`. The representation is unchanged — same string, same `omitempty`, nothing fails to decode — only the interpretation. This is the one break a compiler cannot catch, which is why it is stated here. |
| `embedding.NamedStore` + `embedding.Worker.WithContentStore` | new exported interface; the setter narrows from `storage.StreamableStore` to `NamedStore` (adds `InstanceName() string`) | **Compile break** for an adopter whose own backend implements the backend-neutral `storage.StreamableStore` and nothing more — a sister repo's filestore is exactly that. Deliberately a compile break: the previous shape probed for the method at runtime and silently excluded EVERY offloaded body when it was absent, which is this change's own defect one layer up. In-tree it costs nothing (`*objectstore.Store` already satisfies it). The fix for an adopter is one method returning whatever their producer stamps into `StorageReference.StorageInstance`. |
| `objectstore.DefaultInstanceName` | new exported constant | Additive. Exported because graph-embedding's operator remedy reasons about that exact value; a shared constant makes it a compile-visible edit if the name ever becomes configurable, instead of stale advice nothing checks. |

Six rows, five of them breaks, all taken deliberately under the clean-beta policy. **Four fail loudly at
compile time** — `WorkerMetrics.ReportContentExcluded`, `Storage.SaveGenerated`'s new parameter,
`Worker.WithContentStore`'s narrowing to `NamedStore`, and (for an implementer of the interface) `NamedStore`
itself. **One is silent**: `Record.Reason`'s widened meaning, which no compiler can catch, so it is documented
on the field itself and in the capability spec. The sixth row, `objectstore.DefaultInstanceName`, is purely
additive and breaks nothing.

The count was wrong here for one round — the prose said "all four … the first three fail loudly" after the
table had grown to six. Corrected at final review, and worth the note: an exported-surface gate that
undercounts is the same failure as not running it.

**What is lost, stated here rather than discovered later** — this is the cost ledger for a silent-exclusion
flip, and it is why the change is not a pure improvement:

1. **`FailedCount` stops reflecting this class**, so the index can report `ready` while a whole class of
   entities has no body embedded. `content_unresolved_total` is a counter — it records that it happened, not
   how many entities are currently affected. Filed as gh#881 rather than fixed here. Softened by the qualifier
   (item 2): the current count is now derivable from KV without a new gauge.
2. ~~**The durable record disappears.**~~ **DISSOLVED by the qualified-success reframe (owner ruling,
   2026-08-03) — for entities that carry inline text.** Those store `generated + content_excluded`, so "which
   entities have unreachable bodies" is answerable by scanning the qualifier, and answerable *better* than
   before, because the entity also has a usable vector rather than none.
   **Bound added at re-review:** an entity with an unreachable body and NO inline text has nothing to embed,
   so it takes the ordinary no-text terminal and stores no record at all. It is reported only by
   `content_unresolved_total` and is not enumerable from the index. For that subpopulation the original cost
   stands undissolved, and the spec now says so rather than claiming otherwise.
3. **A mis-wired deployment gets quieter** — but the signal it replaces is currently *wrong*, and the exclusion
   path is loud by construction (a one-shot warning plus a per-entity metric). This one stands.
4. ~~**Fixing the wiring no longer re-embeds the body on restart alone.**~~ **DISSOLVED by the same reframe.**
   Recorded here because it was found at implementation time and was the more serious of the two: the exclusion
   reaches a *successful* terminal, and `SavePendingGuarded` skipped a re-queue over a same-or-newer generated
   record — so an operator who wired the missing store and restarted would have got nothing, converting a
   self-healing state (today's failed record, which that guard overwrites) into a stuck one. The guard's skip
   is now qualifier-aware: a stored generated vector stands only when it is of the same terminal QUALITY the
   incoming write would produce, so anything that would change the outcome re-queues and heals itself.
   `TestIntegration_GH875_WiringTheStoreSelfHealsTheQualifiedRecord` drives it with no ENTITY_STATES write.
   **Re-review correction:** the first version of this compared against "unqualified", which a hop-2 failure
   defeated permanently — see cost-ledger item 6.
5. **A cross-process producer/indexer split loses body embedding — and for text-less entities loses the
   entity from the index entirely.** Found by review, and this one is real and stands. The second half was
   added at re-review: an entity in that deployment with no inline text goes from *body embedded and
   searchable* to *no vector, no record, one counter tick*. That is a larger loss than "loses body embedding"
   implied, and it is the strongest argument for the store-read-port-declares-its-instance open question. An objectstore *Component* stamps its component instance name; a reader's `store-read` port can only
   ever produce a store named after the BUCKET. Such a deployment embeds bodies today through the instance-blind
   fallback and will be excluded after this change. It is the unavoidable price of the bound (there is no way to
   keep serving it without also serving instances we cannot serve), so what it is owed instead is an executable
   remedy — run a storage component owning that instance in the reader's process, which registers it (ADR-063) —
   and an enumerable, self-healing record. Both are now in place, and the exclusion warning names the remedy
   and logs the owned store's instance beside the reference's so the mismatch is visible in one line. See
   design.md Decision 2.
6. **A hop-2 failure erases the qualifier, and the guard has to survive that** — found by re-review as a
   BLOCKING defect, fixed rather than accepted, and recorded because the shape generalizes. `SaveFailed`
   overwrites `Reason` with the failure reason, so a qualified entity that later fails ANY ordinary way
   (embedder down, timeout) loses the qualifier; the restart reprocess of that failed record cannot recover
   it (the record's `StorageRef` was already dropped by hop 1's inline fallback, so hop 2 has nothing to
   re-observe) and writes `generated + ""`. Against a guard testing "is the stored record unqualified", that
   laundered record was frozen permanently — re-opening both properties the qualifier exists to create. The
   guard now compares terminal QUALITY, so the laundered record re-queues and re-qualifies. The general
   lesson: a qualifier that shares a field with a failure classification will be erased by failure, so
   anything reading it must tolerate erasure rather than assume persistence.


**Adopters**: one **silent** interpretation break — `Record.Reason` is now a bounded qualifier of the terminal
state rather than a failure-only classification, so an out-of-tree reader assuming `reason != "" ⇒ failed` is
wrong (nothing fails to decode; see the exported-surface gate above). Otherwise: an operator running a
correctly-wired deployment sees no difference; one with a mis-wired deployment stops seeing a permanently
degraded index, sees `content_unresolved_total` climb, and can now both enumerate the affected entities and fix
them by wiring the store.

**sem\* consumers**: none directly. The behaviour reached today only in deployments whose graph-embedding wires
a `store-read` port — the recommended ADR-063 shape for document bodies — or omits `ports` entirely and takes
the `store-read: MESSAGES` default.

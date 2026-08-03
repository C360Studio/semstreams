# Design — an unreachable body is an exclusion, not a failure

## Context

See `proposal.md` — Why. Four measurements at `origin/main` = `2dce8258` shape the approach:

- `processor/graph-embedding/component.go:1974-1984` — the hop-1 gate ends in `return c.contentStore != nil`.
- `graph/embedding/worker.go:986-993` — hop-2 `resolveStore` returns that same owned handle on a registry
  miss; `:1004-1008` opens the referenced key against it.
- `graph/embedding/worker.go:1154-1159` — a failed record is **durable**, and
  `processor/graph-embedding/component.go:1003-1033` re-seeds the failed map on every restart.
- `graph/index_status.go:228-231` — `FailedCount > 0` sets `IndexStateDegraded` unconditionally, ahead of the
  "ready wins" arm.

One correction worth recording, because it changes the fix: **the background repair loop is not involved.**
`repairTargets` (`component.go:1297-1308`) is scoped to three derived-write reasons and `failReasonContentError`
is not among them — the godoc says so. So there is no give-up to add. The stickiness is the durable record plus
restart re-seeding, which is why the correct fix is *not to write the record for this class at all*.

The exclusion path already exists and is already wired: `reportOffloadedContentExcluded`
(`component.go:1987-2006`) with `content_unresolved_total` (`metrics.go:127`), built for gh#414.

## Goals / Non-Goals

**Goals:**

- Stop a deployment wiring fact from producing a permanent index health verdict.
- Keep every other content failure exactly as it is, including its recovery-on-re-delivery guarantee.
- Make the resolvability decision where the resolvability is actually observed.

**Non-Goals** (design-level; `proposal.md` carries scope):

- Any change to `graph/index_status.go`. `FailedCount > 0 ⇒ degraded` is correct; the defect is what was
  being counted as failed.
- Any change to the excluded-content reporting shape. It is fit for purpose and already has an operator metric.

## Decisions

### 1. Bound the gate AND reclassify at the fetch — both, not either

**Rejected: gate only.** The registry contract is explicitly per-fetch with no caching
(`storage/storeregistry/storeregistry.go:83-91`), and deregistration on component Stop is a live path
(`service/component_manager.go:2168-2180`). So a store present at the hop-1 gate can be gone by the hop-2
fetch, and that race still produces a permanent durable-failed latch — the exact defect, reached by a narrower
door.

**Rejected: reclassify only.** The gate would still send the fetch down a path that opens a foreign key against
a store that never held it. That is a wrong read, not merely a wasted one: it can only fail, but it fails
*after* doing I/O against an unrelated bucket.

**Accepted: both.** The gate stops predicting; the fetch reports what it observed. This is the house rule
applied literally — hop 1 predicts, hop 2 observes, so the classification belongs where the observation is.

### 2. The fallback is bounded to `c.contentStore.InstanceName()`, not deleted

Measured: the fallback has **zero legitimate in-tree producers**. Three store construction sites exist
(`storage/objectstore/component.go:212`, `processor/agentic-loop/component.go:662-664`,
`processor/graph-embedding/component.go:1150-1152`); the only registering one is already resolvable through
the registry, and the other two do not currently write references that reach an entity.

Deleting it anyway is rejected on adopter grounds: a sister repo constructing a bare store against the same
bucket stamps `StorageInstance == bucket` (`storage/objectstore/store.go:105-108`), which is exactly what an
equality check preserves and exactly the single-bucket deploy ADR-063:367-372 named. We cannot enumerate that
population from here, so deleting is a silent behaviour change for it and bounding is not.

Recorded so the rationale is not mis-carried: this equality check does **not** preserve references produced by
the objectstore *Component*, which stamps the hardcoded instance name `"objectstore"`
(`storage/objectstore/component.go:152`). Those never equalled a bucket name, so "keeps the legacy shape
working" is true only for the bare-store case.

**Corrected at implementation time (review finding, gh#875).** The original text continued "nothing is lost,
because that component is a `StoreProvider` and the registry resolves it". That is true only IN-PROCESS, and
the registry is process-local. A deployment that splits the producer from the indexer — an objectstore
Component in one process, graph-embedding in another reading the same bucket through a `store-read` port —
resolves **nothing** through the registry, and its references carry the producer's component instance name
while the reader's owned store is named after the BUCKET (`createContentStore` passes `BucketName` only,
`processor/graph-embedding/component.go:1150-1152`, so `InstanceName()` is always the bucket). That
deployment embeds bodies correctly today, through the instance-blind fallback, and after this change its
bodies are excluded.

So this is a **real regression for a real shape**, not a theoretical one, and it is the price of the bound:
there is no way to keep answering for that deployment without also answering for instances we cannot serve,
which is the defect. What the change owes it instead is an executable remedy and an enumerable record, both
of which it now has:

- The exclusion warning names the remedy that actually works — run a storage component owning that instance
  in this process, so it registers itself (ADR-063) — and explicitly says a `store-read` port cannot help,
  because a port declares a bucket and not an instance. It also logs `owned_store_instance` beside
  `storage_instance` so the mismatch is visible in one line.
- The record is `generated + content_excluded`, so the affected entities are enumerable, and they self-heal
  the moment the instance becomes resolvable.

A `store-read` port that could DECLARE the instance it serves would close this properly. That is new operator
surface and a schema change, so it is recorded as an open question below rather than taken here.

### 3. The distinguishable condition is a class, not a message

Hop 2 must return a condition the caller can branch on — matched with `errors.Is`, not by string. A message
match would be the same defect one layer up: a decision made by predicting text rather than observing a type.

Only the **no-store-for-this-instance** class routes to exclusion. A resolved store's `Open` or read error stays
`failReasonContentError` with its existing recovery guarantee, because that genuinely does recover on
re-delivery.

### 4. An unreachable body is a QUALIFIED SUCCESS — `Reason` generalizes (owner ruling, 2026-08-03)

Added after review. The first implementation stored a plain `generated` record for an excluded entity: a
success carrying no indication the body was missing. That cost two things, and the second is a **regression**
against the behaviour being replaced:

- **Not enumerable.** No field to scan. "Which entities have vectors missing their body?" was answerable
  before (they were `failed` + `content_error`, found by the failed scan) and would not have been after.
- **Not repairable.** `SavePendingGuarded` skips a re-queue over a same-or-newer `StatusGenerated` record, so
  an operator who wires the missing store and restarts gets a last-per-subject re-delivery that the guard
  swallows. Today's *failed* record does not match that condition and IS overwritten, so it repairs. The fix
  would have converted a self-healing state into a stuck one.

**Rejected: a `ContentExcluded bool` on the record.** The record already carries two axes that disagree with
the runtime — the runtime threads `(outcome, reason, terminal)` with four outcomes, the persisted model has
three statuses with `Reason` welded to one. A boolean adds a THIRD axis to that mismatch, and the next
degraded success (body truncated, model fell back) would want a fourth.

**Accepted: `Reason` becomes a bounded QUALIFIER of the terminal state, valid on any `Status`.**

```
generated + ""                 complete
generated + content_excluded   embedded, body unreachable
failed    + content_error      unchanged
```

No new field, no boolean, no new `Status` value — *less* surface than the boolean. Enumeration falls out
(scan the qualifier). Repair falls out: the guard's skip becomes "a same-or-newer **unqualified** generated
vector stands", so a qualified success re-queues on its next delivery and heals itself the moment the store
is wired. The next case is an enum value, not another field.

**Safety, re-measured at implementation rather than inherited.** Three production readers of `Record.Reason`
exist, and every one gates on `Status` first: `SavePendingGuarded` (`storage.go:362`, inside
`Status == StatusGenerated`), `ScanFailed` (`storage.go:826`, inside `Status == StatusFailed`), and the
component's failed-map seed (`component.go:1026`), which only ever sees `FailedEntry` values produced inside
that gate. `incFailure` — the only path to `failures_total{reason}` — is called from `markFailed` and nowhere
else, so a qualifier cannot label the failures counter by construction. `TestQualifiedSuccess_
NeverReachesFailureAccounting` locks all of it, because a grep is not a regression guard.

**Both write sites qualify, and they are not symmetric.** Hop 1's gate is the DOMINANT path: it refuses the
offloaded lane, so hop 2 receives a pending record with no `StorageRef` and cannot observe the condition for
itself — hop 1 therefore writes the qualifier onto the pending record and hop 2 carries it forward (accepting
only the known value, so an unrecognized reason fails closed to unqualified). Hop 2 sets it itself only in
the gate/fetch race. This was found by the self-heal integration test failing on the dominant path, not by
reasoning.

**The break, stated plainly.** `Reason`'s published contract widens: an out-of-tree reader assuming
`reason != "" ⇒ failed` is now wrong. The representation is unchanged (same string, same `omitempty`, nothing
fails to decode) — only the interpretation. Pre-v1, and taken deliberately.

Out of scope, deliberately: the `Status`/`TerminalOutcome` asymmetry (four runtime outcomes, three persisted
statuses) is **gh#887**, a question with evidence. This ruling supplies the mechanism that question would
need, which is why they are sequential and not merged.

## Risks / Trade-offs

- **The index can now report `ready` while a class of entities has no body embedded** → the honest trade, and
  the reason the proposal carries an explicit cost ledger. Mitigation is observability, not accounting:
  `content_unresolved_total` already exists and the warning is one-shot per instance. The "how many entities
  are currently affected" gauge is filed as gh#881, not built — it is cross-repo readiness surface with no
  consumer at birth. Note the qualifier makes that count DERIVABLE from KV even without the gauge.
- ~~**"Which entities have unreachable bodies" stops being answerable from KV**~~ → **DISSOLVED by Decision 4.**
  The stored record carries `generated + content_excluded`, so the population is enumerable by scanning the
  qualifier — strictly better than the failed-scan it replaces, because these records are also servable
  vectors rather than absent ones.
- ~~**Wiring the store no longer re-embeds the body on restart alone**~~ → **DISSOLVED by Decision 4.** The
  guard's skip is now qualifier-aware, so a qualified record re-queues on its next delivery and heals.
  `TestIntegration_GH875_WiringTheStoreSelfHealsTheQualifiedRecord` drives exactly that, with no write to
  ENTITY_STATES — only the restart's re-delivery.
- **A genuinely mis-wired deployment gets quieter** → this is the silent-exclusion-flip shape, so the ledger
  above is mandatory rather than optional. The distinguishing fact is that the signal being removed is
  currently incorrect: it reports an entity problem for a deployment problem.
- **Bounding the fallback changes behaviour for an unmeasurable population** → the equality check is the
  narrowest possible bound: it only stops the fallback answering for instances it demonstrably cannot serve.
  **Corrected (review, gh#875):** an earlier draft justified this with "a deployment it breaks was already
  reading the wrong bucket." That is **false** for the cross-process producer/indexer split — it was reading
  the RIGHT bucket, and only the instance NAME differed (component instance vs bucket name). See Decision 2
  for the full correction and the executable remedy that replaces the claim.

## Migration Plan

- **Deploy**: no ordering constraint, no data migration, no flag. Existing durable failed records with
  `content_error` are unaffected; entities re-delivered after deploy take the new path.
- **Rollback**: revert. Behaviour returns to counting unresolvable instances as failures. Records written
  while this was live decode fine on the reverted build (`reason` is an existing `omitempty` string); a
  reverted worker reads `generated + content_excluded` as an ordinary generated record and its guard skips
  over it as before — the vector is real, so the only loss is the self-heal, which the reverted build did not
  have either.
- **Sequencing**: this MUST land and be observed before gh#873's store-registration step. Between gh#873's
  reference repair and this fix, every trajectory-step entity would carry a reference to an instance most
  deployments cannot resolve — which is precisely the permanent-degraded case.

## Open Questions

- Whether a gauge for currently-unresolvable entities belongs in the readiness envelope. Deferrable: it changes
  no requirement here and no task, only whether a later change adds cross-repo surface. Decide when a consumer
  exists. Filed as **gh#881**; the qualifier now makes the count derivable from KV, which lowers the urgency.
- Whether a `store-read` port should be able to DECLARE the storage instance it serves, instead of always
  implying the bucket name (`createContentStore` passes `BucketName` only). It is the only clean answer for the
  cross-process producer/indexer split described in Decision 2 — today that deployment must run a storage
  component in the reader's process. Not taken here: it is new operator surface plus a schema change, and it
  wants deciding alongside gh#862's store-ownership-from-config work rather than inside a defect fix.
- The `Status`/`TerminalOutcome` asymmetry is **gh#887**, deliberately sequential to this change.

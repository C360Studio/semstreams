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
(`storage/objectstore/component.go:152`). Those never equalled a bucket name. Nothing is lost, because that
component is a `StoreProvider` and the registry resolves it — but "keeps the legacy shape working" is true only
for the bare-store case.

### 3. The distinguishable condition is a class, not a message

Hop 2 must return a condition the caller can branch on — matched with `errors.Is`, not by string. A message
match would be the same defect one layer up: a decision made by predicting text rather than observing a type.

Only the **no-store-for-this-instance** class routes to exclusion. A resolved store's `Open` or read error stays
`failReasonContentError` with its existing recovery guarantee, because that genuinely does recover on
re-delivery.

## Risks / Trade-offs

- **The index can now report `ready` while a class of entities has no body embedded** → the honest trade, and
  the reason the proposal carries an explicit cost ledger. Mitigation is observability, not accounting:
  `content_unresolved_total` already exists and the warning is one-shot per instance. The "how many entities
  are currently affected" gauge is filed, not built — it is cross-repo readiness surface with no consumer at
  birth.
- **"Which entities have unreachable bodies" stops being answerable from KV** → a real loss of inspectability,
  since an exclusion leaves no durable record where a failure did. Accepted: the durable record it replaces was
  a *wrong* record that also degraded the index.
- **A genuinely mis-wired deployment gets quieter** → this is the silent-exclusion-flip shape, so the ledger
  above is mandatory rather than optional. The distinguishing fact is that the signal being removed is
  currently incorrect: it reports an entity problem for a deployment problem.
- **Bounding the fallback changes behaviour for an unmeasurable population** → the equality check is the
  narrowest possible bound: it only stops the fallback answering for instances it demonstrably cannot serve. A
  deployment it breaks was already reading the wrong bucket.

## Migration Plan

- **Deploy**: no ordering constraint, no data migration, no flag. Existing durable failed records with
  `content_error` are unaffected; entities re-delivered after deploy take the new path.
- **Rollback**: revert. Behaviour returns to counting unresolvable instances as failures.
- **Sequencing**: this MUST land and be observed before gh#873's store-registration step. Between gh#873's
  reference repair and this fix, every trajectory-step entity would carry a reference to an instance most
  deployments cannot resolve — which is precisely the permanent-degraded case.

## Open Questions

- Whether a gauge for currently-unresolvable entities belongs in the readiness envelope. Deferrable: it changes
  no requirement here and no task, only whether a later change adds cross-repo surface. Decide when a consumer
  exists.

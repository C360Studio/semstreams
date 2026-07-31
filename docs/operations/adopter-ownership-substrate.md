# Adopter note — booting ownership with no static projection owner

**Audience:** any repo completing a framework-only ownership cutover — every
unmigrated product writer disabled, while lifecycle and rule-pack mutations
still need the ADR-058 Phase-A substrate. semdragon hit this first (gh#812).

**Status:** additive, NOT breaking. `WireOwnership` is unchanged for existing
callers.

## The problem this solves

`service.WireOwnership` always binds the built-in static projection mutation
client. With no enabled static projection owner you have no contracts to pass,
and an empty contract set fails boot:

```
bind static projection mutation client: mutation client has no contracts
```

The only non-empty contract set the SemStreams binaries use comes from
`internal/builtinprojection`, which downstream correctly cannot import. So the
public helper had a composition path only framework binaries could walk.

## The fix — ask for the substrate, not the bind

```go
manager := lifecycle.NewManager(client, logger)
hbCtx, shutdown := service.WireOwnershipShutdown(ctx, manager)
defer shutdown()

registry, heartbeater, err := service.WireOwnershipSubstrate(hbCtx, client, manager, logger)
if err != nil {
    return fmt.Errorf("wire ownership substrate: %w", err)
}
```

You get the complete Phase-A substrate and nothing nil-able on success:

- the ADR-068 D1 retention backstop (`graph.AssertOwnedBucketsClean`),
- ownership buckets + the `Registry` (`ownership.EnsureBuckets`),
- `Manager.AttachOwnership`, so lifecycle creates are ownership-aware,
- the shared heartbeater later contract-bearing owners enrol against.

Then bind your own owners explicitly, against that heartbeater:

```go
mutations, err := projection.BindMutationClient(hbCtx, projection.MutationClientConfig{
    NATS:        client,
    Registry:    registry,
    Heartbeater: heartbeater,   // the substrate's — one heartbeater per boot
    Owner:       myOwnerID,
    Contracts:   myContracts,   // non-empty
})
```

`BindRulePackContracts` enrols against the same heartbeater.

## Why two functions instead of one that skips the bind

The alternative — make `WireOwnership` skip the bind when contracts are empty
and return a nil client — was rejected. It turns one function's behavior into a
silent mode switch on input emptiness, and hands back a *maybe-nil capability*
that a caller eventually dereferences on the wrong side of. Two intents, two
functions.

The corollary matters when you are reading errors: **asking for a bind with
nothing to bind is still an error.** `WireOwnership` with an empty contract set
fails exactly as before. That guard is correct where a bind was actually
requested — if you see `mutation client has no contracts`, you called the
composing helper when you wanted the substrate one.

## What has NOT changed

- `WireOwnership` behavior for existing callers, including both SemStreams
  binaries — it now composes `WireOwnershipSubstrate` plus the static bind.
- Every fail-closed guard. Retention backstop failure, bucket bootstrap failure,
  and a nil lifecycle manager are all still boot errors in the substrate half.
  A partially-wired substrate would be a boot that looks complete.
- `WireOwnershipShutdown` — still how you cancel and join the Manager-internal
  ownership heartbeater. Call it around whichever wiring helper you use.

## Retire your hand-rolled composition

If you composed the public primitives directly while waiting for this (the
ADR-056 sister-adoption path), retire it now. That sequence evolves — the
retention backstop landed in it after ADR-068 — and a copied boot sequence
silently drifts from the framework's. That drift is the reason this helper
exists rather than documentation telling you the steps.

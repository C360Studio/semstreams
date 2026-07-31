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

Then bind your own owners explicitly, against that heartbeater. Predicates must
be registered in the vocabulary before a contract over them validates:

```go
vocabulary.Register("myapp.thing.status") // else Contract.Validate rejects it

mutations, err := projection.BindMutationClient(hbCtx, projection.MutationClientConfig{
    NATS:        client,
    Registry:    registry,
    Heartbeater: heartbeater,   // the substrate's — one heartbeater per boot
    Owner:       myOwnerID,
    Contracts:   myContracts,   // non-empty
})
```

`BindRulePackContracts` enrols against the same heartbeater.

## You MUST run the heartbeater — Phase B is not optional

The substrate CONSTRUCTS the heartbeater; it does not RUN it. Registering an
owner writes its `OWNER_PRESENCE` key once, and nothing refreshes that key until
something runs the heartbeat loop. Register the ownership service before
`StartAll`:

```go
mgr.RegisterInstance("ownership", service.NewOwnershipService(registry, heartbeater, metrics, logger))
```

**Skip this and your ownership quietly expires.** The presence key ages out after
`ownership.PresenceTTL` (120s), the next registrant compacts your owning entry
out of the epoch, and a rival can bind the same predicate cells while your
process is still live and believes it owns them. With owner-lease enforcement
off (the default) that is two live writers on one predicate group; with it on,
your writes start being rejected. The same omission also costs you
`WatchRevival` — the ADR-056 PR-4 watcher that quiesces your owners when another
incarnation takes over — because `OwnershipService.Start` is what runs both.

This is a real hazard, not a theoretical one: it was measured on the first draft
of this note, which omitted the step. No presence bump after 35s (heartbeat
interval is 30s), against a control that showed an explicit heartbeat does
advance the key.

Phase A constructs, Phase B runs (ADR-058). The framework binaries have always
discharged this; a framework-only composition has to do it too.

## Call the substrate ONCE per boot

Do not call `WireOwnershipSubstrate` twice, and do not call it alongside
`WireOwnership`. Each call builds a fresh `Registry` with its own incarnation
nonce; re-registering the same owner ID on the second registry replaces the
first's epoch entry, which invalidates the first registry's `OwnerToken` and
turns its writes into stale-token writes at the ingest seam.

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

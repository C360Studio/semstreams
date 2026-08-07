# Exported surface review — revision-fenced KV delete

- **Review scope:** the bounded pre-merge follow-up to runtime cutover PR #898.
- **Review baseline:** `dbdc9bd8955fd2a9a44e07841c1467766f31167c`, before the follow-up implementation.
- **Owner decision:** approved in the owner task on 2026-08-05 before implementation.
- **Reviewed surface:** one `natsclient.KVStore` method and removal of graph-ingest's duplicate raw authority handle.
- **Verdict:** `EXPORTED SURFACE REVIEW PASS`.

This ruling does not reopen GS-01, change the mutation protocol, or add a new framework capability. It moves the one
production revision-fenced delete that already exists behind the repository's established KV wrapper. The current
uncommitted follow-up is cited below only to show that the approved shape was implemented without broadening it.

## Owner inventory

At the review baseline, `KVStore` already owned the NATS KV handle, configured timeout, logging, unconditional
`Delete`, `Watch`, key validation, and the typed `ErrKVKeyNotFound` and `ErrKVRevisionMismatch` vocabulary
(`natsclient/kv.go:47`, `natsclient/kv.go:68`, `natsclient/kv.go:442`, `natsclient/kv.go:592`,
`natsclient/kv_key_contract.go:111`, `natsclient/kv.go:647`). It did not expose conditional delete.

Graph-ingest was the sole production bypass. Its component held both `entityBucket *natsclient.KVStore` and a raw
`entityStateBucket jetstream.KeyValue`. The raw field had only three responsibilities: initialization from the same
bucket, the bounded startup snapshot, and `deleteEntityAtRevision`. The delete called raw NATS
`Delete(..., jetstream.LastRevision(revision))` and locally translated the same not-found and conflict classes that
the wrapper already owns. At baseline these seams were `processor/graph-ingest/component.go:499`,
`processor/graph-ingest/component.go:1097`, `processor/graph-ingest/component.go:1162`, and
`processor/graph-ingest/component.go:2141` as read from the baseline commit.

The existing wrapper `Watch(ctx, pattern)` is sufficient for the snapshot. The pinned nats.go v1.52.0 defines
`jetstream.AllKeys` as `">"` and implements `WatchAll` by calling `Watch(ctx, AllKeys, opts...)`; therefore
`Watch(ctx, ">")` preserves the no-option snapshot behavior rather than inventing another wrapper method
(`go.mod:11`; nats.go `jetstream/kv.go:492`, `jetstream/kv.go:1363`, and `jetstream/kv.go:1368`).

The current follow-up collapses that inventory to one authority handle: `entityBucket` is the component field and
storage assignment, `startEntityStateGuard` uses its existing `Watch(ctx, ">")`, and `deleteEntityAtRevision` uses
its conditional-delete method (`processor/graph-ingest/component.go:499`, `processor/graph-ingest/component.go:1095`,
`processor/graph-ingest/component.go:1158`, `processor/graph-ingest/component.go:2149`).

## Binding exported shape

The approved API is exactly:

```go
func (kv *KVStore) DeleteAtRevision(ctx context.Context, key string, revision uint64) error
```

Its contract is deliberately narrow:

1. Reject revision zero and an invalid literal key before any NATS I/O.
2. Apply the store's configured timeout to the single delete attempt.
3. Call NATS KV delete with `jetstream.LastRevision(revision)`.
4. Return `ErrKVKeyNotFound` for absence and `ErrKVRevisionMismatch` for a stale revision.
5. Wrap every other transport/server error with the operation, key, revision, and original cause.
6. Do not read, retry, or return a revision.
7. Keep the existing unconditional `Delete(ctx, key)` method and its semantics unchanged.

The zero check is a correctness boundary, not defensive decoration. nats.go stores the supplied last revision but only
sets the expected-last-subject-sequence header when that value is nonzero. Allowing `LastRevision(0)` would therefore
omit the CAS header and turn the operation into an unconditional delete (nats.go v1.52.0
`jetstream/kv_options.go:96`; `jetstream/kv.go:1166`). Validation must precede timeout application and bucket access so
invalid input cannot reach I/O.

The current implementation matches the ruling at `natsclient/kv.go:461`. Its existing wrapper error classifiers and
sentinels remain the public vocabulary; no graph-specific error type moves into `natsclient`.

## Adopter seam

- **What must an adopter know?** Supply the key and the exact nonzero revision previously observed from that key.
- **What happens if they do nothing?** Existing users of unconditional `Delete` are unchanged. No migration is
  required.
- **Where do they find out?** The method name, signature, Go documentation, and existing typed KV errors state the
  complete contract.
- **What should they have to know?** They should not need the raw NATS bucket, delete options, CAS headers, server
  error strings, wrapper retry policy, or graph-ingest internals.

This is observation over prediction: the caller supplies evidence it already observed, and the server decides whether
that evidence is still current. The wrapper reports the observed outcome without trying to predict or repair a race.

## Rejected alternatives

- **Keep the raw delete in graph-ingest.** Rejected because it duplicates wrapper-owned timeout and error semantics
  and forces one component to retain a second handle to the same authority bucket.
- **Expose the raw bucket or add a raw accessor.** Rejected because it exports the substrate and lets every adopter
  invent different validation, timeout, and error contracts.
- **Add delete options or change `Delete`.** Rejected because unconditional and revision-fenced delete are distinct,
  easily named operations. Variadic options would make the safety property caller-optional and risk changing existing
  behavior.
- **Add `WatchAll`, a raw watcher accessor, or wrapper-specific watch options.** Rejected because the existing
  `Watch(ctx, ">")` is exactly equivalent for this option-free snapshot and keeps the wrapper small.
- **Read before delete, retry on mismatch, or return a revision.** Rejected because each would add hidden policy or
  manufacture evidence. The consumer already has the observed revision; one server-fenced attempt is the complete
  operation.

## Ruling

The smallest idiomatic framework surface is `DeleteAtRevision(ctx, key, revision) error` on `KVStore`, with strict
pre-I/O validation, the existing timeout and typed error vocabulary, and one NATS `LastRevision` delete attempt.
Graph-ingest should retain one wrapped authority handle and use the existing `Watch(ctx, ">")` surface for its bounded
snapshot. No other wrapper or compatibility surface is approved.

**Verdict: `EXPORTED SURFACE REVIEW PASS`.**

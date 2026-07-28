# EMBEDDINGS_CACHE removal (BREAKING) — adopter migration checklist

The `EMBEDDINGS_CACHE` KV bucket and every surface that referenced it are
deleted (`reopen-framework-owned-bucket-guards`, follow-up to #622 / PR #716).
The bucket was a dead surface: graph-embedding created it at `Start` and then
never read or wrote it again — durable embedding records live in
`EMBEDDING_INDEX` (with `EMBEDDING_DEDUP` carrying dedup keys), and semantic
queries are served over NATS (`graph.embedding.query.*`). Its only remaining
role was carrying the retention guard's single exemption, so the guard now
covers `graph.FrameworkOwnedBuckets()` with no exceptions.

Two config fields were deleted with it, and **both now fail loudly** — a stale
config will refuse to boot rather than silently ignore them. Work through the
checklist below before taking a semstreams version that includes this change.

## 1. Remove every graph-embedding `outputs` entry

graph-embedding declares **no output ports**: its durable writes
(`EMBEDDING_INDEX`, `EMBEDDING_DEDUP`, the `GRAPH_STATUS` readiness envelope)
are direct bucket writes at `Start`, not ports. ANY `ports.outputs` entry in a
graph-embedding config — the old `EMBEDDINGS_CACHE` declaration or anything
else — is rejected at config validation with:

```
graph-embedding declares no output ports; remove ports.outputs (see docs/operations/embeddings-cache-removal.md)
```

Before:

```json
"ports": {
  "inputs": [
    {"name": "entity_watch", "subject": "ENTITY_STATES", "type": "kv-watch"}
  ],
  "outputs": [
    {"name": "embeddings", "subject": "EMBEDDINGS_CACHE", "type": "kv"}
  ]
}
```

After:

```json
"ports": {
  "inputs": [
    {"name": "entity_watch", "subject": "ENTITY_STATES", "type": "kv-watch"}
  ]
}
```

## 2. Remove `cache_ttl` from graph-embedding config blocks

The `cache_ttl` knob was a phantom: no code ever consumed it. A graph-embedding
config still carrying the key is rejected at component creation with:

```
cache_ttl was removed from graph-embedding; delete it from the config (see docs/operations/embeddings-cache-removal.md)
```

Delete the `"cache_ttl": "..."` line from every graph-embedding config block.
(Other config blocks may carry their own `cache_ttl` keys — those belong to
other surfaces and are unaffected; only graph-embedding's is removed. Scope
your edit by the enclosing component block, not by the key name.)

## 3. Update any framework-owned-bucket parity/inventory literals

If your repo maintains its own literal copy of the framework-owned bucket set
in tests or tooling (a parity check against `graph.FrameworkOwnedBuckets()`, a
bucket inventory in a cutover rehearsal — semsource's beta148 cutover test is
the known instance), remove `EMBEDDINGS_CACHE` from it:
`FrameworkOwnedBuckets()` no longer contains it, so a stale literal fails the
parity assertion.

## 4. Clean up existing deployments (optional)

An orphaned `KV_EMBEDDINGS_CACHE` bucket left behind by a pre-removal
deployment is inert: nothing reads it, nothing writes it, and the boot-time
retention sweep neither inspects nor reports it. It may be removed manually at
any time:

```bash
nats kv rm EMBEDDINGS_CACHE
```

There is no migration code (pre-v1 clean-break policy): nothing of value is in
the bucket, so there is nothing to migrate.

## 5. Find every reference in your repo

```bash
grep -rn "EMBEDDINGS_CACHE\|cache_ttl" configs/ cmd/ test/
```

Caveat on the `cache_ttl` hits: scope them to graph-embedding config blocks —
same-named keys under other components belong to other surfaces and must stay.
Every `EMBEDDINGS_CACHE` hit goes: config declarations, bucket-inventory
literals, and doc prose alike.

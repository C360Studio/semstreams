# EMBEDDINGS_CACHE removal (BREAKING)

The `EMBEDDINGS_CACHE` KV bucket and every surface that referenced it are
deleted (`reopen-framework-owned-bucket-guards`, follow-up to #622 / PR #716).
The bucket was a dead surface: graph-embedding created it at `Start` and then
never read or wrote it again — durable embedding records live in
`EMBEDDING_INDEX` (with `EMBEDDING_DEDUP` carrying dedup keys), and semantic
queries are served over NATS (`graph.embedding.query.*`). Its only remaining
role was carrying the retention guard's single exemption, so the guard now
covers `graph.FrameworkOwnedBuckets()` with no exceptions.

## What was removed

- `graph.BucketEmbeddingsCache` and its `FrameworkOwnedBuckets()` membership.
- The bucket creation in graph-embedding `Start` and the config validation that
  required an `EMBEDDINGS_CACHE` output port. graph-embedding now declares no
  output ports (its durable writes are direct bucket writes, not ports).
- The retention sweep's `EMBEDDINGS_CACHE` exemption — the sweep ranges the full
  owned set directly.

## Config migration

Drop the `EMBEDDINGS_CACHE` output entry from every graph-embedding component
config. Before:

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

A config that still declares the output entry does not fail validation (outputs
are advisory), but it declares a write the component never performs — drop it.

## Existing deployments

An orphaned `KV_EMBEDDINGS_CACHE` bucket left behind by a pre-removal
deployment is inert: nothing reads it, nothing writes it, and the boot-time
retention sweep neither inspects nor reports it. It may be deleted manually at
any time:

```bash
nats kv del EMBEDDINGS_CACHE
```

There is no migration code (pre-v1 clean-break policy): nothing of value is in
the bucket, so there is nothing to migrate.

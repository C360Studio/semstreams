# Framework KV bucket catalog — adopter notes

SemStreams now declares every KV bucket whose write-ownership or retention the
framework guarantees in **one descriptor catalog** (`graph/kvcatalog.go`:
name, responsible component, class, retention policy, write policy, History). Writers
acquire buckets through `natsclient.EnsureFrameworkBucket` (create-or-open,
reconcile the live bucket to the declared policy, verify, fail the owner's
`Start` closed). Framework owners use the name-resolving
`graph.EnsureCatalogBucket` seam. Framework readers use
`graph.OpenCatalogReader`: it binds must-exist, never creates, and returns a
deliberately read-only capability whose dynamic type cannot satisfy
`jetstream.KeyValue` or a mutation interface. `natsclient` contains the
catalog-independent acquisition mechanisms behind those graph seams.

This is a **breaking release** for adopters in four visible ways.

## 1. graph/query readers fail not-ready instead of creating

`graph/query.Client` no longer creates `ENTITY_STATES`, `SPATIAL_INDEX`, or
`INCOMING_INDEX` on first use. If the owning components (graph-ingest,
graph-index-spatial, graph-index) have not provisioned a bucket yet, a query
returns a classified error with code `index_not_ready` **naming the owner**,
e.g.:

```text
failed to get ENTITY_STATES bucket: framework bucket "ENTITY_STATES" is not
ready: its owner (graph-ingest) has not provisioned it in this deployment
```

What to do: preserve and inspect the canonical `index_not_ready`
classification (the same code graph-index already emits for a not-sound
index), then choose the response appropriate to that caller: retry, fail the
request, or expose a degraded posture. A reader can no longer mask a missing
owner by conjuring an empty bucket — if you were relying on that, the queries
were reading from a bucket nothing wrote.

The agentic graph-query tools register unconditionally and bind this reader
seam lazily on execution. Without graph-ingest, execution returns the same
classified not-ready outcome and `ENTITY_STATES` stays absent.

## 2. Removed `graph/query.Config` fields

`Config.EntityStates`, `Config.SpatialIndex`, and `Config.IncomingIndex`
(TTL/History/Replicas structs) are **gone** — a reader carrying bucket
configuration is how the `ENTITY_STATES` History boot-race happened. Bucket
shape is declared once in the catalog and enforced by each owner.

If your code sets those fields, delete the assignments; `DefaultConfig()` and
`Config{EntityCache: ..., AllowUngatedReads: ...}` are unchanged otherwise.
(Verified: semmem/semsage pass `DefaultConfig()`, semsource passes nil — none
compile against the removed fields.)

## 3. Off-catalog port subjects fail boot

A graph-index KV output port whose subject does not resolve to a catalog
bucket now **fails the component's start** (and therefore boot) naming the
subject:

```text
output port "outgoing_index_typo" subject "OUTGOING_INDEX_TYPO" does not
resolve to a framework KV catalog bucket
```

Previously a typo silently created a stray bucket that no guard protected and
no reader consumed. Fix the subject in the flow configuration (the four valid
subjects are `OUTGOING_INDEX`, `INCOMING_INDEX`, `ALIAS_INDEX`,
`PREDICATE_INDEX`).

## 4. ENTITY_STATES History reconciles to 1 on first boot

The catalog declares `ENTITY_STATES` **History = 1** (owner decision:
nothing reads deeper entity-state history). On deployments where the retired
tool-registration create had won the boot race, the live bucket carries
History = 3; the first boot of this release reconciles it down with a WARN
naming both values:

```text
framework bucket History diverges from its catalog declaration; reconciling
bucket=ENTITY_STATES adopted_history=3 declared_history=1
```

Reconciling down discards stored revisions beyond depth 1 for each key. This
is destructive-but-unread — no shipped consumer reads `ENTITY_STATES` history
depth — but if you have out-of-tree tooling that replays entity-state KV
history, capture what you need **before** upgrading.

## Grep guidance for adopters

```bash
# Do you set the removed graph/query bucket config?
grep -rn "EntityStates\s*:\|SpatialIndex\s*:\|IncomingIndex\s*:" --include="*.go" .

# Do you create-or-get any framework bucket yourself? Readers must not create;
# route owners through the catalog seam instead.
framework_buckets='ENTITY_STATES|PREDICATE_INDEX|INCOMING_INDEX|OUTGOING_INDEX|ALIAS_INDEX|NAME_INDEX'
framework_buckets="${framework_buckets}|ENTITY_SUFFIX_INDEX|SPATIAL_INDEX|TEMPORAL_INDEX|TEMPORAL_INDEX_REVERSE"
framework_buckets="${framework_buckets}|EMBEDDING_INDEX|EMBEDDING_DEDUP|COMMUNITY_INDEX|COMMUNITY_SUMMARIES"
framework_buckets="${framework_buckets}|ANOMALY_INDEX|GRAPH_INGEST_APPLIED_SEQ|GRAPH_STATUS|STORAGE_REPORT"
grep -rnE "\"(${framework_buckets})\"" --include="*.go" .

# Do you match on the not-ready error? The code is stable; the text is not.
grep -rn "index_not_ready" --include="*.go" .
```

Rules of thumb going forward:

- **Owners** (you own the bucket's writes): `graph.EnsureCatalogBucket`.
- **Readers**: `graph.OpenCatalogReader`; preserve canonical outcome
  classifications and keep retry/fail/degraded response policy local.
- **Application/product buckets** (AGENT_LOOPS, flow stores, personas, ...)
  are outside the catalog by rule — keep using `CreateKeyValueBucket`.
- Never spell a catalog bucket name as a string literal — reference the
  `graph.Bucket*` constants. In-repo, a contract test enforces this
  mechanically.

## Context

Three defects live in the two-hop embedding pipeline. **Hop 1** is the
ENTITY_STATES watcher (`processor/graph-embedding/component.go`): it sees an entity
change, extracts or references text, and writes a pending `Record` to
`EMBEDDING_INDEX` keyed by entity ID. **Hop 2** is the worker pool
(`graph/embedding/worker.go`, 5 goroutines): it reads the pending record, resolves
the text (`getSourceText` — inline `SourceText`, or fetch-from-ObjectStore for the
offloaded `StorageRef` lane), truncates to `maxSourceTextLen`, checks dedup,
generates if needed, and saves.

Post-Track-0 facts this design builds on (verified in code):

- The dedup key (`DedupKey(EmbedderIdentity, text)`, `cache.go`) folds embedder
  type/model/dimensions. Track 0 derived it at **hop 1** (`component.go:1404`) for
  the inline lane and **disabled dedup** for the offloaded lane, because hop 1
  holds only a `StorageRef` (an address), and hashing the address served the old
  body's vector forever. `message.StorageReference` carries **no content digest**.
- The dedup **check** already happens in hop 2 (`getOrGenerateEmbedding` consults
  `record.ContentHash`), so the key's producer (hop 1) and consumer (hop 2) are
  split across the two-hop boundary — that split is the root of #623.
- `SaveGenerated`/`SaveFailed` do `GetEmbedding` → unconditional `Put`. No CAS.
  `GetEmbedding` discards `entry.Revision()`. The index bucket is a
  `jetstream.KeyValue`, which offers `Update(ctx, key, value, revision)` (CAS).
- `Record.SourceRevision uint64` exists (threaded from hop 1 for the ADR-066
  watermark) but is **dropped** by `SaveGenerated`, which rebuilds the record.
- The cap `maxSourceTextLen` is a worker field set from
  `component.maxSourceTextLen()` (8000/4000 by embedder type); truncation via
  `truncateAtWord` is silent.

## Goals / Non-Goals

**Goals:**

- One derivation site for the dedup key, at the point where the embedded bytes
  exist, so both lanes dedup and the key is provably the key of what was embedded.
- The text cap is observable and is part of a vector's identity.
- For a single entity, only the newest source revision's vector persists, and a
  record's `ContentHash` and `Vector` are always mutually consistent.
- Land as one change against `storage.go` + `worker.go` (+ the hop-1 call site),
  because #623's key-move and #614's write-ordering touch the same
  read-modify-write and co-simplify.

**Non-Goals:**

- BM25 stateful-index redesign (#619 parent).
- Durable repair loop for failed deletes (#625, Epic C). This fixes write
  *ordering*, not delete *durability*.
- Any change to hop-1/hop-2 boundary, ENTITY_STATES ownership, ADR-066 completion
  semantics, or ADR-068 (no lifecycle TTL on the live graph).

## Decisions

### D1 — Derive the dedup key in hop 2, from the resolved+truncated text (#623)

`getSourceText` already returns the exact bytes that get embedded (fetched for the
offloaded lane, truncated to the cap). Compute `key := DedupKey(identity, text)`
there and use it for **both** the dedup check and `SaveDedup`. Stop consulting
`record.ContentHash` (the hop-1 value) as the key.

Consequences:
- The offloaded lane dedups again — the key is content-derived regardless of lane.
- The inline and offloaded lanes converge on one code path.
- The cap participates in the key for free (D2), because the key is over the
  *truncated* text.
- Hop 1 no longer needs to derive a real key. The pending record's `ContentHash`
  becomes vestigial; hop 1 writes it empty. This is what lets #602's "cap in key"
  requirement fall out of D1 rather than needing its own mechanism.

Residual cost, stated honestly: for the offloaded lane the dedup check now needs
the body fetched first (no content digest on the ref to short-circuit on). But
hop 2 already fetches before generating, so this saves the *Generate* call, not
the fetch. A future `StorageReference` content digest could skip the fetch too;
out of scope here. Emit `dedup_skipped_total{reason}` so the residual is visible.

### D2 — The text cap is a contract (#602)

Expose the cap as an operator-reachable config field (JSON round-trip test, no
shadow struct, `task schema:generate`). Report truncation (a counter and/or a
record flag) so the bytes actually embedded are discoverable. Because D1 keys over
the truncated text, a cap change automatically changes the key — a re-cap can no
longer serve a vector built from different bytes. This is the whole of #602's
"key half"; the visible-truncation half is the new counter/flag.

### D3 — Order writes by source revision, under CAS (#614 part 2)

The lost-update bug is **semantic ordering**, not just concurrent clobber: worker A
(source rev N, old text) can finish after worker B (source rev N+1, new text) and
`Put` the stale vector last. Plain KV-revision CAS prevents *lost updates* but not
*out-of-order source revisions* — A's CAS can still succeed and win. So the guard
is on `SourceRevision`, made atomic with CAS:

`SaveGenerated(ctx, entityID, vector, model, dims, contentHash, sourceRevision)`:
1. read `(existing, kvRevision)` via a revision-aware get.
2. `existing == nil` → `ErrRecordGone` (Track 0 drop-not-resurrect, unchanged).
3. `existing.SourceRevision > sourceRevision` → a newer vector already landed;
   **drop and return `ErrSupersededRevision`** — a distinguishable non-failure
   sentinel, not bare `nil`, so `saveAndNotify` can tell a superseded drop from a
   real write and skip the generated callback (firing it would push THIS call's
   older vector into a `WithOnGenerated` consumer's cache).
4. else `Update(entityID, newRecord, kvRevision)` — CAS. On `ErrRevisionMismatch`,
   re-read and re-evaluate from step 1 (bounded retries; a loser drops at step 3,
   so this converges). `ErrCASExhausted` is transient/re-drivable — callers must
   re-drive (the watcher re-delivers), NOT record a generation failure.

`SaveFailed` takes the same shape, plus equal-revision terminal precedence: a
`StatusGenerated` record at the same source revision is NOT downgraded to failed
(a success outranks a same-revision failure).

The record now **persists `SourceRevision`** (stop dropping it) so step 3 has
something to compare. `ContentHash` and `Vector` are set together from the
worker's own hop-2 data — never copied from a possibly-newer `existing` — so they
cannot desync (the second half of #614 part 2).

Note the co-simplification: passing `contentHash` (D1) and `sourceRevision` (D3)
into `SaveGenerated` removes the *reason* it read `existing` (to preserve
`ContentHash`). The read that remains exists only for the CAS revision and the
ordering guard — a strictly simpler contract than today's "read to copy a field."

`SaveFailed` takes the same CAS treatment so a stale failure can't clobber a newer
success, and carries `sourceRevision` for the same guard.

## Risks / Trade-offs

- **CAS retry under contention.** Bounded (e.g. 3) retries; the `SourceRevision`
  guard guarantees a loser drops rather than spins, so the loop converges even
  under sustained same-entity churn. Worst case is a few extra reads, not a stall.
- **Offloaded dedup still pays the fetch.** D1 saves the Generate call but not the
  ObjectStore read. Acceptable — the expensive half (remote embed) is what dedup
  targets — and made visible via `dedup_skipped_total`. A ref-level content digest
  is the future optimization, deliberately deferred.
- **Legacy records have `SourceRevision == 0`.** Treated as oldest/unknown, so any
  real revision wins the D3 guard — a legacy record is re-embedded once, then
  carries a real revision. Consistent with the pre-v1 re-embed posture.
- **BREAKING (state).** The dedup key changes shape again (cap now in the key via
  truncated text; offloaded lane now keyed). Old `EMBEDDING_DEDUP` entries stop
  matching; the first pass re-embeds. The bucket is untimed by design (not the live
  index → no TTL), so stale keys never match and never expire — they must be
  **wiped and reseeded**, not left to age out. Same posture as Track 0's key break —
  acceptable pre-v1, and cheaper now than a compat shim maintained forever post-1.0.
- **Persisting `SourceRevision` on generated records** slightly enlarges the stored
  JSON. Negligible; the field already exists on pending records.

## Why

The embedding pipeline can silently serve the wrong vector, and Track 0 (#624)
made that trade explicit rather than fixed. Three defects share one seam
(`graph/embedding/storage.go` + `worker.go`) and one theme — **evidence cannot
silently expire or silently serve the wrong vector**:

- The dedup key that Track 0 hardened to fold in embedder identity still could not
  cover the offloaded (`StorageRef`) lane, because the key was derived at hop 1
  where only the storage reference exists, not the body. Track 0 disabled dedup
  there rather than hash an address. Measured cost: fresh embedding work rose
  **68 → 191 per statistical e2e run (2.81×)**, every extra one a remote call on
  the neural tier, and **nothing counts it** (#623).
- The embedding text cap is hard-coded at 8000 characters and truncation is
  silent, so the bytes that were actually embedded are undiscoverable — and the
  cap is *part of what the vector depends on*, so it must participate in the dedup
  key or a cap change silently serves stale vectors (#602).
- `SaveGenerated`/`SaveFailed` do a read-modify-write with no revision CAS. Five
  worker goroutines pull one entity's revisions N and N+1 concurrently; if the
  older finishes last its `Put` wins, so `EMBEDDING_INDEX` permanently holds the
  **old text's vector** and the record's `ContentHash` and `Vector` desync
  undetectably (#614 part 2).

Bundled because splitting them means touching the same two files three times, and
because #623's fix (derive the key where the truncated body is in hand) *subsumes*
#602's cap-into-key requirement — they cannot land independently.

## What Changes

- **Derive the dedup key at content resolution (hop 2).** Move `Record.ContentHash`
  from producer (hop 1, ref-only) to consumer (hop 2, `getSourceText`, where the
  fetched and truncated body exists). This restores content-addressed dedup on the
  offloaded lane and unifies it with the inline lane at one derivation site.
- **Make the text cap a contract, not a constant.** Operator-configurable cap;
  truncation is reported (a metric/flag, not silence); the effective cap
  participates in the dedup key so a cap change cannot serve a vector built from
  different bytes.
- **Count the work dedup avoids and the work it skips.** A
  `dedup_skipped_total{reason}` counter so the offloaded-lane re-embed cost (and,
  post-fix, its recovery) is visible rather than inferred.
- **Order concurrent writes by revision.** `SaveGenerated`/`SaveFailed` use the KV
  store's revision CAS (`Update` with the read revision, retry on conflict) so a
  late older-revision write cannot overwrite a newer vector, and `ContentHash`
  cannot desync from `Vector`.
- **BREAKING (state, not API):** `EMBEDDING_DEDUP` keys change shape again (the cap
  and, for the offloaded lane, real content now participate). Existing entries stop
  matching; the first pass re-embeds. `EMBEDDING_DEDUP` is untimed by design (it is
  not the live index, so it correctly carries no TTL), so old-shape keys never
  match *and* never expire — they must be **wiped and reseeded**, not left to age
  out. This is a pre-v1 state wipe consistent with Track 0's dedup-key break.

## Capabilities

### New Capabilities

<!-- None. All behavior lives in the existing graph-embedding capability. -->

### Modified Capabilities

- `graph-embedding`: the spec today documents only scoped semantic search. This
  change seeds three requirements it is silent on today, distilled from code and
  verified against it (lazy-seed, not backfill):
  - **content-addressed dedup identity** — a dedup hit returns a vector only when
    the current embedder identity, effective text cap, and the resolved content
    all match; the key is derived where the embedded bytes exist.
  - **embedding text-cap contract** — the cap is operator-configurable, truncation
    is observable, and the cap is part of the vector's identity.
  - **vector write ordering** — for one entity, only the newest source revision's
    vector persists; `ContentHash` and `Vector` are always mutually consistent.

## Impact

- **Code:** `graph/embedding/storage.go` (dedup record + CAS on the two save
  lanes), `graph/embedding/worker.go` (key derivation in hop 2, skip-counter),
  `processor/graph-embedding/component.go` (cap config surface, remove the hop-1
  key derivation), `graph/embedding/cache.go` (`DedupKey`/`EmbedderIdentity`
  cap field). Metrics registration for `dedup_skipped_total`.
- **Config/schema:** a new operator-reachable embedding text-cap field →
  `task schema:generate`, JSON round-trip test, no shadow struct.
- **State:** `EMBEDDING_DEDUP` invalidated (see BREAKING). `EMBEDDING_INDEX`
  unchanged in shape; its write path gains CAS.
- **Consumers:** `semsource` (primary dogfooding adopter; drives the offloaded
  content lane via `ContentStorable`) and `semboids` (high-volume load) are the
  sem* products that exercise this capability. No API signature change reaches
  them; the state break is absorbed by re-embedding on first pass.
- **Out of scope / follow-on:** the BM25 stateful-index decision (#619 parent) and
  the graph-embedding durable repair loop (#625) are separate epics and untouched
  here.

## Non-goals

- **Not** the BM25 tier redesign (#619 parent) — lexical index over an immutable
  snapshot vs stateless hashed TF is an owner decision, deferred.
- **Not** a durable cleanup/repair loop for failed deletes (#625, Epic C) — this
  change fixes write *ordering*, not delete *durability*.
- **Not** a change to graph-ingest, ENTITY_STATES ownership, or the readiness
  watermark contract (ADR-066). Hop-1/hop-2 boundary and completion semantics stay
  as Track 0 left them.
- **Not** re-enabling any lifecycle TTL on the live graph (ADR-068 stays).

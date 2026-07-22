# Tasks — embedding-evidence-integrity

TDD order: each code group is preceded by the test that fails without it. Verify
fails-without-fix via the `cp`-backup or `git archive` scratch method, never
`git stash`/`checkout` (shared tree). Groups 1→4 are the seam; 5 is the config
surface; 6 is the gate.

## 1. Move dedup-key derivation to hop 2 (D1 / #623)

- [x] 1.1 Test: an offloaded (`StorageRef`) entity and an inline entity with
      byte-identical resolved text produce the **same** dedup key and the second
      dedups (no regeneration). Fails today (offloaded lane has an empty key).
- [x] 1.2 Test: overwriting the body at a stable ObjectStore key changes the dedup
      key and forces regeneration (no stale-body vector).
- [x] 1.3 In `worker.go`, derive `key := DedupKey(embedderIdentity, cap, text)`
      inside/after `getSourceText` (post-fetch, post-truncate) and use it for both
      the dedup check (`getOrGenerateEmbedding`) and `SaveDedup`. Stop consulting
      `record.ContentHash` as the key.
- [x] 1.4 In `component.go`, remove the hop-1 key derivation
      (`DedupKey(embedderIdentity(), text)` at the inline lane) and the
      offloaded-lane skip; hop-1 pending records carry an empty `ContentHash`.
- [x] 1.5 Thread the embedder identity + cap into the worker so hop 2 can build the
      key without a hop-1 value (confirm `embedderIdentity()` is reachable from the
      worker or passed at construction; no cross-goroutine race on identity per the
      Track 0 `atomic` fix).

## 2. Count skipped dedup (D1 observability / #623)

- [x] 2.1 Test: an entity embedded on a lane/condition where dedup is not consulted
      increments `dedup_skipped_total{reason}`.
- [x] 2.2 Register `dedup_skipped_total{reason}` in `processor/graph-embedding/metrics.go`
      and increment at the skip sites (worker metrics adapter). No phantom metric —
      assert a consumer in the test.

## 3. Text cap as contract (D2 / #602)

- [x] 3.1 Test: an operator-set cap is honored (truncate-at-word to the configured
      value) and round-trips through the config JSON (no shadow struct).
- [x] 3.2 Test: truncation emits a signal (counter/flag), not silence.
- [x] 3.3 Test: a cap change that alters the embedded byte range changes the dedup
      key (identity-bearing) — this rides D1's key-over-truncated-text.
- [x] 3.4 Add the operator-reachable cap config field (replaces the hard-coded
      8000/4000 by embedder type); wire `maxSourceTextLen()` to read it with a
      sane default.
- [x] 3.5 Emit the truncation signal at `truncateAtWord` in `getSourceText`.

## 4. Order writes by source revision under CAS (D3 / #614 part 2)

- [x] 4.1 Test: revisions N and N+1 of one entity generating concurrently — the
      older completing last — leave the index holding **N+1**'s vector (drops the
      late N write). Fails today (unconditional `Put`).
- [x] 4.2 Test: two concurrent same-entity commits use revision CAS; no committed
      vector is silently overwritten (assert via injected revision conflict + retry).
- [x] 4.3 Test: a persisted generated record's `ContentHash` and `Vector` come from
      the same generation (never mixed across revisions).
- [x] 4.4 Test: `SaveGenerated`/`SaveFailed` still return `ErrRecordGone` when the
      pending record vanished (Track 0 drop-not-resurrect preserved).
- [x] 4.5 Add a revision-aware get (`GetEmbedding` returning `entry.Revision()`, or
      a sibling) to `storage.go`.
- [x] 4.6 Change `SaveGenerated` signature to accept `contentHash` and
      `sourceRevision`; persist `SourceRevision` on generated records (stop
      dropping it); build the record's `ContentHash`+`Vector` from the passed data,
      not from `existing`.
- [x] 4.7 Implement the CAS loop: read `(existing, rev)`; `nil`→`ErrRecordGone`;
      `existing.SourceRevision > sourceRevision`→drop (return `ErrSupersededRevision`); else
      `Update(key, rec, rev)`; on `ErrRevisionMismatch` re-read and re-evaluate
      (bounded retries). Apply the same treatment to `SaveFailed`.
- [x] 4.8 Update the hop-2 call sites (`saveAndNotify`, `markFailed`) to pass the
      worker's own `contentHash` (from group 1) and `sourceRevision` (from the
      pending record it is completing).

## 5. Config surface + schema

- [x] 5.1 `task schema:generate`; commit the resulting `schemas/` diff (additive
      cap field). JSON round-trip test for the new field.
- [x] 5.2 Confirm no shadow struct and the field is reachable end-to-end from
      operator config (factory overlay — apply the Track 0 lesson: a knob accepted
      and validated but never copied by the factory is a phantom).

## 6. Gate

- [x] 6.1 `go build ./...`, `go vet ./...` plain + `-tags=integration` + `-tags=live_llm`,
      `task lint` clean.
- [x] 6.2 `go test -race ./...` (0 FAIL); tagged integration on `graph/embedding`
      and `processor/graph-embedding`; contract tests.
- [x] 6.3 BREAKING (dedup key + `EMBEDDING_DEDUP` invalidation): both tiers green,
      `validation_errors:0`. Offloaded-lane dedup RESTORED: statistical fresh
      191→**68** (exactly HEAD's baseline), dedup_hits 53→**177**; semantic fresh
      **81**, dedup_hits **168**, known-answer **7/7**. Search quality recovered to
      0.242 (statistical, one run, back in HEAD's 0.240–0.244 range).
- [ ] 6.4 `openspec validate --strict`, then `/opsx:archive` on completion.

## 7. Review rounds (semstreams-reviewer + PR #628 review)

Round 1 (semstreams-reviewer): offloaded truncation counted; superseded-drop
sentinel added; "age out" wording; MaxTextLen negative reject; stale comments.

Round 2 (PR #628 review) — in-scope fixes:
- [ ] 7.1 Rune-safe, Unicode-whitespace truncation unified across both lanes
      (fixes byte-vs-char UTF-8 splitting and cross-lane key divergence; subsumes
      #627).
- [ ] 7.2 `max_text_len` upper bound (schema min/max + runtime) and overflow guard
      on the `+1` LimitReader read.
- [ ] 7.3 `ErrCASExhausted` re-drives (watcher re-delivery), never records a
      generation failure or advances readiness as terminal.
- [ ] 7.4 Equal-revision terminal precedence: `SaveFailed(R)` does not downgrade a
      `StatusGenerated(R)` record.
- [ ] 7.5 Metric-invariant fix (caught by the semantic e2e re-measure): count
      `IncDedupHits` on the successful-store path in `saveAndNotify`, not eagerly at
      lookup in `getOrGenerateEmbedding`. The round-2 superseded-drop skip made a
      dropped dedup hit skip `generated_total` while still counting `dedup_hits`,
      inverting the e2e invariant (`dedup_hits 166 > generated 69`, semantic red).
      Both counters now fire together on store, so `dedup_hits ⊆ resolutions`.

Round 2 — deferred with filed follow-ups (pre-existing or new scope):
- #629 pending-lane (`SavePending`) resurrection under coalescing (pre-existing,
  `coalesce_ms` off by default, needs a deleted-marker protocol).
- #630 worker singleflight for same-content bursts (#623 key property proven by
  e2e; burst-stampede is a separate concurrency optimization).
- ADR-066 addendum documenting the persisted-`SourceRevision` + CAS invariant.

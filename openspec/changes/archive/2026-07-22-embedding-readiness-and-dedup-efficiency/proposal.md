## Why

`graph-embedding` publishes a fully-green readiness envelope (`Ready: true`,
`State: ready`, `lag: 0`) while holding **zero usable vectors**: a failed
embedding advances the low-water watermark to target (advancing on every terminal
outcome is deliberate — `readiness.go:64` — it stops a permanently-failing or
no-text entity from pinning the watermark), but **nothing gates the reported
state on failures**, so a semembed outage during cold start reports caught-up over
nothing but failures (#613). This violates the contract `graph-index` already
honors — the shared `graph-index-readiness` capability requires `failedCount > 0 →
degraded`, unconditional — which today the shared projection does not actually
enforce (only graph-index's caller-side watermark hole does). Worse, the failure
*detail* needed to act on a degraded state
already exists and is thrown away: `SaveFailed` durably records `Status: failed`
+ `ErrorMsg` per entity, and `StatusFailed` is read by **nothing** (it appears
only at its constant definition and the single write site). A bare `degraded`
bit is the same phantom class this program exists to remove — an operator cannot
tell a whole-service outage from three poison entities.

One adjacent efficiency defect sits on the same embedding worker seam: a burst of
byte-identical content stampedes the paid embedder because the workers have no
singleflight (#630). A second, #627 (cross-lane dedup miss on over-cap content),
was already closed in code by increment 1 — `truncateAtWord` is now the single
rune-safe routine for both the inline and offloaded lanes (`worker.go:793`,
`:884`) — so this change only locks that lane-independence behind a regression
test and closes the stale issue.

Now, because the pre-v1 breaking wave (ADR-083/084/085) is the moment to correct
readiness semantics without a permanent compat shim, and because increments 1–3
of this epic each spun off same-seam follow-ups — bundling the complete operator
story (not slicing Layer 3 into yet another follow-up) is the deliberate choice
to stop that cycle.

## What Changes

- **BREAKING — readiness reports `degraded` when embeddings have failed.** A
  `failed` record is not a usable vector. Embedding **keeps advancing its
  low-water watermark on every terminal outcome** (including failed and no-text) —
  a deliberate deadlock-avoidance property (`readiness.go:64`); the fix does NOT
  touch it. What is wrong is that a full-coverage watermark then reports
  `State: ready` with nothing gating on failures. Fix: add a `FailedCount` input
  to the shared `ComputeIndexStatus` and project `FailedCount > 0 → degraded`
  **unconditionally** (before "ready wins") — making the shared projection finally
  enforce the `graph-index-readiness` rule that is written today but only enforced
  by graph-index's caller-side watermark hole. Embedding is the first producer
  whose watermark reaches target *with* failures. Not configurable (owner-settled:
  `FailedCount > 0` is true and simple). `Ready` stays coverage-accurate; `State`
  carries the health verdict; consumers gate on `State` (ADR-085).
- **Failed embeddings become re-processable.** Hop-2 treats `StatusFailed`
  records as re-processable on watcher re-delivery, so a restart (or a
  re-published entity) naturally retries them — no separate re-drive path, and
  the `failedCount` gauge trending down doubles as the recovery signal.
- **Legible degraded state — three observability layers:**
  - **L1 (metrics):** a `failed` count gauge alongside `indexed`/`target`, and a
    `{reason}`-labeled failure counter over a **bounded** enum
    (`connection_refused` / `timeout` / `content_too_large` /
    `dimension_mismatch` / `marshal` / `other`) — classified at `SaveFailed`
    time and stored next to the raw `ErrorMsg`, reusing the fusion
    `body_hydration_failures_total{reason}` pattern from increment 2. Never the
    raw `ErrorMsg` as a label (unbounded → cardinality blowup).
  - **L2 (the central producer report — GRAPH_STATUS):** graph-embedding already
    publishes its ADR-066 envelope to `GRAPH_STATUS/graph-embedding`
    (watched by fusion + graph-query). The degraded envelope carries
    `failedCount` + the **full bounded reason breakdown** (`{reason: count}`, ≤6
    fixed keys) + a first-failure timestamp. Bounded-cardinality, so it stays
    compact and watchable on the hot KV path while answering "how bad *and* what
    kinds" completely — the always-on production aggregate. It must NOT carry the
    unbounded per-entity list.
  - **L3 (per-entity drill-down — two tiers):** the durable failed records live
    in `EMBEDDING_INDEX` (`Status: failed` + reason + `ErrorMsg`). (a)
    **Production escape hatch** through the production query surfaces
    (fusion/graph-query, which already hold the GRAPH_STATUS watch) — the
    always-available, production-safe path for failure detail. (b) **Opt-in debug
    enumerate** via message-logger over `EMBEDDING_INDEX` filtered to
    `Status==failed` — message-logger is a debug surface, **off by default**, an
    operator enables it at reboot for deep forensics. **Constraint: production
    observability (L1 + L2 + the fusion/graph-query escape hatch) must be
    complete without any debug service running.** The exact production-vs-debug
    boundary (how much per-entity detail the production surfaces expose vs. what
    only the debug enumerate gives) is a design-phase `/query-pattern` decision;
    any operator-reachable field gets a JSON-round-trip test.
- **#627 — verify-and-close (no production change).** The cross-lane truncation
  divergence was already fixed by increment 1's rune-safe `truncateAtWord`
  unification. This change adds a regression test asserting cross-lane dedup-key
  identity for byte-identical content, then closes #627; its Option-2 fetch-skip
  optimization is re-homed as a separate deferred item.
- **#630 — keyed singleflight** around the embedder call so a burst of
  byte-identical content pays one remote call, not N. **Process-local**
  (`singleflight.Group`) this increment; the distributed KV-reservation variant
  (semsource/semboids run multiple embedding processes) is deferred as a follow-up
  to file only if cross-process stampede is measured.

## Capabilities

### New Capabilities

None. This change modifies existing capabilities only.

### Modified Capabilities

- `graph-index-readiness`: `ComputeIndexStatus` gains a `FailedCount` input and
  projects `FailedCount > 0 → degraded` unconditionally (before "ready wins"), so
  the shared projection enforces the already-written `failedCount → degraded` rule
  for a producer (graph-embedding) whose watermark advances past failures. The
  metrics/envelope requirements gain the failure detail — a `failed` count gauge
  and a degraded `GRAPH_STATUS` envelope carrying `failedCount` + the full bounded
  reason breakdown + first-failure time.
- `graph-embedding`: (a) failure records are reason-classified (a bounded reason
  enum stored beside `ErrorMsg`) and the current-failed count is tracked as a
  gauge; (b) `StatusFailed` records are re-processable on re-delivery; (c) the
  content-addressed dedup key is lane-independent (already true since inc 1;
  locked by a regression test — #627); (d) concurrent byte-identical content
  collapses to a single embedder call (#630).
- `fusion` and/or `graph-query` (production escape hatch — exact home is a
  design decision): the production query surfaces expose embedding-failure detail
  to operators from the GRAPH_STATUS watch they already hold, so production
  observability does not depend on the message-logger debug surface being on.
  May reuse the existing missing/unhydrated reporting seam rather than a new
  endpoint.

## Impact

- **Code:** `graph/index_status.go` (`FailedCount` input + unconditional
  `FailedCount > 0 → degraded`; envelope failure-detail fields),
  `graph/embedding/worker.go` (hop-2 processes `StatusFailed`; singleflight around
  `generate`; reason classification at the `markFailed` sites),
  `graph/embedding/storage.go` (`SaveFailed` reason field), 
  `processor/graph-embedding/readiness.go` + `component.go` + `metrics.go`
  (current-failed tracking → `FailedCount`, `failed` gauge, GRAPH_STATUS envelope
  reason breakdown), the production escape hatch in `pkg/fusion` / `graph/query`
  (relay failure detail from the GRAPH_STATUS watch they already hold), and an
  opt-in `Status==failed` filter on the message-logger EMBEDDING_INDEX read
  (`service/message_logger_http.go`) — which stays **off by default**.
  `worker.go` truncation is NOT changed (already lane-independent); a regression
  test locks it (#627).
- **Metrics:** new `failed` gauge + `{reason}` counter (per-registry
  register-or-get, no process-global).
- **Wire:** `GRAPH_STATUS` envelope gains failure-detail fields — additive,
  `omitempty`, wire-compatible.
- **Behavior (BREAKING):** graph-embedding readiness reports `degraded` under
  failures where it previously reported `ready`. Requires a relevant e2e tier
  green before the tag (CLAUDE.md hard rule) — statistical/semantic embedding
  tier, extended with a semembed-down → degraded + failure-detail scenario.
- **Products consuming:** `semsource` and `semboids` (both run embedding and
  gate on its readiness; the multi-process posture makes the #630 process-local
  vs distributed decision consequential for them).

## Non-goals

- **Not** making the failed→degraded threshold configurable (owner-decided:
  `FailedCount > 0` is true and simple; no knob). `ComputeIndexStatus` gains a
  `FailedCount` projection input, but that is not an operator knob.
- **Not** changing `#627`'s truncation code (already lane-independent since inc 1)
  or building its Option-2 fetch-skip — verify-test-and-close only.
- **Not** changing `graph-index`'s readiness semantics — its `failedCount > 0 →
  degraded` is already correct and stays as-is.
- **Not** a retry/backoff scheduler or durable repair loop for failed embeddings
  beyond the natural re-delivery reprocess — a durable repair loop is #625 (Epic
  C, derived-state ownership).
- **Not** the coalescer resurrection-via-pending-lane fix (#629, Epic C — a
  cross-bucket ordering protocol, and dormant since no shipped config enables
  `coalesce_ms > 0`).
- **Not** the BM25 tier redesign (#619 — deferred owner-decision; its
  query-pollution interim already shipped) or orphaned-blob GC (#633 — deferred,
  owner-accepted growth).

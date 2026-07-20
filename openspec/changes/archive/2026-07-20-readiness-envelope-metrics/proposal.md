# Proposal: Expose the readiness envelope as Prometheus metrics

## Why

Both producers of the ADR-066 honest-readiness envelope — graph-index and
graph-embedding — **compute** `Ready` / `State` / `IndexedRevision` /
`TargetRevision` / `Lag` (via `computeIndexStatus` and the `revlag.Watermark`)
and **answer** them over NATS (`graph.index.query.status` /
`graph.embedding.query.status`), but **publish none of them as metrics**.
graph-index's 7 metrics are all throughput counters (events/updates/kv-ops/watch/
failures/reindex/reconcile); graph-embedding exposes `embedder_type` + `pending`
but no readiness/lag/watermark. So an operator cannot dashboard or alert on index
readiness or lag without issuing a NATS request per sample — the exact
silent-staleness class #579 warned about, at the source (semboids finding).

This is the **producer** side of the observability story #590 opened on the
**consumer** side (`graph_clustering_index_lag_at_detection`). The honest numbers
already exist; they just are not scrapeable.

## What Changes

- graph-index and graph-embedding each publish gauges for the readiness envelope:
  `readiness` (1 when Ready, else 0), `lag` (revisions behind target),
  `indexed_revision`, `target_revision`, and a `state`-labeled gauge (one-hot
  over building/ready/degraded/reset_required so an operator can distinguish
  "catching up" from "broken"). Names under each component's existing metric
  namespace.
- Values are freshened on a periodic tick that calls the already-existing
  status computation (no new computation, no wire change) — reusing each
  component's existing periodic loop where clean, else a dedicated lightweight
  status tick.
- Add a `graph-index-readiness` spec requirement pinning that the envelope is
  exposed as metrics, not only over NATS — so it can't silently regress to
  NATS-only.

## Capabilities

### Modified Capabilities

- `graph-index-readiness`: adds the requirement that every producer of the
  readiness envelope exposes it as Prometheus gauges.

## Impact

- `processor/graph-index/metrics.go` + component (tick wiring);
  `processor/graph-embedding/metrics.go` + component. No wire/contract change to
  `graph.index.query.status` / `graph.embedding.query.status` — additive metrics.
- Operators (and semboids) can scrape readiness/lag/watermark directly.

## Non-goals

- No change to `computeIndexStatus` / the NATS status envelope semantics.
- Not adding metrics to non-envelope producers (this is scoped to the ADR-066
  readiness envelope).
- Not a new readiness gate — purely exposing what is already computed.

# Tasks — graph-ingest ingest observability + backpressure (gh#480, part 1)

## 1. Metrics

- [ ] 1.1 Add `graph_ingest_processing_duration_seconds` (Histogram) via the existing
      `sync.Once` getter + `MetricsRegistry` pattern (`component.go:41-61`,
      `RegisterHistogram`/`RegisterHistogramVec`). Buckets tuned for sub-ms→seconds
      (the merge+CAS is ~1.5ms; see `pkg/worker/pool.go:125-129` for a template).
- [ ] 1.2 Add `graph_ingest_ingest_lag_seconds` (Histogram), same pattern.
- [ ] 1.3 `Component` struct fields + constructor assignment near `component.go:470`.
- [ ] 1.4 Observe in the consume closure (`component.go:983`): `lag = time.Since(
      msg.Metadata().Timestamp)` at entry (guard the `Metadata()` error — on error skip
      the lag observation, do not nil-deref); `start := time.Now()`;
      `c.handleMessage(...)`; observe `processing = time.Since(start)`. Metrics only —
      no change to ack/dispatch/order.

## 2. Backpressure knob (`max_ack_pending`)

- [ ] 2.1 Add `MaxAckPending int json:"max_ack_pending,omitempty"` to `JetStreamPort`
      (`component/port_jetstream.go:10`) and to `component.ConsumerConfig`
      (`:77`); copy it in `applyJetStreamConsumerConfig` (`:123`).
- [ ] 2.2 graph-ingest maps `ConsumerConfig.MaxAckPending` →
      `StreamConsumerConfig.MaxAckPending` (`component.go:972` mapping block); already
      honored at `natsclient/stream.go:320`.
- [ ] 2.3 Fix the stale `natsclient/stream.go:46` comment ("0 means unlimited" → "0 =
      server default 1000; -1 = unlimited").

## 3. Tests

- [ ] 3.1 Metrics unit/integration: a message ingested through the real consume wire
      records both histograms (processing observed; lag ≈ message age). Use
      `natsclient.NewTestClient(WithKV())`.
- [ ] 3.2 Round-trip: a `JetStreamPort` config with `max_ack_pending` reaches
      `StreamConsumerConfig.MaxAckPending` (closes the plumbing gap gh#480 flagged).
- [ ] 3.3 Schema: `max_ack_pending` is schema'd on the port — regenerate + assert no
      unexpected drift beyond the new field.

## 4. Gates + close

- [ ] 4.1 `openspec validate graph-ingest-ingest-metrics --strict`.
- [ ] 4.2 `go test -race` (graph-ingest, component), `task lint`, `task schema:generate`
      + `git diff` (commit the new schema), `go vet -tags=integration`.
- [ ] 4.3 semstreams-reviewer pre-merge (metric registration follows the pattern; the
      port plumbing round-trips; `Metadata()` error handled; no behavior change).
- [ ] 4.4 Archive → ADD to `openspec/specs/graph-ingest`. PR; CI; merge. (No e2e tier /
      no ADR needed — additive observability + config field, no ordering change.)
- [ ] 4.5 Note on gh#480 that part 1 (measurability + backpressure) shipped; ADR-072
      keyed dispatch remains deferred.

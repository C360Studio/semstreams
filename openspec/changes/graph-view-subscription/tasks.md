# Tasks: graph-view-subscription

## 1. Design gate (DONE — this change)

- [x] 1.1 Reuse scan + corrected site sweep (re-verified 2026-07-19 by the
      5-lens adversarial review): 12 steady-state hand-rolls in four shapes +
      2 in-repo per-client SSE surfaces; only the subscriber fan-out seam is
      new; four-class migration scope in ADR-081 (serving surfaces convert,
      per-revision pipelines stay raw, bounded-cache converts multi-reader
      only)
- [x] 1.2 ADR-081 recording the decision, alternatives (incl. rejected shared
      raw-rate feed), coherence contract G1–G6, and the four-class migration
      table (revised 2026-07-19 post-review)
- [x] 1.3 Capability spec delta: single-watcher validated projection,
      coalescing with tombstones in-lane, snapshot/delta consistency,
      coherence across attach + tick seams, readiness gate + watermark,
      watcher-loss fail-closed, poison surfacing, backpressure, point reads,
      view lifecycle/ownership, coexistence (revised 2026-07-19 post-review)
- [x] 1.4 Cluster boundary: #571 PARTIALLY subsumed (multi-reader half), #340
      docs-referent, #176 orthogonal, #211 downstream; two natsclient
      ergonomics split out
- [x] 1.5 5-lens adversarial review (architect / breaker / feasibility /
      code-accuracy / completeness) + adversarial-verify pass; upheld findings
      folded into ADR + spec + design + proposal (2026-07-19)

## 2. Build (GREEN-LIT 2026-07-19 — owner approval after review + #579 confirmation)

- [ ] 2.1 `pkg/graphview` core: one-watcher projection (LWW/key, tombstones
      in-lane), validating decode-once with `(T, keep, err)` injected decode,
      plain apply-sequence under the projection mutex, in-package revision
      coalescer (lift semantics from graph-index's unexported
      `revisionCoalescer`; `pkg/cache/coalescing_set.go` is keys-only —
      insufficient)
- [ ] 2.2 Fan-out seam: `SnapshotAndSubscribe` atomic at S (capture under the
      lock, delivery outside it), delta-only attach for trigger-shaped
      consumers, per-subscriber LWW buffer bounded by changed-key cardinality
      (values shared; slowness never disconnects), detach releases state,
      shutdown delivers explicit terminal close
- [ ] 2.3 Degraded paths (G5/G6): readiness gate on initial replay + caught-up
      watermark exposure; watcher-loss fail-closed staleness signal to every
      subscriber; re-bootstrap ghost-key reconciliation; per-key poison
      surfacing (typed, ADR-079 semantics)
- [ ] 2.4 Coherence regression suite (mirror
      `cache_stale_repopulation_integration_test.go`): racing attach; the
      TICK seam (detach batch → newer apply → attach → resume delivery —
      revision-guarded enqueue proven); bootstrap-attach gating; watcher-loss
      + re-bootstrap reconcile; poison surfacing; delete delivery to attached
      subscribers
- [ ] 2.5 Observability (Prometheus): caught-up/staleness gauge, per-subscriber
      pending + drop/coalesce counters, poison counter
- [ ] 2.6 First consumer migration: agentic-dispatch AGENT_LOOPS SSE activity
      stream (`http.go:902`) reads from a shared view — N per-client watchers
      → 1 (the in-repo #579 shape)
- [ ] 2.7 `-race` unit + integration, contract tests, `/preflight`;
      semstreams-reviewer concurrency review of the fan-out + tick seams

## 3. Follow-ups (separate changes/issues)

- [ ] 3.1 Split-out natsclient ergonomics: attribute the `nats: slow consumer`
      log (subject/subscription); expose watcher pending-limit config
- [ ] 3.2 message-logger KV-watch SSE (`message_logger_kv_watch.go:216`) reads
      from shared views (per-bucket)
- [ ] 3.3 Serving-projection migrations: community_cache + delta-only
      enhancement-worker attach (COMMUNITY_INDEX 2→1); embedding vector cache
      + delta-only worker attach (EMBEDDING_INDEX 2→1)
- [ ] 3.4 graph-query client (#571, multi-reader processes ONLY): view deltas
      drive cache invalidation + poison latch via G6; single-reader embedder
      processes stay as-is (memory floor); coordinate latch layering with the
      ADR-079 poison-scoping track
- [ ] 3.5 #340 docs: point "current-state projection" guidance at the view;
      note intermediate revisions live in the raw KV lane (spec G4)
- [ ] 3.6 Dead code: remove or repurpose `graph/inference/storage.go:429`
      `Watch` (no production caller)

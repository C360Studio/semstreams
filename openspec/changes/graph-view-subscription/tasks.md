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

- [x] 2.1 `pkg/graphview` core: one-watcher projection (LWW/key, tombstones
      in-lane), validating decode-once with `(T, keep, err)` injected decode,
      plain apply-sequence under the projection mutex, in-package revision
      coalescer (reimplemented — graph-index's `revisionCoalescer` is
      unexported/processor-layer) (DONE 2026-07-19)
- [x] 2.2 Fan-out seam: `SnapshotAndSubscribe` atomic at S (capture under the
      lock, delivery outside it), delta-only `Subscribe` for trigger-shaped
      consumers, per-subscriber LWW buffer bounded by changed-key cardinality
      (values shared; slowness never disconnects), `Unsubscribe`/ctx-cancel
      releases state, `Stop` delivers explicit terminal close (DONE 2026-07-19)
- [x] 2.3 Degraded paths (G5/G6): readiness gate on initial replay
      (`WaitCaughtUp` + typed not-ready) + `AppliedRevision` watermark;
      watcher-loss fail-closed terminal signal to every subscriber;
      `Restart()` re-bootstrap with ghost-key reconciliation; per-key
      `PoisonError` surfacing in-band, heal-on-newer-write (ADR-079
      semantics) (DONE 2026-07-19)
- [x] 2.4 Coherence regression suite (mirror
      `cache_stale_repopulation_integration_test.go` style): racing attach;
      the TICK seam (proven via critical-section fan-out — batch detach +
      subscriber iteration + enqueue under the projection mutex, per-key
      attach-seq filter; in-tree proof is the marker-write negative test,
      mutation checks were manual during development); bootstrap-attach
      gating; watcher-loss + re-bootstrap reconcile; poison surfacing; delete
      delivery to attached subscribers; all `-race`, no sleeps, plus one
      testcontainers integration test over a real bucket (DONE 2026-07-19)
- [x] 2.5 Observability (Prometheus, component-side via `WithHooks` +
      `OnSubscribers`/`OnFanOut`; pkg/graphview stays Prometheus-free):
      caught-up gauge, applied-revision watermark, subscriber gauge,
      max-pending + coalesced-drops, poison counter, watcher-lost counter
      (DONE 2026-07-19)
- [x] 2.6 First consumer migration: agentic-dispatch AGENT_LOOPS SSE activity
      stream reads from ONE shared view (was one `WatchAll` per SSE client —
      the in-repo #579 shape). Wire parity test-pinned incl. KV-write-time
      timestamps via the `EntryMeta` decode seam; watcher-loss fails closed
      with restart-on-next-attach; slow-client isolation proven
      (DONE 2026-07-19)
- [x] 2.7 `-race` unit + integration, contract tests, `/preflight`
      (`task check:push` all stages green) + `task e2e:agentic` green for the
      touched dispatch path; semstreams-reviewer concurrency review of the
      fan-out + tick seams: APPROVE-WITH-NITS — both seams traced structurally
      correct; the one MEDIUM (openWatcher Restart-vs-Stop window) fixed with
      a mutation-verified regression test; lows fixed or documented
      (DONE 2026-07-19)

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

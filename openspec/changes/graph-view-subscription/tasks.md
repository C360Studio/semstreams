# Tasks: graph-view-subscription

## 1. Design gate (DONE — this change)

- [x] 1.1 Reuse scan: confirm every piece is hand-rolled + locate the missing
      fan-out seam (DONE 2026-07-19 — ~7 projection hand-rolls, coalescing ×2,
      watermark, the ADR-079 coherence guard; only the subscriber fan-out seam is
      new; per-site migration verified in ADR-081)
- [x] 1.2 ADR-081 recording the decision, alternatives, coherence contract, and
      the verified per-site cleanup table (DONE 2026-07-19)
- [x] 1.3 Capability spec delta (view lifecycle, coalescing, snapshot/delta
      consistency, coherence G1–G4, backpressure, coexistence) (DONE 2026-07-19)
- [x] 1.4 Cluster boundary: #571 subsumed (watcher-tax), #340 docs-referent, #176
      orthogonal, #211 downstream; two natsclient ergonomics split out (DONE)

## 2. Build (DEFERRED — owner-gated; do NOT start without go-ahead)

- [ ] 2.1 `pkg/graphview`: one-watcher projection (LWW/key, trusted-decode fast
      path) + `revlag.Watermark` sequencing + revision-coalescer view tick
- [ ] 2.2 `SnapshotAndSubscribe` fan-out seam: atomic snapshot+register at one
      sequence S; per-subscriber bounded coalescing buffer (at-most-once, drop to
      staleness, never blocks the watcher or peers)
- [ ] 2.3 Coherence: apply the ADR-079 ABA-generation guard to the attach seam;
      deterministic regression test (mirror `cache_stale_repopulation_integration_test.go`)
      proving no gap / dup / stale-over-newer inversion under a racing attach
- [ ] 2.4 First consumer migration: graph-query reads ENTITY_STATES from the shared
      view (retires the #571 per-write watcher tax); keep the poison latch layered
      per the ADR-079 track
- [ ] 2.5 `-race` unit + integration, contract tests, `/preflight`; semstreams-reviewer
      concurrency review of the fan-out seam

## 3. Follow-ups (separate changes/issues)

- [ ] 3.1 Split-out natsclient ergonomics: attribute the `nats: slow consumer` log
      (subject/subscription); expose watcher pending-limit config
- [ ] 3.2 Incremental migration of the remaining fit-sites (community_cache +
      clustering worker → shared COMMUNITY_INDEX view; embedding/inference/graph-index)
- [ ] 3.3 #340 docs: point "current-state projection" guidance at the view; note
      intermediate revisions live in the raw KV lane (spec G4)

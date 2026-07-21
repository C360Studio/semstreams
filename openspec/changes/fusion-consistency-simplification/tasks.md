# fusion-consistency-simplification — tasks

## 1. Decision record

- [x] 1.1 Write `docs/adr/084-readiness-licenses-health-not-absence.md`
      (decision-only). DONE 2026-07-20; includes the review-driven reshaping
      (bootstrap_complete, coverage-may-license-proceed, hard-stop scoping,
      staleness-is-a-floor). Narrow pointer notes on ADR-066/082/083 land
      with 5.1 — no retrofits.
- [x] 1.2 Adversarial multi-lens review of ADR-084 before Accept. DONE
      2026-07-20: 5 lenses (architect / breaker / feasibility /
      code-accuracy / completeness), READY-WITH-CHANGES; 3 blocking + 14
      high/medium findings verified against source and folded into the ADR,
      deltas, design, and this task list (see design.md "Review record").
      ADR marked Accepted.

## 2. Envelope + gate collapse

- [x] 2.0 `bootstrap_complete` on the envelope (D2): field on
      `graph.IndexStatusResponse` + `fusion.IndexStatus` mirror (lockstep
      comment updated); graph-index publishes from its bootstrap latch
      (true for the authoritatively-empty 0/0 outcome; resets on restart);
      graph-embedding publishes from its own bootstrap; production-decoder
      round-trip tests both sides; absent-field-reads-false documented.
- [x] 2.1 Collapse `EvaluateReadinessGate` (D1): delete `GateMode`; health =
      fresh ∧ no hard stop ∧ bootstrap_complete; freshness parameter
      exact | max_staleness | none; keep the `Ready` caught-up fast path
      (licenses proceed, never defers); compare `StalenessMs +
      reading.Age` against the bound; rename defer reason `empty` →
      `bootstrap_incomplete`; rewrite the stale bit-parity comment. DONE:
      `GateMode`/`GateConfig` deleted; `Freshness` is a constructor-only type
      (`FreshnessExact`/`Within`/`None`) whose ZERO VALUE is exact, so an
      uninitialised field fails toward withholding; `StatusReading` carries
      Fresh+Age so the bound is judged against staleness+age. Stickiness is no
      longer a gate concept — it is the responder's local latch.
- [x] 2.2 graph-index responder: retained UNCHANGED (its pre-bootstrap
      exactness IS bootstrap_complete in-process; reset/failedCount checks
      stay per-query; do NOT fold status ahead of the sticky flag — the
      post-bootstrap stuck-degraded serving exception is deliberate,
      query.go:176-182). DONE: only the gate call and its doc changed; check
      order and the sticky short-circuit are untouched.
- [ ] 2.3 Pins land BEFORE the regate (tests-first): authoritatively-empty
      graph proceeds everywhere; caught-up index + declared bound proceeds
      (presence-encoding wedge); client health gate defers on
      `bootstrap_complete=false` under `State=building` (cutover); responder
      pre-bootstrap + healthy + Lag>0 still returns the transient;
      failedCount→degraded override holds with its ≤1-heartbeat envelope
      latency noted; ranking fixture pinning current resolve-order semantics
      (before 4.3's re-order lands, then updated to pin the fix).
- [x] 2.4 Migrate the graph-clustering call site (component.go:1256 — the
      review found it missing from this list) to the collapsed gate,
      preserving unset/0 `max_staleness` = exact catch-up bit-for-bit (via
      `FreshnessWithin`'s non-positive⇒exact rule); its config schema text
      stays accurate.

## 3. Read-path regate (deliberate #592 supersession)

- [x] 3.1 `graph/query/client.go` `indexNotReadyErr`: health question via
      the collapsed gate (needs 2.0); serves under ordinary lag on a
      healthy, built index; fail-closed unknown branch and
      `AllowUngatedReads` scope unchanged. DONE: declares `FreshnessNone`;
      the gate matrix test is rewritten to pin the reversal (healthy+lagging
      SERVES) alongside the rows that deliberately did not move — every
      unknown shape still fails closed, the escape still never applies to a
      received status, code/class unmoved. A `preBootstrap` fixture pins the
      gh#474 cutover still deferring.
- [ ] 3.2 graph-index responder: verify-only (near-no-op per review — the
      responder never lag-gates post-bootstrap today). Confirm with tests;
      no narrowing applied to `ensureQueryReady` beyond 2.2's constraint.
- [ ] 3.3 Sweep every `ErrorCodeIndexNotReady` emitter/consumer — full list
      from the review: graph/query client.go:321 (watch-lost latch —
      process-lifetime, never rebinds: audit and document restart-required
      or add rebind), graph-index (failedCount/bootstrap — stays),
      graph-ingest (query.go:407,411, component.go:838), graph-embedding
      (component.go:1177,1181, readiness.go:78), spatial (782,786,323),
      temporal (813,817,333), rule entity_watcher.go:45,61, lifecycle
      manager_query.go:228,342,351,419, fusion isIndexNotReady consumers.
      Verify each site's meaning survives the narrowing (all are
      responder-up/watcher-health shaped — confirm, don't assume).

## 4. Fusion regate + unhydrated reporting + scores

- [ ] 4.1 `Fuse` ADOPTS the canonical gate (today hand-rolled `!Ready`,
      engine_lens.go:87) with freshness=none: proceed under lag reporting
      `staleness_ms`; empty-honest envelope only on health defers. Status
      surface split (D6): fusionnats returns a typed readiness-unknown for
      quiet/stale feeds (engine → defer envelope) while wiring failures
      stay loud errors; no ungated escape.
- [x] 4.2 `graph.query.batch` handler (graph-ingest/query.go): `missing:
      [{id, reason}]`, closed enum, first-error contract preserved (`error`
      reserved); fix the stale "never a silent omission" comment; structured
      log + counter for missing-per-call (the gh#597 soak instrumentation:
      was the dropped ID's Get not-found at fail time?). DONE: typed
      `graph.EntityBatchResponse` + `MissingEntity`/`MissingReason` (closed
      set incl. the reserved `error` and client-only `unknown`);
      `fetchEntitiesConcurrent` returns missing IDs (the caller cannot
      recover them — the entity slice carries no correspondence to the
      request); `semstreams_graph_ingest_batch_query_missing_total{reason}`
      + a bounded-ID Warn; prefix caller deliberately ignores missing (its
      IDs came from a live key scan). Integration test pins reconcilability
      AND that a complete batch's raw bytes are unchanged.
- [ ] 4.3 `fusionnats.Entities`: ID-set reconciliation (handler report
      authoritative; synthesize `unknown` for IDs in neither list; one entry
      per ID) + restore resolve order before returning (fixes the live
      cache-order ranking scramble); production-decoder round-trip for the
      new fields; wire pins live in package tests (test/contract does not
      cover fusion — review-verified).
- [ ] 4.4 fusion `Response.unhydrated` (distinct from `Misses`;
      all-seeds-unhydrated synthesizes no Miss) + Miss de-license + doc
      sweep: contract.go:65 ("Only Ready permits a not-found conclusion"),
      contract.go:127-128 ("Miss only appears when Ready is true"),
      retrieval.go:18-19,31, graph-query passthrough comment
      (query.go:183-185), graph-ingest Phase-3 comment; JSON round-trip +
      default-wire-shape-unchanged tests.
- [ ] 4.5 Score passthrough (D5): rank always (post-reorder position),
      similarity where the resolve mode provides one (semantic decode
      struct gains the field), joined by entity ID; opt-in request bool
      with JSON round-trip test; omitempty wire fields.
- [ ] 4.6 `processor/research-graph-execute/adapters.go` (second in-repo
      batch consumer, found by review): reconcile `EntityState` against the
      requested set (or consume `missing`), and fix its comment blessing
      silent omission.
- [x] 4.7 Pin `kv_revision` (mutation responses) and the envelope's
      `IndexedRevision` as the same revision space with a test — ADR-084
      promotes read-your-writes to the one sound per-entity check and
      nothing exercises the comparison today.

## 5. Docs + migration

- [ ] 5.1 Migration notes joining the ADR-083 wave (one release-note set):
      gate meaning change, transient narrowing, `bootstrap_complete`,
      unhydrated/missing consumption, score opt-in, staleness-is-a-floor;
      explicit "what `Ready=false → fall back` becomes" section for
      semsource. UPDATE `docs/operations/migration-readiness-distribution-
      adr083.md` in place: its consumer snippet compile-breaks (GateExact),
      and its `max_staleness` "0 = exact" line is reaffirmed. Pointer notes
      on ADR-066/082/083.
- [ ] 5.2 At spec-sync time, rewrite the `graph-index-readiness` spec
      Purpose paragraph (still teaches the absence license verbatim; deltas
      cover requirements only). No docs/concepts pages teach gate modes
      (review-verified) — no further doc retargets.
- [ ] 5.3 File the fusion e2e coverage-gap issue: no e2e tier exercises
      `Fuse`'s gate, `Entities` reconciliation, or `unhydrated` (house
      rule: tier doesn't cover the path ⇒ file the gap before tagging).

## 6. Gates (all BEFORE merge)

- [ ] 6.1 `task lint` · full `go test -race ./...` (explicit FAIL grep) ·
      `task schema:generate` no-drift · contract tests ·
      `go vet -tags=integration` AND `-tags=live_llm` ·
      `openspec validate --strict`.
- [ ] 6.2 Branch integration sweep (`go test -race -tags=integration ./...`)
      — framework-package change (graph/, pkg/fusion).
- [ ] 6.3 BREAKING ⇒ e2e: `task e2e:statistical` AND `task e2e:semantic`
      green, with log-level evidence (not exit codes through a pipe).
- [ ] 6.4 `semstreams-reviewer` pre-merge; fold findings.

## 7. Close-out

- [ ] 7.1 PR + owner merge; tag TOGETHER with #598's breaks (owner
      sequencing: no semsource tag before this change).
- [ ] 7.2 gh#597 comment: part 1 shipped (drop path closed + resolve-order
      ranking fix), part-2 minimal slice shipped; REMAINS OPEN: the
      cross-store consistency gap (semantic index ranking an ID whose
      ENTITY_STATES read returns not-found) — now visible via the 4.2
      counter; file separately if the soak confirms it.
- [ ] 7.3 gh#592 comment: close-out superseded deliberately for read paths
      (ADR-084); reopen trigger retired.
- [ ] 7.4 Archive change + update memory; sister lockstep PRs remain
      owner-managed (with #598's wave).

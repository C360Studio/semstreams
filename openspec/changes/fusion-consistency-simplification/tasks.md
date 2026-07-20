# fusion-consistency-simplification — tasks

## 1. Decision record

- [ ] 1.1 Write `docs/adr/084-readiness-licenses-health-not-absence.md`
      (decision-only): readiness licenses health, never absence; ADR-066's
      "authoritative not-found" license retired; ADR-083 D4's mode table
      superseded by the two-question gate; #592's close-out superseded (the
      transient narrows to health windows). Narrow pointer notes on
      ADR-066/082/083 — no retrofits.
- [ ] 1.2 Adversarial multi-lens review of ADR-084 before Accept
      (house rule for framework ADRs); fold findings.

## 2. Gate collapse (graph/readiness_gate.go)

- [ ] 2.1 Collapse `EvaluateReadinessGate` to the two-question evaluation
      (health + optional `max_staleness`); delete `GateMode`; keep the typed
      defer reasons (`hard_stop | over_staleness | status_unknown | empty`).
      Coverage (`Ready`/`Lag`) stops being a gate input; stays on the envelope.
- [ ] 2.2 graph-index absorbs sticky-bootstrap privately (its query gate keeps
      bootstrap exactness internally; hard stops unchanged).
- [ ] 2.3 Pin with tests BEFORE the regate lands: gh#474 cutover window still
      defers (bootstrap-incomplete, degraded, reset_required), and the
      unconditional failedCount→degraded override (ADR-082) still holds —
      these are the load-bearing health signals the new gate trusts.

## 3. Read-path regate (deliberate #592 supersession)

- [ ] 3.1 `graph/query/client.go`: reverse-index reads proceed under ordinary
      lag on a healthy index; `indexNotReadyErr` fires only on health failures.
- [ ] 3.2 graph-index read handlers: same narrowing at the responder side;
      the classified transient's meaning is now health, not catch-up.
- [ ] 3.3 Sweep every `ErrorCodeIndexNotReady` emitter and consumer
      (sweep-all-emitters discipline): lifecycle/rule/spatial/temporal sites
      use it for responder-up, NOT catch-up — verify each still means what its
      call site thinks after the narrowing.

## 4. Fusion regate + unhydrated reporting + scores

- [ ] 4.1 `Fuse` top gate → canonical health gate: proceed under lag reporting
      `staleness_ms`; empty-honest envelope only on health defers; the
      `isIndexNotReady` degrade sites keep their shape (now health-scoped).
- [ ] 4.2 `graph.query.batch` handler (`processor/graph-ingest/query.go`):
      report unreturned IDs with not-found vs fault reasons; preserve partial
      success and the existing first-error contract.
- [ ] 4.3 `fusionnats.Entities`: reconcile returned-vs-requested; synthesize
      unknown-reason entries for a mixed-version handler; production-decoder
      round-trip test for the new wire fields.
- [ ] 4.4 fusion `Response`: `unhydrated` list (distinct from `Misses`), with
      the licenses-no-absence doc contract; JSON round-trip + wire-shape tests
      (default shape unchanged for non-requesting callers).
- [ ] 4.5 Score passthrough: `resolveSemantic` keeps `Similarity`; opt-in
      request flag exposes per-node resolve rank + score (omitempty; default
      wire unchanged).

## 5. Docs + migration

- [ ] 5.1 Migration notes joining the ADR-083 wave (one release-note set, one
      sister migration): gate meaning change, transient narrowing, unhydrated
      consumption, score opt-in. Explicit "what `Ready=false → fall back`
      becomes" section for semsource.
- [ ] 5.2 Update `docs/concepts` pages that teach the old gate modes.

## 6. Gates (all BEFORE merge)

- [ ] 6.1 `task lint` · full `go test -race ./...` (explicit FAIL grep) ·
      `task schema:generate` no-drift · contract tests ·
      `go vet -tags=integration` AND `-tags=live_llm` ·
      `openspec validate --strict`.
- [ ] 6.2 Branch integration sweep (`go test -race -tags=integration ./...`) —
      framework-package change (graph/, pkg/fusion).
- [ ] 6.3 BREAKING ⇒ e2e: `task e2e:statistical` AND `task e2e:semantic`
      green, with log-level evidence (not exit codes through a pipe).
- [ ] 6.4 `semstreams-reviewer` pre-merge; fold findings.

## 7. Close-out

- [ ] 7.1 PR + owner merge; tag TOGETHER with #598's breaks (owner sequencing:
      no semsource tag before this change).
- [ ] 7.2 gh#597 comment: part 1 shipped (drop path closed), part 2 minimal
      slice shipped; remaining score-explain scope, if any, stays open.
- [ ] 7.3 gh#592 comment: close-out superseded deliberately (ADR-084), with
      the reopen trigger retired.
- [ ] 7.4 Archive change + update memory; sister lockstep PRs remain
      owner-managed (with #598's wave).

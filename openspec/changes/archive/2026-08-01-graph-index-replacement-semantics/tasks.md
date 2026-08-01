## 1. Store and Query Contract

- [x] 1.1 Pin the complete ownership/filter matrix (layout, arity, owner, literal forward/owner filters,
      overwrite, lifecycle, reset, readiness) in table-driven tests; every literal filter passes the
      `nats-kv-keys` validators. (Carries over: selected-layout unit maxima at `E = 256` already proven —
      451/710/902/256.)
      Evidence: `kv_contract_test.go`, `owner_reconcile_test.go`, and `owner_filter_integration_test.go` pin the
      selected layouts, filters, bounds, owner axes, overwrite behavior, and lifecycle controls.
- [x] 1.2 Define and test the INCOMING source-ownership contract: source fact replacement, source
      removal/tombstone, target retirement preserving live-source assertions, authorized cascade — without
      production wiring.
      Evidence: `owner_reconcile_test.go` and `replacement_reconcile_integration_test.go` cover source replacement,
      source deletion, target retirement, and preservation of assertions owned by a live source.
- [x] 1.3 Freeze public acceptance fixtures for `[A] -> [B] -> []` at the graph-index status watermark across
      OUTGOING, exact/value predicate, list, stats, compound, by-name, incoming, and traversal queries; track
      clustering only after its independently completed next detection cycle.
      Evidence: `replacement_reconcile_integration_test.go` proves public watermark, replacement, restart, repair,
      traversal, predicate-list, and a later production clustering cycle; focused query tests cover other surfaces.
- [x] 1.4 Record ALIAS, spatial/geohash, embedding, blob/ObjectStore, cascade, and global GC as separately owned.
      Evidence: ADR-077's ownership matrix and explicit exclusions preserve these as separate obligations.

## 2. Owner-Filter Proof (real NATS)

- [x] 2.1 Correctness: exact match sets, malformed shorter/longer keys, neighboring-owner and reversed-axis
      controls, concurrent Put/Delete with exact-key dedup before diffing, cancellation, empty buckets, restart,
      clean bucket recreation, convergence to a declared final ENTITY_STATES revision. (Carries over: `[A] -> [B]
      -> []` stale-deletion/insertion proofs and NAME stable-key overwrite already landed in isolated tests.)
      Evidence: `owner_filter_integration_test.go`, `owner_reconcile_test.go`,
      `owner_filter_load_integration_test.go`, and `replacement_reconcile_integration_test.go` provide the named
      correctness fixtures. Performance acceptance remains task 2.2.
- [x] 2.2 Performance: existing ADR-065 5k/3s CI guard + one sustained-churn run on the 21k profile at the
      configured worker shape and one stress shape; record latency (p95 ≤ 3s, p99 ≤ 5s, none at 10s), ingest
      throughput, queue growth, catch-up time, temporary-consumer high-water/return-to-baseline.
      Evidence: revision-pinned 5k/4-worker and 21k/4+16-worker results in
      `docs/operations/32-predicate-layout-smoke-harness.md` record every required latency, throughput, queue,
      catch-up, consumer, resource, convergence, and restart gate.
- [x] 2.3 Select and enforce the graph-index worker maximum in validated configuration.
      Evidence: `maxGraphIndexWorkers` is 16; `Config.Validate` and `component_test.go` reject larger values.
- [x] 2.4 Add malformed-axis and complete-key/filter pre-I/O controls for production CONTEXT, OUTGOING, and every
      remaining graph-index I/O path (watcher, Get, lister, Put, Delete) before activation. Obligation inherited
      from the superseded fixed-arity change's entity-id-contract handoff; it is not waived by the split.
      Evidence: `review_remediation_test.go`, `owner_reconcile_test.go`, and
      `replacement_reconcile_integration_test.go` prove rejection before derived-index I/O and fail-closed state.
- [x] 2.5 Pass pinned real-NATS maximum key/filter exact-match conformance for every bounded graph-index layout at
      the canonical 256-byte entity bound; representative-corpus success does not substitute for governed maxima.
      Inherited obligation — the shared unit bound is a prerequisite, not duplicate proof.
      Evidence: the clean revision-pinned harness passed PREDICATE 451, NAME 710, INCOMING 902, CONTEXT 710, and
      real-NATS OUTGOING 256-byte Put/Get rows on the ADR-pinned server and SDK.

## 3. Decision and Activation

- [x] 3.1 Write and approve the owner-discovery + INCOMING-ownership ADR from the correctness and budget evidence;
      a failed store defers to an explicitly specified dependent bounded mechanism, which becomes a completion
      dependency of this change.
      Evidence: ADR-077 was accepted on 2026-07-17. Activation remains gated by open task 3.2 and the remaining
      closeout and product release gates.
- [x] 3.2 Activate reconciliation for NAME, PREDICATE, and source-owned INCOMING inside the announced pre-v1
      wipe/reseed: affected buckets initialize behind typed not-ready, rebuild from reseeded canonical
      ENTITY_STATES, readiness held until the authoritative replay watermark. Preserve keyed ordering,
      reconcile-at-execution, bounded repair, exact watermarks.
      Implementation-ready evidence: `component.go`, `owner_reconcile.go`, and
      `replacement_reconcile_integration_test.go` prove the watcher, keyed lane, readiness watermark, replacement,
      repair, and replay path. Check this task only after the coordinated tag and deployment wipe/reseed execute.
      **→ MOVED TO gh#827 (2026-08-01, owner ruling). The CODE is complete and proven — this line's own evidence
      says "Implementation-ready" and names the three files that prove it. What remained was executing a
      DEPLOYMENT event, which is not capability truth and must not gate a spec archive. This is the same shape as
      the 2026-07-30 adoption ruling one layer in ("conforming to the framework is the sister repo's job... does
      NOT gate this archive"). Holding the change open on it made `openspec list` misreport code-complete work as
      partially built and fired the staleness tripwire every session on a change that was correctly parked.
      The activation CONTRACT is not lost by archiving — it is promoted: the delta's "Activation is gated and
      starts from canonical fresh state" requirement carries the fresh-state release contract, the typed
      not-ready initialization, and the readiness watermark into current truth, where `validate --strict` reads
      it. A requirement outranks an unchecked task line that only a reader who opens the file will ever see.**
- [x] 3.3 Replace source-owned INCOMING on source fact change/removal and remove the target-prefix hard-delete,
      covered by readiness.
      Evidence: production deletion uses `incomingIndexSourceFilter`; replacement integration proves source removal
      and target retirement behavior through public queries.
- [x] 3.4 Make predicate-list/namespace-list expose only current non-zero memberships; vocabulary history stays in
      the registry.
      Evidence: `query.go` derives lists from raw current memberships; replacement integration proves empty and
      namespace-list results without a catalog.

## 4. Query Determinism

- [x] 4.1 Sort and deduplicate complete exact, value-filtered, compound, list, namespace-list, and stats results;
      apply limits/samples only on wire surfaces that expose them; preserve existing INCOMING/NAME total orders.
      Evidence: `query.go` centralizes sorted/deduplicated membership results and the public replacement fixtures pin
      stable query ordering before limits.

## 5. Closeout

- [x] 5.1 Prove query parity after update, empty projection, source removal, target retirement, repair, restart,
      and shuffled replay; prove the next completed clustering cycle consumes reconciled truth.
      Evidence: `replacement_reconcile_integration_test.go` covers the complete production path and waits for a
      post-retirement clustering revision containing only live members.
- [x] 5.2 Publish the measured owner-discovery matrix as retention-epic input (no retention policy here); correct
      gh#527 cross-links.
      Evidence: operations guide 32 publishes the revision-pinned matrix and links it to gh#527 without selecting
      retention policy.
- [x] 5.3 Supersede the affected ADR-068 D3 clauses; update KV Twofer, index-reference, reset, and query-ordering
      docs.
      **→ FILED AS gh#828 (2026-08-01), not executed here, and the drift is REAL rather than bookkeeping.**
      ADR-068 D3 lines ~238-243 argue the per-entity reverse index is "genuinely new work" from gh#430's composite
      `hash(predicate).entityID` layout — the layout ADR-078 RETIRED in favour of raw canonical predicates. The
      passage is the load-bearing justification for that conclusion, so the conclusion may survive the change while
      its stated argument no longer holds; a reader cannot tell whether it was re-derived or merely inherited.
      Filed rather than rewritten because **ADR supersession is architect-owned**, and because gh#828 also has to
      rule on whether D3's INCOMING refuse/cascade reasoning survives ADR-077's source-owned retraction — a
      judgment, not an edit.
- [x] 5.4 Run lint, race, contracts, real-NATS integration, structural e2e, and affected product suites.
      **→ RAN 2026-08-01 on merged main `32995167`: `task lint` exit 0 · `go vet` plain AND `-tags=integration`
      clean · `go test -race ./...` 135 ok / 0 FAIL · `-race -tags=integration -p 2 -count=1 ./...` 136 ok / 0 FAIL
      · contract tests green · `task schema:generate` zero drift · `task e2e:structural` GREEN, exit 0.
      The exit code was verified load-bearing before being trusted: `structural` is NOT one of gh#811's five
      `ignore_error` tiers (grepped `taskfiles/` directly rather than trusting the issue text).
      NOTE, recorded rather than buried: `task e2e:semantic` is RED on this same tree (gh#830). It is NOT in this
      task's gate — 5.4 specifies structural e2e — but the sister change `predicate-raw-key-representation`
      requires semantic in its 4.2 and is therefore HELD open on gh#830 rather than archived alongside this one.**
- [x] 5.5 Archive only after every affected query-visible store satisfies `[A] -> [B] -> []` through an approved,
      implemented bounded mechanism (including any delivered by explicit dependent changes).
      **→ SATISFIED BY 5.1's EVIDENCE, confirmed deliberately rather than waved through.**
      `replacement_reconcile_integration_test.go` covers the complete production path — update, **empty
      projection**, source removal, **target retirement**, repair, restart, and shuffled replay — which is the
      `[A] -> [B] -> []` shape end to end, and it waits for a post-retirement clustering revision containing only
      live members. The approved mechanism is ADR-077 (accepted 2026-07-17); 3.3 proves production deletion uses
      `incomingIndexSourceFilter` with the target-prefix hard-delete removed, and 3.4 proves list surfaces expose
      only current non-zero memberships. No dependent change is outstanding.

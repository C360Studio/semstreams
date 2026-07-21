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
- [x] 2.3 Pins land BEFORE the regate (tests-first): authoritatively-empty
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
- [x] 3.2 graph-index responder: verify-only (near-no-op per review — the
      responder never lag-gates post-bootstrap today). Confirm with tests;
      no narrowing applied to `ensureQueryReady` beyond 2.2's constraint.
      CONFIRMED: verified by construction, not assumption — past the sticky
      short-circuit the latch is false, so every envelope reaching the gate
      carries BootstrapComplete=false and short-circuits there; the staleness
      comparison is unreachable. The declared FreshnessExact is about the
      pre-bootstrap probe, now stated in the doc.
- [x] 3.3 Sweep every `ErrorCodeIndexNotReady` emitter/consumer — full list
      from the review: graph/query client.go:321 (watch-lost latch —
      process-lifetime, never rebinds: audit and document restart-required
      or add rebind), graph-index (failedCount/bootstrap — stays),
      graph-ingest (query.go:407,411, component.go:838), graph-embedding
      (component.go:1177,1181, readiness.go:78), spatial (782,786,323),
      temporal (813,817,333), rule entity_watcher.go:45,61, lifecycle
      manager_query.go:228,342,351,419, fusion isIndexNotReady consumers.
      Verify each site's meaning survives the narrowing (all are
      responder-up/watcher-health shaped — confirm, don't assume). DONE:
      19 emission + 4 consumer sites swept. ALL emissions outside
      graph/query survive (watcher-health / bootstrap-incomplete shaped);
      the one real residue was fusion's hand-rolled top gate, fixed in 4.1.
      OWNER DECISION on client.go:321: the watch-lost latch is PERMANENT
      (verified: no clearing writer, and the sole WatchAll bind is
      unreachable once `initialized`) — documented as restart-required with
      a test pinning the permanence and an actionable message; class stays
      transient (sister repos match on it), supervised rebind filed
      separately. Sites the task list MISSED, now fixed:
      `graph/mutation_responses.go` ErrorCodeIndexNotReady docstring (was
      still telling operators to probe the `ready` bit — the definition
      site, highest leverage), graph-index's "still catching up" message +
      doc, graph-embedding component.go:424, and the
      engine_lens_readiness_test fixture doc.

## 4. Fusion regate + unhydrated reporting + scores

- [x] 4.1 `Fuse` ADOPTS the canonical gate (today hand-rolled `!Ready`,
      engine_lens.go:87) with freshness=none: proceed under lag reporting
      `staleness_ms`; empty-honest envelope only on health defers. Status
      surface split (D6): fusionnats returns a typed readiness-unknown for
      quiet/stale feeds (engine → defer envelope) while wiring failures
      stay loud errors; no ungated escape. DONE: `fusion.ErrReadinessUnknown`
      sentinel wraps quiet/stale/undecodable; wiring failures (no KV
      capability, watch-start failure) stay unwrapped and loud. The
      IndexStatus->gate-input projection has a JSON-equality test so a field
      added to both structs but forgotten in the converter fails loudly —
      a drop there silently changes whether fusion serves at all.
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
- [x] 4.3 `fusionnats.Entities`: ID-set reconciliation (handler report
      authoritative; synthesize `unknown` for IDs in neither list; one entry
      per ID) + restore resolve order before returning (fixes the live
      cache-order ranking scramble); production-decoder round-trip for the
      new fields; wire pins live in package tests (test/contract does not
      cover fusion — review-verified).
- [x] 4.4 fusion `Response.unhydrated` (distinct from `Misses`;
      all-seeds-unhydrated synthesizes no Miss) + Miss de-license + doc
      sweep: contract.go:65 ("Only Ready permits a not-found conclusion"),
      contract.go:127-128 ("Miss only appears when Ready is true"),
      retrieval.go:18-19,31, graph-query passthrough comment
      (query.go:183-185), graph-ingest Phase-3 comment; JSON round-trip +
      default-wire-shape-unchanged tests.
- [x] 4.5 Score passthrough (D5): rank always (post-reorder position),
      similarity where the resolve mode provides one (semantic decode
      struct gains the field), joined by entity ID; opt-in request bool
      with JSON round-trip test; omitempty wire fields. DONE, with an OWNER
      DECISION deviating from D5: D5 said "no RetrievalClient.Resolve break
      needed... similarity rides the existing decode struct gaining one
      field", which does not work — Resolve returned []string, so a decoded
      similarity had no path to the engine, and the graph.query.semantic wire
      was already reporting a score fusionnats dropped on the floor. Resolve
      now returns []Seed{ID, Similarity, HasSimilarity}; HasSimilarity keeps
      an unscored mode (symbol/prefix) from advertising a perfect
      zero-relevance match. Join is by entity ID and the test is verified
      non-vacuous — a deliberately reordering fixture plus a stash-check that
      a positional join swaps the two scores and fails. CORRECTED in review
      round 1: `Rank` initially reported the RESPONSE position, which the
      caller can count off the array and which the spec delta does not ask
      for. It is the RESOLVE rank now — the gap between where resolve put an
      entity and where ranking landed it is the whole diagnostic signal.
      `Similarity` later became `*float64` so an available 0.0 is
      distinguishable from "this mode does not score" without a companion
      bool every non-Go consumer would have to learn.
- [x] 4.6 `processor/research-graph-execute/adapters.go` (second in-repo
      batch consumer, found by review): reconcile `EntityState` against the
      requested set (or consume `missing`), and fix its comment blessing
      silent omission. DONE: consumes `missing`, logs it bounded, and emits NO
      Evidence for an unread ID (evidence is a claim about something we read).
      Test pins that a REJECTED batch reports no missing either — an
      unvalidated reply's missing list is not evidence.
- [x] 4.7 Pin `kv_revision` (mutation responses) and the envelope's
      `IndexedRevision` as the same revision space with a test — ADR-084
      promotes read-your-writes to the one sound per-entity check and
      nothing exercises the comparison today.

## 5. Docs + migration

- [x] 5.1 Migration notes joining the ADR-083 wave (one release-note set):
      gate meaning change, transient narrowing, `bootstrap_complete`,
      unhydrated/missing consumption, score opt-in, staleness-is-a-floor;
      explicit "what `Ready=false → fall back` becomes" section for
      semsource. UPDATE `docs/operations/migration-readiness-distribution-
      adr083.md` in place: its consumer snippet compile-breaks (GateExact),
      and its `max_staleness` "0 = exact" line is reaffirmed. Pointer notes
      on ADR-066/082/083. DONE: retitled as ONE wave (two changes, one tag);
      Breaks 4/5/6 added with a prominent "if you read one section" pointer,
      because unlike Breaks 1-3 these compile fine and reach production
      silently. Break 4 carries the explicit
      `Ready == false -> fall back` rewrite for semsource. Upgrade order
      gained steps 6-8. Pointer notes: ADR-066 absence license retired,
      ADR-082 consumer-class split retired (naming WHY the split was a
      symptom), ADR-083 D4 gate-mode table superseded. LATER ADDED, from the
      semboids adoption report + review round 3: `max_staleness` sizing —
      the bound must clear BOTH the readiness heartbeat (the judged age
      includes envelope arrival age, so a sub-heartbeat bound is
      unsatisfiable and is now rejected at config validation) and the
      consumer's own worst-case cycle (detection time scales with community
      size). gh#605 carries the measurements and the open question about a
      derived floor.
- [ ] 5.2 At spec-sync time, rewrite the `graph-index-readiness` spec
      Purpose paragraph (still teaches the absence license verbatim; deltas
      cover requirements only). No docs/concepts pages teach gate modes
      (review-verified) — no further doc retargets.
- [x] 5.3 File the fusion e2e coverage-gap issue: no e2e tier exercises
      `Fuse`'s gate, `Entities` reconciliation, or `unhydrated` (house
      rule: tier doesn't cover the path ⇒ file the gap before tagging).
      DONE: gh#599. Gap re-verified by grep first (nothing in test/e2e/
      issues graph.query.batch or graph.query.semantic, nothing calls Fuse) —
      the issue names why each of the three is specifically a LIVE-only
      check, notably that the ordering bug is cache-residency dependent.

## 6. Gates (all BEFORE merge)

**Gate evidence is versioned.** Three review rounds landed after the first green
run, each changing behavior, so evidence is recorded against the commit it
tested rather than as a standing claim. A gate cited against superseded code is
not evidence for HEAD (CLAUDE.md's BREAKING ⇒ e2e rule).

Rounds, in order:
- `d479cd5b` — `semstreams-reviewer` round 1 (3 blockers)
- `1b1029ac` — external PR #604 review (4 blocking + 3 medium)
- `8327f00e` — `semstreams-reviewer` round 2 (2 HIGH + mediums) + the
  semboids adoption response

- [x] 6.1 `task lint` · full `go test -race ./...` (explicit FAIL grep) ·
      `task schema:generate` no-drift · contract tests ·
      `go vet -tags=integration` AND `-tags=live_llm` ·
      `openspec validate --strict`. GREEN at `8327f00e`: 133 ok, 0 FAIL
      lines, lint 0, openspec 31/31, schema regenerated and committed.
      Re-run after every round, not once.
- [x] 6.2 Branch integration sweep (`go test -race -tags=integration ./...`)
      — framework-package change (graph/, pkg/fusion). Green at `1b1029ac`
      (134 packages, 0 FAIL, read from the log not the pipeline exit code);
      RE-RUNNING at `8327f00e`.
      Found real defects a tagged `go vet` could only prove COMPILED: the
      fusionnats real-wire assertions, a clustering fixture row, and — the
      one that mattered — a DEADLOCK introduced while fixing a blocker
      (graph-embedding's snapshot guard repointed at a latch that only
      flips past that guard). Tagged vet is a pre-flight; the sweep is the
      gate.
      Substrate noise along the way, all diagnosed not waved away: a loaded
      Docker daemon timing out container inspects at 180s, cleared by
      stopping a sister stack.
- [x] 6.3 BREAKING ⇒ e2e: `task e2e:statistical` AND `task e2e:semantic`
      green, with log-level evidence (not exit codes through a pipe).
      Green at `1b1029ac`: statistical "Scenario completed successfully",
      validation_errors:0, 15 communities (matches the #598 baseline),
      entities_missing:0, data_loss_percent:0; semantic validation_errors:0,
      7/7 known-answer (matches #598 exactly), 0 of 46 steps failed.
      RE-RUNNING at `8327f00e` — round 3 changed graph-embedding's bootstrap
      semantics and added a config rejection, so the `1b1029ac` evidence does
      not carry.
      Two aborted attempts earlier were a COMPOSE PROJECT-NAME COLLISION,
      fixed in Taskfile.yml: every sem* repo keeps compose files in
      `docker/compose/`, so Compose derived the same project name for all of
      them — our e2e teardown was deleting a sister repo's containers, and a
      sister stack coming up mid-run churned ours out from under a running
      scenario.
- [x] 6.4 `semstreams-reviewer` pre-merge; fold findings. THREE review
      rounds, all folded:
      **Round 1** (`semstreams-reviewer`, pre-PR) — CHANGES REQUESTED, 3
      blockers. The sharp one: `bootstrap_complete` latched on catch-up to
      the LIVE target, a measure-zero instant under continuous write (gh#590
      F1) — the bit would have read false forever on a firehose deployment
      and every health gate would defer, i.e. the change would NOT have
      fixed the bug it exists to fix. Also `Rank` was the response position
      (information-free) rather than the resolve rank the spec requires, and
      the retired absence license survived at the DEFINITION site though its
      mirror had been swept.
      **Round 2** (external, on PR #604) — 4 blocking + 3 medium. A withheld
      response read as HEALTHY through the canonical gate when the defer came
      from a dependency other than graph-index; unknown wire states passed a
      deny-list check; the bounded-freshness arithmetic wrapped negative on an
      out-of-range staleness; graph-embedding published bootstrap_complete on
      DELIVERY rather than applied build.
      **Round 3** (`semstreams-reviewer`, post-fix) — 2 HIGH. A
      `max_staleness` at or below the readiness heartbeat is unsatisfiable
      (measured: 52% of ticks proceed at 3s against a caught-up index), and
      the round-2 sizing doc named the smaller cause; and the
      graph-embedding applied-build stamp was MUTATION-GREEN — deleting it
      left the package passing, while the same mutation on graph-index fails.
      Both verified independently before fixing.

## 7. Delete the freshness knob (ADR-085)

Owner-agreed 2026-07-21, folded in BEFORE the tag: shipping a knob we intend to
delete makes it a sister-repo migration instead of an edit. Round 3's finding —
a `max_staleness` at or below the heartbeat is unsatisfiable, so the knob needed a
floor derived from the transport's tick rate — was the evidence the knob itself is
wrong, not just its documentation.

Survey completed before deleting (the memo's three open verification questions):

- [x] 7.0a Genuine freshness dependencies, in-repo and across sister repos.
      `FreshnessWithin` has **one** call site (graph-clustering); everything else
      declares `FreshnessNone` or a `FreshnessExact` that is a bootstrap probe in
      disguise. `max_staleness` / `index_lag_tolerance` have **zero** adopters
      across all ~20 local `sem*` repos.
- [x] 7.0b Re-read ADR-082's argument rather than assuming it away. It holds and
      is preserved: a periodic whole-result re-deriver CAN act on a stale view.
      What does not follow is that it therefore wants a tolerance — the observation
      argues for not gating, which is what ADR-085 does.
- [x] 7.0c Stamping staleness on the output is sufficient; no consumer needs to
      BLOCK on "this partition was computed stale". Community detection overwrites
      its partition next cycle.
- [x] 7.0d **`max_staleness` never shipped in a tag** (`git grep` against
      `v1.0.0-beta.156` confirms `index_lag_tolerance` is the released key). Sister
      repos migrate `index_lag_tolerance` → nothing and never learn the
      intermediate field; the ADR-083 migration doc's Break 2 collapses.

- [x] 7.1 Delete `graph.Freshness` + `FreshnessExact`/`Within`/`None`; collapse
      `EvaluateReadinessGate` to a single `StatusReading` argument (health only:
      fresh → recognized state → hard stop → bootstrap complete). Delete the
      `over_staleness` and `staleness_unknown` defer reasons and the bound
      arithmetic (`viewAge`, `maxStalenessMs`).
- [x] 7.2 Delete `readiness.MinBoundedStaleness` and `ValidateStalenessBound`.
      **KEEP `FreshnessMultiplier`** — it answers "can I still vouch for this
      reading" (transport liveness), a health question, not a view-age one.
- [x] 7.3 Delete graph-clustering's `max_staleness` config surface
      (`MaxStalenessStr`, `MaxStaleness()`, `validateMaxStaleness`,
      `maxStalenessCeiling`); add a `max_staleness` entry to the removed-key
      rejection map so a carried-forward config fails startup loudly rather than
      being silently ignored.
- [x] 7.4 Keep and re-scope the reporting: `staleness_at_detection_ms` records on
      EVERY run, not only admitted ones (Help text updated);
      `detection_duration_seconds` unchanged. This is the "stamp the age on the
      output instead of refusing to produce one" half.
- [x] 7.5 Drop the freshness argument at all four call sites. At
      `graph-index/query.go` this is a real behavior change, not a no-op: the
      comment claiming the staleness comparison is unreachable there is WRONG —
      `computeIndexStatus` latches on the way past, so the call where the latch
      flips while `Ready` is still false produced one spurious transient
      `IndexNotReady`. Correct the comment; confirm the gh#474 cutover guard is
      preserved via `bootstrap_incomplete`.
- [x] 7.6 `pkg/graphview` gains the reporting half it was missing (owner
      approved folding it in — same intent). Track the KV server write time of the
      newest applied revision beside the applied-revision watermark, expose as one
      atomic pair, carry it on snapshots. Gating is NOT touched: its bootstrap and
      fail-closed gates already are what ADR-085 prescribes. Retires the parked
      §3.3 / ADR-082 G5 follow-up as a REPORTING task, not the gating task it was
      originally framed as.
- [x] 7.7 Rework (never delete) the gate test suites that encode retired
      semantics: `graph/readiness_gate_test.go`,
      `processor/graph-clustering/staleness_gate{,_integration}_test.go`. Pin:
      healthy-but-arbitrarily-stale PROCEEDS and stamps a non-zero staleness;
      each surviving defer reason fires on its own condition; a config carrying
      `max_staleness` fails startup; `bootstrap_complete=false` still defers.
- [x] 7.8 ADR-085 written. Supersedes ADR-084 D1's freshness parameter + D5's
      bounded-staleness clause; completes ADR-082's retirement. Narrow pointer
      notes added to ADR-082 and ADR-084 (no retrofits). ADR-085 also records the
      precise `bootstrap_complete` definition, since it is now the only
      coverage-shaped condition left in the gate.
- [x] 7.9 ADR-083 migration doc: Break 2 rewritten to "removed, no replacement"
      with the never-tagged collapse called out; "Sizing `max_staleness`" replaced
      by "Reading the staleness metrics" (floor caveat + gh#605 dissolution);
      upgrade steps 3 and 7 corrected.
- [x] 7.10 Spec deltas for the collapsed gate + graphview currency reporting;
      `openspec validate --strict`.
- [ ] 7.11 Reference configs: no `max_staleness` value to choose — the open owner
      question dissolves (owner confirmed: drop it). Verify `configs/` carries
      neither key.
- [ ] 7.12 Close gh#605 as dissolved (the tuning-dynamics problem cannot exist
      without a tolerance to tune).
- [x] 7.13 Export `graph.AllDeferReasons` (mirroring `AllIndexStates`) and drive
      graph-clustering's `deferReasons` and both coverage tests from it. The
      hand-maintained second copy failed SILENTLY in the direction that matters:
      `countDefer` drops any label outside its list, so a reason added to the gate
      but not mirrored would defer in production while incrementing nothing.
      Mutation-verified in both directions (gate emits an unlisted reason → fails;
      list declares an unreachable reason → fails). Known limit recorded in the
      test: it cannot catch a reason on a branch no reading provokes, so the
      reading table is load-bearing.
- [x] 7.14 **Review round 4 HIGH** — `graph/clustering/lpa.go:164` cleared
      COMMUNITY_INDEX before rebuilding. Ungating detection turns a rarely-reached
      window into a near-permanent one (runs up to 23.7s inside a 30s interval),
      and `processor/graph-query/community_cache.go` latches `ready` once, so it
      would serve a ready-but-empty community set most of the time — an
      authoritative-looking empty answer, the class ADR-084 retires. Fix:
      write-then-prune (snapshot keys, save over them, delete only unwritten
      keys). Recorded as ADR-085 decision 7; the falsified "overwritten next
      cycle" claim is corrected in place rather than quietly edited out.
- [x] 7.15 Gates re-run at HEAD after 7.14, evidence read from output not exit
      codes. `go test -race ./...` **0 `^FAIL`** · `-p 1 -race -tags=integration`
      **0 `^FAIL`** · `task lint` clean · `go vet -tags=integration` and
      `-tags=live_llm` both exit 0 · `go test ./test/contract/...` ok ·
      `task schema:generate` diff still **exactly 5 lines** (`max_staleness`
      leaving `schemas/graph-clustering.v1.json`), no new drift ·
      `openspec validate --strict` valid.

      **`task e2e:statistical` GREEN** (`statistical-20260721-083951`,
      1m35s): `validation_errors: 0`, `data_loss_percent: 0`,
      `known_answer_tests_passed: 7/7`. The assertions this change could have
      moved were compared directly against the pre-change baseline run
      (`statistical-20260720-213452`) and are **bit-identical**:
      `total_communities` 15=15, `non_singleton_count` 15=15, `largest_size`
      55=55, `average_size` 25=25, `with_keywords` 15=15.

      That equality is the real result for the STORE: detection went from
      effectively never running under the old exact gate to running every tick,
      and the rebuild changed from clear-then-write to write-then-prune — and the
      partition it produces is unchanged. (The harness has write lulls, so
      detection DID run under the old gate here; this tier could not have
      exercised the continuous-write path that motivated the change, and does not
      claim to.)

      **CORRECTION (round 5): this evidence says nothing about the graph-query
      CACHE, and I originally cited it as though it did.** The `communities`
      block comes from `test/e2e/client/nats.go:483`, which reads
      COMMUNITY_INDEX **directly over NATS**, bypassing `CommunityCache`
      entirely. The one stage that does exercise the cache queries `level: 1` and
      treats a low count as a **warning, not a failure**
      (`test/e2e/scenarios/tiered_statistical.go:353-355`) — the
      warn-not-fail-masks-drift trap. Neither result JSON carries a
      `graphrag_global` block. So the bit-identical comparison could not have
      detected the cache truncation found in 7.16, and citing it as broad
      reassurance was wrong. **Coverage gap worth filing: no e2e assertion
      exercises GlobalSearch through the cache as a hard failure.** Filed on
      gh#609 (issuecomment-5035466331) with a concrete suggestion: a hard-failing
      GlobalSearch stage run after TWO detection cycles, since the truncation only
      appears once a prune has deleted keys from a prior partition.

      **Re-run at HEAD after the 7.16 fix** (`statistical-20260721-100128`):
      green, community block still bit-identical (15/15/55/25/15). Unit -race 0
      FAIL, integration -race 0 FAIL, lint clean, both tagged vets, contract ok,
      schema diff still exactly the 5 `max_staleness` lines, openspec strict
      valid.
- [x] 7.16 **Review round 5 HIGH** — the write-then-prune ordering broke
      `processor/graph-query/CommunityCache`, which keyed by bare community ID
      while storage keys by `{level}.{id}`. `handleDelete` rebuilt a level index
      using the level of the STORED occupant after higher-level writes had
      shadowed the bare-ID map, collapsing `byLevel[0]` — which
      `globalSearchTextBased` reads with **no NATS fallback**. Reproduced
      directly: 2 level-0 communities, a level-1 put with a colliding ID, then a
      late delete → `byLevel[0]` = 0, truth 1. Under delete-first ordering the
      rebuild preceded the shadowing, so the defect was latent; this change makes
      it live for most of each cycle and, in the short-detection regime, WORSE
      than the empty window it replaced. My filed blast-radius bound on gh#609
      ("one cycle, level>0, GlobalSearch unaffected") was wrong on three of four
      counts. Fix: key the cache by `(level, ID)`; apply deletions using the
      level from the deleted key. Recorded as ADR-085 decision 7's second
      generalization and as a new `graph-clustering` spec requirement.
- [x] 7.17 Round-5 MEDIUMs: keep-set assembly in `lpa.go:233-236` is
      MUTATION-GREEN (replacing the all-levels loop with `result[0]` makes Prune
      delete every level-1/2 community every run and the whole `graph/clustering`
      suite still passes) — add detector-side coverage; `design.md` D1
      superseded by new D8/D9 (it still specified the deleted knob and
      contradicted every other artifact); ADR-085 decision 7's mechanism
      corrected ("snapshots the prior key set" was not what `Prune` does — it
      lists the CURRENT key set at the end of the run) and its union guarantee
      qualified; `graph-clustering` spec delta ADDED (the capability had none
      despite this change altering its durable rebuild contract).

- [x] 7.18 **Review round 6** — one HIGH, fixed + mutation-verified.
      `enrichCommunitySummaries` keyed rep-entities by BARE community ID three
      lines after `GetCommunity` became level-qualified, so two same-ID summaries
      at different levels both got the LAST level's rep entities while each kept
      its own MemberCount — a digest stitched from two different communities,
      returned to agents on `community_summaries[].entities[]` and fed into the
      answer prompt. Reachable exactly BECAUSE the 7.16 fix stopped collapsing
      levels; an incomplete fix, not new damage. Fixed by indexing rep-entities by
      summary POSITION rather than ID (both loops walk the same slice in the same
      order, so the collision is unrepresentable rather than guarded) —
      `TestEnrichCommunitySummaries_CollidingIDsKeepTheirOwnRepEntities`,
      mutation-verified: restoring the ID-keyed map reproduces the symptom exactly.
- [x] 7.19 Round-6 MEDIUMs closed — both were coverage gaps on correct code.
      (a) The `Level`-population invariant was MUTATION-GREEN at all three sites,
      because nothing exercised `Level >= 1` through a production handler and a
      dropped stamp resolves silently to level 0 — indistinguishable in any
      level-0-only test, since 0 is the Go zero value (the existing
      `Level == 0` assertion was VACUOUS). Now covered at both sites that feed a
      lookup: `globalSearchTextBased` via a `Level: 1` GlobalSearch through the
      real handler in `TestIntegration_CommunityCacheCrossLevelCollision`
      (MemberCount 2-at-L0 vs 3-at-L1 is the discriminator), and
      `findCommunitiesForEntities` via a unit test. Both mutation-verified to
      fail on the exact drop the review found green. `handleLocalSearch:302`
      remains uncovered — nothing reads that Level yet.
      (b) `GetAllCommunities`'s `(level, ID)` tie-break is now asserted; it is
      load-bearing because the consumer re-sorts with an UNSTABLE `sort.Slice` on
      Relevance alone and then truncates to MaxCommunities, so without it WHICH
      same-ID record survives truncation flips between identical queries.
      Mutation-verified.
- [x] 7.20 **INCIDENT: round-6 reviewer destroyed uncommitted work.** It ran
      `git checkout -- graph/clustering/storage.go` to undo a mutation; that file
      held 89 uncommitted lines (the whole `Prune` method), unrecoverable from git
      (never staged). Restored in full from the `git diff` captured earlier in
      session context — NOT from the agent's own recovery file, which was
      incomplete (missing the `if s.kv == nil` testStore branch, which would have
      nil-derefed the in-memory path, and it declared the doc comment
      unrecoverable). Restoration verified three ways: `git diff --stat` back to
      89 lines matching the pre-destruction stat, build + tagged vet clean, and
      all nine Prune/Clear/Rebuild tests green on a CLEARED test cache including
      the real-NATS integration ones. Guard added to
      `.agents/contracts/semstreams-reviewer.md` rule 7: no `git checkout`/
      `restore`/bare `stash`; mutate via `cp` backup; verify `git diff --stat`
      after each round; report destruction at the TOP of the report.
      See [[feedback_verify_fails_without_via_stash_not_checkout]] — the memory
      existed and was not binding on subagents, which is why it is now in the
      contract.

- [x] 7.21 **Review round 7: APPROVE, safe to merge.** No blocking or high
      findings. The reconstructed `Prune`/`Clear` were reviewed as NEW code (no
      original survives to diff against) and are correct on their merits — the
      reviewer specifically hunted the two failure modes that would make
      transcript-reconstruction dangerous and closed both: keep-set key
      reconstruction matches `SaveCommunity` exactly (including the
      `community = summarized` pointer swap at `lpa.go:346`, which cannot drift
      keys because all three summarizers mutate in place and never touch
      ID/Level/Members), and the `s.kv == nil` testStore branch is correct AND
      has no test — omitting it, as the rejected recovery file did, would have
      nil-panicked with nothing catching it. The position-indexing fix holds
      under attack (nothing between the two loops can reorder/resize
      `summaries`; the only real reorder completes inside
      `findCommunitiesForEntities` before enrich is called) and was
      independently mutation-verified. New tests judged targeted, not
      tautological.
- [x] 7.22 **Round 7's two MEDIUMs were against MY OWN contract rule 7** — both
      verified and fixed. (a) The rule prohibited bare `git stash` but thereby
      SANCTIONED `git stash push -- <path>`, which on an UNTRACKED path is a
      silent no-op whose paired `pop` grabs the top of the stack — here, one of
      two unrelated codex stashes, dumped over the tree under review. (b) It
      prescribed `git diff --stat` for restoration verification, which reports
      NOTHING for untracked files — and 7 in-scope paths are untracked,
      including two of the new test files under review, so an unrestored
      mutation would pass silently. Rule 7 now prohibits `git stash` in every
      form and mandates checksum verification plus a `git status --porcelain`
      entry count. **The same guard was added to
      `.agents/contracts/semstreams-developer.md` as rule 7** — developer agents
      run the same mutation checks and had no guard at all.

## 8. Close-out

- [ ] 8.1 PR + owner merge; tag TOGETHER with #598's breaks (owner
      sequencing: no semsource tag before this change).
- [ ] 8.2 gh#597 comment: part 1 shipped (drop path closed + resolve-order
      ranking fix), part-2 minimal slice shipped; REMAINS OPEN: the
      cross-store consistency gap (semantic index ranking an ID whose
      ENTITY_STATES read returns not-found) — now visible via the 4.2
      counter; file separately if the soak confirms it.
- [ ] 8.3 gh#592 comment: close-out superseded deliberately for read paths
      (ADR-084); reopen trigger retired.
- [ ] 8.4 Archive change + update memory; sister lockstep PRs remain
      owner-managed (with #598's wave).

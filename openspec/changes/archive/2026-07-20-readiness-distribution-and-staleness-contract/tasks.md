# Tasks — readiness-distribution-and-staleness-contract

One PR, complete-system (foundation ships with its first users). Clean break, no
deprecated paths (owner decision). Breaking wire + config surface ⇒
`task e2e:statistical` AND `task e2e:semantic` green before the tag.

## State of play (verified 2026-07-20, do not re-derive)

The tree is ~90% done. All PRODUCTION code for §1–§4 is complete and green.
Verified on this tree: `go build ./...` green; `go test ./...` green in EVERY
package except `graph/query` and `pkg/fusion/fusionnats`; `go vet
-tags=integration ./...` green; `task lint` clean; `go test ./test/contract/...`
green; `task schema:generate` stable (diff = the intended `index_lag_tolerance` →
`max_staleness` rename only); repo-wide grep confirms the
`graph.index.query.status` subject + handler are REMOVED (remaining mentions are
doc comments describing the removal — no dead fake responders in-repo).

The ONLY red is ONE bug in TWO test files (task 4.5 below): both packages' test
fakes implement `Get` only, written for an earlier KV-Get transport design; the
production `graph/readiness.Watcher` calls `Watch`, which falls through to the
nil embedded `jetstream.KeyValue` → panic. Fix the fakes; do NOT touch production
code, and do NOT revert anything.

Semantics guardrail for everything below: this PR moves DISTRIBUTION only
(subject → GRAPH_STATUS KV watch). Every gate predicate stays bit-for-bit
(`!Ready` defers exactly as before; unknown fails closed unless
`AllowUngatedReads`). Any change to WHAT gates (health vs coverage) is the
separate follow-up change with its own ADR — do not smuggle it in here.

## 1. Envelope, watermark, and the canonical gate

- [x] 1.1 `pkg/revlag`: track the KV entry commit timestamp per observed revision
      and expose the commit time of the current `Indexed()` floor. Unit tests
      cover sparse delivery, coalescer-collapsed completions, and the
      empty-pending (caught-up) case.
- [x] 1.2 `graph.IndexStatusResponse`: additive `staleness_ms` field; `0` when
      `Ready`. Update `ComputeIndexStatus` and the producer glue. Keep
      `pkg/fusion.IndexStatus` in lockstep (the two structs change together).
      Production-decoder JSON round-trip test for the new field.
- [x] 1.3 Canonical gate helper in `graph` with declared modes (`exact`,
      `bounded-staleness`, `sticky-bootstrap`, `degrade-honest`), hard stops
      under every mode, and freshness input (`status_unknown` short-circuit).
      Unit tests prove: exact-mode bit-parity with `Ready`;
      `max_staleness = 0` ≡ `Ready`; empty graph never ready by tolerance;
      hard stops survive any tolerance. Remove `ReadyWithinLag` with its only
      consumer (compile-time sweep for stragglers).
- [x] 1.4 Shared consumer-side status watcher helper (single code path for all
      consumers): watches a `GRAPH_STATUS` key, holds last-known envelope +
      consumer-local arrival time, answers `(status, fresh|unknown)`. Unit
      tests: immediate-current-value on bind, freshness expiry at 3× heartbeat,
      missing bucket/key → unknown.

## 2. Producers publish readiness as KV state

- [x] 2.1 `GRAPH_STATUS` bucket (decided; History 3, small bounded): created at
      Start by graph-index and graph-embedding, eagerly before any consumer
      could bind. One key per producer.
- [x] 2.2 Publish the envelope every status tick from the same
      `computeIndexStatus` call that sets the #596 gauges (compute once → gauges
      + KV). Publish-failure counter + warn log. Integration test drives the
      production wire (component Start → KV key appears and heartbeats), not a
      helper.

## 3. graph-clustering: watch-based gate, time tolerance, evidence

- [x] 3.1 Status source: the shared watcher helper (1.4) — no request/reply
      path remains. Absent bucket/key (standalone deploy, producer down) reads
      as `status_unknown` → fail closed, `allow_ungated_reads` escape preserved.
- [x] 3.2 Config: add `max_staleness` (duration string, default 0 = exact);
      REMOVE `index_lag_tolerance` (**BREAKING** — loud decode failure, release
      note). `task schema:generate` regenerated and committed; operator-surface
      JSON round-trip test covers `max_staleness`.
- [x] 3.3 Observability: structured defer log (`status_known`, `status_age`,
      `state`, `lag`, `staleness_ms`, `reason`, watch/bucket error
      when present) + `defer_total{reason}` counter
      (`hard_stop | over_staleness | status_unknown | empty`); staleness-at-run
      gauge and INFO line on stale runs carried over from ADR-082.
- [x] 3.4 Rework `bounded_lag_integration_test.go` into staleness-mode tests
      that drive the production wire (real watch → freshness → gate), including
      the status-feed-dies → `status_unknown` defer path.

## 4. Remaining consumers adopt the canonical gate; the status subject is removed

- [x] 4.1 `graph/query/client.go` `indexNotReadyErr` → canonical `exact` mode
      over the shared watcher helper; test proves bit-compatible proceed/defer
      behavior with today. **Production code DONE and verified — only the test
      is red; closing 4.5 closes this.**
- [x] 4.2 graph-index `ensureQueryReady` → `sticky-bootstrap` mode with the
      local `resetState`/`failedCount` overrides preserved; existing gh#474
      gate tests stay green unchanged.
- [x] 4.3 fusion: `pkg/fusion/fusionnats` `Status` moves from the subject to the
      shared watcher helper; top gate and transient-degrade behavior verified
      untouched (`degrade-honest` naming a pure refactor). **Production code
      DONE and verified — only the test is red; closing 4.5 closes this.**
- [x] 4.4 **Remove** the `graph.index.query.status` subscription and
      `handleQueryStatusNATS` (**BREAKING** wire). Repo-wide sweep for
      requesters of the subject (including tests and docs snippets); update the
      probe guidance to `nats kv get GRAPH_STATUS graph-index`.
- [x] 4.5 **UNBREAK (the only red — do this first).** Rework the test fakes in
      `graph/query/readiness_gate_test.go` and
      `pkg/fusion/fusionnats/client_test.go` from `Get`-based to `Watch`-based.
      Root cause: both `statusBucket` and `fakeStatusBucket` embed a nil
      `jetstream.KeyValue` and implement only `Get`; the production
      `graph/readiness.Watcher` calls `bucket.Watch` (watcher.go:330) → nil
      deref panic. DO NOT change production code, and DO NOT weaken any asserted
      outcome — the decision matrices in those tests are the bit-compatibility
      contract and stay exactly as written.
      Pattern to copy: `graph/readiness/watcher_test.go` (`fakeBucket` /
      `fakeWatcher` / `fakeEntry`). Semantics mapping:
      - envelope served → `Watch` returns a watcher whose `Updates()` channel
        (buffered, cap ≥ 2) yields the entry, then STAYS OPEN (closing the
        channel triggers the rebind loop);
      - key absent (was `Get` → `ErrKeyNotFound`) → yield `nil` (the
        end-of-initial-values marker), then stay open — held state stays
        unknown;
      - backend fault → `Watch` returns the error;
      - malformed / stale-Created entries → yield them as-is (decode failure
        and `staleOnArrival` are the watcher's job, already unit-tested).
      Timing: unknown-branch subtests block in `WaitForFirst` until the one-shot
      budget expires — set `statusBindWait: ~25ms` on the test `natsClient`
      (graph/query) and pass a small timeout to `fusionnats.New(fake, ~50ms)`,
      or the table burns ~5s per unknown case. Cleanup: `t.Cleanup(client.Close)`
      for fusionnats; for graph/query set `watchCtx` to a cancellable context
      cancelled in `t.Cleanup` so watcher goroutines don't outlive the test.
      Also update `TestIndexNotReadyErr_ReadsTheContractKey` (`recordingBucket`)
      to record the key from `Watch` instead of `Get`, and fix the two stale
      "KV Get" doc comments (readiness_gate_test.go:19,
      fusionnats/client_test.go:24) to say watch.
      Done when: `go test -race ./graph/query/ ./pkg/fusion/fusionnats/` green,
      then `go test -race ./...` fully green.

## 4b. BLOCKING — open decision from the pre-merge review (owner/architect call)

- [x] 4b.1 **`ViewRevision.Coherent` fail-open** (`pkg/fusion/engine_graph.go:210`).
      Found by `semstreams-reviewer`; **independently confirmed against the code**,
      not taken on the reviewer's word.
      Mechanism: `Fuse` samples readiness at its top gate, passes
      `status.IndexedRevision` down as `startRev`, and after the last graph fetch
      re-samples via `graphViewRevision`, reporting
      `Coherent: start == status.IndexedRevision`. The REMOVED handler computed
      `computeIndexStatus` live per request (verified via
      `git show HEAD:processor/graph-index/name_index.go`), so two samples ms
      apart genuinely differed while the watermark advanced and `Coherent=false`
      correctly fired. Held state refreshed every 5s makes both samples the SAME
      envelope for any sub-heartbeat query, so `Coherent` is now ~always true.
      Why it matters (not theoretical): semsource's UI deletes absent items only
      for a "complete coherent nonzero projection"
      (`ui/src/lib/graph/model.test.ts:43`), so a false coherence claim can delete
      entities that exist. Its decoder (`ui/src/lib/contracts/graph.ts:203`)
      throws only on `coherent && start != end`, which we do NOT trip — so this
      fails silently, not loudly.
      Not caught by any gate because `pkg/fusion/engine_graph_test.go` stubs a
      `RetrievalClient` that still has sample-now semantics, and `fusionnats.New`
      has no in-repo production caller (semsource is the consumer) — so neither
      the unit suite nor `e2e:semantic` exercises it.
      **NOTE: a KV `Get` would NOT fix this.** The resolution loss comes from
      publishing every 5s, not from Watch-vs-Get
      ([[feedback_kv_get_and_watch_same_staleness]]).
      Options: **(a)** restore a provable signal — carry sample identity (KV
      revision / arrival stamp) so "same sample" is distinguishable from
      "observed stable", and report `Coherent=false` when coherence is merely
      unproven; **(b)** narrow the documented contract at
      `engine_graph.go:58`/`:137` + ADR-083 + migration doc.
      **gh#597 (filed by semsource 2026-07-20) raises the stakes on this.** It
      reports `Fuse` silently dropping the TOP-ranked entity from the first call
      after a diverse query burst, self-healing on the next identical call, with
      recall provably intact (`graph.query.semantic` returns it at rank 1). The
      drop path is confirmed in code and is NOT caused by this change (observed
      on .156): `fusionnats.Entities` (`client.go:388`) builds its result from
      `resp.Entities` and never compares against the requested `ids`, and
      graph-ingest's `fetchEntitiesConcurrent` omits any ID whose KV Get returns
      not-found (`query.go:561`) — so "I could not get it" is indistinguishable
      from "it is not there" all the way up to the caller. That is EXACTLY the
      fourth conflated question ADR-083's Consequences call unanswerable, showing
      up in the field.
      The connection to 4b.1: `ViewRevision.Coherent` (on the opt-in graph facet,
      `resp.Graph`) is the ONE field in the response that claims the projection is
      a complete snapshot at a single indexed revision — i.e. the one signal that
      a silently-dropped seed could ever have contradicted. This change makes it
      always-true, and semsource's UI uses it to license DELETING absent items.
      Shipping (b) would mean a dropped seed can become a deletion.
      **RECOMMENDATION REVISED (owner pushback, and the owner is right).**
      Earlier this said "(a) restore a provable signal by carrying sample
      identity". That was wrong, and the correction matters more than the
      original finding:

      **`Coherent` was NEVER soundly provable, before or after this change.**
      Two samples agreeing does not establish that no read in between was stale —
      the watermark can advance and the re-sample still agree, and each of
      fusion's N reads hits a different store at a different instant with no
      snapshot, no read transaction, and no consistent cut. The old before/after
      sampling could sometimes CATCH an advance; it could never PROVE the absence
      of one. `Coherent=true` was always an overclaim. This change did not break
      a sound signal — it removed the noise that made an unsound one look like it
      worked.

      Therefore (a) is off the table: sample identity would buy a better
      heuristic wearing the same absolute-sounding word, adding a mechanism to
      defend a claim that should not exist. **Do the smallest honest thing
      instead: DELETE the claim.** Drop the `Coherent` bool; if the observed
      revision bounds are genuinely useful, report `Start`/`End` as observations
      and let the consumer decide what they mean. A consumer that needs a truly
      coherent view for deletion should use `pkg/graphview` (ADR-081), which has
      actual snapshot/revision semantics — retrieval fusion is best-effort ranked
      evidence and should stop pretending otherwise. This removes a wire field,
      so it is a third break for the same lockstep wave; semsource's
      delete-absent-items path needs a real replacement, not a re-tuned boolean.
      Recorded in ADR-083 Consequences so it is not shipped silently.
      **IMPLEMENTED (owner-directed): the `Coherent` bool is DELETED.**
      `ViewRevision` now carries `Start`/`End` as plain observations;
      `graphViewRevision` no longer computes the claim; the unit suite asserts
      the wire carries NO `coherent` key (regression pin). ADR-083 Consequences
      records the resolution; migration doc gained Break 3 with a consumer
      checklist; fusion capability delta REMOVES the view-revision consistency
      contract requirement and ADDS the observations-only requirement; the
      graph-index-readiness delta's fusion-degrade requirement drops its stale
      `Coherent` parenthetical. Third break in the same lockstep wave —
      semsource's delete-absent-items path must move to `pkg/graphview` or be
      removed (tracked with the §5.4 lockstep PRs; ties to gh#597 part 1).

## 5. Decisions and docs

- [x] 5.1 ADR-083 (one page, decisions only): readiness distributed as KV state
      and the status request/reply REMOVED (clean break — supersedes design D6's
      stale "request/reply retained" wording); view-rate staleness unit is time,
      superseding ADR-082's revision count for the clustering gate.
      → `docs/adr/083-readiness-as-distributed-state.md`
- [x] 5.2 Migration doc (solid, clean-break — the owner's compat posture):
      release notes marking BOTH breaks (`graph.index.query.status` removed →
      watch/get `GRAPH_STATUS`; `index_lag_tolerance: N` →
      `max_staleness: <duration>`), plus a short migration section with
      before/after snippets for each consumer shape (gate via helper, probe via
      `nats kv get`). Update `docs/concepts` where the subject or revision
      tolerance is mentioned. Also fix proposal.md:33 stale wording: staleness
      comes from KV COMMIT timestamps (design D3 + spec are authoritative), not
      "arrival times".
      → `docs/operations/migration-readiness-distribution-adr083.md`. Both stale
      proposal.md spots fixed, and design D6's "request/reply retained" wording
      corrected to the owner's clean break. `docs/concepts` needed no edit (no
      hits). ADR-066 and ADR-082 carry narrow status-line pointers to ADR-083
      (transport / staleness-unit only — neither is fully superseded, and old
      ADRs are history that must not be retrofitted).
- [x] 5.3 gh#590 follow-up comment when merged: what shipped, the
      `max_staleness` knob, the probe retarget, and that the held soak now
      validates a time bound. Coordinate the semboids reference value (design
      open question) with the owner.
- [x] 5.4 Sister-repo sweep (sweep-all-emitters discipline): file lockstep PRs
      where hits exist (sem\* is house-managed — migrate, don't shim).
      **CLOSED AS TRANSFERRED (owner directive 2026-07-20, on merging #598):
      the sweep itself is complete (all coordinates below + Break 3's UI
      delete-absent-items path); filing the semsource/semconnect PRs is
      owner-managed, deliberately sequenced AFTER the
      `fusion-consistency-simplification` change lands so sister repos migrate
      the readiness surface once (no tag before then).**
      **Sweep RE-VERIFIED 2026-07-20 across ALL ~20 local `sem*` repos, not just
      the four originally named** — `index_lag_tolerance` has ZERO adopters
      anywhere, so its removal is unconditionally safe, and only two repos have
      live subject requesters:

      **semsource** (7 code files — note the two starred ones were NOT in the
      earlier four-repo list):
      - `processor/source-manifest/workbench_capabilities.go:23`
        (`structuralStatusSubject` const) — plus `:98-109` hand-mirrors the wire
        struct, so it will not carry `staleness_ms` until updated.
      - `processor/supersession/versiondiff_serve.go:201`
      - `processor/mcp-gateway/tools.go:136` (+ comment at `:124`)
      - `processor/code-context/component.go:128` — indirect via `fusionnats`;
        does NOT grep for the subject string, so sweep by dependency too.
      - \* `processor/source-manifest/workbench_capabilities_test.go:18,139,180`
      - \* `internal/governance/fusion_gateway_integration_test.go:30,78`
      - **Fake responders that go SILENTLY DEAD** (the known §4 hazard — a fake
        for a REMOVED subject still compiles and tests nothing):
        `processor/mcp-gateway/nats_integration_test.go:92` and
        `processor/code-context/scope_integration_test.go:39`. Rework, do not
        just delete the coverage.
      - UI decoder `ui/src/lib/contracts/fusion.ts:121` throws on
        `ready && lag != 0` — NOT triggered by us (Ready ⟹ lag==0 structurally),
        but note it while touching the contract.

      **semconnect**: `conformance/cmd/index-readiness/main.go:20`, a GATING tool
      driven by `conformance/run.sh` — breaks the qualification run until
      migrated. (Its other hits are archived evidence logs; leave them.)

      **semboids, semteams, and every other `sem*` repo: ZERO hits.**

## 6. Gates (before push / PR / tag)

- [x] 6.1 `task lint` clean; `go test -race ./...`; `task schema:generate` with
      no uncommitted drift; `go test ./test/contract/...`.
- [x] 6.2 Framework-package change sweep (graph/, pkg/revlag, two processors):
      `go test -race -tags=integration ./...`; pre-tag build-tag vet
      (`go vet -tags=integration`, `-tags=live_llm`). Both vets clean.
      Integration sweep found and FIXED two real defects, both test-side:
      (a) `fusionnats` `Status_absent_key_is_an_error` asserted the tombstone
      synchronously — with readiness distributed as watched state the client
      CONVERGES on a delete, so the assertion now waits for convergence (that
      eventual visibility is the honest contract, not a bug);
      (b) `graph-index` `TestIntegration_PredicateLayoutSmoke` demanded catalog
      consumer high-water proof that its OWN gatherer was told not to insist on
      (gh#555 — a ~22-row stream's ephemeral consumer can vanish between Info
      polls). One `catalogConsumerVisibilityRequired` constant now feeds both
      sites so they cannot drift; the leak check stays strict for both.
      **Classified, not hand-waved.** THREE full sweeps were run; the failing
      package is DIFFERENT and non-overlapping each time, which is the diagnosis:
      | sweep | failed | verdict |
      |---|---|---|
      | 1 | `fusionnats`, `graph-index` | REAL — both fixed above, green in sweeps 2 AND 3 |
      | 2 | `graph-query` | substrate — passed sweeps 1 & 3; passes alone in 38s vs timing out at 154s |
      | 3 | `pkg/lifecycle` | substrate — **untouched by this change** (`git status` clean for it); passed sweeps 1 & 2; passes alone in 3s |
      Both substrate failures are container-readiness timeouts
      (`wait until ready: port "8222/tcp" not found`) in testcontainer-using
      packages: this machine loses roughly one container race per full parallel
      `./...` run. gh#220 substrate-flake family, not this change. Every package
      this change actually touches passes, repeatedly, including a 2× repeat of
      the full `graph-clustering` package after the race fix.
- [x] 6.3 **BREAKING gate**: `task e2e:statistical` AND `task e2e:semantic`
      green before the tag (clustering tier exercises the migrated gate;
      semantic tier exercises the fusion path off the removed subject).
      **BOTH GREEN (exit 0), 2026-07-20.** Live evidence pulled from the running
      statistical stack rather than inferred from the tier's exit code:
      `GRAPH_STATUS` bucket created at startup; the clustering watcher logged one
      `bucket not found` at 21:48:08.438 and graph-index created the bucket 2ms
      later at .440 — that 2ms is the gap between the failed open and the
      create, NOT the rebind interval (`defaultRebindDelay` is 1s), and the
      watcher then rebound on its next cycle: the eager-create startup race the
      design anticipated, self-healing exactly as intended; community detection
      completed with **15 communities / 3 levels**
      and **`defer_total` == 0 on every reason** — gh#590's 254-deferral symptom
      does not reproduce; `staleness_at_detection_ms` = 0 under the default
      exact gate, confirming bit-parity. All D5 observability present on the
      wire.

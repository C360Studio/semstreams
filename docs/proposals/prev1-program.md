# Pre-v1 core hardening — program entry point

**This file is the baton.** Read it first, do the Next Action, update it last.
It is the only file that carries program state across sessions.

Opened 2026-07-21 · baseline `v1.0.0-beta.157`

---

## Next action

> **Epic A is COMPLETE — mechanical increments (inc 1–4, shipped + archived + released as
> `v1.0.0-beta.158`) AND its test-debt.** The test-debt unit (`#599 + #597`) shipped this
> session as PR **#642 (`db64d2c6`, 2026-07-23)**: a `validate-batch-read-reconciliation`
> e2e:semantic stage driving `graph.query.batch` + the production `fusionnats.Client.Entities`
> over the live wire — asserts the gh#604 missing→unhydrated reconciliation + exactly-once on
> every reply, with a **load-bearing** `batch_query_missing_total` gate (lower-bound, since the
> counter is process-global). The Codex gate found **5 real coverage holes (3 P1)** two reviewers
> missed; all addressed (production-client absent reconciliation; counter load-bearing;
> exactly-once everywhere) + 2 **honestly downscoped** — the gh#604 reorder-under-cache-miss and
> a real gh#597 cache-residency soak need a cache-control seam (**#643**, filed), the `fusion.Fuse`
> envelope → **#391** (re-home note posted). **#599 + #597 CLOSED** by owner decision — #597 is
> *guarded + countable*, NOT proven closed (re-file from a real workload if the counter climbs).
> Merged CLEAN (CI + sister-gate green); second Codex pass waived by owner.
>
> Tracker hygiene this session: **7 issues whose PRs had merged but were never closed
> (#600 #601 #602 #611 #612 #614 #616) are now closed** — the "status words lie" rule (below) in
> action; `gh issue list` now reflects reality.
>
> **NEXT = the SAME owner decision on the next unit** (Epic A now fully done; epics sequential,
> WIP=1). Candidates:
> - **Epic B** — one community truth (level-0-only; disable LLM enhancement until the ownership
>   split; readiness gate). #606 #607 #608 #609 #617 #618. Starts with the community-scope owner
>   decision. Not started.
> - **Epic C** — derived-state ownership + the cross-bucket repair/ordering pair (#622 #527 #625
>   #629; #629 dormant). Starts with accepting one retention ADR. Not started.
> - **Deferred Epic A owner-decisions**: **#619** (BM25 ADR-scale restart-fork redesign — interim
>   shipped, bleeding stopped), **#633** (orphan-blob GC, owner-accepted growth), and the new
>   **#643** (cache-control seam — deterministic e2e reorder + a real #597 cache-residency soak).
>
> Main tip `db64d2c6`; tag baseline still `v1.0.0-beta.158` (#642 is test-only — no new tag). Clean.

---

## How to read this file

Status words lie. That is the central finding of the audit that created this
program, and it applies to project tracking too. **Every row's state is a command
you run, not an adjective someone typed.** If the command disagrees with the
table, the command is right — fix the table.

```bash
gh issue list --state open --limit 100        # what is actually open
openspec list                                 # what is actually in flight
gh run list --workflow=sister-validation.yml  # whether the gate is actually running
git status --porcelain                        # whether the tree is actually clean
```

## Session protocol

1. **Start** — read this file, then run the four commands above and reconcile.
2. **Work** — the Next Action. One thing.
3. **End** — update the table from checkable state, then rewrite Next Action.

**WIP = 1 at the epic level.** Eight OpenSpec changes are already stalled in the
80–95% band; the last increment of each is disproportionately the observability
and guardrail work this program is about. Adding parallel epics is how this
becomes change number twelve through sixteen. Finish, then start.

---

## Evidence (stable — do not re-derive)

| Doc | Holds |
|---|---|
| `prev1-audit-synthesis.md` | reconciliation of two independent audits; convergent findings, disagreements resolved, the merged plan |
| `prev1-graph-core-audit.md` | Claude-side detail: the 13 phantom signals, per-subsystem findings with file:line |

Findings in those docs are **settled evidence**. If a session re-derives them, that
is wasted budget. Cite and move on.

---

## Track 0 — MERGED (PR #624 → `5e80f676`, 2026-07-21)

One PR. No OpenSpec change; ceremony on mechanical fixes is how they reach 90% and
stop. Eight fixes shipped; three reviews (two internal + Codex) folded in; the
`!`-marked BREAKING change had all three e2e tiers green before merge. Follow-ups
filed instead of scope-creeping the PR: **#623** (Epic A) and **#625** (Epic C).

| # | Fix | Issue | Done when |
|---|---|---|---|
| 1 | rule processor: delete the ENTITY_STATES create path | #610 | issue closed |
| 2 | message-logger: `GetKeyValueBucket`, return 404 | #611 | issue closed |
| 3 | dedup key: fold in embedder type + model + cap | #612 | issue closed |
| 4 | two phantom e2e metrics | #615 | issue closed |
| 5 | `WithWorkers(max(1, …))` or explicit `workers` field | #620 | issue closed |
| 6 | tombstone branch → `DeleteEmbedding` | #614 | issue closed |
| 7 | sort before truncating at `graphrag.go:1176` | #621 | issue closed |
| 8 | `GenerateQuery` read-only (stop corpus pollution) | #619 | issue closed |

State: `gh issue view 610 611 612 614 615 619 620 621 --json number,state`

Rows 5–8 are deliberately **slices**: #619 keeps its index-vs-stateless-TF
decision, #620 keeps the ~400–500 LOC phantom-deletion bundle, #621 keeps items
2–5, #614 keeps part 2 (revision CAS). Those remainders belong to the epics.

### Track 0 implementation state (2026-07-21, uncommitted)

All eight implemented. `semstreams-reviewer` returned CHANGES REQUESTED; all five
findings are fixed:

| Finding | Where | Resolution |
|---|---|---|
| BLOCKING — tombstone delete made a nil-deref **reachable**; panic killed a worker permanently | `storage.go:206`/`:237`, `worker.go:277` | drop-not-resurrect via `ErrRecordGone`; `recover()` moved inside the loop |
| BLOCKING — `Model == ""` escape hatch reopened #612 on the upgrade path | `worker.go:441` | inverted to unusable; also fixed a second-order `SaveDedup` overwrite |
| HIGH — `HTTPEmbedder.dimensions` race + placeholder split the dedup keyspace | `http_embedder.go:203` | `atomic.Int64`, CAS-once, `DedupKey` withholds a key when unresolved |
| HIGH — rule startup budget was 10 attempts vs every sibling reader's 30 | `entity_watcher.go` | `startup_attempts`/`startup_interval_ms` config, sibling default 30×500ms |
| HIGH — warn→fail e2e conversions need tiers green | — | structural, semantic, statistical all green |

Gate: `go build`, `go vet` (plain + `integration` + `live_llm`), `task lint`,
`go test -race ./...` (0 FAIL), contract tests, tagged integration on all touched
packages, `task schema:generate` (additive only: `workers`, `startup_attempts`).
`task entity-id:audit` is red on 3 candidates — **verified identical at HEAD** in
a clean worktree (1173 extracted both sides), so pre-existing, not ours.

### Track 0 open questions — BLOCKING the PR

Measured against a HEAD baseline built in a clean worktree (two runs, stable):

| statistical tier | HEAD | Track 0 |
|---|---|---|
| `embedding_dedup_hits` | 188 / 190 | **53** |
| `embedding_generated_total` | 256 / 258 | 241 |
| `known_answer_tests_passed` | **7/7** | **6/7** |
| `communities_total` | **15** | **4** |

- **The dedup collapse is explained and correct.** Those ~137 hits came from the
  offloaded lane, which keyed on the ObjectStore *key* — an address, not content.
  They were precisely the stale-vector hits #612 exists to remove. Dedup is now
  disabled for that lane (`message.StorageReference` carries no digest, and
  hashing the body at hop 1 means a second full read on the watcher's hot path).
  **Cost is real and unmeasured: offloaded entities now re-embed on every update,
  each a remote call on the neural tier.** The clean follow-up the implementer
  named — derive the key inside hop 2 where `getSourceText` already holds the
  fetched, truncated body, which also subsumes the inline lane — is the remaining
  part of #612 and should be filed. A `dedup_skipped_total{reason}` counter is
  the minimum; without it this is invisible.
- **Settled by 4 runs per side** (statistical tier, HEAD in a clean worktree):

  | metric | HEAD (n=4) | Track 0 (n=4) | verdict |
  |---|---|---|---|
  | `known_answer_tests_passed` | 6, 6, 7, 7 | 7, 5, 6, 6 | **noise — no regression.** HEAD is not stably 7/7; the single 7/7 that raised the alarm was the top of HEAD's own range |
  | `communities_total` | 15, 15, 15, 15 | 4, 5, 5, 10 | real change, but see next row |
  | `community_ground_truth_passed` | 0, 0, 0, 0 | 0, 1, 1, 1 | **improvement.** HEAD's stable 15 communities match ground truth 0/3 every run |
  | `embedding_generated_total` | 241–257 | 253–256 | unchanged — we are not doing more embedding work |
  | `embedding_dedup_hits` | 173–189 | 60–65 | explained (stale object-key hits removed) |
  | `search_quality_score` | 0.2399–0.2437 | 0.2255–0.2287 | **real, consistent −6.0%. Ranges do not overlap.** |

  So the two alarms resolved opposite ways. `known_answer` was noise. The
  community drop is accompanied by a *consistent ground-truth improvement*
  (0/3 → 1/3), which reads as #619 working — BM25 vectors no longer shift under
  query traffic — rather than as damage; it is also consistent with the audit's
  own finding that community membership is non-deterministic and low-quality.

- **TRACKED, not blocking** (owner call 2026-07-21): `search_quality_score` drops
  6.0% with **non-overlapping ranges** across 4 runs per side. Not noise, but it
  measures a BM25 tier the audit already recommends shrinking, and #619's parent
  decision will move it again. Carry it forward rather than gate on it.

  **Baseline for the next comparison — use these numbers, do not re-quote a single
  run.** Statistical tier, `search_quality_score`, n=4 per side:

  | | runs | range |
  |---|---|---|
  | HEAD (pre-Track-0) | 0.2421, 0.2399, 0.2437, 0.2415 | 0.2399–0.2437 |
  | Track 0 | 0.2255, 0.2261, 0.2287, 0.2283 | 0.2255–0.2287 |

  Re-measure when Epic A touches BM25 (#619's index-vs-stateless-TF decision) or
  when #623 restores dedup on the offloaded lane. If a change claims to improve
  search quality, it has to clear 0.2287 to be distinguishable from Track 0 at all.
  Recorded on #619 as well so it surfaces when that work starts.

---

## Epics

Sequential, not parallel. Each is a candidate OpenSpec change **only if it carries
genuine spec deltas** — A, B, and C qualify; D and E do not.

**Epic A increment 1 is already scoped** (filed 2026-07-21 as #623): derive the
dedup key at content resolution in hop 2, bundling **#623 + #602 + #614 part 2**.
They share one seam — `graph/embedding/storage.go` + `worker.go` — and the key
derivation subsumes #602's cap half, so splitting them means touching the same
two files three times. Start Epic A here.

| Epic | Scope | Issues | State |
|---|---|---|---|
| **A** — evidence cannot silently expire | body TTL, hydration signal, dedup identity, vector reconciliation, BM25 contract, readiness truth | ~~#612 #623 #602 #614pt2~~ (inc.1 ✓) · ~~#600 #616~~ (inc.2 ✓) · ~~#601~~ (inc.3 ✓) · ~~#613 #627 #630~~ (inc.4 ✓) · ~~#599 #597~~ (test-debt ✓ #642) · #619 #633 (deferred owner-decisions) · #643 (spun off) | **COMPLETE (inc.1–4 + test-debt merged/closed); only deferred owner-decisions #619 #633 remain** |
| **B** — one community truth | level-0-only; disable LLM enhancement until ownership split; readiness gate | #606 #607 #608 #609 #617 #618 | not started |
| **C** — derived-state ownership | accept one retention ADR; owner ledger; extend the boot guard; cross-bucket repair loop + ordering protocol | #622 #527 #625 #629 | not started |
| **D** — consumer-path release gates | see prerequisite below | #615 + CI | **in progress** |
| **E** — semsource clean cut | GRAPH stream posture, dead bucket wiring | semsource#110 | not started |

### Epic D has a prerequisite that was not obvious

`ci.yml` runs **no e2e tier at all** — jobs are lint, test, build,
schema-validation, status-check. Building a verification matrix on top of a suite
no machine runs buys nothing. Order: automate a tier first, then strengthen
assertions, then extend coverage.

Shipped 2026-07-21: `.github/workflows/sister-validation.yml` — semsource e2e per
PR, semsource + semboids + `e2e:core` nightly, all against **this checkout** via
`go mod edit -replace`.

**Status on D — first CI run happened 2026-07-21 (PR #626):**

- **The gate fired for the first time** (run 29871032706, `on: pull_request`). It
  works: `go mod edit -replace` built semsource@main against semstreams@main and
  ran semsource's full e2e suite. The nightly-only jobs (semboids, `e2e:core`)
  correctly skipped on a PR.
- **The local prediction was WRONG and is corrected here.** The baton claimed
  semsource@beta.156 "does not compile against main" (fusion API drift:
  `Resolve`→`[]fusion.Seed`, `Entities`→`fusion.Hydration`). In CI, semsource@main
  **compiled and ran fine** against semstreams@main — no compile break. The
  framework integration is healthy: **28,379 entities ingested end-to-end**
  (java 27,875 / web 83 / git 3 / config 35) across all four domains.
- **One narrow failure**, and it is product-domain, not substrate: semsource's docs
  source emits entities typed `"chunk"` where semsource's own e2e test expects
  `'doc'` (`semsource test/e2e/e2e_test.go:1426`, in `TestE2E_OSH_JavaMavenIngest`).
  This is **main-vs-main drift the gate surfaced** — independent of Track 0 (which
  never touched doc/chunk typing) and of this PR (which only added the gate). Per
  the Product Boundary the doc-vs-chunk type is semsource-owned; most likely
  semsource's docs source started chunking while its e2e assertion stayed on the
  old label. **Owner action: confirm on the semsource side and either fix the
  emitter or update the assertion; file a semsource-asks entry if it is actually a
  semstreams contract change.** Do not carry the disproven compile-drift story
  forward.
- **Main has no required checks**, so this gate is advisory until made required.
  A green check that cannot block a merge is a notification, not a gate.

Also note `semspec-validation.yml` has **zero runs, ever** — itself an instance of
the phantom class. Fold into `sister-validation.yml` or delete; do not leave a
workflow that looks like coverage and has never executed.

### Decisions deliberately not made by the audit

These need an owner, not an implementer:

- **Community scope at v1.** Evidence favors shrinking: non-deterministic
  membership, uniformly unweighted edges, three redundant runs, 0–1 of 3 ground
  truth, level 2 has no consumer and level 1 has one e2e probe — and ADR-061
  already established community is post-hoc decoration on the primary search path.
  Recommended: level-0-only now, **disable LLM enhancement** until the ownership
  split, defer the split itself (ADR-scale: ~500 production LOC, storage-key
  contract change, pre-v1 state wipe).
- **Embedding readiness semantics.** "Terminal includes failed" is a decision, not
  a patch. Fits the ADR-084 health-gates frame.
- **BM25 tier contract.** Lexical index over an immutable snapshot, or stateless
  hashed TF. Do not add locks to the current hybrid and call it fixed.
- **ADR-061 is shipped but still marked `Proposed`.** Promote it, and note it never
  claimed the broader "the semantic tier does not affect the partition" contract —
  if that is what we want, say so explicitly.

---

## Timing — DECIDED

**Owner-approved 2026-07-21: do the breaking half now, in one wave.**

Not speculative. Sister lockstep against .157 is already due and verified —
semsource at beta.156 does not compile against main (`fusion.RetrievalClient.Resolve`
now returns `[]fusion.Seed` not `[]string`; `Entities` returns `fusion.Hydration`).
That cost is being paid regardless, so folding the dedup key change, the community
collapse, the config deletions, and the TTL fixes into the same lockstep is close
to free. Post-v1 every one of them becomes a compat shim maintained forever.

Practical consequence for whoever picks this up: **do not defer a breaking fix to
"after v1" on compatibility grounds.** That trade is already resolved. Breaking
changes still owe the house rule — a relevant e2e tier green before the tag
(CLAUDE.md), which is now partly automatable via `sister-validation.yml`.

## Explicitly not doing

Coverage targets (the mechanism that produced this) · more tests (ratios are
1.2–2.1 and the unit tests are good — the rot was in e2e) · more reference designs
(diminishing returns; they are structurally blind to the phantom class) · a twelfth
parallel OpenSpec change.

---

## Log

Append one line per session. Newest last.

- **2026-07-21** — Program opened. Two audits (Codex + Claude) reconciled into
  `prev1-audit-synthesis.md`. 13 issues filed (#610–#622) + semsource#110.
  `sister-validation.yml` written and locally validated; not yet run in CI.
  **Timing decided by owner: breaking wave now, one shot.** Next: Track 0.
- **2026-07-21 (session 2)** — Track 0 implemented (all 8) + reviewed + 5 review
  findings fixed. Uncommitted. Full local gate green; structural, semantic and
  statistical tiers all green. Two corrections to the plan worth carrying:
  (1) **#610's issue text was wrong** — it prescribed "return a transient error,
  the watcher already retries." Nothing retries: `processor.go:481` calls
  `watchEntityStates` once and swallows the error with a Warn, and the degraded
  latch is process-lifetime sticky. Deleting the create path alone would have
  traded a nondeterministic graph-ingest outage for nondeterministic *permanent
  rule-evaluation disablement*. The fix needed a bounded wait too.
  (2) `sister-validation.yml` has zero runs because **the branch was never
  pushed** — the workflow does not exist on GitHub at all (`gh run list` returns
  404, not an empty list). Epic D's "unproven in CI" is really "not yet on main."
  Ran 4 statistical-tier runs per side against a HEAD worktree baseline to settle
  two suspected regressions: `known_answer` was **noise** (HEAD's own range is
  6–7), and the community-count drop came with a consistent ground-truth
  *improvement* (0/3 → 1/3). One real finding survives: search quality −6.0% with
  non-overlapping ranges. **Method note worth keeping:** the first comparison used
  the audit's quoted 43% dedup figure as the baseline and looked like a large
  regression; building an actual HEAD baseline in a clean worktree is what
  reframed it. Quoted numbers from a prior run are not a baseline.
- **2026-07-21 (session 3)** — Track 0 committed, pushed as PR #624 (off `main`,
  not stacked), and **merged** (squash `5e80f676`). Codex reviewed and found 7
  issues beyond the two internal reviewers; 6 real (1 refuted), all fixed in the
  PR. Highlights: the rule startup knobs were a self-inflicted phantom (accepted,
  validated, schema-published, silently dropped by the factory overlay); the KV
  circuit-breaker exemption was missing (mirrors `GetStream` gh#248); and
  `resolved_total` double-counted dedup hits, which had made me report embedding
  volume as "unchanged" when real fresh work went 68→191 (2.81x) — the callback
  that increments `embeddings_generated_total` fires on the dedup-hit path too.
  Follow-ups #623 (Epic A) + #625 (Epic C repair loop). Branch hygiene: the
  duplicate Track 0 code commit was dropped from this branch via
  `rebase --onto`, leaving only docs + `sister-validation.yml`. Next: land this
  branch (first CI run of the sister gate), then Epic A increment 1.
- **2026-07-22 (session 4)** — Track 0 docs+CI branch merged (#626); sister-gate
  fired its first-ever run and **disproved the baton's own prediction** —
  semsource@main compiles/runs fine against main (28k entities ingested); the one
  red is a product-domain `chunk`/`doc` main-vs-main drift, semsource-owned. Then
  **Epic A increment 1 shipped end-to-end**: spec-driven (`/opsx:new` →
  proposal/design/specs/tasks), implemented, two review rounds (semstreams-reviewer
  + PR review), **MERGED as #628 (`a6ea9979`)**, OpenSpec archived. Offloaded-lane
  dedup restored (fresh embedding work 191→68 vs Track 0). Three lessons worth the
  ink: (1) the **superseded-drop-skips-onGenerated** fix regressed the e2e metric
  invariant (`dedup_hits` counted eagerly at lookup, but a dropped hit skips
  `generated_total`) — the semantic tier's invariant gate caught `dedup_hits 166 >
  generated 69`; **the statistical *smoke* had too little same-entity churn to trip
  it, so re-run the tier that exercises the path, not the fastest one.** (2) A
  **security alert on the reviewer agent** was benign fails-without-fix methodology
  the scanner couldn't distinguish from sabotage — verified the guard intact by
  neutralizing it myself. (3) `#623`'s ContentHash producer→consumer move
  *simplified* `#614 part 2` (removed the read-to-copy). Follow-ups filed: #627,
  #629, #630. Next: Epic A increment 2 (#600 + #616).
- **2026-07-22 (session 5)** — Inc 2 (#632), inc 3 (#635), and both retrospective
  rounds (#636, #638) all shipped since session 4; main tip `08d7c5c2`. **Racked and
  stacked the remaining Epic A surface** and found the baton's "4 remaining"
  (#613/#619/#599/#633) undercounted it: increments 1–3 spun off **four more issues on
  the same embedding seam** (#625/#627/#629/#630) that were never folded back into the
  epic. Real remaining surface = 8, plus #597 to reconcile. Owner decisions:
  (1) **inc 4 = #613 + #627 + #630** — one worker.go hop-2 seam, one e2e tier; #613's
  owner-call settled ("terminal" excludes "failed"; track a `failed` gauge; a
  *configurable* failure-ratio drives `degraded`). (2) **#625 + #629 → Epic C** (both
  cross-bucket/ownership-shaped; #629 dormant). (3) **#619** (interim shipped, bleeding
  stopped) and **#633** (owner-accepted growth) → deferred owner-decisions.
  (4) **#599 + #597** → test-debt/reconcile. Also: **new merge-gate** owner directive
  recorded — a code PR merges only after Codex has posted a review, it's addressed, and
  CI is green (in that order). Next: `/opsx:new` for inc 4.
- **2026-07-22 (session 6)** — **Epic A inc 4 SHIPPED end-to-end and archived** — change
  `embedding-readiness-and-dedup-efficiency`, spec-driven (proposal/design/2 spec deltas/
  tasks, validates `--strict`). Implemented by semstreams-developer, APPROVED by
  semstreams-reviewer (mutation-tested), `task e2e:semantic` GREEN (breaking gate) with the
  degraded path integration-covered, merged as **PR #639 (`fa1041a8`)**. The Codex gate did
  its job: it found **2 P1 correctness bugs the reviewer missed** — (1) the failure map was
  not revision-aware (an older completion could clear a newer failure), (2) a persistence
  miss (SaveFailed/SavePending error) was mis-classified as terminal-skipped, clearing the map
  + advancing readiness — plus 3 P2s (snapshot/live race, cold-start singleflight bypass,
  unbounded scanned reason labels). All 5 fixed with failing-first tests + a second reviewer
  pass (`b7a29f58`) verifying finding 2 does NOT reintroduce the watermark deadlock. #613 +
  #630 closed. Lessons: (a) my initial #613 framing ("must not advance the watermark") was
  WRONG — the watermark advances by design (deadlock avoidance); the fix is a `FailedCount`
  input to the shared projection. (b) #627 was already fixed by inc 1 — verified before
  building; closed as verify-only, no phantom follow-up. (c) the owner ran no bespoke
  degraded-e2e because integration coverage is genuine — no follow-up filed (anti-proliferation).
  Then **TAGGED + PUSHED `v1.0.0-beta.158`** (`d6d3e57e`) — owner-decided to release the
  4-BREAKING wave now (owns the sisters; wants a tight cadence so migration stays small; sister
  tests are the signal). Pre-tag gates: build-tag sweep (integration+live_llm) clean +
  `e2e:semantic` GREEN at HEAD on the FINAL code incl the 5 Codex fixes (the earlier e2e ran on
  pre-fix `037af8f3` — re-ran at HEAD to close the beta.18-style gap). Annotated house-format
  tag names each breaking change + migration doc; sisters not touched (owner manages them).
  **Next: OWNER DECISION on the next unit — Epic B, Epic C (now carrying #625/#629), or the
  deferred Epic A owner-decisions/test-debt (see Next Action).**
- **2026-07-23 (session 7)** — **Epic A test-debt (`#599 + #597`) SHIPPED + CLOSED; Epic A now
  fully complete.** New `validate-batch-read-reconciliation` e2e:semantic stage (test-only, PR
  **#642 `db64d2c6`**) drives `graph.query.batch` + the production `fusionnats.Client.Entities`
  over the live wire — gh#604 missing→unhydrated reconciliation + exactly-once per reply +
  load-bearing `batch_query_missing_total`. Pipeline: semstreams-developer → semstreams-reviewer
  (1 HIGH: verdict overclaimed "resolved" — fixed) → Codex gate (**5 real holes, 3 P1, two
  reviewers missed**: production-client reconciliation untested, counter presence-only,
  count-only `assertAllHydrate`, pre-warmed reorder, non-evicting soak) → all addressed. Lessons:
  (1) **my own over-tightening** — I directed an exact `==` counter invariant; the counter is
  process-global, so exact/upper-bound would flake red on unrelated traffic. Changed to a
  **lower-bound** gate (proves increment / catches silent-stop + reset); present-gap detection
  stays where it's deterministic + attributed (the hydration guards). (2) **Honest downscope beats
  a faked signal** — the gh#604 reorder-under-cache-miss and a real gh#597 cache-residency soak
  are unreachable (entity cache hard-coded 5000/30s over ~74 entities never evicts); filed **#643**
  (cache-control seam) rather than pre-warm an all-hit request into a false pass. Fuse envelope →
  **#391** (re-home note posted). (3) **The out-of-band Codex flow**: no GitHub-integrated bot —
  the owner runs it and relays findings under their account; I cannot trigger it. #599 + #597
  CLOSED (owner: #597 guarded + countable, NOT proven closed). Also reconciled the tracker — **7
  merged-but-open issues (#600 #601 #602 #611 #612 #614 #616) closed**. Merged CLEAN, second Codex
  pass waived. **Next: the SAME owner decision — Epic B / Epic C / deferred Epic A (#619 #633 #643).**

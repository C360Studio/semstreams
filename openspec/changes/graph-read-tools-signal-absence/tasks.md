# Tasks — graph-read-tools-signal-absence

**Amend a task line when the work HAPPENS, not only when it succeeds.** A `[~]` is a recorded decision and MUST also
be noted in the spec delta. No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing in any
OPEN task line as a live caveat; use "pause seam", "barrier", "abort", "does not compile", "MUST fail".

Premises measured on `main@797d294a`: `processor/agentic-tools/executors/graph_query.go:442` (dead `type` compare),
`:531-540` (stub), `:591-617` (no `IsRelationship`), `:428-431` (silent continue), `graph/types.go:24-47`,
`natsclient/kv.go:522-547,558-598`, `graph/kvcatalog.go:261`, `pkg/types/entity_id.go:160-164`,
`release/tier1-packages.txt:79`, `test/e2e/scenarios/agentic/approval_signal.go:36-40,77-88,139-144`,
`pkg/types/entity_id.go:166-186`, `processor/graph-query/graphrag.go:1570-1592`, `test/e2e/mock/cmd/main.go:38`.
Round-4 additions, measured 2026-09-07: `graph_query.go:325` (`direction` defaults to `both`),
`agentic/tools.go:44-59,551-555,576-610` (the pagination contract),
`processor/agentic-tools/loop_result.go:27,55-56,65,78,157-158` (the roster's live producer),
`processor/agentic-loop/result_hint.go:69-88` + `handlers.go:2639` (the live consumer),
`graph/query_prefix_types.go:41-44,74,78,84` (cursor codec and its full-scan cost note),
`processor/graph-ingest/query.go:310-327` (sort-then-cursor), `natsclient/kv.go:582-600` (scan order, no sort),
`vocabulary/namespace_authority.go:47-54,101-118`, `graph/query_batch_types.go:22-46` (`MissingReason`),
`processor/research-graph-execute/adapters.go:55-110,156-159,183-185`.

Sequencing: 1.6 governs the Codex coordination and is re-measured, never quoted — the held set moves and this line
has been stale twice. Measured twice on 2026-09-09 alone: **224** in the morning and **229** by the
implementation review (54 + 181 + 7, then higher again; it was 180 on 2026-09-07 and a stale 176 at round 2, all of
the growth in #1159). Quote none of these — run the command. The implementation's file set — including
4.5's `test/e2e/mock/cmd/main.go` and `test/e2e/scenarios/agentic/approval_signal.go` — intersects none of them, and
the delta is ADDED-only. `approval_signal_test.go` IS held (as is `scenario.go`) while 4.5 edits
`approval_signal.go`: a same-function, not same-file, coordination point.

## 1. Claim and design

- [x] 1.1 Draft PR #1262 opened with `Closes #1261` on `claude/gh1261-graph-read-tools`, own worktree; the OpenSpec
      change is its first commit.
- [x] 1.2 Architect verification pass over the explorer inventory — `inventory-verification.md` (2 strikes, 11 additions).
- [x] 1.3 Independent inventory review (`semstreams-reviewer` re-derivation) recorded on PR #1262 — INVENTORY CHANGES
      REQUESTED (2 blocking rows: `graph.query.summary` as the same-class owner of "IDs by type"; the neighbors budget vs
      spec `:467`). Architect amendment in progress; re-review follows.
- [x] 1.3a Explorer inventory materialized as `inventory.md` with a parseable `base:` line. `task inventory:verify` on it:
      119 pins, 15 ok, 5 moved, 34 drift, 65 malformed, 44 unparsed — the explorer's table/range format does not fit the
      verifier grammar (#1256), so the malformed/unparsed counts are grammar, not drift; the 5 MOVED rows in
      `message/triple.go` (`:56→58`, `:61→63`, `:70→74`) are real pin errors at the explorer's own base and confirm the
      reviewer's HIGH. Re-pin the rows the design rests on; do not treat the verifier's exit as a gate here.
- [x] 1.3b Architect amendment folding the review and the owner note (#1261, 2026-09-05): both BLOCKING rows closed
      (`graph.query.summary` as the same-class owner; the budget classified as a model-facing cap distinct from spec
      `:467`), pins fixed, six rows added; `design.md` gained § Budget, § Break classification and sequencing, and § Tool-preference premise;
      owner questions renumbered 1–11.
- [x] 1.3c Re-review of the amended inventory (`semstreams-reviewer`) recorded on PR #1262 — INVENTORY CHANGES
      REQUESTED (round 2): BLOCKING `graph.index.query.predicateList` as the same-class owner of "which predicates
      exist"; HIGH `hierarchyStats` as a second owner of "which types exist"; four pin corrections; the ADR-106 `:81`
      half-quote (RC-6); two premise pins inside Codex-held files. Round 1's six findings confirmed closed.
- [x] 1.3d Architect amendment round 2 folding 1.3c (owner rows added, ADR-036 case-against rewritten on the
      predicate-catalog owner, RC-6 walked path named, pins fixed) plus the external-evidence table from the Cekikj
      restatement (Part 2 § 2.3/§ 2.5, Part 3 § 3.3/§ 3.4/§ 3.8) in `design.md` § Tool-preference premise.
- [x] 1.3e Re-review round 3 (`semstreams-reviewer`) recorded on PR #1262 — INVENTORY CHANGES REQUESTED: BLOCKING
      `graphrag.filterEntityIDsByType` (ADR-071) as the existing type-segment selector; HIGH the approval walk's
      `status=success` assertion cannot distinguish a served listing from an empty one (RC-6); MEDIUM `:470` in
      `design.md`; NIT ADR-106 `:81-83`; unrecorded `ENTITY_SUFFIX_INDEX` owner. Round 2 confirmed closed.
- [x] 1.3f Architect amendment round 3: per-axis same-class sweep recorded (type selection, predicate presence,
      relationships, ID fragment, neighbor expansion, absence signaling, positional-wildcard listing); the tools'
      matcher named as `MatchEntityIDPattern` with the ADR-071 divergence stated; the fixture claim corrected (the
      agentic tier ingests no sensor entity); RC-6 walked path re-based on 4.2 + new 4.5.
- [x] 1.3g Re-review round 4 (`semstreams-reviewer`) recorded on PR #1262 (comment 4, over `67a921ff`) — INVENTORY
      CHANGES REQUESTED: BLOCKING an ABSENT AXIS, bounded-result continuation (`ToolDefinition.Paginated` +
      `MetadataKey{HasMore,NextOffset,NextCursor}`, live producer `read_loop_result` on the same roster, consumer
      `decorateContentWithPagination`, listing owner `PrefixQueryResponse.NextCursor`) — the design's `truncated`/
      `too_large`/`frontier_remaining` is a second spelling of "there is more"; HIGH the absence-signaling search
      was identifier-only and missed three prose-absence producers (`websearch.go:191`, `personas.go:178`,
      `rules.go:255`); MEDIUM `not_found` vs `unresolved` in the same executor; MEDIUM `filter_registered` vs
      `vocabulary/namespace_authority.go` delegation policy; MEDIUM Codex-held set is 180 paths, not 176, and 4.5's
      two files are outside the enumerated set (intersection still empty); NIT `EntitySampleTruncated`. Round 3
      confirmed closed. The BLOCKING is an absent axis, not a fourth piecemeal owner; the other three are present
      axes with blind search shapes.
- [x] 1.3h Architect amendment round 4 (Opus session, owner ruling G1) LANDED. Inventory gained six rows: the
      continuation axis (Q12 — adopted on `query_by_type`, refused on `query_neighbors` with the reason recorded),
      the prose-absence adoption sweep replacing addition 2's withdrawn "no adoption sweep owed" clause, the
      `not_found`/`unresolved`/`MissingReason` vocabulary row, the predicate-registration authority row, the
      `processor/research-graph-execute/adapters.go` row (#1261 comment 6), and the 180-path re-measure with 4.5's
      file set; the `EntitySampleTruncated` NIT landed in the round-1 row it belongs to. Design and delta applied
      Q1 (64KB), Q4 (`direction` → `outgoing`; explicit `incoming`/`both` → `invalid_args` naming the incoming
      owner), Q6 (three right-anchored tokens), Q9 (`feat(agentic-tools)!:`), Q12 (`Paginated: true`, `cursor`
      argument, `has_more` + opaque `next_cursor` through the graph package's existing codec, `truncated` deleted),
      and cross-referenced #1265 (Q7) and #1266. Two premises were measured differently from the round-4 verdict and
      are recorded in `inventory-verification.md`: `natsclient.FilteredKeys` does NOT sort, and a cursor cannot seek
      (each page re-lists and re-sorts). Delta stays ADDED-only; `openspec validate --strict` passes.
- [x] 1.3i Closure-only re-review round 5 (`semstreams-reviewer`, over `ebeacdbf`, owner ruling G1) — **INVENTORY
      PASS**, recorded on PR #1262. All six round-4 findings confirmed closed with pins; every guard re-measured
      independently (ADDED-only; `openspec/specs/` untouched; 180 Codex-held paths, intersection empty; no new
      `ToolResultHint`; `--strict` valid); the twelve rulings landed at the ruled level with no ratchet and no
      shortfall. No new axis. One MEDIUM — a false "first positional-wildcard `ListKeysFiltered` in the tree" novelty
      claim in `design.md` and 4.2, which the reviewer judged non-gating — was corrected in the same round rather than
      deferred, so the owner reads accurate text at 1.4: positional wildcards are `KeysByFilter`'s documented purpose
      (`natsclient/kv.go:528-530`) with real-NATS precedent at
      `processor/graph-index/owner_filter_integration_test.go:139-148`, whose cancelled-context rejection 4.2 now
      mirrors.
- [x] 1.4 Owner **INVENTORY PASS GIVEN 2026-09-09**, recorded verbatim on PR #1262 ("continue with inventory pass"),
      over head `202bd97f` after five reviewer rounds (1–4 CHANGES REQUESTED, 5 PASS). It authorizes sections 3–6 and
      NOT merge; the merge gate is unchanged and separate. Owner rulings on questions 1–12 and gates G1/G2 RECORDED
      2026-09-07 (#1261 comment 7; recommendations in comments 5 and 6) and APPLIED by 1.3h; questions A–F ruled
      2026-09-09 (`proposal.md` § Owner rulings, second round).
- [x] 1.5 Milestone `v1.0.0-beta.165` placed on #1261, #1260 and PR #1262 (owner ruling Q10, 2026-09-07).
- [x] 1.6 Coordination rule ACTIVE (the hold was relaxed by owner ruling Q11, 2026-09-07, to archive-order
      coordination; 1.4 INVENTORY PASS given 2026-09-09, so sections 3–6 are open). Standing rule while Codex's
      #759/#1146 stack (PRs #1156/#1159/#1141) is open: rebase on `main` after each stack merge; `task e2e:agentic`
      green before this PR's own merge; the delta stays ADDED-only until the stack's `agentic-tools` delta archives.
      **Pre-3.1 re-check done 2026-09-09**: the paginated Codex set has GROWN to **224** unique paths
      (54 / 181 / 7 — PR #1159 moved 137 → 181 since 2026-09-07), and the intersection with this change's
      implementation set is still **empty** — every file sections 3–5 touch is free (`executors/graph_query.go`,
      `register_graph_query.go`, their tests, `test/e2e/mock/cmd/main.go`,
      `test/e2e/scenarios/agentic/approval_signal.go`, the migration doc). The same-function coordination point
      stands and has widened: `approval_signal_test.go` AND `scenario.go` are both held while 4.5 edits
      `approval_signal.go`, which is not. Both premises inside held files re-pinned and UNCHANGED at this base:
      `httpMaxTextSize = 20000` is `executors/httprequest.go:23`; `admitToolCall`'s admission seam still opens at
      `processor/agentic-tools/component.go:974`. Re-run this check after each stack merge, not once.

- [x] 1.7 `HintEmpty` adoption sweep FILED as **#1270** (`v1.0.0-beta.165`, `class:advertised-absent`,
      `horizon:pre-v1`) — owner ruling E, 2026-09-09; same milestone as this change so parent and child sit in one
      tag range. Three planes enumerated and pinned in `inventory-verification.md` § round-4 rows
      (`executors/websearch.go:191`, `personas.go:178`, `rules.go:255`), verbatim-confirmed before filing.
      Enumeration only — this change migrates none of them and is not held on the count (architect contract,
      establishing side; owner ruling 2026-09-01). #1270 is blocked until this change lands: a contract's first
      adopter proves the pattern before three more sites copy it.
- [x] 1.8 Ruling F, 2026-09-09: the release-gate blind spot behind ruling 10's tag-range caveat FILED as **#1271**
      (`v1.0.0-rc.1`). `processor/agentic-tools/executors` is Tier 1 (`release/tier1-packages.txt:79`), so
      `task api:compat` covers this package and passed green on this PR while every break in it is model-facing
      JSON apidiff cannot see. No action inside this change: ruling 9 already makes 5.1's changelog line name the
      model-facing result shapes.

## 2. Spec delta

- [x] 2.1 ADDED requirements only in `specs/agentic-tools/spec.md` — three at first draft, four after 1.3h; no
      MODIFIED block, because `openspec/specs/agentic-tools/spec.md:435/:467/:487` are MODIFIED by PR #1159's pending
      delta (`openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md`, Codex-held).
- [x] 2.2 Delta reconciled against the 2026-09-07 owner rulings (applied in 1.3h): a fourth ADDED requirement for the
      `direction` narrowing (Q4); `query_by_type` gains the three-token arity (Q6), the sort-before-page clause, and
      the pagination contract while losing `truncated` (Q12); `query_neighbors` gains the explicit refusal to
      announce continuation without a token (Q12); the first requirement gains the paragraph separating declaration
      status from minting authority. Still ADDED-only.

## 3. Code

- [x] 3.1 `graph_query.go`: `KVKeyLister`; a pattern BUILDER (one to three right-anchored tokens → six-position
      pattern, validated by `ValidateEntityIDPattern`) shared by `entity_type`/`filter_type`, matching through
      `MatchEntityIDPattern` — no new type extractor or matcher; `queryByType` served, sorting the lister's output
      itself and paging through `graph.EncodeCursor`/`graph.DecodeCursor` with `Paginated: true`, a `cursor`
      argument, and `has_more`/`next_cursor` in `ToolResult.Metadata` (no `truncated` field); `query_relationships`
      serves `direction: outgoing` only and refuses an explicit `incoming`/`both` as `invalid_args` naming the
      incoming owner; `extractRelationships` typed over `EntityState` with `IsRelationship()`, dead branch
      deleted; `predicates_present`/`filter_registered`; neighbors 64KB budget (constant commented beside
      `bashMaxOutputBytes`), `unresolved`, hints, and NO `has_more`; descriptions rewritten.
- [x] 3.2 `register_graph_query.go`: adapter `KeysByPattern` via `natsclient.FilteredKeys`.

## 4. Tests

- [x] 4.1 Unit tests named in `design.md` § Test plan; fixtures via `graph.MarshalEntityState`; `// spec:` citations.
- [x] 4.2 Integration `TestIntegration_QueryByType_ListsFromEntityStates` against real NATS; asserts sorted output
      and one cursor continuation across two pages, and mirrors the precedent's cancelled-context rejection
      (`processor/graph-index/owner_filter_integration_test.go:139-148`: a cancelled ctx yields
      `context.Canceled` and a nil key slice, never a partial list).
- [x] 4.3 Fails-without-fix for the `IsRelationship` filter and the segment match, run against the committed state.
- [x] 4.4 `predicate_authority_contract_test.go` unchanged and green.
- [x] 4.5 Booted-binary walk for `KVKeyLister` (RC-6): `test/e2e/mock/cmd/main.go:38` pins
      `{"entity_type":"agent.execution","limit":5}`; `approval_signal.go` prompt text follows; after the success
      metric, `walkApprovalPath` reads `tool.result.<pending.CallID>`, decodes `ToolResult.Content`, asserts
      `pattern == "*.*.*.agent.execution.*"`, `matched >= 1`, `entity_ids` ∋ the primary loop's
      `LoopExecutionEntityID`. Neither file is Codex-held; `scenario.go` (held) is not edited.

## 5. Docs

- [x] 5.1 `docs/operations/migration-graph-read-tools.md` (home per owner ruling Q5): before/after JSON for the four
      model-facing flips — `IsRelationship()` row filtering, `filter_type` honoured on `query_neighbors`, the 64KB
      budget, and the `direction` narrowing (its own row, ruling Q4) — plus the `query_by_type` stub → served listing
      and its `truncated` → `has_more`/`next_cursor` continuation.

## 5b. Implementation findings escalated, not absorbed

- [x] 5b.1 **`query_neighbors` had a pre-existing depth off-by-one, and this change would have made it read as
      authoritative. RULED 2026-09-09: fix it here, before the archive.**
      `neighborWalk.run` seeded `frontier` with the SOURCE id and looped `for hop := 0; hop < depth`, so at the
      advertised default `depth: 1` hop 0 consumed the source itself (skipped by the `id != w.sourceID` guard),
      queued its targets, and the loop exited before reading any of them — `neighbors` came back EMPTY for an entity
      that has neighbors. Verified pre-existing at `origin/main`: identical shape, `frontier := []string{entityID}`
      and `for d := 0; d < depth`, with `depth := 1` the advertised default (`main:404-417`). **The interaction was
      the finding**: before this change that call answered `count: 0` with no hint, which was merely uninformative;
      this change classifies the same zero as `HintEmpty`, so the model would be told with a typed contract "this
      succeeded and there is nothing here, broaden your filter" about an entity whose neighbors exist. A silent
      under-answer becomes a confident false negative, produced by the very hint contract this change introduces.
      Escalated rather than absorbed (it is a fifth model-facing flip, and no earlier ruling covered it); the owner
      widened the change by that one flip.
      **The fix** names the invariant instead of adjusting an index: the source occupies ring 0 and is never its own
      neighbor, so a walk of `depth` hops needs depth+1 rings — `for ring := 0; ring <= depth`. The advertised
      contract was already correct (`"Number of hops to traverse (default: 1, max: 3)"`); the code disagreed with
      it, so no tool description, argument schema or exported signature changes and `api:compat` cannot move.
      **The tests stopped agreeing with the defect.** `fixtureNeighborDepth` went 2 → 1, the tool's real default,
      and the "expands THROUGH a filtered-out neighbor" subtest went 3 → 2, the depth its fixture actually needs;
      the constant's apologia is deleted. Every one of the eleven pre-existing `query_neighbors` tests passes at the
      honest depth with no assertion touched — they had been written for correct semantics and forced to over-ask.
      New `TestQueryNeighbors_DefaultDepthReturnsDirectNeighbors` calls the tool the way its schema documents it,
      with **no `depth` argument at all** — the shape the defect hid in, since every other test passed an explicit
      depth and quietly passing 2 to mean 1 is what let it survive four review rounds.
      **Mutation-killed**: restoring `ring < depth` fails the new test on all three of its claims, including
      `Should not be: "empty"` — the confident false negative itself, reproduced — and fails
      `TestQueryNeighbors_FilterTypeReadsIDSegment` and its subtest too, so the honest depths are load-bearing and
      not merely cosmetic. Delta gains a normative `depth` sentence and a `the advertised default returns direct
      neighbors` scenario; `task spec:properties` 72 → **73/73**.

## 5c. Substrate observation, attributed and not re-rolled

- [x] 5c.1 One local `scripts/run-integration-tests.sh` run went RED at `9683345d` (`ok: 98 / FAIL: 2`,
      `TestIntegration_ReplyWithHeaders`, a container-connect EOF at `startNATSContainer` — not an assertion) and
      green on a re-run of the same tree. **The re-run is not the evidence** ([[feedback_no_rerun_to_green_fix_first]]);
      the attribution is. Established: the branch changes **zero** files under `natsclient/`
      (`git diff --name-only origin/main..HEAD -- natsclient/` → 0); the test passes in isolation on the same tree;
      host load average was 11.22 with a second session running; the script's own banner says "uncapped package
      parallelism". That is the exact shape of **#736** (OPEN, `class:flake`, `area:ci`, `area:natsclient`,
      `horizon:pre-v1`) — "The integration suite oversubscribes Docker under package parallelism; sub-second tests
      time out" — which names `natsclient` and recommends `-p 1`. The `-p 2` form of the gate is green.
      **This does NOT reach the merge gate's waiver trigger**, which fires on arming auto-merge past a
      *previously-red required job*: CI's `Test` job runs that same script and has been **green on all seven pushed
      heads of this branch**, implementation heads included. The red is a laptop-contention observation, not a CI
      observation. No waiver is sought and none is owed; #736 remains open and unfixed, and a future CI red of this
      shape on this branch WOULD need one.
- [x] 5c.2 The depth fix's first full `go test -race ./...` went RED with **one** failure —
      `TestMaybeStartPProf_Enabled_ServesPprof` (`service/`), `pprof server error: listen tcp :63771: bind: address
      already in use`. Attributed, not re-rolled ([[feedback_no_rerun_to_green_fix_first]]): it is **#1120**, already
      OPEN with `class:flake` and named precisely — "freePort probe-then-bind race (bind :0, read, close, re-bind
      later) — ten call sites; fires under host contention". `freePort`
      (`service/service_manager_health_listener_test.go:277-286`) binds port 0, reads the assigned port, closes the
      listener, and returns the number; `MaybeStartPProf` binds it later, and anything on the host may take it in
      between. Established rather than assumed: this branch changes **zero** files under `service/`
      (`git diff --name-only origin/main..HEAD -- service/` → 0), `pprof_test.go`'s last commit is `26b0c2ce` from
      #299, and the test passes **20/20** in isolation under `-race -count=20`. No new issue filed — filing a
      duplicate of #1120 would be the noise the filing-discipline rule exists to prevent. **Surfaced for owner
      placement**: #1120 has NO milestone and is the same "flake that haunts our CI" class as #1268, which was just
      placed on beta.165; whether it joins it is the owner's call, not this change's.


## 6. Gates

- [x] 6.1 All green, every item measured on the FINAL code head **`4dbb2080`** (the depth fix) — ONE evidence set, deliberately.
      A gate ticked over a superseded tree is not evidence for the tree that ships, and a line asserting two values for
      the same measurement is worse than one that is merely stale (see 6.3, round 3 MEDIUM 2). Each gate re-run here
      independently of the implementer's report — a subagent's state claim goes stale — and the DENOMINATOR is
      checked, never the exit code alone:
      `task lint` 0 · `go test -race ./...` **153 ok / 20 no-test / 0 FAIL** (the first run of this head was RED
      on one unrelated `service/` test — attributed to #1120, not re-rolled; see 5c.2) ·
      integration via `scripts/run-integration-tests.sh` with `SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS=1800`
      **153 ok / 0 FAIL**, banner confirming it ran (an earlier attempt exited 0 having run NOTHING on the
      `/tmp/semstreams-integration.lock` host lock held by a concurrent session — an exit 0 from that script is not
      evidence the suite ran, check the `ok` count) · `openspec validate --strict` valid ·
      `task spec:properties` **73/73** · `go run ./cmd/entity-id-audit .` 0 (**1323** candidates) ·
      `task schema:generate` 0 drift in `schemas/ specs/` and 0 dirty tree-wide ·
      `task api:compat` summary **`compared: 62 · clean: 50 · incompatible: 12 · FAILING TOTAL: 12`** against base
      `v1.0.0-beta.162` — the pre-existing baseline exactly, and its exit 201 is the documented pre-RC posture, not a
      regression (CI's Tier 1 job runs `API_COMPAT_MODE=report`). The strongest form of this evidence is not the
      count but that `task api:compat:report` is **byte-identical** (`diff` clean) to the report at the previous
      head: the depth fix changes no signature, so no API surface can have moved. This package's listed breaks stay
      the pre-existing `Flow*` removals with `KVKeyLister: added` under Compatible changes. All THIRTEEN test names
      the delta's scenarios pin exist in the tree.
- [x] 6.2 `task e2e:agentic` **GREEN on the final head `4dbb2080`** — exit 0, `assertions_run=14`, and task 4.5's
      assertion demonstrably fired: `approval_listing_matched:2`, the booted binary executing the served
      `query_by_type` and the approval walk reading two matched identities. Re-run here deliberately: the first
      green was at `92fd2c5e`, three code commits back, and the repo's hard rule attaches the tier to what LANDS —
      `02414da7` is `feat(agentic-tools)!:`. The reviewer's risk read was near zero (the tier calls `query_by_type`
      without a cursor and never calls `query_neighbors`, so no round-2 or round-3 change touches the proven path)
      and the beta.18 case study in `CLAUDE.md` is exactly a near-zero judgement that was wrong. It is a ~45s tier.
      Substrate note: the tier was initially unrunnable because Docker Desktop's `docker-credential-desktop` hangs
      (`docker pull` → `error getting credentials - err: signal: terminated`, exit 124 twice). Cleared WITHOUT
      touching `~/.docker/config.json` by pulling the three base images once through an isolated credsStore-free
      `DOCKER_CONFIG`; the normal config then builds from cache. Do NOT point `DOCKER_CONFIG` at a bare directory —
      it breaks Compose plugin discovery and context resolution. The wedged helper is a machine condition, unfixed.
- [x] 6.3 `semstreams-reviewer` implementation pass. **Round 1 ran 2026-09-09 over `92fd2c5e` — CHANGES REQUESTED**,
      recorded on PR #1262. One BLOCKING, one HIGH, four MEDIUM, four NIT; every ruling and A–F found implemented at
      the ruled level with no deviation. BLOCKING: `graph.DecodeCursor` is bare base64
      (`graph/query_prefix_types.go:82-93`), so `graph_query.go:1030-1049` refuses only UNdecodable cursors — a
      decodable non-token like `"MQ"` (→ `"1"`) sorts before every key and returns full page 1 with a fresh cursor,
      the infinite page-1 loop owner ruling B forbids, contradicting both the code comment at `:1027` and the
      migration doc. HIGH: the 64KB budget charges compact `len(raw)` while the result ships as
      `json.MarshalIndent`, measured 96,658 bytes of content against a 64,740-byte meter — ~47% over, and the test
      reconstructs the meter's own arithmetic instead of measuring `len(result.Content)`. Both mechanisms
      independently re-derived from the code before dispatch. Code findings returned to the implementer; the two
      `openspec/` findings (a delta scenario citing a test that does not verify it; a stale `proposal.md` header
      declaring the INVENTORY PASS un-given) fixed here.
      **Round 2 ran over `1608e711` — CHANGES REQUESTED**, and it caught a defect the round-1 fix created at its own
      seam ([[feedback_a_fix_creates_the_next_defect_at_its_seam]]): the new canonical-ID validation ran
      unconditionally on a decode, and `graph.DecodeCursor("")` returns `("", nil)` by documented first-page
      contract — so `cursor: ""` was refused and the FIRST page became unreachable, with a message telling the model
      its cursor "decodes to \"\"". It also made `cursor` the only optional string in this executor refused when
      empty (`direction` at `:500` and `relationship_type` at `:425` both read empty as omitted). Round 2's MEDIUM 1
      found the second seam: between the emitted-size trim and the unresolved/empty split, an all-unresolved walk
      admits nothing, so `fitEmitted` returns without setting `truncated` and `HintEmpty` rightly declines — a
      114KB body against a 64KB cap with no flag, no hint and no error. Both fixed in `09d30ba5` and both
      mutation-killed (guard removed → the empty-cursor subtest fails; hint arm removed → the over-budget subtest
      fails). Round 2's three text corrections applied: `design.md`'s R3 invariant and two prose sites still argued
      for the Σ-record-bytes proxy the fix replaced, the delta's `frontier_remaining` gloss no longer described the
      field once records could be given back, and the migration doc repeated the over-claim. Round 3 owed before 6.4.
      **Round 3 ran over `4e999b30` — APPROVE.** No blocking or high findings; both round-2 defects verified closed
      under the reviewer's OWN mutations rather than on the coordinator's report, and the arm-precedence question
      answered definitively: the size check cannot shadow `HintEmpty`, because the envelope with both `neighbors`
      and `unresolved` empty is bounded under 1KB by `MaxEntityIDBytes` on `source_entity`/`filter_type`/`pattern`
      plus three ints and two bools — two orders of magnitude below the cap. It also established that
      `decoded == "" ⟺ cursor == ""` under `RawURLEncoding` (length 1 is always illegal base64), so the empty-cursor
      guard opens no path for a non-empty non-token. Its two MEDIUMs were both coordinator errors and are fixed:
      the new test had been pasted between `neighborWideFixture`'s doc comment and its function, leaving that
      comment describing the opposite fixture and the helper bare — invisible to revive, `go vet` and every test —
      and 6.1 had been re-pointed by prepending rather than replacing, so it asserted two values for the same
      measurement. Fixed in `0676c77d` and in this commit respectively.

- [ ] 6.4 Archive as the final content commit; narrow archive-sync check.

# Design: e2e-tiered-warn-only-stages (issue #1426, task 1.2)

base: e052742a (inventory base `aaddc292`; `git diff --stat aaddc292..HEAD -- test/ cmd/ taskfiles/ .github/` is empty, so
all 168 inventory pins hold at HEAD; only the three openspec files changed). Inventory: `inventory.md`, INVENTORY PASS
recorded on #1426 (task-1.1 landing note). Read-only design pass; no Go file was edited; `AssertionsRun` untouched.
Revision 2 (coordinator correction, same day): `GlobalSearchRequest.IncludeSummaries` exists and no probe used it — P2
rewritten, P14 added, § 2 rows 1-4, § 4-6, § 8 F2 and § 9 revised; D5 dropped.

## § 0 Words that bind (verbatim)

- Owner, 2026-09-29 (#1426): *"pull the warn-only slice into 163"*; *"agree on 1 - let's fix the class while we are in
  here"* — all fourteen paths are in scope; the acceptance sentence stands: no stage that passes on every outcome
  remains in a per-PR variant.
- Owner, 2026-09-29 (#1117, issuecomment-5894112362): *"path-only gate for 1117"* — a path check asks whether the
  plumbing worked and the framework owns every step; a quality check asks whether the small model's answer was good,
  which no PR can change. Three quality stages leave behind an env flag: `validate-thematic-answer-eval`,
  `validate-llm-enhancement`'s 120 s wait, `validate-globalsearch-known-answer`. This change lands first; PR #1425
  rebases.
- Owner, 2026-09-27 (#1222): no parallel assertion-accounting; `AssertionsRun` stays with #1222.
- Owner, 2026-09-22: simple over edge-case code — a doc sentence beats a mechanism; the smallest fix; no new
  framework. A design pass never ratchets complexity up; additions become owner questions (§ 6).
- Proposal boundary: no model is made faster and no 10 s deadline changes without a measurement.

"Per-PR variant" below means a variant the E2E Ladder runs on `pull_request`: `statistical` today
(`.github/workflows/e2e-ladder.yml:74-122`), plus the #1117 semantic path-only run once PR #1425 lands. `structural`
is run by no workflow (`git grep "e2e:structural\|variant structural" -- .github/ taskfiles/` → only
`taskfiles/e2e/structural.yml:20`).

## § 1 Premises (each measured)

P1. Exit-code mechanics: a stage fails the tier only by returning a non-nil error (`tiered.go:449-452`); `Success` is
set true unconditionally after the last stage (`:567`); `Warnings` is never read on that path and is printed nowhere
(`git grep -n "range result.Warnings\|len(result.Warnings)" -- test/e2e/scenarios/ cmd/e2e/` → one hit,
`results.go:449` `WarningCount`). A warning reaches only the results JSON `metadata.warnings`.

P2. Every `globalSearch` strategy and `localSearch` reach `synthesizeQueryAnswer` (`processor/graph-query/graphrag.go:2107`)
by default, regardless of the GraphQL selection set — but for `globalSearch` the REQUEST controls it.
`GlobalSearchRequest.IncludeSummaries *bool` with `shouldIncludeSummaries()` defaulting to true only when nil
(`:181-186`) is exposed by the gateway as `includeSummaries: Boolean` on `globalSearch` and `searchGraph`
(`gateway/graph-gateway/component.go:1855`, `:1866`; mapped to `include_summaries` at `:1424-1425`). Community
enrichment and answer synthesis are one bundled call (`enrichGlobalResponseFromCommunities`, `:2147-2159`), and every
strategy's call to it is behind the flag: pathrag `:455`, temporal `:1245`, spatial `:1315`, entity lookup `:1041`,
semantic `:1164`, graphrag's non-auto-summarize branch `:939`, the text fallback `:1397` and `:1473`. With
`include_summaries:false` no community lookup, no enrichment and no synthesis run. The one exception is graphrag's
auto-summarize branch (`:861-862`, `:876-911`, synthesis at `:903`): `autoSummarize := threshold > 0 &&
len(entityIDs) > threshold`, `DefaultSummarizeThreshold = 50` (`:54`), per-request `summarizeThreshold` (`:170-178`;
non-positive disables). `localSearch` has no flag (`LocalSearchRequest`, `:126-130`: entity_id, query, level) and
synthesizes unconditionally (`:359`). None of the three e2e `globalSearch` probes sets the flag (`git grep -n
"includeSummaries\|include_summaries" -- test/e2e/scenarios/` → 0): `sendNLQuery` (`tiered_structural.go:1741-1748`),
`test-graphrag-global` (`tiered_statistical.go:262`), `awaitReadyGatewayGlobalSearch` (`http_gateway_readiness.go:105`).
Under `configs/semantic.json:42` the synthesizer is seminstruct; under statistical it is the
`TemplateAnswerSynthesizer` (`component.go:454`).

P3. The intent classifier is keyword-only in every tier: `configs/semantic.json:40-44` declares `answer_synthesis`,
`community_summary`, `anomaly_review` and no `query_classification`; `component.go:422-444` wires the LLM classifier
only when that capability resolves. Path intent is regex + entity extraction (`graph/query/classifier.go:106-113`).
So the routing fact the NL stages test is exercised identically in statistical; semantic adds only the synthesis
cost.

P4. Measured model cost on `ubuntu-latest` (4 vCPU), default semantic variant, 2026-09-29: one `globalSearch`
56.4 s, 55.4 s, 35.5 s, >60 s (`test-http-gateway`, red once in four; #1117 issuecomment-5894560665); NL probes 5 of 5
at the 10 s client deadline in every run (stage durations 30.0 s / 20.0 s); `localSearch` 10.013 s timeout in three
runs and one success at 8.0 s returning 7 entities with a community id (job-fail.log run 1:
`graphrag_local_entities_found:7 graphrag_local_latency_ms:8001`); `globalSearch` (graphrag stage) 10.001 s timeout
in all four.

P5. Statistical, the required variant (job 109478124375, run 36589370091, scenario 29.6 s): `nl_path_intent 3/3`
(52 ms), `nl_temporal_intent 2/2` (21 ms), `spatial_query_count:10`, `temporal_query_count:83`,
`zone_relationships_count:12`, `predicate_list_count:31`, `predicate_stats_entity_count:0`,
`predicate_stats_sample_count:0`, `communities_total:18`, `communities_non_singleton:15`,
`community_ground_truth_passed:0/3`, `anomalies_total:0`, `anomaly_ground_truth_expected:1 found:0`,
`rules_firings_count:2`, `actions_dispatched:5`, `graphql_gateway_latency_ms:1`.

P6. The anomaly engine is disabled by configuration in every tier: `enable_anomaly_detection: false` at
`configs/statistical.json:776`, `configs/semantic.json:748`, `configs/semantic-8b.json:789`,
`configs/semantic-frontier.json:798` (`git grep -n enable_anomaly_detection -- ':!docs/'`), since `a785c81b`
(#237, ADR-054 Move 1: "stop the graph-clustering anomaly storm"). `ANOMALY_INDEX` is therefore never written;
`GetAnomalyCounts` returns zero counts without error when the bucket is absent (`test/e2e/client/nats.go`, the
`js.KeyValue` miss branch). `anomalies_total:0` in all seven measured runs is the configured outcome.

P7. The two ground-truth matchers are case-insensitive substring matches on the instance suffix
(`test/e2e/scenarios/anomaly/validator.go:126-131`, `community/validator.go:149-156`); the measured violations quote
live minted IDs (`c360.semstreams-kitchen-sink-ml-24325c.document.sensor.temperature.sensor-temp-001`). The
`c360.logistics` retirement (#1166) does not stale either table: the literal survives only in their `_test.go`
fixtures (`git grep -c c360.logistics -- test/e2e/` → `anomaly/validator_test.go:6`, `community/validator_test.go:2`).
The anomaly expectation (`anomaly/types.go:97-102`: `doc-emergency-001` as `core_isolation`) is stale against the
disabled engine (P6), not the corpus.

P8. Community ground truth is graded against LPA, whose partition varies across identical code: 1/3, 0/3, 1/3 in the
three semantic runs, 0/3 statistical (P4, P5). LPA is deterministic for a fixed entity set (`graph/clustering/lpa.go:33`,
`:223`), but every run mints a fresh authority suffix (`…-24325c`, `…-6d3f79`, `…-8f9da6`), so ID-ordered tie-breaks
differ per run. ADR-099 (accepted 2026-08-23, #606, beta.164) records the arm as "could not exceed 1 of 3 and was
warn-only" and replaces detection with prefix-derived communities, under which all three expectations
(`community/types.go:80-99`) become pure functions of the ID prefix.

P9. `test-predicate-stats` reports zero in every measured run (P4, P5) because the stage decodes the gateway's
advertised camelCase (`tiered_structural.go` `predicateStatsResponse`: `entityCount`, `sampleEntities`;
`predicateListResponse`: `entityCount`) while the handler answers snake_case (`graph/query_predicate_types.go:30-31`
`entity_count`, `sample_entities`) and the gateway passes bodies through byte-for-byte
(`openspec/specs/gateway-response-projection/spec.md:73`; no response re-keying: `grep -n -i "camel\|snake"
gateway/graph-gateway/component.go` → only variable handling at `:1349`, `:1356`). The gateway's introspection
declares `entityCount`/`sampleEntities` (`gateway/graph-gateway/component.go:1883`, `:1885`) while the neighbouring
typeDefs are snake (`:1880-1881`). Cross-check: `compoundPredicateQuery` decodes single-word keys identically on both
sides and measures `predicate_compound_and_matched:5`. Not observed directly: the wire body itself (§ 9).

P10. `validate-rule-transitions` (`tiered_structural.go:232-278`, structural-only row `tiered.go:347`) is a second
spelling of `validate-rules` (`validate_infra.go:464`, all-tier row `tiered.go:398`): both read the same Prometheus
rule metrics; `validate-rules` writes `rules_firings_count`/`actions_dispatched` (`:572-577`), hard-fails on
missing metrics and zero evaluations (`:491-494`), and only warns on `Firings < 1` (`:581-582`). The
`MinRuleFirings:2`/`MinActionsDispatched:1` thresholds (`tiered.go:142-143`) are asserted nowhere. Readers of the
transitions-only keys: none outside the dead `stages/` package (`git grep '"rule_firings"'` → `stages/rules.go:104`,
#1152).

P11. The Go 1.26 client deadline error is `*http.timeoutError` (`net/http/client.go:737`,
`transport.go:2768-2775`): its text ends "(Client.Timeout exceeded while awaiting headers)", `Timeout()` is true,
and `Is(context.DeadlineExceeded)` is true. No site in the four core files branches on it (inventory search → 0).

P12. `globalSearchClientTimeout(60 * time.Second)` already exists as the overridable deadline for synthesis-bound
probes (`http_gateway_readiness.go:149`, defined once, `SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT`; `:8b` sets 300 s,
`:frontier` 180 s, `taskfiles/e2e/semantic.yml:45`, `:63`); none of the fourteen paths use it (inventory).

P13. `s.natsClient` is set at Setup (`tiered.go:219`) and cleared only at Teardown (`:594`); the graph-roundtrip stage
already errors on nil (`:421-424`). The "NATS client not available" warn arms are unreachable in a normal run.

P14. Per probe, with `include_summaries:false` (the coordinator's question 3): the NL path probes resolve to strategy
`pathrag` (`graph/query/classifier.go:106-113` → `resolveStrategy`, `graphrag.go:664-665`) and return at `:469` after
`:455` skips enrichment — no community lookup, no synthesis. The NL temporal probes ("last hour", "today") carry a
`time_range` (`classifier.go:171-173`), resolve to `temporal` and return after `:1245` — no synthesis while the temporal
index answers (measured 81-84 entities); only a missing range or an unavailable route falls back to graphrag (`:1199`,
`:1206`), where `:903` synthesizes regardless of the flag if hits exceed 50. `test-graphrag-global` ("logistics
warehouse operations", no signal) resolves to `graphrag`: with the flag off `:939` is skipped and `:903` runs only if
semantic hits exceed 50 — never measured (the probe never completed). But the flag also empties `community_summaries`
and `answer`, which are exactly what `validateGraphRAGGlobalResult` asserts (`tiered_statistical.go:353-375`): for that
stage the flag does not remove the model cost from the outcome, it removes the outcome. `test-graphrag-local` has no
flag (P2). `test-http-gateway`'s probe (`http_gateway_readiness.go:105-118`) selects `entities`/`count`/`strategy`
only and measured `count:30` (< 50), so the flag alone removes its synthesis today; `summarizeThreshold: 0` beside it
makes that immune to corpus growth.

### § 1b Adopter seam (surfaces reached from outside the stage files)

Outward surfaces touched: the stage table (a component author adding a stage), the results JSON
(`BuildTieredResults`, `results.go:437-470`; readers `cmd/e2e --compare-structured` via `compare_tier_types.go`,
`docs/proposals/*` prior art, `test/e2e/docs/review/06-tiered-semantic.md`), the persisted community report
(`tiered.go:692-702`), and the Prometheus metrics dump. No sister repo reads e2e results (they are `test/e2e/results`
artifacts; sisters are read-only inventory and none was probed for this test-only surface).

1. What must a stage author know? (a) a warning never fails the tier (P1); (b) a recorder is declared in the
   stage-table comment, the convention B0/B2 already use (`tiered.go:311-330`); (c) which variants are per-PR (§ 0).
   Three items: a design finding, recorded as the spec delta (§ 7) rather than a mechanism.
2. If they do nothing: a new `Warnings = append … return nil` stage is green forever — exactly this issue's class.
3. Where they find out: doc (the new spec + table comments), then #1222's evidence pattern once it lands (a stage
   that records no assertion becomes visible). Doc level for a correctness fact is a finding; the structural fix is
   #1222's, not a parallel one (owner, 2026-09-27).
4. Should have to know: nothing. The gap is #1222's; this change narrows it to "declare a recorder or assert".

Key changes visible to results readers: none renamed or re-typed. Deleting `validate-anomaly-detection` (§ 2 #5)
removes the `anomalies_*`/`anomaly_*` metrics and `anomaly_list`/`anomaly_ground_truth` details; `buildAnomalyResults`
(`results.go:378-381`) already emits no `anomalies` object when the totals are zero, which they have been since #237
(P6; both 2026-09-29 results JSON have no `anomalies` key), so the JSON shape a reader sees is unchanged. Deleting
`validate-rule-transitions` (§ 2 #9) removes `rule_firings`, `rule_evaluations`, `rule_transitions_validation`
(no readers, P10); `actions_dispatched` continues to be written by `validate-rules`.

Prefer observation to prediction: the 10 s literal asks a probe to predict the synthesizer's latency. For the NL probes
the framework already offers the knob that removes the prediction (`includeSummaries: false`, P2) and the probes simply
never used it; for `localSearch` no such knob exists (F2); for `test-graphrag-global` the probe's own assertions are the
summaries, so the deadline is the helper's (P12) and the variant is the lever.

## § 2 Per-stage decisions

Legend: **A** assert (return an error on the detected outcome); **B** leave the per-PR variants' stage table, reason
in the table comment; **A-by-variant** assert where framework-owned, leave where model-owned, expressed by the
existing `variants` column; **RECORDER** the B0/B2 convention (declared in the table comment; fails only on
transport/read failure). "Predicted" is the measured outcome on today's main if the assertion lands as designed.

| # | Stage (row) | Decision | Site(s) that change | Predicted: statistical / semantic path-only |
|---|---|---|---|---|
| 1 | `test-nl-path-intent` (`tiered.go:334`, all tiers) | A (all variants) | `sendNLQuery` sends `includeSummaries: false` (`tiered_structural.go:1741-1752`, one variable); `:1881-1885` warn → error; variants unchanged | GREEN (3/3) / predicted ms (P14) |
| 2 | `test-nl-temporal-intent` (`:335`, stat+sem) | A (all variants) | shares `sendNLQuery`; `:1974-1977` warn → error; variants unchanged | GREEN (2/2) / predicted ms (P14) |
| 3 | `test-graphrag-local` (`:377`, semantic) | A-by-variant | variants `{"semantic"}` → `{"statistical"}`; `tiered_statistical.go:76`, `:88`, `:229-231` warn → error; `:166` literal stays (template synthesizer answers in ms) | predicted GREEN in statistical (unmeasured there, § 9) / leaves semantic until F2 |
| 4 | `test-graphrag-global` (`:378`, semantic) | A-by-variant | variants `{"semantic"}` → `{"statistical"}`; `:250`, `:366-368` warn → error; new zero-communities error beside `:353`; `:283` literal stays; `:353-355` (<2) and `:373-375` (member_count) stay warnings, reason in comment | predicted GREEN in statistical (its sibling `test-http-gateway` serves `globalSearch` with summaries in 1 ms there) / leaves semantic |
| 5 | `validate-anomaly-detection` (`:390`, stat+sem) | B | delete row `:390`; one-line comment: engine config-disabled in every tier since #237; D2 for the leg's code | n/a (would be RED in both: expected 1 found 0) |
| 6 | `validate-community-structure` (`:360`, stat+sem) | A (path arms) + RECORDER (ground-truth arm) | `tiered_statistical.go:385`, `:392` warn → error; `:479-486` stays, declared RECORDER in the row comment and the function comment; `:474` hard-fail unchanged | GREEN / GREEN (18 communities, 15 non-singleton) |
| 7 | `validate-virtual-edges` (`:391`, semantic) | A (mismatch arm) | `tiered_semantic.go:743`, `:772-774` warn → error; `:762-764` print stays (optionally reworded to name the disabled engine); `:737` hard-fail unchanged | not run / GREEN (0/0) |
| 8 | `validate-llm-enhancement` (`:310`, semantic) | RECORDER + A (transport arms) | `tiered_semantic.go:219`, `:472`, `:481`, `:486` warn → error; `:529-532`, `:540-550` stay, row comment gains "RECORDER of enhancement throughput; only an unreachable transport or a failed read fails" (mirrors B0's `:321-322`); `:504` degrade stays (its own comment) | not run / GREEN (recorder; wait skipped by #1117's flag → pending=18 recorded) |
| 9 | `validate-rule-transitions` (`:347`, structural) | B (duplicate spelling) | delete row `:347` and `tiered_structural.go:232-278`; D3 decides whether `validate-rules` asserts the thresholds | n/a (structural is not per-PR); `validate-rules` GREEN if D3(b): firings 2-3 ≥ 2, actions 5-7 ≥ 1 |
| 10 | `test-spatial-query` (`:297`, all) | A | `tiered_structural.go:1119-1121` warn → error | GREEN (10) / GREEN (10) |
| 11 | `test-temporal-query` (`:298`, all) | A | `:1227-1229` warn → error | GREEN (83) / GREEN (81-84) |
| 12 | `test-zone-relationships` (`:302`, all) | A | `:1486-1488` warn → error | GREEN (12) / GREEN (10-12) |
| 13 | `test-predicate-list` (`:339`, all) | A | `:2101-2104` warn → error; decoder tag `entityCount` → `entity_count` (same class as #14) | GREEN (31) / GREEN (31) |
| 14 | `test-predicate-stats` (`:340`, all) | A + decoder fix | `:2127-2130`, `:2135-2137` warn → error; new `entityCount == 0` error after the metrics writes; `predicateStatsResponse` tags → `entity_count`, `sample_entities` | RED on main without the decoder fix (0 in all six runs); GREEN with it (D4) |
| 15 | `validate-virtual-edges` zero-edges `fmt.Println` (`tiered_semantic.go:764`) | folded into #7 | the print is the legitimately-zero note; the configured cause (P6) goes in the row comment | — |

Tally: A = 8 (#1, #2, #7, #10, #11, #12, #13, #14); B = 2 (#5, #9); A-by-variant = 2 (#3, #4);
A-with-a-RECORDER-arm = 2 (#6, #8). No new field, flag, type, helper, or mechanism: every change is a `return
fmt.Errorf(...)` in place of a `Warnings = append`, one existing request argument, a `variants` literal, a row
deletion, a JSON tag, or a comment.

### Per-stage detail (the four questions)

**#1 / #2 NL intent.** Outcome detected: no probe returned entities (threshold stays 0, the measured failure; see
R5). Two edits. First, `sendNLQuery` adds one variable — `"includeSummaries": false` — to the request it already
builds (`:1741-1752`): the stage's assertions read `entities` only (`:1848-1866`, `:1937-1956`), it never reads
`communitySummaries`, and with the flag off neither NL strategy reaches synthesis (P14). Second, `:1881-1885` /
`:1974-1977` become
`return fmt.Errorf("NL path intent: 0/%d probes returned entities; first failure: %s", len(testCases), firstFailure)`
where `firstFailure` is the recorded `error` or "returned 0 entities" of the first failed probe (the loop already
records both, `:1829-1868`). The `variants` column is unchanged: the routing fact is framework-owned in every variant
(keyword classifier, P3) and the probe no longer pays the model, so the stages assert in structural, statistical, the
per-PR semantic run and the `:8b`/`:frontier` overlays alike — no variant surgery. Timeout vs empty: the wrapped text
distinguishes them (P11); no `errors.Is` branch (R1). Crux of the measured 0/5 under semantic: a synthesis the probe
never needed, requested by default (P2), at 35-60 s behind a 10 s client — not a routing break (the same probes pass
5/5 in ms without the model, P5). Predicted under semantic with the flag: ms, by construction of P14; unmeasured
(§ 9). Optional, not in the smallest shape: with summaries off the probe could also select and assert `strategy ==
"pathrag"`/`"temporal"` — the real routing assertion — recorded as a residual (F7), not added. Mutation check
(statistical, unchanged): inject `expectResults: false` on every probe (the live gateway returns entities, so every
probe fails and `passedCount` is 0) — warn path restored → tier green with the warning in `metadata.warnings`; fix →
`[n/N] test-nl-path-intent FAILED`.

**#3 / #4 GraphRAG local/global.** Outcome detected: the search request failed, or returned no entities (local) / no
community summaries (global). The two stages exist for the community path — local: entity → community → summary →
answer (`tiered_statistical.go:69`, `:198-235`); global: search → communities → summaries → answer (`:238`,
`:315-380`) — and that path is fully exercised under the statistical config with the template synthesizer
(`component.go:454`): communities exist (18/15 measured), summaries resolve to the statistical floor, `answer` is the
template. The model changes only the answer text, which no assertion here reads beyond `!= ""`. So both rows move to
`[]string{"statistical"}` and leave semantic: the plumbing is asserted per-PR in ms, and the model side stays with the
pre-tag recorders (B0) and `validate-globalsearch-known-answer`. Why not the flag for global: `includeSummaries: false`
empties the summaries the stage asserts (P14) — it would blind the stage, not de-cost it (R9). Why local cannot stay in
semantic: no flag exists on `LocalSearchRequest` and `:359` synthesizes unconditionally (P2) — until F2 lands the stage
is synthesis-bound wherever the synthesizer is a model. Smallest shapes: `:88` → `return fmt.Errorf("GraphRAG local
search failed: %w", err)`; `:76` → error (communities exist but none has a usable member: a framework fact);
`:229-231` → error; `:250` → error; a new `if communityCount == 0 { return fmt.Errorf(...) }` beside `:353` (returned
nothing); `:366-368` → error, an invariant: `synthesizeQueryAnswer` always returns the template floor
(`graphrag.go:2107-2125`), so an empty `answer` with summaries present is a framework defect, never a model outcome.
`:353-355` (`< 2` communities for a broad query) and `:373-375` (`member_count == 0`) stay warnings: the first is a
retrieval-quality threshold, the second unmeasured — both reasons in the code comment. The 10 s literals at `:166` and
`:283` stay: under the template synthesizer the sibling `test-http-gateway` serves `globalSearch` with summaries in
1 ms (P5), so there is no measurement asking for a change (proposal boundary). Timeout vs empty: `%w` carries the
transport text (P11). Predicted: GREEN in statistical for global by the sibling's measurement; GREEN for local
predicted but unmeasured — `localSearch` has never run under the statistical config (§ 9); the per-PR semantic run no
longer carries either stage, so #1425 has nothing to place for them (D1 becomes the coverage question). Cost recorded:
the `:8b`/`:frontier` overlays lose the two probes; `globalSearch` under the model remains covered there by B0 and
`validate-globalsearch-known-answer`; `localSearch` under the model is covered by nothing pre-tag until F2 (D1).
Mutation check (statistical, unchanged): inject the request failure — point `gatewayURL` at a closed port for the one
stage — warn path restored → green with "GraphRAG … failed" in warnings; fix → stage FAILED.

**#5 anomaly detection.** Every arm (`:576`, `:585`, `:621`, `:629`, `:635-641`) warns over an engine the tier
configuration disables (P6), and the ground truth expects a detection from it (P7). Asserting any arm is RED in both
per-PR variants by construction (expected 1, found 0, total 0 in all seven runs). Neither fixture nor runner: the
stage grades a feature that is off. B: delete the row with a one-line comment ("anomaly engine is
`enable_anomaly_detection: false` in every tier config since #237; re-enable → re-add and assert"). The function,
the `anomaly` validator package, the `results.go:378-431` readers and `cmd/e2e/compare_tier_types.go` become
unreferenced — D2 decides delete-now versus keep-for-re-enable (#618 beta.164 and #620 beta.165 both touch the
engine; a grep for callers answers wired, not wanted). Mutation check: none needed for a deletion; the tier-level
check is `grep -c '"validate-anomaly-detection"' tiered.go` → 0 and the semantic stage count drops from 48 to 47
(then 46 with #9 on structural only — n/a).

**#6 community structure.** Path arms → A: `:392` (no communities in 90 s: clustering produced nothing, the stage's
own `:474` hard-fail is unreachable through this arm today, so the stage can pass on "no communities at all") and
`:385` (nil client, unreachable per P13, made consistent with `:421-424`). Ground-truth arm `:479-486` → RECORDER, with
the row comment at `tiered.go:360` and the function comment naming why: P8 (varies 1/3 ↔ 0/3 across identical code;
ADR-099 records it as warn-only; #606 makes it deterministic). Asserting it today arms a flake (R3). The all-singletons
hard-fail at `:474` stays: it is the path assertion the stage exists for. Predicted: GREEN in both. Mutation check:
inject `maxWait` to `1 * time.Millisecond` in `waitForCommunities` for the one stage — warn path restored → green with
"Failed to get communities" in warnings; fix → FAILED. Residual on #606: promote the arm when communities are derived
(§ 8 F3).

**#7 virtual edges.** `:737` already hard-fails the count query (ADR-065). `:772-774` (auto-applied anomaly with no
materialized edge) is the framework plumbing outcome → error; `:743` (auto-applied count read failed) → error, the
same class as `:737`. The zero-edges print at `:762-764` stays a print: with the engine off (P6) zero is the configured
outcome, so the row comment at `tiered.go:391` records that and the print may be reworded to say so (optional, not
required). Predicted: GREEN (0/0). Mutation check: the mismatch arm cannot be injected without an anomaly, and
`s.natsClient` is a concrete type, so this assertion is inspection-only — recorded as such on the PR rather than
claimed.

**#8 LLM enhancement.** The enhancement arms (`:540-550`: enhanced 0 with failed/pending/none) and the summary
quality arm (`:529-532`) are the small model's throughput and output (P4: 5-6/18 enhanced, 8 pending at the 120 s
ceiling, 3-5 failed; the `:8b`/`:frontier` runs raise the wait to 10 m/5 m and are where the owner reads the report).
Under #1117's path-only flag the wait is skipped, so `pending` is 18 and the `:546` arm fires on every per-PR run — as
an error it would red the path-only job on a model fact, which is why it stays a RECORDER arm (declared, like B0's row
`tiered.go:321-322`). Framework arms → A: `:219` (the wait's KV read failed or the stage context ended), `:481` (no
communities in 90 s), `:486` (zero communities where clustering ran), `:472` (nil client, P13). `:504` keeps its
documented degrade (its comment already states the reason). Predicted: GREEN in the full semantic run and the
path-only run. Mutation check: same `waitForCommunities` injection as #6.

**#9 rule transitions.** A second interpreter of one fact (P10) — the contract's rule is consolidate, never extend.
B: delete the row and the function. The thresholds it alone carried (`MinRuleFirings:2`, `MinActionsDispatched:1`)
are asserted nowhere afterwards, and `validate-rules` itself warns on `Firings < 1` (`:581-582`): D3 asks whether the
one home asserts them (measured 2-3 firings, 5-7 actions → green). Mutation check: n/a for a deletion; if D3(b), inject
`MinRuleFirings: 100` → `validate-rules` FAILED; restored → green.

**#10-#13 spatial / temporal / zone / predicate list.** One-line flips: `:1119-1121`, `:1227-1229`, `:1486-1488`,
`:2101-2104` → `return fmt.Errorf(...)` with the existing message. Timeout vs empty: each already hard-fails the
transport error above the arm (`:1063`, `:1166`, `:1424`, `:2058`: `return fmt.Errorf("… request failed: %w")`),
so the new error is by construction the empty case. Predicted GREEN in both (P4, P5). Mutation checks: spatial →
bounding box on the wrong hemisphere (`north:-37.77 …`); temporal → a fixed 2020 range; zone → mint a nonexistent zone
(`"zone.facility.area.does-not-exist"`); predicate list → `"namespace": "no.such"` argument if the query accepts one,
else inspection-only, stated as such. Each: warn path restored → green with the warning; fix → FAILED.

**#14 predicate stats.** Three arms: `:2127-2130` (list request failed) → error; `:2135-2137` (no predicates) →
error; and the outcome the stage exists to detect — stats for a listed predicate — needs the assertion the stage
never had: after `result.Metrics["predicate_stats_entity_count"] = entityCount`, `if entityCount == 0 { return
fmt.Errorf("predicateStats(%q) reported 0 entities for a listed predicate", targetPredicate) }`. By construction the
list emits a predicate only when at least one membership key parsed (`processor/graph-index/query.go`
`predicateSummariesFromMembershipKeys`), and stats reads the same keys through the forward filter
(`predicate_index.go:20-31`, `queryPredicateEntityIDs`), so a listed predicate has count ≥ 1 and no fixture pick is
needed. Today that assertion is RED in every variant (P9): the stage decodes `entityCount` and the body carries
`entity_count`. Crux: a real product finding — the gateway's introspection advertises camelCase for two predicate
types against a byte-for-byte snake body — plus a stage decoder written to the advertised spelling. The stage fix
(tags → `entity_count`, `sample_entities`; list tag → `entity_count`) is test-only and in scope; the gateway
typeDef mismatch is filed (§ 8 F1); D4 asks which lands first. Mutation check: inject `targetPredicate := "no.such.predicate"`
(the exact filter validates and matches nothing) — warn path restored (no assertion) → green; fix → FAILED. The
decoder fix has its own fails-without-fix: revert the tag → `predicate_stats_entity_count` reads 0 → FAILED.

## § 3 Timeout versus empty — the general answer

Nothing beyond wrapping. Every probe already returns the transport error to its stage, and the client deadline error
names itself (P11); every "returned nothing" arm has its own message. Returning `fmt.Errorf("…: %w", err)` for the
former and the existing sentence for the latter makes the two outcomes distinct in the stage's FAILED line, the
`result.Error`, and the exit log — which is what the acceptance asks. An `errors.Is(err, context.DeadlineExceeded)` or
`os.IsTimeout` branch would be one new arm per site in files that have none today, buying a classification the text
already carries (R1). Doc sentence, for `docs/contributing/02-e2e-tests.md` § Assertion Strategy: "A probe that hits
its client deadline fails with the transport error verbatim; an empty result fails with the stage's own sentence; the
two never share a message."

## § 4 Options considered

- **Do nothing / narrow to the eight**: rejected by the owner (fix the class).
- **A everywhere with the 10 s literal**: NL intent and GraphRAG go RED on every semantic CI run on a model fact —
  rejected by the #1117 ruling's path/quality line.
- **A everywhere with a raised literal** (60 s or 120 s for the NL probes): 5 probes × up to 120 s per PR to pay for
  a field no assertion reads; the only measurement above 60 s is the one red — raised blind (R2).
- **The request flag for the NL probes** (#1, #2): `includeSummaries: false` on the one request builder both stages
  share; removes the model from a probe that never read its output, in every variant, with no table edit (P14);
  chosen.
- **The request flag for `test-graphrag-global`**: removes the model and the summaries the stage asserts (P14) —
  rejected (R9).
- **A-by-variant via the `variants` column** (#3, #4): the community path is exercised in full under the template
  synthesizer; the stages move to statistical and leave semantic; chosen. Its cost is pre-tag coverage of
  `localSearch` under the model (D1).
- **A-by-variant for the NL stages** (the first draft): moved the stages out of semantic to dodge a cost the flag
  removes; superseded, and it lost the `:8b`/`:frontier` overlays for nothing.
- **B for the whole stage** where only one arm is model-owned (#6, #8): loses the stages' real hard-fails
  (`:474`, `:737`-class) — rejected (R6); the arm becomes an explicit recorder instead.
- **A new `recorder bool` on the stage struct**: a mechanism the table-comment convention already covers (R7).
- **An assert-if-enabled check reading the tier config for the anomaly leg**: a new interpreter of config in the
  scenario, to grade a disabled engine — rejected (R4); the row leaves.

Rejected alternatives, with the reason:
- R1 `errors.Is`/`os.IsTimeout` classification arms — text already distinguishes; adds a branch per site.
- R2 raising the NL probes' deadline — no measurement above 60 s except a red; the fact is proven without the model.
- R3 asserting community ground truth today — 1/3 ↔ 0/3 on identical code (P8) arms a flake; ADR-099 already records it.
- R4 config-conditional assertion for anomaly — grades a disabled engine either way.
- R5 all-NL-probes-must-pass — ratchets the gate above the measured failure (0/5); a partial pass is a routing-quality
  question, not the outcome the stage exists to detect. Docket only if the owner wants it; not recommended.
- R6 removing `validate-community-structure` / `validate-virtual-edges` wholesale — loses real path hard-fails.
- R7 a `recorder` field or a warnings-fail-the-tier switch — mechanism where a sentence and a comment suffice.
- R8 fixing the gateway introspection in this change — product code, its own review; filed instead (F1).
- R9 `includeSummaries: false` on `test-graphrag-global` — its assertions are the summaries and the answer
  (`tiered_statistical.go:353-375`); the flag empties both, so the stage would pass on a request that proves nothing
  about the community path.
- R10 keeping `test-graphrag-local` in semantic behind the helper deadline — no request knob exists for `localSearch`
  (P2), so the per-PR semantic run would carry a 35-60 s model call behind a 60 s client, the boundary flake the merge
  gate forbids; leaving the variant until F2 costs less than the flake.

## § 5 Ordering with PR #1425 (#1117) and #1222

1. This change lands first (owner). It edits `tiered.go` rows `:377`, `:378` (variants), deletes `:347`, `:390`, and
   edits comments at `:310`, `:334-335`, `:360`, `:391`; PR #1425 rebases over that.
2. After the rebase, #1425's path-only flag still skips exactly the ruling's three stages. Nothing new is
   synthesis-bound in the per-PR semantic run: the NL probes send `includeSummaries: false` and answer in ms (P14);
   `test-graphrag-local`/`-global` no longer run in semantic (§ 2 #3/#4). #1425's own `test-http-gateway` finding
   (issuecomment-5894560665: 35-60 s, red 1-in-4 at 60 s) has the same one-argument fix on its own probe:
   `gatewayGlobalSearchQuery()` (`http_gateway_readiness.go:105-118`) selects `entities`/`count`/`strategy` only, so
   `includeSummaries: false` (plus `summarizeThreshold: 0` to stay under the auto-summarize branch as the corpus grows,
   P14) keeps it a path probe at the 1 ms it measures under statistical — no non-synthesizing query hunt and no
   measured-budget question. That edit belongs to #1425 (its file, its finding); this change does not touch
   `http_gateway_readiness.go`.
3. `validate-llm-enhancement` is a declared recorder after this change; #1425's wait-skip leaves it green.
4. #1222 (Codex, `codex/gh1222-required-e2e-proof`) edits `cmd/e2e/main.go` and the e2e docs; this change edits only
   `test/e2e/scenarios/tiered*.go` and the new spec — disjoint. `AssertionsRun` is not touched; the recorder/assert
   distinction here is what #1222's count will later make visible.
5. Greenness at landing: the per-PR variant is statistical only until #1425 lands, so task 2.2's gate is `task
   e2e:statistical` green with every assertion in place — § 2 predicts green for all given D4(a), with `test-graphrag-local`
   under statistical the one unmeasured stage (§ 9); the semantic path-only greenness is #1425's gate to prove on its
   rebased head, with no known red inherited from this change.

## § 6 Docket (real owner questions only; doc-sentence alternative first)

| # | Question | (a) sentence / smallest | (b) alternative | Recommendation |
|---|---|---|---|---|
| D1 | `test-graphrag-local`/`-global` move to statistical and leave semantic (incl. `:8b`/`:frontier`); `localSearch` under the model is then covered by nothing pre-tag until `LocalSearchRequest` gains `include_summaries` (F2) | Record the gap in the `tiered.go:377-378` comment and on F2; accept it | Keep `test-graphrag-local` in semantic too, behind the helper deadline, and have #1425 skip it per-PR (three quality skips become four) — the R10 boundary in every non-skipped run | (a) |
| D2 | After row `:390` leaves, the anomaly leg's code (`executeValidateAnomalyDetection`, `validateAnomalyGroundTruth`, `test/e2e/scenarios/anomaly/`, `results.go:378-431`, `compare_tier_types.go`) is unreferenced | One comment at the row; keep the code until #618/#620 rule on the engine | Delete the leg now (greenfield: no dead code) — larger diff, touches results readers | (a); (b) if the owner reads #620 as already deciding |
| D3 | `validate-rule-transitions` is a duplicate spelling and leaves; its thresholds (`MinRuleFirings:2`, `MinActionsDispatched:1`) are then asserted nowhere, and `validate-rules` warns on zero firings (`validate_infra.go:581`) | Delete the duplicate; one sentence in the `validate-rules` row comment that firings ≥ min is recorded, not asserted | Also assert the two thresholds in `validate-rules` (measured 2-3 / 5-7 → green) — a fifteenth stage touched | (b), because it is the one home; (a) if scope must stay at fourteen |
| D4 | `test-predicate-stats` asserts RED on main until its decoder reads `entity_count` (P9) | Fix the two decoder tags in this change (test-only) and file the gateway introspection mismatch (F1) | Land the assertion red-and-filed until the gateway typeDefs are corrected | (a) |

## § 7 Spec delta text for task 1.3

Path: `openspec/changes/e2e-tiered-warn-only-stages/specs/e2e-tiered-scenario/spec.md` (new capability; no
`openspec/specs/` entry mentions the tiered scenario — inventory § Adjacent claims; naming follows the kebab capability
names, closest precedent `test-cleanup-policy`).

```markdown
# e2e-tiered-scenario Specification

## Purpose

The tiered e2e scenario (`test/e2e/scenarios/tiered*.go`, `cmd/e2e --scenario tiered --variant <structural|statistical|semantic>`)
is the framework's own end-to-end gate over the ingest → entity → graph store → query path. It exists so that a green
per-PR tier is evidence: a stage that runs in a per-PR variant either asserts the outcome it exists to detect or is
declared a recorder, and a probe's deadline is never mistaken for an empty result. Which variants are per-PR is the
E2E Ladder's decision (`.github/workflows/e2e-ladder.yml`); which stages the semantic path-only run skips is #1117's.

## ADDED Requirements

### Requirement: A per-PR stage never passes on the outcome it exists to detect

Every stage the tiered scenario runs in a per-PR variant MUST return a non-nil error when the outcome the stage exists
to detect occurs, so that `Result.Success` is false and the e2e binary exits 1. A `Result.Warnings` entry SHALL NOT be
a per-PR stage's only record of that outcome. A stage whose detected outcome is owned by a model's latency or answer
quality, or by an engine the tier's configuration disables, SHALL leave the per-PR variant's stage table (by its
`variants` list or by its row) with the reason recorded in the stage-table comment, rather than warn.

Recorder exception: a stage, or one arm of a stage, declared RECORDER in its stage-table comment records its
measurement into `Result.Metrics`/`Result.Details` without gating, and MUST fail only on an unreachable transport or a
failed read. The declared recorders are B0 `validate-thematic-answer-eval`, B2 `validate-partition-colocation`,
`validate-llm-enhancement`'s enhancement-throughput and summary-quality arms, and `validate-community-structure`'s
ground-truth arm. A recorder SHALL NOT be added without the declaration.

#### Scenario: The detected outcome occurs in a per-PR variant
- **GIVEN** a stage in a per-PR variant whose probe returns nothing, or whose read reports the framework outcome the stage exists to detect
- **WHEN** the tiered scenario executes that stage
- **THEN** the stage returns a non-nil error, `Result.Success` is false, `Result.Error` names the stage, and the e2e binary exits 1.

#### Scenario: A declared recorder observes a poor measurement
- **GIVEN** a stage or arm declared RECORDER in the stage table
- **WHEN** its measurement is poor (a low enhancement count, a ground-truth violation)
- **THEN** the tier stays green, the measurement is present in `Result.Metrics` or `Result.Details`, and the violation is in `Result.Warnings`
- **AND** an unreachable transport or a failed read still fails the stage.

#### Scenario: A model-owned or config-disabled outcome is not graded per-PR
- **GIVEN** a stage whose detected outcome depends on the small model's latency or answer, or on an engine the tier config disables
- **WHEN** the per-PR variant's stage list is built
- **THEN** that stage is absent from it, and the stage-table comment records why.

### Requirement: A probe's deadline is reported distinctly from an empty result

When a stage's probe fails at its client deadline, the stage's error MUST carry the transport error verbatim; when the
probe returns an empty result, the error MUST be the stage's own sentence. The two outcomes SHALL never share a
message. A `globalSearch` probe whose assertions do not read `communitySummaries` or `answer` SHALL send
`includeSummaries: false`, so it never pays for synthesis it does not check. Probes that do assert on synthesized fields
SHALL take their client deadline from the shared `globalSearchClientTimeout` helper so a variant overlay can raise it
from a measurement.

#### Scenario: The probe hits its client deadline
- **GIVEN** a probe whose `http.Client` deadline expires before headers arrive
- **WHEN** the stage fails
- **THEN** the error contains "Client.Timeout exceeded" and does not read as an empty result.

#### Scenario: The probe returns nothing
- **GIVEN** a probe that returns HTTP 200 with zero entities
- **WHEN** the stage fails
- **THEN** the error states that the query returned no entities and carries no deadline text.
```

## § 8 Residuals

File (architecture, each would cost a judge round):
- F1 `gateway/graph-gateway/component.go:1883`, `:1885` — introspection declares `entityCount`/`sampleEntities` for
  `PredicateSummary`/`PredicateStatsResult` while the projected body is `entity_count`/`sample_entities`
  (`graph/query_predicate_types.go:30-31`; projection is byte-for-byte per `gateway-response-projection/spec.md:73`);
  the neighbouring typeDefs are snake. A client written to introspection reads zero silently. Milestone: owner's.
- F2 `processor/graph-query/graphrag.go:126-130`, `:359` — `LocalSearchRequest` has no `include_summaries` and
  `handleLocalSearch` synthesizes unconditionally, so no `localSearch` probe can be a path probe under a model
  synthesizer; `globalSearch` already has the knob (`:181-186`). Add the field, defaulting true like `globalSearch`'s.
  Small; unblocks returning `test-graphrag-local` to the semantic variant. Milestone: owner's.

Record (no issue):
- F3 On #606: once communities are prefix-derived, promote `validate-community-structure`'s ground-truth arm from
  RECORDER to an assertion — all three expectations become deterministic (P8).
- F4 `test-predicate-list`/`-stats`/`-compound` post through `http.DefaultClient` (no deadline; three `Do` sites in
  `tiered_structural.go`); bounded only by the scenario context. Not changed here (no measurement asks for it).
- F5 If D3(a): `validate-rules` records firings ≥ min without asserting it.
- F6 The dead `test/e2e/scenarios/stages/` package carries the only other reader of `rule_firings` (#1152).
- F7 With summaries off, the NL probes can select `strategy` and assert `pathrag`/`temporal` — the routing assertion
  the stages were named for; today they pass on any strategy that returns entities. An addition; not in this slice.

## § 9 Not determined

- The NL probes' latency under semantic with `includeSummaries: false` is predicted (P14: no synthesis on either
  strategy) and unmeasured; the first #1425 run after the rebase measures it.
- `test-graphrag-local` has never run under the statistical config; predicted green (communities 18/15, template
  synthesizer), unmeasured until task 2.2.
- The semantic hit count for "logistics warehouse operations" (whether it exceeds the auto-summarize threshold of 50)
  — moot for the chosen shape, load-bearing only for the rejected R9.
- Whether B0's abandoned synthesis requests (`tiered.go:311-322`) inflated the measured NL/GraphRAG latencies: moot
  for the NL probes once they stop synthesizing; the design does not rely on it.
- The wire body of `predicateStats` was not captured; P9 is inferred from the handler tags, the projection spec, the
  gateway's lack of re-keying, the single-word compound cross-check, and six identical zeros. The decoder-fix
  fails-without-fix (§ 2 #14) is the observation that closes it.
- `validate-rule-transitions`' behavior on today's structural tier — no structural run exists from today; the last
  structural results in the scratchpad are 2026-01-28 (rules `triggered_count:3`, from `validate-rules`).
- `graphrag_global` `member_count == 0` (`:373-375`): never observed on this runner; left a warning for that reason.

## § 10 Skills, invariants, tasks

- Decision skills: `kv-or-stream`, `orchestration-check`, `new-payload`, `query-pattern` — none triggers (no new
  communication path, orchestration, payload, or query access; test-only edits plus one spec).
- Invariants: the design carries no codec, grammar, revision, or state machine; the one invariant it asserts
  (`answer != ""` when summaries exist, § 2 #4) is a property of `synthesizeQueryAnswer`'s template floor
  (`graphrag.go:2107-2125`) and is exercised by the named example (the stage). Per the testing policy's PBT decision:
  named examples suffice; no property harness.
- Mutation-evidence protocol for task 2.1 (per assertion, on the PR): `cp` the stage file to the scratchpad and record
  `shasum -a 256` of both; apply the injection named in § 2 with the warning path restored → `task e2e:statistical`
  (or `task e2e:semantic` for #8) green, the warning present in the results JSON `metadata.warnings`; restore
  from the copy and re-verify the checksum; apply the fix with the same injection → the stage's `FAILED after` line;
  remove the injection → green. Never `stash`. The HTTP-seam stages (#1, #2, #10-#14) accept the cheaper equivalent
  of an `httptest.Server` returning the empty/timeout shape in a unit test, which is the production seam
  (`s.config.GraphQLURL`); the KV-seam arms (#6, #8) need the tier; #3/#4 now run under `task e2e:statistical`; #7's mismatch arm is inspection-only and is
  recorded as such.
- tasks.md 2.1 stays as written; its per-stage content is this § 2 table. 2.2's gate at landing is statistical only
  (§ 5 item 5).

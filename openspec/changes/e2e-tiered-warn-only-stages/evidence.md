# Evidence: e2e-tiered-warn-only-stages task 2.1 (issue #1426)

Branch `claude/gh1426-warn-only-stages`, base `6cd459bb`. Commits: `01882ff4` (structural query stages), `ce4d334d`
(NL intent), `eb4bdd34` (community path + recorder arms), `b74a0f63` (disabled-engine and duplicate stages leave;
validate-rules thresholds), `b8ebea30` (unit tests). Host: darwin, Docker Desktop, 2026-09-29. Every mutant was
made from a `cp` backup with its SHA-256 recorded and restored the same way; no stash, checkout, or restore was used.

## 0. Backups

Warn-path originals (base `6cd459bb`), copied before any edit:

```
40272db919ed2e68ea9e049ca6e073b78e22d41a0afa8e6791a1f2a3519adf5b  tiered.go
9cdc174000309295b7e6dc3f4d479bbc0edd19e859c15b52d26e458690852914  tiered_structural.go
af44bd33c8906b54ae1d8a2436506169bf47bb611109dda3419d2d4cb9029c34  tiered_statistical.go
a5edd5b61939c48521e41d5389f6e15990799d2459866a4ffbcbbb3a947435b3  tiered_semantic.go
5b6918e9c27cd1c1d9f735eae7f0c0f0b7daecf4a4d900eeaf951c63663b7904  validate_infra.go
```

Fixed files (as committed):

```
78e80f43a34c7ca54aec60a9344a25eeaea0fa4f56aa99ddd4a9b4a86970db4d  tiered.go
1d540f930e56330bbd1323f75e44b92bf1979528050d212a094b7f02a628c3cb  tiered_structural.go
726550e25aaf9496b558a427264a44f6984a2fcf03302c92b7c4829393b78a7a  tiered_statistical.go
c5553e469bae52f1510a47695635f8dd3a1103d8c5d966223ff054d0dacff166  tiered_semantic.go
d84ee6f4688cb7b03660b37b7318d8a5d8885e04b4cded7d02291cb67f7d214b  validate_infra.go
1e17a833f8fde8b5585be4f6b2e73a6ceb1dab943f328cbc9ab864c714d42bb2  tiered_warn_only_stages_test.go
```

## 1. Unit mutation checks at the production seam (HTTP GraphQL endpoint, metrics scrape)

`tiered_warn_only_stages_test.go` drives the real stage functions against an `httptest` server at
`s.config.GraphQLURL` (and a live Prometheus exposition for `validate-rules`). With the fix every test passes. Each
mutant restores one file's warn path from its backup; each subtest names the stage that went back to passing on the
outcome it exists to detect. Stages proven this way: #1, #2, #4, #10-#14 (design § 10 allows the HTTP-seam
equivalent), plus the unit-reachable arms of #3 (`:76` via no NATS client, `:229` no entities), #6/#8/#7 (no NATS
client), and D3.

```
===== MUTANT: warn path restored in tiered_structural.go
$ cp /private/tmp/claude-501/-Users-coby-Code-c360-semstreams/dabd74af-3488-43f0-8caf-b220ff9b5656/scratchpad/warn/tiered_structural.go test/e2e/scenarios/tiered_structural.go && shasum -a 256 test/e2e/scenarios/tiered_structural.go
9cdc174000309295b7e6dc3f4d479bbc0edd19e859c15b52d26e458690852914  test/e2e/scenarios/tiered_structural.go
$ go test -count=1 -run 'TestWarnOnlyStages|TestNLIntent|TestGraphRAG|TestNATSSeam|TestPredicate|TestValidateRules' ./test/e2e/scenarios/ | grep -E '^(--- FAIL|    --- FAIL|ok|FAIL)'
--- FAIL: TestWarnOnlyStages_EmptyAnswerFails (0.01s)
    --- FAIL: TestWarnOnlyStages_EmptyAnswerFails/test-spatial-query (0.00s)
    --- FAIL: TestWarnOnlyStages_EmptyAnswerFails/test-temporal-query (0.00s)
    --- FAIL: TestWarnOnlyStages_EmptyAnswerFails/test-zone-relationships (0.00s)
    --- FAIL: TestWarnOnlyStages_EmptyAnswerFails/test-predicate-list (0.00s)
    --- FAIL: TestWarnOnlyStages_EmptyAnswerFails/test-nl-path-intent (0.00s)
    --- FAIL: TestWarnOnlyStages_EmptyAnswerFails/test-nl-temporal-intent (0.00s)
--- FAIL: TestNLIntent_TransportFailureIsNotAnEmptyResult (0.00s)
--- FAIL: TestNLIntent_ProbesDeclineSummaries (0.00s)
--- FAIL: TestPredicateStats (0.00s)
    --- FAIL: TestPredicateStats/reads_the_handler's_snake_case_body (0.00s)
    --- FAIL: TestPredicateStats/zero_entities_for_a_listed_predicate_fails (0.00s)
    --- FAIL: TestPredicateStats/no_predicates_listed_fails (0.00s)
    --- FAIL: TestPredicateStats/list_request_failure_fails (0.00s)
--- FAIL: TestPredicateList_ReadsSnakeCaseEntityCount (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	0.377s
FAIL
$ cp /private/tmp/claude-501/-Users-coby-Code-c360-semstreams/dabd74af-3488-43f0-8caf-b220ff9b5656/scratchpad/fix/tiered_structural.go test/e2e/scenarios/tiered_structural.go && shasum -a 256 test/e2e/scenarios/tiered_structural.go
1d540f930e56330bbd1323f75e44b92bf1979528050d212a094b7f02a628c3cb  test/e2e/scenarios/tiered_structural.go
$ go test -count=1 -run 'TestWarnOnlyStages|TestNLIntent|TestGraphRAG|TestNATSSeam|TestPredicate|TestValidateRules' ./test/e2e/scenarios/
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.335s
===== MUTANT: warn path restored in tiered_statistical.go
$ cp /private/tmp/claude-501/-Users-coby-Code-c360-semstreams/dabd74af-3488-43f0-8caf-b220ff9b5656/scratchpad/warn/tiered_statistical.go test/e2e/scenarios/tiered_statistical.go && shasum -a 256 test/e2e/scenarios/tiered_statistical.go
af44bd33c8906b54ae1d8a2436506169bf47bb611109dda3419d2d4cb9029c34  test/e2e/scenarios/tiered_statistical.go
$ go test -count=1 -v -run 'TestWarnOnlyStages|TestNLIntent|TestGraphRAG|TestNATSSeam|TestPredicate|TestValidateRules' ./test/e2e/scenarios/ | grep -E -- '--- FAIL|^(ok|FAIL)'
--- FAIL: TestWarnOnlyStages_EmptyAnswerFails (0.01s)
    --- FAIL: TestWarnOnlyStages_EmptyAnswerFails/test-graphrag-global (0.00s)
--- FAIL: TestGraphRAGGlobal_EmptyAnswerBesideSummariesFails (0.00s)
--- FAIL: TestGraphRAG_RequestFailureFails (0.00s)
--- FAIL: TestGraphRAGLocal_NoEntitiesFails (0.00s)
--- FAIL: TestNATSSeamStages_NoClientFails (0.00s)
    --- FAIL: TestNATSSeamStages_NoClientFails/test-graphrag-local (0.00s)
    --- FAIL: TestNATSSeamStages_NoClientFails/validate-community-structure (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	0.334s
FAIL
$ cp /private/tmp/claude-501/-Users-coby-Code-c360-semstreams/dabd74af-3488-43f0-8caf-b220ff9b5656/scratchpad/fix/tiered_statistical.go test/e2e/scenarios/tiered_statistical.go && shasum -a 256 test/e2e/scenarios/tiered_statistical.go
726550e25aaf9496b558a427264a44f6984a2fcf03302c92b7c4829393b78a7a  test/e2e/scenarios/tiered_statistical.go
$ go test -count=1 -run 'TestWarnOnlyStages|TestNLIntent|TestGraphRAG|TestNATSSeam|TestPredicate|TestValidateRules' ./test/e2e/scenarios/
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.299s
===== MUTANT: warn path restored in tiered_semantic.go
$ cp /private/tmp/claude-501/-Users-coby-Code-c360-semstreams/dabd74af-3488-43f0-8caf-b220ff9b5656/scratchpad/warn/tiered_semantic.go test/e2e/scenarios/tiered_semantic.go && shasum -a 256 test/e2e/scenarios/tiered_semantic.go
a5edd5b61939c48521e41d5389f6e15990799d2459866a4ffbcbbb3a947435b3  test/e2e/scenarios/tiered_semantic.go
$ go test -count=1 -v -run 'TestWarnOnlyStages|TestNLIntent|TestGraphRAG|TestNATSSeam|TestPredicate|TestValidateRules' ./test/e2e/scenarios/ | grep -E -- '--- FAIL|^(ok|FAIL)'
--- FAIL: TestNATSSeamStages_NoClientFails (0.00s)
    --- FAIL: TestNATSSeamStages_NoClientFails/validate-llm-enhancement (0.00s)
    --- FAIL: TestNATSSeamStages_NoClientFails/validate-virtual-edges (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	0.323s
FAIL
$ cp /private/tmp/claude-501/-Users-coby-Code-c360-semstreams/dabd74af-3488-43f0-8caf-b220ff9b5656/scratchpad/fix/tiered_semantic.go test/e2e/scenarios/tiered_semantic.go && shasum -a 256 test/e2e/scenarios/tiered_semantic.go
c5553e469bae52f1510a47695635f8dd3a1103d8c5d966223ff054d0dacff166  test/e2e/scenarios/tiered_semantic.go
$ go test -count=1 -run 'TestWarnOnlyStages|TestNLIntent|TestGraphRAG|TestNATSSeam|TestPredicate|TestValidateRules' ./test/e2e/scenarios/
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.304s
===== MUTANT: warn path restored in validate_infra.go
$ cp /private/tmp/claude-501/-Users-coby-Code-c360-semstreams/dabd74af-3488-43f0-8caf-b220ff9b5656/scratchpad/warn/validate_infra.go test/e2e/scenarios/validate_infra.go && shasum -a 256 test/e2e/scenarios/validate_infra.go
5b6918e9c27cd1c1d9f735eae7f0c0f0b7daecf4a4d900eeaf951c63663b7904  test/e2e/scenarios/validate_infra.go
$ go test -count=1 -run 'TestWarnOnlyStages|TestNLIntent|TestGraphRAG|TestNATSSeam|TestPredicate|TestValidateRules' ./test/e2e/scenarios/ | grep -E '^(--- FAIL|    --- FAIL|ok|FAIL)'
--- FAIL: TestValidateRules_AssertsActivityThresholds (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	0.327s
FAIL
$ cp /private/tmp/claude-501/-Users-coby-Code-c360-semstreams/dabd74af-3488-43f0-8caf-b220ff9b5656/scratchpad/fix/validate_infra.go test/e2e/scenarios/validate_infra.go && shasum -a 256 test/e2e/scenarios/validate_infra.go
d84ee6f4688cb7b03660b37b7318d8a5d8885e04b4cded7d02291cb67f7d214b  test/e2e/scenarios/validate_infra.go
$ go test -count=1 -run 'TestWarnOnlyStages|TestNLIntent|TestGraphRAG|TestNATSSeam|TestPredicate|TestValidateRules' ./test/e2e/scenarios/
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.298s
```

(The structural and validate_infra mutants are from the first pass; the statistical and semantic mutants were re-run
after `TestNATSSeamStages_NoClientFails` was split into per-stage subtests, so each stage is named.)

Stage-table changes (A-by-variant, B) are proven by the table itself:

```
$ grep -c "\"validate-anomaly-detection\", s\." test/e2e/scenarios/tiered.go
0
$ git grep -n "executeValidateRuleTransitions" -- test/; echo "(exit $?)"
(exit 1)
$ grep -n "\"test-graphrag-\(local\|global\)\"" test/e2e/scenarios/tiered.go
393:		{"test-graphrag-local", s.executeTestGraphRAGLocal, []string{"statistical"}},
394:		{"test-graphrag-global", s.executeTestGraphRAGGlobal, []string{"statistical"}},
```

## 2. Tier runs (`task e2e:statistical`, `task e2e:semantic`)

Host check before the first run: `docker ps --format '{{.Names}}'` printed nothing; `pgrep -fl 'e2e'` exited 1.

### 2.1 Gate run at HEAD `b8ebea30` — RED, not predicted by the design

```
[1/42] verify-components completed in 2.356375ms
[2/42] send-mixed-data completed in 5.637292ms
[3/42] validate-processing completed in 10.1955ms
[4/42] wait-for-embeddings completed in 131.248583ms
[5/42] validate-embedding-queue-health completed in 5.592834ms
[6/42] wait-for-entity-stabilization completed in 11.278125ms
[7/42] graph-roundtrip completed in 36.165667ms
[8/42] validate-hierarchy-inference completed in 7.239125ms
[9/42] verify-entity-count completed in 5.512167ms
[10/42] verify-entity-retrieval completed in 4.318875ms
[11/42] validate-entity-structure completed in 4.343541ms
[12/42] verify-index-population completed in 39.241292ms
[13/42] test-pathrag-sensor completed in 4.413708ms
[14/42] test-pathrag-boundary completed in 1.656083ms
[15/42] test-pathrag-document completed in 1.680958ms
[16/42] test-entityid-hierarchy completed in 13.511333ms
[17/42] test-entities-by-prefix completed in 2.719125ms
[18/42] test-spatial-query completed in 1.227458ms
[19/42] test-temporal-query completed in 2.357375ms
[20/42] test-zone-relationships completed in 1.7235ms
[21/42] test-nl-path-intent completed in 41.874291ms
[22/42] test-nl-temporal-intent completed in 15.489208ms
[23/42] test-entity-by-alias completed in 2.028542ms
[24/42] test-predicate-list completed in 3.970583ms
[25/42] test-predicate-stats completed in 6.130209ms
[26/42] test-predicate-compound completed in 3.17775ms
[27/42] verify-search-quality completed in 6.538334ms
[28/42] test-http-gateway completed in 1.1585ms
[29/42] validate-gateway-response-shape completed in 50.919541ms
[30/42] test-embedding-fallback completed in 1.223542ms
[31/42] validate-community-structure completed in 24.858697s
[32/42] validate-retired-structural-bucket-absent completed in 663.208µs
[33/42] validate-authoritative-hierarchy-provenance completed in 53.982792ms
[34/42] validate-incoming-index-predicates completed in 4.885625ms
[35/42] validate-bidirectional-traversal completed in 4.62375ms
[36/42] validate-inverse-edges-materialized completed in 3.934875ms
[37/42] test-graphrag-local completed in 15.669583ms
[38/42] test-graphrag-global FAILED after 14.137291ms: GraphRAG global search returned no community summaries for query "logistics warehouse operations"
time=2026-09-29T14:36:38.274-05:00 level=ERROR msg="Scenario completed with failure" error="test-graphrag-global failed: GraphRAG global search returned no community summaries for query \"logistics warehouse operations\"" duration=25.572723541s assertions_run=0
```

Cause, measured against the live statistical stack (brought up with `scripts/e2e-statistical-up.sh`, scenario run to
load data, then probed with curl; torn down with `docker compose -f docker/compose/tiered.yml --profile statistical
down -v`):

```
$ (globalSearch "logistics warehouse operations", level 1, selecting count answer community_summaries{...})
top-level keys in globalSearch body: ['answer', 'community_summaries', 'count', 'duration_ms', 'entities', 'strategy']
community_summaries 5 first keys ['community_id', 'entities', 'keywords', 'level', 'member_count', 'relevance', 'summary'] first summary len 176 member_count 54
```

The handler answers `community_summaries` / `community_id` (`processor/graph-query/graphrag.go:195`, `:236`), the
gateway passes the body through regardless of the selection set (unselected `duration_ms`, `strategy` are present),
and the gateway's own typeDef is snake too (`gateway/graph-gateway/component.go:1876`). The stage decodes
`communitySummaries` / `communityId` (`tiered_statistical.go` `graphRAGGlobalResponse`), so it has read zero summaries
on every run it ever made. Same class as D4 (a camelCase test decoder over a snake body), in a stage D4 did not name.
Design § 2 #4 predicted GREEN from `test-http-gateway`, which never selects or decodes summaries.

### 2.2 Experiment, NOT committed: the two `graphRAGGlobalResponse` tags read `community_summaries` / `community_id`

Applied in the worktree (SHA `2838ff49…`), one run, then restored from the committed backup
(`726550e25aaf9496b558a427264a44f6984a2fcf03302c92b7c4829393b78a7a`). Changed-stage lines and completion:

```
[18/42] test-spatial-query completed in 1.112291ms
[19/42] test-temporal-query completed in 2.041292ms
[20/42] test-zone-relationships completed in 1.770209ms
[21/42] test-nl-path-intent completed in 38.267791ms
[22/42] test-nl-temporal-intent completed in 22.77625ms
[24/42] test-predicate-list completed in 3.805666ms
[25/42] test-predicate-stats completed in 4.679042ms
[31/42] validate-community-structure completed in 24.320374667s
[37/42] test-graphrag-local completed in 9.438583ms
[38/42] test-graphrag-global completed in 8.347541ms
[40/42] validate-rules completed in 47.365875ms
time=2026-09-29T14:40:12.613-05:00 level=INFO msg="Scenario completed successfully" duration=25.320550083s assertions_run=0
(metrics excerpt: predicate_stats_entity_count:58 predicate_stats_sample_count:5 graphrag_global_communities_found:5 graphrag_global_entities_found:6 graphrag_local_entities_found:7 nl_path_intent_tests_passed:3 nl_temporal_intent_tests_passed:2 rules_firings_count:3 actions_dispatched:7 spatial_query_count:10 temporal_query_count:83 zone_relationships_count:13 predicate_list_count:31)
```

`predicate_stats_entity_count:58` is the D4(a) decoder fix observed on the wire (P9 closed).

### 2.3 Tier mutation checks for the NATS-seam arms

Injection W: `tiered_semantic.go:67` `const maxWait = 90 * time.Second` → `1 * time.Millisecond` (waitForCommunities).
Injection L: `tiered_statistical.go:70` `gatewayURL := s.config.GraphQLURL` → `"http://127.0.0.1:1/graphql"` (graphrag-local only).

#6 validate-community-structure `:392` arm, fix + W (statistical):

```
[31/42] validate-community-structure FAILED after 8.59275ms: failed to get communities: no communities after 0.0s (clustering runs=0)
time=2026-09-29T14:41:02.037-05:00 level=ERROR msg="Scenario completed with failure" error="validate-community-structure failed: failed to get communities: no communities after 0.0s (clustering runs=0)" duration=559.564667ms assertions_run=0
```

Warn path restored (`tiered_statistical.go` from backup, SHA `af44bd33…`) + W + L (statistical) — green; with no
community wait, clustering had not run, so graphrag-local took its `:76` arm instead of reaching L:

```
[31/42] validate-community-structure completed in 9.060209ms
[37/42] test-graphrag-local completed in 1.000459ms
[38/42] test-graphrag-global completed in 1.1185ms
time=2026-09-29T14:41:43.809-05:00 level=INFO msg="Scenario completed successfully" duration=976.2415ms assertions_run=0
metadata.warnings (statistical-20260929-144143.json):
 - Failed to get communities: no communities after 0.0s (clustering runs=0)
 - Could not find entity in community: no communities found
 - GraphRAG global search returned only 0 communities for broad query "logistics warehouse operations", expected >= 2
```

#3 graphrag-local `:88` arm, warn path restored + L only (statistical) — green:

```
[31/42] validate-community-structure completed in 24.377162958s
[37/42] test-graphrag-local completed in 11.632291ms
[38/42] test-graphrag-global completed in 11.892333ms
time=2026-09-29T14:42:54.461-05:00 level=INFO msg="Scenario completed successfully" duration=25.4527945s assertions_run=0
metadata.warnings (statistical-20260929-144254.json), GraphRAG/community lines:
 - Community ground truth violation [unexpected_grouped]: temperature_sensors - entity "c360.semstreams-statistical-7916a8.document.content.human_resources.doc-hr-001" should not be in same community as "c360.semstreams-statistical-7916a8.document.sensor.temperature.sensor-temp-001"
 - Community ground truth violation [unexpected_grouped]: safety_documents - entity "c360.semstreams-statistical-7916a8.document.sensor.motion.sensor-motion-001" should not be in same community as "c360.semstreams-statistical-7916a8.document.content.safety.doc-safety-001"
 - GraphRAG local search failed: request failed: Post "http://127.0.0.1:1/graphql": dial tcp 127.0.0.1:1: connect: connection refused
 - GraphRAG global search returned only 0 communities for broad query "logistics warehouse operations", expected >= 2
```

(The two ground-truth violations are the declared RECORDER arm recording, tier green.)

#3 graphrag-local `:88` arm, fix + L (statistical):

```
[31/42] validate-community-structure completed in 24.378790583s
[37/42] test-graphrag-local FAILED after 11.129583ms: GraphRAG local search failed: request failed: Post "http://127.0.0.1:1/graphql": dial tcp 127.0.0.1:1: connect: connection refused
time=2026-09-29T14:43:58.780-05:00 level=ERROR msg="Scenario completed with failure" error="test-graphrag-local failed: GraphRAG local search failed: request failed: Post \"http://127.0.0.1:1/graphql\": dial tcp 127.0.0.1:1: connect: connection refused" duration=25.088661875s assertions_run=0
```

#8 validate-llm-enhancement `:481` arm, fix + W (semantic):

```
[21/45] validate-llm-enhancement FAILED after 9.017208ms: failed to get communities: no communities after 0.0s (clustering runs=0)
time=2026-09-29T14:45:24.047-05:00 level=ERROR msg="Scenario completed with failure" error="validate-llm-enhancement failed: failed to get communities: no communities after 0.0s (clustering runs=0)" duration=383.707583ms assertions_run=0
```

#8 warn path restored (`tiered_semantic.go` from backup, SHA `a5edd5b6…`) + W (semantic) — the stage passes; the run
then went red at a stage this change did not predict (§ 3):

```
[18/45] test-spatial-query completed in 1.135709ms
[19/45] test-temporal-query completed in 1.930833ms
[20/45] test-zone-relationships completed in 1.560792ms
[21/45] validate-llm-enhancement completed in 13.234916ms
[22/45] validate-thematic-answer-eval completed in 4m10.303884s
[23/45] validate-partition-colocation completed in 8.648541ms
[24/45] test-nl-path-intent completed in 53.035417ms
[25/45] test-nl-temporal-intent FAILED after 20.004472208s: NL temporal intent: 0/2 probes returned entities; first failure: temporal_last_hour: NL query request failed: Post "http://localhost:38180/graph-gateway/graphql": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
time=2026-09-29T14:51:25.379-05:00 level=ERROR msg="Scenario completed with failure" error="test-nl-temporal-intent failed: NL temporal intent: 0/2 probes returned entities; first failure: temporal_last_hour: NL query request failed: Post \"http://localhost:38180/graph-gateway/graphql\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)" duration=4m30.761242291s assertions_run=0
```

After every mutant the file was restored from its fixed backup and its SHA re-checked against the list in § 0;
`grep -rn "MUTATION INJECTION" test/` → no hits, `git status --short` → clean.

Not proven at tier level: #7 validate-virtual-edges' `:743` read arm and `:772` mismatch arm (no anomaly exists with
the engine disabled, and `s.natsClient` is a concrete type), inspection only as the design states; #8's `:219`
wait-error and re-fetch arms (inspection; the nil-client and no-communities arms are proven above); #3's `:229` and
#4's arms are proven at the HTTP seam only (§ 1), #3's `:76` also at tier level in the T3 run above.

## 3. Unpredicted red under semantic: test-nl-temporal-intent

In the T5 run above, `test-nl-temporal-intent` failed with both probes at the 10 s client deadline even with
`includeSummaries:false`; `test-nl-path-intent` passed in 53 ms (P14 held for path). Measured on an idle semantic
stack after the scenario loaded data and clustering ran once:

```
'What happened in the last hour?' {'includeSummaries': False} -> 17.49s strategy=temporal count=0 entities=0
'Show events from today' {'includeSummaries': False} -> 0.03s strategy=temporal count=83 entities=83
'What is related to temp-sensor-001?' {'includeSummaries': False} -> 0.03s strategy=pathrag count=29 entities=29
'What happened in the last hour?' {'includeSummaries': False} -> 5.61s strategy=temporal count=0 entities=0
'What happened in the last hour?' {'includeSummaries': False, 'summarizeThreshold': 0} -> 5.64s strategy=temporal count=0 entities=0
```

Cause: design premise P3 is false for the semantic config. The app logs `"msg":"LLM query classifier enabled",
"component":"graph-query","model":"qwen3-1.7b","timeout":30000000000` — `initLLMClassifier`
(`processor/graph-query/component.go:422-444`) resolves `query_classification` to the registry default
(`configs/semantic.json` `"defaults": {"model": "seminstruct-mid-answer"}`), i.e. the answer model. "last hour" misses
the keyword tier (no explicit intent), so `ClassifierChain.ClassifyQuery` (`graph/query/classifier_chain.go:52-92`)
calls the LLM: the 5.6 s idle / 17.5 s cold gap sits between the `graph.query.globalSearch` receipt and the
"using classifier-refined query" log. Behind B0's queued synthesis on the same model it exceeds 10 s, and the second
probe ("today", keyword-routed, 30 ms idle) also timed out in the run; that it queues behind the abandoned first
request is inferred, not measured. The LLM's answer is also empty (count 0) where statistical returns entities for the
same probe. `includeSummaries:false` does not reach the classifier. This does not affect the statistical gate; it
affects the semantic variant (the #1425 path-only run after rebase, and the full semantic tier).

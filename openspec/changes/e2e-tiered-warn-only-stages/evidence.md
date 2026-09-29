# Evidence: e2e-tiered-warn-only-stages task 2.1 (issue #1426)

Branch `claude/gh1426-warn-only-stages`, base `6cd459bb`. Commits (§ 4 adds `1751e27f`, `c418ec2e`): `01882ff4` (structural query stages), `ce4d334d`
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

Fixed files at task-2.1 commit `b8ebea30` (the mutation backups of § 1-2 were taken from these; § 4 and round 1
changed every file but `tiered_structural.go`, so five of the six no longer match HEAD):

```
78e80f43a34c7ca54aec60a9344a25eeaea0fa4f56aa99ddd4a9b4a86970db4d  tiered.go
1d540f930e56330bbd1323f75e44b92bf1979528050d212a094b7f02a628c3cb  tiered_structural.go
726550e25aaf9496b558a427264a44f6984a2fcf03302c92b7c4829393b78a7a  tiered_statistical.go
c5553e469bae52f1510a47695635f8dd3a1103d8c5d966223ff054d0dacff166  tiered_semantic.go
d84ee6f4688cb7b03660b37b7318d8a5d8885e04b4cded7d02291cb67f7d214b  validate_infra.go
1e17a833f8fde8b5585be4f6b2e73a6ceb1dab943f328cbc9ab864c714d42bb2  tiered_warn_only_stages_test.go
```

Files at HEAD (`git show dddae002:<file> | shasum -a 256`; the round-1 code head; the later commit touches only docs):

```
29eb0d43b093e4edc862a9dcad6a5d819a53dd6886b137c092537f27638cb5d3  tiered.go
1d540f930e56330bbd1323f75e44b92bf1979528050d212a094b7f02a628c3cb  tiered_structural.go
55a5add2a3428ec5eb1c511488fa56dade5460dc9ca079a94dc688e62a81251f  tiered_statistical.go
a731fedd998a3c3dcc2e174d98cf1da721608488120cf3328ead69d81a881c8c  tiered_semantic.go
394443bfdcccba2f920bd2d66444e7c5387395cc473a76a1eadac6acafdbe007  validate_infra.go
8819884ba143610d607eade3d6c1ceaf18cef837972dc82ad4d82b5412a81b42  tiered_warn_only_stages_test.go
6cef0be37b3d0cc2ea2a1daec5b31ea76a5ae08fa9f4eeedf3724ba2d5bece0d  validate_structural.go
0087a5810117d07f14cb8dd5fe88038454b678af0aa1a429df78169f69743112  validate_search.go
5995e0c781eb1b6fcdf6ddbaaa6c56bf6106e449a541e905982940973cd348e1  validate_entity.go
6892b14f4dec187aeb672cee686bdd1529f072c93bdc0042e69bc52585a875df  search/queries.go
a60e844b3312387c8d54125233b191da9925a79e679a863a94238463bd7e0dd7  tiered_warn_only_round1_test.go
008405ae0f39ecc2152a9619eb071071db851e2e61746b9ca87f95a4f77f3894  tiered_warn_only_round1_nats_test.go
2ffba9bc3a143255c8a29ab8c1795b095adca2c4d00c12898b8911c3e111dd6b  ../client/metrics.go
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

## 4. Coordinator decisions on the two reds (2026-09-29), applied in `1751e27f` and `c418ec2e`

### 4.1 Red 1: test-graphrag-global decodes snake_case (second instance of the D4(a) class)

`graphRAGGlobalResponse` now reads `community_summaries` / `community_id` (`tiered_statistical.go:40-60`), matching
the handler (`processor/graph-query/graphrag.go:195`, `:236`). The unit stubs serve the snake body, and
`TestGraphRAGGlobal_ReadsSnakeCaseSummaries` proves two summaries decode.

Left alone on purpose: `sendNLQuery`'s `globalSearchResponse` (`tiered_structural.go`, `communitySummaries {
communityId … }`) carries the same camelCase tags. No assertion reads them, and with `includeSummaries:false` the body
carries no summaries anyway. A reviewer does not need to re-find it.

Mutation check (unit seam; the live-stack halves are § 2.1 camelCase → `[38/42] test-graphrag-global FAILED` and
§ 4.3 snake → completed). `$SP` is the session scratchpad; `camel-tiered_statistical.go` is the file as committed
at `eb4bdd34`..`feacf3db`:

```
===== MUTANT: camelCase graphRAGGlobalResponse tags restored (committed eb4bdd34 file)
$ cp $SP/camel-tiered_statistical.go test/e2e/scenarios/tiered_statistical.go && shasum -a 256 test/e2e/scenarios/tiered_statistical.go
726550e25aaf9496b558a427264a44f6984a2fcf03302c92b7c4829393b78a7a  test/e2e/scenarios/tiered_statistical.go
$ grep -n 'json:"communitySummaries"\|json:"communityId"' test/e2e/scenarios/tiered_statistical.go
27:			CommunityID string `json:"communityId"`
45:				CommunityID string   `json:"communityId"`
57:			} `json:"communitySummaries"`
$ go test -count=1 -v -run 'TestGraphRAG|TestWarnOnlyStages' ./test/e2e/scenarios/ | grep -E -- '--- FAIL|Error:|^(ok|FAIL)'
        	Error:      	"GraphRAG global search returned no community summaries for query \"logistics warehouse operations\"" does not contain "answer field empty"
--- FAIL: TestGraphRAGGlobal_EmptyAnswerBesideSummariesFails (0.00s)
        	Error:      	Received unexpected error:
--- FAIL: TestGraphRAGGlobal_ReadsSnakeCaseSummaries (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	0.322s
FAIL
$ cp $SP/snake-tiered_statistical.go test/e2e/scenarios/tiered_statistical.go && shasum -a 256 test/e2e/scenarios/tiered_statistical.go
8b32dacb3a17eb107778517c58927ac3a096c25da994ada6a90f7209dac4f309  test/e2e/scenarios/tiered_statistical.go
$ go test -count=1 -run 'TestGraphRAG|TestWarnOnlyStages' ./test/e2e/scenarios/
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.298s
```

### 4.2 Red 2: test-nl-temporal-intent leaves the semantic variant

Decision: the row's `variants` go from `{"statistical", "semantic"}` to `{"statistical"}` (`tiered.go:347`); the row
comment at `tiered.go:337-345` is rewritten. `test-nl-path-intent` stays in every variant. The 10 s client deadline is
unchanged.

Reason: the path probes are answered by the keyword tier in every variant. The "last hour" temporal probe misses the
keyword tier and is routed through `query_classification`, which under the semantic tier resolves to the answer model.
A model-owned outcome is not graded per-PR (spec delta, requirement 1, third scenario), and the temporal routing fact
is proven under statistical in ms.

Measurement (§ 3; idle semantic stack, `includeSummaries:false`; app log `"LLM query classifier enabled",
"model":"qwen3-1.7b","timeout":30000000000`):

| probe | latency | strategy | entities |
|---|---|---|---|
| "What happened in the last hour?" (cold) | 17.49 s | temporal | 0 |
| "What happened in the last hour?" (warm) | 5.61 s | temporal | 0 |
| "What happened in the last hour?" (+ summarizeThreshold 0) | 5.64 s | temporal | 0 |
| "Show events from today" | 0.03 s | temporal | 83 |
| "What is related to temp-sensor-001?" | 0.03 s | pathrag | 29 |

In the full semantic run both temporal probes hit the 10 s deadline (`[25/45] test-nl-temporal-intent FAILED after
20.004472208s`); that the keyword-routed "today" probe queued behind the abandoned "last hour" request is inferred, not
measured. For the coordinating session: design § 2 row 2 ("A (all variants)", "predicted ms") and P3 / § 9 ("keyword-only
in every tier") need amending; P3 does not hold for `configs/semantic.json`, where `query_classification` resolves to
the registry default model.

`TestStageTable_WarnOnlyDecisions` pins the table decisions. Mutation check:

```
===== MUTANT: test-nl-temporal-intent row back to {"statistical", "semantic"} (tiered.go as committed at eb4bdd34..b8ebea30)
$ cp $SP/fix/tiered.go test/e2e/scenarios/tiered.go && shasum -a 256 test/e2e/scenarios/tiered.go
78e80f43a34c7ca54aec60a9344a25eeaea0fa4f56aa99ddd4a9b4a86970db4d  test/e2e/scenarios/tiered.go
341:		{"test-nl-temporal-intent", s.executeTestNLTemporalIntent, []string{"statistical", "semantic"}},
$ go test -count=1 -v -run TestStageTable ./test/e2e/scenarios/ | grep -E -- '--- FAIL|Error:|Messages:|^(ok|FAIL)'
        	Error:      	Should be false
        	Messages:   	test-nl-temporal-intent must not run in semantic (model-owned outcome)
--- FAIL: TestStageTable_WarnOnlyDecisions (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	0.318s
FAIL
$ cp $SP/rowfix-tiered.go test/e2e/scenarios/tiered.go && shasum -a 256 test/e2e/scenarios/tiered.go
137c1c8c48fd0dea4f3c519371cc71a45994e38b063f6ab4d0eb0dbbce56581a  test/e2e/scenarios/tiered.go
347:		{"test-nl-temporal-intent", s.executeTestNLTemporalIntent, []string{"statistical"}},
$ go test -count=1 -run TestStageTable ./test/e2e/scenarios/
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	0.277s
```

### 4.3 Statistical gate at `c418ec2e`

`task e2e:statistical`, task exit 0. Host check first: `docker ps --format '{{.Names}}'` printed nothing;
`pgrep -fl 'e2e'` exited 1. There were 42 `completed` stage lines and 0 `FAILED`. The changed stages:

```
[18/42] test-spatial-query completed in 1.390959ms
[19/42] test-temporal-query completed in 1.874042ms
[20/42] test-zone-relationships completed in 1.668459ms
[21/42] test-nl-path-intent completed in 35.85525ms
[22/42] test-nl-temporal-intent completed in 19.616125ms
[24/42] test-predicate-list completed in 3.598292ms
[25/42] test-predicate-stats completed in 4.360417ms
[31/42] validate-community-structure completed in 23.965588458s
[37/42] test-graphrag-local completed in 11.632583ms
[38/42] test-graphrag-global completed in 7.811584ms
[40/42] validate-rules completed in 54.891667ms
time=2026-09-29T15:03:55.994-05:00 level=INFO msg="Scenario completed successfully" duration=25.006969209s assertions_run=0
(metrics excerpt: actions_dispatched:7 graphrag_global_communities_found:5 nl_temporal_intent_tests_passed:2 predicate_stats_entity_count:58 rules_firings_count:3)
```

## 5. Review round 1 (owner rulings Q1 "wait but bound", Q2 "absorb"; design § 10b)

Commits: `3676f167` (inventory sweep), `587881b3` (twelve stages, B2), `12ab2167` (B1 wait, M1), `1eb84e8f` (H1-H3),
`dddae002` (KV-seam tests), then this docs commit. Every mutant below was made from a `cp` backup with its SHA-256
recorded and restored the same way (MATCH printed per file); no stash, checkout, or restore was used.

### 5.1 Sweep pins

```
$ { echo "base: 6127d0be43e9255258adc72049418cab0c53220f"; echo; sed -n '/^## Round-1 sweep/,$p' inventory.md; } > $SP/round1-inventory.md
$ scripts/inventory-verify.sh $SP/round1-inventory.md
changed since base (6127d0be..HEAD):
  (none)
pins=76 ok=76 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0
```

Run before the round-1 code commits, so the pins describe the defect at `6127d0be`. The whole-file
`task inventory:verify -- inventory.md` is red by construction once the fixes land (the pinned warn lines are gone;
at `6127d0be` it already read `pins=168 ok=36 moved=96 ambiguous=15 drift=21`).

### 5.2 Unit mutation checks (the round-1 tests, each pre-fix file restored in turn)

The mutant is the file's content at `6127d0be`, i.e. every round-1 arm in that file back on its warn/pass path.
`tiered_warn_only_round1_test.go` drives the HTTP seams (`/components/list`, `/metrics`, GraphQL);
`tiered_warn_only_round1_nats_test.go` drives the KV seams through `NATSValidationClient` against an in-process
JetStream server holding the readers' exact key layouts.

```
===== BASELINE at dddae002
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	1.195s

===== MUTANT: test/e2e/scenarios/validate_structural.go <- git show 6127d0be:test/e2e/scenarios/validate_structural.go (warn path restored); fixed sha 6cef0be37b3d0cc2ea2a1daec5b31ea76a5ae08fa9f4eeedf3724ba2d5bece0d
72432661c765bf70426b765c1031b6e329bf653ba03b9a36f107a56b28a95935  test/e2e/scenarios/validate_structural.go
--- FAIL: TestIndexPopulation_EmptyRequiredIndexFails (0.02s)
--- FAIL: TestRound1_NATSSeamStages_NoClientFails (0.00s)
    --- FAIL: TestRound1_NATSSeamStages_NoClientFails/verify-index-population (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	1.184s
FAIL
restored from backup: 6cef0be37b3d0cc2ea2a1daec5b31ea76a5ae08fa9f4eeedf3724ba2d5bece0d MATCH

===== MUTANT: test/e2e/scenarios/validate_search.go <- git show 6127d0be:test/e2e/scenarios/validate_search.go (warn path restored); fixed sha 0087a5810117d07f14cb8dd5fe88038454b678af0aa1a429df78169f69743112
1770b17e3c137b7051cb88a9a0ea14ab5d4f73b785a3426e1c78757022e475f2  test/e2e/scenarios/validate_search.go
--- FAIL: TestVerifySearchQuality (0.01s)
    --- FAIL: TestVerifySearchQuality/no_hits_fails_in_both_variants (0.00s)
    --- FAIL: TestVerifySearchQuality/transport_failure_fails (0.00s)
    --- FAIL: TestVerifySearchQuality/missed_known_answer_fails_under_statistical_(BM25) (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	1.205s
FAIL
restored from backup: 0087a5810117d07f14cb8dd5fe88038454b678af0aa1a429df78169f69743112 MATCH

===== MUTANT: test/e2e/scenarios/search/queries.go <- git show 6127d0be:test/e2e/scenarios/search/queries.go (warn path restored); fixed sha 6892b14f4dec187aeb672cee686bdd1529f072c93bdc0042e69bc52585a875df
02a3092747dfb3ddab074cb7ea4d92d0f7ec4403640e6a3c45ba931ca395b273  test/e2e/scenarios/search/queries.go
--- FAIL: TestVerifySearchQuality (0.01s)
    --- FAIL: TestVerifySearchQuality/safety_pattern_matches_the_minted_document_ID (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	1.199s
FAIL
restored from backup: 6892b14f4dec187aeb672cee686bdd1529f072c93bdc0042e69bc52585a875df MATCH

===== MUTANT: test/e2e/scenarios/validate_infra.go <- git show 6127d0be:test/e2e/scenarios/validate_infra.go (warn path restored); fixed sha 394443bfdcccba2f920bd2d66444e7c5387395cc473a76a1eadac6acafdbe007
d84ee6f4688cb7b03660b37b7318d8a5d8885e04b4cded7d02291cb67f7d214b  test/e2e/scenarios/validate_infra.go
--- FAIL: TestVerifyOutputs_MissingOutputFails (0.00s)
--- FAIL: TestValidateProcessing_UnhealthyGraphComponentFails (0.00s)
--- FAIL: TestEmbeddingFallback_UnhealthyGraphEmbeddingFails (0.00s)
    --- FAIL: TestEmbeddingFallback_UnhealthyGraphEmbeddingFails/unhealthy (0.00s)
    --- FAIL: TestEmbeddingFallback_UnhealthyGraphEmbeddingFails/absent (0.00s)
--- FAIL: TestValidateRules_WaitsForThresholdsWithinTheBound (0.00s)
--- FAIL: TestRuleStages_FailedMetricsReadFails (0.02s)
    --- FAIL: TestRuleStages_FailedMetricsReadFails/validate-rules (0.02s)
    --- FAIL: TestRuleStages_FailedMetricsReadFails/wait-for-rule-stabilization (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	1.151s
FAIL
restored from backup: 394443bfdcccba2f920bd2d66444e7c5387395cc473a76a1eadac6acafdbe007 MATCH

===== MUTANT: test/e2e/scenarios/validate_entity.go <- git show 6127d0be:test/e2e/scenarios/validate_entity.go (warn path restored); fixed sha 5995e0c781eb1b6fcdf6ddbaaa6c56bf6106e449a541e905982940973cd348e1
67c11c0757ec1b261ce80cd2c4a7e3bbdcceb78ddb61feb0dc6f36d2487544f0  test/e2e/scenarios/validate_entity.go
--- FAIL: TestEntityStructure_EmptySampleFails (0.02s)
--- FAIL: TestEntityRetrieval_MissingFixtureEntityFails (0.02s)
--- FAIL: TestRound1_NATSSeamStages_NoClientFails (0.00s)
    --- FAIL: TestRound1_NATSSeamStages_NoClientFails/verify-entity-count (0.00s)
    --- FAIL: TestRound1_NATSSeamStages_NoClientFails/verify-entity-retrieval (0.00s)
    --- FAIL: TestRound1_NATSSeamStages_NoClientFails/validate-entity-structure (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	1.194s
FAIL
restored from backup: 5995e0c781eb1b6fcdf6ddbaaa6c56bf6106e449a541e905982940973cd348e1 MATCH

===== MUTANT: test/e2e/scenarios/tiered_semantic.go <- git show 6127d0be:test/e2e/scenarios/tiered_semantic.go (warn path restored); fixed sha a731fedd998a3c3dcc2e174d98cf1da721608488120cf3328ead69d81a881c8c
c5553e469bae52f1510a47695635f8dd3a1103d8c5d966223ff054d0dacff166  test/e2e/scenarios/tiered_semantic.go
--- FAIL: TestHierarchyInference_TooFewContainersFails (0.23s)
--- FAIL: TestIncomingIndex_NoContainerFails (0.03s)
--- FAIL: TestBidirectionalTraversal (0.07s)
    --- FAIL: TestBidirectionalTraversal/no_container_fails (0.02s)
    --- FAIL: TestBidirectionalTraversal/no_member_edge_fails (0.02s)
--- FAIL: TestInverseEdges (0.09s)
    --- FAIL: TestInverseEdges/missing_contains_edge_fails_in_statistical (0.02s)
    --- FAIL: TestInverseEdges/count_mismatch_fails (0.02s)
--- FAIL: TestRound1_NATSSeamStages_NoClientFails (0.00s)
    --- FAIL: TestRound1_NATSSeamStages_NoClientFails/validate-hierarchy-inference (0.00s)
    --- FAIL: TestRound1_NATSSeamStages_NoClientFails/validate-incoming-index-predicates (0.00s)
    --- FAIL: TestRound1_NATSSeamStages_NoClientFails/validate-bidirectional-traversal (0.00s)
    --- FAIL: TestRound1_NATSSeamStages_NoClientFails/validate-inverse-edges-materialized (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	0.983s
FAIL
restored from backup: a731fedd998a3c3dcc2e174d98cf1da721608488120cf3328ead69d81a881c8c MATCH

===== MUTANT: test/e2e/scenarios/tiered_statistical.go <- git show 6127d0be:test/e2e/scenarios/tiered_statistical.go (warn path restored); fixed sha 55a5add2a3428ec5eb1c511488fa56dade5460dc9ca079a94dc688e62a81251f
8b32dacb3a17eb107778517c58927ac3a096c25da994ada6a90f7209dac4f309  test/e2e/scenarios/tiered_statistical.go
--- FAIL: TestGraphRAGGlobal_ClientDeadlineIsOverridable (5.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	6.151s
FAIL
restored from backup: 55a5add2a3428ec5eb1c511488fa56dade5460dc9ca079a94dc688e62a81251f MATCH

===== MUTANT: test/e2e/client/metrics.go <- git show 6127d0be:test/e2e/client/metrics.go (warn path restored); fixed sha 2ffba9bc3a143255c8a29ab8c1795b095adca2c4d00c12898b8911c3e111dd6b
bfde67fc56595da90432ea5fd91c39bc1c867612cdd7f40dd52a06867176def9  test/e2e/client/metrics.go
--- FAIL: TestRuleStages_FailedMetricsReadFails (0.05s)
    --- FAIL: TestRuleStages_FailedMetricsReadFails/validate-rules (0.04s)
    --- FAIL: TestRuleStages_FailedMetricsReadFails/wait-for-rule-stabilization (0.00s)
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	1.180s
FAIL
restored from backup: 2ffba9bc3a143255c8a29ab8c1795b095adca2c4d00c12898b8911c3e111dd6b MATCH

===== RESTORED
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	1.166s
```

Inspection-only in round 1: the failed COMMUNITY_SUMMARIES read arms (H2; `s.natsClient` is concrete and a live read
failure is not injectable without breaking the rest of the stage), and `validate-incoming-index-predicates`'
entries-without-predicates arm, which the reader cannot produce (`incomingEntryFromCompositeKey` drops an empty
predicate, `client/nats.go:981-983`).

### 5.3 Gates at `dddae002`

```
$ go build ./...                                  → go build exit 0
$ task build:e2e                                  → Built cmd/e2e/e2e / task build:e2e exit 0
$ task lint                                       → ok  github.com/c360studio/semstreams/test/natsclient 0.555s / task lint exit 0
$ go test -race -count=1 ./test/e2e/scenarios/    → ok  github.com/c360studio/semstreams/test/e2e/scenarios 4.444s
$ task test                                       → 0 FAIL lines / task test exit 0
```

The first `task lint` run failed `lint:cleanup-roots` (`new uncertain-owner-provenance:
test/e2e/scenarios/tiered_warn_only_round1_nats_test.go|newKVFixture|ordinary|nc.Close|unresolved callback`): the
fixture opened a second raw `nats.Conn`. It now takes JetStream from the validation client's own connection
(`vc.Client().JetStream()`), whose cleanup is `vc.Close(context.Background())`; the guard passes. The baseline was not
touched.

### 5.4 Statistical tier at `dddae002`

`task e2e:statistical`, task exit 0. Host check first: `docker ps --format '{{.Names}}'` printed nothing;
`pgrep -fl e2e` exited 1. 42 `completed` stage lines, 0 `FAILED`. Every stage round 1 changed:

```
[3/42] validate-processing completed in 9.996333ms
[8/42] validate-hierarchy-inference completed in 5.902459ms
[9/42] verify-entity-count completed in 3.071667ms
[10/42] verify-entity-retrieval completed in 2.65625ms
[11/42] validate-entity-structure completed in 2.570542ms
[12/42] verify-index-population completed in 21.270667ms
[22/42] test-nl-temporal-intent completed in 17.119542ms
[27/42] verify-search-quality completed in 6.1045ms
[30/42] test-embedding-fallback completed in 1.213292ms
[31/42] validate-community-structure completed in 24.412061792s
[34/42] validate-incoming-index-predicates completed in 2.620541ms
[35/42] validate-bidirectional-traversal completed in 3.20225ms
[36/42] validate-inverse-edges-materialized completed in 3.505ms
[38/42] test-graphrag-global completed in 8.756792ms
[39/42] wait-for-rule-stabilization completed in 215.640916ms
[40/42] validate-rules completed in 27.536958ms
[42/42] verify-outputs completed in 860.917µs
time=2026-09-29T16:17:26.450-05:00 level=INFO msg="Scenario completed successfully" duration=25.300292417s metrics="map[…]" assertions_run=0
(metrics excerpt: actions_dispatched:7 bidir_member_count:1 entities_retrieved:5 fallback_verified:1 hierarchy_container_count:46 indexes_populated:7 inverse_symmetry_valid:1 known_answer_tests_passed:7 known_answer_tests_total:7 outputs_found:2 rules_firings_count:3 rules_threshold_wait_ms:8 
```

`metadata.warnings` in `statistical-20260929-161726.json` now holds only declared recorder output: the average-score
arm and the three community ground-truth violations. The known-answer miss that was in every earlier run is gone
(7/7) with the pattern fix.

B1 measured wait: `rules_threshold_wait_ms:8` on this host, where the first read already showed 3 firings and 7
actions. The CI runner, which read 1 firing at 17 ms in run 36623877500, is where the wait matters; whether its
fixture reaches 2 within the 30 s bound is measured by the PR's `e2e statistical` job, not here.

### 5.5 H3 and M4

H3: the attribution probe for the semantic "last hour" `count=0` was not made. The instruction was to probe only if an
`e2e:semantic:up` target exists; `task --list` has `e2e:semantic:debug` (full ML stack build) and no `:up`. The
outcome is recorded as unattributed (design § 9, § 10b); nothing is filed.

M4: delivered, `docs/contributing/02-e2e-tests.md` § Assertion Strategy, one sentence.

### 5.6 Per-ruling conformance (M5)

| Ruling (source) | Implemented at | Conforms |
|---|---|---|
| "pull the warn-only slice into 163" (owner, #1426) | placement only: #1426 and PR #1427 on `v1.0.0-beta.163` | yes (no code) |
| "agree on 1 - let's fix the class" (owner, #1426): fourteen paths assert | `tiered_structural.go:1073`, `:1182`, `:1442`, `:1853`; `tiered_statistical.go:233`, `:360`; § 1-4 above | yes |
| D1(a): graphrag local/global statistical only | `tiered.go:407-408` | yes |
| D2(a): anomaly row leaves, code stays for #620 | `tiered.go:420` (comment; row gone); `executeValidateAnomalyDetection` still defined | yes |
| D3(b): `validate-rules` asserts `MinRuleFirings`/`MinActionsDispatched` | `validate_infra.go:509`, `:512` | yes (red on CI at `6127d0be`; Q1 answers it) |
| D4(a): predicate decoders read snake_case | `tiered_structural.go:1969`, `:1984-1985`; `tiered_statistical.go:60` (same class) | yes |
| Q1(a) "wait but bound": wait on the counters, bounded by `ValidationTimeout`, measured wait recorded | `validate_infra.go:494` (call, `rules_threshold_wait_ms`), `:523` (`awaitRuleThresholds`) | yes |
| Q2 "absorb": twelve stages from a sweep of every stage function | inventory § Round-1 sweep; `validate_structural.go:84`; `validate_search.go:53`; `validate_infra.go:201`, `:249`, `:433`; `validate_entity.go:145`, `:379`, `:399`; `tiered_semantic.go:1015`, `:1179`, `:1246`, `:1338`, `:1454` | yes |
| "path-only gate for 1117" (owner, #1117): this change lands first; model-owned outcomes leave per-PR variants | `tiered.go:351` (`test-nl-temporal-intent` statistical only); no `.github/` or `taskfiles/` edit | yes |
| #1222: no parallel assertion accounting; `AssertionsRun` untouched | `git diff 6127d0be -- test/ \| grep -c AssertionsRun` → 0; `cmd/e2e/main.go` unchanged | yes |
| Simple over edge-case; no new mechanism (owner, 2026-09-22) | round 1 adds two unexported helpers: `awaitRuleThresholds` (the wait Q1 rules) and `searchQualityVerdict` (the stage's error return, split out); `ExtractRuleMetrics` changed in place, no new surface. `MetricsClient.WaitForMetric` (`test/e2e/client/metrics.go:573`) was considered for the Q1 wait and not used (round 2 M-d): it waits on one series per call with its own `Timeout`, so two counters under one bound need a shared deadline the API does not take; its first read comes only after one ticker tick; and it returns no value, so the caller would still read once more to assert and record. `awaitRuleThresholds` is that one loop, returning the last read. | yes, with those two named |
| Proposal boundary: no 10 s deadline changes without a measurement | `tiered_statistical.go:290` keeps 10 s as the helper's default | yes |

## 6. Review round 2 (2026-09-30; verdict APPROVE at `af3bbd40`, four mediums, four nits; fixes in `81667ba5`)

| Finding | Disposition | Where |
|---|---|---|
| M-a task 2.1 overstates (four arms revert without a red) | doc-sentence option: the four arms join the inspection-only list, plus the new outgoing-read arm below | `tasks.md` 2.1 |
| M-b H3 row comment claims routing and 2/2 | reworded: the stage fails only when neither probe returns entities; 2/2 is a measurement; line count kept so § 5.6 pins hold | `tiered.go:348-349` |
| M-c baseline read warns and substitutes zero | fails the stage, same shape as `wait-for-rule-stabilization` (`validate_infra.go:750`); unit test at the metrics seam (first scrape 500, later scrapes meet the thresholds); mutation check below | `validate_infra.go:476-480`; `tiered_warn_only_round1_test.go` `TestValidateRules_FailedBaselineReadFails` |
| M-d `awaitRuleThresholds` vs `WaitForMetric` | one sentence recorded in § 5.6 (one series per call, own `Timeout`, first read after one tick, returns no value) | § 5.6 "Simple over edge-case" row |
| Nit `validate_search.go:66` raw `s.config.Variant` | both reads (`:25` executor, `:66` verdict) go through `effectiveVariant`; the verdict helper takes the variant; subtest with `Variant: ""` and `result.Metrics["variant"]="statistical"`; mutation check below | `validate_search.go`; `TestVerifySearchQuality/missed_known_answer_fails_under_auto-detected_statistical` |
| Nit hand-typed minted ID in the pattern test | left as recorded: deriving it would import `examples/processors/document` into the scenario tests; the tier run (`known_answer_tests_passed:7`) is the evidence | `tiered_warn_only_round1_test.go:200` |
| Nit `tiered_semantic.go:1306` discards the outgoing-read error | returns the error (design § 10b row: a failed read is A); no failing-read fixture, so it is listed inspection-only in task 2.1 | `tiered_semantic.go:1306` |
| Nit inventory omits the no-keywords warn | named in § Swept and left with the base-pinned line | `inventory.md` § Swept and left |

Swept one path over: the other raw `s.config.Variant` reads (`validate_infra.go:32`, `:73`; `validate_entity.go:169`;
`tiered_semantic.go:1449`; `validate_search.go:474`, `:508`, `:515`, `:517`; `tiered.go:578` is the detector) predate
this change and are outside the round-2 finding; none is touched here. No per-PR invocation omits `--variant`.

### 6.1 Gates at `81667ba5`
```
$ go test -race -count=1 ./test/e2e/scenarios/
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	4.511s
$ task lint   -> exit 0 (lint:request-guard: ok ./test/natsclient 0.542s)
```

### 6.2 Mutation checks (each file restored from its `cp` backup; mutant = the file at `af3bbd40`)
```
===== MUTANT: test/e2e/scenarios/validate_infra.go <- af3bbd40 (M-c warn-and-zero arm restored); fixed sha 0c8f95bd7e524aaa537db8a8daba03a9cfa1b7e1e4534138bde83612deebf1db
394443bfdcccba2f920bd2d66444e7c5387395cc473a76a1eadac6acafdbe007  test/e2e/scenarios/validate_infra.go
--- FAIL: TestValidateRules_FailedBaselineReadFails (0.00s)
        	Error Trace:	/Users/coby/Code/c360/semstreams-wt/claude/gh1426-warn-only-stages/test/e2e/scenarios/tiered_warn_only_round1_test.go:358
        	Error:      	An error is expected but got nil.
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	0.366s
FAIL
restored from backup: 0c8f95bd7e524aaa537db8a8daba03a9cfa1b7e1e4534138bde83612deebf1db MATCH
===== MUTANT: test/e2e/scenarios/validate_search.go <- af3bbd40 (raw s.config.Variant reads restored); fixed sha 4508eab1edff7e848ba14969541cc0f0b7891395999c96ee73f355d2ddf56ab0
0087a5810117d07f14cb8dd5fe88038454b678af0aa1a429df78169f69743112  test/e2e/scenarios/validate_search.go
--- FAIL: TestVerifySearchQuality (0.01s)
    --- FAIL: TestVerifySearchQuality/missed_known_answer_fails_under_auto-detected_statistical (0.00s)
            	Error Trace:	/Users/coby/Code/c360/semstreams-wt/claude/gh1426-warn-only-stages/test/e2e/scenarios/tiered_warn_only_round1_test.go:199
            	Error:      	"search failed for 8/8 queries: \"What documents mention forklift safety?\": returned no hits; \"Are there safety observations related to temperature?\": returned no hits; \"What maintenance was done on cold storage equipment?\": returned no hits; \"Find all sensors in zone-a\": returned no hits; \"forklift operation inspection equipment maintenance\": returned no hits; \"cold storage temperature monitoring refrigeration\": returned no hits; \"hydraulic fluid maintenance equipment repair\": returned no hits; \"warehouse safety guidelines emergency evacuation fire\": returned no hits" does not contain "known-answer search failed under BM25 (6/7 passed)"
FAIL
FAIL	github.com/c360studio/semstreams/test/e2e/scenarios	0.363s
restored from backup: 4508eab1edff7e848ba14969541cc0f0b7891395999c96ee73f355d2ddf56ab0 MATCH
===== RESTORED
ok  	github.com/c360studio/semstreams/test/e2e/scenarios	4.421s
git status --porcelain entries: 0
```

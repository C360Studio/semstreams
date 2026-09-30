# Inventory: stages the tiered e2e scenario runs under `--variant semantic`
base: 2430ebd15ffb4cfa1cbe0c2f1cc45fa18ed294e4

Surface, per brief: every row of the stage table in `test/e2e/scenarios/tiered.go` (`getStagesForVariant`) whose
`variants` is `nil` or contains `"semantic"`. This is `openspec/changes/e2e-semantic-path-only-gate/`'s own inventory
(issue #1117, draft PR #1425).

## Claimed gap

The proposal (`openspec/changes/e2e-semantic-path-only-gate/proposal.md`) names four things to ground here: which
three stages leave the per-PR run, the `test-http-gateway` residual, whether a skip mechanism already exists, and the
dangling TODO. All four pinned below.

- `openspec/changes/e2e-semantic-path-only-gate/proposal.md:17` — `The three quality stages the owner named leave the per-PR semantic run behind an env flag the ladder sets:`
- `openspec/changes/e2e-semantic-path-only-gate/proposal.md:25` — `residual on this path (a 56 s globalSearch under semantic, measured 2026-09-29 on #1117:`
- `openspec/changes/e2e-semantic-path-only-gate/proposal.md:32` — `about the semantic gate and its baton reference are deleted (scope box 5); the tracking is #1117.`

**No skip flag exists today.** The proposal's own "What Changes" calls the flag's name and mechanism a design
question (`proposal.md:20`: "the inventory (`inventory.md`) grounds them"). Confirmed by exhaustive search: every
`os.Getenv`/`os.LookupEnv` under `test/e2e/` and `cmd/e2e/` (see `## Searches`) is one of `SEMSTREAMS_BASE_URL`,
`UDP_ENDPOINT`, `E2E_VARIANT`, `E2E_OUTPUT_DIR`, `AGENTIC_COMPOSE_FILE`, `AGENTIC_LLM_URL`,
`SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT`, `SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT` — none of which gates stage *membership*.
The only existing per-variant skip mechanism is the stage table's `variants []string` field itself (item 4 below).

## Spellings of the fact

### A. The stage table: 44 of 54 rows run under `--variant semantic`

10 rows are excluded (variants non-nil and not containing `"semantic"`):
`validate-canonical-create-no-hierarchy`, `validate-relationship-no-stub`, `test-temporal-observed-time` (all
`["structural"]`); `test-nl-temporal-intent`, `validate-retired-structural-bucket-absent`, `test-graphrag-local`,
`test-graphrag-global` (all `["statistical"]`); `validate-zero-embeddings`, `validate-zero-clusters`,
`validate-entity-triples` (all `["structural"]`).

- `test/e2e/scenarios/tiered.go:247` — `func (s *TieredScenario) getStagesForVariant(variant string) []stage {`
- `test/e2e/scenarios/tiered.go:442` — `	// Filter stages based on variant`
- `test/e2e/scenarios/tiered.go:445` — `		if len(st.variants) == 0 {`
- `test/e2e/scenarios/tiered.go:448` — `			for _, allowedVariant := range st.variants {`
- `test/e2e/scenarios/tiered.go:449` — `				if variant == allowedVariant {`

Per-stage rows, function definitions, and every `return fmt.Errorf`/`errors.New`/`Warnings = append` assertion site
inside each (item 1 of the brief), with delegate helpers noted where the stage function itself carries no direct
assertion (item 1's "the lines where it returns an error or appends a Warnings entry"):

<!-- total stage rows: 54; semantic-variant rows: 44 -->

### `verify-components` (nil (all variants))
- `test/e2e/scenarios/tiered.go:250` — `{"verify-components", s.executeVerifyComponents, nil},`
- `test/e2e/scenarios/validate_infra.go:20` — `func (s *TieredScenario) executeVerifyComponents(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:24` — `return fmt.Errorf("component verification failed: %w", err)`
- `test/e2e/scenarios/validate_infra.go:69` — `return fmt.Errorf("missing components: %v", missingComponents)`

### `send-mixed-data` (nil (all variants))
- `test/e2e/scenarios/tiered.go:251` — `{"send-mixed-data", s.executeSendMixedData, nil},`
- `test/e2e/scenarios/validate_infra.go:86` — `func (s *TieredScenario) executeSendMixedData(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:91` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Could not capture pre-send baseline: %v", err))`

### `validate-processing` (nil (all variants))
- `test/e2e/scenarios/tiered.go:252` — `{"validate-processing", s.executeValidateProcessing, nil},`
- `test/e2e/scenarios/validate_infra.go:104` — `func (s *TieredScenario) executeValidateProcessing(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:109` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Could not fetch current metrics: %v", err))`
- `test/e2e/scenarios/validate_infra.go:118` — `result.Warnings = append(result.Warnings, "No pre-send baseline available, using default wait")`
- `test/e2e/scenarios/validate_infra.go:121` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Components health wait: %v", err))`
- `test/e2e/scenarios/validate_infra.go:136` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Processing wait: %v (may still be processing)", err))`
- `test/e2e/scenarios/validate_infra.go:144` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to capture flow snapshot: %v", err))`
- `test/e2e/scenarios/validate_infra.go:154` — `return fmt.Errorf("component query failed: %w", err)`
- `test/e2e/scenarios/validate_infra.go:193` — `return fmt.Errorf("graph components not found: %v", missingGraph)`
- `test/e2e/scenarios/validate_infra.go:201` — `return fmt.Errorf("graph components not healthy: %v", unhealthyGraph)`

### `wait-for-embeddings` (['statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:256` — `{"wait-for-embeddings", s.executeWaitForEmbeddings, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/validate_infra.go:284` — `func (s *TieredScenario) executeWaitForEmbeddings(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:296` — `result.Warnings = append(result.Warnings, "semembed unavailable, HTTP embeddings may not be generated")`
- `test/e2e/scenarios/validate_infra.go:314` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/validate_infra.go:337` — `result.Warnings = append(result.Warnings, fmt.Sprintf("Unknown embedding provider: %s, waiting for entity stabilization", variant.embeddingProvider))`

### `validate-embedding-queue-health` (['statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:260` — `{"validate-embedding-queue-health", s.validateEmbeddingQueueHealth, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:794` — `func (s *TieredScenario) validateEmbeddingQueueHealth(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:809` — `return fmt.Errorf("embedding queue health is unverifiable: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:821` — `return fmt.Errorf("embedding queue health is unverifiable: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:824` — `return fmt.Errorf(`
- `test/e2e/scenarios/tiered_semantic.go:899` — `return fmt.Errorf(`
- `test/e2e/scenarios/tiered_semantic.go:914` — `result.Warnings = append(result.Warnings, msg)`
- `test/e2e/scenarios/tiered_semantic.go:919` — `result.Warnings = append(result.Warnings, msg)`
- `test/e2e/scenarios/tiered_semantic.go:927` — `result.Warnings = append(result.Warnings, msg)`
- `test/e2e/scenarios/tiered_semantic.go:930` — `return fmt.Errorf("embedding queue is unhealthy: %s", strings.Join(violations, "; "))`

### `wait-for-entity-stabilization` (nil (all variants))
- `test/e2e/scenarios/tiered.go:265` — `{"wait-for-entity-stabilization", s.executeWaitForEntityStabilization, nil},`
- `test/e2e/scenarios/validate_entity.go:90` — `func (s *TieredScenario) executeWaitForEntityStabilization(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_entity.go:132` — `return fmt.Errorf("entity stabilization failed: got %d, expected %d (timed_out=%v used_sse=%v wait_duration=%s)",`

### `graph-roundtrip` (nil (all variants))
- `test/e2e/scenarios/tiered.go:269` — `{"graph-roundtrip", s.executeGraphRoundTrip, nil},`
- `test/e2e/scenarios/tiered.go:459` — `func (s *TieredScenario) executeGraphRoundTrip(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered.go:461` — `return fmt.Errorf("graph-roundtrip requires the NATS validation client")`
- `test/e2e/scenarios/tiered.go:464` — `return fmt.Errorf("graph-roundtrip requires Message Logger")`
- `test/e2e/scenarios/tiered.go:473` — `return fmt.Errorf("graph-roundtrip: tier variant is unresolved, so the deployment " +`

### `validate-hierarchy-inference` (['structural', 'statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:279` — `{"validate-hierarchy-inference", s.validateHierarchyInference, []string{"structural", "statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:943` — `func (s *TieredScenario) validateHierarchyInference(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:945` — `return fmt.Errorf("NATS client not available for hierarchy inference validation")`
- `test/e2e/scenarios/tiered_semantic.go:965` — `return fmt.Errorf("failed to get entity IDs: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:1015` — `return fmt.Errorf("hierarchy inference not working: only %d containers for %d source entities (expected at least %d)",`

### `verify-entity-count` (nil (all variants))
- `test/e2e/scenarios/tiered.go:281` — `{"verify-entity-count", s.executeVerifyEntityCount, nil},`
- `test/e2e/scenarios/validate_entity.go:143` — `func (s *TieredScenario) executeVerifyEntityCount(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_entity.go:145` — `return fmt.Errorf("NATS client not available for entity count verification")`

### `verify-entity-retrieval` (nil (all variants))
- `test/e2e/scenarios/tiered.go:282` — `{"verify-entity-retrieval", s.executeVerifyEntityRetrieval, nil},`
- `test/e2e/scenarios/validate_entity.go:314` — `func (s *TieredScenario) executeVerifyEntityRetrieval(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_entity.go:316` — `return fmt.Errorf("NATS client not available for entity retrieval verification")`
- `test/e2e/scenarios/validate_entity.go:379` — `return fmt.Errorf("retrieved %d/%d test entities; missing: %v", foundEntities, len(testEntities), missingEntities)`

### `validate-entity-structure` (nil (all variants))
- `test/e2e/scenarios/tiered.go:283` — `{"validate-entity-structure", s.executeValidateEntityStructure, nil},`
- `test/e2e/scenarios/validate_entity.go:386` — `func (s *TieredScenario) executeValidateEntityStructure(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_entity.go:388` — `return fmt.Errorf("NATS client not available for entity structure validation")`
- `test/e2e/scenarios/validate_entity.go:394` — `return fmt.Errorf("failed to get entity sample: %w", err)`
- `test/e2e/scenarios/validate_entity.go:399` — `return fmt.Errorf("no entities available for structure validation")`
- `test/e2e/scenarios/validate_entity.go:489` — `return fmt.Errorf("entity structure validation failed for %d of %d sampled entities: %v",`

### `verify-index-population` (nil (all variants))
- `test/e2e/scenarios/tiered.go:284` — `{"verify-index-population", s.executeVerifyIndexPopulation, nil},`
- `test/e2e/scenarios/validate_structural.go:14` — `func (s *TieredScenario) executeVerifyIndexPopulation(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_structural.go:16` — `return fmt.Errorf("NATS client not available for index population verification")`
- `test/e2e/scenarios/validate_structural.go:84` — `return fmt.Errorf("required indexes empty or unreadable: %v", emptyRequired)`

### `test-pathrag-sensor` (nil (all variants))
- `test/e2e/scenarios/tiered.go:289` — `{"test-pathrag-sensor", s.executeTestPathRAGSensor, nil},`
- `test/e2e/scenarios/tiered_structural.go:555` — `func (s *TieredScenario) executeTestPathRAGSensor(ctx context.Context, result *Result) error {`
  delegates its assertion to a shared helper:
- `test/e2e/scenarios/tiered_structural.go:684` — `func (s *TieredScenario) validatePathRAGResultNamed(resp *pathRAGResponse, startEntity string, latency time.Duration, result *Result, testName string) error {`
- `test/e2e/scenarios/tiered_structural.go:699` — `return fmt.Errorf("PathRAG returned no entities for start entity %s", startEntity)`
- `test/e2e/scenarios/tiered_structural.go:728` — `return fmt.Errorf("PathRAG decay scoring violated: %s", decayViolation)`
- `test/e2e/scenarios/tiered_structural.go:679` — `func (s *TieredScenario) validatePathRAGResult(resp *pathRAGResponse, startEntity string, latency time.Duration, result *Result) error {`

### `test-pathrag-boundary` (nil (all variants))
- `test/e2e/scenarios/tiered.go:290` — `{"test-pathrag-boundary", s.executeTestPathRAGBoundary, nil},`
- `test/e2e/scenarios/tiered_structural.go:1450` — `func (s *TieredScenario) executeTestPathRAGBoundary(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:1470` — `return fmt.Errorf("failed to marshal PathRAG boundary query: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1475` — `return fmt.Errorf("failed to create PathRAG boundary request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1482` — `return fmt.Errorf("PathRAG boundary request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1488` — `return fmt.Errorf("PathRAG boundary returned status %d: %s", resp.StatusCode, string(body))`
- `test/e2e/scenarios/tiered_structural.go:1493` — `return fmt.Errorf("failed to read PathRAG boundary response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1498` — `return fmt.Errorf("failed to parse PathRAG boundary response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1502` — `return fmt.Errorf("PathRAG boundary GraphQL error: %s", graphqlResp.Errors[0].Message)`
- `test/e2e/scenarios/tiered_structural.go:1523` — `return fmt.Errorf("PathRAG maxNodes violated: got %d entities, expected <= %d (maxNodes=%d + start entity)", entityCount, expectedMax, maxNodes)`

### `test-pathrag-document` (nil (all variants))
- `test/e2e/scenarios/tiered.go:292` — `{"test-pathrag-document", s.executeTestPathRAGDocument, nil},`
- `test/e2e/scenarios/tiered_structural.go:577` — `func (s *TieredScenario) executeTestPathRAGDocument(ctx context.Context, result *Result) error {`
  delegates its assertion to a shared helper:
- `test/e2e/scenarios/tiered_structural.go:684` — `func (s *TieredScenario) validatePathRAGResultNamed(resp *pathRAGResponse, startEntity string, latency time.Duration, result *Result, testName string) error {`
- `test/e2e/scenarios/tiered_structural.go:699` — `return fmt.Errorf("PathRAG returned no entities for start entity %s", startEntity)`
- `test/e2e/scenarios/tiered_structural.go:728` — `return fmt.Errorf("PathRAG decay scoring violated: %s", decayViolation)`
- `test/e2e/scenarios/tiered_structural.go:679` — `func (s *TieredScenario) validatePathRAGResult(resp *pathRAGResponse, startEntity string, latency time.Duration, result *Result) error {`

### `test-entityid-hierarchy` (nil (all variants))
- `test/e2e/scenarios/tiered.go:294` — `{"test-entityid-hierarchy", s.executeTestEntityIDHierarchy, nil},`
- `test/e2e/scenarios/tiered_structural.go:737` — `func (s *TieredScenario) executeTestEntityIDHierarchy(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:752` — `return fmt.Errorf("failed to marshal hierarchy query: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:757` — `return fmt.Errorf("failed to create hierarchy request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:765` — `return fmt.Errorf("hierarchy request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:771` — `return fmt.Errorf("hierarchy returned status %d: %s", resp.StatusCode, string(body))`
- `test/e2e/scenarios/tiered_structural.go:776` — `return fmt.Errorf("failed to read hierarchy response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:797` — `return fmt.Errorf("failed to parse hierarchy response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:801` — `return fmt.Errorf("hierarchy GraphQL error: %s", hierarchyResp.Errors[0].Message)`
- `test/e2e/scenarios/tiered_structural.go:817` — `return fmt.Errorf("entityIdHierarchy returned 0 entities")`
- `test/e2e/scenarios/tiered_structural.go:827` — `return fmt.Errorf("entityIdHierarchy returned no children at root level")`

### `test-entities-by-prefix` (nil (all variants))
- `test/e2e/scenarios/tiered.go:295` — `{"test-entities-by-prefix", s.executeTestEntitiesByPrefix, nil},`
- `test/e2e/scenarios/tiered_structural.go:854` — `func (s *TieredScenario) executeTestEntitiesByPrefix(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:884` — `return fmt.Errorf("failed to marshal prefix query: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:889` — `return fmt.Errorf("failed to create prefix request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:895` — `return fmt.Errorf("prefix request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:900` — `return fmt.Errorf("failed to read prefix response: %w", readErr)`
- `test/e2e/scenarios/tiered_structural.go:903` — `return fmt.Errorf("prefix query returned status %d: %s", resp.StatusCode, string(bodyBytes))`
- `test/e2e/scenarios/tiered_structural.go:918` — `return fmt.Errorf("failed to parse prefix response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:921` — `return fmt.Errorf("prefix query GraphQL error: %s", prefixResp.Errors[0].Message)`
- `test/e2e/scenarios/tiered_structural.go:931` — `return fmt.Errorf("prefix query returned repeated continuation cursor %q", cursor)`
- `test/e2e/scenarios/tiered_structural.go:950` — `return fmt.Errorf("entitiesByPrefix returned 0 entities for prefix %s", prefix)`
- `test/e2e/scenarios/tiered_structural.go:961` — `return fmt.Errorf("entity %s does not match prefix %s", entity.ID, prefix)`

### `test-spatial-query` (nil (all variants))
- `test/e2e/scenarios/tiered.go:297` — `{"test-spatial-query", s.executeTestSpatialQuery, nil},`
- `test/e2e/scenarios/tiered_structural.go:980` — `func (s *TieredScenario) executeTestSpatialQuery(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:1002` — `return fmt.Errorf("failed to marshal spatial query: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1007` — `return fmt.Errorf("failed to create spatial request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1015` — `return fmt.Errorf("spatial request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1021` — `return fmt.Errorf("spatial query returned status %d: %s", resp.StatusCode, string(body))`
- `test/e2e/scenarios/tiered_structural.go:1026` — `return fmt.Errorf("failed to read spatial response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1042` — `return fmt.Errorf("failed to parse spatial response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1046` — `return fmt.Errorf("spatial query GraphQL error: %s", spatialResp.Errors[0].Message)`
- `test/e2e/scenarios/tiered_structural.go:1073` — `return fmt.Errorf("spatial query returned 0 entities - check if geo triples are being indexed")`

### `test-temporal-query` (nil (all variants))
- `test/e2e/scenarios/tiered.go:298` — `{"test-temporal-query", s.executeTestTemporalQuery, nil},`
- `test/e2e/scenarios/tiered_structural.go:1082` — `func (s *TieredScenario) executeTestTemporalQuery(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:1106` — `return fmt.Errorf("failed to marshal temporal query: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1111` — `return fmt.Errorf("failed to create temporal request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1119` — `return fmt.Errorf("temporal request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1125` — `return fmt.Errorf("temporal query returned status %d: %s", resp.StatusCode, string(body))`
- `test/e2e/scenarios/tiered_structural.go:1130` — `return fmt.Errorf("failed to read temporal response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1146` — `return fmt.Errorf("failed to parse temporal response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1150` — `return fmt.Errorf("temporal query GraphQL error: %s", temporalResp.Errors[0].Message)`
- `test/e2e/scenarios/tiered_structural.go:1182` — `return fmt.Errorf("temporal query returned 0 entities - check if temporal index is being populated")`

### `test-zone-relationships` (nil (all variants))
- `test/e2e/scenarios/tiered.go:302` — `{"test-zone-relationships", s.executeTestZoneRelationships, nil},`
- `test/e2e/scenarios/tiered_structural.go:1338` — `func (s *TieredScenario) executeTestZoneRelationships(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:1365` — `return fmt.Errorf("failed to marshal relationships query: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1370` — `return fmt.Errorf("failed to create relationships request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1378` — `return fmt.Errorf("relationships request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1384` — `return fmt.Errorf("relationships query returned status %d: %s", resp.StatusCode, string(body))`
- `test/e2e/scenarios/tiered_structural.go:1389` — `return fmt.Errorf("failed to read relationships response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1406` — `return fmt.Errorf("failed to parse relationships response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1410` — `return fmt.Errorf("relationships query GraphQL error: %s", relationshipsResp.Errors[0].Message)`
- `test/e2e/scenarios/tiered_structural.go:1442` — `return fmt.Errorf("zone %s has 0 incoming relationships - check if zone triples are being indexed", zoneEntityID)`

### `validate-llm-enhancement` (['semantic'])
- `test/e2e/scenarios/tiered.go:314` — `{"validate-llm-enhancement", s.executeValidateLLMEnhancement, []string{"semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:474` — `func (s *TieredScenario) executeValidateLLMEnhancement(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:479` — `return fmt.Errorf("NATS client not available for LLM enhancement validation")`
- `test/e2e/scenarios/tiered_semantic.go:487` — `return fmt.Errorf("failed to get communities: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:491` — `return fmt.Errorf("no communities found for LLM enhancement validation")`
- `test/e2e/scenarios/tiered_semantic.go:506` — `return fmt.Errorf("failed to re-fetch communities after LLM wait: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:515` — `return fmt.Errorf("failed to read community summaries: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:535` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_semantic.go:547` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_semantic.go:550` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_semantic.go:553` — `result.Warnings = append(result.Warnings,`

### `validate-thematic-answer-eval` (['semantic'])
- `test/e2e/scenarios/tiered.go:327` — `{"validate-thematic-answer-eval", s.executeValidateThematicAnswerEval, []string{"semantic"}},`
- `test/e2e/scenarios/validate_thematic_eval.go:227` — `func (s *TieredScenario) executeValidateThematicAnswerEval(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_thematic_eval.go:231` — `return fmt.Errorf("thematic-answer eval requires the NATS validation client, which was not initialized")`
- `test/e2e/scenarios/validate_thematic_eval.go:248` — `return fmt.Errorf("thematic-answer eval could not reach graph.query.globalSearch for %q: %w", q.id, err)`

### `validate-partition-colocation` (['semantic'])
- `test/e2e/scenarios/tiered.go:336` — `{"validate-partition-colocation", s.executePartitionColocation, []string{"semantic"}},`
- `test/e2e/scenarios/validate_partition_colocation.go:133` — `func (s *TieredScenario) executePartitionColocation(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_partition_colocation.go:137` — `return fmt.Errorf("partition-colocation diagnostic requires the NATS validation client, which was not initialized")`
- `test/e2e/scenarios/validate_partition_colocation.go:148` — `return fmt.Errorf("partition-colocation diagnostic could not read COMMUNITY_INDEX via GetAllCommunities: %w", err)`

### `test-nl-path-intent` (nil (all variants))
- `test/e2e/scenarios/tiered.go:350` — `{"test-nl-path-intent", s.executeTestNLPathIntent, nil},`
- `test/e2e/scenarios/tiered_structural.go:1759` — `func (s *TieredScenario) executeTestNLPathIntent(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:1853` — `return fmt.Errorf("NL path intent: 0/%d probes returned entities; first failure: %s",`

### `test-entity-by-alias` (nil (all variants))
- `test/e2e/scenarios/tiered.go:353` — `{"test-entity-by-alias", s.executeTestEntityByAlias, nil},`
- `test/e2e/scenarios/tiered_structural.go:1540` — `func (s *TieredScenario) executeTestEntityByAlias(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:1570` — `return fmt.Errorf("failed to marshal entityByAlias query: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1575` — `return fmt.Errorf("failed to create entityByAlias request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1583` — `return fmt.Errorf("entityByAlias request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1589` — `return fmt.Errorf("entityByAlias returned status %d: %s", resp.StatusCode, string(body))`
- `test/e2e/scenarios/tiered_structural.go:1594` — `return fmt.Errorf("failed to read entityByAlias response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1614` — `return fmt.Errorf("failed to parse entityByAlias response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:1618` — `return fmt.Errorf("entityByAlias GraphQL error: %s", aliasResp.Errors[0].Message)`
- `test/e2e/scenarios/tiered_structural.go:1634` — `return fmt.Errorf("entityByAlias failed to resolve serial number %s - alias not indexed (check iot.sensor.serial predicate indexing)", serialNumber)`
- `test/e2e/scenarios/tiered_structural.go:1637` — `return errors.New("entityByAlias returned zero authority KV revision")`
- `test/e2e/scenarios/tiered_structural.go:1651` — `return fmt.Errorf("entityByAlias resolved to wrong entity: expected %s, got %s", expectedEntityID, entity.ID)`

### `test-predicate-list` (nil (all variants))
- `test/e2e/scenarios/tiered.go:355` — `{"test-predicate-list", s.executeTestPredicateList, nil},`
- `test/e2e/scenarios/tiered_structural.go:2009` — `func (s *TieredScenario) executeTestPredicateList(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:2023` — `return fmt.Errorf("failed to marshal predicates query: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2028` — `return fmt.Errorf("failed to create predicates request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2036` — `return fmt.Errorf("predicates request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2042` — `return fmt.Errorf("failed to read predicates response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2047` — `return fmt.Errorf("failed to parse predicates response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2051` — `return fmt.Errorf("predicates query error: %s", predicatesResp.Errors[0].Message)`
- `test/e2e/scenarios/tiered_structural.go:2082` — `return fmt.Errorf("no predicates found - graph may be empty or PREDICATE_INDEX not populated")`

### `test-predicate-stats` (nil (all variants))
- `test/e2e/scenarios/tiered.go:356` — `{"test-predicate-stats", s.executeTestPredicateStats, nil},`
- `test/e2e/scenarios/tiered_structural.go:2090` — `func (s *TieredScenario) executeTestPredicateStats(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_structural.go:2101` — `return fmt.Errorf("failed to create predicate list request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2107` — `return fmt.Errorf("predicate list request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2114` — `return fmt.Errorf("failed to parse predicate list response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2117` — `return fmt.Errorf("no predicates available for stats test")`
- `test/e2e/scenarios/tiered_structural.go:2138` — `return fmt.Errorf("failed to marshal predicateStats query: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2143` — `return fmt.Errorf("failed to create predicateStats request: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2151` — `return fmt.Errorf("predicateStats request failed: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2157` — `return fmt.Errorf("failed to read predicateStats response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2162` — `return fmt.Errorf("failed to parse predicateStats response: %w", err)`
- `test/e2e/scenarios/tiered_structural.go:2166` — `return fmt.Errorf("predicateStats query error: %s", statsResp.Errors[0].Message)`
- `test/e2e/scenarios/tiered_structural.go:2190` — `return fmt.Errorf("predicateStats(%q) reported 0 entities for a listed predicate", targetPredicate)`

### `test-predicate-compound` (nil (all variants))
- `test/e2e/scenarios/tiered.go:357` — `{"test-predicate-compound", s.executeTestPredicateCompound, nil},`
- `test/e2e/scenarios/tiered_structural.go:2249` — `func (s *TieredScenario) executeTestPredicateCompound(ctx context.Context, result *Result) error {`
  delegates its assertion to a shared helper:
- `test/e2e/scenarios/tiered_structural.go:2303` — `func validateCompoundPredicateCoverage(orMatched, andMatched int, andEntities []string, knownEntityID string) error {`
- `test/e2e/scenarios/tiered_structural.go:2305` — `return errors.New("compound predicate AND matched no entities; intersection coverage was not exercised")`
- `test/e2e/scenarios/tiered_structural.go:2308` — `return fmt.Errorf("set theory violation: AND (%d) > OR (%d)", andMatched, orMatched)`
- `test/e2e/scenarios/tiered_structural.go:2311` — `return fmt.Errorf("compound predicate AND omitted known fixture %s", knownEntityID)`

### `verify-search-quality` (['statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:369` — `{"verify-search-quality", s.executeVerifySearchQuality, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/validate_search.go:20` — `func (s *TieredScenario) executeVerifySearchQuality(ctx context.Context, result *Result) error {`
  delegates its assertion to a shared helper:
- `test/e2e/scenarios/validate_search.go:56` — `func (s *TieredScenario) searchQualityVerdict(variant string, stats *search.Stats) error {`
- `test/e2e/scenarios/validate_search.go:67` — `return fmt.Errorf("search failed for %d/%d queries: %s", len(broken), stats.TotalQueries, strings.Join(broken, "; "))`
- `test/e2e/scenarios/validate_search.go:70` — `return fmt.Errorf("known-answer search failed under BM25 (%d/%d passed): %s",`

### `test-http-gateway` (['statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:370` — `{"test-http-gateway", s.executeTestHTTPGateway, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/validate_infra.go:377` — `func (s *TieredScenario) executeTestHTTPGateway(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:385` — `return fmt.Errorf("GraphQL globalSearch strategy = %q, want %q", gqlResp.Data.GlobalSearch.Strategy, "graphrag")`

### `validate-gateway-response-shape` (['statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:377` — `{"validate-gateway-response-shape", s.executeValidateGatewayResponseShape, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/gateway_response_shape.go:85` — `func (s *TieredScenario) executeValidateGatewayResponseShape(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/gateway_response_shape.go:92` — `return fmt.Errorf("gateway shape probe %q: %w", probe.name, err)`
- `test/e2e/scenarios/gateway_response_shape.go:96` — `return fmt.Errorf("gateway shape probe %q: %w", probe.name, err)`
- `test/e2e/scenarios/gateway_response_shape.go:113` — `return fmt.Errorf(`

### `test-embedding-fallback` (['statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:378` — `{"test-embedding-fallback", s.executeTestEmbeddingFallback, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/validate_infra.go:398` — `func (s *TieredScenario) executeTestEmbeddingFallback(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:409` — `return fmt.Errorf("component query failed: %w", err)`
- `test/e2e/scenarios/validate_infra.go:433` — `return fmt.Errorf("graph-embedding not healthy (semembed_available=%v): neither BM25 fallback nor hybrid mode is working", semembedAvailable)`
- `test/e2e/scenarios/validate_infra.go:448` — `return fmt.Errorf("failed to connect for fallback test: %w", err)`

### `validate-community-structure` (['statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:383` — `{"validate-community-structure", s.executeValidateCommunityStructure, []string{"statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_statistical.go:399` — `func (s *TieredScenario) executeValidateCommunityStructure(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_statistical.go:401` — `return fmt.Errorf("NATS client not available for community structure validation")`
- `test/e2e/scenarios/tiered_statistical.go:409` — `return fmt.Errorf("failed to get communities: %w", err)`
- `test/e2e/scenarios/tiered_statistical.go:422` — `return fmt.Errorf("failed to read community summaries: %w", err)`
- `test/e2e/scenarios/tiered_statistical.go:445` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_statistical.go:489` — `return fmt.Errorf("no non-singleton communities found (%d total) - graph connectivity may be broken", totalCount)`
- `test/e2e/scenarios/tiered_statistical.go:501` — `result.Warnings = append(result.Warnings,`

### `validate-authoritative-hierarchy-provenance` (['structural', 'statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:389` — `{"validate-authoritative-hierarchy-provenance", s.validateAuthoritativeHierarchyProvenance, []string{"structural", "statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:1082` — `func (s *TieredScenario) validateAuthoritativeHierarchyProvenance(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:1084` — `return fmt.Errorf("NATS client unavailable for authoritative provenance validation")`
- `test/e2e/scenarios/tiered_semantic.go:1090` — `return fmt.Errorf("check retired context bucket absence: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:1093` — `return fmt.Errorf("retired CONTEXT_INDEX bucket exists on the fresh tier stack")`
- `test/e2e/scenarios/tiered_semantic.go:1098` — `return fmt.Errorf("scan authoritative hierarchy provenance: %w", err)`

### `validate-incoming-index-predicates` (['structural', 'statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:391` — `{"validate-incoming-index-predicates", s.validateIncomingIndexPredicates, []string{"structural", "statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:1144` — `func (s *TieredScenario) validateIncomingIndexPredicates(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:1146` — `return fmt.Errorf("NATS client unavailable for incoming index validation")`
- `test/e2e/scenarios/tiered_semantic.go:1154` — `return fmt.Errorf("failed to get entity IDs for incoming index validation: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:1157` — `return fmt.Errorf("no entities found for incoming index validation")`
- `test/e2e/scenarios/tiered_semantic.go:1179` — `return fmt.Errorf("no .group container entity found for incoming index validation")`
- `test/e2e/scenarios/tiered_semantic.go:1185` — `return fmt.Errorf("incoming entries query failed for %s: %w", containerID, err)`
- `test/e2e/scenarios/tiered_semantic.go:1201` — `return fmt.Errorf("container %s exists but INCOMING_INDEX returned 0 incoming edges — sharded reader/format drift (gh#474)", containerID)`
- `test/e2e/scenarios/tiered_semantic.go:1246` — `return fmt.Errorf("IncomingIndex has %d entries for %s but none have predicates - index may use old []string format", len(entries), containerID)`

### `validate-bidirectional-traversal` (['structural', 'statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:395` — `{"validate-bidirectional-traversal", s.validateBidirectionalTraversal, []string{"structural", "statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:1257` — `func (s *TieredScenario) validateBidirectionalTraversal(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:1259` — `return fmt.Errorf("NATS client unavailable for bidirectional traversal")`
- `test/e2e/scenarios/tiered_semantic.go:1267` — `return fmt.Errorf("failed to get entity IDs for bidirectional traversal: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:1270` — `return fmt.Errorf("no entities found for bidirectional traversal")`
- `test/e2e/scenarios/tiered_semantic.go:1288` — `return fmt.Errorf("no .group container entity found for bidirectional traversal")`
- `test/e2e/scenarios/tiered_semantic.go:1294` — `return fmt.Errorf("incoming entries query failed for %s: %w", containerID, err)`
- `test/e2e/scenarios/tiered_semantic.go:1308` — `return fmt.Errorf("failed to get outgoing entries for %s: %w", containerID, err)`
- `test/e2e/scenarios/tiered_semantic.go:1341` — `return fmt.Errorf("container %s has %d incoming edges but none is hierarchy.type.member", containerID, len(incomingEntries))`

### `validate-inverse-edges-materialized` (['structural', 'statistical', 'semantic'])
- `test/e2e/scenarios/tiered.go:397` — `{"validate-inverse-edges-materialized", s.validateInverseEdgesMaterialized, []string{"structural", "statistical", "semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:1349` — `func (s *TieredScenario) validateInverseEdgesMaterialized(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:1351` — `return fmt.Errorf("NATS client unavailable for inverse edges validation")`
- `test/e2e/scenarios/tiered_semantic.go:1359` — `return fmt.Errorf("failed to get entity IDs for inverse edges validation: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:1362` — `return fmt.Errorf("no entities found for inverse edges validation")`
- `test/e2e/scenarios/tiered_semantic.go:1380` — `return fmt.Errorf("no .group container entity found for inverse edges validation")`
- `test/e2e/scenarios/tiered_semantic.go:1386` — `return fmt.Errorf("outgoing entries query failed for %s: %w", containerID, err)`
- `test/e2e/scenarios/tiered_semantic.go:1402` — `return fmt.Errorf("incoming entries query failed for %s: %w", containerID, err)`
- `test/e2e/scenarios/tiered_semantic.go:1457` — `return fmt.Errorf("inverse edges asymmetric for %s: %d member edges vs %d contains edges", containerID, memberCount, containsCount)`

### `validate-globalsearch-known-answer` (['semantic'])
- `test/e2e/scenarios/tiered.go:413` — `{"validate-globalsearch-known-answer", s.executeValidateGlobalSearchKnownAnswer, []string{"semantic"}},`
- `test/e2e/scenarios/tiered_semantic_known_answer.go:79` — `func (s *TieredScenario) executeValidateGlobalSearchKnownAnswer(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic_known_answer.go:82` — `result.Warnings = append(result.Warnings,`
- `test/e2e/scenarios/tiered_semantic_known_answer.go:115` — `return fmt.Errorf("globalSearch known-answer assertion failed for %d/%d probes: %s",`

### `validate-batch-read-reconciliation` (['semantic'])
- `test/e2e/scenarios/tiered.go:419` — `{"validate-batch-read-reconciliation", s.executeValidateBatchReadReconciliation, []string{"semantic"}},`
- `test/e2e/scenarios/validate_batch_read.go:88` — `func (s *TieredScenario) executeValidateBatchReadReconciliation(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_batch_read.go:90` — `return fmt.Errorf("batch-read reconciliation requires the NATS validation client, which was not initialized")`
- `test/e2e/scenarios/validate_batch_read.go:100` — `return fmt.Errorf("gh#597 counter unverifiable: %w", err)`
- `test/e2e/scenarios/validate_batch_read.go:103` — `return fmt.Errorf("gh#597 counter unverifiable: %s subsystem not scraped — graph-ingest metrics missing", batchMissingSubsystem)`
- `test/e2e/scenarios/validate_batch_read.go:116` — `return fmt.Errorf("batch-read reconciliation needs at least %d present source entities, ENTITY_STATES has %d", minPresent, len(presentIDs))`

### `validate-virtual-edges` (['semantic'])
- `test/e2e/scenarios/tiered.go:427` — `{"validate-virtual-edges", s.executeValidateVirtualEdges, []string{"semantic"}},`
- `test/e2e/scenarios/tiered_semantic.go:725` — `func (s *TieredScenario) executeValidateVirtualEdges(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/tiered_semantic.go:727` — `return fmt.Errorf("NATS client not available for virtual edge validation")`
- `test/e2e/scenarios/tiered_semantic.go:741` — `return fmt.Errorf("failed to count virtual edges: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:748` — `return fmt.Errorf("failed to get auto-applied anomaly count: %w", err)`
- `test/e2e/scenarios/tiered_semantic.go:781` — `return fmt.Errorf("anomalies marked auto_applied (%d) but no virtual edges found in PREDICATE_INDEX", autoApplied)`

### `wait-for-rule-stabilization` (nil (all variants))
- `test/e2e/scenarios/tiered.go:431` — `{"wait-for-rule-stabilization", s.executeWaitForRuleStabilization, nil},`
- `test/e2e/scenarios/validate_infra.go:746` — `func (s *TieredScenario) executeWaitForRuleStabilization(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:752` — `return fmt.Errorf("failed to get initial rule metrics: %w", err)`

### `validate-rules` (nil (all variants))
- `test/e2e/scenarios/tiered.go:437` — `{"validate-rules", s.executeValidateRules, nil},`
- `test/e2e/scenarios/validate_infra.go:474` — `func (s *TieredScenario) executeValidateRules(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:479` — `return fmt.Errorf("failed to capture baseline rule metrics: %w", err)`
- `test/e2e/scenarios/validate_infra.go:497` — `return fmt.Errorf("failed to read final rule metrics: %w", err)`
- `test/e2e/scenarios/validate_infra.go:503` — `return fmt.Errorf("rule engine metrics missing: found %d of 3", foundCount)`
- `test/e2e/scenarios/validate_infra.go:506` — `return fmt.Errorf("rule engine performed no evaluations")`
- `test/e2e/scenarios/validate_infra.go:509` — `return fmt.Errorf("rule firings %d < MinRuleFirings %d", firings, s.config.MinRuleFirings)`
- `test/e2e/scenarios/validate_infra.go:512` — `return fmt.Errorf("actions dispatched %d < MinActionsDispatched %d", actions, s.config.MinActionsDispatched)`

### `validate-metrics` (nil (all variants))
- `test/e2e/scenarios/tiered.go:438` — `{"validate-metrics", s.executeValidateMetrics, nil},`
- `test/e2e/scenarios/validate_infra.go:653` — `func (s *TieredScenario) executeValidateMetrics(_ context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:659` — `return fmt.Errorf("metrics endpoint unreachable: %w", err)`
- `test/e2e/scenarios/validate_infra.go:665` — `return fmt.Errorf("metrics endpoint returned status %d", resp.StatusCode)`
- `test/e2e/scenarios/validate_infra.go:672` — `return fmt.Errorf("failed to read metrics: %w", err)`
- `test/e2e/scenarios/validate_infra.go:722` — `return fmt.Errorf("missing required metrics: %v", missingRequired)`

### `verify-outputs` (nil (all variants))
- `test/e2e/scenarios/tiered.go:439` — `{"verify-outputs", s.executeVerifyOutputs, nil},`
- `test/e2e/scenarios/validate_infra.go:213` — `func (s *TieredScenario) executeVerifyOutputs(ctx context.Context, result *Result) error {`
- `test/e2e/scenarios/validate_infra.go:217` — `return fmt.Errorf("component query failed: %w", err)`
- `test/e2e/scenarios/validate_infra.go:249` — `return fmt.Errorf("missing outputs: %v", missingOutputs)`


### B. Model-producer facts (item 2): which read/wait/timed values come from a model, not framework code

**Embedding model (semembed).** Config: `semembed` endpoint, model `all-MiniLM-L6-v2`, capability `embedding`.
Read/waited-on by `wait-for-embeddings` and `validate-embedding-queue-health` (embedding queue depth/health, semembed
HTTP health check) and by `verify-search-quality` / `validate-batch-read-reconciliation`'s ranked-score arm
(embedding similarity ranking).

- `configs/semantic.json:33` — `      "semembed": {`
- `configs/semantic.json:36` — `        "model": "all-MiniLM-L6-v2",`
- `configs/semantic.json:45` — `      "embedding":             { "preferred": ["semembed"] }`
- `test/e2e/scenarios/validate_infra.go:256` — `func (s *TieredScenario) checkSemembedHealth(result *Result) bool {`

**Answer model (seminstruct, qwen3).** Config capability `answer_synthesis` → `seminstruct-mid-answer`
(`qwen3-1.7b` in `configs/semantic.json`, `qwen3-8b` in `configs/semantic-8b.json`). Consumed by
`graph.query.globalSearch`'s answer synthesis — read by `validate-thematic-answer-eval`'s `globalSearchDirect` and
`validate-globalsearch-known-answer`'s `sendGlobalSearchAtLevel`, and (per the includeSummaries/summarizeThreshold
defaults — section E) by `test-http-gateway`'s request unless it opts out.

- `configs/semantic.json:42` — `      "answer_synthesis":      { "preferred": ["seminstruct-mid-answer"] },`
- `configs/semantic.json:24` — `        "model": "qwen3-1.7b",`
- `configs/semantic-8b.json:26` — `        "model": "qwen3-8b",`
- `test/e2e/scenarios/validate_thematic_eval.go:300` — `func (s *TieredScenario) globalSearchDirect(ctx context.Context, query string, level int) (*globalSearchDirectResponse, time.Duration, error) {`
- `test/e2e/scenarios/tiered_semantic_known_answer.go:331` — `func (s *TieredScenario) sendGlobalSearchAtLevel(ctx context.Context, gatewayURL, query string, level int) (*knownAnswerResponse, time.Duration, error) {`

**Community summarizer (seminstruct, qwen3, via `graph/clustering.LLMSummarizer`).** Config capability
`community_summary` → `seminstruct-mid-summary`. Read/waited-on by `validate-llm-enhancement`'s
`waitForLLMEnhancement`/`validateLLMSummaryQuality` and by `validate-community-structure`'s summary-read arm.

- `configs/semantic.json:43` — `      "community_summary":     { "preferred": ["seminstruct-mid-summary"] },`
- `graph/clustering/summarizer.go:492` — `// LLMSummarizer implements CommunitySummarizer using an OpenAI-compatible LLM API.`
- `test/e2e/scenarios/tiered_semantic.go:197` — `func (s *TieredScenario) waitForLLMEnhancement(`
- `test/e2e/scenarios/tiered_semantic.go:366` — `func (s *TieredScenario) validateLLMSummaryQuality(`

**LLM query classifier (`graph/query.LLMClassifier`) — wired in code, unreachable via any shipped config.**
`initLLMClassifier` (called from `Start()`) resolves model-registry capability `query_classification`
(`model.CapabilityQueryClassification`); on any resolve error it silently stays keyword-only. No file under
`configs/` defines a `"query_classification"` capability — `configs/semantic.json` (and the `-8b`/`-frontier`
overlays) define `"intent_classification"` instead, which is a *different* capability
(`model.CapabilityIntentClassification`) consumed by `processor/agentic-dispatch`, not `graph-query`. So in every
shipped semantic config, `graph-query`'s classifier chain is `NewClassifierChain(NewKeywordClassifier(), nil, nil)` —
keyword-only, no embedding classifier, no LLM classifier — contradicting the in-tree comment that describes an
"LLM classifier" fallback for the non-keyword NL-temporal-intent probe (which, note, is itself excluded from the
semantic-variant surface — see section A's exclusion list).

- `model/registry.go:22` — `	// CapabilityQueryClassification is used by graph-query for LLM-based query classification.`
- `model/registry.go:23` — `	CapabilityQueryClassification = "query_classification"`
- `model/registry.go:31` — `	CapabilityIntentClassification = "intent_classification"`
- `processor/graph-query/component.go:422` — `func (c *Component) initLLMClassifier() {`
- `processor/graph-query/component.go:426` — `	resolved, ep, err := model.ResolveEndpointWithConfig(c.modelRegistry, model.CapabilityQueryClassification)`
- `processor/graph-query/component.go:428` — `		// No query_classification capability configured — keyword-only is fine`
- `processor/graph-query/component.go:442` — `	llmClassifier := query.NewLLMClassifier(adapter, nil)`
- `processor/graph-query/component.go:443` — `	c.classifier = query.NewClassifierChain(query.NewKeywordClassifier(), nil, llmClassifier)`
- `processor/graph-query/component.go:274` — `		classifier:           query.NewClassifierChain(query.NewKeywordClassifier(), nil, nil),`
- `configs/semantic.json:41` — `      "intent_classification": { "preferred": ["seminstruct-fast"] },`
- `graph/query/classifier_llm.go:74` — `func (c *LLMClassifier) ClassifyQuery(ctx context.Context, query string) (*ClassificationResult, error) {`
- `test/e2e/scenarios/tiered.go:346` — `		// LLM classifier on the answer model (measured 17.5 s cold, 5.6 s warm against`

### C. Wall-clock measurements (item 3) — GitHub Actions job logs, not in-repo pins

Job `e2e semantic (measurement, gh#1117)`, workflow `.github/workflows/e2e-ladder.yml`, two per-job tier runs
(`task e2e:semantic` invoked twice: cold then warm). Fetched via `gh run view --job <id> --log`.

**Run 36589370091 (2026-09-29, job id 109478124769) — 48 stages** (pre-#1427: still includes `test-graphrag-local`,
`test-graphrag-global`, `test-nl-temporal-intent`, `validate-anomaly-detection`, none of which are in the current
44-stage semantic surface — see Adjacent claims for the commit that removed them):

| stage | cold (run 1) | warm (run 2) |
|---|---|---|
| validate-llm-enhancement | 2m27.918s | 2m28.450s |
| validate-thematic-answer-eval | 5m45.134s | 5m21.856s |
| validate-globalsearch-known-answer | 1m25.174s | 1m49.695s |
| test-http-gateway | 56.390s | 55.407s |
| validate-community-structure | 28.202s | 21.921s |
| test-nl-path-intent | 30.015s | 30.006s |

first_run_seconds=878, second_run_seconds=844 (whole-job wall clock incl. `docker compose up --wait`).

**Run 36654106894 (2026-09-30, headSha `2430ebd1` — this worktree's exact base) — 44 stages**, matching the
current stage table exactly:

| stage | cold (run 1) | warm (run 2) |
|---|---|---|
| validate-llm-enhancement | 2m28.425s | 2m27.918s |
| validate-thematic-answer-eval | 4m43.976s | 4m47.953s |
| validate-globalsearch-known-answer | 55.860s | 1m24.196s |
| test-http-gateway | 18.734s | 27.514s |
| validate-community-structure | 26.932s | 14.087s |
| test-nl-path-intent | 0.085s | 0.096s |

first_run_seconds=682, second_run_seconds=677.

**Surprise: `test-http-gateway` dropped from ~56s to ~18-27s and `test-nl-path-intent` from ~30s to ~0.09s between
the two runs** — both are pre-existing-vs-post-#1427 comparisons (PR #1427/#1426 changed which requests these stages
send), not a measurement artifact; see Adjacent claims. The three named quality stages did **not** drop: they account
for 8m08s (cold) / 8m40s (warm) of the 9m25s / 8m57s scenario wall clock in the newer run (the 682 s / 677 s figures
are whole `task e2e:semantic` times, compose up included; `taskfiles/e2e/semantic.yml:8`'s "9.6 min" is the older
run's figure).

### D. Skip/shorten mechanisms per variant or env (item 4)

Exhaustive `os.Getenv`/`os.LookupEnv` under `test/e2e/` and `cmd/e2e/` (8 hits total; see `## Searches`): none of them
gate stage table membership. Two gate wait/timeout durations, both fail-safe-to-default on unset/malformed:

- `test/e2e/scenarios/globalsearch_timeout.go:20` — `const globalSearchTimeoutEnv = "SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT"`
- `test/e2e/scenarios/globalsearch_timeout.go:25` — `func globalSearchClientTimeout(def time.Duration) time.Duration {`
- `test/e2e/scenarios/globalsearch_timeout.go:34` — `const llmEnhancementWaitEnv = "SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT"`
- `test/e2e/scenarios/globalsearch_timeout.go:37` — `func llmEnhancementWait(def time.Duration) time.Duration {`
- `test/e2e/scenarios/http_gateway_readiness.go:149` — `	httpClient := &http.Client{Timeout: globalSearchClientTimeout(60 * time.Second)}`
- `test/e2e/scenarios/tiered_semantic.go:211` — `		ctx, communities, llmEnhancementWait(2*time.Minute), 2*time.Second,`

`taskfiles/e2e/semantic.yml` sets these only in the `:8b` and `:frontier` (non-CI, opt-in/pre-tag) variants — the
default `semantic` task (the one the ladder's measurement job runs) sets neither, so it always uses the 60s/2min
defaults:

- `taskfiles/e2e/semantic.yml:21` — `      - cd cmd/e2e && ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`
- `taskfiles/e2e/semantic.yml:45` — `      - cd cmd/e2e && SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT=300s SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT=10m ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`
- `taskfiles/e2e/semantic.yml:63` — `      - cd cmd/e2e && SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT=180s SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT=5m ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`

`docker/compose/tiered.8b.yml`'s overlay separately raises a *server-side* request-handler budget (120s → 300s) for
8B inference — a different knob than the two client-side env vars above (not re-verified line-by-line; found via
`taskfiles/e2e/semantic.yml`'s own comment, NOT RUN against the compose file itself).

The E2E Ladder's `e2e-semantic-measure` job sets **no** env override — it runs `task e2e:semantic` (the bare default)
twice, so every run measured in section C used the 60s/2min defaults, cold and warm alike:

- `.github/workflows/e2e-ladder.yml:134` — `    name: e2e semantic (measurement, gh#1117)`
- `.github/workflows/e2e-ladder.yml:180` — `          task e2e:semantic`
- `.github/workflows/e2e-ladder.yml:186` — `          task e2e:semantic`

### E. `test-http-gateway`'s globalSearch request and includeSummaries/summarizeThreshold (item 5)

The stage's GraphQL document never sets `includeSummaries` or `summarizeThreshold`:

- `test/e2e/scenarios/http_gateway_readiness.go:102` — `func gatewayGlobalSearchQuery() map[string]any {`
- `test/e2e/scenarios/http_gateway_readiness.go:104` — `query($query: String!, $level: Int, $maxCommunities: Int) {`
- `test/e2e/scenarios/validate_infra.go:377` — `func (s *TieredScenario) executeTestHTTPGateway(ctx context.Context, result *Result) error {`

Server-side defaults (unset ⇒ these apply) in the request handler:

- `processor/graph-query/graphrag.go:54` — `	DefaultSummarizeThreshold = 50`
- `processor/graph-query/graphrag.go:164` — `Auto-summarize when results exceed this (default: 50, non-positive=disabled)`
- `processor/graph-query/graphrag.go:165` — `Include community summaries (default: true)`
- `processor/graph-query/graphrag.go:181` — `func (r *GlobalSearchRequest) shouldIncludeSummaries() bool {`
- `processor/graph-query/graphrag.go:172` — `func (r *GlobalSearchRequest) getSummarizeThreshold() int {`
- `processor/graph-query/graphrag.go:862` — `		needsCommunity := req.shouldIncludeSummaries() || req.IncludeSources || autoSummarize`

The semantic tier's fixture loads 74 entities (`test/e2e/scenarios/validate_infra.go` fallback constant, see
Consumers) — above `DefaultSummarizeThreshold=50` — so with both flags left at default, `test-http-gateway`'s
request both auto-summarizes AND includes summaries, i.e. it drives community-summary + answer-synthesis on every
run, not just a path check. Owner-recorded residual and candidate fix quoted verbatim below (Adjacent claims).

Correction (coordinator, 2026-09-30, from the design read): the auto-summarize clause does not fire. Both measured
runs report `graphql_gateway_search_hits:30`, below `DefaultSummarizeThreshold=50` (`processor/graph-query/graphrag.go:54`,
`:861`), so the synthesis on every run comes from `includeSummaries` alone: `enrichGlobalResponse`
(`graphrag.go:943-947`) → `synthesizeQueryAnswer` (`graphrag.go:2133-2156`). The conclusion stands (the stage drives
answer synthesis on every run); the mechanism is the summaries flag, not the threshold.

Contrast: the NL-intent stages in this same surface deliberately opt out —

- `test/e2e/scenarios/tiered_structural.go:1696` — `query($query: String!, $maxCommunities: Int, $includeSummaries: Boolean) {`
- `test/e2e/scenarios/tiered_structural.go:1710` — `			"includeSummaries": false,`

## Adjacent claims

**Specs** (item 7) — `openspec/specs/` entries whose name or Purpose mentions e2e/ladder/tier/CI/gate:

- `openspec/specs/e2e-tiered-scenario/spec.md:8` — `which stages the semantic path-only run skips is #1117's.`
- `openspec/specs/release-candidate-proof/spec.md:70` — `required gate is green SHALL`
  (adjacent: the RC/tag gate, not the per-PR ladder — weaker match, found via the same keyword sweep)

**ADRs, issues, PRs:**

- #1117 — e2e/ci: the default semantic variant is already the small-model CI gate but runs in no workflow (OPEN, milestone v1.0.0-beta.163, labels enhancement/area:ci/area:e2e/horizon:pre-v1/class:e2e-gap)
- #1425 — draft PR, `ci(e2e): measure the default semantic variant on a CI runner (#1117)` (OPEN; this worktree's branch)
- #1427 — `test(e2e): the tiered scenario's warn-only stages assert or leave the per-PR variant (#1426)` (MERGED `0121a535`; this is the commit that removed `validate-anomaly-detection` and restricted `test-graphrag-local`/`test-graphrag-global`/`test-nl-temporal-intent` away from the semantic variant — the reason run 36589370091 (48 stages) and run 36654106894 (44 stages) differ)
- #1426 — seeds the `e2e-tiered-scenario` capability's warn-only-stage requirement (referenced in the spec and the proposal; not independently re-read here)
- #830 (closed) — the globalSearch known-answer probe timeout precedent cited by #1117 and the proposal
- #769 — nightly `e2e:semantic` + `e2e:agentic`; explicitly declined the per-PR half (Non-goals)
- #643 — semantic-tier cache-control seam for determinism (Non-goals)
- #1222 — shared selected-required-check evidence contract; assertion accounting is its lane, not this change's (Non-goals)

**The dangling TODO (item 6) — already deleted on this branch, not yet on main:**

- `git log -S "TODO (end of pre-v1 mega-plan cleanup)" -- .github/workflows/e2e-ladder.yml` → commit `a28fceb3` introduced it (2026, docs #1127); commit `614a4e65` (`ci(e2e): measure the default semantic variant on a CI runner (#1117)`, this branch) replaced it with the current comment block.
- `614a4e65` is on `claude/gh1117-semantic-gate` (`git log --oneline -- .github/workflows/e2e-ladder.yml` on this branch shows it at HEAD-1); it is **not** in `git log --oneline main -- .github/workflows/e2e-ladder.yml` (main's most recent touch to this file is `bfe043bb`) — the TODO deletion has landed on PR #1425 (still draft/open), not on main yet.
- `.github/workflows/e2e-ladder.yml:26` — `#     Owner ruling (2026-08-27, #1117): wiring it here is PER-PR, not`
- `.github/workflows/e2e-ladder.yml:29` — `#     which assertions the per-PR gate carries (path vs quality), not`

**#1117 comment, 2026-09-29T16:42:04Z (Fable session, "a finding, not a ruling") — the `test-http-gateway` residual,
quoted verbatim (the source for section E and the proposal's candidate fix):**

> Measured residual on this issue's own path, 2026-09-29 — `test-http-gateway` is a quality cost wearing a path
> stage's name in the semantic variant. [...] the stage's single globalSearch request took 56.4 s, 55.4 s, 35.5 s,
> and > 60 s; `graphql_gateway_readiness_wait_ms` equals `graphql_gateway_latency_ms` and
> `graphql_gateway_index_not_ready_retries` is 0 every time, so the time is one request's server latency, not a
> readiness poll. Under the statistical variant the same stage takes 1.5 ms. [...] Under the semantic variant
> globalSearch answers through the small model, so this stage's cost and its one-in-four red belong to the model
> [...] the path-only run must either post a gateway query that does not synthesize (any served query proves the
> gateway path) or carry a budget set from measurement with margin.

**test/e2e/README.md and docs/contributing/02-e2e-tests.md also carry the #1117 target, as prose (not re-derived
here beyond the pin):**

- `test/e2e/README.md:220` — `Owner-ruled target (2026-08-27, [#1117](https://github.com/C360Studio/semstreams/issues/1117)): the default`
- `docs/contributing/02-e2e-tests.md:354` — `PR; the per-PR gate is gh#1117, the nightly run gh#769.`

## Consumers

Where a run's outcome is persisted (no config or `Details` reaches disk; the log is the observation surface):

- `test/e2e/results/writer.go:32` — `type TestRunConfig struct {`
- `test/e2e/scenarios/results.go:738` — `func SaveStructuredResults(tr *TieredResults, outputDir string) (string, error) {`
- `cmd/e2e/main.go:546` — `	if flags.outputDir != "" - `cmd/e2e/main.go:545` — `	if flags.outputDir != "" && result.Structured != nil {`- `cmd/e2e/main.go:545` — `	if flags.outputDir != "" && result.Structured != nil {` result.Structured != nil {`

`test-http-gateway`'s only assertion, and the zero-entity fallback it cannot see:

- `test/e2e/scenarios/validate_infra.go:388` — `	hitCount := len(gqlResp.Data.GlobalSearch.Entities)`
- `processor/graph-query/graphrag.go:924` — `		entities, loadErr := c.loadEntities(ctx, entityIDs)`
- `processor/graph-query/graphrag.go:1391` — `			Strategy:   "graphrag",`
- `processor/graph-query/graphrag.go:898` — `			labels, labelErr := c.resolveEntityLabels(ctx, entityIDs)`

Readers of `getStagesForVariant` / the stage table / `TieredConfig.Variant` / the `--variant` CLI flag:

- `test/e2e/scenarios/tiered.go:599` — `stages := s.getStagesForVariant(variant)`
  (the only in-tree caller)
- `cmd/e2e/main.go:136` — `flag.StringVar(&flags.variant, "variant", "",`
  (the CLI flag every taskfile line below sets)
- `taskfiles/e2e/semantic.yml:21` — `      - cd cmd/e2e && ./e2e --scenario tiered --variant semantic --base-url http://localhost:38180 --udp-endpoint localhost:34650 --metrics-url http://localhost:39190 --output-dir ./test/e2e/results`
- `taskfiles/e2e/statistical.yml:20` — `      - cd cmd/e2e && ./e2e --scenario tiered --variant statistical --output-dir ./test/e2e/results`
- `taskfiles/e2e/structural.yml:20` — `      - cd cmd/e2e && ./e2e --scenario tiered --variant structural --output-dir ./test/e2e/results`
- `.github/workflows/e2e-ladder.yml:180` — `task e2e:semantic`
  (the only per-PR-ladder reader of the semantic variant today, and it is the measurement-only job the proposal deletes)

The 74-entity fixture size that puts `test-http-gateway` over `DefaultSummarizeThreshold=50` (section E):

- `test/e2e/scenarios/validate_infra.go:365` — `	return 74 // Kitchen sink dataset`

## Problem shape

The closest existing instance of "assert a classified signal, hard-fail only on the framework/transport case, never
on the content case" — the shape #1117's path/quality split is asking for — already exists on an unrelated plane,
batch-read hydration (ADR-062 fusion), not model quality:

- `test/e2e/scenarios/validate_batch_read.go:296` — `		return fmt.Errorf("assertion-1: absent %q reported with reason %q, want %q", absentID, absentReason, graph.MissingNotFound)`
- `test/e2e/scenarios/validate_batch_read.go:394` — `		return fmt.Errorf("assertion-2: absent %q reported unhydrated reason %q, want %q (closed fusion.UnhydratedReason set)", absentID, u.Reason, fusion.UnhydratedNotFound)`

There, the framework asserts hard on the *shape* of the classification (a closed reason enum, exactly-once
reconciliation) while the *content* a reason attaches to (which entities are actually absent) is a data fact, not a
framework defect — structurally the same split #1117 wants between "the model answered" (path, hard-fail) and "the
model answered well" (quality, record-only).

## Searches

- `git rev-parse HEAD` → `2430ebd15ffb4cfa1cbe0c2f1cc45fa18ed294e4`
- `sed -n '3,79p' openspec/project.md` (Purpose + Product Boundary) → read
- `grep -n "Variant\|variant\|semantic\|Stage{" test/e2e/scenarios/tiered.go` → 40+ hits (stage-table scan)
- `sed -n '247,441p' test/e2e/scenarios/tiered.go` → full `getStagesForVariant` body read
- `awk '/allStages := \[\]stage\{/,/^\t\}$/' test/e2e/scenarios/tiered.go | grep -c '^\s*{"'` → 54
- `git grep -n '^		{"' -- test/e2e/scenarios/tiered.go` → 54 (real line numbers for every stage row)
- `git grep -n "func (s \*TieredScenario) <name>(" -- test/e2e/scenarios/*.go` × 43 names (one combined shell loop) → 43 hits, all found
- `git grep -n '^func ' -- <13 scenario files>` → 182 hits (function boundary map)
- `git grep -n -E 'return (fmt\.Errorf|errors\.New)|Warnings = append|result\.Warnings\b' -- <13 scenario files>` → 303 hits (assertion-site map)
- python correlation of the two greps above by (file, line-range) → per-stage assertion buckets (item 1)
- `git grep -n "qwen3\|seminstruct" -- configs/*.json` → 20+ hits
- `sed -n '1,40p' graph/query/classifier.go` → KeywordClassifier only, no LLM in this file
- `git grep -n "community_summary\|CommunitySummar" -- '*.go'` → 20 hits (LLMSummarizer at graph/clustering/summarizer.go:492)
- `git grep -n "Classifier)" -- 'graph/query/*.go'` → found `LLMClassifier`, `EmbeddingClassifier`, `ClassifierChain`
- `git grep -n "NewClassifierChain\|NewLLMClassifier\|NewEmbeddingClassifier" -- '*.go'` → 9 hits (wiring call sites)
- `sed -n '420,450p' processor/graph-query/component.go` → `initLLMClassifier` gating logic read
- `git grep -n "CapabilityQueryClassification " -- '*.go'` → 3 hits (constant + doc comment + one other reference)
- `grep -n "intent_classification" configs/structural.json configs/statistical.json configs/semantic.json` → only semantic.json (structural/statistical have no ML capabilities block at all)
- `grep -rn "\"query_classification\"" configs/` → **0 hits** (the capability graph-query's LLM classifier reads is configured nowhere)
- `sed -n '15,35p' model/registry.go` → both capability constants read side by side, confirming they are distinct
- `gh run view 36589370091 --json jobs -q '...'` → job id 109478124769, conclusion success
- `gh run view --job 109478124769 --log` → 2048-line log saved; `grep -c '\[.*/48\]'` → 192 (2 runs × 48 stages × 2 lines)
- `grep -n '\[.*/48\] .* starting'` / `... completed in` → full 48-stage ordered list + timings, both runs
- prior-session artifact `measure-36654106894.log` in this session's scratchpad, confirmed via `gh run view 36654106894 --json status,conclusion,headSha,createdAt` → headSha `2430ebd1...` (this worktree's exact base), conclusion success
- `grep -oE '\[[0-9]+/[0-9]+\]' measure-36654106894.log | sed ... | sort -u` → `44` only (confirms post-#1427 stage count)
- `grep -n '\[.*/44\] .* completed in'` → full 44-stage timings, both runs
- `grep -n 'MEASURE\] first_run_seconds\|second_run_seconds\|pull_seconds'` on both logs → 878/844s (old run), 682/677s (new run)
- `git grep -n "os\.Getenv\|os\.LookupEnv" -- test/e2e cmd/e2e | grep -v _test` → 7 hits (all named in section D / Claimed gap)
- `sed -n '1,60p' test/e2e/scenarios/globalsearch_timeout.go` → both env-override functions read in full
- `git grep -n "globalSearchClientTimeout\|llmEnhancementWait(" -- test/e2e` → 5 call sites
- `cat taskfiles/e2e/semantic.yml` (first 120 lines) → all five sub-tasks (`default`, `8b`, `frontier`, `debug`, `fallback`) read
- `grep -n "semantic" .github/workflows/e2e-ladder.yml` → 9 hits; `sed -n '1,35p'` and `'110,200p'` of the same file read in full
- `grep -n 'MEASURE\].*run_seconds\|pull_seconds'` → whole-job timings (above)
- `sed -n '360,400p' test/e2e/scenarios/validate_infra.go` → `executeTestHTTPGateway` and its neighbor read
- `git grep -n "includeSummaries|IncludeSummaries|summarizeThreshold|SummarizeThreshold" -- '*.go' | grep -v _test` → 25 hits
- `sed -n '40,110p' test/e2e/scenarios/http_gateway_readiness.go` → the readiness wait, the query builder, the timeout constants read in full
- `gh api repos/C360Studio/semstreams/issues/1117/comments --paginate -q '...'` filtered to `2026-09-29T16:4*` → 1 hit (quoted above)
- `git grep -n -i 'TODO.*semantic\|semantic.*TODO' -- . ':!openspec/changes/archive'` → 0 hits (no literal TODO-near-"semantic" string remains in tracked content)
- `git grep -n '1117' -- . ':!openspec/changes/archive'` → 20 hits (all reviewed; the dangling-TODO string is not among them — it was already deleted)
- `sed -n '1,35p' .github/workflows/e2e-ladder.yml` → confirms the replacement comment block, no literal TODO left
- `git log --oneline -S "TODO (end of pre-v1 mega-plan cleanup)" -- .github/workflows/e2e-ladder.yml` → 2 hits (`a28fceb3` added, `614a4e65` removed)
- `git log --oneline main -- .github/workflows/e2e-ladder.yml` → most recent is `bfe043bb`; `614a4e65` absent from main
- `git log --oneline -5 -- .github/workflows/e2e-ladder.yml` (this branch) → `614a4e65` present at HEAD-1
- `ls openspec/specs/ | grep -iE "e2e|ladder|tier|ci|gate"` → 5 dir names (2 relevant: e2e-tiered-scenario; the other 4 are `gate*`/`gateway*` false positives on substring "gate")
- `git grep -l -iE "e2e|ladder|\btier\b|\bCI\b|\bgate\b" -- 'openspec/specs/*/spec.md'` → 12 files
- `sed -n '/^## Purpose/,/^## /p' openspec/specs/e2e-tiered-scenario/spec.md openspec/specs/release-candidate-proof/spec.md` → both Purpose sections read in full
- `grep -n -iE "e2e|ladder|\btier\b|\bCI\b|\bgate\b" openspec/specs/release-candidate-proof/spec.md` → 5 hits (RC/tag gate, not the per-PR ladder — weak match)
- `ls openspec/changes/ | grep -i semantic` → 1 (`e2e-semantic-path-only-gate`, this change)
- `task openspec:queue` → 1 change in flight (this one, 0/0 tasks, no tasks.md yet)
- `wc -l` + `grep -n "^#"` on `proposal.md`/`README.md` → structure read; `sed -n '1,75p' proposal.md` → full proposal read
- `gh pr list --search "1117" --state all --json number,title,state,isDraft,body --limit 30` → 7 hits (2 on-point: #1425 open draft, #1427 merged)
- `gh issue view 1117 --json number,title,state,labels,milestone` → OPEN, beta.163
- `gh issue view 1117 --json body` → full body read (scope boxes, owner rulings, the exact dangling-TODO quote)

NOT RUN: `docker/compose/tiered.8b.yml`'s exact line for the 120s→300s request-handler budget claim (cited from
`taskfiles/e2e/semantic.yml`'s comment only, not independently re-verified against the compose file); a full
line-by-line assertion map for the 10 excluded (non-semantic) stage rows; `docker/compose/tiered.yml`'s `semantic`
profile service definitions themselves (only the images/models named in `configs/semantic.json` and issue #1117's
own table were pinned).

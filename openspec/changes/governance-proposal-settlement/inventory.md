# Inventory: governance-proposal-settlement admission and reload
base: 4039530ff6213a25b946332c876b6d1e58c86a04

## Claimed gap

- `openspec/changes/governance-proposal-settlement/proposal.md:5` — `A governance verdict publication failure can currently be logged while the rule consumer acknowledges its proposal.`
- `openspec/changes/governance-proposal-settlement/proposal.md:6` — `Changing ACK to NAK alone is insufficient: persisted message-match state can prevent the failed OnEnter action from`
- `openspec/changes/governance-proposal-settlement/proposal.md:24` — `The admission mechanism, hot-reload behavior, precise implementation boundary and settlement dispositions still`
- `openspec/changes/governance-proposal-settlement/proposal.md:25` — `require a bounded reviewed design and owner acceptance. This proposal does not approve their implementation.`

## Spellings of the fact

- `processor/rule/rule_loader.go:117` — `if err := ValidateDefinition(definition); err != nil {`
- `processor/rule/rule_loader.go:142` — `rp.initialRules = snapshot`
- `processor/rule/rule_loader.go:145` — `rp.initialRulesReady = true`
- `processor/rule/rule_loader.go:226` — `rule, err := CreateRuleFromDefinition(def, ruleDeps)`
- `processor/rule/rule_loader.go:232` — `continue // Skip invalid rules but continue loading others`
- `processor/rule/config_validation.go:500` — `parseActions := func(key string) []Action {`
- `processor/rule/config_validation.go:506` — `raw, err := json.Marshal(val)`
- `processor/rule/config_validation.go:508` — `return nil`
- `processor/rule/config_validation.go:511` — `if err := json.Unmarshal(raw, &actions); err != nil {`
- `processor/rule/config_validation.go:512` — `return nil`
- `processor/rule/config_validation.go:516` — `def.OnEnter = parseActions("on_enter")`
- `processor/rule/config_validation.go:519` — `def.OnRecovery = parseActions("on_recovery")`
- `processor/rule/config_validation.go:525` — `def.Actions = parseActions("actions")`
- `processor/rule/kv_config_integration.go:417` — `if err := ValidateDefinition(ruleDef); err != nil {`
- `processor/rule/kv_config_integration.go:425` — `_, err = kvStore.Put(ctx, key, data)`
- `processor/rule/kv_config_integration.go:491` — `entry, err := kvStore.Get(ctx, key)`
- `processor/rule/kv_config_integration.go:493` — `rcm.logger.Warn("Failed to load rule during ListRules; skipping",`
- `processor/rule/kv_config_integration.go:495` — `continue`
- `processor/rule/kv_config_integration.go:498` — `if err := json.Unmarshal(entry.Value, &def); err != nil {`
- `processor/rule/kv_config_integration.go:501` — `continue`
- `processor/rule/kv_config_integration.go:505` — `return rules, nil`
- `processor/rule/runtime_config.go:126` — `for ruleID, ruleConfig := range rulesMap {`
- `processor/rule/runtime_config.go:138` — `if err := rp.applyExpressionRuleChange(ctx, ruleID, ruleMap); err != nil {`
- `processor/rule/runtime_config.go:139` — `return err`
- `processor/rule/runtime_config.go:144` — `for ruleID := range currentRuleIDs {`
- `processor/rule/runtime_config.go:145` — `rp.removeRuleContext(ctx, ruleID)`
- `processor/rule/runtime_config.go:266` — `rp.rules[ruleID] = newRule`
- `processor/rule/runtime_config.go:267` — `rp.ruleDefinitions[ruleID] = def`
- `processor/rule/runtime_config.go:268` — `rp.ruleConfigs[ruleID] = ruleMap`
- `processor/rule/rule_factory.go:202` — `factory, exists := GetRuleFactory(def.Type)`
- `processor/rule/rule_factory.go:208` — `if err := factory.Validate(def); err != nil {`
- `processor/rule/rule_factory.go:213` — `return factory.Create(def.ID, def, deps)`
- `processor/rule/interfaces.go:35` — `Subscribe() []string`
- `processor/rule/interfaces.go:41` — `ExecuteEvents(messages []message.Message) ([]Event, error)`
- `processor/rule/expression_factory.go:112` — `subjects := []string{">"}`
- `processor/rule/expression_factory.go:114` — `subjects = nil`
- `processor/rule/expression_factory.go:123` — `subscribed:  subjects,`
- `processor/rule/expression_factory.go:329` — `func (r *ExpressionRule) ExecuteEvents(messages []message.Message) ([]Event, error) {`
- `processor/rule/expression_factory.go:353` — `event, err := gtypes.NewEntityUpdateEvent(entityID, properties, gtypes.EventMetadata{`
- `processor/rule/expression_factory.go:365` — `return []Event{event}, nil`
- `processor/agentic-loop/governance_dispatcher.go:115` — `CallID       string         `json:"call_id"``

## Adjacent claims

- `openspec/changes/governance-proposal-settlement/proposal.md:35` — ``agentic-loop-restart-safety/design-r7-rule-input-scope-2026-09-14.md`,`
- `openspec/changes/governance-proposal-settlement/proposal.md:36` — `SHA-256 `b72ce0bbbc6a76b7cea0455901690713a86b9724ba0bab676b19d6fea11db306`.`
- `openspec/changes/governance-proposal-settlement/proposal.md:40` — `- `inventory-r7-governance-evidence-2026-09-14.md`:`
- `openspec/changes/governance-proposal-settlement/proposal.md:41` — ``96d9c252c96d938a19629cf7c59da95da67815f6e4b9f5a2d612d2bffe65904a``
- `openspec/changes/governance-proposal-settlement/proposal.md:42` — `- `inventory-r7-rule-replay-2026-09-14.md`:`
- `openspec/changes/governance-proposal-settlement/proposal.md:43` — ``54e20f241c932b8e751ff9f193ba6dca1ff2e615312f2292e4cdada7e57939af``
- `openspec/changes/governance-proposal-settlement/proposal.md:45` — `Those inventories describe c347eff4 and its recorded working snapshot. They are local provenance, not a claim that`
- `openspec/changes/governance-proposal-settlement/proposal.md:56` — `- #1156 owns the unpublished semantic delivery types.`
- `openspec/changes/governance-proposal-settlement/proposal.md:57` — `- #1159 additionally owns the no-heartbeat SettleDelivery entry point and execution-ID/fingerprint propagation.`
- `openspec/changes/governance-proposal-settlement/proposal.md:58` — `- #1146 R8 owns the related publisher/PubAck correction.`
- `openspec/changes/governance-proposal-settlement/proposal.md:67` — `This claim does not absorb #935, #1043 or #1045, change general rule atomicity, retry projection mutations, or close`
- `openspec/specs/rule-projection-mutations/spec.md:87` — `NOT replay or recompute the old `ExecutionContext`. `commit_unknown` MUST NOT be automatically retried. Successful`
- `openspec/specs/rule-projection-mutations/spec.md:88` — `receipts MUST retain the exact committing revision. No retry helper, knob, loop, or coordinator is part of this`

## Consumers

- `processor/rule/processor.go:468` — `return rp.prepareInitialRules()`
- `processor/rule/processor.go:947` — `if err := rp.prepareInitialRules(); err != nil {`
- `processor/rule/processor.go:1005` — `if err := rp.setupSubscriptions(runCtx); err != nil {`
- `processor/rule/processor.go:1023` — `} else if err := rcm.Start(runCtx); err != nil {`
- `processor/rule/processor.go:1215` — `rp.handleMessage(msgCtx, subject, msg.Data())`
- `processor/rule/processor.go:1216` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/rule/kv_config_integration.go:150` — `return store.Watch(watchCtx, "rules.*")`
- `processor/rule/kv_config_integration.go:156` — `rcm.logger.Warn("Failed to open KV watcher for rules.*, hot-reload disabled", "error", err)`
- `processor/rule/kv_config_integration.go:158` — `return nil`
- `processor/rule/kv_config_integration.go:176` — `go rcm.processKVUpdates(runCtx, watcher, done)`
- `processor/rule/kv_config_integration.go:292` — `if err := rcm.reconcileFromKV(ctx); err != nil {`
- `processor/rule/kv_config_integration.go:305` — `defs, err := rcm.ListRules(ctx)`
- `processor/rule/kv_config_integration.go:331` — `if err := rcm.processor.ValidateConfigUpdate(changes); err != nil {`
- `processor/rule/kv_config_integration.go:336` — `if err := rcm.processor.ApplyConfigUpdate(changes); err != nil {`
- `processor/rule/kv_config_integration.go:512` — `currentConfig := rcm.processor.GetRuntimeConfig()`
- `processor/agentic-tools/executors/rules.go:214` — `if err := e.manager.SaveRule(ctx, ruleID, def); err != nil {`
- `processor/agentic-tools/executors/rules.go:225` — `Content: fmt.Sprintf("Rule '%s' %s successfully. It is now active in the rules engine.", ruleID, action),`
- `processor/rule/runtime_config.go:37` — `return rp.submitRuntimeCommand(func(ctx context.Context) error {`
- `processor/rule/runtime_config.go:179` — `def, err := definitionFromMap(ruleID, ruleMap)`
- `processor/rule/message_handler.go:387` — `ruleSubjects := r.Subscribe()`
- `processor/rule/message_handler.go:392` — `if ruleSubject == ">" || ruleSubject == subject {`
- `processor/rule/message_handler.go:204` — `events, err := ruleInstance.ExecuteEvents(messages)`
- `processor/rule/config_validation.go:128` — `def, err := definitionFromMap(ruleID, ruleMap)`
- `processor/rule/config_validation.go:551` — `def, err := definitionFromMap(ruleID, ruleMap)`
- `processor/rule/config_validation.go:565` — `r, err := CreateRuleFromDefinition(def, deps)`

## Problem shape

- `docs/adr/094-boot-only-composition-and-observable-rule-activation.md:62` — `including ports, dependencies, entity-watch buckets, integration mode, producer identity, and projection bindings.`
- `docs/adr/094-boot-only-composition-and-observable-rule-activation.md:64` — `The Rule processor builds and validates a complete candidate rule set before changing the active generation. A`
- `docs/adr/094-boot-only-composition-and-observable-rule-activation.md:65` — `rejected candidate leaves the previous generation unchanged. Watch, reconciliation, activation, and status publication`
- `docs/adr/094-boot-only-composition-and-observable-rule-activation.md:74` — `pack/rule/revision receipt with activation pending. Write success is not evidence that the running Rule processor`
- `processor/rule/rule_loader.go:125` — `effective, err := deriveEffectiveProjectionContracts(snapshot, rp.config.ProjectionContracts)`
- `processor/rule/rule_loader.go:134` — `if err := validateRuleReconcileActions(targets, definition); err != nil {`
- `processor/rule/processor.go:625` — `if rp.commandFenced || rp.commandWake == nil {`
- `processor/rule/processor.go:627` — `return errs.WrapInvalid(errors.New("runtime command admission is closed"), "RuleProcessor", "runtimeCommand", "processor is not accepting runtime updates")`
- `processor/rule/message_handler.go:211` — `prepared, err := rp.prepareGraphEvents(events)`

## Searches

- Initial `git rev-parse HEAD` → 4039530ff6213a25b946332c876b6d1e58c86a04; initial and pre-write `git status --short` → clean.
- `cat .agents/contracts/semstreams-explorer.md` → complete contract read.
- `sed -n '1,64p' openspec/project.md` → Purpose and Product Boundary read.
- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls workspace_symbol -matcher=fuzzy prepareInitialRules` → 1 result lines.

```text
processor/rule/rule_loader.go:103:22-41 Processor.prepareInitialRules Method

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls workspace_symbol -matcher=fuzzy definitionFromMap` → 5 result lines.

```text
processor/rule/config_validation.go:436:6-23 definitionFromMap Function
processor/rule/config_validation_test.go:49:6-47 TestDefinitionFromMap_PreservesCronFields Function
processor/rule/config_validation_test.go:91:6-55 TestDefinitionFromMap_AbsentCooldownIsEmptyString Function
processor/rule/config_validation_test.go:26:6-56 TestDefinitionFromMap_PreservesCooldownOnHotReload Function
processor/rule/config_validation_test.go:442:6-61 TestDefinitionFromMapRejectsDisabledAndCronRelatedLoops Function

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls workspace_symbol -matcher=fuzzy SaveRule` → 44 result lines.

```text
processor/agentic-tools/executors/rules.go:20:2-10 RuleManager.SaveRule Method
processor/rule/kv_config_integration.go:412:27-35 ConfigManager.SaveRule Method
processor/agentic-tools/executors/rules_test.go:23:27-35 mockRuleManager.SaveRule Method
processor/agentic-tools/executors/register_test.go:250:32-40 recordingRuleManager.SaveRule Method
processor/agentic-tools/executors/rules.go:184:24-32 RuleExecutor.saveRule Method
processor/rule/stateful_evaluator_test.go:1025:6-86 TestStatefulEvaluator_ActionlessLiveRule_PersistsMatchStateWithoutSpuriousFiring Function
test/e2e/scenarios/crud-tools/scenario.go:859:20-34 Scenario.verifyRuleInKV Method
test/e2e/scenarios/crud-tools/scenario.go:112:2-21 Scenario.baselineActiveRules Field
test/e2e/scenarios/crud-tools/scenario.go:893:20-39 Scenario.validateRuleContent Method
processor/rule/metrics.go:22:2-13 github.com/c360studio/semstreams/processor/rule.Metrics.activeRules Field
test/e2e/scenarios/validate_infra.go:527:26-46 TieredScenario.executeValidateRules Method
test/e2e/scenarios/tiered_structural.go:233:26-56 TieredScenario.executeValidateRuleTransitions Method
processor/agentic-loop/governance_dispatcher.go:136:2-8 github.com/c360studio/semstreams/processor/agentic-loop.VerdictPayload.RuleID Field
processor/agentic-loop/governance_dispatcher.go:331:2-8 github.com/c360studio/semstreams/processor/agentic-loop.verdictArrival.ruleID Field
agentic/rule_fields.go:229:24-34 github.com/c360studio/semstreams/agentic.ContextEvent.RuleFields Method
agentic/rule_fields.go:178:27-37 github.com/c360studio/semstreams/agentic.LoopFailedEvent.RuleFields Method
agentic/rule_fields.go:98:28-38 github.com/c360studio/semstreams/agentic.LoopCreatedEvent.RuleFields Method
agentic/rule_fields.go:208:30-40 github.com/c360studio/semstreams/agentic.LoopCancelledEvent.RuleFields Method
agentic/rule_fields.go:128:30-40 github.com/c360studio/semstreams/agentic.LoopCompletedEvent.RuleFields Method
agentic/rule_fields.go:313:32-42 github.com/c360studio/semstreams/agentic.ApprovalPendingEvent.RuleFields Method
processor/rule/processor.go:1626:6-34 github.com/c360studio/semstreams/processor/rule.validateRuleReconcileActions Function
frameworkcapabilities/graphresearch/register.go:362:6-22 github.com/c360studio/semstreams/frameworkcapabilities/graphresearch.validateRulePack Function
agentic/rule_fields_test.go:19:6-53 github.com/c360studio/semstreams/agentic.TestEveryRegisteredAgenticPayloadIsRuleReadable Function
frameworkcapabilities/graphresearch/register.go:387:6-25 github.com/c360studio/semstreams/frameworkcapabilities/graphresearch.validateRuleContent Function
governance/verdict.go:58:2-8 github.com/c360studio/semstreams/governance.VerdictEvent.RuleID Field
processor/rule/deny.go:30:2-8 github.com/c360studio/semstreams/processor/rule.DenyVerdict.RuleID Field
processor/rule/config_validation.go:160:22-44 github.com/c360studio/semstreams/processor/rule.Processor.validateExpressionRule Method
processor/rule/config_validation.go:81:22-46 github.com/c360studio/semstreams/processor/rule.Processor.validateSingleRuleConfig Method
processor/rule/processor.go:1622:22-50 github.com/c360studio/semstreams/processor/rule.Processor.validateRuleReconcileActions Method
processor/rule/schedule_tracker_test.go:376:6-52 github.com/c360studio/semstreams/processor/rule.TestProcessor_RemoveRule_DeletesScheduleRecord Function
processor/rule/runtime_config.go:278:22-32 github.com/c360studio/semstreams/processor/rule.Processor.removeRule Method
processor/rule/runtime_config.go:282:22-39 github.com/c360studio/semstreams/processor/rule.Processor.removeRuleContext Method
test/e2e/scenarios/stages/rules.go:39:26-39 github.com/c360studio/semstreams/test/e2e/scenarios/stages.RulesValidator.ValidateRules Method
test/e2e/scenarios/stages/rules.go:114:26-49 github.com/c360studio/semstreams/test/e2e/scenarios/stages.RulesValidator.ValidateRuleTransitions Method
agentic/rule_fields.go:630:32-42 github.com/c360studio/semstreams/agentic.WebObservationEntity.RuleFields Method
agentic/agentrun/agentrun_test.go:400:6-53 github.com/c360studio/semstreams/agentic/agentrun_test.TestResolveRun_AncestryWalkPath_WhenNoRunTriple Function
agentic/agentrun/agentrun_test.go:386:6-53 github.com/c360studio/semstreams/agentic/agentrun_test.TestResolveRun_TypedTriplePath_WhenRunIDPresent Function
graph/entity_predicate_contract_test.go:118:6-62 github.com/c360studio/semstreams/graph.TestValidateEntityStateContractIdentityAndReferenceRules Function
processor/rule/config_validation_test.go:113:6-55 github.com/c360studio/semstreams/processor/rule.TestValidateExpressionRule_RejectsRuleOpaqueField Function
examples/processors/iot_sensor/vocabulary_test.go:9:6-56 github.com/c360studio/semstreams/examples/processors/iot_sensor.TestRegisterVocabularyDeclaresReferenceRuleOutputs Function
/Users/coby/go/pkg/mod/go.opentelemetry.io/otel@v1.42.0/semconv/v1.26.0/metric.go:280:2-47 semconv.AspnetcoreRateLimitingActiveRequestLeasesName Constant
/Users/coby/go/pkg/mod/go.opentelemetry.io/otel@v1.42.0/semconv/v1.26.0/metric.go:281:2-47 semconv.AspnetcoreRateLimitingActiveRequestLeasesUnit Constant
/Users/coby/go/pkg/mod/go.opentelemetry.io/otel@v1.42.0/semconv/v1.26.0/metric.go:282:2-54 semconv.AspnetcoreRateLimitingActiveRequestLeasesDescription Constant
processor/rule/schedule_tracker_test.go:236:6-42 github.com/c360studio/semstreams/processor/rule.TestScheduleKey_MVPShapeIsBareRuleID Function

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls workspace_symbol -matcher=fuzzy ExecuteEvents` → 8 result lines.

```text
processor/rule/interfaces.go:41:2-15 Rule.ExecuteEvents Method
processor/rule/test_rule_factory.go:125:20-33 TestRule.ExecuteEvents Method
processor/rule/expression_factory.go:329:26-39 ExpressionRule.ExecuteEvents Method
processor/rule/fire_every_n_events_test.go:382:31-44 alwaysMatchTestRule.ExecuteEvents Method
processor/rule/lifecycle_owner_test.go:54:31-44 bootstrapActionRule.ExecuteEvents Method
processor/rule/fire_every_n_events_test.go:375:32-45 executeErrorTestRule.ExecuteEvents Method
processor/rule/entity_rule_pattern_selection_test.go:31:32-45 patternSelectionRule.ExecuteEvents Method
processor/rule/publisher_graph_event_contract_test.go:255:36-49 publisherContractRule.ExecuteEvents Method

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/rule/rule_loader.go:103:22` → 3 result lines.

```text
processor/rule/processor.go:468:12-31
processor/rule/processor.go:947:15-34
processor/rule/rule_loader.go:167:15-34

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/rule/config_validation.go:436:6` → 9 result lines.

```text
processor/rule/action_maxiterations_test.go:281:16-33
processor/rule/action_maxiterations_test.go:405:16-33
processor/rule/config_validation.go:128:14-31
processor/rule/config_validation.go:551:14-31
processor/rule/config_validation_test.go:37:14-31
processor/rule/config_validation_test.go:455:16-33
processor/rule/config_validation_test.go:69:14-31
processor/rule/config_validation_test.go:99:14-31
processor/rule/runtime_config.go:179:14-31

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/rule/kv_config_integration.go:412:27` → 1 result lines.

```text
processor/agentic-tools/executors/rules.go:214:22-30

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls implementation processor/rule/interfaces.go:30:6` → 7 result lines.

```text
processor/rule/entity_rule_pattern_selection_test.go:17:6-26
processor/rule/expression_factory.go:18:6-20
processor/rule/fire_every_n_events_test.go:364:6-25
processor/rule/fire_every_n_events_test.go:368:6-26
processor/rule/lifecycle_owner_test.go:49:6-25
processor/rule/publisher_graph_event_contract_test.go:248:6-27
processor/rule/test_rule_factory.go:15:6-14

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/rule/interfaces.go:41:2` → 6 result lines.

```text
processor/rule/expression_factory_test.go:390:20-33
processor/rule/graph_event_identity_test.go:130:42-55
processor/rule/graph_event_identity_test.go:143:30-43
processor/rule/graph_event_identity_test.go:160:22-35
processor/rule/graph_event_identity_test.go:180:29-42
processor/rule/message_handler.go:204:30-43

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls call_hierarchy processor/rule/rule_loader.go:226:16` → 13 result lines.

```text
caller[0]: ranges 565:12-36 in processor/rule/config_validation.go from/to function createRuleFromConfig in processor/rule/config_validation.go:550:22-42
caller[1]: ranges 420:15-39 in processor/rule/expression_factory_test.go from/to function TestCreateRuleFromDefinition_Expression in processor/rule/expression_factory_test.go:408:6-45
caller[2]: ranges 439:12-36 in processor/rule/expression_factory_test.go from/to function TestCreateRuleFromDefinition_UnknownType in processor/rule/expression_factory_test.go:430:6-46
caller[3]: ranges 226:16-40 in processor/rule/rule_loader.go from/to function loadRules in processor/rule/rule_loader.go:152:22-31
caller[4]: ranges 331:15-39, 337:11-35 in processor/rule/runtime_config_test.go from/to function TestCreateRuleFromDefinition in processor/rule/runtime_config_test.go:301:6-34
caller[5]: ranges 53:28-52 in test/e2e/scenarios/crud-tools/fire_every_n_fixture_test.go from/to function TestFireEveryNRuleDefinitionScopesOnlySeededProbeEntities in test/e2e/scenarios/crud-tools/fire_every_n_fixture_test.go:19:6-63
identifier: function CreateRuleFromDefinition in processor/rule/rule_factory.go:195:6-30
callee[0]: ranges 199:12-26 in processor/rule/rule_factory.go from/to function validatePackID in processor/rule/config.go:176:6-20
callee[1]: ranges 196:12-30 in processor/rule/rule_factory.go from/to function ValidateDefinition in processor/rule/config_validation.go:266:6-24
callee[2]: ranges 213:17-23 in processor/rule/rule_factory.go from/to function Create in processor/rule/rule_factory.go:81:2-8
callee[3]: ranges 208:20-28 in processor/rule/rule_factory.go from/to function Validate in processor/rule/rule_factory.go:90:2-10
callee[4]: ranges 202:21-35 in processor/rule/rule_factory.go from/to function GetRuleFactory in processor/rule/rule_factory.go:157:6-20
callee[5]: ranges 197:19-25, 200:19-25, 204:19-25, 209:19-25 in processor/rule/rule_factory.go from/to function Errorf in /usr/local/go/src/fmt/errors.go:23:6-12

```

- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls workspace_symbol -matcher=fuzzy submitRuntimeCommand` → 1 result lines.

```text
processor/rule/processor.go:622:22-42 Processor.submitRuntimeCommand Method

```

- `git grep -n -E 'ValidateConfigUpdate|ApplyConfigUpdate|ListRules|SaveRule|parseActions|createRule|CreateRule|factory.Create|prepareInitialRules|Subscribe|subscribeTo' -- processor/rule/rule_loader.go processor/rule/config_validation.go processor/rule/runtime_config.go processor/rule/kv_config_integration.go processor/rule/processor.go processor/rule/expression_factory.go processor/agentic-tools/executors/rules.go` → 76.
- `git grep -n -E 'subscribed:|subscribe|Subscribe\(|shouldProcessRule' -- processor/rule/expression_factory.go processor/rule/message_handler.go processor/rule/types.go processor/rule/config.go` → 6.
- `git grep -n -E 'RequestID|request_id|ExecutionID|execution_id|ProposalFingerprint|proposal_fingerprint|CallID|call_id' -- processor/agentic-loop/governance_dispatcher.go` → 32.
- `git ls-files 'docs/adr/*094*' 'openspec/specs/*rule*/*'` → 5.
- `git grep -n -E 'Definition struct|json:"subscribe|json:"on_enter|json:"on_exit|json:"while_true|json:"on_recovery' -- processor/rule` → 8.
- `git grep -n -E 'candidate|admission|activation|SaveRule|ListRules|partial|atomic|complete|last.good|boot.only' -- docs/adr/094-boot-only-composition-and-observable-rule-activation.md` → 22.
- `git grep -n -i -E 'retry|replay|ambiguous' -- openspec/specs/rule-projection-mutations/spec.md` → 5.
- `git grep -n -E 'RequestID|request_id|ExecutionID|execution_id|ProposalFingerprint|proposal_fingerprint|proposal-fingerprint|PROPOSAL_FINGERPRINT' -- processor/agentic-loop/governance_dispatcher.go` → 0.
- `git grep -n -E 'json:"subscribe|json:"subject|SUBSCRIBE|Subscribe' -- processor/rule/rule_factory.go` → 0.
- `git grep -n -E 'watcher, err|Watch\(|Failed.*watch|watch.*disabled' -- processor/rule/kv_config_integration.go` → 3.
- `git grep -n -E 'RequestID|request_id|request-id|REQUEST_ID|ExecutionID|execution_id|execution-id|EXECUTION_ID|ProposalFingerprint|proposalFingerprint|proposal_fingerprint|proposal-fingerprint|PROPOSAL_FINGERPRINT' -- processor/agentic-loop/governance_dispatcher.go` → 0.
- `git grep -n -E 'create_rule|update_rule|save_rule|SaveRule|saveRule|save-rule|SAVE_RULE' -- processor/agentic-tools/executors/rules.go` → 9.
- `git grep -n -E 'definitionFromMap|CreateRuleFromDefinition|prepareInitialRules|ExecuteEvents\(' -- processor/rule/config_validation.go processor/rule/processor.go processor/rule/rule_loader.go processor/rule/runtime_config.go processor/rule/message_handler.go` → 19.
- Literal groups used `| awk '{print} END {print "HITS=" NR}'` to retain exact counts, including zero hits.
- `gh issue list --search 'governance' --state open --json number,title` → FAILED: error connecting to api.github.com; no absence claim.
- `gh pr list --state open --json number,title,body --jq '.[] | select(.number == 1312)'` → FAILED: error connecting to api.github.com; no independent PR-body claim.
- `openspec list` → 1 active change: governance-proposal-settlement, 0/7 tasks.

Pin-window reads:

- `nl -ba openspec/changes/governance-proposal-settlement/proposal.md | sed -n '1,105p'`.
- `nl -ba openspec/changes/governance-proposal-settlement/tasks.md | sed -n '1,95p'`.
- `nl -ba processor/rule/rule_loader.go | sed -n '70,166p'`.
- `nl -ba processor/rule/config_validation.go | sed -n '436,532p'`.
- `nl -ba processor/rule/interfaces.go | sed -n '17,54p'`.
- `nl -ba processor/rule/kv_config_integration.go | sed -n '270,343p'`.
- `nl -ba processor/rule/kv_config_integration.go | sed -n '411,431p'`.
- `nl -ba processor/rule/kv_config_integration.go | sed -n '469,514p'`.
- `nl -ba processor/agentic-tools/executors/rules.go | sed -n '208,233p'`.
- `nl -ba processor/rule/runtime_config.go | sed -n '44,68p'`.
- `nl -ba processor/rule/runtime_config.go | sed -n '99,168p'`.
- `nl -ba processor/rule/runtime_config.go | sed -n '235,277p'`.
- `nl -ba processor/rule/rule_loader.go | sed -n '218,240p'`.
- `nl -ba processor/rule/processor.go | sed -n '942,975p'`.
- `nl -ba processor/rule/processor.go | sed -n '1006,1039p'`.
- `nl -ba processor/rule/expression_factory.go | sed -n '127,150p'`.
- `nl -ba processor/rule/expression_factory.go | sed -n '324,379p'`.
- `nl -ba processor/rule/message_handler.go | sed -n '63,85p'`.
- `nl -ba processor/rule/message_handler.go | sed -n '187,217p'`.
- `nl -ba processor/rule/rule_factory.go | sed -n '16,45p'`.
- `nl -ba processor/rule/rule_factory.go | sed -n '195,215p'`.
- `nl -ba processor/rule/expression_factory.go | sed -n '71,126p'`.
- `nl -ba processor/rule/message_handler.go | sed -n '385,405p'`.
- `nl -ba processor/rule/processor.go | sed -n '981,1007p'`.
- `nl -ba processor/rule/processor.go | sed -n '1212,1219p'`.
- `nl -ba processor/rule/kv_config_integration.go | sed -n '184,246p'`.
- `nl -ba processor/rule/runtime_config.go | sed -n '13,43p'`.
- `nl -ba processor/rule/kv_config_integration.go | sed -n '145,179p'`.
- `nl -ba processor/rule/processor.go | sed -n '622,650p'`.
- `nl -ba docs/adr/094-boot-only-composition-and-observable-rule-activation.md | sed -n '60,77p'`.
- `nl -ba processor/rule/rule_factory.go | sed -n '44,68p'`.
- `nl -ba processor/rule/config_validation.go | sed -n '124,143p'`.
- `nl -ba processor/rule/config_validation.go | sed -n '548,568p'`.

Read-time SHA-256 (`shasum -a 256` with these exact paths; pre-write worktree remained clean):

```text
9ceb6885bd4eff69025fb624e6af1e580305b29b784fdbb67930eaa5f3a75000  processor/rule/rule_loader.go
f28756ecd48d7a9b7c1d3d76330df6750fbf92db16fefc1d077809170741fb5a  processor/rule/config_validation.go
fecef9631ab1c7af6b6f365647c405b413a0d59dee20a221a545a9def2315107  processor/rule/kv_config_integration.go
a649914a93b3ba82a9daf0b60a1421f9afd80990531378f4f6efa45db0ebd402  processor/rule/runtime_config.go
13e1d167ac4c648e81fd7dce28b3091e04792f253f4d9bd069378fb14ab69a6c  processor/rule/rule_factory.go
b2b772d367afc7aa5f0ba55950b545560f3176097819ed2551634312fb1a91cc  processor/rule/expression_factory.go
bd4958b5b570cf937b35c34a85c6d1f583d4d3460a502748f41597968c9ee0cb  processor/rule/message_handler.go
304483872688a57a1bc76a504aad3cbf75a8729c03b13ec93e6ef514cd02ed8b  processor/rule/processor.go
c3f4d298c77ffbac45fbc435407b15ba8ab738f273527e0cfec918b5858fb5f1  processor/agentic-tools/executors/rules.go
aac28b38edb93fbc20f2af6c6ff02f7063312afa56602f3d7eb6814c8ebb679d  processor/agentic-loop/governance_dispatcher.go
d73aa0f1ec31b0a2a8150297db02978b5e832bed707d96e0f2f398c50c61dfa4  openspec/changes/governance-proposal-settlement/proposal.md
65cca810d0132df348850e9ac1c6f69dc605a76e9b9fad93576eba230b0d0267  openspec/changes/governance-proposal-settlement/tasks.md
```

- Prior replay inventories were not re-enumerated; provenance is pinned to this claim's proposal, including their exact SHA-256 values and c347eff4 snapshot limitation.
- Root halted further measurement and requested immediate materialization. Remaining questions below are not absence claims.
- Verification command: `task inventory:verify -- openspec/changes/governance-proposal-settlement/inventory.md`; final digest command: `shasum -a 256 openspec/changes/governance-proposal-settlement/inventory.md`.

NOT RUN:

- NOT RUN — Additional GitHub issue/PR reads and spellings after connection failure; PR #1312 body remains unread independently.
- NOT RUN — Complete individual Definition field-reader census, RuleFactory implementer census, and remaining custom Rule test-implementation source windows; structural results retained above.
- NOT RUN — Detailed tests for malformed action declarations, watcher-open failure, partial ListRules, and multi-rule partial replacement; no coverage conclusion.
- NOT RUN — Additional projection/cooldown/match-counter replay enumeration; existing passed replay inventories reused only by recorded provenance.
- NOT RUN — Main-versus-#1159 source diff beyond the recorded proposal/dispatcher correlation pins; settlement API integration remains unenumerated.
- NOT RUN — Sister-repository adopters and broader migration/ADR spellings beyond the named ADR-094 and projection spec.

## Architect adopter-seam and collision supplement

Inventory-only; no target recommendation. Evidence was collected at claim HEAD
`4039530ff6213a25b946332c876b6d1e58c86a04`, based on main `7698a59fa32c251924636acbaae8d89cad23b161`.
This packet does not claim a fresh GitHub or workspace check.

### Adopter seam

**Persona:** a developer configuring a Rule processor to decide immutable governance proposals, then updating its
rules through `create_rule` or `update_rule`.

They currently must understand three framework details:

1. **Physical input scope determines eligible messages.** `Definition` has no subscription field
   (`processor/rule/rule_factory.go`, complete declaration at 16–69). Built-in message expression rules receive `>`
   at `expression_factory.go:112`; entity-scoped rules receive no message subjects at 113–115. The JetStream callback
   passes its configured filter, not the delivered message's actual subject, at `processor.go:1215`. Conditions alone
   therefore do not declare a proposal-only input boundary.
2. **Saved does not mean activated.** `ConfigManager.SaveRule` validates authoring at `kv_config_integration.go:417`
   and writes KV at 425. The watcher later validates and applies at 331 and 336. Nevertheless, the sole production
   SaveRule caller reports “now active” at `processor/agentic-tools/executors/rules.go:225`. A caller who does nothing
   further can receive success before runtime rejection or while hot reload is disabled; watcher-open failure logs
   and returns nil at `kv_config_integration.go:153–158`.
3. **Redelivery does not imply the same actions execute.** The message path enters the shared stateful evaluator at
   `message_handler.go:142`. Persisted matching state selects OnEnter versus WhileTrue
   (`stateful_evaluator.go:136`, 328, 348), and action counters increment before execution at 423–426. The previously
   accepted replay inventory remains the detailed evidence.

**Where they find out:** constructor/authoring errors are returned; asynchronous activation rejection is logged;
the tool success message currently overstates activation. Publication routing and match-cycle replay consequences
require reading implementation or detailed documentation.

**Knowledge gap:** authors should describe policy and intended inputs. They should not need to predict whether a
successful write became active, whether publication obtained PubAck, or whether persisted match state suppresses
redelivery. More than two hidden correctness facts is an adopter-seam finding, not merely missing documentation.

### Existing owners and collisions

| Semantic job | Existing owner and evidence |
| --- | --- |
| Authoring and composition refusal | `ValidateDefinition`; boot `prepareInitialRules` validates every definition and projection envelope before publishing its snapshot (`rule_loader.go:117`, 134, 142). Runtime validation owns envelope refusal (`config_validation.go:17–47`, 133). |
| Rule construction | Registered `Factory.Validate` then `Factory.Create` (`rule_factory.go:208`, 213). Definition inspection alone does not establish arbitrary registered factory behavior. |
| Physical input ownership | Constructor resolves ports once (`processor.go:391–395`); Start installs subscriptions from those ports (`1120`); exact stream handles are retained (`1224`). |
| Desired versus effective rules | KV `rules.*`, ConfigManager reconciliation, processor rule/definition/config maps, and `GetRuntimeConfig` (`runtime_config.go:322–339`). Runtime commands already have a classified closed-admission refusal (`processor.go:627`). |
| Match-cycle authority | Existing StateTracker KV Get/Put (`state_tracker.go:119`, 157); expression instances also retain cooldown/trigger state (`expression_factory.go:161`, 363–364). |
| Publication consequence | Existing actionPublisher chooses JetStream or core NATS (`publisher.go:46–49`), using literal output-subject equality at 72. That existing R8 dependency remains separate. |
| Pre-effect batch refusal shape | Existing graph-event preparation validates the entire batch before publication (`publisher.go:126–148`). No new general admission primitive is established by this inventory. |

### Adjacent constraints and unproven items

1. ADR-094 requires a fixed component envelope and complete-candidate activation (`61–66`). Current replacement
  applies entries sequentially (`runtime_config.go:126–139`, 266–269); this inventory does **not** establish atomic
  activation.
2. `ListRules` skips individual read/decode failures (`kv_config_integration.go:495`, 501) before full replacement.
  Reviewer additionally measured `definitionFromMap` action-decode errors becoming nil lists at
  `config_validation.go:503–516`, before validation at 541.
3. Projection mutations retain their one-attempt/no-old-context-replay contract
  (`rule-projection-mutations/spec.md:84–89`). Optional `rule_events` remains optional
  (`rule-action-observability/spec.md:35–42`).
4. Detailed publication-only admission grammar, reload refusal behavior, and production-path proof are **not yet
  established**. No new tests were run.
5. Settlement-helper and correlation dependencies remain explicitly stacked; no independent-main landing is asserted.
6. These overlaps do not authorize general reload repair, ADR-094 implementation, or expansion into #935.

### Root live-record reconciliation — 2026-09-15

The explorer's failed network reads above are not absence evidence. Root read `gh pr view 1312` and
`gh issue view 1311` with body and comments successfully on 2026-09-15: PR #1312 is OPEN/DRAFT at this baseline;
#1311 is OPEN in beta.163 with the owner's separate design-claim direction recorded at
https://github.com/C360Studio/semstreams/issues/1311#issuecomment-5666981667.
`gh pr view 1159` confirms OPEN/DRAFT at c347eff4, based on `codex/gh759-semantic-settlement`;
`gh issue view 1146` confirms OPEN. `gh issue view 935` confirms OPEN, post-v1 and unmilestoned.
These successful reads close only the named claim-state question, not a repository-wide absence claim.

## Architect supplemental anchors

The architect adopted the explorer checkpoint `d5fb001048bded51c9048ee209153f1f50e26c77286ebd9329501754e52974d3`
together with the adopter/collision packet above, then supplied these existing-evidence anchors. No target is approved.

- `processor/rule/rule_factory.go:16` — `type Definition struct {`
- `processor/rule/rule_factory.go:130` — `func RegisterRuleFactory(ruleType string, factory Factory) error {`
- `processor/rule/processor.go:391` — `inputs, outputs, err := resolvePorts(*rp.config.Ports)`
- `processor/rule/processor.go:395` — `rp.inputPorts, rp.outputPorts = inputs, outputs`
- `processor/rule/processor.go:1120` — `for _, port := range rp.inputPorts {`
- `processor/rule/processor.go:1179` — `subject := stream.Subjects()[0]`
- `processor/rule/expression_factory.go:161` — `if r.cooldown > 0 && time.Since(r.lastTriggered) < r.cooldown {`
- `processor/rule/expression_factory.go:199` — `result, err := r.evaluator.EvaluateWithStateAndMessage(nil, nil, expression.MessageFields(data), expr)`
- `processor/rule/expression_factory.go:363` — `r.lastTriggered = time.Now()`
- `processor/rule/expression_factory.go:364` — `r.shouldTrigger = false`
- `processor/rule/runtime_config.go:269` — `rp.matchCounters[ruleID] = &atomic.Int64{}`
- `processor/rule/publisher.go:72` — `if subjects := facts.NATSSubjects(); len(subjects) == 1 && subjects[0] == subject {`
- `openspec/specs/rule-action-observability/spec.md:35` — `The `rule_events` output SHALL remain an optional rule-trigger notification. When a rule processor has no`

### Architect disposition of bounded unknowns

| Item | Bounded disposition |
| --- | --- |
| Further GitHub checks | Root's verified claim/issue records supply adjacent-work ownership. Preserve the explorer's failed-call record; it is not an absence claim. |
| Field/factory census | Architect read the complete `Definition` declaration, lines 16–69: no subscription field. `gopls implementation processor/rule/rule_factory.go:79:6` returned `ExpressionRuleFactory` at `expression_factory.go:369` and `TestRuleFactory` at `test_rule_factory.go:156`. Individual readers of every unrelated field were not enumerated; arbitrary external factory purity remains unproven. |
| Detailed tests | Remains NOT RUN. This checkpoint establishes source behavior, not test coverage or corrected behavior. |
| Broader replay | Reuse the accepted 72-pin inventory. Architect's main-versus-c347 comparison returned no differences for `message_handler.go`, `stateful_evaluator.go`, `state_tracker.go`, `action_id.go`, or `expression_factory.go`. New pins identify current cooldown and counter owners without repeating that inventory. |
| Full #1159 diff | A full PR review is outside this checkpoint. Relevant comparison is established: settlement types are absent on main; `SettleDelivery` is an additional 13-line #1159 change beyond frozen #1156; #1159 owns execution-ID/fingerprint propagation; publisher classification gap is unchanged. Preserve main's newer unrelated rule-action changes. This does not establish independent landing. |
| Downstream/migration | External rule/config authors are covered by the adopter packet. No sister-repository census or migration validation was performed. Those remain unknown until an accepted target identifies an actual outward-facing change. |

The decoder omission identified by the reviewer is captured by the explorer's `parseActions` pins. ADR-094's
desired-versus-active and complete-candidate promises remain adjacent constraints, not an instruction to repair
general activation. This completes the architect's inventory audit/adoption; independent review of this final
materialized identity is required before drafting a target.

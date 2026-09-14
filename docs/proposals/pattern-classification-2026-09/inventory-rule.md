# Inventory: #1234 slice rule
base: 32aeddf73612d157261ace65d9810750d2d602e0

## #383 — processor/rule: make rule-level max_iterations impossible to mistake for enforcement
Named sites:
- `processor/rule/rule_factory.go:39` — `// MaxIterations limits how many times a rule can enter the matching state for an entity.`
- `processor/rule/rule_factory.go:42` — `MaxIterations int `json:"max_iterations,omitempty"``
- `processor/rule/actions.go:344` — `MaxIterations *int `json:"max_iterations,omitempty"``
- `processor/rule/stateful_evaluator.go:222` — `"$state.max_iterations":  ev.Rule.MaxIterations,`
- `processor/rule/execution_context.go:303` — `result = strings.ReplaceAll(result, "$state.max_iterations", fmt.Sprintf("%d", ec.State.MaxIterations))`
- `processor/rule/action_id.go:121` — `func (a Action) effectiveMaxIterations() (maxIter int, fromDefault bool) {`
Refusal and observation:
(none — see Searches)
Nearest pattern instance:
admission gate — cron_rule.go already rejects a rule-level `max_iterations` declared on a cron rule via a collected-rejections check.
- `processor/rule/cron_rule.go:180` — `rejected = append(rejected, "max_iterations")`
- `processor/rule/cron_rule.go:191` — `if len(rejected) > 0 {`

## #746 — processor/rule + research-graph: first-wins resolution makes a round-2 companion predicate permanently invisible
Named sites:
- `processor/rule/expression/evaluator.go:395` — `func (d *defaultTypeDetector) GetFieldValue(entityState *gtypes.EntityState, field string) (interface{}, bool, error) {`
- `processor/rule/expression/evaluator.go:399` — `if triple.Predicate == field {`
- `processor/rule/expression/evaluator.go:400` — `return triple.Object, true, nil`
- `configs/rules/research-graph/02-route-decision-dispatch.json:5` — `"description": "ADR-045 R2. Fires on research.route.complete (the route_search component's terminal stamp). Four action-level branches keyed on research.route.action (per ADR-041 action-level when clauses): synthesize_directly short-circuits to synthesize_answer; retighten re-dispatches nl_classify after clearing the classify+route markers (so R1 can re-fire); walk_seeds and decompose both dispatch execute_subqueries. The retighten branch carries MaxIterations=2 (per ADR-045 § Rule chain — caps the classify→retighten ping-pong at 2 rounds). walk_seeds/decompose dispatch is a single action with an `in` operator covering both routes, avoiding action duplication. R0 doesn't re-fire on retighten because research.request.received is never cleared — the retighten path is rule-driven, not kickoff-driven.",`
- `configs/rules/research-graph/04-assess-dispatch.json:5` — `"description": "ADR-045 R4. Fires on research.assess.complete (assess_sufficiency's terminal stamp). Three action-level branches keyed on research.assess.sufficient and $state.iteration: (1) sufficient=true → synthesize_answer (terminal happy path); (2) sufficient=false AND $state.iteration <= 5 → refine via execute_subqueries after clearing the execute+assess markers (so R3 re-fires); (3) sufficient=false AND $state.iteration >= 6 → synthesize_answer (cap-exhaust fallback — the refine actions hit MaxIterations=5 on the 6th R4 fire and skip, so the fallback action must take over or the chain stalls forever). $state.iteration is the per-rule firing counter (see processor/rule/rule_factory.go Definition.MaxIterations doc) — increments on each TransitionEntered. The 1st R4 fire (initial assess) is iteration=1; refines fire R4 again at iterations 2..6 (each refine creates a new assess.complete after the remove_triple clear), so the 6th fire is the cap-exhaust boundary.",`
Refusal and observation:
(none — see Searches)
Nearest pattern instance:
read-through / merge-with-reconcile over a cache —
- `processor/rule/expression/regex_cache.go:35` — `if re, found := globalRegexCache.Get(pattern); found {`

## #1007 — rule: fire_every_n_events silently does not gate stateful actions (on_enter/publish_agent), and submit_work is a ghost tool
Named sites:
- `processor/rule/message_handler.go:40` — `func shouldFireAction(n int, counter *atomic.Int64) bool {`
- `processor/rule/message_handler.go:192` — `if counter == nil || !shouldFireAction(n, counter) {`
processor/rule/stateful_evaluator.go — zero references to `shouldFireAction` or `FireEveryNEvents` (confirmed, see Searches)
- `processor/agentic-tools/categories.go:12` — `// CategoryCore contains essential tools available to all agents (submit_work, etc.)`
- `processor/agentic-tools/categories.go:38` — `"submit_work": CategoryCore,`
- `processor/agentic-tools/emit_diagnosis.go:29` — `// multiple findings per loop before calling submit_work.`
- `processor/agentic-tools/emit_diagnosis.go:55` — `Description: "Emit a structured ops diagnosis finding to the knowledge graph. Call once per finding; you may call multiple times per loop before submit_work. Each call mints a new diagnosis entity with evidence-backed predicates so downstream rules can branch on severity and confidence without parsing prose.",`
- `processor/rule/actions.go:1446` — `func (e *ActionExecutor) resolveToolNames(names []string) []agentic.ToolDefinition {`
- `processor/rule/actions.go:1470` — `e.logger.Warn("publish_agent tool name not found in registry; dropped",`
- `processor/rule/expression_factory.go:161` — `if r.cooldown > 0 && time.Since(r.lastTriggered) < r.cooldown {`
- `processor/rule/expression_factory.go:219` — `if r.cooldown > 0 && time.Since(r.lastTriggered) < r.cooldown {`
`Register.*submit_work` — zero hits repo-wide (confirmed, see Searches)
Refusal and observation:
- `processor/rule/message_handler.go:193` — `rp.logger.Debug("Rule matched but action gated by fire_every_n_events",`
- `processor/rule/message_handler.go:198` — `rp.metrics.actionGatePassesTotal.WithLabelValues(ruleName).Inc()`
- `processor/rule/actions.go:1470` — `e.logger.Warn("publish_agent tool name not found in registry; dropped",`
Nearest pattern instance:
admission gate —
- `processor/rule/message_handler.go:198` — `rp.metrics.actionGatePassesTotal.WithLabelValues(ruleName).Inc()`

## #1041 — rule: an entity-watcher start failure permanently disables message-path rule evaluation too
Named sites:
- `processor/rule/entity_watcher.go:42` — `if err := rp.startWatcherForBucketPattern(ctx, bucketName, pattern); err != nil {`
- `processor/rule/entity_watcher.go:43` — `rp.markGraphStateGuardDegraded(fmt.Errorf("start %s pattern %q: %w", bucketName, pattern, err))`
- `processor/rule/entity_watcher.go:44` — `return errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeIndexNotReady, err)`
- `processor/rule/message_handler.go:80` — `func (rp *Processor) evaluateRulesForMessage(ctx context.Context, subject string, msg message.Message) {`
- `processor/rule/message_handler.go:81` — `if !rp.graphRuleEvaluationReady() {`
- `processor/rule/entity_watcher.go:71` — `return !rp.graphStateResetRequired.Load() && !rp.graphStateGuardDegraded.Load()`
- `processor/rule/processor.go:521` — `case rp.graphStateGuardDegraded.Load():`
Refusal and observation:
- `processor/rule/entity_watcher.go:44` — `return errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeIndexNotReady, err)`
coded: Classified family
processor/rule/message_handler.go:81-82 — the message-lane early return itself carries no errs. call, no log, and no metric (silent latch consumption)
Nearest pattern instance:
authority delegation —
- `processor/rule/actions.go:633` — `err := semtypes.ValidateEntityIDAuthority(entityID, e.platform.Org, e.platform.Platform, false)`

## #1042 — rule: per-rule entity.watch_buckets is parsed and validated but never drives a watcher
Named sites:
- `processor/rule/config_validation.go:472` — `def.Entity.WatchBuckets = make([]string, 0, len(buckets))`
- `processor/rule/config_validation.go:478` — `def.Entity.WatchBuckets = append(def.Entity.WatchBuckets, s)`
- `processor/rule/entity_pattern_contract.go:50` — `for _, bucket := range def.Entity.WatchBuckets {`
- `processor/rule/entity_pattern_contract.go:56` — `if len(def.Entity.WatchBuckets) > 0 {`
- `processor/rule/cron_rule.go:185` — `if len(def.Entity.WatchBuckets) > 0 {`
- `processor/rule/cron_rule.go:186` — `rejected = append(rejected, "entity.watch_buckets")`
- `processor/rule/entity_watcher.go:76` — `return rp.config.EntityWatchBuckets`
- `processor/rule/runtime_config.go:87` — `err := rp.updateWatchBucketsWithFactory(ctx, buckets, rp.prepareEntityWatcher)`
- `processor/rule/config.go:41` — `EntityWatchBuckets map[string][]string `json:"entity_watch_buckets" schema:"type:object,description:ENTITY_STATES patterns for the typed EntityState evaluator,category:advanced"``
Refusal and observation:
(none — see Searches; the rule-level field is validated for grammar only, at entity_pattern_contract.go, with no errs./log/metric tied to the field's non-use)
Nearest pattern instance:
classified refusal + observed signal —
- `processor/rule/entity_pattern_contract.go:68` — `return errs.ClassifiedCodeDetail(`

## #1043 — rule: $prev.* transition conditions silently no-op when the evaluation has no EntityState
Named sites:
- `processor/rule/stateful_evaluator.go:272` — `if !hasTransitionConditions(ruleDef) || entity == nil {`
- `processor/rule/stateful_evaluator.go:273` — `return currentlyMatching`
- `processor/rule/stateful_evaluator.go:136` — `prevState, err := e.stateTracker.Get(ctx, ev.Rule.ID, entityKey)`
- `processor/rule/stateful_evaluator.go:155` — `if ev.Revision > 0 && hadPrevState && prevState.SourceRevision >= ev.Revision {`
Refusal and observation:
processor/rule/stateful_evaluator.go:272-273 — the `entity == nil` short-circuit itself carries no errs. call, no log, no metric
- `processor/rule/stateful_evaluator.go:299` — `e.logger.Warn("Failed to re-evaluate transition conditions",`
a different branch of the same function — the evaluator-error path, not the entity==nil path
Nearest pattern instance:
(none — see Searches)

## #1049 — rule: rule-opacity is enforced on conditions only — action templates can emit opaque predicate values unguarded
Named sites:
- `processor/rule/config_validation.go:210` — `// match structural facts only. Vocabulary marks the predicate`
- `processor/rule/config_validation.go:214` — `if field != "" && vocabulary.IsRuleOpaque(field) {`
- `processor/rule/config_validation.go:298` — `if vocabulary.IsRuleOpaque(c.Field) {`
- `processor/rule/typed_substitution.go:70` — `// typedEntityTripleRe matches `$entity.triple.<predicate>` when the`
- `processor/rule/message_substitution.go:7` — `// The existing `$entity.*` / `$related.*` / `$state.*` / `$caller.*` /`
- `processor/rule/message_substitution.go:16` — `// `$entity.triple.X` precedent.`
- `vocabulary/agentic/register.go:94` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/agentic/register.go:99` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/agentic/register.go:104` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/agentic/register.go:156` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/agentic/register.go:174` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/agentic/register.go:544` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/agentic/register.go:549` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/agentic/register.go:554` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/agentic/register.go:559` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/governance/register.go:33` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/governance/register.go:38` — `vocabulary.WithRuleOpaque(true))`
- `vocabulary/registry.go:568` — `func IsRuleOpaque(predicate string) bool {`
Note: the issue's original citations of `vocabulary/agentic/register.go:540,545,550,555` have moved to `:544,549,554,559` at base (content confirmed identical: `WithRuleOpaque(true))` following the WebTitle/WebSnippet/WebText/WebSourceQuery registrations).
Refusal and observation:
- `processor/rule/config_validation.go:215` — `return errs.WrapInvalid(`
Nearest pattern instance:
create-vs-exists —
- `processor/rule/kv_config_integration.go:394` — `if _, err := kvStore.Create(ctx, key, data); err != nil {`

## #1170 — rule: Start warn-swallows a state-tracker failure that leaves NO action executor — the processor boots healthy and dispatches nothing
Named sites:
- `processor/rule/processor.go:673` — `func (rp *Processor) initializeStateTracker(ctx context.Context) error {`
- `processor/rule/processor.go:691` — `return errs.WrapInvalid(`
- `processor/rule/processor.go:886` — `func (rp *Processor) initializeCronScheduler() error {`
- `processor/rule/processor.go:888` — `return fmt.Errorf("cannot initialize cron scheduler: action executor not initialized")`
- `processor/rule/processor.go:973` — `if err := rp.initializeStateTracker(ctx); err != nil {`
- `processor/rule/processor.go:974` — `rp.logger.Warn("Failed to initialize state tracker, stateful rules will be disabled", "error", err)`
- `processor/rule/processor.go:975` — `// Don't fail - processor can still work with stateless rules`
Refusal and observation:
- `processor/rule/processor.go:691` — `return errs.WrapInvalid(`
uncoded: WrapInvalid family; this is the authority-precondition refusal INSIDE initializeStateTracker, distinct from the Warn-swallow at :974
- `processor/rule/processor.go:974` — `rp.logger.Warn("Failed to initialize state tracker, stateful rules will be disabled", "error", err)`
Nearest pattern instance:
classified refusal + observed signal —
- `processor/rule/entity_pattern_contract.go:68` — `return errs.ClassifiedCodeDetail(`

## #1206 — rule: $caller.* substitution has no production populator — a shipped policy DSL for a caller that never exists
Named sites:
- `processor/rule/caller_substitution.go:37` — `type CallerContext struct {`
- `processor/rule/execution_context.go:150` — `Caller *CallerContext`
- `processor/rule/execution_context.go:225` — `//   - $caller.id: Caller identity ID (caller-aware rules only)`
- `processor/rule/execution_context.go:360` — `// Caller substitutions. No-op when ec.Caller is nil; unknown`
- `processor/rule/execution_context.go:363` — `result = applyCallerSubstitutions(result, ec.Caller)`
`CallerContext{` construction — zero hits repo-wide outside `_test.go` files (confirmed, see Searches)
Refusal and observation:
(none — see Searches; a nil `ec.Caller` is a documented no-op in the substitution layer, with no errs./log/metric marking the absence)
Nearest pattern instance:
authority delegation —
- `processor/rule/actions.go:633` — `err := semtypes.ValidateEntityIDAuthority(entityID, e.platform.Org, e.platform.Platform, false)`

## Adjacent claims
- #383: none of the five draft PRs (1141, 1156, 1159, 1254, 1297) name it
- #746: none of the five draft PRs name it; body names #683, #697, #713
- #1007: none of the five draft PRs name it
- #1041: none of the five draft PRs name it
- #1042: none of the five draft PRs name it
- #1043: none of the five draft PRs name it
- #1049: none of the five draft PRs name it; body names #1045; body names the in-flight change `rule-readable-payload-projection` (not found under `openspec/changes/` at base — see Searches)
- #1170: none of the five draft PRs name it; body names #1148
- #1206: none of the five draft PRs name it; body names #1144
- `docs/adr/032-policy-tenancy-cluster.md:1` — `# ADR-032: Policy DSL, Multi-Tenant Identity, and Cluster Substrate`
cited by #1206
- `docs/adr/036-agent-private-observable-state.md:1` — `# ADR-036: Agent-Private Observable State`
cited by #1049, and by name in config_validation.go comments
- `docs/adr/039-tool-call-governance-rule-driven.md:1` — `# ADR-039: Tool-Call Governance is Rule-Driven`
cited by #1041, #1043
- `docs/adr/041-unified-condition-evaluator.md:1` — `# ADR-041: Unified Condition Evaluator — Rule-Level + Action-When Share Field Resolution`
cited by #746, via the research-graph rule description
- `docs/adr/043-prompt-injection-defense-detonation-corpus.md:1` — `# ADR-043: Prompt-Injection Defense via Detonation Corpus + Embedding Classifier`
cited by #1049
- `docs/adr/045-graph-search-rule-chain.md:1` — `# ADR-045: Graph Search Decomp+Fusion via Rule-Chain + Components`
cited by #746, via the research-graph rule descriptions
- `docs/adr/047-lifecycle-harness-substrate.md:1` — `# ADR-047: Lifecycle Harness Substrate`
cited by #1043's evaluator lifecycle-field comment
- `docs/adr/088-readiness-is-per-producer-aggregation-is-the-consumers.md:1` — `# ADR-088: Readiness Is Per-Producer; Aggregation Belongs to the Consumer`
cited by #1042
- `docs/adr/091-graph-mutation-authority-without-semantic-ownership.md:1` — `# ADR-091: Graph Mutation Authority Without Semantic Ownership`
cited by #1170
- `docs/adr/102-entity-id-segment-semantics.md:1` — `# ADR-102: Entity-ID Positions Have Meanings; `platform` Is the Minting Deployment Authority`
cited by #1170's processor.go comments

## Searches
- `git grep -n "max_iterations\|MaxIterations" processor/rule/rule_factory.go` → 3
- `git grep -n "MaxIterations\|max_iterations" processor/rule/actions.go` → 30+ (head -30)
- `git grep -n "effectiveMaxIterations" processor/rule/*.go` → 4
- `git grep -n '\$state.max_iterations\|state\.max_iterations' processor/rule/*.go processor/rule/expression/*.go` → 8
- `git grep -n "errs\.ClassifiedCode\|errs\.Classified(" processor/rule/*.go` → 3
- `git grep -c "errs\.ClassifiedCode\|errs\.Classified(" processor/rule/*.go | grep -v ':0'` → 3 files
- `git grep -n "Authority\b" processor/rule/*.go` → 20+ (head -30)
- `git grep -n "^func.*) Create(\|^func Create(" processor/rule/*.go` → 4 (all test files)
- `git grep -ni "cache\|reconcile" processor/rule/*.go` → 30+ (head -30)
- `git grep -n "lifecycle\." processor/rule/*.go | grep -v _test.go` → 20+
- `git grep -n "unsupportedEntityWatchBucket\|ErrorCodeEntityWatchBucketUnsupported" processor/rule/*.go` → 6
- `git grep -ln "RunScope" processor/rule/*.go | grep -v _test.go` → 2
- `git grep -n "\.Create(ctx" processor/rule/*.go | grep -v _test.go` → 1
- `git grep -n "shouldFireAction\|Rule matched but action gated by fire_every_n_events\|actionGatePassesTotal\|func (rp \*Processor) evaluateRulesForMessage\|graphRuleEvaluationReady" processor/rule/message_handler.go` → 8
- `git grep -c "shouldFireAction\|FireEveryNEvents" processor/rule/stateful_evaluator.go` → 0
- `git grep -n "submit_work" processor/agentic-tools/categories.go` → 2
- `git grep -n "submit_work" processor/agentic-tools/emit_diagnosis.go` → 2
- `git grep -rn "Register.*submit_work" .` → 0
- `git grep -n "unknown tool\|dropping tool\|not registered\|unregistered tool" processor/rule/actions.go processor/agentic-tools/*.go` → 15+ (none matching the actual mechanism)
- `git grep -n "action.Tools\|\.Tools\b" processor/rule/actions.go` → 6
- `git grep -n "Warn(\"" processor/rule/actions.go` → 6
- `git grep -n "cooldown" processor/rule/expression_factory.go` → 10
- `git grep -n "TransitionExited\|DetectTransition" processor/rule/stateful_evaluator.go` → 3
- `git grep -n "startWatcherForBucketPattern\|markGraphStateGuardDegraded\|graphStateGuardDegraded\|graphStateResetRequired\|graphRuleEvaluationReady\|computeReadinessStatus" processor/rule/entity_watcher.go processor/rule/message_handler.go processor/rule/*.go | grep -v _test.go` → 20+
- `git grep -n "def.Entity.WatchBuckets\|Entity.WatchBuckets" processor/rule/config_validation.go` → 2
- `git grep -n "WatchBuckets" processor/rule/entity_pattern_contract.go` → 4
- `git grep -n "WatchBuckets" processor/rule/cron_rule.go` → 1
- `git grep -n "getEffectiveBucketPatterns\|EntityWatchBuckets" processor/rule/entity_watcher.go processor/rule/runtime_config.go processor/rule/config.go` → 11
- `find . -iname "runtime_config.go"` → 1
- `git grep -n "func reEvaluateTransitions\|hasTransitionConditions\|entity == nil" processor/rule/stateful_evaluator.go` → 3
- `git grep -n "Failed to re-evaluate transition conditions" processor/rule/stateful_evaluator.go` → 1
- `git grep -n "IsRuleOpaque" processor/rule/*.go` → 2
- `git grep -n "entity\.triple\." processor/rule/typed_substitution.go` → 6 (head -10)
- `find . -iname "message_substitution.go"` → 1
- `git grep -n "RuleOpaque" vocabulary/agentic/register.go` → 9
- `git grep -n "RuleOpaque\|governance.injection" vocabulary/governance/register.go` → 3
- `git grep -n "func IsRuleOpaque" vocabulary/*.go` → 1
- `git grep -n "func.*initializeStateTracker\|can still work with stateless rules\|func.*initializeCronScheduler\|action executor not initialized" processor/rule/processor.go` → 4
- `git grep -n "Failed to initialize state tracker, stateful rules will be disabled\|Don't fail - processor can still work with stateless rules\|if err := rp.initializeStateTracker(ctx)" processor/rule/processor.go` → 3
- `git grep -n "errs.WrapInvalid(\|has no deployment authority" processor/rule/processor.go` → 5 (head -5)
- `git grep -n "type CallerContext struct" processor/rule/caller_substitution.go` → 1
- `git grep -n "CallerContext{" -- . ':!*_test.go'` → 0
- `git grep -n "ec.Caller\|\.Caller \*CallerContext\|Caller  *\*" processor/rule/execution_context.go` → 3
- `git grep -n "func (d \*defaultTypeDetector) GetFieldValue\|triple.Predicate == field" processor/rule/expression/evaluator.go` → 4
- `find . -path "*research-graph*02-route-decision-dispatch.json"` → 1
- `git grep -n "research.assess.sufficient\|research.route.action\|candidate-count\|evidence-count\|degraded\|research.classify.complete\|research.route.complete\|research.execute.complete\|research.assess.complete" configs/rules/research-graph/*.json` → 20+ (head -20)
- `git grep -n "messageCache\.\|ruleCache\|\.Get(.*cache\|cache\.Get" processor/rule/*.go | grep -v _test.go` → 2
- `git grep -n "errs\.Classified\|Authority\|func.*Create(\|cache\|Cache" processor/rule/expression/*.go | grep -v _test.go` → 12 (head -15)
- `for p in 1141 1156 1159 1254 1297; do gh pr view $p --json number,body --jq '.body' | grep -nE '#(383|746|1007|1041|1042|1043|1049|1170|1206)\b'; done` → 0 (all five)
- `for n in 032 036 039 041 043 045 047 088 091 102; do find docs/adr -iname "${n}-*.md"; done` → 10/10 found
- `find openspec/changes -maxdepth 1 -iname "*rule-readable-payload-projection*"` → 0
- `git grep -n "rejected" processor/rule/cron_rule.go` → 10 (head -10)
- Final sed -n confirmation batch (17 single-line checks) against the searches above → all confirmed exact, no drift at base
NOT RUN — budget: a dedicated survey of `processor/rule/expression` and `vocabulary/` packages for a fifth/sixth-shape nearest-pattern-instance candidate beyond what surfaced incidentally (specifically for #1043, marked `(none — see Searches)` above); a check of whether any shipped `configs/rules/*.json` currently declares a `$prev.*` transition condition on a message-path rule (#1043's own "UNVERIFIED" note); a check of whether `agent.web.title` etc. (#1049 exposure list) are reachable via any shipped rule pack's action templates.

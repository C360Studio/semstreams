# External rule proposal-input replay supplement

base: c347eff487f50b93bc338d764f43ef5b5ea5e133

Parent inventory: `inventory-r7-governance-evidence-2026-09-14.md`, SHA-256
`96d9c252c96d938a19629cf7c59da95da67815f6e4b9f5a2d612d2bffe65904a`.

Scope: inventory only. Measure whether redelivery of an unchanged governance proposal reruns its required action.
No target, ownership expansion, implementation authority or new durable primitive is declared.

## Message and entity paths share an evaluator, not replay semantics

- `processor/rule/message_handler.go:99` — `if hasDefinition && ruleDef.Entity.Pattern != "" {`
- `processor/rule/message_handler.go:136` — `entityID := extractEntityID(msg)`
- `processor/rule/message_handler.go:139` — `// revision and no bootstrap phase, so those fields stay zero.`
- `processor/rule/message_handler.go:142` — `transition, err := rp.statefulEvaluator.Evaluate(ctx, Evaluation{`
- `processor/rule/message_handler.go:262` — `if !hasDefinition || ruleDef.Entity.Pattern == "" {`
- `processor/rule/message_handler.go:326` — `transition, err := rp.statefulEvaluator.Evaluate(ctx, Evaluation{`
- `processor/rule/message_handler.go:331` — `Revision:          snap.Revision,`
- `processor/rule/message_handler.go:332` — `Bootstrap:         bootstrap,`
- `processor/rule/message_handler.go:432` — `if genericPayload, ok := payload.(*message.GenericJSONPayload); ok {`
- `processor/rule/message_handler.go:433` — `if entityID, exists := genericPayload.Data["entity_id"]; exists {`
- `processor/rule/message_handler.go:435` — `return id`
- `processor/rule/message_handler.go:442` — `return msg.ID()`
- `processor/agentic-loop/governance_dispatcher.go:643` — `baseMsg := message.NewBaseMessage(generic.Schema(), generic, source)`
- `processor/agentic-loop/governance_dispatcher.go:644` — `return json.Marshal(baseMsg)`
- `message/base_message.go:124` — `id:      uuid.New().String(),`
- `message/base_message.go:243` — `ID:      m.id,`
- `message/base_message.go:268` — `m.id = wire.ID`

The same retained source bytes preserve the message identity used for state tracking. Message-path evaluation does
not invoke KV bootstrap recovery. The shared evaluator has two production callers: message and entity evaluation;
altering its unconditional behavior reaches both.
Republishing through this proposal builder mints a new message ID even for the same ExecutionID, whereas reusing
the retained source bytes preserves the state key. The parent inventory pins the complete proposal payload;
it supplies no `entity_id` override.

## Persisted match selection can suppress redelivery

- `processor/rule/stateful_evaluator.go:136` — `prevState, err := e.stateTracker.Get(ctx, ev.Rule.ID, entityKey)`
- `processor/rule/stateful_evaluator.go:144` — `wasMatching = prevState.IsMatching`
- `processor/rule/stateful_evaluator.go:155` — `if ev.Revision > 0 && hadPrevState && prevState.SourceRevision >= ev.Revision {`
- `processor/rule/stateful_evaluator.go:170` — `transition := DetectTransition(wasMatching, currentlyMatching)`
- `processor/rule/stateful_evaluator.go:177` — `if ev.Bootstrap && hadPrevState && transition == TransitionNone && currentlyMatching &&`
- `processor/rule/stateful_evaluator.go:200` — `IsMatching:       currentlyMatching,`
- `processor/rule/stateful_evaluator.go:328` — `actions = ruleDef.OnEnter`
- `processor/rule/stateful_evaluator.go:348` — `actions = ruleDef.WhileTrue`
- `processor/rule/state_tracker.go:92` — `if !wasMatching && nowMatching {`
- `processor/rule/state_tracker.go:93` — `return TransitionEntered`
- `processor/rule/state_tracker.go:98` — `return TransitionNone`
- `processor/rule/state_tracker.go:119` — `entry, err := st.bucket.Get(ctx, key)`
- `processor/rule/state_tracker.go:157` — `_, err = st.bucket.Put(ctx, key, data)`

The parent inventory already pins `runActions` error swallowing, the subsequent `StateTracker.Set`, and successful
return. Together these establish the concrete replay problem: a failed first OnEnter action can leave a persisted
matching state; redelivery with the same match selects WhileTrue rather than OnEnter.

## Action counters and the separate event leg

- `processor/rule/stateful_evaluator.go:183` — `iteration := prevState.Iteration`
- `processor/rule/stateful_evaluator.go:185` — `iteration++`
- `processor/rule/stateful_evaluator.go:192` — `actionIterations := prevState.ActionIterations`
- `processor/rule/stateful_evaluator.go:207` — `ActionIterations: actionIterations,`
- `processor/rule/stateful_evaluator.go:380` — `match, whenErr := e.evaluateWhen(ctx, action.When, entity, stateFields, messageFields)`
- `processor/rule/stateful_evaluator.go:387` — `continue`
- `processor/rule/stateful_evaluator.go:406` — `actionID := action.effectiveID(ruleDef.ID)`
- `processor/rule/stateful_evaluator.go:408` — `fired := ec.State.ActionIterations[actionID]`
- `processor/rule/stateful_evaluator.go:409` — `if !isUnlimited(maxIter) && fired >= maxIter {`
- `processor/rule/stateful_evaluator.go:423` — `ec.State.ActionIterations[actionID] = fired + 1`
- `processor/rule/action_id.go:17` — `const DefaultActionMaxIterations = 3`
- `processor/rule/action_id.go:38` — `if a.ID != "" {`
- `processor/rule/action_id.go:39` — `return a.ID`
- `processor/rule/action_id.go:41` — `return a.fingerprint(ruleID)`
- `processor/rule/action_id.go:122` — `if a.MaxIterations == nil {`
- `processor/rule/action_id.go:123` — `return DefaultActionMaxIterations, true`
- `processor/rule/action_id.go:125` — `return *a.MaxIterations, false`
- `processor/rule/action_id.go:132` — `return maxIter == 0`
- `processor/rule/message_handler.go:164` — `rp.fireRuleActions(ctx, ruleName, hasDefinition, ruleDef, counters, ruleInstance, messages)`
- `processor/rule/message_handler.go:192` — `if counter == nil || !shouldFireAction(n, counter) {`
- `processor/rule/message_handler.go:204` — `events, err := ruleInstance.ExecuteEvents(messages)`
- `processor/rule/expression_factory.go:161` — `if r.cooldown > 0 && time.Since(r.lastTriggered) < r.cooldown {`
- `processor/rule/expression_factory.go:363` — `r.lastTriggered = time.Now()`
- `processor/rule/expression_factory.go:364` — `r.shouldTrigger = false`

Action counters increment before execution and are included in the post-action state write already inventoried.
Action-When evaluation errors skip actions. Separately, FireEveryNEvents gates the later event leg, not the preceding
stateful action call. Expression cooldown can make a subsequent message evaluation nonmatching after ExecuteEvents
advances its timestamp. These are distinct gates, not one interchangeable retry flag.
The counter key is the explicit action ID or its existing rule-scoped fingerprint. Omission defaults the cap to
three; explicit zero means unlimited.

## Shipped and documented governance action sets

- `configs/agentic.json:266` — `"id": "governance-approve-all-audit",`
- `configs/agentic.json:292` — `"on_enter": [`
- `configs/agentic.json:294` — `"type": "approve",`
- `configs/agentic.json:295` — `"subject": "agent.toolcall.approved.$message.execution_id",`
- `docs/operations/17-tool-call-governance.md:134` — `twice. The ADR-039 canonical pattern uses `publish` + `deny` for`
- `docs/operations/17-tool-call-governance.md:164` — `{"type": "deny", "reason": "writes outside worktree blocked"}`
- `docs/operations/17-tool-call-governance.md:197` — `short-circuit subsequent actions — operators may want to add metric-counter`
- `docs/operations/17-tool-call-governance.md:218` — `"when": [{"field": "$message.command", "operator": "contains", "value": "cd /workspace"}],`
- `docs/operations/17-tool-call-governance.md:230` — `"type": "deny",`
- `docs/operations/17-tool-call-governance.md:254` — `"subject": "agent.toolcall.approved.$message.execution_id",`
- `docs/operations/17-tool-call-governance.md:270` — `- Actions inside a rule run sequentially in declared order`
- `docs/operations/17-tool-call-governance.md:271` — `- `deny` short-circuits remaining actions in the same firing (returns`

The shipped audit rule has only OnEnter approve; no WhileTrue retry action is present. The documented rejection
shape publishes first and then denies. The documented consolidated shape has conditional publish/deny pairs and a
trailing approval. Approve may also precede additional actions. Therefore the proposal-input boundary encounters
ordered and potentially partially completed action lists, not just a single isolated publication.

The parent inventory preserves the distinction between required routing publication and best-effort
`GOVERNANCE_VERDICT_AUDIT` emission; the audit record is not routing completion or recovery authority.

## Adjacent constraints

- `openspec/specs/rule-projection-mutations/spec.md:86` — `one mutation request. A definite `revision_mismatch` MUST remain a visible classified action failure; the action MUST`
- `openspec/specs/rule-projection-mutations/spec.md:87` — `NOT replay or recompute the old `ExecutionContext`. `commit_unknown` MUST NOT be automatically retried. Successful`
- `openspec/specs/rule-projection-mutations/spec.md:88` — `receipts MUST retain the exact committing revision. No retry helper, knob, loop, or coordinator is part of this`
- `openspec/specs/rule-action-observability/spec.md:35` — `The `rule_events` output SHALL remain an optional rule-trigger notification. When a rule processor has no`
- `openspec/specs/rule-action-observability/spec.md:37` — `Absence SHALL NOT prevent admitted rule actions from executing or graph events from using their existing delivery`
- `openspec/specs/rule-engine/spec.md:139` — `The stateful evaluation path's behavior SHALL NOT change. A rule pack that evaluates today MUST`

The last sentence belongs to the stateless-matching requirement, not an unconditional prohibition against future
rule-engine changes. Nevertheless, no change to existing stateful behavior is authorized merely by implementing
that stateless API or by this inventory. Reconcile's explicit no-replay contract remains a constraint on any
generic message-action retry proposal.

Parent inventory ownership constraints remain: the rule input is outside the frozen #1146 fifteen subscriptions
and #759 nine heartbeat bindings; #935 is open, post-v1 and unassigned to a milestone by this work.

## Existing problem shape and owners

The shape is source settlement after an ordered required consequence, with replay-sensitive state written by the
existing evaluator. The parent inventory already records `natsclient.SettleDelivery` adoption on governance
validation and the existing rule consumer/action publisher. StateTracker owns match/action counters; rule actions
own routing effects; the separate auditor owns best-effort audit events.

No new durable, communication or coordination primitive is proposed by this supplement. There is consequently no
new consumer-at-birth or establishing-pattern adoption sweep.

## Adopter seam

Specific adopter: an author of an enforce-mode governance rule using the documented publish/deny or approve shapes,
without reading the rule evaluator.

**Must know today:** ordered action selection, identity-keyed match transitions, and firing caps can affect whether
the same proposal produces another verdict. These are more than two correctness facts beyond writing policy.

**Does nothing:** the shipped OnEnter-only form can persist matching state after failed publication; the source is
acknowledged, and a hypothetical NAK alone would not restore the failed OnEnter selection.

**Finds out:** action failure is logged/counted; the loop can time out. No caller-visible settlement result tells
the rule author that MatchState recorded a firing whose required verdict publication failed.

**Should have to know:** the accepted governance-author seam says policy only, without waiter or replay mechanics
(parent inventory, design.md:958). The gap above is inventory evidence, not permission to introduce a new policy,
retry knob or state store.

No sister repository was searched or changed. No claim is made that every external rule pack has been enumerated.

## Searches and reads

Structural queries used the existing cache environment:

```sh
GOCACHE=/private/tmp/semstreams-r7-gopls-cache \
GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache \
gopls workspace_symbol -matcher=fuzzy extractEntityID

GOCACHE=/private/tmp/semstreams-r7-gopls-cache \
GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache \
gopls workspace_symbol -matcher=fuzzy StateTracker

GOCACHE=/private/tmp/semstreams-r7-gopls-cache \
GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache \
gopls references processor/rule/stateful_evaluator.go:133:29

GOCACHE=/private/tmp/semstreams-r7-gopls-cache \
GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache \
gopls workspace_symbol -matcher=fuzzy Action.effectiveMaxIterations

GOCACHE=/private/tmp/semstreams-r7-gopls-cache \
GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache \
gopls workspace_symbol -matcher=fuzzy ExpressionRule.Evaluate
```

Results identified the declarations pinned above and exactly two production Evaluate callers, at
message_handler.go:142 and :326; remaining references were tests.

Literal searches:

```sh
git grep -n -E 'agent\.toolcall\.proposed|agent\.toolcall\.approved|agent\.toolcall\.rejected' \
  -- configs processor/rule docs/operations | head -160

git grep -n -E 'cooldown|lastTriggered|last_triggered|fire_every_n_events|max_iterations|on_recovery|while_true|on_exit' \
  -- configs/agentic.json docs/operations/17-tool-call-governance.md processor/rule/expression_factory.go

rg --files openspec/specs docs/adr | rg '(rule|039-|055-|032-|028-|049-)'
rg --files docs/adr | rg '041|043|046|rule|iteration'
```

The governance literal search returned fewer than 160 lines; no complete-repository absence claim is made.
The second search found no governance rule declaration of cooldown, WhileTrue or recovery actions in those two
specific config/document paths; its config MaxIterations hit belongs to agentic-loop, not the inline rule.

Read windows:

- `stateful_evaluator.go`: 1–265, 305–525.
- `message_handler.go`: 1–305, 315–347, 423–505.
- `state_tracker.go`: 1–168.
- `processor.go`: 1135–1225.
- `action_id.go`: 12–34, 90–153.
- `expression_factory.go`: 151–213, 327–370.
- `actions.go`: 2040–2176.
- `stateful_evaluator_test.go`: 1376–1418.
- `configs/agentic.json`: 260–312.
- `docs/operations/17-tool-call-governance.md`: 130–322.
- Current rule-engine, rule-action-observability and rule-projection-mutations specs: complete.
- ADR-041: complete; prior complete ADR-039 and ADR-055 §3a readings reused.
- R7 validation checkpoint: complete; parent inventory additions inspected.

No tests ran. No repository or external writes occurred.

Root materialized the architect's handoff and these two bounded additions from independent inventory review.
The added message/action identity pins above are the reviewer's measured findings, not a new target or sweep.
Mechanical verification covers every pin after materialization.

## Source hashes

```text
bd4958b5b570cf937b35c34a85c6d1f583d4d3460a502748f41597968c9ee0cb  processor/rule/message_handler.go
d99301763e2ed76fde3e445284c1a62e09cc748e7810f105d109ed5b511c9053  processor/rule/stateful_evaluator.go
1a03a8605184276e0ee3e44f29209a3e5350426a31d18d61dfd12828adeafbf2  processor/rule/state_tracker.go
a39f7be3214cdc29bdeb5af87ea80d4dc2cf03779ebd315b6ba861ead14cb162  processor/rule/action_id.go
b2b772d367afc7aa5f0ba55950b545560f3176097819ed2551634312fb1a91cc  processor/rule/expression_factory.go
144a85fbcfd0dfe63f73cfd50ef4ca20344e635ea84ae2fd0fa83cbbf808f4d1  configs/agentic.json
498fe0f6e2cb6c6366ac005b2569d7214776a912c77d05497d315575e0011a83  docs/operations/17-tool-call-governance.md
33b7910c7acd0d3767ca720cb45dbc00d3c06ce2ae58a2e42c455c3711141cad  openspec/specs/rule-engine/spec.md
7bfed994a7fb7bb3d067bf9203bb260a56dd9ed989a31a2b2f2647b8970b56fb  openspec/specs/rule-action-observability/spec.md
0420ca6c39114f878966a7458828c493f0a40d1ad0a62b399336ed0a06178280  openspec/specs/rule-projection-mutations/spec.md
e78e8d4a8074f02ddfdf381245444419824ff53f12992120c3cbdc62754e477b  docs/adr/041-unified-condition-evaluator.md
9cd4605ac83bce3129a72ac202a9e2b41fb9656faef3801c9ca032cfd0a54184  message/base_message.go
4bcc9d02b38c8d57cecff74959a1ccf064bcf1a70898df2b4b8b3c78ca99c863  processor/agentic-loop/governance_dispatcher.go
```

## Review question

Does this bounded evidence sufficiently describe proposal-input action selection, persisted replay suppression,
partial publication consequences and neighboring no-replay contracts to frame ownership/scope options?

No target state is submitted before that inventory verdict.

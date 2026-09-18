# R8 retention-premise inventory

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Inventory only. No target, formula, new API, or weakening of accepted protections is proposed. Nil-publisher WIP is excluded. Existing intake and admission inventories remain the baseline; this supplement tests the retention premise.

## Boundary matrix

| Restart boundary | Durable evidence and exact absence behavior | What retention protects; what is not established |
|---|---|---|
| Dispatch task birth | Stable source-derived TaskID addresses the exact retained `agent.task` envelope. Present evidence supplies its original LoopID and bytes. Typed absence enters new-task preparation; without a selected continuation, that mints another UUID LoopID. | This is an identity boundary, not merely availability. Safety requires preserving the mapping while that source identity may be processed again, or a separately established absence permission. A consumer timing budget does not establish this. |
| Dispatch terminal route | Terminal's own persisted loop record supplies authoritative route. Its absence returns an error. Ancestor absence is different: exhaust durable ancestry/run links, then report `origin_unresolvable` and settle. | Own-record retention affects eventual terminal routing. Ancestor loss has an explicit existing degraded outcome; do not conflate it with an absent terminal's own record. No calculation here guarantees the record survives an arbitrarily late terminal. |
| Loop task | Current loop KV first. Terminal record suppresses further work. Missing record returns to ordinary task handling with the supplied LoopID. Existing nonterminal record plus absent retained request reconstructs an initial request from the task. | Neither absence path is universally fail-closed. Preservation of terminal/current evidence matters to avoiding repeated or reconstructed work. No measured relation currently demonstrates that all replayable task inputs expire before necessary authority. |
| Loop response | Current loop KV and exact current retained request, including RequestID correlation. Missing required authority/request returns an error rather than advancing. | Retention affects recoverability; present code refuses unsupported advancement. Retry is not guaranteed eventual recovery when evidence has permanently expired. |
| Loop tool result | Current loop KV, originating retained model response, current retained request, and ordered call/execution correlation. Missing required evidence returns an error. | Same safety/progress distinction as response handling. A surviving result can outlive its earlier request/response even when originally co-located. |
| Loop approval | Exact pending authority plus retained request/response. Missing evidence invokes a live StreamInfo policy/age check and rechecks unchanged KV authority. Proven absence completes with `continuation_unavailable`; unproven retention or changed authority retries. | Existing check uses actual loop age, not AckWait arithmetic. It establishes conditions for the negative settlement at that attempt, not eventual successful continuation. |
| Retained governance verdict | Exact approved/rejected subjects and proposal correlation. Found verdict reused; typed absence re-proposes unchanged execution identity to current policy. Unreadable/conflicting evidence is not absence. | Accepted typed-absence amendment permits reevaluation; preservation of the historical verdict is not a prerequisite for this permission. Other policy/publication safeguards remain separate. |
| Model retained response | Exact response subject resolved from model output port. Valid retained response ACKs without provider invocation; typed absence proceeds to provider execution. Read uncertainty retries; invalid evidence quarantines. | Owner-approved provider exception accepts reinvocation after absence. Finite response retention is not proof of exactly-once provider effects, nor required by this exception. |
| Tool completed outcome | Immutable `TOOL_CALL_OUTCOMES` KV record keyed by ExecutionID, validated against call correlation/fingerprint. Found record republishes without executor invocation. `ErrKeyNotFound` permits execution; other failures do not. | This evidence is catalog-owned **no-lifecycle KV**, not finite AGENT/TOOL stream retention. Crash before completed-record creation remains the explicitly accepted ambiguous external-effect window. |

## Source identity, ordering, and timing

1. Source and evidence streams are independently resolved from actual port definitions. Dispatch USER subscription uses its resolved input stream; task lookup uses the resolved `agent.task` output stream. Loop request evidence uses its output port; response/verdict evidence uses their respective input ports. Model response lookup uses its output port. Shipped co-location is not a general invariant.
2. Checked configuration declares AGENT and TOOL MaxAge 24h, USER MaxAge 1h; all three use `discard: old`. These are repository declarations, not observations of a running server.
3. An initial source-before-evidence publication establishes an ordering for those two original messages only. It does **not** establish that the same source identity cannot be republished later, nor that every replayed/re-emitted downstream input preserves that ordering.
4. Consequently a source-relative retention relationship, if established with source identity/republication rules, would be a different claim from a computed consumer-timer horizon. This inventory establishes neither relationship globally.
5. Dispatch USER has MaxDeliver 3; terminal consumers use MaxDeliver 0, heartbeat 10s, delayed retry 30s. Loop task/response/tool-result use component consumer values, 30m work timeout, and BackOff 30s/2m. Model honors port consumer policy and uses 30m message timeout. Tools use default AckWait 5m, heartbeat 5s, and message timeout 10m.
6. Those values govern active attempts, retry spacing, or work cancellation. None of these owners declares a maximum outage, restart delay, source republication age, or total elapsed recovery guarantee. No “horizon plus safety margin” derivation follows solely from the observed values.

## Existing checker shape

The approval path already reads actual StreamInfo and rejects unsafe conditions before converting absence into a terminal outcome: LimitsPolicy, DiscardNew, no evicting per-subject bound, no message TTL, and loop age inside MaxAge. It then rechecks the exact KV revision. This is an observation-based, attempt-local proof.

`CheckStreamBounds` is different: a pure creation-config check requiring finite MaxAge and MaxBytes. It is not a recovery-admission check. Catalog KV has its own observed no-lifecycle acquisition/verification owner. These are existing semantic owners; this inventory introduces no shared primitive or collision.

## Adopter seam inventory

A configuration author can override input/output stream declarations independently. Today, correctness-sensitive retention relationships are not established merely by choosing valid stream names or passing the ordinary finite-bounds check. Defaults also do not prove them: declared DiscardOld permits capacity eviction.

The existing user-visible outcomes differ by boundary: boot-time catalog retention enforcement; runtime Retry on missing authority; explicit failed continuation after proven approval absence; or permitted new/repeated work at typed-absence boundaries. The developer should not have to infer these differences from a formula combining timers. The unresolved evidence is which identity/replay obligations must be enforced for each boundary—not a measured elapsed recovery budget.

No new outward surface or migration is proposed.

## Verifier pins

### Dispatch and configuration

- `processor/agentic-dispatch/task_recovery.go:84` — `subject, streamName, err := dispatchTaskAddress(c.outputPortDefs(), taskID)`
- `processor/agentic-dispatch/task_recovery.go:90` — `retained, retainedData, found, err := c.readRetainedDispatchTask(ctx, streamName, subject)`
- `processor/agentic-dispatch/task_recovery.go:107` — `return preparedDispatchTask{task: retained, data: retainedData, subject: subject}, slot, true, nil`
- `processor/agentic-dispatch/task_recovery.go:119` — `loopID = uuid.NewString()`
- `processor/agentic-dispatch/task_recovery.go:151` — `return subject, stream.Name(), nil`
- `processor/agentic-dispatch/component.go:538` — `StreamName:    bindings.userMessage.streamName,`
- `processor/agentic-dispatch/component.go:543` — `MaxDeliver:    3,`
- `processor/agentic-dispatch/component.go:575` — `MaxDeliver:    0,`
- `processor/agentic-dispatch/component.go:580` — `ctx, agentCompleteCfg, 10*time.Second, terminalRetryPolicy,`
- `processor/agentic-dispatch/component.go:966` — `prepared, err = c.prepareNewDispatchTask(ctx, msg, loopID, vacant)`
- `processor/agentic-dispatch/component.go:982` — `if err := c.natsClient.PublishToStream(ctx, prepared.subject, prepared.data); err != nil {`
- `processor/agentic-dispatch/terminal_settlement.go:114` — `return nil, fmt.Errorf("loop state %q not yet observable: %w", loopID, err)`
- `processor/agentic-dispatch/terminal_settlement.go:261` — `if resolved.reason == reasonOriginUnresolvable {`
- `processor/agentic-dispatch/terminal_settlement.go:266` — `return nil`
- `configs/agentic.json:19` — `"max_age": "24h",`
- `configs/agentic.json:21` — `"discard": "old"`
- `configs/agentic.json:44` — `"max_age": "24h",`
- `configs/agentic.json:52` — `"max_age": "1h",`
- `configs/agentic.json:54` — `"discard": "old"`

### Loop authority and approval

- `processor/agentic-loop/settlement_recovery.go:162` — `evaluated, err = dispatcher.Propose(ctx, loopID, parentLoopID, absent)`
- `processor/agentic-loop/settlement_recovery.go:388` — `if !found {`
- `processor/agentic-loop/settlement_recovery.go:389` — `return HandlerResult{}, nil`
- `processor/agentic-loop/settlement_recovery.go:398` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:421` — `request = c.handler.newTaskRequest(entity.ID, task, messages, tools)`
- `processor/agentic-loop/settlement_recovery.go:522` — `return agentic.LoopEntity{}, 0, fmt.Errorf("request for loop %q is not yet observable", loopID)`
- `processor/agentic-loop/settlement_recovery.go:575` — `return "", 0, fmt.Errorf("loop %q is not yet observable", result.LoopID)`
- `processor/agentic-loop/settlement_recovery.go:582` — `return "", 0, fmt.Errorf("originating response %q is not yet observable", result.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:929` — `info, err := stream.Info(ctx)`
- `processor/agentic-loop/settlement_recovery.go:937` — `age := time.Since(entity.StartedAt)`
- `processor/agentic-loop/settlement_recovery.go:938` — `if retention.Retention != jetstream.LimitsPolicy || retention.Discard != jetstream.DiscardNew ||`
- `processor/agentic-loop/settlement_recovery.go:939` — `(retention.MaxMsgsPerSubject > 0 && !retention.DiscardNewPerSubject) || retention.AllowMsgTTL ||`
- `processor/agentic-loop/settlement_recovery.go:940` — `entity.StartedAt.IsZero() || age < 0 || (retention.MaxAge > 0 && age >= retention.MaxAge) {`
- `processor/agentic-loop/settlement_recovery.go:948` — `if revision == 0 || observed != revision || !reflect.DeepEqual(current, entity) {`
- `processor/agentic-loop/settlement_recovery.go:971` — `failure, messages, err := c.handler.BuildFailureMessages(entity.ID, "continuation_unavailable", errorMsg)`
- `processor/agentic-loop/component.go:1048` — `msgTimeout = 30 * time.Minute`
- `processor/agentic-loop/component.go:1049` — `backOff = []time.Duration{30 * time.Second, 2 * time.Minute}`

### Model, completed outcomes, and existing retention owners

- `processor/agentic-model/component.go:616` — `_, found, err := c.readRetainedAgentResponse(ctx, req.RequestID)`
- `processor/agentic-model/component.go:628` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-model/component.go:636` — `client, endpoint, capability, endpointName, err := c.getClientForRequest(req)`
- `processor/agentic-model/component.go:389` — `MessageTimeout: 30 * time.Minute,`
- `processor/agentic-tools/component.go:32` — `defaultToolsAckWait           = 5 * time.Minute`
- `processor/agentic-tools/component.go:33` — `defaultToolsHeartbeatInterval = 5 * time.Second`
- `processor/agentic-tools/component.go:418` — `MessageTimeout: 10 * time.Minute,`
- `processor/agentic-tools/component.go:713` — `return c.publishCompletedResult(ctx, call, outcome.Result, outcomePathReplay)`
- `processor/agentic-tools/component.go:760` — `result, err := c.executeWithPanicRecovery(ctx, call)`
- `processor/agentic-tools/component.go:773` — `if err := c.persistAndPublishOutcome(ctx, call, result, outcomePathNew, true); err != nil {`
- `processor/agentic-tools/component.go:805` — `data, err := c.outcomes.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/component.go:806` — `if errors.Is(err, jetstream.ErrKeyNotFound) {`
- `graph/kvcatalog.go:40` — `noLifecycle := natsclient.RetentionPolicy{Kind: natsclient.RetentionNoLifecycle}`
- `graph/kvcatalog.go:47` — `Retention:   noLifecycle,`
- `graph/kvcatalog.go:150` — `owned(BucketToolCallOutcomes, "agentic-tools",`
- `graph/kvcatalog.go:249` — `return natsclient.EnsureFrameworkBucket(ctx, client, spec)`
- `natsclient/kv_retention.go:30` — `info, err := stream.Info(ctx)`
- `natsclient/kv_retention.go:35` — `return info.Config.MaxAge, info.Config.MaxBytes, nil`
- `natsclient/kv_retention.go:117` — `if err := CheckNoLifecycleRetention(bucket, maxAge, maxBytes); err != nil {`
- `natsclient/stream_bounds.go:82` — `func CheckStreamBounds(cfg jetstream.StreamConfig, source string) error {`
- `natsclient/stream_bounds.go:88` — `if cfg.MaxAge <= 0 {`
- `natsclient/stream_bounds.go:91` — `if cfg.MaxBytes <= 0 {`

## Searches and reading record

Read-only, no tests or live NATS observation.

- Reused complete current proposal/design/tasks and selected R7/R8 admission inventories/reviews; nil-publisher slice separately reviewed and excluded.
- `gopls workspace_symbol -matcher=fuzzy recoverCommittedTask` returned no exact locator; no absence inference.
- `gopls workspace_symbol -matcher=fuzzy terminalRoute`, `StreamPolicy`, `readLoopEntityRevision`, `retainedResponse`, `outcomeStore`, `EnsureCatalogBucket`; `StreamPolicy` output truncated and used only as a locator.
- `gopls workspace_symbol -matcher=caseSensitive handleRequest`.
- `gopls references` on dispatch `findRetainedDispatchTask`, tools `toolCallOutcomeKey`, and graph `BucketToolCallOutcomes`; followed production callers.
- `git grep -n` for `horizon|retention|absence|Discard|MaxAge` in active capability deltas.
- `git grep -n` for `TOOL_CALL_OUTCOMES`, outcome TTL/retention spellings, `StreamInfo`, `DiscardNew`, `LimitsPolicy`, `AckWait:`, `MessageTimeout:`, `MaxDeliver:`, and tool timing constants in selected production paths.
- An unquoted `natsclient/bucket*.go` glob failed under zsh; replaced with quoted tracked-file pathspec searches. It contributes no absence claim.
- Read located ranges in dispatch task recovery/callers/terminal settlement; loop recovery/task handler/consumer policy; model retained-response lookup/handler/policy; tools completed-outcome implementation/startup/handler; catalog and KV retention; ordinary stream bounds; shipped agentic stream configuration.

## Open evidence questions

1. Dispatch task mapping: what bounds replay or republication of the same source identity relative to its retained task evidence? Current timer values do not answer this.
2. Loop task authority: what establishes preservation of current/terminal KV and request evidence while a task remains processable? Current task absence behavior differs from response/tool-result behavior.
3. Which source-relative retention relationships remain invariant under configured stream separation and permitted republication? Initial causal order alone is insufficient.
4. A finite recovery promise additionally needs an owner-defined elapsed recovery bound. None was measured in these consumers.
5. Existing accepted R8 wording still requires a computed local horizon/safety margin; this inventory supplies evidence for reviewing that premise, not authorization to remove it.

Stop here for independent inventory review.

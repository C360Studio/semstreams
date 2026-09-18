# R8 loop-task reachability supplement

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Read-only source analysis. No tests, edits, or new design. The independently reviewed unit RED proves the unsafe reconstruction branch, not its production reachability.

## Finding

The missing-request branch is reachable under configuration accepted by current code. The simplest case is an independently configured request stream with shorter retention than the task stream and loop KV. That is principally evidence for unfinished admission.

However, merely requiring equal task/request MaxAge does not resolve the proof: first-party task republication can make a task newer than both its latest request and subsequently written nonterminal authority.

### Smallest current-configuration witness

Use existing port overrides:

1. Task stream: LimitsPolicy, DiscardNew, MaxAge 24h, non-evicting capacity behavior.
2. Request stream: LimitsPolicy, DiscardNew, MaxAge 1m.
3. Loop KV: the actually enforced History 10, TTL 24h, nonbinding MaxBytes.
4. Normal consumer configuration.

Sequence:

1. Dispatch publishes task `T`; loop processes it and advances through tool-result rounds.
2. Loop persists progressed nonterminal authority and publishes its latest request.
3. Loop stops. Dispatch redelivers the original USER source and republishes the retained, unchanged task `T`.
4. Request expires naturally; republished task and progressed loop authority remain.
5. Cold task handling reads that authority, receives typed request absence, constructs original-prompt/Iteration1 messages, and publishes the reconstructed request.

No purge, fabricated source identity, or arbitrary caller resubmission is needed. Current setup resolves and binds each input stream; it does not implement the remaining cross-stream retention admission.

**Classification:** source-backed reachability under currently accepted configuration; not proof that the final admitted R8 contract should accept this configuration. The developer's real-NATS dispatch evidence has the same limitation.

## Why equal MaxAge alone is insufficient

A second source-derived ordering witness uses **24h for both task and request streams**, plus the enforced KV TTL24h:

| Relative time | Production event |
|---|---|
| 00:01 | Latest progressed request is published. |
| 00:02 | Its model response requests at least one ordinary approved tool. The loop persists updated nonterminal authority before publishing tool work; no next request exists yet. Stop the loop before that tool batch completes. |
| 00:03 | Dispatch replacement redelivers the original USER input and republishes its exact retained task. The existing loop consumer is offline, so this new task copy remains pending/unconsumed. |
| Next day 00:01:30 | Latest request has expired; the later nonterminal KV write and republished task have not. Cold task processing takes the missing-request reconstruction branch. |

The times are an illustrative scheduling witness derived from production ordering, **not a native reproduction**. This does not promise successful recovery after that outage. It shows why expiry may require refusal rather than reconstruction even while source and authority remain retained.

Initial task-before-request ordering does not survive dispatch's permitted republication. A static `request MaxAge >= task MaxAge` comparison therefore does not, by itself, prove this boundary.

The loop's ordinary timeout is checked when handling model/tool responses; task recovery itself builds the replacement request before those handlers run. Its existence is not an established absence-classification guard.

## Existing authority does not fully classify partial birth

There is no reviewed “Iterations equals zero means unpublished birth” invariant.

1. New authority starts `running`, `Iterations=0`.
2. Completing a tool batch increments iterations.
3. Handling a model tool-call response can persist nonterminal `running` authority without incrementing iterations or producing another request.
4. Therefore, `running/Iterations=0` can describe partial birth **or** first-round work already dispatched.
5. Terminal state, pending approval, and positive progress supply useful positive evidence. They do not make the complement a reliable partial-birth test.
6. Existing tool-batch recovery also adjusts iterations for a pre-publication replay; the counter is not a dedicated publication-state marker.

Accordingly, the unit RED must not be “fixed” by assuming an unproved Iterations guard.

## Correction ownership, not a new design

The strongest case for admission-only correction is substantial: the short-request-stream witness is an unsafe source/evidence relationship that unfinished R8 admission was already intended to refuse.

The equal-retention republication witness prevents declaring that obligation complete through a simple age comparison. Any next correction must remain at the existing owners:

1. Admission must account for the actual source/evidence dependency and internal republication ordering.
2. If admitted operation can still present progressed authority without its required request, loop task recovery must not reinterpret that evidence loss as partial birth.

This supplement selects neither implementation. It warrants the bounded review of the concrete branch and ordering, not new state, IDs, clocks, storage, or a recovery runtime.

## Verifier pins

### Production republication and independently resolved evidence

- `processor/agentic-dispatch/component.go:974` — `// Retained evidence bypasses mutable continuation inference and admission:`
- `processor/agentic-dispatch/component.go:982` — `if err := c.natsClient.PublishToStream(ctx, prepared.subject, prepared.data); err != nil {`
- `processor/agentic-dispatch/component.go:991` — `if err := c.sendResponse(ctx, agentic.UserResponse{`
- `processor/agentic-loop/component.go:996` — `streamName := stream.Name()`
- `processor/agentic-loop/component.go:1003` — `if err := waitForStream(setupCtx, streamName); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:73` — `func agentRequestAddress(definitions []component.PortDefinition, loopID string) (string, string, error) {`
- `processor/agentic-loop/settlement_recovery.go:64` — `if errors.Is(err, jetstream.ErrMsgNotFound) {`

### Progress and authority ordering

- `processor/agentic-loop/handlers.go:2566` — `err := h.loopManager.IncrementIteration(loopID)`
- `processor/agentic-loop/handlers.go:2641` — `messages = h.prependIterationContext(ctx, loopID, newIteration, entity.MaxIterations, messages)`
- `processor/agentic-loop/handlers.go:2674` — `requestSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.request", loopID)`
- `processor/agentic-loop/handlers.go:1318` — `if err := h.handleToolCallResponse(ctx, &result, loopID, response.RequestID, response.Message.ToolCalls, propose); err != nil {`
- `processor/agentic-loop/handlers.go:1326` — `if h.loopManager.AllToolsComplete(loopID) {`
- `processor/agentic-loop/component.go:1618` — `if err := c.persistHandlerResult(ctx, result, revision); err != nil {`
- `processor/agentic-loop/component.go:1795` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1798` — `return c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:2411` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
- `processor/agentic-loop/internal/loopbucket/acquire.go:42` — `if status.History() != 10 || status.TTL() != 24*time.Hour || info.Config.MaxAge != 24*time.Hour || info.Config.MaxBytes > 0 {`

### Reconstruction and partial-birth ambiguity

- `processor/agentic-loop/component.go:1311` — `result, err = c.recoverTaskDelivery(ctx, *task)`
- `processor/agentic-loop/settlement_recovery.go:398` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:402` — `request, retained, err := c.readRetainedAgentRequest(ctx, entity.ID)`
- `processor/agentic-loop/settlement_recovery.go:416` — `messages = c.handler.prependIterationContext(ctx, entity.ID, 1, entity.MaxIterations, messages)`
- `processor/agentic-loop/settlement_recovery.go:421` — `request = c.handler.newTaskRequest(entity.ID, task, messages, tools)`
- `processor/agentic-loop/handlers.go:1083` — `RequestID:      h.loopManager.GenerateRequestID(loopID),`
- `processor/agentic-loop/handlers.go:1137` — `Created: true,`
- `processor/agentic-loop/component.go:1410` — `if err := c.publishResults(ctx, result); err != nil {`
- `agentic/state.go:239` — `State:         LoopStateRunning,`
- `agentic/state.go:242` — `Iterations:    0,`
- `processor/agentic-loop/state.go:487` — `entity.Iterations--`
- `processor/agentic-loop/handlers.go:1230` — `if h.loopManager.IsTimedOut(loopID) {`

## Searches

Reused prior inventory and read only the named production paths. Bounded `rg` located `persistHandlerResult`, `restoreLoopFromRequest`, iteration mutation, task/request publication, startup and timeout owners. Structural text fallback followed the previously reported gopls cache-permission failure. A guessed `agentic/loop_entity.go` and `loop_authority.go` path did not exist; file discovery located `agentic/state.go` and `internal/loopbucket/acquire.go`. No absence claim rests on those failed lookups.

Stop for independent comparison; no production correction is authorized by this source analysis alone.

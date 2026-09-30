# Inventory: agentic-loop-commit-disposition-consolidation — the loop terminal fact and its durable homes
base: 3f2d4617c8d2675f6deb1e3aded420c22e959c79

## Claimed gap

- `openspec/changes/agentic-loop-commit-disposition-consolidation/proposal.md:6` — `left one terminal fact with four durable homes`
- `openspec/changes/agentic-loop-commit-disposition-consolidation/proposal.md:11` — `18 hand-ordered publish+KV-write sites (5 distinct orders, 5 gap policies); 15`
- `processor/agentic-loop/terminal_owner.go:166` — `func (c *Component) commitTerminalSteps(ctx context.Context, candidate terminalOutcome, publication HandlerResult) error {`
- `processor/agentic-loop/terminal_owner.go:123` — `//  1. COMPLETE_<loopID> by Create. A refused Create means the loop already has`
- `processor/agentic-loop/terminal_owner.go:131` — `//  4. The loop record, by compare-and-swap Update. The terminal transition`

## Spellings of the fact

### (a) `COMPLETE_<loopID>` marker — bucket `AGENT_LOOPS` (`c.loopsBucket`)

- `processor/agentic-loop/terminal_owner.go:84` — `func terminalMarkerKey(loopID string) string {`
- `processor/agentic-loop/terminal_owner.go:85` — `return "COMPLETE_" + loopID`
- `processor/agentic-loop/config.go:426` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`
- `processor/agentic-loop/terminal_owner.go:24` — `completed *agentic.LoopCompletedEvent`
- `processor/agentic-loop/terminal_owner.go:25` — `failed    *agentic.LoopFailedEvent`
- `processor/agentic-loop/terminal_owner.go:26` — `cancelled *agentic.LoopCancelledEvent`

Writer (create-once):
- `processor/agentic-loop/terminal_owner.go:265` — `func (c *Component) createTerminalMarker(`
- `processor/agentic-loop/terminal_owner.go:276` — `key := terminalMarkerKey(loopID)`
- `processor/agentic-loop/terminal_owner.go:277` — `_, err = c.loopsBucket.Create(ctx, key, data)`

Readers (in `processor/agentic-loop`, the owner's own adoption reads):
- `processor/agentic-loop/terminal_owner.go:285` — `entry, err := c.loopsBucket.Get(ctx, key)`
- `processor/agentic-loop/terminal_owner.go:395` — `entry, err := c.loopsBucket.Get(ctx, terminalMarkerKey(loopID))`

Independent writers of the SAME key convention, a different subsystem (`processor/research-graph-synthesize`, not the terminal owner — a nested loop's `read_loop_result` envelope, not the terminal owner's marker):
- `processor/research-graph-synthesize/adapters.go:162` — `const loopCompletionKeyPrefix = "COMPLETE_"`
- `processor/research-graph-synthesize/adapters.go:169` — `func (s *natsLoopStore) PutLoopCompletion(ctx context.Context, loopID string, envelope []byte) error {`
- `processor/research-graph-synthesize/component.go:512` — `if err := c.loops.PutLoopCompletion(ctx, loopID, envelopeBytes); err != nil {`

In-tree readers outside `processor/agentic-loop`:
- `processor/agentic-tools/loop_result.go:22` — `const completeKeyPrefix = "COMPLETE_"`
- `processor/agentic-tools/loop_result.go:116` — `entry, err := e.kv.Get(ctx, completeKeyPrefix+loopID)`
- `processor/agentic-dispatch/http_activity.go:21` — `// completeKeyPrefix marks terminal AGENT_LOOPS keys (COMPLETE_<loopID>)`
- `processor/agentic-dispatch/http_activity.go:23` — `const completeKeyPrefix = "COMPLETE_"`
- `processor/agentic-dispatch/http.go:1016` — `if strings.HasPrefix(key, completeKeyPrefix) {`
- `processor/agentic-dispatch/http.go:1017` — `return "loop_completed", strings.TrimPrefix(key, completeKeyPrefix)`
- `processor/agentic-dispatch/http_activity.go:193` — `func (c *Component) ensureActivityView(ctx context.Context) (*graphview.View[activityRecord], error) {`
- `pkg/graphview/view.go:43` — `WatchAll(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)`
- `pkg/graphview/view.go:231` — `watcher, err := v.source.WatchAll(ctx)`

Three independent spellings of the same `"COMPLETE_"` literal exist tree-wide (`processor/agentic-tools/loop_result.go:22`, `processor/agentic-dispatch/http_activity.go:23`, `processor/research-graph-synthesize/adapters.go:162`), plus the inline literal in `terminal_owner.go:85` — no shared constant.

### (b) The graph stamp — predicate `agent.loop.outcome`

- `vocabulary/agentic/predicates.go:398` — `LoopOutcome = "agent.loop.outcome"`

Writers (three builders, one per terminal kind, all via `writeBatch` — one `EntityState` mutation per terminal):
- `processor/agentic-loop/graph_writer.go:280` — `func (w *graphWriter) WriteLoopCompletion(ctx context.Context, event *agentic.LoopCompletedEvent, evidenceIncomplete bool) {`
- `processor/agentic-loop/graph_writer.go:296` — `if err := w.writeBatch(ctx, triples); err != nil {`
- `processor/agentic-loop/graph_writer.go:604` — `triple(agvocab.LoopOutcome, event.Outcome),`
- `processor/agentic-loop/graph_writer.go:306` — `func (w *graphWriter) WriteLoopFailure(ctx context.Context, event *agentic.LoopFailedEvent, evidenceIncomplete bool) {`
- `processor/agentic-loop/graph_writer.go:649` — `triple(agvocab.LoopOutcome, event.Outcome),`
- `processor/agentic-loop/graph_writer.go:504` — `func (w *graphWriter) WriteLoopCancellation(ctx context.Context, event *agentic.LoopCancelledEvent, evidenceIncomplete bool) {`
- `processor/agentic-loop/graph_writer.go:682` — `func buildLoopCancellationTriples(loopEntityID string, event *agentic.LoopCancelledEvent, evidenceIncomplete bool) []message.Triple {`
- `processor/agentic-loop/graph_writer.go:696` — `triple(agvocab.LoopOutcome, event.Outcome),`

Callers (the terminal owner's step 2, `stampTerminal`):
- `processor/agentic-loop/terminal_owner.go:344` — `if err := c.stampLoopCompletionWithBudget(ctx, loopID, outcome.completed); err != nil {`
- `processor/agentic-loop/terminal_owner.go:355` — `c.graphWriter.WriteLoopCancellation(ctx, outcome.cancelled, c.trajectoryAuditLoss.observed(loopID))`
- `processor/agentic-loop/component.go:2564` — `func (c *Component) stampLoopCompletionWithBudget(ctx context.Context, loopID string, completion *agentic.LoopCompletedEvent) error {`
- `processor/agentic-loop/component.go:2579` — `c.graphWriter.WriteLoopCompletion(bctx, completion, evidenceIncomplete)`
- `processor/agentic-loop/component.go:2628` — `func (c *Component) stampLoopFailureWithBudget(ctx context.Context, loopID string, failure *agentic.LoopFailedEvent) error {`
- `processor/agentic-loop/component.go:2634` — `c.graphWriter.WriteLoopFailure(bctx, failure, evidenceIncomplete)`

No production Go reader of `agent.loop.outcome` was found in-tree outside `processor/agentic-loop` itself and `vocabulary/agentic/register.go` (registration, not a read) — see Searches. The reader is the rule engine over `$entity.triple.agent.loop.outcome`, configured, not called:
- `configs/rules/deep-research/01-spawn-researcher.json:15` — `"field": "agent.loop.outcome",`

### (c) The terminal event publish

- `processor/agentic-loop/terminal_owner.go:321` — `func (c *Component) terminalPublication(loopID string, outcome terminalOutcome) ([]PublishedMessage, error) {`
- `processor/agentic-loop/terminal_owner.go:322` — `port := "agent.complete"`
- `processor/agentic-loop/terminal_owner.go:324` — `port = "agent.failed"`
- `processor/agentic-loop/terminal_owner.go:331` — `subject, err := component.ResolveSubject(c.config.Ports.Outputs, port, loopID)`
- `processor/agentic-loop/config.go:438` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Description: "Agent task completions (JetStream)",`
- `processor/agentic-loop/config.go:444` — `Name: "agent.failed", Config: component.JetStreamPort{Subjects: []string{"agent.failed.*"}, StreamName: "AGENT"}, Description: "Loop-failed lifecycle events (JetStream)",`
- `processor/agentic-loop/component.go:3722` — `subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.complete", loopID)`

`terminalPublication` (terminal_owner.go:321-336) has no branch for `outcome.cancelled != nil`: `port` stays `"agent.complete"` for a cancelled outcome, matching the literal `handleCancelSignal` uses directly at `component.go:3722`. No `"agent.cancelled"` output port exists (0 hits, see Searches).

### (d) The loop record compare-and-swap write

- `processor/agentic-loop/terminal_owner.go:198` — `if err := c.persistLoopState(ctx, loopID); err != nil {`
- `processor/agentic-loop/component.go:3259` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) error {`
- `processor/agentic-loop/component.go:3260` — `return c.writeLoopRecord(ctx, loopID, true)`
- `processor/agentic-loop/component.go:3270` — `func (c *Component) writeLoopRecord(ctx context.Context, loopID string, terminalWriter bool) error {`
- `processor/agentic-loop/component.go:3323` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)`
- `processor/agentic-loop/terminal_owner.go:189` — `c.handler.loopManager.settleTerminal(loopID, match)`
- `processor/agentic-loop/state.go:2012` — `func (m *LoopManager) settleTerminal(loopID string, adopted *terminalOutcome) {`

Second writer of a terminal loop record (the cancel lane's cold-adoption path, `adoptDurableCancel`'s own CAS — `recordCommittedTerminal`'s doc at `terminal_owner.go:211-220` names exactly these two as the record's only two writers):
- `processor/agentic-loop/terminal_owner.go:452` — `func (c *Component) writeRecordCancelled(`
- `processor/agentic-loop/terminal_owner.go:479` — `if _, err := c.loopsBucket.Update(ctx, loopID, data, record.revision); err != nil {`

### (e) The fifth writer — the carrier's record write (W3 path)

- `processor/agentic-loop/component.go:1598` — `if result.Deferred {`
- `processor/agentic-loop/component.go:1603` — `return c.settleDeferredContinuation(ctx, *task, result)`
- `processor/agentic-loop/component.go:3362` — `func (c *Component) settleDeferredContinuation(ctx context.Context, task agentic.TaskMessage, result HandlerResult) error {`
- `processor/agentic-loop/component.go:3363` — `err := c.persistDeferredContinuationMarker(ctx, result.LoopID, result.deferredPrompt)`
- `processor/agentic-loop/component.go:3391` — `func (c *Component) resumeDeferredContinuation(ctx context.Context, task agentic.TaskMessage, pending HandlerResult) error {`
- `processor/agentic-loop/component.go:3400` — `return c.settleDeferredContinuation(ctx, task, pending)`
- `processor/agentic-loop/component.go:3442` — `func (c *Component) persistDeferredContinuationMarker(ctx context.Context, loopID, prompt string) error {`
- `processor/agentic-loop/component.go:3492` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)`

This path writes the loop record only (no publish, no marker, no graph stamp) — the deferred turn's marker and text, under the same `c.loopsBucket.Update` CAS bucket as (d), racing the terminal owner's own write of the same record (the W3 window named in the proposal).

### Hand-ordered publish + KV-write sites (`processor/agentic-loop`, non-test, at this base)

The census (#1146 issuecomment-5854803094 § 4) counted 18 sites, 5 orders, 5 gap policies at `078782b1`. Predecessors #1399 (PR #1402, `73ea4f26`, the W2 fix) and #1400 (PR #1403, `3dc4ccbe`, the deferred-write fix) landed since and touch two of these sites (rows 1 and 15 below). The sites below are the ones this pass pinned and read in full; the count is not re-asserted as 18 — see Searches for what was not individually re-read.

| # | Site (pin) | Order as spelled | Disposition on failure, per step |
|---|---|---|---|
| 1 | `processor/agentic-loop/terminal_owner.go:166` `commitTerminalSteps` | marker Create → graph stamp → publish event → record CAS | Fatal / Fatal / Fatal / (`ErrKVRevisionMismatch` → returned as-is else Fatal) |
| 2 | `processor/agentic-loop/terminal_owner.go:265` `createTerminalMarker` (step 1 body) | Create → on conflict Get → decode → same-loop adopt / other-loop refuse | raw error (Fatal at caller) / raw error / raw error / adopt (no error) or refusal `fmt.Errorf` (Fatal at caller) |
| 3 | `processor/agentic-loop/terminal_owner.go:391` `adoptDurableCancel` | Get marker → decode → graph stamp → publish event → record CAS (`writeRecordCancelled`) | NotFound/Deleted → no-op, no error; other Get error → Transient; decode error → Fatal; wrong loop → Fatal; stamp → Fatal; publish → Fatal; CAS conflict → Transient; CAS other → Fatal |
| 4 | `processor/agentic-loop/component.go:2241` `handleLoopFailure` | TransitionLoop (mem) → UpdateCompletion (mem) → BuildFailureMessages → recordTerminalObservation (ObjectStore) → `commitTerminal` (site 1, detached ctx) | TransitionLoop failure → raw error, nothing written; buildErr alone with commit landed → plain wrapped error; commit CAS mismatch → returned as-is; other commit error → Fatal |
| 5 | `processor/agentic-loop/component.go:3690` `handleCancelSignal` | CancelLoop (mem) → recordTerminalObservation (ObjectStore) → marshal event → resolve `"agent.complete"` subject → `commitTerminal` (site 1) | CancelLoop error → `settleUncancellableLoop` (not read here); marshal error → release + Fatal; resolve-subject error → release + Fatal; commit CAS mismatch → returned as-is; other commit error → Fatal |
| 6 | `processor/agentic-loop/component.go:2452` `persistHandlerResult` terminal branch | delegates to `commitTerminal` (site 1) | any error → returned as-is to this call's own caller |
| 7 | `processor/agentic-loop/component.go:2468` `persistHandlerResult` gated branch | `stampPublishedRequest` → `writeLoopRecord` (CAS, `terminalWriter=false`) → `publishResults` | stamp failure → Fatal; `errTerminalOwnedElsewhere` → `settleTerminalGuard` (drop); CAS mismatch → returned as-is; other write error → Fatal; publish failure → Fatal |
| 8 | `processor/agentic-loop/component.go:2512` `publishThenPersistResultState` (L4a — model-response/tool-result/approval lanes, non-gated) | `publishResults` → `stampPublishedRequest` → `writeLoopRecord` (CAS) | publish failure → Fatal; stamp `ErrLoopNotFound` → `settleTerminalGuard`; stamp other → Fatal; write `errTerminalOwnedElsewhere` → `settleTerminalGuard`; CAS mismatch → returned as-is; other write error → Fatal |
| 9 | `processor/agentic-loop/approval_sweeper.go:128` (timeout auto-reject's own terminal failure) | calls `persistHandlerResult` (site 6) | any error → logged `Warn`, loop continues to next candidate (best-effort, not retried) |
| 10 | `processor/agentic-loop/approval_sweeper.go:163` (timeout auto-reject's ordinary result) | calls `persistHandlerResult` (site 7 or 8) | any error → logged `Warn`, continue (best-effort, "not retried (OQ-B): a timer has no delivery to classify") |
| 11 | `processor/agentic-loop/approval_response_handler.go:232` (approval answer settled failed) | calls `persistHandlerResult` (site 6) | nil → `DeliveryDecisionAck`; `!errs.IsFatal` → `DeliveryDecisionRetry`; `errs.IsFatal` → `DeliveryDecisionQuarantine` |
| 12 | `processor/agentic-loop/approval_response_handler.go:283` (ordinary approval result) | calls `persistHandlerResult` (site 7 or 8) | nil → `DeliveryDecisionAck`; `!errs.IsFatal` → `DeliveryDecisionRetry`; `errs.IsFatal` → `DeliveryDecisionQuarantine` |
| 13 | `processor/agentic-loop/terminal_owner.go:452` `writeRecordCancelled` (second writer of a terminal record, home (d)) | read record → mutate fields → CAS Update | stale presence → no-op; unknown presence → Transient; CAS conflict → Transient; other → Fatal |
| 14 | `processor/agentic-loop/component.go:3442` `persistDeferredContinuationMarker` (home (e), record-only, no publish) | observe revision → `readLoopRecord` → mutate 3 fields → CAS Update | no observed revision → Fatal; stale/gone record → no-op, logged; CAS `nats.ErrMaxPayload` → drop prompt from memory, plain error; CAS conflict → Transient; other → plain error |
| 15 | `processor/agentic-loop/component.go:3362` `settleDeferredContinuation` (call-site wrapper around 14; #1400/#1403 changed this disposition) | calls site 14 | nil → Ack; CAS mismatch → redeliver via released loop; `nats.ErrMaxPayload` → Ack (deterministic, not retried); any other failure → remembered, delivery Retried, resumed via `resumeDeferredContinuation` |

## Adjacent claims

- #1146 — the parent issue; Q4/Q7 rulings (issuecomment-5854830449, issuecomment-5856142786) authorize this change
- #1362, #1377, #1399, #1400 — the prior designs and rulings `commitTerminal`'s doc comment cites by number
- `docs/proposals/gh865-866-terminal-event-design.md:268` — `Canonical loop terminal graph facts:`
- `docs/proposals/gh865-866-terminal-event-inventory.md:252` — `Canonical loop terminal graph facts:`
- `docs/adr/028-orchestration-architecture.md:61` — `Bulky content lives in durable stores: `COMPLETE_{loopID}` in AGENT_LOOPS`
- `docs/concepts/03-streams-vs-kv-watches.md:121` — `KV watches have no processing acknowledgment or redelivery. A derived owner must`
- `natsclient/delivery_settlement.go:17` — `type DeliveryDecision uint8`
- `openspec/specs/agentic-loop/spec.md:573` — `stamped on the same terminal graph write that carries`
- semmachina `internal/resume/pending.go:88` — `# Why AGENT_LOOPS cannot answer this` — a sister's own recorded reason AGENT_LOOPS state cannot serve as a liveness/authority signal, relevant to any design that widens the marker's role

## Consumers

In-tree, already pinned above under Spellings of the fact per home: (a) `processor/agentic-tools/loop_result.go:116`, `processor/agentic-dispatch/http.go:1015-1016` via `pkg/graphview/view.go:231`; (b) rule engine only, no Go reader; (c) subject `agent.complete.*`/`agent.failed.*` consumed by JetStream subscribers outside this package (not traced further, out of the stated surface); (d)/(e) `readLoopRecord` (`processor/agentic-loop/loop_evidence.go:291`) is the terminal owner's and cancel lane's own re-read, not a foreign consumer.

`settleTerminalGuard` (`processor/agentic-loop/terminal_owner.go:505`) — the typed guard the terminal-owned-elsewhere / drop path shares — has 8 non-test callers:
- `processor/agentic-loop/approval_response_handler.go:278` — `if err := c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped); err != nil {`
- `processor/agentic-loop/component.go:2083` — `return c.settleTerminalGuard(ctx, result, func() {`
- `processor/agentic-loop/component.go:2382` — `return c.settleTerminalGuard(ctx, result, nil)`
- `processor/agentic-loop/component.go:2424` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2476` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2536` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2546` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2788` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`

### Sister repositories (read-only; `origin/main` fetched this pass)

| Sister | origin/main sha | file:line | line text | home read |
|---|---|---|---|---|
| semspec | `9dd6a2d4b178ffb1c6898d0e7cb2174c45e30c90` | `cmd/semspec/watch_live.go:260` | `if strings.HasPrefix(e.Key, "COMPLETE_") {` | (a) marker key prefix, own scan |
| semspec | `9dd6a2d4b178ffb1c6898d0e7cb2174c45e30c90` | `cmd/semspec/watch_live.go:261` | `completed[strings.TrimPrefix(e.Key, "COMPLETE_")] = struct{}{}` | (a) marker key prefix, own scan |
| semspec | `9dd6a2d4b178ffb1c6898d0e7cb2174c45e30c90` | `pkg/health/orchestrate.go:19` | `const completeLoopMarkerPrefix = "COMPLETE_"` | (a) a fourth independent spelling of the same literal, a different package |
| semteams | `ce22c961d30014c463a09f8f8a2a90044ee1a1cf` | `cmd/semteams/chainpause/pauser.go:65` | `func (p *Pauser) HandleFailed(ctx context.Context, ev *agentic.LoopFailedEvent) (PauseResult, error) {` | (c) terminal event payload type, consumed off the wire |
| semteams | `ce22c961d30014c463a09f8f8a2a90044ee1a1cf` | `cmd/semteams/portresolver/portresolver.go:60` | `// agent.failed and agent.complete with their canonical subjects).` | (c) subject names, independently resolved |
| semsage | `4d28b4dc1210f47da84a3031125167d164de9290` | `processor/ui-api/component.go:217` | `Description: "AGENT_LOOPS KV bucket — read for loop state and SSE activity",` | (a)/(d) whole bucket, own KV read port |
| semsage | `4d28b4dc1210f47da84a3031125167d164de9290` | `tools/spawn/executor.go:283` | `var event agentic.LoopCompletedEvent` | (c) terminal event payload type, decoded off `agent.complete.<loopID>` |
| semsage | `4d28b4dc1210f47da84a3031125167d164de9290` | `tools/spawn/executor.go:301` | `var event agentic.LoopFailedEvent` | (c) terminal event payload type, decoded off `agent.failed.<loopID>` |
| semmachina | `d6b08326116193100372a332d24b27584a6ec274` | `internal/stage/loopfailure.go:501` | `func (w *LoopFailureWatcher) decode(data []byte) (*agentic.LoopFailedEvent, error) {` | (c) terminal event payload type, consumed off the wire |
| semmachina | `d6b08326116193100372a332d24b27584a6ec274` | `internal/resume/pending.go:88` | `# Why AGENT_LOOPS cannot answer this` | (a)/(d) — a recorded reason AGENT_LOOPS is NOT read as a liveness signal |
| semsource | `34bda6406fb06fd723a040988647b204204a1583` | — | — | none found (zero-hit search, see below) |
| semconnect | `d0d06e00bf05a545f30ceea798db1c2b1ee47d4f` | — | — | none found (zero-hit search, see below) |

## Problem shape

- `natsclient/delivery_settlement.go:17` — `type DeliveryDecision uint8`
- `processor/agentic-loop/terminal_owner.go:505` — `func (c *Component) settleTerminalGuard(ctx context.Context, result HandlerResult, recordDrop func()) error {`

The typed disposition (Ack/Retry/Terminate/Quarantine) already exists, and `settleTerminalGuard`'s "read the record, not memory" primitive already has 8 non-test call sites (Consumers, above) — the shape sites 4, 6-8, 11-12 above hand-roll locally instead of routing through a shared "commit the terminal" or "commit the record" function.

## Searches

- `git rev-parse HEAD` → `3f2d4617c8d2675f6deb1e3aded420c22e959c79`
- `gopls references processor/agentic-loop/terminal_owner.go:84:6` (`terminalMarkerKey`) → 9 (2 non-test production: terminal_owner.go:276, :395)
- `git grep -n "COMPLETE_" -- .` (tracked, all files) → 66
- `git grep -n "COMPLETE_" -- '*.go' | grep -v _test.go` → 41
- `git grep -n "AGENT_LOOPS" -- '*.go' | grep -v _test.go` → 50+ (truncated at head -50)
- `git grep -n "loopsBucket\." -- '*.go' | grep -v _test.go` → 9
- `git grep -n "natsLoopStore{"` / `"type natsLoopStore"` / `"PutLoopCompletion\|PutSearchResult("` -- '*.go' → 5 / 5 / 5
- `git grep -n "func buildLoopCompletionTriples\|func buildLoopFailureTriples\|PredicateLoopOutcome\|agent.loop.outcome" -- '*.go' | grep -v _test.go` → 4
- `git grep -n "func buildLoopCancellationTriples" -- '*.go'` → 1
- `gopls references vocabulary/agentic/predicates.go:398:2` (`LoopOutcome`) → 18 total, 5 non-test in-tree (graph_writer.go x3, register.go x1, plus one more counted by gopls); 0 outside `processor/agentic-loop`/`vocabulary/agentic`
- `git grep -n '"agent.complete"\|"agent.failed"\|"agent.cancelled"' -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 6, all `agent.complete`/`agent.failed`, 0 `agent.cancelled`
- `git grep -c '"agent.cancelled"' -- 'processor/agentic-loop/*.go'` → 0
- `git grep -n "^const completeKeyPrefix" -- '*.go'` → 2 (`processor/agentic-dispatch/http_activity.go:23`, `processor/agentic-tools/loop_result.go:22`)
- `git grep -n "^const loopCompletionKeyPrefix" -- '*.go'` → 1 (`processor/research-graph-synthesize/adapters.go:162`)
- `git grep -n "func.*handleActivityStream\|WatchAll(\|func ensureActivityView\|func (c \*Component) ensureActivityView" -- 'processor/agentic-dispatch/*.go' | grep -v _test.go` → 2
- `git grep -n "WatchAll(" -- 'pkg/graphview/*.go' | grep -v _test.go` → 2
- `git grep -n "completeKeyPrefix" -- 'processor/agentic-tools/*.go' | grep -v _test.go` → 4
- `git grep -n "func.*settleTerminal(" -- '*.go' | grep -v _test.go` → 1
- `gopls references processor/agentic-loop/terminal_owner.go:505:26` (`settleTerminalGuard`) → 8, all non-test
- `gopls references natsclient/delivery_settlement.go:17:6` (`DeliveryDecision`) → 101 (not individually walked — see NOT RUN)
- `gh api repos/C360Studio/semstreams/issues/comments/5854803094 --jq .body` → 1 (the Fable judgment comment; read as the hypothesis to re-derive, per the brief, not evidence)
- `git -C /Users/coby/Code/c360/<sister> fetch -q origin` → clean for semspec, semteams, semsage, semsource, semmachina, semconnect (6/6)
- `git -C <sister> grep -n "COMPLETE_" origin/main -- '*.go'` → semspec 16 (2 production files + tests), semteams 0, semsage 0, semsource 0, semmachina 0, semconnect 0
- `git -C <sister> grep -n "AGENT_LOOPS" origin/main -- '*.go'` → semteams 1 (comment only), semsage 8, semsource 0, semmachina 2, semconnect 0
- `git -C <sister> grep -n "agent\.complete\b" origin/main -- '*.go'` → semteams 8+ (portresolver + tests), semsage 8+ (spawn executor + tests), semsource 0, semmachina 1 (test), semconnect 0
- `git -C <sister> grep -n "agent\.loop\.outcome" origin/main -- '*'` → semteams 8+ (rule configs), semsage 0, semsource 0, semmachina 0, semconnect 0
- `git -C <sister> grep -n "LoopCompletedEvent\|LoopFailedEvent\|LoopCancelledEvent" origin/main -- '*.go'` → semteams 8+, semsage 6+, semsource 0, semmachina 8+, semconnect 0
- `task inventory:verify -- openspec/changes/agentic-loop-commit-disposition-consolidation/inventory-terminal-homes.md` → run below

NOT RUN (surface larger than the 40-call bound; honest partial):
- `component.go:2795`, `component.go:2848` (two more `persistHandlerResult` call sites, likely tool-result/model-response lanes) — not individually read for their own pre-steps or disposition wording
- `component.go:3000` (a `publishResults` call for an "echo") — not read; unclear whether it pairs with a KV write at the same site
- `component.go:1875`, `:1889` (`recordTerminalObservation` / `WriteLineageTriples` near spawn) — spawn-time graph writes, adjacent to but not part of the terminal fact; not traced
- the remaining ~3 sites the census's "18" implies beyond the 15 pinned here were not individually located — no search for them came back empty; they were simply not run
- `DeliveryDecision`'s 101 references were not walked individually beyond the 8 `settleTerminalGuard` and 4 `approval_response_handler.go` sites already pinned
- rule-engine subscribers of `agent.complete.*`/`agent.failed.*` JetStream subjects beyond the sister repos already searched — not enumerated in-tree (execution-manager, generic rule watcher)

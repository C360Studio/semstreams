# Inventory: agentic-loop-commit-disposition-consolidation — the loop terminal fact, its durable homes, and the delivery dispositions around it (#1405)

base: 913b74f07b78340a4fbff3eff56eb9ab6355050a

Inventory only (architect contract § The surface inventory, § The adopter seam inventory; handoff shape 1). No target
state, options, recommendation or artifact delta appear here. Every `path:line` pin was generated with `sed -n "${n}p"`
at this base; counts name the tool that produced them (`gopls references` for structural questions, `git grep -n` for
string literals). The code under `processor/`, `natsclient/`, `internal/`, `pkg/`, `agentic/`, `vocabulary/`,
`configs/`, `docs/` and `openspec/specs/` is byte-identical between this base and the two explorer files' base
`3f2d4617` (`git diff --stat 3f2d4617..913b74f0 -- <those dirs>` → empty), so the explorer pins were re-measured, not
re-located.

## Problem statement

One fact — "loop L has ended, in kind K, with payload P" — is written to four durable homes by one owner in a fixed
order with no transaction (`processor/agentic-loop/terminal_owner.go:166-208`): the `COMPLETE_<loopID>` key by
create-once, the `agent.loop.outcome` graph stamp through the mutation port, the terminal event on `agent.complete.*`
or `agent.failed.*`, and the `AGENT_LOOPS/<loopID>` record by compare-and-swap. The accepted windows W1, W2 and W3
(#1146 issuecomment-5854802835, 15 of 67 rows) are gaps between two of those writes. Around the owner, every input
lane separately answers "do I hold this loop, or do I read its record" and separately spells "this delivery is
inapplicable", and each spelling picks its own disposition. This inventory enumerates every home of the fact with its
readers and writers (in this tree and in the sisters at their `origin/main`, read-only), every site that hand-orders
a publish against a KV write with the gap policy it chose, every cold arm, every `ErrLoopNotFound` decision, every
adopt function, every inapplicable-delivery token, every drop-metric family, every `WrapFatal` site feeding the lane
latch, and the test hooks — as the BEFORE column of the owner's acceptance test. It also re-measures each figure the
#1146 census asserted at `078782b1` and records which figure is right and why. Two loop kinds write the record at the bare key and only one ever writes it
terminal; the graph stamp is the one home whose failure the owner does not see.

## Method and starting point

Two `semstreams-explorer` files were the starting point (owner ruling A, 2026-08-30, #1180):
`inventory-terminal-homes.md` (84 pins) and `inventory-dispositions.md` (191 pins). What this pass did with them:
every count was re-run with the tool named; the explorer's zero-hit searches were spot-checked (§ Searches); the
following were ADDED because the explorer missed them — the step-0 record writer `adoptNewerRetainedRequest`'s own
compare-and-swap (`loop_evidence.go:528`), the birth `Create` (`component.go:3151`), the terminal path that bypasses
the owner by ruling (`terminateOversizedBirth`, `component.go:1853`), five bare reason literals (`superseded_request`,
`older_request`, `already_applied`, `terminal_unproven`, `approval_inapplicable`), the sentinel
`errTerminalOwnedElsewhere`, the enum values `requestOrderApplied` and `taskApplied`, the governance family that
carries the verdict drop reasons, the in-tree event consumers (`agentic/agentrun`, `output/otel`,
`internal/agentterminal`, `processor/agentic-dispatch`), the second dispatch record reader
(`terminal_settlement.go`, `loop_admission.go`), and four sister repositories the explorer's table omitted
(semdragon, semdev — readers; semops, semembed — zero hits). STRUCK: the explorer's `WrapFatal` per-file summary
(21/13/12) — its pins sum to 19/14/13, which is the measured split; and its "8 sites check `errs.IsFatal`" — six
lines call `errs.IsFatal(` (the other listed lines are the Quarantine returns those six guard).

Revision 2, after the independent inventory review (PR #1407 issuecomment-5858959863: 2 BLOCKING, 7 MEDIUM; every
finding was re-measured here and none is contested). ADDED: `frameworkcapabilities/graphresearch` as the second
production writer of `AGENT_LOOPS/<loopID>` (`register_tool.go:92`) and the second provisioner of the bucket (`:49`,
`:72`); the research-pipeline loop as a second loop kind whose record is created `executing` and never written terminal
(§ Home (g)); the three terminal stamp builders' Warn-and-continue on a mutation failure or a missing platform identity
(row 1's S cell, gap policy 5, Seams 3 and 4, a new Measurements row); four more rule readers of `agent.loop.outcome`;
the e2e writers of the marker and of the stamp; the e2e wait on the record's cancelled state; semspec
`req_watcher.go:207` and `pkg/health/capture.go:75`; semstreams-ui; the zero-hit searches for semboids, semlink,
semmem, seminstruct and semdocs. CORRECTED: the `COMPLETE_` line count (57, not 41); the `errs.IsFatal(` list
(`component.go:1335` in, `governance_dispatcher.go:637` relabelled a Quarantine return); the test-hook search (the `-E`
form returns 0, the `-P` form returns the 4). STRUCK: "a rule never sees an event without its stamp"; "the agentic tier
waits on the event, never on the record". Three adopter-seam Q4 answers were restated as knowledge gaps only. Sister
rows remain measurements — writers, readers, sha, pinned semstreams version — and the semspec questions are recorded as
migration inputs (owner ruling #1405 issuecomment-5858896669: sisters get migration notes only; no design inference).

## Claimed gap (category 1)

The change claims (a) one fact has four durable homes, (b) the disposition and authority decisions around it are
spelled many times, and (c) `DeliveryDecision` and `settleTerminalGuard` already exist and are used inconsistently.
All three are measured below; none is asserted from the briefing. The owner's own doc comment already states (a):

- `openspec/changes/agentic-loop-commit-disposition-consolidation/proposal.md:6` — `left one terminal fact with four durable homes`
- `processor/agentic-loop/terminal_owner.go:118` — `// commitTerminal is the one owner of a loop's terminal (#1362, design § 5.7,`
- `processor/agentic-loop/terminal_owner.go:123` — `//  1. COMPLETE_<loopID> by Create. A refused Create means the loop already has`
- `processor/agentic-loop/terminal_owner.go:131` — `//  4. The loop record, by compare-and-swap Update. The terminal transition`
- `processor/agentic-loop/terminal_owner.go:166` — `func (c *Component) commitTerminalSteps(ctx context.Context, candidate terminalOutcome, publication HandlerResult) error {`
- `processor/agentic-loop/terminal_owner.go:211` — `// recordCommittedTerminal counts a terminal loop record this process just`
- `processor/agentic-loop/terminal_owner.go:214` — `// has exactly two writers — commitTerminal, and adoptDurableCancel's`
- `natsclient/delivery_settlement.go:17` — `type DeliveryDecision uint8`
- `processor/agentic-loop/terminal_owner.go:505` — `func (c *Component) settleTerminalGuard(ctx context.Context, result HandlerResult, recordDrop func()) error {`

## Spellings of the fact (category 2): every durable home, its writers and its readers

### Home (a) — the `COMPLETE_<loopID>` marker in bucket `AGENT_LOOPS`

Key convention (four independent spellings of the literal in this tree, no shared constant):

- `processor/agentic-loop/terminal_owner.go:84` — `func terminalMarkerKey(loopID string) string {`
- `processor/agentic-loop/terminal_owner.go:85` — `return "COMPLETE_" + loopID`
- `processor/agentic-tools/loop_result.go:22` — `const completeKeyPrefix = "COMPLETE_"`
- `processor/agentic-dispatch/http_activity.go:23` — `const completeKeyPrefix = "COMPLETE_"`
- `processor/research-graph-synthesize/adapters.go:162` — `const loopCompletionKeyPrefix = "COMPLETE_"`
- `processor/agentic-loop/config.go:426` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`

Writers:

- `processor/agentic-loop/terminal_owner.go:277` — `_, err = c.loopsBucket.Create(ctx, key, data)`
- `processor/research-graph-synthesize/adapters.go:170` — `_, err := s.kv.Put(ctx, loopCompletionKeyPrefix+loopID, envelope)`
- `processor/research-graph-synthesize/component.go:512` — `if err := c.loops.PutLoopCompletion(ctx, loopID, envelopeBytes); err != nil {`

The second writer is a different subsystem writing a different payload (a research `SearchResult` envelope, by `Put`,
not create-once) under the same key convention — the reader `read_loop_result` decodes whatever it finds as a
`LoopCompletedEvent` (`loop_result.go:132`). The payload is `Created` by `graph-ingest` for the graph stamp only;
the marker itself has exactly these two writers in this tree (`git grep -n "COMPLETE_" -- '*.go' | grep -v _test.go`
→ 57 lines, 44 outside `test/`, of which 3 are production write sites: the two above plus the `Put` behind
`PutLoopCompletion`; the e2e ops scenario seeds the key directly by `PutKV`, a fourth, test-only writer).

Readers in `processor/agentic-loop` (the owner's own adoption reads):

- `processor/agentic-loop/terminal_owner.go:285` — `entry, err := c.loopsBucket.Get(ctx, key)`
- `processor/agentic-loop/terminal_owner.go:395` — `entry, err := c.loopsBucket.Get(ctx, terminalMarkerKey(loopID))`

Readers outside `processor/agentic-loop`, production:

- `processor/agentic-tools/loop_result.go:116` — `entry, err := e.kv.Get(ctx, completeKeyPrefix+loopID)`
- `processor/agentic-tools/loop_result.go:132` — `var event agentic.LoopCompletedEvent`
- `processor/agentic-dispatch/http.go:1016` — `if strings.HasPrefix(key, completeKeyPrefix) {`
- `processor/agentic-dispatch/http.go:1017` — `return "loop_completed", strings.TrimPrefix(key, completeKeyPrefix)`
- `processor/agentic-dispatch/loop_wire.go:129` — `// Returns ok=false unless the bytes validate as an ordinary terminal payload.`
- `processor/agentic-dispatch/component.go:126` — `// Shared AGENT_LOOPS read view (ADR-081): ONE graphview.View serves every`
- `pkg/graphview/view.go:231` — `watcher, err := v.source.WatchAll(ctx)`
- `processor/agentic-dispatch/terminal_settlement.go:208` — `// OTHER writer of this bucket is prefixed — COMPLETE_<id>,`
- `processor/rule/entity_substitution.go:5` — `// `read_loop_result` tool: AGENT_LOOPS keys are `COMPLETE_<bare-uuid>`,`

Tests and e2e (a reader at `:559`; a writer at `:439`, which seeds `COMPLETE_` keys built at `:419`):

- `test/e2e/scenarios/ops/scenario.go:419` — `kvKey = "COMPLETE_" + seed.loopID`
- `test/e2e/scenarios/ops/scenario.go:439` — `if err := s.nats.PutKV(ctx, loopsBucket, kvKey, loopData); err != nil {`
- `test/e2e/scenarios/ops/scenario.go:559` — `if strings.HasPrefix(key, "COMPLETE_") && !seededKeys[key] {`
- `test/e2e/scenarios/research-graph/scenario.go:840` — `envelope, err := s.nats.GetKV(ctx, "AGENT_LOOPS", "COMPLETE_"+loopID)`

Documentation claims about who reads the marker (no rule config in `configs/rules/` declares a KV watch on
`AGENT_LOOPS`; `git grep -n '"bucket"' -- configs/rules/` → `RESEARCH_EVIDENCE`, `ENTITY_STATES` only — the "rules
engine watches COMPLETE_*" sentence names a reader that does not exist in the shipped configs):

- `processor/agentic-loop/doc.go:393` — `// **COMPLETE_{loopID}**: The loop's durable terminal — completed, failed or`
- `processor/agentic-loop/doc.go:441` — `//  2. Rules engine watches COMPLETE_* keys`
- `processor/agentic-loop/README.md:158` — `| loops | AGENT_LOOPS | `COMPLETE_{loop_id}` | Completion state for rules engine |`
- `docs/concepts/13-agentic-systems.md:302` — `The handoff is managed via the `COMPLETE_{loopID}` KV key pattern, which the rules engine can watch to`
- `docs/adr/028-orchestration-architecture.md:61` — `- Bulky content lives in durable stores: `COMPLETE_{loopID}` in AGENT_LOOPS`

### Home (b) — the graph stamp, predicate `agent.loop.outcome` on the loop-execution entity

Semantic writer is `agentic-loop`'s `graphWriter`; the physical writer of `ENTITY_STATES` is `graph-ingest` through
the mutation port (`writeBatch` → `graphmutation.NewClient`). Three builders, one per kind, plus the synthetic-decide
stamp, all behind `stampTerminal`:

- `vocabulary/agentic/predicates.go:398` — `LoopOutcome = "agent.loop.outcome"`
- `vocabulary/agentic/register.go:435` — `vocabulary.Register(LoopOutcome,`
- `processor/agentic-loop/graph_writer.go:104` — `func (w *graphWriter) writeBatch(ctx context.Context, triples []message.Triple) error {`
- `processor/agentic-loop/graph_writer.go:280` — `func (w *graphWriter) WriteLoopCompletion(ctx context.Context, event *agentic.LoopCompletedEvent, evidenceIncomplete bool) {`
- `processor/agentic-loop/graph_writer.go:306` — `func (w *graphWriter) WriteLoopFailure(ctx context.Context, event *agentic.LoopFailedEvent, evidenceIncomplete bool) {`
- `processor/agentic-loop/graph_writer.go:504` — `func (w *graphWriter) WriteLoopCancellation(ctx context.Context, event *agentic.LoopCancelledEvent, evidenceIncomplete bool) {`
- `processor/agentic-loop/graph_writer.go:604` — `triple(agvocab.LoopOutcome, event.Outcome),`
- `processor/agentic-loop/graph_writer.go:649` — `triple(agvocab.LoopOutcome, event.Outcome),`
- `processor/agentic-loop/graph_writer.go:696` — `triple(agvocab.LoopOutcome, event.Outcome),`
- `processor/agentic-loop/terminal_owner.go:341` — `func (c *Component) stampTerminal(ctx context.Context, loopID string, outcome terminalOutcome) error {`
- `processor/agentic-loop/terminal_owner.go:355` — `c.graphWriter.WriteLoopCancellation(ctx, outcome.cancelled, c.trajectoryAuditLoss.observed(loopID))`
- `processor/agentic-loop/component.go:2564` — `func (c *Component) stampLoopCompletionWithBudget(ctx context.Context, loopID string, completion *agentic.LoopCompletedEvent) error {`
- `processor/agentic-loop/component.go:2628` — `func (c *Component) stampLoopFailureWithBudget(ctx context.Context, loopID string, failure *agentic.LoopFailedEvent) error {`

A second stamp writer outside the owner (best-effort, no marker, no event, no record — see § Home (f)):

- `processor/agentic-loop/component.go:1853` — `func (c *Component) terminateOversizedBirth(ctx context.Context, loopID, taskID string, err error) error {`

Readers: `gopls references vocabulary/agentic/predicates.go:398:2` → 19 total, 4 non-test (3 builders + the
registration); 0 Go readers outside `processor/agentic-loop`. The readers are six rule configurations
(`git grep -n 'agent\.loop\.outcome' -- configs/` → 6):

- `configs/rules/deep-research/01-spawn-researcher.json:15` — `"field": "agent.loop.outcome",`
- `configs/rules/deep-research/02-collect-evidence.json:15` — `"field": "agent.loop.outcome",`
- `configs/rules/deep-research/05-retry-insufficient.json:15` — `"field": "agent.loop.outcome",`
- `configs/rules/deep-research/06-timeout-partial.json:15` — `"field": "agent.loop.outcome",`
- `configs/rules/deep-research/07-spawn-coordinator.json:15` — `"field": "agent.loop.outcome",`
- `configs/rules/example-fan-out/02-stamp-completion-on-parent.json:15` — `"field": "agent.loop.outcome",`

A test WRITER of home (b): the e2e ops scenario seeds the triple straight into `ENTITY_STATES` (`Source:
"e2e-ops-seed"`), bypassing the owner and the `graph-ingest` stamp path:

- `test/e2e/scenarios/ops/scenario.go:456` — `Predicate: "agent.loop.outcome",`

### Home (c) — the terminal event on `agent.complete.<loopID>` / `agent.failed.<loopID>` (stream `AGENT`)

Four independent spellings of the port names inside the owner package, and no `agent.cancelled` port: a
cancellation rides `agent.complete` (`git grep -c '"agent.cancelled"' -- 'processor/agentic-loop/*.go'` → 0).

- `processor/agentic-loop/config.go:438` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Description: "Agent task completions (JetStream)",`
- `processor/agentic-loop/config.go:444` — `Name: "agent.failed", Config: component.JetStreamPort{Subjects: []string{"agent.failed.*"}, StreamName: "AGENT"}, Description: "Loop-failed lifecycle events (JetStream)",`
- `processor/agentic-loop/terminal_owner.go:321` — `func (c *Component) terminalPublication(loopID string, outcome terminalOutcome) ([]PublishedMessage, error) {`
- `processor/agentic-loop/terminal_owner.go:322` — `port := "agent.complete"`
- `processor/agentic-loop/terminal_owner.go:324` — `port = "agent.failed"`
- `processor/agentic-loop/handlers.go:2637` — `completionSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.complete", loopID)`
- `processor/agentic-loop/handlers.go:3472` — `failureSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.failed", loopID)`
- `processor/agentic-loop/component.go:3722` — `subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.complete", loopID)`
- `agentic/agentrun/agentrun.go:483` — `// Cancellation rides agent.complete (not agent.cancelled), so callers MUST`

Publisher (one function, six call sites; `gopls references processor/agentic-loop/component.go:3013:21` → 8, 6
non-test):

- `processor/agentic-loop/component.go:3013` — `func (c *Component) publishResults(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:1773` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:2493` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:2513` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:3000` — `if err := c.publishResults(ctx, HandlerResult{LoopID: loopID, PublishedMessages: []PublishedMessage{*echo}}); err != nil {`
- `processor/agentic-loop/terminal_owner.go:182` — `if err := c.publishResults(ctx, publication); err != nil {`
- `processor/agentic-loop/terminal_owner.go:424` — `if err := c.publishResults(ctx, HandlerResult{LoopID: loopID, PublishedMessages: messages}); err != nil {`

Payload types (the wire contract sisters decode):

- `agentic/events.go:60` — `type LoopCompletedEvent struct {`
- `agentic/events.go:136` — `type LoopFailedEvent struct {`
- `agentic/events.go:203` — `type LoopCancelledEvent struct {`

Readers in this tree, production (the explorer's "not traced further" gap, closed):

- `agentic/agentrun/agentrun.go:519` — `// MilestoneSubscriber decodes terminal loop events from NATS (agent.complete.*`
- `agentic/agentrun/agentrun.go:977` — `FilterSubject: "agent.complete.*",`
- `agentic/agentrun/agentrun.go:1040` — `FilterSubject: "agent.failed.*",`
- `service/milestone_service.go:28` — `// JetStream consumers (agent.complete.* / agent.failed.*) under the`
- `internal/boot/run.go:295` — `// ServiceManager's ordered shutdown. Subscribes to agent.complete.* /`
- `processor/agentic-dispatch/config.go:122` — `Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Required: true,`
- `output/otel/span_collector.go:215` — `strings.HasPrefix(subject, "agent.complete.") || strings.HasPrefix(subject, "agent.failed.") {`
- `internal/agentterminal/terminal.go:1` — `// Package agentterminal is the repository-internal interpretation boundary for`
- `internal/agentterminal/terminal.go:125` — `case *agentic.LoopCompletedEvent:`

Readers, e2e (the agentic tier waits on the event at these two sites; it also waits on the record's terminal state —
`git grep -n 'awaitLoopState(' -- test/e2e` → 3 calls, one on `LoopStateCancelled` — pinned under home (d)):

- `test/e2e/scenarios/agentic/approval_signal.go:922` — `stored, getErr := stream.GetLastMsgForSubject(ctx, "agent.complete."+loopID)`
- `test/e2e/scenarios/agentic/stage_a_process_replacement.go:1141` — `terminal, err := agentStream.GetLastMsgForSubject(ctx, "agent.complete."+task.LoopID)`

### Home (d) — the loop record `AGENT_LOOPS/<loopID>`, terminal state by compare-and-swap

Terminal-record writers (two, as the owner's doc states):

- `processor/agentic-loop/terminal_owner.go:189` — `c.handler.loopManager.settleTerminal(loopID, match)`
- `processor/agentic-loop/state.go:2012` — `func (m *LoopManager) settleTerminal(loopID string, adopted *terminalOutcome) {`
- `processor/agentic-loop/terminal_owner.go:198` — `if err := c.persistLoopState(ctx, loopID); err != nil {`
- `processor/agentic-loop/component.go:3259` — `func (c *Component) persistLoopState(ctx context.Context, loopID string) error {`
- `processor/agentic-loop/component.go:3260` — `return c.writeLoopRecord(ctx, loopID, true)`
- `processor/agentic-loop/component.go:3270` — `func (c *Component) writeLoopRecord(ctx context.Context, loopID string, terminalWriter bool) error {`
- `processor/agentic-loop/component.go:3323` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)`
- `processor/agentic-loop/terminal_owner.go:452` — `func (c *Component) writeRecordCancelled(`
- `processor/agentic-loop/terminal_owner.go:479` — `if _, err := c.loopsBucket.Update(ctx, loopID, data, record.revision); err != nil {`

The one record read primitive and its revision carrier (`gopls references processor/agentic-loop/loop_evidence.go:282:21`
→ 10, 7 non-test callers):

- `processor/agentic-loop/loop_evidence.go:282` — `func (c *Component) readLoopRecord(ctx context.Context, loopID string) loopRecord {`
- `processor/agentic-loop/loop_evidence.go:291` — `entry, err := c.loopsBucket.Get(ctx, loopID)`
- `processor/agentic-loop/component.go:3108` — `func (c *Component) observedLoopRevision(loopID string) (uint64, bool) {`
- `processor/agentic-loop/component.go:113` — `loopRecordMu sync.Mutex`
- `processor/agentic-loop/component.go:116` — `loopsBucket           jetstream.KeyValue`
- `processor/agentic-loop/component.go:890` — `loopsBucket, err := loopbucket.AcquireOwner(ctx, js, name)`
- `processor/agentic-loop/internal/loopbucket/acquire.go:14` — `func AcquireOwner(ctx context.Context, js jetstream.KeyValueManager, name string) (jetstream.KeyValue, error) {`
- `processor/agentic-loop/internal/loopbucket/acquire.go:20` — `bucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, History: 10, TTL: 24 * time.Hour})`

Readers outside the owner package, production:

- `processor/agentic-dispatch/config.go:130` — `Name: agentLoopsPortName, Config: component.KVReadPort{Bucket: "AGENT_LOOPS"}, Required: false,`
- `processor/agentic-dispatch/terminal_settlement.go:158` — `entry, err := kv.Get(ctx, loopID)`
- `processor/agentic-dispatch/loop_admission.go:361` — `// persistedLoopFacts projects the durable AGENT_LOOPS record.`
- `processor/agentic-dispatch/loop_admission.go:368` — `Terminal:      record.State.IsTerminal(),`
- `processor/agentic-dispatch/http_activity.go:329` — `if entity == nil || entity.State.IsTerminal() || msg.UserID == "" || msg.ChannelType == "" || msg.ChannelID == "" ||`
- `agentic/doc.go:260` — `//   - AGENT_LOOPS: LoopEntity per loop ID`
- `graph/kvcatalog.go:9` — `// Application/product buckets (AGENT_LOOPS, personas, governance`

Readers, e2e:

- `test/e2e/scenarios/agentic/approval_signal.go:876` — `// awaitLoopState polls the durable AGENT_LOOPS record until the loop reports`
- `test/e2e/scenarios/agentic/approval_signal.go:880` — `func (s *Scenario) awaitLoopState(`
- `test/e2e/scenarios/agentic/approval_signal.go:643` — `cancelled, err := s.awaitLoopState(ctx, task.LoopID, agentic.LoopStateCancelled)`

### Home (e) — the other writers of the same record (non-terminal; the W3 racers)

Six distinct functions in two packages write `AGENT_LOOPS/<loopID>` (2 `Create`, 4 compare-and-swap `Update`); the
explorer pinned three and revision 1 of this file five. Five are in `processor/agentic-loop`
(`git grep -n "loopsBucket\.\(Create\|Update\|Put\)(" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 6 lines:
the marker `Create` at `terminal_owner.go:277` plus these five); the sixth is `frameworkcapabilities/graphresearch`'s
`CreateLoopEntity`, which creates a research-pipeline record at the bare key with `State = executing` and is the second
production writer `agentic-dispatch` already documents (`terminal_settlement.go:196-203`). That package also provisions
the bucket a second way, with the same `History: 10, TTL: 24h` policy the loop's `AcquireOwner` verifies.

- `processor/agentic-loop/component.go:3151` — `revision, err := c.loopsBucket.Create(ctx, loopID, data)`
- `processor/agentic-loop/component.go:3323` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)`
- `processor/agentic-loop/component.go:3442` — `func (c *Component) persistDeferredContinuationMarker(ctx context.Context, loopID, prompt string) error {`
- `processor/agentic-loop/component.go:3492` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)`
- `processor/agentic-loop/loop_evidence.go:425` — `func (c *Component) adoptNewerRetainedRequest(ctx context.Context, loopID string) (loopRecord, error) {`
- `processor/agentic-loop/loop_evidence.go:528` — `committed, err := c.loopsBucket.Update(ctx, loopID, data, record.revision)`
- `processor/agentic-loop/terminal_owner.go:479` — `if _, err := c.loopsBucket.Update(ctx, loopID, data, record.revision); err != nil {`
- `frameworkcapabilities/graphresearch/register_tool.go:92` — `_, err := w.kv.Create(ctx, loopID, value)`
- `frameworkcapabilities/graphresearch/executor.go:267` — `if err := e.kv.CreateLoopEntity(ctx, loopID, loopBytes); err != nil {`
- `frameworkcapabilities/graphresearch/register_tool.go:49` — `return natsClient.CreateKeyValueBucket(ctx, loopResultBucketConfig(bucketName))`
- `frameworkcapabilities/graphresearch/register_tool.go:72` — `return jetstream.KeyValueConfig{Bucket: bucket, History: 10, TTL: 24 * time.Hour}`
- `processor/agentic-dispatch/terminal_settlement.go:196` — `// The whole entity is validated, not just the state. TWO production`

The carrier's non-terminal write refuses to render a terminal snapshot (the #1377 fix), which is the sentinel that
routes it to the guard:

- `processor/agentic-loop/component.go:3296` — `if !terminalWriter && (errors.Is(err, ErrLoopNotFound) || err == nil && entity.State.IsTerminal()) {`
- `processor/agentic-loop/component.go:3297` — `return errTerminalOwnedElsewhere`
- `processor/agentic-loop/handlers.go:2672` — `var errTerminalOwnedElsewhere = errors.New("loop is terminal in memory or no longer held; the terminal owner writes its record")`

### Home (f) — terminal facts committed outside the owner

Every failure lane reaches `commitTerminal` through `handleLoopFailure` (`gopls references
processor/agentic-loop/component.go:2241:21` → 3 non-test callers: `component.go:2007` spawn-identity failure,
`component.go:2077` handler error, `approval_response_handler.go:442` continuation unavailable), and `commitTerminal`
itself has exactly 3 callers (`gopls references processor/agentic-loop/terminal_owner.go:151:21` → `component.go:2271`,
`:2452`, `:3732`). One terminal path bypasses the owner by ruling: an oversized birth transitions the held loop to
failed, records the trajectory observation and the graph stamp best-effort, and terminates the delivery with no
marker, no event and no record — so for that loop the fact exists in home (b) only.

- `processor/agentic-loop/component.go:1730` — `return c.terminateOversizedBirth(ctx, result.LoopID, task.TaskID, err)`
- `processor/agentic-loop/component.go:1805` — `// It does not go through the terminal owner: COMPLETE_<loopID>, the failure`
- `processor/agentic-loop/component.go:1990` — `func (c *Component) handleSpawnIdentityFailure(ctx context.Context, loopID string, entity agentic.LoopEntity, err error) error {`
- `processor/agentic-loop/component.go:2007` — `return c.handleLoopFailure(ctx, loopID, reason, err)`
- `processor/agentic-loop/component.go:2077` — `return c.handleLoopFailure(ctx, loopID, failureReasonForHandlerError(err), err)`
- `processor/agentic-loop/approval_response_handler.go:442` — `return c.handleLoopFailure(ctx, loopID, continuationUnavailableReason, cause)`
- `processor/agentic-loop/component.go:2271` — `established := c.commitTerminal(errorCtx, terminalOutcome{failed: failure},`
- `processor/agentic-loop/component.go:2452` — `if err := c.commitTerminal(ctx, terminalOutcomeOf(result), result); err != nil {`
- `processor/agentic-loop/component.go:3732` — `if err := c.commitTerminal(ctx, terminalOutcome{cancelled: &completion}, HandlerResult{`

### Home (g) — the research-pipeline loop: a second loop kind whose record never becomes terminal

`frameworkcapabilities/graphresearch` creates a `LoopEntity` at the bare key `AGENT_LOOPS/<loopID>` with
`role = research_pipeline` and `State = executing`, then writes the R0 trigger key. No code in `processor/research-graph-*`,
`frameworkcapabilities/` or `agentic/research/` writes a terminal `State` to that record
(`git grep -nE 'LoopStateComplete|LoopStateFailed|LoopStateCancelled|IsTerminal'` over those paths, non-test → 0), so it
stays `executing` until the bucket TTL removes it. That loop kind's terminal fact is spelled in three OTHER places — the
`search_result.complete.<loopID>` key (R6's trigger), a `COMPLETE_<loopID>` key holding the `SearchResult` envelope by
`Put`, and the `research.search-result.complete` graph stamp — none of them one of homes (a)–(d) as the terminal owner
writes them, and none on the record. Every reader keyed on the record's terminal `state` (in-tree dispatch `/loops` and
`/activity`; semspec's watchers and `findLoopIDForTask`; semsage `ui-api`) therefore reads a research loop as running
for up to 24 h after its result landed. Recorded as measured; whether that is intended is not judged here.

- `agentic/research/constants.go:98` — `const PipelineRole = "research_pipeline"`
- `frameworkcapabilities/graphresearch/executor.go:248` — `loopEntity := agentic.NewLoopEntity(loopID, "", research.PipelineRole, "", persistedIntent.MaxIterations)`
- `frameworkcapabilities/graphresearch/executor.go:249` — `loopEntity.State = agentic.LoopStateExecuting`
- `frameworkcapabilities/graphresearch/executor.go:291` — `if err := e.kv.PutResearchTrigger(ctx, loopID, triggerBytes); err != nil {`
- `frameworkcapabilities/graphresearch/register_tool.go:101` — `_, err := w.kv.Put(ctx, researchTriggerKeyPrefix+loopID, value)`
- `processor/research-graph-synthesize/adapters.go:152` — `_, err := s.kv.Put(ctx, loopStoreKeySearchResultComplete(loopID), envelope)`
- `processor/research-graph-synthesize/component.go:496` — `if err := c.loops.PutSearchResult(ctx, loopID, envelopeBytes); err != nil {`
- `agentic/research/orchestration.go:236` — `func BuildSearchResultCompleteTriples(loopEntityID, resultRef string, ts time.Time) []message.Triple {`

### The fifth durable effect on every terminal path — the ObjectStore trajectory observation

Not a home of the fact (it is evidence, ADR-068), but a durable write sequenced before the owner on each lane, so it
belongs in the order table (`gopls references processor/agentic-loop/trajectory_handler_wiring.go:116:21` → 3):

- `processor/agentic-loop/trajectory_handler_wiring.go:116` — `func (c *Component) recordTerminalObservation(`
- `processor/agentic-loop/component.go:2263` — `c.recordTerminalObservation(ctx, loopID, agentic.TrajectoryStatusFailed, agentic.TrajectoryErrorUnknown,`
- `processor/agentic-loop/component.go:3709` — `c.recordTerminalObservation(ctx, loopID, agentic.TrajectoryStatusCancelled, "",`

### Sister repositories — readers and writers of each home at `origin/main` (read-only; fetched 2026-09-27)

Rows are evidence, not pins (the verify script does not resolve sister paths). Version = the semstreams module each
sister pins in its `go.mod` at that sha. Search per sister: `git -C <sister> grep -n <pattern> origin/main -- '*.go'`
for `COMPLETE_`, `AGENT_LOOPS`, `agent\.complete`, `agent\.failed`, `agent\.loop\.outcome`,
`LoopCompletedEvent\|LoopFailedEvent\|LoopCancelledEvent`, `loop_completed`, `/activity`, non-test and test counted
separately; non-Go tracked content counted with `':!*.go'`.

| Sister | origin/main | pins semstreams | Home | file:line — text | Role |
|---|---|---|---|---|---|
| semspec | `9dd6a2d4b178ffb1c6898d0e7cb2174c45e30c90` | `v1.0.0-beta.107` | (a) | `cmd/semspec/watch_live.go:260` — `if strings.HasPrefix(e.Key, "COMPLETE_") {` | reader: key scan, counts active loops as records with no `COMPLETE_` sibling |
| semspec | same | same | (a) | `cmd/semspec/watch_live.go:240` — `// `COMPLETE_<loop_id>` key by execution-manager / requirement-executor` | reader's belief about the WRITER is wrong (the writer is agentic-loop's terminal owner) |
| semspec | same | same | (a) | `pkg/health/orchestrate.go:19` — `const completeLoopMarkerPrefix = "COMPLETE_"` | reader: a fifth independent spelling; skips marker rows when pulling trajectories |
| semspec | same | same | (d) | `processor/execution-manager/loop_completions.go:20` — `// This replaces the old agent.complete.> JetStream consumer. AGENT_LOOPS is` | reader: KV watch on the record, keyed on `loop.State.IsTerminal()` (`:249`) |
| semspec | same | same | (d) | 11 processors define `watchLoopCompletions` (`architecture-generator`, `execution-manager`, `lesson-decomposer`, `plan-reviewer`, `planner`, `qa-reviewer`, `recovery-agent`, `requirement-executor`, `requirement-generator`, `scenario-generator`, `story-preparer`) | readers: derived owners over a KV watch of the record's terminal state |
| semspec | same | same | (c) | `configs/semspec.json:458` — `"subject": "agentic.loop_completed.v1",` | a config subject that is not this tree's `agent.complete.*` (not traced; recorded) |
| semspec | same | same | (d) | `processor/execution-manager/req_watcher.go:207` — `bucket, err := js.KeyValue(ctx, "AGENT_LOOPS")` | reader: `findLoopIDForTask` (`:190`) does a `Keys`+`Get` full scan, decodes every value as `LoopEntity`, branches on `loop.State.IsTerminal()` — a record reader of a different shape from the 11 watchers |
| semspec | same | same | (d) | `pkg/health/capture.go:75` — `var DefaultKVBuckets = []string{"PLAN_STATES", "AGENT_LOOPS"}` | diagnostic reader: the health bundle captures the whole bucket |
| semteams | `ce22c961d30014c463a09f8f8a2a90044ee1a1cf` | `v1.0.0-beta.160` | (c) | `cmd/semteams/chainpause/pauser.go:65` — `func (p *Pauser) HandleFailed(ctx context.Context, ev *agentic.LoopFailedEvent) (PauseResult, error) {` | reader: `agent.failed.>` subscriber |
| semteams | same | same | (c) | `cmd/semteams/chainpause/subscriber.go:21` — `const DefaultLoopFailedSubject = "agent.failed.>"` | reader |
| semteams | same | same | (c) | `cmd/semteams/main.go:913` — `// agent.complete.*/agent.failed.*, pre-resolves the run, and fans out to` | reader: the framework `MilestoneSubscriber` |
| semteams | same | same | (c) | `cmd/semteams/portresolver/portresolver.go:60` — `// agent.failed and agent.complete with their canonical subjects).` | reader: resolves the two port names independently |
| semteams | same | same | (b) | `configs/rules/agent-run/05-coordinator-failed-run-anchor.json:18` — `"field": "agent.loop.outcome",` | reader: 47 non-Go mentions of the predicate across rule packs (`agent-run`, `autoresearch`, `create-change`, `dev-via-test`) |
| semteams | same | same | (a)/(d) | 23 non-Go mentions of `COMPLETE_`, 12 of `AGENT_LOOPS` (docs, skills, configs); 0 Go readers of the key | none in code |
| semsage | `4d28b4dc1210f47da84a3031125167d164de9290` | `v1.0.0-alpha.3` | (d) | `processor/ui-api/component.go:217` — `Description: "AGENT_LOOPS KV bucket — read for loop state and SSE activity",` | reader: whole-bucket KV watch → its own `/api/activity` SSE (`sse.go:15`), no `COMPLETE_` handling |
| semsage | same | same | (c) | `tools/spawn/executor.go:184` — `fmt.Sprintf("agent.complete.%s", childLoopID),` | reader: core-NATS subscribe before publish; `:283` decodes `LoopCompletedEvent`, `:301` `LoopFailedEvent`; malformed → dropped silently, duplicate → discarded |
| semsage | same | same | (c) | `workflow/dag/definition.go:28` — `CompletionSubject = "agent.complete.>"` | reader |
| semmachina | `d6b08326116193100372a332d24b27584a6ec274` | `v1.0.0-beta.160` | (c) | `internal/stage/loopfailure.go:31` — `LoopFailedSubject = "agent.failed.*"` | reader: durable consumer `semmachina-loop-failures`; `:501` decodes `LoopFailedEvent` |
| semmachina | same | same | (d) | `internal/resume/pending.go:88` — `// # Why AGENT_LOOPS cannot answer this` | a recorded refusal to read the record as a liveness signal (`state=running` is indistinguishable from a dead holder) |
| semdragon | `135a575cbca71d3b1864783118430cde3db56766` | `v1.0.0-beta.21` | (c) | `processor/questbridge/handler.go:447` — `FilterSubjects: []string{"agent.complete.*", "agent.failed.*"},` | reader: decodes `LoopCompletedEvent` (`:522`), `LoopCancelledEvent` on `agent.complete` (`:525`), `LoopFailedEvent` (`:606`); also `processor/questdagexec/events.go:257` |
| semdev | `7a22bc7422c978992977be6e168bca8c3dba138b` | `v1.0.0-beta.160` | (b) | `configs/rules/conversation/04-classifier-terminal-release.json:18` — `"field": "agent.loop.outcome",` | reader: rule packs (`conversation/04`, `05`; `dev-from-task/05`, `07d`); 19 non-Go mentions |
| semdev | same | same | (b) | `test/conformance/condition_literals.go:65` — `"agent.loop.outcome": "FRAMEWORK-owned value space that nothing upstream validates — semstreams #1057 "` | a conformance pin on the predicate's value space |
| semconnect | `d0d06e00bf05a545f30ceea798db1c2b1ee47d4f` | `v1.0.0-beta.160` | — | 0 Go hits; non-Go hits are captured container logs and a cutover template | none |
| semsource | `34bda6406fb06fd723a040988647b204204a1583` | `v1.0.0-beta.161` | — | 0 Go hits; 1 skill-doc mention of `AGENT_LOOPS` | none |
| semops | `d9ac7511c151b58de479ce3060adb7a1f7af9a7c` | — | — | 0 hits | none |
| semembed | `7ceb5281c96b3664321f3f28c9d7f96acbb41843` | — | — | 0 hits | none |
| semstreams-ui | `3814b3d59dab7136011429cad99a8148aeceb0cc` | — (TypeScript) | (a) via `/activity` | `src/lib/types/api.generated.ts:7` — `"/activity": {`; `:16` carries the generated description naming `loop_completed` and `COMPLETE_<id>` | a generated OpenAPI type only; no runtime consumer found (`git grep -n -E 'loop_completed|COMPLETE_|/activity|EventSource' origin/main -- 'src/**'` → the two generated lines and an unrelated `EventSource` mock in `RuntimePanel.test.ts`) |
| semboids | `8c03cc53836ced93a5df7064473c63ff144e64f1` | — | — | 0 hits | none |
| semlink | `a223955eb9a37269f5cedf2c3679a94d196773e8` | — | — | 0 hits | none |
| semmem | `5cb14ccf43e082bbac5072e87bf667e6f44f3224` | — | — | 0 hits | none |
| seminstruct | `7f9135a99cd27a6c63a2a60db5daeee9f5622be4` | — | — | 0 hits | none |
| semdocs | `2b96f6008a02b23ded7bb2665280c6a7317314d6` | — | — | 0 hits | none |
| semsummarize | — | — | — | not a git repository | not searchable at an `origin/main` |

Summary by home: (a) marker — semspec reads it (two spellings), no other sister; (b) predicate — semteams and semdev
rule packs, in-tree deep-research rules; (c) event — semteams, semsage, semmachina, semdragon, plus in-tree agentrun,
otel, dispatch; (d) record — semspec (11 KV-watching processors keyed on terminal `state`, a `Keys`+`Get` scan in `findLoopIDForTask`, and the `pkg/health` bundle capture), semsage (`ui-api` KV
watch), semmachina (explicitly refuses it as liveness), in-tree agentic-dispatch. The `/activity` SSE named in the
issue is consumed by semteams' UI per its docs (`docs/ui-integration-notes.md:29`) and has a sibling implementation
in semsage (`processor/ui-api/http.go:38`); no sister Go or TypeScript runtime code consumes `loop_completed` (0 non-test Go hits in all
sixteen; semstreams-ui carries it only in a generated OpenAPI type). Sister rows are measurements only; the owner
ruled (#1405 issuecomment-5858896669) that sisters receive migration notes and never gate the design.

## Hand-ordered publish + KV-write sites (measurement; scope item 1 and the acceptance BEFORE column)

Definition used (the census's definition at `078782b1` is not recoverable from #1146 issuecomment-5854803094, which
gives figures and no list): a SITE is a non-test function in `processor/agentic-loop` that either (i) sequences two
or more durable effects on the loop (marker KV, record KV, graph stamp, event publish, ObjectStore observation) or
(ii) performs one durable effect and decides the delivery disposition for its own failure. LANE CALL SITES are the
places a handler calls one of those functions and maps its error to a disposition. Orders are written as spelled in
code. Effects: M = marker `COMPLETE_` Create, MG = marker Get, S = graph stamp, P = event/request publish, R = record
CAS `Update`, RC = record `Create`, O = ObjectStore trajectory observation, mem = in-memory transition.

| # | Function | Pin | Order as spelled | Disposition per failing step |
|---|---|---|---|---|
| 1 | `commitTerminalSteps` | `terminal_owner.go:166` | M → (adopt: rebuild P) → S → P → mem settle → GetLoop → R → count | M Fatal; adopt-build Fatal; S: Fatal on budget expiry only — a mutation failure or a missing platform identity is Warn-and-continue inside the builder, no error, no metric (`graph_writer.go:285`, `:297`); P Fatal; GetLoop Fatal; R `ErrKVRevisionMismatch` returned as-is (loop already released) else Fatal |
| 2 | `createTerminalMarker` | `terminal_owner.go:265` | M Create → on conflict MG → decode → same loop ADOPT / other loop REFUSE | marshal/Create/Get/decode: raw error (Fatal at row 1); foreign loop: `fmt.Errorf` (Fatal at row 1); same loop: adopt, no error |
| 3 | `adoptDurableCancel` | `terminal_owner.go:391` | MG → decode → S → P → R (row 4) → count | not found/deleted: no-op; Get error Transient; decode Fatal; not a cancel: no-op; foreign Fatal; S: Fatal on `ctx.Err()` only, else Warn-and-continue (`graph_writer.go:509`, `:517`); P Fatal; R per row 4 |
| 4 | `writeRecordCancelled` | `terminal_owner.go:452` | read record → mutate → R | stale: no-op; unknown Transient; conflict Transient; other Fatal |
| 5 | `handleLoopFailure` | `component.go:2241` | mem Transition → mem UpdateCompletion → build event → O → row 1 (detached 5 s) → release | Transition error: plain error (Retry via closure), nothing written; build error with commit landed: plain error; CAS as-is; other Fatal |
| 6 | `handleCancelSignal` | `component.go:3678` | drain → mem CancelLoop → O → marshal → resolve subject → row 1 → release | CancelLoop error → `settleUncancellableLoop`; marshal/resolve: release + Fatal; CAS as-is; other Fatal |
| 7 | `persistHandlerResult`, terminal branch | `component.go:2452` | O (`recordHandlerResultTrajectory`) → row 1 → release | as returned by row 1 |
| 8 | `persistHandlerResult`, gated branch | `component.go:2472` | O → entry GetLoop check → stamp request name → R (`terminalWriter=false`) → P | stamp Fatal; `errTerminalOwnedElsewhere` → guard (row 24); CAS as-is; other Fatal; P Fatal |
| 9 | `publishThenPersistResultState` | `component.go:2512` | O (by caller) → entry check → P → stamp request name → R (`terminalWriter=false`) | P Fatal; stamp `ErrLoopNotFound` → guard; stamp other Fatal; `errTerminalOwnedElsewhere` → guard; CAS as-is; other Fatal |
| 10 | birth path in `handleTaskMessage` | `component.go:3151`, `:1773` | S (spawn identity, lineage) → RC → P | S failure → row 12 or `handleSpawnIdentityFailure` → row 5; RC `ErrMaxPayload` → row 12; RC `ErrKVKeyExists` → release + Transient; RC other → release + Transient; P → release + plain error (Retry) |
| 11 | `persistDeferredContinuationMarker` | `component.go:3442` | observed revision → read record → R | no revision Fatal; unknown Transient; stale no-op (logged); `ErrMaxPayload` drop prompt + plain error; conflict release + Transient; other plain error |
| 12 | `terminateOversizedBirth` | `component.go:1853` | mem Transition → mem UpdateCompletion → O → S (best-effort, detached 5 s) → release → Terminate | every step best-effort; delivery Terminated regardless; no M, no P, no R |
| 13 | `adoptNewerRetainedRequest` (step 0) | `loop_evidence.go:425` | read record → read retained request → R | stale: return record; unknown Transient; ordering/foreign Fatal; conflict Transient; other write error Transient |
| 14 | `republishPendingApproval` | `component.go:2996` | P only (echo from the record's gate) | build Fatal; P Transient |
| 15 | `publishResults` | `component.go:3013` | per message: adopt retained request → P (MsgID dedup) → context events | adopt error as returned (Fatal); P plain error (`publish result ...`) |
| 16 | `writeLoopRecord` (shared body of rows 1, 8, 9) | `component.go:3270` | render under `loopRecordMu` → refuse terminal snapshot when `terminalWriter=false` → R | `errTerminalOwnedElsewhere`; no observed revision Fatal; conflict release + Transient (`ErrKVRevisionMismatch`); other plain error |
| 17 | `settleDeferredContinuation` (lane wrapper of row 11) | `component.go:3362` | row 11 | nil Ack; CAS as-is (Retry); `ErrMaxPayload` Ack; else remember + Transient (Retry) |
| 18 | approval-timeout sweeper, terminal | `approval_sweeper.go:128` | row 7 | any error: `Warn`, continue (best-effort; a timer has no delivery to classify) |
| 19 | approval-timeout sweeper, ordinary | `approval_sweeper.go:163` | row 8 or 9 | any error: `Warn`, continue |
| 20 | approval answer, failed terminal | `approval_response_handler.go:232` | row 7 | nil Ack; `!IsFatal` Retry; `IsFatal` Quarantine |
| 21 | approval answer, ordinary | `approval_response_handler.go:283` | row 8 or 9 | nil Ack; `!IsFatal` Retry; `IsFatal` Quarantine |
| 22 | model-response lane | `component.go:2090` | row 7, 8 or 9 | bare error → generic closure (`component.go:1325`): Fatal → Quarantine, `PermanentDeliveryError` → Terminate, else Retry |
| 23 | tool-result lane, ordinary and failed-terminal | `component.go:2795`, `:2848` | row 7, 8 or 9 | bare error → generic closure |
| 24 | `settleTerminalGuard` (the disposition rows 8, 9 and the lanes route to) | `terminal_owner.go:505` | read record | stale: Ack + caller's drop metric; live/unknown: Transient |

Counts under the definition above: rows 1–16 are 16 sequencing functions; rows 17–23 are 7 lane call sites; row 24
is the shared guard. Distinct SPELLED sequences that contain both a publish and a KV write: 5 — `M→S→P→R` (rows 1,
5, 6, 7), `MG→S→P→R` (row 3), `P→R` (row 9), `R→P` (row 8), `RC→P` (row 10). Those reduce to 3 relative orders of
publish against KV write (KV-marker before publish before KV-record; publish before KV; KV before publish). KV-only
sequences: 3 (rows 4, 11, 13); publish-only: 1 (row 14); stamp-only terminal: 1 (row 12). Distinct GAP POLICIES
(what a failure between two durable effects does to the delivery), enumerated from the table: 6 — (1) Fatal →
Quarantine for commit-unknown; (2) `ErrKVRevisionMismatch` → Transient → Retry with the loop released; (3) adopt the
durable fact found (marker at row 2, retained request at rows 13 and 15) and continue; (4) Ack without effect
(row 24 stale; row 17 `ErrMaxPayload`; row 11 stale); (5) best-effort log-and-continue (rows 18, 19; the graph stamp at rows 1, 3 and 12 on a mutation failure or a
missing platform identity — the three builders return nothing, `graph_writer.go:297`, `:323`, `:517`, and
`WriteSyntheticDecide` at `:206` — so the event and the record land with no stamp and no metric); (6) Terminate (row 12; `errs.IsInvalid` → `PermanentDeliveryError` at
`component.go:1340`).

Outside the package definition, `graphresearch` sequences `RC → P` for the same record (`executor.go:267`, then the
trigger `Put` at `:291`); it is not counted in the 16.

Stamp failure policy, measured from the builders' bodies: `WriteLoopCompletion`, `WriteLoopFailure` and
`WriteLoopCancellation` return nothing; each returns silently when the platform identity is missing and logs a `Warn`
when `writeBatch` fails (a `MutationFailed` result or a request error). `WriteSyntheticDecide` has the same two exits.
The budget wrappers return an error only when `runWithBudget` times out, and the cancellation branch only on `ctx.Err()`.
`git grep -n "metrics\." -- processor/agentic-loop/graph_writer.go` → 0: no metric counts a swallowed stamp; only the
budget expiry is counted (`graph_write_publish_timeout_total`).

- `processor/agentic-loop/graph_writer.go:285` — `w.logger.Warn("graph_writer: cannot write loop completion, platform identity missing",`
- `processor/agentic-loop/graph_writer.go:297` — `w.logger.Warn("graph_writer: failed to write loop completion batch",`
- `processor/agentic-loop/graph_writer.go:311` — `w.logger.Warn("graph_writer: cannot write loop failure, platform identity missing",`
- `processor/agentic-loop/graph_writer.go:323` — `w.logger.Warn("graph_writer: failed to write loop failure batch",`
- `processor/agentic-loop/graph_writer.go:509` — `w.logger.Warn("graph_writer: cannot write loop cancellation, platform identity missing",`
- `processor/agentic-loop/graph_writer.go:517` — `w.logger.Warn("graph_writer: failed to write loop cancellation batch",`
- `processor/agentic-loop/graph_writer.go:160` — `func (w *graphWriter) WriteSyntheticDecide(ctx context.Context, loopID, modelText string) {`
- `processor/agentic-loop/graph_writer.go:206` — `w.logger.Warn("graph_writer: failed to write synthetic decide triples",`
- `processor/agentic-loop/component.go:2578` — `timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {`
- `processor/agentic-loop/component.go:2633` — `timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {`
- `processor/agentic-loop/terminal_owner.go:356` — `if err := ctx.Err(); err != nil {`

Pins for the rows above not already pinned in § Spellings:

- `processor/agentic-loop/terminal_owner.go:151` — `func (c *Component) commitTerminal(ctx context.Context, candidate terminalOutcome, publication HandlerResult) error {`
- `processor/agentic-loop/terminal_owner.go:265` — `func (c *Component) createTerminalMarker(`
- `processor/agentic-loop/terminal_owner.go:391` — `func (c *Component) adoptDurableCancel(ctx context.Context, loopID string) (bool, error) {`
- `processor/agentic-loop/component.go:2241` — `func (c *Component) handleLoopFailure(ctx context.Context, loopID string, reason string, err error) error {`
- `processor/agentic-loop/component.go:3678` — `func (c *Component) handleCancelSignal(ctx context.Context, signal agentic.UserSignal) error {`
- `processor/agentic-loop/component.go:2371` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:2460` — `return c.publishThenPersistResultState(ctx, result)`
- `processor/agentic-loop/component.go:2472` — `if err := c.writeLoopRecord(ctx, result.LoopID, false); err != nil {`
- `processor/agentic-loop/component.go:2473` — `if errors.Is(err, errTerminalOwnedElsewhere) {`
- `processor/agentic-loop/component.go:2512` — `func (c *Component) publishThenPersistResultState(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/component.go:2541` — `if err := c.writeLoopRecord(ctx, result.LoopID, false); err != nil {`
- `processor/agentic-loop/component.go:2542` — `if errors.Is(err, errTerminalOwnedElsewhere) {`
- `processor/agentic-loop/component.go:2996` — `echo, err := c.handler.approvalPendingMessage(loopID, gate)`
- `processor/agentic-loop/component.go:3362` — `func (c *Component) settleDeferredContinuation(ctx context.Context, task agentic.TaskMessage, result HandlerResult) error {`
- `processor/agentic-loop/component.go:1598` — `if result.Deferred {`
- `processor/agentic-loop/component.go:1603` — `return c.settleDeferredContinuation(ctx, *task, result)`
- `processor/agentic-loop/approval_sweeper.go:128` — `if commitErr := c.persistHandlerResult(ctx, result); commitErr != nil {`
- `processor/agentic-loop/approval_sweeper.go:163` — `if err := c.persistHandlerResult(ctx, result); err != nil {`
- `processor/agentic-loop/approval_response_handler.go:232` — `err = c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/approval_response_handler.go:283` — `if err := c.persistHandlerResult(ctx, result); err != nil {`
- `processor/agentic-loop/component.go:2090` — `return c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:2795` — `return c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:2848` — `return c.persistHandlerResult(ctx, result)`
- `processor/agentic-loop/component.go:1325` — `func(workCtx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:1335` — `if errs.IsFatal(handlerErr) {`
- `processor/agentic-loop/component.go:1336` — `return natsclient.DeliveryDecisionQuarantine, handlerErr`
- `processor/agentic-loop/component.go:1340` — `return natsclient.DeliveryDecisionTerminate, handlerErr`
- `processor/agentic-loop/component.go:1342` — `return natsclient.DeliveryDecisionRetry, handlerErr`

The record's critical section is taken at 6 sites (`git grep -n "loopRecordMu.Lock()" -- 'processor/agentic-loop/*.go'`
→ `component.go:3143`, `:3232`, `:3292`, `:3447`, `loop_evidence.go:426`, `terminal_owner.go:455`); the marker
`Create` at row 2 is outside it.

## "Loop not held → read record → decide" cold arms (scope item 3's evidence)

Definition: a site where the loop is not held in this process's memory (a `GetLoop` / `CancelLoop` /
`ResolveApprovalIfPending` miss returning `ErrLoopNotFound`, a `lookupWaiter` miss, an `observedLoopRevision` miss,
or the terminal-in-memory guard) and the durable record or marker is read to decide the delivery. The presence
vocabulary and the read primitive:

- `processor/agentic-loop/loop_presence.go:10` — `// loopPresence answers one question, asked at six call sites in different`
- `processor/agentic-loop/loop_presence.go:23` — `type loopPresence int`
- `processor/agentic-loop/loop_presence.go:28` — `loopPresenceStale loopPresence = iota`
- `processor/agentic-loop/loop_presence.go:39` — `loopPresenceLive`
- `processor/agentic-loop/loop_presence.go:43` — `loopPresenceUnknown`
- `processor/agentic-loop/loop_presence.go:70` — `func (c *Component) classifyMissingLoop(ctx context.Context, loopID string) loopPresence {`
- `processor/agentic-loop/loop_presence.go:71` — `return c.readLoopRecord(ctx, loopID).presence`
- `processor/agentic-loop/state.go:701` — `func (m *LoopManager) GetLoop(loopID string) (agentic.LoopEntity, error) {`
- `processor/agentic-loop/handlers.go:3484` — `func (h *MessageHandler) GetLoop(loopID string) (agentic.LoopEntity, error) {`

| # | Arm | Pin | Lane | Miss signal | Record/marker read via | Same question as |
|---|---|---|---|---|---|---|
| 1 | `settleResponseWithoutLoop` | `component.go:2157` | model response | routing lookup → `ErrLoopNotFound` | `adoptNewerRetainedRequest` (arm 10) | stale → Ack + `stale_request_id`; unknown → Retry; live → order check → Ack `superseded_request` / Fatal foreign / rebuild |
| 2 | `settleToolResultWithoutLoop` | `component.go:2866` | tool result | routing lookup → `ErrLoopNotFound` | arm 10 | same three-way switch as arm 1, then membership; Ack `stale_execution`/`older_request`/`already_applied` |
| 3 | `settleApprovalResponseWithoutLoop` | `approval_response_handler.go:371` | approval | `ResolveApprovalIfPending` → `ErrLoopNotFound` | arm 10 | same three-way switch; Ack `approval_inapplicable`; Fatal on gate/record mismatch |
| 4 | `settleUncancellableLoop` | `component.go:3645` | cancel | `CancelLoop` → `ErrLoopNotFound` or Invalid | `classifyMissingLoop` then marker via arm 11 | stale → Ack `stale_loop_id`; terminal-in-memory → Ack `already_terminal`; live → adopt cancel marker or Retry |
| 5 | `settleVerdictWithoutWaiter` | `component.go:3814` | governance verdict | `lookupWaiter` miss → `ErrNoGovernanceWaiter` | `readLoopRecord` → `classifyWaiterlessVerdict` | same three-way switch spelled as `verdictDrop*`; Terminate on foreign request |
| 6 | `classifyRedeliveredTask` | `loop_classification.go:142` | task | `GetLoop` miss | `readLoopRecord` | unknown → Retry; stale+terminal → `taskApplied` (Ack); stale+absent → birth; live → task ownership |
| 7 | `persistDeferredContinuationMarker` | `component.go:3442` | deferred turn (writer) | `observedLoopRevision` miss | `readLoopRecord` | unknown → Retry; stale → no-op; live → write |
| 8 | `settleTerminalGuard` | `terminal_owner.go:505` | shared (8 callers) | `terminalOwnedElsewhere` / entry check / `errTerminalOwnedElsewhere` | `readLoopRecord` | stale → Ack + caller's drop metric; live/unknown → Retry |
| 9 | `writeRecordCancelled` | `terminal_owner.go:452` | cancel (writer) | called after a cancel marker was found | `readLoopRecord` | stale → no-op; unknown → Retry; live → CAS |
| 10 | `adoptNewerRetainedRequest` | `loop_evidence.go:425` | shared step 0 | called after the caller's miss | `readLoopRecord` | stale → return; unknown → Retry; live → adopt/write |
| 11 | `adoptDurableCancel` | `terminal_owner.go:391` | cancel | called from arm 4 | marker `Get` | absent → not this arm's; other kind → not this arm's; cancel → adopt; foreign → Fatal |
| 12 | `createTerminalMarker` (the owner's own read) | `terminal_owner.go:285` | every terminal lane | `Create` refused | marker `Get` | same loop → adopt whatever kind; other loop → refuse (Fatal) |

Miss-only decisions (no durable read; listed because they answer the same "not held" question by memory alone):

| # | Site | Pin | Decision |
|---|---|---|---|
| 13 | `resumeDeferredContinuation` | `component.go:3391` | `GetLoop` miss or turn mismatch → Ack without effect, unconditionally |
| 14 | approval-timeout sweeper | `approval_sweeper.go:105` | `ErrLoopNotFound` → log "already released", continue; no record read, no metric |
| 15 | `persistHandlerResult` entry check | `component.go:2423` | `ErrLoopNotFound` or terminal in memory → arm 8 |
| 16 | `writeLoopRecord` non-terminal render | `component.go:3296` | `ErrLoopNotFound` or terminal in memory → `errTerminalOwnedElsewhere` → arm 8 |

Counts: 12 arms read the record or marker, in 6 files (`component.go`, `approval_response_handler.go`,
`loop_classification.go`, `loop_evidence.go`, `loop_presence.go`, `terminal_owner.go`); 4 miss-only decisions in 2
files, one of them new (`approval_sweeper.go`) — 16 sites in 7 files. The three-way presence switch (`stale → Ack`,
`unknown → Retry`, `live → lane-specific`) is spelled at arms 1–10 through `loopPresenceStale` comparisons:
`gopls references processor/agentic-loop/loop_presence.go:28:2` → 15 total, 13 non-test. Row 12 is the terminal
owner's own read (the issue's example of a legitimate hit); rows 9–11 are the cancel lane's second writer and its
marker adopt.

- `processor/agentic-loop/component.go:2157` — `func (c *Component) settleResponseWithoutLoop(ctx context.Context, requestID string) (bool, error) {`
- `processor/agentic-loop/component.go:2164` — `adopted, err := c.adoptNewerRetainedRequest(ctx, loopID)`
- `processor/agentic-loop/component.go:2866` — `func (c *Component) settleToolResultWithoutLoop(ctx context.Context, toolResult agentic.ToolResult) (bool, error) {`
- `processor/agentic-loop/component.go:2872` — `adopted, err := c.adoptNewerRetainedRequest(ctx, loopID)`
- `processor/agentic-loop/approval_response_handler.go:371` — `func (c *Component) settleApprovalResponseWithoutLoop(`
- `processor/agentic-loop/approval_response_handler.go:375` — `record, err := c.adoptNewerRetainedRequest(ctx, loopID)`
- `processor/agentic-loop/approval_response_handler.go:379` — `if record.presence == loopPresenceStale {`
- `processor/agentic-loop/component.go:3645` — `func (c *Component) settleUncancellableLoop(ctx context.Context, loopID string, cause error) error {`
- `processor/agentic-loop/component.go:3654` — `if errors.Is(cause, ErrLoopNotFound) && c.classifyMissingLoop(ctx, loopID) == loopPresenceStale {`
- `processor/agentic-loop/component.go:3666` — `adopted, err := c.adoptDurableCancel(ctx, loopID)`
- `processor/agentic-loop/component.go:3814` — `func (c *Component) settleVerdictWithoutWaiter(`
- `processor/agentic-loop/component.go:3829` — `record := c.readLoopRecord(ctx, loopID)`
- `processor/agentic-loop/component.go:3898` — `case loopPresenceStale:`
- `processor/agentic-loop/governance_dispatcher.go:501` — `func (d *enforceDispatcher) lookupWaiter(callID string) (chan verdictArrival, bool) {`
- `processor/agentic-loop/governance_dispatcher.go:609` — `func (d *enforceDispatcher) HandleVerdict(decision, executionID string, verdict VerdictPayload) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/loop_classification.go:142` — `func (c *Component) classifyRedeliveredTask(`
- `processor/agentic-loop/loop_classification.go:155` — `if _, err := c.handler.GetLoop(loopID); err == nil {`
- `processor/agentic-loop/loop_classification.go:158` — `record := c.readLoopRecord(ctx, loopID)`
- `processor/agentic-loop/loop_classification.go:171` — `return taskApplied, record, nil`
- `processor/agentic-loop/component.go:3457` — `record := c.readLoopRecord(ctx, loopID)`
- `processor/agentic-loop/terminal_owner.go:458` — `record := c.readLoopRecord(ctx, loopID)`
- `processor/agentic-loop/terminal_owner.go:506` — `record := c.readLoopRecord(ctx, result.LoopID)`
- `processor/agentic-loop/loop_evidence.go:429` — `record := c.readLoopRecord(ctx, loopID)`
- `processor/agentic-loop/component.go:3391` — `func (c *Component) resumeDeferredContinuation(ctx context.Context, task agentic.TaskMessage, pending HandlerResult) error {`
- `processor/agentic-loop/component.go:3392` — `entity, err := c.handler.GetLoop(pending.LoopID)`
- `processor/agentic-loop/approval_sweeper.go:104` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`
- `processor/agentic-loop/approval_sweeper.go:105` — `if errors.Is(err, ErrLoopNotFound) {`
- `processor/agentic-loop/component.go:2423` — `case errors.Is(err, ErrLoopNotFound), err == nil && held.State.IsTerminal():`

Held-check spellings behind the arms: `gopls references processor/agentic-loop/handlers.go:3484:26`
(`MessageHandler.GetLoop`) → 81 total, 14 non-test; `git grep -n "loopManager.GetLoop(" -- 'processor/agentic-loop/*.go'`
(direct manager reads inside `handlers.go`) → 7.

## `ErrLoopNotFound` decision sites

`gopls references processor/agentic-loop/state.go:59:2` → 18 (13 non-test in 4 files, 5 in tests). Of the 13: 7 are
decision branches in 3 files and 6 are creation sites in `state.go` (a `LoopManager` map miss wrapped into the
sentinel). `git grep -n "ErrLoopNotFound" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 21 lines in 5
files: the 7 + 6 above, the declaration, and 7 comment lines (`approval_response_handler.go:30`, `:59`,
`approval_sweeper.go:111`, `handlers.go:121`, `state.go:54`, `:711`, `:830`) — `handlers.go` is the fifth file and
holds only a comment. Outside `processor/agentic-loop`: 0 (`git grep -n "ErrLoopNotFound" -- '*.go' | grep -v
"^processor/agentic-loop/"` → empty).

- `processor/agentic-loop/state.go:59` — `ErrLoopNotFound = errors.New("agentic-loop: loop not found")`
- `processor/agentic-loop/approval_response_handler.go:205` — `if errors.Is(err, ErrLoopNotFound) {`
- `processor/agentic-loop/approval_sweeper.go:105` — `if errors.Is(err, ErrLoopNotFound) {`
- `processor/agentic-loop/component.go:2423` — `case errors.Is(err, ErrLoopNotFound), err == nil && held.State.IsTerminal():`
- `processor/agentic-loop/component.go:2531` — `if errors.Is(err, ErrLoopNotFound) {`
- `processor/agentic-loop/component.go:3296` — `if !terminalWriter && (errors.Is(err, ErrLoopNotFound) || err == nil && entity.State.IsTerminal()) {`
- `processor/agentic-loop/component.go:3654` — `if errors.Is(cause, ErrLoopNotFound) && c.classifyMissingLoop(ctx, loopID) == loopPresenceStale {`
- `processor/agentic-loop/component.go:3662` — `if errors.Is(cause, ErrLoopNotFound) {`
- `processor/agentic-loop/state.go:296` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),`
- `processor/agentic-loop/state.go:602` — `return errs.Wrap(fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),`
- `processor/agentic-loop/state.go:715` — `return agentic.LoopEntity{}, errs.Wrap(fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound), "LoopManager", "GetLoop", "find loop")`
- `processor/agentic-loop/state.go:837` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),`
- `processor/agentic-loop/state.go:1327` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound),`
- `processor/agentic-loop/state.go:2072` — `fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound), "LoopManager", "CancelLoop", "find loop")`

## Adopt functions

Three adopt functions plus the marker adopt; production callers by `gopls references`:

- `processor/agentic-loop/loop_evidence.go:340` — `func (c *Component) adoptRetainedRequest(ctx context.Context, loopID, requestID string) (bool, error) {`
- `processor/agentic-loop/component.go:3023` — `published, err := c.adoptRetainedRequest(ctx, result.LoopID, msg.MsgID)`
- `processor/agentic-loop/loop_evidence.go:425` — `func (c *Component) adoptNewerRetainedRequest(ctx context.Context, loopID string) (loopRecord, error) {`
- `processor/agentic-loop/terminal_owner.go:391` — `func (c *Component) adoptDurableCancel(ctx context.Context, loopID string) (bool, error) {`
- `processor/agentic-loop/terminal_owner.go:265` — `func (c *Component) createTerminalMarker(`
- `processor/agentic-loop/terminal_owner.go:168` — `outcome, adopted, err := c.createTerminalMarker(ctx, loopID, candidate)`
- `processor/agentic-loop/terminal_owner.go:294` — `c.logger.WarnContext(ctx, "Terminal refused — the loop's durable terminal names another loop",`
- `processor/agentic-loop/terminal_owner.go:310` — `c.logger.WarnContext(ctx, "Terminal adopted the loop's durable terminal",`

`adoptRetainedRequest`: 1 production caller (warm publish path). `adoptNewerRetainedRequest`: 3 (arms 1–3).
`adoptDurableCancel`: 1 (arm 4). `createTerminalMarker`: 1 (row 1). The two request adopts and the two marker adopts
answer different facts (which request is retained; which terminal is durable) with the same shape (read the durable
fact, prefer it over the candidate, continue).

## Vocabulary for "this delivery is inapplicable"

Every spelling in non-test `processor/agentic-loop` that means the delivery cannot be applied and is acknowledged
without effect (or terminated), with non-test uses by the tool named. Declarations excluded from "uses"; comments
excluded.

| Spelling | Kind | Declaration | Non-test uses | Tool |
|---|---|---|---|---|
| `loopPresenceStale` | enum value | `loop_presence.go:28` | 13 | gopls |
| `staleDrop` | `HandlerResult` field | `handlers.go:123` | 3 | gopls |
| `terminalOwnedElsewhere` | `HandlerResult` field | `handlers.go:109` | 6 | gopls |
| `errTerminalOwnedElsewhere` | sentinel error | `handlers.go:2672` | 3 (`component.go:2473`, `:2542`, `:3297`) | git grep |
| `requestOrderApplied` | enum value | `loop_classification.go:36` | 5 (`component.go:2186`, `:2896`, `:3908`, `handlers.go:1387`, `loop_classification.go:302`) | git grep |
| `taskApplied` | enum value | `loop_classification.go:111` | 4 (`component.go:1542`, `loop_classification.go:171`, `:251`, `:261`) | git grep |
| `verdictDrop*` | 7 string consts | `metrics.go:401-414` | 7 (`component.go:3900`, `:3902`, `:3909`, `:3911`, `:3914`, `metrics.go:425`, `:436`) | git grep |
| `"stale_request_id"` | bare reason literal | — | 2 (`component.go:2085`, `:2171`) | git grep |
| `"superseded_request"` | bare reason literal | — | 2 (`component.go:2191`, `handlers.go:1395`) | git grep |
| `"stale_execution"` | bare reason literal | — | 1 (`component.go:2883`) | git grep |
| `"older_request"` | bare reason literal | — | 2 (`component.go:2902`, `loop_classification.go:312`) | git grep |
| `"already_applied"` | bare reason literal | — | 3 (`component.go:2951`, `handlers.go:1447`, `loop_classification.go:339`) | git grep |
| `"terminal_unproven"` | bare reason literal | — | 2 (`loop_classification.go:296`, `terminal_owner.go:537`) | git grep |
| `"approval_inapplicable"` | bare reason literal | — | 1 (`approval_response_handler.go:304`) | git grep |
| `"already_terminal"` | bare reason literal | — | 1 (`component.go:3650`) | git grep |
| `"stale_loop_id"` | bare reason literal | — | 1 (`component.go:3658`) | git grep |

Totals under this definition: 4 typed identifiers + 2 order/disposition enum values + 7 consts + 9 bare literals =
22 spellings; 56 non-test uses. Two reasons are spelled twice — `older_request` and `already_applied` exist both as
`verdictDrop*` consts (verdict lane) and as bare literals (response, tool and task lanes). Under the census's and the
explorer's narrower grouping (3 identifiers + the `verdictDrop*` family + 3 literal rows = 7 groups) the use count is
43–45 depending on whether declarations and comments count; the census's 42 is not reproducible from the code.

- `processor/agentic-loop/handlers.go:109` — `terminalOwnedElsewhere bool`
- `processor/agentic-loop/handlers.go:123` — `staleDrop bool`
- `processor/agentic-loop/handlers.go:2683` — `terminalOwnedElsewhere: true,`
- `processor/agentic-loop/handlers.go:2693` — `return err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere`
- `processor/agentic-loop/loop_classification.go:36` — `requestOrderApplied`
- `processor/agentic-loop/loop_classification.go:111` — `taskApplied`
- `processor/agentic-loop/component.go:1542` — `if disposition == taskApplied {`
- `processor/agentic-loop/metrics.go:401` — `verdictDropMissingWaiter         = "missing_waiter"`
- `processor/agentic-loop/metrics.go:414` — `verdictDropForeignRequest = "foreign_request"`
- `processor/agentic-loop/component.go:3900` — `return verdictDropLoopTerminal, natsclient.DeliveryDecisionAck`
- `processor/agentic-loop/component.go:2085` — `c.metrics.recordModelResponseDropped("stale_request_id")`
- `processor/agentic-loop/component.go:2171` — `c.metrics.recordModelResponseDropped("stale_request_id")`
- `processor/agentic-loop/component.go:2191` — `c.metrics.recordModelResponseDropped("superseded_request")`
- `processor/agentic-loop/handlers.go:1395` — `h.metrics.recordModelResponseDropped("superseded_request")`
- `processor/agentic-loop/component.go:2883` — `c.metrics.recordToolResultDropped("stale_execution")`
- `processor/agentic-loop/component.go:2902` — `c.metrics.recordToolResultDropped("older_request")`
- `processor/agentic-loop/loop_classification.go:312` — `c.metrics.recordToolResultDropped("older_request")`
- `processor/agentic-loop/component.go:2951` — `c.metrics.recordToolResultDropped("already_applied")`
- `processor/agentic-loop/handlers.go:1447` — `h.metrics.recordModelResponseDropped("already_applied")`
- `processor/agentic-loop/loop_classification.go:339` — `c.metrics.recordToolResultDropped("already_applied")`
- `processor/agentic-loop/loop_classification.go:296` — `c.metrics.recordToolResultDropped("terminal_unproven")`
- `processor/agentic-loop/terminal_owner.go:537` — `c.metrics.recordToolResultDropped("terminal_unproven")`
- `processor/agentic-loop/approval_response_handler.go:304` — `c.metrics.recordToolResultDropped("approval_inapplicable")`
- `processor/agentic-loop/component.go:3650` — `c.metrics.recordSignalDropped("already_terminal")`
- `processor/agentic-loop/component.go:3658` — `c.metrics.recordSignalDropped("stale_loop_id")`
- `processor/agentic-loop/component.go:3839` — `c.metrics.recordVerdictSettledByRecord(reason)`

## Drop-metric families

Three `*_dropped_total` counter families, each with one recorder method, plus a fourth family that carries the same
class of reason for the verdict lane under a non-drop name. Six recorder functions in total (four on `loopMetrics`,
two on `Component`).

- `processor/agentic-loop/metrics.go:170` — `Name:      "tool_results_dropped_total",`
- `processor/agentic-loop/metrics.go:177` — `Name:      "model_responses_dropped_total",`
- `processor/agentic-loop/metrics.go:191` — `Name:      "signals_dropped_total",`
- `processor/agentic-loop/metrics.go:290` — `Name:      "tool_call_governance_subscribe_before_publish_failures_total",`
- `processor/agentic-loop/metrics.go:604` — `func (m *loopMetrics) recordToolResultDropped(reason string) {`
- `processor/agentic-loop/metrics.go:621` — `func (m *loopMetrics) recordSignalDropped(reason string) {`
- `processor/agentic-loop/metrics.go:639` — `func (m *loopMetrics) recordModelResponseDropped(reason string) {`
- `processor/agentic-loop/metrics.go:443` — `func (m *loopMetrics) recordVerdictSettledByRecord(reason string) {`
- `processor/agentic-loop/approval_response_handler.go:302` — `func (c *Component) recordApprovalInapplicable(response agentic.ApprovalResponse) {`
- `processor/agentic-loop/terminal_owner.go:535` — `func (c *Component) recordTerminalToolResultDropped() {`

The terminal counters the owner increments once per committed terminal, for the collision table's Status row:

- `processor/agentic-loop/metrics.go:94` — `Name:      "loops_completed_total",`
- `processor/agentic-loop/metrics.go:101` — `Name:      "loops_failed_total",`
- `processor/agentic-loop/metrics.go:108` — `Name:      "active_loops",`

## `WrapFatal` sites and the lane latch (scope item 4's evidence)

`git grep -c 'errs.WrapFatal(' -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 52 in 6 files:
`approval_response_handler.go` 3, `component.go` 19, `handlers.go` 2, `loop_classification.go` 1,
`loop_evidence.go` 14, `terminal_owner.go` 13 (the explorer lists all 52 lines; they are not repeated here).
`errs.WrapTransient(` → 36 in 5 files (`component.go` 15, `loop_classification.go` 2, `loop_evidence.go` 10,
`state.go` 5, `terminal_owner.go` 4). A Fatal reaches the latch through six `errs.IsFatal(` checks (git grep) that
return `DeliveryDecisionQuarantine` — four on the approval lane, one on the signal lane, and the generic closure at
`component.go:1335` that converts the task, response and tool-result lanes — and every Quarantine stops the owner:

- `processor/agentic-loop/approval_response_handler.go:209` — `if errs.IsFatal(coldErr) {`
- `processor/agentic-loop/approval_response_handler.go:236` — `if !errs.IsFatal(err) {`
- `processor/agentic-loop/approval_response_handler.go:246` — `case errs.IsFatal(err):`
- `processor/agentic-loop/approval_response_handler.go:287` — `if !errs.IsFatal(err) {`
- `processor/agentic-loop/component.go:1335` — `if errs.IsFatal(handlerErr) {`
- `processor/agentic-loop/component.go:3622` — `if errs.IsFatal(err) {`

One Quarantine return bypasses `IsFatal`: the enforce dispatcher's fail-closed placeholder, paired with
`ErrNoGovernanceWaiter` so that `handleToolCallVerdictMessage` replaces it from the record (§ Cold arms, arm 5):

- `processor/agentic-loop/governance_dispatcher.go:637` — `return natsclient.DeliveryDecisionQuarantine,`
- `natsclient/delivery_settlement.go:404` — `case DeliveryDecisionRetry, DeliveryDecisionTerminate, DeliveryDecisionQuarantine:`
- `natsclient/delivery_settlement.go:419` — `ownerStopNeeded: work.decision == DeliveryDecisionQuarantine,`
- `internal/deliverylane/deliverylane.go:31` — `onFatal   func(natsclient.DeliveryResult)`
- `internal/deliverylane/deliverylane.go:82` — `a.onFatal(result)`
- `processor/agentic-loop/component.go:1091` — `func (c *Component) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {`
- `processor/agentic-loop/component.go:149` — `deliveryFatalErr error`
- `processor/agentic-loop/component.go:530` — `if c.deliveryFatalErr != nil {`

The W2 latch that #1399 removed for the same-loop case is asserted for a second loop by
`processor/agentic-loop/approval_cap_sweep_integration_test.go` (18 test files in the package touch the terminal
owner surface: `git grep -l "COMPLETE_\|commitTerminal\|settleTerminalGuard\|adoptDurableCancel\|createTerminalMarker\|terminalMarkerKey" -- 'processor/agentic-loop/*_test.go' | wc -l`
→ 18). The W1 convergence has a unit-level integration test
(`processor/agentic-loop/lost_terminal_record_integration_test.go`) and no e2e assertion (`git grep -n
"ErrKVRevisionMismatch\|lost the record race\|record moved past" -- test/e2e/` → 0).

- `processor/agentic-loop/approval_cap_sweep_integration_test.go:218` — `c, h := lane.c, lane.h`

## `DeliveryDecision` use versus the same outcome by another means

`gopls references natsclient/delivery_settlement.go:17:6` → 101 total: `natsclient` 40, `processor/agentic-loop` 27,
`internal/deliverylane` 10, `processor/agentic-tools` 7, `processor/agentic-dispatch` 5, `agentic/agentrun` 5,
`processor/agentic-governance` 4, `processor/agentic-model` 3. Constant returns in non-test `processor/agentic-loop`
(`git grep -n "natsclient.DeliveryDecision\(Ack\|Retry\|Terminate\|Quarantine\)"`) → 50: `component.go` 27,
`approval_response_handler.go` 17, `governance_dispatcher.go` 6. Six input handler seams are selected by one
`switch port.Name`; three return the typed decision and three return a bare `error` that one closure converts:

- `processor/agentic-loop/component.go:185` — `type inputHandler func(context.Context, []byte) error`
- `processor/agentic-loop/component.go:991` — `settleHandlerFn func(context.Context, []byte) (natsclient.DeliveryDecision, error)`
- `processor/agentic-loop/component.go:999` — `handler = c.handleResponseMessage`
- `processor/agentic-loop/component.go:1001` — `handler = c.handleToolResultMessage`
- `processor/agentic-loop/component.go:1492` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:2011` — `func (c *Component) handleResponseMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:2680` — `func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/approval_response_handler.go:186` — `func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:3591` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:3771` — `func (c *Component) handleToolCallVerdictMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`

The guard's drop callback is passed in three shapes across its 8 callers (`gopls references
processor/agentic-loop/terminal_owner.go:505:21` → 8, all non-test): `c.recordTerminalToolResultDropped` ×6, an
inline closure ×1 (`component.go:2083`), `nil` ×1 (`component.go:2382`).

- `processor/agentic-loop/approval_response_handler.go:278` — `if err := c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped); err != nil {`
- `processor/agentic-loop/component.go:2083` — `return c.settleTerminalGuard(ctx, result, func() {`
- `processor/agentic-loop/component.go:2382` — `return c.settleTerminalGuard(ctx, result, nil)`
- `processor/agentic-loop/component.go:2424` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2476` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2536` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2546` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`
- `processor/agentic-loop/component.go:2788` — `return c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped)`

## Ad hoc test hooks

Four function-valued fields (`git grep -nP "^\s+test[A-Za-z]+\s+func\(" -- 'processor/agentic-loop/*.go' | grep -v _test.go`
→ 4; the `-E` form of the same pattern returns 0 because `\s` is not ERE), set at five test sites:

- `processor/agentic-loop/component.go:167` — `testPublishHook func(subject string, data []byte)`
- `processor/agentic-loop/component.go:170` — `testLineageWriteHook func(context.Context, string, map[string]any) error`
- `processor/agentic-loop/component.go:180` — `testCarrierHook func(loopID, stage string)`
- `processor/agentic-loop/handlers.go:202` — `testApprovedDispatchHook func(loopID, stage string)`
- `processor/agentic-loop/export_test.go:47` — `c.testPublishHook = fn`
- `processor/agentic-loop/spawn_identity_failure_test.go:253` — `c.testLineageWriteHook = func(_ context.Context, loopID string, _ map[string]any) error {`
- `processor/agentic-loop/lineage_preflight_test.go:215` — `component.testLineageWriteHook = func(context.Context, string, map[string]any) error {`
- `processor/agentic-loop/carrier_terminal_race_integration_test.go:587` — `c.testCarrierHook = pause.hook`
- `processor/agentic-loop/carrier_terminal_race_integration_test.go:468` — `lane.h.testApprovedDispatchHook = pause.hook`

## Adjacent claims (category 3)

- #1146 — parent issue, OPEN (beta.163); Q1–Q7 rulings (issuecomment-5854830449, issuecomment-5856142786); census issuecomment-5854802835; judgment issuecomment-5854803094
- #1405 — this change, OPEN (beta.165); scope items 1–5 and the owner's acceptance test
- #1399 (PR #1402, `73ea4f26`) and #1400 (PR #1403, `3dc4ccbe`) — CLOSED predecessors; this base reads the tree after both
- #1362, #1377 — CLOSED; the rulings `commitTerminal`'s doc comment cites (`terminal_owner.go:118`, `:372`); #1362 ruling 1 (cold cancel Retries) is untouched by Q2
- #1398 — OPEN (beta.165): an audit attempt started after release re-marks the loop; touches `persistHandlerResult`'s order (`component.go:2401-2410` comment)
- #1242 — OPEN (beta.165): `active_loops` drift; `terminateOversizedBirth`'s doc names it as the same class
- #1244 — OPEN (beta.165): loop state adopts the StopAll exit contract — touches the same lifecycle seam
- #1314 — OPEN (rc.1): `LoopState` vocabulary collapse — any consolidation that re-seats `State`/`Outcome` (`state.go:2012`) sits on this
- #1283 — OPEN (beta.165): bounded Stop on the rule engine; unrelated code, same "bounded terminal" class
- #753 — OPEN (rc.1): v1 sister-repo cutover tracking — where a sister migration table would be tracked
- `openspec/specs/agentic-loop/spec.md:813` — `### Requirement: Per-loop in-process state is released at terminal, through the one release point`
- `openspec/specs/agentic-loop/spec.md:1241` — `### Requirement: A loop absent from process memory is settled from its record`
- `openspec/specs/agentic-loop/spec.md:1684` — `### Requirement: The loop record names its outstanding request`
- `openspec/specs/agentic-loop/spec.md:1707` — `request with no durable gate behind it. A terminal outcome SHALL be committed as `COMPLETE_<loopID>` by create-once`
- `openspec/specs/agentic-loop/spec.md:1711` — `SHALL adopt the loop's durable terminal by loop ID. On that commit path a durable terminal of a different kind from`
- `openspec/specs/agentic-loop/spec.md:1990` — `record write lost its compare-and-swap because process B had rebuilt the loop from a redelivered input and advanced`
- `openspec/specs/agentic-terminal-events/spec.md:245` — `### Requirement: Framework terminal consumers SHALL share one interpretation`
- `openspec/specs/agentic-dispatch/spec.md:504` — `Dispatch SHALL use one caught-up graph view over `AGENT_LOOPS` for `/activity`, `/loops`, `/debug/state`, and`
- `openspec/specs/agentic-dispatch/spec.md:628` — `Dispatch SHALL decide a loop's existence, ownership and state ONLY from the exact `AGENT_LOOPS/<LoopID>` record read`
- `openspec/specs/agentic-dispatch/spec.md:726` — `Existing ordinary `COMPLETE_` activity SHALL retain its field mappings and canonical suffix/payload identity checks.`
- `docs/adr/028-orchestration-architecture.md:61` — `- Bulky content lives in durable stores: `COMPLETE_{loopID}` in AGENT_LOOPS`
- `docs/adr/081-graph-view-subscription.md:25` — `- `processor/agentic-dispatch/http.go:902` — one AGENT_LOOPS `WatchAll` **per`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:31` — `The loop instance token is the identity plane the agentic substrate keys on: the `AGENT_LOOPS` record, the`
- `docs/adr/053-agent-run-substrate.md:192` — `on `agent.complete`, not a cancel subject — D8), fan to product handlers under a`
- `docs/concepts/03-streams-vs-kv-watches.md:121` — `KV watches have no processing acknowledgment or redelivery. A derived owner must`
- `docs/operations/migration-beta162-to-beta163.md:2027` — `- A terminal whose record update loses its compare-and-swap after `COMPLETE_<loopID>` and its event have landed leaves a`
- `openspec/changes/archive/2026-09-27-agentic-loop-committed-terminal-recovery/design.md:41` — `### OQ1 — W1, lost record CAS: (a) documented bound, or (b)/(c). Recommendation: **(a)**`
- `docs/contributing/07-pattern-adoption.md:27` — `## The shape was already solved`
- semmachina: `internal/resume/pending.go:88` records why `AGENT_LOOPS` `state=running` is not a liveness signal — a sister constraint on any design that widens the record's role
- semspec: `cmd/semspec/watch_live.go:240` attributes the `COMPLETE_` write to execution-manager / requirement-executor — a sister belief about the writer that is already wrong at beta.107
- Active changes: `agentic-loop-commit-disposition-consolidation` is the only non-archived entry in `openspec/changes/` (`ls openspec/changes/ | grep -v archive` → 1)
- #1405 issuecomment-5858896669 — owner ruling 2026-09-27 (transcribed), binding the design phase; the owner's words verbatim: "semspec is out of date anyway. we understand that we are green field and need only note migration requirements for downstreams sisters. we want to maintain feature parity but we do not want deprecated code". For this inventory: sister rows are measurements (writers, readers, sha, pinned semstreams version); the semspec open questions are recorded as migration inputs; no design inference is drawn either way
- `processor/agentic-dispatch/terminal_settlement.go:196` — `// The whole entity is validated, not just the state. TWO production`

Overlaps stated plainly: the `agentic-terminal-events` spec already requires one shared interpretation of the event
for framework consumers (`internal/agentterminal`) — a consolidation of the WRITER side has a reader-side precedent
with a spec home. The `agentic-dispatch` spec pins the `COMPLETE_` activity mapping (`:726`) and the exact-record read
(`:628`), so home (a) and home (d) are contract surfaces of another capability, not private to the loop.

## The consumer at birth (category 4)

No new exported symbol, port, subject, bucket or config field is proposed in this inventory phase; there is nothing
to name a consumer for. The design phase owes this category for every symbol it introduces. Recorded here so the
category is not read as skipped: every existing surface above has its present consumers listed by home.

## The problem shape (category 5)

Independently of the fact: this design's shape is "commit one fact through several non-atomic durable effects, with a
create-once anchor whose refusal on redelivery replays the remaining effects from the saved fact" (the owner, row 1
plus row 2), composed with "admit-or-refuse a delivery against a durable record read" (the cold arms, row 24). Closest
existing instances on other planes, and what each shares:

- `processor/graph-ingest/canonical_mutations.go:280` — `revision, err := c.entityBucket.Create(ctx, entity.ID, encoded)`
- `processor/graph-ingest/canonical_mutations.go:350` — `if errors.Is(err, natsclient.ErrKVRevisionMismatch) {`
- `pkg/lifecycle/manager.go:36` — `// Manager.Transition loop re-reads on ErrKVRevisionMismatch and`
- `processor/agentic-tools/outcomes.go:51` — `_, err := s.bucket.Create(ctx, key, value)`
- `graph/clustering/summary_store.go:207` — `_, writeErr = s.kv.Create(ctx, key, data)`
- `processor/graph-ingest/authority_gate.go:51` — `func (c *Component) authorizeSubject(subject string, importLane bool) error {`
- `natsclient/delivery_settlement.go:17` — `type DeliveryDecision uint8`
- `pkg/graphview/view.go:43` — `WatchAll(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)`

What they share and do not: `graph-ingest` and `pkg/lifecycle` commit ONE effect to ONE store with create-once /
re-read-on-mismatch — the single-store half of the shape, no fan-out to a second store. `agentic-tools/outcomes.go`
and `graph/clustering/summary_store.go` use create-once as an idempotency anchor for one key — the anchor half.
`agentic-dispatch/terminal_settlement.go` sequences a record read and a user-facing publication and classifies its
failures into permanent / transient-before-effect / unknown-publication — the same commit-unknown vocabulary the loop
spells as `errs.WrapFatal`/`WrapTransient` and again as `DeliveryDecision` (three vocabularies for one classification,
on two planes). `authority_gate.go` is the admit-or-refuse instance the contract names: structural check first, one
classified refusal, one metric home. `pkg/graphview` (ADR-081) is the in-tree "derived owner over a KV watch" the
KV-watch contract at `docs/concepts/03-streams-vs-kv-watches.md:121` governs — the shape any option in scope item 2
that derives the event from the record would have to adopt. No in-tree instance was found of the FULL shape
(multi-store fan-out replayed from a create-once anchor) other than the terminal owner itself
(`git grep -n "IsKVConflictError\|ErrKVRevisionMismatch" -- '*.go' | grep -v _test.go | grep -v "^processor/agentic-loop/"`
→ 20 lines, all single-store). If the design names the owner's shape a pattern, it is the establishing instance and
owes the adoption sweep (enumeration only, never migration).

## Same-class collision table

Semantic class: "a loop's terminal outcome as a durable fact, and the disposition of any delivery that meets it after
the loop is no longer held". Every cell cites evidence above or the search that closed it.

| Dimension | Evidence at this base |
|---|---|
| Semantic class | The terminal fact (kind + payload) and the "held → warm / not held → record → decide" authority question; the "inapplicable" disposition vocabulary |
| Owners | `commitTerminal` (`terminal_owner.go:151`, one owner by #1362 ruling); `adoptDurableCancel`/`writeRecordCancelled` (second terminal-record writer, `:391`/`:452`); `terminateOversizedBirth` (terminal outside the owner by ruling, `component.go:1853`); `research-graph-synthesize` (writes the marker key with a foreign payload, `adapters.go:170`); `graph-ingest` (physical writer of the stamp); `agentic-dispatch` (derives `loop_completed` from the key, `http.go:1017`; settles user-facing terminal from the record, `terminal_settlement.go:158`); `internal/agentterminal` (one reader-side interpretation); `agentic/agentrun.MilestoneSubscriber` (`agentrun.go:519`); `frameworkcapabilities/graphresearch` (creates a research-pipeline record at the bare key, `register_tool.go:92`, and provisions the bucket, `:49`; that loop kind's terminal is spelled at `search_result.complete.<loopID>`, `COMPLETE_<loopID>` and `research.search-result.complete`, never on the record) |
| Catalogs | Port declarations `config.go:426`, `:438`, `:444`; dispatch `config.go:122`, `:130`; `graph/kvcatalog.go:9` (application bucket, no framework catalog entry); vocabulary `register.go:435`; `configs/agentic.json:408` and 8 more flow configs declare the bucket; no schema for the marker payload beyond `agentic/events.go` |
| Status | Health latches on `deliveryFatalErr` (`component.go:149`, read at `:530`); `loops_completed_total` / `loops_failed_total{reason}` / `active_loops` counted once at `recordCommittedTerminal` (`terminal_owner.go:225`); three `*_dropped_total` families + the governance family; dispatch activity-view metrics (`processor/agentic-dispatch/metrics.go:211`, `:218`); a graph stamp that fails inside its builder has no metric (`graph_writer.go:297`, `:323`, `:517`) — only the budget expiry counts (`graph_write_publish_timeout_total`) |
| Lifecycle | Bucket policy verified at acquisition: `History: 10, TTL: 24h` (`acquire.go:20`) — the marker and the record both expire at 24 h; the marker is never deleted by the owner; a research-pipeline record is never written terminal and ends only by that TTL (`executor.go:249`; 0 terminal-state writes in `processor/research-graph-*`, `frameworkcapabilities/`, `agentic/research/`); `ENTITY_STATES` history 1 for the stamp; `AGENT` stream retention for the event (`agentic-terminal-events/spec.md:217` "Delivery attempts SHALL be unlimited only within AGENT retention"); per-loop memory released at terminal through one release point (`spec.md:813`, `trajectory_handler_wiring.go:63`, 17 non-test callers by gopls) |
| Ownership | Marker: create-once, adoption by loop ID whatever the kind (#1399); record: compare-and-swap against a per-process observed revision (`component.go:3108`), serialized in-process by `loopRecordMu` (6 sites); no cross-process lease — `loopbucket.AcquireOwner` verifies bucket policy, not exclusivity (`acquire.go:14`); a second holder is detected only at CAS |
| Readers | Production: § Spellings (a)–(d) in-tree lists; sisters: table above (semspec key scan + 11 KV-watch processors; semteams, semsage, semmachina, semdragon event subscribers; semteams/semdev rule packs on the predicate); diagnostic: semspec `pkg/health`; gateway: dispatch `/activity`, semsage `/api/activity`; tests: 18 package test files, agentic + ops + research-graph e2e |
| Writers | Direct: rows 1–4 (owner), 10 (birth), 11 (deferred), 13 (step 0), 16 (carrier); indirect: `graph-ingest` for the stamp; provisioning: `AcquireOwner` creates the bucket when absent (`acquire.go:20`) and `graphresearch.RegisterTool` creates it a second way (`register_tool.go:49`, `:72`); second production writer of the bare record: `graphresearch` `CreateLoopEntity` (`register_tool.go:92`, `executor.go:267`); recovery: `adoptDurableCancel` (`:391`), `createTerminalMarker` adopt (`:285`); tests: `test/e2e/scenarios/agentic/stage_a_process_replacement.go:480` and `ops/scenario.go:419` seed records and markers by direct `PutKV`; e2e writers: `ops/scenario.go:439` (marker and record by `PutKV`), `:456` (stamp by direct triple seed); sister writer: none (research-graph-synthesize and graphresearch are in-tree) |
| Recovery | Redelivery re-enters at row 2 (marker adopt) or a cold arm (rows 1–12 of § Cold arms); W1 converges at the loop's next terminal (`spec.md:1990`, `migration:2027`); W2 converges at the next gate answer; the deferred write Retries (#1400); exhausted retries land in `internal/maxdelivery`; no path terminalizes a research-pipeline record; no reconciler, scan or repair pass reads the marker against the record (`git grep -n "WatchAll\|Watch(" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 0) |

Unknowns the table leaves open are listed under § Open evidence questions.

## Adopter seam inventory (mandatory second deliverable)

Surfaces reached from outside this repository, answered as a developer who has never opened
`terminal_owner.go`. Four surfaces; the adopter for each is named from the sister table.

### Seam 1 — the `COMPLETE_<loopID>` key (semspec `watch_live.go:260`, `pkg/health/orchestrate.go:19`; in-tree `read_loop_result`, dispatch `/activity`)

1. What must they know: the literal prefix (no exported constant — five independent spellings exist, two in semspec);
   that the key holds a terminal EVENT payload discriminated by its `outcome` field, not a `LoopEntity`
   (`terminal_owner.go:90` `decodeTerminalMarker`); that a `COMPLETE_` key may instead hold a research `SearchResult`
   envelope written by `Put` (`adapters.go:170`); that the key expires with the bucket's 24 h TTL; that the marker
   lands BEFORE the event and BEFORE the record turns terminal (`spec.md:1707-1708`), so "marker present, record
   running" is a legal state (W1) with no time bound while `timeout_at` is zero; that the marker is never overwritten
   (create-once), so a different-kind later terminal is adopted, not written. Six facts; more than two is a design
   finding.
2. If they do nothing: semspec's `computeKVActiveLoops` counts a W1 loop as finished while it runs (recorded at
   `migration:2034`, "A watcher keyed on `COMPLETE_<loopID>` counts it as finished while it runs"); `read_loop_result`
   on a research loop decodes a `SearchResult` envelope as a `LoopCompletedEvent` and returns whatever fields happen
   to align; after 24 h a still-live record loses its marker sibling and reappears as "active".
3. Where they find out: doc (`docs/operations/migration-beta162-to-beta163.md:2020-2035`, `doc.go:393`) and nowhere
   for the TTL and the foreign-payload case — no compile, boot or typed runtime signal names the key's contract.
4. What they should have to know: nothing about the write order, the TTL, or a second payload shape under the same
   key. The gap between 1 and 4 is those facts; they are recorded as the gap, not as a mechanism.

### Seam 2 — the `AGENT_LOOPS/<loopID>` record's terminal `state` (semspec's 11 KV-watch processors, `loop_completions.go:249`; semsage `ui-api/sse.go:15`; in-tree dispatch `terminal_settlement.go:158`, `loop_admission.go:361`)

1. What must they know: the record turns terminal LAST, after the marker and the event (`spec.md:1708`); a lost
   compare-and-swap leaves it non-terminal until the loop's next terminal (W1); `state=running` is not a liveness
   signal (semmachina `pending.go:88`); the deferred-turn and step-0 writers move the record's revision without
   changing `state`; the bucket TTL is 24 h; a KV watch has no ack or redelivery, so a derived owner must repair and
   publish readiness itself (`docs/concepts/03-streams-vs-kv-watches.md:121`); the record may be absent for a loop
   whose birth was refused for size (`terminateOversizedBirth`) while its graph stamp exists; two loop kinds write a
   record at the bare key (agentic-loop, research-pipeline) and only the first ever writes it terminal (§ Home (g)).
2. If they do nothing: a watcher keyed on terminal `state` sees a completed loop "slightly later" than the event
   (`migration:2023`) or, under W1, only at the loop's next terminal — semspec's pipeline stage progression waits on
   exactly this signal (`loop_completions.go:16-30`); a watcher that treats `running` as live resumes work on a dead
   holder (the inference semmachina refused). A research-pipeline record is created `executing` (`executor.go:249`) and
   nothing writes it terminal, so every reader keyed on the record's terminal `state` — semspec's 11 watchers and
   `findLoopIDForTask`, semsage `ui-api`, dispatch `/loops` and `/activity` (`loop_admission.go:368`,
   `http_activity.go:329`) — reads it as running until the 24 h TTL removes it; that loop's terminal is spelled at
   `search_result.complete.<loopID>`, `COMPLETE_<loopID>` and a research predicate instead.
3. Where they find out: doc (`migration:2020-2035`, `agentic-dispatch/spec.md:628`) and the semmachina comment; the
   `agentic-dispatch` spec is the only spec-level statement, and it is another capability's.
4. What they should have to know: that the record is terminal when the fact is terminal. The gap between 1 and 4 is
   the ordering itself.

### Seam 3 — the events on `agent.complete.*` / `agent.failed.*` and their three payload types (semteams `chainpause`, `MilestoneSubscriber`; semsage `tools/spawn/executor.go:184`; semmachina `loopfailure.go:31`; semdragon `questbridge/handler.go:447`; in-tree `agentrun`, `otel`, dispatch)

1. What must they know: a cancellation rides `agent.complete` (`agentrun.go:483`; semdragon `handler.go:525`
   discovered it), so the subject does not discriminate the kind — the payload category does; the event may be
   republished on adoption (`terminal_owner.go:129` "A republished terminal is an accepted duplicate"), so a subscriber
   must dedupe (semsage `executor.go` discards duplicates by channel capacity; semteams/semmachina use durable
   consumers); the event precedes the record's terminal state; the stamp precedes the event
   (`terminal_owner.go:338-340`) when it lands, but a stamp whose mutation failed or whose platform identity was
   missing is skipped with a `Warn` and the event still publishes (`graph_writer.go:285`, `:297`), so triples are not
   guaranteed walkable at the event; a
   `MaxPayload` birth refusal publishes no event at all; the wildcard is one token (`agent.failed.*`) and a sister's
   own stream over `agent.failed.>` is refused by NATS for overlap (semmachina `loopfailure.go:100-104`).
2. If they do nothing: a core-NATS subscriber (semsage `executor.go:184`) misses an event published before its
   subscription or during its own restart, with the marker and record as the only recovery — which it does not read;
   a subscriber that keys the kind on the subject treats a cancel as a completion.
3. Where they find out: typed at decode (`internal/agentterminal` fails closed for in-tree consumers; a sister that
   unmarshals directly, as semsage and semdragon do, gets a zero-valued struct — nowhere); doc for the ordering.
4. What they should have to know: nothing about which subject a kind rides on, whether a duplicate is an adoption,
   or whether the stamp landed. The gap is those three facts. Recorded beside it, as a fact about the tree: the
   `agentic-terminal-events` spec requires one interpretation for framework consumers (`:245`) and says "No new
   exported normalized terminal type SHALL be introduced"; the sisters are outside that boundary by Go `internal`
   visibility.

### Seam 4 — the `agent.loop.outcome` predicate (semteams rule packs, 47 mentions; semdev rule packs, 19; in-tree `configs/rules/deep-research`)

1. What must they know: the value space (`success`, `failed`, `cancelled`) is documented, not validated (semdev
   `condition_literals.go:65` pins that fact; semstreams #1057); the stamp is written before the event and inside the
   owner for every terminal except an oversized birth, where it is best-effort; a stamp that exceeds the graph-write budget fails the
   commit (`graph_write_publish_timeout_total`; row 1) — no event, no record — but a stamp whose mutation fails or
   whose platform identity is missing is skipped with a `Warn` and no metric while the event and the record still
   land (`graph_writer.go:285`, `:297`, `:311`, `:323`, `:509`, `:517`), so a rule keyed on the stamp can miss a
   terminal whose event was published.
2. If they do nothing: a rule keyed on `agent.loop.outcome == "failed"` never fires for a tool that refused without
   failing its loop (semdev `05-classifier-fault-note.json:5` records this as "the shipped bug"); a rule that fires
   on `outcome ne ""` fires on cancellations too.
3. Where they find out: doc and a sister conformance test; no vocabulary validator.
4. What they should have to know: nothing about whether the value set is validated or whether the stamp can be
   skipped. The gap is those two facts.

### Prefer observation to prediction

Every seam above asks the adopter to predict an ordering the framework performs: "the marker is there when the event
arrives", "the record will be terminal soon after the event", "a duplicate event means an adoption, not a second
terminal". Under W1 the second prediction is false for an unbounded time. This is recorded as the gap; it is not a
recommendation.

## Measurements — the acceptance test's BEFORE column, at this base

| Measure | Before | Tool / definition |
|---|---|---|
| Durable homes of the terminal fact | 4 (marker, stamp, event, record) + 1 record-only writer class (deferred marker, step 0) racing the terminal write | § Spellings |
| Terminal paths that bypass the owner | 1 (`terminateOversizedBirth`: stamp only) | gopls callers of `commitTerminal` (3) and `handleLoopFailure` (3); grep |
| Loop kinds writing a bare `AGENT_LOOPS/<loopID>` record / kinds whose record ever turns terminal | 2 (agentic-loop, research-pipeline) / 1 | git grep; 0 terminal-state writes under `processor/research-graph-*`, `frameworkcapabilities/`, `agentic/research/` |
| Durable effects on a terminal path that fail with only a log line (no error, no metric) | 4 builders, 8 swallow sites (3 missing-identity returns, 3 `writeBatch` Warns, 2 in `WriteSyntheticDecide`) | sed; `git grep -n "metrics\." -- processor/agentic-loop/graph_writer.go` → 0 |
| Writers of `AGENT_LOOPS/<loopID>` | 6 functions in 2 packages (2 `Create`, 4 CAS `Update`); terminal-record writers 2; bucket provisioners 2 | `git grep "loopsBucket\.\(Create\|Update\)("` → 6 incl. the marker; KV-verb grep over `processor/research-graph-*` and `frameworkcapabilities/graphresearch` → 27 lines, 1 bare-key `Create` |
| Writers of `COMPLETE_<loopID>` | 2 production (owner `Create`; research-graph-synthesize `Put` with a foreign payload) + 1 e2e seed (`ops/scenario.go:439`) | git grep |
| Spellings of the `COMPLETE_` literal | 4 in-tree + 2 in semspec | git grep |
| Spellings of the event port names in the owner package | 4 sites (`terminal_owner.go:322/324`, `handlers.go:2637`, `:3472`, `component.go:3722`) | git grep |
| Sequencing functions (≥2 durable effects, or 1 + disposition) | 16 | § Sequencing table rows 1–16 |
| Lane call sites mapping a sequencing error to a disposition | 7 | rows 17–23 |
| Distinct spelled sequences containing a publish and a KV write | 5 (reducing to 3 relative orders) | table |
| KV-only / publish-only / stamp-only sequences | 3 / 1 / 1 | table |
| Distinct gap policies | 6 | table |
| Cold arms reading the record or marker | 12 in 6 files | gopls `readLoopRecord` (7 non-test callers), `loopsBucket.Get` (3) |
| Miss-only "not held" decisions | 4 in 2 files (1 new file) | grep + reading |
| Re-spellings of the three-way presence switch | 13 non-test refs of `loopPresenceStale` | gopls |
| `ErrLoopNotFound` | 7 decision sites / 3 files; 6 wrap sites / 1 file; grep 21 lines / 5 files | gopls 18 (13 non-test); git grep 21 |
| Adopt functions | 3 + the marker adopt | gopls callers 1 / 3 / 1 / 1 |
| Inapplicable-delivery spellings | 22 (4 identifiers, 2 enum values, 7 consts, 9 literals); 56 non-test uses; 2 reasons spelled twice | gopls + git grep, per table |
| Drop-metric families | 3 `*_dropped_total` + 1 governance family; 6 recorder functions | git grep `NewCounterVec` |
| `errs.WrapFatal(` / `errs.WrapTransient(` | 52 / 6 files; 36 / 5 files | git grep -c |
| `errs.IsFatal(` checks | 6 | git grep |
| `DeliveryDecision` constant returns in the package | 50 (component 27, approval 17, governance 6) | git grep |
| `DeliveryDecision` references repo-wide / in the package | 101 (39 non-test) / 27 (12 non-test) | gopls |
| Handler seams returning the typed decision / a bare error | 3 / 3 (+ 1 converting closure) | grep |
| `settleTerminalGuard` callers; drop-callback shapes | 8; 3 | gopls; grep |
| `releaseLoopTransientState` non-test callers | 17 | gopls (28 total) |
| `loopRecordMu` critical sections | 6 | git grep |
| Held-loop reads (`MessageHandler.GetLoop` non-test refs; direct `loopManager.GetLoop`) | 14; 7 | gopls; git grep |
| Test hooks | 4 fields, 5 set sites | grep |
| Lines: files involved (12: component, handlers, state, terminal_owner, loop_evidence, loop_classification, loop_presence, approval_response_handler, approval_sweeper, governance_dispatcher, metrics, graph_writer) | 14,140 | `wc -l` |
| Lines: `processor/agentic-loop` non-test / files | 18,272 / 31 | `wc -l` |
| `terminal_owner.go` / `loop_presence.go` / `loop_evidence.go` | 539 / 72 / 653 | `wc -l` |
| Test files in package / touching the terminal surface | 124 / 18 | `ls`, git grep -l |
| Sisters reading a home / carrying only a generated type / with zero hits | 6 (semspec, semteams, semsage, semmachina, semdragon, semdev) / 1 (semstreams-ui) / 9 (semsource, semconnect, semops, semembed, semboids, semlink, semmem, seminstruct, semdocs); semsummarize is not a git repository | sister table |
| Sister KV-watch derived owners of the record's terminal state | 11 semspec processors + semsage `ui-api` | sister grep |

## Census mismatches re-measured

| Item | Census `078782b1` | Explorer `3f2d4617` | This pass `913b74f0` | Settled figure and why |
|---|---|---|---|---|
| Cold arms | 15 in 8 files | 13 in 7 | 12 record/marker-reading in 6 files + 4 miss-only → 16 in 7 files | The census list is not recoverable (figures only). The 12 + 4 enumeration above is pinned per site with its miss signal and read path; the explorer omitted `createTerminalMarker`'s read-back and the `writeLoopRecord`/entry-check miss decisions and counted `lookupWaiter` (which reads nothing) as an arm |
| `ErrLoopNotFound` | 21 sites in 5 files | 18 (gopls) vs 21 (grep) | same | 7 decision sites in 3 files is the code fact; 21/5 is a prose-inclusive grep (7 comments + declaration + 6 wraps + 7 decisions), which is evidence the census was grep-based |
| Inapplicable tokens / uses | 7 / 42 | 7 / 45 (its own table sums to 43) | 22 spellings / 56 uses (widest); 7 groups under the census grouping | The census and explorer groupings drop 5 bare literals, `errTerminalOwnedElsewhere` and the two `*Applied` enum values; the widest definition is given with sub-counts so any narrower one can be recomputed |
| `settleTerminalGuard` callers | 9 | 8 | 8 by gopls and 8 call lines by grep | 8; the ninth is the declaration (`grep -c "settleTerminalGuard("` → 9 including it) |
| `WrapFatal` | 52 | 52 (per-file summary 21/13/12 mis-stated) | 52 (3/19/2/1/14/13) | 52; the explorer's pins already sum to the measured split |
| `IsFatal` checks | — | 8 | 6 | 6 lines call `errs.IsFatal(`; the explorer's list included the Quarantine returns those guard |
| Adopt functions | 3 + marker | same | same | agreed |
| Drop-metric families | 3 | 3 (+ governance noted) | 3 + 1 | 3 named drop families; the governance family carries the verdict lane's inapplicable reasons under a non-drop name |
| Test hooks | 4 | 4 | 4 | agreed |
| Publish + KV-write sites / orders / gap policies | 18 / 5 / 5 | 15 pinned, not re-asserted | 16 functions + 7 lane sites / 5 spelled sequences (3 relative orders) / 6 | Definitions differ; the census's are not recoverable. The 5 spelled sequences match the census's "5 distinct orders" if `MG→S→P→R` and `RC→P` count as distinct from `M→S→P→R` and `R→P`; the census's 5 policies omit Ack-without-effect and Terminate and split Fatal from Quarantine |

## Searches

- `git rev-parse HEAD` → `913b74f07b78340a4fbff3eff56eb9ab6355050a`; `git diff --stat 3f2d4617..913b74f0 -- processor natsclient internal pkg agentic vocabulary configs docs openspec/specs` → empty; `git log --oneline 3f2d4617..913b74f0` → 4 docs commits
- `gopls version` → v0.20.0
- `gopls references processor/agentic-loop/state.go:59:2` (`ErrLoopNotFound`) → 18 (13 non-test: 7 decisions, 6 wraps; 5 test)
- `git grep -n "ErrLoopNotFound" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 21 in 5 files; `| grep -v "^processor/agentic-loop/"` over `'*.go'` → 0
- `gopls references processor/agentic-loop/terminal_owner.go:505:21` (`settleTerminalGuard`) → 8, all non-test; `git grep -n "settleTerminalGuard" -- 'processor/agentic-loop/*.go'` → 13 lines (8 calls, 1 decl, 1 own error string, 3 comments)
- `git grep -c 'errs.WrapFatal(' -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 3/19/2/1/14/13 = 52; `errs.WrapTransient(` → 15/2/10/5/4 = 36
- `git grep -n "errs.IsFatal(" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 6
- `git grep -nP "^\s+test[A-Za-z]+\s+func\(" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 4 (`-E` with the same pattern → 0; `git grep -n "^\s*test[A-Za-z]*Hook\s" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 4)
- `gopls references processor/agentic-loop/loop_presence.go:28:2` (`loopPresenceStale`) → 15 (13 non-test); `handlers.go:123:2` (`staleDrop`) → 8 (3 non-test); `handlers.go:109:2` (`terminalOwnedElsewhere`) → 8 (6 non-test)
- `git grep -n "errTerminalOwnedElsewhere" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 6 (3 uses, 1 decl, 2 comments)
- `git grep -n "requestOrderApplied\|taskApplied\b\|requestOrderForeign" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 19 (5 `requestOrderApplied` uses, 4 `taskApplied` uses, 6 `requestOrderForeign` uses, 4 decls/returns in `loop_classification.go`)
- `git grep -n "verdictDrop" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 16 (7 decls, 7 uses, 2 comments)
- `git grep -n '"stale_request_id"\|"stale_execution"\|"stale_loop_id"\|"already_terminal"\|"terminal_unproven"\|"approval_inapplicable"\|"superseded_request"\|"older_request"\|"already_applied"' -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 15 call-site uses (plus comment lines in `metrics.go`, `doc.go`, `terminal_owner.go`)
- `git grep -n "prometheus.NewCounterVec\|prometheus.NewCounter(" -- processor/agentic-loop/metrics.go` → 20; `Name:.*\(dropped\|subscribe_before\)` → 4
- `gopls references processor/agentic-loop/loop_evidence.go:282:21` (`readLoopRecord`) → 10 (7 non-test); `loop_presence.go:70:21` (`classifyMissingLoop`) → 4 (1 non-test); `loop_evidence.go:425:21` (`adoptNewerRetainedRequest`) → 16 (3 non-test); `loop_evidence.go:340:21` (`adoptRetainedRequest`) → 4 (1 non-test); `terminal_owner.go:391:21` (`adoptDurableCancel`) → 1; `terminal_owner.go:265:21` (`createTerminalMarker`) → 3 (1 non-test); `terminal_owner.go:84:6` (`terminalMarkerKey`) → 9 (2 non-test)
- `gopls references processor/agentic-loop/component.go:2371:21` (`persistHandlerResult`) → 18 (7 non-test); `terminal_owner.go:151:21` (`commitTerminal`) → 3; `component.go:2241:21` (`handleLoopFailure`) → 3 non-test; `component.go:3013:21` (`publishResults`) → 8 (6 non-test); `component.go:3270:21` (`writeLoopRecord`) → 3; `trajectory_handler_wiring.go:116:21` (`recordTerminalObservation`) → 3; `trajectory_handler_wiring.go:63:21` (`releaseLoopTransientState`) → 28 (17 non-test); `state.go:2012:23` (`settleTerminal`) → 2 (1 non-test)
- `gopls references processor/agentic-loop/handlers.go:3484:26` (`MessageHandler.GetLoop`) → 81 (14 non-test); `git grep -n "loopManager.GetLoop(" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 7
- `gopls references natsclient/delivery_settlement.go:17:6` (`DeliveryDecision`) → 101 (natsclient 40, agentic-loop 27, deliverylane 10, agentic-tools 7, agentic-dispatch 5, agentrun 5, agentic-governance 4, agentic-model 3)
- `git grep -c "natsclient.DeliveryDecision\(Ack\|Retry\|Terminate\|Quarantine\)" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 17/27/6 = 50
- `git grep -n "^func (c \*Component) handle[A-Za-z]*Message(" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 6 (3 typed, 3 bare)
- `git grep -n "loopsBucket" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 20 (2 `Create`, 4 `Update`, 3 `Get`, nil guards, decl, acquisition)
- `git grep -n "loopRecordMu.Lock()" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 6
- `git grep -n "publishResults(" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 7 (6 calls + decl); `\.Publish(\|\.PublishMsg(\|publisher\.` → 1 (`governance_dispatcher.go:736`, proposed-call publish, no KV pair)
- `git grep -n "COMPLETE_" -- '*.go' | grep -v _test.go` → 57 (44 outside `test/`; 3 production write sites + 1 e2e seed `PutKV`); `git grep -c "COMPLETE_" -- . ':!*.go'` → top files `openspec/specs/agentic-loop/spec.md` 20, archive design 17, migration 16
- `git grep -c '"agent.cancelled"' -- 'processor/agentic-loop/*.go'` → 0
- `git grep -n "AGENT_LOOPS" -- '*.go' | grep -v _test.go | grep -v "^processor/agentic-loop/"` → readers in `agentic-dispatch`, `agentic-tools`, `research-graph-*`, `frameworkcapabilities/graphresearch`, `internal/looptoken`, `graph/kvcatalog.go`
- `git grep -n "AGENT_LOOPS" -- 'configs/'` → 11 (all component `"bucket"`/`loops_bucket` declarations); `git grep -n '"bucket"' -- 'configs/rules/'` → `RESEARCH_EVIDENCE`, `ENTITY_STATES` only (no rule watches `AGENT_LOOPS`)
- `git grep -n 'agent\.complete\|agent\.failed' -- '*.go' | grep -v _test.go | grep -v "^processor/agentic-loop/"` → `agentic/agentrun`, `agentic/doc.go`, `internal/boot/run.go`, `output/otel`, `processor/agentic-dispatch`, `service/milestone_service.go`
- `gopls references vocabulary/agentic/predicates.go:398:2` (`LoopOutcome`) → 19 (4 non-test, 0 outside agentic-loop/vocabulary); `git grep -n "agent\.loop\.outcome" -- . ':!*.go'` → 6 rule configs + docs
- `git grep -n "LoopCompletedEvent\|LoopFailedEvent\|LoopCancelledEvent" -- '*.go' | grep -v _test.go | grep -v "^processor/agentic-loop/\|^agentic/"` → `internal/agentterminal`, `processor/agentic-dispatch`, `processor/agentic-tools`, e2e
- `git grep -n "IsKVConflictError\|ErrKVRevisionMismatch" -- '*.go' | grep -v _test.go | grep -v "^processor/agentic-loop/"` → 20, all single-store (`natsclient/kv.go`, `graph-ingest`, `pkg/lifecycle`, `persona`, `graph/clustering`)
- `git grep -n "\.Create(ctx, " -- '*.go' | grep -v _test.go | grep -v "^processor/agentic-loop/"` → 30 (create-once anchors on other planes: `agentic-tools/outcomes.go:51`, `graph/clustering/summary_store.go:207`, `graph-ingest/canonical_mutations.go:280`, `config/manager.go:1078`; `graphresearch/register_tool.go:92` is NOT another plane — it is the second production writer of this very record, § Home (e))
- `git grep -n "WatchAll\|Watch(" -- 'processor/agentic-loop/*.go' | grep -v _test.go` → 0 (the owner package runs no watch; no reconciler)
- `git grep -l "COMPLETE_\|commitTerminal\|settleTerminalGuard\|adoptDurableCancel\|createTerminalMarker\|terminalMarkerKey" -- 'processor/agentic-loop/*_test.go' | wc -l` → 18; `ls processor/agentic-loop/*_test.go | wc -l` → 124
- `git grep -n "ErrKVRevisionMismatch\|lost the record race\|record moved past" -- 'test/e2e/'` → 0; `git grep -ln "converges at the loop's next terminal\|next terminal of any kind\|lostCAS\|LostCAS\|lost_cas" -- 'processor/agentic-loop/*_test.go'` → `lost_terminal_record_integration_test.go`
- `ls processor/agentic-loop/*.go | grep -v _test.go | xargs wc -l` → 31 files, 18,272 lines
- `grep -n "^### Requirement" openspec/specs/agentic-loop/spec.md | grep -i "terminal\|record\|adopt\|cold\|held"` → `:813`, `:1241`, `:1684` (+ `:237`, `:446`, `:481`, `:642` trajectory/decide); `grep -n "^### Requirement" openspec/specs/agentic-terminal-events/spec.md` → 13; `grep -n "COMPLETE_\|loop_completed\|AGENT_LOOPS" openspec/specs/agentic-dispatch/spec.md` → 11
- `git grep -l "COMPLETE_\|AGENT_LOOPS\|agent\.complete\|terminal owner\|commitTerminal\|LoopCompletedEvent" -- 'docs/adr/*.md'` → 15 ADRs (028, 032, 045, 049, 051, 053, 054, 055, 056, 073, 080, 081, 098, 101, 105)
- `ls openspec/changes/ | grep -v archive` → 1 (this change)
- `gh issue list --state open --limit 200 --json number,title,milestone` filtered on terminal/adopt/marker/latch/agentic-loop → #1405, #1398, #1356, #1270, #1265, #1244, #1242, #1146, #1105, #1035, #1033, #1005, #935, #753; `gh issue view` → #1362/#1377/#1399/#1400 CLOSED, #1146/#1405/#1314/#1283 OPEN
- Sisters: `git -C <sister> fetch -q origin` → 6/6 clean for semspec, semteams, semsage, semsource, semmachina, semconnect (semdev, semdragon, semops, semembed read at their existing `origin/main`); `git -C <sister> rev-parse origin/main` → shas in the table; `git -C <sister> show origin/main:go.mod | grep semstreams` → versions in the table; per-pattern `git -C <sister> grep -n <pattern> origin/main -- '*.go'` non-test/test and `':!*.go'` counts recorded in the table's summary (semspec `AGENT_LOOPS` 131 non-test Go; semteams `agent.failed` 17; semsage `AGENT_LOOPS` 21; semmachina `agent.failed` 4; semdragon `agent.complete` 14; semdev `agent.loop.outcome` 4; semsource/semconnect/semops/semembed 0 Go)
- `git -C semspec grep -n "^func (c \*Component) watchLoopCompletions" origin/main -- '*.go'` → 11; `grep -n 'IsTerminal()' origin/main -- <4 of them>` → keyed on `loop.State.IsTerminal()`
- `git -C <sister> grep -n "loop_completed" origin/main -- '*.go' | grep -v _test.go` → 0 for all ten (no sister Go code consumes the `/activity` event type; semteams consumes it from its UI per `docs/ui-integration-notes.md:29`)
- `ls internal/loopbucket/` → does not exist; `git grep -n "^func AcquireOwner" -- '*.go'` → `processor/agentic-loop/internal/loopbucket/acquire.go:14`
- `openspec/project.md` § Purpose and § Product Boundary read (contract step 1); `docs/contributing/07-pattern-adoption.md` headings read
- `git grep -nE '\.(Put|Create|Update|Get|Watch|WatchAll|Keys|ListKeys|Delete|Purge)\(ctx' -- 'processor/research-graph-*/*.go' 'frameworkcapabilities/graphresearch/*.go' ':!*_test.go'` → 27 lines: 1 bare-key `Create` (`register_tool.go:92`), 1 trigger `Put` (`:101`), 8 stage-key `Put`s, 12 stage-key `Get`s, the `COMPLETE_` `Put` (`adapters.go:170`), the `search_result.complete` `Put` (`:152`), 2 graph `Create`s in `llmwrap`
- `git grep -nE 'LoopStateComplete|LoopStateFailed|LoopStateCancelled|IsTerminal' -- 'processor/research-graph-*' 'frameworkcapabilities/' 'agentic/research/' ':!*_test.go'` → 0 (no research code writes or reads a terminal record state)
- `git grep -n 'PipelineRole' -- '*.go' | grep -v _test.go` → 5 (the constant, its doc, the kickoff triple, the constructor call, the e2e scenario)
- `git grep -n "metrics\." -- processor/agentic-loop/graph_writer.go` → 0; `git grep -n "Warn(" -- processor/agentic-loop/graph_writer.go` → 14 (8 of them on the four terminal/synthetic stamp paths)
- `git grep -n 'agent\.loop\.outcome' -- configs/` → 6 rule files (deep-research 01, 02, 05, 06, 07; example-fan-out 02)
- `git grep -n 'awaitLoopState(' -- test/e2e` → 4 lines (3 calls at `approval_signal.go:198`, `:599`, `:643` + the declaration at `:880`)
- `gopls references natsclient/delivery_settlement.go:17:6 | grep -v _test.go` → 39 (agentic-loop 12, natsclient 8, agentic-dispatch 5, agentic-governance 4, agentrun 4, agentic-tools 3, agentic-model 2, deliverylane 1)
- Sisters, second pass: `ls -d /Users/coby/Code/c360/*` → semstreams-ui, semboids, semlink, semmem, seminstruct, semdocs, semsummarize also present; `git -C <s> grep -c -E 'COMPLETE_|AGENT_LOOPS|agent\.complete|agent\.failed|agent\.loop\.outcome|loop_completed|/activity|LoopCompletedEvent|LoopFailedEvent|LoopCancelledEvent' origin/main` summed → semstreams-ui 9 (2 generated-type lines + 7 unrelated `EventSource` test-mock lines), semboids 0, semlink 0, semmem 0, seminstruct 0, semdocs 0; semsummarize is not a git repository (`git rev-parse --git-dir` fails)
- `git -C semspec show origin/main:processor/execution-manager/req_watcher.go | sed -n '190,215p'` → `findLoopIDForTask` `Keys`+`Get` scan; `git -C semspec show origin/main:pkg/health/capture.go | sed -n '73,77p'` → `DefaultKVBuckets` includes `AGENT_LOOPS`
- `gh api repos/C360Studio/semstreams/issues/comments/5858896669 -q .body` → the owner ruling quoted verbatim under § Adjacent claims
- NOT RUN: no test suite, no Docker, no e2e (a sister session shares the host); no `git` command that mutates any tree; no sister repository was modified

## Open evidence questions

1. The census definitions behind "18 sites / 5 orders / 5 gap policies", "15 cold arms in 8 files" and "42 uses" are
   not recoverable from #1146 issuecomment-5854803094; the definitions used here are stated per section. The
   inventory reviewer should confirm or replace them before the design's before → after numbers are read against the
   census.
2. `read_loop_result` (`agentic-tools/loop_result.go:132`) and dispatch's `loopFromCompletion`
   (`loop_wire.go:129`) decode a `COMPLETE_` value as a terminal event, while `research-graph-synthesize` writes a
   `SearchResult` envelope under the same key (`adapters.go:170`); whether those readers fail closed or partially
   decode was not executed (no Docker on this pass) — it is a code-path observation only.
3. Migration input (owner ruling #1405 issuecomment-5858896669: sisters get migration notes only): semdragon pins
   `v1.0.0-beta.21` and semsage `v1.0.0-alpha.3`; both `origin/main` trees still subscribe to `agent.complete.*`.
   Whether a migration note is owed to either is the owner's; no design inference is drawn.
4. Migration input (same ruling; the owner's words: "semspec is out of date anyway"): semspec's 11
   `watchLoopCompletions` processors and `findLoopIDForTask` key on the record's terminal `state`, and its
   `watch_live.go:240` names the wrong writer of `COMPLETE_`. Recorded for the migration table; not a design premise.
5. No e2e tier asserts a lost-CAS convergence (`git grep` → 0 in `test/e2e/`); the only test of W1 is a
   package-level integration test. The agentic tier does assert the record's terminal state for a cancel
   (`approval_signal.go:643`), so record-state e2e evidence exists; lost-CAS evidence does not. Any design that touches the owner's order will need to name the tier that proves
   it, per `docs/contributing/02-e2e-tests.md` § Breaking Changes.
6. `terminateOversizedBirth` leaves `active_loops` incremented (its own doc names it a #1242-class residual) and
   leaves the fact in home (b) only; whether that path is inside this change's "every terminal is one function"
   acceptance clause is for the design and the owner.
7. The bucket TTL of 24 h (`acquire.go:20`) applies to the marker and the record alike; no reader inventoried here
   accounts for a marker expiring under a still-live record. Whether that is reachable in practice (a loop older than
   24 h) was not measured.
8. Migration input (same ruling): `configs/semspec.json:458` subscribes to `agentic.loop_completed.v1` on the `AGENT`
   stream — a subject this tree never publishes; not traced further (sister, read-only).
9. A research-pipeline record (`executor.go:249`) is never written terminal; every record-state reader in-tree
   (dispatch `/loops` selection at `http_activity.go:329`, `/activity`, `loop_admission.go:368`) and in the sisters
   treats it as running until the TTL. Its terminal lives at `search_result.complete.<loopID>`, `COMPLETE_<loopID>` and
   the research predicate (§ Home (g)). Recorded as measured; for the sisters it is a migration input.
10. The three terminal stamp builders and `WriteSyntheticDecide` swallow a mutation failure or a missing platform
    identity with a `Warn` (8 sites, no error, no metric), so a terminal can publish its event and write its record
    with no `agent.loop.outcome` stamp. Measured from the builders' bodies, not executed. It is evidence for scope
    item 2 (whether home (b) can be dropped or derived); no inference is drawn here.

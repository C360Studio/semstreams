# Retained-reuse prerequisite refresh after live matching

base: c347eff487f50b93bc338d764f43ef5b5ea5e133

This supplements accepted inventory SHA
`8b73908fd59f17708f9c5602fd9c4be0f4bb532f4c30bab792deade735c09a1c`.
Its unaffected surface/adopter/collision evidence remains authoritative.
Live-match handoff `fc4b2d4c…` passed conformance review.
Current governance_dispatcher.go SHA:
`85e35f619380fc23cbbe2411b130d9f5ee2d90c5b2a57828fb9da3c7729cef24`.

Root reports live native controls and loop race green. Final implementation approval is recorded separately in
`review-r7-live-proposal-match-2026-09-15.md`. This architect ran no tests and changed no files; root materialized
the returned inventory. This supplement is pending narrow inventory/conformance review.

Current proposal/design hashes remain those fully read for the accepted inventory.
The complete current tasks were reread; architect intake SHA:
`3cf9eb0e38559b2b6e7b93b64eebed4455594dcc97c4c72c180ae5e1c3642190`.
Root subsequently reconciled task status; that intake identity is not a current task hash.
Applicable current governance/replay-admission clauses and changed live source were read.
Historical artifacts and unaffected inventories were not recut.

## Changed live owner

- `processor/agentic-loop/governance_dispatcher.go:206` — `func matchVerdictProposal(verdict VerdictPayload, proposal ProposedToolCallPayload) error {`
- `processor/agentic-loop/governance_dispatcher.go:217` — `if verdict.CallID != "" && verdict.CallID != proposal.CallID {`
- `processor/agentic-loop/governance_dispatcher.go:407` — `proposal ProposedToolCallPayload`
- `processor/agentic-loop/governance_dispatcher.go:408` — `arrivals chan verdictArrival`
- `processor/agentic-loop/governance_dispatcher.go:418` — `waiters map[string]verdictWaiter`
- `processor/agentic-loop/governance_dispatcher.go:464` — `proposal, err := prepareProposedToolCall(loopID, parentLoopID, call)`
- `processor/agentic-loop/governance_dispatcher.go:469` — `waiters[call.ExecutionID] = d.registerWaiter(proposal)`
- `processor/agentic-loop/governance_dispatcher.go:480` — `if err := publishPreparedProposed(ctx, d.publisher, waiters[call.ExecutionID].proposal, d.logger); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:575` — `if err := matchVerdictProposal(payload, waiter.proposal); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:612` — `func prepareProposedToolCall(loopID, parentLoopID string, call agentic.ToolCall) (ProposedToolCallPayload, error) {`

The full prepared proposal is now the live expectation and publication input.
The matcher remains private and keeps CallID optional.
Current Propose call hierarchy contains preparation, registration, publication and waiting, but no retained read.
matchVerdictProposal has one production caller, HandleVerdict575.
The existing exact reader still has only request/response callers; no verdict operation appeared.

## Existing error-propagation dependency remains

- `processor/agentic-loop/governance_dispatcher.go:481` — `publishFailures[call.ExecutionID] = err`
- `processor/agentic-loop/governance_dispatcher.go:496` — `Reason: fmt.Sprintf("governance publish failed: %v", pubErr),`
- `processor/agentic-loop/governance_dispatcher.go:520` — `return DispatcherResult{Approved: approved, Rejected: rejected}, nil`
- `processor/agentic-loop/governance_dispatcher.go:552` — `return "timeout", fmt.Sprintf("governance wait cancelled: %v", ctx.Err())`
- `processor/agentic-loop/handlers.go:1407` — `govResult, gErr := h.governanceDispatcher.Propose(ctx, loopID, parentLoopID, toolCalls)`
- `processor/agentic-loop/handlers.go:1413` — `return gErr`
- `openspec/changes/agentic-loop-restart-safety/proposal.md:134` — `- Error propagation does not land separately from the durable authority that makes redelivery safe. Any additional`

The handler already propagates an actual Propose error. Publication failure and wait cancellation currently
do not reach that route: they become rejection results. Changing those outcomes alone would omit the
accepted prerequisite for safe redelivery.

## Existing observation APIs and effective local policy

- `component/port_facts.go:71` — `func (f PortFacts) Stream() (StreamFacts, bool) {`
- `component/port_facts.go:235` — `streamName:        port.StreamName,`
- `component/port_facts.go:245` — `maxDeliver:        port.MaxDeliver,`
- `component/port_facts.go:246` — `ackWait:           port.AckWait,`
- `natsclient/client.go:1238` — `func (m *Client) GetStream(ctx context.Context, name string) (jetstream.Stream, error) {`
- `natsclient/client.go:1254` — `stream, err := js.Stream(ctx, name)`
- `processor/agentic-loop/settlement_recovery.go:56` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-loop/settlement_recovery.go:810` — `info, err := stream.Info(ctx)`
- `processor/agentic-loop/component.go:987` — `consumerCfg, componentMaxAckPending, consumerErr := agenticLoopConsumerPolicy(port)`
- `processor/agentic-loop/component.go:1019` — `ackWait = c.config.Consumer.ParsedAckWait()`
- `processor/agentic-loop/component.go:1021` — `maxDeliver = c.config.Consumer.MaxDeliver`
- `processor/agentic-loop/component.go:1023` — `backOff = []time.Duration{30 * time.Second, 2 * time.Minute}`
- `processor/agentic-loop/component.go:1027` — `ackWait = 30 * time.Second`
- `processor/agentic-loop/component.go:1029` — `maxDeliver = consumerCfg.MaxDeliver`
- `processor/agentic-loop/component.go:1040` — `MaxDeliver:     maxDeliver,`
- `processor/agentic-loop/component.go:1041` — `AckWait:        ackWait,`
- `processor/agentic-loop/component.go:1043` — `BackOff:        backOff,`

PortFacts identify the declared local stream; they are not observed server policy.
Likewise raw port consumer fields are not always the effective loop consumer policy:
long-running and fast lanes have different existing resolution paths.
GetStream plus Stream.Info supplies the existing observation path without a new public lookup API.
The pinned Stream.Info caller belongs to human approval; it is evidence of API availability,
not governance admission and not permission to copy that helper's absence policy.

## Already-approved admission owner, currently absent implementation

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:797` — `after resolving its own PortFacts and before its own first dependent`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:799` — `AckWait, BackOff, MaxDeliver, maximum work/replay need, and PubAck dependency. No owner SHALL read another config,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:803` — `Admission SHALL require observed DiscardNew, sufficient MaxAge, and no earlier message bound. Refusal SHALL be typed`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:805` — `allocate or positively settle nothing. It SHALL mutate no stream and persist no state. Approval lifetime is excluded`
- `processor/agentic-loop/component.go:524` — `if err := c.initializeKVBucketsForStart(runCtx); err != nil {`
- `processor/agentic-loop/component.go:527` — `if err := c.restoreApprovalDeadlines(runCtx); err != nil {`
- `processor/agentic-loop/component.go:532` — `if err := c.setupSubscriptions(runCtx, runCtx); err != nil {`

Both structural searches for ObserveAndValidate/agentstreamadmission returned zero.
The exact tracked-Go search for their names and refusal code also returned zero.
These observations confirm an existing R8 sequencing dependency, not an unowned new gate.

The local requirement inputs are specified, but this refresh has not established executable horizon
arithmetic/safety-margin lowering. It does not substitute governance's 1s wait, a global maximum,
another component's config, or approval lifetime for that requirement.

## Collision/adopter delta

The accepted inventory's owners remain unchanged. The newly explicit distinction is:

| Job | Existing home | What it does not establish |
| --- | --- | --- |
| Declared stream identity | PortFacts/StreamFacts | Actual retention |
| Effective loop consumer settings | setupConsumer and agenticLoopConsumerPolicy | Replay admission |
| Current server configuration | GetStream and Stream.Info | The caller's required horizon |
| Exact retained bytes | Existing readExact | Permission after absence |
| Live proposal agreement | prepareProposedToolCall and matchVerdictProposal | Retained lookup/reuse |

No new outward API, knob, authoring field or adopter migration is proposed.
The already-approved R8 behavior is an observed startup refusal naming actual/required values;
the operator is not asked to predict a framework-owned horizon.
No alternate retention gate, readiness surface or state authority is identified here.

## Verification/search record

1. `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r7-retained-verdict-refresh-2026-09-15.md`
   returned `pins=183 ok=124 moved=40 ambiguous=5 drift=14 malformed=0 unparsed=0`.
   Changes are live source/fixtures and task-line movement; historical inventory remains untouched.
2. gopls environment:
   `GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache GOCACHE=/private/tmp/semstreams-r7-test-cache
   GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`.
3. `gopls workspace_symbol -matcher=fuzzy ObserveAndValidate` — zero.
4. `gopls workspace_symbol -matcher=fuzzy agentstreamadmission` — zero.
5. `gopls call_hierarchy processor/agentic-loop/governance_dispatcher.go:447:29`
   — 19 caller functions, one production caller; no retained-read callee.
   An earlier :444:29 call failed because that coordinate was not the declaration.
6. `gopls references processor/agentic-loop/governance_dispatcher.go:206:6` — HandleVerdict575 only.
   An earlier :204:6 call addressed its comment and returned no identifier.
7. `gopls references processor/agentic-loop/settlement_recovery.go:49:46` — request40/response46 only.
8. `gopls references processor/agentic-loop/settlement_recovery.go:335:21` — component1530 only.
9. `gopls workspace_symbol -matcher=fuzzy agenticLoopConsumerPolicy` — component1122 plus tests.
10. `gopls workspace_symbol -matcher=fuzzy GetStream` — located Client.GetStream1238;
    fuzzy output was truncated and is used only as a locator, never an absence/count claim.
11. `gopls workspace_symbol -matcher=fuzzy PortFacts` — located component/port_facts.go.
12. `git grep -n -E 'ObserveAndValidate|agent_stream_replay_inadmissible|agentstreamadmission' -- '*.go'`
    — zero tracked matches; gopls provides the complementary current-package check.
13. `git grep -n -E 'agentstreamadmission|retained.verdict|retained verdict|propos.*fingerprint|waiter.loss' -- openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md openspec/changes/agentic-loop-restart-safety/specs/rule-agent-publishing/spec.md`
    — located accepted R7/R8 clauses.
14. `git grep -n -E 'safety margin|maximum.*replay|replay.*horizon|MaxDeliver|AckWait' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md`
    — located local admission inputs and separate approval/heartbeat clauses.
15. `git grep -n -E 'func .*loopSettlementDecision|IsFatal|IsInvalid' -- processor/agentic-loop/delivery_settlement.go processor/agentic-loop/delivery_owner.go`
    — zero in those pathspecs; no classifier-absence claim.
16. `shasum -a 256` and numbered reads of the named changed/relevant files; no test or mutation commands.

Additional measured hashes:

| File | SHA-256 |
| --- | --- |
| component/port_facts.go | af2aeaac403e5b056c3a6da5f884818cdda67ff05bd67f5f1e3cd622ff937fd3 |
| natsclient/client.go | 9f07a68b0ad8785fc68d025ab9c35335573bd996e8eb1361d48ca957960ac776 |
| specs/agentic-loop/spec.md, within this change | 5c9991222962e8ab60a10cd8745c082cf54f6924475308cc835a2585c14395dc |

Component, handlers, settlement_recovery and governance-spec hashes match the accepted inventory.

## Remaining boundary

The live preparation/match homes are ready for reuse. The existing exact reader still supplies bytes,
not a retained-verdict interpretation. An observed absent message cannot bypass the open R8 admission
contract. Source-error propagation cannot be landed as an independent fix.

No new owner-policy decision was identified. This supplement stops for narrow inventory/conformance review;
it does not authorize a new store, public lookup surface, separate gate or changed absence policy.
#1311/#1312 source settlement and frozen #1156 remain unchanged.

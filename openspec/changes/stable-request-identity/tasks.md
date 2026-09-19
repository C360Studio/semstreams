# Tasks: stable request, call and loop identity

## 1. Land the reviewed checkpoints

- [x] 1.1 Cherry-pick `fd0277c2` — dispatch recovers task identity on redelivery
      (`processor/agentic-dispatch/task_recovery.go`, `http.go`, `component.go`, `agentic/user_types.go`)
- [x] 1.2 Cherry-pick `3d6cab9f` — stable tool execution identity: ExecutionID, CallOrdinal, RequestID on every
      `ToolCall` and `ToolResult` (`processor/agentic-loop/execution_identity.go`, `processor/agentic-tools/outcomes.go`,
      `processor/rule/actions.go`, `schemas/agentic-loop.v1.json`)
- [x] 1.3 Cherry-pick `78d54986` — cancel publishes through synchronous JetStream and requires PubAck before its
      source settles
- [x] 1.4 Cherry-pick `af829616` — agentic-model settles provider work from retained responses; `handleRequest`
      returns a classified `DeliveryDecision`
- [x] 1.5 Drop every `openspec/changes/agentic-loop-restart-safety/` hunk from the picks; the Codex change's
      evidence files stay on the closed branch

## 2. Deterministic RequestID (owner ruling Q4, #1330)

- [x] 2.1 `LoopManager.GenerateRequestID` mints `<loopID>:req:<iteration>:<retry>` from state the manager holds
      (`processor/agentic-loop/state.go`)
- [x] 2.2 Test: the same logical request minted twice is byte-identical; a later iteration is not
      (`TestRequestIDIsDeterministicPerIteration`)
- [x] 2.3 Test: a truncation retry of iteration N is `:N:1`, and forward progress clears the ordinal
      (`TestRequestIDCarriesTheTruncationRetryOrdinal`)
- [x] 2.4 Test: the loop prefix, `ExtractLoopIDFromRequest`, and `agent.response.<requestID>` resolution are
      unchanged (`TestRequestIDKeepsTheLoopPrefixAndSubjectGrammar`)
- [x] 2.5 Mutation evidence: uuid-suffix revert kills 2.2 and 2.3; `Iterations` without the `+1` kills 2.2 and 2.3;
      a constant retry ordinal kills 2.3. Baselines restored by checksum
- [x] 2.6 Migration note records the suffix change (`docs/operations/migration-beta162-to-beta163.md`)

## 3. `Nats-Msg-Id` on every request publish (owner ruling Q5, #1330)

- [x] 3.1 `PublishedMessage` carries `MsgID`; `publishResults` routes through `PublishToStreamWithMsgID`
      (`processor/agentic-loop/handlers.go`, `component.go`)
- [x] 3.2 All three `agent.request` publish sites stamp their RequestID (`handlers.go` task birth, truncation
      retry, tools complete) — the same three sites that mint
- [x] 3.3 Integration test: inside a configured `Duplicates` window the second publish stores one message, while a
      publication with no MsgID still repeats
      (`TestIntegrationStampedRequestIDDeduplicatesInsideTheWindow`)
- [x] 3.4 Integration test: a republished RequestID with no window available is answered from the retained
      response, provider call count one
      (`TestIntegrationRepublishedRequestIDReusesRetainedResponseOutsideAnyWindow`)
- [x] 3.5 Mutation evidence: publishing without the MsgID kills 3.3. Baseline restored by checksum

## 4. Observe the provider-settlement classifications

- [x] 4.1 Test: an undecodable request Terminates and performs no retained lookup
      (`TestUnparseableRequestTerminatesWithoutRetainedLookup`)
- [x] 4.2 Test: unresolvable endpoint publishes its error response before source ACK, with no provider call
      (`TestIntegrationEndpointResolutionFailurePublishesErrorBeforeSourceAck`)
- [x] 4.3 Provider-call failure and post-provider publish failure are covered by the picked
      `TestIntegrationProviderErrorPubAckPrecedesSourceAck` and
      `TestIntegrationPostProviderPrePubAckReplacementMayInvokeAgain`

## 6. Review round 1

- [x] 6.1 The ruled correlation gate is observed at the unit tier:
      `TestUncorrelatedToolCallTerminatesBeforeLedgerOrExecutor` drives four shapes — missing execution id,
      missing request id, zero call ordinal, none at all — through `handleToolDelivery`. The outcome store is
      armed to fail every `Get`, so a delivery that reached the ledger would come back Retry rather than
      Terminate: the fixture pins the ORDERING (terminate-before-execute), not the refusal alone. Each case
      asserts Terminate, a permanent error naming the correlation check, zero executor invocations, zero
      publishes and one counted error
- [x] 6.2 The governance verdict subject break is recorded in
      `docs/operations/migration-beta162-to-beta163.md`: the old→new subject table, the `$message.execution_id`
      rule token, the demux site (`processor/agentic-loop/component.go:2327`), the waiter key
      (`governance_dispatcher.go:401`), the fail-closed symptom (`:485` — an enforce-mode upgrade rejects every
      call until the rules are edited) and the `audit` mode escape hatch
- [x] 6.3 Q4 injectivity is proven by a handler-path test, not by reading:
      `TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath` runs one loop through birth → tool call → tools
      complete → truncation retry and asserts the RequestIDs that reach the wire (`:req:1:0`, `:req:2:0`,
      `:req:2:1`). All three prior tests drove `LoopManager.GenerateRequestID` directly
- [x] 6.4 The specs follow the code, not the brief: a fingerprint conflict Terminates (the Quarantine claim is
      gone, and the difference is material — `natsclient/delivery_settlement.go:407-408` sets `ownerStopNeeded`
      on Quarantine only), and the agentic-governance delta no longer claims an unimplemented quarantine on
      conflicting proposal correlation. It states the four real dispositions and that `ProposalFingerprint` is
      carried, not verified
- [x] 6.5 `ProposalFingerprint` IS read (the rule echo path and the loop-side decode at `component.go:2391`), so
      it is tested rather than deleted: `TestProposalFingerprintIsCarriedAndNotVerified` pins the deterministic
      digest, the verdict round-trip, and that a DISAGREEING fingerprint still Acks — the honest statement of
      "carried, not verified". `docs/operations/17-tool-call-governance.md:324` softened to match
- [x] 6.6 Declared deviations written into `design.md`: no `<iteration>:<retry>` parse helper (an exported parser
      with no present consumer is phantom surface, and L4 owns the durable retry input), the sticky-error-response
      residual, and the `design.md:52` overclaim corrected. `governance_dispatcher.go:165` `EffectiveCallID` is
      documented as test-only
- [x] 6.7 Mutation evidence (`cp` backup + `md5 -q` verified restore, no stash, no checkout;
      `agentic-tools/component.go` baseline `8f70187c…`, `agentic-loop/handlers.go` baseline `abe5c294…`, both
      restored and `git status --porcelain` empty afterwards):

      | mutation | test that dies |
      |---|---|
      | delete the whole `validateToolExecutionCorrelation` block from `handleToolDelivery` | all four `TestUncorrelatedToolCallTerminatesBeforeLedgerOrExecutor` cases at `outcomes_test.go:418` — "an uncorrelated call can never become correlated by redelivery" |
      | replace the tools-complete mint with the literal `loopID + ":req:1:0"` | `TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath` at `request_identity_mint_test.go:107` — continuation RequestID `:req:1:0`, want `:req:2:0` |

## 5. Gates

- [x] 5.1 `task lint` 0, `task schema:generate` with no drift, `task openspec:validate` 0 (56/56),
      `task spec:properties` 0 (176/176), `go test -race ./processor/agentic-tools/... ./processor/agentic-loop/...` 0
- [x] 5.2 `task check:push` 0 on the round-1 head, zero FAIL lines, 310 `ok`, `[INTEGRATION] tests complete`
- [ ] 5.3 Implementation review resolved; archive as the final content commit

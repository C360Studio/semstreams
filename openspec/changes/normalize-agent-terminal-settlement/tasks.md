# Tasks

## 0. Accepted design records

- [x] Preserve the accepted inventory at body SHA-256
  `ae27e5111ee10e531ffe90c4505687367ea534e80c816bc401bf4b7168804676`.
- [x] Record the owner-accepted complete design at body SHA-256
  `a4b5607eefee80fd3910a769c74b509f4faec8cc04eff84cdddeae26868804c5`.
- [x] Record all nineteen owner rulings and the adopter seam in `design.md`.
- [x] Record #952 as the separate heterogeneous-subject change and #857 as the result-by-reference boundary.
- [x] Record the checked AGENT posture and its age/capacity eviction residual without a post-eviction guarantee.

## 1. Internal normalizer — TDD

- [x] Add failing production-envelope tests for success, failure, and cancellation.
- [x] Add failing tests for empty `BaseMessage.ID`, invalid message type, and invalid metadata.
- [x] Add failing tests for nil/unregistered/nonterminal payloads and failed concrete `Validate()`.
- [x] Add failing tests for empty loop ID, empty task ID, and zero `CompletedAt`, `FailedAt`, and `CancelledAt`.
- [x] Add failing tests for every invalid category/outcome collision.
- [x] Assert `loop_failed + truncated` is rejected while production failure emission remains `failed`.
- [x] Implement the private normalized projection through registry-bound `message.Decoder`.
- [x] Mutation-check that subject-based cancellation demux and skipped validation fail the tests.

## 2. Routing reconciliation — TDD

- [x] Add field-wise merge tests across tracker, terminal payload, and persisted `LoopEntity`.
- [x] Prove a complete `ChannelType`/`ChannelID` pair publishes with empty `UserID`.
- [x] Prove missing `ChannelType` and `ChannelID` may be supplied independently by compatible sources.
- [x] Prove an empty field never overwrites or conflicts with a nonempty field.
- [x] Prove conflicting nonempty `ChannelType`, `ChannelID`, and `UserID` values are permanently rejected.
- [x] Prove one empty and one nonempty `UserID` reconcile to the nonempty metadata.
- [x] Prove exactly one nonempty channel-address field is permanently malformed.
- [x] Prove both channel-address fields empty after persisted-state observation are intentionally route-less.
- [x] Prove transient persisted-state lookup failure delayed-NAKs instead of classifying the route.
- [x] Prove malformed persisted JSON and loop-ID mismatch are permanent.

## 3. Dispatch settlement — TDD

- [x] Add failing success-result, failure-error, and cancellation-status projection tests.
- [x] Add idempotent tracker projection using validated terminal timestamps.
- [x] Add stable `ResponseID` and `Nats-Msg-Id` retry tests.
- [x] Add Ack/Nak/Term disposition tests, including shutdown.
- [x] Set terminal consumers to `MaxDeliver=0` without a configuration knob.
- [x] Implement synchronous `PublishToStreamWithMsgID` settlement before source ACK.
- [x] Add bounded validation, routing, publication, and settlement metrics.

## 4. AgentRun and OTel — TDD

- [x] Replace flat AgentRun fixtures with production `BaseMessage` fixtures.
- [x] Prove success, failure, and cancellation invoke the existing AgentRun callback type.
- [x] Remove the AgentRun flat parser and use the internal normalizer.
- [x] Migrate OTel terminal interpretation to the same normalizer.
- [x] Prove OTel processing errors are not unconditionally ACKed.
- [x] Verify both framework binary wiring paths.
- [x] Verify the retained callback contract through the durable representative adopter fixture.
- [ ] **BLOCKED — DOWNSTREAM EVIDENCE NOT RECORDED.** Verify the actual semteams behavioral path after its beta.160
  migration. The checked-in representative adopter fixture is not actual semteams wiring evidence, and the available
  local semteams checkout remains on its beta.159 realignment branch.

## 5. Retention-bound evidence

- [x] Verify shipped AGENT declarations resolve to 24h, 256MiB, DiscardOld.
- [x] Prove transient failure redelivers beyond three attempts while retained.
- [x] Prove `MaxDeliver=0` does not prevent MaxAge eviction.
- [x] Prove `MaxDeliver=0` does not prevent DiscardOld capacity eviction.
- [x] Document that no response guarantee survives source eviction.
- [x] Verify fixed-reason telemetry for retry and permanent rejection.
- [x] Record the per-message eviction visibility gap without claiming a stronger response guarantee.

## 6. Ruling conformance and documentation

- [x] Replace every `UNVERIFIED` ruling row in `design.md` with exact implementation and test file:line evidence.
- [x] Re-run the terminal consumer census and prove dispatch, AgentRun, and OTel use the shared normalizer.
- [x] Verify no new payload, subject, stream, outbox, public normalized type, or adopter retry knob was introduced.
- [x] Update AgentRun, dispatch, `UserResponse`, response-delivery, release-note, and operator documentation.
- [x] Cross-link #865, #866, #952, and #857 without broadening their boundaries.

## 7. Verification

- [x] Run focused race tests for the internal normalizer, dispatch, AgentRun, and OTel.
- [x] Run `go test -race ./...`.
- [x] Run schema generation and inspect `schemas/` and `specs/` diffs.
- [x] Run real-NATS production-envelope, routing, settlement, deduplication, and retention proofs.
- [x] Run `task e2e:agentic` with clean teardown.
- [x] Complete independent `semstreams-reviewer` review with no blocking or high findings.

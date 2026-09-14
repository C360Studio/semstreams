# R5 tool-result and completed-effect recovery evidence

## Scope and baseline

This closes only the existing R5 / old 5.1–5.3 obligation, with the final review below. The owner's direction
is bounded scope without papering over a flawed design: prefer existing owners and reduce complexity where the
evidence permits. No R6–R10 implementation, new recovery authority, payload field, public API or state is added.

PR #1159 remains draft at published HEAD `5e0e2259aa7392f7f3255d7f01533869862d8174`, based on the frozen #759 branch,
not main. Tests use the preserved uncommitted R2/R4/first-R6 work plus the bounded R5 correction. The current
inventory is `inventory-r5-tool-evidence-2026-09-14.md`, SHA-256
`7cd93ffddccb868d9cd64fe029c5f09cccc44222a7aca7e59afdd5ba5a683835`: independent INVENTORY PASS, root verifier
474/474 before correction. It is a frozen baseline with explicit search limits, not a claim that changed lines
still occupy their original locations.

## Existing proof reused

Paths below are relative to `processor/`.

| R5 obligation | Evidence |
|---|---|
| Exact execution identity, fingerprint and immutable winner | `agentic-tools/execution_identity_test.go`, `outcomes_test.go`, concurrent-replica native test |
| Completed outcome replay without executor reinvocation | Native ACK-failure and result-publication-failure replacement tests |
| Missing authority, poison, post-effect ambiguity, panic and observed bounds | Tools unit suite and six selected native tests below |
| Ordered partial prefixes, repeated provider CallID, prior exchanges and once-only iteration | `agentic-loop/tool_result_recovery_test.go`, `TestColdToolResultOrderedBatchCheckpoints` |
| Failed persistence discards speculative warm routing | `TestToolPersistenceRetryDiscardsWarmRoutingBeforeColdRedelivery` |
| Persist result, then downstream PubAck, then native source ACK after replacement | `TestIntegrationColdToolResultRedeliveryUnblocksLaterApproval` |
| Exact StopLoop/max-iteration applied proof, including conflict/absence controls | `TestColdTerminalToolResultRequiresExactAppliedEvidence`, terminal replacement native test |

The six tools native tests passed before correction, with no skips (51.524s). Tools production code did not change
during R5. These include started-component/native redelivery proof as well as direct-owner real-storage proof;
they are not all OS-process-kill tests. No additional tool-effect ledger was needed.

## Two findings and their corrections

The initial independent review requested changes on two bounded findings.

1. The active tools delta contradicted the accepted #759 settlement policy. The retained policy is immutable
   outcome poison → Terminate; safe pre-effect/winner-read or ordinary completed-result publication failure →
   Retry; unresolved post-execution outcome Create → Quarantine and stop the exact owner without settlement.
   Typed observed-bounds exceptions remain unchanged. #759 design D10/tasks 4.2 and 5.3 record that policy, and
   [the owner's closure correction](https://github.com/C360Studio/semstreams/issues/759#issuecomment-5514342431)
   explicitly preserves the settlement rulings. Runtime is corroboration, not the authority for a new decision.
   The active delta/design now agree, including the full existing telemetry requirement so archive cannot retain
   its stale delayed-NAK consequence. Counter families, labels and unaffected scenarios are unchanged. A valid
   concurrent CAS winner is still reused even if the losing execution computed another result.
2. ToolResult.Name is optional, and compact/admission-rejection producers can omit it. Warm and cold loop guards
   nevertheless refused those registered results despite owning the exact originating call name. The regression
   was RED on four omitted-name cases; matching-name and conflicting-name controls passed. The correction uses
   the existing name lookup in live handling and the matched retained call in cold recovery before persistence
   and applied-proof comparison. Nonempty conflicts still refuse; strict stored-result validation is unchanged.
   The five net executable lines add no helper or second owner. The false universal-name-stamping comment is fixed.

The new unit test proves normalized persistence, ordered model context, replacement without double iteration
charging, and later-request applied proof. It faithfully constructs the admitted registered wire shapes but does
not call private tool producers or claim native PubAck. Existing tools tests cover the actual compact producer;
the existing low-payload native test now decodes through the production registry. The existing loop native fixture
uses an unnamed compact result, verifies its persisted name and exact rendered error before source ACK, and keeps
native redelivery/MaxAckPending=1 and subsequent approval checks. No new native fixture is added.

## Verification

All runs below were performed on 2026-09-14. Logs are under `/private/tmp/gh1146-r5.zj3FWR` (native/baseline) and
`/private/tmp/gh1146-r5-name.1X2igZ` (RED/GREEN/correction). The exact seven-file correction against preserved
pre-edit files is `correction.patch`, SHA-256
`b3d024e8deb5e662ab49aa878f65cbc99be045870d659c89fb69519d7eb51445`.

```sh
go test -race ./processor/agentic-tools -count=1
go test -race ./processor/agentic-loop -run '^TestToolResultOptionalNameUsesDispatchedCall$' -count=1 -v
go test -race ./processor/agentic-loop ./processor/agentic-tools -count=1
scripts/run-integration-tests.sh ./processor/agentic-tools -run '^TestIntegration(PostEffectCreateFailureIsAmbiguousAndLeavesNoAuthority|ExecutorPanicCompletesWithCorrelatedInternalResult|ConcurrentReplicasConvergeOnOneCompletedOutcome|AckFailureRestartReplaysWithoutSecondExecution|ResultPublishFailureRestartReplaysStoredOutcome|LowMaxPayloadStoresAndPublishesCompactAuthority)$' -v
scripts/run-integration-tests.sh ./processor/agentic-loop -run '^TestIntegration(ColdToolResultRedeliveryUnblocksLaterApproval|TerminalToolResultAppliedAfterReplacement)$' -v
scripts/run-integration-tests.sh ./processor/agentic-tools -run '^TestIntegrationLowMaxPayloadStoresAndPublishesCompactAuthority$' -v
```

- Baseline full tools race PASS, 1.842s; `tools-unit-race.log` SHA-256
  `aff8cd84b7b86fb039a9751de2fb90be57775815576efd405f515c150fdfeff4`.
- Six tools native tests PASS, 51.524s; `tools-native.log` SHA-256
  `1c49b1d980e74041cb966e33f33a48a92e58613c9c9cfdc3cb80346fcdcf7dcc`.
- Baseline loop native tests PASS, 93.352s; `loop-native.log` SHA-256
  `2e6d9fa8becfdec8cf7f876d3b183e250498f77be1c4c748de467e6dc8226293`.
- Name regression RED: four expected omission failures, 0.548s; `unit-red.log` SHA-256
  `b0502bdf686ef8b5b843ed0c9c6a1f93e530d0b2e0fb70cde7a66392b8e6ccec`.
- Corrected name regression GREEN: all eight cases, 1.551s; `unit-green.log` SHA-256
  `076629077efb71d9b42c38f7e854f2776680dfa5088b6485998449d776c0132d`.
- Corrected full loop/tools race PASS, 3.283s / 1.781s; `loop-tools-unit-race.log` SHA-256
  `db4596dde7d512f8139131f4910fc0fe7c6899a932e53c08a5b2dd9a5da993be`.
- Corrected loop native tests PASS, 96.260s, no skips: terminal StopLoop/max-iteration rows and unnamed compact
  result replacement followed by approval. `loop-native-corrected.log` SHA-256
  `35ef40f3dd3e4b80142797a67e1d78852fe42af5083b644e9520bf7a55b150b9`.
- Corrected compact producer/registry native test PASS, 2.264s, no skips; `tools-compact-native-corrected.log`
  SHA-256 `d59e44d4dec9a94367667cef1fa71f18aec62285acd01ae90d6def241d634e11`.
- Strict OpenSpec PASS, 55/55; `git diff --check` PASS after materialization.

Final R5 code/test SHA-256 values:

```text
agentic-loop/component.go a4c3d7c0c374cd278353efa43cd4f2b6220901f610d28fb5062f3b58becf5e56
agentic-loop/settlement_recovery.go 46e59e01e016498c0516f1d34667f6675e4cfa56cfaa3f9d8dc2f42e8db3e2ec
agentic-loop/handlers.go 32d3694527023fd91d506cba201d2ab35201f4a57c71f96c63c5087eb76c969f
agentic-loop/tool_result_name_contract_test.go f0c71865622f6e4a59594183d827c24959d4f8bd4ecc73dabf951ff74d4ecedc
agentic-loop/tool_result_redelivery_integration_test.go 46d686054394712a6898dddaeb894d3f776989a39805be145cfce8d0bdafad12
agentic-tools/outcomes_test.go 849a852732f728a9ab7f642c30196e4304069f05d7a33dfcd17002a25386e46f
agentic-tools/outcomes_integration_test.go 22627866189bd6e7008b7181a60805da04bf98bcef6e375670ac85d305f70758
```

The four spec/design/task pre-edit snapshots are in `/private/tmp/gh1146-r5-docs.Ac2j8f`. Corrected spec hashes are
tools `5261903f70027b9a0bda20f6a20a0660514941c88d0b5b64b1ca0f445ad5b161`, loop
`4ff924309982f58868e79149c7676925055eabecbd72ab4f49a25f416df8363d`, and design
`beeb224ff53f1ad4cb8eaf388aac4f5db46ca8966f57302d2fa134cb1f4dbd30`.

## Review and limits

Independent review approved the exact code/test correction and the spec reconciliation. The required bounded
post-correction judge check recommends the correction as preserving existing authority: an omitted optional name
is conversation metadata supplied by its existing owner, not additional proof of execution. Its strongest contrary
case was concealing a wrong-tool result; supplied conflicts and strict durable evidence checks prevent that in the
inspected paths. Complete tracker integrity was not re-swept; its gopls read had cache-denied incomplete results,
so no complete structural-coverage claim is made. This is not a new owner ruling or a merge verdict.

Final independent verdict: **R5 CLOSEOUT PASS / APPROVE**, 2026-09-14. The reviewer verified both corrected native
log hashes and passing rows, the unchanged tools-production basis for reused evidence, and the complete bounded
obligation map. Both original findings are resolved; no remaining blocking/high finding was identified within R5.
Root marks only R5 complete. Both runners exited successfully, cleaned their owned containers, and released the
shared lock; no owned test job remains.
R6–R10, final combined R11 proof, documentation/migration closeout, complete PR review,
archive/spec sync and staged landing remain separate. No new full push gate, E2E, commit, push, merge or issue
closure is claimed by this focused slice.

## Later pre-push preparation

The owner requested a commit/push checkpoint before R6. The first `task check:push` stopped at revive: the combined
`handleToolResultMessage` had 94 statements against its existing 80-statement limit. A behavior-preserving
extraction moves only its contiguous routed-correlation guards and optional-name normalization into the private
`validateRoutedToolResult` helper. The same handler remains the sole caller and owner; guard order, error wrapping,
Quarantine mapping and placement before authority handling remain unchanged. No rule, limit or suppression changes.

Independent review approved the exact extraction from the R5 `component.go` hash above to SHA-256
`07ce0915f73e449b45458073b450dcaaba65cb53c59cead9b8f8acc046bba7f2`. The final if-initializer assigns, rather than
shadows, the normalized result. Revive passes at 80 statements and the final full loop race suite passes (4.711s).
The before snapshot is under `/private/tmp/gh1146-prepush-extract.w22fk2`.

Schema generation also updates the existing OpenAPI state-filter description from retired development phases to
the five accepted operational states. This is generated from the already-reviewed API documentation, not a new
API or state contract. `specs/openapi.v3.yaml` SHA-256 is
`2c9a3d6e9d1409fd31b7609178eafe9164eb257d45ad15e44ce7600afb3b19cd`.

The subsequent full gate passed lint, schema, contract and unit-race checks, then exposed a stale cancellation
integration fixture: it gave dispatch a real `AGENT_LOOPS` bucket but left the loop component's bucket nil and
discarded the handler's returned error. Targeted native RED reported `AGENT_LOOPS is unavailable` (1.832s).
The fixture now shares that existing bucket, persists the complete created entity, and requires ACK plus durable
cancelled state before its existing completion assertions. The real HTTP/wire path, registered completion and
distinct operator/owner checks remain intact; production is unchanged. Targeted native race GREEN passed (2.823s,
no skips), followed by independent review approval. Test SHA-256 is
`7baf0d8f4a592ec4f3b90371975ecb4dbab014735074ec5574f97d13b0d7fb1a`; RED/GREEN logs and the before snapshot are under
`/private/tmp/gh1146-cancel-fixture.KX4TlY`. This corrects fixture setup, not R6 cancellation/restart proof.

The final frozen-source `task check:push` run passed build, lint, tagged vet, generated-schema consistency,
contracts and full unit race. Native integration passed the loop (386.449s), model (23.353s), tools (61.243s)
and every other reported package except `natsclient`. Its `TestIntegration_LegacyRequest_SuccessBodyUnchanged`
failed before assertions: Docker's container-start HTTP call exceeded its 30-second context deadline. The test
file is unchanged from the frozen parent; this matches the startup-failure class recorded on #736, but does not
prove its root cause. The required gate therefore remains FAIL, not a green run with an exception inferred.
No rerun or infrastructure correction was made. The runner exited, cleaned its containers and released its lock.

That run tested staged tree `349bd5d3f793c51d40f6f39632359f30b9d01fac`; only this evidence record changed afterward.
Log: `/private/tmp/gh1146-push.UAZVFE/check-push-ready.log`, SHA-256
`cbcf2e4f78ff4d176f13f5d964b60d8e5e799dc57d1617bb85251f91c514762d`.
Any draft-push exception requires the owner's explicit approval and does not waive a merge gate.

These mechanical publication corrections do not reopen R5 behavior or complete R6. The full pre-push gate result
and published checkpoint SHA belong to the PR's current stop point; the earlier R5-only test/source hashes above
retain their historical scope. No new E2E or final combined R11 result is implied.

# R4 task and response settlement evidence

## Scope and baseline

The owner requested **"continue with r4"** on 2026-09-14. This is existing R4 (old C.4 and 4.1–4.3), not a new
design or wider R5–R10 implementation. Active proposal/design/specs govern; dated rejected continuation-removal
proposals are history. R2 remains complete and the extra approval Store remains retired under R3.

PR #1159 is draft on `codex/gh1146-agentic-loop-restart`, published HEAD
`5e0e2259aa7392f7f3255d7f01533869862d8174`, with no upstream divergence. Parent #1156 remains
`417beae5552f8f15ad3540edd7d8504c87174c13`. Tests below use the existing uncommitted R2/R6 source, not just HEAD.
The production/config/schema diff at the beginning of R4 matched the reviewed R2/R3 checkpoint; its SHA-256 was
`c9627d339e1b9fc2c14322d2e230579babf7ff86f6bf6edda2befe66734829e2`.

The current inventory is `inventory-r4-evidence-2026-09-14.md`, SHA-256
`c0108db723ec017e92d6a3dc1833491c7f57b521cb69160db08501abb421e83a`. Root ran `task inventory:verify --` on that file:
405/405 pins passed, no drift. Its declared search limits remain explicit; independent review supplements the
provider post-PubAck replacement and heartbeat wiring. This is evidence mapping, not a new design gate.

## Independent bounded obligation review

Paths in the first five rows are under `processor/agentic-loop/`; provider paths are under `processor/agentic-model/`.

| R4 obligation | Existing source/proof |
|---|---|
| Birth, lineage, post-registration failure | `lineage_preflight_test.go:125`, `spawn_identity_failure_test.go:399`, `delivery_owner_test.go:406` |
| Task identity and state before ACK | `delivery_owner_test.go:153`, `settlement_recovery_test.go:140`, `task_loop_id_integration_test.go:56` |
| Response cold reads, duplicate/conflict/absence | `settlement_recovery_test.go:211`, `delivery_owner_test.go:358`, `settlement_recovery_integration_test.go:22` |
| Required terminal writes, synthetic effect, publication and final marker | `persist_handler_result_test.go:70`, `terminal_selection_test.go:226`, `terminal_marker_redelivery_integration_test.go:92` |
| Terminal signals; no generic terminal ToolResult proof | `settlement_recovery_test.go:329`, `terminal_tool_recovery_test.go:17` |
| Provider result reuse and permitted reinvocation | `provider_post_puback_integration_test.go:39`, `provider_settlement_integration_test.go:171` |
| Initial request and created-event PubAck failure | New `task_output_settlement_integration_test.go`; confirmed RED and bounded ordering correction below |

The new real-NATS fixture confirmed a defect: initial request commits, created publication fails, the independent
response completes the loop, then task redelivery takes the terminal shortcut and positively ACKs while
`agent.created.<LoopID>` is absent. The failure was a broker 404/10037 at the before-ACK observer, before any
byte-equality assertion. Initial-request refusal and ordinary created-refusal/retry rows passed. The initial RED
log is `task-output-initial.log`; it is not a passing closeout run.

Independent review approved the smallest correction direction: publish the existing created event before the
existing initial request. Sequential `publishResults` already waits for each PubAck and stops on failure. Thus
model work cannot complete before created commits, and cold task reconstruction uses the same builder. Active
spec/design require both PubAcks but do not require request-first order. This introduces no helper, receipt, durable
state, authority, or public API. Terminal-created replay was not selected: it is unnecessary for this demonstrated
gap and could fabricate creation after a birth rejection. The fixture must not force a response for work that was
never released; created-event timestamps may change under the admitted at-least-once contract.

## Current verification

Logs are in `/private/tmp/gh1146-r4.mlM0vX`. All commands below ran on 2026-09-14 before the ordering correction.

- `go test -race ./processor/agentic-loop -count=1`: PASS, 3.333s; `loop-unit-race.log`, SHA-256
  `3d7b8c0e4e91066ca6183912ff5929843697cf2356bc48d7d19c7e832bbedcb0`.
- `go test -race ./processor/agentic-model -count=1`: the first sandboxed attempt could not bind an httptest socket
  (`operation not permitted`), so it is not a passing test run. The unchanged command with authorized local socket
  access passed in 8.534s; `model-unit-race-authorized.log`, SHA-256
  `04015bebe6bb98a1a236c8041e9d3505b2f5c94c0bffb6db83b85d6f150b8621`.
  The original failure log is preserved separately.
- Canonical locked native loop run: PASS, all six selected tests, no skips, 66.313s. Log `r4-native.log`, SHA-256
  `c69cbcbf729464eb1b01f6378dd4b54f2b59591c70713978b8c6ab816032c19b`.
- Canonical locked provider run: PASS, all four selected tests, no skips, 6.044s. Log `r4-provider-native.log`, SHA-256
  `1f169efb8c997dccdfd80e2ee97d07fc711e3b961776e544ec5d434c9dbd2179`.
- Native task-output fixture: two rows PASS; response-before-task-retry row RED, 3.488s. Broker refusals are
  actual DiscardNew capacity failures (10077), not mocks of publication.
- A nonmutating Go overlay changed the task publication-error return to ACK. The initial-request row then failed
  on Retry-versus-ACK as expected (`task-output-ack-mutant.log`). The canonical runner stops at first failure, so
  the second row was not exercised by this negative control. This is test sensitivity, not another runtime defect.

Exact native commands:

```sh
scripts/run-integration-tests.sh ./processor/agentic-loop -run '^TestIntegration(TaskAndResponseSettleAcrossProcessReplacement|WarmResponseUsesCurrentRetainedRequestAsAuthority|TerminalMarkerFailureRedeliversAfterComponentReplacement|RuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement|OrdinaryLoopPublicationsMayRepeat|TaskConsumerSerializesRedeliveryBeforeLaterWork)$' -v
scripts/run-integration-tests.sh ./processor/agentic-model -run '^TestIntegration(MatchingRetainedResponseSkipsProviderAndAcknowledgesSource|TypedAbsenceInvokesProviderAndPubAckPrecedesSourceAck|PostProviderPrePubAckReplacementMayInvokeAgain|PostResponsePubAckReplacementReusesLiveProviderResult)$' -v
```

The terminal-marker and task-identity tests include native source redelivery after started-component replacement;
the direct-owner reconstruction cases are not OS-process restart tests. Provider proof includes a live fake HTTP
provider result, interrupted source ACK, Stop/join, replacement and retained-result reuse; the pre-PubAck case permits
another provider call. No paid provider is used. Both native runners cleaned their owned containers and released the
shared-host lock. No new E2E or full repository push-gate run is claimed.

## Corrected candidate

The only R4 production change is the two-entry publication reorder and explanation in `handlers.go`.
Two existing test helpers now locate the request by subject instead of position. No other production file changed
during R4. Pre-edit snapshots are in `/private/tmp/gh1146-r4-order.FYn9Nm/`, including the original failing fixture.
The frozen inventory describes the pre-correction baseline; independent review checks this bounded delta.

Final file SHA-256 values:

```text
handlers.go: df31b69c7501384ac7e262aa5324ccb404543d81d4558ae2b699b3bf349a29fb
create_vs_exists_fence_test.go: 346619653d7f24154b3366b58146c58e0a072c21ef87b9524997f5e0859bb80e
approval_gate_settlement_test.go: 21f1cb0d92e4a8b013fed19dcfdcd9fe006bda7ebcbe14abb4c1547eed585af6
task_output_settlement_integration_test.go: 8f40b1decf37bcc39872cda60ceeb18756d90f0443791f9ab017cab1c8e5277b
```

The final native fixture covers created/request refusal with warm and cold component objects. It proves native
KV identity before output, actual broker refusal with NAK and no ACK/TERM, no model request while creation is
missing, created PubAck before a refused request, and matching registered output identities plus native stream
sequence before source ACK. Cold objects have no inherited pending cache. Request bytes were never committed in
these failure cases; cold reconstruction may generate a RequestID. No byte-identical regenerated timestamp promise
or additional response/terminal replay scenario is claimed by this fixture.

After the correction, on the frozen dirty tree:

- `go test -race ./processor/agentic-loop -count=1`: PASS, 3.212s; `loop-unit-race-corrected.log`, SHA-256
  `335668b4cccc8c5370385c5ad88f8052e954da368005abfb39f8c30304a71a0a`.
- Canonical native loop run: PASS, seven selected tests, no skips, 66.718s. The new fixture passed all four rows
  in 1.19s. `r4-loop-native-corrected.log`, SHA-256
  `d333c2ff298e957a5658be053dc6ffda4615267eccf9e5a3a71a7a496647b799`.
- Unchanged model code retains this turn's full unit-race and four native provider PASS results above.
- `task openspec:validate`: PASS, 55/55. `git diff --check`: PASS.

```sh
scripts/run-integration-tests.sh ./processor/agentic-loop -run '^TestIntegration(TaskRequiredOutputFailureSettlement|TaskAndResponseSettleAcrossProcessReplacement|WarmResponseUsesCurrentRetainedRequestAsAuthority|TerminalMarkerFailureRedeliversAfterComponentReplacement|RuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement|OrdinaryLoopPublicationsMayRepeat|TaskConsumerSerializesRedeliveryBeforeLaterWork)$' -v
```

The complete runtime source remains local/uncommitted against published `5e0e2259`; no new full push gate,
E2E, commit, push, archive, merge or issue closure is implied. Existing R5/R6 and final combined gates remain.

## Review status

Final independent verdict: **R4 CLOSEOUT PASS / APPROVE**, 2026-09-14. No remaining blocking/high findings or missing
proof within old C.4 / 4.1–4.3. The reviewer verified all four final source hashes, both corrected log hashes,
the bounded obligation map and the evidence distinctions above. Existing helper adaptations preserve registered
payload/correlation checks; the production reorder adds no state or authority and does not change multi-turn chat.
The initial CHANGES REQUESTED verdict is resolved. Root marks only R4 complete; R5/R6, whole-PR review, archive,
final combined proof and push/merge readiness are not completed by these focused results.

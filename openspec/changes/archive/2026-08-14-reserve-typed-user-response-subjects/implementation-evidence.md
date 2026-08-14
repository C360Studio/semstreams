# Implementation evidence: typed user-response subject ownership

## Evidence provenance

- SemStreams implementation worktree: `codex/gh952-user-response-contract`, reviewed against the current diff on
  2026-08-14.
- SemStreams developer final: focused unit/race gates, repository gates, schema generation, contract tests, and
  agentic E2E green. The independent reviewer reported no code BLOCKING or HIGH findings; its remaining findings were
  stale documentation hashes and unmaterialized conformance evidence, corrected by this artifact and checkpoint.
  Final post-documentation review remains task 7.6.
- SemDev implementation worktree: `codex/gh952-park-notification-subject`. Its durable evidence is in SemDev
  `docs/evidence-ledger.md:200-238`; the independent SemDev reviewer final is **APPROVE**.
- SemTeams implementation, tag creation, and dependency adoption have not occurred. They remain explicitly pending.

## SemStreams implementation

### Typed response contract and all nine declarations

The concrete payload remains registered at `agentic/payload_registry.go:19-24`. The agentic-dispatch default output
declares `agentic.user_response/v1` at `processor/agentic-dispatch/config.go:92-96`.

Eight shipped configurations explicitly declare the same interface:

| Configuration | Exact declaration |
|---|---|
| `configs/examples/research-graph-pipeline.json` | lines 164-172 |
| `configs/flows/crud-tools-test.json` | lines 142-150 |
| `configs/flows/deep-research-test.json` | lines 156-165 |
| `configs/flows/deep-research.json` | lines 145-154 |
| `configs/flows/lesson-example.json` | lines 140-148 |
| `configs/flows/ops-agent-test.json` | lines 142-150 |
| `configs/flows/ops-agent.json` | lines 146-155 |
| `configs/research-graph-e2e.json` | lines 165-173 |

The production-merge census asserts nine effective typed declarations—the eight explicit rows plus the default-only
ninth—at `service/message_logger_census_test.go:292-307` and `:183-184`. The production decoder proof is
`service/message_logger_user_response_test.go:16-49`; the agentic E2E decodes through the production registry and
requires concrete `*agentic.UserResponse` at `test/e2e/scenarios/agentic/scenario.go:563-584`.

### Rule reservation

- Static validation: `processor/rule/config_validation.go:324-331`.
- Static coverage of `publish`, `publish_agent`, and `approve` across every action list:
  `processor/rule/user_response_subject_reservation_test.go:10-41`.
- Post-substitution enforcement: `processor/rule/actions.go:881-886`, `:1491-1496`, and `:1865-1870`.
- Dynamic direct-executor proof with zero publisher/auditor side effects:
  `processor/rule/user_response_subject_reservation_test.go:55-95`.

### Governance correction and retained behavior

- The retired field and output are absent from the accepted config surface:
  `processor/agentic-governance/config.go:85-91` and `:202-219`.
- Exact raw-key preflight runs before normal construction:
  `processor/agentic-governance/component.go:56-64` and `:176-195`; `true`, `false`, and `null` are pinned at
  `processor/agentic-governance/retired_notify_user_test.go:11-28`.
- Audit storage constructs canonical `violation.<id>` and calls the shared `natsclient.ValidateKVLiteralKey` before
  bucket lookup or any NATS I/O: `processor/agentic-governance/violation.go:164-181`. The invalid-ID test proves that
  ordering at `processor/agentic-governance/violation_preservation_test.go:80-90`.
- The real-NATS preservation test proves logs, metrics, admin alert, violation event, and KV audit storage under
  `violation.preservation-1`, with no `user_errors` output:
  `processor/agentic-governance/violation_preservation_test.go:19-78`.
- The old `violation:<id>` form could never persist because NATS KV rejects `:`. No reader, alias, conversion, or
  retained-record migration exists or is needed.

### Measured declaration truth

The frozen artifact records effective 579 rows, delta 184 rows, 47 loop/dispatch collapses, zero governance
collapses, and 27 added NATS outputs at `service/testdata/message_logger_subject_census.json:48-57`. Recomputed
production-factory assertions are at `service/message_logger_census_test.go:160-184`.

## SemDev lockstep implementation

- Exact subject/interface constants and strict raw decoder:
  SemDev `internal/conversationchannel/parkpost.go:28-93`.
- Decoder-before-no-channel disposition:
  SemDev `internal/conversationchannel/parkpost.go:116-131`, with malformed/unknown/trailing/wrong-identity pins at
  `internal/conversationchannel/parkpost_test.go:284-312`.
- Both stream catalogs and both producer/consumer ports:
  SemDev `test/conformance/park_post_contract_test.go:67-104`;
  `configs/semdev-bootstrap.json:56,228-241,364-377`; and
  `configs/semdev-live-gemini.json:56,244-257,382-395`.
- Every rule phase authoring `run.awaiting.human` through add, update, or non-empty reconcile must publish exactly
  once on the exact subject in that same phase:
  SemDev `test/conformance/park_post_contract_test.go:106-176`.
- The new durable identity is exercised at SemDev `test/e2e/parks_journey_test.go:170-192`.

## Verification evidence

### SemStreams

- Focused unit/race tests for rule, governance, message-logger, dispatch, and schema surfaces: **PASS**.
- `task lint`: **PASS**.
- `go test -race ./...`: **PASS**.
- `task schema:generate`: **PASS**, with only the intended governance schema delta.
- `go test ./test/contract/...`: **PASS**.
- `task e2e:agentic`: **PASS** in 45.324s. The terminal proof decoded
  `agentic.user_response.v1` through the production registry to concrete `*agentic.UserResponse`; the E2E stack was
  cleanly torn down afterward.
- Strict OpenSpec: **PASS**, 48/48. Checkpoint verification is regenerated after this documentation correction.

### SemDev

SemDev `docs/evidence-ledger.md:211-227` records focused gates, `task check`, strict OpenSpec 16/16, and the real-forge
park journey. The exact E2E command passed in 12.48s test time / 14.006s package time on fresh isolated NATS. At
SemDev `test/e2e/parks_journey_test.go:78-146`, the forge POST barrier holds the named durable at
`NumAckPending >= 1`; after the real comment lands, `NumAckPending` reaches zero. The isolated stack was cleanly
removed without touching the co-resident SemBoids stack.

## Explicit pending gates

- **PENDING — SemTeams:** delete the two flat coordinator actions, retain typed producers/observation, run its census,
  contract, and relevant E2E suites, then adopt the breaking version.
- **PENDING — tag:** create the breaking SemStreams tag only after final review and cross-repository landing approval.
- **PENDING — adoption:** update SemDev and SemTeams dependencies only after that tag exists; no pre-tag dependency
  bump is evidence of adoption.
- **PENDING — final review:** rerun independent SemStreams review after these documentation/checkpoint corrections.

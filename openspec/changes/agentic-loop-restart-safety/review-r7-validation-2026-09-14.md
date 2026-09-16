# R7 validation publication settlement checkpoint

Status: **SCOPED IMPLEMENTATION PASS**. Independent review approved the two-file validation publication correction.
R7 remains unchecked. This record is not whole-R7, whole-PR, push, or merge approval.

## Retained-verdict inventory refresh, 2026-09-15

`inventory-r7-retained-verdict-refresh-2026-09-15.md` received independent **INVENTORY PASS** at SHA-256
`8b73908fd59f17708f9c5602fd9c4be0f4bb532f4c30bab792deade735c09a1c`. The independent reviewer and root both verified
183/183 exact pins, with zero drift, malformed or unparsed entries. No production source changed for this refresh.

Initial review of `164134b6…` requested two factual additions only: the parent-loop input's live source/default and
cold restoration, and the already-approved R8 admission contract/current startup ordering. The appended supplement
resolves both. Existing reader, caller/error-propagation, adopter and native/E2E proof boundaries were otherwise
adequately captured. The review adds no runtime scope, new policy, or admission implementation request.

The original broad/wire inventories remain preserved at their recorded identities. Intake follows the owner's
[remaining-#1146 bounded-reading ruling](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5679435736).
Inventory approval permits the next bounded design/task handoff; it does not approve implementation, complete R7/R8,
or discharge #1311 source-settlement and final combined verification.

### Live proposal-match handoff

The architect's `task-r7-live-proposal-match-2026-09-15.md`, SHA-256
`fc4b2d4c6a1c43947ac27c6b2c35705281a6f6628c2cb7834060bb584ecb7ca1`, received independent **CONFORMANCE PASS**.
The existing requirement already quarantines conflicting proposal/verdict correlation. Preparing the proposal once,
retaining it with the existing waiter and comparing the normalized verdict implements that accepted requirement;
it adds no public API, authority, policy, storage or lifecycle owner. Standing execution approval permits TDD.

This is task conformance, not implementation approval. Retained reads, admission, source error propagation,
replacement evidence and #1311 remain separate open obligations. The developer owns only the named dispatcher
implementation and focused fixtures; root retains task truth and the reviewer remains independent.

## Baseline and ownership

- Worktree: `codex/gh1146-agentic-loop-restart`; HEAD `c347eff487f50b93bc338d764f43ef5b5ea5e133`.
- PR #1159 still targets `codex/gh759-semantic-settlement`; its remote parent remains `417beae5`.
- The independently reviewed R6 dirty changes are preserved and are not part of this implementation slice.
- Developer `/root/r6_callback_tests` owns only the shared governance publication-error return and its existing
  callback regression in `processor/agentic-governance/component.go` and `delivery_settlement_test.go`.
- Root owns this record and task truth. Reviewer `/root/r5_tool_review` remains independent of implementation.

## Reviewed slice

The validation-only inventory checkpoint is
`f1d4c1c00e26f1cc7d17fb71ebd1ae3ee37c8ae7515c911273b829f206d67289` (331 verified pins).
An immutable copy is `inventory-reviewed.md` in the durable evidence directory below.
The independent reviewer passed the three physical validation callbacks, shared handler, filter/context propagation,
nonblocking audit, and settlement owner. Full R7 inventory was not passed.

The architect confirmed this as conformance to the existing approved contract, not a new design:

| Existing contract | Measured implementation | Bounded correction |
| --- | --- | --- |
| Allowed output without PubAck retries; output may repeat | Shared publication-error return quarantines | Return Retry |
| Each physical callback owns source settlement | Existing regression exercises only task validation | Exercise all three |
| Retry does not relinquish consumer ownership | Existing regression expects owner loss | Prove NAK and continued admission |

The contract home is `specs/agentic-governance/spec.md`, requirement
`Governance validation settles after its declared consequence`, scenario `Allowed output publication fails`.
The at-least-once constraint is also explicit in `Governance publications are durably at-least-once`.
Owner ruling [5538906152](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5538906152)
rejects generic exactly-once publication recovery. This correction adds no state, helper, API, policy, or lookup.

## Evidence

Baseline governance source was unchanged from HEAD:

```text
f825eab0036e8fe3dccfcd296f1213ed25ae5299c1e0ed208cbe196220059165  component.go
cab3e9fc7672ca8e72e07b30e35db671fc0ba9e27201930f84cddd82253e493f  delivery_settlement_test.go
```

Baseline command:

```sh
GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off \
  go test -race ./processor/agentic-governance -count=1
```

- Sandbox attempt failed at the existing embedded NATS readiness check in
  `TestViolationHandlerRetainsAuditAdminMetricsLoggingAndViolationEvent`, before its behavior assertions.
  This is not behavioral RED.
- The same baseline command with local listener access passed, exit 0, package time 1.586s.
  The old publication-failure test passed because it deliberately expected Quarantine.
- Intended RED: the updated regression reached all three installed callbacks. Each expected NAK count 1 and
  observed 0, with the old owner-loss behavior. Exit 1, package time 0.575s; no runtime change was present.
- Final governance package race pass: exit 0, 1.712s, including all three callback rows and the unchanged
  malformed-input and panic controls. The updated regression invokes each callback twice, checks NAK/no ACK/TERM,
  healthy/running owner status, and no consume-handle drain. Test cleanup cancels and joins even on assertion failure.
- Pinned package revive: exit 0, no warnings. `gofmt -l` for both source files: no output.
- Independent implementation review: **APPROVE**, no findings in this two-file slice. The reviewer verified the
  source, exact patch, RED/GREEN artifacts, preserved counter/cause, and failure-safe observer cleanup.
  No native integration or whole-PR gate was run for this slice.

All developer commands use the same `GOCACHE`, `GOPROXY=off`, and `GOSUMDB=off` prefix shown above:

```sh
go test -race ./processor/agentic-governance \
  -run '^TestGovernanceAllowedPublicationFailureRetriesAllProductionCallbacks$' -count=1 -timeout=60s -v
go test -race ./processor/agentic-governance -count=1 -timeout=60s -v
go tool revive -config revive.toml -formatter friendly ./processor/agentic-governance/...
```

The final source is the one-line publication-error disposition change plus the existing callback regression:

```text
7ad460812839a75afd663a971f23bde1e8128f1ff6765fde55620d646de16897  component.go
e9b75fdf37e1d83e87368098596a944893093e27f6da324da88e03a0c5f1da6f  delivery_settlement_test.go
```

Evidence hashes under `validation-tests/` in the durable directory:

```text
3b26efa89efc3ef9a34c0482dd02db55f675dbbf544c209be6cb9c5f1cca24a0  publication-red.log
6d289dff42d99d9fae5f5b2e19667b75f65f110b35ddcde4619fc0aca1c992c2  governance-package-race-green.log
bd9c156357d878c1c028ac4312d671cfd408dbafadca96e6ddef8f978705dd05  source.patch
8dd222eddc7b9a0a6ca3e729588693e2c0393a261b2df8a1ef7a6d604fe0142d  source.sha256
```

Root verified the copied evidence hashes, final source hashes and the intended failing assertions. The full package
log retains the individual test outcomes; the pass is not an inference from compilation or a filtered command exit.

Durable evidence directory:

```text
/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r7-governance.xEUmEf/
```

## Remaining R7 work

### Additional callback proof: independently approved

The test-only followup adds `processor/agentic-governance/validation_outcomes_test.go`, SHA-256
`f6ab7ad1aa4a0e4a84bec3dfc0fe7ed47fa3c809f20eff0202d8ba96ecc8fdfe`. Both previously approved source hashes are unchanged.
It exercises all three installed callbacks for denial despite observable audit failure, transient filter failure,
and active-filter cancellation with explicit join-before-settlement observations. The only new fixture is a private
test function adapter for the existing Filter interface; production APIs, policies and deadlines are unchanged.
Timers bound test observation/cleanup only; no sleeps or AckWait-derived deadline are introduced.

All nine cases passed on arrival under the race detector (exit 0, 1.527s). This proves existing behavior;
it is not a runtime RED/GREEN claim. Pinned package revive and formatting are clean. Independent review approved
the exact file and evidence with no test findings; the separate rule-input ownership question remains open.
Root then ran the complete governance package race suite on the combined three-file snapshot: exit 0, 1.640s.
`combined-race.json` in the durable directory records that exact source identity and command result.
Exact command, with the same environment prefix as above:

```sh
go test -race ./processor/agentic-governance \
  -run '^TestGovernanceProductionCallbacks(DenyAndFilterFailure|JoinCancelledFilter)$' -count=1 -timeout=60s -v
```

Artifacts under `validation-outcomes/` in the durable evidence directory:

```text
468ca24ced3f230388b163b4393e8f7ac10497642e8f78f2564ee05293ad485a  validation-outcomes-race.log
4c028ed188b37b45a1e2465d93f9db7dabc4d33386fd3178b7bd7b078fafa2ae  source.patch
eaf5f996ebdb7c0cdfe8fd6242279a2dde1b6cf5d1c51a4c21ff87b2e9e33c42  source.sha256
```

### Unproven boundaries

Native source/redelivery/PubAck-at-settlement proof remains. The existing integration test uses real output
publication but fake source messages and consume handles; it is not native redelivery proof.

#### Existing native output-wiring recheck, 2026-09-15

The unchanged `TestIntegrationGovernanceProductionCallbacksPublishBeforeAck` passed through the canonical runner:

```sh
GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly \
  scripts/run-integration-tests.sh ./processor/agentic-governance \
  -run '^TestIntegrationGovernanceProductionCallbacksPublishBeforeAck$' -v
```

Exit 0, one selected test, zero skips; test 0.25s, package 4.075s. The six manual callback invocations cover all three
validation ports and observe real NATS output subject, Message type, and ID. Source messages/handles remain fake;
output is observed after callback return. This does not prove native source ACK/redelivery, replacement, failure
retry, or publication-before-ACK ordering. No source or test was changed, and no additional suite was run.

All 39 source-manifest entries were unchanged after the run and independently rechecked by root. Evidence is in
`/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r7-recovery-intake.0Z0PEj/validation-native/`:
`native.log` SHA-256 `8ef81a20b0c6dcc4a9a8802110891338a7c803083ca3a89bc241c4201e62d409`;
`source-before.sha256` SHA-256 `c9aa6524919e31a8907a82c4ff8fb60bbef694f242312f9a46e0438aaf6f1098`.
The run completed and its test work joined. R7/R11 remain open; this adds evidence, not implementation approval.

#### Earlier inventory closeout, 2026-09-14

The broader review accepted the four boundary additions in inventory snapshot `4dd984d5`. These are now
inventoried but unresolved or unproven seams, not missing inventory entries:

- The rule publisher's dynamic verdict subject classification and required PubAck, an existing R8 dependency.
- Required verdict-field comparisons and the second decode that loses registered-envelope reason/rule context.
- The exact retained-verdict read and its response-replay caller; existing exact reads serve request/response only.
- The distinct best-effort verdict audit record, which must not be counted as routing/recovery authority.

The reviewer also measured an external proposal-input settlement boundary: rule action failure is logged/counted,
the stateful evaluator persists match state and returns nil, and the rule message callback ACKs. The existing
`TestRunActions_NonDenyErrorContinues` expects nil after a transient publish error. Thus checking the publisher's
error return alone cannot establish proposal-to-verdict settlement. This rule input is outside the frozen
15-subscription claim and #759's nine heartbeat bindings. Its ownership is not assigned by this record.

Live issue inspection found [#935](https://github.com/C360Studio/semstreams/issues/935), which describes best-effort
action continuation and partial-action consequences, marked `horizon:post-v1`. Its broader atomic/abort-on-failure
request is not silently pulled forward here. [#1145](https://github.com/C360Studio/semstreams/issues/1145) covers
framework restart declarations/conformance, not an already-selected implementation of this rule-input correction.
Resolve this boundary's ownership and the precise governance guarantee before end-to-end R7 acceptance; do not turn
this discovery into a generalized rule-engine change or claim that a filed issue proves settlement.

Final independent inventory and record closeout passed at inventory SHA-256
`96d9c252c96d938a19629cf7c59da95da67815f6e4b9f5a2d612d2bffe65904a`. The external rule-input boundary is now
recorded with its production source owner and error-propagation tests. Historical validation pins remain explicit
pre-fix snapshot evidence under the preserved `f1d4c1c` inventory, not current-code or whole-R7 proof.
This pass adds no target design or implementation authority. The scope decision remains with the owner.

R6 waiter evidence remains usable within its recorded limits. No new governance bucket, recovery runtime, broad
wire-format migration, or policy expansion is admitted by this checkpoint.

## Followup: bounded rule-input scope review

The owner approved a read-only design pass on the external rule proposal-input settlement boundary.
The parent inventory above remains unchanged at `96d9c252`.
`inventory-r7-rule-replay-2026-09-14.md`, SHA-256
`54e20f241c932b8e751ff9f193ba6dca1ff2e615312f2292e4cdada7e57939af`, received independent **INVENTORY PASS**.
The mechanical verifier passes all 72 pins. It records stable source-message identity versus new proposal identity,
OnEnter-to-WhileTrue replay suppression, action counters, partial effects, and adjacent no-replay contracts.

`design-r7-rule-input-scope-2026-09-14.md`, SHA-256
`b72ce0bbbc6a76b7cea0455901690713a86b9724ba0bab676b19d6fea11db306`, received independent **DESIGN REVIEW PASS**
as a scope-level recommendation only. The reviewer required enforce/audit distinctions and replaced an unproven
admission-mechanism claim with an explicit design obligation. Both corrections are reflected in that exact draft.

The recommendation is a separately claimed publication-only governance prerequisite, not absorbing all of #935.
The alternative is an explicitly owner-approved narrower guarantee. The candidate restriction is new behavior;
its admission and hot-reload design, implementation cost, and native proof are not established by this pass.
No owner choice, implementation-ready design, new claim, milestone movement, or rule-engine change is implied.
R7 remains unchecked. Existing source hashes and published c347 checkpoint are unchanged.
Strict OpenSpec validation passes 55/55; no runtime tests or full push gate were rerun for this read-only pass.

The owner subsequently selected the recommendation: "continue per your recommendation".
The direction and same-milestone placement are recorded on
[#1311](https://github.com/C360Studio/semstreams/issues/1311#issuecomment-5666981667), separately claimed by
[draft PR #1312](https://github.com/C360Studio/semstreams/pull/1312). That two-document design claim starts from
main `7698a59f` at commit `4039530ff6213a25b946332c876b6d1e58c86a04`; it is not independently landable by assumption.
The unpublished #759 types, #1159 SettleDelivery/correlation work and R8 publisher dependency remain explicit.
This selects scope/claiming, not implementation-ready admission/reload behavior. #935 remains post-v1.
R7 stays unchecked without weakening its intended guarantee, and unaffected #1146-owned proofs may continue.

## Unaffected output-publication control refreshed

The existing `TestIntegrationGovernanceProductionCallbacksPublishBeforeAck` passed on the unchanged reviewed
R6/R7 source snapshot: selected test 0.26s, package 4.344s, exit 0, no skips. Exact command:

```sh
GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly \
  scripts/run-integration-tests.sh ./processor/agentic-governance \
  -run '^TestIntegrationGovernanceProductionCallbacksPublishBeforeAck$' -v
```

This rechecks real NATS output publication for all three installed validation callbacks, twice each. The source
messages and consume handles are fake. It is not native source redelivery, process replacement, or an independent
PubAck-at-ACK ordering observation. Those R7 obligations remain open; the test name does not broaden its evidence.

The existing package fixture and selected test each created a NATS container; both were terminated. The runner used
its shared host lock, cached pinned image and normal cleanup. A final exact-ID Docker check found neither NATS
container nor the run's reaper, and the shared lock owner was absent. No source, harness or configuration changed.

The durable evidence directory above now contains `publication-control-2026-09-14.log` and
`publication-control-2026-09-14.json`; the latter records the exact command, time, source hashes and scope.
The integration file remains `1b45323a3ecce000e7241da51690a4a68ee5e486a275d3770244f8dcae2630f4`.
All previously recorded governance and R6 source hashes are unchanged. This is no new whole-package, R11 or E2E pass.

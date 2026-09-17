# R8 loop-authority implementation handoff

base: c364b87c7034426c5f677a26479b52f43789ed20

Status: owner-accepted lowering; independent materialization/conformance review required before implementation.

Owner acceptance: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5696610710.
The owner accepted loop-side bucket-setting retirement and maximum/default approval wait 12h, not a recovery guarantee.

## Evidence retained

The accepted `inventory-r8-loop-authority-2026-09-16.md` remains unchanged at SHA-256
`4cf85988cdaa429000628afa907b6076fa1f3f54f8de79a7e1b5a4acc1358d32` (independent INVENTORY PASS, 118 pins).
The dated lowering docket and reviews remain historical evidence; the new acceptance supersedes their pending choices.
The inventory included preserved WIP: c347-to-c364 committed changes, but current bytes still match its measured hashes:

| Source | SHA-256 |
|---|---|
| processor/agentic-loop/component.go | 4b5bb15f45d6bcf6df18ca90ad8f871d554fae3373d99263aa007f4cfe047af2 |
| processor/agentic-loop/config.go | 3fd0b3e0802025803f01c9c144e7b8fa2cf3809417678e9d75e4a1b7f3ba5604 |
| frameworkcapabilities/graphresearch/register.go | ca37d3b1e429e3898b3e93281c87aa0b8100f41b456167db618b3effcba4e75b |

The architect reused the accepted inventory/options, read the current tasks, and verified relevant source hashes.
No new inventory or design choice is inferred. Root materializes the exact accepted text in the active files below;
review covers those complete replacements together with this handoff, not a second copy of the whole design.

## Exact active contract and documentation homes

- `proposal.md / Holds`: loop-authority bullet records both accepted choices.
- `design.md / Loop-owned AGENT_LOOPS acquisition`: complete integrated initialization/configuration contract.
- `specs/agentic-loop/spec.md / Loop-state authority has one port declaration`: sole existing port, explicit removed-key
  refusal and research agreement-check adaptation.
- `specs/agentic-loop/spec.md / Approval lifetime is bounded by loop-state authority`: omitted default, inclusive
  `(0,12h]` range, explicit invalid refusal, no clamping, retained-deadline preservation and nominal grace only.
- `specs/agentic-loop/spec.md / Loop-state authority is acquired and observed before loop work`: typed acquisition,
  actual-policy observation and failure-before-handle/snapshot/dependent-allocation, with trajectory policy unchanged.
- `docs/operations/migration-beta162-to-beta163.md / Loop bucket declaration and approval-wait limit (#1146)`:
  removed-field migration, custom-port example, tools/stages unchanged and longer-window exclusion.
- `docs/concepts/17-approval-flow.md / Timeouts and restart`, loop README and package documentation: scoped corrections.
- `tasks.md / R8`: conservative implementation/proof obligations remain unchecked.

The active spec retains all four existing acquisition race/drift/lookup scenarios and all unrelated requirements.
No additional capability-delta file or changed requirement heading is needed.

## Ownership and minimum integrated change

The developer is not alone in the worktree. Preserve other owners' edits and wait for root's implementation release.
This is one initializer/configuration/caller slice, not an uncalled-helper delivery.

| Owner/files | Required change and proof |
|---|---|
| Loop component.go/config.go | Extend the existing retired-key/config owner; remove only loop-side field; use captured normalized output facts; preserve DeclarePorts/NewComponent parity; install authority only after admission. |
| Loop internal/loopbucket | Implement the already accepted internal AcquireOwner and call it from the real initializer. No exported framework API, optional observer or caller-installed setter. |
| Graphresearch register.go and tests | Obtain loop identity through existing DeclarePorts/canonical facts; preserve other owners' bucket selection and runtime behavior. |
| Existing config/constructor callers and tests | Adapt removed loop Config references; replace obsolete raw-name tests with port-selection/refusal proof, not deletion of their protected behavior. |
| Nine inventoried JSON configurations and generated loop schema | Remove only loop-side raw keys, including default-valued occurrences; preserve custom ports and tools/stage raw settings. Regenerate through the existing schema owner. |
| Existing loop native fixtures | Correct success fixtures at loop_integration_test.go:61, :120 and :715 to establish admitted authority. Preserve intentional incompatible-authority cases and trajectory controls. |
| Root/writer | Active specs/tasks, migration, concept, README and doc.go corrections. Developer does not alter documentation/task truth. |

## TDD obligations

| Tier | Behavior |
|---|---|
| Unit: production JSON | DeclarePorts/NewComponent reject present retired keys (null/empty/default/matching/custom); actionable replacement error. Omission and custom canonical ports preserve declaration/runtime identity. |
| Unit: timeout | Omission becomes 12h; positive sub-limit and exactly12h accepted; empty/null/wrong type/malformed/negative/zero/12h+1ns refused. No defaulting/clamping; schema removes old field and describes default/cap. |
| Unit: research composition | Default/custom agreement passes; actual loop-port mismatch fails despite otherwise matching raw names; invalid declaration propagates; unselected research unaffected. |
| Unit: acquisition | Existing match, fresh create, wrapped absence, other lookup failure with zero create, typed Exists with exactly one get, race-get/create/status failure, incomplete policy and each mismatch. Preserve context/cause; zero update/reconciliation. |
| Unit: component ordering | Admission failure reaches existing Start rollback before handle publication, snapshot, consumers/queries or sweeper. Helper-only green does not satisfy this row. |
| Native: production boundary | Real KV create/open/race and actual-policy refusal; valid authority reaches normal startup; bad authority leaves dependent work unallocated. Preserve nonblocking trajectory failure. |
| Existing approval controls | Replacement retains RequestedAt/Timeout. Adapt obsolete untimed-default expectation to different valid new config without weakening retained-deadline or native timeout-owner assertions. |

Each new behavior test cites its active requirement heading. Observe the intended assertion RED before implementing;
field-removal compile errors are not behavioral RED. Cover the internal helper through its real production caller.
Grammar/boundary properties exercise production decoding without NATS and reach the inclusive boundary explicitly.
Do not derive expected acceptance by calling the implementation's validator. No new test sleeps or public test seams.

Focused unit race comes first. Native verification uses the canonical runner under root coordination. Do not create
one native test per unit-matrix row. New native starts/time require measured accounting and any required narrow
exception; R7's cost approval does not cover R8. Existing shared TestMain use is not permission to add shared state.

Before publication/landing, root selects existing preflight gates, including schema/contracts and relevant agentic E2E.
Older greens do not approve this later source. R11's final combined proof remains separate.

## Boundaries and decisions preserved

No research provisioning, generic rule KV provisioning, graph bucket catalog, stream-horizon arithmetic, governance
source settlement, tool-effect protection, trajectory-policy or retained-deadline changes. No new store, lease,
timer, coordinator, public knob or admission API. Existing Start-derived context and failed-start rollback remain.

Semstreams-dev supplies production JSON/declaration proof and tiered TDD. Orchestration-check keeps this in component
startup, not a new layer. The bounded-intake ruling 5679435736 remains in force. R7/R8, #1311/#1312, frozen #1156,
R11 and final review/archive/landing holds remain. Acceptance alone completes none of them.

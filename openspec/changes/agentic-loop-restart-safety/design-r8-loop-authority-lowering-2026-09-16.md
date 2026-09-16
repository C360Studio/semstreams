# R8 loop-authority lowering checkpoint

base: c347eff487f50b93bc338d764f43ef5b5ea5e133

Status: recommendation only; two contract details require owner selection before implementation handoff.

## Accepted inventory

Retain without replacement:

`openspec/changes/agentic-loop-restart-safety/inventory-r8-loop-authority-2026-09-16.md`

SHA-256: `4cf85988cdaa429000628afa907b6076fa1f3f54f8de79a7e1b5a4acc1358d32`

Independent INVENTORY PASS; final materialization has 118/118 verified pins.
Production source hashes remain unchanged. R7 test grouping is separate.

## Finding

The accepted acquisition mechanics fit the existing component initializer.
A coherent public-start implementation cannot yet be handed off without deciding:

1. What happens to the existing raw `loops_bucket` surface when the admitted loops port becomes authoritative.
2. The numerical approval safety margin and its exact acceptance boundary.

These are not reasons to reopen bucket ownership, introduce another gate, or reconsider the approved get/create/race
contract. They are missing observable behavior at two existing seams.

## 1. Bucket identity: authority selected, duplicate-surface disposition unresolved

The accepted design already selects the admitted `loops` KV-write port.
It does not select precedence between two configuration inputs.

Current evidence:

- `processor/agentic-loop/component.go:804` reads `c.config.LoopsBucket`.
- `processor/agentic-loop/config.go:438` separately declares the loops KV-write output.
- `frameworkcapabilities/graphresearch/register.go:333` checks raw names across loop, tools and five stages.
- `processor/agentic-loop/component.go:200` exposes existing `DeclarePorts`.
- `processor/agentic-loop/component.go:201` calls the same `resolveConfig` used by NewComponent.
- `processor/agentic-loop/component.go:205` returns the effective port configuration.

Simply switching acquisition to the port would silently ignore some existing raw configuration.
Leaving research validation untouched could also approve matching raw names while the loop actually opens another bucket.

### Smallest genuine options

| Option | Observable behavior | Principal cost |
|---|---|---|
| N0 — Hold implementation | Preserve current behavior; leave R8 unfinished until the duplicate surface is ruled. | Delays the admitted authority gate. |
| N1 — Keep raw name as an explicit agreement constraint | Port selects the bucket; raw config never selects or overrides it. Disagreement fails configuration admission. The existing research validator compares actual loop port identity with its other current owners. | Preserves a redundant fact adopters must keep aligned; default/omitted versus explicitly supplied raw values require precise treatment. |
| N2 — Retire the loop-side raw surface | Port is the sole loop declaration. Remove the Go field/schema entry and reject legacy JSON explicitly. Extend the existing research validator to observe loop DeclarePorts; keep tools/stage bucket semantics unchanged. | An explicit configuration/API break requiring owned configuration/schema/test updates and an adopter migration note. |

Recommendation: **N2**, subject to owner approval.

It removes the measured duplicate declaration and uses existing configuration/port owners.
Its strongest cost is the intentional break for raw-only custom configurations.
That cost must not be concealed as a mechanical initializer change.

N1 is a real compatibility alternative, but not a silent fallback or raw-to-port translation.
Neither option authorizes changing research-stage provisioning, execution, or tool bucket selection.
The research change is only adaptation of its existing common-bucket check to the loop’s actual selected identity.

Do-nothing path under N2: default configurations without the retired key continue selecting AGENT_LOOPS.
A retained `loops_bucket` JSON key gets an actionable configuration error; a Go struct literal gets a compile error.
It is never silently ignored, aliased, or used to override the admitted port.

## 2. Approval lifetime: the accepted requirement is not executable arithmetic

The active loop delta at lines 741–753 requires:

- Default approval timeout of 12 hours.
- Finite, nonzero timeout within observed KV TTL after a framework safety margin.
- Refusal of explicit empty, zero, indefinite and over-retention values before dependent work.

The accepted observed TTL is exactly 24 hours.
Neither that default nor the existing five-second sweeper cadence specifies the margin.

### Smallest genuine options

| Option | Observable behavior | Principal cost |
|---|---|---|
| M0 — Hold implementation | Do not invent a margin; keep the lifetime obligation explicitly open. | Delays a complete startup-conformance task. |
| M1 — Owner selects a fixed framework reserve | Define a private reserve `M` and exact comparison; no public knob. Recommended comparison is `0 < timeout <= observedTTL - M`. | The value controls which configured approval windows are admitted and needs an explicit ruling. |
| M2 — Amend to strict-before-expiry only | Admit finite `0 < timeout < observedTTL`, without a fixed reserve. | This changes the accepted safety-margin requirement and permits arbitrarily small headroom; it is not conformance-only lowering. |

Recommendation: **M1**, with the exact numerical `M` supplied or expressly approved by the owner.

For the recommended inclusive comparison, preserving the accepted 12-hour default requires `0 < M <= 12h`.
This is a consistency constraint, not a proposed margin.
There is no evidence-backed numerical recommendation in the collected record.

Do not infer `M=5s` from sweeper cadence, `M=12h` from the default, or a stream replay horizon from either.
Those would each introduce a policy relationship the accepted record does not establish.

## 3. Approved mechanics remain ready, but are not a standalone helper task

After the two selections, the lowering remains within these existing owners:

| Responsibility | Existing home / bounded extension |
|---|---|
| Effective loop identity and configuration errors | `processor/agentic-loop/component.go`, `config.go`, existing DeclarePorts/resolveConfig |
| Loop-side research composition agreement | `frameworkcapabilities/graphresearch/register.go` and its tests; no research runtime behavior change |
| Acquisition and observed policy | Existing initializer calling the already accepted internal `loopbucket.AcquireOwner` |
| Startup ordering and error propagation | Existing Start/failed-start rollback; no additional public gate |
| Approval lifetime | Existing approval configuration plus validation against the acquired authority |
| Proof and fixture adaptation | Focused owner tests and existing loop native fixtures, including lines 61, 120 and 715 |
| Public documentation/configuration truth | Root/writer-owned schema, nine configuration files and migration note, according to the selected name policy |

The accepted acquisition invariant remains unchanged:

Get first; create only on typed BucketNotFound; propagate other lookup errors without creation.
Typed BucketExists permits exactly one get.
Observe actual History 10, TTL 24h and MaxBytes `<=0` after every successful acquisition path.
Refuse drift without reconciliation.
Publish no handle and start no approval snapshot, dependent consumer or sweeper before required admission succeeds.
Pass the Start-derived context into I/O and propagate failures through existing rollback.

The trajectory audit path keeps its distinct nonblocking failure policy.
Matching research declarations are existing co-creators, not a mandate to migrate their provisioning.
Deliberately incompatible test fixtures remain refusal cases; generic Bucket-only success fixtures need adaptation.

No uncalled helper, raw-name bypass, missing-margin fallback or additional readiness surface is authorized as progress.
No implementation task is released by this checkpoint.

## Decision request and limits

The narrow owner request is:

1. Select N2 retirement, N1 explicit agreement, or continued hold.
2. Select M1 with an exact reserve and boundary, M2’s explicit contract amendment, or continued hold.

Then reconcile only those clauses and materialize the bounded TDD handoff for independent conformance review.

The orchestration-check skill kept this work in component startup and existing configuration owners.
No code, tests, schemas, tasks, PR state or research behavior was changed.
Whole R8, #1311, frozen parent #1156 and held #1312 retain their existing obligations and holds.

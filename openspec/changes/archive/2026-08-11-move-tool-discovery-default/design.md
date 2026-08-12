# Move tool-discovery default design

**Status:** The address cutover, exact post-Foundation-B control/current-census amendments, startup-atomic correction,
focused race gates, service census/full service race, independently rerun full repository race suite, and fresh
corrected-tree crud-tools/agentic E2Es are complete and green. Frozen TSV diffs are empty and strict OpenSpec is 42/42.
Final independent SemStreams correction review returned `APPROVE`; the change is complete, green, and merge-ready but
has not merged. Candidate selection has not begun; no product tag exists, and #827 has not executed. Issue #810
remains parked with no generic overlap implementation.

**Reviewed correction design:** `/private/tmp/tool-discovery-control-inventory.md`, SHA-256
`5c7c7d6b9f3331dce7567339e6ec9148dfd4bea20185caac5a59c5652b2eff18`, `DESIGN REVIEW PASS`.

## Pre-cutover surface inventory

| Surface | Current evidence | Required target |
|---|---|---|
| Default declaration | `processor/agentic-tools/config.go:124-135` declares logical `tool.list` as ordinary `nats` on subject `tool.list`. | Keep the name, use `nats-request`, default to `discovery.tool.list`. |
| Runtime subscription | `processor/agentic-tools/component.go:175-199` starts from a hard-coded `tool.list`, optionally replaces it from port facts, then warns and continues on subscribe failure. | Resolve the declared request port once and subscribe only to its subject; invalid kind fails startup. |
| Discovery adopter note | `docs/operations/adopter-tool-effect-metadata.md:62-80` defines response metadata without naming the request address. | Name the logical port, effective default address, and breaking migration. |
| Agentic component guide | `docs/advanced/08-agentic-components.md:328-355` omits discovery from the port table and example. | Show the `tool.list` `nats-request` input and default subject. |
| Stream guide | `docs/advanced/08-agentic-components.md:575-590` recommends `tool.>` for the AGENT stream. | Use only `tool.execute.>` and `tool.result.>` for tool traffic. |
| Live discovery proof | `test/e2e/scenarios/crud-tools/scenario.go:393-447` requests the old subject and diagnoses broad stream capture. | Request `discovery.tool.list` and prove a nonempty catalog. |

Historical ADRs, remap inventories, archived OpenSpec, and `docs/proposals/prev1-program.md` are evidence of earlier
rulings. They are not live configuration guidance and remain unchanged. The dated migration note supersedes their old
default-address guidance without rewriting history.

## Boundary

The merge of this change closes #842 by moving one shipped default and making the existing request/reply semantics
explicit in the port kind. It does not implement #810's generic defense against an operator choosing a custom request
subject that a JetStream stream captures.

The logical port name remains `tool.list`. The address becomes `discovery.tool.list`, and the kind becomes
`nats-request`. Those are separate facts: callers and operators use the logical name to identify the capability, while
NATS clients use the resolved subject to address it.

## Adopter seam

| Adopter | Must know | If they do nothing | Discovery | Ideal bill |
|---|---|---|---|---|
| Default-config operator | Nothing beyond upgrading the complete framework configuration. | The component serves discovery at the new safe default. | Component guide and migration note. | No stream-overlap reasoning or compatibility switch. |
| Discovery client | Requests move from `tool.list` to `discovery.tool.list`. | Requests to the former default receive no framework response. | Adopter note and migration note. | One canonical address, no probing or fallback. |
| Custom-subject operator | Preserve logical name `tool.list`, kind `nats-request`, and the chosen subject. | A legacy `nats` override fails startup; a same-kind override remains authoritative. | Startup error and migration note. | No framework repair of intent. |
| Stream operator | Cover `tool.execute.>` and `tool.result.>`, not the whole `tool.>` namespace. | The new default remains outside those explicit stream families. | Agentic component guide. | No knowledge of request/reply races. |
| Component integrator | Read the resolved `tool.list` request port. | Runtime subscribes exactly once to its resolved subject. | Port catalog and component startup error. | No hidden default, alias, or second responder. |

## Decision A: one request/reply port and one resolved subject

`agentic-tools` SHALL declare one input with logical name `tool.list`, kind `nats-request`, and default subject
`discovery.tool.list`. The runtime SHALL locate that logical port, require request/reply facts, resolve its one subject,
and subscribe only to that subject.

The runtime SHALL NOT separately subscribe to `tool.list`, synthesize a fallback when resolution fails, or serve both
the configured and default subjects. If an operator explicitly chooses `tool.list` as a same-kind custom subject, that
single resolved address is permitted; it is operator configuration, not a framework compatibility alias.

## Decision B: kind mismatch is a startup failure

An override may replace the subject only while retaining kind `nats-request`. An override with kind `nats` is not an
equivalent spelling: it loses the request/reply contract carried by the port model. Component startup SHALL fail with
an actionable error naming logical port `tool.list`, expected kind `nats-request`, and the observed kind.

The framework SHALL NOT convert, repair, or silently accept the old kind. Failing early makes the breaking migration
visible and leaves no deployment-dependent interpretation.

## Decision C: narrow stream guidance, not a generic overlap guard

The live AGENT stream example SHALL cover `agent.>`, `tool.execute.>`, and `tool.result.>`. It SHALL NOT recommend
`tool.>`. The discovery default is outside those tool execution/result families.

This removes the shipped collision but does not prove that every operator-defined custom discovery subject is safe.
Issue #810 remains parked for that generic class. No provisioning guard, declared request-subject registry,
publish-ack decoder, or exported subject inventory lands in this change.

## Decision D: exact post-Foundation-B target amendment

The frozen Foundation-B worklist remains immutable historical input. The control correction SHALL retire exactly the
frozen Go identity `go:processor/agentic-tools/config.go#L146C3` / `tool.list|NATSPort` and add exactly the current
`processor/agentic-tools/config.go` identity `tool.list|NATSRequestPort`.

The amendment SHALL prove one retirement, one addition, one-for-one cardinality, exact frozen path/name/kind/enclosing
membership, path-local expected AST identity, and explicit total accounting with no net count change. It SHALL NOT
rewrite either frozen TSV, alter the existing graph-query amendment, or introduce a general amendment abstraction for
two heterogeneous cases.

This is test-local target interpretation. It adds no runtime subject, port, registry, decoder, export, or adopter
surface.

### Decision D.1: version-2 current-census amendment

The version-2 artifact `service/testdata/message_logger_subject_census.json` separates frozen authority from
mechanically current targets. Its authority layer is immutable for this change:

- `version` remains `2`;
- `baseline_sha` remains the accepted Foundation-B baseline;
- the complete owner-approved Slice C `ruling` remains byte-equivalent; and
- the ordered 21-configuration `scope` remains byte-equivalent.

The current production census contains nine shipped `agentic-tools` instances. All nine inherit the component's
default `tool.list` input, and the shipped configuration search contains zero explicit `tool.list` rows. Moving the
default from `NATSPort` to `NATSRequestPort` therefore changes only two mechanically current `added_kinds` scalars:

- `nats_inputs`: `18` to `9`; and
- `nats_request_inputs`: `9` to `18`.

Every other artifact field SHALL remain byte-equivalent. This is the same bounded artifact-maintenance class used by
#920, commit `1db4c39e`, which updated mechanically current census targets while preserving the version-2 authority
layer. It is not a new Slice C ruling, scope change, baseline rewrite, configuration edit, or runtime behavior.

The promoted `openspec/specs/message-logger/spec.md` still carries aggregate totals from before #920. That adjacent
documentation drift is outside this runtime/current-count amendment and requires a separate documentation-truth
correction. This change SHALL NOT edit or silently reinterpret that promoted specification.

#### Current-census amendment conformance

| Approved amendment | Result | Code evidence | Test evidence | Artifact evidence | Deviation |
|---|---|---|---|---|---|
| Preserve version-2 authority and move exactly nine inherited agentic-tools inputs from `nats_inputs` to `nats_request_inputs`, changing no other field. | CONFORMS — focused/full service gates green; final independent correction review `APPROVE` | `processor/agentic-tools/config.go:124-133` declares the inherited `tool.list` `NATSRequestPort`; the shipped-config census at `service/message_logger_census_test.go:82-123` proves the fixed 21-config population. | `service/message_logger_census_test.go:124-182,248-353` recomputes and compares version, baseline, ruling, scope, totals, kinds, and affected configs. | `service/testdata/message_logger_subject_census.json:1-74` changes only `added_kinds.nats_inputs` `18→9` and `added_kinds.nats_request_inputs` `9→18`. | None |

## Decision E: discovery startup is fail-closed and atomic

Before allocating a runtime subscription, `agentic-tools` SHALL resolve and validate the discovery request port and
collect and validate every JetStream input fact needed for the attempt. Startup then allocates in two phases:

1. Subscribe to the resolved discovery request subject.
2. Start the required local JetStream consumers.

If discovery subscription fails, startup SHALL return a transient error with `Component` / `Start` /
discovery-subscribe context and preserve the underlying cause for `errors.Is`. It SHALL leave no discovery
subscription, local consumer, or tracked startup resource and SHALL leave `running=false`.

If a later consumer setup fails, startup SHALL unsubscribe discovery, stop every local consumer started by that
attempt, clear the tracked discovery subscription and consumer state, leave `running=false`, and return the setup
error. Rollback SHALL NOT delete a durable consumer or its position. A subsequent `Start` after either failure SHALL
begin from clean local state and may succeed normally.

One lock-internal cleanup path SHALL own both failed-start rollback and normal stop resource accounting. This remains
component-owned execution state under the orchestration boundary: it introduces no rule, workflow, lifecycle entity,
operator-visible phase, retry, recovery mode, readiness state, alias, or fallback.

## Rejected alternatives

| Alternative | Rejection |
|---|---|
| Keep `tool.list` and add a stream-overlap guard | Reopens the broader #810 program instead of taking the approved bounded breaking cutover. |
| Answer both `tool.list` and `discovery.tool.list` | Preserves the captured legacy route, adds two responders, and makes migration completion unknowable. |
| Silently repair kind `nats` | Hides stale configuration and makes declared port facts disagree with runtime behavior. |
| Probe the new address, then fall back to the old one | Moves ambiguity into every client and recreates a compatibility contract with no retirement signal. |

## Proof and issue disposition

The completed pre-correction proof established the new default, same-kind override, wrong-kind startup failure, and
exactly-one-subscription behavior. It does not prove the reviewed Foundation-B amendment or revised startup ordering.
The correction adds focused proof for exact target accounting, discovery-subscription failure, later consumer setup
failure, atomic local rollback, durable consumer preservation, and clean subsequent startup.

Verification SHALL include focused control/runtime/config race tests, frozen-record no-diff checks, the full race
suite, and fresh breaking integration runs:

- `task e2e:crud-tools`, proving discovery at `discovery.tool.list` returns a nonempty effect-bearing catalog; and
- `task e2e:agentic`, proving the narrowed execution/result stream families preserve live agent tool execution.

Both fresh E2Es and every focused/full race gate are green. A reviewer independently reran
`go test -count=1 -race ./...` green and returned final verdict `APPROVE`. The only review note is the nonblocking,
pre-existing promoted message-logger aggregate-total drift already deferred to a separate documentation-truth
correction. The change is merge-ready; the merge has not occurred. #810 remains parked with no partial
guard/registry/decoder/export claim. Candidate selection, tag authorization, publication, and #827 are outside this
change and remain unresolved.

Retained local evidence, not in-tree artifacts:

- crud-tools exited `0`; `/private/tmp/tool-discovery-crud-tools-e2e-escalated.log`, SHA-256
  `fb070c9b014720d7c5eb3224b0003fdd58f1df03b3d465a05256d581fc2ed5a6`, records exact registered/effect-catalog
  proof, `tool_executions=4`, and rule deltas `9/0/3`; and
- agentic exited `0`; `/private/tmp/tool-discovery-agentic-e2e.log`, SHA-256
  `cdbbb54c3bbc38c0807d7505dd1295522ace35316330d75c385797ee10b4ba76`, records `tool_executions=1`.

Focused control/config/agentic-tools race and full agentic-tools integration are green. Focused service census and
full service race are green. `task lint`, `go build ./...`, schema generation with no `schemas/` or `specs/` diff,
and `go test ./test/contract/...` are green. Integration tests completed outside the sandbox for all packages. Frozen
TSV diffs are empty, strict OpenSpec validation is 42/42, and the final diff check is green.

## Dated supersession note

As of 2026-08-11, this approved target and `docs/operations/migration-tool-discovery-default.md` supersede live
guidance that names `tool.list` as the default discovery subject or recommends `tool.>` stream coverage. Historical
ADRs, remap evidence, archived changes, and the frozen pre-v1 program remain unchanged as records of their time.

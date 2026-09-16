# R7 verdict wire contract — review draft

Base: `c347eff487f50b93bc338d764f43ef5b5ea5e133`, with preserved R6/R7/R8 WIP.

Accepted inventory appendices remain unchanged:

- `inventory-r7-verdict-wire-2026-09-15.md`, SHA-256 `e47d02766f636eaf49ed6dea560906ce4488457c113912c5cab1daaf1c4bfcaa`, 92 pins.
- `inventory-r7-verdict-wire-architect-audit-2026-09-15.md`, SHA-256 `a2ab756776ba073fce894808bd95bbd7a2a63baef84affdd716201cff796e7f9`, 31 pins.

These complete, verbatim artifacts accompany this draft for review; they are not replaced by its summary. Independent review returned **INVENTORY PASS**.

Authority: [accepted wire boundary](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5677581095), [bounded intake](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5677867478).

## Options and recommendation

| Option | Cost and consequence |
|---|---|
| Leave current behavior | No edit; raw publish remains dependent on the fallback, and registered verdict context disappears during dispatcher re-decoding. |
| Wrap the producer, retain byte-based dispatch | Small producer edit, but preserves duplicate interpretation; removing raw fallback alone does not fix dispatcher context loss. |
| Wrap at the existing action owner and pass existing VerdictPayload | Changes one existing exported method and its measured callers; removes byte re-decoding and makes required-correlation validation explicit. |

Recommend the third option as the complete bounded wire correction. A producer-only checkpoint is possible, but does not complete this contract.

## Exact implementation contract

### Producer: existing `executePublish`

After existing subject/property substitution and wrapper construction, classify the resolved destination against `agent.toolcall.approved.>` and `agent.toolcall.rejected.>` using existing `flowgraph.SubjectCovers(family, subject)`.

For either family, place the **entire existing wrapper** in `message.NewGenericJSON(payload)`, wrap with `message.NewBaseMessage(..., "rule_engine")`, and marshal that envelope. Preserve `entity_id`, `subject`, `timestamp`, `source`, `properties`, optional `related_id`, and property value types. Do not flatten properties or manufacture correlation.

Continue through the existing publisher and its reviewed R8 transport classification. Outside these families, `publish` retains its current wire behavior. Existing `approve` keeps its registered envelope, correlation echo, configured subject and audit behavior; `deny` keeps its structural short-circuit and audit behavior. This does not redesign absent-publisher or source-settlement behavior.

### Consumer and exported dispatcher

Replace only the existing method:

```go
HandleVerdict(payload VerdictPayload) (natsclient.DeliveryDecision, error)
```

`VerdictPayload` remains the existing exported struct. `Propose`, `Mode`, constructor, setter and getter signatures remain unchanged. Adapt all five implementations and 17 measured references.

The private verdict handler receives the **actual delivered subject** as a plain string. The existing native callback obtains `msg.Subject()` before invoking business work; native messages and settlement methods remain inside the callback. Signal and approval business-handler signatures remain unchanged; necessary private setup adapters carry or ignore the string.

The existing verdict decoder:

1. Calls the configured `message.Decoder` once.
2. Explicitly validates the decoded BaseMessage/payload.
3. Requires `*message.GenericJSONPayload`.
4. Projects its Data through the existing VerdictPayload projection, reporting malformed correlation/container fields; optional Reason/RuleID follow the diagnostic projection rules below.
5. Uses one private normalization/validation implementation, then checks the actual subject against the normalized decision and execution identity.

Delete the raw-map fallback. Pass the resulting `VerdictPayload` directly to the dispatcher. Audit and enforce read that value; neither marshals nor unmarshals it.

Built-in dispatcher methods also call the **same** normalization/validation implementation for direct typed callers. This is one interpreter with multiple callers, not a claim that normalization executes only once. There is no public “normalize first” helper or validated-status machinery.

### Required fields, conflicts and subject treatment

The normalized required set is `Decision`, `LoopID`, `RequestID`, `ExecutionID`, and `ProposalFingerprint`. These lower the fields already named by the active governance correlation requirements.

For each required field, accept the existing top-level or properties representation. Empty top-level values may take the corresponding properties value. Two supplied nonempty representations must agree. Required values must be strings and nonempty; decision must be `approved` or `rejected`. ExecutionID must be a concrete NATS subject token, without dots, whitespace or wildcard tokens.

Missing or malformed required data, malformed envelope and unsupported registered payload type **Terminate**. Conflicting supplied correlation **Quarantines**. Never fill missing fields from the subject, process waiter or current proposal.

`CallID` retains its existing optional status; conflicting supplied top-level/properties CallID is a correlation
conflict. `Reason` and `RuleID` remain optional. Normalize each diagnostic independently: use its nonempty
top-level string, otherwise its string value in Properties, otherwise empty. Thus registered publish verdicts
retain `properties.reason` and `properties.rule_id`, while approve verdicts retain their top-level values.
Different diagnostic values at the two locations follow this precedence; they are not correlation conflicts.
Preserve the existing no-reason behavior and never modify the caller's Properties map.

For a structurally valid payload, the actual subject must equal:

```text
agent.toolcall.<Decision>.<ExecutionID>
```

A conflicting subject Quarantines. The Data wrapper's `subject` is preserved metadata and does not replace the actual delivered subject. Retire the old private subject helper's payload-fallback interpretation if it becomes unused; introduce no new public subject API.

After validation, existing disabled/audit/enforce dispositions remain: valid disabled/audit observations ACK; enforce uses its existing waiter delivery, missing-waiter Retry and full-waiter Quarantine behavior.

This establishes structural wire consistency. It does **not** establish agreement with an originating proposal or supply retained-verdict recovery.

## Small additive spec draft

Append to the existing `agentic-governance` delta under `ADDED Requirements`:

### Requirement: Governance verdicts use one registered wire and typed handoff

Framework rule publications within `agent.toolcall.approved.>` and `agent.toolcall.rejected.>` SHALL use the existing registered `core.json.v1` BaseMessage carrier. The publish action SHALL preserve its complete existing inner wrapper and properties. Approve, publish and deny authoring and audit semantics SHALL remain as specified by their existing owners.

Verdict intake SHALL decode through its configured registry, explicitly validate the decoded message and require GenericJSON. It SHALL NOT accept a raw-map fallback or reserialize/redecode a verdict between intake and the existing GovernanceDispatcher.

The existing dispatcher SHALL accept VerdictPayload directly. One private implementation SHALL normalize and validate both wire-derived and direct typed inputs. No additional public payload, normalization API or serializer SHALL be introduced.

Decision, LoopID, RequestID, ExecutionID and proposal fingerprint SHALL be nonempty and correctly typed.
Decision SHALL be approved or rejected; ExecutionID SHALL be one concrete NATS subject token.
Conflicting supplied correlation SHALL Quarantine; missing or malformed required input SHALL Terminate.
Optional CallID and diagnostic context SHALL NOT become required. Missing correlation SHALL NOT be inferred
from routing or process state.

Reason and RuleID SHALL each use the nonempty top-level string, otherwise the corresponding string in
Properties, otherwise empty. Diagnostic disagreement SHALL follow that precedence and SHALL NOT be classified
as a correlation conflict. Normalization SHALL NOT modify the supplied Properties map. For every valid verdict
represented equivalently as registered wire input and direct VerdictPayload input, normalization SHALL produce
the same decision, correlation and optional diagnostic context. Refused input SHALL NOT mutate a waiter.

Actual delivered subject SHALL equal the normalized decision/execution subject. A conflicting subject SHALL Quarantine before dispatcher effects. Valid verdict context SHALL reach the dispatcher without carrier-dependent loss. These rules do not replace the separately required proposal-match and retained-verdict proofs.

#### Scenario: Both rule authoring paths survive the production codec

- **WHEN** approve or publish emits a valid verdict in either protocol family
- **THEN** production registry decoding exposes the expected inner fields
- **AND** publish retains its entire wrapper/properties and dispatcher context survives.

#### Scenario: Invalid or conflicting input cannot reach a waiter

- **WHEN** wire or direct typed input lacks required correlation, has malformed required fields, or contains conflicting correlation
- **THEN** it receives the specified Terminate or Quarantine disposition before waiter mutation
- **AND** omitted optional context alone does not refuse it.

#### Scenario: Transport identity cannot repair or override payload identity

- **WHEN** a verdict payload lacks required identity or disagrees with the actual subject
- **THEN** intake refuses with the specified disposition
- **AND** neither subject nor process state supplies a replacement value.

These clauses are the test-property home: carrier preservation, required-field refusal, conflict refusal, subject equality, optional-context tolerance, and direct/wire consistency.

## Minimal TDD and file scope

Production changes belong in `processor/rule/actions.go`, `processor/agentic-loop/component.go`, and `processor/agentic-loop/governance_dispatcher.go`; update the existing handler API comments where necessary.

Use existing action, dispatcher, component/delivery, execution-identity and replacement tests:

1. RED: capture actual publish bytes for both families; production decoder plus explicit validation must recover the complete wrapper, nested properties and substituted values. Retain outside-family and approve controls.
2. RED: registered approve/publish inputs reach audit/enforce with their effective Reason and RuleID.
   Cover top-level values, properties-only values, empty-top-level fallback, differing diagnostics following
   top-level precedence, and omission. Assert that diagnostic disagreement is not a correlation refusal.
3. RED: table-test required-field absence/types, duplicate correlation conflicts, raw/unregistered/wrong-type envelopes, subject disagreement, and equivalent direct typed inputs. Assert zero waiter mutation on refusal.
4. Adapt the five implementations and 17 references; reuse missing/full waiter and mode controls.
5. Exercise actual installed callbacks for both verdict ports with real NATS and the observed subject. Retain existing PubAck/settlement controls; this is not retained-verdict recovery proof.

Add one Go native fuzz target, `FuzzGovernanceVerdictBoundary`, beside the existing verdict tests.
Cite `agentic-governance / Governance verdicts use one registered wire and typed handoff`.
Exercise arbitrary subject/field strings, Properties JSON and registered-envelope JSON through the existing
decoder, normalizer and dispatcher boundary. Seed valid approve and publish representations, properties-only
diagnostics, empty-top-level fallback, diagnostic disagreement, missing/conflicting correlation and malformed
envelopes. Assert the specified refusal with no waiter mutation, unchanged caller Properties, and equal
normalized decision/correlation/diagnostic context for valid equivalent wire/direct inputs. Reuse existing
fixtures and waiter controls; add no API or testing framework.

Run the fuzz seed corpus with the affected package race tests, then a bounded native fuzz campaign.
Run focused real-NATS callback controls separately. The breaking change requires the relevant agentic E2E
green before landing; final R11 verification remains separate.

## Migration, limits and approval

Normal rule authors keep `publish`/`approve`/`deny` and property authoring. Raw external publishers use the registered envelope; raw subscribers decode its payload. Known SemSpec configurations still require their own owner to replace loop/call destinations with execution destinations and echo the required correlation fields. Record exact instructions in the SemStreams migration document; make no sister edits.

Direct Go callers replace routing strings/bytes with an existing VerdictPayload value. Implementers change the existing method accordingly; compile errors expose those adaptations. Existing normal component construction adds no adopter step.

Already authorized: the two-family registered carrier, preserved authoring/wrapper, raw fallback removal, elimination of redundant byte decoding, and strict existing correlation.

**Exact owner acceptance after independent design review:** change `GovernanceDispatcher.HandleVerdict` to accept one `VerdictPayload` value with the unchanged decision/error result. This changes an existing exported method rather than adding an API family.

Retained-verdict lookup, exact proposal matching, source settlement in #1311, and full R7 replacement proof remain open. No new recovery mechanism, bucket, runtime, payload family or general publisher migration is proposed. No shared decision skill is newly triggered: this slice changes an existing carrier/handoff, with no new communication path, query or orchestration layer.

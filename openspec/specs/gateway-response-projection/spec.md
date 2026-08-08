# gateway-response-projection Specification

## Purpose

`gateway-response-projection` governs the **shape a caller sees** when the graph gateway turns an
internal NATS query reply into a GraphQL field value: which wrapping is removed, on what evidence,
and what a consumer may rely on across query families.

It owns one decision and its consequences. **Envelope removal is decided from the REPLY, never from
the subject that served it.** The families do not partition by envelope usage — `graph.query.summary`
is served by graph-query's own handler and returns the `QueryResponse` envelope, which a
subject-prefix rule left double-nested as `data.graphSummary.data.*` (gh#762). The property is more
general than that instance: query handlers proxy, forwarding downstream replies verbatim, so a reply
enveloped by one component can surface under another family's subject. A subject-keyed rule is
therefore not merely fragile, it is unsound, and is one downstream change away from being wrong
again.

Two constraints follow, and both exist because the *failure direction* is asymmetric — wrongly
unwrapping is worse than the defect being fixed, because it silently removes a nesting level from a
real payload. Detection admits only the **exact closed key set**, never the presence of `data` alone.
And the envelope's key set is **reserved**: a non-envelope response type may not occupy it, which
converts the one risk detection cannot defend against — a type introduced later that coincidentally
matches — from a latent runtime defect into a reviewable contract violation.

A fifth requirement is about how this capability is *verified* rather than how it behaves, and it is
here deliberately: the wrapped and unwrapped shapes both decode cleanly into any permissive target,
so a test that decodes before asserting passes under the defect and under the fix alike. That is why
gh#762 survived weeks of green stages, and why assertions here are over raw wire bytes.

**What it does NOT cover.** The envelope type itself (`graph.QueryResponse`) and what producers put
in it — producers were untouched by the change that seeded this spec. Query *strategy* belongs to
`graph-query`; index storage to `graph-index`. Error delivery is out of scope: ADR-060 settled that a
reply is either a success body or a classified error on the err channel, so there is no in-body error
field for this projection to interpret. Routing — which GraphQL document reaches which subject — is
the gateway's, not this capability's; a subject that no component serves is a routing gap (gh#784),
not a projection one.
## Requirements
### Requirement: The gateway MUST decide envelope unwrapping from the response, never from the subject
The GraphQL gateway MUST determine whether a NATS query response carries the `graph.QueryResponse`
envelope by examining the RESPONSE, and MUST NOT decide it by matching the subject against a prefix or
an enumerated subject list.

Subject enumeration is not merely fragile here, it is unsound. Query handlers proxy: the graph-query
component's semantic and spatial handlers forward to downstream subjects and return the downstream
response verbatim, so an envelope produced under `graph.index.query.*` can surface under a
`graph.query.*` subject. Whether a given response carries the envelope is therefore a property of the
response and not of the subject that produced it, and any subject-keyed rule is wrong for some
response no matter how the list is maintained.

The consequence of the enumerated form is that a new query family inherits the WRONG shape by
default, silently, and is discovered by a consumer rather than by a test.

#### Scenario: A newly added query family is projected correctly with no gateway change

- **GIVEN** a new query subject whose handler returns a `QueryResponse` envelope
- **AND** the gateway has not been edited to name that subject or its prefix
- **WHEN** a caller reads the corresponding GraphQL field
- **THEN** the payload is projected unwrapped, with the envelope's fields at the top level
- **AND** no `data.<field>.data.*` nesting is present

#### Scenario: A proxied response is projected by its own shape

- **GIVEN** a handler that forwards to a downstream subject and returns that response verbatim
- **WHEN** the downstream response carries the envelope while the outer subject is in a different
  family
- **THEN** the envelope is unwrapped on the basis of the response
- **AND** the outer subject's name does not affect the decision

### Requirement: Envelope detection MUST be conservative, admitting only the exact envelope shape
A response MUST be treated as the envelope only when its top-level JSON object carries both `data` and
`timestamp` and every one of its keys is drawn from the envelope's own field set
(`data`, `request_id`, `timestamp`); any other response MUST be passed through byte-for-byte
unchanged.

Detecting on the presence of `data` alone is prohibited. A payload that legitimately carries a
top-level `data` field would be stripped of a nesting level, which converts this capability's
cosmetic defect into silent data loss — a strictly worse failure than the one being fixed. `timestamp`
carries no `omitempty` tag and is therefore always present on a real envelope, so the conjunction
costs nothing to require. Requiring the key set to be CLOSED additionally means a payload bearing
`data` and `timestamp` alongside other fields is not an envelope and is left alone.

A response that fails detection MUST NOT be reported as an error on that basis; failing to be an
envelope is the ordinary case for the families that do not use one.

#### Scenario: A payload with a legitimate top-level data field is not unwrapped

- **GIVEN** a response whose own payload carries a top-level `data` field and no `timestamp`
- **WHEN** the gateway projects it
- **THEN** the payload is passed through with its nesting intact
- **AND** no field is removed from the caller's view

#### Scenario: A collection response with its own envelope is untouched

- **GIVEN** a response shaped `{entities, next_cursor}`, which is a distinct type and not the query
  envelope
- **WHEN** the gateway projects it
- **THEN** envelope detection does not claim it
- **AND** its own established handling applies unchanged

#### Scenario: An envelope-shaped object bearing extra keys is not an envelope

- **GIVEN** a response carrying `data`, `timestamp`, and at least one key outside the envelope's field
  set
- **WHEN** the gateway projects it
- **THEN** it is passed through unchanged

### Requirement: The envelope's key set MUST be reserved against collision by other response types
A response type served through the gateway's projection path MUST NOT consist solely of keys drawn
from the envelope field set (`data`, `request_id`, `timestamp`) unless it IS the envelope. The
envelope's shape is reserved, and a new or modified response type that would occupy it MUST be given
a distinguishing field or a different field name instead.

Detection on a closed key set is exact for every response type that exists when it is written, and
carries exactly one residual risk forward: a response type introduced LATER that happens to consist
only of envelope keys would be detected as an envelope and unwrapped, silently removing a nesting
level from a payload nobody intended to wrap. Detection cannot defend against this on its own, because
by construction such a response is indistinguishable from the envelope on the wire.

Stating the reservation converts that residual from a latent runtime defect into a **reviewable
contract violation** — visible when the offending type is written, in review, rather than when a
consumer loses a field in production. This is also what earns this capability its own home: an
unowned contract is one nobody checks, and that observation applies forward to types not yet written,
not only backward to the defect that prompted the capability.

#### Scenario: A new response type may not occupy the envelope's shape

- **GIVEN** a proposed gateway response type whose marshalled form consists only of keys drawn from
  `{data, request_id, timestamp}`
- **AND** that type is not the query-response envelope
- **WHEN** it is reviewed
- **THEN** it is rejected as a contract violation
- **AND** the remedy is a distinguishing field or a different field name, not a detection exception

#### Scenario: A reserved-shape collision is caught before it ships

- **GIVEN** a change introducing such a type
- **WHEN** the projection path's response types are checked against the reservation
- **THEN** the collision is reported against the new type
- **AND** it is not left to surface as a missing field in a consumer

### Requirement: The envelope MUST be removed exactly once, never iteratively
The gateway MUST unwrap at most one envelope layer per response, and MUST NOT re-test the unwrapped
payload in order to unwrap again.

One envelope is applied by the producer, so exactly one is removed at the projection boundary.
Unwrapping while the payload continues to look like an envelope would make the number of layers
removed depend on user data: a payload whose own contents happened to match the discriminator would be
silently flattened, and the same query would project differently for different entities.

#### Scenario: Envelope-shaped payload data survives projection

- **GIVEN** a `QueryResponse` whose `data` is itself an object carrying `data` and `timestamp`
- **WHEN** the gateway projects the response
- **THEN** exactly one layer is removed
- **AND** the inner object is delivered to the caller intact

### Requirement: The projected shape MUST be asserted against raw wire bytes, not a decoded struct
The projected response shape MUST be verified by assertions over the raw JSON keys of the served
payload, and MUST NOT be verified solely by decoding into a struct.

The wrapped and unwrapped shapes both unmarshal cleanly into any permissive target, so a struct-level
assertion passes under either one. A test that decodes before asserting cannot distinguish the defect
from the fix, and reports green in both states.

Verification MUST cover representative subjects from EVERY query family routed through the gateway's
response path, not only the family in which the defect was first observed, and MUST assert the
ABSENCE of a repeated `data` hop rather than only the presence of expected fields.

#### Scenario: The shape gate fails against the unfixed gateway

- **GIVEN** the response-shape assertions and a gateway that does not detect the envelope
- **WHEN** the gate runs
- **THEN** it fails
- **AND** its falsifiability is therefore demonstrated rather than assumed

#### Scenario: A repeated data hop is caught rather than read through

- **GIVEN** a projected response carrying `data.<field>.data.*`
- **WHEN** the shape assertions run
- **THEN** the repeated hop is reported as a failure
- **AND** the assertion does not pass merely because the expected leaf values are reachable

### Requirement: GraphQL is the sole public trajectory application surface

Graph-gateway SHALL expose versioned trajectory reads through GraphQL. Direct agentic-loop trajectory HTTP handlers
and OpenAPI paths SHALL be absent. The gateway SHALL reach agentic-loop only through typed internal NATS
request/reply; public callers SHALL NOT need KV keys, bucket names, Store instances, evidence keys, graph state, or
NATS subjects.

The GraphQL production path SHALL expose strict cursor-paged fact metadata and `message.StorageReference` values. It
SHALL preserve agentic-loop's `next_cursor`, page-local `observed_totals`, and `terminal_observed` truth. Neither the
typed internal request nor GraphQL SHALL accept an evidence-hydration option or carry an evidence body. Graph-gateway
SHALL NOT become a trajectory Store borrower. Authorized evidence retrieval remains a separate registered-Store
operation outside this public query contract.

#### Scenario: public trajectory reads use GraphQL

- **GIVEN** a public client requests a loop trajectory
- **WHEN** the production gateway serves it
- **THEN** the client receives the versioned GraphQL response
- **AND** no direct agentic-loop trajectory HTTP/OpenAPI route exists
- **AND** the client supplies no KV, NATS, Store, evidence-key, or graph detail

#### Scenario: evidence references cross the gateway without hydration

- **GIVEN** a returned fact with a valid registered-Store evidence reference
- **WHEN** GraphQL projects the trajectory page
- **THEN** it exposes the reference metadata and no evidence body
- **AND** neither graph-gateway nor agentic-loop borrows a Store for the query

#### Scenario: missing evidence does not invent content

- **GIVEN** a visible fact whose reference is missing or unverifiable
- **WHEN** GraphQL projects the trajectory page
- **THEN** the recorded capture/reference status remains visible without an invented body
- **AND** the page's other facts and observed totals remain available

### Requirement: Trajectory projection is always observed-only

Every GraphQL trajectory response SHALL expose `coverage: observed` and `observed_totals` derived only from returned
visible facts. It SHALL NOT expose a complete/partial/unknown coverage classification, seal, manifest, membership
proof, attempted/recorded/gap count, or any equivalent completeness promise.

When no `loop.terminal` fact is returned, the response SHALL set `terminal_observed: false`. When one or more are
returned, it SHALL set `terminal_observed: true` and preserve every terminal fact in causal/attempt order. Terminal
presence SHALL NOT change coverage or imply that earlier facts/evidence are complete.

#### Scenario: a visible terminal observation does not upgrade coverage

- **GIVEN** a trajectory response containing one or more `loop.terminal` facts
- **WHEN** GraphQL projects the response
- **THEN** `terminal_observed` is true and all terminal observations remain ordered
- **AND** `coverage` remains `observed`
- **AND** totals remain `observed_totals`

#### Scenario: terminal absence is not inferred from adjacent surfaces

- **GIVEN** returned facts contain no `loop.terminal` observation
- **WHEN** GraphQL projects the response
- **THEN** `terminal_observed` is false
- **AND** no outcome is inferred from `COMPLETE_`, terminal events, cache, process memory, or graph state

#### Scenario: projection schema has no completeness machinery

- **WHEN** the GraphQL schema and projection response types are inspected
- **THEN** they contain no terminal seal, completeness status, audit counters, manifest, membership proof, or
  reconstructed gap claim

### Requirement: Graph prefix continuation survives every projection

Graph prefix queries SHALL preserve the typed internal `PrefixQueryResponse` and its `next_cursor` through
graph-gateway. The breaking GraphQL field SHALL accept `prefix`, `limit`, and `cursor`, and return
`EntityPage { entities, next_cursor }`. The former list-only `[Entity]` response SHALL be absent; no alias or silent
cursor discard is permitted.

#### Scenario: a prefix page continues without loss

- **GIVEN** a graph prefix response containing entities and a non-empty `next_cursor`
- **WHEN** graph-gateway projects it through GraphQL
- **THEN** the client receives the same page entities and continuation token
- **AND** a subsequent request can supply that token through the typed query path

#### Scenario: list-only prefix projection is removed

- **WHEN** the GraphQL schema and gateway adapters are inspected
- **THEN** prefix queries return `EntityPage` rather than `[Entity]`
- **AND** no compatibility alias discards `next_cursor`

### Requirement: The existing typed agentic request family owns trajectory routing

Graph-gateway SHALL retain exactly its three required query-family outputs. Its existing `agentic_queries` output SHALL
be a required `nats-request` port with subject family `agentic.query.*` and interface `agentic.query` v1.
Agentic-loop's `trajectory_query` input SHALL be a required `nats-request` port with exact subject
`agentic.query.trajectory` and the same interface/version.

The gateway SHALL derive the exact trajectory request through its existing `querySubject(agentic_family,
"trajectory")` behavior. Agentic-loop SHALL subscribe through its declared input rather than a hard-coded literal
bypass. No fourth gateway output, platform-derived owner, alias, dual subscription, or compatibility shim SHALL be
added.

An isolated deployment MAY supply complete matching overrides on both components, such as
`tenant.agentic.query.*` and `tenant.agentic.query.trajectory`. Each override SHALL repeat kind, required state, and
interface type/version. Mismatched pairs SHALL fail validation before subscription or request handling.

#### Scenario: canonical agentic family resolves the exact trajectory route

- **GIVEN** graph-gateway's canonical `agentic.query.*` output and agentic-loop's canonical exact input
- **WHEN** the gateway resolves the trajectory query subject
- **THEN** it requests `agentic.query.trajectory`
- **AND** agentic-loop receives it through the declared `trajectory_query` input
- **AND** graph-gateway still has exactly three outputs

#### Scenario: interface or paired override mismatch fails validation

- **GIVEN** the gateway family and loop exact input disagree on subject family, kind, required state, interface type,
  or interface version
- **WHEN** port/config validation runs
- **THEN** validation fails before routing is installed
- **AND** no alias, subject inference, literal bypass, or fourth output repairs the mismatch

#### Scenario: explicit paired isolation remains possible

- **GIVEN** complete matching gateway and agentic-loop overrides under an isolated subject prefix
- **WHEN** both declarations validate
- **THEN** `querySubject` resolves the isolated family to the isolated exact input
- **AND** no platform identity is used to derive either subject


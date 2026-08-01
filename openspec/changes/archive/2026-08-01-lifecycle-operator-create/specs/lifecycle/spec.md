## ADDED Requirements

### Requirement: A created instance MUST be reachable by every surface that can list it

Lifecycle creation MUST refuse an entity ID that does not match the workflow's
declared `EntityIDPattern`, and MUST refuse it before any KV write. A create that
commits an out-of-pattern ID produces an instance that is readable by direct
`Get` but invisible to `List` and `Watch` — which filter by the same pattern —
and unreclaimable, because reclamation validates the pattern and refuses. The
surface would then report a successful birth that the same surface cannot
discover or delete.

Owner-lease enforcement MUST NOT be relied on to cover this: an out-of-pattern
write is *unclaimed* rather than *stale*, so a lease check passes it through even
under strict enforcement.

The refusal MUST use the same sentinel the reclamation path already uses, and
MUST render as a client error on any operator surface, never as a server fault.

#### Scenario: an out-of-pattern entity ID is refused

- **GIVEN** a workflow whose declared entity-ID pattern is `*.*.lifecycle.gcs.mission.*`
- **WHEN** creation is requested for an entity ID outside that pattern
- **THEN** creation fails with the entity-ID-pattern-mismatch sentinel
- **AND** no entity is written
- **AND** an operator surface renders it as a client error

#### Scenario: an in-pattern entity ID is created and is discoverable

- **GIVEN** a workflow and an entity ID matching its declared pattern
- **WHEN** creation succeeds
- **THEN** the instance is returned by a list of that workflow's instances

### Requirement: A committed birth MUST NOT be reported as a failure

Lifecycle creation MUST report success whenever the write committed durably, even
when the post-write read-back could not be completed. The mutation contract
defines a degraded response as a committed write whose read-back failed and which
callers MUST NOT retry; reporting that as an error causes an operator to retry a
birth that already happened, and the retry then reports a conflict.

The answer returned to the caller MUST be derived from the causal mutation
response rather than from a separate read issued afterwards. A separate read can
fail after a durable commit, and can also observe another writer's later state —
so it answers a different question than "what did this request commit".

Where a degraded commit leaves no projectable state, the surface MUST still
report success and MUST signal the degradation, rather than converting it into a
failure or a conflict.

#### Scenario: read-back fails after a durable commit

- **GIVEN** a creation whose write committed durably
- **AND** whose post-write read-back could not be completed
- **WHEN** the result is reported
- **THEN** the caller is told the creation succeeded
- **AND** the degradation is signalled rather than the request being failed

#### Scenario: the reported state is the state this request committed

- **GIVEN** a successful creation
- **WHEN** the committed state is returned
- **THEN** it is derived from the mutation response for this request

### Requirement: An operator-initiated birth MUST be attributed to the operator

A lifecycle creation initiated through an operator surface MUST record the
operator as the transition source in its audit trail, not the framework. History
recovers provenance from that audit triple, so a framework attribution on an
operator-initiated create writes a false answer into the audit record of the
highest-privilege operation the surface exposes.

The audit attribution MUST be supplied by the caller that knows the provenance,
in the same way the transition lane already supplies it — not defaulted at the
point where the triples are built.

#### Scenario: a birth created through the operator surface

- **GIVEN** an instance created through an operator surface
- **WHEN** its history is read
- **THEN** the first event's source is the operator

#### Scenario: a birth created in-process by the framework

- **GIVEN** an instance created by framework code
- **WHEN** its history is read
- **THEN** the first event's source is the framework

### Requirement: Workflow registration MUST reject a state type that cannot be projected

Registering a workflow MUST fail when a pointer to the declared `Schema` type
does not implement the `Participant` contract. Allocation of a fresh instance
from `Schema` followed by an unchecked conversion to `Participant` happens on
every projection path, so an unconformant Schema is a panic waiting for the
first request that reaches one.

This MUST be validated at registration rather than left to the first request. The
reachability differs sharply by path: the read paths reach the conversion only
for an entity that already exists, while a creation path reaches it with no
precondition beyond a registered workflow and a non-empty body — so on a fresh
deployment the first external request is the trigger.

Registration MUST NOT additionally require that the declared workflow name equal
the name the Schema's own `Workflow()` reports. That equality is a real wiring
invariant, but enforcing it at registration would convert a **documented
observe-only runtime posture into a boot failure**: registering one schema under
a second name is how a partial migration presents a cross-owner overlap, and the
Manager is deliberately non-fatal there so a mid-migration deployment does not
brick. Overlap rejection belongs to the ownership substrate, which refuses the
claim, not to registration, which records it.

The hazard that equality was reaching for is closed instead by binding a create
to the registration the **caller selected**, rather than re-deriving one from the
Participant's own constant. Re-deriving would let a request routed as one
workflow write with another's pattern, transitions, owner token, and audit
predicates — and, where only the alias is registered, fail a valid advertised
route with a false not-found.

#### Scenario: a Schema that does not implement the contract

- **GIVEN** a workflow declaration whose Schema type does not implement Participant on its pointer
- **WHEN** the workflow is registered
- **THEN** registration fails naming the workflow and the offending type
- **AND** no request path can reach an unchecked conversion for that workflow

#### Scenario: a Schema whose reported workflow name disagrees with the declaration

- **GIVEN** a workflow registered under a name that its Schema's `Workflow()` does not report
- **WHEN** the workflow is registered
- **THEN** registration succeeds
- **AND** the ownership substrate, not registration, is what refuses a genuine cross-owner overlap

#### Scenario: a create is routed to an aliased registration

- **GIVEN** a workflow registered only under an alias whose Schema reports a different, unregistered name
- **WHEN** a create is routed to the alias
- **THEN** the write uses the alias registration's pattern, transitions, and audit predicates
- **AND** the create does not fail with not-found

### Requirement: Must-exist lanes MUST NOT create state

Operator state patch and transition MUST require an existing lifecycle-managed
entity and MUST NOT create one. Only the creation lane brings an instance into
being. A lane that silently created state on the way past would make "this
instance exists" unfalsifiable from the operator surface, and would give the same
surface two creation paths with different validation.

This contract MUST be pinned against the production implementation, not against a
substitute that reimplements it. A guard asserted only through a hand-written
double proves the double, and cannot observe a regression in the code that ships.

#### Scenario: patching an absent instance

- **GIVEN** an entity ID with no lifecycle-managed instance
- **WHEN** an operator state patch is applied to it
- **THEN** the patch fails with entity-not-found
- **AND** no instance exists afterwards

#### Scenario: transitioning an absent instance

- **GIVEN** an entity ID with no lifecycle-managed instance
- **WHEN** an operator transition is requested for it
- **THEN** the transition fails with entity-not-found
- **AND** no instance exists afterwards

### Requirement: The operator surface MUST map every condition its callees can raise

Every domain sentinel reachable from an operator route MUST map to a status that
names what happened, and MUST preserve its message. A domain condition rendered
as a generic server fault with a canned message tells an operator that the
service is broken when in fact their request was refused for a knowable reason.

Adding a route to this surface MUST include an audit of the sentinels the new
callee can raise, because a sentinel that was previously unreachable becomes
reachable the moment a route can reach it. Two instances of this shape are known:
a duplicate-create sentinel that was unmapped until a creation route existed, and
an ownership-quiesce sentinel that a creation route makes reachable.

An ownership-quiesce refusal in particular MUST reach the caller intact — it
means another incarnation has taken over and the caller should retry against the
live owner, which is unactionable if reported as an internal error.

#### Scenario: a superseded process attempts a create

- **GIVEN** a process whose ownership has been superseded by another incarnation
- **WHEN** it creates through the operator surface
- **THEN** the response status distinguishes the refusal from a server fault
- **AND** the response carries the reason rather than a generic message

#### Scenario: a request body exceeds the configured limit

- **GIVEN** a configured maximum request-body size
- **WHEN** a request to any body-carrying operator lane exceeds it
- **THEN** the response reports the payload as too large
- **AND** the status matches what the surface's published interface advertises

#### Scenario: a stream upgrade is requested with a non-read method

- **GIVEN** a request carrying the stream-upgrade query parameter
- **WHEN** its method is not a read
- **THEN** it is not routed to the stream upgrade
- **AND** any error response uses the surface's uniform error envelope

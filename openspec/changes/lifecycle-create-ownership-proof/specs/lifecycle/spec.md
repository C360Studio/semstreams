# lifecycle — delta for lifecycle-create-ownership-proof

## MODIFIED Requirements

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

Ownership of a birth is proven ONLY by holding the causal mutation response for
that request. It MUST NOT be reconstructed from stored state — not by re-reading
the entity, and not by comparing the entity's content against what this request
intended to write. Content that two writers can produce identically is not an
identity: concurrent creations of the same instance build the same initial state,
including the same audit stamp, so a content comparison lets the LOSER match the
WINNER's write and report a success for a birth it did not make. Timestamp
precision is not a remedy — wall-clock granularity is coarser than the stamp
format, so the collision is ordinary rather than exotic.

Concurrent creations of the same instance MUST resolve to exactly one success.
Every other concurrent creation MUST be reported as a conflict, never as a
success and never as a degraded success.

An outcome the request cannot prove MUST be reported as unknown: where the
mutation response was never received and non-delivery was not established, the
surface MUST report the transport failure rather than converting it into either a
conflict or a success. The caller resolves an unknown outcome by reading
authoritative state; the framework MUST NOT guess on the caller's behalf.

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

#### Scenario: two concurrent creations of the same instance

- **GIVEN** two creations of the same instance issued concurrently
- **AND** the two build identical initial state, including identical audit stamps
- **WHEN** both results are reported
- **THEN** exactly one is reported as a success
- **AND** the other is reported as a conflict rather than as a success or a degraded success

#### Scenario: an already-existing instance is reported as a conflict, not re-read for ownership

- **GIVEN** a creation whose mutation response reports the instance already exists
- **WHEN** the result is reported
- **THEN** it is a conflict
- **AND** no read of the stored entity is issued to decide whether this request wrote it

#### Scenario: a creation whose outcome cannot be determined

- **GIVEN** a creation whose mutation response never arrived
- **AND** whose delivery to the mutation handler was not disproved
- **WHEN** the result is reported
- **THEN** the caller is told the transport failed
- **AND** the result is neither a success nor a conflict

## ADDED Requirements

### Requirement: A creation MUST NOT be re-sent unless non-delivery is proven

A lifecycle creation MUST NOT be automatically re-sent after a failure that does
not prove the request was never delivered. Creation is not idempotent: a re-send
races the request's own in-flight self, and the second delivery answers
"already exists" for a birth this same request made — a request manufacturing a
conflict with itself, which is then indistinguishable from a real conflict.

The ambiguity is structural rather than exceptional, because the per-attempt
client deadline is shorter than the mutation handler's own deadline: the client
can give up several times over while the handler is still executing the create.

Automatic re-send is therefore permitted ONLY for the failure class that proves
non-delivery — the transport reporting that no responder was subscribed. That
class MUST retain a retry budget sized for mutation-handler cold start, because
a participant creating an instance on a fast boot path can legitimately outrun
the handler's subscription.

The must-exist lanes are deliberately excluded from this restriction: an update
carrying a compare-and-set condition surfaces a duplicate delivery as a revision
mismatch that its caller re-reads and re-validates, and a delete is idempotent at
the handler. Only creation can turn a re-send into a wrong answer.

#### Scenario: no responder is subscribed when the creation is issued

- **GIVEN** a creation issued before the mutation handler has subscribed
- **WHEN** the transport reports that no responder is available
- **THEN** the creation is re-sent within the cold-start retry budget
- **AND** it succeeds once the handler subscribes

#### Scenario: the mutation handler is slower than the per-attempt deadline

- **GIVEN** a creation delivered to a mutation handler that is still executing
- **WHEN** the per-attempt deadline expires
- **THEN** the creation is not re-sent
- **AND** the caller receives the transport failure

#### Scenario: a compare-and-set update is re-sent

- **GIVEN** an update carrying an expected revision
- **WHEN** its response is lost and it is re-sent
- **THEN** the duplicate delivery is reported as a revision mismatch
- **AND** the caller re-reads and re-validates rather than receiving a wrong answer

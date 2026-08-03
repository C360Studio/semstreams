# lifecycle — delta for lifecycle-create-ownership-proof

## REMOVED Requirements

### Requirement: A committed birth MUST NOT be reported as a failure

**Reason**: the title is falsified by its own body. The requirement now has to
say that a concurrent creation which did NOT commit is reported as a conflict,
and that a creation whose outcome cannot be determined is reported as neither a
success nor a conflict — neither of which is "a committed birth reported as a
failure". A title covering only the degraded-commit case invites the next reader
to treat the conflict and unknown-outcome rules as exceptions to it.

Replaced by "Lifecycle creation MUST report what THIS request committed" below,
which carries the original's content unchanged plus the ownership, concurrency
and unknown-outcome rules.

## ADDED Requirements

### Requirement: Lifecycle creation MUST report what THIS request committed

Lifecycle creation MUST report success whenever the write committed durably, even
when the post-write read-back could not be completed. The mutation contract
defines a degraded response as a committed write whose read-back failed and which
callers MUST NOT retry; reporting that as an error causes an operator to retry a
birth that already happened, and the retry then reports a conflict.

The answer returned to the caller MUST be derived from the causal mutation
response rather than from a separate read issued afterwards. A separate read can
fail after a durable commit, and can also observe another writer's later state —
so it answers a different question than "what did this request commit".

Where creation writes a NEW entity, ownership of that birth is proven ONLY by
holding the causal mutation response for that request. It MUST NOT be
reconstructed from stored state — not by re-reading the entity, and not by
comparing the entity's content against what this request intended to write.
Content that two writers can produce identically is not an identity: concurrent
creations of the same instance build the same initial state, including the same
audit stamp, so a content comparison lets the LOSER match the WINNER's write and
report a success for a birth it did not make. Timestamp precision is not a
remedy — wall-clock granularity is coarser than the stamp format, so the
collision is ordinary rather than exotic.

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

**Deviation, recorded rather than implied — the ATTACH branch.** Creation has a
second branch: an entity that already exists WITHOUT lifecycle state has
lifecycle attached to it by a compare-and-set update. That branch does NOT yet
satisfy the ownership rule above. On a revision mismatch it re-reads once and
reports a conflict if a phase triple is present, so when that phase triple is the
one this request just wrote it answers a false conflict for its own birth — the
mirror image of the removed defect, erring conservative where that erred liberal.
It is not corrected here because, unlike the deleted code, that re-read is a real
fence: without it every unrelated concurrent update to the entity would be
reported as a duplicate birth. Closing it needs request identity on the mutation
seam rather than a better comparison, and is tracked as gh#870. Until then the
ownership rule binds the absent→create branch, and the attach branch is known to
answer conservatively.

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

#### Scenario: two concurrent creations of the same absent instance

- **GIVEN** two creations of the same absent instance issued concurrently
- **AND** the two build identical initial state, including identical audit stamps
- **WHEN** both results are reported
- **THEN** exactly one is reported as a success
- **AND** the other is reported as a conflict rather than as a success or a degraded success

#### Scenario: an already-existing instance is reported as a conflict, not re-read for ownership

- **GIVEN** a creation of an absent instance whose mutation response reports the instance already exists
- **WHEN** the result is reported
- **THEN** it is a conflict
- **AND** no read of the stored entity is issued to decide whether this request wrote it

#### Scenario: a creation whose outcome cannot be determined

- **GIVEN** a creation whose mutation response never arrived
- **AND** whose delivery to the mutation handler was not disproved
- **WHEN** the result is reported
- **THEN** the caller is told the transport failed
- **AND** the result is neither a success nor a conflict

#### Scenario: lifecycle is attached to an entity that already exists

- **GIVEN** an entity that exists without lifecycle state
- **WHEN** lifecycle attachment races another writer and the compare-and-set fails
- **THEN** an entity carrying a phase triple is reported as a conflict
- **AND** an entity carrying no phase triple is reported as retryable contention rather than a duplicate birth
- **AND** that conflict is known to be conservative — it does not establish that another writer created it (gh#870)

### Requirement: A creation MUST NOT be re-sent unless non-delivery is proven

A lifecycle creation MUST NOT be automatically re-sent after a failure that does
not prove the request was never delivered. Creation is not idempotent: a re-send
races the request's own in-flight self, and the second delivery answers
"already exists" for a birth this same request made — a request manufacturing a
conflict with itself, which is then indistinguishable from a real conflict.

The ambiguity is structural rather than exceptional, because the per-attempt
client deadline is shorter than the mutation handler's own deadline: the client
can give up several times over while the handler is still executing the create.

Automatic re-send is therefore permitted ONLY for failures that prove
non-delivery — either reported by the transport as having no subscriber, or
decided by the client before the request was handed to the transport at all. That
set MUST include the client's own refusals to send, such as an open circuit
breaker or an unavailable connection: a client-side refusal proves non-delivery
as firmly as a no-subscriber reply, and excluding it discards the cold-start
budget precisely when several creations are contending for the same client.

The permitted classes MUST retain a retry budget sized for mutation-handler cold
start, because a participant creating an instance on a fast boot path can
legitimately outrun the handler's subscription. A refusal that is ALREADY true
when creation is first attempted MAY fail fast instead of consuming that budget;
the budget exists for conditions that arise or resolve during it.

The must-exist lanes are deliberately excluded from this restriction: an update
carrying a compare-and-set condition surfaces a duplicate delivery as a revision
mismatch that its caller re-reads and re-validates, and a delete is idempotent at
the handler. Only creation can turn a re-send into a wrong answer.

#### Scenario: no responder is subscribed when the creation is issued

- **GIVEN** a creation issued before the mutation handler has subscribed
- **WHEN** the transport reports that no responder is available
- **THEN** the creation is re-sent within the cold-start retry budget
- **AND** it succeeds once the handler subscribes

#### Scenario: the client refuses to send partway through the cold-start budget

- **GIVEN** a creation retrying within its cold-start budget
- **WHEN** the client's own circuit breaker opens before a later attempt is sent
- **THEN** the creation continues within its remaining budget rather than failing
- **AND** it succeeds once the breaker closes and the handler has subscribed

#### Scenario: the mutation handler is slower than the per-attempt deadline

- **GIVEN** a creation delivered to a mutation handler that is still executing
- **WHEN** the per-attempt deadline expires
- **THEN** the creation is not re-sent
- **AND** the caller receives the transport failure

#### Scenario: a compare-and-set update is re-sent

- **GIVEN** a phase transition or an operator state patch, each carrying an expected revision
- **WHEN** its response is lost and it is re-sent
- **THEN** the duplicate delivery is reported as a revision mismatch
- **AND** that caller re-reads and re-validates rather than receiving a wrong answer
- **AND** the lifecycle-attach caller is excepted: it re-reads once and reports a conservative conflict (gh#870)

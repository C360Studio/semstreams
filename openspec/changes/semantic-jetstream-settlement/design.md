# Design: semantic JetStream settlement

## Decisions

### D0 — non-default integration trunk and atomic default-branch cutover

`codex/gh759-semantic-settlement` is the integration trunk and the head of PR #1156. PR #1156 alone targets `main`.
PR #1159 and the #1249 implementation PR target the #759 branch and are separately claimed, reviewed, archived, and
squash-merged there.

The #759 branch may temporarily contain both typed and legacy exports only as unmerged staging state. That condition
is never archived as current framework truth and never reaches `main`. The exact caller list is enforced only as a
zero-growth test that shrinks after each staged child merge.

After #1146 and #1249 integrate, #759 removes `ConsumeWithHeartbeat`, proves the exported symbol and production caller
count are zero, and archives the final capability state. The final default-branch squash therefore performs one
greenfield API cutover rather than accepting a compatibility period.

Non-default child merges do not close their issues. PR #1156 declares the complete closing set before implementation
and owner-requested cross-agent review of the final integrated claim set and owns default-branch closure.

### D1 — permanent additive typed API

The permanent typed entry point is:

```go
func ConsumeDeliveryWithHeartbeat(
    ctx context.Context,
    msg jetstream.Msg,
    policy HeartbeatDeliveryPolicy,
) DeliveryResult
```

The established `Consume...With...` vocabulary describes one delivered message without reading as a heartbeat-message
consumer. The permanent name does not change during staging or final cutover. Final current truth exports only this
typed entry point; `ConsumeWithHeartbeat` and `NewDurableHandler` are absent without aliases.

During non-default integration only, a zero-growth branch-staging guard pins the three current caller files and
rejects any new production caller or alias. That list is not an API allowlist, compatibility promise, current
capability, or merge authority. Documentation and examples teach only the permanent typed API.

### D2 — semantic decision and error-last work contract

```go
type DeliveryDecision uint8

const (
    DeliveryDecisionInvalid DeliveryDecision = iota
    DeliveryDecisionAck
    DeliveryDecisionRetry
    DeliveryDecisionTerminate
    DeliveryDecisionQuarantine
)

// DeliveryWork classifies one delivered payload. Attempt is immutable and
// settlement-authority-free. Data is read-only and invocation-scoped; work
// must not retain or mutate it.
type DeliveryWork func(
    context.Context,
    DeliveryAttempt,
    []byte,
) (DeliveryDecision, error)

type InvalidDeliveryDecisionError struct { /* private */ }
func (e *InvalidDeliveryDecisionError) Error() string
func (e *InvalidDeliveryDecisionError) Unwrap() error

type DeliveryWorkPanicError struct { /* private */ }
func (e *DeliveryWorkPanicError) Error() string
func (e *DeliveryWorkPanicError) Unwrap() error
```

ACK plus nil means the owner-defined durable consequence completed. Retry, Terminate, and Quarantine require non-nil
causes. Zero, unknown, and every mismatched decision/error tuple preserve the requested decision in DeliveryResult,
attempt no terminal method, quarantine with `InvalidDeliveryDecisionError`, and require owner stop. A supplied error
remains reachable through that typed error.

A recovered panic synthesizes Quarantine with `DeliveryWorkPanicError`, attempts no terminal method, and requires
owner stop. There is no DeliveryDisposition type or typed constructor family. Existing
`TerminateDelivery(error) error` and `PermanentDeliveryError` remain exact and outside every #759 removal gate.

### D2a — immutable delivery-attempt observation

```go
type DeliveryAttempt struct {
    number uint64
}

func (a DeliveryAttempt) Number() uint64
func (a DeliveryAttempt) MetadataAvailable() bool
func (a DeliveryAttempt) IsRedelivery() bool

type DeliveryMetadataUnavailableError struct { /* private cause */ }
func (e *DeliveryMetadataUnavailableError) Error() string
func (e *DeliveryMetadataUnavailableError) Unwrap() error
```

`DeliveryAttempt` is a value with one private delivery-number field. Private state matches `DeliveryResult` and
prevents inconsistent caller-authored combinations. There is no pointer, setter, exported constructor, or
native-message escape. Number zero is the unavailable zero value; `MetadataAvailable` is `number != 0` and
`IsRedelivery` is `number > 1`.

After runtime policy defense, natsclient calls `msg.Metadata()` exactly once. Metadata error, nil metadata, or
`NumDelivered == 0` prevents Data access and work invocation. The result synthesizes Quarantine with
`DeliveryMetadataUnavailableError`, attempts no heartbeat or terminal method, and sets `OwnerStopRequired`. The
existing owner-private latch and observer stop the exact committed lane outside the callback.

A valid first delivery supplies Number 1 and `IsRedelivery == false`. A valid second or later delivery supplies its
observed number and `IsRedelivery == true`. Redelivery is conservative evidence only: a process may have stopped
before the earlier work call, so it does not prove a prior effect or commit-unknown outcome.

The attempt exposes no `jetstream.Msg`, headers, reply subject, stream or consumer sequence, stream or consumer
identity, settlement method, or mutable state.

### D3 — lease and semantic retry are separate

```go
type DeliveryRetryPolicy struct { /* opaque */ }

func ImmediateDeliveryRetry() DeliveryRetryPolicy
func DelayedDeliveryRetry(delay time.Duration) (DeliveryRetryPolicy, error)
```

Consumer AckWait/BackOff controls server lease and missing-settlement redelivery. Retry policy controls explicit Nak or
NakWithDelay after semantic Retry. Zero policy and nonpositive delay are invalid. Work never sees transport timing.
Built-ins use explicit 30-second delayed retry only for proven pre-effect or durably replayable failures.

### D4 — validate actual acquisition policy

```go
type HeartbeatDeliveryPolicy struct { /* opaque */ }

func ValidateHeartbeatDeliveryPolicy(
    ctx context.Context,
    cfg StreamConsumerConfig,
    heartbeat time.Duration,
    retry DeliveryRetryPolicy,
    work DeliveryWork,
) (HeartbeatDeliveryPolicy, error)
```

Validation is pure and before acquisition/I/O. It rejects nil/ended context, nil work, invalid retry, nonpositive
heartbeat, negative AckWait, nonpositive BackOff entries, an interval too small for positive heartbeat, and heartbeat
greater than half effective interval. Equality is allowed. Effective interval is shortest BackOff when present,
otherwise positive AckWait, otherwise 30 seconds. One private resolver serves validation and acquisition.

The policy defensively copies BackOff and retains callback/timing only—no context, cancel, handle, goroutine, health,
or cross-delivery state. Zero policy fails before message I/O. Each owner passes the same config variable to validation
and acquisition. Validation stores the work function but no payload; one policy may serve multiple deliveries and each
invocation receives its current body.

### D5 — preserve crash schedules and lower heartbeat when admitted

Tools retains BackOff 15s/60s and moves heartbeat 120s→5s in the foundation. Dispatch remains 10s/30s. Model plus loop
task, response, and tool-result heartbeat migration is additive to the full accepted #1146 vertical; its rebaseline
selects the reviewed settlement route and any timing change. #1249 selects AgentRun's reviewed timing only after it
defines fanout done and replay. The final #759 tree records the resulting explicit crash schedule for every migrated
binding.

Healthy renewal must prevent overlapping redelivery. Process loss must follow declared BackOff rather than silently
falling back to AckWait. Tools' 5-second target permits at most 36 InProgress calls/minute at MaxAckPending 3; three
future loop lanes at 15 seconds permit at most 12/minute total at MaxAckPending 1.

### D6 — inspectable result and control ownership

```go
type DeliveryResult struct { /* private immutable observation */ }

func (r DeliveryResult) Decision() DeliveryDecision
func (r DeliveryResult) Cause() error
func (r DeliveryResult) ControlError() error
func (r DeliveryResult) SettlementError() error
func (r DeliveryResult) SettlementAttempted() bool
func (r DeliveryResult) SettlementMethodSucceeded() bool
func (r DeliveryResult) SettlementMethodFailed() bool
func (r DeliveryResult) ServerConfirmed() bool
func (r DeliveryResult) Quarantined() bool
func (r DeliveryResult) OwnerStopRequired() bool
func (r DeliveryResult) Err() error
```

Decision preserves exactly what joined work requested; invalid tuples do not rewrite it. Panic synthesizes Quarantine
because work returned no tuple. Only clean local Ack has nil Err. Retry/Terminate causes remain reachable after method
success. Control and settlement errors remain separate. Plain Ack/Nak/NakWithDelay/Term never sets ServerConfirmed.
Method error means unknown/not-confirmed, not unsettled or guaranteed redelivery.

OwnerStopRequired is true for semantic quarantine, invalid/panic defense, and every InProgress control failure while
preserving joined Decision/Cause. It is false for a terminal method error alone. Control failure cannot maintain
ownership while work may have run; a method error does not prove the lane's heartbeat control path is lost.

### D7 — cancel, join, interpret

Owner cancellation cancels work, joins it, interprets its exact decision/error tuple, then attempts settlement.
Context error never replaces joined meaning. InProgress failure cancels and joins, preserves meaning, records
ControlError, attempts no later terminal method, and sets OwnerStopRequired. Work panic is recovered inside the
joined goroutine. Every task joins before return; no context is retained.

After runtime policy defense, the typed entry point reads `msg.Metadata()` exactly once and validates a positive
delivery number. Invalid or unavailable metadata returns typed quarantined owner-stop evidence before Data, work,
heartbeat, or settlement. With valid metadata it reads `msg.Data()` exactly once, constructs the immutable attempt,
launches work with attempt plus those bytes, and runs InProgress concurrently while work is pending. Every started
task joins before return and normalizes its returned tuple or panic before branch interpretation. Control failure
adds ControlError and attempts no terminal method; normal completion passes only Ack/Nak/NakWithDelay/Term to the
private terminal executor. The settlement-capable message never reaches work or policy.

### D8 — private exact-owner reaction

Each migrated physical binding privately creates admission and a capacity-one first-fatal signal before acquisition.
Closed admission performs no work, heartbeat, or terminal method. Already-admitted work may complete. The callback
latches only on OwnerStopRequired and never drains or waits on its own handle.

The existing owner commits the exact acquired handle, observes buffered fatal results outside callback, records
bounded evidence through existing health/error fields, and shares one private stop/drain-once path with ordinary Stop.
The observer derives from Start/Run and joins Stop. Shared natsclient owns no lifecycle.

### D9 — private terminal executor and branch-staging containment

Typed and staged legacy helpers may share only a private terminal-method executor. Before extraction,
characterization tests pin every old ACK, 30-second Retry, Term, 5-second cancellation, InProgress, and error-chain
path. Typed does not call legacy; legacy does not translate through the exported DeliveryDecision/DeliveryWork
contract. `TerminateDelivery(error) error` and `PermanentDeliveryError` retain their exact contracts and are not part
of the removal gate.

The zero-growth staging guard names only the model, loop, and AgentRun production files and shrinks after each child
integration. It permits no new caller and grants no compatibility status. Final conformance replaces it with proof
that the old declaration, aliases, and every production caller are absent.

### D10 — staged owner migrations

Stage A migrates tools one and dispatch two. Tools ACKs only after completed outcome authority and result PubAck;
completed-outcome replay publication may Retry; post-execution outcome-Create ambiguity quarantines. Dispatch retries
only proven pre-publish failure; unknown PubAck after invocation quarantines before unlimited retry.

Each of the three policy constructions uses a binding-local `DeliveryWork` closure. The closure accepts and ignores
`DeliveryAttempt`, then delegates unchanged bytes to the existing tools or dispatch domain handler. Transport
observation does not enter those domain handler signatures or their direct tests.

The Stage A tools and dispatch bindings establish the typed foundation but do not authorize PR #1156 to merge.

#1146 retains its full accepted intake, command, model, loop, tools, signal, approval, projection, governance, replay,
and context/lifecycle scope. Its model and three loop heartbeat migrations are additive. It rebases onto the reviewed
#759 foundation checkpoint and chooses a reviewed route for every fast no-heartbeat lane. No lane receives raw
settlement or an exported no-heartbeat API by implication.

#1249 begins from the staged #759 head after #1146 integration so its AgentRun design observes the post-#1146 terminal
and handler shapes. It defines source identity, handler durable done, replay, panic/error, and partial-success
semantics before migrating complete or failed.

No migration maps callback return values mechanically. The binding owner returns ACK only after its named durable
positive or negative consequence and every required downstream acknowledgement.

### D11 — builder removal

`NewDurableHandler` has zero measured adopters. After the permanent API, direct policy tests, Stage A migration,
owner review, and #1155 Stage A proof, remove it without alias. The remaining branch-staging callers do not use it, so
this removal precedes the final `ConsumeWithHeartbeat` cutover without creating a current compatibility surface.

### D12 — gated-DAG domain and transport split

`gated-dag-dispatch` owns each adopter's domain definition of done and replay. `jetstream-consumer-policy` owns
transport settlement, heartbeat, lease, and exact consume-handle mechanics. The domain capability does not prescribe
a generic nil/error callback or one heartbeat API.

A synchronous publish error is ambiguous because the server may have persisted before the acknowledgement was lost.
The executor re-arms its durable claim, and repeated attempts use deterministic `Nats-Msg-Id=unitID`, but server
deduplication is effective only inside the configured `Duplicates` window. Requiring
`Duplicates >= BackstopInterval` covers ordinary backstop redispatch; it is not an unbounded exactly-once guarantee.
After a longer interruption, each adopter's durable already-complete or idempotent replay contract is load-bearing.

SemSpec's enabled execution bridge and SemDragon's staged, unregistered `questdag` have different definitions of done
and replay. SemStreams records exact migration instructions for each checkpoint and mutates neither sister repository.

### D15 — final legacy removal

Final `ConsumeWithHeartbeat` removal belongs to #759. The branch-staging zero-growth guard shrinks as #1146 and #1249
migrate their callers. After the final staged migration, conformance requires zero production callers and absence of
the exported declaration and every alias.

Removal, complete replacement proof, migration reconciliation, and the complete closing claim set precede final PR
#1156 implementation and owner-requested cross-agent review. Archive/spec sync follows accepted fixes and re-review
and is the final content commit. There is no accepted additive dual-API period. Closed issue #1250 remains closed and
is not reclaimed.

## Rejected designs

- Extend the builder into a handle owner: creates a second lifecycle authority and cannot resolve
  callback-before-return.
- Export a no-heartbeat `SettleDelivery`: no present #759 adopter; OTEL needs later pull-specific inventory.
- Derive semantic retry from BackOff: conflates explicit Nak policy with server missing-settlement schedule.
- Remove BackOff: silently weakens tools/loop crash recovery.
- Shared gate/supervisor/durable quarantine: duplicates component and JetStream authority.
- Migrate a binding by mechanical nil-to-ACK/error-to-Retry conversion: invents definition of done and replay safety.
- Pass `jetstream.Msg`, a settlement-capable view, or per-delivery work closure: leaks settlement authority or weakens
  setup-time validation.
- Export `DeliveryAttempt` fields or a public constructor: permits inconsistent caller-authored observations and
  turns a framework-observed fact back into caller prediction.
- Treat redelivery as proof prior work ran: process loss before invocation produces the same later observation.
- Treat deterministic message-ID deduplication as unbounded exactly-once: the server forgets IDs after `Duplicates`.
- Put generic settlement, heartbeat, lease, or handle mechanics in gated-DAG domain semantics: adopters have different
  durable consequences and replay checks.

## Verification gates

- Focused natsclient/tools/dispatch race tests and real-NATS heartbeat/BackOff tests.
- AST zero-growth staging guard and same-config validation/acquisition conformance.
- #1155 Stage A process replacement with one tools effect and no duplicate dispatch response, followed by every
  remaining replacement-proof row before final landing.
- `task e2e:agentic` after Stage A and every later admitted stage.
- During staging, exact AST zero-growth guard: legacy remains only in model, loop, and AgentRun with no new production
  caller or alias. Before archive, zero production callers and no exported declaration or alias.
- Gated-DAG capability validation and a reproducible, SemStreams-owned sister migration record.
- Separate implementation and archive/spec-sync review for #1146 and #1249 on the non-default integration trunk.
- Final integrated implementation review, owner-requested cross-agent review, then final-content archive and narrow
  archive/spec-sync review.

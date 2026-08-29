# Design: semantic JetStream settlement

## Decisions

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
consumer. A separate name allows Stage A to compile while held callers retain the old Go signature. It is not renamed
at final legacy removal.

Legacy remains source- and behavior-compatible only for an exact shrinking allowlist: model, loop, and AgentRun.
New production callers fail AST conformance. Documentation and examples use only the typed API.

### D2 — closed semantic disposition

```go
type DeliveryDispositionKind uint8

const (
    DeliveryDispositionInvalid DeliveryDispositionKind = iota
    DeliveryDispositionAck
    DeliveryDispositionRetry
    DeliveryDispositionTerminate
    DeliveryDispositionQuarantine
)

type DeliveryDisposition struct { /* private */ }

func (d DeliveryDisposition) Kind() DeliveryDispositionKind
func AckDelivery() DeliveryDisposition
func RetryDelivery(cause error) DeliveryDisposition
func TerminateDelivery(cause error) DeliveryDisposition
func QuarantineDelivery(cause error) DeliveryDisposition
```

ACK means the owner-defined durable consequence completed. Retry is repairable and carries no timing. Terminate is
immutable poison. Quarantine means retry and termination are not proven safe. Non-ACK kinds require causes; zero,
unknown, missing-cause, and recovered panic fail closed. `PermanentDeliveryError` compatibility remains exact.

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
    work func(context.Context) DeliveryDisposition,
) (HeartbeatDeliveryPolicy, error)
```

Validation is pure and before acquisition/I/O. It rejects nil/ended context, nil work, invalid retry, nonpositive
heartbeat, negative AckWait, nonpositive BackOff entries, an interval too small for positive heartbeat, and heartbeat
greater than half effective interval. Equality is allowed. Effective interval is shortest BackOff when present,
otherwise positive AckWait, otherwise 30 seconds. One private resolver serves validation and acquisition.

The policy defensively copies BackOff and retains callback/timing only—no context, cancel, handle, goroutine, health,
or cross-delivery state. Zero policy fails before message I/O. Each owner passes the same config variable to validation
and acquisition.

### D5 — preserve crash schedules and lower heartbeat when admitted

Tools retains BackOff 15s/60s and moves heartbeat 120s→5s in Stage A. Dispatch remains 10s/30s. Model moves 90s→60s
only if its hold lifts. Each loop lane moves 60s→15s only when its hold lifts and retains BackOff 30s/120s. AgentRun
remains 10s/30s.

Healthy renewal must prevent overlapping redelivery. Process loss must follow declared BackOff rather than silently
falling back to AckWait. Tools' 5-second target permits at most 36 InProgress calls/minute at MaxAckPending 3; three
future loop lanes at 15 seconds permit at most 12/minute total at MaxAckPending 1.

### D6 — inspectable result and control ownership

```go
type DeliveryResult struct { /* private immutable observation */ }

func (r DeliveryResult) Kind() DeliveryDispositionKind
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

Only clean local Ack has nil Err. Retry/Terminate causes remain reachable after method success. Control and settlement
errors remain separate. Plain Ack/Nak/NakWithDelay/Term never sets ServerConfirmed. Method error means
unknown/not-confirmed, not unsettled or guaranteed redelivery.

OwnerStopRequired is true for semantic quarantine, invalid/panic defense, and every InProgress control failure while
preserving joined Kind/Cause. It is false for a terminal method error alone. Control failure cannot maintain ownership
while work may have run; a method error does not prove the lane's heartbeat control path is lost.

### D7 — cancel, join, interpret

Owner cancellation cancels work, joins it, interprets its exact disposition, then attempts settlement. Context error
never replaces joined meaning. InProgress failure cancels and joins, preserves meaning, records ControlError, attempts
no later terminal method, and sets OwnerStopRequired. Work panic is recovered inside the joined goroutine. Every task
joins before return; no context is retained.

### D8 — private exact-owner reaction

Each migrated physical binding privately creates admission and a capacity-one first-fatal signal before acquisition.
Closed admission performs no work, heartbeat, or terminal method. Already-admitted work may complete. The callback
latches only on OwnerStopRequired and never drains or waits on its own handle.

The existing owner commits the exact acquired handle, observes buffered fatal results outside callback, records
bounded evidence through existing health/error fields, and shares one private stop/drain-once path with ordinary Stop.
The observer derives from Start/Run and joins Stop. Shared natsclient owns no lifecycle.

### D9 — private terminal executor and legacy containment

Typed and legacy helpers may share only a private terminal-method executor. Before extraction, characterization tests
pin every legacy ACK, 30-second Retry, Term, 5-second cancellation, InProgress, and error-chain path. Typed does not call
legacy; legacy does not translate through exported dispositions.

The AST allowlist names only model, loop, and AgentRun production files and shrinks with migration. Legacy receives a
deprecation comment and is removed without alias only after all held addenda/proofs, zero repository callers, sister
migration reconciliation or explicit pre-v1 break ruling, complete E2E, and later owner removal approval.

### D10 — Stage A and held addenda

Stage A migrates tools one and dispatch two. Tools ACKs only after completed outcome authority and result PubAck;
completed-outcome replay publication may Retry; post-execution outcome-Create ambiguity quarantines. Dispatch retries
only proven pre-publish failure; unknown PubAck after invocation quarantines before unlimited retry.

Model, loop task, loop response, loop tool-result, AgentRun complete, and AgentRun failed are six separate
non-authorizing holds. Each requires a then-current line-addressable inventory/adopter/collision addendum, independent
inventory pass, options/design, independent design pass, and explicit owner acceptance naming the lifted binding.
Durable model state additionally requires `entity-or-bucket`. #1148 clears only AgentRun's collision.

### D11 — builder removal

`NewDurableHandler` has zero measured adopters. After the permanent API, direct policy tests, Stage A migration,
owner review, and #1155 Stage A proof, remove it without alias. Held callers do not use it, so this removal is distinct
from the final legacy-helper removal.

## Rejected designs

- Extend the builder into a handle owner: creates a second lifecycle authority and cannot resolve callback-before-return.
- Export a no-heartbeat `SettleDelivery`: no present #759 adopter; OTEL needs later pull-specific inventory.
- Derive semantic retry from BackOff: conflates explicit Nak policy with server missing-settlement schedule.
- Remove BackOff: silently weakens tools/loop crash recovery.
- Shared gate/supervisor/durable quarantine: duplicates component and JetStream authority.
- Migrate held callers by implementation judgment: invents definition of done and replay safety.

## Verification gates

- Focused natsclient/tools/dispatch race tests and real-NATS heartbeat/BackOff tests.
- AST legacy allowlist and same-config validation/acquisition conformance.
- #1155 Stage A process replacement with one tools effect and no duplicate dispatch response.
- `task e2e:agentic` after Stage A and every later admitted stage.
- No final legacy removal until the approved zero-caller gate and separate owner approval.


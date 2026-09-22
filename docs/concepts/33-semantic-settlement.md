# Semantic settlement: the message pump and lease watchdog

SemStreams processes durable JetStream work through a small message-pump pattern. JetStream holds the queued work and
redelivers anything that was not settled. A component defines what “done” means for its own effect. The framework
keeps the delivery lease alive while that work runs, joins the work, and translates its declared result into one
terminal delivery action.

This pattern is sometimes described as middleware, but two more precise names help:

- the **message pump** is the whole receive → work → settle loop; and
- the **lease watchdog** is the heartbeat side that renews JetStream ownership while work is still running.

Neither is a supervisor or persistent state machine. The component already owns its lifecycle, JetStream already owns
unsettled delivery, and the component's existing stream, KV, graph, or ObjectStore consequence remains the durable
authority.

## The contract

The component supplies one `DeliveryWork` callback. It receives an immutable delivery attempt and read-only payload
bytes, but no native message and no Ack/Nak authority. It returns one semantic decision:

| Decision | Meaning | JetStream action |
|---|---|---|
| ACK | The component's durable consequence is complete | `Ack` |
| Retry | Work is safe and useful to try again | immediate or explicitly delayed `Nak` |
| Terminate | The delivery is immutable poison or permanently inapplicable | `Term` |
| Quarantine | Completion is ambiguous or control is no longer safe | no terminal method; stop the exact owner |

ACK requires a nil cause. Retry, Terminate, and Quarantine require a cause. Invalid combinations fail closed rather
than guessing. A local settlement method succeeding is not a server-confirmed transaction; the result records exactly
what the client observed.

“Done” is deliberately not defined as “the callback returned nil.” It is the durable consequence the component
owns. For example, the tools component is done only after the tool outcome is durable and the result publication
receives its PubAck. A different component can own a different durable consequence.

## Happy path

```text
JetStream delivery
    ↓
observe delivery number and payload
    ↓
start component work ───── lease watchdog sends InProgress
    ↓
durable consequence commits
    ↓
work returns ACK, nil
    ↓
message pump joins work and attempts Ack
```

The watchdog only protects the lease. It does not decide whether the effect succeeded. AckWait and BackOff control
missing-settlement redelivery; a component's explicit Retry policy controls Nak timing. Keeping these independent
prevents an operator's crash-recovery schedule from silently changing domain retry behavior.

## Process replacement

Suppose a tool call executes, its completed outcome becomes durable, and the process stops before its result publish
or final Ack:

```text
first process                         replacement process
-------------                         -------------------
execute effect once
write completed outcome
process stops       ── JetStream ──→  redeliver unsettled request
                                      observe completed outcome
                                      publish result from durable outcome
                                      return ACK
```

The replacement does not need a supervisor record saying which step ran. It reconciles against the durable authority
the component already owns. This is the streams-first restart pattern: settle only after durable done, let unsettled
work redeliver, and make replay consult the durable consequence before repeating an effect.

If the component cannot determine whether an external effect committed, it must not hide that ambiguity behind ACK
or unlimited Retry. It returns Quarantine, leaves the message without a terminal method, and asks the existing exact
consumer owner to stop. The next design step is then component-specific reconciliation—not a generic framework state
machine.

## Owner responsibilities

A durable consumer owner composes the pattern in four steps:

1. Define its durable consequence and replay check.
2. Validate heartbeat and semantic retry policy from the exact consumer configuration.
3. Retain the exact native consume handle returned by acquisition.
4. Inspect every `DeliveryResult`; when `OwnerStopRequired` is true, close admission and stop that exact handle outside
   the callback.

Step 4 is one shape, not a per-component invention. In-tree it lives in `internal/deliverylane`: an admission latch
that closes on the first result requiring owner stop, a drain-once binding around the exact handle that lane
committed, and an observer that drains the handle when the latch closes. The five agentic processors compose it; the
package holds no lifecycle authority of its own, so the owner still decides stop, awaits `Closed`, and joins the
observer. It is internal while every consumer is a lane inside this module — the contract test
`test/contract/delivery_lane_one_home_contract_test.go` keeps it the only home — and exporting it is a separate,
measured decision.

The framework SHOULD make transport mechanics uniform. It SHOULD NOT guess a domain's definition of done, invent a
receipt ledger before one is proven necessary, or expose raw settlement authority to make a fast handler convenient.

## What restart safety still requires

This pattern is the foundation, not a claim that every existing consumer is already safe. A binding is restart-safe
only when its happy and sad paths define durable done, classify replay, preserve ambiguous outcomes, and pass process-
replacement proof. Model and loop work continues under #1146.

AgentRun's milestone fanout resolved its version of the question (#1249) in a way worth naming, because one source
delivery reaching several outward-facing handlers has no obvious answer. It settles the WHOLE fanout as one unit:
every attempt runs every registered handler, and the delivery is acknowledged only on an attempt where all of them
returned nil. That deliberately trades at-most-once for at-least-once — a handler already done can be re-invoked by
a replay caused by a different handler — and pays for it with an identity. Every attempt of one delivery presents
the same `SourceMessageID`, so a handler makes its own effect idempotent on that key. The framework does not verify
that obligation and does not try to: a per-handler receipt ledger is exactly the durable primitive this capability
refuses to invent before one is proven necessary.

See [Migrate to direct one-shot lifecycle ownership](../operations/migration-restart-safe-nats-client.md) for the Go
composition and [Gated-DAG semantic-settlement migration](../operations/migration-gated-dag-semantic-settlement.md)
for an example of why different adopters cannot share one guessed definition of done.

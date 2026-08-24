# Design: Exact terminal and nonterminal tool-result scope

## Checkpoint

- Parent inventory: independently reviewed `INVENTORY PASS` at SHA-256
  `d91a49caa42d027df482c0c8adc4ebe4f290459e5b161a81bf8d11a372662d7a`.
- Parent #1063 design: owner accepted at independently reviewed SHA-256
  `56fa9dc95a4dbf6f3f7d121912972e036f7c1c2d55a8834e991eeafa8b37ae7a`.
- Terminal-scope correction: owner accepted on 2026-08-23 at independently reviewed pre-acceptance SHA-256
  `22375a461578b6100a96d838d6726c2d4f2f10bedcfe80b483fc7914e9117332`.

The binding rulings are: preserve `approval_required` as correlated nonterminal coordination; scope durable terminal
guarantees to execution, terminal policy rejection, and COMPLETED replay; treat `MaxAckPending=3` as acknowledgement
admission only; promise neither serialization nor overlap; correct outward truth and durable evidence without changing
production behavior.

## Decision

Preserve two distinct existing result classes. Every wire response remains correlated to its CallID. Initial
`approval_required` is a nonterminal pause: it has a phase-distinct message ID, creates no COMPLETED outcome, and
leaves the same CallID eligible for approved re-dispatch. Logical calls that reach execution or terminal policy
rejection receive exact correlated durable terminal outcomes; COMPLETED redelivery replays that authority without
executor re-invocation.

`MaxAckPending=3` governs delivered-but-unacknowledged admission only. The wire contract promises neither serialized
execution nor overlap. The current one-native-callback path through persistence, publication, and delivery settlement
is nonnormative implementation evidence.

## Rejected options

- Leaving “every admitted call is terminal” would contradict approval pause behavior.
- Making approval-required terminal would collide with the approved same-CallID re-dispatch.
- Promising only an unqualified correlated result would weaken durable terminal execution and rejection guarantees.
- Inferring worker parallelism or serialization from acknowledgement admission would expose an implementation accident.

## Adopter seam

An external executor author or `tool.result.*` consumer needs to distinguish the existing approval pause from a
terminal result. If they do nothing, runtime behavior and configuration remain unchanged. They need not know callback
shape, consumer admission values, worker counts, or predict execution overlap. Existing result/error approval semantics
carry the distinction.

## Verification

The approval test proves the initial pause is nonterminal and the approved same-CallID re-dispatch executes once,
persists terminal authority, and replays it. The causal real-NATS test proves three non-approval-intercepted calls by
exact IDs and contents under a finite bound. The policy guard prevents restoration of the removed sleeps. Contract and
strict OpenSpec validation close the local evidence slice; independent review and owner acceptance remain mandatory.

## Stop conditions

Stop for owner review on any production behavior change; broadened approval terminality; weakened executed/rejected
durability; elapsed-time concurrency inference; missing, duplicate, unexpected, or error result; failed omission
restoration; strict validation failure; or outward wording that implies local parallelism or terminal approval pause.

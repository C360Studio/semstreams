# Design

## Accepted evidence and owner decisions

- Accepted inventory: `docs/proposals/gh865-866-terminal-event-inventory.md`, body SHA-256
  `ae27e5111ee10e531ffe90c4505687367ea534e80c816bc401bf4b7168804676`.
- Accepted complete design: `docs/proposals/gh865-866-terminal-event-design.md`, body SHA-256
  `a4b5607eefee80fd3910a769c74b509f4faec8cc04eff84cdddeae26868804c5`.
- Baseline: `6eb8646992b55aa3c08a695e89db4bfea6b3b000`.
- Separate subject-ownership issue: [#952](https://github.com/C360Studio/semstreams/issues/952).
- Result-by-reference boundary: #857.

## D1 — One fail-closed interpretation home

A repo-internal normalizer decodes production `BaseMessage` bytes using a registry-bound `message.Decoder`. It
requires:

- nonempty source `BaseMessage.ID`;
- valid message type and metadata;
- nonnil concrete payload whose `Validate()` succeeds, including loop/task IDs;
- nonzero applicable `CompletedAt`, `FailedAt`, or `CancelledAt` timestamp;
- one accepted category/outcome pair.

Every failure is a permanent structural rejection. Consumers do not reimplement validation or re-switch on raw
category/outcome.

## D2 — Closed terminal matrix

| Category | Outcome | Normalized class | Tracker state |
|---|---|---|---|
| `loop_completed` | `success` | succeeded | `complete` |
| `loop_failed` | `failed` | failed | `failed` |
| `loop_cancelled` | `cancelled` | cancelled | `cancelled` |

Every other pair is permanently rejected. A truncation failure may persist `LoopEntity.Outcome="truncated"`, but the
emitted `LoopFailedEvent.Outcome` remains `"failed"`. This change neither adds `loop_failed + truncated` nor reconciles
the persisted/wire distinction.

## D3 — Consumer migration

Dispatch, AgentRun, and OTel consume the internal projection. AgentRun maps it into its existing public
`LoopTerminalEvent`; no public terminal union is added. Physical completion/failure subscriptions may remain, but
subject name is never semantic category authority.

## D4 — Field-wise routing reconciliation

Dispatch reconciles `ChannelType`, `ChannelID`, and `UserID` independently from the process-local tracker, normalized
terminal payload, and persisted `AGENT_LOOPS/<loopID>` `LoopEntity`.

Empty fields contribute no value. Matching nonempty values agree. Conflicting nonempty values in any field are
permanent identity/routing collisions. Persisted state must be observed before final classification.

| Merged ChannelType | Merged ChannelID | Result |
|---|---|---|
| nonempty | nonempty | Publish `UserResponse`; `UserID` is optional metadata |
| nonempty | empty | Permanent malformed partial route |
| empty | nonempty | Permanent malformed partial route |
| empty | empty | Intentionally route-less; publish no response |

An empty `UserID` never invalidates a complete channel pair. Two conflicting nonempty `UserID` values remain a
permanent collision. KV unavailability or a temporarily absent loop key is transient. Malformed persisted JSON or a
persisted loop-ID mismatch is permanent. This lookup does not rebuild dispatch's process-local indices.

## D5 — Stable response identity

For one validated source terminal envelope:

```text
terminal-user-response:<source BaseMessage ID>
```

Dispatch uses that value for both `UserResponse.ResponseID` and JetStream `Nats-Msg-Id`. The response timestamp comes
from the validated terminal timestamp. Publication uses synchronous `PublishToStreamWithMsgID`; required PubAck must
arrive before source ACK.

## D6 — Retention-bounded settlement

Dispatch terminal consumers use `MaxDeliver=0`:

- required response PubAck -> Ack;
- intentionally route-less loop after persisted-state observation -> Ack after remaining work;
- transient persisted-state read or response publication failure -> delayed Nak;
- permanent validation, identity, category/outcome, or routing failure -> Term;
- shutdown -> short delayed Nak.

`MaxDeliver=0` removes attempt-count exhaustion but does not override AGENT retention. The checked AGENT posture is:

- MaxAge: 24h;
- MaxBytes: 256MiB;
- Discard: old.

An unsettled terminal may be evicted by age or capacity. After eviction no retry source remains and no response
guarantee exists. No retry-count knob, outbox, alternate stream, KV response authority, or other durable authority is
introduced.

## D7 — Operator visibility

Exactly one fixed-reason metric records each attempt's final disposition: envelope/type rejection,
payload-validation rejection, zero terminal timestamp, identity/category/outcome collision, routing
collision/malformed state, tracker-projection collision, transient routing read, transient response publication,
successful response settlement, or route-less settlement.

The finite-MaxDeliver advisory observer does not prove eviction for this unlimited-attempt consumer. Stream
age/capacity posture is observable, but no per-message proof presently shows that an unsettled terminal was evicted
before response settlement. General eviction visibility remains in the existing retention program; this design does
not claim that program supplies a stronger response guarantee.

## Adopter seam

The adopter is a product developer using AgentRun callbacks or consuming typed `UserResponse` without opening
SemStreams implementation files.

- **What must they know?** A response channel requires `ChannelType` plus `ChannelID`; `UserID` is optional metadata.
  Terminal-derived response IDs are stable across retries. Retries continue only while the terminal remains in AGENT.
- **What happens if they do nothing?** Existing APIs compile, intended AgentRun callbacks begin working, and complete
  channel pairs publish even with empty `UserID`. A long outage or capacity pressure can evict an unsettled terminal.
- **Where do they find out?** This capability spec, stream/operator documentation, release notes, and fixed routing
  and settlement telemetry.
- **What should they have to know?** Only the channel type and channel ID they already use. They do not choose a retry
  count, synthesize a `UserID`, or predict a recovery deadline.

## Boundaries

- #952 owns reserving `user.response.>` for typed `UserResponse` and moving heterogeneous product notifications.
- #857 owns result-by-reference and large completion retrieval.
- Source terminal publication loss and general dispatch restart reconstruction remain separate.
- Persisted truncation/wire failure reconciliation remains separate.
- A stronger guarantee surviving AGENT eviction requires another durable authority and remains separate.
- Actual semteams verification is deferred to its beta.160 migration because its beta.159 tree cannot build against
  this baseline. See `docs/proposals/gh865-866-semteams-verification-deviation.md`; the durable representative adopter
  fixture is compatibility evidence, not an assertion about the unmigrated application.
- Settlement-metric ordering is clarified without changing the accepted design artifact in
  `docs/proposals/gh865-866-terminal-settlement-metrics-addendum.md`.

## Owner-ruling conformance scaffold

The following file:line evidence records the implemented owner rulings.

| # | Owner ruling | Conformance evidence |
|---:|---|---|
| 1 | One repo-internal registry-backed normalizer | E01 |
| 2 | Dispatch, AgentRun, and OTel are the present consumers | E02 |
| 3 | No new payload or exported normalized terminal API | E03 |
| 4 | Existing AgentRun callback type remains | E04 |
| 5 | No response outbox | E05 |
| 6 | Retained AGENT terminal is the sole retry source only while retained | E06 |
| 7 | Field-wise routing; channel pair required, `UserID` optional | E07 |
| 8 | Stable `ResponseID` and `Nats-Msg-Id` derive from source identity | E08 |
| 9 | Synchronous response PubAck precedes terminal ACK | E09 |
| 10 | At-least-once and eviction-bounded; no exactly-once/post-eviction claim | E10 |
| 11 | Heterogeneous `user.response` split remains #952 | E11 |
| 12 | Existing response subject remains in this change | E12 |
| 13 | Source publication loss and general reconstruction remain separate | E13 |
| 14 | AgentRun correction now; OTel migrated to prevent drift | E14 |
| 15 | Dispatch terminal consumers use `MaxDeliver=0` with no retry knob | E15 |
| 16 | Real-NATS and `task e2e:agentic` are required | E16 |
| 17 | Source ID/type/metadata/payload/loop/task/timestamp fail closed | E17 |
| 18 | Exactly three wire pairs; producer truncation semantics unchanged | E18 |
| 19 | AGENT 24h/256MiB/DiscardOld residual and visibility gap recorded | E19 |

- E01: `internal/agentterminal/terminal.go:1-4`, `:97-119`.
- E02: `processor/agentic-dispatch/terminal_settlement.go:155-159`; `agentic/agentrun/agentrun.go:479-480`;
  `output/otel/span_collector.go:199-220`.
- E03: Go internal boundary at `internal/agentterminal/terminal.go:1-4`; non-wire projection at `:65-66`.
- E04: `agentic/agentrun/agentrun.go:371-395`, `:479-522`; durable representative adopter fixture at
  `test/compat/semteams/agentrun_terminal_compat_test.go:1-80` and harness at
  `scripts/verify-semteams-agentrun-compat.sh:1-20`.
- E05: direct synchronous publication at `processor/agentic-dispatch/terminal_settlement.go:136-152`.
- E06: `processor/agentic-dispatch/component.go:413-426`, `:461-474`;
  `docs/operations/38-agent-terminal-settlement.md:46-60`.
- E07: `processor/agentic-dispatch/terminal_settlement.go:34-82`;
  `processor/agentic-dispatch/terminal_settlement_test.go:84-181`.
- E08: `processor/agentic-dispatch/terminal_settlement.go:16`, `:125-132`, `:198-199`.
- E09: `processor/agentic-dispatch/terminal_settlement.go:136-152`;
  `processor/agentic-dispatch/component.go:417-426`, `:465-474`;
  production-callback proof at `processor/agentic-dispatch/terminal_settlement_integration_test.go:175-263`.
- E10: `docs/operations/38-agent-terminal-settlement.md:46-60`.
- E11: `docs/operations/38-agent-terminal-settlement.md:70-73`.
- E12: existing response-subject resolution at `processor/agentic-dispatch/terminal_settlement.go:145-152`.
- E13: `docs/operations/38-agent-terminal-settlement.md:70-73`.
- E14: `agentic/agentrun/agentrun.go:479-522`; `output/otel/span_collector.go:199-255`;
  OTel consumer Term proof at `output/otel/component_terminal_integration_test.go:17-60`.
- E15: `processor/agentic-dispatch/component.go:407-414`, `:461-468`.
- E16: `processor/agentic-dispatch/terminal_settlement_integration_test.go:23-313`;
  `output/otel/component_terminal_integration_test.go:17-60`;
  `test/e2e/scenarios/agentic/scenario.go:160`.
- E17: `internal/agentterminal/terminal.go:97-175`; `internal/agentterminal/terminal_test.go:62-139`.
- E18: `internal/agentterminal/terminal.go:119-175`; `internal/agentterminal/terminal_test.go:115-139`;
  unchanged producer truncation at `processor/agentic-loop/handlers.go:1657-1715` and wire failure construction at
  `processor/agentic-loop/handlers.go:2545-2558`.
- E19: `configs/agentic.json:16-23`; `docs/operations/38-agent-terminal-settlement.md:48-68`.

Metric-ordering clarification: `docs/proposals/gh865-866-terminal-settlement-metrics-addendum.md:1-17`; delivery
attempts emit one final diagnostic disposition, while semantic business metrics follow successful required work.

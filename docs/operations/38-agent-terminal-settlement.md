# Agent terminal settlement

## Release note

**gh#1094 behaviour change to name in the release note:** a routed loop whose
terminal is a non-reply `decide` action no longer receives that decision JSON
as its `result`. A front-door coordinator that ends in, say,
`decide(action="done")` and a client waiting for a `result` will now see
nothing published and the `handoff_settled` reason instead; products deliver
their answers through `respond_direct` / `ask_user`. In exchange, a rule-spawned
workflow's answer is delivered to the originating channel for the first time.
The `decision` field on `loop_completed` is additive and optional: old readers
ignore it, new readers tolerate its absence.

This release corrects existing terminal-event consumers without adding a new
wire type or adopter-facing API. AgentRun callbacks now receive production
success, failure, and cancellation envelopes. Dispatch now publishes a routed
`UserResponse` before acknowledging its source terminal, and OTel rejects
malformed terminal intent instead of settling it as successfully processed.

Existing adopters do not need a configuration change. A response route still
requires `ChannelType` plus `ChannelID`; `UserID` remains optional. The only
operational change is that transient routing or publication failures remain
pending for redelivery while the source is retained, rather than being ACKed
and lost.

SemStreams normalizes the three registered loop-terminal payloads before
dispatch, AgentRun, or OTel interpret them. The accepted wire pairs are exactly:

- `loop_completed + success`;
- `loop_failed + failed`;
- `loop_cancelled + cancelled`.

Cancellation travels on `agent.complete.<loopID>`, so the subject is not type
authority. Invalid production envelopes, missing source identity, invalid
metadata or payload identity, zero terminal timestamps, and any other
category/outcome pair fail closed.

## Dispatch settlement

Dispatch resolves response routing field by field from the process-local
tracker, terminal payload, and the persisted loop record. `ChannelType` and
`ChannelID` are the address and both are required. `UserID` is optional
metadata. Empty fields do not overwrite nonempty fields; conflicting nonempty
values are permanent routing collisions. Once persisted state has been
observed, no channel type and no channel ID means the loop owns no route —
which, for a terminal that carries no user-facing decision, means it is
intentionally route-less and emits no response.

The loops bucket is read from the declared `agent_loops` KV read port
(default `AGENT_LOOPS`). An operator running agentic-loop on a non-default
loops bucket binds the same bucket name on dispatch's port; dispatch no
longer assumes the default name.

## Which terminal is the user's answer (gh#1094, ADR-101)

A workflow's answer is not "whichever loop owns a channel". A front-door
coordinator that hands off to a rule chain owns the channel; the loop that
actually answers is spawned by `publish_agent` and owns nothing. Dispatch
therefore selects by the terminal's typed decision:

| Terminal | Published | Reason label |
|---|---|---|
| `decide(action="respond_direct", reason=…)` | `result` carrying the reason | `response_settled` |
| `decide(action="ask_user", reason=…)` | `prompt` carrying the reason | `response_settled` |
| `decide(action=<anything else>)` | nothing, on any channel | `handoff_settled` |
| No decision, loop owns a route | `result` carrying the result, unchanged | `response_settled` |
| No decision, no route | nothing, unchanged | `route_less_settled` |

`respond_direct` and `ask_user` are framework-reserved action names. A product
that answers under a different action name settles `handoff_settled` — visible
as that metric reason and a Debug line naming the loop and the action. The
`decide` tool description stays vocabulary-agnostic, and
`restricted_decide_actions` can still bar either reserved name.

### Origin resolution for a route-less answer

When a reply decision's own loop owns no route, dispatch resolves the origin
from persisted loop records, typed-first and never settling while an untried
durable link remains:

1. the terminal's `RunID` names the run root's record — routed, that is the
   origin; present but route-less, the walk continues from it;
2. otherwise the `ParentLoopID` chain, up to the nearest routed ancestor; at
   any hop whose parent key is absent, that record's own untried `RunID` is
   tried before anything settles;
3. bounded at 32 hops with cycle detection.

Two distinct outcomes, and the difference matters operationally:

- `route_less_settled` — the walk ended at a record with no links and no
  route. There was no origin (a bus-submitted root, or ancestry severed by a
  spawn fired from a non-loop entity). Expected; not an alert.
- `origin_unresolvable` — a durable link pointed at a record that could not be
  observed, and both the parent chain and every encountered run anchor were
  exhausted (or the walk hit a cycle or the hop bound). The Warn names the
  absent loop and the run anchor. This IS an alert: it means an ancestor's
  `AGENT_LOOPS` key expired (24h after its last write) or its best-effort
  write never landed.

Origin resolution reads only persisted records — never the process-local
tracker — so a restarted dispatch resolves the same origin. That also means
the 24h key TTL and the best-effort persistence of `AGENT_LOOPS` bound it: a
workflow whose routed ancestor record is no longer observable has no delivery
guarantee, the same horizon the AGENT source already has.

A terminal-derived response uses
`terminal-user-response:<source BaseMessage ID>` for both `ResponseID` and
`Nats-Msg-Id`, and uses the validated terminal timestamp. Dispatch requires a
synchronous USER PubAck before ACKing the source terminal. Transient
`AGENT_LOOPS` reads or USER publication failures are delayed-NAKed. Permanent
decode, identity, category/outcome, and routing failures are Termed.

## Bounded guarantee

The dispatch terminal consumers use `MaxDeliver=0`, meaning unlimited delivery
attempts while the source terminal remains retained. It does not mean
indefinite storage. The checked AGENT declaration in `configs/agentic.json` is:

- MaxAge: 24 hours;
- MaxBytes: 256 MiB;
- discard policy: old.

Age or capacity pressure can therefore evict an unsettled terminal. No response
publication is guaranteed after that eviction. The stable USER message ID only
deduplicates within that stream's duplicate window; the declared behavior is
at-least-once within bounded AGENT retention and USER deduplication, not
exactly-once.

The `semstreams_router_terminal_settlement_total{reason}` counter uses fixed
reason labels — including `handoff_settled` and `origin_unresolvable` — and
emits exactly one final disposition per delivery attempt. It does not include
loop, user, channel, subject, or decision-action identifiers; the action name
appears only in log lines.

Origin resolution inherits a second bounded horizon: `AGENT_LOOPS` keys expire
24h after their last write and are written best-effort, so a workflow whose
routed ancestor record is not observable settles `origin_unresolvable` and no
delivery is claimed past that point.
Stream-level age and capacity are observable, but there is no per-message signal
proving that an unsettled terminal was evicted before its response settled. The
finite-MaxDeliver advisory is not such a signal for these unlimited-attempt
consumers.

This correction does not add a response outbox, payload, subject, stream, or
post-eviction authority. Heterogeneous `user.response.>` ownership remains #952;
large terminal result retrieval remains #857; source terminal publication loss
and general dispatch restart reconstruction remain separate.

## Compatibility verification

Both framework wiring paths compile with the unchanged AgentRun constructor and
callback surface:

```text
go test ./cmd/semstreams ./cmd/e2e-semstreams
```

A durable representative adopter fixture verifies that an external-style
product handler receives production success, failure, and cancellation
envelopes through the existing `MilestoneHandler` callback. The harness clones
a supplied adopter checkout, applies a local SemStreams module replacement, and
runs the checked-in fixture without editing the source checkout:

```text
./scripts/verify-semteams-agentrun-compat.sh /path/to/semteams
```

This proves the retained callback surface and production-envelope behavior. It
does not prove the current semteams beta.159 tree builds against this SemStreams
baseline: that tree still imports the removed `semstreams/pkg/ownership`, uses
the retired six-argument AgentRun constructor, and has not completed its
beta.160 migration. Actual semteams wiring verification is deferred to that
migration.

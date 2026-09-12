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

The original #1094 settlement correction required no configuration change. The later dispatch cleanup requires
adopters with custom input-port overrides to remove the retired notification inputs; see the
[dispatch migration](migration-beta162-to-beta163.md#dispatch-reads-loop-authority-instead-of-tracking-notifications-1146).
A response route still
requires `ChannelType` plus `ChannelID`; `UserID` remains optional. The only
operational change is that failed routing or publication does not ACK the source as delivered.
Transient reads retry; ambiguous publication stops the delivery owner with the source unsettled.

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

Dispatch resolves response routing field by field from the terminal payload
and exact validated persisted loop records. It has no process-local tracker. `ChannelType` and
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
as that metric reason and an INFO line naming the loop and the action — one line per workflow, and the only
log-visible trace of this behaviour change. The
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
  `AGENT_LOOPS` key expired or the named durable ancestor could not be established.

Origin resolution reads only persisted records, so replacement does not depend on a previous process receiving
creation notifications. Recovery requires both the terminal source and the routing records it names to remain
retained. Their retention intersection is the delivery boundary; neither resource's full configured horizon is
promised. A transient read retries. Confirmed absence of the terminal's own loop record reports
`terminal_route_unavailable` and leaves the source retryable, without fabricating a route or claiming user delivery.
That observation does not distinguish expiry, deletion, eviction, or a record never written. Ancestor lookup keeps
the fallback and `origin_unresolvable` behavior described above.

A terminal-derived response uses
`terminal-user-response:<source BaseMessage ID>` for both `ResponseID` and
`Nats-Msg-Id`, and uses the validated terminal timestamp. Dispatch requires a
synchronous USER PubAck before ACKing the source terminal. Transient
`AGENT_LOOPS` reads are delayed-NAKed. Ambiguous USER publication returns Quarantine: no terminal method is
attempted, and the exact delivery owner stops. See
[semantic settlement](../concepts/33-semantic-settlement.md) for operator recovery. Permanent
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
at-least-once within the intersection of retained AGENT input and required loop routing state, not exactly-once.
USER deduplication may suppress duplicates only inside its configured window.

The `semstreams_router_terminal_settlement_total{reason}` counter uses fixed
reason labels — including `handoff_settled` and `origin_unresolvable` — and
emits exactly one final disposition per delivery attempt. It does not include
loop, user, channel, subject, or decision-action identifiers; the action name
appears only in log lines.

Origin resolution also requires its routed ancestor records to remain available. Deletion, purge, expiry, or
eviction can end that recovery window before the source event expires. No process projection extends the window
and no delivery is claimed past it.
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

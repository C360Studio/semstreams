# Agent terminal settlement

## Release note

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
tracker, terminal payload, and `AGENT_LOOPS/<loopID>`. `ChannelType` and
`ChannelID` are the address and both are required. `UserID` is optional
metadata. Empty fields do not overwrite nonempty fields; conflicting nonempty
values are permanent routing collisions. Once persisted state has been
observed, no channel type and no channel ID means the loop is intentionally
route-less and emits no response.

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
reason labels and emits exactly one final disposition per delivery attempt. It
does not include loop, user, channel, or subject identifiers.
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

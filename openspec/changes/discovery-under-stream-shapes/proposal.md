## Why

A deployment whose JetStream config covers `tool.>` — **the shipped `TOOL` stream shape** —
silently loses tool discovery. A request to `tool.list` is answered by JetStream with a publish
ack rather than by the component's core-NATS responder:

```
{"stream":"TOOL","seq":1}
```

The caller decodes that into its expected response shape and gets a **zero-tool catalog**,
indistinguishable from "no tools registered". Nothing warns: the core-NATS subscription itself
succeeds, so health, logs and metrics all look fine.

Three properties make this worse than a config mistake:

- **The default collides with the default.** `processor/agentic-tools/config.go:148` defaults the
  discovery port to `tool.list`; the obvious stream shape for tool traffic is `tool.>`. Our own
  shipped e2e config hit it.
- **The hazard was already known and defended only by prose.** That same port's `Description`
  reads *"Override to e.g. 'discovery.tool.list' when JetStream streams cover 'tool.>'"*. A doc
  comment on a port description is the entire guard.
- **gh#749 just made it load-bearing.** The canonical tool `effect` classification now rides the
  discovery response, so sisters will start reading `tool.list`. A silently dead discovery surface
  reads to them as *"semstreams shipped the field but every tool is unclassified"* — the exact
  misreading gh#749 exists to prevent.

It was found by the `verify-tool-effect-catalog` e2e stage on its first run, and that stage is
**deliberately red on the default subject** until this lands.

### The class, not the instance

This is a **plane collision**: a subject serving request/reply is captured by a stream. It is not
specific to `tool.list`, and two sibling reports are the same shape one level up —
consumers cannot see which subjects the framework answers on, so they cannot detect the collision
themselves (gh#822: SemSource's `source-manifest` subscribed to `graph.query.summary` alongside
`graph-query` for an extended period; NATS request/reply with two subscribers is not load
balancing — both reply and the requester keeps whichever arrives first).

A fix that moves one subject leaves the class open. A guard that detects capture closes it,
including for stream configs nobody has written yet.

## What Changes

Three **additive** seams. The breaking one is deliberately deferred — see below.

1. **Provisioning guard** — when streams are provisioned, refuse (or loudly refuse-to-start) a
   stream whose subject filters cover a declared request/reply subject, naming the capturing
   stream and the colliding subject. This is the seam that closes the class: it catches future
   stream shapes and future request/reply subjects without either side knowing about the other.

2. **Pub-ack rejection in the canonical reply decoder** — `graph.UnwrapQueryResponse`
   (`graph/query_contracts.go:91`, already the gateway's decoder) MUST refuse a JetStream publish
   ack instead of decoding it as a reply. A `{"stream":…,"seq":…}` body is never a valid query
   reply, and today it degrades into an empty result. This is defence in depth for deployments
   provisioned outside our guard, and it converts a silent empty answer into a typed error.

3. **Export the request-subject list (gh#822)** — a consumer composing framework components into
   its own process has no reachable answer to *"which subjects does this component answer on?"*
   `setupQueryHandlers` registers from an unexported literal slice, and `InputPorts()` reflects
   the *configured* ports, which in a consumer deployment the consumer supplies. Exporting the
   list makes a consumer-side collision gate exact rather than hand-maintained.

## Deferred — and this needs an explicit ruling

**The default-subject move is NOT in this change.** The baton states this issue's scope two ways:

| Source | Scope |
|---|---|
| Fable ruling (baton, session 20) | provisioning guard + pub-ack rejection in the decoder + **default-subject move** |
| Tag roadmap (baton, gh#840, 2026-08-01) | provisioning guard + **gh#822 subject export** + decoder pub-ack rejection |

Moving the default (`tool.list` → e.g. `discovery.tool.list`) is **breaking** for anyone
requesting the current default — the issue says so itself and asks for lockstep treatment. The
roadmap places this change in **v1.0.0-beta.160**, which it describes as additive, and states
plainly that "additive tags need no lockstep; only breaking waves do, and none is planned".

Reading those together, the breaking move was dropped from .160 deliberately rather than lost.
**This change assumes that reading and excludes it.** If that is wrong, the move belongs in a
breaking wave with sister lockstep, not smuggled into an additive tag — so it is called out here
rather than decided quietly.

The three seams above make the collision **loud** without moving anyone's subject, which is why
they stand on their own regardless of how the default question resolves.

## Impact

- **Affected specs:** `agentic-tools` (discovery reachability), `graph-query` (subject export).
- **Affected code:** stream provisioning (`stream-provisioning` capability),
  `graph/query_contracts.go`, `processor/graph-query/query.go`, `processor/agentic-tools/`.
- **Not affected:** any wire format. No subject moves; no envelope changes; sisters need no
  lockstep.
- **Releases:** gh#810 is the trigger for **v1.0.0-beta.160**, which **releases semdev** — held
  pending their gh#749 ask *and the discovery surface it rides on* both working.
- **Unblocks:** the `verify-tool-effect-catalog` e2e stage returns to green on the default
  subject, becoming a live regression guard for the class instead of a known-red marker.

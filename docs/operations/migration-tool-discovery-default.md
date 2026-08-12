# Move the tool-discovery default

**Effective decision date:** 2026-08-11

**Status:** approved breaking migration. This note does not identify a candidate
or product tag, and it does not claim that #827 has executed.

## What changed

The discovery capability keeps the logical port name `tool.list`, but its port
kind and default NATS address change:

| Contract | Before | After |
|---|---|---|
| Logical port | `tool.list` | `tool.list` |
| Port kind | `nats` | `nats-request` |
| Default subject | `tool.list` | `discovery.tool.list` |
| Runtime subscriptions | Resolved subject with a hard-coded legacy starting point | Only the resolved `nats-request` subject |

There is no legacy responder, alias, dual subscription, or automatic repair. A
default deployment answers discovery only at `discovery.tool.list`.

## Who must act

You must update before adopting the breaking version if either is true:

- your client sends tool-discovery requests to subject `tool.list`; or
- your component configuration explicitly declares logical port `tool.list` as
  kind `nats`, even when it already uses a custom subject.

Deployments that omit the explicit discovery port inherit the new default.

## Client migration

Change the NATS request subject and keep the request/reply behavior unchanged:

```text
tool.list → discovery.tool.list
```

Do not probe the new subject and fall back to the old one. The framework has one
resolved address and does not serve the former default as a compatibility route.

## Configuration migration

Before:

```json
{
  "name": "tool.list",
  "config": {
    "kind": "nats",
    "subject": "tool.list"
  }
}
```

After, using the canonical default:

```json
{
  "name": "tool.list",
  "config": {
    "kind": "nats-request",
    "subject": "discovery.tool.list"
  }
}
```

A custom subject remains supported when the kind stays `nats-request`:

```json
{
  "name": "tool.list",
  "config": {
    "kind": "nats-request",
    "subject": "acme.discovery.tools"
  }
}
```

An override with kind `nats` fails startup. SemStreams does not reinterpret it as
request/reply or silently replace its subject.

## Stream migration

Replace broad tool stream coverage:

```json
"subjects": ["agent.>", "tool.>"]
```

with the explicit execution and result families:

```json
"subjects": ["agent.>", "tool.execute.>", "tool.result.>"]
```

This leaves the discovery request subject outside the stream while retaining
durable tool execution and result delivery.

## Scope and issue disposition

This cutover closes #842 after implementation, independent review, and both
required E2E paths are green:

- `task e2e:crud-tools` proves a nonempty effect-bearing catalog at
  `discovery.tool.list`; and
- `task e2e:agentic` proves live tool execution and result return with the
  narrowed stream subjects.

Issue #810 remains parked as the generic operator-defined overlap problem. This
cutover does not add a stream-overlap guard, request-subject registry, publish-ack
decoder, or exported subject inventory. An operator who chooses a custom request
subject remains responsible for keeping it outside captured stream filters.

## Supersession note

As of 2026-08-11, this migration note supersedes live guidance that names
`tool.list` as the default discovery subject or recommends `tool.>` for the AGENT
stream. Historical ADRs, remap inventories, archived OpenSpec changes, and the
frozen pre-v1 program remain unchanged as evidence of the decisions and system
state recorded at their original dates.

# Inventory: semantic JetStream settlement

## Reviewed checkpoint

- Baseline: `origin/main@3f3133a606461f0d882258fcd0bac7313ec42b8d`.
- Accepted source inventory: C2 first 309 lines, SHA-256
  `07781a0ee04e01b29b3a71d2ceb45e0f8dda18275417e315fdb95a10b519cd06`.
- Independent result: `INVENTORY PASS`.
- Design chain: C5 SHA-256 `11b6ccd93309e93a28b9175666b0fc98afcdc0fcb1514b90eed8dc41b90099be`;
  C6 SHA-256 `9204138bfe74653bb0b421efeada2280dde1e22ebccd6d417124e78526efe361`.
- Independent result: `DESIGN PASS`.
- Owner ruling: #759 comment `issuecomment-5459809936`, approved 2026-08-28.
- C8 decision/error correction: SHA-256
  `122db11f5e4ef8d14c48a9fbf8bacabe624af0662d4f853cd4fc607bd626af7b`, independent result
  `C8 DESIGN PASS`, owner ruling #759 comment `issuecomment-5462213858`, approved 2026-08-29.
- C9 payload correction: SHA-256
  `6665f5ac379b3e0cf54dee4cb519101c64981320361bce3f711603c51dd295fe`, independent result
  `C9 DESIGN PASS`, owner ruling #759 comment `issuecomment-5462371402`, approved 2026-08-29.

### Rebase addendum

The worktree rebased to `b060511f383d74aa6a8684e39e42020a3b073a9b` after #1150 merged. Independent
review returned `REBASE INVENTORY PASS`.

The exact settlement-owner comparison from `3f3133a6` to `b060511f` is byte-identical for
`natsclient/{heartbeat,consume_durable}.go`, tools/dispatch/loop/model component files, loop config, and AgentRun.
Both revisions have 23 Go `ConsumeWithHeartbeat` occurrences and the same production topology: five direct owner
sites, the zero-adopter builder composition, and the helper definition. All nine binding configurations below remain
unchanged.

#1150 changed an adjacent graph-query tool description and example identity/config surfaces, not delivery or replay
ownership. The document and IoT examples still use void callback plus direct Ack and remain #1145 scope. The prior
#1150 collision is therefore resolved. AgentRun remains held for #1148 and its later reviewed addendum.

## Counting units and census

A raw binding template is one named lane or one dynamic `port[*]` acquisition body. A callback implementation is one
actual handler/factory implementation. A settlement policy is one shared branch or helper choosing terminal methods.

- 31 production raw templates across 17 owners: nine dynamic templates and 22 named templates.
- 25 production callback implementations.
- 19 production settlement-policy implementations.
- Two shipped examples bring the reviewed raw-template total to 33.
- Runtime count is 20 fixed non-OTEL lanes, plus visible configured OTEL inputs, plus configured ports in nine dynamic
  families. OTEL default runtime is zero through two pull consumers because absent streams are skipped.

| Owner | Raw templates | Current policy shape |
|---|---:|---|
| graph-ingest | dynamic 1 | keyed classifier |
| ObjectStore | dynamic 1 | closed write disposition |
| File, HTTPPost, WebSocket | dynamic 3 | void then ACK |
| OTEL | named 2 | pull error Term / nil ACK |
| JSON generic/filter/map, Rule | dynamic 4 | void then ACK |
| Agentic model | named 1 | heartbeat callback always nil |
| Agentic tools | named 1 | heartbeat typed classifier and outcome replay |
| Governance | named 3 | parameterized void then ACK |
| Dispatch | named 5 | terminal heartbeat pair plus three void lanes |
| Loop | named 7 | three heartbeat plus four advisory lanes |
| AgentRun | named 2 | shared heartbeat factory |
| MaxDeliver | named 1 | advisory classifier |
| Document and IoT examples | dynamic 2 | void then ACK |

Binding evidence: `processor/graph-ingest/component.go:1504-1558`,
`storage/objectstore/component.go:869-1108`, `output/file/file.go:439-448`,
`output/httppost/httppost.go:425-434`, `output/websocket/websocket.go:1154-1163`,
`output/otel/component.go:431-468`, `processor/json_generic/json_generic.go:375-384`,
`processor/json_filter/json_filter.go:400-409`, `processor/json_map/json_map.go:421-430`,
`processor/rule/processor.go:1176-1181`, `processor/agentic-model/component.go:394-407`,
`processor/agentic-tools/component.go:418-437`, `processor/agentic-governance/component.go:308-507`,
`processor/agentic-dispatch/component.go:511-671`, `processor/agentic-loop/component.go:869-1100`,
`agentic/agentrun/agentrun.go:703-792`, `internal/maxdelivery/observer.go:193-260`, and each shipped example's
`component.go:516-571`.

## Direct helper seam

Five production direct `ConsumeWithHeartbeat` owner sites represent nine raw bindings:

| Owner | Bindings | Stage |
|---|---:|---|
| model | 1 | non-authorizing hold |
| tools | 1 | Stage A |
| dispatch complete/failed | 2 | Stage A |
| loop task/response/tool-result | 3 | three non-authorizing holds |
| AgentRun complete/failed | 2 | two post-#1148 non-authorizing holds |

`NewDurableHandler` has zero measured production and sister adopters. SemDev has two direct legacy-helper callers.
SemMachina retains older `ConsumeDurable` surfaces, which are migration evidence rather than baseline compatibility.

## Measured gaps

| Missing fact | Current spelling | Gap |
|---|---|---|
| semantic decision | nil ACK, wrapper Term, delayed NAK | no closed exhaustive decision contract |
| cancellation result | work cancels and joins | context can overwrite joined meaning |
| unclassified failure | generic retry; panic NAK | no fail-closed quarantine |
| control ownership loss | helper returns an error | exact owner has no typed stop-required result |
| setup validation | builder validates; direct callers predict | zero-adopter builder owns checks direct callers need |
| server confirmation | plain terminal methods | local return is not server confirmation |

Evidence: `natsclient/heartbeat.go:17-145`, `natsclient/consume_durable.go:17-74`,
`natsclient/stream.go:422-777`, and `openspec/specs/jetstream-consumer-policy/spec.md:37-104`.

## Exact owner and collision boundary

Push components retain exact `jetstream.ConsumeContext` handles and own drain/Closed. OTEL separately owns pull
consumer references, fetch cancellation/join, observation cleanup, and local claims. Shared natsclient heartbeat owns
one delivery's heartbeat and terminal attempt only. No generic supervisor, quarantine catalog, lane-stop status, or
recovery owner exists.

ADR-095 separates settlement from lifecycle/backlog/topology and orders effect before ACK. ADR-096 rejects a second
lifecycle authority. The selected design therefore reports `OwnerStopRequired`; it does not perform owner lifecycle.

Claude PR #1148 at inventory head `be6b2072b935e9fb072433313855606431ed841f` edits AgentRun. #759 does not edit
that file before merge/rebase/re-inventory. PR #1150 is merged at the rebase baseline; its examples remain #1145 scope.

## Current timing

| Binding | AckWait | BackOff | Heartbeat | Half-effective result |
|---|---:|---|---:|---|
| model | 120s | empty | 90s | invalid: greater than 60s |
| tools | 300s | 15s,60s | 120s | invalid: greater than 7.5s |
| dispatch pair | 30s effective | empty | 10s | valid |
| loop three | 90s | 30s,120s | 60s | invalid: greater than 15s |
| AgentRun pair | 30s | empty | 10s | valid |

BackOff overrides AckWait and is server missing-settlement policy. Current returned work errors separately use a fixed
30-second delayed NAK. Removing BackOff would lengthen tools crash redelivery 15s→300s and loop 30s→90s.

## Done/replay evidence

- Tools can replay a completed immutable outcome without executor invocation. Unknown outcome Create after execution
  remains ambiguous and requires the existing CallID idempotency contract plus #1155 reference proof.
- Dispatch can retry proven pre-publish failure. Unknown PubAck after publish invocation quarantines rather than
  entering its unlimited delivery posture. Deterministic MsgID dedup is bounded by the configured duplicates window.
- Model has no durable provider outcome or general provider idempotency; paid success plus publication ambiguity is
  non-authorizing.
- Loop void adapters and log-only durability errors do not establish per-binding durable completion or process
  rehydration; each lane is non-authorizing.
- AgentRun swallows partial product-handler failure and has no per-handler completion authority; both lanes remain
  non-authorizing after #1148 until fresh evidence.

## Adopter seam

The direct-helper adopter must define domain done and semantic failure class, pass the exact consumer config, and own
its existing exact handle. The framework observes effective lease, validates heartbeat, and applies the declared
semantic retry policy. It does not ask work to calculate AckWait, BackOff, attempt number, or server confirmation.

During staging, doing nothing preserves the deprecated legacy source and runtime behavior for allowlisted held callers.
New production legacy callers fail repository conformance. Compile failure occurs only at the final approved
zero-caller removal gate.

## DeliveryAttempt addendum — current checkpoint

- Repository head: `776505b3bb4ca4356bd8af6f4bbbcb0ce7610803`.
- `DeliveryWork` currently exposes only context and read-only bytes
  (`natsclient/delivery_settlement.go:28-31`).
- `ConsumeDeliveryWithHeartbeat` currently performs runtime policy defense, derives work context, reads
  `msg.Data()` once, launches work, heartbeats, cancels and joins, interprets, and settles
  (`natsclient/delivery_settlement.go:256-313`). It does not call `msg.Metadata()`.
- JetStream already owns the observation through `Msg.Metadata().NumDelivered`; no durable or model-private
  attempt state is required.
- Current typed production adopters are tools one binding
  (`processor/agentic-tools/component.go:424-426`) and dispatch complete and failed two bindings
  (`processor/agentic-dispatch/component.go:580-582,640-642`).
- A read-only sibling-repository scan found zero external adopters of `DeliveryWork`,
  `ValidateHeartbeatDeliveryPolicy`, or `ConsumeDeliveryWithHeartbeat`.
- Held model, loop, and AgentRun bindings remain unchanged. C8 decision/error and C9 read-only-byte decisions
  remain unchanged.

### Adopter seam

A component author receives an immutable delivery observation and need not predict or parse native JetStream
metadata. Existing callbacks fail at compile time until they accept the added value; they may ignore it. Missing,
erroneous, nil, or zero-number metadata fails before Data or work with typed
`DeliveryMetadataUnavailableError`, Quarantine, and `OwnerStopRequired`.

A redelivery observation is conservative. It does not prove prior work ran because the prior process may have
stopped before invocation. The observation carries no settlement, lifecycle, replay, checkpoint, or durable-state
authority.

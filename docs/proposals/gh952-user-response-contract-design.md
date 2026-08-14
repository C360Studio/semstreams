# GitHub #952 user-response contract design

## Accepted evidence and owner ruling

- SemStreams baseline: `ee1c07285537573b1d704f4711f8a73b1c8e54fd`.
- Inventory: `docs/proposals/gh952-user-response-contract-inventory.md`; immutable SHA-256 recorded in
  `openspec/changes/reserve-typed-user-response-subjects/checkpoint.sha256`.
- Complete design: this file; immutable SHA-256 recorded in the same checkpoint after materialization.
- Owner approval: 2026-08-13, binding the package recorded below.
- Cross-repo baselines: SemDev `fbb5c4cb69571c6b410c910d0dd910a652c44c3c`; SemTeams
  `c761d93d46f13c84354e409f80b89cdb2b39a5ff`.
- Decision record: ADR-093.

## Options considered

- **Do nothing:** keeps an observed ACK-drop because SemDev's flat parser definitively settles valid typed responses.
  Rejected.
- **Add a union decoder, bridge, dual subscription, or alias:** makes every adapter inherit all present and future
  product payloads, hides unmigrated writers, and cannot infer which external effect is intended. Rejected.
- **Rename every non-typed writer to a new subject:** removes the lexical collision but creates phantom governance and
  SemTeams families with no present delivery consumer. Rejected.
- **Reserve typed responses, create only the consumed SemDev request lane, and delete orphan writers:** gives each
  surviving family one payload and one present consumer while failing unmigrated authors loudly. Accepted.

## Applied decision skills

### `kv-or-stream`

SemDev park-post is a request to perform one external forge side effect. On restart it must resume unacknowledged
work, one durable worker should handle it, and a transient post failure must redeliver without replaying acknowledged
posts. All four tests select a JetStream Stream. KV Watch would provide no processing acknowledgement and Core NATS
would lose downtime work.

Typed `UserResponse` remains on its existing USER JetStream path. Message-logger remains a Core NATS observer of the
stream's live subject traffic; observation does not convert it into delivery ownership.

### `orchestration-check`

The rule detects a parked entity and emits a bounded request carrying a reference. The SemDev conversation-channel
component reads authoritative graph state and performs the forge post. Rules do not call the forge or carry the user
message, and the component does not decide when a run is parked. No workflow or lifecycle primitive is added.

### Payload registry check

No SemStreams payload type is added. Typed responses retain registered `agentic.user_response.v1`. SemDev's exact
product request is the existing raw rule-publish envelope and is named on both ports as interface
`semdev.park_post_request` version `v1`; it is deliberately not registered as a framework `BaseMessage` payload.

## D1 — Reserve `user.response.>` for one typed contract

Every message on `user.response.>` SHALL be a registered `BaseMessage` whose concrete payload is
`*agentic.UserResponse` and whose message type is `agentic.user_response.v1`. The resolved subject remains
`user.response.<channel_type>.<channel_id>`.

The reservation does not invent a delivery adapter or claim end-user delivery. A production SemTeams
message-logger fixture must prove registry decoding to concrete `*agentic.UserResponse`; this is decode and
observability evidence only.

The framework default and all eight shipped configurations that explicitly redeclare the dispatch output name
interface `agentic.user_response/v1`. The production-merge census must observe nine effective typed declarations:
eight explicit plus the default-only ninth.

No raw envelope, `core.GenericJSON`, `TaskMessage`, bridge wrapper, or product request may target the family.

## D2 — Reject all arbitrary rule publishers twice

The rule engine SHALL use one private subject-family classifier for all three arbitrary subject-bearing actions:
`publish`, `publish_agent`, and `approve`.

1. Definition validation rejects a literal or templated subject whose fixed tokens already target
   `user.response.>`, including `user.response.$entity.instance`.
2. Each action checks its fully substituted concrete subject immediately before constructing or publishing bytes.
   This catches a wholly dynamic subject that resolves into the reserved family.

The post-substitution check is mandatory even when a caller invokes `ActionExecutor` directly and bypasses config
load. Rejection happens before any publisher call, graph audit side effect specific to the action, or task mint. A
configuration error identifies the rule, action list, index, action type, and forbidden family; a runtime error names
the action type and resolved subject without including message content.

The reservation is token-aware: it covers subjects matching `user.response.>` and does not reject unrelated prefixes
such as `user.responses.*`. No exported subject registry, adopter allowlist, override knob, or new rule action is
added.

## D3 — Give SemDev one exact JetStream request

SemDev owns exact subject:

```text
semdev.park-post.request
```

The corresponding port interface is:

```text
type: semdev.park_post_request
version: v1
```

The wire remains raw JSON produced by the rule `publish` action. Before disposition, the v1 consumer validates the
complete product contract: `entity_id` is canonical and non-empty, envelope `subject` equals
`semdev.park-post.request`, `timestamp` parses as RFC3339, and `source` equals `rule_engine`. `properties` and
`related_id` remain optional. The exact subject supplies the operation identity, so no second payload discriminator
or payload-registry entry is added.

All nine SemDev park rules move to the exact subject. Both shipped SemDev rule-component configs declare an exact
JetStream output named `park_post_requests` on stream `USER` with the interface above. This causes the existing rule
publisher to call `PublishToStream` and observe broker acceptance instead of using Core NATS.

Both SemDev USER stream catalogs explicitly add `semdev.park-post.request` alongside `user.>`. Both
conversation-channel configs replace broad `user_responses/user.response.>` with exact JetStream input
`park_post_requests/semdev.park-post.request`, stream `USER`, and the same interface. The renamed input creates a new
durable identity `conversation-channel-park_post_requests`; routing is exact equality, not prefix dispatch.

The producer port, stream catalog, input port, durable, raw decoder, all nine rules, configuration comments, port
manifest, and contract/E2E fixtures move in one SemDev change. A missing exact stream subject or either port fails
boot or contract validation; no Core fallback is accepted.

The current park fact remains authoritative and is committed before the notification request. Transient graph/forge
failure NAKs for bounded redelivery; definitive malformed/no-channel/no-target cases retain their existing ACK and
visible graph-only behavior. #952 does not change post deduplication or delivery bounds.

## D4 — Remove governance's orphan notification surface

Agentic-governance removes, without replacement:

- `ViolationConfig.NotifyUser` and its generated schema field;
- optional `user_errors` output port;
- the conditional notification branch;
- the flat notification constructor and publisher; and
- shipped `violations.notify_user` configuration and documentation.

Violation logging, bounded metrics, admin severity alerts, and the durable `governance.violation.*` event remain
unchanged. The implementation-time real-NATS preservation test found that the prior KV key `violation:<id>` was
always rejected because NATS KV does not permit `:`. Owner correction on 2026-08-14 binds the fresh-state repair to
canonical valid key `violation.<id>`, checked through the shared `natsclient.ValidateKVLiteralKey` boundary before
bucket lookup or any NATS I/O. No compatibility reader or state conversion is added because the invalid key never
persisted a record.

Because ordinary struct decoding cannot distinguish omission from `null`, `NewComponent` first inspects raw JSON for
the exact nested key `violations.notify_user`. Any presence, including `false`, `true`, or `null`, returns a targeted
breaking-migration error before default merge, port resolution, filter construction, or NATS I/O. There is no
deprecated field, ignored spelling, alias, new governance-owned subject, or automatic conversion to typed
`UserResponse` because governance has no present delivery reader or channel address contract.

## D5 — Re-pin message-logger measured truth

Removing the one default-only governance `user_errors` output changes the 21-config census as follows:

| Measurement | Current | Target | Delta |
|---|---:|---:|---:|
| Raw rows / per-config keys / global strings | 395 / 243 / 54 | 395 / 243 / 54 | 0 / 0 / 0 |
| Effective rows / per-config keys / global strings | 580 / 380 / 70 | 579 / 380 / 70 | -1 / 0 / 0 |
| Effective-minus-raw rows / keys / strings | 185 / 137 / 16 | 184 / 137 / 16 | -1 / 0 / 0 |
| Exact collapses | 48 | 47 | -1 governance collapse |

The target attribution is 47 loop/dispatch collapses and zero governance collapses. Added `nats_outputs` changes
from 28 to 27; the other added-kind counts remain unchanged. This is a measured default-declaration removal, not a
raw-config removal: the shipped governance config already omits `user_errors`, while dispatch retains the same exact
subject key.

The census artifact and assertions update in the implementation. No message-logger behavior, subscription, buffer,
or query API changes.

## D6 — SemTeams keeps typed producers and deletes two flat actions

SemTeams retains its typed command producers, dispatch typed response path, USER stream, and message-logger observer.
It deletes exactly the `publish` action from:

- `configs/rules/coordinator/03-ask-user.json`; and
- `configs/rules/coordinator/03b-respond-direct.json`.

The audit triples and coordinator semantics remain; no replacement signal subject is added because neither flat
action has a current delivery consumer. Stale comments, contract expectations, and E2E assertions describing the
flat bus must be removed or redirected to real typed dispatch evidence.

SemTeams adoption is blocked until its source/config census shows no flat writer under `user.response.>`, a
production typed fixture is decoded by message-logger as concrete `*agentic.UserResponse`, and its contract and
relevant E2E suites are green. Message-logger proof must not claim external delivery.

## D7 — Fresh-state breaking cut

SemStreams and SemDev land in breaking lockstep, and SemTeams cannot adopt the SemStreams version until its companion
cleanup is green. Deployment starts from newly provisioned NATS state after all owned sources, stream catalogs,
ports, configs, schemas, docs, fixtures, and tests are updated.

There is no legacy reader, flat/typed union, dual format, dual subscription, alias, bridge, subject forwarding,
online conversion, retained-state migration, or rollback lane. No prior `user.response.*` park delivery is replayed
onto the new subject. If retained deployed state is discovered as a requirement, landing stops for a separate
owner-reviewed migration design.

## D8 — Acceptance and landing gates

### SemStreams

- Unit and race tests prove static and post-substitution rejection for `publish`, `publish_agent`, and `approve`,
  including direct executor calls and no publisher side effect.
- Governance tests prove `notify_user` is rejected for boolean and `null`, and prove audit/admin behavior remains,
  including real-NATS storage under `violation.<id>` with no invalid-key compatibility path.
- Schema generation removes the field and `user_errors` port with no unrelated drift.
- The repository subject/payload census reports one contract on `user.response.>`.
- The measured message-logger census matches D5.
- `task lint`, `go test -race ./...`, `task schema:generate`, `go test ./test/contract/...`, and strict OpenSpec pass.
- `task e2e:agentic` proves a production typed response on the reserved subject before the breaking commit lands.

### SemDev

- Contract tests prove both USER catalogs include the exact subject and both producer/consumer ports name the exact
  JetStream subject plus `semdev.park_post_request/v1`.
- A raw park fixture is decoded only by the product reader; a typed `UserResponse` fixture is rejected by that reader
  and never treated as a park request.
- The new durable consumes the exact subject, transient post failure redelivers, and the nine-rule census is exact.
- The end-to-end park-post proof is green before the breaking lockstep merge.

### SemTeams

- The two flat actions are absent and no replacement flat writer exists.
- Typed dispatch plus message-logger proves concrete `*agentic.UserResponse` decoding.
- Contract tests and relevant UI/E2E expectations are green before adoption.

## Adopter seam

The adopter is a developer outside SemStreams implementing one product channel component.

### What must they know?

The one subject and named payload contract their component owns. A typed response adapter knows
`agentic.user_response.v1`; the SemDev component knows exact `semdev.park_post_request/v1`.

### What happens if they do nothing?

An unmigrated rule definition or governance config fails at boot; a missing SemDev stream/port contract fails
validation or boot. No tolerant decoder hides the break.

### Where do they find out?

Config validation and boot errors first, then named port interface contracts, ADR-093, the capability spec, and
release notes.

### What should they have to know?

Nothing about other products' envelopes or the framework's writer implementation. They should not predict payload
class from bytes or subscribe broadly and branch.

The framework observes the actual final rule subject after substitution and rejects the incompatible operation. The
adopter does not need to predict whether a template will collide.

## Non-goals

- Building a general subject namespace registry or NATS account ACL system.
- Adding a user-response delivery adapter, UI router, email/SMS integration, or delivery receipt.
- Converting governance violations into typed user responses.
- Adding a SemTeams replacement notification bus.
- Registering SemDev product payloads in the SemStreams payload registry.
- Changing SemDev park facts, forge-post content, retry limits, or post deduplication.
- Migrating retained messages or supporting mixed-version deployments.

## Owner-ruling conformance scaffold

The implementation evidence is durable at
`openspec/changes/reserve-typed-user-response-subjects/implementation-evidence.md`. Pending rows name the absent
SemTeams, tag, or adoption artifact explicitly; they are not inferred complete.
Below, `evidence:Lx-Ly` means exact lines in that implementation-evidence artifact.

| # | Binding ruling | Implementation evidence |
|---:|---|---|
| 1 | Typed family only | VERIFIED — `evidence:17-38` |
| 2 | Static rejection: three actions | VERIFIED — `evidence:40-47` |
| 3 | Runtime rejection: three actions | VERIFIED — `evidence:40-47` |
| 4 | Exact SemDev subject | VERIFIED — `evidence:71-85` |
| 5 | Raw SemDev interface, unregistered | VERIFIED — `evidence:71-85` |
| 6 | Both SemDev USER catalogs | VERIFIED — `evidence:78-81` |
| 7 | JetStream output and new durable | VERIFIED — `evidence:78-85` |
| 8 | Nine rules and strict raw reader | VERIFIED — `evidence:71-85` |
| 9 | Typed observation, not delivery | VERIFIED — `evidence:35-38` |
| 10 | Governance writer/port removed | VERIFIED — `evidence:49-63` |
| 11 | Any retired-key presence fails | VERIFIED — `evidence:53-55` |
| 12 | Governance behavior and valid KV key | VERIFIED — `evidence:56-63` |
| 13 | Correct measured census | VERIFIED — `evidence:65-69` |
| 14 | SemTeams removes two flat actions | **PENDING — implementation.** |
| 15 | SemTeams gates before adoption | **PENDING — contract/E2E/adoption.** |
| 16 | No compatibility path | SemStreams/SemDev VERIFIED — `evidence:62-85`; **PENDING — SemTeams census.** |
| 17 | Both required E2Es green | VERIFIED — `evidence:87-107`; **PENDING — tag/adoption.** |

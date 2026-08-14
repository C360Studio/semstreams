# GitHub #952 user-response contract inventory

## Checkpoint identity

- SemStreams baseline: `ee1c07285537573b1d704f4711f8a73b1c8e54fd`.
- SemDev inventory baseline: `fbb5c4cb69571c6b410c910d0dd910a652c44c3c`.
- SemTeams inventory baseline: `c761d93d46f13c84354e409f80b89cdb2b39a5ff`.
- Issue: [C360Studio/semstreams#952](https://github.com/C360Studio/semstreams/issues/952).
- This inventory's SHA-256 is recorded in
  `openspec/changes/reserve-typed-user-response-subjects/checkpoint.sha256` after materialization.

This is the inventory-only checkpoint. It reports current owners and collisions; target-state decisions are recorded
separately in `gh952-user-response-contract-design.md`.

## Problem statement checked

The issue claims that `user.response.>` carries incompatible production contracts. Repository-first enumeration
confirmed three SemStreams writers or writer classes and two adopter-specific flat rule families:

1. agentic-dispatch publishes a registered `BaseMessage<agentic.UserResponse>`;
2. arbitrary rule actions can publish three incompatible payload classes to any resolved subject;
3. governance can publish an unregistered flat error notification;
4. SemDev uses nine flat rule actions as externally effective park-post requests; and
5. SemTeams uses two flat rule actions as unconsumed wake-up signals while also producing typed responses.

## Surface inventory

### 1. Claimed gap and current typed contract

`agentic.UserResponse` already exists. It requires `ResponseID`, `ChannelType`, `ChannelID`, and response `Type`, with
optional `UserID`, and reports schema `agentic.user_response.v1`: `agentic/user_types.go:178-235`. The payload is
registered by the framework payload bootstrap at `agentic/payload_registry.go:19-25`.

agentic-dispatch declares `user.response.>` as a USER-stream JetStream output at
`processor/agentic-dispatch/config.go:85-98`. Its production send path wraps `UserResponse` in `BaseMessage`, resolves
`user.response.<channel_type>.<channel_id>`, and calls `PublishToStream` at
`processor/agentic-dispatch/component.go:928-948`. Command responses, including product-provided command executors,
converge on that send path at `processor/agentic-dispatch/component.go:658-674`.

No subject-ownership validator currently prevents another writer class from targeting the same family. The rule
definition validator walks every action list at `processor/rule/config_validation.go:307-414`, but its checks do not
classify reserved publish subjects. A runtime subject can still be produced only after template substitution.

### 2. Every current spelling of the modeled fact

#### Arbitrary SemStreams rule publishers

The rule action surface defines three subject-bearing arbitrary publishers:

- `publish` emits a flat JSON map with `entity_id`, resolved `subject`, `timestamp`, `source`, `properties`, and
  optional `related_id`: `processor/rule/actions.go:872-934`.
- `publish_agent` emits a registered `BaseMessage<agentic.TaskMessage>`:
  `processor/rule/actions.go:1395-1416`, `:1717-1730`.
- `approve` emits a registered `BaseMessage<core.GenericJSON>` verdict:
  `processor/rule/actions.go:1836-1856`, `:1900-1913`.

All three call the same `Publisher`. The rule processor installs `actionPublisher` at
`processor/rule/processor.go:686-704`. That publisher selects JetStream only when a configured output port matches the
resolved subject; otherwise it uses Core NATS: `processor/rule/publisher.go:26-50`.

Search used to enumerate the class:

```text
rg -n 'ActionTypePublish|ActionTypePublishAgent|ActionTypeApprove|publisher.Publish' processor/rule
```

It returned the three execution paths above. No fourth arbitrary subject-bearing rule action was found.

#### Governance flat notification

`ViolationConfig.NotifyUser` is a default-true `notify_user` field at
`processor/agentic-governance/config.go:85-92`; `DefaultConfig` also declares optional Core NATS output `user_errors`
on `user.response.*` at `processor/agentic-governance/config.go:203-224` and defaults notification on at `:273-279`.

For non-shadow violations, `Handle` conditionally calls `notifyUser` while independently retaining logging, metrics,
KV storage, admin alerting, and the durable governance violation event:
`processor/agentic-governance/violation.go:92-172`. `notifyUser` emits the incompatible flat
`{type,timestamp,message,severity,details}` object at `processor/agentic-governance/violation.go:192-216`.

Configuration currently unmarshals directly into a defaulted struct at
`processor/agentic-governance/component.go:56-75`. Ordinary `encoding/json` decoding does not retain whether a key
was absent or explicitly supplied as `null`; that distinction matters if a retired key must fail loudly.

The only shipped component configuration enabling agentic-governance is
`configs/flows/deep-research.json:360-463`; it explicitly sets `violations.notify_user` at `:374-378` but does not
declare `user_errors` in its raw ports.

#### Message-logger typed observation

The production message-logger constructor installs a registry-backed `message.Decoder` at
`service/message_logger.go:23-70`. It first attempts typed `BaseMessage` decoding and otherwise records raw JSON at
`service/message_logger.go:764-799`; its typed summary exposes the concrete payload Go type at `:836-845`.

This is an observer and diagnostic reader. It does not ACK a delivery request, invoke an external channel, or own
user-response settlement. SemTeams enables it on `user.>` in both shipped configurations:
`semteams@c761d93d:configs/flow-bootstrap.json:133-146` and
`semteams@c761d93d:configs/e2e-flow-bootstrap.json:98-111`. SemTeams registers framework built-in payloads before
services are constructed at `semteams@c761d93d:cmd/semteams/main.go:860-876`, so a valid response is decoded as
concrete `*agentic.UserResponse` rather than merely accepted as JSON.

The current 21-config SemStreams census is pinned by `service/message_logger_census_test.go:21-148` and
`service/testdata/message_logger_subject_census.json:1-55`:

| Measurement | Raw | Effective | Effective-minus-raw |
|---|---:|---:|---:|
| Rows | 395 | 580 | 185 |
| Per-config exact keys | 243 | 380 | 137 |
| Global strings | 54 | 70 | 16 |

The same artifact records 48 exact collapses: 47 loop/dispatch and one governance. The governance collapse is the
default-only `user_errors` row colliding with the explicit dispatch `user.response.*` row in the sole
governance-enabled configuration.

### 3. Adopter writers and readers

#### SemDev beta.160

Nine enabled rule files publish the flat rule envelope to `user.response.$entity.instance`:

- `semdev@fbb5c4c:configs/rules/run-lifecycle/03-park-awaiting-human.json:23`;
- `semdev@fbb5c4c:configs/rules/run-lifecycle/05-park-station-failure-run.json:30`;
- `semdev@fbb5c4c:configs/rules/run-lifecycle/06-park-station-failure-loop.json:29`;
- `semdev@fbb5c4c:configs/rules/sandbox/02-park-unprovable.json:23`;
- `semdev@fbb5c4c:configs/rules/dev-from-task/06d-route-escalate.json:30`;
- `semdev@fbb5c4c:configs/rules/dev-from-task/06g-route-transient-park.json:30`;
- `semdev@fbb5c4c:configs/rules/dev-from-task/07c-review-park.json:29`;
- `semdev@fbb5c4c:configs/rules/dev-from-task/07d-review-no-verdict.json:30`; and
- `semdev@fbb5c4c:configs/rules/dev-from-task/08b-delivery-park.json:28`.

Both SemDev stream catalogs declare USER as only `user.>`:
`semdev@fbb5c4c:configs/semdev-bootstrap.json:53-60` and
`semdev@fbb5c4c:configs/semdev-live-gemini.json:53-60`. Both rule components omit an output port:
`semdev@fbb5c4c:configs/semdev-bootstrap.json:195-250` and
`semdev@fbb5c4c:configs/semdev-live-gemini.json:211-266`. The rule publisher therefore uses Core NATS; USER captures
the publish only incidentally because `user.>` overlaps the subject. The writer receives no JetStream PubAck.

Conversation-channel declares a broad USER-stream `user.response.>` input at
`semdev@fbb5c4c:internal/conversationchannel/component.go:150-187`, creates a bounded durable at `:414-484`, and
dispatches by prefix at `:487-504`. Its parser accepts only the flat rule envelope and ACK-skips an empty
`entity_id`: `semdev@fbb5c4c:internal/conversationchannel/parkpost.go:16-70`. A valid typed BaseMessage therefore
unmarshals into that struct with empty `EntityID` and is definitively settled without delivery.

The raw rule envelope already carries `entity_id`, `subject`, `timestamp`, `source`, optional `properties`, and
optional `related_id`, but the current reader does not validate the complete product contract. The approved target
closes that gap: `entity_id` must be canonical and non-empty, envelope `subject` must equal
`semdev.park-post.request`, `timestamp` must parse as RFC3339, and `source` must equal `rule_engine`. `properties` and
`related_id` remain optional. This remains a raw SemDev contract, not a registered SemStreams BaseMessage payload.

#### SemTeams beta.159

SemTeams has typed product command producers. Team-hint returns a populated `agentic.UserResponse` at
`semteams@c761d93d:cmd/semteams/commands/teamhint/command.go:64-119`; implement-spec has the same typed return shape at
`semteams@c761d93d:cmd/semteams/commands/implementspec/command.go:291-302`. Dispatch publishes returned command
responses through the framework typed send path. The implement-spec command is present in source but was not found in
the live registration census; this does not change its payload class.

SemTeams also has exactly two flat rule writers:

- `semteams@c761d93d:configs/rules/coordinator/03-ask-user.json:25-44`; and
- `semteams@c761d93d:configs/rules/coordinator/03b-respond-direct.json:25-45`.

Both files state that no delivery consumer exists and direct operators to observe the signal manually. The shipped
dispatch override remains Core NATS `user.response.*` at
`semteams@c761d93d:configs/flow-bootstrap.json:520-533`; typed payload shape is still supplied by dispatch. The
message-logger path above is typed decode evidence, not user delivery evidence.

#### Remaining beta.160 cohort

The command

```text
rg -n --hidden --glob '!vendor/**' --glob '!.git/**' \
  'user\.response|UserResponse|CategoryUserResponse|notify_user|user_errors' <repo>
```

returned zero matches for SemBoids `8c03cc53836c`, SemMachina `841c45e8bb01`, and SemSource `4093d3ce4213`.
SemConnect `d0d06e00bf05` returned only captured historical backend logs containing generated SemStreams schema; no
SemConnect source, configuration, or live test owns the family.

### 4. Adjacent claims on the territory

- GitHub #952 defines the breaking split and forbids a union decoder, dual subscription, alias, or bridge.
- `openspec/changes/normalize-agent-terminal-settlement/` owns typed terminal-to-response settlement and explicitly
  leaves heterogeneous subject ownership to #952.
- `docs/proposals/gh865-866-terminal-event-inventory.md:318-432` previously measured the collision and the SemDev
  ACK-drop. This inventory re-derived the writers and readers against the newer baseline.
- Pre-v1 fresh-state policy in `.agents/contracts/semstreams-architect.md` requires all owned sources, configs,
  fixtures, and readers to move together without a compatibility path.
- The current message-logger capability is observation only; delivery responsibility is outside that capability.

## Same-class collision table

The table is expanded by dimension so every collision remains line-addressable without hiding detail in wide cells.

### Semantic class and owners

- Typed family: channel-addressed response, owned by agentic-dispatch.
- SemDev: request for one external thread post, owned by SemDev rules and conversation-channel.
- Governance: user-facing policy error, owned by agentic-governance.
- SemTeams: wake-up for an unspecified future router, owned by coordinator rules.

### Catalogs and status

- Typed family catalogs are the dispatch output, USER stream, and payload registry. Dispatch logs ordinary publish
  failure; the terminal lane also has settlement telemetry.
- SemDev catalogs are nine rules, two USER declarations, and the conversation input. Conversation health/error
  counters report handling, while the graph park fact exists before the post.
- Governance catalogs are its schema and default output. Logs, metrics, KV audit, admin alerts, and violation events
  report policy handling.
- SemTeams catalogs are two rules, dispatch/USER configuration, and message-logger subjects. Only diagnostic entries
  report the flat signal.

### Lifecycle and ownership

- Typed responses are produced per command or terminal result. Dispatch owns projection; no delivery adapter was
  found. The terminal path retries while retained, but an ordinary command response has no delivery ACK.
- SemDev uses durable bounded redelivery. The post is downstream of the durable park fact, and SemDev owns both the
  product policy and external side effect.
- Governance uses fire-and-forget Core publication. Governance owns policy/audit, but no notification reader exists.
- SemTeams flat signals are fire-and-forget. SemTeams owns coordinator policy, but no delivery owner exists.

### Readers and writers

- Typed family: dispatch and product command returns write; message-logger reads diagnostically.
- SemDev: nine rule `publish` actions write; conversation-channel reads and performs the external effect.
- Governance: `notifyUser` writes; no shape-specific reader was found.
- SemTeams: two rule `publish` actions and typed commands write; message-logger only observes.

The collision is semantic, not merely lexical. Typed responses, park-post external-effect requests, orphan policy
notifications, and unconsumed wake-up signals cannot share one subject family without forcing readers to predict the
payload class.

## Adopter seam inventory

The specific adopter is a product developer implementing a channel adapter without opening SemStreams rule or
governance internals.

### What must they know?

They must know that `user.response.>` can be a registered typed response, a flat rule envelope, or a flat governance
error. They must also know which products overload it and whether a matching message represents delivery work or
only a diagnostic signal.

### What happens if they do nothing?

A decoder for one shape can ACK-drop another. SemDev does this to valid typed responses today. A broad union decoder
would hide new collisions and cannot infer which external side effect is intended.

### Where do they find out?

Today the facts are split across source, product rule files, port configs, and issue #952. The failure appears only as
a log from the wrong decoder, below the correctness threshold.

### What should they have to know?

One subject family, one named payload contract, and whether the lane is delivery work. They should not inspect bytes
to infer product ownership or choose between typed and flat decoders.

The current surface asks the caller to predict a fact the framework and product already own: which contract a subject
means. That prediction is the design gap. The framework can reject incompatible generic writers, while each product
can own its own action subject and delivery component.

## Consumer-at-birth findings

- The typed `UserResponse` contract has present producers and a typed diagnostic reader, but no production delivery
  adapter was found. Message-logger evidence must not be relabeled as delivery evidence.
- SemDev's park-post request has one present effectful reader and therefore qualifies for a product-owned request
  lane.
- Governance's flat user notification has no present reader. A replacement subject or second payload would be a
  phantom surface.
- SemTeams' two flat signals have no present delivery reader. Their current authorship cannot justify another subject
  family.

## Inventory measurements and closed searches

- SemStreams source/config census:

  ```text
  rg -n 'user\.response|UserResponse|CategoryUserResponse|notify_user|user_errors' \
    agentic processor service configs test
  ```
- Rule publisher census:
  `rg -n 'ActionTypePublish|ActionTypePublishAgent|ActionTypeApprove|publisher.Publish' processor/rule`.
- Adopter cohort census: the command recorded under "Remaining beta.160 cohort".
- No `user.response` repair, bridge, alias, dual-subscription, or product delivery adapter was found with
  `rg -n 'user\.response|response.*bridge|dual.*response|response.*alias'` across SemStreams and the enumerated
  adopters.

# Change: Reserve typed user-response subjects

## Why

`user.response.>` currently carries three incompatible SemStreams payload classes and two adopter flat-rule uses.
SemDev beta.160 durably consumes the family as park-post work but decodes only the flat rule envelope; it therefore
ACK-drops valid registered `agentic.UserResponse` BaseMessages. A tolerant union decoder would preserve the collision
and make every adapter understand every product.

The accepted inventory and design are recorded in:

- `docs/proposals/gh952-user-response-contract-inventory.md`;
- `docs/proposals/gh952-user-response-contract-design.md`; and
- ADR-093.

Their immutable hashes and SemStreams baseline `ee1c07285537573b1d704f4711f8a73b1c8e54fd` are recorded in
`checkpoint.sha256`.

## What Changes

- Reserve `user.response.>` for registered `BaseMessage<agentic.UserResponse>` with type
  `agentic.user_response.v1`; type the default output and all eight explicit shipped declarations, yielding nine
  effective typed declarations after production merge.
- Reject `publish`, `publish_agent`, and `approve` rule actions statically and after subject substitution when they
  target the reserved family.
- In lockstep, move SemDev's nine park rules and exact durable reader to JetStream subject
  `semdev.park-post.request`, named on both ports as raw product interface `semdev.park_post_request/v1`, and add the
  exact subject to both SemDev USER stream catalogs. Its raw reader validates a canonical non-empty `entity_id`,
  envelope `subject` exactly `semdev.park-post.request`, an RFC3339 `timestamp`, and `source` exactly `rule_engine`;
  `properties` and `related_id` remain optional.
- Remove governance's orphan user-notification writer, `user_errors` port, and `notify_user` configuration without a
  replacement; reject any raw presence of `notify_user`, including `null`; and repair its audit key from the
  NATS-invalid `violation:<id>` spelling to canonical `violation.<id>`, validated at the shared pre-I/O KV boundary,
  without a compatibility reader because the old key never persisted.
- Re-pin the message-logger declaration census after the one default-only governance output disappears.
- Require SemTeams to retain typed response producers/observation while deleting exactly two unconsumed flat rule
  actions before adopting the breaking SemStreams version.
- Use a pre-v1 fresh-state cut with no compatibility path.

## Impact

- Modified SemStreams surfaces: rule definition/execution validation, agentic-governance configuration and outputs,
  governance audit key, generated governance schema, shipped deep-research configuration, message-logger census,
  agentic E2E, and docs.
- Cross-repo dependency: SemDev exact-subject producer/stream/consumer migration at baseline
  `fbb5c4cb69571c6b410c910d0dd910a652c44c3c`.
- Adoption dependency: SemTeams flat-action cleanup and typed observation proof at baseline
  `c761d93d46f13c84354e409f80b89cdb2b39a5ff`.
- Breaking landing gate: SemStreams `task e2e:agentic` and SemDev end-to-end park-post proof must be green before the
  breaking commits land.
- Measured census target: raw 395/243/54 unchanged; effective 579/380/70; delta 184/137/16; 47 exact collapses,
  all loop/dispatch and none governance.

## Non-goals

- No generic subject namespace registry or NATS ACL framework.
- No user-response delivery adapter, UI router, or delivery receipt.
- No governance replacement notification or SemTeams replacement bus.
- No SemDev payload registration in SemStreams.
- No union decoder, bridge, alias, dual subscription, forwarding subject, retained-state conversion, or mixed-version
  support.
- No change to SemDev park facts, forge-post content, retry limits, or deduplication.

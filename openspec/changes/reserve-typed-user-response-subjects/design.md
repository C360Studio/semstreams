# Design: Typed user-response subject ownership

## Accepted records

- Baseline: `ee1c07285537573b1d704f4711f8a73b1c8e54fd`.
- Inventory and adopter seam: `docs/proposals/gh952-user-response-contract-inventory.md`.
- Complete owner-approved design and ruling scaffold:
  `docs/proposals/gh952-user-response-contract-design.md`.
- Cross-repo decision: `docs/adr/093-typed-user-response-subject-ownership.md`.
- Immutable artifact hashes: `checkpoint.sha256`.

The complete design is incorporated by reference. This change-local document records the implementation boundaries
needed to apply and archive the delta.

## One typed family

`user.response.>` carries only registered `agentic.user_response.v1` BaseMessages with concrete
`*agentic.UserResponse`. The reservation is a payload ownership rule; it does not claim that a diagnostic observer is
an end-user delivery adapter.

The dispatch default and eight explicit shipped output declarations name `agentic.user_response/v1`; the
production-merge census requires all nine effective declarations to retain that interface.

One private token-aware rule helper is consumed by `publish`, `publish_agent`, and `approve`. Definition validation
rejects known literal/template prefixes, and each execution path rejects the fully substituted subject before bytes
or action-specific side effects are emitted. No exported subject registry or configuration knob is added.

## Product request and communication primitive

The `kv-or-stream` restart, queue, side-effect, and nature tests all select JetStream for SemDev park-post work.
Exact subject `semdev.park-post.request` and interface `semdev.park_post_request/v1` appear on:

- both SemDev USER stream catalogs;
- both rule-component exact JetStream outputs;
- both conversation-channel exact JetStream inputs; and
- all nine park rules and their fixtures.

The input rename creates durable `conversation-channel-park_post_requests`. The payload remains the raw rule-publish
envelope and is not registered in SemStreams. The reader validates a canonical non-empty `entity_id`, envelope
`subject` exactly `semdev.park-post.request`, an RFC3339 `timestamp`, and `source` exactly `rule_engine`;
`properties` and `related_id` are optional. Per `orchestration-check`, the rule carries a parked entity reference;
conversation-channel owns graph lookup and the external post.

## Delete zero-consumer surfaces

Governance has no reader capable of delivering its flat user error, so the writer, output, and knob are deleted
without replacement. A raw-map preflight detects exact nested key `violations.notify_user` before defaulted decoding;
every value, including `null`, fails with a migration error. Audit/admin behavior is preserved. A real-NATS
implementation test measured the old KV key `violation:<id>` as invalid; the owner-approved fresh-state correction
uses `violation.<id>` and the shared KV literal-key validator before bucket lookup or NATS I/O. No compatibility
reader or conversion exists because the rejected key never persisted.

SemTeams' two flat rule actions also have no delivery consumer and are deleted without a replacement subject. Its
typed command responses and message-logger observer remain. Adoption waits for contract and relevant E2E proof.

## Measured declaration change

The sole governance-enabled shipped configuration explicitly declares no `user_errors` port, so raw census numbers
stay 395 rows, 243 per-config keys, and 54 global strings. Removing the default output subtracts one effective NATS
output row but no unique key:

- effective: 579 rows / 380 keys / 70 strings;
- effective-minus-raw: 184 rows / 137 keys / 16 strings;
- added NATS outputs: 27; and
- exact collapses: 47 loop/dispatch, zero governance.

## Breaking landing

The cut is fresh-state and lockstep. There is no compatibility or state migration surface. SemStreams agentic E2E
and SemDev park-post E2E must be green before breaking landing. SemTeams is an adoption gate, not a reason to add a
temporary bridge.

## Ruling evidence

The seventeen-row conformance scaffold now links exact file:line evidence and names SemTeams/tag/adoption gaps as
explicit `PENDING` work. `implementation-evidence.md` preserves gate outcomes and review provenance.

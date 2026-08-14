# Migrating beta.160 user-response subjects

The release after beta.160 makes `user.response.>` a single-contract subject family. Every message in that family is
a registered `agentic.user_response.v1` `BaseMessage`; generic rule publishers and governance notifications may no
longer target it.

This is a fresh-state, lockstep migration. Stop the deployment, update SemStreams and SemDev together, provision new
NATS state, and then start the updated components. Do not retain or forward old park-post messages from
`user.response.*`.

SemDev PR #6 completed the one-time coordinated #952 migration. That exception is closed and is not a precedent.
After this cut, SemStreams agents treat SemDev, SemTeams, and every other sister repository as read-only: they may
inventory downstream impact, but downstream owners must implement these instructions, run their native gates, and
publish their own changes. SemStreams agents must not mutate sister-repository branches, files, GitHub state, tags,
or releases.

## Required adopter changes

- Delete `violations.notify_user` from every agentic-governance configuration. Any presence, including `false` or
  `null`, now fails boot. Governance audit storage, metrics, logging, admin alerts, and `governance.violation.*`
  events remain available. Governance audit records use valid KV key `violation.<id>`; the retired
  `violation:<id>` spelling never persisted because NATS KV rejects `:`, so there is no legacy record to read or
  convert. The new key is checked by the shared KV literal-key validator before bucket lookup or NATS I/O.
- Preserve `agentic.user_response/v1` on every explicit dispatch `user.response` output override. SemStreams ships
  eight explicit declarations plus the default-only ninth; the production census must report all nine as typed.
- Move SemDev park-post producers and the conversation-channel consumer to exact JetStream subject
  `semdev.park-post.request` and interface `semdev.park_post_request/v1`. Use SemDev's matching release; do not copy
  the product payload contract into SemStreams.
- Remove SemTeams' two unconsumed flat coordinator actions before adopting this SemStreams version. Typed dispatch
  responses and message-logger observation remain.
- Move every remaining `publish`, `publish_agent`, or `approve` rule action away from `user.response.>`. Fixed-prefix
  definitions fail during rule validation; wholly dynamic templates fail after substitution before publication.

There is no legacy reader, union decoder, alias, bridge, forwarding subject, dual subscription, retained-state
conversion, or mixed-version compatibility lane. A message-logger entry proves registry decoding and observation; it
does not prove end-user delivery.

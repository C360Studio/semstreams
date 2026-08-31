# Change: The platform authority is unique by default

**Scope: Case B only, knobless** — owner scope cut on #1168, 2026-08-30. Design:
`docs/proposals/gh1168-federation-identity-design.md`; premises pinned in
`docs/proposals/gh1168-federation-identity-pins.md`; inventory
`docs/proposals/gh1168-federation-identity-inventory.md` (`5967394f`, sha256 `7ec8c088…`, INVENTORY PASS WITH
DIVERGENCES — its Case A and import-lane sections describe surfaces this change no longer touches). ADR draft:
ADR-104.

Rehomed by the same cut and **not** in this change: the framed-digest run identity → **#1192**; the import-lane
collision gate → **#1194**; the `run_scope=new` anchor-append bug → **#1193**; the environment-override surface →
**#1186**; the three zero-caller deletions → **#1187**.

## Why

One measured fact on `main@48a84641` (inventory §1, design §1):

`platform.org` / `platform.id` are positions 1–2 of every identity this deployment mints, and they are validated for
shape and byte budget only. Two deployments provisioned from one configuration template therefore mint under one
authority, and ADR-102 d7 makes that permanent rather than repairable — the only detector, `local_authority_claimed`,
fires after the two have already collided inside one graph. A first-boot persist-and-compare mechanism already exists
(`config.Manager.Start`, bucket `semstreams_config`); it persists a value the file holds and never mints one.

## What Changes

- **BREAKING:** `platform.id` gains a framework-minted entropy suffix (`-` plus six lowercase hex bytes from
  `crypto/rand`) on a deployment's genuine first boot. It is persisted with an atomic `Create` as the
  `semstreams_config/platform_identity` record `{org, stem, id}` and adopted by every later boot and co-process on
  that bucket.
- **Identity is established before arbitration, from one bucket read, in three branches** — adopt the record if
  present; mint only when the bucket is otherwise empty; refuse Start naming the cause, minting nothing and creating
  nothing, when the bucket holds configuration but no identity record.
- **No new configuration key.** The opt-out is operational, not documentary: an operator who owns global uniqueness
  pre-creates the record with `id == stem`, and the adopt branch validates it under the same charset,
  subject-safety and budget rules as a configuration value.
- Four existing config-manager paths are closed so the record cannot be clobbered or misread: the KV `platform`
  key becomes a read-only mirror (`updateConfig`'s `case "platform"` arm is deleted); first-boot detection ignores
  the identity record; the authority-pair budget reserves the suffix's seven bytes at load; and a bucket that
  predates identity minting is refused before anything is minted.
- e2e observes the effective pair from the record instead of predicting it from a configuration file, and
  `task e2e:core` gains the `validate-minted-authority` and `validate-pre-identity-bucket-refusal` stages.
- Doc correction: the collision claim in `docs/proposals/gh1095-entity-id-segment-semantics-design.md`.

## Non-goals

- Any rewrite, alias, or migration of stored identity (ADR-102 d7); fresh-state break only.
- Ownership, claims, registries, or cross-deployment coordination (ADR-091).
- Run identity, the import-lane admission fact, the anchor-append bug, the environment surface, and the zero-caller
  deletions — each has its own issue (above).
- A configuration key, environment variable, or any other documentary opt-out (owner ruling, 2026-08-30).
- An HTTP or readiness exposure of the effective pair (design §7 F2 — filed, not built).
- Editing sister repositories; impacts go in `docs/operations/migration-beta162-to-beta163.md`.

## Capabilities touched

`entity-id-contract` (MODIFIED ×1, ADDED ×1), `component-runtime-config` (MODIFIED ×1, ADDED ×1),
`framework-bucket-catalog` (ADDED ×1 — the shared configuration bucket now carries a framework retention guarantee,
so the catalog owns its descriptor).

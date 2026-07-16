# Breaking Change: Canonical Entity IDs

This is the SemStreams-local release note for gh#531. It is a clean pre-v1 break: incompatible beta data is wiped
and rebuilt, not migrated or preserved in place.

## Contract

Every authoritative entity ID, triple subject, and explicit `@id` reference now uses this exact contract:

- six positions: `org.platform.domain.system.type.instance`;
- no more than 256 serialized bytes, including separators; and
- each position matches `[A-Za-z0-9][A-Za-z0-9_-]*`.

SemStreams does not trim, normalize, encode, hash, alias, or repair invalid identity bytes. Declaration patterns are
also exactly six positions and allow only a complete `*` position. Query prefixes contain one through six literal
positions and never contain wildcards. APIs with an empty match-all sentinel handle it explicitly before validation.

## Source and Configuration Changes

- Use `pkg/types.ValidateEntityID`, `ValidateEntityIDPattern`, and `ValidateEntityIDPrefix` instead of local regexes or
  arity checks.
- Mark relationship objects with `message.EntityReferenceDatatype` (`@id`). Untyped strings remain scalar values.
- Replace rule `entity_watch_patterns` with
  `entity_watch_buckets: {"ENTITY_STATES": ["*.*.*.*.*.*"]}`.
- Give every entity-scoped rule an `entity.pattern` using the exact six-position declaration language.
- Update constructors, configs, schema examples, test fixtures, seed data, and exact query inputs together.
- Run `task entity-id:audit`; intentional negative fixtures require exact source classifications.

## Operational Cutover

Stop all writers, delete the incompatible local NATS resources, restart on the breaking binary, and reseed only
canonical source data. The exact Docker and persistent-NATS commands plus required verification gates are in
[Entity-ID Contract Clean Cutover](29-entity-id-contract-clean-cutover.md).

There is deliberately no beta export/preservation contract, compatibility reader, dual reader/writer, online
migration, in-process rewrite, or rollback path for retained incompatible state. Post-v1 retained-state changes are
owned by the operational retention hardening work and require a versioned migration manifest.

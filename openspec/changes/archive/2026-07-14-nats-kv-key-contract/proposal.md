# NATS KV Key Contract

## Why

SemStreams currently delegates key and filter acceptance to unexported behavior in the pinned `nats.go` SDK or to
the NATS server. That is not a sufficient storage contract. In `nats.go` v1.48.0, the legacy and `jetstream` KV
clients accept a restricted ASCII alphabet but their local checks still admit shapes such as interior empty tokens
and embedded wildcard characters. The real server applies stricter subject semantics. Production code also builds
filters and opaque tokens through local helpers, so correctness can depend on which call path happens to reach NATS.

Graph-index reconciliation needs exact-position filters and may compare raw and encoded key axes. Before that work
can make a storage decision, SemStreams needs one exported, versioned contract for literal tokens, literal keys,
wildcard filters, opaque token encoding, size limits, and stable invalid-input errors. Without it, a benchmark can
prove a key shape that another SDK path rejects, silently broaden a filter, or accept a lossy identity rewrite.

## What Changes

- Add exported production helpers for validating one literal KV token, a complete literal KV key, and a complete KV
  wildcard filter.
- Freeze conservative SemStreams budgets of 512 bytes per literal token, 1,024 bytes and 64 tokens per logical key,
  and 1,024 bytes and 64 tokens per filter. These are project limits, not claims about NATS' maximum capacity.
- Add a versioned, deterministic, reversible, collision-free opaque-token codec over arbitrary bytes. Version 1 uses
  `x1_` followed by canonical lowercase hexadecimal, accepts at most 254 input bytes, and always produces one valid
  literal token of at most 511 bytes.
- Export stable code, reason, and detail-key constants for `invalid` failures; apply a deterministic validation
  precedence and mandatory non-sensitive detail schema without echoing raw key material.
- Treat validation and encoding as separate caller choices. Validation never rewrites input, and an invalid literal
  identity is never silently encoded or sanitized.
- Pin compatibility tests to `github.com/nats-io/nats.go v1.48.0`, including both KV APIs, and make a dependency bump
  rerun the acceptance matrix before it can merge.
- Prove accepted boundaries and wildcard match sets through every KV wire path against a normative real NATS server
  pinned to an exact patch and immutable image digest. Tests use default server/client configuration and measure the
  actual Put, Get/direct-get, Create, Update, Delete, list, filtered-list, and watch paths; they do not derive support
  for a lower custom `max_control_line` from the 1,024-byte logical-key budget.
- Inventory every production KV key/filter boundary and publish an owning-migration ledger. Existing paths keep their
  byte and behavioral contracts until an owning semantic-bound, codec, or layout change authorizes validation and any
  required rebuild. Any new or changed key/filter path after this baseline validates with the shared helpers before
  I/O and fails without side effects.
- Do not retire or centralize an existing production helper here. Its ledger owner may do so later only when semantics
  and stored bytes match, or when that owning change authorizes a clean physical cutover.

**Compatibility:** this prerequisite is not breaking. It adds helpers and evidence but does not change existing
`KVStore` or raw-bucket Get/Put/Create/Update/Delete/List/Watch behavior, stored bytes, or rejection semantics.

## Non-goals

- Selecting graph-index layouts, raw-versus-hash predicate representation, index ownership, alias ownership, or
  retention/garbage-collection policy.
- Migrating every existing key format or adding compatibility readers, dual writes, or deprecated shims.
- Opportunistically enforcing the new validators across existing `KVStore`, raw-bucket, list, filter, or watch paths.
- Defining NATS bucket-name policy, custom server control-line support, stream subjects, publish subjects, consumer
  names, or general string sanitation.
- Claiming that every SDK-accepted search expression is a valid SemStreams filter.
- Replacing semantic validation for predicates, entity IDs, aliases, or other domain values.

## Capabilities

### Added Capabilities

- `nats-kv-keys`: canonical KV token/key/filter validation, opaque token encoding, stable errors, budgets, and
  conformance obligations.

## Dependencies

- The implementation is pinned to `github.com/nats-io/nats.go v1.48.0` and the repository's real-NATS test server.
- `graph-index-fixed-arity-reconciliation` depends on this change and MUST consume its validators, codec contract,
  and budgets rather than defining graph-local NATS syntax or size rules.

## Impact

- **Framework code:** `natsclient` gains exported validation/encoding helpers, constants, and classified errors;
  existing wrapper behavior remains unchanged in this prerequisite.
- **Tests:** SDK acceptance matrix, boundary and malformed-shape tables, fuzz/property tests, and real-NATS KV
  conformance.
- **Stored data:** none by this change alone. Existing codecs are not changed unless byte identity is preserved or a
  separate owning change authorizes a clean rebuild.
- **Migration:** a checked-in ledger assigns each existing production boundary to an owning follow-up or records why
  it already matches; this change performs no broad enforcement migration.
- **Consumers:** future index, query, and storage work can reason about exact key arity and size without copying SDK
  regexes or depending on server rejection.

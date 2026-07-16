# NATS KV Key Contract

SemStreams treats NATS KV key syntax as a versioned storage contract. A value that the NATS client or server happens
to accept is not automatically a supported SemStreams key or filter.

## Literal tokens

`natsclient.ValidateKVLiteralToken` accepts one non-empty ASCII token containing only letters, digits, `-`, `/`, `_`,
or `=`. A token cannot contain `.`, `*`, or `>` and cannot exceed 512 bytes. Validation never trims, normalizes,
case-folds, replaces, hashes, or encodes input.

## Literal keys

`natsclient.ValidateKVLiteralKey` accepts one to 64 literal tokens separated by single dots. A complete key cannot
exceed 1,024 bytes. Leading, trailing, or consecutive dots and every wildcard shape are invalid.

## Wildcard filters

`natsclient.ValidateKVWildcardFilter` accepts one to 64 tokens and at most 1,024 bytes. Each position is one of:

- a literal token;
- a complete `*` token, which matches one position; or
- a complete final `>` token, which matches one or more remaining positions.

An exact literal key is also a valid filter. Embedded wildcards such as `foo*bar`, a non-final `>`, and empty tokens
are rejected even when a pinned SDK regex admits the string.

These limits are SemStreams parser and storage-complexity budgets. They are not NATS maxima and do not promise
operation under a custom lower `max_control_line`, custom JetStream API/domain prefix, or custom inbox prefix.

## Opaque tokens

An axis whose owner explicitly declares arbitrary byte identity can use `natsclient.EncodeKVOpaqueToken`. Version 1
encodes zero to 254 bytes as `x1_` followed by lowercase hexadecimal. The result is one literal token of at most 511
bytes. `natsclient.DecodeKVOpaqueToken` accepts only the canonical form.

Encoding is reversible and collision-free inside the input budget. It is never an automatic fallback after literal
validation fails. A domain contract must decide whether an axis is literal, opaque, or a deliberate hash index.

## Stable failures

All helper failures are non-retryable `errs.ErrorInvalid` values. Callers branch on the exported error-code, reason,
and detail-key constants, not message text. Detail never includes the raw token, key, filter, version text, or decoded
bytes. Whole-input size faults precede token-count and left-to-right token faults, making multi-fault results stable.

## Adoption rule

This baseline does not add validation to existing wrappers or raw buckets and does not change existing stored bytes.
Every new key/filter path, and every later change to an existing physical construction, validates the complete key or
filter before I/O under its owning change. The owner must prove domain bounds, classify existing data and callers,
authorize any rebuild, and prove invalid input has no side effects.

The production-boundary inventory and assigned migrations live in
[the KV key migration ledger](../operations/26-nats-kv-key-migration-ledger.md).

## Compatibility evidence

The SDK matrix is pinned to `github.com/nats-io/nats.go v1.48.0`. Normative integration runs against default-config
NATS Server `2.12.4-alpine` at manifest digest
`sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea`.

The common current and legacy SDK matrix covers Put, direct Get, Create, Update, Delete, list, and watch paths. The
current API also covers filtered lists. The unchanged SemStreams wrapper covers Put, Get, Create, Update,
`UpdateWithRetry`'s direct Create, Delete, list, watch, prefix list, and fixed-position filter list; the raw
`FilteredKeys` helper is exercised separately against the wrapper bucket. A dependency-pin change fails the unit guard
until the SDK matrix and normative real-NATS evidence are deliberately updated together.

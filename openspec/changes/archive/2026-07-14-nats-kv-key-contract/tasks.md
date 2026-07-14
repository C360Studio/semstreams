# NATS KV Key Contract Tasks

## 1. Contract and API

- [x] 1.1 Add exported 512-byte token, 1,024-byte key/filter, 64-token, and 254-byte opaque-input constants in
      `natsclient`
- [x] 1.2 Add exported literal-token, literal-key, and wildcard-filter validators with no rewriting or implicit
      encoding
- [x] 1.3 Add the tagged `x1_` lowercase-hex opaque encoder/decoder with empty and maximum-input behavior
- [x] 1.4 Add exported code, reason, and detail-key constants; return `errs.ErrorInvalid` failures with the mandatory
      non-sensitive detail schema and deterministic validation precedence

## 2. Local and Pinned-SDK Proof

- [x] 2.1 Pin a table of accepted and rejected token/key/filter shapes, including empty/interior-empty tokens,
      alphabet boundaries, exact filters, full-token wildcards, embedded/misplaced wildcards, and every size limit
- [x] 2.2 Prove every SemStreams-accepted key/filter is admitted by both KV APIs in `nats.go v1.48.0`; record each
      intentional stricter rejection where the SDK's private regex admits an unsafe shape
- [x] 2.3 Add one-byte and one-token over-limit plus simultaneous-fault precedence tests that pin stable code, reason,
      mandatory detail keys, and side-effect-free failure before NATS I/O
- [x] 2.4 Add fuzz/property tests for validator panic freedom, opaque round-trip, canonical re-encoding, distinct
      outputs, malformed versions/hex, and size boundaries
- [x] 2.5 Add a dependency-bump guard requiring the SDK acceptance matrix and real-NATS suite when `nats.go` changes

## 3. Production Boundary Inventory and Ownership

- [x] 3.1 Inventory every production `KVStore` and raw-bucket Get/Put/Create/Update/UpdateWithRetry/UpdateJSON/Delete,
      Purge, Keys/list, KeysByPrefix, KeysByFilter, FilteredKeys, Watch, and direct key/filter construction boundary
- [x] 3.2 Publish a checked-in ledger recording each boundary's current shape/bytes, semantic owner, shared-contract
      status, missing bound/codec/layout decision, rebuild effect, owning change, and migration state
- [x] 3.3 Leave every existing boundary byte-for-byte and behaviorally unchanged; prohibit opportunistic validation
      inside current wrappers, including UpdateWithRetry direct Create and KeysByPrefix filter construction
- [x] 3.4 Require each post-baseline new or changed key/filter path to validate before I/O under its owning change,
      including semantic-bound proof, data classification, rebuild authorization, and side-effect-free invalid tests

## 4. Real-NATS Conformance

- [x] 4.1 Pin the normative NATS Server 2.12.4 image to an immutable OCI manifest digest in a checked-in test constant
      or lock; record the exact patch, digest, SDK version, and platform with the evidence
- [x] 4.2 Validate test inputs with the new helpers, then on default server/client configuration exercise and measure
      equivalent raw/current Put, Get/direct-get, Create, Update, direct Create, Delete, Keys/list, prefix/filter list,
      raw-bucket filtered list, and Watch paths without asserting current wrappers enforce the helpers
- [x] 4.3 Prove exact, `*`, and final-`>` match sets with malformed shorter/longer, interior-empty, embedded-wildcard,
      misplaced-wildcard, one-byte-over, and one-token-over controls
- [x] 4.4 Record actual subject/control-line sizes for every normative path without claiming support for a custom lower
      `max_control_line`, API/domain prefix, or inbox prefix
- [ ] 4.5 Optionally run a rolling NATS lane for drift detection; keep it non-blocking until an explicit normative pin
      update passes the full matrix

## 5. Production Helper Inventory

- [x] 5.1 Inventory production KV Put/Get/Create/Update/Delete/Purge/List/Watch call sites, direct bucket Create paths,
      key/filter builders, reversible codecs, hashes,
      and lossy sanitation; classify non-KV consumer-name, metrics, URL, and fingerprint helpers out of scope
- [x] 5.2 Cross-check the inventory against the owning-migration ledger; do not modify an existing boundary unless its
      separately approved owner authorizes validation and any required clean rebuild
- [x] 5.3 Leave ad hoc reversible KV codecs unchanged here; record that their owner may centralize/retire them only
      when output bytes and limits match or an owning change authorizes a clean physical rebuild
- [x] 5.4 Record every identity-bearing KV path that uses lossy replacement, dropping, trimming, case folding, or
      normalization in the owning-migration ledger; add no rejection behavior here, and require an explicit owning
      semantic contract before a later change modifies that path

## 6. Documentation and Closeout

- [x] 6.1 Document validation-versus-encoding choice, parser/storage budgets, wildcard grammar, stable errors, pinned
      normative server lane, migration-ledger rule, unchanged existing wrappers, and no lower-control-line claim
- [x] 6.2 Run lint, race, contract, integration, and normative real-NATS conformance gates
- [x] 6.3 Archive this change and seed the `nats-kv-keys` baseline before graph-index fixed-arity filter or raw-key
      decisions proceed

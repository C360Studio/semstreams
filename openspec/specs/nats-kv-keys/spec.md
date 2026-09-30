# nats-kv-keys Specification

## Purpose
TBD - created by archiving change nats-kv-key-contract. Update Purpose after archive.
## Requirements
### Requirement: Literal KV token validation is strict and non-mutating

SemStreams MUST expose a production literal-token validator. A valid token MUST be non-empty, at most 512 bytes,
contain only `-/_=A-Za-z0-9`, and contain neither `.` nor `*` nor `>`. Validation MUST measure bytes, MUST NOT
rewrite input, and MUST fail before NATS I/O with `errs.ErrorInvalid`, code `kv_token_invalid`, and a stable
non-sensitive reason.

#### Scenario: a valid literal token is unchanged

- **GIVEN** a non-empty token within the byte budget and accepted ASCII alphabet
- **WHEN** literal-token validation runs
- **THEN** validation succeeds without trimming, normalization, case folding, or encoding

#### Scenario: a wildcard-looking token is not a literal

- **GIVEN** a token containing `.`, `*`, or `>`
- **WHEN** literal-token validation runs
- **THEN** it fails as `kv_token_invalid` before NATS I/O
- **AND** it does not return a sanitized replacement

### Requirement: Literal KV keys have bounded non-empty token structure

SemStreams MUST expose a production literal-key validator. A valid literal key MUST contain one to 64 valid literal
tokens separated by single dots and MUST be at most 1,024 bytes. Leading, trailing, or consecutive dots and wildcard
tokens MUST be rejected. Failure MUST be `errs.ErrorInvalid` with code `kv_key_invalid` and stable non-sensitive
reason detail.

#### Scenario: an interior empty token is rejected locally

- **GIVEN** the key `foo..bar`, which may pass the pinned SDK's private regex
- **WHEN** literal-key validation runs
- **THEN** it fails with reason `empty_token` before NATS I/O

#### Scenario: a boundary key is accepted

- **GIVEN** a literal key with at most 64 tokens whose byte length is exactly 1,024
- **WHEN** literal-key validation and real-NATS conformance run
- **THEN** the validator accepts it and Put, Get, and Delete pass in the normative default-config NATS lane

#### Scenario: a key exceeds one independent budget

- **GIVEN** a syntactically valid key with 1,025 bytes or 65 tokens
- **WHEN** literal-key validation runs
- **THEN** it fails with code `kv_key_invalid` and the applicable measured and allowed sizes
- **AND** no server-visible write occurs

### Requirement: KV wildcard filters use complete-token wildcard grammar

SemStreams MUST expose a production wildcard-filter validator. A valid filter MUST contain one to 64 non-empty
dot-separated tokens and MUST be at most 1,024 bytes. Each token MUST be a valid literal token, a complete `*` token,
or a complete final `>` token. A literal exact key MUST also be a valid filter. Embedded wildcards, non-final `>`,
empty tokens, and wildcard-containing literals MUST fail before NATS I/O with `errs.ErrorInvalid`, code
`kv_filter_invalid`, and stable non-sensitive reason detail.

#### Scenario: exact and wildcard filters are distinct valid forms

- **GIVEN** the filters `domain.category.property`, `domain.*.property`, and `domain.category.>`
- **WHEN** wildcard-filter validation runs
- **THEN** each succeeds with its exact token positions preserved

#### Scenario: permissive SDK search syntax does not broaden the project contract

- **GIVEN** `foo*bar`, `foo>`, `foo.>.bar`, or `foo..bar`
- **WHEN** wildcard-filter validation runs
- **THEN** it fails with `kv_filter_invalid` before creating a lister or watcher

#### Scenario: real NATS proves the filter match set

- **GIVEN** exact-arity keys plus shorter, longer, and neighboring controls in a real KV bucket
- **WHEN** an accepted exact, `*`, or final-`>` filter is listed or watched
- **THEN** the observed keys equal the declared token-level match set with no false positives

### Requirement: KV budgets are parser and storage-complexity limits

SemStreams MUST treat 512 token bytes, 1,024 logical key/filter bytes, and 64 key/filter tokens as versioned project
limits rather than NATS maxima or a derived control-line envelope. Normative real-NATS conformance MUST exercise and
measure every supported KV wire path on default server/client configuration, including direct Get subjects that may
repeat bucket/key material. The contract MUST NOT claim support for a custom lower `max_control_line`, API/domain
prefix, or inbox prefix. Increasing server capacity MUST NOT implicitly expand the project limits.

#### Scenario: one successful wire path does not prove another

- **GIVEN** a maximum-size key whose Put succeeds but whose direct Get uses a longer API subject
- **WHEN** SemStreams documents or enforces its KV limits
- **THEN** the logical key limit remains 1,024 bytes
- **AND** support is claimed only after both paths and every other normative KV path pass on pinned default NATS

### Requirement: Opaque KV token encoding is versioned, reversible, and collision-free

SemStreams MUST expose production opaque-token encode/decode helpers over arbitrary bytes. Version 1 MUST encode as
`x1_` followed by lowercase hexadecimal, accept zero to 254 input bytes, and produce a single valid literal token of
3 to 511 bytes. Decode MUST accept only the canonical version-1 form, including an even lowercase-hex suffix, and
MUST enforce encoded and decoded limits. Encode failure MUST be `errs.ErrorInvalid` with code
`kv_token_encode_invalid`; decode failure MUST use code `kv_token_decode_invalid`.

For every supported byte string `x`, `Decode(Encode(x))` MUST equal `x`. Distinct supported byte strings MUST have
distinct encoded tokens, and re-encoding a decoded valid token MUST reproduce the identical token.

#### Scenario: empty and maximum opaque inputs round-trip

- **GIVEN** an empty byte string and a 254-byte string
- **WHEN** each is encoded and decoded
- **THEN** each round-trips byte-for-byte
- **AND** each encoded form passes literal-token validation

#### Scenario: a non-canonical token is rejected

- **GIVEN** an unknown version, odd-length hex, uppercase hex, invalid hex, or decoded value over the limit
- **WHEN** opaque-token decoding runs
- **THEN** it fails with `kv_token_decode_invalid` and no lossy recovery

#### Scenario: encoded identity is an explicit storage choice

- **GIVEN** domain input fails literal-token or literal-key validation
- **WHEN** the owning axis has not declared opaque storage
- **THEN** the caller MUST NOT silently encode the input to make it acceptable

### Requirement: Shared helpers and new or changed paths never perform lossy identity sanitation

The shared helpers and every post-baseline new or changed identity-bearing KV path MUST NOT trim, drop, replace,
case-fold, Unicode-normalize, or otherwise map distinct inputs onto one key unless an owning semantic contract
explicitly defines those inputs as equivalent before KV key construction. Literal validation MUST only accept or
reject. Opaque encoding MUST preserve every input byte. Deliberate hash indexes remain separate physical-layout
contracts and MUST NOT be described as reversible encoding.

An existing lossy path MUST remain behaviorally unchanged in this prerequisite, MUST be recorded as
nonconforming/pending in the owning-migration ledger, and MUST require its owning semantic contract before later
modification or conformance.

#### Scenario: two invalid identities cannot collapse through cleanup

- **GIVEN** two distinct inputs that a replacement-based sanitizer would map to the same token
- **WHEN** a shared helper or post-baseline new or changed path prepares them for an identity-bearing KV axis
- **THEN** literal validation rejects each or an explicitly selected reversible codec preserves their distinction

#### Scenario: an existing lossy path is assigned without changing behavior

- **GIVEN** an existing identity-bearing KV path performs lossy sanitation
- **WHEN** this prerequisite inventories that path
- **THEN** the ledger marks it nonconforming/pending and names its owning semantic contract
- **AND** this change does not alter its accepted input, stored bytes, or rejection behavior

### Requirement: Helper failures have stable classified control-flow shape

Token, key, filter, opaque-encode, and opaque-decode failures MUST be non-retryable `errs.ErrorInvalid` errors with
exported code constants `ErrorCodeKVTokenInvalid`, `ErrorCodeKVKeyInvalid`, `ErrorCodeKVFilterInvalid`,
`ErrorCodeKVTokenEncodeInvalid`, and `ErrorCodeKVTokenDecodeInvalid`, whose values are `kv_token_invalid`,
`kv_key_invalid`, `kv_filter_invalid`, `kv_token_encode_invalid`, and `kv_token_decode_invalid` respectively.

Exported reason constants MUST be `KVReasonEmpty=empty`, `KVReasonBytes=bytes`, `KVReasonTokens=tokens`,
`KVReasonEmptyToken=empty_token`, `KVReasonTokenBytes=token_bytes`, `KVReasonSeparator=separator`,
`KVReasonWildcard=wildcard`, `KVReasonPosition=position`, `KVReasonAlphabet=alphabet`, `KVReasonVersion=version`,
`KVReasonHex=hex`, and `KVReasonNoncanonical=noncanonical`.

Exported detail-key constants MUST be `KVDetailReason=reason`, `KVDetailMeasuredBytes=measured_bytes`,
`KVDetailAllowedBytes=allowed_bytes`, `KVDetailMeasuredTokens=measured_tokens`,
`KVDetailAllowedTokens=allowed_tokens`, and `KVDetailTokenIndex=token_index`.

Every error detail MUST contain `reason`. Byte-limit failures MUST also contain `measured_bytes` and `allowed_bytes`;
token-count failures MUST contain `measured_tokens` and `allowed_tokens`; token-local key/filter failures MUST contain
the zero-based `token_index`. Raw token, key, filter, version text, and decoded bytes MUST NOT appear in message or
detail. Callers MUST branch on classification/code and constants rather than message text.

Validation precedence MUST be: empty whole input, whole-input bytes, key/filter token count, then the first
left-to-right token fault ordered as empty token, wildcard/position, token bytes, and alphabet. A literal token MUST
order wildcard, separator, then alphabet after empty/bytes. Decode MUST order empty, bytes, version, malformed hex,
then noncanonical hex. Multiple-fault inputs MUST return the same winning code, reason, and detail keys across runs.

#### Scenario: a validation error crosses a classified boundary

- **GIVEN** an invalid filter is returned through a classified request boundary
- **WHEN** the consumer inspects the error
- **THEN** `errs.IsInvalid` is true and the stable code remains `kv_filter_invalid`
- **AND** retry policy does not retry it

#### Scenario: multiple faults have one deterministic winner

- **GIVEN** an over-budget filter that also contains an empty token and misplaced wildcard
- **WHEN** wildcard-filter validation runs repeatedly
- **THEN** the whole-input byte failure wins by precedence
- **AND** every result carries only the mandatory non-sensitive detail for that winning reason

### Requirement: Existing boundaries remain unchanged until their owning migration

Implementation MUST inventory every production `KVStore` and raw-bucket key/filter boundary and publish a checked-in
owning-migration ledger. The ledger MUST record the boundary, current accepted shape and stored bytes, responsible component,
shared-contract status, missing semantic bound/codec/layout decision, rebuild consequence, owning change, and state.

This prerequisite MUST NOT add validation to existing Get, Put, Create, Update, UpdateWithRetry, UpdateJSON, Delete,
Purge, Keys/list, KeysByPrefix, KeysByFilter, FilteredKeys, Watch, or direct raw-bucket paths. Existing accepted bytes,
rejections, retries, callbacks, filtering, logging, metrics, and stored data MUST remain behaviorally unchanged until
their owning semantic-bound, codec, or layout change authorizes validation and any required clean rebuild. The
UpdateWithRetry direct Create branch and KeysByPrefix complete-filter construction are explicitly included. A shared
helper MUST NOT be opportunistically wired globally in this change.

After this baseline archives, every new key/filter path and every existing path whose key/filter construction changes
MUST validate the complete literal key or wildcard filter before I/O. Its owning change MUST prove semantic bounds,
classify existing data/callers, authorize a rebuild if bytes change, and prove invalid input has no NATS, retry,
callback, lister, watcher, raw-input log, operation-metric, or server-visible side effect.

#### Scenario: the prerequisite does not break a permissive existing path

- **GIVEN** an existing wrapper currently passes a shape that the new shared validator would reject
- **WHEN** this prerequisite lands without that wrapper's owning migration
- **THEN** the wrapper retains its prior bytes, result, retry, and side-effect behavior
- **AND** the ledger records the unresolved owner instead of enabling validation

#### Scenario: a new path validates before I/O

- **GIVEN** a key/filter path is added or its physical construction changes after the baseline
- **WHEN** its owning change prepares a NATS operation
- **THEN** the complete key/filter passes the shared validator before I/O
- **AND** invalid input has the stable classified error and no side effect

### Requirement: Pinned SDK and real-server conformance gate contract changes

For the pinned `github.com/nats-io/nats.go v1.48.0`, tests MUST prove that every SemStreams-accepted literal key and
filter is admitted by both KV API paths. Intentional cases accepted by the SDK but rejected by SemStreams MUST be
recorded. The normative real-NATS lane MUST pin NATS Server 2.12.4 to an immutable OCI manifest digest in checked-in
test configuration and record patch, digest, SDK version, and platform. A floating minor tag MUST NOT select the
normative result. An optional rolling-version lane MAY detect drift but MUST remain non-blocking until an explicit
normative pin update.

On default server/client configuration, normative tests MUST first validate inputs with the new helpers, then exercise
and measure equivalent raw/current Put, Get including direct-get, Create, Update, direct Create, Delete, Keys/list,
prefix/filter list, raw-bucket filtered list, and Watch paths. Tests MUST NOT assert that existing wrappers enforce the
helpers. They MUST prove accepted-shape match sets, boundary sizes, helper rejection, and actual subject/control-line
sizes without claiming support for custom lower control lines or custom prefixes. Fuzz/property tests MUST cover
validator panic freedom, opaque round trips, canonical re-encoding, distinct outputs, malformed forms, and boundary
lengths. A `nats.go` or normative server pin change MUST rerun and update this evidence before merge.

#### Scenario: an SDK bump cannot silently change the storage language

- **GIVEN** the pinned `nats.go` version changes
- **WHEN** dependency validation runs
- **THEN** the SDK acceptance matrix and real-NATS conformance suite are required
- **AND** any acceptance drift is resolved through an explicit contract decision

### Requirement: Existing production helpers remain assigned, not migrated

Implementation MUST inventory production KV key/filter builders, reversible codecs, hashes, and lossy sanitation.
Existing NATS-boundary helpers and stored reversible codecs MUST be assigned in the owning-migration ledger and remain
unchanged here, even when they appear compatible. A later owning change MAY centralize or retire one only when the
shared codec preserves its exact bytes and limits or that change authorizes a clean physical rebuild. Non-KV
consumer-name, metrics, URL, and fingerprint helpers are outside this capability.

#### Scenario: an existing untagged graph codec is not silently rewritten

- **GIVEN** a graph-index key uses an existing untagged hexadecimal predicate token
- **WHEN** this prerequisite is implemented
- **THEN** its stored bytes remain unchanged
- **AND** any adoption of the tagged shared codec waits for an owning graph-index layout decision and clean rebuild

### Requirement: Filtered KVStore listings finalize native watcher delivery

`KVStore.KeysByFilter` and its `KeysByPrefix` alias SHALL preserve the existing operation timeout and native filter
semantics. When construction returns no watcher, operation cancellation or deadline expiry SHALL take precedence
and return nil keys with the typed context error. With a live operation context, a direct
`jetstream.ErrNoKeysFound` constructor result SHALL retain the existing `(nil, nil)` compatibility behavior without
Stop or drain; this is the sole exception to snapshot-completion and Updates-closure requirements.

For a returned watcher, successful results SHALL require initial snapshot completion and native Updates closure
after terminal cleanup. Post-marker entries SHALL be discarded. Other constructor failures, a missing watcher
without the sentinel, premature closure, non-benign Stop errors, or failure to observe closure SHALL NOT produce
successful keys. Operation cancellation SHALL return nil keys with the typed context error. Cleanup failure SHALL
remain distinguishable without replacing the primary failure. Later cancellation during terminal cleanup SHALL NOT
retroactively relabel an already completed snapshot as a collection timeout.

SemStreams SHALL invoke Stop once synchronously for a returned watcher, then consume Updates through closure using
a separate five-second terminal window derived from the operation context without inheriting its cancellation.
This window SHALL bound only the post-Stop wait, not contextless Stop or total public-call duration. No asynchronous
finalizer, retry, hidden fallback or adopter-configured cleanup budget SHALL be introduced.

Updates closure SHALL establish a delivery boundary only, not native goroutine joining or successful consumer
deletion. Only `nats.ErrBadSubscription` and `nats.ErrConnectionClosed` MAY be accepted as already-terminal Stop
conditions, and only after Updates closure is observed. Other Stop errors SHALL remain visible even after closure.
Drain expiry SHALL report incomplete delivery cleanup without falsely classifying a completed snapshot as an
operation-context deadline failure.

This requirement applies to the full-capability KVStore filtered path. The minimal-reader `FilteredKeys` and
`CatalogReader` method sets remain unchanged.

#### Scenario: Cancellation with pending native delivery

- **GIVEN** a filtered KVStore listing has a returned watcher with pending Updates delivery
- **WHEN** the operation context is canceled
- **THEN** the result is nil keys with the typed cancellation error
- **AND** Stop is requested synchronously and Updates is consumed through closure
- **AND** inability to observe closure is retained as an additional cleanup failure.

#### Scenario: Completed snapshot with queued later updates

- **WHEN** the initial snapshot marker is received before operation cancellation
- **THEN** only snapshot keys are eligible for the result
- **AND** later updates are discarded during terminal cleanup
- **AND** successful keys require delivery closure and acceptable Stop completion.

#### Scenario: Terminal drain does not complete

- **WHEN** Updates does not close within five seconds after synchronous Stop returns
- **THEN** the result is nil keys and a delivery-cleanup failure
- **AND** the call makes no native-completion claim
- **AND** no SemStreams-created asynchronous finalizer remains running.

#### Scenario: Stop has not returned

- **WHEN** native contextless Stop remains in progress
- **THEN** the post-Stop drain window does not certify or preempt that call
- **AND** public-call duration includes its actual duration.

#### Scenario: No-match constructor result respects cancellation

- **GIVEN** construction returns no watcher and direct `jetstream.ErrNoKeysFound`
- **WHEN** the operation context is still live
- **THEN** the result is `(nil, nil)` without Stop or drain
- **BUT WHEN** the operation context is canceled or expired
- **THEN** the result is nil keys with the corresponding typed context error, without Stop or drain.

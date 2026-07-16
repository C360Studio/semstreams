# NATS KV Key Contract Design

## Context

NATS KV keys are transported as suffixes of NATS subjects. The pinned `nats.go` v1.48.0 clients keep their KV key
and search-key validators private. Both client APIs use the same narrow ASCII regex, reject empty and leading or
trailing-dot keys, and otherwise leave important subject semantics to the server. In particular, local validation
is not evidence that token boundaries or wildcard placement are safe for a fixed-position query.

The 512/1,024/64 limits below are SemStreams parser and storage-complexity budgets, not a derived NATS control-line
envelope. KV operations do not all use the same wire subject: direct Get can repeat bucket/key material inside a
JetStream API subject and request/reply paths add API, domain, and inbox prefixes. A test with
`max_control_line=1152` proved this distinction: a maximum-key Put succeeded while a new-API Get closed the
connection. This contract therefore proves every supported path on pinned default-config NATS and makes no support
claim for a lower custom `max_control_line`.

Several production helpers hash, encode, or sanitize values for unrelated purposes. Consumer-name sanitation,
Prometheus label sanitation, URL cursors, and content fingerprints are not KV identity codecs. The current
`graph.EncodePredicateToken` is a reversible untagged physical graph codec. It must not be silently changed by this
prerequisite because doing so would change stored graph-index keys.

## Goals / Non-Goals

**Goals:**

- make the accepted literal token, literal key, and wildcard filter languages explicit and exported;
- freeze byte and token budgets that can be tested locally and against real NATS;
- provide a total, canonical opaque byte-string-to-token representation within its input budget;
- return stable machine-classified invalid-input failures before NATS I/O;
- make every intentional difference from the pinned SDK acceptance set visible in tests;
- give dependent changes one contract without moving their storage-layout decisions here.

**Non-Goals:**

- infer domain validity from KV validity;
- auto-encode invalid literals;
- decide graph-index, ALIAS, retention, or ObjectStore layouts;
- provide a general NATS subject API;
- preserve malformed beta key formats through compatibility paths.

## Decisions

### 1. Expose three validators with non-overlapping meanings

The production API exposes helpers equivalent to:

```go
func ValidateKVLiteralToken(token string) error
func ValidateKVLiteralKey(key string) error
func ValidateKVWildcardFilter(filter string) error
```

A literal token is non-empty, contains no dot or wildcard, uses only `-/_=A-Za-z0-9`, and is at most 512 bytes.
A literal key is one to 64 non-empty literal tokens separated by single dots and is at most 1,024 bytes. It contains
no wildcard tokens.

A wildcard filter is one to 64 non-empty dot-separated tokens and is at most 1,024 bytes. Each token is either a
literal token, a complete `*` token, or a complete final `>` token. `>` cannot appear earlier; `*` and `>` cannot be
embedded in literals. An exact literal key is also a valid filter. These rules intentionally reject some strings
accepted by the SDK's private search-key regex, including `foo..bar`, `foo*bar`, and `foo>`. The real server and the
SemStreams fixed-position contract, not the permissive regex, define the usable language.

All lengths are byte lengths. The accepted alphabet is ASCII, so token counts and byte counts are deterministic and
independent of Unicode normalization.

### 2. Freeze conservative project budgets

The v1 constants are:

| Quantity | Limit |
|---|---:|
| Literal token | 512 bytes |
| Literal key | 1,024 bytes |
| Literal key arity | 64 tokens |
| Wildcard filter | 1,024 bytes |
| Wildcard filter arity | 64 tokens |
| Opaque codec input | 254 bytes |
| Opaque v1 encoded token | 511 bytes |

The 1,024-byte logical limit is independent of the server's control-line setting. Normative conformance uses default
server/client configuration and measures each actual wire path, including direct Get subjects, rather than assuming
all operations use `$KV.<bucket>.<key>`. Increasing server capacity does not expand these SemStreams limits, and a
deployment with a lower custom `max_control_line` is not covered by this contract.

The 64-token limit is a SemStreams complexity bound. It is intentionally far above the current six-part entity and
nine-part raw predicate-member candidates while preventing unbounded parser/filter shapes. Changing any limit or
alphabet is a versioned contract change with SDK and real-server conformance evidence.

### 3. Use a tagged canonical opaque codec

The production API exposes helpers equivalent to:

```go
func EncodeKVOpaqueToken(raw []byte) (string, error)
func DecodeKVOpaqueToken(token string) ([]byte, error)
```

Version 1 is `x1_` plus lowercase hexadecimal bytes. Empty input encodes to `x1_`; 254 input bytes encode to 511
bytes. Every output is one valid literal token. The encoder rejects larger input before allocation proportional to
an invalid result. The decoder accepts only the exact `x1_` prefix followed by an even number of lowercase hex
digits, rejects other versions and non-canonical uppercase, and enforces both encoded and decoded limits.

For all supported byte strings, decode(encode(x)) equals x. For all distinct supported inputs, encoded outputs are
distinct. Re-encoding a successfully decoded token yields the identical token. The version tag avoids treating an
untagged hex-looking literal as this codec's output, but callers still declare whether an axis is literal or opaque.

This change does not rewrite the existing graph predicate codec. A dependent graph layout may adopt the shared
codec only through a byte-identical migration or its already-required clean derived-bucket rebuild.

### 4. Validation never changes identity

Validators return the original input's validity and never trim, case-fold, normalize, replace, drop, hash, or encode
characters. Encoding is an explicit physical-layout choice made by the owner of an opaque axis. A caller cannot
fall back from failed literal validation to opaque encoding without that axis's specification authorizing opaque
storage.

The shared helpers and every post-baseline new or changed identity-bearing KV path forbid lossy sanitation because
different inputs can converge to one key. An existing lossy path remains behaviorally unchanged here, is recorded
as nonconforming/pending in the owning-migration ledger, and requires its owning semantic contract before later
modification. Hashes remain valid for axes whose owner deliberately selects a collision-risk model and lookup
scheme; they are not represented as reversible opaque encoding.

### 5. Freeze stable invalid error constants, detail, and precedence

Every helper failure is `errs.ErrorInvalid`, is non-retryable, and uses these exported code constants:

| Exported constant | Value |
|---|---|
| `ErrorCodeKVTokenInvalid` | `kv_token_invalid` |
| `ErrorCodeKVKeyInvalid` | `kv_key_invalid` |
| `ErrorCodeKVFilterInvalid` | `kv_filter_invalid` |
| `ErrorCodeKVTokenEncodeInvalid` | `kv_token_encode_invalid` |
| `ErrorCodeKVTokenDecodeInvalid` | `kv_token_decode_invalid` |

Exported reason constants are:

| Exported constant | Value |
|---|---|
| `KVReasonEmpty` | `empty` |
| `KVReasonBytes` | `bytes` |
| `KVReasonTokens` | `tokens` |
| `KVReasonEmptyToken` | `empty_token` |
| `KVReasonTokenBytes` | `token_bytes` |
| `KVReasonSeparator` | `separator` |
| `KVReasonWildcard` | `wildcard` |
| `KVReasonPosition` | `position` |
| `KVReasonAlphabet` | `alphabet` |
| `KVReasonVersion` | `version` |
| `KVReasonHex` | `hex` |
| `KVReasonNoncanonical` | `noncanonical` |

Exported detail-key constants are:

| Exported constant | Value |
|---|---|
| `KVDetailReason` | `reason` |
| `KVDetailMeasuredBytes` | `measured_bytes` |
| `KVDetailAllowedBytes` | `allowed_bytes` |
| `KVDetailMeasuredTokens` | `measured_tokens` |
| `KVDetailAllowedTokens` | `allowed_tokens` |
| `KVDetailTokenIndex` | `token_index` |

Every error detail MUST contain `reason`. A `bytes` or `token_bytes` failure MUST also contain `measured_bytes` and
`allowed_bytes`; a `tokens` failure MUST contain `measured_tokens` and `allowed_tokens`; a token-local key/filter
failure MUST contain the zero-based `token_index`. No detail or message contains the raw token, key, filter, version
text, or decoded bytes. The code and detail constants are the control-flow contract; message text is not.

Validation precedence is deterministic:

1. reject empty whole input;
2. reject whole-input byte overflow;
3. for keys/filters, reject token-count overflow;
4. scan tokens left-to-right and reject the first empty token, wildcard/position fault, per-token byte overflow, or
   alphabet fault in that order;
5. for one literal token, wildcard and separator faults precede alphabet faults;
6. for decode, after empty/byte checks, reject version, malformed hex, then non-canonical hex in that order.

Encode has only the input-byte bound after nil/empty is accepted. Tests with multiple simultaneous faults pin the
winning code, reason, and detail keys so implementation order cannot change observable errors.

### 6. Establish a baseline without globally changing wrapper behavior

This change inventories `KVStore.Get`, `Put`, `Create`, `Update`, `UpdateWithRetry`, `UpdateJSON`, `Delete`, any Purge
path, `KeysByPrefix`, `KeysByFilter`, raw-bucket `FilteredKeys`, `Watch`, and direct raw-bucket key/filter operations.
It publishes an owning-migration ledger containing the boundary, current accepted shape and stored bytes, semantic
owner, shared-contract status, required bound/codec/layout decision, rebuild consequence, owning change, and state.

The prerequisite MUST NOT add validation to those existing paths merely because their intended semantics appear to
match. That would change beta acceptance/rejection behavior before unbounded domain axes and stored layouts have
owners. Existing paths remain byte-for-byte and behaviorally unchanged until their ledger owner authorizes validation
and any clean rebuild. This includes the direct Create branch inside `UpdateWithRetry` and complete-filter construction
inside `KeysByPrefix`.

After this baseline archives, every new key/filter path and every existing path whose key/filter construction changes
MUST use the shared literal-key or wildcard-filter validator before I/O. Its owning change proves semantic bounds,
classifies any existing data, authorizes a rebuild when bytes change, and tests side-effect-free invalid failure. A
change cannot call a rewrite “validation” to bypass those ownership obligations.

### 7. Make pinned SDK and real NATS conformance complementary

Unit tables run against the public helpers and equivalent operations through both pinned KV APIs. The invariant is
one-way: every value SemStreams accepts must be accepted by the pinned SDK path. Inputs the SDK accepts but this
contract rejects are recorded as intentional strictness, not parity failures.

The normative real-NATS lane pins NATS Server 2.12.4 to an immutable OCI manifest digest in a checked-in test-image
constant/lock; a floating `2.12-alpine` tag is not normative. It uses default server, JetStream domain/API prefix,
client inbox prefix, and payload settings. The evidence records the exact server patch, digest, SDK version, platform,
and actual subject/control-line size observed for each path. An optional rolling-version lane MAY detect upcoming
drift, but its result is non-blocking until a deliberate pin update.

The normative lane first validates test inputs with the new helpers, then exercises equivalent raw/current SDK and
existing-wrapper operations without asserting that existing wrappers enforce validation. It covers maximum-boundary
and representative shapes through Put, Get including direct-get, Create, Update, direct Create, Delete, Keys/list,
prefix/filter listing, raw-bucket filtered listing, and Watch. It also covers exact filters, `*`, final `>`, maximum
byte/token boundaries, and malformed helper inputs. This proves the helper acceptance set works on default
configuration only; it does not claim that a custom lower `max_control_line`, custom API/domain prefix, or custom
inbox prefix is supported.

Fuzz/property tests cover validator panic freedom, encode/decode round trips, canonical re-encoding, distinct-output
sampling, malformed prefixes/hex, and boundary lengths. A `nats.go` version change must update the recorded SDK
acceptance matrix and pass real-NATS conformance before the dependency bump merges.

### 8. Assign migrations instead of performing a global rewrite

The implementation inventory classifies production helpers as:

1. new/changed literal KV token/key/filter builders that must validate through this contract;
2. existing literal paths assigned to an owning migration and left unchanged here;
3. reversible opaque KV codecs that may centralize only if bytes and limits match, or an owning clean cutover exists;
4. deliberate hash-based physical indexes that remain layout-specific;
5. non-KV sanitation/encoding helpers that are out of scope.

The ledger includes `natsclient.KeysByPrefix`, `KeysByFilter`, `FilteredKeys`, direct KV watch/filter entry points, and
all key operations because they cross the NATS boundary. It does not itself authorize modifying any of them. No broad
repository rewrite or opportunistic enforcement is authorized by this prerequisite.

## Risks / Trade-offs

- **Project limits may be lower than some deployments support:** stable conservative bounds are preferable to
  environment-dependent stored identities; a future expansion is an explicit contract revision.
- **The shared opaque prefix changes bytes relative to existing untagged hex:** do not migrate stored keys here.
- **Strict validation may expose latent malformed callers when adopted:** preserve current behavior here and let each
  owning migration classify callers/data before enabling it.
- **Server behavior can drift across upgrades:** keep real-NATS conformance and the SDK acceptance matrix as release
  gates rather than copying private regexes into production.

## Rollout Plan

1. Implement exported constants, classified errors, validators, and opaque codec without changing stored layouts.
2. Add boundary, fuzz/property, pinned-SDK, and real-NATS conformance tests.
3. Inventory every production key/filter boundary and publish the complete owning-migration ledger without changing
   existing wrapper behavior.
4. Record deferred semantic bounds, layout-specific codecs, rebuild needs, and owning changes; add no shims.
5. Archive this non-breaking change to establish the `nats-kv-keys` baseline.
6. Require every later new/changed key/filter path to validate before I/O under its owning change.
7. Allow `graph-index-fixed-arity-reconciliation` to consume the baseline for its new proof/activation paths.

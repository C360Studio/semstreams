## Why

SemStreams has three conflicting definitions of a six-part entity ID. `pkg/types.ParseEntityID` checks only arity
and non-empty segments, `message.IsValidEntityID` checks an ASCII alphabet but permits `-` and `_` in the first
position and has no byte bound, and graph-ingest carries a private regular expression plus a 255-byte limit. The
same identifier can therefore parse, classify as a relationship, and persist differently depending on the call
path.

This is also the missing storage proof identified by `graph-index-fixed-arity-reconciliation`: the worst current
INCOMING key contains two entity IDs, so graph-index cannot activate fixed-position owner reconciliation until the
entity axis has a governed maximum. At a 256-byte entity-ID maximum, the reviewed `2E + 390` INCOMING formula is
902 bytes, safely inside the shared 1,024-byte NATS KV key/filter contract.

Beta is the clean-break window. gh#531 should establish one framework-wide identity language before graph-index
activation or v1, without carrying a permissive mode or malformed persisted identities forward.

## What Changes

- Define a canonical entity ID as exactly six non-empty dot-separated ASCII segments in
  `org.platform.domain.system.type.instance` order.
- Require every segment to begin with an ASCII alphanumeric byte; remaining bytes may be ASCII alphanumeric, `_`,
  or `-`. The exact serialized key is at most 256 bytes, including five dots. There is no independent per-segment
  maximum.
- Make `pkg/types` the only parser and validator authority. Existing `message` entry points delegate to it instead
  of retaining a second grammar.
- Export one stable invalid-entity code, reason constants, and non-sensitive detail-key constants. Failure precedence
  is empty input, byte limit, arity, empty segment, first byte, then segment alphabet.
- Delete graph-ingest's private entity-ID regular expression and 255-byte check; all authoritative ingest paths use
  the shared 256-byte contract.
- Define entity-ID patterns separately: exactly six tokens, each either the complete token `*` or a canonical
  literal entity-ID segment, with a maximum serialized length of 256 bytes. Patterns are not entity IDs and `>` is
  not valid pattern syntax.
- Define entity-ID query prefixes as a third, distinct language. A non-empty prefix contains one through six
  canonical literal segments, is at most 256 bytes, and contains no wildcard or trailing empty segment. Empty is
  accepted only on public surfaces that already promise empty means match-all.
- Validate `ContentStorable.EntityID()` before ObjectStore derives or writes any content or binary object name, so
  an invalid entity cannot leave an orphan before graph-ingest rejects it.
- Audit and update all local and owned-reference producers, schemas, configurations, tests, and fixtures to the same
  literal, pattern, and prefix contract.
- Gate graph-index fixed-arity reconciliation on the implemented bound and complete-key/filter conformance proof.

**BREAKING:** no SemStreams product is in production, and every reference design is owned. The pre-v1 cutover
announces the exact contract break, updates every owned source/configuration/fixture, wipes all incompatible NATS
state, reseeds from canonical owned sources, and reruns affected product e2e. It does not export, preserve, inspect,
rewrite, or roll back old beta state. There is no compatibility reader, alias ledger, normalization shim, dual
reader/writer, or online/in-place migration.

## Non-goals

- Changing the six semantic positions or treating a query prefix as a stored, optional, variable-arity, or
  hierarchical-short identity.
- Lowercasing, trimming, Unicode-normalizing, escaping, hashing, or otherwise rewriting identity bytes.
- Adding a per-segment size limit; the 256-byte serialized-key maximum is the only size bound below the shared NATS
  key contract.
- Treating an entity-ID pattern as a stored identity or accepting NATS `>`/embedded wildcard syntax as an entity
  pattern.
- Selecting graph-index's predicate representation or implementing its owner-reconciliation activation in this
  change.
- Selecting ObjectStore retention, reachability, reference counting, or reclamation policy; only entity-ID
  validation before entity-derived object-name I/O is in scope.
- Preserving, exporting, auditing, or rolling back invalid beta state through runtime aliases, permissive
  configuration, compatibility readers, or an online/in-place migration.

## Capabilities

### New Capabilities

- `entity-id-contract`: canonical literal identity grammar, pattern and prefix grammars, shared parser/validator
  ownership, persistence enforcement, storage-budget proof, and clean beta cutover.

## Dependencies

- The archived `nats-kv-keys` contract supplies the 1,024-byte literal-key/filter and 64-token limits plus shared
  pre-I/O validation. This change selects the narrower semantic limit `E <= 256`; it does not change the NATS
  contract or claim that 256 is a server maximum.
- `graph-index-fixed-arity-reconciliation` depends before framework activation on this change's completed local
  contract/API, local zero-violation corpus, ObjectStore zero-I/O, clean pre-v1 wipe/reseed, key-budget, and breaking
  e2e tasks. It does not wait for this change to archive. With `E = 256`, its maximum current
  INCOMING key is `2E + 390 = 902` bytes, below 1,024. The graph-index change still owns real-NATS filter performance,
  reconciliation correctness, and its activation ADR.
- Predicate-contract enforcement remains independent: predicates and entity IDs have different grammars, semantic
  positions, and source/configuration inventories even though both participate in complete graph-index keys.

## Impact

- **Framework code:** `pkg/types`, `message`, graph-ingest, graph-index, lifecycle, ownership, projection, rules,
  ObjectStore, agentic ID constructors, query/export helpers, schemas, fixtures, and every other literal, pattern,
  or prefix consumer.
- **Stored data:** the pre-v1 cutover wipes all incompatible NATS state and reseeds it from canonical owned sources;
  this change provides no old-state preservation contract.
- **Consumers:** SemSource, SemOps, SemConnect, SemTeams, SemSpec, SemDragon, SemLink, reference deployments, and any
  other product that produces entity IDs or declares entity-ID patterns.
- **Operations:** an exact breaking announcement, owned source/configuration/fixture checklist, NATS wipe/reseed
  runbook, and recorded fresh-state product e2e.
- **Delivery:** closes the entity-axis contract tracked by gh#531 and unblocks the separate graph-index activation
  proof once its named local prerequisites pass. Every owned reference design remains a coordinated v1 release and
  archive gate.

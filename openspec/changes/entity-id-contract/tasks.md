## 1. Contract and Failing Tests

- [ ] 1.1 Inventory every local entity-ID literal, constructor, parser, validator, pattern, schema, config field,
      graph-state seed, KV key/filter builder, and direct split/match implementation; classify each as literal,
      declaration pattern, query prefix, unrelated test glob, or malformed. Explicitly include federation entity and
      RETRACT IDs, OMS Observation, SensorML `NewAsset`, `StoredMessage.Validate`, gated-DAG config/schema, lifecycle
      OpenAPI source/generated OAS, rule expression helpers, graph/fusion/embedding scopes, and gateway prefix inputs
- [x] 1.2 Write failing table tests in `pkg/types` for exact six-part arity, non-empty segments, ASCII alphanumeric
      first bytes, allowed `A-Z a-z 0-9 _ -` remaining bytes, leading `_`/`-`, Unicode, whitespace, slash, `*`, `>`,
      embedded wildcards, and empty segments
- [x] 1.3 Write failing byte-bound tests at 255, 256, and 257 bytes, including a valid 246-byte segment in a
      256-byte ID, proving there is no independent per-segment maximum
- [x] 1.4 Write failing parser/serializer property and fuzz tests for exact round-trip, no mutation, deterministic
      typed failure precedence, panic freedom, and agreement between string validation and `EntityID.IsValid`
- [x] 1.5 Write failing pattern tests for exactly six tokens, literal-or-complete-`*` token syntax, 256-byte total
      bound, literal-pattern equivalence, and rejection of `>`, partial globs, empty tokens, and literal-parser use
- [x] 1.6 Write query-prefix validator tests for one through six canonical literal tokens, the 256-byte total bound,
      wildcard/Unicode/trailing-empty rejection, and the non-empty validator's rejection of empty
- [x] 1.6a Prove explicit empty-match-all behavior on the graph client, graph-embedding, and FusionNATS boundaries;
      prove invalid prefixes fail before request, lister, storage, or embedding work on the implemented graph client,
      graph-ingest, graph-query, graph-embedding, and FusionNATS boundaries
- [ ] 1.6b Add explicit empty-prefix listing proof at graph-ingest and empty-prefix forwarding proof at graph-query
- [ ] 1.6c Add equivalent pre-I/O prefix tests for every remaining gateway, watcher, tool, schema-driven, and reference
      surface discovered by task 1.1

## 2. Canonical `pkg/types` Authority

- [x] 2.1 Add the exported 256-byte limit plus coded `ValidateEntityID(string) error` and
      `ParseEntityID(string) (EntityID, error)` surfaces in `pkg/types`, with no normalization or implicit encoding
- [x] 2.1a Export and pin `ErrorCodeEntityIDInvalid="entity_id_invalid"`; reasons
      `EntityIDReasonEmpty="empty"`, `EntityIDReasonBytes="bytes"`, `EntityIDReasonArity="arity"`,
      `EntityIDReasonEmptySegment="empty_segment"`, `EntityIDReasonFirstByte="first_byte"`, and
      `EntityIDReasonAlphabet="alphabet"`
- [x] 2.1b Export and pin detail keys `EntityIDDetailReason="reason"`,
      `EntityIDDetailMeasuredBytes="measured_bytes"`, `EntityIDDetailAllowedBytes="allowed_bytes"`,
      `EntityIDDetailMeasuredParts="measured_parts"`, `EntityIDDetailAllowedParts="allowed_parts"`, and
      `EntityIDDetailSegmentIndex="segment_index"`; pin precedence as empty, bytes, arity, empty segment, first byte,
      then alphabet, and expose only non-sensitive measured/allowed values and segment index
- [x] 2.2 Make `pkg/types.IsValidEntityID`, `message.IsValidEntityID`, and `EntityID.IsValid` delegate to coded literal
      validation with exact true/false parity and no coded-error claim; preserve the six-field `Key`/`String` bytes
- [x] 2.3 Add coded `ValidateEntityIDPattern(string) error` with
      `ErrorCodeEntityIDPatternInvalid="entity_id_pattern_invalid"`; validate syntax and total bytes before matching,
      and reuse applicable literal reason/detail constants without adding a second reason taxonomy
- [x] 2.4 Replace `message` grammar code with delegators to `pkg/types`; add cross-package conformance tests proving
      parser and validator parity across representative valid, boundary, wildcard, Unicode, empty-segment, and empty
      inputs; task 3.5 still owns the complete repository corpus
- [x] 2.5 Delete graph-ingest's private `entityIDRegex`, `regexp` import, and 255-byte branch; delegate its persistence
      validation to `pkg/types` and prove a 256-byte ID is accepted while 257 bytes is rejected
- [x] 2.6 Add coded `ValidateEntityIDPrefix(string) error` with
      `ErrorCodeEntityIDPrefixInvalid="entity_id_prefix_invalid"`, reuse applicable literal reason/detail constants,
      and keep empty handling outside the non-empty API
- [x] 2.6a Route `graph.query.prefix`, graph-ingest prefix handling, graph-query resolution, graph-embedding `Scope`,
      and FusionNATS prefix/scope inputs through the prefix API while preserving their explicit empty-match-all
      contracts
- [ ] 2.6b Route and test `graph.MatchesAnyIDPrefix`, any fusion engine path beyond FusionNATS, gateways, and every
      additional prefix/scope boundary discovered by task 1.1

## 3. Authoritative Enforcement and Local Source Cutover

- [ ] 3.1 Add failing tests that exercise every ENTITY_STATES create, update, merge, batch, CAS, Graphable,
      foreign-edge, inference, rule, direct-adapter, and repair lane with invalid entity subjects/references; require
      typed rejection before state or projection I/O
- [ ] 3.2 Apply the canonical literal check at the complete-final-candidate persistence seam and independent replay
      decoders; keep optional handler validation delegating and non-authoritative. The existing graph-ingest private
      gate now delegates, but complete candidate/replay coverage remains unproven
- [x] 3.3 Validate lifecycle `Workflow.EntityIDPattern` and `ReferenceSpec.TargetPattern` plus ownership
      `OwnerClaim.Pattern` and `ForeignEdgeClaim.TargetPattern` through the shared pattern API
- [ ] 3.3a Route projection contracts, rule-watch, gateway, schema, tool, and reference-configuration patterns through
      the shared API before registration, validation, or watcher creation
- [ ] 3.4 Update all local ID constructors, pass-through producers, constants, fixtures, configs, schemas, and direct
      split/match helpers. Explicitly cover federation entity/RETRACT IDs, OMS Observation, SensorML `NewAsset`,
      `StoredMessage.Validate`, gated-DAG `FanOutInstanceID` and schema, lifecycle OpenAPI source/generated OAS, and
      rule expression helpers; remove duplicate regexes, alphabets, arity-only checks, magic limits, and validators
- [ ] 3.5 Check in the deterministic local literal/pattern/prefix source corpus report and exact breaking source/config
      change list; reach zero unexplained violations without creating a runtime alias or transformation ledger
- [ ] 3.6 Add invalid-input side-effect tests proving no NATS call, retry, callback, watcher/lister creation, raw-ID
      log field, or operation metric occurs before rejection
- [x] 3.6a Prove the implemented prefix/scope boundaries reject before downstream request, lister, storage, or paid
      embedding work; prove ObjectStore rejects before content extraction or operation metrics and remove its raw-ID
      invalid-input log field
- [ ] 3.6b Extend zero-side-effect proof to every literal, pattern, prefix, replay, gateway, watcher, retry, callback,
      and metric boundary discovered by task 1.1
- [x] 3.7 Validate `ContentStorable.EntityID()` at ObjectStore `StoreContent` before generating or writing any binary
      or content object name; add zero-I/O tests for invalid IDs while leaving retention, reachability, reference
      counting, and reclamation policy out of scope

## 4. Storage Budget and Graph-Index Dependency Proof

- [x] 4.1 Pin formula tests at `E = 256`: PREDICATE 321 bytes, NAME/CONTEXT 710, INCOMING
      `2E + 390 = 902`, OUTGOING 256, and raw PREDICATE candidate 451
- [x] 4.2 Construct maximum literal keys and exact-position owner/forward filters for every bounded entity-bearing
      graph-index layout; pass each through the shared 1,024-byte/64-token NATS key/filter validators before I/O
- [ ] 4.2a Hand ALIAS's true maximum identity bound and raw/opaque-key decision to the owning graph-index change; its
      current representative raw key is an audit fixture, not a governed maximum
- [x] 4.3 Add a 257-byte semantic-axis control proving inactive PREDICATE, NAME, and INCOMING reconciliation helpers
      reject before lister, Put, or Delete I/O, including both INCOMING entity axes
- [ ] 4.3a Add malformed-axis and complete-key/filter controls for production CONTEXT/OUTGOING and every remaining
      graph-index path, covering watcher, Get, and any additional I/O before activation
- [ ] 4.4 Run pinned real-NATS Put/Get/Delete/ListKeysFiltered/Watch conformance for maximum valid shapes and exact
      match sets, including shorter, longer, neighboring-owner, and reversed-axis controls
- [ ] 4.5 Keep framework fixed-arity reconciliation inactive until local tasks 1.1-4.4, 5.1-5.3, 5.6, and 6.1-6.4
      pass plus the dependent graph-index ADR/performance/correctness gates. Record the evidence handoff there; do not
      require this change to archive or owned-reference release tasks 5.4-5.5 to complete for local reconciliation

## 5. Clean Pre-v1 Cutover and Owned Reference Updates

- [ ] 5.1 Publish the exact pre-v1 breaking contract and runbook: required owned source/configuration/fixture updates,
      complete incompatible NATS resource wipe, restart, canonical reseed, and affected product e2e; provide no
      export, persisted-state audit/preservation, in-place migration, or rollback procedure
- [ ] 5.2 Reach zero violations in SemStreams local source, configuration, schemas, tools, fixtures, and reference seed
      data; inject malformed current writes/direct NATS data and prove typed fail-fast rejection without partial state
      or projection output
- [ ] 5.3 Wipe all incompatible local NATS state, restart, reseed from canonical owned sources, and prove fresh-state
      replay watermark recovery plus exact query-result parity with no beta reader, writer, or state dependency
- [ ] 5.4 Run the shared corpus audit in SemSource, SemOps, SemConnect, SemTeams, SemSpec, SemDragon, SemLink, owned
      reference deployments, and every additional discovered owned producer; require all to participate
- [ ] 5.5 Update every owned-reference literal/pattern, configuration, schema, tool, fixture, and seed to a SemStreams
      version containing the new contract; wipe its incompatible NATS state, reseed it, and require product e2e green
- [ ] 5.6 Verify by source and binary/config audit that no permissive flag, alias/rename ledger, legacy validator,
      sanitizer, compatibility reader, dual reader/writer, beta persisted-state migration exporter/inspector, rollback
      path, or in-process persisted-state rewriter remains

## 6. Quality, Documentation, and Release Gates

- [ ] 6.1 Run focused `pkg/types`, `message`, graph-ingest, graph-index, lifecycle, ownership, projection, rule, query,
      agentic, gateway, federation, OMS, SensorML, gated-DAG, ObjectStore, and export unit suites while implementing
      each failing-test slice
- [x] 6.2 Run `task lint`, `go test -race ./...`, and `go test ./test/contract/...` for the first reviewed
      implementation slice
- [ ] 6.2a Run `task schema:generate` and verify no schema/spec drift after schema-facing source updates are complete.
      The first-slice run produced zero schema/spec drift, but this remains open until those updates are complete
- [ ] 6.3 Run the repository's real-NATS integration suite with `-race`, including canonical replay, malformed current
      direct-NATS injection, fresh wipe/reseed, maximum key/filter, and concurrent watcher/list behavior
- [x] 6.4 Run every affected e2e tier before the BREAKING commit lands, at minimum core, structural, agentic, and
      semantic ingest-to-ENTITY_STATES-to-index-to-query paths. Green evidence before commit: `task e2e:core` 2/2;
      `task e2e:structural` 37/37; `task e2e:agentic` scenario success with 3 loops; `task e2e:semantic` 46/46 in
      9m08s with exit 0
- [ ] 6.5 Update `pkg/types` API docs, entity-ID/federation concepts, lifecycle/ownership pattern docs, query-prefix
      and scope docs, schemas/OpenAPI, examples, contributor guidance, and graph-index dependency documentation with
      the literal/pattern/prefix distinction
- [ ] 6.6 Publish BREAKING changelog and release notes for gh#531 with the grammar, 256-byte boundary, source audit,
      owned-reference update checklist, exact NATS wipe/reseed commands, and product e2e evidence; promise no beta
      persisted-state migration export/preservation contract, compatibility reader, online migration, or rollback
- [ ] 6.7 Strict-validate and review this OpenSpec change, then archive it only after implementation, all owned
      reference v1 release gates, real-NATS proof, and relevant e2e evidence are complete; archive is not a
      prerequisite to local framework graph-index reconciliation after task 4.5's named local evidence passes

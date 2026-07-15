## 1. Contract and Failing Tests

- [ ] 1.1 Inventory every local entity-ID literal, constructor, parser, validator, pattern, schema, config field,
      graph-state seed, KV key/filter builder, and direct split/match implementation; classify each as literal,
      declaration pattern, query prefix, unrelated test glob, or malformed. Explicitly include federation entity and
      RETRACT IDs, OMS Observation, SensorML `NewAsset`, `StoredMessage.Validate`, gated-DAG config/schema, lifecycle
      OpenAPI source/generated OAS, rule expression helpers, graph/fusion/embedding scopes, and gateway prefix inputs
- [ ] 1.2 Write failing table tests in `pkg/types` for exact six-part arity, non-empty segments, ASCII alphanumeric
      first bytes, allowed `A-Z a-z 0-9 _ -` remaining bytes, leading `_`/`-`, Unicode, whitespace, slash, `*`, `>`,
      embedded wildcards, and empty segments
- [ ] 1.3 Write failing byte-bound tests at 255, 256, and 257 bytes, including a valid 246-byte segment in a
      256-byte ID, proving there is no independent per-segment maximum
- [ ] 1.4 Write failing parser/serializer property and fuzz tests for exact round-trip, no mutation, deterministic
      typed failure precedence, panic freedom, and agreement between string validation and `EntityID.IsValid`
- [ ] 1.5 Write failing pattern tests for exactly six tokens, literal-or-complete-`*` token syntax, 256-byte total
      bound, literal-pattern equivalence, and rejection of `>`, partial globs, empty tokens, and literal-parser use
- [ ] 1.6 Write failing query-prefix tests for one through six canonical literal tokens, the 256-byte total bound,
      wildcard/Unicode/trailing-empty rejection, and empty accepted only on surfaces whose contract promises
      match-all; require invalid prefixes to fail before filter, query, or watcher I/O

## 2. Canonical `pkg/types` Authority

- [ ] 2.1 Add the exported 256-byte limit plus coded `ValidateEntityID(string) error` and
      `ParseEntityID(string) (EntityID, error)` surfaces in `pkg/types`, with no normalization or implicit encoding
- [ ] 2.1a Export and pin `ErrorCodeEntityIDInvalid="entity_id_invalid"`; reasons
      `EntityIDReasonEmpty="empty"`, `EntityIDReasonBytes="bytes"`, `EntityIDReasonArity="arity"`,
      `EntityIDReasonEmptySegment="empty_segment"`, `EntityIDReasonFirstByte="first_byte"`, and
      `EntityIDReasonAlphabet="alphabet"`
- [ ] 2.1b Export and pin detail keys `EntityIDDetailReason="reason"`,
      `EntityIDDetailMeasuredBytes="measured_bytes"`, `EntityIDDetailAllowedBytes="allowed_bytes"`,
      `EntityIDDetailMeasuredParts="measured_parts"`, `EntityIDDetailAllowedParts="allowed_parts"`, and
      `EntityIDDetailSegmentIndex="segment_index"`; pin precedence as empty, bytes, arity, empty segment, first byte,
      then alphabet, and expose only non-sensitive measured/allowed values and segment index
- [ ] 2.2 Make `pkg/types.IsValidEntityID`, `message.IsValidEntityID`, and `EntityID.IsValid` delegate to coded literal
      validation with exact true/false parity and no coded-error claim; preserve the six-field `Key`/`String` bytes
- [ ] 2.3 Add coded `ValidateEntityIDPattern(string) error` with
      `ErrorCodeEntityIDPatternInvalid="entity_id_pattern_invalid"`; validate syntax and total bytes before matching,
      overlap analysis, registration, lister, or watcher creation and reuse applicable literal reason/detail constants
- [ ] 2.4 Replace `message` grammar code with delegators to `pkg/types`; add cross-package conformance tests proving
      parser and validator results are identical for the full boundary corpus
- [ ] 2.5 Delete graph-ingest's private `entityIDRegex`, `regexp` import, and 255-byte branch; delegate its persistence
      validation to `pkg/types` and prove a 256-byte ID is accepted while 257 bytes is rejected
- [ ] 2.6 Add coded `ValidateEntityIDPrefix(string) error` with
      `ErrorCodeEntityIDPrefixInvalid="entity_id_prefix_invalid"` and route `graph.query.prefix`,
      `graph.MatchesAnyIDPrefix`, graph-embedding/fusion `Scope`, graph-query resolution, and gateway inputs through it;
      reuse applicable literal reason/detail constants and preserve empty-as-match-all only where explicitly promised

## 3. Authoritative Enforcement and Local Migration

- [ ] 3.1 Add failing tests that exercise every ENTITY_STATES create, update, merge, batch, CAS, Graphable,
      foreign-edge, inference, rule, direct-adapter, and repair lane with invalid entity subjects/references; require
      typed rejection before state or projection I/O
- [ ] 3.2 Apply the canonical literal check at the complete-final-candidate persistence seam and independent replay
      decoders; keep optional handler validation delegating and non-authoritative
- [ ] 3.3 Validate lifecycle `Workflow.EntityIDPattern` and `ReferenceSpec.TargetPattern`, ownership
      `OwnerClaim.Pattern`/`ForeignEdgeClaim.TargetPattern`, projection contracts, rule-watch, gateway, schema, tool,
      and reference-configuration patterns at registration/configuration time through the shared pattern API
- [ ] 3.4 Migrate all local ID constructors, pass-through producers, constants, fixtures, configs, schemas, and direct
      split/match helpers. Explicitly cover federation entity/RETRACT IDs, OMS Observation, SensorML `NewAsset`,
      `StoredMessage.Validate`, gated-DAG `FanOutInstanceID` and schema, lifecycle OpenAPI source/generated OAS, and
      rule expression helpers; remove duplicate regexes, alphabets, arity-only checks, magic limits, and validators
- [ ] 3.5 Check in the deterministic local literal/pattern/prefix corpus report and reviewed breaking rename ledger;
      reach zero unexplained violations without loading the ledger at runtime
- [ ] 3.6 Add invalid-input side-effect tests proving no NATS call, retry, callback, watcher/lister creation, raw-ID
      log field, or operation metric occurs before rejection
- [ ] 3.7 Validate `ContentStorable.EntityID()` at ObjectStore `StoreContent` before generating or writing any binary
      or content object name; add zero-I/O tests for invalid IDs while leaving retention, reachability, reference
      counting, and reclamation policy out of scope

## 4. Storage Budget and Graph-Index Dependency Proof

- [ ] 4.1 Pin formula tests at `E = 256`: PREDICATE 321 bytes, NAME/CONTEXT 710, INCOMING
      `2E + 390 = 902`, OUTGOING 256, and raw PREDICATE candidate 451
- [ ] 4.2 Construct maximum literal keys and exact-position owner/forward filters for every graph-index layout; pass
      each through the shared 1,024-byte/64-token NATS key/filter validators before I/O
- [ ] 4.3 Add one-byte-over and malformed-axis controls proving complete keys/filters fail locally with no lister,
      watcher, Put, Get, or Delete side effect
- [ ] 4.4 Run pinned real-NATS Put/Get/Delete/ListKeysFiltered/Watch conformance for maximum valid shapes and exact
      match sets, including shorter, longer, neighboring-owner, and reversed-axis controls
- [ ] 4.5 Keep production fixed-arity reconciliation inactive until local tasks 1.1-4.4, 5.1-5.3, 5.6, and 6.1-6.4
      pass plus the dependent graph-index ADR/performance/correctness gates. Record the evidence handoff there; do not
      require this change to archive or sister tasks 5.4-5.5 to complete for framework reconciliation

## 5. Clean Beta Cutover and Sister Migrations

- [ ] 5.1 Add an offline preflight command/report that distinguishes invalid literal IDs, declaration patterns, and
      query prefixes and prints source/location or persisted-key evidence plus export/reset/reingest guidance
- [ ] 5.2 Seed invalid preexisting ENTITY_STATES in real NATS and prove graph-ingest, graph-index, query, traversal,
      clustering, spatial, temporal, embedding, lifecycle, rule, and other replay consumers remain sticky not-ready
      and emit no partial derived output
- [ ] 5.3 Prove optional export, clean graph/index bucket reset, process restart, canonical-source reingest, replay
      watermark recovery, and exact query-result parity with no legacy identity remaining
- [ ] 5.4 Run the shared corpus audit in SemSource, SemOps, SemConnect, SemTeams, SemSpec, SemDragon, SemLink, owned
      reference deployments, and any additional discovered producer; publish a coordinated participation ledger
- [ ] 5.5 Migrate participating owned sister literals/patterns, configs, schemas, tools, and contract fixtures to a
      SemStreams version containing the new contract; require zero violations before the breaking release
- [ ] 5.6 Verify by source and binary/config audit that no permissive flag, alias/rename table, legacy validator,
      sanitizer, dual reader/writer, or in-process persisted-state rewriter remains

## 6. Quality, Documentation, and Release Gates

- [ ] 6.1 Run focused `pkg/types`, `message`, graph-ingest, graph-index, lifecycle, ownership, projection, rule, query,
      agentic, gateway, federation, OMS, SensorML, gated-DAG, ObjectStore, and export unit suites while implementing
      each failing-test slice
- [ ] 6.2 Run `task lint`, `go test -race ./...`, `task schema:generate`, verify no schema/spec drift, and run
      `go test ./test/contract/...`
- [ ] 6.3 Run the repository's real-NATS integration suite with `-race`, including canonical replay, invalid poison,
      reset/reingest, maximum key/filter, and concurrent watcher/list behavior
- [ ] 6.4 Run every affected e2e tier before the BREAKING commit lands, at minimum core, structural, agentic, and
      semantic ingest-to-ENTITY_STATES-to-index-to-query paths; record exact commands and green evidence
- [ ] 6.5 Update `pkg/types` API docs, entity-ID/federation concepts, lifecycle/ownership pattern docs, query-prefix
      and scope docs, schemas/OpenAPI, examples, contributor guidance, and graph-index dependency documentation with
      the literal/pattern/prefix distinction
- [ ] 6.6 Publish BREAKING changelog and release notes for gh#531 with the grammar, 256-byte boundary, audit command,
      sister compatibility matrix, optional export, mandatory reset/reingest, rollback, and readiness diagnostics
- [ ] 6.7 Strict-validate and review this OpenSpec change, then archive it only after implementation, local and sister
      coordinated v1 release gates, real-NATS proof, and relevant e2e evidence are complete; archive is not a
      prerequisite to local framework graph-index reconciliation after task 4.5's named local evidence passes

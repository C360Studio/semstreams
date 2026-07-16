## Completion contract

This change is complete when the closed seam list below has green contract tests, the bounded hygiene audit
passes, fixtures are canonical, and the final gates rerun green. Candidate counts never gate completion. Adding a
seam or audit category is a deliberate signed-off task with maintainer approval, not a discovery.

Closed seam list (the only acceptance surface):

1. ENTITY_STATES write seam — `graph.MarshalEntityState` (final candidate ID, subjects, `@id` references)
2. All graph-ingest lanes — fact (pre-guard), mutation handlers, direct persistence methods
3. Replay decoders — `graph.UnmarshalEntityState`, graph-index replay, aggregate read seams, direct-NATS poison
4. Prefix/scope APIs — graph client, graph-ingest, graph-query, graph-embedding, FusionNATS
5. Remaining named prefix consumers — `graph.MatchesAnyIDPrefix`, graph-gateway prefix inputs (task 2.6b)
6. Pattern APIs — lifecycle workflow/reference patterns, ownership claim/foreign-edge patterns
7. Rule watch-lane and reference-configuration patterns (task 3.3a)
8. ObjectStore `StoreContent`

## 1. Contract and Failing Tests

- [x] 1.1 Run `task entity-id:audit`, a BOUNDED hygiene lint over statically identifiable entity-ID-shaped
      literals in tracked Go source and structured `testdata`. Every finding is a canonical positive fixture or
      carries one exact intentional classification (negative test, pre-substitution template, declaration pattern,
      query prefix, unrelated glob). The audit claims nothing about implementation-surface coverage and is not
      enforcement evidence — the seam contract tests are. Evidence: audit passes with zero unresolved findings
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
- [x] 1.6b Add explicit empty-prefix listing proof at graph-ingest and empty-prefix forwarding proof at graph-query

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
- [ ] 2.6b Route the remaining NAMED prefix consumers through the shared prefix API with pre-I/O rejection tests:
      `graph.MatchesAnyIDPrefix` and the graph-gateway prefix query inputs. This list is closed

## 3. Authoritative Enforcement and Local Source Cutover

Tasks 3.4a, 3.4b, and 3.6c (graph-event constructors, derived alert/trigger identity, PackID) moved to the
`rule-event-identity` change gated on ADR-076.

- [ ] 3.1 Add failing tests that exercise every ENTITY_STATES create, update, merge, batch, CAS, Graphable,
      foreign-edge, inference, rule, direct-adapter, and repair lane with invalid entity IDs, explicit subjects, and
      references. Prove the Graphable fact lane fills only an empty projected subject from the exact envelope ID before
      the authoritative seam; mutation/direct/replay lanes reject empty or malformed subjects. Cover canonical-shaped
      string references plus explicitly marked `message.EntityReferenceDatatype = "@id"` string, malformed-string, and
      non-string objects; require typed rejection before state or projection I/O
- [x] 3.2 Apply canonical literal and explicit-subject checks at the complete-final-candidate authoritative write seam;
      keep optional handler validation delegating and non-authoritative. Do not normalize mutation/direct candidates or
      any non-empty subject bytes. PR #534 is the merge evidence: its authoritative marshal seam validates the final
      entity ID, explicit subjects, and classified entity references before state or projection I/O
- [ ] 3.2a Apply the same complete-candidate contract independently at every authoritative replay decoder, including
      direct-NATS poison; classify malformed stored state fail-closed before derived projection I/O. Implementation
      exists on the branch (commits 84a442e5, 256c9325); this task closes on review of that implementation against
      the seam list, not on new code
- [x] 3.3 Validate lifecycle `Workflow.EntityIDPattern` and `ReferenceSpec.TargetPattern` plus ownership
      `OwnerClaim.Pattern` and `ForeignEdgeClaim.TargetPattern` through the shared pattern API
- [ ] 3.3a Route the remaining NAMED pattern consumers through the shared pattern API before registration or watcher
      creation: rule watch-lane patterns and reference-configuration patterns. This list is closed. Pattern
      enforcement from f3adabb8 counts here; that commit's generalized watcher/coalescing/evaluation-fence work is
      slice 3 of the landing map and is NOT reviewed under this task
- [x] 3.3b Record projection-contract pattern coverage as satisfied transitively: `projection.Contract.Validate`
      derives ownership claims, whose owner and foreign-edge target patterns use the shared pattern API before bind
- [ ] 3.4 Update all local ID constructors, pass-through producers, constants, fixtures, configs, schemas, and known
      entity-ID parser/builder helpers in the selected framework composition. Add the grammar-only
      `internal/semantictest` entity-ID fixture builder, make it delegate without normalization to `pkg/types`, ban
      imports from production Go files, and migrate positive test fixtures without adding a shared `graph.EntityState`
      factory. Explicitly cover `StoredMessage.Validate`, gated-DAG `FanOutInstanceID` and schema, lifecycle OpenAPI
      source/generated OAS, graph-research sources, and rule expression helpers; remove duplicate regexes, alphabets,
      arity-only checks, magic limits, and validators
- [x] 3.5 Run the bounded hygiene audit and publish the concise breaking source/config change list; zero unresolved
      findings across production, `*_test.go`, and structured `testdata`. Every intentional invalid is bound to one
      exact source occurrence, value, contract kind, and authoritative reason; missing, stale, duplicate, broad, or
      reason-mismatched classifications fail the audit. No generated full-corpus report is checked in; diagnostic JSON
      is produced on demand. The audit is a fixture-hygiene lint over its defined corpus — it does not prove
      implementation-surface coverage, and operations guide 29 states this boundedness explicitly
- [ ] 3.6 Add invalid-input side-effect tests at the closed seam list proving no NATS call, retry, callback,
      watcher/lister creation, raw-ID log field, or success/business/operation metric occurs before rejection;
      separately require exactly one bounded lane/reason rejection metric at the designated boundary with no identity
      bytes in labels
- [x] 3.6a Prove the implemented prefix/scope boundaries reject before downstream request, lister, storage, or paid
      embedding work; prove ObjectStore rejects before content extraction or operation metrics and remove its raw-ID
      invalid-input log field
- [x] 3.7 Validate `ContentStorable.EntityID()` at ObjectStore `StoreContent` before generating or writing any binary
      or content object name; add zero-I/O tests for invalid IDs while leaving retention, reachability, reference
      counting, and reclamation policy out of scope

## 4. Storage Budget and Graph-Index Dependency Proof

- [x] 4.1 Pin formula tests at `E = 256`: PREDICATE 321 bytes, NAME/CONTEXT 710, INCOMING
      `2E + 390 = 902`, OUTGOING 256, and raw PREDICATE candidate 451
- [x] 4.2 Construct maximum literal keys and exact-position owner/forward filters for every bounded entity-bearing
      graph-index layout; pass each through the shared 1,024-byte/64-token NATS key/filter validators before I/O
- [x] 4.2a Hand ALIAS's unbounded representative audit and raw/opaque-key plus owner-discovery decision to the owning
      graph-index change. Its tasks 0.6 and 2.2 and design ownership matrix explicitly keep ALIAS separate and prevent
      its missing governed maximum from blocking unrelated current-layout reconciliation
- [x] 4.3 Add a 257-byte semantic-axis control proving inactive PREDICATE, NAME, and INCOMING reconciliation helpers
      reject before lister, Put, or Delete I/O, including both INCOMING entity axes

The dependent graph-index reconciliation change owns the remaining production CONTEXT/OUTGOING and
complete-key/filter pre-I/O controls, pinned real-NATS maximum/exact-match conformance, and the framework activation
gate. Those obligations are not waived by this entity-axis contract.

## 5. Clean Pre-v1 Cutover and Owned Reference Updates

Owned-product tasks (5.1a, 5.4, 5.5) are coordinated pre-v1 release gates; they do not block the local framework
merge. Owned-repo graph-event migration moved to `rule-event-identity`.

- [ ] 5.1 Publish the exact SemStreams-local pre-v1 breaking contract and developer runbook: local source/configuration/
      fixture updates, complete incompatible local NATS resource wipe, restart, canonical reseed, and framework query/
      e2e proof; provide no export, persisted-state audit/preservation, in-place migration, or rollback procedure
- [ ] 5.1a Before the v1 release and archive, publish the coordinated owned-product cutover checklist and exact
      per-product source/configuration/fixture update, wipe, restart, reseed, and affected product-e2e commands
- [ ] 5.2 Reach zero violations in SemStreams local source, configuration, schemas, tools, fixtures, and reference seed
      data; inject malformed current writes/direct NATS data and prove typed fail-fast rejection without partial state
      or projection output
- [ ] 5.3 Wipe all incompatible local NATS state, restart, reseed from canonical owned sources, and prove fresh-state
      replay watermark recovery plus exact query-result parity with no beta reader, writer, or state dependency
- [ ] 5.4 Run the shared corpus audit in owned repositories and reference deployments as a release gate
- [ ] 5.5 Update every owned-reference literal/pattern, configuration, schema, tool, fixture, and seed to a SemStreams
      version containing the new contract; wipe its incompatible NATS state, reseed it, and require product e2e green
- [ ] 5.6 Verify by source and binary/config audit that no permissive flag, alias/rename ledger, legacy validator,
      sanitizer, compatibility reader, dual reader/writer, beta persisted-state migration exporter/inspector, rollback
      path, or in-process persisted-state rewriter remains

## 6. Quality, Documentation, and Release Gates

Graph-event API documentation and changelog items moved to `rule-event-identity`.

- [ ] 6.1 Run focused `pkg/types`, `message`, graph-ingest, graph-index, lifecycle, ownership, projection, rule, query,
      agentic, graph-research, gateway, gated-DAG, ObjectStore, and export unit suites while implementing each
      failing-test slice
- [x] 6.2 Run `task lint`, `go test -race ./...`, and `go test ./test/contract/...` for the first reviewed
      implementation slice
- [ ] 6.2a Run `task schema:generate` and verify no schema/spec drift after schema-facing source updates are complete
- [ ] 6.2b After the complete local implementation lands, rerun `task lint`, `go test -race ./...`, and
      `go test ./test/contract/...`; first-slice evidence does not substitute for this final merge gate
- [ ] 6.3 Run the repository's entity-contract real-NATS integration scope with `-race`, including canonical replay,
      malformed current direct-NATS injection, and fresh wipe/reseed
- [x] 6.4 Run every affected e2e tier before the BREAKING commit lands, at minimum core, structural, agentic, and
      semantic ingest-to-ENTITY_STATES-to-index-to-query paths. Green evidence before commit: `task e2e:core` 2/2;
      `task e2e:structural` 37/37; `task e2e:agentic` scenario success with 3 loops; `task e2e:semantic` 46/46 in
      9m08s with exit 0
- [ ] 6.4a After all local enforcement, schemas, and fixtures are complete, rerun every affected e2e tier; first-slice
      evidence does not substitute for this final BREAKING gate
- [ ] 6.5 Update SemStreams-local `pkg/types` and `message` API docs, entity-ID concepts, lifecycle/ownership
      pattern docs, query-prefix and scope docs, schemas/OpenAPI, examples, contributor guidance, and graph-index
      dependency documentation with the literal/pattern/prefix and explicit `@id` reference distinctions
- [ ] 6.5a Before v1 release and archive, update every owned product/reference document, generated schema, example, and
      operator cutover guide; this is not a local framework merge or graph-index activation gate
- [x] 6.5x Split operations guide 29 by scope: entity-ID content stays here; event/PackID identity content moves to
      the `rule-event-identity` runbook; predicate-lineage content moves to `predicate-contract-enforcement`; the
      guide's opening states the audit's boundedness explicitly
- [ ] 6.6 Publish the SemStreams-local BREAKING changelog for gh#531 with the grammar, 256-byte boundary, source audit,
      local source/config update checklist, and exact NATS wipe/reseed commands; promise no beta persisted-state
      migration export/preservation contract, compatibility reader, online migration, or rollback
- [ ] 6.6a Before v1 release and archive, publish coordinated product release notes with the owned-reference update
      checklist and recorded product e2e evidence
- [ ] 6.7 Strict-validate and review the completed SemStreams-local OpenSpec implementation and evidence
- [ ] 6.7a Archive this change only after every other task is complete and the coordinated owned-product release gates
      (5.1a, 5.4, 5.5, 6.5a, 6.6a) are satisfied. Archive is not a prerequisite to dependent graph-index
      reconciliation

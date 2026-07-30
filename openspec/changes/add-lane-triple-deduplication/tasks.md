## 1. Shared identity predicate (do this first — both planes depend on it)

- [x] 1.1 Lift the six-field identity into one exported, I/O-free implementation usable by both
      `processor/graph-ingest` and `pkg/projection`. It MUST match `sameAppendTuple`
      (`pkg/projection/mutation_client.go:1324`) exactly: subject, predicate, object, datatype,
      source, context — excluding `Confidence`, `Timestamp`, `ExpiresAt`
      — `message.AppendIdentityKey` / `message.SameAppendTuple` in `message/triple_identity.go`
      (`message` is imported by both consumers and imports neither, so no cycle)
- [x] 1.2 Rewrite `sameAppendTuple` to delegate to it, so the two cannot drift. Do NOT leave a
      second copy that "agrees today"
      — **DEVIATION**: `appendFactsPresent` was its only caller and now consumes the key form
      directly, so a delegating `sameAppendTuple` would have had ZERO callers. Deleted rather
      than left as a phantom (repo discipline: grep for the consumer — delete, don't wire).
      `sameFullTriple`/`objectsEqual` stay: nine-field replace/create verification is a
      different question and still has callers
- [x] 1.3 Canonicalize `Object` the way `objectsEqual` (`:1355`) does — `reflect.DeepEqual` then
      JSON bytes — so `int(85)` and `float64(85)` are one key, not two
      — key form is the JSON encoding, which is how a triple actually round-trips into
      `ENTITY_STATES`; unmarshalable objects fall back to `%#v` behind a non-JSON `!` marker,
      which stays conservative (never collapses two values that could not have been persisted).
      **CORRECTION (review):** an earlier note here called JSON "the coarser of the two
      relations". That was inverted. `objectsEqual` is the UNION `DeepEqual ∪ jsonEqual`, so the
      JSON key is a strict SUBSET — the STRICTER relation. The property that actually holds, and
      the one worth carrying forward: where the two diverge (`0.0` vs `-0.0` — equal under `==`
      hence `DeepEqual`, but `"0"` vs `"-0"` in JSON) the key distinguishes values `objectsEqual`
      calls identical, so the lane can only ever FAIL to suppress, never falsely suppress.
      Divergence needs contrivances; the asymmetry is the safe one either way
- [x] 1.4 Build the key NUL-separated or length-prefixed. A dot/pipe join over arbitrary
      predicate/source/context strings reopens the gh#741 raw-key-collision class — add a test
      with a delimiter character embedded in a context value proving no collision
      — length-prefixed; `TestAppendIdentityKey_NoCollisionAcrossFieldBoundaries` covers dot,
      pipe, embedded NUL, digits adjacent to a length prefix, and empty-vs-shifted
- [x] 1.5 Unit-test the predicate directly: differing only in confidence → same key; differing
      only in timestamp → same key; differing only in expiry → same key; differing in any of the
      six → different key — `message/triple_identity_test.go`

## 2. Suppression helper and the zero-write sentinel

- [x] 2.1 Add a package-level helper taking stored + incoming triples and returning survivors plus
      a suppressed count, seeding a key-based set (O(n+k), NOT pairwise). Package-level, not
      inline — `revive.toml`'s 50-statement cap and the already-long closures (`component.go:2619`)
      — `message.DedupeAppendTriples(stored, incoming) (survivors, suppressed)`; `task lint`
      clean, so neither closure breached the statement cap
- [x] 2.2 Collapse duplicates within one request, preserving first-input order
      — same helper (one set seeded from stored, then extended as survivors are accepted)
- [x] 2.3 Add the skip sentinel following the `errNoOpRemove` precedent (`component.go:3202`), and
      verify `errors.Is` survives the `retry.NonRetryable` + `fmt.Errorf` wrap chain
      (`natsclient/kv.go:305`, `pkg/retry/retry.go:28`)
      — `errNoOpAddDuplicate`, proven by `TestErrNoOpAddDuplicate_SurvivesUpdateWithRetryWrapChain`
      driving the REAL `KVStore.UpdateWithRetry`. That test also pins two adjacent hazards: the
      error stays `retry.IsNonRetryable` (so the CAS loop stops rather than burning retries) and
      does NOT trip `natsclient.IsKVConflictError`, which substring-matches "key exists" /
      "wrong last sequence" on the message text

## 3. Wire both CAS closures

- [x] 3.1 `Component.AddTriple`: suppress inside the closure before the append (`component.go:3012`);
      recover the sentinel at `:3023` **before** `atomic.AddInt64(&c.errors, 1)`
      — body moved to `addTripleLane(ctx, triple, lane)`; `AddTriple` is a thin wrapper so the
      exported signature is unchanged. Suppression runs against the `entity` decoded from the
      bytes read at the revision this iteration will CAS on
- [x] 3.2 `Component.AddTriples`: suppress inside the closure before the append (`:3140`); recover
      inside the `if casErr != nil` block at `:3151` **before** the `failedSubjects`, `c.errors`,
      and `allAbsences` handling. A suppression counted into `allAbsences` (`:3158`) misclassifies
      a mixed batch — test that case explicitly
      — body moved to `addTriplesLane`, which additionally returns the suppressed count;
      `AddTriples` is a thin wrapper so the exported signature is unchanged. The per-group
      suppressed count is ASSIGNED (not accumulated) by the closure so a CAS retry replaces the
      losing attempt's count. `TestAddTriples_SuppressionDoesNotMisclassifyMixedBatch` covers the
      mixed case: subject A wholly duplicate + subject B absent must still wrap `ErrKVKeyNotFound`
- [x] 3.3 Verify no post-commit side effect is skipped: the sentinel path covers the KV write only.
      Suffix-index maintenance, relationship-target creation, and foreign-edge routing live in
      `MergeEntity`/`createEntity`, not the add lane — assert this by reading, and record the
      finding in the task if any add-lane caller does have one
      — **VERIFIED by reading; no add-lane caller has a post-commit side effect.** The only
      post-success work in either add-lane body is `clearEntityPoisonOnCommit` +
      `invalidateEntityCacheEntry`, and both are write-coherence follow-ups that are correctly
      owed only when bytes changed. `updateSuffixIndex` and `ensureRelationshipTargetsExist` are
      called ONLY from `createEntity` (`component.go:2555`, `:2566`) and `MergeEntity` (`:2660`,
      `:2672`); `classifyForeignEdges` only from the merge path (`:1816`). The ADR-072
      applied-sequence redelivery guard lives in `processIngest` (`keyed_ingest.go:143`), on the
      Graphable JetStream lane, and never consults the add lane — so a suppression cannot mask a
      redelivery. The three RPC callers (`decide.go`, `graph_writer.go`, `triplepub.go`) do their
      work before the call and only inspect the response

## 4. Response shape

- [x] 4.1 `WrittenCount` counts only newly appended tuples
      — `writtenCount += len(group) - groupSuppressed`
- [x] 4.2 Add an additive `Deduplicated` count to `AddTriplesBatchResponse` and a `Deduplicated`
      signal to `AddTripleResponse`, so `WrittenCount + Deduplicated == submitted` lets a client
      short-circuit without a read-back
- [x] 4.3 A fully-suppressed request returns success: zero written, empty `FailedSubjects`, nil
      error, and the **live unchanged** `KVRevision` — never zero
      (`openspec/specs/graph-index-readiness/spec.md:47-51` depends on this)
      — `handleTripleAdd` already re-read the live revision after success, so it satisfies this
      unchanged. `handleTripleAddBatch` reported NO revision at all (always 0, pre-existing), so
      **INTERPRETATION**: added `singleSubjectRevision`, which reports the entity's live revision
      when every triple in the batch shares one subject and 0 when the batch spans several. That
      is the shape `pkg/projection`'s `AppendEvidence` uses — it is single-entity by contract and
      copies `response.KVRevision` straight into its receipt — so this is the lane where the
      "never zero" requirement actually bites.
      **REVIEWED AND KEPT.** The alternative — plumbing the revision out of the CAS closure — is
      not implementable: the closure signature is `func(current []byte) ([]byte, error)`, it
      never receives the revision, `UpdateWithRetry` returns only `error`, and on a suppressed
      write there is no committed revision to plumb. The post-hoc `Get` is the established
      pattern in this file. Review M1 then corrected the SHAPE — see 4.3b
- [x] 4.3b (review M1) "Never zero" was false as written, and zero is the UNSAFE value: the
      consumer check is `IndexedRevision >= myRev`, which a zero satisfies vacuously.
      `singleSubjectRevision` returned 0 for two unrelated reasons; they are now distinct.
      A failed post-write read-back returns a **degraded** response carrying the read-back reason
      instead of a bare zero, following `handleEntityUpdateWithTriples`
      (`mutations.go:1062-1066`, the #120 pattern) — applied via the shared
      `revisionAfterMutation` helper to `triple.add`, `triple.add_batch`, AND `triple.remove`,
      which had the same `err == nil` swallow. A multi-subject batch reports NO revision and is
      NOT degraded: it genuinely has no single entity revision, which is undefined, not a
      failure. The `MutationResponse` docstring claiming "triple-mutation handlers don't use
      Degraded" was false and is corrected. Spec delta text updated to match the code exactly
      (no MUST the code knowingly violates); `openspec validate --all --strict` passes 35/35.
      Covered by `TestTripleHandlers_FailedRevisionReadbackDegradesInsteadOfReportingZero`
      (all three handlers, each with a healthy control) and
      `TestHandleTripleAddBatch_MultiSubjectReportsNoRevisionAndIsNotDegraded`
- [x] 4.4 Amend the response contract doc at `graph/mutation_responses.go:199` — "FailedSubjects
      empty + WrittenCount>0 → all entities committed" becomes false under this change
      — rewritten to "FailedSubjects empty → every requested subject was processed without
      failure; WrittenCount MAY be zero", with the read-Deduplicated instruction
- [x] 4.5 Audit the three callers that gate on `FailedSubjects` and use the count only in a message
      (`agentic-tools/decide.go:748`, `agentic-loop/graph_writer.go:156`,
      `research-graph-llmwrap/triplepub.go:131`) and confirm each stays correct
      — **ALL THREE STAY CORRECT.** Each branches solely on `len(resp.FailedSubjects) > 0` and
      interpolates `resp.WrittenCount` only into the error string of that branch, so a smaller
      written count cannot change control flow. A fully-suppressed batch returns empty
      `FailedSubjects` → each returns nil, which is right: the triples ARE present. A suppressed
      subject never enters `FailedSubjects` (pinned by
      `TestAddTriples_SuppressionDoesNotMisclassifyMixedBatch`). None reads `AddTripleResponse`
      fields at all beyond decoding for shape

## 5. Repair the merged client code this breaks

- [x] 5.1 `canonicalizeAppend` (`pkg/projection/mutation_client.go:1157`) collapses duplicate
      evidence, preserving first-input order
      — collapse runs AFTER `canonicalizeTriples` (which stamps the identity-bearing
      Source/Context) and AFTER the per-triple validation loop, so that loop's positional
      diagnostics still name the caller's own indexes
- [x] 5.2 `appendFactsPresent` (`:1272`) switches from multiset consumption to set presence
      — set is keyed by `message.AppendIdentityKey`, i.e. the same key the server suppressed by
- [x] 5.3 Failing-first test: an `AppendEvidence` batch carrying two identical six-field tuples
      must NOT report `CommitNotCommitted`. Prove it fails before the fix — this is the break, and
      a test that passes both ways proves nothing
      — `TestAppendEvidenceWithInternalDuplicatesIsNotReportedNotCommitted` in
      `pkg/projection/mutation_client_dedup_test.go`. **Observed failure before the fix:**
      `AppendEvidence with internally duplicated evidence: projection mutation append-evidence
      failed (internal, not-committed): add-batch response wrote 1 of 2 requested triples` —
      exactly the `MutationInternal` + `CommitNotCommitted` the design predicted. Two sibling
      tests also failed first: `TestCanonicalizeAppendCollapsesDuplicatesPreservingFirstInputOrder`
      ("evidence = 4 triples, want 2 after collapsing") and
      `TestAppendFactsPresentUsesSetPresenceNotMultisetConsumption` ("three identical evidence
      tuples must be satisfied by the one stored copy")
- [x] 5.4 Confirm `verifyAnomalousAppend` (`:1020`) still returns `CommitVerified` on a deduped
      append, and that `mutation_client_test.go:2054` still holds
      — `TestAppendEvidenceAnomalousSuccessRequiresAuthoritativeVerification` passes, including
      its `WrittenCount: 0 → CommitVerified` case. New
      `TestAppendEvidenceFullySuppressedResponseVerifiesThroughReadBack` pins the same degradation
      against a server that suppressed everything AND stored the tuple with differing
      confidence/timestamp — it PASSED before the client fix too, which is the point: the client
      already degraded correctly, at the cost of one extra `ReadAuthoritative`
- [x] 5.5 (review B1, BLOCKING) `classifyAppendResponse` (`:1010`) ignored `Deduplicated` and
      still gated on `WrittenCount != expectedCount`, so a fully-suppressed append raised an
      anomaly. With an `ambiguousCause` already recorded (`:859-866`) that escalates to
      `CommitUnknown` **plus a non-nil error** — a regression THIS change introduced, and a direct
      violation of our own delta ("a late commit followed by an identical retry stores one tuple
      AND the retry reports success"). Now gated on
      `WrittenCount + Deduplicated != expectedCount`; `FailedSubjects` is already known empty at
      that point, so every submitted tuple was either newly written or already present. An old
      server sends `Deduplicated: 0` and this degrades to exactly the previous check.
      Consequences handled in the same edit: `AppendEvidence:874`'s `return receipt, nil` is now
      reachable, which PRESERVES `receipt.KVRevision` (this is what makes `singleSubjectRevision`
      load-bearing rather than dead code), and
      `TestAppendEvidenceFullySuppressedResponseVerifiesThroughReadBack` was split into
      `TestAppendEvidenceFullySuppressedResponse` with two legs — `Deduplicated` present → 1 RPC
      call, `CommitCommitted`, revision 42 preserved; `Deduplicated` absent (old server) → 2
      calls, `CommitVerified`. **Failing-first:**
      `the retry must report success, got: projection mutation append-evidence failed
      (commit-unknown, unknown): request timed out`, plus `commit = "verified", want "committed"`
      and the classifier's `unexpected anomaly ... wrote 0 of 3 requested triples`.
      Two snippet assertions in `mutation_client_test.go` moved to the new message text; probed
      first to confirm both cases still return `validFailure=false` with a non-nil anomaly, i.e.
      behavior unchanged and only the diagnostic reworded

## 6. Observability

- [x] 6.1 Lane-labeled suppressed-duplicate counter. NOT a per-occurrence log — replay traffic
      makes that unbounded
      — `semstreams_graph_ingest_duplicate_triples_suppressed_total{lane}`. No log on the
      suppression path at all
- [x] 6.2 Label cardinality is bounded to a fixed lane enum, not caller-supplied strings
      — `type dedupLane string` with exactly four constants (`add`, `add_batch`, `hierarchy`,
      `foreign_edge`), chosen at each in-repo call site; no producer-supplied value reaches it
- [x] 6.3 Verify the counter is non-zero in the gh#713 regression test — a suppression that is
      invisible is indistinguishable from a lane that never ran
      — the regression asserts the EXACT derived count on the `hierarchy` lane (3 entities × [3
      container-inverse + 2 sibling-inverse] = 15) and
      `TestSuppressedDuplicateCounter_IsLaneAttributed` proves the hierarchy adder does not
      charge the operator-mutation lane. Break-an-input check: changing the expectation to 99
      failed with "Max difference between 99 and 15 allowed is 0.0001, but difference was 84",
      so the assertion is genuinely exercised

## 7. Regression coverage for gh#713

- [x] 7.1 Integration regression in `processor/graph-ingest/hierarchy_integration_test.go` (or
      `hierarchy_sync_integration_test.go`): write multiple same-type entities with hierarchy
      inference enabled; reconstruct/restart the component over the same store; replay the
      unchanged entities through the production startup path; assert **exact triple cardinality**
      and **unchanged revisions** after quiescence. This is what gh#713 asks for
      — new file `processor/graph-ingest/hierarchy_replay_integration_test.go`
      (`hierarchy_sync_integration_test.go` carries a "DO NOT EDIT" generated header). Seeds three
      same-type entities, STOPS that component, builds a fresh one over the same NATS store and
      runs `Initialize`+`Start` (production startup: `initStorage` re-acquires `ENTITY_STATES`
      through the catalog seam), then compares per-key KV revision, entity version, and the sorted
      multiset of triple identity keys. `createTestComponentWithHierarchyConfig` was split so a
      second component can share one test client
- [x] 7.2 Prove it fails without the fix — via `git stash`, not `git checkout`
      — **METHOD DEVIATION, deliberate.** `.agents/contracts/semstreams-developer.md` forbids
      `git stash` in any form (it destroys untracked work — three of this change's files were
      untracked at the time). Used the contract's mandated `cp` backup instead, with
      checksum-verified restore: `component.go` md5 `34d9bf432202988ee726098a31c79469` before
      neutering and identical after restore. Both CAS suppression sites were replaced with the
      original blind appends. **Observed failure:**
      `c360.platform.robotics.mav1.drone.replay001: KV revision advanced on a replay with no
      source change` (expected 0xe, actual 0x1f), `entity version advanced` (0x3 → 0x5), and
      `stored triple cardinality changed across the replay` — the diff shows each entity's two
      `hierarchy.type.sibling` edges duplicated to four, reproducing gh#713's `4 -> 6` class
      exactly
- [x] 7.3 Cover the `createEntity` 409 path specifically: an already-present ID whose
      `GetHierarchyTriples` side effects commit before `Create` returns `ErrKVKeyExists`
      (`component.go:2542` → `:2575`). That is the actual trigger; a test that only re-adds via
      `add_batch` does not reach it
      — replay goes through `CreateEntityStrict`, and the test `require.ErrorIs`es each
      re-registration against `natsclient.ErrKVKeyExists`, so a refactor that stopped reaching the
      409 path would fail the test rather than silently pass it
- [x] 7.4 Count the assertions that actually ran — a green new test may have skipped everything
      — a `compared` counter is asserted equal to 6 (3 entities + 3 containers), so a skipped
      key-comparison fails the test; seed-sanity `require`s pin 6 keys, 3 inverse `contains` edges
      on the type container, and 2 sibling edges per entity BEFORE the replay, so a degenerate
      store cannot make the comparison vacuous. Break-an-input confirmed the metric assertion is
      live (see 6.3), and the neutered-code run (7.2) confirmed the revision/version/cardinality
      assertions are live
- [x] 7.5 (review H1, HIGH) A no-op mutation let the rule engine claim ANOTHER writer's revision.
      `handleTripleAdd` reports the entity's live revision; on a suppression that revision was
      produced by someone else; `triple_mutator.go:83` recorded it unconditionally on
      `KVRevision > 0`; and `processor.go` `shouldSkipRule` consumes it once and returns true — so
      the rule's watcher **drops a genuine external change** for that (rule, entity). Not
      marginal: `actions.go:762-771` builds the triple with a constant `Source: "rule_engine"`,
      empty `Context`, and only `Timestamp`/`ExpiresAt` varying — all excluded from identity — so
      EVERY rule re-assertion of the same (subject, predicate, object) is now a suppression.
      Fixed by gating on `&& !resp.Deduplicated`.
      **`remove_triple` needed the same guard and got it rather than being left as a known
      exception.** `RemoveTripleResponse.Removed` was hard-coded `true`, so the no-op path
      (`errNoOpRemove`, pre-dating this change) was unreportable: `Component.RemoveTriple` now
      delegates to `removeTripleReported`, which returns whether a write COMMITTED, the handler
      reports it honestly, and the mutator gates on `&& resp.Removed`.
      **Failing-first, both legs**, driving the real wire (real `tripleMutator` over real NATS
      into a real graph-ingest, real `Processor` as tracker) in
      `processor/rule/triple_mutator_revision_integration_test.go`:
      `Should be false — a suppressed add committed nothing, so claiming the external writer's
      revision makes the rule drop a genuine external change` and the same for the no-op remove.
      Each test's "fixture sanity" `require` passes first, proving the no-op genuinely reported
      the external writer's revision rather than the test manufacturing the condition.
      Wire-contract change recorded in the spec delta as its own requirement
- [x] 7.6 (review) `TestIntegration_TodoWriteReadRoundTrip` failed 15 vs 13 under the full
      `-race -tags=integration ./...` sweep. **Test-fidelity gap, not a production defect:**
      production `write_todos` never touches the append lane — it goes through
      `projection.ReplaceOwned` (`write_todos.go:190`), the replace lane, which this change does
      not touch. The test planted via `c.AddTriples`, a lane production never uses for todos, and
      because a real batch shares one `now` across items its three `agent.todo.updated-at`
      tuples are byte-identical and collapsed to one — shearing the reader's fixed five-stride
      grouping (`todos.go:130`). Fixed by planting through the CREATE lane (folded into the
      existing `CreateEntityStrict`), which also stores the caller's candidate verbatim, so the
      stored state matches what `ReplaceOwned` produces without dragging owner-token/contract
      scaffolding into a test about compaction survival. NOT fixed by changing 15 to 13 (that
      leaves the reader grouping 13 in runs of 5) and NOT by making the planted timestamps
      distinct (production genuinely shares one `updated_at`, so that would make the fixture less
      faithful). The written-count assertion became a STORED-state assertion read back over the
      same `graph.ingest.query.entity` surface the reader consumes. **Verified the test still
      catches a reader regression:** perturbing `filterTodoTriples` to drop one predicate family
      produced `"[]" should have 3 item(s), but has 0`; `todos.go` restored, md5
      `6e45263ef715bd42f87484769c39bf7b` before and after
- [x] 7.7 (review, owner-approved) **scratchpad — a REAL production add-lane defect.**
      `scratchpad.go` emits four triples per call on the loop entity and is explicitly
      append-only, but only `ScratchID` carried the per-call UUID. `ScratchText`,
      `ScratchCreatedAt` and `ScratchChars` carried no per-entry discriminator, so two calls with
      the same text — or merely the same CHARACTER COUNT, which is far likelier — emitted
      byte-identical tuples and the second entry landed as an id with no chars. Fixed by stamping
      `Context = scratchID` on all four triples: `Context` is documented as the correlation ID
      for "grouping related triples from the same processing batch" (`message/triple.go:76-79`)
      and is one of the six identity fields, so it discriminates without touching any predicate
      or vocabulary surface. `Timestamp` cannot do this job — it is deliberately excluded from
      identity. The comment at `scratchpad.go:188-190` claiming consumers "group triples sharing
      the same scratch.id Object" was FALSE (three of four triples had no scratch id); corrected
      to describe the `Context` grouping the fix actually provides.
      **Failing-first**, asserted through `message.AppendIdentityKey`/`DedupeAppendTriples` (the
      same primitive the server lane suppresses by, not a re-implementation):
      `the add lane would suppress 3 of 8 emitted triples, leaving an incomplete scratchpad entry
      (survivors=5)` for identical text, and `suppress 2 of 8` for different text of identical
      length
- [x] 7.8 (review) **Swept the remaining add-lane emitters for the same pattern** — a group of
      triples representing one logical entry where only some carry a per-entry discriminator.
      **None of the six is affected**, each for a specific reason:
      · `agentic-tools/decide.go:441` — the group's four predicates are all DISTINCT
      (`next_action`, `decision_reason`, optional `sap_coerced`, optional `subtopics`), so no
      intra-call collision. Across calls these are single-valued decision facts about one loop,
      not accumulating entries: a repeat is genuinely the same assertion, and a different
      decision has a different Object and still appends.
      · `research-graph-llmwrap/triplepub.go:213` (`StampOrchestrationTriples`) — unaffected,
      but NOT for the reason first recorded here. The original note argued the group carries a
      `*Complete` triple with a unique RFC3339Nano Object. **That argument is WRONG and must not
      be reused**: each triple dedups INDEPENDENTLY, so a unique member protects nothing —
      which is precisely what the scratchpad defect was (`ScratchID` was unique per call and its
      three siblings still collided). The companions here DO suppress on a repeat:
      `candidate-count` and `evidence-count` (`strconv.Itoa`), `degraded` and
      `assess.sufficient` (`"true"`/`"false"`), `route.action` (one of four constants) — and
      these stages genuinely re-run, since `configs/rules/research-graph/02-*.json` retightens
      (`MaxIterations=2`) and R4 loops back, clearing ONLY the `*.complete` markers.
      What actually makes it safe, and this is the reusable argument: (1) field resolution is
      **FIRST-wins** (`processor/rule/expression/evaluator.go:394-403` returns the first
      predicate match), so round 1's value already answers every rule whether or not round 2
      appends; (2) no research rule counts — the pack's operators are `eq`×16, `ne`×5, `in`×1,
      `gte`×1, with no `length_eq`, `.length`, or `.triples`; (3) the unique `*Complete` stamp
      still advances the revision, so the trigger still fires. Multiplicity is invisible here.
      `BuildKickoffTriples` is not on the add lane at all (`create_with_triples`).
      **Pre-existing defect surfaced, not caused — file separately:** because resolution is
      first-wins and the loop-back clears only `*.complete`, a round-2
      `research.assess.sufficient = true` is ALREADY invisible to R4 on merged main; round 1's
      `false` stays first forever. Single-valued predicate on an append lane — the loop-back
      must clear the companions too, or they move to a replace verb.
      · `executors/websearch.go:259` and `httprequest.go:270` — per-triple `AddTriple` in a loop,
      but every predicate in the group is distinct, and the observation entity is keyed per URL,
      not per observation. Re-observing a URL re-asserts single-valued facts about that URL
      (`web.url`, `web.title`); suppressing those is the DESIRED behavior, and the
      per-observation facts (`WebObservedAt`/`WebFetchedAt`, RFC3339Nano) still append, so no
      observation is lost.
      · `rule/actions.go:1689` — `stampRun` emits two distinct predicates (`LoopRun`,
      `LoopRunEntityID`) as run ANCHORS ("this entity belongs to run X"). Re-stamping is the same
      fact, and `agentrun.Mint` is already idempotent. Its interaction with the revision tracker
      is the H1 fix (7.5).
      · `rule/actions.go:1760` — a single triple whose Object is
      `taskID = fmt.Sprintf("rule-%s-%d", entityID, time.Now().UnixNano())` (`:1492`), unique per
      spawn, so it always appends

- [x] 7.9 (Codex C1, BLOCKING) **A failed or no-op mutation could be reported as
      committed-degraded.** `handleTripleAddBatch` called `singleSubjectRevision` unconditionally,
      so for an absent entity the append wrote nothing AND the revision read returned not-found —
      the response carried BOTH `FailedSubjects` and `Degraded=true`. `AppendEvidence` checks
      `response.Degraded` FIRST (`mutation_client.go:830`), so it entered committed verification
      and could return a committed state for a not-found. `handleTripleRemove` had the identical
      shape. **Root cause was my own M1 directive**, which said "route a failed read-back to
      degraded" and never scoped it to writes that actually committed — so the fix is the
      contract, not the symptom: revision and degraded state are now derived from whether THAT
      SUBJECT committed (`addTriplesResult.CommittedRevisions`), failed subjects are resolved
      before any degraded handling, and a no-op is never degraded. **Failing-first:**
      `Should be false — a subject that FAILED did not commit, so it must not be flagged
      degraded` and `Should be empty, but was post-write read-back failed: kv: key not found`,
      on both the append and remove lanes. End-to-end absent cases added at the handler
      (`TestHandleTripleAddBatch_AbsentEntityIsFailedNotDegraded`,
      `TestHandleTripleRemove_AbsentEntityIsNoOpNotDegraded`) and at the client
      (`TestAppendEvidenceAbsentEntityIsNotCommitted` → `CommitNotCommitted` + `MutationNotFound`)
- [x] 7.10 (Codex C2, BLOCKING) **The post-hoc revision read could make a rule suppress another
      writer's real update.** `revisionAfterMutation` did an independent live `Get` AFTER the CAS,
      so a writer committing in between made the handler return THAT writer's revision — and
      because our own mutation genuinely committed, the new `!Deduplicated` / `Removed` gates
      recorded it as the rule's own, after which `shouldSkipRule` consumed exactly that revision
      and dropped the external change. This is H1 through a different door: H1 was "on a no-op the
      revision belongs to someone else", this is "even on a real commit the re-read can".
      **The comment at `mutations.go:385` was wrong and is deleted, not preserved.** It claimed
      over-reporting is harmless because revisions are monotonic; that holds for the readiness
      consumer (`IndexedRevision >= myRev`) and is false for the rule tracker. Two consumers, two
      safety properties — the analysis had generalized from the tolerant one, and I accepted that
      generalization in review. The replacement text names both properties explicitly.
      **Fix:** built the plumbing we had previously concluded was unavailable — new
      `natsclient.KVStore.UpdateWithRetryRev` returns the exact revision the committed CAS
      produced (`kv.Update` already returned it and it was being discarded). `UpdateWithRetry`
      keeps its signature and delegates, so no other call site moves. Committed → exact CAS
      revision; suppressed/no-op → live read and never degraded, per C1.
      **Failing-first**, after restructuring the assertion (the first form asserted on an
      injected external write, but the fix REMOVES the post-CAS read entirely so the injection
      point no longer exists — the honest assertion is that no post-CAS re-read happens at all).
      With the re-read temporarily reinstated: `Should be zero, but was 1 — the committed path
      must not re-read the revision` and `Should not be: 0x3 — the handler returned the later
      external writer's revision`, on both lanes. `mutations.go` md5
      `daac519ba7f13b0b3984f357f026c3d0` before and after. Coordinated add/remove tests commit an
      external write at the exact seam; `natsclient` integration tests pin
      `UpdateWithRetryRev`'s own contract (own-commit vs later live read, create path, and zero
      on failure)
- [x] 7.11 (Codex C3, BLOCKING) **The documented struct-object hole violated the guarantee this
      change makes.** A struct-valued `Object` keyed differently from its persisted
      `map[string]any` form (declaration order vs sorted-key order), so replaying the same valid
      in-process triple appended it again on every restart, advanced revisions and refired
      watchers — exactly the corruption this change exists to eliminate. The reviewer and I had
      both classified this "record, don't fix" on the grounds that it fails safe; **that was the
      wrong lens** — the key is now the authoritative server-side contract, `Object` is still
      `any`, and a missed suppression IS the failure the requirement forbids.
      **Fixed rather than documented:** `canonicalObjectKey` now normalizes structured values
      through a JSON decode/re-encode so struct and map forms converge, with a scalar fast path
      (string/number/bool/nil) so the hot path pays nothing. Slice order is preserved — order is
      meaning in a list. The "KNOWN HOLE" comment is deleted, not softened. **Failing-first:**
      `a replayed structured object must not advance the revision` and `cardinality must be
      unchanged across the replay`, on BOTH the single and batch lanes. Spec delta amended to
      require canonical-encoding object identity

- [x] 7.12 (reviewer nit, escalated to a FIX) **The scalar fast path contradicted the normalizing
      path.** Measured: `int64(9007199254740993)` (2^53+1) keyed as `9007199254740993` while its
      own persisted form — which JSON-decodes to `float64` — keyed as `9007199254740992`, so a
      producer replaying a large-int scalar never suppressed and re-appended on every restart.
      gh#713's exact failure mode surviving for one value class. The SAME `int64` inside a slice
      took the normalizing round-trip and suppressed correctly, so the two paths disagreed with
      each other. Verified empirically before changing anything.
      **Fixed rather than documented, because this is C3 again**: "no in-repo producer today" is
      "fails safe" in different clothes, and Codex already rejected that reasoning for an
      authoritative server-side suppression contract.
      The fast path is no longer "scalars" — it is now exactly the set for which the round-trip is
      PROVABLY the identity (`string`, `bool`, `nil`, confirmed by measurement), renamed
      `roundTripIsIdentity`. Every number normalizes. Strings remain fast-pathed, which is where
      the saving actually was (entity references and enum-like literals). Comment rewritten to
      describe what the code does and to name numeric width as a first-class divergence beside
      container ordering. **Failing-first:** four subtests of
      `TestAppendIdentityKey_ScalarKeysMatchTheirPersistedForm` (int64/int/uint64/max-int64) plus
      `TestAppendIdentityKey_ScalarAndInContainerAgree`; at the lane,
      `a replayed large-int scalar must not advance the revision` and `cardinality must be
      unchanged across the replay`, with `message/triple_identity.go` md5
      `14a184ef16877170c43c247ef1277780` before and after the temporary reinstatement
- [x] 7.13 (reviewer nits 2-4) `AddTripleResponse`'s docstring still claimed KVRevision "is still
      the entity's live revision, never zero" — doubly false after C2 (the committed path
      deliberately does NOT report the live revision, and zero is reachable on the no-op and
      failed paths). **Third round with this shape**, so this was done as a SWEEP rather than a
      line fix: grepped `never zero|live revision|live, unchanged|read-your-writes|IndexedRevision`
      across all non-test Go and corrected every sibling — `AddTripleResponse`,
      `RemoveTripleResponse`, and the `MutationResponse` suppressed-path bullet.
      The unreachable degraded guard KEEPS its guard (an invariant assertion against a
      non-conforming backend is not a phantom signal) but drops the misleading
      `post-write read-back failed: ` prefix, since that path performs no read-back; the comment
      now states plainly that it is unreachable with real NATS because JetStream KV revisions are
      stream sequences starting at 1, so no one hunts for a live path. The residual M1 shape that
      deliberately survives — a no-op whose live read fails reports KVRevision 0 with
      Degraded false, leaving the caller no read-your-writes anchor — is now documented on both
      response types, on the helper, and in the spec delta

## 8. Spec and docs

- [ ] 8.1 Apply the `graph-ingest` delta; `openspec validate --strict` for this change and the
      full set
- [ ] 8.2 Replace the false clause at `openspec/specs/graph-ingest/spec.md:6-13` — "This matches
      the mutation (`AddTriples`) lane's merge semantics" has never been true on merged main
- [ ] 8.3 Fill the `graph-ingest` spec Purpose — it is still the `TBD - created by archiving
      change graphable-merge-semantics` stub, the same class cleared for `nats-streaming` in #740
- [ ] 8.4 Record the `ExpiresAt` forward hazard (D2) where a future TTL-enforcement author will
      see it, not only in this change directory

## 9. Gates

- [ ] 9.1 `task lint` clean (revive warnings = CI failure); `go vet` plain **and**
      `-tags=integration` **and** `-tags=live_llm`
- [ ] 9.2 `go test -race ./...` — grep `^FAIL` explicitly; the pipeline exit code reports the tail
      stage, not the test run
- [ ] 9.3 Tagged integration on every touched package
- [ ] 9.4 `task schema:generate` then `git diff schemas/ specs/` must be empty (the additive
      response field will move schemas — commit them)
- [ ] 9.5 `go test ./test/contract/...`
- [ ] 9.6 Branch integration sweep: `-race -tags=integration ./...` (framework-package change)
- [ ] 9.7 **BREAKING gate: `task e2e:structural` green at HEAD on the final code**, including any
      review fixes — not on a pre-fix commit
- [ ] 9.8 All gates under `GOFLAGS=-mod=readonly`; the user-global `-mod=mod` contaminates go.mod

## 10. Review and integration

- [ ] 10.1 `semstreams-reviewer` pre-merge review; treat an internal APPROVE as
      necessary-not-sufficient on CAS/concurrency code
- [ ] 10.2 Fable review — this is a durability/ack-class change on the sole `ENTITY_STATES` writer
- [ ] 10.3 Owner-run Codex gate; address findings before merge
- [x] 10.4 Adopter note. **State the RULE, not just the delta** (Fable, 2026-07-30): "scratch
      triples now carry Context" teaches nothing and every sister repo re-derives the trick,
      giving spellings six through nine. The rule is:
      > Under add-lane dedup, an **occurrence-shaped triple group MUST carry an occurrence
      > discriminator, and `Context` is the designated field.** Identity is
      > `(subject, predicate, object, datatype, source, context)`; `Timestamp`, `Confidence`,
      > and `ExpiresAt` are excluded and cannot do this job. The audit test is per-MEMBER, not
      > per-group: **a unique triple does not protect its siblings** — each triple dedups
      > independently, which is exactly what the scratchpad defect was.
      Concrete wire-observable deltas to carry alongside the rule:
      · repeated identical assertions on the add lane now collapse; a producer relying on tuple
      **multiplicity** loses data silently
      · `RemoveTripleResponse.removed` was effectively constant `true`; it now reports `false` on
      an idempotent re-removal — a caller using it as a success signal must switch to the error
      channel
      · `graph.mutation.triple.add` / `.add_batch` / `.remove` can now return `degraded: true`
      with `kv_revision` omitted; callers MUST NOT retry on degraded
      · scratchpad triples now carry `Context = scratchID` where they carried none — matching
      `agent.scratch.chars` by predicate/object is unaffected, filtering on empty `Context` is not
      · **an OLD client against a NEW server** pays one extra `ReadAuthoritative` per deduped
      `AppendEvidence`, and on the ambiguous-retry path gets `CommitUnknown` — the regression B1
      fixed on the new client. This is the strongest argument for sister lockstep on the tag wave
- [x] 10.4b Attach the five-spelling occurrence-identity inventory to gh#683 as motivating
      evidence and name the class — DONE, issue comment 5133062720
- [ ] 10.5 Owner routes the two falsified statements in
      `public-projection-mutation-client`'s spec (`:343-345`, `:355`) to that thread — this change
      does not edit another thread's change directory
- [ ] 10.6 File the hierarchy re-fire follow-up: `createEntity` calls `GetHierarchyTriples`
      unconditionally where `MergeEntity` gates on an absence probe (`component.go:2391`). Dedup
      makes the writes free but leaves the O(N) reads. Fold in `hierarchy.edgesCreated`, whose
      meaning shifts from "edges stored" to "edges asserted" under suppression (observability
      only — `GetMetrics()` has zero non-test callers)
- [x] 10.6b File the research-graph first-wins defect surfaced by the emitter sweep — DONE,
      **gh#746**. Pre-existing on main: a round-2 `research.assess.sufficient = true` is invisible
      to R4 because resolution is first-wins and the loop-back clears only `*.complete`
- [ ] 10.6c Note the new `CONTEXT_INDEX` write on the agentic hot path: `graph-index` skips
      context indexing when `Context == ""` (`processor/graph-index/component.go:1379`), which is
      what scratchpad triples carried before. Four keys per call now, and `UpdateContextIndex`
      re-lists all of an entity's context keys on every re-index. Bounded by the loop's iteration
      budget and the same shape `pkg/projection` already ships — not a defect, but it should not
      be rediscovered from a latency graph
- [ ] 10.7 Verify `gh pr checks` + `mergeStateStatus` explicitly — this repo has no required
      checks; never `--auto`

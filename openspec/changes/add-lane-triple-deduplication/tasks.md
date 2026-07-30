## 1. Shared identity predicate (do this first — both planes depend on it)

- [ ] 1.1 Lift the six-field identity into one exported, I/O-free implementation usable by both
      `processor/graph-ingest` and `pkg/projection`. It MUST match `sameAppendTuple`
      (`pkg/projection/mutation_client.go:1324`) exactly: subject, predicate, object, datatype,
      source, context — excluding `Confidence`, `Timestamp`, `ExpiresAt`
- [ ] 1.2 Rewrite `sameAppendTuple` to delegate to it, so the two cannot drift. Do NOT leave a
      second copy that "agrees today"
- [ ] 1.3 Canonicalize `Object` the way `objectsEqual` (`:1355`) does — `reflect.DeepEqual` then
      JSON bytes — so `int(85)` and `float64(85)` are one key, not two
- [ ] 1.4 Build the key NUL-separated or length-prefixed. A dot/pipe join over arbitrary
      predicate/source/context strings reopens the gh#741 raw-key-collision class — add a test
      with a delimiter character embedded in a context value proving no collision
- [ ] 1.5 Unit-test the predicate directly: differing only in confidence → same key; differing
      only in timestamp → same key; differing only in expiry → same key; differing in any of the
      six → different key

## 2. Suppression helper and the zero-write sentinel

- [ ] 2.1 Add a package-level helper taking stored + incoming triples and returning survivors plus
      a suppressed count, seeding a key-based set (O(n+k), NOT pairwise). Package-level, not
      inline — `revive.toml`'s 50-statement cap and the already-long closures (`component.go:2619`)
- [ ] 2.2 Collapse duplicates within one request, preserving first-input order
- [ ] 2.3 Add the skip sentinel following the `errNoOpRemove` precedent (`component.go:3202`), and
      verify `errors.Is` survives the `retry.NonRetryable` + `fmt.Errorf` wrap chain
      (`natsclient/kv.go:305`, `pkg/retry/retry.go:28`)

## 3. Wire both CAS closures

- [ ] 3.1 `Component.AddTriple`: suppress inside the closure before the append (`component.go:3012`);
      recover the sentinel at `:3023` **before** `atomic.AddInt64(&c.errors, 1)`
- [ ] 3.2 `Component.AddTriples`: suppress inside the closure before the append (`:3140`); recover
      inside the `if casErr != nil` block at `:3151` **before** the `failedSubjects`, `c.errors`,
      and `allAbsences` handling. A suppression counted into `allAbsences` (`:3158`) misclassifies
      a mixed batch — test that case explicitly
- [ ] 3.3 Verify no post-commit side effect is skipped: the sentinel path covers the KV write only.
      Suffix-index maintenance, relationship-target creation, and foreign-edge routing live in
      `MergeEntity`/`createEntity`, not the add lane — assert this by reading, and record the
      finding in the task if any add-lane caller does have one

## 4. Response shape

- [ ] 4.1 `WrittenCount` counts only newly appended tuples
- [ ] 4.2 Add an additive `Deduplicated` count to `AddTriplesBatchResponse` and a `Deduplicated`
      signal to `AddTripleResponse`, so `WrittenCount + Deduplicated == submitted` lets a client
      short-circuit without a read-back
- [ ] 4.3 A fully-suppressed request returns success: zero written, empty `FailedSubjects`, nil
      error, and the **live unchanged** `KVRevision` — never zero
      (`openspec/specs/graph-index-readiness/spec.md:47-51` depends on this)
- [ ] 4.4 Amend the response contract doc at `graph/mutation_responses.go:199` — "FailedSubjects
      empty + WrittenCount>0 → all entities committed" becomes false under this change
- [ ] 4.5 Audit the three callers that gate on `FailedSubjects` and use the count only in a message
      (`agentic-tools/decide.go:748`, `agentic-loop/graph_writer.go:156`,
      `research-graph-llmwrap/triplepub.go:131`) and confirm each stays correct

## 5. Repair the merged client code this breaks

- [ ] 5.1 `canonicalizeAppend` (`pkg/projection/mutation_client.go:1157`) collapses duplicate
      evidence, preserving first-input order
- [ ] 5.2 `appendFactsPresent` (`:1272`) switches from multiset consumption to set presence
- [ ] 5.3 Failing-first test: an `AppendEvidence` batch carrying two identical six-field tuples
      must NOT report `CommitNotCommitted`. Prove it fails before the fix — this is the break, and
      a test that passes both ways proves nothing
- [ ] 5.4 Confirm `verifyAnomalousAppend` (`:1020`) still returns `CommitVerified` on a deduped
      append, and that `mutation_client_test.go:2054` still holds

## 6. Observability

- [ ] 6.1 Lane-labeled suppressed-duplicate counter. NOT a per-occurrence log — replay traffic
      makes that unbounded
- [ ] 6.2 Label cardinality is bounded to a fixed lane enum, not caller-supplied strings
- [ ] 6.3 Verify the counter is non-zero in the gh#713 regression test — a suppression that is
      invisible is indistinguishable from a lane that never ran

## 7. Regression coverage for gh#713

- [ ] 7.1 Integration regression in `processor/graph-ingest/hierarchy_integration_test.go` (or
      `hierarchy_sync_integration_test.go`): write multiple same-type entities with hierarchy
      inference enabled; reconstruct/restart the component over the same store; replay the
      unchanged entities through the production startup path; assert **exact triple cardinality**
      and **unchanged revisions** after quiescence. This is what gh#713 asks for
- [ ] 7.2 Prove it fails without the fix — via `git stash`, not `git checkout`
- [ ] 7.3 Cover the `createEntity` 409 path specifically: an already-present ID whose
      `GetHierarchyTriples` side effects commit before `Create` returns `ErrKVKeyExists`
      (`component.go:2542` → `:2575`). That is the actual trigger; a test that only re-adds via
      `add_batch` does not reach it
- [ ] 7.4 Count the assertions that actually ran — a green new test may have skipped everything

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
- [ ] 10.4 Adopter note: repeated identical assertions now collapse. Any sister repo relying on
      tuple **multiplicity** loses data silently — this is not greppable from here and needs an
      explicit ask before merge
- [ ] 10.5 Owner routes the two falsified statements in
      `public-projection-mutation-client`'s spec (`:343-345`, `:355`) to that thread — this change
      does not edit another thread's change directory
- [ ] 10.6 File the hierarchy re-fire follow-up: `createEntity` calls `GetHierarchyTriples`
      unconditionally where `MergeEntity` gates on an absence probe (`component.go:2391`). Dedup
      makes the writes free but leaves the O(N) reads
- [ ] 10.7 Verify `gh pr checks` + `mergeStateStatus` explicitly — this repo has no required
      checks; never `--auto`

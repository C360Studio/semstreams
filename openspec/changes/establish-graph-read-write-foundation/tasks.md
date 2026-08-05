# Tasks — graph read/write foundation

> The design, review, and owner gates are complete. Runtime work proceeds in one draft implementation PR. Task sections
> are reviewable TDD slices, but no partially migrated breaking contract may merge to `main`.

## 0. Durable target state

- [x] 0.1 Preserve the accepted repository-first inventory and independent `INVENTORY PASS` by exact content identity.
- [x] 0.2 Preserve the reviewed revision-39 design and independent `DESIGN REVIEW PASS` by content hash.
- [x] 0.3 Record owner acceptance of all sixteen rulings and the two-PR delivery plan.
- [x] 0.4 Supersede and archive the recovery-era investigation so it cannot act as a competing implementation baton.
- [x] 0.5 Add capability deltas and ADR-091 without changing runtime code.
- [x] 0.6 Obtain an independent foundation-record review and close every semantic promotion gap before publication.

## 1. Contract-first tests

- [ ] 1.1 Add failing contract tests for the four operation request/response shapes and retired subjects/fields.
- [ ] 1.2 Add failing tests for the typed `nats-request` interface, canonical `graph.mutation.>` family, subject
      resolution, and exactly-one-provider static validation.
- [ ] 1.3 Add failing exact-read tests proving entity and nonzero KV revision come from one entry.
- [ ] 1.4 Add failing cross-lane race, hierarchy disposition, relationship absence, and lost-reply tests.

## 2. Component-port mutation contract

- [ ] 2.1 Preserve flat and typed `NATSRequestPort.Interface` metadata through construction and JSON round trips.
- [ ] 2.2 Declare graph-ingest's required mutation provider input and every in-repo requester's matching output.
- [ ] 2.3 Resolve the four exact subjects from the declared family and delete hidden subject fallbacks/tables.
- [ ] 2.4 Validate one compatible provider per flow, many requesters, and no process-wide leader/election claim.

## 3. Exact authority read

- [ ] 3.1 Add the exact entity result carrying a validated entity and same-entry KV revision.
- [ ] 3.2 Serve the admitted GraphQL entity operation as `{entity, kvRevision}` with typed not-found/poison outcomes.
- [ ] 3.3 Add one operation-specific embedded adapter and migrate projection/lifecycle callers; add no general client or
      raw-KV fallback.

## 4. Four-operation mutation kernel

- [ ] 4.1 Implement strict atomic create and delete the non-strict upsert birth path.
- [ ] 4.2 Implement required-revision reconcile with complete selected-predicate-set semantics.
- [ ] 4.3 Implement exact-tuple append/dedup with explicit per-subject partial results.
- [ ] 4.4 Implement required-revision conditional delete without claiming a delete-marker revision.
- [ ] 4.5 Return classified server outcomes and typed client transport outcomes including `commit_unknown`.
- [ ] 4.6 Add the bounded mutation-outcome metric and structured revision-mismatch log.
- [ ] 4.7 Prove every existing-key `ENTITY_STATES` write uses CAS and an ingest/RPC race cannot erase an acknowledged
      write; the keyed pool remains a local throughput optimization only.

## 5. Local projection and caller migration

- [ ] 5.1 Retain projection contracts as local birth/reconcile/append schemas; delete semantic owner derivation,
      registry, heartbeat, token, presence, foreign-edge, and overlap behavior.
- [ ] 5.2 Replace rule `replace_owned` with contract-bound `reconcile`, preserving exact static targets and receipts.
- [ ] 5.3 Give rule reconcile one fresh exact read and one retry after definite revision mismatch; never retry
      `commit_unknown` automatically.
- [ ] 5.4 Migrate lifecycle, gated-DAG, agentic tools, lesson/todo writers, research/inference writers, GraphQL, both
      binaries, configs, examples, and E2E harnesses.

## 6. Hierarchy and unresolved references

- [ ] 6.1 Invoke hierarchy only from Graphable ingest; RPC create has no hierarchy side effects.
- [ ] 6.2 Birth real inferred hierarchy containers with atomic Create and update must-exist inverse targets with CAS.
- [ ] 6.3 Delete relationship-target stubs, stub restamp/filtering, claim-driven foreign-edge behavior, inverse gates,
      and the unused pending-edge spelling.
- [ ] 6.4 Preserve source relationships to absent objects and expose typed missing results during exact dereference,
      hydration, and traversal.

## 7. Delete semantic ownership

- [ ] 7.1 Delete all `pkg/ownership` production/test files and `OwnershipService` production/test files.
- [ ] 7.2 Remove ownership buckets/catalog entries, lease config, six explicit settings, schemas, metrics, request
      fields,
      boot wiring, shutdown wiring, and documentation.
- [ ] 7.3 Preserve the graph-state guard and catalog cleanliness check under neutral composition names.
- [ ] 7.4 Update current project context and affected ADR status notes at the coordinated runtime cutover.

## 8. Verification and coordinated cutover

- [ ] 8.1 Run touched-package tests and focused tagged integration tests for each slice; record exact evidence.
- [ ] 8.2 After the mutation kernel and at final cutover, run the full race/integration gates; do not run them after
      every
      commit.
- [ ] 8.3 Regenerate schemas and verify no generated drift; run strict OpenSpec and contract validation.
- [ ] 8.4 Run final `e2e:core`, `e2e:structural`, `e2e:semantic`, `e2e:lifecycle`, and `e2e:agentic` tiers with active
      polling and fast abort when wedged. Statistical E2E is not a blocker unless this change directly alters it.
- [ ] 8.5 Produce a per-ruling conformance table with `file:line` evidence and no unapproved deviation.
- [ ] 8.6 Verify production code is net-negative and no new bucket, stream, service, status key, coordination primitive,
      compatibility path, or MCP surface was added.
- [ ] 8.7 Run the communicate-only wire census across the ten named sister repositories and publish the migration note;
      edit no downstream code and treat no finding as design authority.
- [ ] 8.8 Obtain final SemStreams reviewer approval, mark the draft PR ready, and merge the single coordinated breaking
      cutover only when all required gates are green.

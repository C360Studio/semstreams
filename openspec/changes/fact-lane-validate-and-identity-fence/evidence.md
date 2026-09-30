# Evidence: fact-lane-validate-and-identity-fence

Runner: `go version go1.26.4 darwin/arm64`, default GOMAXPROCS, `-count=1`. Worktree
`semstreams-wt/claude/gh1112-fact-lane-identity`, branch `claude/gh1112-fact-lane-identity`.

## Fails-without-fix (tasks 2.1 and 2.2)

Procedure: `docs/contributing/01-testing.md` § Establish Sensitivity to a Selected Mutation, `cp` backup and checksum
restoration; no git restoration or stash. Experiment run at `fbd82ac7` (both fix commits in, worktree clean before and
after: `git status --porcelain` empty). The mutant is the whole fix reverted: the pre-fix bytes of both production
files, copied from the base `1b1accf4` before editing. Test files unchanged across all three runs.

| File | Pre-fix md5 (`1b1accf4`) | Fixed md5 (`fbd82ac7`) |
|------|--------------------------|------------------------|
| `processor/graph-ingest/component.go` | `1bdf1efb1d8c8cb2df6b8f05890e96b5` | `f2d04a95c2a86535f3c621be11fe28f0` |
| `agentic/loop_execution_entity.go` | `998e7303f3a7daec33dd731f8a9ba9d3` | `d33465d7b7dbcc2af797f4cd9f960aa5` |

```bash
SP=<session scratchpad>
GI='TestDecodeEntity_ValidateFailureRejectsBeforeIdentity|TestDecodeEntity_FencesIdentityPanics|TestHandleMessage_RejectionsLandOnPoisonAccounting'
AG='TestLoopExecutionEntity_MalformedIdentityReturnsSentinel|TestLoopExecutionEntity_MalformedReferenceOmitsTriple|TestLoopExecutionEntity_ValidateRefusesOnlyMalformedReference'
cp processor/graph-ingest/component.go $SP/component.go.fixed
cp agentic/loop_execution_entity.go $SP/loop_execution_entity.go.fixed
go test -count=1 -v -run "$GI" ./processor/graph-ingest/   # GREEN
go test -count=1 -v -run "$AG" ./agentic/                  # GREEN
cp $SP/component.go.orig processor/graph-ingest/component.go
cp $SP/loop_execution_entity.go.orig agentic/loop_execution_entity.go
md5 -q processor/graph-ingest/component.go agentic/loop_execution_entity.go   # = pre-fix sums
go test -count=1 -v -run "$GI" ./processor/graph-ingest/   # RED
go test -count=1 -v -run "$AG" ./agentic/                  # RED
cp $SP/component.go.fixed processor/graph-ingest/component.go
cp $SP/loop_execution_entity.go.fixed agentic/loop_execution_entity.go
md5 -q processor/graph-ingest/component.go agentic/loop_execution_entity.go   # = fixed sums
go test -count=1 -v -run "$GI" ./processor/graph-ingest/   # GREEN
go test -count=1 -v -run "$AG" ./agentic/                  # GREEN
```

GREEN (fixed, and again after restore): every selected test and subtest PASS; `ok` for both packages.

RED (pre-fix bytes), the intended assertions:

- 2.1 (a) `TestDecodeEntity_ValidateFailureRejectsBeforeIdentity`: `An error is expected but got nil.` (the invalid
  payload decoded and extracted; `EntityID` ran).
- 2.1 (b)/(c) `TestDecodeEntity_FencesIdentityPanics/{panic-entity-id,panic-triples}`: `should not panic`,
  `Panic value: fence test payload: EntityID boom` / `... Triples boom`.
- 2.1 poison accounting `TestHandleMessage_RejectionsLandOnPoisonAccounting`: `invalid` fails `poison count for mode
  invalid` and `persisted entities for mode invalid` (it was persisted); `panic-entity-id` and `panic-triples` fail
  `should not panic`; the `conforming` control row PASSES on both sides.
- 2.2 `TestLoopExecutionEntity_MalformedIdentityReturnsSentinel/*` (6 rows): `EntityID panicked on decoded input:
  LoopExecutionEntityID: ... must not be empty` / `... must not contain dots`.
- 2.2 `TestLoopExecutionEntity_MalformedReferenceOmitsTriple/{dotted_parent,dotted_reply-to}`: `Triples panicked on
  decoded input`.
- 2.2 `TestLoopExecutionEntity_ValidateRefusesOnlyMalformedReference/{parent_only,reply-to_only}`: `Validate panicked
  on decoded input`.

Scope of 2.2's `Validate()` cases after the rulings: see the Revision 1 and Revision 2 addenda below.

## Gates (task 2.4), at `fa20dc7c`

| Command | Exit |
|---------|------|
| `task check` | 0 |
| `go test -race -count=1 ./processor/graph-ingest/... ./agentic/...` | 0 |
| `task schema:generate` | 0 |
| `git diff --stat schemas/ specs/` | empty (0 lines) |
| `openspec validate fact-lane-validate-and-identity-fence --strict` | 0 (`Change ... is valid`) |
| `go run ./cmd/entity-id-audit .` | 0 (`entity ID audit passed: 1336 structured candidates across 1 roots`) |
| `npx markdownlint-cli2` on the three edited/new docs | 0 errors |

Not run by the developer (coordinator's gate, task 3.2): any e2e tier; `task test:integration` (testcontainers,
Docker).

## Revision 1: `Validate()` rejects a malformed reference

Owner ruling via the coordinator, 2026-09-30.

`LoopExecutionEntity.Validate()` now checks a non-empty `ParentLoopID` and `InReplyTo` through
`TryLoopExecutionEntityID`. `TestLoopExecutionEntity_MalformedReferenceOmitsTriple` now also asserts that
`Validate()` returns an error. The sentinel annotation moved to `line=94` (one import line added).

Same `cp` + md5 ritual; only `agentic/loop_execution_entity.go` swapped, tests fixed across runs.

| Bytes | md5 |
|-------|-----|
| pre-revision (`ccd0d1da`) | `d33465d7b7dbcc2af797f4cd9f960aa5` |
| revised, and again after restore | `4ff0edd7ac217d2fba126d46a1b3240c` |

- GREEN, revised: all three `TestLoopExecutionEntity_Malformed*` / `ValidateRefuses*` tests PASS.
- RED, pre-revision bytes: `TestLoopExecutionEntity_MalformedReferenceOmitsTriple/{dotted_parent,dotted_reply-to}`
  FAIL with `Validate() = nil, want an error: a malformed reference violates the writer contract`; the other two tests
  PASS (they do not depend on the revision).
- GREEN, restored: all PASS.

Re-run gates on the revision tree (the committed bytes; only this table was filled in afterwards):

| Command | Exit |
|---------|------|
| `task check` | 0 |
| `go test -race -count=1 ./processor/graph-ingest/... ./agentic/...` | 0 |
| `openspec validate fact-lane-validate-and-identity-fence --strict` | 0 |
| `go run ./cmd/entity-id-audit .` | 0 (`line=94` pin verified) |

## Revision 2: one fence over the lane entry; RunID; production-seam proof

Review r1 (2 HIGH, 3 MEDIUM, 2 NIT). The coordinator's rulings of 2026-09-30 apply.

### Unit red/green against the pre-revision bytes (`6f15f8ed`)

Code commits are `dccb33dd` (graph-ingest) and `36c143b9` (agentic). The experiment ran at `36c143b9` with a clean
worktree, and the worktree was clean again afterwards. The same `cp` + md5 ritual as above was used.

| File | Pre-revision md5 (`6f15f8ed`) | Revised md5 (`36c143b9`), and again after restore |
|------|-------------------------------|---------------------------------------------------|
| `processor/graph-ingest/component.go` | `f2d04a95c2a86535f3c621be11fe28f0` | `4fa7278e1ce00934248b07ac37d52800` |
| `agentic/loop_execution_entity.go` | `4ff0edd7ac217d2fba126d46a1b3240c` | `a1ba87ba2acfafe9666bfc83de9d2fd1` |

Selected tests:

- graph-ingest: `TestDecodeEntity_FencesPayloadPanics`, `TestHandleMessage_RejectionsLandOnPoisonAccounting` and
  `TestDecodeEntity_MalformedLoopExecutionEntityIsInvalid`.
- agentic: `TestLoopExecutionEntity_MalformedReferenceOmitsTriple` and
  `TestLoopExecutionEntity_ValidateRefusesOnlyMalformedReference`.

Results:

- GREEN with the revised bytes, and again after restore: all pass.
- RED with the pre-revision bytes:
  - `FencesPayloadPanics/{panic-unmarshal,panic-validate}` and `RejectionsLandOnPoisonAccounting/{panic-validate,
    panic-unmarshal}` fail `should not panic` with `Panic value: fence test payload: UnmarshalJSON boom` and
    `... Validate boom`.
  - `MalformedReferenceOmitsTriple/dotted_run` and `ValidateRefusesOnlyMalformedReference/run_only` fail
    `Validate() = nil, want an error`.
  - These rows still pass on the old bytes, as expected: the rows already fixed in earlier revisions, and
    `MalformedLoopExecutionEntityIsInvalid` (Revision 1 already refused a dotted `loop_id` in `Validate()`).

### Integration: the real consume closure

`TestIntegration_FactLane_PoisonLoopExecutionEntityIsAckDropped` (`fact_lane_fence_integration_test.go`) publishes
hand-written `LoopExecutionEntity` wire bytes with a dotted `loop_id` on the `ENTITY` stream. The test waits until
`c.errors` has incremented by exactly one. It then checks the server-side consumer info: `NumPending` and
`NumAckPending` reach 0, and `NumRedelivered` is 0. Finally it lists `ENTITY_STATES` and expects no keys.

- With `component.go` alone restored to `1b1accf4` (`1bdf1efb…`), the test still passes. At that point the agentic fix
  alone makes `EntityID()` return `""`, which the pre-fix lane already rejected as poison. The lane change and the
  agentic change each close this case independently.
- RED with both files at `1b1accf4` (`1bdf1efb…`, `998e7303…`): natsclient logs `ERROR panic in message handler
  panic="LoopExecutionEntityID: ... loopID \"loop.dotted\" must not contain dots"` repeatedly, which is the
  Nak-and-redeliver loop. The test fails with `Condition never satisfied` / `the poison message must be counted on the
  lane's error path`.
- GREEN after restore (`4fa7278e…`, `a1ba87ba…`): the test passes.

### Gates (Revision 2 tree)

| Command | Exit |
|---------|------|
| `task check` | 0 |
| `go test -race -count=1 ./processor/graph-ingest/... ./agentic/...` | 0 |
| `go test -tags=integration -race -count=1 -run TestIntegration_FactLane_PoisonLoopExecutionEntityIsAckDropped ./processor/graph-ingest/` | 0 |
| `openspec validate fact-lane-validate-and-identity-fence --strict` | 0 |
| `go run ./cmd/entity-id-audit .` | 0 (sentinel pin unchanged at `line=94`) |
| `npx markdownlint-cli2` on every edited `.md` (change dir, both deltas, migration note, skill, concept doc) | 0 errors |

To reach 0 markdownlint errors, the two ADDED requirement headings were shortened to fit MD013's 80-character
heading limit, and each delta gained its `# <capability> Delta` H1, matching the archived neighbour. Nothing in the
tree cites these ADDED requirements yet. The one test comment that quoted the old heading was updated.

## Revision 3: a panic through the real consume closure; validate wording

This revision acts on two NITs from review round 2.

`TestIntegration_FactLane_PanickingPayloadIsAckDropped` publishes a fence test payload whose `Validate()` panics. It
publishes on the real `ENTITY` stream and makes the same server-side assertions as the poison row: counted once,
pending and ack-pending 0, redelivered 0, `ENTITY_STATES` empty. The two rows share `assertPoisonAckDropped`.

The red/green check used the `cp` + md5 ritual at `549b6a5c`, changing only `processor/graph-ingest/component.go`.

- RED: with the Revision 1 extraction-only fence (`6f15f8ed`, `f2d04a95…`), natsclient logs `ERROR panic in message
  handler panic="fence test payload: Validate boom"` repeatedly, which is the Nak-and-redeliver loop. The test fails
  with `the poison message must be counted on the lane's error path`.
- GREEN: after restore (`4fa7278e…`, matching HEAD), the test passes.

The wording "before any method of the payload runs" now reads "before any identity method (`EntityID()`, `Triples()`,
`StorageRef()`, `IndexingProfile()`) runs". The change is in the graph-ingest delta, the migration note and the
proposal. `design.md` D1 already said "between P1's two calls" and is unchanged.

| Command | Exit |
|---------|------|
| `task check` | 0 |
| `go test -race -count=1 ./processor/graph-ingest/... ./agentic/...` | 0 |
| `scripts/run-integration-tests.sh ./processor/graph-ingest/` (host lock, `-race`, whole package) | 0 |
| `openspec validate fact-lane-validate-and-identity-fence --strict` | 0 |
| `npx markdownlint-cli2` on the change dir, both deltas and the migration note | 0 errors |

## Breaking gate (task 3.2)

Final code revision `8e6cbeac` (`git rev-parse HEAD` equal to `origin/claude/gh1112-fact-lane-identity`, porcelain 0).
Pre-check before starting: `docker compose ls -q | wc -l` = 0; e2e processes = 0; Docker server 29.8.0; both peer
Claude sessions notified of the window, one confirmed clear. Run 2026-09-30 by the coordinating session:

| Command | Exit | Evidence |
|---|---|---|
| `task e2e:core` | 0 | tier ran and tore down cleanly (`docker compose ... down -v`) |
| `task e2e:agentic` | 0 | `msg="Scenario completed successfully" duration=5m35.485572417s` |

The agentic tier is the relevant one for the `LoopExecutionEntity` change (loops birth execution entities through
the writer's `Validate()` and the Graphable lane); the core tier covers the plain Graphable path. The one
`error:`-matching line in the agentic log is quoted in the coordinator's session record, not a stage failure.

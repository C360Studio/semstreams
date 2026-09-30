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

Scope note for 2.2 "Validate() returns an error on each": with a malformed org, platform or loop ID, `Validate()`
returns the identity error. With a malformed parent or reply-to, D3 omits that triple; `Validate()` returns an error
when no other spawn-identity fact remains (the `*_only` rows) and returns nil when other facts are present (the triple
is omitted and the rest ingests). Neither case panics.

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

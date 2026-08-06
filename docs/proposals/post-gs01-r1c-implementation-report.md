# Post-GS-01 R1c implementation report

Status: **complete; independently reviewed and approved**.

## Evidence character

This is the durable in-tree, integrator-observed execution record for R1c. It records commands and outcomes observed
during implementation and final integration. It does not claim that complete raw stdout or stderr logs are retained in
the repository.

## Identity and boundary

- implementation baseline: `313579fd8b66f0af9c62a8a05d9b9f9fffa486b4`;
- passed post-R1b inventory SHA-256:
  `e0f6479e1ace79a60b487fb32d7e001eb5192874fa9c7c81742ff3a984b74720`;
- semantic boundary: rule one-read/one-mutation/no replay and lifecycle owner-local full-intent reconstruction; and
- production-runtime delta: zero.

R1c added no production Go code, API, configuration, metric, retry helper, retry knob, coordinator, E2E hook, shim,
deprecated path, bucket, stream, service, or status key.

## Exact changed-file census

Behavior characterization:

- `pkg/projection/mutation_client_test.go`;
- `processor/rule/actions_reconcile_test.go`; and
- `pkg/lifecycle/manager_test.go`.

Current contract and adopter truth:

- `openspec/specs/rule-projection-mutations/spec.md`;
- `openspec/specs/lifecycle/spec.md`; and
- `docs/concepts/28-governed-semantic-state.md`.

Execution control and evidence:

- `docs/proposals/post-gs01-r1c-retry-truth-control.md`; and
- `docs/proposals/post-gs01-r1c-implementation-report.md`.

`docs/proposals/post-gs01-post-r1b-foundation-inventory.md` was materialized and preserved unchanged at the identity
above. It is prerequisite evidence, not an R1c-authored semantic delta.

## RED characterization evidence

Three reversible production mutations proved the new tests detect the forbidden behavior. Each production file was
copied before mutation, restored afterward, and verified byte-identical by MD5.

1. Lifecycle conflict continuation was temporarily changed to return immediately. Both changed-authority transition
   tests failed by returning the first `revision_mismatch` instead of rebuilding or rejecting fresh authority. The
   restored `pkg/lifecycle/manager.go` MD5 was `dc2e1d2b5398663e66308b71d93a8849`.
2. Projection reconcile was temporarily given a second authority read. `TestRevisionMismatchRemainsDefinite` failed
   with three requests instead of exactly one read plus one mutation. The restored
   `pkg/projection/mutation_client.go` MD5 was `675ff258bb0f4388f7f3ac3e6c9cbda8`.
3. Rule reconcile was temporarily invoked a second time after mismatch.
   `TestReconcileRevisionMismatchDoesNotReplayExecutionContext` failed with two calls instead of one. The restored
   `processor/rule/actions.go` MD5 was `342b792181b2171fb8c02cf1275ea312`.

The final diff contains none of those production mutations.

## Focused and contract gates

The final focused gates were observed green:

```text
go test -race ./pkg/lifecycle ./pkg/projection ./processor/rule
exit 0

go test ./test/contract/...
exit 0

openspec validate --all --strict
36 passed, 0 failed

git diff --check
exit 0
```

## Audit correction and broader gate disposition

The first full check attempt exposed a shifted line-addressed intentional-malformed entity-ID audit annotation after
the lifecycle test insertion moved the fixture. The annotation was corrected to the actual
`pkg/lifecycle/manager_test.go:1062` location without changing behavior or production code. The following focused
audit evidence passed after that correction:

```text
GOCACHE=/private/tmp/semstreams-r1c-full-gocache \
  go test ./internal/entityidaudit \
  -run TestAuditRepositoryFullWithAbsoluteRootReportsRepositoryRelativeCandidates \
  -count=1
exit 0
go run ./cmd/entity-id-audit ./pkg/lifecycle
14 structured candidates
exit 0
```

After the annotation correction, the integrator reran the complete unrestricted gate on the current worktree:

```text
GOCACHE=/private/tmp/semstreams-r1c-full-gocache task check:push
exit 0
```

That green run covered build, lint, default/integration/`live_llm` vet, schema drift, contract tests, the complete race
suite, and Docker-backed integration tests. The separate repository-wide `task entity-id:audit` command is not claimed
as a gate; it has unrelated existing findings. The audit evidence claimed by R1c is the focused repository audit test
and lifecycle-scoped CLI run above, both of which directly cover the corrected fixture.

## Review and E2E disposition

The independent `semstreams-reviewer` returned **APPROVE** after the requested occurrence-chain, timestamp, and durable
conformance corrections were applied and verified.

No product E2E tier was run. The reviewed R1c boundary changes no production runtime, and its accepted proof uses
characterization, contract, strict OpenSpec, focused audits, full race, and the green CI-equivalent `task check:push`.
No additional product E2E is warranted for this test/spec-only slice.

## Completion disposition

R1c is complete with no deviation. The next gate is an owner-approved post-R1c remap. The inherited old R1d, R1e, and
R2 sequence remains stopped until that remap records executable boundaries and prerequisites.

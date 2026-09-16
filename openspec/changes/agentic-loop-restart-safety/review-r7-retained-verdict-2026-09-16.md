# R7 retained-verdict implementation review

## Verdict and scope

**IMPLEMENTATION APPROVE — candidate-02**, independently reviewed on 2026-09-16.
The HIGH test-budget finding is closed. No runtime findings remain in this retained-verdict slice.
The earlier independent implementation review found no runtime-correctness findings but missed the test budget.
Its candidate-01 implementation and proof are preserved below; they are not current publication readiness.
This review does not complete full R7/R8, #1311, combined E2E, or publication/merge readiness.

### Test-policy correction

The candidate-01 top-level `TestIntegrationResponseRetainedVerdictReplacement` took 213.52s, exceeding the
three-minute integration-test ceiling in `docs/contributing/01-testing.md`. The independent reviewer confirmed
this HIGH finding; no exact exception existed. The five-minute package ceiling does not waive the test ceiling.

A mechanical candidate-02 correction splits the same seven cases into three sequential semantic top-level tests:
`TestIntegrationResponseRetainedVerdictReuseAfterReplacement` (two cases),
`TestIntegrationResponseAbsentVerdictCurrentPolicyAfterReplacement` (two cases), and
`TestIntegrationResponseGovernanceRefusalAfterReplacement` (three cases).
The entire per-case body is byte-identical; assertions, contexts, isolated containers, cleanup and production
30-second Retry are unchanged. No production file changed. Independent mechanical review APPROVED the split;
fresh grouped timing and final independent review now pass as recorded below.

Splitting does not reduce the aggregate measured cost: seven sequential container starts and approximately
3.6 minutes. The owner explicitly approved that narrow baseline-increase exception with "approved; continue":
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5695458130.
The independent reviewer also explicitly approved the exact exception. It applies only to these three tests and
seven existing cases, owned by Coby and the #1146 Codex/Sol claimant, and becomes invalid upon any change to the
case matrix, container strategy or production Retry timing. Real retained-source redelivery supplies evidence
helper-only tests cannot provide; isolated leaf containers and bounded cleanup remain unchanged.
This waives only the baseline-increase rule, not test/package ceilings or unrelated costs. No blanket waiver applies.
The original request is preserved in comment `5695122835`. The owner-decision hold is cleared; grouped native
verification was authorized. Fresh timing and final review close this finding without widening the exception.
No timeout increase, parallel execution, shared mutable fixture, runtime timing override or blanket waiver is proposed.

Candidate-02 native-file SHA-256: `64a4a7c11a97260d6425ce78b86758d50704d44281ba0eea20c024f78803675b`.
Nine-file manifest SHA-256: `4a6d77d32909f9364eb08888334eae79baa912e92cecca008a7b79af603df50b`.
Mechanical patch SHA-256: `db9568d121c802f410d8248341527b55d5f8bf72a036a66c612e855a9689b08d`.
The baseline, candidate and patch are preserved in `developer-test-grouping/` beside the candidate-01 bundle below.
Formatting, whitespace and integration-tagged language-server checks pass.

### Candidate-02 grouped native proof

One canonical invocation passed unchanged, with three top-level tests and seven leaf cases, zero failures,
skips or race reports. The top-level durations are 61.01s, 61.03s and 91.48s, each below the three-minute ceiling.
Aggregate test work is 213.52s; package 217.610s; runner 223s. Scoped integration-tagged vet and pinned revive
also pass with empty output. All nine candidate-02 hashes match before and after verification and final review.

```sh
GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly scripts/run-integration-tests.sh ./processor/agentic-loop -run '^TestIntegrationResponse(RetainedVerdictReuse|AbsentVerdictCurrentPolicy|GovernanceRefusal)AfterReplacement$' -v
```

Actual resources were seven added per-case NATS containers, one inherited TestMain NATS and one inherited Ryuk.
The log records eight NATS creations and explicit NATS terminations. All nine observed IDs and the runner lock
were absent afterward; cleanup required no manual intervention. Source and test files did not change.

Exact command, leaf timings, PID/ownership and resource accounting are in the existing durable
`developer-test-grouping/GROUPED-VERIFICATION.md`, SHA-256
`f232058ad4c20c4345b9877cabe45933b4527f5bf1c507ca930500f764e118c2`.
Native log SHA-256: `3eca00dec8cefed4991e367e49c5c7d542f20d6548e62338a1cd26cc5aa78ed4`.
Artifact manifest SHA-256: `fad2ccc7abe15de9b919a37c700f3236df475fb62a33ca233052a7c6260552df`.

The reviewer independently verified source hashes, command/output, timing, counts and cleanup and returned
IMPLEMENTATION APPROVE for candidate-02 only. Full pre-push, combined E2E, #1311 and whole R7/R8 remain separate
gates. The approved baseline-cost exception does not waive any of them.

### Full pre-push attempt after candidate-02

The complete `task check:push` attempt on 2026-09-16 exited 201 during its whole-repository unit race pass.
Build, lint, tagged static checks, schema consistency and contracts passed. Integration was not reached.
This is a failed full gate, not publication readiness or a runtime finding in retained-verdict behavior.

The entity-ID repository audit found stale positional fixture annotations in `processor/rule/actions_test.go`.
The unchanged intentionally malformed `e.1` value is at line 2144, column 47, but its annotation named line 2143.
Correcting that reference exposed the next fail-fast mismatch: the `$entity.id` template is at line 2964,
column 18, while its annotation named line 2963. An earlier approved wire-decoder assertion added one line above
both. A complete inventory found exactly these two annotations; both frozen-baseline targets have the same drift.

The aggregate correction changes only `2143` to `2144` and `2963` to `2964`. Classification, value, column,
surface, reason and test behavior are unchanged. The final file SHA-256 is
`751d8604359167aea7e964e1a2d04ce3b56da17073e6ca4fed607917facfa5a4`; the maintenance patch SHA-256 is
`725a994601fbc0e8d4c5224b047c3efe29585e24c0e01c36f6749cd12dcf0701`.
The exact focused repository audit passes: one test, zero skips, 14.47s test / 15.874s package, exit 0.
The first correction-only run remains a recorded failure. Independent aggregate review APPROVES the exact
two-pin maintenance and focused proof, without approving the full gate. Durable correction evidence is
`prepush-frozen.HBcZE7/annotation-maintenance/ANNOTATION-MAINTENANCE.md`, SHA-256
`843731bde9029e7f02fd6b32684013cdd4d45d364ca3658740afb67469c4b3bb`.
The focused GREEN log SHA-256 is `dc4c220e2f77ae1e935dcbeb6f7c397c7048eeb5d87bcbe1a29f1a2c35c4c5e0`.

At completion of the failed run, before annotation maintenance, all 2,248 source and 36 module/schema hashes
matched the pre-run snapshot; formatting and generation caused no source drift. Only root-announced
documentation changed. The failed command exited naturally and released its
ownership; no native integration or extra CI-parity gate ran.

Evidence is preserved in `prepush-frozen.HBcZE7/` under the durable R7 bundle:
`PREPUSH-FAILURE.md`, SHA-256 `3c2a7113fa883839bd2aa0f1d533bb31523aed83250417e1cccc0d98e8f2f0ee`, and
`check-push.log`, SHA-256 `6c22d4904821e8fba681662a34d53d76e046a363ea91f6f0d046516741fd4aa3`.
The complete gate must run again after the independently reviewed correction; partial passes do not replace it.

### Complete gate after annotation maintenance

The full `task check:push` rerun exited 0, using the same command/environment from a new frozen snapshot.
It ran from the beginning: build, lint, both tagged static checks, schema generation/no-drift check, contracts,
unit race and the canonical native integration runner. The loop integration package passed in 666.295s.
This supersedes the failed attempt as current test evidence; both failures remain preserved above.

The source snapshot and complete log are preserved in `prepush-reviewed.Vr3J7O/` under the durable R7 bundle.
The snapshot archive SHA-256 is `25ed720d47c1015779782aa29ec15d57e0c4f389e62e80923d8e0fef64d3161f`;
its source-manifest SHA-256 is `fb5dcdffdd90085da808587bcaa69a10e48994e3675a6da9b4d113ce6037061d`.
The nine retained-verdict candidate files and the reviewed annotation file were verified before launch.
The log is plain package output: individual assertion/skip totals are unavailable, and cached unit results are
not claimed as fresh executions. The earlier verbose seven-case proof remains separate.

All known task/runner/test PIDs exited and Docker reported no remaining containers after completion. No manual
cleanup, paid provider call, native rerun-to-green or source edit occurred during the successful run.
Final source/module/schema drift checks passed for all 2,248 source files and 36 module/schema files.
The additional CI-parity checks below remain separate. This result does not complete R7/R8, combined E2E or
publication/merge review.

### Post-staging fixture audit

The first additional entity-ID audit passed with 1,299 candidates but omitted the then-untracked new test files:
its source set uses `git ls-files --cached`. After staging the reviewed checkpoint, the same audit failed with
1,302 candidates and three invalid `ExecutionContext.EntityID="entity"` placeholders, in
`processor/agentic-loop/verdict_wire_integration_test.go:222` and `processor/rule/verdict_wire_test.go:19` / `:57`.
These are ordinary positive wire fixtures, not intentionally malformed-ID coverage.

The bounded correction replaces those three literals with the existing canonical `acme.ops.test.svc.entity.001`
fixture and adjusts one direct payload expectation. No runtime, helper, audit annotation, parser or waiver changes
are authorized. The exact fixture correction has independent APPROVE. Focused rule proof passes (two tests,
six leaves, 1.578s), and the exact native wire callback test passes (three leaves, 0.35s test / 4.075s package).
Owned native resources and the host lock cleared. The failed staged-audit log SHA-256 is
`a2344991c02d60aa31f49a3cb239a81ac1614ca19297109a33af55b59ae8fcad` in `prepush-reviewed.Vr3J7O/`.

The four-literal patch SHA-256 is `8f9d7b761878d534083fb6da120dc8da4a226684240bae6f7876f373fa5eac26`.
Final loop wire-test SHA-256: `6c71cbee66d90f45ef4040f9ac3e7a01b766e709830e36f0a23eea96cd7586b3`.
Final rule wire-test SHA-256: `aceabe609b8b42f8315110dccac8a2bb1101f1dbacfae6d6e471241ef0febbe4`.
Backups, patch and logs are in the durable R7 bundle's `fixture-identity-maintenance/` directory.
Its `FIXTURE-IDENTITY.md` SHA-256 is `c96b9699c2a46e656a22cb7de560bf5a9e21139c8842a154647f050fbd2539ed`.

Post-staging guards pass: entity-ID audit 1,302 candidates, properties 377/377, strict OpenSpec 55/55, Linux
CGO-disabled amd64 build, inventory/fixed-port fixtures, module tidy diff and API-compatibility fixtures (six cases).
Schema-generator tests pass with 14 top-level and 31 subtest passes, plus two explicit skips:
`TestSchemaValidationWithMetaSchema` and `TestMetaSchemaValidity`, because `specs/component-schema-meta.json`
is absent. No zero-skips claim is made. All 36 module/schema hashes remain unchanged.

The API compatibility report is unavailable: tool setup failed before comparing any packages (task exit 201,
nested exit 2). The offline temporary module lacks the dependency; the fallback uses unsupported
`go build path@version`. No network retry, script fix, dependency change or compatibility verdict is claimed.
CI explicitly treats this report step as nonblocking (`.github/workflows/ci.yml:236`), separately from its
required fixture step, which passed. This is the existing CI posture, not a new waiver. Preserve the report log
at SHA-256 `1085c004dcff631594ae304737a28bc92154a94874052e6283d4c2f88edead31`; tooling repair is not this slice.

The reviewer confirmed that the existing preflight policy requires another complete `task check:push` on the
corrected integration-test snapshot. Focused proof supplements but does not replace that gate. Finish staged
static guards before starting it; no incremental-substitution waiver is requested. Future checkpoint checks must
include newly added files before claiming a tracked-corpus audit pass. No guard script is changed in this slice.

### Final current-fixture full gate

The required complete `task check:push` rerun on the corrected fixtures exited 0 on 2026-09-16, collected at
approximately 10:59:40 UTC. It tested frozen staged tree `7a823337817a65f62deaec2ac3c872db9162bb15` on
HEAD `c347eff487f50b93bc338d764f43ef5b5ea5e133`, using the same readonly/offline command environment.
Build, lint, tagged static checks, schema generation/consistency, contracts, unit race and the canonical native
integration suite all passed. The loop integration package passed in 666.300s. No further test run was started.

The final evidence directory is `prepush-final.ldkJQ1/` under the durable R7 bundle.
Full log SHA-256: `96c505a73c42d43d7667e63046b63f8679809846ede25881b1c81e30c47e690f`.
Frozen source-manifest SHA-256: `e0f7a0c0b352359f989d2361ab4163654451f76d8e6e510bb2f95de1f3325070`.
This is the current-fixture full-gate result; earlier failures and the earlier successful snapshot remain history.
All 2,248 source and 36 module/schema hashes matched the final snapshot. The four known task/runner/test PIDs
were gone, Docker's testcontainers listing was empty and the host lock was absent. No manual cleanup was needed.
Plain output records 154 unit package passes (144 cached) and 154 native package passes (none cached), with
20 no-test-file packages in each phase. Individual skip/assertion totals remain unavailable. Only root-owned
verification records changed after the run; production and tests remain frozen. The API-report limitation and
two schema-generator skips above remain explicit; no compatibility or zero-skips claim is added.

R7/R8, #1311, combined replacement/E2E and whole-PR review remain open. A checkpoint push does not complete those
obligations or authorize a parent advance, restack, archive, merge or closure.

### Previously reviewed candidate-01

Base: `c347eff487f50b93bc338d764f43ef5b5ea5e133` plus preserved starting WIP in the #1159 claim worktree.
The approved task is `task-r7-retained-verdict-2026-09-16.md`, SHA-256
`5359f69e4d69c7cc9e91d0329d98c8af1795b1f36e0bf842164ed86df5242b1e`.
The exact nine-file candidate manifest is SHA-256
`539fb851e53c2f07b7c176db159eda6d8f239d868088478a65c337da5f6e636e`.
The WIP-relative patch is SHA-256 `6d9e9cdd07e8ab6749eff140047cdba32dc482ea9b34fce4b21f6aa361ce2ef1`.
All nine live file hashes matched after verification and independent review. Earlier WIP was preserved.

Four existing production files changed (+172/-41 lines); five same-package test files changed, including two
new focused test files. No public signature, configuration, timer, bucket or recovery runtime was added.
The developer released source and test-runner ownership; no background test remains from this slice.

## Ruling conformance and existing owners

All file pins below are relative to `processor/agentic-loop/` at the reviewed candidate.

| Accepted responsibility | Implementation |
| --- | --- |
| Reuse or typed absence (`5682070598`) | `settlement_recovery.go:131` and `:197` |
| Both streams; only typed message absence | `settlement_recovery.go:64`, `:103`, `:211` |
| Failed/unresolved observation Retry | `settlement_recovery.go:206`, `component.go:1557` |
| Dual-match Quarantine (`5694233488`) | `settlement_recovery.go:228` |
| Mandatory constructor and response wiring | `component.go:357` and `:1557` |
| Shared wire/subject check and proposal match | `component.go:2567`, `settlement_recovery.go:197` |
| Private per-invocation operation; public handler signature unchanged | `handlers.go:1194` |
| Errors are not synthetic policy rejection | `governance_dispatcher.go:470` and `:491` |

The reviewer traced the prepared `FailureState` branch: these governance failures return a nonterminal result
without a prepared failure state, so the response owner releases speculative process state and returns the
classified disposition. It does not persist a synthetic business failure and ACK that delivery.
Missing/full live waiters retain their existing behavior; retained reads do not authorize orphan-verdict ACK.
Disabled/audit behavior and ordinary configured verdict timeout remain unchanged.

## Verification

Commands ran in the claim worktree with:

```sh
GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly
```

| Check | Result on the frozen candidate |
| --- | --- |
| Focused source `-race` | Exit 0; 25 leaves; 1.440s |
| Full loop `-race -json` | Exit 0; 571 top-level / 1,260 total RUN events; zero failures/skips; 3.975s |
| Native replacement | Exit 0; seven leaves; test 213.52s, package 215.590s; zero skips |
| Existing native controls | Exit 0; three top-level tests, six leaves; 33.492s; zero skips |
| Integration-tagged scoped vet and pinned revive | Exit 0; empty logs |
| Nine-file gofmt, diff whitespace and final manifest verification | Clean; all nine hashes match |

Exact primary commands:

```sh
go test -race -json ./processor/agentic-loop -count=1
scripts/run-integration-tests.sh ./processor/agentic-loop -run '^TestIntegrationResponseRetainedVerdictReplacement$' -v
```

The exact focused and preserved-control selectors are recorded in the checksummed `EVIDENCE.md` below.
Preserved controls use the same runner with `-run` and `-v`.
Vet and revive used `GOFLAGS='-mod=readonly -tags=integration'`:

```sh
go vet ./processor/agentic-loop
go tool revive -config revive.toml -formatter friendly ./processor/agentic-loop
```

All native runs used the canonical runner and its shared lock; no contention, direct tagged test invocation,
lock bypass or paid provider call occurred. Replacement rows retain the production 30-second delayed Retry.
Full task lint and whole-repository push gates were not run; scoped lint does not replace them.

### Failure-to-success evidence

Preserved initial REDs contain 13 retained-response cases and three enforce-error cases; the source-owner RED
separately demonstrates publication and missing-reader failures incorrectly ACKing. Its cancellation control
already passed. Expanded tests caught missing/empty-address errors leaking the Invalid classification.
The native constructor RED then proved that the real replacement response could not ACK without default reader
composition. These implementation failures were corrected and the final frozen candidate passed.

Intermediate fixture failures are also preserved, not counted as production RED proof: one pending-tools
assertion expected an execution ID instead of the existing call ID; the initial native fixture assumed the reader
was already installed; a publication assertion compared the outer callback context to the owner's timeout child.
The corrected native assertion observes and compares the exact operation context through an existing evidence hook.

Native rows cover reuse of both decisions, both current-policy outcomes after absence, dual-match Quarantine,
actual DiscardNew publication refusal, and cancellation. The refusal is the real broker's API503/err_code10077
`maximum messages exceeded`, with source Retry and no tool work. The dual-match case leaves the source unsettled
and drains only its affected response consumer. Required KV/stream effects and wire decoding are real.

## Reproducible artifacts and remaining limits

The durable local bundle is beneath
`/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r7-retained-20260916.uSBSQW/`
in `developer-candidate01-evidence/`. It contains the complete starting loop tree, exact candidate files,
WIP-relative patch, manifests, every failed/intermediate/final log and exact commands in `EVIDENCE.md`.

| Artifact | SHA-256 |
| --- | --- |
| `EVIDENCE.md` | `701c6ac2f329efc62e37dabdf73af499b51fb2358617f69e653236254a1e2d7b` |
| `native-context-fixed.log` | `c5a573d0e118b818dc0819fad19d5ae2d0a7a8bfc63136f5a4116ae00510c342` |
| `native-controls.log` | `a56362601ba0dbaf37ad7876991b8cffc694ac648bd8a4afb375d50107398049` |

The reviewer independently checked the complete patch, caller/implementer references, production paths,
source hashes and command evidence. Root independently rechecked hashes, event counts and native completion.

Native tests seed retained verdicts and use controlled current-policy evaluation. They do not prove real rule
evaluation or #1311's proposal-source-to-verdict settlement. Optional graph/audit providers are unavailable in
these fixtures. Separate tool-effect protection, observed DiscardNew and other R8 obligations remain required.
No finite verdict-retention horizon is reintroduced.

R7/R8 remain unchecked. Preserve frozen #1156, the held #1312 sequence, combined replacement/E2E and whole-PR
review/publication gates. This checkpoint makes no commit, push, restack, archive, merge or closure claim.

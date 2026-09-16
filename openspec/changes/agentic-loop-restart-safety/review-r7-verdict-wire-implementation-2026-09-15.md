# R7 governance verdict wire implementation review — 2026-09-15

## Scope and verdict

Independent SemStreams reviewer: **APPROVE — bounded R7 verdict-wire implementation only; no findings**.
Reviewed baseline: `c347eff487f50b93bc338d764f43ef5b5ea5e133` plus preserved local R6/R7/R8 work.
No commit, push, rebase, archive, merge or issue closure is part of this checkpoint.

The implementation conforms to the independently reviewed design
`design-r7-verdict-wire-2026-09-15.md`, SHA-256
`91480e9cd53dba53c74f388fdf0ab04e6f1687fa33c3e7493eb10469740524c4`.
Owner rulings: registered wire boundary
[5677581095](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5677581095), bounded intake
[5677867478](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5677867478), and exact typed signature
[5678920204](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5678920204).

The reviewer separately approved the additive spec promotion and migration section. R7 and R8 remain unchecked.

## Exact reviewed implementation

Final manifest SHA-256: `769135d0f27a3871967f8763efa2740e2b86013ed1573ca01a4a6e79ce5082aa`.
Root and reviewer verified all 13 entries:

```text
1403942ea497a7214bca8dbfa641d14cb5d93f6024f26b4561abf5ee592d7933  processor/rule/actions.go
f18512418750964f097a0b9fc53743630ce7618d90adb8ee2010677f186b83c3  processor/rule/actions_test.go
ed6318099ac9f83c60ac665419eaddf72f2fc22b28f471b62036837b6e4d9579  processor/rule/verdict_wire_test.go
6b8d0e247ecca71bb0570e87fa3aee1e0bb2bf6c11a06a116e0f8f34758972db  processor/agentic-loop/component.go
5056a7155efcefb4495c90ae13a95c65f3670d81ed33aefa00806f5dd4d35a05  processor/agentic-loop/governance_dispatcher.go
7b43f2b1398a49d12022171085069e86938273d74d545b5e26c811c8ecc817d5  processor/agentic-loop/handlers.go
16d62bf2a65af755bb9712f79bb7a5ffd778e2ab9b24ca28a557ca6775f8fb4f  processor/agentic-loop/governance_dispatcher_test.go
d82edf45a6829f4d794a4b07b287d8d35cc7b3fe36a4f6473fa2e96bf89e766b  processor/agentic-loop/recovery_test.go
b1dd53af123b8ca223832106f4fe8cc6452b7b493e588e89829fb8aac86dfe5d  processor/agentic-loop/execution_identity_test.go
cf2c5eac3d8304c02d3153223e65d06ebebf401a09750d000c1c8410484ba303  processor/agentic-loop/delivery_owner_test.go
16a386dd1f6f5d86336b30918f82fd76bafcea7811a357cded630d4ca3266cc2  processor/agentic-loop/verdict_wire_test.go
466b8261eff3d71f4929afad582eff9df89274ad85137bafe290a08a94f8f7d8  processor/agentic-loop/fastlane_replacement_integration_test.go
ce9600dfcbad325fcf1c857d05eeab72bb379c918a29360c5ebda7b1ec68b5f9  processor/agentic-loop/verdict_wire_integration_test.go
```

The three new files are the two verdict_wire_test.go files and verdict_wire_integration_test.go. All other edits
are the approved production handoff, associated comments, or directly affected existing fixtures/callers.
Isolated diffs compare against checksummed pre-slice backups, not HEAD's combined R6/R7/R8 diff.
Reviewed R8 publisher files and trajectory_handler_wiring.go remain unchanged by this slice.

## Conformance

| Accepted obligation | Reviewed implementation |
| --- | --- |
| Registered carrier only for the two verdict families; preserve complete inner wrapper | `processor/rule/actions.go:1128` |
| Existing typed method and return contract; five implementations adapted | `processor/agentic-loop/governance_dispatcher.go:258` |
| Registered decode, explicit validation, no raw fallback/redecode | `processor/agentic-loop/component.go:2570` |
| One nonmutating correlation/diagnostic normalizer for wire and direct inputs | `processor/agentic-loop/governance_dispatcher.go:162` |
| Observe actual subject; refuse mismatch before effects | `processor/agentic-loop/component.go:1077`, `:2561` |
| Preserve valid mode outcomes, missing-waiter Retry and full-waiter Quarantine | Existing dispatcher controls, refusal matrix and native replacement proof |

No new exported symbol, payload family, state store, serializer, recovery mechanism or policy language was added.
The existing unused private subject-fallback helper was retired. Optional diagnostics retain their defined
precedence; they do not become required correlation. Malformed or conflicting inputs do not mutate waiters.

## Evidence and exact scope

Developer handoff SHA-256: `b8caed346cf72a2d560ca76ef9a36b083476a4528fde8254f0e1ac7c2a6fa1b1`.
Durable backup:
`/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r7-verdict-wire.nHHlAB/`.
Its evidence directory contains the handoff, exact commands/regexes, logs, manifests and isolated baseline diffs;
its source archive preserves the reviewed runtime/tests and shared records.

Commands use `GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`.

- Intended RED: both publish families failed production registry decode; both registered authoring shapes lost
  reason/rule ID; raw input incorrectly ACKed. Four outside-family controls passed. Exact initial rerun GREEN:
  nine leaf cases over four top-level tests, rule 1.539s / loop 1.485s.
- Final focused race matrix: 13 top-level tests, 156 leaves, 171 named PASS nodes including 13 fuzz seeds;
  rule 1.366s / loop 2.205s. The handoff records the exact selection and distinguishes seed execution from fuzzing.
- `go test -race ./processor/rule ./processor/agentic-loop -count=1`: PASS, rule 4.970s / loop 3.663s.
- Canonical native runner: actual exported rule actions, real PubAck, Start-installed callbacks for approve,
  publish-approved and publish-rejected, preserved diagnostics, subject-conflict refusal, and existing missing-
  waiter replacement. Two top-level tests; new test 0.38s, replacement 30.36s, package 34.710s; no skips.
- Bounded native fuzz: initial 12-seed target passed 8,440 executions in 13.617s; final enlarged 13-seed target
  passed 525 executions in 13.581s. Both used `-race -fuzztime=10s -parallel=2`; no runtime correction resulted.
- `go vet -tags=integration ./processor/rule ./processor/agentic-loop`: clean.
- Pinned revive for both packages: clean after declaration-only grouping of adjacent handlerFn/admission variables.
  The change reduced the setupConsumer statement count below the existing lint limit without adding a helper.
- `git diff --check`, final manifest verification and strict active OpenSpec validation: PASS.

Native/fuzz runs used component SHA `7035387bc3b67787f2a05828cbf887ddd6df3e2b977dc3dc1e7a12f3130bd213`.
The final component differs only in the adjacent declaration grouping. The reviewer verified it as behavior-neutral;
final full-package race, focused matrix/seeds, vet and lint cover the final source. Native/fuzz are not relabeled
as rerun after that edit. The native publisher adapter supplies transport, not production R8 classification proof.

## Remaining gates

R7 still requires originating-proposal matching, retained-verdict lookup, #1311 source settlement and remaining
replacement evidence. R8's action admission/missing-publisher/authority work is unchanged. The full combined push
gate, schema/contract suite, relevant agentic E2E, R11 proof and final archive/spec sync are not claimed here.

Root ran `task spec:properties`: FAIL, 3 of 330 tracked citations unresolved. They predate this slice and match
HEAD c347eff4 with no local edits to either test file:

| Existing location | Unresolved citation |
| --- | --- |
| `agentic/state_test.go:15` | `agentic-dispatch / The shared loop view classifies the mixed bucket` |
| `processor/agentic-loop/create_vs_exists_fence_test.go:417` | Extra `Requirement:` before the existing heading |
| `processor/agentic-loop/create_vs_exists_fence_test.go:498` | Same extra prefix |

These remain in R14's existing spec reconciliation, not a new issue or a runtime edit in this wire slice.
The checker uses tracked `git grep` and does not yet scan the new untracked wire tests. Root and reviewer separately
verified their citations against the exact promoted governance heading; the global checker is not claimed green.

All owned test jobs completed. The canonical native runner completed cleanup. Shared-host process inspection
found no active SemStreams test/spec-check process at checkpoint, apart from the inspection itself.
The draft remains local/uncommitted at this checkpoint; no permission or test waiver for a push is inferred.

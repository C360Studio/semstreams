# R7 live proposal-match implementation review

Status: **SCOPED IMPLEMENTATION APPROVE, no findings**, 2026-09-15. This completes the live comparison slice,
not retained-verdict recovery, R7/R8, #1311, or a push/merge gate. All changes remain local and uncommitted.

## Authority and exact source

The architect's task `task-r7-live-proposal-match-2026-09-15.md` passed independent CONFORMANCE review at
SHA-256 `fc4b2d4c6a1c43947ac27c6b2c35705281a6f6628c2cb7834060bb584ecb7ca1`.
It implements the already-accepted correlation requirement under standing execution approval. Owner comment
`5679435736` extends bounded intake only; it is not new behavior or API authorization.

Worktree: `codex/gh1146-agentic-loop-restart`; HEAD/upstream `c347eff487f50b93bc338d764f43ef5b5ea5e133`, 0/0.
PR #1159 remains draft and based on frozen #1156 at `417beae5552f8f15ad3540edd7d8504c87174c13`.

Exact eight-file manifest: `73f664e4216acf1b9127f476c9d8a4d63dbc3f41eeacc28403aa315aae292a37`.
Exact starting-WIP-relative patch: `108819cdb02f60291472d1e7a35af30a98ea53043f9c32c6637d6b5c70893a9c`.
The sole production file, `processor/agentic-loop/governance_dispatcher.go`, is
`85e35f619380fc23cbbe2411b130d9f5ee2d90c5b2a57828fb9da3c7729cef24` (+87/-41).
The remaining changes are six existing test files and one new focused test file.

## Reviewed behavior

One prepared, fingerprinted proposal is held with the existing waiter and is the value actually published.
The enforce handler normalizes, locates the waiter, compares originating identity, then enqueues. Conflicting
loop/request/execution/fingerprint or nonempty CallID quarantines before mutation. Empty CallID remains valid;
Reason/RuleID remain diagnostics. A refused mismatch leaves the slot available for a matching verdict.

The same waiter map, mutex, channel, lifecycle and public signatures remain. Audit/disabled behavior and the existing
per-call preparation/publication failure policy are preserved. No new API, store, timer, goroutine, context retention
or coordination owner. Preserving that failure policy here does not approve it as the final R7/R8 settlement policy.

Independent reviewer `/root/r5_tool_review` read the complete 1,170-line WIP-relative diff, verified all eight hashes,
checked the production call sites and final test evidence, and returned APPROVE with no findings. Root independently
verified all eight current hashes, artifact hashes and the intended failing assertions.

## Verification

All Go commands used `GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`.
Exact commands and complete logs are preserved in `evidence/handoff.md` and its adjacent files below.

| Proof | Result |
| --- | --- |
| Real Propose, direct/registered flat/nested mismatch tests | 13 intended RED leaves: 12 field/carrier rows plus cross-execution conflict; then GREEN |
| Positive controls | Optional/correct CallID, both decisions, diagnostics, early arrival, fixed published fingerprint, repeated provider IDs |
| Final `go test -race ./processor/agentic-loop -count=1 -v` | PASS, 4.195s, zero skips, including all 20 explicit fuzz seeds |
| Existing fuzz target, race, 10s, two workers | PASS, 671 executions, 13.622s; 20 explicit seeds plus one existing cached baseline |
| Canonical native runner, exact `TestIntegrationGovernanceLiveProposalMatch` | PASS, two isolated cases, 0.70s; package 4.250s, zero skips |
| Tagged package vet, pinned revive, formatting/diff check | Clean; executing owner reported exit 0 |
| Root strict OpenSpec validation and new live/wire citation heading checks | 55/55 valid; named untracked citations resolve |

The native matching case holds the real proposal publisher after PubAck, so actual source ACK observes delivery
in the buffered waiter before Propose consumes it. The wrong-fingerprint case observes zero ACK/NAK/TERM, an untouched
waiter, unhealthy status, and one drain of only the affected verdict consumer. Both work paths join.
Native verification preceded only one additional valid nested fuzz seed; production/native source stayed unchanged,
and the final full race run includes the added seed. This is live matching, not retained reads or restart proof.
Adapted manual-waiter replacement/rule-wire fixtures compiled under tagged vet but were not rerun by this slice.

## Preservation and remaining work

Durable source/evidence directory:

```text
/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r7-live-match.khpIcD/
```

`source-wip.tar.gz` captures the 43 explicitly enumerated dirty/untracked files before this review record was added.
Its SHA-256 is `f4f98d9281b6dfeb9919a315c344c899fddea4b095e817a365c22cdb2b474e5d`.
`evidence/` preserves the frozen developer handoff, exact diff/manifest, all logs and starting loop-source backup.
All 124 starting loop-file backups verify; all other loop files are unchanged. The earlier 39 governance/client,
three rule-wire and three R8-publisher source checksums also match. This is not an all-repository baseline claim.
Root-owned record changes are separate. No module churn, parent change, rebase, commit, push or archive occurred.

All test jobs completed and developer write ownership was released. R7 retained reads, source error propagation,
admission and replacement/publication proofs remain open; #1311 stays separately held. Full repository push gates,
relevant E2E, R11 and whole-PR review were not rerun or waived by this scoped pass.

## Next prerequisite: bounded inventory/conformance pass

`inventory-r7-replay-admission-prerequisite-2026-09-15.md` received independent scoped INVENTORY/CONFORMANCE PASS
at SHA-256 `4b3dc5f0131e5069623e6d231d8174eb78ae5f6ccad00bb386cab674aca6302d`; root and reviewer verified 41/41 pins.
The already-approved R8 local observed-stream admission is the supported next prerequisite before retained absence
can justify proposal republication. No new owner sequencing decision is needed. Admission is necessary, not sufficient
proof of safe absence/reuse. Full R8 inventory/design, executable horizon arithmetic, retained reads and source error
propagation are not approved by this bounded review. The exact supplement remains at its reviewed identity.

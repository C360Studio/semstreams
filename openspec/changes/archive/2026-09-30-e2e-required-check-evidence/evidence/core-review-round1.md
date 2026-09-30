# #1222 core implementation review

Mode: independent bounded implementation review, read-only. **CHANGES REQUESTED — K1 (HIGH).**
This verdict covers the three frozen core files only. It is not whole-feature approval. No tests, source edits,
Git mutations, GitHub writes, Docker, integration or other gate runs were performed by this reviewer.

## Snapshot and authority

Worktree: `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`.
Base: `fe6e2cc03e16f5db47e293f55939548572f204cc`.
Authority: accepted design SHA-256 `a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c`,
active e2e-evidence spec delta, accepted implementation handoff and bounded core pass-through catalog.
The three hashes supplied by the author matched before and after review:

| File | SHA-256 |
| --- | --- |
| test/e2e/scenarios/core_health.go | 2d391ec5b041b19799736fd203787a3b97c0684c8aecf7775f82aa9e75a8c31d |
| test/e2e/scenarios/core_dataflow.go | 0ad581f8d56e1fadd620a3094e821b81b4c376535da1ed3e9259fa900f188c0d |
| test/e2e/scenarios/core_evidence_test.go | c3483a27ff6770b9a31fc604f77cbff3cb9434e4a6501243c994bc628a125983 |

Read the full tracked core diff and new tests, plus execution/lifecycle paths, actual file retrieval helpers,
configured protocol-flow pass-through, JSONMap output serialization and File JSONL emission. The author handoff's
appended core checkpoint was read from `/private/tmp/semstreams-1222-result-fixes.md`; the root identifies its
retained copy as `evidence/result-fixes-round2.md`. Prior Writer findings are not reopened by this review.

## K1 — HIGH test/e2e/scenarios/core_dataflow.go:325 — Unperformed comparisons are recorded as passed

Mechanism: named content and identity status are inferred solely from whether each issue slice is empty
(:325–326, with empty reason becoming passed at :405–411). But the validator's early exits leave a sibling
comparison unperformed without an issue:

- A selected malformed JSON line appends only ContentIssues at :368–370 and never decodes/checks its run/sequence.
  The identity issue slice remains empty, so the runner records `core-dataflow.pass-through-identity=passed`.
- Foreign/missing run or sequence and unsent sequence append only IdentityIssues at :373–380, then skip lookup
  and comparison of the independently expected type/value/timestamp. The content issue slice remains empty,
  so `core-dataflow.pass-through-content=passed` is recorded without the required comparison.

These are not merely helper labels: executeValidateProcessing publishes all three named observations before
returning the failure. Even a selected set consisting only of an unsent sequence produces a passed content
observation. The active Required observations determine success clause at spec.md:58–59 requires the expected
identity/value to be compared with the actual consumer result before recording passed. Observation failure must
remain a failed observation with a reason; an empty issue list is not evidence of an evaluation.

Fix: retain the actual evaluation status of each named obligation. Malformed selected JSON must make unavailable
identity proof failed (with its reason); identity errors that prevent expected-content comparison must likewise
leave content failed/unproven, never passed. This can be a small correction to the existing issue/status model;
no new framework or threshold is needed. Count can keep its independent distinct-valid minimum semantics.

Verification/refutation: the overall scenario still returns a failure at :327–328, and the shared finalizer cannot
make a complete required result from that failure. This is a concrete false-positive named observation, not a
whole-run false-green claim. Current tests explicitly expect no identity issue for malformed JSON
(core_evidence_test.go:103) and no content issue for unsent/foreign identity (:104/:106). They inspect only the
helper result, never the generated CheckObservations, so their green does not refute K1. Add controls that assert
the named statuses/reasons through observation recording and finalization for malformed, unsent, foreign and
missing identity cases, including at least one other valid line and a sufficient distinct count. No executed
reproduction is claimed; the code path is statically established.

## Conformance and refuted concerns

| Obligation | Evidence / disposition |
| --- | --- |
| Accepted narrow catalog | core_health.go:80 declares core-health.components; core_dataflow.go:100–105 declares the three accepted pass-through IDs. No selective-filter or extra capability guarantee is added. |
| Oracle originates in sent data | core_dataflow.go:243–265 constructs expected value/timestamp before send and records only successful UDP writes. Run ID and sequence are included in transmitted JSON. |
| Current config is pass-through | configs/protocol-flow.json:182 has empty criteria and :215 empty mappings. Removed greater-than-50 expectation is correct; the 40/60 positive example covers both sides. |
| Consumer wire matches production | JSONMap wraps NewGenericJSON(transformed) in BaseMessage and marshals it (json_map.go:625–638); GenericJSONPayload.Data has json:data; File jsonl appends that message and newline (file.go:677–680). payload.data decoding matches this actual path. |
| Distinct count cannot be inflated | core_dataflow.go:386–389 increments once per successfully sent sequence only after exact value/type/timestamp checks. Duplicate mutation is detected. |
| Current-run identity enforced for selected records | :373–380 checks run equality, sequence presence and membership in successful sends. Unrelated lines lacking the raw marker do not inflate count. K1 concerns the sibling unperformed observation. |
| Content corruption is fatal | :382–384 compares all original sent fields; :327–328 returns error when content issues exist. Corruption is no longer just a warning. |
| No health substitution | Count/retrieval failure calls failPassThroughChecks and returns error (:294–306); the old executeValidateComponentsOnly fallback is removed. |
| Missing output stays red | Zero retrieved lines or count zero with retrieved lines fails; zero selected lines adds content and identity failures; low distinct count fails independently. |
| Required health observation at actual check | core_health.go:179–249 records component query/missing/minimum/unhealthy failures; :252 records pass only after configured checks succeed. |
| Earlier/later failures remain fatal | Both Execute loops set Result.Error and return partial Result on stage failure; dataflow's raw ObjectStore and max-delivery stages remain after validation. Successful named checks do not erase those errors in the shared finalizer. |
| Record refusal is not silently cleared | Helpers ignore RecordCheck's returned error, but the existing Result method retains it in Errors. Runner finalization sees it; this is not discarded acceptance failure. |

CountFileOutputLines and GetFileOutputLines swallow some Docker-command errors into zero/empty values in the
existing client (observability.go:198–200/:235–236). The new scenario's zero/empty guards nevertheless fail closed,
including count-zero followed by a successful nonempty retrieval. This refutes restoration of the former component
fallback. Exact underlying transport cause may still be lost by the old helper; no improved transport diagnostic
claim is made by this slice.

The raw-marker substring prefilter (:353) is followed by exact decoded identity validation. It cannot make a foreign
record satisfy count or identity. It can conservatively select a line that only mentions the marker elsewhere;
the current test intentionally requires such a line to fail. This review does not broaden that into global rejection
of all unrelated output, nor infer guarantees of all-message UDP delivery, ordering or exactly-once output.

Scenario Execute only declares when identity config is supplied. CLI constructor wiring was still pending/being
fixed outside this freeze; an unconfigured constructor must not be represented as adopted proof. This bounded
review does not certify the default/all invocation end to end. The core slice adds no competing persistence,
NATS, state or finalization owner; the private validator owns the capability-specific expected/actual comparison.

## Tests and mutation evidence

All retained logs below were read and hashes verified:

| Artifact | SHA-256 / observed evidence |
| --- | --- |
| /private/tmp/semstreams-1222-core-red.log | 5f273d6da32241a757812b8de618ca56c10e28d0609ce0a12c8a97f3cc2f10c6 — named health observation absent and UDP run identity absent fail assertions |
| /private/tmp/semstreams-1222-core-race.log | a784d5f381911ec7b6ceb1dc9df26f0565bddf57eaae895945937212c5856348 — focused health, UDP and six validator examples pass; author identifies go test -race |
| /private/tmp/semstreams-1222-core-mutant-red.log | 6af38eaf4400e05fb79730cb9aaa5536869411083391b66c143defe6e9670dce — duplicate_cannot_inflate fails because Distinct is 2 rather than 1; sibling cases execute |
| /private/tmp/semstreams-1222-core-restored-green.log | 8a7aa261b305ee75129262ef4bee7e8062e866b72734a4cb04c3415e6a099486 — same six validator examples pass after restoration |

The author records the valid mutation `if !seen[*data.Sequence]` -> `if true`, with cp backup/restoration.
The retained backup `/private/tmp/semstreams-1222-core_dataflow.go.bak` matches the reviewed/restored source hash
`0ad581f8d56e1fadd620a3094e821b81b4c376535da1ed3e9259fa900f188c0d`. The raw mutation output shows the intended
duplicate assertion reached, not a build error or unrelated timeout. **Detected**, bounded to distinct counting.
No claim is made that this single mutation establishes content/identity/fallback/outer-exit sensitivity.

The health example uses the production HTTP client and Execute, and the sender example observes actual UDP JSON on
an ephemeral local socket. The validator examples use independently chosen sent expectations, including 40 and 60;
they do not recompute the expected result by invoking the subject. This is useful lower-tier evidence. They do not
exercise file retrieval, the named recording/finalization bridge, setup/teardown error propagation, failed artifacts
or a real UDP-to-File application pipeline. No new tests in this slice drive executeValidateProcessing itself.

Named examples are reasonable for the current finite count/content/identity cases, supplemented by the shared
Result properties already reviewed. A generated permutation/duplicate law would be useful if this validator grows,
but a second required-set property suite is unnecessary. K1 shows the current sibling-status expectations need
correction; sheer case count or the duplicate mutation does not establish observation truthfulness.

The accepted remaining core mutations include malformed/wrong content, wrong run/sequence, below-minimum output,
and the old component-only fallback. They are not demonstrated by the duplicate mutation. Temporary deferral is
accepted only for this intermediate read-only checkpoint; final scope proof still requires the planned compiled,
reached mutations with restored baselines and the actual assembled core run. No full scenarios package, CLI, Task,
Docker, hosted-retention or final push-gate success is inferred. Previously blocked full-package listener tests
remain distinct from this author-reported focused escalated run.

Compiler-backed structural discovery remained unavailable in this review session because gopls workspace loading
hit restricted Go cache access. Bounded rg enumerated core constructors, current CLI callers, declaration helpers,
validator use, output transport/serialization and file retrieval. No complete gopls enumeration is claimed.

**CHANGES REQUESTED — K1.** Re-review the corrected observation statuses and focused evidence at a new frozen
snapshot, then continue the separate wiring and assembled proof. No change to the accepted core scope is requested.

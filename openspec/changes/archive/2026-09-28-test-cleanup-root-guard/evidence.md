# Implementation evidence

## Current reviewed manifest

The installed exact manifest is SHA256
`a5b7ea6f86d2b8a35b104063c1fdffba440b511cdb81f2667e591579eabcefe1`: 334 legacy debt entries and 86 manual
resolutions. Independent source and exact-record review is retained in `review/final-manifest-review.md`, with
canonical validation of all 445 explicit dependencies and full source snapshots. This supersedes the earlier
325-entry checkpoint below. The 1,402-site census retains 641 ordinary-only unknowns; those are not safety claims.
Final classifier source review passed at the hashes below. Installed-manifest guard validation passed;
full local preflight passed. This change is archived with its synchronized capability spec.

The final process-reader extraction received scoped approval at SHA256
`339ca6f11b481ccdd13f8b380e55168c671fb9fd1115845363b2805404dc0729`; event ordering and ownership are unchanged.
Earlier process results below predate that extraction; the full local gate subsequently validated the current
source under both race selections.

Final staged citation resolution passed 445/445 references, and its checker fixture passed 34 checks. The initial
untracked-file checkpoint covered only 444 citations; it is not the final evidence. Strict OpenSpec validation
passed all 58 items before archive. Post-archive citation, strict validation and queue checks are separate checks.

## Admission slice

Evidence applies to the dirty implementation over claim `f60d78906086f0c9090a99f24c389f32d40cc167`.
The following checks execute isolated temporary Git modules, actual Task definitions and the actual integration
runner. Their Go shim invokes the real fixture analyzer; later expensive commands are fenced. They do not execute
Docker-backed integration or contact live providers.

The full admission matrix passed in 34.029s. It exercises fifty entry/classification combinations, focused
iteration, CI ordering, and process ownership controls:

```bash
go test -count=1 ./test/testinfra -run '^TestCleanupAdmission'
```

The focused ownership and ordering controls also passed:

```bash
go test -count=1 ./test/testinfra \
  -run '^TestCleanupAdmission(CancellationOwnership|SupervisorFailurePaths|CIOrdering|CILintWiring)$'
```

The four affected existing runner wiring cases passed in 1.700s. Their exact selection is reproduced by:

```bash
pattern='CanonicalCommandAndRyukPolicy|TaskAndCIConverge'
pattern="$pattern|CachedImageDoesNotRequireRegistry|MissingOrRefreshImagePullsUnderLock"
go test -count=1 ./test/testinfra -run "^TestIntegrationRunner_($pattern)$"
```

These checks used host Task v3.51.1 and Go 1.26.4 on darwin/arm64. CI's pinned Task v3.53.1 has now been installed
in `/private/tmp/gh1064-task-tools` without replacing the host tool. Final verification will select that version.

The classifier was still evolving during these checks; its exact source snapshot was not frozen for these admission
results. Repeat the necessary final verification after classifier stabilization. These are bounded wiring evidence,
not final classifier conformance evidence.

Initial test-first admission cases failed because Task/runner/CI paths reached the expensive-command fence without
creating a real guard observation. The same selected cases passed after sequential guard wiring. The later full
matrix first exposed a nil synthetic test-package module in the classifier; its corrected version produced the pass
above. These failures are evidence of development, not outstanding test flakes.

The first ownership fixture returned an unresolved EPERM probe during group termination. It now keeps every
non-ESRCH result unresolved until either a later positive absence observation or the cleanup deadline. The corrected
fixture passed only after observing absence. EPERM is never accepted as evidence of successful cleanup.

The supervisor verifies and acknowledges its own process group, installs its cancellation reader before spawning,
and retains ownership until self-group cancellation. Parent cancellation uses a private pipe; the parent never sends
a killing signal to a remembered numeric PID/PGID. The parent joins the supervisor and separately observes group
absence under a fresh finite cleanup budget. Captured output is file-backed. This proof covers the controlled chain
that stays in that group, not arbitrary children creating their own sessions or process groups.

Independent admission review: scoped APPROVE. Classifier/census/baseline and full-gate readiness are excluded.

### Reviewed admission source hashes

```text
3c560cc4b68c88d2276777269b8e920ff708962e26ee3fe58c461561018a30f9  test/testinfra/cleanup_admission_test.go
2e060f07a88b903c70aafd0d35615f96eb75430bc3f17d661786f7db347ca5a3  test/testinfra/cleanup_process_unix_test.go
a83dc10f04084787ee73024d2ab6eb0322e659130281afe7d1ed58ef893c4e82  scripts/check-cleanup-roots.sh
9a4bcf19e1dff768cfa78ac8824ce645e095bfd504f554dac520d614223a58d9  scripts/run-integration-tests.sh
8e856b699c9da20151b61f3550febe5dbeb667112b943b3341ff4977de178a54  test/testinfra/integration_runner_contract_test.go
e87500c66d2fbe80732c2687b4ad651c90648a9cd29d68bad848c7b5e86ffd57  Taskfile.yml
88fe12882bba3fdf58b54e362d17ddb537795cecc4e2ee3d56a3ecafbe24d90b  taskfiles/lint.yml
613f0661ccfa3ca843e4ed6b1d658a1bc1152527b8670a63ab680e046810941c  taskfiles/test.yml
5576da78abf2c150ffc43b6e0b4374b0f4371875ed8cc27d959495d39fbb4fb8  .github/workflows/ci.yml
```

### Retained focused output

```text
ok  	github.com/c360studio/semstreams/test/testinfra	34.029s
```

## Historical intermediate evidence limits

The final source-accounted census, complete classifier semantic review, final-source property/mutation/race evidence,
cost measurements, full preflight, and whole-PR review remain incomplete. The exact 325-entry known-debt subset
has since received independent source and reconciliation review; the checkpoint below records its limited scope. A finite supplied context is not proof of synchronous Stop return, fresh authority, asserted errors, or
correct component-before-substrate ordering.

## Admission corrections and current proof

The original Cartesian proof measured 111.621s under race (112.18s wall). Independent design review accepted factoring
classification outcomes from uniform entry-point propagation. The resulting proof has 23 matrix cases; stale-case
preparation adds a separate analyzer call. No race-runtime setting, canned analyzer result, or result cache was added.

The factored normal run passed in 19.188s (19.43s wall). Subsequent descriptor, CI metadata and acknowledgement
corrections were included in the frozen race run below. They are also covered by the scoped independent review.

```bash
go test -race -c -o /private/tmp/gh1064-admission-final-race.test ./test/testinfra
PATH=/private/tmp/gh1064-task-tools:$PATH /usr/bin/time -l \
  /private/tmp/gh1064-admission-final-race.test \
  -test.v -test.run '^TestCleanupAdmission' -test.timeout=3m
```

Result: PASS, 50.41s wall, 804,995,072 bytes maximum resident set size on darwin/arm64, Go 1.26.4, Task v3.53.1.
All four canonical rejection categories exited 1. Missing acknowledgement executed its forced failure path and
was refused despite successful containment; that control passed in 0.13s. This is a measured local cost, not a
portable performance budget. Full repository guard cost and composed preflight overhead remain to be measured.

The frozen binary avoids reading an intermediate classifier edit while other role work continues. Its snapshot is:

```text
a4719e68e8c4e76b25b9bb1c0715876a74d79d203ea95bae230e528ca15a7e47  cleanup_admission_test.go
9b71703914deca20b151fa179c8997be617127dd1590c324add45bbdbe7fcea1  cleanup_process_unix_test.go
f91a5da7b491ae9816e8f6e52191e4e725a9c57a805c07f79678f1dff1e999f6  cleanup_analyzer_test.go
ecbbf2a1920a245047ca2b04d7f8c0befb1162ce869c6ffad3e6072e4afa0dd7  cleanup_guard_test.go
88b3788a04a98ceff6b2e844b9011392b24183ac74e05f294a75c25dd32d4860  frozen race test binary
```

Source filenames above are under `test/testinfra`. The root admission/process sources received scoped APPROVE at
these hashes. The classifier remains under development and is excluded from that approval.

The retained `evidence/` logs show actual assertion activation:

- `admission-race.txt`: all selected matrix cases, structural negatives, and process controls.
- `admission-mutations.txt`: omitted, late, and ignored guard mutations each reached the intended failing assertion;
  cp restoration preserved the original Task-file checksum, and the restored baseline passed. Mutation assessment
  accepted by the independent reviewer. No remaining runtime-deadlock diagnostic was observed.
- `private-descriptors-red.txt`: actual child exec inherited both private protocol descriptors before CLOEXEC repair.
- `ci-metadata-red.txt`: ignoring step/job execution metadata admitted each deliberately invalid workflow shape.
- `missing-ack-red.txt`: process containment succeeded while the previous parent incorrectly reported cleanup success.

A prior unfrozen race attempt hit an in-progress analyzer compile state and selected no runtime tests; it supplies no
behavior evidence. None of these focused results substitutes for the final classifier and whole-PR gates.

## Historical 325-entry baseline checkpoint

Independent source review inspected 325 physical cleanup sites across 104 files. It rejected 128 duplicate
caller-labelled records from an initial 453-record proposal; the reviewed set retains the actual lexical declarations.
A separate reconciliation review approved the exact corrected candidate set for baseline consumption, with all
fingerprints and source hashes unchanged. Only one ordinal changed after removing a false duplicate.

```text
3df0dea4ce389232d6b09803cbe3e8823ab32cfb170bf75ddb3747b0fd0bb5e8  review/known-debt-current-candidates.json
0143d3e986772a3131a58e959e9b22370cba04c137857559d0da70d696daa7bc  review/known-debt-source-review.json
6040e9babfc6bccbd666847eb6856565ac040f8db4dccf838581f2f923720d69  review/known-debt-reconciliation.json
1cc1811cb437d8a11270c2ed86589f03889fd8a64b7e9e9bcfc7c2875424790f  test/testinfra/cleanup_baseline.json
```

At that historical checkpoint, the baseline contained only those 325 exact entries, with reasons and #1064 references; no uncertain site has been
approved by generation. At scanner snapshot `7c5c3fa093ad2b4d0f8d29962f99ca957171ab7e1fdd12c7ebebd63a3fa45291`,
the guard reported zero new unbounded entries and zero stale approvals, while still refusing 61 cleanup-relevant
unknowns. That checkpoint is not final census or classifier approval. Subsequent source review found hidden
Background Stop cleanup in graph-index returned callbacks, and implementation review found additional callback
and captured-mutation cases. Their corrections and exact evidence review remain required.

## Dependency-load cost probe

A standalone read-only probe on the discovery checkout compared current all-dependency syntax loading with typed
initial-package loading plus a separate metadata-only dependency/module load. It did not execute repository tests
or change the classifier. Its compiled-file totals include synthetic test-package files and are not census counts.

Across default, integration and live_llm selections, both modes returned identical initial package/file sets,
Stop/Close/Context symbol-reference evidence, and all 77 module records, including their complete module metadata.
Local Go 1.26.4 measurements, one sample each:

| Mode | Three-selection wall time | Maximum resident set size |
|---|---:|---:|
| Existing full syntax mode | 5.21s | 2,613,460,992 bytes |
| Reduced syntax, first run needing export compilation | 18.87s | 1,226,342,400 bytes |
| Reduced syntax, warmed export cache | 4.47s | 1,193,787,392 bytes |

This motivates a bounded trial, not an accepted final performance claim. Final adoption requires identical actual
census evidence, strict load-error refusal, explicit dependency/module metadata, foreign binding fixtures, and final
guard/suite measurements. The first reduced sample includes export compilation and is not a fully cold-cache
benchmark. No global cache was cleared. Retained `load-*.jsonl`, `load-*.time.txt` and `load-probe.go.txt` reproduce
the measured inputs and result evidence. The probe source is non-Go-suffixed to avoid admitting artifact code as
repository test support.

## Scoped design corrections during classifier review

Independent review accepted exact ordinary-only ownership disposition and qualified method/type evidence at design
SHA-256 `519ca4d36ec068bd00a3d904e0250a6ad95b9e52832decec0ebc3cb8507c7684` and spec
`0538c8f95d9e1e5db34ead83ece912bedd71e836da57ce66a55f136c7829e1d3`. The disposition preserves uncertain
classification and cannot override automatic cleanup ownership, source/load failure, or another invocation path.
Its required counterexample adds a new cleanup caller to the same physical helper after installing the disposition.

The reviewer subsequently accepted the exact typed `service.(*Manager).StopAll(context.Context) error` terminal
boundary at design `34922df16f30e4026a19582692bd8719d7230665ae9b13ab5cd467783fafc2b3` and spec
`2ef36756af10c07ec4f2591be61a6f8c82914099fa49be3aaa91eeba13e051a6`. Source review additionally identified
Background cleanup callers at `service/framework_owned_bucket_guards_integration_test.go:264` and `:334`.
Approval of the boundary does not approve any newly exposed baseline entries.

The architect confirmed that a depth/cycle cutoff on arbitrary production recursion is not positive cleanup-candidate
evidence. Declaration census and traversal of established cleanup/context/callback paths must stay distinct. Explicit
cleanup edges and candidate-bearing unresolved callbacks remain blocking. This corrects implementation scope within
the existing design rather than granting an exception for unresolved cleanup.

## Final classifier source review

Independent source review approved the complete callback correction family, bounded ownership handling, baseline
freshness, loader split and lint extractions at these hashes:

```text
9a8754b04e4391951719bc75b417e963e7f2dca13e31e484c442f141251320bf  test/testinfra/cleanup_analyzer_test.go
0971bbed668a1ac9bae10af537811145f6d0f7147263b526bcfbf8be49de9ad8  test/testinfra/cleanup_guard_test.go
a5b7ea6f86d2b8a35b104063c1fdffba440b511cdb81f2667e591579eabcefe1  test/testinfra/cleanup_baseline.json
```

`review/classifier-conformance.md` pins the implemented obligations and retained development evidence. The final
installed guard passed in 7.481s; `review/final-reconciled-census.json` retains the complete result. It accounts for
2,337 sources (2,334 typed), 1,402 variants and 973 reported exclusions. Selected packages are 515/522/515 and
selected files 2,079/2,330/2,083 for default/integration/live_llm. Classes: 334 unbounded, 77 finite-context,
349 non-lifecycle, one deliberate contract and 641 ordinary-only unknowns. Uncertainty is retained, not certified safe.

Final owned fixtures passed in 14.761s. The six corrected callback controls passed under race in 2.757s. The final
pinned revive check was clean. Three valid final classifier mutants each failed the intended assertion: suppressed
unbounded refusal, suppressed unknown refusal, and ignored approval fingerprint. A cp backup restored the exact
original checksum, then the combined selection passed. The retained `final-v2-mutation-*.txt` files distinguish
behavioral failures from a discarded compile-invalid attempt. Admission omission/order/propagation mutations are
retained separately. Whole-PR race and integration evidence must still come from the current final gate.

## Final cold and warm cost

An immutable binary from the reviewed runtime source was built with `go test -c`. The first measurement used a new,
private empty GOCACHE; the second reused only that cache. The shared build cache was not cleared. Both runs used
`-test.run '^TestCleanupRootGuard$' -test.v -test.timeout=5m` and passed the installed 334-entry, 86-resolution manifest.
A subsequent fixture edit corrects only the citation comment syntax; full preflight uses that corrected source.

| Go build/export cache | Wall time | Reported maximum RSS |
|---|---:|---:|
| Private empty cache | 29.80s | 1,603,387,392 bytes |
| Same cache, warm | 7.34s | 1,662,631,936 bytes |

Toolchain: Go 1.26.4, darwin/arm64. Measurement: `/usr/bin/time -l`, one sample per state, existing downloaded modules.
The binary SHA256 is `6ebfc02b4f4508292493b34851e2ce75383d5551dd8616be9e898b2613890ed3`.
Raw output and resource reports are retained as `evidence/guard-{cold,warm}*.txt`. These are local observations,
not portable budgets or cold-network dependency-fetch measurements.

The composed check:push path reaches the guard four times: lint admission, the unit race package, integration
runner admission, and the additive integration race package. Classifier and entry-point proofs also run in both
race suites. Preserve fresh admission and all existing coverage when #1293 considers deduplication; this change
introduces no skip flag. Final package timing is recorded with preflight evidence.

## Full local preflight

The current implementation, including the citation-only fixture correction, passed:

```bash
PATH=/private/tmp/gh1064-task-tools:$PATH /usr/bin/time -l task check:push
```

Exit 0; 851.30s wall (14m11s), Go 1.26.4, Task 3.53.1, darwin/arm64. This executed build, pinned lint,
integration/live_llm vet, schema generation/drift, contract checks, unit race, then the canonical uncached additive
integration race suite with its host lock, Docker preflight, Ryuk, failfast and two-package concurrency preserved.
Some unchanged unit packages used Go's valid test cache; integration used `-count=1`. No failures were rerun to green.

The testinfra package passed at 133.504s in unit race and 130.688s in integration race. Standalone admission calls
passed at 7.569s and 7.719s. The two package durations sum to 264.192s; this measures the entire existing package,
not exclusively newly introduced overhead. Their duplication is recorded for #1293. The final package scope
includes all classifier, source-accounting, manifest, actual-entry-point, process ownership and existing runner tests.

Retained logs: `evidence/check-push.txt` and `evidence/check-push-stderr.txt`. Staged citation verification resolved
445/445 references after correcting the annotation syntax; its checker fixture matrix had passed 34 checks.
Strict OpenSpec validation passed 58 items. Standalone product E2E was not required by this test-infrastructure-only
diff; no production lifecycle behavior or public API changed. Archive/spec-sync validation and hosted CI remain
separate from these local results.

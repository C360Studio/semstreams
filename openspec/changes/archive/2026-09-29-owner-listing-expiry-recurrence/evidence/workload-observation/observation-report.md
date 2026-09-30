# Actual owner-load workload observation

This is the single accepted actual-workload observer slice for #1421 / draft PR #1435. The private observer arms only the five initial CI predicate-forward calls in the existing harness; the production KVStore, SDK, runner, workload shape, deadlines, assertions, and unarmed paths were not changed. A healthy run establishes observer wiring, not the cause or resolution of the intermittent deadline.

## Source and review identity

- Worktree branch: `codex/gh1421-listing-expiry-recurrence`; implementation based on HEAD `5a1e5531bf47b36854125510efba30ca30fa5a09` (docs-only commits may advance HEAD).
- Accepted design: `workload-observation-design.md` SHA-256 `77a7dbc61d54a536ab5056f0ee9f0a01a03957746dde37c23f47a4db09cde81e`.
- Independent corrected-source approval: observer SHA-256 `c0edd1a466c447fe10b4eff1d34cb396e10b736ce35df055182e0bdd05090dcd`; harness SHA-256 `8bed916461903bd0e2d769cc51159a217a4c0d0bf89159ed6bcdf95445818834`.
- Pre-review backup observer `f5a64c07d7fe5991b132615c97d7479c47db608172a12bcea9209bad407e6c65`, harness `5dc159dc991414b2145b39136ebec166a1ae6370668c2084331cb0d55067a658`. All four sources and their bytewise diffs are retained here.
- No exported surface or production behavior changed. The observer embeds the native KeyLister and does not override `Keys`; it delegates `Stop` synchronously once, recording but not altering its error. Its deadline-only callback captures bounded stacks and joins under one lexical terminal allowance; unresolved ownership blocks further attempt admission. Reporter publication is lexical, including `FailNow`/`runtime.Goexit`.

## Focused proof chronology

The raw logs, including failed commands, are byte-preserved in `raw-logs.zip`; `sha256-manifest.txt` gives archive/member checksums.

1. Initial tagged runner proof failed to compile because `assert.CollectT` has no exported `Failed` method (`gh1421-workload-red.log`). This is a compile failure, not TDD RED. Source snapshot: `compile-red-observer.go.txt`.
2. With the proof corrected and lexical publication omitted, the same narrow tagged runner proof failed at `lexical report absent after FailNow` (`gh1421-workload-red-behavior.log`). This is the intended behavioral RED. Source snapshot: `behavioral-red-observer.go.txt`. The integration-tagged package TestMain started and cleaned real NATS containers even for this fake-boundary test.
3. Initial implementation's focused tagged race proof passed (`gh1421-workload-focused.log`) but independent review found four material issues: early-exit callback ownership, snapshot/return marker coherence, after-Stop phase labeling, and unnecessary tagged coupling. That passing pre-review source was not admitted for native execution.
4. An ordinary untagged compile probe selected no tests (`gh1421-workload-untagged.log`); a second ordinary package test did run (`gh1421-workload-untagged-actual.log`). Neither substitutes for the observer proof. After the four fixes, the first ordinary observer command failed compilation because two `sync.Once` fields were omitted from the struct (`gh1421-workload-fixed-unit.log`). A sandbox-denied attempt before that retry did not execute a Go test. Both failures were corrected before the approved source snapshot.
5. Corrected ordinary unit/race observer suite passed without integration TestMain (`gh1421-workload-fixed-unit-v2.log`): transparent delegation, constructor/expiry boundaries, real `FailNow` with held active callback and independent release/join, ordering, unresolved join/admission, after-Stop state, and coherent capture boundary.
6. Exact 0–4 activation proof passed with `scripts/run-integration-tests.sh -race -timeout=180s -run '^TestOwnerLoadObserverExactActivation$' -v ./processor/graph-index` (`gh1421-workload-fixed-activation.log`). Because the proof is tagged, package TestMain started real NATS and Ryuk containers and logged teardown.
7. Selected current-source publication mutation removed only lexical record publication. Backup/current SHA-256 was `c0edd1a466c447fe10b4eff1d34cb396e10b736ce35df055182e0bdd05090dcd`, mutant SHA-256 `acc210d0006641862b42f5a47c3725aec5c09270072246eba857ef809c6cfdc2`. `go test -race -count=1 -run '^TestOwnerLoadObserverFailNowRetainsPriorAndFailed$' -v ./processor/graph-index` failed on the intended `lexical report absent after FailNow` assertion (`gh1421-workload-publication-mutant.log`). A `cp` restoration from `/tmp/gh1421-workload-publication-mutation.go.bak` matched the original SHA, and the same selected proof passed (`gh1421-workload-publication-restored.log`). `publication-mutation.diff` records the one-line fault. The held callback fixture released and joined on both paths.

## Single native run

- Command: `env -u GRAPH_INDEX_OWNER_FILTER_FULL scripts/run-integration-tests.sh -timeout=180s -run '^TestIntegration_OwnerFilterLoadHarness$' -v ./processor/graph-index`.
- `start=2026-09-30T01:48:19Z`, `finish=2026-09-30T01:48:24Z`, exit `0` (`native-times.txt`); raw log `gh1421-workload-native.log`, SHA-256 `2e3f95f96eca64e3cf414b4a6db9c711f89fe7da20158e096af9c4a72f7d0d7c`.
- Go `go1.26.4 darwin/arm64`, while CI uses `go1.26.8`; NATS Go SDK `github.com/nats-io/nats.go v1.52.0`; TestMain shared fixture `nats:2.14-alpine`, repo digest `nats@sha256:4063edae0717ba5f7501bfde75f97fd9b57f5b93597b92c70b6a6fbbf6a74e06`; measured owner-load fixture `nats:2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66` (native log lines 33, 35–39); host `Mac.localdomain`, Darwin `25.5.0 arm64`.
- All five actual CI predicate-forward attempts returned `5000` non-nil keys and nil operation errors, with `deadline_error=false`, `canceled_error=false`, and `callback_joined=true`. Native construction durations were 1.056–1.205 ms, collection-to-Stop intervals 57.833–59.957 ms, and entry-to-return intervals 59.060–61.100 ms. The five records are lines 49–53 of the exact raw log.
- Native delegated `Stop` returned `nats: invalid subscription` on each record. The unchanged production call returned success and the harness passed. This observation does not establish a cause for the historical deadline and does not prove native internal watcher completion.
- Resource phase showed subscriptions `80` before and `80` after, slow consumers `0`; package TestMain logged both owned test fixture containers stopped and terminated. At 01:48:34Z the host lock was absent, `docker ps` was empty, and no runner/test/NATS process remained. No native repeat, full suite, CI, or production change was performed.

## Execution allowance

Accepted maximum: 300 seconds of cumulative local command execution for this slice. Tool-reported durations of the earlier seven focused/probe commands total about 20.8 seconds; corrected unit, tagged activation, mutant, and restored commands were 3.30 + 5.62 + 2.30 + 2.34 = 13.56 seconds. Before native admission we conservatively debited 60 seconds for all focused/compile/probe commands and 6 seconds for host ownership checks, leaving 234 seconds; this exceeds the required 210-second native reserve. The native command's UTC start-to-finish interval was 5 seconds; the post-run concrete ownership inspection completed in about 3 seconds. Conservative final debit is 74/300 seconds, leaving at least 226 seconds unused. Editing and independent review idle time are excluded by the accepted design. The native command was admitted once and not repeated.

## Post-native proof-only correction

After this one native run, independent review found that the transparent-success test compared the forwarded child deadline with `time.Now()+5s` after the call, allowing a scheduler pause to cause a false failure. The test now brackets `KeysByFilter` with timestamps, requires the forwarded deadline to fall within the corresponding five-second bounds, and checks equality with the observer's recorded child deadline. The production path and harness were unchanged. Corrected observer SHA-256 is `f2eb3db07f11cd3d7cbe7182fca5de144997a448357b2e8354271bdd7aacef46`; the narrow ordinary race proof passed (`post-native-deadline-proof-focused.log.txt`). The native log and source snapshot above retain their original identities; no native rerun occurred.

## Limit

This healthy sample confirms transparent observation of the actual workload, including the final `Stop` result. It did not reproduce the intermittent deadline. The origin, presence, and timing of any earlier failing run's partial collection remain unknown; no repair or issue-closure claim follows from this run.

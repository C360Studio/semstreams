# #1222 Task reporting and CLI correction checkpoint

Worktree: `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`.
Owner of this slice: CLI and narrow Task reporting. Other agents' edits are preserved.

## CLI correction checkpoint (C1-C4)

Four independently reviewed findings from `/private/tmp/semstreams-1222-cli-review.md` were corrected:

- C1: resolver now maps minted-authority alias and both preidentity Task spellings to their constructed Scenario names. Identity admission remains exact.
- C2: optional typed analysis serialization now follows the terminal aggregate Writer projection, including legacy final-validation and teardown failures.
- C3: tiered GraphQL uses the selected base URL; default semantic selection uses the intended port 38180 while explicit caller endpoints remain intact.
- C4: runner logs explicit required/unattested disposition before Setup.

Each has a compiled behavioral RED and focused GREEN in `/private/tmp/semstreams-1222-cli-c{1,2,3,4}-{red,green}.log`. The first attempted C1 RED was an inaccessible default Go cache and was replaced by an executed test using `GOCACHE=/private/tmp/semstreams-1222-go-build`; it is not counted as behavior evidence. C1-C4 GREEN exit 0. `gofmt -d` over six files and `git diff --check -- cmd/e2e` returned no differences/errors. Full `go test ./cmd/e2e -count=1` initially ran and found only a synthetic fixture manifest filename collision with an analysis glob; corrected to `provenance-manifest.json`. Immediate rerun was blocked at compilation by concurrent in-progress Writer API additions (`ChildExpectation`/`validateChildExpectations` undefined), so full-package GREEN after this last test-fixture fix is pending a stable dependency snapshot.

Frozen six-file SHA-256:

```text
d50e6283d2947e52831ddb401d7a8eb4106eec8dc2ac8417b20d1722ba2b0f52  cmd/e2e/main.go
969e11b27e306ae27cd58bb0094ef74adf82850ab6d5a9e3de9d2519089a5aeb  cmd/e2e/main_test.go
73f936d770c8fc496d95b3de7d2f50877bce635bc14aaf6bbd3b3a04001114b4  cmd/e2e/runner.go
b980cb684a573b29d67135482112097c285250a5c28bf3607020140ff217c829  cmd/e2e/runner_test.go
9c4c5f0d2ceb5017a1f89e24e938f5a0e63b74b33dfc0b8864359c788cfdffa7  cmd/e2e/selection.go
6f6943aa1d66d4ae22aaac36e1cfafd7c27bf724ea8567e477f64d17475f5917  cmd/e2e/selection_test.go
```

## Pending report slice

`cmd/e2e/report.go` and `report_test.go` contain a private fixed Task scope resolver with direct core shell declaration IDs, CLI child expectations via the existing CLI resolver, one slow-consumer child, and a five-family composite with explicit exclusions. Its four focused tests passed before Writer API compilation became transiently unavailable. These two files are not part of the six-file CLI correction freeze.

Root reconciled architect advice: persisted `Config.ChildExpectations` and `RequireTaskStatuses`, paired `ParentID`/`ParentMemberID`, immutable Writer child/member methods, command+cleanup statuses. The Result/Writer owner is implementing those methods; reporter code will consume them, never add a second outcome or filesystem owner. Task wrappers and workflow remain untouched until report mode compiles and is testable. Core minted-authority required child adoption awaits held `platform_identity.go` ownership resolution; preidentity seed is setup and no-record must come from an actual observed comparison, not a legacy child exit alone.

No Docker, GitHub, full gate, commit or push was run in this slice.

## Resumed checkpoint, frozen on 2026-09-27

The preceding pending statements describe the earlier pause and are superseded here. Root released the six-file CLI freeze after C1–C4 re-review approval. The shared Writer API is now present; no Writer files were edited by this slice.

CLI C5 now writes exact selected `Result` declarations (run/member/check requirements) in the initial aggregate before Setup and replaces those slots with Execute results at terminal write. The actual selected-run test read initial JSON in Setup. Behavioral RED: `/private/tmp/semstreams-1222-cli-c5-red.log` (empty initial Scenarios). GREEN: `/private/tmp/semstreams-1222-cli-c5-green.log`.

CLI C6 preserves an explicitly unsuccessful Execute-returned Result even when its named checks pass and there is no Go error. Behavioral RED: `/private/tmp/semstreams-1222-cli-execution-false-red.log` (exit 0); focused GREEN: `/private/tmp/semstreams-1222-report-provenance-green.log`. The bridge treats an empty returned check catalog as legacy/unattested; required selection refuses it. The actual tiered fallback resolver/constructor/catalog bridge test passes. No behavioral RED was retained for that separate fallback correction.

The private CLI/Task report seam now persists initialized Task declarations, immutable shell/child member observations, exact child verification, typed manifest preparation, closed CLI child logs, and terminal Task statuses. Producer source SHA/dirty/patch/untracked-input digests, runner bytes/build info and effective nonsecret settings are observed locally. Typed Task input supplies selected constituent paths and app phase facts; file and app binary bytes are rehashed. A selected CLI child fixture verified its parent slot plus retained log/manifest exact SHA-256 bytes in `/private/tmp/semstreams-1222-cli-child-provenance.log`. Malformed Task finalization input now still attempts a failed terminal aggregate.

Already-edited public default Task bodies: `core`, `structural`, `statistical`, `semantic`, `agentic`, `slow-consumer`. They retain their process commands and use the narrow `scripts/e2e-required-report.sh` serializer; each initialized body has one cleanup → closed Task log → report finalization path. Core shell comparisons name readiness, heartbeat, shutdown, early cancellation and preidentity refusal. The preidentity no-record member remains missing because its current legacy CLI exit cannot prove a named observation. Core report-init also refuses until held #1404 minted/graph scenario adoption is reconciled. No Docker execution or assembled Task behavior was claimed. `e2e:tiers` and `e2e:all` were not changed; the five-family composite and parent provenance merge are unimplemented because the owner requested a pause before those additions.

Final bounded validation: `GOCACHE=/private/tmp/semstreams-1222-go-build go test ./cmd/e2e ./test/e2e/results -count=1` exit 0 in `/private/tmp/semstreams-1222-reporting-final-go-test.log` (`cmd/e2e` 3.317s; results 2.235s). `sh -n` on the helper and each of six extracted Task shell bodies, `task --list`, `gofmt -d` over changed Go files and `git diff --check` all exit 0. No Docker, full gate, commit, push or GitHub mutation.

Auto-review rejected the attempted `.github/workflows/e2e-ladder.yml` `upload-artifact@v4` edit for the entire `test/e2e/results/` directory. Reason: the export may contain sensitive logs, manifests, metadata or binaries without explicit user authorization for that specific export; it expressly said not to bypass via workaround. No workflow change landed. CI artifact retention therefore remains blocked for owner handling.

Current source SHA-256 snapshot:

```text
d9e61f4f9a271dccb1c69b271ab1cd7f91207da2ff28f5ca5809e819ac62a8f5  cmd/e2e/main.go
282db7bb6f5471c0dde379128b95c7936a82af0dad4446dcb204d148e3c8f04d  cmd/e2e/main_test.go
9790a9a27052592a94854ac4e0d1b793b29af52bac930129034c9258e5762c5e  cmd/e2e/runner.go
6d113caeb35bb1e769eaed1fa44ea8412984dcda6808575e9373d1edde5a769c  cmd/e2e/runner_test.go
9c4c5f0d2ceb5017a1f89e24e938f5a0e63b74b33dfc0b8864359c788cfdffa7  cmd/e2e/selection.go
6f6943aa1d66d4ae22aaac36e1cfafd7c27bf724ea8567e477f64d17475f5917  cmd/e2e/selection_test.go
1b503d673b8eec83ee6981530a62ee628840ee975de423cb775db26dbd2fc9f1  cmd/e2e/report.go
fb351b3f6d11ae3f3e8f3ab4a0d82ce76fe57de408425e62adee16da1d0284c2  cmd/e2e/report_test.go
f1913eecbe936f90ea888ad0f41496b05b11ef849e446255e98bdb69e1ee93eb  scripts/e2e-required-report.sh
c5df6bb42daa3539a6ec48e73634f68df7f31da415026064d08e5e04580b315e  taskfiles/e2e/core.yml
8e9f98d1210928904769c77f59acd3037ac00b5f91c60736f882a822b9f28c20  taskfiles/e2e/structural.yml
d4b00117968804ae53cd525728a124e4443cca694feaf612e94ba912d1a40b9c  taskfiles/e2e/statistical.yml
0fa9a797bf8ad3da1dc2821764d25489b35a339ad4129a0144dcf8ae36f92aec  taskfiles/e2e/semantic.yml
987d32c03827777c4595dec4741a6887aab6fb9814dfcc42ac5413c334ec6835  taskfiles/e2e/agentic.yml
3f4c7eb2aef2ffaee3ed774948eca16d80db6ea1e5ac0d9c426356a175912c40  taskfiles/e2e/slow-consumer.yml
```

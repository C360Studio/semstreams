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

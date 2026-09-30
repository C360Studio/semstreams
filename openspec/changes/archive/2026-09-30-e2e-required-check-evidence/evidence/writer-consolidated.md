# #1222 consolidated Writer/report API checkpoint

Worktree HEAD `fe6e2cc03e16f5db47e293f55939548572f204cc`.
This snapshot extends the bounded R1/R2 correction approved in
`/private/tmp/semstreams-1222-result-review-round2.md`; it is not whole-issue
acceptance. The accepted `implementation-handoff.md` and manifest shape advice
carry the design rationale. Current direct consumer is `cmd/e2e` report mode.

Writer now retains paired parent/slot identity, command/cleanup exit facts,
initialized child expectations, and exact task-status applicability.
`WriteMember`/`LoadMember` bind immutable member files to initialized
declarations. `WriteManifest` retains one exact JSON object, no-clobber, and
returns an `ArtifactReference`. `VerifyChild` returns the same reference shape
after checking exact parent/slot/selection/member proof, child JSON bytes, and
referenced log/manifest bytes against digests. `Compare` exposes both evidence
statuses. Dotted member and selection IDs are accepted.

| File | SHA-256 |
| --- | --- |
| test/e2e/results/writer.go | 42f603b03daf46184ae80809b994771a073bedb5a315ed05251b48ecb732ab20 |
| test/e2e/results/writer_task.go | b70e3763a4f1f911b192fc6c300740dfc0ce53030f8fea4d68eaae3100a8c5bd |
| test/e2e/results/writer_test.go | 60262a27fe72f3affe1b60919b59963f5395df4c7aa30dfdf71c8a8f02195fbf |
| test/e2e/results/writer_task_test.go | cd313b10443862ebf2079a507b42583fa31a62f3336c02c96b61e11bb5c7681c |

Behavioral RED for live child retained-byte verification: all missing/tampered
log/manifest cases failed the intended assertions before implementation,
`/private/tmp/semstreams-1222-child-retained-red.log` SHA-256
`73c9a90d62b79ca915289ee0fb31625ae6706c8788fd337167e79ef04fd67b2b`.
The same focused tests passed afterward, log
`/private/tmp/semstreams-1222-child-retained-green.log` SHA-256
`b521a68f178a14eff409cf8c565b4d39985f28467bd9a655cd0d3a0bbc62d7c4`.
The new manifest API's first RED was missing-symbol compilation only, log
`/private/tmp/semstreams-1222-manifest-api-red.log` SHA-256
`79c2ef731e29adda6b7bf4a7df1a9e571784023d37451a6999fcb577f68f05af`.

Full `go test ./test/e2e/results -count=1 -v` passed, log
`/private/tmp/semstreams-1222-manifest-api-green.log` SHA-256
`0c4674b09aceb880a494beca30423efb39b335ae7b2c2c9035758fa6386f77dd`.
Current `go test ./cmd/e2e ./test/e2e/results -count=1` and the same with
`-race` both passed, logs `/private/tmp/semstreams-1222-writer-consumer.log`
SHA-256 `b52a6458678e727765aeda8aaeec0984fb4e1ddf44e9a5a67f64aa38c6c76793`
and `/private/tmp/semstreams-1222-writer-consumer-race.log` SHA-256
`b0c9ee449849d0088cfe075ff9ec561cb56c8d486fbaa669ac472663dac8895c`.
`git diff --check` on the four Writer files exit 0. The package tests include
native fuzz seed replay; no exploratory fuzz run is claimed.

Auto-review rejected two temporary production-source mutation attempts before
either edit occurred: replacing no-clobber `os.Link` with `os.Rename` could
overwrite retained evidence, and disabling the retained digest comparison
would temporarily weaken the integrity gate. Neither mutant ran. The source
and its pre-attempt backup both have SHA-256
`b70e3763a4f1f911b192fc6c300740dfc0ce53030f8fea4d68eaae3100a8c5bd`.
Duplicate-attempt and missing/tampered-byte behavior tests pass; mutation
sensitivity for these two guards remains **UNVERIFIED**. No bypass was attempted.

The exact first requested patch was `return os.Link(tmp.Name(), path)` →
`return os.Rename(tmp.Name(), path)`. Auto-review's stated reason was:
“Replacing no-clobber os.Link with os.Rename can overwrite existing
manifest/member artifacts and irreversibly destroy retained evidence, exceeding
the authorized bounded persistence behavior.” The exact second requested
patch was `if fmt.Sprintf("%x", sum) != digest` →
`if false && fmt.Sprintf("%x", sum) != digest`. Auto-review's stated reason
was: “The patch disables retained-artifact digest verification, allowing
tampered evidence to be accepted and persistently weakening the integrity gate
without explicit authorization.” Each rejection also instructed against a
workaround or indirect execution. Both patch calls were rejected before edit.

A possible *separately approved* experiment would put a source-only mutant copy
under an isolated temporary directory and use a Go overlay plus an isolated
build cache to run only named tests against temporary fixture directories;
the shared source and retained artifacts would remain untouched. This is a
description for owner/reviewer assessment, not authorization or an executed
mutation, and it must not be used to route around either rejection.

Open: independent review of this consolidated API and actual reporter/Task
producer semantics, retained constituent and app-phase content proof, assembled
E2E, release gates. No Docker, integration, commit or push was run here.

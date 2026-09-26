# Reconciliation: implementation vs design (`one-composition-root`, 2026-09-26)

The developer (Opus, `implemented-by: opus`) landed tasks 1–6 and 7.1 in seven commits, `f3bf341d` … `e1f1bd8b`
(13 ahead of `main` `8d53c084`, tree clean), and reported twelve points where the design was silent or wrong, plus
two departures from the brief. Each row records the coordinating session's disposition; the reviewer checks the
implementation against `design.md` **as amended by this file**. Nothing here re-opens a ruling.

## Coordinator's own re-measurement at `e1f1bd8b`

`task lint` 0 · `go vet ./...` 0 (no `-tags=`) · `go run ./cmd/entity-id-audit .` 0 · `go test ./test/contract/...`
ok · boot packages (`internal/boot`, `internal/e2eboot`, `internal/bootstrapobservability`, `internal/maxdelivery`,
`internal/agentprofiles`, `cmd/...`) 11 ok / 0 fail · `docker/Dockerfile`: `grep -c tags=` 0, `grep -c ^FROM` 3 ·
`git grep 'go:build e2e_' -- '*.go'` 0 (the 13 remaining hits are the inventory's own pins and this change's docs) ·
`go list -deps ./cmd/semstreams` ∩ {harness, e2eboot, e2eslowconsumer, examples, cmd/e2e-semstreams} = 0 ·
`go list -deps ./cmd/e2e-semstreams` ⊇ {fixtures, mission, e2eboot, e2eslowconsumer, harness/{lessoncuration,
milestoneprobe, processbarrier}} · CI at `e1f1bd8b`: Build, Lint, Schema pass; Test, Tier 1, ladder pending at the
time of writing.

## Departures from the brief — both correct

| Departure | Disposition |
|---|---|
| Sections 1–3 were pushed together with 4 (`963d8de7`), not one at a time | **Correct.** The ladder runs on `pull_request`; before 4.2 set the option variables, its slow-consumer and statistical jobs could not pass. "Push only green states" outranks "push per section". |
| Commit trailer `Co-Authored-By: Claude Opus 5.5 (1M context)` instead of the Fable line | **Correct.** The trailer names the model that did the work, which is what `implemented-by` is for. The coordinating session's own commits carry the Fable trailer. |

## The twelve points

| # | Developer finding | Disposition | Design text affected |
|---|---|---|---|
| 1 | `PostStart []func(context.Context) error` is unimplementable: `seedMission` needs the lifecycle manager and the platform, which exist only inside `Run` | **Accepted.** `PostStart []func(context.Context, *lifecycle.Manager, types.PlatformMeta) error`. | § 2.1 |
| 2 | `RegistryFor(opts, full bool)` cannot select capabilities without the config | **Accepted.** `RegistryFor(opts, cfg, full)`; `cfg` may be nil when `full`. The full-vs-selected policy is unchanged (D9). | § 2, D9 |
| 3 | `Options` embeds `CLI`, so `Validate`, `ShowVersion`, `ShowHelp` are fields too | **Accepted.** I1's reflection covers them. | § 2.1 |
| 4 | `NATSURLs` has nothing that can set it — no flag, env var or main writes it; only `run.go:135` reads it | **Drop the field** (fix pass). A scalar nothing sets is the advertised-absent class the design deletes elsewhere (D14). `createNATSClient` keeps its own `SEMSTREAMS_NATS_URLS` → config → default resolution. | § 2.1, P7's "NATSURLs" analogy stays as semdev's, not ours |
| 5 | `ConfigPatches` placement: § 2.1's comment says after `cfg.Validate`, § 2.3's rule says where it already ran (production: before `Validate`, `main.go:139` vs `:143`) | **Accepted — before `Validate`.** The patched configuration is what boots, so it is what validates. § 2.1's comment was wrong; § 2.3's rule governs. | § 2.1 |
| 6 | With one parser, `FromEnv` starts from `Production()` and keeps the parsed `HealthPort` (default 0) instead of forcing 0 | **Accepted.** D7 reads: both mains pass the parsed flag, whose default is 0; I1 holds by construction. | D7 |
| 7 | `NewE2EPhaseA` deletion and the e2e half of `boot_order_test.go` landed in section 3 (cannot compile before the e2e main is thin); the `e2eslowconsumer` tag removal landed in section 2 (`e2eboot` imports it untagged) | **Accepted.** Compile order; the task numbering was a checklist, not a build order. | tasks 1.4, 3.2 |
| 8 | I6 in `test/contract` cannot import `e2eboot`: doing so links the example and mission vocabulary `init()`s (OQ2's residual), which trip `TestFrameworkPredicateDataTypesAreCanonicalAndRatcheted` with 8 `mission.*` predicates (measured). So `TestE2EBootVariableSetMatchesTierTable` parses the `options` table in `internal/e2eboot/fromenv.go` from source; every option name is a string literal; a new test pins the literal to `milestoneprobe.EnvVar` | **Accepted, reviewer to weigh fidelity.** The alternative — I6 as an `e2eboot` package test reading the spec table — would duplicate `tierTable`'s parser. The measured collision is new evidence for OQ2 (b) as a follow-up: the `init()`s now cost a contract-test import, not only tier fidelity. | § 6 I6, § 11.3 |
| 9 | Two behaviour changes "production copy wins" causes that the design did not name: the e2e binary's `--version` prints `semstreams version …` (`test/release/release_smoke_test.go` updated); `bin/e2e-semstreams validate configs/hello-world.json` now needs `SEMSTREAMS_E2E_EXAMPLES=1` because the verbs list only the components the enabled options register (`docs/basics/05-first-processor.md:50-52` updated) | **Accepted as consequences of D2 + D9.** The quickstart now teaches the option variable; the production binary never validated an example composition either. Named for the owner in the landing report; not a question. | § 7 rows 3, 12 |
| 10 | Removing the tag exposed a revive `if-return` finding in `internal/e2eslowconsumer/probe.go`; fixed in section 3 | **Accepted.** Exactly what D10 predicted a tagged tree hides. | D10 |
| 11 | I7's test is behavioural (in-process NATS server; the extension sees the connected client and its error fails the connection step) instead of a source-order pin; `boot_order_test.go` still pins `connectNATSWithSpinner` before `StartValidatedConfigManager` | **Accepted — stronger than designed.** | § 6 I7 |
| 12 | Four stale pins outside the 6.1 sweep, stale before this change: `docs/advanced/12-coordinator-pattern.md:85,156`, `configs/rules/lessons/README.md:26`, `docs/operations/09-http-middleware.md:14`, `processor/agentic-tools/README.md:118` | **Left alone.** Pre-existing; not this change's residue. Recorded here so the next docs pass sees them. | — |

## Fix pass after review

Row 4 (drop `NATSURLs`) plus whatever the implementation review finds, in one commit, before the OQ1 e2e evidence is
taken at the final code revision. The archive + spec sync follows as the last content commit.

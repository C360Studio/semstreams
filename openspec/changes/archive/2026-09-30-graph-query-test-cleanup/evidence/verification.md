# Graph-query cleanup verification

Reviewed source: `80565d7b0cab337a8e750cc330aee4a4d4940d8d`.
[Execution metadata](execution-metadata.json) records all nine source/baseline hashes and native run commands.

## Focused results

| Recorded check | Result | Package / wall |
|---|---|---|
| [Owner race proofs](focused-owner-race-baseline.log.txt) | PASS | 1.623s / 3.70s |
| [Restored ordinary cases and owner proofs](focused-restored-race.log.txt) | PASS | 1.669s / 2.63s |
| [Corrected native lifecycle and late-summary checks](focused-native-review-fix.log.txt) | PASS | 2.643s / 6.11s |
| [Cleanup guard](cleanup-guard.log.txt) | PASS | 8.052s / 8.25s |

The race logs cover setup exit, caller exit, concrete cleanup/fence errors, independent terminal authority, terminal
expiry and no implicit retry. Restored checks include attack/component cases, duplicate Start and short-lived Start.
The corrected native cases took 0.66s and 0.48s and observe callback, captured runtime and exact-view completion.
No skipped case is credited as execution; the standalone child dispatcher is not an additional scenario proof.

The native correction installs release/task ownership before admission and prevents finalizer access until Stop
returns. Its seven-second failure observation does not promise bounded contextless joining. The corrected run above
is separate from the earlier 8.414s and 2.675s package runs; those earlier passes do not validate the correction.

## RED and mutation evidence

[Original B38 RED](prechange-behavioral-red.log.txt) records exit 1 and the missing terminal-before-substrate witness
at described source state `3c0eb2d6`, before owner conversion. That historical instrumented source was described,
not preserved as an exact snapshot. This limited original RED is distinct from later mutation sensitivity.

All five compiling mutations exited 1 at the intended oracle. [Mutation results](mutation-results.json) retain exact
commands, checksums and timings; paired baseline/mutant source snapshots and logs sit beside that record.

| Mutation | Intended observed failure | Wall |
|---|---|---|
| Late registration | Missing terminal-before-substrate witness | 2.515s |
| Early Start cancellation | Native Close observes ended accepted Start authority | 2.423s |
| Discard concrete error | Parent rejects unexpected child success | 3.365s |
| Ignore terminal expiry | Expected deadline error is absent | 2.377s |
| Implicit retry | Two concrete attempts instead of one | 2.465s |

Independent review checked sensitivity and source restoration. The restored ordinary/race run passed.

## Guard and remaining validation

Exactly 39 legacy identities were removed; final totals are **234 legacy entries / 97 resolutions**.
Review approved only deferred `o.cancelStart`, fingerprint `07b402a8…`, with its five owner declarations as
dependencies. Existing resolutions remain unchanged. See [implementation review](../review/implementation-review.md).

## Canonical gate record

`task check:push` at the reviewed revision **FAILED** after 513.883s: exit 201, inner integration exit 1.
Build, lint, tagged vet, schema, contract and ordinary race stages passed. Source hashes were unchanged.
The unchanged `pkg/lifecycle` test `TestIntegration_CreateFromOperator_IsCreateOrFail` failed after 30.31s
during NewTestClient: Docker's `/containers/eae445…/start` exceeded its 30-second deadline. Captured start,
kill and destroy events plus the later absent-container inspection record cleanup.

The coordinator observed concurrent `e2e:agentic` processes 83272/83452; retained Compose labels identify
`claude/gh1112-fact-lane-identity`. These observations establish overlap, not causation. The process-exit
snapshot at 10:26:48 UTC records that both processes had exited.

The separate canonical integration stage **PASSED**, exit 0, in 725.344s (10:27:10–10:39:15 UTC).
Its recorded source hashes match the failed run and remained unchanged. Required local stages have passing evidence
across these two recorded runs; the full `task check:push` command itself remains failed.

[Gate provenance ZIP](gate-provenance-80565d7b.zip) preserves both original metadata/log pairs, the bounded Docker
failure-state capture, the process-exit snapshot and an explicitly labeled transcription of the positive overlap observation. It does not add a causal conclusion or a speed-improvement claim.
Original gate log SHA-256: `473837c1dcaa92f73d06f42f1a5e5dc10f7fca8b5c51fb9561ffe9a0fedd6325`.
Integration repeat log SHA-256: `02055daded0e9d24a6c17687d9fb0553bf4344fdfcb418277332cee327feefc5`.

Final archive/spec review and hosted CI remain required; no merge readiness is asserted.

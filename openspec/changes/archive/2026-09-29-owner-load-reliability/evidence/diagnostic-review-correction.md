# Diagnostic implementation review correction

The first reviewer-corrected diagnostic source is `natsclient/kv_filter_lifecycle_diagnostic_integration_test.go`, SHA-256
`c2975cce25a7a45021a6055f6d00cbaacdee215671112f9bf348d50ccfa4a2ac`.
The earlier `diagnostic-report.md` and `diagnostic-mutation-provenance.md` describe a historical first implementation;
their source checksum does not identify this corrected source. No production or graph-index source was changed.

## Review findings and correction

- Parent Wait after Kill (lines 186–202, 244–259): a finalizer is installed immediately after Start/Wait ownership.
  At 27 s it kills the child and gives the sole Wait owner 3 s to join. An unresolved join is reported without
  reading live output state or starting another case.
- Child early returns (lines 332–365, 403–524, 645–730): facade receive/send can stop on release/abort. One
  lexical owner cancels, releases, and joins the listing and facade under one terminal deadline on every exit.
  Close and joins share at most 8 s, within the accepted 10 s maximum. Child work is limited to 18 s, leaving
  27 s for the parent kill trigger and 3 s for Wait containment.
- Expired/nonblocked witness (lines 555–620): the gate requires distinct `[chan send]` goroutines, exact pinned
  SDK function names and lines, and a still-live captured framework context with the unchanged five-second
  deadline. Expiry before acceptance reports `GATE_NOT_REACHED`.

The first focused check log (`diagnostic-review-focused.txt`) exited 0 but took 14.00 s: its two early-finalizer
subtests each waited 7.00 s for the abort reserve. That **green run exposed an implementation latency defect**;
it was not a historical #1421 failure or a red test. Releasing an already-acquired facade in the lexical finalizer
fixed it. `diagnostic-review-focused-corrected.txt` passes the receive, send, post-Kill Wait-bound, and expired-gate
checks in 0.01 s of test execution. The exact focused command was:

```bash
./scripts/run-integration-tests.sh ./natsclient \
  -run '^TestKVFilterDiagnostic(EarlyFinalizer|KillJoinBound|ExpiredGate)$' -timeout=30s -v
```

The post-Kill check uses a never-completing synthetic Wait channel and verifies the kill callback runs once and
the 5 ms join allowance expires promptly. It does not claim to kill a live child process. The parent source owns
that wiring and reports `OWNERSHIP_UNRESOLVED` if its real Wait owner does not join in the reserved 3 s.

## Exact-source matrix and mutations

The real-NATS runs used the accepted canonical command:

```bash
SEMSTREAMS_KV_LIFECYCLE_DIAGNOSTIC=1 ./scripts/run-integration-tests.sh \
  ./natsclient -run '^TestIntegration_KVFilterLifecycleDiagnostic$' -timeout=180s -v
```

- Baseline exit 0: `diagnostic-review-matrix-baseline.txt`, SHA-256
  `1521c177b861ca980d08318e786938e9db6220145054d53b293ab311d460fe33`, all three cases pass in 10.75 s.
- Stop-delegation mutant exit 1 as expected: `diagnostic-review-stop-mutant-run.txt`, SHA-256
  `7fc5b4d4fb3c7e7abbdc257557013f5248a2f48c5deb692d22402a29bee0370a`.
  It compiled, passed the control, and failed the cancel case at the independent observer assertion
  `delegated Stop invocation count=0 want=1` in 1.96 s, before any deadline timeout.
- Restored baseline exit 0: `diagnostic-review-matrix-restored.txt`, SHA-256
  `244da55c9210a5b2fe5ca4b0ef86132fd7c20f0e76d571f16a05872eb1036480`, all three cases pass in 8.97 s.

For the Stop mutant, a cp backup at `/private/tmp/gh1421-review-stop-baseline.go` was created before replacement.
The script asserted exactly one `err := f.observer.Stop()` in the facade at line 388, replaced it with a nil error,
retained the one-line diff, and installed an EXIT trap restoring the backup. The observer at line 300 and assertion
at line 704 were unchanged. `diagnostic-review-stop-mutant.diff` SHA-256 is
`dc2c418d5fc4d0d6b65dcc4daeec934022a6c170e7d41a836fae30d4ece3e669`.
The backup and restored source both match the corrected-source checksum above.

One selected finalizer mutation independently proved the new fast-failure check. A cp backup at
`/private/tmp/gh1421-finalizer-focus-baseline.go` was made; the script asserted the unique already-acquired facade
release block in `finish`, omitted that release, retained `diagnostic-finalizer-mutant.diff` SHA-256
`dbe269d9f154bf86fdc0d01b1191e1c389f4744b260fa8132b2694f35d86d0b4`, and restored by EXIT trap.
The selected `TestKVFilterDiagnosticEarlyFinalizer/native_receive` compiled and failed its 2 s assertion after
7.00 s; `diagnostic-finalizer-mutant-run.txt` SHA-256 is
`b308943ac14c73df75df10b61bcb1f8a0f45dd0dbd73ddc96cb57d447e7a7109`.
The corrected focused run passed before the native matrix, and both cp backups match the restored source checksum.

## Observations and limits

The final restored run seeded 1,024 exact matching keys in one pinned NATS container, and the control compared the
entire exact set. After the first key was consumed, both injected cases captured complete stack blocks for distinct
native goroutines blocked at the pinned SDK's forwarding and watcher sends at about 28 ms, while the framework's
five-second context was live. Cancellation returned `context.Canceled` with nil keys at 28 ms; the unchanged
default deadline returned `context.DeadlineExceeded` with nil keys at 5.007 s. Native Stop was delegated once and
reported `nats: invalid subscription`; Client.Close still returned nil promptly with drain and closed statuses.
Post-release watcher presence varied across snapshots and does not prove a persistent leak or native join.

This controlled withholding did not reproduce #1421's historical 15-second drain timeout. It does not identify
the original stalled phase, host pressure, or subscription identity. The diagnostic is opt-in; retaining it as
permanent package coverage requires a separate cost and fidelity decision. The three finite named schedules,
exact-set oracle, and explicit causal gates remain the examples-based PBT decision from the accepted design.

## Final artifact identity and focused correction

The final retained source is native-kv-lifecycle-diagnostic.go.txt, SHA-256
`cbfd896bf49834ab5278c7c6a8c8fee50eb8fa6a2e301a975c906d7c134c7523`. Independent review approved this exact source.
The native matrix and mutation runs above belong to predecessor
`c2975cce25a7a45021a6055f6d00cbaacdee215671112f9bf348d50ccfa4a2ac`, not a new matrix on the final source.

The final source adds checked deferred fixture finalization before starting forwarding and changes the facade-join
label to identify the test facade only. Exact diff: diagnostic-final-tiny-correction.diff, SHA-256
`b96eaaa8dffca2c72e25c7a79598da55d8f4ad4d1f214fae5e91d4b34afd3aa3`. Native case/owner behavior is otherwise
identical; the reviewer accepted applicability of the predecessor evidence without another native run.

Final-source execution was focused only: canonical runner, -race, early-finalizer native_receive/output_send,
Kill/Wait bound and expired-gate checks, all PASS. Log diagnostic-tiny-focused.txt, SHA-256
`5af2eb1b803bffb161844a9ad97fa9c91f23e94ef729ea8b8cbdeb428ffae182`; package 1.506 seconds, exit 0.
This is reviewed evidence applicability plus focused execution, not an exact-final-source native matrix claim.
The compiled diagnostic fixture has been removed; reproduction.md documents the retained experiment.

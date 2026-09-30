# Experiment design review: final

**DESIGN REVIEW PASS** for exact SHA-256
`5e1e18c10f1d0aacda7d6b95e1042e44cc0c73d97862ce772997e2b891afd1f4`.

Independent reviewer confirmed both prior findings resolved. The experiment uses two existing schedules with phase/SDK
tracing and defers transport faults. Its decision table preserves the historical-cause limit. Startup, work, cleanup
and reporting total 33 seconds inside the 35-second kill trigger, followed by five seconds for Wait; admission and
substrate reserves fit the 180-second alarm.

`Client.NewKVStore` is the existing method at `natsclient/kv.go:55`. A traced SDK handle on `Client.GetConnection()`
provides the measurement seam without replacing client internals. The 1,024-key fixture's difference from CI is explicit.

Four deletion origins, unknown attribution, native completion limits, checked cleanup and applicable focused proofs
remain required. Changed recorder or ownership behavior needs current evidence; archived mutation evidence applies
only to unchanged mechanisms. No remaining material design finding.

Root accepts this bounded, private diagnostic slice under the user's authorization to continue the test-reliability
work. This is not approval of a production repair, historical attribution or issue closure. The user waiver was for
merged PR #1432 only. No new permission is needed to execute the reviewed diagnostic within its finite scope.

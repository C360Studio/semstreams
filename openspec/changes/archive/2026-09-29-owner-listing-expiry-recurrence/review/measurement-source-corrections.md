# Measurement source corrections and budget interpretation

## Narrow source review

Independent reviewer: **APPROVE** narrow source corrections at SHA-256
`f50543bbd69effa1cb29f0e45b0e3c2c59a1bd401aee05598eec794ef8d16d71`.

Both findings are resolved. Context-completion joining uses the shared terminal deadline and reports unresolved
ownership. Deferred trace finalization preserves evidence on early returns, runs after cleanup and combines its errors
with the primary failure. The parent rejects child failures and retains queued output after Wait.

No source blocker remains to the single authorized native pass once focused checks succeed. Execution evidence and
causal conclusions still require independent review.

## Budget interpretation

The first native launch was conservatively refused because the team initially applied a continuous wall-clock cutoff
from the first focused command, including time spent editing and reviewing. Only 87 seconds remained against a native
envelope of 130 seconds before reporting. This refusal is retained in the evidence record; no native pass had run.

Independent review confirmed that cumulative command execution matches the accepted design's five-minute aggregate
local execution allowance. Editing and review idle time are excluded. This interpretation does not reset the budget or
authorize another native pass. Preflight, builds, lock waits, tests and teardown all count.

Log birth-to-last-write times alone do not cover time after the last output. Admission therefore requires actual command
start/completion durations or a verified conservative upper bound for all prior executions. The remaining cumulative
allowance must cover the unchanged single native pass and terminal cleanup. Root accepts this interpretation under the
existing authorized scope; the developer must record the debit and remaining allowance before launch.

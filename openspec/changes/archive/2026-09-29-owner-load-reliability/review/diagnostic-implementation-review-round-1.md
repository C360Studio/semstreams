# Diagnostic implementation and evidence review — round 1

Reviewer: semstreams-reviewer, /root/gh1421_inventory_review, 2026-09-29.
Source SHA-256: da21c07c5b404d62f3a00a3899604fbe1c4e4d1f7926e49e2153eb3bc44484c9.
File: natsclient/kv_filter_lifecycle_diagnostic_integration_test.go.

## Findings

HIGH line 193 — Parent kill path waits without a bound.
After the child allowance expires, Kill is followed by bare receive from the sole Wait owner. Process/copy
completion could consume the outer package alarm. Bound post-kill join inside reserved cleanup authority, report
unresolved ownership on expiry, stop successor admission, and inspect output/error/stderr only after its writer
joins. Install immediate parent-owned child finalization after Start so parent exits/cancellation also terminate
and join the same owner. Normal writer/Wait synchronization is otherwise correct.

HIGH line 415 — Child failure cleanup is not consistently owned.
First/second-key and witness failures at 418–432 and invalid snapshots at 492–495 release without observing
facade.forwardDone or framework completion. Facade forwarding at 283/288 has unconditional native receive/output
send; release need not unblock it after the consumer exits. The work-bound path at 449–456 may join framework
and return without joining the facade. Install lexical cleanup upon acquisition: cancel, release and observe both
done signals under one shared at-most-ten-second terminal budget on every exit. Make test-owned receive/send
stoppable. Record native-call nonreturn and process containment explicitly when joins cannot finish. Do not start
fresh budgets per join or append five seconds close plus ten seconds cleanup after the twenty-second work window.

MEDIUM lines 338,429 — Causal gate can overclaim.
Match complete native blocked-send state/function, not only SDK filename/line. Require the captured five-second
operation context/deadline to remain live when accepting witnesses. Expiry before gates is GATE_NOT_REACHED.
The recorded passing logs do contain chan-send frames around thirty milliseconds, before the deadline; this does
not invalidate those specific observations.

## Independently verified execution evidence

The final baseline, mutant, diff and restored hashes match the report: a52418a7, c1f371c5, 59603b23 and 4e475460.
The cp backup /private/tmp/gh1421-kv-diagnostic-final-baseline.go matches the source hash above. The mutant changes
only the facade invocation at 308; observer 251–258 is unchanged. It compiled, passed control, reached cancellation's
explicit zero-count assertion and joined the facade; the following deadline case was skipped. This is valid
delegated-Stop sensitivity evidence, not native join proof.

The exact control set has 1,024 keys. Two distinct native goroutines were observed blocked at SDK 1451/1290 before
cancellation/deadline. Framework errors preserve context identity and return nil keys; real Client.Close returns nil
with DRAINING_SUBS/CLOSED observed in roughly 12–14 ms while delivery remains withheld. Releasing retained Keys
joins the facade and closes Keys. Immediate watcher-ID presence varies; persistent leakage and general native join
are unproven. This synthetic schedule did not reproduce the historical fifteen-second drain failure or identify
the original five-second expiry trigger.

CHANGES REQUESTED — the three bounded fixture corrections above. Historical measurements remain valid within their
stated successful schedules; current source is not yet approved as reliable retained diagnostic coverage.
No tests or edits ran in the reviewer role.

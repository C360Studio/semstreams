# Change: remediate graph-ingest test cleanup

## Why

#1423 executes the graph-ingest batch of #1417 after shared test support landed in #1419.
The independently accepted inventory reconciles 32 exact legacy cleanup identities across 16 files.
All discard an unbounded Stop result; six returning helpers also expose early setup assertion exits and caller
cancellation-order gaps. These are source liabilities, not evidence of 32 reproduced hangs.

## What Changes

Adopt the existing lexical test-owner pattern privately within graph-ingest. Own components before fallible setup,
transfer returning-helper ownership explicitly to callers, and use checked fresh finite terminal contexts before
Start cancellation and substrate teardown. Preserve distinct operation authority for replay/readiness phase fences.
Update the six helpers and their 44 measured callers, including files beyond the original 16. Remove two duplicate
TestClient Terminate calls. Keep all current skips and distinguish their three source-only cleanup repairs.
Reconcile the exact 32 legacy entries through the existing guard and independent source review.

## Impact

Production lifecycle behavior, public APIs, NATS topology and canonical runner ownership are unchanged.
The normative delta extends test-cleanup-policy with fixture ownership; current component-lifecycle and runtime-context
contracts remain unchanged. No new framework, watchdog, guard exemption or package-wide timing rewrite is introduced.
Current baseline is 329 debt entries / 89 resolutions; 334 is historical. Expected remaining legacy debt is 297 if no
independent changes intervene. Source counts do not imply guaranteed wall-clock teardown or executed skipped tests.
#1417 remains open for subsequent package batches; production defects observed during proof require measured scope
reconciliation. Accepted inventory is preserved at checkpoint `0085323e`; design review precedes implementation.

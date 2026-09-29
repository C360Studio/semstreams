# Owner load harness reliability

## Why

Issue #1421 records a framework KV deadline failure in the graph-index owner-filter load harness, followed by a
15-second NATS drain failure. This work repairs the measured cause while preserving meaningful regression failures
and prompt, owned cleanup. The owner selected this issue as the next test-reliability repair after #1428.

## What Changes

The mechanism is intentionally undecided at claim time. First enumerate the convergence read, its production KV
path, worker and watcher completion, cancellation, cleanup, runner ownership, and the governing test contracts.
Independent inventory review precedes design. The resulting design must separate observed facts from hypotheses,
including the issue's shared-runner-stall explanation, and identify a falsifiable proof for the chosen correction.

The #1284 owner ruling deliberately keeps genuine framework deadline breaches red. This claim does not authorize
weakening that contract, hiding errors with retries, or changing percentile budgets owned by #1287. Any required
change to that ruling must be presented explicitly for owner decision before implementation.

## Impact

Initial investigation addresses #1421 and adjacent NATS/test-infrastructure seams needed to establish its cause.
Exact source and contract impact will be reconciled after inventory and reviewed design. #1417 remains the broader
cleanup tracker, #1293 the verification-infrastructure tracker, and Claude's #1426 remains independently owned.

## Stop Point

The source-pinned inventory passed independent review at SHA-256
`d0872671356ee5505087160054d890384c13ceb19d4364d7fc2745a3b01fd3a1`; all 164 pins verified.
Original CI run 36567709902 / job 109403662339 and related issue/ruling snapshots are retained in the evidence
bundle. Historical attribution remains unproven. The bounded diagnostic design passed independent review and was accepted at SHA-256
`5dae252d611467eaba43f83b747f321e9c8ae4ca5cabca164ba76b88dbff0da6`. Implementation of that diagnostic is next;
no diagnostic execution, production correction or new test pass is claimed. No spec delta exists before the target behavior is accepted.

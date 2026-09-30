# Abort diagnostic source review — round 1

Reviewer: independent semstreams-reviewer gh1421_inventory_review.
Verdict: CHANGES REQUESTED; native HOLD.
Sources: ordinary68847d513fbfdd038cccd4070ade7162c0211341d6fe743e252f7d0f41e3494f;
tagged1571f26742d68739dca4425e10f16e339674afab1f4e3c2a6b0f7909b93a9b57.

## High: failure output is discarded

Ordinary source70 parseEvents=false drops stderr. Parent108/149 flushes partial stdout after draining events;
kill finalizer never drains remaining events. Panic/race/timeout evidence can disappear. Retain bounded stderr,
finalize both streams before publishing remaining output on normal/contained exits, and prove stderr, partial-line
and early-exit retention.

## High: required early-exit ownership proof is absent

Tagged source197 finalizer is not exercised by ordinary proofs. Expired-gate proof merely defers release/cancel;
early fatal exit does not independently join listing. Exercise actual private finalization with fakes: held-gate
return, once-only Close, finite unresolved join and independent fixture recovery. No separate test implementation.

## High: child cleanup reserve is not anchored to alarm

Tagged source236 work20s + join3s + Close15s totals38s starting after entry, without guaranteed startup/report reserve
under40s process alarm. Derive work admission from actual test deadline, reserving join, full15sClose and reporting.

Native Keys delegation, synchronous Stop, natural expiry and strict witness cardinality match accepted design.
Mutation packet is honest: intended classifier assertion failed; restoration supported; channel-type proof corrections
isolated. Passing-baseline/mutant/restored sequence remains incomplete. Tagged source remains uncompiled.

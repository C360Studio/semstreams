# Workload observer source review, round 1

Canonical independent reviewer: `semstreams-reviewer` (`gh1421_inventory_review`).
Disposition: **CHANGES REQUESTED**. Native execution remained on hold.

The developer preserved the pre-correction source with `cp`; root verified these backup checksums:

- Observer: `f5a64c07d7fe5991b132615c97d7479c47db608172a12bcea9209bad407e6c65`.
- Harness: `5dc159dc991414b2145b39136ebec166a1ae6370668c2084331cb0d55067a658`.

The reviewer inspected the live pre-correction source but did not independently record its full checksums at that
moment. The reviewer subsequently verified both retained artifacts and the cited finding ranges. This is retrospective
artifact verification, not a claim that the hashes were recorded during the initial live review.

## Findings

1. **High: callback finalization was not lexical.** The outer scope deferred publication, while callback joining
   remained a manual call after listing. The FailNow proof joined before its fatal assertion, so it did not prove
   early-exit ownership. Install once-only bounded finalization before native construction and prove Goexit with an
   active callback. Release and join held fixtures even on assertion failure. An unresolved join must not receive a
   second terminal budget.
2. **High: snapshot end and return-marker observation were incoherent.** The timestamp preceded mutex acquisition,
   allowing a later marker to produce a straddling label outside the stated capture interval. Observe the boundary
   coherently and cover the ordering deterministically.
3. **Medium: a completed Stop was labeled collection.** Distinguish completed Stop awaiting the caller's return marker;
   do not imply collection is still active.
4. **Medium: fake proofs inherited Docker startup.** The integration tag selected the package's real-NATS TestMain.
   Move observer core and fake proofs into the ordinary unit lane. Keep only ExactActivation beside the tagged harness.

Native-channel delegation, bounded five-attempt selection, the unchanged deadline and deferred publication were
otherwise consistent with the accepted design. The initial failing publication assertion established that publication
was necessary, but did not establish callback cleanup on early exit.

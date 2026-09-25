# Service listener ownership tasks

## Accepted scope

Owner acceptance: https://github.com/C360Studio/semstreams/issues/1120#issuecomment-5835779961.
The accepted design and inventory hashes are preserved in `review.md`. Broader E2E/composer API work remains #1301.

- [x] Independently verify the 27-call inventory and review the listener-ownership design.
- [x] Record owner acceptance of the reviewed design and materialize the capability delta before implementation.
- [x] Implement the accepted concrete metrics API/address behavior and private service seams with focused behavioral proof.
- [x] Replace all 27 racy allocations, preserve native acquisition/refusal coverage, and surface early startup errors.
- [x] Record required sensitivity experiments and restored checks; complete the owner-ruling conformance table.
- [ ] Run required preflight gates and obtain independent implementation review, resolving findings.
- [ ] Reconcile and archive the capability delta as the final content commit; obtain narrow archive/spec-sync review.

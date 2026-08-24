# Design: Controlled lifecycle lane and bounded abort observation

## Checkpoint

- Accepted compact inventory: `docs/proposals/gh1062-compact-lifecycle-lane-inventory.md`.
- Inventory SHA-256: `f33782076205b58504124ddfe2fb391cc70073a92f6c2b17f6c99bebac5820ed`.
- Independent result: `INVENTORY PASS`.
- The owner accepted this design on 2026-08-23 at independently reviewed SHA-256
  `10fae45ccada66c38092b02dafe6f72b6081b54e4d6c18b290c8ee3d3e21a809`.
- This design supersedes the abort-to-nil target in lifecycle amendment SHA-256
  `82c02d41468988987d159cfee3b758b038c39151b13155cd10b393aa9be1f307`; that earlier artifact remains historical
  evidence, not implementation authority for this target.

## Controlled lane

Composition keeps the accepted Start context and NATS substrate live while calling Stop with a separate finite
context. Stop performs the owner's resource-specific admission fence, drain, join, and finalization and returns nil.
Only after Stop returns does composition cancel Start and terminate shared infrastructure. The two Rule readiness
tests exercise this lane and retain strict nil assertions.

## Abort lane

The accepted Start parent has already ended. Continuing work loses its original authority and orderly native drain
may already have terminalized exact handles. Stop still runs synchronously with a fresh finite caller context, but it
is bounded best effort: accurate native cleanup and deadline errors remain results rather than being normalized to
nil. If the Stop bound wins, the portable contract does not claim complete join or leak freedom and grants no later
rejoin authority.

Abort cleanup does not invent replacement authority, detach work, retain or recover the Start context, widen timeout,
retry cleanup, or alter owner ordering.

## Tests

The standard AcceptedStartParentCancellation proof invokes Stop exactly once and synchronously under a five-second
context, permits a nonnil result, and requires the exact caller-context error whenever that bound wins. The Rule
real-NATS proof uses one test-owned Stop call shared by the body and failure cleanup, additionally keeps NATS live,
records any terminal result, and requires `errors.Is(stopErr, stopCtx.Err())` whenever `stopCtx.Err()` is nonnil. It
does not inspect error text or use nil as an abort success oracle.

Repeated abort runs are observational evidence. Accurate native errors or deadline results are permitted. A panic,
test timeout, lost deadline identity, dead NATS substrate, or controlled-lane failure remains a test failure.

## Adopter seam

An adopter needs only the composition rule: keep Start live through controlled Stop; after unexpected Start loss,
still call bounded Stop and observe its result. The adopter does not predict native watcher state, create replacement
authority, or retry after a bound wins.

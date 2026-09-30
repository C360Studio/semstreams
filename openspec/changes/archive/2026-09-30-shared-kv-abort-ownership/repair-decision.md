# Selected repair scope

The coordinating session accepts the independently reviewed v2 design within the user's instruction to continue
fixing the current test-reliability chain. This selection authorizes the private production bug fix and regression
work below; it is not a new owner ruling, issue-closure declaration or merge waiver. No exported method set,
configuration knob, wire format or accepted filter grammar changes.

## Selected behavior

Use the full bucket's WatchFiltered capability for KVStore filtered listings and the existing prefix alias.
Consume its native Updates channel, Stop synchronously once, then wait at most five seconds for delivery closure.
The terminal window derives from the operation context without inheriting cancellation. It bounds only the wait
after Stop returns. Contextless Stop and total public-call duration remain outside that bound and remain measured.

Snapshot completion plus delivery closure is required for successful results from a returned watcher. Cleanup
errors become visible and are joined with any primary error; failed calls return nil keys. The live-context,
no-watcher direct ErrNoKeysFound compatibility case is the explicit exception. Cancellation takes precedence there.
Only already-terminal Stop conditions confirmed by actual Updates closure can be accepted as successful cleanup.

The five-second private window supplies finite containment without changing the existing operation deadline or
handing callers a new tuning responsibility. Unit expiry examples use the virtual test clock, preserving this
production interval without real five-second waits. No latency assertion or workload is relaxed.

## Boundaries and remaining evidence

The minimal FilteredKeys/CatalogReader contracts, unfiltered Keys and long-lived Watch remain outside this repair.
Their hidden native ownership limits are retained in repair-design.md. Full-bucket test adapters and the persistent
owner-load observer must follow the new production seam without supplying a diagnostic drain.

This repairs the positively observed terminal-delivery defect. It does not attribute the initial hosted listing
stall or fifteen-second client-drain failure, prove indefinite survival, or establish a native goroutine join.
Issue #1421 remains open. The earlier #1432 waiver does not authorize merging #1435.

The original experiment is complete and its compiled diagnostic has been removed. Its source, raw native log and
false-green limitation remain intact. A new, permanent public-path regression must fail on unfinished cleanup;
classifier-only evidence cannot satisfy this requirement. Independent source review and mutation restoration gate
one focused post-repair native validation. A failing native result stops escalation to broad gates until understood.
Required final verification remains task check:push; successful focused validation does not replace it.

## Implementation review clarifications

The ordinary regression observes closed and drained Updates immediately after the public call, then separately
joins the test-owned producer epilogue. It must not demand an immediate goroutine join from a delivery-closure
contract. The native capacity witness requires a live operation context around the observation.

The focused native regression uses the existing normative server version/digest constants, matching the accepted
NATS 2.14.4 counterexample environment. This is stricter than using the mutable default TestClient tag; actual image
and server provenance are still recorded. The runner's default-image preflight is a separate observation.

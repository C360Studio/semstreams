# Scope reconciliation: persistent failure evidence

The first measurement pass did not explain the historical expiry or additional Stop/return interval. The accepted
actual-workload design now identifies a concrete, independently reviewable outcome: persistent phase evidence in the
five initial CI predicate-forward attempts. Historical cause attribution and production repair remain open on #1421.
Completing this diagnostic change does not resolve that issue, and its PR must not declare `Closes #1421` at landing.

The architect supplied an added graph-index requirement for this private test-harness behavior, beside the existing
owner-load failure-retention requirement. It changes no production contract. This clarifies the accepted design's
“No production spec delta” statement without modifying that historical design or its reviewed SHA-256. The temporary
measurement artifacts remain historical evidence, not current production behavior or recurring suite coverage.

The accepted single focused native pass is an experiment limit. Required repository verification before an implementation
push remains governed by the preflight skill and shared protocol. A full suite or hosted run is not authorized merely
to seek another sample or retry a failure to green. Required validation must be recorded separately from the one-pass
experiment and may not be cited as historical cause evidence without an actual observed failure.

No additional issue is needed for the unresolved cause: #1421 already owns it. Graph-query #1433 / #1434 remains at its
accepted inventory checkpoint. The previous owner waiver applied only to merged #1432, never to this follow-up.

## Cleanup guard compatibility correction

Required preflight stopped in nine seconds on two uncertain deferred callback sites: `ownerLoadAttemptScope` invokes
`reportCleanup`, and `ownerLoadObservationScope` invokes `publish`. The canonical architect reviewed both helper
bindings in default and integration selections. Their bodies report copied diagnostics; lifecycle finalization remains
separately owned by the existing bounded `o.finish(parent)` call.

The cleanup guard requires exact reviewed classification metadata for these two deferred diagnostic-publication callbacks.
This correction permits only their `resolutions` records in `test/testinfra/cleanup_baseline.json`. It adds no legacy
cleanup debt, changes no guard rules or cleanup ownership, and preserves the accepted observer behavior. This explicitly
reconciles the original design's baseline-file exclusion. Root accepts this narrow implementation compatibility correction
within the user's instruction to continue, subject to independent review of exact sites, dependencies and classification.

The records retain cleanup applicability; they do not use an ordinary-only disposition. Complete caller fingerprints
include the focused proof's buffered channel and publication. Non-lifecycle classification does not prove bounded logging,
diagnostic callback completion or native watcher completion. The original native sample retains its original source hash;
the later deadline-assertion refinement changes only a fake proof and needs no second diagnostic experiment.

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

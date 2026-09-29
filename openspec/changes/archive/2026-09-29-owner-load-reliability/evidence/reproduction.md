# Native lifecycle experiment reproduction

The temporary diagnostic is retained as `native-kv-lifecycle-diagnostic.go.txt`; it is not package test coverage.
Final source SHA-256: cbfd896bf49834ab5278c7c6a8c8fee50eb8fa6a2e301a975c906d7c134c7523.
Its final review is `../review/diagnostic-implementation-review-final.md`; execution identities and limits are in
`diagnostic-review-correction.md`. Historical runs are explicitly labeled in the older report/provenance files.

To reproduce in a dedicated SemStreams checkout, first ensure the destination file below does not exist. Copy
this artifact verbatim to `natsclient/kv_filter_lifecycle_diagnostic_integration_test.go`, verify its SHA-256, then
use the canonical runner from the repository root:

```bash
scripts/run-integration-tests.sh ./natsclient -run '^TestKVFilterDiagnostic' -timeout=60s -v
SEMSTREAMS_KV_LIFECYCLE_DIAGNOSTIC=1 scripts/run-integration-tests.sh \
  ./natsclient -run '^TestIntegration_KVFilterLifecycleDiagnostic$' -timeout=180s -v
```

The runner owns -race, the shared-host lock and Docker prerequisites. The native experiment starts one pinned NATS
container and three sequential isolated child cases. Successful output requires exact control, active cancellation
and default-deadline assertions, with independently observed Stop delegation and native blocked-stack gates.
Remove only the copied diagnostic file when finished. Do not install it as a default CI test or change production
SDK, listing, drain, retry or deadline semantics to reproduce the experiment.

The last native matrix/mutations ran on c2975cce25a7a45021a6055f6d00cbaacdee215671112f9bf348d50ccfa4a2ac.
The retained final source differs only by pre-goroutine proof-fixture cleanup and a corrected join-event label;
the exact diff is retained. Final source received focused execution plus independent applicability review. Neither
version reproduces the historical #1421 drain timeout. Native callback disappearance in a snapshot is not a join;
test-facade completion and child-process containment are reported separately.

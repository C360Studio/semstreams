# Rule cleanup verification handoff

The bounded rule batch repairs all 24 accepted cleanup roots and 37 associated helper calls in test code.
Private concrete owners protect setup escapes, preserve accepted Start authority through synchronous Stop,
record the attempt before entering native cleanup, and check results before substrate teardown.
All six external execution clocks still begin after Initialize; direct cancellation defers satisfy tagged vet.

The independently approved baseline is 273 legacy entries and 94 resolutions. All 273 retained debt entries
and 90 pre-existing resolutions are unchanged. Four new exact resolutions classify private native cancellation
functions; they do not exempt lifecycle Stop calls. The unchanged cleanup guard passes.

## Evidence and limits

The implementation report, source manifests, mutation patches, backups, logs, and independent reviews are retained
under this packet's `review/` directory. The original 34-case integration run remains evidence for its recorded
source. The later correction changes only the external integration file and passes its seven selected cases.
The final 16-file source identity is `evidence/implementation-source-current.json`.

The PBT decision uses named ownership histories and native completion observations; three selected plausible
faults are detected by compiled mutants with unchanged assertions and exact-byte restoration. New proof waits
are bounded, and failure exits release and observe test-owned work. These checks do not turn a supplied native
Stop deadline into a hard interruption guarantee for contextless watchers/cache operations or ignored cancellation.

The first full local gate stopped at tagged vet after 14.364 seconds. The six cancellation defers were simplified,
reviewed, and verified before rerunning. The final full pre-push gate passed in 858.046 seconds; its exact command, status, and full log
are retained as `evidence/check-push-final-status.json` and `evidence/check-push-final.txt`.
The final rule integration package passed in 45.131 seconds. All 16 reviewed source hashes and the approved
baseline hash still match after verification.

## Landing and remaining work

The capability policy already states this contract. No normative spec delta is required; archive with validation
enabled and `--skip-specs`, then obtain the narrow archive/spec review. Hosted checks must evaluate the final head.
The live main-branch rules require `CI Status Check` and `e2e statistical`. The CI summary depends on Lint, Test,
Build, Schema Validation and Tier 1 API Compatibility; the additional slow-consumer attribution E2E job is also
reported. Local green does not substitute for those current-head hosted results.

#1421 is an open observed required-Test-job flake. The #1404-specific waiver does not apply to #1429. The shared
protocol requires a landed fix or explicit owner waiver recorded on this PR before merge, even if fresh CI is green.
This work records the hold without expanding into graph-index or authorizing a waiver.

#1417 remains the open cleanup tracker. Until merge, main remains at 297 debt entries / 90 resolutions; this branch
proposes 273 / 94. After the known CI flake is resolved or explicitly waived, the next default cleanup package is
processor/graph-query (39 current entries), followed by the separately reviewed packages listed on #1417.
#1293 retains gate duplication/cancellation/visibility; #1411/#1412 retain production lifecycle scope. Claude owns
#1426/#1427. #1416 retains its separate owner disposition. No additional issue was needed for this bounded batch.

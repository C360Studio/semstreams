# Repair implementation review, round 1

Reviewer: semstreams-reviewer, gh1421_inventory_review. Baseline: 81a4deb8 plus the scoped repair.
Production SHA-256: 30cfcf2bff1c795611657369d5923c8e70a6054c0f15a2d3d2c9097e7d335610.
Ordinary-test SHA-256: 5d1bc418ecbc50a4b5b93c097b7842c8721b251bd04679eb7c605a860438a31b.
Native-test SHA-256: 5582655e7cc5bfdc81d5eaa1da531acb08713a508d80aa40c3066d3c80892613.
Observer SHA-256: f70308a9839fd7933c60a636cec5ee1bb145c315693d5aa96140ec2355e822c7.

CHANGES REQUESTED. Native validation remains on hold.

1. HIGH, ordinary test line 185: Updates closes before the producer's deferred done close. An immediate done check
   can fail correct production. Assert closed/drained Updates at public return and separately join the test-owned
   producer epilogue. Preserve omitted-drain sensitivity.
2. MEDIUM, native test line 104: the capacity-observation select can win after context expiry. Check the actual
   child context's liveness around the observation.
3. MEDIUM, native test line 47: the default server tag differs from the accepted counterexample. Reuse existing
   normative server version/digest constants for this validation.

No production semantic, observer or cleanup-metadata blocker was found. The full-watcher implementation matches
accepted error, ownership and timer behavior. The observer remains passive on exact Updates, including ownership
of a returned watcher alongside a constructor error. Reached mocks preserve matching and constructor faults.

The reviewer independently compared all 273 unchanged debt entries and 96 resolutions; only the same two
resolutions' dependency fingerprints changed. Site identities, rationale and reporting-only callbacks remain.
Eight observer ZIP members and hashes were independently verified, including the final exact-source race log.

The initial RED remains meaningful historical evidence. Correct the fixture, repeat the owning-path mutation with
exact restoration on final proof source, and request narrow re-review before native admission.

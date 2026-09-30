# Core preparation checkpoint

This archive retains an earlier developer checkpoint, before the review corrections. Filenames inside use
"final" for that checkpoint, not final approval. The source hashes inside the manifest identify the exact bytes;
no native test had executed. Its original immediate producer-done assertion was later rejected as potentially
flaky. The code, RED, mutations and outcomes remain historical evidence.

After correcting the fixture's immediate producer-done assertion, a drain-bypass mutation survived the cancellation
example because collection could consume its only entry. The independent never-closing example still failed.
A subsequent production context precheck made this specific example deterministic, but review required the fixture
itself to guarantee a post-Stop delivery obligation, independently of that check.

The precheck is accepted as prompt cancellation handling within the existing contract. Mutation evidence does not
establish that this production shape is necessary. The subsequent final fixture must retain the initial pending
entry and require another delivery after an explicit Stop handshake, then closure. Final proof is tracked separately.

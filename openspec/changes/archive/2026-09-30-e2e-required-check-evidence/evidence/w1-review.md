# #1222 W1 correction review

Mode: bounded read-only implementation re-review. Worktree
`/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`, base
`fe6e2cc03e16f5db47e293f55939548572f204cc`. Only the frozen Writer correction was
reviewed; evolving C5/C6 consumers and inference corrections are not certified.

## Disposition

**Original W1 resolved, but CHANGES REQUESTED for new W2 below.**

`writer.go:186–244,259–317` now compares terminal members against retained
requirements, restores removed/renamed/demoted obligations, and records declaration
drift as failed proof before serialization. Missing members retain their declared
obligations and missing summary; failed observations remain retained. Unchanged
initial-to-terminal proof still passes. `:359–365` refuses changed declarations on
another initial write and terminal-to-initial rollback for required runs. Legacy
RequireEvidence=false is outside those guards. No catalog or exported shape was added.

## HIGH W2 — writer.go:276 — a failed terminal submission becomes declaration authority

`reconcileInitializedChecks` reads the immediately preceding aggregate and treats
all of its Scenarios as initialized declarations, even when that aggregate is
terminal. On a foreign member RunID, lines 296–298 restore CheckRequirements but
leave the foreign RunID in the retained failed Result. A subsequent correctly
bound terminal correction is compared against that foreign ID and fails again.
Likewise, duplicate members in one failed terminal submission become duplicate
*initialized* declarations at lines 278–280; a later single correctly declared
member hits the duplicate branch at line 293. Neither corruption was present in
the original initialized declaration.

This is a static source trace, not a reviewer-executed reproduction. It breaks the
accepted same-run aggregate correction behavior (`implementation-handoff.md:193–194,
242`) while leaving the first failed artifact readable.

Smallest contract-correct remedy: retain canonical initialized member identity and
requirements through failed terminal serialization, and do not promote malformed
submitted identities or duplicate observed records into initialization authority.
Preserve the offending supplied identity/duplicate facts as failure diagnostics and
retain the failed terminal artifact. Do not add a recovery service or correction
history, and do not erase real failed observations or overwrite member files.

Verification needed: initial declaration → terminal foreign identity / duplicate
submission (retained failure) → correctly bound terminal correction of the aggregate
serialization (permitted); unchanged declarations must remain fixed throughout.
Also retain the current removed/renamed/demoted and missing/failed controls.

Refutation: later corrections already work when the first failure leaves canonical
member identity and one declaration per member; the new tests exercise only
initial-to-terminal transitions. Config equality and top-level run identity checks
do not repair the poisoned per-member baseline. This is not a request to turn an
actually failed observed check into a pass or replace immutable member evidence.

## Snapshot and evidence

SHA-256 values matched before/after review under `test/e2e/results/`:

| File | SHA-256 |
| --- | --- |
| writer.go | 9f68ac8c2399e5d0132b9bb85d7a128aef5d4871d8775bd5873dae8a4059f2c9 |
| writer_test.go | a3f17ce4acfaf5a346a1f6a6b18edf57df296f4aeabd87e7daa6cfbbeb5ae57b |
| writer_task.go (unchanged) | b70e3763a4f1f911b192fc6c300740dfc0ce53030f8fea4d68eaae3100a8c5bd |
| writer_task_test.go (unchanged) | cd313b10443862ebf2079a507b42583fa31a62f3336c02c96b61e11bb5c7681c |

Checkpoint `/private/tmp/semstreams-1222-w1.md`:
`ba7b6467c873c3e9bf3dd3c268cd4e823efde2d994755ba9d3da441b1b35354b`.
Read/hashed logs, prefix `/private/tmp/semstreams-1222-w1-`:

| Log | SHA-256 |
| --- | --- |
| red.log | 367813ab20ca55263497d93ff1771d92c8475865213624a375c4c66455a68e9a |
| reinit-red.log | 26105cd65935ed011713a4f687a8fb1848b8b21bfbac092f74f80f906c583ee5 |
| green.log | af7a54878025518635c0b27d80e62a35b2686060cafd1dce49f250aa19f23ca0 |
| race.log | 312df5c826f15e778aa4796c351afe028fca29def33b58bc8c597859c4d4122e |

Behavioral RED reached the intended assertions for remove/rename/demote and changed
reinitialization. GREEN records package success including those controls; race
invocation is author-recorded in the checkpoint. This is behavioral regression
evidence, not a new executed mutation claim. The prior explicitly accepted bounded
deferral for two auto-review-denied Writer mutations remains unchanged; exact
sensitivity stays UNVERIFIED. No new experiment was attempted or authorized here.

No reviewer tests, source edits, Git/GitHub operations or assembled gates. No broader
package-consumer freeze or feature readiness is asserted. Release this source freeze
after retaining the report; W2 needs correction and re-review.

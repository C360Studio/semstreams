# #1222 Writer W2 bounded re-review

Mode: read-only implementation re-review. Worktree `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`; base `fe6e2cc03e16f5db47e293f55939548572f204cc`. Scope is W2 in `test/e2e/results/writer.go` and its Writer tests. Scenario, CLI and Task reporter edits were not reviewed. No source edit, mutation, Docker or reviewer-run test.

## Disposition

**APPROVE W2 for this bounded correction.** The W1 defect (failed terminal evidence poisoning the next same-run aggregate correction) is resolved for the foreign-run and duplicate-member cases it identified. This verdict does not establish whole #1222 readiness.

`reconcileInitializedChecks` now canonicalizes each submitted member before a terminal artifact is marshaled: it restores the persisted RunID and exact CheckRequirements for declared members, while preserving the supplied foreign RunID as an Error (`writer.go:307-318`). Duplicate submitted records collapse to one canonical member; the duplicate diagnostic, Errors, Warnings and CheckObservations, including a failed observation, are retained (`writer.go:285-301`). FinalizeChecks sees those Errors/duplicate observations and fails proof rather than laundering the artifact (`writer.go:194-212`). A subsequent correctly bound aggregate submission reads one canonical declaration from that failed artifact and can complete. No member file is overwritten by this aggregate path (`writer_task.go:139-170,244-285`).

The behavior test writes an initial declaration, submits each malformed terminal, loads and asserts a failed artifact with canonical identity/declarations and diagnostic, then writes and loads a complete corrected aggregate (`writer_test.go:423-495`). The duplicate case explicitly asserts the failed observation remains. The retained RED reached the canonical-declaration assertion in both cases; GREEN passes both. W1 remove/rename/demote and changed-reinitialization tests also pass (`writer_test.go:315-420`), as do missing/failed-member controls (`writer_test.go:497-552`). The W1 constraints and same-run correction contract are consistent with `implementation-handoff.md:184-194` and active `e2e-evidence` delta. No new exported surface or durable authority was added by W2.

Attempted refutation: a failed terminal becoming the next prior snapshot would reintroduce W2 if it retained a foreign RunID or duplicate member list. The current canonicalization leaves one member with the initialized RunID and requirements before persistence. The next correction compares against that record, while the failed duplicate/foreign facts remain as failure diagnostics. This is proven for the two reported malformed shapes; no general crash/restart history or arbitrary corrupt-JSON recovery is claimed. Existing explicitly accepted deferral of two auto-review-denied Writer mutation experiments remains unchanged; exact guard-removal sensitivity is UNVERIFIED, with the accepted limits in the prior Writer review.

## Frozen evidence verified

Source SHA-256 matched the author checkpoint before and after this review:

| File | SHA-256 |
| --- | --- |
| `writer.go` | `1164d792f6e71c408e3b6a83368bf212a24aabe93aadff63fe356549889bb7cd` |
| `writer_test.go` | `d32acb3bb275d0af6ef79649989729add7c920b9a29b7dc965d235a1b539f0fa` |
| `writer_task.go` | `b70e3763a4f1f911b192fc6c300740dfc0ce53030f8fea4d68eaae3100a8c5bd` |
| `writer_task_test.go` | `cd313b10443862ebf2079a507b42583fa31a62f3336c02c96b61e11bb5c7681c` |

Author checkpoint `/private/tmp/semstreams-1222-w2.md` SHA-256 `8260cfc6bea6210302be8837cd501d04a2a3ef15b9a11a538631a7f4ffb76bff`. Raw logs were read and hashes verified: RED `/private/tmp/semstreams-1222-w2-red.log` `9a993f92a3bcb44c1cdd9279dc98123a93c04bed9a05358c7ef21075a106f1e9` (exit 1); GREEN `/private/tmp/semstreams-1222-w2-green.log` `0f902b4b2d75ec734c665fa10002aec2e916a91e01e54dd186bb0f26b8372671` (exit 0); race `/private/tmp/semstreams-1222-w2-race.log` `5351e95438bfb2c1482bc5dce4dfbcc20330e88207823b8a67fba1adc182e310` (exit 0). Exact author commands are in the checkpoint. These are local external artifacts, not CI or in-tree gate proof; assembled/broad gates remain outstanding. Active tasks do not mark implementation/review/gates complete.

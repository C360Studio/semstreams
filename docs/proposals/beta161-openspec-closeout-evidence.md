# Beta.161 OpenSpec closeout evidence

Baseline: `1089545bc5eadb78facf657e89a56d61072df6ba`

Accepted inventory body SHA-256: `b029f68cea409c93517384e3e9152f4508d7a52133b772d32d72e23968fcdd50`

Owner-accepted design body SHA-256: `d8ab2c70c77f3be2aa2cf74215dffe51c78166b9dc886f7ce7c5ab24b841e060`

Status: documentation/OpenSpec transaction complete; pending independent canonical review and owner merge decision.

## Scope

This transaction changes SemStreams documentation and OpenSpec truth only. It runs no Docker, integration, E2E, or
runtime tests; accesses no sister repository; mutates no GitHub issue; and changes no Go, Svelte, or generated schema.

## Supplied implementation evidence

| Change | Independent session review | Focused race | Real-NATS evidence |
|---|---|---|---|
| #947 capacity neutrality | `/root/gh952_semdev_review`: APPROVE, no findings | PASS, 1.333s | `natsclient` PASS, 4.276s |
| #948 durable MaxDeliver | `/root/gh952_semdev_review`: APPROVE, no findings | `config` 1.552s; observer 1.402s; helpers 1.404s | `config` 28.088s; observer 9.644s; restrictive ACL and three-node proof included |

PR #947 and #948 have empty GitHub review collections. The evidence above is current-session independent review, not
retroactive GitHub approval. The terminal-normalization task ledger already records its independent SemStreams review.

## Immutable active-tree verification

### #952 checkpoint

Command:

```bash
grep -v '^#' openspec/changes/reserve-typed-user-response-subjects/checkpoint.sha256 | sha256sum -c -
```

Result: PASS, all ten recorded artifact bodies reported `OK`.

### Post-G manifest

Command:

```bash
(
  cd openspec/changes/post-g-tag-safety-closeout
  sha256sum -c manifest.sha256
)
```

Result: PASS, all twelve manifest-covered bodies reported `OK`.

## Mutable task disposition

- #947 final review/evidence task is complete from supplied current-session evidence.
- #948 task 4.4 is complete from supplied current-session evidence.
- #953 SemTeams task remains `[ ]` and is explicitly superseded as a product-owned, nonblocking archive gate.
- No unsupported `[~]` marker is used.

## Companion truth

`beta161-user-response-current-truth` contains the four framework-owned #952 requirements plus the accepted
fresh-storage/downstream-ownership requirement. It contains no SemDev or SemTeams product requirement.

`beta161-post-g-current-truth` carries all seven frozen capability outcomes and preserves the merge-sensitive current
scenario identities. The accepted preservation count is:

| Capability | Current identities preserved from the merge guard |
|---|---:|
| `entity-id-contract` | 2 |
| `framework-composition` | 3 |
| `graph-clustering` | 2 |
| `graph-index` | 3 |
| **Total** | **10** |

Strict pre-archive validation passed for each companion and for all 50 current specs/changes. `git diff --check` was
clean.

Read-only replay after the post-G companion archive found every accepted capability requirement and the complete
scenario union. The companion archive itself completed without an OpenSpec merge-guard omission.

## Bounded archive-output correction

After archive step 4, strict validation passed 49/49 but `git diff --check` reported one archive-generated final blank
line at `openspec/specs/stream-provisioning/spec.md:626`. The canonical reviewer approved one bounded mechanical
correction. Only that final blank line was removed; requirement and scenario content remained unchanged. No other
mutation occurred between the failed check and this correction.

After archive step 12, strict validation passed 49/49 but `git diff --check` reported archive-generated final blank
lines in `entity-id-contract`, `framework-composition`, `graph-embedding`, and `graph-index`. Read-only replay proved
the exact ten-scenario union and all seven post-G outcomes, and the canonical reviewer approved a second bounded
mechanical correction. Only those four final blank lines were removed; no requirement or scenario content changed.
No other mutation occurred between the failed check and this correction.

## Suspended change baseline

`semantic-tier-split` must remain byte-identical:

| Artifact | SHA-256 |
|---|---|
| `proposal.md` | `68829750b18bbd3b1c8410b9a22b236f86ca994f56cac83440f02ad56137072e` |
| `specs/e2e-tiers/spec.md` | `bb8e269aa8d04f3224e87698e964d08de1a635c2162104c34e5801b1d2e21e2f` |
| `tasks.md` | `6a47eaea26e25e35d67fc4880a824102a8a33eb471458d6c4cbe13ba46ed9bd9` |

## Exact archive transaction

1. PASS — active-tree #952 checkpoint verified 10/10.
2. PASS — active post-G manifest verified 12/12.
3. PASS — archived capacity change normally as
   `2026-08-14-stream-capacity-rejection-is-circuit-neutral`; strict validation and diff checks passed.
4. PASS — archived max-delivery change normally as `2026-08-14-durable-max-delivery-occurrences`; strict validation
   passed 49/49, then the approved archive-generated EOF correction restored a clean diff check.
5. PASS — applied the accepted `max-delivery-observability` Purpose.
6. PASS — archived terminal normalization normally as `2026-08-14-normalize-agent-terminal-settlement`; the one
   downstream-owned task remained recognized `[ ]` truth and post-archive checks passed.
7. PASS — applied the accepted `agentic-terminal-events` Purpose.
8. PASS — archived the #952 framework-only companion normally as
   `2026-08-14-beta161-user-response-current-truth`; post-archive checks passed.
9. PASS — applied the accepted `user-response-subject-ownership` Purpose.
10. PASS — archived the immutable #952 original with `--skip-specs` as
    `2026-08-14-reserve-typed-user-response-subjects`.
11. PASS — immediately mapped and verified all ten #952 checkpoint paths; every body reported `OK`.
12. PASS — archived the post-G merge-safe companion normally as `2026-08-14-beta161-post-g-current-truth`; strict
    validation passed 49/49, then the approved four-file EOF correction restored a clean diff check.
13. PASS — read-only replay confirmed the accepted `2 + 3 + 2 + 3 = 10` merge-sensitive scenario preservation table
    and all seven post-G capability outcomes in current specs.
14. PASS — applied the accepted `release-candidate-proof` and `rule-action-observability` Purpose bodies.
15. PASS — archived the immutable post-G original with `--skip-specs` as
    `2026-08-14-post-g-tag-safety-closeout`.
16. PASS — immediately verified the relative twelve-body manifest from the generated post-G archive directory; every
    body reported `OK`.
17. PASS — final repository-truth preparation shows strict validation 48/48, one active suspended change, seven
    archive directories, five approved Purpose bodies, and clean whitespace checks.

## Archive directories

Exactly seven directories were generated:

1. `2026-08-14-stream-capacity-rejection-is-circuit-neutral`
2. `2026-08-14-durable-max-delivery-occurrences`
3. `2026-08-14-normalize-agent-terminal-settlement`
4. `2026-08-14-beta161-user-response-current-truth`
5. `2026-08-14-reserve-typed-user-response-subjects`
6. `2026-08-14-beta161-post-g-current-truth`
7. `2026-08-14-post-g-tag-safety-closeout`

## Archive command transcript

| Step | Command | Result |
|---|---|---|
| 3 | `openspec archive -y stream-capacity-rejection-is-circuit-neutral` | PASS; one `nats-streaming` requirement merged |
| 4 | `openspec archive -y durable-max-delivery-occurrences` | PASS; four observability requirements added and one provisioning requirement added |
| 6 | `openspec archive -y normalize-agent-terminal-settlement` | PASS with the accepted one-task `[ ]` warning; eleven terminal requirements added |
| 8 | `openspec archive -y beta161-user-response-current-truth` | PASS with two pending review/archive-ledger tasks; five framework-only requirements added |
| 10 | `openspec archive -y --skip-specs reserve-typed-user-response-subjects` | PASS with seven historical unchecked tasks; no spec merge |
| 12 | `openspec archive -y beta161-post-g-current-truth` | PASS with two pending review/archive-ledger tasks; seven additions and seven modifications merged across seven capabilities |
| 15 | `openspec archive -y --skip-specs post-g-tag-safety-closeout` | PASS with ten historical candidate tasks; no spec merge |

Post-archive strict/diff results were green after every archive, subject only to the two transparently recorded,
reviewer-approved EOF corrections. Validation totals progressed through 49, 48, and final 48 items as active changes
were consumed.

## Post-move immutable verification

### #952 mapped checkpoint

Command executed immediately after step 10:

```bash
grep -v '^#' openspec/changes/archive/2026-08-14-reserve-typed-user-response-subjects/checkpoint.sha256 \
  | sed 's#  openspec/changes/reserve-typed-user-response-subjects/#  openspec/changes/archive/2026-08-14-reserve-typed-user-response-subjects/#' \
  | sha256sum -c -
```

Result: PASS, five repository-global and five mapped archive bodies reported `OK`.

### Post-G relative manifest

Command executed immediately after step 15:

```bash
(
  cd openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout
  sha256sum -c manifest.sha256
)
```

Result: PASS, all twelve bodies reported `OK`.

## Current Purpose truth

The five newly materialized capabilities contain the exact owner-accepted Purpose bodies:

- `max-delivery-observability`
- `agentic-terminal-events`
- `user-response-subject-ownership`
- `release-candidate-proof`
- `rule-action-observability`

No archive-generated `TBD` remains in those specs. Existing Purpose text in modified capabilities, including
`graph-embedding`, was preserved.

## Final gates

- `task openspec:queue`: PASS; only `semantic-tier-split` remains active at 0/31 and retains its existing blocker.
- `openspec list --changes`: only `semantic-tier-split`.
- Strict OpenSpec: PASS, 48/48.
- `git diff --check`: PASS.
- Archive count: exactly seven.
- `semantic-tier-split`: all three SHA-256 values match the pre-transaction baseline and its diff is empty.
- Scope audit: no Docker, integration, E2E, runtime, sister-repository, GitHub issue, commit, or push action occurred.

Independent canonical review of the exact uncommitted transaction remains required before merge.

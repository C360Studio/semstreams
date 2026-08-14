# Post-beta.160 repository truth checkpoint

**Checkpoint date:** 2026-08-13

**Repository baseline:** `421f450ecc8d395ffc5718133c87d2b51520cef1`

**Release candidate:** `8403a2218000e45a31c5132fbfe01af42ed04f14`

This checkpoint reconciles current repository truth after `v1.0.0-beta.160`. It does not alter the frozen post-G
inventory, its 155-issue snapshot, either checksum sidecar, the twelve-file candidate package manifest, or the
candidate-era evidence template and disposition ledger. Those artifacts remain historical evidence at their stated
baselines.

## Immutable beta.160 release result

All ten candidate and publication outcomes described by E.1 through E.10 of
`openspec/changes/post-g-tag-safety-closeout/tasks.md` are externally complete for candidate
`8403a2218000e45a31c5132fbfe01af42ed04f14`:

- both `candidate-proof-8403a2218000e45a31c5132fbfe01af42ed04f14` and `v1.0.0-beta.160` resolve to the candidate;
- the candidate-proof Release asset records a clean candidate, verification of the existing twelve-file manifest,
  every bound test and E2E gate green, semantic active polling, retained-path results, independent review, exact-SHA
  CI, the fresh-storage ruling, and tag authorization;
- the product Release attestation records successful binary and container publication, exact tag resolution,
  inclusion of the fresh-storage premise in Release notes, no destructive storage operation, and the final release
  decision.

Immutable records:

- Candidate-proof Release:
  `https://github.com/C360Studio/semstreams/releases/tag/candidate-proof-8403a2218000e45a31c5132fbfe01af42ed04f14`
- Candidate-proof asset SHA-256: `2db681d779fe118e8832d7cdb5f0944d5e17766dff3c64c2426b42c74b5b8bfe`
- Product Release: `https://github.com/C360Studio/semstreams/releases/tag/v1.0.0-beta.160`
- Product release-attestation asset SHA-256:
  `2b323e3f38bac654c4f600857a47a6d0bbe738f2c4a34083ee4e9cf76820febd`

The E.1-E.10 boxes remain unchanged in the candidate-era task file because it is covered by the verified package
manifest. Rewriting it after publication would destroy the historical checksum relationship. This checkpoint is the
post-publication current-state record; it does not carry old proof forward to authorize a different candidate.

## OpenSpec queue reconciliation

The baseline contained nine active changes. Four fully complete changes are archived through `openspec archive`,
with their durable requirements promoted to current specs:

- `durable-tool-call-outcomes` archives as `2026-08-13-durable-tool-call-outcomes`; it promotes `agentic-tools`,
  `framework-bucket-catalog`, and `nats-streaming` truth.
- `fix-top-level-entity-digest-labels` archives as `2026-08-13-fix-top-level-entity-digest-labels`; it promotes
  `graph-query` truth.
- `prove-slow-consumer-attribution-e2e` archives as `2026-08-13-prove-slow-consumer-attribution-e2e`; it promotes
  `nats-client-diagnostics` truth.
- `restore-websocket-output-path` archives as `2026-08-13-restore-websocket-output-path`; it promotes
  `websocket-output` truth.

Five change directories remain active for truthful, distinct reasons:

- `post-g-tag-safety-closeout`: beta.160 candidate and publication outcomes are externally complete. Its
  manifest-covered candidate-era task truth remains frozen and is reconciled above.
- `durable-max-delivery-occurrences`: implementation and recorded gates are complete. Independent SemStreams reviewer
  approval is not recorded.
- `stream-capacity-rejection-is-circuit-neutral`: implementation merged. Required independent review and
  integration-gate result provenance are not recorded.
- `normalize-agent-terminal-settlement`: SemStreams implementation/review is recorded. Actual semteams post-beta.160
  behavioral evidence is not recorded.
- `semantic-tier-split`: remains suspended and frozen. No task is completed by this checkpoint.

## Current issue-count checkpoint

The frozen post-G census remains a complete 155-open-issue snapshot from 2026-08-11. A fresh GitHub query on
2026-08-13 reports **128 open issues**. This is a count checkpoint, not a replacement row-by-row census and not an
authority to reclassify or close issues.

The current set includes new issue
[#963](https://github.com/C360Studio/semstreams/issues/963), `component: JetStreamPort max_ack_pending is accepted but
not honored by most consumers`. It was discovered after beta.160 at baseline `421f450e`; it remains a separate open
component-contract finding. It does not unfreeze `semantic-tier-split` or widen any surviving change in this
checkpoint.

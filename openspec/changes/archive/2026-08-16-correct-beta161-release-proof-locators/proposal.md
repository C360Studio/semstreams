## Why

The accepted beta.161 closeout moved the immutable post-G release package into the generated archive, but current
`release-candidate-proof` truth still names its removed active location, describes regeneration as if the package were
still mutable, and does not bind the lifecycle-required core E2E gate. A release operator following current truth
cannot literally resolve every frozen locator or produce a complete exact-candidate proof.

## What Changes

- Preserve every byte of `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/`; its manifest and covered
  package SHALL never be edited or regenerated.
- Correct current `release-candidate-proof` truth to the exact generated archive paths while retaining the frozen
  schema as historical evidence.
- Permit read-only manifest verification during or after archival, and require a fresh read-only reverification after
  the exact candidate SHA is selected.
- Require detached proof to translate all four former-active locators in the frozen schema: the manifest command, the
  disposition ledger, and both pre-tag and post-publication fresh-storage decision references.
- Add `task e2e:core` as a normative, provenance-complete exact-candidate row alongside mandatory semantic E2E and
  30–60 second semantic polling.
- Reject old-SHA, prior-worktree, and pre-selection results as authority for the corrected candidate.

## Non-goals

- No runtime, API, configuration, schema, payload, subject, bucket, Docker, or sister-repository change.
- No compatibility alias, restored active copy, generic archive resolver, or second proof authority.
- No weakening of retained gates, semantic polling, exact-SHA CI, independent review, fresh-storage, or publication
  attestation requirements.
- No edit, regeneration, replacement, or re-archive of the accepted post-G package.
- No P2 or P3 lifecycle implementation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-candidate-proof`: corrects immutable-package locators and temporal verification truth, explicitly translates
  the frozen template, and makes the beta.161 core E2E gate part of exact-candidate proof.

## Impact

Only OpenSpec release-governance truth changes. Release owners, technical-writer custodians, and independent reviewers
receive one literal archive location and one complete exact-candidate checklist. Downstream product adopters receive
no new runtime surface; their existing fresh-storage and post-publication duties remain unchanged. The corrected
commit must be selected and fully reproved as a new candidate.

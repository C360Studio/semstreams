## 1. Correct current release-proof truth

- [x] 1.1 Confirm the accepted inventory SHA-256 is
  `cc83357397c33330bc08e44da3cd06686c87525ab34316f9a464e199db61ad4a` and preserve its contents in the design
  verbatim.
- [x] 1.2 Materialize the bounded `release-candidate-proof` corrections: exact archive locators, verification-only
  archive handling, all four frozen-locator translations, and the normative `task e2e:core` proof row.
- [x] 1.3 State the temporal rule explicitly: read-only verification may occur during or after archival, and exact-
  candidate proof must reverify after candidate selection.
- [x] 1.4 Strictly validate the active correction and run `git diff --check`; touch no runtime, generated schema,
  archive byte, or sister repository.

## 2. Review and promote current truth

- [x] 2.1 Obtain independent `semstreams-reviewer` approval of the exact documentation/specification diff.
- [ ] 2.2 Archive this correction normally so `release-candidate-proof` becomes current truth; do not regenerate,
  edit, replace, or re-archive the accepted post-G package.
- [ ] 2.3 Verify that the promoted spec names the exact generated archive and contains no compatibility alias, restored
  active copy, old-SHA authority, or worktree-proof authority.
- [ ] 2.4 Merge only after required documentation gates and exact-SHA GitHub CI are green.

## 3. Select and prove the corrected candidate

- [ ] 3.1 After all in-tree corrections land, select one clean immutable merged SHA as a new candidate. Prior SHA,
  prior-worktree, pre-selection, and old candidate results are context only and do not authorize this candidate.
- [ ] 3.2 Reverify all twelve existing relative entries in the archived manifest from the generated archive directory;
  record exact command, runner, UTC start/end, result, and log SHA-256 without editing or regenerating the archive.
- [ ] 3.3 In detached proof, record each frozen former-active locator and its exact translation: manifest command,
  ledger path, candidate-proof fresh-storage decision reference, and product-attestation fresh-storage decision
  reference.
- [ ] 3.4 Run `task e2e:core` on the exact candidate and record a separate provenance-complete row with runner identity,
  UTC start/end, exit/result, and log or artifact SHA-256.
- [ ] 3.5 Run every other bound candidate-proof command on the exact candidate, including mandatory
  `task e2e:semantic`; poll semantic proof every 30–60 seconds with `/readyz`, authoritative counters, and stage
  timestamps, and abort a provably wedged run.
- [ ] 3.6 Record independent review and exact-SHA green GitHub CI, then seal the immutable candidate-proof asset only
  when every gate is green. Create no product tag from missing, red, old-SHA, or worktree-only evidence.
- [ ] 3.7 In the separate post-publication attestation, use the translated archived fresh-storage decision reference
  and preserve all existing product-tag, artifact, no-destructive-operation, and limitation fields.

# correct-beta161-release-proof-locators

Preserve the accepted post-G archive while correcting current beta.161 release-proof truth.

- `inventory.md` is the accepted repository-first inventory. Its accepted SHA-256 is
  `cc83357397c33330bc08e44da3cd06686c87525ab34316f9a464e199db61ad4a`.
- `design.md` reproduces that inventory verbatim, records Options A–D, and documents the owner-accepted Option B.
- The capability delta makes archive verification read-only, requires exact-candidate reverification after selection,
  translates all four frozen locators, and adds a provenance-complete `task e2e:core` row.
- The accepted archive is never edited or regenerated, and prior SHA or worktree results never authorize the new
  candidate.

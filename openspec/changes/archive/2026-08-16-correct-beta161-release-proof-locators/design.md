## Context

The accepted beta.161 closeout archived `post-g-tag-safety-closeout` with `--skip-specs` and immediately verified its
relative twelve-entry manifest from
`openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/`. The current `release-candidate-proof` capability was
materialized from the pre-archive companion and still points to the removed active directory. The archived evidence
schema retains the same pre-archive strings under its manifest and cannot be edited without destroying the evidence it
exists to preserve.

## Accepted inventory (verbatim)

Accepted SHA-256: `cc83357397c33330bc08e44da3cd06686c87525ab34316f9a464e199db61ad4a`.

The following is the complete accepted `inventory.md` content without modification:

## Mandatory surface inventory

Evidence boundary: `19f446bf6840a43ab4e0ea1e4b70abd176291e84`; `correct-beta161-release-proof-locators` is
untracked. All five active-change artifacts, the current capability spec, all twelve archived package bodies, the
closeout evidence, and the migration guide were read in full.

### 1. Claimed gap

- Current normative truth names the removed active ledger path at
  `openspec/specs/release-candidate-proof/spec.md:18-22`.
- Current truth binds an unqualified `candidate-evidence.md` at
  `openspec/specs/release-candidate-proof/spec.md:95-100`.
- Current manifest language still describes regeneration as final preparation and says verification SHALL occur only
  after candidate selection at `openspec/specs/release-candidate-proof/spec.md:52-72,81-87`.
- The former directory is absent; the archive exists:
  - `test -d openspec/changes/post-g-tag-safety-closeout` → exit 1.
  - `test -d openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout` → exit 0.
- Exactly one candidate-evidence schema, ledger, and package manifest exist, all under the archive. Closed search:
  - `rg --files . | rg '(^|/)candidate-evidence\.md$|(^|/)disposition-ledger\.md$|(^|/)manifest\.sha256$'`.

### 2. Every current spelling of the release-proof facts

Normative current truth:

- Stale active ledger locator: `openspec/specs/release-candidate-proof/spec.md:18-22`.
- Unqualified evidence-schema locator: `openspec/specs/release-candidate-proof/spec.md:95-100`.
- Generic regenerate-then-verify ordering, including the assertion that verification SHALL occur only after candidate
  selection: `openspec/specs/release-candidate-proof/spec.md:52-72,81-87`.

Frozen archived schema:

- Declares itself a template, not proof: `candidate-evidence.md:1-9`.
- Defines candidate selection before proof and separate candidate/product assets: `candidate-evidence.md:11-33`.
- Contains four former-active locators, not only the two inventoried by the active design:
  - Manifest command: `candidate-evidence.md:48`.
  - Ledger: `candidate-evidence.md:50`.
  - Fresh-storage decision reference: `candidate-evidence.md:99`.
  - Same post-publication reference: `candidate-evidence.md:125`.
- Command matrix and provenance fields: `candidate-evidence.md:52-77`.
- Semantic polling schema: `candidate-evidence.md:79-86`.
- Review, CI, fresh-storage and authorization fields: `candidate-evidence.md:88-106`.
- Product-attestation fields: `candidate-evidence.md:108-133`.

Other immutable former-active spellings:

- Archived post-G design: `design.md:16-19,384-389`.
- Archived post-G release delta: `specs/release-candidate-proof/spec.md:13-17,90-95`.
- Archived beta.161 current-truth companion:
  `openspec/changes/archive/2026-08-14-beta161-post-g-current-truth/design.md:9-12` and its release delta
  `:13-17,90-95`.
- Historical closeout inventory/design and post-beta.160 checkpoint also retain former-active references.
  Repository-wide closed enumeration:
  - `rg -l --hidden --glob='!.git/**' 'openspec/changes/post-g-tag-safety-closeout' .`
  - It returned eleven files; only `openspec/specs/release-candidate-proof/spec.md` is current normative capability
    truth. The others are archived or historical records.

Before this inventory was materialized, archive-locator spellings existed only in:

- `docs/proposals/beta161-openspec-closeout-evidence.md`.
- The active correction design and release delta.
- Closed search:
  - `rg -l --hidden --glob='!.git/**' 'openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout' .`

The same search in the exact materialized tree now returns those three files plus this evidence-only inventory.

### 3. Manifest temporal truth

- The frozen package says its manifest regeneration completed before candidate selection:
  - `proposal.md:3-7`.
  - `design.md:3-19,295-300`.
  - `tasks.md:111-134`.
- Closeout evidence records:
  - Active-tree verification 12/12 before archive: `beta161-openspec-closeout-evidence.md:38-50,103-107`.
  - Archive via `--skip-specs`: `:125-129,153-155`.
  - Immediate archive-relative verification 12/12: `:175-186`.
- The manifest contains twelve relative entries: `manifest.sha256:1-12`.
- Independent read-only verification now returned all twelve `OK`:
  - `(cd openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout && shasum -a 256 -c manifest.sha256)`.
- Archived candidate tasks already distinguish later verification from regeneration at `tasks.md:136-158`.
- Accepted evidence therefore directly contradicts current truth's assertion that the manifest SHALL only be verified
  after candidate selection: verification occurred in the active tree and immediately after archival, both before
  candidate selection.
- The current normative spec's regeneration wording describes the package's pre-archive preparation history, not an
  available operation on the now-archived package.

### 4. Exact-candidate core and semantic obligations

- The immutable candidate schema explicitly includes semantic E2E at `candidate-evidence.md:68` and semantic active
  polling at `:79-86`.
- `task e2e:core` is absent from:
  - Current `release-candidate-proof`.
  - Archived `candidate-evidence.md`.
  - Archived release delta.
  - Active correction release delta.
- Closed search:
  - `rg -n 'e2e:core' openspec/specs/release-candidate-proof/spec.md openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/candidate-evidence.md openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/specs/release-candidate-proof/spec.md openspec/changes/correct-beta161-release-proof-locators/specs/release-candidate-proof/spec.md`
  - Result: zero matches.
- The breaking lifecycle change separately requires both tiers:
  - `restore-go-lifecycle-ownership/proposal.md:8,66-71` marks the change BREAKING and requires both relevant E2E
    gates.
  - `restore-go-lifecycle-ownership/design.md:163-167`.
  - `restore-go-lifecycle-ownership/tasks.md:17-18,72-85`.
  - Migration guide `:325-348`.
- The repository hard rule requires relevant E2E before a breaking commit/tag and specifically guards the semantic
  production path: `AGENTS.md:205-227`, mirrored at `CLAUDE.md:246-268`.
- The published documentation index marks the caller-owned lifecycle migration as BREAKING:
  `docs/README.md:78-90`.
- Prior core 3/3 and semantic 48/48 evidence is explicitly worktree evidence, not exact breaking commit/tag authority:
  lifecycle tasks `:74-85`; migration guide `:340-348`.
- The locator correction's design and tasks recognize the supplemental requirement at `design.md:80-86` and
  `tasks.md:17-21`, but its release-spec delta does not add core to the bound candidate evidence schema.
- Adjacent CI commentary says statistical subsumes ordinary core coverage at `.github/workflows/e2e-ladder.yml:17-23`;
  the lifecycle change nevertheless names both tiers. No release-spec or active-delta text was found transferring the
  lifecycle core obligation to statistical.

### 5. Adjacent claims and consumers

- `release-candidate-proof` is the sole current normative capability owner.
- Archived package ownership:
  - Ledger binds owner decisions: `disposition-ledger.md:3-17`.
  - Candidate schema assigns release owner, technical-writer custody, and independent review:
    `candidate-evidence.md:6-9`.
- The current adopter-facing post-G operations guide names retained exact-candidate gates and the two evidence phases:
  `docs/operations/migration-post-g-tag-safety-closeout.md:3-5,53-64,83-94`.
- Historical beta.160 completion does not authorize another candidate:
  `docs/proposals/post-beta160-repository-truth-checkpoint.md:9-12,14-39`.
- No Go, TypeScript, Svelte, JSON, YAML, CLI, config, NATS, Docker, or sister-repository consumer was found. Closed
  search:
  - `rg -n 'post-g-tag-safety-closeout|candidate-proof-|candidate-evidence\.md|disposition-ledger\.md|manifest\.sha256' --glob='*.go' --glob='*.ts' --glob='*.svelte' --glob='*.json' --glob='*.yaml' --glob='*.yml' .`
  - Result: zero matches.
- No same-class collision table is triggered: the active change introduces no new durable, communication, or
  runtime-coordination primitive.

## Mandatory adopter-seam inventory

Specific adopters: the SemStreams release owner, technical-writer custodian, and independent reviewer.

1. What must they know?

- The sole live package is the generated archive directory.
- Its manifest is verification-only.
- The frozen schema's four former-active locators require explicit archive translation in detached proof.
- Semantic is in the bound schema, while lifecycle-required core currently exists only in adjacent change/task truth.

This is more than two correctness facts and is therefore an adopter-seam finding.

2. What happens if they do nothing?

- Following the current ledger locator or frozen manifest command fails loudly because the active directory does not
  exist.
- Following the unqualified `candidate-evidence.md` requires repository search rather than literal resolution.
- Omitting core is not equivalently loud: the archived schema and current release-proof capability can appear complete
  without a core row, while the lifecycle release task still requires it.

3. Where do they find out?

- Missing active paths: shell/path error.
- Archive transaction and 12/12 result: closeout evidence.
- Translation rule: unmerged active correction.
- Core obligation: lifecycle design/tasks and locator-correction task, not the bound candidate schema.

The split core obligation is currently discoverable only at documentation/task level.

4. What should they have to know?

- Ideally only one literal proof-package locator and one complete exact-candidate checklist.
- Product teams and component authors should know nothing about archive movement or manifest translation. Their
  existing fresh-storage and post-publication obligations remain unchanged.
## Goals / Non-Goals

**Goals:**

- Restore literal conformance between current release-proof truth and the accepted archive transaction.
- Preserve the archived package and manifest byte-for-byte.
- Translate all four frozen former-active locators explicitly and independently reviewably.
- Put lifecycle-required core E2E beside semantic E2E in normative exact-candidate proof.
- Permit harmless read-only verification during/after archive while requiring reverification after selection.
- Select and prove a new candidate after the specification correction.

**Non-Goals:**

- Reopen post-G dispositions, release limitations, or fresh-storage decisions.
- Carry old-SHA proof forward as exact-candidate evidence.
- Introduce a generic archive resolver, compatibility alias, or mutable proof registry.
- Edit, regenerate, replace, or re-archive any accepted post-G archive byte.

## Options

### Option A: Rewrite the archived package and regenerate its manifest

Edit the frozen schema to use archive paths, add a core row to that schema, and regenerate `manifest.sha256`.

- **Benefit:** The template would be self-contained at its present location.
- **Cost:** It destroys the byte identity and evidence meaning of the owner-accepted archive, rewrites historical task
  truth, and creates a different package without an authorized archive transaction.
- **Disposition:** Rejected. Archive immutability is a binding premise, not a convenience.

### Option B: Preserve the archive and correct current truth

Leave the archive byte-identical. Promote literal archive paths and temporal verification truth through a normal
OpenSpec delta. Require detached proof to translate all four frozen locators and append a provenance-complete
`task e2e:core` row alongside the frozen semantic row and active-polling record.

- **Benefit:** Preserves historical evidence, restores literal current truth, closes the quiet core-omission gap, and
  gives operators one normative checklist.
- **Cost:** Detached proof must record an explicit four-row translation and one supplemental command row; every newly
  selected candidate must run the full proof again.
- **Disposition:** Recommended and owner accepted.

### Option C: Restore the former active directory or add a compatibility alias

Copy, symlink, or resolve `openspec/changes/post-g-tag-safety-closeout` to the generated archive.

- **Benefit:** Frozen strings would appear to resolve without changing the template.
- **Cost:** Creates a second apparent authority, hides archive movement from reviewers, leaves manifest timing and core
  proof underspecified, and makes future resolution depend on non-normative repository mechanics.
- **Disposition:** Rejected. There must be one literal package authority.

### Option D: Do nothing and rely on operator inference

Treat archived history, closeout evidence, lifecycle tasks, and repository search as sufficient guidance.

- **Benefit:** No new OpenSpec change or proof cycle.
- **Cost:** Current normative truth remains false; three path uses fail or require inference, the post-publication
  reference stays stale, and core can be silently omitted from an otherwise complete-looking proof.
- **Disposition:** Rejected. Documentation/task discovery does not satisfy a release-proof correctness fact.

## Decisions

### Correct current truth through one normal OpenSpec delta

The change modifies only the three affected requirements in `release-candidate-proof`, then archives normally so the
current capability becomes authoritative before candidate selection. Directly editing the current spec would omit the
proposal, design, task, and merge history required for a binding correction.

### Preserve the original package and translate all four locators externally

The archived `candidate-evidence.md`, ledger, design, and manifest remain byte-identical. Detached evidence records
the frozen value and exact translated value for every occurrence:

| Frozen field | Frozen value | Translated value |
|---|---|---|
| Manifest verification command | `(cd openspec/changes/post-g-tag-safety-closeout && shasum -a 256 -c manifest.sha256)` | `(cd openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout && shasum -a 256 -c manifest.sha256)` |
| Disposition ledger | `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md` | `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/disposition-ledger.md` |
| Candidate-proof decision reference | `openspec/changes/post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise` | `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise` |
| Post-publication decision reference | `openspec/changes/post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise` | `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise` |

The candidate-proof asset carries the first three translated values. The separate product-Release attestation uses
the translated post-publication reference in its own field. The exact archive-path manifest result and log digest make
the translation reviewable without an alias or historical rewrite.

### Archive verification is read-only and temporally explicit

The accepted manifest exists only to verify its twelve relative bodies. Read-only verification may occur while the
package is archived or at any later time; the accepted closeout already contains both pre-selection cases. Neither
case is exact-candidate authority. After selection, candidate proof must freshly reverify the same entries from the
generated archive directory and record full provenance. The archive is never edited, regenerated, replaced, or
re-archived.

### Core is a normative detached exact-candidate row

The immutable template already binds `task e2e:semantic` and its polling record but predates the adjacent lifecycle
requirement for `task e2e:core`. Current truth therefore requires detached beta.161 proof to append a distinct core
row without editing the template. That row records exact command, runner identity, UTC start/end, exit/result, and log
or artifact SHA-256, exactly like every bound matrix row. Core does not replace semantic; semantic remains mandatory
with 30–60 second `/readyz`, authoritative-counter, and stage-timestamp polling.

### New candidate means new exact-SHA proof

This specification correction precedes candidate selection. The corrected merged commit becomes a new candidate and
receives manifest reverification, the full bound proof matrix (including core and semantic), exact-SHA CI, and
independent review. Old-SHA, pre-selection, and prior-worktree logs may explain history but never authorize the new
candidate, even though the correction contains no runtime code.

## Risks / Trade-offs

- **Another exact-candidate proof cycle costs wall time.** → Keep the correction documentation-only and run the bound
  gates once on the final merged SHA, with mandatory active polling for semantic work.
- **Editing the archive would invalidate the manifest.** → Prohibit archive edits and verify its existing digest.
- **A supplemental core row is not physically present in the frozen template.** → Make it a normative detached
  beta.161 row with the same provenance fields and independent review as bound rows.
- **A future archive move could stale another literal.** → This contract intentionally names the exact immutable
  package; moving it again is a specification correction, not an inferred alias.

## Migration Plan

1. Materialize and strictly validate this delta without touching the accepted archive.
2. Obtain independent SemStreams review and archive this correction normally to update current truth.
3. Merge through CI and select the clean merged commit as the new candidate.
4. Reverify the twelve-entry archive manifest and record all four frozen-to-archive translations.
5. Run the full exact-candidate matrix, including provenance-complete core and semantic rows and semantic polling.
6. Publish no proof or product tag unless the new candidate is fully green, exact-SHA CI is green, and independent
   review approves the exact candidate and evidence.
7. Use the translated post-publication fresh-storage reference in the separate product-Release attestation.

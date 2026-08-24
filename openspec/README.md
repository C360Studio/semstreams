# SemStreams OpenSpec

SemStreams uses [OpenSpec](https://github.com/Fission-AI/OpenSpec) for
spec-driven development: specs are the current truth per capability, and every
non-trivial or cross-cutting change is proposed as a delta against them before
code. The CLI and Claude Code skills are installed (`/opsx:new`, `/opsx:continue`,
`/opsx:apply`, `/opsx:archive`; `openspec list`, `openspec validate`).

SemStreams is the last `sem*` repo to adopt this — semspec, semteams (fullest
shape: `specs/` + `changes/`), and semsource already run it. We converge on the
same format so a change authored in one repo reads the same in the next.

## Layout

- `project.md` — standing project context: Purpose, **Product Boundary**, and
  conventions. Read this first when scoping anything.
- `config.yaml` — machine context injected into artifact creation, plus
  per-artifact rules. The human-readable source of truth is `project.md`.
- `specs/<capability>/spec.md` — **current truth** for a capability: `Requirement`
  + `GIVEN/WHEN/THEN` scenarios describing what it does *today*.
- `changes/<id>/proposal.md` — why the change exists, what changes (`## Why`,
  `## What Changes`, `## Non-goals`).
- `changes/<id>/tasks.md` — implementation checklist in dependency order.
- `changes/<id>/specs/<capability>/spec.md` — the **delta**: the target-state
  requirements this change adds/modifies/removes.
- `changes/archive/` — completed changes, moved here on `openspec archive`.

## Discipline (the reason we adopted this)

Specs-as-current-truth exist to kill documentation drift — design docs that
silently stop matching the code. Two rules make that real:

1. **Seed specs lazily.** Create a spec when a change first touches that
   capability, distilling what is still true from the code and existing docs. Do
   NOT backfill everything up front — pre-writing specs for areas that may change
   is dead work, and an unverified spec is just another drifting doc.
2. **Archive changes in the PR that completes them.** A change is `proposal →
   tasks → deltas → implement → review → owner cross-agent review → fixes/re-review
   → archive`, and the archive is the **final content commit of the landing PR**,
   never a follow-up PR. `openspec archive <id>` moves the change and promotes its
   durable requirements into the baseline `specs/`; a narrow final review checks
   that sync against the reviewed implementation before integration. Any correction
   after archive re-enters reconciliation and final review—no later content commit
   bypasses the archive/spec-sync check. The merge under the branch ruleset
   (required checks, no bypass) is the proof that the archived state is CI-green —
   an archive cannot reach `main` any other way, so nothing has to be assumed.
   Where a change lands over several PRs, the last one archives. Do not let
   completed or abandoned changes accumulate as ambient "Proposed" documents — that
   status ambiguity is its own drift problem.

   Corollary: **no task may assert a post-merge fact.** "CI green", "merged",
   "merge-ready", "hosted CI approval obtained" cannot be ticked before the merge,
   so such a task strands the change unarchived. Write tasks that are checkable on
   the branch — "PR #n open with `Closes #n`", "reviewer verdict recorded in
   `conformance.md`", "focused gates run: <commands + results>" — and let the merge
   gate own CI. CI runs `openspec validate --all --strict` on every PR.

OpenSpec changes are contract deltas, not program backlogs. Keep sequencing,
discovery, and proof campaigns in issues; split a change when it crosses distinct
owners or independently reviewable behavior. If implementation changes the target,
reconcile or supersede the change immediately instead of appending another wave.
See [OpenSpec change discipline](../docs/contributing/06-openspec-change-discipline.md).

## Relationship to `docs/`

- `docs/adr/` — genuine **decisions** only (irreversible choices, cross-repo
  contracts). History. See `docs/adr/README.md`.
- `docs/0X-*.md` — retired **gradually**: "how it works" content migrates into
  `specs/` as each area is touched; getting-started, operations, and runbook
  content stays as docs.

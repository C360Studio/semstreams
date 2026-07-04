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
2. **Archive changes on completion.** A change is `proposal → tasks → deltas →
   implement → archive`. Do not let completed or abandoned changes accumulate as
   ambient "Proposed" documents — that status ambiguity is its own drift problem.
   On archive, durable requirements are promoted into the baseline `specs/`.

## Relationship to `docs/`

- `docs/adr/` — genuine **decisions** only (irreversible choices, cross-repo
  contracts). History. See `docs/adr/README.md`.
- `docs/0X-*.md` — retired **gradually**: "how it works" content migrates into
  `specs/` as each area is touched; getting-started, operations, and runbook
  content stays as docs.

# ADR-069: Adopt OpenSpec; ADRs become pure decision records

## Status

**Accepted — 2026-07-04.** Process/convention decision, effective beta.132+.
This ADR is itself an example of the new rule: a genuine, cross-cutting,
hard-to-reverse choice recorded once as history. Its *mechanics* (layout,
lazy-seeding, archive discipline) live in `openspec/project.md`,
`openspec/README.md`, and `docs/adr/README.md`, which stay current.

## Context

SemStreams was the last `sem*` repo not on OpenSpec — semspec, semteams (fullest
shape: `specs/` + `changes/`), and semsource already run it, and semspec runs its
own migrations *as* OpenSpec changes today. The recurring failure OpenSpec fixes
is **documentation drift**: `docs/0X-*.md` design docs that silently stop matching
the code, with no mechanism forcing them to track reality (the family's audits
kept having to invent an "implemented vs config-gated vs doc-only" taxonomy just
to read a repo — a taxonomy that should not need to exist).

ADRs were never the drift problem — an ADR is history, and history doesn't drift.
The problem is design docs *pretending* to be current truth. So the fix is two
distinct homes: a place for current truth that is forced to track reality (specs),
and a place for decisions that is never updated (ADRs).

## Decision

1. **Adopt OpenSpec** (CLI + Claude Code `.claude/` skills; `schema: spec-driven`).
   Non-trivial or cross-cutting work starts as a change (`proposal.md` + `tasks.md`
   + spec deltas) before code.
2. **`openspec/specs/<capability>/spec.md` is current truth**, seeded **lazily**
   (when a change first touches a capability) and **verified against code** — never
   backfilled.
3. **`openspec/changes/<id>/` holds proposed target state as deltas**, archived on
   completion — not left to accumulate as ambient "Proposed" documents.
4. **ADRs are pure decision records** going forward: irreversible choices and
   cross-repo contracts only (the *why*). The *how* an ADR implies lives in the
   capability spec. Existing ADRs 001–068 are preserved untouched as history.
5. **`docs/0X-*.md` retire gradually** — "how it works" migrates into specs as each
   area is touched; getting-started / operations / runbooks stay as docs.
6. **`openspec/project.md` carries the Purpose and Product Boundary** (semsource's
   convention): SemStreams owns substrate/primitives, not product domain semantics.

## Consequences

- Current-truth documentation now has a drift-killing mechanism (delta-per-change);
  the "how do I even read this repo" taxonomy is retired.
- Convergence with the rest of the `sem*` family: a change reads the same across
  repos, and semstreams' own history becomes agent-consumable in the format the
  family's tooling already reads.
- New surface to maintain: `openspec/` and the seeding/archive discipline. The risk
  is a spec written once and left to rot — mitigated by lazy seeding (only write a
  spec you will keep current) and verify-against-code.
- No code change and no retroactive rework; adoption is additive.

## References

- `openspec/project.md`, `openspec/README.md`, `docs/adr/README.md`,
  `CLAUDE.md` → "Spec-driven development (OpenSpec)".
- Family precedent: semteams (`specs/` + `changes/` + `changes/archive/`),
  semsource (`project.md` Product Boundary; changes for large migrations), semspec
  (migrations as OpenSpec changes).
- OpenSpec — https://github.com/Fission-AI/OpenSpec.

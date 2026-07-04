# Architecture Decision Records

## What an ADR is now (post-OpenSpec, beta.132+)

An ADR is a **pure decision record**: the *why* behind an irreversible choice or
a cross-repo contract, captured at the moment it was decided. ADRs are **history**
— and history doesn't drift. That is exactly their value.

An ADR is **not** current-truth documentation. How a capability behaves *today*
lives in its OpenSpec spec (`openspec/specs/<capability>/spec.md`), which is kept
current as the code changes. The mechanics an ADR implies are described there, not
in the ADR.

The split, with a worked example:

- **Decision → ADR.** "We adopt the two-loop principle." One page: the choice, the
  forces, the alternatives rejected, the consequences. Written once, never
  updated (a superseding decision is a *new* ADR).
- **Mechanics → spec.** *How* the two loops interact — the states, the
  transitions, the guarantees — lives in the capability spec, which tracks reality
  because every change edits it via a delta.

Write an ADR only when the thing being recorded is:

1. **Irreversible or expensive to reverse** — a data model, a wire contract, a
   single-writer invariant, a retirement (a package removed, a registry
   collapsed).
2. **A cross-repo contract** — a mutation-API shape, a payload envelope, a
   readiness signal, a vocabulary predicate that another `sem*` product depends on.

If it is neither — if it is "how X works today" or "the plan for Y" — it is a
**spec** (current truth) or an OpenSpec **change** (proposed target state), not an
ADR. Reach for `/opsx:new`.

## Existing ADRs (001–068) stay as history

Every ADR already in this directory is preserved untouched. Many predate OpenSpec
and mix decision with mechanics; that is fine — they are the record of what was
decided when. As a capability's spec gets seeded (lazily, when a change first
touches it), the *current-truth* half of an old ADR migrates into that spec, and
the ADR remains as the decision's provenance. Do not retrofit old ADRs.

## Status discipline

An ADR is `Proposed`, `Accepted`, or `Superseded by ADR-NNN`. A design that is
still being shaped is an OpenSpec **change** (which archives on completion), not a
long-lived `Proposed` ADR — ambient `Proposed` documents are their own drift
problem. A `Proposed` ADR is a short-lived state on the way to `Accepted` in the
same or the next PR, or it should have been a change.

## See also

- `openspec/project.md` — Purpose, Product Boundary, the full role split.
- `openspec/README.md` — the OpenSpec layout and lazy-seeding / archive discipline.
- `CLAUDE.md` → "Spec-driven development (OpenSpec)".

---
name: resume
description: Continue from the last session — reconcile measured repo state against the baton, report drift, and pick up the Next Action. Use at the start of any session on the pre-v1 program, especially after /clear.
---

# Resume the program from measured state

The baton (`docs/proposals/prev1-program.md`) carries program state across sessions, but
close-outs are written by sessions and sessions have been wrong before (session 18: five
claims written from intent, all disprovable by the commands below). **Reconcile from
measurement, never from prose.**

## Steps

1. **Read the baton**: the Next Action block, the session protocol, and the standing rules.
   Note any HOLDs, gates, and the model-roles rule.
2. **Measure, in one pass**:
   - `git -C . log --oneline -5` and `git status --porcelain` (whose tree is this? another
     agent's uncommitted work = hands off, use a worktree)
   - `git worktree list` (other agents' active trees — never touch them)
   - `gh pr list` (armed auto-merges, pending reviews)
   - `gh issue list --limit 15` (queue movement since the baton was written)
   - `openspec list` (change queue vs the baton's claims)
3. **Diff measurement against the baton.** Every mismatch is either the baton being stale
   (fix it — make the claim true or correct it, auditable) or work that happened untracked
   (record it). Do not proceed on top of unexplained drift.
4. **Report compactly**: current state in one paragraph, the Next Action, any drift found
   and how it was resolved, and anything armed/in-flight that will land on its own.
5. **Route by model role** (baton session protocol): execution → proceed; epic planning,
   change-proposal/design review, or critical-stage pre-merge review (breaking, boot-path,
   durability/ack semantics, cross-plane ownership, new framework surface) → that is Fable
   work; flag it rather than doing it in an execution session.

## Rules that bind here

- The baton wins over memory files wherever they disagree; measurement wins over both.
- A stalled change (>7 days) found in step 2 triggers the staleness tripwire: explain it in
  the baton or rescope it.
- End-of-session: update the baton FROM the same commands, never from the session's
  internal model — state files carry measurements, not predictions.

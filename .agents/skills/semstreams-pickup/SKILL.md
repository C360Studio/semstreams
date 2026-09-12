---
name: semstreams-pickup
description: >-
  Resume a named SemStreams PR or issue by verifying live worktree, GitHub, OpenSpec, and test evidence.
  Use for a fresh session, explicit handoff, or resumed context needing reconciliation.
---

# Pick up SemStreams work

Read [the shared protocol](../../protocol.md) first. Treat the named effort and any
[handoff prompt](../semstreams-handoff/SKILL.md) as addresses to inspect, not proof of current state.
No handoff block or private memory file is required. Preserve the user's objective, constraints, and authorization.

## Locate and measure

Find the live PR, base/head branches, issue decisions, milestone, OpenSpec change, and dedicated worktree. Inspect
branch, HEAD, status, upstream divergence, and worktree list before any mutation. Read the PR's actual changed files
and stop point; a title or summary alone is not enough to resume implementation.

Run `task openspec:queue` from the claim's worktree and read its tasks and holds. The primary checkout is
discovery-only; its queue does not enumerate unmerged changes elsewhere. Check current main and PR CI, known
failures or waivers, and the SHA each result belongs to. A GitHub connector may replace `gh`. Report unavailable
reads as unavailable; do not interpret them as empty queues, absent claims, or passing checks.

These local checks are useful once inside the verified worktree:

```bash
git branch --show-current
git rev-parse HEAD
git status --short
git worktree list
git rev-parse --abbrev-ref --symbolic-full-name '@{u}'
git log --oneline '@{u}..HEAD'
git log --oneline 'HEAD..@{u}'
task openspec:queue
```

An absent upstream is a fact to report, not permission to invent or reset one. Do not automatically checkout,
rebase, stash, reset, clean, cherry-pick, or create a competing claim to make reality match the handoff.

## Establish write ownership

A PR claim, matching agent prefix, or quiet terminal does not show that another session has stopped writing.
Follow a recorded or explicit transfer and verify the intended worktree and release by the previous writer.
If ownership is uncertain, continue read-only investigation and resolve it before editing. Do not seize an active
worktree or move another task as a side effect of pickup.

For unclaimed work, follow the protocol's Take ritual within the user's authorization. Preserve recorded holds.
Resuming context does not waive review, owner decisions, test gates, or publication boundaries. Approval of adjacent
work does not authorize closing this issue.

## Reconcile and continue

Commands establish current machine/git state; GitHub, OpenSpec, and ADRs establish their declared shared facts;
transcripts establish what happened and what the user authorized. An old or compressed summary does not silently
supersede them. Explain discrepancies and correct the appropriate record only when established and authorized.
Do not erase history, infer completion, or relabel a reviewer recommendation as an owner decision.

Preserve unfinished local work. Distinguish uncommitted edits, unpushed commits, and remote commits missing locally.
An unpushed commit makes the checkpoint local; it does not erase an existing draft-PR claim. Verify access to local
evidence. Missing artifacts and historical passes remain missing or historical until re-proven.

Do not rerun every expensive check because the session is new. Run necessary remaining validation through canonical
tasks and respect the shared-host lock. Do not duplicate a paid or background run already in flight.

Report the verified PR/worktree/head, material drift, holds, write owner, usable evidence, and next authorized step
in one short update. Continue when no unresolved dependent gate applies. Automatic compaction alone creates no new
claim and requires no new task. Use [semstreams-handoff](../semstreams-handoff/SKILL.md) at the next useful checkpoint
or requested transfer.

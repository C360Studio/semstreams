---
name: semstreams-handoff
description: >-
  Checkpoint a SemStreams PR or issue for continued work or transfer between Codex and Claude sessions.
  Reconcile shared records and return a short pickup prompt at a meaningful work boundary.
---

# Checkpoint SemStreams work

Read [the shared protocol](../../protocol.md) first. It owns claims, state locations, filing, and landing gates.
Use this workflow within the user's authorized effort. It grants no extra publication, merge, closure, or
cross-repository authority. Preserve authorization already given without asking for it again.

## Reconcile the checkpoint

Identify the effort by PR or issue. Verify its worktree path, branch, HEAD, upstream divergence, and dirty state.
Read the current PR, applicable issue decisions, and OpenSpec change. Run `task openspec:queue` in the claim's
worktree: an empty queue in the primary checkout does not describe unmerged work elsewhere. Use the available
GitHub connector or `gh`; command spelling is not the authority.

Record shared facts in the protocol's existing homes: the PR stop point, OpenSpec tasks and holds, issue decisions
or ADRs, and milestone membership. Re-read a shared record before updating it and preserve other authors' content.
Do not turn a reviewer recommendation or inferred decision into an owner ruling. Do not create a separate prose
handoff document competing with those homes.

Keep test evidence attached to the revision it tested: command, result, timestamp or run link, and artifact path.
Identify tests on a dirty tree and the snapshot or diff needed to reproduce them. Distinguish failed, skipped, and
unrun checks. Historical green does not prove later edits. Run inexpensive state checks now; do not repeat expensive
suites solely to write a handoff or describe a previous result as freshly verified.

Record pending commands, approvals, automations, and background jobs with their identifiers and owners, including
whether they will keep running. Do not silently cancel or duplicate another session's work.

## Preserve work and ownership

When a push is already authorized and applicable gates pass, verify the branch still matches the PR head and
publish the checkpoint through the protocol. Do not force a push, weaken a gate, or include someone else's edits
to satisfy handoff wording. If publication is blocked, preserve local work and report what is unpushed or uncommitted,
why, and where it lives. The receiver must verify access before relying on a local-only checkpoint.

For a transfer, identify the writer relinquishing ownership and the intended receiver. A draft PR identifies the
effort; a matching agent prefix does not show that its worktree is idle. The receiver may inspect but must not edit
until the old writer has stopped or released ownership.

## Return the pickup prompt

Use these five fields in the conversation. Include useful addresses, constraints, and checks; write `none` for an
empty field. Shared decisions, holds, and ordering are pointers to their homes. Machine-local details belong here
only when the receiver cannot obtain them from shared state.

```text
Goal: PR/issue and the authorized outcome.
Addresses: PR, change ID, worktree, branch, checkpoint SHA, and evidence locations.
Constraints: Decision/hold pointers, local access limits, and write-ownership transfer status.
Done when: Existing acceptance criteria and the remaining boundary.
Checks to run: Cheap reconciliation commands; necessary unrun validation distinguished from prior evidence.
```

End with a request to use [semstreams-pickup](../semstreams-pickup/SKILL.md) for the named effort. A Codex task link or
Claude transcript may supplement these addresses; neither replaces current GitHub, OpenSpec, or command evidence.
A private memory file is optional and cannot be required for another agent.

## Platform behavior

In Codex desktop, invoke `$semstreams-handoff` or ask to checkpoint the effort. In Claude, use the repository's
`semstreams-handoff` adapter. The names deliberately differ from personal `handoff` and `pickup` skills.

Keep auto-compaction enabled if that is the user's preference. Checkpoint after material decisions, review rounds,
or verified work slices. Do not impose context-size thresholds, require a fresh task at every compaction, or promise
an automatic pre-compaction hook. A checkpoint need not end the task; continue unless a transfer or real hold applies.

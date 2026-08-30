# Shared work protocol (Claude and Codex)

Canonical. `CLAUDE.md` and `AGENTS.md` carry a pointer to this file plus the three gates (claim, merge, close) inline;
edit the protocol here only. Read it before taking, landing, or closing work; the `pickup` and `handoff` rituals read it.

State that both agents must see lives in the repository's tools — never in a prose document or either agent's private
memory. Each question has one home, and each home is a `gh` or `task` query.

| Question | Home | Rule |
|---|---|---|
| What is wanted, what kind, is it decided | GitHub issue + labels (`type:` / `area:` / `class:` / `status:` / `horizon:`) | `status:needs-decision` is the owner's docket; a ruling is posted as an issue comment and the label removed. `status:blocked` names its blocker in a comment. |
| What gates the next tag | GitHub milestone named for the intended version | Membership is the gate: in or out; an unruled item is out. `horizon:pre-v1` means before v1.0.0, not before the next tag. |
| An epic | A tracking issue labeled `type:epic` whose body carries a task list of `#n` children | GitHub renders the progress; there is no separate epic document. |
| Who has claimed what | A **draft PR** opened at the start of the work, `Closes #n` in its body; the branch prefix names the agent (`claude/…`, `codex/…`) | No draft PR, no claim. Design-phase work claims the same way — the OpenSpec proposal is its first commit. A stop-point goes in the PR description. |
| Target state and task truth | The OpenSpec change inside that PR; `task openspec:queue` reads its holds | The archive (`openspec archive <id>` + spec sync) is the landing PR's last commit, reviewed with the code; the ruleset-enforced merge is the CI-green proof. No task may assert a post-merge fact ("CI green", "merge-ready") — such a task strands the change. |
| Why | An ADR, or the owner's ruling comment on the issue | — |

Rituals:

- **Start:** `gh issue list --milestone <m> --state open` · `gh pr list` (drafts are claims — skip them) ·
  `task openspec:queue` · `gh run list --branch main --limit 3` · `gh issue list --label status:needs-decision`.
- **Take work:** an unclaimed milestone issue → dedicated worktree on an agent-prefixed branch → push → draft PR with
  `Closes #n` → then work. One claimed PR owns one worktree. When multiple agents share a host, the primary checkout is
  discovery-only; no agent commits from it. Immediately before every commit and push, verify that the worktree's
  current branch is the draft PR head; a mismatch stops the operation.
- **Worktree hygiene:** the claim's worktree lives at a durable sibling path — `git worktree add ../semstreams-wt/<branch>
  -b <branch> origin/main` — never under `/private/tmp`, which a reboot purges (22 dead entries were pruned on
  2026-08-25); `git worktree remove` it when the PR merges. Heavy local gates (the full integration suite, an e2e tier)
  run one agent at a time on a shared host: worktrees fix the git collision, not the CPU one (#736). CI is the arbiter;
  a local red under contention is not a finding.
- **Land:** implementation review → the owner-run cross-agent round where the owner asks for it → fixes and re-review →
  archive as the final content commit → narrow reviewer check of the archive/spec sync → undraft → CI green with
  **no known unfixed flake in a required job** (a fresh green over a known flake is rerun-to-green: fix it, or file it
  and obtain an explicit owner waiver recorded as a PR comment) → squash merge closes the issue. A correction after
  archive re-enters reconciliation and final review; no later content commit bypasses the archive/spec-sync check.
  State `implemented-by: <persona>` in the PR body; Codex uses `Sol`.
- **Close:** no issue closes without the owner's explicit `CONFIRM-CLOSE` visible in the issue or PR. A chat-only signal
  is not shared durable state and does not authorize a `Closes #n` merge. The signal MUST name the issues it closes and
  covers only those. An approval of adjacent work — a PR, a review round, a design, a waiver — never widens into a
  close, and a bare "approved" is never read as `CONFIRM-CLOSE` (owner ruling, 2026-08-29: "stick with the confirm
  close requirement so we can all ensure that an 'approved' does not drift into too wide a condition"). A PR carrying
  `Closes #a` and `Closes #b` needs the confirm for both, named. Transcribing the owner's own words into the issue or
  PR is recording; inferring the gate from an adjacent approval is not, and is the failure this rule exists to stop.
- **Tag:** milestone at 100% → candidate selection per `openspec/specs/release-candidate-proof/spec.md`. The
  milestone never names the candidate SHA.

There is no program baton document; `docs/proposals/*-program.md` files are retired history.


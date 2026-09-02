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
| Who has claimed what | A **draft PR** opened at the start of the work, `Closes #n` in its body; the branch prefix names the agent (`claude/…`, `codex/…`) | No draft PR, no claim. Design-phase work claims the same way — the OpenSpec proposal is its first commit. Expect that first push to fail CI on the Lint job's last step, `Validate OpenSpec changes and specs (strict)`: a change with no delta is refused, and the red clears when the first delta lands — that is the one expected red on a claim push; any other red on that run is real. A stop-point goes in the PR description. |
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
- **Close:** the squash-merge of a PR that declared `Closes #n` in its body at review time closes the issue — the
  merge IS the authorization; no separate confirm is required (owner ruling, 2026-08-31, recorded on #1198,
  superseding 2026-08-29: "if an issue is tied to a PR (which it should be) and we follow the PR process there is
  really no reason to need me to CONFIRM-CLOSE"). The declaration must predate the review rounds that cover it:
  adding `Closes #n` to a PR after its reviews ran re-enters review, because the reviews must have covered the claim
  the merge authorizes. What survives from 2026-08-29, the half that caught real drift: a close with NO merged PR
  behind it — duplicate, stale, fixed-elsewhere — takes the owner's word on the issue itself; an approval of
  adjacent work — a PR, a review round, a design, a waiver — never widens into a close; and a bare "approved" closes
  nothing. Transcribing the owner's own words is recording; inferring a close from an adjacent approval is the
  failure the surviving half exists to stop.
- **Tag:** milestone at 100% → candidate selection per `openspec/specs/release-candidate-proof/spec.md`. The
  milestone never names the candidate SHA.

There is no program baton document; `docs/proposals/*-program.md` files are retired history.

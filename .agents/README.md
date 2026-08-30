# SemStreams Agent Profiles

The files in `contracts/` are the tracked, platform-neutral behavioral authority. Platform adapters are intentionally
thin and must point to exactly one canonical contract.

## Role and platform mapping

- SemStreams architect
  - Canonical: `.agents/contracts/semstreams-architect.md`
  - Claude: `.claude/agents/semstreams-architect.md`
  - Codex: `.codex/agents/semstreams-architect.toml`
- SemStreams developer
  - Canonical: `.agents/contracts/semstreams-developer.md`
  - Claude: `.claude/agents/semstreams-developer.md`
  - Codex: `.codex/agents/semstreams-developer.toml`
- SemStreams reviewer
  - Canonical: `.agents/contracts/semstreams-reviewer.md`
  - Claude: `.claude/agents/semstreams-reviewer.md`
  - Codex: `.codex/agents/semstreams-reviewer.toml`
- SemStreams explorer (enumerate-only; cheap model; writes the inventory file the architect may start from)
  - Canonical: `.agents/contracts/semstreams-explorer.md`
  - Claude: `.claude/agents/semstreams-explorer.md`
  - Codex: `.codex/agents/semstreams-explorer.toml`

## Shared work protocol

`.agents/protocol.md` is the canonical shared work protocol (state homes, rituals, worktree hygiene). `CLAUDE.md` and
`AGENTS.md` carry a pointer plus the three gates — claim, merge, close — inline; edit the protocol only in
`.agents/protocol.md`.

## Shared decision skills

The files in `skills/` are the tracked, platform-neutral canonical instructions for the five shared decision
heuristics. The `.claude/skills/` entries of the same names are thin adapters (frontmatter for Claude discovery +
a one-line pointer); Codex reads the canonical SKILL.md paths directly via `AGENTS.md`.

- `.agents/skills/entity-or-bucket/SKILL.md` — graph entity triples vs private/operational KV
- `.agents/skills/kv-or-stream/SKILL.md` — KV Watch vs JetStream Stream (4-test heuristic)
- `.agents/skills/new-payload/SKILL.md` — payload-registry checklist
- `.agents/skills/orchestration-check/SKILL.md` — rule vs component vs lifecycle boundary
- `.agents/skills/query-pattern/SKILL.md` — GraphQL vs MCP vs NATS Direct

All other `.claude/skills/` entries (openspec workflow, preflight, e2e-doctor, tag-release, semstreams-dev, …) are
Claude-workflow tooling and remain platform-specific by design — do not mirror them.

## Manual read-only parity smoke

Run this procedure after changing a contract, adapter, or repository routing rule. It only reads tracked files.

1. Confirm all four canonical contracts and all eight adapters exist, and that `.agents/protocol.md` exists.
2. Confirm each adapter names exactly its matching `.agents/contracts/...` path and says to read it fully first.
3. Confirm the Claude reviewer and architect tool lists contain `Read`, `Bash`, `Grep`, `Glob`, `Skill`, and `LSP`,
   but not `Edit`, `Write`, `Task`, or another delegation tool; the explorer adds `Write` (the inventory file only)
   and no delegation tool.
4. Confirm the Codex reviewer and architect set `sandbox_mode = "read-only"`; the developer has no sandbox override
   and therefore inherits the parent workspace permissions.
5. Confirm `AGENTS.md` and `CLAUDE.md` route the same logical roles.
6. Inspect adapter size with `wc -l .claude/agents/semstreams-*.md .codex/agents/semstreams-*.toml`; adapters should
   remain short and contain no copied checklist.
7. Confirm each of the five shared-skill adapters in
   `.claude/skills/{entity-or-bucket,kv-or-stream,new-payload,orchestration-check,query-pattern}/SKILL.md`
   names exactly its matching `.agents/skills/...` path, says to read it fully first, and contains no copied body
   (`wc -l` ≈ 8). Confirm `AGENTS.md` lists the same five canonical skill paths.

Use these semantic fixtures when reading the routing text:

- "Implement a nontrivial graph-index change" routes first to SemStreams developer and then SemStreams reviewer.
- "Review a nontrivial SemStreams change" routes to SemStreams reviewer in read-only mode.
- "Design an API contract or OpenSpec target" routes to SemStreams architect (surface inventory first, drafts as
  text); binding rulings and approval stay with the owner session.
- "Update durable docs or reconcile task truth" remains technical-writer-owned.
- "Enumerate what is on this surface for a change" routes to SemStreams explorer (inventory file with every search
  recorded); the architect may start from that file and the reviewer re-derives it independently.
- "Check an isolated Go idiom" may use a generic Go agent only as a second pass.

The smoke passes only when Claude and Codex resolve the same logical role and canonical contract for every fixture.

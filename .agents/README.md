# SemStreams Agent Profiles

The files in `contracts/` are the tracked, platform-neutral behavioral authority. Platform adapters are intentionally
thin and must point to exactly one canonical contract.

## Role and platform mapping

- SemStreams developer
  - Canonical: `.agents/contracts/semstreams-developer.md`
  - Claude: `.claude/agents/semstreams-developer.md`
  - Codex: `.codex/agents/semstreams-developer.toml`
- SemStreams reviewer
  - Canonical: `.agents/contracts/semstreams-reviewer.md`
  - Claude: `.claude/agents/semstreams-reviewer.md`
  - Codex: `.codex/agents/semstreams-reviewer.toml`

The existing `.claude/skills/semstreams-dev` skill is a narrower component workflow and remains independent. Skill
parity or migration is outside this profile change.

## Manual read-only parity smoke

Run this procedure after changing a contract, adapter, or repository routing rule. It only reads tracked files.

1. Confirm both canonical contracts and all four adapters exist.
2. Confirm each adapter names exactly its matching `.agents/contracts/...` path and says to read it fully first.
3. Confirm the Claude reviewer tool list contains `Read`, `Bash`, `Grep`, `Glob`, and `Skill`, but not `Edit`, `Write`,
   `Task`, or another delegation tool.
4. Confirm the Codex reviewer sets `sandbox_mode = "read-only"`; the developer has no sandbox override and therefore
   inherits the parent workspace permissions.
5. Confirm `AGENTS.md` and `CLAUDE.md` route the same logical roles.
6. Inspect adapter size with `wc -l .claude/agents/semstreams-*.md .codex/agents/semstreams-*.toml`; adapters should
   remain short and contain no copied checklist.

Use these semantic fixtures when reading the routing text:

- "Implement a nontrivial graph-index change" routes first to SemStreams developer and then SemStreams reviewer.
- "Review a nontrivial SemStreams change" routes to SemStreams reviewer in read-only mode.
- "Design an API contract or OpenSpec target" remains architect-owned.
- "Update durable docs or reconcile task truth" remains technical-writer-owned.
- "Check an isolated Go idiom" may use a generic Go agent only as a second pass.

The smoke passes only when Claude and Codex resolve the same logical role and canonical contract for every fixture.

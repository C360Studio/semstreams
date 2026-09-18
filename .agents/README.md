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
- SemStreams judge (one bounded question over collected evidence; read-only; the one role pinned to Fable)
  - Canonical: `.agents/contracts/semstreams-judge.md`
  - Claude: `.claude/agents/semstreams-judge.md`
  - Codex: `.codex/agents/semstreams-judge.toml`

## Shared work protocol

`.agents/protocol.md` is the canonical shared work protocol (state homes, rituals, worktree hygiene). `CLAUDE.md` and
`AGENTS.md` carry a pointer plus the three gates — claim, merge, close — inline; edit the protocol only in
`.agents/protocol.md`.

## Shared skills

Canonical skills live in `skills/`. Read the relevant `SKILL.md` fully; the platform adapters add discovery
metadata and argument handling, not another rule set. Repository protocol and role contracts remain authoritative.

| Canonical skill | Purpose | Claude command |
| --- | --- | --- |
| [entity-or-bucket](skills/entity-or-bucket/SKILL.md) | Graph triples vs private/operational KV | `/entity-or-bucket` |
| [kv-or-stream](skills/kv-or-stream/SKILL.md) | Facts via KV Watch vs work via JetStream | `/kv-or-stream` |
| [new-payload](skills/new-payload/SKILL.md) | Explicit payload registration and wire serialization | `/new-payload` |
| [orchestration-check](skills/orchestration-check/SKILL.md) | Rule vs component vs lifecycle boundary | `/orchestration-check` |
| [query-pattern](skills/query-pattern/SKILL.md) | Admitted remote operation vs named typed adapter; MCP graph access unavailable | `/query-pattern` |
| [semstreams-dev](skills/semstreams-dev/SKILL.md) | Development contracts, existing patterns and production-path proof | `/semstreams-dev` |
| [semstreams-preflight](skills/semstreams-preflight/SKILL.md) | Scope existing verification gates and record their evidence | `/preflight` |
| [semstreams-handoff](skills/semstreams-handoff/SKILL.md) | Publish shared state and preserve unfinished work | `/semstreams-handoff` |
| [semstreams-pickup](skills/semstreams-pickup/SKILL.md) | Reconcile current state and verify worktree ownership | `/semstreams-pickup` |

Codex can invoke the canonical names with `$`, or read their paths directly. Claude uses the command names above;
its adapters live in `.claude/skills/<command-name>/SKILL.md`. The `preflight` adapter deliberately maps to the
canonical `semstreams-preflight` name.

The remaining Claude tools (OpenSpec workflows, `e2e-doctor`, `tag-release`) stay platform-specific. Their
repository rules link to shared sources; do not copy their platform mechanics into another checklist. A private
memory reference cannot be required to interpret a shared rule. Historical examples remain evidence for their
recorded revision and do not override current contracts or issue/PR state.

## Manual read-only parity smoke

Run this procedure after changing a contract, adapter, or repository routing rule. It only reads tracked files.

1. Confirm all five canonical contracts and all ten adapters exist, and that `.agents/protocol.md` exists.
2. Confirm each adapter names exactly its matching `.agents/contracts/...` path and says to read it fully first.
3. Confirm the Claude reviewer, architect, and judge tool lists contain `Read`, `Bash`, `Grep`, `Glob` (plus `Skill`
   for reviewer and architect), but not `Edit`, `Write`, `Task`, or another delegation tool; the explorer adds `Write`
   (the inventory file only) and no delegation tool. `gopls` is reached through `Bash`; do not add an `LSP` tool name —
   an unresolved tool name makes Claude Code refuse to launch the agent.
4. Confirm the Codex reviewer, architect, and judge set `sandbox_mode = "read-only"`; the developer and the explorer
   have no sandbox override (the explorer writes the inventory file) and therefore inherit the parent workspace
   permissions.
5. Confirm `AGENTS.md` and `CLAUDE.md` are byte-identical and under the word ceiling:
   `go test ./test/contract/ -run TestGuidanceMap` (the map is one file under two names).
6. Inspect adapter size with `wc -l .claude/agents/semstreams-*.md .codex/agents/semstreams-*.toml`; adapters should
   remain short and contain no copied checklist.
7. For every row in the shared-skills table, confirm the canonical file and Claude adapter exist, the adapter
   points to that canonical file and says to read it fully, and it contains no copied checklist. Resolve the
   `preflight` name mapping explicitly. Confirm both repository entry points route to the same shared skills.
8. Validate changed skill frontmatter and relative links. A structurally valid adapter does not prove its
   instructions make the right decisions; walk the applicable scenarios below without private memory.

Use these semantic fixtures when reading the routing text:

- "Implement a nontrivial graph-index change" routes first to SemStreams developer and then SemStreams reviewer.
- "Review a nontrivial SemStreams change" routes to SemStreams reviewer in read-only mode.
- "Design an API contract or OpenSpec target" routes to SemStreams architect (surface inventory first, drafts as
  text); binding rulings and approval stay with the owner session.
- "Update durable docs or reconcile task truth" remains technical-writer-owned.
- "Enumerate what is on this surface for a change" routes to SemStreams explorer (inventory file with every search
  recorded); the architect may start from that file and the reviewer re-derives it independently.
- "Which of these two shapes / is this finding real / what should the owner rule on X" routes to SemStreams judge
  (one bounded question over evidence given as paths; a recommendation and the ruling it prepares — the owner rules).
- "Check an isolated Go idiom" may use a generic Go agent only as a second pass.
- "Add a payload" reaches explicit registration and the applicable production/E2E composition roots.
- "Verify an integration change" reaches the canonical runner and its shared host lock.
- "Prepare a docs-only PR" retains protocol review/CI gates and selects checks for the changed artifacts.
- "The previous revision was green" preserves that evidence as historical, not current-head proof.
- "Review a property whose expected result calls the implementation" reaches the independent-oracle and
  boundary requirements, not a coverage-percentage substitute.
- "Continue without private memory" resolves shared state and verifies ownership through pickup/protocol.

The smoke passes only when Claude and Codex resolve the same logical role and canonical contract for every fixture.

# ADR-032 Programme — Resume Doc

**Branch**: `feat/adr-032-programme`
**Paused**: 2026-05-05
**Last commit on branch**: `6bab268` (wip tag 4 chunk 3 partial)
**Will be deleted**: when the final PR merges, this doc disappears with the
branch. Do not link to it from outside the branch.

## Why this exists

The ADR-032 programme is a six-tag, breaking-change rewrite of subjects + KV
buckets for multi-tenant isolation. Per the programme strategy (see master
plan), it ships as ONE FINAL PR at end-of-programme — never piecemeal — to
avoid disrupting the sister products (semspec, semteams) that consume the
framework's wire format. We paused mid-tag-4 because bug fixes are stacking
up on `main` and we need to clear that backlog before continuing.

This doc is the single source of truth for resuming. Read it first.

## Programme status snapshot

| Tag | Scope | Status | Anchor commit |
|---|---|---|---|
| 1 | $caller.* substitution + deny action | **Shipped to main as beta.32** (PR #20) | already on main |
| 2 | identity-as-struct + NATS-header propagation + $caller.* condition support | **On branch** (PR #23 closed without merge per strategy revision) | last tag-2 commit `9f0b00e` |
| 3 | shadow mode + count_in_window + negate + governance/ package + $message.violations.* end-to-end | **On branch** + simplify pass | last tag-3 commit `f6d5a40` |
| 4 chunk 1 | natsclient.KVBucket wrapper + tracker migration | **On branch** | `388385c` |
| 4 chunk 2 | component.BucketName + ResolveSubjectForOrg helpers + 22 canonical bucket constants | **On branch** | `d375a48` |
| 4 chunk 3 | KV bucket org-suffix migration | **WIP partial** — restart from architect spec on resume | `6bab268` |
| 4 chunk 4 | publish-site org-prefix migration | pending | — |
| 4 chunk 5 | HTTP org-validation middleware | pending | — |
| 4 chunk 6 | integration tests + testcontainer fixture sweep | pending | — |
| 5 | JetStream cluster docs + reconnect defaults | pending | — |
| 6 | cert-based auth + per-org-account hardening + mTLS deployment recipe | pending | — |
| Final | One comprehensive migration doc + ADR status flips + ONE PR to main | pending | — |

## Branch divergence

At pause time:
- Branch is **3 commits ahead of `origin/feat/adr-032-programme`** (all pushed at pause)
- Branch is **18 commits behind `main`** (sister-product bug fixes; mostly orthogonal)

### Rebase policy while paused

Sister-agent's bug fixes will continue stacking on main. To keep resume cost
manageable, **rebase on main every 2-3 weeks** while paused. Rebase always,
never merge — preserves linear history + clear tag boundaries.

```bash
git checkout feat/adr-032-programme
git fetch origin
git rebase origin/main
# resolve conflicts
git push --force-with-lease origin feat/adr-032-programme
```

If a sister commit ships something the programme depends on (e.g. interface
refactor we use), absorb it via rebase + retest the affected tag's
end-of-tag verification. Most of sister's surface area (`agentic-loop`,
`agentic-tools`, `graph-query`) is orthogonal to the tag-4 area
(`component/`, `processor/rule/`, `natsclient/`).

## Locked-in decisions

These are programme-level decisions that should NOT be re-debated on resume.

### From the original ADR-032 planning session

| Decision | Resolution |
|---|---|
| Tenancy unit | Tenant = org. 6-part entity ID's `org` field IS tenant identity. No sub-tenancy. |
| Multi-tenancy model | Data-aware only, not runtime. One process = one org. |
| Org wire convention | Always emit. Subjects org-prefixed; buckets org-suffixed. |
| Account model | NATS accounts are an optional hardening layer. Framework code identical either way. |
| Migration policy | Greenfield — manual per-project migration with one consolidated doc. |
| Identity model | `string` → `auth.Identity{ID, Role, Org, Source}` struct. Pre-1.0 beta acceptable. |
| `$caller.*` fields | Three only: `$caller.id`, `$caller.role`, `$caller.org`. |

### From the strategy revision (2026-05-02)

| Decision | Resolution |
|---|---|
| Per-tag PRs | **Abandoned.** Tags 1+2 had churn with sister-agent's tag numbering; switched to single-final-PR. |
| Migration docs | Defer ALL to one consolidated doc at end-of-programme. |
| ADR status updates | Defer to one final pass. |
| Mid-flight CHANGELOG entries | Defer similarly. |
| Tag renumbering | Once at final-PR time based on what's available on main. |

### From the tag-4 architect pass (2026-05-05)

| Decision | Resolution |
|---|---|
| Graph buckets in scope | YES — ENTITY_STATES, COMMUNITY_INDEX, SPATIAL_INDEX, TEMPORAL_INDEX, EMBEDDING_*, ANOMALY_INDEX, STRUCTURAL_INDEX, PREDICATE_INDEX, INCOMING_INDEX, OUTGOING_INDEX, ALIAS_INDEX, CONTEXT_INDEX, EMBEDDINGS_CACHE, OASF_RECORDS all org-suffix |
| Rule-action user-supplied bucket names | Auto-suffix transparently. Rule author writes `bucket: "MY_AUDIT"`; framework writes to `MY_AUDIT_<org>`. Document via inline code comment. |
| `agentic.query.trajectory` request/reply subject | Yes prefix — for uniformity. Even though it's not via ResolveSubject (literal), it gets `<org>.agentic.query.trajectory`. |
| KVBucket wrapper bundling | YES — bundled with tag 4 since tag 4 already touches every tracker. Closes the long-deferred `project_kv_wrapping_debt.md` debt. |

## Critical lessons (memory references)

These memory files encode the hard-won lessons from the programme. Read on
resume:

- `~/.claude/projects/-Users-coby-Code-c360-semstreams/memory/feedback_pseudo_field_namespace_full_path.md` — adding a `$foo.*` namespace requires THREE pieces (substitution + state-fields + re-evaluation gate), not two
- `~/.claude/projects/-Users-coby-Code-c360-semstreams/memory/feedback_dev_agents_propagate_patterns.md` — dev agents reflexively mirror neighboring patterns even when those patterns carry debt; PM must call out pattern-debt explicitly
- `~/.claude/projects/-Users-coby-Code-c360-semstreams/memory/feedback_review_cadence_pre_commit.md` — pre-commit review > post-hoc review (every chunk)
- `~/.claude/projects/-Users-coby-Code-c360-semstreams/memory/project_kv_wrapping_debt.md` — closed by tag 4 chunk 1 (commit `388385c`)
- `~/.claude/projects/-Users-coby-Code-c360-semstreams/memory/feedback_lsp_lag_after_subagent.md` — gopls lags after a subagent commit; trust `go build` + `go test` over LSP diagnostics

## Architect spec for tag 4

Lives at: `/Users/coby/.claude/projects/-Users-coby-Code-c360-semstreams/ceeb546a-026c-4f5c-8523-057a95817415/tool-results/bfjuqri61.txt`

Sections:
- 1.1: BucketName end-to-end wiring path with file:line for every producer/consumer
- 1.2: ResolveSubjectForOrg end-to-end wiring path
- 1.3: KVBucket interface definition + adapter spec
- 1.4: HTTP middleware design
- 2.x: KVBucket interface specification (chunk 1 — DONE)
- 3.1: Bucket migration matrix (all 13+ buckets with file:line)
- 3.2: Subject migration matrix (16 subject patterns)
- 3.3: NATS stream subject-filter changes
- 4.x: HTTP middleware design (chunk 5)
- 5.x: Test plan (chunk 6)
- 6.x: Backward compatibility surface
- 7.x: Sequencing risks
- 8.x: Architect review checklist per chunk

This file is in conversation history (session `ceeb546a-026c-4f5c-8523-057a95817415`). When resuming, regenerate it via the architect agent if the file is no longer accessible — section 1 of this RESUME doc has enough to brief the architect.

## Master plan

Lives at: `~/.claude/plans/just-finished-a-big-staged-waterfall.md`

Contains the original six-tag breakdown, branching strategy, governance↔rules
boundary discussion, breaking-change inventory, and review cadence.

## Resume protocol

When picking this back up:

```bash
# 1. Verify branch
git checkout feat/adr-032-programme
git branch --show-current  # MUST be feat/adr-032-programme

# 2. Rebase on current main
git fetch origin
git rebase origin/main
# resolve any conflicts; force-push if rebase rewrote any of our commits

# 3. Verify state still compiles + tests pass post-rebase
go build ./...
go test -race -count=1 ./...
go test -race -count=1 -tags=integration ./processor/rule/... ./natsclient/...

# 4. Read this doc + lessons memory files

# 5. Decide next chunk
```

### Picking the next chunk

The WIP commit `6bab268` is partial chunk 3 (rule-package buckets only,
~5 of ~13 buckets done). On resume:

- **Option A (recommended)**: revert `6bab268` and restart chunk 3 cleanly
  from the architect spec. Avoids the half-done state polluting reviewer's
  context. The dev brief lives in conversation history under the
  rejected-tool-call before pause; reconstruct it from architect spec
  section 1.1 + 3.1.

- **Option B**: extend `6bab268` chunk-by-bucket. Risky — easy to miss a
  producer/consumer pair when not starting from the full list. Only do this
  if you have the full architect-spec wiring matrix open in front of you.

After chunk 3, chunks 4 → 5 → 6 follow the architect spec. Each chunk gets
its own pre-commit go-reviewer pass per `feedback_review_cadence_pre_commit.md`.

### Test surface that must pass before any merge

Per the programme strategy, the final PR is the only PR. It must demonstrate:

- `task lint` clean
- `go test -race ./...` clean
- `go test -race -tags=integration ./...` clean
- `task schema:generate` produces no diff
- `/security-review` skill clean
- `/simplify` skill applied
- New flagship test `TestIntegration_TwoOrgsShareNATS_NoBucketCollision`
  passes (chunk 6 — fails red if any bucket/subject migration missed)
- New `TestIntegration_OrgMiddleware_RejectsCrossOrgHost` passes (chunk 6)
- All existing integration tests pass after testcontainer fixtures are
  updated to set `Platform.Org="test"` (chunk 6)
- Manual e2e verification with two-process NATS setup (acme + widgets)

### Sister-product coordination todos

Before opening the final PR:

- Open tracking issues in semspec, semteams, semdragon (the three sister
  products that consume this framework) describing the breaking surface:
  - Subject namespace change (every framework subject gets org prefix)
  - KV bucket renames (every framework bucket gets org suffix)
  - Identity struct shape (already shipped in tag 2)
  - HTTP middleware optional install
- Wait for confirmation that they have a migration plan before merging

## Outstanding TODOs surfaced during pause

- The `getOrCreateEntityBucket` DEPRECATED helper in `entity_watcher.go`
  still references `"ENTITY_STATES"` literal (not `BucketName(...)`). Either
  delete the helper entirely (no callers remaining post-chunk-3) or migrate.
  The WIP commit migrates `getOrCreateBucket` but leaves the deprecated
  sibling untouched.
- The local `ScheduleBucketName` constant in `schedule_tracker.go` and
  `WindowsBucketName` in `window_tracker.go` are now redundant with the
  canonical `component.RuleSchedulesBucketName` / `component.RuleWindowsBucketName`.
  Drop or alias to the canonical names during chunk 3 cleanup.
- Schema regen has NOT been run after chunk 3 WIP. Run `task schema:generate`
  early in chunk 3 cleanup; expect no diff (no schema changes in tag 4).

## Programme breaking-change inventory (for the final migration doc)

When the programme reaches end-of-programme and we write the consolidated
migration doc, it must cover:

1. Tag 2: `WithIdentity(ctx, string)` → `WithIdentity(ctx, Identity{ID:"..."})` — small breaking, pre-1.0 acceptable
2. Tag 3: `enabled: false` → tristate (`Mode` type with custom UnmarshalJSON for backward compat); shadow mode is opt-in
3. Tag 4: subject namespace change (`agent.>` → `<org>.agent.>`)
4. Tag 4: KV bucket renames (`RULE_STATE` → `RULE_STATE_<org>`)
5. Tag 4: tracker constructors take `natsclient.KVBucket` instead of `jetstream.KeyValue` (rare external API)
6. Tag 4: deployments without `Platform.Org` set fail at startup (already required so should be ~zero affected)
7. Tag 4: out-of-tree subscribers to e.g. `agent.>` need `*.agent.>` instead
8. Tag 4: `cmd/semstreams/main.go` installs `OrgValidationMiddleware`; downstream products follow same pattern
9. Tag 4: rule-action user-supplied bucket names auto-suffixed (transparent to authors; document)
10. Tag 6: optional mTLS / per-org-account opt-in deployment guide

## Quick decision matrix on resume

| If... | Then... |
|---|---|
| Branch is wildly behind main (50+ commits) | Consider whether the programme is still the right shape vs starting fresh |
| `task lint` fails post-rebase | Investigate before extending — sister product may have changed conventions |
| Integration tests fail post-rebase | Likely a sister-product change to a shared bucket/subject; coordinate fix |
| chunks 4-6 blocked by sister product | Open a coordination issue and pause again |
| Programme paused > 6 weeks | Re-validate the architect spec against current main; some file:line numbers may have drifted |

## Tracking

GitHub issue: TBD (created at pause time; link to be added after `gh issue create`).

---
name: preflight
description: Run the full pre-push / pre-PR gate the semstreams way — scope the diff, Docker-preflight if needed, run the right gates (mirrors CI), and detect breaking changes that demand an e2e tier. Use before pushing a branch, opening a PR, or whenever you want a green-light read on local changes.
argument-hint: [optional: base ref, defaults to main]
---

# Preflight — pre-push gate with judgment

The deterministic core is `task check:push`. This skill adds the judgment CI can't: **what scope of
gate this diff actually needs**, **Docker health before integration/e2e**, and **whether the change
is BREAKING** (which forces an e2e tier before it can land — CLAUDE.md HARD RULE).

Base ref: $ARGUMENTS (default `main`).

## Step 0 — Scope the diff

```bash
git diff <base>...HEAD --stat        # committed
git status -s                        # uncommitted
```

Classify (pick the *highest* that matches — gates are cumulative):

| Diff shape | Gate to run |
|---|---|
| **A. Docs / markdown / `.claude/` only** | `task build:default` + `task lint` + (if any `schema:`/component config touched) `task schema:generate` && `task schema:check-changes` |
| **B. Go logic, no NATS/graph/integration surface** | `task check` (lint + unit) + `go test ./test/contract/...` |
| **C. Touches `natsclient/`, `component/`, `graph*`, `pkg/`, validator, vocabulary, or any integration-tagged test** | **`task check:push`** (the full gauntlet) — framework-package changes need the integration sweep (`feedback_framework_change_needs_branch_integration_sweep`) |
| **D. BREAKING** (see Step 2) | C **plus** a relevant **e2e tier green** before it lands |

When unsure between B and C, run C. False-cheap; a missed integration break is expensive.

## Step 1 — Docker preflight (only if Step 0 = C/D, i.e. integration/e2e will run)

Run `/e2e-doctor` first (or inline: `docker system df`, check for leaked testcontainers, `task
e2e:check-ports`). A starved daemon makes testcontainers time out at ~60s and *looks* like a code
failure (the beta.115 trap). Reclaim before running, not after a confusing red.

## Step 2 — Detect BREAKING

A change is breaking if ANY of:
- the commit/PR title carries `!` (e.g. `feat(x)!:`) or a `BREAKING CHANGE:` footer
- a **removed/renamed exported symbol** in a `pkg/`, `natsclient`, `graph`, `component`, or message type
- a **wire-contract change** (request/response struct fields, NATS subject semantics, error envelope)
- a registry/singleton retirement or factory+payload split (the half-migrated-binary class)

If breaking: pick the e2e tier that covers the touched path and confirm it **green before merge** —
this is non-negotiable (`feedback_e2e_required_for_breaking_changes`). Rough map:

| Touched path | Tier |
|---|---|
| rules / structural inference / graph wire / mutation error contract | `task e2e:structural` |
| BM25 / statistical | `task e2e:statistical` |
| embeddings / neural / LLM | `task e2e:semantic` |
| agent loop / tools | `task e2e:agentic` |
| CRUD round-trip / pattern-B | `task e2e:crud-tools` |
| lifecycle harness | `task e2e:lifecycle` |

If no tier covers it, that's a coverage gap — file it before merging, don't wave it through.

## Step 3 — Run the gate & report

Run the gate from Step 0. Report a per-step checklist (✅/❌). On red, **show the actual failing
output** and stop — do not push. Common reds and what they mean:
- `schema:check-changes` fails → you forgot to commit a `schemas/`/`specs/` diff after a config change.
- integration timeout at ~60s with `port "4222/tcp" not found` → Docker starvation, NOT your code → `/e2e-doctor`.
- one known pre-existing flake: rule `TestEntityWatcher_RuleTriggerDebouncing` (load-induced; passes in isolation) — re-run it alone to confirm before blaming the diff (`feedback_investigate_before_classifying_as_flake`).

## Step 4 — Green → next

All green: safe to push / open the PR. Note this repo has **no required CI checks**, so a local
green IS the merge gate; if you still want CI-gated merge, `gh run watch <id> --exit-status` to green
THEN merge (`feedback_repo_no_required_checks_auto_merges_immediately`). For a non-trivial Go change,
run `semstreams-reviewer` before merge. To cut a tag, use `/tag-release`.

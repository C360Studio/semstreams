# Pattern classification of the open `class:*` symptom issues (#1234)

Date: 2026-09-13 · Baseline `main` @ `8e41e46f`

Status: **classified.** Every open `class:*` issue is below as `issue → missing pattern → owning package →
closes-with`, followed by the **bundles** the owner places (`.agents/protocol.md` § File). The audit is read-only
over issues and code: it writes no production code and files no issues.

## Inputs

- `issues.md` — the set, re-derived at the baseline (60 unique across the five labels; the issue body's 55 was
  the 2026-09-01 count, and the body itself says to re-derive rather than quote).
- `inventory-<slice>.md` — explorer sweeps by owning area, one file per slice; every pin is verifiable with
  `task inventory:verify -- docs/proposals/pattern-classification-2026-09/<file>`.
- Vocabulary source: `openspec/changes/archive/2026-09-02-loop-scoped-request-seams/inventory-precedent.md`
  (pinned at `0a40ddf3`; 8 of its 41 pins drift at this baseline because PR #1231 swept
  `processor/agentic-dispatch` — re-pinned here in `inventory-sweeps-vocabulary.md`).

## Classification

Pattern column, against the six terms in the #1234 body: **refusal+signal** = classified refusal + observed signal
(`errs.ClassifiedCode*` vs the uncoded `Wrap*` family) · **gate** = admission gate (one home, form-check first, one
metric-reason home, one named log string) · **create-vs-exists** · **read-through** = read-through /
merge-with-reconcile over a cache · **authority** = authority delegation · **none** = no pattern applies, a genuine
one-off.

| issue | pattern | owning package | closes-with |
|---|---|---|---|
| #348 | refusal+signal | `processor/graph-query` | standalone (ADR-060 precedent is in the same package) |
| #383 | gate | `processor/rule` | B4 with #1042 |
| #436 | gate | `processor/gated-dag` | standalone |
| #472 | refusal+signal | `service` | B3 |
| #589 | none (removal) | `graph/inference` | B8 |
| #608 | refusal+signal | `processor/graph-clustering` | B2; its level-blind scan with #1212 |
| #618 | refusal+signal | `processor/graph-clustering` | B3 |
| #620 | none (removal) | graph core, many | B8 — **rescope, most of it landed** |
| #621 | refusal+signal | `pkg/fusion`, `processor/graph-query` | B3 |
| #659 | gate | `component` | B4 with #734 |
| #734 | gate | `component`, `cmd/openapi-generator` | B4 with #659 |
| #746 | none | `processor/rule/expression` | standalone |
| #764 | none (removal) | `pkg/dispatch` | B8 |
| #810 | gate | `composition`, `natsclient` | B7 — `status:blocked` |
| #824 | gate | `gateway/lifecycle-gateway`, `pkg/lifecycle` | B5 |
| #857 | refusal+signal | `natsclient` + every `Put` caller | standalone — `status:needs-decision` |
| #980 | refusal+signal | `processor/agentic-tools` | B2 |
| #1002 | none (doc) | `pkg/logging` | standalone; #1202 is its sweep home |
| #1007 | gate | `processor/rule`, `processor/agentic-tools` | B5 |
| #1029 | authority | `pkg/projection`, `processor/graph-ingest` | standalone |
| #1035 | refusal+signal | `processor/agentic-loop` | B9 |
| #1041 | refusal+signal | `processor/rule` | B2 |
| #1042 | gate | `processor/rule` | B4 with #383 |
| #1043 | refusal+signal | `processor/rule` | B3 |
| #1045 | gate | `processor/agentic-governance`, `processor/rule/expression` | B6 |
| #1049 | gate | `processor/rule`, `vocabulary` | B6 |
| #1076 | none (removal + audit) | `natsclient` | B8 |
| #1121 | none (removal) | `test/e2e/client` | B8 — post-v1 |
| #1123 | none (removal or wiring) | `service` | B8 |
| #1124 | gate | `processor/agentic-tools` | B5 |
| #1125 | none (removal) | `testutil` | B8 |
| #1126 | create-vs-exists | `metric` | standalone |
| #1132 | refusal+signal | `agentic`, `processor/agentic-loop` | B2 |
| #1135 | none (removal) | `cmd/e2e` | B8 — post-v1 |
| #1136 | none (vocabulary + doc) | `processor/graph-query` | standalone — **asserted ruling has not landed** |
| #1138 | refusal+signal | `processor/agentic-tools/executors` | standalone — **CLAIMED by PR #1141** |
| #1140 | gate | `processor/agentic-governance`, `processor/agentic-loop` | B6 — `status:blocked` |
| #1143 | gate | `composition`, `processor/graph-ingest` | B7 — `status:blocked` |
| #1146 | read-through | `processor/agentic-loop` | B9 — **CLAIMED by PR #1159** |
| #1152 | none (removal) | `test/e2e/scenarios/stages` | B8 — post-v1 |
| #1170 | refusal+signal | `processor/rule` | B2 |
| #1172 | refusal+signal | `graph/inference` | B3 |
| #1187 | none (removal) | `message`, `config` | B8 — **rescope, one of three has a caller** |
| #1201 | gate | `composition` | B7 — the approved enabler |
| #1202 | none (sweep container) | repo-wide | B8 — post-v1; absorbs #1002 |
| #1203 | none (triage container) | repo-wide | B8 — the filed home |
| #1204 | refusal+signal (container) | repo-wide | B2 — the filed home |
| #1206 | none (remove or implement) | `processor/rule` | B8 |
| #1212 | none (key discriminator) | `processor/graph-ingest` | standalone; with #608's level-blind scan |
| #1222 | none (test gap) | `test/e2e/scenarios/research-graph` | B1 — **rescope, agentic half is closed** |
| #1223 | authority | `test/e2e/config` | B1 — **rescope, guard residual only** |
| #1224 | refusal+signal | `processor/research-graph-synthesize` | B1 |
| #1239 | none (removal) | `processor/agentic-loop` | B9 — declared by PR #1156 |
| #1244 | refusal+signal | `processor/agentic-loop` | B9 — named but disclaimed by PR #1159 |
| #1249 | create-vs-exists | `agentic/agentrun` | B9 — declared by PR #1156 |
| #1252 | none (removal) | `agentic/identity` | B8 — **cited ADR-107 does not resolve** |
| #1255 | none (hygiene + guard) | repo-wide test files | B1 |
| #1270 | refusal+signal | `processor/agentic-tools/executors` | B3 — the filed home |
| #1286 | none (test-guard defect) | `processor/graph-index` | B1 |
| #1288 | refusal+signal | `processor/research-graph-synthesize`, `processor/agentic-tools` | B9 |

## What the distribution says

| pattern | issues |
|---|---|
| classified refusal + observed signal | 19 |
| admission gate | 14 |
| no pattern applies | 22 |
| create-vs-exists | 2 |
| authority delegation | 2 |
| read-through / merge-with-reconcile | 1 |

The owner's framing holds, with one correction worth placing work against. **Two of the six terms account for 33 of
the 60**, and both are the same missing vocabulary at different distances from the seam: a refusal the caller cannot
branch on, and a gate that never refuses at all. That is the "class of dev failures, not a class of errors" the
filing describes, and it is why a per-issue hunt kept finding the same defect in new packages.

The correction is the 22. They are not a missing pattern in any form — they are **excavation**: surfaces left by an
abandoned direction, a stale doc, a dead test harness, a key that omits a discriminator. No pattern would have
prevented them and no sweep fixes them, because each one needs a wanted-vs-wired ruling the code cannot supply
(#1203's own rule). Grouping them as one bundle is a scheduling convenience, not a technical claim. A classification
that mapped all 60 onto the six terms would have been the less credible answer.

Within the 19, the two sub-lanes cost different work and are bundled apart. **An error exists but is uncoded**
(`Wrap*` discarded, or a `Warn` that continues) is mechanical adoption against a worked example — #348, #608, #980,
#1035, #1041, #1132, #1170, #1244. **No error exists at all** — a seam returns nil, empty, truncated, or skipped and
says nothing — needs the refusal to be invented before it can be coded: #472, #618, #621, #1043, #1172, #1270.

## Bundles

Nine bundles. Each names its blast radius, the in-tree worked example the fix copies, and what gates it. Ordering
inside a bundle is stated only where it binds.

**B1 — Make the e2e tier able to see (5): #1222, #1223, #1224, #1255, #1286.** Test-only, no production package, no
exported surface. Worked example: `test/contract/rapid_test_only_dependency_test.go` for the guard shape,
`test/e2e/config/tier_authority.go` for read-not-predict. **Place this first.** Every other bundle's verification
runs through the tier these five issues make honest, and #1224 is a live case of a green scenario measuring a
degraded path. Two rescope before work starts: #1222's agentic half is already closed and only research-graph
remains, and #1223's migration gap is closed with only the guard residual left.

**B2 — Boot honesty (6): #1204 is the home; #608, #980, #1041, #1132, #1170.** Five packages:
`processor/graph-clustering`, `processor/agentic-tools`, `processor/rule` (two issues), `agentic` +
`processor/agentic-loop`. No exported-surface change; all are Start-path behavior. Worked example:
`processor/graph-ingest/authority_gate.go` and `processor/rule/entity_pattern_contract.go:68`. Nothing claimed,
nothing blocked. #1132 is the inverse of the other four and belongs here anyway: it crashes where the neighbours
swallow, and both answers are decided by the same question about what a Start path owes its caller.

**B3 — Typed absence at a read seam (6): #1270 is the home; #472, #618, #621, #1043, #1172.** Six packages.
Adopter-visible: `ResultHint` producers and a `Truncated` field change what a caller receives. Worked examples both
landed recently, which is what makes this bundle cheap — `processor/agentic-tools/executors/graph_query.go`
(`ResultHint`, from #1261) and `processor/rule/actions.go:689` (`foreignFiringSkipRecorder`, from #1169). #1172 is
pure adoption of #1169's shape in a second package.

**B4 — Declared config that does nothing is refused at load (4): #383, #1042, #659, #734.** Two packages,
`processor/rule` and `component` (plus `cmd/openapi-generator`). **Breaking for adopters:** a config that loads today
starts failing, and #659/#734 regenerate every schema. Worked example: `processor/rule/cron_rule.go:180-191`, which
already collects and rejects both of the rule-level fields #383 and #1042 describe. The closed-vocabulary precedent
landed in #1267. Needs a schema-validation run and an e2e tier per the breaking-change rule.

**B5 — The advertised set is the enforced set (3): #824, #1007, #1124.** `gateway/lifecycle-gateway` +
`pkg/lifecycle`, `processor/rule`, `processor/agentic-tools`. Adopter-visible: a workflow or tool disappears from a
list it should never have been on. Worked example is 40 lines from the defect —
`processor/agentic-tools/executor.go:92-109` (`RegisterExecutor`) already validates two-pass what `RegisterTool` at
`:52` does not.

**B6 — The ADR-043 verdict is rule-readable (3): #1045, #1049, #1140.** `processor/agentic-governance`,
`processor/rule` + `processor/rule/expression`, `vocabulary`. **#1140 is `status:blocked`**, and PR #1159 names it
explicitly as remaining separate, so the bundle cannot be placed whole until that block is resolved. Worked example:
`processor/agentic-governance/component.go:349-364`, an admission gate already in the owning package that the
tool-result lane bypasses. #1045 may be better answered by registering the verdict as a predicate than by gating the
unwalkable key; that fork is a design question, not a classification one.

**B7 — Subject-plane admission gate (3), ordered: #1201, then #1143, then #810.** `composition`, `natsclient`,
`processor/graph-ingest`. Adopter-visible and the highest-risk bundle here: a flow config that boots today becomes
invalid. #1201 is already approved and on beta.165 and is the enabler. **#810 and #1143 are both
`status:blocked`** — #810 behind "fix port handling first" (owner, 2026-08-31) and #1143 behind PR #1148 — so only
#1201 is placeable now. Worked example: `composition/analyze.go:114` (`explicitStreamCovers`).

**B8 — Dead surface, ruled per site (14): #1203 is the home; #589, #620, #764, #1076, #1121, #1123, #1125, #1135,
#1152, #1187, #1202, #1206, #1252.** This is the excavation bundle and the one that shrinks the unplaced pre-v1
count most. It is **not one change**: #1203's own rule is that wanted-vs-wired is ruled per site, and four members
(#1121, #1135, #1152, #1202) are `horizon:post-v1`. Ten of these are already named in #1203's body, so the bundle
exists as filed work and needs placement, not re-filing. Blast radius is the gate: removals land in `message`,
`config`, `service`, `pkg/dispatch` and delete `agentic/identity` outright, so every site must clear
`task api:compat` and the Tier 1 apidiff guard before it is scheduled. Three rescope first — #620 has lost most of
its named sites to work that already landed, #1187 is two surfaces not three because `DeploymentPrefix` has a
production caller and a `MUST export` line in the entity-id spec, and #1252's ADR-107 citation does not resolve
against the ADR-107 that exists.

**B9 — Loop durability and settlement (6): #1146, #1239, #1249 in flight; #1035, #1244, #1288 to follow.**
`processor/agentic-loop`, `agentic/agentrun`, `processor/research-graph-synthesize`. **Codex holds the front half**:
PR #1159 carries `Closes #1146`, and PR #1156's body declares it will add `Closes #1249`, `Closes #1239`,
`Closes #1155` and convert `Refs #759` at integrated review. #1244 is named in PR #1159 and explicitly disclaimed
there ("#1244 transition review remain separate"); #1288 is named with no `Closes` line. Both are unclaimed, and
neither may be started against those branches. Place #1035, #1244, #1288
as a follow-on after the Codex stack squashes. Worked example for #1244 is
`service/component_manager.go:991-1070`, whose four-outcome exit contract the issue quotes under the wrong name.

**Standalone, deliberately not bundled (10): #348, #436, #746, #857, #1002, #1029, #1126, #1136, #1138, #1212.**
Each is one package with no sibling that shares its fix; bundling them would buy scheduling noise and no leverage.
Two are not placeable as they stand: **#857 carries `status:needs-decision`** and is the payload-size class root,
and **#1136 asserts a `sources`→`attributions` rename as ruled when that rename has not landed** and would break an
exported field. **#1138 is claimed by PR #1141** (`Closes #1138`) and is listed only so the set stays complete.
#1126 is the cleanest create-vs-exists instance in the 60: `metric/registry.go:245` discards what
`RegisterOrGetGaugeVec` returns one line away.

## Two shapes the six-term vocabulary does not carry

Surfaced by the classification, offered as findings rather than as an extension anyone applied here.

**Key completeness.** #1212 and the second half of #608 are the same defect: a KV key omits a discriminator the
domain requires, so two distinct entities collide and a removal cross-deletes. The level-keyed form at
`graph/clustering/storage.go:521` is the shape both want. None of the six terms names it, so both classify as
"no pattern applies", which undersells a repeatable defect.

**Readiness-wait adoption.** #1076 counts roughly 21 production bucket-acquisition sites against 7 `pkg/resource`
watcher adopters, and #618, #1041 and #1172 each turn on whether a consumer waited. ADR-088 records the decision;
what is missing is a term for the adoption gap, and that gap is currently spread across three patterns.

## The usage-vs-audit tag, in one line

The 2026-08-31 recommendation to tag usage-found and audit-found bugs separately cannot be applied retroactively to
this set: no issue in the 60 records its discovery channel, so the partition would have to be guessed. If the owner
wants the tag, it has to be applied at filing time going forward, which makes it a File-ritual question rather than
a backlog one.

## What this hands the owner

Placement of nine bundles, in this order of leverage: **B1** (test-only, protects every later verification),
**B2** and **B3** (the two adoption lanes, 12 issues, worked examples already in tree), **B4** and **B5** (small,
breaking, adopter-visible), **B8** (14 issues, the largest cut to the unplaced count, but per-site rulings and the
Tier 1 gate), **B7** and **B6** (blocked members), **B9** (behind the Codex stack).

Four rescopes are owed before their bundles are worked: #620, #1187, #1222, #1223. Two issues need an owner ruling
before placement: #857 and #1136. Nothing in this audit was filed as a new issue, per #1234 and the File ritual.

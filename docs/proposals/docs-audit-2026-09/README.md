# Documentation audit: correctness and reduction

Several prominent builder paths cannot be relied on as written. The README's first-run composition, the first
processor tutorial, package examples, and agentic quickstart disagree with current declarations or admission
checks. Other guides overstate token precision, trajectory completeness, and configuration reload behavior.

The immediate v1 priority is to **correct and shorten the learning paths together**. Updating every copied example
would leave the same maintenance problem in place. Retired tutorials and competing explanations make developers
and coding agents reconstruct context that the framework is intended to expose.

This is a September 14, 2026 snapshot at `ea22e6a4e75d12bf7f6050c6d8121de1d089b191`, immediately after #1300.
Issue [#1302] owns this audit; remediation is separately scoped. This PR changes audit Markdown only.

## Scope and confidence

The mechanical census covers **1,184 Markdown files / 260,819 lines**, plus a separate count of **85 `doc.go`
files / 13,235 lines**. It found **140 missing-target link occurrences**, representing 87 targets across 59 pages,
and three stale-anchor candidates. Those counts include history and examples; they are triage results, not 140
independently verified current-user defects.

Three specialist inventories and independent review checked selected high-impact claims against source and
current capability specifications. No whole page was certified. Tutorials were not compiled or executed, and no
resource benchmark, offline/federation test, model call, or external URL check ran. Source contradictions below
are distinct from observed runtime failures.

The supporting [inventory](inventory.md), [search/coverage record](searches.md), and [reproducible census](methods.md)
provide evidence and limits. They are investigation records, not another adopter guide.

## Findings that affect an adopter

1. **The first-run and component-authoring paths need repair first.** README `task dev:start` builds the framework
   binary and selects a config enabling `iot_sensor`; the inspected composition registers that example only in
   the E2E binary. The registry rejects unknown factories. This is a source-traced mismatch, not an executed boot
   result. The 1,369-line first-processor tutorial still uses global `payloadregistry.Register` in `init()` and
   removed `PortDefinition.Type`/`Subject` fields. Current registration is explicit and instance-based.
   [Evidence: onboarding and package examples](inventory.md#onboarding-and-package-examples).

2. **Package and contributor references also contain unusable copied code.** The message README has the wrong
   module path, a one-argument constructor where three arguments are required, and a decoder example without a
   registry. Component registration examples omit the required port declarer. The schema tutorial uses `desc`
   where the parser admits `description`, and an obsolete factory signature. Embedding and clustering READMEs
   show outdated constructor forms. Replacing a tutorial link with godoc alone does not repair these examples.
   [Evidence: onboarding](inventory.md#onboarding-and-package-examples),
   [contributor APIs](inventory.md#operations-and-contributor-apis),
   [package references](inventory.md#package-reference-and-spec-disagreements).

3. **The agentic quickstart and context explanation disagree with current primitives.** The sample `publish_agent`
   action lacks its required subject and uses an incompatible prompt substitution. The tool example converts a
   `map[string]any` argument value to bytes. The context guide promises exact token budgets while the estimator
   uses a character ratio. The agentic overview describes complete trajectories stored at loop completion under
   a loop key; current evidence contracts describe immutable per-attempt observations and explicitly disallow
   completeness claims. These are central promises for an app that exposes agent actions to people.
   [Evidence: agent examples and evidence interpretation](inventory.md#agent-examples-and-evidence-interpretation).

4. **Operator guidance promises reload behavior the composition contract excludes.** The model-registry runbook
   says dependent components restart on changes; the current contract selects the registry at boot and requires
   process restart for later writes. A nearby source comment also retains the older restart claim. Test-helper
   examples call `Stop(5*time.Second)` although the lifecycle interface takes `context.Context`. README and setup
   prerequisites advertise Go 1.25 while `go.mod` requires 1.26.3; automatic toolchain selection was not tested.
   [Evidence: operations and contributor APIs](inventory.md#operations-and-contributor-apis).

5. **One disagreement requires contract reconciliation before documentation repair.** The nested entity-watching
   guide and current rule-watching spec require a dedicated authoritative `WatchAll` guard before pattern
   bootstrap. Production code explicitly excludes that dedicated guard. Neither copying the spec into the guide
   nor rewriting both to match code establishes the intended guarantee. Existing #765 covers the guide's claim;
   its implementation/spec conflict must remain visible when that work is taken.
   [Evidence: package reference and spec disagreements](inventory.md#package-reference-and-spec-disagreements).

6. **The edge story needs measured and clearly bounded operational claims.** The deployment README associates a
   small-edge profile with memory/CPU limits and links to absent production Compose/deployment guides. The two
   limit keys occur only in six lines across three deployment JSON files in the literal inventory; no enforcement
   reader or measurement was established. Root package docs also summarize persistence and sync as offline
   capability. This audit does not prove those operating guarantees. Retain edge guidance, but trace what enforces
   each limit and state which disconnection/recovery behavior has actually been exercised.
   [Evidence: operations](inventory.md#operations-and-contributor-apis),
   [root documentation](inventory.md#navigation-and-document-authority).

7. **Navigation gives obsolete guidance current authority.** Four legacy workflow pages already have banners,
   yet the docs index advertises a workflow quickstart and configuration reference, and current pages link onward
   to them. Other current guides point to renamed ADRs, a misplaced vocabulary directory, and missing schema
   guides. The docs index still labels a landed lifecycle migration pending. `docs/ROADMAP.md` describes alpha
   blockers despite GitHub milestones being the current release authority.
   [Evidence: learning paths](inventory.md#learning-paths-and-retirement-labels),
   [navigation and authority](inventory.md#navigation-and-document-authority).

Useful counterexamples matter: the inspected Query Access sections describe their limited admitted operations;
the loop reference describes observed evidence; the SemSource chapter names its version and proof limits; current
integration-runner instructions agree with CI. The audit supports targeted repair, not a conclusion that all docs
or all runtime guarantees are wrong.

## Where to simplify or reduce

| Group | Measured size | Recommended treatment |
| --- | ---: | --- |
| Four legacy workflow pages | 2,501 lines | Replace obsolete bodies with reviewed retirement pointers |
| First-processor tutorial | 1,369 lines; 1,169 fenced | Short narrative around a maintained, compiled example |
| Message README and `doc.go` | 1,226 lines | Separate package purpose/API reference from one canonical payload how-to |
| Schema contributor guides 03–05 | 1,631 lines | Share API facts; keep task-specific guidance |
| Root AGENTS and CLAUDE files | 685 lines; 314 shared | Evaluate shared context; keep entry points and gates |
| `docs/ROADMAP.md` | 302 lines | Retire its status role in favor of GitHub milestones |

These are source volumes, **not a promised deletion count**. Some material must survive in a shorter form or move
to its appropriate home. Topic overlap alone is not duplication.

For the four legacy pages, preserve any unique useful explanation in the current
[orchestration catalog](../../concepts/14-orchestration-layers.md) or the
[phased-chain guide](../../concepts/25-phased-agentic-chains.md), with current examples checked before rerouting.
Then replace retired bodies with brief pointers that preserve old links. The catalog already names rules and
components as the two orchestration layers.

The mechanical pass found these **11 inbound Markdown links in non-proposal guidance**. Three additional historical
proposal citations are inline-code references and should retain their historical meaning.

| Retired destination | Lines | Inbound source locations |
| --- | ---: | --- |
| `basics/08-workflow-quickstart.md` | 624 | docs index:78; basics07:386 |
| `advanced/09-workflow-configuration.md` | 813 | docs index:79; concepts22:254; `pkg/context/README.md`:187 |
| `advanced/10-reactive-workflows.md` | 353 | advanced06:13; basics08:621; concepts23:668; two rule guides below |
| `concepts/23-parallel-agents.md` | 711 | concepts22:253 |

The `basics`, `advanced`, and `concepts` shorthand above is under `docs/`; the two rule guides are under
`processor/rule/docs/custom-rules.md:5` and `processor/rule/docs/operations.md:4`. Internal links between retired
pages disappear with the old bodies; active entry points
need a useful replacement. Do not merely move the same 2,501 lines into another current directory.

For component authors, the [IoT example](../../../examples/processors/iot_sensor/) is a candidate code home,
with the [payload registry guide](../../concepts/15-payload-registry.md) owning registration/decoding explanation.
Its framework-binary integration must be resolved before presenting it as the checked first-run path. Keep the
[SemSource chapter](../../basics/09-building-semsource.md) as the worked app narrative, with its pinned version;
it complements a minimal compiled example rather than replacing one.

Preserve material that serves a distinct purpose:

- Historical ADRs, proposal evidence, and OpenSpec archives explain decisions and investigation. There are 85,
  152, and 592 Markdown files in those groups respectively; their size is not evidence that they should be deleted.
  Keep current learning paths separate from historical evidence, using existing history banners where applicable.
- Operator recovery instructions and useful domain decisions, including edge/property guidance in #1260, reduce
  hidden context. Shortening them is only useful if the required decision and consequence remain clear.
- The ten role adapters already total just 85 lines. Shared-skill adapters are already thin. Keep that structure.
  Generated OpenSpec command/skill pairs require generator and platform-entry-point ownership checks before any
  deduplication. Their generic archive instructions must not obscure the project's completion gate.
- Concept overviews and precise references have different readers. Put wire keys, constructors and guarantees in
  one maintained reference, while retaining the overview's explanation of why and when to use the primitive.

## Existing issue coverage and uncovered work

This is a dated coverage map, not a new backlog or a change to any issue's status. The root session captured 258
open issue summaries and read twelve directly related issue bodies. Active PRs are claims, not shipped behavior.

| Finding family | Existing coverage | Boundary |
| --- | --- | --- |
| Retired workflows and parallel guidance | [#457], [#486] | Reuse for retirement and current routing |
| Predicate and log-subject reference drift | [#668], [#1002] | Concrete narrow documentation corrections |
| Entity-watch guard | [#765] | Include current code/spec disagreement in reconciliation |
| Retrieval evidence wording | [#1136] | Owner ruling includes a wire change; docs alone do not complete it |
| Lifecycle sentinels | [#1218] | Distinct from the `Stop(context.Context)` example error |
| ADR rationale / relationship guidance | [#828], [#1260] | Preserve history and useful decision support |
| Fusion Purpose / product boundary | [#1034], [#1163] | Existing ownership; avoid inventing a product decision |
| Type-authority wording | [#1133] | Two specific residues, not broad onboarding coverage |

The broad first-run/constructor/schema examples, hot-reload and token/trajectory explanations, current navigation
repair, resource-limit evidence, and source-consolidation work are **not covered by those narrow documentation
issues merely because they share a topic**. #1302 owns identification and planning, not their remediation.
Do not open one issue per typo. Reuse existing coverage and propose bounded follow-ups when the corresponding
slice is taken. Issue #1136's related evidence work does not establish coverage for every trajectory claim.

PR #1298 owns the separate issue-pattern audit. PRs #1254, #1156, #1159, and #1141 cover auth, settlement, restart,
and HTTP-read work. This report neither claims those files nor treats their target behavior as current capability.

## Recommended sequence

Three approaches are available. Leaving the docs unchanged has no editing cost but keeps the demonstrated broken
paths. Repairing every page in place minimizes navigation changes but retains competing examples and their future
maintenance cost. A full reorganization could clarify the taxonomy, but would create a large link migration before
the best replacement content is proven. Prefer **small correctness fixes paired with consolidation**.

1. **Repair the entrance and retire misleading routes.** Resolve the first-run composition and toolchain claim;
   use #457/#486 for the obsolete workflow path. Remove the stale roadmap's status role and correct the pending
   lifecycle label. Exit evidence: the exact documented first run reaches its stated observation, and current
   navigation no longer recommends retired implementations. Any required code fix needs its own runtime scope.
2. **Establish one checked component-authoring path.** Maintain the example code as compilable source, then shorten
   the tutorial and correct package/schema examples around it. Cover explicit registration, ports, decoding and
   tool arguments. Exit evidence: a builder can follow the path without searching history for missing wiring.
3. **Reconcile promises before polishing them.** Correct boot/restart, estimated tokens and observed evidence
   wording against reviewed contracts. Route #765's disagreement through contract review. Trace edge resource and
   offline claims to an enforcement path or measured scenario before retaining them as guarantees.
4. **Repair and consolidate current references.** Fix representative moved links, route each API fact to its
   maintained home, and keep human tutorials distinct from normative specs and history. Add focused link/example
   verification where it protects these maintained paths; avoid a repository-wide historical-link gate by default.
5. **Reduce instruction duplication deliberately.** Consolidate shared root context if both platform entry points
   retain required discovery and the three inline protocol gates. Check generated OpenSpec ownership and archive
   precedence. Preserve already-thin adapters; do not hand-delete generated pairs based on matching text alone.

The resulting navigation should start with explicit semantic context and a working Go app, use SemSource as the
first product walkthrough, and present structural/statistical/semantic capabilities with their operating costs.
Model generation, agentic loops, observable actions and optional human controls can then build on that foundation.
This preserves non-loop and edge use cases while making the agent-assisted builder's path clearer.

## Verification

The evidence received independent `INVENTORY PASS` after correcting the root-package census and adding source
counterexamples. All 133 source pins verify at the recorded baseline. The embedded census reproduces the reported
mechanical counts. Local `task build:default` and `task lint` pass with no production changes.
Final report review and artifact checks are recorded in the PR before it leaves draft.

[#1302]: https://github.com/C360Studio/semstreams/issues/1302
[#457]: https://github.com/C360Studio/semstreams/issues/457
[#486]: https://github.com/C360Studio/semstreams/issues/486
[#668]: https://github.com/C360Studio/semstreams/issues/668
[#765]: https://github.com/C360Studio/semstreams/issues/765
[#1002]: https://github.com/C360Studio/semstreams/issues/1002
[#1136]: https://github.com/C360Studio/semstreams/issues/1136
[#1218]: https://github.com/C360Studio/semstreams/issues/1218
[#828]: https://github.com/C360Studio/semstreams/issues/828
[#1260]: https://github.com/C360Studio/semstreams/issues/1260
[#1034]: https://github.com/C360Studio/semstreams/issues/1034
[#1163]: https://github.com/C360Studio/semstreams/issues/1163
[#1133]: https://github.com/C360Studio/semstreams/issues/1133

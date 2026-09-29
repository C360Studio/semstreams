# Design: lifecycle test cleanup audit and guard (#1064)

Status: revision 2 independently reviewed; accepted by the coordinating session within the existing issue scope.
No individual census or baseline entry is approved by this design.

Accepted inventory: `openspec/changes/test-cleanup-root-guard/inventory.md`
Inventory SHA-256: `18ae6cf5377c44c5e554f16499327dce9aeb5a06bcce2fc741141e86a4fb7683`
Inventory baseline: `f60d78906086f0c9090a99f24c389f32d40cc167`
The accepted inventory checkpoint is preserved verbatim in PR comment 5873595698.
The working inventory only shortened its task-checkbox pin after those tasks completed; runtime evidence is unchanged.

## Decision and alternatives

**Recommendation:** extend the existing test-infrastructure verification surface with a small, unexported,
type-aware cleanup classifier and a separate reviewed cleanup baseline.

| Option | Cost and result |
|---|---|
| Do nothing | Leaves the owner-required audit and early regression gate unmet |
| Add Stop matching to the existing syntax-only scanner | Smallest patch, but cannot satisfy receiver/provenance requirements |
| Extend testinfra with typed cleanup analysis | Reuses its fixture, baseline, and refusal patterns; meets the bounded scope |
| Introduce a general static-analysis framework | Larger API and maintenance burden; no measured consumer requires it |

Keep implementation under `test/testinfra`, using sibling test files and unexported types/functions.
No runtime helper, exported API, watchdog, or production lifecycle change is introduced.
Keep the existing sleep baseline and its matching behavior unchanged.

## Measured premises

- The infrastructure guard already rejects new, duplicate, and stale baseline entries.
- The production context guard already demonstrates `go/packages`, `go/types`, alias resolution, and load-error refusal.
- Current literal matches are 534 Background Stop lines; only 307 match same-line cleanup syntax.
- Root-module gopls component references increase from 510 to 801 when integration files are selected.
- Ordinary `.go` test support, live-provider tags, and a nested module make one `*_test.go` or default-build scan incomplete.
- A method selector is insufficient: current cleanup invokes a returned `stop(context.Background())` callback.
- Context-taking terminal owners include KeyedPool and CronScheduler, beyond LifecycleComponent and Service.
- Supplying a finite context does not establish wall-clock completion, error checking, or correct teardown ordering.

Each premise and its limits are supported by the accepted inventory.

## Source boundary and loading

Enumerate current, nonignored owned Go sources through Git, including tracked files and explicit untracked additions.
Report source counts and exclusions. Ignore VCS metadata, vendored dependencies, and nested worktree infrastructure.

Parse that source set before type loading so an omitted candidate cannot disappear silently.
Load the root module with tests enabled for default, integration, and live_llm build selections.
Loading does not execute provider tests or contact a model.
Deduplicate physical sites across package variants; retain the build selections admitting each site.

Inspect all `_test.go` files. Include ordinary `.go` test-support entry points accepting testing authority.
From these entry points, traverse only statically resolved paths needed to classify terminal cleanup ownership,
context provenance, and cleanup callback origin. Do not traverse every production function reachable from tests.

Report nested modules and otherwise unrepresented build selections separately.
A parsed, untyped file with no relevant candidate is a reported exclusion, not a certified safe file.
A candidate without sufficient type/provenance evidence is uncertain and blocks admission.
Parse failures prevent establishing candidate absence and must fail admission.

Analyzer fixtures are inline source strings or non-Go-suffixed fixture data, materialized into temporary modules.
Their intentionally invalid/unbounded examples are not checked-in `.go` inputs to the repository census.
Fixture source sets exercise the same analyzer through explicit internal inputs; they do not alter repository exclusions.

Package-load/type errors are infrastructure failures, never baselineable debt or a clean result.
Do not execute tests, run generators, modify source, or download new tooling to manufacture successful classification.

## Candidate and receiver boundary

Produce an auditable record for every admitted Stop call and context-taking cleanup callback:
location, enclosing function, cleanup origin, resolved target, receiver information, context provenance,
classification, and supporting evidence or unresolved reason.

Recognize:

- Type-resolved LifecycleComponent and Service Stop calls, including interface receivers.
- Repository-owned context-taking `Stop(context.Context) error` methods, including KeyedPool and CronScheduler.
- Method values, direct method expressions, type assertions, import aliases, and calls split across lines.
- Context-taking callbacks invoked by defer/Cleanup, even when their local name is not `Stop`.
- Reusable helper calls that register or perform terminal cleanup.

Record known concrete receiver types where available.
For interface dispatch, report the authoritative static interface and unresolved dynamic receiver explicitly;
do not invent a concrete implementation.

A standard ticker Stop is non-lifecycle.
Foreign native handles and repository-owned contextless Stop methods receive explicit records:
known contracts can establish exclusion from this context-taking guard; unresolved runtime ownership stays uncertain.
“Non-lifecycle” means outside the admitted contract, not leak-free or harmless.

## Bounded provenance analysis

Use a small abstract result: finite supplied context, unbounded supplied context, or unknown.
Track local bindings by type-resolved object identity, not identifier spelling.

Supported forms:

- Background/TODO roots; local aliases; assignments and reassignment.
- WithTimeout/WithDeadline and their cause variants.
- WithCancel/WithCancelCause inheriting their parent's deadline classification.
- WithoutCancel removing deadline authority; a subsequent finite constructor restores a finite classification.
- Direct closure capture, local function aliases, method values, and method expressions.
- Statically resolved helper parameters and context-returning helpers, including multiple return values.
- Helpers that register Cleanup callbacks or forward terminal callbacks.

Join branches conservatively. A finite classification requires every reachable supported source to be finite.
A cancellation-only context is not finite merely because its cancel function exists.
Typed `*testing.T.Context()` is deadline-free in the supported Go 1.26.3 CI and Go 1.26.4 local toolchains, so it is
unbounded provenance. The actual top-level and subtest contexts must have no Deadline in a toolchain contract test;
a future semantic change fails that check and requires reconciling this rule. A WithTimeout/WithDeadline descendant
still supplies a finite deadline. None of these facts establishes fresh cleanup authority.

Summarize only statically resolved helpers on the admitted cleanup/context paths, substituting caller provenance.
Memoize summaries; cycles, unsupported mutation/escape, unresolved higher-order flow, reflection, and ambiguous
callback origins become unknown. They must not disappear or become safe because analysis stopped.

Respect Go's evaluation timing. `defer owner.Stop(ctx)` captures its argument when the defer is registered.
A Cleanup closure reading `ctx` observes the captured variable when the callback executes.
Subsequent reassignment therefore can change the closure's classification without changing the direct defer's.
Unsupported ordering or escaping mutation remains uncertain.

For returned callbacks, follow a directly resolvable returned closure/method value.
The milestone subscriber example must either resolve through this supported path or remain an explicit uncertain site.
This is bounded source analysis, not whole-program dispatch or runtime termination proof.

## Cleanup ownership and five classifications

Maintain two independent facts: the five-way census classification and applicability to cleanup admission.

Admission covers lifecycle Stop/callback invocations owned by defer or testing Cleanup, including their supported
transitive helpers. Unresolved evidence that could conceal such an invocation blocks admission.
Ordinary calls positively established outside those cleanup paths remain visible census records requiring review.
Their unresolved classification does not independently block cleanup admission and does not establish safety,
boundedness, or deliberate contract intent.

Determine applicability from typed ownership paths, not merely the record's current `origin` string.
A helper's syntactically ordinary Stop inherits cleanup ownership when reached from defer/Cleanup.
A physical site reached through multiple paths retains all relevant ownership/provenance variants; an ordinary path
must not erase a cleanup-owned or unresolved path.
Exact legacy approvals and manual resolutions authorize admission only for the corresponding guarded sites.

| Classification | Treatment |
|---|---|
| Unbounded terminal cleanup root | Fails unless an exact reviewed legacy-debt entry matches |
| Already bounded cleanup/call | Passes this deadline-supply check; retains provenance and evidence limits |
| Deliberate lifecycle/API contract call | Requires an exact reviewed classification when intent is not structurally established |
| Non-lifecycle Stop | Retained with the resolved exclusion reason |
| Uncertain owner/provenance | Blocks when cleanup-owned or cleanup applicability is unresolved; positively ordinary census records remain visible for review |

### Bounded ownership accounting

Enumerate defer and typed testing Cleanup registrations throughout the admitted source set, including nested
function literals and reusable support bodies. Analyze their supported helper/callback paths independently of
ordinary-call inventory traversal.

An unresolved target, callback origin, helper cycle, or unsupported dispatch on a cleanup path produces a blocking
record at the unresolved edge. Analysis stopping is never evidence that the path contains no lifecycle cleanup.
Candidate-bearing callables passed to an unclassified registration/invocation surface retain unresolved ownership.
Known ordinary test-body invocation, including type-resolved testing Run callbacks, supplies ordinary ownership;
test names and assertion wrappers do not supply deliberate-contract classification.

An ordinary-only disposition requires a supported ordinary invocation path and no cleanup-owned or unresolved
ownership variant for that site within the admitted source model. Unreached helper declarations are not proven
ordinary merely because the analyzer initialized their origin to ordinary.

This is bounded ownership analysis of admitted sources and supported call paths. It does not prove whole-program
reachability, external adoption behavior, or reflective dispatch. Unsupported relevant edges remain explicit.

Do not infer deliberate contract intent solely from a test name, an assertion wrapper, or location outside Cleanup.
The two Background calls in `component/lifecycle_test_suite.go` must remain distinct records.

Human review may resolve an uncertain record only through an exact-site resolution record containing:

- The semantic site identity and one permitted resulting classification from the other four categories, or uncertain classification retained
  by the exact ordinary-only ownership disposition defined below.
- The specific unresolved question, reason for the classification, reviewer attribution, and owner/issue reference.
- A finite, explicit dependency list naming the source declarations and resolved symbols used as evidence.
- Fingerprints of the site, ownership/context evidence, and every declared review dependency.

Dependencies include helper bodies and any further declarations on which the manual conclusion relies.
Identify source dependencies by repository path and qualified declaration; fingerprint normalized AST and symbols,
including relevant build constraints. Identify dependency-owned symbols through their selected module/version evidence.
Unenumerable dependencies leave the site uncertain. Blanket, path-wide, and receiver-wide approvals are invalid.

Recompute these fingerprints even when automatic analysis still returns the same unknown result.
A dependency changing or disappearing invalidates the resolution and fails as stale approval.
Resolving a site to legacy unbounded debt additionally requires its exact legacy-debt approval.
Package/type-load failures cannot be overridden. An unresolved cleanup-relevant uncertain entry never authorizes admission.

## Identity, baseline, and report

Use a separate `test/testinfra/cleanup_baseline.json`.
Reuse the existing baseline validation and bidirectional reconciliation pattern without altering sleep policy.

A semantic site identity contains:

1. Repository-relative path and qualified enclosing function/method.
2. Cleanup origin and lexical ownership path.
3. Resolved callable declaration identity and receiver/binding origin.
4. Normalized context-provenance descriptor.
5. Occurrence ordinal among otherwise identical semantic sites.

Line numbers are diagnostics only. Do not key approval by raw source text.

Attach an evidence fingerprint derived from normalized relevant AST and resolved symbols:
the enclosing ownership/context logic, helper summaries actually used, and explicit manual-review dependencies.
Whitespace, comments, and line movement do not change it; relevant semantic changes invalidate approval.

An additional otherwise identical occurrence increases cardinality and creates an unmatched ordinal.
An identical replacement preserving cardinality and all relevant semantic evidence is not historically distinguishable.
This identity scheme approves current semantic evidence; it does not reconstruct edit history.

Removed sites, changed classifications, changed evidence, duplicate entries, and obsolete approvals fail explicitly.
A legacy debt entry requires a reason and owner/issue reference.
A reviewed contract classification must not authorize moving the call into cleanup.

The guard never writes or refreshes its own approval baseline.
Its report emits sorted candidates and proposed classifications for review.
Initial known-debt baseline creation follows independent source-based review of the exact census records,
not acceptance of historical grep counts. This review can approve already classified debt; unresolved ownership
still requires an explicit, evidence-backed disposition and cannot be waived by baseline generation.

## Invariants and specification home

The analyzer has no current capability specification. Its invariants therefore require a new
`test-cleanup-policy` spec before implementation; the formal delta follows design review.

Proposed requirement homes and obligations:

- **Source accounting:** every selected candidate is classified or reported uncertain; load failures cannot pass.
- **Cleanup context classification:** finite status follows supported provenance, never spelling alone.
- **Exact reviewed debt:** only matching reviewed legacy debt is grandfathered.
- **Baseline freshness:** removed or materially changed automatic or manually declared evidence fails reconciliation,
  including changed helper dependencies while the call site and automatic unknown result remain unchanged.
- **Identity stability:** formatting, comments, and ordinary line movement preserve semantic identities.
- **Early refusal:** new debt, stale approval, unresolved uncertainty, and parse/type-load failure prevent admission
  through the named full-suite verification entry points.
- **Evidence limits:** classification makes no wall-clock return, join-completion, or teardown-order guarantee.

These are proposed analyzer requirements, not claims that the existing lifecycle spec already specifies this guard.

## Test-first verification

Use small typed fixture packages with independent expected classifications.
Fixtures must exercise:

- One-line/multiline calls; defer and Cleanup closures; nested helpers.
- Aliased/dot context imports; method values and expressions; interface/type-asserted receivers.
- Background/TODO, cancellation-only descendants, finite descendants, deadline removal, and reassignment.
- Finite context-returning helpers and helpers registering cleanup.
- Known returned callbacks and unresolved callback provenance.
- Deliberate nil/error contract calls and explicit cleanup outside defer/Cleanup.
- Tickers, native handles, contextless local owners, and additional context-taking runtime owners.
- Build selection, ordinary-file helpers, source omissions, and type-load failures.
- New/deleted/duplicated debt, stale classifications, changed helper evidence, and stable line movement.

- Paired direct-defer and Cleanup-closure calls with identical initial context and subsequent reassignment:
  direct defer retains the evaluated argument; the closure observes the later binding.
- Manual resolution whose helper evidence changes while its call site and automatic unknown result remain unchanged:
  the old resolution must fail as stale.
- Missing/unresolvable review dependencies, blanket approvals, and attempted load-failure overrides.
- Identical-site cardinality increase and stable equal-cardinality semantic identity, without claims about edit history.

Formatting invariant: exhaust the twelve current transformations (zero through five blank lines, with and without
an inserted comment) using the actual typed analyzer. Compare each independently with the baseline semantic identity,
fingerprint and classification. This replaces repeated random sampling of that finite domain; it guarantees every
current input without duplicate module loads. Named examples separately cover semantic changes, admission and
provenance branches. Report exhaustive finite-domain coverage, not Rapid execution. Independent implementation
review accepted this proof correction before the final measurement.

Mutation evidence is required for this enforcement change.
Select bounded faults that make an unbounded root pass, suppress stale detection, or treat unknown/load failure as clean.
Record passing baseline, valid mutant reaching the intended assertion, detected failure, and restored pass.

## Early gate and measured cost

Use one internal script, `scripts/check-cleanup-roots.sh`, as the shared invocation:
`go test -count=1 ./test/testinfra -run '^TestCleanupRootGuard$'`.
It runs the complete three-selection census and reconciliation, without executing integration/live-provider tests.
The exact named test and script are implementation targets, not existing interfaces.

CI Lint and Test currently run independently. A Lint-only guard cannot prevent Test from starting expensive work.
Place admission on the actual full-suite paths:

| Entry point | Required sequential ordering |
|---|---|
| `task lint` | Cleanup guard before existing lint commands |
| `task test` | Cleanup guard before `go test ./...` |
| `task test:race` | Cleanup guard before `go test -race ./...` |
| `task test:live` | Cleanup guard before the repository-wide live-provider test command |
| `task check` | Existing sequential lint then test; both preserve their admission boundary |
| `task check:push` | Move lint before build; guard is lint's first command, preceding contract/race/integration phases |
| `task test:integration` | Its existing integration runner performs admission |
| Direct full integration runner | Guard before host-lock acquisition, Docker inspection/pull, or tagged `go test` |
| CI Test job | Existing `scripts/run-integration-tests.sh` command reaches that same runner admission boundary |
| CI Lint job | Invoke the same guard before its existing lint commands; this is additional feedback, not Test admission |

Task ordering uses sequential `cmds`, not parallel dependencies.
No CI job dependency on the entire Lint job is needed.
Build/schema/E2E workflow redesign remains outside this change.

For the integration runner, no package arguments and an explicit `./...` select full-suite admission.
Normalize its package arguments before lock acquisition.
Focused package arguments retain the existing iteration path without forcing a repository-wide three-build scan.
Likewise, direct focused `go test ./some/package -run ...` remains available.
Focused execution is not evidence that the repository cleanup guard passed.

Do not add a caller-controlled “already checked” or skip flag.
Initially accept repeated guard invocations in composed full gates and measure their aggregate cost.
Any deduplication requires measured need and evidence freshness; it must not create a bypass.

Prove ordering through the checked-in Task definitions and integration runner, not a separate illustrative wrapper.
The subprocess fixture harness invokes each named Task entry point and the runner in a temporary fixture workspace.
Prove all five classifier outcomes (clean, new debt, stale approval, cleanup uncertainty, and load failure) through
the canonical full-suite runner. Prove clean admission and representative new-debt rejection through every additional
actual entry point. Every executed fixture uses the real classifier/reconciler. A harness-only executable shim may
dispatch the guard invocation to that fixture analysis and mark subsequent Go/Docker/build commands with sentinels;
it must not substitute hard-coded success/failure for analyzer outcomes.

Verify that every path invokes the same shared guard sequentially and propagates failure uniformly. Record the
canonical rejection exit statuses. Entry wrappers must not inspect classification output or conditionally handle
failure categories; if a path handles distinct outcomes differently, exercise those additional outcomes there.
Structural checks reject ignored guard errors, success-forcing command substitutions, or conditional handling that
breaks the uniform-failure premise.

For each executed failing fixture, assert nonzero exit and absence of expensive-command sentinels.
For each clean fixture, assert admission and the expected subsequent command.
For the runner, also assert failures occur before lock acquisition. Retain sensitivity evidence for omitted guard,
guard after expensive work, and continuation after guard failure.
Execute the actual CI Test `run` command extracted from the workflow and verify that it still targets this runner.
Add wiring assertions for command ordering and exact guard invocation so changing Task/CI paths breaks the fixture.
Focused-package fixtures must demonstrate preserved iteration behavior.

Measure cold and warm guard duration, selected packages/files, peak resources, and aggregate full-gate overhead.
Record actual cost in the PR before proposing budgets or deduplication.
A slow or failed load is a finding to resolve, not grounds to weaken coverage, invent a tiny timeout,
or pass optimistically.

## Delivery and remediation boundary

Draft implementation tasks after design review:

1. Materialize the analyzer spec and fixture expectations.
2. Implement source accounting, typed classification, semantic identity, and baseline reconciliation.
3. Produce the complete census; independently review classifications and exact legacy entries.
4. Wire and prove early gate refusal; measure cold/warm cost.
5. Run focused race checks, mutation evidence, independent implementation review, and required preflight.
6. Record evidence-driven package repair batches; synchronize spec/archive at completion.

This change establishes audit truth and rejects new debt. It does not mechanically replace existing contexts,
rewrite all cleanup, weaken assertions, or repair production Stop implementations.
Package remediation preserves its own review and rollback boundary.
No new owner policy decision is requested beyond accepting this concrete implementation scope.

## Scope correction from implementation census

The initial design conflated inventory uncertainty with cleanup admission. #1064 explicitly requires a broad census
and a guard for defer/Cleanup roots. The correction above preserves ordinary uncertainty for review without inventing
contract intent or baseline approvals. The second provisional census contained 469 ordinary unknowns, not 469 proven
cleanup violations. Subsequent nested-callback enumeration changed those counts; no provisional census is an accepted
baseline. Ordinary finalization can still hang and remains remediation evidence, including the two distinct shared
lifecycle-suite sites. A universal rule for ordinary Stop calls would be a separate scope decision.

Independent review passed for this correction; the exact reviewed hashes and coordinator acceptance are recorded in
PR #1414 comment 5874372507. It does not waive source accounting, type-load errors, unresolved cleanup dispatch, exact
approvals, or baseline freshness.

## Admission proof cost correction

The initial 10-entry by 5-outcome Cartesian matrix measured 34.029s normally and 111.621s with the race detector
(112.18s wall time, 1,140,293,632 bytes maximum resident set size on this host). The race run used CI-pinned Task
v3.53.1 and Go 1.26.4 on darwin/arm64; classifier code was not yet frozen. This is fixture overhead, not a final
repository guard measurement or portable performance budget.

The revised witness set above has 23 matrix cases: five canonical outcomes and clean/reject for each of
nine additional paths. It factors classification from uniform wrapper propagation rather than changing race runtime
configuration, introducing result caching, skipping race coverage, or replacing the analyzer with canned outcomes.
The countercase is classification-specific wrapper behavior; exact shared invocation and uniform-status checks are
required, with additional outcomes whenever that premise stops holding. Independent design review passed at SHA-256
`bcd9d3c0158312bc0da746ef72c7b5e60ced475ad5341b40f7732830b8fd73c3`. The stale fixture also invokes the analyzer
while preparing its baseline; 23 matrix cases do not mean exactly 23 analyzer executions.

## Testing context evidence

Go 1.26.3 `src/testing/testing.go` returns `c.ctx` from Context (lines 1513–1515); T.Run constructs it with
WithCancel(Background) at line 1963, and runTests does the same at line 2427. Local Go 1.26.4 has the same semantics
at lines 1589, 2071 and 2565. Official versioned source:
<https://raw.githubusercontent.com/golang/go/go1.26.3/src/testing/testing.go>.

Architect and independent reviewer accepted the narrow typed provenance rule with the behavioral toolchain check.
It avoids a patch-version-specific manual approval or a new standard-library dependency fingerprint mechanism.

## Exact manual ownership disposition

The remaining shared-wrapper and reflective-suite records expose a representation limit: some exact source paths
can be established as ordinary test-body invocation while their lifecycle intent remains uncertain. Calling every
ordinary finalizer a deliberate contract test would be false. The existing five-way classification therefore stays
separate from an exact reviewed ownership disposition.

A manual resolution may optionally request `applicability: ordinary-only` only when the automatic record has
classification `uncertain-owner-provenance` and applicability `unresolved`. In that form its classification must
remain `uncertain-owner-provenance`. It must carry the exact identity, unchanged site fingerprint, ownership question,
source-based reason, reviewer, owner issue, and finite explicit evidence dependencies. It cannot override automatic
`cleanup-owned` applicability, source/load failure, or other records/ownership variants. If automatic analysis later
resolves its applicability, the manual resolution is obsolete and must fail as stale until removed.

The source review must enumerate the actual caller/registration evidence used to establish ordinary-only ownership.
A changed dependency fails as stale. Adding a new cleanup caller or unresolved invocation edge must create its own
blocking variant or invalidate the reviewed record; matching one ordinary disposition must never suppress that edge.
If this cannot be established for a proposed record, leave it blocking instead of broadening the resolution.

Source dependencies may select an exact type declaration or a receiver-qualified method using `Type.Method` as well
as an unambiguous function/value declaration. Pointer/value receiver shape remains in its normalized fingerprint.
An unqualified method name shared by multiple receivers is ambiguous and must be rejected. Fingerprints include
relevant imports, build constraints and declaration/type evidence, normalize ordinary comments away, and fail closed
when dependency bindings or module replacement identity cannot be established. This is a bounded review-record
format, not automatic analysis of reflective dispatch or all receiver implementations.

Required controls use the actual analyzer/reconciler: exact unknown ordinary disposition succeeds while preserving
unknown classification; matching an automatic cleanup-owned record is refused; a new cleanup or unresolved caller reaching the same physical helper after installing the disposition
still fails; changing/removing a dependency or automatic resolution makes the approval stale; receiver-qualified
methods select the intended declaration; type changes invalidate evidence; ordinary comment changes preserve it,
while build-constraint changes invalidate it. No existing unknown is approved by adding this capability.

## Named manager terminal boundary

The exact resolved `github.com/c360studio/semstreams/service.(*Manager).StopAll(context.Context) error` contract is a
named terminal lifecycle boundary. Record the invocation at its caller using the same context-provenance and
ownership rules as admitted Stop calls; do not descend into its production dispatch implementation. An identically
named method on another receiver is not admitted by spelling and remains subject to ordinary helper/callback
analysis. Aliases, method values and method expressions preserve the same typed identity; ambiguous bindings remain
uncertain. Ordinary StopAll calls remain ordinary census evidence, without an inferred contract-test intent.

This boundary follows `service/service_manager.go:838`, its exact caller-context dispatch at lines 886 and 904,
`openspec/specs/service-shutdown/spec.md` lines 18–22, and the context contract exercised by
`service/lifecycle_context_contract_test.go` lines 115–146. The actual deferred Background caller at
`service/component_manager_integration_test.go:258` supplies the cleanup authority and is the reviewable debt site.
The three production dispatch sites found by unconstrained traversal are not separate test-owned cleanup roots.

Paired fixtures must cover deferred Background, bounded context, ordinary invocation, alias/method forms, and an
identically named method on another receiver. Wrappers around the admitted boundary must preserve cleanup ownership.
Each newly exposed debt entry still requires independent exact source review.

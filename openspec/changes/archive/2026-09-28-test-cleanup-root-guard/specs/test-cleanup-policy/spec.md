## ADDED Requirements

### Requirement: Source accounting

The guard SHALL enumerate nonignored repository-owned Go sources through Git, including tracked files and
explicit untracked additions, and parse the source set before type loading.
It SHALL report source counts and exclusions and load root-module tests for default, integration, and live_llm
build selections without executing those tests. Physical sites SHALL be deduplicated across package variants.

The guard SHALL inspect test files and ordinary-file test-support entry points accepting testing authority.
Helper traversal SHALL follow only statically resolved cleanup-ownership, context-provenance, and callback-origin
paths. It SHALL NOT promise analysis of all production code reachable from tests.

Relevant candidates SHALL include Stop calls, context-taking terminal callbacks, and cleanup helpers.
Nested modules and unrepresented build selections SHALL be reported separately.
Untyped candidate-bearing files SHALL produce uncertainty; parsed untyped files without candidates SHALL be
reported exclusions. Parse or package/type-load failures SHALL prevent admission and SHALL NOT be overridable.
Intentionally negative fixtures SHALL use inline strings or non-Go-suffixed data materialized into temporary modules.

#### Scenario: Build selections and reusable support
- **GIVEN** candidates in integration tests, live_llm tests, and an ordinary-file shared test helper
- **WHEN** the repository guard runs
- **THEN** each candidate SHALL be accounted for with its admitting build selections and classification.

#### Scenario: Source outside successful type loading
- **GIVEN** a nested-module or otherwise unrepresented source file
- **WHEN** parsing establishes a relevant candidate without sufficient type evidence
- **THEN** the guard SHALL report uncertainty and refuse admission.
- **AND** a parse or load failure SHALL fail admission rather than establish candidate absence.

### Requirement: Cleanup context classification

Each admitted record SHALL identify its location, enclosing function, cleanup origin, resolved callable,
available receiver evidence, context provenance, classification, and unresolved reasons.
The five classifications SHALL be unbounded terminal cleanup, already bounded cleanup/call, deliberate
lifecycle/API contract call, non-lifecycle Stop, and uncertain owner/provenance.

Classification SHALL use type-resolved bindings and supported provenance, including aliases, assignments,
method values/expressions, closures, and statically resolved helper parameters and return values.
Finite constructors SHALL establish finite supplied contexts; cancellation-only constructors SHALL inherit
their parent's deadline classification; WithoutCancel SHALL remove it until a finite constructor restores it.
All reachable supported sources SHALL be finite to establish automatic finite classification.
Unsupported flow SHALL remain uncertain. Interface dispatch SHALL NOT invent a concrete receiver.
Explicit finalization outside defer/Cleanup SHALL remain visible; spelling, assertions, or test names alone
SHALL NOT establish deliberate contract intent.

The guard SHALL recognize the type-resolved
`github.com/c360studio/semstreams/service.(*Manager).StopAll(context.Context) error` as a terminal lifecycle boundary.
It SHALL classify the caller's supplied context and cleanup ownership without recursively enumerating its production
dispatch implementation as new cleanup roots. Method spelling alone SHALL NOT admit another receiver's method.

Census classification and applicability to cleanup admission SHALL be recorded separately.
The guard SHALL propagate defer/Cleanup ownership through supported helpers and callbacks.
Ordinary syntax within a helper SHALL NOT establish ordinary-only applicability.
A site with cleanup-owned or unresolved ownership variants SHALL NOT be exempted by another ordinary path.
Positively ordinary-only calls SHALL remain visible census records requiring review; unresolved classification
alone SHALL NOT block cleanup admission for those records or establish deliberate contract intent.

#### Scenario: Manager cleanup boundary
- **GIVEN** a deferred Background StopAll invocation on the resolved service Manager
- **WHEN** the guard analyzes that cleanup path
- **THEN** it SHALL emit unbounded terminal cleanup at the caller and no synthetic internal dispatch roots.
- **AND** a finite supplied descendant SHALL retain finite classification under the ordinary provenance rules.

#### Scenario: Ordinary census uncertainty
- **GIVEN** a Stop invocation positively established outside cleanup paths
- **WHEN** its context or contract intent remains unresolved
- **THEN** the census SHALL retain that uncertainty without requiring a passing-baseline approval.

#### Scenario: Ordinary syntax inside a cleanup helper
- **GIVEN** a helper containing an ordinary Stop invocation
- **WHEN** defer or testing Cleanup reaches that helper
- **THEN** the invocation SHALL retain cleanup applicability and its unbounded or unresolved evidence SHALL block
  unless an applicable exact reviewed approval resolves admission.

#### Scenario: Unresolved cleanup dispatch
- **GIVEN** a cleanup registration whose helper or callback target cannot be resolved
- **WHEN** ownership analysis reaches that edge
- **THEN** the guard SHALL report blocking uncertainty rather than infer absence of cleanup.

#### Scenario: Finite provenance through a helper
- **GIVEN** a statically resolved helper returning a context derived with WithTimeout
- **WHEN** cleanup receives that result through a local alias
- **THEN** the guard SHALL classify the supplied context as finite and retain its supporting provenance.

#### Scenario: Deferred argument versus captured variable
- **GIVEN** a finite ctx later reassigned to Background after registration
- **WHEN** comparing `defer owner.Stop(ctx)` with a Cleanup closure that later calls `owner.Stop(ctx)`
- **THEN** the direct defer SHALL retain the evaluated finite argument and the closure SHALL reflect reassignment.
- **AND** unsupported ordering or escaping mutation SHALL remain uncertain.

### Requirement: Exact reviewed debt

Only an exact matching reviewed legacy-debt entry SHALL permit admission of an unbounded terminal cleanup site
within the guarded ownership population.
The separate cleanup baseline SHALL require a reason and owner/issue reference and SHALL NOT refresh itself.
Cleanup-owned uncertainty and unresolved cleanup applicability SHALL NOT be grandfathered as passing debt.
Ordinary-only census records SHALL NOT require fabricated approvals to permit cleanup admission.
Deliberate contract classification SHALL still require exact review when structural evidence cannot establish it.

A manual resolution SHALL record its exact semantic site, permitted resulting classification, unresolved question,
reason, reviewer attribution, owner/issue reference, and finite explicit source/symbol evidence dependencies.
Dependencies SHALL identify declarations and relevant build constraints; dependency-owned symbols SHALL identify
selected module/version evidence. Unenumerable dependencies SHALL leave the site uncertain.
Blanket, path-wide, receiver-wide, and load-failure approvals SHALL be rejected.
Manual classification as legacy unbounded debt SHALL additionally require the exact legacy approval.

A manual ordinary-only ownership disposition MAY retain uncertain classification while resolving an automatic
unresolved applicability. It SHALL require exact site and finite explicit caller/registration evidence dependencies
under the same freshness rules. It SHALL NOT override automatic cleanup ownership, source/load failure, or another
ownership variant. Automatically resolved applicability SHALL make the manual disposition stale. New cleanup or
unresolved invocation paths SHALL remain blocking even when another path has an ordinary-only disposition.
Source evidence dependencies SHALL support exact type declarations and receiver-qualified methods; ambiguous
unqualified methods SHALL be refused. Ordinary comments SHALL NOT alter dependency fingerprints, while relevant
build constraints and binding changes SHALL invalidate them.

#### Scenario: New debt and unresolved uncertainty
- **GIVEN** a new unbounded cleanup site or an unresolved cleanup-relevant uncertain site
- **WHEN** no applicable exact approval resolves admission
- **THEN** the guard SHALL fail and report the site and reason.

#### Scenario: Reviewed ordinary ownership with uncertain intent
- **GIVEN** an exact unresolved record whose explicit source dependencies establish ordinary-only invocation
- **WHEN** a reviewed ownership disposition resolves only that applicability
- **THEN** its census classification SHALL remain uncertain and its dependencies SHALL be checked for freshness.
- **AND** a new cleanup or unresolved ownership variant SHALL still fail admission.

#### Scenario: Attempted blanket resolution
- **GIVEN** an approval covering an entire path or receiver without exact site and evidence dependencies
- **WHEN** baseline validation runs
- **THEN** the guard SHALL reject the approval.

### Requirement: Baseline freshness

The guard SHALL reconcile findings and approvals bidirectionally and reject duplicate, removed, obsolete,
or materially changed approvals. Evidence fingerprints SHALL include normalized ownership/context logic,
used helper summaries, and every explicit manual-review dependency.
Manual dependencies SHALL be rechecked even when automatic analysis continues to return the same unknown result.

#### Scenario: Manual helper evidence changes
- **GIVEN** an exact manual resolution relying on a helper declaration
- **WHEN** that helper changes while the call site and automatic unknown result remain unchanged
- **THEN** the previous resolution SHALL fail as stale.

#### Scenario: Approved site disappears
- **GIVEN** an approved legacy site
- **WHEN** the site is removed but its baseline entry remains
- **THEN** reconciliation SHALL fail with a stale-entry diagnostic.

### Requirement: Identity stability

Semantic site identity SHALL include repository-relative path, qualified enclosing declaration, cleanup ownership
path, callable and receiver/binding origin, normalized context provenance, and an ordinal among identical sites.
Line numbers SHALL be diagnostic only. Formatting, comments, and ordinary line movement SHALL preserve identity.
An additional identical occurrence SHALL exceed approved cardinality.
An equal-cardinality replacement preserving all relevant semantic evidence SHALL NOT be claimed historically
distinguishable by this identity scheme.

#### Scenario: Formatting-only transformation
- **GIVEN** a classified fixture and baseline
- **WHEN** only whitespace, comments, or ordinary line placement change
- **THEN** semantic identities, classifications, and evidence fingerprints SHALL remain unchanged.

#### Scenario: Identical additional occurrence
- **GIVEN** one approved occurrence
- **WHEN** an otherwise identical second occurrence is added
- **THEN** the additional ordinal SHALL remain unapproved and fail admission.

### Requirement: Early refusal

A shared guard invocation SHALL precede expensive execution in task lint, test, test:race, and test:live.
Task check SHALL preserve sequential lint/test ordering; check:push SHALL run lint before build and subsequent
contract, race, and integration phases.
The integration runner SHALL run the guard before host-lock acquisition or Docker activity for no-argument and
explicit ./... full-suite selections. CI Test SHALL reach this boundary through its actual runner command,
independently of CI Lint; CI Lint SHALL also invoke the guard before its existing lint commands.
Focused package execution SHALL remain available without requiring the full three-selection census.
Focused success SHALL NOT constitute repository guard evidence. Caller-controlled bypass flags SHALL NOT be added.

#### Scenario: Refusal through actual verification entry points
- **GIVEN** fixtures producing new guarded debt, stale approval, cleanup-relevant uncertainty, or load failure
- **WHEN** actual Task entry points and the CI-selected integration runner execute
- **THEN** each SHALL fail before expensive-command sentinels or integration lock acquisition.
- **AND** the corresponding clean fixture SHALL admit the subsequent command.

#### Scenario: Focused iteration
- **GIVEN** a focused package invocation rather than a full-suite selection
- **WHEN** the existing iteration path executes
- **THEN** it SHALL remain usable without automatically requiring the repository-wide census.

### Requirement: Evidence limits

Finite-context classification SHALL mean only that supported evidence establishes a supplied deadline.
Reports SHALL NOT claim guaranteed wall-clock return, joined work, asserted Stop errors, fresh cancellation
authority, or correct teardown ordering from that classification.
Non-lifecycle exclusion SHALL NOT imply leak freedom. Unknown ownership or provenance SHALL remain explicit.
Cold/warm runtime, source/package counts, peak resources, and composed-gate overhead SHALL be measured before
setting performance budgets; slowness or load failure SHALL NOT authorize optimistic admission.

#### Scenario: Synchronous Stop with finite context
- **GIVEN** a synchronous Stop call receiving a finite context
- **WHEN** its implementation might ignore cancellation
- **THEN** the report SHALL describe finite deadline supply without claiming hang containment.

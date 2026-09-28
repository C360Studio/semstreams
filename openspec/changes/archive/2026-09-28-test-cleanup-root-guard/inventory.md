# Inventory: lifecycle test cleanup roots (#1064)

base: f60d78906086f0c9090a99f24c389f32d40cc167

Repository: SemStreams. Worktree: `/Users/coby/.codex/worktrees/gh1397-runner-tests/semstreams`.
The inspected tree was clean. No source changes or tests were performed.

This inventories the implementation seams, observed population shapes, and classification limits needed to design the
audit and guard. **The issue's complete AST/type-aware cleanup census has not been implemented or completed.**
Textual matches and gopls references below are separate measurements, neither a reviewed debt baseline.

## 1. Claimed gap and authority

Issue [#1064](https://github.com/C360Studio/semstreams/issues/1064) requires an AST/type-aware census covering direct and
multiline calls, defer/Cleanup ownership, aliases, method expressions, context provenance, bounded helpers, and concrete
receiver identity. Its five classifications are unbounded terminal cleanup, bounded cleanup, deliberate contract call,
non-lifecycle Stop, and uncertain ownership. The guard must reject new debt and stale exact baseline entries.

The [owner ruling](https://github.com/C360Studio/semstreams/issues/1064#issuecomment-5873261389), read in full, makes this
a beta.163 gate. It preserves separate package repair boundaries, component-before-substrate cleanup, fresh finite
terminal contexts, and observable Stop errors. A deadline is a cooperative bound; production changes require separate
deterministic proof. Historical counts are not current debt totals.

The active proposal and tasks were read in full:

- `openspec/changes/test-cleanup-root-guard/proposal.md:18` — `This is the initial claim, not an accepted analyzer design. The historical textual counts are hypotheses to`
- `openspec/changes/test-cleanup-root-guard/tasks.md:5` — `1.1 Produce a repository-first, line-pinned inventory of cleanup ownership, receiver/context classification,`

The historical next-tag inventory/design were inspected as provenance. They describe the earlier syntax-only census and
the causal #1062 correction; their counts and old line locations are not reused as current classifications.

Current `component-lifecycle`, `runtime-context-ownership`, and `service-shutdown` specifications were read in full.
They distinguish controlled shutdown from abort cleanup, reject nil, and do not promise running-generation rejoin after
a Stop deadline. They also distinguish component/service lifecycle from the workflow Lifecycle harness.

- `component/lifecycle.go:63` — `type LifecycleComponent interface {`
- `component/lifecycle.go:67` — `Stop(ctx context.Context) error`
- `service/base.go:481` — `type Service interface {`
- `service/base.go:489` — `Stop(ctx context.Context) error`

Testing policy establishes infrastructure ownership and the cleanup-context exception:

- `docs/contributing/01-testing.md:416` — `owns its client and container and registers`
- `docs/contributing/01-testing.md:471` — `Cleanup is the intentional exception. The Go test runner cancels`
- `docs/contributing/01-testing.md:473` — `Those independent contexts preserve the measured 10-second cleanup ceiling`

No existing inspected guard classifies Stop cleanup roots. The existing infrastructure guard's inspected classifier
handles container starts, direct container APIs, fabricated testing values, and integration sleeps. The existing
type-aware context guard examines production storage of context authority, not test call provenance.

## 2. Current population measurements

### Textual source measurements

The source set was `git ls-files '*_test.go'`; patterns were applied to individual lines without parsing. Comments and
strings were not excluded. Counts therefore describe **matched lines**, not semantic calls.

| Pattern | Matched lines | Files |
|---|---:|---:|
| `.Stop(context.Background())` | 534 | 135 |
| Same-line `defer` or `.Cleanup(` followed by that spelling | 307 | 96 |
| `.Stop(context.TODO())` | 0 | 0 |
| `.Stop()` | 133 | 52 |
| `.Stop(nil)` | 54 | 38 |

Largest same-line cleanup concentrations: graph-query 39, graph-gateway 32, graph-ingest 30, graph-index 26,
output/websocket 22, rule 21, agentic-loop 17, graph-clustering 16, and service 16.

The zero TODO result excludes neither aliases nor variables derived from TODO. The no-argument population includes
comments, tickers, native consumer handles, and runtime owners.

### Structural measurements

The initial sandboxed gopls load failed because its Go build cache was inaccessible. After a cache-access escalation,
the bounded structural queries succeeded without diagnostics:

| gopls query | Build selection | Results |
|---|---|---:|
| `implementation component/lifecycle.go:63:6` | default | 51 |
| `references component/lifecycle.go:67:2` | default | 510 |
| Same component Stop references | `GOFLAGS=-tags=integration` | 801 |
| `implementation service/base.go:481:6` | default | 26 |
| `references service/base.go:489:2` | integration | 176 |

Implementations include test doubles and source helpers. References include deliberate contract calls and ordinary
calls outside cleanup. These counts are not additive and do not establish cleanup ownership or context boundedness.

Component implementation families include input/output/gateway components, graph and agentic processors, rule,
gated-DAG, research processors, storage, examples, the test error wrapper, and service test doubles. Service
implementations include BaseService, managers, heartbeat, logging, metrics, milestones, storage observability, and
test doubles.

Source-set boundaries matter:

1. The tracked test build-constraint search found 251 `integration` and four `live_llm` lines.
2. Root `./...` does not enumerate the nested module below.
3. Reusable test support exists in ordinary `.go` files.
4. Filesystem parsing and selected-build type loading are different populations.

- `docs/experiments/ooze-pilot/harness/go.mod:1` — `module semstreams-ooze-pilot`
- `component/lifecycle_test_suite.go:455` — `comp.Stop(context.Background())`

No complete accounting of excluded build variants, duplicate test-package views, nested-module calls, or unsupported
forms has been produced yet.

## 3. Observed classifications and spelling variants

These are inspected examples of the required classes, not an exhaustive manifest.

### Confirmed unbounded lifecycle cleanup

The gateway test starts a real component then defers its Stop with Background:

- `gateway/graph-gateway/query_test.go:403` — `require.NoError(t, comp.Start(context.Background()))`
- `gateway/graph-gateway/query_test.go:404` — `defer comp.Stop(context.Background())`

A type-asserted interface receiver is also present:

- `processor/graph-index/replacement_reconcile_integration_test.go:426` — `defer clusteringComponent.(component.LifecycleComponent).Stop(context.Background())`

The latter is **not** a method expression such as `(*Type).Stop(receiver, ctx)`. A broad method-expression-shaped text
probe found this false positive; it established no current true method-expression occurrence.

### Confirmed finite supplied contexts

The corrected rule readiness test creates its terminal context inside Cleanup, keeps NATS available, and reports errors:

- `processor/rule/readiness_integration_test.go:92` — `stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)`
- `processor/rule/readiness_integration_test.go:94` — `if err := processor.Stop(stopCtx); err != nil {`

The lifecycle cohort cleanup shares one finite context across its owned processors:

- `processor/rule/lifecycle_integration_test.go:42` — `ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)`
- `processor/rule/lifecycle_integration_test.go:52` — `if err := processor.Stop(ctx); err != nil {`

A helper establishes a finite cleanup context but discards the Stop result:

- `service/component_manager_lifecycle_test.go:16` — `stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)`
- `service/component_manager_lifecycle_test.go:18` — `_ = cm.Stop(stopCtx)`

Thus finite-context classification does not establish error observability or complete policy conformance.

A context-returning helper supplies the bound indirectly:

- `pkg/dispatch/keyed_pool_test.go:18` — `func stopCtx(t *testing.T) context.Context {`
- `pkg/dispatch/keyed_pool_test.go:20` — `ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)`
- `pkg/dispatch/keyed_pool_test.go:22` — `return ctx`

Finite detachment preserves parent values while discarding its ended authority:

- `service/stream_override_expiry_test.go:292` — `stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(runtimeCtx), 30*time.Second)`

The production failed-Start helper already owns a separate finite-callback shape:

- `internal/lifecyclecleanup/lifecyclecleanup.go:33` — `ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)`
- `internal/lifecyclecleanup/lifecyclecleanup.go:36` — `rollbackErr := rollback(ctx)`

These calls are synchronous. A finite context alone proves neither wall-clock return nor successful join. Creation
timing, prior cancellation, deadline expiry, and ordering relative to substrate teardown are separate observations.

### Deliberate lifecycle/API contract calls

Nil rejection is deliberately tested outside cleanup:

- `service/lifecycle_context_contract_test.go:62` — `err = svc.Stop(nil)`

The shared error-injection helper has two distinct Background calls: the first is the operation under assertion; the
second explicitly performs cleanup. A rule based only on filename or surrounding test function cannot equate them:

- `component/lifecycle_test_suite.go:445` — `err = comp.Stop(context.Background())`
- `component/lifecycle_test_suite.go:454` — `// Always try to clean up`
- `component/lifecycle_test_suite.go:455` — `comp.Stop(context.Background())`

### Non-lifecycle Stop

The same source file containing a returned terminal callback also stops a ticker:

- `agentic/agentrun/refusal_wiring_integration_test.go:152` — `ticker := time.NewTicker(10 * time.Millisecond)`
- `agentic/agentrun/refusal_wiring_integration_test.go:153` — `defer ticker.Stop()`

Name matching alone cannot distinguish this from lifecycle cleanup.

### Ownership and admission requiring explicit treatment

Lifecycle ownership extends beyond the two large interfaces:

- `pkg/dispatch/keyed_pool.go:365` — `func (p *KeyedPool[W]) Stop(ctx context.Context) error {`
- `processor/rule/cron_scheduler.go:381` — `func (s *CronScheduler) Stop(ctx context.Context) error {`

Neither requires `component.LifecycleComponent` conformance. KeyedPool's documentation states that deadline expiry
stops waiting while lanes continue draining. CronScheduler has its own owner-specific Stop semantics.

Contextless local runtime owners also exist; they are not automatically harmless because their signature differs:

- `graph/readiness/set_test.go:72` — `t.Cleanup(s.Stop)`
- `graph/readiness/set.go:85` — `func (s *Set) Stop() {`

A returned terminal callback contains no `.Stop` selector at its cleanup call:

- `agentic/agentrun/refusal_wiring_integration_test.go:70` — `stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{`
- `agentic/agentrun/refusal_wiring_integration_test.go:74` — `defer func() { require.NoError(t, stop(context.Background())) }()`

Its terminal purpose is evident from the surrounding source. Its admission to the new guard's lifecycle population and
the depth of callback provenance analysis remain unresolved; failure to follow that provenance cannot classify it safe.

Text probes found no named/dot `context` import alias in tracked test files. This is a limited spelling observation;
alias and true method-expression support remain explicit issue requirements. Method-value Cleanup registrations
currently occur for readiness owners, graph views, and native consumer handles.

## 4. Existing owners of the audit/guard problem shape

The problem shape is source enumeration → candidate classification → reviewed exception reconciliation → refusal of
new or stale debt. Existing instances supply different portions; none supplies the complete requested cleanup analysis.

### AST infrastructure ratchet

- `test/testinfra/policy_guard_test.go:30` — `func (f finding) key() string {`
- `test/testinfra/policy_guard_test.go:31` — `return strings.Join([]string{f.Category, f.Path, f.Function, f.Call, strconv.Itoa(f.Ordinal)}, "|")`
- `test/testinfra/policy_guard_test.go:62` — `if stats.GoFiles < 100 || stats.TestFiles < 100 || stats.IntegrationFiles == 0 || stats.Calls == 0 {`
- `test/testinfra/policy_guard_test.go:350` — `file, err := parser.ParseFile(fset, path, source, parser.ParseComments)`
- `test/testinfra/policy_guard_test.go:371` — `name = item.Name.Name`
- `test/testinfra/policy_guard_test.go:573` — `t.Errorf("duplicate baseline key %q", item.Key)`
- `test/testinfra/policy_guard_test.go:585` — `stale = append(stale, key)`

This guard walks `.go` files, skips selected infrastructure directories, recognizes import aliases syntactically,
checks build constraints, validates baseline metadata, and reports both unexpected and stale findings. Its existing
identity survives ordinary line movement, but contains formatted call text and an ordinal. It is evidence of a ratchet
pattern, not an already-approved exact identity for the new semantic population.

### Type-aware context authority guard

- `test/contract/context_ownership_contract_test.go:27` — `loaded, err := packages.Load(&packages.Config{`
- `test/contract/context_ownership_contract_test.go:36` — `packages.NeedTypesInfo,`
- `test/contract/context_ownership_contract_test.go:41` — `if errs := packageErrors(loaded); len(errs) > 0 {`
- `test/contract/context_ownership_contract_test.go:201` — `typ = types.Unalias(typ)`

It loads production syntax/types, rejects package-load errors, and recognizes aliased/wrapped context storage. Its
configuration does not enable test-package loading. It does not trace call arguments, cleanup registration, or context
flow.

### Other adjacent audit patterns

- `internal/entityidaudit/source_set.go:29` — `args := []string{"-C", root, "ls-files", "-z", "--cached"}`
- `internal/modelresolveaudit/audit.go:152` — `return nil, fmt.Errorf("%s:%d: stale modelresolveaudit:allow annotation covers no unresolved lookup", path, a.line)`
- `test/natsclient/request_guard_test.go:216` — `func TestRequestGuardKnownGapMethodValueCapture(t *testing.T) {`

Entity-ID auditing has an explicit Git source set and optional untracked inclusion. Model-resolution auditing has
heuristic same-function provenance and stale-annotation rejection. The Request guard deliberately documents and tests
its method-value blind spot. That existing limitation is evidence to state coverage honestly, not authorization to
silently omit #1064's required forms.

No new durable, communication, or runtime-coordination primitive is proposed by this inventory; the corresponding
runtime collision table is not triggered.

## 5. Adjacent claims and consumer at birth

The only tracked active proposal found outside the archive is `test-cleanup-root-guard`.

#1062's bounded cleanup correction and #1410's suite adoption are present examples, not population-wide closure.
#1293, #1411, and #1412 retain their separate scope under the owner ruling. Production Stop behavior is unchanged by
the assigned audit/guard task.

The immediate consumer is repository test verification and the contributor/reviewer interpreting its findings.
No new exported runtime symbol, subject, bucket, or config field is proposed. Existing shared test APIs are relevant
because they hold cleanup sites; no downstream API migration is currently identified.

## 6. Adopter seam inventory

No new external runtime surface is selected. The relevant adopter is a contributor adding or maintaining a component
test, including reusable test helpers consumed by component authors.

| Question | Current state |
|---|---|
| What must they know? | Cleanup ownership, finite terminal authority, cancellation timing, Stop error handling, and component-before-substrate ordering |
| What happens if they do nothing? | Direct Background cleanup compiles; the inspected current guards do not reject it |
| Where do they find out? | Policy/review, or a later test timeout; type checking verifies call shape rather than deadline provenance |
| What should they have to know? | A finding should identify the concrete call and classification limit; the existing gap is that cleanup debt can pass without that observation |

The proposed guard's diagnostic and baseline obligations are not yet designed. This inventory does not transfer
prediction of runtime completion to test authors or treat a selected timeout as proof of completion.

## 7. Open evidence questions

1. Exact admitted receiver population: components, services, additional runtime owners, contextless owners, and returned
   terminal callbacks need explicit classification boundaries.
2. Cleanup ownership beyond literal defer/Cleanup includes explicit finalization, reusable helpers, callbacks, and
   aliases. Their unsupported cases must remain visible as uncertain.
3. Source selection must account for ordinary test support, integration/live variants, nested modules, test doubles,
   and duplicate package views.
4. Context provenance must distinguish finite descendants from cancellation-only roots, reassignment, helper results,
   parameter forwarding, and removal of a parent deadline. This inventory establishes examples, not a dataflow proof.
5. Stable exact identities must survive line movement while detecting removed, replaced, duplicated, and newly introduced
   debt. Existing identities inform this question without settling it.
6. No current semantic totals, reviewed baseline entries, runtime timing evidence, or guarantee of wall-clock return
   has been established.

## Searches

All repository queries used the named worktree and baseline. Reads used `cat`, `sed`, and `nl`; no analyzer was written.

- `git status --short`; `git rev-parse HEAD`; `git diff --stat` — baseline above, clean tree.
- `gh issue view 1064 --json number,title,state,body,comments`
- `gh api repos/C360Studio/semstreams/issues/comments/5873261389`
  — initial network failure; escalated read succeeded.
- `git ls-files 'test/*' 'internal/*' '*baseline*' 'openspec/specs/*/spec.md' 'openspec/changes/test-cleanup-root-guard/*' ':!:test/e2e/**' ':!:internal/test**'`
- `git grep -n -E '1064|unbounded.*(Stop|cleanup)|cleanup.*root|go/types|go/packages|stale.*baseline|baseline.*stale' -- test internal openspec/specs .agents/contracts docs/proposals/next-tag-test-gate-blockers-inventory.md docs/proposals/next-tag-test-gate-blockers-design.md`
- gopls implementation/reference queries and build selections are recorded in section 2. Initial cache failure was not
  treated as absence; successful reruns supplied the reported counts.
- `gopls workspace_symbol -matcher=fuzzy Service`; `stopContext`; `KeyedPool.Stop`; `CronScheduler.Stop`; `Set.Stop`;
  `bounded`; `stopCtx` — declaration locators only. A broad case-sensitive `Stop` query with an overly restrictive
  output filter returned zero; that result was not used as absence evidence.
- `git grep -n -E 'Stop\(|WithTimeout|WithDeadline|WithoutCancel' -- service/component_manager_lifecycle_test.go pkg/dispatch/keyed_pool_test.go processor/rule/lifecycle_integration_test.go component/lifecycle_test_suite.go`
- `git grep -n -E 'go/packages|go/types|types\.Selections|\.Selections|MethodExpr|MethodVal' -- test internal scripts`
- `git grep -n -E 'type Service interface|Stop\(ctx context.Context\)' -- service/base.go`
- `git grep -n -E '^//go:build' -- '*_test.go' | cut -d: -f3- | sort | uniq -c`
- `git ls-files '*go.mod' 'go.mod'`
- `git ls-files 'openspec/changes/*/proposal.md' ':!:openspec/changes/archive/**'`
- `git grep -n -E 'defer.*\.Stop\(ctx\)|defer.*\.Stop\(stopCtx\)' -- '*_test.go'` — zero for those exact same-line forms.
- `git grep -n -E 'WithCancelCause|WithTimeoutCause|WithDeadlineCause' -- '*_test.go'` — zero for those spellings.
- Additional scoped locator searches covered Stop/Cleanup/context/baseline language in taskfiles, CI, contracts,
  testing policy, the nested harness, live-provider tests, and guard scripts. A scripts search displayed only its first
  65 matches and was not used to claim exhaustive absence.

The Python textual measurement used `git ls-files '*_test.go'`, read each file line-by-line, and applied:

```text
\.Stop\(context\.Background\(\)\)
(?:defer|\.Cleanup\().*\.Stop\(context\.Background\(\)\)
\.Stop\(context\.TODO\(\)\)
\.Stop\(\)
\.Stop\(nil\)
```

Additional spelling probes over tracked Go sources used:

```text
^\s*(?:[A-Za-z_][A-Za-z0-9_]*|\.)\s+"context"
\([^\n]*\)\.Stop\b
\.Stop\b(?!\()
context\.(?:WithTimeoutCause|WithDeadlineCause|WithoutCancel)\(
```

These produced zero test context-import aliases, one false-positive method-expression shape, 30 method-value/multiline
candidate lines including prose, and three finite-detachment test lines. They remain lexical evidence only.

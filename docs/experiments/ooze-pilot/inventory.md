# Ooze utility pilot: existing surface inventory

base: 84fc01e46c104d890bffe32b0046e72f006df454

Claim: #1318 / draft PR #1319.
Claim HEAD: 228e0a546f411e168bab7b0cb5fed1aa91db33db.
Observed: 2026-09-16, approximately 14:08 UTC.
Worktree: /Users/coby/Code/c360/semstreams-wt/codex/gh1318-ooze-pilot.
Branch: codex/gh1318-ooze-pilot.

This is an inventory checkpoint, not an adoption decision or experiment result.
The caller must record the materialized artifact's content hash before independent review.

## Problem and scope

The approved evaluation asks whether a pinned Ooze revision produces trustworthy, actionable
test-sensitivity evidence with less effort than existing manual mutation checks. The issue limits
ownership to a new pilot/evidence directory and disposable source snapshots. It excludes production
edits, root dependencies, CI changes, shared Docker use, sister-repository writes, and interference
with claimed work.

The local inventory covers the existing entity-ID tests, optional shutdown tests, evidence conventions,
test commands, and adjacent claims. Upstream Ooze semantics and tool qualification are a separate
inventory supplied by the caller.

The claim commit is empty: `git diff 84fc01e46c104d890bffe32b0046e72f006df454..HEAD --stat`
returned no output. `git status --short` returned no output. The PR reported no changed files.

## 1. Claimed gap: structured mutation evaluation

A repository-wide tracked-content search found no Ooze or gtramontina integration. It found the
testing policy's explicit statement that no mutation runner is selected, plus a historical
recommendation to mutation-test important assertions. This is evidence of no current integration
under those spellings, not evidence that manual mutation practice is missing.

- `docs/contributing/01-testing.md:62` — `change. This discipline does not select a mutation runner, require a library, or introduce a score threshold.`
- `go.mod:3` — `go 1.26.3`
- `go.mod:25` — `pgregory.net/rapid v1.3.0`

There was no existing `docs/experiments` directory at the baseline. This locational absence does
not imply that experiment evidence is absent: existing witnesses live beside their tests and the
policy assigns larger snapshots to the PR record.

## 2. Current spellings of the measured behavior

### Entity-ID authority and candidate fault sites

The fact under assessment is conformity to the existing canonical identity contract; the pilot
does not define a new identity grammar.

- `openspec/specs/entity-id-contract/spec.md:21` — `### Requirement: Every entity ID has one canonical six-segment ASCII form`
- `openspec/specs/entity-id-contract/spec.md:26` — `longer than 256 bytes. There MUST be no independent per-segment length maximum. The `instance` position MUST be the`
- `pkg/types/entity_id.go:13` — `MaxEntityIDBytes = 256`
- `pkg/types/entity_id.go:68` — `const canonicalEntityIDParts = 6`
- `pkg/types/entity_id.go:133` — `if err := ValidateEntityID(s); err != nil {`
- `pkg/types/entity_id.go:150` — `return validateEntityIDValue(value, ErrorCodeEntityIDInvalid, canonicalEntityIDParts, canonicalEntityIDParts, false)`
- `pkg/types/entity_id.go:180` — `if patternParts[index] != "*" && patternParts[index] != entityParts[index] {`
- `pkg/types/entity_id.go:190` — `return validateEntityIDValue(value, ErrorCodeEntityIDPrefixInvalid, 1, canonicalEntityIDParts, false)`
- `pkg/types/entity_id.go:197` — `if len(value) > MaxEntityIDBytes {`
- `pkg/types/entity_id.go:205` — `if len(parts) < minimumParts || len(parts) > maximumParts {`
- `pkg/types/entity_id.go:222` — `if !isEntityIDAlphanumeric(part[0]) {`
- `pkg/types/entity_id.go:244` — `return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'`
- `pkg/types/entity_id.go:248` — `return isEntityIDAlphanumeric(value) || value == '_' || value == '-'`

Literal, pattern, and prefix validation share `validateEntityIDValue`. A mutation there can affect
more than the literal-ID behavior named by a selected test.

### Existing checks and their different evidence

- `pkg/types/entity_id_contract_test.go:14` — `func TestValidateEntityIDContract(t *testing.T) {`
- `pkg/types/entity_id_contract_test.go:54` — `func TestValidateEntityIDByteBoundary(t *testing.T) {`
- `pkg/types/entity_id_contract_test.go:76` — `func TestEntityIDFailurePrecedence(t *testing.T) {`
- `pkg/types/entity_id_contract_test.go:171` — `func TestValidateEntityIDPrefix(t *testing.T) {`
- `pkg/types/entity_id_contract_test.go:227` — `func FuzzParseEntityIDRoundTrip(f *testing.F) {`
- `pkg/types/entity_id_prop_test.go:47` — `var entityIDSegment = rapid.StringMatching(`[a-zA-Z0-9][a-zA-Z0-9_-]{0,38}`)`
- `pkg/types/entity_id_prop_test.go:62` — `func TestPropEntityIDRoundTrip(t *testing.T) {`
- `pkg/types/entity_id_prop_test.go:89` — `func TestPropEntityIDByteBound(t *testing.T) {`
- `pkg/types/entity_id_prop_test.go:97` — `rapid.IntRange(MaxEntityIDBytes-1, MaxEntityIDBytes+1),`
- `pkg/types/entity_id_prop_test.go:101` — `if total <= MaxEntityIDBytes && err != nil {`
- `pkg/types/entity_id_prop_test.go:104` — `if total > MaxEntityIDBytes && err == nil {`
- `pkg/types/entity_id_prop_test.go:114` — `func TestPropEntityIDPatternMatch(t *testing.T) {`

The byte-bound property's generator and expectation both reference the production constant.
The explicit boundary table independently names 255, 256, and 257. These are different protections;
the property alone cannot establish sensitivity to every change to that constant.

The fuzz target checks deterministic classification, agreement between validation and parsing, and
round-trip consistency for accepted values. It does not independently specify malformed-input
rejection.

Existing commentary records two relevant manual experiments:

- `pkg/types/entity_id_prop_test.go:34` — `// first-byte rule to admit a leading '_' widens the accept path while`
- `pkg/types/entity_id_prop_test.go:35` — `// preserving the Key() round-trip, and it survives 5.2M fuzz executions over`
- `pkg/types/entity_id_prop_test.go:36` — `// 30s AND all three properties. Only the tables catch it. (The narrower`
- `pkg/types/entity_id_prop_test.go:147` — `// Measured: an EqualFold mutation of MatchEntityIDPattern survived the`
- `pkg/types/entity_id_prop_test.go:148` — `// independent draw at the default budget on six fresh seeds, and needed`

These are historical records, not results re-executed by this inventory.

### Entity-ID witness and corpus ownership

- `pkg/types/testdata/rapid/README.md:8` — `Run mutation checks with `-rapid.nofailfile` so they never write here in the first place.`
- `pkg/types/testdata/rapid/README.md:18` — `This seed was recorded while a **deliberate off-by-one mutation** was applied to production code, so it`
- `pkg/types/testdata/rapid/README.md:19` — `is a *mutation-kill witness* — evidence that the property catches that defect class — not a defect ever`
- `pkg/types/testdata/rapid/README.md:22` — ``TestPropEntityIDByteBound` is a plain `rapid.Check` property, not a `t.Repeat` state machine, so its`

The registered witness is
`pkg/types/testdata/rapid/TestPropEntityIDByteBound/TestPropEntityIDByteBound-20260831140043-41866.fail`.
Its README records detection of changing `>` to `>=` at the 256-byte boundary and clean replay on
the original implementation. That claim still requires fresh paired execution during qualification.

### Optional shutdown slice

- `openspec/specs/service-shutdown/spec.md:16` — `### Requirement: Coordinated shutdown treats an already-stopped service as clean success`
- `openspec/specs/service-shutdown/spec.md:136` — `### Requirement: Terminal StopAll success deregisters every service; failure retains them for retry`
- `service/service_manager.go:838` — `func (m *Manager) StopAll(ctx context.Context) error {`
- `service/service_manager.go:888` — `if stopErr != nil && !stderrors.Is(stopErr, ErrAlreadyStopped) {`
- `service/service_manager_prop_test.go:82` — `func TestStopAllRetryAfterFailedPassTreatsAlreadyStoppedAsClean(t *testing.T) {`
- `service/service_manager_prop_test.go:131` — `func TestPropStopAllShutdownContract(t *testing.T) {`
- `service/service_manager_prop_test.go:150` — `t.Repeat(map[string]func(*rapid.T){`
- `service/service_manager_prop_test.go:267` — `registered := manager.GetAllServices()`

The property generates registration, an armed service failure, and StopAll operations. It observes
reverse visit order, context-token propagation, error aggregation, and actual manager registry
membership.

The manager also aggregates its own BaseService, health-publisher, runtime-server, and startup-metrics
teardown errors. Issue #1219 owns the property gap for those sources. The present property constructs
a manager without those runtime dependencies. This inventory does not claim complete shutdown
failure reachability.

- `service/testdata/rapid/README.md:8` — `Run mutation checks with `-rapid.nofailfile` so they never write here in the first place.`
- `service/testdata/rapid/README.md:24` — `- The recorded stream **ends where the failure was**, so a passing run cannot consume it to the end.`
- `service/testdata/rapid/README.md:26` — `- The stream is **positional**. Adding or renaming any action changes `SampledFrom(actionKeys)``
- `service/testdata/rapid/README.md:30` — `So every curated seed here must also have a **named deterministic test** carrying its coverage. For the`

The shutdown seed is provenance for a synthetic sentinel-filter fault. Its named deterministic test
preserves the operation sequence because the original Rapid stream can truncate or decode differently
after action changes. A green run reporting an invalid fail file does not alone prove paired replay.

## 3. Adjacent claims and policy constraints

Live open-PR census:

| PR | Branch | Observed claim |
|---|---|---|
| #1319 | codex/gh1318-ooze-pilot | This isolated evaluation |
| #1312 | codex/gh1311-governance-proposal-settlement | Governance proposal settlement design |
| #1254 | claude/gh1205-auth-inventory | Principal/auth inventory |
| #1159 | codex/gh1146-agentic-loop-restart | Agentic-loop restart durability |
| #1156 | codex/gh759-semantic-settlement | Semantic delivery settlement |
| #1141 | codex/gh1138-http-page-read | Bounded GET-only page retrieval |

The census establishes current claim identity, not an exhaustive cross-branch diff or host-resource
availability measurement. No other worktree was inspected or modified.

Issue #1293 owns alignment of local/CI verification, citation checks, exploratory fuzzing, and coverage
artifacts. Issue #1219 owns additional shutdown property behavior. The pilot issue expressly excludes
implementing those issues and #1292/#1317.

The current testing policy already specifies:

1. disposable copies or exact backups;
1. fixed checks and runner configuration during each implementation comparison;
1. baseline, valid mutant, intended assertion failure, restored bytes, and restored pass;
1. snapshots including relevant uncommitted and untracked files;
1. paired replay of generated failures;
1. distinct detected, survived, invalid, and inconclusive observations;
1. equivalence and deferral as separate reviewer assessments;
1. durable PR evidence rather than private scratchpad-only evidence.

- `docs/contributing/01-testing.md:64` — `### Establish Sensitivity to a Selected Mutation`
- `docs/contributing/01-testing.md:79` — `Record the source revision and the experiment snapshot, including relevant uncommitted and untracked files; identify`
- `docs/contributing/01-testing.md:85` — `Retain small experiment snapshots inline in the PR record; attach larger snapshots, including patches, relevant files,`
- `docs/contributing/01-testing.md:93` — `Preserve a replayable witness for important generated discoveries. If generator or tool changes make replay unstable,`
- `docs/contributing/01-testing.md:100` — `- **Survived:** the selected checks did not detect the mutation. Investigate missing inputs, assertions, or scope.`

The current capability specs were read. The mutation-runner search found no tool-specific current
spec, ADR, or active OpenSpec change. Existing identity and shutdown behavior remains governed by its
current capability specs; this inventory introduces no behavior or contract amendment.

## 4. Consumer at birth and repository entry points

No new exported framework symbol, port, subject, bucket, or configuration field is proposed by the
approved evaluation. Its present consumers are the SemStreams evaluation author/reviewer and the
owner considering a later tool decision.

- `taskfiles/test.yml:7` — `- go test ./...`
- `taskfiles/test.yml:12` — `- go test -race ./...`
- `taskfiles/test.yml:17` — `- scripts/run-integration-tests.sh`
- `Taskfile.yml:8` — `dotenv: ['.env']`
- `Taskfile.yml:106` — `- scripts/inventory-verify.sh {{.CLI_ARGS}}`
- `Taskfile.yml:116` — `- scripts/spec-properties.sh {{.CLI_ARGS}}`

Root Task invocation loads `.env`; ordinary Go package testing is a separate existing entry point.
The root module declares Go 1.26.3 and Rapid 1.3.0. This inventory did not execute either tool and
does not establish the installed toolchain version or dependency availability.

A local search of `pkg/types` found no `TestMain`, build constraint, `net.Listen`, `NewTestClient`,
or `exec.Command` spelling. This supports the bounded package selection but is not a transitive
dependency or runtime-resource proof.

## 5. Existing problem shape

The shape is a controlled fault-injection experiment evaluating the sensitivity of an existing check,
with paired observations and preserved evidence.

Existing instances already include:

1. the entity-ID byte-bound witness and its documented `>` to `>=` experiment;
1. first-byte acceptance widening, including measured property/fuzz survival;
1. pattern case-folding experiments that changed the generator's near-miss coverage;
1. shutdown sentinel-filter removal and a promoted deterministic operation sequence;
1. the canonical four-stage controlled-comparison procedure.

No new reusable runtime primitive is being established. This inventory records the existing shape
without choosing an implementation or tool-adoption target state.

## Same-class collision trigger

Not triggered under the claimed scope: the pilot adds no durable primitive, communication primitive,
or runtime-coordination primitive. Evidence artifacts are experiment records, not operational state.
The identity and shutdown runtime owners are observed inputs, not new ownership candidates.

The integration-runner host lock and active claims remain relevant resource boundaries. A separate
worktree does not establish absence of CPU or memory contention.

## Adopter seam inventory

No changed or newly exposed framework surface is included in this evaluation. The four questions for
a developer outside this repository are therefore closed as follows:

| Question | Current bounded evaluation |
|---|---|
| What must they know? | No new framework requirement; the existing identity/shutdown contracts remain the observed behavior. |
| What happens if they do nothing? | Their existing runtime, configuration, dependencies, and tests receive no pilot change. |
| Where do they find out? | The evaluation's issue/PR and retained evidence communicate findings; no correctness obligation is delegated through those documents. |
| What should they have to know? | Nothing to continue using SemStreams under its existing contracts. |

Sharing findings with SemDev does not adopt a tool or change SemDev policy. A later developer-facing
runner contract, root dependency, CI gate, runtime surface, or adopter migration would reopen this
inventory and its seam questions.

## Search record

All commands ran from the claim worktree. No mutating Git command was used.

1. `rg --files docs/experiments pkg/types service openspec .agents | rg '(ooze|prop_test|rapid|testing-discipline|testing|mutation|experiment|entity-id|shutdown)'`
   Located properties, witnesses, and current/archived specs. Reported absent `docs/experiments`.
2. `git grep -n -i -E 'ooze|mutation (test|experiment)|mutation.*baseline|Prompts focus|property.*counterexample' -- . ':!go.sum' ':!go.mod'`
   Located canonical testing policy, developer guidance, and unrelated graph-mutation prose.
3. `git grep -n -i -E 'rapid|mutation|counterexample|witness|snapshot|source preservation|ooze' -- .agents/contracts .agents/skills pkg/types service go.mod Taskfile.yml docs/contributing docs/proposals openspec/changes ':!*integration*' ':!*_test.go'`
   Overbroad output was truncated; it is not used to support absence or completeness claims.
4. `gopls workspace_symbol -matcher=fuzzy 'ValidateEntityID'`
   Failed while attempting to open the goimports cache under Library/Caches: operation not permitted.
   No structural-completeness claim is made from this result.
5. `git grep -n -E 'go 1\.|rapid|^  test:|go test|spec:properties|inventory:verify|ooze' -- go.mod Taskfile.yml .gitignore .agents/protocol.md`
   Located the module version, Rapid dependency, and Task entry points.
6. `git grep -n -i -E 'ooze|gtramontina|mutation testing|mutation runner|mutation.check|rapid' -- go.mod go.sum .github scripts Taskfile.yml docs/adr openspec/changes ':!openspec/changes/archive'`
   Found Rapid and one existing shell-fixture mutation-control comment; no Ooze integration.
   Other “rapid” hits were ordinary prose.
7. `rg -n 'TestValidateEntityIDContract|TestValidateEntityIDPrefix|TestEntityIDFailurePrecedence|FuzzParseEntityIDRoundTrip|func .*StopAll|stopErr != nil|errors.Is\(stopErr' pkg/types service/service_manager.go`
   Fallback symbol location after gopls failure; located the tests and shutdown seam read above.
8. `git grep -n -i -E 'ooze|gtramontina|mutation.runner|mutation.testing' -- . ':!go.sum'`
   Two hits: testing policy line 62 and historical graph-core audit prose; no Ooze/gtramontina hit.
9. `rg -n 'func TestMain|^//go:build|net.Listen|NewTestClient|exec.Command' pkg/types`
   Zero hits. Limited to these spellings and this directory.
10. `git grep -n -E 'Every entity ID|MUST be no|Terminal StopAll|Coordinated shutdown' -- openspec/specs/entity-id-contract/spec.md openspec/specs/service-shutdown/spec.md`
    Located the current requirements pinned above.
11. `gh pr list --state open --limit 100 --json number,title,headRefName,isDraft,body,url`
    Broad body output was truncated; no absence claim relies on it.
12. `gh pr list --state open --limit 100 --json number,title,headRefName,isDraft,url`
    Returned the six PRs listed above.
13. `gh issue view 1318 --json number,title,body,comments,url`
    Read the complete approved evaluation scope; no comments were present.
14. `gh pr view 1319 --json body,files,headRefOid,baseRefName`
    Read the complete claim artifact; no changed files, base main, claim HEAD as recorded.
15. `gh issue view 1219 --json number,title,body,url`
    Read the complete separately owned shutdown reachability gap.
16. `gh issue view 1293 --json number,title,body,url`
    Read the complete separately owned verification-plumbing scope.

Direct reads included the architect contract, `openspec/project.md`, shared protocol, current
entity-ID and shutdown specs, testing policy, root test tasks, implementation/test ranges, and both
Rapid witness READMEs. An initial attempt to read `docs/contributing/08-testing-discipline.md` failed:
the canonical policy is `docs/contributing/01-testing.md`.

## Open evidence questions

1. The upstream inventory must establish exact Ooze revision, operators, runner classification,
  cancellation, and source-preservation semantics.
1. Installed Go version, dependency availability, and isolated-copy fidelity have not been measured here.
1. Historical mutation results and witness claims need fresh paired execution before becoming pilot results.
1. Active-claim identity is measured; full cross-branch overlap and current shared-host contention are not.
1. The shutdown generator cannot currently establish the manager-owned teardown clause owned by #1219.
1. Inventory pin validity and completeness have not yet received independent INVENTORY PASS.

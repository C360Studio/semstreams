# Predicate Corpus Audit and Release Gate

**Status:** Release procedure. The commands define the evidence required for the ADR-074 breaking cutover; this
document is not evidence that the gate has passed.

The coordinated release audit has two jobs:

1. prove that no owned production source, configuration, schema, reference deployment, or exact query still uses
   an identity from the breaking rename ledger; and
2. prove that every remaining predicate candidate satisfies the canonical three-part lower-kebab grammar.

SemStreams now has a committed bounded production-corpus gate: `task predicate:audit`. It uses Go AST parsing for
selected production declarations and field shapes, walks JSON and YAML structures, and recognizes selected
predicate declarations/substitutions in the other owned source formats listed below. It is intentionally not a
complete language-semantic or whole-repository proof: Go `*_test.go`, every `testdata` directory, ignored build
directories, and unrecognized expression shapes are outside its result. Broad candidate manifests, exact legacy
identity scans, native contract tests, and repository e2e gates supply the complementary evidence. Sister
repositories have not yet adopted and passed the coordinated gate.

## Audit Scope

The coordinated owned-repository set is:

- `semstreams`
- `semops`
- `semlink`
- `semsource`
- `semdev`
- `semdragon`
- `semboids`
- `semteams`
- `semconnect`
- `semstreams-ui`
- `semspec` while its remaining production paths are being archived

The scan covers Go, Python, JavaScript, TypeScript, Svelte, JSON, YAML, TOML, GraphQL, Protocol Buffer, and CUE
source/configuration files. It intentionally does not scan Git history or release documentation because the
breaking ledger must preserve old names. Add any repository-specific executable configuration extension before
accepting that repository's result.

## Prepare a Clean Evidence Directory

Run from the predicate enforcement worktree after all coordinated changes have been applied:

```bash
cd /Users/coby/Code/c360/semstreams/.worktrees/predicate-contract-retention-spec
audit_dir=/tmp/predicate-cutover-audit
rm -rf "$audit_dir"
mkdir -p "$audit_dir"
git rev-parse HEAD | tee "$audit_dir/semstreams.commit"
git status --short | tee "$audit_dir/semstreams.status"
```

The final release run requires an empty status file. A development run may be dirty, but its output cannot be used
as release evidence.

## Exact Legacy-Identity Scan

The function below reads the production identities before the ledger's test-fixture appendix and matches direct
quoted literals or lifecycle-tag assignments. The boundary check prevents `agent.run` from falsely matching the
canonical `agent.run.entity-id`. Raw `*_test.go` files are excluded because parser and rejection tests deliberately
contain invalid identities; production-like e2e scenario source remains included.

```bash
cd /Users/coby/Code/c360/semstreams/.worktrees/predicate-contract-retention-spec
ledger="$PWD/docs/operations/24-predicate-breaking-rename-ledger.md"

legacy_predicates() {
  awk -F '|' '
    /^## Test-Fixture Normalization Seen in the Diff$/ { exit }
    /^\| `[^`]+` \| `[^`]+` \|$/ {
      value = $2
      gsub(/^[[:space:]]*`|`[[:space:]]*$/, "", value)
      print value
    }
  ' "$ledger" | sort -u
}

scan_legacy_predicates() {
  repo="$1"
  result=0
  while IFS= read -r predicate; do
    pattern="(?:[\"'\\x60]\\Q${predicate}\\E[\"'\\x60]|predicate=\\Q${predicate}\\E(?=[,\\x60]))"
    if rg -n --pcre2 "$pattern" "$repo" \
      --glob '*.{go,py,js,mjs,cjs,ts,tsx,svelte,json,json5,yaml,yml,toml,graphql,gql,proto,cue}' \
      --glob '!**/*_test.go'; then
      result=1
    fi
  done < <(legacy_predicates)
  return "$result"
}

scan_legacy_predicates "$PWD"
```

Success is exit code zero and no output. A quoted old identity in a production comment also fails this scan because
it teaches the obsolete contract. Inspect any other match against the candidate manifest and native tests; do not
grow an ad hoc grep exclusion list.

## Local SemStreams Contract Gates

Run the focused grammar and declarative-surface tests first:

```bash
cd /Users/coby/Code/c360/semstreams/.worktrees/predicate-contract-retention-spec

task predicate:audit
go test ./vocabulary \
  -run '^(TestParsePredicate|TestParsePredicateMaximumLength|TestRegisteredPredicatesConform)$' \
  -count=1
go test ./test \
  -run '^TestReferenceConfigs_AllTripleRefsResolveToKnownPredicates$' \
  -count=1
go test ./processor/rule ./processor/gated-dag ./pkg/lifecycle ./pkg/ownership ./pkg/projection \
  -count=1
```

Then run the repository release gates and preserve the output:

```bash
cd /Users/coby/Code/c360/semstreams/.worktrees/predicate-contract-retention-spec
audit_dir=/tmp/predicate-cutover-audit

task lint 2>&1 | tee "$audit_dir/semstreams.lint.log"
go test -race ./... 2>&1 | tee "$audit_dir/semstreams.race.log"
task schema:generate 2>&1 | tee "$audit_dir/semstreams.schema-generate.log"
git diff --exit-code -- schemas specs 2>&1 | tee "$audit_dir/semstreams.schema-drift.log"
go test ./test/contract/... 2>&1 | tee "$audit_dir/semstreams.contract.log"
task e2e:structural 2>&1 | tee "$audit_dir/semstreams.e2e-structural.log"
```

Because this is a breaking ingest-to-query contract, unit and integration success does not replace the structural
e2e result.

## Owned Sister-Repository Scan

Run the same exact-token scan against every owned checkout. Missing repositories fail the command instead of being
silently skipped.

```bash
cd /Users/coby/Code/c360/semstreams/.worktrees/predicate-contract-retention-spec
audit_dir=/tmp/predicate-cutover-audit

repos=(
  semops
  semlink
  semsource
  semdev
  semdragon
  semboids
  semteams
  semconnect
  semstreams-ui
  semspec
)

result=0
for name in "${repos[@]}"; do
  repo="/Users/coby/Code/c360/$name"
  if [[ ! -d "$repo/.git" && ! -f "$repo/.git" ]]; then
    echo "missing owned repository: $repo" | tee -a "$audit_dir/sisters.missing.log"
    result=1
    continue
  fi
  git -C "$repo" rev-parse HEAD > "$audit_dir/$name.commit"
  git -C "$repo" status --short > "$audit_dir/$name.status"
  if ! scan_legacy_predicates "$repo" > "$audit_dir/$name.legacy.log" 2>&1; then
    cat "$audit_dir/$name.legacy.log"
    result=1
  fi
done
test "$result" -eq 0
```

For release evidence, each `*.status` file must be empty and each `*.legacy.log` file must be empty. Record the
commit IDs in the coordinated release issue so the result cannot be detached from the audited revisions.

## Candidate Manifest for Manual and Tool Review

Generate a broad candidate manifest from all owned repositories as a review aid alongside the committed auditor.
This is an inventory, not a validator, so output is expected.

```bash
audit_dir=/tmp/predicate-cutover-audit
repos=(
  semstreams semops semlink semsource semdev semdragon semboids semteams semconnect semstreams-ui semspec
)

for name in "${repos[@]}"; do
  repo="/Users/coby/Code/c360/$name"
  rg -n --pcre2 \
    '(?i)(predicate|predicates|phasepredicate|linkpredicate|triplepredicate|referencepredicates|"field")' \
    "$repo" \
    --glob '*.{go,py,js,mjs,cjs,ts,tsx,svelte,json,json5,yaml,yml,toml,graphql,gql,proto,cue}' \
    > "$audit_dir/$name.candidates.log"
done
```

The manifest includes test files so the reviewer must reconcile each result with one of these buckets:

- parsed directly by the shared SemStreams predicate parser;
- declared and validated by a repository-local AST/config test;
- not a predicate candidate, with a short reason; or
- a violation to fix.

Manual manifest review is required complementary cutover evidence. It covers test files and expression shapes that
the bounded auditor intentionally excludes or may not recognize; it does not replace `task predicate:audit` or
native repository contract tests.

## Committed Structured Auditor

`cmd/predicate-audit` is the committed offline auditor and `task predicate:audit` is its local invocation. Within
its bounded production corpus, it:

- uses Go AST parsing to recognize selected predicate-bearing fields, constants, registrations, assignments, and
  tags, and walks JSON/YAML configuration structures instead of treating every dotted string as a predicate;
- uses bounded structured-text patterns for selected predicate declarations and substitutions in Python,
  JavaScript, TypeScript, Svelte, TOML, GraphQL, Protocol Buffer, and CUE sources;
- reports recognized Go triple fields, predicate constants, lifecycle tags, ownership/projection declarations,
  rule/config fields, schema defaults, generated tool enums, and exact-query fields;
- emits repository, file, line, candidate, and typed rejection reason;
- uses the ADR-074 grammar exactly, including ASCII and byte limits;
- exits nonzero for any unclassified or invalid production candidate; and
- accepts one or more repository roots, so sister repositories can pin the same implementation without enabling a
  permissive runtime mode.

Intentional invalid files inside the scanned production corpus require an exact
`predicate-audit:allow-invalid <identity> <reason>` annotation. Go test files and `testdata` are not scanned by
this command. Their negative identities are owned by executable grammar/contract tests; the broad candidate
manifest and native tests must separately classify stale positive fixtures.

OpenSpec task 1.1 is complete for the declared local production corpus: the committed auditor generates the
structured candidate set, while native grammar/contract tests own intentional invalid and positive test fixtures
outside that set. Tasks 1.5 and 5.1 remain open until the broad sister-repository manifests are reconciled and every
owned repository has pinned or reproduced the required audit, resolved its findings, and recorded a clean commit
and native test result. A green SemStreams-only run is bounded local evidence, not a claim that the coordinated
cross-repository corpus is clean.

## Draft Baseline, Not Release Evidence

The conservative production/e2e literal scan was run against the local checkouts on 2026-07-14 while coordinated
work was still in progress. Commits and clean status were not captured, so these numbers are triage only.

| Repository | Files containing a previous production identity |
|---|---:|
| `semstreams` | 10 |
| `semops` | 0 |
| `semlink` | 0 |
| `semsource` | 0 |
| `semdev` | 12 |
| `semdragon` | 8 |
| `semboids` | 0 |
| `semteams` | 69 |
| `semconnect` | 6 |
| `semstreams-ui` | 0 |
| `semspec` | 4 |

This table predates the implementation pass and is not a statement about the current worktree. Its SemStreams hits
included behavior-bearing source, e2e queries, stale comments, and examples; subsequent edits may have resolved
them. A separate test-literal scan found 16 `*_test.go` files containing previous production identities. Some were
intentional parser rejection fixtures and others were positive integration fixtures. Regenerate the manifest and
classify those files through their native tests before using any count as release evidence.

## Operator Reset and Query-Parity Gate

After source audits are clean, rehearse the breaking deployment with representative beta state:

1. Seed or preserve a beta graph containing at least one renamed predicate.
2. Start the breaking binary and prove `graph_state_reset_required` prevents readiness and all affected queries.
3. Export required source data, stop writers, and reset graph/index buckets using
   [`17-predicate-cutover-reset-reingest.md`](17-predicate-cutover-reset-reingest.md).
4. Reingest canonical source data.
5. Prove exact queries for representative renamed predicates and namespace queries return expected results.
6. Restart again and prove replay produces the same results and readiness revision.

Capture the before/after fixture queries, expected entity IDs, actual entity IDs, target revision, indexed revision,
and readiness state in the release issue.

## Zero-Violation Sign-Off

The coordinated release is blocked unless all of the following are true:

- the SemStreams and sister production/e2e exact-token scans have zero hits;
- the bounded structured production audit has zero invalid or unclassified recognized candidates;
- the broad candidate manifests, including tests and `testdata`, are reconciled through native contract tests or
  explicit review;
- all audited repository statuses are clean and commit IDs are recorded;
- local parser, registry, reference-config, declarative, race, schema, contract, and structural e2e gates pass;
- every affected sister repository passes its native contract and e2e gates against the same breaking version;
- the reset/reingest and restart/query-parity rehearsal passes; and
- review confirms no runtime alias, dual read/write, permissive mode, or in-process rewrite was introduced.

One missing repository, unclassified candidate, dirty evidence checkout, or untested exact-query consumer is a
failed gate, not a warning.

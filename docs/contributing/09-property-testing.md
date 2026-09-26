# Property-Based Testing with Rapid

The [testing policy](01-testing.md#when-to-use-property-based-testing) owns when PBT is expected and what evidence
belongs in a PR. This guide shows how to implement that decision with the existing `pgregory.net/rapid v1.3.0`
dependency. Rapid properties are ordinary Go tests and already run in the normal unit and additive CI suites.
No separate runner, build tag, or new library is needed.

## Start from an obligation

Write the invariant and its exact `// spec: <capability> / <requirement heading>` citation before the generator.
State what an adopter would observe if the invariant were violated, including forbidden writes or emissions.
Derive the expected result from the cited contract, a small independent model, or an external reference; copying
production branching or asking the production validator what should be accepted carries the same assumptions.

- [Entity-ID properties](../../pkg/types/entity_id_prop_test.go) exercise canonical acceptance and positional
  parsing. Valid-only generation does not prove classified rejection.
- [Graph-query properties](../../processor/agentic-tools/executors/graph_query_prop_test.go) exercise type matching
  and cursor partitions against an expected identity set. Mock-backed observations do not establish broker behavior.
- [Shutdown model](../../service/service_manager_prop_test.go) exercises order, errors, and retry state. It does
  not generate manager-owned teardown failures; see #1219 below.

For example, `TestPropEntityIDRoundTrip` keeps the generated segments as its expected positional values, calls
`ParseEntityID`, and checks each result field as well as the round trip. The classified rejection tables in the
same package remain necessary: a round trip can be self-consistent while accepting something the contract forbids.

## Construct useful generators

Use `rapid.Check` with generators such as `StringMatching`, `IntRange`, `OneOf`, and `SampledFrom`; call `Draw` with
stable, descriptive labels. Keep a generator close to the property until actual reuse justifies sharing it.
Construct meaningful inputs directly instead of generating arbitrary values and filtering almost all of them out.

- Partition accepted and rejected classes deliberately. For a rejection test, introduce the named violation while
  keeping unrelated grammar valid so the assertion observes the intended refusal and its classification.
- Include empty/minimum/maximum cases and near misses. A generator's numeric endpoints are not necessarily the
  contract's interesting boundaries. `TestPropEntityIDByteBound` combines a broad range with a boundary range.
- Keep independent boundary examples where the limit itself is contractual. The byte-bound property shares a
  constant with production; the explicit 255/256/257 examples protect the contract's numeric value separately.
- `OneOf` makes a case reachable; it does not guarantee that every case appears in a finite run. Use explicit
  examples or a deterministic loop over required cases when execution of each is part of the evidence claim.
- Inspect early returns, `Filter`, and `Skip`: record which classes still reach the relevant assertions. A large
  successful-case count cannot establish coverage of a branch that the generator never constructs.

Native Go fuzzing remains useful for parser/decoder byte surfaces and coverage-guided exploration. Rapid is useful
for structured inputs and generated operation sequences. Choose assertions and execution scope by the obligation;
corpus replay during ordinary `go test` is not an exploratory `-fuzz` run. Neither technique makes examples redundant.

## Model stateful histories

`TestPropStopAllShutdownContract` demonstrates `t.Repeat` with `register`, `armStopFailure`, and `stopAll` actions.
It holds expected registration order separately, uses a service spy to inject failures and record observations,
and checks the contract after actions; its empty-name action supplies the invariant check for `Repeat`.

For a new model, define a small state representation, legal actions and preconditions, then observe the real subject
through the production seam at the chosen tier. Compute the expected transition from the contract and check both
results and forbidden effects. Reset subject/model state for each `rapid.Check` case and clean up owned resources.
Bound collection sizes and expensive work in the action definitions. Do not introduce a production state-machine
runtime to make the test possible.

A test-owned reference model is useful even when production already has state. For example, a map from generated
entity identities to their latest contract-defined triples can supply expected query membership after replacement
or deletion. Update that map from the generated operations; compare it with observations of the production subject.
Do not call the production reconciliation algorithm to populate the expected map. Model size alone does not decide
independence: the question is whether the expectation can disagree with a plausible implementation error.

Check assertion activation separately from generator reachability. A loop over an always-empty applied-result set
executes no membership assertion; a gate invariant behind a gate that no action creates is equally unexercised.
A named prefix can establish the required nonempty set or gate before generating subsequent actions. Alternatively,
retain a deterministic witness for that obligation and describe the property's narrower scope. Do not require a
random run to hit an arbitrary quota of rare states.

A sampled history does not guarantee a particular failure/retry sequence. Retain deterministic sequences for those
obligations. Sequential model testing does not establish concurrent interleavings, abrupt process replacement, or
broker recovery; those need the existing tests at the appropriate tier. Use explicit synchronization for concurrent
checks and the canonical host-locked runner when real NATS semantics are required.

The current shutdown model can arm service failures, but constructs no runtime that can produce a manager-owned
teardown failure. [#1219](https://github.com/C360Studio/semstreams/issues/1219) owns that extension.
[#1292](https://github.com/C360Studio/semstreams/issues/1292) owns the planned graph-index reference model. These are
examples and separately owned work, not capabilities supplied by this guide.

## Run and bound the checks

Run from the repository root. Select the package that imports Rapid before passing its flags; a package without
that dependency does not register them. `-count=1` bypasses Go's result cache. `-v` shows the selected test names and
Rapid's actual check summary; inspect it rather than treating a zero exit or a requested budget as execution proof.

```bash
go test ./pkg/types -run '^TestPropEntityID' -count=1 -race -timeout=60s -v \
  -rapid.checks=100 -rapid.shrinktime=5s -rapid.seed=1320

go test ./service -run '^TestPropStopAllShutdownContract$' -count=1 -race -timeout=60s -v \
  -rapid.checks=100 -rapid.steps=30 -rapid.shrinktime=5s -rapid.seed=1320
```

These are bounded starting commands, not repository-wide budgets. Measure the chosen package under the intended
race/toolchain environment and record actual duration, checks, and history classes before increasing the budget.
Keep deterministic witnesses alongside exploration. Use `-rapid.seed=0` for fresh exploration. A fixed seed controls
Rapid's PRNG for the same generator/version; it does not control scheduling, clocks, or external state. Preserve the
actual failing input/history and relevant environment, and do not mistake repeated sampling for broader exploration.

Rapid v1.3.0 defaults to 100 checks, an average of 30 `Repeat` actions, and up to 30 seconds of shrinking; matching
`RAPID_*` environment variables can change defaults. `-rapid.steps` is an average, not a hard action cap. Bound costly
actions in the test itself. `-rapid.shrinktime` limits minimization; it is not the whole-run deadline. Go's test
timeout bounds the test binary, not dependency download/build or an arbitrary descendant process. Suite execution
and its budgets remain owned by the existing tasks and CI.

## Replay and retain a failure

Preserve the failing assertion, generated values/action sequence, source snapshot, Go/Rapid versions, command,
flags, and relevant environment. Rapid reports the replay seed and, unless disabled, writes a minimized `.fail`
file. Repeat the selected test with that reported seed or `-rapid.failfile`; the example seed above is not a failure
seed. Pin the property/generator source as well as the tool version: changing draws or action names can change what
the same seed or byte stream means.

This existing curated entity-ID witness can be checked explicitly; its path is relative to the test package:

```bash
go test ./pkg/types -run '^TestPropEntityIDByteBound$' -count=1 -race -timeout=60s -v \
  -rapid.checks=100 -rapid.shrinktime=5s -rapid.seed=1320 \
  -rapid.failfile=testdata/rapid/TestPropEntityIDByteBound/TestPropEntityIDByteBound-20260831140043-41866.fail
```

That command passes on the correct implementation. Rapid attempts the specified fail file before normal generated
checks. A missing, version-mismatched, or invalid file can log a diagnostic and then fall through to fresh checks;
a green exit and check count do not establish witness replay. Inspect the verbose diagnostics and preserve a named
deterministic witness when the exact case is required. The file above is a synthetic mutation witness, not proof of
a defect that existed on main. Even a valid green replay is not mutation evidence; use the controlled comparison.

Do not bulk-commit new `.fail` files. Triage and minimize the case, then follow the package's corpus register:
[entity-ID register](../../pkg/types/testdata/rapid/README.md) and
[shutdown register](../../service/testdata/rapid/README.md). For an important new discovery, retain a named
deterministic regression where generator evolution would make replay fragile, and record provenance when deliberately
curating a corpus entry. Preserve diagnostics before disposing of exploratory or duplicate files.

The shutdown witness has a deterministic companion, `TestStopAllRetryAfterFailedPassTreatsAlreadyStoppedAsClean`.
Its recorded `Repeat` stream can become invalid on green code or decode differently after action changes. The
named sequence carries the coverage; the `.fail` file records how it was found. Use `-rapid.nofailfile` for deliberate
mutation experiments to prevent synthetic debris, while preserving the witness through the experiment record.

## Review and hand off

Record the PBT decision, cited invariant, independent expectation, generated classes/history scope, exact command,
actual execution summary, and retained witness or remaining gap. Run `task spec:properties` to check citation
resolution; independently review the property's meaning. The task exists but is not currently wired into the common
CI/pre-push path. [#1293](https://github.com/C360Studio/semstreams/issues/1293) owns that verification work.

Use the policy's [visible evidence record](01-testing.md#visible-pbt-evidence-and-assertion-reachability), subject
to its prospective rollout. These two worked records illustrate the distinction; they claim no new execution:

- **Generated-property choice:** the milestone aggregate's contract defines outcome precedence independently of
  the implementation's ordinal ordering. Generate lists of the four outcome classes and permutations; assert the
  highest contract-priority member and permutation invariance. The equality assertion executes on every list,
  including the empty list; retain deterministic examples for any composition claimed as definitely exercised.
  Record the selected test's revision, command, seed and actual Rapid summary when run. Until then, execution is
  **unrun**, even if citation validation passes. This property observes aggregation of already-classified outcomes;
  it does not prove handler classification, broker settlement or delivery.
- **Named-example choice:** for a narrowly scoped first-fatal latch change, examples that exercise clean-only,
  first-fatal, and a second distinct fatal can pin refusal and first-cause retention. The rationale must explain why
  these cases cover the changed obligation and why arbitrary repeated results add no distinct state transition.
  Identify any concurrent observer/Stop race separately and drive it with explicit synchronization. Record the exact
  named examples and their run evidence; an untested race remains a limit. The existing delivery-lane Rapid property
  also explores result histories, so a change affecting its broader contract should assess and retain that evidence.

Keep the four evidence labels separate in the summary. For example, a passing `task spec:properties` belongs under
citation validation while the Rapid test's actual check summary belongs under generated property execution.

Apply the separate [mutation criteria](01-testing.md#when-targeted-mutation-evidence-is-required) to check sensitivity
when triggered. Passing generated checks provide scoped evidence, not a guarantee over every possible input/history.
The policy's prospective rollout preserves existing claims and reviewed slices; this guide changes no test or CI gate.

API and flag reference: [Rapid v1.3.0](https://pkg.go.dev/pgregory.net/rapid@v1.3.0) and its
[pinned flag definitions](https://github.com/flyingmutant/rapid/blob/v1.3.0/engine.go).

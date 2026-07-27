# Tasks: Rule Projection Contract Derivation

## 1. Lock derivation semantics with failing tests

- [x] 1.1 Add table-driven tests scanning enabled and disabled definitions across `on_enter`, `on_exit`,
  `while_true`, `on_recovery`, and cron `actions`.
- [x] 1.2 Prove deterministic union and ordering by contract, group, mode, predicate, and diagnostic action location.
- [x] 1.3 Prove only reviewed `replace_owned` actions derive groups; raw Add/Remove/Update remain excluded.
- [x] 1.4 Add target-scope tests for omitted subject plus `entity.pattern`, exact `$entity.id`, literal entity ID,
  message-path omission, cron omission, and every dynamic template class.
- [x] 1.5 Add conflicting-pattern tests proving no inferred least-common wildcard and actionable errors identify
  both rule/action locations.
- [x] 1.6 Reject malformed and wildcard static action subjects as authoring errors rather than dynamic obligations.

## 2. Implement immutable effective-contract derivation

- [x] 2.1 Derive minimal contracts from the same copied initial-rule snapshot consumed at start.
- [x] 2.2 Keep authored `Config.ProjectionContracts` distinct from immutable effective runtime contracts.
- [x] 2.3 Refactor the binder/preflight ordering so composition reads effective contracts only after successful
  preflight and never binds a pre-preflight raw slice.
- [x] 2.4 Preserve copies at every processor/service boundary; do not expose mutable group or predicate slices.
- [x] 2.5 Keep zero-action/no-declaration packs contract-free with no injected mutation client.

## 3. Validate explicit supersets

- [x] 3.1 Add canonical six-position pattern-containment tests for literal/literal, wildcard/literal,
  wildcard/wildcard, and invalid narrowing cases.
- [x] 3.2 Validate every derived contract/group/predicate is covered with exact group mode.
- [x] 3.3 Permit equality and explicit extra contracts/groups/predicates/wider patterns only after existing
  projection and aggregate overlap validation.
- [x] 3.4 Reject missing contracts, missing predicates, mode changes, narrower patterns, duplicates, and ambiguous
  selectors before binding.
- [x] 3.5 Require an explicit covering envelope for unresolved dynamic targets and prove runtime client validation
  rejects resolved IDs outside that envelope before transport.
- [x] 3.6 Preserve override field presence so omitted contracts derive while an explicit empty array fails to cover
  any derived obligation.

## 4. Preserve explicit-only metadata and #700 posture

- [x] 4.1 Keep `BirthPredicates`, `ForeignEdges`, `IndexingProfile`, and optional `MessageType` explicit-only.
- [x] 4.2 Test derived owning, explicit birth-only, append-only, foreign-edge-only, and mixed effective sets against
  Registry, nil-heartbeater, token, presence, and enrollment expectations.
- [x] 4.3 Prove declared superset additions can change posture only through existing projection derivation; add no
  special-case heartbeat, token, or presence logic.
- [x] 4.4 Prove invalid derivation/override failure has no Registry write, presence write, enrollment, injection, or
  mutation transport side effect.

## 5. Preserve hot reload, schema, and operator behavior

- [x] 5.1 Keep action `projection_contract`, `projection_group`, and literal predicate selectors required.
- [x] 5.2 Reject hot-reload authority expansion after minimal derivation; accept actions inside a boot-declared
  explicit superset without rebinding.
- [x] 5.3 Prove removing or disabling an action does not shrink or rebind the running effective contract.
- [x] 5.4 Update generated schema/config descriptions for omission derivation, superset overrides, and explicit-only
  fields without changing JSON field shapes.
- [x] 5.5 Add decode/preflight/encode tests proving omitted contracts remain omitted and explicit contracts round
  trip byte-shape compatibly.
- [x] 5.6 Audit both production binaries and service composition so they retain one shared pre-`StartAll` binder.

## 6. Integration and fail-closed evidence

- [x] 6.1 Add service tests proving every pack derives and validates before the first rule-pack bind.
- [x] 6.2 Add real Registry/MutationClient integration tests for a derived owning pack and explicit non-owning pack.
- [x] 6.3 Add pack-pack, pack-vs-built-in, and stale external overlap tests using effective contracts.
- [x] 6.4 Prove dynamic target outside an explicit envelope returns the existing invalid/not-committed client result
  with zero mutation transport.
- [x] 6.5 Audit production code for zero raw replacement fallback, manual owner token, lazy bind, or hot-reload bind.
- [x] 6.6 Make configured, enabled `rule-processor` factory, creation, and lifecycle-initialization failures
  boot-fatal without changing ordinary component log-and-continue behavior.
- [x] 6.7 Aggregate multiple rule-pack admission failures deterministically by configured instance name while
  preserving each wrapped factory or rule/action cause.
- [x] 6.8 Add production `Manager.CreateService("component-manager", ...)` tests proving a factory-invalid pack and
  an initialization/derivation-invalid pack each abort before any valid sibling bind, injection, start, or
  Registry/presence/heartbeat/mutation side effect.
- [x] 6.9 Prove disabled invalid rule packs remain excluded, ordinary component failures remain isolated, and the
  valid multi-pack production path still discovers every binder before binding.

## 7. Verification and release gates

- [x] 7.1 Run focused rule/service unit tests with race detection.
- [x] 7.2 Run complete tagged rule and service integration suites on a clean host.
- [x] 7.3 Run repository-wide test/race, vet, lint, build, schema generation, generated-drift, contract, and
  predicate-audit gates.
- [x] 7.4 Run the applicable structural/semantic end-to-end tier and explicitly tear down the stack.
- [x] 7.5 Run strict validation for this change and the complete OpenSpec set, Markdown lint, line-length audit, and
  `git diff --check`.
- [x] 7.6 Obtain independent architecture and Go review covering containment, snapshot immutability, fail-closed
  ordering, #700 posture, concurrency, schema compatibility, and deletion/scope audits.
- [ ] 7.7 Obtain mandatory Fable review of the public rule-authoring contract and resolve every finding before
  implementation acceptance.
- [ ] 7.8 Update issue #706 and the SemDragon #313 migration prerequisite with final evidence and compatibility
  guidance.

## Evidence

- Architecture decision: explicit declarations are validated supersets. Equality is accepted, but automatic
  derivation remains minimal and never widens patterns or predicates.
- Architecture decision: dynamic targets require an explicit envelope and remain runtime-fenced by the existing
  mutation client.
- Architecture decision: configured, enabled rule-processor admission failures are deterministic and boot-fatal;
  generic component failures retain best-effort isolation.
- Architecture decision: the public authoring behavior warrants mandatory Fable review despite no
  `pkg/projection` API change.
- RED derivation command:

  ```bash
  red_tests='Test(DeriveEffectiveProjectionContracts|'
  red_tests+='ConfigProjectionContractsPresenceRoundTrip|'
  red_tests+='ProcessorProjectionBindingsExposeEffectiveImmutableSnapshotOnlyAfterPreflight|'
  red_tests+='PatternContainsPattern|DerivationDiagnosticsAreStable)'
  go test ./processor/rule -run "$red_tests" -count=1
  ```

  failed to build because `deriveEffectiveProjectionContracts` and `projectionPatternContains` did not exist.
- RED config command:

  ```bash
  config_tests='TestConfigUnmarshal(PreservesDefaultsWhileOmissionSelectsDerivation|'
  config_tests+='ProjectionContractsPresenceAndAtomicErrors)'
  GOCACHE=/private/tmp/semstreams-706-go-cache \
    go test ./processor/rule -run "$config_tests" -count=1
  ```

  proved omission erased defaults, a type error mutated the receiver, and JSON `null` was accepted.
- RED admission command:

  ```bash
  admission_tests='TestComponentManagerInitialize('
  admission_tests+='AggregatesEnabledRulePackFailuresDeterministically|OrdinaryFailureRemainsBestEffort)'
  GOCACHE=/private/tmp/semstreams-706-go-cache \
    go test ./service -run "$admission_tests" -count=1
  ```

  returned nil for the invalid enabled pack; focused production integration reproduced the same failure for
  derived-invalid, missing-`pack_id`, and invalid-`pack_id` configurations.
- GREEN focused rule/service tests pass with race detection. Full tagged integration race suites pass against
  Docker/NATS: `processor/rule` in 48.069 seconds and `service` in 79.275 seconds; independent reruns also pass.
- Repository gates pass: `go test -race ./...`, `go build ./...`, `go test ./test/contract/...`, `go vet ./...`,
  pinned revive, whole-repository gofmt read-check, the fixed-port guard, and the raw NATS request guard.
- Repeated `task schema:generate` runs have zero drift. Generated MD5 values are
  `d9000912ba8c46795de0941069351a78` for the rule schema and
  `213d7b5bee8ba6fe3fd5b24e0b01282d` for OpenAPI.
- `task predicate:audit` and focused rule/service fixture audits pass. Repository-wide
  `task predicate:test-audit` reproduces only the two unchanged pre-existing findings at
  `processor/graph-query/graphrag_test.go:574` and `processor/graph-query/graphrag_test.go:590`.
- `task e2e:structural` passes all 37 steps with zero validation errors; the scenario completes in
  844.755416 milliseconds. Explicit deferred teardown removed both containers, the network, and the volume;
  an escalated filtered `docker ps` returned empty.
- Source audits find exactly one shared pre-`StartAll` `BindRulePackContracts` call in each production binary and
  no rule/service production raw replacement fallback, manual owner token, lazy bind, or hot-reload bind.
- Independent architecture review and SemStreams review returned `APPROVED` with no findings. Both confirmed
  deterministic rule-pack admission, immutable least-authority derivation, explicit-superset and hot-reload
  behavior, #700 posture, config compatibility, and public mutation-client/wire invariants.
- Target strict OpenSpec validation and all 33/33 strict validations pass. Markdown lint, the 120-character audit,
  tracked-diff hygiene, and untracked-file diff hygiene are clean.
- Mandatory Fable review and final issue/migration updates remain open under tasks 7.7 and 7.8.

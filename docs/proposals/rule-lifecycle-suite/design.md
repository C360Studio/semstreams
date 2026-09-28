# Rule lifecycle-suite adoption

Inventory: `inventory.md`, SHA-256 `597e19ad35b71df23ca21ba58e314919ea22875d6b50c10bb58102f864bca6a3`.
Independent inventory review: **INVENTORY PASS**, SemStreams reviewer (Astra/high), 2026-09-28.

The user-approved shape is adoption of the existing lifecycle suite. No runtime behavior or public contract changes.

## Options considered

- **Adopt the existing suite:** closes #1410 using the established factory contract; costs one integration fixture and its cleanup.
- **Do nothing:** leaves rule outside the portable conformance suite.
- **Introduce a new harness or watchdog:** changes framework testing behavior and requires separate scope. It is unnecessary for this adoption.

Proceed with the first option, subject to independent review of the fixture.

## Implementation

Add `processor/rule/lifecycle_integration_test.go` with the integration build tag and one `TestIntegration_…` entry point.

1. Create one fresh `natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithKVBuckets(graph.BucketEntityStates))` for the entire coherent lifecycle test. Keep package `TestMain` unchanged. Let `NewTestClient` own client/container termination.
2. Build valid configuration through `rule.NewConfig`. Supply no rules or external action traffic. Explicitly configure one valid, empty `ENTITY_STATES` pattern so the production watcher acquisition and cleanup paths are entered; the default KV input declaration alone does not activate a watcher. The fixture writes no entity values.
3. Marshal configuration and establish dependencies before entering the suite. Supply nonempty `deps.Platform`, the live NATS client, and no metrics registry. Use `rule.CreateRuleProcessor`, the registered production factory.
4. Pass a concurrency-safe closure to `component.StandardLifecycleTests`. Each invocation constructs and returns the actual processor through `component.LifecycleComponent`. Do not wrap or replace lifecycle methods. Treat marshaled configuration and dependencies as immutable. Protect the collection of constructed processors with a mutex.
5. Factory failures must not call `Fatal`, `FailNow`, or `require` from suite worker goroutines. Validate fixture inputs beforehand; report unexpected constructor/type failures with `t.Errorf` and return nil. The suite handles nil returns in both its ordinary and worker paths, preserving registered cleanup.
6. Register processor cleanup after creating the TestClient, so it executes before NATS teardown. Track every successfully constructed processor, including instances never started or abandoned after an assertion failure. Cleanup calls their existing Stop synchronously using one shared finite cleanup context, reporting errors with `t.Errorf`. This is a cooperative aggregate budget, not an independent wall-clock watchdog. It creates no promise of rejoining a timed-out running generation.

Fixed readiness writes remain shared within this test's cohort. The accepted inventory review found no established interference with lifecycle assertions. Do not describe the fixture as providing per-instance readiness isolation.

## Evidence and limits

The independent oracle is the current `component-lifecycle` spec and existing suite assertions. Adoption covers its named supported lifecycle sequences against production construction and real NATS lifecycle paths. `Start` can log an acquisition failure and continue, so suite success alone does not prove every runtime facility was acquired. Preserve and inspect focused verbose output for setup degradation; owner-local tests remain responsible for resource-specific guarantees.

PBT decision: named examples are sufficient for this wiring change because the existing suite explicitly enumerates the supported calls and rejected contexts, including repeated Stop and concurrent fresh instances. No new lifecycle history or invariant is introduced.

Mutation decision: no experiment is required for the wiring alone. A bounded synthetic removal of nil-Stop rejection may supplement the evidence that the adopted suite reaches its intended assertion; it is not an orphan-fence or hang-containment proof. It changes no invariant enforcement and claims no newly demonstrated detection of the historical orphan-fence regression. Existing owner-local deterministic tests remain responsible for that fault class.

Run the existing focused integration runner with race detection, record actual duration, and complete required repository gates. Do not claim generic hang containment, forced deadline activation, resource-specific drain-order proof, or complete leak freedom.

## OpenSpec

No spec delta is needed: the current lifecycle requirements and suite remain unchanged. Preserve this inventory/design and execution evidence in the existing PR record; no new ADR, public API, or runtime proposal is warranted.

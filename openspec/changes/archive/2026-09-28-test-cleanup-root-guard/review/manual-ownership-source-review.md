# Manual ownership source review

Independent reviewer source review supports the following proposed dispositions. This does not approve final
identities, fingerprints, baseline records, resolution enforcement, or the whole PR. The source checkpoint remains
claim `f60d78906086f0c9090a99f24c389f32d40cc167`; exact records must reconcile after classifier corrections.

| Source site | Supported disposition | Source evidence |
|---|---|---|
| `component/lifecycle_test_suite.go:115` | Deliberate contract | Explicit assertion that Stop(nil) returns an error; portable-floor table invokes the selected helper through t.Run |
| `component/lifecycle_test_suite.go:388` | Ordinary-only; retain unknown class | Error wrapper forwards contexts from both an asserted operation and a separate ordinary finalizer |
| `service/registry_race_test.go:120` | Ordinary-only; retain unknown class | Sole discovered construction is registered for the reverse-order test; direct Manager.StopAll dispatch reaches its Stop |
| `service/suite_integration_test.go:125` | Ordinary-only; retain unknown class | Selected testify suite.Run enumerates the method and invokes it through t.Run and reflection as a test body |

## Exact evidence dependencies to materialize

The nil-contract record requires `testNilStopContext`, `testPortableLifecycleFloor`, `StandardLifecycleTests`,
`LifecycleFactory`, and `LifecycleComponent`.

The error-wrapper record requires `ErrorInjectingComponent`, its qualified `Stop`, `NewErrorInjectingComponent`,
the complete `TestErrorInjection` declaration, and its current `TestWebSocketOutput_ErrorInjection` entry.
It must not classify every forwarded invocation as deliberate contract behavior.

The shutdown-wrapper record requires both mock/wrapper types and qualified Stop methods,
`TestServiceManager_ReverseOrderShutdown`, `Registry.Register`, `Constructor`, `Manager.CreateService`,
`Manager.StopAll`, `Manager.stopAll`, and `Service`. These preserve actual construction and dispatch evidence.

The reflective-suite record requires `ServiceSuite`, `TestServiceSuite`, qualified
`ServiceSuite.TestService_LifecycleSuite`, the `BaseService.Stop` binding, and selected testify v1.11.1
`suite.Run`/`runTests` evidence. Preserve the integration build constraint.

New cleanup or unresolved invocation paths must still block independently. None of these exact dispositions is an
exported-wrapper, suite-wide, package-wide, or receiver-wide exemption. No baseline entries were approved by this
source-only review, and the reviewer ran no tests.

## Registry and milestone callback source review

Independent review also supports the exact thirteen registry callbacks and five milestone callbacks in
`remaining-triage.json`. This is source classification evidence; final identity/fingerprint and resolution-enforcement
review remain pending.

All thirteen registry callbacks restore the saved registry map while holding `registryMu`; they perform no lifecycle
dispatch. Nine register SnapshotRegistry() directly with Cleanup and four defer its returned restore function.
Each exact resolution must include its enclosing caller/binding, SnapshotRegistry, registryMu, predicateRegistry,
and PredicateMetadata evidence. This is non-lifecycle classification, not a mutex-completion guarantee.

MilestoneSubscriber.Start binds its named stop result to owner.stop at agentrun.go:964 and returns that capability
after acquisition at line 1080. Reviewed alternative paths can return a no-op or return nil/the same authority with
an error. A possible no-op does not justify excluding the terminal capability.

The invocation at agentrun_integration_test.go:257 supplies a sixty-second timeout and supports finite-context
classification. The invocations at :339 and :480, milestone_policy_integration_test.go:51, and
refusal_wiring_integration_test.go:74 supply deadline-free testing contexts or Background and support unbounded
terminal classification. These four need exact uncertainty resolutions and exact legacy-debt approvals.

Each milestone resolution requires its complete caller, MilestoneSubscriber, qualified MilestoneSubscriber.Start,
milestoneConsumerOwner, qualified milestoneConsumerOwner.stop, and waitMilestoneLane. Preserve relevant build
constraints and the accepted testing-context semantic check. External-module behavior is not needed to justify the
supplied-context classifications. Finite context still does not establish wall-clock return or complete joining.

## Remaining cancellation, finite-context, and contextless callback source review

Independent source review accepted the ten cancellation variants and two loop-local finite contexts described in
`cancel-context-triage.md`. Constructor identity and exact return/capture slots justify cancellation classification;
CancelFunc type or callback spelling alone does not. The two loop bodies establish finite supplied contexts, not
wall-clock completion. Final records must preserve the complete caller/helper declarations and typed bindings.

The other nine callback records comprise two native NATS Close calls, one fusion readiness Close, four activity
view callbacks, one blocked-watcher release, and one startup-gate release helper. Reviewed receiver construction and
selected module bindings support their exclusion from the context-taking lifecycle Stop census. The readiness and
activity callbacks contain contextless joins; exclusion does not establish bounded completion. The release helpers
close gates without lifecycle dispatch. The startup helper's four caller bindings must remain freshness dependencies;
new cleanup callers must be detected independently rather than inheriting a blanket callback exemption.

Exact proposed records, semantic dependency hashes, and source snapshots are retained in
`final-manifest-candidate.json` and `final-source-snapshots.json`. Their generation is not approval. The classifier
checkpoint preserves all 325 earlier identities, the 324 fingerprint migrations, five additional automatic debt
sites, and all 43 unresolved records before manual reconciliation.

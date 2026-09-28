# Proposed review input for the remaining 61 cleanup candidates

Status: evidence and proposed dispositions only. This packet does not approve baseline entries, invent reviewer consent,
or certify the latest classifier. Its exact 61 candidate records belong to the frozen source scan named below.

## Checkpoint

- Repository base: `f60d78906086f0c9090a99f24c389f32d40cc167`.
- Frozen packet: `/private/tmp/gh1064-stabilization-packet.json`.
- Frozen packet SHA256: `2e448dd458d53bc1cd7979e6191ad72c8d80832f02a4da13f891eec8adaddae6`.
- Frozen scanner source `test/testinfra/cleanup_analyzer_test.go` SHA256: `7c5c3fa093ad2b4d0f8d29962f99ca957171ab7e1fdd12c7ebebd63a3fa45291`.
- Frozen scanner source `test/testinfra/cleanup_guard_test.go` SHA256: `2ccebabfe8f0b6d8143995939d30a157c66c9eb328c24db5c0ef23740c1b171e`.
- Exact-record companion: `/private/tmp/gh1064-remaining-triage.json`.
- Companion SHA256: `67ccad30cb99b5568a9ee1762616180512dd3cbf3156901f58a1730b63049284`.
- Candidate count: 61, partitioned exactly once into the fourteen evidence groups below.
- Analyzer corrections may change identities, fingerprints, applicability, and counts. Reconcile against the final
  scanner snapshot before proposing any machine-baseline records. The 325 previously reviewed debt candidates remain
  a separate immutable review checkpoint. None of these 61 is approved by that earlier review.

## Scope and evidence limits

This is a source-based triage packet after accepted inventory and design review, not another repository census.
The earlier inventory remains unchanged. Prior targeted gopls definition/reference queries supply structural evidence;
the packet lists their relevant results. Queries required cache-write escalation after read-only cache permission
failures. No absence conclusion is based on a failed query. No tests, heavy loads, repository edits, or new agents were
used to produce this packet. Current hashes below capture source evidence and are not classifier fingerprints.

A bounded supplied context does not prove wall-clock return. A non-lifecycle classification means outside this
guard’s admitted context-taking lifecycle Stop contract; it does not prove a contextless Close, cancel/join, mutex
operation, filesystem operation, or channel wait bounded. All source/classification proposals need independent review.

## Evidence groups

### A. TLS file cleanup (15)

Resolve the exact fourth returned callable and classify outside the admitted context-taking lifecycle Stop contract (non-lifecycle-stop).

Exact frozen candidates:

- `pkg/tlsutil/tlsutil_test.go:87` — `TestLoadServerTLSConfig`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:176` — `TestLoadClientTLSConfig`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:282` — `TestLoadServerTLSConfig_CertificateValidation`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:324` — `TestLoadClientTLSConfig_AdditionalCA`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:389` — `TestLoadServerTLSConfigWithMTLS_Disabled`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:412` — `TestLoadServerTLSConfigWithMTLS_RequireClientCert`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:437` — `TestLoadServerTLSConfigWithMTLS_OptionalClientCert`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:462` — `TestLoadServerTLSConfigWithMTLS_WithCNWhitelist`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:487` — `TestLoadServerTLSConfigWithMTLS_MissingClientCA`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:561` — `TestLoadClientTLSConfigWithMTLS_Disabled`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:581` — `TestLoadClientTLSConfigWithMTLS_Enabled`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:604` — `TestLoadClientTLSConfigWithMTLS_MissingCert`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:622` — `TestLoadClientTLSConfigWithMTLS_MissingKey`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:641` — `TestBackwardCompatibility_ServerWithoutMTLS`; target `cleanup`.
- `pkg/tlsutil/tlsutil_test.go:664` — `TestBackwardCompatibility_ClientWithoutMTLS`; target `cleanup`.

Source evidence:

- pkg/tlsutil/tlsutil_test.go:64 — setupTestFiles has named fourth result cleanup func().
- pkg/tlsutil/tlsutil_test.go:78 — cleanup is assigned a closure that calls os.RemoveAll(tmpDir); the results return at line 82.

Dependencies required for a reviewable resolution:

- Each enclosing caller declaration and its four-result binding
- pkg/tlsutil/tlsutil_test.go :: setupTestFiles
- Resolved standard-library os.RemoveAll symbol

No certificate generator internals are needed to prove what the returned cleanup does. No wall-clock bound is claimed for RemoveAll.

### B. Vocabulary registry restoration (13)

Resolve the returned callable and classify outside the admitted lifecycle Stop contract (non-lifecycle-stop).

Exact frozen candidates:

- `pkg/fusion/fusionvocab/signals_test.go:37` — `TestPredicateSalience`; target `vocabulary.SnapshotRegistry()`.
- `pkg/projection/contract/contract_test.go:11` — `TestContractValidateUsesVocabularyProfiles`; target `vocabulary.SnapshotRegistry()`.
- `pkg/projection/contract/contract_test.go:43` — `TestValidateShapeSkipsPredicateDeclaration`; target `vocabulary.SnapshotRegistry()`.
- `pkg/projection/contract_test.go:12` — `TestContractUsesOnlyReconcileAndAppendIntent`; target `vocabulary.SnapshotRegistry()`.
- `pkg/projection/contract_test.go:32` — `TestValidateContractsRejectsDuplicateNames`; target `vocabulary.SnapshotRegistry()`.
- `pkg/projection/contract_test.go:48` — `TestContractLiteralCompilesAgainstAliases`; target `vocabulary.SnapshotRegistry()`.
- `pkg/projection/contract_test.go:70` — `TestOverlappingLocalContractsConstruct`; target `vocabulary.SnapshotRegistry()`.
- `pkg/projection/mutation_client_test.go:299` — `projectionTestContract`; target `vocabulary.SnapshotRegistry()`.
- `pkg/projection/mutation_client_test.go:420` — `TestCreateFillsFromRegisteredContract`; target `vocabulary.SnapshotRegistry()`.
- `test/contract/predicate_datatype_contract_test.go:95` — `TestEveryCanonicalDataTypeHasExactlyOneExportRendering`; target `restore`.
- `test/contract/predicate_datatype_contract_test.go:280` — `TestFrameworkPredicateDataTypesAreCanonicalAndRatcheted`; target `restore`.
- `test/e2e/scenarios/lessons/scenario_test.go:313` — `TestComposeScenarioClientsRegistersBuiltinsBeforeContractValidation`; target `restore`.
- `vocabulary/builtins/register_test.go:13` — `TestRegisterProvidesBootVocabularyWithoutIncidentalInitialization`; target `restore`.

Source evidence:

- vocabulary/registry.go:656 — SnapshotRegistry copies predicateRegistry under registryMu and returns a closure restoring that map under the same mutex.
- Callers use both t.Cleanup(vocabulary.SnapshotRegistry()) and a returned restore variable invoked by defer.

Dependencies required for a reviewable resolution:

- Each enclosing caller declaration and callback binding
- vocabulary/registry.go :: SnapshotRegistry
- vocabulary/registry.go :: registryMu and predicateRegistry declarations and their resolved types

Shared evidence may explain all thirteen sites, but each exact candidate still needs its own identity/freshness. No package exemption or lock completion guarantee.

### C. Graph-index returned cleanup hides lifecycle debt (9)

Resolve the exact third returned callable. Expose its Background-fed Stop as unbounded-terminal-cleanup; newly exposed debt requires independent exact source review.

Exact frozen candidates:

- `processor/graph-index/query_integration_test.go:76` — `TestQueryOutgoing_Integration`; target `cleanup`.
- `processor/graph-index/query_integration_test.go:163` — `TestQueryIncoming_Integration`; target `cleanup`.
- `processor/graph-index/query_integration_test.go:225` — `TestQueryAlias_Integration`; target `cleanup`.
- `processor/graph-index/query_integration_test.go:283` — `TestQueryPredicate_Integration`; target `cleanup`.
- `processor/graph-index/query_integration_test.go:354` — `TestContextTimeout_Integration`; target `cleanup`.
- `processor/graph-index/query_integration_test.go:382` — `TestConcurrentQueries_Integration`; target `cleanup`.
- `processor/graph-index/query_integration_test.go:431` — `TestQueryNotFound_Integration`; target `cleanup`.
- `processor/graph-index/query_integration_test.go:502` — `TestQueryInvalidRequest_Integration`; target `cleanup`.
- `processor/graph-index/query_integration_test.go:724` — `TestQueryStatus_RevisionLag_Integration`; target `cleanup`.

Source evidence:

- processor/graph-index/query_integration_test.go:29 — setupIntegrationTest creates graphIndexComp and returns cleanup as its third result.
- processor/graph-index/query_integration_test.go:65 — the returned closure calls graphIndexComp.Stop(context.Background()); results return at line 70.

Dependencies required for a reviewable resolution:

- Each enclosing caller declaration and third-result binding
- processor/graph-index/query_integration_test.go :: setupIntegrationTest
- CreateGraphIndex result and *Component type assertion/binding
- processor/graph-index :: (*Component).Stop and receiver declaration

These nine frozen unresolved callback records are not nine automatically approved debt entries. The next census must preserve physical-site/ownership-variant identity semantics and show the real resulting debt. Do not infer all no-argument callbacks are non-lifecycle.

### D. HTTP test server close callbacks (3)

Resolve the returned method value to (*httptest.Server).Close and classify outside the admitted context-taking lifecycle Stop contract.

Exact frozen candidates:

- `input/http/http_lifecycle_test.go:61` — `TestInputStartStopAndCompletedRepeat`; target `closeServer`.
- `input/http/http_lifecycle_test.go:88` — `TestInputParentCancellationStopsOwnedLoop`; target `closeServer`.
- `input/http/http_lifecycle_test.go:110` — `TestInputStopDeadlineDoesNotPromiseRejoin`; target `closeServer`.

Source evidence:

- input/http/http_lifecycle_test.go:29 — newLifecycleHTTPInput creates an httptest server.
- input/http/http_lifecycle_test.go:40 — the helper returns the input and server.Close.

Dependencies required for a reviewable resolution:

- Each enclosing caller declaration and second-result binding
- input/http/http_lifecycle_test.go :: newLifecycleHTTPInput
- Resolved net/http/httptest.(*Server).Close and *Server receiver type

Foreign contextless Close is outside this guard, not proved bounded.

### E. Native NATS connection close callbacks (2)

Resolve each callback method value to github.com/nats-io/nats.go.(*Conn).Close and classify outside the admitted context-taking lifecycle Stop contract.

Exact frozen candidates:

- `internal/boot/root_resources_test.go:27` — `inProcessClient`; target `connection.Close`.
- `natsclient/client_connect_test.go:118` — `newConnectCandidate`; target `candidate.Close`.

Source evidence:

- internal/boot/root_resources_test.go:27 — inProcessClient registers connection.Close after nats.Connect.
- natsclient/client_connect_test.go:118 — newConnectCandidate registers candidate.Close after nats.Connect.
- go.mod:12 — github.com/nats-io/nats.go v1.52.0.
- Local gopls definition resolved (*nats.Conn).Close to /Users/coby/go/pkg/mod/github.com/nats-io/nats.go@v1.52.0/nats.go:6042.

Dependencies required for a reviewable resolution:

- internal/boot/root_resources_test.go :: inProcessClient
- natsclient/client_connect_test.go :: newConnectCandidate
- Selected github.com/nats-io/nats.go module/version
- Resolved nats.Connect return type and (*Conn).Close symbol

A local cache path is supporting evidence, not a portable baseline key. Preserve selected module/symbol binding. No foreign Close completion claim.

### F. Fusion readiness watcher close (1)

Known contextless owner chain outside the admitted context-taking lifecycle Stop contract; proposed non-lifecycle-stop classification means policy exclusion only.

Exact frozen candidates:

- `pkg/fusion/fusionnats/client_test.go:172` — `newStatusClient`; target `c.Close`.

Source evidence:

- pkg/fusion/fusionnats/client_test.go:172 — newStatusClient registers c.Close.
- pkg/fusion/fusionnats/client.go:64 — Client.statusWatch is *readiness.Watcher.
- pkg/fusion/fusionnats/client.go:89 — (*Client).Close detaches the watcher, then invokes watch.Stop at line 104.
- graph/readiness/watcher.go:276 — (*Watcher).Stop cancels and waits on w.done without a supplied context.

Dependencies required for a reviewable resolution:

- pkg/fusion/fusionnats/client_test.go :: newStatusClient
- pkg/fusion/fusionnats/client.go :: Client type and (*Client).Close
- graph/readiness/watcher.go :: Watcher type and (*Watcher).Stop

This is not proof of boundedness or leak freedom. Expanding this issue into generic contextless Close/cancel/join contracts would change scope.

### G. Activity view stop callbacks (4)

Resolve (*Component).stopActivityView; known contextless helper outside the admitted context-taking lifecycle Stop contract.

Exact frozen candidates:

- `processor/agentic-dispatch/delivery_owner_test.go:514` — `seedCurrentLoops`; target `c.stopActivityView`.
- `processor/agentic-dispatch/http_activity_test.go:379` — `newActivityTestComponent`; target `comp.stopActivityView`.
- `processor/agentic-dispatch/http_activity_test.go:705` — `TestActivityStream_NilNATSClientDegradesPerRequest`; target `comp.stopActivityView`.
- `processor/agentic-dispatch/http_activity_test.go:898` — `TestActivityStreamCoalescesToViewRate`; target `comp.stopActivityView`.

Source evidence:

- processor/agentic-dispatch/http_activity.go:251 — stopActivityView reads activityCancel and activityDone, cancels, waits on done, then clears fields.
- All four registrations bind a typed *Component method value.

Dependencies required for a reviewable resolution:

- Every enclosing caller declaration listed below
- processor/agentic-dispatch :: Component type, activityCancel and activityDone fields
- processor/agentic-dispatch/http_activity.go :: (*Component).stopActivityView

Record the unbounded channel receive as an adjacent remediation gap, without claiming an observed hang. Do not label the helper bounded or broaden this guard into all joins.

### H. Explicit release of the blocked-stop test gate (1)

Resolve (*blockingStopWatcher).release; non-lifecycle-stop gate release.

Exact frozen candidates:

- `processor/graph-index/lifecycle_order_test.go:230` — `newBlockedStopOwner`; target `watcher.release`.

Source evidence:

- processor/graph-index/lifecycle_order_test.go:230 — newBlockedStopOwner registers watcher.release.
- processor/graph-index/lifecycle_order_test.go:252 — blockingStopWatcher declaration.
- processor/graph-index/lifecycle_order_test.go:275 — release calls releaseOnce.Do with a closure closing stopRelease.

Dependencies required for a reviewable resolution:

- processor/graph-index/lifecycle_order_test.go :: newBlockedStopOwner
- processor/graph-index/lifecycle_order_test.go :: blockingStopWatcher type and (*blockingStopWatcher).release
- releaseOnce and stopRelease field bindings

Resolve the method body and receiver; do not use its name as an exemption.

### I. Observed manager start release parameter (1)

Substitute the four source-resolved release arguments. Each releases an explicit test gate; proposed non-lifecycle-stop for this callback invocation.

Exact frozen candidates:

- `service/startup_observability_test.go:67` — `beginObservedManagerStart`; target `release`.

Source evidence:

- service/startup_observability_test.go:62 — beginObservedManagerStart registers a cleanup closure; line 67 calls non-nil release.
- The cleanup subsequently waits for start completion using a five-second select and calls manager.StopAll with a newly bounded context. Those operations are separate from the release candidate.
- gopls found four caller sites: service/startup_observability_test.go:584 and :699; service/startup_observability_amendment_test.go:348 and :389.
- Those arguments are closures using sync.Once.Do to close a channel, including local alias composition.

Dependencies required for a reviewable resolution:

- service/startup_observability_test.go :: beginObservedManagerStart
- Full enclosing caller declarations at service/startup_observability_test.go:584 and :699
- Full enclosing caller declarations at service/startup_observability_amendment_test.go:348 and :389
- Their local release closures, sync.Once bindings, and closed-channel bindings

The four argument/caller declarations form part of the finite evidence set; an added unresolved or cleanup-bearing caller must not inherit a name-based approval.

### J. MilestoneSubscriber returned context-taking stop (5)

Resolve Start return binding to (*milestoneConsumerOwner).stop. The :257 supplied context is finite; refusal :74 is Background-fed; :339, :480, and milestone policy :51 use deadline-free testing.T.Context. The last four are proposed unbounded-terminal-cleanup.

Exact frozen candidates:

- `agentic/agentrun/agentrun_integration_test.go:257` — `TestIntegration_MilestoneSubscriberBindsAStreamThatAppearsDuringStart`; target `stop`.
- `agentic/agentrun/agentrun_integration_test.go:339` — `TestIntegration_MilestoneSubscriber_StartsWhenStreamPresent`; target `stop`.
- `agentic/agentrun/agentrun_integration_test.go:480` — `TestIntegration_MilestoneSubscriberProductionEnvelopeCallbacks`; target `stop`.
- `agentic/agentrun/milestone_policy_integration_test.go:51` — `TestIntegration_MilestoneLanesDeclareFiniteMaxDeliver`; target `stop`.
- `agentic/agentrun/refusal_wiring_integration_test.go:74` — `TestIntegration_MilestoneLanesDeclareBufferedRefusal`; target `stop`.

Source evidence:

- agentic/agentrun/agentrun.go:938 — (*MilestoneSubscriber).Start has named stop func(context.Context) error result.
- agentic/agentrun/agentrun.go:963 — an owner is created; line 964 assigns stop = owner.stop.
- agentic/agentrun/agentrun.go:845 — (*milestoneConsumerOwner).stop accepts the supplied context and coordinates consumer/callback shutdown.
- agentic/agentrun/agentrun_integration_test.go:218 — TestIntegration_MilestoneSubscriberBindsAStreamThatAppearsDuringStart derives its 60-second context, used by deferred stop at :257.

Dependencies required for a reviewable resolution:

- Each enclosing caller declaration and Start result/context bindings
- agentic/agentrun/agentrun.go :: MilestoneSubscriber type and (*MilestoneSubscriber).Start
- agentic/agentrun/agentrun.go :: milestoneConsumerOwner type and (*milestoneConsumerOwner).stop
- Typed context constructors, and testing.T.Context semantic evidence for the three T.Context calls

Do not infer callback behavior from the result name. A finite supplied context is not a guarantee that stop returns on time. Newly exposed unbounded variants need exact independent source review.

### K. Reusable component lifecycle suite (4)

Resolve :185 and :233 as bounded local contexts. Review :115 as an exact deliberate nil-context contract case. For :388 retain uncertain classification unless argument/ownership propagation resolves it; an exact unresolved→ordinary-only disposition is possible only on finite complete caller/branch evidence.

Exact frozen candidates:

- `component/lifecycle_test_suite.go:115` — `testNilStopContext`; target `github.com/c360studio/semstreams/component.Stop`.
- `component/lifecycle_test_suite.go:185` — `testParallelFreshInstances`; target `github.com/c360studio/semstreams/component.Stop`.
- `component/lifecycle_test_suite.go:233` — `testNoResourceLeaks`; target `github.com/c360studio/semstreams/component.Stop`.
- `component/lifecycle_test_suite.go:388` — `Stop`; target `github.com/c360studio/semstreams/component.Stop`.

Source evidence:

- component/lifecycle_test_suite.go:185 and :233 — Stop arguments come from immediately preceding five-second context.WithTimeout calls at :184 and :232.
- component/lifecycle_test_suite.go:114 — testNilStopContext explicitly asserts an error for Stop(nil) at :115.
- component/lifecycle_test_suite.go:36 — testPortableLifecycleFloor selects named cases including nil Stop at :46; :54 invokes tt.test within t.Run; StandardLifecycleTests is at :20.
- component/lifecycle_test_suite.go:384 — (*ErrorInjectingComponent).Stop forwards its supplied context at :388 when injectStopError is false.
- component/lifecycle_test_suite.go:392 — TestErrorInjection constructs wrapper at :427, invokes ordinary operation Stop at :445 and final cleanup at :455.

Dependencies required for a reviewable resolution:

- component/lifecycle_test_suite.go :: testParallelFreshInstances and testNoResourceLeaks, including local context constructors
- For :115: testNilStopContext, testPortableLifecycleFloor table and t.Run dispatch, StandardLifecycleTests
- For :388: ErrorInjectingComponent type, NewErrorInjectingComponent, (*ErrorInjectingComponent).Stop, TestErrorInjection complete table/branch/operation/cleanup path
- component :: LifecycleComponent interface

Do not conflate the deliberate nil contract with wrapper forwarding or label all finalization as deliberate contract. A new cleanup or unresolved caller must remain a separate blocking variant despite any ordinary-only resolution.

### L. Dispatch fallback using testing context (1)

Typed testing.T.Context has no finite deadline; proposed unbounded-terminal-cleanup for this fallback in t.Cleanup.

Exact frozen candidates:

- `processor/agentic-dispatch/lifecycle_causal_test.go:219` — `TestComponentStopCancelsAndJoinsBlockedActivityAcquisition`; target `github.com/c360studio/semstreams/processor/agentic-dispatch.Stop`.

Source evidence:

- processor/agentic-dispatch/lifecycle_causal_test.go:212 — cleanup registration includes the stop-not-started fallback c.Stop(t.Context()) at :219.

Dependencies required for a reviewable resolution:

- processor/agentic-dispatch/lifecycle_causal_test.go :: TestComponentStopCancelsAndJoinsBlockedActivityAcquisition
- Typed *Component.Stop and testing.T.Context bindings
- Testing context semantic evidence below

A context already canceled by testing cleanup order does not establish a finite deadline. The surrounding causal lifecycle test does not make this fallback a deliberate nil/unbounded contract assertion.

### M. Service shutdown tracking forwarding method (1)

Proposed exact ordinary-only ownership disposition with uncertain context classification retained, if independent review confirms the finite registration/dispatch/caller evidence. Otherwise remain blocking.

Exact frozen candidates:

- `service/registry_race_test.go:120` — `Stop`; target `github.com/c360studio/semstreams/service.Stop`.

Source evidence:

- service/registry_race_test.go:113 — shutdownTrackingService embeds mockService.
- service/registry_race_test.go:118 — (*shutdownTrackingService).Stop invokes its callback then forwards ctx to mockService.Stop at :120.
- gopls found its concrete creation at service/registry_race_test.go:261 in TestServiceManager_ReverseOrderShutdown (:247).
- That test registers the wrapped factory, creates/starts services, then calls manager.StopAll(context.Background()) at :280 to assert reverse shutdown order.
- service/service_manager.go:886 — service.Stop(ctx) is the production dispatch boundary reached from StopAll.

Dependencies required for a reviewable resolution:

- service/registry_race_test.go :: shutdownTrackingService type and (*shutdownTrackingService).Stop
- service/registry_race_test.go :: mockService type and (*mockService).Stop
- service/registry_race_test.go :: TestServiceManager_ReverseOrderShutdown including factory registration, CreateService, Start, StopAll, assertions
- service/service_manager.go :: enclosing StopAll/stop-all dispatch declarations and service interface binding

The two Stop declarations require receiver qualification. No blanket mock-service exemption. A new cleanup/unresolved caller must not be masked by this exact ordinary-only record.

### N. Testify suite lifecycle assertion (1)

Proposed exact ordinary-only ownership disposition with current uncertain classification retained; an exact deliberate contract classification is optional only if its intent is independently established. Do not add generic reflection analysis.

Exact frozen candidates:

- `service/suite_integration_test.go:125` — `TestService_LifecycleSuite`; target `github.com/c360studio/semstreams/service.Stop`.

Source evidence:

- service/suite_integration_test.go:54 — TestServiceSuite calls suite.Run(t, new(ServiceSuite)) at :56.
- service/suite_integration_test.go:125 — (*ServiceSuite).TestService_LifecycleSuite stops its BaseService between running/stopped lifecycle assertions.
- The selected github.com/stretchr/testify v1.11.1 suite.Run implementation begins at local suite/suite.go:126, enumerates Test-prefixed methods around :154, creates a test invocation around :164 and invokes method.Func.Call at :196.

Dependencies required for a reviewable resolution:

- service/suite_integration_test.go :: ServiceSuite type, TestServiceSuite, (*ServiceSuite).TestService_LifecycleSuite
- Typed BaseService and Stop binding
- Selected github.com/stretchr/testify module/version and suite.Run symbol/dispatch evidence

Exact reflection-backed evidence is finite here. Unknown dispatch without defensible finite dependencies stays blocking. This is not permission to mark every Test-prefixed method ordinary.

## Narrow testing.T.Context semantics

The local toolchain is Go 1.26.4. `/usr/local/go/src/testing/testing.go:1589` returns `c.ctx`.
Its `T.Run` creates that context with `context.WithCancel(context.Background())` at :2071 and stores it at :2083;
the root test path creates it similarly at :2565 and stores it at :2573. The CI-selected Go 1.26.3 source uses the
same semantics: Context at :1513–1515, T.Run construction at :1963, and root construction/storage at :2427/:2435.
Primary source: https://raw.githubusercontent.com/golang/go/go1.26.3/src/testing/testing.go .

Recommendation: support the type-resolved `(*testing.T).Context` semantic as deadline-free, hence unbounded for the
supplied-context policy. Preserve the ordinary finite interpretation of an explicit WithTimeout/WithDeadline child.
Use a small toolchain-contract test asserting no Context deadline at top level and within a subtest, so a future
toolchain semantic change fails visibly. Avoid patch-version source fingerprints or a new external-source engine.
The documented cancellation-at-cleanup behavior must never be substituted for proof of a finite deadline.

## Bounded manual evidence representation

Qualified method dependencies must distinguish receiver and pointer/value form where meaningful, package, and method
name; two `Stop` methods in one file cannot match the first simple-name declaration. Type-declaration dependencies
must include the actual receiver/field/interface declaration used by the review, not only value declarations. Existing
source normalization/fingerprint rules must preserve comment/format stability while detecting semantic dependency
changes. Ambiguity, missing declarations, module mismatch, or failed source/type loads refuses admission.

A proposed applicability-only manual record may move one exact unresolved ownership variant to ordinary-only while
retaining uncertain classification. It must state its question, source-based reason, reviewer, issue, exact candidate
identity/fingerprint, and finite qualified dependency set. It cannot override automatic cleanup-owned analysis, alter
a known classification dishonestly, or apply to a different/new ownership variant of the same source site. Added
cleanup or unresolved callers to the same previously dispositioned helper must remain blocking. Unenumerable callers
or dependencies stay uncertain. No package/path-wide approvals, absence inference from failed loads, or blanket
Test-prefix/mock/reflection exemption is proposed. These are pending design/spec review and final-snapshot resolution.

Required focused regression: review an exact ordinary helper/caller variant, then add a cleanup or unresolved caller
to that same helper. The old record must not hide the new blocking variant. Change a named helper body without changing
the original unknown callsite; the old resolution must become stale. Pair same-named receiver methods and type-only
semantic changes with harmless comment/format changes to prove exact dependency selection and freshness.

## Recommended next move

1. Complete bounded return-slot/method-value/callback-argument resolution, the two local finite-context cases, and typed
   testing.T.Context semantics. Preserve all cleanup and unresolved ownership variants.
2. Take one refreshed census checkpoint. Keep the 325 previously reviewed exact debt checkpoint intact; reconcile newly
   exposed graph-index and milestone/dispatch debt independently, without mechanically copying approval.
3. Submit the few remaining contract-intent/ordinary-ownership proposals with exact qualified dependencies for independent
   review. Use manual applicability only where finite source evidence is defensible; otherwise remain blocking.
4. Treat contextless join risks as adjacent remediation notes. They do not authorize expanding this guard or repairing
   all test cleanup in this change.

## Source checkpoint

Each hash captures the worktree file used for these source facts. HEAD equality is reported rather than assumed.
The JSON companion includes both hashes.

| Source | Worktree SHA256 | Matches base HEAD |
|---|---|---|
| `agentic/agentrun/agentrun.go` | `032f153c3909a1aadbd086debff5752ba8323223751220a2a036f75268843352` | true |
| `agentic/agentrun/agentrun_integration_test.go` | `4ca6a48b18fbf0f041c8df0a133bc4939446e94276caf2caea88f363ce31e33e` | true |
| `agentic/agentrun/milestone_policy_integration_test.go` | `b51f597918545a8ac82662e264efb3caeecdadd3fdfa405b3f148621c5124449` | true |
| `agentic/agentrun/refusal_wiring_integration_test.go` | `3ec2432addf1c8351ee932e40951685b3c1c8587f0bbacac884d625643ba2276` | true |
| `component/lifecycle.go` | `42bba6807fd871ea5a964a5de405bd66ded52a02078d49ee148a297178935f5f` | true |
| `component/lifecycle_test_suite.go` | `da21a4b65040b55a206c5289d986ed612c9cda1d6ec3d0e56079388691547d89` | true |
| `go.mod` | `5c5604facfe641eed2f0b47c78839dfce1721a769b8227c3792600a68a823062` | true |
| `graph/readiness/watcher.go` | `985abddcf6c6d380b46c2927eaf6ab14d5d0c0b0eb7f0fb56ed9c301cef62209` | true |
| `input/http/http_lifecycle_test.go` | `67c78b1e2970272880efd126bd25389c34d1f02b550dcbf8125bfce005db78ed` | true |
| `internal/boot/root_resources_test.go` | `1e26c56f91aac2c54264944f6975e62960adf886099a8577233b1670ec088774` | true |
| `natsclient/client_connect_test.go` | `2b5beb9b2b6115dde43fc6dffba46217db1a833cc5d8c396fc6abaaa2bf63db6` | true |
| `pkg/fusion/fusionnats/client.go` | `c592e6ea135f7cd49c7d4a3b3b5da33c666f1c1430f0f7c58c3252168f2a371f` | true |
| `pkg/fusion/fusionnats/client_test.go` | `c3a340d9dcac0d4b934ab9b92f3e208356775c0e87b51021ae5092079c97595d` | true |
| `pkg/fusion/fusionvocab/signals_test.go` | `e124af6836e3ffbe0b0d6fc365ce8789f127b7628b8bb38e042749d8c9eb69b0` | true |
| `pkg/projection/contract/contract_test.go` | `fdc42980591a3b564edab7811d4d37510049c793be4fc91f987580377e5a97bc` | true |
| `pkg/projection/contract_test.go` | `18b470b4d7836320cbedab37965e6fe00c7808c60168040d76d5b831120b8ced` | true |
| `pkg/projection/mutation_client_test.go` | `0b442a7e96b0beefee5dcf94739a03cf3a0a38654c1c9c0ad515bdab873c8698` | true |
| `pkg/tlsutil/tlsutil_test.go` | `3f0b20ecc6f14677bc76d467984653c6feaefa4f302af193edca68cc393b242b` | true |
| `processor/agentic-dispatch/delivery_owner_test.go` | `761801ab27799a56604467587bb3bd83c0f8055effa9692dd85615cb655e298f` | true |
| `processor/agentic-dispatch/http_activity.go` | `24943fc44c7e38c9c3074274f77d5cb864fc3a38f6927a08749a66aecf3b2afe` | true |
| `processor/agentic-dispatch/http_activity_test.go` | `571271659d6b88b871ae2f675ee55a56924ed4457948a09c3b2f1ebf6ef14d23` | true |
| `processor/agentic-dispatch/lifecycle_causal_test.go` | `33602268a886996a9052b153f1c89c6b71ce50077d6344e954346c15bb0165db` | true |
| `processor/graph-index/component.go` | `21bbb25cf02d1bdc3c30b5bae966c99a8ba0abfd14e3d7840dd4f9b6970036cf` | true |
| `processor/graph-index/lifecycle_order_test.go` | `07a0610e3d60832cd9da159f34bf58edd12497e1c18acae9d12afe585416741c` | true |
| `processor/graph-index/query_integration_test.go` | `cdc1f5b95d66c19a25c83cd6423eeab9dc2c09588050c6dc37a452e31eace9b8` | true |
| `service/registry_race_test.go` | `8c103c0a44d657991fe0fb1ac3a703f093505ae7558496a865b7c71e2030ddfd` | true |
| `service/service_manager.go` | `61f263462084783d1dd8baac87f99bb5254444a729849424ef5a35d25e26805d` | true |
| `service/startup_observability_amendment_test.go` | `4a06fa7252bf86153ae869cdd5f81e4132e2c6ea360096248a5bd95c1dd2a207` | true |
| `service/startup_observability_test.go` | `3666efe40ac5eb156acdfaf2472b087ca77042b030bcf7f272fe2be5ce383294` | true |
| `service/suite_integration_test.go` | `afa58429632a2ed7486a0c8ae3757828d62f8c7ca2d115bd88dcfa817f13a71f` | true |
| `test/contract/predicate_datatype_contract_test.go` | `54c0ba20c7603f18756a21679417759198208598370e0713c56a19b939f0eb62` | true |
| `test/e2e/scenarios/lessons/scenario_test.go` | `105c913758d51341e74f6f6a014ca779c815b26868cc222530063c41b15fbf23` | true |
| `vocabulary/builtins/register_test.go` | `33d310267693dc0c31cd6811766fa834c098d9d3516a3044ee0efc1b9a6ea606` | true |
| `vocabulary/registry.go` | `0490a805ddfb415ac8280079edd5bdc7349031cae8331ee88fa9da325ac3187f` | true |

# Graph-query test cleanup inventory

base: f9cb87600c2c713247399d005bd0a98dce12c5b6

Production baseline: `41d6236a84a8443b284785372126a092331096f6`
Change: `graph-query-test-cleanup`
Issue/claim: #1433 / draft PR #1434
Phase: inventory only; awaiting independent `INVENTORY PASS`.

## Problem and measured boundary

The current manifest contains 273 legacy entries and 94 reviewed resolutions. Exactly 39 entries concern graph-query,
across five files. No entry in `resolutions` references `processor/graph-query/`.

All 39 sites supply `context.Background()` to terminal Stop. Thirty-six are deferred calls/closures and three use
`testing.Cleanup`. Thirty-eight discard the terminal result; B38 checks it with `require.NoError`. None checks
terminal-context expiry. All registrations occur after Initialize and a Start invocation, leaving earlier setup exits
outside that registration.

These are measured ownership liabilities, not evidence that every case hangs. The manifest is not a complete census
of explicit lifecycle calls, omitted Stop operations, substrate cleanup, or native join behavior.

The only active change artifacts are `proposal.md` and `tasks.md`; both were read fully. Current specifications read
fully: `test-cleanup-policy`, `component-lifecycle`, `runtime-context-ownership`, and `graph-query`.

## 1. Claimed gap: exact baseline ledger

For every row, the exact identity is:

```text
<source path>|<enclosing function>|<origin>|github.com/c360studio/semstreams/processor/graph-query.Stop|*github.com/c360studio/semstreams/processor/graph-query.Component|unbounded|1
```

Every row has classification `unbounded-terminal-cleanup`, owner `#1064`, and the existing direct-Background
known-debt reason. The manifest line column identifies its exact identity record in
`test/testinfra/cleanup_baseline.json`.

| ID | Enclosing function | Origin | Source pin and exact line text | Manifest line |
|---|---|---|---|---|
| B01 | TestAttack_ConcurrentHealthChecks | defer | `processor/graph-query/attack_test.go:235` — `defer comp.Stop(context.Background())` | 1419 |
| B02 | TestAttack_ConcurrentMetricsAccess | defer | `processor/graph-query/attack_test.go:265` — `defer comp.Stop(context.Background())` | 1426 |
| B03 | TestAttack_DeeplyNestedJSON | defer | `processor/graph-query/attack_test.go:331` — `defer comp.Stop(context.Background())` | 1433 |
| B04 | TestAttack_EmptyRequestBody | defer | `processor/graph-query/attack_test.go:457` — `defer comp.Stop(context.Background())` | 1440 |
| B05 | TestAttack_MalformedJSON | defer | `processor/graph-query/attack_test.go:97` — `defer comp.Stop(context.Background())` | 1447 |
| B06 | TestAttack_MissingEntityID | defer | `processor/graph-query/attack_test.go:142` — `defer comp.Stop(context.Background())` | 1454 |
| B07 | TestAttack_MissingStartEntity | defer | `processor/graph-query/attack_test.go:161` — `defer comp.Stop(context.Background())` | 1461 |
| B08 | TestAttack_PathSearchEmptyStartEntity | defer | `processor/graph-query/attack_test.go:394` — `defer comp.Stop(context.Background())` | 1468 |
| B09 | TestAttack_PathSearchExcessiveMaxDepth | defer | `processor/graph-query/attack_test.go:372` — `defer comp.Stop(context.Background())` | 1475 |
| B10 | TestAttack_VeryLongEntityID | defer | `processor/graph-query/attack_test.go:309` — `defer comp.Stop(context.Background())` | 1482 |
| B11 | TestAttack_ZeroMaxDepth | defer | `processor/graph-query/attack_test.go:75` — `defer comp.Stop(context.Background())` | 1489 |
| B12 | TestIntegration_BatchQueryPassthrough_ForwardsToGraphIngest | cleanup | `processor/graph-query/batch_passthrough_integration_test.go:73` — `t.Cleanup(func() { _ = gq.Stop(context.Background()) })` | 1496 |
| B13 | TestIntegration_BatchQueryPassthrough_RejectsMalformedRequest | cleanup | `processor/graph-query/batch_passthrough_integration_test.go:139` — `t.Cleanup(func() { _ = gq.Stop(context.Background()) })` | 1503 |
| B14 | TestIntegration_AnswerSynthesis | defer | `processor/graph-query/component_integration_test.go:493` — `defer graphQuery.Stop(context.Background())` | 1510 |
| B15 | TestIntegration_CommunityCacheCrossLevelCollision | defer | `processor/graph-query/component_integration_test.go:743` — `defer graphQuery.Stop(context.Background())` | 1517 |
| B16 | TestIntegration_EnrichGlobalResponse | defer | `processor/graph-query/component_integration_test.go:612` — `defer graphQuery.Stop(context.Background())` | 1524 |
| B17 | TestIntegration_GraphRAGLifecycle | defer | `processor/graph-query/component_integration_test.go:298` — `defer graphQuery.Stop(context.Background())` | 1531 |
| B18 | TestIntegration_MetricsTracking | defer | `processor/graph-query/component_integration_test.go:197` — `defer graphQuery.Stop(context.Background())` | 1538 |
| B19 | TestIntegration_PathSearch_Structure | defer | `processor/graph-query/component_integration_test.go:231` — `defer graphQuery.Stop(context.Background())` | 1545 |
| B20 | TestIntegration_StaticRouting | defer | `processor/graph-query/component_integration_test.go:676` — `defer graphQuery.Stop(context.Background())` | 1552 |
| B21 | TestComponent_PathSearch_ContextCancellation | defer | `processor/graph-query/component_test.go:744` — `defer comp.Stop(context.Background())` | 1559 |
| B22 | TestComponent_PathSearch_CyclicGraph | defer | `processor/graph-query/component_test.go:841` — `defer comp.Stop(context.Background())` | 1566 |
| B23 | TestComponent_PathSearch_DirectionBoth | defer | `processor/graph-query/component_test.go:1041` — `defer comp.Stop(context.Background())` | 1573 |
| B24 | TestComponent_PathSearch_DirectionIncoming | defer | `processor/graph-query/component_test.go:985` — `defer comp.Stop(context.Background())` | 1580 |
| B25 | TestComponent_PathSearch_MaxDepthEnforced | defer | `processor/graph-query/component_test.go:704` — `defer comp.Stop(context.Background())` | 1587 |
| B26 | TestComponent_PathSearch_MaxPathsLimit | defer | `processor/graph-query/component_test.go:1210` — `defer comp.Stop(context.Background())` | 1594 |
| B27 | TestComponent_PathSearch_PredicateFilter_NoMatch | defer | `processor/graph-query/component_test.go:1151` — `defer comp.Stop(context.Background())` | 1601 |
| B28 | TestComponent_PathSearch_PredicateFilter_Single | defer | `processor/graph-query/component_test.go:1096` — `defer comp.Stop(context.Background())` | 1608 |
| B29 | TestComponent_PathSearch_SimpleTraversal | defer | `processor/graph-query/component_test.go:643` — `defer comp.Stop(context.Background())` | 1615 |
| B30 | TestComponent_PathSearch_StartEntityNotFound | defer | `processor/graph-query/component_test.go:797` — `defer comp.Stop(context.Background())` | 1622 |
| B31 | TestComponent_PathSearch_Timeout | defer | `processor/graph-query/component_test.go:771` — `defer comp.Stop(context.Background())` | 1629 |
| B32 | TestComponent_QueryEntity_ComponentUnavailable | defer | `processor/graph-query/component_test.go:545` — `defer comp.Stop(context.Background())` | 1636 |
| B33 | TestComponent_QueryEntity_InvalidRequest | defer | `processor/graph-query/component_test.go:563` — `defer comp.Stop(context.Background())` | 1643 |
| B34 | TestComponent_QueryEntity_PassthroughSuccess | defer | `processor/graph-query/component_test.go:523` — `defer comp.Stop(context.Background())` | 1650 |
| B35 | TestComponent_QueryRelationships_TransformSuccess | defer | `processor/graph-query/component_test.go:595` — `defer comp.Stop(context.Background())` | 1657 |
| B36 | TestComponent_Start_AlreadyStarted | defer | `processor/graph-query/component_test.go:397` — `defer comp.Stop(context.Background())` | 1664 |
| B37 | TestComponent_Start_Success | defer | `processor/graph-query/component_test.go:376` — `defer comp.Stop(context.Background())` | 1671 |
| B38 | TestGraphQueryStartRegistersStableLocalSearchResponder | cleanup | `processor/graph-query/component_test.go:245` — `t.Cleanup(func() { require.NoError(t, comp.Stop(context.Background())) })` | 1678 |
| B39 | TestIntegration_GraphQuery_SummaryBucketCreatedLate_Attaches | defer | `processor/graph-query/summary_bucket_late_attach_integration_test.go:73` — `defer func() { _ = gq.Stop(context.Background()) }()` | 1685 |

Counts: attack 11; batch integration 2; component integration 7; component unit 18; summary-late-attach integration 1.

## 2. Current spellings of ownership and authority

### Setup helpers and transfer boundaries

- `processor/graph-query/component_test.go:911` — `func createTestComponent(t *testing.T) *Component {`
- `processor/graph-query/component_test.go:915` — `return createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:919` — `func createTestComponentWithMockClient(t *testing.T, mockClient *mockNATSClient) *Component {`
- `processor/graph-query/component_test.go:929` — `require.NoError(t, err)`
- `processor/graph-query/component_test.go:934` — `return &Component{`

These helpers return an unstarted component. Port-resolution assertions occur before the component literal is acquired.
They neither Start nor retain terminal ownership. Their complete direct caller sets are below.

- `processor/graph-query/component_integration_test.go:19` — `func setupTestNATS(t *testing.T) (*natsclient.Client, func()) {`
- `processor/graph-query/component_integration_test.go:22` — `testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())`
- `processor/graph-query/component_integration_test.go:26` — `return testClient.Client, func() {}`

The returned cleanup callback is a no-op. Actual infrastructure ownership is the earlier `NewTestClient` cleanup:

- `natsclient/test_client.go:856` — `testClient, err := newTestClient(t.Context(), productionTestClientFactoryDeps, opts...)`
- `natsclient/test_client.go:863` — `t.Cleanup(func() {`
- `natsclient/test_client.go:864` — `if err := testClient.Terminate(); err != nil {`
- `natsclient/test_client.go:865` — `t.Errorf("clean up NATS test infrastructure: %v", err)`

Thus lexical component defers currently precede NATS testing cleanup. B12/B13 register component testing cleanup after
NATS testing cleanup, obtaining component-before-NATS LIFO order, but only after Start succeeds. This is distinct from
preserving live testing-derived Start authority: `testing.Cleanup` is too late for that if future fixture work derives
Start from `t.Context()`.

No helper in the 39-site path returns a running component. Provisional *running-owner* transfer is therefore not an
existing obligation hidden inside these helpers. Changing the helpers would nevertheless affect unstarted handler,
metadata and contract cases outside B01–B39.

### Typed helper callers

`gopls references` established 19 callers of `createTestComponent`, 33 callers of
`createTestComponentWithMockClient`, and 16 callers of `setupTestNATS`. Default and integration references for the
mock-client helper agree.

The following pins enumerate all 68 direct caller sites:

- `processor/graph-query/attack_test.go:39` — `comp := createTestComponent(t)`
- `processor/graph-query/attack_test.go:71` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:94` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:139` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:158` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:185` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:207` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:232` — `comp := createTestComponent(t)`
- `processor/graph-query/attack_test.go:262` — `comp := createTestComponent(t)`
- `processor/graph-query/attack_test.go:306` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:328` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:368` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:391` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:417` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:432` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/attack_test.go:454` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/batch_passthrough_integration_test.go:36` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/batch_passthrough_integration_test.go:115` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/community_summary_wire_integration_test.go:66` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:34` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:79` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:118` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:177` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:211` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:268` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:342` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:418` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:558` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:658` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_integration_test.go:705` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/component_test.go:242` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:264` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:275` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:295` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:305` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:324` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:333` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:348` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:371` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:382` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:392` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:406` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:418` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:520` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:542` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:560` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:592` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:640` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:700` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:741` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:768` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:794` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:838` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:870` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:890` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:915` — `return createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:982` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:1038` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:1093` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:1148` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:1207` — `comp := createTestComponentWithMockClient(t, mockClient)`
- `processor/graph-query/component_test.go:1235` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:1274` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:1296` — `comp := createTestComponent(t)`
- `processor/graph-query/component_test.go:1323` — `comp := createTestComponent(t)`
- `processor/graph-query/query_error_class_test.go:41` — `comp := createTestComponentWithMockClient(t, mock)`
- `processor/graph-query/summary_bucket_late_attach_integration_test.go:34` — `natsClient, cleanup := setupTestNATS(t)`
- `processor/graph-query/summary_view_lifecycle_integration_test.go:46` — `natsClient, cleanup := setupTestNATS(t)`

The extra returning helper is unstarted handler support:

- `processor/graph-query/query_error_class_test.go:39` — `func newComponentForHandlerTest(t *testing.T, mock *mockNATSClient) *Component {`
- `processor/graph-query/query_error_class_test.go:42` — `comp.router = NewStaticRouter(comp.logger)`
- `processor/graph-query/query_error_class_test.go:43` — `return comp`

Its downstream handler tests are outside the running-fixture population. The package-wide Start search finds no
component Start in `query_error_class_test.go` or its other pure-handler sibling files.

### Ordinary calls, lifecycle probes and setup escapes outside the 39 entries

These eight ordinary Stop sites are not deferred/Cleanup baseline entries. Their test intent and assertions matter
independently of guard admission:

| Case | Pin | Current meaning |
|---|---|---|
| Repeated fresh lifecycle loop | `processor/graph-query/attack_test.go:190` — `require.NoError(t, comp.Stop(context.Background()))` | Ten fresh instances; Initialize/Start assertion exits precede this terminal call. Goroutine-count check follows the loop. |
| Accepted Start cancellation | `processor/graph-query/attack_test.go:217` — `require.NoError(t, comp.Stop(context.Background()))` | Explicitly cancels accepted Start first; deliberate abort probe, followed by goroutine-count observation. |
| Health after disconnect | `processor/graph-query/attack_test.go:444` — `comp.Stop(context.Background())` | Terminal cleanup after health assertion; result discarded. |
| Stop success | `processor/graph-query/component_test.go:412` — `err := comp.Stop(context.Background())` | Explicit API probe; result checked afterward. |
| Stop before Start | `processor/graph-query/component_test.go:421` — `err := comp.Stop(context.Background())` | Deliberate no-action terminal transition, result checked. |
| Accepted Start cancellation | `processor/graph-query/component_test.go:883` — `err := comp.Stop(context.Background())` | Explicit Start cancellation precedes Stop; result checked. |
| Integration lifecycle | `processor/graph-query/component_integration_test.go:70` — `err = graphQuery.Stop(context.Background())` | Explicit Initialize/Start/health/Stop sequence; result checked. |
| Orderly generation cancellation | `processor/graph-query/component_integration_test.go:394` — `require.NoError(t, graphQuery.Stop(context.Background()))` | Phase fence before checking exact generation revocation, empty cache and absence of retry. |

An additional accepted-Start test has no Stop:

- `processor/graph-query/component_test.go:891` — `ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)`
- `processor/graph-query/component_test.go:897` — `err := comp.Start(ctx)`

`TestComponent_RespectsContext_Timeout` accepts either successful Start or a context-related error, defers cancellation,
and returns without synchronous terminal observation.

Distinct request cancellation must remain separate from Start authority:

- `processor/graph-query/component_test.go:743` — `require.NoError(t, comp.Start(context.Background()))`
- `processor/graph-query/component_test.go:747` — `ctx, cancel := context.WithCancel(context.Background())`
- `processor/graph-query/component_test.go:748` — `cancel()`
- `processor/graph-query/component_test.go:752` — `response, err := comp.handlePathSearch(ctx, queryData)`
- `processor/graph-query/component_test.go:773` — `ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)`
- `processor/graph-query/component_test.go:778` — `response, err := comp.handlePathSearch(ctx, queryData)`

B21 and B31 therefore exercise ended *operation* authority while their component Start authority remains independent.
B39 instead currently shares a cancellable context between Start and KV operations, with its Stop defer registered
after the earlier `defer cancel()`.

### Existing owner-specific regression probes

These remain a separate production-contract surface, not ordinary fixture-finalizer calls:

- `processor/graph-query/lifecycle_owner_test.go:68` — `require.NoError(t, c.Stop(t.Context()))`
- `processor/graph-query/lifecycle_owner_test.go:79` — `require.Error(t, c.Stop(nil))`
- `processor/graph-query/lifecycle_owner_test.go:136` — `go func() { stopResult <- c.Stop(stopCtx) }()`
- `processor/graph-query/lifecycle_owner_test.go:172` — `go func() { stopResult <- c.Stop(stopCtx) }()`
- `processor/graph-query/lifecycle_owner_test.go:229` — `require.NoError(t, c.Stop(t.Context()))`
- `processor/graph-query/lifecycle_owner_test.go:240` — `require.ErrorIs(t, c.Stop(expired), context.Canceled)`
- `processor/graph-query/lifecycle_owner_test.go:242` — `require.NoError(t, c.Stop(t.Context()))`

These prove no-action terminal state, nil-context nonmutation, Start/Stop coordination without holding the component
lock, callback and child lifetime through drain, retained failed-Start rollback retry, and no replay of a completed
running Stop error. Explicit retries here are test subjects, not permission for an ordinary finalizer to retry.

Their embedded-server helper is another, uncounted substrate spelling:

- `processor/graph-query/lifecycle_owner_test.go:55` — `t.Cleanup(server.Shutdown)`
- `processor/graph-query/lifecycle_owner_test.go:59` — `t.Cleanup(func() { _ = client.Close(context.Background()) })`

It is outside the five-file debt batch and outside the 39 identities; its unbounded Close is not certified by the
manifest count. The factory in `graph_query_lifecycle_test.go` has one typed caller, the no-action Stop test at
`lifecycle_owner_test.go:66`; this package does not currently invoke the shared standard suite through that factory.

### Native terminal ownership and actual joins

- `processor/graph-query/component.go:505` — `componentCtx, cancel := context.WithCancel(ctx)`
- `processor/graph-query/component.go:516` — `rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, c.cleanupFailedStart)`
- `processor/graph-query/component.go:571` — `c.superviseCommunityGenerations(componentCtx)`
- `processor/graph-query/component.go:579` — `c.superviseSummaryView(componentCtx)`
- `processor/graph-query/component.go:583` — `c.wg.Wait()`
- `processor/graph-query/component.go:584` — `close(done)`

Start owns two supervisor goroutines and a runtime completion channel. The production Component stores private
cancellation and completion state, not a context:

- `processor/graph-query/component.go:181` — `startDone         chan struct{}`
- `processor/graph-query/component.go:182` — `cancel            context.CancelFunc`
- `processor/graph-query/component.go:183` — `runtimeDone       chan struct{}`

The inspected context-valued fields are operation-taking callbacks, not context-returning providers.
A tracked production-source search for `context.Background`, `context.TODO`, and `context.WithoutCancel` within
graph-query returned zero matches.

Stop waits for Start completion under its caller context, rejects concurrent Stop, and distinguishes retained
failed-Start cleanup from running-generation termination. Native ordering is:

- `processor/graph-query/component.go:661` — `if err := sub.Drain(ctx); err != nil {`
- `processor/graph-query/component.go:672` — `c.cancel()`
- `processor/graph-query/component.go:676` — `case <-c.runtimeDone:`
- `processor/graph-query/component.go:681` — `case <-ctx.Done():`
- `processor/graph-query/component.go:686` — `if err := c.llmClient.Close(); err != nil {`
- `processor/graph-query/component.go:693` — `if err := c.answerSynthesizer.Close(); err != nil {`

Responder drain precedes cancellation. Successful drain observes completion of the native delivery goroutine:

- `natsclient/client.go:806` — `if err := s.sub.Drain(); !stderrors.Is(err, nats.ErrConnectionClosed) {`
- `natsclient/client.go:814` — `case <-s.done:`
- `natsclient/client.go:816` — `case <-ctx.Done():`

The supervisor subtree includes contextless joins:

- `processor/graph-query/community_cache.go:73` — `defer watcher.Stop()`
- `processor/graph-query/summary_view.go:184` — `view.Stop()`
- `pkg/graphview/view.go:302` — `v.wg.Wait()`

Graph-query can return its runtime-wait deadline without proving those children finished. Contextless LLM/client
Close calls occur synchronously after that wait and are not interrupted by supplying a finite Stop context.
A nonnil running Stop still commits terminal state and clears handles; it does not establish a later rejoin right.
Failed-Start rollback retains unresolved ownership when rollback fails and is the documented separate retry case.

Mock tests return nil subscription handles:

- `processor/graph-query/component_test.go:89` — `// Return a nil subscription since the mock doesn't actually subscribe`
- `processor/graph-query/component_test.go:90` — `return nil, nil`

Their constructors omit catalog-reader openers. Their successful cleanup therefore does not exercise real responder
drain or attached watch generations. Real integration tests do.

B01/B02 additionally create 100 test-owned workers and receive every result before return. Those workers are not
joined by Component.Stop:

- `processor/graph-query/attack_test.go:256` — `err := <-errCh`
- `processor/graph-query/attack_test.go:286` — `err := <-errCh`

The receives have no independent timeout. No present worker hang was established.

NATS substrate cleanup owns distinct client/container attempts:

- `natsclient/test_client.go:310` — `closeCtx, closeCancel := context.WithTimeout(context.Background(), timeout)`
- `natsclient/test_client.go:311` — `if err := client.Close(closeCtx); err != nil {`
- `natsclient/test_client.go:317` — `terminateCtx, terminateCancel := context.WithTimeout(context.Background(), timeout)`
- `natsclient/test_client.go:318` — `if err := container.Terminate(terminateCtx); err != nil {`

It gives each substrate operation a fresh budget and aggregates failures. Component ownership remains distinct.

## 3. Adjacent claims and governing constraints

- `openspec/specs/test-cleanup-policy/spec.md:100` — `### Requirement: Exact reviewed debt`
- `openspec/specs/test-cleanup-policy/spec.md:141` — `### Requirement: Baseline freshness`
- `openspec/specs/test-cleanup-policy/spec.md:199` — `### Requirement: Evidence limits`
- `openspec/specs/test-cleanup-policy/spec.md:213` — `### Requirement: Lexical ownership of lifecycle test fixtures`
- `openspec/specs/test-cleanup-policy/spec.md:234` — `#### Scenario: Setup assertion before transfer`
- `openspec/specs/test-cleanup-policy/spec.md:240` — `#### Scenario: Caller assertion after transfer`
- `openspec/specs/test-cleanup-policy/spec.md:245` — `#### Scenario: Explicit terminal phase fence`
- `openspec/specs/test-cleanup-policy/spec.md:252` — `#### Scenario: Deadline supply versus completion`

`component-lifecycle` and `runtime-context-ownership` distinguish controlled shutdown with live accepted Start
authority from abort cleanup. They preserve owner-specific failed-Start retries and reject a portable
running-generation second-rejoin promise.

The completed shared-lifecycle, graph-ingest and rule cleanup changes already occupy this test-ownership territory.
The current graph-query specification owns stable responders, generation publication/revocation, optional summary
view teardown and unchanged query results. Cleanup changes cannot replace those observations with mere
deadline-provenance checks.

The active proposal records the coordination boundaries: #1417 owns the parent debt campaign; #1421 remains open;
#1293 owns broader coverage/common-gate concerns. The coordinator reports #1411/#1412 production lifecycle work,
Claude's #1426/#1427 E2E work and #1117/#1425 remain separate. These live issue statuses were not independently
refetched in this inventory; they are coordination constraints, not evidence that any failure is waived.
The #1432 waiver is PR-specific and supplies no #1434 merge authorization.

## 4. Consumer at birth

There is no proposed exported symbol, port, subject, bucket, configuration field or replacement framework primitive
in this inventory. The active proposal expressly limits the batch to test ownership. The present consumers being
measured are B01–B39 and the enumerated helper callers; no future consumer justifies additional public surface.

This category is closed by reading both active artifacts fully and listing the active change files with:

```sh
rg --files openspec/changes/graph-query-test-cleanup
```

Result: only `proposal.md` and `tasks.md`.

## 5. Problem shape and existing instances

The shape is lexical ownership of a test-acquired component across fallible setup, explicit terminal attempts and
substrate teardown, with operation authority distinct from private Start cancellation.

Existing instances:

- `component/lifecycle_test_suite.go:29` — `type lifecycleTestOwner struct {`
- `component/lifecycle_test_suite.go:65` — `o.attempted = true // A returned error or panic does not authorize an implicit retry.`
- `component/lifecycle_test_suite.go:66` — `o.concreteStopErr = o.component.Stop(stopCtx)`
- `component/lifecycle_test_suite.go:67` — `o.stopBoundErr = stopCtx.Err()`
- `component/lifecycle_test_suite.go:87` — `defer o.cancelStart() // Stop completes before accepted Start authority ends.`
- `processor/graph-ingest/test_owner_support_test.go:11` — `type graphIngestTestOwner struct {`
- `processor/graph-ingest/test_owner_support_test.go:36` — `stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)`
- `processor/graph-ingest/test_owner_support_test.go:58` — `func (o *graphIngestTestOwner) provisionalFinish(operationCtx context.Context, t *testing.T) {`
- `processor/rule/test_owner_support_test.go:13` — `type processorTestOwner struct {`
- `processor/rule/test_owner_support_test.go:30` — `func (o *processorTestOwner) stop(operationCtx context.Context) error {`

These are observations, not a selection among designs. The graph-ingest/rule owners use concrete receiver types;
the shared standard suite owns its interface-dispatched portable cases. The suite also distinguishes explicit abort
expectations and concrete result from terminal expiry.

`test/testutil` does not exist. The actual `testutil/` package contains mocks and polling support, not this lexical
owner abstraction:

- `testutil/mock.go:66` — `func (m *MockComponent) Stop(ctx context.Context) error {`
- `testutil/mock.go:70` — `m.StopCalls++`

`test/testinfra/` owns admission analysis, exact debt and classifier fixtures; it is not runtime fixture ownership:

- `test/testinfra/cleanup_guard_test.go:41` — `func TestCleanupRootGuardTypedFixture(t *testing.T) {`
- `test/testinfra/cleanup_guard_test.go:65` — `report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})`

No new reusable pattern is established during inventory, so an establishing-side adoption sweep is not triggered.

### Collision applicability and observed ownership

No durable, communication or production coordination primitive has been proposed. The following records the
existing semantic overlap without choosing a new owner.

| Dimension | Observed ownership |
|---|---|
| Semantic class | Tests own fixture terminal attempts; graph-query owns native responder drain and supervisor completion; NATS support owns substrate termination. |
| Owners | Five fixture files, private owners in component/graph-ingest/rule, graph-query Component, NATS TestClient. |
| Catalogs | Exact cleanup baseline; classifier policy; internal graph-query operation inventory. No fixture-owned durable catalog. |
| Status | Test assertions/results, guard findings, Component Health, generation publication/revocation and summary-view readiness. |
| Lifecycle | Fixture acquisition/Initialize/Start/Stop; production failed-Start rollback; responder drain; supervisor cancellation/join; NATS Close/container Terminate. |
| Ownership | Per-test concrete instances; private cancellation; attempted/transfer state in existing test-owner shapes. No distributed claim or lease for fixture cleanup. |
| Readers | Tests and guard; query handlers read component-owned views. Downstream query consumers do not read fixture ownership state. |
| Writers | Test setup; native Component state transitions; testinfra approval records. |
| Recovery | Failed-Start retry is native and owner-specific; running Stop does not promise rejoin. Fixtures have no restore/replay/expiry protocol. |

## Adopter seam inventory

No outward-facing surface change is proposed. The B ledger and local test helpers are repository-private test code.
The inherited graph-query API remains externally reached, so its relevant seam is recorded rather than silently
declared irrelevant.

For a component composer:

1. **Knowledge currently required:** accepted Start authority owns continuing work; controlled Stop uses separate finite
   authority while Start remains live; abort results can be nonnil; failed-Start retry differs from running Stop.
   This is more than two correctness facts and is already governed by the current lifecycle specifications.
2. **Doing nothing:** failing to call Stop leaves terminal completion unobserved; canceling Start first chooses abort
   behavior. A deadline alone cannot interrupt contextless Close or establish completed joins.
3. **Discovery:** nil/ended Start receives runtime error; sequencing and join limits are principally contract/spec
   knowledge. No compile-time check enforces controlled-stop ordering.
4. **Desired knowledge boundary:** this test-only inventory introduces no additional burden or knob for that composer.
   Whether production should absorb more sequencing knowledge belongs to the separate lifecycle work.

For an author using the current private test helpers:

1. They must know the helpers return unstarted components, install terminal protection themselves, choose appropriate
   Start/operation authority, preserve explicit probes and finish components before NATS testing cleanup.
2. Following the common current examples installs cleanup only after fallible setup and usually discards its result.
3. The manifest and guard expose exact debt, but ordinary calls and omitted cleanup remain separate census questions.
4. The measured gap is fixture ownership and result observation; this inventory selects no replacement API.

No sister repository was inspected because no sister-owned source or externally visible behavior is proposed for
change. An outward-facing change discovered during design would reopen this inventory's applicability boundary.

## Search and evidence record

Commands ran from the declared worktree. Source reads used `cat` for required whole documents and `nl -ba`/`sed -n`
for located source ranges. No test, guard, mutation, temporary artifact write or Git state mutation ran.

Discovery/search commands:

```sh
rg --files openspec/changes/graph-query-test-cleanup
rg --files openspec test/testutil test/testinfra processor/graph-query
git ls-files '*testutil*'
git ls-files openspec/changes
git ls-files internal
git grep -n -E 'graph-query|1417|1421|1433|terminal.cleanup|cleanup liability' -- openspec/changes openspec/specs test
git grep -n -E 'defer |Cleanup\(|Background\(|WithoutCancel\(|TODO\(|\.Stop\(|\.Start\(|\.Close\(|\.Drain\(|Wait\(|WaitFor' -- processor/graph-query '*testutil*.go'
git grep -n -E 'func .*NewTestClient|func .*cleanup|func .*Close|func .*Drain' -- natsclient/test_client.go natsclient/client.go natsclient/subscription.go
git grep -n -E 'func |Stop\(' -- processor/graph-query/lifecycle_owner_test.go processor/graph-query/graph_query_lifecycle_test.go
git grep -n -E 'func .*Close|Close\(\) error' -- processor/graph-query/answer.go graph/llm/openai.go
git grep -n -E 'context\.(Background|TODO|WithoutCancel)|context\.Context|CancelFunc|func\(\).*context.Context' -- processor/graph-query ':!*_test.go'
git grep -n -E 'context\.(Background|TODO|WithoutCancel)' -- processor/graph-query ':!*_test.go'
git grep -n -E 'Stop\(|Cleanup\(|terminal|lifecycle' -- testutil
git grep -n -E 'Requirement: (Lexical|Evidence|Exact|Baseline)|Scenario: (Setup assertion|Caller assertion|Explicit terminal|Deadline supply)' -- openspec/specs/test-cleanup-policy/spec.md
git grep -n -E 'watcher.Stop|func .*Stop|done|wg.Wait' -- processor/graph-query/community_cache.go pkg/graphview/view.go
git log -3 --format='%H %s' -- processor/graph-query/component.go
```

The broad initial historical search was truncated and was not used to claim absence. The `test/testutil` discovery
reported that path missing. The final production-root search returned zero matches.

Typed structural commands:

```sh
gopls workspace_symbol -matcher=fuzzy 'setupTestComponent|setupIntegration|Cleanup|Terminal|Lifecycle'
gopls references processor/graph-query/component_test.go:919:6
gopls references processor/graph-query/component_test.go:911:6
GOFLAGS=-tags=integration gopls references processor/graph-query/component_integration_test.go:19:6
GOFLAGS=-tags=integration gopls references processor/graph-query/component_test.go:919:6
gopls call_hierarchy processor/graph-query/component.go:597:21
gopls references processor/graph-query/graph_query_lifecycle_test.go:11:6
```

The first workspace-symbol attempt failed on sandboxed Go cache access and established no absence.
Subsequent structural queries succeeded with authorized cache access. Call hierarchy included interface callers in
shared suites and service orchestration; those are not additional graph-query fixture debt identities.

Read-only Python enumeration independently:

1. loaded `cleanup_baseline.json`, counted arrays and selected every record mentioning `processor/graph-query/`;
2. scanned every package `*test.go`, retaining enclosing declarations and all Start/Stop/Cleanup registrations;
3. matched baseline identities to exact source calls and manifest lines;
4. printed all three named helpers' direct caller pins;
5. computed the following source hashes.

| Source | SHA-256 |
|---|---|
| attack_test.go | `469bc121cf909ba6ff73188f27f267e83e99a8738918de35c303ab3a46eafbfe` |
| batch_passthrough_integration_test.go | `5c1e08e289c549de4ed9fdbce6e0f5072f1266da4bf706d9a3d979cacf5e00ca` |
| component_integration_test.go | `cf1dfa39868e276e28f448160738455d9884ad08b0085f9fb2fc92ef83029146` |
| component_test.go | `9c50628590c45705312760cfee7a64197eebdf3fe74590af3a4518bce066655c` |
| summary_bucket_late_attach_integration_test.go | `151ed7fa46d175a98a58a054e3190d139d427b80ae4b235185637a42140869e7` |
| test/testinfra/cleanup_baseline.json | `1ffdf6ad20159fa8d25371c2b3820afba938323e66ecb488400c44002be34b82` |

The five abbreviated test filenames above are under `processor/graph-query/`. The worktree remained clean.

## Explicit omissions and open evidence questions

1. Independent review must reconcile whether the eight ordinary Stop sites and accepted-Start/no-Stop timeout case
   belong in this package repair's implementation scope. They are inventoried here without silently converting
   deliberate API/abort probes into ordinary cleanup.
2. No execution established cleanup success, setup-failure behavior, race freedom, guard freshness after edits, or
   native completion. Finite context supply must remain distinguishable from those proofs.
3. `lifecycle_owner_test.go`'s embedded NATS Close, contextless view/worker tests and shared-helper siblings are adjacent
   ownership surfaces, not part of the 39 baseline identities. Their omission from this debt ledger is explicit.
4. The package contains no existing provisional running-component helper to migrate. Any design that changes an
   unstarted constructor must account for the complete caller set above, including metadata and pure-handler cases.
5. The production contract contains contextless joins and Close operations. This batch has no evidence or authority
   to replace their native semantics or claim timeout-based interruption.
6. No new public surface, durable state, communication path, payload, query access or orchestration behavior has been
   proposed. The corresponding design skills do not trigger during this inventory-only phase.
7. The caller must materialize this exact artifact and record its content hash before independent inventory review.
   Binding scope decisions and design acceptance remain with the owner.

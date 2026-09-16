# R8 loop-authority admission inventory — 2026-09-16

base: c347eff487f50b93bc338d764f43ef5b5ea5e133

Status: inventory only; independent INVENTORY PASS on 2026-09-16, followed by the mechanical corrections below.

Worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`

Preserved candidate: R7 candidate-01 nine-file manifest
`539fb851e53c2f07b7c176db159eda6d8f239d868088478a65c337da5f6e636e`.
This inventory does not rerule, reimplement, or claim verification of that candidate.

## Scope and intake

This measures acquisition and admission of the existing loop KV authority: bucket identity, creation/race behavior,
observed History/TTL/MaxBytes, approval lifetime, startup ordering, related owners, and existing proof.

It excludes AGENT stream horizon arithmetic, first-party publishing, #1311 proposal-input settlement, retained-verdict
implementation, and broad lifecycle redesign. Parent #1156 and held #1312 remain untouched.

The architect contract, project guidance, full current proposal/design/tasks, current loop capability specification
and active loop delta were read or reused from the same unchanged session intake. Relevant R7/R8 inventories were
used as locators, not as proof that this sub-boundary already passed inventory review. ADR-028/039 and ADR-081 were
read; ADR-081 constrains the separate dispatch read-side view, not bucket provisioning.

The earlier R8 checkpoint inventory explicitly remained incomplete. No new claim of complete R8 admission follows
from this narrower inventory.

## 1. Existing contract and claimed gap

The accepted change already distinguishes loop KV authority acquisition from stream replay admission.

- `openspec/changes/agentic-loop-restart-safety/design.md:922` — `AGENT replay admission and loop-state authority acquisition are separate gates. Agentic-loop resolves its bucket from`
- `openspec/changes/agentic-loop-restart-safety/design.md:923` — `the admitted `loops` KV-write port, then calls internal`
- `openspec/changes/agentic-loop-restart-safety/design.md:924` — `The helper calls `KeyValue` first and calls`
- `openspec/changes/agentic-loop-restart-safety/design.md:925` — `only when `errors.Is(err, jetstream.ErrBucketNotFound)`. Permission, timeout, transport, and every`
- `openspec/changes/agentic-loop-restart-safety/design.md:926` — `other lookup error return with zero create mutation. A typed `jetstream.ErrBucketExists` create race permits exactly`
- `openspec/changes/agentic-loop-restart-safety/design.md:927` — `one KeyValue retry before observation.`
- `openspec/changes/agentic-loop-restart-safety/design.md:929` — `Creation declares History 10, TTL 24h, and non-binding MaxBytes. After get, create, or race-get, the helper observes`
- `openspec/changes/agentic-loop-restart-safety/design.md:930` — `actual status/backing stream and requires exact History 10, exact TTL 24h, and MaxBytes `<=0`. It publishes the handle`
- `openspec/changes/agentic-loop-restart-safety/design.md:931` — `and starts task/response/tool-result/signal/approval/verdict consumers and the approval sweeper only after that`
- `openspec/changes/agentic-loop-restart-safety/design.md:932` — `observation succeeds. Retained or race-winning drift is refused without update or reconciliation because earlier`
- `openspec/changes/agentic-loop-restart-safety/design.md:933` — `eviction may already have destroyed authority. Approval timeout is validated against this observed bucket only.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:741` — `When approval gating is enabled, timeout SHALL be finite, nonzero, and within observed AGENT_LOOPS TTL after the`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:742` — `framework safety margin. The default SHALL be 12 hours. Zero, empty, indefinite, and over-retention values SHALL fail`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:743` — `before dependent loop work.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:318` — `Loop authority uses typed not-found/create-exists handling and observes History 10, TTL 24h, nonbinding MaxBytes`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:319` — `and approval lifetime before consumers/sweeper; no reconciliation of retained drift or additional public gate.`

The active loop delta additionally contains distinct native-race scenarios at lines 852, 858, 864 and 869: fresh
concurrent acquisition, retained/race-winner drift, non-absence lookup error, and exactly one post-Exists get.

Measured gap: `gopls workspace_symbol -matcher=fuzzy AcquireOwner`, tracked Go string search for
`loopbucket|AcquireOwner`, and a working-tree Go search under `processor/agentic-loop` all returned no implementation.
The absence claim includes untracked Go files in that component.

## 2. Current loop owner

### Acquisition and publication of the handle

- `processor/agentic-loop/component.go:797` — `func (c *Component) initializeKVBuckets(ctx context.Context) error {`
- `processor/agentic-loop/component.go:798` — `js, err := c.natsClient.JetStream()`
- `processor/agentic-loop/component.go:804` — `loopsBucket, err := js.KeyValue(ctx, c.config.LoopsBucket)`
- `processor/agentic-loop/component.go:805` — `if err != nil {`
- `processor/agentic-loop/component.go:807` — `loopsBucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{`
- `processor/agentic-loop/component.go:809` — `History: 10,`
- `processor/agentic-loop/component.go:810` — `TTL:     24 * time.Hour,`
- `processor/agentic-loop/component.go:816` — `c.loopsBucket = loopsBucket`

After successful JetStream acquisition, **every** bucket lookup error enters Create. The loop path does not distinguish
typed absence, does not handle typed create-exists with a get, and does not call Status or inspect backing-stream
policy before assigning the handle. History and TTL are declarations on fresh creation, not observations of an
existing bucket. MaxBytes is omitted from this declaration.

`gopls references` on `initializeKVBuckets` identifies its normal production caller as
`initializeKVBucketsForStart`; the other production handle references are reads/writes, not alternative acquisition
or policy validation.

### Identity and public configuration

- `processor/agentic-loop/component.go:211` — `func resolveConfig(rawConfig json.RawMessage) (Config, []component.Port, []component.Port, error) {`
- `processor/agentic-loop/component.go:216` — `config := DefaultConfig()`
- `processor/agentic-loop/component.go:228` — `if err := config.Validate(); err != nil {`
- `processor/agentic-loop/component.go:235` — `merged, err = component.MergePortConfig(merged, *config.Ports)`
- `processor/agentic-loop/component.go:262` — `port, err := definition.Resolve(component.DirectionOutput)`
- `processor/agentic-loop/component.go:266` — `if definition.Name == "trajectories" {`
- `processor/agentic-loop/component.go:281` — `func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {`
- `processor/agentic-loop/config.go:212` — `if strings.TrimSpace(c.LoopsBucket) == "" {`
- `processor/agentic-loop/config.go:438` — `Name: "loops", Config: component.KVWritePort{Bucket: "AGENT_LOOPS"}, Description: "Loop state storage",`
- `schemas/agentic-loop.v1.json:87` — `"loops_bucket": {`

Two existing public spellings describe the storage identity: exported `Config.LoopsBucket` / JSON `loops_bucket`
and the `loops` KV-write output. Both default to AGENT_LOOPS. The current initializer reads only the raw config field.
Output-port resolution has a trajectory-specific validator but no corresponding loop-output interpretation.

`gopls references Config.LoopsBucket` locates production reads only in config validation and initialization, plus its
default assignment. Consequently, declaring a different `loops` port does not presently select the initializer’s
bucket. Conversely, the existing custom-bucket constructor test changes `LoopsBucket`; it does not prove port-derived
runtime selection.

Tracked spellings also occur in nine shipped/example/test flow files:

1. `configs/agentic.json`
2. `configs/examples/research-graph-pipeline.json`
3. `configs/research-graph-e2e.json`
4. `configs/flows/{crud-tools-test,deep-research-test,deep-research,lesson-example,ops-agent-test,ops-agent}.json`

These carry raw `loops_bucket` and a loops-port bucket spelling. The generated agentic-loop schema exposes both.
This is an existing identity split, not a new configuration surface.

### Approval lifetime

- `processor/agentic-loop/config.go:219` — `// Validate approval timeout (empty is allowed — means wait forever)`
- `processor/agentic-loop/config.go:220` — `if strings.TrimSpace(c.ApprovalTimeoutStr) != "" {`
- `processor/agentic-loop/config.go:225` — `if d < 0 {`
- `processor/agentic-loop/config.go:247` — `func (c Config) ApprovalTimeout() time.Duration {`
- `processor/agentic-loop/config.go:248` — `if c.ApprovalTimeoutStr == "" {`
- `processor/agentic-loop/config.go:249` — `return 0`
- `schemas/agentic-loop.v1.json:10` — `"description": "Auto-reject pending approvals after this duration (e.g. 5m or 1h). Empty means wait indefinitely",`
- `processor/agentic-loop/handlers.go:2465` — `if err := entity.BeginAwaitingApproval(toolResult.CallID, toolName, args, toolResult.Error, h.config.ApprovalTimeout(), toolResult.TraceID); err != nil {`

DefaultConfig does not supply ApprovalTimeoutStr. The current runtime therefore defaults to indefinite approval,
accepts zero and arbitrarily long parsed nonnegative durations, and has no relationship to observed bucket TTL.
The generated schema advertises the same indefinite behavior. `configs/agentic.json:315` explicitly supplies `5m`.

`gopls references ApprovalTimeout` finds the production consumer at `gateForApproval`, plus a test expecting zero;
it does not find an admission-time validator.

### Startup, recovery discovery, health and context ownership

- `processor/agentic-loop/component.go:482` — `if ctx == nil {`
- `processor/agentic-loop/component.go:490` — `if c.lifecycleUsed {`
- `processor/agentic-loop/component.go:495` — `runCtx, cancel := context.WithCancel(ctx)`
- `processor/agentic-loop/component.go:505` — `rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, c.cleanup)`
- `processor/agentic-loop/component.go:527` — `if err := c.initializeKVBucketsForStart(runCtx); err != nil {`
- `processor/agentic-loop/component.go:530` — `if err := c.restoreApprovalDeadlines(runCtx); err != nil {`
- `processor/agentic-loop/component.go:535` — `if err := c.setupSubscriptions(runCtx, runCtx); err != nil {`
- `processor/agentic-loop/component.go:571` — `c.started = true`
- `processor/agentic-loop/component.go:584` — `sweepCtx, cancelSweep := context.WithCancel(runCtx)`
- `processor/agentic-loop/component.go:592` — `c.runApprovalTimeoutSweeper(sweepCtx)`
- `processor/agentic-loop/component.go:429` — `healthy := c.started`
- `processor/agentic-loop/approval_sweeper.go:28` — `watcher, err := c.loopsBucket.WatchAll(ctx, jetstream.MetaOnly(), jetstream.IgnoreDeletes())`
- `processor/agentic-loop/approval_sweeper.go:57` — `if err := watcher.Stop(); err != nil {`
- `processor/agentic-loop/approval_sweeper.go:69` — `entity, found, err := c.readLoopEntity(ctx, key)`
- `processor/agentic-loop/approval_sweeper.go:74` — `entity.PendingApproval == nil || entity.PendingApproval.Timeout <= 0 {`
- `processor/agentic-loop/approval_sweeper.go:94` — `const approvalSweepInterval = 5 * time.Second`

The existing ordering is initializer → finite metadata snapshot/exact current-record reads → input consumers →
query subscriptions → started state → sweeper. Initializer errors already return before those dependents, through
existing failed-start rollback. No separate bucket-readiness record or policy-health state was found in this owner.

The snapshot is deadline discovery, not bucket-policy observation. It stops its watcher, then reads exact current
records, and skips untimed/nonpending/nonawaiting values. It does not establish retention admissibility.

The inspected Component retains private cancel functions and join channels, not a Context field. Start passes its
derived operation context into these startup calls. Existing single-use lifecycle protection is per Component;
no bucket lease or exclusive cross-process provisioning claim exists in this acquisition path.

## 3. Same-bucket owners and nearby problem shapes

### Explicit co-provisioners

All five research stages plus research-tool registration can create/open the selected loop bucket through
`Client.CreateKeyValueBucket`. Each declares History 10 / TTL 24h, omits MaxBytes, and installs its adapter after
the generic wrapper succeeds.

- `frameworkcapabilities/graphresearch/register_tool.go:49` — `return natsClient.CreateKeyValueBucket(ctx, loopResultBucketConfig(bucketName))`
- `frameworkcapabilities/graphresearch/register_tool.go:72` — `return jetstream.KeyValueConfig{Bucket: bucket, History: 10, TTL: 24 * time.Hour}`
- `processor/research-graph-assess/component.go:234` — `bucket, err := c.deps.NATSClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{`
- `processor/research-graph-classify/component.go:298` — `bucket, err := c.deps.NATSClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{`
- `processor/research-graph-execute/component.go:274` — `bucket, err := c.deps.NATSClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{`
- `processor/research-graph-route/component.go:247` — `bucket, err := c.deps.NATSClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{`
- `processor/research-graph-synthesize/component.go:219` — `bucket, err := c.deps.NATSClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{`
- `frameworkcapabilities/graphresearch/register.go:333` — `func validateLoopsBuckets(configs map[string][]json.RawMessage) error {`
- `frameworkcapabilities/graphresearch/register.go:334` — `names := append([]string{"agentic-loop", "agentic-tools"}, stageFactories...)`
- `frameworkcapabilities/graphresearch/register.go:346` — `bucket := bucketConfig.LoopsBucket`

The research composition validator compares the raw `loops_bucket` fields of loop, tools and stages, defaulting
empty fields to AGENT_LOOPS. It does not compare their admitted port identities. This is relevant to a port-derived
loop bucket even though changing the research composition is outside this inventory’s authority.

### Generic provisioning and catalog boundary

- `natsclient/client.go:1301` — `bucket, err := js.KeyValue(ctx, cfg.Bucket)`
- `natsclient/client.go:1310` — `bucket, err = js.CreateKeyValue(ctx, cfg)`
- `natsclient/client.go:1313` — `if isAlreadyExistsError(err) {`
- `natsclient/client.go:1318` — `bucket, err = js.KeyValue(ctx, cfg.Bucket)`
- `natsclient/client.go:1693` — `errStr := err.Error()`
- `graph/kvcatalog.go:9` — `// Application/product buckets (AGENT_LOOPS, personas, governance`
- `graph/kvcatalog.go:11` — `// outside it by rule and keep plain CreateKeyValueBucket.`
- `processor/rule/kv_writer.go:85` — `func (w *natsKVWriter) acquireBucket(ctx context.Context, bucketName string) (jetstream.KeyValue, error) {`
- `processor/rule/kv_writer.go:86` — `if _, catalogued := gtypes.SpecFor(bucketName); catalogued {`
- `processor/rule/kv_writer.go:101` — `bucket, err := w.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{`
- `processor/rule/kv_writer.go:104` — `History:     5,`

The generic wrapper also creates after any initial lookup error. Its create-race classification uses message
substrings, not `errors.Is(ErrBucketExists)`, and it returns an existing handle without observed-policy validation.

Generic `update_kv` accepts substituted non-catalog bucket names and can therefore provision AGENT_LOOPS with History
5 / omitted TTL if configured to that name. This is a reachable configurable collision, not proof that a shipped
rule currently does so. The literal configuration search found research documentation/continuation references, not
a shipped literal AGENT_LOOPS update_kv declaration.

### Closest existing problem-shape implementations

The shape is **get/create/race acquisition followed by observed-policy admission before exposing a handle**.

- `processor/agentic-loop/component.go:821` — `if errors.Is(trajectoryErr, jetstream.ErrBucketNotFound) {`
- `processor/agentic-loop/component.go:876` — `status, err := bucket.Status(ctx)`
- `processor/agentic-loop/component.go:880` — `return validateTrajectoryFactBucketContract(status.History(), status.TTL())`
- `natsclient/kv.go:133` — `func BucketRetention(ctx context.Context, bucket jetstream.KeyValue) (maxAge time.Duration, maxBytes int64, err error) {`
- `natsclient/kv.go:134` — `status, err := bucket.Status(ctx)`
- `natsclient/kv.go:138` — `bucketStatus, ok := status.(*jetstream.KeyValueBucketStatus)`
- `natsclient/kv.go:146` — `return info.Config.MaxAge, info.Config.MaxBytes, nil`
- `natsclient/kvspec.go:293` — `case RetentionNoLifecycleStrict:`
- `natsclient/kvspec.go:297` — `if rerr := c.NewKVStore(bucket).AssertNoLifecycleRetention(ctx, spec.Name); rerr != nil {`
- `natsclient/kvspec.go:301` — `case RetentionBoundedTTL:`
- `natsclient/kvspec.go:302` — `if rerr := reconcileBoundedTTL(ctx, js, spec.Name, spec.Retention.TTL, logger); rerr != nil {`
- `natsclient/kvspec.go:321` — `if err := reconcileHistory(ctx, js, spec.Name, spec.History, logger); err != nil {`

The same Component’s trajectory path already uses typed lookup absence and Status validation before installing an
audit handle. Its failure policy is deliberately nonblocking audit degradation; it is not loop-authority admission.

`BucketRetention` already observes backing MaxAge/MaxBytes and rejects Status errors, unexpected status type, and
nil backing info. It does not enforce History 10 / TTL 24h or perform acquisition.

The framework acquisition mechanism already has observe/refuse and reconcile shapes, but its bounded-TTL arm
reconciles TTL and its final step reconciles History. AGENT_LOOPS is outside that catalog. These mechanisms are not
evidence that the accepted no-reconciliation loop gate currently exists.

No new reusable pattern or adoption sweep is proposed by this inventory.

## 4. Same-class collision table

| Dimension | Measured evidence |
|---|---|
| Semantic class | Durable loop/current approval authority with bounded KV retention; active delta 739–873. This differs from immutable trajectory audit and dispatch read projections. |
| Owners | Loop initializer; six explicit research co-provisioners above; generic rule writer can target the non-catalog name. |
| Catalogs | Raw config, resolved loops port, generated schema, shipped flows and research common-bucket validator duplicate identity. Graph catalog explicitly excludes AGENT_LOOPS. |
| Status | Loop health derives from started/delivery/audit state, not bucket policy. Dispatch activity has separate view readiness; it is not admission proof. |
| Lifecycle | Start performs acquisition before snapshot/consumers/sweeper. Current acquired policy is not observed. Creation literals carry TTL24h. Snapshot is one-shot; sweeper cadence is5s. |
| Ownership | Per-Component single-use lifecycle; native create race is possible across loop/research processes. No exclusive AGENT_LOOPS lease or catalog owner-only guard in this path. |
| Readers | Loop exact-current reads and approval snapshot; dispatch activity/terminal routing; tools lazy read_loop_result; research adapters; native/E2E fixtures. These are readers of values or views, not retention admission. |
| Writers | Loop conditional/current/terminal writes; six research adapters; configurable rule writer; test/E2E seeds. This inventory measures their acquisition posture, not every value-level mutation. |
| Recovery | Loop deadline snapshot stops then performs exact reads; typed key absence differs from bucket absence. Current retained bucket policy is adopted without reconciliation or refusal. Separate trajectory refusal and framework reconciliation are not loop recovery proof. |

Additional reader pins:

- `processor/agentic-dispatch/config.go:76` — `bucket, ok := facts.KVReadBucket()`
- `processor/agentic-dispatch/http_activity.go:209` — `kv, err := c.natsClient.GetKeyValueBucket(ctx, bucket)`
- `processor/agentic-tools/executors/lazy_loops_kv.go:34` — `bucket, err := l.client.GetKeyValueBucket(ctx, l.bucket)`
- `processor/agentic-loop/settlement_recovery.go:246` — `entry, err := c.loopsBucket.Get(ctx, loopID)`
- `processor/agentic-loop/settlement_recovery.go:247` — `if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {`

These readers do not provision the bucket. Dispatch observes its read-port binding; the lazy tool adapter opens
must-exist at operation time. Neither substitutes for the loop owner’s accepted acquisition obligation.

## 5. Existing tests and proof limits

- `processor/agentic-loop/lifecycle_causal_test.go:88` — `func TestLifecycleCausalStartStopRollbackRetention(t *testing.T) {`
- `processor/agentic-loop/lifecycle_causal_test.go:94` — `c.initializeKVBucketsInput = func(context.Context) error {`
- `processor/agentic-loop/lifecycle_causal_test.go:95` — `c.loopsBucket = emptyApprovalDeadlineBucket()`
- `processor/agentic-loop/approval_timeout_recovery_test.go:61` — `func TestApprovalDeadlineSnapshotRestoresOnlyCurrentTimedPending(t *testing.T) {`
- `processor/agentic-loop/approval_timeout_recovery_test.go:107` — `func TestApprovalDeadlineSnapshotPreservesDeadlineWhenNewApprovalsAreUntimed(t *testing.T) {`
- `processor/agentic-loop/approval_timeout_recovery_test.go:109` — `require.Zero(t, f.c.config.ApprovalTimeout())`
- `processor/agentic-loop/approval_timeout_recovery_test.go:161` — `func TestApprovalDeadlineSnapshotFailureIsNotEmptySuccess(t *testing.T) {`
- `processor/agentic-loop/approval_timeout_recovery_test.go:217` — `func TestApprovalTimeoutSweepLeavesApplicationToNativeOwner(t *testing.T) {`
- `processor/agentic-loop/loop_integration_test.go:32` — `func TestTrajectoryFactBucketIsImmutableHistoryWithoutTTL(t *testing.T) {`
- `processor/agentic-loop/loop_integration_test.go:57` — `func TestExistingIncompatibleTrajectoryBucketDisablesAuditAndDegradesHealth(t *testing.T) {`
- `processor/agentic-loop/loop_integration_test.go:120` — `natsclient.WithKVBuckets("AGENT_LOOPS"),`
- `processor/agentic-loop/loop_integration_test.go:61` — `natsclient.WithKVBuckets("AGENT_LOOPS"),`
- `processor/agentic-loop/loop_integration_test.go:715` — `natsclient.WithKV(), natsclient.WithKVBuckets("AGENT_LOOPS"))`
- `natsclient/test_client.go:873` — `func (tc *TestClient) setupKVBuckets(ctx context.Context, buckets []string) error {`
- `natsclient/test_client.go:877` — `cfg := jetstream.KeyValueConfig{`
- `natsclient/test_client.go:878` — `Bucket: fullName,`
- `natsclient/test_client.go:881` — `_, err := tc.Client.CreateKeyValueBucket(ctx, cfg)`
- `processor/agentic-loop/approval_integration_test.go:333` — `func TestIntegration_ApprovalTimeoutSweeper_PublishesWireResponse(t *testing.T) {`
- `processor/agentic-loop/approval_replacement_integration_test.go:839` — `require.LessOrEqual(t, info.Config.MaxBytes, int64(0))`
- `natsclient/kvspec_integration_test.go:153` — `func TestIntegration_EnsureFrameworkBucket_BoundedTTLConvergesToDeclared(t *testing.T) {`
- `natsclient/kvspec_integration_test.go:200` — `func TestIntegration_OpenFrameworkBucket_AbsentIsNotReadyAndNeverCreates(t *testing.T) {`
- `natsclient/kv_retention_integration_test.go:18` — `func TestIntegration_BucketRetention_D1Guardrail(t *testing.T) {`

What these establish by inspection:

1. Lifecycle tests exercise causal startup/rollback after replacing actual KV initialization with a fake successful
  initializer. They do not prove observed-policy refusal before dependency allocation.
2. Deadline tests cover finite snapshot discovery, exact reads, snapshot termination/cancellation/errors and preservation
  of retained deadlines. One deliberately expects new approvals to remain untimed under current default config.
3. Existing native History/TTL assertions concern AGENT_TRAJECTORIES, not AGENT_LOOPS.
4. The approval replacement MaxBytes assertion concerns the AGENT stream, not the KV backing stream.
5. The loop native TestMain precreates AGENT_LOOPS with a Bucket-only config. It does not request History10/TTL24h.
  Existing native success against this fixture is therefore not positive proof of the accepted loop authority policy.
  The same Bucket-only fixture also precedes real startup in the incompatible-trajectory test at line 61 and
  trajectory-capture test at line 715; correcting TestMain alone would leave these same-family fixtures behind.
6. Generic substrate tests cover observed retention and different acquire/reconcile contracts; they are adjacent proof,
  not native loop-start conformance.

No focused behavioral or native proof was located for the accepted loop authority matrix: fresh create, existing
matching policy, typed non-absence lookup failure with zero create, typed Exists with one get, observed
History/TTL/MaxBytes drift with no mutation/handle/dependent work, approval default12h, or approval lifetime refusal
against actual KV policy. No new test result is claimed.

## 6. Adopter seam inventory

Specific adopter: a developer outside this repository assembling agentic-loop and tool/research components through
their existing constructors and configuration, without knowing the initializer implementation.

| Question | Current measured answer |
|---|---|
| What must they know? | Raw `loops_bucket` controls current loop acquisition despite a separately declared loops port; research composition compares raw names; preexisting bucket policy is adopted unchecked; omitted approval timeout means indefinite despite fresh bucket TTL24h. |
| What happens if they do nothing? | Names agree on default AGENT_LOOPS. First creator determines actual retained policy. Loop starts after a successful open even if policy differs from creation literals. New pending approvals have no deadline under DefaultConfig. |
| Where do they find out? | Empty raw bucket and malformed/negative timeout can fail construction. NATS acquisition failures fail Start. Schema explicitly advertises indefinite timeout. Retention drift, MaxBytes and approval-versus-retention mismatch currently have no dedicated boot refusal. |
| What should they have to know? | The already accepted change places observed bucket policy and timeout checking inside loop startup, with no additional public gate. The measured gap is that callers presently must predict or externally coordinate facts already observable by the framework. |

There is no new exported symbol, bucket, subject, setter or public gate in this inventory. The proposed internal
AcquireOwner spelling already has its named consumer in the accepted design: agentic-loop startup.

Sister repositories were not reswept. Their deployed bucket policies, configurations and current adoption status are
unproven; no cross-repository migration or write is authorized here.

## 7. Open evidence questions, not design choices

1. The selected current loop spec requires a framework safety margin but supplies no numeric margin or arithmetic.
   The scoped source/design/spec search found no loop approval-margin implementation or value. This inventory does
   not select one.
2. The admitted-port requirement and existing raw-name research compatibility check are both measured. Their current
   mismatch is not evidence that the research validator or other creators may be silently changed.
3. The native shared fixture lacks the admitted declaration. Existing successful tests cannot be relabeled as
   admission proof.
4. No evidence here covers live policy edits after admission; the accepted section describes startup observation.
5. Whole R8 and #1311 remain open. This inventory neither closes them nor changes their dependence on separate source
   settlement, stream admission or producer proof.

## 8. Baseline hashes

```text
4b5bb15f45d6bcf6df18ca90ad8f871d554fae3373d99263aa007f4cfe047af2  processor/agentic-loop/component.go
3fd0b3e0802025803f01c9c144e7b8fa2cf3809417678e9d75e4a1b7f3ba5604  processor/agentic-loop/config.go
39388a8ed0907d42a66a3af7fc685499085d5da921323c62bd68eb44133ef293  processor/agentic-loop/approval_sweeper.go
55b43adc2b93aa4f6f521e1ed965496d4d48d943d4263f963e90f99eac6d70e8  processor/agentic-loop/approval_timeout_recovery_test.go
4bd2ff72c429124a9923273cdc783505656746cf7c0ce44dc242bb784d3012fd  processor/agentic-loop/loop_integration_test.go
a2402ef66d6fd974f3f1bf208e7f16586d0454ad8f612dc98f21735b135ff2ef  processor/agentic-loop/lifecycle_causal_test.go
9f07a68b0ad8785fc68d025ab9c35335573bd996e8eb1361d48ca957960ac776  natsclient/client.go
9b9b4c40aa6f65b66bdcb68a7db881e0fdfb4bb8d46f8b2af4d42d86d1696725  natsclient/kv.go
70d12afe8c2f650eb141003b2d43dc61666f0bb683bb1d21b4f5c422d142cad5  natsclient/kvspec.go
5989421bccc4c22d079afaf2a8f3baad3c227aad0894134270cbae343438ca7a  natsclient/test_client.go
1d11cd6efe08b3af6bb7b7a7315e95b2b76164a31c8f709c4a63b1a986ad05c1  graph/kvcatalog.go
ca37d3b1e429e3898b3e93281c87aa0b8100f41b456167db618b3effcba4e75b  frameworkcapabilities/graphresearch/register.go
f2e970635bc2933d119c6b65763a1685c61b9fb28c56385836bfe5c15d08fdd1  frameworkcapabilities/graphresearch/register_tool.go
10583e65bb4694c734f8f86fdb852ed8d7343b8790d64a03f5e9c021cf2150f5  processor/research-graph-assess/component.go
0847bd4d5f34bf17a86a0cf4551ec4d77b93a9d4416c65f2821681ba4a895070  processor/research-graph-classify/component.go
9389cd701c8059ea9a3b9df5005b2d5dc5f1bcb60a1d92b5dae0aa702f98bf38  processor/research-graph-execute/component.go
b6694f028ac33e9761aa9f089eec800ef5ba1eb81342e09bfe3bcfb927ec1d7a  processor/research-graph-route/component.go
dce4539eee821fa3294f1051a46f45b08ac05259a82e4c0dd40ae0295afe99dd  processor/research-graph-synthesize/component.go
c47d4dbfa0de90706ecc0b2e222dd24e1872aa205922b6c83b337d40e31c9f73  processor/rule/kv_writer.go
268835456b05e06b08b7b3f237aa2e95dbb73cd85ef082441833593f717d778f  processor/agentic-dispatch/config.go
58076a81e8b687483ae4eb1ba2daa30c4a7845d9d3d5d5cdd3f049ba88d6c4cb  processor/agentic-dispatch/http_activity.go
d824b51578c983d784cebea1a174e601f0435587655b571465c4c2c25885922a  processor/agentic-tools/executors/lazy_loops_kv.go
a53e6d91771c36b3c60c6ad1add2d79ce7e72bbdd6ec97874733f03c1e989037  schemas/agentic-loop.v1.json
10813af6caddf9444f61194daa38bafb7e4855dbf9813a85fa44c67c7edfb663  openspec/specs/agentic-loop/spec.md
f2d841a10484dafd896649f0d41a92daefff61b0928f59e9c377fd0f29f6d614  openspec/changes/agentic-loop-restart-safety/proposal.md
a7f77c98012e49d0f452b6035a0e6e3275de8327b635381e74ce68d6aea0c205  openspec/changes/agentic-loop-restart-safety/design.md
6683075f19d188f0876826b3e1468c369e61c1c82b8a0cef18a520d4fa252582  openspec/changes/agentic-loop-restart-safety/tasks.md
d2524d8f047e9e4f11827101ea737f4372c0169e3b7cba3d483c4148c380c0b9  openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md
```

## Searches

Commands ran in the claim worktree unless noted. These searches enumerated; they did not execute tests.

Structural searches used this environment prefix:

```sh
env GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache \
GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off \
GOFLAGS=-mod=readonly gopls
```

Invocations:

```text
workspace_symbol -matcher=fuzzy initializeKVBuckets
workspace_symbol -matcher=fuzzy AcquireOwner
workspace_symbol -matcher=fuzzy ApprovalTimeout
references processor/agentic-loop/component.go:797:21
references processor/agentic-loop/config.go:54:2
references processor/agentic-loop/config.go:247:17
workspace_symbol -matcher=fuzzy CreateKeyValueBucket
workspace_symbol -matcher=fuzzy loopsBucket
workspace_symbol -matcher=fuzzy setupSubscriptions
workspace_symbol -matcher=fuzzy Health
references processor/agentic-loop/component.go:62:2
workspace_symbol -matcher=fuzzy isAlreadyExistsError
workspace_symbol -matcher=fuzzy WithKVBuckets
workspace_symbol -matcher=fuzzy setupTestKV
references natsclient/client.go:1284:18
```

Default gopls configuration does not enumerate integration-tagged references. Native test files were therefore
searched/read separately. Broad symbol output was truncated; only returned locations subsequently read were used.
The final CreateKeyValueBucket references result was filtered to production paths before display.

Literal/path searches:

```sh
rg --files processor/agentic-loop | rg 'bucket|retention|admission|start'
git grep -n -E 'AGENT_LOOPS|loopbucket|AcquireOwner|History.*10|24.*time.Hour|approval.*retention|retention.*approval' -- processor/agentic-loop natsclient config service openspec/changes/agentic-loop-restart-safety/inventory.md openspec/changes/agentic-loop-restart-safety/inventory-rebaseline-2026-09-03-post-1251.md openspec/specs/agentic-loop/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md
git grep -n -E 'AGENT_LOOPS|loops_bucket|loopbucket|AcquireOwner' -- '*.go' ':!**/*_test.go'
git grep -n -E 'ErrBucketNotFound|ErrBucketExists|CreateKeyValue|KeyValue\(|\.Status\(|MaxBytes|History:|TTL:' -- frameworkcapabilities/graphresearch/register_tool.go 'processor/research-graph-*/component.go' processor/agentic-dispatch/http_activity.go natsclient/kv.go natsclient/kv_retention.go natsclient/kvspec.go ':!**/*_test.go'
git grep -n -E 'initializeKVBuckets|LoopsBucket|loops_bucket|approval_timeout|History|MaxBytes|TTL|ErrBucket' -- processor/agentic-loop/*test.go | head -150
rg --files natsclient internal service | rg '(bucket|retention|kv)'
git grep -n -E 'AGENT_LOOPS|loops_bucket|"name": "loops"|approval_timeout' -- configs schemas specs ':!**/*test*' | head -160
rg -n 'loopbucket|AcquireOwner|History 10|TTL 24|approval lifetime|LoopsBucket|loops_bucket' openspec/changes/agentic-loop-restart-safety/inventory-r8-admission-checkpoint-2026-09-15.md openspec/changes/agentic-loop-restart-safety/inventory-r7-retained-verdict-refresh-2026-09-15.md
rg --files processor/agentic-loop | rg '(lifecycle_causal|approval_timeout|config.*test|admission|retention|bucket|loop_integration)'
rg -n 'loopbucket|AcquireOwner|ErrBucketNotFound|ErrBucketExists|\.Status\(|MaxBytes|History:|TTL:' processor/agentic-loop/{*test.go,*.go} | rg -v 'RecentHistory|CompactedHistory|ToolResultMaxBytes|ToolResultManifest|MaxHistory|MonitorMaxBytes|assembly.MaxBytes|resultMaxBytes|MaxBytes: limit|MaxBytes: 1|MaxBytes: max'
git grep -n -E 'loopbucket|AcquireOwner|safety margin|12h|12 hours|24h|approval.*retention|loop.*authority' -- openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/proposal.md
rg --files docs | rg '(ADR|adr).*(028|039|081|bucket|retention)'
git grep -n -E 'KVBuckets|kvBuckets|CreateKeyValue|History:|TTL:' -- natsclient/test_client.go natsclient/test_config.go
git grep -n -E 'AGENT_LOOPS|loops_bucket|approval_timeout' -- docs/adr/028-orchestration-architecture.md docs/adr/039-tool-call-governance-rule-driven.md docs/adr/081-graph-view-subscription.md docs/concepts/26-human-approval.md openspec/specs/agentic-loop/spec.md
rg -n '^func Test' processor/agentic-loop/approval_timeout_recovery_test.go natsclient/kv_retention_test.go natsclient/kv_retention_integration_test.go natsclient/kvspec_integration_test.go natsclient/kvspec_test.go processor/agentic-loop/shipped_config_test.go
git grep -n -E 'loopbucket|AcquireOwner' -- '*.go'
rg -n 'ApprovalTimeoutStr|approval_timeout' processor/agentic-loop/{config_test.go,component_test.go}
rg -n '^base:|manifest|539fb851|SHA-256' openspec/changes/agentic-loop-restart-safety/review-r7-retained-verdict-2026-09-16.md | head -18
rg -n 'KV|[Rr]etention|[Pp]ersist|[Aa]pproval' openspec/specs/agentic-loop/spec.md | head -75
git grep -n -E 'safety.margin|SafetyMargin|approvalRetention|approval.*margin|margin.*approval' -- processor/agentic-loop openspec/changes/agentic-loop-restart-safety/{proposal,design,tasks}.md openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md
rg -n 'loopbucket|AcquireOwner' processor/agentic-loop --glob '*.go'
git grep -n -E 'AGENT_LOOPS|loops_bucket' -- config service component configs | tail -50
rg --files .agents/contracts | rg 'reader|test'
```

Search failures, not absence evidence:

- An initial `rg -n -E '...' inventory*.md` used `-E` as an encoding option and failed; corrected targeted search is above.
- An unquoted `internal/*/*bucket*.go` shell glob failed before the intended search ran; corrected owner search is above.
- An unquoted `docs/architecture/adr-*.md` glob failed; the quoted path had no relevant results because current ADRs
  are under `docs/adr/`. Correct paths were then located and searched/read.
- `head`/`tail` locator outputs were not treated as exhaustive absence claims. The unbounded tracked Go/name searches,
  gopls owner references and working-tree loop-source searches support the absence claims.

Read-only baseline checks: `git status --short`, `git rev-parse HEAD`, `git diff -- design.md`, selected `nl`/`sed`
ranges, full required intake reads, and `shasum -a 256` over the listed files. No test, formatting, source edit,
commit, PR or issue mutation occurred.

End of inventory. Independent `INVENTORY PASS` is recorded below; no target-state or implementation authorization
is supplied by this inventory verdict.

Root materialization note: the handoff is preserved with mechanical pin-format corrections, the actual `rerr`
spelling at kvspec.go:302, and task pins moved by the concurrent R7 test-budget record correction. The tasks hash
above describes the architect's intake; its materialization-time hash is
`2d5787fcfbc3fc0532e06df54f5a3374dda2503a1960c9cece50d001091e3c06`.
Candidate-02 changes only R7 test grouping and does not change this inventory's production baseline.

Independent review passed the original materialized inventory at SHA-256
`f3248bd511b7510d770f057ec60284e20c9a8c0d5765613331a8748d2179a5f4` with no missing production acquisition owner.
The reviewer verified all other non-task baseline hashes and the separately updated tasks hash, corrected the
truncated http_activity.go checksum and nine-file count, and identified the two same-family fixtures now pinned.
Safety-margin arithmetic and configuration reconciliation remain unproven, not permission to select a policy.
No tests, runtime edits, target changes, whole-R8 approval or test-budget waiver follow from inventory review.

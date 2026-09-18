# R8 retained-facts surface and adopter inventory

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Status: inventory only. No storage, retention, admission, or runtime policy is proposed or approved here. Owner ruling `5726829372` authorizes bounded investigation. Existing reviewed task-reuse changes remain untouched.

## Evidence boundary

The following accepted inventories remain binding, unchanged appendices:

1. `inventory-r8-retention-premise-2026-09-17.md`: `fb937d0ef9c3c53e3a85deed5ee3d426f7cff3b786cc930767aa56d1881b2a40`.
2. `inventory-r8-loop-retention-reachability-2026-09-17.md`: `cd9c12ae52a35c8f7eb10f04892cba44b01ab1b9ba82ab96f98ecc9cdba89cec`.
3. `inventory-r8-origin-age-2026-09-18.md`: `bb968d5526815929612331c2a668320d290cc09151b091b8cfc3af249dd517d5`.

This supplement measures existing owners and representations, not another replay census. Prior native dispatch expiry evidence and source-backed loop reachability remain distinct; neither was rerun.

Skills applied: `entity-or-bucket` identifies current operational and graph owners without choosing a new home; `orchestration-check` keeps execution decisions inside existing components. ADR-049, the framework bucket catalog spec, and orchestration-layer guidance were read. No new communication path, payload, or exported consumer is introduced.

## 1. Facts and content are different requirements

| Ambiguity | Existing evidence and owner | Information separating safe cases | Content additionally needed for successful work |
|---|---|---|---|
| Dispatch mapping absent | Dispatch reads the exact retained registered TaskMessage using stable source-derived TaskID; a new task otherwise receives a LoopID. | Whether this source already selected a task/loop identity, and whether the incoming source agrees with it. Current validation checks more than the two IDs. | Retained task includes prompt, prior messages, routing, and execution context. Full task bytes currently provide both correlation evidence and publication commitment. |
| Loop authority absent | Loop reads the current LoopEntity by LoopID. Typed absence takes the ordinary new-work path. | Whether this is legitimate first birth or a previously admitted loop whose authority disappeared; terminal disposition also matters. | LoopEntity carries current task ownership, routing, configuration, pending work, approval state, and terminal fields. An identity-only fact would not reconstruct all of these. |
| Nonterminal authority present, request absent | Loop reads the latest retained AgentRequest for the loop. Absence currently rebuilds the initial prompt and iteration-one context. | Whether absence represents valid partial birth or loss after a request/progress had already committed. Current state, mutable timestamps, and Iterations do not establish the required discriminator. | Successful conversation reconstruction uses AgentRequest.Messages and associated execution parameters. Preventing unsafe reconstruction does not inherently require retaining all that content. |

The last column is not a claim that all content must survive indefinitely. The current task handler does not have an accepted task-scoped “continuation unavailable” settlement contract. The existing approval-specific permission cannot silently supply one.

## 2. Existing owner / same-class collision inventory

| Semantic class and owners | Representation, readers and writers | Lifecycle, ownership and recovery facts |
|---|---|---|
| Dispatch source→task/loop commitment; dispatch | Exact retained TaskMessage envelope on the resolved task output stream. USER and HTTP paths read it; dispatch publishes it. | Source-derived TaskID, minted LoopID, exact correlation validation. Retained-task reuse now avoids publishing an already-proven task again. Stream retention still controls survival. No separate mapping store was found in this seam. |
| Current loop authority; agentic-loop | Payload-only LoopEntity at bare LoopID in configured loop KV. Handlers write; recovery, continuation and dispatch activity read. | Current TaskID changes on continuation. Current reads use Get and revision/CAS where required, not historical reconstruction. Local acquisition requires History 10, TTL/MaxAge 24h, and no binding MaxBytes. |
| Selected terminal outcome; agentic-loop | Payload-only terminal event at `COMPLETE_<LoopID>`; Create selects an immutable winner. Recovery/publication and `read_loop_result` consume it. | Winner persists before required terminal publication; terminal LoopEntity is persisted afterward. Terminal transient release does not delete this durable record. |
| Conversation reconstruction; loop / model publication seam | AgentRequest envelope on independently resolved request stream; its Messages reconstruct process-local ContextManager regions. | No ContextManager persistence implementation was found in the bounded context-manager sources. Latest request content, not KV revision history, is the implemented cold reconstruction input. |
| Research loop and stage state; graphresearch tool plus five research components | Same configurable loop bucket: bare loop records, intent, stage outputs, snapshots, and completion keys. Typed stage `LoopStore` adapters wrap ordinary KVStore. | Research provisioning also requests History 10 / TTL 24h. Stage writes are Put; research initial loop creation is Create. These are actual co-tenants, not hypothetical future users. |
| Completed external tool outcome; agentic-tools | Dedicated catalog-owned completed-outcome KV; execution identity, request/call correlation, fingerprint, and ToolResult. Get/Create-only narrow owner. | Existing no-lifecycle operational precedent, History 1. No claimed/in-progress record. Absence permits execution under the tool’s existing idempotency contract; this is not dispatch mapping or loop conversation authority. |
| Tool-call audit; agentic-tools | `KVToolCallStore` stores timestamp-suffixed ToolCallRecord keys. | Distinct from immutable completed-outcome authority. Its `Store` method is not a generic loop recovery store. |
| Generic durable content; storage/objectstore, ComponentManager, storeregistry; trajectory recorder is an existing borrower | `Store` provides Put/Get/List/Delete; `StreamableStore` adds Open. Registry resolves a logical storage-instance name to the live provider. Trajectory capture stores canonical digest-addressed bytes, reuses matching existing content, reports mismatches/read faults, and verifies uncertain Put results through a freshly resolved provider. | Provider registration rejects duplicate live ownership; borrowers resolve lazily and do not own Close. ObjectStore creation omits TTL and reconciles then verifies no binding MaxAge/MaxBytes. Delete remains available. This is an existing non-evicting content/audit pattern—not already-established task mapping, loop authority, conversation reconstruction, or safe garbage collection. |
| Graph execution identity/current semantic state; loop graph writer / graph-ingest | LoopExecutionEntity births graph entity/triples; ENTITY_STATES is catalog-owned current graph authority. | Current graph state is not a historical request log. The measured task-recovery reader uses loop KV and retained requests, not graph queries. |
| Trajectory evidence; loop audit owner | Separate retained trajectory facts/evidence and reporting surfaces. | Trajectory acquisition checks History 1 / TTL 0. Audit failure is reported explicitly. No reconstruction reader in the three measured identity branches consults trajectory evidence. |
| Catalog retention enforcement; graph / natsclient | Bucket descriptors, Ensure/Open acquisition, owner-only generic-write guard, and observed retention checks. | Existing no-lifecycle and strict-no-reconciliation variants. Ownership is call-site/review enforcement, not caller authentication. AGENT_LOOPS is explicitly excluded by current catalog commentary, while loop acquisition separately enforces its local policy. |

The framework **does have a generic content-storage owner**: `storage.Store`, its `StreamableStore` extension, the production `storage/objectstore.Store`, and `storeregistry.Registry`. The loop already borrows that owner for trajectory evidence. The earlier Store enumeration was too narrowly scoped and omitted this existing primitive. No measured task/loop reconstruction branch currently uses it as recovery authority, and its availability does not establish a safe release condition or authorize a new recovery record.

### Co-location and representation collisions

AGENT_LOOPS contains eleven research intent/stage/snapshot key families in addition to bare loop and `COMPLETE_` records:

`research.request.received.`, `classify.complete.`, `classify.snapshot.`, `route.complete.`, `route.snapshot.`, `execute.complete.`, `execute.snapshot.`, `assess.complete.`, `assess.snapshot.`, `search_result.complete.`, `synthesize.snapshot.`

Research synthesize writes a registered SearchResult **envelope** at `COMPLETE_`; ordinary loop terminal selection writes the terminal **payload** there. `read_loop_result` currently unmarshals that value directly into LoopCompletedEvent. This is an existing representation/ownership collision, not an instruction to expand this change.

The sample AGENT stream covers `agent.>` with 24h MaxAge, 256 MiB MaxBytes, and DiscardOld. Changing that shared stream affects task/request evidence and unrelated co-located outputs. Resolved port overrides mean shared placement cannot be assumed globally.

## 3. Lifetime, release, and costs measured

1. Loop KV retains ten revisions but the measured recovery owners consume current values. More revisions do not presently supply missing-authority recovery.
2. LoopEntity itself can contain pending tool results, metadata and a full terminal result; it is not a tiny identity-only record.
3. COMPLETE stores full result/error payload content; `read_loop_result` pages the result for callers.
4. AgentRequest retains conversation messages and tool definitions/configuration. Retaining request content has a different cost from preserving enough evidence to prevent an unsafe first-prompt restart.
5. No durable Delete/Purge call or history-based recovery reader was found in the bounded dispatch, loop, graphresearch and five research-component owners. Current cleanup releases in-memory aggregates; durable disappearance is policy-driven.
6. No source-settlement-aware durable release condition was found. “Loop terminal” is not currently implemented as permission to erase its routing, completion, or replay evidence.
7. Bucket-wide lifetime changes would affect research outputs/snapshots and up to ten revisions, not merely the three correctness distinctions.
8. Ordinary-stream bounds currently require positive MaxAge and MaxBytes. “Retain AGENT forever” is not a configuration-only change under that contract.
9. Existing no-lifecycle KV precedent does not prove safe administrative purge, deletion, reset, lifecycle ownership, or bounded storage growth. Those remain separate obligations.

The earlier deletion search covered loop/research callers, not the generic storage owner. ObjectStore exposes an actual Delete operation. Therefore “no source-settlement-aware release predicate found in the bounded recovery owners” remains supported; “no durable deletion facility” would be false. Content storage already exists without reference-blind TTL eviction, but extending its use would still require explicit correctness ownership, reference lifetime, failure disposition, and release semantics. No such extension is selected here.

## 4. Active constraints and outward seams

R8’s planned observed stream-admission work is not an already-installed safety gate: searches for `agentstreamadmission`, `ObserveAndValidate`, and `agent_stream_replay_inadmissible` found no production Go implementation.

Current loop-bucket observation is real and enforced. Catalog-owned buckets also have real acquisition/write guards. AGENT_LOOPS does not automatically acquire those catalog protections.

Existing registered-envelope validation, publisher refusal, PubAck settlement, typed read failures and correlation conflicts remain unchanged. Governance typed absence may repropose; provider absence may reinvoke; completed tool-outcome absence may execute. None establishes the missing task/loop distinction by analogy.

| Specific adopter | Must currently know / no-change behavior | Discovery and observed gap |
|---|---|---|
| Component/config author routing tasks and requests through custom streams | Port overrides determine actual evidence owners; retention can remove identity or reconstruction evidence while source work remains replayable. | Some policy errors fail at acquisition; the complete R8 stream check is unfinished. Correctness should not depend on independently guessing a recovery duration. |
| Chat/API adopter sending a fresh continuation to an older loop | A young task timestamp does not establish the age or survival of old loop/request evidence. | Existing metadata cannot independently separate all expired-authority cases from legitimate first work. No new expiry refusal has been promised. |
| Research-pack integrator using the configured loop bucket | The bucket also contains research intent/output/snapshot/completion content with existing readers. | Bucket-wide changes have concrete cross-component effects; the shared COMPLETE representation requires explicit acknowledgement in any later design. |
| Operator provisioning/resetting NATS state | Local loop policy currently demands 24h/History 10; ordinary streams demand age/byte bounds; catalog owners have other retention contracts. | Boot errors expose some mismatches. Removing a TTL cannot restore records already lost; current strict catalog precedent makes that limitation explicit. |

No new outward symbol, bucket, subject, field, or present-consumer claim is made in this inventory.

## 5. Verifier pins

- `processor/agentic-dispatch/task_recovery.go:65` — `func stableDispatchTaskID(msg agentic.UserMessage) string {`
- `processor/agentic-dispatch/task_recovery.go:84` — `subject, streamName, err := dispatchTaskAddress(c.outputPortDefs(), taskID)`
- `processor/agentic-dispatch/task_recovery.go:103` — `if err := validateRetainedDispatchTask(retained, msg, taskID, msg.ReplyTo); err != nil {`
- `processor/agentic-dispatch/task_recovery.go:119` — `loopID = uuid.NewString()`
- `agentic/state.go:46` — `type LoopEntity struct {`
- `processor/agentic-loop/state.go:309` — `entity.TaskID = task.TaskID`
- `processor/agentic-loop/settlement_recovery.go:246` — `entry, err := c.loopsBucket.Get(ctx, loopID)`
- `processor/agentic-loop/settlement_recovery.go:247` — `if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {`
- `processor/agentic-loop/settlement_recovery.go:276` — `subject, streamName, err := agentRequestAddress(c.config.Ports.Outputs, loopID)`
- `processor/agentic-loop/settlement_recovery.go:388` — `if !found {`
- `processor/agentic-loop/settlement_recovery.go:398` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:416` — `messages = c.handler.prependIterationContext(ctx, entity.ID, 1, entity.MaxIterations, messages)`
- `agentic/types.go:108` — `type AgentRequest struct {`
- `processor/agentic-loop/context_manager.go:51` — `regions          map[RegionType][]contextMessage`
- `processor/agentic-loop/state.go:365` — `for _, msg := range request.Messages {`
- `processor/agentic-loop/component.go:1941` — `data, err := json.Marshal(candidate)`
- `processor/agentic-loop/component.go:1959` — `key := "COMPLETE_" + loopID`
- `processor/agentic-loop/component.go:1960` — `if _, err := c.loopsBucket.Create(ctx, key, data); err == nil {`
- `processor/agentic-loop/component.go:2411` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
- `processor/agentic-loop/internal/loopbucket/acquire.go:42` — `if status.History() != 10 || status.TTL() != 24*time.Hour || info.Config.MaxAge != 24*time.Hour || info.Config.MaxBytes > 0 {`
- `processor/agentic-loop/trajectory_handler_wiring.go:68` — `_ = c.handler.loopManager.DeleteLoop(loopID)`
- `processor/agentic-loop/state.go:703` — `delete(m.contextManagers, loopID)`
- `frameworkcapabilities/graphresearch/register_tool.go:72` — `return jetstream.KeyValueConfig{Bucket: bucket, History: 10, TTL: 24 * time.Hour}`
- `frameworkcapabilities/graphresearch/register_tool.go:92` — `_, err := w.kv.Create(ctx, loopID, value)`
- `frameworkcapabilities/graphresearch/executor.go:32` — `const researchTriggerKeyPrefix = "research.request.received."`
- `processor/research-graph-classify/adapters.go:238` — `func loopStoreKeyClassifyComplete(loopID string) string { return "classify.complete." + loopID }`
- `processor/research-graph-classify/adapters.go:241` — `func loopStoreKeySnapshot(loopID string) string { return "classify.snapshot." + loopID }`
- `processor/research-graph-route/adapters.go:83` — `func loopStoreKeyRouteComplete(loopID string) string    { return "route.complete." + loopID }`
- `processor/research-graph-route/adapters.go:84` — `func loopStoreKeyRouteSnapshot(loopID string) string    { return "route.snapshot." + loopID }`
- `processor/research-graph-execute/adapters.go:367` — `func loopStoreKeyExecuteComplete(loopID string) string  { return "execute.complete." + loopID }`
- `processor/research-graph-execute/adapters.go:368` — `func loopStoreKeyExecuteSnapshot(loopID string) string  { return "execute.snapshot." + loopID }`
- `processor/research-graph-assess/adapters.go:83` — `func loopStoreKeyAssessComplete(loopID string) string  { return "assess.complete." + loopID }`
- `processor/research-graph-assess/adapters.go:84` — `func loopStoreKeyAssessSnapshot(loopID string) string  { return "assess.snapshot." + loopID }`
- `processor/research-graph-synthesize/adapters.go:78` — `return "search_result.complete." + loopID`
- `processor/research-graph-synthesize/adapters.go:80` — `func loopStoreKeySynthesizeSnapshot(loopID string) string { return "synthesize.snapshot." + loopID }`
- `processor/research-graph-synthesize/adapters.go:170` — `_, err := s.kv.Put(ctx, loopCompletionKeyPrefix+loopID, envelope)`
- `processor/agentic-tools/loop_result.go:133` — `if err := json.Unmarshal(entry.Value, &event); err != nil {`
- `processor/agentic-dispatch/http_activity.go:98` — `if strings.HasPrefix(key, completeKeyPrefix) {`
- `processor/agentic-tools/outcomes.go:25` — `type completedOutcome struct {`
- `processor/agentic-tools/outcomes.go:35` — `type completedOutcomeStore interface {`
- `processor/agentic-tools/store.go:82` — `key := fmt.Sprintf("%s.%d", record.Call.ID, record.StartTime.UnixNano())`
- `processor/agentic-loop/graph_writer.go:459` — `entity := &agentic.LoopExecutionEntity{`
- `processor/agentic-loop/component.go:915` — `if history != 1 || ttl != 0 {`
- `graph/kvcatalog.go:9` — `// Application/product buckets (AGENT_LOOPS, personas, governance`
- `graph/kvcatalog.go:58` — `entityStates := owned(BucketEntityStates, "graph-ingest",`
- `graph/kvcatalog.go:135` — `Retention:   natsclient.RetentionPolicy{Kind: natsclient.RetentionNoLifecycleStrict},`
- `graph/kvcatalog.go:150` — `owned(BucketToolCallOutcomes, "agentic-tools",`
- `processor/rule/actions.go:2187` — `if gtypes.IsFrameworkOwnedBucket(bucket) {`
- `natsclient/kvspec.go:104` — `RetentionNoLifecycleStrict RetentionKind = "no-lifecycle-strict"`
- `natsclient/stream_bounds.go:88` — `if cfg.MaxAge <= 0 {`
- `natsclient/stream_bounds.go:91` — `if cfg.MaxBytes <= 0 {`
- `configs/agentic.json:17` — `"agent.>"`
- `configs/agentic.json:19` — `"max_age": "24h",`
- `configs/agentic.json:21` — `"discard": "old"`
- `storage/storage.go:51` — `type Store interface {`
- `storage/storage.go:86` — `Delete(ctx context.Context, key string) error`
- `storage/storage.go:94` — `type StreamableStore interface {`
- `storage/storeregistry/storeregistry.go:41` — `type Registry struct {`
- `storage/storeregistry/storeregistry.go:58` — `func (r *Registry) Register(instance string, s storage.StreamableStore) error {`
- `storage/storeregistry/storeregistry.go:77` — `func (r *Registry) Deregister(instance string) {`
- `storage/storeregistry/storeregistry.go:97` — `func (r *Registry) Store(instance string) (storage.Store, bool) {`
- `service/component_manager.go:1276` — `if err := cm.storeRegistry.Register(instance, store); err != nil {`
- `processor/agentic-loop/trajectory_evidence.go:45` — `store, ok := r.stores.Store(r.storageInstance)`
- `processor/agentic-loop/trajectory_evidence.go:53` — `existing, getErr := store.Get(ctx, key)`
- `processor/agentic-loop/trajectory_evidence.go:64` — `case !errors.Is(getErr, storage.ErrObjectNotFound):`
- `processor/agentic-loop/trajectory_evidence.go:70` — `putErr := store.Put(ctx, key, encoded)`
- `processor/agentic-loop/trajectory_evidence.go:79` — `verifyStore, ok := r.stores.Store(r.storageInstance)`
- `storage/objectstore/store.go:123` — `storeConfig := jetstream.ObjectStoreConfig{`
- `storage/objectstore/store.go:149` — `if err := reconcileNoLifecycleRetention(ctx, js, bucketName, guardLogger); err != nil {`
- `storage/objectstore/store.go:358` — `func (s *Store) Delete(ctx context.Context, key string) error {`
- `storage/objectstore/store.go:368` — `err := s.store.Delete(ctx, key)`

## Searches and limits

All source searches used the claim checkout. Bounded range reads followed locators; truncated locator output was not treated as completeness evidence.

1. `gopls workspace_symbol -matcher=caseSensitive 'Store'` failed during packages.Load because Go build-cache access was denied; no packages loaded. No cache rebuild attempted. Structural completeness remains independently unverified.
2. Scoped fallback searched Store/context persistence spellings in ContextManager, compaction, loop/research adapters and tool stores. Results distinguish actual research LoopStore and tool audit/outcome owners; no generic context recovery Store was established.
3. `git grep -n -E 'loopResultBucketConfig|History:|TTL:'` over graphresearch and all five research component files found common History 10 / TTL 24h provisioning.
4. `git grep -n -E '\.(Delete|Purge|History|GetRevision)\('` over loop, dispatch, graphresearch and all five research processors, excluding tests, found only policy History observations; no durable deletion or historical-recovery calls.
5. `git grep -n -E 'agentstreamadmission|ObserveAndValidate|agent_stream_replay_inadmissible' -- '*.go' ':!*_test.go'` returned no matches.
6. `git grep -n -E 'research.request.received|classify.complete|classify.snapshot|route.complete|route.snapshot|execute.complete|execute.snapshot|assess.complete|assess.snapshot|search_result.complete|synthesize.snapshot'` over graphresearch executor and five stage adapters enumerated the shared key families.
7. `git grep -n -E 'WriteLoopCreation|releaseLoopTransientState|delete\(m\.(loops|contextManagers)|Get\(ctx, completeKeyPrefix|IsFrameworkOwnedBucket|type (KVToolCallStore|completedOutcome)|request.Messages|regions +map'` over loop/tools/rule, excluding tests, located transient release, reconstruction, result readers and catalog guards.
8. `git grep -n -E 'type LoopExecutionEntity|func .*WriteLoopCreation|TaskID.*json|SourceMessageID.*json'` in the initially named graph/loop/type scopes returned no matches; this was not used to claim absence.
9. Follow-up `git grep -n -E 'func \(e \*?LoopEntity\)|agentRunTransitions|loopTransitions|loopEntity\.Triples|entity.Triples|PersistLoopCreated|WriteLoopCreated'` located current transition and graph-birth implementation.
10. `rg --files` located the three reused inventory files; `shasum -a 256` confirmed their exact checkpoint hashes. `git rev-parse HEAD` confirmed the baseline.
11. `rg -n 'retained.facts|retained facts|572682|horizon|NoLifecycle|AGENT_LOOPS'` against the dated expiry-direction artifact found no matches; no policy claim was inferred from that search.

Independent reviewer `gopls implementation` at `storage/storage.go:51` succeeded and identified the production ObjectStore implementation. Architect inspected `storage/storage.go:1–175`, `storage/storeregistry/storeregistry.go:1–114`, `trajectory_evidence.go:1–111`, and `storage/objectstore/store.go:85–165,320–395`. `rg --files | rg 'storeregistry|trajectory_evidence.go$'` located registry/borrower files. Scoped `git grep` for registry registration, storage-provider methods, `storeregistry`, `StorageInstance`, and `Store(` located ComponentManager registration and the trajectory borrower. This correction supersedes the earlier incomplete Store enumeration; it does not claim a new recovery-storage design.

## Open evidence and decision boundaries

The existing records establish where identity, current authority, terminal payloads and reconstruction content live. They do **not** establish a safe durable deletion rule, a compact existing partial-birth discriminator, or a task-scoped terminal-unavailable settlement contract.

Any later design must explicitly account for shared bucket writers, current catalog ownership boundaries, actual resolved streams, and existing callers. It must distinguish preserving safety evidence from guaranteeing successful continuation. Administrative deletion/purge and indefinite recovery remain unproven, not implicit promises.

No tests or mutations were performed. Stop here for independent inventory review.

# R8 origin-age supplemental inventory

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Inventory only under owner ruling `5726121727`. No expiry policy, threshold, field, store, timer or implementation is proposed. Retained-task reuse is separate concurrent work; this supplement does not depend on its completion.

## Reused evidence

The following independently reviewed inventories remain unchanged evidence appendices:

1. `inventory-r8-retention-premise-2026-09-17.md`, SHA256 `fb937d0ef9c3c53e3a85deed5ee3d426f7cff3b786cc930767aa56d1881b2a40`.
2. `inventory-r8-loop-retention-reachability-2026-09-17.md`, SHA256 `cd9c12ae52a35c8f7eb10f04892cba44b01ab1b9ba82ab96f98ecc9cdba89cec`.

This supplement adds origin-age facts, not another replay-admission census.

## Existing timestamps and their owners

| Surface | Creation and preservation | Current validation/use |
|---|---|---|
| BaseMessage envelope | Default `created_at` and `received_at` originate at envelope construction on the producer clock. Wire format preserves both as Unix milliseconds. `WithTime` and `WithMeta` permit caller-supplied metadata. | Validation requires nonnil metadata, not a nonzero timestamp, age bound, ordering or future-time bound. Decoding reconstructs supplied times. |
| Durable UserMessage | Carries payload `Timestamp` separately from envelope timestamps. Dispatch decodes the envelope, then passes the UserMessage payload to submission handling. | UserMessage validation checks identity/content, not Timestamp. Retained task/source comparison does not compare timestamps. |
| HTTP submission | HTTP handler generates a new MessageID and current Timestamp for each request. | Separate HTTP requests are not same-source redelivery merely because their content matches. |
| Dispatch TaskMessage | Fresh task envelope is constructed at task preparation. UserMessage Timestamp/envelope creation time is not copied into TaskMessage. | TaskMessage has no dedicated origin timestamp or absolute expiry field. Loop intake extracts its payload without using envelope age. |
| Rule TaskMessage | Shared `publishAgentOnce` constructs a new task identity and then a new BaseMessage for each action execution. | Task validation precedes publication. No age contract is added by the rule publisher. A fresh action execution is outside the accepted same-publication reuse claim. |
| Same serialized publication | NATS publishing passes the supplied data bytes without rewriting envelope metadata. | Same-byte retry preserves embedded timestamps; it is not the same fact as the broker storage time of a later accepted publication. |
| Loop authority | `StartedAt`/`TimeoutAt` are operational fields. `SetTimeout` assigns both using current process time. | The metadata-configuration path runs for birth and attachment. StartedAt is not an immutable loop-birth timestamp across continuations. |
| Exact evidence readers | Dispatch returns retained raw data; loop reader returns subject/data. | These private readers do not expose broker record time or an absent record's former publication time. |

## Four static producer configurations

These are four configurations using the **same rule constructor**, not four independent timestamp implementations:

| Configuration | Loaded definitions | Timestamp owner |
|---|---|---|
| `configs/flows/deep-research.json` | Deep-research rule files | `publishAgentOnce` → `NewBaseMessage` |
| `configs/flows/deep-research-test.json` | Deep-research rule files | Same |
| `configs/examples/research-graph-pipeline.json` | Research-graph rule files | Same |
| `configs/research-graph-e2e.json` | Research-graph rule files | Same |

The action envelope timestamp is generated when the action wraps its task; it is not inherited from the triggering entity's creation time. The prior producer inventory identifies dispatch and rule execution as the runtime construction owners.

## What the existing facts distinguish

| Case | Observable fact | Limit |
|---|---|---|
| Delayed first delivery | Embedded creation time can be old while no earlier execution occurred. | Old age alone does not prove replay or prior effects. Current handlers impose no source-age refusal. |
| Same-byte retry | Embedded creation/received timestamps remain unchanged. | Current contracts do not validate those times as authoritative first-publication bounds. |
| Reconstructed publication | A new BaseMessage receives fresh metadata by default. | Content/TaskID correlation does not establish preserved origin age. Fresh upstream rule execution already has separate identity semantics. |
| New continuation on an older loop | Fresh submission/task can name an existing LoopID; attachment rebinds the loop's TaskID. | Task creation time does not date earlier loop authority or conversation evidence. Task payload does not carry a separate birth-versus-attachment discriminator. |
| Clock skew | Producer timestamps and consumer `time.Now()` can disagree. | No surveyed agentic boundary supplies a clock-skew contract or validates future creation times. |
| Missing identity evidence | Current typed absence remains observable. | Neither absent record age nor never-created versus expired history is supplied by that absence result. |

Existing metadata can carry an age observation across unchanged bytes. It does **not currently establish** the semantics needed to turn age into permission to create or reconstruct work.

## Existing deadlines, retention and closest problem shape

Loop execution timeout defaults to `120s`; configuring it sets `StartedAt=now` and `TimeoutAt=now+duration`. Model-response handling checks that deadline. TaskMessage.Timeout is a duration for LLM calls, not an absolute task-admission deadline. Model execution derives a request context timeout when making the call.

The existing loop bucket owner observes History 10, TTL/MaxAge 24h and nonbinding MaxBytes. Stream retention remains separately resolved/observed as inventoried in the retained-premise appendix. These operational durations do not establish an elapsed recovery guarantee.

The closest existing **absence plus observed-age refusal** is `settleAbsentApprovalEvidence`: inspect actual StreamInfo, compare current age against retention, reject zero/future/unproven age, and recheck the authority revision before its terminal path. This is an existing component-owned pattern, not proof that its StartedAt predicate transfers to every task/continuation boundary.

Two adjacent clock-aware owners have narrower jobs: graph-ingest reports broker-timestamp queue lag, including possible negative skew; storage growth excludes unusable future/too-close observations. Neither supplies agentic admission semantics.

No new primitive is proposed, so no new same-class owner or consumer-at-birth is introduced.

## Adopter seam inventory

Person: an external adapter/direct-task producer using registered envelopes without reading agentic recovery internals.

1. **Must know today:** preserve the same serialized publication for an uncertain retry; supply valid task identity. No accepted origin-age or skew policy is currently specified.
2. **Doing nothing:** default envelopes receive local timestamps, but delayed work is not rejected by an origin-age gate. Historical/custom metadata remains representable.
3. **Where discovered:** identity failures have typed validation; a correctness-sensitive timestamp contract is not currently exposed by those validators.
4. **Unresolved burden:** an age policy could affect historical timestamps, delayed first delivery and attachment to older loops. The inventory does not decide whether those cases should run or refuse.
5. **What should not be inferred:** that every producer must calculate a retention horizon, or that a timestamp identifies whether earlier work happened.

## Verifier pins

- `message/base_message.go:63` — `func WithTime(createdAt time.Time) Option {`
- `message/base_message.go:73` — `func WithMeta(meta Meta) Option {`
- `message/base_message.go:127` — `meta:    NewDefaultMeta(time.Now(), source),`
- `message/base_message.go:200` — `if m.meta == nil {`
- `message/base_message.go:236` — `"created_at":  timestamp.ToUnixMs(m.meta.CreatedAt()),`
- `message/base_message.go:237` — `"received_at": timestamp.ToUnixMs(m.meta.ReceivedAt()),`
- `message/base_message.go:276` — `createdAtMs := timestamp.Parse(wire.Meta["created_at"])`
- `message/base_message.go:290` — `m.meta = NewDefaultMetaWithReceivedAt(createdAt, receivedAt, source)`
- `message/meta_default.go:24` — `receivedAt: timestamp.Now(),`
- `agentic/user_types.go:72` — `Timestamp time.Time `json:"timestamp"``
- `agentic/user_types.go:76` — `func (m UserMessage) Validate() error {`
- `agentic/user_types.go:388` — `Timeout string `json:"timeout,omitempty"``
- `agentic/user_types.go:412` — `func (t TaskMessage) Validate() error {`
- `processor/agentic-dispatch/component.go:721` — `userMsg, ok := baseMsg.Payload().(*agentic.UserMessage)`
- `processor/agentic-dispatch/component.go:850` — `func (c *Component) buildTaskMessage(ctx context.Context, msg agentic.UserMessage, loopID, taskID string) agentic.TaskMessage {`
- `processor/agentic-dispatch/http.go:151` — `MessageID:     uuid.New().String(),`
- `processor/agentic-dispatch/http.go:161` — `Timestamp:     time.Now(),`
- `processor/agentic-dispatch/task_recovery.go:59` — `return append([]byte(nil), raw.Data...), true, nil`
- `processor/agentic-dispatch/task_recovery.go:122` — `data, err := json.Marshal(message.NewBaseMessage(task.Schema(), &task, "agentic-dispatch"))`
- `processor/rule/actions.go:1717` — `taskID := fmt.Sprintf("rule-%s-%d", entityID, time.Now().UnixNano())`
- `processor/rule/actions.go:1965` — `baseMsg := message.NewBaseMessage(task.Schema(), &task, "rule-engine")`
- `processor/rule/actions.go:1970` — `if err := e.publisher.Publish(ctx, subject, data); err != nil {`
- `configs/flows/deep-research.json:471` — `"pack_id": "deep-research-rules",`
- `configs/flows/deep-research-test.json:378` — `"pack_id": "deep-research-test-rules",`
- `configs/examples/research-graph-pipeline.json:641` — `"pack_id": "research-graph-example-rules",`
- `configs/research-graph-e2e.json:641` — `"pack_id": "research-graph-e2e-rules",`
- `natsclient/client.go:994` — `Data:    data,`
- `natsclient/client.go:1005` — `_, err = js.PublishMsg(ctx, msg)`
- `processor/agentic-loop/component.go:1287` — `task, ok := baseMsg.Payload().(*agentic.TaskMessage)`
- `processor/agentic-loop/state.go:309` — `entity.TaskID = task.TaskID`
- `processor/agentic-loop/handlers.go:937` — `h.configureLoopMetadata(loopID, task)`
- `processor/agentic-loop/handlers.go:525` — `if err := h.loopManager.SetTimeout(loopID, timeout); err != nil {`
- `processor/agentic-loop/state.go:1186` — `entity.StartedAt = now`
- `processor/agentic-loop/state.go:1187` — `entity.TimeoutAt = now.Add(timeout)`
- `processor/agentic-loop/handlers.go:1230` — `if h.loopManager.IsTimedOut(loopID) {`
- `processor/agentic-loop/config.go:371` — `Timeout:                           "120s",`
- `processor/agentic-model/component.go:994` — `reqCtx, cancel := context.WithTimeout(ctx, timeout)`
- `processor/agentic-loop/settlement_recovery.go:70` — `return retainedLoopMessage{subject: raw.Subject, data: append([]byte(nil), raw.Data...)}, true, nil`
- `processor/agentic-loop/settlement_recovery.go:937` — `age := time.Since(entity.StartedAt)`
- `processor/agentic-loop/settlement_recovery.go:940` — `entity.StartedAt.IsZero() || age < 0 || (retention.MaxAge > 0 && age >= retention.MaxAge) {`
- `processor/agentic-loop/internal/loopbucket/acquire.go:42` — `if status.History() != 10 || status.TTL() != 24*time.Hour || info.Config.MaxAge != 24*time.Hour || info.Config.MaxBytes > 0 {`
- `processor/graph-ingest/component.go:1556` — `c.ingestLag.Observe(time.Since(meta.Timestamp).Seconds())`
- `natsclient/storage_growth.go:177` — `if current.At.Sub(candidate.At) < MinGrowthSampleInterval {`

## Searches and limits

1. Reused reviewed producer/admission/retention inventories; refreshed only timestamp, caller, constructor and continuation facts.
2. `gopls workspace_symbol -matcher=caseSensitive 'TaskMessage'` failed because sandbox permissions blocked Go build/goimports caches. No result or absence claim was inferred; no cache rebuild attempted.
3. Scoped `rg`/`git grep` located `TaskMessage`, `UserMessage`, `CreatedAt`, `ReceivedAt`, `Timestamp`, `WithTime`, `WithMeta`, `StartedAt`, `TimeoutAt`, `SetTimeout`, `attachContinuation`, `PublishToStream`, `rules_files`, `publish_agent` and `four static` in the named source/configuration and prior evidence files.
4. Closest-shape search: `git grep -n -E 'CreatedAt\(\).*Before|CreatedAt\(\).*After|time.Since\(.*CreatedAt|received_at.*(reject|valid)|clock.?skew|future.?timestamp' -- ':!**/*test*' ':!openspec/changes/**' ':!docs/**'`; followed graph-ingest, graphview and storage-growth hits. Locator output truncation/head limits were not used to prove absence.
5. Read complete relevant type/validation bodies and located publication, intake, timeout, continuation and policy branches. Some initially guessed filenames did not exist; file discovery corrected them.
6. No tests, live clock measurements, mutations, policy selection or downstream census. Concurrent implementation may move pins; root should verify the materialized supplement before review.
7. Open evidence: accepted meaning/trust of origin time; delayed-first-delivery treatment; continuation relationship to older evidence; clock-skew behavior; which absence decisions, if any, those facts can justify.

Stop for independent inventory review.

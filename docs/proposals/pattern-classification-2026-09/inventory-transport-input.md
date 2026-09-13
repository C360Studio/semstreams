# Inventory: #1234 slice transport-input
base: 32aeddf73612d157261ace65d9810750d2d602e0

## #472 — message-logger /entries applies limit before subject filter — agent/tool queries return 0 under graph-ingest storms

Named sites:
- `service/message_logger_http.go:241` — `func (ml *MessageLogger) handleGetEntries(w http.ResponseWriter, r *http.Request) {`
- `service/message_logger_http.go:262` — `subjectFilter := query.Get("subject")`
- `service/message_logger_http.go:265` — `entries := ml.GetLogEntries(limit)`
- `service/message_logger_http.go:268` — `if subjectFilter != "" {`
- `service/message_logger_http.go:373` — `func (ml *MessageLogger) handleKVQuery(w http.ResponseWriter, r *http.Request) {`
All five sites hold at the same line numbers the body cites; no drift.

Refusal and observation:
- `service/message_logger_http.go:281` — `ml.logger.Error("Failed to encode entries", "error", err)`
No `errs.` calls inside `handleGetEntries` (the function has no size/order refusal logic at all — that is the defect); no metric increment. The one log emission covers only the JSON-encode failure path, unrelated to the limit/filter ordering.

Nearest pattern instance:
- `service/message_logger.go:423` — `return semerrs.WrapTransient(errors.New("message logger stopping"), "MessageLogger", "reconcileSubjects", "reconciliation admission fenced")`
No `errs.Classified`/`errs.ClassifiedCode*` call exists anywhere in `service/*.go`.

## #824 — lifecycle-gateway: workflows that cannot be created through the operator route are still advertised on it

Named sites:
- `gateway/lifecycle-gateway/handlers.go:385` — `func (c *Component) handleCreateInstance(w http.ResponseWriter, r *http.Request, workflow string) {`
- `gateway/lifecycle-gateway/handlers.go:399` — `c.recordRequest(true, "")`
- `pkg/lifecycle/manager.go:997` — `func (m *Manager) CreateFromOperator(ctx context.Context, workflow string, initial json.RawMessage) (CreateResult, error) {`
- `pkg/lifecycle/manager.go:1014` — `dec.DisallowUnknownFields()`
- `pkg/lifecycle/manager.go:1020` — `if target.EntityID() == "" {`
`DisallowUnknownFields` lives in `pkg/lifecycle/manager.go`, not `gateway/lifecycle-gateway/`; the body's "#816 added it" is this call site. The gateway route calls `CreateFromOperator` at `handlers.go:393` (the `result, err :=` line, not separately pinned above).

Refusal and observation (inside `CreateFromOperator`):
- `pkg/lifecycle/manager.go:1004` — `ErrInvalidInitialState, workflow)`
- `pkg/lifecycle/manager.go:1017` — `ErrInvalidInitialState, workflow, err.Error())`
- `pkg/lifecycle/manager.go:1022` — `ErrInvalidInitialState, workflow)`
- `pkg/lifecycle/manager.go:1030` — `return CreateResult{}, fmt.Errorf("%w: create response has no entity", ErrEmitFailed)`
None of these are `errs.` family calls — `CreateFromOperator` refuses via `fmt.Errorf("%w: ...", ErrInvalidInitialState)` / `ErrEmitFailed` sentinels, not `errs.Wrap*`/`errs.Classified*`. No slog/log emission and no metric increment inside `CreateFromOperator` itself.

Nearest pattern instance:
- `pkg/lifecycle/manager.go:297` — `func (m *Manager) Create(ctx context.Context, initial Participant) error {`

## #857 — payload-size class: framework writes that scale with data volume — one site handles the limit, ten can silently lose data

This issue's body is itself a multi-site ledger (two structural roots + nine silent-loss rows). Every cited site, re-pinned at the current base:

Named sites — structural root 1 (the one guard, and the writes that lack it):
- `graph/clustering/storage.go:138` — `if stderrors.Is(err, nats.ErrMaxPayload) {`
This is the gh#837 fix; no drift from the body's citation.
- `natsclient/kv.go:358` — `if kv.options.MaxValueSize > 0 && len(newValue) > kv.options.MaxValueSize {`
Body cited `kv.go:346`; drifted +12, relocated via `git grep -nF 'MaxValueSize'`.
- `natsclient/kv.go:194` — `func (kv *KVStore) Put(ctx context.Context, key string, value []byte) (uint64, error) {`
Body cited `kv.go:182`; drifted +12.
- `natsclient/kv.go:211` — `func (kv *KVStore) Create(ctx context.Context, key string, value []byte) (uint64, error) {`
Body cited `kv.go:199`; drifted +12.
- `natsclient/kv.go:231` — `func (kv *KVStore) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {`
Body cited `kv.go:219`; drifted +12.
- `natsclient/client.go:858` — `func (m *Client) Publish(ctx context.Context, subject string, data []byte) error {`
Body cited `client.go:842`; drifted +16.

Named sites — structural root 2 (`errs.Classify` default-to-Transient):
- `pkg/errs/errs.go:264` — `func Classify(err error) ErrorClass {`
Body cited `errs.go:264-281`; start line holds, no drift.

Named sites — silent-loss ledger:
- `processor/agentic-loop/component.go:1951` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
This is `persistCompletionState`. Body cited `1711`; drifted +240.
- `processor/agentic-loop/component.go:1978` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
This is `persistFailureState`. Body cited `1738`; drifted +240.
- `processor/agentic-loop/component.go:2003` — `if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {`
This is `persistCancellationState`. Body cited `1763`; drifted +240.
- `processor/agentic-loop/component.go:2032` — `if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {`
This is `persistLoopState`. Body cited `1792`; drifted +240.
- `natsclient/request.go:396` — `if respErr := msg.Respond(response); respErr != nil {`
Body cited `request.go:391-395`. The block now handles `nats.ErrMaxPayload` explicitly — see Refusal and observation below.
Site gone: `storage/objectstore/component.go:599` — line 599 now falls inside `Stop` (`storage/objectstore/component.go:578-628`), unrelated to any reply path. `grep -ni respond storage/objectstore/component.go` and `grep -ni reply storage/objectstore/component.go` are both zero-hit in this file (see Searches).
- `processor/graph-ingest/component.go:2700` — `// invalidation is owed either.`
Body cited `2700`; no drift.
- `processor/graph-ingest/component.go:2707` — `atomic.AddInt64(&c.errors, 1)`
Body cited `2707`; no drift.
Site gone: `processor/graph-ingest/component.go` line `2969` — the file is now 2870 lines total (`wc -l`), so line 2969 does not exist. No identifying text was given in the body for this specific line beyond the row's general description, so it could not be relocated by `git grep -nF`.
- `graph/clustering/summary_store.go:156` — `if _, err := s.kv.Put(ctx, SummaryKey(rec.Level, rec.MembershipHash), data); err != nil {`
No drift.
- `graph/embedding/storage.go:224` — `if _, err := s.indexBucket.Put(ctx, entityID, data); err != nil {`
No drift.
- `graph/embedding/storage.go:273` — `if _, err := s.indexBucket.Put(ctx, entityID, data); err != nil {`
No drift; identical text to line 224 — two separate call sites.
- `graph/embedding/storage.go:628` — `if _, err := s.dedupBucket.Put(ctx, contentHash, data); err != nil {`
No drift.
- `processor/graph-index/component.go:1719` — `return errs.Wrap(putErr, "Component", "updateOutgoingIndexBatch", "KV store")`
Body cited `1739`; drifted -20.
- `processor/research-graph-synthesize/component.go:491` — `c.logger.Warn("snapshot write failed; chain continues but downstream readback may miss it",`
Body cited `393`; drifted +98.
- `processor/research-graph-synthesize/component.go:513` — `c.logger.Warn("COMPLETE_ write failed; parent's read_loop_result will return key-not-found",`
Body cited `409`; drifted +104.
Site gone: `processor/agentic-loop/graph_writer.go` line `519` — `StoreContent` does not appear anywhere in this file (case-insensitive, zero hits — see Searches).
Site gone: `processor/graph-ingest/query.go` line `222` — `maxPrefixResponseBytes` does not appear anywhere in this file (case-insensitive, zero hits — see Searches).
- `config/streams.go:175` — `Subjects:  []string{"governance.verdict.>"},`
No drift.
- `config/streams.go:179` — `Discard:   StreamDiscardOld,  // See the note above — DiscardNew would break the verdict publish path`
No drift.

Refusal and observation (representative — the sites above already carry the `errs.`/log/metric call for each row):
- `natsclient/request.go:398` — `refusal := errs.ClassifiedCodeDetail(`
This is coded, and new relative to the body's description of this row ("requester sees only a timeout, never 'too large'") — the block now classifies `nats.ErrMaxPayload` explicitly.
- `processor/graph-index/component.go:1719` — `return errs.Wrap(putErr, "Component", "updateOutgoingIndexBatch", "KV store")`
Uncoded `Wrap`.
- `processor/graph-ingest/component.go:2707` — `atomic.AddInt64(&c.errors, 1)`
Metric-style counter.

Nearest pattern instance:
- `natsclient/request.go:398` — `refusal := errs.ClassifiedCodeDetail(`

## #1002 — pkg/logging: doc.go documents the NATS log subject backwards (logs.{source}.{level}); the handler emits logs.{level}.{source}

Named sites:
- `pkg/logging/doc.go:42` — `// Publishes log records to NATS subjects in the format logs.{source}.{level}.`
- `pkg/logging/doc.go:57` — `//	// Published to: logs.my-component.INFO`
- `pkg/logging/doc.go:61` — `//	// Published to: logs.udp-input.INFO`
- `pkg/logging/doc.go:65` — `//	// Published to: logs.system.INFO`
- `pkg/logging/doc.go:91` — `//	logs.{source}.{level}`
- `pkg/logging/nats_handler.go:86` — `// Build subject: logs.{level}.{source}`
- `pkg/logging/nats_handler.go:88` — `subject := fmt.Sprintf("logs.%s.%s", r.Level.String(), source)`
All seven sites hold at the exact lines the body cites; no drift.

Refusal and observation:
(none — see Searches)
`pkg/logging/nats_handler.go` has no `errs.` calls at all (`grep -n 'errs\.' pkg/logging/nats_handler.go` → 0); the file is an `slog.Handler` implementation, not a refusal site.

Nearest pattern instance:
(none — see Searches)
No `errs.Classified`/admission-gate/create-vs-exists/read-through/authority-delegation shape found in `pkg/logging/*.go` within budget.

## #1076 — audit: KV bucket bindings at boot — WaitForBucket has zero callers; which consumers bind without a readiness wait?

Named sites:
- `natsclient/client.go:1383` — `func (m *Client) WaitForBucket(ctx context.Context, name string, timeout time.Duration) (jetstream.KeyValue, error) {`
Body cited `client.go:1371`; drifted +12.
- `natsclient/client.go:1398` — `watcher := resource.NewWatcher(name, func(checkCtx context.Context) error {`
- `natsclient/client.go:1411` — `if !watcher.WaitForStartup(ctx) {`
`WaitForBucket` callers, re-measured 2026-09-13: `git grep -n "WaitForBucket(" -- '*.go' | grep -v _test.go` → 1 hit (the definition itself) — zero callers confirmed, matching the body's claim.

The body's per-package site counts are not path:line citations and are recorded here as plain text, not pinned: bucket-acquisition sites outside test files (`GetKeyValueBucket(` / `EnsureFrameworkBucket(` / `.KeyValue(ctx` / `WaitForBucket(`) — 43 total, 22 in `test/e2e`, ~21 production across `processor/rule` (2), `processor/agentic-loop` (2), `processor/agentic-dispatch` (2), `graph/readiness` (2), `service/message_logger_*` (3), `processor/graph-index` (1), `processor/agentic-tools` (1), `processor/agentic-governance` (1), `pkg/retry` (1), `pkg/resource` (1). `pkg/resource.Watcher` adopters (7): `graph-clustering`, `graph-index`, `graph-index-temporal`, `graph-index-spatial`, `graph-embedding`, `rule/entity_watcher.go`, and `natsclient/client.go` itself, inside `WaitForBucket` (confirmed above at line 1398). None of these package-level counts were individually re-pinned — NOT RUN, see Searches; out of budget for a 43-site re-derivation.

Refusal and observation (inside `WaitForBucket`):
- `natsclient/client.go:1412` — `return nil, fmt.Errorf("bucket %q not available after %s", name, timeout)`
Not an `errs.` family call — plain `fmt.Errorf`. No direct `slog`/log call inside `WaitForBucket` itself — `m.logger` is passed into `resource.Config` at line 1408, not called inline. No metric increment.

Nearest pattern instance:
- `natsclient/kvspec.go:239` — `func EnsureFrameworkBucket(ctx context.Context, c *Client, spec BucketSpec) (jetstream.KeyValue, error) {`

## #1201 — composition lint: refuse a declared request/reply subject captured by a declared stream filter

Named sites:
- `docs/adr/100-compositions-are-validated-diagrams-are-projections.md:1` — `# ADR-100: Compositions Are Validated; Diagrams Are Projections`
- `configs/flows/crud-tools-test.json:23` — `"TOOL": {`
The parked third attempt's files do not exist in the tree: `ls natsclient/subject_capture.go component/request_reply_subjects.go` → both `No such file or directory` (2026-09-13), matching the body's "never merged" claim.

No concrete ADR-100 composition-validator Go entry point was located within budget. `git grep -ln 'ValidateFlow\|ValidateComposition\|flow.*[Vv]alidat' -- '*.go'` (excluding `_test.go`) surfaces `component/flowgraph/flowgraph.go` and `component/flowgraph/flowgraph_analysis.go` among ten files, but `grep -n 'func '` against `flowgraph_analysis.go` returns zero hits under both anchored and unanchored patterns. The exact validator function is NOT RUN further — see Searches.

Refusal and observation:
NOT RUN — no named validator function was located within budget to inspect (see Searches).

Nearest pattern instance:
(none — see Searches)
No admission-gate/classified-refusal/create-vs-exists shape was located on the composition-validation surface within budget.

## Adjacent claims

- #472: none of the five draft PRs name it
- #472: body names semspec #300 (external repo issue, not in the 60-set); no ADR/spec cited
- #824: none of the five draft PRs name it
- #824: body names #814, #816 (Refs gh#814, PR #816) — neither in the 60-set
- #857: none of the five draft PRs name it
- #857: body names gh#855, gh#837 — neither in the 60-set
- #1002: none of the five draft PRs name it
- #1002: body names #997 (semstreams-ui migration) — not in the 60-set; no ADR/spec cited
- #1076: none of the five draft PRs name it
- #1076: body names #1073 (the stream-side twin, referenced three times) — not in the 60-set; body also cites "the reviewer contract's 'exported surface with no consumer' rule" — a role-contract reference, not a pinned spec/ADR
- #1201: none of the five draft PRs name it
- #1201: body names #810, #1143 — both in the 60-set, at `docs/proposals/pattern-classification-2026-09/issues.md:28` and `:52`
- #1201: cross-reference — #1201 is the config-composition subset the owner carved out of the parked #810 ("Relationship to the #810 park ruling" section, 2026-08-31 ruling on #810); body also names #859, #862 as the possible sequencing gate on the port model — neither in the 60-set
- `docs/adr/100-compositions-are-validated-diagrams-are-projections.md:1` — `# ADR-100: Compositions Are Validated; Diagrams Are Projections`

## Searches
- `gh issue view 472,824,857,1002,1076,1201 --json number,title,body,labels,milestone` (one loop) → 6 bodies fetched
- `gh pr view 1141,1156,1159,1254,1297 --json number,body` (one loop) → 5 bodies fetched
- `grep -nE '#(472|824|857|1002|1076|1201)\b' prs-transport-input.txt` → 0
- `cat docs/proposals/pattern-classification-2026-09/issues.md` → 60-set reviewed (74 lines)
- `git grep -n "func.*handleGetEntries" -- service/message_logger_http.go` → 1
- `git grep -n "GetLogEntries(limit)" -- service/message_logger_http.go` → 1
- `git grep -n "subjectFilter" -- service/message_logger_http.go` → 3
- `git grep -n "handleKVQuery" -- service/message_logger_http.go` → 4
- `grep -n "ml.logger.Error" service/message_logger_http.go` → 5
- `git grep -n "errs.Classified\|errs.Wrap" -- service/*.go` → 18 (all `Wrap*`; 0 `Classified*`)
- `grep -n "func.*beginReconciliation\|func.*endReconciliation" service/message_logger.go` → 2
- `git grep -ln "workflows/{type}\|POST /workflows" -- '*.go'` → 5 files
- `git grep -rln "DisallowUnknownFields" -- '*.go'` (filtered to gateway) → 1 (`gateway/graph-gateway/component.go`, not lifecycle-gateway)
- `grep -n "func.*[Cc]reate.*[Ww]orkflow\|workflows/{" -- gateway/lifecycle-gateway/*.go` → 17
- `grep -n "func.*writeErrorFromLifecycle\|func.*writeBodyReadError\|func.*recordRequest" gateway/lifecycle-gateway/*.go` → 3
- `git grep -n "func.*CreateFromOperator" -- pkg/lifecycle/*.go` → 1 (plus 5 test references, not counted)
- `grep -n "func (m \*Manager) CreateFromOperator\|ErrInvalidInitialState\|DisallowUnknownFields\|target.EntityID() == \"\"\|ErrEmitFailed" pkg/lifecycle/manager.go` → 9
- `grep -n "^func (m \*Manager) Create(" pkg/lifecycle/manager.go` → 1
- `sed -n "${n}p"` over 28 literal `path:line` citations from #857's body (one loop) → 8 matched the body's description unchanged, 20 required relocation or were found gone
- `grep -n "MaxValueSize" natsclient/kv.go` → 4
- `grep -n "^func.*KVStore.*Put(\|^func.*KVStore.*Create(\|^func.*KVStore.*Update(\|^func.*KVStore.*UpdateWithRetry" natsclient/kv.go` → 5
- `grep -n "func.*Publish(" natsclient/client.go` → 1
- `grep -n "COMPLETE_" processor/agentic-loop/component.go` → 9
- `grep -n "loopsBucket.Put" processor/agentic-loop/component.go` → 4
- `grep -n "msg.Respond\|func.*Respond" natsclient/request.go` → 1
- `grep -n "\.Respond(" storage/objectstore/component.go` → 0
- `grep -ni "respond" storage/objectstore/component.go` → 0
- `grep -ni "reply\b" storage/objectstore/component.go` → 0
- `grep -n "^func (c \*Component)" storage/objectstore/component.go` → 20
- `grep -n "func.*[Pp]ut\|func.*[Cc]reate\|CompareAndSwap\|CompareAndUpdate\|entityStatesBucket\.\|statesBucket\." processor/graph-ingest/component.go` → 5
- `wc -l processor/graph-ingest/component.go` → 2870 (line 2969 does not exist)
- `grep -n "\.Put(ctx\|\.Create(ctx\|\.Update(ctx\|CompareAndUpdate\|\.CompareAndSet(" processor/graph-ingest/component.go` → 3
- `grep -n "entityBucket\." processor/graph-ingest/component.go` → 9
- `grep -n "errs.Wrap(putErr" processor/graph-index/component.go` → 1
- `grep -n "OUTGOING" processor/graph-index/component.go` → 3
- `grep -n "func.*PutSnapshot\|func.*PutLoopCompletion\|Warn" processor/research-graph-synthesize/component.go` → 5
- `grep -n "snapshot write failed\|COMPLETE_ write failed" processor/research-graph-synthesize/component.go` → 2
- `grep -n "StoreContent" processor/agentic-loop/graph_writer.go` → 0
- `grep -ni "storecontent" processor/agentic-loop/graph_writer.go` → 0
- `grep -n "func " processor/agentic-loop/graph_writer.go` → 19 (none named `StoreContent`)
- `grep -n "maxPrefixResponseBytes" processor/graph-ingest/query.go` → 0
- `grep -ni "prefixresponse\|maxprefix" processor/graph-ingest/query.go` → 2 (both `graph.MaxPrefixQueryLimit`, unrelated identifier)
- `sed -n "${n}p"` over `config/streams.go:175,179` → both matched unchanged
- `sed -n '1371p' natsclient/client.go` (body's original citation) → blank line, drifted
- `grep -n "func.*WaitForBucket" natsclient/client.go` → 1
- `git grep -n "WaitForBucket(" -- '*.go' | grep -v _test.go` → 1 (definition only; zero callers)
- `sed -n '1383,1420p' natsclient/client.go` → viewed function body
- `grep -n "^func.*Watcher\|^type Watcher" pkg/resource/*.go` → 9
- `grep -n "^func.*EnsureFrameworkBucket" natsclient/*.go` → 1
- `sed -n "${n}p" pkg/logging/doc.go` for lines 42,57,61,65,91 → all 5 matched unchanged
- `sed -n '84,89p' pkg/logging/nats_handler.go` → viewed context
- `grep -n "Build subject: logs\|fmt.Sprintf(\"logs.%s.%s\"" pkg/logging/nats_handler.go` → 2
- `grep -n "errs\.\|slog\." pkg/logging/nats_handler.go` → 10 (all `slog.`, 0 `errs.`)
- `grep -rn "composition validator\|CompositionValidator\|offline composition" -i .` (zsh glob error on `--include`) → errored, superseded by the `git grep` searches below
- `ls docs/adr/ | grep -i 100` → 1 (`100-compositions-are-validated-diagrams-are-projections.md`)
- `grep -n "^# " docs/adr/100-compositions-are-validated-diagrams-are-projections.md` → 1
- `ls configs/flows/crud-tools-test.json` → found
- `ls natsclient/subject_capture.go component/request_reply_subjects.go` → both missing
- `grep -n "tool.list\|TOOL" configs/flows/crud-tools-test.json` → 5
- `git grep -ln "compositionvalidat\|CompositionValidat\|composition_validat" -- '*.go'` → 0
- `git grep -ln "ValidateFlow\|ValidateComposition\|flow.*[Vv]alidat" -- '*.go'` (excluding `_test.go`) → 10 files
- `grep -n "^func " component/flowgraph/flowgraph_analysis.go` → 0
- `grep -n "func " component/flowgraph/flowgraph_analysis.go` (unanchored) → 0

NOT RUN:
- Individual `path:line` relocation for #1076's 43 package-level bucket-acquisition sites (only the package/count breakdown from the body is recorded; out of budget)
- The concrete Go entry point for #1201's "ADR-100 offline composition validator + boot path" (only the ADR heading and a two-file lead in `component/flowgraph/` were found; no `func` declaration located in the leads within budget)
- Refusal/observation and nearest-pattern-instance search for #1201 beyond the above (depends on locating the validator entry point first)

# Inventory: #1234 slice agentic-tools
base: 32aeddf73612d157261ace65d9810750d2d602e0

## #810 — agentic-tools: tool.list discovery is silently swallowed when a JetStream stream covers tool.>
Named sites:
- `processor/agentic-tools/config.go:131` — `Name: "tool.list", Config: component.NATSRequestPort{Subject: "discovery.tool.list"}, Description: "Tool discovery request/reply",`
- `configs/flows/crud-tools-test.json:23` — `"TOOL": {`
The body quotes this stream's subjects as `["tool.>"]`; that literal is gone from the file today — `configs/flows/crud-tools-test.json:25` now reads `"tool.execute.>",` and :26 reads `"tool.result.>"` (the 2026-08-02-comment "nine config narrowings", d621ab9c). The stream-vs-request/reply collision this issue reports is about the class, not this one file, per the 2026-08-31 archaeology comment on the issue.
The body's quoted port description ("Override to e.g. 'discovery.tool.list' when JetStream streams cover 'tool.>'.") no longer exists anywhere in `config.go`; PR #941 (c893cd53) replaced it with the plain description above and moved the shipped default itself off the streamable subject (recorded on-issue 2026-08-31).
`natsclient/subject_capture.go` and `component/request_reply_subjects.go` (the parked provisioning guard) are NOT on main — zero hits (see Searches).
Refusal and observation:
(none — see Searches: `DefaultConfig` at `processor/agentic-tools/config.go:124` is a pure data constructor, no errs./slog./metric call sites)
Nearest pattern instance:
- `composition/analyze.go:82` — `if explicitStreamCovers(streams, streamNames[portKey{warning.SubscriberComp, warning.SubscriberPort}], warning.Subjects) {`
`explicitStreamCovers` (declared `composition/analyze.go:114`) is a declared-stream-vs-declared-subject coverage check in the composition lint — the closest existing shape to the cross-check the Fable ruling (2026-07-31, on-issue) specifies for the guard, though it checks stream requirement coverage for subscribers, not request/reply-subject capture. No literal `stream-provisioning` Go package exists; `openspec/specs/stream-provisioning/` is the OpenSpec capability home.

## #1045 — agentic-governance: the ADR-043 verdict is published but unreachable — dotted detail keys cannot be walked by $message paths
Named sites:
ADR-043 decision 4 (`docs/adr/043-prompt-injection-defense-detonation-corpus.md:219`, quoted content has inline backticks so it is not pinned as a bullet — see the clean :210 heading pin below for the anchor): "4. Verdict surfaces as a rule-readable predicate on the message (e.g., governance.injection_risk: 0.87)... the rule sees the score and bucket but the full detonation trajectory (if any) stays in KV."
- `processor/agentic-governance/violation.go:157` — `if err := h.natsClient.Publish(ctx, subject, violationJSON); err != nil {`
- `processor/agentic-governance/config.go:216` — `Name: "violations", Config: component.JetStreamPort{Subjects: []string{"governance.violation.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/injection_classifier.go:206` — `governance.InjectionSignal:      signal,`
- `processor/agentic-governance/injection_classifier.go:230` — `WithDetail(governance.InjectionSignal, match.Intent).`
- `processor/agentic-governance/violation.go:46` — `// Details contains filter-specific violation information`
- `processor/agentic-governance/violation.go:260` — `func (v *Violation) WithDetail(key string, value any) *Violation {`
- `processor/rule/expression/message_path.go:28` — `func ExtractMessageValue(data MessageFields, path string) (any, bool) {`
The body cited `message_path.go:14-27` for this function; that range now holds the package/function doc comment (added since `43dbf6fb`) and the function itself starts at line 28 — same fact, shifted down.
- `vocabulary/governance/register.go:33` — `vocabulary.WithRuleOpaque(true))`
- `processor/rule/config_validation.go:214` — `if field != "" && vocabulary.IsRuleOpaque(field) {`
- `processor/rule/config_validation.go:298` — `if vocabulary.IsRuleOpaque(c.Field) {`
Every other cited line (violation.go:147-157 publish block, config.go:216 port, injection_classifier.go:206-233 detail map, violation.go:46-47/260-265) still holds at the body's original line numbers.
Refusal and observation:
- `processor/agentic-governance/violation.go:101` — `h.metrics.recordViolation(violation.FilterName, violation.Severity)`
- `processor/agentic-governance/violation.go:150` — `return errs.WrapInvalid(err, "ViolationHandler", "Handle", "resolve violation subject")`
- `processor/agentic-governance/violation.go:154` — `return errs.Wrap(err, "ViolationHandler", "Handle", "marshal violation")`
- `processor/agentic-governance/violation.go:158` — `return errs.WrapTransient(err, "ViolationHandler", "Handle", "publish violation")`
- `processor/rule/config_validation.go:215` — `return errs.WrapInvalid(`
- `processor/rule/config_validation.go:299` — `return errs.WrapInvalid(`
`ExtractMessageValue` (`processor/rule/expression/message_path.go`) itself has zero errs./slog./metric calls — it is a pure walker that returns `(nil, false)` on miss (none — see Searches). `injection_classifier.go`'s `buildMetadata`/`buildViolation` (the functions building the detail map) likewise have zero errs./slog./metric calls (none — see Searches).
Nearest pattern instance:
- `processor/rule/entity_pattern_contract.go:68` — `return errs.ClassifiedCodeDetail(`
`processor/rule` (the package that owns `message_path.go` and the opacity guard) already has a classified-refusal-plus-observed-signal instance at this call site — contrast with the uncoded `errs.WrapInvalid` family used at the two `IsRuleOpaque` refusal sites above.

## #1124 — ExecutorRegistry: a tool registered under a dispatch key its executor does not advertise dispatches but never lists — advertised vs dispatchable divergence
Named sites:
- `processor/agentic-tools/executor.go:158` — `func (r *ExecutorRegistry) ListTools() []agentic.ToolDefinition {`
- `processor/agentic-tools/executor.go:211` — `func (r *ExecutorRegistry) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {`
- `processor/agentic-tools/executor.go:52` — `func (r *ExecutorRegistry) RegisterTool(name string, executor ToolExecutor) error {`
The body's "The #1116 guard covers the eleven retired flow names only" — that guard is test-only:
- `processor/agentic-tools/executors/register_test.go:489` — `var retiredFlowToolNames = []string{`
- `processor/agentic-tools/executors/register_test.go:517` — `for _, name := range retiredFlowToolNames {`
No production-side guard named "eleven" or "retired flow" exists (zero hits outside this test file — see Searches).
Refusal and observation:
- `processor/agentic-tools/executor.go:54` — `return errs.WrapInvalid(fmt.Errorf("tool name cannot be empty"), "ExecutorRegistry", "RegisterTool", "validate name")`
- `processor/agentic-tools/executor.go:57` — `return errs.WrapInvalid(fmt.Errorf("executor cannot be nil"), "ExecutorRegistry", "RegisterTool", "validate executor")`
- `processor/agentic-tools/executor.go:64` — `return errs.WrapInvalid(fmt.Errorf("tool %q is already registered", name), "ExecutorRegistry", "RegisterTool", "check duplicate")`
`RegisterTool` has no check that `executor.ListTools()` advertises the name it is keyed under — that absence is the issue. `Execute` (:211) has zero errs./slog./metric calls in its own body (none — see Searches; it delegates to the found executor).
Nearest pattern instance:
- `processor/agentic-tools/executor.go:109` — `if def.Name == "" {`
`RegisterExecutor` (declared `:92`) already validates every definition name and effect and rejects duplicates before committing any (two-pass, validate-then-commit) — the shape `RegisterTool` lacks. Same package, same divergence the issue names.

## #1138 — agentic-tools/http_request: advertised readable-text contract returns raw HTML
Named sites:
- `processor/agentic-tools/executors/httprequest.go:110` — `func (e *HTTPRequestExecutor) ListTools() []agentic.ToolDefinition {`
- `processor/agentic-tools/executors/httprequest.go:204` — `content := string(body)`
- `processor/agentic-tools/executors/web_emit_test.go:232` — `e.emitObservation(context.Background(), call, "https://example.com/docs", resp, "<html>body</html>", false)`
- `processor/agentic-tools/executors/web_emit_test.go:258` — `agvocab.WebText:        "<html>body</html>",`
All four sites hold at (or within one line of) the body's cited ranges; the raw-HTML expectation is still pinned exactly as described.
Refusal and observation:
`Execute` (`processor/agentic-tools/executors/httprequest.go:135`) has zero `errs.` calls — every failure is a bare string on `ToolResult.Error` (e.g. `Error: fmt.Sprintf("request failed: %v", err)`), no `ErrorKind` classification (none — see Searches).
`emitObservation` (`:234`, the function that stores the raw value into the web-observation entity) has six `slog.Warn`/`e.logger.Warn` calls, no `errs.`, no metric:
- `processor/agentic-tools/executors/httprequest.go:236` — `e.logger.Warn("http_request emission skipped: tool call missing loop_id",`
- `processor/agentic-tools/executors/httprequest.go:242` — `e.logger.Warn("http_request emission skipped: cannot resolve loop entity",`
- `processor/agentic-tools/executors/httprequest.go:249` — `e.logger.Warn("http_request emission skipped: cannot build observation entity",`
- `processor/agentic-tools/executors/httprequest.go:268` — `e.logger.Warn("http_request emission skipped: observation fails its contract",`
- `processor/agentic-tools/executors/httprequest.go:274` — `e.logger.Warn("http_request observation emission failed",`
- `processor/agentic-tools/executors/httprequest.go:284` — `e.logger.Warn("http_request backlink emission failed",`
Nearest pattern instance:
- `processor/agentic-tools/executors/graph_query.go:214` — `ErrorKind: agentic.ToolErrorNotFound,`
Same package (`processor/agentic-tools/executors`); `graph_query.go` is the only executor file using typed `ErrorKind` classification (also :226, :237, :243, :280) — the shape `httprequest.go` lacks entirely.

## #1140 — agentic-governance: untrusted tool results bypass injection filtering before model context
Named sites:
- `docs/adr/043-prompt-injection-defense-detonation-corpus.md:9` — `Forcing function: sponsor interest in the`
- `docs/adr/043-prompt-injection-defense-detonation-corpus.md:210` — `### Runtime classification flow`
- `processor/agentic-governance/config.go:189` — `Name: "task_validation", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:193` — `Name: "request_validation", Config: component.JetStreamPort{Subjects: []string{"agent.request.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:197` — `Name: "response_validation", Config: component.JetStreamPort{Subjects: []string{"agent.response.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/component.go:309` — `func (c *Component) setupInputConsumers(ctx context.Context) error {`
- `processor/agentic-loop/handlers.go:2194` — `func (h *MessageHandler) HandleToolResult(ctx context.Context, loopID string, toolResult agentic.ToolResult) (HandlerResult, error) {`
- `processor/agentic-loop/handlers.go:2111` — `ParentLoopID: entity.ParentLoopID,`
- `processor/agentic-loop/handlers.go:2423` — `toolStatus = "failed"`
- `processor/agentic-loop/handlers.go:2528` — `messages := cm.GetContext()`
- `processor/agentic-loop/trajectory_evidence.go:22` — `func (r *trajectoryRecorder) captureEvidence(`
The three multi-line ranges the body cites (2111-2181, 2423-2478, 2528-2570) span `handleCompleteResponse` (:2088), `buildToolTrajectoryStep` (:2419) plus `handleToolsComplete` (:2450), and a third helper respectively — `HandleToolResult` itself (the function the body names as the direct-context path) is declared at :2194, not inside any of the three cited ranges.
Refusal and observation:
- `processor/agentic-loop/handlers.go:2245` — `return result, errs.WrapFatal(fmt.Errorf("loop timeout exceeded"), "agentic-loop", "HandleToolResult", "check timeout")`
- `processor/agentic-loop/handlers.go:2236` — `slog.String("loop_id", loopID),`
- `processor/agentic-loop/handlers.go:2253` — `slog.String("loop_id", loopID),`
- `processor/agentic-governance/component.go:364` — `result, err := c.chain.Process(ctx, &msg)`
Nearest pattern instance:
- `processor/agentic-governance/component.go:364` — `result, err := c.chain.Process(ctx, &msg)`
`handleMessage` (`:349`, the function this line sits in) is an admission-gate shape already in the owning package: form-check first (`json.Unmarshal`), classify (`c.chain.Process`), one metric-reason home (`c.metrics.recordMessageProcessed`), then violation handling — the shape `HandleToolResult`'s direct tool-result path bypasses for untrusted content.

## #1252 — retire: delete agentic/identity — ADR-075's removal was executed for its siblings and missed this package
Named sites:
`agentic/identity/credential.go` — present, 249 lines (`wc -l`)
`agentic/identity/local_provider.go` — present, 248 lines
`agentic/identity/did.go` — present, 168 lines
`agentic/identity/provider.go` — present, 78 lines
`agentic/identity/errors.go` — present, 33 lines
`agentic/identity/did_test.go` — present, 245 lines
`agentic/identity/provider_test.go` — present, 234 lines
The body's per-file table sums to 1,255 lines against its own claimed total of 1,682. The current tree has two files the table omits: `agentic/identity/agent_identity.go` (181 lines, added at `a533306e`) and `agentic/identity/credential_test.go` (246 lines) — 1,255 + 181 + 246 = 1,682, matching the body's total exactly. The aggregate is right; the table is incomplete.
- `docs/adr/075-framework-package-admission-and-composition.md:69` — `| AGNTCY identity provider stub and durable core coupling | Remove |`
- `docs/operations/27-framework-package-boundary-clean-break.md:97` — `Do not recreate these facades by copying their implementations. A future A2A or SLIM adapter requires a conformant`
- `docs/operations/27-framework-package-boundary-clean-break.md:102` — `provider. No caller should treat the old stub as a migration source.`
The body cites this quote at `:100`; the sentence containing it (wrapped across the paragraph) resolves to line 102 today.
Zero-importer claim still holds: `git grep -ln "agentic/identity" -- '*.go'` outside the package itself → 0 (see Searches).
Refusal and observation:
(none — see Searches: this issue is a removal proposal, not a runtime defect; no function is named as exhibiting a refusal/observation gap)
Nearest pattern instance:
(none — see Searches: dead-surface package removal does not match any of the six named shapes)

## #1270 — agentic-tools: HintEmpty adoption sweep — three executors still answer 'nothing matched' as English prose while #1261 makes the graph-read tools its first typed adopters
Named sites:
- `processor/agentic-tools/executors/websearch.go:191` — `Content: "No results found.",`
- `processor/agentic-tools/executors/personas.go:178` — `return agentic.ToolResult{CallID: call.ID, Content: "No personas configured."}, nil`
- `processor/agentic-tools/executors/rules.go:255` — `return agentic.ToolResult{CallID: call.ID, Content: "No rules configured."}, nil`
- `processor/agentic-tools/executors/register.go:136` — `func RegisterBuiltins(ctx context.Context, reg *agentictools.ExecutorRegistry, deps ToolDependencies) error {`
- `agentic/tools.go:542` — `// typed enum: producers set it directly; consumers branch on the`
- `agentic/tools.go:563` — `HintEmpty ToolResultHint = "empty"`
`agentic/tools.go:632` — `ResultHint ToolResultHint` field with json tag `result_hint,omitempty`; the full line (with its embedded backtick-delimited struct tag) is not pinned as a bullet for that reason.
All three cited lines hold exactly; the field is named `ResultHint` (not `Hint`) — the body's own zero-hit grep `'Hint:'` matches as a substring of `ResultHint:`, so the pre-#1261 "zero production producers" and current-count claims are unaffected by the naming.
The body's Provenance cites `openspec/changes/graph-read-tools-signal-absence/inventory-verification.md`; that change is now archived at `openspec/changes/archive/2026-09-09-graph-read-tools-signal-absence/inventory-verification.md` — the un-prefixed path no longer exists.
Refusal and observation:
(none — see Searches: `Execute`/`listPersonas`/`listRules` at all three sites have zero errs./slog./metric calls around the cited lines)
Nearest pattern instance:
- `processor/agentic-tools/executors/graph_query.go:1282` — `ResultHint: hint,`
Same package; this is the one production producer of `ResultHint` on main today (`git grep -n 'Hint:' -- '*.go' ':!*_test.go'` → 1), landed by #1261.

## Adjacent claims
- #810: none of the five draft PRs (1141, 1156, 1159, 1254, 1297) name it.
- #810: body names none of the 60-set (references gh#749 and PR #809, neither in the set).
- #810: owner ruling on-issue, 2026-08-31 — the config-layer lint (#1201) proceeds in beta.165; "fix port handling first" continues to govern this issue's own implementation, so #810 stays `status:blocked`; residual scope (pub-ack decoder rejection, start-time probe) re-verifies after #1201 lands.
- #1045: none of the five draft PRs name it.
- #1045: body names none of the 60-set.
ADR-043 decision 4 (`docs/adr/043-prompt-injection-defense-detonation-corpus.md:219`) is the spec citation the #1045 body relies on; see the issue section above for why it is not pinned as a bullet.
- #1124: none of the five draft PRs name it.
- #1124: body names none of the 60-set (references #1116, #1093, neither in the set).
- #1138: PR #1141 body names it — `Closes #1138`.
- #1138: body names none of the 60-set.
- #1140: PR #1159 body names it — `#1140 policy content, #1145 framework pattern work and #1244 transition review remain separate.`
- #1140: body names #1045, #1049, #857 from the 60-set (also #1006, #1033, #1058, #808, #1113, none of which are in the 60-set).
- `docs/adr/043-prompt-injection-defense-detonation-corpus.md:9` — `Forcing function: sponsor interest in the`
- #1252: none of the five draft PRs name it.
- #1252: body names none of the 60-set (references epic #1205, not in the set).
- `docs/adr/075-framework-package-admission-and-composition.md:1` — `# ADR-075: Framework Package Admission and Explicit Capability Composition`
`docs/adr/042-oasf-taxonomy-adoption.md:1` — ADR-042 title, "OASF Taxonomy Adoption via `vocabulary/oasf` Sub-Package"; not pinned as a bullet because the title itself contains backticks.
- The body cites ADR-107 as stating the A2A/agent-trust framing rather than carving the package out; `docs/adr/107-semantic-web-vocabulary-lives-at-the-export-edge.md` (the only ADR-107 in the tree) has zero mentions of "identity" or "A2A" under any case (see Searches) — the citation does not resolve against the ADR-107 that exists today.
- #1270: none of the five draft PRs name it.
- #1270: body names #1239, #1255 from the 60-set (also #1261, not in the 60-set).

## Searches
- `git rev-parse HEAD` → `32aeddf73612d157261ace65d9810750d2d602e0`
- `gh issue view {810,1045,1124,1138,1140,1252,1270} --json number,title,body,labels,milestone` → 7/7 fetched
- `gh pr view {1141,1156,1159,1254,1297} --json number,title,body` → 5/5 fetched
- `gh issue view 810 --json comments` → 5 comments
- `grep -n "#810\b" <PR bodies>` → 0
- `grep -n "#1045\b" <PR bodies>` → 0
- `grep -n "#1124\b" <PR bodies>` → 0
- `grep -n "#1138\b" <PR bodies>` → 1 (PR 1141 `Closes #1138`)
- `grep -n "#1140\b" <PR bodies>` → 1 (PR 1159 scope boundary line)
- `grep -n "#1252\b" <PR bodies>` → 0
- `grep -n "#1270\b" <PR bodies>` → 0
- `awk` per-issue `grep -oE '#[0-9]+'` over `agentic-tools-issues.txt` → #810: {749,809,810}; #1045: {1045}; #1124: {1093,1116,1124}; #1138: {1138}; #1140: {1006,1033,1045,1049,1058,1113,1140,808,857}; #1252: {1205,1252}; #1270: {1239,1255,1261,1270}
- `git grep -n "Tool discovery request/reply" -- processor/agentic-tools/config.go` → 1 (:131)
- `git grep -n "discovery.tool.list" -- processor/agentic-tools/config.go` → 1 (:131)
- `git grep -n "tool.>" -- configs/flows/crud-tools-test.json` → 0
- `sed -n '23,27p' configs/flows/crud-tools-test.json` → subjects are `tool.execute.>`, `tool.result.>`
- `git grep -n "ClassifyReply" -- natsclient/*.go` → 20 (function exists, no pub-ack-specific rejection change visible at this budget)
- `git ls-files | grep -i subject_capture` → 0
- `git ls-files | grep -i request_reply_subjects` → 0
- `git grep -ln "subject.*capture\|CapturesSubject\|captures.*subject" -- '*.go'` → 4 files, incl. `composition/analyze.go`, `config/streams.go`
- `grep -n "^#" docs/adr/043-prompt-injection-defense-detonation-corpus.md` → 34 headings
- `find processor/agentic-governance -name "violation.go"` → 1
- `grep -n "func.*Handle\|Publish\|governance.violation" processor/agentic-governance/violation.go` → 8
- `grep -n "Details\|WithDetail" processor/agentic-governance/violation.go` → 7
- `grep -n "governance.violation" processor/agentic-governance/config.go` → 1 (:216)
- `grep -n "InjectionSignal\|InjectionTier\|InjectionScore\|InjectionTopMatchID" processor/agentic-governance/injection_classifier.go` → 8
- `grep -n "IsRuleOpaque" processor/rule/config_validation.go` → 2 (:214, :298)
- `grep -n "func ExtractMessageValue" processor/rule/expression/message_path.go` → 1 (:28)
- `awk` range 96-165 grep `errs\.|metrics\.|slog\.` on violation.go → 4 hits (:101, :150, :154, :158)
- `git grep -n "errs.Classified\|errs.ClassifiedCode" -- processor/agentic-governance/*.go` → 1 (test file only)
- `git grep -n "errs.Classified" -- processor/rule/*.go` → 7, incl. `entity_pattern_contract.go:68`
- `grep -n "func.*RegisterTool\|func.*ListTools\|func.*Execute" processor/agentic-tools/executor.go` → 3 (:52, :158, :211)
- `awk` range 52-90 grep `errs\.` on executor.go → 3 hits (:54, :57, :64)
- `git grep -n "retired" -- processor/agentic-tools/executor.go` → 0
- `git grep -n "eleven" -- '*.go'` → 3, incl. `processor/agentic-tools/executors/register_test.go:485`
- `grep -n "if def.Name" processor/agentic-tools/executor.go` → 1 (:109)
- `sed -n '109p;131p;204p;287p' processor/agentic-tools/executors/httprequest.go` (declaration/raw-return range) → all present
- `grep -n "<html>body</html>" processor/agentic-tools/executors/web_emit_test.go` → 2 (:232, :258)
- `awk` range 135-233 grep `errs\.|slog\.|metric` on httprequest.go → 0
- `grep -n "errs\.\|slog\.\|e\.logger\|Metrics\|metric" processor/agentic-tools/executors/httprequest.go` → 8 (all `slog.Logger`/`e.logger`, zero `errs.`)
- `git grep -ln "ErrorKind" -- processor/agentic-tools/executors/*.go` (non-test) → 2 (`composition_tools.go`, `graph_query.go`)
- `grep -n "ErrorKind" processor/agentic-tools/executors/graph_query.go` → 5
- `sed -n` on ADR-043 lines 9,25,210,220; config.go 185,219; component.go 308,337; handlers.go 2111,2181,2423,2478,2528,2570; trajectory_evidence.go 22,96 → all present, text captured
- `grep -n "func.*HandleToolResult" processor/agentic-loop/handlers.go` → 1 (:2194)
- `grep -n "agent\.task\.\*\|agent\.request\.\*\|agent\.response\.\*"` processor/agentic-governance/config.go → 3 (:189, :193, :197)
- `awk` enclosing-func scan for handlers.go lines 2181/2423/2528 → `handleCompleteResponse` (:2088), `buildToolTrajectoryStep` (:2419)/`handleToolsComplete` (:2450)
- `awk` range 2194-2260 grep `errs\.|slog\.|metric` on handlers.go → 6 hits incl. `:2245` (`errs.WrapFatal`)
- `grep -n "^func" processor/agentic-governance/component.go` → 23 functions
- `grep -n "result, err := c.chain.Process"` processor/agentic-governance/component.go → 1 (:364)
- `git ls-files | grep "^agentic/identity/"` → 9 files
- `git grep -ln "agentic/identity" -- '*.go' | grep -v '^agentic/identity/'` → 0
- `wc -l agentic/identity/*.go` → 1682 total across 9 files
- `git log --oneline -1 -- agentic/identity/agent_identity.go` → `a533306e`
- `git log --oneline -1 -- agentic/identity/credential_test.go` → `a533306e`
- `grep -n "agntcy_provider" docs/operations/27-framework-package-boundary-clean-break.md` → 1 (:100, referring text)
- `grep -n "No caller should treat the old stub"` → 1 (:102)
- `grep -n "AGNTCY identity provider stub" docs/adr/075-framework-package-admission-and-composition.md` → 1 (:69)
- `grep -ni "identity\|A2A\|agent-to-agent" docs/adr/107-semantic-web-vocabulary-lives-at-the-export-edge.md` → 0
- `ls docs/adr/ | grep -i "075\|107\b"` → 2 files
- `sed -n '191p'/'178p'/'255p'` on websearch.go/personas.go/rules.go → all present
- `git grep -n "Hint:" -- '*.go' ':!*_test.go'` → 1 (`graph_query.go:1282`)
- `grep -n "func RegisterBuiltins" processor/agentic-tools/executors/register.go` → 1 (:136)
- `find openspec -iname "inventory-verification.md"` → 2, incl. `openspec/changes/archive/2026-09-09-graph-read-tools-signal-absence/inventory-verification.md`
- `awk` enclosing-func + range grep `errs\.|slog\.|metric` for websearch.go:191/personas.go:178/rules.go:255 → 0 for all three
- `docs/proposals/pattern-classification-2026-09/issues.md` read in full (74 lines) for the 60-set cross-reference
NOT RUN: a repo-wide sweep for other spellings of the ADR-043/ADR-075/ADR-107 citations beyond the exact heading/quote lines shown above; a check of whether `natsclient.ClassifyReply` already rejects a JetStream pub-ack shape (only its existence was confirmed, not its current behavior) — budget spent on the seven named issues instead.

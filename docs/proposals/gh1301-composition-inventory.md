# gh#1301 — one composition root plus a capability contract: inventory

Enumeration only. No options, recommendation, target state, or verdict appear below. Every bulleted line under a
numbered section is a pin (`` `path:line` — `text` ``), checkable with `task inventory:verify docs/proposals/gh1301-composition-inventory.md`.
Tables, prose paragraphs, and numbered (`1.`) lists are not pins and are not checked — they carry cross-references
(issues, sister repos, non-file facts) that have no `path:line` in this repository.

base: df3effb25678b46c21c2e1f8713c53a26c315359

## 1. The two in-tree roots

### 1a. `cmd/semstreams/main.go` — every function

`main`, `dispatchCompositionVerb`, `fullComponentRegistry`, `run`, `(*semstreamsRootResources).close`,
`(*semstreamsRootResources).abortOnReturn`, `parseCLI`, `connectNATSWithSpinner`, `ensureStreamsWithSpinner`,
`createNATSClient`, `extractRestrictedDecideActions`, `extractPlatformMeta`, `setupRegistriesAndManager`,
`createServiceDependencies`, `configureAndCreateServices`, `registerMilestoneService`, `runUntilShutdown`,
`stopAndCloseRuntime`, `attributeRootCloseError`, `stopWithinShutdownBudget`, `remainingShutdownBudget`,
`logShutdownError`, `printHelp`, `buildRuleManager`, `buildPersonaManagerConcrete`, `loadConfig`, `registerPayloads`
(plus `printBanner`, `parseFlags`, `validateFlags`, `printDetailedHelp`, `NewSpinner` in sibling files not read for
this inventory). 838 lines total (`wc -l` → 838; file itself has 838 lines, last content line 837 per the read).

- `cmd/semstreams/main.go:100` — `func fullComponentRegistry() (*component.Registry, error) {`
- `cmd/semstreams/main.go:114` — `//revive:disable-next-line:function-length // Keep process ownership and boot ordering visible in one composition root.`
- `cmd/semstreams/main.go:115` — `func run() (runErr error) {`

### 1b. `cmd/e2e-semstreams/main.go` — every function

`main`, `dispatchCompositionVerb`, `fullComponentRegistry`, `run`, `(*e2eRootResources).close`,
`(*e2eRootResources).abortOnReturn`, `completeE2EPhaseA`, `buildPayloadRegistry`, `seedMission`, `buildRuleManager`,
`buildPersonaManager`, `parseCLI`, `getEnvOrDefault`, `printBanner`, `printHelp`, `loadConfig`,
`connectToNATSWithSpinner`, `createNATSClient`, `ensureStreamsWithSpinner`, `extractPlatformMeta`,
`extractRestrictedDecideActions`, `setupRegistriesAndManager`, `createServiceDependencies`,
`configureAndCreateServices`, `runWithSignalHandling`, `registerMilestoneService`, `runUntilShutdown`,
`stopAndCloseRuntime`, `attributeRootCloseError`, `stopWithinShutdownBudget`, `remainingShutdownBudget`,
`logShutdownError`, `registerExampleComponents`. 956 lines total.

- `cmd/e2e-semstreams/main.go:115` — `func run() (runErr error) {`
- `cmd/e2e-semstreams/main.go:335` — `type e2ePhaseAResult struct {`
- `cmd/e2e-semstreams/main.go:945` — `func registerExampleComponents(registry *component.Registry) error {`

`diff cmd/semstreams/main.go cmd/e2e-semstreams/main.go | grep -c '^[<>]'` → 771 differing lines (whole-file raw diff,
not the four named helpers below).

### 1c. The four named shared helpers, diffed

- `cmd/semstreams/main.go:559` — `func createServiceDependencies(`
- `cmd/e2e-semstreams/main.go:737` — `func createServiceDependencies(`

`diff <(sed -n '558,575p' cmd/semstreams/main.go) <(sed -n '737,753p' cmd/e2e-semstreams/main.go)` → 1 hunk, 1 line
(`cmd/semstreams`'s doc comment `// createServiceDependencies creates the Dependencies struct for services` has no
counterpart in the e2e file); every code line is byte-identical.

- `cmd/semstreams/main.go:578` — `func configureAndCreateServices(`
- `cmd/e2e-semstreams/main.go:755` — `func configureAndCreateServices(`

`diff <(sed -n '577,588p' cmd/semstreams/main.go) <(sed -n '755,765p' cmd/e2e-semstreams/main.go)` → 1 hunk, 1 line,
same shape (missing doc comment only); every code line is byte-identical.

- `cmd/semstreams/main.go:528` — `func setupRegistriesAndManager(cfg *config.Config) (*component.Registry, *service.Manager, error) {`
- `cmd/e2e-semstreams/main.go:702` — `func setupRegistriesAndManager(cfg *config.Config) (*component.Registry, *service.Manager, error) {`

`diff <(sed -n '527,556p' cmd/semstreams/main.go) <(sed -n '702,735p' cmd/e2e-semstreams/main.go)` → 3 hunks: the doc
comment (1 line), two `slog.Debug`/`slog.Info` message-text differences (2 lines), and a 5-line inserted block:

- `cmd/e2e-semstreams/main.go:719` — `	// Register bundled example/domain components used by e2e configs`
- `cmd/e2e-semstreams/main.go:720` — `	if err := registerExampleComponents(componentRegistry); err != nil {`

9 differing lines total, matching the issue's own count.

- `cmd/semstreams/main.go:647` — `func runUntilShutdown(`
- `cmd/e2e-semstreams/main.go:834` — `func runUntilShutdown(`

`diff <(sed -n '647,690p' cmd/semstreams/main.go) <(sed -n '834,869p' cmd/e2e-semstreams/main.go)` → 4 hunks: the
`healthPort int` parameter vs. `postStart func(context.Context) error` (1 line); the production copy's early
"shutdown requested before service startup" guard, absent from the e2e copy (5 lines):

- `cmd/semstreams/main.go:655` — `	select {`
- `cmd/semstreams/main.go:656` — `	case <-shutdownRequested:`

the dedicated health-listener block vs. the `postStart` hook invocation (9 lines vs. 6 lines):

- `cmd/semstreams/main.go:675` — `	if err := manager.StartHealthListener(runtimeCtx, healthPort); err != nil {`
- `cmd/e2e-semstreams/main.go:852` — `	if err := postStart(runtimeCtx); err != nil {`

and the final log-message text (1 line):

- `cmd/semstreams/main.go:688` — `	slog.Info("SemStreams shutdown complete")`
- `cmd/e2e-semstreams/main.go:867` — `	slog.Info("E2E SemStreams shutdown complete")`

### 1d. Order of every registration and wiring call in `cmd/semstreams/main.go`'s `run()`

- `cmd/semstreams/main.go:118` — `builtins.Register()`
- `cmd/semstreams/main.go:143` — `if err := cfg.Validate(); err != nil {`
- `cmd/semstreams/main.go:146` — `if err := rulepackcap.ValidateConfig(cfg); err != nil {`
- `cmd/semstreams/main.go:149` — `if err := graphresearch.ValidateConfig(cfg); err != nil {`
- `cmd/semstreams/main.go:192` — `natsClient, err := createNATSClient(cfg, phaseLogging.Client, metricsRegistry)`
- `cmd/semstreams/main.go:206` — `configManager, effectiveConfig, err := bootstrapobservability.StartValidatedConfigManager(`
- `cmd/semstreams/main.go:217` — `if err := ensureStreamsWithSpinner(bootCtx, effectiveConfig, natsClient, phaseLogging.ConfigManager); err != nil {`
- `cmd/semstreams/main.go:232` — `rootResources.stopMaxDeliveryObserver, err = maxdelivery.Start(runtimeCtx, natsClient, metricsRegistry, logger)`
- `cmd/semstreams/main.go:247` — `platform := extractPlatformMeta(cfg)`
- `cmd/semstreams/main.go:254` — `componentRegistry, manager, err := setupRegistriesAndManager(cfg)`
- `cmd/semstreams/main.go:265` — `payloadReg, err := registerPayloads(cfg)`
- `cmd/semstreams/main.go:270` — `lifecycleManager := lifecycle.NewManager(natsClient, logger)`
- `cmd/semstreams/main.go:272` — `mutationClient, err := service.WireGraphRuntime(`
- `cmd/semstreams/main.go:284` — `personaMgr := buildPersonaManagerConcrete(natsClient, logger)`
- `cmd/semstreams/main.go:286` — `if err := persona.LoadFromDirectory(bootCtx, "configs/personas/fragments", personaMgr, logger); err != nil {`
- `cmd/semstreams/main.go:291` — `if err := executors.RegisterBuiltins(bootCtx, toolRegistry, executors.ToolDependencies{`
- `cmd/semstreams/main.go:304` — `if err := registerE2EProcessBarrier(toolRegistry, natsClient); err != nil {`
- `cmd/semstreams/main.go:307` — `if graphresearch.Selected(cfg) {`
- `cmd/semstreams/main.go:314` — `svcDeps := createServiceDependencies(natsClient, metricsRegistry, logger, platform, configManager, componentRegistry)`
- `cmd/semstreams/main.go:327` — `svcDeps.LifecycleManager = lifecycleManager`
- `cmd/semstreams/main.go:334` — `if err := agentrun.Register(svcDeps.LifecycleManager); err != nil {`
- `cmd/semstreams/main.go:346` — `if err := registerMilestoneService(manager, svcDeps, natsClient, metricsRegistry, platform, logger); err != nil {`
- `cmd/semstreams/main.go:351` — `if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {`
- `cmd/semstreams/main.go:363` — `if err := service.ConfigureRulePackMutations(manager); err != nil {`
- `cmd/semstreams/main.go:368` — `return runUntilShutdown(`

### 1e. Order of every registration and wiring call in `cmd/e2e-semstreams/main.go`'s `run()`

- `cmd/e2e-semstreams/main.go:118` — `builtins.Register()`
- `cmd/e2e-semstreams/main.go:136` — `if err := cfg.Validate(); err != nil {`
- `cmd/e2e-semstreams/main.go:139` — `if err := rulepackcap.ValidateConfig(cfg); err != nil {`
- `cmd/e2e-semstreams/main.go:142` — `if err := graphresearch.ValidateConfig(cfg); err != nil {`
- `cmd/e2e-semstreams/main.go:173` — `phaseA, err := completeE2EPhaseA(ctx, cfg, phaseLogging, metricsRegistry, cliCfg.ShutdownTimeout)`
- `cmd/e2e-semstreams/main.go:184` — `rootResources.stopMaxDeliveryObserver, err = maxdelivery.Start(ctx, natsClient, metricsRegistry, logger)`
- `cmd/e2e-semstreams/main.go:189` — `componentRegistry, manager, err := setupRegistriesAndManager(cfg)`
- `cmd/e2e-semstreams/main.go:194` — `payloadReg, err := buildPayloadRegistry(cfg)`
- `cmd/e2e-semstreams/main.go:199` — `lifecycleManager := lifecycle.NewManager(natsClient, logger)`
- `cmd/e2e-semstreams/main.go:201` — `mutationClient, err := service.WireGraphRuntime(`
- `cmd/e2e-semstreams/main.go:207` — `lessonCurator := agentictools.NewLessonCurator(mutationClient, mutationClient, logger)`
- `cmd/e2e-semstreams/main.go:208` — `rootResources.lessonCurationSub, err = natsClient.SubscribeForRequests(`
- `cmd/e2e-semstreams/main.go:220` — `personaMgr := buildPersonaManager(natsClient, logger)`
- `cmd/e2e-semstreams/main.go:226` — `toolRegistry := agentictools.NewExecutorRegistry()`
- `cmd/e2e-semstreams/main.go:227` — `if err := executors.RegisterBuiltins(ctx, toolRegistry, executors.ToolDependencies{`
- `cmd/e2e-semstreams/main.go:240` — `if graphresearch.Selected(cfg) {`
- `cmd/e2e-semstreams/main.go:246` — `svcDeps := createServiceDependencies(natsClient, metricsRegistry, logger, platform, configManager, componentRegistry)`
- `cmd/e2e-semstreams/main.go:254` — `svcDeps.LifecycleManager = lifecycleManager`
- `cmd/e2e-semstreams/main.go:257` — `if err := svcDeps.LifecycleManager.Register(mission.WorkflowDeclaration()); err != nil {`
- `cmd/e2e-semstreams/main.go:261` — `if err := agentrun.Register(svcDeps.LifecycleManager); err != nil {`
- `cmd/e2e-semstreams/main.go:272` — `if err := registerMilestoneService(manager, svcDeps, natsClient, metricsRegistry, platform, logger); err != nil {`
- `cmd/e2e-semstreams/main.go:276` — `if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {`
- `cmd/e2e-semstreams/main.go:287` — `if err := service.ConfigureRulePackMutations(manager); err != nil {`
- `cmd/e2e-semstreams/main.go:291` — `return runWithSignalHandling(ctx, manager, cliCfg.ShutdownTimeout, func(seedCtx context.Context) error {`

Ordering difference: `run()` registers `mission.WorkflowDeclaration()` (line 257) and `agentrun.Register` (line 261)
back-to-back, both after `LifecycleManager` construction and before `registerMilestoneService`; `cmd/semstreams`
has only the `agentrun.Register` call (line 334) at the equivalent point, since it registers no product-level
lifecycle workflow.

### 1f. Everything in `cmd/e2e-semstreams/` and what each depends on

Directory listing (`find cmd/e2e-semstreams -type f | sort`): `main.go`; `bootstrap_observability_test.go`;
`milestone_wiring_test.go`; `registry_wiring_test.go`; `signal_shutdown_test.go`; `fixtures/register.go`,
`fixtures/register_test.go`; `mission/command.go`, `mission/command_component_test.go`, `mission/state.go`,
`mission/entity_id_semantics_test.go`.

- `cmd/e2e-semstreams/fixtures/register.go:8` — `package fixtures`
- `cmd/e2e-semstreams/fixtures/register.go:15` — `	"github.com/c360studio/semstreams/message"`
- `cmd/e2e-semstreams/fixtures/register.go:68` — `func RegisterPayloads(reg *payloadregistry.Registry) error {`
- `cmd/e2e-semstreams/mission/state.go:121` — `func WorkflowDeclaration() lifecycle.Workflow {`
- `cmd/e2e-semstreams/mission/command.go:115` — `func RegisterPayloads(reg *payloadregistry.Registry) error {`
- `cmd/e2e-semstreams/mission/command.go:161` — `func Register(registry *component.Registry) error {`
- `cmd/e2e-semstreams/mission/command.go:26` — `	"github.com/c360studio/semstreams/component"`

`fixtures` depends on `message`, `payloadregistry`, `pkg/types`, `vocabulary` (its own imports, line 10-19).
`mission` depends on `component`, `message`, `natsclient`, `payloadregistry`, `pkg/errs`, `types`, and
`github.com/nats-io/nats.go` (its own imports, `command.go:16-33`); `mission/state.go` additionally depends on
`pkg/lifecycle` and `pkg/types` for `WorkflowDeclaration`'s `lifecycle.Workflow` return type.

## 2. Every pre-`ComponentManager.Start` step from the issue's table

- `cmd/semstreams/main.go:118` — `builtins.Register()`
- `vocabulary/builtins/register.go:12` — `func Register() {`

`vocabulary/builtins.Register` fills the vocabulary registry (calls `vocabulary/agentic.Register()` and
`vocabulary/rulepacks.Register()`; no other dependency).

- `cmd/semstreams/main.go:102` — `if err := componentregistry.Register(registry); err != nil {`
- `componentregistry/register.go:81` — `func Register(registry *component.Registry) error {`
- `cmd/semstreams/main.go:105` — `if err := graphresearch.RegisterComponents(registry); err != nil {`
- `frameworkcapabilities/graphresearch/register.go:478` — `func RegisterComponents(registry *component.Registry) error {`
- `cmd/semstreams/main.go:108` — `if err := optionalotel.Register(registry); err != nil {`
- `frameworkadapters/otel/register.go:25` — `func Register(registry *component.Registry) error {`

`componentregistry.Register`, `graphresearch.RegisterComponents`, and `optionalotel.Register` all fill
`*component.Registry`; none takes a config or another registry as a dependency.

- `cmd/semstreams/main.go:265` — `payloadReg, err := registerPayloads(cfg)`
- `cmd/semstreams/main.go:828` — `if err := payloadbuiltins.Register(reg); err != nil {`
- `payloadbuiltins/register.go:36` — `func Register(reg *payloadregistry.Registry) error {`
- `cmd/semstreams/main.go:832` — `if err := graphresearch.RegisterPayloads(reg); err != nil {`
- `frameworkcapabilities/graphresearch/register.go:502` — `func RegisterPayloads(registry *payloadregistry.Registry) error {`

`payloadbuiltins.Register` and `graphresearch.RegisterPayloads` both fill `*payloadregistry.Registry`; no other
dependency.

- `cmd/semstreams/main.go:272` — `mutationClient, err := service.WireGraphRuntime(`
- `service/graph_runtime.go:16` — `func WireGraphRuntime(`

`WireGraphRuntime` fills the mutation client (`*pkg/projection.MutationClient`, consumed by `ToolDependencies` and
by every stateful tool). It depends on the NATS client, logger, and the payload registry's contract set
(`payloadReg.Contracts()...`), so it must run after `registerPayloads` and before `executors.RegisterBuiltins`.

- `cmd/semstreams/main.go:284` — `personaMgr := buildPersonaManagerConcrete(natsClient, logger)`
- `cmd/semstreams/main.go:286` — `if err := persona.LoadFromDirectory(bootCtx, "configs/personas/fragments", personaMgr, logger); err != nil {`
- `persona/file_loader.go:31` — `func LoadFromDirectory(ctx context.Context, root string, mgr *Manager, logger *slog.Logger) error {`

`persona.LoadFromDirectory` fills the persona manager (KV-backed, constructed by `persona.NewManager` inside
`buildPersonaManagerConcrete`) from a checked-in fragment directory.

- `cmd/semstreams/main.go:291` — `if err := executors.RegisterBuiltins(bootCtx, toolRegistry, executors.ToolDependencies{`
- `processor/agentic-tools/executors/register.go:136` — `func RegisterBuiltins(ctx context.Context, reg *agentictools.ExecutorRegistry, deps ToolDependencies) error {`
- `processor/agentic-tools/executors/register.go:38` — `//   - ComponentRegistry nil → list_components skipped (Pattern-B step 5)`
- `processor/agentic-tools/executors/register.go:54` — `	RuleManager       RuleManager         // Pattern-B step 1`
- `processor/agentic-tools/executors/register.go:55` — `	PersonaManager    PersonaManager      // Pattern-B step 3`
- `processor/agentic-tools/executors/register.go:56` — `	ComponentRegistry *component.Registry // Pattern-B step 5; nil → list_components skipped`
- `processor/agentic-tools/executors/register_component_catalog.go:18` — `		logger.Warn("list_components tool disabled: no ComponentRegistry provided")`

`RegisterBuiltins` fills `*agentictools.ExecutorRegistry` from a `ToolDependencies` struct whose fields are
threaded from the outputs of every earlier step: `NATSClient`, `MutationClient` (from `WireGraphRuntime`),
`RuleManager` (its own `buildRuleManager` helper), `PersonaManager` (from `persona.LoadFromDirectory`'s manager),
`ComponentRegistry` (from `componentregistry.Register`'s registry) — a nil `ComponentRegistry` here is what makes
`list_components` skip silently, per `register_component_catalog.go:18`.

- `cmd/semstreams/main.go:334` — `if err := agentrun.Register(svcDeps.LifecycleManager); err != nil {`
- `agentic/agentrun/agentrun.go:232` — `func Register(mgr *lifecycle.Manager) error {`

`agentrun.Register` fills the Lifecycle harness `Manager` (`pkg/lifecycle.NewManager`, constructed at
`cmd/semstreams/main.go:270`) with the agent-run workflow declaration; it must run after the Manager exists.

- `cmd/semstreams/main.go:254` — `componentRegistry, manager, err := setupRegistriesAndManager(cfg)`
- `cmd/semstreams/main.go:351` — `if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {`
- `cmd/semstreams/main.go:363` — `if err := service.ConfigureRulePackMutations(manager); err != nil {`
- `service/rule_pack_bind.go:49` — `func ConfigureRulePackMutations(manager *Manager) error {`

`createServiceDependencies` → `configureAndCreateServices` → `ConfigureRulePackMutations` fills `*service.Manager`;
`ConfigureRulePackMutations` depends on the rule processors already being constructed
(`configureAndCreateServices`'s `manager.ConfigureFromServices` call), so it runs strictly after.

## 3. The e2e hooks and options named in the scope bound

### 3a. Examples/fixtures registration policy

- `cmd/e2e-semstreams/main.go:945` — `func registerExampleComponents(registry *component.Registry) error {`
- `cmd/e2e-semstreams/main.go:394` — `func buildPayloadRegistry(cfg *config.Config) (*payloadregistry.Registry, error) {`
- `cmd/e2e-semstreams/main.go:399` — `	if err := iotsensor.RegisterPayloads(reg); err != nil {`
- `cmd/e2e-semstreams/main.go:402` — `	if err := document.RegisterPayloads(reg); err != nil {`
- `cmd/e2e-semstreams/main.go:405` — `	if err := mission.RegisterPayloads(reg); err != nil {`
- `cmd/e2e-semstreams/main.go:410` — `	if err := fixtures.RegisterPayloads(reg); err != nil {`

Gated by root selection only (no build tag, no env var): only `cmd/e2e-semstreams` imports
`examples/processors/document`, `examples/processors/iot_sensor`, `cmd/e2e-semstreams/fixtures`, and
`cmd/e2e-semstreams/mission`; `cmd/semstreams` imports none of them. Used by the `core` phase 2, `lessons`,
`structural`, `statistical`, `semantic`, `research-graph`, `lifecycle`, and `ops` tiers (all boot
`cmd/e2e-semstreams`; the exact per-tier registration subset is in the table at §4).

### 3b. Mission workflow and `--lifecycle-seed`

- `cmd/e2e-semstreams/main.go:257` — `if err := svcDeps.LifecycleManager.Register(mission.WorkflowDeclaration()); err != nil {`
- `cmd/e2e-semstreams/main.go:436` — `func seedMission(ctx context.Context, mgr *lifecycle.Manager, platform types.PlatformMeta, seedSuffix string) error {`
- `cmd/e2e-semstreams/main.go:510` — `	LifecycleSeed string`
- `cmd/e2e-semstreams/main.go:521` — `		LifecycleSeed:   os.Getenv("SEMSTREAMS_LIFECYCLE_SEED"),`
- `cmd/e2e-semstreams/main.go:538` — `		case arg == "--lifecycle-seed":`
- `cmd/e2e-semstreams/main.go:541` — `				cliCfg.LifecycleSeed = os.Args[i]`
- `cmd/e2e-semstreams/main.go:291` — `return runWithSignalHandling(ctx, manager, cliCfg.ShutdownTimeout, func(seedCtx context.Context) error {`

Gated by root selection (the `mission` package exists only under `cmd/e2e-semstreams/`) plus, for the seed call
itself, the `--lifecycle-seed` flag or `SEMSTREAMS_LIFECYCLE_SEED` env var being non-empty. Used by the `lifecycle`
tier (`docker/compose/lifecycle.yml:62`, `"--lifecycle-seed"`).

### 3c. Lesson-curation responder

- `cmd/e2e-semstreams/main.go:207` — `lessonCurator := agentictools.NewLessonCurator(mutationClient, mutationClient, logger)`
- `cmd/e2e-semstreams/main.go:208` — `rootResources.lessonCurationSub, err = natsClient.SubscribeForRequests(`
- `test/e2e/harness/lessoncuration/contract.go:6` — `const SubjectPromote = "e2e.control.lesson.promote"`
- `test/e2e/harness/lessoncuration/handler.go:20` — `func Handler(promoter Promoter) func(context.Context, []byte) ([]byte, error) {`

Gated by root selection only: the subscription is unconditional in `cmd/e2e-semstreams`'s `run()`, with no build
tag and no env-var check. Used by the `ops` tier per the payload-registry spec table (§4).

### 3d. Process barrier

- `cmd/semstreams/process_barrier_e2e.go:1` — `//go:build e2e_process_barrier`
- `cmd/semstreams/process_barrier_e2e.go:60` — `func registerE2EProcessBarrier(registry *agentictools.ExecutorRegistry, client *natsclient.Client) error {`
- `cmd/semstreams/process_barrier_disabled.go:1` — `//go:build !e2e_process_barrier`
- `cmd/semstreams/process_barrier_disabled.go:16` — `func registerE2EProcessBarrier(*agentictools.ExecutorRegistry, *natsclient.Client) error {`
- `test/e2e/harness/processbarrier/processbarrier.go:29` — `	ToolName = "e2e_process_barrier"`
- `test/e2e/harness/processbarrier/processbarrier.go:90` — `func Register(registry *agentictools.ExecutorRegistry, client *natsclient.Client) error {`

Gated by the `e2e_process_barrier` Go build tag on `cmd/semstreams`. Used by the `agentic` tier
(`docker/Dockerfile` target `e2e-process-barrier`, `docker/compose/agentic.yml:68`).

### 3e. Slow-consumer probe

- `cmd/semstreams/slow_consumer_probe_e2e.go:1` — `//go:build e2e_slow_consumer`
- `cmd/semstreams/slow_consumer_probe_e2e.go:12` — `func runSlowConsumerProbe(ctx context.Context, client *natsclient.Client) error {`
- `cmd/semstreams/slow_consumer_probe_disabled.go:1` — `//go:build !e2e_slow_consumer`
- `internal/e2eslowconsumer/probe_e2e.go:1` — `//go:build e2e_slow_consumer`
- `internal/e2eslowconsumer/probe_e2e.go:28` — `func Run(parent context.Context, client *natsclient.Client) error {`
- `cmd/semstreams/main.go:440` — `	if err := runSlowConsumerProbe(ctx, natsClient); err != nil {`

Gated by the `e2e_slow_consumer` Go build tag on `cmd/semstreams`. Used by the `slow-consumer` tier
(`docker/Dockerfile` target `e2e-slow-consumer`, `docker/compose/e2e-slow-consumer.yml:22`).

### 3f. Milestone probe

- `cmd/semstreams/milestone_probe_e2e.go:1` — `//go:build e2e_process_barrier`
- `cmd/semstreams/milestone_probe_e2e.go:18` — `func registerE2EMilestoneProbe(`
- `cmd/semstreams/milestone_probe_disabled.go:1` — `//go:build !e2e_process_barrier`
- `test/e2e/harness/milestoneprobe/milestoneprobe.go:53` — `	EnvVar = "SEMSTREAMS_E2E_MILESTONE_PROBE"`
- `test/e2e/harness/milestoneprobe/milestoneprobe.go:198` — `func Register(subscriber *agentrun.MilestoneSubscriber, client *natsclient.Client, logger *slog.Logger) error {`
- `test/e2e/harness/milestoneprobe/milestoneprobe.go:199` — `	if os.Getenv(EnvVar) == "" {`
- `cmd/semstreams/main.go:633` — `	if err := registerE2EMilestoneProbe(subscriber, natsClient, logger); err != nil {`
- `docker/compose/agentic.yml:87` — `      - SEMSTREAMS_E2E_MILESTONE_PROBE=1`

Gated by the SAME build tag as the process barrier (`e2e_process_barrier`) PLUS the `SEMSTREAMS_E2E_MILESTONE_PROBE`
env var (checked at runtime inside `Register`, so the tagged build is inert unless the tier's compose file also sets
the var). Used by the `agentic` tier only.

### 3g. Every file carrying `e2e_process_barrier` or `e2e_slow_consumer`

`git grep -n "go:build" -- '*.go' | grep -E "e2e_process_barrier|e2e_slow_consumer"` → 11 files:

- `cmd/semstreams/milestone_probe_disabled.go:1` — `//go:build !e2e_process_barrier`
- `cmd/semstreams/milestone_probe_disabled_test.go:1` — `//go:build !e2e_process_barrier`
- `cmd/semstreams/milestone_probe_e2e.go:1` — `//go:build e2e_process_barrier`
- `cmd/semstreams/process_barrier_disabled.go:1` — `//go:build !e2e_process_barrier`
- `cmd/semstreams/process_barrier_e2e.go:1` — `//go:build e2e_process_barrier`
- `cmd/semstreams/process_barrier_e2e_test.go:1` — `//go:build e2e_process_barrier`
- `cmd/semstreams/slow_consumer_probe_disabled.go:1` — `//go:build !e2e_slow_consumer`
- `cmd/semstreams/slow_consumer_probe_disabled_test.go:1` — `//go:build !e2e_slow_consumer`
- `cmd/semstreams/slow_consumer_probe_e2e.go:1` — `//go:build e2e_slow_consumer`
- `internal/e2eslowconsumer/probe_e2e.go:1` — `//go:build e2e_slow_consumer`
- `internal/e2eslowconsumer/probe_e2e_test.go:1` — `//go:build e2e_slow_consumer`

### 3h. `docker/Dockerfile` targets, every `docker/compose/*` file, and the Taskfile e2e tasks that build them

Four runnable targets plus three intermediate builder stages:

- `docker/Dockerfile:22` — `FROM golang:${GO_VERSION}-alpine AS builder`
- `docker/Dockerfile:65` — `FROM alpine:latest AS production`
- `docker/Dockerfile:117` — `FROM alpine:latest AS e2e`
- `docker/Dockerfile:182` — `FROM builder AS process-barrier-builder`
- `docker/Dockerfile:185` — `	-tags=e2e_process_barrier \`
- `docker/Dockerfile:193` — `FROM production AS e2e-process-barrier`
- `docker/Dockerfile:201` — `FROM builder AS slow-consumer-builder`
- `docker/Dockerfile:204` — `	-tags=e2e_slow_consumer \`
- `docker/Dockerfile:212` — `FROM production AS e2e-slow-consumer`
- `docker/Dockerfile:46` — `RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \`
- `docker/Dockerfile:52` — `    ./cmd/semstreams`
- `docker/Dockerfile:60` — `    ./cmd/e2e-semstreams`

`docker/compose/` holds 11 files: `agentic.yml`, `crud-tools.yml`, `deep-research.yml`, `e2e-slow-consumer.yml`,
`e2e.yml`, `lifecycle.yml`, `ops.yml`, `research-graph.yml`, `services.yml`, `tiered.8b.yml`, `tiered.frontier.yml`,
`tiered.yml`. `services.yml`, `tiered.8b.yml`, and `tiered.frontier.yml` build no SemStreams image (they carry
`image:` references to `semembed`/`seminstruct`/`step-ca`/`prometheus`/`grafana`, or in the `.8b`/`.frontier` cases
only environment overlays for the `semantic` tier).

- `docker/compose/crud-tools.yml:66` — `      target: production`
- `docker/compose/deep-research.yml:69` — `      target: production`
- `docker/compose/e2e.yml:56` — `      target: production`
- `docker/compose/e2e.yml:108` — `      target: e2e`
- `docker/compose/lifecycle.yml:56` — `      target: e2e`
- `docker/compose/ops.yml:76` — `      target: e2e`
- `docker/compose/research-graph.yml:67` — `      target: e2e`
- `docker/compose/tiered.yml:204` — `      target: e2e`
- `docker/compose/tiered.yml:256` — `      target: e2e`
- `docker/compose/tiered.yml:307` — `      target: e2e`
- `docker/compose/agentic.yml:68` — `      target: e2e-process-barrier`
- `docker/compose/e2e-slow-consumer.yml:22` — `      target: e2e-slow-consumer`

Taskfile: 14 `e2e:*` tasks are `includes:` pointing at `./taskfiles/e2e/<name>.yml`, plus the top-level `e2e:tiers`,
`e2e:tier`, and `e2e:all` tasks.

- `Taskfile.yml:51` — `  e2e:`
- `Taskfile.yml:53` — `  e2e:core:`
- `Taskfile.yml:55` — `  e2e:slow-consumer:`
- `Taskfile.yml:57` — `  e2e:structural:`
- `Taskfile.yml:59` — `  e2e:statistical:`
- `Taskfile.yml:61` — `  e2e:semantic:`
- `Taskfile.yml:63` — `  e2e:agentic:`
- `Taskfile.yml:65` — `  e2e:lessons:`
- `Taskfile.yml:67` — `  e2e:research-graph:`
- `Taskfile.yml:69` — `  e2e:deep-research:`
- `Taskfile.yml:71` — `  e2e:crud-tools:`
- `Taskfile.yml:73` — `  e2e:ops:`
- `Taskfile.yml:75` — `  e2e:lifecycle:`
- `Taskfile.yml:77` — `  e2e:throughput:`
- `Taskfile.yml:79` — `  e2e:openai-responses:`
- `Taskfile.yml:171` — `  e2e:tiers:`
- `Taskfile.yml:206` — `  e2e:tier:`
- `Taskfile.yml:215` — `  e2e:all:`
- `Taskfile.yml:152` — `    desc: "Full pre-push gate, mirrors CI (~11min, needs Docker): build, lint, vet integration+live_llm+e2e_process_barrier, schema drift, contract, race unit + integration. Use /preflight for the judgment layer (diff scope, breaking->e2e)."`
- `Taskfile.yml:163` — `      - go vet -tags=e2e_process_barrier ./cmd/semstreams ./test/e2e/...`

## 4. The tier table and contract test from PR #1360

The migration checklist and seam pin B's docket names live in `openspec/specs/payload-registry/spec.md`.

- `openspec/specs/payload-registry/spec.md:12` — `The payload registry MUST be the single authority for which `message.Type` keys (`domain.category.version`) exist in a`
- `openspec/specs/payload-registry/spec.md:23` — `That choice generalises to one rule for every E2E tier (owner ruling on #1249, 2026-09-22, amending Q5). An E2E tier boots`
- `openspec/specs/payload-registry/spec.md:31` — `The tier's binary, target and gate are READ from the artifacts that boot it — `build.target` in the tier's compose service,`
- `openspec/specs/payload-registry/spec.md:35` — `| Tier (`task e2e:<tier>`) | Compose service | Target → binary | Gate | E2E-only registrations and hooks | Synthetic types stamped on `entity.create` |`
- `openspec/specs/payload-registry/spec.md:79` — `#### Scenario: every tier's target, binary and gate are the ones its artifacts carry`
- `openspec/specs/payload-registry/spec.md:92` — `- **AND** the test that verifies this is `TestE2ETierTableMatchesComposeAndDockerfile``
- `openspec/specs/payload-registry/spec.md:94` — `#### Scenario: an E2E-only hook cannot reach the production build`
- `openspec/specs/payload-registry/spec.md:102` — `- **AND** the test that verifies this is `TestProductionRootReachesNoE2EHarnessWithoutABuildTag``

The navigation copy (documented as secondary to the spec table) lives in `docs/contributing/02-e2e-tests.md`:

- `docs/contributing/02-e2e-tests.md:188` — `**The source of truth is the tier table in `openspec/specs/payload-registry/spec.md`** — until this change archives,`
- `docs/contributing/02-e2e-tests.md:199` — `| Tier (`task e2e:<tier>`) | Compose file : service | Dockerfile target | Binary |`

The contract test that re-reads the table against the compose files and Dockerfile on every run:

- `test/contract/e2e_tier_binary_contract_test.go:74` — `func tierTable(t *testing.T) (string, []tierRow) {`
- `test/contract/e2e_tier_binary_contract_test.go:266` — `func composeServices(t *testing.T) map[string]composeService {`
- `test/contract/e2e_tier_binary_contract_test.go:315` — `func readDockerfileTargets(t *testing.T) dockerfileTargets {`
- `test/contract/e2e_tier_binary_contract_test.go:428` — `func assertNoComposeFileArmsAnUndeclaredHook(t *testing.T, rows []tierRow) {`
- `test/contract/e2e_tier_binary_contract_test.go:527` — `func TestE2ETierTableMatchesComposeAndDockerfile(t *testing.T) {`
- `test/contract/e2e_tier_binary_contract_test.go:608` — `func TestProductionRootReachesNoE2EHarnessWithoutABuildTag(t *testing.T) {`
- `test/contract/e2e_tier_binary_contract_test.go:676` — `func importsE2EHarness(imports []*ast.ImportSpec) bool {`

## 5. Component start ordering already owned by the framework (context only)

- `service/component_manager.go:488` — `if err := cm.startAllComponents(runtimeCtx); err != nil {`
- `service/component_manager.go:568` — `func (cm *ComponentManager) startAllComponents(ctx context.Context) error {`
- `service/component_manager.go:582` — `func (cm *ComponentManager) startComponentsBarrier(ctx context.Context, names []string) error {`
- `openspec/specs/framework-composition/spec.md:28` — `### Requirement: Composition roots register explicit capability sets`
- `openspec/specs/framework-composition/spec.md:152` — `### Requirement: Component starts form a fail-closed boot barrier`
- `openspec/specs/framework-composition/spec.md:155` — ``ComponentManager.Start` is a component-start barrier: it launches component `Start` calls concurrently, returns`
- `docs/adr/094-boot-only-composition-and-observable-rule-activation.md:35` — `### Boot seals service and component composition`

## 6. Readers of the roots

No package imports `cmd/semstreams` or `cmd/e2e-semstreams` (Go forbids importing `package main`);
`git grep -n "semstreams/cmd/semstreams\|semstreams/cmd/e2e-semstreams" -- '*.go'` finds only importers of the two
`cmd/e2e-semstreams` sub-packages:

- `cmd/e2e-semstreams/fixtures/register_test.go:12` — `	"github.com/c360studio/semstreams/cmd/e2e-semstreams/fixtures"`
- `composition/shipped_configs_test.go:10` — `	"github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"`
- `service/message_logger_census_test.go:13` — `	"github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"`
- `test/predicate_rule_authoring_test.go:10` — `	_ "github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"`

The offline composition verbs from #1107 (`fullComponentRegistry` vs. `Selected(cfg)`):

- `cmd/semstreams/main.go:88` — `	registry, err := fullComponentRegistry()`
- `cmd/semstreams/main.go:100` — `func fullComponentRegistry() (*component.Registry, error) {`
- `cmd/semstreams/main.go:96` — `// fullComponentRegistry registers everything this binary can compose. Boot`
- `cmd/semstreams/main.go:307` — `if graphresearch.Selected(cfg) {`
- `composition/cli/main.go:37` — `func IsVerb(arg string) bool {`
- `composition/cli/main.go:65` — `func Main(args []string, registry *component.Registry, stdout, stderr io.Writer) int {`

Docs that teach composition:

- `docs/basics/05-first-processor.md:126` — `Your composition root must call both registration functions and pass the payload registry to consuming`
- `docs/basics/05-first-processor.md:133` — `[cmd/e2e-semstreams](../../cmd/e2e-semstreams/main.go). Select your own application components and capabilities;`
- `docs/basics/09-building-semsource.md:84` — `composition root. Payload registration is explicit; adding a Go type alone does not register its wire decoder.`
- `docs/concepts/15-payload-registry.md:98` — `processors, `graphresearch`) register directly at each binary's own composition root instead — see`
- `docs/concepts/15-payload-registry.md:99` — ``cmd/e2e-semstreams/main.go`'s `buildPayloadRegistry`. A type registered in one binary's composition root but not`
- `docs/concepts/15-payload-registry.md:289` — `each binary's own composition root (`cmd/semstreams/main.go`'s `registerPayloads`,`
- `docs/concepts/15-payload-registry.md:321` — `**Fix**: Wire the call into `payloadbuiltins.Register` or the binary's composition root (Step 4).`

ADR-094 and ADR-100:

- `docs/adr/094-boot-only-composition-and-observable-rule-activation.md:35` — `### Boot seals service and component composition`
- `docs/adr/094-boot-only-composition-and-observable-rule-activation.md:37` — `Successful boot fixes service and component identity, declaration, dependency, port, and configuration state for the`
- `docs/adr/100-compositions-are-validated-diagrams-are-projections.md:1` — `# ADR-100: Compositions Are Validated; Diagrams Are Projections`

Other in-repo ADRs naming "composition root" as a term of art (found by `git grep -ln "composition root"`):
`docs/adr/058-boot-lifecycle-phases.md`, `docs/adr/075-framework-package-admission-and-composition.md`,
`docs/adr/102-entity-id-segment-semantics.md`, `docs/adr/103-payload-registry-is-the-single-type-authority.md`,
`docs/adr/104-unique-platform-authority.md`.

- `docs/adr/102-entity-id-segment-semantics.md:57` — `the composition root's own identity field`
- `docs/adr/103-payload-registry-is-the-single-type-authority.md:38` — `holds; the composition root derives its contract set from the registry. The string-keyed floor table and the`
- `docs/adr/104-unique-platform-authority.md:143` — `A sister conforms when: its composition root passes `deps.Platform` unchanged; its configuration files declare the`

Related open issues named in the #1301 issue body itself (prose, not re-derived here): #1092/#1089 (declaration-side
composition validation, done), #1103 (docs residual — the "retired registration call" the issue names for
`docs/basics/05` was not reproduced verbatim in the text at `:126`/`:133` read above; the two lines cited there
describe today's `registerPayloads`/`buildPayloadRegistry` split, not a retired call), #1263 (tool-provider seam).
`gh issue view 1107/1108/795` confirm all three are OPEN: #1107 "composition verbs judge the FULL component catalog
while boot registers graph-research/OTEL only when Selected(cfg)"; #1108 "SafeUnmarshal validates before
ApplyDefaults, so 10 of 33 factories refuse {}"; #795 "graph/readiness: package the consumer front door".

## 7. Sister roots (read-only inventory; not modified)

Read at each sister's own HEAD, not a shared timestamp. Local uncommitted modifications existed in semdev and
semspec at read time; every file/line cited below was cross-checked against `git show HEAD:<path>` and, where the
sister's working tree carried an uncommitted diff intersecting the cited region, that diff is noted.

| Sister | HEAD | Composition entry point |
|---|---|---|
| semdev | `ca3956af2ed87d5fa5bdb8183cdb506f7beb7240` | `internal/boot/runtime.go`'s `NewRuntime`/`Run`, invoked from `cmd/semdev/main.go:59` (`boot.Run(ctx, opts)`) |
| semteams | `ce22c961d30014c463a09f8f8a2a90044ee1a1cf` | `cmd/semteams/main.go`'s `run()` |
| semspec | `5a9496eecc453747f4bc557b95444db6304c1420` | `cmd/semspec/main.go`'s `setupInfrastructure`/`run()` |
| semdragon | `07f4de9b65887801ff18a7273d14233023049321` | `cmd/semdragons/main.go` (module `semdragons`, plural) |
| semsage | `4d28b4dc1210f47da84a3031125167d164de9290` | `cmd/semsage/main.go`, pinned to `semstreams v1.0.0-alpha.3` |

Which of the eight steps (vocabulary, components, payloads, graph runtime, personas, tools, lifecycle, services)
each sister performs, and which framework function it calls — measured by `grep -n` for the framework symbol name in
each sister's composition file(s), read-only, no `go list`/`go build` run against any sister module:

| Step | semdev | semteams | semspec | semdragon | semsage |
|---|---|---|---|---|---|
| vocabulary | `internal/vocab.Register()` (own package; `internal/boot/runtime.go:746`) — framework `vocabulary/builtins.Register` NOT called | `vocabbuiltins.Register()` = framework `vocabulary/builtins.Register` aliased (`cmd/semteams/main.go:81`) | 0 hits for `semstreams/vocabulary/builtins`; own `vocabulary/{semspec,source,spec,project,workflow,ics,observability}` packages call the base `semstreams/vocabulary` package directly | 0 hits for any vocabulary registration call | 0 hits |
| components | own `RegisterAll` (`internal/boot/boot.go:78`) wraps framework `componentregistry.Register` at `internal/boot/boot.go:79`, called from `internal/boot/runtime.go:619` | framework `componentregistry.Register` (`cmd/semteams/main.go:777`) | framework `componentregistry.Register` (`cmd/semspec/main.go:552` at HEAD) | own `componentregistry.RegisterAll` (module-local package `github.com/c360studio/semdragons/componentregistry`, `cmd/semdragons/main.go:437`) — framework `componentregistry.Register` NOT imported | 0 hits for `component.Registry`/`componentregistry` at all — no component composition in `cmd/semsage/main.go` |
| payloads | framework `payloadbuiltins.Register` (`internal/boot/runtime.go:624`) | framework `payloadbuiltins.Register` (`cmd/semteams/main.go:873`) | framework `payloadbuiltins.Register` passed as a func value in a registration list (`cmd/semspec/main.go:315` at HEAD) | framework `payloadbuiltins.Register` (`cmd/semdragons/main.go:424`) | 0 hits |
| graph runtime | 0 hits for `WireGraphRuntime`/`WireOwnership` called; `internal/boot/runtime.go:645` comment names `service.WireOwnership(builtinprojection.Contracts()...)` as the framework path a product shell has no access to | framework `service.WireGraphRuntime` (`cmd/semteams/main.go:923`) | 0 hits for `WireGraphRuntime` or `MutationClient` in `cmd/semspec/main.go` | 0 hits for `WireGraphRuntime` | 0 hits |
| personas | framework `persona.LoadFromDirectory` (`internal/boot/runtime.go:484`) | framework `persona.LoadFromDirectory` (`cmd/semteams/main.go:465,479`, base + overlay directories) | own `persona.NewManager` only (`cmd/semspec/persona_substrate.go:47`); `persona.LoadFromDirectory` NOT called | 0 hits for `persona` package | 0 hits |
| tools | own `RegisterTools` (`internal/boot/boot.go:141`) wraps framework `executors.RegisterBuiltins` at `internal/boot/boot.go:142`, called from `internal/boot/runtime.go:652`, with `SkipBuiltins` set for `write_todos` (`internal/boot/runtime.go:645-648`) | framework `executors.RegisterBuiltins` (`cmd/semteams/main.go:291`, inside `setupToolsAndPreprocessor`) | 0 hits for `semstreams/processor/agentic-tools/executors`; semspec's own `tools/register.go` and `tools/*` packages | 0 hits for `agentic-tools/executors`; semdragon exposes tools as components instead (per the issue body) | 0 hits |
| lifecycle | own `RegisterLifecycle` (`internal/boot/boot.go:361`) wraps framework `agentrun.Register` at `internal/boot/boot.go:362`, called from `internal/boot/runtime.go:657` | framework `lifecycle.NewManager` (`cmd/semteams/main.go` — construction near §9c) then `agentrun.Register` (nested in `setupToolsAndPreprocessor`, not re-derived line-by-line here) | framework `lifecycle.NewManager` (`cmd/semspec/main.go:460` at HEAD, aliased through `runlifecycle`/`sessionlifecycle` product workflows); 0 hits for `agentrun.Register` anywhere in the repo | framework `lifecycle.NewManager` (`cmd/semdragons/main.go:200`) registering its own `questlifecycle.WorkflowDeclaration()` (`:201`); 0 hits for `agentrun.Register` | 0 hits for `lifecycle.NewManager` |
| services | framework `service.RegisterAll`/`service.NewServiceManager` (`internal/boot/runtime.go:689,692`) | framework `service.RegisterAll`/`service.NewServiceManager` (`cmd/semteams/main.go` §8) | framework `service.RegisterAll`/`service.NewServiceManager` (`cmd/semspec/main.go:349,354` at HEAD) | framework `service.RegisterAll`/`service.NewServiceManager` (`cmd/semdragons/main.go:446,453`) | framework `service.RegisterAll`/`service.NewServiceManager` (`cmd/semsage/main.go:94,103`) — the ONLY framework composition step semsage performs |

`ConfigureRulePackMutations` — 0 hits in any of the five sisters (`grep -rn "ConfigureRulePackMutations"` against
each sister's tracked `.go` files, `.claude/worktrees` excluded). `graphresearch.` — 0 hits selecting the capability
in any sister (semteams's only hit is a comment naming it as future work, `cmd/semteams/main.go:106`).

None of the five sisters "look alike" (per the issue body): semdev and semteams wrap the framework's eight-step
shape closely (semteams almost verbatim, including the `0.`/`8.`/`9a.`-style numbered boot comments also used in
`cmd/semstreams/main.go`); semdev additionally interposes its own `RegisterAll`/`RegisterTools`/`RegisterLifecycle`
wrapper layer (`internal/boot/boot.go`) between the sister main and the framework calls; semspec and semdragon each
skip several steps outright (graph runtime, in both; vocabulary and tools-as-executors, in both); semsage performs
only the `services` step and touches no `component.Registry` at all.

## 8. Existing capability-like groupings

- `frameworkcapabilities/graphresearch/register.go:478` — `func RegisterComponents(registry *component.Registry) error {`
- `frameworkcapabilities/graphresearch/register.go:502` — `func RegisterPayloads(registry *payloadregistry.Registry) error {`
- `frameworkcapabilities/graphresearch/register_tool.go:32` — `func RegisterTool(ctx context.Context, tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger, bucketName string) error {`
- `frameworkcapabilities/graphresearch/register.go:130` — `func Selected(cfg *config.Config) bool {`
- `frameworkcapabilities/graphresearch/register.go:170` — `func ValidateConfig(cfg *config.Config) error {`
- `frameworkcapabilities/graphresearch/register.go:254` — `func LoopsBucket(cfg *config.Config) string {`

`graphresearch` is the closest existing example: components, payloads, and a tool are registered by three separate
package-level functions (no shared exported type or interface bundles them), plus `Selected`/`ValidateConfig` as
free functions a composition root calls explicitly and in a fixed order (`RegisterComponents` at boot-registry time,
`RegisterPayloads` at payload-registry time, `RegisterTool` at tool-registry time, gated each call site by
`Selected(cfg)`). `git grep -n "type Capability\b\|type Capability interface\|type Capability struct" -- '*.go'` → 0
hits: no exported `Capability` type or interface exists anywhere in the tree today.

- `frameworkadapters/otel/register.go:12` — `func Selected(cfg *config.Config) bool {`
- `frameworkadapters/otel/register.go:25` — `func Register(registry *component.Registry) error {`

`optionalotel` is a smaller instance of the same shape: one `Register` plus one `Selected`, components only (no
payloads, no tools).

- `frameworkcapabilities/rulepacks/validate.go:17` — `func ValidateConfig(cfg *config.Config) error {`

`rulepackcap` (the framework's rule-pack composition validator) has no `Register`/`RegisterComponents` counterpart
at all — it validates rule packs loaded elsewhere, so it is not itself a capability grouping in the same shape.

## Open facts

1. The issue's `docs/basics/05` "retired registration call" residual (attributed to #1103) was not reproduced at the
  lines the issue would most plausibly mean (`:126`, `:133`, read above); a targeted search for what the residual
  actually says (a specific retired function name) was not run — the issue does not name the retired call, and
  `git log -p --follow docs/basics/05-first-processor.md` (searching for a removed registration symbol across its
  history) was NOT RUN.
2. semteams's exact `agentrun.Register` call site and its position relative to `lifecycle.NewManager` inside
  `setupToolsAndPreprocessor` were not individually re-derived line-by-line (the file's own §9c/§9d comment markers
  were used as the order evidence instead of tracing every intermediate call); a fully mechanical re-derivation
  would need `gopls call_hierarchy` inside the semteams module, which was NOT RUN (this pass never ran `go build`/
  `gopls` against any sister module, per the read-only constraint).
3. semspec's actual graph-mutation wiring mechanism (given 0 hits for `WireGraphRuntime` and `MutationClient` in
  `cmd/semspec/main.go`) was not traced to its actual construction site elsewhere in the semspec tree — recorded as
  an absence in the composition file read, not as "semspec has no mutation client anywhere."
4. `gopls implementation` / `gopls references` / `gopls workspace_symbol` were not invoked in this pass; every
  citation above is `git grep -n` plus `sed -n` against the working tree (or `git show HEAD:<path>` for the two
  sisters with uncommitted local changes). If the launching agent needs implementer/caller closure beyond what a
  literal `grep` finds (e.g., every caller of `WireGraphRuntime` through an interface indirection), that pass was
  NOT RUN here.

## Searches

- `gh issue view 1301 --comments` → issue body + 3 comments (symptom, sibling issues #1107/#1108/#795, #1249 cross-reference, scope-bound ruling)
- `gh api repos/C360Studio/semstreams/issues/comments/5777214879` → full #1249 docket comment body
- `gh issue view 1301 --json title,body,number,state,labels,milestone` → full issue body, milestone beta.163
- `git log --oneline --all | grep -i 1360` → 1 (`521e6276` PR #1360 title)
- `sed -n '1,80p' openspec/project.md` → Purpose + Product Boundary read
- `grep -n "inventory:verify" -r Taskfile.yml` → 2 (task defs at :103,:108); `ls docs/proposals/` → full listing
- `cat scripts/inventory-verify.sh` → full grammar read
- `Read docs/proposals/gh1168-federation-identity-inventory.md` (partial, 202/331 lines) → no `base:` line
- `bash scripts/inventory-verify.sh docs/proposals/gh1168-federation-identity-inventory.md` → `BASE missing` (not a valid grammar exemplar)
- `grep -l "^base:" docs/proposals/*.md` → 1 (`gh1168-federation-identity-pins.md`)
- `bash scripts/inventory-verify.sh docs/proposals/gh1168-federation-identity-pins.md` → exit 0, `pins=5 ok=5`
- `Read docs/proposals/gh1168-federation-identity-pins.md` → grammar exemplar confirmed (strict pin bullets under a plain `## ` header; non-pin bullets safe only where they don't start with a backtick, or in `## Adjacent claims`/non-bulleted prose)
- `Read cmd/semstreams/main.go` (full, 838 lines) → every function and call site
- `Read cmd/e2e-semstreams/main.go` (full, 956 lines) → every function and call site
- `wc -l cmd/e2e-semstreams/main.go` → 956; `diff cmd/semstreams/main.go cmd/e2e-semstreams/main.go | grep -c '^[<>]'` → 771; `wc -l cmd/semstreams/main.go cmd/e2e-semstreams/main.go` → 837/956 (+1793 total)
- `ls cmd/semstreams/` → 20 files; `find cmd/e2e-semstreams -type f | sort` → 11 files
- `diff <(sed -n '558,575p' cmd/semstreams/main.go) <(sed -n '737,753p' cmd/e2e-semstreams/main.go)` → 1 line (doc comment only)
- `diff <(sed -n '577,588p' ...) <(sed -n '755,765p' ...)` (configureAndCreateServices) → 1 line (doc comment only)
- `diff <(sed -n '527,556p' ...) <(sed -n '702,735p' ...)` (setupRegistriesAndManager) → 3 hunks, 9 differing lines
- `diff <(sed -n '647,690p' ...) <(sed -n '834,869p' ...)` (runUntilShutdown) → 4 hunks
- `Read cmd/semstreams/process_barrier_e2e.go, process_barrier_disabled.go, slow_consumer_probe_e2e.go, slow_consumer_probe_disabled.go, milestone_probe_e2e.go, milestone_probe_disabled.go` → full read, 6 files
- `git grep -n "go:build" -- '*.go' | grep -E "e2e_process_barrier|e2e_slow_consumer"` → 11; `| wc -l` → 11
- `git grep -n "go:build" -- 'cmd/*.go' 'cmd/**/*.go'` → 10 (9 in cmd/semstreams + 1 unrelated `integration` tag)
- `cat -n docker/Dockerfile` → full read, 215 lines, 4 targets + 3 builder stages identified
- `ls docker/compose/` → 12 files (11 + this listing); `grep -n "target:" docker/compose/*.yml` → 15 hits across 8 files
- `grep -n "target:\|image:\|dockerfile:\|build:" docker/compose/tiered.8b.yml docker/compose/tiered.frontier.yml docker/compose/services.yml` → 0 SemStreams `target:` hits (side-service images only)
- `sed -n '1,115p' docker/compose/e2e.yml` → fixtures profile, port-collision comment, ADR-103 comment
- `grep -n "^  e2e:\|^    e2e" Taskfile.yml` → 18 hits (14 tier tasks + `e2e:tiers`/`e2e:tier`/`e2e:all` + the bare `e2e:` alias)
- `ls taskfiles/` → 10 files + `e2e/` dir; `ls taskfiles/e2e/` → 15 files
- `git grep -n "e2e_process_barrier\|e2e_slow_consumer" -- 'Taskfile.yml' 'taskfiles/*' 'taskfiles/**/*'` → 2 (both in `Taskfile.yml`, lines 152 and 163)
- `grep -rn "lifecycle-seed\|lifecycle_seed\|LIFECYCLE_SEED" taskfiles/ docker/compose/` → 1 (`docker/compose/lifecycle.yml:62`)
- `grep -n "target:\|SEMSTREAMS_E2E_MILESTONE_PROBE\|command:" docker/compose/agentic.yml docker/compose/e2e-slow-consumer.yml docker/compose/e2e.yml docker/compose/lifecycle.yml` → 13 hits
- `cat -n cmd/e2e-semstreams/fixtures/register.go` → full read, 84 lines
- `cat -n cmd/e2e-semstreams/mission/command.go | head -80` → partial read
- `grep -n "^func \|^func(" cmd/e2e-semstreams/mission/command.go cmd/e2e-semstreams/mission/state.go` → 26 functions
- `find test/e2e/harness/processbarrier -type f`, `.../milestoneprobe -type f`, `.../lessoncuration -type f`, `find internal/e2eslowconsumer -type f` → 2, 7 (incl. fuzz corpus), 4, 3 files
- `grep -n "^func \|^const \|EnvVar\|ToolName" test/e2e/harness/processbarrier/processbarrier.go test/e2e/harness/milestoneprobe/milestoneprobe.go test/e2e/harness/lessoncuration/handler.go test/e2e/harness/lessoncuration/contract.go internal/e2eslowconsumer/probe_e2e.go internal/e2eslowconsumer/contract.go` → 27 hits
- `git log --oneline -1 -- docs/contributing/02-e2e-tests.md` → 1 (`521e6276`, same commit as PR #1360)
- `grep -n "^|" docs/contributing/02-e2e-tests.md` → 20 table rows
- `sed -n '180,235p' docs/contributing/02-e2e-tests.md` → full tier table + notes read
- `find . -name "e2e_tier_binary_contract_test.go"` → 1; `wc -l` → 739
- `grep -n "^#\|Tier\|tier table\|E2E-only" openspec/specs/payload-registry/spec.md` → 22 hits
- `sed -n '1,105p' openspec/specs/payload-registry/spec.md` → full requirement + tier table + first 3 scenarios read
- `grep -n "^func Test\|^func " test/contract/e2e_tier_binary_contract_test.go` → 24 functions
- `sed -n '475,495p;560,585p' service/component_manager.go` → barrier code read
- `find openspec/specs -iname "*composition*"` → 3 dirs (`composition-validation`, `service-composition`, `framework-composition`)
- `grep -n "^#\|provider-first\|barrier" openspec/specs/framework-composition/spec.md` → 24 headings
- `sed -n '28,65p' openspec/specs/framework-composition/spec.md` → capability-set requirement read
- `ls frameworkcapabilities/graphresearch/`, `grep -n "^func \|^type " frameworkcapabilities/graphresearch/*.go` (excl. `_test.go`) → 20 symbols
- `ls frameworkadapters/otel/`, `grep -n "^func \|^type " frameworkadapters/otel/*.go` (excl. `_test.go`) → 2 symbols
- `git grep -n "type Capability\b\|type Capability interface\|type Capability struct" -- '*.go'` → 0
- `for f in componentregistry payloadbuiltins vocabulary/builtins persona; do grep -n "^func Register\|^func LoadFromDirectory" $f/*.go; done` → 4 hits
- `grep -n "^func Register\|^func WireGraphRuntime\|^func ConfigureRulePackMutations" service/*.go` → 4 hits
- `grep -n "^func Register\b" agentic/agentrun/*.go` → 1; `grep -n "^func RegisterBuiltins\b" processor/agentic-tools/executors/*.go` → 1
- `sed -n '1,95p' processor/agentic-tools/executors/register.go` → `ToolDependencies` struct + Pattern-B comments read
- `grep -n "Pattern-B step" processor/agentic-tools/executors/register.go` → 4 hits
- `grep -rn "list_components" processor/agentic-tools/executors/*.go | grep -v _test` → 3 hits
- `grep -n "^func Test" cmd/semstreams/*_test.go cmd/e2e-semstreams/*_test.go cmd/e2e-semstreams/mission/*_test.go cmd/e2e-semstreams/fixtures/*_test.go` → 26 test functions across both roots
- `git grep -n "semstreams/cmd/semstreams\|semstreams/cmd/e2e-semstreams" -- '*.go' | grep -v "^cmd/e2e-semstreams/main.go\|^cmd/semstreams/"` → 4 (fixtures ×1, mission ×3)
- `ls composition/cli/`, `grep -n "^func \|^type " composition/cli/*.go` (excl. `_test.go`) → 6 symbols
- `ls docs/basics/` → 10 files; `grep -n "RegisterBuiltins\|componentregistry.Register\|payloadbuiltins.Register\|WireGraphRuntime\|composition root\|main.go" docs/basics/05*.md docs/basics/07*.md` → 3 hits
- `sed -n '115,140p' docs/basics/05-first-processor.md`, `sed -n '370,395p' docs/basics/07-agentic-quickstart.md` → both read
- `grep -n "flow\|Flow\|template\|Pattern-B\|persona\|ConfigManager" docs/basics/05-first-processor.md docs/basics/07-agentic-quickstart.md` → 7 hits, none matching a nameable "retired call"
- `git grep -ln "composition root\|composition roots" -- 'docs/**/*.md' 'docs/*.md'` → 30 files
- `grep -n "composition root\|composition roots" docs/basics/09-building-semsource.md docs/adr/058*.md docs/adr/075*.md docs/adr/102*.md docs/adr/103*.md docs/adr/104*.md docs/concepts/15-payload-registry.md docs/operations/27*.md` → 15 hits
- `ls docs/adr/ | grep -E "^094|^100"` → 2 files
- `grep -n "^#\|composition root\|boot seal\|seals composition" docs/adr/094*.md` → 10 headings; `grep -n "^#\|composition root" docs/adr/100*.md` → 9 headings
- `sed -n '33,58p' docs/adr/094-boot-only-composition-and-observable-rule-activation.md` → boot-seals-composition text read
- `ls composition/` → 15 files; `grep -n "^func \|^type " composition/*.go` (excl. `_test.go`) → 18 symbols
- `for d in semdev semteams semspec semdragon semsage; do git -C ... rev-parse HEAD; git -C ... status --short | head -3; done` → 5 HEAD shas, semdev+semspec dirty
- `find /Users/coby/Code/c360/semdev -path "*internal/boot/boot.go"` → 1; `find .../semteams -maxdepth 2 -name main.go -path "*cmd/semteams*"` → 0 (too shallow); `find .../semteams -maxdepth 3 -iname main.go` → 2; same pattern for semspec (6 mains) and semsage (1)
- `find /Users/coby/Code/c360/semdragon -path "*componentregistry/register.go"` → 6 (1 real + 5 `.claude/worktrees` snapshots, excluded from all subsequent semdragon reads)
- semdev: `wc -l internal/boot/boot.go` → 366; `grep -n "^func \|Register(\|RegisterBuiltins\|WireGraphRuntime\|LoadFromDirectory\|ConfigureRulePackMutations\|builtins.Register\|componentregistry\|payloadregistry\|lifecycle.NewManager\|agentrun.Register" internal/boot/boot.go` → 12 hits
- semdev: `grep -rln "boot.RegisterAll\|boot.RegisterTools\|boot.RegisterLifecycle" cmd/` → 0 (wrong grep target); `grep -n "internal/boot\|boot\." cmd/semdev/main.go` → 8 hits incl. `boot.Run`
- semdev: `ls internal/boot/`, `grep -rn "^func Run(\|^func RunOptions\|^type RunOptions" internal/boot/*.go` → `runtime.go:110,907`
- semdev: `wc -l internal/boot/runtime.go` → 924; `grep -n "builtins\.Register\|componentregistry\.Register\|payloadbuiltins\.Register\|WireGraphRuntime\|persona\.LoadFromDirectory\|ConfigureRulePackMutations\|RegisterBuiltins\|agentrun\.Register\|RegisterAll(\|RegisterTools(\|RegisterLifecycle(\|graphresearch\." internal/boot/runtime.go` → 12 hits
- semdev: `sed -n '605,660p' internal/boot/runtime.go` → SkipBuiltins/write_todos rationale read
- semdev: `grep -rn "os\.WriteFile\|WireGraphRuntime" internal/` → confirms 0 for `WireGraphRuntime`; `grep -rn "semstreams/vocabulary/builtins" internal/ cmd/` → 0
- semdev: `grep -n "vocab \"" internal/boot/runtime.go` → 0 (wrong quote form); `grep -n "vocab" internal/boot/runtime.go` → 5, resolved to `internal/vocab` at line 61
- semdev: `grep -rln "WireGraphRuntime" --include="*.go" .` (repo-wide, worktrees excluded) → 0
- semdev: `grep -rn "ConfigureRulePackMutations\|NewMilestoneSubscriber\|NewServiceManager\|StartAll(\|ComponentManager\b" internal/boot/runtime.go` → 4 hits, none matching `ConfigureRulePackMutations` or `NewMilestoneSubscriber`
- semteams: `wc -l cmd/semteams/main.go` → 1100; `grep -n "builtins.Register|componentregistry.Register|payloadbuiltins.Register|WireGraphRuntime|persona.LoadFromDirectory|RegisterBuiltins|agentrun.Register|ConfigureRulePackMutations|NewMilestoneSubscriber|graphresearch\.|lifecycle.NewManager|service.NewServiceManager|service.RegisterAll|componentregistry\"" cmd/semteams/main.go` → 19 hits
- semteams: `grep -n "vocabbuiltins " cmd/semteams/main.go` → 1 (import alias); `grep -n "graphresearch" cmd/semteams/main.go` → 1 (comment only)
- semteams: `sed -n '74,235p' cmd/semteams/main.go | grep -n "^\s*//\s*[0-9]+[.a-z]*\.|vocabbuiltins.Register|componentregistry|payloadReg|WireGraphRuntime|persona.LoadFromDirectory|RegisterBuiltins|agentrun.Register|ConfigureRulePackMutations|setupRegistriesAndManager|createServiceDependencies|setupTools"` → 19 hits, numbered-comment order confirmed (0, 1, 2, 2.5, 3–12, 9a–9h, 11b)
- semspec: `wc -l cmd/semspec/main.go` → 1088 (dirty); `grep -n "builtins.Register|componentregistry.Register|payloadbuiltins.Register|WireGraphRuntime|persona.LoadFromDirectory|RegisterBuiltins|agentrun.Register|ConfigureRulePackMutations|NewMilestoneSubscriber|graphresearch\.|lifecycle.NewManager|service.NewServiceManager|service.RegisterAll\b" cmd/semspec/main.go` → 5 hits
- semspec: `git diff --stat cmd/semspec/main.go` → 9 lines changed, unrelated to composition-order lines cited; `git show HEAD:cmd/semspec/main.go > /tmp/semspec_main_head.go` then re-grepped → same 5 hits at HEAD
- semspec: `grep -n "semstreams/persona|semstreams/processor/agentic-tools/executors|semstreams/agentic/agentrun|semstreams/vocabulary/builtins|semstreams/pkg/lifecycle" /tmp/semspec_main_head.go` → 1 (`pkg/lifecycle` only)
- semspec: `grep -rln "semstreams/persona\"|semstreams/processor/agentic-tools/executors\"|semstreams/agentic/agentrun\"|semstreams/vocabulary/builtins\"" --include="*.go" .` (worktrees excluded) → 2 files (`cmd/semspec/persona_substrate.go`, `prompt/substrate_personas.go`)
- semspec: `grep -n "semstreams/persona|persona.LoadFromDirectory|^func " cmd/semspec/persona_substrate.go` then `git show HEAD:... | grep -n "persona\."` → confirms `persona.NewManager` only, no `LoadFromDirectory`
- semspec: `grep -rln "semstreams/processor/agentic-tools\""` → 12 files, all semspec's own `tools/*`; `grep -rln "semstreams/agentic/agentrun"` → 0; `grep -rln "semstreams/vocabulary\""` → 0 (only semspec's OWN `vocabulary/*` importing base `semstreams/vocabulary`, confirmed via `grep -n "semstreams/vocabulary" vocabulary/semspec/predicates.go` → 1)
- semspec: `grep -rln "semstreams/vocabulary/builtins"` → 0; `grep -rln "ConfigureRulePackMutations"` → 0; `grep -rln "WireGraphRuntime"` → 0; `grep -n "MutationClient|projection\." /tmp/semspec_main_head.go` → 0
- semdragon: `ls componentregistry/`, `wc -l componentregistry/register.go` → 247; `grep -n "^func |builtins.Register|componentregistry.Register|payloadbuiltins.Register|WireGraphRuntime|persona.LoadFromDirectory|RegisterBuiltins|agentrun.Register|ConfigureRulePackMutations" componentregistry/register.go` → 4 hits, all its own `RegisterAll`/`RegisterProcessors`/`ComponentNames`/`ProcessorNames`
- semdragon: `find . -maxdepth 2 -iname main.go` (worktrees excluded) → 0; `find . -iname main.go -not -path "*/.claude/*"` → 5
- semdragon: `wc -l cmd/semdragons/main.go` → 607; `grep -n "semstreams/vocabulary/builtins|semstreams/componentregistry\"|semstreams/payloadbuiltins|semstreams/persona\"|semstreams/agentic/agentrun\"|semstreams/processor/agentic-tools/executors\"|WireGraphRuntime|ConfigureRulePackMutations|componentregistry\.Register|payloadbuiltins\.Register|builtins\.Register|persona\.LoadFromDirectory|executors\.RegisterBuiltins|agentrun\.Register" cmd/semdragons/main.go` → 2 hits
- semdragon: `grep -n "\"github.com/c360studio/semdragon/componentregistry\"|\"github.com/c360studio/semstreams/componentregistry\"" cmd/semdragons/main.go` → 0; `sed -n '1,40p' ... | grep -n "componentregistry|c360studio"` → resolved to `github.com/c360studio/semdragons/componentregistry` (own package)
- semdragon: `grep -n "lifecycle.NewManager|WireGraphRuntime|questlifecycle\.|\.Register(mgr|\.Register(lifecycleM|service.NewServiceManager|service.RegisterAll" cmd/semdragons/main.go` → 4 hits
- semsage: `wc -l cmd/semsage/main.go` → 331; `grep -n "semstreams/vocabulary/builtins|semstreams/componentregistry\"|semstreams/payloadbuiltins|semstreams/persona\"|semstreams/agentic/agentrun\"|semstreams/processor/agentic-tools/executors\"|WireGraphRuntime|ConfigureRulePackMutations|componentregistry\.Register|payloadbuiltins\.Register|builtins\.Register|persona\.LoadFromDirectory|executors\.RegisterBuiltins|agentrun\.Register|lifecycle.NewManager|service.NewServiceManager|service.RegisterAll" cmd/semsage/main.go` → 2 hits (`service.RegisterAll`, `service.NewServiceManager` only)
- semsage: `grep -rln "semstreams/componentregistry\"|semstreams/payloadbuiltins\"|semstreams/vocabulary/builtins\"|semstreams/persona\"|semstreams/agentic/agentrun\"|semstreams/processor/agentic-tools/executors\""` (whole repo, worktrees excluded) → 0
- semsage: `grep -n "component.Registry|component.NewRegistry|WireGraphRuntime|MutationClient" cmd/semsage/main.go` → 0; `grep "semstreams" go.mod` → 1 (`v1.0.0-alpha.3`)
- `gh pr list --search "1301" --state all --json number,title,state` → 6 (incl. claim PR #1390, no competing claim)
- `gh issue list --search "composition root" --state open --json number,title` → 12, #1301 itself the top hit
- `ls openspec/changes/ | grep -i "composition\|1301"` → 0
- `gh issue view 1107/1108/795 --json number,title,state` → all 3 OPEN
- `grep -n "LifecycleSeed\b" cmd/e2e-semstreams/main.go` → 5 hits

Pin count: 257 pin bullets across sections 1–8 (every line matching the `` - `path:line` — `text` `` grammar);
`task inventory:verify` output is the authority for the exact machine count.

// Package main implements the entry point for the SemStreams application.
// SemStreams is a semantic stream processing framework that combines
// protocol-level data processing with semantic knowledge graph capabilities.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	_ "net/http/pprof" // Register pprof handlers on DefaultServeMux (served by service.MaybeStartPProf)
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	compositioncli "github.com/c360studio/semstreams/composition/cli"
	"github.com/c360studio/semstreams/config"
	optionalotel "github.com/c360studio/semstreams/frameworkadapters/otel"
	"github.com/c360studio/semstreams/frameworkcapabilities/graphresearch"
	rulepackcap "github.com/c360studio/semstreams/frameworkcapabilities/rulepacks"
	"github.com/c360studio/semstreams/internal/bootstrapobservability"
	"github.com/c360studio/semstreams/internal/maxdelivery"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/persona"
	shutdownerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
	rulepkg "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
	"github.com/c360studio/semstreams/vocabulary/builtins"
)

const appName = "semstreams"

var (
	// Version is the semantic version, replaced with release metadata via -ldflags.
	Version = "0.1.0"
	// GitCommit is the source revision, replaced with release metadata via -ldflags.
	GitCommit = "unknown"
	// BuildTime is the build timestamp, replaced with release metadata via -ldflags.
	BuildTime = "dev"
)

func main() {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			_, _ = fmt.Fprintf(os.Stderr, "PANIC: %v\nStack trace:\n%s\n", r, string(buf[:n]))
			os.Exit(2)
		}
	}()

	// Composition verbs (catalog, validate <config>, graph <config>) serve
	// the catalog this binary can compose and exit; no NATS, no banner.
	if code, handled := dispatchCompositionVerb(os.Args[1:]); handled {
		os.Exit(code)
	}

	// Run application with proper error handling
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// dispatchCompositionVerb serves a composition verb against the full catalog
// this binary can compose (core, graph-research, OTEL) and reports whether the
// arguments named one.
func dispatchCompositionVerb(args []string) (int, bool) {
	if len(args) == 0 || !compositioncli.IsVerb(args[0]) {
		return 0, false
	}
	builtins.Register()
	registry, err := fullComponentRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1, true
	}
	return compositioncli.Main(args, registry, os.Stdout, os.Stderr), true
}

// fullComponentRegistry registers everything this binary can compose. Boot
// gates graph-research and OTEL on the configuration (setupRegistriesAndManager);
// the catalog and the offline validator judge against the full set so a
// configuration that selects either capability validates the same way.
func fullComponentRegistry() (*component.Registry, error) {
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		return nil, fmt.Errorf("register components: %w", err)
	}
	if err := graphresearch.RegisterComponents(registry); err != nil {
		return nil, fmt.Errorf("register graph research components: %w", err)
	}
	if err := optionalotel.Register(registry); err != nil {
		return nil, fmt.Errorf("register optional OTEL adapter: %w", err)
	}
	return registry, nil
}

//revive:disable-next-line:function-length // Keep process ownership and boot ordering visible in one composition root.
func run() (runErr error) {
	// Register first-party semantic names before config/rule/workflow
	// validation. Import side effects are not an authoring contract.
	builtins.Register()

	// 1. Print banner
	printBanner()

	// 2. Parse and validate CLI flags
	cliCfg, shouldExit, err := parseCLI()
	if shouldExit || err != nil {
		return err
	}

	// 2.5. Start pprof server if debug mode enabled (before NATS - independent).
	// ADR-058 step 4: shared helper, deliberately NOT a Service (see
	// service.MaybeStartPProf) — kept early so a wedged boot stays profilable.
	service.MaybeStartPProf(cliCfg.Debug, cliCfg.DebugPort)

	// 3. Load and validate configuration
	cfg, err := loadConfig(cliCfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	if err := rulepackcap.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("invalid rule-pack composition: %w", err)
	}
	if err := graphresearch.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("invalid capability composition: %w", err)
	}

	if cliCfg.Validate {
		// --validate is an alias of the `validate <config>` verb: the same
		// composition findings, printed the same way, non-zero on errors.
		registry, err := fullComponentRegistry()
		if err != nil {
			return err
		}
		if code := compositioncli.Main(
			[]string{compositioncli.VerbValidate, cliCfg.ConfigPath}, registry, os.Stdout, os.Stderr,
		); code != compositioncli.ExitOK {
			return fmt.Errorf("composition validation exited %d", code)
		}
		return nil
	}

	// 4. Build all local observability dependencies before the NATS client can
	// capture them. The client/config-manager graphs remain non-forwarding.
	metricsRegistry, phaseLogging, err := bootstrapobservability.NewProductionPhaseA(
		os.Stdout, cliCfg.LogLevel, cliCfg.LogFormat,
		[]slog.Attr{
			slog.String("service", "semstreams"),
			slog.String("version", Version),
			slog.Int("pid", os.Getpid()),
		},
	)
	if err != nil {
		return fmt.Errorf("compose phase-A logger: %w", err)
	}
	slog.SetDefault(phaseLogging.Process)

	// 5. Observe shutdown before the first context-aware NATS root acquisition. Boot
	// cancellation stays separate from the live runtime authority used by
	// continuing owners during ordered shutdown.
	runtimeCtx := context.Background()
	bootCtx, stopSignals := signal.NotifyContext(runtimeCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// NATS is required. Publish the inert client to root ownership before
	// connection/readiness so every partial acquisition reaches bounded cleanup.
	natsClient, err := createNATSClient(cfg, phaseLogging.Client, metricsRegistry)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	rootResources := &semstreamsRootResources{natsClient: natsClient}
	defer rootResources.abortOnReturn(cliCfg.ShutdownTimeout, &runErr)
	if err := connectNATSWithSpinner(bootCtx, natsClient, phaseLogging.Client); err != nil {
		return err
	}

	// 6. Complete config arbitration before any final composition decision.
	if err := bootCtx.Err(); err != nil {
		return fmt.Errorf("bootstrap canceled before config manager start: %w", err)
	}
	configManager, effectiveConfig, err := bootstrapobservability.StartValidatedConfigManager(
		runtimeCtx, cfg, natsClient, phaseLogging.ConfigManager,
	)
	if err != nil {
		return err
	}
	rootResources.configManager = configManager
	if err := bootCtx.Err(); err != nil {
		return fmt.Errorf("bootstrap canceled after config manager start: %w", err)
	}
	// 7. Effective streams, including LOGS, exist before forwarding is built.
	if err := ensureStreamsWithSpinner(bootCtx, effectiveConfig, natsClient, phaseLogging.ConfigManager); err != nil {
		return err
	}

	forwardingHandler, err := bootstrapobservability.NewForwardingHandler(
		effectiveConfig.Services, natsClient, phaseLogging.Process,
	)
	if err != nil {
		return err
	}
	logger := phaseLogging.Steady(forwardingHandler)
	slog.SetDefault(logger)
	if err := bootCtx.Err(); err != nil {
		return fmt.Errorf("bootstrap canceled before MaxDeliver observer start: %w", err)
	}
	rootResources.stopMaxDeliveryObserver, err = maxdelivery.Start(runtimeCtx, natsClient, metricsRegistry, logger)
	if err != nil {
		return fmt.Errorf("start MaxDeliver observer: %w", err)
	}
	if err := bootCtx.Err(); err != nil {
		return fmt.Errorf("bootstrap canceled after MaxDeliver observer start: %w", err)
	}

	slog.Info("SemStreams bootstrap infrastructure initialized",
		"version", Version,
		"git_commit", GitCommit,
		"build_time", BuildTime)

	// All remaining composition consumes post-arbitration desired state.
	cfg = effectiveConfig
	platform := extractPlatformMeta(cfg)
	logger.Info("Platform identity configured",
		"org", platform.Org,
		"platform", platform.Platform,
		"environment", cfg.Platform.Environment)

	// 8. Setup registries and manager
	componentRegistry, manager, err := setupRegistriesAndManager(cfg)
	if err != nil {
		return err
	}

	// 9a. Build the shared payload registry and register builtins. Done
	// BEFORE the tools registry so any boot path that needs payload
	// resolution (e.g., a stateful tool unmarshaling stored messages)
	// has the registry available. Mirrors the tools-registry shape
	// shipped in beta.16. See registerPayloads for the example-processor
	// migration (post-beta.18 payload-registry singleton retirement).
	payloadReg, err := registerPayloads(cfg)
	if err != nil {
		return err
	}

	lifecycleManager := lifecycle.NewManager(natsClient, logger)
	// The registry is the one table of framework contracts (ADR-103).
	mutationClient, err := service.WireGraphRuntime(
		bootCtx, natsClient, logger, payloadReg.Contracts()...,
	)
	if err != nil {
		return fmt.Errorf("wire graph runtime: %w", err)
	}

	// 9b. Build the shared tool registry and register builtins. Done
	// BEFORE creating service dependencies so the registry is available
	// to component construction via deps.ToolRegistry. Pattern-B
	// managers (rule, flow, persona, flow-template) are built here too;
	// stateful tools that need them resolve at registration.
	personaMgr := buildPersonaManagerConcrete(natsClient, logger)
	if personaMgr != nil {
		if err := persona.LoadFromDirectory(bootCtx, "configs/personas/fragments", personaMgr, logger); err != nil {
			logger.Warn("persona file loader encountered errors", slog.Any("error", err))
		}
	}
	toolRegistry := agentictools.NewExecutorRegistry()
	if err := executors.RegisterBuiltins(bootCtx, toolRegistry, executors.ToolDependencies{
		NATSClient:              natsClient,
		MutationClient:          mutationClient,
		Platform:                platform,
		Logger:                  logger,
		RuleManager:             buildRuleManager(bootCtx, natsClient, configManager, logger),
		PersonaManager:          personaMgr,
		ComponentRegistry:       componentRegistry,
		LoopsBucket:             graphresearch.LoopsBucket(cfg),
		RestrictedDecideActions: extractRestrictedDecideActions(cfg, logger),
	}); err != nil {
		return fmt.Errorf("register builtin tools: %w", err)
	}
	if graphresearch.Selected(cfg) {
		if err := graphresearch.RegisterTool(bootCtx, toolRegistry, natsClient, platform, logger, graphresearch.LoopsBucket(cfg)); err != nil {
			return fmt.Errorf("register graph research tool: %w", err)
		}
	}

	// 10. Create service dependencies
	svcDeps := createServiceDependencies(natsClient, metricsRegistry, logger, platform, configManager, componentRegistry)
	svcDeps.ToolRegistry = toolRegistry
	svcDeps.PayloadRegistry = payloadReg

	// 10b. Build the shared Lifecycle harness Manager (ADR-047) and
	// plumb it into service dependencies. The framework binary
	// itself registers no Participant workflows — that's app-side
	// responsibility — but the Manager is always available so the
	// rule processor's lifecycle_* actions + lifecycle-gateway
	// can operate the moment an app does call Manager.Register.
	// This matches the wiring discipline from
	// [[feedback_verify_main_go_wire_for_sister_asks]] —
	// half-migrated framework binaries silently break workflows.
	svcDeps.LifecycleManager = lifecycleManager

	// 10c. Register the agent-run workflow (ADR-053 D2). Must come after
	// the Manager is constructed so lifecycle_* actions that reference
	// "agent-run" entities resolve at rule-evaluation time, not at boot.
	// The framework binary registers no product-level handlers — product
	// code (semteams, etc.) adds MilestoneHandlers via AddHandler.
	if err := agentrun.Register(svcDeps.LifecycleManager); err != nil {
		return fmt.Errorf("register agent-run workflow: %w", err)
	}

	// 10d. ADR-058 Phase B — agent-run milestone subscriber (ADR-053 D6) under the
	// ServiceManager's ordered shutdown. Subscribes to agent.complete.* /
	// agent.failed.*, pre-resolves the run, and fans out to registered product
	// handlers (none in the framework binary). Lifecycle terminal mutations remain
	// coordinator/component work through declared ports. Registered before
	// component services so StopAll stops it after their event publishers. Its Start
	// can abort boot on a genuine consumer-start failure;
	// the stream-absent case graceful-skips inside the subscriber (gh#246).
	if err := manager.RegisterInstance("milestone", service.NewMilestoneService(
		agentrun.NewMilestoneSubscriber(
			svcDeps.LifecycleManager,
			agentrun.NewNATSLoopTripleReader(natsClient),
			platform.Org,
			platform.Platform,
			logger,
		),
		natsClient,
		agentrun.StartConfig{StreamName: agentrun.AgentStreamName},
		logger,
	)); err != nil {
		return fmt.Errorf("register milestone service: %w", err)
	}

	// 11. Configure and create services
	if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {
		return err
	}

	// 11b. Validate each rule pack's local projection-contract composition. Done
	// HERE (after the rule processors are
	// constructed by configureAndCreateServices) and BEFORE the signal-handling
	// runtime path calls StartAll. Pack contracts are read ONCE, statically — this is
	// the ONLY rule-pack bind site, and it must NEVER be called from the
	// hot-reload path. Every composition, overlap, and mutation-client injection
	// error is a boot gate. Repeated binding reaches the processor's one-time
	// injection guard.
	if err := service.ConfigureRulePackMutations(manager); err != nil {
		return fmt.Errorf("validate rule-pack composition: %w", err)
	}

	// 12. Admit the fixed boot composition only while shutdown remains absent.
	return runUntilShutdown(
		runtimeCtx, bootCtx.Done(), manager, cliCfg.ShutdownTimeout, cliCfg.HealthPort, rootResources.close,
	)
}

type semstreamsRootResources struct {
	natsClient              *natsclient.Client
	configManager           *config.Manager
	stopMaxDeliveryObserver func(context.Context) error
	closeAttempted          bool
}

func (r *semstreamsRootResources) close(ctx context.Context) error {
	var closeErr error
	if r.stopMaxDeliveryObserver != nil {
		closeErr = errors.Join(closeErr, shutdownerrs.NewShutdownError(
			appName+"/max-delivery", shutdownerrs.PhaseDrainConsumers, r.stopMaxDeliveryObserver(ctx),
		))
	}
	if r.configManager != nil {
		closeErr = errors.Join(closeErr, stopWithinShutdownBudget(ctx, r.configManager.Stop))
	}
	r.closeAttempted = true
	return errors.Join(closeErr, shutdownerrs.NewShutdownError(
		appName, shutdownerrs.PhaseCloseTransport, r.natsClient.Close(ctx),
	))
}

func (r *semstreamsRootResources) abortOnReturn(timeout time.Duration, runErr *error) {
	if r.closeAttempted {
		return
	}
	abortCtx, abortCancel := context.WithTimeout(context.Background(), timeout)
	defer abortCancel()
	*runErr = errors.Join(*runErr, r.close(abortCtx))
}

// parseCLI parses and validates CLI flags.
func parseCLI() (*CLIConfig, bool, error) {
	cliCfg := parseFlags()
	if err := validateFlags(cliCfg); err != nil {
		return nil, false, fmt.Errorf("invalid flags: %w", err)
	}

	if cliCfg.ShowVersion {
		fmt.Printf("%s version %s\n", appName, Version)
		return nil, true, nil
	}

	if cliCfg.ShowHelp {
		printHelp()
		return nil, true, nil
	}

	return cliCfg, false, nil
}

// connectNATSWithSpinner connects an already-owned NATS client with a spinner
// for user feedback.
// NATS is a hard requirement - semstreams cannot operate without it.
func connectNATSWithSpinner(
	ctx context.Context,
	natsClient *natsclient.Client,
	logger *slog.Logger,
) error {
	spinner := NewSpinner("Connecting to NATS...")
	spinner.Start()

	if err := bootstrapobservability.ConnectClient(ctx, natsClient, logger); err != nil {
		spinner.StopWithError(err)
		return err
	}
	if err := runSlowConsumerProbe(ctx, natsClient); err != nil {
		spinner.StopWithError(err)
		return fmt.Errorf("run slow-consumer E2E probe: %w", err)
	}

	spinner.Stop()
	return nil
}

// ensureStreamsWithSpinner creates JetStream streams with a spinner for user feedback.
func ensureStreamsWithSpinner(
	ctx context.Context,
	cfg *config.Config,
	natsClient *natsclient.Client,
	logger *slog.Logger,
) error {
	spinner := NewSpinner("Creating JetStream streams...")
	spinner.Start()

	if err := bootstrapobservability.EnsureEffectiveStreams(ctx, cfg, natsClient, logger); err != nil {
		spinner.StopWithError(err)
		return err
	}

	spinner.Stop()
	return nil
}

// createNATSClient creates a NATS client from config.
func createNATSClient(
	cfg *config.Config,
	logger *slog.Logger,
	metricsRegistry *metric.MetricsRegistry,
) (*natsclient.Client, error) {
	natsURLs := "nats://localhost:4222"

	// Environment variable override takes precedence
	if envURL := os.Getenv("SEMSTREAMS_NATS_URLS"); envURL != "" {
		natsURLs = envURL
	} else if len(cfg.NATS.URLs) > 0 {
		natsURLs = strings.Join(cfg.NATS.URLs, ",")
	}

	return bootstrapobservability.NewClient(natsURLs, logger, metricsRegistry)
}

// extractRestrictedDecideActions reads the deployment-level decide-action
// restriction policy (gh#239) from the agentic-tools component config. The
// decide tool bars these action names for every coordinator task (front-
// door and rule-spawned); empty means the permissive default. Mirrors
// graphresearch.LoopsBucket — the agentic-tools Config field is the operator/schema
// surface; this bridges it into the boot-time ToolDependencies.
func extractRestrictedDecideActions(cfg *config.Config, logger *slog.Logger) []string {
	for _, cc := range cfg.Components {
		if cc.Name != "agentic-tools" || !cc.Enabled {
			continue
		}
		var tcfg struct {
			RestrictedDecideActions []string `json:"restricted_decide_actions"`
		}
		if err := json.Unmarshal(cc.Config, &tcfg); err != nil {
			// Don't silently fall back to permissive: a malformed policy
			// disables a guard the operator explicitly asked for. The
			// typed component Config unmarshal backstops genuine type
			// errors, but warn here so the security-adjacent gate's
			// non-enforcement is never silent.
			logger.Warn("could not parse agentic-tools restricted_decide_actions; decide-action restriction disabled (permissive default)",
				slog.Any("error", err))
			continue
		}
		if len(tcfg.RestrictedDecideActions) > 0 {
			return tcfg.RestrictedDecideActions
		}
	}
	return nil
}

// extractPlatformMeta extracts platform identity from config.
func extractPlatformMeta(cfg *config.Config) types.PlatformMeta {
	platformID := cfg.Platform.InstanceID
	if platformID == "" {
		platformID = cfg.Platform.ID
	}

	return types.PlatformMeta{
		Org:      cfg.Platform.Org,
		Platform: platformID,
	}
}

// setupRegistriesAndManager creates registries and service manager
func setupRegistriesAndManager(cfg *config.Config) (*component.Registry, *service.Manager, error) {
	componentRegistry := component.NewRegistry()
	slog.Debug("Registering core component factories (UDP, WebSocket, parsers)")
	if err := componentregistry.Register(componentRegistry); err != nil {
		return nil, nil, fmt.Errorf("register components: %w", err)
	}
	if graphresearch.Selected(cfg) {
		if err := graphresearch.RegisterComponents(componentRegistry); err != nil {
			return nil, nil, fmt.Errorf("register graph research components: %w", err)
		}
	}
	if optionalotel.Selected(cfg) {
		if err := optionalotel.Register(componentRegistry); err != nil {
			return nil, nil, fmt.Errorf("register optional OTEL adapter: %w", err)
		}
	}

	factories := componentRegistry.ListFactories()
	slog.Info("Core component factories registered", "count", len(factories), "factories", factories)

	serviceRegistry := service.NewServiceRegistry()
	if err := service.RegisterAll(serviceRegistry); err != nil {
		return nil, nil, fmt.Errorf("register services: %w", err)
	}

	manager := service.NewServiceManager(serviceRegistry)

	return componentRegistry, manager, nil
}

// createServiceDependencies creates the Dependencies struct for services
func createServiceDependencies(
	natsClient *natsclient.Client,
	metricsRegistry *metric.MetricsRegistry,
	logger *slog.Logger,
	platform types.PlatformMeta,
	configManager *config.Manager,
	componentRegistry *component.Registry,
) *service.Dependencies {
	return &service.Dependencies{
		NATSClient:        natsClient,
		MetricsRegistry:   metricsRegistry,
		Logger:            logger,
		Platform:          platform,
		Manager:           configManager,
		ComponentRegistry: componentRegistry,
	}
}

// configureAndCreateServices configures the manager and creates all services
func configureAndCreateServices(
	cfg *config.Config,
	manager *service.Manager,
	svcDeps *service.Dependencies,
) error {
	slog.Debug("Configuring Manager")
	if err := manager.ConfigureFromServices(cfg.Services, svcDeps); err != nil {
		return fmt.Errorf("configure service manager: %w", err)
	}
	return nil
}

type runtimeManager interface {
	StartAll(context.Context) error
	StartHealthListener(context.Context, int) error
	StopAll(context.Context) error
}

func runUntilShutdown(
	runtimeCtx context.Context,
	shutdownRequested <-chan struct{},
	manager runtimeManager,
	shutdownTimeout time.Duration,
	healthPort int,
	closeTransport func(context.Context) error,
) error {
	select {
	case <-shutdownRequested:
		return errors.New("shutdown requested before service startup")
	default:
	}

	slog.Info("Starting all services")
	if err := manager.StartAll(runtimeCtx); err != nil {
		cleanupErr := stopAndCloseRuntime(manager, shutdownTimeout, closeTransport)
		logShutdownError(cleanupErr)
		return errors.Join(fmt.Errorf("start services: %w", err), cleanupErr)
	}
	slog.Info("All services started successfully")

	// Optional dedicated health-port listener (#100). Binds /health and
	// /healthz on a port independent of the service-manager UI's
	// HTTPPort — convenience for Docker / k8s probes. Zero is a no-op
	// (the default). Bind failure is logged here at Warn level;
	// boot continues since the service-manager's main /health is the
	// authoritative health surface.
	if err := manager.StartHealthListener(runtimeCtx, healthPort); err != nil {
		slog.Warn("dedicated health listener failed to start; continuing without it",
			"port", healthPort, "error", err)
	}

	<-shutdownRequested
	slog.Info("Received shutdown signal")

	if err := stopAndCloseRuntime(manager, shutdownTimeout, closeTransport); err != nil {
		logShutdownError(err)
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	slog.Info("SemStreams shutdown complete")
	return nil
}

func stopAndCloseRuntime(
	manager runtimeManager,
	shutdownTimeout time.Duration,
	closeTransport func(context.Context) error,
) error {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	stopErr := manager.StopAll(shutdownCtx)
	var closeErr error
	if closeTransport != nil {
		closeErr = attributeRootCloseError(closeTransport(shutdownCtx))
	}
	return errors.Join(stopErr, closeErr)
}

func attributeRootCloseError(err error) error {
	if err == nil {
		return nil
	}
	var shutdownErr *shutdownerrs.ShutdownError
	if errors.As(err, &shutdownErr) {
		return err
	}
	return shutdownerrs.NewShutdownError(appName, shutdownerrs.PhaseCloseTransport, err)
}

func stopWithinShutdownBudget(ctx context.Context, stop func(time.Duration) error) error {
	budget, budgetErr := remainingShutdownBudget(ctx)
	if budgetErr != nil {
		stopErr := stop(time.Nanosecond)
		return errors.Join(
			shutdownerrs.NewShutdownError(appName+"/config-manager", shutdownerrs.PhaseDrainSubscriptions, budgetErr),
			shutdownerrs.NewShutdownError(appName+"/config-manager", shutdownerrs.PhaseDrainSubscriptions, stopErr),
		)
	}
	stopErr := stop(budget)
	if stopErr == nil {
		stopErr = ctx.Err()
	}
	return shutdownerrs.NewShutdownError(appName+"/config-manager", shutdownerrs.PhaseDrainSubscriptions, stopErr)
}

func remainingShutdownBudget(ctx context.Context) (time.Duration, error) {
	if ctx == nil {
		return 0, errors.New("shutdown context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0, errors.New("shutdown context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, context.DeadlineExceeded
	}
	return remaining, nil
}

func logShutdownError(err error) {
	var shutdownErr *shutdownerrs.ShutdownError
	if errors.As(err, &shutdownErr) {
		slog.Error("shutdown phase failed",
			"owner", shutdownErr.Owner,
			"phase", shutdownErr.Phase,
			"error", shutdownErr.Err,
		)
	}
}

// printHelp prints help information
func printHelp() {
	printDetailedHelp()
}

// buildRuleManager constructs a rule.ConfigManager dedicated to CRUD against
// the rules KV namespace. We intentionally pass nil for the rule.Processor
// reference: this manager is used only by the Pattern-B tool executors
// (create_rule/update_rule/delete_rule/list_rules/get_rule), not for
// runtime hot-reload into a live processor. All CRUD methods prefer the
// kvStore path when available (SaveRule/GetRule/DeleteRule always;
// ListRules as of ADR-029 step 1), so nil processor is safe.
//
// Hot-reload lives on the rule component itself — a second ConfigManager
// is constructed in processor/rule/processor.go startHotReloadManager
// with the live processor reference and watches rules.* KV directly.
// Two ConfigManager instances coexist against the same semstreams_config
// bucket by design: this one is write-only CRUD, the component-internal
// one is read+apply. NATS KV serialises per-key writes, so the split is
// safe.
//
// Returning nil on init failure is intentional: registerRules treats a
// nil RuleManager as "skip registration", keeping boot resilient to KV
// unavailability.
func buildRuleManager(ctx context.Context, natsClient *natsclient.Client, configMgr *config.Manager, logger *slog.Logger) executors.RuleManager {
	rcm := rulepkg.NewConfigManager(nil, configMgr, logger)
	if err := rcm.InitializeKVStore(ctx, natsClient); err != nil {
		logger.Warn("rule CRUD tools disabled: could not initialise rules KV store",
			slog.Any("error", err))
		return nil
	}
	return rcm
}

// buildPersonaManagerConcrete constructs a *persona.Manager (KV-backed
// persona CRUD) and returns the concrete type so callers like the file
// loader can call Upsert directly. Nil is returned on init failure;
// callers must nil-check before use.
//
// The concrete *persona.Manager satisfies executors.PersonaManager, so
// it can be passed directly to executors.RegisterAll without a cast.
func buildPersonaManagerConcrete(natsClient *natsclient.Client, logger *slog.Logger) *persona.Manager {
	mgr, err := persona.NewManager(natsClient)
	if err != nil {
		logger.Warn("persona CRUD tools disabled: could not initialise persona store",
			slog.Any("error", err))
		return nil
	}
	return mgr
}

// loadConfig loads configuration from the specified file path
func loadConfig(path string) (*config.Config, error) {
	loader := config.NewLoader()
	cfg, err := loader.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}

// registerPayloads builds the core payload registry and adds only capabilities
// explicitly selected by this deployment.
func registerPayloads(cfg *config.Config) (*payloadregistry.Registry, error) {
	reg := payloadregistry.New()
	if err := payloadbuiltins.Register(reg); err != nil {
		return nil, fmt.Errorf("register builtin payloads: %w", err)
	}
	if graphresearch.Selected(cfg) {
		if err := graphresearch.RegisterPayloads(reg); err != nil {
			return nil, fmt.Errorf("register graph research payloads: %w", err)
		}
	}
	return reg, nil
}

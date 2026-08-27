// Package main provides the E2E test application for SemStreams.
// This application imports semstreams as a library, registering core and
// example components for tiered E2E testing.
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
	"github.com/c360studio/semstreams/cmd/e2e-semstreams/fixtures"
	"github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	compositioncli "github.com/c360studio/semstreams/composition/cli"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/examples/processors/document"
	iotsensor "github.com/c360studio/semstreams/examples/processors/iot_sensor"
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
	semtypes "github.com/c360studio/semstreams/pkg/types"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
	rulepkg "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/test/e2e/harness/lessoncuration"
	"github.com/c360studio/semstreams/types"
	"github.com/c360studio/semstreams/vocabulary/builtins"
)

var (
	// Version is the semantic version of the E2E test application.
	Version = "0.1.0-e2e"
	// BuildTime is the build timestamp, set during compilation.
	BuildTime = "dev"
)

const appName = "e2e-semstreams"

func main() {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			_, _ = fmt.Fprintf(os.Stderr, "PANIC: %v\nStack trace:\n%s\n", r, string(buf[:n]))
			os.Exit(2)
		}
	}()

	// Composition verbs serve the catalog this binary can compose and exit;
	// mirrors cmd/semstreams.
	if code, handled := dispatchCompositionVerb(os.Args[1:]); handled {
		os.Exit(code)
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// dispatchCompositionVerb serves a composition verb against the full catalog
// this binary can compose (core, graph-research, OTEL, the bundled examples).
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

// fullComponentRegistry registers everything this binary can compose.
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
	if err := registerExampleComponents(registry); err != nil {
		return nil, fmt.Errorf("register example components: %w", err)
	}
	return registry, nil
}

func run() (runErr error) {
	// Match the production composition root: authoring validation must see all
	// first-party semantic names before any checked-in config is loaded.
	builtins.Register()

	printBanner()

	cliCfg, shouldExit, err := parseCLI()
	if shouldExit || err != nil {
		return err
	}

	// pprof (debug mode) — ADR-058 step 4 shared helper, NOT a Service. Kept early
	// (before NATS) so a wedged boot stays profilable. Identical to cmd/semstreams.
	service.MaybeStartPProf(cliCfg.Debug, cliCfg.DebugPort)

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

	metricsRegistry, phaseLogging, err := bootstrapobservability.NewE2EPhaseA(
		os.Stdout, cliCfg.LogLevel, cliCfg.LogFormat,
		[]slog.Attr{
			slog.String("service", "e2e-semstreams"),
			slog.String("version", Version),
			slog.Int("pid", os.Getpid()),
		},
	)
	if err != nil {
		return fmt.Errorf("compose phase-A logger: %w", err)
	}
	slog.SetDefault(phaseLogging.Process)

	ctx := context.Background()
	phaseA, err := completeE2EPhaseA(ctx, cfg, phaseLogging, metricsRegistry, cliCfg.ShutdownTimeout)
	if err != nil {
		return err
	}
	cfg = phaseA.config
	logger := phaseA.logger
	platform := phaseA.platform
	natsClient := phaseA.natsClient
	configManager := phaseA.configManager
	rootResources := &e2eRootResources{natsClient: natsClient, configManager: configManager}
	defer rootResources.abortOnReturn(cliCfg.ShutdownTimeout, &runErr)
	rootResources.stopMaxDeliveryObserver, err = maxdelivery.Start(ctx, natsClient, metricsRegistry, logger)
	if err != nil {
		return fmt.Errorf("start MaxDeliver observer: %w", err)
	}

	componentRegistry, manager, err := setupRegistriesAndManager(cfg)
	if err != nil {
		return err
	}

	payloadReg, err := buildPayloadRegistry(cfg)
	if err != nil {
		return err
	}

	lifecycleManager := lifecycle.NewManager(natsClient, logger)
	// The registry is the one table of framework contracts (ADR-103).
	mutationClient, err := service.WireGraphRuntime(
		ctx, natsClient, logger, payloadReg.Contracts()...,
	)
	if err != nil {
		return fmt.Errorf("wire graph runtime: %w", err)
	}
	lessonCurator := agentictools.NewLessonCurator(mutationClient, mutationClient, logger)
	rootResources.lessonCurationSub, err = natsClient.SubscribeForRequests(
		ctx,
		lessoncuration.SubjectPromote,
		lessoncuration.Handler(lessonCurator),
	)
	if err != nil {
		return fmt.Errorf("subscribe E2E lesson curation control: %w", err)
	}

	// Build the shared tool registry and register builtins BEFORE
	// service deps so component construction can resolve via
	// deps.ToolRegistry. Mirrors cmd/semstreams/main.go — see ADR-029.
	personaMgr := buildPersonaManager(natsClient, logger)
	if personaMgr != nil {
		if err := persona.LoadFromDirectory(ctx, "configs/personas/fragments", personaMgr, logger); err != nil {
			logger.Warn("persona file loader encountered errors", slog.Any("error", err))
		}
	}
	toolRegistry := agentictools.NewExecutorRegistry()
	if err := executors.RegisterBuiltins(ctx, toolRegistry, executors.ToolDependencies{
		NATSClient:              natsClient,
		MutationClient:          mutationClient,
		Platform:                platform,
		Logger:                  logger,
		RuleManager:             buildRuleManager(ctx, natsClient, configManager, logger),
		PersonaManager:          personaMgr,
		ComponentRegistry:       componentRegistry,
		LoopsBucket:             graphresearch.LoopsBucket(cfg),
		RestrictedDecideActions: extractRestrictedDecideActions(cfg, logger),
	}); err != nil {
		return fmt.Errorf("register builtin tools: %w", err)
	}
	if graphresearch.Selected(cfg) {
		if err := graphresearch.RegisterTool(ctx, toolRegistry, natsClient, platform, logger, graphresearch.LoopsBucket(cfg)); err != nil {
			return fmt.Errorf("register graph research tool: %w", err)
		}
	}

	svcDeps := createServiceDependencies(natsClient, metricsRegistry, logger, platform, configManager, componentRegistry)
	svcDeps.ToolRegistry = toolRegistry
	svcDeps.PayloadRegistry = payloadReg

	// Build the lifecycle Manager. DO NOT seed yet — graph-ingest is not
	// started until the signal-handling runtime path calls manager.StartAll; seeding
	// pre-start would deadlock (emit retry holds the goroutine that would
	// otherwise call StartAll). See gh#170.
	svcDeps.LifecycleManager = lifecycleManager

	// Register workflows (must come after Manager is constructed).
	if err := svcDeps.LifecycleManager.Register(mission.WorkflowDeclaration()); err != nil {
		return fmt.Errorf("register mission workflow: %w", err)
	}
	// Register the agent-run workflow (ADR-053 D2). Mirrors cmd/semstreams wiring.
	if err := agentrun.Register(svcDeps.LifecycleManager); err != nil {
		return fmt.Errorf("register agent-run workflow: %w", err)
	}

	// ADR-058 Phase B — agent-run milestone subscriber (ADR-053 D6) under the
	// ServiceManager's ordered shutdown. Mirrors cmd/semstreams wiring (identical
	// RegisterInstance block — the half-migration guard). Registered before
	// component services so StopAll stops it after their event publishers. Lifecycle
	// terminal mutations remain coordinator/component work through declared ports.
	// Start can abort boot on a genuine consumer-start failure; stream absence skips.
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

	if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {
		return err
	}

	// Validate each rule pack's local projection-contract composition. Done HERE
	// (after configureAndCreateServices constructs
	// the rule processors) and BEFORE the signal-handling runtime path calls StartAll.
	// SAME shared helper as cmd/semstreams. Pack contracts are read ONCE,
	// statically; this is the ONLY rule-pack bind site and must NEVER be invoked
	// from the hot-reload path. Every composition, overlap, and mutation-client
	// injection error is a boot gate. Repeated binding reaches the one-time guard.
	if err := service.ConfigureRulePackMutations(manager); err != nil {
		return fmt.Errorf("validate rule-pack composition: %w", err)
	}

	return runWithSignalHandling(ctx, manager, cliCfg.ShutdownTimeout, func(seedCtx context.Context) error {
		if cliCfg.LifecycleSeed == "" {
			return nil
		}
		return seedMission(seedCtx, svcDeps.LifecycleManager, svcDeps.Platform, cliCfg.LifecycleSeed)
	}, rootResources.close)
}

type e2eRootResources struct {
	natsClient              *natsclient.Client
	configManager           *config.Manager
	stopMaxDeliveryObserver func(context.Context) error
	lessonCurationSub       *natsclient.Subscription
	closeAttempted          bool
}

func (r *e2eRootResources) close(ctx context.Context) error {
	var closeErr error
	if r.lessonCurationSub != nil {
		closeErr = errors.Join(closeErr, shutdownerrs.NewShutdownError(
			appName+"/lesson-curation", shutdownerrs.PhaseDrainSubscriptions, r.lessonCurationSub.Unsubscribe(),
		))
	}
	if r.stopMaxDeliveryObserver != nil {
		closeErr = errors.Join(closeErr, shutdownerrs.NewShutdownError(
			appName+"/max-delivery", shutdownerrs.PhaseDrainConsumers, r.stopMaxDeliveryObserver(ctx),
		))
	}
	closeErr = errors.Join(closeErr, stopWithinShutdownBudget(ctx, r.configManager.Stop))
	r.closeAttempted = true
	return errors.Join(closeErr, shutdownerrs.NewShutdownError(
		appName, shutdownerrs.PhaseCloseTransport, r.natsClient.Close(ctx),
	))
}

func (r *e2eRootResources) abortOnReturn(timeout time.Duration, runErr *error) {
	if r.closeAttempted {
		return
	}
	abortCtx, abortCancel := context.WithTimeout(context.Background(), timeout)
	defer abortCancel()
	*runErr = errors.Join(*runErr, r.close(abortCtx))
}

type e2ePhaseAResult struct {
	natsClient    *natsclient.Client
	configManager *config.Manager
	config        *config.Config
	logger        *slog.Logger
	platform      types.PlatformMeta
}

// completeE2EPhaseA keeps the E2E root's presentation details around the
// shared plain bootstrap helpers while preserving its explicit stdout-only
// steady-state logger.
func completeE2EPhaseA(
	ctx context.Context,
	initial *config.Config,
	phaseLogging *bootstrapobservability.PhaseALogging,
	metricsRegistry *metric.MetricsRegistry,
	shutdownTimeout time.Duration,
) (*e2ePhaseAResult, error) {
	natsClient, err := connectToNATSWithSpinner(ctx, initial, phaseLogging.Client, metricsRegistry)
	if err != nil {
		return nil, err
	}
	configManager, effective, err := bootstrapobservability.StartValidatedConfigManager(
		ctx, initial, natsClient, phaseLogging.ConfigManager,
	)
	if err != nil {
		abortCtx, abortCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer abortCancel()
		closeErr := shutdownerrs.NewShutdownError(appName, shutdownerrs.PhaseCloseTransport, natsClient.Close(abortCtx))
		return nil, errors.Join(err, closeErr)
	}
	if err := ensureStreamsWithSpinner(ctx, effective, natsClient, phaseLogging.ConfigManager); err != nil {
		abortCtx, abortCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer abortCancel()
		configErr := stopWithinShutdownBudget(abortCtx, configManager.Stop)
		closeErr := shutdownerrs.NewShutdownError(appName, shutdownerrs.PhaseCloseTransport, natsClient.Close(abortCtx))
		return nil, errors.Join(err, configErr, closeErr)
	}

	logger := phaseLogging.Steady(nil)
	slog.SetDefault(logger)
	logger.Info("E2E SemStreams ready", "version", Version, "build_time", BuildTime)
	platform := extractPlatformMeta(effective)
	logger.Info("Platform identity configured",
		"org", platform.Org,
		"platform", platform.Platform,
		"environment", effective.Platform.Environment)
	return &e2ePhaseAResult{
		natsClient: natsClient, configManager: configManager, config: effective, logger: logger, platform: platform,
	}, nil
}

// buildPayloadRegistry constructs the shared payload registry and
// registers builtins + the example processor payloads loaded by
// this binary (iot_sensor, document, mission). Mirrors
// cmd/semstreams/main.go's split: payloadbuiltins.Register covers
// only first-party builtins; example processors register their own
// payload types so downstream consumers (semdragons, semspec)
// don't inherit example dependencies.
func buildPayloadRegistry(cfg *config.Config) (*payloadregistry.Registry, error) {
	reg := payloadregistry.New()
	if err := payloadbuiltins.Register(reg); err != nil {
		return nil, fmt.Errorf("register builtin payloads: %w", err)
	}
	if err := iotsensor.RegisterPayloads(reg); err != nil {
		return nil, fmt.Errorf("register iot_sensor payloads: %w", err)
	}
	if err := document.RegisterPayloads(reg); err != nil {
		return nil, fmt.Errorf("register document payloads: %w", err)
	}
	if err := mission.RegisterPayloads(reg); err != nil {
		return nil, fmt.Errorf("register mission payloads: %w", err)
	}
	// The keys the e2e scenarios stamp on entity.create (ADR-103): without
	// this every scenario birth through the real wire is refused.
	if err := fixtures.RegisterPayloads(reg); err != nil {
		return nil, fmt.Errorf("register e2e fixture payloads: %w", err)
	}
	if graphresearch.Selected(cfg) {
		if err := graphresearch.RegisterPayloads(reg); err != nil {
			return nil, fmt.Errorf("register graph research payloads: %w", err)
		}
	}
	return reg, nil
}

// seedMission Creates a mission Participant at the given entity ID
// in the planning phase. Used by the lifecycle e2e tier's startup
// flag so the gateway has a known instance to serve before the
// scenario runs. Already-exists is treated as a no-op so the binary
// is idempotent across restarts in the e2e fixture.
//
// The seed's authority pair MUST equal this deployment's own
// platform.org/platform.id. Since ADR-102 the mission-command processor
// stamps positions 1-2 from deps.Platform and never from the wire, so a
// seed carrying a different pair creates an entity no command can ever
// reach: the tier then fails 5s later as "rule did not transition", which
// names the symptom and hides the cause. Reject it at boot instead.
func seedMission(ctx context.Context, mgr *lifecycle.Manager, platform types.PlatformMeta, entityID string) error {
	parsed, err := semtypes.ParseEntityID(entityID)
	if err != nil {
		return fmt.Errorf("--lifecycle-seed %q is not a canonical entity ID: %w", entityID, err)
	}
	if parsed.Org != platform.Org || parsed.Platform != platform.Platform {
		return fmt.Errorf(
			"--lifecycle-seed %q claims authority %s.%s but this deployment's authority is %s.%s "+
				"(platform.org/platform.id): the mission-command processor stamps its own authority, "+
				"so nothing would ever command the seeded entity",
			entityID, parsed.Org, parsed.Platform, platform.Org, platform.Platform)
	}
	state := &mission.State{
		EntityIDField: entityID,
		PhaseField:    mission.PhasePlanning,
	}
	err = mgr.Create(ctx, state)
	if err == nil {
		slog.Info("seeded mission", "entity_id", entityID, "phase", mission.PhasePlanning)
		return nil
	}
	// Manager.Create returns ErrAlreadyExists when the entity is
	// already lifecycle-managed (has the phase triple). Treat as a
	// no-op so the e2e binary is idempotent across restarts.
	if errors.Is(err, lifecycle.ErrAlreadyExists) {
		slog.Info("mission already seeded", "entity_id", entityID)
		return nil
	}
	return err
}

// buildRuleManager constructs a rule.ConfigManager for CRUD against the
// rules KV namespace. Mirrors cmd/semstreams/main.go — same rationale (nil
// processor reference, kvStore-backed CRUD only, hot-reload deferred).
func buildRuleManager(ctx context.Context, natsClient *natsclient.Client, configMgr *config.Manager, logger *slog.Logger) executors.RuleManager {
	rcm := rulepkg.NewConfigManager(nil, configMgr, logger)
	if err := rcm.InitializeKVStore(ctx, natsClient); err != nil {
		logger.Warn("rule CRUD tools disabled: could not initialise rules KV store",
			slog.Any("error", err))
		return nil
	}
	return rcm
}

// buildPersonaManager mirrors cmd/semstreams/main.go; ADR-029 Pattern B. It
// returns the concrete manager so the startup file loader can install the same
// checked-in persona fragments used by the production composition root.
func buildPersonaManager(natsClient *natsclient.Client, logger *slog.Logger) *persona.Manager {
	mgr, err := persona.NewManager(natsClient)
	if err != nil {
		logger.Warn("persona CRUD tools disabled: could not initialise persona store",
			slog.Any("error", err))
		return nil
	}
	return mgr
}

// --- CLI and Config Functions (copied from semstreams main.go) ---

// CLIConfig holds command-line configuration for the E2E application.
type CLIConfig struct {
	ConfigPath      string
	LogLevel        string
	LogFormat       string
	Debug           bool
	DebugPort       int
	Validate        bool
	ShowVersion     bool
	ShowHelp        bool
	ShutdownTimeout time.Duration
	// LifecycleSeed is the entity ID to seed into the mission
	// workflow at startup. ADR-047 has no public Create HTTP
	// endpoint by design; the lifecycle e2e tier uses this
	// flag to put a known instance in place before the scenario
	// hits the gateway. Empty = no seeding (default).
	LifecycleSeed string
}

func parseCLI() (*CLIConfig, bool, error) {
	cliCfg := &CLIConfig{
		ConfigPath:      getEnvOrDefault("SEMSTREAMS_CONFIG", "config.json"),
		LogLevel:        getEnvOrDefault("SEMSTREAMS_LOG_LEVEL", "info"),
		LogFormat:       getEnvOrDefault("SEMSTREAMS_LOG_FORMAT", "text"),
		Debug:           os.Getenv("SEMSTREAMS_DEBUG") == "true",
		DebugPort:       6060,
		ShutdownTimeout: 30 * time.Second,
		LifecycleSeed:   os.Getenv("SEMSTREAMS_LIFECYCLE_SEED"),
	}

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch {
		case arg == "-c" || arg == "--config":
			if i+1 < len(os.Args) {
				i++
				cliCfg.ConfigPath = os.Args[i]
			}
		case arg == "-v" || arg == "--version":
			cliCfg.ShowVersion = true
		case arg == "-h" || arg == "--help":
			cliCfg.ShowHelp = true
		case arg == "--validate":
			cliCfg.Validate = true
		case arg == "--lifecycle-seed":
			if i+1 < len(os.Args) {
				i++
				cliCfg.LifecycleSeed = os.Args[i]
			}
		}
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

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func printBanner() {
	fmt.Print(`
  ____                ____  _
 / ___|  ___ _ __ ___|  _ \| |_ _ __ ___  __ _ _ __ ___  ___
 \___ \ / _ \ '_ ` + "`" + ` _ \ |_) | __| '__/ _ \/ _` + "`" + ` | '_ ` + "`" + ` _ \/ __|
  ___) |  __/ | | | | |  _ <| |_| | |  __/ (_| | | | | | \__ \
 |____/ \___|_| |_| |_|_| \_\\__|_|  \___|\__,_|_| |_| |_|___/
                                                E2E Test Build
`)
}

func printHelp() {
	fmt.Printf(`%s - E2E Test Application

Usage: %s [options]

Options:
  -c, --config PATH   Configuration file path (default: config.json)
  -v, --version       Show version information
  -h, --help          Show this help message
  --validate          Validate the composition and exit (alias of: validate <config-path>)

Verbs:
  catalog                          print every registered factory with default ports
  validate <config-path>           print composition findings; exit 1 on errors
  graph <config-path> [--mermaid]  print the composition graph projection

Environment:
  SEMSTREAMS_CONFIG      Configuration file path
  SEMSTREAMS_LOG_LEVEL   Log level (debug, info, warn, error)
  SEMSTREAMS_LOG_FORMAT  Log format (text, json)
  SEMSTREAMS_DEBUG       Enable debug mode (true/false)
  SEMSTREAMS_NATS_URLS   NATS server URLs (comma-separated)
`, appName, appName)
}

func loadConfig(path string) (*config.Config, error) {
	loader := config.NewLoader()
	cfg, err := loader.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}

// --- Infrastructure Setup (copied from semstreams main.go) ---

func connectToNATSWithSpinner(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	metricsRegistry *metric.MetricsRegistry,
) (*natsclient.Client, error) {
	fmt.Print("Connecting to NATS...")

	natsClient, err := createNATSClient(cfg, logger, metricsRegistry)
	if err != nil {
		fmt.Println(" ✗")
		return nil, fmt.Errorf("create NATS client: %w", err)
	}

	if err := bootstrapobservability.ConnectClient(ctx, natsClient, logger); err != nil {
		fmt.Println(" ✗")
		return nil, err
	}

	fmt.Println(" ✓")
	return natsClient, nil
}

func createNATSClient(
	cfg *config.Config,
	logger *slog.Logger,
	metricsRegistry *metric.MetricsRegistry,
) (*natsclient.Client, error) {
	natsURLs := "nats://localhost:4222"

	if envURL := os.Getenv("SEMSTREAMS_NATS_URLS"); envURL != "" {
		natsURLs = envURL
	} else if len(cfg.NATS.URLs) > 0 {
		natsURLs = strings.Join(cfg.NATS.URLs, ",")
	}

	return bootstrapobservability.NewClient(natsURLs, logger, metricsRegistry)
}

func ensureStreamsWithSpinner(
	ctx context.Context,
	cfg *config.Config,
	natsClient *natsclient.Client,
	logger *slog.Logger,
) error {
	fmt.Print("Creating JetStream streams...")

	if err := bootstrapobservability.EnsureEffectiveStreams(ctx, cfg, natsClient, logger); err != nil {
		fmt.Println(" ✗")
		return err
	}

	fmt.Println(" ✓")
	return nil
}

func extractPlatformMeta(cfg *config.Config) types.PlatformMeta {
	// platform.id is the single deployment authority field (ADR-102, ruled
	// O-2): positions 1-2 of every identity this process mints.
	return types.PlatformMeta{
		Org:      cfg.GetOrg(),
		Platform: cfg.GetPlatform(),
	}
}

// extractRestrictedDecideActions reads the deployment-level decide-action
// restriction policy (gh#239) from the agentic-tools component config so an
// e2e flow config can exercise the gate. Mirrors the cmd/semstreams helper;
// empty means the permissive default.
func extractRestrictedDecideActions(cfg *config.Config, logger *slog.Logger) []string {
	for _, cc := range cfg.Components {
		if cc.Name != "agentic-tools" || !cc.Enabled {
			continue
		}
		var tcfg struct {
			RestrictedDecideActions []string `json:"restricted_decide_actions"`
		}
		if err := json.Unmarshal(cc.Config, &tcfg); err != nil {
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

func setupRegistriesAndManager(cfg *config.Config) (*component.Registry, *service.Manager, error) {
	componentRegistry := component.NewRegistry()
	slog.Debug("Registering core component factories")
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

	// Register bundled example/domain components used by e2e configs
	if err := registerExampleComponents(componentRegistry); err != nil {
		return nil, nil, fmt.Errorf("register example components: %w", err)
	}

	factories := componentRegistry.ListFactories()
	slog.Info("Core component factories registered", "count", len(factories))

	serviceRegistry := service.NewServiceRegistry()
	if err := service.RegisterAll(serviceRegistry); err != nil {
		return nil, nil, fmt.Errorf("register services: %w", err)
	}

	manager := service.NewServiceManager(serviceRegistry)

	return componentRegistry, manager, nil
}

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

// runWithSignalHandling starts all services and then, while the
// process is alive, runs the optional postStart hook. postStart sees
// a fully-started service graph (graph-ingest subscriptions live,
// rules wired) so callers that need to emit immediately on boot —
// e.g. lifecycle seed via Manager.Create — can do so without racing
// the cold-start path captured in gh#170.
func runWithSignalHandling(
	ctx context.Context,
	manager *service.Manager,
	shutdownTimeout time.Duration,
	postStart func(context.Context) error,
	closeTransport func(context.Context) error,
) error {
	signalCtx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()
	return runUntilShutdown(ctx, signalCtx.Done(), manager, shutdownTimeout, postStart, closeTransport)
}

type runtimeManager interface {
	StartAll(context.Context) error
	StopAll(context.Context) error
}

func runUntilShutdown(
	runtimeCtx context.Context,
	shutdownRequested <-chan struct{},
	manager runtimeManager,
	shutdownTimeout time.Duration,
	postStart func(context.Context) error,
	closeTransport func(context.Context) error,
) error {

	slog.Info("Starting all services")
	if err := manager.StartAll(runtimeCtx); err != nil {
		cleanupErr := stopAndCloseRuntime(manager, shutdownTimeout, closeTransport)
		logShutdownError(cleanupErr)
		return errors.Join(fmt.Errorf("start services: %w", err), cleanupErr)
	}
	slog.Info("All services started successfully")

	if postStart != nil {
		if err := postStart(runtimeCtx); err != nil {
			cleanupErr := stopAndCloseRuntime(manager, shutdownTimeout, closeTransport)
			logShutdownError(cleanupErr)
			return errors.Join(fmt.Errorf("post-start hook: %w", err), cleanupErr)
		}
	}

	<-shutdownRequested
	slog.Info("Received shutdown signal")

	if err := stopAndCloseRuntime(manager, shutdownTimeout, closeTransport); err != nil {
		logShutdownError(err)
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	slog.Info("E2E SemStreams shutdown complete")
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

// registerExampleComponents registers bundled example/domain processors.
// These are kept out of componentregistry.Register() so that downstream
// consumers (semdragons, semspec) don't inherit example dependencies.
func registerExampleComponents(registry *component.Registry) error {
	if err := iotsensor.Register(registry); err != nil {
		return fmt.Errorf("register iot_sensor: %w", err)
	}
	if err := document.Register(registry); err != nil {
		return fmt.Errorf("register document: %w", err)
	}
	if err := mission.Register(registry); err != nil {
		return fmt.Errorf("register mission-command: %w", err)
	}
	return nil
}

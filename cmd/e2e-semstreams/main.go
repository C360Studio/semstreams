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
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/examples/processors/document"
	iotsensor "github.com/c360studio/semstreams/examples/processors/iot_sensor"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/flowtemplate"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/persona"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
	rulepkg "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	// Version is the semantic version of the E2E test application.
	Version = "0.1.0-e2e"
	// BuildTime is the build timestamp, set during compilation.
	BuildTime = "dev"
	appName   = "e2e-semstreams"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			_, _ = fmt.Fprintf(os.Stderr, "PANIC: %v\nStack trace:\n%s\n", r, string(buf[:n]))
			os.Exit(2)
		}
	}()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	printBanner()

	cliCfg, shouldExit, err := parseCLI()
	if shouldExit || err != nil {
		return err
	}

	if cliCfg.Debug && cliCfg.DebugPort > 0 {
		go startPProfServer(cliCfg.DebugPort)
	}

	cfg, err := loadConfig(cliCfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	if cliCfg.Validate {
		fmt.Println("✓ Configuration is valid")
		return nil
	}

	ctx := context.Background()
	natsClient, err := connectToNATSWithSpinner(ctx, cfg)
	if err != nil {
		return err
	}
	defer natsClient.Close(ctx)

	if err := ensureStreamsWithSpinner(ctx, cfg, natsClient); err != nil {
		return err
	}

	logger := setupLogger(cliCfg.LogLevel, cliCfg.LogFormat, natsClient, cfg)
	slog.SetDefault(logger)

	slog.Info("E2E SemStreams ready",
		"version", Version,
		"build_time", BuildTime)

	metricsRegistry, platform, configManager, err := setupRemainingInfrastructure(ctx, cfg, natsClient, logger)
	if err != nil {
		return err
	}
	defer configManager.Stop(5 * time.Second)

	componentRegistry, manager, err := setupRegistriesAndManager(cfg)
	if err != nil {
		return err
	}

	payloadReg, err := buildPayloadRegistry()
	if err != nil {
		return err
	}

	// Build the shared tool registry and register builtins BEFORE
	// service deps so component construction can resolve via
	// deps.ToolRegistry. Mirrors cmd/semstreams/main.go — see ADR-029.
	toolRegistry := agentictools.NewExecutorRegistry()
	if err := executors.RegisterBuiltins(ctx, toolRegistry, executors.ToolDependencies{
		NATSClient:              natsClient,
		Platform:                platform,
		Logger:                  logger,
		RuleManager:             buildRuleManager(ctx, natsClient, configManager, logger),
		FlowManager:             buildFlowManager(natsClient, logger),
		PersonaManager:          buildPersonaManager(natsClient, logger),
		FlowTemplateManager:     buildFlowTemplateManager(natsClient, logger),
		ComponentRegistry:       componentRegistry,
		RestrictedDecideActions: extractRestrictedDecideActions(cfg, logger),
	}); err != nil {
		return fmt.Errorf("register builtin tools: %w", err)
	}

	svcDeps := createServiceDependencies(natsClient, metricsRegistry, logger, platform, configManager, componentRegistry)
	svcDeps.ToolRegistry = toolRegistry
	svcDeps.PayloadRegistry = payloadReg

	// Build the lifecycle Manager. DO NOT seed yet — graph-ingest is not
	// started until manager.StartAll inside runWithSignalHandling; seeding
	// pre-start would deadlock (emit retry holds the goroutine that would
	// otherwise call StartAll). See gh#170.
	svcDeps.LifecycleManager = lifecycle.NewManager(natsClient, logger)

	// ADR-058 Phase A — wire ownership buckets, Registry, and static projection
	// contracts. Returns nil, nil on bucket-bootstrap failure (disabled this
	// boot, best-effort — never a boot gate).
	//
	// The Manager-internal heartbeater (spawned by AttachOwnership inside
	// WireOwnership) runs on hbCtx. ADR-058 rollout step 2: the Manager is
	// deliberately NOT wrapped as a Service (it fails the Is-it-a-Service test —
	// WaitOwnership already provides the join). Its shutdown cancel+join is
	// factored into one shared helper so the two mains cannot drift; the deferred
	// cleanup runs (cancel→join) before the earlier-registered NATS Close defer.
	hbCtx, ownershipShutdown := service.WireOwnershipShutdown(ctx, svcDeps.LifecycleManager)
	defer ownershipShutdown()

	ownerReg, staticOwnerHB := service.WireOwnership(hbCtx, natsClient, svcDeps.LifecycleManager, logger,
		loopExecutionProjectionContract())
	// ADR-058 Phase B — static heartbeater goroutine under the ServiceManager's
	// ordered shutdown.
	manager.RegisterInstance("ownership", service.NewOwnershipService(ownerReg, staticOwnerHB, metricsRegistry, logger))

	// Register workflows (must come after Manager is constructed).
	if err := svcDeps.LifecycleManager.Register(mission.WorkflowDeclaration()); err != nil {
		return fmt.Errorf("register mission workflow: %w", err)
	}
	// Register the agent-run workflow (ADR-053 D2). Mirrors cmd/semstreams wiring.
	if err := agentrun.Register(svcDeps.LifecycleManager); err != nil {
		return fmt.Errorf("register agent-run workflow: %w", err)
	}

	// Start the agent-run milestone subscriber (ADR-053 D6). Mirrors
	// cmd/semstreams wiring. No product handlers registered in e2e binary;
	// D3 zombie-prevention still applies.
	milestoneSubscriber := agentrun.NewMilestoneSubscriber(
		svcDeps.LifecycleManager,
		agentrun.NewNATSLoopTripleReader(natsClient),
		agentrun.NewNATSTriplePublisher(natsClient),
		platform.Org,
		platform.Platform,
		logger,
	)
	stopMilestoneSubscriber, err := milestoneSubscriber.Start(ctx, natsClient, agentrun.StartConfig{
		StreamName: agentrun.AgentStreamName,
	})
	if err != nil {
		return fmt.Errorf("start agent-run milestone subscriber: %w", err)
	}
	defer stopMilestoneSubscriber()

	if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {
		return err
	}

	// ADR-056 #278 inc 2 — bind each rule pack's projection contracts under the
	// ownership substrate. Done HERE (after configureAndCreateServices constructs
	// the rule processors) and BEFORE StartAll runs inside runWithSignalHandling.
	// SAME shared helper as cmd/semstreams. Pack contracts are read ONCE,
	// statically; this is the ONLY rule-pack bind site and must NEVER be invoked
	// from the hot-reload path. Best-effort / observe-only.
	if ownerReg != nil {
		service.BindRulePackContracts(hbCtx, manager, ownerReg, staticOwnerHB, logger)
	}

	return runWithSignalHandling(ctx, manager, cliCfg.ShutdownTimeout, func(seedCtx context.Context) error {
		if cliCfg.LifecycleSeed == "" {
			return nil
		}
		return seedMission(seedCtx, svcDeps.LifecycleManager, cliCfg.LifecycleSeed)
	})
}

// buildPayloadRegistry constructs the shared payload registry and
// registers builtins + the example processor payloads loaded by
// this binary (iot_sensor, document, mission). Mirrors
// cmd/semstreams/main.go's split: payloadbuiltins.Register covers
// only first-party builtins; example processors register their own
// payload types so downstream consumers (semdragons, semspec)
// don't inherit example dependencies.
func buildPayloadRegistry() (*payloadregistry.Registry, error) {
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
	return reg, nil
}

// seedMission Creates a mission Participant at the given entity ID
// in the planning phase. Used by the lifecycle e2e tier's startup
// flag so the gateway has a known instance to serve before the
// scenario runs. Already-exists is treated as a no-op so the binary
// is idempotent across restarts in the e2e fixture.
func seedMission(ctx context.Context, mgr *lifecycle.Manager, entityID string) error {
	state := &mission.State{
		EntityIDField: entityID,
		PhaseField:    mission.PhasePlanning,
	}
	err := mgr.Create(ctx, state)
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
	if err := rcm.InitializeKVStore(natsClient); err != nil {
		logger.Warn("rule CRUD tools disabled: could not initialise rules KV store",
			slog.Any("error", err))
		return nil
	}
	_ = ctx
	return rcm
}

// buildFlowManager constructs a flowstore.Manager for flow CRUD. Mirrors
// cmd/semstreams/main.go. Returns nil on init failure so registerFlows
// skips registration — consistent with the RuleManager path.
func buildFlowManager(natsClient *natsclient.Client, logger *slog.Logger) executors.FlowManager {
	mgr, err := flowstore.NewManager(natsClient)
	if err != nil {
		logger.Warn("flow CRUD tools disabled: could not initialise flow store",
			slog.Any("error", err))
		return nil
	}
	return mgr
}

// buildPersonaManager mirrors cmd/semstreams/main.go; ADR-029 Pattern B.
func buildPersonaManager(natsClient *natsclient.Client, logger *slog.Logger) executors.PersonaManager {
	mgr, err := persona.NewManager(natsClient)
	if err != nil {
		logger.Warn("persona CRUD tools disabled: could not initialise persona store",
			slog.Any("error", err))
		return nil
	}
	return mgr
}

// buildFlowTemplateManager mirrors cmd/semstreams/main.go; ADR-029 Pattern B.
func buildFlowTemplateManager(natsClient *natsclient.Client, logger *slog.Logger) executors.FlowTemplateManager {
	mgr, err := flowtemplate.NewManager(natsClient)
	if err != nil {
		logger.Warn("flow-template tools disabled: could not initialise flow-template store",
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
  --validate          Validate configuration and exit

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

func connectToNATSWithSpinner(ctx context.Context, cfg *config.Config) (*natsclient.Client, error) {
	fmt.Print("Connecting to NATS...")

	natsClient, err := createNATSClient(cfg)
	if err != nil {
		fmt.Println(" ✗")
		return nil, fmt.Errorf("create NATS client: %w", err)
	}

	if err := natsClient.Connect(ctx); err != nil {
		fmt.Println(" ✗")
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := natsClient.WaitForConnection(connCtx); err != nil {
		fmt.Println(" ✗")
		return nil, fmt.Errorf("NATS connection timeout: %w", err)
	}

	fmt.Println(" ✓")
	return natsClient, nil
}

func createNATSClient(cfg *config.Config) (*natsclient.Client, error) {
	natsURLs := "nats://localhost:4222"

	if envURL := os.Getenv("SEMSTREAMS_NATS_URLS"); envURL != "" {
		natsURLs = envURL
	} else if len(cfg.NATS.URLs) > 0 {
		natsURLs = strings.Join(cfg.NATS.URLs, ",")
	}

	return natsclient.NewClient(natsURLs)
}

func ensureStreamsWithSpinner(ctx context.Context, cfg *config.Config, natsClient *natsclient.Client) error {
	fmt.Print("Creating JetStream streams...")

	quietLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	streamsManager := config.NewStreamsManager(natsClient, quietLogger)

	if err := streamsManager.EnsureStreams(ctx, cfg); err != nil {
		fmt.Println(" ✗")
		return fmt.Errorf("ensure streams: %w", err)
	}

	fmt.Println(" ✓")
	return nil
}

func setupLogger(level, format string, _ *natsclient.Client, _ *config.Config) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: logLevel}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

func setupRemainingInfrastructure(
	ctx context.Context,
	cfg *config.Config,
	natsClient *natsclient.Client,
	logger *slog.Logger,
) (*metric.MetricsRegistry, types.PlatformMeta, *config.Manager, error) {
	metricsRegistry := metric.NewMetricsRegistry()

	platform := extractPlatformMeta(cfg)

	slog.Info("Platform identity configured",
		"org", platform.Org,
		"platform", platform.Platform,
		"environment", cfg.Platform.Environment)

	configManager, err := config.NewConfigManager(cfg, natsClient, logger)
	if err != nil {
		return nil, types.PlatformMeta{}, nil, fmt.Errorf("create config manager: %w", err)
	}

	if err := configManager.Start(ctx); err != nil {
		return nil, types.PlatformMeta{}, nil, fmt.Errorf("start config manager: %w", err)
	}

	return metricsRegistry, platform, configManager, nil
}

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
	ensureServiceManagerConfig(cfg)

	return componentRegistry, manager, nil
}

func ensureServiceManagerConfig(cfg *config.Config) {
	if cfg.Services == nil {
		cfg.Services = make(types.ServiceConfigs)
	}

	if _, exists := cfg.Services["service-manager"]; !exists {
		defaultConfig := map[string]any{
			"http_port":  8080,
			"swagger_ui": true,
			"server_info": map[string]string{
				"title":       "SemStreams E2E API",
				"description": "E2E test application",
				"version":     Version,
			},
		}
		defaultConfigJSON, _ := json.Marshal(defaultConfig)
		cfg.Services["service-manager"] = types.ServiceConfig{
			Name:    "service-manager",
			Enabled: true,
			Config:  defaultConfigJSON,
		}
	}
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

	slog.Debug("Creating services from config", "count", len(cfg.Services))
	for name, svcConfig := range cfg.Services {
		if name == "service-manager" {
			continue
		}

		if err := createServiceIfEnabled(manager, name, svcConfig, svcDeps); err != nil {
			return err
		}
	}

	return nil
}

func createServiceIfEnabled(
	manager *service.Manager,
	name string,
	svcConfig types.ServiceConfig,
	svcDeps *service.Dependencies,
) error {
	if !svcConfig.Enabled {
		return nil
	}

	if !manager.HasConstructor(name) {
		slog.Warn("Service configured but not registered", "key", name)
		return nil
	}

	if _, err := manager.CreateService(name, svcConfig.Config, svcDeps); err != nil {
		return fmt.Errorf("create service %s: %w", name, err)
	}

	slog.Info("Created service", "name", name)
	return nil
}

// runWithSignalHandling starts all services and then, while the
// process is alive, runs the optional postStart hook. postStart sees
// a fully-started service graph (graph-ingest subscriptions live,
// rules wired) so callers that need to emit immediately on boot —
// e.g. lifecycle seed via Manager.Create — can do so without racing
// the cold-start path captured in gh#170.
func runWithSignalHandling(ctx context.Context, manager *service.Manager, shutdownTimeout time.Duration, postStart func(context.Context) error) error {
	signalCtx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	slog.Info("Starting all services")
	if err := manager.StartAll(signalCtx); err != nil {
		return fmt.Errorf("start services: %w", err)
	}
	slog.Info("All services started successfully")

	if postStart != nil {
		if err := postStart(signalCtx); err != nil {
			return fmt.Errorf("post-start hook: %w", err)
		}
	}

	<-signalCtx.Done()
	slog.Info("Received shutdown signal")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := shutdown(shutdownCtx, manager, shutdownTimeout); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	slog.Info("E2E SemStreams shutdown complete")
	return nil
}

func shutdown(ctx context.Context, manager *service.Manager, timeout time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}

	if err := manager.StopAll(timeout); err != nil {
		slog.Error("Error stopping services", "error", err)
		return err
	}

	return nil
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

func startPProfServer(port int) {
	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Starting pprof server on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
		fmt.Printf("pprof server error: %v\n", err)
	}
}

// loopExecutionProjectionContract returns the graph projection contract for
// loop-execution entities (ADR-056 W0 4c-pre-1). Mirrors cmd/semstreams/main.go.
// See that function's godoc for the full rationale.
func loopExecutionProjectionContract() projection.Contract {
	return projection.Contract{
		Name:          "agentic.loop-execution",
		MessageType:   agentic.LoopExecutionMessageType().Key(),
		EntityPattern: "*.*.agent.agentic-loop.execution.*",
		Groups: []projection.PredicateGroup{{
			Mode: ownership.ModeReplaceOwned,
			Predicates: []string{
				agvocab.LoopRole,
				agvocab.LoopTask,
				agvocab.LoopParent,
				agvocab.LoopRun,
				agvocab.LoopRunEntityID,
				agvocab.LoopReplyTo,
				agvocab.LoopWorkflow,
				agvocab.LoopWorkflowStep,
				agvocab.LoopUser,
				agvocab.LoopDescription,
			},
		}},
	}
}

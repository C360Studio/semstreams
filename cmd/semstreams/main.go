// Package main implements the entry point for the SemStreams application.
// SemStreams is a semantic stream processing framework that combines
// protocol-level data processing with semantic knowledge graph capabilities.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // Register pprof handlers on DefaultServeMux
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/examples/processors/document"
	iotsensor "github.com/c360studio/semstreams/examples/processors/iot_sensor"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/flowtemplate"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/persona"
	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
	rulepkg "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
)

// Build information constants
const (
	Version   = "0.1.0"
	BuildTime = "dev"
	appName   = "semstreams"
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

	// Run application with proper error handling
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. Print banner
	printBanner()

	// 2. Parse and validate CLI flags
	cliCfg, shouldExit, err := parseCLI()
	if shouldExit || err != nil {
		return err
	}

	// 2.5. Start pprof server if debug mode enabled (before NATS - independent)
	if cliCfg.Debug && cliCfg.DebugPort > 0 {
		go startPProfServer(cliCfg.DebugPort)
	}

	// 3. Load and validate configuration
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

	// 4. Connect to NATS (required - semstreams cannot operate without NATS)
	ctx := context.Background()
	natsClient, err := connectToNATSWithSpinner(ctx, cfg)
	if err != nil {
		return err
	}
	defer natsClient.Close(ctx)

	// 5. Ensure JetStream streams exist (LOGS, HEALTH, METRICS, FLOWS)
	if err := ensureStreamsWithSpinner(ctx, cfg, natsClient); err != nil {
		return err
	}

	// 6. NOW create the full logger with NATS publisher (no nil, no mutation)
	logger := setupLogger(cliCfg.LogLevel, cliCfg.LogFormat, natsClient, cfg)
	slog.SetDefault(logger)

	slog.Info("SemStreams ready",
		"version", Version,
		"build_time", BuildTime)

	// 7. Create remaining infrastructure
	metricsRegistry, platform, configManager, err := setupRemainingInfrastructure(ctx, cfg, natsClient, logger)
	if err != nil {
		return err
	}
	defer configManager.Stop(5 * time.Second)

	// 8. Setup registries and manager
	componentRegistry, manager, err := setupRegistriesAndManager(cfg)
	if err != nil {
		return err
	}

	// 9. Create service dependencies
	svcDeps := createServiceDependencies(natsClient, metricsRegistry, logger, platform, configManager, componentRegistry)

	// 10. Configure and create services
	if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {
		return err
	}

	// 11. Register the global agentic tool executors after components have
	// been wired. Scheduling this post-configure lets Pattern-B managers
	// (rule.ConfigManager today; flow/persona/template managers later per
	// ADR-029) resolve against already-initialised infrastructure. Stateful
	// tools (read_loop_result, decide, query_entity) need natsClient +
	// platform which are available from step 7.
	executors.RegisterAll(ctx, executors.ToolDependencies{
		NATSClient:          natsClient,
		Platform:            platform,
		Logger:              logger,
		RuleManager:         buildRuleManager(ctx, natsClient, configManager, logger),
		FlowManager:         buildFlowManager(natsClient, logger),
		PersonaManager:      buildPersonaManager(natsClient, logger),
		FlowTemplateManager: buildFlowTemplateManager(natsClient, logger),
		ComponentRegistry:   componentRegistry,
	})

	// 11. Run application with signal handling
	return runWithSignalHandling(ctx, manager, cliCfg.ShutdownTimeout)
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

// connectToNATSWithSpinner connects to NATS with a spinner for user feedback.
// NATS is a hard requirement - semstreams cannot operate without it.
func connectToNATSWithSpinner(ctx context.Context, cfg *config.Config) (*natsclient.Client, error) {
	spinner := NewSpinner("Connecting to NATS...")
	spinner.Start()

	natsClient, err := createNATSClient(cfg)
	if err != nil {
		spinner.StopWithError(err)
		return nil, fmt.Errorf("create NATS client: %w", err)
	}

	if err := natsClient.Connect(ctx); err != nil {
		spinner.StopWithError(err)
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := natsClient.WaitForConnection(connCtx); err != nil {
		spinner.StopWithError(err)
		return nil, fmt.Errorf("NATS connection timeout: %w", err)
	}

	spinner.Stop()
	return natsClient, nil
}

// ensureStreamsWithSpinner creates JetStream streams with a spinner for user feedback.
func ensureStreamsWithSpinner(ctx context.Context, cfg *config.Config, natsClient *natsclient.Client) error {
	spinner := NewSpinner("Creating JetStream streams...")
	spinner.Start()

	// Use a quiet logger for stream creation (we have the spinner for feedback)
	quietLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	streamsManager := config.NewStreamsManager(natsClient, quietLogger)

	if err := streamsManager.EnsureStreams(ctx, cfg); err != nil {
		spinner.StopWithError(err)
		return fmt.Errorf("ensure streams: %w", err)
	}

	spinner.Stop()
	return nil
}

// setupRemainingInfrastructure creates metrics, platform, and config manager.
func setupRemainingInfrastructure(
	ctx context.Context,
	cfg *config.Config,
	natsClient *natsclient.Client,
	logger *slog.Logger,
) (*metric.MetricsRegistry, types.PlatformMeta, *config.Manager, error) {
	// Create metrics registry
	metricsRegistry := metric.NewMetricsRegistry()

	// Extract platform identity
	platform := extractPlatformMeta(cfg)

	slog.Info("Platform identity configured",
		"org", platform.Org,
		"platform", platform.Platform,
		"environment", cfg.Platform.Environment)

	// Create and start config manager
	configManager, err := config.NewConfigManager(cfg, natsClient, logger)
	if err != nil {
		return nil, types.PlatformMeta{}, nil, fmt.Errorf("create config manager: %w", err)
	}

	if err := configManager.Start(ctx); err != nil {
		return nil, types.PlatformMeta{}, nil, fmt.Errorf("start config manager: %w", err)
	}

	return metricsRegistry, platform, configManager, nil
}

// createNATSClient creates a NATS client from config.
func createNATSClient(cfg *config.Config) (*natsclient.Client, error) {
	natsURLs := "nats://localhost:4222"

	// Environment variable override takes precedence
	if envURL := os.Getenv("SEMSTREAMS_NATS_URLS"); envURL != "" {
		natsURLs = envURL
	} else if len(cfg.NATS.URLs) > 0 {
		natsURLs = strings.Join(cfg.NATS.URLs, ",")
	}

	return natsclient.NewClient(natsURLs)
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

	// Register bundled example/domain components (not in core registry to avoid
	// pulling example deps into downstream consumers like semdragons/semspec)
	if err := registerExampleComponents(componentRegistry); err != nil {
		return nil, nil, fmt.Errorf("register example components: %w", err)
	}

	factories := componentRegistry.ListFactories()
	slog.Info("Core component factories registered", "count", len(factories), "factories", factories)

	serviceRegistry := service.NewServiceRegistry()
	if err := service.RegisterAll(serviceRegistry); err != nil {
		return nil, nil, fmt.Errorf("register services: %w", err)
	}

	manager := service.NewServiceManager(serviceRegistry)
	ensureServiceManagerConfig(cfg)
	ensureMetricsConfig(cfg)

	return componentRegistry, manager, nil
}

// ensureServiceManagerConfig ensures service-manager config exists with defaults
func ensureServiceManagerConfig(cfg *config.Config) {
	if cfg.Services == nil {
		cfg.Services = make(types.ServiceConfigs)
	}

	if _, exists := cfg.Services["service-manager"]; !exists {
		slog.Debug("Adding default service-manager config")
		defaultConfig := map[string]any{
			"http_port":  8080,
			"swagger_ui": true,
			"server_info": map[string]string{
				"title":       "SemStreams API",
				"description": "semantic stream processing framework - protocol and semantic layers",
				"version":     Version,
			},
		}
		defaultConfigJSON, _ := json.Marshal(defaultConfig)
		cfg.Services["service-manager"] = types.ServiceConfig{
			Name:    "service-manager",
			Enabled: true,
			Config:  defaultConfigJSON,
		}
		slog.Debug("Service-manager config added", "enabled", true)
	} else {
		slog.Debug("Service-manager config already exists", "enabled", cfg.Services["service-manager"].Enabled)
	}
}

// ensureMetricsConfig ensures metrics service is always present with defaults.
// Observability should not be opt-in — metrics are critical for tuning and SLA validation.
func ensureMetricsConfig(cfg *config.Config) {
	if _, exists := cfg.Services["metrics"]; !exists {
		slog.Debug("Adding default metrics config")
		defaultConfig := map[string]any{
			"port":               9090,
			"path":               "/metrics",
			"include_go_metrics": true,
		}
		defaultConfigJSON, _ := json.Marshal(defaultConfig)
		cfg.Services["metrics"] = types.ServiceConfig{
			Name:    "metrics",
			Enabled: true,
			Config:  defaultConfigJSON,
		}
		slog.Debug("Metrics config added", "port", 9090)
	}
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

	slog.Debug("Creating services from config", "count", len(cfg.Services))
	for name, svcConfig := range cfg.Services {
		if name == "service-manager" {
			slog.Debug("Skipping service-manager (configured directly)")
			continue
		}

		if err := createServiceIfEnabled(manager, name, svcConfig, svcDeps); err != nil {
			return err
		}
	}

	return nil
}

// createServiceIfEnabled creates a service if it's enabled and registered
func createServiceIfEnabled(
	manager *service.Manager,
	name string,
	svcConfig types.ServiceConfig,
	svcDeps *service.Dependencies,
) error {
	slog.Debug("Processing service config", "key", name, "name", svcConfig.Name, "enabled", svcConfig.Enabled)

	if !svcConfig.Enabled {
		slog.Info("Service disabled in config", "name", name)
		return nil
	}

	if !manager.HasConstructor(name) {
		slog.Warn("Service configured but not registered", "key", name, "available_constructors", manager.ListConstructors())
		return nil
	}

	slog.Debug("Creating service", "name", name, "has_constructor", true)
	if _, err := manager.CreateService(name, svcConfig.Config, svcDeps); err != nil {
		return fmt.Errorf("create service %s: %w", name, err)
	}

	slog.Info("Created service", "name", name, "config_name", svcConfig.Name)
	return nil
}

// runWithSignalHandling starts services and handles shutdown signals
func runWithSignalHandling(ctx context.Context, manager *service.Manager, shutdownTimeout time.Duration) error {
	slog.Debug("Setting up signal handling")
	signalCtx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	slog.Info("Starting all services")
	if err := manager.StartAll(signalCtx); err != nil {
		return fmt.Errorf("start services: %w", err)
	}
	slog.Info("All services started successfully")

	<-signalCtx.Done()
	slog.Info("Received shutdown signal")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := shutdown(shutdownCtx, manager, shutdownTimeout); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	slog.Info("SemStreams shutdown complete")
	return nil
}

// shutdown performs graceful shutdown of all services
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
	if err := rcm.InitializeKVStore(natsClient); err != nil {
		logger.Warn("rule CRUD tools disabled: could not initialise rules KV store",
			slog.Any("error", err))
		return nil
	}
	_ = ctx // reserved for future use if KV init needs a context
	return rcm
}

// buildFlowManager constructs a flowstore.Manager (KV-backed flow CRUD).
// Nil returned on init failure so registerFlows skips tool registration
// — consistent with the nil-RuleManager path. Matches ADR-029 Pattern B.
func buildFlowManager(natsClient *natsclient.Client, logger *slog.Logger) executors.FlowManager {
	mgr, err := flowstore.NewManager(natsClient)
	if err != nil {
		logger.Warn("flow CRUD tools disabled: could not initialise flow store",
			slog.Any("error", err))
		return nil
	}
	return mgr
}

// buildPersonaManager constructs a persona.Manager (KV-backed persona
// CRUD). Mirrors buildRuleManager / buildFlowManager shape. Assembler
// integration — where saved personas override code-defined
// DefaultFragments — is a separate step (ADR-029 step 3b); for now the
// tool surface just reads/writes the PERSONAS bucket.
func buildPersonaManager(natsClient *natsclient.Client, logger *slog.Logger) executors.PersonaManager {
	mgr, err := persona.NewManager(natsClient)
	if err != nil {
		logger.Warn("persona CRUD tools disabled: could not initialise persona store",
			slog.Any("error", err))
		return nil
	}
	return mgr
}

// buildFlowTemplateManager constructs a flowtemplate.Manager (KV-backed
// template CRUD + render). Same shape as the other Pattern-B builders.
func buildFlowTemplateManager(natsClient *natsclient.Client, logger *slog.Logger) executors.FlowTemplateManager {
	mgr, err := flowtemplate.NewManager(natsClient)
	if err != nil {
		logger.Warn("flow-template tools disabled: could not initialise flow-template store",
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
	return nil
}

// startPProfServer starts the pprof HTTP server for profiling.
// The server runs on http.DefaultServeMux which has pprof handlers
// registered via the blank import of net/http/pprof.
func startPProfServer(port int) {
	addr := fmt.Sprintf(":%d", port)
	// Use a simple logger that works before slog is configured
	fmt.Printf("Starting pprof server on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
		fmt.Printf("pprof server error: %v\n", err)
	}
}

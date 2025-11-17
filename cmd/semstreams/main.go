// Package main implements the entry point for the SemStreams application.
// SemStreams is a semantic stream processing framework that combines
// protocol-level data processing with semantic knowledge graph capabilities.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/componentregistry"
	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/service"
	"github.com/c360/semstreams/types"
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
		slog.Error("Application failed", "error", err, "exit_code", 1)
		os.Exit(1)
	}
}

func run() error {
	// Parse and validate CLI flags
	cliCfg, logger, shouldExit, err := initializeCLI()
	if shouldExit || err != nil {
		return err
	}

	// Load and validate configuration
	cfg, err := initializeConfiguration(cliCfg)
	if err != nil {
		return err
	}

	if cliCfg.Validate {
		slog.Info("Configuration is valid")
		return nil
	}

	// Setup core infrastructure
	ctx := context.Background()
	natsClient, metricsRegistry, platform, configManager, err := setupInfrastructure(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer natsClient.Close(ctx)
	defer configManager.Stop(5 * time.Second)

	// Setup registries and manager
	componentRegistry, manager, err := setupRegistriesAndManager(cfg)
	if err != nil {
		return err
	}

	// Create service dependencies
	svcDeps := createServiceDependencies(natsClient, metricsRegistry, logger, platform, configManager, componentRegistry)

	// Configure and create services
	if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {
		return err
	}

	// Run application with signal handling
	return runWithSignalHandling(ctx, manager, cliCfg.ShutdownTimeout)
}

// initializeCLI parses flags and sets up logging
func initializeCLI() (*CLIConfig, *slog.Logger, bool, error) {
	cliCfg := parseFlags()
	if err := validateFlags(cliCfg); err != nil {
		return nil, nil, false, fmt.Errorf("invalid flags: %w", err)
	}

	if cliCfg.ShowVersion {
		fmt.Printf("%s version %s\n", appName, Version)
		return nil, nil, true, nil
	}

	if cliCfg.ShowHelp {
		printHelp()
		return nil, nil, true, nil
	}

	logger := setupLogger(cliCfg.LogLevel, cliCfg.LogFormat)
	slog.SetDefault(logger)

	slog.Info("Starting SemStreams (semantic stream processing)",
		"version", Version,
		"build_time", BuildTime,
		"config_path", cliCfg.ConfigPath)

	return cliCfg, logger, false, nil
}

// initializeConfiguration loads and validates configuration
func initializeConfiguration(cliCfg *CLIConfig) (*config.Config, error) {
	cfg, err := loadConfig(cliCfg.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// setupInfrastructure creates and connects core infrastructure components
func setupInfrastructure(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) (*natsclient.Client, *metric.MetricsRegistry, types.PlatformMeta, *config.Manager, error) {
	natsClient, metricsRegistry, platform, err := createCoreDependencies(cfg)
	if err != nil {
		return nil, nil, types.PlatformMeta{}, nil, fmt.Errorf("create dependencies: %w", err)
	}

	if err := connectToNATS(ctx, natsClient); err != nil {
		return nil, nil, types.PlatformMeta{}, nil, err
	}

	configManager, err := setupConfigManager(ctx, cfg, natsClient, logger)
	if err != nil {
		return nil, nil, types.PlatformMeta{}, nil, err
	}

	return natsClient, metricsRegistry, platform, configManager, nil
}

// connectToNATS establishes NATS connection and waits for it to be ready
func connectToNATS(ctx context.Context, natsClient *natsclient.Client) error {
	slog.Info("Connecting to NATS")
	if err := natsClient.Connect(ctx); err != nil {
		return fmt.Errorf("connect to NATS: %w", err)
	}

	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := natsClient.WaitForConnection(connCtx); err != nil {
		return fmt.Errorf("NATS connection timeout: %w", err)
	}

	return nil
}

// setupConfigManager creates and starts the config manager
func setupConfigManager(
	ctx context.Context,
	cfg *config.Config,
	natsClient *natsclient.Client,
	logger *slog.Logger,
) (*config.Manager, error) {
	slog.Info("Creating Manager")
	configManager, err := config.NewConfigManager(cfg, natsClient, logger)
	if err != nil {
		return nil, fmt.Errorf("create config manager: %w", err)
	}

	if err := configManager.Start(ctx); err != nil {
		return nil, fmt.Errorf("start config manager: %w", err)
	}

	return configManager, nil
}

// setupRegistriesAndManager creates registries and service manager
func setupRegistriesAndManager(cfg *config.Config) (*component.Registry, *service.Manager, error) {
	componentRegistry := component.NewRegistry()
	slog.Debug("Registering core component factories (UDP, WebSocket, parsers)")
	if err := componentregistry.Register(componentRegistry); err != nil {
		return nil, nil, fmt.Errorf("register components: %w", err)
	}

	factories := componentRegistry.ListFactories()
	slog.Info("core component factories registered", "count", len(factories), "factories", factories)

	serviceRegistry := service.NewServiceRegistry()
	if err := service.RegisterAll(serviceRegistry); err != nil {
		return nil, nil, fmt.Errorf("register services: %w", err)
	}

	manager := service.NewServiceManager(serviceRegistry)
	ensureServiceManagerConfig(cfg)

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
	slog.Debug("Signal handling setup complete")

	slog.Info("About to start all services")
	if err := manager.StartAll(signalCtx); err != nil {
		return fmt.Errorf("start services: %w", err)
	}
	slog.Info("StartAll completed successfully")
	slog.Info("SemStreams started successfully (semantic stream processing ready)")

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

// createCoreDependencies creates the core dependencies needed by services
func createCoreDependencies(
	cfg *config.Config,
) (*natsclient.Client, *metric.MetricsRegistry, types.PlatformMeta, error) {
	// Create NATS client
	natsURL := "nats://localhost:4222"

	// Environment variable override takes precedence
	if envURL := os.Getenv("SEMSTREAMS_NATS_URLS"); envURL != "" {
		natsURL = envURL
	} else if len(cfg.NATS.URLs) > 0 {
		natsURL = cfg.NATS.URLs[0]
	}

	natsClient, err := natsclient.NewClient(natsURL)
	if err != nil {
		return nil, nil, types.PlatformMeta{}, fmt.Errorf("create NATS client: %w", err)
	}

	// Create metrics registry
	metricsRegistry := metric.NewMetricsRegistry()

	// Extract platform identity (prefer instance_id for federation)
	platformID := cfg.Platform.InstanceID
	if platformID == "" {
		platformID = cfg.Platform.ID
	}

	platform := types.PlatformMeta{
		Org:      cfg.Platform.Org, // From config, required field
		Platform: platformID,       // InstanceID or ID
	}

	slog.Info("Platform identity configured",
		"org", platform.Org,
		"platform", platform.Platform,
		"environment", cfg.Platform.Environment)

	return natsClient, metricsRegistry, platform, nil
}

// shutdown performs graceful shutdown of all services
func shutdown(ctx context.Context, manager *service.Manager, timeout time.Duration) error {
	// Calculate timeout from context
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}

	// Stop all services in reverse order
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

// loadConfig loads configuration from the specified file path
func loadConfig(path string) (*config.Config, error) {
	loader := config.NewLoader()
	cfg, err := loader.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}

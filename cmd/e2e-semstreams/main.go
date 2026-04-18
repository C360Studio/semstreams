// Package main provides the E2E test application for SemStreams.
// This application imports semstreams as a library, registering core and
// example components for tiered E2E testing.
package main

import (
	"context"
	"encoding/json"
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

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/examples/processors/document"
	iotsensor "github.com/c360studio/semstreams/examples/processors/iot_sensor"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
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

	// Register the global agentic tool executors. Mirrors
	// cmd/semstreams/main.go — explicit boot-time wiring so the
	// agentic-tools component stays a pure tool-execution endpoint.
	executors.RegisterAll(ctx, natsClient, platform, logger)

	componentRegistry, manager, err := setupRegistriesAndManager(cfg)
	if err != nil {
		return err
	}

	svcDeps := createServiceDependencies(natsClient, metricsRegistry, logger, platform, configManager, componentRegistry)

	if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {
		return err
	}

	return runWithSignalHandling(ctx, manager, cliCfg.ShutdownTimeout)
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
}

func parseCLI() (*CLIConfig, bool, error) {
	cliCfg := &CLIConfig{
		ConfigPath:      getEnvOrDefault("SEMSTREAMS_CONFIG", "config.json"),
		LogLevel:        getEnvOrDefault("SEMSTREAMS_LOG_LEVEL", "info"),
		LogFormat:       getEnvOrDefault("SEMSTREAMS_LOG_FORMAT", "text"),
		Debug:           os.Getenv("SEMSTREAMS_DEBUG") == "true",
		DebugPort:       6060,
		ShutdownTimeout: 30 * time.Second,
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

func runWithSignalHandling(ctx context.Context, manager *service.Manager, shutdownTimeout time.Duration) error {
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
	return nil
}

func startPProfServer(port int) {
	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Starting pprof server on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
		fmt.Printf("pprof server error: %v\n", err)
	}
}

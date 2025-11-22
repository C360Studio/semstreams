// Package main provides the E2E test CLI for StreamKit core components
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	// StreamKit E2E infrastructure
	"github.com/c360/semstreams/test/e2e/client"
	"github.com/c360/semstreams/test/e2e/config"
	scenarios "github.com/c360/semstreams/test/e2e/scenarios"
)

var (
	// Version information (set by build)
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	// Parse command-line flags
	flags := parseCommandLineFlags()

	// Handle version and list commands
	if handleVersionCommand(flags.showVersion) {
		return
	}
	if handleListCommand(flags.listScenarios) {
		return
	}

	// Setup logger
	logger := setupLogger(flags.verbose)

	// Create clients and setup context
	edgeClient, cloudClient, ctx := setupClientsAndContext(logger, flags.baseURL, flags.cloudURL)

	// Run scenarios and exit
	exitCode := runScenarios(ctx, logger, edgeClient, cloudClient, flags)
	os.Exit(exitCode)
}

// cliFlags holds parsed command-line flags
type cliFlags struct {
	scenarioName  string
	verbose       bool
	baseURL       string
	cloudURL      string
	udpEndpoint   string
	wsEndpoint    string
	showVersion   bool
	listScenarios bool
}

// parseCommandLineFlags parses and returns command-line flags
func parseCommandLineFlags() *cliFlags {
	flags := &cliFlags{}

	flag.StringVar(&flags.scenarioName, "scenario", "",
		"Run specific scenario (core-health, core-dataflow, core-federation, or 'all')")
	flag.BoolVar(&flags.verbose, "verbose", false, "Enable verbose logging")
	flag.StringVar(&flags.baseURL, "base-url", config.DefaultEndpoints.HTTP, "StreamKit HTTP endpoint (edge)")
	flag.StringVar(&flags.cloudURL, "cloud-url", "http://localhost:8081",
		"StreamKit cloud HTTP endpoint (federation only)")
	flag.StringVar(&flags.udpEndpoint, "udp-endpoint", config.DefaultEndpoints.UDP, "UDP test endpoint")
	flag.StringVar(&flags.wsEndpoint, "ws-endpoint", "ws://localhost:8082/stream",
		"WebSocket endpoint (federation only)")
	flag.BoolVar(&flags.showVersion, "version", false, "Show version information")
	flag.BoolVar(&flags.listScenarios, "list", false, "List available scenarios")

	// Support environment variables for Docker Compose
	if envURL := os.Getenv("STREAMKIT_BASE_URL"); envURL != "" {
		flags.baseURL = envURL
	}
	if envUDP := os.Getenv("UDP_ENDPOINT"); envUDP != "" {
		flags.udpEndpoint = envUDP
	}

	flag.Parse()
	return flags
}

// handleVersionCommand shows version information and returns true if version flag is set
func handleVersionCommand(showVersion bool) bool {
	if !showVersion {
		return false
	}

	fmt.Printf("StreamKit E2E Test Runner\n")
	fmt.Printf("Version: %s\n", version)
	fmt.Printf("Commit:  %s\n", commit)
	fmt.Printf("Date:    %s\n", date)
	return true
}

// handleListCommand shows available scenarios and returns true if list flag is set
func handleListCommand(listScenarios bool) bool {
	if !listScenarios {
		return false
	}

	fmt.Println("Available scenarios:")
	fmt.Println("\nProtocol Layer:")
	fmt.Printf("  core-health         - Validates core component health (UDP, JSONFilter, JSONMap, File, HTTP POST, WebSocket)\n")
	fmt.Printf("  core-dataflow       - Tests complete data pipeline: UDP → JSONFilter → JSONMap → File\n")
	fmt.Printf("  core-federation     - Tests federation: Edge (UDP → WebSocket Out) → Cloud (WebSocket In → File)\n")
	fmt.Println("\nSemantic Layer:")
	fmt.Printf("  semantic-basic       - Basic semantic processing: UDP → JSONGeneric → Graph Processor\n")
	fmt.Printf("  semantic-indexes     - Core semantic indexes (fast, no external dependencies)\n")
	fmt.Printf("  semantic-kitchen-sink - Comprehensive semantic: Indexes + Embedding + Metrics + HTTP Gateway\n")
	fmt.Println("\nRule Processor:")
	fmt.Printf("  rules-graph          - Rule → Graph integration with EnableGraphIntegration flag\n")
	fmt.Printf("  rules-performance    - Load testing (throughput, latency, stability)\n")
	fmt.Println("\nTest Suites:")
	fmt.Printf("  all                 - Runs all core scenarios (excludes federation and kitchen sink)\n")
	fmt.Printf("  semantic            - Runs all semantic scenarios\n")
	fmt.Printf("  rules               - Runs all rule processor scenarios\n")
	return true
}

// setupLogger creates and configures the logger
func setupLogger(verbose bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))
	slog.SetDefault(logger)
	return logger
}

// setupClientsAndContext creates clients and sets up signal handling
func setupClientsAndContext(logger *slog.Logger, baseURL, cloudURL string) (
	*client.ObservabilityClient,
	*client.ObservabilityClient,
	context.Context,
) {
	edgeClient := client.NewObservabilityClient(baseURL)
	cloudClient := client.NewObservabilityClient(cloudURL)

	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Received interrupt signal, shutting down...")
		cancel()
	}()

	return edgeClient, cloudClient, ctx
}

// runScenarios runs the appropriate scenarios based on flags
func runScenarios(
	ctx context.Context,
	logger *slog.Logger,
	edgeClient, cloudClient *client.ObservabilityClient,
	flags *cliFlags,
) int {
	logger.Info("Connecting to StreamKit",
		"base_url", flags.baseURL,
		"udp_endpoint", flags.udpEndpoint,
	)

	if flags.scenarioName == "" || flags.scenarioName == "all" {
		logger.Info("Running all core scenarios...")
		return runAllScenarios(ctx, logger, edgeClient, flags.udpEndpoint)
	} else if flags.scenarioName == "semantic" {
		logger.Info("Running all semantic scenarios...")
		return runSemanticScenarios(ctx, logger, edgeClient, flags.udpEndpoint)
	} else if flags.scenarioName == "rules" {
		logger.Info("Running all rule processor scenarios...")
		return runRulesScenarios(ctx, logger, edgeClient, flags.udpEndpoint)
	}

	// Run specific scenario
	scenario := createScenario(flags.scenarioName, edgeClient, cloudClient, flags.udpEndpoint, flags.wsEndpoint)
	if scenario == nil {
		logger.Error("Unknown scenario", "name", flags.scenarioName)
		fmt.Println("\nAvailable scenarios:")
		fmt.Println("  core-health            - Validates core component health")
		fmt.Println("  core-dataflow          - Tests complete data pipeline")
		fmt.Println("  core-federation        - Tests edge-to-cloud federation")
		fmt.Println("  semantic-basic         - Basic semantic processing")
		fmt.Println("  semantic-indexes       - Core semantic indexes (fast)")
		fmt.Println("  semantic-kitchen-sink  - Comprehensive semantic stack")
		fmt.Println("  rules-graph            - Rule → Graph integration")
		fmt.Println("  rules-performance      - Rule processor load testing")
		return 1
	}

	logger.Info("Running scenario", "name", flags.scenarioName)
	return runScenario(ctx, logger, scenario)
}

// createScenario creates a specific scenario by name
func createScenario(
	name string,
	edgeClient *client.ObservabilityClient,
	cloudClient *client.ObservabilityClient,
	udpEndpoint string,
	wsEndpoint string,
) scenarios.Scenario {
	switch name {
	case "core-health", "health":
		return scenarios.NewCoreHealthScenario(edgeClient, nil)
	case "core-dataflow", "dataflow":
		return scenarios.NewCoreDataflowScenario(edgeClient, udpEndpoint, nil)
	case "core-federation", "federation":
		return scenarios.NewCoreFederationScenario(edgeClient, cloudClient, udpEndpoint, wsEndpoint, nil)
	case "semantic-basic", "basic":
		return scenarios.NewSemanticBasicScenario(edgeClient, udpEndpoint, nil)
	case "semantic-indexes", "indexes":
		return scenarios.NewSemanticIndexesScenario(edgeClient, udpEndpoint, nil)
	case "semantic-kitchen-sink", "kitchen-sink", "kitchen":
		return scenarios.NewSemanticKitchenSinkScenario(edgeClient, udpEndpoint, nil)
	case "rules-graph", "rules-graph-integration":
		return scenarios.NewRulesGraphScenario(edgeClient, udpEndpoint, nil)
	case "rules-performance", "rules-perf":
		return scenarios.NewRulesPerformanceScenario(edgeClient, udpEndpoint, nil)
	default:
		return nil
	}
}

// runScenario executes a single scenario
func runScenario(ctx context.Context, logger *slog.Logger, scenario scenarios.Scenario) int {
	logger.Info("Setting up scenario", "name", scenario.Name())

	if err := scenario.Setup(ctx); err != nil {
		logger.Error("Scenario setup failed", "error", err)
		return 1
	}

	logger.Info("Executing scenario", "name", scenario.Name())
	result, err := scenario.Execute(ctx)

	// Always cleanup
	logger.Info("Tearing down scenario", "name", scenario.Name())
	if teardownErr := scenario.Teardown(ctx); teardownErr != nil {
		logger.Warn("Teardown failed", "error", teardownErr)
	}

	if err != nil {
		logger.Error("Scenario failed", "error", err)
		return 1
	}

	if !result.Success {
		logger.Error("Scenario completed with failure",
			"error", result.Error,
			"duration", result.Duration)
		return 1
	}

	logger.Info("Scenario completed successfully",
		"duration", result.Duration,
		"metrics", result.Metrics)

	return 0
}

// runAllScenarios executes all core scenarios
func runAllScenarios(
	ctx context.Context,
	logger *slog.Logger,
	obsClient *client.ObservabilityClient,
	udpEndpoint string,
) int {
	tests := []scenarios.Scenario{
		scenarios.NewCoreHealthScenario(obsClient, nil),
		scenarios.NewCoreDataflowScenario(obsClient, udpEndpoint, nil),
	}

	passed := 0
	failed := 0

	for _, scenario := range tests {
		logger.Info("Running scenario", "name", scenario.Name())
		exitCode := runScenario(ctx, logger, scenario)

		if exitCode == 0 {
			passed++
			logger.Info("Scenario PASSED", "name", scenario.Name())
		} else {
			failed++
			logger.Error("Scenario FAILED", "name", scenario.Name())
		}
	}

	logger.Info("Test suite complete",
		"passed", passed,
		"failed", failed,
		"total", len(tests))

	if failed > 0 {
		return 1
	}
	return 0
}

// runSemanticScenarios executes all semantic scenarios
func runSemanticScenarios(
	ctx context.Context,
	logger *slog.Logger,
	obsClient *client.ObservabilityClient,
	udpEndpoint string,
) int {
	tests := []scenarios.Scenario{
		scenarios.NewSemanticBasicScenario(obsClient, udpEndpoint, nil),
		scenarios.NewSemanticIndexesScenario(obsClient, udpEndpoint, nil),
		scenarios.NewSemanticKitchenSinkScenario(obsClient, udpEndpoint, nil),
	}

	passed := 0
	failed := 0

	for _, scenario := range tests {
		logger.Info("Running semantic scenario", "name", scenario.Name())
		exitCode := runScenario(ctx, logger, scenario)

		if exitCode == 0 {
			passed++
			logger.Info("Semantic scenario PASSED", "name", scenario.Name())
		} else {
			failed++
			logger.Error("Semantic scenario FAILED", "name", scenario.Name())
		}
	}

	logger.Info("Semantic test suite complete",
		"passed", passed,
		"failed", failed,
		"total", len(tests))

	if failed > 0 {
		return 1
	}
	return 0
}

// runRulesScenarios executes all rule processor scenarios
func runRulesScenarios(
	ctx context.Context,
	logger *slog.Logger,
	obsClient *client.ObservabilityClient,
	udpEndpoint string,
) int {
	tests := []scenarios.Scenario{
		scenarios.NewRulesGraphScenario(obsClient, udpEndpoint, nil),
		scenarios.NewRulesPerformanceScenario(obsClient, udpEndpoint, nil),
	}

	passed := 0
	failed := 0

	for _, scenario := range tests {
		logger.Info("Running rule processor scenario", "name", scenario.Name())
		exitCode := runScenario(ctx, logger, scenario)

		if exitCode == 0 {
			passed++
			logger.Info("Rule processor scenario PASSED", "name", scenario.Name())
		} else {
			failed++
			logger.Error("Rule processor scenario FAILED", "name", scenario.Name())
		}
	}

	logger.Info("Rule processor test suite complete",
		"passed", passed,
		"failed", failed,
		"total", len(tests))

	if failed > 0 {
		return 1
	}
	return 0
}

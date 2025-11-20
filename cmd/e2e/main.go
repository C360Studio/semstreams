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
	// Command-line flags
	var (
		scenarioName = flag.String(
			"scenario",
			"",
			"Run specific scenario (core-health, core-dataflow, core-federation, or 'all')",
		)
		verbose  = flag.Bool("verbose", false, "Enable verbose logging")
		baseURL  = flag.String("base-url", config.DefaultEndpoints.HTTP, "StreamKit HTTP endpoint (edge)")
		cloudURL = flag.String(
			"cloud-url",
			"http://localhost:8081",
			"StreamKit cloud HTTP endpoint (federation only)",
		)
		udpEndpoint   = flag.String("udp-endpoint", config.DefaultEndpoints.UDP, "UDP test endpoint")
		wsEndpoint    = flag.String("ws-endpoint", "ws://localhost:8082/stream", "WebSocket endpoint (federation only)")
		showVersion   = flag.Bool("version", false, "Show version information")
		listScenarios = flag.Bool("list", false, "List available scenarios")
	)

	// Support environment variables for Docker Compose
	if envURL := os.Getenv("STREAMKIT_BASE_URL"); envURL != "" {
		*baseURL = envURL
	}
	if envUDP := os.Getenv("UDP_ENDPOINT"); envUDP != "" {
		*udpEndpoint = envUDP
	}

	flag.Parse()

	// Show version
	if *showVersion {
		fmt.Printf("StreamKit E2E Test Runner\n")
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Commit:  %s\n", commit)
		fmt.Printf("Date:    %s\n", date)
		os.Exit(0)
	}

	// List scenarios
	if *listScenarios {
		fmt.Println("Available scenarios:")
		fmt.Println("\nProtocol Layer:")
		fmt.Printf(
			"  core-health         - Validates core component health (UDP, JSONFilter, JSONMap, File, HTTP POST, WebSocket)\n",
		)
		fmt.Printf("  core-dataflow       - Tests complete data pipeline: UDP → JSONFilter → JSONMap → File\n")
		fmt.Printf(
			"  core-federation     - Tests federation: Edge (UDP → WebSocket Out) → Cloud (WebSocket In → File)\n",
		)
		fmt.Println("\nSemantic Layer:")
		fmt.Printf("  semantic-basic       - Basic semantic processing: UDP → JSONGeneric → Graph Processor\n")
		fmt.Printf("  semantic-indexes     - Core semantic indexes (fast, no external dependencies)\n")
		fmt.Printf(
			"  semantic-kitchen-sink - Comprehensive semantic: Indexes + Embedding + Metrics + HTTP Gateway\n",
		)
		fmt.Println("\nRule Processor:")
		fmt.Printf("  rules-graph          - Rule → Graph integration with EnableGraphIntegration flag\n")
		fmt.Printf("  rules-performance    - Load testing (throughput, latency, stability)\n")
		fmt.Println("\nTest Suites:")
		fmt.Printf("  all                 - Runs all core scenarios (excludes federation and kitchen sink)\n")
		fmt.Printf("  semantic            - Runs all semantic scenarios\n")
		fmt.Printf("  rules               - Runs all rule processor scenarios\n")
		os.Exit(0)
	}

	// Setup logger
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	// Create observability clients
	edgeClient := client.NewObservabilityClient(*baseURL)
	cloudClient := client.NewObservabilityClient(*cloudURL)

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Received interrupt signal, shutting down...")
		cancel()
	}()

	// Connect to running StreamKit instance
	logger.Info("Connecting to StreamKit",
		"base_url", *baseURL,
		"udp_endpoint", *udpEndpoint,
	)

	// Run scenarios
	var exitCode int
	if *scenarioName == "" || *scenarioName == "all" {
		// Run all core scenarios (excludes federation and kitchen sink)
		logger.Info("Running all core scenarios...")
		exitCode = runAllScenarios(ctx, logger, edgeClient, *udpEndpoint)
	} else if *scenarioName == "semantic" {
		// Run all semantic scenarios
		logger.Info("Running all semantic scenarios...")
		exitCode = runSemanticScenarios(ctx, logger, edgeClient, *udpEndpoint)
	} else if *scenarioName == "rules" {
		// Run all rule processor scenarios
		logger.Info("Running all rule processor scenarios...")
		exitCode = runRulesScenarios(ctx, logger, edgeClient, *udpEndpoint)
	} else {
		// Run specific scenario
		scenario := createScenario(*scenarioName, edgeClient, cloudClient, *udpEndpoint, *wsEndpoint)
		if scenario == nil {
			logger.Error("Unknown scenario", "name", *scenarioName)
			fmt.Println("\nAvailable scenarios:")
			fmt.Println("  core-health            - Validates core component health")
			fmt.Println("  core-dataflow          - Tests complete data pipeline")
			fmt.Println("  core-federation        - Tests edge-to-cloud federation")
			fmt.Println("  semantic-basic         - Basic semantic processing")
			fmt.Println("  semantic-indexes       - Core semantic indexes (fast)")
			fmt.Println("  semantic-kitchen-sink  - Comprehensive semantic stack")
			fmt.Println("  rules-graph            - Rule → Graph integration")
			fmt.Println("  rules-performance      - Rule processor load testing")
			exitCode = 1
		} else {
			logger.Info("Running scenario", "name", *scenarioName)
			exitCode = runScenario(ctx, logger, scenario)
		}
	}

	os.Exit(exitCode)
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

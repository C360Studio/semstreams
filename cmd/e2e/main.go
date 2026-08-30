// Package main provides the E2E test CLI for SemStreams core components
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// SemStreams E2E infrastructure
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/config"
	"github.com/c360studio/semstreams/test/e2e/results"
	scenarios "github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/c360studio/semstreams/test/e2e/scenarios/agentic"
	crudtools "github.com/c360studio/semstreams/test/e2e/scenarios/crud-tools"
	deepresearch "github.com/c360studio/semstreams/test/e2e/scenarios/deep-research"
	lessonsscenario "github.com/c360studio/semstreams/test/e2e/scenarios/lessons"
	lifecyclescenario "github.com/c360studio/semstreams/test/e2e/scenarios/lifecycle"
	opsscenario "github.com/c360studio/semstreams/test/e2e/scenarios/ops"
	researchgraph "github.com/c360studio/semstreams/test/e2e/scenarios/research-graph"
	"github.com/c360studio/semstreams/test/e2e/scenarios/throughput"
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

	// Handle compare command
	if flags.compare {
		exitCode := handleCompareCommand(logger, flags.outputDir)
		os.Exit(exitCode)
	}

	// Handle compare-tiers command
	if flags.compareTiers {
		exitCode := handleCompareTiersCommand(logger, flags.outputDir)
		os.Exit(exitCode)
	}

	// Handle structured comparison command
	if flags.compareStructured {
		if flags.baselineFile != "" && flags.targetFile != "" {
			// Compare specific files
			exitCode := handleCompareFilesCommand(logger, flags.baselineFile, flags.targetFile)
			os.Exit(exitCode)
		}
		if flags.baselineVariant != "" && flags.targetVariant != "" {
			// Auto-find latest files for each variant
			exitCode := handleAutoCompareCommand(logger, flags.outputDir, flags.baselineVariant, flags.targetVariant)
			os.Exit(exitCode)
		}
		logger.Error("compare-structured requires either --baseline/--target or --baseline-variant/--target-variant")
		os.Exit(1)
	}

	// Create client and setup context
	edgeClient, ctx := setupClientsAndContext(logger, flags.baseURL)

	// Run scenarios and exit
	exitCode := runScenarios(ctx, logger, edgeClient, flags)
	os.Exit(exitCode)
}

// cliFlags holds parsed command-line flags
type cliFlags struct {
	scenarioName  string
	verbose       bool
	baseURL       string
	udpEndpoint   string
	showVersion   bool
	listScenarios bool
	// Tiered test variant flags
	variant      string // "structural", "statistical", or "semantic"
	outputDir    string // Directory for results output
	compare      bool   // Generate comparison report from existing results
	compareTiers bool   // Generate tier comparison report (0 vs 1 vs 2)
	metricsURL   string // Prometheus metrics endpoint URL
	// Structured comparison flags
	compareStructured bool   // Compare two structured result files
	baselineFile      string // Baseline structured result file
	targetFile        string // Target structured result file
	baselineVariant   string // Baseline variant for auto-compare
	targetVariant     string // Target variant for auto-compare
	// WebSocket status stream endpoint (uses baseURL by default)
	wsStatusURL string
	// Throughput scenario options
	messageCount int
	graphqlURL   string
	profileAll   bool
	// Throughput SLA and advanced options
	uniqueEntities       int
	queryDuringIngestion bool
	maxQueryP99Ms        float64
	maxQueryErrorRate    float64
}

// parseCommandLineFlags parses and returns command-line flags
func parseCommandLineFlags() *cliFlags {
	flags := &cliFlags{}

	flag.StringVar(&flags.scenarioName, "scenario", "",
		"Run specific scenario (core-health, core-dataflow, core-graph-roundtrip, lessons, or 'all')")
	flag.BoolVar(&flags.verbose, "verbose", false, "Enable verbose logging")
	flag.StringVar(&flags.baseURL, "base-url", config.DefaultEndpoints.HTTP, "SemStreams HTTP endpoint (edge)")
	flag.StringVar(&flags.udpEndpoint, "udp-endpoint", config.DefaultEndpoints.UDP, "UDP test endpoint")
	flag.BoolVar(&flags.showVersion, "version", false, "Show version information")
	flag.BoolVar(&flags.listScenarios, "list", false, "List available scenarios")
	// Tiered test variant flags
	flag.StringVar(&flags.variant, "variant", "",
		"Test variant: structural (rules-only), statistical (BM25), semantic (neural+LLM)")
	flag.StringVar(&flags.outputDir, "output-dir", "",
		"Directory for saving results JSON (empty=no output)")
	flag.BoolVar(&flags.compare, "compare", false,
		"Generate comparison report from existing results in output-dir")
	flag.BoolVar(&flags.compareTiers, "compare-tiers", false,
		"Generate tier comparison report (Tier 0 vs 1 vs 2) from existing results")
	flag.StringVar(&flags.metricsURL, "metrics-url", config.DefaultEndpoints.Metrics,
		"Prometheus metrics endpoint URL")
	// Structured comparison flags
	flag.BoolVar(&flags.compareStructured, "compare-structured", false,
		"Compare two structured result files (requires --baseline and --target)")
	flag.StringVar(&flags.baselineFile, "baseline", "",
		"Baseline structured result file for comparison")
	flag.StringVar(&flags.targetFile, "target", "",
		"Target structured result file for comparison")
	flag.StringVar(&flags.baselineVariant, "baseline-variant", "",
		"Baseline variant for auto-compare (finds latest file)")
	flag.StringVar(&flags.targetVariant, "target-variant", "",
		"Target variant for auto-compare (finds latest file)")
	flag.StringVar(&flags.wsStatusURL, "ws-status-url", "",
		"WebSocket status stream base URL (defaults to base-url)")
	// Throughput scenario options
	flag.IntVar(&flags.messageCount, "message-count", 10000,
		"Number of messages for throughput scenario")
	flag.StringVar(&flags.graphqlURL, "graphql-url", "http://localhost:38080/graph-gateway/graphql",
		"GraphQL endpoint for throughput query load (empty to skip query phase)")
	flag.BoolVar(&flags.profileAll, "profile-all", false,
		"Capture all profile types including block and mutex (throughput scenario)")
	flag.IntVar(&flags.uniqueEntities, "unique-entities", 0,
		"Generate N unique synthetic entities (0 = cycle testdata)")
	flag.BoolVar(&flags.queryDuringIngestion, "query-during-ingestion", false,
		"Run queries concurrently with message ingestion")
	flag.Float64Var(&flags.maxQueryP99Ms, "max-query-p99-ms", 0,
		"Fail if query P99 latency exceeds this threshold in ms (0 = disabled)")
	flag.Float64Var(&flags.maxQueryErrorRate, "max-query-error-rate", 0,
		"Fail if query error rate exceeds this ratio (0 = disabled, 0.05 = 5%)")

	// Support environment variables for Docker Compose
	if envURL := os.Getenv("SEMSTREAMS_BASE_URL"); envURL != "" {
		flags.baseURL = envURL
	}
	if envUDP := os.Getenv("UDP_ENDPOINT"); envUDP != "" {
		flags.udpEndpoint = envUDP
	}
	if envVariant := os.Getenv("E2E_VARIANT"); envVariant != "" {
		flags.variant = envVariant
	}
	if envOutput := os.Getenv("E2E_OUTPUT_DIR"); envOutput != "" {
		flags.outputDir = envOutput
	}

	flag.Parse()
	return flags
}

// handleVersionCommand shows version information and returns true if version flag is set
func handleVersionCommand(showVersion bool) bool {
	if !showVersion {
		return false
	}

	fmt.Printf("SemStreams E2E Test Runner\n")
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

	fmt.Println("Available E2E Tasks (task e2e:<tier>):")
	fmt.Println("")
	fmt.Println("  e2e:core        - Platform boots, data flows (~10s)")
	fmt.Println("  e2e:structural  - Rules + structural inference (~30s)")
	fmt.Println("  e2e:statistical - BM25 + community detection (~60s)")
	fmt.Println("  e2e:semantic    - Neural embeddings + LLM (~90s)")
	fmt.Println("  e2e:agentic     - Agent loop + tools with mock LLM (~30s)")
	fmt.Println("  e2e:lessons     - Direct product lesson birth/lifecycle/reader-matcher gate")
	fmt.Println("  e2e:research-graph - ADR-045 direct + walk_seeds R0-R6 paths (~60s)")
	fmt.Println("")
	fmt.Println("Individual Scenarios:")
	fmt.Println("")
	fmt.Println("  Core:")
	fmt.Println("    core-health     - Component health checks")
	fmt.Println("    core-dataflow   - UDP → Filter → Map → File pipeline")
	fmt.Println("    core-graph-roundtrip - Projection write → ENTITY_STATES/index → GraphQL read")
	fmt.Println("    core-slow-consumer - Assembled slow-consumer attribution")
	fmt.Println("")
	fmt.Println("  Tiered (unified scenario with --variant flag):")
	fmt.Println("    tiered --variant structural  - Rules-only, ZERO embeddings/clusters")
	fmt.Println("    tiered --variant statistical - BM25 embeddings, no external ML")
	fmt.Println("    tiered --variant semantic    - Neural embeddings + LLM summaries")
	fmt.Println("")
	fmt.Println("  Agentic:")
	fmt.Println("    agentic         - Agent loop, model, and tools E2E test")
	fmt.Println("                      Uses mock LLM by default (CI-friendly)")
	fmt.Println("                      Override with AGENTIC_LLM_URL for real LLM")
	fmt.Println("    deep-research   - Rules-driven multi-agent research flow")
	fmt.Println("                      Requires rule-processor with deep-research rules")
	fmt.Println("    research-graph  - ADR-045 Phase 1 R0-R6 chain end-to-end")
	fmt.Println("                      Mock LLM scripted with synthesize_directly happy path")
	fmt.Println("    research-graph-execute - ADR-045 walk_seeds execute/fusion proof")
	fmt.Println("")
	fmt.Println("  Lessons:")
	fmt.Println("    lessons         - Direct product birth, promotion, matcher eligibility, and recreate convergence")
	fmt.Println("")
	fmt.Println("  Lifecycle (ADR-047):")
	fmt.Println("    lifecycle       - Lifecycle-gateway + rule-engine + Manager round-trip")
	fmt.Println("                      Uses cmd/e2e-semstreams with the mission workflow")
	fmt.Println("")
	fmt.Println("  Throughput (for profiling):")
	fmt.Println("    throughput       - High-volume stress test (10,000+ messages)")
	fmt.Println("                       Captures pprof profiles when SEMSTREAMS_DEBUG=true")
	fmt.Println("                       Use --message-count to adjust volume")
	fmt.Println("")
	fmt.Println("Variant flag (for tiered scenario):")
	fmt.Println("  --variant structural  - Rules-only, validates ZERO ML inference")
	fmt.Println("  --variant statistical - BM25 fallback, no external ML services")
	fmt.Println("  --variant semantic    - Full ML stack (SemEmbed + SemInstruct)")
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

// setupClientsAndContext creates the client and sets up signal handling
func setupClientsAndContext(logger *slog.Logger, baseURL string) (
	*client.ObservabilityClient,
	context.Context,
) {
	edgeClient := client.NewObservabilityClient(baseURL)

	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Received interrupt signal, shutting down...")
		cancel()
	}()

	return edgeClient, ctx
}

// runScenarios runs the appropriate scenarios based on flags
func runScenarios(
	ctx context.Context,
	logger *slog.Logger,
	edgeClient *client.ObservabilityClient,
	flags *cliFlags,
) int {
	logger.Info("Connecting to SemStreams",
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
	scenario := createScenario(edgeClient, flags)
	if scenario == nil {
		logger.Error("Unknown scenario", "name", flags.scenarioName)
		fmt.Println("\nRun with --list to see all available scenarios")
		return 1
	}

	logger.Info("Running scenario", "name", flags.scenarioName)
	return runScenario(ctx, logger, scenario, flags)
}

// createScenario creates a specific scenario by name.
//
// Tiered scenario supports three variants:
//   - structural  → rules-only, ZERO embeddings/clusters
//   - statistical → BM25 embeddings, no external ML
//   - semantic    → neural embeddings + LLM summaries
//
// Legacy variant names are supported for backwards compatibility:
//   - core → statistical
//   - ml   → semantic
func createScenario(
	edgeClient *client.ObservabilityClient,
	flags *cliFlags,
) scenarios.Scenario {
	switch flags.scenarioName {
	// Core scenarios
	case "core-health", "health":
		return scenarios.NewCoreHealthScenario(edgeClient, nil)
	case "core-dataflow", "dataflow":
		// Create WebSocket client for status stream verification
		var wsClient *client.WebSocketClient
		wsURL := flags.wsStatusURL
		if wsURL == "" {
			wsURL = flags.baseURL // Default to same base URL
		}
		wsClient = client.NewWebSocketClient(wsURL)
		return scenarios.NewCoreDataflowScenario(edgeClient, wsClient, flags.udpEndpoint, nil)
	case "core-graph-roundtrip", "graph-roundtrip":
		// The core stack runs configs/protocol-flow.json. Its platform.org /
		// platform.id are the STEM of the authority the graph accepts (ADR-102
		// d5); the pair itself carries the entropy suffix the framework minted
		// at first boot (ADR-104), so the probe reads it from
		// semstreams_config/platform_identity and uses this value to check it is
		// driving the configuration it was pointed at.
		return scenarios.NewGraphRoundTripScenario(
			config.DefaultEndpoints.NATS,
			flags.baseURL,
			strings.TrimRight(flags.baseURL, "/")+"/graph-gateway/graphql",
			config.CoreAuthorityStem,
		)
	case "core-minted-authority", "minted-authority":
		// ADR-104: the deployment minted an entropy suffix onto platform.id at
		// first boot and recorded it. Everything else in the e2e tree now READS
		// that record; this stage is what proves the record is there and shaped
		// the way the cross-repo contract says.
		return scenarios.NewMintedAuthorityScenario(config.DefaultEndpoints.NATS, config.CoreAuthorityStem)
	case "core-pre-identity-seed":
		return scenarios.NewPreIdentityBucketScenario(
			config.DefaultEndpoints.NATS, "seed", config.CoreAuthorityStem)
	case "core-pre-identity-assert":
		return scenarios.NewPreIdentityBucketScenario(
			config.DefaultEndpoints.NATS, "assert", config.CoreAuthorityStem)
	case "core-slow-consumer", "slow-consumer":
		return scenarios.NewSlowConsumerAttributionScenario(scenarios.SlowConsumerAttributionConfig{
			AppContainer: "semstreams-e2e-slow-consumer-app",
			MetricsURL:   flags.metricsURL,
		})
	// Tiered scenario (unified: structural, statistical, semantic)
	case "tiered", "structural", "statistical", "semantic":
		cfg := scenarios.DefaultTieredConfig()
		cfg.MetricsURL = flags.metricsURL
		cfg.ServiceManagerURL = flags.baseURL
		cfg.GatewayURL = flags.baseURL + "/api-gateway"
		cfg.OutputDir = flags.outputDir
		// Set variant from flag or scenario name
		cfg.Variant = flags.variant
		if cfg.Variant == "" {
			// Allow scenario name to specify variant directly
			if flags.scenarioName == "structural" || flags.scenarioName == "statistical" || flags.scenarioName == "semantic" {
				cfg.Variant = flags.scenarioName
			}
		}
		// Set GraphQL URL based on variant — via ServiceManager shared mux
		if cfg.Variant == "semantic" {
			cfg.GraphQLURL = "http://localhost:38180/graph-gateway/graphql"
			// Semantic tier has the slowest entity-load pipeline:
			// neural embeddings, multiple ML services (semembed +
			// 3 seminstruct instances), and the longest file-loader
			// path. Under Docker pressure (parallel projects on the
			// host), the verify-entity-count critical-entity check
			// can need >60s before all PathRAG-test entities land.
			// 120s gives 2× the historical budget; steady-state
			// entity load on idle hosts is still well under 5s, so
			// this only matters on contended hosts.
			cfg.ValidationTimeout = 120 * time.Second
		} else {
			cfg.GraphQLURL = "http://localhost:38080/graph-gateway/graphql"
		}
		return scenarios.NewTieredScenario(edgeClient, flags.udpEndpoint, cfg)

	// Agentic scenario (agent loop, model, tools)
	case "agentic":
		cfg := agentic.DefaultConfig()
		cfg.MetricsURL = flags.metricsURL
		return agentic.NewScenario(edgeClient, cfg)

	// Deep-research scenario (rules-driven multi-agent research flow)
	case "deep-research":
		cfg := deepresearch.DefaultConfig()
		cfg.MetricsURL = flags.metricsURL
		return deepresearch.NewScenario(edgeClient, cfg)

	// Research-graph scenario (ADR-045 Phase 1 R0-R6 chain)
	case "research-graph":
		cfg := researchgraph.DefaultConfig()
		if flags.metricsURL != "" {
			cfg.MetricsURL = flags.metricsURL
		}
		return researchgraph.NewScenario(edgeClient, cfg)
	case "research-graph-execute":
		cfg := researchgraph.DefaultConfig()
		cfg.FixtureMode = researchgraph.FixtureModeExecute
		if flags.metricsURL != "" {
			cfg.MetricsURL = flags.metricsURL
		}
		return researchgraph.NewScenario(edgeClient, cfg)

	// CRUD-tools scenario (ADR-029 Pattern-B CRUD round-trip)
	case "crud-tools":
		cfg := crudtools.DefaultConfig()
		cfg.MetricsURL = flags.metricsURL
		return crudtools.NewScenario(edgeClient, cfg)

	// Direct-product lesson scenario (no agent loop or mock LLM)
	case "lessons":
		return lessonsscenario.NewScenario()

	// Ops scenario (ADR-027 Phase 1 ops agent observable)
	case "ops":
		cfg := opsscenario.DefaultConfig()
		cfg.MetricsURL = flags.metricsURL
		cfg.BaseURL = flags.baseURL
		return opsscenario.NewScenario(edgeClient, cfg)

	// Lifecycle scenario (ADR-047 — gateway + rule-engine round-trip)
	case "lifecycle":
		cfg := lifecyclescenario.DefaultConfig()
		if flags.baseURL != "" {
			cfg.BaseURL = flags.baseURL
		}
		if flags.udpEndpoint != "" {
			cfg.UDPEndpoint = flags.udpEndpoint
		}
		return lifecyclescenario.NewScenario(edgeClient, cfg)

	// Throughput scenario (high-volume stress test with profiling + query load)
	case "throughput":
		return newThroughputScenario(flags)

	default:
		return nil
	}
}

// newThroughputScenario builds the throughput scenario from CLI
// flags. Extracted from createScenario to keep that switch
// dispatch under revive's function-length limit (50 statements).
func newThroughputScenario(flags *cliFlags) scenarios.Scenario {
	cfg := throughput.DefaultConfig()
	cfg.MessageCount = flags.messageCount
	cfg.GraphQLURL = flags.graphqlURL
	cfg.ProfileAll = flags.profileAll
	cfg.UniqueEntities = flags.uniqueEntities
	cfg.QueryDuringIngestion = flags.queryDuringIngestion
	cfg.MaxQueryP99Ms = flags.maxQueryP99Ms
	cfg.MaxQueryErrorRate = flags.maxQueryErrorRate
	if flags.outputDir != "" {
		cfg.ProfileDir = flags.outputDir + "/profiles"
	}
	return throughput.NewScenario(flags.metricsURL, flags.udpEndpoint, cfg)
}

// runScenario executes a single scenario
func runScenario(ctx context.Context, logger *slog.Logger, scenario scenarios.Scenario, flags *cliFlags) int {
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
		logger.Error("Scenario failed", "error", err, "assertions_run", assertionsRun(result))
		return 1
	}

	if !result.Success {
		logger.Error("Scenario completed with failure",
			"error", result.Error,
			"duration", result.Duration,
			"assertions_run", result.AssertionsRun)
		return 1
	}

	logger.Info("Scenario completed successfully",
		"duration", result.Duration,
		"metrics", result.Metrics,
		"assertions_run", result.AssertionsRun)

	// Save structured results if output directory is specified and results exist
	if flags.outputDir != "" && result.Structured != nil {
		filepath, err := scenarios.SaveStructuredResults(result.Structured, flags.outputDir)
		if err != nil {
			logger.Warn("Failed to save structured results", "error", err)
		} else {
			logger.Info("Saved structured results", "file", filepath)
		}

		// Also save raw Prometheus metrics dump
		variant := flags.variant
		if variant == "" {
			variant = flags.scenarioName
		}
		metricsPath, err := saveMetricsDump(logger, flags.metricsURL, variant, flags.outputDir)
		if err != nil {
			logger.Warn("Failed to save metrics dump", "error", err)
		} else {
			logger.Info("Saved metrics dump", "file", metricsPath)
		}
	}

	return 0
}

func assertionsRun(result *scenarios.Result) int {
	if result == nil {
		return 0
	}
	return result.AssertionsRun
}

// saveMetricsDump fetches raw Prometheus metrics and saves them to a file
func saveMetricsDump(_ *slog.Logger, metricsURL, variant, outputDir string) (string, error) {
	// Fetch metrics from Prometheus endpoint
	metricsEndpoint := metricsURL + "/metrics"
	resp, err := http.Get(metricsEndpoint)
	if err != nil {
		return "", fmt.Errorf("failed to fetch metrics from %s: %w", metricsEndpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metrics endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read metrics response: %w", err)
	}

	// Save using the scenarios helper
	return scenarios.SaveMetricsDump(string(body), variant, outputDir)
}

// runAllScenarios executes all core scenarios
func runAllScenarios(
	ctx context.Context,
	logger *slog.Logger,
	obsClient *client.ObservabilityClient,
	udpEndpoint string,
) int {
	// Create WebSocket client for dataflow scenario
	// When running all scenarios, we use the default HTTP endpoint for WebSocket
	wsClient := client.NewWebSocketClient(config.DefaultEndpoints.HTTP)

	// `all` is every scenario the PRODUCTION binary serves. The graph
	// round-trip probe is not among them: it births a synthetic type
	// (test.fixture.v1) that only the e2e binary registers (ADR-103 — a
	// production-target tier stamps only what the production binary
	// registers), so `task e2e:core` runs it as its second phase against the
	// e2e-target app (`--scenario core-graph-roundtrip`).
	tests := []scenarios.Scenario{
		scenarios.NewCoreHealthScenario(obsClient, nil),
		scenarios.NewCoreDataflowScenario(obsClient, wsClient, udpEndpoint, nil),
	}

	passed := 0
	failed := 0

	for _, scenario := range tests {
		logger.Info("Running scenario", "name", scenario.Name())
		exitCode := runScenario(ctx, logger, scenario, &cliFlags{})

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
	// Run tiered scenario (covers all semantic functionality)
	cfg := scenarios.DefaultTieredConfig()
	tests := []scenarios.Scenario{
		scenarios.NewTieredScenario(obsClient, udpEndpoint, cfg),
	}

	passed := 0
	failed := 0

	for _, scenario := range tests {
		logger.Info("Running semantic scenario", "name", scenario.Name())
		exitCode := runScenario(ctx, logger, scenario, &cliFlags{})

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

// runRulesScenarios executes structural tier (rules-only) scenario
func runRulesScenarios(
	ctx context.Context,
	logger *slog.Logger,
	obsClient *client.ObservabilityClient,
	udpEndpoint string,
) int {
	// Run tiered scenario with structural variant
	cfg := scenarios.DefaultTieredConfig()
	cfg.Variant = "structural"
	tests := []scenarios.Scenario{
		scenarios.NewTieredScenario(obsClient, udpEndpoint, cfg),
	}

	passed := 0
	failed := 0

	for _, scenario := range tests {
		logger.Info("Running structural tier scenario", "name", scenario.Name())
		exitCode := runScenario(ctx, logger, scenario, &cliFlags{})

		if exitCode == 0 {
			passed++
			logger.Info("Structural tier scenario PASSED", "name", scenario.Name())
		} else {
			failed++
			logger.Error("Structural tier scenario FAILED", "name", scenario.Name())
		}
	}

	logger.Info("Structural tier test suite complete",
		"passed", passed,
		"failed", failed,
		"total", len(tests))

	if failed > 0 {
		return 1
	}
	return 0
}

// handleCompareCommand generates comparison report from existing results
func handleCompareCommand(logger *slog.Logger, outputDir string) int {
	if outputDir == "" {
		logger.Error("Output directory required for comparison (use --output-dir)")
		return 1
	}

	logger.Info("Generating comparison report", "output_dir", outputDir)

	writer := results.NewWriter(outputDir)

	// List all available runs
	files, err := writer.ListRuns()
	if err != nil {
		logger.Error("Failed to list runs", "error", err)
		return 1
	}

	if len(files) < 2 {
		logger.Warn("Need at least 2 test runs to compare", "found", len(files))
		return 1
	}

	// Find statistical and semantic variant runs (look for latest of each)
	var statisticalRun, semanticRun *results.TestRun
	for i := len(files) - 1; i >= 0; i-- {
		run, err := writer.LoadRun(files[i])
		if err != nil {
			logger.Warn("Failed to load run", "file", files[i], "error", err)
			continue
		}

		if run.Config.Variant == "statistical" && statisticalRun == nil {
			statisticalRun = run
		} else if run.Config.Variant == "semantic" && semanticRun == nil {
			semanticRun = run
		}

		if statisticalRun != nil && semanticRun != nil {
			break
		}
	}

	if statisticalRun == nil || semanticRun == nil {
		logger.Warn("Need both statistical and semantic variant runs to compare",
			"has_statistical", statisticalRun != nil,
			"has_semantic", semanticRun != nil)
		return 1
	}

	// Compare: baseline=statistical, current=semantic
	comparison := results.Compare(statisticalRun, semanticRun)

	// Write comparison report
	filepath, err := writer.WriteComparison(comparison)
	if err != nil {
		logger.Error("Failed to write comparison report", "error", err)
		return 1
	}

	// Print summary
	printComparisonSummary(logger, statisticalRun, semanticRun, comparison, filepath)

	return 0
}

// printComparisonSummary outputs a human-readable comparison
func printComparisonSummary(
	logger *slog.Logger,
	statisticalRun, semanticRun *results.TestRun,
	comparison *results.Comparison,
	filepath string,
) {
	fmt.Println("\n=== Statistical vs Semantic Variant Comparison ===")
	fmt.Printf("Statistical variant: %s\n", statisticalRun.Timestamp.Format(time.RFC3339))
	fmt.Printf("Semantic variant:    %s\n", semanticRun.Timestamp.Format(time.RFC3339))

	fmt.Println("\n--- Duration ---")
	fmt.Printf("Statistical: %s\n", statisticalRun.DurationStr)
	fmt.Printf("Semantic:    %s\n", semanticRun.DurationStr)

	fmt.Println("\n--- Success ---")
	fmt.Printf("Statistical: %d/%d passed (%.0f%%)\n",
		statisticalRun.Summary.PassedScenarios,
		statisticalRun.Summary.TotalScenarios,
		statisticalRun.Summary.SuccessRate*100)
	fmt.Printf("Semantic:    %d/%d passed (%.0f%%)\n",
		semanticRun.Summary.PassedScenarios,
		semanticRun.Summary.TotalScenarios,
		semanticRun.Summary.SuccessRate*100)

	fmt.Println("\n--- Overall Comparison ---")
	fmt.Printf("Status Changes:    %d\n", comparison.Overall.StatusChanges)
	fmt.Printf("Improvements:      %d\n", comparison.Overall.Improvements)
	fmt.Printf("Regressions:       %d\n", comparison.Overall.Regressions)
	fmt.Printf("Metrics Improved:  %d\n", comparison.Overall.MetricsImproved)
	fmt.Printf("Metrics Regressed: %d\n", comparison.Overall.MetricsRegressed)

	if len(comparison.Diffs) > 0 {
		fmt.Println("\n--- Scenario Diffs ---")
		for _, diff := range comparison.Diffs {
			status := "unchanged"
			if diff.StatusChanged {
				if diff.CurrentSuccess {
					status = "IMPROVED"
				} else {
					status = "REGRESSED"
				}
			}
			fmt.Printf("  %s: %s (duration delta: %dms)\n",
				diff.ScenarioName, status, diff.DurationChangeMs)
		}
	}

	logger.Info("Comparison report written", "file", filepath)
}

// handleCompareTiersCommand generates a tier comparison report (Tier 0 vs 1 vs 2)
func handleCompareTiersCommand(logger *slog.Logger, outputDir string) int {
	if outputDir == "" {
		outputDir = "test/e2e/results"
	}

	logger.Info("Generating tier comparison report", "output_dir", outputDir)

	// Build tier comparison report
	report := TierComparisonReport{
		GeneratedAt: time.Now(),
		OutputDir:   outputDir,
		Tiers:       make(map[string]TierMetrics),
	}

	// Define tier expectations
	tierExpectations := map[string]TierExpectation{
		"structural": {
			Name:               "Rules-Only",
			ExpectedEmbeddings: 0,
			ExpectedClusters:   0,
			ExpectedInference:  false,
		},
		"statistical": {
			Name:               "Native (BM25 + LPA)",
			ExpectedEmbeddings: -1, // Any non-zero
			ExpectedClusters:   -1, // Any non-zero
			ExpectedInference:  true,
		},
		"semantic": {
			Name:               "LLM (Neural + Summaries)",
			ExpectedEmbeddings: -1, // Any non-zero
			ExpectedClusters:   -1, // Any non-zero
			ExpectedInference:  true,
		},
	}

	// Print the report
	fmt.Println("\n=== Tier Comparison Report ===")
	fmt.Printf("Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Printf("Output Dir: %s\n\n", outputDir)

	fmt.Println("Tier Expectations:")
	fmt.Println("------------------")
	for tier, exp := range tierExpectations {
		embStr := "0"
		if exp.ExpectedEmbeddings < 0 {
			embStr = ">0"
		}
		clustStr := "0"
		if exp.ExpectedClusters < 0 {
			clustStr = ">0"
		}
		fmt.Printf("  %s (%s):\n", tier, exp.Name)
		fmt.Printf("    Embeddings: %s\n", embStr)
		fmt.Printf("    Clusters: %s\n", clustStr)
		fmt.Printf("    Inference: %v\n", exp.ExpectedInference)
	}

	fmt.Println("\nTo run all tiers and generate comparison data:")
	fmt.Println("  task e2e:tiers")
	fmt.Println("\nThis will run structural → statistical → semantic sequentially and output results.")

	// Save report to JSON
	tierReportFile := fmt.Sprintf("%s/tier-comparison-%s.json", outputDir, time.Now().Format("20060102-150405"))
	data, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		if err := os.WriteFile(tierReportFile, data, 0644); err == nil {
			logger.Info("Report saved", "file", tierReportFile)
		}
	}

	return 0
}

// TierComparisonReport holds the comparison data across tiers
type TierComparisonReport struct {
	GeneratedAt time.Time              `json:"generated_at"`
	OutputDir   string                 `json:"output_dir"`
	Tiers       map[string]TierMetrics `json:"tiers"`
}

// TierMetrics holds metrics for a single tier
type TierMetrics struct {
	Tier             int     `json:"tier"`
	Name             string  `json:"name"`
	DurationMs       int64   `json:"duration_ms"`
	EntitiesStored   int     `json:"entities_stored"`
	EmbeddingsGen    int     `json:"embeddings_generated"`
	CommunitiesFound int     `json:"communities_found"`
	RulesEvaluated   int     `json:"rules_evaluated"`
	RulesTriggered   int     `json:"rules_triggered"`
	SearchQuality    float64 `json:"search_quality"`
}

// TierExpectation defines expected behavior for a tier
type TierExpectation struct {
	Name               string
	ExpectedEmbeddings int // -1 means any non-zero
	ExpectedClusters   int // -1 means any non-zero
	ExpectedInference  bool
}

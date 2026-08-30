// Package throughput provides a high-throughput E2E scenario for performance profiling.
package throughput

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/config"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

// Scenario sends high volumes of messages for performance profiling.
type Scenario struct {
	name        string
	description string
	udpAddr     string
	metrics     *client.MetricsClient
	profile     *client.ProfileClient
	nats        *client.NATSValidationClient
	config      *Config
}

// throughputVariant is the tier compose profile `task e2e:throughput` brings
// up. Setup reads the deployment authority for THIS variant, so the name must
// equal the profile the taskfile actually starts —
// TestTierEntityMatchesTheProfileTheTaskBringsUp pins that.
const throughputVariant = config.VariantStatistical

// Config holds configuration for the throughput scenario.
type Config struct {
	// Message configuration
	MessageCount int `json:"message_count"` // Total messages to send (default: 10000)
	MessageRate  int `json:"message_rate"`  // Messages per second, 0 = unlimited
	MessageSize  int `json:"message_size"`  // Approximate bytes per message (default: 256)

	// Profile configuration
	ProfileDuration int    `json:"profile_duration"` // CPU profile seconds (default: 30)
	ProfileDir      string `json:"profile_dir"`      // Directory for profile output

	// Query load configuration
	QueryConcurrency int           `json:"query_concurrency"` // Parallel query goroutines (default: 10)
	QueryDuration    time.Duration `json:"query_duration"`    // Sustain queries for (default: 15s)
	GraphQLURL       string        `json:"graphql_url"`       // Gateway GraphQL endpoint

	// Extended profiling
	ProfileAll bool `json:"profile_all"` // Capture block + mutex profiles too

	// Validation
	ValidationTimeout time.Duration `json:"validation_timeout"`  // Max wait for processing
	MinProcessedRatio float64       `json:"min_processed_ratio"` // Min ratio of messages processed (default: 0.9)

	// Synthetic entity generation (0 = use testdata cycling)
	UniqueEntities int `json:"unique_entities"`

	// Concurrent query-during-ingestion mode
	QueryDuringIngestion bool `json:"query_during_ingestion"`

	// Query SLA thresholds (0 = disabled, no assertion)
	MaxQueryP99Ms     float64 `json:"max_query_p99_ms"`
	MaxQueryErrorRate float64 `json:"max_query_error_rate"`

	// NATSURL is where the running stack records the authority it mints under.
	NATSURL string `json:"nats_url"`

	// Authority is `org.platform` as the running stack RECORDS it, read from
	// semstreams_config/platform_identity during Setup. It is not configuration:
	// nothing may set it, because predicting it from a config file has been
	// wrong by the minted entropy suffix since ADR-104.
	Authority string `json:"-"`
}

// DefaultConfig returns sensible defaults for throughput testing.
func DefaultConfig() *Config {
	return &Config{
		MessageCount:      10000,
		MessageRate:       0, // Unlimited
		MessageSize:       256,
		ProfileDuration:   30,
		ProfileDir:        "test/e2e/results/profiles",
		QueryConcurrency:  10,
		QueryDuration:     15 * time.Second,
		GraphQLURL:        config.DefaultEndpoints.HTTP + "/graph-gateway/graphql",
		ValidationTimeout: 60 * time.Second,
		MinProcessedRatio: 0.9,
	}
}

// NewScenario creates a new throughput scenario.
func NewScenario(metricsURL, udpAddr string, cfg *Config) *Scenario {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if udpAddr == "" {
		udpAddr = config.DefaultEndpoints.UDP
	}
	if metricsURL == "" {
		metricsURL = config.DefaultEndpoints.Metrics
	}
	if cfg.NATSURL == "" {
		cfg.NATSURL = config.DefaultEndpoints.NATS
	}

	return &Scenario{
		name:        "throughput",
		description: "High-throughput stress test with integrated profiling",
		udpAddr:     udpAddr,
		metrics:     client.NewMetricsClient(metricsURL),
		profile:     client.NewProfileClient(config.DefaultEndpoints.PProf, cfg.ProfileDir),
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *Scenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *Scenario) Description() string {
	return s.description
}

// Setup prepares the scenario.
func (s *Scenario) Setup(ctx context.Context) error {
	// Verify UDP endpoint is reachable
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		return fmt.Errorf("cannot reach UDP endpoint %s: %w", s.udpAddr, err)
	}
	_ = conn.Close()

	// Verify metrics endpoint
	if err := s.metrics.Health(ctx); err != nil {
		return fmt.Errorf("metrics endpoint not healthy: %w", err)
	}

	// Read the authority the deployment mints under. Every fixture entity ID
	// this scenario queries is composed from it, and since ADR-104 it is the
	// shipped config's platform.id plus a minted entropy suffix — unknowable
	// without asking the stack.
	natsClient, err := client.NewNATSValidationClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("NATS validation client is required to read the deployment authority: %w", err)
	}
	s.nats = natsClient
	authority, err := config.EffectiveTierAuthority(ctx, natsClient, throughputVariant)
	if err != nil {
		return err
	}
	s.config.Authority = authority

	return nil
}

// Execute runs the throughput test.
func (s *Scenario) Execute(ctx context.Context) (*scenarios.Result, error) {
	result := &scenarios.Result{
		ScenarioName: s.name,
		StartTime:    time.Now(),
		Success:      false,
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
		Errors:       []string{},
		Warnings:     []string{},
	}

	// Setup profiling if available
	profilingEnabled := s.profile.IsAvailable(ctx)
	result.Details["profiling_enabled"] = profilingEnabled
	if profilingEnabled {
		cleanup := s.startProfiling(ctx, result)
		defer cleanup()
	}

	// Capture baseline metrics
	baseline, err := s.metrics.CaptureBaseline(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to capture metrics baseline: %v", err))
	}

	sendStart := time.Now()
	var queryResult *QueryLoadResult

	if s.config.QueryDuringIngestion && s.config.GraphQLURL != "" {
		queryResult, err = s.executeConcurrent(ctx, profilingEnabled, baseline, result)
	} else {
		queryResult, err = s.executeSequential(ctx, profilingEnabled, baseline, result)
	}
	if err != nil {
		return result, nil // error already recorded in result
	}

	// Capture final metrics delta
	s.captureMetricsDelta(ctx, baseline, result)

	// Capture final profiles (if we didn't already capture during query phase)
	if profilingEnabled {
		s.captureFinalProfiles(ctx, result)
	}

	// Calculate end-to-end throughput
	totalDuration := time.Since(sendStart)
	if sent, ok := result.Metrics["messages_sent"].(int); ok && totalDuration.Seconds() > 0 {
		result.Metrics["total_throughput_msg_sec"] = float64(sent) / totalDuration.Seconds()
	}

	// Query SLA validation
	if queryResult != nil {
		if s.config.MaxQueryP99Ms > 0 && queryResult.P99LatencyMs > s.config.MaxQueryP99Ms {
			result.Success = false
			result.Errors = append(result.Errors, fmt.Sprintf(
				"query P99 latency %.0fms exceeds SLA threshold %.0fms",
				queryResult.P99LatencyMs, s.config.MaxQueryP99Ms))
		}
		if s.config.MaxQueryErrorRate > 0 && queryResult.TotalQueries > 0 {
			errorRate := float64(queryResult.ErrorCount) / float64(queryResult.TotalQueries)
			if errorRate > s.config.MaxQueryErrorRate {
				result.Success = false
				result.Errors = append(result.Errors, fmt.Sprintf(
					"query error rate %.1f%% exceeds SLA threshold %.1f%%",
					errorRate*100, s.config.MaxQueryErrorRate*100))
			}
		}
	}

	// Only mark success if SLA checks didn't already flag a failure
	if !result.Success && len(result.Errors) > 0 {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, nil
	}

	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// executeConcurrent runs message ingestion and queries in parallel.
func (s *Scenario) executeConcurrent(ctx context.Context, profilingEnabled bool, _ *client.MetricsBaseline, result *scenarios.Result) (*QueryLoadResult, error) {
	var sendResult *SendResult
	var sendErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		sendResult, sendErr = s.sendMessages(ctx, result)
	}()

	// Brief warmup before starting queries
	time.Sleep(2 * time.Second)

	queryResult := s.runQueryPhase(ctx, profilingEnabled, result)

	wg.Wait()

	if sendErr != nil {
		result.Error = fmt.Sprintf("send failed: %v", sendErr)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return queryResult, sendErr
	}

	result.Metrics["messages_sent"] = sendResult.MessagesSent
	result.Metrics["send_duration_ms"] = sendResult.Duration.Milliseconds()
	result.Metrics["send_rate_msg_sec"] = sendResult.Rate
	result.Details["send_summary"] = fmt.Sprintf(
		"Sent %d messages in %v (%.0f msg/sec)",
		sendResult.MessagesSent, sendResult.Duration, sendResult.Rate)
	result.Details["mode"] = "concurrent"

	return queryResult, nil
}

// executeSequential runs the original send → wait → query flow.
func (s *Scenario) executeSequential(ctx context.Context, profilingEnabled bool, baseline *client.MetricsBaseline, result *scenarios.Result) (*QueryLoadResult, error) {
	sendResult, err := s.sendMessages(ctx, result)
	if err != nil {
		result.Error = fmt.Sprintf("send failed: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return nil, err
	}

	result.Metrics["messages_sent"] = sendResult.MessagesSent
	result.Metrics["send_duration_ms"] = sendResult.Duration.Milliseconds()
	result.Metrics["send_rate_msg_sec"] = sendResult.Rate
	result.Details["send_summary"] = fmt.Sprintf(
		"Sent %d messages in %v (%.0f msg/sec)",
		sendResult.MessagesSent, sendResult.Duration, sendResult.Rate)
	result.Details["mode"] = "sequential"

	processingStart := time.Now()
	if err := s.waitForProcessing(ctx, baseline, sendResult.MessagesSent); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Processing validation: %v", err))
	}
	result.Metrics["processing_wait_ms"] = time.Since(processingStart).Milliseconds()

	var queryResult *QueryLoadResult
	if s.config.GraphQLURL != "" {
		queryResult = s.runQueryPhase(ctx, profilingEnabled, result)
	}

	return queryResult, nil
}

// runQueryPhase executes the query load phase with optional profiling.
// Returns nil when the phase is skipped or fails before collecting results.
func (s *Scenario) runQueryPhase(ctx context.Context, profilingEnabled bool, result *scenarios.Result) *QueryLoadResult {
	// Wait for entities to be queryable before hammering the gateway
	fmt.Print("\n[QUERY-LOAD] Waiting for entities to be queryable...")
	if err := s.waitForEntities(ctx); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Entity readiness check failed: %v", err))
		fmt.Printf(" skipping query load (%v)\n", err)
		return nil
	}
	fmt.Println(" ready")

	fmt.Printf("[QUERY-LOAD] Starting %d concurrent query workers for %v...\n",
		s.config.QueryConcurrency, s.config.QueryDuration)

	// Capture pre-query goroutine count
	var preQueryGoroutines string
	if profilingEnabled {
		if path, err := s.profile.CaptureGoroutine(ctx, "throughput-pre-query"); err == nil {
			preQueryGoroutines = path
			result.Details["profile_pre_query_goroutine"] = path
		}
	}

	// Start CPU profile for query phase (async)
	var cpuWg sync.WaitGroup
	if profilingEnabled {
		cpuWg.Add(1)
		go func() {
			defer cpuWg.Done()
			duration := int(s.config.QueryDuration.Seconds()) + 2 // Slightly longer than query phase
			if path, err := s.profile.CaptureCPU(ctx, "throughput-query", duration); err == nil {
				result.Details["profile_query_cpu"] = path
			}
		}()
	}

	// Run query load
	queryResult, err := runQueryLoad(ctx, s.config)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Query load failed: %v", err))
		return nil
	}

	// Record query metrics
	result.Metrics["query_total"] = queryResult.TotalQueries
	result.Metrics["query_per_sec"] = queryResult.QueriesPerSec
	result.Metrics["query_errors"] = queryResult.ErrorCount
	result.Metrics["query_p50_ms"] = queryResult.P50LatencyMs
	result.Metrics["query_p95_ms"] = queryResult.P95LatencyMs
	result.Metrics["query_p99_ms"] = queryResult.P99LatencyMs
	result.Details["query_load"] = queryResult

	printQueryLoadSummary(queryResult)

	// Wait for query CPU profile to finish
	cpuWg.Wait()

	// Capture post-query profiles
	if profilingEnabled {
		if path, err := s.profile.CaptureGoroutine(ctx, "throughput-post-query"); err == nil {
			result.Details["profile_post_query_goroutine"] = path
		}
		if path, err := s.profile.CaptureHeap(ctx, "throughput-post-query"); err == nil {
			result.Details["profile_post_query_heap"] = path
		}
		if path, err := s.profile.CaptureAllocs(ctx, "throughput-post-query"); err == nil {
			result.Details["profile_post_query_allocs"] = path
		}
	}

	// Analyze profiles and print summary
	if profilingEnabled {
		analysis, err := analyzeProfiles(s.config.ProfileDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Profile analysis failed: %v", err))
		} else {
			result.Details["profile_analysis"] = analysis
			printProfileAnalysis(analysis)
		}

		// Check goroutine delta
		if preQueryGoroutines != "" {
			result.Details["goroutine_check"] = "pre and post query goroutine profiles captured for comparison"
		}
	}

	return queryResult
}

// startProfiling captures baseline heap and starts CPU profiling.
// Returns a cleanup function that waits for CPU profile completion.
func (s *Scenario) startProfiling(ctx context.Context, result *scenarios.Result) func() {
	// Capture baseline heap before load
	if path, err := s.profile.CaptureHeap(ctx, "throughput-baseline"); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to capture baseline heap: %v", err))
	} else {
		result.Details["profile_baseline_heap"] = path
	}

	// Start CPU profile async (runs during the load phase)
	var cpuProfilePath string
	var cpuProfileErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cpuProfilePath, cpuProfileErr = s.profile.CaptureCPU(ctx, "throughput", s.config.ProfileDuration)
	}()

	return func() {
		wg.Wait()
		if cpuProfileErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("CPU profile failed: %v", cpuProfileErr))
		} else if cpuProfilePath != "" {
			result.Details["profile_cpu"] = cpuProfilePath
		}
	}
}

// captureMetricsDelta captures and records the metrics delta from baseline.
func (s *Scenario) captureMetricsDelta(ctx context.Context, baseline *client.MetricsBaseline, result *scenarios.Result) {
	if baseline == nil {
		return
	}
	diff, err := s.metrics.CompareToBaseline(ctx, baseline)
	if err != nil {
		return
	}
	result.Metrics["total_duration_sec"] = diff.Duration.Seconds()
	if rate, ok := diff.RatePerSec["semstreams_udp_datagrams_received_total"]; ok {
		result.Metrics["udp_receive_rate"] = rate
	}
	if rate, ok := diff.RatePerSec["semstreams_file_lines_written_total"]; ok {
		result.Metrics["file_write_rate"] = rate
	}
}

// captureFinalProfiles captures heap and goroutine profiles after the test.
func (s *Scenario) captureFinalProfiles(ctx context.Context, result *scenarios.Result) {
	if path, err := s.profile.CaptureHeap(ctx, "throughput-final"); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to capture final heap: %v", err))
	} else {
		result.Details["profile_final_heap"] = path
	}

	if path, err := s.profile.CaptureGoroutine(ctx, "throughput-final"); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to capture goroutine profile: %v", err))
	} else {
		result.Details["profile_goroutine"] = path
	}

	// Extended profiling: block + mutex
	if s.config.ProfileAll {
		if profiles, err := s.profile.CaptureBlockAndMutex(ctx, "throughput-final"); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to capture block/mutex profiles: %v", err))
		} else {
			for k, v := range profiles {
				result.Details["profile_final_"+k] = v
			}
		}
	}
}

// Teardown cleans up after the scenario.
func (s *Scenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		err := s.nats.Close(ctx)
		s.nats = nil
		if err != nil {
			return fmt.Errorf("close NATS validation client: %w", err)
		}
	}
	return nil
}

// SendResult holds the results of the send phase.
type SendResult struct {
	MessagesSent int
	Duration     time.Duration
	Rate         float64 // messages per second
}

// sendMessages sends messages at the configured rate.
func (s *Scenario) sendMessages(ctx context.Context, result *scenarios.Result) (*SendResult, error) {
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		return nil, fmt.Errorf("UDP connection failed: %w", err)
	}
	defer conn.Close()

	// Pre-generate messages
	var messages [][]byte
	if s.config.UniqueEntities > 0 {
		messages = GenerateSyntheticMessages(s.config.UniqueEntities)
		result.Details["synthetic_entities"] = s.config.UniqueEntities
	} else {
		messages = s.generateMessages(s.config.MessageCount, s.config.MessageSize)
	}
	result.Details["messages_pregenerated"] = len(messages)

	// Rate limiting setup
	var ticker *time.Ticker
	if s.config.MessageRate > 0 {
		interval := time.Second / time.Duration(s.config.MessageRate)
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
		result.Details["rate_limit_msg_sec"] = s.config.MessageRate
	} else {
		result.Details["rate_limit_msg_sec"] = "unlimited"
	}

	start := time.Now()
	sent := 0
	errors := 0

	for i, msg := range messages {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if ticker != nil {
			<-ticker.C
		}

		if _, err := conn.Write(msg); err != nil {
			errors++
			if errors < 10 {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Send error at msg %d: %v", i, err))
			}
			continue
		}
		sent++

		// Progress logging every 1000 messages
		if sent%1000 == 0 && sent > 0 {
			elapsed := time.Since(start)
			rate := float64(sent) / elapsed.Seconds()
			fmt.Printf("  Sent %d/%d messages (%.0f msg/sec)\n", sent, s.config.MessageCount, rate)
		}
	}

	elapsed := time.Since(start)
	result.Metrics["send_errors"] = errors

	return &SendResult{
		MessagesSent: sent,
		Duration:     elapsed,
		Rate:         float64(sent) / elapsed.Seconds(),
	}, nil
}

// generateMessages pre-generates all test messages.
func (s *Scenario) generateMessages(count, targetSize int) [][]byte {
	messages := make([][]byte, count)

	// Calculate padding to reach target size
	baseMsg := map[string]any{
		"type":      "throughput-test",
		"sequence":  0,
		"timestamp": time.Now().UnixNano(),
		"data": map[string]any{
			"value":   0,
			"padding": "",
		},
	}

	baseBytes, _ := json.Marshal(baseMsg)
	paddingNeeded := targetSize - len(baseBytes)
	if paddingNeeded < 0 {
		paddingNeeded = 0
	}

	padding := make([]byte, paddingNeeded)
	for i := range padding {
		padding[i] = 'x'
	}
	paddingStr := string(padding)

	for i := 0; i < count; i++ {
		msg := map[string]any{
			"type":      "throughput-test",
			"sequence":  i,
			"timestamp": time.Now().UnixNano(),
			"value":     i * 10, // Varying values for filter testing
			"data": map[string]any{
				"index":   i,
				"padding": paddingStr,
			},
		}
		msgBytes, _ := json.Marshal(msg)
		messages[i] = msgBytes
	}

	return messages
}

// waitForProcessing waits for messages to be processed.
func (s *Scenario) waitForProcessing(ctx context.Context, baseline *client.MetricsBaseline, expectedCount int) error {
	if baseline == nil {
		// No baseline, just wait a fixed time
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}

	// Wait for file lines written to reach expected count
	deadline := time.Now().Add(s.config.ValidationTimeout)
	minExpected := float64(expectedCount) * s.config.MinProcessedRatio

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}

		diff, err := s.metrics.CompareToBaseline(ctx, baseline)
		if err != nil {
			continue
		}

		// Check file output lines
		if delta, ok := diff.Deltas["semstreams_file_lines_written_total"]; ok {
			if delta >= minExpected {
				return nil
			}
		}
	}

	return fmt.Errorf("timeout waiting for processing (expected %.0f messages)", minExpected)
}

// waitForEntities polls the GraphQL gateway until ALL known entity IDs are
// queryable. This ensures file inputs have been fully ingested before we start
// the query load phase. Previously this only checked for 1 entity via prefix
// query, which caused "not found" errors when the query load queried entities
// that hadn't been ingested yet.
func (s *Scenario) waitForEntities(ctx context.Context) error {
	entities := knownEntityIDs(s.config.Authority)
	if s.config.UniqueEntities > 0 {
		// Synthetic entities have platform-dependent IDs, so probe via
		// prefix query instead of exact entity lookups.
		return s.waitForSyntheticEntities(ctx)
	}
	httpClient := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(s.config.ValidationTimeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}

		missing := s.probeEntities(ctx, httpClient, entities)
		if len(missing) == 0 {
			return nil
		}

		fmt.Printf(".")
	}

	// Final check to report which entities are still missing
	missing := s.probeEntities(ctx, httpClient, entities)
	return fmt.Errorf("timeout: %d/%d entities not queryable at %s: %v",
		len(missing), len(entities), s.config.GraphQLURL, missing)
}

// waitForSyntheticEntities waits until synthetic entities are queryable via predicate search.
// Uses entitiesByPredicate instead of exact entity ID lookups since synthetic entity IDs
// contain the platform name which varies across configs (statistical vs semantic).
func (s *Scenario) waitForSyntheticEntities(ctx context.Context) error {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(s.config.ValidationTimeout)

	query := `{"query":"{ entitiesByPredicate(predicate: \"sensor.classification.type\", limit: 1) }"}`

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.GraphQLURL,
			bytes.NewReader([]byte(query)))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			fmt.Print(".")
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Check if we got at least one entity back
		if resp.StatusCode == http.StatusOK && !bytes.Contains(body, []byte(`"errors"`)) &&
			bytes.Contains(body, []byte(`"entitiesByPredicate"`)) {
			// Verify the result array is non-empty
			if !bytes.Contains(body, []byte(`[]`)) {
				return nil
			}
		}
		fmt.Print(".")
	}

	return fmt.Errorf("timeout: synthetic entities not queryable at %s via predicate search", s.config.GraphQLURL)
}

// probeEntities checks which entity IDs are queryable and returns the missing ones.
func (s *Scenario) probeEntities(ctx context.Context, httpClient *http.Client, entityIDs []string) []string {
	if s == nil || s.config == nil || s.config.GraphQLURL == "" {
		return append([]string(nil), entityIDs...)
	}

	var missing []string
	for _, eid := range entityIDs {
		query := fmt.Sprintf(`{"query":"{ entity(id: \"%s\") { entity { id } kvRevision } }"}`, eid)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.GraphQLURL,
			bytes.NewReader([]byte(query)))
		if err != nil {
			missing = append(missing, eid)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			missing = append(missing, eid)
			continue
		}
		var response struct {
			Data struct {
				Entity *struct {
					Entity *struct {
						ID string `json:"id"`
					} `json:"entity"`
					KVRevision uint64 `json:"kvRevision"`
				} `json:"entity"`
			} `json:"data"`
			Errors []json.RawMessage `json:"errors"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&response)
		closeErr := resp.Body.Close()
		exact := response.Data.Entity
		if resp.StatusCode != http.StatusOK || decodeErr != nil || closeErr != nil || len(response.Errors) != 0 ||
			exact == nil || exact.Entity == nil || exact.Entity.ID != eid || exact.KVRevision == 0 {
			missing = append(missing, eid)
		}
	}
	return missing
}

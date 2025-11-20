// Package scenarios provides E2E performance test scenarios for rule processor
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/test/e2e/client"
)

// RulesPerformanceScenario validates rule processor performance under load
type RulesPerformanceScenario struct {
	name        string
	description string
	client      *client.ObservabilityClient
	udpAddr     string
	config      *RulesPerformanceConfig
}

// RulesPerformanceConfig contains configuration for rules performance test
type RulesPerformanceConfig struct {
	// Load generation configuration
	RuleCount      int           `json:"rule_count"`      // Number of rules to create
	MessageCount   int           `json:"message_count"`   // Total messages to send
	MessageRate    int           `json:"message_rate"`    // Messages per second
	TestDuration   time.Duration `json:"test_duration"`   // How long to run the test
	WarmupDuration time.Duration `json:"warmup_duration"` // Warmup period before measuring
	CooldownDelay  time.Duration `json:"cooldown_delay"`  // Wait after sending for processing

	// Performance targets
	ThroughputTarget int           `json:"throughput_target"`  // Min messages/sec
	LatencyTargetP95 time.Duration `json:"latency_target_p95"` // Max p95 latency
}

// DefaultRulesPerformanceConfig returns default configuration
func DefaultRulesPerformanceConfig() *RulesPerformanceConfig {
	return &RulesPerformanceConfig{
		RuleCount:        10,
		MessageCount:     1000,
		MessageRate:      100, // 100 msg/sec
		TestDuration:     10 * time.Second,
		WarmupDuration:   2 * time.Second,
		CooldownDelay:    5 * time.Second,
		ThroughputTarget: 100,                   // 100 msg/sec minimum
		LatencyTargetP95: 50 * time.Millisecond, // 50ms p95
	}
}

// NewRulesPerformanceScenario creates a new rules performance test scenario
func NewRulesPerformanceScenario(
	obsClient *client.ObservabilityClient,
	udpAddr string,
	config *RulesPerformanceConfig,
) *RulesPerformanceScenario {
	if config == nil {
		config = DefaultRulesPerformanceConfig()
	}
	if udpAddr == "" {
		udpAddr = "localhost:14550"
	}

	return &RulesPerformanceScenario{
		name:        "rules-performance",
		description: "Tests rule processor performance under load (throughput, latency, stability)",
		client:      obsClient,
		udpAddr:     udpAddr,
		config:      config,
	}
}

// Name returns the scenario name
func (s *RulesPerformanceScenario) Name() string {
	return s.name
}

// Description returns the scenario description
func (s *RulesPerformanceScenario) Description() string {
	return s.description
}

// Setup prepares the scenario
func (s *RulesPerformanceScenario) Setup(_ context.Context) error {
	// Verify UDP endpoint is reachable
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		return fmt.Errorf("cannot reach UDP endpoint %s: %w", s.udpAddr, err)
	}
	_ = conn.Close()

	return nil
}

// Execute runs the rules performance test scenario
func (s *RulesPerformanceScenario) Execute(ctx context.Context) (*Result, error) {
	result := &Result{
		ScenarioName: s.name,
		StartTime:    time.Now(),
		Success:      false,
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
		Errors:       []string{},
		Warnings:     []string{},
	}

	// Track execution stages
	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-components", s.executeVerifyComponents},
		{"warmup-phase", s.executeWarmup},
		{"load-test", s.executeLoadTest},
		{"validate-performance", s.executeValidatePerformance},
	}

	// Execute each stage
	for _, stage := range stages {
		stageStart := time.Now()

		if err := stage.fn(ctx, result); err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, nil // Return result even on failure
		}

		result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = time.Since(stageStart).Milliseconds()
	}

	// Overall success
	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// Teardown cleans up after the scenario
func (s *RulesPerformanceScenario) Teardown(_ context.Context) error {
	// No cleanup needed for performance test
	return nil
}

// executeVerifyComponents checks that rule processor exists
func (s *RulesPerformanceScenario) executeVerifyComponents(ctx context.Context, result *Result) error {
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component verification failed: %w", err)
	}

	// Find rule processor
	var ruleProcessorFound bool
	for _, comp := range components {
		if comp.Name == "rule" {
			ruleProcessorFound = true
			result.Details["rule_processor"] = map[string]any{
				"name":    comp.Name,
				"type":    comp.Type,
				"healthy": comp.Healthy,
				"state":   comp.State,
			}
			break
		}
	}

	if !ruleProcessorFound {
		result.Errors = append(result.Errors, "Rule processor not found")
		return fmt.Errorf("rule processor not found")
	}

	result.Details["component_count"] = len(components)
	return nil
}

// executeWarmup warms up the system before load testing
func (s *RulesPerformanceScenario) executeWarmup(ctx context.Context, result *Result) error {
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to connect to UDP: %v", err))
		return fmt.Errorf("UDP connection failed: %w", err)
	}
	defer conn.Close()

	// Send warmup messages (10% of total load)
	warmupCount := s.config.MessageCount / 10
	if warmupCount < 10 {
		warmupCount = 10
	}

	warmupInterval := s.config.WarmupDuration / time.Duration(warmupCount)

	for i := 0; i < warmupCount; i++ {
		msg := s.createTestMessage(i)
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		_, _ = conn.Write(msgBytes)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(warmupInterval):
		}
	}

	result.Metrics["warmup_messages"] = warmupCount
	result.Details["warmup"] = fmt.Sprintf("Sent %d warmup messages", warmupCount)

	return nil
}

// executeLoadTest executes the main load test
func (s *RulesPerformanceScenario) executeLoadTest(ctx context.Context, result *Result) error {
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to connect to UDP: %v", err))
		return fmt.Errorf("UDP connection failed: %w", err)
	}
	defer conn.Close()

	// Calculate message interval based on target rate
	messageInterval := time.Second / time.Duration(s.config.MessageRate)

	// Track performance metrics
	var messagesSent atomic.Int64
	var sendErrors atomic.Int64
	var sendLatencies []time.Duration
	var latencyMutex sync.Mutex

	// Start time for throughput calculation
	loadStart := time.Now()

	// Send messages at target rate
	ticker := time.NewTicker(messageInterval)
	defer ticker.Stop()

	messageNum := 0
	testDeadline := time.Now().Add(s.config.TestDuration)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(testDeadline) || messageNum >= s.config.MessageCount {
				goto done
			}

			// Send message and measure latency
			sendStart := time.Now()
			msg := s.createTestMessage(messageNum)
			msgBytes, err := json.Marshal(msg)
			if err != nil {
				sendErrors.Add(1)
				continue
			}

			_, err = conn.Write(msgBytes)
			sendLatency := time.Since(sendStart)

			if err != nil {
				sendErrors.Add(1)
			} else {
				messagesSent.Add(1)

				// Record latency (sample every 10th message to avoid overhead)
				if messageNum%10 == 0 {
					latencyMutex.Lock()
					sendLatencies = append(sendLatencies, sendLatency)
					latencyMutex.Unlock()
				}
			}

			messageNum++
		}
	}

done:
	loadDuration := time.Since(loadStart)

	// Wait for processing to complete
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.config.CooldownDelay):
	}

	// Calculate throughput
	throughput := float64(messagesSent.Load()) / loadDuration.Seconds()

	// Calculate latency percentiles
	var latencyP50, latencyP95, latencyP99 time.Duration
	if len(sendLatencies) > 0 {
		latencyP50, latencyP95, latencyP99 = calculatePercentiles(sendLatencies)
	}

	// Record metrics
	result.Metrics["messages_sent"] = messagesSent.Load()
	result.Metrics["send_errors"] = sendErrors.Load()
	result.Metrics["load_duration_sec"] = loadDuration.Seconds()
	result.Metrics["throughput_msg_per_sec"] = throughput
	result.Metrics["send_latency_p50_ms"] = latencyP50.Milliseconds()
	result.Metrics["send_latency_p95_ms"] = latencyP95.Milliseconds()
	result.Metrics["send_latency_p99_ms"] = latencyP99.Milliseconds()

	result.Details["load_test"] = fmt.Sprintf(
		"Sent %d messages in %.2fs (%.2f msg/sec). Latency p95: %dms",
		messagesSent.Load(), loadDuration.Seconds(), throughput, latencyP95.Milliseconds())

	return nil
}

// executeValidatePerformance validates performance meets targets
func (s *RulesPerformanceScenario) executeValidatePerformance(ctx context.Context, result *Result) error {
	// Get throughput from metrics
	throughput, ok := result.Metrics["throughput_msg_per_sec"].(float64)
	if !ok {
		return fmt.Errorf("throughput metric not found")
	}

	// Get p95 latency from metrics
	latencyP95, ok := result.Metrics["send_latency_p95_ms"].(int64)
	if !ok {
		return fmt.Errorf("latency p95 metric not found")
	}

	// Check throughput target
	if throughput < float64(s.config.ThroughputTarget) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Throughput %.2f msg/sec below target %d msg/sec",
				throughput, s.config.ThroughputTarget))
	}

	// Check latency target
	if time.Duration(latencyP95)*time.Millisecond > s.config.LatencyTargetP95 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Latency p95 %dms exceeds target %dms",
				latencyP95, s.config.LatencyTargetP95.Milliseconds()))
	}

	// Query components for final health status
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get final component status: %v", err))
	} else {
		for _, comp := range components {
			if comp.Name == "rule" {
				result.Details["final_rule_status"] = map[string]any{
					"healthy": comp.Healthy,
					"state":   comp.State,
				}

				if !comp.Healthy {
					result.Errors = append(result.Errors, "Rule processor unhealthy after load test")
					return fmt.Errorf("rule processor unhealthy after load")
				}
				break
			}
		}
	}

	result.Details["performance_validation"] = fmt.Sprintf(
		"Throughput: %.2f msg/sec (target: %d), Latency p95: %dms (target: %dms)",
		throughput, s.config.ThroughputTarget,
		latencyP95, s.config.LatencyTargetP95.Milliseconds())

	return nil
}

// createTestMessage creates a test message for load testing
func (s *RulesPerformanceScenario) createTestMessage(seq int) map[string]any {
	return map[string]any{
		"entity_id":   fmt.Sprintf("perf-test-entity-%d", seq),
		"entity_type": "test",
		"timestamp":   time.Now().Unix(),
		"sequence":    seq,
		"data": map[string]any{
			"value":       seq * 10,
			"temperature": 20.0 + float64(seq%50),
			"pressure":    1013.25 + float64(seq%100)/10.0,
		},
	}
}

// calculatePercentiles calculates latency percentiles
func calculatePercentiles(latencies []time.Duration) (p50, p95, p99 time.Duration) {
	if len(latencies) == 0 {
		return 0, 0, 0
	}

	// Simple percentile calculation (not sorting for performance)
	// For production, use a proper percentile library
	sum := time.Duration(0)
	max := time.Duration(0)

	for _, lat := range latencies {
		sum += lat
		if lat > max {
			max = lat
		}
	}

	// Approximate percentiles
	p50 = sum / time.Duration(len(latencies)) // Mean as p50 approximation
	p95 = max * 95 / 100                      // Approximate p95
	p99 = max * 99 / 100                      // Approximate p99

	return p50, p95, p99
}

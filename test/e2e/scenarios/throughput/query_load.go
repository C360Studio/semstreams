package throughput

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// QueryLoadResult holds aggregate results from the query load phase.
type QueryLoadResult struct {
	TotalQueries    int                       `json:"total_queries"`
	QueriesPerSec   float64                   `json:"queries_per_sec"`
	Duration        time.Duration             `json:"duration"`
	ByType          map[string]QueryTypeStats `json:"by_type"`
	ErrorCount      int                       `json:"error_count"`
	NotFoundCount   int                       `json:"not_found_count"`
	NotFoundDetails []string                  `json:"not_found_details,omitempty"` // unique not-found messages
	ErrorDetails    []string                  `json:"error_details,omitempty"`     // unique infra error messages
	P50LatencyMs    float64                   `json:"p50_latency_ms"`
	P95LatencyMs    float64                   `json:"p95_latency_ms"`
	P99LatencyMs    float64                   `json:"p99_latency_ms"`
}

// QueryTypeStats holds per-query-type statistics.
type QueryTypeStats struct {
	Count        int     `json:"count"`
	Errors       int     `json:"errors"`
	NotFound     int     `json:"not_found"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	TotalMs      float64 `json:"-"` // accumulator
}

// querySpec defines a single GraphQL query to fire.
type querySpec struct {
	Name  string
	Query string
}

// knownEntityIDs returns entity IDs known to exist in the statistical testdata.
// Entity IDs are built by iot_sensor as: {org}.{platform}.environmental.sensor.{type}.{device_id}
// where {type} comes from the "type" field in sensors.jsonl (e.g., "combustible_gas", not "gas").
func knownEntityIDs() []string {
	return []string{
		"c360.logistics.environmental.sensor.temperature.temp-sensor-001",
		"c360.logistics.environmental.sensor.temperature.temp-sensor-002",
		"c360.logistics.environmental.sensor.humidity.humid-sensor-001",
		"c360.logistics.environmental.sensor.pressure.pressure-sensor-001",
		"c360.logistics.environmental.sensor.power.power-sensor-001",
		"c360.logistics.environmental.sensor.vibration.vibration-sensor-001",
		"c360.logistics.environmental.sensor.flow.flow-sensor-001",
		"c360.logistics.environmental.sensor.combustible_gas.gas-sensor-001",
		"c360.logistics.environmental.sensor.illumination.light-sensor-001",
		"c360.logistics.environmental.sensor.level.level-sensor-001",
	}
}

// buildQueryPool returns a weighted list of query specs to randomly sample from.
// Weights: entity 30%, prefix 20%, spatial 20%, predicate 20%, temporal 10%.
func buildQueryPool() []querySpec {
	pool := make([]querySpec, 0, 100)

	entities := knownEntityIDs()

	// Entity queries (30 entries = 30%)
	for i := range 30 {
		eid := entities[i%len(entities)]
		pool = append(pool, querySpec{
			Name: "entity",
			Query: fmt.Sprintf(
				`{"query":"{ entity(id: \"%s\") { entity { id triples { subject predicate object } } kvRevision } }"}`,
				eid),
		})
	}

	// Prefix queries (20 entries = 20%)
	prefixes := []string{
		"c360.logistics",
		"c360.logistics.environmental",
		"c360.logistics.environmental.sensor",
		"c360.logistics.environmental.sensor.temperature",
		"c360.logistics.warehouse",
	}
	for i := range 20 {
		pfx := prefixes[i%len(prefixes)]
		pool = append(pool, querySpec{
			Name: "prefix",
			Query: fmt.Sprintf(
				`{"query":"{ entitiesByPrefix(prefix: \"%s\", limit: 50) { entities { id triples { subject predicate object } } next_cursor } }"}`,
				pfx),
		})
	}

	// Spatial queries (20 entries = 20%) — bounding boxes around San Francisco testdata
	type bbox struct{ north, south, east, west float64 }
	boxes := []bbox{
		{37.78, 37.77, -122.41, -122.43},
		{37.79, 37.76, -122.40, -122.44},
		{37.7760, 37.7740, -122.4180, -122.4200},
		{37.80, 37.75, -122.39, -122.45},
		{37.7755, 37.7745, -122.4190, -122.4200},
	}
	for i := range 20 {
		b := boxes[i%len(boxes)]
		pool = append(pool, querySpec{
			Name: "spatial",
			Query: fmt.Sprintf(
				`{"query":"{ spatialSearch(north: %f, south: %f, east: %f, west: %f, limit: 50) { id triples { subject predicate object } } }"}`,
				b.north, b.south, b.east, b.west),
		})
	}

	// Predicate filter queries (20 entries = 20%) — simulates semdragon's
	// game board entity filtering: find entities by predicate prefix, limit 100.
	predicates := []string{
		"sensor.measurement",
		"sensor.classification",
		"geo.location",
		"iot.sensor",
		"time.observation",
	}
	for i := range 20 {
		pred := predicates[i%len(predicates)]
		pool = append(pool, querySpec{
			Name: "predicate",
			Query: fmt.Sprintf(
				`{"query":"{ entitiesByPredicate(predicate: \"%s\", limit: 100) }"}`,
				pred),
		})
	}

	// Temporal queries (10 entries = 10%)
	windows := []struct{ start, end string }{
		{"2024-11-15T00:00:00Z", "2024-11-15T23:59:59Z"},
		{"2024-11-15T06:00:00Z", "2024-11-15T12:00:00Z"},
		{"2024-11-15T08:00:00Z", "2024-11-15T10:00:00Z"},
		{"2024-11-14T00:00:00Z", "2024-11-16T00:00:00Z"},
	}
	for i := range 10 {
		w := windows[i%len(windows)]
		pool = append(pool, querySpec{
			Name: "temporal",
			Query: fmt.Sprintf(
				`{"query":"{ temporalSearch(startTime: \"%s\", endTime: \"%s\", limit: 50) { id triples { subject predicate object } } }"}`,
				w.start, w.end),
		})
	}

	return pool
}

// queryObservation records a single query execution.
type queryObservation struct {
	queryType string
	latency   time.Duration
	err       bool
	notFound  bool
	errorMsg  string // first error message (for diagnostics)
}

// runQueryLoad fires concurrent GraphQL queries for the configured duration.
func runQueryLoad(ctx context.Context, cfg *Config) (*QueryLoadResult, error) {
	pool := buildQueryPool()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	var (
		mu           sync.Mutex
		observations []queryObservation
	)

	loadCtx, cancel := context.WithTimeout(ctx, cfg.QueryDuration)
	defer cancel()

	var wg sync.WaitGroup
	start := time.Now()

	for range cfg.QueryConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
			workerLoop(loadCtx, rng, pool, httpClient, cfg.GraphQLURL, &mu, &observations)
		}()
	}

	wg.Wait()
	return aggregateObservations(observations, time.Since(start)), nil
}

// workerLoop runs a tight query loop until the context expires.
func workerLoop(ctx context.Context, rng *rand.Rand, pool []querySpec, httpClient *http.Client,
	graphqlURL string, mu *sync.Mutex, observations *[]queryObservation) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		spec := pool[rng.IntN(len(pool))]
		qStart := time.Now()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlURL,
			bytes.NewReader([]byte(spec.Query)))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		latency := time.Since(qStart)

		// If the load-phase context expired, the request was cancelled as part of
		// normal shutdown — not a real error.  Drop the observation and exit.
		if err != nil && ctx.Err() != nil {
			return
		}

		isErr := err != nil
		isNotFound := false
		var errMsg string

		if err != nil {
			errMsg = err.Error()
		} else {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil {
				var gqlResp struct {
					Errors []struct {
						Message string `json:"message"`
					} `json:"errors"`
				}
				if jsonErr := json.Unmarshal(body, &gqlResp); jsonErr == nil && len(gqlResp.Errors) > 0 {
					errMsg = gqlResp.Errors[0].Message
					// Classify: "not found" is a data-level miss, not an infra error
					if strings.Contains(errMsg, "not found") {
						isNotFound = true
					} else {
						isErr = true
					}
				}
			}
		}

		mu.Lock()
		*observations = append(*observations, queryObservation{
			queryType: spec.Name,
			latency:   latency,
			err:       isErr,
			notFound:  isNotFound,
			errorMsg:  errMsg,
		})
		mu.Unlock()
	}
}

// aggregateObservations computes summary statistics from raw observations.
func aggregateObservations(observations []queryObservation, elapsed time.Duration) *QueryLoadResult {
	result := &QueryLoadResult{
		Duration: elapsed,
		ByType:   make(map[string]QueryTypeStats),
	}

	notFoundSeen := make(map[string]bool)
	errorSeen := make(map[string]bool)
	latencies := make([]float64, 0, len(observations))
	for _, obs := range observations {
		result.TotalQueries++
		if obs.err {
			result.ErrorCount++
			if obs.errorMsg != "" && !errorSeen[obs.errorMsg] {
				errorSeen[obs.errorMsg] = true
				result.ErrorDetails = append(result.ErrorDetails, obs.errorMsg)
			}
		}
		if obs.notFound {
			result.NotFoundCount++
			if obs.errorMsg != "" && !notFoundSeen[obs.errorMsg] {
				notFoundSeen[obs.errorMsg] = true
				result.NotFoundDetails = append(result.NotFoundDetails, obs.errorMsg)
			}
		}
		latencies = append(latencies, float64(obs.latency.Milliseconds()))

		stats := result.ByType[obs.queryType]
		stats.Count++
		if obs.err {
			stats.Errors++
		}
		if obs.notFound {
			stats.NotFound++
		}
		stats.TotalMs += float64(obs.latency.Milliseconds())
		result.ByType[obs.queryType] = stats
	}

	for name, stats := range result.ByType {
		if stats.Count > 0 {
			stats.AvgLatencyMs = stats.TotalMs / float64(stats.Count)
		}
		result.ByType[name] = stats
	}

	if elapsed.Seconds() > 0 {
		result.QueriesPerSec = float64(result.TotalQueries) / elapsed.Seconds()
	}

	if len(latencies) > 0 {
		sort.Float64s(latencies)
		result.P50LatencyMs = percentile(latencies, 0.50)
		result.P95LatencyMs = percentile(latencies, 0.95)
		result.P99LatencyMs = percentile(latencies, 0.99)
	}

	return result
}

// percentile returns the p-th percentile from a sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// printQueryLoadSummary prints a human-readable summary of query load results.
func printQueryLoadSummary(r *QueryLoadResult) {
	fmt.Printf("\n  Query Load: %d queries in %v (%.0f q/sec)\n",
		r.TotalQueries, r.Duration.Round(time.Millisecond), r.QueriesPerSec)
	for name, stats := range r.ByType {
		errInfo := fmt.Sprintf("%d errors", stats.Errors)
		if stats.NotFound > 0 {
			errInfo = fmt.Sprintf("%d errors, %d not-found", stats.Errors, stats.NotFound)
		}
		fmt.Printf("    %-10s %d queries, avg %.0fms, %s\n",
			name+":", stats.Count, stats.AvgLatencyMs, errInfo)
	}
	fmt.Printf("  P50: %.0fms  P95: %.0fms  P99: %.0fms\n",
		r.P50LatencyMs, r.P95LatencyMs, r.P99LatencyMs)
	if r.ErrorCount > 0 || r.NotFoundCount > 0 {
		fmt.Printf("  Errors: %d infra, %d not-found (%.1f%% total failure rate)\n",
			r.ErrorCount, r.NotFoundCount,
			float64(r.ErrorCount)/float64(r.TotalQueries)*100)
		for _, detail := range r.ErrorDetails {
			fmt.Printf("    infra: %s\n", detail)
		}
		for _, detail := range r.NotFoundDetails {
			fmt.Printf("    not-found: %s\n", detail)
		}
	}
}

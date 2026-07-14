// Package graphindextemporal query handlers
package graphindextemporal

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// setupQueryHandlers sets up NATS request/reply subscriptions for query handlers
func (c *Component) setupQueryHandlers(ctx context.Context) error {
	// Subscribe to temporal range query
	sub, err := c.natsClient.SubscribeForRequests(ctx, "graph.temporal.query.range", c.handleQueryRangeNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe range query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	c.logger.Info("query handlers registered",
		"subjects", []string{"graph.temporal.query.range"})

	return nil
}

// TemporalResult represents an entity found in temporal search
type TemporalResult struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// handleQueryRangeNATS handles temporal range queries via NATS request/reply
func (c *Component) handleQueryRangeNATS(ctx context.Context, data []byte) ([]byte, error) {
	if err := c.ensureBootstrapReady(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var req struct {
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryRangeNATS", "invalid request JSON")
	}

	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryRangeNATS", "invalid startTime format")
	}
	endTime, err := time.Parse(time.RFC3339, req.EndTime)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryRangeNATS", "invalid endTime format")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	// Use targeted prefix scan instead of full key enumeration.
	prefixes := generateTemporalPrefixes(startTime, endTime)
	var keys []string
	for _, prefix := range prefixes {
		matched, err := natsclient.FilteredKeys(ctx, c.temporalBucket, prefix)
		if err != nil {
			return nil, errs.WrapTransient(err, "Component", "handleQueryRangeNATS", "list temporal index keys")
		}
		keys = append(keys, matched...)
	}

	results, err := c.collectTemporalResults(ctx, keys, startTime, endTime, limit)
	if err != nil {
		return nil, errs.WrapTransient(err, "Component", "handleQueryRangeNATS", "fetch temporal index buckets")
	}
	return json.Marshal(results)
}

// collectTemporalResults fetches time buckets and filters events within the query range.
func (c *Component) collectTemporalResults(ctx context.Context, keys []string, startTime, endTime time.Time, limit int) ([]TemporalResult, error) {
	results := make([]TemporalResult, 0)
	seen := make(map[string]bool)

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(results) >= limit {
			break
		}

		bucketTime, err := parseTimeBucketKey(key)
		if err != nil {
			continue
		}

		// Skip if bucket is outside the query range
		bucketEnd := bucketTime.Add(time.Hour)
		if bucketEnd.Before(startTime) || bucketTime.After(endTime) {
			continue
		}

		entry, err := c.temporalBucket.Get(ctx, key)
		if err != nil {
			if natsclient.IsKVNotFoundError(err) {
				continue
			}
			return nil, err
		}

		var temporalData struct {
			Events []struct {
				Entity    string `json:"entity"`
				Type      string `json:"type"`
				Timestamp string `json:"timestamp"`
			} `json:"events"`
		}
		if err := json.Unmarshal(entry.Value(), &temporalData); err != nil {
			return nil, err
		}

		for _, event := range temporalData.Events {
			if seen[event.Entity] {
				continue
			}
			eventTime, err := time.Parse(time.RFC3339, event.Timestamp)
			if err != nil {
				continue
			}
			if eventTime.Before(startTime) || eventTime.After(endTime) {
				continue
			}
			seen[event.Entity] = true
			results = append(results, TemporalResult{ID: event.Entity, Type: "entity"})
			if len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

// parseTimeBucketKey parses a time bucket key in format "YYYY.MM.DD.HH"
func parseTimeBucketKey(key string) (time.Time, error) {
	var year, month, day, hour int
	_, err := fmt.Sscanf(key, "%04d.%02d.%02d.%02d", &year, &month, &day, &hour)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC), nil
}

// generateTemporalPrefixes produces NATS wildcard patterns that cover [startTime, endTime]
// without scanning every bucket key.
//
// Key format is YYYY.MM.DD.HH; dots are NATS subject separators, so the server
// honours ">" suffix filtering server-side.
//
//   - Ranges ≤ 30 days  → one "YYYY.MM.DD.>" pattern per calendar day
//   - Ranges  > 30 days → one "YYYY.MM.>" pattern per calendar month
func generateTemporalPrefixes(startTime, endTime time.Time) []string {
	// Truncate to UTC so boundary arithmetic is clean.
	start := startTime.UTC()
	end := endTime.UTC()

	// Number of whole days covered by the range (ceiling so a partial last day
	// is still included).
	hours := end.Sub(start).Hours()
	days := int(math.Ceil(hours / 24))

	if days <= 30 {
		// Day-level prefixes: one per calendar day in the range.
		var prefixes []string
		current := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
		for !current.After(endDay) {
			prefixes = append(prefixes,
				fmt.Sprintf("%04d.%02d.%02d.>", current.Year(), int(current.Month()), current.Day()))
			current = current.AddDate(0, 0, 1)
		}
		return prefixes
	}

	// Month-level prefixes: one per calendar month in the range.
	var prefixes []string
	current := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	endMonth := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !current.After(endMonth) {
		prefixes = append(prefixes,
			fmt.Sprintf("%04d.%02d.>", current.Year(), int(current.Month())))
		current = current.AddDate(0, 1, 0)
	}
	return prefixes
}

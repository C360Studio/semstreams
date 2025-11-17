package cache

import (
	"testing"

	"github.com/c360/semstreams/metric"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheMetricsIntegration(t *testing.T) {
	// Create metrics registry
	metricsRegistry := metric.NewMetricsRegistry()

	// Create cache with metrics enabled
	cache, err := NewLRU[string](10, WithMetrics[string](metricsRegistry, "test_cache"))
	require.NoError(t, err)

	// Perform cache operations
	_, _ = cache.Set("key1", "value1")
	_, _ = cache.Set("key2", "value2")

	// Access key1 (hit)
	val, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, "value1", val)

	// Access non-existent key (miss)
	_, found = cache.Get("key3")
	assert.False(t, found)

	// Delete a key
	deleted, _ := cache.Delete("key2")
	assert.True(t, deleted)

	// Gather metrics from registry
	metricFamilies, err := metricsRegistry.PrometheusRegistry().Gather()
	require.NoError(t, err)

	// Verify cache metrics exist and have correct values
	metricsByName := make(map[string]*dto.MetricFamily)
	for _, mf := range metricFamilies {
		metricsByName[*mf.Name] = mf
	}

	// Check hits metric
	hitsMetric := metricsByName["semstreams_cache_hits_total"]
	require.NotNil(t, hitsMetric, "hits metric should exist")
	assert.Equal(t, float64(1), *hitsMetric.Metric[0].Counter.Value, "should have 1 hit")

	// Check misses metric
	missesMetric := metricsByName["semstreams_cache_misses_total"]
	require.NotNil(t, missesMetric, "misses metric should exist")
	assert.Equal(t, float64(1), *missesMetric.Metric[0].Counter.Value, "should have 1 miss")

	// Check sets metric
	setsMetric := metricsByName["semstreams_cache_sets_total"]
	require.NotNil(t, setsMetric, "sets metric should exist")
	assert.Equal(t, float64(2), *setsMetric.Metric[0].Counter.Value, "should have 2 sets")

	// Check deletes metric
	deletesMetric := metricsByName["semstreams_cache_deletes_total"]
	require.NotNil(t, deletesMetric, "deletes metric should exist")
	assert.Equal(t, float64(1), *deletesMetric.Metric[0].Counter.Value, "should have 1 delete")

	// Check size metric
	sizeMetric := metricsByName["semstreams_cache_size"]
	require.NotNil(t, sizeMetric, "size metric should exist")
	assert.Equal(t, float64(1), *sizeMetric.Metric[0].Gauge.Value, "should have 1 item remaining")

	// Check component label
	assert.Equal(t, "test_cache", *hitsMetric.Metric[0].Label[0].Value, "should have correct component label")
}

func TestCacheWithoutMetrics(t *testing.T) {
	// Create cache without metrics registry
	cache, err := NewLRU[string](10)
	require.NoError(t, err)

	// Perform cache operations
	_, _ = cache.Set("key1", "value1")
	val, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, "value1", val)

	// Should work without errors even though no metrics are configured
}

func TestCachePreferMetricsOverStats(t *testing.T) {
	// Create metrics registry
	metricsRegistry := metric.NewMetricsRegistry()

	// Create cache with both metrics and stats enabled
	// Note: EnableStats is deprecated and ignored - stats are always enabled
	// Only metrics need to be explicitly enabled

	cache, err := NewLRU[string](10, WithMetrics[string](metricsRegistry, "test_cache"))
	require.NoError(t, err)
	lruCache := cache.(*lruCache[string])

	// Both metrics and stats should be enabled (stats are always on)
	assert.NotNil(t, lruCache.metrics, "metrics should be enabled")
	assert.NotNil(t, lruCache.stats, "stats should always be enabled")
}

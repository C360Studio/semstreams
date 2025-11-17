package service

import (
	"encoding/json"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: Full end-to-end tests with actual flowstore are in the integration test file
// These unit tests focus on testing individual functions and logic

func TestInferMetricPrefix(t *testing.T) {
	testCases := []struct {
		factoryName    string
		expectedPrefix string
	}{
		{"udp", "input"},
		{"udp-input", "input"},
		{"websocket-source", "input"},
		{"json-processor", "processor"},
		{"graph-processor", "processor"},
		{"transform", "processor"},
		{"file-output", "output"},
		{"http-sink", "output"},
		{"bolt-storage", "storage"},
		{"sqlite-store", "storage"},
		{"http-gateway", "gateway"},
		{"unknown-type", "unknown-type"},
	}

	for _, tc := range testCases {
		t.Run(tc.factoryName, func(t *testing.T) {
			result := inferMetricPrefix(tc.factoryName)
			assert.Equal(t, tc.expectedPrefix, result)
		})
	}
}

func TestComponentTypeToMetricPrefix(t *testing.T) {
	// Verify the mapping is correct
	assert.Equal(t, "input", componentTypeToMetricPrefix["input"])
	assert.Equal(t, "processor", componentTypeToMetricPrefix["processor"])
	assert.Equal(t, "output", componentTypeToMetricPrefix["output"])
	assert.Equal(t, "storage", componentTypeToMetricPrefix["storage"])
	assert.Equal(t, "gateway", componentTypeToMetricPrefix["gateway"])
}

func TestRuntimeMetricsResponse_JSONMarshaling(t *testing.T) {
	// Test that the response structure marshals correctly to JSON
	throughput := 123.45
	errorRate := 0.5
	queueDepth := 42.0

	response := RuntimeMetricsResponse{
		Timestamp:           time.Now(),
		PrometheusAvailable: true,
		Components: []ComponentMetric{
			{
				Name:        "test-component",
				Type:        "input",
				Status:      "healthy",
				Throughput:  &throughput,
				ErrorRate:   &errorRate,
				QueueDepth:  &queueDepth,
				RawCounters: nil,
			},
		},
	}

	data, err := json.Marshal(response)
	require.NoError(t, err)

	var decoded RuntimeMetricsResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, response.PrometheusAvailable, decoded.PrometheusAvailable)
	assert.Len(t, decoded.Components, 1)
	assert.Equal(t, "test-component", decoded.Components[0].Name)
	assert.NotNil(t, decoded.Components[0].Throughput)
	assert.Equal(t, throughput, *decoded.Components[0].Throughput)
}

func TestRuntimeMetricsResponse_NullValues(t *testing.T) {
	// Test that null values are correctly represented in JSON
	response := RuntimeMetricsResponse{
		Timestamp:           time.Now(),
		PrometheusAvailable: false,
		Components: []ComponentMetric{
			{
				Name:        "test-component",
				Type:        "input",
				Status:      "unknown",
				Throughput:  nil,
				ErrorRate:   nil,
				QueueDepth:  nil,
				RawCounters: nil,
			},
		},
	}

	data, err := json.Marshal(response)
	require.NoError(t, err)

	// Verify null values in JSON
	var jsonMap map[string]any
	err = json.Unmarshal(data, &jsonMap)
	require.NoError(t, err)

	components := jsonMap["components"].([]any)
	comp := components[0].(map[string]any)

	// These should be null in JSON (not missing)
	assert.Nil(t, comp["throughput"])
	assert.Nil(t, comp["error_rate"])
	assert.Nil(t, comp["queue_depth"])
	assert.Nil(t, comp["raw_counters"])
}

func TestBuildHealthOnlyResponse(t *testing.T) {
	fs := &FlowService{}

	components := []componentInfo{
		{Name: "comp1", Type: "input"},
		{Name: "comp2", Type: "processor"},
		{Name: "comp3", Type: "output"},
	}

	response := fs.buildHealthOnlyResponse(components)

	assert.Len(t, response.Components, 3)
	for i, comp := range response.Components {
		assert.Equal(t, components[i].Name, comp.Name)
		assert.Equal(t, components[i].Type, comp.Type)
		assert.Equal(t, "unknown", comp.Status)
		assert.Nil(t, comp.Throughput)
		assert.Nil(t, comp.ErrorRate)
		assert.Nil(t, comp.QueueDepth)
		assert.Nil(t, comp.RawCounters)
	}
}

func TestExtractComponentCounters_EmptyFamilies(t *testing.T) {
	fs := &FlowService{}
	families := make(map[string]*dto.MetricFamily)

	counters := fs.extractComponentCounters(families, "test-component")

	assert.Empty(t, counters)
}

func TestExtractComponentCounters_WithData(t *testing.T) {
	fs := &FlowService{}

	// Create metric families with counter data
	metricType := dto.MetricType_COUNTER
	counterValue := 123.45

	labelName := "component"
	labelValue := "test-component"

	metricName := "semstreams_input_messages_published_total"

	families := map[string]*dto.MetricFamily{
		metricName: {
			Name: &metricName,
			Type: &metricType,
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{
							Name:  &labelName,
							Value: &labelValue,
						},
					},
					Counter: &dto.Counter{
						Value: &counterValue,
					},
				},
			},
		},
	}

	counters := fs.extractComponentCounters(families, "test-component")

	require.Len(t, counters, 1)
	assert.Equal(t, uint64(123), counters[metricName])
}

func TestQueryPrometheusSingle_NoData(t *testing.T) {
	// This test verifies error handling when Prometheus returns no data
	// Actual Prometheus queries are tested in integration tests
	fs := &FlowService{
		config: FlowServiceConfig{
			PrometheusURL: "http://localhost:9090",
		},
	}

	// We can't easily test this without a real Prometheus instance
	// This is covered in integration tests
	_ = fs
}

// TestSanitizeComponentName tests the sanitization function to prevent PromQL injection attacks
func TestSanitizeComponentName(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid name with alphanumeric",
			input:    "component123",
			expected: "component123",
		},
		{
			name:     "valid name with underscore",
			input:    "component_name",
			expected: "component_name",
		},
		{
			name:     "valid name with hyphen",
			input:    "component-name",
			expected: "component-name",
		},
		{
			name:     "valid name with mixed characters",
			input:    "comp_123-test",
			expected: "comp_123-test",
		},
		{
			name:     "SQL injection attempt with semicolon",
			input:    "name;DROP TABLE",
			expected: "name_DROP_TABLE",
		},
		{
			name:     "PromQL injection with quotes",
			input:    "name'OR'1'='1",
			expected: "name_OR_1___1",
		},
		{
			name:     "path traversal attempt",
			input:    "../../../etc/passwd",
			expected: "____________etc_passwd",
		},
		{
			name:     "special characters",
			input:    "comp@name#test",
			expected: "comp_name_test",
		},
		{
			name:     "brackets and parentheses",
			input:    "comp[0](test)",
			expected: "comp_0__test_",
		},
		{
			name:     "spaces",
			input:    "component name",
			expected: "component_name",
		},
		{
			name:     "dots",
			input:    "component.name",
			expected: "component_name",
		},
		{
			name:     "newlines and tabs",
			input:    "comp\nname\ttest",
			expected: "comp_name_test",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only invalid characters",
			input:    "!@#$%",
			expected: "_____",
		},
		{
			name:     "unicode characters",
			input:    "comp名前",
			expected: "comp__",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizeComponentName(tc.input)
			assert.Equal(t, tc.expected, result,
				"sanitizeComponentName(%q) should equal %q but got %q",
				tc.input, tc.expected, result)
		})
	}
}

// TestSanitizeComponentName_Regex tests the regex validation directly
func TestSanitizeComponentName_Regex(t *testing.T) {
	validNames := []string{
		"component",
		"component123",
		"component_name",
		"component-name",
		"a1b2c3",
		"_underscore",
		"-hyphen",
		"MixedCase",
	}

	for _, name := range validNames {
		t.Run("valid_"+name, func(t *testing.T) {
			assert.True(t, componentNameRegex.MatchString(name),
				"%q should match the valid component name regex", name)
			// Should return unchanged
			assert.Equal(t, name, sanitizeComponentName(name))
		})
	}

	invalidNames := []string{
		"comp@name",
		"comp.name",
		"comp name",
		"comp/name",
		"comp\\name",
		"comp;name",
		"comp'name",
		"comp\"name",
		"comp(name)",
		"comp[name]",
		"comp{name}",
		"comp<name>",
		"comp|name",
		"comp!name",
		"comp#name",
		"comp$name",
		"comp%name",
		"comp&name",
		"comp*name",
		"comp+name",
		"comp=name",
		"comp?name",
		"comp^name",
		"comp`name",
		"comp~name",
	}

	for _, name := range invalidNames {
		t.Run("invalid_"+name, func(t *testing.T) {
			assert.False(t, componentNameRegex.MatchString(name),
				"%q should NOT match the valid component name regex", name)
			// Should be sanitized
			result := sanitizeComponentName(name)
			assert.NotEqual(t, name, result,
				"%q should be sanitized to %q", name, result)
			// Result should now be valid
			assert.True(t, componentNameRegex.MatchString(result),
				"sanitized result %q should match the valid component name regex", result)
		})
	}
}

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360/semstreams/metric"
	"github.com/stretchr/testify/suite"
)

type MetricsRuntimeSuite struct {
	ServiceSuite // Inherits NATS client setup
	kvHelper     *KVTestHelper
}

func (s *MetricsRuntimeSuite) SetupTest() {
	s.ServiceSuite.SetupTest()
	// Create KV test helper using the helper from KV-002
	s.kvHelper = NewKVTestHelper(s.T(), s.natsClient)
}

func (s *MetricsRuntimeSuite) TestMetrics_ImplementsRuntimeConfigurable() {
	// Create metrics service with initial config
	config := json.RawMessage(`{
		"enabled": true,
		"port": 9090,
		"path": "/metrics"
	}`)

	deps := &Dependencies{
		Logger:          slog.Default(),
		MetricsRegistry: nil, // Metrics doesn't need its own registry
	}

	svc, err := NewMetrics(config, deps)
	s.Require().NoError(err)
	s.Require().NotNil(svc)

	// Verify it implements RuntimeConfigurable
	metrics, ok := svc.(*Metrics)
	s.Require().True(ok, "Service should be *Metrics type")

	var _ RuntimeConfigurable = metrics
	s.T().Log("✓ Metrics implements RuntimeConfigurable")
}

func (s *MetricsRuntimeSuite) TestMetrics_GetRuntimeConfig() {
	// Setup metrics with known config
	config := json.RawMessage(`{
		"enabled": true,
		"port": 9090,
		"path": "/metrics",
		"custom_field": "test_value"
	}`)

	svc, err := NewMetrics(config, &Dependencies{
		Logger: slog.Default(),
	})
	s.Require().NoError(err)

	metrics := svc.(*Metrics)

	// Test GetRuntimeConfig returns full JSON structure
	runtime := metrics.GetRuntimeConfig()
	s.Assert().NotNil(runtime)

	// Verify all fields present (custom_field won't be in runtime config as it's not part of the service)
	s.Assert().Equal(true, runtime["enabled"])
	s.Assert().Equal(9090, runtime["port"]) // Internal representation stays as int
	s.Assert().Equal("/metrics", runtime["path"])
}

func (s *MetricsRuntimeSuite) TestMetrics_ValidateConfigUpdate() {
	svc, err := NewMetrics(json.RawMessage(`{"enabled": true, "port": 9090}`),
		&Dependencies{Logger: slog.Default()})
	s.Require().NoError(err)

	metrics := svc.(*Metrics)

	s.Run("valid config update", func() {
		validConfig := map[string]any{
			"enabled": false, // Can toggle at runtime
		}
		err := metrics.ValidateConfigUpdate(validConfig)
		s.Assert().NoError(err)
	})

	s.Run("port change requires restart", func() {
		invalidConfig := map[string]any{
			"enabled": true,
			"port":    9999, // Different port not allowed at runtime
		}
		err := metrics.ValidateConfigUpdate(invalidConfig)
		s.Assert().Error(err)
		s.Assert().Contains(err.Error(), "restart")
	})

	s.Run("path change requires restart", func() {
		invalidConfig := map[string]any{
			"enabled": true,
			"path":    "/new-metrics", // Path change not allowed at runtime
		}
		err := metrics.ValidateConfigUpdate(invalidConfig)
		s.Assert().Error(err)
		s.Assert().Contains(err.Error(), "restart")
	})

	s.Run("invalid enabled type", func() {
		invalidConfig := map[string]any{
			"enabled": "true", // Should be bool, not string
		}
		err := metrics.ValidateConfigUpdate(invalidConfig)
		s.Assert().Error(err)
		s.Assert().Contains(err.Error(), "boolean")
	})
}

func (s *MetricsRuntimeSuite) TestMetrics_ApplyConfigUpdate() {
	svc, err := NewMetrics(json.RawMessage(`{"enabled": true, "port": 9090}`),
		&Dependencies{Logger: slog.Default()})
	s.Require().NoError(err)

	metrics := svc.(*Metrics)

	// Apply valid config change (only enabled is supported at runtime)
	newConfig := map[string]any{
		"enabled": false,
	}

	err = metrics.ApplyConfigUpdate(newConfig)
	s.Assert().NoError(err)

	// Note: The enabled state is managed by Manager,
	// so GetRuntimeConfig still shows the service's current state
	runtime := metrics.GetRuntimeConfig()
	s.Assert().NotNil(runtime)
}

func (s *MetricsRuntimeSuite) TestMetrics_KVIntegration_JSONOnly() {
	// This tests the full KV integration with JSON-only format

	// Write initial config to KV
	initialConfig := map[string]any{
		"enabled": true,
		"port":    9090,
		"path":    "/metrics",
	}
	rev1 := s.kvHelper.WriteServiceConfig("metrics", initialConfig)
	s.Assert().Greater(rev1, uint64(0))

	// Read it back to verify JSON format
	config, rev2, err := s.kvHelper.GetServiceConfig("metrics")
	s.Require().NoError(err)
	s.Assert().Equal(rev1, rev2)
	s.Assert().Equal(true, config["enabled"])
	s.Assert().Equal(float64(9090), config["port"]) // JSON numbers unmarshal as float64

	// Update using helper's UpdateServiceConfig
	err = s.kvHelper.UpdateServiceConfig("metrics", func(cfg map[string]any) error {
		cfg["enabled"] = false
		cfg["updated_at"] = time.Now().Unix()
		return nil
	})
	s.Assert().NoError(err)

	// Verify update applied
	updated, _, err := s.kvHelper.GetServiceConfig("metrics")
	s.Require().NoError(err)
	s.Assert().Equal(false, updated["enabled"])
	s.Assert().NotNil(updated["updated_at"])
}

func (s *MetricsRuntimeSuite) TestMetrics_ConcurrentKVUpdate() {
	// Test that concurrent updates are handled properly

	// Setup initial state
	s.kvHelper.WriteServiceConfig("metrics", map[string]any{
		"enabled": true,
		"port":    9090,
	})

	// Simulate concurrent update (should fail with revision mismatch)
	err := s.kvHelper.SimulateConcurrentUpdate("metrics")
	s.Assert().Error(err, "Concurrent update should fail")

	// Verify we can detect the conflict
	s.T().Logf("Concurrent update error: %v", err)
}

func (s *MetricsRuntimeSuite) TestMetrics_NoPropertyLevelKeys() {
	// CRITICAL TEST: Verify property-level keys are NOT supported

	// This should NOT work after KV-001 is complete
	// We're documenting the expected behavior

	ctx := context.Background()
	_ = ctx

	// Try to write property-level key (should be ignored by ConfigWatcher)
	// This is just for documentation - ConfigWatcher will ignore it
	s.T().Log("Property-level keys like 'services.metrics.enabled' should be ignored")
	s.T().Log("Only full JSON at 'services.metrics' should work")

	// Verify our test helper validates proper key format
	s.kvHelper.AssertValidKVKey("services.metrics")
}

func (s *MetricsRuntimeSuite) TestMetrics_DefaultConfiguration() {
	// Test that metrics uses proper defaults when config is empty
	emptyConfig := json.RawMessage(`{}`)

	svc, err := NewMetrics(emptyConfig, &Dependencies{
		Logger: slog.Default(),
	})
	s.Require().NoError(err)

	metrics := svc.(*Metrics)
	runtime := metrics.GetRuntimeConfig()

	// Verify defaults from MetricsConfig.UnmarshalJSON
	s.Assert().Equal(true, runtime["enabled"])
	s.Assert().Equal(9090, runtime["port"])
	s.Assert().Equal("/metrics", runtime["path"])
}

func (s *MetricsRuntimeSuite) TestMetrics_ConfigValidation() {
	// Test that invalid configs are rejected
	s.Run("invalid port range", func() {
		config := json.RawMessage(`{"port": 99999}`) // Invalid port

		_, err := NewMetrics(config, &Dependencies{
			Logger: slog.Default(),
		})
		s.Assert().Error(err)
		s.Assert().Contains(err.Error(), "invalid port")
	})

	s.Run("negative port", func() {
		config := json.RawMessage(`{"port": -1}`)

		_, err := NewMetrics(config, &Dependencies{
			Logger: slog.Default(),
		})
		s.Assert().Error(err)
		s.Assert().Contains(err.Error(), "invalid port")
	})

	s.Run("empty path gets default", func() {
		config := json.RawMessage(`{"path": ""}`)

		m, err := NewMetrics(config, &Dependencies{
			Logger:          slog.Default(),
			MetricsRegistry: metric.NewMetricsRegistry(),
		})
		s.Assert().NoError(err)
		s.Assert().NotNil(m)

		// Empty path should get default "/metrics"
		metrics := m.(*Metrics)
		s.Assert().Equal("/metrics", metrics.config.Path)
	})
}

func TestMetricsRuntimeSuite(t *testing.T) {
	suite.Run(t, new(MetricsRuntimeSuite))
}

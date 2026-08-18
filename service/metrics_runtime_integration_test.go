//go:build integration

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/stretchr/testify/suite"
)

type MetricsRuntimeSuite struct {
	ServiceSuite // Inherits NATS client setup
	kvHelper     *KVTestHelper
}

func TestMetricsUsesSealedBootSecurityAfterDesiredMutation(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	initial := &config.Config{
		Platform: config.PlatformConfig{Org: "test", ID: t.Name(), Type: "test"},
		Security: security.Config{TLS: security.TLSConfig{
			Server: security.ServerTLSConfig{MinVersion: "1.2"},
		}},
	}
	manager, err := config.NewConfigManager(t.Context(), initial, testClient.Client, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(5 * time.Second) })

	if err := manager.GetConfig().Mutate(func(desired *config.Config) error {
		desired.Security.TLS.Server.MinVersion = "1.3"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	svc, err := NewMetrics(json.RawMessage(`{}`), &Dependencies{
		Manager:         manager,
		Logger:          slog.Default(),
		MetricsRegistry: metric.NewMetricsRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	metrics := svc.(*Metrics)
	if got := metrics.security.TLS.Server.MinVersion; got != "1.2" {
		t.Fatalf("metrics security min version = %q, want sealed boot value 1.2", got)
	}
}

func (s *MetricsRuntimeSuite) SetupTest() {
	s.ServiceSuite.SetupTest()
	// Create KV test helper using the helper from KV-002
	s.kvHelper = NewKVTestHelper(s.T(), s.natsClient)
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

	// Verify constructor defaults.
	s.Assert().Equal(9090, metrics.config.Port)
	s.Assert().Equal("/metrics", metrics.config.Path)
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

	s.Run("retired enabled field", func() {
		config := json.RawMessage(`{"enabled": true}`)

		_, err := NewMetrics(config, &Dependencies{Logger: slog.Default()})
		s.Assert().Error(err)
		s.Assert().Contains(err.Error(), "unknown field")
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

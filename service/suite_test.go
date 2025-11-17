package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c360/semstreams/config"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/types"
)

// ServiceSuite provides shared testcontainer infrastructure for all service tests
type ServiceSuite struct {
	suite.Suite
	testClient *natsclient.TestClient
	natsClient *natsclient.Client
}

// SetupSuite uses the package-level shared NATS container
func (s *ServiceSuite) SetupSuite() {
	// Use the shared container created in TestMain
	s.testClient = sharedTestClient
	s.natsClient = sharedNATSClient

	// Verify shared resources are available
	if s.testClient == nil || s.natsClient == nil {
		s.T().Fatal("Shared test resources not initialized - TestMain should have created them")
	}
}

// TearDownSuite no longer needs to clean up (handled by TestMain)
func (s *ServiceSuite) TearDownSuite() {
	// Cleanup is handled by TestMain - do not terminate shared container here
}

// SetupTest runs before each test method to ensure clean state
func (s *ServiceSuite) SetupTest() {
	// Ensure NATS connection is healthy before each test
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.natsClient.WaitForConnection(ctx)
	s.Require().NoError(err, "NATS connection should be ready for test")
}

// TestServiceSuite runs the test suite
func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}

// Helper methods for the suite

// getNATSClient returns the shared NATS client for tests
func (s *ServiceSuite) getNATSClient() *natsclient.Client {
	return s.natsClient
}

// getNATSURL returns the NATS URL for configuration
func (s *ServiceSuite) getNATSURL() string {
	return s.testClient.URL
}

// TestService_CreationSuite demonstrates suite pattern for service tests
func (s *ServiceSuite) TestService_CreationSuite() {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			ID:   "test-platform",
			Type: "vessel",
		},
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"default_timeout": "30s", "health_interval": "10s"}`),
			},
		},
	}

	// Use shared NATS client instead of creating new container
	natsClient := s.getNATSClient()

	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	s.NotNil(service)
	s.Equal("test-service", service.Name())
	s.Equal(StatusStopped, service.Status())
	s.False(service.IsHealthy())
}

// TestService_LifecycleSuite demonstrates suite pattern for lifecycle tests
func (s *ServiceSuite) TestService_LifecycleSuite() {
	cfg := &config.Config{
		Platform: config.PlatformConfig{
			ID:   "test-platform",
			Type: "vessel",
		},
		Services: types.ServiceConfigs{
			"test": types.ServiceConfig{
				Name:    "test",
				Enabled: true,
				Config:  json.RawMessage(`{"default_timeout": "100ms", "health_interval": "50ms"}`),
			},
		},
	}

	// Use shared NATS client
	natsClient := s.getNATSClient()
	service := NewBaseServiceWithOptions("test-service", cfg,
		WithNATS(natsClient),
		WithMetrics(metric.NewMetricsRegistry()))

	// Start service
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := service.Start(ctx)
	s.Require().NoError(err)
	s.Equal(StatusRunning, service.Status())

	// Wait briefly for initialization
	time.Sleep(10 * time.Millisecond)

	// Stop service
	err = service.Stop(5 * time.Second)
	s.Require().NoError(err)
	s.Equal(StatusStopped, service.Status())
}

package service

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/c360/semstreams/natsclient"
)

type KVTestHelperSuite struct {
	suite.Suite
	natsClient *natsclient.Client
	testClient *natsclient.TestClient
}

func (s *KVTestHelperSuite) SetupSuite() {
	// Use the shared container created in TestMain
	s.testClient = sharedTestClient
	s.natsClient = sharedNATSClient

	// Verify shared resources are available
	if s.testClient == nil || s.natsClient == nil {
		s.T().Fatal("Shared test resources not initialized - TestMain should have created them")
	}
}

func (s *KVTestHelperSuite) TearDownSuite() {
	// Cleanup is handled by TestMain - do not terminate shared container here
}

func (s *KVTestHelperSuite) TestKVTestHelper_JSONOnly() {
	helper := NewKVTestHelper(s.T(), s.natsClient)

	// Test writing full JSON config
	config := map[string]any{
		"enabled": true,
		"port":    9090,
		"path":    "/metrics",
	}
	rev1 := helper.WriteServiceConfig("metrics", config)
	s.Assert().Greater(rev1, uint64(0))

	// Test reading config back
	readConfig, rev2, err := helper.GetServiceConfig("metrics")
	s.Require().NoError(err)
	s.Assert().Equal(rev1, rev2)
	s.Assert().Equal(true, readConfig["enabled"])
	s.Assert().Equal(float64(9090), readConfig["port"]) // JSON numbers are float64

	// Test update with function
	err = helper.UpdateServiceConfig("metrics", func(cfg map[string]any) error {
		cfg["enabled"] = false
		cfg["new_field"] = "test"
		return nil
	})
	s.Assert().NoError(err)

	// Verify update applied
	updated, _, err := helper.GetServiceConfig("metrics")
	s.Require().NoError(err)
	s.Assert().Equal(false, updated["enabled"])
	s.Assert().Equal("test", updated["new_field"])
}

func (s *KVTestHelperSuite) TestKVTestHelper_ConcurrentUpdate() {
	helper := NewKVTestHelper(s.T(), s.natsClient)

	// Setup initial state
	helper.WriteServiceConfig("test-service", map[string]any{
		"enabled": true,
		"value":   100,
	})

	// Test concurrent update simulation
	err := helper.SimulateConcurrentUpdate("test-service")
	s.Assert().Error(err, "Should fail due to revision mismatch")
	s.Assert().True(natsclient.IsKVConflictError(err))
}

func (s *KVTestHelperSuite) TestKVTestHelper_KeyValidation() {
	helper := NewKVTestHelper(s.T(), s.natsClient)

	// Valid keys
	helper.AssertValidKVKey("services.metrics")
	helper.AssertValidKVKey("platform.id")
	helper.AssertValidKVKey("components.instances.udp_1")

	// These would panic/fail if uncommented:
	// helper.AssertValidKVKey("UPPERCASE")       // fails: uppercase
	// helper.AssertValidKVKey("invalid..key")    // fails: double dot
	// helper.AssertValidKVKey(".leading.dot")    // fails: leading dot
}

func (s *KVTestHelperSuite) TestKVTestHelper_ComponentConfig() {
	helper := NewKVTestHelper(s.T(), s.natsClient)

	// Test component config writing
	componentConfig := map[string]any{
		"type":    "udp",
		"port":    8080,
		"enabled": true,
	}

	rev := helper.WriteComponentConfig("inputs", "udp_listener", componentConfig)
	s.Assert().Greater(rev, uint64(0))
}

func TestKVTestHelperSuite(t *testing.T) {
	suite.Run(t, new(KVTestHelperSuite))
}

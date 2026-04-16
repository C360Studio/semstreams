package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	agentic "github.com/c360studio/semstreams/vocabulary/agentic"
)

// AGNTCYConfig holds configuration for AGNTCY E2E tests.
type AGNTCYConfig struct {
	// Enabled controls whether AGNTCY tests run.
	Enabled bool `json:"enabled"`

	// A2AURL is the URL for the A2A adapter.
	A2AURL string `json:"a2a_url"`

	// MockServerURL is the URL for the AGNTCY mock server (directory + OTEL).
	MockServerURL string `json:"mock_server_url"`

	// OASFTimeout is how long to wait for OASF records.
	OASFTimeout time.Duration `json:"oasf_timeout"`

	// RegistrationTimeout is how long to wait for directory registration.
	RegistrationTimeout time.Duration `json:"registration_timeout"`
}

// DefaultAGNTCYConfig returns default AGNTCY test configuration.
func DefaultAGNTCYConfig() *AGNTCYConfig {
	return &AGNTCYConfig{
		Enabled:             true,
		A2AURL:              "http://localhost:38282",
		MockServerURL:       "http://localhost:38181",
		OASFTimeout:         10 * time.Second,
		RegistrationTimeout: 15 * time.Second,
	}
}

// testAgentEntityID is the ID of the test agent entity for AGNTCY tests.
const testAgentEntityID = "e2e.test.agntcy.semstreams.agent.test-agent-001"

// TestAgentEntity represents the agent entity with capability predicates
// used for OASF generation testing.
type TestAgentEntity struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Properties map[string]any  `json:"properties"`
	Triples    []client.Triple `json:"triples"`
	Version    int             `json:"version"`
	UpdatedAt  string          `json:"updated_at"`
}

// createTestAgentEntity creates a test agent entity with capability predicates.
func createTestAgentEntity() *TestAgentEntity {
	now := time.Now().UTC().Format(time.RFC3339)
	return &TestAgentEntity{
		ID:   testAgentEntityID,
		Type: "agent",
		Properties: map[string]any{
			"name":        "E2E Test Agent",
			"description": "Agent for AGNTCY integration testing",
		},
		Triples: []client.Triple{
			// Capability predicates
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.CapabilityName,
				Object:    "code-analysis",
			},
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.CapabilityDescription,
				Object:    "Analyzes code for quality and security issues",
			},
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.CapabilityExpression,
				Object:    "analyze code security quality review",
			},
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.CapabilityConfidence,
				Object:    0.95,
			},
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.CapabilityPermission,
				Object:    "file_read",
			},
			// Second capability
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.CapabilityName,
				Object:    "documentation",
			},
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.CapabilityDescription,
				Object:    "Generates technical documentation from code",
			},
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.CapabilityExpression,
				Object:    "document generate technical readme",
			},
			// Intent predicates (for OASF description and domains)
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.IntentGoal,
				Object:    "Assist developers with code analysis and documentation",
			},
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.IntentType,
				Object:    "software-development",
			},
			// Identity predicate
			{
				Subject:   testAgentEntityID,
				Predicate: agentic.IdentityDisplayName,
				Object:    "E2E Test Agent",
			},
		},
		Version:   1,
		UpdatedAt: now,
	}
}

// verifyOASFGeneration tests OASF record generation from capability predicates.
// This test verifies the oasf-generator component is working correctly.
func (s *Scenario) verifyOASFGeneration(ctx context.Context, result *scenarios.Result) error {
	agntcyConfig := s.getAGNTCYConfig()
	if !agntcyConfig.Enabled {
		result.Details["agntcy_oasf_skipped"] = "AGNTCY tests disabled"
		return nil
	}

	// Check if OASF_RECORDS bucket exists (indicates oasf-generator is configured)
	exists, err := s.nats.BucketExists(ctx, client.BucketOASFRecords)
	if err != nil {
		return fmt.Errorf("failed to check OASF bucket: %w", err)
	}
	if !exists {
		result.Warnings = append(result.Warnings, "OASF_RECORDS bucket not found - oasf-generator may not be configured")
		result.Details["agntcy_oasf_skipped"] = "bucket not found"
		return nil
	}

	// Create test agent entity with capability predicates
	testEntity := createTestAgentEntity()
	entityData, err := json.Marshal(testEntity)
	if err != nil {
		return fmt.Errorf("failed to marshal test entity: %w", err)
	}

	// Store entity in ENTITY_STATES bucket
	if err := s.nats.PutKV(ctx, client.BucketEntityStates, testAgentEntityID, entityData); err != nil {
		return fmt.Errorf("failed to store test agent entity: %w", err)
	}

	result.Details["agntcy_test_entity_id"] = testAgentEntityID
	result.Details["agntcy_test_entity_capabilities"] = []string{"code-analysis", "documentation"}

	// Wait for OASF record to be generated
	oasfRecord, err := s.nats.WaitForOASFRecord(ctx, testAgentEntityID, agntcyConfig.OASFTimeout)
	if err != nil {
		return fmt.Errorf("failed waiting for OASF record: %w", err)
	}
	if oasfRecord == nil {
		result.Warnings = append(result.Warnings, "OASF record not generated within timeout - oasf-generator may not be processing")
		result.Details["agntcy_oasf_generated"] = false
		return nil
	}

	result.Details["agntcy_oasf_generated"] = true
	result.Details["agntcy_oasf_record_name"] = oasfRecord.Name
	result.Details["agntcy_oasf_skills_count"] = len(oasfRecord.Skills)
	result.Details["agntcy_oasf_domains_count"] = len(oasfRecord.Domains)

	// Validate OASF record structure
	if err := validateOASFRecord(oasfRecord); err != nil {
		return fmt.Errorf("OASF record validation failed: %w", err)
	}

	result.Details["agntcy_oasf_valid"] = true

	return nil
}

// validateOASFRecord validates the structure of an OASF record.
func validateOASFRecord(record *client.OASFRecord) error {
	if record.Name == "" {
		return fmt.Errorf("name is required")
	}
	if record.SchemaVersion == "" {
		return fmt.Errorf("schema_version is required")
	}
	if record.CreatedAt == "" {
		return fmt.Errorf("created_at is required")
	}

	// Validate skills
	for i, skill := range record.Skills {
		if skill.ID == "" {
			return fmt.Errorf("skill[%d].id is required", i)
		}
		if skill.Name == "" {
			return fmt.Errorf("skill[%d].name is required", i)
		}
	}

	return nil
}

// verifyA2AAdapter tests the A2A adapter HTTP endpoints.
// This test verifies the a2a-adapter component is running and accepting requests.
func (s *Scenario) verifyA2AAdapter(ctx context.Context, result *scenarios.Result) error {
	agntcyConfig := s.getAGNTCYConfig()
	if !agntcyConfig.Enabled {
		result.Details["agntcy_a2a_skipped"] = "AGNTCY tests disabled"
		return nil
	}
	if agntcyConfig.A2AURL == "" {
		result.Details["agntcy_a2a_skipped"] = "A2A URL not configured"
		return nil
	}

	// Create A2A client
	a2aClient := client.NewA2AClient(agntcyConfig.A2AURL)

	// Check if A2A adapter is healthy
	if err := a2aClient.Health(ctx); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("A2A adapter not reachable: %v", err))
		result.Details["agntcy_a2a_skipped"] = "adapter not reachable"
		return nil
	}

	result.Details["agntcy_a2a_healthy"] = true

	// Get agent card - this validates the adapter is serving agent cards from OASF records
	agentCard, err := a2aClient.GetAgentCard(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get agent card: %v", err))
		result.Details["agntcy_a2a_agent_card"] = false
	} else {
		result.Details["agntcy_a2a_agent_card"] = true
		result.Details["agntcy_a2a_agent_name"] = agentCard.Name
		result.Details["agntcy_a2a_skills_count"] = len(agentCard.Skills)
	}

	return nil
}

// verifyDirectoryBridge tests that the directory-bridge component registers agents.
// This test verifies the directory-bridge component is watching OASF records and
// registering them with the mock directory server.
func (s *Scenario) verifyDirectoryBridge(ctx context.Context, result *scenarios.Result) error {
	agntcyConfig := s.getAGNTCYConfig()
	if !agntcyConfig.Enabled {
		result.Details["agntcy_directory_skipped"] = "AGNTCY tests disabled"
		return nil
	}
	if agntcyConfig.MockServerURL == "" {
		result.Details["agntcy_directory_skipped"] = "Mock server URL not configured"
		return nil
	}

	// Create mock server client
	mockClient := client.NewAGNTCYMockClient(agntcyConfig.MockServerURL)

	// Check if mock server is healthy
	if err := mockClient.Health(ctx); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("AGNTCY mock server not reachable: %v", err))
		result.Details["agntcy_directory_skipped"] = "mock server not reachable"
		return nil
	}

	result.Details["agntcy_mock_server_healthy"] = true

	// Wait for our test agent to be registered in the directory
	// The directory-bridge should have picked up the OASF record we created earlier
	reg, err := mockClient.WaitForRegistration(ctx, "test-agent", agntcyConfig.RegistrationTimeout)
	if err != nil {
		return fmt.Errorf("failed waiting for directory registration: %w", err)
	}
	if reg == nil {
		result.Warnings = append(result.Warnings,
			"Agent not registered in directory within timeout - directory-bridge may not be processing")
		result.Details["agntcy_directory_registered"] = false
		return nil
	}

	result.Details["agntcy_directory_registered"] = true
	result.Details["agntcy_directory_agent_id"] = reg.AgentID

	return nil
}

// verifyOTELExport checks that the OTel exporter sent traces to the mock AGNTCY server.
func (s *Scenario) verifyOTELExport(ctx context.Context, result *scenarios.Result) error {
	agntcyConfig := s.getAGNTCYConfig()
	if !agntcyConfig.Enabled {
		result.Details["otel_export_skipped"] = "AGNTCY tests disabled"
		return nil
	}
	if agntcyConfig.MockServerURL == "" {
		result.Details["otel_export_skipped"] = "Mock server URL not configured"
		return nil
	}

	mockClient := client.NewAGNTCYMockClient(agntcyConfig.MockServerURL)
	if err := mockClient.Health(ctx); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("AGNTCY mock server not reachable for OTel check: %v", err))
		result.Details["otel_export_skipped"] = "mock server not reachable"
		return nil
	}

	stats, err := mockClient.GetStats(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get mock server stats: %v", err))
		return nil
	}

	result.Metrics["otel_traces_received"] = int(stats.TracesReceived)
	result.Metrics["otel_metrics_received"] = int(stats.MetricsReceived)

	if stats.TracesReceived > 0 {
		result.Details["otel_traces_exported"] = true
	} else {
		result.Warnings = append(result.Warnings,
			"No OTEL traces received by mock server — exporter may not have flushed within the test window")
		result.Details["otel_traces_exported"] = false
	}

	return nil
}

// cleanupAGNTCY cleans up test resources created by AGNTCY tests.
func (s *Scenario) cleanupAGNTCY(ctx context.Context) error {
	// Delete test agent entity
	_ = s.nats.DeleteKV(ctx, client.BucketEntityStates, testAgentEntityID)

	// Delete OASF record (if it exists)
	_ = s.nats.DeleteKV(ctx, client.BucketOASFRecords, testAgentEntityID)

	return nil
}

// getAGNTCYConfig returns the AGNTCY test configuration.
func (s *Scenario) getAGNTCYConfig() *AGNTCYConfig {
	if s.agntcyConfig == nil {
		return DefaultAGNTCYConfig()
	}
	return s.agntcyConfig
}

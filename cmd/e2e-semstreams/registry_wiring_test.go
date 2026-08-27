package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/config"
)

// TestBuildPayloadRegistryRegistersEveryE2EStamp pins the WIRING, not the
// primitive: the e2e binary's registry must hold every key a scenario stamps on
// entity.create, which is only true if buildPayloadRegistry calls
// fixtures.RegisterPayloads (ADR-103, tasks 7.1 (g)).
func TestBuildPayloadRegistryRegistersEveryE2EStamp(t *testing.T) {
	reg, err := buildPayloadRegistry(&config.Config{})
	require.NoError(t, err)
	for _, key := range []string{
		"test.fixture.v1",
		"e2e.probe.v1",
		"e2e.eventtime.v1",
		"e2e.canonical_create_contract.v1",
		"e2e.relationship_contract.v1",
		"research.e2e_search_seed.v1",
	} {
		_, ok := reg.GetRegistration(key)
		require.Truef(t, ok, "the e2e binary's registry does not hold %s", key)
	}
	for _, key := range []string{"graph.hierarchy_container.v1", "lifecycle.harness.v1", "agentic.agent_lesson.v1"} {
		_, ok := reg.GetRegistration(key)
		require.Truef(t, ok, "the e2e binary's registry does not hold the framework type %s", key)
	}
}

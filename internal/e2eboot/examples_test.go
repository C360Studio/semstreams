package e2eboot

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
)

// TestExamplesOptionRegistersEveryE2EStamp pins the WIRING, not the primitive:
// with SEMSTREAMS_E2E_EXAMPLES set, the E2E binary's payload registry must hold
// every key a scenario stamps on entity.create, which is only true if the
// option carries fixtures.RegisterPayloads (ADR-103).
func TestExamplesOptionRegistersEveryE2EStamp(t *testing.T) {
	opts := FromEnv(testCLI, testBuild, env(map[string]string{"SEMSTREAMS_E2E_EXAMPLES": "1"}))
	reg := payloadregistry.New()
	require.NoError(t, payloadbuiltins.Register(reg))
	for _, register := range opts.Payloads {
		require.NoError(t, register(reg))
	}
	for _, key := range []string{
		"test.fixture.v1",
		"e2e.probe.v1",
		"e2e.eventtime.v1",
		"e2e.canonical_create_contract.v1",
		"e2e.relationship_contract.v1",
		"research.e2e_search_seed.v1",
	} {
		_, ok := reg.GetRegistration(key)
		require.Truef(t, ok, "the examples option does not register %s", key)
	}
}

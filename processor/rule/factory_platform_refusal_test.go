package rule

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// TestCreateRuleProcessorRefusesAbsentDeploymentAuthority pins the second of
// the three LOUD paths the migration note promises (review HIGH-3). Without it
// the refusal was documented and unasserted, and deleting it left six suites
// green.
//
// The rule engine mints trigger identities and run-scope state under this pair
// and decides foreign-vs-local by comparing against it, so an absent pair would
// make the engine either mint an invalid identity or judge every firing entity
// local — both silently.
func TestCreateRuleProcessorRefusesAbsentDeploymentAuthority(t *testing.T) {
	rawConfig := json.RawMessage(`{"pack_id":"refusal-pin"}`)

	for _, tc := range []struct {
		name     string
		platform component.PlatformMeta
	}{
		{"both absent", component.PlatformMeta{}},
		{"org absent", component.PlatformMeta{Platform: "dep1"}},
		{"platform absent", component.PlatformMeta{Org: "acme"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			processor, err := CreateRuleProcessor(rawConfig, component.Dependencies{
				NATSClient: &natsclient.Client{},
				Platform:   tc.platform,
			})
			require.Error(t, err, "the rule processor must refuse to construct without the deployment authority")
			assert.Nil(t, processor, "no processor may be returned alongside the refusal")
			assert.Contains(t, err.Error(), "deps.Platform",
				"the refusal must name the dependency an operator has to fix")
		})
	}
}

// TestCreateRuleProcessorAcceptsAConcreteDeploymentAuthority is the negative
// space: the refusal is about the ABSENT pair, not about construction failing
// for an unrelated reason.
func TestCreateRuleProcessorAcceptsAConcreteDeploymentAuthority(t *testing.T) {
	processor, err := CreateRuleProcessor(json.RawMessage(`{"pack_id":"refusal-pin"}`),
		component.Dependencies{
			NATSClient: &natsclient.Client{},
			Platform:   component.PlatformMeta{Org: "acme", Platform: "dep1"},
		})
	require.NoError(t, err)
	require.NotNil(t, processor)

	rp, ok := processor.(*Processor)
	require.True(t, ok)
	assert.Equal(t, "acme", rp.platform.Org)
	assert.Equal(t, "dep1", rp.platform.Platform)
}

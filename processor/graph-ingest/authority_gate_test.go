package graphingest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// Review HIGH-3: the migration note promises three LOUD paths. Deleting all
// three refusals in one compiling mutant left six suites green, which made the
// promise artifact-free. These pin two of them; the third
// (inference.GetHierarchyTriples) lives beside its own code in graph/inference.
//
// A refusal that nothing asserts is a comment.

// TestCreateGraphIngestRefusesAbsentDeploymentAuthority pins the boot-time
// refusal. An absent pair has no honest reading: admitting everything silently
// retires the gate, rejecting everything takes the graph down at the first
// fact. Refusing to construct is the only fail-closed answer, and it must be
// the OBSERVED behaviour, not a documented intention.
func TestCreateGraphIngestRefusesAbsentDeploymentAuthority(t *testing.T) {
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)

	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)

	for _, tc := range []struct {
		name     string
		platform component.PlatformMeta
	}{
		{"both absent", component.PlatformMeta{}},
		{"org absent", component.PlatformMeta{Platform: "dep1"}},
		{"platform absent", component.PlatformMeta{Org: "acme"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comp, err := CreateGraphIngest(configJSON, component.Dependencies{
				NATSClient:      natsClient,
				PayloadRegistry: newTestPayloadRegistry(t),
				Platform:        tc.platform,
			})
			require.Error(t, err, "graph-ingest must refuse to construct without the deployment authority")
			assert.Nil(t, comp, "no component may be returned alongside the refusal")
			assert.Contains(t, err.Error(), "deps.Platform",
				"the refusal must name the dependency an operator has to fix")
		})
	}
}

// TestCreateGraphIngestAcceptsAConcreteDeploymentAuthority is the negative
// space: the refusal above must be about the ABSENT pair, not about
// construction failing for some unrelated reason.
func TestCreateGraphIngestAcceptsAConcreteDeploymentAuthority(t *testing.T) {
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)

	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, testDependencies(t, natsClient))
	require.NoError(t, err)
	require.NotNil(t, comp)

	c := comp.(*Component)
	assert.Equal(t, testDeploymentOrg, c.org)
	assert.Equal(t, testDeploymentPlatform, c.platform)
}

package payloadbuiltins_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/vocabulary"
)

type graphable interface {
	EntityID() string
	Triples() []message.Triple
}

// TestPayloadRegistryIsTheSingleTypeAuthority is the one-table test (ADR-103):
// every framework type born on the mutation lane is registered by the builtin
// set with a factory producing a Graphable payload and a non-empty floor; the
// two types that hold a contract register it with the type; the composition
// root's contract set is exactly the registry's; every floor is empty or valid.
func TestPayloadRegistryIsTheSingleTypeAuthority(t *testing.T) {
	reg := payloadbuiltins.NewTestRegistry(t)

	mutationLaneTypes := []struct {
		key      string
		floor    string
		contract string
	}{
		{"agentic.loop_execution.v1", vocabulary.IndexingProfileControl, "agentic.loop-execution"},
		{"agentic.agent_lesson.v1", vocabulary.IndexingProfileContent, "agentic.lesson-record"},
		{"agentic.ops_diagnosis.v1", vocabulary.IndexingProfileContent, ""},
		{"agentic.model_endpoint.v1", vocabulary.IndexingProfileControl, ""},
		{"agentic.web_observation.v1", vocabulary.IndexingProfileContent, ""},
		{"lifecycle.harness.v1", vocabulary.IndexingProfileControl, ""},
		{"graph.hierarchy_container.v1", vocabulary.IndexingProfileControl, ""},
	}
	for _, tc := range mutationLaneTypes {
		t.Run(tc.key, func(t *testing.T) {
			registration, ok := reg.GetRegistration(tc.key)
			require.Truef(t, ok, "%s is not registered by the builtin set", tc.key)
			assert.Equal(t, tc.floor, registration.IndexingProfile, "registered floor")

			floor, registered := reg.IndexingProfileFor(tc.key)
			assert.True(t, registered)
			assert.Equal(t, tc.floor, floor)

			parts := strings.SplitN(tc.key, ".", 3)
			payload := reg.Create(parts[0], parts[1], parts[2])
			require.NotNil(t, payload, "factory must produce a payload")
			_, isPayload := payload.(message.Payload)
			assert.True(t, isPayload, "factory payload must implement message.Payload")
			_, isGraphable := payload.(graphable)
			assert.True(t, isGraphable, "factory payload must implement EntityID() and Triples()")

			if tc.contract == "" {
				assert.Empty(t, registration.Contracts, "no birth contract is minted here (O-4 = defer)")
				return
			}
			require.Len(t, registration.Contracts, 1)
			assert.Equal(t, tc.contract, registration.Contracts[0].Name)
			assert.Equal(t, tc.key, registration.Contracts[0].MessageType.Key(), "the contract names the registration's key")
		})
	}

	names := make([]string, 0)
	seen := make(map[string]struct{})
	for _, contract := range reg.Contracts() {
		_, duplicate := seen[contract.Name]
		assert.Falsef(t, duplicate, "contract name %s repeats", contract.Name)
		seen[contract.Name] = struct{}{}
		names = append(names, contract.Name)
	}
	sort.Strings(names)
	assert.Equal(t, []string{"agentic.lesson-record", "agentic.loop-execution"}, names,
		"Contracts() is exactly the retired builtinprojection set")

	for key, registration := range reg.List() {
		assert.Truef(t, registration.IndexingProfile == "" || vocabulary.IsValidIndexingProfile(registration.IndexingProfile),
			"%s declares an invalid floor %q", key, registration.IndexingProfile)
	}
}

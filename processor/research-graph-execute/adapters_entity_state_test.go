package researchexecute

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeEntityStateResponseRejectsCompleteCandidatePoison(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidEntityID := "bad"
	poison := graph.EntityState{ID: validID, Triples: []message.Triple{{
		Subject: validID, Predicate: "test.state.target", Object: invalidEntityID, Datatype: message.EntityReferenceDatatype,
	}}}
	data, err := json.Marshal(map[string]any{"entities": []graph.EntityState{{ID: validID}, poison}})
	require.NoError(t, err)

	entities, missing, err := decodeEntityStateResponse(data)
	require.Error(t, err)
	assert.True(t, graph.IsStateContractError(err))
	assert.Nil(t, entities, "the valid prefix of a poisoned batch must not escape")
	assert.Nil(t, missing, "a rejected batch must not report unhydrated IDs either — "+
		"the reply was never validated, so its missing list is not evidence")
}

// TestDecodeEntityStateResponseCarriesMissing pins that the adapter reads the ADR-084
// `missing` report rather than inferring omission from a shorter list. Without it the
// adapter would keep silently evidencing 3 of 40 seeds with no way to tell whether the
// other 37 do not exist or were simply not read (gh#597).
func TestDecodeEntityStateResponseCarriesMissing(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	absentID := "acme.ops.test.system.widget.404"
	data, err := json.Marshal(graph.EntityBatchResponse{
		Entities: []graph.EntityState{{ID: validID}},
		Missing:  []graph.MissingEntity{{ID: absentID, Reason: graph.MissingNotFound}},
	})
	require.NoError(t, err)

	entities, missing, err := decodeEntityStateResponse(data)
	require.NoError(t, err)
	require.Len(t, entities, 1)
	require.Len(t, missing, 1)
	assert.Equal(t, absentID, missing[0].ID)
	assert.Equal(t, graph.MissingNotFound, missing[0].Reason)
}

// entity-id-audit:classify intentional-malformed "bad" line=17 column=21 surface=go-assignment:invalidEntityID entity_id_invalid:arity research evidence aggregate reference poison fixture

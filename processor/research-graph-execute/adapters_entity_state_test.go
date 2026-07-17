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

	entities, err := decodeEntityStateResponse(data)
	require.Error(t, err)
	assert.True(t, graph.IsStateContractError(err))
	assert.Nil(t, entities, "the valid prefix of a poisoned batch must not escape")
}

// entity-id-audit:classify intentional-malformed "bad" line=17 column=21 surface=go-assignment:invalidEntityID entity_id_invalid:arity research evidence aggregate reference poison fixture

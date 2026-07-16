package graphgateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_PrefixRejectsCompleteCandidatePoisonBeforeEmission(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidGatewayEntityID := "bad"
	poisons := []graph.EntityState{
		{ID: invalidGatewayEntityID},
		{ID: validID, Triples: []message.Triple{{Subject: invalidGatewayEntityID, Predicate: "test.state.value"}}},
		{ID: validID, Triples: []message.Triple{{
			Subject: validID, Predicate: "test.state.target", Object: invalidGatewayEntityID, Datatype: message.EntityReferenceDatatype,
		}}},
	}

	for index, poison := range poisons {
		t.Run(fmt.Sprintf("poison-%d", index), func(t *testing.T) {
			response, err := json.Marshal(graph.PrefixQueryResponse{Entities: []graph.EntityState{{ID: validID}, poison}})
			require.NoError(t, err)
			_, validationErr := validateAndUnwrapPrefixResponse(response)
			require.Error(t, validationErr)
			assert.True(t, graph.IsStateContractError(validationErr))
			var classified *errs.ClassifiedError
			require.ErrorAs(t, validationErr, &classified)
			assert.Equal(t, errs.ErrorFatal, classified.Class)
			assert.Equal(t, graph.ErrorCodeGraphStateResetRequired, classified.Code)

			comp := createTestGateway(t)
			beforeErrors := atomic.LoadInt64(&comp.errors)
			recorder := httptest.NewRecorder()
			comp.handleNATSResponse(recorder, "graph.query.prefix", response)

			assert.Equal(t, http.StatusInternalServerError, recorder.Code)
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Contains(t, body, "errors")
			assert.NotContains(t, body, "data", "poison must not be emitted as GraphQL success data")
			assert.Equal(t, beforeErrors+1, atomic.LoadInt64(&comp.errors))
		})
	}
}

// entity-id-audit:classify intentional-malformed "bad" line=22 column=28 surface=go-assignment:invalidGatewayEntityID gateway prefix complete-candidate poison fixture

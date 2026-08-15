package graphgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestGateway_QueryContractClosure_RootInventoryAndRoutes(t *testing.T) {
	comp := createTestGateway(t)
	want := map[string]string{
		"entity":                 "graph.query.entity",
		"entitiesByPrefix":       "graph.query.prefix",
		"entityByAlias":          "graph.query.entityByAlias",
		"relationships":          "graph.query.relationships",
		"entityIdHierarchy":      "graph.query.hierarchyStats",
		"pathSearch":             "graph.query.pathSearch",
		"spatialSearch":          "graph.query.spatial",
		"temporalSearch":         "graph.query.temporal",
		"semanticSearch":         "graph.query.semantic",
		"findSimilar":            "graph.query.similar",
		"localSearch":            "graph.query.localSearch",
		"globalSearch":           "graph.query.globalSearch",
		"graphSummary":           "graph.query.summary",
		"searchGraph":            "graph.query.searchGraph",
		"trajectory":             "agentic.query.trajectory",
		"entitiesByPredicate":    "graph.index.query.predicate",
		"predicates":             "graph.index.query.predicateList",
		"predicateStats":         "graph.index.query.predicateStats",
		"compoundPredicateQuery": "graph.index.query.predicateCompound",
	}

	queryType := findTypeByName(buildIntrospectionSchema(), `__type(name: "Query")`).(map[string]interface{})
	fields := queryType["fields"].([]map[string]interface{})
	require.Len(t, fields, 19)
	got := make(map[string]bool, len(fields))
	graphQueryBacked := 0
	for _, field := range fields {
		name := field["name"].(string)
		got[name] = true
		subject, ok := want[name]
		require.True(t, ok, "unexpected advertised root field %q", name)
		require.Equal(t, subject, comp.mapGraphQLQueryToNATSSubject("query { "+name+" }"),
			"advertised field %q has no matching production route", name)
		require.Equal(t, name, comp.subjectToGraphQLField(subject),
			"advertised field %q has no matching response projection", name)
		if strings.HasPrefix(subject, "graph.query.") {
			graphQueryBacked++
		}
	}
	require.Equal(t, 14, graphQueryBacked)
	for name := range want {
		require.True(t, got[name], "missing admitted root field %q", name)
	}
	require.Nil(t, findTypeByName(buildIntrospectionSchema(), `__type(name: "Capabilities")`))
}

func TestGateway_QueryContractClosure_SemanticSearchHasNoAlias(t *testing.T) {
	comp := createTestGateway(t)
	require.Equal(t, "graph.query.semantic",
		comp.mapGraphQLQueryToNATSSubject(`query { semanticSearch(query: "graph") { id } }`))
	require.Equal(t, "semanticSearch", comp.subjectToGraphQLField("graph.query.semantic"))

	for _, retired := range []string{"similaritySearch", "textSearch", "capabilities"} {
		t.Run(retired, func(t *testing.T) {
			query := "query { " + retired + " }"
			require.Equal(t, "graph.query.unknown", comp.mapGraphQLQueryToNATSSubject(query))
		})
	}
}

func TestGateway_QueryContractClosure_ClassifiedErrorsPreserveAuthority(t *testing.T) {
	t.Run("coded handler error", func(t *testing.T) {
		mock := newMockNATSRequester()
		mock.requestFunc = func(context.Context, string, []byte, time.Duration) ([]byte, error) {
			return nil, &semerrs.ClassifiedError{
				Class:   semerrs.ErrorTransient,
				Message: "community cache unavailable",
				Code:    graph.ErrorCodeIndexNotReady,
				Detail:  map[string]any{"internal": "must not escape"},
			}
		}
		status, graphErr := executeGraphQLForContractClosure(t, mock,
			`query { localSearch(entityId: "acme.ops.test.graph.entity.001") { count } }`)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, "community cache unavailable", graphErr["message"])
		require.Equal(t, map[string]interface{}{
			"class": "transient",
			"code":  graph.ErrorCodeIndexNotReady,
		}, graphErr["extensions"])
	})

	t.Run("uncoded classified error", func(t *testing.T) {
		mock := newMockNATSRequester()
		mock.requestFunc = func(context.Context, string, []byte, time.Duration) ([]byte, error) {
			return nil, &semerrs.ClassifiedError{
				Class:   semerrs.ErrorInvalid,
				Message: "invalid query",
			}
		}
		status, graphErr := executeGraphQLForContractClosure(t, mock,
			`query { entity(id: "acme.ops.test.graph.entity.001") { id } }`)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, map[string]interface{}{"class": "invalid"}, graphErr["extensions"])
	})

	t.Run("plain transport error", func(t *testing.T) {
		mock := newMockNATSRequester()
		mock.requestFunc = func(context.Context, string, []byte, time.Duration) ([]byte, error) {
			return nil, errors.New("no responders")
		}
		status, graphErr := executeGraphQLForContractClosure(t, mock,
			`query { entity(id: "acme.ops.test.graph.entity.001") { id } }`)
		require.Equal(t, http.StatusInternalServerError, status)
		require.Equal(t, "query failed", graphErr["message"])
		require.NotContains(t, graphErr, "extensions")
	})
}

func TestGateway_QueryContractClosure_InvalidPrefixKeepsStatusAndCode(t *testing.T) {
	mock := newMockNATSRequester()
	status, graphErr := executeGraphQLForContractClosure(t, mock,
		`query { entitiesByPrefix(prefix: "acme.*") { entities { id } } }`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, map[string]interface{}{
		"class": "invalid",
		"code":  semtypes.ErrorCodeEntityIDPrefixInvalid,
	}, graphErr["extensions"])
}

func executeGraphQLForContractClosure(
	t *testing.T,
	mock *mockNATSRequester,
	query string,
) (int, map[string]interface{}) {
	t.Helper()
	comp := createTestGatewayWithMock(t, mock)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, comp.Stop(context.Background())) })

	body, err := json.Marshal(map[string]interface{}{"query": query})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	comp.handleGraphQL(w, req)

	var response struct {
		Errors []map[string]interface{} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Errors, 1, "unexpected GraphQL response: %s", w.Body.String())
	return w.Code, response.Errors[0]
}

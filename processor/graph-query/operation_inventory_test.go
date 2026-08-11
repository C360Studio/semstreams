package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

func TestGraphQueryOperationInventoryIsExactAndComplete(t *testing.T) {
	types := map[string][2]string{
		"entity": {"{id:string}", "graph.ExactEntity"}, "entityByAlias": {"{aliasOrID:string}", "graph.ExactEntity"},
		"batch":          {"{ids:[]string}", "graph.EntityBatchResponse"},
		"relationships":  {"{entity_id:string,direction:string}", ""},
		"pathSearch":     {"graphquery.PathSearchRequest", "graphquery.PathSearchResponse"},
		"hierarchyStats": {"{prefix:string}", "{prefix:string,totalEntities:int,children:[]graphquery.HierarchyChild}"},
		"prefix":         {"graph.PrefixQueryRequest", "graph.PrefixQueryResponse"},
		"spatial":        {"{north:float64,south:float64,east:float64,west:float64,limit:int}", "[]graphindexspatial.SpatialResult"},
		"temporal":       {"{startTime:string,endTime:string,limit:int}", "[]graphindextemporal.TemporalResult"},
		"semantic":       {"graphembedding.SearchRequest", "graphembedding.SearchResponse"},
		"similar":        {"graphembedding.SimilarRequest", "graphembedding.SimilarResponse"},
		"globalSearch":   {"graphquery.GlobalSearchRequest", "graphquery.GlobalSearchResponse"},
		"summary":        {"graph.SummaryRequest", "graph.SummaryData"},
		"searchGraph":    {"graphquery.GlobalSearchRequest", "graphquery.GlobalSearchResponse"},
		"byName":         {"{name:string,limit:int}", "graph.NameData"},
		"localSearch":    {"graphquery.LocalSearchRequest", "graphquery.LocalSearchResponse"},
	}
	consumers := map[string][]string{
		"entity": {"graph-gateway", "fusionnats"}, "entityByAlias": {"graph-gateway"},
		"batch":         {"research-graph-execute", "fusionnats"},
		"relationships": {"graph-gateway", "research-graph-execute", "fusionnats"},
		"pathSearch":    {"graph-gateway"}, "hierarchyStats": {"graph-gateway"},
		"prefix": {"graph-gateway", "fusionnats"}, "spatial": {"graph-gateway"},
		"temporal": {"graph-gateway", "research-graph-execute"},
		"semantic": {"graph-gateway", "fusionnats"}, "similar": {"graph-gateway"},
		"globalSearch": {"graph-gateway"}, "summary": {"graph-gateway"},
		"searchGraph": {"graph-gateway", "research-graph-classify", "research-graph-execute"},
		"byName":      {"fusionnats"}, "localSearch": {"graph-gateway"},
	}
	availability := map[string]string{
		"entity": "authority responder required", "entityByAlias": "alias view errors remain classified",
		"batch": "authority responder required", "relationships": "graph-index errors remain classified",
		"pathSearch": "required backing responders remain classified", "hierarchyStats": "authority errors remain classified",
		"prefix": "authority responder required", "spatial": "optional index errors remain classified",
		"temporal": "optional index errors remain classified", "semantic": "optional embedding errors remain classified",
		"similar":      "optional embedding errors remain classified",
		"summary":      "required backing responders remain classified",
		"byName":       "name-index errors remain classified",
		"localSearch":  "transient index_not_ready until community cache usable",
		"globalSearch": "community-only tier returns transient index_not_ready; lower tiers preserve results with community_cache_not_ready degradation",
		"searchGraph":  "semantic fallback preserves its strategy and reports requested unavailable community enrichment",
	}
	want := []struct {
		operation string
		suffix    string
		graphql   string
		envelope  queryEnvelopeShape
	}{
		{"entity", "entity", "entity", queryEnvelopeBare},
		{"entityByAlias", "entityByAlias", "entityByAlias", queryEnvelopeBare},
		{"batch", "batch", "", queryEnvelopeBare},
		{"relationships", "relationships", "relationships", queryEnvelopeBare},
		{"pathSearch", "pathSearch", "pathSearch", queryEnvelopeBare},
		{"hierarchyStats", "hierarchyStats", "entityIdHierarchy", queryEnvelopeBare},
		{"prefix", "prefix", "entitiesByPrefix", queryEnvelopeBare},
		{"spatial", "spatial", "spatialSearch", queryEnvelopeBare},
		{"temporal", "temporal", "temporalSearch", queryEnvelopeBare},
		{"semantic", "semantic", "semanticSearch", queryEnvelopeBare},
		{"similar", "similar", "findSimilar", queryEnvelopeBare},
		{"globalSearch", "globalSearch", "globalSearch", queryEnvelopeBare},
		{"summary", "summary", "graphSummary", queryEnvelopeStandard},
		{"searchGraph", "searchGraph", "searchGraph", queryEnvelopeBare},
		{"byName", "byName", "", queryEnvelopeStandard},
		{"localSearch", "localSearch", "localSearch", queryEnvelopeBare},
	}
	require.Len(t, graphQueryOperations, len(want))
	seenSubjects := make(map[string]struct{}, len(want))
	for index, expected := range want {
		operation := graphQueryOperations[index]
		require.Equal(t, expected.operation, operation.operation)
		require.Equal(t, expected.suffix, operation.suffix)
		require.Equal(t, expected.graphql, operation.graphQLField)
		require.Equal(t, expected.envelope, operation.envelope)
		require.Equal(t, types[operation.operation][0], operation.requestType)
		if operation.operation != "relationships" {
			require.Equal(t, types[operation.operation][1], operation.successType)
		}
		require.Equal(t, consumers[operation.operation], operation.consumers)
		if expectedAvailability, ok := availability[operation.operation]; ok {
			require.Equal(t, expectedAvailability, operation.availability)
		}
		require.NotNil(t, operation.handler)
		require.NotNil(t, operation.handler(&Component{}))

		subject, err := component.ResolveSubject([]component.PortDefinition{{
			Name:   graphQueriesPortName,
			Config: component.NATSRequestPort{Subject: graphQuerySubjectFamily},
		}}, graphQueriesPortName, operation.suffix)
		require.NoError(t, err)
		require.Equal(t, "graph.query."+operation.suffix, subject)
		_, duplicate := seenSubjects[subject]
		require.False(t, duplicate, "duplicate operation subject %s", subject)
		seenSubjects[subject] = struct{}{}
	}
	require.Len(t, seenSubjects, 16)
}

func TestRelationshipInventoryMatchesResponderWire(t *testing.T) {
	operation := graphQueryOperation(t, "relationships")
	require.Equal(t, "[]"+reflect.TypeOf(relationshipWire{}).Name(), operation.successType)

	encoded, err := json.Marshal([]relationshipWire{{
		EdgeType: "test.edge", FromEntityID: "from", ToEntityID: "to",
	}})
	require.NoError(t, err)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(encoded, &rows))
	require.Equal(t, []map[string]any{{
		"edge_type": "test.edge", "from_entity_id": "from", "to_entity_id": "to",
	}}, rows)
	require.NotContains(t, rows[0], "predicate")
}

func TestSearchInventoryRecordsCommunityGenerationOutcomes(t *testing.T) {
	component := newSummaryTestComponent(func(context.Context, string, []byte, time.Duration) ([]byte, error) {
		return nil, errors.New("optional semantic responder unavailable")
	})
	component.communityCache = newCommunityCache(component.logger)

	request := []byte(`{"query":"no current matches"}`)
	for _, test := range []struct {
		operation string
		handler   func(context.Context, []byte) ([]byte, error)
	}{
		{operation: "globalSearch", handler: component.handleGlobalSearch},
		{operation: "searchGraph", handler: component.handleSearchGraph},
	} {
		t.Run(test.operation, func(t *testing.T) {
			_, err := test.handler(context.Background(), request)
			require.Error(t, err)
			var classified *errs.ClassifiedError
			require.ErrorAs(t, err, &classified)
			require.Equal(t, "index_not_ready", classified.Code)
			availability := graphQueryOperation(t, test.operation).availability
			require.NotContains(t, availability, "unmarked empty success")
		})
	}
}

func graphQueryOperation(t *testing.T, name string) queryOperationSpec {
	t.Helper()
	for _, operation := range graphQueryOperations {
		if operation.operation == name {
			return operation
		}
	}
	t.Fatalf("operation %q not found", name)
	return queryOperationSpec{}
}

func TestGraphQueryProviderRejectsDeclarationDrift(t *testing.T) {
	tests := []struct {
		name       string
		definition component.PortDefinition
	}{
		{name: "unknown port", definition: graphQueryDefinition("unknown", graphQuerySubjectFamily, graphQueryInterfaceType, graphQueryInterfaceVersion)},
		{name: "out of family", definition: graphQueryDefinition(graphQueriesPortName, "other.query.*", graphQueryInterfaceType, graphQueryInterfaceVersion)},
		{name: "missing interface", definition: graphQueryDefinition(graphQueriesPortName, graphQuerySubjectFamily, "", "")},
		{name: "wrong interface", definition: graphQueryDefinition(graphQueriesPortName, graphQuerySubjectFamily, "other.query", graphQueryInterfaceVersion)},
		{name: "wrong version", definition: graphQueryDefinition(graphQueriesPortName, graphQuerySubjectFamily, graphQueryInterfaceType, "v2")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			config.Ports.Inputs = []component.PortDefinition{test.definition}
			raw, err := json.Marshal(config)
			require.NoError(t, err)
			_, err = CreateGraphQuery(raw, component.Dependencies{NATSClient: &natsclient.Client{}})
			require.Error(t, err)
		})
	}
}

func graphQueryDefinition(name, subject, interfaceType, version string) component.PortDefinition {
	var contract *component.InterfaceContract
	if interfaceType != "" || version != "" {
		contract = &component.InterfaceContract{Type: interfaceType, Version: version}
	}
	return component.PortDefinition{
		Name: name, Required: true,
		Config: component.NATSRequestPort{Subject: subject, Interface: contract},
	}
}

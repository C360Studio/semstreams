package researchgraph

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/message"
	graphembedding "github.com/c360studio/semstreams/processor/graph-embedding"
	graphquery "github.com/c360studio/semstreams/processor/graph-query"
	"github.com/stretchr/testify/require"
)

func TestResearchEmbeddingSearchResponderReturnsSeededHit(t *testing.T) {
	seedEntityID := researchGraphSeedEntityID("abc123")
	request, err := json.Marshal(graphembedding.SearchRequest{
		Query: researchGraphTopic,
		Limit: 25,
	})
	require.NoError(t, err)

	responseData, err := newResearchEmbeddingSearchHandler(seedEntityID)(context.Background(), request)
	require.NoError(t, err)

	var response graphembedding.SearchResponse
	require.NoError(t, json.Unmarshal(responseData, &response))
	require.Equal(t, researchGraphTopic, response.Query)
	require.Len(t, response.Results, 1)
	require.Equal(t, seedEntityID, response.Results[0].EntityID)
	require.Greater(t, response.Results[0].Similarity, graphquery.MinSemanticRelevance)
}

func TestResearchEmbeddingSearchResponderRejectsUnexpectedTopic(t *testing.T) {
	request, err := json.Marshal(graphembedding.SearchRequest{Query: "some other topic"})
	require.NoError(t, err)

	_, err = newResearchEmbeddingSearchHandler(researchGraphSeedEntityID("abc123"))(
		context.Background(),
		request,
	)
	require.ErrorContains(t, err, "unexpected query")
}

func TestResearchEmbeddingSearchResponderRejectsMalformedRequest(t *testing.T) {
	_, err := newResearchEmbeddingSearchHandler(researchGraphSeedEntityID("abc123"))(
		context.Background(),
		[]byte(`{"query":`),
	)
	require.ErrorContains(t, err, "decode embedding search request")
}

func TestResearchEmbeddingSearchResponderBindsRunScopedSeed(t *testing.T) {
	firstSeedEntityID := researchGraphSeedEntityID("aaa111")
	secondSeedEntityID := researchGraphSeedEntityID("bbb222")
	require.NotEqual(t, firstSeedEntityID, secondSeedEntityID)
	_, err := message.ParseEntityID(firstSeedEntityID)
	require.NoError(t, err)
	_, err = message.ParseEntityID(secondSeedEntityID)
	require.NoError(t, err)

	request, err := json.Marshal(graphembedding.SearchRequest{Query: researchGraphTopic})
	require.NoError(t, err)

	firstResponseData, err := newResearchEmbeddingSearchHandler(firstSeedEntityID)(context.Background(), request)
	require.NoError(t, err)
	secondResponseData, err := newResearchEmbeddingSearchHandler(secondSeedEntityID)(context.Background(), request)
	require.NoError(t, err)

	var firstResponse, secondResponse graphembedding.SearchResponse
	require.NoError(t, json.Unmarshal(firstResponseData, &firstResponse))
	require.NoError(t, json.Unmarshal(secondResponseData, &secondResponse))
	require.Equal(t, firstSeedEntityID, firstResponse.Results[0].EntityID)
	require.Equal(t, secondSeedEntityID, secondResponse.Results[0].EntityID)
}

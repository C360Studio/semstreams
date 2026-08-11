package researchgraph

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/fusion"
	graphembedding "github.com/c360studio/semstreams/processor/graph-embedding"
	graphquery "github.com/c360studio/semstreams/processor/graph-query"
	"github.com/stretchr/testify/require"
)

func TestNewScenarioSelectsExplicitFixtureMode(t *testing.T) {
	direct := NewScenario(nil, DefaultConfig())
	require.Equal(t, "research-graph", direct.Name())
	require.Equal(t, FixtureModeDirect, direct.config.FixtureMode)

	executeConfig := DefaultConfig()
	executeConfig.FixtureMode = FixtureModeExecute
	execute := NewScenario(nil, executeConfig)
	require.Equal(t, "research-graph-execute", execute.Name())
}

func TestValidateExecuteBranchArtifactsAcceptsControlledEvidence(t *testing.T) {
	evidence := fusion.Evidence{
		EntityID: ControlledSeedEntityID,
		Tier:     "0",
		Source:   walkSeedsEntityStateSource,
	}
	execution := research.ExecutionOutput{
		Topic:         researchGraphTopic,
		Action:        research.ActionWalkSeeds,
		Evidence:      []fusion.Evidence{evidence},
		SubQueryCount: 3,
	}
	assessment := research.AssessmentOutput{
		Topic:         researchGraphTopic,
		Sufficient:    true,
		EvidenceCount: 1,
	}
	searchResult := research.SearchResult{
		Synthesis: "Controlled seed evidence supports the answer.",
		Evidence:  []fusion.Evidence{evidence},
		DecompTrace: &research.DecompTrace{
			RouterAction: research.ActionWalkSeeds,
			SeedEntities: []string{"0"},
		},
	}

	require.NoError(t, validateExecuteBranchArtifacts(execution, assessment, searchResult))
}

func TestValidateExecuteBranchArtifactsAcceptsPartialFanoutWithControlledEvidence(t *testing.T) {
	evidence := fusion.Evidence{
		EntityID: ControlledSeedEntityID,
		Tier:     "0",
		Source:   walkSeedsEntityStateSource,
	}
	execution := research.ExecutionOutput{
		Topic:          researchGraphTopic,
		Action:         research.ActionWalkSeeds,
		Evidence:       []fusion.Evidence{evidence},
		SubQueryCount:  3,
		Degraded:       true,
		DegradedReason: "optional predicate walk unavailable",
	}
	assessment := research.AssessmentOutput{
		Topic:          researchGraphTopic,
		Sufficient:     true,
		EvidenceCount:  1,
		Degraded:       true,
		DegradedReason: "optional predicate walk unavailable",
	}
	searchResult := research.SearchResult{
		Synthesis: "Controlled seed evidence supports the answer.",
		Evidence:  []fusion.Evidence{evidence},
		DecompTrace: &research.DecompTrace{
			RouterAction: research.ActionWalkSeeds,
			SeedEntities: []string{"0"},
		},
	}

	require.NoError(t, validateExecuteBranchArtifacts(execution, assessment, searchResult))
}

func TestValidateExecuteBranchArtifactsRejectsUnattributedSynthesisEvidence(t *testing.T) {
	execution := research.ExecutionOutput{
		Topic:         researchGraphTopic,
		Action:        research.ActionWalkSeeds,
		Evidence:      []fusion.Evidence{{EntityID: ControlledSeedEntityID, Tier: "0", Source: walkSeedsEntityStateSource}},
		SubQueryCount: 1,
	}
	assessment := research.AssessmentOutput{
		Topic:         researchGraphTopic,
		Sufficient:    true,
		EvidenceCount: 1,
	}
	searchResult := research.SearchResult{
		Synthesis: "Fabricated evidence should fail attribution.",
		Evidence: []fusion.Evidence{
			{EntityID: ControlledSeedEntityID, Tier: "0", Source: walkSeedsEntityStateSource},
			{EntityID: "c360.rg-e2e.research.seed.document.fabricated", Tier: "0", Source: walkSeedsEntityStateSource},
		},
		DecompTrace: &research.DecompTrace{
			RouterAction: research.ActionWalkSeeds,
			SeedEntities: []string{"0"},
		},
	}

	require.ErrorContains(t,
		validateExecuteBranchArtifacts(execution, assessment, searchResult),
		"not present in execution evidence",
	)
}

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

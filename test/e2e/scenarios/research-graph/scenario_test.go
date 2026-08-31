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

// testControlledSeedEntityID composes the controlled seed under the DECLARED
// stem. The running deployment mints a suffix onto platform.id (ADR-104), so
// the scenario observes its authority in Setup; these unit tests exercise the
// validator, which takes the composed ID as an argument and therefore does not
// care which authority produced it.
func testControlledSeedEntityID() string {
	cfg := DefaultConfig()
	return cfg.PlatformOrg + "." + cfg.PlatformID + "." + ControlledSeedSuffix
}

func TestValidateExecuteBranchArtifactsAcceptsControlledEvidence(t *testing.T) {
	controlled := testControlledSeedEntityID()
	evidence := fusion.Evidence{
		EntityID: controlled,
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
		Synthesis: ControlledSeedSynthesis,
		Evidence:  []fusion.Evidence{evidence},
		DecompTrace: &research.DecompTrace{
			RouterAction: research.ActionWalkSeeds,
			SeedEntities: []string{"0"},
		},
	}

	require.NoError(t, validateExecuteBranchArtifacts(execution, assessment, searchResult, controlled))
}

func TestValidateExecuteBranchArtifactsAcceptsPartialFanoutWithControlledEvidence(t *testing.T) {
	controlled := testControlledSeedEntityID()
	evidence := fusion.Evidence{
		EntityID: controlled,
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
		Synthesis: ControlledSeedSynthesis,
		Evidence:  []fusion.Evidence{evidence},
		DecompTrace: &research.DecompTrace{
			RouterAction: research.ActionWalkSeeds,
			SeedEntities: []string{"0"},
		},
	}

	require.NoError(t, validateExecuteBranchArtifacts(execution, assessment, searchResult, controlled))
}

func TestValidateExecuteBranchArtifactsRejectsUnattributedSynthesisEvidence(t *testing.T) {
	controlled := testControlledSeedEntityID()
	execution := research.ExecutionOutput{
		Topic:         researchGraphTopic,
		Action:        research.ActionWalkSeeds,
		Evidence:      []fusion.Evidence{{EntityID: controlled, Tier: "0", Source: walkSeedsEntityStateSource}},
		SubQueryCount: 1,
	}
	assessment := research.AssessmentOutput{
		Topic:         researchGraphTopic,
		Sufficient:    true,
		EvidenceCount: 1,
	}
	searchResult := research.SearchResult{
		Synthesis: ControlledSeedSynthesis,
		Evidence: []fusion.Evidence{
			{EntityID: controlled, Tier: "0", Source: walkSeedsEntityStateSource},
			{EntityID: "c360.research-graph-e2e.seed.research.document.fabricated", Tier: "0", Source: walkSeedsEntityStateSource},
		},
		DecompTrace: &research.DecompTrace{
			RouterAction: research.ActionWalkSeeds,
			SeedEntities: []string{"0"},
		},
	}

	require.ErrorContains(t,
		validateExecuteBranchArtifacts(execution, assessment, searchResult, controlled),
		"not present in execution evidence",
	)
}

// TestValidateExecuteBranchArtifactsRejectsUnresolvedAuthority pins the
// fail-closed half of the ADR-104 observation: with no observed authority the
// validator refuses instead of comparing against an empty entity ID, which
// every evidence item would fail to match for the wrong reason.
func TestValidateExecuteBranchArtifactsRejectsUnresolvedAuthority(t *testing.T) {
	require.ErrorContains(t,
		validateExecuteBranchArtifacts(
			research.ExecutionOutput{}, research.AssessmentOutput{}, research.SearchResult{}, ""),
		"Setup did not observe the deployment authority",
	)
}

// TestValidateExecuteBranchArtifactsRejectsQuoteBackFallbackSynthesis pins the
// assertion that keeps the execute fixture from passing through
// research-graph-synthesize's degraded path: when the model's evidence_refs
// fail quote-back the component still returns the controlled evidence, but it
// appends a note to the synthesis. Only the verbatim fixture prose passes.
func TestValidateExecuteBranchArtifactsRejectsQuoteBackFallbackSynthesis(t *testing.T) {
	controlled := testControlledSeedEntityID()
	evidence := fusion.Evidence{
		EntityID: controlled,
		Tier:     "0",
		Source:   walkSeedsEntityStateSource,
	}
	execution := research.ExecutionOutput{
		Topic:         researchGraphTopic,
		Action:        research.ActionWalkSeeds,
		Evidence:      []fusion.Evidence{evidence},
		SubQueryCount: 1,
	}
	assessment := research.AssessmentOutput{
		Topic:         researchGraphTopic,
		Sufficient:    true,
		EvidenceCount: 1,
	}
	searchResult := research.SearchResult{
		Synthesis: ControlledSeedSynthesis +
			"\n\n[note: synthesizer emitted no quote-back refs; chain echoed top evidence]",
		Evidence: []fusion.Evidence{evidence},
		DecompTrace: &research.DecompTrace{
			RouterAction: research.ActionWalkSeeds,
			SeedEntities: []string{"0"},
		},
	}

	require.ErrorContains(t,
		validateExecuteBranchArtifacts(execution, assessment, searchResult, controlled),
		"quote-back failed",
	)
}

// testSeedEntityID composes a run-scoped seed under the SAME deployment
// authority the scenario's DefaultConfig declares. Since ADR-102 d5 the graph
// accepts no other pair, so a fixture that hardcoded one would drift silently
// from the config the tier actually boots.
func testSeedEntityID(runToken string) string {
	cfg := DefaultConfig()
	return researchGraphSeedEntityID(cfg.PlatformOrg, cfg.PlatformID, runToken)
}

func TestResearchEmbeddingSearchResponderReturnsSeededHit(t *testing.T) {
	seedEntityID := testSeedEntityID("abc123")
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

	_, err = newResearchEmbeddingSearchHandler(testSeedEntityID("abc123"))(
		context.Background(),
		request,
	)
	require.ErrorContains(t, err, "unexpected query")
}

func TestResearchEmbeddingSearchResponderRejectsMalformedRequest(t *testing.T) {
	_, err := newResearchEmbeddingSearchHandler(testSeedEntityID("abc123"))(
		context.Background(),
		[]byte(`{"query":`),
	)
	require.ErrorContains(t, err, "decode embedding search request")
}

func TestResearchEmbeddingSearchResponderBindsRunScopedSeed(t *testing.T) {
	firstSeedEntityID := testSeedEntityID("aaa111")
	secondSeedEntityID := testSeedEntityID("bbb222")
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

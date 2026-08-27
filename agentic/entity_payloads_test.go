package agentic_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// graphable is the shape graph-ingest extracts from a decoded payload.
type graphable interface {
	EntityID() string
	Triples() []message.Triple
}

const (
	payloadTestOrg       = "acme"
	payloadTestPlatform  = "ops"
	payloadTestLoopID    = "acme.ops.agent.agentic-loop.execution.loop-ops-abc"
	payloadTestEvidence  = "acme.ops.agent.agentic-loop.execution.loop-1"
	payloadTestEvidence2 = "acme.ops.agent.agentic-loop.execution.loop-2"
)

var payloadTestTime = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

// decodeThroughProductionDecoder marshals the payload through the real publish
// wrap and decodes it through message.NewDecoder on the agentic registry.
func decodeThroughProductionDecoder(t *testing.T, payload message.Payload) message.Payload {
	t.Helper()
	base := message.NewBaseMessage(payload.Schema(), payload, "test")
	data, err := json.Marshal(base)
	require.NoError(t, err)
	decoded, err := message.NewDecoder(payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)).Decode(data)
	require.NoError(t, err)
	return decoded.Payload()
}

func predicateSet(triples []message.Triple) map[string]struct{} {
	set := make(map[string]struct{}, len(triples))
	for _, triple := range triples {
		set[triple.Predicate] = struct{}{}
	}
	return set
}

func fullLoopExecution() *agentic.LoopExecutionEntity {
	return &agentic.LoopExecutionEntity{
		Org: payloadTestOrg, Platform: payloadTestPlatform, LoopID: "loop-1",
		Task: &agentic.TaskMessage{
			LoopID: "loop-1", TaskID: "task-1", Role: "researcher", Model: "mock", Prompt: "do the thing",
			WorkflowSlug: "wf", WorkflowStep: "design", UserID: "user-1",
			ParentLoopID: "loop-0", RunID: "run-1", InReplyTo: "loop-9",
		},
	}
}

func fullLesson() *agentic.AgentLessonEntity {
	return &agentic.AgentLessonEntity{
		Org: payloadTestOrg, Platform: payloadTestPlatform, ID: "11111111-1111-5111-8111-111111111111",
		Category: "retention-policy", Polarity: "avoid", Severity: "warning", Status: "proposed",
		CreatedAt: payloadTestTime,
		Summary:   "cap retention sweeps", Detail: "the detail", InjectionForm: "Cap sweeps.",
		Evidence:     []string{payloadTestEvidence, payloadTestEvidence2},
		AppliesTo:    []string{"tag:go", "id:acme.ops.agent"},
		ObservedRole: "ops", ExecutedBy: payloadTestLoopID,
	}
}

func fullDiagnosis() *agentic.OpsDiagnosisEntity {
	return &agentic.OpsDiagnosisEntity{
		Org: payloadTestOrg, Platform: payloadTestPlatform, ID: "550e8400-e29b-41d4-a716-446655440000",
		Finding: "the finding", Recommendation: "the recommendation", Confidence: 0.85,
		Evidence:     []string{payloadTestEvidence, payloadTestEvidence2},
		ObservedRole: "ops", Severity: "warn", ExecutedBy: payloadTestLoopID,
	}
}

func fullModelEndpoint() *agentic.ModelEndpointEntity {
	return &agentic.ModelEndpointEntity{
		Org: payloadTestOrg, Platform: payloadTestPlatform, Name: "gpt4o",
		Provider: "openai", Model: "gpt-4o", URL: "https://api.openai.com/v1",
		SupportsTools: true, MaxTokens: 128000,
		InputPricePer1MTokens: 5, OutputPricePer1MTokens: 15, RequestsPerMinute: 60,
	}
}

func fullWebObservation(tool agentic.WebObservationTool) *agentic.WebObservationEntity {
	return &agentic.WebObservationEntity{
		Org: payloadTestOrg, Platform: payloadTestPlatform,
		CanonicalURL: "https://example.com/path?q=1", Tool: tool, LoopEntityID: payloadTestLoopID,
		FetchedAt: "2026-08-26T12:00:00.000000001Z", ContentType: "text/html", StatusCode: 200,
		Text: "body text", Truncated: true,
		Title: "Example", Snippet: "a snippet", SourceQuery: "example query",
		ObservedAt: "2026-08-26T12:00:00.000000002Z",
	}
}

func assertRoundTrip[T any](t *testing.T, original *T, decoded message.Payload) {
	t.Helper()
	got, ok := any(decoded).(*T)
	require.Truef(t, ok, "decoded payload must be %T, got %T", original, decoded)
	require.Equal(t, *original, *got, "fields must survive the production decoder")
	og, ok := any(original).(graphable)
	require.True(t, ok)
	dg, ok := any(got).(graphable)
	require.True(t, ok)
	require.NotEmpty(t, og.EntityID())
	assert.Equal(t, og.EntityID(), dg.EntityID())
	assert.Equal(t, predicateSet(og.Triples()), predicateSet(dg.Triples()))
}

func TestLoopExecutionEntity_RoundTrip(t *testing.T) {
	entity := fullLoopExecution()
	assertRoundTrip(t, entity, decodeThroughProductionDecoder(t, entity))
	assert.Equal(t, "agentic.loop_execution.v1", entity.Schema().Key())
}

func TestAgentLessonEntity_RoundTrip(t *testing.T) {
	entity := fullLesson()
	assertRoundTrip(t, entity, decodeThroughProductionDecoder(t, entity))
	assert.Equal(t, "agentic.agent_lesson.v1", entity.Schema().Key())
}

func TestOpsDiagnosisEntity_RoundTrip(t *testing.T) {
	entity := fullDiagnosis()
	assertRoundTrip(t, entity, decodeThroughProductionDecoder(t, entity))
	assert.Equal(t, "agentic.ops_diagnosis.v1", entity.Schema().Key())
}

func TestModelEndpointEntity_RoundTrip(t *testing.T) {
	entity := fullModelEndpoint()
	assertRoundTrip(t, entity, decodeThroughProductionDecoder(t, entity))
	assert.Equal(t, "agentic.model_endpoint.v1", entity.Schema().Key())
}

func TestWebObservationEntity_RoundTrip(t *testing.T) {
	for _, tool := range []agentic.WebObservationTool{agentic.WebObservationToolHTTPRequest, agentic.WebObservationToolWebSearch} {
		t.Run(string(tool), func(t *testing.T) {
			entity := fullWebObservation(tool)
			assertRoundTrip(t, entity, decodeThroughProductionDecoder(t, entity))
			assert.Equal(t, "agentic.web_observation.v1", entity.Schema().Key())
		})
	}
	t.Run("unknown tool fails validation", func(t *testing.T) {
		entity := fullWebObservation("ftp")
		require.Error(t, entity.Validate())
	})
}

// TestRegisteredContractMatchesTriples (F-1): for every contract registered
// with an agentic type, birth(C) ⊆ predicates(Triples() of a fully populated
// entity) ⊆ birth(C) ∪ groups(C). A birth predicate removed from the builder
// but not from the contract fails naming the predicate.
func TestRegisteredContractMatchesTriples(t *testing.T) {
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	contracts := reg.Contracts()
	require.NotEmpty(t, contracts, "the agentic registry must carry the framework contracts")

	entities := map[string]graphable{
		agentic.LoopExecutionMessageType().Key(): fullLoopExecution(),
		agentic.AgentLessonMessageType().Key():   fullLesson(),
	}
	for _, contract := range contracts {
		t.Run(contract.Name, func(t *testing.T) {
			entity, ok := entities[contract.MessageType.Key()]
			require.Truef(t, ok, "no fully populated entity for registered type %s", contract.MessageType.Key())
			emitted := predicateSet(entity.Triples())

			allowed := make(map[string]struct{})
			for _, birth := range contract.BirthPredicates {
				allowed[birth] = struct{}{}
				_, found := emitted[birth]
				assert.Truef(t, found, "contract %s birth predicate %s is not emitted by Triples()", contract.Name, birth)
			}
			for _, group := range contract.Groups {
				for _, predicate := range group.Predicates {
					allowed[predicate] = struct{}{}
				}
			}
			for predicate := range emitted {
				_, found := allowed[predicate]
				assert.Truef(t, found, "Triples() emits %s which contract %s does not declare", predicate, contract.Name)
			}
		})
	}
}

// goldenTriple is one row of a builder's former output: everything the
// byte-identity rule pins except Timestamp.
type goldenTriple struct {
	Predicate  string
	Object     any
	Source     string
	Confidence float64
}

func assertGoldenTriples(t *testing.T, entityID string, got []message.Triple, want []goldenTriple) {
	t.Helper()
	require.Len(t, got, len(want), "triple count")
	for i := range want {
		assert.Equal(t, want[i].Predicate, got[i].Predicate, "triple %d predicate", i)
		assert.Equal(t, want[i].Object, got[i].Object, "triple %d object (type and value)", i)
		assert.Equal(t, want[i].Source, got[i].Source, "triple %d source", i)
		assert.Equal(t, want[i].Confidence, got[i].Confidence, "triple %d confidence", i)
		assert.Equal(t, entityID, got[i].Subject, "triple %d subject", i)
		assert.False(t, got[i].Timestamp.IsZero(), "triple %d timestamp is stamped", i)
	}
}

// TestModelEndpointEntityMatchesBuilder: golden captured from
// processor/agentic-loop/graph_writer.go buildModelEndpointTriples at
// 08660fc5 for a fully populated endpoint and for one with every optional
// field zero (the five zero-gates; bool/int/float64 objects).
func TestModelEndpointEntityMatchesBuilder(t *testing.T) {
	const entityID = "acme.ops.agent.model-registry.endpoint.gpt4o"
	const source = "agentic-loop"

	full := fullModelEndpoint()
	require.Equal(t, entityID, full.EntityID())
	assertGoldenTriples(t, entityID, full.Triples(), []goldenTriple{
		{agvocab.ModelProvider, "openai", source, 1},
		{agvocab.ModelName, "gpt-4o", source, 1},
		{agvocab.ModelSupportsTools, true, source, 1},
		{agvocab.ModelMaxTokens, 128000, source, 1},
		{agvocab.ModelInputPrice, 5.0, source, 1},
		{agvocab.ModelOutputPrice, 15.0, source, 1},
		{agvocab.ModelEndpointURL, "https://api.openai.com/v1", source, 1},
		{agvocab.ModelRateLimit, 60, source, 1},
	})

	zero := &agentic.ModelEndpointEntity{
		Org: payloadTestOrg, Platform: payloadTestPlatform, Name: "gpt4o",
		Provider: "ollama", Model: "llama3.2",
	}
	assertGoldenTriples(t, entityID, zero.Triples(), []goldenTriple{
		{agvocab.ModelProvider, "ollama", source, 1},
		{agvocab.ModelName, "llama3.2", source, 1},
		{agvocab.ModelSupportsTools, false, source, 1},
	})
}

// TestOpsDiagnosisEntityMatchesBuilder: golden captured from
// processor/agentic-tools/emit_diagnosis.go buildEmitDiagnosisTriples at
// 08660fc5: the full set, the %g confidence object, and the entity's
// Confidence on every triple.
func TestOpsDiagnosisEntityMatchesBuilder(t *testing.T) {
	const entityID = "acme.ops.ops.diagnosis.finding.550e8400-e29b-41d4-a716-446655440000"
	const source = "ops-emit-diagnosis"

	full := fullDiagnosis()
	require.Equal(t, entityID, full.EntityID())
	assertGoldenTriples(t, entityID, full.Triples(), []goldenTriple{
		{agvocab.OpsDiagnosisFinding, "the finding", source, 0.85},
		{agvocab.OpsDiagnosisRecommendation, "the recommendation", source, 0.85},
		{agvocab.OpsDiagnosisConfidence, "0.85", source, 0.85},
		{agvocab.OpsDiagnosisEvidence, payloadTestEvidence, source, 0.85},
		{agvocab.OpsDiagnosisEvidence, payloadTestEvidence2, source, 0.85},
		{agvocab.OpsDiagnosisObservedRole, "ops", source, 0.85},
		{agvocab.OpsDiagnosisSeverity, "warn", source, 0.85},
		{agvocab.ActionExecutedBy, payloadTestLoopID, source, 0.85},
	})

	zero := &agentic.OpsDiagnosisEntity{
		Org: payloadTestOrg, Platform: payloadTestPlatform, ID: "550e8400-e29b-41d4-a716-446655440000",
		Finding: "f", Recommendation: "r", Confidence: 1,
		Evidence: []string{payloadTestEvidence}, Severity: "info", ExecutedBy: payloadTestLoopID,
	}
	assertGoldenTriples(t, entityID, zero.Triples(), []goldenTriple{
		{agvocab.OpsDiagnosisFinding, "f", source, 1},
		{agvocab.OpsDiagnosisRecommendation, "r", source, 1},
		{agvocab.OpsDiagnosisConfidence, "1", source, 1},
		{agvocab.OpsDiagnosisEvidence, payloadTestEvidence, source, 1},
		{agvocab.OpsDiagnosisSeverity, "info", source, 1},
		{agvocab.ActionExecutedBy, payloadTestLoopID, source, 1},
	})
}

// TestWebObservationEntityMatchesToolBuilders (F-2): per tool, the triple set
// equals the former inline builder — httprequest.go:257-266 (source
// agent-http-request, seven unconditional predicates) and websearch.go:255-262
// (source agent-web-search, six) at 08660fc5 — zero values included.
func TestWebObservationEntityMatchesToolBuilders(t *testing.T) {
	entityID, canonical, err := agentic.TryWebObservationEntityID(payloadTestOrg, payloadTestPlatform, "https://example.com/path?q=1")
	require.NoError(t, err)

	t.Run("http_request", func(t *testing.T) {
		const source = "agent-http-request"
		full := fullWebObservation(agentic.WebObservationToolHTTPRequest)
		require.Equal(t, canonical, full.CanonicalURL)
		require.Equal(t, entityID, full.EntityID())
		assertGoldenTriples(t, entityID, full.Triples(), []goldenTriple{
			{agvocab.WebURL, canonical, source, 1},
			{agvocab.WebFetchedAt, "2026-08-26T12:00:00.000000001Z", source, 1},
			{agvocab.WebFetchedBy, payloadTestLoopID, source, 1},
			{agvocab.WebContentType, "text/html", source, 1},
			{agvocab.WebStatusCode, 200, source, 1},
			{agvocab.WebText, "body text", source, 1},
			{agvocab.WebTruncated, true, source, 1},
		})

		zero := &agentic.WebObservationEntity{
			Org: payloadTestOrg, Platform: payloadTestPlatform, CanonicalURL: canonical,
			Tool: agentic.WebObservationToolHTTPRequest, LoopEntityID: payloadTestLoopID,
		}
		assertGoldenTriples(t, entityID, zero.Triples(), []goldenTriple{
			{agvocab.WebURL, canonical, source, 1},
			{agvocab.WebFetchedAt, "", source, 1},
			{agvocab.WebFetchedBy, payloadTestLoopID, source, 1},
			{agvocab.WebContentType, "", source, 1},
			{agvocab.WebStatusCode, 0, source, 1},
			{agvocab.WebText, "", source, 1},
			{agvocab.WebTruncated, false, source, 1},
		})
	})

	t.Run("web_search", func(t *testing.T) {
		const source = "agent-web-search"
		full := fullWebObservation(agentic.WebObservationToolWebSearch)
		require.Equal(t, entityID, full.EntityID())
		assertGoldenTriples(t, entityID, full.Triples(), []goldenTriple{
			{agvocab.WebURL, canonical, source, 1},
			{agvocab.WebTitle, "Example", source, 1},
			{agvocab.WebSnippet, "a snippet", source, 1},
			{agvocab.WebSourceQuery, "example query", source, 1},
			{agvocab.WebObservedAt, "2026-08-26T12:00:00.000000002Z", source, 1},
			{agvocab.WebObservedBy, payloadTestLoopID, source, 1},
		})

		zero := &agentic.WebObservationEntity{
			Org: payloadTestOrg, Platform: payloadTestPlatform, CanonicalURL: canonical,
			Tool: agentic.WebObservationToolWebSearch, LoopEntityID: payloadTestLoopID,
		}
		assertGoldenTriples(t, entityID, zero.Triples(), []goldenTriple{
			{agvocab.WebURL, canonical, source, 1},
			{agvocab.WebTitle, "", source, 1},
			{agvocab.WebSnippet, "", source, 1},
			{agvocab.WebSourceQuery, "", source, 1},
			{agvocab.WebObservedAt, "", source, 1},
			{agvocab.WebObservedBy, payloadTestLoopID, source, 1},
		})
	})

	t.Run("an invalid identity yields an empty entity ID, never a panic", func(t *testing.T) {
		bad := &agentic.WebObservationEntity{Org: "acme.corp", Platform: payloadTestPlatform, CanonicalURL: canonical, Tool: agentic.WebObservationToolWebSearch}
		require.NotPanics(t, func() { assert.Empty(t, bad.EntityID()) })
		require.Error(t, bad.Validate())
	})
}

// TestWebObservationEntityIDAgreesWithRawURLIdentity (review MEDIUM-5): the
// executors compute the entity ID from the RAW URL and the entity's Triples()
// compute their Subject from the CANONICAL URL through the same builder; they
// agree only if canonicalisation is idempotent. Pinned on URLs with a
// non-trivial path, percent-encoding, a default port, a fragment, userinfo,
// and a query.
func TestWebObservationEntityIDAgreesWithRawURLIdentity(t *testing.T) {
	for _, raw := range []string{
		"HTTPS://User:Secret@Example.COM:443/Docs/A%20B/c%2Fd/?q=x%26y&z=1#frag",
		"http://example.com:80/",
		"https://example.com/path/with%2Fencoded/segment?a=%20b#section-2",
		"https://example.com/plain",
	} {
		t.Run(raw, func(t *testing.T) {
			rawID, canonical, err := agentic.TryWebObservationEntityID(payloadTestOrg, payloadTestPlatform, raw)
			require.NoError(t, err)
			entity := &agentic.WebObservationEntity{
				Org: payloadTestOrg, Platform: payloadTestPlatform, CanonicalURL: canonical,
				Tool: agentic.WebObservationToolWebSearch, LoopEntityID: payloadTestLoopID,
			}
			require.Equal(t, rawID, entity.EntityID(), "the entity's ID over the canonical URL must equal the executor's ID over the raw URL")
			for i, triple := range entity.Triples() {
				assert.Equal(t, rawID, triple.Subject, "triple %d subject", i)
			}
			// Canonicalisation is a fixed point: the canonical form canonicalises to itself.
			_, again, err := agentic.TryWebObservationEntityID(payloadTestOrg, payloadTestPlatform, canonical)
			require.NoError(t, err)
			assert.Equal(t, canonical, again)
		})
	}
}

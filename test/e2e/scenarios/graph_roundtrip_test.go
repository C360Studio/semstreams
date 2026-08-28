package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/vocabulary"
)

func TestTieredVariantsIncludeGraphRoundTripStageExactlyOnce(t *testing.T) {
	t.Parallel()

	for _, variant := range []string{"structural", "statistical", "semantic"} {
		variant := variant
		t.Run(variant, func(t *testing.T) {
			t.Parallel()

			scenario := &TieredScenario{}
			count := 0
			for _, stage := range scenario.getStagesForVariant(variant) {
				if stage.name == "graph-roundtrip" {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("graph-roundtrip stage count = %d, want 1", count)
			}
		})
	}
}

func TestStructuralVariantIncludesCanonicalMutationContractStages(t *testing.T) {
	t.Parallel()

	want := map[string]int{
		"validate-canonical-create-no-hierarchy": 1,
		"validate-relationship-no-stub":          1,
	}
	for _, stage := range (&TieredScenario{}).getStagesForVariant("structural") {
		if _, ok := want[stage.name]; ok {
			want[stage.name]--
		}
	}
	for name, remaining := range want {
		if remaining != 0 {
			t.Fatalf("structural stage %q count mismatch: remaining=%d", name, remaining)
		}
	}

	for _, variant := range []string{"statistical", "semantic"} {
		for _, stage := range (&TieredScenario{}).getStagesForVariant(variant) {
			if stage.name == "validate-canonical-create-no-hierarchy" || stage.name == "validate-relationship-no-stub" {
				t.Fatalf("%s unexpectedly includes structural-only stage %q", variant, stage.name)
			}
		}
	}
}

func TestValidateTitleReplacementRejectsAppendInsteadOfReplace(t *testing.T) {
	t.Parallel()

	entity := &graph.EntityState{Triples: []message.Triple{
		{Predicate: vocabulary.DCTermsTitle, Object: "before"},
		{Predicate: vocabulary.DCTermsTitle, Object: "after"},
	}}
	if err := validateTitleReplacement(entity, "before", "after"); err == nil {
		t.Fatal("validateTitleReplacement accepted an appended title")
	}

	entity.Triples = entity.Triples[1:]
	if err := validateTitleReplacement(entity, "before", "after"); err != nil {
		t.Fatalf("validateTitleReplacement rejected exact replacement: %v", err)
	}
}

func TestGraphRoundTripUsesExactEntityGraphQLShape(t *testing.T) {
	t.Parallel()

	if !strings.Contains(graphQLExactEntityQuery, "entity { id triples") {
		t.Fatalf("query does not select the nested authority entity: %s", graphQLExactEntityQuery)
	}
	if !strings.Contains(graphQLExactEntityQuery, "kvRevision") {
		t.Fatalf("query does not select authority revision evidence: %s", graphQLExactEntityQuery)
	}
}

func TestGraphRoundTripFixtureIsStructurallyValid(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	entity := newGraphRoundTripEntity("c360.e2e.graph.core.canary.fixture", updatedAt)
	entity.Triples = []message.Triple{{
		Subject: entity.ID, Predicate: vocabulary.DCTermsTitle, Object: "fixture",
	}}
	if err := graph.ValidateDecodedEntityState(entity); err != nil {
		t.Fatalf("graph-roundtrip fixture violates graph contract: %v", err)
	}
	if entity.Version == 0 {
		t.Fatal("graph-roundtrip fixture has non-production version 0")
	}
	if entity.UpdatedAt.IsZero() {
		t.Fatal("graph-roundtrip fixture has zero updated_at")
	}
}

func TestResponseErrorPreservesAllGraphQLErrors(t *testing.T) {
	t.Parallel()

	err := responseError([]graphQLError{{Message: "index not ready"}, {Message: "watermark missing"}})
	if err == nil || err.Error() != "GraphQL errors: index not ready; watermark missing" {
		t.Fatalf("responseError = %v", err)
	}
}

func TestQueryGraphQLEntityConsumesExactEntityResult(t *testing.T) {
	t.Parallel()

	const entityID = "c360.e2e.graph.core.canary.fixture"
	httpClient := &http.Client{Transport: graphRoundTripTransport(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		if !strings.Contains(request.Query, "entity { id triples") ||
			!strings.Contains(request.Query, "kvRevision") {
			t.Errorf("query does not select ExactEntity fields: %s", request.Query)
		}
		body := `{"data":{"entity":{"entity":{"id":"` + entityID +
			`","triples":[{"subject":"` + entityID +
			`","predicate":"dc.terms.title","object":"after"}]},"kvRevision":2}}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	probe := &GraphRoundTripProbe{
		graphqlURL: "http://graphql.test/query",
		httpClient: httpClient,
	}
	entity, err := probe.queryGraphQLEntity(context.Background(), entityID)
	if err != nil {
		t.Fatalf("queryGraphQLEntity: %v", err)
	}
	if err := validateTitleReplacement(entity, "before", "after"); err != nil {
		t.Fatalf("exact GraphQL entity did not reach the validator: %v", err)
	}
}

type graphRoundTripTransport func(*http.Request) (*http.Response, error)

func (transport graphRoundTripTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestValidateMutationTraceEntries(t *testing.T) {
	t.Parallel()

	expected, entries := mutationTraceFixture()
	wire, err := json.Marshal(client.TraceResponse{Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	var response client.TraceResponse
	if err := json.Unmarshal(wire, &response); err != nil {
		t.Fatalf("unmarshal Message Logger response shape: %v", err)
	}
	if matched, err := validateMutationTraceEntries(response.Entries, expected); err != nil || len(matched) != 2 {
		t.Fatalf("valid Message Logger trace matched=%d err=%v", len(matched), err)
	}

	tests := []struct{ change, want string }{
		{"subject-prefix", mutationCreateSubject},
		{"entity", "payload entity_id"},
		{"request", "payload request_id"},
		{"payload-trace", "payload trace_id"},
		{"entry-trace", "entry trace_id"},
		{"empty-span", "span_id is empty"},
		{"reused-span", "reused span ID"},
	}
	for _, test := range tests {
		t.Run(test.change, func(t *testing.T) {
			expected, entries := mutationTraceFixture()
			switch test.change {
			case "subject-prefix":
				entries[0].Subject += ".extra"
			case "entity":
				entries[0].RawData = bytes.ReplaceAll(entries[0].RawData, []byte(traceEntityID), []byte(traceEntityID+".wrong"))
			case "request":
				entries[1].RawData = bytes.ReplaceAll(entries[1].RawData, []byte("replace-request"), []byte("wrong-request"))
			case "payload-trace":
				entries[0].RawData = bytes.ReplaceAll(entries[0].RawData, []byte(traceFixtureID), []byte(wrongTraceID))
			case "entry-trace":
				entries[1].TraceID = wrongTraceID
			case "empty-span":
				entries[0].SpanID = ""
				expected[mutationCreateSubject] = mutationTraceExpectation{
					EntityID: traceEntityID, RequestID: "create-request", TraceID: traceFixtureID,
				}
			case "reused-span":
				entries[1].SpanID = entries[0].SpanID
				reconcile := expected[mutationReconcileSubject]
				reconcile.SpanID = entries[0].SpanID
				expected[mutationReconcileSubject] = reconcile
			}
			_, err := validateMutationTraceEntries(entries, expected)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

const (
	traceFixtureID = "0123456789abcdef0123456789abcdef"
	wrongTraceID   = "ffffffffffffffffffffffffffffffff"
	traceEntityID  = "c360.e2e.graph.core.canary.fixture"
)

func mutationTraceFixture() (map[string]mutationTraceExpectation, []client.MessageEntry) {
	expected := map[string]mutationTraceExpectation{
		mutationCreateSubject: {
			EntityID: traceEntityID, RequestID: "create-request", TraceID: traceFixtureID, SpanID: "1111111111111111",
		},
		mutationReconcileSubject: {
			EntityID: traceEntityID, RequestID: "replace-request", TraceID: traceFixtureID, SpanID: "2222222222222222",
		},
	}
	entries := []client.MessageEntry{
		{Subject: mutationCreateSubject, TraceID: traceFixtureID, SpanID: "1111111111111111",
			RawData: json.RawMessage(`{"entity":{"id":"c360.e2e.graph.core.canary.fixture"},"trace_id":"0123456789abcdef0123456789abcdef","request_id":"create-request"}`)},
		{Subject: mutationReconcileSubject, TraceID: traceFixtureID, SpanID: "2222222222222222",
			RawData: json.RawMessage(`{"entity_id":"c360.e2e.graph.core.canary.fixture","trace_id":"0123456789abcdef0123456789abcdef","request_id":"replace-request"}`)},
	}
	return expected, entries
}

package scenarios

import (
	"bytes"
	"encoding/json"
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

func TestGraphRoundTripFixtureIsStructurallyValid(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	entity := newGraphRoundTripEntity("c360.e2e.core.graph.canary.fixture", updatedAt)
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
				replace := expected[mutationReplaceSubject]
				replace.SpanID = entries[0].SpanID
				expected[mutationReplaceSubject] = replace
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
	traceEntityID  = "c360.e2e.core.graph.canary.fixture"
)

func mutationTraceFixture() (map[string]mutationTraceExpectation, []client.MessageEntry) {
	expected := map[string]mutationTraceExpectation{
		mutationCreateSubject: {
			EntityID: traceEntityID, RequestID: "create-request", TraceID: traceFixtureID, SpanID: "1111111111111111",
		},
		mutationReplaceSubject: {
			EntityID: traceEntityID, RequestID: "replace-request", TraceID: traceFixtureID, SpanID: "2222222222222222",
		},
	}
	entries := []client.MessageEntry{
		{Subject: mutationCreateSubject, TraceID: traceFixtureID, SpanID: "1111111111111111",
			RawData: json.RawMessage(`{"entity":{"id":"c360.e2e.core.graph.canary.fixture"},"trace_id":"0123456789abcdef0123456789abcdef","request_id":"create-request"}`)},
		{Subject: mutationReplaceSubject, TraceID: traceFixtureID, SpanID: "2222222222222222",
			RawData: json.RawMessage(`{"entity":{"id":"c360.e2e.core.graph.canary.fixture"},"trace_id":"0123456789abcdef0123456789abcdef","request_id":"replace-request"}`)},
	}
	return expected, entries
}

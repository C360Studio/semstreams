package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTime is a stable timestamp for triple fixtures.
var fixedTime = time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

// withFixedTime supplies only the stable timestamp; every semantic position
// remains explicit at the fixture call site.
func withFixedTime(triple message.Triple) message.Triple {
	triple.Timestamp = fixedTime
	return triple
}

// ---- parseTripleQueryParams ----

func TestParseTripleQueryParams_Defaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples", nil)
	p, errMsg := parseTripleQueryParams(r)

	require.Empty(t, errMsg)
	assert.Equal(t, "", p.subject)
	assert.Equal(t, "", p.predicate)
	assert.Equal(t, "", p.object)
	assert.Equal(t, graphTriplesDefaultLimit, p.limit)
}

func TestParseTripleQueryParams_AllParams(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?subject=acme.ops.test.service.entity.1&predicate=test.state.value&object=o&limit=50", nil)
	p, errMsg := parseTripleQueryParams(r)

	require.Empty(t, errMsg)
	assert.Equal(t, "acme.ops.test.service.entity.1", p.subject)
	assert.Equal(t, "test.state.value", p.predicate)
	assert.Equal(t, "o", p.object)
	assert.Equal(t, 50, p.limit)
}

func TestParseTripleQueryParams_LimitClamped(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=9999", nil)
	p, errMsg := parseTripleQueryParams(r)

	require.Empty(t, errMsg)
	assert.Equal(t, graphTriplesMaxLimit, p.limit)
}

func TestParseTripleQueryParams_LimitZeroUsesDefault(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=0", nil)
	p, errMsg := parseTripleQueryParams(r)

	require.Empty(t, errMsg)
	assert.Equal(t, graphTriplesDefaultLimit, p.limit)
}

func TestParseTripleQueryParams_LimitNegative(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=-1", nil)
	_, errMsg := parseTripleQueryParams(r)

	assert.NotEmpty(t, errMsg, "negative limit should be rejected")
}

func TestParseTripleQueryParams_LimitNonInteger(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=abc", nil)
	_, errMsg := parseTripleQueryParams(r)

	assert.NotEmpty(t, errMsg, "non-integer limit should be rejected")
}

func TestParseTripleQueryParams_LimitOne(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=1", nil)
	p, errMsg := parseTripleQueryParams(r)

	require.Empty(t, errMsg)
	assert.Equal(t, 1, p.limit)
}

func TestParseTripleQueryParams_LimitMaxExact(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=1000", nil)
	p, errMsg := parseTripleQueryParams(r)

	require.Empty(t, errMsg)
	assert.Equal(t, 1000, p.limit)
}

// ---- tripleMatchesQuery ----

func TestTripleMatchesQuery_NoFilters(t *testing.T) {
	tr := withFixedTime(message.Triple{Subject: semantictest.EntityID(t, "acme", "ops", "test", "service", "entity", "1"), Predicate: semantictest.Predicate(t, "test", "state", "value"), Object: "val", Source: "src", Confidence: 1.0})
	p := tripleQueryParams{limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_SubjectMatch(t *testing.T) {
	tr := withFixedTime(message.Triple{Subject: semantictest.EntityID(t, "acme", "ops", "test", "service", "entity", "1"), Predicate: semantictest.Predicate(t, "test", "state", "value"), Object: "val", Source: "src", Confidence: 1.0})
	p := tripleQueryParams{subject: "acme.ops.test.service.entity.1", limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_SubjectNoMatch(t *testing.T) {
	tr := withFixedTime(message.Triple{Subject: semantictest.EntityID(t, "acme", "ops", "test", "service", "entity", "1"), Predicate: semantictest.Predicate(t, "test", "state", "value"), Object: "val", Source: "src", Confidence: 1.0})
	p := tripleQueryParams{subject: "acme.ops.test.service.entity.2", limit: 100}
	assert.False(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_PredicateMatch(t *testing.T) {
	tr := withFixedTime(message.Triple{Subject: semantictest.EntityID(t, "acme", "ops", "test", "service", "entity", "1"), Predicate: semantictest.Predicate(t, "ops", "diagnosis", "finding"), Object: "found something", Source: "ops-emit-diagnosis", Confidence: 0.9})
	p := tripleQueryParams{predicate: "ops.diagnosis.finding", limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_PredicateNoMatch(t *testing.T) {
	tr := withFixedTime(message.Triple{Subject: semantictest.EntityID(t, "acme", "ops", "test", "service", "entity", "1"), Predicate: semantictest.Predicate(t, "ops", "diagnosis", "finding"), Object: "found something", Source: "ops-emit-diagnosis", Confidence: 0.9})
	p := tripleQueryParams{predicate: "ops.diagnosis.recommendation", limit: 100}
	assert.False(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_ObjectStringMatch(t *testing.T) {
	tr := withFixedTime(message.Triple{Subject: semantictest.EntityID(t, "acme", "ops", "test", "service", "entity", "1"), Predicate: semantictest.Predicate(t, "test", "state", "value"), Object: "hello world", Source: "src", Confidence: 1.0})
	p := tripleQueryParams{object: "hello world", limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_ObjectStringNoMatch(t *testing.T) {
	tr := withFixedTime(message.Triple{Subject: semantictest.EntityID(t, "acme", "ops", "test", "service", "entity", "1"), Predicate: semantictest.Predicate(t, "test", "state", "value"), Object: "hello world", Source: "src", Confidence: 1.0})
	p := tripleQueryParams{object: "goodbye", limit: 100}
	assert.False(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_SubjectAndPredicateBothMatch(t *testing.T) {
	tr := withFixedTime(message.Triple{Subject: semantictest.EntityID(t, "acme", "ops", "test", "service", "entity", "1"), Predicate: semantictest.Predicate(t, "test", "state", "value"), Object: "val", Source: "src", Confidence: 1.0})
	p := tripleQueryParams{subject: "acme.ops.test.service.entity.1", predicate: "test.state.value", limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_SubjectMatchPredicateNoMatch(t *testing.T) {
	tr := withFixedTime(message.Triple{Subject: semantictest.EntityID(t, "acme", "ops", "test", "service", "entity", "1"), Predicate: semantictest.Predicate(t, "test", "state", "value"), Object: "val", Source: "src", Confidence: 1.0})
	p := tripleQueryParams{subject: "acme.ops.test.service.entity.1", predicate: "test.state.other", limit: 100}
	assert.False(t, tripleMatchesQuery(tr, p))
}

// ---- serveGraphTriples HTTP handler ----

// stubQuerier builds a querier function that returns a fixed result or error.
func stubQuerier(results []message.Triple, err error) func(context.Context, tripleQueryParams) ([]message.Triple, error) {
	return func(_ context.Context, _ tripleQueryParams) ([]message.Triple, error) {
		return results, err
	}
}

// captureQuerier builds a querier that captures the params it was called with
// and returns the provided results.
func captureQuerier(results []message.Triple) (func(context.Context, tripleQueryParams) ([]message.Triple, error), *tripleQueryParams) {
	var captured tripleQueryParams
	fn := func(_ context.Context, p tripleQueryParams) ([]message.Triple, error) {
		captured = p
		return results, nil
	}
	return fn, &captured
}

func TestServeGraphTriples_MethodNotAllowed(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "/graph/triples", nil)
			w := httptest.NewRecorder()

			serveGraphTriples(w, r, stubQuerier(nil, nil), nil)

			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		})
	}
}

func TestServeGraphTriples_MalformedLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=bad", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, stubQuerier(nil, nil), nil)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeGraphTriples_NegativeLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=-5", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, stubQuerier(nil, nil), nil)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeGraphTriples_EmptyResultIsArray(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, stubQuerier([]message.Triple{}, nil), nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Must decode as a JSON array, never null.
	var decoded []message.Triple
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded))
	assert.NotNil(t, decoded, "empty result must be [] not null")
	assert.Len(t, decoded, 0)
}

func TestServeGraphTriples_NonEmptyResult(t *testing.T) {
	ts := fixedTime
	triples := []message.Triple{
		{
			Subject:    "acme.ops.demo.sys.entity.001",
			Predicate:  semantictest.Predicate(t, "ops", "diagnosis", "finding"),
			Object:     "high error rate",
			Source:     "ops-emit-diagnosis",
			Confidence: 0.9,
			Timestamp:  ts,
		},
		{
			Subject:    "acme.ops.demo.sys.entity.002",
			Predicate:  semantictest.Predicate(t, "ops", "diagnosis", "recommendation"),
			Object:     "restart component",
			Source:     "ops-emit-diagnosis",
			Confidence: 0.8,
			Timestamp:  ts,
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/graph/triples?predicate=ops.diagnosis.finding", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, stubQuerier(triples, nil), nil)

	require.Equal(t, http.StatusOK, w.Code)

	var decoded []message.Triple
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded))
	require.Len(t, decoded, 2)
	assert.Equal(t, "ops.diagnosis.finding", decoded[0].Predicate)
	assert.Equal(t, 0.9, decoded[0].Confidence)
	assert.Equal(t, "ops-emit-diagnosis", decoded[0].Source)
	assert.Equal(t, ts.UTC(), decoded[0].Timestamp.UTC())
}

func TestServeGraphTriples_BackendError(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, stubQuerier(nil, errors.New("nats connection refused")), nil)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	// Body should be a terse message, not a stack trace.
	body := w.Body.String()
	assert.NotEmpty(t, body)
	assert.NotContains(t, body, "goroutine")
}

func TestServeGraphTriples_ParamsForwardedToQuerier(t *testing.T) {
	querier, captured := captureQuerier([]message.Triple{})

	r := httptest.NewRequest(http.MethodGet, "/graph/triples?subject=acme.ops.test.service.entity.1&predicate=test.state.value&object=obj&limit=42", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, querier, nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "acme.ops.test.service.entity.1", captured.subject)
	assert.Equal(t, "test.state.value", captured.predicate)
	assert.Equal(t, "obj", captured.object)
	assert.Equal(t, 42, captured.limit)
}

func TestServeGraphTriples_LimitClamped(t *testing.T) {
	querier, captured := captureQuerier([]message.Triple{})

	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=50000", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, querier, nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, graphTriplesMaxLimit, captured.limit, "limit must be clamped to max")
}

func TestServeGraphTriples_ContentTypeJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, stubQuerier([]message.Triple{}, nil), nil)

	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

// ---- message.Triple wire-shape round-trip ----

// TestServeGraphTriples_ContextAndDatatypeRoundTrip verifies that Context and
// Datatype — fields that TripleResponse formerly dropped — now survive the full
// HTTP encode/decode cycle.
func TestServeGraphTriples_ContextAndDatatypeRoundTrip(t *testing.T) {
	ts := fixedTime
	triple := message.Triple{
		Subject:    "acme.ops.demo.sys.entity.001",
		Predicate:  semantictest.Predicate(t, "ops", "diagnosis", "finding"),
		Object:     "high error rate",
		Source:     "ops-emit-diagnosis",
		Confidence: 0.9,
		Timestamp:  ts,
		Context:    "batch-abc-123",
		Datatype:   "xsd:string",
	}

	r := httptest.NewRequest(http.MethodGet, "/graph/triples", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, stubQuerier([]message.Triple{triple}, nil), nil)

	require.Equal(t, http.StatusOK, w.Code)

	var decoded []message.Triple
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded))
	require.Len(t, decoded, 1)
	assert.Equal(t, "batch-abc-123", decoded[0].Context, "Context field must round-trip")
	assert.Equal(t, "xsd:string", decoded[0].Datatype, "Datatype field must round-trip")
}

// TestServeGraphTriples_SourcePreservedWhenEmpty verifies that an empty Source is
// preserved in JSON (message.Triple uses source without omitempty), not silently
// dropped. The wire JSON must contain "source":"" for consumers relying on
// the authoritative shape.
func TestServeGraphTriples_SourcePreservedWhenEmpty(t *testing.T) {
	triple := message.Triple{
		Subject:    "acme.ops.demo.sys.entity.001",
		Predicate:  semantictest.Predicate(t, "ops", "test", "predicate"),
		Object:     "value",
		Source:     "", // explicitly empty
		Confidence: 1.0,
		Timestamp:  fixedTime,
	}

	r := httptest.NewRequest(http.MethodGet, "/graph/triples", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, stubQuerier([]message.Triple{triple}, nil), nil)

	require.Equal(t, http.StatusOK, w.Code)

	// Inspect raw JSON map to confirm "source" key is present with empty value.
	var raw []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	require.Len(t, raw, 1)
	val, hasSource := raw[0]["source"]
	assert.True(t, hasSource, "source key must be present even when empty (no omitempty on message.Triple.Source)")
	assert.Equal(t, "", val, "source value must be empty string, not omitted")
}

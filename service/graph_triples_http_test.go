package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTime is a stable timestamp for triple fixtures.
var fixedTime = time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

// makeTriple is a fixture builder for test triples.
func makeTriple(subject, predicate string, object any, source string, confidence float64) message.Triple {
	return message.Triple{
		Subject:    subject,
		Predicate:  predicate,
		Object:     object,
		Source:     source,
		Confidence: confidence,
		Timestamp:  fixedTime,
	}
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
	r := httptest.NewRequest(http.MethodGet, "/graph/triples?subject=s&predicate=p&object=o&limit=50", nil)
	p, errMsg := parseTripleQueryParams(r)

	require.Empty(t, errMsg)
	assert.Equal(t, "s", p.subject)
	assert.Equal(t, "p", p.predicate)
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
	tr := makeTriple("s1", "pred", "val", "src", 1.0)
	p := tripleQueryParams{limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_SubjectMatch(t *testing.T) {
	tr := makeTriple("s1", "pred", "val", "src", 1.0)
	p := tripleQueryParams{subject: "s1", limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_SubjectNoMatch(t *testing.T) {
	tr := makeTriple("s1", "pred", "val", "src", 1.0)
	p := tripleQueryParams{subject: "s2", limit: 100}
	assert.False(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_PredicateMatch(t *testing.T) {
	tr := makeTriple("s1", "ops.diagnosis.finding", "found something", "ops-emit-diagnosis", 0.9)
	p := tripleQueryParams{predicate: "ops.diagnosis.finding", limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_PredicateNoMatch(t *testing.T) {
	tr := makeTriple("s1", "ops.diagnosis.finding", "found something", "ops-emit-diagnosis", 0.9)
	p := tripleQueryParams{predicate: "ops.diagnosis.recommendation", limit: 100}
	assert.False(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_ObjectStringMatch(t *testing.T) {
	tr := makeTriple("s1", "pred", "hello world", "src", 1.0)
	p := tripleQueryParams{object: "hello world", limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_ObjectStringNoMatch(t *testing.T) {
	tr := makeTriple("s1", "pred", "hello world", "src", 1.0)
	p := tripleQueryParams{object: "goodbye", limit: 100}
	assert.False(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_SubjectAndPredicateBothMatch(t *testing.T) {
	tr := makeTriple("s1", "pred", "val", "src", 1.0)
	p := tripleQueryParams{subject: "s1", predicate: "pred", limit: 100}
	assert.True(t, tripleMatchesQuery(tr, p))
}

func TestTripleMatchesQuery_SubjectMatchPredicateNoMatch(t *testing.T) {
	tr := makeTriple("s1", "pred", "val", "src", 1.0)
	p := tripleQueryParams{subject: "s1", predicate: "other", limit: 100}
	assert.False(t, tripleMatchesQuery(tr, p))
}

// ---- serveGraphTriples HTTP handler ----

// stubQuerier builds a querier function that returns a fixed result or error.
func stubQuerier(results []TripleResponse, err error) func(context.Context, tripleQueryParams) ([]TripleResponse, error) {
	return func(_ context.Context, _ tripleQueryParams) ([]TripleResponse, error) {
		return results, err
	}
}

// captureQuerier builds a querier that captures the params it was called with
// and returns the provided results.
func captureQuerier(results []TripleResponse) (func(context.Context, tripleQueryParams) ([]TripleResponse, error), *tripleQueryParams) {
	var captured tripleQueryParams
	fn := func(_ context.Context, p tripleQueryParams) ([]TripleResponse, error) {
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

	serveGraphTriples(w, r, stubQuerier([]TripleResponse{}, nil), nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Must decode as a JSON array, never null.
	var decoded []TripleResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded))
	assert.NotNil(t, decoded, "empty result must be [] not null")
	assert.Len(t, decoded, 0)
}

func TestServeGraphTriples_NonEmptyResult(t *testing.T) {
	ts := fixedTime
	triples := []TripleResponse{
		{
			Subject:    "acme.ops.demo.sys.entity.001",
			Predicate:  "ops.diagnosis.finding",
			Object:     "high error rate",
			Source:     "ops-emit-diagnosis",
			Confidence: 0.9,
			Timestamp:  ts,
		},
		{
			Subject:    "acme.ops.demo.sys.entity.002",
			Predicate:  "ops.diagnosis.recommendation",
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

	var decoded []TripleResponse
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
	querier, captured := captureQuerier([]TripleResponse{})

	r := httptest.NewRequest(http.MethodGet, "/graph/triples?subject=sub&predicate=pred&object=obj&limit=42", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, querier, nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "sub", captured.subject)
	assert.Equal(t, "pred", captured.predicate)
	assert.Equal(t, "obj", captured.object)
	assert.Equal(t, 42, captured.limit)
}

func TestServeGraphTriples_LimitClamped(t *testing.T) {
	querier, captured := captureQuerier([]TripleResponse{})

	r := httptest.NewRequest(http.MethodGet, "/graph/triples?limit=50000", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, querier, nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, graphTriplesMaxLimit, captured.limit, "limit must be clamped to max")
}

func TestServeGraphTriples_ContentTypeJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/graph/triples", nil)
	w := httptest.NewRecorder()

	serveGraphTriples(w, r, stubQuerier([]TripleResponse{}, nil), nil)

	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

// ---- tripleFromMessage shape consistency ----

func TestTripleFromMessage_FieldMapping(t *testing.T) {
	ts := fixedTime
	src := makeTriple("s", "p", "o", "src", 0.75)
	src.Timestamp = ts

	resp := tripleFromMessage(src)

	assert.Equal(t, "s", resp.Subject)
	assert.Equal(t, "p", resp.Predicate)
	assert.Equal(t, "o", resp.Object)
	assert.Equal(t, "src", resp.Source)
	assert.Equal(t, 0.75, resp.Confidence)
	assert.Equal(t, ts, resp.Timestamp)
}

func TestTripleFromMessage_EmptySourceOmitted(t *testing.T) {
	tr := makeTriple("s", "p", "o", "", 1.0)
	resp := tripleFromMessage(tr)

	// Source is omitempty — marshal should omit it when empty.
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	_, hasSource := m["source"]
	assert.False(t, hasSource, "empty source should be omitted from JSON")
}

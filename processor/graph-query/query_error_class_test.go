// Package graphquery — error-class fidelity tests for RequestClassified migration (gh#304).
//
// This file verifies that graph-ingest handler errors surface to the caller as
// proper *errs.ClassifiedError values (not silently decoded as success data) after
// the handleQueryPrefix and handleQueryHierarchyStats migration from plain Request()
// to RequestClassified().
//
// ADR-060 PR-D: the legacy "error: <msg>" body-prefix fallback is gone from
// ClassifyReply. A handler failure is now signalled ONLY by the X-Status: error
// header. Tests simulate this by returning (nil, classifiedErr) directly from
// requestClassifiedFunc — the same shape the production wire produces after
// ClassifyReply reconstructs from headers.
package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/query"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/nats-io/nats.go"
)

// newComponentForHandlerTest builds a minimal Component with router wired
// without starting a full NATS connection. The router is the only dependency
// the pure-query handlers need beyond the natsClient mock.
func newComponentForHandlerTest(t *testing.T, mock *mockNATSClient) *Component {
	t.Helper()
	comp := createTestComponentWithMockClient(t, mock)
	comp.router = NewStaticRouter(comp.logger)
	return comp
}

func TestLoadEntitiesRejectsPoisonedAggregateBeforeReturn(t *testing.T) {
	t.Parallel()

	invalidEntityID := "bad"
	response, err := json.Marshal(map[string]any{"entities": []*graph.EntityState{
		{ID: "acme.ops.test.system.widget.001"},
		{ID: invalidEntityID},
	}})
	require.NoError(t, err)
	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		assert.Equal(t, "graph.ingest.query.batch", subject)
		return response, nil
	}
	comp := newComponentForHandlerTest(t, mock)

	entities, err := comp.loadEntities(context.Background(), []string{"acme.ops.test.system.widget.001"})
	require.Error(t, err)
	assert.True(t, graph.IsStateContractError(err))
	assert.Nil(t, entities, "the valid prefix of a poisoned batch must not escape")
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, errs.ErrorFatal, classified.Class)
	assert.Equal(t, graph.ErrorCodeGraphStateResetRequired, classified.Code)
}

func TestPublicEntitySurfacesRejectCompleteCandidatePoisonBeforeSuccess(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidPublicEntityID := "bad"
	poisons := []graph.EntityState{
		{ID: invalidPublicEntityID},
		{ID: validID, Triples: []message.Triple{{Subject: invalidPublicEntityID, Predicate: "test.state.value"}}},
		{ID: validID, Triples: []message.Triple{{
			Subject: validID, Predicate: "test.state.target", Object: invalidPublicEntityID, Datatype: message.EntityReferenceDatatype,
		}}},
	}
	tests := []struct {
		name    string
		request []byte
		wrap    func(graph.EntityState) []byte
		handle  func(*Component, context.Context, []byte) ([]byte, error)
	}{
		{
			name:    "entity",
			request: []byte(`{"id":"acme.ops.test.system.widget.001"}`),
			wrap:    func(entity graph.EntityState) []byte { return mustMarshalQueryFixture(t, entity) },
			handle:  (*Component).handleQueryEntity,
		},
		{
			name:    "entity by alias",
			request: []byte(`{"aliasOrID":"acme.ops.test.system.widget.001"}`),
			wrap:    func(entity graph.EntityState) []byte { return mustMarshalQueryFixture(t, entity) },
			handle:  (*Component).handleQueryEntityByAlias,
		},
		{
			name:    "batch",
			request: []byte(`{"ids":["acme.ops.test.system.widget.001"]}`),
			wrap: func(entity graph.EntityState) []byte {
				return mustMarshalQueryFixture(t, map[string]any{"entities": []graph.EntityState{{ID: validID}, entity}})
			},
			handle: (*Component).handleQueryBatch,
		},
		{
			name:    "prefix",
			request: []byte(`{"prefix":"acme.ops"}`),
			wrap: func(entity graph.EntityState) []byte {
				return mustMarshalQueryFixture(t, graph.PrefixQueryResponse{Entities: []graph.EntityState{{ID: validID}, entity}})
			},
			handle: (*Component).handleQueryPrefix,
		},
	}

	for _, tt := range tests {
		for poisonIndex, poison := range poisons {
			t.Run(fmt.Sprintf("%s/poison-%d", tt.name, poisonIndex), func(t *testing.T) {
				response := tt.wrap(poison)
				mock := newMockNATSClient()
				mock.requestClassifiedFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
					if subject == "graph.index.query.alias" {
						return []byte(`{"data":{}}`), nil
					}
					return response, nil
				}
				comp := newComponentForHandlerTest(t, mock)

				got, err := tt.handle(comp, context.Background(), tt.request)
				require.Error(t, err)
				assert.True(t, graph.IsStateContractError(err))
				assert.Nil(t, got, "poison must not be emitted as a successful public response")
				var classified *errs.ClassifiedError
				require.ErrorAs(t, err, &classified)
				assert.Equal(t, errs.ErrorFatal, classified.Class)
				assert.Equal(t, graph.ErrorCodeGraphStateResetRequired, classified.Code)
				assert.Zero(t, comp.messagesProcessed, "poison must not increment success metrics")
				assert.EqualValues(t, 1, comp.errors, "poison is recorded as an error")
			})
		}
	}
}

func TestHandleGlobalSearchDoesNotFallbackAfterAuthoritativePoison(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidFallbackEntityID := "bad"
	semantic := mustMarshalQueryFixture(t, map[string]any{
		"results":       []map[string]any{{"entity_id": validID, "similarity": 0.99}},
		"embedder_type": "neural",
	})
	poisonedBatch := mustMarshalQueryFixture(t, map[string]any{"entities": []graph.EntityState{
		{ID: validID},
		{ID: validID, Triples: []message.Triple{{Subject: invalidFallbackEntityID, Predicate: "test.state.value"}}},
	}})
	var requests atomic.Int64
	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		requests.Add(1)
		switch subject {
		case "graph.embedding.query.search":
			return semantic, nil
		case "graph.ingest.query.batch":
			return poisonedBatch, nil
		default:
			return nil, fmt.Errorf("unexpected fallback request: %s", subject)
		}
	}
	comp := newComponentForHandlerTest(t, mock)

	got, err := comp.handleGlobalSearch(context.Background(), []byte(`{
		"query":"widget", "summarize_threshold":-1, "include_summaries":false
	}`))
	require.Error(t, err)
	assert.True(t, graph.IsStateContractError(err))
	assert.Nil(t, got, "fatal replay poison must not degrade to a successful text fallback")
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, errs.ErrorFatal, classified.Class)
	assert.Equal(t, graph.ErrorCodeGraphStateResetRequired, classified.Code)
	assert.EqualValues(t, 2, requests.Load(), "only semantic lookup and authoritative batch load should run")
	assert.Zero(t, comp.messagesProcessed, "fatal poison must not record success")
}

func TestEntityLookupStrategyDoesNotFallThroughAfterAuthoritativePoison(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidLookupEntityID := "bad"
	poisonedBatch := mustMarshalQueryFixture(t, map[string]any{"entities": []graph.EntityState{{
		ID: validID,
		Triples: []message.Triple{{
			Subject: validID, Predicate: "test.state.target", Object: invalidLookupEntityID, Datatype: message.EntityReferenceDatatype,
		}},
	}}})
	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		assert.Equal(t, "graph.ingest.query.batch", subject)
		return poisonedBatch, nil
	}
	comp := newComponentForHandlerTest(t, mock)
	classification := &query.ClassificationResult{Options: map[string]any{"path_start_node": validID}}

	got, handled, err := comp.handleStrategyEntityLookup(context.Background(), classification, "widget", time.Now())
	require.Error(t, err)
	assert.True(t, graph.IsStateContractError(err))
	assert.Nil(t, got)
	assert.False(t, handled, "fatal poison is an error, not permission to run a fallback strategy")
	assert.Zero(t, comp.messagesProcessed)
}

func TestHandleGlobalSearchPreservesWireClassifiedGraphReset(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	semantic := mustMarshalQueryFixture(t, map[string]any{
		"results":       []map[string]any{{"entity_id": validID, "similarity": 0.99}},
		"embedder_type": "neural",
	})
	wireReply := &nats.Msg{Header: nats.Header{}, Data: []byte(`{"message":"authoritative graph reset required"}`)}
	wireReply.Header.Set(natsclient.HeaderStatus, natsclient.HeaderStatusError)
	wireReply.Header.Set(natsclient.HeaderErrorClass, errs.ErrorFatal.String())
	wireReply.Header.Set(natsclient.HeaderErrorCode, graph.ErrorCodeGraphStateResetRequired)
	_, wireErr := natsclient.ClassifyReply(wireReply)
	require.Error(t, wireErr)
	require.True(t, graph.IsStateContractError(wireErr))

	var requests atomic.Int64
	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		requests.Add(1)
		switch subject {
		case "graph.embedding.query.search":
			return semantic, nil
		case "graph.ingest.query.batch":
			return nil, wireErr
		default:
			return nil, fmt.Errorf("unexpected fallback request: %s", subject)
		}
	}
	comp := newComponentForHandlerTest(t, mock)

	got, err := comp.handleGlobalSearch(context.Background(), []byte(`{
		"query":"widget", "summarize_threshold":-1, "include_summaries":false
	}`))
	require.Error(t, err)
	assert.Equal(t, wireErr, err, "wire-classified graph reset must pass through unchanged")
	assert.True(t, graph.IsStateContractError(err))
	assert.False(t, errs.IsTransient(err), "fatal reset must not acquire an outer transient class")
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, errs.ErrorFatal, classified.Class)
	assert.Equal(t, graph.ErrorCodeGraphStateResetRequired, classified.Code)
	assert.Nil(t, got)
	assert.EqualValues(t, 2, requests.Load(), "fatal batch error must stop before fallback")
	assert.Zero(t, comp.messagesProcessed)
}

func mustMarshalQueryFixture(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

// TestHandleQueryEntity_PassthroughPropagatesClassifiedError is the B1
// regression lock (go-reviewer, ADR-060 PR-D). handleQueryEntity is a
// passthrough to graph-ingest's graph.ingest.query.entity handler. A downstream
// classified error (e.g. entity_not_found) MUST propagate as a *errs.ClassifiedError
// so graph-query's SubscribeForRequests wrapper re-stamps the wire class+code —
// it must NOT be re-emitted verbatim as a success body, which a consumer would
// decode as a zero entity (the silent 404→200 PR-D would otherwise introduce
// once the legacy error-body fallback was removed). Pre-PR-D this used plain
// Request() and dropped the classification.
func TestHandleQueryEntity_PassthroughPropagatesClassifiedError(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, "entity_not_found", errors.New("not found: acme.x"))
	}

	comp := newComponentForHandlerTest(t, mock)

	resp, err := comp.handleQueryEntity(context.Background(), []byte(`{"id":"acme.x"}`))
	require.Error(t, err, "a downstream classified error must propagate, not be re-emitted as a success body")
	require.Nil(t, resp)
	require.True(t, errs.IsInvalid(err), "the invalid class must survive the passthrough")
	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, "entity_not_found", ce.Code, "the entity_not_found code must survive the passthrough (404 mapping)")
}

// entity-id-audit:classify intentional-malformed "bad" line=49 column=21 surface=go-assignment:invalidEntityID GraphRAG batch aggregate poison fixture
// entity-id-audit:classify intentional-malformed "bad" line=76 column=27 surface=go-assignment:invalidPublicEntityID public query complete-candidate poison fixture
// entity-id-audit:classify intentional-malformed "bad" line=152 column=29 surface=go-assignment:invalidFallbackEntityID GraphRAG fatal fallback poison fixture
// entity-id-audit:classify intentional-malformed "bad" line=194 column=27 surface=go-assignment:invalidLookupEntityID entity lookup fatal fallback poison fixture

// ─────────────────────────────────────────────────────────────────────────────
// handleQueryPrefix — error-class fidelity (gh#304 primary fix)
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleQueryPrefix_InvalidPrefixHasNoDownstreamRequest(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	var requests atomic.Int64
	mock.requestClassifiedFunc = func(context.Context, string, []byte, time.Duration) ([]byte, error) {
		requests.Add(1)
		return nil, nil
	}
	comp := newComponentForHandlerTest(t, mock)
	_, err := comp.handleQueryPrefix(context.Background(), []byte(`{"prefix":"acme.*"}`))
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, semtypes.ErrorCodeEntityIDPrefixInvalid, classified.Code)
	assert.Zero(t, requests.Load())
}

// TestHandleQueryPrefix_TransientHandlerErrorSurfacesAsError is the primary
// regression lock for gh#304. A graph-ingest handler returning a transient
// failure must reach the caller as a non-nil error — NOT as a byte slice that
// json.Unmarshal would silently decode as success data.
//
// Before the fix: plain Request() returned the body verbatim with err==nil.
// Callers that json.Unmarshal'd the body got silent corruption.
//
// After the fix: RequestClassified returns a *errs.ClassifiedError directly;
// the handler-under-test surfaces it as err != nil.
func TestHandleQueryPrefix_TransientHandlerErrorSurfacesAsError(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		assert.Equal(t, "graph.ingest.query.prefix", subject,
			"must forward to entityPrefix route")
		// Transient handler failure — e.g. store unavailable.
		return nil, errs.Classified(errs.ErrorTransient, errors.New("store unavailable"))
	}

	comp := newComponentForHandlerTest(t, mock)

	resp, err := comp.handleQueryPrefix(context.Background(), []byte(`{"prefix":"acme","limit":100}`))

	require.Error(t, err,
		"handler error must surface as err, not be returned as success bytes")
	assert.Nil(t, resp,
		"response must be nil when the handler reported an error")
}

// TestHandleQueryPrefix_HandlerErrorNotSilentlyDecoded verifies that a handler
// error from graph-ingest cannot be fed to json.Unmarshal as though it were
// valid JSON — the caller always receives err != nil and nil resp on failure.
//
// Regression: before gh#304 a raw "error: " body reached the caller with
// err==nil; the caller's json.Unmarshal then silently produced a zero-value
// struct (silent corruption). This test locks that path closed.
func TestHandleQueryPrefix_HandlerErrorNotSilentlyDecoded(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		// Transient handler failure.
		return nil, errs.Classified(errs.ErrorTransient, errors.New("jetstream timeout"))
	}

	comp := newComponentForHandlerTest(t, mock)

	resp, err := comp.handleQueryPrefix(context.Background(), []byte(`{"prefix":"x","limit":1}`))

	require.Error(t, err)
	// resp must be nil on error — no body that could be silently unmarshalled.
	if resp != nil {
		// If resp is non-nil it must be valid JSON (not a raw error string).
		var probe interface{}
		unmarshalErr := json.Unmarshal(resp, &probe)
		assert.NoError(t, unmarshalErr,
			"if resp is non-nil it must be valid JSON, not a raw error string")
	}
}

// TestHandleQueryPrefix_TransportErrorSurfacesAsTransient verifies that a NATS
// transport error (returned as err from the mock, not as a handler error) still
// surfaces correctly after the RequestClassified migration.
func TestHandleQueryPrefix_TransportErrorSurfacesAsTransient(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	mock.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return nil, errs.WrapTransient(nil, "mock", "Request", "no responders")
	}

	comp := newComponentForHandlerTest(t, mock)

	_, err := comp.handleQueryPrefix(context.Background(), []byte(`{"prefix":"a","limit":5}`))

	require.Error(t, err)
	assert.True(t, errs.IsTransient(err),
		"transport failure must be classified as Transient: %v", err)
}

// TestHandleQueryPrefix_SuccessBodyPassesThroughIntact verifies that a valid
// graph-ingest response passes through unmodified — no spurious errors.
func TestHandleQueryPrefix_SuccessBodyPassesThroughIntact(t *testing.T) {
	t.Parallel()

	successBody := []byte(`{"entities":[{"id":"acme.ops.robotics.gcs.drone.001"}],"cursor":"","total":1}`)

	mock := newMockNATSClient()
	mock.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return successBody, nil
	}

	comp := newComponentForHandlerTest(t, mock)

	resp, err := comp.handleQueryPrefix(context.Background(), []byte(`{"prefix":"acme.ops","limit":100}`))

	require.NoError(t, err, "success body must not produce an error")
	assert.Equal(t, successBody, resp, "success body must be returned intact")
}

// ─────────────────────────────────────────────────────────────────────────────
// handleQueryHierarchyStats — error-class fidelity (gh#304 sibling fix)
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleQueryHierarchyStatsRejectsPoisonedPrefixAggregate(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidEntityID := "bad"
	response, err := json.Marshal(graph.PrefixQueryResponse{Entities: []graph.EntityState{
		{ID: validID},
		{ID: validID, Triples: []message.Triple{{Subject: invalidEntityID, Predicate: "test.state.value"}}},
	}})
	require.NoError(t, err)
	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(context.Context, string, []byte, time.Duration) ([]byte, error) {
		return response, nil
	}
	comp := newComponentForHandlerTest(t, mock)

	got, err := comp.handleQueryHierarchyStats(context.Background(), []byte(`{"prefix":"acme.ops"}`))
	require.Error(t, err)
	assert.True(t, graph.IsStateContractError(err))
	assert.Nil(t, got, "poison must fail before hierarchy aggregation")
}

// TestHandleQueryHierarchyStats_TransientHandlerErrorSurfacesAsError is the
// regression lock for the hierarchyStats sibling fix (gh#304).
//
// Before the fix: a transient store failure from graph-ingest arrived as an
// "error: " body with err==nil; json.Unmarshal failed with "invalid character
// 'e'" which was then wrapped as WrapInvalid — wrong class, and surfaced via
// a parse-step error rather than the original handler failure.
//
// After the fix: RequestClassified returns (nil, classifiedErr) directly;
// the handler surfaces it before json.Unmarshal is called, preserving both
// the error class and the message origin.
func TestHandleQueryHierarchyStats_TransientHandlerErrorSurfacesAsError(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		assert.Equal(t, "graph.ingest.query.prefix", subject,
			"hierarchyStats must fan out to entityPrefix route")
		// Transient handler failure from graph-ingest.
		return nil, errs.Classified(errs.ErrorTransient, errors.New("raft store unavailable"))
	}

	comp := newComponentForHandlerTest(t, mock)

	_, err := comp.handleQueryHierarchyStats(context.Background(), []byte(`{"prefix":"acme.ops"}`))

	require.Error(t, err,
		"handler error from graph-ingest must surface via err return")

	// Key regression: before the fix this contained "invalid character 'e'" from
	// json.Unmarshal trying to parse an "error: " body as an entities envelope.
	assert.NotContains(t, err.Error(), "invalid character",
		"error must not be a JSON parse failure (indicates pre-fix behaviour where handler body reached Unmarshal)")
	assert.NotContains(t, err.Error(), "parse prefix response",
		"error must not be attributed to the parse step (pre-fix: unmarshal of error body)")
}

// TestHandleQueryHierarchyStats_SuccessReturnsHierarchy verifies normal
// operation after the RequestClassified migration: a valid entity list from
// graph-ingest produces the expected hierarchy stats response.
func TestHandleQueryHierarchyStats_SuccessReturnsHierarchy(t *testing.T) {
	t.Parallel()

	entitiesBody := []byte(`{"entities":[
		{"id":"acme.ops.robotics.gcs.drone.001"},
		{"id":"acme.ops.robotics.gcs.drone.002"},
		{"id":"acme.ops.robotics.ground.sensor.003"}
	]}`)

	mock := newMockNATSClient()
	mock.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return entitiesBody, nil
	}

	comp := newComponentForHandlerTest(t, mock)

	resp, err := comp.handleQueryHierarchyStats(context.Background(), []byte(`{"prefix":"acme.ops"}`))

	require.NoError(t, err)
	require.NotEmpty(t, resp, "hierarchy response must not be empty")

	// Decode using the production response shape (HierarchyChild is the production type).
	var stats struct {
		Prefix        string           `json:"prefix"`
		TotalEntities int              `json:"totalEntities"`
		Children      []HierarchyChild `json:"children"`
	}
	require.NoError(t, json.Unmarshal(resp, &stats),
		"hierarchy response must decode as the production stats shape")
	assert.Equal(t, "acme.ops", stats.Prefix)
	assert.Equal(t, 3, stats.TotalEntities)
	require.NotEmpty(t, stats.Children, "should have at least one child prefix bucket")
}

// TestHandleQueryHierarchyStats_InvalidRequestSurfacesAsInvalid verifies that
// a malformed incoming request body still returns WrapInvalid — not Transient.
// This ensures the RequestClassified migration did not accidentally swallow
// client-side parse errors.
func TestHandleQueryHierarchyStats_InvalidRequestSurfacesAsInvalid(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	// requestFunc should not be called — the handler should reject before routing.
	mock.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		t.Error("requestFunc should not be called for an invalid incoming request")
		return nil, nil
	}

	comp := newComponentForHandlerTest(t, mock)

	_, err := comp.handleQueryHierarchyStats(context.Background(), []byte(`{not valid json}`))

	require.Error(t, err)
	assert.True(t, errs.IsInvalid(err),
		"malformed incoming request must be classified as Invalid: %v", err)
}

// ─────────────────────────────────────────────────────────────────────────────
// handleStrategyTemporal / handleStrategySpatial — error-class fidelity (gh#326)
// ─────────────────────────────────────────────────────────────────────────────
//
// These strategy paths delegate to graph-temporal / graph-spatial via NATS, then
// feed the response to parseEntityIDsFromResults. Pre-fix they used plain Request():
// a handler error arrived as a body that parseEntityIDsFromResults decoded as an
// EMPTY result set — a silent 0-entity "success". After the ADR-060 migration to
// RequestClassified, a handler failure surfaces as a non-nil error.
//
// The mock returns an empty-array body via raw Request() (the silent-corruption
// shape) AND a classified error via RequestClassified. Regressed code (raw Request)
// would parse "[]" into zero entities and return a 0-entity success with err==nil;
// the fixed code surfaces the classified error before parsing.

func TestHandleStrategyTemporal_HandlerErrorSurfaces(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	// Silent-corruption shape: regressed raw Request() would decode this as 0 entities.
	mock.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return []byte("[]"), nil
	}
	mock.requestClassifiedFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		assert.Equal(t, "graph.temporal.query.range", subject, "must route to the temporal index")
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("temporal index unavailable"))
	}

	comp := newComponentForHandlerTest(t, mock)
	cr := &query.ClassificationResult{Options: map[string]any{
		"time_range": &query.TimeRange{Start: time.Unix(0, 0), End: time.Unix(3600, 0)},
	}}
	req := &GlobalSearchRequest{Query: "events in the last hour"}

	resp, err := comp.handleStrategyTemporal(context.Background(), cr, req, time.Unix(0, 0), 0)

	require.Error(t, err, "a graph-temporal handler error must surface, not decode as an empty result set")
	require.Nil(t, resp)
	assert.True(t, errs.IsTransient(err), "the transient class must survive: %v", err)
}

func TestHandleStrategySpatial_HandlerErrorSurfaces(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	mock.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return []byte("[]"), nil
	}
	mock.requestClassifiedFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		assert.Equal(t, "graph.spatial.query.bounds", subject, "must route to the spatial index")
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("spatial index unavailable"))
	}

	comp := newComponentForHandlerTest(t, mock)
	cr := &query.ClassificationResult{Options: map[string]any{
		"geo_bounds": &query.SpatialBounds{North: 1, South: 0, East: 1, West: 0},
	}}
	req := &GlobalSearchRequest{Query: "sensors near the GCS"}

	resp, err := comp.handleStrategySpatial(context.Background(), cr, req, time.Unix(0, 0), 0)

	require.Error(t, err, "a graph-spatial handler error must surface, not decode as an empty result set")
	require.Nil(t, resp)
	assert.True(t, errs.IsTransient(err), "the transient class must survive: %v", err)
}

// entity-id-audit:classify intentional-malformed "bad" line=436 column=21 surface=go-assignment:invalidEntityID hierarchy prefix aggregate poison fixture

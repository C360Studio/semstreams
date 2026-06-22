// Package graphquery — error-class fidelity tests for RequestClassified migration (gh#304).
//
// This file verifies that transient graph-ingest errors arriving as the legacy
// "error: <msg>" body shape surface to the caller as proper errors (not as
// silently-decoded success data) after the handleQueryPrefix and
// handleQueryHierarchyStats migration from plain Request() to RequestClassified().
//
// The mock's RequestClassified runs the body through ClassifyReply (legacy body
// path — no real X-Error-Class header), exercising the same classification
// behaviour the wire would trigger on pre-#93 handlers.
package graphquery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/pkg/errs"
)

// legacyErrorBody returns the wire body that pre-#93 handlers emit on failure.
// ClassifyReply's body-prefix fallback maps this conservatively to ErrorInvalid.
// With RequestClassified the error surfaces via err before the body reaches
// json.Unmarshal — fixing silent corruption and enabling correct class detection.
func legacyErrorBody(msg string) []byte {
	return []byte("error: " + msg)
}

// newComponentForHandlerTest builds a minimal Component with router wired
// without starting a full NATS connection. The router is the only dependency
// the pure-query handlers need beyond the natsClient mock.
func newComponentForHandlerTest(t *testing.T, mock *mockNATSClient) *Component {
	t.Helper()
	comp := createTestComponentWithMockClient(t, mock)
	comp.router = NewStaticRouter(comp.logger)
	return comp
}

// ─────────────────────────────────────────────────────────────────────────────
// handleQueryPrefix — error-class fidelity (gh#304 primary fix)
// ─────────────────────────────────────────────────────────────────────────────

// TestHandleQueryPrefix_TransientLegacyBodySurfacesAsError is the primary
// regression lock for gh#304. A graph-ingest handler returning a transient
// failure via the legacy "error: <msg>" body must reach the caller as a
// non-nil error — NOT as a byte slice that json.Unmarshal would silently
// decode as success data.
//
// Before the fix: plain Request() returned the body verbatim with err==nil.
// Callers that json.Unmarshal'd the body got silent corruption.
// Callers that sniffed the "error: " prefix got ErrorInvalid (wrong class).
//
// After the fix: RequestClassified intercepts the body inside the mock's
// ClassifyReply path and returns err != nil.
func TestHandleQueryPrefix_TransientLegacyBodySurfacesAsError(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	mock.requestFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		assert.Equal(t, "graph.ingest.query.prefix", subject,
			"must forward to entityPrefix route")
		// Pre-#93 transient failure emitted as legacy body shape
		return legacyErrorBody("store unavailable"), nil
	}

	comp := newComponentForHandlerTest(t, mock)

	resp, err := comp.handleQueryPrefix(context.Background(), []byte(`{"prefix":"acme","limit":100}`))

	require.Error(t, err,
		"legacy 'error: ' body must surface as err, not be returned as success bytes")
	assert.Nil(t, resp,
		"response must be nil when the handler reported an error")
}

// TestHandleQueryPrefix_ErrorBodyNotSilentlyDecoded verifies that the "error: "
// body cannot be fed to json.Unmarshal as though it were valid JSON — i.e. the
// caller never receives the raw error string as a response payload.
func TestHandleQueryPrefix_ErrorBodyNotSilentlyDecoded(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	mock.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return legacyErrorBody("jetstream timeout"), nil
	}

	comp := newComponentForHandlerTest(t, mock)

	resp, err := comp.handleQueryPrefix(context.Background(), []byte(`{"prefix":"x","limit":1}`))

	require.Error(t, err)
	// The returned bytes (if any) must not be valid JSON wrapping an error string.
	// (Regression: before the fix resp == []byte("error: jetstream timeout"), which
	// the caller would then json.Unmarshal into a result struct silently.)
	if resp != nil {
		// If resp is non-nil it should at least be valid JSON (not the raw error: body)
		var probe interface{}
		unmarshalErr := json.Unmarshal(resp, &probe)
		assert.NoError(t, unmarshalErr,
			"if resp is non-nil it must be valid JSON, not a raw 'error: ' string")
	}
}

// TestHandleQueryPrefix_TransportErrorSurfacesAsTransient verifies that a NATS
// transport error (returned as err from the mock, not as a legacy body) still
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

// TestHandleQueryHierarchyStats_TransientLegacyBodySurfacesAsError is the
// regression lock for the hierarchyStats sibling fix (gh#304).
//
// Before the fix: plain Request() returned "error: <msg>" with err==nil;
// json.Unmarshal tried to parse the error string as an entities envelope and
// failed with "invalid character 'e'" — which was then wrapped as WrapInvalid.
// A transient store failure arrived at the caller mis-classified as Invalid.
//
// After the fix: RequestClassified intercepts the body and returns err != nil
// before json.Unmarshal is called, preserving the error class signal.
func TestHandleQueryHierarchyStats_TransientLegacyBodySurfacesAsError(t *testing.T) {
	t.Parallel()

	mock := newMockNATSClient()
	mock.requestFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		assert.Equal(t, "graph.ingest.query.prefix", subject,
			"hierarchyStats must fan out to entityPrefix route")
		return legacyErrorBody("raft store unavailable"), nil
	}

	comp := newComponentForHandlerTest(t, mock)

	_, err := comp.handleQueryHierarchyStats(context.Background(), []byte(`{"prefix":"acme.ops"}`))

	require.Error(t, err,
		"legacy 'error: ' body from graph-ingest must surface via err return")

	// Key regression: before the fix this contained "invalid character 'e'" from
	// json.Unmarshal trying to parse the "error: " body as an entities envelope.
	assert.NotContains(t, err.Error(), "invalid character",
		"error must not be a JSON parse failure of the error: body (indicates pre-fix behaviour)")
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

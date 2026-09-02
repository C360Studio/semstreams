package agenticdispatch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec: agentic-dispatch / One control-signal payload travels the loop signal subject
// The dispatch-local control-signal category is gone from the composed
// registry, and the user control signal is the only payload registered for the
// loop signal subject.
//
// It builds the registry the way a binary does — payloadbuiltins.Register, the
// one composition root that ran the retired dispatch registration — rather than
// asserting over a hand-listed set, so a composition root that still called a
// dispatch registration would fail here. That call cannot come back silently
// either: agenticdispatch.RegisterPayloads no longer exists, so restoring it is
// a compile error at the caller, not a quiet re-registration.
func TestRetiredSignalMessageCategoryIsUnregistered(t *testing.T) {
	reg := payloadbuiltins.NewTestRegistry(t)

	assert.Nil(t, reg.Create(agentic.Domain, "signal_message", agentic.SchemaVersion),
		"the retired dispatch-local control-signal category must not resolve to a payload")

	require.NotNil(t, reg.Create(agentic.Domain, agentic.CategorySignal, agentic.SchemaVersion),
		"the user control signal stays registered — it is the one type on this subject")
	_, isUserSignal := reg.Create(agentic.Domain, agentic.CategorySignal, agentic.SchemaVersion).(*agentic.UserSignal)
	assert.True(t, isUserSignal, "the signal category resolves to agentic.UserSignal")

	for _, registration := range reg.ListByDomain(agentic.Domain) {
		assert.NotEqual(t, "signal_message", registration.Category,
			"no registration in the agentic domain still carries the retired category")
	}
}

// spec: agentic-dispatch / One control-signal payload travels the loop signal subject
// The loop signal endpoint is gone, and it is gone from BOTH surfaces an
// adopter can see: the route table a running process serves, and the OpenAPI
// document generated from the same declaration. Asserting only one of them
// would let the other come back — a route with no documented path, or a
// documented path with no route.
//
// It drives the real registrar rather than reading the source, so a
// reintroduced handler fails here at the status code.
func TestLoopSignalEndpointIsGone(t *testing.T) {
	mux := http.NewServeMux()
	newTestComponent(t).RegisterHTTPHandlers("/", mux)

	req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/signal",
		strings.NewReader(`{"type":"cancel"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code,
		"no handler is registered for the retired signal endpoint")

	// The route that replaced it is still there: cancelling a loop is a
	// /cancel command on the message endpoint.
	_, messagePattern := mux.Handler(httptest.NewRequest(http.MethodPost, "/message", nil))
	assert.NotEmpty(t, messagePattern, "POST /message is the cancel lane and stays registered")

	spec := agenticDispatchOpenAPISpec()
	for path := range spec.Paths {
		assert.NotContains(t, path, "/signal",
			"the published OpenAPI document declares no signal path")
	}
	for _, schemaType := range append(spec.ResponseTypes, spec.RequestBodyTypes...) {
		assert.NotContains(t, schemaType.Name(), "Signal",
			"no signal request or response type is published as a component schema")
	}
}

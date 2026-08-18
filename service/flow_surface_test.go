package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFlowLifecycleRoutesAndSchemasAreAbsent(t *testing.T) {
	spec := flowServiceOpenAPISpec()
	for _, path := range []string{
		"/deployment/{id}/deploy", "/deployment/{id}/start", "/deployment/{id}/stop",
		"/status/stream", "/flows/{id}/runtime/logs", "/flows/{id}/runtime/health",
		"/flows/{id}/runtime/metrics", "/flows/{id}/runtime/messages",
	} {
		if _, ok := spec.Paths[path]; ok {
			t.Fatalf("retired flow lifecycle/ownership path still advertised: %s", path)
		}
	}
	if _, ok := spec.Paths["/flows/{id}/publish-component-configs"]; !ok {
		t.Fatal("publish-component-configs path missing")
	}

	service := &FlowService{BaseService: NewBaseServiceWithOptions("flow-surface-test", nil)}
	mux := http.NewServeMux()
	service.RegisterHTTPHandlers("/flowbuilder", mux)
	for _, path := range []string{
		"/flowbuilder/deployment/example/start",
		"/flowbuilder/status/stream",
		"/flowbuilder/flows/example/runtime/logs",
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("retired route %s returned %d", path, recorder.Code)
		}
	}
}

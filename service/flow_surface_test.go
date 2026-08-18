package service

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/flowstore"
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

func TestFlowOpenAPIPreservesFlowCRUDWireSchema(t *testing.T) {
	spec := flowServiceOpenAPISpec()
	for _, operation := range []*OperationSpec{
		spec.Paths["/flows"].POST,
		spec.Paths["/flows/{id}"].PUT,
		spec.Paths["/flows/{id}/validate"].POST,
	} {
		if got := operation.RequestBody.SchemaRef; got != "#/components/schemas/Flow" {
			t.Errorf("request schema ref = %q, want Flow", got)
		}
	}

	if got := spec.Paths["/flows"].POST.Responses["201"].SchemaRef; got != "#/components/schemas/Flow" {
		t.Errorf("create response schema ref = %q, want Flow", got)
	}
	if got := spec.Paths["/flows/{id}"].PUT.Responses["200"].SchemaRef; got != "#/components/schemas/Flow" {
		t.Errorf("update response schema ref = %q, want Flow", got)
	}

	schema := SchemaFromType(reflect.TypeOf(flowstore.Flow{}))
	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"id", "version", "created_at", "updated_at", "created_by", "last_modified"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("Flow schema lost preserved field %q", name)
		}
	}
}

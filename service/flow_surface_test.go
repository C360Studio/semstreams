package service

import (
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestFlowOpenAPISeparatesStrictAuthoringRequestsFromResponses(t *testing.T) {
	spec := flowServiceOpenAPISpec()
	tests := []struct {
		operation *OperationSpec
		wantRef   string
		typeOf    reflect.Type
	}{
		{operation: spec.Paths["/flows"].POST, wantRef: "#/components/schemas/FlowCreateRequest", typeOf: reflect.TypeOf(FlowCreateRequest{})},
		{operation: spec.Paths["/flows/{id}"].PUT, wantRef: "#/components/schemas/FlowUpdateRequest", typeOf: reflect.TypeOf(FlowUpdateRequest{})},
		{operation: spec.Paths["/flows/{id}/validate"].POST, wantRef: "#/components/schemas/FlowValidateRequest", typeOf: reflect.TypeOf(FlowValidateRequest{})},
	}
	for _, test := range tests {
		if got := test.operation.RequestBody.SchemaRef; got != test.wantRef {
			t.Errorf("request schema ref = %q, want %q", got, test.wantRef)
		}
		schema := SchemaFromType(test.typeOf)
		if got, ok := schema["additionalProperties"].(bool); !ok || got {
			t.Errorf("schema %s additionalProperties = %#v, want false", test.typeOf.Name(), schema["additionalProperties"])
		}
	}

	if got := spec.Paths["/flows"].POST.Responses["201"].SchemaRef; got != "#/components/schemas/Flow" {
		t.Errorf("create response schema ref = %q, want Flow", got)
	}
	if got := spec.Paths["/flows/{id}"].PUT.Responses["200"].SchemaRef; got != "#/components/schemas/Flow" {
		t.Errorf("update response schema ref = %q, want Flow", got)
	}
}

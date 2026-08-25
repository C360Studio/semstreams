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
	for _, operation := range []struct {
		name string
		spec *OperationSpec
		want string
	}{
		{"create", spec.Paths["/flows"].POST, "#/components/schemas/FlowCreateRequest"},
		{"update", spec.Paths["/flows/{id}"].PUT, "#/components/schemas/FlowUpdateRequest"},
		{"validate draft", spec.Paths["/flows/{id}/validate"].POST, "#/components/schemas/Flow"},
	} {
		if got := operation.spec.RequestBody.SchemaRef; got != operation.want {
			t.Errorf("%s request schema ref = %q, want %q", operation.name, got, operation.want)
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

	// GET /flows declares its response body: the list is a required, non-null
	// array of the Flow object schema.
	if got := spec.Paths["/flows"].GET.Responses["200"].SchemaRef; got != "#/components/schemas/FlowListResponse" {
		t.Errorf("list response schema ref = %q, want FlowListResponse", got)
	}
	registeredResponse := make(map[reflect.Type]bool, len(spec.ResponseTypes))
	for _, responseType := range spec.ResponseTypes {
		registeredResponse[responseType] = true
	}
	if !registeredResponse[reflect.TypeOf(FlowListResponse{})] {
		t.Error("ResponseTypes does not carry FlowListResponse — its SchemaRef would dangle")
	}

	listSchema := SchemaFromType(reflect.TypeOf(FlowListResponse{}))
	required, _ := listSchema["required"].([]string)
	if len(required) != 1 || required[0] != "flows" {
		t.Errorf("FlowListResponse required = %v, want exactly [flows]", listSchema["required"])
	}
	listProperties, ok := listSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("FlowListResponse schema has no properties: %#v", listSchema)
	}
	flowsSchema, ok := listProperties["flows"].(map[string]any)
	if !ok {
		t.Fatalf("FlowListResponse has no flows property: %#v", listProperties)
	}
	if got := flowsSchema["type"]; got != "array" {
		t.Errorf("flows type = %v, want array", got)
	}
	items, ok := flowsSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("flows has no items schema: %#v", flowsSchema)
	}
	if _, nullable := items["anyOf"]; nullable {
		t.Errorf("flows items declare a null alternative: %#v", items)
	}
	itemProperties, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("flows items is not an object schema: %#v", items)
	}
	for _, name := range []string{"id", "name", "version", "nodes", "connections"} {
		if _, ok := itemProperties[name]; !ok {
			t.Errorf("flows item schema lost field %q", name)
		}
	}
}

// TestFlowUpdateRequestSchemaOmitsServerAuditFields pins the request/response
// schema split: the server owns version and the audit timestamps, so neither
// request body declares them, while Flow (the response, and the validate-draft
// request) keeps them.
func TestFlowUpdateRequestSchemaOmitsServerAuditFields(t *testing.T) {
	propertyNames := func(schema map[string]any) map[string]bool {
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("schema has no properties: %#v", schema)
		}
		names := make(map[string]bool, len(properties))
		for name := range properties {
			names[name] = true
		}
		return names
	}
	requiredSet := func(schema map[string]any) map[string]bool {
		required, _ := schema["required"].([]string)
		set := make(map[string]bool, len(required))
		for _, name := range required {
			set[name] = true
		}
		return set
	}
	sameSet := func(got map[string]bool, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for _, name := range want {
			if !got[name] {
				return false
			}
		}
		return true
	}

	updateSchema := SchemaFromType(reflect.TypeOf(FlowUpdateRequest{}))
	updateProperties := propertyNames(updateSchema)
	for _, name := range []string{"created_at", "updated_at", "last_modified"} {
		if updateProperties[name] {
			t.Errorf("FlowUpdateRequest declares server-owned field %q", name)
		}
	}
	if got := requiredSet(updateSchema); !sameSet(got, "id", "version", "name", "nodes", "connections") {
		t.Errorf("FlowUpdateRequest required = %v, want exactly id/version/name/nodes/connections", updateSchema["required"])
	}
	for _, name := range []string{"description", "created_by"} {
		if !updateProperties[name] {
			t.Errorf("FlowUpdateRequest lost optional field %q", name)
		}
	}

	createSchema := SchemaFromType(reflect.TypeOf(FlowCreateRequest{}))
	createProperties := propertyNames(createSchema)
	for _, name := range []string{"version", "created_at", "updated_at", "last_modified"} {
		if createProperties[name] {
			t.Errorf("FlowCreateRequest declares server-owned field %q", name)
		}
	}
	if got := requiredSet(createSchema); !sameSet(got, "name", "nodes", "connections") {
		t.Errorf("FlowCreateRequest required = %v, want exactly name/nodes/connections", createSchema["required"])
	}
	for _, name := range []string{"id", "description", "created_by"} {
		if !createProperties[name] {
			t.Errorf("FlowCreateRequest lost optional field %q", name)
		}
	}

	spec := flowServiceOpenAPISpec()
	registered := make(map[reflect.Type]bool, len(spec.RequestBodyTypes))
	for _, bodyType := range spec.RequestBodyTypes {
		registered[bodyType] = true
	}
	for _, want := range []reflect.Type{
		reflect.TypeOf(FlowCreateRequest{}),
		reflect.TypeOf(FlowUpdateRequest{}),
		reflect.TypeOf(flowstore.Flow{}),
	} {
		if !registered[want] {
			t.Errorf("RequestBodyTypes does not carry %s — its SchemaRef would dangle", want.Name())
		}
	}
	if got := spec.Paths["/flows"].POST.RequestBody.SchemaRef; got != "#/components/schemas/FlowCreateRequest" {
		t.Errorf("POST /flows request schema ref = %q, want FlowCreateRequest", got)
	}
	if got := spec.Paths["/flows/{id}"].PUT.RequestBody.SchemaRef; got != "#/components/schemas/FlowUpdateRequest" {
		t.Errorf("PUT /flows/{id} request schema ref = %q, want FlowUpdateRequest", got)
	}
}

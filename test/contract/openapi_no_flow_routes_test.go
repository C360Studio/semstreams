package contract

import (
	"strings"
	"testing"
)

// retiredFlowPaths is the path set the removed flow-builder declaration
// contributed to the published document, enumerated from the owning
// declaration (`flowServiceOpenAPISpec`, `service/flow_service.go:206-236`)
// before its deletion. The generated document merges service fragments without
// their mount prefix, so these are the literals a client sees under
// `/flowbuilder`.
var retiredFlowPaths = []string{
	"/flows",
	"/flows/{id}",
	"/flows/{id}/validate",
	"/flows/{id}/publish-component-configs",
	"/flows/{id}/observations/health",
	"/flows/{id}/observations/metrics",
	"/flows/{id}/observations/messages",
}

// retiredFlowSchemas is the schema set the same declaration contributed
// (ResponseTypes + RequestBodyTypes). A downstream client generator keys off
// these names, so a leftover schema is a published surface even with no path.
var retiredFlowSchemas = []string{
	"Flow",
	"FlowCreateRequest",
	"FlowUpdateRequest",
	"FlowListResponse",
	"RuntimeHealthResponse",
	"RuntimeMetricsResponse",
	"RuntimeMessagesResponse",
	"publishComponentConfigsResponse",
}

// TestOpenAPIHasNoFlowRoutes is the published-surface half of ADR-100 D5. The
// service-registry guard proves nothing constructs a flow-builder; this proves
// nothing advertises one, which is the artifact adopters generate clients from.
func TestOpenAPIHasNoFlowRoutes(t *testing.T) {
	spec := loadOpenAPISpec(t)

	for _, path := range retiredFlowPaths {
		if _, ok := spec.Paths[path]; ok {
			t.Errorf("specs/openapi.v3.yaml still publishes %q; ADR-100 D5 removes it without an alias", path)
		}
	}
	for path := range spec.Paths {
		if strings.HasPrefix(path, "/flowbuilder") {
			t.Errorf("specs/openapi.v3.yaml still publishes a /flowbuilder path: %q", path)
		}
	}
	for _, schema := range retiredFlowSchemas {
		if _, ok := spec.Components.Schemas[schema]; ok {
			t.Errorf("specs/openapi.v3.yaml still publishes schema %q; it left with the flow-builder service", schema)
		}
	}
}

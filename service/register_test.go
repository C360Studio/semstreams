package service

import (
	"testing"
)

// retiredFlowRoutes is the routed/advertised surface ADR-100 decision D5
// removes, enumerated from the declaration that owned it
// (`flowServiceOpenAPISpec`, `service/flow_service.go`) before deletion. It is
// spelled here — not derived from the live registry — because a guard derived
// from the surface it guards passes vacuously once the surface is gone.
var retiredFlowRoutes = []string{
	"/flows",
	"/flows/{id}",
	"/flows/{id}/validate",
	"/flows/{id}/publish-component-configs",
	"/flows/{id}/observations/health",
	"/flows/{id}/observations/metrics",
	"/flows/{id}/observations/messages",
}

// TestServiceRegistryHasNoFlowBuilder is the absence guard for ADR-100 D5:
// the framework registers no flow-builder service, so no process can compose
// one and no route it owned can be served. It also asserts the advertised
// surface, because a service can contribute OpenAPI rows through the package
// registry (`RegisterOpenAPISpec`) from an init() that no service registry
// entry mentions — deleting the constructor without deleting the declaration
// would leave the routes published and unserved.
func TestServiceRegistryHasNoFlowBuilder(t *testing.T) {
	registry := NewServiceRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"flow-builder", "flow-service", "flowbuilder"} {
		if _, exists := registry.Constructor(name); exists {
			t.Errorf("service registry still offers %q; ADR-100 D5 removes the flow-builder service without an alias", name)
		}
	}

	specs := GetAllOpenAPISpecs()
	if _, ok := specs["flow-service"]; ok {
		t.Error(`GetAllOpenAPISpecs still carries the "flow-service" declaration; its init() registration must go with the service`)
	}
	for name, spec := range specs {
		if spec == nil {
			continue
		}
		for _, route := range retiredFlowRoutes {
			if _, ok := spec.Paths[route]; ok {
				t.Errorf("service %q still declares retired route %q", name, route)
			}
		}
	}
}

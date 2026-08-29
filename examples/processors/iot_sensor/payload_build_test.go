package iotsensor

import (
	"testing"

	"github.com/c360studio/semstreams/payloadregistry"
)

// TestRegisteredExamplesBuildThroughTheRegistry drives the production
// Registry.Build over each registration's own Example map.
//
// It exists because a Builder must key on the WIRE name, not the Go field
// name: Build's documented fallback when no Builder is supplied is a JSON
// round-trip through Factory, so the two paths must agree on the same keys.
// Every field on these two types is untagged, which makes the Go name and the
// wire name identical — except EntityIDValue, which carries `json:"entity_id"`.
// That single divergence is invisible to the compiler and silently yields an
// empty entity ID, which graph-ingest rejects as "graphable payload returned
// empty entity ID". Building the registered Example is the cheapest place to
// catch it.
func TestRegisteredExamplesBuildThroughTheRegistry(t *testing.T) {
	registry := payloadregistry.NewWithSubset(t, RegisterPayloads)

	for _, registration := range registry.List() {
		t.Run(registration.MessageType(), func(t *testing.T) {
			built, err := registry.Build(
				registration.Domain, registration.Category, registration.Version, registration.Example)
			if err != nil {
				t.Fatalf("Build from the registered Example failed: %v", err)
			}
			identified, ok := built.(interface{ EntityID() string })
			if !ok {
				t.Fatalf("built %T does not expose EntityID()", built)
			}
			if identified.EntityID() == "" {
				t.Error("built payload has an empty entity ID; the Example key and the wire key disagree")
			}
		})
	}
}

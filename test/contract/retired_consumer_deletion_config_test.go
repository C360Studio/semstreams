package contract

import (
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestPublishedSchemasDoNotAdvertiseLifecycleConsumerDeletion(t *testing.T) {
	registry := component.NewRegistry()
	if err := registerPublishedComposition(registry); err != nil {
		t.Fatalf("register published composition: %v", err)
	}

	for _, componentName := range []string{
		"otel-exporter",
		"agentic-dispatch",
		"agentic-loop",
		"agentic-model",
		"agentic-tools",
	} {
		t.Run(componentName, func(t *testing.T) {
			schema, err := registry.GetComponentSchema(componentName)
			if err != nil {
				t.Fatalf("GetComponentSchema(%q): %v", componentName, err)
			}
			if _, exists := schema.Properties["delete_consumer_on_stop"]; exists {
				t.Error("schema advertises retired lifecycle consumer deletion")
			}
		})
	}
}

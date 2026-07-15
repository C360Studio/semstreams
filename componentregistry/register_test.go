package componentregistry_test

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
)

func TestRegisterCoreExcludesUnselectedCapabilities(t *testing.T) {
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{
		"github_webhook", "oasf-generator", "directory-bridge", "a2a-adapter",
		"slim-bridge", "otel-exporter", "research-graph-classify",
		"research-graph-route", "research-graph-execute", "research-graph-assess",
		"research-graph-synthesize",
	} {
		if _, ok := registry.GetFactory(name); ok {
			t.Errorf("core registry unexpectedly contains %q", name)
		}
	}

	for _, name := range []string{"graph-ingest", "graph-index", "graph-query", "rule-processor", "agentic-loop"} {
		if _, ok := registry.GetFactory(name); !ok {
			t.Errorf("core registry is missing %q", name)
		}
	}
}

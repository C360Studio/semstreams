package main

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
)

func TestRegisterPayloadsAddsResearchOnlyWhenSelected(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		registry, err := registerPayloads(&config.Config{})
		if err != nil {
			t.Fatalf("registerPayloads: %v", err)
		}
		if _, ok := registry.GetRegistration("research.intent.v1"); ok {
			t.Fatal("research.intent.v1 registered without graph research selection")
		}
	})

	t.Run("selected", func(t *testing.T) {
		componentConfig, err := json.Marshal(map[string]any{
			"allowed_tools": []string{"research_graph"},
		})
		if err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{Components: config.ComponentConfigs{
			"agentic-tools": {
				Name:    "agentic-tools",
				Type:    types.ComponentTypeProcessor,
				Enabled: true,
				Config:  componentConfig,
			},
		}}

		registry, err := registerPayloads(cfg)
		if err != nil {
			t.Fatalf("registerPayloads: %v", err)
		}
		if _, ok := registry.GetRegistration("research.intent.v1"); !ok {
			t.Fatal("research.intent.v1 missing when graph research is selected")
		}
	})
}

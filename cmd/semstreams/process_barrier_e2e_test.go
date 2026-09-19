//go:build e2e_process_barrier

package main

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/config"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/test/e2e/harness/processbarrier"
	"github.com/c360studio/semstreams/types"
)

func TestPrepareE2EProcessBarrierConfigAddsTaggedToolOnce(t *testing.T) {
	toolConfig := agentictools.Config{Timeout: "10s", AllowedTools: []string{"query_entity"}}
	wire, err := json.Marshal(toolConfig)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Components: config.ComponentConfigs{
		"agentic-tools": {
			Type: types.ComponentTypeProcessor, Name: "agentic-tools", Enabled: true, Config: wire,
		},
	}}

	if err := prepareE2EProcessBarrierConfig(cfg); err != nil {
		t.Fatalf("first overlay: %v", err)
	}
	if err := prepareE2EProcessBarrierConfig(cfg); err != nil {
		t.Fatalf("second overlay: %v", err)
	}

	var got agentictools.Config
	if err := json.Unmarshal(cfg.Components["agentic-tools"].Config, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.AllowedTools) != 2 || got.AllowedTools[0] != "query_entity" ||
		got.AllowedTools[1] != processbarrier.ToolName {
		t.Fatalf("allowed tools = %v, want query_entity plus one process barrier", got.AllowedTools)
	}
}

func TestPrepareE2EProcessBarrierConfigIgnoresMissingComponent(t *testing.T) {
	if err := prepareE2EProcessBarrierConfig(&config.Config{}); err != nil {
		t.Fatalf("missing agentic-tools component: %v", err)
	}
}

func TestPrepareE2EProcessBarrierConfigRejectsMalformedEnabledComponent(t *testing.T) {
	cfg := &config.Config{Components: config.ComponentConfigs{
		"agentic-tools": {Enabled: true, Config: json.RawMessage(`{`)},
	}}
	if err := prepareE2EProcessBarrierConfig(cfg); err == nil {
		t.Fatal("malformed enabled agentic-tools component accepted")
	}
}

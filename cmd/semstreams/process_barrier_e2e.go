//go:build e2e_process_barrier

package main

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/test/e2e/harness/processbarrier"
)

// prepareE2EProcessBarrierConfig is a compile-time E2E overlay. It is absent
// from ordinary binaries and keeps the shipped agentic quickstart allowlist
// free of test-only tools.
func prepareE2EProcessBarrierConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	componentConfig, ok := cfg.Components["agentic-tools"]
	if !ok || !componentConfig.Enabled {
		return nil
	}
	var toolConfig map[string]json.RawMessage
	if err := json.Unmarshal(componentConfig.Config, &toolConfig); err != nil {
		return fmt.Errorf("decode agentic-tools config: %w", err)
	}
	allowedWire, declared := toolConfig["allowed_tools"]
	if !declared || string(allowedWire) == "null" {
		return nil // An absent/null allowlist already admits every registered tool.
	}
	var allowedTools []string
	if err := json.Unmarshal(allowedWire, &allowedTools); err != nil {
		return fmt.Errorf("decode agentic-tools allowed_tools: %w", err)
	}
	if len(allowedTools) == 0 {
		return nil // The component contract treats an empty list as allow-all.
	}
	for _, name := range allowedTools {
		if name == processbarrier.ToolName {
			return nil
		}
	}
	allowedWire, err := json.Marshal(append(allowedTools, processbarrier.ToolName))
	if err != nil {
		return fmt.Errorf("encode agentic-tools allowed_tools: %w", err)
	}
	toolConfig["allowed_tools"] = allowedWire
	wire, err := json.Marshal(toolConfig)
	if err != nil {
		return fmt.Errorf("encode agentic-tools config: %w", err)
	}
	componentConfig.Config = wire
	cfg.Components["agentic-tools"] = componentConfig
	return nil
}

func registerE2EProcessBarrier(registry *agentictools.ExecutorRegistry, client *natsclient.Client) error {
	return processbarrier.Register(registry, client)
}

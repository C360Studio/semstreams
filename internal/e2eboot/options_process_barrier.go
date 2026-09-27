package e2eboot

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/boot"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
	"github.com/c360studio/semstreams/test/e2e/harness/processbarrier"
)

// enableProcessBarrier registers the agentic tier's process-barrier tool
// executor and admits it through the agentic-tools allowlist of the loaded
// configuration, so the shipped agentic config never names a test-only tool.
func enableProcessBarrier(opts *boot.Options, _ string) {
	opts.ConfigPatches = append(opts.ConfigPatches, prepareE2EProcessBarrierConfig)
	opts.Tools = append(opts.Tools, registerE2EProcessBarrier)
}

// prepareE2EProcessBarrierConfig adds the barrier tool to an explicit
// agentic-tools allowlist once; an absent, null or empty allowlist already
// admits every registered tool and is left alone.
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

func registerE2EProcessBarrier(
	_ context.Context, registry *agentictools.ExecutorRegistry, deps executors.ToolDependencies,
) error {
	return processbarrier.Register(registry, deps.NATSClient)
}

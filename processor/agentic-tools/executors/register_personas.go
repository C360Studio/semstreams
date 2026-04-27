package executors

import (
	"fmt"
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// Persona CRUD tool names. Single source of truth for publish_agent.tools
// scoping.
const (
	personaToolCreate = "create_persona"
	personaToolUpdate = "update_persona"
	personaToolDelete = "delete_persona"
	personaToolList   = "list_personas"
	personaToolGet    = "get_persona"
)

// registerPersonas wires PersonaExecutor. A nil manager is a deployment
// choice (skip + nil); a registry-level failure (duplicate name)
// propagates so RegisterBuiltins can surface it at boot.
func registerPersonas(tools *agentictools.ExecutorRegistry, manager PersonaManager, logger *slog.Logger) error {
	if manager == nil {
		logger.Debug("persona CRUD tools disabled: no PersonaManager provided")
		return nil
	}

	executor := NewPersonaExecutor(manager)
	toolNames := []string{
		personaToolCreate,
		personaToolUpdate,
		personaToolDelete,
		personaToolList,
		personaToolGet,
	}
	for _, name := range toolNames {
		if err := tools.RegisterTool(name, executor); err != nil {
			return fmt.Errorf("register persona tool %q: %w", name, err)
		}
	}
	logger.Info("Registered persona CRUD tools",
		slog.Int("count", len(toolNames)))
	return nil
}

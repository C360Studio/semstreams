package executors

import (
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

// registerPersonas wires PersonaExecutor globally. Nil manager = skip,
// same pattern as registerRules / registerFlows.
func registerPersonas(tools *agentictools.ExecutorRegistry, manager PersonaManager, logger *slog.Logger) {
	if manager == nil {
		logger.Debug("persona CRUD tools disabled: no PersonaManager provided")
		return
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
			logger.Warn("Failed to register persona tool",
				slog.String("tool", name),
				slog.Any("error", err))
			return
		}
	}
	logger.Info("Registered persona CRUD tools (global)",
		slog.Int("count", len(toolNames)))
}

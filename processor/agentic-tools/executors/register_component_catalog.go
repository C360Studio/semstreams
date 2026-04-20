package executors

import (
	"log/slog"

	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerComponentCatalog wires ComponentCatalogExecutor globally. Nil
// registry skips registration (Pattern-B step 5: nil → tool silently
// disabled so flows that don't need it pay nothing).
func registerComponentCatalog(reg *component.Registry, logger *slog.Logger) {
	if reg == nil {
		logger.Warn("list_components tool disabled: no ComponentRegistry provided")
		return
	}

	executor := agentictools.NewComponentCatalogExecutor(reg, logger)
	if err := registerGlobal(agentictools.ComponentCatalogToolName, executor); err != nil {
		logger.Warn("Failed to register list_components tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered list_components tool (global)")
}

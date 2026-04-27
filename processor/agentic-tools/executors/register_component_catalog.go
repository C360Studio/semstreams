package executors

import (
	"log/slog"

	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerComponentCatalog wires ComponentCatalogExecutor into the
// supplied tool registry. Nil component registry skips registration
// (Pattern-B step 5: nil → tool silently disabled so flows that don't
// need it pay nothing).
func registerComponentCatalog(tools *agentictools.ExecutorRegistry, compReg *component.Registry, logger *slog.Logger) {
	if compReg == nil {
		logger.Warn("list_components tool disabled: no ComponentRegistry provided")
		return
	}

	executor := agentictools.NewComponentCatalogExecutor(compReg, logger)
	if err := tools.RegisterTool(agentictools.ComponentCatalogToolName, executor); err != nil {
		logger.Warn("Failed to register list_components tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered list_components tool")
}

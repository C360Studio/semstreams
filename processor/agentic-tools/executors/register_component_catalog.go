package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerComponentCatalog wires ComponentCatalogExecutor into the
// supplied tool registry. A nil component registry is a deployment
// choice — the tool silently disables so flows that don't need it pay
// nothing. A registry-level failure (duplicate name) propagates so
// RegisterBuiltins can surface it at boot.
func registerComponentCatalog(tools *agentictools.ExecutorRegistry, compReg *component.Registry, logger *slog.Logger) error {
	if compReg == nil {
		logger.Warn("list_components tool disabled: no ComponentRegistry provided")
		return nil
	}

	executor := agentictools.NewComponentCatalogExecutor(compReg, logger)
	if err := tools.RegisterTool(agentictools.ComponentCatalogToolName, executor); err != nil {
		return fmt.Errorf("register list_components: %w", err)
	}
	logger.Info("Registered list_components tool")
	return nil
}

package agenticmodel

import (
	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the agentic-model processor component with the given registry
func Register(registry RegistryInterface) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:         "agentic-model",
		Factory:      NewComponent,
		Ports:        DeclarePorts,
		Schema:       agenticModelSchema,
		Type:         "processor",
		Protocol:     "openai",
		Domain:       "semantic",
		Description:  "OpenAI-compatible agentic model processor with tool calling support",
		Version:      "0.1.0",
		Dependencies: []string{component.DepModelRegistry},
	})
}

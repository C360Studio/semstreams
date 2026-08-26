package document

import "github.com/c360studio/semstreams/component"

// Register registers the document processor component with the given registry.
// This enables the component to be discovered and instantiated by the component
// management system.
//
// The registration includes:
//   - Component factory function for creating instances
//   - Configuration schema for validation and UI generation
//   - Type information (processor, domain: content)
//   - Protocol identifier for component routing
//   - Version information for compatibility tracking
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "document_processor",
		Factory:     NewComponent,
		Ports:       DeclarePorts,
		Schema:      documentSchema,
		Type:        "processor",
		Protocol:    "document",
		Domain:      "content",
		Description: "Transforms incoming JSON documents into Graphable payloads for semantic search",
		Version:     "0.1.0",
	})
}

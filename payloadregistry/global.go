package payloadregistry

// global is the package-level payload registry singleton. Preserved
// from the previous component-package implementation so existing
// init()-based registration callers (agentic, message, processor/*)
// keep working through this refactor. The singleton itself is
// scheduled for removal in a follow-up — see ADR-029 / the v1
// registry-cleanup punch list — at which point each consumer will
// receive an injected *Registry instance instead.
var global = New()

// Register registers a payload factory globally.
// Convenience wrapper around the package-level Registry.
// Payloads use init() registration as they are data types, not lifecycle components.
//
// New code should prefer creating an instance via New() and dependency-
// injecting it; this global is preserved for backward compatibility.
func Register(registration *Registration) error {
	return global.Register(registration)
}

// Create creates a payload instance using the globally registered factory.
// Returns nil if no factory is registered for the given type.
// Used by message.BaseMessage.UnmarshalJSON for typed deserialization.
func Create(domain, category, version string) any {
	return global.Create(domain, category, version)
}

// Build creates a typed payload from field mappings using the globally
// registered builder. Returns an error if the payload type is not
// registered or if the builder fails. Used by workflow variable
// interpolation to construct typed payloads from step output maps.
func Build(domain, category, version string, fields map[string]any) (any, error) {
	return global.Build(domain, category, version, fields)
}

// Global returns the global payload registry. Useful for introspection,
// such as listing all registered payloads.
func Global() *Registry {
	return global
}

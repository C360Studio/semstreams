// Package flowengine validates and compiles saved flow diagrams.
//
// Engine.ValidateFlowDefinition checks diagram structure, component factory
// declarations, ports, and connections. Engine.Compile validates first, rejects
// duplicate component instance names, and returns detached enabled
// config.ComponentConfigs candidates.
//
// The Engine owns no component or service lifecycle. Compilation alone has no
// runtime or persistence effect. Callers that intentionally publish candidates
// use the configuration manager's explicit per-component persistence seam; the
// current process continues to run its sealed boot component map.
package flowengine

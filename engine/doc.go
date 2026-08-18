// Package flowengine validates saved flow diagrams and compiles them into
// component-configuration candidates.
//
// A Flow is authoring state, not a runtime lifecycle record. Engine therefore
// has no deploy, start, stop, or undeploy operation. ValidateFlowDefinition
// checks the saved graph against the component factory declarations, while
// Compile returns a detached component-config map. Neither operation changes
// configuration or a running process.
//
// The service layer exposes explicit publication of compiled entries. That
// operation performs sorted, retry-safe upserts through config.Manager and
// reports exact partial progress. Published values become effective only when
// a later process boot composes components from configuration.
package flowengine

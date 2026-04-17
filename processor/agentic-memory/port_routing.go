package agenticmemory

import "context"

// handlerForPort returns the registered handler for the named input port and
// whether the port is recognized. Extracted from setupInputConsumers so the
// routing table is independently testable.
func (c *Component) handlerForPort(name string) (func(context.Context, []byte), bool) {
	switch name {
	case "compaction_events":
		return c.handleCompactionEvent, true
	case "hydrate_requests":
		return c.handleHydrateRequest, true
	case "layer_approved_events":
		return c.handleLayerApproved, true
	case "loop_created_events":
		return c.handleLoopCreated, true
	default:
		return nil, false
	}
}

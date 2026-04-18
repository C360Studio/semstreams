package agentictools

import "log/slog"

// registerDecide wires the coordinator's decide terminal tool. The tool
// publishes triples via the graph.mutation.triple.add NATS surface (same
// path rule actions use) so no extra infrastructure is needed beyond the
// natsClient already held by the component. Registered globally for the
// same LLM-advertisement reason described on registerReadLoopResult.
func (c *Component) registerDecide() {
	publisher := NewNATSTriplePublisher(c.natsClient)
	executor := NewDecideExecutor(publisher, c.platform)
	if err := registerGlobalTool(decideToolName, executor); err != nil {
		c.logger.Warn("Failed to register decide tool",
			slog.Any("error", err))
		return
	}
	c.logger.Info("Registered decide tool (global)",
		slog.String("org", c.platform.Org),
		slog.String("platform", c.platform.Platform))
}

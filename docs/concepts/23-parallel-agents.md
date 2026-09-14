# Parallel Agent Execution

> **Retired implementation guide.** `processor/reactive` and its workflow builder APIs were removed.
> This page preserves existing links; its former examples are not a path for new development.

Fan-out remains a supported composition pattern: `for_each` dispatches an agent task per item; the receiving
components and their configured capacity determine execution concurrency. A join needs explicit completion and
failure policy. The retired workflow engine is not required for these patterns.

Use [Orchestration Layers](14-orchestration-layers.md) for single triggers, pipelines, branches,
bounded iteration and fan-out/join. Rules trigger work; components execute; lifecycle describes named entity phases.
[Phased Agentic Chains](25-phased-agentic-chains.md) explains role and phase composition.

For the previous implementation, consult the [historical page](https://github.com/C360Studio/semstreams/blob/ea22e6a4e75d12bf7f6050c6d8121de1d089b191/docs/concepts/23-parallel-agents.md).

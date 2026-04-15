# Agentic Systems

Agentic systems are LLM-powered autonomous task executors that can use tools and iterate to complete complex goals.

## Optional Capability

The agentic components are an **optional extension** to SemStreams. The core system — event ingestion,
knowledge graph, indexing, queries, and rules — works without any agentic components deployed.

Add agentic capabilities when you need:

- Autonomous task execution with tool use
- LLM-powered reasoning over your knowledge graph
- Multi-step workflows that adapt based on results

Skip them when:

- Your workload is pure data processing (ingest, transform, query)
- You don't need LLM integration
- You want minimal operational complexity

The agentic components integrate with the existing graph and rule infrastructure but don't modify it — they're
consumers like any other application.

## What is an Agentic System?

Traditional LLM applications follow a simple request-response pattern: you send a prompt, you get a completion.
Agentic systems go further by enabling the LLM to:

1. **Decide what actions to take** based on the current situation
2. **Execute tools** to interact with external systems
3. **Observe results** and incorporate them into reasoning
4. **Iterate** until the task is complete or a stopping condition is met

This transforms LLMs from passive responders into active problem solvers.

## The Agentic Loop

At the heart of every agentic system is the **agentic loop**:

```text
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│    Task ──▶ Model ──▶ Tool Calls ──▶ Results ──▶ Model     │
│                           │                        │        │
│                           └────────────────────────┘        │
│                              (loop until complete)          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Typical flow:**

1. User submits a task (e.g., "Review main.go for security issues")
2. Model analyzes task, decides it needs to read the file
3. Model requests tool execution (read_file with path main.go)
4. Tool executes, returns file contents
5. Model analyzes contents, finds issues
6. Model either requests more tools or returns final answer
7. Loop completes when model indicates task is done

## Why Agentic Systems Matter

**Complex tasks require iteration.** Many real-world tasks can't be solved in a single LLM call:

- Code review requires reading multiple files
- Research requires searching, reading, synthesizing
- Data analysis requires querying, computing, visualizing

**Tools extend capabilities.** LLMs can reason but can't act. Tools let them:

- Read and write files
- Query databases
- Call APIs
- Execute code

**Autonomous execution scales.** Instead of humans orchestrating every step, the agent decides what to do next.
This enables:

- Batch processing of complex tasks
- 24/7 operation without human intervention
- Handling tasks that would be tedious for humans

## Core Concepts

### State Machine

Agentic systems use a state machine to track progress through well-defined phases:

```text
┌───────────┐   ┌──────────┐   ┌─────────────┐   ┌───────────┐   ┌───────────┐
│ exploring │──▶│ planning │──▶│ architecting│──▶│ executing │──▶│ reviewing │
└───────────┘   └──────────┘   └─────────────┘   └───────────┘   └─────┬─────┘
      ▲               ▲               ▲                ▲               │
      │               │               │                │               │
      └───────────────┴───────────────┴────────────────┘               │
                   (fluid backward transitions)                         │
                                                                        ▼
                                                    ┌───────────────────────────┐
                                                    │complete│failed│cancelled  │
                                                    ├───────────────────────────┤
                                                    │paused │ awaiting_approval │
                                                    └───────────────────────────┘
```

**States:**

| State | Terminal | Description |
|-------|----------|-------------|
| `exploring` | No | Initial state, gathering information |
| `planning` | No | Developing approach |
| `architecting` | No | Designing solution |
| `executing` | No | Implementing solution |
| `reviewing` | No | Validating results |
| `complete` | Yes | Successfully finished |
| `failed` | Yes | Failed due to error or max iterations |
| `cancelled` | Yes | Cancelled by user signal |
| `paused` | No | Paused by user signal, can resume |
| `awaiting_approval` | No | Waiting for user approval |

**Why states matter:**

- **Checkpointing**: Can resume from interruptions
- **Observability**: Know where the agent is in its process
- **Control**: Can intervene at specific states
- **Debugging**: Understand where things went wrong

SemStreams uses **fluid states** — the agent can move backward (e.g., from executing back to exploring) when it
needs to rethink. Only terminal states (complete, failed, cancelled) are final.

### Signal Handling

Users can send control signals to affect running loops:

```text
┌─────────────────────────────────────────────────────────────┐
│                     Signal Types                             │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  cancel  ──▶  Stop execution immediately (→ cancelled)      │
│  pause   ──▶  Pause at next checkpoint (→ paused)           │
│  resume  ──▶  Continue paused loop (→ previous state)       │
│  approve ──▶  Approve pending result (→ complete)           │
│  reject  ──▶  Reject with reason (→ failed)                 │
│  retry   ──▶  Retry failed loop (→ exploring)               │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

Signals are published to `agent.signal.{loop_id}` and processed by the loop orchestrator.

### Tool Abstraction

Tools are the agent's interface to the outside world. The tool system has three parts:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                         Tool System                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐  │
│  │ Tool Definition │    │    Tool Call    │    │   Tool Result   │  │
│  ├─────────────────┤    ├─────────────────┤    ├─────────────────┤  │
│  │ • name          │    │ • call ID       │    │ • call ID       │  │
│  │ • description   │───▶│ • tool name     │───▶│ • content       │  │
│  │ • parameters    │    │ • arguments     │    │ • error (if any)│  │
│  │   (schema)      │    │                 │    │                 │  │
│  └─────────────────┘    └─────────────────┘    └─────────────────┘  │
│                                                                      │
│       What the tool       Agent's request        Tool's response    │
│        can do             to use the tool        after execution    │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

**Tool Definition** describes what a tool can do — its name, a natural language description, and a schema
defining what parameters it accepts. The model uses this information to decide which tools to call and how.

**Tool Call** is the agent's request to execute a tool. It includes a unique call ID (for tracking), the tool
name, and the arguments matching the parameter schema.

**Tool Result** is the response after execution. It references the call ID, contains the output content (file
contents, query results, etc.), and an error field if something went wrong.

### Context Management

Long-running loops can exceed model token limits. The context manager handles this automatically:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                    Context Regions (by priority)                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  5. system_prompt      Never evicted                                 │
│  4. compacted_history  Summarized old conversation                   │
│  3. hydrated_context   Retrieved from knowledge graph                │
│  2. recent_history     Recent messages                               │
│  1. tool_results       Tool outputs (GC'd by age)                    │
│                                                                      │
│  Lower priority = evicted first when approaching token limit         │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

When context approaches the model's token limit:

1. **Compaction** summarizes older messages into a compressed form
2. **GC** removes tool results older than N iterations
3. **Hydration** recovers relevant context from the knowledge graph

Context events are published to `agent.context.compaction.*` for observability and integration with
agentic-memory.

### Trajectory

A trajectory captures the complete execution path of an agentic loop:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                         Trajectory                                   │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Loop ID: loop_123                                                   │
│  Started: 10:30:00    Ended: 10:31:45    Outcome: complete          │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │ Step 1: Model Call                                          │    │
│  │   Tokens in: 150    Tokens out: 50    Duration: 800ms       │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                              │                                       │
│                              ▼                                       │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │ Step 2: Tool Call (read_file)                               │    │
│  │   Duration: 25ms                                            │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                              │                                       │
│                              ▼                                       │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │ Step 3: Model Call                                          │    │
│  │   Tokens in: 500    Tokens out: 200   Duration: 1200ms      │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                      │
│  Totals: 650 tokens in, 250 tokens out                              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

**Why trajectories matter:**

- **Debugging**: See exactly what happened step by step
- **Cost tracking**: Know token usage for billing and optimization
- **Compliance**: Audit trail for actions taken
- **Analytics**: Understand agent behavior patterns across many executions
- **Training data**: Successful trajectories become examples for fine-tuning or few-shot prompts

## Multi-Agent Patterns

Complex tasks often benefit from multiple specialized agents working together.

### Architect/Editor Split

One pattern separates planning from implementation:

```text
┌─────────────────┐                    ┌─────────────────┐
│    Architect    │                    │     Editor      │
│    (planning)   │                    │  (implementing) │
├─────────────────┤      plan          ├─────────────────┤
│ • Analyzes task │ ─────────────────▶ │ • Receives plan │
│ • Designs soln  │                    │ • Makes changes │
│ • Produces plan │                    │ • Focuses on    │
│                 │                    │   execution     │
└─────────────────┘                    └─────────────────┘
```

This separation enables:

- Different prompting strategies per role
- Clearer responsibility boundaries
- Independent cost and time tracking

The handoff is managed via the `COMPLETE_{loopID}` KV key pattern, which the rules engine can watch to
spawn the editor when the architect completes.

### Parallel Tool Execution

When an agent needs multiple independent pieces of information, tools can execute concurrently:

```text
                    ┌─────────────┐
                    │    Model    │
                    │  requests   │
                    │  3 tools    │
                    └──────┬──────┘
                           │
           ┌───────────────┼───────────────┐
           ▼               ▼               ▼
      ┌─────────┐    ┌─────────┐    ┌─────────┐
      │ Tool A  │    │ Tool B  │    │ Tool C  │
      │ (file)  │    │ (search)│    │ (query) │
      └────┬────┘    └────┬────┘    └────┬────┘
           │              │              │
           └──────────────┼──────────────┘
                          ▼
                    ┌─────────────┐
                    │    Model    │
                    │  receives   │
                    │ all results │
                    └─────────────┘
```

The loop orchestrator tracks pending tools and aggregates results before continuing to the next model call.

### Hierarchical Delegation

For very complex tasks, agents can spawn sub-agents:

```text
                ┌─────────────────────┐
                │    Parent Agent     │
                │  (coordinates work) │
                └──────────┬──────────┘
                           │ spawns
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Sub-Agent│ │ Sub-Agent│ │ Sub-Agent│
        │ (task A) │ │ (task B) │ │ (task C) │
        └──────────┘ └──────────┘ └──────────┘
```

Each sub-agent has its own loop, state, and trajectory — enabling divide-and-conquer for complex problems.
Parent-child relationships are tracked via the `parent_loop_id` field.

## When to Use Agentic Systems

**Good fit:**

- Tasks requiring multiple steps (code review, research)
- Tasks needing external data (file access, API calls)
- Tasks with iterative refinement (debugging, optimization)
- Open-ended goals where steps aren't predetermined

**Poor fit:**

- Simple transformations (just use the LLM directly)
- Hard real-time requirements (agent loops add latency)
- Tasks where errors are catastrophic (agents can fail)
- High-volume, identical tasks (batch processing is cheaper)

## Integration with Knowledge Graph

The agentic system integrates with SemStreams' knowledge graph and rule processor, but these integrations are
**optional enhancements** — the agentic components work standalone without them.

### Agentic System is Self-Contained

The agentic-loop manages its own state machine internally. State transitions happen based on model responses
(tool_call, complete, error) without any external dependencies:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                    Self-Contained Loop Orchestration                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   Model Response                     agentic-loop                    │
│   ─────────────                      ───────────                     │
│   status: tool_call  ─────────────▶  Dispatch tools, track pending  │
│   status: complete   ─────────────▶  Mark complete, save trajectory │
│   status: error      ─────────────▶  Mark failed                    │
│                                                                      │
│   All Tools Complete ─────────────▶  Increment iteration, continue  │
│   Max Iterations     ─────────────▶  Mark failed                    │
│   User Signal        ─────────────▶  Handle cancel/pause/resume     │
│                                                                      │
│   No rules required. No external state machine driver.              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Optional: Loop Entities in the Graph

Agent loops are stored in NATS KV (`AGENT_LOOPS`) as queryable entities:

```text
┌─────────────────────────────────────────────┐
│ Key: loop_123                               │
├─────────────────────────────────────────────┤
│ id             = "loop_123"                 │
│ task_id        = "task_456"                 │
│ state          = "executing"                │
│ role           = "general"                  │
│ model          = "gpt-4"                    │
│ iterations     = 3                          │
│ max_iterations = 20                         │
│ user_id        = "user_789"                 │
│ channel_type   = "cli"                      │
│ parent_loop_id = ""                         │
└─────────────────────────────────────────────┘
```

On completion, an enriched state is also written to `COMPLETE_{loopID}`:

```text
┌─────────────────────────────────────────────┐
│ Key: COMPLETE_loop_123                      │
├─────────────────────────────────────────────┤
│ loop_id     = "loop_123"                    │
│ task_id     = "task_456"                    │
│ outcome     = "success"                     │
│ role        = "architect"                   │
│ result      = "Designed auth system..."     │
│ model       = "gpt-4"                       │
│ iterations  = 3                             │
│ parent_loop = ""                            │
└─────────────────────────────────────────────┘
```

This `COMPLETE_*` pattern enables rules-based orchestration without tight coupling.

### Optional: Trajectory Storage

Trajectories are stored in NATS KV (`AGENT_TRAJECTORIES`) on loop completion:

```text
┌─────────────────────────────────────────────┐
│ Key: loop_123                               │
├─────────────────────────────────────────────┤
│ loop_id        = "loop_123"                 │
│ start_time     = "2024-01-15T10:30:00Z"     │
│ end_time       = "2024-01-15T10:31:45Z"     │
│ outcome        = "complete"                 │
│ total_tokens_in  = 1500                     │
│ total_tokens_out = 800                      │
│ duration       = 105000 (ms)                │
│ steps          = [...]                      │
└─────────────────────────────────────────────┘
```

### Optional: Rule Processor Integration

The rule processor can observe and react to agent activity, but **does not drive agent behavior**:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                    Rules ↔ Agents Relationship                       │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌──────────────┐                      ┌──────────────┐            │
│   │    Rules     │                      │    Agents    │            │
│   │  (optional)  │                      │ (standalone) │            │
│   └──────┬───────┘                      └──────┬───────┘            │
│          │                                     │                     │
│          │  Can observe ──────────────────────▶│                     │
│          │  (watch COMPLETE_* keys in KV)      │                     │
│          │                                     │                     │
│          │  Can trigger ──────────────────────▶│                     │
│          │  (publish to agent.task.*)          │                     │
│          │                                     │                     │
│          │  Cannot control ───────────────────X│                     │
│          │  (no forced state transitions)      │                     │
│          │                                     │                     │
└─────────────────────────────────────────────────────────────────────┘
```

**Rules can observe agents:**
- Watch `COMPLETE_*` keys in KV bucket for completion
- Fire alerts when iterations exceed thresholds
- Track agent costs and durations
- Log completions for compliance

**Rules can trigger agents:**
- Use the `publish` action to send tasks to `agent.task.*`
- Spawn agents based on graph events (e.g., new entity triggers investigation)
- Chain agents by triggering follow-up tasks on completion (architect → editor)

**Rules cannot control agents:**
- No mechanism for rules to force state transitions
- Agents are autonomous once started
- This is intentional — agents should reason, not be puppeted

### Optional: agentic-memory Integration

The agentic-memory component provides graph-backed persistent memory:

```text
┌────────────────┐     ┌─────────────────┐     ┌──────────────┐
│  agentic-loop  │────▶│ agentic-memory  │────▶│   Graph      │
│                │     │                 │     │  Processor   │
│                │◀────│                 │     │              │
└────────────────┘     └─────────────────┘     └──────────────┘
  context.compaction.*   graph.mutation.*
                         context.injected.*
```

- **Fact extraction**: Before context compaction, extract key facts to the graph
- **Context hydration**: Recover relevant context from the graph after compaction
- **Pre-task preparation**: Inject relevant prior knowledge before a task starts

### Optional: Graph Query as Tool

Agents can query the knowledge graph via a registered tool, enabling access to accumulated knowledge.
The graph_query tool accepts entity IDs for direct lookup or natural language queries for semantic search.

This creates a feedback loop: agents act on knowledge, generate new knowledge, which future agents can access.
But this tool is registered by the application — it's not built into the agentic components.

## Failure Modes and Recovery

### Iteration Limits

Agents can get stuck in loops. SemStreams enforces max iterations:

- Default limit: 20 iterations
- Configurable per task based on complexity
- When exceeded: loop transitions to "failed" state with diagnostic info

### Timeout Guards

Long-running loops can consume resources. Timeouts apply at multiple levels:

- **Loop timeout** (default 120s): Maximum total execution time
- **Tool timeout** (default 60s): Maximum time per tool execution
- **LLM request timeout**: Per-call budget resolved from a four-layer precedence
  chain — task → endpoint → capability → component default. Lets operators
  give fast classification calls a shorter leash than heavy planning calls
  without editing every producer. See
  [agentic-components → Timeout Resolution](../advanced/08-agentic-components.md#timeout-resolution).
- **Context cancellation**: Propagates through all operations for clean shutdown

### Tool Allowlists

Restrict what tools agents can use for security:

- Empty allowlist: All registered tools permitted
- Populated allowlist: Only listed tools allowed
- Blocked tools: Return error result (not system failure) — agent can reason about it

### Graceful Degradation

Agents handle tool errors as part of their reasoning. When a tool returns an error (e.g., "permission denied"),
the agent receives that error as a tool result and can adapt — trying a different approach, asking for help,
or reporting what it couldn't do.

## SemStreams Agentic Components

SemStreams provides five components for building agentic systems:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                      Component Architecture                          │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌──────────────────┐                                              │
│   │ agentic-dispatch │  User message routing, commands, permissions │
│   │                  │  Bridges input channels to agentic system    │
│   └────────┬─────────┘                                              │
│            │                                                         │
│            ▼                                                         │
│   ┌─────────────────┐                                               │
│   │  agentic-loop   │  State machine, orchestration, trajectory     │
│   │                 │  Coordinates the entire agent lifecycle       │
│   └────────┬────────┘                                               │
│            │                                                         │
│     ┌──────┴──────┐                                                 │
│     ▼             ▼                                                 │
│ ┌────────────┐ ┌──────────────┐                                     │
│ │  agentic-  │ │   agentic-   │                                     │
│ │   model    │ │    tools     │                                     │
│ │            │ │              │                                     │
│ │ LLM calls  │ │ Tool exec    │                                     │
│ │ (OpenAI-   │ │ Registry     │                                     │
│ │ compatible)│ │ Allowlist    │                                     │
│ └────────────┘ └──────────────┘                                     │
│            │                                                         │
│            ▼                                                         │
│   ┌──────────────────┐                                              │
│   │ agentic-memory   │  Graph-backed persistent memory              │
│   │                  │  Context hydration, fact extraction          │
│   └──────────────────┘                                              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

These communicate over NATS JetStream for reliable, ordered message delivery:

```text
External Input (CLI/Slack/Discord/Web)
      │
      ▼
user.message.* ────▶ agentic-dispatch ────▶ agent.task.*
                           │                     │
                           │                     ▼
                           │               agentic-loop ─────▶ agent.request.*
                           │                     │                   │
                           │                     │                   ▼
                           │                     │             agentic-model ◀──▶ LLM
                           │                     │                   │
                           │                     │◀──── agent.response.*
                           │                     │
                           │                     ├─────▶ tool.execute.*
                           │                     │              │
                           │                     │              ▼
                           │                     │        agentic-tools ──▶ Executors
                           │                     │              │
                           │                     │◀──── tool.result.*
                           │                     │
                           │                     ├─────▶ agent.context.compaction.*
                           │                     │              │
                           │                     │              ▼
                           │                     │       agentic-memory ──▶ Graph
                           │                     │              │
                           │                     │◀──── agent.context.injected.*
                           │                     │
                           │◀──── agent.complete.*
                           │
                           ▼
                    user.response.*
```

For implementation details, configuration options, and production guidance, see the
[Advanced: Agentic Components](../advanced/08-agentic-components.md) guide.

For orchestration patterns (when to use rules vs. workflows), see
[Orchestration Layers](./14-orchestration-layers.md).

# ADR-098: Observability Splits by Subject — Agent Execution to the Graph, Substrate Out of Band

## Status

**Accepted (2026-08-22).** **Supersedes ADR-027 Phases 2 and 3**, and withdraws
`emit_diagnosis` as a framework capability. ADR-027 Phase 1 remains history: it shipped,
and the reasoning that produced it is not retracted — only its continuation.

## Context

ADR-027 proposed an ops agent as a meta-harness: an LLM observing execution patterns,
emitting diagnosis for human review (Phase 1, shipped 2026-04-20), then proposing harness
changes (Phase 2), then tuning autonomously against a Pareto frontier (Phase 3).

A 2026-08-22 review established four things, each verified against the tree:

**Nothing triggers it.** `configs/flows/ops-agent.json` declares no rule processor, and no
config in `configs/rules/` dispatches the `ops` role. It wakes only on a user message. The
capability has been shipped and inert for four months.

**Its stated trigger does not exist.** ADR-080 decision 4 says the reference ops flow "fires
per loop completion (`agent.complete.*`)". No shipped config implements that, and the
`agent.complete.*` port on the ops dispatch is terminal settlement, not debrief.

**The cost model is inverted.** An observer that queries the graph, walks trajectories, and
reasons over them is strictly more expensive than the work it observes. Wired the obvious way
— one ops loop per completed loop — it cannot keep up by construction. The only two reachable
states are *off* and *does not scale*. ADR-027's own answer to this was a per-cycle token
budget: capping the blast radius rather than reducing the work.

**The substrate cannot be asked to narrate its own failure.** A substrate problem — storage
pressure, capacity exhaustion, an index not ready — is a fact about the machinery. Recording
it as graph triples requires the failing component to perform additional work *through the
machinery that is failing*, and to succeed at a durable write precisely when writes are what
is degrading. Observability of the substrate must not depend on the substrate's health.

A fifth fact shaped the boundary rather than the retirement: substrate facts are **standing
states**, not events. The rule engine reacts to writes. A readiness producer that dies simply
stops writing, so the verdict that matters most — *unknown* — arrives as silence, which no
write-triggered engine can match. A rule conditioned on the last written value latches
`ready` for the duration of the outage.

## Decision

**1. The framework ships no bespoke ops agent.** ADR-027 Phases 2 and 3 are withdrawn.
`emit_diagnosis` is not carried forward as a framework capability: the judgement it encodes
is operational and product-specific, and operational reality changes the correct answer
faster than a framework can encode a guess at it. Products compose their own operational
agents from framework primitives — rules, conditions, agent loops, governance.

**2. Agent execution is observable through the graph.** The framework's obligation is to
publish **reportable conditions** about agent execution: classified, framework-exclusive
facts about an addressable entity, stamped as triples, which rules branch on and products act
on. This is where framework observability earns its keep, because these are facts only the
framework can know and no external observer can reconstruct. `agent.loop.terminal-reason` and
`agent.loop.evidence-integrity` are the shipped instances.

**3. Substrate observability is logs and metrics, out of band.** Substrate facts SHALL NOT be
published as graph triples. Logs and metrics are the contract for substrate observability,
and the obligation that follows is that they be *tight* — complete, well-labelled, and
alertable — because they are the only surface. This is a correctness position, not a
preference: see the fourth force above.

**4. The framework does not decide what is operationally concerning.** Thresholds, alert
policy, and response belong to the operator and the product. The framework publishes
classified facts and refuses to enforce on them — consistent with ADR-088's rejection of a
framework-declared key list, and with storage-observability's report-only commitment.

## Consequences

### Positive

- The cost of agent observability becomes proportional to problems rather than to traffic: a
  condition is stamped when something goes wrong, and a healthy system pays nothing.
- Substrate observability stops depending on substrate health.
- Products gain a machine-readable seam (triples, rules) instead of an opinion delivered as
  an agent.
- Two inert framework artifacts stop implying a roadmap that will not be built.

### Negative

- Operators wanting LLM-assisted diagnosis must build it. The framework provides the facts
  and the primitives, not the analyst.
- Agent-execution conditions must be added one at a time, each earning its place against the
  test that it is framework-exclusive, actionable, classified, and about a named subject.
- ADR-027's Pareto-frontier tuning ambition is not replaced. Nothing in this decision
  provides automated harness optimisation.

### Neutral

- ADR-027 Phase 1 shipped and is history. This ADR retires its continuation, not its record.
- Storage-observability is unaffected and its shape is affirmed: a service that collects,
  derives, and publishes, with Prometheus metrics, alerting rules, health, and an HTTP route
  as consumers. It is the model for decision 3, not an exception to it.

## Alternatives Considered

**A. Enable ADR-027 Phase 2 as designed (config-only).** Rejected. Phase 2 is reachable
today by editing `allowed_tools`, and has never been enabled in four months, because the
agent it would empower has no trigger. Granting mutation tools to an inert agent does not
make it useful; it widens the blast radius of something nobody runs.

**B. Wire the ops agent to fire per loop completion.** Rejected on the inverted cost model.
This is the design ADR-080 decision 4 assumed already existed. Building it would make the
observer more expensive than the observed, permanently.

**C. Publish substrate conditions as graph triples so rules can react.** Rejected on the
fourth force: it asks a degrading substrate to do more durable writes about its own
degradation. It also fails on the standing-state problem — the most important substrate
verdict is the absence of writes, which a write-triggered engine cannot observe.

**D. Add a typed operational-KV lane to the rule engine.** Rejected. Assessed in detail: the
transport is not a barrier (a KV bucket is a JetStream stream), the port abstraction is
already generic, and the transition machinery is not entity-specific. It was rejected on
population and on capability — the only fact that genuinely cannot live in the graph is
readiness, it has no present rule consumer, and a write-triggered lane structurally cannot
express its critical *unknown* state.

**E. Have producers materialise "interesting" changes as messages for rules to consume.**
Rejected. It puts the producer in charge of predicting which transitions a consumer cares
about — a framework-declared threshold in disguise — and is lossier than the report it
summarises.

## Related decisions

- **ADR-027** — the ops agent meta-harness. Phase 1 history; Phases 2 and 3 superseded here.
- **ADR-080** — decision 4's per-loop-completion trigger describes a mechanism that was never
  implemented; this ADR removes the expectation rather than building it.
- **ADR-088** — readiness aggregation is consumer-declared; the framework declares no key
  list. Decision 4 above is the same principle for thresholds.
- **ADR-049 / ADR-055** — the bucket-ownership rubric that decides where a fact is stored;
  this ADR decides which facts are graph-observable at all.
- **ADR-068** — agent execution evidence is non-regenerable, which is why evidence integrity
  was the first condition shipped.

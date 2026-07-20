# graph-index-readiness — delta

## ADDED Requirements

### Requirement: The readiness envelope is exposed as Prometheus metrics
Every producer of the ADR-066 readiness envelope (graph-index, graph-embedding) SHALL expose it as scrapeable Prometheus gauges, not only over the NATS status subject. At minimum the gauges are `readiness` (1 when Ready else 0), `lag` (revisions behind target), `indexed_revision`, and `target_revision`, plus a `state`-labeled gauge distinguishing building / ready / degraded / reset_required. The gauges MUST reflect the same values `computeIndexStatus` returns and stay fresh independent of query traffic (refreshed on a periodic tick); this is additive — the NATS status envelope is unchanged.

#### Scenario: Readiness and lag are scrapeable without a NATS query
- **GIVEN** graph-index is running and catching up under continuous write
- **WHEN** Prometheus scrapes the component
- **THEN** the `readiness`, `lag`, `indexed_revision`, and `target_revision`
  gauges are present and reflect the current `computeIndexStatus` values
- **AND** no NATS `graph.index.query.status` request is required to read them

#### Scenario: State distinguishes catching-up from broken
- **GIVEN** the index is `building` with lag, versus `degraded` or `reset_required`
- **WHEN** an operator inspects the `state`-labeled gauge
- **THEN** the current state is identifiable (so "catching up" can be alerted
  differently from "broken"), not collapsed into `readiness=0`

#### Scenario: Metrics stay fresh without query traffic
- **GIVEN** no consumer is issuing `graph.index.query.status` requests
- **WHEN** Prometheus scrapes over time
- **THEN** the readiness gauges still update (refreshed on the component's tick),
  never frozen at a stale last-queried value

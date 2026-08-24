# Local Monitoring

Run Prometheus and Grafana locally to monitor SemStreams during development and e2e testing.

## Quick Start

```bash
# Start observability stack
task observe:up

# Open dashboards
open http://localhost:3000  # Grafana (admin/admin)
open http://localhost:9090  # Prometheus
```

Stop when done:

```bash
task observe:down
```

## Architecture

```text
SemStreams (:9090/metrics) --> Prometheus (:9090) --> Grafana (:3000)
     |                              |                      |
  Exposes metrics            Scrapes every 10s      Visualizes data
```

All SemStreams components expose metrics at a single `/metrics` endpoint on port 9090. Prometheus scrapes this endpoint and stores time-series data. Grafana queries Prometheus and renders dashboards.

## Dashboards

Four pre-configured dashboards auto-load on startup:

| Dashboard | Purpose | Key Panels |
|-----------|---------|------------|
| **SemStreams Overview** | System health at a glance | Service status, throughput, error rate, latency percentiles |
| **IndexManager Metrics** | Index operations | Backlog size, update rates, query latency by index type |
| **Graph Processor** | Graph processing | Entity processing rate, triple operations, community updates |
| **Cache Performance** | Cache efficiency | Hit/miss ratios, eviction rates, memory usage |

Access dashboards: Grafana sidebar > Dashboards > SemStreams folder.

## Key Metrics Reference

### Health Indicators

| Metric | Description | Alert Threshold |
|--------|-------------|-----------------|
| `up{job="semstreams"}` | Service availability (1=up, 0=down) | < 1 |
| `semstreams_messages_failed_total` | Failed message count | Rate > 0 sustained |
| `indexmanager_backlog_size` | Pending index updates | > 1000 |

### Throughput

| Metric | Description | Typical Range |
|--------|-------------|---------------|
| `semstreams_messages_total` | Total messages received | Varies by load |
| `semstreams_messages_processed_total` | Successfully processed | Should track total |
| `indexmanager_updates_total` | Index update operations | 1-10x message rate |

### Latency

| Metric | Description | Target |
|--------|-------------|--------|
| `semstreams_processing_duration_seconds` | End-to-end processing time | p95 < 100ms |
| `indexmanager_query_duration_seconds` | Index query latency | p95 < 10ms |
| `indexmanager_update_duration_seconds` | Index update latency | p95 < 50ms |

### Index-Specific

| Metric | Description |
|--------|-------------|
| `indexmanager_size{index="..."}` | Entries per index type |
| `indexmanager_queries_total{index="..."}` | Queries per index |
| `indexmanager_updates_total{index="..."}` | Updates per index |

Index types: `predicate`, `incoming`, `outgoing`, `alias`, `spatial`, `temporal`, `structural`, `embedding`, `community`.

### Graph Mutation Outcomes (ADR-091)

| Metric | Description | Useful query |
|--------|-------------|--------------|
| `semstreams_graph_mutation_outcomes_total{operation,outcome}` | Bounded outcomes for create, reconcile, append, and delete | `sum by (operation, outcome) (increase(semstreams_graph_mutation_outcomes_total[1h]))` |

**What it means.** The counter records graph-ingest's observed command result without an entity-ID label. Outcomes
include applied, unchanged, not-found, exists, revision mismatch, invalid input, and other bounded classified failures.

**How to read it.**

- A sustained `revision_mismatch` rate means writers are contending on observed state. Components decide whether a
  fresh exact read and bounded retry fits their domain.
- `entity_not_found` means a must-exist operation raced or preceded birth. The framework does not create a stub.
- Transport-level `commit_unknown` is returned to the caller rather than retried automatically; it is not a server
  outcome that can be reconstructed from this counter.

Relationship targets may be absent. That is valid eventual graph state and is reported during dereference rather than
through a writer-authorization metric.

### Rules Engine

| Metric | Description |
|--------|-------------|
| `semstreams_rule_evaluations_total` | Total rule evaluations |
| `semstreams_rule_triggers_total{rule="..."}` | Triggers per rule |
| `semstreams_rule_state_transitions_total` | State machine transitions |

### Process & Runtime (per-container)

Startup progress is available as a fixed, low-cardinality gauge while a service
or component `Start` is still in flight:

| Metric | Labels | Description |
|--------|--------|-------------|
| `semstreams_startup_units` | `owner`, `stage` | Process-local startup counts. |

`owner` is only `services` or `components`; `stage` is a fixed vocabulary. No
service or component identity label is emitted. These counts explain startup
progress, but `/readyz` remains the readiness authority because it also observes
current health. During composed production boot, Manager privately registers
this collector before binding the configured Prometheus listener; it is not a
public `CoreMetrics` recording surface.

These come for free from the Prometheus Go client — no semstreams-specific configuration needed. They describe the process emitting the metrics (one series per scrape target).

| Metric | Description | Useful query |
|--------|-------------|--------------|
| `process_cpu_seconds_total` | Cumulative CPU time | `rate(process_cpu_seconds_total[1m]) * 100` → CPU% |
| `process_resident_memory_bytes` | RSS | Plot directly |
| `process_virtual_memory_bytes` | VSZ | Plot directly |
| `process_start_time_seconds` | Unix time of process start | `time() - process_start_time_seconds` → uptime |
| `process_open_fds` / `process_max_fds` | File descriptor usage | Ratio triggers "near limit" alerts |
| `go_goroutines` | Live goroutines | Leak detector — flat line is healthy |
| `go_gc_duration_seconds` | GC pause durations | `histogram_quantile(0.99, ...)` for tail latency |
| `go_threads` | OS threads | Correlates with goroutines under load |

### Log Severity Counters

Every slog record at WARN or above increments a counter keyed by component and level. The counter is pure — no message bodies, no payloads — so it's safe to expose on an unauthenticated metrics endpoint. Drill-downs go through the existing message-logger UI, not the counter.

| Metric | Labels | Description |
|--------|--------|-------------|
| `semstreams_log_entries_total` | `component`, `level` | Cumulative count of WARN+ slog records |

Components self-label by calling `slog.With("component", "...")` — common values include `agentic-loop`, `agentic-model`, `graph-processor`, `udp-input`, `rule`. Records without a `component` attr land under `component="unknown"`.

`level` is `warn` or `error`. Debug and Info records skip the counter path entirely.

Example queries:

```promql
# Warnings per minute, last 1m window
rate(semstreams_log_entries_total{level="warn"}[1m]) * 60

# Errors in the last 5 minutes, grouped by component
increase(semstreams_log_entries_total{level="error"}[5m])

# All components currently logging errors
semstreams_log_entries_total{level="error"} > 0
```

Use `rate()` for "right now" trending (1m window smooths transient spikes) and `increase()` for "how bad was it" counts over longer windows.

### Multi-Container Deployments

When multiple containers each run a semstreams process (e.g., a product deploying several roles), the metrics above are per-process — each emits its own numbers. Container identity lives in the Prometheus scrape job config, not in the metric label set:

```yaml
scrape_configs:
  - job_name: 'semspec'
    static_configs:
      - targets: ['semspec:9090']
        labels:
          container: 'semspec'

  - job_name: 'semsource'
    static_configs:
      - targets: ['semsource:9090']
        labels:
          container: 'semsource'
```

Prometheus adds `job`, `instance`, and any static `labels` to every scraped sample. Downstream dashboards filter on `container="semspec"` without semstreams needing to know its own container name. This also means NATS (which runs its own [Prometheus exporter](https://github.com/nats-io/prometheus-nats-exporter)) and non-semstreams containers can be scraped the same way and appear alongside semstreams metrics under different `job` labels.

## Running with E2E Tests

Start observability before running e2e tests to watch metrics in real-time:

```bash
# Terminal 1: Start observability
task observe:up

# Terminal 2: Run e2e tests
task e2e:core:default
# or
task e2e:tiers
```

Watch the Grafana dashboards during test execution to see:
- Message throughput spikes as test data flows
- Index backlog rising then draining
- Processing latency under load

## Prometheus Direct Queries

For ad-hoc analysis, query Prometheus directly at `http://localhost:9090`:

```promql
# Messages per second (5m average)
rate(semstreams_messages_total[5m])

# Error rate percentage
rate(semstreams_messages_failed_total[5m]) / rate(semstreams_messages_total[5m]) * 100

# p99 processing latency
histogram_quantile(0.99, rate(semstreams_processing_duration_seconds_bucket[5m]))

# Index backlog by type
indexmanager_backlog_size
```

## Configuration

### Prometheus Scrape Config

Located at `configs/prometheus/prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'semstreams'
    static_configs:
      - targets: ['host.docker.internal:9090']
    scrape_interval: 10s
```

For Docker network setups (e.g., running SemStreams in a container), use the service name instead:

```yaml
- targets: ['semstreams:9090']
```

### Grafana Provisioning

Dashboards auto-load from `configs/grafana/dashboards/`. To add custom dashboards:

1. Create JSON dashboard in Grafana UI
2. Export via Share > Export > Save to file
3. Place in `configs/grafana/dashboards/`
4. Restart Grafana (or wait 10s for auto-reload)

## Troubleshooting

### "No data" in Grafana

1. Check SemStreams is running and exposing metrics:
   ```bash
   curl http://localhost:9090/metrics | head -20
   ```

2. Check Prometheus can reach the target:
   - Open `http://localhost:9090/targets`
   - Look for `semstreams` job status

3. Verify time range in Grafana (top-right) includes recent data

### Prometheus can't scrape metrics

If running SemStreams on host (not in Docker):
- Use `host.docker.internal:9090` in prometheus.yml (macOS/Windows)
- Use `172.17.0.1:9090` on Linux

If running SemStreams in Docker:
- Ensure both containers are on the same network
- Use container/service name instead of localhost

### Grafana login issues

Default credentials: `admin` / `admin`

To reset password:
```bash
docker exec -it semstreams-grafana grafana-cli admin reset-admin-password newpassword
```

## Next Steps

- [Configuration](../basics/06-configuration.md) - Capability tiers and feature flags
- [Performance](../advanced/03-performance.md) - Optimization strategies

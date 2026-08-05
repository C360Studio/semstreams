# GS-01 suffix lookup measurement

> **DESIGN EVIDENCE ONLY.** This measurement closes the cardinality, latency, concurrency, transfer, allocation, and
> two-second-budget gaps named by the suffix inventory. It does not select an owner or target mechanism.

## Baseline and harness

- Repository baseline: `d322708a8ec360658d513a077fa99c9fe1ef5a81`
- Date: 2026-08-04
- Host: local macOS Docker environment
- NATS server: repository test-client default `2.14-alpine`, file-backed JetStream
- NATS Go client: `v1.52.0`
- Caller budget applied per operation: `2s`, matching
  `processor/graph-query/entity_resolver.go:86-115`
- External harness: `/private/tmp/gs01-suffix-measurement/main.go`
- Harness SHA-256: `58bf3b23c8c755b1be01cdfd1b76f6f0ca50a1b1ae3bb829f6ef0f5e9890e4f0`
- Harness size: 284 lines, 6,817 bytes

Cardinalities were selected from repository evidence:

- `125`: the observed statistical E2E population recorded in
  `docs/proposals/graph-state-read-write-program.md:418-423`;
- `5,000`: the CI profile in `docs/operations/evidence/graph-index-pre-tag-0a7af288.md:37`;
- `21,000`: the full profile in the same evidence at `:72`.

Each authority key had the six-part form `acme.edge.demo.system.sensor.%06d`. The target was the lexically last key.
The harness compared:

- one direct KV Get from a suffix index;
- the current SDK `Keys` collection followed by dot-boundary suffix matching; and
- an explicit `WatchAll` + `IgnoreDeletes` + `MetaOnly` collector that requires the initial nil completion marker,
  rejects channel close before that marker, sorts/compacts, then performs the same match.

The two scan methods transfer metadata only. `key_bytes_min` is the exact sum of key text and excludes NATS protocol,
subject, header, object, channel, and slice overhead. `alloc_one` is the Go process `TotalAlloc` increase around one
operation after GC and is an approximate process-level allocation observation, not a retained-heap measurement.

## Results

No operation failed or reached the two-second budget in this run.

### Direct suffix-index Get

- **125 entities:** concurrency 1 p50 `138µs`, p95 `175µs`; concurrency 32 p50 `298µs`, p95 `482µs`.
- **5,000 entities:** concurrency 1 p50 `136µs`, p95 `159µs`; concurrency 32 p50 `271µs`, p95 `382µs`.
- **21,000 entities:** concurrency 1 p50 `135µs`, p95 `159µs`; concurrency 32 p50 `280µs`, p95 `394µs`.
- One operation allocated approximately `8–11 KiB` in the process-level observation.

The direct lookup remained effectively independent of authority cardinality.

### Current SDK Keys scan

- **125 entities / 4,375 key bytes minimum:**
  - concurrency 1 p50 `761µs`, p95 `1.09ms`;
  - concurrency 8 p50 `3.48ms`, p95 `4.48ms`;
  - concurrency 32 p50 `8.09ms`, p95 `14.88ms`;
  - one scan allocated approximately `301 KiB`.
- **5,000 entities / 175,000 key bytes minimum:**
  - concurrency 1 p50 `7.72ms`, p95 `8.48ms`;
  - concurrency 8 p50 `49.87ms`, p95 `51.17ms`;
  - concurrency 32 p50 `187.12ms`, p95 `192.82ms`;
  - one scan allocated approximately `8.30 MiB`.
- **21,000 entities / 735,000 key bytes minimum:**
  - concurrency 1 p50 `28.24ms`, p95 `30.30ms`;
  - concurrency 8 p50 `183.64ms`, p95 `192.66ms`;
  - concurrency 32 p50 `729.25ms`, p95 `737.73ms`;
  - one scan allocated approximately `35.23 MiB`.

### Completion-proven WatchAll scan

- **125 entities / 4,375 key bytes minimum:**
  - concurrency 1 p50 `751µs`, p95 `1.31ms`;
  - concurrency 8 p50 `2.91ms`, p95 `4.04ms`;
  - concurrency 32 p50 `9.24ms`, p95 `17.23ms`;
  - one scan allocated approximately `258 KiB`.
- **5,000 entities / 175,000 key bytes minimum:**
  - concurrency 1 p50 `9.00ms`, p95 `44.06ms`;
  - concurrency 8 p50 `52.78ms`, p95 `55.14ms`;
  - concurrency 32 p50 `178.52ms`, p95 `222.60ms`;
  - one scan allocated approximately `8.30 MiB`.
- **21,000 entities / 735,000 key bytes minimum:**
  - concurrency 1 p50 `29.04ms`, p95 `31.55ms`;
  - concurrency 8 p50 `185.39ms`, p95 `186.39ms`;
  - concurrency 32 p50 `728.27ms`, p95 `729.21ms`;
  - one scan allocated approximately `35.23 MiB`.

The explicit completion proof has the same material cost curve as the current SDK scan. At 21,000 entities, a scan
was roughly 200 times slower than an indexed Get at concurrency 1 and more than 2,600 times slower at concurrency 32.
Thirty-two scans can also create roughly 1.1 GiB of aggregate allocation work before retained-memory and protocol
overheads, even though this local run remained below the two-second response budget.

## Limits and remaining unknowns

- This is one local host and one NATS server, not a capacity SLA for edge hardware, clusters, leaf nodes, or WAN links.
- Scan sample counts were 20 at concurrency 1 and two waves at concurrency 8/32; percentile precision is directional.
- The harness measures the storage operation and matching, not the surrounding NATS request/reply, graph-query alias
  attempt, classification, JSON, logging, scheduling, or other component load.
- A warm process-cache hit was not measured. It avoids NATS I/O and is expected to be cheaper than the indexed Get.
- Entity values were tiny, but both scan paths are `MetaOnly`; value size should not affect transfer. Key length,
  cardinality, server topology, and concurrent watchers do.
- This run does not measure current production tier hit distribution, collision frequency, stale hits, or edge target
  cardinality. Those remain observational gaps.
- Staying below two seconds at 21,000 entities on this host does not bound larger populations or slower edge targets.

# graph-ingest

> Delta for gh#480 (part 1). ADDs an ingest-measurability requirement to the
> `graph-ingest` capability. Verified against `processor/graph-ingest/component.go`.

## ADDED Requirements

### Requirement: Ingest MUST expose metrics that separate queue wait from processing time

graph-ingest MUST expose Prometheus metrics that make the ingest pipeline measurable at
the component: a per-message processing-duration histogram (time spent applying a
message — the merge and CAS write) and an ingest-lag histogram (the age of a message
when processing begins, i.e. how long it waited in the stream/delivery buffer before
ingest reached it). Together these MUST let an operator distinguish backlog/queue wait
from per-message processing time, which previously could only be inferred downstream. The
existing throughput counter (`entities_updated_total`) remains the ingest-rate signal.

#### Scenario: an operator can read processing vs queue time

- **GIVEN** graph-ingest processing a backlog of messages
- **WHEN** the operator scrapes metrics
- **THEN** the processing-duration histogram reports per-message apply time
- **AND** the ingest-lag histogram reports how long each message waited before processing
- **AND** the two are separately observable (backlog latency is not conflated with
      processing time)

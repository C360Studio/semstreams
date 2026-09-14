# Building Your First Processor

Start with the maintained [IoT sensor example](../../examples/processors/iot_sensor/), then adapt its domain model
for your application. It turns a raw reading into a sensor entity, a zone entity, and a relationship between them.
This path uses local NATS and deterministic Go processing. No model or agent loop is needed.

The source files are the implementation reference. This guide explains how they fit together rather than copying
an implementation that can drift from the compiled example.

## Check the example

From the repository root, with the Go version declared in [go.mod](../../go.mod) installed (currently 1.26.3):

```bash
go test -race ./examples/processors/iot_sensor/...
```

This checks transformation, entity identity, semantic predicates, registration and payload behavior. It does not
start the UDP-to-graph application. Docker is needed only for the live example below;
see [Prerequisites](00-prerequisites.md).

## Run the local dataflow

This run uses Docker, `curl`, `jq` and netcat (`nc`), in addition to Go.

The example is composed by **`cmd/e2e-semstreams`**, the repository's example/test harness. It explicitly registers
the IoT component and its payloads. The core `cmd/semstreams` binary does not include that example registration;
`task dev:start` currently builds the core binary, so use the commands here for this example.

Use a fresh local NATS instance. The checked config uses TCP 4222 for NATS, TCP 8080 for HTTP, TCP 9090 for metrics,
and UDP 14550 for input. Run without `SEMSTREAMS_NATS_URLS` or `SEMSTREAMS_LIFECYCLE_SEED` overrides.

### 1. Start NATS

In one terminal:

```bash
docker run --rm --name semstreams-hello \
  -p 127.0.0.1:4222:4222 nats:2.14.4-alpine -js
```

This disposable learning instance keeps no data after the container is removed. A deployed edge application needs
its own durable storage and recovery configuration; this example does not prove offline synchronization.

### 2. Build, validate and start the example

In another terminal, from the repository root:

```bash
go build -o bin/e2e-semstreams ./cmd/e2e-semstreams
./bin/e2e-semstreams validate configs/hello-world.json
./bin/e2e-semstreams --config configs/hello-world.json
```

Validation should report no errors. This config reports warnings for optional API ports and unwatched indexes.
It starts six components: UDP input, IoT processor, graph ingest, graph index, graph query, and graph gateway.
Clustering is not enabled, so optional community-index/summary availability warnings can appear during the run.
The prefix query below exercises the structural path.

### 3. Observe a sensor entity

In a third terminal, check that startup completed:

```bash
curl --max-time 5 -fsS http://localhost:8080/readyz
```

Expect `READY`. Then send a reading with netcat (`nc`):

```bash
printf '%s\n' \
  '{"device_id":"sensor-001","type":"temperature","reading":23.5,"unit":"celsius","location":"warehouse-7"}' \
  | nc -u -w 1 localhost 14550
```

Query the gateway mounted on the service manager's HTTP server:

```bash
curl --max-time 5 -sS http://localhost:8080/graph-gateway/graphql \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "{entitiesByPrefix(prefix: \"demo\", limit: 10) {entities {id triples {predicate object}} next_cursor}}"
  }' \
  | jq
```

Look for an ID ending in `.sensor.environmental.temperature.sensor-001` and a zone ending in
`.zone.facility.area.warehouse-7`. The platform segment includes a deployment suffix; do not hard-code the full ID.
Ingest and indexing are asynchronous, so repeat the query if the reading is not yet visible. The response may
include generated hierarchy entities. If `next_cursor` is present, pass it as `cursor`
to read the next page.

The observed sensor has `sensor.measurement.celsius = 23.5` and `geo.location.zone` referencing the zone entity.
That is the first-success check: input became explicit, queryable domain facts.

Stop the example with Ctrl-C in its terminal, then stop NATS with Ctrl-C in its terminal. The harness exits after
shutting down its components. This is a learning composition, not a production application template.

## Adapt the domain, keep the framework contracts

Decide what facts your application needs before copying files. The IoT example separates these responsibilities:

| File | What to learn or adapt |
| --- | --- |
| [vocabulary.go](../../examples/processors/iot_sensor/vocabulary.go) | Named predicates, units and descriptions |
| [payload.go](../../examples/processors/iot_sensor/payload.go) | Payloads, identity and registration |
| [processor.go](../../examples/processors/iot_sensor/processor.go) | Raw input mapped to domain facts |
| [component.go](../../examples/processors/iot_sensor/component.go) | Ports, lifecycle and message emission |
| [register.go](../../examples/processors/iot_sensor/register.go) | Factory, schema and pure port declaration |

### 1. Give facts an identity and meaning

Define the domain's predicates and payload fields. A payload returns its minted entity ID and semantic triples;
relationship objects use the related entity's ID. Mint under the deployment authority supplied through component
dependencies. The application owns the source system, taxonomy and leaf identity; it does not substitute its
product name for the deployment authority. The example's identity and predicate tests show these distinctions.

See [Graphable](03-graphable-interface.md) and [Vocabulary](04-vocabulary.md) for the underlying concepts.

### 2. Register payloads and component factories separately

The example exports `RegisterPayloads(reg *payloadregistry.Registry) error` for wire decoding and
`Register(registry *component.Registry) error` for component creation. Its registration includes `Factory`,
`Schema` and `Ports: DeclarePorts`. The port declarer and constructor share configuration derivation.

Your composition root must call both registration functions and pass the payload registry to consuming
components. Importing a package does not perform payload registration. For standalone decoding, use
`message.NewDecoder(reg)` with that same registry.
Follow the [Payload Registry Guide](../concepts/15-payload-registry.md)
for registration, encoding and decoding; do not introduce an `init()` payload singleton.

For a working composition, inspect the explicit example registrations and service dependencies in
[cmd/e2e-semstreams](../../cmd/e2e-semstreams/main.go). Select your own application components and capabilities;
the harness's additional test facilities are not required in an adopter binary.

### 3. Declare the dataflow

Port definitions carry typed configuration in `Config`; they do not have flat `Type` or `Subject` fields.
Use the example's `DeclarePorts` implementation and [hello-world config](../../configs/hello-world.json) together:
UDP publishes raw input, the domain processor emits registered payloads, and graph ingest writes entity state.
The processor supplies semantic facts rather than writing graph buckets itself.

Keep domain transformation in the processor and execution mechanics in the component. Check ordinary behavior
with the package tests, then use the application binary's composition validator and an observed input/output
check. A successful compile alone does not establish a working graph path.

## Continue with an application

The [SemSource walkthrough](09-building-semsource.md) applies these ownership decisions to a real source-to-context
application. [Orchestration layers](../concepts/14-orchestration-layers.md) explains how rules and components add
multi-step behavior when the application needs it. Model use, agent loops and human interaction remain explicit
application choices.

# IoT Sensor Example Processor

This maintained example maps raw sensor readings to semantic entities and their zone relationships.
The application owns the vocabulary and domain transformation; framework primitives provide ports, message
registration, lifecycle and graph ingestion. No model or agent loop is needed.

Follow [Building Your First Processor](../../../docs/basics/05-first-processor.md) for the checked local run and
adaptation guide. The example is registered by `cmd/e2e-semstreams`, not the core `cmd/semstreams` binary.

## Source map

| File | Responsibility |
| --- | --- |
| [vocabulary.go](vocabulary.go) | Predicate descriptions and units |
| [payload.go](payload.go) | Sensor/zone Graphable payloads, identity minting and payload registration |
| [processor.go](processor.go) | Raw input to domain facts |
| [component.go](component.go) | Typed ports, dependencies, lifecycle and message emission |
| [register.go](register.go) | Component factory, schema and port declarer |

## Check behavior

From the repository root:

```bash
go test -race ./examples/processors/iot_sensor/...
```

These unit checks cover transformation and payload/component contracts. The linked guide separately verifies
UDP input through the local example composition to a queryable graph entity. The integration-tagged tests in
this directory require the repository's [integration runner](../../../docs/contributing/01-testing.md).

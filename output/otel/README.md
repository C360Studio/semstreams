# OpenTelemetry Exporter

The `otel-exporter` is an explicit optional framework adapter. It converts agent and tool events into OTLP JSON spans
and metrics and sends them to an OTLP/HTTP collector. Core composition does not register it; a binary must select the
OTEL adapter explicitly.

## Proven contract

- OTLP/HTTP only. Set `protocol` to `http`; other values fail validation.
- `endpoint` is the collector base URL, including `http://` or `https://`. The default is
  `http://localhost:4318`.
- Trace export posts OTLP JSON to `/v1/traces` when `export_traces` is enabled.
- Metric export posts OTLP JSON to `/v1/metrics` when `export_metrics` is enabled.
- `headers` are copied onto both request types.
- `batch_timeout` controls the flush interval and `export_timeout` bounds a flush request.
- `sampling_rate` controls span sampling. Metrics are not sampled.

The adapter does not implement OTLP logs, configurable resource attributes, item-count batch limits, or an
`insecure` transport switch. HTTP versus HTTPS is selected by the endpoint URL. Supplying removed or unknown config
fields fails component construction instead of being silently ignored.

## Example

```json
{
  "type": "output",
  "name": "otel-exporter",
  "enabled": true,
  "config": {
    "endpoint": "http://otel-collector:4318",
    "protocol": "http",
    "service_name": "semstreams",
    "service_version": "1.0.0",
    "export_traces": true,
    "export_metrics": true,
    "batch_timeout": "5s",
    "export_timeout": "30s",
    "sampling_rate": 1.0,
    "headers": {
      "Authorization": "Bearer replace-me"
    }
  }
}
```

Port definitions may be omitted to use the component defaults. The default inputs consume agent lifecycle events and
tool results from the `AGENT` stream.

## Selection and ownership

See [ADR-075](../../docs/adr/075-framework-package-admission-and-composition.md). OTEL remains framework-owned only as
this explicit, fail-closed adapter; it is not evidence of an AGNTCY integration layer.

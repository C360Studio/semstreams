# Compose bootstrap client observability

## Why

Both primary binaries construct `natsclient.Client` before final logger and metrics composition. The client snapshots
the bootstrap default logger, and neither client receives `WithMetrics` before `Connect`. Production therefore loses
configured local formatting, client WARN/ERROR counting, and stable component identity. Assigning production's final
logger wholesale would create a same-client NATS-forwarding cycle on publish failures that reach failure accounting.

Production also derives log-forwarder policy from the stale file-loaded config before `config.Manager.Start`, contrary
to the current effective-`SafeConfig` service-composition contract.

## What changes

- Add shared internal Phase-A helpers that construct metrics and one configured local handler before NATS connection.
- Construct the client with the existing `WithLogger` and `WithMetrics` options before `Connect`.
- Keep the client and config-manager logger graphs structurally non-forwarding while preserving identical configured
  local output and common base attributes across logger instances.
- Run post-arbitration validation and effective stream provisioning before final log-forwarder composition.
- Resolve enabled log-forwarder inner policy through one internal semantic owner used by both boot and the service
  constructor, while preserving the named public `service.LogForwarderConfig` type.
- Preserve production and E2E destination differences and add real-construction regression evidence without arbitrary
  sleeps.

## Non-goals

- No logger setter, mutable proxy, deferred handler, process-global mutable logger, or silent fallback logger.
- No new public `natsclient`, service, logging, config, metric, status, or health symbol.
- No new subject, stream, payload, metric family, durable state, communication path, or runtime reconfiguration.
- No NATS forwarding for client or config-manager records through the same client.
- No ADR; this applies ADR-058 using existing constructor options.

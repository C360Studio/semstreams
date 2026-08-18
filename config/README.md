# Configuration Package

The `config` package loads and validates SemStreams configuration, arbitrates file and NATS KV state at startup, and
provides defensive concurrent reads through `SafeConfig`.

## Activation boundary

Config Manager selects desired configuration during `Start` and seals a defensive boot snapshot. Service and
component constructors consume that snapshot. The running process does not watch configuration as lifecycle commands:
later `services.*`, `components.*`, `platform`, `nats`, and `model_registry` writes cannot create, stop, reconfigure,
restart, or replace runtime instances.

`GetConfig()` exposes the current desired configuration view. `BootConfig()` exposes the immutable configuration
selected for this process. Runtime construction and effective-state reporting must use `BootConfig()`.

Rule definitions are the bounded exception. Their dedicated processor-owned API can hot reload definitions inside an
already-composed Rule component; general configuration writes cannot change that component's envelope.

## Durable desired state

The `semstreams_config` KV bucket stores desired state. Config Manager:

- arbitrates file and KV versions at startup;
- pushes first-boot configuration into KV;
- supports explicit desired-state component upserts and deletes;
- records local write revisions so its desired view does not double-apply its own watcher echoes; and
- refuses every write when startup detects that the bucket belongs to a different platform identity.

A write after boot is persistence for a later boot, not proof of activation. Callers compare desired components with
the sealed boot component map when they need restart-required truth.

## Platform identity safety

The platform identity in a populated config bucket must match the local platform identity. On mismatch, Config Manager
continues with local configuration in detached mode and returns a fatal classified error from `PushToKV`,
`PutComponentToKV`, and `DeleteComponentFromKV`. It never writes into the foreign bucket.

## Configuration shape

Top-level configuration includes platform identity, NATS transport, security, service definitions, component
definitions, model registry entries, and rule packs. Service `config` values and component-specific `config` values are
JSON objects decoded by their owning constructors or factories.

Use the generated schemas under `schemas/` as the operator-facing field contract. Unknown service-constructor fields
are rejected rather than silently accepted.

## Testing

Use `natsclient.NewTestClient` for real-KV integration coverage and the race detector for `SafeConfig` concurrency.
Tests should prove boot selection, desired-versus-boot separation, platform identity refusal, and exact persistence
outcomes. Use explicit synchronization for watcher behavior; arbitrary sleeps are not an acceptance mechanism.

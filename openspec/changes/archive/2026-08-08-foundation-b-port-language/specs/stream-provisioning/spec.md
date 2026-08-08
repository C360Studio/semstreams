<!-- markdownlint-disable MD041 -->

## ADDED Requirements

### Requirement: Port-derived stream declarations consume canonical normalized facts

Raw component configuration SHALL be decoded through canonical `PortConfig` and resolved before stream provisioning.

Only canonical `jetstream` output ports with normalized stream facts SHALL contribute generic stream declarations.

When a canonical JetStream output omits `stream_name`, only the canonical generic provisioner MAY derive the physical
stream name from its declared subjects. No input consumer, component-local helper, or specialized non-provisioning
path may use that derivation.

Provisioners SHALL NOT infer stream identity or policy from retired flat fields, unresolved configuration, concrete
configuration type switches, or consumer-local defaults.

#### Scenario: Named canonical JetStream output contributes exact facts

- **GIVEN** a valid canonical `jetstream` output with an explicit `stream_name`
- **WHEN** component-derived stream declarations are collected
- **THEN** its stream name, subjects, storage, retention, size, replicas, and consumer policy are taken from its
  normalized stream facts
- **AND** those values are not reconstructed independently by the provisioner

#### Scenario: Generic provisioner derives omitted output stream name

- **GIVEN** a valid canonical JetStream output with non-empty subjects and no `stream_name`
- **WHEN** the canonical generic provisioner collects component-derived stream declarations
- **THEN** it derives the physical stream name from the output subjects
- **AND** no other consumer or component-local helper performs that derivation

#### Scenario: Non-JetStream output does not contribute a stream

- **GIVEN** a valid output whose canonical kind is not `jetstream`
- **WHEN** component-derived stream declarations are collected
- **THEN** that output contributes no generic stream declaration
- **AND** its concrete configuration type is not inspected for stream-like fields

#### Scenario: Invalid output fails without fallback derivation

- **GIVEN** an output that cannot be decoded or normalized
- **WHEN** stream declarations are collected
- **THEN** collection fails with typed component, port, kind, and field context
- **AND** no stream declaration is inferred from partial or legacy fields

#### Scenario: Gated-DAG specialization remains narrow

- **GIVEN** the approved gated-DAG specialized provisioning path
- **WHEN** its work-queue stream is provisioned
- **THEN** canonical port facts own resource identity, stream name, subjects, storage, and work-queue retention
- **AND** only the local specialized provisioner owns its exact `MaxBytes`, discard-new behavior, `MaxAge`, and
  deduplication policy
- **AND** that exception does not authorize generic provisioners or other consumers to infer port meaning

### Requirement: Trajectory KV and evidence ObjectStore bypass ordinary stream provisioning

`AGENT_TRAJECTORIES` SHALL be acquired as a KV bucket with history `1` and no TTL. Its immutable per-attempt keys make
each successful Create both the current fact and the watch notification. Prefix listing and watch initial replay SHALL
rehydrate visible facts after restart. The bucket SHALL NOT be declared, bounded, reconciled, or repaired as an
ordinary JetStream stream.

The shipped `AGENT_CONTENT` evidence bucket SHALL be owned by the registered ObjectStore component. Its `OBJ_` backing
stream SHALL remain outside ordinary stream provisioning. Agentic-loop's `kv-write` fact output and StoreRegistry
evidence writes SHALL NOT contribute generic JetStream declarations.

Foundation B SHALL apply no automatic fact TTL, evidence expiry, or guessed retention horizon. Reference-aware
evidence reclamation, treatment of deliberately reclaimed evidence, and legal/privacy deletion policy remain a later
retention ruling. The absence of that policy SHALL NOT block the history-1/no-TTL foundation.

The `kv-or-stream` classification is KV fact/watch, not JetStream queued work: readers need visible immutable facts
after restart, multiple readers may observe them, and no processing ACK/redelivery contract belongs to the audit log.
Existing agent task/model/tool requests remain on their separately declared JetStream work paths.

#### Scenario: trajectory facts provision through the KV owner

- **GIVEN** the canonical `AGENT_TRAJECTORIES` descriptor
- **WHEN** the trajectory store is acquired
- **THEN** the KV bucket has history 1 and no TTL
- **AND** the ordinary stream provisioner never creates or reconciles its `KV_` backing stream

#### Scenario: evidence provisions through the ObjectStore owner

- **GIVEN** a shipped `objectstore` component configured with bucket `AGENT_CONTENT`
- **WHEN** storage starts
- **THEN** the ObjectStore component owns acquisition of its physical bucket
- **AND** the ordinary stream provisioner never creates, bounds, or reconciles its `OBJ_` backing stream

#### Scenario: restart replay exposes current immutable facts

- **GIVEN** trajectory facts committed before a process restart
- **WHEN** a reader prefix-lists the loop or starts a matching KV watch
- **THEN** current per-attempt facts are returned without cache hydration
- **AND** the watch's initial replay is treated as fact rehydration rather than queued-work redelivery

#### Scenario: Foundation B guesses no retention horizon

- **WHEN** trajectory KV and evidence Store configuration are inspected
- **THEN** no fact TTL or automatic evidence expiry is present
- **AND** no caller-computed retention knob is required to use the audit path

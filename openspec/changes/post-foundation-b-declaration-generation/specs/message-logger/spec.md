## Purpose

Defines message-logger as an optional default-off raw-body diagnostic whose started wildcard mode observes accepted
runtime declarations. It is neither application log-level behavior nor a production-default service.

## ADDED Requirements

### Requirement: Message-logger activation is explicit and next-boot only

As an optional service, message-logger SHALL use outer `ServiceConfig.Enabled` as its sole activation input. The loader
SHALL NOT inject a default message-logger entry, and the global `slog` level SHALL NOT activate or suppress the service.

Inner `enabled` and `log_level` SHALL be removed and strictly rejected without aliases or compatibility shims. Omitted
or outer-disabled message-logger SHALL create no runtime instance, buffer, observer, subscription, route, delivery
work, capture, or backpressure.

#### Scenario: Omission is fully off

- **GIVEN** no message-logger service entry
- **WHEN** the process composes services
- **THEN** no message-logger runtime resource exists

#### Scenario: Global log level does not control activation

- **GIVEN** outer-enabled message-logger and any supported global application log level
- **WHEN** the service starts
- **THEN** it attaches its configured runtime resources independently of that global level

#### Scenario: Retired inner activation fields fail

- **WHEN** message-logger inner config contains `enabled` or `log_level`
- **THEN** strict config validation fails
- **AND** no alias or deprecated decoder accepts the field

### Requirement: Started wildcard logger reconciles complete Registry declaration state

Only an outer-enabled, started message-logger in `"*"` mode SHALL attach the Registry observer. Start SHALL reconcile
the complete current declaration set, including empty, and later successful add, replacement, and removal. Stop SHALL
detach the observer and every logger-owned subscription.

The logger SHALL observe only declared `nats`, `nats-request`, and `jetstream` subjects, union explicit operator
subjects, and SHALL NOT implicitly capture bare wildcards, inboxes, replies, or undeclared traffic. Registry mutation
SHALL NOT block on logger reconciliation.

#### Scenario: Started wildcard logger follows complete sets

- **GIVEN** an enabled logger configured with `"*"`
- **WHEN** Start runs and generations are later added, replaced, and removed
- **THEN** logger subscriptions reconcile each newest complete declared set

#### Scenario: Stop releases observation

- **WHEN** a started logger stops
- **THEN** its Registry observer and every logger-owned subscription are cancelled

### Requirement: Resolved observation expansion is exact and inspectable

Across the 25 shipped configurations, wildcard discovery SHALL preserve the measured migration from 389 raw subject
rows, 245 summed per-configuration exact keys, and 51 global strings to 565 effective rows, 380 keys, and 66 strings.

The 176-row / 135-key / 15-string delta SHALL have zero removals. Exact deduplication SHALL account for 40
loop/dispatch collapses and one governance collapse. The three named `configs/agentic.json` wildcard-containment
overlaps SHALL produce no duplicate capture, and runtime inspection SHALL expose the resolved union and overlap
handling.

The exact accepted containment overlaps are:

- new `agent.toolcall.proposed.*` under raw `agent.toolcall.proposed.>`;
- raw `agent.toolcall.approved.*` under new `agent.toolcall.approved.>`; and
- raw `agent.toolcall.rejected.*` under new `agent.toolcall.rejected.>`.

#### Scenario: Shipped census remains exact

- **WHEN** effective generations are constructed for every enabled component in all 25 shipped configurations
- **THEN** the raw and effective totals equal 389/245/51 and 565/380/66 respectively
- **AND** the delta equals 176/135/15 with zero removals and 41 exact collapses

#### Scenario: Wildcard containment does not duplicate capture

- **GIVEN** one of the three exact accepted `configs/agentic.json` containment overlaps named above
- **WHEN** matching traffic is observed
- **THEN** the message is captured once
- **AND** runtime inspection identifies the resolved overlap handling

### Requirement: Message-logger does not predict declarations from raw component config

Message-logger SHALL NOT parse component `PortConfig`, skip malformed raw rows, or infer effective declarations from
construction-time component config. It SHALL use accepted Registry snapshots, including replacement and removal.

#### Scenario: Factory defaults are observed without raw-config parsing

- **GIVEN** a component generation with an effective default subject omitted from raw component config
- **WHEN** wildcard logger reconciles the Registry snapshot
- **THEN** it observes the effective declared subject
- **AND** it performs no raw component-config scan

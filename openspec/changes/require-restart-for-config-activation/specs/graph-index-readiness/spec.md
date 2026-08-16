## MODIFIED Requirements

### Requirement: Readiness is published as watchable KV state in a dedicated bucket

Every ADR-066 envelope producer SHALL continue to publish its readiness JSON to its framework-cataloged key in the
dedicated `GRAPH_STATUS` KV bucket (History 3) on the fixed heartbeat tick. The write SHALL remain the single source for
gauges, point-in-time reads, watch delivery, and liveness freshness. The bucket SHALL remain operational component
state, separate from graph data and graph-ingest mutation. Existing non-Rule producer keys and consumers SHALL remain
unchanged.

For Rule, `process_slot` SHALL be the validated, non-empty `platform.instance_id` sealed into the boot snapshot. Rule
hot-reload admission SHALL fail when it is absent. The Rule readiness key SHALL be stable per
`(process_slot, component_id, pack_id)` and derived from sealed composition through framework-owned grammar. Its
envelope SHALL carry a freshly generated `boot_id` and repeat the stable identities. A new boot SHALL overwrite the
slot's current value and retain ordinary History 3; `boot_id` SHALL NOT be part of the key. Callers SHALL NOT provide
Rule key grammar.

Rule SHALL claim a missing or expired stable key with KV compare-and-set. A fresh key carrying a different `boot_id`
SHALL fail admission with typed `readiness_slot_collision` and SHALL NOT be overwritten. Each heartbeat SHALL update
the claimed KV revision with compare-and-set. Losing that revision SHALL degrade the Rule producer and hot reload; two
live boots SHALL never both claim the same stable readiness slot.

The existing status request/reply subject SHALL remain removed. `GRAPH_STATUS` SHALL be the sole Rule liveness fact;
rule activation SHALL NOT create a parallel membership or heartbeat catalog.

#### Scenario: Rule restart overwrites one stable readiness slot

- **GIVEN** Rule boot B published readiness for process slot S, component C, and pack P
- **WHEN** a replacement boot B' starts with the same S, C, and P
- **THEN** B' overwrites the stable `(S, C, P)` key with an envelope carrying B'
- **AND** no accumulating per-boot key is created
- **AND** activation facts naming B cannot join to B'

#### Scenario: Activation uses the existing liveness fact

- **GIVEN** a rule activation fact identifies B, S, C, and P
- **WHEN** the activation reader classifies it as current or historical
- **THEN** it joins only to a fresh Rule `GRAPH_STATUS` envelope carrying the same B, S, C, and P
- **AND** no second liveness catalog can disagree

#### Scenario: Concurrent slot collision fails closed

- **GIVEN** fresh Rule boot B owns the readiness slot for S, C, and P
- **WHEN** boot B' tries to claim the same slot
- **THEN** compare-and-set rejects B' with `readiness_slot_collision`
- **AND** B' does not overwrite B or claim hot reload ready

### Requirement: Consumers distinguish not-ready from status-unknown

A readiness consumer SHALL judge status freshness by consumer-local arrival time. Held status SHALL be fresh only
within three producer heartbeat intervals and SHALL be `unknown` afterward. A fresh not-ready status SHALL defer on
its merits; unknown status SHALL fail closed, subject only to the existing `allow_ungated_reads` standalone escape.

An instance-aware consumer SHALL require exact `boot_id`, process-slot, component, and pack identity. A fresh status
for a different incarnation SHALL NOT make the requested incarnation current. If identity or freshness cannot be
established, the result SHALL be `unknown`, never a fallback to another or older key.

#### Scenario: Crashed Rule becomes historical by freshness

- **GIVEN** Rule boot B loses power without deleting its readiness key
- **WHEN** three heartbeat intervals pass without a B update
- **THEN** consumers classify B liveness as unknown/expired
- **AND** matching activation facts are historical rather than current

#### Scenario: Identity mismatch is unknown

- **GIVEN** a fresh Rule status exists for B' but an activation fact names B
- **WHEN** a consumer joins readiness to activation
- **THEN** B' supplies no liveness evidence for B
- **AND** B cannot be reported current

### Requirement: The rule processor MUST report bootstrap replay completion per watcher generation

The Rule processor SHALL publish readiness for its immutable boot-configured entity-watcher set. `BootstrapComplete`
SHALL be true only when every currently-authoritative watcher generation for that set has observed its
end-of-initial-values sentinel. Post-boot configuration SHALL NOT add, remove, or replace a configured bucket/pattern
identity.

An unexpected transport loss MAY create a repair generation only for the same boot-authoritative watcher identity.
During repair, `BootstrapComplete` SHALL return to false until the replacement generation replays. The old generation
SHALL lose dispatch authority atomically and SHALL never regain it. Repair does not authorize a desired configuration
change to enter the running process.

`Start` returning SHALL NOT imply bootstrap completion. `State` SHALL remain `degraded` while watcher transport is
lost or repair is incomplete and `reset_required` when the stored-state contract kill switch has fired.

#### Scenario: Configured watcher edit waits for restart

- **GIVEN** boot B is complete for watcher set W
- **WHEN** desired configuration changes the set to W'
- **THEN** B continues using W and does not reset bootstrap for W'
- **AND** W' is eligible only on the next successful boot

#### Scenario: Same-watcher transport repair resets bootstrap

- **GIVEN** boot-authoritative watcher identity W loses its transport after bootstrap
- **WHEN** the supervisor prepares a replacement transport for the exact same W
- **THEN** readiness becomes degraded and `BootstrapComplete` becomes false
- **AND** readiness returns only after the repair generation observes its sentinel

### Requirement: Aggregate readiness MUST be folded by the consumer, never published

Aggregate readiness SHALL remain a consumer-local fold over declared producer keys and SHALL NOT be published to
`GRAPH_STATUS`. Existing non-Rule producer keys SHALL continue to come from the consumer's `readiness_keys`
configuration.

Rule producer keys SHALL instead be derived by the framework from sealed boot composition using validated
`platform.instance_id`, Rule component identity, and `pack_id`. The consumer SHALL merge those discovered Rule keys
with its configured non-Rule keys before applying the existing single-key readiness gate. An absent, collided, or
unfresh discovered Rule key SHALL fail closed as status-unknown. Neither callers nor operators SHALL construct Rule key
grammar.

#### Scenario: Consumer discovers Rule keys without grammar

- **GIVEN** sealed composition contains Rule components for packs P1 and P2
- **WHEN** the readiness consumer builds its producer set
- **THEN** the framework derives both stable Rule keys from sealed identities
- **AND** configured non-Rule `readiness_keys` remain unchanged
- **AND** the consumer folds all keys through the existing single-key gate

### Requirement: An operator MUST be able to read every watched readiness envelope

The gateway SHALL expose the envelope and consumer-local known/fresh/age facts for every watched `GRAPH_STATUS` key.
Its watched set SHALL include configured non-Rule `readiness_keys` plus Rule keys discovered from sealed composition.
It SHALL NOT require an operator to supply Rule key grammar or publish a computed aggregate verdict.

Process-liveness endpoints SHALL remain separate from data-plane coverage. A Rule readiness response SHALL expose the
stable process-slot/component/pack identity and current envelope `boot_id` so activation observations can name the
exact incarnation without making the key per boot.

#### Scenario: Operator sees discovered Rule incarnation

- **GIVEN** the gateway discovered a Rule readiness key from sealed composition
- **WHEN** an operator reads the readiness surface
- **THEN** the response includes its stable identities, current `boot_id`, freshness, and age
- **AND** the operator supplied no Rule bucket key

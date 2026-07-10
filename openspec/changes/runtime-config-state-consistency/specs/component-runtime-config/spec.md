## ADDED Requirements

### Requirement: Runtime config-map mutations are serialized so a concurrent add/remove is never lost

The shared configuration store MUST serialize each read-modify-write so that two
concurrent mutations cannot drop one another's change. Every site that reads the
current config, mutates it, and swaps it back — the KV-watcher apply path
(`config.Manager.updateConfig`, reached by `PutComponentToKV` / `DeleteComponentFromKV`)
AND the engine caller-goroutine sites (`enableComponent`, `disableComponent`,
`deleteComponentConfig`, `writeComponentConfigs`, `writeToKV`) that share the same
`SafeConfig` instance — MUST perform the whole `read → mutate → swap` under the store's
write lock (e.g. a `SafeConfig.Mutate(fn)` primitive), NOT as a lock-free clone-then-swap.
A component add applied on the caller goroutine concurrently with an unrelated component
change applied by the watcher goroutine MUST NOT lose either change (last-writer-wins on
the whole map is forbidden).

#### Scenario: concurrent add and remove both take effect

- **GIVEN** a config with components A and B
- **WHEN** one goroutine adds component C and another concurrently removes B, interleaving their read-modify-write sequences
- **THEN** the resulting config contains A and C and does not contain B
- **AND** neither mutation is silently dropped

#### Scenario: watcher apply and caller add do not clobber

- **WHEN** the KV watcher applies an external `components.X` update while a caller invokes `PutComponentToKV("Y", ...)` concurrently
- **THEN** the final in-memory config contains both X's update and Y
- **AND** subscribers are notified for both keys

### Requirement: A component's effective config has one source of truth that GET config reflects

The ComponentManager MUST expose a single authoritative source for a component's
effective config, and the config read API (`GET /config/<component>`) MUST derive
its response from that source so it reflects what the component is actually running
— including after a KV-watch-driven restart, not only after a live `PUT`. A second
retained config copy that is refreshed on only some write paths MUST NOT back the
read API; the source of truth is the field refreshed on every write path (create,
KV-restart, and live-PUT).

#### Scenario: GET config after a KV-driven restart returns the new body

- **GIVEN** a running component created with config C
- **WHEN** a KV-watch config change restarts it with config C'
- **THEN** `GET /config/<component>` returns C' (not the stale C)

#### Scenario: GET config after a live PUT returns the applied body

- **GIVEN** a running component that supports live runtime reconfiguration
- **WHEN** a `PUT /config/<component>` applies config C' live
- **THEN** `GET /config/<component>` returns C'

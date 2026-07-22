## ADDED Requirements

### Requirement: Content ObjectStores carry no lifecycle retention

Content ObjectStores MUST NOT use NATS TTL (`MaxAge`) or a binding `MaxBytes` as a
lifecycle mechanism. This covers every ObjectStore holding ref-addressed
`ContentStorable` payloads — the generic message store, the agent content bucket, and
the embedding evidence store — because their objects are referenced by live-graph
entities that outlive them, and age/size eviction is reachability-blind (ADR-068): it
would strand an entity pointing at content that has silently expired. The shared
ObjectStore constructor MUST NOT stamp any retention on the backing stream, and no
zero-valued TTL knob is exposed on the configuration surface.

Enforcement is boot-time and self-healing: on start, each content store's backing
stream (`OBJ_<bucket>`) is inspected; any binding `MaxAge`/`MaxBytes` is stripped in
place and logged (covering legacy buckets the constructor's create-or-get path would
otherwise never reconcile), then re-asserted — if retention is still binding, startup
fails closed rather than proceeding to silently expire evidence.

#### Scenario: the constructor stamps no retention on a content store

- **GIVEN** a content ObjectStore is created through the shared constructor
- **WHEN** its backing stream configuration is built
- **THEN** the backing stream carries `MaxAge` `0` and no binding `MaxBytes`, and no
  TTL field is present on the store configuration surface

#### Scenario: boot strips a legacy retention config and warns

- **GIVEN** a content ObjectStore whose backing stream already carries a non-zero
  `MaxAge` (e.g. the historical 24h TTL) from before this contract
- **WHEN** the store starts and inspects the backing stream
- **THEN** the retention is cleared in place via a stream update and a warning is
  logged naming the bucket and the removed retention
- **AND** no stored object is deleted by the reconciliation

#### Scenario: boot fails closed when retention cannot be stripped

- **GIVEN** a content ObjectStore whose backing stream carries a binding
  `MaxAge`/`MaxBytes` that the reconciliation could not clear
- **WHEN** the store re-asserts the backing stream configuration after reconciliation
- **THEN** startup fails with a fatal error naming the bucket and its offending
  retention, rather than proceeding

#### Scenario: a clean content store boots normally

- **GIVEN** a content ObjectStore whose backing stream has `MaxAge` `0` and no binding
  `MaxBytes`
- **WHEN** the store starts and inspects the backing stream
- **THEN** the guardrail passes and startup proceeds

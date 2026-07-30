## ADDED Requirements

### Requirement: A no-op runtime config update does not restart a running component

The ComponentManager MUST restart an existing enabled component on a per-component
runtime config update ONLY when the component's effective `ComponentConfig` differs
from the config it is currently running. A per-component update whose effective
config is unchanged MUST be a skipped no-op — no `Stop`/`Start` cycle, no store
deregistration, no port re-acquisition, and no HTTP-handler re-registration — so
that a full-config sync or a repeated identical write cannot churn a healthy
running component. This idempotency protects components that own external
resources, hold subscriptions or long-lived connections, or register handlers into
a one-shot mux (where re-registration would panic). To compare, the manager MUST
retain each managed component's effective `ComponentConfig`.

A changed effective config MUST still drive exactly one restart via the existing
graceful `restartComponentWithNewConfig` path. Creating a missing enabled component
and stopping a disabled or removed one are unaffected. The bulk
`reconcileComponents` path remains conservative (it already does not restart
already-running components) and is unchanged.

#### Scenario: an identical config update is a no-op

- **GIVEN** a running enabled component with effective config C
- **WHEN** a per-component config update with an effective config equal to C is received
- **THEN** the component is not stopped and not started
- **AND** no store deregistration, port re-acquisition, or handler re-registration occurs
- **AND** the manager logs the update as a skipped no-op

#### Scenario: a changed config update restarts exactly once

- **GIVEN** a running enabled component with effective config C
- **WHEN** a per-component config update with an effective config C' ≠ C is received
- **THEN** the component is restarted exactly once via the graceful restart path
- **AND** the manager retains C' as the component's effective config

#### Scenario: bulk reconcile with unchanged configs restarts nothing

- **GIVEN** a set of running enabled components whose effective configs are unchanged
- **WHEN** a bulk `components.*` reconcile is processed against the full config
- **THEN** no running component is restarted
- **AND** missing enabled components are still created and disabled/removed ones still stopped

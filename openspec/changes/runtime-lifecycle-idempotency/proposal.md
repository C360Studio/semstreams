## Why

Runtime lifecycle operations are not idempotent in two places, and both bite
components that own external resources. (1) `ComponentManager.handleComponentConfigUpdate`
restarts an existing enabled component **unconditionally** on any per-component
config notification — with no check that the effective config actually changed —
so a full-config sync or a repeated write can stop/start-cycle a healthy running
component. For a component that registers HTTP handlers into a one-shot
`http.ServeMux`, that restart re-registers the same pattern and **panics**; for
components holding subscriptions or long-lived connections it drops and rebuilds
them for nothing. (2) During coordinated shutdown, a service that already observed
parent-context cancellation before `Manager.StopAll` reached it is in a
stopped/stopping terminal state that must be read as clean success, not aggregated
as a fatal stop error.

## What Changes

- `ComponentManager` retains each managed component's **effective `ComponentConfig`**
  and, on a per-component config update for an existing enabled component, restarts
  **only when the effective config changed**. An unchanged (no-op) update is a
  logged skip — no `Stop`/`Start`, no store deregistration, no port churn, no
  HTTP-handler re-registration.
- The `component-runtime-config` capability gains a requirement pinning this
  no-op-update-is-idempotent contract. (The bulk `reconcileComponents` path is
  already conservative and unchanged.)
- A new `service-shutdown` capability pins the coordinated-shutdown idempotency
  contract: `Manager.StopAll` treats an "already stopped/stopping" terminal state
  as success and does not aggregate it as a fatal error; the framework `Stop`
  contract (already honored by `BaseService.Stop`) is made explicit and the
  `StopAll` aggregation is hardened to not fail a clean shutdown.
- Consolidates **gh#514** (config-equality guard) into this change. gh#515
  (`updateConfig` lost-update race) is related but out of scope.

Not breaking: both changes only *remove* spurious restarts / spurious errors on
paths that were previously churning or falsely failing. No public API, config
surface, or wire contract changes.

## Capabilities

### New Capabilities

- `service-shutdown`: coordinated teardown of registered services via
  `Manager.StopAll` — ordering, per-service `Stop` error aggregation, and the
  idempotency contract for already-stopped/stopping services during
  parent-context-cancellation shutdown ordering.

### Modified Capabilities

- `component-runtime-config`: add a requirement that a per-component runtime config
  update restarts a running component **only** when its effective `ComponentConfig`
  changed; a no-op update must not stop/start-cycle the component.

## Impact

- **Code**: `service/component_manager.go` (`handleComponentConfigUpdate`,
  `restartComponentWithNewConfig`, `CreateComponent`/managed-component bookkeeping
  to retain effective config); `component/lifecycle.go` (`ManagedComponent` gains
  a retained config field, or a CM-side per-name config map); `service/service_manager.go`
  (`StopAll` aggregation); `service/base.go` (`BaseService.Stop` already compliant —
  covered by contract test, no behavior change).
- **Config equality**: needs a total equality over `types.ComponentConfig`
  (`reflect.DeepEqual` or a generated `Equal`) — design decision.
- **Consumers**: every sem* product that runs the framework with runtime config
  sync (semsource e2e config sync, semboids) and every deployment that performs a
  graceful `StopAll` shutdown. No consumer code changes required.
- **Issues**: closes gh#520 and gh#514; cross-links gh#515.

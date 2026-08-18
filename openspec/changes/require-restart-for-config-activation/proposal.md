# Change: Make boot the component-composition activation boundary

## Why

SemStreams currently treats broad configuration writes as commands against the running process. ComponentManager can
watch configuration and mutate running composition even though no shipped configuration enables that general behavior
and no production component implements its generic live-update hook.

The demonstrated live-authoring need is narrower: rule create/update/delete followed by activation inside an already
running Rule processor. This change separates durable desired state, sealed boot-effective composition, and that one
dedicated rules-only hot-reload capability.

## What changes

- Treat service and component composition as immutable after successful boot.
- Keep config KV, flow storage, rule storage, schemas, validation, and authoring APIs as durable desired state.
- Make flow deploy/start/stop/undeploy operations mutate desired state only while running and report runtime unchanged,
  restart required.
- Retire ComponentManager config watching, generic live config PUT, runtime create/remove/restart/replace, and
  model-registry-triggered component restart.
- Make Registry and observation value-only. ComponentManager remains the sole runtime-handle owner and exposes only
  callback-scoped access; lifecycle behavior for those callbacks is delegated.
- Retire the generic component `UpdateConfig` capability.
- Preserve one bounded exception: rule definitions may hot reload inside an already-running Rule processor while its
  ports, dependencies, watch-bucket set, integration mode, and projection bindings remain boot-only.
- Scope rule authoring to an already-composed `pack_id`, use typed desired tombstones, and make activation
  revision-bound and observable.
- Use KV Watch for desired rule facts and rule-activation facts.

ADR-095 and `simplify-one-shot-lifecycle-ownership` are external prerequisites and exclusively own generic
component/service callback-borrow shutdown, Stop/Close terminal sequencing, native handle lifecycle, failed-Start
cleanup, ACK ordering, settlement, controlled/dirty recovery, and lifecycle proof. This change owns only Rule-specific
activation terminalization—fencing status publication and canceling/joining Rule-local work—executed under simplify's
generic lifecycle contract. It receives no generic lifecycle task or proof completion credit from that dependency.

## Capabilities

### New capability

- `rule-hot-reload`: bounded live rule-definition activation and revision-bound outcome truth.
- `flow-activation-truth`: durable desired activation and independently observed effective runtime state.

### Delegated dependency capability

- `restart-safe-shutdown`: ADR-095 and `simplify-one-shot-lifecycle-ownership` own all lifecycle mechanics and proof
  required before boot-only activation can claim release readiness.

### Modified capabilities

- `component-runtime-config`: desired component configuration is next-boot state; generic live apply and generation
  replacement are retired.
- `service-composition`: all service and component composition is sealed at boot, with the dedicated Rule exception.
- `component-discovery`: Registry admission is boot-owned and has no live replacement/removal protocol or lifecycle
  authority.
- `framework-composition`: the component-start barrier consumes one boot snapshot and has no late boot-drain or
  post-boot dynamic Start path.
- `graph-index-readiness`: Rule readiness gains boot-incarnation identity and remains the sole Rule liveness fact;
  configured entity-watch membership becomes boot-only.
- `rule-entity-watching`: watcher generation repair preserves the boot-authoritative configured pattern set.

## Impact

- **Breaking API and behavior:** ComponentManager live mutation methods, Registry replacement, generic config PUT, and
  live flow-topology activation retire without compatibility shims.
- **Preserved authoring:** flow and rule definitions remain durable and validated. Flow mutations become
  pending-next-boot; rule-definition mutations retain bounded hot activation.
- **Flow truth:** desired activation and sealed runtime-effective state are distinct and observable.
- **Observability:** rule writers receive a desired revision and can observe its typed terminal activation outcome
  without knowing operational KV grammar. Flow writers receive an honest restart-required result.
- **Deployment identity:** Rule hot reload requires a validated stable `platform.instance_id`; competing ownership of
  one readiness slot fails admission through compare-and-set.
- **Migration:** sister repositories remain read-only. Downstream owners update and validate their own repositories.
- **Release dependency:** this change cannot claim activation or release readiness until the simplify lifecycle proof
  passes. It owns no generic lifecycle task or shutdown mechanism, controlled/dirty proof, or E2E lifecycle gate; its
  only lifecycle-adjacent work is Rule-specific activation terminalization under simplify's generic contract.

# Design: Durable MaxDeliver occurrence visibility

## Surface inventory

| Responsibility | Current owner / evidence before this change | Consumer / consequence |
|---|---|---|
| Framework stream provisioning before component start | `cmd/semstreams/main.go` and `cmd/e2e-semstreams/main.go` call `StreamsManager.EnsureStreams` before `Manager.StartAll`; declarations resolve in `config/streams.go` and `config/stream_bounds.go`. | Every configured component consumer assumes its input stream already exists. |
| Application consumer retry ceiling | `component/port_jetstream.go` projects `max_deliver`; `storage/objectstore/component.go` passes it to the durable consumer and NAKs transient failures. | JetStream stops delivery at the declared ceiling. This change does not reinterpret or change it. |
| MaxDeliver occurrence source | NATS server publishes the typed `io.nats.jetstream.advisory.v1.max_deliver` event on `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.{stream}.{consumer}`. | Previously no SemStreams consumer; the occurrence was ephemeral to the framework. |
| Capacity reporting | `natsclient/storage_inventory.go` and `service/storage_observability*` report account resources and pressure. | Operators consume capacity truth, not delivery-failure occurrences. Extending it would conflate a resource gauge with an event ledger. |
| Platform metrics | `metric.MetricsRegistry` owns the Prometheus registry and accepts service-specific collectors. | Operator scraping is the present consumer of the new occurrence and decoder counters. |
| Structured runtime logging | Both binaries construct one `slog.Logger` before service startup. | Operators get the advisory ID and sequence for incident correlation/deduplication. |

No existing owner durably captures the advisory. Search terms included `MAX_DELIVERIES`, `MaxDeliver`, `advisory`,
`parked`, `exhaust`, stream provisioning, consumer metrics, and ObjectStore acknowledgement disposition. The only
existing advisory subscription is the ObjectStore poison-message `MSG_TERMINATED` integration test; it is a test
observer, not runtime ownership.

## Adopter seam inventory

There are two specific adopters: a developer outside this repository declaring an ordinary component with a finite
`max_deliver` value, and an operator deploying SemStreams under restrictive NATS authorization.

| Question | Answer |
|---|---|
| What must they know? | Component authors: nothing new. Restrictive-deployment operators: the runtime principal must be allowed to provision/reconcile the fixed stream, bind/consume/ACK the fixed durable, and subscribe to NATS request inboxes. |
| What happens if they do nothing? | Component authors gain retained occurrence telemetry without a behavior change. A restrictive deployment missing a required permission fails boot during central stream provisioning or observer binding; it does not start silently without coverage. |
| Where do they find out? | Operators find the tested sufficient subject grants in the ACL section below and permission failures in boot logs. Runtime occurrences appear in `semstreams_nats_max_delivery_exhaustions_total{domain,stream,consumer}` and structured logs keyed by `advisory_id`. |
| What should they have to know? | Component authors should know nothing beyond alerting on the metric. Operators should grant framework runtime capabilities, but never choose the capture subject, stream name, retention, durable identity, or observer retry ceiling. |

The framework owns every value needed to observe the real server outcome, so there is no adopter knob. Asking an
application to predict advisory rate, boot order, or the correct system subject would violate the adopter seam rule.

## Decision: a JetStream occurrence ledger

The `kv-or-stream` restart test decides this sharply: after an observer restart, it must resume unacknowledged
occurrences without replaying acknowledged work. That is JetStream Stream semantics. KV would represent current state,
but no authoritative current parked set exists; Core NATS would lose downtime incidents. A fixed durable consumer also
passes the queue test: across replicas, exactly one observer should emit the occurrence.

The capture stream uses LimitsPolicy rather than WorkQueuePolicy so acknowledged occurrences remain available as a
bounded incident ledger. This is occurrence history, not redrive state and not an authoritative current count.

## Fixed declaration and retention sizing

`MAX_DELIVERY_EVENTS` captures exactly `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>` with:

| Field | Value | Reason |
|---|---:|---|
| Storage | file | Incidents survive process/server restart on the hosting node. |
| Retention | LimitsPolicy | Acknowledgement does not erase incident history. |
| Discard | old | At the ceiling, retain the newest operational evidence rather than reject the server's next incident event. |
| MaxAge | 168h | A bounded seven-day investigation horizon. |
| MaxBytes | 64 MiB | Backstop sized for an incident storm without becoming an unbounded audit stream. |
| Replicas | 1 | Matches existing framework-stream policy and works on single-node deployments. |

The representative typed payload in `internal/maxdelivery/observer_test.go` is below 512 bytes; that bound is asserted
in the test. Using 512 bytes per stored occurrence as conservative payload arithmetic, 64 MiB holds at least 131,072
payloads before JetStream storage overhead, equivalent to 18,724/day, 780/hour, or 13/minute across seven days.
JetStream overhead reduces the exact count, so these are sizing inputs, not a completeness guarantee. Operators can
inspect the stream's actual state through existing storage inventory.

Completeness is limited by the earlier of MaxAge and MaxBytes, server/account availability, permissions, and the
single replica's availability. DiscardOld makes truncation oldest-first. This is deliberately not presented as an
exhaustive audit trail.

## Observer and acknowledgement contract

Every replica binds durable `semstreams-max-delivery-observer`. JetStream distributes each delivery to one active
binding. The consumer is explicit-ack, DeliverAll, and unlimited MaxDeliver.

For a valid typed event:

1. Validate JSON, exact type, required ID/timestamp/stream/consumer/sequence/deliveries, and subject-to-payload match.
2. Increment `semstreams_nats_max_delivery_exhaustions_total{domain,stream,consumer}`.
3. Emit structured ERROR with advisory ID, timestamp, domain, stream, consumer, stream sequence, and deliveries.
4. ACK and verify the broker advances the durable floor in runtime proof.

The ID and sequence never become metric labels. If telemetry reporting fails, NAK so the durable observer retries. If
the event is malformed, wrong-type, incomplete, or subject-mismatched, increment
`semstreams_nats_max_delivery_advisory_decode_errors_total{reason}`, emit ERROR, and ACK the poison event.

ACK/NAK calls can fail after the observer has bound (for example, a permission can be revoked asynchronously). The
observer checks every settlement result. Failure increments
`semstreams_nats_max_delivery_advisory_settlement_errors_total{operation}`, emits ERROR, and leaves the durable
delivery pending for broker redelivery; it never reports settlement as successful merely because the callback ran.

A crash after telemetry but before ACK can duplicate the log and counter. The structured `advisory_id` is the stable
deduplication key for incident processors. Prometheus counters are at-least-once occurrence signals, not exact billing.

## Boot and failure semantics

Both binaries execute this sequence:

1. Connect to NATS.
2. Resolve every stream declaration without I/O, rejecting a configured `MAX_DELIVERY_EVENTS` collision.
3. Provision/reconcile all streams, including the fixed advisory ledger, through the one central stream manager.
4. Construct metrics and logger.
5. Bind the fixed durable observer.
6. Construct and start services/components through `Manager.StartAll`.

Therefore no component owned by these binaries can begin consuming before the capture stream exists. Failure to
provision or bind the framework observer fails boot rather than running with silent loss. Once running, an occurrence
does not alter component health, readiness, consumer policy, or data-plane disposition. No observer-up gauge is
published because asynchronous permission loss cannot be measured honestly by the current consumer API.

## ACL and cluster semantics

The runtime proof demonstrates the following **sufficient, narrowly scoped** grants (a broader existing `$JS.API.>`
grant also covers the API rows). It subtractively proves stream API, STREAM.UPDATE, reply-inbox, and consumer-create
permission classes. It does not claim that every individual consumer-next or ACK token is independently minimal:

| Direction | Subject grant | Purpose |
|---|---|---|
| publish | `$JS.API.STREAM.INFO.*` | Inspect central stream declarations before create/reconcile. |
| publish | `$JS.API.STREAM.CREATE.*` | Provision any missing central stream, including `MAX_DELIVERY_EVENTS`. |
| publish | `$JS.API.STREAM.UPDATE.*` | Reconcile editable central stream drift. |
| publish | `$JS.API.CONSUMER.INFO.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer` | Bind the existing fixed durable. |
| publish | `$JS.API.CONSUMER.CREATE.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer.>` | Create/update the fixed filtered durable. |
| publish | `$JS.API.CONSUMER.MSG.NEXT.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer` | Pull the next retained occurrence. |
| publish | `$JS.ACK.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer.>` | ACK/NAK occurrence deliveries. |
| subscribe | `_INBOX.>` | Receive JetStream API and pull-consumer replies. |

The account must permit the framework to declare a stream capturing
`$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>`. The runtime principal does **not** need a direct Core subscription ACL
to that advisory wildcard: the server captures it, while the observer consumes through its inbox. Upgrade discovery
is fail-loud: missing stream API or inbox permission aborts central provisioning, missing STREAM.UPDATE fails drift
reconciliation, and missing consumer-create permission aborts observer binding. A later ACK/NAK failure is visible via
the settlement-error counter and ERROR and leaves the delivery pending. Add these grants before deploying the
upgraded binary; there is no configuration or schema migration.

With several SemStreams replicas, all bind the same durable; only the replica receiving an occurrence increments its
local scrape target. Fleet alerts must aggregate the counter across instances. The capture stream has one replica, so
it is not highly available across a JetStream node loss. Raising replication is a future framework deployment-policy
decision, not an application knob hidden in this change. The three-node runtime proof deliberately provisions R=1,
requires all nodes to have two routes and current JetStream metadata, requires exactly one metadata leader whose API
reports three peers (the peer-list API is leader-scoped), binds clients through different nodes, and demonstrates one
retained message and one logical handling. It does not claim replicated storage availability.

## Options rejected

| Option | Rejection |
|---|---|
| Infer current parked count from consumer counters/AckFloor | NATS accounting does not preserve that semantic state; later acknowledgements can move the floor. |
| Direct Core NATS subscriber | Loses incidents while every SemStreams process is down. |
| KV row per stream/consumer | Invents current-state authority and replacement/removal semantics the server advisory does not provide. |
| Raise or remove application MaxDeliver | Changes retry/outage policy and does not solve account-wide visibility. |
| Add redrive/API/DLQ behavior | Expands from report-only occurrence visibility into operator action and message ownership. |
| Put it inside storage capacity report | Conflates event occurrence with resource capacity/current state. |
| Adopter-configurable stream/durable/retention | Makes callers predict framework-owned values and creates cross-replica identity drift. |

## Binding ruling conformance

The final file:line evidence is maintained in `tasks.md` after implementation and verification. Any unimplemented row
is a deviation requiring owner re-ruling; there are no silent substitutions.

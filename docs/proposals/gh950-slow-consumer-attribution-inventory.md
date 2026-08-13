# GitHub #950 Slow-Consumer Attribution Inventory

Baseline: `0f98936692a1c4910fe4353ee5db503a3f1393c4`

Phase: `inventory-only`

Body SHA-256: `4e95a7623271621cce8aa8123d41c1ce20cf4c3f87c4d4a99e5219fc2d23e6b3`

## Inventory body

No files were changed and no tests were run during the read-only inventory. The worktree was clean. Live GitHub state
was verified: #950 and #586 are both open.

### Claimed gap

GitHub #950 claims that `natsclient` receives enough information from nats.go to identify a slow subscription but
discards it, leaving operators unable to tell which watcher or subscription dropped messages. The repository confirms
the gap:

- The sole connection-wide async error registration is `nats.ErrorHandler(m.handleError)`:
  `natsclient/client.go:405-417`.
- `handleError` discards both the connection and `*nats.Subscription`, logging only `"NATS error"` plus `error`:
  `natsclient/client.go:1705-1709`.
- It deliberately does not call `recordFailure`; subscription errors do not alter failure count, circuit state, or
  last-failure time: `natsclient/client.go:253-307,1705-1709`.
- The exported `natsclient.Subscription` wrapper exposes only `Unsubscribe`; it hides the raw subscription and its
  diagnostics: `natsclient/client.go:841-852`.
- `KVStore.Watch` returns only `jetstream.KeyWatcher`; it exposes no underlying subscription identity or pending/drop
  counters: `natsclient/kv.go:593-600`.

No existing exported or unexported spelling for slow-consumer attribution or pending-limit control exists in
`natsclient`. This search produced no relevant production matches; its sole match was the unrelated test name
`IllegalNameIsPublishedNotDropped` at `natsclient/storage_report_test.go:682`:

```text
rg -n '(ErrSlowConsumer|slow[ _-]consumer|Dropped\(|PendingLimits\(|SetPendingLimits\(|WithPending|SubChanLen)' \
  natsclient --glob '*.go'
```

### Every current spelling of the fact

| Surface / owner | Existing spelling and behavior |
|---|---|
| nats.go connection async-error carrier | `go.mod:12` pins nats.go v1.52.0. `ErrHandler` is `func(*Conn, *Subscription, error)` (`nats.go:226-228`); `ErrorHandler` installs it (`:1327-1333`). On a client-side pending-limit overflow, nats.go increments `sub.dropped`, marks `SubscriptionSlowConsumer`, sets the connection last error, and invokes the callback with the exact `sub` (`:3880-3900`). |
| nats.go subscription identity | `Subscription.Subject` is the subscribed pattern and `Queue` is the queue group (`nats.go:705-718`). This is subscription identity, not necessarily the concrete received subject for wildcard subscriptions. |
| nats.go diagnostics | `Pending()` returns current queued messages/bytes (`nats.go:5588-5602`); `PendingLimits()` returns configured limits (`:5645-5661`); `Dropped()` returns cumulative known pending-limit drops (`:5697-5710`). Defaults are 500,000 messages/64 MiB (`:5637-5643`). Calls can fail for nil, closed, or channel subscriptions; `Dropped()` warns that its value may be invalid for a server-declared connection slow consumer. |
| nats.go connection status spelling | nats.go stores `ErrSlowConsumer` as the connection `LastError` (`nats.go:3895-3898,4148-4158`). No in-repo production caller reads `LastError`, and it carries no subscription identity. |
| SemStreams subscription logging | `handleError` emits one event shape: message `NATS error`, attribute `error`; no subject, queue, pending, limits, dropped count, or error class: `natsclient/client.go:1705-1709`. |
| SemStreams async-publish collision | The distinct `asyncPublishErrHandler` attributes failed async publish acknowledgements with `msg.Subject`, updates the circuit, and optionally records a JetStream operation error: `natsclient/client.go:1045-1062`. Tests cover failure accounting and nil messages: `natsclient/publish_async_test.go:62-93`. It does not model inbound subscription overflow. |
| Connection status and health | `natsclient.Status` contains connection state, failure count, last failure, reconnects, and RTT: `natsclient/client.go:58-65,450-468`. `IsHealthy` means only connected: `natsclient/client.go:238-241`. `/health` reports a connected NATS client healthy with RTT and otherwise reports connection status/failure count: `service/service_manager.go:1218-1228`. Subscription drops can coexist with healthy status. |
| natsclient metrics | `WithMetrics` creates only `jetstreamMetrics`: `natsclient/options.go:207-222`. It covers tracked stream state, JetStream consumer pending/delivered/acked/redelivered, and operation errors: `natsclient/jetstream_metrics.go:13-34,42-132,196-211`. No core-NATS subscription drop/error series or subscription-subject label exists. |
| Platform metrics and logging | Core NATS metrics declare connected, RTT, reconnect, and circuit-breaker series only: `metric/core.go:25-30,117-152`. The production logger can count WARN+ as `semstreams_log_entries_total{component,level}`: `cmd/semstreams/logging.go:31-74`, `pkg/logging/counter_handler.go:10-59`; it has no subject/error label. |
| Primary runtime wiring | Production constructs the NATS client before the metrics registry/full logger and calls `natsclient.NewClient(natsURLs)` with no options: `cmd/semstreams/main.go:113-132,385-397`. E2E does the same: `cmd/e2e-semstreams/main.go:110-122,513-523,565-589`. Neither passes `WithMetrics` nor `WithLogger`. Because `NewClient` captures `slog.Default()` at construction (`natsclient/client.go:154-158`), client async errors also bypass the later production NATS log handler and WARN+ counter. |
| Graph-view adjacent drops | `pkg/graphview` owns local fan-out coalescing, not NATS subscription overflow. Its hooks report watcher loss and slow local subscriber overwrites: `pkg/graphview/view.go:51-84,728-757,823-859`. Dispatch exports corresponding view metrics: `processor/agentic-dispatch/metrics.go:37-45,186-234,253-259`. These are not an equivalent attribution surface. |
| Tests | No `natsclient` test mentions `handleError`, `NATS error`, or `ErrSlowConsumer`. Graph-index load tests scrape aggregate server `/varz slow_consumers` and require zero: `processor/graph-index/owner_filter_load_integration_test.go:100-106,442-449`; `processor/graph-index/predicate_layout_smoke_integration_test.go:120-126,670-695`. They neither trigger nor attribute client-side buffer drops. |

Exact empty searches:

```text
rg -n '(handleError|NATS error|ErrSlowConsumer)' natsclient/*_test.go
# 0 matches

rg -n '(drop|Drop|slow|Slow)' health/status.go
# 0 matches

rg -n '(natsclient\.WithMetrics|natsclient\.WithLogger)' cmd/semstreams cmd/e2e-semstreams --glob '*.go'
# 0 matches
```

### Adjacent claims on the territory

- ADR-081 distinguishes shared-view local backpressure/watcher loss from raw `WatchAll`, then explicitly splits two
  independent `natsclient` ergonomics from graph-view work: slow-consumer attribution and watcher pending-limit
  configuration: `docs/adr/081-graph-view-subscription.md:1-225`, specifically `:223-225`.
- Open #586 predates #950 and combines those two asks. Open #950 narrows to attribution and adds measured SemSource
  evidence: a 32,157-entity corpus, an approximately 22-minute seed on 2 CPU, and approximately 124 unattributed
  slow-consumer ERROR lines. It says PubAck/scored-retrieval evidence showed the JetStream ingest plane unaffected;
  that external scorecard is not in this repository.
- `openspec/specs/nats-streaming/spec.md` governs publish paths and async publish acknowledgements, not inbound
  subscription errors.
- `openspec/specs/graph-view-subscription/spec.md` governs shared-view readiness, local subscriber coalescing, and
  watcher loss; it does not specify connection-global nats.go async-error attribution.
- The issue census classifies #586 as deferred operational observability/config work requiring measured need:
  `docs/proposals/post-g-foundation-remap-issue-census.tsv:55`. #950 supplies measured attribution need but does not
  supply a present metric consumer or resolve pending-limit API design.

### Consumer at birth

- Present consumer of attributed log facts: the external SemSource operator diagnosing the recorded scale run. This
  is a current consumer, not a prospective observability use.
- Present in-repo consumer of a new per-subscription metric: none found. No dashboard, alert, PromQL query, health
  gate, or status reader names such a series; #950 calls a metric optional.
- Existing generic consumers are stdout readers, the production NATS log forwarder, and
  `semstreams_log_entries_total`; current primary runtime wiring prevents the captured `natsclient` logger from
  reaching the latter two.

No same-class collision table is required: this inventory reaches ephemeral log/metric attribution, not a new
durable, communication, or coordination primitive. The adjacent graph-view drop metrics are a different local
fan-out class and are enumerated above to prevent semantic conflation.

### Adopter seam inventory

Specific adopter: a developer outside this repository writing a SemStreams component using `natsclient.Subscribe` or
a KV watcher.

1. **What must they know?** nats.go may drop inbound messages when a subscription exceeds its pending limits;
   SemStreams hides the raw diagnostics; and the emitted ERROR does not name the subscription. Diagnosis requires
   reconstructing topology from code and understanding nats.go's connection-wide callback.
2. **What happens if they do nothing?** nats.go records known client-buffer drops and SemStreams emits a generic
   ERROR. Connection status, health, and circuit state can remain healthy; no subscription-specific metric appears.
   The affected subscription and application consequence remain unknown.
3. **Where do they find out?** A generic runtime log, then upstream nats.go documentation/source and repository
   archaeology. There is no compile error, boot error, typed runtime error, status field, health signal, or attributed
   metric.
4. **What should they have to know?** Nothing beyond operating their component. The framework already observes the
   affected subscription; converting that observation into an unattributed error is the seam gap.

### Measured premises and open evidence

- One connection async-error owner: registration and handler citations above; repository-wide `ErrorHandler(` search
  finds no sibling owner.
- No current core-subscription drop metric or attributed test: cited searches above.
- No present metric consumer: repository-wide searches for slow-consumer/drop series, dashboard queries, and alerts
  found only the unrelated graph-view metrics and aggregate NATS-server test stats enumerated above.
- The exact subscription responsible for the SemSource incidents is unknowable from retained SemStreams logs; #950
  lists KV watcher inbox, request/reply, and status watch only as candidates.
- External SemSource composition may wire a custom logger or registry differently; that repository is outside this
  inventory.

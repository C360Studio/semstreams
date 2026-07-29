## Context

SemStreams has four physically distinct growth surfaces — ordinary event streams, authoritative graph
KV, derived indexes, and out-of-line content — and no shared capacity view over any of them. The
existing metrics surface is explicitly partial: `natsclient/jetstream_metrics.go:14` tracks "only
streams and consumers that are created/accessed through this client", with a `streams` map populated
by `trackStream` (`:32`, `:136`). There is no `KV_*` or `OBJ_*` enumeration and no storage doctor
anywhere in the repo. The first signal an operator gets today is a write failure.

Its predecessor, `bounded-storage-operability`, tried to solve this by handing NATS a `DiscardNew`
`MaxBytes` ceiling on graph KV as a fail-closed circuit breaker. That premise was falsified by
measurement (see Evidence below): at the ceiling NATS refuses to **replace an existing key** while
still accepting deletes. Because graph-ingest writes one entity across many independently-ceilinged
buckets with no cross-bucket transaction, a ceiling does not stop growth — it tears the graph's
internal consistency at a nondeterministic point, and it lets storage policy make the lifecycle
decision that ADR-068 reserves to the graph.

The correct ordering follows from that: **see the growth and warn with lead time, so capacity is
corrected before anything has to be refused.** Denying writes is not a capacity strategy; it is what
happens when the capacity strategy never ran.

## Goals / Non-Goals

**Goals:**

- One account-wide inventory and pressure model over streams, KV, and ObjectStores.
- Projected time-to-threshold with enough lead time for an operator to add capacity.
- Explicit, reconciled bounds on the lane where age eviction is semantically correct.
- Honest reporting of what is unknown, unbounded, and over-committed.

**Non-Goals:**

- Any enforcement derived from pressure — no admission control, throttling, or readiness failure.
- NATS-level `MaxAge`/`MaxBytes` on graph KV buckets or content ObjectStores.
- Semantic entity GC, cascade deletion, or reachability-based reclamation.
- A single retention duration or byte budget suitable for every product.

## Decisions

### 1. The provisioner refuses `KV_*` and `OBJ_*` by name, at the provisioner

This is the load-bearing decision in the change, and it is a **prohibition, not an exemption**.
Requiring bounds only on ordinary streams would be satisfied by an implementation that merely omits
KV and ObjectStore backing streams from the *required* set while still permitting the reconciler to
write `MaxAge`/`MaxBytes`/`Discard` onto them. That write path is open today: `config/streams.go:254`
takes an operator-authored map with arbitrary keys, `:289` takes an operator-authored port stream
name, and `createStream` (`:358`) applies no name filter at all. Extending the reconciler to the
retention fields — which this change does — is exactly what would make that reachable.

So the provisioner refuses any stream whose name carries the `KV_` or `OBJ_` prefix and fails closed.
The refusal is by prefix, not by catalog membership, because the three plausible safety nets each
have a hole: `ReconcileNoLifecycleRetention` (`natsclient/kv_retention.go:99-100`) clears only
`MaxAge` and `MaxBytes` and **never a discard policy**; `EnsureFrameworkBucket`'s
`case RetentionUnmanaged:` arm reconciles nothing at all, which covers `COMPONENT_STATUS`
(`graph/kvcatalog.go:116`); and product or sister-repo buckets outside the catalog have neither a
seam nor the pre-start backstop. Boot ordering does put stream provisioning
(`cmd/semstreams/main.go:344`) before `WireOwnership` (`service/ownership_service.go:149`), so the
common case would be repaired — but a guarantee that holds for 19 of 22 buckets and one of three
fields is not a guarantee.

### 2. "Ordinary stream" is defined normatively by the prefix rule

Nothing in `config/streams.go` distinguishes stream lanes today: `StreamConfig` (`:19-39`) has no
kind field, and `EnsureStreams` treats the five framework constants (`:242-249`), operator entries
(`:254`), and port-derived streams (`:260-313`) identically. Rather than introduce a declaration
field that every existing config would have to adopt, the spec states the discriminator normatively:
an ordinary stream is any provisioned stream that is not a `KV_`- or `OBJ_`-prefixed backing stream.
That rule is mechanical, needs no config migration, and is the same fact that makes decision 1
enforceable.

### 3. Inventory the account, not the client's memory of it

Inventory enumerates JetStream directly rather than reporting the `trackStream` map. A resource
created by a prior deploy, a sister process, or an operator out-of-band is exactly the resource most
likely to be the growth problem, and it is precisely what a client-touched inventory cannot see.
The existing tracked-stream metrics stay as they are; this is an additional, account-scoped view.

**The cost bound forbids O(N) round-trips — a describe call per resource — not "exactly one API
call."** Resource configuration and state come from the paged listing that already returns both
together, never from a follow-up describe. A second *paged* listing is O(pages) with a cheaper
per-page payload, not an N+1, and one is required for correctness (below).

**The info listing alone is not account-complete, and cannot report that it isn't.** The server moves
any stream carrying an `offlineReason` out of `Streams` and into `Missing`/`Offline`
(`nats-server/v2@v2.12.4/server/jetstream_api.go:502-503`, `:2046-2074`) while still counting it in
`Total`. The Go client parses neither field and exposes no `Total` accessor (`nats.go@v1.48.0`
`jetstream/jetstream.go:411-415`, `api.go:29-33`), so a collector reading only `ListStreams` publishes
a complete-looking inventory that silently omits exactly the resources nobody can read, bound, or
watch grow. This is not clustered-only: `server/jetstream.go:1334-1342` sets `offlineReason` on the
**standalone single-server** recovery path when a persisted stream config or consumer requires a
higher API level than the running binary — a NATS image rollback, which is precisely when an operator
needs the storage view most.

The names listing does NOT filter offline streams (`jetstream_api.go:1932-1949` appends every
`filteredStreams` entry unconditionally), so names-minus-infos is exactly the unreadable set. Those
resources are published as rows with a real name, kind and bucket from `ClassifyBackingStream`, and
unknown tier and capacity. This is what gives "unknown capacity" its first groundable production
producer rather than leaving the state near-unreachable.

The two listings are not a consistent snapshot: a stream deleted between them yields a transient
unknown row. That is the correct trade — a phantom row that resolves on the next collection is
strictly better than a silent omission that never does.

Collection is interval-driven with a timeout, never on the component-start or health path, and
degrades to last-good-with-timestamp. A monitoring surface that can take down the system it monitors
is a worse bug than the blindness it fixes. Because every process polling account-wide multiplies
cost by deployment size, the polling interval is configuration and the report states which process
produced it.

### 4. Owner attribution for KV derives from the descriptor catalog

`KV_*` resources map to a logical owner through `graph.OwnerOf(bucket)` (`graph/kvcatalog.go:213`),
which already returns `""` for a bucket outside the catalog — exactly the "unattributed" semantics
wanted. This is a read of the one catalog table, not a second mapping: the founding invariant of
`framework-bucket-catalog` is that every enforced or reported set is a derived view, and an inventory
maintaining its own owner list could disagree with the acquisition seam about who owns a bucket.

The name mapping is where mis-attribution would live, so it is specified rather than left to the
implementer: strip exactly one leading `KV_` to recover the bucket name. A product bucket named
`KV_FOO` has backing stream `KV_KV_FOO` and must resolve to `KV_FOO`, not `FOO`.

### 5. Unknown, unbounded, and bounded are three distinct states

An unreadable limit is not an absent limit, and an absent limit is not a safe one. Collapsing any two
produces the phantom-signal class this program exists to remove: a resource reported "healthy"
because its capacity could not be read is worse than one reported not at all, because it manufactures
confidence. Unknown capacity suppresses projection entirely rather than emitting a fabricated number.

This applies to the account limit too. `js.AccountInfo` — already called at `config/streams.go:192` —
reports `-1` for unlimited (`:227`), and testcontainers reports unlimited by default (`:220-223`), so
"account limit unbounded" is the normal case in integration tests. The report says so explicitly and
marks the over-commitment comparison not-applicable rather than silently satisfied.

### 6. Pressure is derived from time-to-threshold as well as headroom, per storage tier

Proportional headroom alone misranks: a 2 TiB resource at 40% filling in an hour is more urgent than
a 1 GiB resource at 85% static for a month. Pressure takes the worse of a proportional-headroom band
and a projected-time-to-threshold band, and reports which input raised it so an operator can tell a
capacity problem from a rate problem.

Over-commitment is computed **per storage tier**. JetStream has separate memory and file account
limits, and this repo's own streams span both — `HEALTH`, `METRICS`, and `FLOWS` are memory
(`config/streams.go:110`, `:121`, `:132`) while `LOGS` and `GOVERNANCE_VERDICT_AUDIT` are file.
Summing across tiers would produce a number that means nothing.

### 7. Growth rate must survive restart, or report unknown

An in-process sample window is the obvious implementation and it is self-defeating: every restart
blanks the projection for a full window, and the longer the window needed to keep a burst from
tripping `critical`, the longer the blackout. A deploy-loop or crash-loop — when the projection
matters most — would guarantee there is never one.

The tempting fix is to read a stream's own `FirstTime`/`LastTime` span and divide its byte count by
it, needing no history at all. **That is wrong for most of the catalog and was corrected here rather
than shipped.** It is defensible for a bounded stream at steady state, where the span is effectively
the retention window — but a KV bucket under `History` 1 compacts superseded revisions, so an
actively churning bucket holds roughly constant bytes while its timestamps span a long window.
Bytes-over-span would report sustained growth, and project exhaustion, for a bucket whose size never
changes. More fundamentally, a single snapshot cannot distinguish "grew to 100 MB over a year" from
"grew to 100 MB yesterday," and those demand opposite operator responses.

Growth is therefore Δbytes over Δt across **successive observations**, and the per-key history of the
report bucket (decision 8) is where those observations already live — restart-surviving by
construction, with no separate sample store to build. This is why the report bucket sequences ahead
of the growth work rather than after it. Where fewer than two observations exist, the rate reports
unknown rather than extrapolating from one.

### 8. The report is published to KV; HTTP and CLI are consumers, not the surface

The report is written to a framework-owned KV bucket, one key per resource, and every operator-facing
surface reads from there. Applying the 4-test heuristic gives an unambiguous answer: on restart a
consumer needs the current report immediately rather than resumption from last ack (re-delivery of
current state is correct recovery); the gateway, CLI, alert rule, and metrics exporter should all
react rather than one dequeuing; consuming a report is fast and idempotent; and it is a fact about
the world — the spec calls pressure a *state*. Four for KV, no conflict, so nothing here is two
concepts conflated.

The twofer then pays three times. `Get`/range is the operator read, so decision 4.4's "named
transport" needs no new invention. `Watch` is the fan-out that gives the pressure gauge a consumer at
merge time rather than leaving it a phantom signal. And per-key history is a restart-surviving growth
series — the same job decision 7 solves from server-retained state, now with a second and better
source that retains the *computed* row rather than raw byte counts.

**One key per resource, not one report blob.** A single blob would be bounded by the NATS max payload
on a large account, which is what would otherwise push the report toward ObjectStore. Per-resource
keys remove that ceiling entirely, make history per-resource rather than per-snapshot, and let a
disappeared resource be reclaimed by a KV delete — a semantic deletion the collector performs,
which is the ADR-068-correct mechanism, rather than a retention policy this change has spent its
whole argument refusing to add.

**ObjectStore is deliberately NOT used, and the reason is a trap worth recording.** The framework has
no bounded object lane: merged `graph-retention` requires content ObjectStores to carry no lifecycle
retention (the constructor stamps none, boot strips any), and the `windowed` lifetime class that
would have provided one was retired with `bounded-storage-operability`. A report-detail store would
therefore grow without bound and without any legal way to expire it — making the observability
system the exact unbounded-growth case it exists to detect. Per-resource keys mean the question
never arises.

The cost is that a consumer ranging the bucket sees a mix of revisions rather than an atomic
snapshot. That is consistent with what the inventory already is — eventually consistent across its
two listings — and every row carries its own collection timestamp so a consumer can see the spread.

The bucket is declared in the descriptor catalog like every other framework-guaranteed bucket
(operational class, owner-only writes, no-lifecycle retention with bounded History), following the
`GRAPH_STATUS` precedent. Note the self-reference this creates: the inventory will report its own
bucket. That is honest and correct — the observer is a real storage resource — and bounded History
keeps it from being the thing that fills the account.

### 9. The collector is a Service, not a Component

Storage observability watches the **substrate**, not the flow. Its peers are already services —
`flow_runtime_health`, `flow_runtime_metrics`, `metrics_forwarder`, `log_forwarder`, `heartbeat`,
`pprof` — and every one of them is infrastructure observability. A component is a flow participant
with ports that processes messages; the collector has no ports and never receives or emits one.
Filing it as a component would place substrate monitoring in the data-flow graph as though it were a
pipeline stage.

The `Service` interface (`service/base.go:486`) settles it independently: it declares
`Health() health.Status` and `RegisterMetrics(metric.MetricsRegistrar) error`. Tasks 4.3 and 4.1 are
exactly those two methods, so the component route would mean reimplementing inside a component what
the service contract already requires.

**A restart is the supported way to change this service's configuration**, and that is a deliberate
narrowing of an earlier position. Hot reconfiguration matters when a stale value silently drives a
durable resource to the wrong policy — the capacity-ceiling case, where the damage is invisible and
persistent. A stale threshold is not that: it evaluates pressure with old numbers until restart,
visibly, destroying nothing. Restarting the collector is cheap precisely because section 3 put the
report and its growth series in KV, where they survive the restart. The service constructor signature
(`Constructor func(rawConfig json.RawMessage, deps *Dependencies) (Service, error)`) also makes
build-from-config the natural service shape, so requiring live re-reads would be swimming upstream
for a knob a reboot handles.

The `ThresholdSource` seam from section 3 stays because it already exists and costs one function
type — an operator who wants to retune during an incident can, without restarting the monitoring they
are relying on. But nothing depends on it, and no work should be spent extending it.

One cost is accepted knowingly rather than discovered later: **no service has a generated schema** —
all 34 entries in `schemas/` are components. The operator surface is protected by a JSON round-trip
test, which is the project's stated bar, but the asymmetry is a real framework gap and is filed as
such rather than absorbed silently. (The `isHTTPManager` gating on service config-change watching,
`service/service_manager.go:864`, is moot under the restart posture above.)

### 10. Report-only, and the gate future enforcement must clear

Nothing here rejects, throttles, degrades, or evicts. This is sequencing, not timidity: pressure-driven
enforcement built on an unmeasured, untrusted signal is how the predecessor went wrong.
Application-level admission — rejecting an entity write in graph-ingest *before* any bucket is
touched, the only place a rejection can be entity-atomic and cross-bucket-coherent — is the correct
eventual home for backpressure. Its gate is written to be checkable rather than aspirational:

1. this change merged;
2. the rejection path demonstrably classifies transient and NAKs rather than acking and dropping;
3. projected time-to-threshold verified against observed outcome on at least three real resources,
   with no `critical` state that resolved without operator action.

Condition 3 is the one that would otherwise become "when it feels trustworthy," so it reads off the
system being built.

### 9. Thresholds resolve at use, not at boot

Pressure thresholds are operator configuration, and SemStreams is flow-based with runtime-configurable
components: `watchConfigUpdates` (`service/component_manager.go:427`, defined `:1224`) is launched
after the boot barrier specifically so post-boot `components.<name>` edits reach running components.
Threshold values are therefore read from live configuration at evaluation time, never captured into a
value frozen at composition-root construction. A frozen threshold would apply a runtime edit's
successor value *successfully and silently* to the stale number — the post-boot-cutoff class that
`framework-bucket-catalog` closed at the acquisition seam, reintroduced one layer up. The same
reasoning binds any future capacity budget: resolve at the seam, from live config.

## Risks

- **A new `DiscardNew` knob is a new footgun.** This change spends its Why proving that a
  `DiscardNew` ceiling produces producer-side `503 err_code=10077` at the limit, then makes discard
  policy an operator choice on ordinary streams. That is correct for time-shaped streams — but the
  declaration diagnostic and the runbook must both state what `DiscardNew` does at the ceiling, or an
  operator will select it and rediscover the failure this change characterized.
- **Report-only observability can become the phantom class.** A pressure gauge with nothing
  downstream is precisely what this program has deleted thirteen instances of. The storage report is
  a genuine consumer of the inventory; the pressure state needs one too, which is why an example
  alert rule and health-status visibility ship with the metrics rather than after them.
- **The bounds requirement has a wide blast radius.** Every component-derived stream is created today
  as `StreamConfig{Subjects: subjects}` (`config/streams.go:303-306`) with no declared bounds, so
  requiring explicit declaration fails readiness for every such stream and every existing operator
  entry across sister repos. The expiring migration override is what makes this landable; without it
  the change is a flag day. Overrides must expire, or the bridge becomes permanent.
- **Enumeration cost scales with account size and process count.** Bounded interval, timeout, and
  last-good degradation cover the first; the report naming its producer covers the second.

## Evidence

Measured against NATS 2.12 via testcontainers; KV bucket `MaxBytes` 128 KiB, `History` 1, 1 KiB
values. NATS creates KV backing streams with `Discard: DiscardNew` automatically. Filled to 121 keys,
then rejected with `code=503 err_code=10077 maximum bytes exceeded`.

| operation at the ceiling | result |
|---|---|
| write a new key | rejected |
| replace an existing key | **rejected** |
| delete a key | succeeded |
| purge a key | succeeded |
| write a new key after purge | succeeded |

A same-size replacement under `History` 1 is net-zero bytes because the superseded revision compacts
away, but the append is checked against `MaxBytes` before that compaction — so replacement fails
first, and "reserve headroom for replacement" is not expressible against a single append-checked
limit. This is the measurement that retired `bounded-storage-operability` and that confirms
`docs/adr/073-graph-ingestion-retention-contract.md:77-79`.

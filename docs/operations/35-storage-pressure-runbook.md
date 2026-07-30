# Storage Pressure Runbook

Operator procedure for JetStream account storage pressure (`storage-capacity-observability`).
Pressure is a **report** derived from a resource's usage, its configured bound, and the growth rate
measured across successive published observations. It says how much room is left and how long you
have.

**Nothing here rejects, throttles, degrades, or evicts anything.** SemStreams refuses no write and
fails no readiness gate because a resource is under pressure. Every state below is a message to a
human. What CAN refuse a write is JetStream itself, at a ceiling — see
[The ceiling](#the-ceiling-what-happens-when-a-bound-is-actually-reached).

## Reading the report

The produced truth is the `STORAGE_REPORT` KV bucket: one key per resource, plus the reserved
`_account.tiers` key. Every surface below reads it and none recomputes, so they cannot disagree.

| Surface | Use it for |
|---|---|
| `GET /storage-observability/report` | The whole picture, including fields no metric carries |
| `nats kv get STORAGE_REPORT <resource>` | One resource, when the HTTP route is unreachable |
| `nats kv history STORAGE_REPORT <resource>` | The growth series itself — the last 10 observations |
| Prometheus `semstreams_storage_*` | Alerting and dashboards |
| Component health message | A summary while you are already looking at health |

Read these fields first on any row:

- **`resource.bytes.state`** — `bounded`, `unbounded`, or `unknown`. These are three different
  situations and never collapse. `unknown` means the limit or usage could not be READ; it is not
  unlimited and not healthy.
- **`pressure.evaluated_against`** — `own-bound` or `account-tier`. **Read this before acting.** It
  decides what the fix is; see [Two ceilings](#two-ceilings-and-why-the-basis-decides-your-fix).
- **`pressure.raised_by`** — `headroom`, `time-to-threshold`, or `both`. A capacity problem and a
  rate problem are fixed differently.
- **`growth.observed_over`** — the interval the rate was measured across. A rate with no interval is
  not interpretable; a five-hour average and a one-minute one read identically without it.
- **`collected_at`** — this row's own collection time. Ranging the bucket returns a mix of revisions,
  so each key carries its own.

## What each pressure state means, and does not

| State | Means | Does NOT mean |
|---|---|---|
| `normal` | Neither headroom nor projection is inside a configured band | That the resource is bounded — read `bytes.state` |
| `warning` | An input crossed the widest band (default: 25% free, or 72h projected) | That anything has been refused |
| `high` | An input crossed the middle band (15% free, or 24h) | That eviction has started |
| `critical` | An input crossed the tightest band (5% free, or 4h) | That SemStreams is refusing writes — it is not |
| *absent* (`evaluated: false`) | No state could be derived; `unavailable` says why | `normal`. This is the case that most needs a human |

An absent state is a finding, not a pass. The reasons are distinct because they resolve differently:

- `unknown-capacity` — the server declined to describe the resource (typically a persisted stream
  config requiring a higher API level than the running binary, after an image rollback). Nothing can
  warn about its growth until it becomes describable.
- `unbounded-no-account-tier-ceiling` — the resource declares no bound AND its storage tier's account
  limit is unlimited too. Nothing anywhere constrains it. Common on a stock server.
- `unbounded-account-tier-unknown` — the tier's ceiling may well exist; this process cannot read it.
  A gap in visibility, not a clean bill.

## Two ceilings, and why the basis decides your fix

A resource with a bound of its own is evaluated against it. A resource with **no** bound of its own
is evaluated against the account limit of its storage tier, because that is the only ceiling it has.
Both publish a `resource_pressure` severity; only the basis tells them apart.

**`own-bound`** — this resource is filling its own declared limit.
Fix: raise `max_bytes`/`max_age` for it, or reduce what it retains. The lever is on the resource.

**`account-tier`** — the tier this resource lives in is filling. The state is the tier's, shared
verbatim with every other unbounded resource in that tier, and **it is not relieved by changing
anything about this resource**. Fix: add account capacity (`max_file_store` / `max_memory_store` in
`nats.conf` — a server-side setting the framework cannot push), or delete data. For an archival
stream this is the whole set of levers, because it cannot evict.

Two consequences worth internalising before your first incident:

1. **One tier fact produces many rows.** Every framework KV bucket is unbounded on bytes, so a tier
   crossing a band makes twenty-odd resources report the same state in the same collection. That is
   not twenty problems. Count distinct `account-tier` states as **one** finding per tier. The
   per-resource alerts are deliberately scoped to bounded resources for this reason; the tier alerts
   are the ones that page for this class.
2. **A small resource can be critical.** An archive holding 50 MiB of a nearly-full tier reports the
   tier's state, unscaled. That is correct: when the tier fills, the stream that cannot evict is the
   one with no move left. Do not dismiss it as too small to matter.

## Correcting capacity ahead of the projection

`time_to_threshold` targets the **critical-headroom level, not the bound** — capacity has to be
corrected before the last byte fits. Work backwards from it.

1. **Confirm the rate is real.** Check `growth.observed_over`. A rate measured over one minute during
   a burst is not a trend; the series survives restarts, so prefer a reading spanning several
   collections. `nats kv history STORAGE_REPORT <resource>` shows the observations directly.
2. **Decide whether it is capacity or rate.** `raised_by: headroom` on a slow-growing resource is a
   sizing problem — raise the bound once. `raised_by: time-to-threshold` on a resource with plenty of
   free space is a rate problem — find the producer. Proportional headroom alone misranks: a 2 TiB
   resource at 40% filling in an hour is more urgent than a 1 GiB resource at 85% static for a month.
3. **Act on the right ceiling.** Per the basis, above.
4. **Verify against the next collection**, not against the alert clearing. The report is republished
   each interval (default 1m); `collected_at` moving with your expected numbers is the confirmation.

### Over-commitment is a different question

`account_overcommitment{state="over-committed"}` compares the **declared bounds** in a tier against
the tier's account limit. It can report `within-limit` while the tier's actual usage is minutes from
exhaustion — declarations are not usage, and an unbounded resource declares nothing, so
`account_declared_bytes` is a FLOOR whenever the tier holds unbounded or unknown resources. Read
`account_used_bytes` and `account_headroom_bytes` for what is actually stored. The two fail
independently and both are published.

## The ceiling: what happens when a bound is actually reached

This is the part SemStreams does not control, and the discard policy is the operator's choice of
which failure they get.

**`discard: old`** (the framework default for observability and audit streams) — at the ceiling
JetStream **evicts the oldest messages** to make room. The write always succeeds. The loss is silent
and at the tail. Correct where the newest data is what matters and stale data is worthless: health,
metrics, flow status, capability announcements.

**`discard: new`** — at the ceiling JetStream **refuses the write**. The producer sees
`503 err_code=10077 "maximum bytes exceeded"` on every publish until something is deleted. Nothing is
evicted. Correct where losing a message is worse than failing loudly — a work queue, where a dropped
request strands whatever claimed it.

Two things about `discard: new` that surprise people:

- **It refuses a REPLACEMENT of an existing key too.** A full stream under `DiscardNew` does not
  accept an update that would not grow it. This is measured NATS behavior, not an inference; it is
  why the framework does not use `MaxBytes` as a bound on KV buckets holding authoritative state.
- **Under `limits` retention the ceiling fills with ACKED messages.** A stream that retains processed
  messages reaches its byte ceiling from history, not backlog, and then refuses everything while the
  consumer sits idle and healthy. If you set `discard: new`, the stream needs `workqueue` retention
  (delete on ack) so the ceiling means what you think it means. `gated-dag` refuses that combination
  at validation for exactly this reason.

## Staleness: the failure that looks like calm

A collector that stops produces no new observations, and **every other metric keeps looking fresh
forever** — Prometheus stamps scrape time, not data time. Alert on
`time() - semstreams_storage_report_collected_timestamp_seconds > <horizon>`, whose VALUE is the
collection time. Never alert on a series being absent: report rows are reclaimed semantically and a
row may transiently vanish and return under concurrent producers.

If the freshness gauge is stale, treat every other storage number as unreliable until it clears, and
check the storage-observability service's health message for the last collection outcome.

## Alert rules

`configs/prometheus/rules/storage-pressure.yml` ships tuned examples. Which fires for what:

| Alert | Fires for |
|---|---|
| `StorageResourcePressureHigh` / `Critical` | A resource filling its OWN bound (scoped to bounded resources) |
| `StorageAccountTierPressureHigh` / `Critical` | A tier filling — and thereby every unbounded resource in it |
| `StorageResourceExhaustionProjected` | A resource's own projection inside 24h |
| `StorageAccountTierExhaustionProjected` | A tier's projection inside 24h — the lead time for archival streams |
| `StorageAccountTierOvercommitted` | Declared bounds in a tier exceed the tier's account limit |
| `StorageResourceUnbounded` | A resource with no bound at all, so the choice stays deliberate |
| `StorageResourcesWithUnknownCapacity` | Resources the server will not describe |
| `StorageReportStale` | The collector stopped |
| `StorageStreamMigrationOverrideExpired` | A time-limited bridge lapsed; the next deploy will refuse to boot |

## An expired migration override

A `stream_migration_overrides` entry admits an existing unbounded stream that predates the bounds
contract. It carries an owner and a deadline, and the deadline is the point.

**Enforcement is at validation and provisioning, not at runtime readiness.** A running instance that
crosses the deadline keeps serving and reports the lapse — a WARN per tick and
`semstreams_streams_migration_override_expired{stream,owner} == 1`. The **next boot refuses to
start**. That split is deliberate: the admitted stream still works, so a lapsed bridge is a hygiene
failure, and taking a healthy fleet out of the load balancer simultaneously because a calendar date
passed would turn it into an outage. The refusal lands where an operator can act on it anyway.

The practical consequence: **resolve it before your next deploy, not during one.** The failure mode
this alert exists to prevent is a routine deploy failing at boot for a reason nobody connected to a
date set months earlier.

Two ways out, and they are not interchangeable:

- The stream should be bounded → declare `max_age`, `max_bytes` and `discard` on it and delete the
  override.
- The stream is permanently unbounded by contract → move it to `archival_streams` with an owner and a
  reason. Archival is permanent by declaration and has no deadline, which is why an archive must never
  be expressed as a renewed override: renewing on autopilot is what makes genuinely time-limited
  bridges invisible.

## What this capability does not do

Admission control. Nothing refuses a write, throttles a component, or applies retention because of
pressure. That is sequencing rather than timidity: enforcement built on an unmeasured signal is how
this capability's predecessor went wrong, and the correct eventual home for backpressure is
application-level admission in `graph-ingest`, where a rejection can be entity-atomic and
cross-bucket-coherent. Four conditions must hold before anything is allowed to act on a pressure
state, and they are recorded in the `storage-observability` capability spec ("Capacity admission MUST
NOT be built until the deferral gate is satisfied") — including that no `critical` may have resolved
without operator action, which is the one most likely to fail first.

If you want backpressure today, read the report and decide explicitly in your own code, where the
choice is visible.

## See also

- [Framework bucket catalog](framework-bucket-catalog.md) — which buckets exist and who owns them
- [ADR-068](../adr/068-graph-retention-deletion-lifecycle.md),
  [ADR-073](../adr/073-graph-ingestion-retention-contract.md) —
  why the live graph never uses NATS TTL or `MaxBytes`, and why stream provisioning refuses to touch
  a KV or ObjectStore backing stream

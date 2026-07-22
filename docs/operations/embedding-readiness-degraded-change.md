# Embedding readiness reports `degraded` under failures (BREAKING)

**Change:** `embedding-readiness-and-dedup-efficiency` (Epic A increment 4, #613).
**Applies to:** any adopter that runs graph-embedding and gates reads on its
readiness (`semsource`, `semboids`).

## What changed (and why it is BREAKING)

Before this change, graph-embedding could publish a fully-green readiness envelope
(`Ready: true`, `State: ready`, `lag: 0`) **while holding zero usable vectors** —
a failed embedding was a terminal outcome that advanced the readiness watermark,
so a semembed outage during cold start reported "caught up" over nothing but
failures.

Now the shared readiness projection (`ComputeIndexStatus`) takes a `FailedCount`
input and projects **`FailedCount > 0 → State = degraded`, unconditionally** (it
wins over the "ready" branch). A subsystem that previously reported `ready` over
failures now reports `degraded`.

- **`State`** carries the health verdict; consumers gate on `State` (ADR-085). A
  consumer that gates on health will now correctly **withhold** reads while
  embeddings are failing, where it previously served over a false `ready`.
- **`Ready`** stays coverage-accurate (a full-coverage index that also holds
  failures is still "covered"). `Ready: true` with `State: degraded` is an
  intentional, coherent envelope — **do not gate on `Ready`; gate on `State`.**

The embedding watermark still advances on every terminal outcome (including
failed and no-text), so a permanently-failing or telemetry-only entity never
stalls readiness — that behavior is unchanged.

## New observability — how to read a degraded embedding subsystem

A bare `degraded` is not actionable, so the degraded envelope and metrics now
carry bounded failure detail. **All of it is always-on (no debug service
required).**

On the `GRAPH_STATUS/graph-embedding` envelope (additive, omitted when zero):

| Field | Meaning |
|---|---|
| `failed_count` | entities currently in a failed embedding state |
| `failed_reasons` | a `{reason: count}` map over the bounded reason enum |
| `first_failure_at` | RFC3339 timestamp of the earliest current failure |

As Prometheus gauges/counters:

- `semstreams_graph_embedding_failed` — the **current** failed count (NOT
  cumulative; drops to 0 as failures resolve). Drives `degraded` while `> 0`.
- `semstreams_graph_embedding_failures_total{reason}` — **cumulative** failures
  by bounded reason.

**Reason enum** (the only values that ever appear as a label or map key):
`connection_refused`, `timeout`, `dimension_mismatch`, `embedder_error`,
`content_error`, `internal`. The raw error message is never a label.

**Triage:** a high `failed_count` dominated by a single connectivity reason
(`connection_refused` / `timeout`) is a whole-dependency outage; a small, stable
`failed_count` under `content_error` is a few un-embeddable entities. That
distinction is the point of the breakdown.

**Deep drill-down (opt-in, debug only):** the message-logger service — which is
**off by default** — accepts `?status=failed` over `EMBEDDING_INDEX` to enumerate
the specific failed entities with their reason and raw error. Enable message-logger
at reboot only when you need per-entity forensics; production observability is
complete without it.

## Recovery

Failed embeddings recover **without operator action** once the embedding
dependency returns:

- On **restart**, every entity is re-enumerated from `ENTITY_STATES` and
  re-embedded; failed records are replaced.
- On a **new entity revision**, the entity re-embeds through the normal path.

`failed_count` drops as failures resolve, and `State` returns to `ready` when the
last failure clears — watch `failed_count` trending down as the recovery signal.

**Boundary:** if the dependency recovers with **no restart and no new revision**,
the affected entities stay `degraded` until the next restart or revision. A
durable background repair loop that retries without re-delivery is a separate,
future increment (#625) and is intentionally out of scope here.

## Rollback

Safe. The envelope fields and the `Record.reason` field are additive and
omitempty; `failed_count` is tracked in memory only. A worker rolled back to the
prior version ignores the new field and reverts to the old
report-`ready`-over-failures behavior — no crash, no data corruption, no schema
migration. Roll forward again to restore the honest `degraded` reporting.

## Action for adopters

1. **Confirm your read gate uses `State`, not `Ready`.** If anything keys off
   `Ready` past the canonical health gate, move it to `State` — otherwise it will
   serve over `degraded`.
2. **Alert on `semstreams_graph_embedding_failed > 0`** (or `State = degraded`)
   and dashboard `failed_reasons` so an embedding-dependency outage is visible.
3. No config change is required; no data migration is required.

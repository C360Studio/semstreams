# ADR-058: Boot Lifecycle — Phase-A Eager Wiring vs Phase-B Managed Services

- **Status**: Draft
- **Date**: 2026-06-18
- **Supersedes**: none
- **Related**: ADR-047 (Lifecycle harness), ADR-056 (authoritative semantic
  state / ownership write-lease)

## Context

`cmd/semstreams/main.go run()` and `cmd/e2e-semstreams/main.go run()` hand-wire
their process-lifetime background subsystems with raw `go X.Run()` +
`sync.WaitGroup` + LIFO-`defer` shutdown ordering, instead of using the
framework's existing `service.Service` lifecycle. Two concrete forcing
functions:

1. **`run()` is over the function-length limit.** ADR-056 PR-4 added a
   `WatchRevival` goroutine to the inline ownership block; `run()` is now 82
   statements against an 80 limit. Every new background subsystem makes this
   worse.
2. **The eager wiring is duplicated AND already divergent across the two
   mains.** The identical ownership sequence — `EnsureBuckets` →
   `AttachOwnership` → static heartbeater goroutine → `WatchRevival` goroutine
   → `BindAndHeartbeat` — appears inline in `cmd/semstreams/main.go:209-266`
   and wrapped in a `wireLifecycleManager` helper in
   `cmd/e2e-semstreams/main.go:264-315`. The rule-pack contract bind
   (`service.BindRulePackContracts`) is inline in both. This is exactly the
   beta.18 half-migration failure mode CLAUDE.md documents: one binary gets a
   change, the sister binary silently does not.

The framework already has almost all the pieces; we are mostly not using them.
There is **one genuine gap** — a way to register a composition-root-constructed
Service *instance* (today the `ServiceManager` only creates services from
config-registered constructors). We close it with one minimal primitive
(`RegisterInstance`, below), not a new subsystem. The existing pieces:

- `service.Service` interface — `Name / Start(ctx) / Stop(timeout) / Status /
  IsHealthy / GetStatus / Health / RegisterMetrics`
  (`service/base.go:471`). `service.BaseService` (`service/base.go:66`) embeds
  for defaults and provides a `waitGroup` + `done` channel
  (`service/base.go:94-95`).
- `service.Manager` — `StartAll` (`service/service_manager.go:266-273`) starts
  registered services (iterating a **map — random order**) and **aborts boot on
  the first `Start()` error**; `StopAll` (`service/service_manager.go:382+`)
  stops in **reverse registration order** and **continues on `Stop()` errors**,
  collecting them. Services enter only via config-driven `CreateService` /
  `ConfigureFromServices` (`service/service_manager.go:163,77`) — there is **no
  instance-registration path today** (the gap above).
- Exemplar to mirror: `HeartbeatService` (`service/heartbeat.go:125`) — `Start`
  launches its loop goroutine under its own `wg`; `Stop` signals + joins with an
  inline `done`-channel-`select` timeout. Other exemplars: `Metrics`,
  `LogForwarder`, `MessageLogger`, `FlowService`.

## Decision

Classify every boot citizen into one of two phases. This is the whole ADR;
everything else is application.

### The two phases

**Phase A — eager registration / construction.** Create resources and register
identities that *other subsystems reference by name or by claim*: NATS
connection, streams, metrics, loggers, the payload/tool/component registries,
KV buckets, the ownership Registry, lifecycle workflow registrations,
rule-pack projection contracts. Phase A:

- must run in **deterministic, source-order** sequence,
- must run **before** the consumers that reference those identities,
- must run **before** `StartAll` (which is post-wiring and starts services in
  **random map-iteration order** — `service/service_manager.go:266`),
- lives in the **composition root**, factored into **plain shared helper
  functions** so the two mains call the same code, never duplicated per binary.

Phase A is boring on purpose: it is sequential Go in a function. No DAG, no
phase enum, no container.

**Phase B — runtime lifecycle.** Background goroutine(s) and runtime resources
that need a clean `Start` and a join-on-`Stop`: the static-owner heartbeater,
`WatchRevival`, the milestone subscriber. Phase B lives in a
`service.Service` registered with the `ServiceManager`, which gives us
ordered shutdown and uniform health/metrics for free. (The pprof server looks
Phase-B but fails the Is-it-a-Service test — no state to flush, needs no clean
Stop — so it stays a Phase-A fire-and-forget helper; see rollout step 4.)

### The "Is it a Service?" test

Make it a Phase-B `service.Service` if and only if **all three** hold:

1. it lives for the whole process,
2. it owns background goroutine(s) or runtime resources,
3. it needs a clean `Stop` / join (not just process exit).

If it only creates a resource or registers an identity, it is Phase A — a
helper-function call, not a Service.

### run() inventory

| Boot citizen | Phase | Today | Target |
|---|---|---|---|
| NATS / streams / metrics / logger / payload+tool+component registries | A | eager, correct | leave alone |
| ownership buckets + Registry + `AttachOwnership` | A | inline, divergent | shared `WireOwnership` helper |
| ownership rule-pack contract bind (`BindRulePackContracts`) | A | inline, duplicated | shared helper, post-construction call |
| ownership static heartbeater goroutine | B | `go`+WG+`defer` | `OwnershipService` |
| ownership `WatchRevival` goroutine | B | `go`+WG+`defer` (PR-4) | `OwnershipService` |
| lifecycle `Manager` construct + workflow `Register` | A | inline / helper | shared helper |
| lifecycle Manager-internal heartbeater (`AttachOwnership` spawns it) | B | already joined via `WaitOwnership` | NOT a Service — cancel+join factored into shared `WireOwnershipShutdown` helper (step 2) |
| milestone subscriber (`Start()`→stop-func) | B | hand-managed stop-func | wrap as Service |
| pprof server | A | hand-started HTTP, duplicated | shared `service.MaybeStartPProf` helper — NOT a Service (step 4) |

### Robustness constraints (non-negotiable)

These three rules exist because the subsystems being migrated (ownership,
lifecycle) are **observe-only / best-effort** today and must stay that way.
They are not future-proofing; each one prevents a concrete regression.

- **R1 — A Phase-B `Start()` must be infallible for soft failures.** Because
  `StartAll` aborts the whole boot on any `Start()` error
  (`service/service_manager.go:268-271`), a best-effort subsystem that fails
  to start must **degrade to disabled** (log + no-op, return `nil`), never
  return an error. This preserves today's "ownership disabled this boot"
  posture (`cmd/semstreams/main.go:213`) and matches graph-ingest, which
  already graceful-skips when `OWNER_CLAIMS` is absent
  (`processor/graph-ingest/component.go:812`). Ownership and lifecycle must
  **never** become a boot gate. Hard dependencies (NATS itself) stay in
  Phase A, where a failure *is* allowed to abort boot.

- **R2 — Identity/state is created once in Phase A, never in `Start()`.** The
  ownership `Registry` — including its incarnation nonce and quiesced set —
  is constructed exactly once, in Phase A. A `service.Service` restart must
  **not** re-mint the incarnation (a fresh nonce would self-supersede the
  process's own prior claims → false quiesce, per ADR-056) and must not
  dangle the references consumers already hold. The Service owns only the
  **restartable goroutines**; it holds a pointer to the Phase-A-owned
  Registry, it does not own its construction.

- **R3 — No consumer-presence assumptions; independence is symmetric.** A
  Phase-B Service must be a no-op with zero consumers: an empty owner set
  means `WatchRevival` and the heartbeater do nothing. Consumers already
  fail-open on absent ownership (`AttachOwnership` is a nil-safe no-op,
  `pkg/lifecycle/manager.go:237`; the claim reader graceful-skips,
  `processor/graph-ingest/component.go:806-815`) and that must be preserved.
  Symmetrically: ownership failing or restarting must not cascade into graph
  components, and missing graph consumers must not break ownership.

## Worked example — ownership (the PR-4 forcing function)

Today's inline block is mixed Phase A + Phase B. Split it cleanly.

### Phase A — `service.WireOwnership` (plain function, shared by both mains)

A composition-root helper that does only the eager, deterministic work and
returns the constructed identities. No goroutines spawned here.

```go
// WireOwnership performs Phase-A ownership wiring: create buckets, construct
// the Registry (R2 — once, here), attach it to the lifecycle Manager, and bind
// static projection contracts. Best-effort (R1): a bucket-bootstrap failure
// logs and returns a nil Registry; callers treat nil as "ownership disabled
// this boot" and pass it straight to the OwnershipService (which no-ops on nil).
//
// Returns the Registry and its static heartbeater so the caller can (a) start
// the Phase-B OwnershipService and (b) bind rule-pack contracts AFTER the rule
// processors are constructed.
func WireOwnership(
    ctx context.Context,
    natsClient *natsclient.Client,
    lcm *lifecycle.Manager,
    logger *slog.Logger,
) (*ownership.Registry, *ownership.Heartbeater) {
    reg, err := ownership.EnsureBuckets(ctx, natsClient, logger, vocabulary.InverseResolver)
    if err != nil {
        logger.Warn("ownership: bucket bootstrap failed — disabled this boot", slog.Any("error", err))
        return nil, nil // R1: degrade, do not abort.
    }
    lcm.AttachOwnership(ctx, reg) // nil-safe; spawns the Manager-internal
                                  // heartbeater, joined via lcm.WaitOwnership().
    staticHB := reg.NewHeartbeater(ownership.HeartbeatInterval)
    if _, err := projection.BindAndHeartbeat(ctx, reg, staticHB,
        "agentic-loop-graph-writer", loopExecutionProjectionContract()); err != nil {
        logger.Warn("ownership: projection contract bind failed", slog.Any("error", err))
    }
    return reg, staticHB
}
```

The static heartbeater is *constructed* in Phase A (it is identity-adjacent —
it heartbeats the static owner's presence key) but it is not *run* here. The
goroutine that runs it moves to Phase B.

### Phase B — `OwnershipService` (mirrors `HeartbeatService`)

Lives in **`package service`** (alongside `WireOwnership` and
`BindRulePackContracts`) and carries its **own `logger`** field exactly like
`HeartbeatService` (`heartbeat.go:67`) — do not reach for `BaseService`'s
unexported `logger`, which has no accessor.

```go
type OwnershipService struct { // in package service — no self-qualification below
    *BaseService
    logger   *slog.Logger             // own logger (mirrors HeartbeatService); set by ctor.
    reg      *ownership.Registry      // R2: owned by Phase A, borrowed here.
    staticHB *ownership.Heartbeater
    metrics  *metric.MetricsRegistry
    cancel   context.CancelFunc
    wg       sync.WaitGroup
}

func (s *OwnershipService) Start(ctx context.Context) error {
    // Re-entrancy guard (mirrors HeartbeatService:126-128). An already-running
    // double-Start is a BUG-CLASS error, NOT an R1 soft failure — returning an
    // error here is correct even though R1 forbids erroring on soft failures.
    // Without it, BaseService.Start returns nil-if-already-running (base.go:220)
    // and control falls through to a duplicate wg.Add(2) + leaked goroutines.
    if s.Status() == StatusRunning {
        return fmt.Errorf("ownership service already running")
    }
    // Mark the service running — even on the disabled path it is
    // intentionally-disabled-but-healthy, not crashed, so Status()/Health()
    // report correctly rather than "stopped".
    if err := s.BaseService.Start(ctx); err != nil {
        return err
    }
    if s.reg == nil { // R1 (disabled this boot) + R3 (no consumers): idle, no goroutines.
        s.logger.Info("ownership service: no registry — running idle (disabled this boot)")
        return nil
    }
    runCtx, cancel := context.WithCancel(ctx)
    s.cancel = cancel
    s.wg.Add(2)
    go func() { defer s.wg.Done(); s.staticHB.Run(runCtx) }()
    go func() { defer s.wg.Done(); _ = s.reg.WatchRevival(runCtx, s.metrics) }()
    return nil
}

func (s *OwnershipService) Stop(timeout time.Duration) error {
    if s.cancel != nil {
        s.cancel() // signal (no-op on the disabled path — cancel is nil)
    }
    // Join with timeout — inline, mirroring HeartbeatService.Stop:142-160.
    done := make(chan struct{})
    go func() { s.wg.Wait(); close(done) }()
    select {
    case <-done:
    case <-time.After(timeout):
        s.logger.Warn("ownership service: stop timeout waiting for goroutines")
    }
    return s.BaseService.Stop(timeout)
}
```

`Start` only ever errors if `BaseService.Start` does (exotic) — there is no
soft-failure path that returns an error, so it cannot trip `StartAll`'s boot
abort (R1). The Registry is never constructed here (R2). With zero enrolled
owners both goroutines idle (R3). `s.wg` mirrors `HeartbeatService`'s own-`wg`
precedent; cancellation is via `runCtx` because the goroutines (`staticHB.Run`,
`WatchRevival`) already take a context.

### The one new primitive — `RegisterInstance`

`OwnershipService` is built by the composition root (it borrows the Phase-A
Registry, which is not config-shaped), but the `ServiceManager` today only
admits services via config-driven constructors (`CreateService`). We add one
small method — the minimal honest gap-closer, not a subsystem:

```go
// RegisterInstance admits a pre-built Service to the manager (composition-root
// wiring, as opposed to config-driven CreateService). Same map + order tracking
// CreateService uses, so StartAll/StopAll treat it identically.
func (m *Manager) RegisterInstance(name string, svc Service) {
    m.mu.Lock(); defer m.mu.Unlock()
    m.services[name] = svc
    m.order = append(m.order, name)
}
```

This is the *only* new mechanism in this ADR. The constructor path is rejected
for ownership precisely because threading a non-config dependency (the Registry)
through `Dependencies` + a no-op-config constructor is more indirection than a
five-line instance register.

### What the mains become

The two divergent ownership blocks (`cmd/semstreams/main.go:209-266` and the
`wireLifecycleManager` body in `cmd/e2e-semstreams/main.go:264-315`) collapse to
one shared Phase-A call + one Phase-B registration + the existing
post-construction bind:

```go
// Manager-internal heartbeater (spawned eagerly by AttachOwnership in Phase A)
// needs a shutdown-cancellable ctx + a join. The Manager is deliberately NOT a
// Service (rollout step 2 — it fails the Is-it-a-Service test; WaitOwnership
// already provides the join). The cancel+join is factored into one shared helper
// so the two mains cannot drift; the deferred cleanup runs cancel→join, both
// before the earlier-registered NATS Close defer (gh#279).
hbCtx, ownershipShutdown := service.WireOwnershipShutdown(ctx, svcDeps.LifecycleManager)
defer ownershipShutdown()

ownerReg, staticHB := service.WireOwnership(hbCtx, natsClient, svcDeps.LifecycleManager, logger) // Phase A
manager.RegisterInstance("ownership", service.NewOwnershipService(ownerReg, staticHB, metricsRegistry, logger)) // Phase B
// ... configureAndCreateServices(...) constructs rule processors ...
if ownerReg != nil {
    service.BindRulePackContracts(hbCtx, manager, ownerReg, staticHB, logger) // Phase A, post-construction
}
```

What deletes: the hand-rolled `ownershipWG`, the two ownership goroutine spawns
(`go staticHB.Run` / `go WatchRevival`), and their `WaitGroup.Wait` defer — those
move into `OwnershipService`, which `StopAll` cancels-then-joins in reverse
registration order before `run()` returns to the NATS `Close` defer.

What STAYS (corrected — do not over-claim the cleanup): the Manager-internal
heartbeater is **not** a Service, and is **never** wrapped as one (see rollout
step 2 below). It stays **Phase-A-spawned** by `AttachOwnership` so its boot-time
spawn behavior is preserved. Its `hbCtx`/cancel and the `WaitOwnership()` join —
the gh#279 fix from ADR-056 PR-4 — are factored into the shared
`service.WireOwnershipShutdown` helper rather than left hand-rolled per binary.
PR-1 (ownership) left them hand-rolled identically in both mains; rollout step 2
folds them into the helper.

**Half-migration guard:** PR-1 already converged the two mains to the identical
three-line `hbCtx`/`hbCancel`/`WaitOwnership` pattern (`cmd/semstreams/main.go`
and `cmd/e2e-semstreams/main.go`). The drift risk is therefore *prospective* — a
future editor diverging one main's three lines from the other's. Rollout step 2
eliminates that class structurally by replacing the three lines with one shared
`WireOwnershipShutdown(ctx, mgr)` call both mains make identically. Edit BOTH
mains in the same PR and verify the call sites are shape-identical.

## Consequences

**Positive**

- `run()` drops back under the function-length limit; new background
  subsystems become "write a Service + register it," not "add a goroutine +
  WaitGroup + defer to `run()`."
- The Phase-A sequence lives in one shared helper, so the two mains cannot
  drift — the beta.18 half-migration class is structurally prevented for this
  code.
- Ownership goroutines get uniform health + metrics + ordered shutdown via the
  framework, for free.

**Negative / cost**

- One new `service.Service` type per migrated subsystem (small, mechanical,
  mirrors `HeartbeatService`).
- One new `ServiceManager.RegisterInstance(name, Service)` method (~5 lines) —
  the single new framework mechanism, to admit composition-root-built service
  instances. Justified over the constructor path (which would force a non-config
  dependency through `Dependencies`); covered by a test.
- The R1 infallible-`Start` discipline is a footgun if forgotten: a future
  author who returns an error from a best-effort `Start` re-introduces the
  boot gate. Mitigation: state it in the Service's doc comment and cover it
  with a test that asserts `Start` returns `nil` on the disabled path. It is a
  review checklist item, not a new mechanism.
- Phase 1 (PR-1, ownership) does NOT fully clean the mains: the lifecycle
  Manager-internal heartbeater's cancel + `WaitOwnership` join stay hand-rolled
  in both mains. Rollout step 2 resolves this — NOT by making lifecycle a Service
  (it isn't one; `WaitOwnership` already joins), but by factoring the cancel+join
  into the shared `service.WireOwnershipShutdown` helper. The heartbeater stays
  permanently Phase-A-spawned by design; there is no further "until lifecycle is a
  Service" step.

**Neutral**

- No config-schema change. `OwnershipService` is wired by the composition root
  via `RegisterInstance`, not a config-registered constructor, because its
  Phase-A dependency (the Registry) is not config-shaped. (If a later subsystem
  *is* config-driven, use the existing `Constructor` / `CreateService` path
  instead.)

## Rollout — one behavior-preserving PR per subsystem

1. **Ownership** (this ADR's forcing function): extract `WireOwnership` +
   `OwnershipService`, delete the inline blocks and `wireLifecycleManager`'s
   ownership half. Behavior-preserving: same goroutines, same shutdown order,
   same observe-only posture.
2. **Lifecycle Manager** (DONE): `construct + workflow Register` stays Phase A.
   The Manager is deliberately NOT wrapped as a Service — it fails the
   Is-it-a-Service test (criterion 3's clean Stop/join is already provided by
   `WaitOwnership`), and a wrapper would be import-cycle-forced ceremony that
   either moves the heartbeater spawn from Phase A to `StartAll` (a behavior
   change) or has a no-op `Start`. Instead, the hand-rolled
   `hbCtx`/`hbCancel`/`WaitOwnership` is factored into the shared
   `service.WireOwnershipShutdown(ctx, mgr)` helper so the two mains cannot drift.
   Behavior-preserving: the heartbeater spawn point is unchanged.
3. **Milestone subscriber** (`cmd/semstreams/main.go:283-297`): already
   `Start()`-returns-a-stop-func — Service-shaped, wrap it.
4. **pprof server** (DONE): the duplicated `startPProfServer` (both mains) is
   factored into the shared `service.MaybeStartPProf(debug, port)` helper — and
   deliberately NOT a Service. Applying the Is-it-a-Service test (like step 2 did
   for the lifecycle Manager): pprof fails criterion 3 — an HTTP `/debug/pprof`
   mux has no process state to flush, so process exit cleans it with no
   correctness loss (it is fire-and-forget today). And it is started EARLY, before
   NATS, so a wedged/slow boot stays profilable; a StartAll-timed Service would
   lose that window. So, mirroring step 2, the duplication is killed by a shared
   helper, keeping the early fire-and-forget start. Behavior-preserving.

Each PR is independently revertible and adds nothing to the public surface
beyond a new internal Service type.

## Explicitly NOT in scope (anti-over-engineering)

We considered and rejected each of these because the existing
`service.Service` + a plain helper already covers the need:

- A boot-orchestration engine, dependency-DAG resolver, or DI container —
  Phase-A ordering is source-order sequential code; a DAG would encode an
  ordering Go already gives us for free.
- A lifecycle-phase enum or boot state machine — "Phase A vs Phase B" is a
  *classification for humans*, not a runtime construct. Encoding it as state
  would be a mechanism with no reader.
- A generalized `Subsystem` interface beyond `service.Service` — there is
  nothing a boot subsystem needs that `service.Service` does not already
  provide.
- A new registry type or config-schema change — the existing `Dependencies` /
  `Constructor` / `CreateService` path covers the config-driven case; the
  non-config case is handled by the one new `RegisterInstance` *method* on the
  existing `Manager` (not a new registry, not config).
- A big-bang migration — the rollout is one subsystem per PR, each
  behavior-preserving.

## 1. Atomic Stop contract prerequisite

- [x] 1.1 In one PR, change `Service.Stop`, `Manager.StopAll`, and `LifecycleComponent.Stop` to accept
  `context.Context`; migrate every SemStreams implementation and direct caller.
- [x] 1.2 Preserve service idempotency, reverse order, continued stop attempts, and genuine error aggregation.
- [x] 1.3 Remove service and component shutdown roots; prove Start-owned goroutines are canceled and joined.
- [x] 1.4 Remove `ManagedComponent.Cancel`; keep cancellation only in private synchronized manager state.
- [x] 1.5 Add service tests for nil context, cancellation, deadline, repeated Stop, StopAll order, aggregation, and
  races.
- [x] 1.6 Add component tests for nil context, cancellation-before-wait, deadline, repeated Stop, private cancellation,
  and races.
- [x] 1.7 Add a generation-binding test proving cancel, exact in-flight Start completion/finalization, then
  same-generation Stop, with no Start/Stop method overlap.
- [x] 1.8 Establish the manager lifecycle supervisor with Start context passed as its goroutine function parameter;
  prove dynamic post-boot Start descends from that context rather than a request context.
- [x] 1.9 Add no duration adapter, deprecated overload, default duration, or incomplete intermediate signature set.
- [x] 1.10 Obtain semstreams-reviewer approval; run `task lint`, `go test -race ./...`, integration and contract tests,
  schema drift check, `task e2e:core`, and `task e2e:semantic` before merging the atomic prerequisite.

Completion of section 1 is deliberately narrow. The implementation retains only private `lifecyclejoin.Generation`
and `lifecyclejoin.Operation` cancellation, completion, and terminal-result authority; it stores no new contexts.
NATS, JetStream, and HTTP shutdown uses the protocol's native drain/shutdown completion plus generation-scoped joins.
The sole new framework-owned root is the bounded five-second context used synchronously by
`RunPartialStartRollback` when an uncommitted Start has no external Stop caller. It does not launch detached cleanup.

The completed contract also permits a failed Start to retain cleanup authority. Such a generation rejects another
Start until a later Stop, called with a fresh live shutdown context, reaches terminal cleanup. In particular,
`MilestoneSubscriber.Start` can return both a non-nil stop function and a non-nil error after partial acquisition;
the caller must preserve and invoke that stop function.

## 2. Replacement lifecycle protocol

- [ ] 2.1 Remove runtime handles from Registry generations, snapshots, observers, and flow graph.
- [ ] 2.2 Internalize Registry `CreateComponent`, `ReplaceComponent`, and `GetFactory`; retire `Component`,
  `ListComponents`, and deprecated `GetComponent`; leave exported Registry methods registration-only or value-only.
- [ ] 2.3 Retire ComponentManager raw handle reads, exported `ManagedComponent` leakage, `component.Lookup`, and
  `Dependencies.ComponentRegistry`; add value DTO observation and scoped `WithComponent` borrows.
- [ ] 2.4 Add typed missing, Transitioning, and Failed borrow errors; prohibit same-instance lifecycle mutation inside
  a borrow callback.
- [ ] 2.5 Implement replacement/removal gate close/drain, cancellation, exact Start finalization, and same-generation
  Stop in that order.
- [ ] 2.6 Implement terminal manager Stop as validate context, close all gates, cancel all runtimes, drain borrows,
  await exact Start/finalization, then component Stop.
- [ ] 2.7 Prove cancellation is irreversible availability loss and successful Stop alone authorizes infallible commit.
- [ ] 2.8 On post-cancel Start-drain expiry, retain incumbent Failed/unavailable, discard candidate, and launch no
  detached cleanup; later authorized cleanup joins before Stop.
- [ ] 2.9 Reuse the prerequisite supervisor for candidate Start and prove request cancellation after replacement
  admission does not cancel runtime.
- [ ] 2.10 On candidate Start failure, cancel and join that generation, call Stop, remove exact partial store claims,
  then retain it current Failed/unavailable without predecessor restoration.
- [ ] 2.11 Add deterministic active-borrow races for Remove and terminal Stop, plus replacement, Stop failure,
  drain-expiry, and Start-failure tests.
- [ ] 2.12 Obtain reviewer approval; run all local gates, `task e2e:core`, and `task e2e:semantic` before merge.

## 3. Remaining context debt

This entire phase remains deferred. In particular, the stored-context and invented-root debt inventoried in Rule and
the other phase-3 areas is not erased by completion of the atomic Stop prerequisite.

- [ ] 3.1 Remove the nine stored context fields across eight structs listed in `inventory.md` in reviewed slices.
- [ ] 3.2 Inventory and remove remaining production Background, TODO, WithoutCancel, nil fallback, and indirect roots.
- [ ] 3.3 Add a type-aware zero-debt guard that distinguishes process/test roots from production library violations.
- [ ] 3.4 Verify no exported lifecycle record exposes `context.CancelFunc`.
- [ ] 3.5 Give every slice focused race tests and the relevant E2E tier; do not combine unrelated cleanup.

## 4. Migration and release evidence

- [x] 4.1 Keep the SemStreams migration guide synchronized with exact final signatures and compiler-visible changes.
- [x] 4.2 Record sister-repository callsites as read-only notices; do not edit sister repositories.
- [x] 4.3 Add the breaking changelog entry and name the atomic Stop prerequisite.
- [x] 4.4 Run `openspec validate restore-go-lifecycle-ownership --strict` after every contract edit.
- [ ] 4.5 Before the next breaking tag, run all required E2E tiers and record exact commits and results.

Implementation evidence for the completed atomic prerequisite:

- independent `semstreams-reviewer` approval after integration;
- `task lint`, `go test -race ./...`, `task test:integration`, and `go test ./test/contract/...` passed;
- `task schema:generate` produced no schema or materialized-spec drift;
- strict OpenSpec validation passed;
- `task e2e:core` passed 3/3 scenarios;
- `task e2e:semantic` passed 48/48 scenarios. Its non-gating thematic recorder also reported one degraded-floor
  observation; that recorder metric does not change the green 48/48 gate result.

Task 4.5 remains open because this evidence belongs to an uncommitted implementation worktree and therefore cannot
yet name the exact breaking commit/tag. The breaking entry is indexed from `docs/README.md`; the repository's release
workflow later derives the GitHub changelog from commit subjects. No sister repository was edited.

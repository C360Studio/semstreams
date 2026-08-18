## 1. Historical completed Stop(ctx) prerequisite

Checked items in this section record the landed context-bearing signature prerequisite. They do not define current
lifecycle mechanics, terminal ordering, failed-Start behavior, or restart proof.

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
  prove its supervisor-owned Start context rather than a request context. The historical dynamic post-boot Start half
  is superseded by ADR-094 and is not a current target.
- [x] 1.9 Add no duration adapter, deprecated overload, default duration, or incomplete intermediate signature set.
- [x] 1.10 Obtain semstreams-reviewer approval; run `task lint`, `go test -race ./...`, integration and contract tests,
  schema drift check, `task e2e:core`, and `task e2e:semantic` before merging the atomic prerequisite.

Completion of section 1 is deliberately narrow. The context-bearing signature prerequisite is complete. Historical
implementation and E2E evidence below proves only that prerequisite at its original integration point.

## 2. Delegated lifecycle and composition work

No tasks are tracked in this section. `require-restart-for-config-activation` owns Registry and boot-only composition.
ADR-095 and `simplify-one-shot-lifecycle-ownership` own exact Start finalization, failed-Start cleanup,
callback-borrow fencing, terminal owner sequencing, native drain/Closed behavior, ACK ordering, controlled/dirty
restart proof, and lifecycle-specific release gates. Delegation grants this change no completion credit.

## 3. Remaining context debt

This phase remains required. The beta.161 refresh found five retained direct runtime contexts across four structs and
39 unauthorized production roots after the approved rollback/BaseContext exceptions; the older nine/eight baseline
count below is historical, not current truth.

- [ ] 3.1 Remove the five retained direct runtime contexts across the four beta.161 structs listed in `inventory.md`.
- [ ] 3.2 Inventory and remove remaining production Background, TODO, WithoutCancel, nil fallback, and indirect roots.
- [ ] 3.3 Add a type-aware zero-debt guard that distinguishes process/test roots from production library violations.
- [ ] 3.4 Verify no exported lifecycle record exposes `context.CancelFunc`.
- [ ] 3.5 Give every slice focused race tests and the relevant E2E tier; do not combine unrelated cleanup.

## 4. Historical prerequisite migration and evidence

- [x] 4.1 Keep the SemStreams migration guide synchronized with exact final signatures and compiler-visible changes.
- [x] 4.2 Record sister-repository callsites as read-only notices; do not edit sister repositories.
- [x] 4.3 Add the breaking changelog entry and name the atomic Stop prerequisite.
- [x] 4.4 Run `openspec validate restore-go-lifecycle-ownership --strict` after every contract edit.

Implementation evidence for the completed atomic prerequisite:

- independent `semstreams-reviewer` approval after integration;
- `task lint`, `go test -race ./...`, `task test:integration`, and `go test ./test/contract/...` passed;
- `task schema:generate` produced no schema or materialized-spec drift;
- strict OpenSpec validation passed;
- `task e2e:core` passed 3/3 scenarios;
- `task e2e:semantic` passed 48/48 scenarios. Its non-gating thematic recorder also reported one degraded-floor
  observation; that recorder metric does not change the green 48/48 gate result.

The next-tag lifecycle and E2E release gate is tracked only in `simplify-one-shot-lifecycle-ownership`; this historical
evidence does not satisfy it. No sister repository was edited.

# Design: One-shot running lifecycle with retained failed-Start cleanup authority

## Surface inventory

### 1. Claimed gap: OpenSpec transaction shape

OpenSpec `MODIFIED` blocks must use the exact title of a requirement already present in the current capability spec.
An invented or renamed title under `MODIFIED` cannot match current truth. A title change must be represented as the
exact current requirement under `REMOVED` plus the replacement under `ADDED`.

The exact current titles reached by this change are:

| Current spec and line | Exact current requirement title | Delta operation |
|---|---|---|
| `openspec/specs/service-shutdown/spec.md:16` | `Coordinated shutdown treats an already-stopped service as clean success` | `MODIFIED` exact title |
| `openspec/specs/service-shutdown/spec.md:47` | `A framework service Stop is idempotent on repeated invocation` | `MODIFIED` exact title |
| `openspec/specs/jetstream-consumer-policy/spec.md:37` | `Every exported port-backed consumption operation requires policy context` | `MODIFIED` exact title |
| `openspec/specs/jetstream-consumer-policy/spec.md:54` | `Non-port consumption is explicit and bounded` | `MODIFIED` exact title |
| `openspec/specs/jetstream-consumer-policy/spec.md:87` | `Consumer policy metrics never retain stale effective truth` | `MODIFIED` exact title |
| `openspec/specs/graph-ingest/spec.md:162` | `Concurrent ingest MUST bound in-flight work and preserve at-least-once ack` | `MODIFIED` exact title |
| `openspec/specs/graph-ingest/spec.md:181` | `A redelivered stale message MUST NOT overwrite a newer write` | `MODIFIED` exact title |

New concepts—failed-Start cleanup authority, duplicate durable identity rejection, and the new
`restart-safe-shutdown` capability—use `ADDED`, not `MODIFIED`.

The current `openspec/specs/lifecycle/spec.md` is domain workflow lifecycle and remains untouched.

### 2. Every retained PR #984 restart-safe guarantee

PR #984's conflicting handle/rejoin/deletion mechanics are superseded, but the following normative guarantees are all
retained and must be transferred before its old delta is removed:

| Guarantee | Existing source | New-delta destination |
|---|---|---|
| broad mutable NATS roots retire; no `Unsafe*`; sister repos read-only | old restart-safe spec `:170-186` | requirement `Broad NATS ownership roots retire before release` |
| durable handler ACK follows required effects/publications | `:188-210` | requirement `Durable settlement distinguishes completed from unfinished work` |
| accepted outbound NATS publications flush/drain before clean close | `:195-217` | same settlement requirement and scenario |
| all controlled shutdowns exit; fresh Client/process; clean/non-clean status | `:219-255` | requirement `Restart safety is proven across a real process boundary` |
| proof includes in-flight and pending work, semantic result, recovery, listener/consumer ownership, next-boot config | `:227-255` | same controlled-proof requirement |
| dirty recovery is independent of Stop, Drain, defer, finalizer, detached cleanup | `:257-267` | requirement `Dirty restart correctness does not depend on shutdown hooks` |
| crash-critical work/state uses durable JetStream or KV; core NATS excluded unless loss is explicitly noncritical | `:259-286` | same dirty-recovery requirement |
| crash-critical streams/KV are file backed and live storage/replica policy is validated at boot | `:264-293` | same dirty-recovery requirement |
| effect-before-ACK and idempotent/stable-key convergence; no false exactly-once external claim | `:269-300` | same dirty-recovery requirement |
| deterministic process kill after delivery/effect/publication/before ACK | `:302-319` | requirement `Dirty restart is proven at settlement boundaries` |
| SemStreams and isolated NATS are killed; NATS restarts from the same file store | `:304-326` | same dirty-proof requirement |
| every boot consumes latest committed desired state regardless of prior exit | `:309-333` | same dirty-proof requirement |
| clean-exit marker is observability, never activation prerequisite | `:311-333` | same dirty-proof requirement |
| stale prior-boot observation is never current evidence for new boot | `:328-333` | same dirty-proof scenario |

No row may be replaced by a summary sentence in proposal/design/tasks alone. Each remains a normative requirement or
scenario in the new `restart-safe-shutdown` delta.

### 3. Active `require-restart-for-config-activation` inventory

Exact files requiring reconciliation:

```text
openspec/changes/require-restart-for-config-activation/proposal.md
openspec/changes/require-restart-for-config-activation/design.md
openspec/changes/require-restart-for-config-activation/tasks.md
openspec/changes/require-restart-for-config-activation/specs/jetstream-consumer-policy/spec.md
openspec/changes/require-restart-for-config-activation/specs/restart-safe-shutdown/spec.md
docs/operations/migration-restart-safe-nats-client.md
```

The proposal, design, and tasks must explicitly delegate the complete restart-safe capability to
`simplify-one-shot-lifecycle-ownership`. The two old spec delta files leave PR #984's active target only after their
complete compatible content exists in the new delta. PR #984 retains boot-only composition, rule hot reload, flow
activation truth, raw-root dependency, controlled/dirty proof gates, and latest-desired-state dependency by reference
to the new capability.

These PR #984 files remain byte-identical:

```text
docs/adr/094-boot-only-composition-and-observable-rule-activation.md
openspec/changes/require-restart-for-config-activation/inventory.md
openspec/changes/require-restart-for-config-activation/native-surface-inventory.md
```

All non-lifecycle spec deltas remain owned by PR #984.

### 4. Active `restore-go-lifecycle-ownership` inventory

Exact package:

```text
openspec/changes/restore-go-lifecycle-ownership/proposal.md
openspec/changes/restore-go-lifecycle-ownership/design.md
openspec/changes/restore-go-lifecycle-ownership/inventory.md
openspec/changes/restore-go-lifecycle-ownership/tasks.md
openspec/changes/restore-go-lifecycle-ownership/specs/runtime-context-ownership/spec.md
openspec/changes/restore-go-lifecycle-ownership/specs/service-shutdown/spec.md
```

Compatible retained truth:

- context-bearing `Service.Stop`, `Manager.StopAll`, and `LifecycleComponent.Stop` signatures;
- Start context is runtime authority; production structs retain cancel/join state, not `context.Context`;
- no invented/detached library roots, with the measured synchronous failed-Start rollback and HTTP BaseContext
  exceptions;
- caller Stop context bounds work but is not runtime authority;
- nil context is rejected before action;
- manager Start context is passed as a goroutine function parameter;
- exact `startDone`/Start finalization and no Start/Stop method-body overlap;
- failed Start may retain exact cleanup authority, reject another Start, and later clean under manager Stop;
- callback borrow gates close without manager/gate locks during callbacks or waits.

Conflicting truth to remove/delegate:

- `design.md:29-31,118-121,132`: repeated running Stop rejoins and retained genuine error;
- `specs/service-shutdown/spec.md:5-8,28-31`: `stopping` is already clean;
- the same file `:69-82`: concurrent Stop shares completion and repeated Stop replays retained error;
- `tasks.md:21-30,42-43`: stateful `Generation`/`Operation` terminal-result authority and rejoinable deadline failure;
- `runtime-context-ownership/spec.md:78-128`: keep context ownership and exact ordering, but change generic idempotency to
  completed repeated no-op and explicitly prohibit pre-cancel WG wait.

`restore-go-lifecycle-ownership/specs/service-shutdown/spec.md` must leave that active change after the new change owns
the exact current-title modifications. The restore proposal/design/tasks/inventory must point service-shutdown semantics
to ADR-095 and `simplify-one-shot-lifecycle-ownership`; its runtime-context capability remains active.

### 5. Consumer at birth

| Surface | Present consumer | Disposition |
|---|---|---|
| native `jetstream.ConsumeContext` | every owner starting JetStream delivery | exact returned lifecycle handle |
| stateless context wait helper | 42 lifecycle owners | internal, no stored context/result |
| bounded failed-Start rollback | 21 paths | retained with cleanupPending authority |
| duplicate identity check | sealed local composition | boot validation; reject-only claim only when not derivable |
| backlog observer | graph readiness and accepted agent-loop inflight | read-only and separate from lifecycle |
| fixture/admin deletion | namespaced tests | no production Stop/config surface |
| concurrent/rejoin/retained running Stop | no production consumer | removed from both active changes |

## Adopter seam inventory

### Component author

The author retains the exact returned native handle and implements one owner Stop. The framework sequence is fixed:
fence; native Drain/Shutdown; exact native Closed while callback authority is live; cancel remaining runtime; await
owner WG/done; cleanup. They do not know consumer identity derivation, backlog math, deletion policy, generation IDs,
rejoin, or Client catalogs.

If Start fails after acquisition, the owner record remains authoritative until bounded rollback or later manager Stop
completes cleanup. Clearing it early is a typed boot failure, not a documentation concern.

### Agent-loop inflight caller

No adopter change. The accepted exported request/reply contract continues to hide stream/durable names and return
unknown rather than false zero. Only its internal observation source moves out of Client lifecycle bookkeeping.

### Supervisor and test author

The supervisor observes process exit status and never rejoins a running process generation. A test fixture records and
deletes only its own durable identities through fixture/admin teardown; it does not set a production deletion knob.

## Target-state decisions

### D1. Running Stop is one-shot and direct

The exact native owner order is fence, native Drain/Shutdown, exact Closed with callback authority live, cancel, owner
join, and terminal cleanup. Deadline failure remains non-clean and still issues cancellation before any ctx-driven join.
Completed repeated Stop is nil/no-op; concurrent Stop and running-generation rejoin are not contracts.

### D2. Failed Start retains cleanup authority

The owner publishes cleanup authority before acquisition escapes and retains `startDone` where Stop can race Start.
Failed or expired rollback retains every exact handle in `cleanupPending`, rejects another Start, and permits later
manager Stop. This is distinct from running-generation rejoin.

### D3. Native consumption is the commit point

All fallible setup and observation precedes `Consumer.Consume`; successful commit returns the exact native
`jetstream.ConsumeContext`. Duplicate local identity rejects rather than replaces. Observation and fixture deletion
remain separate from lifecycle.

### D4. Complete PR #984 guarantees transfer normatively

The restart-safe delta owns broad-root retirement, settlement and outbound flush, controlled process proof, dirty
recovery without hooks, durable-only crash-critical communication, live storage/replica validation, external-effect
limits, SemStreams/NATS kill proof, clean-marker independence, and latest-desired-state recovery.

### D5. Restore retains context truth, not terminal-result semantics

The restore change keeps caller-owned context signatures, lexical Start authority, nil rejection, exact Start
finalization, failed-Start cleanup, and no detached roots. ADR-095 and this change exclusively own service-shutdown and
terminal sequencing.

## Approved-ruling conformance

| Approved ruling | Contract evidence |
|---|---|
| Exact current-title delta transaction | `specs/service-shutdown/spec.md`; `specs/jetstream-consumer-policy/spec.md`; `specs/graph-ingest/spec.md` |
| Complete PR #984 guarantee transfer | `specs/restart-safe-shutdown/spec.md` |
| PR #984 delegates the complete lifecycle target | `../require-restart-for-config-activation/{proposal,design,tasks}.md` |
| Restore delegates service-shutdown and preserves runtime-context truth | `../restore-go-lifecycle-ownership/` |

Every runtime and proof task remains unchecked.

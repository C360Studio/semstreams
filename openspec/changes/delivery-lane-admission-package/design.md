# Design — delivery-lane-admission-package (#1341) — round 2

> Base `20fe8d09` for every `file:line` unless prefixed `L3:` (= `c58c65bd`, the rebased L3 head, the implementation
> base). `inventory.md` beside this file is the checkpoint: `INVENTORY PASS` recorded (`latch/inventory-pass.md`),
> MAJOR-1/MAJOR-2/MINOR-1..3 closed in this revision; `scripts/inventory-verify.sh` →
> `pins=268 ok=268 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`, EXIT=0. Round-1 review:
> `latch/design-review.md` (DESIGN CHANGES REQUESTED, 0 BLOCKING); every finding is addressed below and listed in
> § 14. Nothing here is owner-approved.

## 1. The problem, in one paragraph

Five components each carry the same reaction to `natsclient.DeliveryResult.OwnerStopRequired()`: latch the lane
closed, record the fatal into health synchronously, buffer the result, and let an observer drain the exact
`jetstream.ConsumeContext` once. The copies are 550 lines in five `delivery_owner.go` files (594 with governance's
out-of-file drain and wrapper and five binding structs); they have drifted in three enumerable ways (§ 2); the
#1249 draft is about to add a sixth (`gh1249/design-draft-4.md:85-92`). The ruling is to hoist. This design records
the home (decided, § 3), the shape, every per-copy difference as a parameter or a one-line composition, the size
against the ~550 it replaces, and what #1249 and L4 call instead.

## 2. Pairwise differences between the five copies (all at `20fe8d09`; byte-identical at `L3:`)

| Aspect | dispatch | tools | loop | model | governance | Expressed in the shared shape as |
|---|---|---|---|---|---|---|
| Panic wrapper (`run*DeliveryWork`) | in file `:13`, cause prefix `dispatch` | none | in file `:12`, prefix `loop` | none | `component.go:349`, prefix `governance` | unexported `runWork(ctx, data, owner, work)` inside `Settle`; `owner` is the prefix, so `"governance delivery work panicked"` (asserted at `delivery_settlement_test.go:138`) is byte-identical. Heartbeat lanes never needed it (the typed helper synthesizes Quarantine on panic, D6). |
| `onRefused` field + `refuse(subject)` | yes `:32`, `:51` | yes `:19`, `:38` | no | no | no | `NewAdmission(onFatal, onRefused)`; nil = undeclared, which is what loop/model/governance and dispatch's settlement lane do today. **Strictly narrower than today, and closer to spec `:603`:** today's `consumeAdmittedDelivery` computes `msg.Subject()` on every refusal (dispatch `:86-92`, tools `:73-79`); the draft reads it only when a declarer exists. No production lane observes the difference (every lane reaching the path declares); three tools test sites that pass `(nil, nil)` (`:137`, `:182`, `:209`) take the narrower path and assert nothing about it. |
| `consumeAdmittedDelivery` | yes, refuse arm `:80-97` | yes, refuse arm `:67-84` | yes, silent `:74-86` | yes, silent `:60-72` | none | `Consume(ctx, msg, policy, admission)`: one body; the arm is the nil check above. Governance simply does not call it. |
| Settlement-only callback body (admit → work → settle → latch) | 3 lanes inline at this base; **1 at `L3:` (`:571-576`, user.message)** — `agent.created` / `agent.approval_pending` deleted by `2693df0e` | — | 1 shape inline `:1090-1096` (`L3:1121-1127`), `SettleDeliveryWithRetry(settleRetry)` | — | inline `:511-516` (`L3:` same), `SettleDelivery` | `Settle(ctx, msg, retry, admission, owner, work)`; dispatch and governance pass `natsclient.ImmediateDeliveryRetry()`, which is exactly what `SettleDelivery` does (`natsclient/delivery_settlement.go:291-293`). |
| `recordDeliveryOwnerFatal` | `component.go:798` (`L3:721`), plus per-lane `recordAgentCompleteFatal`/`recordAgentFailedFatal` | `component.go:521`, `errors++` | in file `:65`, no counter | in file `:50`, `errors++` | `component.go:718`, atomic `errors++` | Stays a component method passed as `onFatal`. The two in-file ones relocate to their `component.go` (17 lines). Health semantics untouched. |
| Binding ctor + drain-once | `:99`, `:112` | `:86`, `:99` | `:88`, `:92` | `:74`, `:78` | ctor `:54`; drain `component.go:728` | `NewBinding(handle) *Binding`, `(*Binding).Drain()`. Pointer type: today's `drainOnce *sync.Once` exists only so a value copy in `c.consumers` shares the Once with the observer's local; a pointer makes that structural, and `go vet` copylocks makes an accidental copy loud. |
| Observer signature | `(ctx, binding, admission)` | same | `+ portName` | `(ctx, binding, admission)` | `+ portName` | `Observe(ctx, binding, admission, react)`; the port name is captured by the caller's closure. `react` is required (§ 5). |
| Observer fatal arm | `observeTerminalDelivery(err)` + `logger.Error("Terminal delivery ownership lost")` | `recordHandlerError(ctx, err)` | `logger.Error("Loop delivery ownership lost", port)` | `logger.Error("Model delivery ownership lost")` | `logger.Error("Governance delivery ownership lost", port)` | `react` per component; `Observe` then drains. Order (reaction, then drain) is today's order in all five. |
| Imports | `fmt`, `slog` | — | `fmt` | — | — | Package imports `context`, `fmt`, `sync`, `natsclient`, `jetstream`. `slog` stays with the callers. |

## 3. Home — decided; options kept for the record

**Decision (owner, 2026-09-19, round 2): `internal/deliverylane`.** Export is a future gate, not taken: its trigger is
semdev's scheduled migration off `ConsumeWithHeartbeat` (two sites, `inventory.md` § 4), at which point option C is a
directory move plus the RC-6 walked path — the spec delta names no path (MEDIUM-5), so no second spec delta is owed.

**A. `internal/deliverylane` (taken).** Importable by every in-module owner including `agentic/agentrun` (#1249).
Outside both ADR-106 tiers; `scripts/api-compat.sh` sees nothing. Precedents: `internal/lifecyclecleanup`
(`lifecyclecleanup.go:1-2`) and `internal/maxdelivery` (imports `natsclient` and `jetstream`, `observer.go:18,22`). No
import cycle: `natsclient` imports nothing under `internal/` or `processor/`. Reconciles with D8 (archived design
`:229`) and ADR-095 `:26`: the owner constructs and retains every value; the package has no registry, no catalog, no
goroutine except the per-lane observer the owner starts on its Start-derived context and joins in Stop.
`openspec/project.md:19-23` admits a package on two-or-more product reuses or a recorded usability gap; present
external consumers are zero, so the admission test for an exported form is not met today.

**B. `natsclient`.** Rejected: D8 says "Shared natsclient owns no lifecycle"; spec `:651` forbids a shared helper
owning admission or a native handle inside the settlement package; `Client owns no consumer or subscription
children` (`:304`); Tier 1 widening (`release/tier1-packages.txt:57`); a goroutine-spawning function in `natsclient`
for the first time.

**C. `pkg/deliverylane` (exported).** Deferred as above.

**D. Share the admission only.** Leaves 5 × ≈35 lines of binding + observer; the issue names both. Rejected.

**E. Do nothing.** Sixth copy lands (+75); five silent lanes (#1342) become five hand-edits. Rejected by the ruling.

## 4. Size, measured

| | Lines |
|---|---|
| Replaced, in the five files (`wc -l`; byte-identical at `L3:`) | **550** |
| Replaced, outside the files (governance `drain` 6 + `runGovernanceDeliveryWork` 13 + five binding structs 25) | 44 |
| New package `internal/deliverylane/deliverylane.go` (`wc -l` on the gofmt-clean round-2 draft, § 5) | **228** (139 code, 89 comment/blank) |
| Residue: relocated `recordDeliveryOwnerFatal` bodies (loop 8, model 9) | +17 |
| Residue: one reaction method per component (≈5 lines × 5) | +25 |
| Residue: five import lines | +5 |
| Residue: settlement-only callbacks collapse from 6 lines to 3 at **three** sites at `L3:` (dispatch user.message, loop, governance) | −9 |
| Residue: five Stop paths drop the `if done != nil` guard (`Done()` is never nil, § 5) — 3 lines → 1 | −10 |
| **After** (package + residue) | **256** (266 if the guards are kept) |

256 ≤ 550 (47%) against the in-file bound; 256 vs 594 all-in (43%). The −9 depends on the L3 head: at `20fe8d09` it
was −15 (five sites). Test code (not in the bound): the 36 tests / 16 files in `inventory.md` § 4 stay, edited at
their pinned lines; one new package test ≈ 130 lines.

## 5. The shape (round-2 draft text; `gofmt -l` clean; the developer lands it verbatim)

Path: `internal/deliverylane/deliverylane.go`. Draft file: `<scratchpad>/latch/draft/deliverylane/deliverylane.go`.
Changes from round 1: `runWork` unexported (MEDIUM-1); `Observe` requires `react` and fails at wiring on nil
(MEDIUM-2); `Done()` never nil — `NewBinding` seeds it with a closed channel (MEDIUM-3); `Settle` documents the nil
message as a caller contract violation while `Consume` tolerates nil because the typed helper does (NIT-2); package
doc spells out what "no lifecycle authority" excludes — Stop, restart, reconstruction, registry.

```go
// Package deliverylane owns the one owner-side reaction the typed settlement
// contract asks every durable-consumer owner to build: a per-lane admission
// latch that closes on the first result requiring owner stop, a drain-once
// binding around the exact jetstream.ConsumeContext that lane committed, and
// the observer that drains that handle when the latch closes.
//
// It owns no lifecycle authority: no Stop, no restart, no reconstruction, no
// registry of lanes. The owner constructs the admission before acquisition,
// retains the binding, decides Stop, awaits Closed, and joins the observer;
// JetStream keeps delivery authority throughout. It is internal because its
// present consumers are the five agentic components and agentrun (#1249);
// exporting it is a Tier 1 widening gated on a measured adopter outside this
// module (design § 3, option C).
package deliverylane

import (
	"context"
	"fmt"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Admission is one lane's latch. JetStream retains delivery authority; the
// latch only prevents new local work after ownership control becomes unsafe.
type Admission struct {
	mu        sync.Mutex
	open      bool
	fatal     chan natsclient.DeliveryResult
	onFatal   func(natsclient.DeliveryResult)
	onRefused func(subject string)
}

// NewAdmission returns an open latch. onFatal runs synchronously on the first
// owner-stop result, before that result is buffered for Observe, so health
// latches before the exact handle drains; nil is allowed and means the owner
// records nothing. onRefused, when non-nil, declares each delivery the closed
// latch refuses; nil leaves the refusal undeclared, which is what every lane
// without a refusal counter does today (#1342 tracks the gap).
func NewAdmission(
	onFatal func(natsclient.DeliveryResult),
	onRefused func(subject string),
) *Admission {
	return &Admission{
		open:      true,
		fatal:     make(chan natsclient.DeliveryResult, 1),
		onFatal:   onFatal,
		onRefused: onRefused,
	}
}

// Admit reports whether the lane still accepts new local work.
func (a *Admission) Admit() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.open
}

// Latch closes the lane on the first result requiring owner stop and buffers
// that result for Observe. Every later result, fatal or not, changes nothing:
// the first cause is the one health and the observer see.
func (a *Admission) Latch(result natsclient.DeliveryResult) {
	if !result.OwnerStopRequired() {
		return
	}
	a.mu.Lock()
	if !a.open {
		a.mu.Unlock()
		return
	}
	a.open = false
	a.mu.Unlock()
	if a.onFatal != nil {
		a.onFatal(result)
	}
	a.fatal <- result
}

// refuse declares a refused delivery through onRefused. The lane is drained
// rather than stopped, so buffered deliveries still reach this path after the
// latch; without a declarer only the first fatal is visible and every later
// refusal is a silent drop. The subject is the only metadata a closed lane
// reads, and only when a declarer exists to receive it.
func (a *Admission) refuse(msg jetstream.Msg) {
	if a.onRefused == nil {
		return
	}
	subject := ""
	if msg != nil {
		subject = msg.Subject()
	}
	a.onRefused(subject)
}

// Consume runs one heartbeat-lane delivery under admission: a closed lane
// refuses without reading, heartbeating, or settling; an open lane consumes
// through the typed heartbeat helper and latches on its result. The bool
// reports admission, so a caller can guard every branch on it. A nil msg is
// tolerated because the typed helper quarantines it.
func Consume(
	ctx context.Context,
	msg jetstream.Msg,
	policy natsclient.HeartbeatDeliveryPolicy,
	admission *Admission,
) (natsclient.DeliveryResult, bool) {
	if !admission.Admit() {
		admission.refuse(msg)
		return natsclient.DeliveryResult{}, false
	}
	result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)
	admission.Latch(result)
	return result, true
}

// Settle runs one settlement-only delivery under admission: a closed lane
// refuses; an open lane runs work under the panic guard, applies one terminal
// method under retry, and latches on the result. Pass
// natsclient.ImmediateDeliveryRetry() for a lane that settles immediately.
// Unlike Consume, msg must be non-nil: work needs its payload, and the NATS
// callback never delivers nil; a nil here is the caller's contract violation.
func Settle(
	ctx context.Context,
	msg jetstream.Msg,
	retry natsclient.DeliveryRetryPolicy,
	admission *Admission,
	owner string,
	work natsclient.DeliveryWork,
) (natsclient.DeliveryResult, bool) {
	if !admission.Admit() {
		admission.refuse(msg)
		return natsclient.DeliveryResult{}, false
	}
	decision, cause := runWork(ctx, msg.Data(), owner, work)
	result := natsclient.SettleDeliveryWithRetry(msg, retry, decision, cause)
	admission.Latch(result)
	return result, true
}

// runWork invokes settlement-only work and converts a panic into Quarantine
// with a cause naming the owner, so the lane latches instead of the NATS
// callback unwinding. Heartbeat lanes do not need it: the typed helper
// synthesizes Quarantine on panic itself.
func runWork(
	ctx context.Context,
	data []byte,
	owner string,
	work natsclient.DeliveryWork,
) (decision natsclient.DeliveryDecision, cause error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			decision = natsclient.DeliveryDecisionQuarantine
			cause = fmt.Errorf("%s delivery work panicked: %v", owner, recovered)
		}
	}()
	return work(ctx, data)
}

// noObserver is what Done returns for a binding no observer was started on:
// already closed, so a Stop that joins it returns at once instead of blocking
// on a nil channel forever.
var noObserver = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

// Binding retains the exact consume handle a lane committed and drains it at
// most once, whether Observe or the owner's Stop reaches it first. It is
// constructed after acquisition, so a fatal reported before the handle
// returned stays buffered in the Admission until Observe runs.
type Binding struct {
	handle    jetstream.ConsumeContext
	drainOnce sync.Once
	done      <-chan struct{}
}

// NewBinding wraps the handle acquisition returned. The owner retains the
// pointer for Stop; the package never enumerates or stops bindings itself.
func NewBinding(handle jetstream.ConsumeContext) *Binding {
	return &Binding{handle: handle, done: noObserver}
}

// Drain stops this lane through jetstream.ConsumeContext.Drain, not Stop.
// Admission latches BEFORE the handle is drained, so every buffered delivery
// Drain flushes hits closed admission, runs no work, attempts no terminal
// method, and stays pending for the reconstructed owner; an already-admitted
// in-flight delivery can finish and settle instead of being abandoned
// mid-effect. Repeated calls rejoin the one drain.
func (b *Binding) Drain() { b.drainOnce.Do(b.handle.Drain) }

// Closed is the exact handle's Closed channel, for the owner's Stop to await
// after Drain.
func (b *Binding) Closed() <-chan struct{} { return b.handle.Closed() }

// Done is never nil: it is closed once the observer started by Observe has
// exited, or already closed when no observer was started. Stop joins it after
// cancelling the observer's context.
func (b *Binding) Done() <-chan struct{} { return b.done }

// Observe starts the lane's owner-stop observer on ctx, which must derive
// from the owner's Start. On the first buffered fatal it runs react, then
// drains the exact handle; on ctx cancellation it exits without draining.
// react is required — a lane whose fatal drains with no owner reaction is a
// silent degrade — and a nil react fails here, at wiring, not at the first
// fatal. Call Observe before publishing the binding to other goroutines; the
// owner's lifecycle mutex is what orders Done against Stop.
func Observe(
	ctx context.Context,
	binding *Binding,
	admission *Admission,
	react func(natsclient.DeliveryResult),
) {
	if react == nil {
		panic("deliverylane: Observe requires a non-nil react")
	}
	done := make(chan struct{})
	binding.done = done
	go func() {
		defer close(done)
		select {
		case result := <-admission.fatal:
			react(result)
			binding.Drain()
		case <-ctx.Done():
		}
	}()
}
```

Two hooks, deliberately: `onFatal` (in `NewAdmission`) is the synchronous health writer that must run in the
callback before the result is buffered — L1's spec scenarios say health latches "before owner-stop observation
drains the exact handle" (`settle-after-durable-effect/specs/agentic-loop/spec.md:72-77`, and the model, dispatch,
governance deltas); `react` (in `Observe`) is the asynchronous owner action the five observers perform today.
`onFatal` keeps its nil guard because it has present nil consumers (loop and model tests call
`newDeliveryLaneAdmission(nil)`); `react` has none, so nil is refused at wiring.

`Done()` safety caveat (MEDIUM-3): the never-nil channel removes the block-forever path, but ordering `Done()`
against Stop still rests on `Observe` running before the binding is published under `lifecycleMu`, which it does at
every site today (dispatch `:584-587`, loop `:1112-1117`, and the same pattern in tools, model, governance). The five
`lifecycle_causal_test.go` files construct bindings by literal with no observer (`[]streamConsumerBinding{{handle:
h}}`) and are the present consumers of the do-nothing path; on pointers they become
`[]*deliverylane.Binding{deliverylane.NewBinding(h)}` and their Stop joins a closed channel.

Not exported, by category 4: `runWork` (only `Settle` calls it; every former caller is deleted by tasks 2.3–2.5,
and #1249's lanes are heartbeat lanes) and a `Fatal()` accessor (its only reader is `Observe`); the tests that today
assert `len(admission.fatal) == 1` assert `!admission.Admit()` instead, and the buffering invariant is proven once
in the package's own test (I2). Not added: metrics, a status surface, a registry, a stop-all, any `context.Context`
field (`Observe` takes the context as an argument and the goroutine closes over it; nothing stores it).

### Call-site rewrite, per component (lines at `L3:` = `c58c65bd`; re-derive at the merged L3 commit)

| Component | Today (`L3:` lines) | After |
|---|---|---|
| tools `:434-436`, `:449`, `:459-460` | `newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal, refuseFn)`; `consumeAdmittedDelivery(...)`; `binding := newStreamConsumerBinding(handle)`; `c.observeDeliveryLane(ctx, &binding, admission)` | `deliverylane.NewAdmission(c.recordDeliveryOwnerFatal, refuseFn)`; `deliverylane.Consume(...)`; `binding := deliverylane.NewBinding(handle)`; `deliverylane.Observe(ctx, binding, admission, func(r natsclient.DeliveryResult) { c.recordHandlerError(ctx, r.Err()) })` — `ctx` captured because `recordHandlerError` branches on `ctx.Err()` (`:485-500`) |
| model `:408`, `:415`, `:424-425` | same shape, no refuse arm | `NewAdmission(c.recordDeliveryOwnerFatal, nil)`; `Consume`; `binding := NewBinding(handle)`; `Observe(ctx, binding, admission, c.reactDeliveryFatal)` where `reactDeliveryFatal` logs `"Model delivery ownership lost"` |
| loop heartbeat `:1098-1100`; settlement `:1120-1127`; `:1143-1145` | `newDeliveryLaneAdmission`; `consumeAdmittedDelivery` / inline admit→`runLoopDeliveryWork`→`SettleDeliveryWithRetry`→latch; `newStreamConsumerBinding` | `NewAdmission(c.recordDeliveryOwnerFatal, nil)`; `Consume` / `deliverylane.Settle(msgCtx, msg, settleRetry, admission, "loop", settleHandlerFn)`; `binding := NewBinding(handle)`; `Observe(consumerCtx, binding, admission, func(r) { c.logger.Error("Loop delivery ownership lost", "port", port.Name, "error", r.Err()) })` |
| dispatch terminal lanes `:610-614`, `:651-655` | refuse arm + `consumeAdmittedDelivery` | `NewAdmission(c.recordAgentCompleteFatal, refuseFn)` / `NewAdmission(c.recordAgentFailedFatal, refuseFn)`; `Consume` |
| dispatch settlement lane `:569-576` (user.message — the only one at `L3:`) | inline admit→`runDispatchDeliveryWork`→`SettleDelivery`→latch | `deliverylane.Settle(msgCtx, msg, natsclient.ImmediateDeliveryRetry(), admission, "dispatch", c.handleUserMessage)` |
| dispatch bindings + observers `:584-585`, `:625-626`, `:666-667` | `newStreamConsumerBinding(handle)`; `c.observeDeliveryLane(ctx, &b, a)` | `b := NewBinding(handle)`; `deliverylane.Observe(ctx, b, a, c.reactDeliveryFatal)` where `reactDeliveryFatal` = `observeTerminalDelivery(r.Err())` + the existing `logger.Error("Terminal delivery ownership lost", ...)` |
| governance `:509-516`, `:524-525` | inline admit→`runGovernanceDeliveryWork`→`SettleDelivery`→latch; `newStreamConsumerBinding`; observer with port | `Settle(msgCtx, msg, natsclient.ImmediateDeliveryRetry(), admission, "governance", handler)`; `binding := NewBinding(handle)`; `Observe(ctx, binding, admission, func(r) { c.logger.Error("Governance delivery ownership lost", "port", port.Name, "error", r.Err()) })` |
| all five Stop paths (dispatch `:497-498`, `:505`; governance `:634-635`, `:650`; loop `:766-767`, `:785`; model `:541-542`, `:557`; tools `:672-673`, `:688`) | `binding.drain()`; `binding.handle.Closed()`; `if done := c.consumers[i].observerDone; done != nil { <-done }` | `binding.Drain()`; `binding.Closed()`; `<-c.consumers[i].Done()` (loop/model/governance keep their `select` on `ctx.Done()`); field `consumers []*deliverylane.Binding` |

`Settle` reads `msg.Data()` before the panic guard, exactly as the three inline bodies do today; a closed lane
returns before reading anything, as today. A stale comment at `L3:716-718` still names `agent.created` and
`agent.approval_pending` as lanes "this change brings under settlement" — L3's defect, flagged for L3's reviewer,
not touched here.

## 6. What the two downstream changes call instead

**#1249 (`gh1249/design-draft-4.md` § 2.6, § 2.7) — RULED amend, owner 2026-09-19 (#1341 docket).** Written assuming the
amendment: the sixth copy (`agentic/agentrun/delivery_owner.go`, +75) is struck. Per lane:
`admission := deliverylane.NewAdmission(s.recordDeliveryOwnerFatal, nil)` before `Consume`; the closure at
`agentrun.go:812` becomes `result, admitted := deliverylane.Consume(msgCtx, msg, policy, lane.admission)`; after
acquisition `lane.binding = deliverylane.NewBinding(handle)` and `deliverylane.Observe(runCtx, lane.binding,
lane.admission, react)`. `milestoneConsumerOwner` (`agentrun.go:679-689`) replaces `complete/failed
jetstream.ConsumeContext` + `completeDrained/failedDrained` with two `*deliverylane.Binding`; its `stop()`
(`:691-735`) calls `Drain()` on both (both-drain-first holds, `:716-721`), awaits both `Closed()`, cancels `runCtx`,
joins both `Done()` — a join draft-4 never specified. That deletes the draft's hand-rolled drained flags and its "No
second `drainOnce`" caveat: there is exactly one drain-once, the binding's. `DeliveryFatal()` and `RegisterMetrics`
are unaffected — they sit on `MilestoneSubscriber`, fed by `recordDeliveryOwnerFatal`, which is the `onFatal` this
package takes. Without the Q2 ruling the sixth copy lands regardless of what #1341 builds.

**L4 / #1330 (`l4/change/design.md`).** Nothing new to call. Its draft lists `delivery_owner.go` under "Survives"
(`:217`) — after this change that file does not survive, so L4's Survives list needs one edit on rebase. L4 edits
the loop's `setupConsumer` neighbourhood and handlers, not the lane mechanics; after this change those lanes read
`deliverylane.Consume` / `deliverylane.Settle` / `deliverylane.Observe`, and L4's new effect-free ACK paths and
identity adoption are `DeliveryWork` decisions inside the handlers that never touch admission. L4 must not
introduce a latch spelling; task 2.7's grep is the check.

## 7. Invariants, each with its spec home

| # | Invariant (holds for every input/sequence) | Spec home |
|---|---|---|
| I1 | After the first result with `OwnerStopRequired()` on a lane, `Admit()` is false forever on that lane. | `jetstream-consumer-policy:602-603` |
| I2 | Exactly one result reaches the observer per lane: the first fatal; later fatals change neither the buffer nor health. | `:602` ("buffer its first"); L1 `agentic-loop` delta `:77` ("neither overwrites nor recounts") |
| I3 | `onFatal` completes in the callback before the result is buffered, so health reads the fatal before the handle can drain. | L1 `agentic-loop` delta `:72-76`; `agentic-model` delta `:10-12`; `agentic-dispatch` delta `:22-23`; `agentic-governance` delta `:21-22` |
| I4 | `handle.Drain()` is called at most once per binding across `Observe` and Stop, in any order and concurrency. | `:604-605`; L1 `agentic-loop` delta `:69` |
| I5 | A closed lane performs no work, heartbeat, or terminal method, and reads no payload; the only metadata it reads is the subject, and only when a declarer exists. | `:603`, strengthened by the `**AND**` clause this change adds to the requirement prose |
| I6 | The observer exits on the owner's context cancellation or after the fatal drain; `Done()` is never nil and closes either way, and is already closed when no observer ran, so Stop can always join it without blocking. | `:605`; `component-lifecycle:11-12` |
| I7 | A panic in settlement-only work yields Quarantine with a cause `"<owner> delivery work panicked: …"`, never a nil error and never an unwound callback. | L1 `agentic-loop` delta `:60-62`; `agentic-governance` delta (panic scenario) |
| I8 | A terminal-method error alone never closes admission. | `:589-590`; `:571-582` |
| I9 | The refusal declarer, when present, is invoked once per refused delivery with that delivery's subject; when absent, nothing on the delivery is read. | `:607-609` |

I5 and I9 no longer contradict (MEDIUM-4): the subject read is carved out of I5 and conditioned in I9. These nine are
the only admissible source for any property or fuzz harness the developer writes.

## 8. Test plan and mutation checks

Existing per-component tests keep proving admission semantics through the production wiring: the **36 test
functions across 16 files** enumerated mechanically in `inventory.md` § 4 (4 files `//go:build integration`) stay,
edited only where they reached a private field or constructed a binding by literal. The package's own test proves
I1–I9 with a fake handle and a fake message. Every string assertion stays byte-identical: `"delivery ownership
lost"` (×8), `"terminal delivery ownership lost"`, `"governance delivery work panicked"`, `"Terminal delivery
refused by latched lane"`, `"Tool delivery refused by latched lane"`.

Mutation checks are named by the CALL deleted, never the primitive; each is run by `cp` backup + checksum with
`[applied]` printed before the test run and the restore verified:

| # | Mutation (delete the call) | Turns red |
|---|---|---|
| M1 | `admission.Latch(result)` inside `Consume` | tools `TestDeliveryLaneBuffersFatalBeforeHandleAndRefusesLaterDelivery` (`Admit()` stays true; health never latches); loop/model `…QuarantinesAndStopsExactOwner` (`drains` never reaches 1) |
| M2 | `a.onFatal(result)` inside `Latch` | dispatch `TestTerminalLaneFatalHealthFailsClosedIndependently`, `TestDeliveryFatalHealthKeepsFirstCauseAcrossLanes`; tools `:95`; model `:132` |
| M3 | `binding.Drain()` inside `Observe`'s fatal arm | every `require.Eventually(drains == 1)`: dispatch `:143`, tools `:107`, loop `:157`, model `:84`, governance `:46`, `:141` |
| M4 | `admission.refuse(msg)` inside `Consume` | dispatch `TestRefusedTerminalDeliveryIsLoggedAndCounted`; tools `:116-121` |
| M5 | replace `b.drainOnce.Do(b.handle.Drain)` with `b.handle.Drain()` | tools `:123` and dispatch `:146` ("fatal and ordinary stop share drain-once": `drains` becomes 2) |
| M6 | the `deliverylane.Observe(...)` CALL at one component site (model `L3:425`) | model `TestModelSetupWiresMetadataFailureToAcquiredOwner` (the handle never drains) — the wiring mutation |
| M7 | the `recover()` block in `runWork` | governance `delivery_settlement_test.go:138`; loop `TestLoopApprovalPanicProductionCallbackQuarantinesExactOwner` |
| M8 | the `admission.Admit()` guard inside `Settle` | **Accepted as a pre-existing hole**: today's inline `if !admission.admit() { return }` at governance `:511-513` is equally invisible — `TestGovernanceProductionCallbackPanicLatchesFirstFatalAndDrainsExactOwner` replays into a *different* port. Detectors after this change: the package test `TestSettleRefusesWithoutInvokingWork` (primitive) and a new ~10-line component test (task 2.8) that replays a second delivery into the **same** latched settlement lane and asserts `acks+naks+terms == 0` and the work counter unchanged (production seam). |

Mutation criterion (`docs/contributing/01-testing.md:96-97`): "enforcement of a consequential invariant whose
violation could … leave owned work running after shutdown" applies to I1, I4, I6.

## 9. Sequencing and the rebase base

Order: L3 (#1329, PR #1338) lands → this change → #1249 and #1330 implementation. Measured at the rebased L3 head
`c58c65bd` (not assumed): `20fe8d09` IS its ancestor, the five `delivery_owner.go` files are byte-identical there,
and dispatch's lane COUNT drops 5 → 3 (`agent.created`, `agent.approval_pending` deleted by `2693df0e`; static lane
constructions 10 → 8). The re-pin at the merged commit is therefore a re-measure for dispatch, not a line shift.
The developer's base is **the squash-merge commit of PR #1338 on `main`** — take it from `git log --oneline -1
origin/main` after that merge, confirm `git ls-tree --name-only origin/main -- processor/agentic-*/delivery_owner.go`
lists all five and `git grep -c 'newDeliveryLaneAdmission(' origin/main -- processor/agentic-dispatch/component.go`
is 3, then regenerate every pin in `inventory.md` at that commit (`gen_inventory.py` with `BASE` updated;
`scripts/inventory-verify.sh` EXIT 0) before the first code commit. Branch `claude/gh1341-delivery-lane-package` in
its own worktree; draft PR with `Closes #1341` before work.

### 9a. Re-pin executed (round 3, implementation) — base `3faca84f`

The base is `3faca84f`, the squash-merge of PR #1338. Neither `20fe8d09` nor `c58c65bd` is an ancestor of it, so
every pin in `inventory.md` was re-derived POSITIONALLY (old blob vs new blob, difflib opcodes, then `sed -n
"${n}p"`), never by text search: 15 pins were textually AMBIGUOUS at the new base, one of them a bare `}` with 245
hits. `scripts/inventory-verify.sh` → `pins=272 ok=272 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`, EXIT=0.

Every premise this design rests on holds, re-measured not assumed: five `delivery_owner.go` files, byte-identical to
`20fe8d09` and untouched by L3 (`git diff --stat fbd3c173 3faca84f -- 'processor/*/delivery_owner.go'` empty), still
550 lines; 8 lane constructions (dispatch 3, governance 1, loop 2, model 1, tools 1); 3 refusal declarers. Two
measurements moved:

- **Dispatch's two deleted lanes** (`agent.created`, `agent.approval_pending`) are gone, exactly as § 9 predicted —
  eight inventory pins dropped rather than re-pinned. The § 5 rewrite table's `L3:` dispatch lines shift by +8
  (`:569`→`:577`, `:610`→`:618`, `:651`→`:659`, `:584-585`→`:592-593`, `:625-626`→`:633-634`, `:666-667`→`:674-675`,
  `:497-498`→`:505-506`, `:505`→`:513`); the shapes are unchanged, so the table is not re-transcribed.
- **The test census grew** from 36 tests / 16 files / 4 integration-tagged to **45 / 23 / 5**. L2 and L3 added seven
  files that touch these symbols, six under `agentic-loop`; the fifth integration-tagged file,
  `processor/agentic-loop/terminal_failure_record_integration_test.go`, was not named in `tasks.md`'s gate line and
  now is. Task 2.6 grows with it; tasks 1.x and the dispatch conversion do not.

### 9b. Implementation order and the round-2 findings applied in code

Order (coordinator, 2026-09-21): package, then **dispatch first** — the widest copy (refuse arm + settlement lane +
the only constructor wider than `(onFatal)`) — then a review pass on package + dispatch before tools, loop, model and
governance follow. `tasks.md` keeps its numbering; 2.4 runs before 2.1.

MEDIUM-A (`design-review-2.md` § 5) is applied to the § 5 text as it lands: the package doc comment says "its
consumers are the five agentic components, and `agentic/agentrun` once #1249 adopts it" rather than asserting
agentrun as a present consumer. Q2 is RULED amend, so agentrun becomes a consumer, but not until #1249 lands; the
doc comment states the tense correctly. Nothing else in § 5 changes.

## 10. Residuals recorded, not filed

- R1 → **filed as #1342** (2026-09-19, blocked by #1341): five lanes at `c58c65bd` refuse silently against spec
  `:607-609` (three declare: dispatch `agent.complete`, `agent.failed`; tools `tool.execute`). Not a #1341 behavior
  change; after this change the fix is one `onRefused` argument per lane plus a counter per component.
- R2 Heartbeat lanes get no panic wrapper because `ConsumeDeliveryWithHeartbeat` synthesizes Quarantine on panic
  (D6); `runWork` is settlement-only by construction. Model's missing wrapper (L1 residual `:139-141`) therefore
  stays a non-issue on its heartbeat lane.
- R3 `Binding` becomes a pointer; the slice-copy-shared `*sync.Once` idiom disappears. The five literal
  constructions in `lifecycle_causal_test.go` move to `NewBinding` (task 2.6).
- R4 The eleven `drainIssued` bindings on other planes (`inventory.md` § 2) keep their bool: one Stop-side drainer,
  no observer, so drain-once solves nothing there; they become adopters only with typed settlement plus a concurrent
  drainer, which no change sequences.

## 11. Skills applied

- `kv-or-stream`: no new communication path (the package adds no subject, bucket, or stream) — not triggered.
- `orchestration-check`: no multi-step behavior; the observer is the same single-trigger goroutine each component
  runs today — not triggered.
- `new-payload`, `query-pattern`: no payload, no query — not triggered.

## 12. Adopter seam and ADR-106

The internal home exposes no surface outside this module; the adopter seam inventory (`inventory.md` § Adopter seam)
is answered for the typed API as it stands, and its finding — six facts an adopter must hold, learned only from a
doc — is the recorded argument for the export gate in § 3, not for changing this change. `scripts/api-compat.sh`:
nothing to report (no exported symbol added or removed in a Tier 1 package; the five deleted types were unexported).

## 13. Decisions recorded and the one open question

Recorded (owner, round 2, 2026-09-19):

- **Q1 Home:** `internal/deliverylane`. Export is a future gate triggered by semdev's migration off
  `ConsumeWithHeartbeat`; recorded in § 3, not taken.
- **Q3 R1:** filed as **#1342**, blocked by #1341; § 10.
- **Q4 Spec wording:** accepted with MEDIUM-5 applied — no import path inside a SHALL; the delta names "one shared
  package within this module, which owns no lifecycle authority".

Open, on #1341 as a docket:

- **Q2 #1249 draft-4 § 2.6 amendment.** Confirm the 2026-09-19 #1341 ruling supersedes draft-4 § 2.6 so #1249
  consumes `Admission`/`Binding`/`Observe` and drops its drained flags (§ 6; RULED amend, owner 2026-09-19). Without it the sixth
  copy lands.

## 14. Round-2 changes, by finding

HIGH-1 dispatch lane count at `L3:` (§ 2 row 4, § 4 −9, § 5 rewrite table, § 9) · HIGH-2 per-component gate adds
`go vet -tags=integration` (`tasks.md` § 2) · MEDIUM-1 `runWork` unexported · MEDIUM-2 `react` required, nil branch
dropped · MEDIUM-3 `Done()` never nil · MEDIUM-4 I5/I9 reconciled · MEDIUM-5 path out of the SHALL (spec delta) ·
MEDIUM-6 "eighteen tests" → 36/16 · MEDIUM-7 silent-lane count → 5 at `L3:` (7 at base), R1 → #1342 · NIT-1
"strictly narrower" claimed (§ 2 row 2) · NIT-2 `Settle`/`Consume` nil asymmetry documented · NIT-3 guard and SHALL
narrowed to the latch (`tasks.md` 3.1, spec delta) · Q1/Q3/Q4 recorded (§ 13).

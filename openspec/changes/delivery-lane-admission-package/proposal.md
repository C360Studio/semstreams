# Change: one home for the delivery-lane admission latch

> Change id `delivery-lane-admission-package`; closes #1341. Every `file:line` is at `20fe8d09` (the pushed L1 head)
> unless prefixed `L3:` (= `c58c65bd`, the rebased L3 head); the developer re-derives every pin at the merged L3
> commit on `main` before the first code commit (`design.md` § 9). Owner ruling 2026-09-19 on #1341: "agree - if we
> have a legit abstraction lets add it after L3 so L4 does not need to do it all over." Round 2: `INVENTORY PASS`
> recorded; design-review findings applied (`design.md` § 14). Design-phase claim; owner acceptance pending.

## Why

The owner-side reaction the typed settlement contract asks for — a per-lane admission latch that closes on the first
`OwnerStopRequired` result, a drain-once binding around the exact `jetstream.ConsumeContext`, and an observer that
drains that handle when the latch closes — is spelled five times: `processor/agentic-{dispatch,tools,loop,model,
governance}/delivery_owner.go` (136 + 122 + 116 + 101 + 75 = 550 lines, byte-identical at `L3:`), plus governance's
drain and panic wrapper in `component.go:728` and `:349` and five `streamConsumerBinding` struct declarations (594
lines all in). The copies have already diverged in three ways the design enumerates (refuse arm; fatal recorder
placement; panic wrapper presence) and none of the divergence is a difference in behavior a component wanted.

The #1249 design (`gh1249/design-draft-4.md` § 2.6) copies loop `:29-63` and `:74-86` verbatim as a sixth spelling into
`agentic/agentrun`; L4 (#1330) edits the loop copy's neighbourhood. L1 recorded the hoist as a residual at a bound of
four copies (`openspec/changes/settle-after-durable-effect/design.md:142-146`). At five going on six the bound no
longer holds, and the owner has ruled the direction.

## What changes

1. New internal package `internal/deliverylane` (home decided, `design.md` § 3): an `Admission` latch with
   `Admit`/`Latch` and an optional refusal declarer; `Consume` (heartbeat lane) and `Settle` (settlement-only lane,
   panic-guarded) as the two entry points, mirroring `natsclient`'s two settlement operations; a `Binding` with
   `Drain` (once), `Closed`, and a never-nil `Done`; and `Observe`, which runs the owner's required reaction and
   drains the exact handle on the first buffered fatal. 228 lines with doc comments, 139 code.
2. The five `delivery_owner.go` files are deleted. Governance's out-of-file `drain` and `runGovernanceDeliveryWork`,
   and the five `streamConsumerBinding` structs, are deleted with them. The two fatal recorders that lived in the
   loop and model copies move to their `component.go` beside the other three.
3. Every lane call site — at `L3:` dispatch ×3 (one settlement-only, two terminal heartbeat), tools ×1, loop ×2
   shapes, model ×1, governance ×1 — constructs the package's admission and binding and passes its own reaction; no
   behavior changes: per-lane latch, synchronous health fatal, exact-handle drain-once, the refuse arm on the three
   lanes that declare refusals today, the fatal recorders, and governance's narrower shape all survive as arguments
   or one-line compositions. No mode flag.
4. Spec: `jetstream-consumer-policy` MODIFIES the THEN clause "no shared helper owns admission, a native handle,
   health, shutdown, or restart" (current `:651`, and the two restatements L1 lands) to name one shared package
   within this module and what it does NOT own (lifecycle authority: Stop, restart, reconstruction, registry), and
   adds one scenario to "control loss shuts down through the existing exact owner". No import path appears in a
   SHALL. No other capability's behavior changes.
5. Downstream: #1249 consumes `deliverylane` instead of copying (its § 2.6 and the `milestoneConsumerOwner` drained
   flags are re-cut — pending owner ruling on #1341 Q2; `design.md` § 6); L4 keeps calling the loop's
   `setupConsumer` lanes, which now call the package, and corrects one line of its Survives list.

## Impact

- Packages: new `internal/deliverylane`; edits in the five agentic processors (call sites, Stop paths, `consumers`
  field type, the relocated recorders, 36 tests in 16 files). `natsclient` untouched.
- ADR-106 Tier 1: **not touched.** The package is internal (outside both tiers); the deleted types were unexported.
  `scripts/api-compat.sh` reports nothing for this change. Exporting later (`pkg/deliverylane`) is a separate,
  compatible Tier 1 widening with its own walked path (RC-6), gated on semdev's migration off `ConsumeWithHeartbeat`.
- Sisters: no surface reached from outside changes. semdev's two legacy `ConsumeWithHeartbeat` sites are unaffected
  by this change (`inventory.md` § Adopter seam).
- E2E: not a BREAKING change; `task check:push` is the gate. `task e2e:agentic` is the relevant tier if the
  reviewer wants a walked lane, not a requirement.
- Size: replaces 550 in-file (594 all-in) lines with 228 + ≈28 lines of call-site residue = 256 (`design.md` § 4).
- Related: #1342 (five silent-refusal lanes at `L3:`, blocked by this change).

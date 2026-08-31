# Change: StopAll's terminal transition and the already-stopped answer are stated, not inferred from code

Closes #1214. Spec-only for the capability; the one code edit retires a `SPIKE FINDING` marker that this delta
makes obsolete. No production behavior changes.

## Why

Two facts about coordinated shutdown are true in code and absent from `openspec/specs/service-shutdown/spec.md`.
Both were found by the rapid state machine landed in PR #1213 (`48b127ce`), which read the spec literally and
disagreed with the implementation.

**1. The registry clear is a terminal transition nobody wrote down.** At `service/service_manager.go:925-932`:

```go
if len(errors) > 0 {
    return fmt.Errorf("stop errors: %w", stderrors.Join(errors...))   // returns BEFORE the clear
}
m.mu.Lock()
m.services = make(map[string]Service)
m.order = nil
m.mu.Unlock()
```

A clean pass deregisters everything, so a second `StopAll` visits nothing. A failed pass returns before the clear,
retaining every registration for retry. The spike hit this on its second generated sequence
(`register → stopAll → stopAll`): its model expected a re-visit and got an empty list.

The asymmetry is intentional, and the placement is the evidence — an accidental clear would be unconditional, at
the end of the function regardless of outcome. Instead it sits deliberately after the error check, so failure keeps
the authority it needs to retry. The same shape is already stated one requirement over, at
`service-shutdown/spec.md:114`, *"ComponentManager failed Start retains cleanup authority"*. This change writes the
StopAll half down. **Owner ruling, 2026-08-31: intentional — spec delta, not a code fix.**

**2. The spec is narrower than the contract it describes.** `spec.md:65` requires a repeated `Stop` to return nil.
Three places in code admit nil *or* `ErrAlreadyStopped`:

- `service/base.go:22-28` — "A Stop that observes completed teardown MAY return nil (the `BaseService.Stop`
  default) or this sentinel; both are success."
- `service/base.go:483-487`, the `Service` interface doc — "returns success (nil or `ErrAlreadyStopped`)"
- `service/service_manager.go:888` — StopAll explicitly tolerates the sentinel when aggregating

The code is right and the spec is behind: if a repeated Stop always returned nil, `ErrAlreadyStopped` would have no
reason to exist and the tolerance at line 888 would be dead code. **Owner ruling, 2026-08-31: widen the spec.**

## What Changes

**ADDED — one requirement.** *Terminal StopAll success deregisters every service; failure retains them for retry.*
Given its own heading rather than folded into the existing requirement, because it is a distinct fact (registry
lifecycle) from the one that requirement states (already-stopped tolerance), and because it mirrors the existing
failed-Start requirement it parallels.

**MODIFIED — `Coordinated shutdown treats an already-stopped service as clean success`.** The
*"Completed service is visited again"* scenario silently assumed the service is still registered. It describes a
service that self-stopped *within* a pass (the gh#520/gh#549 shape), not the post-clean-pass state — where the
service is not visited at all, because it no longer exists in the registry. That unstated assumption is precisely
what misled the spike's model. The GIVEN is narrowed to say which case it covers; the header is unchanged, and its
THEN is widened in step with change 2.

**MODIFIED — `A framework service Stop is idempotent on repeated invocation`.** The requirement text and the
*"Completed Stop is called again"* scenario widen from nil to success (nil or `ErrAlreadyStopped`). The
*"Stop called twice returns nil the second time"* scenario keeps its header and narrows its GIVEN to the
`BaseService.Stop` default, which is the case that genuinely returns nil — leaving it general would contradict the
widened requirement. One scenario is added pinning StopAll's tolerance of the sentinel, which is currently
implemented at `service_manager.go:888` and asserted nowhere in the spec.

**Code:** the `SPIKE FINDING` comment at `service/service_manager_prop_test.go:242-247` is replaced by a `// spec:`
citation of the new requirement. The assertion it guards is unchanged — the model already mirrors the ruled
behavior, so no test logic moves.

## What does NOT change

No production code. The delta states behavior that already ships; every scenario added or widened is satisfied by
`main` today. This is deliberately not the moment to change shutdown semantics.

## Adopter seam

An adopter implementing `Service` learns one thing they could previously only discover by reading our source: after
a clean `StopAll` their service is deregistered, so a second `StopAll` will not visit it — and after a failed one it
will. They need do nothing; both behaviors already ship. The seam this closes is that the framework was asking them
to predict a registry state it owns and never published.

## Scenario-header discipline

Every `MODIFIED` block restates the requirement's full current scenario set, and no scenario header is renamed —
bodies are narrowed under their original headers. openspec 1.7.0 refuses an archive that drops or renames one, and
`--skip-specs` is not an option here.

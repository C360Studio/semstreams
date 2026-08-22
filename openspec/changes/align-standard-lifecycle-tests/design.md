# Design: Align StandardLifecycleTests with component lifecycle authority

Status: owner accepted on 2026-08-22 after independent `DESIGN REVIEW PASS`.

The accepted inventory is `docs/proposals/gh1022-standard-lifecycle-tests-inventory.md`, SHA-256
`8a9b788c07396710d3540e0330e4bbe93b5b8a74c402cbde43c6e8f50747fe7d` (`INVENTORY PASS`). The accepted design is
`docs/proposals/gh1022-standard-lifecycle-tests-design.md`, SHA-256
`b1906e949e9d731ded36f936630264f6c3fa360fae2ae9775a398f52506d6a37`. The owner accepted reviewed rulings R1-R8
with the controlled-Stop and UDP completion-observation corrections.

## Decision

Apply accepted Option C: correct the portable shared contract and the one measured production rejoin conflict. Add no
replacement lifecycle framework or outward-facing surface.

### R1 — Portable floor, unchanged factory

`LifecycleFactory` remains `func() LifecycleComponent`. `StandardLifecycleTests` is a portable minimum, not proof of
resource-specific drain ordering, blocked joins, or partial-acquisition rollback.

### R2 — Corrected shared assertions

The shared suite retains:

- fresh Initialize;
- a controlled Initialize→Start→Stop whose Start authority remains live while a separate finite Stop context bounds
  the owner sequence;
- a distinct fresh-instance case in which an accepted Start parent is canceled before a separate finite Stop observes
  completion;
- nil Start/Stop rejection;
- pre-canceled/pre-expired Start rejection followed by safe Stop;
- safe Stop before Start;
- completed repeated Stop returning nil; and
- deterministic parallel fresh-instance/leak smoke, if retained.

Keeping Start authority live during controlled Stop preserves owner-specific drain-before-cancel protocols. The suite
removes repeated-Start result, Start-without-Initialize, post-Stop Initialize, concurrent Initialize, concurrent Stop
result equality/replay, and canceled/expired Stop followed by later-Stop assertions.

### R3 — Failed Start remains owner-specific

The shared failed-Start floor is safe Stop after Start rejects before acquisition. Components with fallible acquisition
retain owner-local deterministic proof for exact-handle rollback and retained failed-Start cleanup authority. No
exported test hook or fault harness is added.

### R4 — Owner-specific Stop order

Start's accepted context owns continuing work. Stop uses its exact caller context only to bound the concrete owner's
terminal admission-fence, cancellation, join, and cleanup sequence; it does not prescribe cancellation before every
protocol fence. Nil is rejected before action and completed repeated Stop is nil/no-op. Concurrent execution, result
replay, later running-generation rejoin, reinitialization, and restart are not portable promises.

### R5 — UDP owner correction

UDP replaces retained later-Stop rejoin state with:

- a private synchronized cancel function;
- a private Start-owned completion observation channel; and
- the existing WaitGroup as owner/test proof.

Start derives runtime cancellation from the exact caller context and publishes cancellation authority and the
completion channel before launching the read goroutine. It retains no context and invents no root.

The Start-owned read goroutine owns a synchronous exit finalizer. Only when it actually exits does it publish
`running=false`, clear `conn=nil`, close terminal buffer/resource state, complete the WaitGroup, and then close its
completion channel. No generic finalizer framework is introduced.

The first valid Stop consumes cancellation authority once, snapshots the corresponding completion channel, fences
admission by closing the exact socket, cancels runtime work, and selects completion against the exact caller context.
Stop launches no waiter or detached cleanup goroutine. If the bound wins, Stop returns the caller error honestly.
Because cancellation authority is already consumed, later Stop ignores the completion channel and returns immediate
nil/no-op; it does not observe or rejoin that running generation.

If a blocked read goroutine is later released, its own exit finalizer completes state/resource cleanup and closes the
channel as natural Start-owner completion. While completion remains unobserved, health derives from observed resource
state and cannot report healthy solely because `conn` is a nonnil closed pointer.

Deterministic owner tests use channels and the exact completion/WaitGroup observations, not sleeps. They prove the
first bounded Stop error, immediate later-Stop no-op before release, post-release `running=false`, `conn=nil`,
nonhealthy health, finalized resources, and exactly one teardown.

### R6 — Existing aligned adopters remain production-stable

Gateway/http and graph-index receive no production changes. Their existing shared-suite call sites pass the corrected
floor. Graph-index owner-specific failed-Start and no-rejoin tests remain authoritative. Output/websocket helper and
benchmark compatibility is preserved.

### R7 — Dedicated current capability

Add only the `component-lifecycle` capability delta. `service-shutdown` explicitly excludes component lifecycle; the
workflow `lifecycle` capability and `component-runtime-config` own different concepts.

### R8 — Adjacent boundaries

This change makes no service-manager/process proof, restartable-instance promise, resolution of #867/#1012/#1013,
or tag-readiness claim. It adds no config, durable primitive, payload, query surface, agent, LLM, persona, role,
prompt, model call, ops agent, or scenario.

## Adopter seam

The external `LifecycleComponent` author gets no signature or factory change. The portable facts are small: construct
a fresh instance at the portable reuse boundary, pass Start context into owned work, and call Stop with a nonnil
caller-bounded context; a completed repeat is harmless. Exact native drain order and partial-Start rollback stay with
the owner that observes the resources. Callers do not predict generations, completion timing, or cleanup resources.

## File scope

Expected production changes:

- `component/lifecycle.go`
- `input/udp/udp.go`

Expected test changes:

- `component/lifecycle_test_suite.go`
- `input/udp/udp_lifecycle_test.go`

Verification-only, with no expected source change:

- `gateway/http/http_lifecycle_test.go`
- `processor/graph-index/lifecycle_integration_test.go`
- `processor/graph-index/lifecycle_order_test.go`
- `processor/graph-index/failed_start_subscription_test.go`
- `output/websocket/websocket_test.go`

Explicitly out of scope are `service/*`, ADR edits, `docs/basics/05-first-processor.md`, sister-repository edits,
E2E/config/schema/wire changes, and adjacent issues.

## Implementation conformance

`conformance.md` maps binding rulings R1-R8 to final production, test, and capability `file:line` evidence. It also
records the initial RED, the two implementation-review corrections and their GREEN resolution, local gate evidence,
and the explicit pending status of hosted CI, final review, and integration.

## Verification plan

Focused:

- `go test -race ./component ./gateway/http ./input/udp ./output/websocket -count=1`
- graph-index real-NATS lifecycle integration through the repository integration task/filter
- repeated deterministic UDP completion-channel no-rejoin and post-release owner-finalization test under
  `-race -count=10`

Repository gates:

- `task lint`
- `go test -race ./...`
- `task test:integration`
- `go build ./...`
- `task schema:generate` plus zero `schemas/`/`specs/` drift
- `go test ./test/contract/...`
- `openspec validate align-standard-lifecycle-tests --strict --no-interactive`
- hosted CI on the exact candidate

No E2E tier is required by the measured boundary because there is no wire, config, or persisted-state behavior and no
BREAKING commit claim. Expansion into process shutdown, #867, transport Close, or another assembled path requires a
separate design/evidence ruling.

# Service listener design draft (#1120)

Status: proposed; independent design review and owner acceptance required before implementation.

## Accepted inventory checkpoint

The accepted inventory is incorporated verbatim by reference:
`openspec/changes/service-test-listener-ownership/inventory.md`
Baseline: `50ff16eb8bc89bac520713531c8c70df1676cf2d`
SHA256: `f52f793d9fe3531530a7bf5b04bebec9eff64ec5def2edd80bf1d5349b14b97f`

Independent reviewer `nats_flake_review` returned INVENTORY PASS, verified all 79 pins, and independently
confirmed 18+3+6 calls with gopls. That resolves the inventory's recorded typed-census uncertainty.
The accepted inventory bytes remain unchanged; this clarification is separate review evidence.

## Problem and measured premises

Tests release a port reservation before the real owner binds, so another allocation can invalidate their prediction.
The repair must preserve real HTTP/metrics serving, startup ordering, owner cleanup, and refusal assertions.

| Premise | Measurement in accepted inventory |
|---|---|
| 27 calls exercise the same bounded owners | 18 freePort, 3 freeMetricsPort, 6 freeServerPort calls |
| Shared HTTP already supports test ephemeral acquisition | service_manager_test.go:610 assigns config directly; service_manager.go:1176 binds it |
| Public HTTP 0 still means 8080 | ConfigureFromServices at service_manager.go:153,182 |
| Health 0 disables | service_manager.go:1287 |
| Metrics 0 means 9090 at both constructors | service/metrics.go:78; metric/handler.go:42 |
| Manager startup metrics uses concrete Server | service_manager.go:81,620; metrics.go:315 |
| Metrics provider field is not a constructor injection point | metrics.go:45,142 |
| Pprof is early/asynchronous/best-effort | pprof.go:27–35; ADR-058 rollout 4 |
| Same-shape retained-listener precedent exists | gateway lifecycle test:81,99 and component.go:727 |
| Unrelated transport helpers exist | output/websocket, input/udp, internal/maxdelivery pins in inventory |

## Options considered

| Option | Benefit | Cost or limitation |
|---|---|---|
| Do nothing | No production changes | Retains the demonstrated reservation gap and startup-wait hangs |
| Use existing/private seams only | No new public contract; shared HTTP already fits | Health/pprof can be extracted privately, but service tests cannot access metric.Server's private acquisition through its concrete package boundary |
| Extend the existing metric owner with listener-taking Start; private service seams | Preserves concrete TLS/Serve/Stop paths and all 27 tests; narrowly defined ownership transfer | Adds one exported method requiring explicit listener ownership semantics and owner approval; changes existing `Address()` to observe the owned listener while active. Before acquisition and after terminal cleanup, its configured-address behavior remains unchanged. |
| Export configurable listener factories or new test constructors | Flexible injection across packages | Larger adopter surface, callback/factory policy, and testing vocabulary without a demonstrated product consumer |
| Change public zero-port meanings | Easy ephemeral binding | Changes existing disabled/default semantics and is outside the accepted repair |

Recommendation: extend the existing metric owner narrowly, using private extraction seams elsewhere.
Do not add a shared runtime listener registry, allocator, factory interface, or coordination package.
This adopts existing bind-once/observe-address behavior, rather than establishing a new reusable pattern.

## Recommended boundaries

### Shared Manager HTTP

Use the existing test constructor with HTTPPort 0.
After the existing bound signal or a gated child-Start signal, read the retained listener under Manager's mutex.
Build test URLs from its actual TCP port with explicit loopback host, avoiding wildcard/proxy behavior.
Keep ConfigureFromServices and its 8080 default unchanged.
Do not inject an already-running HTTP runtime into startup tests: they must still exercise StartAll acquisition.

### Health

Extract the current implementation into a private helper accepting the acquisition function:
`startHealthListener(ctx context.Context, port int, listen func(string, string) (net.Listener, error)) error`.
The public `StartHealthListener` delegates with `net.Listen`; checks, binding, serving, and publication remain common.
Tests supply an acquisition function that binds loopback 0 and returns that same retained listener.
The positive integer supplied by tests selects the existing enabled branch; it is not advertised as an actual port.
Tests obtain the actual endpoint from the acquired listener, not that branch-selection integer.
Preserve nil/canceled context validation, zero no-op, one-shot guard, synchronous bind errors, and StopAll ownership.
Refusal tests use an acquisition spy that fails if invoked; they need no port allocation.
No exported health signature changes.

### Concrete metrics owner

Propose the single new export:
`func (s *Server) StartWithListener(ctx context.Context, listener net.Listener) error`.

Present consumers are concrete metric tests and service tests exercising standalone and Manager-owned metrics.
`Start` and `StartWithListener` share one private setup/serve implementation.
`Start` continues to bind its configured address synchronously after existing validation and TLS preparation.
`StartWithListener` serves the supplied raw TCP listener without acquiring another socket.
Server applies configured TLS wrapping exactly once on both paths; callers must not supply a TLS-wrapped listener.

Listener ownership and lifecycle validation:

1. Nil/ended context, nil listener, and an already-used instance reject before transfer.
2. Preserve existing `Start` ordering: context validation precedes locking and the used guard; `used` becomes true
   before registry and TLS validation.
3. `StartWithListener` additionally rejects a nil listener or an unusable TCP endpoint before changing `used`.
   The accepted listener must expose an address from which a host and assigned port can be obtained.
4. Registry or TLS-preparation errors consume the instance's one-shot opportunity, matching existing `Start`,
   but do not transfer or close the supplied listener.
5. Every returned error leaves the supplied listener the caller's responsibility. All synchronous fallible
   preparation precedes transfer.
6. Successful return transfers listener-release responsibility to Server. The caller must not close it during serving.
7. Existing Stop performs graceful/forced closure and exact Serve completion; terminal-repeat behavior remains unchanged.

Update existing `Address()` rather than adding a getter:

- Read lifecycle state under `s.mu`, protecting the listener snapshot against Start/Stop.
- While `s.listener` is retained, report its actual assigned host and port, including during Stop before terminal
  cleanup clears ownership.
- For an unspecified bind host, use `localhost`, preserving a dialable URL and the existing wildcard-bind convention.
  Preserve explicit loopback or other concrete bind hosts; format IPv6 with `net.JoinHostPort`.
- Before successful acquisition and after terminal cleanup clears the listener, report the configured
  `localhost:s.port` endpoint, as today.
- A rejected initial startup with no retained listener reports the configured fallback. A rejected second Start
  preserves the already-owned listener and its observed address.
- Preserve `/path` and configured `http` versus `https` scheme in every state.
- `Address()` reports the owned endpoint, not readiness or successful acceptance of a connection.

Thus `NewServer(9090, ...)` followed by successful ephemeral `StartWithListener` reports the ephemeral endpoint
through `Address()`. Tests must consume that accessor rather than hide a mismatch by constructing their own metrics URL.

### Service metrics bridge

Add one private optional test listener field on Metrics, set only before lifecycle start by same-package tests.
Add one private `startServer(ctx, server)` method: no supplied test listener delegates to `server.Start(ctx)`;
a supplied test listener delegates to `server.StartWithListener(ctx, listener)`.
Both standalone Metrics.Start and Manager's startup-metrics Start call that same method on the owning Metrics.
Keep claimManagerServer's concrete Server construction, exclusive claim, service order, and Manager cleanup unchanged.
This is a private per-instance seam; no process-global hook, retained context, or production configuration is added.
The test fixture owns an untransferred listener, including early failures before server Start.
After successful transfer, the framework owner performs normal Stop; fixture cleanup is only a final safety close.
The bridge must not silently replace the Server with a fake or start metrics before Manager's production bind phase.

### Pprof

Keep exported `MaybeStartPProf(debug, port)` unchanged.
Extract a private helper receiving an `*http.Server` and acquisition function and returning Serve completion.
The public wrapper provides a server with nil Handler and `net.Listen`, discarding the private completion handle.
Enabled acquisition and serving still occur inside the asynchronous goroutine, before NATS composition.
Preserve start/error stdout reporting, default-mux semantics, disabled gate, and best-effort failure.
Tests supply a real server and loopback 0 acquisition, observe the assigned listener, and close/join in cleanup.
Disabled/invalid tests assert acquisition is never invoked; do not probe an unowned address for absence.
No managed-service conversion, context API, public stop handle, or composition-root changes.

## Test coordination and release

Replace all 27 helper calls; delete the three helpers when their reference counts reach zero.
Refusal/no-op cases use acquisition spies or values that cannot reach acquisition, without reserving ports.
Success cases retain one listener from acquisition until production cleanup.
Intentional occupied-port failures retain their existing occupied listeners and test native synchronous refusal.
Fresh-instance listener-taking tests use fresh ephemeral listeners. Retain a separate positive native `Start(ctx)`
test: construct normally, set the package-private `server.port = 0` in the same-package test before Start, then
exercise native acquisition, a real request through `Address()`, and production Stop. This test-only assignment
does not change NewServer's public zero-default behavior and performs no probe-close-rebind.

When startup tests wait for child entry or shared binding, also select on the StartAll result.
An early startup error must fail with that error immediately, not leave the test waiting on an unreachable gate.
Keep a distinct completion observation for cleanup, so consuming an early error cannot make cleanup wait twice.
Register cleanup before starting goroutines: release test gates, observe Start completion, then bounded Stop.
Do not race StopAll against a still-blocked Start or cancel its runtime parent before controlled cleanup.
Tests must release blocked handlers before cleanup joins; arbitrary sleeps are not synchronization.
Use bounded waits only as failure ceilings, reporting the outstanding owner/signal.

## Invariants and specification homes

Existing requirements remain:
framework-composition “Component starts form a fail-closed boot barrier” governs early diagnostics,
readiness gating, service order, and post-child diagnostic release.
service-shutdown governs caller-context Stop, aggregation, and completed-repeat behavior.
runtime-context-ownership governs context validation, private cancellation, and controlled cleanup.
ADR-058 preserves the pprof phase and best-effort posture.

The following proposed requirement text is a draft only, for framework-composition after owner acceptance:

“Metric Server SHALL admit a caller-bound raw TCP listener through StartWithListener without acquiring another socket.
Successful return SHALL transfer listener-release responsibility to Server's existing lifecycle.
Every returned error SHALL leave the supplied listener under caller responsibility.
Nil/ended context, nil or unusable listener, and used-instance rejection SHALL precede transfer.
Registry and TLS-preparation failures SHALL preserve the existing consumed-instance semantics without taking
listener ownership.
Configured TLS, request context ancestry, one-shot admission, and terminal Stop behavior SHALL match Start.
While Server retains its accepted listener, Address SHALL report that listener's actual assigned endpoint,
using localhost for an unspecified bind host and preserving configured TLS scheme and path.
Before successful acquisition and after terminal cleanup releases ownership, Address SHALL report its configured
localhost endpoint.
Address SHALL NOT imply readiness.
Existing port-zero configuration meanings SHALL remain unchanged.”

Draft scenarios: native Start acquisition and serving; successful raw/TLS listener transfer; configured-port versus
accepted-port mismatch; Address before Start, while owned, and after Stop; nil/ended context and used-instance
rejection; registry/TLS-preparation rejection leaves listener caller-owned while consuming the one-shot instance;
Stop releases the accepted listener and joins serving.
Do not write a runtime/spec delta before independent design pass and owner acceptance.

## TDD and sensitivity evidence

First add deterministic checks around the actual owner paths, with explicit acquisition and completion signals.
For unavailable symbols, a compile failure only establishes scaffold red; it does not establish behavioral sensitivity.
After compiling the seam, preserve behavioral red and targeted mutation evidence for the ownership assertions.

Required observations:
1. Both acquisition paths receive positive production-path proof. Native Start binds once after a same-package
   private-port-zero setup; StartWithListener retains the supplied socket without a second bind. Both serve real HTTP
   through `Address()` and complete production Stop. For StartWithListener, deliberately construct with 9090 and supply
   an ephemeral listener: Address must name the actual assigned endpoint. Repeat the address/request proof with
   configured TLS and verify the HTTPS scheme and single TLS wrapping.
2. Shared/metrics diagnostics answer while child Start is blocked; full routes remain gated.
3. Standalone Metrics and Manager-owned metrics both traverse the real concrete Server path.
4. Context, used-instance, disabled, nil/unusable-listener, registry, and TLS-preparation refusals have no forbidden
   acquisition or transfer. Assert both caller listener ownership and the specified `used` state. Assert Address's
   configured fallback before Start, after rejected initial startup with no owned listener, and after completed Stop;
   a rejected second Start preserves the actual endpoint while ownership is retained.
5. Failed BaseService commitment releases the accepted provider through rollback.
6. Completed Stop observes original-listener closure and exact Serve completion, including bounded forced shutdown.
7. Pprof positive cleanup closes and joins; disabled/invalid paths never acquire.
8. Startup bind failure is reported through startDone before the child-entry wait can hang.

Run a deterministic synthetic mutant that closes/rebinds the accepted listener: the original-listener observation
must fail, even if a replacement socket happens to serve successfully.
Run a shutdown mutant that omits release of the accepted listener: closure/completion evidence must fail.
Cover the private service bridge at both production call sites; bypassing it must produce a bounded explicit failure.
Use unchanged assertions, compiling/reached mutants, cp backups and checksum restoration; retain exact evidence.
A high-count green stress run is supplementary and cannot substitute for these observations.

Add a targeted Address mutant that returns the configured port while an accepted listener is retained. The mismatch
check must fail through the existing Address accessor; test URL construction must remain unchanged. Native Start
proof must remain in the selected test set after migrating the listener-taking tests, so retaining only the alternate
entry point cannot satisfy verification.

PBT decision: named deterministic histories cover the bounded operation orders relevant here:
reject-before-start, successful transfer/Stop/repeat, failed-Start rollback, blocked Start/Stop, and forced Stop.
No new grammar or codec is introduced; random scheduling cannot guarantee these ownership boundaries are exercised.
Mutation criteria apply because existing checks admitted this regression and listener leaks are a plausible fault.

## Proposed artifact and delivery changes

After owner acceptance, update proposal/tasks to the 27-call scope and chosen seam; add the draft spec requirement.
Implementation files: service_manager.go, metrics.go, pprof.go, metric/handler.go and their enumerated owner tests.
Preserve the reviewed inventory checkpoint and append design/verification evidence separately.
No ADR is proposed: this is existing-owner mechanics, with one additive API contract, not a new coordination model.
No schema/default or transport-owner migration is proposed; adjacent UDP/websocket/maxdelivery work remains separate.
Use semstreams-preflight to select gates; targeted race tests first, required check:push before push, reviewer before integration.
Any discovered API/default break returns to design review and requires the relevant breaking-change E2E evidence.

Declared public-surface changes are one new method, `metric.Server.StartWithListener`, and revised active-owner
behavior of existing `metric.Server.Address`; there is no new address getter, configuration field, or constructor.
Record the before-acquisition/after-cleanup fallback and actual-owned-endpoint behavior in the method documentation
and proposed spec.

Applied semstreams-dev production-seam/test-fidelity discipline and orchestration-check: acquisition stays inside
existing runtime owners; no rule, workflow, or orchestration layer is introduced. Other decision skills do not trigger.

Open owner decision: accept the additive metric.Server.StartWithListener contract and the narrowly private seams,
or select a different option. No implementation is authorized by this draft itself.

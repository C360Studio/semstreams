# Bounded native KV listing diagnostic — execution record

Historical first implementation record. The reviewer-corrected source and exact-source proof are in
`diagnostic-review-correction.md`; the source checksum and run names below apply only to this earlier revision.

Source revision before diagnostic file: `b2fea55402a95c3a014d48ec59392a37f2843744`.
Final diagnostic source SHA-256: `da21c07c5b404d62f3a00a3899604fbe1c4e4d1f7926e49e2153eb3bc44484c9`.
Selected dependencies: `github.com/nats-io/nats.go v1.52.0` and the graph-index owner-load image reference
`nats:2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66`.
No production, SDK, runner, or graph-index source changed in this diagnostic slice.

## Final exact-source runs

Each invocation used the canonical host-locked integration runner and the same command:

```bash
SEMSTREAMS_KV_LIFECYCLE_DIAGNOSTIC=1 ./scripts/run-integration-tests.sh \
  ./natsclient -run '^TestIntegration_KVFilterLifecycleDiagnostic$' -timeout=180s -v
```

- Baseline: exit 0; three cases pass. `diagnostic-final-baseline.txt` SHA-256:
  `a52418a78f36a898cb29e6485c572f192c03e76c31a1fbb2b7eba33b4bef85de`.
- Stop-delegation mutant: exit 1 as expected. `diagnostic-final-mutant-run.txt` SHA-256:
  `c1f371c5fc09ade93839c288486a614cb6e274914c4c27a461c6b006745f9b0a`.
  It compiled; control passed; cancel reached `delegated Stop invocation count=0 want=1` at ~29 ms.
  Deadline was skipped after failure; there was no timeout.
- Restored baseline: exit 0; three cases pass in 8.89 s. `diagnostic-final-restored.txt` SHA-256:
  `4e475460636e08e379d6febad5a93a188ba20940b6ac43b47521f0b0b79aa24c`.

The one-line mutation is retained in `diagnostic-final-mutant.diff` (SHA-256
`59603b237675bbf4125373223a52a5c4029c7e90a3dc392f7557c8dbbf2cc743`):
only `kvDiagnosticFacade.Stop` skipped calling the unchanged private invocation observer.
The test-owned facade forwarding goroutine joined after the mutant assertion. A `cp` backup was installed before
mutation and restored on shell exit. The backup and final source have the identical SHA-256 above. The mutation
demonstrates sensitivity to **delegated Stop invocation**, not native producer completion.

The earlier `diagnostic-run.txt`, `diagnostic-baseline-final.txt`, `diagnostic-stop-mutant-run.txt`,
`diagnostic-stop-mutant.diff`, and `diagnostic-restored.txt` are historical diagnostic/instrumentation runs.
Their source predates the final child-output ownership correction and they are not final-source proof.

## Observed native schedule

One parent-owned NATS container held 1,024 distinct keys matching `diag.>`; each isolated child used a fresh
production Client/KVStore and exactly one listing. The control returned and compared the exact 1,024-key set.
The injected cases withheld delivery after the first key. A second `Keys` invocation witnessed that production
collection consumed that key. Full, nontruncated goroutine snapshots then showed two distinct native goroutines
blocked at the pinned SDK's `jetstream/kv.go:1451` forwarding send and `jetstream/kv.go:1290` watcher callback send.
The complete matched goroutine blocks and IDs are in both final passing logs. This is controlled backpressure,
not a reproduction of the unchanged owner-load harness or the historical runner conditions.

In the restored run, cancellation returned `context.Canceled` with nil keys at 29 ms; default production timeout
returned `context.DeadlineExceeded` with nil keys at 5.011 s. The decorator observed one delegated native Stop
invocation per case. Native Stop returned `nats: invalid subscription` in the control and injected cases; the
existing framework collector ignores that error. The diagnostic records it without inferring why the SDK returned it.

Client.Close began while withholding remained active. It returned nil with native `DRAINING_SUBS` and `CLOSED`
statuses observed: cancel at 41.8 ms and deadline at 5.025 s from child start. The facade was released only after
Close. Its owned forwarding goroutine joined after native Keys closed. The post-release snapshot found the
previously witnessed watcher ID present in the restored cancellation case and absent in the restored deadline case;
the earlier final-source baseline found both watcher IDs present at its immediate post-release snapshots. These
immediate snapshots do not
prove a persistent leak or a native watcher join. Child process exit contains native work that cannot be joined
through public KeyLister.

This schedule establishes that blocked SDK producers can coexist with prompt framework cancellation/deadline
return and prompt Client.Close. It does **not** reproduce #1421's 15-second drain timeout. The historical failed
listing phase, runner pressure, and subscription identity remain unmeasured.

## Bounds and evidence limits

The package-private `newTestClient` receives a 20-second child of `t.Context()` and
`productionTestClientFactoryDeps` to own the real container during setup and seeding. Its returned TestClient has
one immediate `t.Cleanup` registration; Terminate retains separate 15-second client-close and
container-termination contexts. The parent
admits a child only with 30 seconds of work/cleanup allowance before its cooperative 140-second cutoff; the
test alarm is 180 seconds. Each child gets 20 seconds of work and at most 10 seconds of cleanup. One goroutine
owns Cmd.Wait; the event writer streams observations, and Wait completion joins stdout copying and marks EOF.
Exit without the `done` event fails immediately. The parent kills an over-budget child and joins the Wait owner.
No `time.Sleep` establishes ordering; the bounded stack poll only observes the two native blocked frames.

PBT decision: the three named schedules and exact seeded-set oracle cover the accepted finite lifecycle question;
random histories would not add a relevant input class. The diagnostic remains opt-in and skipped in ordinary CI.
No production repair, timeout relaxation, retry, or historical causal attribution is supported by this record.

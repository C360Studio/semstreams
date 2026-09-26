# Inventory supplement: retry timing and existing virtual-time tests

base: a0c11028a6c3e946f4041f73e5581c981a3ff707

Scope: narrow evidence for the five-second review finding. Inventory only; no revised design or implementation selected. No tests, Docker, or writes performed.

## Existing timing obligation and retry mechanism

- `docs/contributing/01-testing.md:433` — `| Unit top-level test under `-race` | 1s | 5s |`
- `processor/graph-index/component.go:1388` — `const indexWriteMaxAttempts = 3`
- `processor/graph-index/component.go:1156` — `for attempt := 0; attempt < indexWriteMaxAttempts; attempt++ {`
- `processor/graph-index/component.go:1167` — `case <-time.After(time.Duration(attempt+1) * 25 * time.Millisecond):`

A persistent failure incurs two production timer waits: 25ms plus 50ms. One such history in each of Rapid’s normal 100 checks therefore entails at least 7.5 seconds of timer waiting outside virtual time, before other work. The HIGH finding is supported.

`gopls references processor/graph-index/component.go:1154:21` returned one caller, `component.go:1568`, where production entity updates invoke this retry method.

## Existing repository pattern

- `service/metrics_forwarder_test.go:14` — `"testing/synctest"`
- `service/metrics_forwarder_test.go:235` — `synctest.Test(t, func(t *testing.T) {`
- `service/metrics_forwarder_test.go:261` — `time.Sleep(350 * time.Millisecond)`
- `service/metrics_forwarder_test.go:262` — `synctest.Wait()`
- `processor/rule/lifecycle_runtime_test.go:9` — `"testing/synctest"`
- `processor/rule/lifecycle_runtime_test.go:26` — `synctest.Test(t, func(t *testing.T) {`
- `processor/rule/lifecycle_runtime_test.go:56` — `stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)`
- `processor/rule/lifecycle_runtime_test.go:93` — `synctest.Test(t, func(t *testing.T) {`
- `processor/rule/lifecycle_runtime_test.go:120` — `synctest.Wait()`
- `go.mod:3` — `go 1.26.3`
- `go.mod:25` — `pgregory.net/rapid v1.3.0`

The existing shape is an isolated standard-library virtual-time test around unchanged production timers and lifecycle code. It is already used on unrelated planes; no new runtime clock primitive is needed to obtain virtual timer behavior.

No same-class durable, communication, or runtime-coordination primitive is proposed. No adopter seam changes are introduced by this supplement.

## Standard-library and Rapid compatibility evidence

These are external local-source references, not repository inventory-verifier pins.

Local toolchain: `go version go1.26.4 darwin/arm64`.

`/usr/local/go/src/testing/synctest/synctest.go`:

External line 20: `// Within a bubble, the [time] package uses a fake clock.`
External lines 24–25: time advances when every bubble goroutine is durably blocked.
External lines 276–277: `Test` waits for bubble goroutines to exit; deadlock fails the test.
External line 279: `Test` must not be called from within a bubble.
External lines 283–286: cleanup and `T.Context()` belong to the bubble.
External line 287: `//   - T.Run, T.Parallel, and T.Deadline must not be called.`

SHA-256: `5abfa83a4e45dd62bcd0d71692ecc4578e2a259879e707e56a970a3303142421`.

`/Users/coby/go/pkg/mod/pgregory.net/rapid@v1.3.0/engine.go`:

External line 80: default `checks: 100`.
External lines 180–185: `checkDeadline` recognizes concrete `*testing.T` and calls `t.Deadline()`.
External line 207: `Check` calls `checkTB(t, checkDeadline(t), prop)`.
External lines 279–281: Rapid measures execution using `time.Now`/`time.Since`.
External line 285: successful output reports checks and duration, not the automatic base seed.
External lines 451–459: Rapid catches property failure panics around the property callback.
External lines 593–595: generators should run in one goroutine; `Draw` on a given Rapid test is not concurrency-safe.

SHA-256: `2dbfcbad418e81ce80858976e29e51fa98c47afef3dbff6da741a8acedd6fe21`.

**Compatibility finding:** directly placing `rapid.Check(bubbleT, …)` inside `synctest.Test` is unsupported: Rapid calls the bubble test’s forbidden `Deadline` method. Merely wrapping the whole property is not a sound correction.

The virtual timer mechanism fits the inspected production wait. Its composition with Rapid must also preserve real-time measurement, Rapid’s failure capture/shrinking, bubble isolation, and single-goroutine draws. Those are concrete review constraints for the revised design, not grounds to alter production retry timing.

## Seed and measurement finding

- `docs/contributing/01-testing.md:450` — `using randomness MUST print the seed.`

The earlier instruction to record an automatically selected seed from ordinary successful Rapid output was unsupported. Explicit recorded nonzero seeds such as 1292 and 1293 avoid that evidence gap. This supplement does not claim either has been executed.

Virtual-time source inspection removes the timer-delay premise, but does not establish CPU/runtime performance. Normal 100-check execution under `-race`, measured outside virtual time, remains necessary to demonstrate the five-second ceiling. Reduced check counts, short mode, larger timeout, or canceled contexts would not discharge the finding.

## Exact search/read ledger

| Command | Result |
|---|---|
| `git rev-parse HEAD` | `a0c11028a6c3e946f4041f73e5581c981a3ff707` |
| `git grep -n -E 'testing/synctest\|synctest\.(Test\|Run\|Wait)' -- '*.go' ':!processor/graph-index/predicate_layout_smoke_integration_test.go'` | 7 matching lines across 2 files. |
| `git grep -n -E '5s\|5.s\|five\|synctest' -- docs/contributing/01-testing.md docs/contributing/09-property-testing.md .agents/contracts/semstreams-developer.md` | 7 matching lines; includes the budget and unrelated textual matches. |
| `gopls references processor/graph-index/component.go:1154:21` | 1 caller. |
| `go doc testing/synctest` | Complete package documentation returned. |
| `go doc testing/synctest.Test` | Complete function documentation returned. |
| `go env GOROOT` | `/usr/local/go` |
| `go env GOMODCACHE` | `/Users/coby/go/pkg/mod` |
| `go version` | `go1.26.4 darwin/arm64` |
| `rg -n 'Deadline\(\|\.Run\(\|\.Parallel\(\|time\.(Now\|Since\|Until\|After)\|func Check\|func checkTB\|func checkOnce\|type TB' /Users/coby/go/pkg/mod/pgregory.net/rapid@v1.3.0/engine.go` | 20 matching lines; deadline and timing paths subsequently read. |
| `rg -n 'seed\|checks\|Deadline\|recover\|panic' /Users/coby/go/pkg/mod/pgregory.net/rapid@v1.3.0/engine.go \| head -35` | 35 preview lines; not an exhaustive-match count. |
| `rg -n 'time\.Now\|time\.After\|go func\|WaitGroup' natsclient/kv_store.go natsclient/kv_store_core.go` | Failed: both guessed paths absent. No KV-wrapper conclusion drawn. |
| `shasum -a 256 /usr/local/go/src/testing/synctest/synctest.go /Users/coby/go/pkg/mod/pgregory.net/rapid@v1.3.0/engine.go` | Two hashes recorded above. |
| `git status --short` | Existing untracked `docs/proposals/graph-index-reference-model/design.md`; no architect mutation. |

Markdown `\|` escapes table syntax; executed regex alternations used ordinary `|`.

Bounded source reads used `nl -ba <path> | sed -n '<range>p'`:

Read: `service/metrics_forwarder_test.go`: 220–286.
Read: `processor/rule/lifecycle_runtime_test.go`: 1–155; file ended at 143.
Read: `processor/graph-index/component.go`: 1150–1172 and 1380–1392.
Read: `docs/contributing/01-testing.md`: 417–451.
Read: `go.mod`: 1–16 and 24–41.
Read: Standard-library `synctest.go`: 12–47, 248–280, 280–296.
Read: Rapid `engine.go`: 74–88, 176–209, 271–296, 398–472, 550–625.

**NOT RUN:** tests, timing experiments, mutation experiments, Docker/integration/E2E, protected smoke inspection, shared-runner operations, or any code changes.

**NOT ESTABLISHED:** measured normal-100-check runtime, a working Rapid/synctest composition, or mutation replay under that composition. Independent supplement review precedes revised design.

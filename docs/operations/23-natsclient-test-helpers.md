# natsclient Test-Client Helper Patterns

This page shows how to use the SemStreams NATS test substrate. The normative rules for test placement, container
justification, isolation, readiness, cleanup, budgets, parallelism, and exceptions live in the
[Testing Policy](../contributing/01-testing.md). If an example here conflicts with that policy, the policy wins.

## Decide Before Starting Docker

Use `natsclient.NewTestClient` only when the assertion depends on real NATS or JetStream behavior, such as KV history
or watches, stream delivery, durable consumers, ObjectStore, persistence, or the production request/reply wire.

Keep validation, mapping, state-machine, retry-policy, and time-calculation tests in process. A component talking to
NATS in production does not by itself justify a container in every test.

Every Docker-backed file MUST carry the integration constraint and naming convention:

```go
//go:build integration

package mypkg
```

Run the full suite through the repository task:

```bash
task test:integration
```

For a focused package, pass the selector to the canonical runner:

```bash
scripts/run-integration-tests.sh ./path/to/package/...
```

Do not use direct `go test -tags=integration` commands. Both full and focused runs need the runner's host lock, image
preflight, Ryuk policy, `-race`, `-failfast`, timeout, and fresh-count flags.

## Canonical Runner Behavior

The runner acquires the fixed host lock `/tmp/semstreams-integration.lock` before any Docker operation. Contention
fails immediately by default. Set `SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS` to an integer from 1 through 3600 only
when the caller deliberately accepts that bounded wait; lock diagnostics identify the current owner.

Once it holds the lock, the runner verifies Docker and inspects the pinned `nats:2.14-alpine` image. It uses a cached
copy without pulling and pulls only if the exact tag is missing. Ordinary test execution does not refresh an existing
image. Set `SEMSTREAMS_INTEGRATION_REFRESH_IMAGE=1` for a deliberate refresh. The canonical runner performs that pull
under the host lock and bounds it to five minutes.

The runner sets `TESTCONTAINERS_RYUK_DISABLED=false`, while explicit helper cleanup remains primary. It invokes Go with
`-race -failfast -tags=integration -timeout=20m -count=1` and does not override Go's package parallelism. Because the
`integration` tag is additive, the default `./...` selection runs the unit and integration-tagged tests together once;
CI does not run a duplicate untagged unit lane first.

Go's `-timeout=20m` applies independently to each package; it does not bound the local aggregate suite. Local callers
may interrupt an aggregate run. CI's 25-minute outer timeout supplies the whole-job and process-tree bound, including
Docker preflight and cleanup. The per-package timeout is transitional, not a per-test budget; the normative measured
budgets remain in the testing policy.

## The Canonical Substrate

`natsclient.NewTestClient(t, opts...)` starts the repository-pinned NATS image through testcontainers, connects a
production `*natsclient.Client`, and registers test cleanup. Construct one client with the options the test needs;
options compose on that single instance:

```go
tc := natsclient.NewTestClient(t,
    natsclient.WithKVBuckets("ENTITY_STATES", "AGENT_LOOPS"),
    natsclient.WithStreams(natsclient.TestStreamConfig{
        Name:     "GRAPH_EVENTS",
        Subjects: []string{"graph.>"},
    }),
)
```

Use no options when the test needs only core NATS, or only `WithJetStream` when it needs JetStream without pre-created
resources. `WithKV`, `WithKVBuckets`, and `WithStreams` enable JetStream as needed. `WithFileStorage` is for tests whose
volume would exceed memory-backed limits; it requires a resource-budget explanation. The default container starts and
exposes only 4222/tcp, and `TestClient.MonitoringURL` is empty. A test that scrapes `/varz` or another NATS monitoring
endpoint must add `WithMonitoring()`; that option starts `--http_port 8222`, exposes and requires 8222/tcp, and returns
the usable mapped URL.

Callers MUST NOT import testcontainers, start or replace a container, or resolve Docker host ports themselves. The
canonical substrate owns those operations so readiness and cleanup fixes apply everywhere. It normally starts once.
Its only sequential replacement follows a 10-second required-port observation budget in which at least one successful
Docker Inspect snapshot showed a configured required port absent, the parent context remains live, and cleanup of the
failed attempt succeeds. The configured set is 4222/tcp by default and adds 8222/tcp only with `WithMonitoring()`;
absence of an unconfigured port is ignored. If every Inspect fails, it does not replace the attempt. It starts at most
one replacement, never has two live containers, and never retries any other setup or cleanup failure.

## Basic Integration Shape

Derive I/O contexts from the test and keep the deadline narrower than the test budget:

```go
func TestIntegration_ComponentPublishes(t *testing.T) {
    ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
    t.Cleanup(cancel)

    tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
    component := NewComponent(tc.Client)

    require.NoError(t, component.Start(ctx))
    t.Cleanup(func() {
        require.NoError(t, component.Stop(5*time.Second))
    })

    require.NoError(t, component.Publish(ctx, payload))
    require.EventuallyWithT(t, func(collect *assert.CollectT) {
        got, err := readPublishedState(ctx, tc.Client)
        assert.NoError(collect, err)
        if err == nil {
            assert.Equal(collect, want, got)
        }
    }, 5*time.Second, 50*time.Millisecond)
}
```

Prefer a component-ready channel, subscription confirmation, or completion callback over polling. Bounded observation
is acceptable only when the external service exposes no causal signal. It MUST report the last value or error when it
times out.

## Request/Reply With Classified Errors

Test the same classified wire used by production callers:

```go
func TestIntegration_QueryRejectsMissingID(t *testing.T) {
    ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
    t.Cleanup(cancel)

    tc := natsclient.NewTestClient(t)
    _, err := tc.Client.SubscribeForRequests(ctx, "mypkg.query.entity",
        func(_ context.Context, data []byte) ([]byte, error) {
            var request QueryRequest
            if err := json.Unmarshal(data, &request); err != nil {
                return natsclient.ReplyError(errs.Invalid("invalid request"))
            }
            if request.EntityID == "" {
                return natsclient.ReplyError(errs.Invalid("entity_id is required"))
            }
            return json.Marshal(QueryResponse{Entity: loadEntity(request.EntityID)})
        })
    require.NoError(t, err)

    _, err = component.QueryEntity(ctx, "")
    require.True(t, errs.IsInvalid(err))
}
```

Subscribe before publishing or requesting unless the test specifically proves retry behavior. A retry regression MUST
observe a real first-attempt signal or use an injected retry hook or clock. It MUST NOT sleep in the hope that an
attempt occurred.

Use `RequestClassified` and `ClassifyReply` for new mutation and error-path tests. See
[NATS Request and Retry](07-nats-request-retry.md).

## KV, Stream, and ObjectStore Isolation

A fresh per-test container permits fixed production resource names without cross-test pollution. This is the default
when the component owns names such as `ENTITY_STATES` or `GRAPH_EVENTS`.

When the production API accepts names, derive all related resources from one per-test namespace:

```go
namespace := testNamespace(t) // Package-local, tested sanitizer plus a run-unique suffix.
tc := natsclient.NewTestClient(t,
    natsclient.WithKV(),
    natsclient.WithBucketPrefix(namespace),
)
subject := namespace + "events.entity"
consumer := namespace + "entity-reader"
```

The namespace MUST cover streams, subjects, consumers, ObjectStores, and object keys as well as KV buckets. Do not use
a prefix when doing so would stop the test from exercising fixed production names; use a fresh container instead.

Create ObjectStore through the production client with a test-owned name when configuration permits:

```go
store, err := tc.Client.CreateObjectStore(ctx, natsclient.ObjectStoreConfig{
    Bucket:      namespace + "ARTIFACTS",
    Description: "integration artifacts",
})
require.NoError(t, err)
```

## Readiness Order

The canonical substrate MUST establish readiness in this order:

1. observe the NATS process's internal ready signal;
2. resolve the configured required mappings from one Docker Inspect snapshot per poll under one cancellable 10-second
   budget;
3. connect the NATS client under its own deadline; and
4. wait for the component or subscription signal required by the assertion.

Do not copy a `ForListeningPort`, `ForHTTP`, `Host`, or `MappedPort` sequence into a test. Host-port inspection during
container startup adds Docker API pressure and predicts a fact the daemon owns. A mapped-port retry that can exceed its
documented budget because one inspect call blocks is not truly bounded; the operation itself must receive the bounded
context.

If the helper violates this order, fix the substrate rather than adding a sleep or a second readiness strategy at the
call site.

## Cleanup Ownership

`NewTestClient(t)` owns the NATS client and container through `t.Cleanup`. Do not add `defer tc.Terminate()`; double
ownership makes cleanup order and failures ambiguous.

Register resources that depend on NATS after creating the test client so cleanup runs in safe LIFO order:

```go
tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

component := NewComponent(tc.Client)
require.NoError(t, component.Start(ctx))
t.Cleanup(func() {
    require.NoError(t, component.Stop(5*time.Second))
})
```

Explicit cleanup is load-bearing even when the testcontainers Reaper is enabled. Reaper configuration MUST match
between Task and CI. Any temporary disablement requires the exception evidence defined by the testing policy and a
post-run check for leaked containers.

`NewTestClient` derives setup work from `t.Context()` and applies narrower deadlines for container startup, mapped
ports, connection, and initial resource creation. Cleanup intentionally does not derive from `t.Context()`: Go cancels
that context before `t.Cleanup` callbacks run. Client close and container termination therefore each use a fresh,
bounded `context.Background()` child so one blocked phase cannot consume the other's measured 10-second cleanup
budget. This exception belongs to the canonical helper only; ordinary test I/O still derives from `t.Context()`.

## Shared Containers Are an Exception

`NewSharedTestClient` exists for rare package-level owners that can prove complete isolation. It is not the default for
a large suite, and fewer startups alone are not sufficient justification.

Before approval, document:

- every mutable resource class used by the package;
- unique naming or deterministic reset for each class;
- how leaked subscriptions, consumers, goroutines, and files are detected;
- why fresh containers are materially worse for this package; and
- measured behavior under the intended parallelism.

Do not pass a fabricated `&testing.T{}` to `NewTestClient`. An approved `TestMain` uses `NewSharedTestClient`, preserves
the test exit code, and terminates the shared owner on every reachable path.

## Failure Evidence

A helper setup failure identifies its attempt, phase, original cause, parent-context state, container ID when one was
returned, and any cleanup cause. A required-port observation failure additionally identifies total observation
attempts, shared mapping budget, elapsed time, the last successful observation's attempt and missing ports when one
exists, and the last Inspect error and its attempt when one exists. Ports outside the configured required set do not
participate in this decision. A polling timeout must include the last observed state, not only
`condition was never satisfied`.

When diagnosing paid or long-running E2E work, also follow the active-polling rules in `AGENTS.md`: inspect
authoritative state every 30–60 seconds and abort once a wedge is proven.

## Anti-Patterns

- Starting testcontainers outside the canonical `natsclient` helper.
- Running Docker-backed behavior from an untagged unit file.
- Having more than one live NATS container in a top-level test without approval.
- Starting a replacement outside the canonical helper's exact mapped-port recovery policy.
- Sharing a container because it appears faster without proving state isolation.
- Waiting with `time.Sleep`, including retry and cleanup tests.
- Using `context.Background()` for test I/O that can block.
- Hardcoding or predicting host ports.
- Adding both helper cleanup and direct `Terminate` calls.
- Raising a timeout without evidence explaining the regression.

## Enforcement Status

CI now runs AST guards for untagged container starts, direct container/readiness APIs, fabricated `testing.T` values,
and integration sleeps. Fabricated `testing.T`, untagged container starts, and direct container APIs are zero-debt
categories and cannot be baselined. The only baseline entries are 305 exact legacy integration sleeps; that baseline
is manually maintained and shrink-only. Positive and negative fixtures protect every category.

Naming, exception expiry, duration, container counts, complete resource naming, and general context usage remain
review-enforced. A helper-specific structural test, if present, covers only the canonical helper's context contract.
See the policy's
[Enforcement Status and Backlog](../contributing/01-testing.md#enforcement-status-and-backlog).

## Cross-References

- [Testing Policy](../contributing/01-testing.md) — canonical rules and budgets
- [End-to-End Testing](../contributing/02-e2e-tests.md) — deployed-stack evidence
- [NATS Request and Retry](07-nats-request-retry.md) — classified request/reply behavior
- `natsclient/test_client.go` — helper implementation and options

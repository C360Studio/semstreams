# natsclient Test-Client Helpers — Gateway Integration Test Patterns

When you're writing an integration test for a component that talks to
NATS — request/reply, JetStream stream, KV bucket, ObjectStore — reach
for `natsclient.NewTestClient(t, opts...)` and the patterns below
rather than rolling local fakes. Gateways (semconnect, future CS API
hosts) and sister-repo components have re-derived the same setup
shapes enough times that gh#173 made it an explicit ask; this page is
the canonical answer.

## The substrate

`natsclient.NewTestClient(t, opts...)` spins up a NATS 2.12-alpine
container via testcontainers, returns a `*TestClient` with a connected
`*natsclient.Client`, and registers `t.Cleanup` to tear the container
down. Options compose:

```go
import "github.com/c360studio/semstreams/natsclient"

// Plain core NATS
tc := natsclient.NewTestClient(t)

// JetStream
tc := natsclient.NewTestClient(t, natsclient.WithJetStream())

// JetStream + KV
tc := natsclient.NewTestClient(t, natsclient.WithKV())

// JetStream + KV + pre-created buckets
tc := natsclient.NewTestClient(t,
    natsclient.WithKVBuckets("ENTITY_STATES", "AGENT_LOOPS"),
)

// JetStream + KV + pre-created streams
tc := natsclient.NewTestClient(t,
    natsclient.WithStreams(natsclient.TestStreamConfig{
        Name:     "GRAPH_INGEST",
        Subjects: []string{"graph.mutation.>"},
    }),
)

// Test isolation: per-test bucket prefix
tc := natsclient.NewTestClient(t,
    natsclient.WithKV(),
    natsclient.WithBucketPrefix(t.Name()+"_"),
)

// Run with: go test -tags=integration -race ./your-pkg/...
```

`tc.Client` is the production `*natsclient.Client` — pass it to your
component constructor exactly as production main.go would. Build-tag
your test file `//go:build integration` so the unit-test layer stays
Docker-free.

## Pattern: request/reply with classified error headers

Post-#93, the canonical request shape is `RequestClassified` /
`ClassifyReply`. Test handlers should mimic the production shape so
your component's caller-side classification logic actually exercises.

```go
//go:build integration

package mypkg

import (
    "context"
    "encoding/json"
    "testing"

    "github.com/c360studio/semstreams/natsclient"
    "github.com/c360studio/semstreams/pkg/errs"
    "github.com/stretchr/testify/require"
)

func TestComponent_QueryHandler(t *testing.T) {
    tc := natsclient.NewTestClient(t)
    ctx := context.Background()

    // Stub responder. ReplyError stamps the classified header; the
    // production caller side reads it via ClassifyReply.
    _, err := tc.Client.SubscribeForRequests(ctx, "mypkg.query.entity",
        func(_ context.Context, data []byte) ([]byte, error) {
            var req QueryRequest
            require.NoError(t, json.Unmarshal(data, &req))
            if req.EntityID == "" {
                return natsclient.ReplyError(errs.Invalid("entity_id is required"))
            }
            return json.Marshal(QueryResponse{Entity: ...})
        })
    require.NoError(t, err)

    // Drive your component through its public API. Internally it
    // calls natsclient.RequestClassified + ClassifyReply on the same
    // subject; classification routes back through pkg/errs sentinels.
    result, err := component.QueryEntity(ctx, "")
    require.True(t, errs.IsInvalid(err))
}
```

For new code, prefer `RequestClassified` over the legacy bare-`Request`
path. The dual-encoding window from gh#93 Phase 1+2+3 stays open until
Phase 4 (gh#161) drops the compat shim — write new tests to the
post-Phase-4 shape so they don't need to change.

## Pattern: JetStream stream + KV bucket setup

Components that own a stream-and-watch pair need both pre-created
before the component starts. `WithStreams` + `WithKVBuckets` compose
cleanly:

```go
func TestComponent_StreamConsumer(t *testing.T) {
    tc := natsclient.NewTestClient(t,
        natsclient.WithKVBuckets("ENTITY_STATES"),
        natsclient.WithStreams(natsclient.TestStreamConfig{
            Name:     "EVENTS",
            Subjects: []string{"events.>"},
        }),
    )
    ctx := context.Background()

    comp := mypkg.NewComponent(tc.Client)
    require.NoError(t, comp.Start(ctx))
    t.Cleanup(func() { comp.Stop(0) })

    // Publish to the stream; the component's JetStream consumer
    // picks it up and writes to ENTITY_STATES.
    payload := buildBaseMessage(t, ...)
    require.NoError(t, tc.Client.PublishToStream(ctx, "events.entity", payload))

    // Assert on KV state via the test client's GetKVBucket helper —
    // it applies the bucket prefix automatically if you set one.
    bucket, err := tc.Client.GetKeyValueBucket(ctx, "ENTITY_STATES")
    require.NoError(t, err)
    require.Eventually(t, func() bool {
        entry, err := bucket.Get(ctx, "some.entity.id")
        return err == nil && entry != nil
    }, 5*time.Second, 50*time.Millisecond, "entity should land in ENTITY_STATES")
}
```

`require.Eventually` with explicit timeout + poll is the load-bearing
pattern. Don't use `time.Sleep` to wait for async wires — flake risk.

## Pattern: ObjectStore for StorageRef-backed payloads

ObjectStore tests need JetStream enabled and the bucket created via
the same client. There's no `WithObjectStore` option today — call
`CreateObjectStore` on the client directly:

```go
func TestComponent_StorageRef(t *testing.T) {
    tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
    ctx := context.Background()

    // Production-shape: object-store bucket name comes from config.
    osCfg := natsclient.ObjectStoreConfig{
        Bucket:      "csapi-artifacts",
        Description: "CS API artifact entities (gh#171)",
    }
    os, err := tc.Client.CreateObjectStore(ctx, osCfg)
    require.NoError(t, err)

    // Store a payload; the storage reference is what flows on triples.
    ref, err := storage.PutContent(ctx, os, "swe/schemas/temp-celsius-v1.json",
        []byte(`{"type":"DataRecord", ...}`),
        storage.ContentType("application/swe+json"))
    require.NoError(t, err)

    // Drive your component; it reads the storage ref and fetches.
    result, err := comp.ReadArtifact(ctx, ref)
    require.NoError(t, err)
    require.Equal(t, "DataRecord", result.Type)
}
```

## Pattern: lifecycle-safe startup (subscriber-ready-before-publish)

The gh#170 cold-start race illustrates the discipline: if you publish
before the subscriber is ready, NATS returns "no responders." There
are two correct patterns depending on what you control.

**A. You control the subscriber.** Block until `SubscribeForRequests`
returns, THEN publish:

```go
_, err := tc.Client.SubscribeForRequests(ctx, "subject", handler)
require.NoError(t, err)  // subscription is live before this returns

// Now safe to publish/request on this subject.
resp, err := tc.Client.Request(ctx, "subject", payload, 5*time.Second)
```

**B. You're testing retry resilience (gh#170 style).** Fire the
publish first, sync on a "started" channel inside the goroutine,
small sleep so the first attempt definitely fails, THEN subscribe:

```go
createCh := make(chan error, 1)
createStarted := make(chan struct{})
go func() {
    close(createStarted)
    createCh <- mgr.Create(ctx, entity)  // emits via RequestWithRetry
}()
<-createStarted
time.Sleep(50 * time.Millisecond)  // let the first emit attempt fail

// Now subscribe the responder. Retry budget converges on the next attempt.
_, err := tc.Client.SubscribeForRequests(ctx, subject, handler)
require.NoError(t, err)

require.NoError(t, <-createCh)
```

Pattern B is the regression-test shape for gh#170 — see
`pkg/lifecycle/manager_integration_test.go` for the canonical example.

## Cleanup: it's automatic, but know the boundaries

`NewTestClient(t)` registers `t.Cleanup` to terminate the container
and close the client. You don't need defer/Cleanup yourself unless
you're managing additional resources (component lifecycle, file
handles).

For long-running test suites (testify `suite.Suite`), use the same
container across tests via `t.Helper`-aware setup, but reset state
between tests by purging KV buckets:

```go
func (s *MySuite) SetupTest() {
    ctx := context.Background()
    bucket, err := s.tc.Client.GetKeyValueBucket(ctx, "ENTITY_STATES")
    s.Require().NoError(err)
    s.Require().NoError(bucket.PurgeDeletes(ctx))
}
```

PR #169's commit `38af4393` reaps KV buckets between tests to keep
consumer churn from killing the shared-container connection past
minute one. Mirror that pattern if your suite races against
container resource limits.

## Anti-patterns

- **Hand-rolled `nats.Connect` in tests.** Skip the production retry/auth/TLS plumbing and you drift from production behavior. Use `NewTestClient` always.
- **`time.Sleep` instead of `require.Eventually`** when waiting on async wire state. Sleeps are flake bait under host load; explicit polling has an explicit timeout that fails loud.
- **Bare `Request` for mutation tests.** Use `RequestClassified` so the production error-classification path is exercised. See [07-nats-request-retry.md](07-nats-request-retry.md) for the mutation-vs-query rule.
- **Subscribing AFTER publishing in non-retry tests.** Hidden race — passes locally, flakes under load. Subscribe first unless you're explicitly testing retry behavior.
- **Hardcoded bucket names without prefix.** Breaks parallel test isolation. Either use `WithBucketPrefix` per test, or run tests with `t.Helper` enforcing serial container reuse.

## Cross-references

- [07-nats-request-retry.md](07-nats-request-retry.md) — when to use `RequestWithRetry` vs. bare `Request`.
- `natsclient/test_client.go` — `NewTestClient` and option signatures.
- `natsclient/integration_test.go` — `startNATSContainer` helper used by older tests.
- `pkg/lifecycle/manager_integration_test.go` — Pattern B (retry-resilience) canonical example.
- `pkg/dispatch/integration_test.go` — KV-twofer setup canonical example.
- gh#93 — the classified-error wire shape these patterns build on.
- gh#170 — the cold-start race Pattern B exists to regression-test.

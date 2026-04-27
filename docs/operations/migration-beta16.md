# Migration Guide: beta.15 → beta.16

## Summary

The agentic-tools registry is now constructor-injected (ADR-029 Pattern A),
matching the component factory registry. The package-level singleton and its
helpers (`agentictools.RegisterTool`, `GetGlobalRegistry`, `ListRegisteredTools`)
have been removed. Downstream binaries that registered tools via `init()` must
switch to explicit registration on a process-owned `*ExecutorRegistry`.

The payload registry has also moved to a new leaf package
(`payloadregistry/`) so `component/` can declare a `ToolRegistryReader`
interface without an import cycle. **This is a breaking change for
payload registrants** — the package-level helpers
(`component.RegisterPayload`, `component.PayloadRegistration`,
`component.CreatePayload`) are removed and existing call sites must swap
their import path and rename the symbols. Roughly 17 files in the
semstreams repo itself needed the swap; downstream binaries with their
own payload registrations will need the same edit.

## Why

Tool registry was the only outlier in Pattern A — every other registry
(component factories, services, gateways) is a struct held by a constructor
and plumbed through Dependencies. Removing the singleton:

- Lets `go test` build per-test registries with no shared state, eliminating
  the `t.Cleanup(DeregisterTool)` smell.
- Surfaces duplicate registrations as boot-time errors instead of silent
  no-ops.
- Replaces a string-match dispatch fallback with a typed
  `agentic.ErrToolNotFound` sentinel.
- Gives downstream consumers (semspec, semdragon) a clean entry point for
  the wrapping pattern, with `wrapping_pattern_test.go` documenting the
  contract.

## Breaking Changes

### Removed Public API

| Symbol | Replacement |
|---|---|
| `agentictools.RegisterTool(name, exec)` | `(*ExecutorRegistry).RegisterTool(name, exec)` |
| `agentictools.GetGlobalRegistry()` | inject your own `agentictools.NewExecutorRegistry()` |
| `agentictools.ListRegisteredTools()` | `deps.ToolRegistry.ListTools()` (component side) or `(*ExecutorRegistry).ListTools()` |
| `agentictools.DeregisterTool(name)` | not needed — per-test registries are isolated |
| `executors.RegisterAll(ctx, deps)` | `executors.RegisterBuiltins(ctx, reg, deps)` |
| `component.RegisterPayload(reg)` | `payloadregistry.Register(reg)` |
| `component.PayloadRegistration` | `payloadregistry.Registration` |
| `component.CreatePayload(d, c, v)` | `payloadregistry.Create(d, c, v)` |
| `component.BuildPayload(d, c, v, f)` | `payloadregistry.Build(d, c, v, f)` |

`(*component.Registry).RegisterPayload` (instance method) is preserved
and now delegates to the leaf package; only the package-level helpers
moved.

### Behaviour changes

- `ExecutorRegistry.Execute` now wraps a typed sentinel: callers detect
  not-found via `errors.Is(err, agentic.ErrToolNotFound)` instead of parsing
  the error string.
- Duplicate registration on a registry is a hard error (was a swallowed
  no-op via the deleted `registerGlobal` wrapper).

## Migration Steps

### 1. Replace `init()`-based registration

Before:

```go
package mytools

import agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

func init() {
    agentictools.RegisterTool("my_tool", &MyExecutor{})
}
```

After:

```go
package mytools

import agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

// Exported constructor; the embedder wires it during registry build.
func NewExecutor() agentictools.ToolExecutor { return &MyExecutor{} }
```

Then in your binary:

```go
reg := agentictools.NewExecutorRegistry()
if err := executors.RegisterBuiltins(ctx, reg, executors.ToolDependencies{
    NATSClient: natsClient,
    // ...managers...
}); err != nil {
    return fmt.Errorf("register builtins: %w", err)
}
if err := reg.RegisterTool("my_tool", mytools.NewExecutor()); err != nil {
    return err
}

deps := component.Dependencies{
    ToolRegistry: reg,
    // ...nats client, etc...
}
```

The shared registry is plumbed through `component.Dependencies.ToolRegistry`
and the component dispatches local-first / shared-fallback per the
wrapping-pattern contract.

### 2. Replace `executors.RegisterAll` with `executors.RegisterBuiltins`

`RegisterBuiltins` takes the registry explicitly and propagates errors:

```diff
- executors.RegisterAll(ctx, executors.ToolDependencies{...})
+ if err := executors.RegisterBuiltins(ctx, reg, executors.ToolDependencies{...}); err != nil {
+     return fmt.Errorf("register builtin tools: %w", err)
+ }
```

### 3. Replace tool-listing call sites

If your component reads available tools (was
`agentictools.ListRegisteredTools()`), read from the deps-injected registry
instead:

```diff
- tools := agentictools.ListRegisteredTools()
+ tools := c.deps.ToolRegistry.ListTools()
```

The interface is `component.ToolRegistryReader`, satisfied by
`*agentictools.ExecutorRegistry`.

### 4. Remove `t.Cleanup(DeregisterTool)` patterns from tests

Build a fresh registry per test instead:

```diff
 func TestSomething(t *testing.T) {
-    if err := agentictools.RegisterTool("test_tool", exec); err != nil { ... }
-    t.Cleanup(func() { agentictools.DeregisterTool("test_tool") })
+    reg := agentictools.NewExecutorRegistry()
+    if err := reg.RegisterTool("test_tool", exec); err != nil { ... }

     // ...wire reg into the unit under test...
 }
```

### 5. Switch dispatch error checks to `errors.Is`

```diff
- if strings.Contains(err.Error(), "not found") {
+ if errors.Is(err, agentic.ErrToolNotFound) {
     // fall back...
 }
```

### 6. Swap payload-registry imports

If you have `init()`-style payload registrations using the
`component.RegisterPayload` / `component.PayloadRegistration` helpers,
update the import and rename the symbols. The shape of the
`Registration` struct is otherwise unchanged.

```diff
 package mypayloads

 import (
-    "github.com/c360studio/semstreams/component"
+    "github.com/c360studio/semstreams/payloadregistry"
 )

 func init() {
-    err := component.RegisterPayload(&component.PayloadRegistration{
+    err := payloadregistry.Register(&payloadregistry.Registration{
         Domain:   "myorg",
         Category: "mytype",
         Version:  "v1",
         Factory:  func() any { return &MyPayload{} },
     })
     if err != nil { panic(err) }
 }
```

The same swap pattern applies to call sites of `component.CreatePayload`
→ `payloadregistry.Create` and `component.BuildPayload` →
`payloadregistry.Build`.

A repo-wide find-and-replace covers most cases:

```bash
# Adjust paths as needed.
git grep -l 'component\.\(RegisterPayload\|PayloadRegistration\|CreatePayload\|BuildPayload\)' \
  | xargs sed -i '' \
    -e 's|component\.RegisterPayload|payloadregistry.Register|g' \
    -e 's|component\.PayloadRegistration|payloadregistry.Registration|g' \
    -e 's|component\.CreatePayload|payloadregistry.Create|g' \
    -e 's|component\.BuildPayload|payloadregistry.Build|g'

# Then add the payloadregistry import to each touched file (goimports
# can do this) and remove the now-unused component import where it
# only existed for these symbols.
goimports -w .
```

## Boot-time behaviour (non-breaking)

These changes don't require migration but operators should know about
them:

- **Aggregated registration errors.** `RegisterBuiltins` collects every
  registry-level failure across the boot via `errors.Join` and returns
  the aggregate. A misconfigured deployment (two packages claiming the
  same tool name) sees every collision in one error rather than only the
  first. Pre-condition skips (nil manager, missing env var, KV bucket
  unreachable after retries) remain warn-and-skip — those are intentional
  disable paths, not misconfigurations.

- **KV bucket open retried during boot.** `read_loop_result`,
  `query_entity`, and `monitor_flow` now wrap their `CreateKeyValueBucket`
  calls in `retry.Quick` (10 attempts over ~6s). A transient NATS hiccup
  at boot — circuit breaker open from a recent flap, JetStream API
  momentarily unavailable — no longer silently disables the tool for the
  process lifetime. After retries are exhausted the warn-and-skip path
  still applies, but the log line now reads `"...could not open … after
  retries"` so the failure is distinguishable from the deployment-choice
  case.

## Verification

After migrating, the following should hold:

- `go build ./...` succeeds.
- `go test -race ./...` passes.
- The binary boots and `task e2e:agentic` exercises tool dispatch end-to-end.

## Known consumers coordinated

semspec and semdragon are tracked separately. No deprecation window — clean
break. The release notes for `v1.0.0-beta.16` carry this same migration
recipe so external consumers see it on the tag.

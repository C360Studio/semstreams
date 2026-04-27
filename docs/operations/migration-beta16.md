# Migration Guide: beta.15 → beta.16

## Summary

The agentic-tools registry is now constructor-injected (ADR-029 Pattern A),
matching the component factory registry. The package-level singleton and its
helpers (`agentictools.RegisterTool`, `GetGlobalRegistry`, `ListRegisteredTools`)
have been removed. Downstream binaries that registered tools via `init()` must
switch to explicit registration on a process-owned `*ExecutorRegistry`.

The payload registry has also moved to a new leaf package
(`payloadregistry/`) so `component/` can declare a `ToolRegistryReader`
interface without an import cycle. Existing payload registrations using
`component.RegisterPayload` keep working unchanged via delegation.

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

## Verification

After migrating, the following should hold:

- `go build ./...` succeeds.
- `go test -race ./...` passes.
- The binary boots and `task e2e:agentic` exercises tool dispatch end-to-end.

## Known consumers coordinated

semspec and semdragon are tracked separately. No deprecation window — clean
break. The release notes for `v1.0.0-beta.16` carry this same migration
recipe so external consumers see it on the tag.

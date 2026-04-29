# Migration Guide: beta.22 → beta.23

## Summary

Beta.23 lands ADR-030 Phase 1: a single, framework-level HTTP
middleware seam on `service.Manager`. Products supply their own
middleware at boot — auth, request logging, panic recovery, rate
limiting, CORS — and the framework wraps every registered route
(component handlers, gateway-component handlers, system endpoints
like `/openapi.json` and `/health`) uniformly.

Additive surface; no API breakage. Components that already
register HTTP handlers via `RegisterHTTPHandlers(prefix, mux)`
need no edits. The framework still ships zero default middleware
— that is deliberate and is the boundary the rest of ADR-030
defends.

## What changes

### New public type

```go
// service/middleware.go
type HTTPMiddleware func(http.Handler) http.Handler
```

Standard `func(http.Handler) http.Handler` shape — anything that
matches works directly (chi-style middleware, gorilla-style,
hand-rolled). Order is **outermost-first**: the first middleware
passed (or registered across calls) sees the request first and
the response last.

### New `Manager` setter

```go
// service/service_manager.go
func (m *Manager) UseHTTPMiddleware(mws ...HTTPMiddleware)
```

Appends middleware to the chain wrapped around every HTTP route
the framework registers. Multiple calls accumulate in call order.
**Must be called before the HTTP server starts** (i.e., before
`StartAll`); calls after the server is running are ignored with a
warning log — the `http.Server.Handler` field is set at boot, so
late additions can't take effect, and the warning surfaces the
operator misuse instead of silently dropping it.

### Wired at server boot

`completeHTTPSetup` and the deprecated `startHTTPServer` now
construct `http.Server` with `Handler: m.buildHTTPHandler()`,
which composes the registered chain over the framework mux. With
no middleware registered the chain is a single bounds check + the
bare mux — products that don't plug anything in pay nothing.

## Migrating product binaries

This is the **only** code change in your product binary. Add a
single call between `service.NewServiceManager(...)` and
`manager.StartAll(ctx)`:

```go
manager := service.NewServiceManager(serviceRegistry)
// ... existing constructor registration ...

manager.UseHTTPMiddleware(
    panicRecoveryMiddleware(logger),
    requestLoggingMiddleware(logger),
    authMiddleware,  // your product's auth shape
)

if err := manager.StartAll(ctx); err != nil {
    return err
}
```

### Pairing with the beta.22 identity helper

Products that want HTTP-authenticated identity to flow into the
framework's downstream consumers (so `agentic-dispatch` handlers
log the authenticated user, not the body claim or the
`"http-user"` default) plug their identity-resolving middleware
into the same seam:

```go
import (
    agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
    "github.com/c360studio/semstreams/service"
)

func identityMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Product policy: read the header your auth layer guarantees,
        // or extract from a JWT, or look up a session, etc.
        identity := r.Header.Get("X-User-Id")
        if identity == "" {
            // No claim — let downstream fall back to the body or default.
            next.ServeHTTP(w, r)
            return
        }
        ctx := agenticdispatch.WithIdentity(r.Context(), identity)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

manager.UseHTTPMiddleware(identityMiddleware)
```

Now the existing `agenticdispatch.IdentityFromRequest(r, fallbackBody)`
helper inside `handleHTTPMessage` and `handleLoopApproval` (and any
future identity-aware handler) picks up the authenticated identity
via ctx — no handler edit needed, and no inline body-fallback for
the new handler to forget.

## What is NOT changing

- **`RegisterHTTPHandlers(prefix, *http.ServeMux)`** — same
  signature on every component. No handler edits.
- **`service.Dependencies`** — middleware is a Manager-level
  concern, not a per-service dependency. The struct is unchanged.
- **Wire-level identity fields** (`agentic.UserMessage.UserID`,
  `agentic.ApprovalResponse.ApprovedBy`,
  `agentic.ToolCall.ApprovedBy`, etc.) — Phase 2 territory,
  deferred per ADR-030. They remain "presenter claims" today.
- **Server-side bypass tokens** — Phase 3 territory, deferred per
  ADR-030. The `ApprovedBy` forgery surface stays mitigated by
  NATS auth scoping until product pressure or a security
  incident forces the migration.

## Order convention

`UseHTTPMiddleware(a, b, c)` produces `a(b(c(handler)))`. `a`
sees the request first, the response last. Common practice:
panic recovery outermost, request logging next, auth/identity
next, then product-specific concerns. The framework does not
take a position — order what makes sense for your product.

## Verification

```bash
# Boot your product binary.
./bin/semteams &

# Hit any endpoint — confirm middleware ran in your logs.
curl -H 'X-User-Id: alice' \
     -X POST localhost:8080/teams-dispatch/message \
     -d '{"content":"hello"}'

# Check that downstream sees the authenticated user, not "http-user".
# (semteams logs, agentic-dispatch logs, agent.created event payload, etc.)
```

## Related

- ADR-030: HTTP Middleware Contract and Authenticated Identity —
  `docs/adr/030-http-middleware-and-identity-pattern.md`
- Operations doc: `docs/operations/09-http-middleware.md`
- Beta.22 helpers: `processor/agentic-dispatch/identity.go`
  (`IdentityFromRequest`, `WithIdentity`, `DefaultIdentity`)

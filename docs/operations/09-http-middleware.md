# HTTP Middleware Contract

The framework provides a single seam for product-supplied HTTP
middleware. The seam wraps every route the framework registers —
component handlers, gateway-component handlers, and system
endpoints (`/openapi.json`, `/docs`, `/health`, `/livez`,
`/readyz`). The framework ships zero default middleware. Auth,
request logging, panic recovery, rate limiting, and CORS are
**product policy** and live in product-shell middleware.

This document is for product engineers integrating semstreams as
a library. It assumes you build your own `cmd/<product>/main.go`
and call `service.NewServiceManager(...)` directly. If you are
running the upstream `cmd/semstreams/main.go` as-is, no change is
needed — the upstream binary is intentionally neutral.

## Contract

```go
// service/middleware.go
type HTTPMiddleware func(http.Handler) http.Handler

// service/service_manager.go
func (m *service.Manager) UseHTTPMiddleware(mws ...HTTPMiddleware)
```

Standard Go middleware shape. Anything that matches the type
slots in directly: chi middleware, gorilla middleware,
hand-rolled wrappers, the standard library's `http.TimeoutHandler`.

### Order

Outermost-first. `UseHTTPMiddleware(a, b, c)` produces an effective
handler of `a(b(c(routedHandler)))`. `a` sees the request first
and the response last; `c` is closest to the registered handler.

Multiple calls to `UseHTTPMiddleware` accumulate in call order, so
you can layer middleware progressively (helpful when one part of
your boot path always needs panic recovery and another part
conditionally adds auth).

### Lifecycle

`UseHTTPMiddleware` must be called **before** the HTTP server
starts — i.e., before `manager.StartAll(ctx)`. Calls after the
server is running are ignored with a warning log because the
`http.Server.Handler` field is set at boot and late additions
can't take effect. The warning is the framework's way of
surfacing operator misuse instead of silently dropping the
registration.

## Common patterns

### Identity-aware middleware (paired with the beta.22 helpers)

Plug into the existing `agenticdispatch.WithIdentity` /
`agenticdispatch.IdentityFromRequest` pair. The middleware
populates ctx; downstream HTTP handlers (today: `POST /message`,
`POST /loops/{id}/approval`; future: any handler that calls
`IdentityFromRequest`) pick it up automatically.

```go
import (
    agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
    "github.com/c360studio/semstreams/service"
)

func identityFromHeader(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if id := r.Header.Get("X-User-Id"); id != "" {
            ctx := agenticdispatch.WithIdentity(r.Context(), id)
            r = r.WithContext(ctx)
        }
        next.ServeHTTP(w, r)
    })
}

manager.UseHTTPMiddleware(identityFromHeader)
```

The empty-header case falls through without setting ctx, so
`IdentityFromRequest` resolves to body-or-default per its
documented contract. Don't write `WithIdentity(ctx, "")`
defensively — empty ctx values are correctly treated as "no
claim" by the helper, but stamping a zero value is noisy.

### Panic recovery

The framework's `http.Server` does not install `http.Handler`-
level panic recovery; a panic inside a handler returns a 500
without the server crashing, but the response body is empty and
the panic is logged by `net/http` directly to stderr. Products
wanting structured panic logs and consistent error responses
plug in their own:

```go
func panicRecovery(logger *slog.Logger) service.HTTPMiddleware {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            defer func() {
                if rec := recover(); rec != nil {
                    logger.Error("panic in HTTP handler",
                        "panic", rec, "path", r.URL.Path)
                    http.Error(w, "internal error", http.StatusInternalServerError)
                }
            }()
            next.ServeHTTP(w, r)
        })
    }
}

manager.UseHTTPMiddleware(panicRecovery(logger))
```

Order outermost so it catches panics from inner middleware too.

### Request logging

The framework logs nothing about HTTP requests — handlers may log
their own work, but there is no per-request access log out of the
box. Products that want one supply it:

```go
func requestLog(logger *slog.Logger) service.HTTPMiddleware {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            wrapped := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
            next.ServeHTTP(wrapped, r)
            logger.Info("http",
                "method", r.Method,
                "path", r.URL.Path,
                "status", wrapped.status,
                "duration_ms", time.Since(start).Milliseconds())
        })
    }
}
```

(`statusRecorder` is a small `http.ResponseWriter` wrapper that
captures the status code; left as an exercise.)

### CORS

Products serving browser clients add their CORS shape directly.
The framework intentionally does not assume a default policy —
"allow all" would be wrong for production semteams; "deny all"
would be wrong for a developer-tools product.

## Per-route or per-prefix policy

The seam is a single global chain. If your product wants
"auth on `/loops/*` but not on `/openapi.json`," the middleware
inspects `r.URL.Path` and short-circuits or delegates
accordingly. This keeps the framework neutral and pushes route
policy to the product where it belongs.

```go
func authOnLoopPaths(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if !strings.HasPrefix(r.URL.Path, "/agentic-dispatch/loops") {
            next.ServeHTTP(w, r)
            return
        }
        if !checkAuth(r) {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

## What the framework guarantees

1. Every HTTP route the framework knows about (component-
   registered, gateway-registered, system endpoints) goes
   through the same chain.
2. Order across `UseHTTPMiddleware` calls is preserved (call
   order = outermost-first).
3. Nil entries in the slice are skipped at chain-build time —
   convenient when conditionally including middleware
   (`var auth HTTPMiddleware; if cfg.AuthEnabled { auth = ... };
   manager.UseHTTPMiddleware(auth, log)`).
4. The no-middleware path is a single bounds check + the bare
   mux. Products that don't plug anything in pay nothing.

## What the framework does NOT do

- Install default middleware. (Even logging and panic recovery
  are product policy.)
- Provide per-route or per-prefix middleware registration.
  (Inspect `r.URL.Path` in your middleware.)
- Validate or authenticate requests. (Auth is product policy.)
- Make any opinion about identity shape, header names, or token
  formats. (`agenticdispatch.WithIdentity` is the contract for
  populating identity in ctx; how you authenticate the caller is
  yours.)

## Related

- ADR-030: `docs/adr/030-http-middleware-and-identity-pattern.md`
- Beta.22 identity helpers: `processor/agentic-dispatch/identity.go`
- Beta.23 migration: `docs/operations/migration-beta22-to-beta23.md`

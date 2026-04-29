# Migration Guide: beta.21 → beta.22

## Summary

Beta.22 adds the canonical HTTP entry point for the beta.19 approval
flow: `POST /loops/{id}/approval` on the `agentic-dispatch` component.
Products running upstream binaries (semteams, semspec, semdragon) get
the endpoint at their configured prefix (e.g.,
`POST /teams-dispatch/loops/{id}/approval`) automatically — no
product code change required beyond optional auth middleware.

Additive surface; no API breakage. The endpoint is a no-op for
flows that don't use `approval_required`.

## What changes

### New HTTP endpoint

```
POST /loops/{id}/approval
Content-Type: application/json

{
  "decision": "approve" | "reject" | "modify",
  "modified_arguments": { ... },     // optional, only meaningful for modify
  "reason": "...",                   // optional, free text
  "user_id": "..."                   // optional; defaults to "http-user"
}
```

Response:

```json
{
  "loop_id": "...",
  "decision": "approve",
  "accepted": true,
  "message": "Approval 'approve' submitted for loop ...",
  "timestamp": "2026-04-29T10:30:00Z"
}
```

Status codes:

- `200` — approval accepted and published on
  `agent.approval_response.<loop_id>`. The framework's loop
  consumes the response asynchronously; this endpoint is fire-and-
  forget from the HTTP caller's perspective.
- `400` — invalid body, unknown decision value.
- `404` — loop not tracked by dispatch (process restart, never
  existed, etc.).
- `409` — loop tracked but not awaiting approval (no pending
  approval cached, or already resolved).
- `500` — NATS publish failure.

The handler resolves the gated tool's CallID from dispatch's
in-memory tracker (populated by a new `agent.approval_pending.*`
JetStream subscription) — no per-request KV.Get round-trip.

### New input port: `agent.approval_pending`

Dispatch subscribes to `agent.approval_pending.*` to keep its
in-memory tracker populated. The port is optional in `DefaultConfig`
— deployments that don't use `approval_required` continue to work
without it; only the HTTP approval endpoint requires the cache.

### New output port: `agent.approval_response`

Dispatch publishes `ApprovalResponse` envelopes here when the HTTP
handler accepts a submission. Consumed by `agentic-loop`'s existing
approval-response handler.

### New metric

```
semstreams_router_loop_approvals_submitted_total{decision, status}
```

Counter labelled by decision (`approve`/`reject`/`modify`) and
status (`success`/`error`). `success` means the response was
published to NATS; `error` means publish failed or validation was
rejected at the dispatch boundary.

### `IdentityFromRequest` helper (forward-compatible seam)

`processor/agentic-dispatch/identity.go` introduces
`IdentityFromRequest(r, fallbackBody) string` and
`WithIdentity(ctx, identity) context.Context`. Resolution order:
ctx (set by middleware) > body fallback > `"http-user"` default.

Today no framework middleware sets ctx; the helper degrades to
body+default — behaviour-equivalent to the prior inline pattern.
The helper is the seam for the planned HTTP middleware contract
(see [ADR-030](../adr/030-http-middleware-and-identity-pattern.md)).
The `handleHTTPMessage` handler is migrated to use the helper too,
so all dispatch HTTP entry points route through one identity
resolver.

## What you should do

For most deployments: nothing. Pull beta.22 and the new endpoint
becomes available. Approval UIs can `curl -X POST` against it.

If you want **header-based identity** (e.g., semteams' `X-User-Id`
convention), add a small middleware in your binary's `main.go`:

```go
import "github.com/c360studio/semstreams/processor/agentic-dispatch"

func xUserIDMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if userID := r.Header.Get("X-User-Id"); userID != "" {
            r = r.WithContext(agenticdispatch.WithIdentity(r.Context(), userID))
        }
        next.ServeHTTP(w, r)
    })
}

// Wrap the dispatch handlers — typically in your binary's main.go
// (e.g., cmd/semteams/main.go) before mounting the dispatch
// component's mux on your HTTP server. Concretely: capture the
// *http.ServeMux that service.Manager hands components, wrap it
// with xUserIDMiddleware, and pass the wrapped handler to your
// http.Server. Framework-side middleware-chain support is planned
// per ADR-030 Phase 1; until then, products own this wiring.
```

The framework reads from ctx via `IdentityFromRequest`, so the
middleware-supplied identity wins over any body claim.

If you want **role-gated approvals** (e.g., only admins can approve
delete operations), add validation in your middleware before the
handler runs. Framework stays neutral on permissions; products
own that policy.

## What didn't change

- `agentic.ApprovalResponse` wire payload shape — beta.19's contract.
- `agent.approval_response.<loop_id>` subject pattern — same as beta.19.
- `LoopManager.ResolveApprovalIfPending` atomicity (beta.19 M1) —
  unchanged. Two HTTP requests racing for the same approval are
  arbitrated by the loop, not by dispatch.
- Approval-required filter behavior — unchanged from beta.21.
- All other API surfaces — unchanged.

## Verification

After upgrading:

- `go build ./...` succeeds.
- `go test -race ./...` passes including the new
  `processor/agentic-dispatch/approval_handler_test.go`,
  `processor/agentic-dispatch/identity_test.go`, and the
  `LoopTracker_*PendingApproval*` test additions in
  `loop_tracker_test.go`.
- `task lint` reports 0 revive warnings.
- `task schema:generate` produces no diff against
  `specs/openapi.v3.yaml` — the new `/loops/{id}/approval` route
  and `ApprovalRequest`/`ApprovalAcceptResponse` types are
  registered.
- A POST to the new endpoint with a body like
  `{"decision":"approve"}` against a loop that's awaiting approval
  succeeds with 200; against a loop that isn't, returns 409.

## Operational notes

### Cache divergence on dispatch restart

Dispatch's `LoopInfo.PendingApproval` cache is in-memory — process
restart loses it. JetStream's existing redelivery on the
`agent.approval_pending.*` consumer (MaxDeliver=10 in beta.22)
covers most race windows; combined with the LoopTracker's bounded
TTL'd buffer for approval-events that arrive before their loop is
tracked, the cache repopulates on its own.

If a user attempts to approve while the cache is genuinely empty
(e.g., the gating event already drained but dispatch restarted
between then and the approval click), the handler returns 409 and
the user retries. The framework's loop state is canonical; the
cache is purely an optimization.

### Identity is forgeable on the wire (still)

Body `user_id` and the resulting `ApprovalResponse.ApprovedBy` are
plaintext claims, not authenticated identity. Anyone with HTTP
access can submit any value. This is consistent with the rest of
the framework's HTTP and NATS surfaces — see
`feedback_approval_bypass_forgery.md` and ADR-030 Phase 3 for the
threat model and planned defense-in-depth fix.

For deployments where this matters today: add product-shell
middleware that authenticates the caller (OAuth, mTLS, etc.) and
overwrites or rejects the body's claimed identity before forwarding
to the framework handler.

## Related

- [ADR-030: HTTP Middleware Contract and Authenticated Identity](../adr/030-http-middleware-and-identity-pattern.md)
  — the broader pattern problem and phased migration this commit
  starts.
- [migration-beta20-to-beta21.md](migration-beta20-to-beta21.md) —
  the previous migration (LLM truncation handling).
- [`processor/agentic-dispatch/identity.go`](../../processor/agentic-dispatch/identity.go)
  — the new helper.
- `processor/agentic-dispatch/http.go:handleLoopApproval` — the new
  handler.

# Migration Guide: beta.32 → beta.33

## Summary

Beta.33 ships tag 2 of the [ADR-032](../adr/032-policy-tenancy-cluster.md) programme —
identity-as-struct and NATS-header propagation. Closes the early-adopter role-filter
use case end-to-end.

| Surface | Status |
|---|---|
| New `auth/` package | **Additive** |
| `auth.Identity` struct (`ID`, `Role`, `Org`, `Source`) | **New public type** |
| `auth.WithIdentity(ctx, *Identity)` / `auth.IdentityFromContext(ctx)` / `auth.IdentityFromRequest(r, body)` | **New helpers** |
| NATS `X-Caller-*` headers (`X-Caller-Id`, `X-Caller-Role`, `X-Caller-Org`, `X-Caller-Source`) | **New wire-format public contract** |
| `processor/agentic-dispatch/identity.go` | **DELETED** — use `auth/` instead |
| `agenticdispatch.WithIdentity(ctx, string)` → `auth.WithIdentity(ctx, *auth.Identity)` | **BREAKING** |
| `agenticdispatch.IdentityFromRequest(r, body) string` → `auth.IdentityFromRequest(r, body) *auth.Identity` | **BREAKING** |
| `$caller.*` substitution | **Activated end-to-end** (was substitution-only in beta.32; condition-gating now works) |
| Rule conditions referencing `$caller.role` / `$caller.id` / `$caller.org` | **New capability** |

**The simplest beta.32 → beta.33 upgrade is to do nothing** — unless your product calls
`agenticdispatch.WithIdentity` or `agenticdispatch.IdentityFromRequest` directly.
Rules without `$caller.*` references behave identically. Rules WITH `$caller.*` references
now evaluate against real caller identity rather than rendering empty.

## What's new

### Identity-as-struct and the `auth/` package

`auth.Identity{ID, Role, Org, Source}` is the canonical carrier for authenticated caller
claims throughout the framework. It travels two paths:

- **In-process**: via context (`auth.WithIdentity` / `auth.IdentityFromContext`).
- **Across NATS messages**: via `X-Caller-*` headers, auto-injected and auto-extracted
  by `natsclient` at every publish and subscribe site.

Pointer-everywhere semantics apply: a nil `*Identity` means "no identity in scope" (cron
fires, KV-watch fires, anonymous system-internal triggers). A zero-value `Identity{}` means
"anonymous" — an intentional explicit claim.

The `Source` field tracks provenance via typed constants:

| Constant | Meaning |
|---|---|
| `auth.SourceHTTPHeader` | Identity resolved from HTTP middleware-populated ctx |
| `auth.SourceBodyClaim` | Identity resolved from request body field |
| `auth.SourceCtx` | Identity forwarded from an upstream context (NATS consumer path) |
| `auth.SourceDefault` | Fallback to framework default ("http-user" or equivalent) |

HTTP precedence is unchanged from tag 1: ctx > body > default. On the NATS-consumer side:
header > nothing (message body is rule input, not identity carrier — no body-claim
extraction on the NATS path).

### NATS X-Caller-* headers — public wire contract

Four headers constitute the NATS identity wire contract:

| Header | Content |
|---|---|
| `X-Caller-Id` | Caller identity string |
| `X-Caller-Role` | Role claim |
| `X-Caller-Org` | Organisation claim |
| `X-Caller-Source` | Provenance tag (one of the `auth.Source*` constants) |

Four flat headers are preferred over a single JSON header for two reasons: `nats-trace`
debuggability (headers are visible in the NATS monitoring UI without a JSON parser), and
graceful partial-data handling (a publisher that only knows the caller's ID can set one
header without constructing a full JSON envelope).

**Injection and extraction are automatic.** The `natsclient` package injects `X-Caller-*`
at every site that already injects trace headers — `Publish`, `PublishToStream`,
`PublishToStreamWithAck`, `Request`, `RequestWithHeaders`, `ReplyWithHeaders`,
`RequestWithRetry` (8 sites total). Extraction runs automatically in `Subscribe`,
`SubscribeForRequests`, `ConsumeStreamWithConfig` (3 sites), making the identity available
on the handler's `context.Context` via `auth.IdentityFromContext`.

Out-of-tree NATS consumers that bypass `natsclient` can call `auth.ExtractIdentity(msg)`
(core NATS) or `auth.ExtractIdentityFromJetStream(headers)` (JetStream) to read
inbound identity from raw headers.

### `$caller.*` end-to-end activation

In beta.32 (tag 1), `$caller.*` substitution worked in action fields (deny reasons,
publish payloads) but conditions referencing `$caller.*` silently treated the field as
missing and never matched. Authors could write the rule; it did not fire as intended.

In beta.33 (tag 2), the expression evaluator gained a `$caller.*` prefix branch —
mirroring the existing `$state.*` branch — so conditions like
`{"field": "$caller.role", "operator": "ne", "value": "admin"}` actually gate rule firing
against the live caller identity.

**Safety invariant for missing-caller context.** Cron-fired rules and KV-watch-triggered
rules have no caller in scope. When `ExecutionContext.Caller` is nil, every `$caller.*`
condition evaluates to **FALSE** for all operators. This means:

- `$caller.role != "admin"` does NOT match when no caller is in scope.
- `$caller.id == "alice"` does NOT match when no caller is in scope.

A `deny` rule that fires on `$caller.role != "admin"` will not accidentally deny system-
internal triggers. Authors who want to deny all non-admin callers AND block anonymous
system triggers must add a separate condition or accept that cron/KV-watch triggers bypass
the deny — which is the correct behavior for scheduled maintenance rules.

## End-to-end example

The following rule now works as intended in beta.33. Authors who wrote it in beta.32 will
see it begin evaluating against real caller identity without any config change.

```json
{
  "id": "role-based-deny",
  "type": "expression",
  "conditions": [
    {"field": "$caller.role", "operator": "ne", "value": "admin"}
  ],
  "logic": "and",
  "on_enter": [
    {"type": "deny", "reason": "user $caller.id (role=$caller.role) blocked"}
  ]
}
```

The canonical end-to-end integration test for this pattern is
`processor/rule/identity_propagation_integration_test.go::TestIntegration_IdentityPropagation_RoleBasedDeny`.

## Migration steps

### Framework callers of the old dispatch helpers

`processor/agentic-dispatch/identity.go` has been deleted. Any product code that imported
`agenticdispatch.WithIdentity` or `agenticdispatch.IdentityFromRequest` must migrate to
the `auth/` package. The import-path swap is mechanical:

```sh
# Step 1 — update import path
sed -i 's|"github.com/c360studio/semstreams/processor/agentic-dispatch"|auth "github.com/c360studio/semstreams/auth"|g' \
    $(grep -rl 'agenticdispatch.WithIdentity\|agenticdispatch.IdentityFromRequest' .)

# Step 2 — update call sites
sed -i \
    -e 's|agenticdispatch\.WithIdentity(ctx, "\(.*\)")|auth.WithIdentity(ctx, \&auth.Identity{ID: "\1"})|g' \
    -e 's|agenticdispatch\.IdentityFromRequest|auth.IdentityFromRequest|g' \
    $(grep -rl 'agenticdispatch.WithIdentity\|agenticdispatch.IdentityFromRequest' .)
```

Before beta.33, `WithIdentity` accepted a plain string. After, it accepts `*auth.Identity`:

```go
// Before (beta.22–beta.32):
ctx = agenticdispatch.WithIdentity(ctx, "alice")

// After (beta.33+):
ctx = auth.WithIdentity(ctx, &auth.Identity{ID: "alice"})
// or, with full claims:
ctx = auth.WithIdentity(ctx, &auth.Identity{
    ID:     "alice",
    Role:   "admin",
    Org:    "acme",
    Source: auth.SourceHTTPHeader,
})
```

`IdentityFromRequest` previously returned `string`; it now returns `*auth.Identity`. The
caller's identity string is `id.ID`; role and org are now also available:

```go
// Before:
identity := agenticdispatch.IdentityFromRequest(r, req.UserID)
// identity is a string

// After:
id := auth.IdentityFromRequest(r, req.UserID)
// id.ID  — the identity string (same as before)
// id.Role — role claim (new)
// id.Org  — org claim (new)
```

### Custom NATS consumers (out-of-tree)

Products or libraries that manage their own NATS subscriptions outside of `natsclient`
receive no automatic header injection or extraction. To participate in the identity chain:

**Publishing side** — populate ctx before publishing:

```go
ctx = auth.WithIdentity(ctx, &auth.Identity{ID: "alice", Role: "admin"})
// natsclient will inject X-Caller-* headers automatically
nc.Publish(ctx, subject, payload)
```

**Subscribing side** — extract from inbound message:

```go
// Core NATS:
identity, ok := auth.ExtractIdentity(msg)

// JetStream:
identity, ok := auth.ExtractIdentityFromJetStream(msg.Header)
```

If the subscription goes through `natsclient.Subscribe` or `natsclient.ConsumeStreamWithConfig`,
extraction is automatic and the identity is available via `auth.IdentityFromContext(ctx)` in
the handler.

### Existing rules

No changes required. Rules without `$caller.*` references behave identically. Rules that
already reference `$caller.*` in action fields (deny reason text, publish payloads) will
continue to substitute correctly. Rules that reference `$caller.*` in conditions will now
begin evaluating against live caller identity — which is the intended behavior.

## Body fields become presenter claims

`agentic.UserMessage.UserID` and `agentic.ApprovalResponse.ApprovedBy` stay in the
payload schema for backward compatibility, but they are no longer authoritative when
`X-Caller-*` headers are present. The server treats header identity as the preferred
authority for any new endpoint design.

Both header and body claims remain forgeable on the wire until tag 6 (cert-based auth +
per-org-account hardening) provides transport-enforced binding via mTLS + step-ca + cert
SAN. The framework owner's project-memory entries `feedback_approval_bypass_forgery.md`
and `feedback_http_identity_pattern.md` carry the full threat model and migration history
across phases.

Products implementing identity middleware should set headers via ctx (`auth.WithIdentity`)
BEFORE the dispatch handler reads body claims, so header identity takes precedence.

## Behavioural notes and caveats

- **Cron-fired and KV-watch-triggered rules** continue to have `Caller` nil per tag 1
  design. All `$caller.*` conditions in those rules evaluate to FALSE. Cron rule schemas
  reject the `conditions` field at config-load anyway; this note applies to expression
  rules that happen to fire from a KV-watch trigger without an HTTP caller in scope.

- **`$caller.source` is not a condition-gatable token in beta.33.** The `Source` field on
  `Identity` is dropped when converting to `CallerContext` for the rule engine. Authors
  cannot gate rule conditions on `$caller.source`. If audit-trail conditions become a
  requirement in a later tag, the conversion step is the only place to add it.

- **The four NATS header names are now a public contract.** Out-of-tree consumers that
  read `X-Caller-*` headers directly have a one-line dependency on these names. They will
  not be renamed without a coordinated migration.

- **The identity precedence chain is: ctx > body > default.** A request that sets both
  `X-User-Id` (via middleware → ctx) and a `user_id` body field will use the ctx value.
  Empty body fields are treated as "no claim" and fall through — they do not strip an
  authenticated identity already in ctx (privilege-escalation guard, carried from beta.22).

## Forward look

Tag 3 (beta.34) lands shadow mode at the rule level, `count_in_window` windowed-aggregate
operator, per-condition `negate`, and the filter-output bridge. Tag 4 (beta.35) is the
breaking org-wiring pass — the only hard-breaking change in the programme, coordinated
with semteams, semspec, and semdragon. See the full programme roadmap in the
[ADR-032 status table](../adr/032-policy-tenancy-cluster.md).

| Tag | Beta | Scope |
|---|---|---|
| 1 | beta.32 | `$caller.*` substitution + `deny` action + `cronFireStatusDenied` |
| 2 | beta.33 | Identity-as-struct + NATS-header propagation (this tag) |
| 3 | beta.34 | Shadow mode + `count_in_window` + `negate` + filter-output bridge |
| 4 | beta.35 | Org-wiring pass (HARD BREAKING) |
| 5 | beta.36 | JetStream cluster docs + reconnect defaults + replication runbook |
| 6 | beta.37 | Cert-based auth + per-org-account hardening |

## Related

- [ADR-032: Policy DSL, Multi-Tenant Identity, and Cluster Substrate](../adr/032-policy-tenancy-cluster.md)
- [ADR-030: HTTP Middleware and Identity Pattern](../adr/030-http-middleware-and-identity-pattern.md)
- [ADR-028: Orchestration Architecture](../adr/028-orchestration-architecture.md)
- [migration-beta31-to-beta32.md](migration-beta31-to-beta32.md) — tag 1: `$caller.*` substitution +
  `deny` action
- [docs/operations/08-agentic-components.md](08-agentic-components.md) — approval flow and
  identity context background
- [docs/operations/09-http-middleware.md](09-http-middleware.md) — HTTP middleware contract and
  `WithIdentity` usage

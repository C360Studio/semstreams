# Inventory — auth principal primitive (#1205, #1144)

**Inventory-only deliverable.** Per `.agents/contracts/semstreams-architect.md` step 2, this file carries the surface
inventory and the adopter seam inventory and stops there. No target state, options, recommendation, spec delta, or
task list is in this file; those exist in draft and are held pending `INVENTORY PASS` (step 3).

```text
base: 14255914e00f810cf75ab1d029009e3e656ac4da
```

Pins re-verified at that base on 2026-09-02 during inventory revision. The bare `base:` line above is the grammar
`scripts/inventory-verify.sh:22` requires — the first draft wrote `architect base:` and
`task inventory:verify` exited 2 on it, so the re-verification path this file recommends did not exist for this
file. Note a residual limitation: that script parses only bullet pins (`` - `path:line` — `text` ``) and skips the
pins carried in markdown tables below, so it verifies a subset. Table pins were re-checked by hand this revision.

Originally produced at `78813ec7`.

## 0. Verification of the pins #1205's own body names

Every reference in the epic re-checked. **Three had drifted.**

| Epic's pin | Status | Correction |
|---|---|---|
| `service/service_manager.go:1112-1125` for `Manager.UseHTTPMiddleware` | **MISLABELED** — that range is `buildHTTPHandler` | `UseHTTPMiddleware` declared at `service/service_manager.go:1086`; field `:52`; late-call warning `:1100`; wrap `:1124`; assignment `:1188` |
| `service/middleware.go:24-42` | RESOLVES | `:24` `type HTTPMiddleware func(http.Handler) http.Handler`; `:34-42` `chainMiddleware` |
| `pkg/security` `Config` TLS-only | RESOLVES | `pkg/security/config.go:11-14` — one field, `TLS TLSConfig` |
| `rule.CallerContext` + `$caller.{id,role,org}` | RESOLVES | `processor/rule/caller_substitution.go:37-52`, `:61-70`; `processor/rule/execution_context.go:150`, `:225-227`, `:363` |
| `processor/agentic-dispatch/http.go:133-135` "claimed identity field" | **DRIFTED** — those lines are the comment | call site is `http.go:138` — `req.UserID = IdentityFromRequest(r, req.UserID)`; default constant `identity.go:17` `DefaultIdentity = "http-user"` |
| `input/websocket/websocket_input.go:931-970` | RESOLVES | `:931` `authenticateRequest`; `:938` env read; `:939-941` fails closed on unset; `:949` `subtle.ConstantTimeCompare` |
| `natsclient/options.go:147` (from #802) | **DRIFTED by 1** | `:147` is the doc comment; `func WithCredentials(username, password string)` is `:148` |

## 1. Category 1 — the claimed gap

**Claim: the framework has no `Principal` type. MEASURED TRUE.**

`git grep -n "Principal" -- '*.go'` returns exactly one hit, and it is prose asserting the same absence:

```text
processor/agentic-tools/executors/predicate_authority_contract_test.go:22:
// principal to bear — NATS auth is connection-level and no Principal/Actor
```

Closing searches, all run with stderr visible:

- `git grep -n "^type \(Actor\|Subject\|Principal\|Identity\|Credential\)\b" -- '*.go'` → one hit,
  `natsclient/typed.go:50: type Subject[T any] struct` — a NATS subject, unrelated. **Name-collision note:** a
  `Subject` field on a framework principal would collide conceptually with `natsclient.Subject`.
- `git grep -n "Principal" -- 'openspec/specs/**'` → zero.
- `git grep -n "UseHTTPMiddleware\|HTTPMiddleware\|IdentityFromRequest\|WithIdentity" -- 'openspec/specs/**'` →
  **zero hits.** The middleware seam is specified nowhere; it exists only in `docs/adr/030-*.md` and
  `docs/operations/09-http-middleware.md`. **A shipped Tier 1 exported surface with no spec home and no walked path.**
- `git grep -n "UseHTTPMiddleware" -- 'cmd/**'` → **zero.** Neither production binary mounts middleware.
- `git grep -n "WithIdentity" -- '*.go' | grep -v _test` → only the declaration at
  `processor/agentic-dispatch/identity.go:72`. **No in-tree caller populates the identity ctx.**
- #1206's "zero production populators" re-verified independently and confirmed for `CallerContext`: every hit
  outside `caller_substitution.go` is a `_test.go` file.
- **`WithIdentity`'s in-tree cost is test-side, and owner ruling Q4 removes the symbol.** Four test call sites, one
  of them cross-package: `processor/agentic-loop/dispatch_cancel_integration_test.go:100`, plus
  `processor/agentic-dispatch/identity_test.go:13,42,75`. Enumerated so the removal's cost is known rather than
  discovered.
- **Examined and excluded:** `processor/agentic-dispatch/http_activity.go:289` reads `X-Client-ID` into a
  caller-scoped handle, falling back to the request ID. It is correlation, not identity or authorization; recorded
  so a refresh does not re-litigate it.

## 2. Category 2 — every current spelling of "who is the caller"

Seven in-tree spellings. More than one home for one fact is a defect to consolidate toward one primitive.

| # | Spelling | Pin | Holds | Verified? |
|---|---|---|---|---|
| 1 | `agenticdispatch.IdentityFromRequest` / `WithIdentity` / `DefaultIdentity` | `processor/agentic-dispatch/identity.go:17,52,72` | a bare `string`, package-local ctx key `identityKey{}` | No — ctx > body > `"http-user"` |
| 2 | `rule.CallerContext{ID,Role,Org}` | `processor/rule/caller_substitution.go:37-52` | the shape the DSL wants | N/A — never constructed in production |
| 3 | `loopAdmissionRequest.Requester` | `processor/agentic-dispatch/loop_admission.go:150`, `:296`, `:306` | the value a **shipped authorization check** compares | No — see §4 |
| 4 | `agentic.UserMessage.UserID` / `ApprovalResponse.ApprovedBy` / `ToolCall.ApprovedBy` | tabulated `docs/adr/030-*.md:25-32` | wire fields | No — forgeable by any NATS publisher, per ADR-030's own table |
| 5 | `message.Meta.Source()` | `message/meta.go:26` — *"Used for debugging, tracing, and access control."* | originator string | No |
| 6 | `component.PortConfig.Import` (JetStream) | `component/port_jetstream.go:50-61` — nothing on the wire is authenticated; provenance is the declaration plus the envelope `source` | operator trust declaration | Declared, not verified — deliberately |
| 8 | `PermissionConfig` — `View` / `SubmitTask` / `CancelAny` / `Approve` allowlists | `processor/agentic-dispatch/config.go:15,32`; resolved by `component.go:1151` (`hasPermission`) and `:1169` (`inList`); compared at `loop_admission.go:285,303` | **operator-authored lists of caller-identity strings** — the shipped catalog | No — compares the same unverified `Requester` |
| 7 | `agentic/identity` — `AgentIdentity`, `DID`, `VerifiableCredential`, `LocalProvider` | `agentic/identity/agent_identity.go:10`, `credential.go:11`, `did.go:11`, `local_provider.go:17` | a DID/VC identity subsystem, ed25519-signing | **Zero importers** — `git grep -ln "agentic/identity" -- '*.go'` excluding the package itself is empty |

**Item 8 is the shipped identity catalog**, and it sits three lines from the pins in row 3 — `loop_admission.go:285`
(`hasPermission(req.Requester, "approve")`) and `:303` (`inList(req.Requester, …CancelAny)`) bracket `:296` and
`:306` inside the same switch. The lists are published operator surface at `schemas/agentic-dispatch.v1.json:44,51`.
Phase 1 adds none of them but changes what a string in them **means** — asserted becomes verified — so the design
must state whether an operator's existing allowlist entries keep their meaning across the cutover.

**Item 7 disposition — RULED since this inventory was produced.** Owner, 2026-09-02: retire and remove; filed as
**#1252**. ADR-075 (2026-07-15) already ruled *"AGNTCY identity provider stub and durable core coupling — Remove"*
and the removal executed for the siblings (`agntcy_provider.go`, `oasf`, `agntcy`, `directory`) while missing this
package. It is **not** part of this change and **not** a migration source for a framework principal
(`docs/operations/27-framework-package-boundary-clean-break.md:100`).

### Verification-mechanism spellings (donors, not identity spellings)

- `input/websocket/websocket_input.go:931-970` + `input/websocket/config.go:72-75` — bearer/basic, env-sourced,
  `subtle.ConstantTimeCompare`. The only in-tree inbound verifier. **Precise posture:** its first branch is
  `:932-934` — `if i.config.Auth == nil || i.config.Auth.Type == "none" { return true }`, so *unconfigured admits
  everything* (`Auth` is a pointer, so that is the default). Fail-closed at `:939-941` covers only
  *configured-but-empty*. The design's enablement rule turns on exactly this distinction, so it is stated rather
  than compressed to "fails closed".
- `input/http/config.go:78-83` `AuthConfig{Type,Token,Username,Password}` — **outbound** (sets `Authorization` at
  `input/http/http.go:435,438`), not a verifier. **Two `AuthConfig` types with opposite directionality.**
- `gateway/http/http.go:355-359` — `strings.Contains(errStr, "unauthorized")` → 403. String-matching an error
  message for an authorization verdict; not an auth mechanism.

## 3. Blast radius of one mounted middleware

`chainMiddleware` wraps `m.diagnosticMux` **and** `m.httpRoutes` (`service/service_manager.go:1111-1125`), so a
Manager-level middleware wraps all of:

```text
gateway/graph-gateway/component.go:974     gateway/http/http.go:168
gateway/lifecycle-gateway/component.go:504 graph/inference/http_handlers.go:33
processor/agentic-dispatch/http.go:85      service/component_manager_http.go:59
service/message_logger_http.go:27          service/storage_observability_http.go:205
```

**…and `/healthz` + `/readyz`**, registered on the same mux (`service/service_manager.go:1704-1705`). The dedicated
health listener (`StartHealthListener`, `:1267`) is a *separate* `http.Server` and is **not** wrapped — but it
registers **only `/health` and `/healthz`** (`:1302`, and the comment at `:57` says so), **never `/readyz`**, and is
a no-op unless its port is configured. **So there is no unwrapped `/readyz` anywhere**; exempting it inside the
middleware is the only option, not a deployment workaround.

Measured consequence: `docker/compose/agentic.yml:99` healthchecks `http://localhost:8080/readyz` — the wrapped
port. **A naive global auth middleware breaks the e2e tier's own container healthcheck.**

**This holds in Service-Manager mode only.** graph-gateway ships an operator-settable knob,
`standalone_server` (`gateway/graph-gateway/component.go:77`, published `schemas/graph-gateway.v1.json:839`,
`category:basic`), under which it builds its **own** `http.Server` on its own `BindAddress` (`component.go:718`) and
registers onto a private mux (`:757`). **A Manager-mounted middleware cannot reach it**, and the routes so exposed
are `/graphql` and the MCP path — the highest-value surfaces in §7's sweep. The doc comment calls standalone
"tests/development" (`component.go:7`); nothing enforces that.

## 4. The shipped authorization check consuming the unverified identity

Landed 2026-09-02 (`openspec/changes/archive/2026-09-02-loop-scoped-request-seams/`, closing #1227):

- `processor/agentic-dispatch/loop_admission.go:296` —
  `if facts.UserID == "" || facts.UserID != req.Requester { … codeLoopNotOwned }`
- `processor/agentic-dispatch/http.go:633` — `Requester: IdentityFromRequest(r, "")`
- `processor/agentic-dispatch/http.go:336`, `component.go:916`, `commands.go:130,228` — `Requester: msg.UserID`

**With no middleware mounted, every anonymous HTTP caller resolves to `"http-user"` and passes every other
anonymous caller's ownership check.** The capability spec states this itself and names this epic as the remedy —
`openspec/specs/agentic-dispatch/spec.md:11-13`.

The reachable path is the write path, not the read path: `POST /message` carrying `reply_to`
(`HTTPMessageRequest.ReplyTo`, `http.go:38`) → `req.UserID = IdentityFromRequest(r, req.UserID)` (`:138`) →
`"http-user"` when the body omits `user_id` → `Requester: msg.UserID` (`:336`, `seamHTTPSubmission`,
`loopOpContinue`) → `loop_admission.go:296`. `http.go:633` is `loopOpRead`, which is deliberately not
ownership-checked.

**Two further in-tree consumers read the same unverified `msg.UserID`**, and they are not ownership checks:

- `processor/agentic-governance/rate_limiter.go:91-94` — per-user token buckets keyed on `msg.UserID`
  (`getUserBucket(msg.UserID)`). Every anonymous caller shares one bucket; a caller who supplies any `user_id` in
  the body mints a fresh one. Same forgeability, availability blast radius rather than ownership.
- `processor/agentic-governance/violation.go:148` — `component.ResolveSubject(h.outputs, "violations",
  violation.FilterName+"."+violation.UserID)`, with `violation.UserID` sourced from `msg.UserID` at `:239`. **A
  caller-supplied string becomes a NATS subject token.** That is a subject-validity fact as well as an
  identity-provenance one; it is enumerated here rather than left to the design to rediscover.

## 5. Category 3 — adjacent claims on the territory

| Claim | Pin | Overlap / conflict |
|---|---|---|
| ADR-030 Phases 0+1 shipped | `docs/adr/030-*.md:5-18,54-117` | The seam this builds on. **Numbering conflict: ADR-030's own "Phase 2" (NATS headers) ≈ #1205's "Phase 3."** |
| ADR-030 "zero default middleware" | `docs/adr/030-*.md:18,114,193`; restated in source at `service/middleware.go:17-19` | Asserted in four places, one of them live code |
| ADR-032 Tag 2 "Identity-as-struct + NATS-header propagation" | `docs/adr/032-*.md:15` | The tag being woken; split — struct in, propagation out |
| ADR-032 Track A item 3 `$caller.tenant`, `$caller.trust_score` | `docs/adr/032-*.md:223-228` | **Never shipped.** Only `id/role/org` exist |
| ADR-032 entity-ID order | `docs/adr/032-*.md:106` — `org.platform.domain.system.type.instance` | **Stale vs ADR-102** (`org.platform.system.domain.type.instance`). Not this change's job; noted so a successor does not copy it |
| ADR-105 carve-out | `docs/adr/105-*.md:14-25` | "the missing control is authorization at the seam that ATTACHES to a loop, filed as #1227" — #1227 landed keyed on an unverified identity |
| #802 deferral finding 1 | issue #802 — *"There is no principal to bear"* | This is the substrate #802 named as prerequisite |
| #1206 | `class:advertised-absent` | Partially addressed; ruled 2026-09-02 to stay open for the role/org half |
| #680 | `service/openapi.go:12-23`, `service/openapi_types.go:28` have no security field | Rides with Phase 2 |
| #882 / #211 / #678 | issues | Consume the principal in Phase 3 |
| #1253 | filed 2026-09-02 | Forwarded-header reference middleware — three adopters hand-roll the trusted-proxy pattern |
| **`openspec/specs/agentic-dispatch/spec.md:202`** — `### Requirement: The gate is not authorization, and the spec says so` | shipped requirement | **Phase 1 makes it false.** Its body states *"This capability MUST NOT be read, cited, or extended as an authorization boundary… A party that can reach a dispatch seam can therefore claim any identity."* OpenSpec cannot rename, so this is REMOVE+ADD. It carries one live citation — `processor/agentic-dispatch/loop_seams_test.go:482` — which `task spec:properties` (`Taskfile.yml:111`) verifies, so a careless delta strands the citation and reds that gate |
| `openspec/specs/service-composition/spec.md:106` — `### Requirement: Composition seals before any service starts or contributes HTTP or OpenAPI` | shipped requirement | The ordering window the mount must sit inside — the same window `UseHTTPMiddleware` enforces with its post-boot WARN (`service/service_manager.go:1100`). §1's "specified nowhere" is true of the seam's *shape*, not of its *timing* |
| `docs/operations/09-http-middleware.md:69-96` | doc | Teaches the *string* identity pattern; stale the day a principal lands |
| `docs/operations/17-tool-call-governance.md:270-283` | doc | **Ships a `$caller.role` rule example that cannot match today** |

## 6. Category 4 — the consumer at birth

| Proposed symbol | Consumer at birth | Pin |
|---|---|---|
| `security.Principal` | agentic-dispatch admission gate | `processor/agentic-dispatch/loop_admission.go:296` |
| `security.PrincipalFrom(ctx)` | `handleHTTPMessage`, `handleLoopApproval`, `handleGetLoop` | `processor/agentic-dispatch/http.go:138,633,730` |
| `security.WithPrincipal(ctx,p)` | the reference middleware; semteams' `xUserIDIdentityMiddleware`; semconnect's `IdentityMiddleware` | `semteams/cmd/semteams/middleware.go:44` (read-only) |
| `httpauth.Bearer(...)` | `cmd/e2e-semstreams/main.go` (the walked path); semdragon's mount | **proposed** mount site beside `service.NewServiceManager` in `cmd/e2e-semstreams/main.go` — not an existing pin; `semdragon/cmd/semdragons/main.go:154` is |
| `security.BearerConfig` under `security.Config` | `config/config.go:305-350` validation path; `component.Dependencies.Security` | `component/dependencies.go:71` |
| `rule.ExecutionContext.Caller` population | `docs/operations/17-tool-call-governance.md:270-283` documented rule pattern | as pinned |

One symbol was **considered and dropped** on this rule: a `Principal.IsVerified() bool` predicate — no Phase 1+2
consumer decides on it, so it would be born a phantom.

## 7. Category 5 — problem shape, and the adoption sweep

**Shape:** *establish a fact at the admitting seam, carry it forward as framework-owned truth, and let downstream
consumers read rather than predict it.* Three existing in-tree instances, which this change adopts rather than
establishes:

1. `processor/graph-ingest/authority_gate.go:35-62` — admit-or-refuse, structural check first, explicit carve-out
   (`importLane`), classified refusal with `Code`/`Detail`, one metric-reason home (`authorityMetricReason`), one
   named log constant (`:33`).
2. ADR-104 / `test/e2e/scenarios/platform_identity.go:19-21` — *"the value every other fixture now READS instead of
   predicting."*
3. `processor/agentic-dispatch/loop_admission.go:195,254-307,504-536` — fixed-order gate (form → existence →
   ownership) with an HTTP-status mapping home.

**Adoption sweep — enumeration obligation only** (2026-09-01 ruling), never a migration obligation. One named
primitive intended for reuse (`security.Principal`); the planes that should adopt it, to be filed as one tracking
issue. **This change fixes none of them and their count does not block it:**

- `gateway/graph-gateway/component.go:974` — `func (c *Component) RegisterHTTPHandlers`

  `/graphql`, no principal (#882). Escapes the chain entirely under `standalone_server: true` — see §3.

- `gateway/lifecycle-gateway/component.go:504` — `func (c *Component) RegisterHTTPHandlers`

  Operator surface, no principal (#678).
- `gateway/http/http.go:168` + `:355-359` — string-matches "unauthorized" into a 403
- `graph/inference/http_handlers.go:33` — `func (h *HTTPHandler) RegisterHTTPHandlers`

  No principal.
- `service/component_manager_http.go:59`, `service/message_logger_http.go:27`,
  `service/storage_observability_http.go:205` — diagnostic/operator surfaces, no principal
- `input/websocket/websocket_input.go:900-904` — **verifies, then discards the result**: `authenticateRequest`
  returns `bool` and no principal is constructed from the credential it just checked
- `natsclient/options.go:148` — `func WithCredentials(username, password string)`

  Connection-level credentials, no per-message principal (Phase 3 / #802).

**Planes no Manager-level mount can reach at all** — separate `http.Server` instances, listed separately because a
single mount does not cover them and §3 would otherwise read as the complete HTTP perimeter:

- `service/pprof.go:35` — `http.ListenAndServe(addr, nil)`

  On `http.DefaultServeMux`, serving `/debug/pprof/*` from both binaries. Debug-gated, unauthenticated,
  unreachable by any framework middleware.

- `metric/handler.go:113` — `server = &http.Server{`

  Served at `:144`. `grep -ci "auth\|token" metric/handler.go` → **0**.

- `output/websocket/websocket.go:744` — `server = &http.Server{`

  Served at `:1342`. Its own doc states the position — `output/websocket/doc.go:225`,
  *"No authentication/authorization (add reverse proxy)"*. That is the framework directing adopters to solve auth
  outside the framework, which is this epic's premise.

- `service/service_manager.go:1307` — `server := &http.Server{`

  The dedicated health listener (see §3): only `/health` and `/healthz`, never `/readyz`.

  graph-gateway under `standalone_server: true` (see §3) also escapes the chain — `/graphql` and MCP on a private
  mux.

## 8. Same-class collision table

Semantic class: **"who is the caller, and is that answer the framework's or the caller's?"**

| Dimension | Evidence |
|---|---|
| Semantic class | Establishing and carrying request-scoped caller identity |
| Owners | `processor/agentic-dispatch`; `processor/rule` (never constructed); `input/websocket` (verifies, discards); `agentic/identity` (retiring, #1252); outside: semconnect, semteams, semdragon, semsource, semsage |
| Catalogs | **`PermissionConfig`** — `View`/`SubmitTask`/`CancelAny`/`Approve` (`processor/agentic-dispatch/config.go:15,32`), an operator-authored catalog of caller-identity strings, published at `schemas/agentic-dispatch.v1.json:44,51` and resolved at `component.go:1151,1169`. No catalog of the *seam* exists: `git grep -n "UseHTTPMiddleware\|HTTPMiddleware" -- 'openspec/specs/**'` → zero |
| Status | **None.** No readiness signal, health field, or operator-visible state reports whether auth is mounted — the defect that lets three adopters run fail-open unnoticed |
| Lifecycle | `UseHTTPMiddleware` is boot-only; post-`Start` calls dropped with a WARN (`service/service_manager.go:1099-1102`); chain assembled once at `:1188`. Specified: `openspec/specs/service-composition/spec.md:106` — composition seals before any service contributes HTTP |
| Ownership | Manager-level, single chain, single-writer at boot. No claim/lease/partition question |
| Readers | `processor/agentic-dispatch/http.go:138,633,730`; `loop_admission.go:285,296,303,306`; `processor/agentic-governance/rate_limiter.go:91-94` (per-user buckets); `processor/agentic-governance/violation.go:148,239` (**identity becomes a NATS subject token**); would-be reader `processor/rule/execution_context.go:363` |
| Writers | **Zero in-tree.** Outside: `semteams/cmd/semteams/middleware.go:44`, `semdragon/cmd/semdragons/main.go:154` |
| Recovery | Request-scoped, nothing durable. The one durable trace is the loop owner at `loop_admission.go:398` — an existing field whose *provenance* changes, not its shape |

**No new durable, communication, or coordination primitive is in scope.**

Decision skills, applied and recorded: **`/kv-or-stream` not triggered** — no new communication path; the principal
rides an in-process request context (Phase 3's NATS headers would trigger it). **`/orchestration-check` not
triggered** — no multi-step behavior, no rule/component boundary moves. **`/new-payload` partially triggered** — no
new payload type; adds fields to an existing payload's `RuleFields()` projection, covered by the projection
contract at `message/rule_readable.go:59`. **`/query-pattern` not triggered** — no new query access.

## 9. Context-ownership audit

- `service/service_manager.go:1189` and `:1310` set `http.Server.BaseContext` with a closure capturing the exact
  `Start` context — the narrow permitted exception, already correct. **Every request context already descends from
  `Start`**, so a middleware deriving with `context.WithValue` and passing `r.WithContext(...)` retains nothing and
  creates no root.
- No struct retains `context.Context`; no `context.Background`/`TODO`/`WithoutCancel` added; no exported
  `CancelFunc`.
- `processor/agentic-dispatch/identity.go:11` already uses an unexported struct key (`identityKey{}`) — the correct
  pattern.

## 10. Adopter seam inventory

### Person A — a developer outside this repo writing a component

1. **What must they know?** *Nothing.* The principal is read at seams the framework already owns and stamped onto
   the fact the component already consumes (`Requester`, `UserID`). No port, config, signature, or registration
   change. To scope by caller is one call.
2. **If they do nothing?** Today's behavior. Nothing compiles differently.
3. **Where do they find out?** They don't have to. Where they do look: a two-value accessor is a compile error if
   the presence flag is dropped.
4. **What SHOULD they know?** Nothing. **Gap: zero** — the reason the principal is stamped onto the existing fact
   rather than added as a new component-visible surface.

### Person B — a developer outside this repo composing a product that wants auth

1. **What must they know?** One mount, and one accessor if their own code needs the caller. Three things they must
   **not** have to know, and today do:
   - which routes to protect — semdragon invented `ProtectNamespace`; the framework registered every route and owns
     `/healthz`+`/readyz` (§3);
   - whether an unset credential means "off" or "closed" — semdragon (`internal/httpauth/auth.go:22-24`) and
     semsource chose "off"; the in-tree donor chose "closed";
   - that a secret comparison must be constant-time — semdragon `auth.go:35` uses `token != apiKey`; the in-tree
     donor uses `subtle.ConstantTimeCompare` (`websocket_input.go:949`).
2. **If they do nothing?** Today's behavior. (A design could add one difference — a config that *names* a credential
   source resolving empty becoming a boot refusal rather than a silent passthrough — but that behavior does not
   exist today and is not measured here.)
3. **Where do they find out?** Compile error → boot error → typed runtime error → doc. Nothing correctness-bearing
   rests on the doc.
4. **What SHOULD they know? One mount, one accessor.** The gap is the three must-nots, and closing it is deletion of
   knobs, not documentation of them.

### Prefer observation to prediction — the three predictions the adopter makes today

**Left column is measured; right column is what a design would have to absorb and is NOT a description of existing
behavior.** Stated this way because this file is inventory-only and none of the right column exists.

| Prediction the adopter makes today (measured) | What the framework would have to absorb |
|---|---|
| *Which paths need protecting?* — semdragon names `lifecycleGatewayNamespace` (`internal/httpauth/auth.go:53`) | The framework registered every route and owns `/healthz`+`/readyz` (`service_manager.go:1704-1705`); a design could exempt its own diagnostics by construction |
| *Is auth on?* — `LoadAPIKey()` returning `""` means "development passthrough", indistinguishable from a missing env var in production (`semdragon/internal/httpauth/auth.go:22-24`) | A design could make presence of a resolvable credential source the enablement, with configured-but-empty a boot refusal and no third state |
| *How do I compare a secret?* — semdragon uses `token != apiKey` (`auth.go:35`) | A design could put the comparison inside the framework, lifting `subtle.ConstantTimeCompare` from `websocket_input.go:949` |

The one thing an adopter would still predict, deliberately: the credential's own value.

### Sister measurement (read-only, one bounded pass)

Four adopters implemented a verifier; **three fail open.**

| Adopter | Mechanism | Posture |
|---|---|---|
| semstreams `input/websocket` (in-tree donor) | bearer/basic, env-sourced, constant-time | **fails closed** (`websocket_input.go:939-941`) |
| semdragon `internal/httpauth` | API key / bearer, `token != apiKey` | **fails open** on unset (`auth.go:22-24`); non-constant-time (`:35`) |
| semsource `processor/mcp-gateway` | `SEMSOURCE_API_TOKEN` bearer | **permissive when unset** (its ADR-0006) |
| semconnect `gateway/cs-api` | records `X-Forwarded-User`/`-Email` | **verifies nothing** — `Verified` documented always false |
| semteams `cmd/semteams/middleware.go` | trusts `X-User-Id` verbatim | no verification |

Five independently invented principal shapes. semconnect's (`gateway/cs-api/identity.go:15-28`) is closest to the
proposed one including the how-verified field, arrived at with no framework input.

**Three adopters trust an upstream header** — semteams, semconnect, semlink. Phase 2 ships a bearer verifier, which
is not what any of them needs; filed as **#1253**.

## 11. Known gaps in this inventory

Stated rather than left for the reviewer to infer:

- **`task inventory:verify` covers a subset of this file, by design of the checker, not by drift.** It now runs
  (the `base:` line is in the grammar it requires) and reports `pins=27 ok=10 moved=0 ambiguous=0 drift=0` over the bullet
  pins (17 `MALFORMED`, 10 `UNPARSED` — all prose bullets, see below). It does **not** see the ~40 pins carried in markdown tables — the parser reads bullets only — and it reports
  this file's prose bullets as `UNPARSED`, because under any `## ` heading other than `## Searches` or
  `## Adjacent claims` it requires every bullet to be a pin. The architect contract's five-category inventory format
  produces prose and tables, so that mismatch is structural rather than specific to this file; filed separately.
  Every table pin in this file was re-checked by hand during the 2026-09-02 revision.
- The sister pass was **one bounded pass** (greenfield ruling: it sizes the migration note, it does not gate
  design). Repos with zero hits: semops, semmachina, semdev, semboids, semmem, semembed, semlink (Go), semsummarize,
  servicesim, semstreams-ui. semconnect's Go hit was its own local identity type.
- No enumeration was done of NATS-side identity beyond `natsclient/options.go:148` — Phase 3 territory by ruling,
  so bounded deliberately rather than missed.
- `agentic/identity` (§2 item 7) is enumerated but out of scope by the 2026-09-02 ruling (#1252).

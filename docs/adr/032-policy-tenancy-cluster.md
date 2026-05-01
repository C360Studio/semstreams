# ADR-032: Policy DSL, Multi-Tenant Identity, and Cluster Substrate

## Status

**Accepted — In Progress (2026-04-30).** Single ADR covering three coordinated tracks
because they surfaced together in a framework review and they are
operationally coupled — adopting any one in production exposes the
others.

### Implementation Status

| Tag | Beta | Status | Date | Notes |
|---|---|---|---|---|
| 1 | beta.32 | **Shipped** | 2026-05-01 | `$caller.{id,role,org}` substitution + `deny` action + `cronFireStatusDenied`. |
| 2 | beta.33 | **Shipped** | 2026-05-01 | Identity-as-struct (`auth.Identity`) + NATS `X-Caller-*` headers + `$caller.*` condition support. |
| 3 | beta.34 | Pending | — | Shadow mode + `count_in_window` + `negate` + filter-output bridge |
| 4 | beta.35 | Pending | — | Org wiring pass (HARD BREAKING) |
| 5 | beta.36 | Pending | — | JetStream cluster docs + reconnect defaults |
| 6 | beta.37 | Pending | — | Cert-based auth + per-org-account hardening |

The forcing function: a 2026-04-30 review of [Microsoft Agent
Governance Toolkit](https://github.com/microsoft/agent-governance-toolkit)
mapped against the OWASP Agentic Security Initiative (ASI) Top 10
exposed three concerns at once — policy expressiveness, multi-tenant
isolation, and operational substrate. They are not strictly
dependent on each other (NATS accounts isolation is enforced at the
server protocol layer regardless of topology, and a JetStream
cluster matters for durability whether or not accounts are in use)
but they share design surface: tenant-aware identity, KV bucket
scoping, and `ResolveSubject` substitution all touch each other,
and any deployment serious enough to adopt server-enforced tenant
isolation will also want HA durability. Capturing them in one ADR
keeps that shared surface coherent.

## Context

### Three concerns, one programme

**1. Governance gap vs OWASP-ASI Top 10.** Current state:

| ASI | Coverage | Why |
|---|---|---|
| 01 Goal hijacking | Medium | InjectionFilter catches input attacks; no goal-mutation lock on the loop entity |
| 02 Excessive capabilities | Medium | Approval filter + `Action.Tools` allowlist + ToolCallFilter blocklist; default-allow not default-deny |
| 03 Identity & privilege abuse | Weak (framework); **product-owned** by `feedback_framework_boundary.md` — `user_id` and `ApprovedBy` are forgeable wire fields (`feedback_approval_bypass_forgery.md`) |
| 04 Uncontrolled code execution | Strong | `processor/agentic-tools/sandbox/` per-loop container scoping |
| 05 Insecure output handling | Strong | PII / Injection / Content filters bidirectional; truncation handled (beta.21) |
| 06 Memory poisoning | **Weak** | No provenance triples, no signed content hash, no quarantine, no integrity check on reads |
| 07 Unsafe inter-agent comms | Weak (framework primitives only) | NATS transport; no signed delegation tokens, no trust scoring |
| 08 Cascading failures | Medium | `model/RollingWindowBreaker` LLM-endpoint-scoped only; no agent or tool breaker |
| 09 Trust deficit | Strong | Trajectory entity per loop, OTel traces, governance violation KV; no chain-hash audit |
| 10 Rogue agents | Weak | `LoopManager.CancelLoop` per-loop only; no global kill switch, no anomaly-driven isolation |

Microsoft AGT ships a declarative policy language (YAML / OPA Rego /
Cedar) and claims 0.00% violation rate via deterministic enforcement at
every step. semstreams encodes policy across filter configs, rule
conditions, and per-component Go code. The rule engine is already an
ECA (Event-Condition-Action) policy engine that maps cleanly to
6 of the 10 ASI items — the four where ECA is awkward (02, 03, 07,
parts of 06) are either product-owned or addressable with small
additions.

**2. Multi-tenant isolation.** semstreams runs against stock,
unauthenticated NATS today. Cross-tenant lesson exfiltration is the
most dangerous shape of ASI-06 memory poisoning. Soft prefix-only
isolation (no auth) depends on every read site getting the prefix
right forever and is not a serious answer.

NATS provides two server-enforced isolation primitives that are
genuinely different:

- **Accounts** — top-level namespace boundary. Subjects, streams,
  KV, ObjectStore are physically separated. Cross-account access is
  an explicit, audited export/import.
- **User-level subject permissions** — within one account, a user
  can be locked to `publish/subscribe: ["acme.>"]`. The server
  enforces it at protocol layer. Same applies to KV via subject
  permission on `$KV.<bucket>.<prefix>.>`. Buckets/streams are
  shared, but read/write authority is prefix-scoped.

Authentication can ride on either NATS JWTs or mTLS client certs
(`tls.verify_and_map` extracts cert subject/SAN and maps to a NATS
user, which carries account assignment and subject permissions).
mTLS is universal identity (same cert authenticates HTTP, gRPC,
NATS), composes with SPIFFE-style SAN URIs
(`spiffe://semstreams/tenants/$tenant/agents/$role`), and uses
mature PKI tooling like step-ca with ACME for short-lived certs and
auto-renewal. JWTs are NATS-specific and require NSC + a resolver.

Four options on the table:

| Strategy | Isolation | Ops cost | Cross-tenant queries | Leak risk |
|---|---|---|---|---|
| Soft prefix only (no auth) | Discipline-based | Lowest | Easy via wildcard sub | High — one wrong wildcard or missed `ResolveSubject` site leaks |
| **mTLS + user-permissions on prefix** (one account, N tenants) | **Hard, transport-enforced** | Moderate (PKI: step-ca, ACME, cert templates) | "Admin" user with broader permissions | Low |
| **NATS accounts** (one cluster, N accounts; auth via JWT or mTLS) | **Hard, transport-enforced + physical resource separation** | Moderate-to-high (JWT/account chain, or PKI per-account) | Explicit export/import (audited) | Lowest |
| Per-tenant NATS instance | Hardest | Highest (N clusters to operate) | Impossible without external aggregator | None |

The right granularity depends on tenant nature, not topology:

- **Unrelated tenants** (different customers, different blast
  radius, possibly different compliance regimes) → accounts
- **Related sub-tenants** (orgs within one customer; teams within
  one org) → user-permissions on subject prefix inside one account
- **The 6-part entity ID already encodes this hierarchy.**
  `org.platform.domain.system.type.instance` — `org` is the natural
  account-internal namespace; the account boundary sits above `org`
  for multi-customer deployments.

**Recommended default: mTLS + step-ca + cert SAN URI carrying
tenant claim + NATS user-permissions for subject/KV scoping, with
account boundaries reserved for hostile multi-tenancy** (compliance,
unrelated customers, regulatory residency). This composes cleanly:
products that need accounts get them, products that don't pay only
the PKI cost (which most ops teams already pay anyway).

**3. Operational substrate.** The framework's documentation pattern
to date assumes "stock NATS" — single-node, unauthenticated,
ephemeral-friendly. Ops teams have started asking how to back up the
SKG (Semantic Knowledge Graph). The honest answer is **don't back up
a single-node SKG; run a JetStream cluster with replicated streams.**
Accounts work is unsafe on a single node anyway — losing the node
loses every tenant's state simultaneously.

### What semstreams already has working in its favor

The 6+ months of work since alpha has accidentally pre-built a lot of
the seams these tracks need:

- `component.ResolveSubject` (beta.8 / beta.12) — every publish site
  routes through one resolver. Tenant-scoping at the resolver is one
  diff, not N.
- `ConsumerNameSuffix` already exists for per-deployment uniqueness.
- 6-part entity ID has `org` as part one — the data layer already
  respects tenancy.
- Model registry, rule definitions, governance violations, profile
  data are all KV-backed — naturally per-tenant.
- ADR-030 Phase 0/1 (`IdentityFromRequest`, `service.HTTPMiddleware`)
  shipped; Phase 2 (NATS message-header identity) is the obvious
  carrier for tenant claims and was deferred specifically waiting for
  a forcing function. Multi-tenant work is that function.
- ADR-031 cron rule type (sister-agent branch, beta.27 candidate) is
  the periodic-enforcement primitive several ASI-10 sweeps need.
- `model/RollingWindowBreaker` shape is reusable for agent-scoped and
  tool-scoped breakers (ASI-08, ASI-10).

What semstreams **lacks** that these tracks need:

- Tenant-aware `Dependencies` so each component sees its tenant
  context.
- KV bucket name templating (whether per-account default buckets or
  `AGENT_LOOPS_${tenant}` style — open question, see Decision).
- A `deny`-style action in the rule engine for short-circuit
  enforcement.
- Documented production topology — single-node JetStream is currently
  the only path the docs describe in detail.
- Operational tooling around the chosen auth issuer (PKI/step-ca
  for mTLS, NSC + resolver for JWT-based accounts, or both).

### The cost we're explicitly accepting

Server-enforced tenant isolation has real ops cost no matter which
auth path we take.

- **JWT-based accounts** require operator/account/user JWT chains,
  signing-key custody, NSC tooling, and a resolver service. The
  framework owner has direct experience of this pain ("I have lived
  it"). The reward is decentralized auth and account-native features.
- **mTLS + step-ca** moves the cost into PKI: cert templates,
  provisioner config, ACME endpoints, revocation. Most orgs already
  pay this cost for HTTP/gRPC services, so it's incremental rather
  than additive. The reward is universal identity (one cert, many
  protocols) and SPIFFE compatibility.

This ADR accepts the cost in either form as the price of hard
tenant isolation, and constrains scope to **make the framework
work with whichever the deployment chooses** rather than to invent
auth tooling. The recommended default (mTLS + step-ca + user-
permissions, accounts reserved for hostile boundaries) is the
combination with the broadest ops familiarity, but the framework
must not lock that choice in.

## Constraints and goals

- **Honor the framework boundary** (`feedback_framework_boundary.md`).
  The framework provides seams; products plug in auth issuers, tenant
  catalogs, role taxonomies. semstreams does not become an auth
  issuer (whether NATS JWT, X.509 certificate authority, or
  anything else).
- **Stay backward compatible during early phases.** Single-tenant
  deployments on single-node NATS must keep working through Track A
  and most of Track C. Track B is the only opt-in breaking change.
- **One evaluator, one config shape.** The rules engine extensions in
  Track A do not become a parallel policy DSL — they are additive
  operators and action types on the existing evaluator.
- **Replication, not snapshots.** SKG durability comes from JetStream
  cluster replication (R≥3), not from external backup tooling.
- **No new orchestration layer.** Per ADR-028, governance enforcement
  remains rule-shaped where ECA fits and component-shaped (filter
  chain) where it doesn't.

## Decision

Three coordinated tracks. Each track is independently shippable, but
the order matters: A → C → B.

### Track A — Promote the rule engine to a first-class policy DSL

The rule engine already expresses governance for 6/10 ASI items
natively. Five additive extensions promote it from "orchestration
engine that happens to enforce some policy" to "policy engine that
happens to orchestrate":

1. **`deny` action type.** Short-circuit a flow rather than only
   generating side effects. Enables: "on `agent.task` with role=admin
   and `$caller.role != admin_pool`, deny." Rule evaluator gains a
   policy verdict, not just an action list.
2. **`logic: "not"` / per-condition `negate: true`.** Express
   blocklist-style policies declaratively rather than as imperative Go
   in filter implementations.
3. **Principal pseudo-fields.** `$caller.id`, `$caller.role`,
   `$caller.tenant`, `$caller.trust_score` — same shape as existing
   `$state.*` and `$entity.*`. The values come from the
   tenant-aware identity context (Track B carries them on the wire;
   Track A can land with a placeholder resolver that reads from
   message headers when present).
4. **Windowed aggregate operator.** `count_in_window(field, window:
   "1m") > 10` — closes the "10 calls in last hour" gap that today
   only exists as Go in `RateLimiter`. Enables ASI-08 / ASI-10 rules
   without component-side state.
5. **Shadow mode at rule level.** `enabled: "shadow"` evaluates and
   logs without firing actions. Already exists for the governance
   filter chain (`log_only`); promote to rules. Required for safe
   policy rollout.

Three secondary additions, lower priority:

- **`policy` rule type** (or a `category: "policy"` convention) — same
  evaluator, separate config bucket (`POLICIES` vs `RULES`), separate
  metrics. Operators don't confuse policy with orchestration.
- **Rule priority + first-match-wins resolution.** Today rules fire
  independently; for `deny`-style policies, ordering matters.
- **Policy unit-test harness.** Evaluate a rule against a fixture
  set, report pass/fail. Cheap if the rule evaluator is pure (it is).

Coverage delta when Track A ships:

| ASI | Before | After Track A |
|---|---|---|
| 01 Goal hijacking | Medium | **Strong** — goal-lock triple + `deny` rule on mutation |
| 02 Excessive capabilities | Medium | **Strong** — `deny` rule with `$caller.role` + tool name allowlist |
| 06 Memory poisoning | Weak | **Medium** — provenance triples + windowed write-rate guard (full strong needs Track B for cross-tenant) |
| 08 Cascading failures | Medium | **Strong** — agent/tool breakers as windowed-count rules |
| 10 Rogue agents | Weak | **Medium** — global kill switch as KV flag + cron sweep (full strong needs Track B scoping) |

### Track B — Tenant-aware identity and server-enforced isolation

The architectural commitment is "tenant-aware identity carried on
the wire, with server-enforced isolation at the NATS protocol
layer." The framework becomes tenant-aware at the seams that
already exist; the *mechanism* for that isolation is a deployment
choice (mTLS + user-permissions, accounts, or both) and the
framework supports all of them.

**Recommended default deployment shape:**

- **mTLS for connection auth.** step-ca (or any ACME-compatible PKI)
  issues short-lived certs with SPIFFE-style SAN URIs:
  `spiffe://semstreams/tenants/$tenant/agents/$role`.
- **NATS `tls.verify_and_map`** maps cert SAN to a NATS user.
- **User permissions on subject prefix:** `publish/subscribe:
  ["$tenant.>"]`, `subscribe: ["$KV.AGENT_LOOPS.$tenant.>"]`.
- **One NATS account per administrative boundary**, not per tenant.
  Sub-tenants ride on subject-prefix permissions inside the account.
- **Accounts reserved for hostile multi-tenancy** — unrelated
  customers, regulatory residency, compliance separation.

**Framework changes (auth-mechanism-agnostic):**

1. **Tenant-aware `Dependencies`.** A `Dependencies.Tenant` field
   carries the active tenant context. Components that already use
   `Dependencies.NATSClient` and `Dependencies.MetricsRegistry` see no
   API change; the connection is authenticated and scoped before
   `Dependencies` is constructed.
2. **`ResolveSubject` learns about `$tenant`.** Required substitution
   variable in multi-tenant deployments. Default deployments without
   tenancy treat it as a no-op (empty string collapses cleanly). Used
   uniformly for both prefix-mode (`$tenant.agent.task.>`) and
   account-mode cross-account export/import.
3. **KV scoping is connection-driven.** Under accounts, bucket names
   stay identical (`AGENT_LOOPS` per account). Under prefix-mode in
   one account, keys are `$tenant`-prefixed within shared buckets,
   and watchers subscribe under their tenant prefix. Same code path
   either way once `$tenant` is in `Dependencies`.
4. **ADR-030 Phase 2 unblocked.** Tenant claims ride on NATS message
   headers alongside identity claims. The wire format is independent
   of whether the underlying auth is JWT or mTLS — both can carry
   the claim. This ADR is the forcing function ADR-030 Phase 2 was
   waiting for.
5. **Per-tenant model registries, rule sets, profile data, governance
   policies** — all already KV-backed, all naturally per-tenant once
   the connection is tenant-scoped (whether by account or by user
   permissions).
6. **Connection bootstrap accepts both auth shapes.** The
   `natsclient` package's connection options surface both
   cert-based (`nats.ClientCert`, `nats.RootCAs`) and JWT-based
   (`nats.UserCredentials`, `nats.UserJWT`) auth without the
   framework caring which is in use.

What Track B does **not** do:

- Become an auth issuer (JWT or PKI). The framework consumes
  credentials; products and ops infrastructure issue them. Same
  boundary as ADR-030's identity stance.
- Provide a tenant catalog, billing, or quota enforcement beyond
  what NATS server-side primitives already give. Product concerns.
- Force a choice between mTLS and JWT. The framework supports both
  on the same code path; deployments choose based on existing PKI
  infrastructure, compliance posture, and ops familiarity.
- Eliminate prefix-mode. A `$tenant` substitution path stays
  available for shared-account deployments (dev/staging/prod on
  shared infra, single-org multi-environment).

Coverage delta when Track B ships (on top of Track A):

| ASI | After Track A | After Track B |
|---|---|---|
| 03 Identity & privilege abuse | Weak (forgeable claims) | **Stronger** — tenant claim authenticated at transport (cert SAN or JWT, not body field) |
| 06 Memory poisoning | Medium (in-tenant) | **Strong** — cross-tenant leakage is transport-impossible (server-enforced, not discipline-enforced) |
| 07 Unsafe inter-agent comms | Weak | **Medium** — coordinator → worker handoffs stay tenant-scoped; cross-tenant is explicit |
| 10 Rogue agents | Medium | **Strong** — kill switch is tenant-scoped, blast radius bounded |

### Track C — JetStream cluster as the production substrate

Single-node NATS is acceptable for development and demo; it is not a
production substrate for any deployment that runs accounts mode or
cares about SKG durability. The framework's documented production
topology becomes:

1. **JetStream cluster, 3+ nodes**, replicated streams and KVs at
   R=3. Standard JetStream HA topology.
2. **SKG durability via replication, not backup.** The user-stated
   position: "you don't back up the SKG — you run a proper NATS
   cluster." Snapshot-style backups remain available for
   compliance-driven point-in-time recovery, but they are not the
   primary durability mechanism.
3. **Connection and reconnection semantics audited.** The
   `natsclient` package already handles reconnects; cluster-aware
   defaults (server list, max reconnects, custom reconnect handler)
   become documented production defaults.
4. **Documented operational runbook.** What "a proper cluster" means
   in concrete ops terms: leader election, raft groups for streams,
   storage sizing, observability for replication lag,
   monitoring-relevant metrics. This is a docs deliverable, not
   framework code, but it lives in the repo so version skew is
   visible.
5. **Auth issuer custody documentation.** Whichever path the
   deployment chooses — PKI (step-ca templates, ACME provisioner
   config, signing-key storage, revocation procedures) or NATS JWT
   (operator/account/user chain, NSC tooling, resolver service) —
   the runbook covers it. Light-touch; the framework does not own
   issuance, but acknowledges both paths so ops teams have a
   starting point for either.

Track C is the substrate layer. It does not change framework code in
most components; it changes what semstreams *recommends* and what
docs describe as the production path.

## Phasing

| Phase | Track | Scope | Backward compatible |
|---|---|---|---|
| **0** | — | This ADR. No code. | Yes |
| **1** | A | Five rule-engine extensions, shadow mode, secondary items deferred | Yes |
| **2** | C | Cluster topology docs + reconnect defaults + replication runbook | Yes (docs + defaults; deployments unchanged) |
| **3** | B | Tenant-aware `Dependencies`, account-scoped connections, ADR-030 Phase 2 | **No** — opt-in for products that adopt accounts |
| **4** | — | Coordinated migration with semteams / semspec / semdragon | Per-product |

Order rationale: A first because it's pure additive value with no
ops cost. C second because it is mostly docs and reconnect
defaults, and any deployment adopting B will also want HA
durability — not because B is unsafe without C (it isn't;
NATS-server-enforced isolation works on single-node just as well as
on a cluster). B last because its coordination cost (auth issuer
infrastructure — PKI or JWT — tenant-aware `Dependencies`,
downstream product work in semteams / semspec / semdragon) is the
highest in the programme.

## Consequences

### Positive

- ASI-01, 02, 06, 08, 10 measurably stronger; 06 and 10 move from
  Weak to Strong.
- Cross-tenant data leakage becomes transport-impossible rather than
  discipline-dependent.
- SKG durability story matches industry expectation. Ops teams have
  a documented answer to "how do I back this up."
- Rules-as-policy is a smaller, more coherent addition than embedding
  OPA/Rego — one evaluator, one config shape, one hot-reload path,
  decisions become graph triples.
- ADR-030 Phase 2 gets a real forcing function instead of staying
  deferred indefinitely.

### Negative

- Server-enforced tenant isolation has real ops cost in either auth
  path. JWT-based accounts are operationally painful (framework
  owner has direct experience and explicitly accepts this cost).
  mTLS + step-ca shifts cost into PKI, which most ops teams already
  pay for HTTP/gRPC services — incremental rather than additive,
  but still real. Either way, new users adopting tenancy mode will
  feel it.
- Cluster topology raises the floor on "what production looks like."
  Demo and development paths must remain frictionless or adoption
  suffers.
- Three coordinated tracks means a longer migration window than any
  single track. Coordinated semteams / semspec / semdragon
  deliverables for Track B specifically. The auth-mechanism-agnostic
  framing of Track B keeps each product free to choose its own auth
  path, but means the framework must test both shapes (mTLS and
  JWT).
- Tenant-aware `Dependencies` is a non-trivial refactor surface —
  every component that uses `Dependencies` is touched, even if most
  changes are mechanical.
- Some ASI items (03, 07) remain product-owned. The framework gets
  the seam, not the answer. This is consistent with
  `feedback_framework_boundary.md` but worth restating because
  external readers expect "10/10 OWASP" to mean the framework
  delivers all 10 — it doesn't.

### Things this is not

- **Not a competing product to Microsoft AGT.** AGT is a multi-module
  governance suite over arbitrary agent frameworks. This is a
  programme of additions to *one* framework's existing seams.
- **Not a commitment to embed OPA/Rego.** Considered and rejected
  (rules-as-policy keeps coherence; OPA would mean a second config
  source, second lifecycle, second testing story).
- **Not a commitment to per-tenant NATS clusters.** Considered and
  rejected as default (operationally too costly at our expected
  scale). Remains an available *deployment topology* for
  compliance-driven scenarios.
- **Not a commitment to one auth mechanism.** mTLS + PKI is the
  recommended default because of broader ops familiarity and
  universal-identity composability, but the framework supports
  JWT-based accounts equally; deployments choose based on existing
  infrastructure.
- **Not a tag-able single delivery.** Three tracks, four phases. The
  ADR is the deliverable; implementation lands incrementally.

## Open questions

1. **Bucket naming under accounts.** Identical names per account
   (cleaner code) vs suffixed names (clearer in admin UIs that span
   accounts)? Lean: identical names, but defer until Track B design.
2. **Connection pooling.** One semstreams process per account vs one
   process with a multi-account connection pool? Per-process is
   simpler and matches NATS account boundaries; connection pool is
   denser. Defer until Track B.
3. **Prefix-mode coexistence.** Should `$tenant` substitution stay
   supported indefinitely as a degraded option, or sunset once
   accounts mode is mature? Lean: indefinitely — single-org
   multi-environment is a real use case.
4. **Policy rule type vs convention.** First-class type
   (`"type": "policy"`) vs convention (`"category": "policy"`)? Lean:
   convention to start, promote to type if metrics/operator UX demand
   it.
5. **Where does the auth issuer live?** Out of scope for this ADR
   — by the framework boundary it's product-owned — but ops teams
   will ask. Worth a docs page (Track C deliverable) pointing at
   accepted patterns for both shapes (step-ca + ACME provisioner
   for PKI; NSC + nats-account-resolver for JWT) without endorsing
   one.
6. **mTLS-vs-JWT default in docs.** The framework supports both
   equally; the docs need a recommended default to cut decision
   fatigue for new adopters. Lean: mTLS + step-ca + user-permissions
   recommended for most cases; JWT accounts called out for
   compliance-driven hostile-tenant boundaries. Defer concrete
   wording until Track B implementation surfaces real cases.
7. **Sub-tenant scaling ceiling.** At what scale does
   user-permissions-on-prefix start to creak vs accounts? NATS
   server permission lists are per-user and grow with the number of
   prefixes. Defer until we have real deployment data.
8. **Rule priority semantics.** First-match-wins vs explicit
   `priority` field vs both. Lean: explicit `priority` int with
   documented default. Defer until Track A's `deny` action ships
   and the need becomes concrete.

## References

- [Microsoft Agent Governance Toolkit](https://github.com/microsoft/agent-governance-toolkit)
- [OWASP Agentic Security Initiative — Agentic AI Threats and
  Mitigations](https://genai.owasp.org/resource/agentic-ai-threats-and-mitigations/)
- [ADR-016: Agentic Governance Layer](016-agentic-governance-layer.md)
- [ADR-028: Orchestration Architecture](028-orchestration-architecture.md)
- [ADR-030: HTTP Middleware and Identity Pattern](030-http-middleware-and-identity-pattern.md)
- [ADR-031: Time-Trigger Primitive for Reactive Rules](031-time-trigger-primitive.md)
- `feedback_framework_boundary.md` — framework vs product boundary
- `feedback_approval_bypass_forgery.md` — `ApprovedBy` is forgeable
  over the wire (motivates Track B)
- `feedback_multi_tenant_debt_avoidance.md` — design retrofit-safe
  for multi-tenant

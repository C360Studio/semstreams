# ADR-089: Tool Effect Is a Worst-Effect Claim, and Absent Means Unknown

## Status

Accepted — 2026-07-31. Decision record for the `tool-effect-metadata` change
(gh#749).

This is a **cross-repo contract**: semdev and a second consumer both asked for it
and were told not to hand-roll interim schemas while waiting. Mechanics live in
`openspec/specs/agentic-tools/`; this records only the decision and why the
obvious alternatives were rejected.

## Context

Downstream consumers need to tell a read-only tool from a mutating or externally
effectful one, so their own discovery surfaces and default approval policies can
treat them differently. `agentic.ToolDefinition` carried no such classification.

Two independently invented downstream classification schemes is precisely the
cross-consumer convention reinvention the framework exists to prevent, so both
consumers held and shipped nothing rather than working around the gap. The cost
of that hold is what made this the first post-tag framework item.

The ask arrived with one load-bearing behavior already named: **missing metadata
must never imply read-only.**

## Decision

**1. One enum, and it is a worst-effect claim rather than a taxonomy.**

`agentic.ToolEffect` has four members: `unknown`, `read_only`, `mutating`,
`external_effect`. A tool declares the **most severe** effect it can have.

The strongest argument for orthogonal fields is that a POST to a third party is
both mutating and external. That argument dissolves once the question is stated
correctly: the field does not ask *what does this tool do*, it asks *how bad can
this tool get*. `external_effect` dominates `mutating`, so the third-party POST is
`external_effect`, full stop. This is also the answer to argument-dependence — a
tool whose severity varies with its arguments declares the worst case it admits,
which is why no per-argument classification is needed (and none will be added
absent a real consumer demanding it).

Two booleans were rejected on a mechanical ground as well as a conceptual one:
their JSON zero value collapses *absent* and *false* into one state, which is
exactly the "missing implies safe" failure the issue forbids, recoverable only via
`*bool`. A string enum's zero value is naturally distinguishable from every
declared member.

**2. `unknown` is no claim at all, and is treated as maximally restrictive.**

It is not a middle rung between `read_only` and `mutating`. A consumer mapping
effect onto policy treats `unknown` as at least as restrictive as
`external_effect`. Absence of a classification is not evidence of safety — the
tool counterpart of the framework's standing rule that an absent measurement must
never render as a measurement of absence (ADR-084).

**3. Resolution happens at the framework boundary, not in each consumer.**

Empty and unrecognized values both resolve to `unknown`. The registry normalizes
the definitions it serves, and the discovery response carries the **resolved**
value as an always-present field — `"unknown"` appears explicitly rather than the
key being omitted. A consumer therefore reads a plain string and never
re-implements the fail-safe.

**4. Enforcement is refused at registration; the fail-safe is applied at
aggregation.** Both, because neither alone is total.

An unrecognized spelling in a framework or application executor fails
registration at boot rather than degrading silently. But registration-time
refusal cannot be the guarantee: the registry stores **executors**, not
definitions, and re-invokes `ListTools()` live on every aggregation, so what boot
validated is not necessarily what is served; and the `RegisterTool(name,
executor)` path never inspects a definition at all. Normalization at the
aggregation choke point — which default-tool resolution, per-loop discovery, and
the catalog all read through — is what makes the guarantee hold.

A tool definition arriving on a **task** with an unrecognized value is *not*
rejected. The field is descriptive and has no enforcement consumer; failing a
task over a typo would convert description into an availability hazard. It
resolves to `unknown` at use.

**5. The classification is descriptive. Enforcement is unchanged.**

The configured approval-required name set, the configured allowed-tool name set,
and the per-loop advertised-tool admission check remain the sole authorities over
what executes. A `read_only` declaration does not buy a tool out of a gate an
operator configured; an `external_effect` declaration does not add one. This
increment ships no enforcement wiring at all, and a test asserts that outcome
identity across every effect value in both directions.

When a future capability does derive policy from effect, **the authoritative
value is the registry's definition read at the dispatch seam, never a copy
carried in task or tool-call data** — otherwise a crafted task could downgrade a
declared effect and weaken the control it feeds. Task-carried and
discovery-carried copies are display and discovery grade only. That follow-up
should compile an effect-based config knob down to the existing name set at boot,
rather than adding a second runtime gate.

**6. Effect does not cross the provider wire.**

No provider function schema has a slot for it, and the model is not a party this
classification is for: it has no legitimate decision to make with it, and by this
design's own terms must not be trusted to act on it, since enforcement is
framework-side. Sending it would manufacture the appearance of a control.
`Paginated` is the precedent for framework-informational metadata that stops at
the loop. A product that wants the model to know a tool is dangerous says so in
persona prose — a product decision, not a wire contract.

**7. Metered external reads are `read_only`; mediation does not launder effect.**

Two cases the four definitions did not decide, and adopters hit both immediately.

*Metered reads.* A query against a paid search or data API consumes quota. Quota
consumption is a **cost**, not an effect on the world, so a metered external read
stays `read_only`. "Spend" in the `external_effect` sense means an irrevocable
commercial action the tool initiates — an order, a transfer, a booking.

*Transitivity.* A tool that writes a rule or deploys a flow is `mutating`, even
when the deployed flow later performs an outbound POST. The tool's own effect is
the configuration write; the outbound action belongs to the deployed component
and is classified where that component is described. `bash` is `external_effect`
not because it is mediated but because the command it runs can reach anything
directly. Without this rule every configuration tool collapses to
`external_effect` and the enum stops discriminating.

**8. The enum is open for extension.**

Consumers must not switch exhaustively without a default arm resolving to
`unknown`, so a later member lands additively without a coordinated release.

## Consequences

- Additive and non-breaking. `omitempty` on the canonical field keeps existing
  wire bytes byte-identical for undeclared tools; the discovery DTO gains one
  field, which existing readers ignore. No sister lockstep.
- The name `read_only` is deliberately reused from
  `agentic.FilesystemPolicyReadOnly` (ADR-052/ADR-067), which is a task-scoped
  filesystem **write scope**, not a tool classification. The two are orthogonal
  and legitimately disagree: a tool may be `external_effect` while executing
  under filesystem policy `read_only`. Enum values are scoped by the field that
  carries them; the Go constants (`ToolEffectReadOnly` vs
  `FilesystemPolicyReadOnly`) do not collide. Namespacing the value would have
  bought nothing except divergence from the spelling both consumers were told to
  wait for.
- Every framework-owned tool is classified at birth, enforced by a source-level
  check that scans every in-repo package registering into the shared executor
  registry — `processor/agentic-tools{,/executors}` and
  `frameworkcapabilities/graphresearch`. An all-`unknown` framework catalog would
  have poisoned the deferred approval-defaulting work before it started. The
  check validates the VALUE, not merely the presence of the field: 10 of the 16
  builtin registration sites use `RegisterTool(name, executor)`, which never
  inspects a definition, so boot-time enum refusal structurally cannot see their
  typos.
- Application-supplied executors outside those packages are not bound by that
  check; the fail-safe covers them — an undeclared effect resolves to
  `unknown`, never to `read_only`.
- Two definitions of the same tool NAME may legitimately carry different effects
  when they are different implementations. `web_search` is the shipped case: the
  Brave-backed executor is `mutating` because it writes observation triples to
  the graph, while the stub emits nothing and is `read_only`. Discovery reports
  whichever implementation the deployment registered.

## Non-goals

- **Per-argument or dynamic effect classification.** Worst-effect semantics is
  the answer to argument-dependence. Out of scope unless a real consumer
  demonstrates a need.
- **Effect-derived enforcement in this increment.** Deliberately deferred with
  its constraint recorded above.
- **Serving canonical tool definitions over discovery.** The discovery response
  stays a narrow projection; shipping full parameter schemas over `tool.list` is
  a payload and surface decision nobody has asked for.

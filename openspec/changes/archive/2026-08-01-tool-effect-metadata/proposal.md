## Why

Two sister repos (semdev and one further consumer) need to distinguish a
read-only tool from a mutating or externally-effectful one in their own
discovery and approval-defaulting layers, and `agentic.ToolDefinition` carries
no such classification today. Both were told on gh#749 **not** to hand-roll an
interim effect enum, because two independently-invented downstream
classification schemes is exactly the cross-consumer convention reinvention the
framework exists to prevent — so they are blocked rather than working around it.

The load-bearing behavior the ask names is a fail-safe one: **missing metadata
must never imply read-only.** That is the same invariant `agentic-loop` already
specifies for in-flight answers ("an absent measurement must never render as a
measurement of absence"), applied to tool classification: an undeclared tool is
*unclassified*, not *safe*.

The classification is **descriptive**. semstreams' existing approval enforcement
— the `approval_required` name set consulted by `ApprovalFilter`, the
`allowed_tools` set, and the per-loop advertise-and-enforce admission via
`agentic.MetadataKeyAdvertisedTools` — remains the sole authority over what
actually executes. This change adds a field and its fail-safe resolution rules;
it deliberately wires **no** enforcement.

## What Changes

- `agentic.ToolDefinition` gains `Effect ToolEffect` (`json:"effect,omitempty"`),
  a new single string enum with four values: `unknown` (fail-safe default),
  `read_only`, `mutating`, `external_effect`.
- The enum's semantics are **worst-effect**, not a taxonomy: a tool is
  classified by the most severe effect it can have, so `external_effect`
  dominates `mutating` and the "an external POST is both" objection does not
  arise. `unknown` is **no claim** — not a middle rung — and policy consumers
  must treat it as at least as restrictive as `external_effect`.
- Two resolution helpers on the enum: `Known() bool` (does this value name a
  declared member) and `Canonical() ToolEffect` (empty or unrecognized →
  `ToolEffectUnknown`).
- **Registration rejects** an unknown non-empty `Effect`: `ExecutorRegistry.
  RegisterExecutor` fails registration in its existing validate-then-commit
  first pass, alongside the empty-`Name` check. Empty stays legal (undeclared).
- **Aggregation normalizes**: `ExecutorRegistry.ListTools()` returns copies
  whose `Effect` is `Canonical()`. This is the load-bearing fail-safe rather
  than belt-and-suspenders, because the registry stores *executors* and
  re-invokes `executor.ListTools()` live on every aggregation — so a definition
  served downstream need not be one boot validated — and because
  `RegisterTool(name, executor)` never sees a `ToolDefinition` at all.
- **Discovery carries the resolved value**: `agentictools.ToolDefinition` (the
  `tool.list` response DTO) gains `Effect string` with `json:"effect"` and **no**
  `omitempty`, always populated via `Canonical()`, so `"unknown"` appears
  explicitly. Consumers never re-implement the absent-means-unknown rule.
- A **structural decision test** over the DTO projection: every field of
  `agentic.ToolDefinition` must appear in an explicit projected-or-dropped
  allowlist, so a future canonical field cannot be silently dropped the way
  `Strict` and `Paginated` already were.
- All in-repo tool producers are classified. `ToolDefinition.Validate()` gains
  the same enum check so the method stays truthful.

Not breaking. Purely additive; `omitempty` keeps the canonical wire bytes
identical for undeclared tools. The `tool.list` DTO gains one field, which is an
additive JSON change for existing readers.

## Capabilities

### New Capabilities

- `agentic-tools`: the framework's tool contract — what a tool definition
  declares about itself, how an undeclared or unrecognized declaration resolves,
  where an invalid declaration is refused, and the boundary between descriptive
  metadata and the authoritative name-based execution controls.

## Impact

- **Code**: `agentic/tools.go` (enum, field, helpers, `Validate`);
  `processor/agentic-tools/executor.go` (`RegisterExecutor` rejection,
  `ListTools` normalization); `processor/agentic-tools/external.go` (DTO field);
  `processor/agentic-tools/component.go` (`Component.ListTools` projection);
  the ~22 executor `ListTools()` implementations under
  `processor/agentic-tools/` and `processor/agentic-tools/executors/`
  (classification only).
- **Not touched, deliberately**: `processor/agentic-model/client_wire.go` and
  `translate_responses.go` — effect does not cross the provider wire. No
  provider schema has a slot for it, the model has no legitimate decision to
  make with it, and enforcement is framework-side by this design's own terms;
  sending it would manufacture the appearance of a control. `Paginated` is the
  precedent for a framework-informational field that does not cross.
- **Consumers**: semdev and the second gh#749 consumer adopt
  `ToolDefinition.Effect` / the `tool.list` `effect` field. No sister lockstep —
  additive with a fail-safe default, so an unadapted consumer keeps compiling.
- **Decision record**: one ADR, because the values, the worst-effect semantics,
  the fail-safe-unknown rule, the descriptive-not-enforcement boundary, the
  open-for-extension clause, and the does-not-cross-the-provider-wire non-goal
  are a cross-repo contract.
- **Issues**: closes gh#749. Files two named follow-ups: effect-derived default
  approval policy (with the registry-not-wire constraint recorded), and wiring
  `Validate()` into `RegisterExecutor` (which would broaden the boot gate to
  require `Parameters` — a separate decision).

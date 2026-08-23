## Why

**The payload registry gives every component polymorphic typed payloads everywhere in a flow —
except the one place that reads messages to make decisions.** Rule evaluation reads message data
only from `*message.GenericJSONPayload`, so a payload that is correctly registered, correctly
enveloped, and correctly decoded is then discarded because it is not the one type the engine knows
how to read.

The failure is silent at every layer. `processor/rule/expression_factory.go:130-135` asserts to
`*GenericJSONPayload`, gets nil, and returns `false`. Nothing distinguishes "this condition did not
match" from "this payload was structurally unreadable."

### It is not hypothetical — a shipped rule has never fired

`configs/rules/agentic-workflow/architect-editor.json` is `enabled: true`. It conditions on
`$message.role == "architect"` and `$message.outcome == "success"`, then spawns an editor agent.
Those field names match `LoopCompletedEvent` exactly (`agentic/events.go:63-64`).

`agent.complete.*` carries `LoopCompletedEvent` — a registered typed payload
(`agentic/events.go:101`, its own `MarshalJSON`/`UnmarshalJSON`). So the payload is never readable,
the condition is never true, and the architect→editor handoff has never happened. No error, no
warning, no metric.

### The framework has been paying the workaround visibly

`f8a798f5` ("approve verdict uses payload registry (discipline)") correctly wrapped a publish in a
`BaseMessage` — and chose `core.json.v1` `GenericJSONPayload`, the one payload type carrying no
domain type information, because that is the only shape a rule can read. The consumer then converts
it back: `verdictPayloadFromMap` (`processor/agentic-loop/component.go:2333`) *"translates a
GenericJSONPayload.Data map into"* a typed `VerdictPayload`.

So the round trip is **typed intent → untyped map → typed struct**, with the type erased in the
middle purely to survive rule evaluation. A commit whose subject is "uses payload registry" produces
the payload that defeats the registry.

### Why an interface, and why explicit

`message/behaviors.go` already establishes the pattern: *"PURE structural behavioral interfaces
that payloads can optionally implement... discovered at runtime through type assertions."* Ten ship
today. Rule-readability is the missing member of that family, and its absence is why the rule lane
is the only consumer that demands a concrete type instead of discovering a capability.

Projection is explicit rather than reflective. Reflection in the evaluation hot path is the wrong
trade, and — more importantly — a reflective default would make payloads rule-readable *by
accident*, exposing fields their author never intended a rule to match on. That is the ADR-036
concern, and it is live: `Violation.OriginalContent` carries up to 500 characters of raw user text.

## What Changes

- **`message.RuleReadable`** — a new optional behavior interface in `message/behaviors.go`
  alongside its ten siblings:

  ```go
  type RuleReadable interface {
      RuleFields() map[string]any
  }
  ```

- **One projection helper** replacing three hand-rolled type switches
  (`expression_factory.go:130`, `message_handler.go:445`, `message_handler.go:412`): assert
  `RuleReadable`, fall back to `GenericJSONPayload.Data`, otherwise report unreadable. One home for
  interpreting a shared type.

- **An unreadable payload becomes observable.** A rule with `$message.*` conditions whose payload
  implements neither surface currently evaluates to `false` forever in silence. It must surface —
  once per rule/type pair, not per message.

- **The 15 agentic payloads plus `GenericJSONPayload` implement it**, not a lazy subset. (These
  are not every framework-owned registered type — the registry holds 21; the five residuals
  are recorded in `tasks.md` 8.5 and now fail loudly rather than silently.) `Locatable` and `Timeable`
  are adopter-owned: a product implements them on its own struct. The agentic payloads are
  framework-owned, so an adopter who wants a rule on `ToolCall` cannot add a method — they file a
  framework PR and wait for a release. Every payload skipped today is that PR tomorrow.

- **Content stays out.** `RuleFields()` exposes structural facts — role, outcome, ids, counts,
  states — and withholds LLM-authored and user content, per ADR-036 and the rule-opaque discipline
  the graph plane already enforces. `UserMessage` text, `AgentResponse` output, `ToolResult` bodies
  and `AgentRequest` prompts are the cases that matter. Deciding this once, consistently, is the
  reason the interface is explicit rather than reflective.

## Impact

- **Affected specs:** `rule-engine` (the payload-readability seam and the observable miss).
- **Affected code:** `message/` (interface + `GenericJSONPayload` implementation), `agentic/` (15
  payload implementations), `processor/rule/` (helper, three call sites, observability).
- **Not breaking.** Additive interface; a payload that does not implement it behaves exactly as
  today. `GenericJSONPayload.RuleFields()` returns `Data`, so every rule that works today keeps
  working.
- **Fixes by consequence:** the dead `architect-editor` rule, and the first barrier of #1045.
- **Not in scope:** the governance verdict payload itself (#1045 — depends on this landing first);
  retiring the `verdictPayloadFromMap` type-erasure round trip; extending rule-opacity enforcement
  to `$message.*` paths and action templates (a separate `predicate-contract` question).

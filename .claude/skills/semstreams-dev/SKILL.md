---
name: semstreams-dev
description: Opinionated front door for building in semstreams — components, ports, payloads, and the conventions that aren't enforced by the compiler. Use when adding or changing a component (input/processor/output/storage/gateway), choosing a port, or wiring a new data flow, and you want to do it the house way.
argument-hint: [what you're building, e.g. "a UDP input" or "an outbound poller"]
---

# Building in semstreams — the house way

## What are you building?

$ARGUMENTS

Route to the right section / sibling skill, then come back and run the **Finish checklist** at the bottom — it catches the silent footguns (unregistered binary, schema drift, missing e2e).

| You're… | Go to |
|---|---|
| Adding/changing a component (input, processor, output, storage, gateway) | **§1 Component anatomy** + **§2 Port picker** below |
| Choosing how two components talk (KV watch vs JetStream vs pub/sub) | `/kv-or-stream`, then **§2** to declare the port |
| Adding a new message/payload type | `/new-payload` (registration checklist) |
| Deciding rule vs component vs something else | `/orchestration-check` |
| Exposing graph queries (admitted HTTP / named typed adapter; no MCP) | `/query-pattern` |

Core mental model (CLAUDE.md): semstreams is a **knowledge-graph engine**, not an event bus. The
write IS the event. Components **execute work**; they don't know their caller. Rules **trigger**;
they don't do work inline. State ownership is **exclusive**.

---

## §1 Component anatomy

A component is a self-describing unit discovered at runtime. **Five** registered types
(`RegistrationConfig.Type`): `input` (source), `processor` (transform), `output` (sink),
`storage` (persist), `gateway` (query surface). Note `component/doc.go` still says "four" — it
predates `gateway`; trust the code (`feedback_capabilities_from_code_not_docs`). Typical package
layout:

```
processor/your-thing/
  config.go            # Config struct with schema tags (drives validation + discovery + CI schema gen)
  your_thing.go        # implements Discoverable (+ LifecycleComponent if it runs)
  register.go          # exports Register(*component.Registry) error
  payload_registry.go  # ONLY if it emits new payload types — see /new-payload
  *_test.go
```

### The two interfaces

**`Discoverable` (required)** — what makes it a component (`component/discovery.go`):

```go
Meta() Metadata             // name, type, version, domain
InputPorts() []Port         // ports it accepts data on
OutputPorts() []Port        // ports it produces data on
ConfigSchema() ConfigSchema // generated from your Config struct's schema tags
Health() HealthStatus
DataFlow() FlowMetrics      // messages/bytes counters
```

**`LifecycleComponent` (optional, type-asserted at runtime)** — implement it if the component
*runs* (watches, listens, ticks) rather than being a pure transform (`component/lifecycle.go`):

```go
Start(ctx context.Context) error   // ctx is per-component; honor cancellation
Stop(timeout time.Duration) error  // graceful shutdown within timeout
```

Discipline: an owning `Start` or `Run` may derive a lifecycle child context locally. Thread the exact received or
derived operation context directly into every goroutine, callback, and helper you spawn. Component work derives from
`Start` or `Run`, and spawned tasks join `Stop`. Context is the first argument to every operation that needs it; nil
is rejected, never defaulted.

**HARD RULE:** production structs SHALL NOT retain `context.Context`, including through embedding, aliases, wrappers,
getters, providers, or knobs. Roots exist only at process composition. Constructors, factories, callbacks, watchers,
and goroutines do not invent them with `context.Background`, `context.TODO`, or `context.WithoutCancel`. Use
context-aware variants for blocking or cancelable operations when available. `http.Server.BaseContext` may inject
lifecycle only where the server is composed and its closure captures the exact `Start` context; generic repository
context getters and providers remain prohibited. Callers never pass nil; exported boundaries reject it when able to
return an error, private helpers rely on the invariant, and nothing defaults nil to `context.Background`.

Detach only terminal cleanup or finalization, or already-accepted durability work whose invariant requires bounded
completion. `context.WithTimeout` is the immediate boundary. With a parent, use
`context.WithTimeout(context.WithoutCancel(parent), budget)`; a timeout-only `Stop` or equivalent finalizer with no
parent contract may use `context.WithTimeout(context.Background(), budget)`. Complete synchronously or join all tasks
before return, and never feed `Start`, `Run`, `Watch`, or continuing work. Do not use `WithoutCancel` directly or
create an unbounded descendant; nested `WithCancel`, `WithCancelCause`, or other child cancellation beneath the
bounded context is allowed only when all tasks join. Only the lifecycle owner may retain a private, synchronized
`context.CancelFunc`; exported lifecycle records do not expose it. Stop and read
`.agents/contracts/semstreams-developer.md` fully if a change would violate these rules.

### The factory + explicit registration (the #1 footgun)

semstreams uses **EXPLICIT registration, NOT `init()` self-registration** (`component/doc.go`).
Your package exports a `Register`:

```go
func Register(r *component.Registry) error {
    return r.RegisterWithConfig(component.RegistrationConfig{
        Name:        "your-thing",
        Factory:     func(raw json.RawMessage, deps component.Dependencies) (component.Discoverable, error) { ... },
        // REQUIRED (ADR-100): a pure port declarer — the ports the Factory will report for raw,
        // with no deps. Derive it from the same parse+resolve the Factory uses; boot admission
        // compares declaration and constructed ports and refuses on any difference, and
        // RegisterFactory refuses a nil Ports outright.
        Ports:       func(raw json.RawMessage, instance string) (component.PortConfig, error) { ... },
        Schema:      yourSchema,         // from ConfigSchema()
        Type:        "processor",        // input | processor | output | storage
        Protocol:    "...",              // udp, websocket, file, …
        Domain:      "...",              // network, storage, processing, semantic, robotics
        Version:     "v1",
        Dependencies: []string{ /* component.DepModelRegistry, … */ },
    })
}
```

> **HARD RULE — wire it into EVERY framework binary, not just one.** Components register through
> `componentregistry.RegisterAll`; the critical pair is the production + e2e binaries —
> `cmd/semstreams/main.go` **AND** `cmd/e2e-semstreams/main.go` (example binaries like
> `cmd/examples/github-pr-workflow` carry their own registration; services live in
> `service/register.go`). A component registered in `e2e-semstreams` but not `semstreams` (or
> vice-versa) is the exact "half-migrated binary" failure class behind the **breaking-change e2e
> rule** (CLAUDE.md; `feedback_e2e_required_for_breaking_changes`). After adding registration, grep
> every binary: `grep -rn "your-thing\|RegisterAll" cmd/`.

### Config via schema tags (feeds the CI schema gate)

Config fields are described with `schema:` struct tags — they drive validation, the operator
discovery surface, AND the generated JSON schema. Example (`processor/agentic-dispatch/config.go`):

```go
type Config struct {
    DefaultRole string `json:"default_role" schema:"type:string,description:Default role for new tasks,default:general,category:basic,required"`
    AutoContinue bool  `json:"auto_continue" schema:"type:bool,description:Continue last active loop,default:true,category:basic"`
    StreamName  string `json:"stream_name"  schema:"type:string,description:NATS stream name,default:USER,category:advanced"`
}
```

Directives: `type:` · `description:` · `default:` · `category:basic|advanced` · bare `required`.
Every operator-reachable field needs a JSON-round-trip test (no shadow structs) —
`feedback_polymorphic_config_needs_json_roundtrip_test`. **Any config change → run
`task schema:generate` and commit the `schemas/`+`specs/` diff, or CI fails** (see Finish checklist).

---

## §2 Port picker

Ports are typed I/O dependencies (`component/port_*.go`). A `Port` has `Name`, `Direction`
(`input`/`output`), `Required`, `Description`, and a `Config` implementing `Portable`
(`ResourceID()` for conflict detection, `IsExclusive()`, `Type()`). Pick by what the data IS:

| Need | Port | Notes / opinion |
|---|---|---|
| Declared internal state dependency | **KVWatchPort** | Owner-only; bootstrap hydrates inputs |
| Durable request/work | **JetStreamPort** | At-least-once; unacked work may redeliver |
| Fire-and-forget **pub/sub** (no durability) | **NATSPort** | Rare; most "events" should be a KV write instead. |
| Bind a raw **TCP/UDP socket** | **NetworkPort** | Ingress inputs (e.g. `input/udp`). |
| **Outbound HTTP** client / polling input | **HTTPClientPort** | **Descriptor-not-runtime; secrets-as-refs** (ADR, beta.114). No live client in the descriptor. |
| **Filesystem** read/write | **FilePort** | File input/output components. |
| Stream-read **bulky content** from ObjectStore | **StoreReadPort** | Pairs with `ContentStorable` + ref-triples on the owning entity. |
| **Periodic** tick / scheduled trigger | **TimerPort** | Tick-driven components. |

Rules of thumb:
- There is no general typed reactive semantic subscription. A measured consumer
  may propose a named typed operation with adopter/surface reduction evidence;
  raw KV watches are owner/dependency or operator seams, not a fallback.
- **Facts → KV, requests → JetStream.** If you're reaching for NATSPort pub/sub, re-check
  `/kv-or-stream` — the write usually *is* the event.
- **Bulky payloads never ride rules or messages** — store via `ContentStorable`/ObjectStore and pass
  a ref (ADR-028; `docs/concepts/14-orchestration-layers.md`).
- Set `ResourceID()` so two components contending for the same socket/bucket/stream are caught at
  wiring time, and `IsExclusive()` truthfully.

---

## Finish checklist (run before you call it done)

1. **Registered in every binary?** `grep -rn "your-thing" cmd/` — present in `cmd/semstreams` AND
   `cmd/e2e-semstreams` (+ others if relevant). Half-wired = silent flow break.
2. **Schema regenerated?** `task schema:generate` then `git diff schemas/ specs/` is **empty**
   (commit it if not) — this is a CI gate.
3. **New payload types registered?** `/new-payload` (init() + `MarshalJSON` wrapping `BaseMessage`
   + blank import). Every NATS publish wraps in `BaseMessage` — `feedback_nats_publishes_use_payload_registry`.
4. **NATS request callers checked?** If you call `natsclient.Request`, handler errors arrive as a
   `{message,detail}` body (post beta.115 / ADR-060), NOT the `err` return — use
   `RequestClassified` for classified handlers or you silently decode an error as success (gh#337).
5. **Config round-trip tested?** Every operator-reachable field.
6. **Gates green:** `task build` · `task lint` (exit 0, revive warnings = fail) · `go test -race ./...`
   · `go test -race -tags=integration ./...` · `go test ./test/contract/...`.
7. **Breaking change?** At least one relevant **e2e tier green BEFORE it lands on main** (HARD RULE,
   CLAUDE.md). Pre-tag also: `go vet -tags=integration` AND `-tags=live_llm`.

Sibling skills: `/kv-or-stream` · `/new-payload` · `/orchestration-check` · `/query-pattern`.

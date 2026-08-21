# Design: Caller-owned lifecycle contexts

## Landed decisions

### Context-bearing boundaries

Component and service `Stop` operations accept `context.Context`. `Manager.StopAll` forwards the exact caller context
in reverse registration order, attempts every service, and aggregates genuine failures. There is no duration adapter
or deprecated overload.

### Lexical runtime authority

Production lifecycle structs do not retain `context.Context`, a wrapper containing one, or a provider that recovers
one later. Owners retain only private synchronized cancellation and join state. Exported lifecycle records do not
expose cancellation authority.

### Nil handling

Core lifecycle boundaries that can return an error reject nil context before inspecting or mutating lifecycle state.

## Explicitly separate debt

This change does not claim repository-wide removal of every `context.Background`, `context.TODO`, or
`context.WithoutCancel` call. It does not define NATS Client shutdown, native drain ordering, failed-Start cleanup,
callback borrowing, settlement, restart proof, Registry, or boot composition. Those require independently bounded
issues if current evidence justifies changing them.

## Verification

The landed migration has compiler-directed API coverage, focused race and integration tests, a type-aware production
context-ownership contract guard, schema no-drift evidence, core and semantic E2E evidence, and independent review.

# Foundation B completed-tree release evidence

## Scope

This record captures the final local validation of the Foundation B implementation working tree on 2026-08-07. The
tree was based on `4d3ea2ff5db69b40840c51ef76a3e2f730edef62`; task 5.9 remains responsible for recording the
eventual merged commit and baseline. This is command/result evidence, not a claim that local output replaces required
CI.

## Static and unit gates

| Command | Result |
|---|---|
| `task lint` | Pass: vet, formatting, revive, fixed-port guard, request guard |
| `task build` | Pass: `cmd/semstreams` built successfully |
| `go vet -tags=integration ./...` | Pass |
| `go vet -tags=live_llm ./...` | Pass |
| `go test -race ./...` | Pass |

## Integration gate

`task test:integration` passed with the repository runner's integration build tag, race detector, fail-fast behavior,
20-minute package timeout, and uncapped Go package parallelism. The final run completed every listed package. In
particular, `natsclient` completed in 116.724 seconds rather than timing out, and the engine, UDP, component, service,
processor, storage, contract, and test-infrastructure packages all passed.

The final run supersedes two earlier failing discovery runs. Those runs exposed and led to correction of one engine
fixture without explicit `INPUT` retention bounds, stale UDP plain-NATS test declarations after the JetStream cutover,
and a five-second shared Docker cleanup ceiling that failed only under parallel teardown pressure.

## Generated artifacts and contracts

`task schema:generate` passed. The intentionally changed generated artifacts were byte-identical before and after the
generator ran:

| Artifact | SHA-256 after both observations |
|---|---|
| `schemas/agentic-loop.v1.json` | `870a28333fc6b4ecda630de0561ffbaac5b893bf3644e7de431c660a985d13bc` |
| `specs/openapi.v3.yaml` | `11fd3e435f986c716c280718784595c410dd30d568a275a8e39d50938c577879` |

`go test ./test/contract/...` passed. `task openspec:validate` passed 35 items with zero failures.

## Breaking end-to-end gates

`task e2e:all` passed its core, structural, statistical, semantic, and agentic executions. The semantic execution
completed all 48 stages. The agentic execution observed ten strict reference-only trajectory facts and a terminal
observation. `task e2e:research-graph` also passed. After the final message-logger out-of-order ring correction,
`task e2e:core` passed health, dataflow, and graph round-trip again with exactly two graph trace entries.

## Frozen response-bound inputs

The two owner-approved request/reply artifacts retained their exact accepted identities after implementation:

- `request-reply-response-bounds-inventory.md`: 344 lines, 22,788 bytes, SHA-256
  `26ea5b020e1f292ee646dfd45115bf753e0ac392493a6d672e5743c2336e182e`.
- `request-reply-response-bounds-design.md`: 425 lines, 21,033 bytes, SHA-256
  `e71bd4f2e0e8ef24440c2632721bb939a2d24ad9344e6c95aea50887d93c1015`.

The design file's own status paragraph records its state when it was presented for review. The later owner approval in
`openspec/changes/foundation-b-port-language/approval.md` supersedes that historical prose without mutating the frozen
artifact.

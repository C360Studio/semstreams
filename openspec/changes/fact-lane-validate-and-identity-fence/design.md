# Design: validate, then fence, on the Graphable lane

Base for every pin: `1b1accf4ea4ea878c26236b5a9e6cb83d2d89d7a` (`origin/main`, 2026-09-30).

## Premises

- P1 `processor/graph-ingest/component.go:1674-1684` — `decodeEntity` runs `c.decoder.Decode(data)` then
  `extractEntityFromMessage(baseMsg)`. No `Validate()` call, no `recover()`. It is the one entry both the consume
  closure (`:1558-1569`, decode failure = poison: `c.errors`++, WARN, ack) and the synchronous `handleMessage`
  (`:1653-1668`) share.
- P2 `message/decoder.go:39-50` — `Decode` is `json.Unmarshal` into a `BaseMessage`; `message/base_message.go:261-`
  `UnmarshalJSON` creates the typed payload through the registry and never calls `Validate()`.
  `message/base_message.go:222-225` — `MarshalJSON` calls `m.Validate()` before serializing: the producer half of the
  contract exists; the consumer half does not.
- P3 `processor/graph-ingest/component.go:1798-1846` — `extractEntityFromMessage` calls `graphable.EntityID()`
  (`:1805`), rejects an empty ID (`:1806-1808`), then `graphable.Triples()` (`:1810`), then the optional
  `StorageRef()` (`:1829`) and `IndexingProfile()` (`:1844`). Every call is on the decoded payload, unguarded.
- P4 `agentic/loop_execution_entity.go:87-89` — `EntityID()` returns `LoopExecutionEntityID(...)`, which panics
  (`agentic/entity_ids.go:71-77`). `:129-132` and `:143-146` — `Triples()` does the same for `Task.ParentLoopID` and
  `Task.InReplyTo`. `:136-139` — the `Task.RunID` branch in the same function already uses
  `TryChainExecutionEntityID` and skips the triple on error. `:179-190` — `Validate()` uses the `Try` form for the
  loop ID, then calls `e.Triples()`, so it panics today on a malformed parent or reply-to.
- P5 The four sibling registered Graphable types return `""` with a line-pinned `entity-id-audit:classify
  intentional-sentinel` annotation: `agentic/agent_lesson_entity.go:218-225`, `agentic/model_endpoint_entity.go:55-62`,
  `agentic/ops_diagnosis_entity.go:85-92`, `agentic/web_observation_entity.go:89-96`.
- P6 `agentic/payload_registry.go:52` registers `LoopExecutionEntity` (ADR-103, #1109), so the decoder reaches P4.
- P7 Sweep, 2026-09-30, every non-test `EntityID() string` and `Triples() []message.Triple` body in the tree
  (`grep -rn -A14 -E 'func \(.*\) (EntityID\(\) string|Triples\(\) \[\]message\.Triple) \{' --include='*.go' .`
  filtered for `panic(` and the three panicking constructors): the only hits are P4's three lines. The eight other
  callers of the panicking constructors (`processor/agentic-loop/graph_writer.go:250,290,316,514,724`,
  `processor/agentic-loop/handlers.go:322`, `test/e2e/scenarios/agentic/scenario.go:966,1017`) are producers acting
  on inputs they own; the issue's boundary (O-2 export ruling on #1109) leaves them alone.
- P8 `openspec/specs/payload-registry/spec.md:205` — "A registered payload's `Validate()` is the writer's full
  contract". `openspec/specs/graph-ingest/spec.md:232` — the structural gate rejects invalid IDs and predicates
  after extraction; nothing there covers a payload that cannot be extracted.

## Decisions

- D1 **Validate at the lane's entry.** `decodeEntity` calls `baseMsg.Validate()` between P1's two calls. Failure is
  a classified error (`errs.WrapInvalid(err, "graph-ingest", "decodeEntity", "payload validation failed")`) on P1's
  existing poison path. This is the consumer half of P2: what `MarshalJSON` refused to emit, the lane refuses to
  ingest.
- D2 **One fence, at the consumer.** `decodeEntity` recovers a panic raised during `extractEntityFromMessage` and
  returns a classified error carrying the message type and the recovered value; the goroutine survives and the
  message follows the poison path. The fence covers P3's four payload calls in one place and covers product-registered
  types the tree cannot see. It does not wrap `Decode` itself (JSON decoding does not run payload code) and does not
  reach past extraction (the merge path runs framework code on already-extracted data).
- D3 **`LoopExecutionEntity` takes the sibling shape (P5).** `EntityID()` uses `TryLoopExecutionEntityID` and returns
  `""` on error, with the sentinel annotation `go run ./cmd/entity-id-audit .` requires (annotations are line-pinned:
  regenerate the `line=` after the edit). `Triples()` uses the `Try` form for parent and reply-to and omits the triple
  on error, matching its own run branch (P4 `:136-139`). `Validate()` keeps its text; it no longer panics because
  `Triples()` cannot. The producer-side writer still validates before birthing the entity, so a malformed parent or
  reply-to is refused at publish and, if it arrives anyway, rejected at ingest with an empty ID or a validation error.
- D4 **No new metric.** The Graphable lane accounts decode failures in `c.errors` plus a WARN (P1); D1 and D2 land on
  that path unchanged. A reason-labelled counter for this lane (the mutation lane has `mutation_rejections_total`)
  is an observability ask this fix does not ratchet in; recorded here as the residual, not filed.
- D5 **The doc sentence.** `.agents/skills/new-payload/SKILL.md` and `docs/concepts/15-payload-registry.md` each
  state once: a registered Graphable's `EntityID()` and `Triples()` never panic on decoded input; return `""` or omit
  the triple and let graph-ingest reject.

## Rejected

- R1 Validate inside `Decoder.Decode` (`message/`). Every decoder consumer (rule engine, outputs, gateway, tests)
  would change in one move; the write boundary the issue names is the Graphable lane, and P2 shows the producer half
  is already a `BaseMessage` concern the consumer can mirror locally. Widening is an owner question, not this fix.
- R2 `recover()` inside each registered type's methods. Five in-tree sites plus every product type; the fence at the
  consumer (D2) covers all of them once.
- R3 Make the exported `LoopExecutionEntityID` non-panicking. Exported contract change with eight producer callers
  (P7) outside #1112's boundary.
- R4 Fence-only, no `Validate()`. Leaves the consumer half of P2 missing; the issue names both.

## Declared cost

- One `Validate()` per Graphable arrival. For `LoopExecutionEntity` that builds `Triples()` once more than today;
  microseconds against a KV round trip.
- Behavior change on the lane: a decoded payload failing its own `Validate()` is dropped as poison instead of
  persisted. In-tree producers publish through `MarshalJSON` (P2) and are unaffected. The migration note carries the
  one check a sister needs: does any producer hand-write wire JSON for a registered Graphable type?
- Breaking gate: a relevant e2e tier green at the final revision (`task e2e:core`; the tier ingests through this
  lane), Docker window announced to live sister sessions first.

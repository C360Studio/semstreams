## Why

The graph's two dotted-token namespaces — **6-part entity IDs**
(`org.platform.domain.system.type.instance`) and **3-part predicates**
(`domain.category.property`) — are foundational: they are split, prefix-matched, wildcard-subscribed,
and embedded into NATS KV keys, and every consumer *assumes* the structure holds. But enforcement is
**asymmetric and incomplete**:

- `message.IsValidEntityID` (`message/triple.go:138`) is rigorous — exactly 6 non-empty parts, KV-safe
  charset — and is wired into ~10 call sites (graph-index INCOMING/CONTEXT/NAME indexes, graph-query,
  rule engine).
- `vocabulary.IsValidPredicate` (`vocabulary/predicates.go:458`) only counts dots (`== 2`), does **not**
  check for empty segments or charset, and has **zero callers** — it is dead code.

Nothing rejects a non-conforming **predicate** at any write boundary (graph-ingest, `message.Triple`
creation, `vocabulary.Register`, rule `add_triple`, or an LLM-authored predicate). A single malformed
predicate (a sister-repo product, a careless rule stamp, an agent-authored triple) silently corrupts
positional consumers that read the predicate's parts (e.g. `test/e2e/client/nats.go` reads
`parts[1]`/`parts[2]` to parse an inference band), the gh#474 composite reverse-index KV keys that embed
the predicate as a whole token, and the `domain.category.*` wildcard-ability the vocabulary is built on
(`docs/basics/04-vocabulary.md`) — with no gate to stop it and consumers that mis-split rather than
reject.

Every first-party token conforms today, so this is a **latent foundational gap**, not active breakage —
but for tokens that key into KV and drive deterministic splits, an unenforced core invariant is a
blocker. It also blocks `enforce`-free disambiguation in gh#519 (the `.value` suffix can only be
distinguished from a real `*.value` predicate if 3-part structure is guaranteed).

## What Changes

- **Harden the predicate validator.** `IsValidPredicate` gains the same rigor as `IsValidEntityID`:
  exactly 3 dot-separated parts, each non-empty, each a safe token — a single source of truth for the
  predicate contract. Add a symmetric typed validator surface so predicates and entity IDs are validated
  the same way.
- **Enforce at the write boundary (fail-closed, log loudly).** graph-ingest — the sole writer to
  `ENTITY_STATES` (ADR-055) — rejects any mutation whose entity ID is not 6-part or whose triple
  predicate is not 3-part, returning a classified validation error and emitting a loud (WARN/ERROR) log
  naming the offending token, rule/source, and reason — rather than persisting a structurally-invalid
  token. This is the choke point every product write already flows through. **BREAKING** for any caller
  currently emitting a non-conforming token (expected: none in first-party code; confirmed by the audit
  gate below).
- **Gate the flip on a dry-run audit.** Before enforcement flips to fail-closed, a dry-run pass +
  `structural_validation_rejects_total{reason}` metric runs over live ingest and the reference-config /
  vocabulary corpus, so a real violation surfaces as a counted SKIP — not a silent reject — and is
  backfilled first (cost-ledger discipline).
- **Fix the entity-ID contract drift.** The contract is **exactly 6 parts, no dotted segments** — as
  `IsValidEntityID` already enforces (its charset excludes `.`). `parseEntityID`
  (`graph/clustering/summarizer.go`) drifts by accepting `>= 6` and joining a dotted instance
  (`strings.Join(parts[5:], ".")`); that leniency is corrected to the single exactly-6 contract so the
  invariant is not defined two ways.
- **Retire the bad-pattern debt.** 2-part test fixtures (`agent.role`, `battery.level`, `source.value`)
  are an accepted-because-unenforced pattern that seeds more of the same. Migrate fixtures to conforming
  3-part predicates and add a lint (extending `test/reference_configs_test.go`) that fails on any
  non-conforming predicate in fixtures or reference configs.
- **Wire the dead validator in.** `IsValidPredicate` stops being dead code — it becomes the enforced
  contract the ingest boundary and the lint both call.

## Capabilities

### New Capabilities

- `structural-identity`: the enforced structural contract for the graph's dotted-token namespaces —
  6-part entity IDs and 3-part predicates — including the validity rules (part count, non-empty segments,
  safe charset), the fail-closed write-boundary enforcement point, the rejection error contract, and the
  determinism guarantees downstream consumers (KV keys, wildcards, prefix/domain splits) rely on. First
  OpenSpec touch of this contract; distilled from `message/triple.go`, `vocabulary/predicates.go`, and
  verified against the consuming split/key sites.

### Modified Capabilities

- `graph-ingest`: the sole `ENTITY_STATES` writer gains a **structural-validation gate** — a mutation
  carrying a non-6-part entity ID or non-3-part predicate is rejected with a classified validation error
  before persistence. (Requirement-level behavior change: previously any dotted string was accepted.)

## Impact

- **Code**: `vocabulary/predicates.go` (harden `IsValidPredicate` + typed validator), `message/triple.go`
  (entity-ID validator reconciliation), graph-ingest mutation-handler validation gate + rejection metric,
  `test/reference_configs_test.go` (predicate-structure lint), fixture migrations across
  `processor/rule/` and other 2-part-predicate test data.
- **Consumers**: all sem* products write graph mutations through graph-ingest, so all inherit the gate.
  Non-conforming tokens (expected: none) would be rejected — the audit gate surfaces any before the flip.
  Unblocks gh#519's structural `.value` disambiguation.
- **Determinism**: guarantees the positional-split assumptions (predicate `parts[1]`/`parts[2]` band
  parsing in the e2e client; entity-ID `parts[2]` = domain in `parseEntityID`/`pkg/types/entity_id.go`),
  the gh#474 whole-token predicate KV keys, and `domain.category.*` wildcard-ability, so those sites can
  rely on the contract instead of guarding or silently mis-splitting.
- **Not a live-graph retention change**: no NATS TTL/MaxBytes/MaxAge involvement (ADR-068 untouched).

## Non-goals

- **A predicate ontology / semantic vocabulary registry.** This change enforces *structure* (part count,
  segment validity), not *meaning* — it does not decide which predicates are semantically valid or force
  registration. Product domain semantics stay out of the substrate (Product Boundary).
- **Renaming or migrating existing conforming predicates.** No churn to the ~all-conforming first-party
  corpus; only non-conforming tokens are affected.
- **Enforcing predicate structure inside agent-authored *content*** (rule-opaque predicates, ADR-036).
  Those are validated at the same write boundary if they reach `ENTITY_STATES`, but this change does not
  add a second in-agent gate.
- **Changing entity-ID or predicate *semantics*** (the 6-part hierarchy meaning, the domain.category.property
  roles) — only their structural validity and its enforcement.
- **gh#519 itself.** The `.value` suffix change consumes this invariant but ships separately, on top.

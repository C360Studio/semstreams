# SemStreams Developer Agent Contract

## Purpose and authority

The SemStreams developer implements nontrivial backend changes without weakening the semantic, storage, or runtime
contracts that make this repository more than a generic Go event processor. This contract is canonical for every
SemStreams developer adapter.

The architect owns architecture, API contracts, ADRs, and OpenSpec target state. The technical writer owns durable
documentation and task truth. Generic Go agents may provide a second pass for isolated language idioms, concurrency,
or runtime mechanics; they do not replace this project-specific role.

## Required workflow

0. **Ruled-change conformance (binding, 2026-08-02 audit).** When the active change carries recorded rulings,
   constraints, or approval conditions, five rules govern every task slice:
   1. *Conformance is a table, not a sentence.* Before merge, produce a per-ruling table — ruling → `file:line`
      implementing it, or an explicit DEVIATION row with the owner's recorded sign-off. Citing the ruling in a
      commit message is not conformance evidence.
   2. *A deviation escalates; it never executes.* If mid-implementation you conclude a ruling, constraint, or
      approval condition is wrong or unimplementable as ruled, stop the slice and surface it for re-ruling. This
      binds at ANY severity label anyone assigns it.
   3. *The exported-surface gate re-runs when the surface grows.* A shape review scoped to the symbols planned
      covers only those symbols; every export added mid-flight re-enters the gate before merge. Scope is what
      shipped, not what was planned.
   4. *Correction-propagation sweep before merge.* Every mid-flight correction — a repudiated mechanism, a
      measured-false premise, a review-fix — invalidates text in outer layers. Grep the change's own artifacts
      (commit message draft, spec deltas, doc comments, adopter notes, task lines, cited ruling conditions) for the
      superseded mechanism or claim, and re-sync each hit or record why it stands.
   5. *Evidence claims carry artifacts.* A gate result, measurement, or re-run asserted in tasks, commit messages,
      or docs must be reproducible from an in-tree or CI artifact; otherwise record it as UNVERIFIED, never as fact.

1. Read `openspec/project.md`, the applicable current capability specs, and every file in the active change before
   coding. Read the full proposal, design, spec deltas, and tasks rather than relying on excerpts or task summaries.
2. Confirm one architect-reviewed task slice. Implement only that coherent slice and identify its callers, callees,
   persistence seams, query surfaces, and release gates.
3. Use TDD: add a behavior-level failing test, observe the intended failure, implement the minimum complete change,
   then run focused tests before broader gates.
4. Trace the complete semantic path when applicable:
   producer -> graph-ingest -> `ENTITY_STATES` -> KV watchers -> derived indexes -> query/search/clustering.
5. Report exact commands and outcomes. Do not mark mixed OpenSpec task wording complete; give the technical writer
   evidence for conservative task-truth updates.
6. Complete SemStreams implementation review and the owner-run cross-agent round, resolve findings, and obtain any
   required re-review before archiving. Then archive the change as the landing PR's final content commit
   (`openspec archive <id>`) and require a narrow final reviewer check of the archive/spec sync before integration.
   A correction after archive re-enters reconciliation and final review; no later content commit bypasses that check.
   The ruleset-enforced merge is the CI-green proof. Never write or leave a task that asserts a post-merge fact —
   "CI green", "merge-ready", "hosted CI approval" cannot be ticked before merge and strand the change unarchived.
   Tasks assert branch-checkable facts: the PR number, the recorded reviewer verdict, the commands run with results.
7. **Never run a git command that can discard working-tree state**: `git checkout -- <path>`, `git restore <path>`,
   `git stash` in any form (including `git stash push -- <path>`), `git clean`, `git reset --hard`. You work on trees
   holding UNCOMMITTED, UNSTAGED, and UNTRACKED work — yours and the caller's — and these destroy it unrecoverably.
   This has already cost real work on PR #604.

   Step 3's "observe the intended failure" and any mutation check must be done with a `cp` backup you make first, and
   restoration verified by checksum:

   ```bash
   cp path/to/file.go /tmp/file.go.bak && md5 -q path/to/file.go   # BEFORE
   cp /tmp/file.go.bak path/to/file.go && md5 -q path/to/file.go   # AFTER; sums MUST match
   ```

   Do not verify restoration with `git diff --stat` — it reports nothing for untracked files, and new test files are
   routinely untracked. If you destroy work, report it at the TOP of your response before anything else.

### Locating and reading

Structural questions — who calls this, who implements this, where is this declared — are one `gopls` call each
(`references`, `implementation`, `workspace_symbol`, `call_hierarchy`), never a grep sweep; grep (`git grep -n`) is
for string literals. Read ranges (`grep -n` → `sed -n a,bp`), not whole files: a whole-file read is paid again on
every later turn. After commits touching an inventoried surface, `task inventory:verify -- <inventory.md>` names the
pins that drifted; refresh them rather than re-sweeping.

## Before adding anything new

Most defects in this repository's record entered as ADDITIONS duplicating something nobody had inventoried — a
second pub-ack detector beside the gateway's existing one, a resolver re-deriving a classification FlowGraph already
performed, a bool spelling a fact an existing port type already carried. Before adding ANY new symbol, field,
channel, resolver, classifier, port, subject, bucket, or config key — exported or not — answer five questions with
evidence, and carry the evidence into the handoff:

1. **Who owns this responsibility today?** Search the concept under every plausible spelling: exported and
   unexported names, config keys, port types, payload kinds, subject grammars. If an owner exists, extend it or
   escalate — never add a sibling. A second interpreter or second spelling of an existing fact is wrong at birth
   even when it works.
2. **Is the premise true?** A task, ruling, or issue saying "add X because X is missing" asserts an absence —
   measure it. If the search finds X, stop and escalate with `file:line`; implementing as written re-commits the
   defect the instruction meant to prevent.
3. **Who consumes it at birth?** Name the present consumer of every new surface. "For observability" or "for
   future use" with zero present consumers is a phantom — do not add it.
4. **Am I asking a caller to predict something the framework could observe?** A new knob, threshold, limit, name, or
   "remember to call X first" hands the caller a fact the framework already holds; they will get it wrong, and
   silently. Prefer acting and handling the real outcome over making them compute it in advance. If the slice cannot
   absorb the failure, escalate — do not ship the knob and document it.
5. **Am I establishing a pattern other planes should adopt?** If question 1 found *no* present owner and what you are
   adding is a named primitive intended for reuse across planes — a validator, gate, authority, classified-error
   family, dispatcher, or lifecycle shape — the change owes an **adoption sweep**: one line per plane that should
   adopt it, pinned at `file:line`, filed as issues or one tracking issue. It is an **enumeration obligation, never a
   migration obligation** — you fix none of them, and the number found never blocks your PR. Worked case and the
   reason the bound matters: `docs/contributing/07-pattern-adoption.md`.

The architect's surface and adopter seam inventories answer these at design time. This check is the
implementation-time re-run, scoped to the slice you touch — slices grow symbols the design never named.

## Semantic identity and graph contracts

- Predicates are exactly three canonical parts. Validate at the authoritative boundary and do not let hashing or
  encoding become acceptance authority.
- Literal entity IDs are exactly six parts and at most 256 serialized bytes. Keep literal IDs, six-token declaration
  patterns, and one-to-six-token query prefixes as separate languages with separate APIs.
- Use shared NATS literal-key and wildcard-filter validators. Reject malformed semantic axes, complete keys, and
  filters before lister, watcher, request, Put, Get, Delete, callback, retry, or operation-metric side effects.
- Index token axes are semantic contracts, not convenient string concatenation. Prove the axis owner and exact
  forward/owner filters before relying on fixed positions.
- Every query-visible current-state index must implement replacement, including `[A] -> [B] -> []`. Test removal
  through public exact, value, list, stats, name, incoming, traversal, search, and clustering surfaces that apply.
- Sort and deduplicate complete result sets deterministically before applying limits or samples. Preserve established
  ranking tie-breaks.
- Keep readiness and authoritative watermarks honest. Never expose partial replay, repair, or index state as ready.
- Construct maximum supported keys and filters and prove their exact match sets against real NATS. Representative
  corpus success and arithmetic alone do not authorize an index layout.

## Exported-surface contracts

These bind every NEW exported symbol. New exported surface on the framework packages (`natsclient`, `graph`,
`message`, `pkg/*`) additionally requires owner design review BEFORE implementation (model-roles rule).

- Return the answer, not the components and not a capability. If the doc comment must warn callers against using
  part of the return, or the return is a handle, connection, map, or internal context where the caller needs a
  value, collapse the signature until the warning is unnecessary. A signature's affordances are its contract;
  prose does not override them, and a leaked handle offers its whole wider surface to every future caller.
- Three or more correlated non-error returns are a named struct. Values that travel together get a type;
  positional tuples drift and misbind at call sites.
- Widen deliberately, never speculatively. When a real second consumer needs more than the current surface
  answers, that is the moment to extend — under the same review.

## Guarantee, signal, and revision contracts

Distilled from the measured Codex blocking-finding record: nineteen of twenty-two blocking findings across eight
PRs fell into the four classes below.

- **Enumerate the hole class before claiming a guard.** A guard, sweep, gate, or coverage
  claim protects a CLASS, never the motivating instance. Before claiming it, grep the guarded
  primitive and enumerate EVERY seam, emitter, entry path, and creation site — including ones
  added by this same change — and cite the enumeration where the claim is made. The recurring
  shape: a second entry path (config lane, reconnect auto-create, escape-hatch branch, the
  guard's own grammar) reopens what the first pass closed.
- **Every failure, teardown, and absent path fails closed.** A failure path must produce the
  negative or UNKNOWN signal; a positive signal (ready, complete, committed, provisioned)
  requires its precondition provably held on that exact path. A zero value, nil map, empty
  read, or given-up join is never an answer. Enum and grammar validation rejects unknown and
  empty values explicitly — silent drop from a derived set is the fail-open shape.
- **Bind every action to the revision it acted on.** A CAS reports its OWN resulting revision
  — never a post-hoc live read that can capture a foreign writer's commit. Convergence and
  repair marks clear CAUSALLY against the revision that created them, never on "any later
  terminal". A baseline or cache of published state commits only AFTER the publish succeeds.
  Identity keys must canonicalize across every representation the value takes (in-memory vs
  persisted JSON) or restart re-fires the class.
- **A filed issue does not discharge an in-PR guarantee.** If this PR asserts a guarantee, it
  holds at execution time in this PR; filing the gap is recording, not satisfying.
- **Remedies get the original's scrutiny.** Fix commits for review findings are new code with less design time
  than what they replace — remedies are where new blockers enter. Re-run the adversarial pass on your own fixes.
- **A skip, drop, or degrade is a declared event, never a private choice.** Where a path
  deliberately continues past a failure — a tolerated push failure, a fallback, a dropped
  element, a partial result — it emits a log line AND a metric naming what was skipped and why
  continuing is safe, or it refuses loudly; write the decision at the site. Route by ADR-098:
  substrate → log+metric; agent execution → graph conditions, never a parallel channel. Write
  the test that observes the signal and mutation-check it like a refusal (skip the emit → the
  test MUST fail). This is what turns the next session's two mystery bugs into two error
  messages.

## Storage and retention contracts

- Keep `windowed`, `entity-owned`, and `retained` storage classes distinct. Bounded admission and capacity rejection
  are operational protection, not semantic entity GC.
- Live graph state and required current indexes never use TTL or `DiscardOld` lifecycle eviction. A finite graph
  ceiling is only a verified `DiscardNew` circuit breaker with replacement/recovery reserve and honest rejection.
- Large content uses backend-neutral `storage.Store` and `StorageReference` contracts. NATS ObjectStore is one
  bounded backend, not a mandatory address exposed to graph or query contracts.
- Before v1, breaking identity/index adoption starts downstreams on newly provisioned NATS storage after every owned
  source, configuration, schema, fixture, and query is updated. Prove cold start, readiness, and affected product E2E.
  Do not require migration, preservation, wipe, or reseed for absent state, and add no legacy reader, beta-state
  exporter, alias, dual format, online migration, or rollback path. If retained deployed state is discovered, stop
  for a separate owner-reviewed migration or recovery design. Preserve typed poison recovery and optional-state
  degradation.
- After v1, retained-state upgrades are authorized only by the active `bounded-storage-operability` contract: a
  versioned report-only preflight, operator-approved plan, proven backup/restore, staged enforcement, safe rollback
  point, and removal deadline for temporary migration compatibility.

## Runtime footguns

### Context ownership

- Production structs SHALL NOT retain `context.Context`. This includes embedded fields, renamed imports, type
  aliases, wrapper types, interface containers, getters, provider closures, and public knobs that hide or recover a
  stored context. Existing violations are removal work, never precedent.
- Pass context as the first argument. An owning `Start` or `Run` may derive a lifecycle child context locally; pass
  the exact received or derived operation context directly into goroutines, callbacks, and helpers. Lifecycle owners
  may retain only a private `context.CancelFunc` with synchronization matching the start/stop contract. Component
  work derives from `Start` or `Run`, and every spawned task joins `Stop`.
- Create production root contexts only at the process composition boundary. Constructors, factories, callbacks,
  watchers, and goroutines must not invent roots with `context.Background`, `context.TODO`, or
  `context.WithoutCancel`. Use context-aware standard APIs for blocking or cancelable operations when available.
  `http.Server.BaseContext` may inject lifecycle only where the server is composed and its closure captures the exact
  `Start` context; repository-defined generic context getters and providers remain prohibited.
- Callers never pass nil context. Exported context-taking boundaries reject nil when able to return an error; private
  helpers rely on the caller invariant. Never default nil to `context.Background`.
- Detach only terminal cleanup or finalization, or an already-accepted durability operation whose invariant requires
  bounded completion after owner cancellation. `context.WithTimeout` is the immediate boundary. With a parent, use
  `context.WithTimeout(context.WithoutCancel(parent), budget)`. A timeout-only `Stop` or equivalent finalizer with no
  parent contract may use `context.WithTimeout(context.Background(), budget)`. Complete synchronously or join all
  tasks before return; never feed `Start`, `Run`, `Watch`, or continuing work.
- Do not use `context.WithoutCancel(parent)` directly or create an unbounded descendant. Nested `context.WithCancel`,
  `context.WithCancelCause`, or other child cancellation is allowed beneath the bounded context only when all tasks
  join before the terminal operation returns.
- Exported lifecycle records SHALL NOT expose `context.CancelFunc`. Existing violations are removal debt; only the
  lifecycle owner may retain a private, synchronized cancel function.
- Before changing a lifecycle or concurrency seam, inventory it for every disguised form above. If the requested
  implementation would add, preserve, or work around any violation above, stop the slice and escalate for a removal
  design; do not implement it.

### NATS RPC

- Classified handlers require `RequestClassified` or `RequestWithRetryClassified`. Raw `Request` plus JSON unmarshal
  can decode an error envelope as a zero-valued success response.
- Propagate classified request errors without destroying their class/code/detail. Treat handler errors as response
  bodies according to the repository RPC contract.
- Use `errors.Is` for JetStream sentinels and cover sibling states such as not-found/deleted and no-keys/not-found.

### Payload registry

- Every polymorphic payload publish uses `BaseMessage`.
- A new payload requires registry factory registration, alias-based `MarshalJSON`, and an import in every binary that
  must execute registration.
- Round-trip through the production decoder, not an anonymous shape cast.

### State ownership and component wiring

- Only graph-ingest writes domain entities to `ENTITY_STATES`; other components emit `Graphable` or use an explicitly
  owned operational bucket.
- Single-valued lifecycle and projection facts replace old triples; they do not append competing scalar values.
- Register every new or migrated component/payload in `cmd/semstreams` and `cmd/e2e-semstreams` as applicable.
- Run schema generation for operator-facing configuration and verify committed schemas/specs have no drift.
- Register every OpenAPI `SchemaRef` type and test configuration through production JSON and wiring paths.

### Orchestration

- There is no separate workflow engine. Rules trigger work, components execute it, and lifecycle is a convention for
  durable named-entity phase/state. State ownership remains exclusive.
- Rules carry references, never bulky content. Semantic judgments over content belong in a coordinator that emits a
  structured result.
- Give `when`-gated loops a cap-exhaust behavior, audit substitution grammar collisions, and verify reference tokens
  against the production stamper.

## Test and operational fidelity

- Drive production constructors, registries, codecs, NATS handlers, and wire envelopes. Helper-only tests do not prove
  the assembled system.
- Any new exported surface that parses, decodes, or validates external bytes or strings — subjects, keys, entity
  IDs, payload envelopes, config — ships with a native `Fuzz*` target and a seed corpus covering each grammar
  class it accepts AND each it must reject, asserting an invariant (never panics; round-trips; rejection is a
  typed error), following the in-tree pattern (`FuzzParseEntityIDRoundTrip`, `FuzzKVValidatorsNeverPanic` — both
  seed rejects heavily). Seed corpora run in plain `go test`. Where fuzzing is genuinely inapplicable to the
  surface, say why and cite the filed gap issue — silence is the finding, not the exemption.
- A property-based test encodes an invariant the design cited, never one inferred from the implementation. Write
  the generator from the input grammar and the property from the cited spec clause; annotate the test
  `// spec: <capability> / <requirement heading>`, quoting the `### Requirement:` heading text verbatim, so the
  citation is greppable and resolves against `openspec/specs/` or the active change's delta. A property whose
  expected value is recomputed by the implementation's own algorithm is the reconstructs shape — it cannot fail.
  The generator must provably reach every boundary the cited clause names — proof is by construction (a
  boundary-hugging generator whose value set contains the bound) or a committed shrunk counterexample from a
  mutation kill. A wide range that merely strides a bound catches an off-by-one only probabilistically (measured
  on PR #1213 — a `>=` mutation at the 256-byte entity-ID bound survived 100 uniform draws; a boundary-hugging
  `rapid.OneOf` generator killed it after 0 tests). If implementation reveals an invariant the spec never stated,
  do not quietly encode it: mark it at the assertion and file a spec-gap issue, then cite that issue in place of
  the clause (the route #1213 took for the unstated clean-`StopAll` registry clear, filed as #1214).
- Use ephemeral ports, explicit synchronization, and no `t.Parallel()` around process-global state such as
  `slog.SetDefault`. Explain wall-clock assertions and give them realistic tolerance.
- Run focused unit tests, `task lint`, `go test -race ./...`, schema generation/no-drift, contract tests, and relevant
  real-NATS integration in proportion to the slice.
- Any BREAKING commit must have every relevant e2e tier green before it lands. If no tier covers the path, record the
  coverage gap before release.
- For paid LLM calls, cloud runs, prolonged CI, or other costly operations, validate monitor filters and actively poll
  authoritative state every 30-60 seconds. Compare progress timestamps and abort promptly when a wedge is proven.

## Handoff

Summarize the implemented task slice, semantic blast radius, tests and exact results, unresolved gates, and any
follow-up owned by the architect, reviewer, or technical writer. Do not claim completion from compilation alone.

# Inventory: existing framework admission/authority precedent
base: 0a40ddf347db325c8fc34924b61260f3dc316e68

Third axis, written by the orchestrating session alongside the two explorer sweeps
(`inventory-carriers.md`, `inventory-attach.md`). Those two enumerate what the agentic plane does.
This one enumerates **what the framework already provides one or two levels up**, because
"we must build a primitive" and "a proven primitive exists and this plane never adopted it" are
different rulings and the evidence separating them is not on the agentic surface.

Pins only. No verdict on whether the precedent transfers — that is the architect's read.

## Claimed gap — an existing, fully realized admission gate on another plane

`processor/graph-ingest/authority_gate.go` implements, for the graph plane, every element the three
issues report missing on the agentic plane:

- `processor/graph-ingest/authority_gate.go:51` — `func (c *Component) authorizeSubject(subject string, importLane bool) error`
- `processor/graph-ingest/authority_gate.go:38` — `// It is called at every seam that already validates an entity ID structurally,`
- `processor/graph-ingest/authority_gate.go:39` — `// on every lane — Graphable fact arrival, each graph.mutation.> operation, and`
- `processor/graph-ingest/authority_gate.go:40` — `// direct in-process persistence — before any KV I/O. Structural validation runs`
- `processor/graph-ingest/authority_gate.go:41` — `// first inside ValidateEntityIDAuthority, so an authority reason never masks a`
- `processor/graph-ingest/authority_gate.go:42` — `// malformed candidate.`
- `processor/graph-ingest/authority_gate.go:45` — `// It is NEVER called for an @id OBJECT: a relationship target keeps structural`
- `processor/graph-ingest/authority_gate.go:57` — `// authorityMetricReason maps an authority rejection to its mutation_rejections`
- `processor/graph-ingest/authority_gate.go:59` — `// One home for the mapping so the fact lane and the mutation lane cannot disagree.`
- `processor/graph-ingest/authority_gate.go:31` — `// authorityRejectionLogMessage is the single WARN a refused candidate produces`
- `processor/graph-ingest/authority_gate.go:32` — `// on any lane. Named so the test pinning the requirement's "loud log" matches`
- `processor/graph-ingest/authority_gate.go:75` — `func (c *Component) recordAuthorityRejection(arrival, reason string, err error)`
- `processor/graph-ingest/authority_gate.go:79` — `// the whole point of the gate is that a foreign identity is not this`

Note for the architect: the ordering comment at `:41` ("structural validation runs first ... so an
authority reason never masks a malformed candidate") is the same ordering question that #1228 (form)
and #1227 (authorization) pose on the agentic plane, already answered here.

### The framework primitive underneath it

- `pkg/types/entity_id_authority.go:35` — `func ValidateEntityIDAuthority(candidate, org, platform string, importLane bool) error`
- `pkg/types/entity_id_authority.go:8` — `ErrorCodeEntityIDAuthorityInvalid = "entity_id_authority_invalid"`
- `pkg/types/entity_id_authority.go:20` — `EntityIDLaneLocal = "local"`
- `pkg/types/entity_id_authority.go:22` — `EntityIDLaneImport = "import"`
- `pkg/types/entity_id.go:149` — `func ValidateEntityID(value string) error`

Its non-test callers (`git grep 'ValidateEntityIDAuthority' -- '*.go' | grep -v _test` → 7 hits, 4 of
them the declaration and comments):

- `graph/inference/hierarchy.go:217`
- `processor/graph-ingest/authority_gate.go:52`
- `processor/rule/actions.go:597`

### A second authority object, different surface

- `vocabulary/namespace_authority.go:101` — `func (a *PredicateAuthority) Authorize(producer, predicate string) error`
- `vocabulary/namespace_authority.go:97` — `// Authorize validates predicate syntax and declaration authority.`
- `agentic/tools.go:455` — `func AuthorizeLineageTriplePredicate(producer, predicate string) error`

### One more admission home, named as such

- `component/port_facts.go:155` — `// one place a foreign org.platform is admitted (ADR-102 d5). False is the`

## Spellings of the fact — classified refusal vocabulary

`pkg/errs` distinguishes two families. Only one carries a machine-readable code and detail:

- `pkg/errs/errs.go:94` — `type ClassifiedError struct {`
- `pkg/errs/errs.go:327` — `func Classified(class ErrorClass, err error) *ClassifiedError`
- `pkg/errs/errs.go:338` — `func ClassifiedCode(class ErrorClass, code string, err error) *ClassifiedError`
- `pkg/errs/errs.go:356` — `func ClassifiedCodeDetail(class ErrorClass, code string, detail map[string]any, err error) *ClassifiedError`
- `pkg/errs/errs.go:394` — `func Wrap(err error, component, method, action string) error`
- `pkg/errs/errs.go:435` — `func WrapInvalid(err error, component, method, action string) error`

Adoption of the coded family, by package (`git grep -l 'errs.Classified' -- '*.go' | grep -v _test`,
directory counts): natsclient 5 · processor/graph-query 4 · processor/graph-ingest 4 ·
processor/agentic-loop 4 · graph 4 · processor/graph-embedding 3 · pkg/lifecycle 3 ·
processor/rule 2 · processor/graph-index 2 · pkg/fusion/fusionnats 2 · gateway/http 1 ·
processor/agentic-tools 1 · (17 more at 1).

**`processor/agentic-dispatch` does not appear in that list.**
`git grep -c 'errs.Classified' -- 'processor/agentic-dispatch/'` → no file matches (0).

Its full `errs.` usage is the uncoded family (`git grep -o 'errs\.[A-Za-z]*' -- 'processor/agentic-dispatch/*.go' | grep -v _test`, 34 hits):

| symbol | count |
|---|---|
| `errs.WrapInvalid` | 18 |
| `errs.WrapTransient` | 11 |
| `errs.Wrap` | 3 |
| `errs.ErrNoConnection` / `ErrMissingConfig` / `ErrInvalidData` / `ErrInvalidConfig` / `ErrAlreadyStarted` | 1 each |

- `processor/agentic-dispatch/command_registry.go:14` — imports `pkg/errs`
- `processor/agentic-dispatch/commands.go:13` — imports `pkg/errs`
- `processor/agentic-dispatch/component.go:20` — imports `pkg/errs`
- `processor/agentic-dispatch/config.go:7` — imports `pkg/errs`
- `processor/agentic-dispatch/global.go:25` — imports `pkg/errs`
- `processor/agentic-dispatch/loop_tracker.go:14` — imports `pkg/errs`

## Adjacent claims

- `pkg/lifecycle/manager.go:221` — `func (m *Manager) Get(ctx context.Context, workflow, entityID string) (Participant, error)`
- `pkg/lifecycle/manager.go:230` — `func (m *Manager) GetWithRevision(ctx context.Context, workflow, entityID string) (Participant, uint64, error)`
- `pkg/lifecycle/manager.go:297` — `func (m *Manager) Create(ctx context.Context, initial Participant) error`
- `pkg/errs/errs.go:386` — `var ErrRevisionMismatch = &ClassifiedError{`
  (a framework create-vs-exists distinction with revision-checked update and a classified mismatch;
  the architect should compare it against `LoopManager.CreateLoopWithID`'s unconditional overwrite,
  pinned in `inventory-attach.md`)
- ADR-102 — governs the entity-ID authority boundary the graph-plane gate enforces
- ADR-049 — Lifecycle harness; participation is a property of the ENTITY, not the component
- #1226 — `processor/rule/expression/evaluator.go` hand-rolls `isValidEntityID` rather than calling
  `pkg/types`; the same hand-rolling shape on a neighbouring surface, not swept here
- #1225, #1227, #1228, #1230; PR #1159, PR #1231

## Consumers

`ValidateEntityIDAuthority` — 3 non-test callers, listed above. `PredicateAuthority.Authorize` and
`AuthorizeLineageTriplePredicate` not enumerated for callers (out of this axis's scope; NOT RUN).

## Searches

- `git grep -n -i 'admit' -- '*.go' | grep -v _test` → 172
- `git grep -n -iE 'func .*(Authorize|Permitted|CanAccess|IsOwner|OwnedBy)' -- '*.go' | grep -v _test` → 4
- `git grep -c -iE 'Authorize|Permission' -- '*.go' | grep -v _test` → 20 files
- `git grep -n 'ValidateEntityIDAuthority' -- '*.go' | grep -v _test` → 7
- `git grep -l 'errs.Classified' -- '*.go' | grep -v _test` → 43 files across 25 directories
- `git grep -n 'errs\.Classified' -- 'processor/agentic-dispatch/*.go' 'processor/agentic-loop/*.go' 'agentic/*.go' | grep -v _test` → 19 (0 in agentic-dispatch)
- `git grep -c 'errs.Classified' -- 'processor/agentic-dispatch/'` → 0 files match
- `git grep -o 'errs\.[A-Za-z]*' -- 'processor/agentic-dispatch/*.go' | grep -v _test` → 34
- `git grep -n 'pkg/errs' -- 'processor/agentic-dispatch/*.go'` → 8 (2 are test files)
- `ls pkg/` → 24 directories
- `grep -rn '^func \|^type ' pkg/security/*.go | grep -v _test` → 18 (TLS/ACME config only; no request-authorization surface)
- NOT RUN: callers of `PredicateAuthority.Authorize`; `gopls` structural passes (this axis used
  literal search only — the two explorer files carry the `gopls` enumeration)

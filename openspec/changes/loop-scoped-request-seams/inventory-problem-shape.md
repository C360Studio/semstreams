# Inventory: problem shape

base: 5389ec5d102ea5327e477d5c2b202630f29e25d9

Pilot of the fifth inventory category proposed in #1232. The other four ask what facts exist; this one asks
**what SHAPE this design is, and where the repository already has one.** It exists because the fact-scoped
categories cannot surface a cross-plane pattern: every pin in `inventory-attach.md` and `inventory-carriers.md`
is on the agentic surface, and the answer to "must we build a primitive?" was one directory away in
`processor/graph-ingest`, invisible to any search scoped by the fact.

Method: name the shape from the semantic job, not from the proposed symbol. Search the repository for that job
under other names. State whether this design adopts, extends, or departs — and if it departs, why.

## Shape 1 — an admission gate for one plane

The job: many seams accept the same kind of untrusted reference and each needs the same ordered checks. Today
each hand-rolls a subset, and the holes are the subsets nobody filled.

Closest existing instance — the graph plane's entity-ID authority gate. Note that the ordering argument, the
carve-out, the one-mapping-home argument, and the named-log-string argument are all written into the source:

- `processor/graph-ingest/authority_gate.go:51` — `func (c *Component) authorizeSubject(subject string, importLane bool) error {`
- `processor/graph-ingest/authority_gate.go:38` — `// It is called at every seam that already validates an entity ID structurally,`
- `processor/graph-ingest/authority_gate.go:41` — `// first inside ValidateEntityIDAuthority, so an authority reason never masks a`
- `processor/graph-ingest/authority_gate.go:45` — `// validation only, no stub is created for it, and an absent target is permitted`
- `processor/graph-ingest/authority_gate.go:58` — `func authorityMetricReason(err error) (string, bool) {`
- `processor/graph-ingest/authority_gate.go:33` — `const authorityRejectionLogMessage = "graph-ingest: entity authority rejected"`
- `processor/graph-ingest/authority_gate.go:31` — `// on any lane. Named so the test pinning the requirement's "loud log" matches`
- `processor/graph-ingest/authority_gate.go:79` — `func (c *Component) recordAuthorityRejection(arrival, reason string, err error) {`
- `processor/graph-ingest/authority_gate.go:124` — `func (c *Component) recordDirectAuthorityRejection(err error) error {`

The `Detail`-carrying refusal it depends on, which `processor/agentic-dispatch` has never used:

- `pkg/errs/errs.go:356` — `func ClassifiedCodeDetail(class ErrorClass, code string, detail map[string]any, err error) *ClassifiedError {`

Second instance of the same shape on other planes, confirming it is the house pattern rather than one
component's idiom — neither is a closer fit than the graph-ingest gate:

- `vocabulary/namespace_authority.go:101` — `func (a *PredicateAuthority) Authorize(producer, predicate string) error {`
- `agentic/tools.go:455` — `func AuthorizeLineageTriplePredicate(producer, predicate string) error {`

**Disposition: ADOPT, element for element.** The agentic plane needs the same six elements and has none of them.

## Shape 2 — a create-vs-exists fence with a sentinel the caller branches on

The job: a registration keyed by an id must distinguish "new" from "already there", and the caller must be able
to act on the difference rather than only log it.

Closest existing instance — the Lifecycle harness, whose sentinel is consumed by four production callers, not
merely declared:

- `pkg/lifecycle/manager.go:297` — `func (m *Manager) Create(ctx context.Context, initial Participant) error {`
- `pkg/lifecycle/manager.go:290` — `// Returns ErrAlreadyExists if the entity already has a triple for`
- `pkg/lifecycle/errors.go:50` — `ErrAlreadyExists = errors.New("lifecycle: entity already lifecycle-managed")`
- `pkg/errs/errs.go:386` — `var ErrRevisionMismatch = &ClassifiedError{`
- `pkg/lifecycle/manager.go:964` — `// duplicate ID returns ErrAlreadyExists rather than overwriting. There is no`
- `agentic/agentrun/agentrun.go:318` — `if errors.Is(err, lifecycle.ErrAlreadyExists) {`
- `gateway/lifecycle-gateway/handlers.go:650` — `case errors.Is(err, lifecycle.ErrAlreadyExists):`
- `processor/gated-dag/executor.go:162` — `if err := e.mgr.Create(runCtx, inst); err != nil && !errors.Is(err, lifecycle.ErrAlreadyExists) {`

The exact anti-instance, which overwrites where the pattern refuses, and says so in its own doc comment:

- `processor/agentic-loop/state.go:151` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:148` — `// map write below OVERWRITES an existing record and its context manager. That`
- `processor/agentic-loop/state.go:170` — `m.loops[loopID] = &entity`
- `processor/agentic-loop/state.go:171` — `m.pendingTools[loopID] = make(map[string]bool)`
- `processor/agentic-loop/state.go:180` — `m.contextManagers[loopID] = NewContextManager(loopID, model, m.contextConfig, opts...)`

**Disposition: ADOPT THE SHAPE, DEPART FROM THE HOME.** Re-homing loops onto the harness was assessed and ruled
a reach — ADR-049 makes participation a property of the ENTITY, and a loop's state machine is not a workflow
phase machine. The sentinel and the caller-branches contract transfer; the harness does not.

## Shape 3 — merge two observations of one fact, refusing the conflict

The job: a fact is observable from two places, neither authoritative, either possibly absent. Trusting one is
wrong; silently preferring one is worse.

Closest existing instance — in the very package being changed, twenty lines from the seam:

- `processor/agentic-dispatch/terminal_settlement.go:39` — `func mergeRouteField(name string, values ...string) (string, error) {`
- `processor/agentic-dispatch/terminal_settlement.go:49` — `if merged != value {`
- `processor/agentic-dispatch/terminal_settlement.go:56` — `func reconcileTerminalRoute(tracker *LoopInfo, event agentterminal.Event, persisted *agentic.LoopEntity) (terminalRoute, error) {`
- `processor/agentic-dispatch/terminal_settlement.go:83` — `func (c *Component) loadPersistedLoop(ctx context.Context, loopID string) (*agentic.LoopEntity, error) {`
- `processor/agentic-dispatch/terminal_settlement.go:297` — `// persistLoopState is best-effort. Absence is a WALK signal (try the other`
- `processor/agentic-dispatch/terminal_settlement.go:300` — `func isLoopRecordAbsent(err error) bool {`

**Disposition: ADOPT AND LITERALLY REUSE.** Same package, same two sources, same absence semantics. The
strongest instance in this inventory: the design does not need a new primitive, it needs a second caller of one
that already exists.

## Shape 4 — observe the name, never predict it

The job: a reader needs a bucket, subject, or consumer name. Computing it from a constant is a prediction that
goes silently wrong when configuration differs. This instance carries its measured failure in the comment:

- `processor/agentic-dispatch/config.go:45` — `const agentLoopsPortName = "agent_loops"`
- `processor/agentic-dispatch/config.go:43` — `// with a constant, so a deployment running a non-default loops bucket lost`
- `processor/agentic-dispatch/config.go:49` — `// configuration. It is the only place the bucket is obtained; readers carry`

**Disposition: ADOPT.** The gate's durable read goes through the same port projection — no constant, no default
of its own. This is the observation-over-prediction rule with a measured failure behind it, in the same file the
design touches.

## Shape 5 — one terminal-release point, so a future path cannot leak half

The job: several per-subject aggregates must be freed when the subject settles, from several terminal paths.
Freeing them at each path invites a new path that frees some and leaks others.

Closest existing instance — again in the package being changed, with its placement argument already written:

- `processor/agentic-loop/trajectory_handler_wiring.go:52` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-loop/trajectory_handler_wiring.go:39` — `// It is the single terminal-release point so a future terminal path cannot`
- `processor/agentic-loop/trajectory_handler_wiring.go:41` — `// terminal return) AFTER the loop's terminal observation and terminal graph`
- `processor/agentic-loop/component.go:1447` — `defer c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:1613` — `defer c.releaseLoopTransientState(result.LoopID)`
- `processor/agentic-loop/component.go:1813` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:2128` — `defer c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/trajectory_observability.go:64` — `// releaseLoopTransientState alongside the loop's trajectory aggregate; the`

The aggregate this point does NOT yet release, and the function whose zero callers are #1233:

- `processor/agentic-loop/state.go:340` — `func (m *LoopManager) DeleteLoop(loopID string) error {`

#1225's leaked `activeLoops` gauge is the same shape at a smaller scale — terminal state failing to release
what start acquired — which is why the two are one change and not two: #1225 leaks a counter for a submission
that never became a loop, #1233 leaks the loop's whole in-process footprint for every loop that ever ran.

**Disposition: ADOPT, AS THE HOME.** The obvious fix — call `DeleteLoop` at the terminal transition — is wrong
for a reason the existing comment already states: the transition happens BEFORE the terminal observation and the
terminal graph write, both of which read the entity. The correct placement was already written down; the
design's only job is to notice it.

## What this category caught that the others did not

1. That the admission gate is not new work. Four of the five shapes above already exist in the tree; three are
   in the two packages this change edits. The framing question ("is a primitive missing?") was answerable only
   by searching for the JOB, and every fact-scoped search was scoped to the agentic surface.
2. That `DeleteLoop` is the wrong home for #1233's fix. A fact-scoped inventory finds `DeleteLoop` and its zero
   callers and stops there. Asking "what shape is a terminal release?" finds the point that already owns the
   placement argument.
3. That `CreateLoopWithID` is an anti-instance of a house pattern, not merely an unguarded function — which is
   what makes "adopt the shape" a smaller change than "design a fence".

Cost: five searches beyond the fact-scoped set, listed below. Recommend the category be kept.

## Searches

- `git grep -n -iE 'func .*(Authorize|Permitted|CanAccess|IsOwner|OwnedBy)' -- '*.go' | grep -v _test` → 4
- `git grep -l 'errs.Classified' -- '*.go' | grep -v _test` → 43 files across 25 directories; `processor/agentic-dispatch` absent
- `git grep -n 'ErrAlreadyExists' -- '*.go' | grep -v _test` → 19 (declaration, 4 production consumers, doc comments)
- `git grep -n 'releaseLoopTransientState' -- '*.go' | grep -v _test` → 7 (declaration, 4 deferred/called sites, 2 doc comments)
- `git grep -n 'mergeRouteField\|reconcileTerminalRoute' -- '*.go' | grep -v _test` → 5

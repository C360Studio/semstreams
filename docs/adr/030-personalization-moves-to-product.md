# ADR-030: Personalization Moves to the Product Layer

## Status

Proposed — 2026-04-18

Amends ADR-025 (Phase 2). See "Relationship to ADR-025" below.

## Audience

This ADR is written to be readable by both the semstreams and semteams
teams. Semteams owns the re-landing work described in
"Action for the semteams team"; semstreams owns the freeze, deprecation,
and delete sequence described in "Action for semstreams."

## Context

ADR-025 (Proposed, 2026-04-17) consolidated semteams into semstreams in
two phases. Phase 1 upstreamed framework primitives (prompt assembler,
tool categories, filters, governance filter, built-in executors). Phase 2
upstreamed a **personalization layer**: a `/onboard` slash command
driving a five-layer elicitation interview (operating rhythms, recurring
decisions, dependencies, institutional knowledge, friction), an
`agentic/operating-model/` data model (1,918 LOC), an intent classifier,
and a profile-context injection handler that publishes a "How this user
works" preamble into the agent loop's hydrated-context region.

ADR-029 step 3b ("wire the persona assembler into the agent loop so
KV-backed personas affect prompt assembly") began on 2026-04-18, one day
after ADR-025. That work surfaced a boundary question the Phase 2
consolidation had quietly papered over:

- `prompt.DefaultFragments()` in the assembler already ships a role
  taxonomy (explorer, architect, editor, reviewer, general, researcher,
  research-coordinator). The research-coordinator fragment references
  `read_loop_result` and the `decide` tool from ADR-026 — semteams
  product content embedded in framework code.
- Adding a fourth system-message injection channel (persona-driven
  registry composition) alongside the three that already existed
  (`task.Context.Content`, `profile_context` events,
  assembler-unwired) would accrete surface without resolving whose
  content goes where.
- Both Phase 2 features are config-gated off by default
  (`enable_onboarding`, `inject_profile_context`,
  `enable_profile_context`). Config-gating papered over the boundary;
  it did not decide it.

The project-level feedback `memory/feedback_framework_boundary.md`
captures the guiding principle:

> semstreams is a framework, no product opinions — role taxonomies,
> persona content, workflow names belong in products (semteams,
> semspec); when e2e fails, check product-owned entry points before
> adding framework config. Beta breaking changes need strong
> justification.

The five-layer operating-model schema, the `/onboard` command, and the
interview questions themselves are product taxonomy by this definition.
They are not framework primitives. The honest correction is to move
them back to the product layer — where they came from one day prior,
and where ADR-025's own scope boundary already placed product UX
(Svelte UI, flow configs, rule configs, persona configs, documentation).

## Decision

Personalization moves from semstreams to the product layer (semteams).
Semstreams retains ADR-025 Phase 1 (framework primitives only). Phase 2
is walked back in a coordinated, non-destructive sequence so semteams
can re-land the code before semstreams deletes its copy.

### What moves

The following artifacts leave semstreams for the product repository.
Total scope: roughly 5,000 LOC including tests (1,918 LOC in
`agentic/operating-model/` plus ~3,000 LOC in processor code and tests).

**`agentic/operating-model/` — 16 files, 1,918 LOC (pure move):**

`constants.go`, `doc.go`, `entity_ids.go`, `entity_ids_test.go`,
`entry.go`, `entry_test.go`, `graph_reader.go`, `layer_approved.go`,
`layer_approved_test.go`, `payload_registry.go`,
`payload_registry_test.go`, `profile_context.go`,
`profile_context_test.go`, `reader.go`, `triples.go`,
`triples_test.go`. The package doc comment makes the product nature
explicit: *"the data model and payload types for a user's work
operating model: rhythms, decisions, dependencies, institutional
knowledge, and friction."*

**`processor/agentic-dispatch/` onboarding files — 5 files, 1,302 LOC (pure move):**

`onboard_command.go`, `onboard_command_test.go`,
`onboarding_interview.go`, `onboarding_interview_test.go`,
`onboarding_tracker.go`. These implement the `/onboard` slash command
and the five-layer interview state machine; they have no framework
role.

**`processor/agentic-dispatch/` intent classifier — 2 files, 368 LOC (move with personalization):**

`intent_classifier.go`, `intent_classifier_test.go`. ADR-025 grouped
these with Phase 2. No code outside the tests imports the classifier
today (`ClassifyIntent` / `IntentClassifier` / `ClassifiedIntent` —
grep confirms only the package and its own test file reference them).
The classifier looks framework-generic at first read
(`new_task`, `continue`, `signal`, `question`, `meta` intent types),
but it is unused in semstreams and was introduced for
semteams-flavored dispatch. Ship it with the rest of the
personalization layer; the product repo decides whether to keep or
rewrite it.

**`processor/agentic-loop/` profile context handler — 2 files, 311 LOC (pure move):**

`profile_context_handler.go`, `profile_context_handler_test.go`. The
handler subscribes to `operating_model.profile_context.v1` events and
injects the rendered preamble into `RegionHydratedContext`. It is
coupled to the operating-model payload type; moving it with the
payload is clean.

**`processor/agentic-memory/` profile support — 5 files, 1,034 LOC (pure move):**

`profile_support.go`, `profile_context.go`, `profile_context_test.go`,
`layer_approved_handler.go`, `layer_approved_handler_test.go`. These
read the graph-backed operating-model profile and publish the
`profile_context` event. Pure coupling to the operating-model data
model.

### What changes (surgical edits, not moves)

Three files contain mixed generic + personalization code; each gets a
surgical edit during the delete phase rather than a file-level move.

- **`processor/agentic-dispatch/config.go`** — remove the
  `EnableOnboarding` field.
- **`processor/agentic-loop/config.go`** — remove the
  `InjectProfileContext` field.
- **`processor/agentic-memory/config.go`** — remove the
  `EnableProfileContext` field.
- **`processor/agentic-memory/component.go`** — remove the
  `profileReader` field from `Component`, the `initProfileReader`
  call in `NewComponent`, and the `operatingmodel` import. Generic
  hydrator/extractor memory component stays.

Schemas regenerate via `task schema:generate` after these edits.

### What stays (framework)

- `persona/` package — generic KV-backed persona storage, Pattern B
  per ADR-029. Manager + fragment conversion landed in commit
  `971de1b` and is framework plumbing regardless of where personas
  come from.
- `processor/agentic-loop/prompt/` — registry, assembler, types, and
  `DefaultFragments`. Only `DefaultFragments` content shrinks (see
  next point); the machinery stays.
- Everything else in ADR-025 Phase 1 (prompt assembler infrastructure,
  tool categories, `ToolCallFilter`, `ApprovalFilter`, tool call
  governance filter, built-in executors in `agentic-tools/executors/`).

### `DefaultFragments` shrinks

The in-code role taxonomy in `processor/agentic-loop/prompt/assembler.go`
is product opinion. It stays in semstreams only for the
framework-universal fragments that describe framework invariants, not
roles.

- **Keep** (framework-universal — describe how any agent in the system
  behaves, not who it is): `system-identity`, `system-tool-usage`,
  `system-submit-work`, `constraint-iteration-budget`,
  `constraint-child-agent`.
- **Move** (product taxonomy — name specific roles a product chose to
  model): `role-explorer`, `role-architect`, `role-editor`,
  `role-reviewer`, `role-general`, `role-researcher`,
  `role-research-coordinator`.

The role-taxonomy move is semstreams-only; no semteams dependency
gates it. It can land in parallel with semteams' re-landing work.
`processor/agentic-loop/prompt/assembler_test.go` drops the
role-filtering cases or narrows them to a single framework-universal
fixture; the product repo grows its own tests for its role taxonomy.

### System-message injection: two channels, not four

To prevent the same accretion happening again, semstreams commits to
exactly two system-message injection channels going forward. Any new
injection rail requires an ADR.

1. **`TaskMessage.Context.Content`** — rule-supplied static context at
   dispatch time. Product-owned content; framework just forwards.
   `handlers.go:307-312` reads this and prepends a system message.
   Lowest-level rail, always available.

2. **Assembler-driven system prompt** — agent loop composes at loop
   start via `prompt.Registry`. The registry holds
   framework-universal fragments plus whatever
   `persona.Manager.Fragments(ctx)` returns from the product's
   PERSONAS KV bucket. Product owns all role taxonomy and persona
   content via KV. Wiring lands as ADR-029 step 3b follow-up, once
   the extraction is done.

The existing `profile_context` event channel (currently channel #3)
departs with the personalization move. If a product needs
runtime-injected preamble content beyond the two channels above, it
publishes via a rule action into `TaskMessage.Context.Content`, or
stages the content as a persona record.

We explicitly **do not** build a generic "system preamble event" to
replace the `profile_context` channel. Generic preamble plumbing is
how personalization was re-labelled as framework in the first place;
the correction is to leave preamble composition as product concern.

## Relationship to ADR-025

ADR-025's scope boundary already placed product UX (Svelte UI, flow
configs, rule configs, persona configs, documentation) in semteams.
ADR-030 extends the same principle one level deeper: the interview
questions, the five-layer schema, the `/onboard` command, and the
intent taxonomy are also product concerns and belong on the product
side of that boundary.

ADR-025 stands as written for Phase 1 (framework primitives). Phase 2
(personalization layer) is amended by ADR-030. An amendment banner is
added to ADR-025 pointing at this ADR.

ADR-025 cited two counter-arguments in favor of upstreaming
personalization, which ADR-030 answers:

- *"The operating model is just another entity type producing triples,
  following the same pattern as every other Graphable. The discomfort
  is real but the architectural objection is weak."* — Graphable is
  the framework pattern; every caller of it ships its own entity types
  and triples. Ops agents, coordinator agents, and products all
  produce Graphables. That does not make every Graphable-producing
  concept a framework primitive — it makes Graphable the primitive.
- *"Extension points take 3–5 weeks and wrong abstractions are hard to
  reverse."* — true, but the ADR-029 Pattern-B normalization work that
  shipped in the intervening week gives us the extension point for
  free: a KV-backed Manager plus a tool-executor surface. Products
  use that today for rules, flows, personas, and flow-templates;
  personalization fits the same shape without new framework code.

## Migration style: freeze + remove after semteams re-lands

Semteams currently depends on the semstreams copies (ADR-025 Phase 2
was one day ago and has already propagated into semteams' build). A
clean extraction would break semteams' build. ADR-030 therefore
sequences the work so semteams is never broken:

1. **ADR-030 lands** in semstreams as a doc-only PR. No code changes.
2. **Semstreams freezes** the personalization code: no new features,
   `Deprecated:` doc comments on exported surfaces (`OnboardingWorkflowSlug`,
   `ClassifyIntent`, `ProfileContext`, etc.), optional startup log when
   `enable_onboarding`, `inject_profile_context`, or
   `enable_profile_context` is true, pointing at ADR-030.
3. **`DefaultFragments` shrinks** (parallel track, semstreams-only, no
   semteams dependency).
4. **Semteams reads ADR-030, acknowledges direction, plans the re-landing.**
5. **Semteams implements its copy** — fork from the current semstreams
   files (clean baseline) or reconstruct from pre-ADR-025 history.
6. **Semteams confirms** its e2e is green against semstreams `main`
   pre-delete.
7. **Semteams signals readiness.**
8. **Semstreams delete PRs land** — 3–5 small stacks (agentic-memory
   profile files, agentic-dispatch onboarding files, agentic-loop
   profile handler, operating-model package, intent classifier).
   Each stack deletes, edits the relevant config files, regenerates
   schemas, keeps CI green.
9. **Semteams bumps its semstreams dependency** to the post-delete
   tag and verifies e2e green.

Alternatives considered and rejected: clean extraction (breaks
semteams' build, not acceptable) and deprecate-in-one-beta
(unrealistic if semteams is working on other priorities).

## Action for the semstreams team

Tracked as follow-up PRs in the order they unblock. Items marked
*(parallel)* can start immediately; items marked *(blocked)* wait on
semteams' readiness signal.

- [ ] **ADR-030 PR** — doc-only, this document plus the ADR-025
  amendment banner.
- [ ] **Deprecation pass** *(parallel)* — `Deprecated:` comments and
  startup log.
- [ ] **`DefaultFragments` shrink** *(parallel)* — remove role
  taxonomy, update `assembler_test.go`.
- [ ] **Delete PR 1: operating-model package** *(blocked)* — delete
  `agentic/operating-model/`, fix any remaining imports.
- [ ] **Delete PR 2: agentic-memory profile code** *(blocked)* —
  delete `profile_support.go`, `profile_context.go`,
  `profile_context_test.go`, `layer_approved_handler.go`,
  `layer_approved_handler_test.go`; surgical-edit `component.go` and
  `config.go`; regenerate schemas.
- [ ] **Delete PR 3: agentic-dispatch onboarding + intent** *(blocked)*
  — delete `onboard_command*.go`, `onboarding_*.go`,
  `intent_classifier*.go`; surgical-edit `config.go`; regenerate
  schemas.
- [ ] **Delete PR 4: agentic-loop profile handler** *(blocked)* —
  delete `profile_context_handler*.go`; surgical-edit `config.go`;
  regenerate schemas.
- [ ] **Step-3b wiring** *(blocked, follow-on)* — wire
  `prompt.Assemble` into
  `handlers.go:303-321 buildInitialMessages` with an injected
  `*prompt.Registry` seeded from `DefaultFragments` +
  `persona.Manager.Fragments()`.
- [ ] **Mock wiring + e2e coverage** *(blocked, follow-on)* — remove
  the "not wired into the task dispatcher" comment at
  `test/e2e/mock/cmd/main.go:88`, add a persona-override scenario
  that asserts the mock sees the override content.
- [ ] **Memory revision** — update
  `memory/project_semteams_consolidation.md` and
  `memory/project_next_session_plan.md`.

## Action for the semteams team

Semstreams cannot delete the personalization code until semteams lands
its own copy and confirms e2e. Semteams owns this workstream; the list
below is written as a self-contained checklist so ADR-030 can be
shared standalone.

- [ ] **Acknowledge ADR-030 direction.** Reply on the ADR PR with a
  target date for completion or a push-back if the timeline is
  unrealistic.

- [ ] **Pick a re-landing strategy.** Two options:
  - *Fork from current semstreams copies* — copy the files listed
    below from semstreams `main` into semteams. Rename packages and
    adjust imports. Clean baseline.
  - *Reconstruct from pre-ADR-025 history* — revert to semteams'
    own copies before ADR-025 deleted them. Preserves pre-existing
    local commits but may miss framework improvements that landed in
    the semstreams copies during Phase 2.

- [ ] **Land the operating-model package in semteams** at a path of
  your choosing (e.g., `agentic/operating-model/` or
  `teams-memory/operating-model/`). Source: `agentic/operating-model/`
  in semstreams, 16 files, 1,918 LOC. Package doc comment at
  `doc.go:1-43`.

- [ ] **Land the onboarding command + interview** (source:
  `processor/agentic-dispatch/onboard_command*.go`,
  `onboarding_interview*.go`, `onboarding_tracker.go` in semstreams;
  5 files, 1,302 LOC). Config gate currently at
  `processor/agentic-dispatch/config.go:21` (`EnableOnboarding`).

- [ ] **Land the intent classifier if you want it** (source:
  `processor/agentic-dispatch/intent_classifier*.go` in semstreams;
  2 files, 368 LOC). Unused in semstreams today; product decides
  whether to keep or rewrite.

- [ ] **Land the profile context handler in your loop layer** (source:
  `processor/agentic-loop/profile_context_handler*.go` in
  semstreams; 2 files, 311 LOC). Config gate at
  `processor/agentic-loop/config.go:56` (`InjectProfileContext`).

- [ ] **Land the memory-side profile plumbing** (source:
  `processor/agentic-memory/profile_support.go`,
  `profile_context*.go`, `layer_approved_handler*.go` in semstreams;
  5 files, 1,034 LOC). Config gate at
  `processor/agentic-memory/config.go:18` (`EnableProfileContext`).

- [ ] **Pick your two product-level injection channels** to decide
  how the onboarding preamble reaches the LLM in the
  post-extraction world. Options documented in "System-message
  injection" above: `TaskMessage.Context.Content` (rule action
  publishes a rendered preamble) or persona KV (operating-model
  answers map to stored personas). Both are supported by semstreams
  with no framework changes needed.

- [ ] **Confirm semteams e2e green** against semstreams `main` in its
  pre-delete state. This is the "ready" signal semstreams is
  waiting on.

- [ ] **Bump semstreams dependency** after semstreams delete PRs
  land and verify e2e green against the post-delete tag.

## Out of scope

- No code changes in this PR.
- No actual deletion of any extraction-scope file in this PR.
- No persona-assembler wiring in the agent loop — blocked until the
  extraction lands and the `DefaultFragments` shrink is done.
- Step-3b plumbing commit `971de1b` stays.

## Verification

ADR-only; verification is review, not test.

1. **Cross-ADR consistency** — ADR-025's Phase 2 now has an
   amendment banner pointing at ADR-030.
2. **Downstream audit** — before any delete PR, grep
   `semteams` / `semspec` / `openclaw` locally for imports of
   `agentic/operating-model`, `onboard`, `profile_context`. Every
   import must have a semteams-local replacement before the
   corresponding delete PR lands.
3. **Precedent check** — `/kv-or-stream` and `/orchestration-check`
   against "how should a product inject runtime preamble content"
   should both pick one of the two remaining channels
   (`TaskMessage.Context.Content` or persona KV). If either skill
   would pick a third channel the ADR must reconcile.
4. **Schema regeneration** — `task schema:generate` after each
   delete PR; `git diff schemas/ specs/` must be committed with the
   PR.
5. **Memory revision** — `memory/project_semteams_consolidation.md`
   and `memory/project_next_session_plan.md` updated to cite
   ADR-030.

## Consequences

### Positive

- Semstreams regains clean framework identity — no role taxonomy, no
  interview questions, no personalization schema. The
  `feedback_framework_boundary.md` principle becomes enforceable
  going forward.
- System-message injection has two named, bounded channels instead
  of three de-facto channels plus a proposed fourth.
- ADR-029 step 3b wiring lands against a decided boundary, not a
  drifting one.
- Products (semteams, semspec, openclaw) can ship differentiated
  personalization models without coordinating with framework
  releases.

### Negative

- Semteams pays the cost of re-landing ~5,000 LOC it just upstreamed.
  Mitigated by the short interval (one day) and the coordination
  sequence that never breaks semteams' build.
- ADR-025 becomes partially amended within a week of its
  acceptance, which is reputational churn. The counter-argument is
  that correcting a boundary call within a week is cheaper than
  correcting it after products build on it for a quarter.
- `enable_onboarding`, `inject_profile_context`, and
  `enable_profile_context` config fields become gone in a beta
  release. Breaking change; noted in the release notes pointing at
  ADR-030. Consistent with `feedback_framework_boundary.md`'s note
  that beta breaking changes need strong justification — the
  justification here is the boundary correction.

### Neutral

- Intent classifier moves even though it is not obviously
  product-specific. Since it is unused in semstreams today, the
  move costs nothing; if semstreams needs a dispatch-level intent
  classifier later it can be reintroduced framework-neutrally
  under its own ADR.

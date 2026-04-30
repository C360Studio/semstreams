# Migration Guide: beta.25 → beta.26

## Summary

Beta.26 is the **personalization correctness + quality** tag. It
closes six GitHub issues semteams transferred to semstreams once
semteams retired its hard fork and adopted semstreams as a library
(see ADR-025). Four are code fixes/features, two are docs-only.

| # | Title | Disposition |
|---|---|---|
| 14 | Multi-user isolation in `GraphProfileReader` | **Bug fix — security/correctness** |
| 16 | `/onboard` re-run / `ProfileVersion` bump | Fix |
| 11 | LLM-assisted `NormalizeLayerAnswer` | Build |
| 12 | Populate `LessonsLearned` slice | Build |
| 13 | Markdown export (`USER.md`, etc.) | **Closed out-of-scope** + docs page |
| 15 | Proactive scheduling primitive | **Deferred** — ADR-031 |

All four code changes share a single guarantee: **without explicit
wiring, behaviour is identical to beta.25.** The dispatch and memory
components default to `EmptyProfileReader{}` and a stub normalizer.
Activation requires per-component setter calls in flow init. This
migration describes both states (no-wiring vs wired).

Additive surface; no API breakage. No data migration. No payload
schema changes other than `agentic.ContextEvent` gaining an
`omitempty` field. Existing data continues to read back unchanged.

## What changes

### #14 — Multi-user isolation in `GraphProfileReader`

`ReadOperatingModel` previously flat-scanned KV with
`KeysByPrefix("{org}.{platform}.user.teams.om-entry.")` and returned
**every user's** entries. In a multi-user deployment that silently
leaked one user's onboarding entries into another user's
profile-context injection.

**Fix.** Replace the flat scan with the typed graph traversal the
schema already supported:

```text
profile entity --user.operating_model.has_layer--> layer entity
layer entity   --om.layer.has_entry--> entry entity
```

The profile entity ID is user-scoped by construction
(`{org}.{platform}.user.teams.profile.{userID}`), so traversing out
from there guarantees per-user scoping with no change to the writer
side, the entity-ID format, or any on-disk data.

`getState`'s NotFound path also switches to
`errors.Is(err, natsclient.ErrKVKeyNotFound)` so wrapped errors are
detected consistently — fixes a latent bug beyond #14's scope.

**Read-amplification.** New cost is `1 + L + E` point gets (profile +
its layers + its entries). Pre-fix was `1 prefix scan + N gets`
where N was the bucket-wide entry count. Scales with the user's own
profile, not the bucket.

### #16 — `ProfileVersion` bump on `/onboard` re-run

`handleOnboardCommand` previously hardcoded `ProfileVersion: 1` on
every loop. The completion message told the user to re-run `/onboard`
"to refresh," but each refresh collapsed back to version 1.

**Fix.** New `ReadProfileVersion(ctx, org, platform, userID) (int, error)`
on the `ProfileReader` interface (single KV get on the profile
entity). Dispatch component now carries an
`atomic.Pointer[ProfileReader]` with `SetProfileReader` /
`getProfileReader` accessors mirroring agentic-memory's pattern.
`/onboard` calls `nextProfileVersion(ctx, userID)` → returns
`prior + 1` (or `1` on no-prior / unwired reader / read error).

Return contract for `ReadProfileVersion`:

| Result | Meaning |
|---|---|
| `(0, nil)` | No profile yet (KV NotFound). First-time onboard. |
| `(N, nil)`, N > 0 | Persisted version. |
| `(0, error)` | KV transport error or corrupt state. Caller decides whether to surface or fall back. |

The split lets callers distinguish "no prior profile" from "graph
briefly unavailable" — important so a transient KV hiccup during
re-run doesn't silently look like a first-time onboard.

### #11 — LLM-assisted `NormalizeLayerAnswer`

`NormalizeLayerAnswer` was a v1 stub that produced exactly one entry
per freeform answer (`title=first-line, summary=full-text`). All the
structured Entry fields the schema supported — `cadence`, `trigger`,
`inputs`, `stakeholders`, `constraints` — were plumbed for nothing.

**Build.** New `LayerNormalizer` function-shaped extension point on
the dispatch component:

```go
type LayerNormalizer func(ctx context.Context, layer, answer string) ([]operatingmodel.Entry, error)
```

`(*Component).normalizeLayerAnswerWithLLM` is the default
implementation; mirrors `LLMIntentClassifier`'s pattern (resolve
endpoint via `model.RegistryReader`, call
`agenticmodel.Client.ChatCompletion` with a per-layer system prompt,
parse a `{"entries": [...]}` envelope). New `SetLayerNormalizer`
public hook for tests / deployments. Stored as
`atomic.Pointer[LayerNormalizer]` so swapping is safe under
concurrent in-flight onboarding turns.

Per-layer system prompts focus on the relevant Entry fields:

| Layer | Focus |
|---|---|
| `operating_rhythms` | cadence, trigger |
| `recurring_decisions` | cadence, trigger, inputs |
| `dependencies` | stakeholders, inputs |
| `institutional_knowledge` | constraints |
| `friction` | constraints, stakeholders |

**Fallback discipline.** Every non-success path falls back to the
deterministic `NormalizeLayerAnswer` stub: nil normalizer, returned
error, returned empty slice, no model registry, no resolved
endpoint, model client failure, `finish_reason=length` (truncation),
JSON parse failure. Generous on purpose — coarse stub entries beat
rejecting the user's answer because the LLM hiccupped. The
existing `TestNormalizeLayerAnswer_StubShape` regression test still
passes; the function signature is preserved as the fallback.

**Prompt-injection mitigations.**

1. **Length cap.** User answers > `extractionAnswerMaxBytes` (4096
   bytes) are truncated before the call. Bounds both injection blast
   radius and silent token-budget overruns.
2. **Data fence.** User content is wrapped in
   `<<<USER_ANSWER … END_USER_ANSWER>>>` markers and the system
   prompt explicitly tells the model to treat fence content as data,
   not instructions.
3. **Server-side EntryID regeneration.** The LLM is not trusted to
   produce dot-free EntryIDs; `finalizeExtractedEntries` always
   assigns a fresh `om-{layer}-{uuid}` ID and drops entries that
   fail `Entry.Validate()`.

`extractionMaxTokens` is **2048** (sized for 5–10 entries with all
optional fields, plus formatting). `Timeout` is 30s,
`Temperature` is 0.1.

### #12 — Lessons-learned slice population

`ProfileContext.LessonsLearned` was reserved-but-empty.
`SystemPromptPreamble` rendered "## Lessons from prior sessions"
only when content was non-empty, and nothing ever made it non-empty.
Each session's compaction summary was hydrated into the current loop
and then thrown away.

**Build.** Full structural pipeline:

```text
agentic-loop compaction_complete event (now carries UserID)
  → agentic-memory.persistCompactionAsLesson
  → LessonTriples emit on graph.mutation.{loopID}
  → graph-ingest writes to ENTITY_STATES
  → next loop's ReadLessons traversal picks it up
  → AssembleProfileContext renders into LessonsLearned slice
  → SystemPromptPreamble emits "## Lessons from prior sessions"
```

New surface:

- `agentic.ContextEvent.UserID` (omitempty). agentic-loop's
  `maybeCompact` and truncation-retry path populate it from
  `loopManager.GetLoop(loopID).UserID`. Empty UserID is back-compat:
  the consumer treats it as "don't persist a lesson."
- `agentic/operating-model.Lesson` struct + `LessonTriples` writer +
  `LessonEntityID` constructor. Lesson entities live at
  `{org}.{platform}.user.teams.lesson.{lessonID}`, linked from the
  user's profile via the new `user.profile.has_lesson` predicate.
  All lesson triples carry `Source: "lessons_learned"` so observers
  can filter the two streams (lessons vs operating-model).
- `ProfileReader.ReadLessons(ctx, org, platform, userID, limit) ([]Lesson, error)`.
  Returns up to `limit` of the user's lessons, ranked
  most-recent-first by `LearnedAt`. `limit <= 0` defaults to 50.
  `EmptyProfileReader.ReadLessons` returns `(nil, nil)`.
- `AssembleProfileContext` splits `TokenBudget` 75/25 between
  operating-model and lessons-learned slices.
  `ProfileContextInputs` gains `Lessons []operatingmodel.Lesson`.
- `processor/agentic-memory/handlers.go.persistCompactionAsLesson`
  builds a `Lesson{LessonID: "lesson-<8 hex>", Summary,
  SessionID: LoopID, LearnedAt}` per `compaction_complete` event,
  validates, and emits `LessonTriples(...)` via the existing
  `publishGraphMutations` path.

`agenticmemory.ContextEvent` is now a type alias for
`agentic.ContextEvent` so future field drift between the canonical
type and the local mirror is impossible. JSON unmarshal behaviour is
identical.

**Defensive guards.** `persistCompactionAsLesson` short-circuits
without constructing entity IDs when `event.UserID == ""`,
`event.Summary == ""`, or `c.platform.Org` / `c.platform.Platform`
is empty. The platform guard prevents a panic in `mustValidatePart`
that would otherwise feed `safeHandleMessage`'s recover → nak →
NATS redeliver loop → DLQ.

**Token budget contract.** `splitTokenBudget(total)`:

- `total <= 0` → `(0, 0)`. Pass-through.
- `total >= 1` → `lessons = floor(0.25 * total)`, `om = total - lessons`.
- Invariant `lessons + om == total` always holds for `total > 0`.
- For `total ∈ {1, 2, 3}` lessons rounds to 0; operating-model gets
  the whole budget. The renderer's at-least-one contract still emits
  content if entries exist.

### Predicate namespace summary

| Namespace | Owner | Source field |
|---|---|---|
| `om.entry.*` | Operating-model entries | `"operating_model"` |
| `om.layer.*` | Operating-model layer checkpoints | `"operating_model"` |
| `user.operating_model.*` | Profile-side OM relationships | `"operating_model"` |
| `user.profile.has_lesson` | Profile-side lesson relationship | `"lessons_learned"` |
| `user.lesson.*` | Lesson entity attributes | `"lessons_learned"` |

Observers filtering by `Source` can subscribe to either or both
streams cleanly.

### Documentation additions (#13, #15)

- **#13 (markdown export)**: closed out-of-scope. The framework
  already provides the moving parts — rule `publish` action plus
  output components — and rendering specific markdown shapes is a
  product concern. New `docs/concepts/18-rule-driven-artifacts.md`
  documents the canonical `rule → publish → output-component`
  pattern with worked examples (JSON snapshots, markdown rendering
  via `publish_agent`). Cross-linked from
  `docs/concepts/14-orchestration-layers.md`.
- **#15 (proactive scheduling)**: deferred to ADR-031. Captures the
  cron-rule-type vs dedicated-scheduler-component vs
  defer-to-product design space. Records the lean (cron rule,
  Option A) and the implementation-pressure triggers that should
  reopen the ADR. Trigger-text parsing flagged as a separate
  decision.

## What is NOT changing

- **Entity-ID formats.** Profile, layer, entry, and lesson all keep
  their pre-beta.26 shapes. Existing data reads back unmodified
  through the new traversal.
- **Predicate names.** All `om.*` and `user.operating_model.*`
  predicates are unchanged. Lesson predicates are net-new under
  `user.lesson.*` and `user.profile.has_lesson`.
- **`ProfileContext` payload shape.** `LessonsLearned` already had
  its `ProfileContextSlice` reserved; beta.26 just makes it
  non-empty.
- **`SystemPromptPreamble` rendering.** Section headers and
  conditional-on-non-empty rendering unchanged.
- **`LayerApproved` payload.** `ProfileVersion` field already
  existed; #16 just writes a non-stale value.
- **`/onboard` rejection-while-active behaviour.** An active loop
  on the same channel still blocks.
- **`NormalizeLayerAnswer(layer, answer)` function signature.**
  Preserved as the deterministic stub fallback.
- **`agentic.ContextEvent`** Validate/Schema. UserID is omitempty,
  not required.
- **`LLMExtractor.ExtractFacts`.** Still the no-op stub it has
  always been. Beta.26 takes the deterministic path
  (one lesson per compaction summary). When `ExtractFacts` is
  wired with a real LLM client, fine-grained insights will flow
  through the same `LessonTriples` writer without further changes.

## What is explicitly deferred

- **Marking prior-version OM entries `status=superseded`.** Re-runs
  add fresh-UUID entries; old ones remain `status=active`. Reader-
  side filtering on `user.operating_model.version` works as a
  workaround.
- **`/onboard --layer <name>` shortcut.** Re-runs walk all 5 layers
  today.
- **Wiring `LLMExtractor` with a real LLM client.** Fine-grained
  lesson-from-compaction-summary extraction stays out of scope.
- **Lesson categories / tags / supersession / dedup.** Lessons
  accumulate monotonically by `LearnedAt`. Dedup is a future tag.
- **Production wiring of `SetProfileReader` / `SetLayerNormalizer`.**
  Both default to safe stubs. Activation is one wiring call per
  component in flow init.
- **Cadence-driven dispatch.** ADR-031 captures the design space;
  no code today.

## Operational impact

### Without wiring (default — observably identical to beta.25)

A deployment that doesn't call any of:

- `agenticmemory.SetProfileReader(...)`
- `agenticdispatch.SetProfileReader(...)`
- `agenticdispatch.SetLayerNormalizer(...)`

sees no behaviour change. Default values:

| Setter | Default | Behaviour |
|---|---|---|
| `agenticmemory` profile reader | `EmptyProfileReader{}` | Returns nil; OM + lessons slices stay empty. |
| `agenticdispatch` profile reader | `EmptyProfileReader{}` | `nextProfileVersion` returns 1 every time. |
| `agenticdispatch` layer normalizer | `normalizeLayerAnswerWithLLM` | Method exists but degrades to stub when no model registry / endpoint resolves. |

The bug fix in #14 is also opt-in in this sense: it's only
observable through the same `SetProfileReader` wiring.

### With wiring

```go
reader, err := operatingmodel.NewGraphProfileReader(
    ctx, natsClient, "ENTITY_STATES", logger)
// ...
agenticMemoryComponent.SetProfileReader(reader)
agenticDispatchComponent.SetProfileReader(reader)
// LayerNormalizer is wired by the dispatch constructor; override only for tests.
```

With this in place:

- **#14 closes the leak.** Each user's `ReadOperatingModel` returns
  only their own entries.
- **#16 bumps versions.** First `/onboard` → version 1. Each
  re-run (after the prior loop reaches terminal state) → prior + 1.
  `LayerApproved.ProfileVersion` carries the bumped value into the
  graph writer.
- **#11 produces structured entries.** Multi-fact answers fan out
  into multiple Entry rows with `cadence` / `trigger` populated.
- **#12 populates lessons.** Each `compaction_complete` event with
  a UserID produces a Lesson on the user's profile. Subsequent
  `loop_created` events render lessons into the system-prompt
  preamble's "## Lessons from prior sessions" section.

### Read-error handling

`ReadProfileVersion` errors → dispatch logs `Warn` and falls back to
1. `ReadLessons` errors are non-fatal → memory logs `Warn` and
renders the operating-model slice without lessons. A transient KV
hiccup degrades injection quality, never blocks the loop.

### Cost / latency

- **#14 traversal**: 1 + L + E KV gets per call (profile + layers +
  entries reachable from profile). For typical L=5 / E=5–20 this is
  10× *fewer* round-trips than the pre-fix flat scan over a
  multi-user bucket.
- **#16 version read**: 1 KV get per `/onboard` invocation.
- **#11 LLM call**: one call per layer (5 layers per onboarding
  interview). MaxTokens 2048, Timeout 30s. Health-policy gated
  (beta.15 circuit breaker).
- **#12 lessons read**: 1 KV get per lesson (capped at
  `defaultLessonsLimit = 50`). Lesson persist on every
  `compaction_complete` event with a UserID.

## Migration impact for products

- **Custom `ProfileReader` implementations** must add two new methods:
  `ReadProfileVersion(ctx, org, platform, userID) (int, error)` and
  `ReadLessons(ctx, org, platform, userID, limit) ([]Lesson, error)`.
  Recommended fallbacks: `(0, nil)` and `(nil, nil)` respectively to
  preserve old behaviour. For `ReadProfileVersion`, propagate
  transport errors as `(0, err)` so callers can distinguish "no
  profile" from "graph unavailable."
- **Tests previously assigning `c.normalizerFn = nil`** should call
  `c.SetLayerNormalizer(nil)` instead — the field is now
  `atomic.Pointer[LayerNormalizer]`.

## Verification

```bash
# Unit tests + race detector
go test -race ./...

# Lint (no new warnings)
task lint

# Schema regen — confirm no drift
task schema:generate
git diff schemas/ specs/openapi.v3.yaml

# Integration test for #14 (requires Docker)
go test -race -tags=integration \
  -run TestIntegration_ReadOperatingModel_MultiUserIsolation \
  ./agentic/operating-model/...
```

Manual end-to-end (deployment with `SetProfileReader` wired):

- **#14**: Onboard two users; each user's `/loops/{id}` profile
  context contains only their own entries.
- **#16**: Run `/onboard` twice on the same user (let the first
  complete). `user.operating_model.version` on the profile entity
  reads `2`.
- **#11**: Answer a layer with multiple facts ("weekly planning
  Mondays, daily standups 10am, biweekly Friday reviews"). The
  approval prompt lists three distinct entries, each with `cadence`
  / `trigger` populated.
- **#12**: Run a loop long enough to trigger compaction (low
  context limit, long task). Start a follow-up loop. Inspect the
  rendered `ProfileContext.SystemPromptPreamble()` for "## Lessons
  from prior sessions" with the prior compaction summary as the
  bulleted body.

## Related

- GitHub issues: #11, #12, #13, #14, #15, #16 (semteams transfer)
- Plan: `~/.claude/plans/semteams-just-moved-6-playful-rose.md`
- ADR-024 layered LLM timeouts
  (`docs/adr/024-layered-llm-timeouts.md`)
- ADR-025 semteams consolidation
  (`docs/adr/025-semteams-consolidation.md`)
- ADR-028 orchestration architecture
  (`docs/adr/028-orchestration-architecture.md`)
- ADR-031 time-trigger primitive (Proposed)
  (`docs/adr/031-time-trigger-primitive.md`) — answers #15
- Concept: rule-driven artifacts
  (`docs/concepts/18-rule-driven-artifacts.md`) — answers #13

### Source

- `agentic/events.go` — `ContextEvent.UserID`
- `agentic/operating-model/graph_reader.go` —
  `ReadOperatingModel` traversal, `ReadProfileVersion`, `ReadLessons`
- `agentic/operating-model/reader.go` — interface additions
- `agentic/operating-model/lesson.go` — new file
- `agentic/operating-model/entity_ids.go` — `LessonEntityID`
- `agentic/operating-model/constants.go` — lesson predicates
- `processor/agentic-dispatch/component.go` — `SetProfileReader`,
  `SetLayerNormalizer`
- `processor/agentic-dispatch/onboard_command.go` —
  `nextProfileVersion`
- `processor/agentic-dispatch/normalize_extractor.go` — new file
- `processor/agentic-loop/handlers.go` — `lookupLoopUserID` +
  UserID stamping on context events
- `processor/agentic-memory/handlers.go` —
  `persistCompactionAsLesson`
- `processor/agentic-memory/profile_context.go` — budget split,
  `renderLessonsSlice`

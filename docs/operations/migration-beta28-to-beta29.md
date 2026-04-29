# Migration Guide: beta.28 → beta.29

## Summary

Beta.29 closes the v1-stub gap on `ProfileContext.LessonsLearned`
(issue #12). Pre-beta.29 the slice was reserved-but-empty —
`SystemPromptPreamble` rendered "## Lessons from prior sessions"
only when content was non-empty, and nothing ever made it
non-empty.

The fix adds the full structural pipeline:

```text
agentic-loop compaction_complete event (now carries UserID)
  → agentic-memory.persistCompactionAsLesson
  → LessonTriples emit on graph.mutation.{loopID}
  → graph-ingest writes to ENTITY_STATES
  → next loop's profile context assembly:
      GraphProfileReader.ReadLessons traverses
        profile --user.profile.has_lesson--> lesson entity
      → AssembleProfileContext renders into
        ProfileContext.LessonsLearned
  → SystemPromptPreamble emits "## Lessons from prior sessions"
```

Each compaction produces one lesson whose `Summary` is the
compaction summary text the loop generated. Future tags can fold
in fine-grained insights from `LLMExtractor.ExtractFacts` (still
a stub today) — the framework will route them through the same
`LessonTriples` writer without further changes.

## What changes

### `agentic.ContextEvent` carries `UserID`

```go
type ContextEvent struct {
    Type        string  `json:"type"`
    LoopID      string  `json:"loop_id"`
    UserID      string  `json:"user_id,omitempty"` // NEW
    // ...
}
```

`agentic-loop`'s `maybeCompact` and the truncation-retry path
populate `UserID` from `loopManager.GetLoop(loopID).UserID`. The
field is `omitempty` so old publishers that don't set it are
back-compat (the new agentic-memory code treats empty UserID as
"don't persist a lesson").

### New `Lesson` payload + `LessonTriples` writer

`agentic/operating-model/lesson.go`:

```go
type Lesson struct {
    LessonID  string    `json:"lesson_id"`
    Summary   string    `json:"summary"`
    SessionID string    `json:"session_id,omitempty"`
    LearnedAt time.Time `json:"learned_at"`
}

func LessonTriples(ref ProfileRef, l Lesson) []message.Triple
```

Lesson entities live at
`{org}.{platform}.user.teams.lesson.{lessonID}` and are linked
from the user's profile entity via
`PredicateProfileHasLesson` (`user.profile.has_lesson`). All
lesson triples carry `Source: "lessons_learned"` so observers
can distinguish them from operating-model triples (`Source:
"operating_model"`).

New predicates in `agentic/operating-model/constants.go`:

| Predicate | Purpose |
|---|---|
| `PredicateProfileHasLesson` (`user.profile.has_lesson`) | Profile-side relationship to a lesson |
| `PredicateLessonSummary` (`user.lesson.summary`) | Lesson body text |
| `PredicateLessonSessionID` (`user.lesson.session_id`) | Originating loop ID |
| `PredicateLessonLearnedAt` (`user.lesson.learned_at`) | RFC3339 wall-clock time |

### `ProfileReader.ReadLessons` interface method

```go
ReadLessons(ctx context.Context, org, platform, userID string, limit int) ([]Lesson, error)
```

Returns up to `limit` of the user's lessons, ranked
most-recent-first by `LearnedAt`. `limit <= 0` defaults to 50
(implementation cap; the assembler's budget normally truncates
well below that). `EmptyProfileReader.ReadLessons` returns `(nil,
nil)`.

`GraphProfileReader.ReadLessons` traverses the user's profile
through `has_lesson` edges, fetches each lesson entity, sorts
by `LearnedAt` descending, and caps at `limit`. Same isolation
guarantees as the beta.26 fix to `ReadOperatingModel`.

**Migration impact for products:** any custom implementation of
`ProfileReader` must add `ReadLessons`. The recommended fallback
returns `(nil, nil)` to preserve old behaviour.

### `AssembleProfileContext` splits TokenBudget 75/25

The total `TokenBudget` (default 800) is split:

- **75%** (default 600) for the operating-model slice
- **25%** (default 200) for the lessons-learned slice

`splitTokenBudget(total)` enforces a minimum of 1 token per
slice when the total is positive but tiny, and passes
zero/negative through unchanged.

Each slice's renderer follows the same "at least one entry
renders even if oversized" contract that
`renderOperatingModelSlice` has always had.

`ProfileContextInputs` now carries `Lessons
[]operatingmodel.Lesson`:

```go
type ProfileContextInputs struct {
    UserID         string
    LoopID         string
    ProfileVersion int
    Entries        []operatingmodel.Entry
    Lessons        []operatingmodel.Lesson  // NEW
    TokenBudget    int
    Now            time.Time
}
```

Existing callers that don't set `Lessons` keep their
operating-model rendering unchanged; the lessons slice stays
empty.

### Type alias for compaction events (no drift)

`agenticmemory.ContextEvent` is now a type alias for
`agentic.ContextEvent`:

```go
type ContextEvent = agentic.ContextEvent
```

The previous mirror declaration silently drifted from the
canonical struct twice (this tag's UserID add being the second
instance). The alias makes future drift impossible while keeping
the local-package name `agenticmemory.ContextEvent{...}` that
integration tests rely on.

### Compaction handler persists summaries as lessons

`processor/agentic-memory/handlers.go`'s
`handleCompactionComplete` now calls
`persistCompactionAsLesson(event)` after the existing post-
compaction hydration. The new helper short-circuits in three
ways before constructing any entity ID:

1. Empty `event.UserID` → no-op (system-initiated loops, or
   pre-beta.29 publishers that don't set the field).
2. Empty `event.Summary` → no-op (no payload to persist).
3. Empty `c.platform.Org` or `c.platform.Platform` → no-op with
   a Debug log. Without this guard, `LessonEntityID` /
   `ProfileEntityID` would panic in `mustValidatePart`; the
   panic would feed `safeHandleMessage`'s recover → nak → NATS
   redeliver loop → DLQ. Matches the identical guard in
   `layer_approved_handler.go`.

When all three guards pass, the helper:

1. Builds a `Lesson{LessonID: "lesson-<8 hex>", Summary:
   event.Summary, SessionID: event.LoopID, LearnedAt: now}`.
2. Validates and emits `LessonTriples(...)` via the existing
   `publishGraphMutations` path.

Publish failures are logged at `Warn` and not retried —
post-hydration has already succeeded for the current loop, and a
missed lesson degrades future-session quality, not current
correctness.

### `splitTokenBudget` tiny-budget contract

For totals 1–3 the 25% lessons share rounds to 0 and the
operating-model slice claims the whole budget. The renderer's
at-least-one contract still emits content if entries exist; the
invariant `lessons + om == total` always holds. The original
implementation tried to bump both shares to a minimum of 1 for
tiny totals, which violated the invariant and could underflow
either share to zero in a confusing way.

## What is NOT changing

- **`ProfileContext` payload shape** — unchanged.
  `LessonsLearned` already had its `ProfileContextSlice` shape
  reserved; beta.29 just makes it non-empty.
- **`SystemPromptPreamble` rendering** — unchanged. The
  "## Lessons from prior sessions" section was always rendered
  conditionally on non-empty content; the preamble's structure
  stays the same.
- **Operating-model triple shape** — unchanged. The 75% budget
  share is still ample for typical 5-layer profiles.
- **`LLMExtractor.ExtractFacts`** — still a stub. Beta.29 takes
  the deterministic path (one lesson per compaction summary).
  When `ExtractFacts` is later wired with a real LLM client,
  fine-grained insights can flow through the same
  `LessonTriples` writer.

## What is explicitly deferred

- **Wiring `LLMExtractor` with a real LLM client** — out of
  scope. The class is still a no-op stub
  (`LLMExtractor.llmClient == nil`).
- **Fine-grained lesson categories / tags** — `Lesson` carries
  only `Summary` + `SessionID` + `LearnedAt`. Categories
  (`blocker`, `pattern`, `tool`, etc.) are a future schema
  extension.
- **Lesson supersession / dedup** — lessons accumulate
  monotonically. Two consecutive compactions on the same loop
  produce two lessons. Reader-side dedup or supersession is a
  future tag.
- **Wiring a `GraphProfileReader` into agentic-memory's
  `SetProfileReader`** — same deferred wiring as beta.27.
  Without that wiring, `EmptyProfileReader` returns
  `(nil, nil)` from `ReadLessons` and the slice stays empty in
  the rendered preamble (identical to pre-beta.29 behaviour).
  Activating lessons end-to-end requires the same one-line
  wiring that activates the operating-model slice.

## Operational impact

### Without wiring

A deployment that hasn't called `agentic-memory.SetProfileReader`
sees `LessonsLearned` stay empty (Empty reader returns nil).
Identical observable behaviour to beta.28.

### With wiring

A deployment that wires
`operatingmodel.NewGraphProfileReader(ctx, natsClient,
"ENTITY_STATES", logger)` and calls `memoryComponent.
SetProfileReader(reader)`:

- Each compaction_complete event with a UserID produces a Lesson
  triple-set on the user's profile.
- The next loop's `loop_created` event triggers
  `assembleProfileContextFromGraph`, which now reads lessons
  and renders them into the system prompt's "## Lessons from
  prior sessions" section.
- Lessons accumulate across sessions; ranking by recency keeps
  the freshest insights visible.

### Cost / latency

`ReadLessons` adds one KV get per lesson on top of the existing
profile read. For a typical user accumulating 5–20 lessons, the
read amplification is bounded and bounded again by `defaultLessonsLimit
= 50`.

## Verification

```bash
# Unit tests (agentic, agentic/operating-model, agentic-memory,
# agentic-dispatch, agentic-loop)
go test -race ./...

# Lint
task lint

# Schema regen — ContextEvent shape change is captured in
# regenerated schemas; confirm no other drift.
task schema:generate
git diff schemas/ specs/openapi.v3.yaml
```

Manual: in a deployment with both readers wired, run an agent
loop long enough to trigger compaction (set a low context
limit or send a long task), then start a follow-up loop and
inspect the rendered `ProfileContext.SystemPromptPreamble()`
for "## Lessons from prior sessions" with the prior
compaction summary as the bulleted body.

## Related

- GitHub issue: #12 (semteams)
- Plan: `~/.claude/plans/semteams-just-moved-6-playful-rose.md`
- Sibling fix: beta.28 LLM-assisted normalization
  (`migration-beta27-to-beta28.md`)
- Multi-user isolation pattern reused: beta.26
  (`migration-beta25-to-beta26.md`)
- Source:
  - `agentic/events.go` (`ContextEvent.UserID`)
  - `agentic/operating-model/lesson.go` (new)
  - `agentic/operating-model/constants.go` (predicates)
  - `agentic/operating-model/entity_ids.go` (`LessonEntityID`)
  - `agentic/operating-model/graph_reader.go` (`ReadLessons`)
  - `agentic/operating-model/reader.go` (interface)
  - `processor/agentic-memory/handlers.go`
    (`persistCompactionAsLesson`)
  - `processor/agentic-memory/profile_context.go`
    (budget split + `renderLessonsSlice`)
  - `processor/agentic-loop/handlers.go`
    (`lookupLoopUserID` + UserID stamping on context events)

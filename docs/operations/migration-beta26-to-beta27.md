# Migration Guide: beta.26 → beta.27

## Summary

Beta.27 closes the `/onboard` re-run version-bump gap (issue #16).
Pre-beta.27, `processor/agentic-dispatch.handleOnboardCommand`
hardcoded `ProfileVersion: 1` in the new loop's metadata on every
run, regardless of whether the user had a prior completed
profile. The completion message even told the user to re-run
`/onboard` to refresh, but each refresh produced new
`user.operating_model.version` triples that all read as `1` —
collapsing the version history.

The fix wires a `ProfileReader` into the dispatch component
(mirroring the agentic-memory pattern from prior tags) and reads
the prior profile version before stamping the new loop's
metadata. Re-runs now bump monotonically: 1 → 2 → 3 → … with the
bumped value flowing through `LayerApproved.ProfileVersion` to the
graph writer.

Additive surface; no API breakage. No data migration. No schema
changes.

## What changes

### New `ProfileReader.ReadProfileVersion` interface method

`agentic/operating-model.ProfileReader` gained one method:

```go
ReadProfileVersion(ctx context.Context, org, platform, userID string) (int, error)
```

`GraphProfileReader.ReadProfileVersion` issues a single KV get on
the user's profile entity and reads the
`user.operating_model.version` triple. Cheaper than
`ReadOperatingModel` for callers that only need the version.

Return contract:

| Result | Meaning |
|---|---|
| `(0, nil)` | No profile exists for this user yet (KV NotFound). Caller treats as first-time onboard. |
| `(N, nil)` for `N > 0` | Persisted version. |
| `(0, error)` | KV transport error or corrupt state. Caller surfaces the failure (in dispatch, this triggers a Warn-and-fall-back-to-1). |

This split lets callers distinguish "no prior profile" from
"graph briefly unavailable" — important so a one-off KV
hiccup during a re-run doesn't silently look like a first-time
onboard.

`EmptyProfileReader.ReadProfileVersion` returns `(0, nil)`.

**Migration impact for products:** any custom implementation of
the `ProfileReader` interface must add the new method. The
recommended pattern: return `(0, nil)` for absent-profile cases,
and propagate transport errors as `(0, err)` so callers can
distinguish the two.

### `agenticdispatch.Component` wires a `ProfileReader`

The dispatch component now carries an
`atomic.Pointer[operatingmodel.ProfileReader]` field, defaulting
to `EmptyProfileReader{}` at construction. New public methods:

- `(*Component).SetProfileReader(reader operatingmodel.ProfileReader)`
  — production wiring entry point. Passing `nil` restores the
  empty default; the component never holds a `nil` reader.

Production deployments that want re-run version bumps to actually
work must call `SetProfileReader` with a real
`operatingmodel.NewGraphProfileReader(...)` instance during flow
init. Without that wiring, the dispatch component falls back to
the empty reader (returns `0`) and `/onboard` stamps version `1`
on every run — i.e. **identical to pre-beta.27 behaviour**. The
bump is opt-in through wiring.

### `/onboard` reads prior version

`handleOnboardCommand` calls a new helper:

```go
profileVersion := c.nextProfileVersion(ctx, msg.UserID)
```

`nextProfileVersion` calls `ReadProfileVersion` on the wired
reader and returns `prior + 1` (or `1` if no prior, or on error
— the read is best-effort and must not block `/onboard` from
starting). The bumped version is stamped on the loop's metadata
under the existing `OnboardMetaProfileVersion` key, which
`onboardingApproveLayer` already reads when constructing the
`LayerApproved` payload.

## What is NOT changing

- **Existing `OnboardMetaProfileVersion` key + accessor** —
  unchanged. The fix changes only the value written, not where
  it lives.
- **`LayerApproved` shape** — unchanged. The `ProfileVersion`
  field already existed; beta.27 just writes a non-stale value.
- **`/onboard` rejection-while-active behaviour** — unchanged.
  An active loop on the same channel still blocks.
- **EntityID format for entries** — unchanged (`om-{layer}-{uuid}`).
  New-version entries get fresh UUIDs and live alongside prior
  entries in the graph.
- **Triple writer** — `agentic/operating-model.LayerTriples` was
  already plumbed for any `Version` value; no change needed.

## What is explicitly deferred

- **Marking prior-version entries `status=superseded`** — the
  plan (`semteams-just-moved-6-playful-rose.md`) called this out
  as a follow-up. Implementing it cleanly requires either a
  per-entry version triple at write time (writer-side change) or
  a query-then-update flow during onboarding. Since prior entries
  remain in the graph with `status=active` from their original
  run, downstream consumers that want only "current-version"
  entries should filter on `user.operating_model.version` at read
  time until the supersede pass is added in a future tag.
- **`/onboard --layer <name>` shortcut** — also deferred. Re-runs
  today walk all 5 layers; targeted layer refresh is a future
  affordance.

## Operational impact

### Without wiring

A deployment that doesn't call `SetProfileReader` sees no
behaviour change — `/onboard` still always stamps version `1`,
identical to beta.26. The fix is structurally complete; activating
it requires one wiring call per dispatch instance.

### With wiring

A deployment that wires
`operatingmodel.NewGraphProfileReader(ctx, natsClient,
"ENTITY_STATES", logger)` and calls
`dispatchComponent.SetProfileReader(reader)`:

- First-time `/onboard` on a user → version `1`.
- Each subsequent `/onboard` after the prior loop completed →
  version `prior + 1`.
- Each `LayerApproved` event carries the bumped version, which
  the graph writer stamps onto every triple it emits.

Reading `user.operating_model.version` from the user's profile
entity returns the most recent (highest) version after every
re-run.

### Read-error handling

`ReadProfileVersion` failures (graph down, KV unreachable) log a
`Warn` and fall back to `1`. The first re-run after a KV outage
will collapse to version `1` rather than the correct `prior +
1` — this is intentional: a stale version is better than blocking
a user from re-onboarding because the graph is briefly
unavailable. The next successful re-run will bump correctly off
whatever ended up persisted.

## Verification

```bash
# Unit tests (includes new TestHandleOnboardCommand_RerunBumpsVersion
# and TestReadProfileVersion)
go test -race ./agentic/operating-model/... ./processor/agentic-dispatch/...

# Lint
task lint

# Schema regen unchanged
task schema:generate
git diff schemas/ specs/openapi.v3.yaml
```

Manual: in a deployment with a wired `ProfileReader`, run
`/onboard` twice on the same user (completing the first run
between calls), and assert via `gh api` /
graph-gateway / GraphQL that
`user.operating_model.version` on the profile entity reads `2`,
not `1`.

## Related

- GitHub issue: #16 (semteams)
- Plan: `~/.claude/plans/semteams-just-moved-6-playful-rose.md`
- Sibling fix: beta.26 multi-user isolation
  (`migration-beta25-to-beta26.md`)
- Source:
  - `agentic/operating-model/reader.go` (interface change)
  - `agentic/operating-model/graph_reader.go`
    (`ReadProfileVersion`)
  - `processor/agentic-dispatch/component.go`
    (`SetProfileReader`)
  - `processor/agentic-dispatch/onboard_command.go`
    (`nextProfileVersion`)

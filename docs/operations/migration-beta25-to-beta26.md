# Migration Guide: beta.25 → beta.26

## Summary

Beta.26 is a **security/correctness** tag closing a multi-user
data-isolation bug in `agentic/operating-model.GraphProfileReader`
that semteams flagged as issue #14. Pre-beta.26,
`ReadOperatingModel` flat-scanned the KV bucket for every key
matching `{org}.{platform}.user.teams.om-entry.` and returned
every match, regardless of which user's profile owned the
entry. In a multi-user deployment this silently leaked one
user's onboarding entries into another user's profile-context
injection.

The fix swaps the flat scan for the typed graph traversal the
schema already supported:

```text
profile entity --user.operating_model.has_layer--> layer entity
layer entity   --om.layer.has_entry--> entry entity
```

The profile entity ID is user-scoped by construction
(`{org}.{platform}.user.teams.profile.{userID}`), so traversing
out from there guarantees per-user scoping without any change
to the writer side, the entity-ID format, or the on-disk data.

Additive surface; no API breakage. No data migration. No
configuration changes. Existing entry data continues to work
unmodified — the writer was already emitting both
`has_layer` and `has_entry` triples.

## What changes

`ReadOperatingModel(ctx, org, platform, userID)`:

| | Before (beta.25) | After (beta.26) |
|---|---|---|
| Method | `kv.KeysByPrefix("{org}.{platform}.user.teams.om-entry.")` | Walk profile → has_layer → has_entry |
| Scope | All entries with the prefix (all users) | Only entries reachable from this user's profile |
| KV reads | 1 prefix scan + N entry gets | 1 profile + L layer gets + E entry gets (typically L=5, E=number of entries) |
| Failure modes | Flat scan returns silently if bucket has unrelated entries | Profile-not-found → empty result; missing layer/entry → skipped with debug log |

`getState`'s NotFound path now uses `errors.Is(err,
natsclient.ErrKVKeyNotFound)` instead of
`natsclient.IsKVNotFoundError(err)` so wrapped errors are
detected consistently.

## What is NOT changing

- **Entry entity-ID format** — still
  `{org}.{platform}.user.teams.om-entry.{entryID}` where
  `entryID = om-{layer}-{uuid}`. Existing data is read by the
  new traversal without rewrites.
- **Predicate names** — `user.operating_model.has_layer` and
  `om.layer.has_entry` were already defined and emitted by
  `agentic/operating-model.LayerTriples`. The traversal reads
  what the writer was already producing.
- **`ProfileReader` interface** — same signature, same
  semantics, same `ProfileResult` shape.
- **`NewGraphProfileReader` constructor** — unchanged.
- **`EmptyProfileReader`** — unchanged.

## Operational impact

### Before deploying beta.26

Multi-user deployments that had been running pre-beta.26 saw
profile-context injection that mixed users' operating-model
entries together. After beta.26 each user's
`ProfileContext.OperatingModel` will contain only their own
entries. Expect the rendered system-prompt preamble to shrink
for users whose profile was being padded with other users'
data.

The bug was largely invisible in single-user deployments and in
deployments where every user was on the same physical KV bucket
with no cross-user reads — so most teams will see no behavior
change beyond the leak being closed.

### Read-amplification

The new traversal does more KV gets than the old prefix scan
when a profile has many layers and entries: roughly `1 + L +
E` reads vs. `1 + N` (where N is the global entry count). For
typical profiles (L=5 canonical layers, E ≈ 5–20 entries) this
is an order-of-magnitude *reduction* in reads compared to the
flat scan over a multi-user bucket, since it stops touching
other users' data.

The `getState` path is read-only and uses the same
`KVStore.Get` plumbing as before.

### Defensive deduplication

If two layers happen to reference the same entry entity (which
the writer should never produce), the new reader returns the
entry once. Migration data with bad shape is therefore handled
silently rather than returning duplicates.

## Verification

```bash
# Unit tests
go test -race ./agentic/operating-model/...

# Integration test (requires Docker — testcontainers spins up real NATS)
go test -race -tags=integration -run TestIntegration_ReadOperatingModel_MultiUserIsolation ./agentic/operating-model/...

# Lint
task lint
```

The unit-test suite includes
`TestReadOperatingModel_MultiUserIsolation` which writes two
users' profiles to a fake KV and asserts each `ReadOperatingModel`
call returns only that user's entries. The integration test
runs the same scenario against a real NATS KV bucket via
testcontainers.

## Related

- GitHub issue: #14 (semteams)
- Memory:
  `project_multi_user_profile_isolation.md` (to be added)
- Plan: `~/.claude/plans/semteams-just-moved-6-playful-rose.md`
- Source: `agentic/operating-model/graph_reader.go`

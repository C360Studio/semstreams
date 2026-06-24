---
name: tag-release
description: Cut a semstreams release tag the house way — preflight green, pre-tag build-tag sweep, e2e-before-breaking, compute the next beta version, annotated tag in house format, push, never re-tag. User-invoked only (tagging is irreversible and outward-facing).
argument-hint: [optional: explicit version like v1.0.0-beta.NNN, else next is computed]
disable-model-invocation: true
---

# Tag a release

Tagging is **irreversible** (the Go module proxy pins a version on first fetch — `feedback_never_retag`)
and **outward-facing** (for a breaking tag, sister repos conform against it). Do every step; never
skip to the tag. Confirm the version with the human before pushing.

## Step 1 — Preconditions

```bash
git checkout main && git pull origin main --ff-only   # on main, up to date
git status -s                                          # working tree CLEAN
```

If not on main / not clean, stop. Tags go on a merged `main` commit, not a branch.

## Step 2 — Gates green

Run `/preflight` (or `task check:push`) — must be fully green on the commit you're about to tag.

**Pre-tag build-tag sweep (mandatory, both tags):**

```bash
go vet -tags=integration ./...
go vet -tags=live_llm ./...
```

Plain `go vet` does NOT cover tagged files; a broken integration/live_llm file ships otherwise
(`feedback_pre_tag_sweep_includes_build_tags`).

## Step 3 — Breaking? → e2e BEFORE the tag (HARD RULE)

If this release contains a BREAKING change (see `/preflight` Step 2 for the test), at least one
relevant **e2e tier must be green before the tag lands** (`feedback_e2e_required_for_breaking_changes`).
Pick the tier by touched path (table in `/preflight`). No green tier → do not tag.

After a registry-retirement / factory+payload-split style migration, grep every binary for the
migrated symbol to confirm none is half-migrated:

```bash
grep -rn "<migrated-symbol>" cmd/    # must appear in cmd/semstreams AND cmd/e2e-semstreams
```

## Step 4 — Compute the version

```bash
git tag --sort=-creatordate | head -3        # current latest, e.g. v1.0.0-beta.114
git tag -l '<candidate>'                       # MUST be empty (available)
```

Bump the beta number by one unless the human gave an explicit `$ARGUMENTS` version. **Confirm the
exact version with the human before proceeding** — this is the one number you can't take back.

## Step 5 — Annotated tag (house format)

Tags here are **annotated** with subject `vX — <short summary>`:

```bash
git tag -a <version> <commit> -m "<version> — <summary>
<optional body: what changed; for BREAKING, name the lockstep + migration doc>"

# verify it points where you think:
git rev-parse <version>^{commit} HEAD          # both hashes must match
```

## Step 6 — Push

```bash
git push origin <version>
```

## Step 7 — After the tag

- **Breaking?** The tag is the lockstep trigger — hand each sister team the migration doc
  (`docs/adr/0NN-*-summary.md`), pinned to the tag URL (a fixed ref won't drift under them). Do NOT
  touch the sister repos yourself unless asked.
- Close the issues the release resolves; record the tag in the relevant project memory.
- If you got the version wrong: **do not move the tag** — cut the next number. Re-tagging a pushed
  version corrupts the module-proxy cache for everyone.

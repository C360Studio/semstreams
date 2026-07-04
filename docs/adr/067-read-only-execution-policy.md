# ADR-067: Read-only execution policy — provable worktree non-mutation for inspect roles

## Status

**Accepted — 2026-07-03.** Scopes gh#443 (SemSpec dogfooding ask: role-scoped
read-only tool execution). Defines the **framework enforcement seam**; the
OS-level containment guarantee is explicitly **deferred to the sandbox
substrate** (ADR-052, still pre-ADR — `docs/proposals/sandbox-substrate.md`).
v1 is shippable now and does not depend on the gated substrate.

**A pre-Accept adversarial code review revised this ADR:** it refuted the
original enforcement seam (the main-path metadata merge is NOT universal — the
approval re-dispatch path dropped the policy) and surfaced three further holes
(git-state laundering, fill-vs-overwrite direction, spawn non-inheritance). The
design below is the corrected version; the crux moved to the shared
`dispatchToolCall` seam. See §Design and §Risks.

## Decision

Add a framework-owned, **task-scoped read-only execution policy**: a well-known
metadata key (`agent.exec.filesystem_policy = read_only`) plus optional
in-worktree scratch-path exemptions, **stamped authoritatively onto every tool
call at the shared `dispatchToolCall` seam** (the same seam that makes `loop_id`
and the run anchor path-universal) and enforced by the `bash` executor as a
**pre + post git worktree-and-HEAD non-mutation proof** returning a typed
violation. The policy rides `Metadata` (never tool `Arguments`) and is stamped by
the framework, so the **model cannot set or unset it**. A product maps its
inspect roles (planner, plan-reviewer) → `read_only` when it spawns those loops.

The policy uses the sandbox substrate's own `filesystem.{read_only,
workspace_write, host_write}` vocabulary and is the **enforcement seam ADR-052's
substrate later backs with OS-level read-only mounts**. v1 proves *net worktree
and HEAD non-mutation* via git — honestly scoped below OS-level containment (see
Non-goals; it is a guard against ordinary and git-state mutation, not against a
determined agent writing outside the worktree or through a laundering primitive
the git proof can't observe). It extends the `bash` executor's existing
`verify_clean` pre-check with the symmetric post-check that gh#443 identifies as
missing — not a new parallel path.

## Context

### The gap (verified against code)

The framework has **two unconnected notions of "read-only," and SemSpec's need
sits between them:**

| Model | "Read-only" means | State (verified) |
|---|---|---|
| Tool categories (`categories.go` `ReadOnlyCategories()`) | which *tools* — excludes `CategoryInspect`, i.e. no `bash` at all | defined but **unwired** (only its own test references it, `categories.go:98`); all-or-nothing |
| Sandbox substrate (ADR-052) | FS write-scope on the *environment* (`filesystem.read_only` IRI + admission pre-check) | **pre-ADR, gated, ~5 weeks stale**; no write-prevention, no worktree proof; runner convergence explicitly out of v1 |

gh#443 needs `bash` **on** (planner/reviewer must inspect the repo, `/sources`,
contracts, and run probes) but its *worktree writes* to be a **typed violation**.
Neither model expresses that.

The closest real primitive is the `bash` executor's `verify_clean` /
`read_only_paths` (`executors/bash.go:145-330`): it runs `git status -z
--porcelain --untracked-files=all` **before** the command and refuses if a dirty
path matches, returning a typed violation via `formatVerifyCleanViolation`. But —
exactly as the issue states — it is **pre-command only**: it proves the tree was
clean *before*, never that the command *left it* unchanged. The missing
**post-condition** is the heart of #443.

### The enforcement seam — and the trap the review caught

Enforcement must be a property of the *task/role*, resolved framework-side on
every call, that the model cannot disable. The vehicle is `ToolCall.Metadata`
("Domain context, propagated from task", `agentic/tools.go:77`) — the model
produces only `Name`/`Arguments`/`ID` (verified across every model→ToolCall
decode site in `processor/agentic-model/`; none writes `Metadata`).

**The trap:** the obvious merge — `TaskMessage.Metadata` → each approved call at
`handlers.go:1096-1110` — is **bound to the main path only** (`GetCachedMetadata`
has exactly one non-test caller). The **approval re-dispatch** path rebuilds a
*bare* `ToolCall{ID, Name, Arguments}` (`approval_response_handler.go:114-124`)
and calls `dispatchToolCall`, which stamps **only** `loop_id`
(`handlers.go:1338`) and the run anchor (`:1351-1356`) — it never re-merges task
metadata. So an **approval-gated** `bash` call loses `filesystem_policy` entirely.
That is reachable and dangerous: the more cautious the operator (bash in the
approval set, `approval_filter.go:40-46`), the bigger the hole.

The same defect is latent today in `MetadataKeyDecideActionAllowlist`: `decide`
resolves its allowlist from `call.Metadata` (`decide.go:295`), so an approved
`decide` call also loses its closed vocabulary. The fix below closes both.

**The correct seam** is `dispatchToolCall` — the comment at `handlers.go:1089-1094`
literally states `loop_id` was centralized there "so every dispatch path (this
main path, approval re-dispatch, and queue dequeue) gets it consistently."
`filesystem_policy` must be stamped at the same seam, and — being a framework
security fact, not domain context — as an **authoritative overwrite**, exactly
like the run anchor (`:1351-1356`), not the fill-only merge (a fill-only merge
would let any pre-existing `Metadata[filesystem_policy]` survive and defeat the
control).

## Why this shape (and why not the alternatives)

- **Not a per-call tool argument** (today's `verify_clean` boolean). Enforcement
  the model can omit is not enforcement.
- **Not the tool-category model.** `ReadOnlyCategories` removes `bash` entirely;
  #443 needs `bash` present but write-scoped. Orthogonal axis — left untouched.
- **Not "wait for the sandbox substrate."** ADR-052 is gated, does only
  *admission* pre-checks, and pushes FS containment onto renderers; it would not
  unblock SemSpec, who pause local work until this path is accepted.
- **Reuse the git proof, extend it pre→post.** The typed-violation machinery
  (`-z` porcelain parse surviving quoted paths, R/C rename-row handling) already
  exists and is tested. v1 adds the symmetric post-check + HEAD pinning.

## Design

### 1. Policy vocabulary (`agentic` package)

Well-known metadata keys, documented in the `MetadataKey*` house style
(`Set by / read by / round-trip note / empty = back-compat`):

```go
// MetadataKeyFilesystemPolicy — a task's filesystem write-scope, flowed to
// write-capable tool executors (v1: bash). Values are the sandbox-substrate
// filesystem enum (ADR-052): "read_only" | "workspace_write". Absent/empty ==
// workspace_write (unchanged, back-compat). Under "read_only" the executor
// enforces a pre+post git worktree-and-HEAD proof and returns a typed violation
// on mutation. Model-uncontrollable: stamped by dispatchToolCall (authoritative
// overwrite), never read from tool Arguments. Set by the product's role→policy
// mapping when spawning an inspect-role loop.
const MetadataKeyFilesystemPolicy = "agent.exec.filesystem_policy"

// MetadataKeyScratchPaths — in-worktree paths EXEMPT from the read_only proof
// (e.g. a declared ".scratch/" build dir). Out-of-worktree paths are exempt
// automatically (git in the worktree never reports them). []string; comes back
// from BaseMessage decode as []any (coerce on access, like the decide
// allowlist). Empty == only out-of-worktree paths are writable.
const MetadataKeyScratchPaths = "agent.exec.scratch_paths"

const (
	FilesystemPolicyReadOnly       = "read_only"       // == vocabulary/sandbox filesystem.read_only
	FilesystemPolicyWorkspaceWrite = "workspace_write" // == filesystem.workspace_write (default)
)
```

Values deliberately match the substrate's enum so there is **one**
filesystem-policy model, not two. `host_write` has no v1 meaning here (an
environment-level concern the substrate owns).

### 2. The enforcement seam (`dispatchToolCall`)

`dispatchToolCall` resolves `filesystem_policy` + `scratch_paths` from
`GetCachedMetadata(loopID)` and stamps them onto `tc.Metadata` as an
**authoritative overwrite**, alongside the existing `loop_id` / run-anchor
stamps. This makes the policy reach **every** dispatch path — main, approval
re-dispatch, queue dequeue — and makes it model-proof. The main-path merge at
`handlers.go:1096-1110` stays for the rest of the domain metadata; the security
keys are additionally pinned here so they cannot be lost or overridden.

**Bundled fix:** stamp `MetadataKeyDecideActionAllowlist` at the same seam — it
has the identical approval-redispatch gap today (an approved `decide` call
silently loses its closed vocabulary). One seam fix, both controls made
path-universal. (Per the "footgun fix surfaces latent bugs" discipline: the
surfaced `decide` gap is fixed in the same PR, not deferred.)

### 3. The read_only invariant (`bash` executor)

Under `filesystem_policy = read_only`, the protected worktree must be **clean
modulo scratch, and on the same HEAD, before AND after** each command:

1. **Pre-capture:** `git status -z --porcelain --untracked-files=all` (dirty set)
   + `git rev-parse HEAD`. If any dirty path is **not** under a scratch exemption,
   **refuse** (an inspect role starts clean; a dirty protected tree at entry is
   itself an anomaly). (The proof pins the worktree and HEAD only; remote-tracking
   refs like `@{u}` are out of scope — moving one via `git fetch` is a documented
   no-remote-mutation non-goal below.)
2. Run the command.
3. **Post-capture:** same. **Violation** if HEAD moved OR any dirty path is not
   under a scratch exemption — the command created, modified, formatted,
   generated, deleted, or committed/reset a protected worktree file.

HEAD pinning catches the common git-state mutations a formatter/generator or an
over-eager agent might trigger (`git commit`, `git reset --hard`, `git checkout
<ref>`) that leave the *worktree* clean. It does **not** catch every laundering
primitive — see Non-goals.

**Violation logic is the COMPLEMENT of `formatVerifyCleanViolation`, not a
reuse of it.** That function treats its path list as *protected* (match ⇒
violation; empty ⇒ every dirty path violates). read_only needs the inverse: a
dirty path violates *unless* it is under scratch (`!pathMatchesAny(path,
scratchPaths)`). The `pathMatchesAny` *matcher* is reused; a new
`formatReadOnlyViolation` supplies the inverted logic and names the violating
paths (auditable facts) with a stable error prefix.

Scratch exemption = `MetadataKeyScratchPaths` (in-worktree) ∪ all out-of-worktree
paths. The correct invariant is **out-of-worktree**, not "/tmp": `git status` in
the worktree never reports paths outside it, so probes there are invisible to the
proof by construction. /tmp is the common case, but a deploy that roots the
worktree *under* /tmp (test worktrees, `t.TempDir()` → `/private/tmp` on macOS)
would see writes under `workDir` reported — so the policy documents "out-of-
worktree," and a product's scratch dirs are declared explicitly.

### 4. Local and remote paths

Both dispatch paths enforce. `verifyCleanLocal` / `verifyCleanRemote` already
implement the pre-check against the local workdir and the remote runner's
container; v1 adds their post-check + HEAD-pin twins. **Implementation note:** the
local `Execute` path today reads `call.Metadata` only inside the `if e.runner !=
nil` branch (`bash.go:176`); v1 must read the policy on **both** branches (the
`call.Metadata` map is present on both). Honest scope: the local executor runs on
the host (only the *worktree* is proven, not the whole host FS; sensitive env is
already stripped by `filterEnv`); the remote runner runs in the sandbox
container. True host/OS containment is the substrate.

### 5. Spawn is not inheritance

`filesystem_policy` is **per-task** and does **not** propagate to spawned
sub-loops (mirroring `decide`'s per-spawn allowlist threading, `decide.go:212`).
So: (a) a product that spawns inspect sub-loops MUST re-assert `read_only` on each
child `TaskMessage`; (b) a `read_only` role SHOULD NOT carry child-minting
orchestration tools (`spawn_agent`, `CategoryOrchestration`) that could mint an
unpoliced child — otherwise the model routes a write through an unguarded
grandchild. This is a product-policy constraint the ADR names explicitly; the
framework cannot infer intended child scope.

## Relationship to existing primitives

- **`ReadOnlyCategories` / `CategoryInspect`** — untouched. That axis is *which
  tools*; this is *how `bash` writes*. They compose (a role can have `bash`
  enabled **and** `read_only`). We do not wire/retire the unwired
  `ReadOnlyCategories` here.
- **`verify_clean` / `read_only_paths`** — subsumed as the pre-check half; the
  per-call boolean stays for ad-hoc callers (back-compat).
- **Sandbox substrate (ADR-052)** — this policy is the enforcement seam it backs.
  When the substrate ships, `read_only` resolves to an OS-level read-only mount /
  overlay (which closes the git-laundering and out-of-worktree holes); the git
  proof remains as defense-in-depth and the local / no-substrate fallback. Same
  vocabulary; no fork.

## Non-goals (honest scope of the git proof)

From the issue's Non-Ask, plus the holes the review made explicit:

- No SemSpec-specific scheduler, lifecycle, or readiness state machine; no
  framework ownership of product labels; no gated-DAG change.
- **No OS-level write *prevention*.** v1 *detects* net mutation and returns a
  typed violation; it does not stop the write mid-command. Prevention
  (read-only mounts / overlays / seccomp) is the substrate's job.
- **git-state laundering is only partially covered.** HEAD pinning catches
  commit / reset-to-different-ref / checkout-to-different-ref. It does NOT catch:
  `git clean -fdx` deleting untracked files (incl. scratch), a reset/checkout
  that lands back on the *same* HEAD with a clean tree, a mid-command write the
  command itself deletes before returning, or a **backgrounded/detached** write
  (`(sleep 1; echo x > f) &`) that lands after the post-capture. These leave HEAD
  and the dirty set unchanged at capture time; only OS-level observation (the
  substrate) sees them. An LLM inspect role is unlikely to launder deliberately,
  but the guarantee is stated honestly, not overstated.
- **No host-filesystem proof.** A command writing *outside* the worktree on the
  local host is not caught (the remote runner's container bounds this; the
  substrate bounds it generally).
- **No remote-mutation proof.** A `read_only` role that also holds `github_write`
  or runs `bash git push` can mutate remote state; that is a tool-availability
  decision (don't give inspect roles remote-write tools), not this policy's scope.
- **No auto-revert.** v1 reports; it does not undo (surprising side effects; the
  loop/product decides).

## Consequences

### Positive

- Unblocks SemSpec's planner/reviewer isolation now, without the gated substrate.
- One filesystem-policy vocabulary across framework and future substrate.
- Enforcement is structural, model-proof, and **path-universal** (stamped at the
  shared dispatch seam), and it hardens the `decide` allowlist as a bonus.
- Extends a tested primitive rather than forking a third read-only model.

### Negative / cost

- The git proof is a *net worktree+HEAD* proof, not write-prevention, with the
  laundering caveats above. Documented; the substrate closes the gap.
- Two extra `git status` + two `git rev-parse` per read_only `bash` call. Cheap
  vs. command runtime; only on read_only tasks.

## Risks

- **Approval-gated bash (the BLOCKING case).** Closed by stamping at
  `dispatchToolCall`; a regression test must dispatch an *approved* read_only
  bash call and assert the policy still enforces.
- **Fill vs. overwrite.** The policy key is stamped as an overwrite; stale
  comments in `processor/agentic-model/client_wire.go:495,526-529` still assert a
  carrier lifts into `ToolCall.Metadata` — false today (ADR-051 moved it to
  `ReasoningRecords`), but a fill-only merge + that regression would be a
  model-controlled bypass. Update those stale comments in the same PR.
- **Spawn escape.** Named in §5; the product must re-assert on children and keep
  child-minting tools off read_only roles.
- **Scratch over-exemption.** Product-declared; logged in the violation scope
  string so it is auditable.

## Open questions

1. **Typed `TaskMessage.ExecPolicy` field vs. metadata key.** v1 uses the metadata
   key (consistent with `loop_id` / `decide` allowlist, zero new propagation
   wiring). A typed field is a possible future hardening if the policy grows
   structure.
2. **Beyond `bash`.** `bash` is the only *local-worktree* write vector today
   (verified: no `os.WriteFile`/`Create`/`Remove` in `executors/`; other write
   tools hit GitHub/graph/KV, not the worktree). A future worktree-write tool
   reads the same policy key.

## Related decisions

- ADR-052 (sandbox substrate, pre-ADR) — the OS-level backing this seam defers to.
- ADR-051 (reasoning records off Metadata) — why the model cannot reach
  `ToolCall.Metadata` today; the stale comments to fix.
- ADR-036 (agent-private observable state) — `write_todos` opacity is per-loop;
  this is per-task filesystem scope. Different axis.
- `MetadataKeyDecideActionAllowlist` — the model-uncontrollable-metadata pattern
  this mirrors, and whose approval-redispatch gap this fixes.

## References

- gh#443 (this ask); SemSpec `docs/upstream/semstreams-asks.md`.
- `processor/agentic-tools/executors/bash.go` (`verify_clean`,
  `formatVerifyCleanViolation`, `pathMatchesAny`);
  `processor/agentic-tools/categories.go` (`ReadOnlyCategories`);
  `processor/agentic-loop/handlers.go:1323-1367` (`dispatchToolCall` seam),
  `:1096-1110` (main-path merge); `processor/agentic-loop/approval_response_handler.go:114-124`
  (bare re-dispatch); `processor/agentic-tools/decide.go:295,522`.
- `docs/proposals/sandbox-substrate.md` + `-design-exercise.md`
  (`filesystem.{read_only, workspace_write, host_write}` vocabulary, two-layer
  admission model).

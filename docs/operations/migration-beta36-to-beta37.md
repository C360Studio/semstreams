# Migration Guide: beta.36 → beta.37

## Summary

Beta.37 adds two coordinated features to the bash agentic tool plus a
small, safe API extension to the sandbox client. The combination lets
agent-driven shell flows declare paths that must not be modified, and
gate command execution on a clean precondition check before the real
command runs.

| Surface | Status |
|---|---|
| Bash tool: new `read_only_paths` argument (`[]string`) | **Additive** |
| Bash tool: new `verify_clean` argument (`bool`) | **Additive** |
| `sandbox.Client.CreateWorktree` signature change — now takes `CreateWorktreeOptions` | **BREAKING** for external consumers (one-line migration) |
| `sandbox.CreateWorktreeOptions{ReadOnlyPaths []string}` | **New type** — wire-level metadata; server-side enforcement is the sandbox server's responsibility |
| `git status` precondition uses `-z --porcelain --untracked-files=all` | **Internal** — handles untracked-dir collapse and quoted paths |

**The simplest beta.36 → beta.37 upgrade is to do nothing**, unless your
code calls `sandbox.Client.CreateWorktree` directly — then add an empty
`CreateWorktreeOptions{}` argument. Existing bash tool calls behave
identically when neither new argument is present.

## What's new

### Bash tool: `read_only_paths` + `verify_clean`

Two optional bash-tool arguments, designed to be used together:

```jsonc
{
  "command": "go test ./...",
  "read_only_paths": ["src/", "configs/foo.json"],
  "verify_clean": true
}
```

- `read_only_paths` declares paths (files or directories, relative to
  the worktree root) that the agent considers off-limits to modify.
  Trailing slashes are tolerated; an entry like `"src/"` matches
  anything under `src/` and the directory itself.
- `verify_clean: true` triggers a precondition `git status -z
  --porcelain --untracked-files=all` before the real command. If any
  dirty path matches `read_only_paths`, the bash tool refuses to run
  the command and returns an error naming the dirty paths.

When `verify_clean: true` is set with `read_only_paths` empty (or
absent), the precondition degenerates into a general "tree must be
totally clean" gate — useful for "I want to start from a clean slate"
agent flows.

The precondition uses `-z` so paths with spaces, tabs, double-quotes,
backslashes, or non-ASCII bytes are parsed correctly regardless of git's
`core.quotePath` setting — without `-z`, git would emit
`?? "src/has space.go"` (literal quotes), and a naive parser would
silently fail to match it against `read_only_paths`.

### `sandbox.Client.CreateWorktree` accepts options

```go
// Before:
info, err := client.CreateWorktree(ctx, taskID)

// After:
info, err := client.CreateWorktree(ctx, taskID, sandbox.CreateWorktreeOptions{
    ReadOnlyPaths: []string{"src/", "configs/foo.json"},
})
```

`CreateWorktreeOptions` is the extension point for create-time worktree
metadata. The current field is `ReadOnlyPaths`; the type is reserved
for future expansion (sister projects' env-var injection, cgroup limits,
etc.).

The Go client transmits the field over the wire as JSON; **server-side
enforcement is the sandbox server's responsibility.** This client
change unblocks the wire pipeline so semspec/semteams/semdragon's
sandbox server team can add enforcement on their schedule. Until then,
`read_only_paths` set at create-time is metadata only — the bash tool's
`verify_clean` check is the operative defence.

## Migration steps

### External consumers of `sandbox.Client.CreateWorktree`

Any code that calls `CreateWorktree` directly needs a one-line update:

```sh
# Before:
client.CreateWorktree(ctx, taskID)

# After (no read_only_paths needed):
client.CreateWorktree(ctx, taskID, sandbox.CreateWorktreeOptions{})

# After (with read_only_paths):
client.CreateWorktree(ctx, taskID, sandbox.CreateWorktreeOptions{
    ReadOnlyPaths: []string{"src/", "configs/foo.json"},
})
```

In-repo grep confirmed zero non-test callers in semstreams. Sister
projects (semspec, semteams, semdragon) consume this package directly
and will see a compile break — coordinate the bump or pin to beta.36
until ready.

### Rule prompts that drive bash tool calls

If you want agents to honour read-only paths, hand the LLM the path
list in the prompt and let it forward both arguments to the bash tool:

```jsonc
{
  "prompt": "You may modify any files except those listed in read_only_paths. Always pass verify_clean: true to the bash tool to confirm the working tree is clean before destructive operations.",
  "tool_call_constraints": {
    "bash": {
      "always_include": {
        "read_only_paths": ["$entity.triple.protected_paths"],
        "verify_clean": true
      }
    }
  }
}
```

(The `tool_call_constraints` key is a sketch — actual enforcement of
"always include these arguments" is upstream's responsibility. The
bash-tool surface area in this tag is just the new arguments; how
products wire them into prompts is product-specific.)

## Backward compatibility

- Bash tool calls without `read_only_paths` / `verify_clean`: unchanged
  behaviour. No precondition runs.
- `sandbox.Client.CreateWorktree`: signature change. One-line migration
  for external callers. Wire format unchanged when
  `CreateWorktreeOptions{}` is passed empty (`read_only_paths` is
  `omitempty`).

## Cross-references

- `processor/agentic-tools/executors/bash.go:73` — bash tool schema
- `processor/agentic-tools/executors/bash.go:171,189` —
  `verifyCleanSandbox` / `verifyCleanLocal`
- `processor/agentic-tools/executors/bash.go:226` —
  `formatVerifyCleanViolation` (handles `-z` records, R/C two-record
  rename rows, paths with spaces / non-ASCII)
- `processor/agentic-tools/sandbox/client.go:114` —
  `CreateWorktreeOptions` + new `CreateWorktree` signature
- `processor/agentic-tools/executors/bash_test.go` — local-mode and
  fake-sandbox-mode coverage; `TestFormatVerifyCleanViolation` includes
  rename two-record and quoted-path cases per go-reviewer feedback
- `processor/agentic-tools/sandbox/client_test.go` —
  `TestCreateWorktree_WireFormat_*` pins the JSON envelope
- semspec ask: read_only_paths + verify_clean (the empirical case study)

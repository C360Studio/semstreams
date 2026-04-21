# Persona Fragments Directory

Prompt fragments for agent roles, loaded at startup by `persona.LoadFromDirectory`.

## Directory convention

```
configs/personas/fragments/
  <role>/
    <fragment-id>.md
    <fragment-id>.md
  <role>/
    ...
```

- Each subdirectory name becomes the **role** the fragment is scoped to.
- Each `.md` filename (without the `.md` extension) becomes the **fragment ID**.
- The full file body is used as the fragment **content** — there is no YAML
  frontmatter. Priority and category are not configurable here; set them via
  the `create_persona` / `update_persona` CRUD tools if needed.

Example: `configs/personas/fragments/ops/00-identity.md` loads as:

```json
{"id": "00-identity", "roles": ["ops"], "content": "<file body>"}
```

## Precedence and source-of-truth model

Fragment sources apply in this order at **startup** (later wins):

1. `DefaultFragments()` — code-defined baseline baked into the binary.
2. **This directory** — overrides checked into source control. Any KV entry
   whose fragment ID matches a file in this directory is **overwritten** by
   the file on every restart.

At **runtime**, tool writes (e.g. `update_persona`) persist in the `PERSONAS`
KV bucket — but only until the next restart, at which point the file on disk
wins again.

**Operator contract: files in git are the source of truth.**
Runtime edits via `update_persona` or similar CRUD tools are **ephemeral** —
they are reset to file state on every process restart. Do not rely on runtime
tool edits for long-lived configuration. If you want a change to survive
restarts, edit the `.md` file and redeploy.

This means the effective runtime precedence is:

```
DefaultFragments (code) < files on disk (startup) < tool writes (current process lifetime)
```

And on every restart the sequence resets:

```
DefaultFragments → files override any stale KV entries → fresh tool writes override files
```

## Startup-only (Phase 1)

There is no file watcher. Edits to files in this directory take effect on the
next process restart. If hot-reload is needed in a future phase, a file watcher
can be added without changing the precedence contract.

## Skipped entries

The loader silently skips:

- Hidden files (names starting with `.`).
- Non-markdown files (`.txt`, `.yaml`, files without extension, etc.).
- Nested subdirectories — only `<role>/<fragment>.md` depth is scanned.
- Symlinks that resolve outside this directory tree.

Missing or empty directories are not errors — the loader logs a warning and
continues boot normally.

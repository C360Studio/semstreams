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

## Precedence

Fragment sources are applied in this order (later wins):

1. `DefaultFragments()` — code-defined baseline baked into the binary.
2. **This directory** — startup file overrides checked into source control.
3. `PERSONAS` KV bucket — runtime overrides written by CRUD tools or agents.

`persona.LoadFromDirectory` is called at startup, after building the manager,
before the KV watch starts. KV entries written at runtime (or loaded via the
`PERSONAS` bucket on restart) win over these files.

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

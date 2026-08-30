# gh#1168 — federation identity: design premises, pinned

Companion to `docs/proposals/gh1168-federation-identity-design.md` (O-11). It exists so a rebase cannot silently
invalidate a load-bearing premise: `bash scripts/inventory-verify.sh docs/proposals/gh1168-federation-identity-pins.md`
re-checks every pin below and names the ones that moved or drifted, so a refresh reads only those files.

It holds the **Case B** premises the design actually rests on. The accepted inventory
(`docs/proposals/gh1168-federation-identity-inventory.md`, `5967394f`, sha256 `7ec8c088…`) is NOT converted: its
headings and 90 prose bullets are outside this grammar, and rewriting them would re-hash the artifact whose sha256 is
the recorded INVENTORY PASS checkpoint.

base: 4c46fbc55caa42efb9ce55090d8e12102256055c

## Design premises

- `config/manager.go:567` — `		case "platform":`
- `config/manager.go:840` — `	return len(keys) > 0, nil`
- `config/config.go:795` — `func validateAuthorityPair(org, id string) error {`

## Adjacent claims

- #1192 — the framed-digest run identity (Case A), rehomed by the owner scope cut of 2026-08-30
- #1194 — the import-lane collision gate (`entity.import.lane`, `import_collision`)
- #1193 — `run_scope=new` appends rather than replaces the run anchor
- #1186 — the environment-override surface, widened to namespace all env vars `SEMSTREAMS_`
- #1187 — the three zero-caller deletions (`FederationMeta` family, `DeploymentPrefix`, `MinimalConfig`)
- #1188 — namespace the config KV bucket by the authority pair and retire the gh#459 guard; it namespaces on the
  PRE-mint `(org, stem)`, so this change must be correct without that guard

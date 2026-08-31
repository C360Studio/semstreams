# gh#1168 — federation identity: design premises, pinned

Companion to `docs/proposals/gh1168-federation-identity-design.md` (O-11). It exists so a rebase cannot silently
invalidate a load-bearing premise: `bash scripts/inventory-verify.sh docs/proposals/gh1168-federation-identity-pins.md`
re-checks every pin below and names the ones that moved or drifted, so a refresh reads only those files.

It holds the **Case B** premises the design rests on. The accepted inventory
(`docs/proposals/gh1168-federation-identity-inventory.md`, `5967394f`, sha256 `7ec8c088…`) is NOT converted: its
headings and 90 prose bullets are outside this grammar, and rewriting them would re-hash the artifact whose sha256 is
the recorded INVENTORY PASS checkpoint.

**Re-cut after implementation.** The three pins this file carried at the design checkpoint —
`config/manager.go`'s `case "platform":` arm, `hasKVConfig`'s `return len(keys) > 0, nil`, and
`validateAuthorityPair`'s declaration — were the *pre-change* measurements of collisions 1, 2 and 3. The change
deletes the first two and rewrites the third, so pinning them now would pin history. What they measured is recorded
in `openspec/changes/federation-identity/conformance.md` and design §3. The pins below are the premises that are
still live: the facts this change DEPENDS on and did not itself author.

base: 656bf5c8c2e4adf52284f1f8ae6ba9ec99eb9bec

## Design premises

- `pkg/types/framework_identity_families.go:65` — `	return MaxEntityIDBytes - LongestFrameworkIdentityFamily().FixedBytes()`
- `config/config.go:818` — `	return semtypes.MaxAuthorityPairBytes() - mintedSuffixBytes`
- `config/manager.go:211` — `	hasConfig, err := cm.establishPlatformIdentity(ctx)`
- `config/manager.go:1126` — `		current.Services = make(types.ServiceConfigs)`
- `natsclient/kv.go:209` — `			return 0, ErrKVKeyExists`

## Adjacent claims

- #1192 — the framed-digest run identity (Case A), rehomed by the owner scope cut of 2026-08-30; it carries the
  170→168 budget tightening, so `MaxAuthorityPairBytes()` is still 170 here
- #1194 — the import-lane collision gate (`entity.import.lane`, `import_collision`)
- #1193 — `run_scope=new` appends rather than replaces the run anchor
- #1186 — the environment-override surface, widened to namespace all env vars `SEMSTREAMS_`
- #1187 — the three zero-caller deletions (`FederationMeta` family, `DeploymentPrefix`, `MinimalConfig`)
- #1188 — namespace the config KV bucket by the authority pair and retire the gh#459 guard; it namespaces on the
  PRE-mint `(org, stem)`, so this change must be correct without that guard — the adopt branch compares `org`
  itself rather than leaning on the guard's tuple
- semsource — `entityid.MaxOrgLen` assumes a 9-byte platform; the minted suffix adds 7 (migration obligation 5)

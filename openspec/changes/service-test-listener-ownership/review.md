# Service listener review checkpoints

## Inventory

Baseline: `50ff16eb8bc89bac520713531c8c70df1676cf2d`.
Artifact: `inventory.md`, SHA-256 `f52f793d9fe3531530a7bf5b04bebec9eff64ec5def2edd80bf1d5349b14b97f`.
Independent role: `semstreams-reviewer` (`/root/nats_flake_review`, GPT-6 Astra).
Verdict: **INVENTORY PASS**, 2026-09-25.

The reviewer independently enumerated before reading the completed artifact. Typed `gopls` references confirmed
18 `freePort` calls, three `freeMetricsPort` calls, and six `freeServerPort` calls, resolving the artifact's stated
reference-census uncertainty. The reviewer found no missing owner or blocking completeness gap and checked the
composition, shutdown, context-ownership, and pprof constraints. This verdict permits design; it does not approve it.

Mechanical check at this baseline:

```text
task inventory:verify -- openspec/changes/service-test-listener-ownership/inventory.md
pins=79 ok=79 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0
```

The inventory is retained verbatim as the reviewed historical checkpoint; its pending-review wording is superseded
by this record. Any later implementation drift must be assessed separately, not hidden by rewriting this identity.

## Pre-owner design

Independent role: `semstreams-reviewer` (`/root/nats_flake_review`, GPT-6 Astra).
Final verdict: **DESIGN REVIEW PASS**, 2026-09-25. This is not owner acceptance.

| Artifact | SHA-256 |
|---|---|
| `design.md` | `3be761c8bb5dc8bd93fb886726f0b7c87d79e6f18ef66daa53f418771e1e61f9` |
| `proposal.md` | `a04796a37e72854bd4d7ab74d67436fab496bcf1b12ab6136ec8aa579e76527b` |
| `inventory.md` | `f52f793d9fe3531530a7bf5b04bebec9eff64ec5def2edd80bf1d5349b14b97f` |

The first design review requested two corrections: existing Address must report the owned endpoint after listener
transfer, and positive native Start acquisition/serve/Stop coverage must remain after the test migration. Both were
resolved and independently re-reviewed. The final package also clarifies that a rejected second Start preserves
an already-owned listener and its address; configured fallback applies only when no listener remains owned.

The reviewer found no remaining design issue. The frozen inventory is still the review baseline. The proposed
public changes are one additive method, metric.Server.StartWithListener, and accurate Address reporting while
ownership exists. No runtime code or capability spec delta is part of this checkpoint.

## Documentation checkpoint verification

Source baseline: `50ff16eb8bc89bac520713531c8c70df1676cf2d`; Go source is unchanged from `50742980`.
Local `task build:default` and `task lint` completed successfully. The inventory checker verified all 79 pins.
`git diff --check` passed. Strict OpenSpec validation reports the expected design-phase hold:

```text
Change must have at least one delta. No deltas found.
```

The shared protocol permits this delta-free claim checkpoint. Adding a spec delta solely to turn validation green
would bypass owner acceptance; the delta follows acceptance of the reviewed design.

On the initial proposal-only head, hosted [CI run 36148599581](https://github.com/C360Studio/semstreams/actions/runs/36148599581)
passed Test, Build, Schema Validation, and Tier 1 API Compatibility. Lint failed only in strict OpenSpec validation;
the aggregate status consequently failed. Both jobs in the
[E2E ladder](https://github.com/C360Studio/semstreams/actions/runs/36148599224) passed. These are baseline checks,
not evidence for an implementation that has not been written.

## Hold

Await explicit owner acceptance of the final design identity above before handing it to a developer, changing
runtime code, or writing a capability spec delta. Keep the PR draft. No implementation or merge is claimed.

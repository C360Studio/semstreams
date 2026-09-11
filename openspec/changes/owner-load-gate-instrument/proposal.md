# Change: The owner-filter CI guard becomes a regression guard, and condition 4's evidence moves to a supervised run

Closes #1284. Claim: draft PR #1285 on `claude/gh1284-owner-load-gate`, own worktree. Premises pinned at
`main@29187077` in `inventory.md` (92 pins, `task inventory:verify` exit 0). Milestone: `v1.0.0-beta.165`.

**Design status: RULED.** The owner ruled on 2026-09-11 (#1284 comment 5635299542). This proposal describes a decided
target state, not a set of options. Six questions remain on the docket (`design.md` § 9) and every one of them is a
HOLD in `tasks.md` § 2. **Revision 3** replaces the options-and-recommendation framing of revisions 1-2 and voids the
evidence revision 2 called its strongest — `design.md` § 11.1.

## Why

`TestIntegration_OwnerFilterLoadHarness` failed the required `Test` job five times on a per-repetition wall-clock
budget that a runner stall can forge: 3.356s, 3.515s, 4.760s and 4.805s against a 3-second budget, plus one event
that reached the framework's own 5-second deadline and failed as `context deadline exceeded`. Four of 42 CI failures
since 2026-08-26, one on `main`. #750 closed as COMPLETED having shipped only a comment describing the flake, so a
known-unfixed flake in a required job had no open issue behind it since 2026-07-30.

The measured facts that decided the shape:

1. **The gate predicted a ceiling the framework does not enforce, inside a band where production succeeds.**
   `KeysByFilter` applies `DefaultKVOptions().Timeout` = 5s (`natsclient/kv.go:39`, guarded at `:69`, applied at
   `:538`) and the harness passes no override (`:168`). Production graph-index builds its stores the same way
   (`processor/graph-index/component.go:927`, `:953`). All four budget firings land in the 3s-5s band where the
   framework and production both succeed and only the test fails.
2. **A predicted budget has no correct value here.** Below the enforced deadline it fires on stalls the framework
   tolerates — the measured defect. Above it, it can never fire: the full profile's `operationBudget: 10 *
   time.Second` at `:85` is unreachable, because an expiry fails at `:487` before `:489` is reached. Both defects
   are in the tree today.
3. **No budget choice reduces the residual below one event.** Healthy forward max is 389ms; the five events imply
   stalls of ~3.2s, ~3.3s, ~4.6s, ~4.6s and >=4.7s. Absorbing the largest needs ~5s of budget — exactly where
   `KeysByFilter` fails as a typed error instead, and one event already did.
4. **The instrument destroyed its own evidence, and a green run recorded none.** `require.Less` at `:489` calls
   `t.FailNow()` before `assertOwnerLoadLatency` at `:492`; `assertOwnerLoadLatency` sorts at `:497`; and `t.Logf`
   reaches CI only for failing tests (`scripts/run-integration-tests.sh:311` passes no `-v`). ADR-077's Status
   requires evidence "recorded against the exact implementation revision" (`:8`). On every passing run, the gate that
   authorizes production recorded nothing.
5. **The evidence home was already historical.** The owner-filter acceptance record at `docs/operations/32-...:80`+
   was measured at revision `0a7af288` on `nats:2.12.4-alpine`, and `:43` declares those rows historical. Condition 4
   had no current-pin evidence before this change either.
6. **`gh#220`'s >=3x headroom rule was satisfied and was never the problem.** 3s / 389ms = 7.7x. A stall *adds*
   seconds rather than multiplying them, so the load-bearing quantity is absolute headroom (2.61s), not the ratio. A
   ratio rule buys a slow assertion many absolute seconds and a fast one almost none.

## What changes

Test tier, documentation and contract text only. No production code changes.

1. **The CI per-operation budget is deleted**, and with it both per-repetition wall-clock assertions — `:489` and
   `:376`. The ceiling becomes the typed error `require.NoError` already raises at `:487` and `:374` when
   `KeysByFilter` reaches its deadline. The framework observes the real bound; the test stops predicting one.
2. **`p95Budget` and `p99Budget` are kept and re-derived** from a fresh supervised measurement, moving from 3s to
   **1s**. Basis, in the unit the measurements are in: 12.8x the quiet-box worst healthy p95 (77.861 ms), 5.7x the
   worst shared-runner healthy p95 (175.4 ms), 2.6x the worst shared-runner healthy sample (389.0 ms). At
   `repetitions: 5` the percentiles select `durations[3]`, so **two** repetitions must breach — the deleted gate
   tripped on one, which is why this is not that gate returning.
3. **Condition 4's activation evidence moves to a supervised run**, recorded with its revision, host, runtime, pin,
   timestamp and complete per-filter distribution. That run has been executed at revision `60c79736` on the current
   pin: 21k full PASS 43.17 s, 5k CI PASS 2.12 s, both exit 0. Publishing it in-tree is `tasks.md` § 3.3-3.4.
4. **The CI profile is named for what it is** — a regression guard asserting exact match sets, post-churn
   convergence, the typed-error ceiling, percentile budgets, a bounded queue, consumer baselines, released
   subscriptions, zero slow consumers and the RSS bound.
5. **The distribution is recorded in submission order on every run**, pass or fail.
6. **Citation defect A repaired**: `openspec/specs/graph-index/spec.md:184` attributes the 3s CI guard to ADR-065,
   which contracts no such budget; the source is ADR-077 §8 condition 4.

## What does not change

- No production code, no `natsclient` surface, no exported symbol, no communication path, no payload. Not BREAKING;
  no package in `release/tier1-packages.txt` changes.
- ADR-077 itself. The amendment is drafted in `design.md` § 10 and stays unapplied pending Q3. Condition 5 is not
  touched.
- ADR-065, which says nothing wrong (Q6).
- `docs/operations/32-...:70`-`:71`. That table describes the **smoke** harness and its staleness belongs with
  **#1286** (Q8).
- `ownerLoadFullProfile`'s dead `operationBudget: 10 * time.Second` at `:85` — the same deletion logic applies, the
  resolution is proposed in `design.md` § 9, and it is **not applied** (Q7).
- The two contract tests are rewritten, not removed: the anti-relaxation property has to survive the move
  (`tasks.md` § 4.4).

## Non-goals

- Re-deriving the full profile's own `p95Budget`/`p99Budget`, now at 9.6x/15.6x over measured. Folded into Q7.
- Raising `repetitions` (Q9), adding a p50 floor or per-class budgets (Q10).
- Splitting the harness into its own CI job (Q4) — the ruling removed its original justification.
- Repairing **#1286**, the sibling harness whose 10s budget is unreachable dead code and whose widening read another
  harness's milliseconds as seconds. Referenced, not fixed here.
- Any sister-repository change. No sister cites condition 4 or the 3-second figure.

## Adopter seam inventory

The outward surface is ADR-077 §8 condition 4 — the text a downstream reads to judge whether owner discovery is
activation-ready — and, one level down, `natsclient.KVStore.KeysByFilter`'s 5s bound. The specific person: a
sister-repo developer deciding whether to provision fresh storage against an activated graph-index.

1. **What must they know?** That condition 4 is satisfied by a supervised record on the current pin, not by a green
   CI run; and that their `KeysByFilter` calls are bounded at 5s. Two facts, both about guarantees.
2. **What happens if they do nothing?** Before this change they read a green CI run as recorded evidence when it
   recorded nothing, and read condition 4 as a per-observation guarantee the instrument could not deliver on a shared
   runner. After it, the ADR points at a record that states its own provenance and expires with its pin. For
   `KeysByFilter` nothing bad happens: the framework enforces its bound, returns `context deadline exceeded`, and
   `collectFilteredKeys` refuses to return a partial key set on expiry (`natsclient/kv.go:589`, `:592`).
3. **Where do they find out?** ADR-077 §8 plus the runbook record for the guarantee; a typed runtime error for the
   5s bound. The correctness fact is already at the good tier; the evidence-policy fact belongs at doc tier.
4. **What SHOULD they have to know?** Nothing about the instrument. The ADR states the guarantee, the runbook holds
   the numbers with their unit and provenance, and the spec holds the rules.

**Prefer observation to prediction — this change is that rule applied to a test.** The harness was an adopter of
`KVStore` predicting a 3-second ceiling the framework does not enforce. The framework already observes the real
ceiling and reports the breach as a typed error; the harness already asserts on it. So the predicted number was never
load-bearing, and the ruling deletes the knob rather than retuning it.

## Consumers at birth

No new exported symbol, port, subject, bucket, or config field. The changed surfaces have present consumers:
`openspec/specs/graph-index/spec.md:174`, `docs/adr/077-...:139`, `docs/operations/32-...:80`+, and the two test
files pinned in `inventory.md` § 7.

# Established recipe and exceptions

Owner direction: existing recipe, compact exceptions, implementation, focused checks and one implementation review.
No separate inventory/options/design-review cycle is needed for this workflow-pattern adoption.

Recipe: `.github/workflows/release.yml` already uses job prerequisites; `.github/workflows/ci.yml` owns the final
`needs`/`if: always()` result check. Task commands already stop sequential execution on failure. Move the existing
pinned strict OpenSpec validation ahead of Test and reuse `taskfiles/openspec.yml` for the local command.

Exceptions to preserve: the integration runner still owns cleanup admission, lock and Docker; CI Test still uses
that exact runner; required status rejects skipped/failed/cancelled validation or tests. Independent CI jobs remain
parallel. E2E Ladder is separate claimed work and is outside this slice.

Oracle: invalid OpenSpec must prevent the next expensive command; valid input must admit it. The aggregate may pass
only when every required job succeeded. Use focused actual Task execution, workflow wiring and aggregate-script
controls. Finite job-result classes are sufficient examples; no generated property or broad mutation matrix is
needed. Select only a plausible admission/result fault whose detection needs proof, with checked restoration.

Cost: retain local refusal/healthy timings and hosted job timings separately. A subsecond local validator does not
predict hosted Node/npm setup cost. Compare healthy runtime with the existing measurements; do not claim all
historical wasted time is recoverable.

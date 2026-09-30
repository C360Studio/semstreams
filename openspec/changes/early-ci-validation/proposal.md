# Early CI validation

## Why

#1438 addresses four candidates that consumed 75m51s of Test-job time, including 65m50s after strict OpenSpec
validation had already failed. The existing validator runs late in CI Lint and is absent from local check:push.

## What Changes

Run the existing strict OpenSpec validation before CI Test admission and before local check:push expensive work.
Use existing workflow prerequisites and sequential Task failure propagation. Preserve the existing additive
race/integration command, independent-job parallelism and a failing aggregate when required evidence is absent.

## Scope

One CI/Task ordering change with focused refusal, healthy-admission and final-result checks. #1293 retains suite
cost/duplication, fuzz and coverage work. #1222/#1117 own their E2E workflows; those workflows are not edited here.
No production shutdown semantics, timeout, test selection, retry policy or testing framework changes are included.

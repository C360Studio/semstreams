# Assertion-exit child plumbing review

Reviewer: `semstreams-reviewer` (`test_reliability_review`), 2026-09-29.
Scope: implementing the accepted selected self-execution proof in graph-ingest's existing test package.
This is a test-only implementation detail; no production or workflow change is approved.

## Measured constraint and choice

The integration-tagged package TestMain creates shared NATS before m.Run. Re-executing that binary normally would
create an additional child container. Moving the proof to a !integration-only file would omit it from hosted CI's
additive integration selection. Keep the proof untagged and dispatch only its exact child mode before NATS setup.

## Reviewer recommendation

The narrow TestMain branch is an allowed test-only implementation detail. Keep the proof untagged so both default
and integration selections execute it.

Before NATS acquisition, bypass package setup only when:

- A dedicated child-mode variable names an explicitly supported fixture scenario.
- The selected test is the exact anchored child fixture, with one execution; reject conflicting selection/list/
  benchmark flags.
- The child calls m.Run and propagates its actual exit status. Invalid child requests fail explicitly.

The parent must launch the current binary with explicit arguments, require the expected failing exit and
scenario-specific assertion/cleanup/order witnesses, and reject timeout, panic, race or missing-witness outcomes.
A nonzero exit alone is insufficient. Retain bounded ownership and one terminal Wait.

Without child mode, TestMain's existing NATS setup/run/teardown path must remain unchanged. This avoids an additional
child container while retaining hosted coverage; it does not introduce a general integration bypass.

The coordinator accepts this bounded detail under the existing batch authorization. Implementation and execution
remain subject to final source/proof review; no completed validation is asserted by this recommendation.

# Migrate to the Framework-Owned Lesson Contract Snapshot

This is the SemStreams-local release note and downstream migration guide for #1029.

SemStreams provides `agentictools.LessonProjectionContract()` for product composition roots. It returns an
independent snapshot of the canonical lesson projection contract. Products keep one local mutation client and inject
its narrow reconcile/read capabilities through `NewLessonCurator`.

The previously retired `NewNATSLessonCurator` helper remains absent.

## Product migration

External products that currently mirror `agentic.lesson-record` should:

1. upgrade to the SemStreams release containing `LessonProjectionContract`;
2. include that snapshot in the product's complete local mutation-client contract set;
3. remove the mirrored lesson contract literals;
4. retain genuinely product-owned contracts and tests;
5. retain first-party and product vocabulary registration before client construction;
6. keep `NewLessonCurator(mutations, mutations, logger)` or equivalent narrow wrappers; and
7. run promotion, retirement, and supersession tests against real graph-ingest.

The change is additive. Existing mirror code keeps compiling, but doing nothing retains its drift risk.

## Known downstream impact

For semdev, replace the literal lesson mirror in `internal/graphown/contracts.go` with
`agentictools.LessonProjectionContract()` in the existing shared composition-root client. Remove the lesson literal
section of `test/conformance/standards_contracts_test.go` after equivalent upstream-snapshot coverage exists.

The lesson-only wrapper in `internal/graphown/construction.go` is not automatically obsolete. The semdev owner decides
whether it remains useful as local least-privilege hardening. SemStreams does not claim the external copied-contract
risk retired until the semdev owner supplies adoption evidence against the released API.

For semteams, replace the mirrored lesson literals only if a fresh consumer census shows the contract belongs in its
local set. Retain the loop-execution contract used by `write_todos`.

SemStreams agents do not edit sister repositories.

## History and scope

SemStreams previously exported a raw-adapter `NewNATSLessonCurator` helper and deliberately removed it in commit
`9a48638d` after then-current callers migrated to narrow capability injection. The new semdev evidence supersedes the
old no-external-consumer premise, but not local client construction or narrow curator dependencies.

This migration adds no NATS factory, hidden client, bespoke agent, LLM persona, prompt role, or framework agent type.
It changes no graph wire format, durable state, identity, subject, bucket, stream, payload, or configuration schema.
Global immutable-birth enforcement remains a separate #818 decision.

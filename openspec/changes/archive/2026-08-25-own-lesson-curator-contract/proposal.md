# Change: Export the canonical lesson contract snapshot

## Why

An external product using `LessonCurator` must construct a local projection mutation client, but the canonical
`agentic.lesson-record` contract is framework-private. A locally valid copied contract can move a birth predicate
into the lifecycle reconcile group; a later transition then deletes that predicate and returns verified success.

SemStreams previously exported `NewNATSLessonCurator` and deliberately retired it after bounded zero-reference and
parity evidence. New semdev adoption evidence supersedes the no-external-consumer premise, but not the retained
composition-root-local client and narrow curator interfaces.

## What Changes

- Export one purpose-scoped, independent snapshot of the canonical lesson projection contract.
- Derive the public snapshot and internal built-in aggregate from one declaration.
- Retain composition-root-local mutation clients and `NewLessonCurator` narrow capability injection.
- Keep `NewNATSLessonCurator` retired.
- Prove snapshots are independent and lifecycle reconciliation preserves every lesson birth predicate.
- Publish downstream migration guidance without editing sister repositories.
- Introduce no bespoke agent, LLM persona, prompt role, or framework agent type.

## Non-goals

- Reintroducing a NATS lesson-curator factory.
- Exporting the complete built-in projection-contract catalog.
- Changing generic local projection-contract semantics.
- Adding graph-ingest contract identity or immutable-predicate enforcement.
- Changing lifecycle, gated-DAG, raw rule, or Graphable complete-set behavior.
- Resolving #818.

# Change: The payload registry is the single type authority

## Why

`message.Type` has three authorities and no cross-check (#1100). The payload registry decodes and collision-checks;
`internal/builtinprojection` holds two birth contracts keyed by a type string the registry never sees; graph-ingest's
`indexingProfileDefaults` is a third string-keyed table. Six framework entity types are born on the mutation lane
(`entity.create`) with a stamp none of the three knows — the five `agentic.*_entity.go` types and `lifecycle.harness.v1` —
so a lesson takes the `control` floor by default, has no wire form as itself, and cannot cross the #1095 import lane.
ADR-056 gave the stamp its meaning ("IS the registered `MessageType`"); ADR-091 superseded ADR-056 in full without saying
what the stamp is now. The owner's direction (2026-08-26): one decision — the registry is the single type authority,
contract and floor are attributes registered with the type, `EntityState.MessageType` is always a registered key, and ingest
rejects a stamp the registry does not know. ADR-103 records it; this change carries the mechanics.

## What Changes

- `payloadregistry.Registration` gains `IndexingProfile` and `Contracts`; `Register` validates them; the registry exposes
  `IndexingProfileFor` and `Contracts()`. The contract data types move to the leaf `pkg/projection/contract` with aliases in
  `pkg/projection` (sister literals compile unchanged); the leaf's private profile map is replaced by
  `vocabulary.IsValidIndexingProfile`.
- The six framework mutation-lane types become registered Graphable payloads with factories, floors, and (five) contracts;
  their triple builders move beside the type; `internal/builtinprojection` and the four `_Distinct` tests are deleted; the
  composition roots derive their contract set from the registry.
- graph-ingest reads the floor from the registry, rejects an `entity.create` whose `message_type` is not registered with the
  new closed code `message_type_unregistered`, and refuses to construct without a payload registry.
- `indexing_profile_default_total{message_type}` changes meaning: a registered type with no declared floor.
- Test and e2e fixtures register the keys they stamp; a stub-type helper joins `payloadregistry/testing.go`.
- `pkg/projection.MutationClient.Create` fills an empty `entity.MessageType` from the bound contract and rejects a
  conflicting one (owner ruling O-17): a product using `CreateMutation` may omit the stamp entirely.
- `docs/operations/migration-beta162-to-beta163.md` (new, SemStreams-owned) records every sister's impact and obligation;
  sister repositories are not edited (owner ruling O-11/O-12).
- **BREAKING** for sisters that stamp unregistered types on `entity.create`: semmachina (4), semdev (2), semconnect (11).

## Non-goals

- Birth-predicate enforcement at ingest (#818) — this change gives it a home, it does not implement it.
- Retiring `Contract.IndexingProfile` or `Contract.MessageType` (owner item O-13).
- Any reader filtering by `message_type`; any migration of stored entities (none is needed — readers never consult the registry).
- Editing sisters or filing anything in a sister repository — they are read-only; obligations live in
  `docs/operations/migration-beta162-to-beta163.md`.
- Amending ADR-054/056/076/091 text (history).

## Consumers

semmem (federation MVP: lessons as importable payloads), semteams and semdev (framework lesson/loop contracts from the
registry), semmachina and semconnect (register their birth types), semsource (no change; already registered on the fact lane).

## Sequencing

Lands first in the beta.163 wave; #1095's implementation (its tasks 5.1 and 5.3) rebases onto the contracts this change moves
into `agentic`. Merge-order overlap with #1093 at `cmd/semstreams/main.go`. The breaking commit lands only behind the complete
eight-tier e2e union plus `TestWebObservationBirthIsRegistered` (owner ruling O-6). Sister migration: `docs/operations/migration-beta162-to-beta163.md`.

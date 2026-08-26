# Conformance — single-type-authority (skeleton, revision 1)

Per-decision map from the owner's direction on #1100 (2026-08-26) and ADR-103 to the code, spec delta, and test that carry
each. Every `file:line` is to be measured at the head that holds the last change to any `.go` file or spec delta on the
branch; `tasks.md` rows cite section numbers. Fill the right-hand columns at implementation time; a row with an empty
Implementation column at review time is a deviation to record, not a gap to hide. Owner-item numbers follow the design §15.

| # | Ruling / decision | Implementation | Spec delta | Test / evidence |
|---|---|---|---|---|
| D1 | The payload registry is the single type authority (ADR-103 d1) | | `specs/payload-registry/spec.md` ADDED "A message type is a type of the deployment only if it is registered…" | `TestPayloadRegistryIsTheSingleTypeAuthority`; `payloadbuiltins/register_test.go` |
| D2 | Contract and floor are attributes registered with the type, not parallel tables (d2) | | payload-registry ADDED "A registration carries the indexing-profile floor and the projection contracts…", "The registry exposes floor and contract lookups"; `projection-mutation-client` MODIFIED "Projection contracts are local schemas" | `TestRegisterFillsAndChecksContractMessageType`, `TestContractsReturnsIndependentSortedCopies`, `TestIndexingProfileFor`; `grep builtinprojection` → 0; `indexing_profile_registry.go` absent |
| D3a | `EntityState.MessageType` is always a registered key — mutation lane rejects `message_type_unregistered` (d3) | | `specs/graph-ingest/spec.md` ADDED "A mutation-lane birth MUST carry a registered message type" | `TestCreateRejectsUnregisteredMessageType`, `TestCreateAcceptsRegisteredMessageType`, `TestFactoryRejectsNilPayloadRegistry` |
| D3b | Readers, codec, boot sweep, and the Graphable merge path never consult the registry (d3; L3) | | `specs/graph-state-contract/spec.md` ADDED "The canonical codec and the boot sweep never consult the payload registry" | `TestResidentUnregisteredStampIsNotPoison` |
| L1 | Premise for option A: the framework's writers call `internal/graphmutation` directly; only the ingest gate covers them | | graph-ingest ADDED (gate requirement) | `TestCreateRejectsUnregisteredMessageType` against a direct `graphmutation` client |
| L2 | Nil registry at the seam is fail-closed; fixture helper + 23-literal sweep in the same change (O-15) | | graph-ingest ADDED scenario "a create with no registry configured is refused" | `TestCreateSeamRejectsWhenRegistryMissing`; tasks 5.4 |
| D1 | Floors are per-binary because registrations are (O-14) | | payload-registry ADDED (first requirement, per-binary clause) | `TestIndexingProfileFor` on a registry without research |
| L5 | New import edge named in the package comment | | — | `go list -deps ./payloadregistry` recorded in tasks 3.2 |
| D3c | Floor read from the registered type; metric means "registered type with no floor" | | graph-ingest ADDED "The indexing-profile floor is read from the registered type" | `TestFloorComesFromRegistration`; rewritten `indexing_profile_registry_test.go` |
| D4 | Framework mutation-lane types are registered Graphable payloads with factories (d4) | | payload-registry ADDED "Framework entity types born on the mutation lane are registered Graphable payloads"; `agentic-lessons` MODIFIED "A lesson is an evidence-cited…"; `lifecycle` ADDED "Harness births carry the registered lifecycle type" | five `_RoundTrip` tests, `TestHarnessEntity_RoundTrip`, `TestRegisteredContractMatchesTriples`, `TestEmitLessonBuildsEntityTriples`, `TestWebObservationBirthIsRegistered` |
| D4-snap | `LessonProjectionContract()` returns the registered contract | | `agentic-lessons` MODIFIED "External lesson composition uses the framework-owned contract snapshot" | scenario "The snapshot is the registered contract" |
| D5 | Sister obligation; BREAKING; covering tiers (d5, Consequences) | N/A in-tree — PR body migration list; sister issues (tasks 7.5) | — | tasks 7.3 tier results |
| C1 | `_Distinct` tests replaced by registry collision detection | | payload-registry ADDED (first requirement, collision scenario) | `payloadbuiltins/register_test.go`; the four functions absent |
| C2 | One-table test | | — | `TestPayloadRegistryIsTheSingleTypeAuthority` |
| C3 | e2e and unit fixtures register what they stamp | | — | `TestFixturesRegisterEveryE2EStamp`; tasks 5.3 |
| O-1 | ADR-103 accepted as worded | ruling comment URL | — | — |
| O-2 | `pkg/projection/contract` leaf + aliases approved | ruling comment URL | projection-mutation-client MODIFIED | `pkg/projection/contract_test.go` GREEN unchanged |
| O-3 | Floors (ops diagnosis `content` confirmed or changed) | ruling comment URL | payload-registry ADDED (floor list) | `TestPayloadRegistryIsTheSingleTypeAuthority` |
| O-4 | Three new birth contracts minted here vs #818 | ruling comment URL | payload-registry ADDED (contract clause) | `TestRegisteredContractMatchesTriples` |
| O-5 | Empty floor metered (not rejected) | ruling comment URL | payload-registry ADDED scenario "a registered type may declare no floor" | `TestIndexingProfileFor` |
| O-6 | Milestone / wave placement | ruling comment URL | — | — |
| O-7 | Order relative to PR #1099 | ruling comment URL | — | — |
| O-8 | Skill + CLAUDE.md rewrite | tasks 6.3 | — | — |
| O-9 | ops seed key corrected; direct `PutKV` filed | tasks 6.2; issue URL | — | `task e2e:ops` |
| O-10 | web_observation e2e coverage gap filed | issue URL | — | `TestWebObservationBirthIsRegistered` |
| O-11 | semteams copied literals — communicated | issue URL | — | — |
| O-12 | semmem finding location | pointer | — | — |
| O-13 | `Contract.IndexingProfile` retained with agreement check | | payload-registry ADDED (shape validation clause) | `TestRegisterRejectsInvalidIndexingProfile` |
| O-14 | Per-binary floors intended | ruling comment URL | payload-registry ADDED | — |
| O-15 | Fail-closed seam on nil registry | ruling comment URL | graph-ingest ADDED | `TestCreateSeamRejectsWhenRegistryMissing` |
| DEVIATION | (record any owner-signed deviation here with its comment URL) | | | |

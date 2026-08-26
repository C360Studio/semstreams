# ADR-103: The Payload Registry Is the Single Type Authority

## Status

**Accepted (2026-08-26).** Owner ruling on #1100: O-1…O-18 as recommended, with three overrides — O-6 (the release gate for
the breaking commit is the complete eight-tier e2e union plus the web-observation integration test until that tier exists),
O-11/O-12 (sister repositories stay read-only; obligations are recorded in `docs/operations/migration-beta162-to-beta163.md`),
O-17 (the contract-bound mutation client fills an empty stamp from its contract, in this change) — and explicit acceptances:
O-16 (a) hierarchy containers carry a registered type with a construction-time check, O-14 per-binary floors, O-15 fail-closed
nil registry, O-18 two distinct registration acts for payload types and entity-ID domains, and #1095's permanent
hierarchy-inference skip for foreign-authority entities. Drafted 2026-08-26 from the architect package
(`docs/proposals/gh1100-type-authority-inventory.md`, `docs/proposals/gh1100-type-authority-design.md`) at `origin/main`
`c3a17741`. It re-homes the one premise ADR-091 let lapse when it superseded ADR-056 in full: ADR-056 `:281-284`
("producer identity for the gate **IS the registered `MessageType`** — the payload-registry key already on `EntityState`")
gave the stamp its meaning; ADR-091 deleted the gate and said nothing about what the stamp is. This page says what it is.
Mechanics live in `openspec/specs/payload-registry/spec.md` (new) and the `graph-ingest`, `agentic-lessons`,
`graph-state-contract`, `lifecycle`, and `projection-mutation-client` capabilities; this page records only the decision.

## Context

Three framework tables are keyed by the `message.Type` namespace and none checks another: the payload registry (decode,
collision), the projection contracts (birth predicates; `MessageType` optional), and graph-ingest's string-keyed
indexing-profile floor. Six framework entity types are born on the mutation lane with a stamp none of the three knows
(`agentic.{agent_lesson,loop_execution,ops_diagnosis,model_endpoint,web_observation}.v1`, `lifecycle.harness.v1`); their
`_Distinct` tests hand-compare category strings because there is no registry to compare against. Each table is locally
coherent; the residue is a durable type identifier no authority can resolve to a schema, a floor, or a wire form — which is
why a lesson cannot cross a federation boundary as itself. The fact lane already rejects an unregistered type at decode
(`message/base_message.go:301-307`); the mutation lane accepts any three non-empty parts. The framework's own writers call the mutation client directly
(`internal/graphmutation/client.go:89`), so an ingest-side gate is the only check that can cover them.

## Decision

1. **The payload registry is the single type authority.** A `message.Type` is a type of this deployment if and only if it
   is registered in the binary's payload registry. There is no second catalogue of types.
2. **A projection contract and an indexing-profile floor are attributes registered with the type, not parallel tables.**
   `payloadregistry.Registration` carries `IndexingProfile` (the ADR-054 channel-(c) floor) and `Contracts` (the projection
   contracts bound to the type; each names the registration's key). graph-ingest reads the floor from the registry it already
   holds; the composition root derives its contract set from the registry. The string-keyed floor table and the
   framework-internal contract table are deleted.
3. **`EntityState.MessageType` is always a registered key.** On the fact lane by construction (decode); on every birth path
   — the typed `entity.create` RPC and graph-ingest's own in-process creates — because **graph-ingest rejects a stamp the
   registry does not know** with the closed, coded outcome `message_type_unregistered` (class invalid; the caller registers the
   type, it does not retry). graph-ingest's one in-process birth today, the hierarchy container, carries a registered
   framework type (`graph.hierarchy_container.v1`, transitional until gh606 retires containers) rather than an exception, and
   graph-ingest refuses to construct with hierarchy enabled and that type unregistered. Readers, the canonical codec,
   and the boot sweep never consult the registry: an entity persisted under a stamp that is later unregistered stays readable
   and mutable through must-exist operations.
4. **A framework entity type born on the mutation lane is a registered Graphable payload** with a factory, so it has a
   serializable form as itself and can arrive on the fact lane (including an import lane). Registering it is what makes
   the registry's duplicate rejection its collision detector.
5. **Sister obligation.** Every product that stamps a type on `entity.create` registers that type in the registry of the
   binary that hosts graph-ingest, with its floor and any birth contract it holds, before adopting the tag that carries this
   decision. The wire contract change is the new outcome code; `projection.Contract` literals keep compiling. A product that
   creates through the contract-bound mutation client may omit the stamp: the client fills it from the contract and rejects
   only a conflicting one.

## Consequences

- One place per type: key, factory, floor, contract. Adding a type is one literal; forgetting to register is a typed
  rejection at the first write, not a silent `control` and a metric nobody scrapes.
- `indexing_profile_default_total{message_type}` now names a registration whose floor is empty — an editable literal —
  rather than a key that exists in no file.
- Floors and contracts exist per binary, because registrations do: a type a binary does not register can neither be decoded
  nor born there, so the floor table it replaces was describing types some binaries never see.
- The gate is create-only; no read, merge, codec, or boot-sweep path consults the registry, so retained state needs no migration.
- Birth discipline (#818) has a home to read from without inventing another table.
- **BREAKING** for semmachina (4 types), semdev (2), semconnect (11 — exported from `gateway/cs-api` and registered by the
  host, which holds the only registry); none for semsource and semteams. Sister repositories are not edited; each impact
  and migration instruction is recorded in `docs/operations/migration-beta162-to-beta163.md`. Covering tiers before
  the breaking commit lands: `e2e:agentic` and `e2e:lessons` at minimum; the full union in the change's tasks.
- The contract data types move to a leaf package with aliases in `pkg/projection` (new `pkg/*` surface — owner design review).

## Alternatives rejected

- **Cross-check three tables at boot.** Keeps three places to edit per type and re-derives a linkage the registry can carry
  directly; the ruling names it as the rejected shape.
- **Bind contracts by name.** A linkage by naming coincidence; fails downstream, not at registration.
- **Keep "mutation-only, deliberately unregistered".** The rationale ("registering would advertise a decode path that does
  not exist") is exactly the gap: the decode path is the federation requirement, and the type without it is an anomaly
  beside `StoredMessage`, not the pattern.

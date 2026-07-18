# structural-identity Specification

## Purpose
TBD - created by archiving change enforce-structural-invariants. Update Purpose after archive.
## Requirements
### Requirement: Entity IDs MUST be exactly six non-empty dot-separated parts

An entity ID MUST consist of exactly six dot-separated parts
(`org.platform.domain.system.type.instance`), each part non-empty and composed only of the characters
`[a-zA-Z0-9_-]` (no interior dots). A string with fewer than or more than six parts, an empty part, or a
part containing any other character (including a dotted "instance") is not a valid entity ID. This is the
single contract — there is no `>= 6` variant.

#### Scenario: A canonical six-part ID is valid
- **WHEN** `acme.ops.robotics.gcs.drone.001` is validated
- **THEN** it is accepted as a valid entity ID

#### Scenario: Fewer than six parts is rejected
- **WHEN** `acme.ops.robotics.gcs.drone` (five parts) is validated
- **THEN** it is rejected as structurally invalid

#### Scenario: A dotted instance (more than six parts) is rejected
- **WHEN** `acme.ops.robotics.gcs.drone.001.left` (seven parts) is validated
- **THEN** it is rejected as structurally invalid — the instance segment MUST NOT contain dots

#### Scenario: An empty segment is rejected
- **WHEN** `acme.ops..gcs.drone.001` is validated
- **THEN** it is rejected as structurally invalid

### Requirement: Predicates MUST be exactly three non-empty dot-separated parts

A predicate MUST consist of exactly three dot-separated parts (`domain.category.property`), each part
non-empty. A string with fewer than or more than three parts, or with an empty part, is not a valid
predicate. The validator MUST check part count AND segment non-emptiness — a bare dot-count check is
insufficient. This requirement is the structural floor, not the full acceptance rule: the wired
validator (`vocabulary.IsValidPredicate`, delegating to `vocabulary.ParsePredicate`) enforces the
stronger canonical lower-kebab contract (per-segment charset `[a-z][a-z0-9]*(-[a-z0-9]+)*` and byte
bounds — owned by the predicate-contract-enforcement capability), so a 3-part token with, e.g., an
underscore still rejects.

#### Scenario: A canonical three-part predicate is valid
- **WHEN** `sensor.temperature.celsius` is validated
- **THEN** it is accepted as a valid predicate

#### Scenario: A two-part predicate is rejected
- **WHEN** `agent.role` (two parts) is validated
- **THEN** it is rejected as structurally invalid

#### Scenario: A four-part token is rejected as a predicate
- **WHEN** `openspec.change.revision.value` (four parts) is validated as a predicate
- **THEN** it is rejected as structurally invalid

#### Scenario: An empty segment is rejected
- **WHEN** `sensor..celsius` is validated
- **THEN** it is rejected as structurally invalid

### Requirement: The framework MUST expose one authoritative validator per namespace

The framework MUST provide exactly one authoritative validation function for entity IDs and one for
predicates, and every enforcement point (the ingest gate, the reference-config lint) MUST call it rather
than re-deriving the rule. The predicate validator MUST NOT be dead code — it MUST be wired into
enforcement.

#### Scenario: Enforcement and lint share the validator
- **WHEN** the ingest structural gate and the reference-config lint both validate a predicate
- **THEN** they call the same authoritative validator, so the contract cannot drift between call sites

### Requirement: A validated token MUST split deterministically for downstream keys and queries

A token that passes validation MUST split deterministically at the fixed positions downstream consumers
use. Because entity IDs and predicates are embedded in NATS KV keys, enable `domain.category.*` wildcard
queries, and are read by fixed-position splits (e.g. a predicate's `parts[1]`/`parts[2]` category/property;
an entity ID's `parts[2]` = domain), consumers MAY rely on this structural guarantee instead of guarding
each split.

#### Scenario: A valid predicate splits deterministically at fixed positions
- **WHEN** a validated predicate `inferred.semantic.high` is read positionally (`parts[1]` = category,
  `parts[2]` = property)
- **THEN** `parts[1]` is unambiguously `semantic` and `parts[2]` is `high`, with no out-of-range or
  empty-segment risk


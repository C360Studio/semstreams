## MODIFIED Requirements

### Requirement: Scope keys use a typed grammar with minimum specificity
The writer SHALL require at least one `applies_to` scope key and MUST accept only typed keys —
`id:<entity-ID prefix>` with at least three positions, or `tag:<token>` — and scope matching
MUST compare id-prefixes on entity-ID position boundaries only. Under the canonical order a three-position
`id:` key scopes a lesson to one source within one deployment (`org.platform.system`); a lesson meant for a
taxonomy across sources MUST use a `tag:` key, because that grouping is not a prefix.

#### Scenario: Untyped or over-broad scope key is rejected
- **WHEN** `emit_lesson` is called with `applies_to` of `["c360"]` or `["id:c360"]`
- **THEN** the call fails naming the typed-grammar and minimum-specificity rules (an id-prefix
  needs at least three positions)

#### Scenario: Prefix matching respects segment boundaries
- **WHEN** a lesson carries `applies_to: ["id:c360.ops.robotics"]` and a loop's scope contains
  `c360.ops-agent.robotics.gcs.drone.001`
- **THEN** the lesson does not match (the prefix `c360.ops` is not the segment `c360.ops-agent`)

#### Scenario: A three-position key scopes to a source
- **WHEN** a lesson carries `applies_to: ["id:acme.dep1.src"]` and a loop's scope contains
  `acme.dep1.src.git.commit.a1` and another loop's scope contains `acme.dep1.other.git.commit.a1`
- **THEN** the first loop matches and the second does not
- **AND** the test that verifies this is `TestAppliesToThreeSegmentsIsSourceScope`

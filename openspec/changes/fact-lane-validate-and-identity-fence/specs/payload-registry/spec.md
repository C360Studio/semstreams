# payload-registry Delta

## ADDED Requirements

### Requirement: A registered Graphable MUST NOT panic on decoded input

A registered payload that implements `EntityID()` and `Triples()` MUST return normally on any decoded content: an
identity that cannot be constructed yields `""` from `EntityID()` (graph-ingest rejects the empty ID) and the
triple whose object identity cannot be constructed is omitted from `Triples()`. A panicking entity-ID constructor
MAY keep its contract for producer-owned inputs, but a registered type MUST reach it only through its error-returning
form. `Validate()` remains the writer's full contract and MUST NOT panic either.

#### Scenario: A registered type decodes a malformed identity

- **WHEN** a registered Graphable payload is decoded with an org, platform or instance part that is empty or
  contains a dot
- **THEN** `EntityID()` returns `""` and `Validate()` returns an error, and neither panics

#### Scenario: A registered type decodes a malformed referenced identity

- **WHEN** a registered Graphable payload's `Triples()` would stamp an entity ID built from a malformed decoded
  field
- **THEN** that triple is omitted from `Triples()`, `Validate()` returns an error, and neither call panics

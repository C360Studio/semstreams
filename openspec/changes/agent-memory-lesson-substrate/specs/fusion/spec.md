# fusion — delta

## ADDED Requirements

### Requirement: The lessons facet is additive, opt-in, and never fabricated
The fusion engine SHALL include lesson content in a projection only when the request declares
`want:[lessons]`, and MUST omit the facet entirely — rather than emitting an empty or
fabricated section — when no lesson matches the request scope.

#### Scenario: Facet absent when not declared
- **WHEN** a fusion request omits `lessons` from its `want` list
- **THEN** the projection contains no lesson content, regardless of matching lessons in the
  graph

#### Scenario: Facet absent when nothing matches
- **WHEN** a request declares `want:[lessons]` and no active lesson's `lesson.applies_to`
  matches the request scope
- **THEN** the projection omits the lessons facet entirely

### Requirement: Lessons facet matching and ordering are deterministic
The lessons facet SHALL select lessons by deterministic string matching of `lesson.applies_to`
keys against the request's declared scope (entity-ID prefixes or tags) and SHALL order results
by severity, then recency, then entity-ID tiebreak, so that identical request and graph state
MUST yield an identical facet.

#### Scenario: Same inputs, same projection
- **WHEN** the same `want:[lessons]` request runs twice against unchanged graph state
- **THEN** both projections contain the same lessons in the same order

#### Scenario: No similarity ranking
- **WHEN** lessons match a request scope
- **THEN** their selection and order derive only from declared scope-key matches and the
  severity/recency/ID ordering, with no embedding or similarity scoring

### Requirement: Lessons facet delivery is bounded, observable, and retirement-aware
The lessons facet SHALL return at most K lesson injection forms (K request-declarable with a
framework default) together with matched-versus-returned counts, and MUST exclude lessons whose
status is retired or superseded from default delivery.

#### Scenario: Truncation is observable
- **WHEN** more lessons match than the facet bound K
- **THEN** the projection returns K injection forms plus counts showing how many matched and
  how many were returned

#### Scenario: Retired lessons are excluded
- **WHEN** a matching lesson has `lesson.status` of `retired` or `superseded`
- **THEN** it is absent from the facet while active matches are returned

#### Scenario: Injection forms carry their provenance
- **WHEN** the facet returns a lesson
- **THEN** the entry carries the lesson's entity ID alongside its injection form, so a
  consumer can dereference the full record

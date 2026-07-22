## ADDED Requirements

### Requirement: Offloaded entities embed their inline identity text alongside the body

The system MUST embed an offloaded entity's inline identity text — the triples
selected by the configured text suffixes — together with its resolved body,
identity-first, in a single vector, so the text-suffix configuration takes effect
on offloaded entities exactly as it does on inline ones. An offloaded entity is one
whose body is resolved from a storage reference; today only its body is embedded and
its identity triples (title, signature, comment, and the like) are silently
excluded, which also makes the text-suffix configuration inert for it.

The combined text is subject to the same embedding-text cap as any other lane;
because identity is placed first, truncation trims the body and the identity always
survives. The deduplication key is derived over the combined, truncated bytes (the
exact text embedded), so a change to either the identity or the body regenerates the
vector. An offloaded entity that carries no inline identity text embeds its body
alone, unchanged; symmetrically, an offloaded entity whose resolved body is empty
embeds its identity text alone (no trailing separator), so it deduplicates against
an inline entity carrying the same text.

#### Scenario: an offloaded entity embeds identity text ahead of its body

- **GIVEN** an offloaded entity carrying inline identity triples selected by the text suffixes
- **WHEN** its embedding text is produced
- **THEN** the embedded text is the identity text followed by the resolved body, in that order

#### Scenario: a text-suffix setting takes effect on offloaded entities

- **GIVEN** a text-suffix configured to include an identifying predicate (e.g. a code signature)
- **AND** an offloaded entity carrying that predicate and an offloaded body
- **WHEN** the entity is embedded
- **THEN** the predicate's text is present in the embedded text rather than excluded

#### Scenario: identity survives the cap ahead of the body

- **GIVEN** an offloaded entity whose identity-plus-body text exceeds the embedding-text cap
- **WHEN** the combined text is truncated at the cap
- **THEN** the identity text is retained and the body is trimmed from the end

#### Scenario: the deduplication key covers the combined text

- **GIVEN** an offloaded entity embedded from its identity text and body
- **WHEN** either the identity text or the body changes
- **THEN** the deduplication key changes and the vector is regenerated, never served from the prior bytes

#### Scenario: an offloaded entity with no inline identity text is unchanged

- **GIVEN** an offloaded entity carrying no inline text-suffix triples
- **WHEN** its embedding text is produced
- **THEN** the embedded text is the resolved body alone

#### Scenario: an offloaded entity with an empty body embeds its identity alone

- **GIVEN** an offloaded entity whose resolved body is empty and which carries inline identity text
- **WHEN** its embedding text is produced
- **THEN** the embedded text is the identity text alone, with no trailing separator
- **AND** it deduplicates against an inline entity whose text is that same identity

#### Scenario: identity inclusion on the offloaded lane is observable

- **WHEN** an offloaded entity is embedded
- **THEN** whether inline identity text was included alongside its body is reported, so a producer can confirm the text-suffix configuration took effect rather than infer it from silence

## ADDED Requirements

### Requirement: Entity-segment substitution tokens are named by position meaning

The substitution layer MUST expose `$entity.org`, `$entity.platform`, `$entity.system`, `$entity.domain`,
`$entity.type`, `$entity.instance` and the same six under `$related.`, and MUST resolve each token from the named
field of `pkg/types.ParseEntityID`, never from a raw index into the dotted string. A token name MUST keep its meaning
across any canonical-order change. An entity ID that fails canonical validation MUST leave the tokens unresolved so
the existing unresolved-template warning fires.

#### Scenario: system and domain resolve by name under the canonical order

- **GIVEN** a rule template `src=$entity.system dom=$entity.domain id=$entity.instance`
- **WHEN** it is substituted against `acme.dep1.src.git.commit.a1`
- **THEN** the result is `src=src dom=git id=a1`
- **AND** the test that verifies this is `TestSegmentTokensResolveByName`

#### Scenario: an invalid entity ID leaves the tokens unresolved

- **GIVEN** the same template and the value `acme.dep1.src.git.commit`
- **WHEN** substitution runs
- **THEN** every `$entity.<segment>` token survives unchanged
- **AND** the unresolved-template warning fires
- **AND** the test that verifies this is `TestSegmentTokensUnresolvedOnInvalidID`

## MODIFIED Requirements

### Requirement: The authority pair is bounded at configuration load

Configuration load MUST reject a `platform.org`/`platform.id` pair that cannot carry the framework-minted entropy
suffix: `len(org) + len(platform) + 7` MUST NOT exceed the budget derived from the longest fixed-suffix framework
family — `256 − 86 = 170` bytes while the rule trigger family (`rules.graph.trigger.` + 64 hex + two separators) is
the longest — naming the binding family in the error. The seven reserved bytes are the suffix
`component-runtime-config` mints onto `platform.id` (`-` plus six hex bytes); reserving them at load is what stops a
pair that fits only unsuffixed from being durably recorded and then refused forever, which ADR-102 decision 7 makes
unrepairable. A declared pair may therefore be at most 163 bytes.

The reserve MUST apply only where a pair is DECLARED, and a configuration MUST declare the stem — never a minted
identifier — so that one field carries one kind of value. An effective pair — a minted identifier, an adopted
identity record's, or the running configuration's — already carries whatever suffix it will ever carry and MUST be
bounded at the full 170-byte budget; reserving the same seven bytes against it as well would refuse, after Start, a
declaration that had already passed load. Every declaration boundary MUST apply the same 163-byte bound and every effective-pair
boundary the same 170-byte bound, so no path can admit a pair another path rejects. The budget MUST be derived from
the framework's own family table, never configured by the operator.
Framework constructors MUST keep fail-closed canonical validation as the second layer. This amends ADR-076 decision
2: framework identities are bounded, not fixed-length.

#### Scenario: an oversized authority pair does not boot

- **GIVEN** a configuration whose `platform.org` and `platform.id` total 164 bytes
- **WHEN** configuration load runs
- **THEN** it returns an error naming the trigger family, the 170-byte budget, and the seven bytes reserved for the
  minted suffix
- **AND** the test that verifies this is `TestConfigRejectsOversizedAuthorityPair`

#### Scenario: a pair that fits only unsuffixed is refused before anything is minted

- **GIVEN** a configuration whose `platform.org` and `platform.id` total 165 bytes, which would fit the 170-byte
  budget unsuffixed but not once seven bytes are minted onto it
- **WHEN** configuration load runs
- **THEN** the load fails and no identity record is created
- **AND** the test that verifies this is `TestConfigRejectsPairThatOnlyFitsUnsuffixed`

#### Scenario: a pair at the declarable budget boots

- **GIVEN** a configuration whose `platform.org` and `platform.id` total exactly 163 bytes
- **WHEN** the deployment loads that configuration and starts against an empty bucket
- **THEN** the load succeeds, the entropy suffix is minted, the effective pair is 170 bytes, and Start succeeds
- **AND** the tests that verify this are `TestMaximumDeclarablePairMintsAndStarts` and
  `TestEffectivePairIsBoundedWithoutTheDeclarationReserve`

## ADDED Requirements

### Requirement: A deployment provisioned from a cloned template does not share its authority pair

The framework MUST NOT let two deployments provisioned from one configuration template silently mint under the same
`org.platform` pair. `platform.id` MUST receive a framework-minted entropy suffix — `-` followed by six lowercase hex
bytes from `crypto/rand` — on the deployment's genuine first boot; the suffixed value is the deployment's `platform`
position from that boot on, and the mechanics of minting, persisting and adopting it are specified by
`component-runtime-config`. The framework MUST NOT decide "already minted" by inspecting the value's grammar, and no
configuration key, environment variable, or other value carried inside the cloned document MAY disable the mint: an
operator who owns global uniqueness declares it by pre-creating the deployment's identity record, which is
per-deployment by construction and cannot be cloned through a template.

#### Scenario: two fresh boots from one template mint distinct authorities

- **GIVEN** two deployments whose configuration files are byte-identical copies of one template with `platform.id` `dep`
- **WHEN** each boots for the first time against its own NATS server
- **THEN** the `org.platform` pair each mints under differs from the other's and each `platform` position is `dep-` followed by six hex bytes
- **AND** each deployment's pair is stable across its own later restarts
- **AND** the test that verifies this is `TestFirstBootMintsDistinctSuffixesPerDeployment`

#### Scenario: an operator-provisioned identity record is adopted unsuffixed

- **GIVEN** a configuration declaring `platform.id` `field-ops-7`
- **AND** an operator who created `semstreams_config/platform_identity` as `{"org":"acme","stem":"field-ops-7","id":"field-ops-7"}` before the deployment's first boot
- **WHEN** the deployment boots
- **THEN** its effective `platform` position is exactly `field-ops-7` and no suffix is minted
- **AND** the test that verifies this is `TestPreCreatedIdentityRecordIsAdoptedUnsuffixed`

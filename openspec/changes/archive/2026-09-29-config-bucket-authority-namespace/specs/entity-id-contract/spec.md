# entity-id-contract Delta

## MODIFIED Requirements

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
- **AND** an operator who created `semstreams_config_acme_field-ops-7/platform_identity` as `{"org":"acme","stem":"field-ops-7","id":"field-ops-7"}` before the deployment's first boot
- **WHEN** the deployment boots
- **THEN** its effective `platform` position is exactly `field-ops-7` and no suffix is minted
- **AND** the test that verifies this is `TestPreCreatedIdentityRecordIsAdoptedUnsuffixed`

# framework-bucket-catalog Delta

## MODIFIED Requirements

### Requirement: The framework catalog SHALL own the shared runtime configuration bucket

The catalog SHALL declare the runtime configuration bucket family — every bucket named `semstreams_config_<suffix>`
with a nonempty suffix, one per declared authority pair — as operational state whose retention AND write-ownership
the framework guarantees, with History 5, Replicas 1, no lifecycle reclamation, and owner-only writes. Resolving any
member name SHALL yield that one descriptor carrying the member's concrete name; the bare name `semstreams_config` is
not a member. Its declared owner is the config manager, the bucket's only acquirer; the rule ConfigManager writes
`rules.*` only through the key family the config manager serves it.

Owner-only is required by what the bucket now holds, not by how many components write it. The platform identity
record is create-once, and a boot ADOPTS a recorded identifier after validating only its segment grammar and byte
budget; a generic rule `update_kv` writing that key would therefore move the authority every entity is minted under,
or, with a mismatched value, prevent the deployment from ever booting again — neither repairable, because ADR-102
decision 7 forbids rewriting a minted authority. The derived generic-write guard is what makes ownership enforceable,
and the config manager does not consult it.

Every production acquisition of a member bucket SHALL resolve through this one descriptor. Two creators each
spelling their own bucket configuration is the split-owner shape the catalog exists to remove: the retention
guarantee would otherwise hold only for whichever creator won the race.

Its retention kind SHALL be strict: acquisition VERIFIES that no TTL and no binding size cap is in force and fails
closed when one is, and SHALL NOT reconcile the policy in place. Stripping a TTL repairs the policy while saying
nothing about the keys it already deleted, and for create-once identity that is the worse outcome — a silent repair
hands the next boot an empty bucket to mint a second authority into, which ADR-102 decision 7 forbids ever
reconciling.

#### Scenario: both writers of the shared configuration bucket resolve one descriptor

- **GIVEN** the config manager, which acquires the deployment's configuration bucket, and the rule ConfigManager,
  which writes `rules.*` through the config manager's key family
- **WHEN** the bucket is acquired
- **THEN** it resolves through the catalog descriptor and no other production file acquires it
- **AND** the tests that verify this are `TestCatalogBucketNamesAreNeverAcquiredDirectly` (no file naming a
  catalogued bucket makes a direct acquisition call, checked per call), `TestCatalogResolvingOwnersUseTheSeam` (the
  config manager still resolves the descriptor), `TestGenericKVWritersConsultTheCatalog` and
  `TestConfigBucketFamilyResolvesEveryMember`

#### Scenario: a generic rule write into the shared configuration bucket is refused

- **GIVEN** a rule pack whose `update_kv` action targets a configuration bucket such as `semstreams_config_acme_dep`,
  whether named literally or resolved from a variable at runtime
- **WHEN** the pack is loaded, and when the action executes
- **THEN** both the load-time and the runtime ownership guard reject it, naming the bucket's declared owners
- **AND** the rule engine's own KV writer refuses to acquire the bucket at all
- **AND** the tests that verify this are `TestUpdateKV_RejectsSharedConfigBucket_AtLoad`,
  `TestUpdateKV_RejectsSharedConfigBucket_AtRuntime` and `TestKVWriterRefusesCatalogedOwnerOnlyBucket`

#### Scenario: an evicting policy on a strict-retention bucket is refused, not repaired

- **GIVEN** the deployment's configuration bucket already exists carrying a TTL or a binding size cap
- **WHEN** an owner acquires it through the catalog
- **THEN** acquisition fails naming the bucket and the offending policy value
- **AND** the policy is left as found, because repairing it would conceal that the identity may already have expired
- **AND** the tests that verify this are `TestEvictingConfigBucketRefusesStart` and
  `TestIdentityUnderAnEvictingBucketNeverRemints`

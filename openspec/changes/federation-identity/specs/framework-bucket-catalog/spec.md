## ADDED Requirements

### Requirement: The framework catalog SHALL own the shared runtime configuration bucket

The catalog SHALL declare `semstreams_config` as operational state whose RETENTION the framework guarantees, with
History 5, Replicas 1, no lifecycle reclamation, and open writes. It is catalogued for retention rather than
write-ownership: two subsystems write it by design — the config manager for configuration keys and the rule
ConfigManager for `rules.*` — while the platform identity record it holds is create-once and MUST never be evicted.
Declaring it owner-only would add it to the derived generic-write guard set and change rule behaviour; open writes
declare what is true.

Every production acquisition of that bucket SHALL resolve through this one descriptor. Two creators each spelling
their own bucket configuration is the split-owner shape the catalog exists to remove: the retention guarantee would
otherwise hold only for whichever creator won the race.

Its retention kind SHALL be strict: acquisition VERIFIES that no TTL and no binding size cap is in force and fails
closed when one is, and SHALL NOT reconcile the policy in place. Stripping a TTL repairs the policy while saying
nothing about the keys it already deleted, and for create-once identity that is the worse outcome — a silent repair
hands the next boot an empty bucket to mint a second authority into, which ADR-102 decision 7 forbids ever
reconciling.

#### Scenario: both writers of the shared configuration bucket resolve one descriptor

- **GIVEN** the config manager and the rule ConfigManager, either of which may create the bucket first
- **WHEN** each acquires `semstreams_config`
- **THEN** both resolve it through the catalog descriptor rather than their own bucket configuration
- **AND** the test that verifies this is `TestSharedConfigBucketResolvesThroughOneDescriptor`

#### Scenario: an evicting policy on a strict-retention bucket is refused, not repaired

- **GIVEN** `semstreams_config` already exists carrying a TTL or a binding size cap
- **WHEN** an owner acquires it through the catalog
- **THEN** acquisition fails naming the bucket and the offending policy value
- **AND** the policy is left as found, because repairing it would conceal that the identity may already have expired
- **AND** the tests that verify this are `TestEvictingConfigBucketRefusesStart` and
  `TestIdentityUnderAnEvictingBucketNeverRemints`

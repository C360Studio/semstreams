## MODIFIED Requirements

### Requirement: The community index is rebuilt non-destructively

A detection run SHALL NOT empty the community index as a step in rebuilding it. It SHALL write candidate communities
over the prior partition in place. After every candidate community at every configured level has persisted
successfully, the detector SHALL invoke the storage removal step with the complete partition. Detectors SHALL NOT
clear the store before a rebuild.

Any candidate `SaveCommunity` failure makes that detection run incomplete. A record-local permanent rejection MAY
allow later writable siblings at the same level to be attempted and persisted, but the run SHALL return an error,
SHALL NOT construct dependent higher levels, SHALL NOT invoke `Prune`, and SHALL NOT report complete success.
Successful or partial candidate writes may overwrite a same-key community record or entity mappings before failure,
so readers may observe a mixed prior/candidate projection. The exact guarantee is that no prune-driven deletion occurs;
the framework does not promise rollback, byte-identical prior state, or an unmixed stale superset.

The removal step SHALL derive owned keys from the complete partition inside the storage layer. A removal failure after
a complete candidate SHALL NOT fail the run: every new community is already persisted, and stale extra keys may remain
until a later complete prune succeeds.

A genuinely empty authoritative graph is a complete candidate and SHALL invoke `Prune` with an empty keep set. A
failure of that prune remains nonfatal under the same complete-candidate rule.

#### Scenario: A reader mid-rebuild never observes an empty index

- **GIVEN** a populated community index and a detection run in progress
- **WHEN** a consumer reads the index
- **THEN** it may observe prior records, candidate records, overwritten mappings, or a mixed projection
- **AND** it never observes an empty index on account of a pre-rebuild clear

#### Scenario: A complete candidate attempts removal

- **GIVEN** every community at every configured level persisted successfully
- **WHEN** detection completes candidate persistence
- **THEN** it invokes `Prune` with the complete partition
- **AND** it reports complete success even if that removal attempt fails

#### Scenario: A permanent candidate rejection withholds prune and completion

- **GIVEN** a prior partition and a new level containing one permanently rejected community and writable siblings
- **WHEN** detection attempts the level
- **THEN** writable siblings may persist and overwrite mappings
- **AND** the run returns the existing permanent/invalid error classification
- **AND** `Prune` and prune-driven `Delete` are not invoked
- **AND** no complete-success accounting occurs

#### Scenario: A partial mapping write is still incomplete

- **GIVEN** `SaveCommunity` writes the community record and one entity mapping but a later mapping write fails
- **WHEN** detection receives that failure
- **THEN** the run returns an error and the earlier writes may remain visible
- **AND** `Prune` and prune-driven `Delete` are not invoked
- **AND** no rollback or unmixed prior projection is claimed

#### Scenario: A removal failure after a complete candidate is nonfatal

- **GIVEN** a complete persisted candidate whose `Prune` call fails
- **WHEN** the run returns
- **THEN** every community of the new partition is present
- **AND** stale keys may remain
- **AND** the run does not report an error
- **AND** a later complete cycle attempts removal again

#### Scenario: An empty authoritative graph attempts empty prune

- **GIVEN** authoritative enumeration completes successfully with zero entities
- **WHEN** detection runs
- **THEN** the empty candidate is treated as complete
- **AND** `Prune` is invoked with an empty keep set
- **AND** a prune failure is nonfatal and may leave old keys until a later complete cycle

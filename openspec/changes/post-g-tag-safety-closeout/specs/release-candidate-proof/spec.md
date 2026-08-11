## ADDED Requirements

### Requirement: Every advertised deterministic path has an explicit candidate disposition

Before candidate freeze, every retained advertised deterministic path SHALL have a green exact-candidate result in the
detached attestation. #301, #844, and #860 are retained gates. A nonzero test or wrapper result SHALL be treated as red,
and D SHALL NOT authorize a fix, removal, or coverage transfer merely because a retained path is red.

Every release-truth finding outside the approved runtime scope SHALL likewise be recorded as an accepted limitation,
a separately approved blocker, or a deferred named program. Recording a finding SHALL NOT imply conformance or
implementation authority.

The binding decision record SHALL be `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md`. It SHALL
record owner, decision date, disposition, and coverage/publication plan. It SHALL NOT predict the SHA of the commit
that contains it. Exact candidate identity, command/results, timestamps, and evidence pointers SHALL live in the
immutable detached GitHub Release attestation keyed to that SHA. Any required detached field left PENDING blocks
candidate freeze.

#### Scenario: A retained advertised path is red

- **GIVEN** #301, #844, #860, or another retained advertised deterministic path
- **WHEN** its candidate proof is red
- **THEN** candidate freeze is blocked
- **AND** wrapper silence or invocation shape does not convert the red result into success

#### Scenario: D encounters a red retained path

- **GIVEN** #301, #844, or #860 is red on the exact candidate
- **WHEN** D records the result
- **THEN** candidate freeze stops
- **AND** D does not authorize a runtime fix or remove the retained path

#### Scenario: A matrix finding remains outside runtime scope

- **GIVEN** a disposition-only finding such as DI-01 through DI-04, #619, #672, temporal cleanup, #857, or #829
- **WHEN** release disposition is recorded
- **THEN** the row names its limitation, blocker, or owning future program
- **AND** no runtime implementation is inferred from the disposition

#### Scenario: The accepted community-value limitation is published

- **GIVEN** #839 is accepted for this tag
- **WHEN** the candidate is prepared for publication
- **THEN** release material states that an oversized community value may be rejected by NATS
- **AND** it claims only #855's incomplete-candidate protection, not oversized-community success

### Requirement: Release proof is tied to one exact candidate

The release candidate SHALL be one clean exact commit SHA. Focused tests, lint, full race, integration,
schema/no-drift, contracts, strict OpenSpec, required deterministic E2E paths, independent review, and GitHub CI SHALL
refer to that same candidate.

The authoritative proof record SHALL be an immutable detached GitHub Release attestation keyed to the candidate's full
SHA. The in-tree `candidate-evidence.md`, covered by `manifest.sha256`, SHALL define its schema but SHALL NOT be
completed as evidence or redefine candidate identity. The detached record SHALL include candidate cleanliness,
command/result provenance, semantic polls, retained-path results, limitation publication, independent review and CI
identity, tag resolution, binary/container identity, and #827 outcome.

The detached attestation SHALL NOT contain or require its own SHA-256. A digest MAY be carried only by external GitHub
Release metadata or a sibling checksum asset created after upload, and SHALL NOT redefine candidate or attestation
identity.

Long-running paid or resource-intensive proof SHALL be actively polled using authoritative readiness, counters, and
stage progress. A provably wedged run SHALL be aborted rather than allowed to consume its natural timeout.

Any code, specification, evidence, generated-file, or task-truth correction after proof begins SHALL create a new
candidate identity and invalidate affected proof, review, and CI.

#### Scenario: The candidate cannot name itself

- **GIVEN** the in-tree decision and attestation-template files
- **WHEN** the candidate commit is created
- **THEN** neither file contains or predicts that commit's SHA
- **AND** the release owner creates the SHA-keyed detached attestation afterward

#### Scenario: The candidate changes after review

- **GIVEN** an independently reviewed candidate SHA
- **WHEN** any file in the release tree changes
- **THEN** the prior review and exact-candidate evidence no longer authorize release
- **AND** the corrected candidate is reproved and independently reviewed

#### Scenario: Semantic proof continues making progress

- **GIVEN** the semantic E2E is running
- **WHEN** the operator polls at the recorded cadence
- **THEN** readiness, counters, timestamps, or stage output demonstrate forward progress
- **AND** silence alone is never recorded as success

#### Scenario: A paid run is provably wedged

- **GIVEN** authoritative state has made no progress for more than twice the expected step duration
- **WHEN** the operator confirms the run cannot converge
- **THEN** the run is aborted
- **AND** the candidate remains unproven

### Requirement: The published tag and artifacts identify the approved candidate

The release tag SHALL resolve to the exact reviewed and CI-green candidate SHA. Release notes SHALL name clean breaks,
owner-accepted limitations, and downstream migration responsibility. The coordinated #827 wipe/reseed SHALL be
scheduled at the tag boundary; if its permitted pre-v1 window closes first, tagging SHALL halt and the operation SHALL
become an explicit migration.

Tag resolution, binary version/checksum, container reference/digest/reported version, and exact-SHA CI identity SHALL
be recorded in the detached attestation.

The built binary and published container SHALL report the intended version. Container tag and digest SHALL be recorded.
Downstream repositories MAY pin and migrate after publication; they SHALL NOT be treated as an exhaustive pre-tag gate.

#### Scenario: The tag points to a different commit

- **GIVEN** an approved candidate SHA
- **WHEN** the proposed tag resolves elsewhere
- **THEN** publication is blocked

#### Scenario: The pre-v1 wipe window closes

- **GIVEN** #827 has not executed and the permitted pre-v1 boundary has closed
- **WHEN** release preparation reaches tagging
- **THEN** tagging halts
- **AND** wipe/reseed is handled through an explicit migration instead

#### Scenario: A downstream has not yet migrated

- **GIVEN** the exact framework candidate is proven and published
- **WHEN** one downstream remains on an older pin
- **THEN** that adoption work does not create a framework compatibility shim
- **AND** the downstream migrates and proves product parity after pinning

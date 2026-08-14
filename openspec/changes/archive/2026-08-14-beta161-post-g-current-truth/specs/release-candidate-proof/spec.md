## ADDED Requirements

### Requirement: Every advertised deterministic path has an explicit candidate disposition

Every retained advertised deterministic path SHALL have a green exact-candidate result before tag authorization.
Issues #301, #844, and #860 are retained gates. A nonzero test or wrapper result SHALL be treated as red. The
documentation slice SHALL NOT authorize a fix, removal, or coverage transfer merely because a retained path is red.

Every release-truth finding outside approved runtime scope SHALL be recorded as an accepted limitation, a separately
approved blocker, or a deferred named program. Recording a finding SHALL NOT imply conformance or implementation
authority.

The binding decision record SHALL be `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md`. It SHALL
record owner, decision date, disposition, and coverage/publication plan. It SHALL NOT predict the SHA of its containing
commit. Exact candidate identity, command results, timestamps, and evidence pointers SHALL live in the immutable
`candidate-proof-<fullSHA>` GitHub Release asset. Product tag, artifact, fresh-state publication, and final decision
facts SHALL live in the separate immutable product-Release attestation.

#### Scenario: A retained advertised path is red

- **GIVEN** #301, #844, #860, or another retained advertised deterministic path
- **WHEN** its exact-candidate proof is red
- **THEN** tag authorization is blocked
- **AND** wrapper silence or invocation shape does not convert the red result into success

#### Scenario: The documentation slice encounters a red retained path

- **GIVEN** #301, #844, or #860 is red on the exact candidate
- **WHEN** the result is recorded
- **THEN** tag authorization stops
- **AND** the documentation slice does not authorize a runtime fix or remove the retained path

#### Scenario: A matrix finding remains outside runtime scope

- **GIVEN** a disposition-only finding such as DI-01 through DI-04, #619, #672, temporal cleanup, #857, or #829
- **WHEN** release disposition is recorded
- **THEN** the row names its limitation, blocker, or owning future program
- **AND** no runtime implementation is inferred from the disposition

#### Scenario: The accepted community-value limitation is published

- **GIVEN** #839 is accepted for the product release
- **WHEN** product Release notes are published
- **THEN** they state that an oversized community value may be rejected by NATS
- **AND** they claim only #855's incomplete-candidate protection, not oversized-community success

### Requirement: Candidate selection precedes immutable pre-tag proof

Candidate freeze SHALL mean selection of one clean immutable commit SHA after all in-tree preparation. The package
manifest SHALL be regenerated once as the final preparation edit and SHALL only be verified after selection. Candidate
selection SHALL NOT require a proof record, product tag, artifact, or downstream deployment fact that cannot yet
exist.

After candidate selection, proof runs SHALL collect local/run evidence. Only after every required gate is green SHALL
the release owner create a non-product GitHub Release tag named `candidate-proof-<fullSHA>` targeting the exact
candidate and publish its immutable asset. Because this tag does not start with `v`, it SHALL NOT trigger the current
product release or container workflows. Its immutable asset SHALL record only green pre-tag facts: candidate identity
and cleanliness, package-manifest verification, exact command/result provenance, semantic polls, retained-path
results, independent review, exact-SHA CI, the binding fresh-storage ruling and decision reference, and tag
authorization. A red gate
SHALL reject the candidate through local/run evidence and SHALL NOT require a failed candidate-proof Release.

The candidate-proof asset SHALL NOT contain or require its own URL or SHA-256. GitHub Release metadata or a sibling
checksum asset created after upload MAY carry that external verification metadata.

Any code, specification, generated-file, task-truth, or package-manifest correction after selection SHALL create a new
candidate identity and invalidate affected proof, review, and CI.

#### Scenario: The candidate cannot name itself

- **GIVEN** the in-tree decision and evidence-schema files
- **WHEN** the candidate commit is selected
- **THEN** neither file contains or predicts that commit's SHA
- **AND** the release owner creates the SHA-keyed candidate proof afterward

#### Scenario: The manifest is checked after selection

- **GIVEN** the manifest was regenerated as the final in-tree preparation edit
- **WHEN** candidate proof begins
- **THEN** the operator verifies every existing manifest entry
- **AND** does not regenerate or edit the manifest on the candidate

#### Scenario: The candidate changes after review

- **GIVEN** an independently reviewed candidate SHA
- **WHEN** any file in the release tree changes
- **THEN** the prior review and candidate proof no longer authorize release
- **AND** the corrected candidate is selected, reproved, and independently reviewed

### Requirement: Candidate proof binds exact commands and active observation

The candidate-proof record SHALL bind and record the exact commands in `candidate-evidence.md` for focused tests,
lint, full race, integration, schema generation, schema/spec no-drift, contracts, strict OpenSpec, statistical,
semantic, agentic, research direct-plus-execute, deep-research, crud-tools, and ops gates. It SHALL record runner
identity, UTC start/end, exit/result, and log or artifact SHA-256 for every command.

Every bound `go test` command SHALL use `-count=1` so cached results cannot satisfy exact-candidate proof. The focused
command SHALL cover both core graph packages, both processor wrappers, the store registry, test infrastructure, and
the crud-tools and research-graph scenario packages.

One `task e2e:research-graph` invocation SHALL prove both isolated direct and execute/fusion rounds. One
`task e2e:crud-tools` invocation MAY prove #301 and #860 only when their distinct assertions are identified. The #860
assertion SHALL send nine matching events to a rule configured with `FireEveryNEvents = 3` and observe exact per-rule
deltas of nine `semstreams_rule_evaluations_total{result="triggered"}` increments, zero
`semstreams_rule_evaluations_total{result="not_triggered"}` increments, and three
`semstreams_rule_action_gate_passes_total` increments. It SHALL NOT use
`semstreams_rule_events_published_total` as evidence of gate admission. An unreachable scrape, missing required
series after collector reachability is established, non-converging value, or different delta SHALL make #860 red.

Long-running paid or resource-intensive proof SHALL be polled every 30–60 seconds using `/readyz`, authoritative
counters, and stage timestamps. A provably wedged run SHALL be aborted rather than allowed to consume its natural
timeout.

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

#### Scenario: The retained #860 gate observes exact action admission

- **GIVEN** a live rule named by the fixture with `FireEveryNEvents = 3`
- **WHEN** the crud-tools scenario sends nine matching events
- **THEN** the triggered-evaluation delta is exactly nine
- **AND** the not-triggered-evaluation delta is exactly zero
- **AND** the named rule's action-gate-pass delta is exactly three
- **AND** optional rule-event publication metrics are not used to infer admission

#### Scenario: The retained #860 deltas do not converge

- **GIVEN** the #860 live proof has established collector reachability
- **WHEN** any required series is missing or any required delta differs from nine, zero, and three respectively
- **THEN** #860 is red
- **AND** tag authorization remains blocked

### Requirement: Tag authorization and publication are separate evidence phases

The release owner SHALL authorize a product tag only after all candidate-proof gates are green and the proof records
the binding owner-approved ruling that every downstream product adopting the stable release starts on newly
provisioned NATS storage, with its decision date and in-tree reference. Candidate proof SHALL NOT inspect or predict
future downstream storage.

The product tag SHALL resolve to the authorized candidate SHA. Release publication SHALL NOT perform or require a
destructive storage operation.

After publication, a separate immutable asset on the product GitHub Release SHALL link and externally digest the
candidate-proof asset. It SHALL record product tag resolution, binary version/checksum, container
reference/digest/reported version, inclusion of the fresh-storage premise in product Release notes, confirmation that
no destructive storage operation was performed during publication, the final release decision, and accepted or
deferred limitations. It SHALL NOT contain or require its own URL or SHA-256.

The candidate tree SHALL NOT be edited after proof to inject release facts. Downstream repositories MAY pin and adopt
after publication; they SHALL NOT be treated as exhaustive pre-tag gates. Discovery of retained deployed state SHALL
stop only the affected adoption and require a separate owner-reviewed migration or recovery design.

#### Scenario: Tag authorization is requested before proof is green

- **GIVEN** the candidate SHA has been selected
- **WHEN** any required pre-tag gate is red or missing
- **THEN** the release owner rejects tag authorization
- **AND** no failed candidate-proof Release is required

#### Scenario: The tag points to a different commit

- **GIVEN** an authorized candidate SHA
- **WHEN** the proposed product tag resolves elsewhere
- **THEN** publication is blocked

#### Scenario: Fresh-storage publication states the release premise

- **GIVEN** the exact framework candidate is proven and authorized
- **WHEN** the product tag and Release notes are published
- **THEN** the notes require downstream adoption on newly provisioned NATS storage
- **AND** the attestation records that no destructive storage operation was performed during publication

#### Scenario: Retained deployed state is discovered during adoption

- **GIVEN** a downstream begins adoption of the published release
- **WHEN** retained deployed NATS state is discovered
- **THEN** that adoption stops
- **AND** a separate owner-reviewed migration or recovery design is required
- **AND** no compatibility reader, alias, dual format, online migration, or rollback is inferred

#### Scenario: A downstream has not yet adopted

- **GIVEN** the exact framework candidate is proven and published
- **WHEN** one downstream remains on an older pin
- **THEN** that adoption work does not create a framework compatibility shim
- **AND** the downstream provisions fresh storage and proves product parity after pinning

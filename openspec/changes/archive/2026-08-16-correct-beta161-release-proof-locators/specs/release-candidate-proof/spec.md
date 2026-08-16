## MODIFIED Requirements

### Requirement: Every advertised deterministic path has an explicit candidate disposition

Every retained advertised deterministic path SHALL have a green exact-candidate result before tag authorization.
Issues #301, #844, and #860 are retained gates. A nonzero test or wrapper result SHALL be treated as red. The
documentation slice SHALL NOT authorize a fix, removal, or coverage transfer merely because a retained path is red.

Every release-truth finding outside approved runtime scope SHALL be recorded as an accepted limitation, a separately
approved blocker, or a deferred named program. Recording a finding SHALL NOT imply conformance or implementation
authority.

The binding decision record SHALL be
`openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/disposition-ledger.md`. It SHALL record owner, decision
date, disposition, and coverage/publication plan. It SHALL NOT predict the SHA of its containing commit. Exact
candidate identity, command results, timestamps, and evidence pointers SHALL live in the immutable
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

#### Scenario: The archived disposition is resolved literally

- **GIVEN** the accepted post-G package has been archived
- **WHEN** a release owner or reviewer follows the binding disposition locator
- **THEN** it resolves to the exact archived ledger without an alias or inferred active path

### Requirement: Candidate selection precedes immutable pre-tag proof

Candidate freeze SHALL mean selection of one clean immutable commit SHA after all in-tree preparation. The archived
post-G package manifest at
`openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/manifest.sha256` records the package frozen by the
accepted archive transaction. Read-only verification of its existing relative entries MAY occur during or after the
archive transaction. The archived manifest and every covered package body SHALL NOT be regenerated, edited, replaced,
or re-archived. Candidate selection SHALL NOT require a proof record, product tag, artifact, downstream deployment
fact, or prior verification result that cannot establish exact-candidate authority.

After candidate selection, exact-candidate proof SHALL freshly reverify every existing relative manifest entry from
the generated archive directory and SHALL collect new local/run evidence for the selected SHA. Only after every
required gate is green SHALL
the release owner create a non-product GitHub Release tag named `candidate-proof-<fullSHA>` targeting the exact
candidate and publish its immutable asset. Because this tag does not start with `v`, it SHALL NOT trigger the current
product release or container workflows. Its immutable asset SHALL record only green pre-tag facts: candidate identity
and cleanliness, package-manifest verification, exact command/result provenance, semantic polls, retained-path
results, independent review, exact-SHA CI, the binding fresh-storage ruling and decision reference, and tag
authorization. A red gate SHALL reject the candidate through local/run evidence and SHALL NOT require a failed
candidate-proof Release.

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

- **GIVEN** the post-G package and its existing manifest are immutable in the generated archive
- **WHEN** candidate proof begins
- **THEN** the operator freshly reverifies every existing relative manifest entry from the archive directory
- **AND** records exact-candidate provenance for that verification
- **AND** does not regenerate, edit, replace, or re-archive the archived manifest or package

#### Scenario: Read-only verification occurs before candidate selection

- **GIVEN** the accepted package is being archived or already resides in its generated archive directory
- **WHEN** a custodian performs a read-only manifest verification before candidate selection
- **THEN** the verification does not violate archive immutability
- **AND** it does not replace the mandatory reverification after exact-candidate selection

#### Scenario: The candidate changes after review

- **GIVEN** an independently reviewed candidate SHA
- **WHEN** any file in the release tree changes
- **THEN** the prior review and candidate proof no longer authorize release
- **AND** the corrected candidate is selected, reproved, and independently reviewed

### Requirement: Candidate proof binds exact commands and active observation

The candidate-proof record SHALL bind and record the exact commands in
`openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/candidate-evidence.md` for focused tests, lint, full
race, integration, schema generation, schema/spec no-drift, contracts, strict OpenSpec, statistical, semantic,
agentic, research direct-plus-execute, deep-research, crud-tools, and ops gates. It SHALL record runner identity, UTC
start/end, exit/result, and log or artifact SHA-256 for every command.

For beta.161, the detached candidate-proof record SHALL also contain a distinct normative row for `task e2e:core`.
That row and the existing `task e2e:semantic` row SHALL each record the exact command, runner identity, UTC start/end,
exit/result, and log or artifact SHA-256. Semantic proof SHALL additionally retain the mandatory active-polling
record below. Neither statistical coverage nor any pre-selection or prior-worktree result SHALL transfer, replace, or
satisfy either exact-candidate row.

The archived evidence schema is immutable and retains four former-active locators. Across the detached candidate
proof and separate post-publication attestation, the records SHALL inventory all four frozen values and their explicit
translations:

- Manifest command: `(cd openspec/changes/post-g-tag-safety-closeout && shasum -a 256 -c manifest.sha256)` translates
  to `(cd openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout && shasum -a 256 -c manifest.sha256)`.
- Disposition ledger:
  `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md` translates to
  `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/disposition-ledger.md`.
- Candidate-proof fresh-storage decision reference:
  `openspec/changes/post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise` translates to
  `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise`.
- Post-publication fresh-storage decision reference:
  `openspec/changes/post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise` translates to
  `openspec/changes/archive/2026-08-14-post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise`.

The candidate-proof record SHALL carry the manifest-command, disposition-ledger, and candidate-proof decision-
reference mappings. It SHALL also carry the exact archive-path manifest result, runner identity, UTC start/end,
exit/result, and log digest. The product-Release attestation SHALL carry only the post-publication decision-reference
mapping in its fresh-storage decision-reference field; it SHALL NOT duplicate the manifest-command, manifest-result,
manifest-provenance, manifest-log-digest, or disposition-ledger evidence. Neither record SHALL edit the archived
schema, infer a compatibility alias, restore an active copy, or treat an old-SHA or worktree result as proof authority.

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

#### Scenario: A frozen template locator is translated transparently

- **GIVEN** the archived evidence schema contains a frozen manifest command, ledger path, and two fresh-storage
  decision references using the former active directory
- **WHEN** the detached candidate proof and post-publication attestation are completed
- **THEN** all four locators are preserved as frozen inputs and translated to their exact generated archive forms
- **AND** the product attestation uses the translated post-publication fresh-storage decision reference
- **AND** no archive byte, alias, or restored active copy is used to make the old path resolve

#### Scenario: Core and semantic proof belong to the exact candidate

- **GIVEN** beta.161 includes the breaking lifecycle change
- **WHEN** candidate proof records its required E2E gates
- **THEN** `task e2e:core` and `task e2e:semantic` each have a distinct provenance-complete row for the selected SHA
- **AND** semantic proof includes the required 30–60 second active-polling observations
- **AND** prior-SHA, pre-selection, or worktree-only success satisfies neither row

## ADDED Requirements

### Requirement: Desired rule definitions are durable KV facts

Rule create, update, and delete SHALL target one already-composed `pack_id`. They SHALL write a typed desired record to
the authoritative pack-scoped rules KV namespace and return an opaque activation receipt containing `pack_id`,
`rule_id`, and the exact committed desired revision.

The desired record SHALL distinguish `present` from `deleted`. Delete SHALL commit a tombstone through a revision-
returning write rather than call the current error-only KV Delete API or infer a revision after the fact. One current
record per pack/rule identity keeps restart replay and receipt comparison deterministic.

A successful write SHALL report activation `pending` until an owning Rule processor publishes or the typed activation
reader derives a terminal outcome for that receipt.

The writer SHALL NOT claim that a rule is active, inactive, updated, or deleted in the running processor solely because
the KV mutation succeeded.

#### Scenario: Create returns desired revision, not fabricated activation

- **WHEN** a valid rule create commits at revision R
- **THEN** the response contains R and activation `pending`
- **AND** it does not claim the rule is active

#### Scenario: Delete returns a committed tombstone receipt

- **GIVEN** rule D exists in pack P
- **WHEN** delete commits desired tombstone revision R
- **THEN** the response contains receipt `(P, D, R)` and activation `pending`
- **AND** restart replay observes deletion without inferring a missing-key revision

#### Scenario: Authoring target is unambiguous

- **GIVEN** independently composed rule packs P1 and P2
- **WHEN** a caller authors rule D for P1
- **THEN** only P1's desired record changes
- **AND** no global `rules.D` record asks both packs to predict ownership

#### Scenario: Invalid rule never enters desired state

- **WHEN** a rule definition fails the complete authoring contract
- **THEN** no desired KV revision is committed
- **AND** no activation outcome is published

### Requirement: Rule hot reload is bounded to definitions inside a fixed component envelope

An admitted Rule processor MAY activate expression and cron rule-definition create/update/delete while running. The hot
path SHALL accept Definition content only.

Ports, dependencies, entity-watch buckets, graph integration, producer identity, projection contract/index bindings,
and every other Rule component configuration field SHALL remain the immutable boot envelope. A request that changes
that envelope SHALL be rejected from the hot path and reported as restart-required desired config.

#### Scenario: Definition update can activate live

- **GIVEN** a running Rule processor whose fixed boot envelope can execute rule D
- **WHEN** desired rule definition D changes at revision R
- **THEN** the processor may validate and activate a new rule-set generation containing R
- **AND** the Rule component generation and declaration remain unchanged

#### Scenario: Watch-bucket change cannot enter through rule reload

- **WHEN** a live mutation includes `entity_watch_buckets` or another Rule component field
- **THEN** rule hot reload rejects it before runtime mutation
- **AND** the response identifies restart as the activation boundary

### Requirement: Rule activation is atomic and preserves the previous generation on rejection

The Rule processor SHALL build and validate a complete candidate rule set before changing the active generation. A
validation, construction, projection-contract, cron-registration, or commit-precondition failure SHALL leave the
previous active generation unchanged.

Successful activation SHALL publish one new process-local rule-set generation. Readers and evaluators SHALL observe
the complete previous or complete new generation, never a partially updated rule map or scheduler.

#### Scenario: Invalid candidate leaves active rules unchanged

- **GIVEN** active rule-set generation G
- **WHEN** desired revision R produces an invalid candidate
- **THEN** G remains active and complete
- **AND** R receives terminal outcome `rejected` with a typed cause

#### Scenario: Successful candidate swaps as one generation

- **GIVEN** active generation G and valid candidate G+1
- **WHEN** activation commits
- **THEN** evaluation observes complete G before commit or complete G+1 after commit
- **AND** the desired revision receives terminal outcome `applied`

### Requirement: Every observed desired revision reaches an observable terminal outcome

The owning Rule processor SHALL publish processor-instance-scoped activation facts to a framework-cataloged bounded KV
store. Processor identity SHALL include a freshly generated, never-reused `boot_id`, stable process-slot identity, and
Rule component identity. Each observed desired revision SHALL reach exactly one terminal state: `applied`, `rejected`,
`superseded`, or `canceled_shutdown`. The current active rule-set generation SHALL also be observable.

An activation fact SHALL identify the Rule processor instance, desired revision, terminal state, active generation,
and typed failure or superseding revision when applicable. Multiple Rule processors SHALL publish independent truth.

Rule's existing readiness envelope in `GRAPH_STATUS` SHALL be the sole liveness fact. `process_slot` SHALL be the
validated, non-empty `platform.instance_id` sealed at boot; Rule hot-reload admission SHALL fail when it is absent. The
framework-owned key SHALL be stable per `(process_slot, component_id, pack_id)` and SHALL preserve History 3. The
envelope SHALL carry the exact `boot_id` and repeat those stable identities. A new boot SHALL overwrite its stable slot
rather than create a per-boot key.

Rule SHALL claim a missing or expired stable slot with KV compare-and-set. A fresh slot carrying a different `boot_id`
SHALL fail admission with typed `readiness_slot_collision` and SHALL NOT be overwritten. Heartbeats SHALL update the
claimed revision with compare-and-set; loss of ownership SHALL degrade Rule readiness and hot reload. The typed reader
SHALL join activation facts with the exact envelope `boot_id` and use the existing
consumer-local three-heartbeat freshness rule. A clean Stop SHALL make the readiness incarnation not current only
after required terminal activation facts commit; a dirty shutdown SHALL become historical through heartbeat expiry.
Expired, explicitly stopping, or tombstoned readiness facts SHALL classify matching activation facts as historical
and SHALL NOT merge them with current activation truth. Unreadable or indeterminate readiness SHALL yield `unknown`
current activation rather than a stale fallback. No second membership or liveness catalog SHALL exist.

The store SHALL keep one current status key per `(boot_id, component_id, pack_id, rule_id)` with KV history fixed at
five revisions. Framework GC SHALL purge keys belonging to expired boot incarnations after the `GRAPH_STATUS`
freshness grace period
and SHALL retain at most the five most recent boot incarnations per stable process/component slot. A receipt outside
retained history SHALL return typed `history_expired` unless a newer desired record proves `superseded`; the reader
SHALL NOT guess that the old receipt was applied or rejected. Retention and liveness constants SHALL NOT be
adopter-facing tuning knobs.

A typed framework activation reader SHALL be born with the status store. In-process rule tool executors SHALL use its
admitted operation-specific Go interface. A remote web client SHALL use a schema-defined operation on the existing
GraphQL-shaped HTTP facade, backed by the same reader. Rule create/update/delete responses, `get_rule`, `list_rules`,
and a dedicated activation-status read operation SHALL consume that reader and SHALL NOT expose or require bucket/key
grammar. The current in-process tools SHALL NOT add an MCP network hop. For a receipt older than the current desired
record, the reader MAY derive `superseded` even when a watcher never observed the intermediate revision.

#### Scenario: Coalesced write is not mislabeled applied

- **GIVEN** revisions R1 and R2 arrive before one candidate activation
- **WHEN** the candidate represents R2 and R1 was never independently active
- **THEN** R1 reaches `superseded` by R2
- **AND** only R2 may reach `applied`

#### Scenario: Multiple processors do not overwrite one another

- **GIVEN** Rule processors A and B observe desired revision R
- **WHEN** A applies R and B rejects R
- **THEN** A's status reports applied and B's status reports rejected
- **AND** neither status overwrites the other

#### Scenario: Crashed processor is not reported current

- **GIVEN** processor instance A published active generation G and then lost power
- **WHEN** A's framework-owned liveness evidence expires
- **THEN** the typed reader no longer reports A/G as currently active
- **AND** any retained terminal history is labeled stale process evidence

#### Scenario: New boot cannot inherit stale current status

- **GIVEN** boot B published active generation G before losing power
- **AND** replacement boot B' uses the same stable process slot and component identity
- **WHEN** the typed reader classifies current activation
- **THEN** only facts carrying B' and its exact fresh readiness incarnation can be current
- **AND** B's facts remain historical even if their payload otherwise matches B'

#### Scenario: Unknown readiness fails honest

- **GIVEN** activation facts exist but Rule `GRAPH_STATUS` cannot establish current freshness
- **WHEN** a caller reads activation status
- **THEN** current activation is `unknown`
- **AND** retained history is returned only as historical evidence

#### Scenario: Rule readiness is the sole liveness join

- **GIVEN** activation fact A names boot B, component C, and pack P
- **WHEN** the typed reader determines whether A is current
- **THEN** it joins A only to the fresh Rule `GRAPH_STATUS` envelope carrying B, C, and P
- **AND** no separate membership catalog can disagree with Rule readiness freshness

#### Scenario: Caller observes status without storage knowledge

- **GIVEN** a caller received activation receipt A
- **WHEN** it uses the typed activation-status read operation
- **THEN** it receives per-processor pending or terminal truth for A
- **AND** it supplies no bucket name, key grammar, process identity, or debounce timing

#### Scenario: Shutdown settles an observed candidate honestly

- **GIVEN** revision R was observed but shutdown fences activation before commit
- **WHEN** the processor terminates the candidate
- **THEN** R reaches `canceled_shutdown`
- **AND** no status claims R became active

#### Scenario: Restart replay reconstructs active truth

- **GIVEN** desired rule facts exist before Rule processor start
- **WHEN** the processor replays the current KV snapshot
- **THEN** it validates and installs one complete initial rule-set generation
- **AND** publishes the generation's revision-bound activation truth

### Requirement: Rule hot reload lifetime descends from Rule Start

The Rule processor SHALL own watching, candidate preparation, activation, and status publication under its Start
context. Goroutines SHALL receive that context as a function parameter. Production structs and retained callbacks
SHALL NOT store or recover it; only private cancellation and join state may be retained.

For planned shutdown, Stop SHALL first close a private activation-admission fence without canceling the Start context.
The Start-owned supervisor SHALL finish or reject any commit-selected candidate and publish
`canceled_shutdown` for every observed, uncommitted revision while its Start context and NATS authority remain live.
Stop SHALL use its context only to bound waiting for that supervisor-owned terminal publication. After the publication
barrier completes, the owner SHALL cancel and join the Start lifetime. Transport drain SHALL occur only after required
status publications have been accepted. No detached cleanup or background root SHALL be created for rule activation.

If the Stop deadline prevents a required terminal publication, Stop SHALL return a typed shutdown failure naming the
revision and publication phase. It SHALL NOT fabricate `canceled_shutdown`, mark the readiness incarnation stopped,
or report a clean activation boundary. Abrupt parent cancellation MAY prevent publication and therefore relies on
dirty-restart readiness expiry and reconciliation rather than invented terminal status.

Watcher bootstrap failure, unexpected watcher closure, reconcile failure, and activation-status publication failure
SHALL be visible in Rule readiness and metrics. The Start-owned supervisor SHALL retry transport failures with bounded
framework-owned backoff and full-snapshot reconciliation. Existing active rules MAY continue while hot reload reports
degraded; the processor SHALL NOT silently describe hot reload as available.

#### Scenario: Parent cancellation ends rule activation work

- **GIVEN** a Rule processor started with context C
- **WHEN** C is canceled
- **THEN** its rule watcher, candidate work, and status publisher stop
- **AND** no terminal outcome is fabricated if cancellation removed publication authority
- **AND** a replacement boot uses readiness expiry and desired-state replay to reconstruct current truth

#### Scenario: Planned Stop races activation deterministically

- **GIVEN** Stop races candidate activation for revision R
- **WHEN** Stop closes the private activation fence while the Start context remains live
- **THEN** activation either commits completely before the fence or does not commit
- **AND** the Start-owned supervisor publishes `canceled_shutdown` for observed uncommitted R
- **AND** Stop waits for terminal publication before canceling and joining the Start lifetime
- **AND** transport drain follows the publication barrier

#### Scenario: Terminal publication deadline fails shutdown

- **GIVEN** planned Stop fenced observed revision R before activation commit
- **WHEN** `canceled_shutdown` cannot be published before the Stop deadline
- **THEN** Stop returns a typed failed-shutdown result naming R and the publication phase
- **AND** it does not mark readiness stopped or claim a clean shutdown boundary

#### Scenario: Watcher loss degrades and repairs

- **GIVEN** a running Rule processor loses its desired-rule KV watcher
- **WHEN** the watcher closes unexpectedly
- **THEN** Rule readiness reports hot reload degraded and metrics identify watcher loss
- **AND** the supervisor retries under its Start context
- **AND** successful repair performs a full desired snapshot reconciliation before reporting ready

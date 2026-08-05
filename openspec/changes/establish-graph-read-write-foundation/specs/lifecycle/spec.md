# lifecycle — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: Lifecycle entity reclamation

Lifecycle delete MUST exact-read the entity, submit its nonzero KV revision to conditional `entity.delete`, and
propagate
typed not-found, revision-mismatch, poison, unavailable, and `commit_unknown` outcomes. It MUST NOT consult an owner
token, lease, incarnation, quiescence state, or overlap registry. Delete has no relationship cascade or stub cleanup.

#### Scenario: A newer transition cannot be deleted by a stale reclaim

- **GIVEN** lifecycle read entity A at revision R and A advanced to R+1
- **WHEN** reclamation submits expected revision R
- **THEN** delete returns typed revision mismatch
- **AND** A at R+1 remains authoritative

### Requirement: Transition-then-reclaim convenience

Transition-then-reclaim MUST preserve the transition's exact committing revision and use that revision for conditional
delete. A lost mutation reply MUST surface `commit_unknown`; convenience code MUST NOT infer success from author
identity
or retry an ambiguous operation automatically.

#### Scenario: Ambiguous transition stops convenience reclamation

- **GIVEN** the transition reply is lost after possible delivery
- **WHEN** transition-then-reclaim handles the outcome
- **THEN** it returns `commit_unknown`
- **AND** it does not issue delete against a predicted revision

### Requirement: Workflow registration MUST reject a state type that cannot be projected

Workflow registration MUST validate projection shape, entity identity, lifecycle predicates, and local contract
compatibility. Registration records workflow capability; it MUST NOT claim semantic predicates or reject cross-component
overlap. Any genuine runtime conflict is observed through CAS outcomes.

#### Scenario: Registration records without granting write permission

- **GIVEN** two workflows project an overlapping lifecycle predicate
- **WHEN** each valid workflow registers
- **THEN** registration may succeed
- **AND** no ownership claim, lease, or token is created

### Requirement: Must-exist lanes MUST NOT create state

Every lifecycle transition and reconcile against an absent entity MUST return typed `entity_not_found`. It MUST NOT
create state, a referential stub, or a pending operation. The component decides whether a later retry is meaningful.

#### Scenario: A transition racing birth reports absence

- **GIVEN** a lifecycle transition arrives before entity birth
- **WHEN** graph-ingest evaluates it
- **THEN** it returns typed not-found
- **AND** no placeholder entity is created

### Requirement: The operator surface MUST map every condition its callees can raise

The operator gateway MUST preserve typed create, reconcile, delete, exact-read, and `commit_unknown` outcomes. Ownership
quiesce and owner-lease-stale mappings MUST be deleted because those conditions no longer exist.

#### Scenario: Operator sees revision conflict

- **GIVEN** an operator acts from a stale exact read
- **WHEN** lifecycle returns revision mismatch
- **THEN** the gateway exposes the conflict without converting it to success or an ownership error

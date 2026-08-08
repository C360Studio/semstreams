# predicate-contract — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: Vocabulary declaration and namespace authority are explicit and separate from syntax

Canonical three-segment predicate syntax and vocabulary declaration MUST remain separate concerns. Neither vocabulary
membership, namespace declaration, component identity, nor a local projection contract grants global write permission.
Mutation admission is determined by the selected typed operation, local contract validation where present, and observed
Create/CAS outcome.

#### Scenario: Valid vocabulary does not predict mutation success

- **GIVEN** a canonical declared predicate
- **WHEN** a component reconciles from a stale revision
- **THEN** the mutation returns revision mismatch
- **AND** vocabulary authority does not override CAS

### Requirement: Mutation-lane access MUST be treated as the trust boundary, not as authenticated identity

NATS permissions and endpoint authentication remain the infrastructure trust boundary. The typed mutation port and
operation schema validate admissible content, but a request's component, pack, or message identity MUST NOT be treated
as
semantic ownership proof. The canonical subject set is the four-operation `graph.mutation.>` protocol.

#### Scenario: Authenticated caller still receives a storage conflict

- **GIVEN** an authenticated component sends a valid reconcile with stale evidence
- **WHEN** graph-ingest evaluates it
- **THEN** the caller receives revision mismatch
- **AND** authentication does not grant last-write authority

### Requirement: The beta cutover updates owned producers and resets incompatible state

The pre-v1 cutover MUST update every in-repo mutation caller, schema, configuration, fixture, and binary in one
coordinated break. No alias subject, legacy body, owner-token tolerance, or mixed-version reader is shipped. Retired
ownership buckets
are discarded under the existing clean wipe/reseed policy.

#### Scenario: Old eight-subject caller fails visibly

- **GIVEN** a caller still uses a retired mutation subject
- **WHEN** it runs against the post-cutover framework
- **THEN** it receives no compatible handler
- **AND** no fallback silently changes semantics

# Post-G tag-safety disposition ledger

**Status:** Binding owner decisions recorded on 2026-08-11. Candidate-specific results and publication evidence
remain PENDING in the detached GitHub Release attestation; those fields block candidate freeze.

**Binding owner:** Coby, SemStreams repository owner.

**Decision date:** 2026-08-11. The exact UTC decision time was not captured and is not reconstructed here.

**Custodian:** Technical writer, responsible for faithful transcription and conservative task truth; custody is not
decision authority.

**Candidate identity and evidence:** Not stored in this ledger. The exact candidate SHA, commands, results, and
evidence pointers belong in the immutable detached attestation keyed by that SHA. This in-tree record cannot predict
the SHA of the commit that contains it.

| Finding | Surface | Owner decision | Disposition | Coverage or publication plan | Candidate result / evidence |
|---|---|---|---|---|---|
| #301 | Advertised crud-tools path | Retain; the exact candidate must pass or freeze stops. | `retained-candidate-gate` | Run `task e2e:crud-tools` on the exact candidate. No fix is authorized by D if red. | PENDING — detached attestation |
| #844 | Advertised ops path | Retain; the exact candidate must pass or freeze stops. | `retained-candidate-gate` | Run `task e2e:ops` on the exact candidate. No fix is authorized by D if red. | PENDING — detached attestation |
| #860 | Advertised rule/crud-tools path | Retain; the exact candidate must pass or freeze stops. | `retained-candidate-gate` | Run `task e2e:crud-tools` and retain its rule assertions on the exact candidate. No fix is authorized by D if red. | PENDING — detached attestation |
| DI-01 | Suffix collision/retraction | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — limitation publication in detached attestation |
| DI-02 | Alias collision/retraction | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — limitation publication in detached attestation |
| DI-03 | Spatial stale rows/malformed aggregate | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — limitation publication in detached attestation |
| #619 | BM25/dedup lifecycle | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — limitation publication in detached attestation |
| #672 | Clustering identity caches | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — limitation publication in detached attestation |
| Temporal cleanup | Malformed aggregate and reverse cleanup failures | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — limitation publication in detached attestation |
| DI-04 | Anomaly lifecycle/cleanup truth | Defer explicitly; publish the limitation. | `deferred-named-program` | Anomaly Lifecycle and Retention Program | PENDING — limitation publication in detached attestation |
| #839 | Community value can exceed the NATS payload ceiling | Accept the measured limitation for this tag. | `accepted-release-limitation` | Retain #855 incomplete-candidate protection and statistical E2E; promise no oversized-community success. | PENDING — limitation publication in detached attestation |
| #857 | Framework payload bounds and retention | Defer explicitly; publish the limitation. | `deferred-named-program` | Payload Bounds and Retention Program | PENDING — limitation publication in detached attestation |
| #829 | Semantic summary content/quality | Defer explicitly; publish the limitation. | `deferred-named-program` | Semantic Summary Content/Quality Program | PENDING — limitation publication in detached attestation |

`retained-candidate-gate` records the binding decision to retain a capability; it does not claim the candidate is
green. `accepted-release-limitation` and `deferred-named-program` do not imply conformance or authorize runtime work.
Inventory presence is not a disposition. No issue is closed by this ledger.

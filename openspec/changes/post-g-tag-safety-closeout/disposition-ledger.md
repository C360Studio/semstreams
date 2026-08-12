# Post-G tag-safety disposition ledger

**Status:** Binding owner decisions recorded on 2026-08-11. Candidate-specific results remain PENDING. A fully green
candidate publishes the pre-tag proof record; a red required result blocks tag authorization and needs no failed
candidate-proof Release.

**Binding owner:** Coby, SemStreams repository owner.

**Decision date:** 2026-08-11. The exact UTC decision time was not captured and is not reconstructed here.

**Custodian:** Technical writer, responsible for faithful transcription and conservative task truth; custody is not
decision authority.

**Candidate identity and evidence:** Not stored in this ledger. The exact candidate SHA, commands, results, and
evidence pointers belong in the immutable `candidate-proof-<fullSHA>` Release asset. Product tag, artifact, and
fresh-state publication facts belong in the separate product-Release attestation. This in-tree record cannot predict
its containing SHA.

| Finding | Surface | Owner decision | Disposition | Coverage or publication plan | Candidate result / evidence |
|---|---|---|---|---|---|
| #301 | Advertised crud-tools path | Retain; the exact candidate must pass or tag authorization stops. | `retained-candidate-gate` | Run `task e2e:crud-tools` on the exact candidate. No fix is authorized by D if red. | PENDING — candidate proof |
| #844 | Advertised ops path | Retain; the exact candidate must pass or tag authorization stops. | `retained-candidate-gate` | Run `task e2e:ops` on the exact candidate. No fix is authorized by D if red. | PENDING — candidate proof |
| #860 | Advertised rule/crud-tools path | Retain; the exact candidate must pass or tag authorization stops. | `retained-candidate-gate` | Run `task e2e:crud-tools` and retain its rule assertions. No fix is authorized by D if red. | PENDING — candidate proof |
| #827 | Former stable-release storage gate | Superseded by the owner ruling that every downstream adoption starts on newly provisioned NATS storage. | `superseded-release-gate` | Close after this housekeeping change merges. Discovery of retained deployed state requires a separate owner-reviewed migration or recovery proposal. | PENDING — housekeeping merge |
| DI-01 | Suffix collision/retraction | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — product Release notes |
| DI-02 | Alias collision/retraction | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — product Release notes |
| DI-03 | Spatial stale rows/malformed aggregate | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — product Release notes |
| #619 | BM25/dedup lifecycle | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — product Release notes |
| #672 | Clustering identity caches | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — product Release notes |
| Temporal cleanup | Malformed aggregate and reverse cleanup failures | Defer explicitly; publish the limitation. | `deferred-named-program` | Derived-Index Convergence Program | PENDING — product Release notes |
| DI-04 | Anomaly lifecycle/cleanup truth | Defer explicitly; publish the limitation. | `deferred-named-program` | Anomaly Lifecycle and Retention Program | PENDING — product Release notes |
| #839 | Community value can exceed the NATS payload ceiling | Accept the measured limitation for this tag. | `accepted-release-limitation` | Retain #855 incomplete-candidate protection and statistical E2E; promise no oversized-community success. | PENDING — product Release notes |
| #857 | Framework payload bounds and retention | Defer explicitly; publish the limitation. | `deferred-named-program` | Payload Bounds and Retention Program | PENDING — product Release notes |
| #829 | Semantic summary content/quality | Defer explicitly; publish the limitation. | `deferred-named-program` | Semantic Summary Content/Quality Program | PENDING — product Release notes |

`retained-candidate-gate` records the binding decision to retain a capability; it does not claim the candidate is
green. `accepted-release-limitation` and `deferred-named-program` do not imply conformance or authorize runtime work.
Inventory presence is not a disposition. No issue is closed by this ledger alone. #827 is not a coordinated release
operation and is closed only after the housekeeping merge supplies its durable supersession reference.

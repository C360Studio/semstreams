# Post-G tag-safety evidence schema

**Status:** In-tree schema/template only. This file is neither candidate proof nor a release attestation and MUST NOT
be completed with a candidate SHA or run result.

**Evidence owner:** Release owner.

**Custodian:** Technical writer. Independent SemStreams review validates the exact candidate identity, diff, and
proof records.

## Evidence order

Candidate freeze means selecting one clean immutable commit SHA. It happens before proof. No proof record, tag,
artifact, or release fact is required to select that SHA.

Evidence is published in two distinct immutable records:

1. **Pre-tag candidate proof.** After candidate selection, collect every required pre-tag result. Only when all gates
   are green may the release owner create a non-product GitHub Release tag named `candidate-proof-<fullSHA>` targeting
   the exact candidate and publish its immutable asset. Because the tag does not start with `v`, it MUST NOT trigger
   the product release or container workflows. A red gate rejects the candidate through local/run evidence; it does
   not require publication of a failed candidate-proof Release.
2. **Post-publication release attestation.** After the product tag boundary, publish a separate immutable asset on the
   product GitHub Release. It links and digests the candidate-proof asset, then records tag resolution, published
   artifacts, fresh-state Release-note inclusion, no destructive storage operation, the final release decision, and
   limitations.

Neither asset contains or requires its own URL or SHA-256. GitHub Release metadata or a sibling checksum asset created
after upload MAY carry the asset URL or digest. That metadata does not redefine candidate or evidence identity.

Any correction to code, specification, generated content, task truth, or the package manifest selects a new candidate
SHA and requires a new `candidate-proof-<fullSHA>` record. Evidence from the old candidate is not carried forward as
release authority.

## Pre-tag candidate-proof record

### Candidate identity

| Required field | Detached value |
|---|---|
| Candidate full SHA | `<required>` |
| Candidate commit URL | `<required>` |
| Proof tag | `candidate-proof-<fullSHA>` |
| Proof tag-resolved SHA and command output | `<required>` |
| Clean `git status --short` | `<required>` |
| Accepted inventory SHA-256 | `8368e9b17e869561ca5c2123c8028d1311e449dae930c483d450c627a4acfcc6` |
| Package-manifest SHA-256 | `<required>` |
| Package-manifest verification | `(cd openspec/changes/post-g-tag-safety-closeout && shasum -a 256 -c manifest.sha256)` |
| Generated schema/spec no-drift result | `<required>` |
| Disposition ledger commit path | `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md` |

### Command and result provenance

Each row records runner identity, UTC start and end, exit/result, and a log or artifact SHA-256 in addition to the
bound command below.

| Gate | Exact command | Runner identity | Start UTC | End UTC | Exit/result | Log or artifact SHA-256 |
|---|---|---|---|---|---|---|
| Focused affected tests | `go test -count=1 -race ./graph/clustering ./graph/embedding ./processor/graph-clustering ./processor/graph-embedding ./storage/storeregistry ./test/testinfra ./test/e2e/scenarios/crud-tools ./test/e2e/scenarios/research-graph` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Lint | `task lint` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Full race | `go test -count=1 -race ./...` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Integration | `task test:integration` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Schema generation | `task schema:generate` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Schema/spec no drift | `task schema:check-changes` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Contracts | `go test -count=1 ./test/contract/...` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Strict OpenSpec | `task openspec:validate` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Statistical E2E | `task e2e:statistical` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Semantic E2E | `task e2e:semantic` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Agentic E2E | `task e2e:agentic` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Research direct and execute/fusion rounds | `task e2e:research-graph` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Deep-research E2E | `task e2e:deep-research` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| #301 and #860 crud-tools paths | `task e2e:crud-tools` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| #844 ops path | `task e2e:ops` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |

The research task runs and proves both isolated rounds in one invocation. The crud-tools task proves #301 and #860 in
one invocation, but the proof record identifies their distinct assertions. Any red retained path blocks tag
authorization. The documentation slice does not authorize a fix or allow wrapper output to be reclassified as green.

### Semantic active polling

| Poll UTC | `/readyz` result | Authoritative counters | Current stage/output timestamp | Progress judgment |
|---|---|---|---|---|
| `<required, repeated every 30-60 seconds>` | `<required>` | `<required>` | `<required>` | `<required>` |

If authoritative state shows no forward progress for more than twice the expected step duration, record abort evidence
and leave the semantic gate failed. Silence is not success.

### Review, CI, disposition, and tag authorization

| Required field | Detached value |
|---|---|
| Independent reviewer and result | `<required>` |
| Reviewed candidate SHA | `<required>` |
| Reviewed diff/artifact pointer | `<required>` |
| GitHub CI run/check identities and result | `<required>` |
| CI candidate SHA | `<required>` |
| Binding fresh-storage invariant | `Every downstream product adopting the stable release starts on newly provisioned NATS storage.` |
| Fresh-storage decision date | `2026-08-11` |
| Fresh-storage decision reference | `openspec/changes/post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise` |
| Binding release owner | `<required>` |
| Tag authorization and UTC time | `<required>` |

The candidate proof points to the owner decisions in `disposition-ledger.md` and records #301, #844, and #860 as
green. It also confirms that the product Release notes will disclose #839 and every deferred named program. It does
not inspect or predict future downstream storage, predict a product tag, identify unpublished artifacts, or preserve
a rejected candidate as a published proof Release.

## Post-publication release attestation

This separate immutable product-Release asset contains only facts knowable after tag authorization or publication.
Tag-specific migration guidance lives in the product GitHub Release notes and this attestation; the candidate tree is
not edited after proof.

| Required field | Release value |
|---|---|
| Candidate full SHA | `<required>` |
| Candidate-proof tag | `candidate-proof-<fullSHA>` |
| Candidate-proof asset pointer and external SHA-256 | `<required>` |
| Product tag name | `<required>` |
| Product tag-resolved SHA and command output | `<required>` |
| Binary version output and SHA-256 | `<required>` |
| Container reference, digest, and reported version | `<required>` |
| Fresh-storage premise included in product Release notes | `<required>` |
| Destructive storage operation performed during publication | `none` |
| Fresh-storage decision reference | `openspec/changes/post-g-tag-safety-closeout/design.md#decision-g-fresh-state-stable-release-premise` |
| Binding release owner | `<required>` |
| Final decision and UTC time | `<required>` |
| Exact published candidate/tag | `<required>` |
| Retained-path outcomes and accepted/deferred limitations | `<required>` |

Every downstream adoption begins on newly provisioned NATS storage. If retained deployed state is discovered during
adoption, only that adoption stops; it requires a separate owner-reviewed migration or recovery design and does not
retroactively redefine release publication.

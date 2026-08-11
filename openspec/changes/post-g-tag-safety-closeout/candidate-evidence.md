# Post-G tag-safety detached attestation schema

**Status:** In-tree schema/template only. This file is not candidate evidence and MUST NOT be completed with a
candidate SHA or run result.

**Evidence owner:** Release owner.

**Custodian:** Technical writer. Independent SemStreams review validates the exact candidate identity, diff, and
detached attestation.

## Authority and publication

The candidate commit cannot contain or predict its own Git SHA. Exact candidate identity and all command/run evidence
therefore live outside the candidate tree in an immutable GitHub Release attestation keyed by the candidate's full
commit SHA. The release owner creates that attestation only after the candidate commit exists.

The detached attestation SHALL:

- identify its full candidate SHA in both its release metadata and document body;
- use this file as its field schema without treating this template as evidence;
- remain immutable after approval; a correction creates a new candidate and a new attestation;
- link the in-tree disposition ledger and package manifest from the candidate tree; and
- fill every required field below before candidate freeze.

The Git object SHA is candidate identity. This template, its package-manifest digest, a branch name, a pull request,
or a workflow run MUST NOT redefine candidate identity.

The completed attestation MUST NOT contain or require its own SHA-256. GitHub Release metadata or a sibling checksum
asset created after the attestation upload MAY carry a digest. That external digest is verification metadata only; it
does not redefine candidate or attestation identity.

## Candidate identity

| Required field | Detached value |
|---|---|
| Candidate full SHA | `<required>` |
| Candidate commit URL | `<required>` |
| Clean `git status --short` | `<required>` |
| Accepted inventory SHA-256 | `8368e9b17e869561ca5c2123c8028d1311e449dae930c483d450c627a4acfcc6` |
| Package-manifest SHA-256 | `<required>` |
| Generated schema/spec diff | `<required>` |
| Disposition ledger commit path | `<required>` |

## Command and result provenance

| Gate | Exact command | Runner identity | Start UTC | End UTC | Exit/result | Log or artifact SHA-256 |
|---|---|---|---|---|---|---|
| Focused affected tests | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Lint | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Full race | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Integration | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Schema/no drift | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Contracts | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Strict OpenSpec | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Statistical E2E | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Semantic E2E | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Agentic E2E | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Research direct branch | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Research execute/fusion branch | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| Deep-research E2E | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| #301 crud-tools path | `task e2e:crud-tools` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| #844 ops path | `task e2e:ops` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |
| #860 rule/crud-tools path | `task e2e:crud-tools` | `<required>` | `<required>` | `<required>` | `<required>` | `<required>` |

Any red retained path blocks freeze. D does not authorize a fix or allow wrapper output to be reclassified as green.

## Semantic active polling

| Poll UTC | `/readyz` result | Authoritative counters | Current stage/output timestamp | Progress judgment |
|---|---|---|---|---|
| `<required, repeated every 30-60 seconds>` | `<required>` | `<required>` | `<required>` | `<required>` |

If authoritative state shows no forward progress for more than twice the expected step duration, the attestation
records abort evidence and leaves the semantic gate failed.

## Disposition outcomes and release limitations

The attestation SHALL point to the owner decisions in `disposition-ledger.md`, record the exact-candidate result for
each retained path, and reproduce every accepted or deferred limitation in the release notes. At minimum it records:

- #301, #844, and #860 as retained and green, or blocks freeze;
- #839 as an owner-accepted community-value limitation;
- the Derived-Index Convergence Program limitations;
- the Anomaly Lifecycle and Retention Program limitation;
- the Payload Bounds and Retention Program limitation; and
- the Semantic Summary Content/Quality Program limitation.

## Review and CI identity

| Required field | Detached value |
|---|---|
| Independent reviewer | `<required>` |
| Review result | `<required>` |
| Reviewed candidate SHA | `<required>` |
| Reviewed diff/artifact pointer | `<required>` |
| GitHub CI run/check identities | `<required>` |
| CI candidate SHA | `<required>` |
| CI result | `<required>` |

## Tag and artifact identity

| Required field | Detached value |
|---|---|
| Tag name | `<required>` |
| Tag-resolved SHA | `<required>` |
| Tag resolution command/output | `<required>` |
| Binary version output | `<required>` |
| Binary SHA-256 | `<required>` |
| Container reference | `<required>` |
| Container digest | `<required>` |
| Container-reported version | `<required>` |

## Coordinated #827 outcome

| Required field | Detached value |
|---|---|
| Operation owner | `<required>` |
| Scheduled tag boundary | `<required>` |
| Pre-v1 window open | `<required>` |
| Wipe/reseed result or migration halt | `<required>` |
| Evidence pointer | `<required>` |

## Release decision

| Required field | Detached value |
|---|---|
| Binding release owner | `<required>` |
| Decision and UTC time | `<required>` |
| Exact approved candidate/tag | `<required>` |
| Detached attestation asset URL | `<required>` |

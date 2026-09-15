# Governance proposal settlement review

## Evidence baseline

Claim HEAD: `4039530ff6213a25b946332c876b6d1e58c86a04`.
All artifacts below are local design work unless the PR explicitly identifies a published revision.
No runtime implementation, capability-delta promotion, rebase, or merge is approved by these verdicts.

## Inventory review — 2026-09-15

Artifact: `inventory.md`.
SHA-256: `b915b346fe72a30a2b229ff233d03001397ab990ffbac812779300923d8899cb`.
Root verification: `task inventory:verify -- openspec/changes/governance-proposal-settlement/inventory.md`;
105 pins, 105 exact, zero moved/drifted/malformed/unparsed.

Independent SemStreams reviewer verdict: **INVENTORY PASS**.
The inventory covers independently traced admission, reload, refusal, physical-input, decoding, and factory
boundaries. ADR-094 promises are distinguished from current behavior. Task 1.1 may be completed; this is not
target-design or test-coverage approval.

## First pre-owner design review — 2026-09-15

Artifact: `design.md`.
Reviewed SHA-256: `ea7346ab5e1c820256207bc2481d8ee53a36af7a367218eda8773d8aef81be65`.
Independent SemStreams reviewer verdict: **DESIGN CHANGES REQUESTED**.

1. HIGH, reviewed design line 30: options omit preserving documented publication-only `publish`/`deny`
   composition. That bounded form does not inherently require #935 atomicity. Compare it with the proposed
   single-policy, first-verdict contract before recommending retirement at reviewed lines 53–76.
2. HIGH, reviewed design line 142: mixed reload semantics are undefined. Entity rules may coexist, but
   `processor/rule/runtime_config.go:126–145` applies entries sequentially and may fail midway. Specify whether
   the proposal-policy update installs or remains unchanged when an unrelated entity-rule construction fails.
   No general rollback mechanism is required.

Both findings returned to the architect. No runtime implementation or owner acceptance is approved.

## Corrected pre-owner design review — 2026-09-15

Artifact: `design.md`.
SHA-256: `d561f666bdf6a5ec423140dd5968360b27b2cc3ed0b59e50bb214b98f637d186`.
Independent SemStreams reviewer verdict: **DESIGN REVIEW PASS**.

Both findings are resolved: bounded publish/approve/deny composition is preserved, and unrelated reload failure
explicitly retains the complete prior proposal portion, including cross-lane IDs. Settlement and migration wording
agree. Owner acceptance and approval of the stacked Git arrangement remain required. No implementation or merge
is authorized by this verdict.

## Decision-skill and scope checks

Root applied `semstreams-dev` and `orchestration-check`: the existing Rule processor evaluates conditions and
invokes its existing action owner. No workflow runtime, new durable state, communication primitive, or payload type
is introduced by the target. The inventory and independent review caused removal of the unnecessary proposed
single-policy/first-verdict language before owner review.

The scope ruling is https://github.com/C360Studio/semstreams/issues/1311#issuecomment-5666981667.

| Approved constraint | Reviewed target location |
| --- | --- |
| Publication-only work; no #935/general action retry | `design.md:26`, `design.md:148` |
| Reuse existing condition/action owners; no ledger or new retry runtime | `design.md:40`, `design.md:121` |
| Required publication before source completion | `design.md:93`, `design.md:133` |
| Preserve ordinary rule paths, projection no-replay, audit and optional notifications | `design.md:58`, `design.md:121`, `design.md:228` |
| Explicit integration, no copied primitives or frozen-parent changes | `design.md:193` |
| Separate design acceptance before implementation | `design.md:3`, task 1.4 |

At the pre-owner review, the dedicated physical proposal input, admitted action restrictions, reload semantics and
stacked Git sequence remained proposed, not additional rulings inferred from the scope approval.

## Owner acceptance — 2026-09-15

The owner answered "approved" to the corrected contract and stacked integration plan.
Ruling: https://github.com/C360Studio/semstreams/issues/1311#issuecomment-5676602813.
The accepted target is the reviewed `d561f666...` artifact, reproduced in full at
https://github.com/C360Studio/semstreams/pull/1312#issuecomment-5676303914.

Subsequent design edits update acceptance/hold status only; the reviewed mechanism is unchanged.
Task 1.4 is satisfied. Capability deltas may now be materialized and reviewed.
Implementation and restacking still require the exact reviewed #1159 prerequisite checkpoint to be published.
This acceptance does not establish R8 completion, waive gates, alter the frozen #1156 parent, or close any issue.

## Accepted delta review and documentation preflight — 2026-09-15

Independent SemStreams reviewer verdict: **PASS**.
Reviewed capability delta SHA-256:
`19348003a1a25c4ad27d141c8c7059cf3da88173fe6c7ca5f4a7d18e689783a0`.
Reviewed task text before recording completion of 2.1:
`a841971b5b21a018cd997765aea05b092b6c2bfa919fd784588c55631fcf67dd`.
Status-updated design SHA-256:
`1c2ffd60ee4d45a5a012db1dabe0c22fe4fccee065722bd49f23bad014cddc61`.

The delta matches the accepted design; invariant pins and recovery scenarios are correct. One review correction
made task 4.4 branch-checkable: actual landing and post-merge facts are uncheckboxed constraints, not child completion.
The generic-publish wire dependency remains explicitly held at task 2.2. Task 2.1 may be completed; implementation,
prerequisite completion, and merge are not approved by this review.

Preflight ran on claim HEAD `4039530f` plus this documentation-only snapshot; no runtime source changes:

| Command | Result |
| --- | --- |
| `openspec validate --all --strict` | PASS: 54 items, zero failures |
| `task spec:properties` | PASS: 101/101 citations resolve |
| `task inventory:verify -- openspec/changes/governance-proposal-settlement/inventory.md` | PASS: 105/105 exact pins |
| `task build:default lint` | PASS: binary, vet, fmt, pinned revive, port guard and request guard |
| `git diff --check` | PASS: no whitespace errors |

Build/lint used `GOCACHE=/private/tmp/semstreams-r7-test-cache`, `GOPROXY=off`, `GOSUMDB=off`,
and `GOFLAGS=-mod=readonly`. Request guard completed in 0.673s. This is documentation preflight, not runtime or E2E proof.
The queue remains held on the unpublished reviewed #1159 prerequisites. No rebase, integration, or issue closure
is claimed by this checkpoint.

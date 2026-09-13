---
name: tag-release
description: Prepare and publish an explicitly requested SemStreams release tag using the shared candidate-proof contract and an authorized exact version and commit. User-invoked only.
argument-hint: "[explicit version, if already chosen]"
disable-model-invocation: true
---

# Tag a SemStreams release

Read the [shared protocol](../../../.agents/protocol.md) and the full
[release-candidate proof contract](../../../openspec/specs/release-candidate-proof/spec.md) before proceeding.
They own milestone completion, candidate selection, immutable proof, authorization and publication. This
helper does not substitute a local preflight result or a version calculation for those gates.

## Select and prove the candidate

Follow the contract to select one clean, immutable merged-main candidate SHA after in-tree preparation.
Use an isolated candidate worktree for proof; do not switch the shared discovery checkout or another claim's
worktree. Resolve the candidate explicitly instead of assuming the current directory's HEAD is the release.
Preserve the contract's archived records and manifest; do not regenerate them to fit the candidate.

Use [semstreams-preflight](../../../.agents/skills/semstreams-preflight/SKILL.md) for local gate selection.
The pre-tag build-tag sweep includes `go vet -tags=integration ./...` and `go vet -tags=live_llm ./...`;
these compile/check tagged code and do not replace executed tests. Derive breaking-change coverage from the
actual release range and the contract's required paths. Registration migrations must trace every applicable
production and E2E binary through explicit registration; a grep hit alone is insufficient.

Complete exact-candidate proof and independent review as the release contract requires, then obtain any
still-missing owner authorization for the exact version and SHA. Preserve authorization already given.
Do not publish when a required gate is red, missing or bound to a different candidate.

## Publish and verify

Check that the authorized version is available locally and remotely. Create the annotated product tag on
the authorized SHA with subject `vX — <short summary>` and an accurate body. Verify the tag resolves to that
SHA before pushing that specific tag. Follow the separate publication/attestation phase in the release
contract; a successful push does not establish that binaries, containers and attestations are complete.

Never move a published version tag. If the version is wrong, prepare a new version through the same process.
Record release facts in the shared release artifacts and GitHub state. Private memory can retain a pointer;
it is not the release record.

Provide the SemStreams migration document with the release for affected adopters. Sister repository owners
perform their own changes and validation. Tagging does not grant authority to mutate their repositories or
close unrelated issues: apply the protocol's Close rule to each issue's actual merged-PR or owner record.

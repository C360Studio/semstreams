# Lifecycle worktree recovery manifest

This is the evidence-preserving cleanup authority for the eight dirty lifecycle worktrees found during the recovery
inventory. It records content; it does not approve any old implementation. The classifications below are binding for
cleanup, and the recovery ledger remains the sole implementation tracker.

## Fingerprint conventions

- **Tracked patch SHA-256** is the output of `git diff --binary | shasum -a 256` in the named worktree.
- **Untracked SHA-256** is the content hash of the named untracked file.
- **Classification fingerprint** is the read-only classifier's fingerprint over the classified workspace content.
- **DROP** means the hashes in this manifest are sufficient evidence; remove the exact worktree only after this
  manifest is merged.
- **EXTRACT, THEN DROP** means first preserve the stated bounded subset as a recoverable patch artifact or clean
  commit, verify its recorded subset hash, and only then remove the source worktree.

Do not copy a whole old patch forward. Preserved subsets must be reviewed against current `main` and receive no
lifecycle-migration credit until they land through the current recovery gates.

## Inventory summary

| Worktree | Classification | Dirty summary | Disposition |
|---|---|---:|---|
| `/private/tmp/semstreams-context-debt-cleanup` | SUPERSEDED | 0 tracked, 2 untracked | DROP |
| `/private/tmp/semstreams-context-race` | ACCEPTED-ALREADY-MERGED plus residue | 1 tracked | DROP residue |
| `/private/tmp/semstreams-generation-removal-1` | REJECTED | 1 tracked, 1 untracked | DROP |
| `/private/tmp/semstreams-lifecycle` | REJECTED | 5 tracked, 11 untracked | DROP |
| `/private/tmp/semstreams-p3-rule-config-context` | SUPERSEDED | 2 tracked, 1 untracked | DROP |
| `/private/tmp/semstreams-pr2-minimal-handles` | SUPERSEDED | 42 tracked | EXTRACT, THEN DROP |
| `/private/tmp/semstreams-pr2-owner-handles` | SUPERSEDED | 38 tracked | EXTRACT, THEN DROP |
| `/private/tmp/semstreams-restart-safe-shutdown` | SUPERSEDED/MIXED | 36 tracked, 6 untracked | EXTRACT, THEN DROP |

## Dirty worktree records

### `semstreams-context-debt-cleanup`

- Path: `/private/tmp/semstreams-context-debt-cleanup`
- Branch: `codex/context-debt-cleanup`
- HEAD: `19f446bf6840a43ab4e0ea1e4b70abd176291e84`
- Dirty state: 0 tracked files and 2 untracked files; no tracked diff stat.
- Tracked patch SHA-256: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
- Classification fingerprint: `264525f91cea987f79b4d1520399d2907cb3f0d00c24e331c660d6616bf16d48`
- Classification: **SUPERSEDED**. The inventory is historical and superseded; the phase 3 design is rejected.
- Disposition: **DROP** both files after this manifest is merged.

Untracked content:

- `openspec/changes/restore-go-lifecycle-ownership/phase3-design.md`
  - SHA-256: `cca10e7136939e53f81723d49c0f3249d7ed3891cb9a20d371d28b8f3e294521`
- `openspec/changes/restore-go-lifecycle-ownership/phase3-inventory.md`
  - SHA-256: `d96c52eb5b54152aec045cbae26bed04fa72c78e5b008fd2233398e825eb63d5`

### `semstreams-context-race`

- Path: `/private/tmp/semstreams-context-race`
- Branch: `codex/component-context-handoff`
- HEAD: `d6fe4e456cb719835d33fd56f5fbeb713f500b08`
- Dirty state: 1 tracked file; 190 insertions and 0 deletions.
- Changed file: `go.sum`
- Tracked patch SHA-256: `edad4ad0ced49abe1e30970abaa717ac9585d951edf7deb69d99f6d885901aee`
- Classification: **ACCEPTED-ALREADY-MERGED** for the substantive committed work. The dirty `go.sum` change is only
  residue and is not part of that accepted work.
- Disposition: **DROP** the dirty residue after this manifest is merged.

### `semstreams-generation-removal-1`

- Path: `/private/tmp/semstreams-generation-removal-1`
- Branch: `codex/remove-generation-leaf-owners`
- HEAD: `9fcc841ee792a080a7b9998bfb51400cd81b24fe`
- Dirty state: 1 tracked file and 1 untracked file; 108 insertions and 12 deletions in the tracked diff.
- Tracked file: `processor/graph-index/keyed_dispatcher.go`
- Untracked file: `processor/graph-index/keyed_dispatcher_lifecycle_test.go`
- Tracked patch SHA-256: `0ba989894c967bf3d262ef7721496f04a3f0e0dcf8885907c15d710cdd8247df`
- Untracked file SHA-256: `a46802aaa9074cc43c93f7a89366207e4d4920847512bf096bcdc33604aabec3`
- Classification: **REJECTED**. It adds bespoke admission/drain machinery to a leaf owner and preserves the parent
  cancellation mismatch that the recovery design removes.
- Disposition: **DROP** the entire experiment after this manifest is merged. Do not copy or expand it.

### `semstreams-lifecycle`

- Path: `/private/tmp/semstreams-lifecycle`
- Branch: `codex/component-generation-lifecycle`
- HEAD: `1002a0513a8aab3dd4d28faf4f0ee6c8d50a49ab`
- Dirty state: 5 tracked files and 11 untracked files; 1,123 insertions and 333 deletions in the tracked diff.
- Tracked patch SHA-256: `c09719a8fa144368910e4e29dd9294198b0b1c4f87313edb54b0db69d2c23686`
- Classification fingerprint: `c741de9c35f33a9958ce061bedda33d29e2c09e973fc472a8050defc13bd9a8b`
- Classification: **REJECTED**. It is the abandoned component-generation design and conflicts with boot-only
  component topology and ordinary owner-local lifecycle primitives.
- Disposition: **DROP** the entire experiment after this manifest is merged.

Tracked files:

```text
component/lifecycle.go
component/registry.go
service/base.go
service/component_manager.go
service/component_manager_start_barrier_test.go
```

Untracked files and content hashes:

- `component/registry_heartbeat_test.go`
  - SHA-256: `0a3ce0e4e62a14ba745441c56d6295095443a9dde5d52696d962ea9dfde82999`
- `docs/proposals/component-generation-lifecycle-design.md`
  - SHA-256: `4a2de1ee0ef2be26b6e7147026475dc2bde024602665e2dba01f49d38355aa42`
- `docs/proposals/component-generation-lifecycle-inventory.md`
  - SHA-256: `ec351f99a58e8b8fd0f16945ae2de7c61e240e00b53134c6fb19f2d02be164af`
- `openspec/changes/component-generation-lifecycle/design.md`
  - SHA-256: `32c7c372fa80ccbe0510740c1f6d2b7cddc7ad025c6c9b485d84a2fc67c783d9`
- `openspec/changes/component-generation-lifecycle/proposal.md`
  - SHA-256: `15e53958c5a9feea07ccffffca8b1992946b90d444006a3de69f34813324fa98`
- `openspec/changes/component-generation-lifecycle/specs/component-runtime-config/spec.md`
  - SHA-256: `d5a334a34a928030c7d08bb040103f134e9d31f2152481c952fc0da30e3848f0`
- `openspec/changes/component-generation-lifecycle/specs/framework-composition/spec.md`
  - SHA-256: `cdab077038f5e29500cedfe0f14a55f1a7c248f2e905166a369fca3527abcf89`
- `openspec/changes/component-generation-lifecycle/specs/service-shutdown/spec.md`
  - SHA-256: `dd182bd2c96c912e02ee6e9525a7917be3b99187043163caa433350a9cd96a91`
- `openspec/changes/component-generation-lifecycle/tasks.md`
  - SHA-256: `07031f7cdb8945b45fb0dd9c526d0e65c8bbfc74807208661017bd8433c3cb38`
- `service/lifecycle_generation.go`
  - SHA-256: `3d77008d575e2c427f6a8602abc5daeb8301b4e3068307201f477dbc8d540177`
- `service/lifecycle_generation_test.go`
  - SHA-256: `fd0d6f59eb6dca614a79548be1aed9e4b27eb6dfcc7a93606a17fd1988b74255`

### `semstreams-p3-rule-config-context`

- Path: `/private/tmp/semstreams-p3-rule-config-context`
- Branch: `codex/p3-rule-config-context`
- HEAD: `9d0ff67f377ea3dd82dca2f3bf614871c0100766`
- Dirty state: 2 tracked files and 1 untracked file; 235 insertions and 13 deletions in the tracked diff.
- Tracked patch SHA-256: `543dbf05ee38b2f002bb154727759ea94e7645e5f10589d1837b4d95a1f1cfab`
- Classification fingerprint: `2e65f0ecc6dd6c6d07f49ebb66e98d14100ea4d771a7b65a6a65e8e6560c8650`
- Classification: **SUPERSEDED**. The `ConfigManager` portion is partial and superseded; the `go.sum` residue and
  concurrent-Stop test preserve rejected semantics.
- Disposition: **DROP** the entire patch after this manifest is merged.

Content fingerprints:

- `go.sum` (tracked)
  - SHA-256: `249602502ca1ced81a530438343330f6e4e1f3203f59247e74351a54256c7d03`
- `processor/rule/kv_config_integration.go` (tracked)
  - SHA-256: `b31a3c769eeed6cb29869714ad764ab1717e107ea7812e9c0c20778b3e328f1a`
- `processor/rule/kv_config_lifecycle_test.go` (untracked)
  - SHA-256: `2120001acb3d9ff82f1ecf676a2639e043bdf69cccc3b8a86c9e1a3b59a27d27`

### `semstreams-pr2-minimal-handles`

- Path: `/private/tmp/semstreams-pr2-minimal-handles`
- Branch: `codex/pr2-minimal-handles`
- HEAD: `991c96bb517f74350cfabce3d55fed7c130b8833`
- Dirty state: 42 tracked files; 538 insertions and 476 deletions.
- Tracked patch SHA-256: `714830cdb029cb5b2c5bccf9a125a3bdd0cb08d80a34dbc6a51cc4e4e8b7abe0`
- Classification: **SUPERSEDED**. The broad owner-handle prototype belongs to the rejected framework-like plan.
- Disposition: **EXTRACT, THEN DROP**. Preserve only the exact three-file `ConsumeDurable` deletion recorded under
  [bounded preservation](#bounded-preservation); all other content is hash-only and must not carry forward.

Changed files:

```text
agentic/agentrun/agentrun.go
component/registry.go
component/registry_integration_test.go
examples/processors/document/component.go
examples/processors/iot_sensor/component.go
internal/maxdelivery/observer.go
natsclient/client.go
natsclient/client_integration_test.go
natsclient/client_test.go
natsclient/consume_durable.go (deleted)
natsclient/consume_durable_integration_test.go (deleted)
natsclient/consume_durable_test.go (deleted)
natsclient/consumer_handle_integration_test.go
natsclient/consumer_policy_callsite_test.go
natsclient/consumer_policy_integration_test.go
natsclient/consumer_policy_test.go
natsclient/consumer_stop_test.go
natsclient/integration_test.go
natsclient/publish_async_integration_test.go
natsclient/stream.go
natsclient/stream_integration_test.go
output/file/file.go
output/httppost/httppost.go
output/websocket/websocket.go
processor/agentic-dispatch/component.go
processor/agentic-dispatch/terminal_settlement_integration_test.go
processor/agentic-governance/component.go
processor/agentic-loop/component.go
processor/agentic-loop/inflight.go
processor/agentic-loop/inflight_test.go
processor/agentic-model/component.go
processor/agentic-tools/component.go
processor/graph-ingest/component.go
processor/graph-ingest/readiness.go
processor/graph-ingest/readiness_test.go
processor/json_filter/json_filter.go
processor/json_generic/json_generic.go
processor/json_map/json_map.go
processor/rule/processor.go
service/flow_runtime_stream.go
storage/objectstore/component.go
test/testinfra/policy_baseline.json
```

### `semstreams-pr2-owner-handles`

- Path: `/private/tmp/semstreams-pr2-owner-handles`
- Branch: `codex/pr2-owner-handles`
- HEAD: `63a733a2378dff9f09c74c461ba776d352f79221`
- Dirty state: 38 tracked files; 625 insertions and 465 deletions.
- Tracked patch SHA-256: `542b08deaa36074afe3e1f583235a23cf5fcd747e1f2010cbb2267691b009b71`
- Classification: **SUPERSEDED**. This is the broader predecessor of the rejected owner-handle prototype.
- Disposition: **EXTRACT, THEN DROP**. It contains the same exact three-file `ConsumeDurable` deletion recorded
  under [bounded preservation](#bounded-preservation); all other content is hash-only and must not carry forward.

Changed files:

```text
agentic/agentrun/agentrun.go
component/registry.go
component/registry_integration_test.go
examples/processors/document/component.go
examples/processors/iot_sensor/component.go
internal/maxdelivery/observer.go
natsclient/client.go
natsclient/consume_durable.go (deleted)
natsclient/consume_durable_integration_test.go (deleted)
natsclient/consume_durable_test.go (deleted)
natsclient/consumer_handle_integration_test.go
natsclient/consumer_policy_callsite_test.go
natsclient/consumer_policy_integration_test.go
natsclient/consumer_policy_test.go
natsclient/consumer_stop_test.go
natsclient/stream.go
natsclient/stream_integration_test.go
natsclient/subscription_test.go
output/file/file.go
output/httppost/httppost.go
output/websocket/websocket.go
processor/agentic-dispatch/component.go
processor/agentic-dispatch/terminal_settlement_integration_test.go
processor/agentic-governance/component.go
processor/agentic-loop/component.go
processor/agentic-loop/inflight.go
processor/agentic-loop/inflight_test.go
processor/agentic-model/component.go
processor/agentic-tools/component.go
processor/graph-ingest/component.go
processor/graph-ingest/readiness.go
processor/graph-ingest/readiness_test.go
processor/json_filter/json_filter.go
processor/json_generic/json_generic.go
processor/json_map/json_map.go
processor/rule/processor.go
service/flow_runtime_stream.go
storage/objectstore/component.go
```

### `semstreams-restart-safe-shutdown`

- Path: `/private/tmp/semstreams-restart-safe-shutdown`
- Branch: `codex/rejoinable-nats-close`
- HEAD: `991c96bb517f74350cfabce3d55fed7c130b8833`
- Dirty state: 36 tracked files and 6 untracked files; 2,068 insertions and 1,068 deletions in the tracked diff.
- Tracked patch SHA-256: `832f435e2a2bbe8ab44b92b69a139d6dc4fedd920e874a1f45c52520f9cb6503`
- Classification: **SUPERSEDED/MIXED**. The rejoinable client and shutdown-state machinery is rejected, but twelve
  exact files contain independently useful cleanup or test changes.
- Disposition: **EXTRACT, THEN DROP**. Preserve only the exact twelve-file subset under
  [bounded preservation](#bounded-preservation); all other tracked and untracked content is hash-only.

Tracked files:

```text
docs/README.md
input/websocket/websocket_input_integration_test.go
natsclient/README.md
natsclient/client.go
natsclient/client_async_error_test.go
natsclient/client_close_test.go (deleted)
natsclient/client_test.go
natsclient/consumer_policy.go
natsclient/consumer_policy_test.go
natsclient/consumer_stop_test.go
natsclient/doc.go
natsclient/heartbeat_integration_test.go
natsclient/integration_test.go
natsclient/jetstream_metrics.go
natsclient/options.go
natsclient/request.go
natsclient/request_response_bounds_integration_test.go
natsclient/storage_inventory.go
natsclient/stream.go
natsclient/stream_integration_test.go
natsclient/subscription_test.go
openspec/changes/require-restart-for-config-activation/design.md
openspec/changes/require-restart-for-config-activation/inventory.md
openspec/changes/require-restart-for-config-activation/proposal.md
openspec/changes/require-restart-for-config-activation/specs/restart-safe-shutdown/spec.md
openspec/changes/require-restart-for-config-activation/tasks.md
output/websocket/websocket_integration_test.go
pkg/logging/doc.go
processor/agentic-tools/outcomes_integration_test.go
processor/agentic-tools/startup_atomic_integration_test.go
processor/graph-ingest/README.md
processor/graph-ingest/component.go
processor/graph-ingest/component_test.go
processor/graph-ingest/doc.go
service/doc.go
test/testinfra/policy_baseline.json
```

Untracked files and content hashes:

- `docs/operations/migration-restart-safe-nats-client.md`
  - SHA-256: `14ab2df747843e1befc2d2957978edabf14dd07ae49f511cfed9b30a14cf4842`
- `natsclient/client_close_lifecycle_integration_test.go`
  - SHA-256: `1cbfb33c05580e7b15879d06c63d6164deb294757ad4c3260af28ca31188df77`
- `natsclient/client_generation_test.go`
  - SHA-256: `325183765f7d799bfa628d2852b80b45a173b5a922c6177e3a470da6c4962dbe`
- `natsclient/close_lifecycle_test.go`
  - SHA-256: `a7bd9401d4e984a261ed4c6bf657ef16cc869a39d722725c038b9c8fe273e9a0`
- `natsclient/consumer_delete_lifecycle_test.go`
  - SHA-256: `391bf77d848f8ad0610f2c90661f2382dd10b69bed637785fff40f4bb93f3e24`
- `natsclient/lifecycle.go`
  - SHA-256: `dd702bcdf2622b18cfc542129b50d3b1a79184a236154361c0d5149a54c6435a`

## Bounded preservation

These two subsets are the only dirty-worktree content approved for preservation beyond the hashes above. They must
be extracted as bounded recoverable patch artifacts or clean commits before any source worktree containing them is
removed. Extraction preserves evidence; it does not approve the subset for merge.

### Restart-safe twelve-file subset

Expected subset patch SHA-256:
`67cc40f4465f5127793693a1423479a6b24df2b0607f4050d1e28f5e1a42f6b4`.

```text
natsclient/client_async_error_test.go
natsclient/client_close_test.go (deleted)
natsclient/client_test.go
natsclient/integration_test.go
natsclient/options.go
natsclient/stream_integration_test.go
processor/agentic-tools/startup_atomic_integration_test.go
processor/graph-ingest/README.md
processor/graph-ingest/component.go
processor/graph-ingest/component_test.go
processor/graph-ingest/doc.go
test/testinfra/policy_baseline.json
```

### `ConsumeDurable` three-file deletion

Expected subset patch SHA-256:
`ddcee4f8643d4992485f5e367ef4c0e203167e03901705c85cffdba4ce98782b`.

```text
natsclient/consume_durable.go
natsclient/consume_durable_integration_test.go
natsclient/consume_durable_test.go
```

The deletion appears identically in both `semstreams-pr2-minimal-handles` and `semstreams-pr2-owner-handles`.
Preserve one verified artifact; do not duplicate it.

## Kept and excluded worktrees

Draft PR #990 is explicitly **KEEP** and outside cleanup:

- Path: `/private/tmp/semstreams-gh986-boot-only-flow-activation`
- Branch: `codex/gh986-boot-only-flow-activation`
- HEAD: `8f19ef3678a549913385b090e4de1766a7a43a27`
- State: clean
- Scope: boot-only flow activation; zero lifecycle-migration credit

The following user-owned or unrelated worktrees are explicitly outside this manifest and deletion scope:

- `/Users/coby/Code/c360/semstreams-wt-741`
- `/Users/coby/Code/c360/semstreams/.claude/worktrees/*`

Do not remove an unlisted worktree by inference. Cleanup uses only exact paths from this manifest, does not touch
Docker resources, and begins only after this manifest is durable and both bounded artifacts are verified.

## Cleanup result

Cleanup completed on 2026-08-18 after this manifest became durable through PR #992 at merge commit
`8961357a4fdc8286e2b2d66b97e85359e39b81b3`.

The two bounded preservation artifacts are clean and pushed:

- `codex/recovery-artifact-restart-safe-prereqs` at
  `92d59b0a2ef9db7ff4118a61e8aa48b1470c4e21` preserves the exact twelve-file subset. Its verified subset
  SHA-256 is `67cc40f4465f5127793693a1423479a6b24df2b0607f4050d1e28f5e1a42f6b4`.
- `codex/recovery-artifact-consume-durable-retirement` at
  `fea02a42c4cbe67089d900c6712fb52c8648ed1a` preserves the exact `ConsumeDurable` three-file deletion. Its
  verified subset SHA-256 is `ddcee4f8643d4992485f5e367ef4c0e203167e03901705c85cffdba4ce98782b`.

Both temporary artifact worktrees were removed cleanly. The following eight manifested dirty worktrees were each
removed with exit status 0:

```text
/private/tmp/semstreams-context-debt-cleanup
/private/tmp/semstreams-context-race
/private/tmp/semstreams-generation-removal-1
/private/tmp/semstreams-lifecycle
/private/tmp/semstreams-p3-rule-config-context
/private/tmp/semstreams-pr2-minimal-handles
/private/tmp/semstreams-pr2-owner-handles
/private/tmp/semstreams-restart-safe-shutdown
```

Post-cleanup verification found `main` clean at `8961357a4fdc8286e2b2d66b97e85359e39b81b3`. Draft PR #990 remains
clean and unchanged at `8f19ef3678a549913385b090e4de1766a7a43a27` on
`codex/gh986-boot-only-flow-activation`. User-owned and unlisted exclusions were untouched.

The recovery artifact commits are preservation only. They carry zero lifecycle or runtime completion credit and
require review against current `main` before any content may land.

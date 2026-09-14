# Rescue pause checkpoint — 2026-09-12

Owner requested a pause for reboot after the first reduced implementation checkpoint. Do not resume automatically.

## Location and authority

- Claimed worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`.
- Branch: `codex/gh1146-agentic-loop-restart`; draft PR #1159 targets #1156's non-default branch.
- Local HEAD: `fd9dac345bf2099d634f4f30b372cb0a44730882` (prior documentation commit).
- Remote runtime HEAD: `160ad091de057beddafb06367832a95b21f683f7`.
- Frozen parent: `417beae5552f8f15ad3540edd7d8504c87174c13`.
- No commit, push, merge, issue closure or staged-parent change occurred in this rescue turn.
- Owner scope reduction: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5643508407.
- Pre-reduction exact source/index backup: `/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWgc/`.
  Its source copy is a backup, not a Git worktree; do not run Git mutations from it.

The current proposal/design/tasks/dispatch delta are authoritative. The research-rendering proposal is withdrawn,
not awaiting implementation. Scope reduction passed design review; the smaller decoder-preservation amendment is
`7947ae7a6310c69105c7842a20658565b4cdeb33ca2f1b05929388b981606c25`.

## Implemented reduction, still unpublished

- Keep tracker/created/pending-consumer removal and validated KV authority.
- Remove the optional SearchResult rendering branch and the two production research imports.
- Preserve ordinary private/raw AND registered completion decoding, validation, identity checks and mappings.
  The ordinary registered decode/marshal/projection sequence is retained, not claimed fixed.
- Canonical LoopID values remain validated current authority; corruption poisons until healing. Other non-completion
  keys are outside that authority. Completion-only poison remains observable activity error, not current state.
- Nine research prefix-consolidation files now have zero diff against HEAD. No replacement interface/package exists.
- Native stream terminal decoding, exact KV approval admission, required PubAcks, loop approval-restart test and
  all other underlying settlement owners are unchanged by this rescue delta.
- The preexisting schema staging is preserved; OpenAPI has the additional unstaged activity-description correction.

## Verification at pause

Root reproduced core dependency closure RED before edits: forbidden `/agentic/research` import.
Developer subsequently reported these commands GREEN:

- `go test ./test/contract/... -run '^TestCoreCompositionDependencyClosure$' -count=1` (1.093s).
- Focused dispatch projection/activity/readiness/lifecycle tests with `-race` (1.808s).
- Native `TestIntegrationMixedLoopBucketSharesOneCurrentView` with `-race` (4.131s).
- Native `TestIntegrationApprovalRequiredResultRetriesMatchingPendingPrompt` with `-race` (32.538s).
- Final focused decoder rerun after adding registered-nonterminal/raw-invalid controls (1.456s).

Root ran `openspec validate --all --strict`: 55 passed, 0 failed; `git diff --check` passed.
No full new-candidate unit/integration suite, full pre-push gate or E2E was run. These focused greens are not R1
completion, whole-PR approval, permission to archive, or permission to merge.

## Review and next action

The independent reviewer inspected the 16-file rescue delta against the backup, including untracked tests, and found
no production defect. It verified the other 45 manifest entries/critical owners unchanged. It had not reviewed the
final test revision or the full underlying unpublished dispatcher implementation before the pause.

One fixture finding remains to reconcile on resume: malformed `{"type":"other.result","payload":{}}` data does not
prove a VALID registered unsupported payload is rejected as activity-only poison. The developer had already added a
registered LoopCreatedEvent unit control before the pause, and its rerun passed. The native fixture still needs the
reviewer's bounded check/correction. Do not treat the whole finding as resolved from that unit test alone.

First resume action: inspect these exact tests and finish that bounded review/proof. Then resume the existing R1 and
combined gate work; do not reopen the research interface/package proposal or restore the tracker.
Approval-Store's evidence-dependent owner ruling, the 15 ACK duties, and #1156/#1249 atomic main landing are unchanged.

## Source identity and process state

- Developer's tracked slice diff SHA-256: `bba563f0ed66a85c13134bfdbe20e269a8148059f132d43acd675990a48ab150`.
  Scope: http_activity.go, loop_wire.go, http.go, loop_token_test.go, specs/openapi.v3.yaml; relative to HEAD.
- Untracked loop_projection_test.go: `873776d96d52d630156f1575de1260b53a676a8ea824c37a2cb0f481a56899be`.
- Untracked loop_projection_integration_test.go:
  `c73b8f43d65fffa8562ca3d91e377ff37b5426e4e9f6df31c96b72c1572b5f84`.

Developer reports all sessions exited 0 and none active. Root checked Docker: no running containers. An escalated
process listing showed no active Go tests or check:push command (only the inspection command itself).
No paid operation or background automation was started. All agents were instructed to pause.

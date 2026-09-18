# R8 attachment-retirement implementation checkpoints

## Scope and authority

Local dirty candidate on `68c14c8eb25c512e988f740cbf7ea14b6815976f`, PR #1159,
base `codex/gh759-semantic-settlement`. Owner ruling `5728438234`; reviewed mechanical lowering
`design-r8-attachment-retirement-2026-09-18.md`, SHA-256 `08741a60000d0549a5a2259ee20e495143cf55d3e81e9db893531cdc267b6f04`.
The first checkpoint below covers input/config retirement; subsequent sections record raw-loop and E2E work.
None completes R8 or the PR. Nothing is committed or pushed.

The developer removed AutoContinue, submission ReplyTo, inferred command targets, activeLoop and continuation-only
dispatch admission. Existing config resolution, USER validation/negative response, and HTTP refusal own rejection.
Known retired JSON keys refuse by presence, including false/null/empty/folded/escaped/duplicate spellings. Unrelated
unknown fields retain their prior behavior. The only new production private field is nonserialized
`UserMessage.retiredReplyTo`; it holds presence, not an execution target, and resets on each decode.

Nine shipped configurations and both generated artifacts remove the public settings. Explicit commands, permissions,
lineage, ordered displayed history, retained-task validation and the two retained-publication guards remain.
Root owns the package/concept/migration documentation; developer changes include no loop or rule runtime edits.

## Conformance map

| Accepted obligation | Candidate evidence |
| --- | --- |
| Refuse config key before allocation; declaration and construction agree | `component.go` resolveConfig; constructor/DeclarePorts table |
| Refuse HTTP key before commands, lookup or publication | `http.go` handleHTTPMessage; HTTP no-effects table |
| Decode registered USER, refuse before branching, negative response before Term; failed response retries | `user_types.go` UnmarshalJSON/Validate; installed USER callback test |
| Each new dispatch task gets fresh identity; retained tasks are reused | `task_recovery.go`, both submission paths; native identity/replay controls |
| Preserve ordinary chat after replacement | Existing sequential-chat production-component integration |
| No new payload, store, identity mode, TTL or compatibility acceptance | Production diff is deletion plus existing-boundary refusal checks |

Paths above are in `processor/agentic-dispatch/` except `agentic/user_types.go`.

## Executed evidence

Developer logs: `/private/tmp/gh1146-retired-inputs.KB9kJI`. These are ephemeral supporting logs; the commands,
meaningful outputs and candidate hashes below preserve the checkpoint in-tree.

- Intended compiled TDD RED: `red-assertions.log`, dispatch package FAIL 0.532s. Config construction returned nil
  error for retired presence; HTTP returned 200 instead of 400; registered USER returned text instead of error and
  missed the required retry. The earlier fixture nil-NATS panic in `red.log` is not behavioral RED evidence.
- `go test -race ./agentic ./processor/agentic-dispatch`: PASS, 4.932s and 2.167s respectively
  (`final-package-race.log`), including fixed fuzz seeds. An intermediate adapted explicit-control fixture failed;
  its correction preceded this final run.
- `go test -race ./processor/agentic-dispatch -run '^$' -fuzz '^FuzzRetiredTargetJSON$' -fuzztime=5s -parallel=1`:
  PASS, 1,452 executions, eight fixed seeds and 14 new interesting inputs, 8.565s (`fuzz.log`).
- `task schema:generate`: PASS (`schema.log`); generated changes only remove dispatch auto_continue and HTTP
  reply_to and reconcile the prior_messages description.
- `go vet -tags=integration ./agentic ./processor/agentic-dispatch`: PASS (`tagged-vet-corrected.log`). The earlier
  compile-only failure was a now-invalid `:=` in an adapted fixture; it was fixed before this pass.
- `go tool revive -config revive.toml -formatter friendly ./agentic/... ./processor/agentic-dispatch/...`: PASS
  (`lint.log`). `git diff --check`: PASS. No go.mod/go.sum change.
- Three compiled mutations independently disabled the USER Validate, config presence, and HTTP presence guards.
  All failed their intended assertions: error versus text, required constructor error, and HTTP 400 versus 200.
  Baseline and restored selections passed. Root independently compared the five current/source-backup SHA-256
  values below; all match. No mutation remains. Logs: `mutation-{baseline,user,config,http,restored}.log`.

Root ran the existing host-locked integration runner, not a direct tagged test:

```sh
scripts/run-integration-tests.sh ./processor/agentic-dispatch \
  -run '^(TestIntegrationSequentialChatAfterComponentReplacement|TestIntegrationUserMessageReplayAfterTaskCommitKeepsOneLogicalTask|TestIntegrationUserMessageTaskMappingConflictQuarantines|TestIntegrationDispatchTaskPublicationPreservesFreshMintAndLineage)$' -v
```

The sandboxed attempt stopped at Docker socket permission, before tests. The authorized retry completed with exit 0
on the frozen candidate (2026-09-18, local 13:09). Four selected top-level tests ran, with eight nested identity
cases. The replay output observed one source sequence delivered twice with the same retained TaskID and LoopID.
Chat assertions exercise displayed user/assistant history exactly once, distinct execution IDs, fresh budget,
component stop/replacement and exactly two fake-provider calls. This is not OS-process replacement or graph/audit
proof: the fixture deliberately omits the graph identity/store and emits corresponding diagnostics. It uses no paid
model. Deployed E2E and the combined full gates remain outstanding.

## Candidate fingerprints

SHA-256; dispatch paths are relative to `processor/agentic-dispatch/`.

```text
0fca85f9108147c1277334069b35a45dccd5c11145462ed25cfdece975e841b6  agentic/user_types.go
42138a37edb20a83f02f2fc0560caad736ab9477b2878e9ad1be867924ed27d6  agentic/rule_fields.go
0dba5ac8d69deae456977468f7b360bad6ad3ab97266c99f7e409298f94633d6  commands.go
076d8344a327cc8d5e47c44d6f8c6ce526facaf6ca87a5fabacf6dcc8c5d44ef  component.go
de11d654057d60eaee1c75c0a2d37e1db393cba2b37e0b701f915a49d9423591  config.go
199a02b3b8ab77094f1c6169e2a44a4e39cfbefb9cc82dd8fd2da2859f7f0a80  http.go
2120e3f7ecf7fced989aab4c3bda0b2a6ba2812a4ddc7f563b53fb4812be0fa9  http_activity.go
7637a5f1f5bba4c36717fddd07e1344bec6cc86e4fd0a6e4289f4a1169faca38  loop_admission.go
088f34d2325d8a7d8d67926ee040b1761c08525af2d8bed090e4710600a84de3  metrics.go
da70666abedb7fc2c51dd6e0d72c143386d4bda4ccc2be723e6c0d9b9561f7d6  task_recovery.go
11eca479182228a96949ec114260f5feb5a9a2a1ea96f066ae080bc8abc0732b  agentic/user_retired_input_test.go
3b5648baa86120329c4423aff9ace2f1fabb8d3d7d4f47f4d003e810162e5cfd  retired_inputs_test.go
6b29339a42c314d1bfba2d527a0195faa3b83bfee260f81009e3e30f051e8bbd  sequential_chat_integration_test.go
7b498481c4fe11d84ffd0b3cc30faac23551a4c537c03a767bba5b31a323c199  restart_identity_integration_test.go
```

## Remaining gates

Independent reviewer returns **APPROVE for this frozen input/config slice only**, with no blocking/high findings.
The reviewer verified all 14 candidate hashes, five mutation restorations, intended REDs, three killed mutants,
race/fuzz evidence and configuration/schema/documentation conformance. The native run is root-observed: the tool
reported exit 0, but its combined output truncated the final tail, so no complete native log was independently
reviewed. No repeat run was started just to recover that log; this limitation does not expand the bounded verdict.

Raw-loop rebind removal, its two obsolete spec-citation adaptations and
ownership mutation proof follow separately. Preserve the missing-request RED at
`processor/agentic-loop/settlement_recovery_test.go` SHA-256
`774ad585d2b173351a33ca820cb346fac0f86abaecd81b36d4171f82532b80d0` and the unrelated rule publisher WIP.
Retirement's E2E extension/run, finite supported-retention proofs and R11 remain open. No stack or landing hold changed.

## Raw-loop retirement candidate

Subsequent frozen candidate, same HEAD, 2026-09-18. The input checkpoint above is unchanged. The developer removes
`attachContinuation`, attachment-only `ErrLoopBusy`/`ErrLoopTerminal`, and their special intake handling. Existing
`CreateLoopWithID` still validates form before checking existence and never overwrites existing state. HandleTask
wraps its existing collision in the existing fatal correlation error; durable intake maps it to Quarantine.
The three production files have 18 added and 205 removed lines. There is no new production symbol.
Root adds only the corresponding removed-sentinel migration note; it changes no accepted behavior.

### Proof and limits

The new `TestTaskIntakeCannotRebindKnownLoop` uses existing registered-wire, creation-fence, retained-evidence,
heartbeat/delivery-owner and settlement-spy fixtures. Its 27 cases cover warm partial birth, warm retained authority,
cold retained authority, the five states, same-task recovery and different-task conflict. It explicitly excludes
same-task local terminal without durable authority as unsupported proof, rather than claiming every imagined state
is reachable. Pending-tool and approval cases adapt existing tests; old attachment assertions are removed, not renamed
to claim unchanged behavior. Initial/tool/cold PriorMessages and form-first/create controls remain.

Developer logs/backups: `/private/tmp/gh1146-loop-retirement.Wovz3Y`.

- Intended compiled RED (`red-assertions.log`) observed task B ACKed, A's TaskID rebound, B's prompt appended,
  extra model request generated and authority overwritten during warm partial birth. Other collision states failed
  the required fatal/Quarantine classification. The earlier fixture compile error in `red.log` is not RED evidence.
- One compiled rebind mutant was detected by the fatal/Quarantine and unchanged-TaskID assertions. Baseline PASS
  1.555s; mutant FAIL 0.502s; cp-restored PASS 1.404s. All six current files match their `mutation.nl48yy` backups;
  root independently compared the checksums. Logs: `mutation-{baseline,rebind,restored}.log`.
- Final focused race selection PASS 1.580s (`final-focused.log`). Exact commands:

```sh
go test -race -count=1 ./processor/agentic-loop -run '^TestTaskIntakeCannotRebindKnownLoop$'
go test -race -count=1 -v ./processor/agentic-loop -run '^TestTaskIntakeCannotRebindKnownLoop$'
go test -race -count=1 -v ./processor/agentic-loop -run '^Test(TaskIntakeCannotRebindKnownLoop|DifferentTask|SameTaskRedelivery|CreateLoopWithID|FormRefusal|PriorMessages|TaskPersistence|TaskAssembly|DirectHandleTask|TaskDeliveryQuarantines|Approval|ColdTaskRedelivery(ReusesRetainedRequest|ConflictingMappingQuarantines|WithoutRequestRebuildsFromTaskAndPreservesLoop|WithTerminalAuthorityCreatesNoRequest))'
go vet -tags=integration ./processor/agentic-loop
go tool revive -config revive.toml -formatter friendly ./processor/agentic-loop/...
```

Tagged vet and scoped revive PASS (`tagged-vet.log`, `revive.log`). Native fuzz does not apply to this raw-loop
subtraction: no parsing or exported boundary is added; deterministic history cases exercise the finite ownership
distinction. Input grammar fuzz proof remains in the preceding checkpoint. No concurrency or expiry proof is claimed.

Root's unrestricted package selection, `go test -race -json ./processor/agentic-loop`, returns exit 1, 2.900s.
There are 574 top-level PASS and 750 nested PASS records, no SKIPs, and exactly one failed test:
`TestColdTaskRedeliveryWithProgressAndMissingRequestRefusesInitialRebuild`. It still observes progressed Iterations 2
rebuilding an Iteration 1 request instead of refusing absent required evidence. The preserved test file's SHA remains
`774ad585d2b173351a33ca820cb346fac0f86abaecd81b36d4171f82532b80d0`.
Full JSON evidence: `/private/tmp/gh1146-retirement-native.Eb6SfN/loop-full-race.jsonl`.
This is **not** a full-loop green result or a waiver of that failure.

`task spec:properties` now passes 386/386; `git diff --check` passes. No full push gate or deployed E2E run belongs
to this candidate. Independent reviewer returns **APPROVE for the raw-loop retirement slice only**, no blocking/high
findings. The reviewer verified all six candidate/backup hashes, the 27-case matrix, compiled mutant and restoration,
focused results and the complete root full-loop JSON failure list. Removed sentinels have no remaining Go references;
the added migration note is accurate. Missing-history, supported-retention and final gates stay open.

### Raw-loop fingerprints

SHA-256; paths relative to `processor/agentic-loop/`:

```text
4c91ebf2069d38b37a4937263b0479590d25a6fd5cafe4e830da00628d30be80  state.go
6a6962bd1b15317a0bdc31036858117a467489d4cbd2556536d4990aa4d602cd  handlers.go
35384cf22c556d937092e433c30f129f0e1d4519b31b213274ae6f042466d1b0  component.go
fdfad5c657f01f54d771691ce65553ea40041308ee5aea265e75f5fd0784f93a  create_vs_exists_fence_test.go
615a2b7391ccc4860cb4eb7447012245d893b79798f26a91de717dee17ff7eea  prior_messages_test.go
042da23fc1115ce041163d1ea0ae3c62466b344b659255c431c8d00b370f3c93  attachment_retirement_test.go
```

## E2E retirement candidate

Same dirty HEAD, 2026-09-18. Exactly five scenario/test files extend the existing agentic tier; no new stack,
mock behavior, production mechanism, schema or dependency accompanies this slice. The two new asserting stages
follow the existing counter-sensitive walks: `walk-independent-chat-turns` and `refuse-retired-target-inputs`.
The tier now declares 20 stages, 18 of them asserting, and final validation requires all eight new observations.

The chat walk submits two HTTP messages on the same route with distinct LoopIDs. It observes the registered,
correlated, routed terminal UserResponse; only that displayed assistant content enters the second submission's
PriorMessages. The retained registered AgentRequest must contain exactly the displayed user/assistant exchange
followed by the second user message, after framework-owned leading system messages. This tests transcript transport,
not model answer quality or retained tool/system context.

The refusal walk requires HTTP 400 naming `reply_to` and a registered USER error for folded `RePlY_To: null`.
It brackets both refusals with the retained `agent.task.>` count and waits for the exact USER source delivery and
consumer settlement before checking that no task was added. This is a sequential-tier assertion, not a concurrent
traffic proof. Existing process-replacement and approval stages remain in the same tier.

### Review and focused evidence

Logs/backups: `/private/tmp/gh1146-e2e-retirement.5akoje`.

- `red-stage-evidence.log`: compiled intended RED for the absent two stages and eight required observations.
- Two compiled mutations were detected: altered second-turn history forwarding (`mutant-history.log`) and disabled
  no-task comparison (`mutant-task.log`). Restored race selection passes (`restored.log`); no mutation remains.
- Initial review requested one correction: dispatch can commit a terminal result before its submission status,
  so a latest-only USER read can hide the result. The correction uses existing `GetMsg` plus `WithGetMsgSubject`
  and a local advancing sequence cursor. No production ordering or polling owner changes.
- `review-order-red-assertion.log`: the result-before-status fixture fails with `no displayed result` before
  correction. `review-order-red.log` is only a fixture validation failure and is not meaningful RED evidence.
- `go test -race ./test/e2e/scenarios/agentic -run '^TestChatTurnReadsRetainedResultBeforeLaterSubmissionStatus$' -count=1`:
  PASS 1.534s after correction (`review-order-green.log`).
- `go test -race ./test/e2e/scenarios/agentic ./test/e2e/mock -count=1`: PASS 1.399s / 1.761s
  (`review-final-race.log`).
- `go tool revive -config revive.toml -set_exit_status ./test/e2e/scenarios/agentic/... ./test/e2e/mock/...`:
  PASS (`review-pinned-revive.log`). `git diff --check`: PASS.

Independent re-review returns **APPROVE for this corrected E2E source candidate**, no remaining findings.
Reviewer verified the ordered observation, intended RED/GREEN, final race/lint evidence and all five hashes.
Deployed execution remains a separate gate; this source approval is not a deployed pass, a missing-request fix,
supported-retention proof or PR-readiness decision.

### E2E candidate fingerprints

SHA-256; paths relative to `test/e2e/scenarios/agentic/`:

```text
f1f28a09b00309d7681d9890cf89d38bf88d494da4820a06940340d1d1ffd9be  scenario.go
ba9b326ee7175eaed6b773663188f88f7feaa97b0d7f3cfa4a1743826515201e  approval_signal_test.go
783f586bc1c3c0ae190a78418384d1ba7d4b7bbdc50c52f904e56ec65454908c  approval_restart_test.go
e5d0e9c1bf9ab402c7c89cd6fc224bd095902a143b8641cf6f83a130f9323320  chat_retirement.go
857de5555dac89f306c857606a5fc75fa7916d57017f5b49d19fd1daa839827c  chat_retirement_test.go
```

### Deployed execution

Root ran `env -u AGENTIC_LLM_URL task e2e:agentic` with `pipefail` and complete log capture against this frozen dirty
candidate. Preflight observed no containers, no volumes, no integration lock and all six selected ports free. The
existing task built the production/process-barrier binaries and used its local mock provider, not a paid provider.
Root inspected application readiness, Docker process start times and current stream/consumer progress during the run.

Result: **exit 0**, scenario start 2026-09-18 13:57:19 +02:00, completion 13:59:52 +02:00,
duration **2m33.040884875s**, **18 asserting stages**. Meaningful reported stage durations/observations:

- independent chat turns: 429ms;
- retired-target refusals: 217ms;
- approval after restart: 6,501ms;
- stage-A process replacement: 99,497ms, one replay executor effect and one dispatch replacement user response;
- durable tool replay: 44,658ms, one executor invocation;
- final validation completed, including all eight retirement/chat observations.

The task's deferred teardown completed. Post-run Docker container and volume listings are empty; only this run's
temporary stack was removed. The five scenario hashes and preserved missing-request test hash remain unchanged.
Full log: `/private/tmp/gh1146-retirement-e2e.dp7PRk/agentic.log`, SHA-256
`02ad3881e1082ac9053acd22f0377bec8b49bdefc6526f3e2d481e7bc54758b2`.
The log contains the complete build, scenario result and cleanup; the runner emits aggregate stage evidence, not
individual retained payloads. This is deployed retirement/chat and existing restart-path proof, not model-quality,
indefinite retention or every missing-history case proof.

`task spec:properties` passes 386/386 and `openspec validate --all --strict` passes 55/55. Their logs are alongside
the E2E log. The retirement integration/E2E subtask can now be checked; the separate missing-request RED,
finite supported-retention proofs, R8 and final combined-source gates remain open. No commit, push, restack, archive,
merge or closure occurred. Parent #1156 and held #1312 stay untouched.

Independent evidence/task reconciliation returns **PASS**: the reviewer verified the complete log hash, success,
assertion count, recorded durations and cleanup, and confirmed that only the retirement subtask is checked.
The attempted PR-description synchronization was refused by publication approval; the local records contain this
completed E2E checkpoint, but the published stop point still needs that update. No alternate write was attempted.

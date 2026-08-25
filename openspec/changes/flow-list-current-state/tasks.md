# Tasks — flow-list-current-state

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was never recorded is
indistinguishable from one that was skipped. A `[~]` is a recorded decision and MUST also be noted in the spec delta.
No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads the words hold / blocked / blocking / halt / red / failed / failing
in any OPEN task line as a live caveat. Only task 2.6 (the baseline capture) uses them. Everywhere else say "pause
seam", "barrier", "abort", "does not compile", "MUST fail".

Premises (re-measured at `dc2724c4`): `flowstore/manager.go:212-230` (List: raw `s.bucket.Keys` `:214`; empty bucket
wrapped transient `:215-217`; per-key abort re-wrapped transient `:222-225`); `:214` is the only reader of
`Manager.bucket` (`grep -n 's\.bucket' flowstore/*.go`); `:97-113` (Get: transient wrap of every KV error `:104`,
fatal on decode `:109`); `:26-33` (`beforeUpdateWrite` seam precedent); `natsclient/kv.go:76-93` (Get →
`ErrKVKeyNotFound` for absent and tombstoned keys), `:494-508` (`Keys` → `nil, nil` on `ErrNoKeysFound`), `:650`
(sentinel); `pkg/errs/errs.go:121-124` (`Unwrap` keeps the chain), `:155-163,199-207` (`IsTransient`/`IsFatal`
resolve the first classified error via `errors.As`, so an outer `WrapTransient` masks an inner fatal);
`service/flow_service.go:127-130` (startup substring), `:221` (GET /flows `200` has no `SchemaRef`), `:239-245`
(`ResponseTypes`), `:266-277` (`handleListFlows` substring); `service/schema.go:12-20` (pointer → `anyOf` null),
`:46-54` (slice → inline items), `:106-109` (`required` = not `omitempty`, not pointer);
`processor/agentic-tools/executors/flows.go:178-184` (`No flows configured.` — with the period),
`flows_test.go:84-95,212-235` (fake-only empty test); tagged executor tests exist and share `skipAllBut`
(`register_graph_query_integration_test.go:33`); `FlowListResponse` has no definition anywhere
(`grep -rn FlowListResponse --include='*.go' --include='*.yaml' .` → 0); `test/e2e` has no `list_flows` script and no
flow list scenario (`grep -rn 'list_flows\|flowbuilder/flows' test/` → only the unused client helper
`test/e2e/client/observability.go:87-114`).

## 1. Claim

- [x] 1.1 Branch `claude/gh1010-flowstore-list-current-state` pushed; draft PR open with `Closes #1010` and
      `implemented-by: <persona>` in the body; this change directory is its first commit.
      Draft PR **#1085** (`Closes #1010`, `implemented-by: opus`); first commit `a2f7620d docs(openspec):
      flow-list-current-state — Slice B target state for #1010`, whose only content is this change directory.

## 2. Baseline capture — write the named tests first

- [x] 2.1 `flowstore/manager.go`: add the pause seam `beforeListGet func(ctx context.Context, key string)` on
      `Manager` (nil in production; doc comment mirrors `beforeUpdateWrite`; never exported, never an option or
      constructor parameter), invoked in `List` immediately before each per-key `Get`. Nothing else in `List`
      changes in this commit, so the §2 tests fail behaviourally rather than at compile. Grep proof after:
      `grep -rn "beforeListGet" . --include='*.go'` → hits only in `flowstore/manager.go` and
      `flowstore/manager_integration_test.go` (`package flowstore`).
      `flowstore/manager.go:35-41` (the field and its doc comment, mirroring `beforeUpdateWrite`), invoked at
      `:229-231` immediately before `s.Get(ctx, key)`. Nothing else in `List` changed in the RED commit.
      `grep -rn "beforeListGet" . --include='*.go'` → 7 hits: 4 in `flowstore/manager.go`, 3 in
      `flowstore/manager_integration_test.go` (`package flowstore`). No exported field, option, constructor
      parameter, or build tag.
- [x] 2.2 `flowstore/manager_integration_test.go` (`//go:build integration`; real NATS via `newTestManager`):
      - `TestManagerListEmptyBucketReturnsNonNilEmpty` — fresh bucket; assert `err == nil`, `flows != nil`,
        `len(flows) == 0`.
      - `TestManagerListSkipsOnlyVanishedKey` — create A and B; seam: when `key == B.ID`, count the call and
        `store.kvStore.Delete(ctx, B.ID)`; assert `err == nil`, exactly one element and it is A, the seam fired
        exactly once for B; clear the seam and assert a second List also returns exactly A. No sleep, no retry.
      - `TestManagerListPreservesPerKeyTransientFailure` — create A and B; run List under
        `ctx, cancel := context.WithCancel(t.Context())`; seam: when `key == B.ID`, `cancel()`; assert `err != nil`,
        `errs.IsTransient(err)`, `errors.Is(err, context.Canceled)`,
        `!errors.Is(err, natsclient.ErrKVKeyNotFound)`, and `flows == nil` (no partial result as success).
        Expected GREEN at baseline (baseline aborts on everything); forced omission 4.2 is what makes it fail.
      - `TestManagerListPreservesCorruptRecordFailure` — create A; `store.kvStore.Put(ctx, "corrupt-flow",
        []byte("{not json"))`; assert `err != nil`, `errs.IsFatal(err)`, `!errs.IsTransient(err)`,
        `!errors.Is(err, natsclient.ErrKVKeyNotFound)`, `flows == nil`.

      `flowstore/manager_integration_test.go:488` (`TestManagerListEmptyBucketReturnsNonNilEmpty`), `:503`
      (`TestManagerListSkipsOnlyVanishedKey`), `:548` (`TestManagerListPreservesPerKeyTransientFailure`), `:585`
      (`TestManagerListPreservesCorruptRecordFailure`), over real NATS via `newTestManager`.
- [x] 2.3 `processor/agentic-tools/executors/flows_integration_test.go` (new; `//go:build integration`;
      `package executors`): `TestFlowExecutorListFlowsRealManagerEmpty` —
      `natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())` → `flowstore.NewManager` →
      `RegisterBuiltins(ctx, registry, ToolDependencies{NATSClient: client, FlowManager: mgr,
      SkipBuiltins: skipAllBut("flows")})` → `registry.Execute(ctx, agentic.ToolCall{ID: "l1", Name: "list_flows"})`;
      assert `execErr == nil`, `result.Error == ""`, `result.ErrorKind == ""`, and
      `result.Content == "No flows configured."` (exact `==`, not `Contains`; the period is part of the literal).
      If `RegisterBuiltins` needs unrelated dependencies to admit the flows gate, record that here and drive
      `NewFlowExecutor(mgr).Execute` directly instead.
      `processor/agentic-tools/executors/flows_integration_test.go:23`. `RegisterBuiltins` admitted the flows gate
      with `NATSClient` + `FlowManager` alone (`register.go:201` gates on nothing but a non-nil `deps.FlowManager`),
      so the production wire is driven as written — no direct `NewFlowExecutor` fallback was needed. The test also
      asserts `registry.GetTool("list_flows") != nil` so a silent registration skip cannot masquerade as a pass.
- [x] 2.4 `service/flow_service_test.go` (`//go:build integration`, `package service_test`):
      - `TestHandleListFlowsEmptyResponseIsNonNullArray` — `createTestFlowService` (fresh, empty bucket);
        `GET /flowbuilder/flows`; assert `200`, `Content-Type` `application/json`, the raw body's `flows` member is
        exactly `[]` (decode into `map[string]json.RawMessage`; `string(raw["flows"]) == "[]"`), and decoding into a
        fresh `service.FlowListResponse` gives `Flows != nil && len(Flows) == 0`. Expected GREEN at baseline (the
        substring branch happens to produce `[]`); forced omissions 4.3 and 4.4(a) are what make it fail.
      - `TestEnsureDefaultFlowEmptyListUsesTypedOutcome` — a boot config with one enabled component
        (`types.ComponentConfig{Type: types.ComponentTypeInput, Name: "udp", Enabled: true,
        Config: json.RawMessage(`{"port":14550}`)}`); a logger writing to a `bytes.Buffer`;
        `NewFlowServiceFromConfig` → `Start(t.Context())` with `Stop` in `t.Cleanup`; assert `Start` returned nil,
        the buffer does not contain `Failed to create default flow diagram`, and `flowStore.List` returns exactly one
        Flow named `default` with one node. Expected GREEN at baseline; forced omission 4.3 is what makes it fail.
      - Extend `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager`: after the POST,
        `GET /flowbuilder/flows` → decode into a FRESH `service.FlowListResponse` (never into a value the test still
        holds); assert exactly one Flow with the created `id`, `name`, `version` 1 and `created_by`. This guards the
        `[]*Flow → []Flow` builder (4.5).

      `service/flow_service_test.go:484` (`TestHandleListFlowsEmptyResponseIsNonNullArray`), `:506`
      (`TestEnsureDefaultFlowEmptyListUsesTypedOutcome`), and the list assertion inside
      `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager` at `:133-144`. The startup test
      logs through a mutex-guarded sink (`lockedBuffer`, `:464-481`) because `Start` spawns the stream-override
      expiry reporter with a logger derived from the same handler — an unguarded `bytes.Buffer` is a data race
      under `-race`, not a flake.
- [x] 2.5 `service/flow_surface_test.go` `TestFlowOpenAPIPreservesFlowCRUDWireSchema`: extend — `GET /flows` `200`
      `SchemaRef == "#/components/schemas/FlowListResponse"`; `ResponseTypes` carries
      `reflect.TypeOf(FlowListResponse{})`; `SchemaFromType(reflect.TypeOf(FlowListResponse{}))` has `required`
      exactly `["flows"]`, `properties.flows.type == "array"`, and `properties.flows.items` is an object whose
      `properties` include `id`, `name`, `version`, `nodes`, `connections` and which has no `anyOf` key. Until 3.4
      the `service` test binary does not compile (`undefined: FlowListResponse`) — that is the baseline capture of
      2.4 and 2.5 together, as in Slice A.
      `service/flow_surface_test.go:76-121` inside `TestFlowOpenAPIPreservesFlowCRUDWireSchema`.
- [x] 2.6 RED capture on baseline code (§2 tests + the 2.1 seam only; production `List` untouched), recorded here
      verbatim (package + test name + failing assertion):

  ```
  go test -race -tags=integration -count=1 -run 'TestManagerList' ./flowstore/
  go test -race -tags=integration -count=1 -run 'TestFlowExecutorListFlowsRealManagerEmpty' ./processor/agentic-tools/executors/
  go test -race -tags=integration -count=1 -run 'TestHandleListFlowsEmptyResponseIsNonNullArray|TestEnsureDefaultFlowEmptyListUsesTypedOutcome|TestFlowCRUDDoesNotPublish' ./service/
  go test -race -count=1 -run 'TestFlowOpenAPIPreservesFlowCRUDWireSchema' ./service/
  ```

  RED at `a2f7620d` + the §2 tests + the 2.1 seam only (production `List` logic untouched). NATS `INFO` lines
  elided; every `--- FAIL` / build-failure line is verbatim.

  ```
  $ go test -race -tags=integration -count=1 -run 'TestManagerList' ./flowstore/
  --- FAIL: TestManagerListEmptyBucketReturnsNonNilEmpty (0.41s)
      manager_integration_test.go:493: List over an empty bucket returned an error: flowstore.List: list KV keys failed: nats: no keys found
  --- FAIL: TestManagerListSkipsOnlyVanishedKey (0.23s)
      manager_integration_test.go:529: List with a key deleted at the seam returned an error: flowstore.List: get flow flow-b failed: flowstore.Get: get from KV failed: kv: key not found
  --- FAIL: TestManagerListPreservesCorruptRecordFailure (0.24s)
      manager_integration_test.go:600: a stored record that does not decode is not classified fatal: flowstore.List: get flow corrupt-flow failed: flowstore.Get: unmarshal flow failed: invalid character 'n' looking for beginning of object key string
      manager_integration_test.go:603: a stored record that does not decode is classified transient: flowstore.List: get flow corrupt-flow failed: flowstore.Get: unmarshal flow failed: invalid character 'n' looking for beginning of object key string
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	1.479s
  FAIL

  $ go test -race -tags=integration -count=1 -run 'TestFlowExecutorListFlowsRealManagerEmpty' ./processor/agentic-tools/executors/
  --- FAIL: TestFlowExecutorListFlowsRealManagerEmpty (0.41s)
      flows_integration_test.go:40:
          	Error:      	Not equal:
          	            	expected: ""
          	            	actual  : "list failed: flowstore.List: list KV keys failed: nats: no keys found"
          	Messages:   	an empty store must carry no error attachment
      flows_integration_test.go:42:
          	Error:      	Not equal:
          	            	expected: "No flows configured."
          	            	actual  : ""
  FAIL
  FAIL	github.com/c360studio/semstreams/processor/agentic-tools/executors	0.793s
  FAIL

  $ go test -race -tags=integration -count=1 -run 'TestHandleListFlowsEmptyResponseIsNonNullArray|TestEnsureDefaultFlowEmptyListUsesTypedOutcome|TestFlowCRUDDoesNotPublish' ./service/
  # github.com/c360studio/semstreams/service [github.com/c360studio/semstreams/service.test]
  service/flow_surface_test.go:83:40: undefined: FlowListResponse
  service/flow_surface_test.go:87:46: undefined: FlowListResponse
  FAIL	github.com/c360studio/semstreams/service [build failed]
  FAIL

  $ go test -race -count=1 -run 'TestFlowOpenAPIPreservesFlowCRUDWireSchema' ./service/
  # github.com/c360studio/semstreams/service [github.com/c360studio/semstreams/service.test]
  service/flow_surface_test.go:83:40: undefined: FlowListResponse
  service/flow_surface_test.go:87:46: undefined: FlowListResponse
  FAIL	github.com/c360studio/semstreams/service [build failed]
  FAIL
  ```

  Notes on the capture: (a) no `[no tests to run]` line appeared on any command — the tagged runs each stood up
  their own NATS server (four separate `Connecting to NATS` lines on the `./flowstore/` run, one per test), so
  every named test executed; (b) `TestManagerListPreservesPerKeyTransientFailure` PASSED at baseline, as the task
  predicted — baseline `List` aborts on every per-key error, so it is a regression guard here and 4.2 is what makes
  it fail; its `errors.Is(err, context.Canceled)` and `errs.IsTransient` assertions both held at baseline, so the
  cancellation cause does survive `Manager.Get`'s wrap; (c) the corrupt-record failure names both class assertions —
  `IsFatal` false AND `IsTransient` true — which is exactly the outer `WrapTransient` masking `Get`'s `WrapFatal`;
  (d) both `./service/` commands fail identically because `package service` (`flow_surface_test.go`) and
  `package service_test` (`flow_service_test.go`) compile into one test binary, so the internal-package build error
  masks the external one, as in Slice A.

  Expected shape: `TestManagerListEmptyBucketReturnsNonNilEmpty` failed with a transient error carrying
  `nats: no keys found`; `TestManagerListSkipsOnlyVanishedKey` failed with a transient abort `get flow <B>` carrying
  `kv: key not found`; `TestManagerListPreservesCorruptRecordFailure` failed on the class assertion (`IsFatal` false —
  the outer transient wrap); `TestManagerListPreservesPerKeyTransientFailure` passed (regression guard);
  `TestFlowExecutorListFlowsRealManagerEmpty` failed with `result.Error` = `list failed: … no keys found`; both
  `./service/` commands failed to build (`undefined: FlowListResponse`). A `[no tests to run]` line means the tag or
  `-run` is wrong, not that the suite is green — record it as a broken invocation and fix it.

## 3. GREEN — implement Slice B

- [x] 3.1 `flowstore/manager.go` `List`: `keys, err := s.kvStore.Keys(ctx)` (a Keys failure stays
      `errs.WrapTransient` as today); `flows := make([]*Flow, 0, len(keys))`; per key: seam, then `s.Get`; on
      `errors.Is(err, natsclient.ErrKVKeyNotFound)` → `continue`; on any other error →
      `return nil, fmt.Errorf("flowstore.List: get flow %s: %w", key, err)` — a plain `%w` wrap, no `errs.Wrap*`, so
      the class Get assigned is the first one `errs.IsFatal`/`errs.IsTransient` resolve. Doc comment states: current
      state, typed-absence omission, abort with the read's class, no ordering promise.
      `flowstore/manager.go:218-260` — doc comment `:218-236` (current state, typed-absence omission, abort with
      the read's class and WHY the wrap is a plain `%w`, no ordering promise); `s.kvStore.Keys` at `:239`; the
      `errs.WrapTransient` on a Keys failure is unchanged at `:240-242`; `make([]*Flow, 0, len(keys))` at `:244`;
      the seam at `:246-248`; `errors.Is(err, natsclient.ErrKVKeyNotFound)` → `continue` at `:251-253`; the plain
      abort `return nil, fmt.Errorf("flowstore.List: get flow %s: %w", key, err)` at `:254`.
      `grep -n 'errs.Wrap' flowstore/manager.go` shows no `errs.Wrap*` inside the per-key loop.
- [x] 3.2 Remove `Manager.bucket` and its comment once `grep -n 's\.bucket' flowstore/*.go` returns nothing
      (`NewManager` keeps its local `bucket` for `natsClient.NewKVStore(bucket)`). If another reader exists, record
      it here and keep the field.
      Measured, not assumed: `grep -n 's\.bucket' flowstore/*.go` → no matches (exit 1) after 3.1, so the field had
      exactly one reader and it is gone. `Manager` now has one production field, `kvStore`, plus the two test-only seams (`flowstore/manager.go:22-33`); the stale
      comment `// Raw bucket for operations like Keys()` went with it. `Watch` already read through
      `s.kvStore.Watch` (`:264`), so nothing else lost a path. `NewManager` keeps its local `bucket` for
      `natsClient.NewKVStore(bucket)` and the `jetstream` import is still needed there and by `Watch`'s return type.
      Context-ownership re-check on the touched seam (repo HARD RULE): `Manager` retains no `context.Context` —
      `beforeListGet` and `beforeUpdateWrite` RECEIVE one per call and store nothing — and this change adds no
      `context.Background()`/`TODO()`. The one `context.Background()` in the file is pre-existing, at
      `flowstore/manager.go:49` inside `NewManager`; it is out of this slice's scope and was left untouched.
      `grep -n 'context.Background()\|context.TODO()' flowstore/manager.go service/flow_service.go
      processor/agentic-tools/executors/flows.go` → that one hit only.
- [x] 3.3 `service/flow_service.go`: `handleListFlows` — on error keep the existing opaque `500` (Slice C owns the
      projection); on success `fs.writeJSON(w, newFlowListResponse(flows))`. `ensureDefaultFlowFromConfig` —
      `if err != nil { return fmt.Errorf("list flows: %w", err) }`; delete the substring branch.
      `grep -n 'no keys found' service/flow_service.go` → 0.
      `service/flow_service.go:267-274` (`handleListFlows`: the opaque 500 is byte-identical to baseline; success
      is `fs.writeJSON(w, newFlowListResponse(flows))`) and `:127-130` (`ensureDefaultFlowFromConfig`, now a plain
      `if err != nil`). `grep -n 'no keys found' service/flow_service.go` → no matches (exit 1). The remaining
      `strings.` uses in the file are `:189` (`HasSuffix` on the route prefix) and `:463,:478` (the metric-name
      helper) — no substring branch on an error survives.
- [x] 3.4 `service/flow_service.go`: `FlowListResponse` (exported; `Flows []flowstore.Flow` tagged `json:"flows"`;
      doc comment: present and non-null, `[]` when empty) and one unexported builder
      `newFlowListResponse(flows []*flowstore.Flow) FlowListResponse` that allocates
      `make([]flowstore.Flow, 0, len(flows))` and appends `*f` in the Manager's order. Register the type in
      `ResponseTypes`; set `GET /flows` `200` `SchemaRef` to `#/components/schemas/FlowListResponse`.
      `service/flow_service.go:276-284` (`FlowListResponse`, `Flows []flowstore.Flow` tagged `json:"flows"` with no
      `omitempty`, so `schema.go:106-109` derives it required), `:286-294` (`newFlowListResponse`, the ONE builder;
      `grep -n 'newFlowListResponse' service/*.go` → its definition plus the single call at `:273`), `:245`
      (`ResponseTypes`), `:221` (the `GET /flows` `200` `SchemaRef`).
- [x] 3.5 All §2 tests green: the four focused commands from 2.6, then
      `go test -race -count=1 ./flowstore/... ./service/... ./processor/agentic-tools/...` and
      `go test -race -tags=integration -p 2 -count=1 ./flowstore/... ./service/... ./processor/agentic-tools/executors/...`.
      Record output shape here. Commit GREEN before §4.

  ```
  $ go test -race -tags=integration -count=1 -v -run 'TestManagerList' ./flowstore/
  --- PASS: TestManagerListEmptyBucketReturnsNonNilEmpty (0.40s)
  --- PASS: TestManagerListSkipsOnlyVanishedKey (0.23s)
  --- PASS: TestManagerListPreservesPerKeyTransientFailure (0.24s)
  --- PASS: TestManagerListPreservesCorruptRecordFailure (0.24s)
  ok  	github.com/c360studio/semstreams/flowstore	2.418s
  $ go test -race -tags=integration -count=1 -v -run 'TestFlowExecutorListFlowsRealManagerEmpty' ./processor/agentic-tools/executors/
  --- PASS: TestFlowExecutorListFlowsRealManagerEmpty (0.41s)
  ok  	github.com/c360studio/semstreams/processor/agentic-tools/executors	1.816s
  $ go test -race -tags=integration -count=1 -v -run 'TestHandleListFlowsEmptyResponseIsNonNullArray|TestEnsureDefaultFlowEmptyListUsesTypedOutcome|TestFlowCRUDDoesNotPublish' ./service/
  --- PASS: TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager (0.25s)
  --- PASS: TestHandleListFlowsEmptyResponseIsNonNullArray (0.28s)
  --- PASS: TestEnsureDefaultFlowEmptyListUsesTypedOutcome (0.30s)
  ok  	github.com/c360studio/semstreams/service	2.785s
  $ go test -race -count=1 -v -run 'TestFlowOpenAPIPreservesFlowCRUDWireSchema' ./service/
  --- PASS: TestFlowOpenAPIPreservesFlowCRUDWireSchema (0.00s)
  ok  	github.com/c360studio/semstreams/service	1.442s
  $ go test -race -count=1 ./flowstore/... ./service/... ./processor/agentic-tools/...
  ok  	github.com/c360studio/semstreams/flowstore	1.271s
  ok  	github.com/c360studio/semstreams/service	6.371s
  ok  	github.com/c360studio/semstreams/processor/agentic-tools	2.124s
  ok  	github.com/c360studio/semstreams/processor/agentic-tools/executors	2.603s
  ok  	github.com/c360studio/semstreams/processor/agentic-tools/runner	1.538s
  $ go test -race -tags=integration -p 2 -count=1 ./flowstore/... ./service/... ./processor/agentic-tools/executors/...
  ok  	github.com/c360studio/semstreams/flowstore	3.920s
  ok  	github.com/c360studio/semstreams/service	42.019s
  ok  	github.com/c360studio/semstreams/processor/agentic-tools/executors	4.321s
  ```

  The four focused commands were re-run with `-v` so the PASS lines name the tests: an `ok` alone cannot
  distinguish a green suite from a `-run` that matched nothing.

## 4. Forced omissions — each guard must be load-bearing

Commit §3 first. For each mutation: apply, print `[applied]`, run the named test, record the FAIL line verbatim,
restore with `cp` from a pre-mutation copy and confirm `shasum` equals the committed file (no git checkout / stash /
restore of any kind).

- [x] 4.1 M1 — remove the typed-absence `continue` (every per-key error aborts): `TestManagerListSkipsOnlyVanishedKey`
      MUST fail.

  ```
  [applied] M1 — typed-absence continue removed
  $ go test -race -tags=integration -count=1 -run 'TestManagerListSkipsOnlyVanishedKey' ./flowstore/
  --- FAIL: TestManagerListSkipsOnlyVanishedKey (0.43s)
      manager_integration_test.go:529: List with a key deleted at the seam returned an error: flowstore.List: get flow flow-b: flowstore.Get: get from KV failed: kv: key not found
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	0.801s
  ```

  Restored by `cp` from the pre-mutation copy; `shasum flowstore/manager.go` →
  `2540ac674486fd1d6ba66b18a2b6f48aa5d21b92`, equal to the committed file.
- [x] 4.2 M2 — `continue` on every per-key error (the persona/flowtemplate shape):
      `TestManagerListPreservesPerKeyTransientFailure` and `TestManagerListPreservesCorruptRecordFailure` MUST fail.

  ```
  [applied] M2 — continue on every per-key error
  $ go test -race -tags=integration -count=1 -run 'TestManagerListPreservesPerKeyTransientFailure|TestManagerListPreservesCorruptRecordFailure' ./flowstore/
  --- FAIL: TestManagerListPreservesPerKeyTransientFailure (0.40s)
      manager_integration_test.go:569: List under a cancelled read returned no error (flows=[flow-a])
  --- FAIL: TestManagerListPreservesCorruptRecordFailure (0.24s)
      manager_integration_test.go:597: List over a record that does not decode returned no error (flows=[flow-a])
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	0.985s
  ```

  This is the mutation that makes `TestManagerListPreservesPerKeyTransientFailure` load-bearing: it PASSED at RED
  (2.6) because baseline `List` aborted on everything, so M2 is its only proof. Restored by `cp`;
  `shasum flowstore/manager.go` → `2540ac674486fd1d6ba66b18a2b6f48aa5d21b92`.
- [x] 4.3 M3 — the empty bucket becomes an error again with the old text
      (`if len(keys) == 0 { return nil, errs.WrapTransient(jetstream.ErrNoKeysFound, "flowstore", "List",
      "list KV keys") }`), consumers untouched: `TestManagerListEmptyBucketReturnsNonNilEmpty`,
      `TestFlowExecutorListFlowsRealManagerEmpty`, `TestHandleListFlowsEmptyResponseIsNonNullArray` (500), and
      `TestEnsureDefaultFlowEmptyListUsesTypedOutcome` MUST fail. A consumer that still matched the substring would
      pass here; the required FAIL is what proves it branches on the typed outcome.

  ```
  [applied] M3 — empty bucket is an error again, with the old text
  $ go test -race -tags=integration -count=1 -run 'TestManagerListEmptyBucketReturnsNonNilEmpty' ./flowstore/
  --- FAIL: TestManagerListEmptyBucketReturnsNonNilEmpty (0.41s)
      manager_integration_test.go:493: List over an empty bucket returned an error: flowstore.List: list KV keys failed: nats: no keys found
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	0.752s

  $ go test -race -tags=integration -count=1 -run 'TestFlowExecutorListFlowsRealManagerEmpty' ./processor/agentic-tools/executors/
  --- FAIL: TestFlowExecutorListFlowsRealManagerEmpty (0.41s)
          	            	expected: ""
          	            	actual  : "list failed: flowstore.List: list KV keys failed: nats: no keys found"
          	            	expected: "No flows configured."
          	            	actual  : ""
  FAIL
  FAIL	github.com/c360studio/semstreams/processor/agentic-tools/executors	0.817s

  $ go test -race -tags=integration -count=1 -run 'TestHandleListFlowsEmptyResponseIsNonNullArray|TestEnsureDefaultFlowEmptyListUsesTypedOutcome' ./service/
  --- FAIL: TestHandleListFlowsEmptyResponseIsNonNullArray (0.26s)
      flow_service_test.go:491:
          	Error:      	Not equal:
          	            	expected: 200
          	            	actual  : 500
          	Messages:   	{"error":"Internal server error"}
  --- FAIL: TestEnsureDefaultFlowEmptyListUsesTypedOutcome (0.26s)
      flow_service_test.go:549:
          	Error:      	"time=... level=WARN msg=\"Failed to create default flow diagram from boot config\" error=\"list flows: flowstore.List: list KV keys failed: nats: no keys found\"\ntime=... level=INFO msg=\"Flow service started\"\n" should not contain "Failed to create default flow diagram"
          	Messages:   	an empty store is ordinary state, not a default-flow import failure
  FAIL
  FAIL	github.com/c360studio/semstreams/service	1.153s
  ```

  **Correction made under this mutation, and re-run.** The first M3 run failed
  `TestEnsureDefaultFlowEmptyListUsesTypedOutcome` at a fixture pre-check (`flowStore.List` asserted empty BEFORE
  `Start`) rather than at the startup assertion — a guard upstream of the mechanism proves the guard, not the
  mechanism. The pre-check was deleted (commit `a46001e5`), the test re-verified green on unmutated code, and M3
  re-applied; the capture above is the re-run, which lands on the warning assertion. Consumers were untouched by
  the mutation, so all four FAILs prove they branch on the typed outcome and not on the message text. Restored by
  `cp`; `shasum flowstore/manager.go` → `2540ac674486fd1d6ba66b18a2b6f48aa5d21b92`.
- [x] 4.4 M4 — non-null → null: (a) the builder uses `var out []flowstore.Flow` (nil when empty) →
      `TestHandleListFlowsEmptyResponseIsNonNullArray` MUST fail on `"flows":null`; (b) `Manager.List` returns
      `nil, nil` for no keys → `TestManagerListEmptyBucketReturnsNonNilEmpty` MUST fail.

  ```
  [applied] M4(a) — builder returns a nil slice when empty
  $ go test -race -tags=integration -count=1 -run 'TestHandleListFlowsEmptyResponseIsNonNullArray' ./service/
  --- FAIL: TestHandleListFlowsEmptyResponseIsNonNullArray (0.27s)
      flow_service_test.go:498:
          	Error:      	Not equal:
          	            	expected: "[]"
          	            	actual  : "null"
          	Messages:   	an empty store must serialise as [], never null
  FAIL
  FAIL	github.com/c360studio/semstreams/service	1.180s

  [applied] M4(b) — List returns nil, nil for no keys
  $ go test -race -tags=integration -count=1 -run 'TestManagerListEmptyBucketReturnsNonNilEmpty' ./flowstore/
  --- FAIL: TestManagerListEmptyBucketReturnsNonNilEmpty (0.42s)
      manager_integration_test.go:496: List over an empty bucket returned a nil slice, want a non-nil empty slice
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	0.757s
  ```

  Restored by `cp` after each half; `shasum service/flow_service.go` → `be645e88587dca0d9d63b3beb0d816f30a56d1d8`
  and `shasum flowstore/manager.go` → `2540ac674486fd1d6ba66b18a2b6f48aa5d21b92`, both equal to the committed
  files.
- [x] 4.5 M5 — the builder drops elements (returns the empty slice for any input): the populated-list assertion in
      `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager` MUST fail.

  ```
  [applied] M5 — builder drops every element
  $ go test -race -tags=integration -count=1 -run 'TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager' ./service/
  --- FAIL: TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager (0.26s)
      flow_service_test.go:142:
          	Error:      	"[]" should have 1 item(s), but has 0
          	Messages:   	list must carry exactly the created flow: {"flows":[]}
  FAIL
  FAIL	github.com/c360studio/semstreams/service	1.165s
  ```

  Restored by `cp`; `shasum service/flow_service.go` → `be645e88587dca0d9d63b3beb0d816f30a56d1d8`.
- [x] 4.6 M6 — re-apply an outer `errs.WrapTransient` to the per-key abort:
      `TestManagerListPreservesCorruptRecordFailure` MUST fail (`IsFatal` false).

  ```
  [applied] M6 — outer errs.WrapTransient re-applied to the per-key abort
  $ go test -race -tags=integration -count=1 -run 'TestManagerListPreservesCorruptRecordFailure' ./flowstore/
  --- FAIL: TestManagerListPreservesCorruptRecordFailure (0.43s)
      manager_integration_test.go:600: a stored record that does not decode is not classified fatal: flowstore.List: get flow corrupt-flow failed: flowstore.List: get flow corrupt-flow: flowstore.Get: unmarshal flow failed: invalid character 'n' looking for beginning of object key string
      manager_integration_test.go:603: a stored record that does not decode is classified transient: flowstore.List: get flow corrupt-flow failed: flowstore.List: get flow corrupt-flow: flowstore.Get: unmarshal flow failed: invalid character 'n' looking for beginning of object key string
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	0.769s
  ```

  The plain `%w` is therefore load-bearing, not stylistic. Restored by `cp`; `shasum flowstore/manager.go` →
  `2540ac674486fd1d6ba66b18a2b6f48aa5d21b92`.
- [x] 4.7 Post-restore: `shasum` of every mutated file equals its committed hash; `git status --porcelain` empty; the
      3.5 commands green again. Repeat-run stability of the seam tests (no sleeps, so deterministic):
      `go test -race -tags=integration -count=5 -run 'TestManagerListSkipsOnlyVanishedKey|TestManagerListPreservesPerKeyTransientFailure' ./flowstore/`.

  ```
  $ shasum flowstore/manager.go service/flow_service.go
  2540ac674486fd1d6ba66b18a2b6f48aa5d21b92  flowstore/manager.go
  be645e88587dca0d9d63b3beb0d816f30a56d1d8  service/flow_service.go
  $ git status --porcelain
  (no output)
  $ go test -race -tags=integration -count=1 -v -run 'TestManagerList' ./flowstore/
  --- PASS: TestManagerListEmptyBucketReturnsNonNilEmpty (0.41s)
  --- PASS: TestManagerListSkipsOnlyVanishedKey (0.24s)
  --- PASS: TestManagerListPreservesPerKeyTransientFailure (0.23s)
  --- PASS: TestManagerListPreservesCorruptRecordFailure (0.24s)
  ok  	github.com/c360studio/semstreams/flowstore	2.460s
  $ go test -race -tags=integration -count=1 -v -run 'TestFlowExecutorListFlowsRealManagerEmpty' ./processor/agentic-tools/executors/
  --- PASS: TestFlowExecutorListFlowsRealManagerEmpty (0.42s)
  ok  	github.com/c360studio/semstreams/processor/agentic-tools/executors	1.823s
  $ go test -race -tags=integration -count=1 -v -run 'TestHandleListFlowsEmptyResponseIsNonNullArray|TestEnsureDefaultFlowEmptyListUsesTypedOutcome|TestFlowCRUDDoesNotPublish' ./service/
  --- PASS: TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager (0.26s)
  --- PASS: TestHandleListFlowsEmptyResponseIsNonNullArray (0.29s)
  --- PASS: TestEnsureDefaultFlowEmptyListUsesTypedOutcome (0.31s)
  ok  	github.com/c360studio/semstreams/service	2.780s
  $ go test -race -count=1 -v -run 'TestFlowOpenAPIPreservesFlowCRUDWireSchema' ./service/
  --- PASS: TestFlowOpenAPIPreservesFlowCRUDWireSchema (0.00s)
  ok  	github.com/c360studio/semstreams/service	1.475s
  $ go test -race -tags=integration -count=5 -run 'TestManagerListSkipsOnlyVanishedKey|TestManagerListPreservesPerKeyTransientFailure' ./flowstore/
  ok  	github.com/c360studio/semstreams/flowstore	3.947s
  ```

  Both mutated files were restored by `cp` from a pre-mutation copy taken before the first mutation; no
  `git checkout`, `git restore`, or `git stash` was used at any point.

## 5. Schema regeneration — Slice B rows only

- [x] 5.1 `task schema:generate`; `git diff --stat schemas/ specs/openapi.v3.yaml` shows only
      `paths./flows.get.responses.200` gaining the `FlowListResponse` ref and the new
      `components.schemas.FlowListResponse` entry (required `flows`; `flows.items` an inline Flow object schema; no
      `anyOf`/null on the items; no rows from Slices C–D). Commit the delta.

  ```
  $ git diff --stat schemas/ specs/openapi.v3.yaml
   specs/openapi.v3.yaml | 94 ++++++++++++++++++++++++++++++++++++++++++++++++++-
   1 file changed, 93 insertions(+), 1 deletion(-)
  ```

  Exactly two rows, and nothing under `schemas/`. (a) `paths./flows.get.responses.200` — the single deletion is
  its bare `type: object` becoming `$ref: '#/components/schemas/FlowListResponse'`. (b) the new
  `components.schemas.FlowListResponse`: `required: [flows]`, `flows.type: array`, and `flows.items` an INLINE
  Flow object schema (`id`, `name`, `version`, `nodes`, `connections`, `created_at`, `updated_at`, `last_modified`
  required; `description`/`created_by` optional) with no `anyOf` and no `type: null` anywhere in the added block —
  the value element type is what buys that. Committed as `e38b5a4c`.
- [x] 5.2 Regenerate once more; `task schema:check-changes` (`git diff --exit-code schemas/ specs/openapi.v3.yaml`)
      exits 0 — no drift.
      Re-ran `task schema:generate` on the committed tree, then
      `task schema:check-changes` → `git diff --exit-code schemas/ specs/openapi.v3.yaml`, exit 0;
      `git status --porcelain` empty.
- [x] 5.3 `go test ./test/contract/...` green (`TestCommittedOpenAPISpecValid`, `TestOpenAPISchemaReferences`).

  ```
  $ go test ./test/contract/...
  ok  	github.com/c360studio/semstreams/test/contract	2.871s
  $ go test -count=1 -v -run 'TestCommittedOpenAPISpecValid|TestOpenAPISchemaReferences' ./test/contract/...
  --- PASS: TestCommittedOpenAPISpecValid (0.00s)
  --- PASS: TestOpenAPISchemaReferences (0.00s)
  ok  	github.com/c360studio/semstreams/test/contract	0.379s
  ```

  The `-v -run` re-run names both tests, so the package `ok` is not standing in for a filter that matched nothing.

## 6. Standard gates — record each command and its result

- [x] 6.1 `task lint` — 0 warnings (revive warnings fail CI).
      Exit 0. `go vet ./...`, `go fmt ./...`, `go tool revive -config revive.toml -formatter friendly ./...`, the
      fixed-port guard (`scripts/lint-test-ports.sh`), and `go test ./test/natsclient/` (`ok`, 0.532s) all ran.
      Zero `file.go:line` diagnostic lines in the output and zero occurrences of `warning`/`error`;
      `git status --porcelain` empty afterwards, so `go fmt` rewrote nothing.
- [x] 6.2 `go test -race ./...` — no `^FAIL` lines.
      Exit 0 in 1m08.9s wall. `grep -c '^FAIL'` → 0 and `grep -c '^--- FAIL'` → 0; 153 `ok` packages, 19
      `no test files`.
- [x] 6.3 `go test -race -tags=integration -p 2 -count=1 ./...` — no `^FAIL` lines (Docker required; one agent at a
      time on a shared host per `AGENTS.md`; CI is the arbiter of a local result under contention).
      Exit 0 in 9m00.7s wall (this agent was the only heavy suite running). `grep -c '^FAIL'` → 0 and
      `grep -c '^--- FAIL'` → 0; 153 `ok` packages, 19 `no test files` — the same package counts as the untagged
      run, so the tag did not silently drop a package. The three touched packages:
      `ok flowstore 4.515s`, `ok service 42.543s`, `ok processor/agentic-tools/executors 4.106s`.
- [x] 6.4 `task build`, plus the CI cross-compile invocation
      `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o semstreams-linux-amd64 ./cmd/semstreams`.
      `task build` → `Built bin/semstreams`, exit 0. The CI cross-compile produced a 29,671,586-byte
      linux/amd64 binary, exit 0 (written outside the tree so it cannot dirty the worktree).
- [x] 6.5 `go vet -tags=integration ./...` clean (tagged tests compile).
      Exit 0, no output.
- [x] 6.6 `openspec validate flow-list-current-state --strict` — pass.
      `Change 'flow-list-current-state' is valid`.

## 7. Review and archive (inside the landing PR; the `AGENTS.md` Land order)

- [x] 7.1 `semstreams-reviewer` (Fable) on `7e393a2b`: **APPROVE**, no BLOCKING/HIGH. Dispositions: MEDIUM (FILE)
      `natsclient/kv.go:494-508` `KVStore.Keys` has no post-collection `ctx.Err()` guard (a cancelled context could
      read as empty) → filed with the `==` sentinel compare and the adjacent `"no keys found"` spellings, not Slice B;
      NIT "one field" wording here → FIXED (this commit); NIT combined-candidate e2e obligation has no `gh` home →
      filed as an issue for the owner to place. Rulings: (a) `newFlowListResponse` nil-deref acceptable — the only
      producer is `(*Manager).List`, a nil element is impossible by construction; (b) the `context.Canceled` clause in
      `TestManagerListPreservesPerKeyTransientFailure` is a documented nats.go contract and Slice C's deadline→504
      early guard — keep; (c) `GET /flows` vs `{prefix}flows` consistent with the existing spec. Owner items (i) tool
      literal period, (ii) filing — assessed in the PR body. Reviewer re-ran the focused suites and M1/M2/M4a/M6;
      full-suite §6 lines are the implementer's record.
- [ ] 7.2 The owner-run cross-agent round where the owner asks for it; fixes and re-review recorded here.
- [ ] 7.3 Reconcile: every scenario in `specs/flow-authoring/spec.md` names the test that verifies it, and that test
      exists and passed in 6.2/6.3. Any `[~]` in this file is ALSO written into the delta before archiving.
- [ ] 7.4 `openspec archive flow-list-current-state` with the spec sync as the final content commit; the narrow
      reviewer check of the archive/spec sync follows as a PR comment; then undraft. A correction after archive
      re-enters 7.3 and 7.1.

## 8. Not in scope (recorded so the archiver does not infer completion)

- Slices C (#1008 vocabulary, exact messages, List failure projection, must-exist DELETE) and D (Get projections).
- The named E2E scenarios `flow-list-current-state` (core tier; 4 assertions, empty list plus controlled
  churn/survivor) and `flow-crud-tools-empty` (extension of `task e2e:crud-tools`; completion, no error attachment,
  exact empty content; 3 assertions). Ground: the design's gate split — "Every slice independently runs lint, full
  repository race unit/integration, build, schema/no-drift, and contract tests. The exact combined candidate repeats
  them plus core and CRUD-tools E2E." Their authoring is also combined-candidate work: the crud-tools harness scripts
  a single `create_rule` through the mock LLM (`test/e2e/scenarios/crud-tools/scenario.go:1-15,170-179`) and has no
  `list_flows` script, the core tier has no flow-list scenario, and their "baseline fails N" figures are measured
  against the design baseline `774c85dc`, which only the combined proof re-runs.
- Sibling Pattern-B list semantics; other `"no keys found"` substring spellings; `$ref` reuse in the generator.
- semstreams-ui candidate validation (owner-run tag gate).

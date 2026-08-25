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
      exactly one reader and it is gone. `Manager` is now one field (`flowstore/manager.go:22-23`); the stale
      comment `// Raw bucket for operations like Keys()` went with it. `Watch` already read through
      `s.kvStore.Watch` (`:264`), so nothing else lost a path. `NewManager` keeps its local `bucket` for
      `natsClient.NewKVStore(bucket)` and the `jetstream` import is still needed there and by `Watch`'s return type.
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

- [ ] 4.1 M1 — remove the typed-absence `continue` (every per-key error aborts): `TestManagerListSkipsOnlyVanishedKey`
      MUST fail.
- [ ] 4.2 M2 — `continue` on every per-key error (the persona/flowtemplate shape):
      `TestManagerListPreservesPerKeyTransientFailure` and `TestManagerListPreservesCorruptRecordFailure` MUST fail.
- [ ] 4.3 M3 — the empty bucket becomes an error again with the old text
      (`if len(keys) == 0 { return nil, errs.WrapTransient(jetstream.ErrNoKeysFound, "flowstore", "List",
      "list KV keys") }`), consumers untouched: `TestManagerListEmptyBucketReturnsNonNilEmpty`,
      `TestFlowExecutorListFlowsRealManagerEmpty`, `TestHandleListFlowsEmptyResponseIsNonNullArray` (500), and
      `TestEnsureDefaultFlowEmptyListUsesTypedOutcome` MUST fail. A consumer that still matched the substring would
      pass here; the required FAIL is what proves it branches on the typed outcome.
- [ ] 4.4 M4 — non-null → null: (a) the builder uses `var out []flowstore.Flow` (nil when empty) →
      `TestHandleListFlowsEmptyResponseIsNonNullArray` MUST fail on `"flows":null`; (b) `Manager.List` returns
      `nil, nil` for no keys → `TestManagerListEmptyBucketReturnsNonNilEmpty` MUST fail.
- [ ] 4.5 M5 — the builder drops elements (returns the empty slice for any input): the populated-list assertion in
      `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager` MUST fail.
- [ ] 4.6 M6 — re-apply an outer `errs.WrapTransient` to the per-key abort:
      `TestManagerListPreservesCorruptRecordFailure` MUST fail (`IsFatal` false).
- [ ] 4.7 Post-restore: `shasum` of every mutated file equals its committed hash; `git status --porcelain` empty; the
      3.5 commands green again. Repeat-run stability of the seam tests (no sleeps, so deterministic):
      `go test -race -tags=integration -count=5 -run 'TestManagerListSkipsOnlyVanishedKey|TestManagerListPreservesPerKeyTransientFailure' ./flowstore/`.

## 5. Schema regeneration — Slice B rows only

- [ ] 5.1 `task schema:generate`; `git diff --stat schemas/ specs/openapi.v3.yaml` shows only
      `paths./flows.get.responses.200` gaining the `FlowListResponse` ref and the new
      `components.schemas.FlowListResponse` entry (required `flows`; `flows.items` an inline Flow object schema; no
      `anyOf`/null on the items; no rows from Slices C–D). Commit the delta.
- [ ] 5.2 Regenerate once more; `task schema:check-changes` (`git diff --exit-code schemas/ specs/openapi.v3.yaml`)
      exits 0 — no drift.
- [ ] 5.3 `go test ./test/contract/...` green (`TestCommittedOpenAPISpecValid`, `TestOpenAPISchemaReferences`).

## 6. Standard gates — record each command and its result

- [ ] 6.1 `task lint` — 0 warnings (revive warnings fail CI).
- [ ] 6.2 `go test -race ./...` — no `^FAIL` lines.
- [ ] 6.3 `go test -race -tags=integration -p 2 -count=1 ./...` — no `^FAIL` lines (Docker required; one agent at a
      time on a shared host per `AGENTS.md`; CI is the arbiter of a local result under contention).
- [ ] 6.4 `task build`, plus the CI cross-compile invocation
      `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o semstreams-linux-amd64 ./cmd/semstreams`.
- [ ] 6.5 `go vet -tags=integration ./...` clean (tagged tests compile).
- [ ] 6.6 `openspec validate flow-list-current-state --strict` — pass.

## 7. Review and archive (inside the landing PR; the `AGENTS.md` Land order)

- [ ] 7.1 Implementation review by `semstreams-reviewer` on the GREEN + §4 head; every finding's disposition recorded
      here with its commit. A finding on a path nothing reads is filed as an issue, not fixed in this PR.
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

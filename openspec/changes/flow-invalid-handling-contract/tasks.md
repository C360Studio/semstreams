# Tasks — flow-invalid-handling-contract

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was never recorded is
indistinguishable from one that was skipped. A `[~]` is a recorded decision and MUST also be noted in the spec delta.
No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads the words hold / blocked / blocking / halt / red / failed / failing
(and "deliberate", "not done", "still open") in any OPEN task line as a live caveat. They appear in exactly one open
task — 2.9, the baseline capture — and otherwise only in CLOSED tasks. Everywhere else say "MUST fail", "pause seam",
"abort", "does not compile", "load-bearing".

Premises (re-measured at `5cc0c7fb`): `flowstore/flow.go:58-132` (fail-fast `Validate`; thirteen `WrapInvalid` sites
`:61,64,71,76,81,86,93,103,108,113,120,125`; no type constants); `flowstore/manager.go:78-80,151-153` (empty-ID guard
BEFORE `Validate` at `:83`/`:156`), `:101-104` (existing key → `WrapInvalid` carrying `natsclient.ErrKVKeyExists`),
`:161-164` (Update's missing key → `WrapTransient` carrying `natsclient.ErrKVKeyNotFound`), `:208-212` (typed
conflict), `:215-225` (Delete: no read; every error `WrapTransient`); `natsclient/kv.go:444-459` (`KVStore.Delete`;
its not-found branch `:450-452` is unreachable on the plain path because nats.go v1.52.0 `jetstream/kv.go:1125-1172`
publishes a DEL marker with no existence check → a missing key deletes with nil error → repeat `DELETE` is `204`);
`natsclient/kv.go:76-93` (`Get` → `ErrKVKeyNotFound` for absent and tombstoned keys), `:650-652` (sentinels);
`engine/validator.go:36-43,76-84` (the five types; `Suggestions` `omitempty` at `:83`), `:88-92` (only
`Errors`/`Warnings` allocated), `:99-113` (`empty_flow` returns with nil `Nodes`/`DiscoveredConnections`; component
`(none)`), `:120-133` (build-error return with nil `DiscoveredConnections`), `:137-141` (pattern error → `nil,
WrapInvalid`), `:207-210,268-271` (`graph_build_error` with empty `ComponentName`), literal types at
`:103,208,226,254,269,304,368,560,592`; `engine/engine.go:63-70` (nil Flow and structural failure → error), `:87-113`
(`Compile`; duplicate instance name `:98-100` and marshal `:101-104` are plain errors with a non-nil result),
`:115-126` (`ValidationError`); `service/flow_service.go:207-253` (OpenAPI rows and types), `:267-274` (List → opaque
500), `:350-369` (Create: `:353` `Invalid request body` 400, `:361` opaque 500), `:380-400` (Update: `:383` 400,
`:387` `ID mismatch` 400, `:392-394` 409 with `err.Error()`, `:396` opaque 500), `:402-408` (Delete: 500 / 204),
`:420-441` (publish: `:423` 404 pre-read, `:426-432` 400 with `err.Error()` and possibly-null result, `:433-439` 500
progress), `:516-545` (validate: `:522` echoes decoder text, `:526` echoes both IDs, `:531-538` pre-read, `:541`
500 with `err.Error()`), `:554-560` (`writeJSONError`); `service/schema.go:13-21` (pointer → `anyOf` null), `:107`
(`required` = not `omitempty`, not pointer; `omitzero` unrecognised; `go.mod` `go 1.26.3`);
`service/openapi_types.go:47-52` (`ResponseSpec`: one `SchemaRef` + `IsArray`);
`cmd/openapi-generator/openapi_operations.go:41-46` (output `SchemaRef.OneOf` exists), `openapi_builder.go:126-165`
(never populated for responses), `validate.go:38-62` (walks `[]any`, so `oneOf` refs are checked);
`pkg/errs/errs.go:155-196` (`IsTransient` resolves the first classified error; `context.DeadlineExceeded` consulted
only for unclassified errors at `:172` — nothing classifies a deadline distinctly), `:378-380` (check
`ErrRevisionMismatch` before `IsInvalid`); `gateway/http/http.go:300-377` (a second, package-local mapper; not
reusable); claimed-gap greps (`FlowErrorResponse`, the twelve structural type names, `connection_pattern_error`, the
nine table messages) → 0 hits outside the design; `engine` importers → `service/flow_service.go:15` only;
existing tests to amend: `service/flow_service_test.go:349` (`Contains "Invalid JSON"`), `:396-398` (`Contains
"Flow ID mismatch"` + both IDs), `:169-170` (409 without a message assertion); `test/e2e` has no Flow HTTP scenario
(`grep -rn 'flowbuilder/flows' test/e2e` → `test/e2e/client/observability.go:87-114` only).

## 1. Claim

- [ ] 1.1 Branch `claude/gh1008-flow-invalid-handling` pushed; draft PR open with `Closes #1008` and
      `implemented-by: <persona>` in the body; this change directory is its first commit. Record the PR number here
      and in `conformance.md` R1.
- [ ] 1.2 Record the owner's answers to `proposal.md` §Open questions 1 (generic-invalid message), 2 (publication
      projection of non-finding `Compile` errors), and 3 (BREAKING and the tier) here with the comment URLs before
      §3 begins. 3.5 reads answer 1, 3.6 reads answer 2, 6.7 reads answer 3. Copy each into `conformance.md` DEVIATION
      only if it departs from the delta; otherwise note "as drafted".

## 2. Baseline capture — write the named tests first

- [ ] 2.1 `flowstore/flow_test.go` (unit, `package flowstore`):
      - `TestFlowValidateAggregatesStructuralIssuesInOrder` — (a) the six-fault Flow of the delta scenario: assert
        `errors.As(err, &ve)`, the exact ordered type slice, severity `error` for each, `component_name` per the
        identity rule (Flow ID; node name / ID / `strconv.Itoa(index)`; connection ID / index), `ve.Result.Nodes !=
        nil && len == 0`, `ve.Result.DiscoveredConnections != nil && len == 0`, every `Suggestions != nil`; (b) a
        twelve-fault Flow (empty ID, empty name, a node with empty ID/component/type/name, a duplicate ID, a
        connection with empty ID/source port/target port and unknown source and target): assert exactly twelve
        issues whose `Type`s equal the twelve constants, in the delta's order; (c) `validTestFlow()` still returns
        nil and a Flow with `Nodes: []FlowNode{}` returns nil.
      - `TestFlowValidateErrorCarriesResultAndIsInvalid` — for each `TestFlowValidate` mutation: `errs.IsInvalid`
        true, `errs.IsTransient` false, `errors.As` succeeds, `Result.Status == "errors"`, and `err.Error()` contains
        every issue's `Type` and `ComponentName`.
      - The existing `TestFlowValidate` stays as it is (it asserts only `IsInvalid`).
- [ ] 2.2 `flowstore/manager_integration_test.go` (`//go:build integration`; real NATS via `newTestManager`):
      - `TestManagerCreateAndUpdateReturnStructuralResult` — Create with `ID: ""` and a node lacking `Component`:
        assert `errors.As(err, &ve)` and `Result.Errors` types are exactly `[flow_id_required,
        node_component_required]`; `KVStore.Keys` on the bucket is empty afterwards. Then create a valid Flow and
        Update it with a node lacking `Component`: same assertions; stored record unchanged (`storedFlow` helper).
      - `TestManagerDeleteMissingReportsTypedAbsence` — Delete a never-created ID and, after creating and deleting a
        Flow, delete it again: both errors non-nil with `errors.Is(err, natsclient.ErrKVKeyNotFound)`; a Delete of
        an existing Flow returns nil and a subsequent Get reports typed absence.
- [ ] 2.3 `engine/validator_test.go` and a new `engine/engine_test.go` (unit, `package flowengine`, the existing
      `validationTestComponent` and `compileTestEngine` fixtures):
      - `TestValidationResultArraysAreNonNull` — validate an empty Flow, a Flow naming an unknown component, and a
        valid Flow through `Engine.ValidateFlowDefinition`; for each result assert all four slices non-nil, every
        issue `Suggestions != nil` and `ComponentName != ""`, and `json.Marshal` of the result contains no `null`
        for those members (decode into `map[string]json.RawMessage` and compare the raw members to `[]` where
        empty).
      - `TestEngineValidationTypesAreFlowstoreAliases` — `reflect.TypeOf` equality for the six names.
      - `TestValidationIssueTypesAreTheClosedVocabulary` — the twenty constants are distinct and equal the delta's
        names; every issue emitted in this test file's cases has a `Type` in that set.
      - `TestValidatorPatternErrorBecomesFinding` — a registry with a test component declaring two output ports of
        `component.PatternNetwork` on the same `ConnectionID` (or any shape that makes
        `ConnectComponentsByPatterns` return its classified invalid error at `component/flowgraph/flowgraph.go:243`);
        assert `err == nil`, `Status == "errors"`, exactly one issue, `Type == flowstore.IssueConnectionPatternError`
        (constant name at the implementer's discretion; assert through the constant), `ComponentName` is the Flow
        identity, all four arrays non-nil; then `Engine.Compile` of the same Flow returns a `*ValidationError`
        carrying that result. If the network-conflict shape proves impossible with a unit fixture, use the
        graph-mutation-provider shape (`flowgraph.go:266-281`) and record which here.
      - `TestEngineValidateFlowDefinitionStructuralInvalidIsResult` — a Flow whose node lacks `Component`:
        `ValidateFlowDefinition` returns `(result, nil)`, `Status == "errors"`, one `node_component_required`
        issue, arrays non-nil; a nil Flow still returns an error with `errs.IsInvalid`.
- [ ] 2.4 `service/flow_service_test.go` (`//go:build integration`, `package service_test`; `createTestFlowService`
      / `createTestFlowServiceWithConfigManager`; every response decoded into a FRESH value — never into the request
      struct the test still has):
      - `TestHandleCreateFlowInvalidIsBadRequestWithResult` — POST a Flow whose node lacks `component`: `400`,
        fresh `service.FlowErrorResponse` with `Error == "Flow validation failed"`, `ValidationResult.Errors` one
        issue of type `node_component_required`; raw body's `validation_result` is an object; then `GET /flows` →
        `flows` is `[]`. Also POST the bytes `{"name":` → `400`, `Error == "Invalid request body"`, raw body has no
        `validation_result` member.
      - `TestHandleCreateFlowExistingIDIsConflict` — POST twice with the same `id`: second is `409`,
        `Error == "Flow already exists"`; `GET /flows/{id}` still returns the first record (version 1, first name).
      - `TestHandleUpdateFlowInvalidIsBadRequestWithResult` — create; PUT at the current version with a node
        lacking `component`: `400`, `Error == "Flow validation failed"`, one `node_component_required`; GET shows
        the stored record unchanged at version 1.
      - `TestHandleUpdateFlowMissingIsNotFound` — PUT a well-formed body to an ID never created: `404`,
        `Error == "Flow not found"`, no `validation_result` member.
      - `TestHandleValidateFlowStructuralInvalidIsOK` — POST `/validate` with a draft whose node lacks
        `component`: `200`; fresh `flowstore.ValidationResult` (or `flowengine.ValidationResult` — same type) with
        `Status == "errors"`, one `node_component_required`; raw `nodes` and `discovered_connections` are `[]`.
      - `TestHandleValidateFlowPatternErrorIsOK` — a draft with two `udp` input nodes both configured on port
        `14550` (a network-pattern conflict through the real `componentregistry`): `200`, `Status == "errors"`,
        exactly one `connection_pattern_error` issue; arrays non-nil. If two `udp` inputs do not conflict at the
        registry level, find the conflicting pair by reading `component/flowgraph` `validateNetworkPorts` and record
        the pair here.
      - `TestHandleDeleteFlowMustExist` — create; DELETE → `204`, `recorder.Body.Len() == 0`,
        `recorder.Header().Get("Content-Type") == ""`; DELETE again → `404`, `Error == "Flow not found"`; GET →
        `404`.
      - `TestHandlePublishComponentConfigsInvalidIsBadRequestWithResult` — create a Flow whose node's `component`
        is `no-such-component` (structurally valid, saves fine); POST publish: `400`,
        `Error == "Flow validation failed"`, `ValidationResult.Errors` contains an `unknown_component` issue;
        `configManager.GetConfig().Get().Components` is empty.
      - `TestHandlePublishComponentConfigsStructuralInvalidHasResult` — write raw JSON for a Flow whose node has
        no `component` straight into the `semstreams_flows` bucket (`natsClient.CreateKeyValueBucket` returns the
        existing bucket; `natsClient.NewKVStore(bucket).Put`); POST publish: `400`; raw body's `validation_result`
        is an object containing `node_component_required`, never `null`.
      - `TestHandleListFlowsFailureProjectionMatrix` — table: (i) request built with
        `context.WithDeadline(t.Context(), time.Now().Add(-time.Second))` → `504`,
        `Error == "Flow storage request timed out"`; (ii) request under an already-cancelled context → `503`,
        `Error == "Flow storage temporarily unavailable"`; (iii) a `{not json` record `Put` into the bucket → `500`,
        `Error == "Internal server error"`. For every row: `Content-Type` `application/json`, no `validation_result`
        member, body does not contain `nats`, `invalid character`, or `flowstore`. Row (iii) is what proves the
        Manager's fatal class survives (a `503` there means the mapper re-classified). Rows (i)/(ii) use the same
        producers as Slice B's `TestManagerListPreservesPerKeyTransientFailure`; there is no `Close()` on
        `*natsclient.Client`, so a connection-shaped transient is covered by the unit matrix in 2.5.
      - Extend `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager`: the `409` at `:169-170`
        additionally decodes into a fresh `FlowErrorResponse` and asserts `Error == "Flow version conflict"` and no
        `validation_result` member.
      - Amend `TestHandleValidateFlow_InvalidJSON` (`:349`) to `Error == "Invalid request body"` exactly, and
        `TestHandleValidateFlow_IDMismatch` (`:396-398`) to `Error == "Flow ID does not match request path"` exactly
        and `NotContains` either ID — both decode into a fresh `FlowErrorResponse`.
- [ ] 2.5 `service/flow_error_test.go` (new, unit, `package service`) and `service/flow_surface_test.go`:
      - `TestFlowErrorMapperClassMatrix` — the delta's eight rows through `projectFlowError`; exact `==` on status
        and message; only the `ValidationError` row has a non-zero `ValidationResult`; no message contains the input
        error's text. Add a ninth row for the generic-invalid branch (`errs.WrapInvalid(errs.ErrInvalidConfig, …)`)
        asserting `400` and the message from 1.2 answer 1.
      - `TestFlowErrorResponseOmitsAbsentResult` — marshal a `FlowErrorResponse` without and with a result; the raw
        JSON has no `validation_result` key in the first case and an object (not `null`) in the second.
      - `TestSchemaFromTypeTreatsOmitzeroAsOptional` — a struct with `json:"x,omitzero"` on a struct-typed field:
        `x` in `properties`, not in `required`, no `anyOf`.
      - `TestFlowOpenAPIErrorAndValidationSchemas` — `ResponseTypes` carries `flowstore.ValidationResult{}`,
        `flowstore.ValidationIssue{}`, `FlowErrorResponse{}`; `SchemaFromType` required sets exactly as the delta
        states; `FlowErrorResponse.properties.validation_result` has no `anyOf`; each operation/status cell listed
        in the delta has the stated `SchemaRef`; the publish `500` `SchemaRefs` are exactly
        `[#/components/schemas/FlowErrorResponse, #/components/schemas/publishComponentConfigsResponse]`. Until 3.5
        the `service` test binary does not compile (`undefined: FlowErrorResponse`) — that is the baseline capture
        for 2.4 and 2.5 together, as in Slices A and B.
- [ ] 2.6 `processor/agentic-tools/executors/flows_integration_test.go` (`//go:build integration`):
      `TestFlowExecutorDeleteMissingIsError` — the Slice B fixture (`RegisterBuiltins` with `skipAllBut("flows")`);
      execute `delete_flow` with `flow_id: "never-saved"`: `execErr == nil`, `result.Error != ""`, `result.Content`
      does not contain `deleted`. Also assert `registry.GetTool("delete_flow") != nil`.
- [ ] 2.7 `cmd/openapi-generator/openapi_builder_test.go` (new or extended, unit): `TestOpenAPIBuilderRendersOneOfResponse`
      — a `service.OpenAPISpec` with one operation whose `500` names two `SchemaRefs`; build; assert the response's
      `application/json` schema has `OneOf` of exactly those two `$ref`s and no top-level `$ref`; run
      `validateOpenAPIRefs` with both schemas registered → nil, with one missing → error naming it.
- [ ] 2.8 Compile-time alias guard in `engine`: `var _ flowstore.ValidationResult = ValidationResult{}` (and the
      other five) in `engine/engine_test.go`, so a future de-aliasing does not compile. Grep proof after 3.4:
      `grep -rn '"github.com/c360studio/semstreams/engine"' --include='*.go' .` → `service/flow_service.go` only.
- [ ] 2.9 RED capture on baseline code (§2 tests only; production untouched), recorded here verbatim (package + test
      name + failing assertion, `--- FAIL` lines; NATS `INFO` lines elided). Expected: `flowstore` unit tests fail to
      compile until the types/constants exist (record the `undefined:` lines as the RED for 2.1); `flowstore`
      integration: Create/Update return an error with no `ValidationError` in the chain (`errors.As` false); Delete
      of a missing key returns nil; `engine`: build failure on the aliases/constants, then after 3.1 the pattern test
      fails with a non-nil error and the arrays test fails on nil `nodes`; `service`: build failure
      (`undefined: FlowErrorResponse`); `executors`: `result.Error == ""` for a missing Flow; generator: `OneOf`
      empty. A `[no tests to run]` line means the tag or `-run` is wrong — record it as a broken invocation and fix
      the invocation, never the expectation.

  ```
  go test -race -count=1 -v -run 'TestFlowValidate' ./flowstore/
  go test -race -tags=integration -count=1 -v -run 'TestManagerCreateAndUpdateReturnStructuralResult|TestManagerDeleteMissingReportsTypedAbsence' ./flowstore/
  go test -race -count=1 -v -run 'TestValidationResultArraysAreNonNull|TestEngineValidationTypesAreFlowstoreAliases|TestValidationIssueTypesAreTheClosedVocabulary|TestValidatorPatternErrorBecomesFinding|TestEngineValidateFlowDefinitionStructuralInvalidIsResult' ./engine/
  go test -race -tags=integration -count=1 -v -run 'TestHandle(Create|Update|Validate|Delete|List|Publish)Flow|TestHandlePublishComponentConfigs|TestFlowCRUDDoesNotPublish' ./service/
  go test -race -count=1 -v -run 'TestFlowErrorMapperClassMatrix|TestFlowErrorResponseOmitsAbsentResult|TestSchemaFromTypeTreatsOmitzeroAsOptional|TestFlowOpenAPIErrorAndValidationSchemas' ./service/
  go test -race -tags=integration -count=1 -v -run 'TestFlowExecutorDeleteMissingIsError' ./processor/agentic-tools/executors/
  go test -race -count=1 -v -run 'TestOpenAPIBuilderRendersOneOfResponse' ./cmd/openapi-generator/
  ```

## 3. GREEN — implement Slice C

- [ ] 3.1 `flowstore/validation.go` (new): `ValidationResult`, `ValidationIssue` (`Suggestions []string
      json:"suggestions"` — no `omitempty`; `PortName` keeps `omitempty`), `ValidatedNode`, `ValidatedPort`,
      `DiscoveredConnection` (moved verbatim from `engine/validator.go:36-84`, JSON tags unchanged); the twenty
      exported type constants with a one-line doc each; `ValidationError{Result *ValidationResult}` with `Error()`
      enumerating `type(component_name): message` for every error issue; `newValidationResult()` allocating all four
      slices; an unexported identity helper (`flowIdentity(f)`, `nodeIdentity(n, i)`, `connectionIdentity(c, i)`) —
      ONE home for the identity rule, used by `Validate` and by the engine. Record the file's line ranges here.
- [ ] 3.2 `flowstore/flow.go` `Validate`: build a result through `newValidationResult`, append one issue per fault in
      the delta's order, and return `errs.WrapInvalid(&ValidationError{Result: r}, "flowstore", "Validate",
      "validation failed")` when any error issue exists, else nil. `errors.As` reaches the `*ValidationError` through
      `ClassifiedError.Unwrap` → `fmt.Errorf` `%w` (`pkg/errs/errs.go:121-124,398`). The thirteen `WrapInvalid`
      sites are gone; `grep -c 'WrapInvalid' flowstore/flow.go` → 1.
- [ ] 3.3 `flowstore/manager.go`: in `Create` and `Update`, keep the nil guard, move `flow.Validate()` above the
      empty-ID guard and delete that guard (Validate now reports `flow_id_required`); `Delete`: `entry, err :=
      s.kvStore.Get(ctx, id)` → `errs.WrapTransient(err, "flowstore", "Delete", "read before delete")` on error (the
      `ErrKVKeyNotFound` sentinel survives the wrap; the mapper checks it before the class), then the existing
      `s.kvStore.Delete`; doc comment states must-exist, the read-then-delete shape, and that no fence is promised.
      `Manager` retains no `context.Context`; the one pre-existing `context.Background()` at `:58` is out of scope
      and untouched.
- [ ] 3.4 `engine/validator.go`, `engine/engine.go`: replace the five type definitions with aliases; alias
      `ValidationError`; emit every type through the `flowstore` constants (`grep -n '"empty_flow"\|"graph_build_error"\|
      "unknown_component"\|"disconnected_node"\|"orphaned_port"\|"interface_mismatch"\|"missing_interface"'
      engine/*.go` → 0); build results through `flowstore.newValidationResult`-equivalent (exported constructor or
      allocate all four in `ValidateFlow`); allocate `Suggestions` at every emission site; `empty_flow` and the two
      registry-level `graph_build_error` sites carry `flowstore` Flow identity; `:137-141` becomes: append one
      `connection_pattern_error` issue (Flow identity; `err.Error()` as message; non-nil suggestions), set `Status =
      "errors"`, extract node ports for the UI as the build-error path does, and `return result, nil`;
      `ValidateFlowDefinition`: on `flow.Validate()` error, `errors.As` the `*ValidationError` and `return ve.Result,
      nil` (an error without a result is still returned as an error). `Compile` unchanged.
- [ ] 3.5 `service/flow_service.go`: `FlowErrorResponse{Error string json:"error"; ValidationResult
      flowstore.ValidationResult json:"validation_result,omitzero"}` with a doc comment (why a value, why
      `omitzero`); the nine message constants in one block; `projectFlowError(err error) (int, FlowErrorResponse)` in
      the delta's order — with the generic-invalid message from 1.2 answer 1; `(fs *FlowService) writeFlowError(w,
      err)` that logs the original error (`fs.logger.Error("flow request failed", "error", err)`) and writes the
      projection; `writeFlowMessage(w, status, msg)` for the two fixed 400s. `service/schema.go:107`: treat
      `omitzero` like `omitempty`.
- [ ] 3.6 Handlers: Create/Update decode failure → `writeFlowMessage(400, "Invalid request body")`; Update mismatch →
      `writeFlowMessage(400, "Flow ID does not match request path")`; every Manager error in Create/Update/Delete/List
      → `writeFlowError`; Delete success unchanged (`WriteHeader(204)`, no header set); Validate: decode failure and
      mismatch → the two fixed 400s, `ValidateFlowDefinition` error → `writeFlowError`, pre-read untouched;
      Publish: pre-read untouched; `configs, validation, err := Compile(flow)`: `err != nil && validation != nil` →
      `400 FlowErrorResponse{Error: "Flow validation failed", ValidationResult: *validation}` (1.2 answer 2 if it
      departs); `err != nil && validation == nil` → `writeFlowError`; persistence path untouched. `writeJSONError`
      stays for the three runtime handlers (Slice D retires it): `grep -n 'writeJSONError' service/flow_service.go`
      → only its definition.
- [ ] 3.7 OpenAPI: `ResponseTypes` gains `flowstore.ValidationResult{}`, `flowstore.ValidationIssue{}`,
      `FlowErrorResponse{}`; `service/openapi_types.go` `ResponseSpec` gains `SchemaRefs []string
      json:"schema_refs,omitempty"` (doc: rendered as `oneOf`; mutually exclusive with `SchemaRef`);
      `cmd/openapi-generator/openapi_builder.go` renders `len(SchemaRefs) > 1` as `SchemaRef{OneOf: …}`; the rows
      exactly as the delta's OpenAPI requirement lists them, descriptions taken from the message table.
- [ ] 3.8 All §2 commands green with `-v` (an `ok` alone cannot distinguish a green suite from a `-run` that matched
      nothing; `grep -c 'no tests to run'` → 0), then `go test -race -count=1 ./flowstore/... ./engine/... ./service/...
      ./processor/agentic-tools/... ./cmd/openapi-generator/...` and `go test -race -tags=integration -p 2 -count=1
      ./flowstore/... ./service/... ./processor/agentic-tools/executors/...`. Record the output shape here. Commit
      GREEN before §4.

## 4. Forced omissions — each mechanism must be load-bearing

Commit §3 first. For each mutation: apply, print `[applied]`, run the named test, record the `--- FAIL` line
verbatim, restore with `cp` from a pre-mutation copy and confirm `shasum` equals the committed file (no git
checkout / stash / restore of any kind). M1–M5 are the design's five (`:205-206`); M6–M12 are one per new mechanism.

- [ ] 4.1 M1 — opaque Create restored (`handleCreateFlow` writes today's fixed-text `500` for every Manager error):
      `TestHandleCreateFlowInvalidIsBadRequestWithResult` and `TestHandleCreateFlowExistingIDIsConflict` MUST fail.
- [ ] 4.2 M2 — fail-fast validation restored (`Validate` returns after the first fault):
      `TestFlowValidateAggregatesStructuralIssuesInOrder` MUST fail on the issue count.
- [ ] 4.3 M3 — the pattern error is returned again (`return nil, errs.WrapInvalid(…)` at the former `:140`):
      `TestValidatorPatternErrorBecomesFinding` and `TestHandleValidateFlowPatternErrorIsOK` MUST fail.
- [ ] 4.4 M4 — nil arrays: (a) `newValidationResult` leaves `Nodes`/`DiscoveredConnections` nil →
      `TestValidationResultArraysAreNonNull` and `TestHandleValidateFlowStructuralInvalidIsOK` (raw `[]`) MUST fail;
      (b) `Suggestions` gets `omitempty` back and one emission site stops allocating → `TestValidationResultArraysAreNonNull`
      MUST fail.
- [ ] 4.5 M5 — string conflict: `projectFlowError` decides `409` by `strings.Contains(err.Error(), "conflict")` and the
      Manager's conflict message drops the word: the `409` assertion in
      `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager` and the conflict row of
      `TestFlowErrorMapperClassMatrix` MUST fail.
- [ ] 4.6 M6 — mapper order: move the `ErrKVKeyNotFound` check below the transient check:
      `TestHandleUpdateFlowMissingIsNotFound` and `TestHandleDeleteFlowMustExist` (second delete → `503`) MUST fail.
- [ ] 4.7 M7 — mapper deadline: delete the `context.DeadlineExceeded` branch: row (i) of
      `TestHandleListFlowsFailureProjectionMatrix` and the deadline row of `TestFlowErrorMapperClassMatrix` MUST
      fail with `503`.
- [ ] 4.8 M8 — structural converter drops the result: `Validate` returns a `WrapInvalid` without the
      `*ValidationError`: `TestFlowValidateErrorCarriesResultAndIsInvalid`,
      `TestManagerCreateAndUpdateReturnStructuralResult`, and `TestEngineValidateFlowDefinitionStructuralInvalidIsResult`
      MUST fail.
- [ ] 4.9 M9 — response builder: `FlowErrorResponse.ValidationResult` becomes a pointer with `omitempty`:
      `TestFlowOpenAPIErrorAndValidationSchemas` (an `anyOf` appears) MUST fail; and remove `omitzero` from the value
      field instead: `TestFlowErrorResponseOmitsAbsentResult` and the "no `validation_result` member" assertions of
      `TestHandleUpdateFlowMissingIsNotFound` MUST fail.
- [ ] 4.10 M10 — must-exist read removed from `Manager.Delete`: `TestManagerDeleteMissingReportsTypedAbsence`,
      `TestHandleDeleteFlowMustExist` (second delete → `204`), and `TestFlowExecutorDeleteMissingIsError` MUST fail.
- [ ] 4.11 M11 — publication drops the result on 400 (`FlowErrorResponse{Error: …}` only):
      `TestHandlePublishComponentConfigsInvalidIsBadRequestWithResult` and
      `TestHandlePublishComponentConfigsStructuralInvalidHasResult` MUST fail.
- [ ] 4.12 M12 — generator: (a) `schema.go` stops recognising `omitzero` → `TestSchemaFromTypeTreatsOmitzeroAsOptional`
      and the `FlowErrorResponse` required-set assertion MUST fail; (b) the builder ignores `SchemaRefs` →
      `TestOpenAPIBuilderRendersOneOfResponse` MUST fail.
- [ ] 4.13 Post-restore: `shasum` of every mutated file equals its committed hash; `git status --porcelain` empty; the
      3.8 commands green again; a subtest whose assertions also pass under its mutation is recorded here as a finding
      and strengthened before §5 (Slice B 3.7 precedent).

## 5. Schema regeneration — Slice C rows only

- [ ] 5.1 `task schema:generate`; `git diff --stat schemas/ specs/openapi.v3.yaml` shows only: the new
      `components.schemas.ValidationResult`, `ValidationIssue`, `FlowErrorResponse` entries (required sets as the
      delta states; `FlowErrorResponse.validation_result` an inline object with no `anyOf`; `ValidationIssue.suggestions`
      required); the operation rows listed in the delta; the publish `500` `oneOf`. No row from Slice D
      (`GET /flows/{id}` and the three observations are untouched), nothing under `schemas/`. Commit the delta.
- [ ] 5.2 Regenerate once more; `task schema:check-changes` exits 0 — no drift; `git status --porcelain` empty.
- [ ] 5.3 `go test -count=1 -v -run 'TestCommittedOpenAPISpecValid|TestOpenAPISchemaReferences' ./test/contract/...`
      names both tests PASS.

## 6. Standard gates — record each command and its result

- [ ] 6.1 `task lint` — 0 warnings (revive warnings fail CI); `git status --porcelain` empty afterwards.
- [ ] 6.2 `go test -race ./...` — `grep -c '^FAIL'` → 0 and `grep -c '^--- FAIL'` → 0; record package counts.
- [ ] 6.3 `go test -race -tags=integration -p 2 -count=1 ./...` — same greps → 0; same package counts as 6.2
      (Docker required; one agent at a time on a shared host; CI is the arbiter under contention).
- [ ] 6.4 `task build`, plus `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o
      semstreams-linux-amd64 ./cmd/semstreams` written outside the tree.
- [ ] 6.5 `go vet -tags=integration ./...` clean.
- [ ] 6.6 `openspec validate flow-invalid-handling-contract --strict` — pass.
- [ ] 6.7 BREAKING per 1.2 answer 3. If BREAKING: the commit subject carries `!` (or a `BREAKING CHANGE:` footer
      naming must-exist DELETE and the status projection) AND either `flow-authoring-http-contract` is authored under
      `test/e2e/scenarios/` and `task e2e:core` is recorded green here with the scenario's assertion count, or the
      owner's waiver comment URL is recorded here with #1087 named as the coverage gap. If not BREAKING: record the
      ruling URL and the grounding the owner gave.

## 7. Review and archive (inside the landing PR; the `AGENTS.md:63-68` Land order)

- [ ] 7.1 `semstreams-reviewer` on the GREEN + §4 + §5 head: verdict, every finding and its disposition (FIXED /
      FILED #n / ruling) recorded here. Findings on unused paths are FILED, not fixed.
- [ ] 7.2 Owner-run Codex round where the owner asks for it: verdict and dispositions recorded here; each fix
      re-enters 7.1 and re-runs the focused commands of 3.8 with `-v`.
- [ ] 7.3 `conformance.md`: replace every `__` placeholder with the measured `file:line` at the head that carries the
      last `.go` or delta change; fill R1's PR number; fill C19's assertion count; move any 1.2 answer that departs
      from the delta into DEVIATION with its comment URL. Maintained as part of every commit that moves a line, not
      at the end.
- [ ] 7.4 Reconcile: every scenario in `specs/flow-authoring/spec.md` (ADDED and MODIFIED) names a test that exists
      and is green in 6.2/6.3; table of scenario → test → location recorded here. Any `[~]` in this file is ALSO
      written into the delta before archiving.
- [ ] 7.5 `openspec archive flow-invalid-handling-contract` with the spec sync as the final content commit; the
      narrow reviewer check of the archive/spec sync follows as a PR comment; then undraft. A correction after
      archive re-enters 7.4 and 7.1. The PR body is a published layer: re-read it at undraft and correct any claim
      the branch no longer supports.

## 8. Not in scope (recorded so the archiver does not infer completion)

- Slice D (#1060): the six Get projections and `TestFlowOpenAPIResponseMatrix` (54 cells); the publication and
  Validate pre-read `503/504` rows.
- `flow-authoring-http-contract` and the other #1087 scenarios, unless 6.7 pulls the first forward; semstreams-ui
  candidate validation (owner-run tag gate).
- A `duplicate_node_name` structural type (proposal open question 2, recommended alternative); classifying
  `jetstream.ErrInvalidKey` invalid at the flowstore boundary; consolidating this mapper with
  `gateway/http/http.go:300-377`; retiring `writeJSONError` (Slice D); ordering of `ValidationResult.Nodes`.

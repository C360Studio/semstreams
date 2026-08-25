# Tasks — flow-update-server-owned-audit-cas

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was never recorded is
indistinguishable from one that was skipped. A deliberate not-done gets `[~]` AND a note in the spec delta. No task
here asserts a post-merge fact; the merge gate owns CI.

Premises (re-measured at `d219f0e9`; `git diff --stat 774c85dc..HEAD` over every Slice A file is empty):
`flowstore/manager.go:106-148` (Update), `:119-123` (Get drops the revision), `:126-130` (logical compare, message
carries `conflict`), `:132-135` (two separate `time.Now()`), `:143` (unfenced `Put`); `natsclient/kv.go:76-93`
(`Get` returns `KVEntry{Revision}`), `:222-239` (`Update` fence → `ErrKVRevisionMismatch`), `:652`;
`pkg/errs/errs.go:386-390` (`ErrRevisionMismatch`, code `revision_mismatch`), `:296-307,443` (`WrapInvalid` inherits
an inner code); `service/flow_service.go:221,225` (request `SchemaRef` Flow), `:245` (`RequestBodyTypes`),
`:274-292` (create decode), `:303-322` (update decode; `:314` substring `conflict`); `service/schema.go:106-109`
(`required` = not `omitempty`, not pointer); `service/flow_surface_test.go:43-67` (pins request refs to `Flow`).

## 1. Claim

- [x] 1.1 Branch `claude/gh1009-flowstore-update-timestamps` pushed; draft PR open with `Closes #1009` and
      `implemented-by: <model>` in the body; this change directory is its first commit.
      Draft PR **#1083** (`Closes #1009`); first commit `a7856040 docs(openspec): flow-update-server-owned-audit-cas
      — Slice A target state for #1009`.

## 2. RED — write the named tests first and capture the baseline failures

- [x] 2.1 `flowstore/manager_integration_test.go` (`//go:build integration`, real NATS via
      `natsclient.NewTestClient`): add `TestManagerUpdatePreservesStoredCreatedAt` (omitted `created_at`; also asserts
      `created_by` stored verbatim), `TestManagerUpdateIgnoresForgedCreatedAt`,
      `TestManagerUpdateTwoManagersExactlyOneWins` (two `NewManager` over one client/bucket; both read the same
      revision; an explicit pause/release barrier; assert exactly one nil, one typed conflict, version +1 once, stored content is
      the winner's, loser input deeply equal, winner input unchanged until commit — no sleeps),
      `TestManagerUpdateFailedWriteDoesNotMutateInput` (a deterministic failed persist: the losing side of the fence
      or a context cancelled at the pause seam; `reflect.DeepEqual` against a pre-call copy),
      `TestManagerUpdateSuccessMutatesInputAfterCommit` (input unchanged at the pause seam; after commit equals the
      stored record; `UpdatedAt == LastModified`).
- [x] 2.2 The pause seam is an unexported package-private seam on `Manager` (nil in production, set only from
      `package flowstore` tests). No exported field, option, or constructor parameter.
      `flowstore/manager.go:25-33` — `beforeUpdateWrite func(ctx context.Context)`, nil in production, invoked at
      `manager.go:155-157` immediately before the fenced write. Grep proof that nothing outside the package can
      reach it: `grep -rn "beforeUpdateWrite" . --include='*.go'` → 5 hits, all in `flowstore/manager.go` and
      `flowstore/manager_integration_test.go` (`package flowstore`). It was added in the RED commit rather than the
      GREEN one so the §2.1 tests fail behaviourally (a compile error is not the intended failure); baseline
      `Update` is otherwise untouched at RED.
- [x] 2.3 `service/flow_surface_test.go`: add `TestFlowUpdateRequestSchemaOmitsServerAuditFields` — uses
      `SchemaFromType` on `FlowUpdateRequest` and `FlowCreateRequest`, asserts absent timestamp/version properties,
      the exact required sets, that `RequestBodyTypes` carries both types, and the POST/PUT `SchemaRef`s. Update
      `TestFlowOpenAPIPreservesFlowCRUDWireSchema` so it pins POST → `FlowCreateRequest`, PUT → `FlowUpdateRequest`,
      validate → `Flow`, responses → `Flow`.
- [x] 2.4 `service/flow_service_test.go` `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager`:
      extend with (a) a PUT of the full `Flow` body carrying a forged `created_at` → 200 and `created_at` equal to
      the created value; (b) a re-PUT of the now-stale body → 409.
      Added at `service/flow_service_test.go:124-146`. Its RED is the same build failure as 2.3 — `package service`
      and `package service_test` compile into one test binary, so the undefined request types fail both.
- [x] 2.5 RED capture on baseline code, recorded here verbatim (package + test name + failing assertion):
      `go test -race -tags=integration -count=1 -run 'TestManagerUpdate' ./flowstore/` and
      `go test -race -count=1 -run 'TestFlow(UpdateRequestSchema|OpenAPIPreserves)' ./service/` (the schema test is
      expected to fail to compile until 3.3 — record that as its RED). Expected shape: created_at zero / forged
      value stored; both writers succeed; input mutated before the failed Put.

  RED at `9d23f2cd` + the §2 tests + the 2.2 seam only (production `Update` logic untouched):

  ```
  $ go test -race -tags=integration -count=1 -run 'TestManagerUpdate' ./flowstore/
  --- FAIL: TestManagerUpdatePreservesStoredCreatedAt (0.41s)
      manager_integration_test.go:150: stored created_at = 0001-01-01 00:00:00 +0000 UTC, want 2026-08-25 07:13:36.389156 -0500 CDT (restored from the stored record)
      manager_integration_test.go:153: returned created_at = 0001-01-01 00:00:00 +0000 UTC, want 2026-08-25 07:13:36.389156 -0500 CDT
  --- FAIL: TestManagerUpdateIgnoresForgedCreatedAt (0.25s)
      manager_integration_test.go:180: stored created_at = 1999-01-02 03:04:05 +0000 UTC, want 2026-08-25 07:13:36.625323 -0500 CDT (forged value must be ignored)
  --- FAIL: TestManagerUpdateTwoManagersExactlyOneWins (0.25s)
      manager_integration_test.go:224: A mutated its input before commit:
           got flowstore.Flow{... Version:2, ... UpdatedAt:time.Date(2026, time.August, 25, 7, 13, 36, 877272000, time.Local) ...}
          want flowstore.Flow{... Version:1, ... UpdatedAt:time.Date(2026, time.August, 25, 7, 13, 36, 875085000, time.Local) ...}
      manager_integration_test.go:227: B mutated its input before commit:
           got flowstore.Flow{... Version:2, ...}
          want flowstore.Flow{... Version:1, ...}
      manager_integration_test.go:262: want exactly one winner, got 2 (A=<nil> B=<nil>)
  --- FAIL: TestManagerUpdateFailedWriteDoesNotMutateInput (0.25s)
      --- FAIL: TestManagerUpdateFailedWriteDoesNotMutateInput/logical_version_mismatch (0.00s)
          manager_integration_test.go:298: error is not the typed conflict: flowstore.Update: conflict: flow was modified by another user failed: version mismatch: expected 1, got 0
      --- FAIL: TestManagerUpdateFailedWriteDoesNotMutateInput/lost_revision_fence (0.00s)
          manager_integration_test.go:333: update committed over a foreign write
      --- FAIL: TestManagerUpdateFailedWriteDoesNotMutateInput/decode_failure_on_a_corrupt_record (0.00s)
          manager_integration_test.go:378: corrupt stored JSON must be fatal: flowstore.Update: get current version failed: flowstore.Get: unmarshal flow failed: invalid character 'n' looking for beginning of object key string
  --- FAIL: TestManagerUpdateSuccessMutatesInputAfterCommit (0.25s)
      manager_integration_test.go:436: input mutated before commit:
           got flowstore.Flow{... Version:2, ... UpdatedAt:time.Date(2026, time.August, 25, 7, 13, 37, 378629000, time.Local) ...}
          want flowstore.Flow{... Version:1, ... UpdatedAt:time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC) ...}
      manager_integration_test.go:449: created_at: input=0001-01-01 00:00:00 +0000 UTC stored=0001-01-01 00:00:00 +0000 UTC, want 2026-08-25 07:13:37.37711 -0500 CDT
      manager_integration_test.go:452: input updated_at=2026-08-25 07:13:37.378629 -0500 CDT m=+1.323868168 last_modified=2026-08-25 07:13:37.378629 -0500 CDT m=+1.323868293, want one server instant
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	1.722s
  FAIL

  $ go test -race -count=1 -run 'TestFlow(UpdateRequestSchema|OpenAPIPreserves)' ./service/
  # github.com/c360studio/semstreams/service [github.com/c360studio/semstreams/service.test]
  service/flow_surface_test.go:111:48: undefined: FlowUpdateRequest
  service/flow_surface_test.go:127:48: undefined: FlowCreateRequest
  service/flow_surface_test.go:149:18: undefined: FlowCreateRequest
  service/flow_surface_test.go:150:18: undefined: FlowUpdateRequest
  FAIL	github.com/c360studio/semstreams/service [build failed]
  FAIL
  ```

  Notes on the capture: (a) the `:452` line is the two-`time.Now()` defect — the two values print alike but carry
  different monotonic readings (`m=+1.323868168` vs `m=+1.323868293`), and `time.Equal` compares the monotonic
  readings when both have them; (b) `logical_version_mismatch` proves the baseline conflict is untyped, reachable
  only by its message text; (c) the three immutability sub-cases that already held at baseline
  (`read failure on a missing key`, `marshal failure`, `structural validation failure`) are recorded as passing at
  RED — they guard the delta's full failure-path list, not a baseline defect; (d) `decode_failure_on_a_corrupt_record`
  is a RED against task 3.2's parenthetical, not against baseline "unchanged" behaviour — see the note under 3.2.

## 3. GREEN — implement Slice A

- [x] 3.1 `flowstore/manager.go` `Update`: validate → `s.kvStore.Get` (value + revision) → decode stored → logical
      compare → copy candidate → `candidate.CreatedAt = stored.CreatedAt`, `candidate.Version = stored.Version + 1`,
      one `now` for `UpdatedAt` and `LastModified`, `CreatedBy` from the request → marshal →
      `s.kvStore.Update(ctx, id, data, entry.Revision)` → on `natsclient.ErrKVRevisionMismatch` return the typed
      conflict → on success `*flow = candidate`. Every failure returns before any assignment to `*flow`.
      `flowstore/manager.go:132-187`. The sequence is exactly as written; `*flow = candidate` is the last statement
      before `return nil` (`:185`) and is the only assignment to `*flow` in the function.
- [x] 3.2 The typed conflict for BOTH the logical mismatch and the fence loss carries the existing ADR-060 code
      (e.g. `errs.WrapInvalid(errs.ClassifiedCode(errs.ErrorInvalid, errs.ErrRevisionMismatch.Code, cause),
      "flowstore", "Update", ...)`); assert `errors.Is(err, errs.ErrRevisionMismatch)` in the tests. No new exported
      sentinel. Non-conflict failure classifications (missing key → transient, corrupt JSON → fatal) are unchanged.
      One helper, `flowstore/manager.go:189-197` `versionConflict`, serves both sites (`:157` logical, `:180` fence);
      `errs.WrapInvalid` inherits the inner `ClassifiedCode`'s machine contract (`pkg/errs/errs.go:296-307,443`), so
      the wrapped error carries `Code == "revision_mismatch"` and `errors.Is(err, errs.ErrRevisionMismatch)` is true.
      Asserted in `TestManagerUpdateTwoManagersExactlyOneWins`,
      `TestManagerUpdateFailedWriteDoesNotMutateInput/{logical_version_mismatch,lost_revision_fence}` and
      `TestManagerDiagramCRUDAndVersioning`.
      **Measured correction to this task's parenthetical.** "corrupt JSON → fatal" is what the new code does
      (`:151` `WrapFatal`) but it is NOT unchanged: at baseline `Update` read through `Manager.Get`, whose inner
      `WrapFatal` (`manager.go:109`) was re-wrapped by `WrapTransient` at `manager.go:122`, and `errs.IsFatal` reads
      the OUTERMOST `*ClassifiedError` — so corrupt stored JSON surfaced as **transient** before this change. The RED
      run records that (`decode_failure_on_a_corrupt_record`). The parenthetical was implemented as written and the
      baseline mismatch is recorded here rather than silently resolved either way. Missing key stays transient
      (`:147`), as both readings agree.
- [x] 3.3 `service/`: add `FlowCreateRequest` and `FlowUpdateRequest` (HTTP boundary types; `omitempty` on the
      optional fields so `schema.go:107` derives the required sets) with a helper that builds a `flowstore.Flow`;
      decode POST/PUT into them; register both in `RequestBodyTypes`; point the POST/PUT `SchemaRef`s at them.
      Do not call `DisallowUnknownFields` — legacy full-`Flow` bodies must keep decoding.
      `service/flow_service.go:278-330` (both types plus their unexported `flow()` builders), decode at `:332-337`
      (POST) and `:362-368` (PUT), `RequestBodyTypes` at `:246-250`, `SchemaRef`s at `:222` (POST) and `:226` (PUT).
      `grep -n "DisallowUnknownFields" service/flow_service.go` → no match.
- [x] 3.4 `handleUpdateFlow`: replace `strings.Contains(err.Error(), "conflict")` with
      `errors.Is(err, errs.ErrRevisionMismatch)` → 409. Leave every other status and body as it is (Slice C owns the
      mapper and the exact messages).
      `service/flow_service.go:374` — `if errors.Is(err, errs.ErrRevisionMismatch)`. The 500 branch, both bodies and
      every other status are byte-identical to baseline; `grep -n 'strings.Contains' service/flow_service.go` no
      longer hits `handleUpdateFlow` (remaining hits are the List "no keys found" branches at `:128`/`:269` and the
      metric-name helpers at `:445`/`:460`, all Slice B/other work).
- [x] 3.5 All tests from §2 green: the two focused commands from 2.5, then `go test -race ./flowstore/... ./service/...`
      and `go test -race -tags=integration -p 2 -count=1 ./flowstore/... ./service/...`. Record output shape here.

  ```
  $ go test -race -tags=integration -count=1 -run 'TestManagerUpdate' ./flowstore/
  ok  	github.com/c360studio/semstreams/flowstore	2.694s
  $ go test -race -count=1 -run 'TestFlow(UpdateRequestSchema|OpenAPIPreserves)' ./service/
  ok  	github.com/c360studio/semstreams/service	1.519s
  $ go test -race -tags=integration -count=1 -run 'TestFlowCRUDDoesNotPublish' ./service/
  ok  	github.com/c360studio/semstreams/service	2.237s
  $ go test -race -count=1 ./flowstore/... ./service/...
  ok  	github.com/c360studio/semstreams/flowstore	1.289s
  ok  	github.com/c360studio/semstreams/service	6.484s
  $ go test -race -tags=integration -p 2 -count=1 ./flowstore/... ./service/...
  ok  	github.com/c360studio/semstreams/flowstore	2.983s
  ok  	github.com/c360studio/semstreams/service	33.206s
  ```

## 4. Forced omissions — the fence and the copy must each be load-bearing

Commit §3 first. For each mutation: apply, print `[applied]`, run the named test, record the FAIL line verbatim,
restore with `cp` + checksum (no git checkout/stash of any kind).

- [x] 4.1 M1 — replace the fenced `kvStore.Update` with `kvStore.Put`: `TestManagerUpdateTwoManagersExactlyOneWins`
      MUST fail (both succeed / version advanced twice / loser content stored).

  ```
  [applied] M1 — fenced kvStore.Update replaced by kvStore.Put
  $ go test -race -tags=integration -count=1 -run 'TestManagerUpdateTwoManagersExactlyOneWins' ./flowstore/
  --- FAIL: TestManagerUpdateTwoManagersExactlyOneWins (0.42s)
      manager_integration_test.go:262: want exactly one winner, got 2 (A=<nil> B=<nil>)
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	0.788s
  ```
- [x] 4.2 M2 — remove copy-on-write (stamp the caller's `*Flow` in place before persisting):
      `TestManagerUpdateFailedWriteDoesNotMutateInput` MUST fail; the loser-unchanged assertion in
      `TestManagerUpdateTwoManagersExactlyOneWins` MUST fail.

  ```
  [applied] M2 — copy-on-write removed; caller's *Flow stamped in place
  $ go test -race -tags=integration -count=1 -run 'TestManagerUpdateFailedWriteDoesNotMutateInput|TestManagerUpdateTwoManagersExactlyOneWins' ./flowstore/
  --- FAIL: TestManagerUpdateTwoManagersExactlyOneWins (0.42s)
      manager_integration_test.go:224: A mutated its input before commit:
      manager_integration_test.go:227: B mutated its input before commit:
      manager_integration_test.go:265: loser input mutated:
  --- FAIL: TestManagerUpdateFailedWriteDoesNotMutateInput (0.25s)
      --- FAIL: TestManagerUpdateFailedWriteDoesNotMutateInput/lost_revision_fence (0.00s)
          manager_integration_test.go:339: input mutated by a failed write:
      --- FAIL: TestManagerUpdateFailedWriteDoesNotMutateInput/marshal_failure (0.00s)
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	1.003s
  ```
  Repeat-run stability of the barrier (no sleeps, so it should be deterministic):
  `go test -race -tags=integration -count=5 -run 'TestManagerUpdateTwoManagersExactlyOneWins|TestManagerUpdateSuccessMutatesInputAfterCommit' ./flowstore/`
  → `ok  	github.com/c360studio/semstreams/flowstore	4.031s`.

  (`marshal_failure` fails too: without the copy, the version/timestamp stamp lands on the caller's value before
  the marshal that rejects it.)
- [x] 4.3 M3 — drop the `CreatedAt` restore: `TestManagerUpdatePreservesStoredCreatedAt` and
      `TestManagerUpdateIgnoresForgedCreatedAt` MUST fail.

  ```
  [applied] M3 — CreatedAt no longer restored from the stored record
  $ go test -race -tags=integration -count=1 -run 'TestManagerUpdatePreservesStoredCreatedAt|TestManagerUpdateIgnoresForgedCreatedAt' ./flowstore/
  --- FAIL: TestManagerUpdatePreservesStoredCreatedAt (0.42s)
      manager_integration_test.go:150: stored created_at = 0001-01-01 00:00:00 +0000 UTC, want 2026-08-25 07:19:39.66714 -0500 CDT (restored from the stored record)
      manager_integration_test.go:153: returned created_at = 0001-01-01 00:00:00 +0000 UTC, want 2026-08-25 07:19:39.66714 -0500 CDT
  --- FAIL: TestManagerUpdateIgnoresForgedCreatedAt (0.24s)
      manager_integration_test.go:180: stored created_at = 1999-01-02 03:04:05 +0000 UTC, want 2026-08-25 07:19:39.912526 -0500 CDT (forged value must be ignored)
  FAIL
  FAIL	github.com/c360studio/semstreams/flowstore	0.995s
  ```
- [x] 4.4 M4 — restore the substring branch in `handleUpdateFlow` and make the conflict message omit `conflict`:
      the 409 assertion in 2.4(b) MUST fail (proves classification, not text, decides the status).

  ```
  [applied] M4 — handleUpdateFlow back on strings.Contains(err, "conflict"); conflict message no longer contains the word
  [applied] M4 (cont) — errs import blanked so the mutation compiles
  $ go test -race -tags=integration -count=1 -run 'TestFlowCRUDDoesNotPublish' ./service/
  --- FAIL: TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager (0.28s)
          	Error Trace:	service/flow_service_test.go:146
          	Error:      	Not equal:
          	            	expected: 409
          	            	actual  : 500
  FAIL
  FAIL	github.com/c360studio/semstreams/service	1.221s
  ```
- [x] 4.5 Post-restore checksum matches the committed file for every mutated file; the full §3.5 commands are green
      again after restoration. Restored with `cp` from the pre-mutation copies (no git checkout/stash at any point).

  ```
  $ shasum flowstore/manager.go service/flow_service.go     # after every restore, == the pre-mutation capture
  edf8372169b66129f2c8262762d60541f4f88012  flowstore/manager.go
  8e301938c65777f5ac911a143ef99f6ce5a8bcaf  service/flow_service.go
  $ git status --porcelain                                  # (empty)
  $ go test -race -count=1 ./flowstore/... ./service/...
  ok  	github.com/c360studio/semstreams/flowstore	1.236s
  ok  	github.com/c360studio/semstreams/service	6.427s
  $ go test -race -tags=integration -p 2 -count=1 ./flowstore/... ./service/...
  ok  	github.com/c360studio/semstreams/flowstore	2.887s
  ok  	github.com/c360studio/semstreams/service	33.029s
  ```

## 5. Schema regeneration — Slice A rows only

- [x] 5.1 `task schema:generate`; `git diff --stat schemas/ specs/openapi.v3.yaml` shows only the
      `FlowCreateRequest`/`FlowUpdateRequest` schemas and the two request-body refs (no rows from Slices B–D).
      Commit the delta. Committed as `de6f23fa`.

  ```
  $ git diff --stat schemas/ specs/openapi.v3.yaml
   specs/openapi.v3.yaml | 146 +++++++++++++++++++++++++++++++++++++++++++++++++-
   1 file changed, 144 insertions(+), 2 deletions(-)
  ```
  Content of the delta: `paths./flows.post.requestBody` `$ref` Flow → FlowCreateRequest;
  `paths./flows/{id}.put.requestBody` `$ref` Flow → FlowUpdateRequest; and the two new `components.schemas` entries
  (`FlowCreateRequest` required `name,nodes,connections`; `FlowUpdateRequest` required
  `id,version,name,nodes,connections`; neither declares `created_at`, `updated_at` or `last_modified`, and
  `FlowCreateRequest` declares no `version`). No `schemas/` file changed and no other path or schema moved.
- [x] 5.2 Regenerate once more; `task schema:check-changes` (i.e. `git diff --exit-code schemas/
      specs/openapi.v3.yaml`) passes — no drift. Ran `task schema:generate` again after the commit;
      `task schema:check-changes` exited 0 with no diff output, and `git status --porcelain` was empty.
- [x] 5.3 `go test ./test/contract/...` green (`TestCommittedOpenAPISpecValid`, `TestOpenAPISchemaReferences`).

  ```
  $ go test ./test/contract/...
  ok  	github.com/c360studio/semstreams/test/contract	3.033s
  $ go test -v -count=1 -run 'TestCommittedOpenAPISpecValid|TestOpenAPISchemaReferences' ./test/contract/...
  --- PASS: TestCommittedOpenAPISpecValid (0.00s)
  --- PASS: TestOpenAPISchemaReferences (0.00s)
  ok  	github.com/c360studio/semstreams/test/contract	0.383s
  ```

## 6. Standard gates — record each command and its result

- [x] 6.1 `task lint` — 0 warnings (revive warnings fail CI). `go vet ./...`, `go fmt ./...`,
      `go tool revive -config revive.toml -formatter friendly ./...`, the fixed-port guard and the request guard all
      ran; revive printed no findings; `ok github.com/c360studio/semstreams/test/natsclient 0.515s`;
      `git status --porcelain` empty afterwards (nothing reformatted).
- [x] 6.2 `go test -race ./...` — no `^FAIL` lines. exit=0; `grep -E "^FAIL|^--- FAIL"` over the full output
      returned nothing.
- [x] 6.3 `go test -race -tags=integration -p 2 -count=1 ./...` — no `^FAIL` lines (Docker required). exit=0,
      543s wall; `grep -E "^FAIL|^--- FAIL"` over the full output returned nothing.
- [x] 6.4 `task build` — Linux cross-compile green. `task build` → `Built bin/semstreams`; and the exact CI
      invocation `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o semstreams-linux-amd64
      ./cmd/semstreams` (`.github/workflows/ci.yml:137-141`) produced a 29671586-byte linux/amd64 binary.
- [x] 6.5 `go vet -tags=integration ./...` clean (tagged tests compile). No output.
- [x] 6.6 `openspec validate flow-update-server-owned-audit-cas --strict` — pass.
      `Change 'flow-update-server-owned-audit-cas' is valid`

## 7. Review and archive (inside the landing PR)

- [ ] 7.1 `semstreams-reviewer` review requested on the undrafted PR; verdict and every finding's disposition recorded
      as PR comments (or `conformance.md` in this change). A finding on an unused path is filed, not fixed here.
- [x] 7.2 Reconcile: every scenario in `specs/flow-authoring/spec.md` names the test that verifies it and that test
      exists and is green; any deliberate not-done is `[~]` here AND noted in the delta. No `[~]` — every scenario
      is verified by a test that exists and passed in the 6.2/6.3 runs. Mapping:

  | Scenario | Test | Location |
  |---|---|---|
  | omitted created_at restored | `TestManagerUpdatePreservesStoredCreatedAt` | `flowstore/manager_integration_test.go:124` |
  | supplied created_at ignored | `TestManagerUpdateIgnoresForgedCreatedAt` | `:161` |
  | update timestamps are one server instant | `TestManagerUpdateSuccessMutatesInputAfterCommit` | `:413` |
  | stale logical version rejected without a write | `TestManagerDiagramCRUDAndVersioning` + `TestManagerUpdateFailedWriteDoesNotMutateInput` | `:18`, `:280` |
  | created_by caller-preserved | `TestManagerUpdatePreservesStoredCreatedAt` | `:124` |
  | two Managers, exactly one wins | `TestManagerUpdateTwoManagersExactlyOneWins` | `:188` |
  | logical + revision mismatch are one typed conflict | `TestManagerUpdateTwoManagersExactlyOneWins` + `TestManagerDiagramCRUDAndVersioning` | `:188`, `:18` |
  | HTTP 409 by classification | `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager` | `service/flow_service_test.go:91` |
  | failed write does not mutate the input | `TestManagerUpdateFailedWriteDoesNotMutateInput` | `flowstore/manager_integration_test.go:280` |
  | loser keeps its input | `TestManagerUpdateTwoManagersExactlyOneWins` | `:188` |
  | success mutates the input only after commit | `TestManagerUpdateSuccessMutatesInputAfterCommit` | `:413` |
  | update request schema omits audit fields | `TestFlowUpdateRequestSchemaOmitsServerAuditFields` | `service/flow_surface_test.go:79` |
  | create request schema omits version/timestamps | `TestFlowUpdateRequestSchemaOmitsServerAuditFields` | `service/flow_surface_test.go:79` |
  | legacy full-Flow update body decodes | `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager` | `service/flow_service_test.go:91` |
  | Flow response schema unchanged | `TestFlowOpenAPIPreservesFlowCRUDWireSchema` | `service/flow_surface_test.go:43` |
- [ ] 7.3 Last commit of the landing PR: `openspec archive flow-update-server-owned-audit-cas` with the spec sync,
      reviewed alongside the code.

## 8. Not in scope (recorded so the archiver does not infer completion)

- Slices B (#1010), C (#1008 vocabulary, exact messages, must-exist DELETE, 404 on missing Update target), D (Get
  projections); semstreams-ui candidate validation (owner-run tag gate, not a task here); repair of records already
  stored with a zero `created_at`.

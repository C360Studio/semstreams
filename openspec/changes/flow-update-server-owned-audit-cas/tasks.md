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

- [ ] 3.1 `flowstore/manager.go` `Update`: validate → `s.kvStore.Get` (value + revision) → decode stored → logical
      compare → copy candidate → `candidate.CreatedAt = stored.CreatedAt`, `candidate.Version = stored.Version + 1`,
      one `now` for `UpdatedAt` and `LastModified`, `CreatedBy` from the request → marshal →
      `s.kvStore.Update(ctx, id, data, entry.Revision)` → on `natsclient.ErrKVRevisionMismatch` return the typed
      conflict → on success `*flow = candidate`. Every failure returns before any assignment to `*flow`.
- [ ] 3.2 The typed conflict for BOTH the logical mismatch and the fence loss carries the existing ADR-060 code
      (e.g. `errs.WrapInvalid(errs.ClassifiedCode(errs.ErrorInvalid, errs.ErrRevisionMismatch.Code, cause),
      "flowstore", "Update", ...)`); assert `errors.Is(err, errs.ErrRevisionMismatch)` in the tests. No new exported
      sentinel. Non-conflict failure classifications (missing key → transient, corrupt JSON → fatal) are unchanged.
- [ ] 3.3 `service/`: add `FlowCreateRequest` and `FlowUpdateRequest` (HTTP boundary types; `omitempty` on the
      optional fields so `schema.go:107` derives the required sets) with a helper that builds a `flowstore.Flow`;
      decode POST/PUT into them; register both in `RequestBodyTypes`; point the POST/PUT `SchemaRef`s at them.
      Do not call `DisallowUnknownFields` — legacy full-`Flow` bodies must keep decoding.
- [ ] 3.4 `handleUpdateFlow`: replace `strings.Contains(err.Error(), "conflict")` with
      `errors.Is(err, errs.ErrRevisionMismatch)` → 409. Leave every other status and body as it is (Slice C owns the
      mapper and the exact messages).
- [ ] 3.5 All tests from §2 green: the two focused commands from 2.5, then `go test -race ./flowstore/... ./service/...`
      and `go test -race -tags=integration -p 2 -count=1 ./flowstore/... ./service/...`. Record output shape here.

## 4. Forced omissions — the fence and the copy must each be load-bearing

Commit §3 first. For each mutation: apply, print `[applied]`, run the named test, record the FAIL line verbatim,
restore with `cp` + checksum (no git checkout/stash of any kind).

- [ ] 4.1 M1 — replace the fenced `kvStore.Update` with `kvStore.Put`: `TestManagerUpdateTwoManagersExactlyOneWins`
      MUST fail (both succeed / version advanced twice / loser content stored).
- [ ] 4.2 M2 — remove copy-on-write (stamp the caller's `*Flow` in place before persisting):
      `TestManagerUpdateFailedWriteDoesNotMutateInput` MUST fail; the loser-unchanged assertion in
      `TestManagerUpdateTwoManagersExactlyOneWins` MUST fail.
- [ ] 4.3 M3 — drop the `CreatedAt` restore: `TestManagerUpdatePreservesStoredCreatedAt` and
      `TestManagerUpdateIgnoresForgedCreatedAt` MUST fail.
- [ ] 4.4 M4 — restore the substring branch in `handleUpdateFlow` and make the conflict message omit `conflict`:
      the 409 assertion in 2.4(b) MUST fail (proves classification, not text, decides the status).
- [ ] 4.5 Post-restore checksum matches the committed file for every mutated file; the full §3.5 commands are green
      again after restoration.

## 5. Schema regeneration — Slice A rows only

- [ ] 5.1 `task schema:generate`; `git diff --stat schemas/ specs/openapi.v3.yaml` shows only the
      `FlowCreateRequest`/`FlowUpdateRequest` schemas and the two request-body refs (no rows from Slices B–D).
      Commit the delta.
- [ ] 5.2 Regenerate once more; `task schema:check-changes` (i.e. `git diff --exit-code schemas/
      specs/openapi.v3.yaml`) passes — no drift.
- [ ] 5.3 `go test ./test/contract/...` green (`TestCommittedOpenAPISpecValid`, `TestOpenAPISchemaReferences`).

## 6. Standard gates — record each command and its result

- [ ] 6.1 `task lint` — 0 warnings (revive warnings fail CI).
- [ ] 6.2 `go test -race ./...` — no `^FAIL` lines.
- [ ] 6.3 `go test -race -tags=integration -p 2 -count=1 ./...` — no `^FAIL` lines (Docker required).
- [ ] 6.4 `task build` — Linux cross-compile green.
- [ ] 6.5 `go vet -tags=integration ./...` clean (tagged tests compile).
- [ ] 6.6 `openspec validate flow-update-server-owned-audit-cas --strict` — pass.

## 7. Review and archive (inside the landing PR)

- [ ] 7.1 `semstreams-reviewer` review requested on the undrafted PR; verdict and every finding's disposition recorded
      as PR comments (or `conformance.md` in this change). A finding on an unused path is filed, not fixed here.
- [ ] 7.2 Reconcile: every scenario in `specs/flow-authoring/spec.md` names the test that verifies it and that test
      exists and is green; any deliberate not-done is `[~]` here AND noted in the delta.
- [ ] 7.3 Last commit of the landing PR: `openspec archive flow-update-server-owned-audit-cas` with the spec sync,
      reviewed alongside the code.

## 8. Not in scope (recorded so the archiver does not infer completion)

- Slices B (#1010), C (#1008 vocabulary, exact messages, must-exist DELETE, 404 on missing Update target), D (Get
  projections); semstreams-ui candidate validation (owner-run tag gate, not a task here); repair of records already
  stored with a zero `created_at`.

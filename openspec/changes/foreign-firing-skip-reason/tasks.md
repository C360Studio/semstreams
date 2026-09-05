# Tasks — foreign-firing-skip-reason

**Amend a task line when the work HAPPENS, not only when it succeeds.** A `[~]` is a recorded decision and MUST also
be noted in the spec delta. No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing in any
OPEN task line as a live caveat; use "pause seam", "barrier", "abort", "does not compile", "MUST fail".

Premises measured on `main@5b7c3db3`: `processor/rule/actions.go:596-598` (the bool guard), `:634-651` (the
recorder hardcoding the reason), `processor/rule/cron_scheduler.go:650-656` (the entity-less cron context),
`pkg/types/entity_id_authority.go:36-39` (structural error returned before the authority comparison),
`openspec/specs/graph-ingest/spec.md:1043-1161` (the requirement restated in the delta).

## 1. Claim

- [x] 1.1 Draft PR opened with `Closes #1169` on `claude/gh1169-cron-skip-reason`, own worktree.

## 2. Spec delta

- [x] 2.1 MODIFIED `Framework-minted runtime state carries the deployment's own authority and never writes to an
      imported firing entity` — full restatement of all eight current scenarios; the counter paragraph names the
      two-token `reason` vocabulary; one scenario added for the cron/no-entity case, pinned by
      `TestPublishAgentSkipReasonSeparatesUnresolvableFromForeign`.

## 3. Code

- [x] 3.1 `processor/rule/actions.go`: `foreignFiringSkipReason` classifies by the classified error's code
      (`ErrorCodeEntityIDAuthorityInvalid` → `foreign_authority`, else `unresolvable_firing_entity`, nil → local);
      `foreignFiringEntity` wraps it; the recorder takes the reason for the label and the log field and picks the
      truthful message.
- [x] 3.2 `processor/rule/metrics.go`: Help string and doc comment state the two-value vocabulary.
- [x] 3.3 Move `capturedRecord` / `capturingHandler` / `withMessage` from the tagged
      `actions_run_scope_integration_test.go` into an untagged `log_capture_test.go` (same package, no change).

## 4. Tests

- [x] 4.1 `TestPublishAgentSkipReasonSeparatesUnresolvableFromForeign` (unit, untagged): cron context → the
      unresolvable label increments once, `foreign_authority` does not, one Info line with the unresolvable reason
      and message; a structurally invalid entity → same; a canonical import → `foreign_authority`; a local entity →
      neither, and `rule.task.spawned` is written. Cites the requirement with `// spec:`.
- [x] 4.2 Existing pins unchanged and green: the three tagged run-scope tests and
      `TestPublishAgentThroughExportedFullConstructorSkipsForeignSpawnedTask` (the empty-authority case still reads
      `foreign_authority`).
- [x] 4.3 Fails-without-fix, run against the committed state `d3f606b8` and restored by checkout: (A) the recorder
      hardcoding `foreign_authority` again, and (B) the classifier answering `foreign_authority` for every error —
      each turned 4.1's cron and malformed cases red on the `foreign_authority` assertion, the import and local cases
      stayed green.

## 5. Docs

- [x] 5.1 `docs/operations/migration-beta162-to-beta163.md` foreign-firing passage: says which label means what.

## 6. Gates

- [x] 6.1 `task lint` clean; `go test -race ./processor/rule/...` ok; the five tagged run-scope/foreign-firing pins
      under `-tags=integration` against real NATS ok (5 PASS, 0 SKIP); `openspec validate --strict` valid;
      `task spec:properties` 48/48; `task schema:generate` no drift; `task api:compat:report` at the baseline's 12
      with no entry from this change.
- [ ] 6.2 `semstreams-reviewer` pass; findings addressed.
- [ ] 6.3 Archive as the final content commit after review.

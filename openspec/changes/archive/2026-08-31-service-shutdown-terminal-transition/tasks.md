# Tasks — service-shutdown-terminal-transition

**Amend a task line when the work HAPPENS, not only when it succeeds.** A `[~]` is a recorded decision and MUST also
be noted in the spec delta. No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing in any
OPEN task line as a live caveat; use "pause seam", "barrier", "abort", "does not compile", "MUST fail".

Premises measured on `main@48b127ce`: `service/service_manager.go:925-932` (the conditional registry clear),
`service/base.go:22-28` and `:483-487` (nil-or-sentinel), `service/service_manager.go:888` (StopAll's tolerance),
`openspec/specs/service-shutdown/spec.md:16-92` (the two requirements restated here).

## 1. Claim

- [x] 1.1 Draft PR opened with `Closes #1214` on `claude/gh1214-service-shutdown-terminal`, own worktree.

## 2. Spec delta

- [x] 2.1 ADDED requirement: terminal StopAll success deregisters; failure retains for retry. Two scenarios.
- [x] 2.2 MODIFIED `Coordinated shutdown treats an already-stopped service as clean success` — full restatement of
      all six current scenarios; `Completed service is visited again` narrowed in body only, header unchanged.
- [x] 2.3 MODIFIED `A framework service Stop is idempotent on repeated invocation` — full restatement of all four
      current scenarios, requirement text and `Completed Stop is called again` widened to nil-or-sentinel,
      `Stop called twice returns nil the second time` narrowed to the `BaseService.Stop` default, one scenario
      added for StopAll's sentinel tolerance.
- [x] 2.4 Scenario-header sets diffed against the current spec: zero dropped, zero renamed.

## 3. Code

- [x] 3.1 Retire the `SPIKE FINDING` comment at `service/service_manager_prop_test.go:242-247`; replace with a
      `// spec:` citation of the new requirement. No assertion changes — the model already mirrors ruled behavior.

## 4. Verify

- [x] 4.1 `openspec validate --strict` clean for this change.
- [x] 4.2 The property test still passes, and still fails if the ruled behavior is mutated away — the citation must
      point at an assertion that can fail.
- [x] 4.3 `task lint`, unit suite, `task schema:generate` no drift.

## 5. Review and land

- [x] 5.1 `semstreams-reviewer` round (APPROVE, 3 MEDIUM + 3 NIT); all six addressed in `af84ed3c`,
      dispositions on the PR; three findings filed as #1218 / #1219 / #1220.
- [x] 5.2 Archive + spec sync as the LAST content commit, reviewed with the delta.
- [ ] 5.3 Undraft; PR body carries `implemented-by:` and `Closes #1214`.

## Deliberately not done

- **No production code change.** The delta states shipped behavior. Changing shutdown semantics is a separate
  decision the owner has not been asked for, and #1214's ruling was explicitly "intentional — spec, not code".
- **No new e2e tier.** Nothing here is BREAKING; no behavior moves.

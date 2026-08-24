# GH-1062 compact lifecycle lane inventory

## Checkpoint

- Historical defect snapshot: `35a64ee19ad86f14bd2a1fc6fe0b39984e169a35`, the exact baseline of the prior
  accepted surface inventory `docs/proposals/next-tag-test-gate-blockers-inventory.md`, SHA-256
  `1c8c5a6e99085c3f4c70306f3dfa58d85d5998dd2f42d7ed44893c61fd02b880`.
- Current candidate snapshot: tracked HEAD `89821a19019e1f137f9c9ca8d3a9fffb0d103862` plus the scoped uncommitted files
  listed below at their exact SHA-256 identities. References labeled historical or candidate do not mix snapshots.
- Prior lifecycle amendment SHA-256
  `82c02d41468988987d159cfee3b758b038c39151b13155cd10b393aa9be1f307` required nil abort Stop and is superseded
  target state, not inventory authority for the compact ruling.
- Owner direction on 2026-08-23: controlled shutdown retains strict clean drain; accepted-parent cancellation is a
  bounded abort and may return accurate terminal errors. Exact materialized target acceptance remains pending review.

## Contract and standard-suite surfaces

- Historical `35a64ee`: `component/lifecycle.go:43-57` requires caller-bounded terminal work but contains no explicit
  controlled/abort lane prose.
- Historical `35a64ee`: `component/lifecycle_test_suite.go:63-73` models the controlled lane correctly: Start authority
  remains live, bounded Stop returns nil, then the caller cancels Start.
- Historical `35a64ee`: `component/lifecycle_test_suite.go:75-84` cancels accepted Start before Stop and requires Stop
  to return nil.
- Candidate `component/lifecycle.go` SHA-256
  `da43b6e8ad962812f20b87fb6c71e2c296bc6760224d7604b3ad09c8b5922862` already contains the compact bounded-abort
  wording.
- Candidate `component/lifecycle_test_suite.go` SHA-256
  `da21a4b65040b55a206c5289d986ed612c9cda1d6ec3d0e56079388691547d89` already permits abort errors and causally
  checks the exact Stop-context error when its authority ends.
- `component/lifecycle_test_suite.go:191-260` is a controlled-lane goroutine-growth proof; it is not evidence of full
  join after abort Stop authority expires.
- Standard-suite adopters are `gateway/http/http_lifecycle_test.go:57`, `input/udp/udp_lifecycle_test.go:88`, and
  `processor/graph-index/lifecycle_integration_test.go:78`. Rule has its own real-NATS proof.

## Canonical specification and ADR surfaces

- No canonical component-lifecycle specification exists at historical snapshot `35a64ee`; it was created by the later
  #1048 projection.
- Candidate `openspec/specs/component-lifecycle/spec.md` SHA-256
  `643d3b951457583edd2973dd8d6f510c9647a91a5d2baa6c79747cae3f74590e` owns accepted-parent cancellation and
  controlled Stop truth, including no universal cancel-before-drain order and no later running-generation rejoin.
- Candidate #1062 component-lifecycle delta SHA-256
  `4ff5f10b41f13fb07f59153f9b41913e152e0b72e3fbaa10ce8b8d7ae7f02bc9` carries compact bounded-abort wording but
  omits part of the complete current projected requirement.
- Candidate #1062 runtime-context delta SHA-256
  `d5365b4a0be2b23556ce884c84f3249baa4e07f312b112ed49a79c233772b567` already limits abort work to exact live Stop
  authority and forbids replacement authority.
- `docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md:24-36` defines controlled
  ordering and already treats deadline as failed exit with no later rejoin. It requires no amendment.

## Production and Rule proof surfaces

- `service/component_manager.go:900-940` is controlled: it invokes Stop before retained Start cancellation and reports
  nonnil Stop as failed shutdown.
- `processor/rule/readiness_integration_test.go:62-164` contains two controlled real-NATS proofs; both keep Start and
  NATS live and require nil Stop.
- Candidate `processor/rule/readiness_integration_test.go` SHA-256
  `7f548f185b7eaa5370ff752556ea24d0ebfd6e191b594ee1c9af40e0b2655fcb` cancels accepted Start before Stop. It currently
  registers one Cleanup Stop and calls a second Stop in the body, violating the no-second-rejoin contract.
- That Rule test also checks deadline identity only when error text contains a deadline string. If Stop authority
  expires but the returned error omits the caller-context error, the test can false-pass.
- Candidate `processor/rule/entity_evaluation_fence_test.go` SHA-256
  `f7edbbcd60efd03e70a4efcc6f739af731c24ed8073b67013f54a460522dd6f6` is an owner-local deterministic
  cancellation/completion proof. It does not establish full join after caller Stop authority expires.
- Unresolved proof question: how one test-owned Stop operation can cover both ordinary body execution and failure
  cleanup without a second Stop, while causally detecting loss of an expired Stop-context error.

## Measured #1062 evidence

- The controlled command
  `go test -v -race -tags=integration ./processor/rule -run '^(TestIntegration_RuleReadiness_EmptyReplayIsAuthoritativelyNothingToDo|TestIntegration_RuleReadiness_NonEmptyReplayReportsScope)$' -count=1 -failfast -timeout=60s`
  passed 2/2 in 2.562s with accepted Start authority and NATS live; this is recorded in
  `openspec/changes/gh1062-rule-lifecycle-cleanup/conformance.md`.
- The abort command
  `go test -v -race -tags=integration ./processor/rule -run '^TestIntegration_RuleStopAfterAcceptedStartParentCancellation$' -count=20 -failfast -timeout=90s`
  has produced nil, native already-terminal watcher errors, Start-parent cancellation at the runtime command fence,
  and one failure after 11.226s whose Stop result was an `errors.Join` rendering two
  `context deadline exceeded` lines while NATS remained live.
- A later run of that command passed 20/20 in 9.578s, but used the candidate double-Stop/string-gated proof and is not
  authoritative evidence for deadline preservation.
- Successively converting those abort outcomes to nil exposed the next outcome and did not repair the bounded abort
  model. The abort-to-nil Rule production changes were removed; relevant Rule production files returned to their prior
  tracked state.
- Therefore native/deadline abort results are honest observations, not evidence that controlled production ordering is
  broken.

## Adopter seam and unresolved proof boundary

The affected adopter is a custom component caller that cancels accepted Start authority before calling Stop. Production
composition does not do this. The adopter must still call Stop once with finite authority and observe its result; it
must not assume nil, invent replacement authority, or retry the running generation.

The prior accepted inventory at SHA-256 `1c8c5a6e...b880` inventories Rule coordinator, subscription, stream-consumer,
watcher-record, entity-borrow, cron, status-loop, runtime, hot-reload, queue, and cache owners and their completion
surfaces. A nil Stop can establish joined completion. If exact Stop authority expires first, portable evidence can
establish only that termination was driven while authority remained and that failure was reported accurately. It
cannot claim orderly drain, completed join, or leak freedom at return.

Unresolved scope question: whether the compact correction requires any lifecycle API, timeout, production ordering,
error normalization, detached cleanup, or Rule production refactor, or is fully expressed by contract and proof
changes.

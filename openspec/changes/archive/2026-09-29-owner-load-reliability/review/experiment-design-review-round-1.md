# Experiment design review — round 1

Reviewer: semstreams-reviewer, /root/gh1421_inventory_review, 2026-09-29.
Design SHA-256: e15d36b509fca1943c2fe0dc4b7f5fdd6f42d0690d6ad3fda3b9fa0709a76785.

## Findings

HIGH experiment-design.md:132 — Missing independent Stop-delegation mutation oracle.
The facade records its own entry/return/error, which may still happen if native delegation is bypassed.
SDK cancellation independently unsubscribes, so final native state need not differ. Put an invocation observer
below the mutated facade boundary, hold that observer fixed, require its observation, and bypass the call
reaching it in the mutant while retaining outer telemetry. Invocation remains separate from native completion.
Alternatively narrow/defer the claim explicitly.

HIGH experiment-design.md:83 — Parent cleanup time is not reserved.
Setup 20 seconds plus three children at 20 seconds work and 10 seconds cleanup is 110 seconds. Canonical
TestClient may then spend 15 seconds closing and 15 seconds terminating the container. The 120-second panic
alarm can preempt owned cleanup. Use cooperative admission/deadline accounting or an alarm beyond 140 seconds
plus reporting/slack (for example 180 seconds); stop new admission after terminal failures.

## Accepted scope and evidence

The facade honestly models a stalled consumer, not unchanged harness behavior. Stack witnesses, not fixture
cardinality alone, establish both producer blockages. Capture complete/nontruncated snapshots; a deadline preceding
the gates is GATE_NOT_REACHED. Distinct IDs/frames and post-release observations separate producer completion.
Expected errors, nil failed snapshots and exact control set are independent oracles. Three named schedules justify
the examples-based PBT decision. Native Stop nonreturn/process containment must not be described as joined work.

The runner forwards all arguments after canonical flags and Go accepts known test flags following package names;
the trailing timeout override can supply the diagnostic alarm. No natsclient TestMain was found. No tests or
mutations ran during this review.

DESIGN CHANGES REQUESTED — the two bounded corrections above. No broad rediscovery or production-fix design required.

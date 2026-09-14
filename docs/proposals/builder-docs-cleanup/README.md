# Builder documentation cleanup

This Markdown-only change takes #1304 and the existing retirement work in #457, following the audit in #1302 /
PR #1303. The source baseline is `ea22e6a4e75d12bf7f6050c6d8121de1d089b191`.
The [inventory](inventory.md) records source counterparts, immutable starting-document observations and searches.

## Editorial choice

Repairing every copied implementation would retain the drift problem; deleting the learning path would remove
useful adopter context. This change keeps one checked example path, links to maintained source and shortens the
orchestration catalog to patterns, ownership and debugging decisions. The SemSource chapter remains the worked
application narrative with its own version limits.

The first-processor guide is about 150 lines instead of 1,369. The four retired workflow guides total 44 lines
instead of 2,501, with immutable historical links. The orchestration catalog is about 206 lines instead of 622.
The changed adopter documents remove about 4,400 lines before counting this evidence record. These reductions
preserve the application choices and current contract homes; they do not establish every untouched guide's accuracy.

## Checked example

Root exercised the unchanged IoT package and existing `cmd/e2e-semstreams` harness on 2026-09-14:

1. `go test -race ./examples/processors/iot_sensor/...` passed.
2. Both existing binaries built; `task build:default` and `task lint` passed.
3. `bin/e2e-semstreams validate configs/hello-world.json` exited zero with no errors and seven optional API/index
   warnings. Validation was separate from live startup.
4. An owned, temporary NATS 2.14.4 container and the unchanged hello-world config reached `/readyz` HTTP 200.
5. A bounded netcat UDP send and query at `http://localhost:8080/graph-gateway/graphql` returned sensor
   `demo.hello-world-467884.sensor.environmental.temperature.sensor-001`, measurement 23.5, and a reference to
   `demo.hello-world-467884.zone.facility.area.warehouse-7`; the zone entity was also returned.
6. After review, the final readiness, UDP and query shell blocks were extracted from the guide and executed again.
   The explicit triples selection returned the same measurement and relationship under deployment suffix `e0dd60`.
7. SIGINT shut down both harness runs with exit zero; only this task's NATS containers were stopped and removed.

The local run selected no model, agent-loop or embedding component. It logged unavailable optional community
indexes/summaries because clustering was not selected. The documented prefix query succeeded. This is structural
example proof, not proof of production composition, resource limits, offline recovery or the entire E2E ladder.

## Retirement and boundaries

The post-edit retirement search leaves only retirement explanations or historical records. Current routes now
lead to rule/component/lifecycle composition. The catalog distinguishes per-item dispatch from execution
concurrency, private artifacts from rule-readable facts, and descriptive tool effects from application policy.
Context pages use the explicit TaskMessage.Context seam, not retired workflow-step interpolation.

ADRs, historical proposals and migration records are preserved. The roadmap's retirement explanation was checked;
its broader stale status role remains part of the separate audit backlog. #486's JSON comment and pkg/context's
Go comment are outside this Markdown-only change; #486 remains open. No Go, configuration, tasks, CI, capability
contracts or sister repositories were edited. There is no OpenSpec behavior delta or archive to perform.

## Review and checks

The final inventory received independent INVENTORY PASS after builder-entry and current-target counterexamples
were added. Its 107 current source pins verify; baseline guide observations use immutable links because those
files are being shortened. The full-corpus mechanical link comparison found no new missing targets or anchor
candidates. Independent documentation review approved the change with three nonblocking corrections; all were
applied: one orchestration navigation link, visible triples in the query, and estimated token-budget wording.
Exact diff checks and hosted CI status are recorded in the PR.

Audit PR #1303's hosted Test job failed in PredicateLayoutSmoke with a KeysByFilter deadline and NATS drain
timeout. Existing #1286 concerns that harness's guard. This does not prove a root cause or authorize rerunning to
green; the observed failure remains a merge-review hold until resolved or explicitly waived under the protocol.

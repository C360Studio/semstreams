# Service listener implementation conformance

Accepted design: `design.md`, SHA-256
`3be761c8bb5dc8bd93fb886726f0b7c87d79e6f18ef66daa53f418771e1e61f9`.
Owner acceptance: https://github.com/C360Studio/semstreams/issues/1120#issuecomment-5835779961.

The inventory and design retain their exact reviewed historical identities. This table records implementation
conformance separately; source pins in the historical inventory are not rewritten to conceal implementation drift.

| Accepted constraint | Implementation and behavioral evidence |
|---|---|
| Replace all 27 temporary port reservations across the six inventoried files | No remaining `freePort`, `freeMetricsPort`, or `freeServerPort` symbols in `metric/` or `service/`; six test files changed. |
| Preserve public port-zero meanings and native occupied-port refusal | `metric/handler.go:43`, `service/service_manager.go:1284`; refusal tests `service/service_manager_health_listener_test.go:183` and `service/startup_observability_test.go:660`. |
| Keep native metric Start acquisition/request/Stop positive proof | `metric/handler_test.go:101` starts the real server with its private port set to zero, requests Address, and stops it. |
| Add only StartWithListener; successful return transfers the original raw TCP listener | `metric/handler.go:67`; original-listener request/accept/close observations at `metric/handler_test.go:145` and `:185`. |
| Every synchronous rejection leaves supplied listener caller-owned; preserve one-shot ordering | `metric/handler.go:71`; refusal, preparation failure, and used-instance tests at `metric/handler_test.go:198`, `:237`, and `:260`. |
| Address observes actual owned endpoint, retains scheme/path, uses configured fallback without ownership | `metric/handler.go:273`; mismatch/fallback tests at `metric/handler_test.go:145` and TLS scheme/path proof at `:280`; scoped IPv6 URL parsing at `:122`. |
| Rejected second Start preserves the first owned endpoint | `metric/handler_test.go:260` checks original address and both listener ownership states after rejection. |
| TLS wraps exactly once; request contexts descend from caller Start | `metric/handler_test.go:145` checks exact BaseContext; `:280` performs a real HTTPS request with the generated test CA. |
| Standalone and Manager metrics traverse one private bridge into concrete Server | `service/metrics.go:146`, `:322`; `service/service_manager.go:639`; standalone proof `service/metrics_owner_test.go:125`, Manager proof `service/startup_observability_test.go:538`. |
| Health uses a private acquisition seam; disabled/rejected starts never acquire | `service/service_manager.go:1284`; context/zero/repeated-start tests in `service/service_manager_health_listener_test.go:66`, `:156`, `:208`, `:227`. |
| Pprof retains asynchronous best-effort production behavior and explicit test closure/completion | `service/pprof.go:32`; acquisition spy and real pprof request/close/join tests at `service/pprof_test.go:15` and `:41`. |
| Shared HTTP retains existing ephemeral acquisition path and startup route gating | Existing `service/service_manager.go:1176` acquisition remains; `service/startup_observability_test.go:37` observes its assigned port and uses explicit loopback. The blocked-child test at `:538` verifies diagnostic availability and route gating. |
| Early StartAll errors are observed while waiting for child entry/shared binding | `service/startup_observability_test.go:62` retains separate completion and error observations; `requireSignal` selects early completion. |
| Cleanup releases test gates, joins Start, then stops owned runtime under bounded contexts | Manager helper `service/startup_observability_test.go:62` releases gates, waits for completion, then uses a bounded StopAll context. |
| Retention, shutdown, Address, and both service bridge mutations detect the named violations | Five detected mutants across four required families have baseline/mutant/restored exits 0/1/0; reviewed snapshot hashes and assertion output are retained in `evidence.md`, including refreshed Address proof after the IPv6 correction. |
| Broad reusable E2E/composer API remains #1301 | Production changes are limited to four owner files. No E2E composer, binary, schema, configuration, or port-default change. |

## Verification state

Focused race checks and all five final-byte mutation comparisons passed their intended outcomes.
Implementation review and full pre-push verification remain pending; these are not claims of merge readiness.

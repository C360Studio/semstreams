# Test reliability audit inventory checkpoint
base: 3dc4ccbef32e87096c6d998fde7e76e896cf2f3c

Boundary: #1397 recurrence, #1283 and rule/cron shutdown hangs, existing async test foundations and runner/gate cost.
Purpose: enough evidence for a repair plan, not an exhaustive typed census of the repository.
No target-state decisions, new APIs, implementation tasks, or code changes are included.

## Materialized inventory identities
- incident-inventory.md: SHA-256 cd1396b233640e494d9aa97e4bf770e18b4155b6eb5c50c9e3fb543a3eb9c630
- runner-inventory.md: SHA-256 038f0d1082f728af6e029a019656a9b43219079545ce62bb0c1c9e0692dbd313
- async-inventory.md: SHA-256 8ca5b46a6397aedb653f4683d8655deed3270a02e18df2a8b63feb3083a39ff0
- rule-hang-inventory.md: SHA-256 af123d10b97bdcef265401c4e01b74d7d4246b4aa12dd3ed2685c0c399a72408
- policy-bound-inventory.md: SHA-256 a75e318c01f07075ee2112b1fc888c01af99b2c952b27ae85fd82409eaa28abd

## Inventory verification
Runner:88/88; async:59/59; rule-hang:88/88; policy-bound:8/8 source pins valid at baseline.
Issue/history records have external GitHub evidence and saved raw JSON/log files, not source pin-verifier entries.
Architect independently validated the core claim/owner/policy boundaries and provided policy-bound supplement.
Structural gopls failures remain unresolved; inventories declare bounded sampling and source-search limitations.

## Complete inventory text

=== incident-inventory.md ===

# Issue and CI incident inventory
base: 3dc4ccbef32e87096c6d998fde7e76e896cf2f3c

Collected 2026-09-28 from GitHub via gh. Local source baseline is clean main. No tests were executed.
The user reports a new local #1397 occurrence and a #1283-related 20-minute CI hang in a paused Claude session; no local log path supplied.

## Search boundary
- gh issue list --state all --label class:flake --limit 100: 22 issues.
- gh issue list --state all --search "test in:title" --limit 100: 35 issues.
- Additional searches: hang in:title; timeout in:title; cleanup in:title; test infrastructure in:title,body.
- gh pr list --state open --limit 100: 6 open claims, saved in open-prs.json.
- gh run list --limit 60: 60 recent workflow entries, 5 failure conclusions and 2 cancelled entries; entries are not necessarily unique commits.
- Saved exact issue bodies/comments and job logs alongside this file; issue statements remain historical claims unless code/log independently confirms them.

## Live ownership / scheduling facts
- #1397, #1283, #1287, #1317 are open and assigned beta.165.
- #1064 cleanup census/guard and #736 container pressure are open without milestones in current API response.
- #1293 has explicit 2026-09-27 owner ruling retaining rc.1: common local/CI verification, duplicate suite removal, citation/fuzz/coverage plumbing.
- #1222 / draft PR #1406 owns E2E required-check accounting/selection; explicitly excludes generic test framework and expansion to every tier per PR.
- #1349 owns test-only shared KV fake; no open draft PR in sampled claims closes it.
- Paused live PRs: #1407 loop design, #1406 E2E evidence design, #1404 config namespace, #1368 NEAR audit plan, #1254 auth inventory, #1141 page retrieval.

## Historical issue evidence (not fresh reproduction)
- #1397 [OPEN] flake(testinfra): TerminationReapsPullBeforeReleasingLock predicts a 3s pipe budget that host load can miss: Four 3s pipe synchronization waits; historical 1/5 local failure then 5/5 isolated pass; waiver was PR1388 only. Source: https://github.com/C360Studio/semstreams/issues/1397
- #1290 [CLOSED] flake(testinfra): HostLockHasBoundedContentionDiagnostics predicts a 3s exit budget for the holder — 2/10 under load, one day after #1284 deleted the same shape: Three 3s process-exit waits removed using test deadline after failures under full-suite load; runner lock contender bound intentionally retained. Source: https://github.com/C360Studio/semstreams/issues/1290
- #1284 [CLOSED] flake(graph-index): OwnerFilterLoadHarness's 3s per-rep gate fires on runner stalls three orders of magnitude off the same run's distribution — #750 was closed by documenting it: Per-operation performance threshold removed; aggregate performance budgets deliberately retained pending #1287 calibration. Source: https://github.com/C360Studio/semstreams/issues/1284
- #1061 [CLOSED] testinfra: termination proof races the runner's bounded KILL escalation: Same runner test had a causal TERM/KILL race; test-private clock boundary handshake fixed that earlier failure, not the remaining 3s waits. Source: https://github.com/C360Studio/semstreams/issues/1061
- #1062 [CLOSED] test lifecycle: unbounded Background Stop cleanup can consume the global integration timeout: Test cleanup destroyed NATS via defer before component Cleanup, then unbounded Stop; four-site repair kept broader census separate in #1064. Source: https://github.com/C360Studio/semstreams/issues/1062
- #1064 [OPEN] test lifecycle: audit and guard unbounded Stop cleanup roots: Open: AST/type-aware classification and guard for unbounded lifecycle cleanup; 517 raw matches / 295 same-line cleanup sites are historical figures, not current verified defects. Source: https://github.com/C360Studio/semstreams/issues/1064
- #1283 [OPEN] fix(rule): a bounded Stop can block forever on an orphaned fence barrier, violating component-lifecycle spec:34: Open production bug: orphaned runtime command fence can be appended after final drain; bare receives can ignore an expired caller context; messageCache and watcher join ownership residuals included. Source: https://github.com/C360Studio/semstreams/issues/1283
- #1370 [CLOSED] agentic-tools: TestIntegrationAckFailureRestartReplaysWithoutSecondExecution hung 20m in Stop → cleanup: Closed: agentic-tools Stop/cleanup hang with Background waited 20 minutes; issue body alone does not establish current mechanism/fix. Source: https://github.com/C360Studio/semstreams/issues/1370
- #1340 [CLOSED] natsclient: TestKVContractPinnedNATSGoDependency shells out to `go list` inside `go test` and can hang until the 20m alarm: Closed: nested go list subprocess in test hung until package alarm under cold build; not a functional broker assertion. Source: https://github.com/C360Studio/semstreams/issues/1340
- #1375 [CLOSED] agentic-model: TestIntegrationPreProviderReplacementSeesAbsenceAndInvokesOnce — replacement never sees the redelivered request within 5s: Closed: replacement redelivery test synchronization race; initial diagnosis explicitly inferred, later wait-for-consumer change landed in #1389. Source: https://github.com/C360Studio/semstreams/issues/1375
- #1394 [CLOSED] test(graph-index): tolerate completed key-lister consumer during cleanup: Closed: expected key-lister auto-completion raced explicit Stop; retain consumer-count cleanup proof while recognizing already-deleted consumer. Source: https://github.com/C360Studio/semstreams/issues/1394
- #736 [OPEN] The integration suite oversubscribes Docker under package parallelism; sub-second tests time out: Open: broad container pressure class; measured -p1 full CI 23m37s vs -p2 12m45s in Sep19 comment. Container-start failures persist after -p2. A passing rerun does not prove infrastructure-only attribution. Source: https://github.com/C360Studio/semstreams/issues/736
- #469 [OPEN] flaky test: TestWebSocketFederation_MessageEnvelopeProtocol (+ flow_runtime_stream websocket read panic) trips the integration gate intermittently: Open: WebSocket readiness/sleep and repeated-read-after-failure symptoms; historical isolation pass is not proof of no production race. Source: https://github.com/C360Studio/semstreams/issues/469
- #1287 [OPEN] test(graph-index): re-derive BOTH owner-filter profiles' percentile budgets from the recorded submission-order distributions: Open: calibrated aggregate performance evidence, both CI and full profiles; owner forbids guessing tighter thresholds before submission-order evidence. Source: https://github.com/C360Studio/semstreams/issues/1287
- #1317 [OPEN] e2e ladder: host-port bind fails after verified reservation and green preflight: Open: bind may fail after snapshot/reservation; needs actual bind evidence. Source: https://github.com/C360Studio/semstreams/issues/1317
- #1349 [OPEN] test: one shared in-memory KV bucket fake replaces seventeen hand-written ones: Open: consolidate repeated unit KV fakes with production contract fidelity, not substitution for real broker semantics. Source: https://github.com/C360Studio/semstreams/issues/1349
- #508 [CLOSED] ComponentManager: performDetailedHealthCheck leaks a cm.mu reader on its 50ms timeout, deadlocking Stop: Closed production lock leak: abandoned lock-acquiring goroutine later acquired RLock without unlock; replaced with TryRLock, causal lock-held regression RED/GREEN. Source: https://github.com/C360Studio/semstreams/issues/508

## Direct CI observations
- run 36343596313, head ada5c46aa1ecc03296780a054aa9595e4ea64041, PR1404 branch: Test job 1400s (23m20s); internal/boot package alarm at 1200.059s; TestRootRuleManagerHotReloadsIntoTheProcessor active 19m59s.
- Its stack: processor/rule.cleanup processor.go:1394; Stop :1299; internal/boot.startedRuleProcessor cleanup rule_hot_reload_integration_test.go:72; CronScheduler.Stop.func1 cron_scheduler.go:500. This is NOT the issue1283 barrier-receive stack. Exact SHA mapping is delegated to rule-hang inventory.
- run 36307464897 main 078782b16a099e1b4e29e354a252b28755a20273: Test job 457s; TestIntegrationProductionCallbackFirstDispositionAfterOwnRecordLoss/delete failed in NATS container-start POST after 30.24s; feature assertions did not execute. Logs prove startup timeout, not a specific resource bottleneck.
- run 36326127730: Lint failed at 92s, Test succeeded at 818s.
- run 36330220038: Lint failed at 92s, Test succeeded at 815s.
- run 36330545049: Lint failed at 74s, Test cancelled at 1515s. Full Test log ends with cancellation, cleanup names an orphaned rule.test process (PID 95570), and no timeout stack is present; the exact blocked rule function/test remains unproven.
- The three Lint failures are OpenSpec validation in design-claim PRs; protocol explicitly anticipates initial delta-less claim red. They are not test flakes. Independently scheduled Test jobs still consumed their durations.
- Sources: run-<id>.json, run-<id>-failed.log, run-36330545049-test.log. Individual job start/end timestamps used for duration, not total workflow updatedAt.

## Same-baseline successful timing control
Latest successful main CI run 36329754777 tests source 3dc4ccbef32e87096c6d998fde7e76e896cf2f3c.
Test job completed in 814 seconds (13m34s); 160 completed package duration records were extracted from its Test log.
Largest recorded packages: natsclient 103.683s; agentic-dispatch 85.170s; agentic-loop 75.774s;
objectstore 64.526s; agentic-tools 59.324s; graph-index 56.166s; rule 48.320s; test/release 45.037s;
test/testinfra 44.643s; graph-ingest 42.853s. These are package runtimes, not additive wall time or p95 distributions.
No measured container counts or cold/warm setup split are inferred from this sample.
Source: run-36329754777.json and run-36329754777.log.

## Limits
No new reproductions, CI retries, stress runs, paid calls, host pressure measurements or changes to code/issues/PRs.
The two reported current incidents were not presumed to share a cause. Raw textual test counts are candidates for typed audit, not defect counts.


=== runner-inventory.md ===

# Inventory: test runner, test infrastructure, CI gates
base: 3dc4ccbef32e87096c6d998fde7e76e896cf2f3c

## Claimed gap
- `test/testinfra/integration_runner_contract_test.go:289` — `readPipeSignal(t, parentReadyReader, 3*time.Second, "parent retained pull PID")`
- `test/testinfra/integration_runner_contract_test.go:298` — `readPipeSignal(t, termAckReader, 3*time.Second, "pull helper TERM acknowledgement")`
- `test/testinfra/integration_runner_contract_test.go:299` — `readPipeSignal(t, gracePauseReader, 3*time.Second, "cleanup grace paused")`
- `test/testinfra/integration_runner_contract_test.go:345` — `readPipeSignal(t, reapAckReader, 3*time.Second, "post-reap lock removal")`

## Spellings of the fact
- `scripts/run-integration-tests.sh:16` — `image_pull_timeout_seconds="${SEMSTREAMS_CONTRACT_IMAGE_PULL_TIMEOUT_SECONDS:-300}"`
- `scripts/run-integration-tests.sh:136` — `terminate_and_reap_image_pull() {`
- `scripts/run-integration-tests.sh:139` — `local owned_pid grace_deadline now`
- `scripts/run-integration-tests.sh:142` — `kill -TERM "$owned_pid" 2>/dev/null || true`
- `scripts/run-integration-tests.sh:143` — `grace_deadline=$(($(date +%s) + 1))`
- `scripts/run-integration-tests.sh:155` — `kill -KILL "$owned_pid" 2>/dev/null || true`
- `scripts/run-integration-tests.sh:165` — `run_bounded_image_pull() {`
- `scripts/run-integration-tests.sh:172` — `pull_deadline=$(($(date +%s) + image_pull_timeout_seconds))`
- `scripts/run-integration-tests.sh:192` — `acquire_lock() {`
- `scripts/run-integration-tests.sh:194` — `deadline=$((owner_started + wait_seconds))`
- `scripts/run-integration-tests.sh:236` — `trap cleanup_runner EXIT`
- `scripts/run-integration-tests.sh:237` — `trap 'exit 130' INT TERM`
- `scripts/run-integration-tests.sh:243` — `export TESTCONTAINERS_RYUK_DISABLED=false`
- `scripts/run-integration-tests.sh:329` — `go test -race -failfast -tags=integration -timeout=20m -count=1 -p 2 "${packages[@]}"`
- `test/testinfra/integration_runner_contract_test.go:804` — `func waitForFileContent(t *testing.T, path, content string, timeout time.Duration) {`
- `test/testinfra/integration_runner_contract_test.go:823` — `func readPipeSignal(t *testing.T, reader *os.File, timeout time.Duration, description string) {`
- `test/testinfra/integration_runner_contract_test.go:851` — `func untilTestDeadline(t *testing.T) time.Duration {`
- `test/testinfra/integration_runner_contract_test.go:876` — `func newCommandWaiter(command *exec.Cmd) *commandWaiter {`
- `test/testinfra/integration_runner_contract_test.go:888` — `func (w *commandWaiter) wait(timeout time.Duration) error {`
- `test/testinfra/integration_runner_contract_test.go:899` — `func (w *commandWaiter) killAndWait() error {`
- `test/testinfra/integration_runner_contract_test.go:1015` — `func assertProcessGone(t *testing.T, pid int, description string) {`
- `test/testinfra/integration_runner_contract_test.go:1022` — `func processExists(pid int) bool {`
- `Taskfile.yml:152` — `desc: "Full pre-push gate, mirrors CI (~11min, needs Docker): build, lint, vet integration+live_llm, schema drift, contract, race unit + integration. Use /preflight for the judgment layer (diff scope, breaking->e2e)."`
- `Taskfile.yml:161` — `- go test -race ./...`
- `Taskfile.yml:162` — `- task: test:integration`
- `taskfiles/test.yml:7` — `- go test ./...`
- `taskfiles/test.yml:12` — `- go test -race ./...`
- `taskfiles/test.yml:15` — `desc: Run integration tests (uses testcontainers for Docker infra)`
- `taskfiles/test.yml:17` — `- scripts/run-integration-tests.sh`
- `.github/workflows/ci.yml:13` — `lint:`
- `.github/workflows/ci.yml:135` — `test:`
- `.github/workflows/ci.yml:138` — `timeout-minutes: 25`
- `.github/workflows/ci.yml:249` — `needs: [lint, test, build, schema-validation, api-compat]`
- `.github/workflows/ci.yml:149` — `- name: Run additive unit and integration suite`
- `.github/workflows/ci.yml:150` — `run: scripts/run-integration-tests.sh`
- `docs/contributing/01-testing.md:286` — `its containers at once and the container starts miss their own budgets (gh#736); changes to concurrency MUST be`
- `docs/contributing/01-testing.md:533` — `per-package migration lever; it is a runner-level setting (gh#736).`
- `test/testinfra/policy_guard_test.go:56` — `func TestInfrastructurePolicyGuard(t *testing.T) {`
- `test/testinfra/policy_guard_test.go:63` — `t.Fatalf("policy guard scanned too little source: %+v; a zero/near-zero scan is a broken guard, not a clean repository", stats)`
- `test/testinfra/policy_guard_test.go:591` — `t.Errorf("new test-infrastructure policy violations (%d); move Docker evidence to integration, use the canonical substrate, or replace the wait:\n  %s",`
- `test/testinfra/policy_guard_test.go:595` — `t.Errorf("stale policy baseline entries (%d); remove resolved debt so the ratchet cannot hide a regression:\n  %s",`

## Adjacent claims
- #1397 — `flake(testinfra): TerminationReapsPullBeforeReleasingLock predicts a 3s pipe budget that host load can miss` (OPEN; cached issue record).
- #1290 — `flake(testinfra): HostLockHasBoundedContentionDiagnostics predicts a 3s exit budget for the holder — 2/10 under load, one day after #1284 deleted the same shape` (CLOSED; cached issue record).
- #1061 — `testinfra: termination proof races the runner's bounded KILL escalation` (CLOSED; cached issue record).
- #1291 — not located in selected tracked files or `/private/tmp/semstreams-test-audit-20260928/flake-issues.json`.
- #1293 — `ci: unify local and CI verification with citation, fuzz, and coverage evidence` (OPEN; cached issue record).
- #1222 — `e2e: make selected required checks and their evidence determine success` (OPEN; cached issue record).
- PR #1406 — `test(e2e): design required-check evidence (#1222)` (cached open-PR record; body references #1293 and #1397).
- `openspec/changes/archive/2026-09-27-one-composition-root/tasks.md:170` — `pre-fix-pass argument positions; not the #1397 flake; fixed in `05182c2e`, a two-line test change). At `05182c2e`:`
- `scripts/run-integration-tests.sh:319` — `# distribution to this file instead, and it is printed below on pass and on failure alike (#1284).`
- `scripts/run-integration-tests.sh:328` — `# (gh#736). The flag looks like a pessimization; it is not free to remove.`
- `test/testinfra/integration_runner_contract_test.go:43` — `// gh#736: at most two test packages at a time. Uncapped, every`
- `docs/contributing/01-testing.md:286` — `its containers at once and the container starts miss their own budgets (gh#736); changes to concurrency MUST be`
- `docs/contributing/01-testing.md:533` — `per-package migration lever; it is a runner-level setting (gh#736).`

## Consumers
- `taskfiles/test.yml:15` — `desc: Run integration tests (uses testcontainers for Docker infra)`
- `taskfiles/test.yml:17` — `- scripts/run-integration-tests.sh`
- `taskfiles/test.yml:20` — `desc: Run IoT sensor integration tests only`
- `taskfiles/test.yml:22` — `- scripts/run-integration-tests.sh ./examples/processors/iot_sensor/...`
- `taskfiles/test.yml:25` — `desc: Run graph processor integration tests`
- `taskfiles/test.yml:27` — `- scripts/run-integration-tests.sh ./processor/graph/...`
- `.github/workflows/ci.yml:149` — `- name: Run additive unit and integration suite`
- `.github/workflows/ci.yml:150` — `run: scripts/run-integration-tests.sh`
- `.github/workflows/ci.yml:249` — `needs: [lint, test, build, schema-validation, api-compat]`
- `Taskfile.yml:161` — `- go test -race ./...`
- `Taskfile.yml:162` — `- task: test:integration`
- `test/testinfra/integration_runner_contract_test.go:20` — `func TestIntegrationRunner_CanonicalCommandAndRyukPolicy(t *testing.T) {`
- `test/testinfra/integration_runner_contract_test.go:177` — `func TestIntegrationRunner_ImagePullTimeoutIsBounded(t *testing.T) {`
- `test/testinfra/integration_runner_contract_test.go:211` — `func TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock(t *testing.T) {`
- `test/testinfra/integration_runner_contract_test.go:516` — `func TestIntegrationRunner_HostLockHasBoundedContentionDiagnostics(t *testing.T) {`
- `test/testinfra/integration_runner_contract_test.go:578` — `func TestIntegrationRunner_CleansOnlyProvablyStaleLock(t *testing.T) {`
- `test/testinfra/integration_runner_contract_test.go:622` — `func TestIntegrationRunner_PublishesLatencyEvidence(t *testing.T) {`
- `test/testinfra/integration_runner_contract_test.go:670` — `func TestIntegrationRunner_TaskAndCIConverge(t *testing.T) {`
- `test/testinfra/policy_guard_test.go:56` — `func TestInfrastructurePolicyGuard(t *testing.T) {`

## Problem shape
- `test/testinfra/integration_runner_contract_test.go:289` — `readPipeSignal(t, parentReadyReader, 3*time.Second, "parent retained pull PID")`
- `test/testinfra/integration_runner_contract_test.go:298` — `readPipeSignal(t, termAckReader, 3*time.Second, "pull helper TERM acknowledgement")`
- `test/testinfra/integration_runner_contract_test.go:299` — `readPipeSignal(t, gracePauseReader, 3*time.Second, "cleanup grace paused")`
- `test/testinfra/integration_runner_contract_test.go:345` — `readPipeSignal(t, reapAckReader, 3*time.Second, "post-reap lock removal")`
- `test/testinfra/integration_runner_contract_test.go:825` — `if err := reader.SetReadDeadline(time.Now().Add(timeout)); err != nil {`
- `test/testinfra/integration_runner_contract_test.go:806` — `deadline := time.NewTimer(timeout)`
- `test/testinfra/integration_runner_contract_test.go:807` — `ticker := time.NewTicker(10 * time.Millisecond)`
- `test/testinfra/integration_runner_contract_test.go:843` — `// untilTestDeadline is the wait for a process the test has already released:`
- `test/testinfra/integration_runner_contract_test.go:877` — `waiter := &commandWaiter{`
- `test/testinfra/integration_runner_contract_test.go:882` — `waiter.err = command.Wait()`
- `test/testinfra/integration_runner_contract_test.go:894` — `case <-timer.C:`
- `test/testinfra/integration_runner_contract_test.go:906` — `killErr := w.command.Process.Kill()`
- `scripts/run-integration-tests.sh:142` — `kill -TERM "$owned_pid" 2>/dev/null || true`
- `scripts/run-integration-tests.sh:155` — `kill -KILL "$owned_pid" 2>/dev/null || true`
- `scripts/run-integration-tests.sh:236` — `trap cleanup_runner EXIT`
- `test/testinfra/policy_guard_test.go:236` — `categories: []string{"integration-time-sleep"},`
- `test/testinfra/policy_guard_test.go:591` — `t.Errorf("new test-infrastructure policy violations (%d); move Docker evidence to integration, use the canonical substrate, or replace the wait:\n  %s",`
- `test/testinfra/policy_guard_test.go:595` — `t.Errorf("stale policy baseline entries (%d); remove resolved debt so the ratchet cannot hide a regression:\n  %s",`

## Searches
- `rg -n '^## (Purpose|Product Boundary)' openspec/project.md` → 2
- `gopls workspace_symbol -matcher=fuzzy commandWaiter` → 0 results; workspace load failed (`package unsafe is not in std`, `/usr/local/go/src/unsafe`).
- `gopls references test/testinfra/integration_runner_contract_test.go:823:6` → 0 results; same workspace load failure, then `no package metadata for file`.
- `git grep -n -E 'timeout|deadline|first.failure|first failure|lock|cleanup|signal|phase|testcontainers|integration|race|tag|gotestsum|go test' -- scripts/run-integration-tests.sh test/testinfra Taskfile.yml taskfiles/test.yml .github/workflows/ci.yml` → 547
- `git grep -n -E 'readPipeSignal|commandWaitTimeoutError|commandWaiter|waitForFileContent|assertProcessGone' -- test/testinfra scripts taskfiles Taskfile.yml .github/workflows/ci.yml` → 24
- `git grep -n -E '1397|1290|1291|1061|1284|736' -- scripts/run-integration-tests.sh test/testinfra Taskfile.yml taskfiles/test.yml .github/workflows/ci.yml docs/contributing docs/adr openspec/specs openspec/changes` → 121 (output truncated)
- `git grep -n -E '#1397|1397|#1290|1290|#1291|1291|#1061|1061|#1284|1284|#736' -- scripts/run-integration-tests.sh test/testinfra Taskfile.yml taskfiles/test.yml .github/workflows/ci.yml docs/contributing/01-testing.md openspec/changes/archive/2026-09-27-one-composition-root/tasks.md` → 13
- `git grep -n '^func TestIntegrationRunner' -- test/testinfra/integration_runner_contract_test.go` → 16
- `rg --files test/testinfra` → 3
- `git grep -n -E 'readPipeSignal\(t, .*3\*time.Second|waitForFileContent\(t, .*3\*time.Second|time\.After\(3\*time.Second|3 \* time.Second' -- test/testinfra` → 9
- `git grep -n -E 'TestInfrastructurePolicyGuard|scanned too little|integration-time-sleep|new test-infrastructure policy|stale policy baseline' -- test/testinfra/policy_guard_test.go` → 9
- `git grep -n -E '^func (waitForFileContent|readPipeSignal|untilTestDeadline|newCommandWaiter|assertProcessGone|processExists|TestIntegrationRunner_|TestInfrastructurePolicyGuard)|^func \(w \*commandWaiter\) (wait|killAndWait)|^terminate_and_reap_image_pull\(\)|^run_bounded_image_pull\(\)|^acquire_lock\(\)|^cleanup_runner\(\)|^trap |^go test -race|^packages=|^latency_log=|^deadline=|^pull_deadline=|^grace_deadline=' -- scripts/run-integration-tests.sh test/testinfra/integration_runner_contract_test.go test/testinfra/policy_guard_test.go` → 39
- `git grep -n -E 'image_pull_timeout_seconds=|local owned_pid|grace_deadline=|kill -TERM|kill -KILL|pull_deadline=|deadline=|TESTCONTAINERS_RYUK_DISABLED|docker info failed|image pull timed out|running Docker-backed tests|no latency distributions|tests failed with status|waitForFileContent|readPipeSignal|untilTestDeadline|go test -race|fast-feedback duplicate|timeout-minutes:|Run additive unit|task: test:integration|go test -race ./\.\.\.' -- scripts/run-integration-tests.sh test/testinfra/integration_runner_contract_test.go Taskfile.yml taskfiles/test.yml .github/workflows/ci.yml` → 39
- `git grep -n -E 'TestInfrastructurePolicyGuard|scanned too little|integration-time-sleep|new test-infrastructure policy|stale policy baseline' -- test/testinfra/policy_guard_test.go` → 9
- `git grep -n -E '^  [a-zA-Z0-9_-]+:|^    needs:|^    timeout-minutes:' -- .github/workflows/ci.yml` → 11
- `jq 'map(select(.number == 1397 or .number == 1290 or .number == 1291 or .number == 1061) | {number, title, state, labels})' /private/tmp/semstreams-test-audit-20260928/flake-issues.json` → 3
- `jq '{number,title,state,url}' /private/tmp/semstreams-test-audit-20260928/issue-1293.json` → 1
- `jq '{number,title,state,url}' /private/tmp/semstreams-test-audit-20260928/issue-1222.json` → 1
- `jq 'map(select(.number == 1406) | {number,title,body})' /private/tmp/semstreams-test-audit-20260928/open-prs.json` → 1


=== async-inventory.md ===

# Inventory: asynchronous test design and test infrastructure
base: 3dc4ccbef32e87096c6d998fde7e76e896cf2f3c

## Claimed gap
- `test/testinfra/integration_runner_contract_test.go:289` — `readPipeSignal(t, parentReadyReader, 3*time.Second, "parent retained pull PID")`
- `test/testinfra/integration_runner_contract_test.go:298` — `readPipeSignal(t, termAckReader, 3*time.Second, "pull helper TERM acknowledgement")`
- `test/testinfra/integration_runner_contract_test.go:299` — `readPipeSignal(t, gracePauseReader, 3*time.Second, "cleanup grace paused")`
- `test/testinfra/integration_runner_contract_test.go:345` — `readPipeSignal(t, reapAckReader, 3*time.Second, "post-reap lock removal")`

## Spellings of the fact
- `model/breaker_test.go:12` — `type fakeClock struct {`
- `model/breaker_test.go:17` — `func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }`
- `model/breaker_test.go:25` — `func (c *fakeClock) Advance(d time.Duration) {`
- `processor/graph-index/reconciliation_model_test.go:8` — `"testing/synctest"`
- `processor/graph-index/reconciliation_prop_test.go:7` — `"testing/synctest"`
- `processor/rule/lifecycle_runtime_test.go:9` — `"testing/synctest"`
- `processor/rule/lifecycle_runtime_test.go:120` — `synctest.Wait()`
- `service/metrics_forwarder_test.go:14` — `"testing/synctest"`
- `service/metrics_forwarder_test.go:235` — `synctest.Test(t, func(t *testing.T) {`
- `service/metrics_forwarder_test.go:262` — `synctest.Wait()`

## Adjacent claims
- #1397 — `flake(testinfra): TerminationReapsPullBeforeReleasingLock predicts a 3s pipe budget that host load can miss`
- #1290 — `flake(testinfra): HostLockHasBoundedContentionDiagnostics predicts a 3s exit budget for the holder — 2/10 under load, one day after #1284 deleted the same shape`
- #1284 — `flake(graph-index): OwnerFilterLoadHarness's 3s per-rep gate fires on runner stalls three orders of magnitude off the same run's distribution — #750 was closed by documenting it`
- #1061 — `testinfra: termination proof races the runner's bounded KILL escalation`
- #1283 — production Stop deadlock / 20-minute CI hang (identified in the task brief)
- #1064 — `test lifecycle: audit and guard unbounded Stop cleanup roots`
- #1340 — `natsclient: TestKVContractPinnedNATSGoDependency shells out to go list inside go test and can hang until the 20m alarm`
- #1375 — `agentic-model: TestIntegrationPreProviderReplacementSeesAbsenceAndInvokesOnce — replacement never sees the redelivered request within 5s`
- #1394 — `test(graph-index): tolerate completed key-lister consumer during cleanup`
- #469 — `flaky test: TestWebSocketFederation_MessageEnvelopeProtocol (+ flow_runtime_stream websocket read panic) trips the integration gate intermittently`
- #736 — `The integration suite oversubscribes Docker under package parallelism; sub-second tests time out`
- #1349 — `test: one shared in-memory KV bucket fake replaces seventeen hand-written ones`
- `docs/contributing/01-testing.md:429` — `Tests MUST NOT use `time.Sleep` to wait for readiness, delivery, retries, cleanup, or state convergence. A longer sleep`
- `docs/contributing/01-testing.md:438` — `Polling MUST have a narrow deadline and report the last value and last error on failure. It MUST NOT silently turn a`
- `docs/contributing/01-testing.md:441` — `Test I/O contexts MUST derive from `t.Context()` and then narrow the deadline:`
- `docs/contributing/01-testing.md:455` — `Cleanup is the intentional exception. The Go test runner cancels `t.Context()` before invoking registered cleanup`
- `docs/contributing/01-testing.md:472` — `| Integration package | 3m | 5m |`
- `docs/contributing/01-testing.md:482` — `Failures involving asynchronous or external state MUST identify the condition, elapsed time, attempts, last observed`
- `docs/contributing/01-testing.md:543` — `- exact existing `time.Sleep` calls in integration files are ratcheted.`
- `docs/contributing/01-testing.md:281` — `The canonical integration runner uses `-race -failfast -tags=integration -timeout=20m -count=1 -p 2 ./...`. The`
- `docs/contributing/01-testing.md:296` — `preflight, the host lock, and the Reaper policy. Go's `-timeout=20m` is a per-package timeout, not a whole-suite`

## Consumers
- `natsclient/test_client.go:789` — `if err := client.WaitForConnection(connectCtx); err != nil {`
- `natsclient/test_client.go:813` — `resourceSetupCtx, resourceSetupCancel := context.WithTimeout(ctx, cfg.timeout)`
- `natsclient/test_client.go:848` — `func NewSharedTestClient(opts ...TestOption) (*TestClient, error) {`
- `natsclient/test_client.go:854` — `func NewTestClient(t testing.TB, opts ...TestOption) *TestClient {`
- `natsclient/client.go:363` — `func (m *Client) WaitForConnection(ctx context.Context) error {`
- `natsclient/client.go:1379` — `func (m *Client) WaitForBucket(ctx context.Context, name string, timeout time.Duration) (jetstream.KeyValue, error) {`
- `test/e2e/client/metrics.go:573` — `func (c *MetricsClient) WaitForMetric(ctx context.Context, metricName string, expected float64, opts WaitOpts) error {`
- `test/e2e/client/nats.go:1117` — `func (c *NATSValidationClient) WaitForEntityCountSSE(`
- `test/e2e/client/nats.go:1228` — `func (c *NATSValidationClient) waitForSourceEntityCountPolling(`
- `test/e2e/client/messagelogger.go:147` — `func (c *MessageLoggerClient) WaitForTrace(`
- `pkg/dispatch/keyed_pool_test.go:310` — `var wg sync.WaitGroup`
- `natsclient/jetstream_metrics_test.go:175` — `close(handle.release)`

## Problem shape
- `natsclient/jetstream_metrics_test.go:151` — `started chan struct{}`
- `natsclient/jetstream_metrics_test.go:156` — `close(b.started)`
- `natsclient/jetstream_metrics_test.go:173` — `<-handle.started`
- `natsclient/jetstream_metrics_test.go:175` — `close(handle.release)`
- `natsclient/jetstream_metrics_test.go:176` — `<-done`
- `natsclient/publish_async_integration_test.go:134` — `go func() { wg.Wait(); close(done) }()`
- `natsclient/publish_async_integration_test.go:136` — `case <-done:`
- `pkg/dispatch/keyed_pool_test.go:311` — `stop := make(chan struct{})`
- `pkg/dispatch/keyed_pool_test.go:312` — `started := make(chan struct{})`
- `pkg/dispatch/keyed_pool_test.go:335` — `<-started // ensure submits are in flight, racing with Stop`
- `pkg/dispatch/keyed_pool_test.go:338` — `wg.Wait()`
- `service/metrics_forwarder_test.go:261` — `time.Sleep(350 * time.Millisecond)`
- `service/metrics_forwarder_test.go:262` — `synctest.Wait()`
- `test/e2e/scenarios/http_gateway_readiness.go:166` — `"(%d attempts over %s; last response: code=%q class=%q message=%q): "+`
- `test/e2e/scenarios/http_gateway_readiness.go:175` — `attempts++`
- `test/e2e/scenarios/validate_infra.go:722` — `timeout := time.After(s.config.ValidationTimeout)`
- `test/e2e/scenarios/validate_infra.go:728` — `case <-timeout:`
- `pkg/dispatch/dispatcher_test.go:151` — `time.Sleep(20 * time.Millisecond)`
- `pkg/dispatch/integration_test.go:131` — `time.Sleep(500 * time.Millisecond)`
- `test/testinfra/integration_runner_contract_test.go:853` — `deadline, ok := t.Deadline()`
- `test/testinfra/integration_runner_contract_test.go:883` — `close(waiter.done)`
- `test/testinfra/integration_runner_contract_test.go:892` — `case <-w.done:`
- `test/testinfra/integration_runner_contract_test.go:901` — `<-w.done`
- `natsclient/test_client.go:789` — `if err := client.WaitForConnection(connectCtx); err != nil {`

## Repository-wide syntactic counts
Searches were over tracked `*_test.go`; there are 1,351 such files. Counts are lexical occurrences, not defect classifications. Distribution is by top-level path prefix.

time.Sleep: 446 lines / 117 files — component 1, config 2, gateway 2, graph 6, health 1, input 3, natsclient 10, output 6, pkg 13, processor 62, service 8, storage 2, test 1.
time.After: 346 lines / 126 files — agentic 1, config 6, gateway 1, graph 10, input 5, internal 4, metric 1, model 1, natsclient 16, output 4, pkg 15, processor 50, service 12.
time.NewTimer: 7 lines / 5 files — internal 1, natsclient 1, processor 2, test 1.
require.Eventually: 213 lines / 75 files — agentic 1, config 1, gateway 1, internal 2, natsclient 5, output 3, pkg 1, processor 54, service 6, test 1.
sync.WaitGroup: 106 lines / 73 files — component 1, config 4, graph 6, health 1, input 1, internal 1, metric 1, model 1, natsclient 6, output 4, payloadregistry 1, pkg 14, processor 27, service 4, vocabulary 1.
wg.Wait(): 107 lines / 75 files — component 1, config 4, graph 6, health 1, input 4, metric 1, model 1, natsclient 6, output 4, payloadregistry 1, pkg 14, processor 27, service 4, vocabulary 1.
context.WithTimeout: 325 lines / 136 files. t.Context: 378 lines / 51 files.

The fake-clock/synctest spelling search returned 23 lines across 5 files: `model/breaker_test.go` has 8 `fakeClock` matches; `processor/graph-index/reconciliation_model_test.go` 5; `processor/graph-index/reconciliation_prop_test.go` 2; `processor/rule/lifecycle_runtime_test.go` 4; `service/metrics_forwarder_test.go` 4. The exact query was `synctest|testing/synctest|clockwork|FakeClock|fakeClock|ManualClock|NewFakeClock`; the matches include the local `model.fakeClock` and Go `testing/synctest` usage. It returned no `clockwork`, `ManualClock`, or `NewFakeClock` matches.

## Searches
- `git rev-parse HEAD` → 3dc4ccbef32e87096c6d998fde7e76e896cf2f3c.
- `sed -n '1,63p' openspec/project.md` → Purpose and Product Boundary read as required.
- `git grep -n '^## Purpose\|^## Product Boundary\|^# Purpose\|^# Product Boundary' -- openspec/project.md` → 2.
- `gopls workspace_symbol -matcher=fuzzy 'TestHelper'` → workspace load failed, no symbols returned.
- `gopls workspace_symbol -matcher=fuzzy 'Lifecycle'` → workspace load failed, no symbols returned.
- `gopls workspace_symbol -matcher=fuzzy 'Dispatcher'` → workspace load failed, no symbols returned.
- `gopls workspace_symbol -matcher=fuzzy 'Eventually'` → workspace load failed, no symbols returned.
- `git grep -n 'package test\|func New.*Test\|func .*Eventually\|func .*Wait\|func .*Ready' -- internal/test* test/helpers natsclient` → shell glob failed before git grep.
- `git grep -n '^func Test\|Eventually\|time.After\|time.Sleep\|WaitGroup' -- pkg/lifecycle` → matches; output truncated, exact hit count not retained.
- `git grep -n '^func Test\|Eventually\|time.After\|time.Sleep\|WaitGroup' -- pkg/dispatch` → matches; output truncated, exact hit count not retained.
- `git grep -n 'Eventually\|time.After\|time.Sleep\|WaitGroup\|poll\|Poll\|ready\|Ready' -- test/e2e` → matches; output truncated, exact hit count not retained.
- `git grep -n 'synctest\|fake clock\|Eventually\|time.Sleep\|timeout\|synchron' -- docs/contributing/01-testing.md docs/concepts openspec/specs` → matches; output truncated, exact hit count not retained.
- `jq '.. | objects | select((.number? == 1397) or ((.title? // "") | test("flake|test|timeout|async|race"; "i"))) | {number,title,state,body,url}' flake-issues.json test-issue-index.json` → matching records; output truncated.
- `git ls-files '*_test.go' | wc -l` → 1,351.
- `git grep -n -F 'time.Sleep(' -- '*_test.go'` → 446 lines / 117 files.
- `git grep -n -F 'time.After(' -- '*_test.go'` → 346 lines / 126 files.
- `git grep -n -F 'time.NewTimer(' -- '*_test.go'` → 7 lines / 5 files.
- `git grep -n -F 'require.Eventually(' -- '*_test.go'` → 213 lines / 75 files.
- `git grep -n -F 'sync.WaitGroup' -- '*_test.go'` → 106 lines / 73 files.
- `git grep -n -F 'wg.Wait()' -- '*_test.go'` → 107 lines / 75 files.
- `git grep -n -F 'context.WithTimeout(' -- '*_test.go'` → 325 lines / 136 files.
- `git grep -n -F 't.Context()' -- '*_test.go'` → 378 lines / 51 files.
- `git grep -n -E 'func (TestContext|TestCtx|NewTest|WaitFor|Eventually|EventuallyWithT|RequireEventually|WaitFor.*Ready|Await|Poll)' -- '*_test.go' 'internal/test*' 'test/helpers/**'` → 0.
- `git grep -n -E 'context\.WithTimeout|context\.WithCancel|t\.Context\(\)|context\.Background\(\)' -- '*_test.go'` → matches; exact count not retained.
- `git grep -n -E 'synctest|testing/synctest|clockwork|FakeClock|fakeClock|ManualClock|NewFakeClock' -- '*.go'` → 23 lines / 5 files.
- `git grep -n -E 'WaitFor|await|Eventually|synctest|testContext|NewTestClient|readyCh|doneCh|started|startedCh|startedOnce' -- internal test/helpers natsclient pkg/lifecycle pkg/dispatch` → matches; output truncated.
- `git grep -n -E '^func (NewTestClient|NewSharedTestClient|testContext|TestContext|WaitFor|WaitUntil|Eventually|EventuallyWithT|Await|waitFor)' -- natsclient/*test.go test/helpers internal/*test*` → 6 matching lines.
- `git grep -n -E '^func NewTestClient|^func NewSharedTestClient|WaitForConnection|WaitForBucket|WaitForStartup|func TestContext' -- natsclient/client.go natsclient/test_client.go natsclient/client_test.go natsclient/test_client_readiness_test.go` → 13 matching lines.
- `git grep -n -E '^func WaitFor|^func \(.*\) WaitFor|^func \(.*\) waitFor|^func \(.*\) await|PollInterval|last observed|attempts' -- test/e2e/client test/e2e/scenarios` → matches; output truncated.
- `git grep -n -E 'close\(.*ready|close\(.*done|<-.*ready|<-.*done|synctest\.Wait|require\.Eventually' -- pkg/dispatch/*_test.go pkg/lifecycle/*_test.go natsclient/*test.go test/testinfra/*test.go` → matches; output truncated.
- `git grep -n -E 'time\.Sleep\(|time\.After\(|time\.NewTimer\(' -- pkg/dispatch/*_test.go pkg/lifecycle/*_test.go natsclient/*test.go test/testinfra/*test.go` → matches; output truncated.
- `git grep -n -E 'MUST NOT use `time.Sleep`|Polling MUST|last observed value|Test I/O contexts|Go test runner cancels|20m is a per-package|25-minute outer job' -- docs/contributing/01-testing.md` → 5.
- `git grep -n -E 'time\.Sleep|Eventually|wait.*(ready|complete)|deadline|cancel|diagnostic|last observed|timeout' -- openspec/specs/testing* openspec/specs/test*` → shell glob failed before git grep.
- `git grep -n -E 'func Test.*(Reap|Timeout|Stop|Wait|Readiness|Watch|Async|Drain)|t\.Deadline\(|readPipeSignal|waitFor.*(ready|Ready|completion|Complete)' -- test/testinfra pkg/dispatch pkg/lifecycle natsclient` → matches; output truncated.
- `jq '.[0] | keys' flake-issues.json` → 10 issue fields; `jq '.[0] | keys' test-issue-index.json` → 7 issue fields.
- `jq -r '.[] | select((.number|tostring) == "1397" or (.title|test("test|flake|async|timeout|wait|poll|race|hang";"i"))) | [.number,.title] | @tsv' flake-issues.json` → 18 records.
- `git grep -n -E 'synctest|testing/synctest|clockwork|FakeClock|fakeClock|ManualClock|NewFakeClock' -- '*.go'` → 23 lines / 5 files (re-run to establish exact count).
- `git grep -n -E 'started chan struct\{|close\(b\.started\)|<-handle\.started|close\(handle\.release\)|<-done|context\.WithTimeout\(ctx|WaitForConnection\(connectCtx\)|ctx\.Done\(\)|return nil$|time\.Sleep\(20 \* time.Millisecond\)|readPipeSignal\(t, parentReadyReader|WaitForMetric\(' -- natsclient/jetstream_metrics_test.go natsclient/publish_async_integration_test.go natsclient/test_client.go pkg/dispatch/dispatcher_test.go test/testinfra/integration_runner_contract_test.go test/e2e/client/metrics.go` → matches.
- `go env GOROOT` → `/usr/local/go`; `ls -ld /usr/local/go/src/unsafe` → directory exists.
- `sed` excerpts used for source pins: `docs/contributing/01-testing.md:281-305`, `:410-495`, `:535-550`; `natsclient/test_client.go:760-870`; `natsclient/test_client_readiness_test.go:1-90`; `natsclient/jetstream_metrics_test.go:150-180`; `natsclient/publish_async_integration_test.go:120-145`; `service/metrics_forwarder_test.go:219-270`; `pkg/dispatch/dispatcher_test.go:131-160`; `pkg/dispatch/keyed_pool_test.go:296-340`; `pkg/lifecycle/watch_atomic_bootstrap_test.go:90-145`; `test/e2e/scenarios/http_gateway_readiness.go:138-182`; `test/e2e/scenarios/validate_infra.go:712-750`; `test/testinfra/integration_runner_contract_test.go:205-355`, `:790-910`; `model/breaker_test.go:1-33`.

## Sampling and blind spots
The queried gopls commands each reported `package unsafe is not in std (/usr/local/go/src/unsafe)` while `go env GOROOT` resolved to `/usr/local/go` and that directory exists. The cause of the gopls workspace-load failure is unresolved. Broad package/e2e searches produced truncated output; their exact hit counts are marked as not retained. The wait-group counts are lexical and do not identify each variable spelling or whether an individual wait is bounded elsewhere. Named polling spellings do not enumerate every custom retry loop. The line-pinned sample is bounded.


=== rule-hang-inventory.md ===

# Inventory: rule Stop, runtime command fence, and teardown hang surface
base: 3dc4ccbef32e87096c6d998fde7e76e896cf2f3c

## Claimed gap
- `processor/rule/processor.go:1266` — `func (rp *Processor) Stop(ctx context.Context) error {`
- `processor/rule/processor.go:1319` — `func (rp *Processor) cleanup(ctx context.Context) error {`
- `processor/rule/processor.go:1322` — `barrier := rp.fenceRuntimeCommands()`
- `processor/rule/processor.go:1329` — `stopErrors := []error{settleRuntimeCommandFence(ctx, barrier, cancel, coordinatorDone)}`
- `processor/rule/processor.go:1503` — `if coordinatorDone != nil {`
- `processor/rule/processor.go:1504` — `<-coordinatorDone`
- `processor/rule/processor.go:1502` — `barrierErr := <-barrier`
- `processor/rule/processor.go:622` — `func (rp *Processor) submitRuntimeCommand(run func(context.Context) error) error {`
- `processor/rule/processor.go:644` — `return <-command.result`
- `processor/rule/processor.go:588` — `defer close(rp.coordinatorDone)`
- `processor/rule/processor.go:611` — `func (rp *Processor) failQueuedRuntimeCommands(err error) {`
- `processor/rule/processor.go:615` — `rp.commandMu.Unlock()`
- `processor/rule/processor.go:647` — `func (rp *Processor) fenceRuntimeCommands() <-chan error {`
- `processor/rule/processor.go:661` — `rp.commands = append(rp.commands, barrier)`
- `openspec/specs/component-lifecycle/spec.md:34` — `A successfully running component's Stop MUST be caller-bounded.`
- `openspec/specs/component-lifecycle/spec.md:40` — `#### Scenario: Stop bound wins`

## Spellings of the fact
- `processor/rule/processor.go:592` — `rp.failQueuedRuntimeCommands(ctx.Err())`
- `processor/rule/processor.go:596` — `rp.commandMu.Lock()`
- `processor/rule/processor.go:598` — `rp.commandMu.Unlock()`
- `processor/rule/processor.go:603` — `rp.commandMu.Unlock()`
- `processor/rule/processor.go:612` — `rp.commandMu.Lock()`
- `processor/rule/processor.go:615` — `rp.commandMu.Unlock()`
- `processor/rule/processor.go:624` — `rp.commandMu.Lock()`
- `processor/rule/processor.go:629` — `if rp.coordinatorDone != nil {`
- `processor/rule/processor.go:631` — `case <-rp.coordinatorDone:`
- `processor/rule/processor.go:632` — `rp.commandMu.Unlock()`
- `processor/rule/processor.go:639` — `rp.commandMu.Unlock()`
- `processor/rule/processor.go:649` — `rp.commandMu.Lock()`
- `processor/rule/processor.go:651` — `if rp.coordinatorDone != nil {`
- `processor/rule/processor.go:653` — `case <-rp.coordinatorDone:`
- `processor/rule/processor.go:654` — `rp.commandMu.Unlock()`
- `processor/rule/processor.go:663` — `rp.commandMu.Unlock()`
- `processor/rule/processor.go:1485` — `func settleRuntimeCommandFence(`
- `processor/rule/processor.go:1487` — `barrier <-chan error,`
- `processor/rule/processor.go:1489` — `coordinatorDone <-chan struct{},`
- `processor/rule/processor.go:1503` — `if coordinatorDone != nil {`
- `processor/rule/processor.go:1504` — `<-coordinatorDone`
- `processor/rule/processor.go:1518` — `rp.coordinatorDone = nil`
- `processor/rule/cron_scheduler.go:340` — `func (s *CronScheduler) fenceDispatch() <-chan error {`
- `processor/rule/cron_scheduler.go:344` — `if s.dispatchDone == nil {`
- `processor/rule/cron_scheduler.go:351` — `case <-s.dispatchDone:`
- `processor/rule/cron_scheduler.go:358` — `s.dispatchQueue = append(s.dispatchQueue, barrier)`
- `processor/rule/cron_scheduler.go:283` — `defer close(s.dispatchDone)`
- `processor/rule/cron_scheduler.go:496` — `barrier := s.fenceDispatch()`
- `processor/rule/cron_scheduler.go:497` — `nativeStop := s.cron.Stop()`
- `processor/rule/cron_scheduler.go:500` — `<-barrier`
- `processor/rule/cron_scheduler.go:505` — `<-dispatchDone`
- `processor/rule/cron_scheduler.go:507` — `close(stopDone)`
- `processor/rule/cron_scheduler.go:472` — `func (s *CronScheduler) Stop() context.Context {`
- `processor/rule/cron_scheduler.go:496` — `barrier := s.fenceDispatch()`
- `processor/rule/cron_scheduler.go:498` — `go func() {`
- `processor/rule/cron_scheduler.go:499` — `<-nativeStop.Done()`
- `processor/rule/cron_scheduler.go:500` — `<-barrier`
- `processor/rule/cron_scheduler.go:504` — `if dispatchDone != nil {`
- `processor/rule/cron_scheduler.go:505` — `<-dispatchDone`
- `processor/rule/processor.go:1345` — `cronDone = cronScheduler.Stop().Done()`
- `processor/rule/readiness_integration_test.go:171` — `func TestIntegration_RuleStopAfterAcceptedStartParentCancellation(t *testing.T) {`
- `processor/rule/readiness_integration_test.go:193` — `stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)`
- `processor/rule/readiness_integration_test.go:196` — `stopErr = processor.Stop(stopCtx)`
- `processor/rule/readiness_integration_test.go:197` — `}, "abort Stop must remain a synchronous bounded lifecycle call")`
- `processor/rule/lifecycle_runtime_test.go:9` — `"testing/synctest"`
- `processor/rule/lifecycle_runtime_test.go:26` — `synctest.Test(t, func(t *testing.T) {`
- `processor/rule/lifecycle_runtime_test.go:56` — `stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)`
- `processor/rule/lifecycle_runtime_test.go:93` — `synctest.Test(t, func(t *testing.T) {`
- `processor/rule/lifecycle_runtime_test.go:120` — `synctest.Wait()`
- `service/config_boot_process_restart_integration_test.go:86` — `cmd := exec.CommandContext(ctx, executable, "-test.run=^TestConfigBootProcessRestartHelper$", "-test.v")`
- `test/testinfra/integration_runner_contract_test.go:472` — `command := exec.Command("/bin/sh", "-c", "exec /bin/sleep 30")`
- `test/testinfra/integration_runner_contract_test.go:481` — `if err := waiter.wait(10 * time.Millisecond); !errors.As(err, &timeoutErr) {`
- `test/testinfra/integration_runner_contract_test.go:484` — `if err := waiter.killAndWait(); err == nil {`
- `test/testinfra/integration_runner_contract_test.go:488` — `t.Fatalf("cleanup returned before command was reaped: state=%v", command.ProcessState)`
- `scripts/run-integration-tests.sh:329` — `-timeout=20m`

## Adjacent claims
- #1283 — `fix(rule): a bounded Stop can block forever on an orphaned fence barrier, violating component-lifecycle spec:34`. Its issue body states two separate unbounded receive sites: `submitRuntimeCommand` waiting on `command.result`, and `settleRuntimeCommandFence` waiting on `barrier` after its context fires. It describes the exit window where `failQueuedRuntimeCommands` releases `commandMu` after the final drain while `defer close(coordinatorDone)` has not yet run; a fence may append after that drain. The issue also records `ApplyConfigUpdate` as an exported path to `submitRuntimeCommand`, and identifies the readiness integration assertion as the bounded-Stop contract check.
- #1283 — recorded run `36343596313`, exact run head SHA `ada5c46`: package `internal/boot` failed after `1200.059s`; test `TestRootRuleManagerHotReloadsIntoTheProcessor` ran `19m59s`. The captured stack has test cleanup `startedRuleProcessor` at `internal/boot/rule_hot_reload_integration_test.go:72`, which calls `proc.Stop(context.Background())`; `Processor.cleanup` is waiting at `processor/rule/processor.go:1394`. A second captured goroutine is in `CronScheduler.Stop` at `processor/rule/cron_scheduler.go:500`, waiting on the scheduler dispatch barrier. At the exact run head, cleanup line 1394 selects on `done`/`ctx.Done`, and the cron Stop settlement goroutine waits on native Stop, then its barrier, then dispatch completion. This stack is distinct from the rule runtime-command fence receive at `settleRuntimeCommandFence:1502`; it records a cron settlement wait under a teardown call whose context is `Background`.
- Run `36343596313` consumer detail: the exact run head `ada5c46` contains `internal/boot/rule_hot_reload_integration_test.go:72`, `t.Cleanup(func() { _ = proc.Stop(context.Background()) })`; this file is absent at base `3dc4ccbe`.
- Run `36330545049`, source SHA `b2ef95c903db56e9d08e22768fe66d1e14cc3b23`: the Test job ran from `15:42:34` to cancellation at `16:07:40`; the last package completion in the saved test log is `processor/research-graph-synthesize` at `15:52:52`. Cleanup reports orphan `bash`, `go`, and `rule.test` PID 95570. The saved log has no timeout stack or test name for that process; its relationship to #1283 and the blocking code path are unproven by this artifact.
- #1064 — `test lifecycle: audit and guard unbounded Stop cleanup roots`. Saved issue body reports an audit population of 517 `Stop(context.Background())` calls and 295 same-line defer/Cleanup sites across 90 files; it calls for AST/type-aware classification, stable manifest identities, and a guard for newly introduced unbounded lifecycle cleanup roots. It references #1062 as the immediate four-site cleanup fix and records two Rule readiness calls changed from five-second Stop budgets to `context.Background()` in commit `61fbd48f`.
- #1283 refs #1273, #1274, #1159, #1146 (as listed in saved issue body).
- No active OpenSpec changes were listed by `openspec list`.
- Draft/open PR body inventory and live open-issue query were unavailable: GitHub API connection failed. Saved issue bodies for #1283 and #1064 were available locally.

## Consumers
- `processor/rule/runtime_config.go:14` — `func (rp *Processor) ApplyConfigUpdate(changes map[string]any) error {`
- `processor/rule/runtime_config.go:37` — `return rp.submitRuntimeCommand(func(ctx context.Context) error {`
- `processor/rule/entity_watcher.go:314` — `return rp.submitRuntimeCommand(func(ctx context.Context) error {`
- `processor/rule/processor.go:1322` — `barrier := rp.fenceRuntimeCommands()`
- `processor/rule/lifecycle_owner_test.go:159` — `if err := processor.submitRuntimeCommand(func(commandCtx context.Context) error {`
- `processor/rule/lifecycle_owner_test.go:167` — `barrier := processor.fenceRuntimeCommands()`
- `processor/rule/readiness_integration_test.go:197` — `}, "abort Stop must remain a synchronous bounded lifecycle call")`

## Problem shape
- `processor/rule/lifecycle_runtime_test.go:26` — `synctest.Test(t, func(t *testing.T) {`
- `processor/rule/lifecycle_runtime_test.go:56` — `stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)`
- `processor/rule/lifecycle_runtime_test.go:58` — `stopErr := processor.Stop(stopCtx)`
- `processor/rule/readiness_integration_test.go:193` — `stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)`
- `processor/rule/readiness_integration_test.go:196` — `stopErr = processor.Stop(stopCtx)`
- `service/config_boot_process_restart_integration_test.go:85` — `ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)`
- `service/config_boot_process_restart_integration_test.go:86` — `cmd := exec.CommandContext(ctx, executable, "-test.run=^TestConfigBootProcessRestartHelper$", "-test.v")`
- `test/testinfra/integration_runner_contract_test.go:481` — `waiter.wait(10 * time.Millisecond)`
- `test/testinfra/integration_runner_contract_test.go:484` — `waiter.killAndWait()`
- `scripts/run-integration-tests.sh:329` — `-timeout=20m`

## Searches
- `git rev-parse HEAD` → `3dc4ccbef32e87096c6d998fde7e76e896cf2f3c`
- `gopls workspace_symbol -matcher=fuzzy Stop` → unavailable; workspace load failed (`package unsafe is not in std`)
- `gopls workspace_symbol -matcher=fuzzy cleanup` → unavailable; workspace load failed (`package unsafe is not in std`)
- `gopls workspace_symbol -matcher=fuzzy startedRuleProcessor` → unavailable; workspace load failed (`package unsafe is not in std`)
- `gopls references processor/rule/processor.go:1266:25` → unavailable; workspace load failed, no package metadata
- `gopls call_hierarchy processor/rule/processor.go:1266:25` → unavailable; workspace load failed, no package metadata
- `git grep -n -E 'settleRuntimeCommandFence|fenceRuntimeCommands|submitRuntimeCommand|failQueuedRuntimeCommands|coordinatorDone|startedRuleProcessor|TestRootRuleManagerHotReloadsIntoTheProcessor|Stop bound wins|caller-bounded|caller bounded|synctest|CronScheduler.*Stop|func \(.*\) Stop\(' -- processor internal/boot pkg/lifecycle openspec/specs docs/adr .agents/contracts` → 80 lines
- `git grep -n -F 'ApplyConfigUpdate' -- processor/rule` → 35
- `git grep -n -F 'settleRuntimeCommandFence' -- processor/rule` → 2
- `git grep -n -F 'fenceRuntimeCommands' -- processor/rule` → 3
- `git grep -n -F 'func (rp *Processor) cleanup' -- processor/rule` → 1
- `git grep -n -F 'TestRootRuleManagerHotReloadsIntoTheProcessor' -- internal/boot` → 0 on base; present at PR SHA `a319048`
- `git grep -n -F 'Stop MUST be caller-bounded' -- openspec/specs/component-lifecycle/spec.md` → 1
- `git grep -n -F 'Stop bound wins' -- openspec/specs/component-lifecycle/spec.md` → 3
- `git grep -n -F 'synctest.Test' -- processor/rule/lifecycle_runtime_test.go` → 2
- `git grep -n -F 'Stop' -- processor/rule/readiness_integration_test.go` → 11
- `git grep -n -F 'func (rp *Processor) Stop' -- processor/rule/processor.go` → 1
- `git grep -n -F 'func (rp *Processor) submitRuntimeCommand' -- processor/rule/processor.go` → 1
- `git grep -n -F 'func (rp *Processor) fenceRuntimeCommands' -- processor/rule/processor.go` → 1
- `git grep -n -F 'func (rp *Processor) failQueuedRuntimeCommands' -- processor/rule/processor.go` → 1
- `git grep -n -F 'func settleRuntimeCommandFence' -- processor/rule/processor.go` → 1
- `git grep -n -F 'func (s *CronScheduler) Stop' -- processor/rule/cron_scheduler.go` → 1
- `git grep -n -F 'func startedRuleProcessor' -- internal/boot/rule_hot_reload_integration_test.go` → 0 on base; present at PR SHA `a319048`
- `git grep -n -F 'func TestIntegration_RuleStopAfterAcceptedStartParentCancellation' -- processor/rule/readiness_integration_test.go` → 1
- `git grep -n -F 'failQueuedRuntimeCommands' -- processor/rule` → 2
- `git grep -n -F 'coordinatorDone' -- processor/rule/processor.go` → 12
- `git grep -n -F 'commandMu' -- processor/rule/processor.go` → 13
- `git grep -n -F 'barrier <-chan error' -- processor/rule/processor.go` → 1
- `git grep -n -F 'stopCtx, stopCancel := context.WithTimeout' -- processor/rule/readiness_integration_test.go` → 3
- `git grep -n -F 'cronDone' -- processor/rule/processor.go` → 4
- `git grep -n -F 'settleRuntimeCommandFence(ctx' -- processor/rule/processor.go` → 1
- `git grep -n -F 'ApplyConfigUpdate' -- processor/rule` → 35
- `git grep -n -F 'Stop MUST be caller-bounded' -- openspec/specs/component-lifecycle/spec.md` → 1
- `git grep -n -F 'Stop bound wins' -- openspec/specs/component-lifecycle/spec.md` → 3
- `git grep -n -F 'abort Stop must remain a synchronous bounded lifecycle call' -- processor/rule/readiness_integration_test.go` → 1
- `git grep -n -F 'synctest' -- processor/rule/lifecycle_runtime_test.go` → 4
- `git grep -n -F 'startedRuleProcessor' -- internal/boot` → 0 on base; present at PR SHA `a319048`
- `git grep -n -F 'TestRootRuleManagerHotReloadsIntoTheProcessor' -- internal/boot` → 0 on base; present at PR SHA `a319048`
- `git grep -n -F 'exec.CommandContext' -- '*_test.go'` → 2
- `git grep -n -F 'exec.Command(' -- '*_test.go'` → 14
- `git grep -n -F 'go test -timeout' -- .github scripts taskfile.yml Taskfile.yml` → 0
- `git grep -n -F 'ProcessState' -- '*_test.go'` → 6
- `git grep -n -F 'context.WithTimeout' -- processor/rule/*_test.go` → 23
- `git grep -n -E -- '-timeout|timeout=' -- scripts .github taskfile.yml Taskfile.yml` → 1
- `git grep -n -F 'exec.CommandContext' -- '*_test.go'` → 2
- `git grep -n -F 'ProcessState' -- '*_test.go'` → 6
- `git grep -n -F 'abort Stop must remain a synchronous bounded lifecycle call' -- processor/rule/readiness_integration_test.go` → 1
- `git grep -n -F 'synctest' -- processor/rule/lifecycle_runtime_test.go` → 4
- `git grep -n -F 'startedRuleProcessor' -- internal/boot` → 0 on base; present at PR SHA `a319048`
- `git grep -n -F 'TestRootRuleManagerHotReloadsIntoTheProcessor' -- internal/boot` → 0 on base; present at PR SHA `a319048`
- `git grep -n -F 'exec.CommandContext' -- '*_test.go'` → 2
- `git grep -n -F 'ProcessState' -- '*_test.go'` → 6
- `git grep -n -F 'Stop() context.Context' -- processor/rule/cron_scheduler.go` → 1
- `git grep -n -F '#1283' -- openspec/specs openspec/changes docs/adr docs/operations .agents/contracts` → 0
- `git grep -n -F '#1064' -- openspec/specs openspec/changes docs/adr docs/operations .agents/contracts` → 0
- `git grep -n -E 'FAIL|1200\.059|Test.*Hot|goroutine .*\[select|cleanup\(' /private/tmp/semstreams-test-audit-20260928/run-36343596313-failed.log` → 4
- `rg -n -i 'cancel|timeout|orphan|PID.?95570|15:52:52|16:07:40|internal/boot|processor/rule|FAIL' /private/tmp/semstreams-test-audit-20260928/run-36330545049-failed.log` → 0
- `rg -n '15:52:52|16:07:40|go test|rule\.test|95570|Orphan|orphan|Cancelled|cancelled|duration' /private/tmp/semstreams-test-audit-20260928/run-36330545049-test.log` → 7
- `find /private/tmp/semstreams-test-audit-20260928 -maxdepth 2 -type f -print` → 23 files
- `gh issue list --search 'rule Stop fence' --state open --json number,title` → unavailable (GitHub API connection error)
- `openspec list` → 0 active changes
- `gh pr list --state open --limit 100 --json number,title,body` → unavailable (GitHub API connection error)


=== policy-bound-inventory.md ===

# Inventory supplement: policy and execution bounds
base: 3dc4ccbef32e87096c6d998fde7e76e896cf2f3c

## Problem shape
- `docs/contributing/01-testing.md:299` — `per-package value is transitional and is not a budget for a new test.`
- `docs/contributing/01-testing.md:438` — `Polling MUST have a narrow deadline and report the last value and last error on failure. It MUST NOT silently turn a`
- `docs/contributing/01-testing.md:439` — `missing producer, subscriber, or component into a full-window timeout.`
- `docs/contributing/01-testing.md:467` — `| Unit top-level test under `-race` | 1s | 5s |`
- `docs/contributing/01-testing.md:468` — `| Integration top-level test on a warm host | 30s | 3m |`
- `docs/contributing/01-testing.md:472` — `| Integration package | 3m | 5m |`
- `test/testinfra/integration_runner_contract_test.go:855` — `deadline = time.Now().Add(10 * time.Minute) // go test's default -timeout`
- `test/testinfra/integration_runner_contract_test.go:857` — `return time.Until(deadline) - 5*time.Second`

The policy requires bounded diagnostic observation; the runner-test completion helper inherits the test-binary deadline.
The existence of a larger execution deadline does not establish prompt detection of a missing producer.

## Searches
- Architect independent git grep over synchronization/budgets in testing policy, runner and contract tests: 49 lines.
- Architect independently read integration_runner_contract_test.go:804-930; parent materialized exact source pins.


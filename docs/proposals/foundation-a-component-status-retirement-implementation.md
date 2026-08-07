# Foundation A component-status retirement: implementation report

**Status:** implementation and local release gates complete. The first independent SemStreams implementation review
returned `FAIL`; every reported finding was addressed, independent re-review returned `PASS/APPROVE`, and the final
post-remediation/post-review `task check:push` rerun passed. No merge is claimed here.
**Implementation baseline:** `7cff48215b035c2c08918f9f58f9aa45bc596c78`.
**Authority:** post-R1c roadmap section 7 and the owner approval recorded in
`post-r1c-foundation-remap-roadmap-approval.md`.

## Implemented slice

Foundation A cleanly deletes the redundant component-stage diagnostic plane without replacing or renaming it:

- deleted `component.Status`, the reporter interface, KV/no-op/throttled implementations, configuration, and catalog
  acquisition helper;
- deleted all 24 production reporter fields, 24 catalog-reporter construction sites, two component-local no-op
  constructions, 53 production stage reports, and three graph-clustering cycle reports;
- removed the diagnostic bucket constant and descriptor, its direct catalog/write-policy tests, and the unused E2E
  client helper cluster;
- corrected current documentation and graph-retention specification truth; and
- corrected the graph-readiness catalog owner text to the four present producers.

The diff adds no exported production symbol, configuration key, port, subject, bucket, alias, deprecated API, or
compatibility path. A structural contract prevents the deleted production/current-truth vocabulary from returning
while deliberately excluding historical ADRs, proposals, and archived OpenSpec evidence.

## Preserved boundaries

- Component lifecycle control and health remain: `component.State`, `LifecycleComponent`, and `ManagedComponent` are
  unchanged; ComponentManager still reads `Discoverable.Health()` at `service/component_manager.go:2183-2192`.
- Graph readiness remains: the four producer keys remain at `graph/readiness/watcher.go:51-62`; publisher, watcher,
  outcome, freshness, and policy behavior are unchanged.
- Domain lifecycle remains: `pkg/lifecycle.Participant` and `pkg/lifecycle.Manager` remain at
  `pkg/lifecycle/participant.go:29` and `pkg/lifecycle/manager.go:47`.
- Generic message-logger KV access remains caller-selected and must-exist. Exact query acquisition remains at
  `service/message_logger_http.go:484`; watch acquisition remains at `service/message_logger_kv_watch.go:197-198`.
  Neither path creates a bucket. A real-NATS production-seam test covers query and watch against both a framework
  bucket and a product bucket, then proves both missing-bucket reads leave the bucket absent.

## Ruled-change conformance

| ID | Foundation A ruling | Result |
|---|---|---|
| A1 | Delete the full diagnostic plane [E1] | CONFORMS |
| A2 | Preserve ComponentManager health [E2] | CONFORMS |
| A3 | Preserve graph readiness and all four producer keys [E3] | CONFORMS |
| A4 | Preserve domain lifecycle [E4] | CONFORMS |
| A5 | Preserve generic must-exist message-logger query/watch [E5] | CONFORMS |
| A6 | Make a clean exported break with no replacement [E6] | CONFORMS |
| A7 | Stop if a dedicated correctness consumer is found [E7] | CONFORMS; not triggered |

Evidence is line-addressable and reproducible as follows:

- **E1:** At baseline `7cff4821`, `component/lifecycle.go:110-137` defined the retired types;
  `component/lifecycle_reporter.go:1-339` and `component/lifecycle_reporter_catalog.go:1-48` implemented the plane;
  `graph/constants.go:47` and `graph/kvcatalog.go:103-118` declared its bucket. The current guard is
  `test/contract/component_status_retirement_contract_test.go:12-76`. Run the baseline diff commands below.
- **E2:** `component/lifecycle.go:10-108` retains `State`, `LifecycleComponent`, and `ManagedComponent`;
  `service/component_manager.go:2182-2195` still calls each component's `Health()`. Reproduce with
  `task check:push` and the focused component/service tests below.
- **E3:** `graph/readiness/watcher.go:40-70` retains `GRAPH_STATUS` and all four producer keys;
  `graph/kvcatalog.go:69-74` names those producers and preserves history 3. Reproduce with the focused readiness test
  and `task e2e:core` below.
- **E4:** `pkg/lifecycle/participant.go:29-39` retains `Participant`; `pkg/lifecycle/manager.go:47-57` retains `Manager`
  and its exact-reader/current-state boundary. Reproduce with `task check:push`.
- **E5:** Query remains lookup-only at `service/message_logger_http.go:470-490`; watch remains lookup-only at
  `service/message_logger_kv_watch.go:195-203`. Real-NATS proof is
  `service/message_logger_kv_acquisition_integration_test.go:18-145`; run its focused tagged command below.
- **E6:** `test/contract/component_status_retirement_contract_test.go:12-76` rejects every retired exported spelling
  and current-truth phrase while excluding history. Run its focused command and the exact search below.
- **E7:** The accepted inventory at `docs/proposals/post-r1c-foundation-remap-inventory.md:170-200` records no dedicated
  reader and accounts for message-logger; the stop rule is
  `docs/proposals/post-r1c-foundation-remap-roadmap.md:183-187`. Re-run those recorded searches and the current search.

No deviation was required.

## Independent review status

The first independent implementation review returned `FAIL`, not approval. Its concrete findings are addressed:

- removed graph-query's now-unused concrete `rawNATSClient` field and assignment;
- removed stale lifecycle-reporting claims from UDP and graph-query current documentation;
- corrected message-logger watch and KV descriptor/retention comments to current behavior;
- extended the retirement guard to the exact stale current-truth phrases; and
- added real-NATS production-seam query/watch/no-create coverage for framework and product buckets.

Independent re-review returned `PASS/APPROVE`. The first-review failure and remediation record above remains part of the
implementation history. The final post-remediation/post-review `task check:push` rerun passed; no PR-ready transition or
merge is claimed here.

## Reproducible evidence so far

Red test observed before implementation:

```text
go test ./test/contract -run TestComponentStatusPlaneRemainsRetired -count=1
=> FAIL with production/current-truth violations for the complete retired surface
```

Focused post-implementation evidence:

```text
go test ./test/contract -run TestComponentStatusPlaneRemainsRetired -count=1
=> PASS

go test ./component ./graph ./gateway/http ./gateway/graph-gateway ./input/file ./input/udp \
  ./output/file ./output/httppost ./output/websocket ./storage/objectstore \
  ./processor/graph-index ./processor/graph-index-spatial ./processor/graph-index-temporal \
  ./processor/graph-embedding ./processor/graph-ingest ./processor/graph-query \
  ./processor/graph-clustering ./processor/json_filter ./processor/json_generic ./processor/json_map \
  ./processor/research-graph-assess ./processor/research-graph-classify \
  ./processor/research-graph-execute ./processor/research-graph-route \
  ./processor/research-graph-synthesize ./processor/rule
=> PASS

go test ./service ./graph/readiness -count=1
=> PASS

go test -race -tags=integration ./service \
  -run TestIntegration_MessageLoggerReadsExistingKVBucketsWithoutCreating -count=1
=> PASS

go test -race ./test/contract ./processor/graph-query ./natsclient ./service -count=1
=> PASS

go vet ./service ./processor/graph-query ./natsclient ./test/contract
=> PASS

go tool revive -config revive.toml \
  ./service/... ./processor/graph-query/... ./natsclient/... ./test/contract/...
=> PASS

rg -n -S 'BucketComponentStatus|COMPONENT_STATUS|LifecycleReporter|ReportStage|ReportCycle' \
  --glob '!docs/adr/**' --glob '!docs/proposals/**' \
  --glob '!openspec/changes/archive/**' \
  --glob '!test/contract/component_status_retirement_contract_test.go' .
=> no output

git diff --check
=> PASS

openspec validate --all --strict
=> 36 passed, 0 failed

task check:push
=> PASS (final post-remediation/post-review rerun, exit 0; includes Docker race integration and
   TestIntegration_MessageLoggerReadsExistingKVBucketsWithoutCreating)

task e2e:core
=> PASS: 3/3 scenarios; teardown completed cleanly
```

Baseline deletion evidence:

```text
git diff --name-status 7cff48215b035c2c08918f9f58f9aa45bc596c78 -- \
  component/lifecycle.go component/lifecycle_reporter.go \
  component/lifecycle_reporter_catalog.go component/lifecycle_reporter_test.go \
  graph/constants.go graph/kvcatalog.go graph/kvcatalog_test.go test/e2e/client/nats.go
=> M component/lifecycle.go
   D component/lifecycle_reporter.go
   D component/lifecycle_reporter_catalog.go
   D component/lifecycle_reporter_test.go
   M graph/constants.go
   M graph/kvcatalog.go
   M graph/kvcatalog_test.go
   M test/e2e/client/nats.go

git diff --numstat 7cff48215b035c2c08918f9f58f9aa45bc596c78 -- \
  component/lifecycle.go component/lifecycle_reporter.go \
  component/lifecycle_reporter_catalog.go component/lifecycle_reporter_test.go \
  graph/constants.go graph/kvcatalog.go graph/kvcatalog_test.go test/e2e/client/nats.go
=> 0/29, 0/339, 0/48, 0/222, 3/4, 1/21, 3/15, 0/147 additions/deletions by listed path
```

All required local execution gates are green, the final `task check:push` rerun passed, and independent SemStreams
re-review returned `PASS/APPROVE`. No merge is claimed here.

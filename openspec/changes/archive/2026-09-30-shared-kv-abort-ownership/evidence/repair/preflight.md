# Repair preflight evidence

## Initial full gate and correction

`task check:push` on clean 089ba53b started at 2026-09-30 04:48:39.220 UTC. Lint, build, tagged vet,
schema drift and contract checks passed. The ordinary race suite then failed
`TestAuditRepositoryFullWithAbsoluteRootReportsRepositoryRelativeCandidates`: additions to the graph-ingest mock
had displaced two line-pinned entity-ID annotations. The coordinating session interrupted its remaining owned
process tree at 04:50:19.256 UTC, before integration. Recorded command duration was 100.215 seconds and final exit
status 201 reflects interruption; this does not replace or excuse the genuine audit failure in the retained log.
All eight recorded source hashes were unchanged. No integration result is claimed for this attempt.

The correction changes only annotation targets 789 to 826 and 891 to 928, both matching the 37 inserted lines.
Graph-ingest component_test.go now has SHA-256
`9f9fea7dfc27d5e891f4b44713cf0524c4530dafe5399e4d219fade344e58285`.
The narrow ordinary command passed, exit 0, package duration 15.649 seconds:

```bash
go test -race -count=1 -timeout=2m -run '^TestAuditRepositoryFullWithAbsoluteRootReportsRepositoryRelativeCandidates$' -v ./internal/entityidaudit
```

Exact failed-gate and correction logs, command metadata, abort process ownership, and the corrected source snapshot
are retained in verification/. Its raw-logs.zip also retains exact lint and focused native logs, which repository
ignore rules otherwise exclude. The final-source packet remains the earlier reviewed semantic source snapshot;
only these two annotation coordinates differ in the current graph-ingest source.

A new full gate remains required after independent correction review. This is a corrected deterministic failure,
not a retry of the unresolved hosted flake.

## Corrected full gate

The full `task check:push` passed on clean `4d55ebf0861e4f48de34bcff06785f1876edb0c7` from 2026-09-30T04:53:05.200465+00:00 through
2026-09-30T05:07:48.183741+00:00, exit 0, in 882.953 seconds. All eight recorded source hashes were unchanged.
Exact output and active process-monitor snapshots are in verification/final-gate-logs.zip; metadata and SHA-256
manifest are adjacent. The command ran lint/build/tagged vet/schema drift/contract checks, the ordinary race lane
and the canonical additive integration lane. Ordinary cached results remain labeled cached in the raw log.

The integration lane passed natsclient in 94.174 seconds, graph-index in 42.359 seconds, graph-ingest in 51.127 seconds,
and graph-query in 12.836 seconds. These are package durations, not individual regression timings. The separate
focused native run retains its exact 12.220-second result. No local E2E run or historical-cause attribution is claimed.

The earlier deterministic gate failure remains retained. The completed gate supports this revision's verification;
it does not waive the known unresolved hosted failure or close #1421. Final archive/spec reconciliation and its
narrow independent review follow this successful gate.

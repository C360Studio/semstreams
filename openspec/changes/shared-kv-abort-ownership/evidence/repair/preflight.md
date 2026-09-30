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

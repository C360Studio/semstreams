# #1222 bounded manifest persistence advice

Read-only architecture advice, not implementation approval or an owner ruling.

Baseline: `fe6e2cc03e16f5db47e293f55939548572f204cc`, with active edits. Accepted design SHA-256
`a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c` and its passed inventory checkpoint remain unchanged.

## Bounded inventory refresh

The existing surface and adopter inventories are incorporated unchanged through `inventory.md`, `adopter-seams.md`,
and frozen inventory manifest `584fa0db8b0d3fb44940578ec8dccf53c5b0642816827241e7a4bcf0152ec34a`.
This clarification concerns their existing persistence owner.

| Surface | Observed evidence |
|---|---|
| Aggregate persistence and acceptance | `test/e2e/results/writer.go:129` — `WriteRun` accepts a TestRun, finalizes proof and atomically writes aggregate JSON. It has no manifest-byte input. |
| Immutable artifact shape | `test/e2e/results/writer_task.go:171` — `WriteMember` validates initialization and retains bytes through temporary-file write, sync, close and no-clobber link. |
| Initialized snapshot | `test/e2e/results/writer_task.go:141` — `verifyInitializedSnapshot` compares stored identity/configuration/declarations and refuses a stored terminal run. |
| Correlated reference | `test/e2e/results/writer_task.go:27` — `ChildArtifact` already groups RunID, Path and SHA256. |
| Manifest producer | `cmd/e2e/report.go:63` — private `buildReportManifest` prepares constituent and application-phase observations; `:116` hashes observed files. |
| Reference acceptance | `test/e2e/results/writer.go:528` — required provenance checks reference/digest shapes. `:354` loads historical reports without reopening referenced files. |
| Live child verification | `test/e2e/results/writer_task.go:65` — currently validates and hashes report JSON; referenced manifest/log byte verification remains implementation work. |

The same-class owner stays Writer: aggregate, member and manifest persistence belong to the initialized run.
Task owns execution and shell capture; the reporter owns manifest preparation. No catalog, storage service,
recovery loop, expiry mechanism or additional execution owner is introduced.

Adopter refresh: the reporter author needs the initialized snapshot and validated serialized observations.
Writer supplies filename and digest. Omitting required manifest evidence leaves proof incomplete through existing
Writer acceptance. Errors are returned at runtime; callers need not predict run IDs, filenames or digests.
Private report finalization should contain the ordering so Task users do not carry it.

## Shape recommendation

Use a specific Writer operation and one shared reference value:

```go
// ArtifactReference identifies exact retained bytes belonging to one test run.
type ArtifactReference struct {
    RunID  string `json:"run_id"`
    Path   string `json:"path"`
    SHA256 string `json:"sha256"`
}

func (w *Writer) WriteManifest(
    initialized *TestRun,
    data []byte,
) (ArtifactReference, error)

func (w *Writer) VerifyChild(
    path string,
    expected ChildExpectation,
) (ArtifactReference, error)
```

Rename the uncommitted `ChildArtifact` to `ArtifactReference`; add no compatibility alias.
The immediate consumers are report-child and report-finalize. Both require exactly the same run-bound byte reference.
Child acceptance remains behavior of `VerifyChild`, rather than an additional meaning encoded by its return struct.
Sharing this value does not authorize `WriteArtifact`, arbitrary filenames or a generic artifact service.

Alternatives considered:

- **Do nothing:** leaves the accepted retained-manifest obligation unsatisfied.
- **Extend WriteRun:** requires a manifest-byte argument or transient TestRun field, couples aggregate replacement
  to immutable auxiliary-file creation, and complicates initial writes and permitted same-run terminal corrections.
  This is fewer method names but a broader contract.
- **Specific WriteManifest:** follows the existing WriteMember persistence shape with one immediate consumer.
  Recommended.
- **Generic WriteArtifact:** introduces arbitrary artifact naming and kinds without a present requirement.

## Validation and lifecycle

Writer validates nonnil input, safe existing run identity, schema, and the persisted initialized snapshot before
deriving the destination. It must not generate another run ID. Require absent completion/exit on the supplied snapshot
as well as the stored snapshot. Reuse the existing snapshot guard and private no-clobber persistence mechanics.

Derive one fixed basename, such as `e2e-manifest-<runID>.json`, inside Writer’s resolved output directory.
Return Path relative to that directory and SHA256 over the exact retained bytes. Report errors with the intended
absolute destination where available. Do not accept a caller-supplied destination or digest.

Reject empty or malformed JSON/non-object input as a serialization error; domain completeness remains the reporter’s
responsibility. Do not re-encode the supplied bytes or add a second manifest-schema interpreter inside Writer.

Persist synchronously through temporary write, sync, close and atomic no-clobber creation. A duplicate, even with
identical bytes, returns an error and preserves the existing file. Return a zero reference on failure; never return a
digest/path presented as successfully retained. No goroutine, stored context, retry service or crash-recovery promise
is needed.

The finalizer keeps the initialized declaration snapshot intact while loading observed members. After command,
cleanup and captured-log completion, it prepares the manifest and calls WriteManifest with that snapshot. It then
places the returned reference into the terminal aggregate’s existing Environment keys and calls WriteRun.

Manifest failure remains a report failure: preserve observed statuses and partial members, attempt failed terminal
aggregate serialization, and return nonzero. It must not suppress terminal writing merely because manifest writing
failed. A successfully written manifest followed by aggregate-write failure may remain unreferenced; the command
still fails. No cleanup or restart protocol is added.

Same-run terminal aggregate corrections remain permitted by the accepted handoff. They reuse the retained manifest;
WriteManifest does not overwrite it or become a correction-history mechanism.

## Responsibility boundary

**Task/CLI producers and private reporter preparation** observe selected paths and application identities, recompute
constituent digests, preserve distinct core production/fixture phases, validate applicable manifest content, and exclude
secrets. Use the initialized run’s resolved `Environment["output_dir"]`; do not trust a separately supplied output
location without comparing it. Missing observations remain explicit and prevent complete proof.

**Writer** owns run binding, safe destination, exact-byte retention, digest calculation, and aggregate acceptance.
Manifest persistence success establishes retained bytes, not authorship or truth of supplied application observations.

**Live child verification** additionally checks retained manifest/log bytes against their declared digests, resolving
references from the child report’s output location. Reuse byte-reference validation without interpreting application
phases or duplicating the private manifest schema. Missing or mismatched bytes refuse the child.

**Historical LoadRun** continues validating the report without reopening original-machine paths. This distinction is
already required by `implementation-handoff.md:207–213`.

Shell capture remains the log producer. Finish and close the captured log before hashing it; finalizer diagnostics
must not append to that already-digested file. Writer need not create or authenticate the log.

## Evidence and limits

Relevant invariants remain in the active `e2e-evidence` delta: Observation identity is preserved (`:119`),
Failure reaches the outer gate (`:143`), and Run evidence binds one invocation (`:178`).

Focused implementation controls should cover exact stored bytes/digest, wrong or terminal snapshot, duplicate
no-clobber preservation, manifest failure followed by retained failed aggregate, both core phases, and live child
refusal for missing/tampered referenced bytes while historical LoadRun remains usable. These controls were not run
by this advice.

Applied `orchestration-check`: Task retains execution authority; reporting and persistence add no coordinator.
No NATS communication, payload or graph-query skill is triggered.

Observed file hashes:

- `writer.go`: `497b469f43edc798c21d724f9d24770f39bb13adf2bc3dbf0a0165b5e32573d0`
- `writer_task.go`: `9cd44e3d888868570d272b7d131479693d228593244191bb093f8d1eb2ac8645`
- `report.go`: `8c4bce596af7b2b170ba67216728764b5b2b9e5784e38697f81780c950817555`

`gopls workspace_symbol -matcher=fuzzy Writer` failed workspace loading because the Go build cache was inaccessible;
its empty output is not absence evidence. Conclusions above use bounded direct code inspection and the accepted
inventory. No source edits, tests, gates or Git/GitHub mutations were performed.

Root should reconcile this concrete shape and obtain independent review before implementation. No new owner scope
ruling is identified unless the stated obligations change.

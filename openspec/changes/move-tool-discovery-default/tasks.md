# Tasks — move tool-discovery default

The address cutover, post-Foundation-B control/current-census amendments, startup-atomic correction, focused/full race
and integration gates, fresh corrected-tree E2Es, and final independent SemStreams correction review are complete and
green. The reviewer returned `APPROVE`; the only note is the nonblocking pre-existing promoted message-logger drift
already deferred to a separate documentation-truth correction. The change is merge-ready but has not merged.
Candidate selection has not begun; no product tag exists, and #827 has not executed. Issue #810 remains parked with no
generic overlap implementation.

## A. Contract and documentation

- [x] A.1 Record the pre-cutover surface inventory, adopter seam, breaking boundary, and rejected compatibility paths.
- [x] A.2 Specify logical port `tool.list`, kind `nats-request`, and default subject `discovery.tool.list`.
- [x] A.3 Specify one resolved runtime subscription, same-kind custom override, wrong-kind startup failure, and no
  legacy responder, alias, or repair.
- [x] A.4 Update live adopter and component guidance and publish the dated migration/supersession note without editing
  historical ADR/remap evidence or the frozen pre-v1 program.
- [x] A.5 Record #842 as this cutover's closeout issue and #810 as a parked generic overlap problem with no partial
  guard, registry, decoder, or export claim.

## B. Implementation and focused proof

- [x] B.1 Change the default `tool.list` port to kind `nats-request` on subject `discovery.tool.list`.
- [x] B.2 Resolve the configured logical port once and subscribe only to its subject; remove hard-coded fallback and
  any warn-and-continue path that would hide an invalid request port.
- [x] B.3 Permit a custom subject on kind `nats-request`; fail startup for kind `nats` or other incompatible facts.
- [x] B.4 Add focused tests for the default, same-kind override, wrong-kind failure, and lack of an implicit legacy
  subscription or repair path.
- [x] B.5 Run focused race tests and obtain independent SemStreams implementation review.

## C. Breaking integration proof

- [x] C.1 Run the pre-correction `task e2e:crud-tools` proof green with discovery requested at
  `discovery.tool.list` and a nonempty, effect-bearing catalog asserted. Fresh corrected-tree proof is tracked in D.8.
- [x] C.2 Run the pre-correction `task e2e:agentic` proof green with explicit `tool.execute.>` and `tool.result.>`
  stream families. Fresh corrected-tree proof is tracked in D.8.
- [x] C.3 Configure the merge to close #842 only after B and both E2E gates are green; keep #810 parked and make no
  generic overlap-guard, request-subject-registry, publish-ack-decoder, or subject-export claim.

## D. Post-Foundation-B control and startup correction

- [x] D.1 Add the exact one-for-one Foundation-B amendment: retire only the frozen
  `go:processor/agentic-tools/config.go#L146C3` / `tool.list|NATSPort` identity, add only the current
  `tool.list|NATSRequestPort` identity, and prove exact membership, cardinality, path-local expectations, and net-zero
  total accounting without editing either frozen TSV or the graph-query amendment.
- [x] D.2 Correct only the stale live comments in `config/stream_bounds.go` and `config/streams_test.go`: preserve the
  historical failure explanation and state that shipped guidance now uses `tool.execute.>` plus `tool.result.>`.
- [x] D.3 Add RED/GREEN production-seam proof for discovery-subscription failure, then return a transient contextual
  startup error that preserves `errors.Is(err, natsclient.ErrNotConnected)` for the representative transport failure;
  leave no subscription, local consumer, tracked resource, or running state, and prove a clean subsequent `Start`.
- [x] D.4 Add RED/GREEN proof for a later JetStream-consumer setup failure, then atomically roll back discovery and
  every local consumer started by that attempt; clear tracked resources, leave `running=false`, preserve the setup
  error, permit a clean subsequent `Start`, and do not delete durable consumer state or position.
- [x] D.5 Use one lock-internal cleanup path for failed-start rollback and normal stop; add no retry, recovery,
  readiness state, alias, fallback, workflow, or lifecycle surface.
- [x] D.6 Run focused tool-discovery and graph-query amendment tests, frozen-record tests, the full
  `internal/portgrammarcontrol` and `config` packages, and focused agentic-tools race tests; confirm both frozen TSVs
  have no diff.
- [x] D.7 Confirm the 21-config production census contains nine shipped `agentic-tools` instances inheriting the
  default and zero explicit `tool.list` config rows. In `service/testdata/message_logger_subject_census.json`, change
  only `added_kinds.nats_inputs` from `18` to `9` and `added_kinds.nats_request_inputs` from `9` to `18`; preserve
  `version`, `baseline_sha`, the complete Slice C ruling, the ordered 21-config scope, and every other field
  byte-for-byte. Follow the bounded #920 / `1db4c39e` current-target precedent without reopening frozen authority.
  Focused service census and full service race are green; leave the promoted message-logger aggregate-total drift to
  a separate documentation-truth correction.
- [x] D.8 Run the full race suite and then fresh `task e2e:crud-tools` and `task e2e:agentic` on the corrected startup
  path. Retain the logs locally rather than claiming in-tree artifacts: crud-tools exit `0`, SHA-256
  `fb070c9b014720d7c5eb3224b0003fdd58f1df03b3d465a05256d581fc2ed5a6`, with registered/effect-catalog proof,
  `tool_executions=4`, and rule `9/0/3`; agentic exit `0`, SHA-256
  `cdbbb54c3bbc38c0807d7505dd1295522ace35316330d75c385797ee10b4ba76`, with `tool_executions=1`. Record focused
  control/config/agentic-tools race, full agentic-tools integration, focused service census, full service race, the
  independently rerun full repository race suite, empty frozen TSV diffs, and strict OpenSpec 42/42.
- [x] D.9 Obtain final independent SemStreams correction approval of the exact amendment, startup ordering, rollback,
  durable-consumer preservation, and verification evidence.
- [x] D.10 Restore complete/green status and authorize the configured PR to close #842 when it merges. This records
  merge readiness, not a claim that merge, candidate selection, product tagging, or #827 execution has occurred.

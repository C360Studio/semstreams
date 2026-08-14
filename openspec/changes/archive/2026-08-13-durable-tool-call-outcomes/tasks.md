# Tasks

- [x] Add the centrally owned `TOOL_CALL_OUTCOMES` catalog declaration and owner acquisition before consumers.
- [x] Add V1 key, canonical fingerprint, immutable record validation, replay, and Create-CAS winner resolution.
- [x] Publish with deterministic message ID synchronously before request ACK.
- [x] Propagate handler and settlement outcomes through `ConsumeWithHeartbeat`.
- [x] Add typed observed oversize compact fallback and executor panic recovery.
- [x] Document the external-effect ambiguity window and executor idempotency obligation.
- [x] Add focused race-tested units for identity, disposition, CAS, concurrency, compact fallback, panic, and replay.
- [x] Add real-NATS concurrent-replica and restart persistence coverage.
- [x] Prove real-NATS result-publication failure after COMPLETED, component restart, exact replay, ACK, and one execution.
- [x] Add the agentic E2E result-publication fault injection, exact correlated-result decode, and invocation-count assertion.
- [x] Complete the parent-owned final `task e2e:agentic` rerun: green in 45.159746708s,
  `durable_tool_replay_executor_invocations=1`, with clean teardown.
- [x] Complete independent `semstreams-reviewer` review with zero blocking or high findings after all corrections.

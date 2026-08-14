# Stream capacity rejection circuit accounting design

## Scope and existing owners

The client already owns circuit state in `recordFailure`/`resetCircuit` (`natsclient/client.go:253-307,336-348`) and
already receives the server's actual PubAck result at exactly three failure-accounting seams: synchronous publish
(`natsclient/client.go:1035-1042`), the connection-level async acknowledgement handler
(`natsclient/client.go:1045-1063`), and acknowledged publish (`natsclient/stream.go:650-659`). Batch publishing does not
account independently; it aggregates the original future errors (`natsclient/client.go:1183-1257`). This change
extends that owner rather than adding a second breaker, retry path, or caller-facing classifier.

## Adopter seam inventory

The specific adopter is Ava, a developer outside this repository writing a custom input component like the shipped
UDP input. The existing component resolves whether its configured subject uses JetStream, calls
`PublishToStream(ctx, subject, data)`, and propagates the returned error (`input/udp/udp.go:728-737`). The public sync,
async, and batch surfaces already return an error, PubAck future, or aggregate (`natsclient/client.go:970-994,
1065-1091,1183-1257`); acknowledged publishing preserves an error chain (`natsclient/stream.go:617-659`).

**What must Ava know today?** Only the subject/data contract and the existing return surface. She does not know a
stream's current bytes, message count, server limit, API error code, or breaker threshold. The UDP exemplar simply
calls and propagates (`input/udp/udp.go:728-737`).

**What happens if Ava does nothing?** The actual server publish decides. A full stream still returns its original
refusal through the sync error, acknowledged wrapper, async future, or batch aggregate
(`natsclient/stream_capacity_circuit_integration_test.go:21-109`). The stream remains full until its owner/operator
changes capacity or data; this slice does not claim recovery.

**Where does Ava find out?** At the same existing error/future boundary she already handles, specified by
`openspec/specs/nats-streaming/spec.md:23-34,52-102` and the change delta. No new notification surface exists.

**What should Ava have to know after this change?** Nothing new. The client privately recognizes the server's typed
result and keeps an unrelated connection circuit usable (`natsclient/client.go:309-334`).

**Observation over prediction.** There is no max-bytes/messages knob or preflight. The client attempts the real
publish and classifies only the observed typed PubAck (`natsclient/client.go:321-333`). Tests provision real
`DiscardNew` ceilings and assert the server's exact returned descriptions
(`natsclient/stream_capacity_circuit_integration_test.go:21-109`).

## Binding-ruling conformance

| Binding ruling | Evidence | Deviation |
|---|---|---|
| Typed code/description allowlist only | E1 | None |
| Neutral means neither record nor reset | E2 | None |
| One private owner at all three seams | E3 | None |
| Preserve caller-visible error forms | E4 | None |
| Preserve metrics, logs, and async enqueue reset | E5 | None |
| All excluded/similar errors continue counting | E6 | None |
| No outward surface or adjacent-issue expansion | E7 | None |

- **E1:** Exact typed classifier: `natsclient/client.go:321-333`. Positive, wrapped, untyped, typed-nil,
  other-code, unknown-description, `nats.ErrMaxPayload`, and generic tests:
  `natsclient/stream_capacity_circuit_test.go:13-85`.
- **E2:** The private helper returns without record or reset at `natsclient/client.go:314-319`. Sync and acknowledged
  real-NATS tests preserve seed 14 at `natsclient/stream_capacity_circuit_integration_test.go:21-58`.
- **E3:** The three accounting seams are `natsclient/client.go:1035-1038,1050-1053` and
  `natsclient/stream.go:650-655`.
- **E4:** Sync returns the original error (`natsclient/client.go:1035-1038`); acknowledged publish keeps its wrapper
  (`natsclient/stream.go:650-655`); the async handler does not alter the future
  (`natsclient/client.go:1050-1063`); batch joins future errors (`natsclient/client.go:1221-1257`). Real-NATS
  `errors.As`, future, completion, and aggregate proof is
  `natsclient/stream_capacity_circuit_integration_test.go:21-109,140-146`.
- **E5:** Async metric/log remain at `natsclient/client.go:1052-1063`; acknowledged metric remains at
  `natsclient/stream.go:652-655`; successful enqueue still resets at `natsclient/client.go:1141-1156`. Async
  real-NATS reset proof is `natsclient/stream_capacity_circuit_integration_test.go:60-90`.
- **E6:** Helper fallthrough records at `natsclient/client.go:314-319`; negative matrix and typed-nil accounting are
  `natsclient/stream_capacity_circuit_test.go:25-35,65-81`; generic async threshold behavior remains at
  `natsclient/publish_async_test.go:64-82`.
- **E7:** New symbols are private (`natsclient/client.go:314,321`), and production changes only replace accounting at
  E3. Proposal non-goals record the exclusions
  (`openspec/changes/stream-capacity-rejection-is-circuit-neutral/proposal.md`).

There are no deviations from the approved architecture contract.

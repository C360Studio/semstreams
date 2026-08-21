# Change: Simplify one-shot lifecycle ownership

## Why

Startup and shutdown had accumulated framework-owned generations, retained results, child catalogs, stop-by-name
operations, and deletion switches around native Go and NATS resources. That made ordinary component ownership hard to
understand and made small lifecycle changes expensive.

The target is deliberately smaller: the code that starts work owns its exact handle, cancellation, and completion;
`Stop(ctx)` fences, drains, cancels, and joins that work; durable recovery remains the responsibility of JetStream
ACK/NAK/redelivery. A fresh runtime lifecycle uses a fresh owner instance.

## Current design status

All 36 production owner migrations are complete. N1a landed as reviewed commit
`8da1b83ae9c2f323bf484dc28e0574d81504bef9` (`refactor(lifecycle): remove unused lifecyclejoin`). It deleted the four
`internal/lifecyclejoin` package files and changed one test-only diagnostic: 1 insertion, 749 deletions, net -748, with
zero production additions. Independent implementation and merge review returned `APPROVE` with no findings.

The atomic N1b code cutover is independently reviewed `APPROVE` and commit-authorized on baseline
`18cd4fcefeaa6e10780776dc0450b5b1dd877a46`. Its 35-file implementation diff has SHA-256
`887ffc0a3b61d52c7497b889756bd02b36e269be64919cdbe606bde40062fe60`: production is 23 files, +102/-570, net -468;
tests are 12 files, +292/-415, net -123; total is net -591. It atomically completes exact-handle port convergence,
the stateless durable handler, and Client child/catalog/name-lifecycle removal. Atomic packaging avoids publishing an
incoherent outward API while downstream durable owners still combine direct `ConsumeDurable` acquisition with
Client-wide `StopAllConsumers`; it was not required by a SemStreams production caller. It preserves independent claims
and observation, and it leaves `Subscription.Drain` unchanged.

The local configuration/schema boundary is also independently reviewed `APPROVE`. It removes five Go fields and five
published-schema properties without adding a production cleanup mechanism or private helper; no current SemStreams
fixture requires one. The pre-comment-correction diff was +57/-72 (net -15), including production net -7 and schemas
net -30. Replacing the lifecycle integration comment makes the final 17-file slice +58/-73 (net -15): 16 tracked
implementation paths are +26/-73 and the new 32-line regression is +32/0. The tracked-path SHA excludes the untracked
regression; per-file ledger hashes identify all 17 files. This completes task 2.5. Task 2.3,
task 3.3, the 36-hit read-only sister migration obligation, candidate E2E/controlled/dirty proof, release, and tag work
remain unchecked. No release or tag is authorized.

The accepted read-only N1 inventory remains evidence at baseline
`2f974bdb7f22efb39ac5136e9c0b719b711249c2`, SHA-256
`2a95a0f5fd6683aeed585c8dca43d65ff662f32b2b046ce2262f6b97f74612e9`. It does not force every inventoried surface
into the current execution target. In particular, the previous six-ruling plan's `Subscription.Drain` redesign is
withdrawn and deferred. Existing Drain behavior and tests remain unchanged until a concrete defect or requirement
justifies revisiting them.

The owner's current direction is binding for simplification: first restore a working system that can be understood,
then decide from evidence whether anything else needs improvement. It is not approval for speculative lifecycle
semantics.

This narrowed N1 does not claim complete ADR-095 conformance. It preserves the landed Client-local internal claim's
reject-not-replace behavior and defers canonical sealed pre-Start validation and errors naming both owners. No new
claim owner-label state is added.

## What changes

- Keep N1a's deletion of the unused shared lifecycle state machine.
- Make the two canonical port-backed consume methods return the exact native `jetstream.ConsumeContext`; remove their
  temporary `*Handle` bridges after local callers migrate.
- Remove Client-owned consumer/subscription child catalogs, same-name lifecycle operations, name-routed Stop/delete,
  `OutstandingWork`, and Close-time child cleanup. Client Close owns transport and Client workers only.
- Remove the five inert `DeleteConsumerOnStop` Go fields and generated-schema properties. Add no cleanup helper because
  no current SemStreams fixture requires one.
- Replace `ConsumeDurable` with stateless `NewDurableHandler`, preserving the existing
  `ConsumeWithHeartbeat` ACK/NAK/Term/InProgress, cancellation, redelivery, work-join, and WARN behavior. Validate
  heartbeat against the minimum positive BackOff interval, or AckWait/default when BackOff is absent.
- Preserve independently owned duplicate claims, policy/metrics observation, OTEL claims, internal consumption,
  graph-ingest readiness, and agent-loop inflight observation. They do not become lifecycle catalogs.
- Make no change to `Subscription.Drain` semantics or tests in this pass.

The remaining N1 surface has a hard complexity budget: delete seven exports, add one (`NewDurableHandler`) for net
-6; remove five fields and their schema properties; delete catalogs and retained lifecycle state; add zero lifecycle
structs, interfaces, maps, mutexes, goroutines, contexts, or configuration switches.

## Capabilities

### New capability

- `restart-safe-shutdown`: direct owner ordering, lifecycle/topology separation, settlement boundaries, and proof
  gates without shared lifecycle machinery.

### Modified capabilities

- `service-shutdown`: owners retain and stop their exact resources; Client Close is transport-and-worker-only.
- `jetstream-consumer-policy`: port consumption returns native ownership and durable settlement composition becomes a
  stateless callback builder.
- `graph-ingest`: existing poison, settlement, readiness, and keyed convergence behavior remains unchanged.

## Impact

- **Port adopters:** the two canonical methods gain a returned native handle; downstream owners retain and stop it.
- **Durable adopters:** callers compose `NewDurableHandler` with the canonical handle-return method; ACK semantics and
  operator-visible failures remain unchanged.
- **Configuration:** five inert local fields/schema properties disappear without a production replacement. Published
  discovery is the migration signal. OTEL's existing strict decoder rejects the stale key; dispatch, loop, model, and
  tools retain their existing lenient unknown-field behavior.
- **Subscriptions:** no semantic or test change; further simplification is explicitly deferred.
- **Tests:** a new regression checks the five schemas emitted by the published composition; it does not validate
  downstream copies or change runtime decoder strictness.
- **Repository boundary:** SemStreams documents downstream impact but does not edit sister repositories.
- **Release:** N1a, the atomic N1b code cutover, and local configuration/schema removal are reviewed but unreleased;
  downstream migrations, controlled/dirty proof, and relevant E2E gates remain incomplete. No release or tag is
  authorized.
- **Recovery authority:** [`recovery-ledger.md`](recovery-ledger.md) remains the durable execution record.

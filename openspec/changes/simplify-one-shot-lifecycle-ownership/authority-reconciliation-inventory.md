# Lifecycle authority reconciliation inventory

Baseline: clean `main` at `f37d64e458605737983723d01a56d138f3105044`. This is inventory only; it does not
approve target text or complete runtime/proof work.

## Problem statement

Three active changes and one adopter guide still spell the same lifecycle authority after `recovery-ledger.md` assigned
it exclusively to `simplify-one-shot-lifecycle-ownership`. The correction must propagate through every normative
owner; relabeling duplicate tasks as superseded is not enough while active designs/specs still teach the rejected
mechanics.

## Surface inventory

### 1. Claimed gap and accepted authority

- `simplify-one-shot-lifecycle-ownership/recovery-ledger.md:25-39` assigns
  Generation/Operation/StopWithQuiesce migration, owner-local lifecycle, helper deletion,
  controlled/dirty/settlement proof solely to simplify; restore owns stored-context/invented-root removal only;
  require-restart owns boot-only composition and rules-only hot reload only.
- ADR-095:24-59 defines exact native ConsumeContext ownership, one-shot running Stop, retained failed-Start cleanup, no
  Client child catalogs/deletion/result replay, effect-before-ACK, and leaves runtime/proof incomplete.
- `simplify.../tasks.md:1-3,14-33` is the sole unchecked lifecycle implementation/proof list.

### 2. Current spellings and collisions

Simplify accepted authority:

- `recovery-ledger.md:170-219,221-295` owns owner migration, native protocol ordering, helper deletion, ACK audit,
  controlled/dirty proof, and zeros.
- `specs/restart-safe-shutdown/spec.md:3-13` and `specs/service-shutdown/spec.md:73-79` normatively own running
  shutdown and failed-Start cleanup.
- `design.md:95-119,181-185` still incorrectly assigns exact Start finalization and failed-Start cleanup to restore;
  those claims must move fully to simplify.

Restore collisions:

- `tasks.md:33-47` still carries boot runtime, Registry/ComponentManager borrow, terminal sequencing, and proof tasks;
  only `tasks.md:1-31` historical Stop(ctx) prerequisite and `:49-59` context/root debt fit its boundary.
- `specs/runtime-context-ownership/spec.md:78-139` owns native drain ordering, exact Start-finalization selection,
  manager borrow fencing, deadline/cancellation sequencing, not merely context provenance.
- `design.md:24-37,73-127` owns NATS ordering, Registry/borrow topology, boot supervisor, terminal manager Stop, and
  exact-generation joins; `proposal.md:15-16` still says it preserves exact Start finalization and failed-Start
  cleanup.
- `proposal.md:23-37,70-77` mixes the completed Stop(ctx) source break with active
  drain/supervisor/Registry-borrow/terminal-shutdown/E2E claims; only signature/history and context/root facts remain
  restore authority.
- `design.md:14-20,129-146` repeats Registry/borrow, terminal fencing, exact-generation shutdown, dirty recovery, and
  lifecycle proof; these are correction-propagation collisions.
- `tasks.md:61-80` is historical migration/evidence for the completed prerequisite except unchecked 4.5, whose
  breaking-tag lifecycle/E2E release gate belongs solely to simplify and must not remain an open restore task.
- `inventory.md:11-15` is historical measured evidence and may retain obsolete terms only when explicitly labeled
  non-normative provenance.

Require-restart collisions:

- `tasks.md:12-65` carries a full lifecycle delivery sequence including obsolete ManagedConsumer, DrainAndDelete,
  rejoin, retained Close, controlled/dirty proof; `tasks.md:75,79-80,143` also duplicate shutdown/borrow/proof
  authority.
- Intended owned work is `tasks.md:67-74,76-78` boot-only composition, `:82-99` flow authoring/desired-effective
  truth, `:100-132` rules-only hot reload, and capability-local migration/verification at `:134-142`.
- `design.md:49-53,173-180,188-297,304-324` defines terminal Stop/results, lifecycle simplification,
  shutdown/ACK/dirty proof, obsolete D10 mechanics, and current-looking lifecycle conformance.
- `design.md:45-47,326-338` also defines terminal borrow fencing and lifecycle proof/risk claims outside the earlier
  ranges.
- `proposal.md:28-29,41-61,104-121` continues to claim callback shutdown, owner shutdown/Close, controlled/dirty
  recovery and proof despite delegation at `:63-81`.
- `specs/component-runtime-config/spec.md:63-100` normatively owns terminal borrow fencing/drain/self-stop ordering;
  `specs/component-discovery/spec.md:9-11` owns exact-generation terminal sequencing.
- `inventory.md:5-7,82-87,121-149,155-198,200-243` is an active reset inventory that still says superseded lifecycle
  rulings are unchanged and prescribes ManagedConsumer/DrainAndDelete/rejoin dispositions; its adopter seam still
  instructs ManagedConsumer/handle Drain and superseded composition shutdown. Preserve its baseline/hash only as
  historical provenance; remove current authority or replace with an explicit superseded-evidence banner and no
  target instructions. Its adopter seam is superseded provenance, not current migration guidance.
- D1-D6 otherwise own sealed boot composition, desired/effective activation, flow authoring, and rules-only hot reload.

Adopter guide and outer-layer collision:

- `docs/operations/migration-restore-go-lifecycle-ownership.md:21-66` teaches context-bearing RemoveComponent,
  name-routed StopConsumer, rejoin, and terminal replay; `:81-99` presents Generation/Operation as authority;
  `:101-125` pre-cancels Start before StopAll; `:127-230` teaches a reusable generic generation/retained-error
  pattern; `:249-303` preserves obsolete live replacement/borrow behavior.
- `docs/README.md:80-90` indexes only the obsolete restore guide and describes it as covering NATS consumers.
- `docs/operations/migration-restart-safe-nats-client.md:1-144` is the accepted target guide: exact native
  ConsumeContext, one-shot Stop, failed-Start, no lifecycle deletion/catalog, settlement, terminal transport, proof
  gates, and it explicitly says implementation/proof are incomplete. It is not indexed.
- `docs/proposals/gh963-max-ack-pending-design.md:743-746,800-812` routes policy-record cleanup through
  consumerBinding, replacement, StopConsumer/StopAllConsumers/StopAndDeleteConsumer, and Client Close.
- Baseline production code confirms those temporary lifecycle homes at `natsclient/stream.go:692-723,729-750`; they
  are migration debt, not target precedent.

### 3. Adjacent claims

- `restore.../proposal.md:12-16,65-68` and `require.../proposal.md:63-81` already contain the intended delegation but
  are contradicted elsewhere in the same artifacts.
- `simplify.../proposal.md:12-29` and ADR-095 are the accepted target.
- Historical approved artifact hashes and inventories remain provenance. They must not appear as current migration
  instructions or completion credit.
- Sister repositories remain read-only.

### 4. Consumer at birth

No new exported Go symbol, port, subject, bucket, payload, config key, or query surface is introduced. Present
consumers are maintainers/agents reading active OpenSpec truth and downstream component authors reading the migration
guide. Zero runtime consumer exists for the rejected concurrent/rejoin/result-replay contract.

## Same-class collision table

| Dimension | Inventory evidence |
|---|---|
| Semantic class | lifecycle ownership, shutdown sequencing, failed-Start cleanup, ACK/restart proof |
| Owners | accepted: simplify ledger/tasks/specs; duplicates: restore §2/design/spec and require §2/D7/D9/D10/specs/guide |
| Catalogs | Client child/binding catalogs and name-routed cleanup remain baseline debt; ADR-095/recovery ledger require zero |
| Status | rule activation status remains require scope; backlog/readiness observations do not grant lifecycle authority |
| Lifecycle | simplify owns one-shot running Stop/native handles/failed-Start/controlled+dirty proof |
| Ownership | restore owns context provenance/debt; require owns boot topology/desired-effective/rules-only reload |
| Readers | maintainers, agents, reviewers, downstream Go component authors; docs index and both lifecycle migration guides |
| Writers | technical writer maintains task/spec truth; runtime writers are unchanged by this reconciliation |
| Recovery | recovery ledger is durable authority; historic hashes are provenance only; runtime/proof stays unchecked |

## Adopter seam inventory

Specific adopter: a downstream Go component author who has not read SemStreams internals.

- What must they know? `Start(ctx)` owns runtime lifetime; `Stop(ctx)` receives a fresh live bounded shutdown context;
  retain the exact native handle for resources they start; completed repeated Stop is nil/no-op; topology edits are
  desired next-boot state; only rule definitions hot reload.
- What happens if they do nothing? Stop signature changes fail compilation, which is safe. The current guide can
  instead direct them to temporary StopConsumer/Generation/rejoin/replacement APIs that are explicitly being removed,
  which is unsafe and can invert drain/cancel ordering.
- Where do they find out? Compile errors and typed restart-required/activation responses first; then the indexed
  Stop(ctx) prerequisite guide for the landed source break and the indexed native one-shot target guide, clearly
  marked pending until implementation lands. No correctness fact should depend only on an old design paragraph.
- What should they know? Only caller contexts, exact returned native ownership, next-boot topology, and rules-only live
  activation. They should not know consumer names, lifecyclejoin types, catalogs, rejoin/result replay, deletion
  policy, or storage grammar.
- Prediction check: reconciliation adds no caller knob or predicted framework fact. Policy observation cleanup follows
  the exact owner record/handle; callers do not predict consumer identity.

## Open evidence question

None. Every conflicting normative surface above is directly necessary correction propagation. Historical inventories
remain immutable evidence; active tasks/design/spec/migration guidance must be reconciled.

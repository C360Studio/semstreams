# Lifecycle authority reconciliation design

## Accepted inventory

Use `authority-reconciliation-inventory.md` SHA-256
`d495b4acb908bb846194eec0ce9c97076691bf3cba5d3143b8c2453335792f4e` verbatim.

## Options

A. Edit only task files. Smallest diff, but leaves active specs/designs/guides contradicting the ledger; rejected as
cosmetic.

B. Propagate the authority split through every inventoried normative/adopter surface while retaining explicit
historical tombstones. Moderate docs-only diff; one current authority per fact and history remains inspectable.
Recommended.

C. Archive/recreate all three changes. Clearest file separation but large churn, loses active task continuity, and
risks false completion. Rejected.

D. Do nothing. Preserves contradictory guidance and repeats the compaction failure. Rejected.

## Recommendation and premises

Choose B. Premises: `recovery-ledger.md:25-39` is owner-approved sole authority; `ADR-095:24-59` is the accepted
target; all runtime/proof tasks remain unchecked; historical artifact hashes are evidence not instructions; no new
runtime/exported surface is introduced. Measurements are the accepted inventory.

## Exact artifact deltas

1. `simplify.../tasks.md`: keep every runtime/proof box unchecked. Amend 1.4 to say all inventoried normative and
   adopter surfaces were reconciled. Add callback-borrow fence/return behavior to 2.3; do not create another lifecycle
   task list.

2. `simplify.../design.md`: split lines 95-106 into “retained restore context truth” (Stop(ctx) signatures, lexical
   Start authority, no retained context/root, nil rejection) and “lifecycle truth owned here”
   (startDone/finalization, failed-Start cleanupPending, callback-borrow shutdown fence). Replace D5 with: “Restore
   retains the completed context-bearing signature prerequisite and remaining context/root debt. This change
   exclusively owns exact Start finalization, failed-Start cleanup, service shutdown, terminal sequencing, ACK
   ordering, and controlled/dirty proof.”

3. `simplify.../specs/service-shutdown/spec.md`: add transferred requirement “Terminal ComponentManager shutdown
   fences callback borrows” with scenarios: admitted callback returns before component shutdown OR new borrow gets
   typed stopping; no manager/gate lock while waiting/calling; callback must return before outer composition requests
   Stop and cannot self-stop. This preserves behavior while moving authority.

4. `restore.../proposal.md`: retain only completed Stop(ctx)/StopAll(ctx) source break plus runtime context/root debt.
   Replace sequencing/Registry/borrow/restart bullets with one dependency paragraph: ADR-095/simplify own startDone,
   failed-Start cleanup, quiesce/drain, terminal sequencing, ACK/restart proof; require-restart owns boot-only
   composition. Impact release proof is historical prerequisite evidence, not an open gate here.

5. `restore.../design.md`: keep D2 signature and D3 no-root rules. Narrow D1 to context provenance only: Start
   receives/derives runtime context; owner may retain private cancel/join state, never context; Stop validates nonnil
   and uses caller context only to bound the terminal operation defined by simplify; it neither stores the argument,
   invents a root, nor launches work. Delete D4-D7 and replace one “Delegated lifecycle and composition” section
   pointing Registry/boot to require-restart and all lifecycle mechanics/proof to simplify. Replace
   invariants/validation with context-only invariants and P3 proof.

6. `restore.../specs/runtime-context-ownership/spec.md`: replace lines 78-139 with `Requirement: Stop uses
   caller-owned bounded authority without inventing a root`. Normative text: reject nil; Stop context only bounds the
   separately specified terminal operation; it is not retained and launches no runtime work;
   cancellation/deadline is returned honestly and never replaced with Background/TODO/WithoutCancel; this capability
   does not define drain ordering, startDone, failed-Start, borrow fencing, rejoin, or result replay. Keep three
   scenarios: Stop context never becomes work authority; canceled/deadlined Stop invents no replacement root; nil
   rejects before state/action. Transfer all removed lifecycle behavior to simplify, not deletion.

7. `restore.../tasks.md`: mark §1 explicitly “historical completed Stop(ctx) prerequisite; checked items do not
   define current lifecycle mechanics.” Replace §2 with a no-checkbox delegation note. Keep §3 unchanged and
   unchecked. Keep §4.1-4.4 as historical evidence; remove open 4.5 and state next-tag lifecycle/E2E gate is tracked
   only in simplify.

8. `restore.../inventory.md`: at lines 11-15, preserve baseline evidence but state it is non-normative provenance;
   restore owns the completed context-bearing signature prerequisite and context/root debt only; exact Start
   finalization, failed-Start cleanup, Stop ordering, and proof are simplify authority. Leave the forensic body intact.

9. `require.../proposal.md`: keep boot-only composition, desired/effective flow truth, rules-only reload. Remove
   callback shutdown, Close, owner drain, ACK/dirty proof as owned changes; replace with one dependency paragraph to
   simplify with zero completion credit. Remove terminal mechanics from Impact/Release and name them prerequisite
   cross-links only. Rules-only reload retains capability-local activation terminalization—fencing status publication
   and canceling/joining Rule-local activation work—executed under simplify's generic lifecycle contract; this does not
   grant require-restart generic component/service lifecycle or proof authority.

10. `require.../design.md`: D1 retains sealed boot set, value-only Registry, callback-scoped handle lifetime; remove
    terminal fencing/result/startDone prose and cross-reference simplify. D7 becomes “Lifecycle authority is an
    external prerequisite”: simplify exclusively owns generic component/service lifecycle ordering and
    controlled/dirty proof, while require-restart retains only Rule-specific activation terminalization under that
    contract. D9 becomes a short dependency statement: this change cannot claim activation/release readiness until
    simplify proof passes, owns no generic lifecycle tasks, and retains only that Rule-local terminalization. D10
    becomes a tombstone only: original owner-approved artifact hash and Git history are provenance;
    ManagedConsumer/DrainAndDelete/rejoin/result mechanics are superseded and non-normative. Delete the old D10 body.
    Reduce conformance table to boot topology, desired/effective truth, rules-only reload, dependency/no credit,
    history hash, sister-read-only. Remove lifecycle-test/proof risk claims; retain dependency risk only.

11. `require.../inventory.md`: add a top banner after provenance: “Historical reset inventory. ADR-095 and simplify
    supersede every ManagedConsumer, DrainAndDelete, Client catalog, rejoin, retained-result, and lifecycle-proof
    disposition below. Preserve counts/hash as evidence only; no row is current implementation or migration
    guidance.” Change “lifecycle rulings unchanged” to “historical rulings subsequently superseded.” Do not rewrite
    the evidence body.

12. `require.../specs/component-runtime-config/spec.md`: keep manager-owned callback-scoped runtime access and no
    retained handle. Remove terminal Stop order/self-stop scenarios and say shutdown behavior is specified only by
    simplify service-shutdown. Keep callback lifetime and raw-handle-unavailable scenarios.

13. `require.../specs/component-discovery/spec.md`: replace lines 9-11 with “Registry exposes no lifecycle
    authority. Process-local state ends with process lifetime; this capability defines no terminal shutdown ordering.”

14. `require.../tasks.md`: replace §2 with historical reset/no-checkbox delegation note; delete 2.2-2.6 prose.
    Remove 3.6 and 3.8 (transferred to simplify). Keep boot/config 3.1-3.5,3.7; all §4 and §5 unchecked. Narrow 6.5 to
    exact commit/tests/E2E for boot/flow/rule work and cross-reference simplify prerequisite evidence; do not track
    controlled/dirty proof here.

15. `docs/operations/migration-restore-go-lifecycle-ownership.md`: make it only the landed caller-owned Stop(ctx)
    prerequisite. Keep Stop/StopAll compiler break, fresh bounded Stop context, no stored context/root, streaming
    handler context, and historical prerequisite validation. Remove current RemoveComponent migration,
    StopConsumer/rejoin, Generation/Operation, pre-cancel-before-StopAll example, generic
    runGeneration/terminal-error replay code, and future replacement/borrow protocol. State live removal is retired:
    persist desired state/restart. Link the pending native one-shot guide and say temporary lifecyclejoin/name-routed
    APIs are not migration destinations.

16. `docs/README.md`: Stop-context link describes components/services/managers only. Add an indexed “Native one-shot
    lifecycle migration target” link to `migration-restart-safe-nats-client.md`, visibly PENDING until simplify
    runtime/proof gates pass.

17. `docs/operations/migration-restart-safe-nats-client.md`: retain as authoritative target guide; add link back to
    the landed Stop(ctx) prerequisite and recovery ledger. Keep explicit incomplete status.

18. `docs/proposals/gh963-max-ack-pending-design.md`: preserve the hashed body byte-for-byte. Add a supersession
    addendum before `## Design body` and outside its hash method: the body remains accepted historical evidence, but
    its consumerBinding/replacement/StopConsumer/StopAllConsumers/StopAndDeleteConsumer/Client Close policy-cleanup
    mechanics are superseded by ADR-095/simplify and non-normative. Current target: attach policy observation cleanup
    to the owner-local exact ConsumeContext record; refresh the exact handle; reject duplicates; have the owner forget
    labels after exact Closed; leave the OTEL cleanup closure unchanged. Do not alter the body or hash.

19. `recovery-ledger.md`: retain inventory/design checkpoint links and state reconciliation changes no runtime/proof
    credit.

## Adopter seam outcome

Must know: Stop(ctx), fresh live bound, exact returned native handle, next-boot topology, rules-only reload. Doing
nothing: compiler/typed response, not silent fallback. Discovery: docs index points separately to landed prerequisite
and pending target. Should know no names, lifecyclejoin, catalogs, deletion, rejoin/results, storage grammar. No
prediction knob.

## Strict validation

- `git diff --check`
- `openspec validate simplify-one-shot-lifecycle-ownership --strict --no-interactive`
- `openspec validate restore-go-lifecycle-ownership --strict --no-interactive`
- `openspec validate require-restart-for-config-activation --strict --no-interactive`
- `openspec validate --all --strict --no-interactive`
- exact rg audit over active non-inventory guidance for
  ``internal/lifecyclejoin|runGeneration|`Generation`|`Operation`|lifecyclejoin\.(Generation|Operation)|StopWithQuiesce|ManagedConsumer|DrainAndDelete|Client\.(StopConsumer|StopAllConsumers|StopAndDeleteConsumer)|DeleteConsumerOnStop``;
  remaining hits must be in immutable inventories, explicit supersession tombstones, ADR/ledger/zero gates, or
  migration removal lists only.
- verify every runtime/proof task in simplify §2-4, restore §3, require §3-6 remains unchecked.

No Go/runtime/E2E claim is made by this documentation reconciliation.

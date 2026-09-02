# Greenfield staging amendment: atomic semantic-settlement landing

## Status and evidence

Owner-approved sequencing correction after independent judge review.

This amendment preserves the accepted inventory:

- artifact: `openspec/changes/semantic-jetstream-settlement/inventory-rebaseline-2026-09-02.md`
- evidence base: `39444c9de649775a4be6866a946b7d73400f4639`
- SHA-256: `542458e2e46d5be2ea49e6ec5ab7de64366f58f782d94a32396aaaec38b4f437`
- independent result: `INVENTORY PASS`
- verifier: `231/231`
- materialization commit: `4d3894028d0100a67f2383672f35b42a4befc10e`

It supersedes only the earlier merge-first/Stage-A-close proposal.

## Binding rulings

1. #759 and draft PR #1156 remain open until:
   - every production `ConsumeWithHeartbeat` caller has migrated;
   - exported `ConsumeWithHeartbeat` is removed without alias; and
   - the full replacement-proof and OpenSpec landing gates pass.
2. The current three-file caller list is a branch-staging zero-growth guard only. It is not:
   - an API allowlist;
   - adopter compatibility;
   - merge authority;
   - accepted current-spec truth; or
   - permission for another production caller.
3. #1146 builds, tests, and receives review against the staged typed #759 foundation.
4. #1249 builds, tests, and receives review against the staged typed #759 foundation after the #1146 integration
   point.
5. Final helper removal remains #759 closure work. Closed issue #1250 remains closed and is not reclaimed.
6. No durable lane may be migrated by mechanically converting nil to ACK and error to Retry.
7. A fast no-heartbeat lane receives no raw-message settlement workaround and no implied exported no-heartbeat API.
   If its existing owner path is insufficient, work stops for a separately reviewed capability delta.
8. No commit reaches `main` with both the new typed API and exported `ConsumeWithHeartbeat` presented as accepted
   framework surface.

## Recommendation

Use `codex/gh759-semantic-settlement` as the non-default integration trunk.

Integrate #1146 and #1249 through separately claimed and reviewed child PRs whose base is the #759 branch. Only PR
#1156 targets `main`. Child merges therefore stage reviewed code without changing the default branch or closing their
issues. After both child changes are integrated, #759 removes the legacy helper, proves zero callers, completes all
migration and replacement evidence, archives every change, and carries the closing keywords for every issue whose
closure the final default-branch merge authorizes.

This preserves:

- one atomic public API transition on `main`;
- separate claims and reviews for #759, #1146, and #1249;
- each PR's `implemented-by` and review record;
- default-branch closing semantics; and
- one final integrated OpenSpec/spec-sync review.

## Exact branch and PR topology

- Integration trunk: PR #1156; head `codex/gh759-semantic-settlement`; base `main`; starts from current `main`,
  including merged #1245.
- Restart vertical: PR #1159; head `codex/gh1146-agentic-loop-restart`; base
  `codex/gh759-semantic-settlement`; starts from exact reviewed #759 foundation checkpoint `F`.
- AgentRun fanout: new draft PR for #1249; head `codex/gh1249-agentrun-fanout-settlement`; base
  `codex/gh759-semantic-settlement`; starts from exact staged #759 head `A` after #1159 merges into it.
- Final public landing: PR #1156; head `codex/gh759-semantic-settlement`; base `main`; starts from the exact staged
  head after #1146, #1249, helper removal, proof, and archive.

`F` is not guessed in advance. It is the pushed #759 head after:

- reconciliation with then-current `main`;
- preservation of #1245 approval/signal/refusal coverage;
- the typed foundation and Stage A bindings;
- gated-DAG truth and adopter migration documentation;
- semantic-settlement concept documentation; and
- independent review of that checkpoint.

Record `F` as the full remote head SHA of `origin/codex/gh759-semantic-settlement` in PR #1156 before #1159 rebases
or implements. Freeze the remote parent at `F` throughout #1159 implementation, proof, and review. Immediately before
the hosted #1159 merge, fetch the remote parent and require its full SHA to remain exactly `F`; any unexpected advance
invalidates the branch base and prior review.

`A` is the full remote #759 head SHA produced by the reviewed merge of PR #1159 into the non-default #759 branch.
Record it before creating the #1249 worktree and draft PR. Freeze the remote parent at `A` throughout #1249
implementation, proof, and review. Immediately before the hosted #1249 merge, fetch the remote parent and require its
full SHA to remain exactly `A`; any unexpected advance invalidates the branch base and prior review.

## Exact landing sequence

### 1. Reconcile and freeze the staged foundation

1. Rebase `codex/gh759-semantic-settlement` onto then-current `origin/main`.
2. Resolve current-main collisions, including merged #1245 coverage.
3. Finish only the foundation, Stage A, gated-DAG, migration, and concept work.
4. Run the focused foundation and Stage A gates.
5. Obtain review of this checkpoint.
6. Push it and record its full SHA as `F`.
7. Keep PR #1156 draft and unmergeable.

Stop if `origin/main` advances across a touched surface before #1159 rebases. Reconcile #759 again and issue a new
reviewed `F`.

### 2. Rebase and review #1146 against `F`

1. Change PR #1159's base from `main` to `codex/gh759-semantic-settlement`.
2. Rebase `codex/gh1146-agentic-loop-restart` onto exact `F`, preserving commit author metadata.
3. Force-push only with lease from the PR-owned worktree.
4. Verify:
   - PR #1159 contains only #1146 work relative to `F`;
   - `git merge-base` of its head and the #759 base is `F`;
   - the full accepted #1146 scope remains;
   - model plus loop task/response/tool-result migration is additive;
   - every fast no-heartbeat lane has a reviewed settlement route; and
   - no raw settlement or exported no-heartbeat workaround appears.
5. Correct #1146's active proposal, design, and tasks for exact `F`, the non-default PR base, the full-scope fast-lane
   gate, and the #1249 AgentRun transfer before production implementation begins.
6. Implement and prove #1146 with its complete `Closes #1146` claim set already visible on PR #1159.
7. Obtain implementation review.
8. Obtain the owner-requested cross-agent review.
9. Apply every finding and repeat implementation and cross-agent review until accepted.
10. Archive `agentic-loop-restart-safety` as #1159's final content commit.
11. Obtain narrow archive/spec-sync review and confirm its current-spec sync is present.

Hosted landing checklist after step 11:

1. Fetch `origin/codex/gh759-semantic-settlement` immediately before merge and require its SHA to equal exact `F`.
2. If it differs, stop; pin the new parent, rebase, retest, repeat implementation and cross-agent review, re-archive if
   content changed, and repeat narrow archive review.
3. If it remains `F`, undraft only after required CI passes with no known unfixed required-job flake.
4. Squash-merge PR #1159 into `codex/gh759-semantic-settlement`, not `main`.

Because its base is non-default, this merge does not close #1146. Post a staging comment on #1146 and PR #1159
recording the reviewed head, staging merge commit, and that PR #1156 owns final default-branch closure.

Fast-forward the #759 worktree to the hosted staging merge before any further #759 commit. Do not recreate the child
merge by cherry-pick.

### 3. Claim and review #1249 against post-#1146 staging

1. Record the updated #759 full SHA as `A`.
2. Create `codex/gh1249-agentrun-fanout-settlement` from exact `A`.
3. The first commit is its OpenSpec proposal.
4. Push and immediately open a draft PR:
   - base: `codex/gh759-semantic-settlement`;
   - head: `codex/gh1249-agentrun-fanout-settlement`;
   - body: `Closes #1249`;
   - body: `Refs #759`, `Refs #1146`, and `Refs #1155`;
   - body: `implemented-by: <actual persona>`.
5. Inventory and design against the post-#1146 terminal and handler shapes.
6. Do not implement until independent inventory/design review and owner acceptance.
7. Correct #1249's active proposal, design, and tasks for exact `A`, the non-default PR base, and the complete
   AgentRun transfer before production implementation begins.
8. Implement and prove both AgentRun bindings only through the accepted handler-done/replay contract, with the
   complete `Closes #1249` claim set already visible on its PR.
9. Obtain implementation review.
10. Obtain the owner-requested cross-agent review.
11. Apply every finding and repeat implementation and cross-agent review until accepted.
12. Archive the #1249 OpenSpec change as its final content commit.
13. Obtain narrow archive/spec-sync review and confirm its current-spec sync is present.

Hosted landing checklist after step 13:

1. Fetch `origin/codex/gh759-semantic-settlement` immediately before merge and require its SHA to equal exact `A`.
2. If it differs, stop; pin the new parent, rebase, retest, repeat implementation and cross-agent review, re-archive if
   content changed, and repeat narrow archive review.
3. If it remains `A`, undraft only after required CI passes with no known unfixed required-job flake.
4. Squash-merge the #1249 PR into `codex/gh759-semantic-settlement`, not `main`.

The non-default merge does not close #1249. Record the reviewed head and staging merge commit on #1249 and identify
PR #1156 as the final closing PR.

### 4. Complete #759 on the integration trunk

After both child PRs are integrated:

1. Fast-forward the #759 worktree to the hosted #1249 staging merge.
2. Re-run the production caller census.
3. Require zero production calls to `ConsumeWithHeartbeat`.
4. Remove exported `ConsumeWithHeartbeat` without alias.
5. Replace the staging guard with final conformance asserting:
   - the symbol is absent;
   - aliases are absent;
   - production callers are zero; and
   - all nine original bindings use their accepted typed settlement contracts.
6. Reconcile all SemStreams-owned sister migration records.
7. Complete every #1155 replacement-proof row.
8. Run focused race, full race/integration, lint, build, schema, contracts, gated-DAG, and serialized agentic E2E.
9. Replace staging `Refs #759` with `Closes #759`, add `Closes #1146` and `Closes #1249`, and add `Closes #1155`
   only after every #1155 acceptance row passes. This complete claim set precedes implementation review.
10. Obtain implementation review of the full integrated code and complete claim set.
11. Obtain the owner-requested cross-agent review.
12. Apply every finding and repeat implementation and cross-agent review until accepted.
13. Confirm the #1146 and #1249 changes are already archived and their current-spec sync is present.
14. Archive `semantic-jetstream-settlement` as PR #1156's final content commit.
15. Obtain narrow integrated archive/spec-sync review.
16. Make no later content commit. Any correction re-enters reconciliation, implementation/cross-agent review as
    applicable, archive, and narrow archive/spec-sync review.

Hosted landing checklist after step 16:

1. Confirm the complete closing claim set remains in the PR body and predates the accepted implementation and
   cross-agent reviews.
2. Undraft only after all required CI is green with no known unfixed required-job flake.
3. Squash-merge PR #1156 to `main`.

Only the final hosted merge changes the default branch. The single resulting default-branch commit simultaneously
introduces the permanent typed surface, migrates every production caller, and removes the legacy export.

## Default-branch issue closing

Child PRs retain `Closes #1146` and `Closes #1249` as their claim declarations, but their non-default merges do not
close those issues.

During staging PR #1156 keeps `Refs #759`. Only after zero callers, legacy removal, and full proof are complete, and
before implementation and owner-requested cross-agent review of the final integrated claim set, replace that reference
and add the complete closing set:

```markdown
Closes #759
Closes #1146
Closes #1249
Closes #1155

implemented-by: Sol
```

`Closes #1155` is added only after every acceptance row is satisfied. If any #1155 row remains unproved, stop: do not
add the keyword, undraft, or merge #1156.

The closing declarations must predate the final implementation and owner-requested cross-agent reviews. Adding or
changing one after those reviews invalidates them and requires a new implementation and cross-agent review round
before archive.

#1250 receives no closing keyword. Its work was returned to #759 by owner ruling, and it remains closed without a
merged PR.

## Authorship and review record

Each child PR retains:

- its original author;
- its actual `implemented-by: <persona>`;
- its reviewed head SHA;
- its review results;
- its archive/spec-sync review; and
- its hosted non-default staging merge.

Do not flatten child work into #759 through local cherry-picks or an unreviewed local merge.

Before final review, PR #1156 adds this table with actual values:

| Issue | Reviewed staging PR | Reviewed head | Staging merge | Implemented by |
|---|---|---|---|---|
| #759 | #1156 | `<final-parent-head>` | final default-branch squash | Sol |
| #1146 | #1159 | `<reviewed-head>` | `<staging-merge-sha>` | Sol |
| #1249 | `<pr-number>` | `<reviewed-head>` | `<staging-merge-sha>` | `<actual persona>` |

The final squash commit cannot preserve the child commit graph. The retained hosted child PR records, their review
records, and this integration table are the authorship and implementation record.

## Exact active-artifact amendments

### Proposal amendment

Add:

```markdown
## Atomic public landing

#759 owns the complete public API transaction: introduce the permanent typed settlement surface, integrate the nine
owner-specific migrations, and remove exported `ConsumeWithHeartbeat` without alias. PR #1156 remains draft and does
not merge until the old symbol and every production caller are absent.

#1146 retains its full accepted restart-safety scope and implements model plus loop task/response/tool-result against
the staged #759 foundation through PR #1159. #1249 independently designs and implements AgentRun complete/failed
fanout settlement against the post-#1146 staged foundation. Both PRs target the non-default #759 branch and receive
their own reviews. Their work reaches `main` only through the final reviewed #1156 squash merge.

The three current production caller files form a zero-growth branch-staging guard only. They are not an API allowlist,
compatibility promise, current capability, or merge gate.

No binding migration is a mechanical nil-to-ACK/error-to-Retry conversion. Each ACK requires its accepted
owner-specific durable definition of done. A fast lane does not gain raw settlement authority or an exported
no-heartbeat workaround.
```

Replace proposal impact/removal text with:

```markdown
- Breaking pre-v1 API replacement: the default branch receives `ConsumeDeliveryWithHeartbeat` and removal of
  `ConsumeWithHeartbeat` in one final PR.
- No accepted default-branch interval exposes both APIs.
- #1146 and #1249 are separately claimed and reviewed on the non-default #759 integration branch.
- Final PR #1156 carries the default-branch closing authority for #759, #1146, #1249, and, only after complete proof,
  #1155.
- `NewDurableHandler` and `ConsumeWithHeartbeat` are both absent from final current truth.
```

Add proposal non-goals:

```markdown
- No merge of PR #1156 while exported `ConsumeWithHeartbeat` or any production caller remains.
- No API allowlist or compatibility status derived from the branch-staging zero-growth guard.
- No mechanical ACK conversion.
- No raw-message settlement escape or unreviewed exported no-heartbeat API.
- No child-PR merge directly to `main`.
- No intermediate accepted dual-API state.
```

### Design amendment

Add:

```markdown
### D0 — non-default integration trunk and atomic default-branch cutover

`codex/gh759-semantic-settlement` is the integration trunk and the head of PR #1156. PR #1156 alone targets `main`.
PR #1159 and the #1249 implementation PR target the #759 branch and are separately claimed, reviewed, archived, and
squash-merged there.

The #759 branch may temporarily contain both typed and legacy exports only as unmerged staging state. That condition
is never archived as current framework truth and never reaches `main`. The exact caller list is enforced only as a
zero-growth test that shrinks after each staged child merge.

After #1146 and #1249 integrate, #759 removes `ConsumeWithHeartbeat`, proves the exported symbol and production caller
count are zero, and archives the final capability state. The final default-branch squash therefore performs one
greenfield API cutover rather than accepting a compatibility period.

Non-default child merges do not close their issues. PR #1156 declares the complete closing set before implementation
and owner-requested cross-agent review of the final integrated claim set and owns default-branch closure.

### D10 — staged owner migrations

The Stage A tools and dispatch bindings establish the typed foundation but do not authorize #1156 to merge.

#1146 retains its full accepted intake, command, model, loop, tools, signal, approval, projection, governance, replay,
and context/lifecycle scope. Its model and three loop heartbeat migrations are additive. It rebases onto the reviewed
#759 foundation checkpoint and chooses a reviewed route for every fast no-heartbeat lane. No lane receives raw
settlement or an exported no-heartbeat API by implication.

#1249 begins from the staged #759 head after #1146 integration so its AgentRun design observes the post-#1146 terminal
and handler shapes. It defines source identity, handler durable done, replay, panic/error, and partial-success
semantics before migrating complete or failed.

No migration maps callback return values mechanically. The binding owner returns ACK only after its named durable
positive or negative consequence and every required downstream acknowledgement.

### D15 — final legacy removal

Final `ConsumeWithHeartbeat` removal belongs to #759. The branch-staging zero-growth guard shrinks as #1146 and #1249
migrate their callers. After the final staged migration, conformance requires zero production callers and absence of
the exported declaration and every alias.

Removal, complete replacement proof, migration reconciliation, and the complete closing claim set precede final PR
#1156 implementation and owner-requested cross-agent review. Archive/spec sync follows accepted fixes/re-review and is
the final content commit. There is no accepted additive dual-API period.
```

### Exact #1146 active OpenSpec amendments

These corrections are required before #1146 production implementation, implementation review, or archive. They
replace the merge-first statements currently at:

- `openspec/changes/agentic-loop-restart-safety/proposal.md:10-12,30,44`;
- `openspec/changes/agentic-loop-restart-safety/design.md:5,17,22,408,420,424`;
- `openspec/changes/agentic-loop-restart-safety/tasks.md:13,16`; and
- the AgentRun hold at `openspec/changes/agentic-loop-restart-safety/tasks.md:125-128`.

Replace the merge-first proposal text with:

```markdown
#1146 remains dependent on #759's accepted typed settlement foundation, but it does not wait for #759 to merge to
`main`. It builds, proves, and receives review against the exact reviewed remote #759 foundation checkpoint `F` while
PR #1156 remains draft. PR #1159 targets `codex/gh759-semantic-settlement`; its reviewed non-default merge stages the
restart vertical for the final atomic #759 landing.

The full accepted #1146 scope remains: user-message intake and commands, model, loop, tools, signals, approval,
projections, governance correlation, replay admissibility, and context/lifecycle closure. Migration of model plus
loop task/response/tool-result heartbeat bindings is additive to that scope.

Every current fast no-heartbeat durable-input lane is reinventoried against `F`. It uses an existing owner settlement
path, the admitted heartbeat route only when work is long-running, or stops for a separately reviewed capability
delta. No raw settlement authority or exported no-heartbeat interpreter is admitted by implication.

AgentRun complete/failed settlement is transferred to #1249 and is not #1146 implementation scope.
```

Replace the #1146 design status and holds with:

```markdown
## Status

Owner-accepted target state after independent `DESIGN REVIEW PASS`, subject to required rebaseline against exact
reviewed #759 foundation checkpoint `F`. Implementation remains blocked until the active proposal, design, tasks, and
PR base are corrected for this non-default staging sequence and the refreshed inventory/design is reviewed.

## Holds

- PR #1159 SHALL target `codex/gh759-semantic-settlement` and its branch SHALL be based on exact remote parent SHA `F`.
- The remote #759 parent remains frozen at `F` throughout #1146 implementation, proof, and review. Unexpected advance
  invalidates the base and review and requires a new pin, rebase, retest, and re-review.
- The full accepted #1146 scope remains; model and three loop heartbeat migrations are additive.
- Every fast no-heartbeat durable-input lane requires a line-addressable inventory and selected existing or reviewed
  settlement route. Raw direct settlement and an exported no-heartbeat API are prohibited.
- AgentRun is transferred to #1249; no AgentRun production or spec work lands in #1146.
- #1155 owns the complete real-NATS process-replacement proof matrix.
- Governance content and policy coverage remain #1140.
- Framework-wide restart generalization remains #1145.
```

Replace measurable premises 1 and 12 and the related AgentRun out-of-scope line at current #1146 design lines 408,
420, and 424 with:

```markdown
1. Exact reviewed remote #759 checkpoint `F` supplies the accepted `DeliveryResult` settlement foundation for every
   touched lane. PR #1159 targets the non-default `codex/gh759-semantic-settlement` branch, is based on exact `F`, and
   receives implementation and cross-agent review while the remote parent remains frozen at `F`. This premise does
   not require #759 to merge first.

12. AgentRun complete/failed settlement is transferred to #1249. #1249 begins from exact remote post-#1146 staged
    parent checkpoint `A`, inventories and designs against those handler shapes, and receives separate review before
    either AgentRun binding migrates.

## Out of scope

- AgentRun production implementation and capability deltas; #1249 owns its post-#1146 inventory, design,
  complete/failed migration, and replacement proof from exact staged parent checkpoint `A`.
```

These measurable-premise and out-of-scope corrections are part of the mandatory pre-implementation reconciliation.
#1146 does not begin production work, implementation review, or archive while the merge-first premise at line 408 or
the former #1148 AgentRun hold at lines 420/424 remains.

Replace #1146 tasks 1.1 and 1.4, add 1.9, and replace the AgentRun hold with:

```markdown
- [ ] 1.1 Before implementation, retarget PR #1159 to `codex/gh759-semantic-settlement`, rebase the branch onto exact
      reviewed remote parent SHA `F`, and verify that its merge base and PR diff contain only #1146 work above `F`.
- [ ] 1.4 Reconcile the full accepted design against `F` and post-#1231/#1245 surfaces; stop for reinventory and
      review if any touched surface or authority differs materially.
- [ ] 1.9 Inventory every current fast no-heartbeat durable-input lane and select an existing owner path, the admitted
      heartbeat route only for long-running work, or a separately reviewed capability delta. Add no raw settlement
      escape or exported no-heartbeat interpreter.

## Transferred: AgentRun

AgentRun tasks H.1/H.2 are removed from #1146. #1249 owns its post-#1146 inventory, design, complete/failed migration,
and replacement proof against staged parent checkpoint `A`.
```

Replace #1146 verification/review tasks 11.6-11.8 with:

```markdown
- [ ] 11.6 Confirm the complete `Closes #1146` claim set and full accepted scope are visible on PR #1159.
- [ ] 11.7 Obtain SemStreams implementation review.
- [ ] 11.8 Obtain the owner-requested cross-agent review.
- [ ] 11.9 Apply every finding and repeat implementation and cross-agent review until accepted.
- [ ] 11.10 Archive as the final content commit.
- [ ] 11.11 Obtain narrow archive/spec-sync review and confirm current-spec sync is present.
```

Undraft, CI, exact remote-base verification, and hosted merge are not #1146 OpenSpec tasks. They occur only through
the hosted landing checklist after task 11.11.

### Tasks amendment

Replace the held integration/removal sections with:

```markdown
## 6. Non-default staged integrations

- [ ] 6.1 Rebase #759 onto current `main`, preserve merged #1245 coverage, complete the foundation/docs/spec
      reconciliation, review it, and record the pushed remote parent full SHA as `F`.
- [ ] 6.2 Retarget PR #1159 to base `codex/gh759-semantic-settlement`, rebase its branch onto exact `F`, and verify its
      merge base and diff before implementation.
- [ ] 6.3 Correct #1146 proposal/design/tasks for exact `F`, the non-default base, full-scope fast-lane gate, and
      AgentRun transfer before its implementation, review, or archive.
- [ ] 6.4 Confirm #1146 implementation/proof, complete-claim implementation review, owner-requested cross-agent review,
      fixes/re-review, final-content archive, and narrow archive/spec-sync review are recorded in that order.
- [ ] 6.5 After hosted #1159 integration, confirm its reviewed content, archive, and current-spec sync are present on
      #759; record its reviewed head and staging merge SHA without representing #1146 as closed.
- [ ] 6.6 Record the updated remote #759 head as `A`; create `codex/gh1249-agentrun-fanout-settlement` from exact `A`,
      commit its proposal first, and open a draft PR based on `codex/gh759-semantic-settlement` with `Closes #1249`.
- [ ] 6.7 Correct #1249 proposal/design/tasks for exact `A`, the non-default base, and complete AgentRun transfer before
      its implementation, review, or archive.
- [ ] 6.8 Confirm independent inventory/design review and owner acceptance precede AgentRun implementation.
- [ ] 6.9 Confirm #1249 implementation/proof, complete-claim implementation review, owner-requested cross-agent review,
      fixes/re-review, final-content archive, and narrow archive/spec-sync review are recorded in that order.
- [ ] 6.10 After hosted #1249 integration, confirm its reviewed content, archive, and current-spec sync are present on
       #759; record its reviewed head and staging merge SHA without representing #1249 as closed.

## 7. Final zero-caller cutover

- [ ] 7.1 Fast-forward the #759 worktree after each hosted child merge; do not recreate reviewed integrations through
      cherry-pick or local merge.
- [ ] 7.2 Shrink the branch-staging zero-growth guard after #1146 and #1249; never describe it as an API allowlist.
- [ ] 7.3 Prove zero production `ConsumeWithHeartbeat` calls, remove the exported helper without alias, and replace the
      staging guard with final absence conformance.
- [ ] 7.4 Prove all nine original bindings use their accepted typed settlement contracts and no binding was migrated
      through mechanical nil/error conversion.
- [ ] 7.5 Reconcile SemStreams-owned sister migration instructions and the temporary branch-only adopter seam.
- [ ] 7.6 Complete every #1155 replacement-proof row and run focused race, full race/integration, lint, build, schema,
      contracts, gated-DAG, and serialized agentic E2E gates.
- [ ] 7.7 After zero-caller/removal/full-proof gates pass, replace PR #1156 staging `Refs #759` with `Closes #759` and
      add `Closes #1146`, `Closes #1249`, and `Closes #1155` before implementation review of the complete claim set.
- [ ] 7.8 Obtain SemStreams implementation review of the complete integrated code and claim set.
- [ ] 7.9 Obtain the owner-requested cross-agent review.
- [ ] 7.10 Apply every finding and repeat implementation and cross-agent review until accepted.
- [ ] 7.11 Confirm #1146 and #1249 are archived and their current-spec sync is present.
- [ ] 7.12 Archive `semantic-jetstream-settlement` as PR #1156's final content commit.
- [ ] 7.13 Obtain narrow integrated archive/spec-sync review and make no later content commit.
```

Undraft, CI, and hosted merge are not active OpenSpec tasks. They remain only in the hosted landing checklists after
the applicable narrow archive/spec-sync review.

### Final `jetstream-consumer-policy` delta

Use:

```markdown
### Requirement: semantic heartbeat settlement has one permanent exported surface

The framework SHALL expose `ConsumeDeliveryWithHeartbeat` with validated `HeartbeatDeliveryPolicy`,
`DeliveryAttempt`, `DeliveryDecision`, and `DeliveryResult`.

`ConsumeWithHeartbeat` and `NewDurableHandler` SHALL NOT exist or have aliases. Every original model, tools, dispatch,
loop, and AgentRun heartbeat binding SHALL use the permanent typed surface with its owner-specific durable definition
of done.

No final capability SHALL describe a production legacy allowlist. Any exact caller list used before final integration
is branch-staging conformance only and SHALL be zero before archive.

#### Scenario: final public surface

- **WHEN** the semantic-settlement change is archived
- **THEN** the permanent typed API exists
- **AND** `ConsumeWithHeartbeat`, `NewDurableHandler`, and every alias are absent
- **AND** production callers of those removed symbols are zero

#### Scenario: binding migration requires semantic authority

- **WHEN** a durable binding migrates
- **THEN** its decision matrix names the exact durable positive and negative consequences
- **AND** nil/error callback behavior alone does not authorize ACK or Retry

#### Scenario: fast lane lacks an admitted settlement route

- **WHEN** an inventoried fast no-heartbeat lane cannot use an existing owner path
- **THEN** migration stops for a separately reviewed capability delta
- **AND** no raw message settlement or exported no-heartbeat interpreter is introduced
```

### Final `nats-streaming` delta

Keep the accepted restart-authority and replay requirements, but replace held/additive wording with:

```markdown
### Requirement: no staged compatibility surface becomes current truth

Temporary coexistence of typed and legacy settlement on a non-default integration branch SHALL NOT be archived as
current capability truth or merged to the default branch. Final current truth SHALL contain only the permanent typed
surface and its migrated production bindings.

JetStream remains the delivery and redelivery authority. This staging rule adds no supervisor, checkpoint, outbox,
receipt ledger, state-machine runtime, or new durable primitive.
```

### Documentation amendments

```markdown
`docs/concepts/33-semantic-settlement.md` describes only the permanent public pattern. It does not teach or advertise
`ConsumeWithHeartbeat`.

During staging, `docs/operations/migration-restart-safe-nats-client.md` may identify the old symbol as a removal
boundary, but final content must say it is removed and show only the typed policy/consumer composition.

The final migration guide must state:

- the new and removed exports reach `main` together;
- nil/error is not a portable definition of done;
- each adopter defines its durable consequence and replay behavior;
- fast consumers receive no raw/no-heartbeat workaround;
- SemSpec and SemDragon retain owner-specific gated-DAG mappings;
- deterministic `Nats-Msg-Id` dedupe is bounded by `Duplicates`; and
- sister repositories are not modified by SemStreams.
```

## Temporary exported-symbol adopter seam

### What must an external developer know?

Nothing for ordinary released/default-branch use. The #759 integration branch is not an accepted framework version.
A developer deliberately building against it must know that the simultaneous presence of
`ConsumeDeliveryWithHeartbeat` and deprecated `ConsumeWithHeartbeat` is temporary integration scaffolding. The old
symbol admits no new caller and has no compatibility promise.

### What happens if they do nothing?

`main` remains unchanged until the atomic final merge. When that breaking pre-v1 merge lands, code still calling
`ConsumeWithHeartbeat` fails to compile and must adopt the typed API with an owner-specific durable done/replay
contract.

### Where do they find out?

- the draft PR #1156 staging warning;
- the old symbol's deprecation comment while it exists on the branch;
- `docs/operations/migration-restart-safe-nats-client.md`;
- `docs/concepts/33-semantic-settlement.md`;
- the final breaking changelog/release migration note; and
- the SemStreams-owned sister migration record.

### What should they ideally have to know?

Only the permanent contract: retain the exact consume handle, define the durable business consequence, return
ACK/Retry/Terminate/Quarantine, inspect `DeliveryResult`, and stop the exact owner on classified control loss. They
should never need to understand the temporary branch duality, native message settlement, or an API allowlist.

## Hosted PR bodies and comments

### PR #1156 body replacement

```markdown
Refs #759
Refs #1146
Refs #1249
Refs #1155

implemented-by: Sol

## Summary

- stage the permanent typed semantic-settlement foundation;
- integrate separately reviewed #1146 model/loop restart safety and #1249 AgentRun fanout settlement;
- migrate all nine original production bindings through owner-specific durable definitions of done;
- remove `ConsumeWithHeartbeat` and `NewDurableHandler` without aliases;
- preserve JetStream/component ownership without a supervisor, state-machine runtime, checkpoint, outbox, or new
  durable primitive; and
- land the new API and legacy removal atomically on `main`.

## Greenfield staging boundary

This PR remains draft while exported `ConsumeWithHeartbeat` exists or any production caller remains.

The current three-file caller list is a zero-growth branch-staging guard only. It is not an API allowlist, adopter
contract, merge authority, or accepted compatibility surface.

PR #1159 and the #1249 implementation PR target this non-default branch and receive separate implementation and
archive/spec-sync reviews. Their non-default merges do not close their issues. After zero-caller, removal, and complete
proof gates pass, and before final implementation and owner-requested cross-agent review, this PR replaces
`Refs #759` with `Closes #759` and adds `Closes #1146`, `Closes #1249`, and `Closes #1155`.

No intermediate default-branch commit exposes the staged dual API.

## Stop conditions

Do not undraft or merge while:

- the old export or a production caller exists;
- #1146 or #1249 lacks accepted review/archive evidence;
- any lane uses mechanical nil/error settlement;
- a fast lane relies on raw or unreviewed no-heartbeat settlement;
- any #1155 row is incomplete;
- current specs are unsynchronized;
- content follows the final #759 archive commit; or
- required CI has a known unfixed flake.
```

### PR #1159 body correction

```markdown
## Staged integration base

PR #1159 targets `codex/gh759-semantic-settlement`, not `main`, and builds against the exact reviewed #759 foundation
checkpoint `F` recorded on PR #1156. The remote parent remains frozen at `F` through implementation, proof, and
review. Immediately before hosted merge, the remote base SHA must still equal `F`; otherwise the prior base and review
are invalid and the branch must be repinned, rebased, retested, and re-reviewed.

The full accepted #1146 scope remains unchanged. Model plus loop task/response/tool-result heartbeat migration is
additive. Every fast no-heartbeat durable-input lane is reinventoried and either uses its existing owner settlement
path, uses the admitted heartbeat path because the work is long-running, or stops for a separately reviewed
capability delta. No raw settlement or exported no-heartbeat interpreter is admitted.

This PR retains `Closes #1146` as its claim. Its non-default staging merge will not close #1146; final default-branch
closure belongs to reviewed PR #1156.

The active #1146 proposal, design, and tasks must replace their merge-first statements and transfer AgentRun H.1/H.2
to #1249 before implementation, implementation review, or archive.
```

### New #1249 draft PR body

```markdown
Closes #1249
Refs #759
Refs #1146
Refs #1155

implemented-by: <actual persona>

## Claim

Design and implement replay-safe AgentRun complete/failed fanout settlement against exact remote staged parent SHA
`A` after reviewed #1146 integration. The remote parent remains frozen at `A` through implementation, proof, and
review; an unexpected parent advance invalidates the base and review and requires repin, rebase, retest, and re-review.

## Required contract

- preserve source-message identity through fanout;
- define durable done for the exported handler seam;
- classify run resolution, panic, handler error, partial success, and replay;
- compare whole-fanout replay, stable receipts, and one composite consequence;
- add no receipt authority without measured justification and owner acceptance;
- migrate both bindings only after independent inventory/design review; and
- prove complete and failed process replacement.

This PR targets the non-default #759 integration branch. Its merge stages reviewed work but does not close #1249.
Final default-branch closure belongs to PR #1156.
```

### Hosted checkpoint comment on PR #1156

```markdown
## Reviewed staged-foundation checkpoint

- Integration branch: `codex/gh759-semantic-settlement`
- Default-branch base: `<main-sha>`
- Reviewed foundation head `F`: `<full-sha>`
- Inventory SHA-256: `542458e2e46d5be2ea49e6ec5ab7de64366f58f782d94a32396aaaec38b4f437`
- Inventory result: `INVENTORY PASS`; pins `231/231`

PR #1159 must use exact `F` as its branch base and PR #1156 as its hosted base. This is a staging checkpoint, not
merge authority. PR #1156 remains draft until every production legacy caller and exported `ConsumeWithHeartbeat` are
absent. The remote integration branch is frozen at exact `F` throughout #1159 implementation, proof, and review.
```

### Hosted child staging-merge comment template

```markdown
## Non-default staged integration

- Issue: #<issue>
- Reviewed PR: #<pr>
- Implemented by: <persona>
- Expected remote parent: `<F-or-A-full-sha>`
- Reviewed head: `<full-sha>`
- Archive/spec-sync review: <link>
- Staging merge into `codex/gh759-semantic-settlement`: `<full-sha>`

Immediately before hosted merge, the fetched remote parent equaled the expected full SHA. Any mismatch would have
invalidated the review and required a new pin, rebase, retest, implementation/cross-agent re-review, and archive
reconciliation. This merge did not target the default branch and therefore did not close #<issue>. PR #1156 owns
final default-branch integration and will carry `Closes #<issue>` before implementation review of the final claim set.
```

### Final PR #1156 implementation-review claim comment

```markdown
## Atomic landing implementation claim

Reviewed staged integrations:

| Issue | PR | Reviewed head | Staging merge | Implemented by |
|---|---|---|---|---|
| #1146 | #1159 | `<sha>` | `<sha>` | Sol |
| #1249 | #<pr> | `<sha>` | `<sha>` | `<persona>` |

Final conformance:

- production `ConsumeWithHeartbeat` callers: 0
- exported `ConsumeWithHeartbeat`: absent
- aliases: 0
- all nine original bindings: typed and owner-reviewed
- #1155 replacement rows: complete
- child OpenSpec changes: archived with current-spec sync present
- PR body: staging `Refs #759` replaced by `Closes #759`
- PR body: `Closes #1146`, `Closes #1249`, and `Closes #1155` present

The PR body now carries the complete default-branch closing set. This comment starts implementation review followed
by the owner-requested cross-agent review. No earlier review covers the newly added closing authority. Findings are
fixed and both reviews repeat before the final #759 archive commit.
```

### Final PR #1156 archive checkpoint comment

```markdown
## Atomic landing archive checkpoint

- accepted implementation review: <link>
- accepted owner-requested cross-agent review: <link>
- #1146 archive/current-spec sync: `<sha>` / present
- #1249 archive/current-spec sync: `<sha>` / present
- #759 archive/spec sync: final content commit `<sha>`
- narrow archive/spec-sync review: <link>
- content after archive: none

Hosted undraft, required CI, and default-branch squash merge may proceed. Any content correction re-enters the
applicable implementation/cross-agent review, produces a new final archive commit, and repeats narrow archive review.
```

## Strongest alternative

Run #1146 and #1249 as sibling branches from `F`, then rebase both independently into #759.

This shortens calendar time, but #1249 would design against pre-#1146 terminal/handler shapes or require a second
inventory and review after #1146 integrates. It also increases parent-branch races and makes review bases harder to
audit. The linear `F -> #1146 -> A -> #1249 -> final #759` sequence is recommended because AgentRun consumes the
terminal surface #1146 may change.

## Risks

- Retargeting or rebasing a child PR invalidates prior code review; review occurs after the final base is pinned.
- The remote parent must remain exactly `F` during #1159 review and exactly `A` during #1249 review; any advance
  invalidates that child's base, tests, and review.
- A hosted child merge advances the parent branch behind its local worktree; only fast-forward the parent afterward.
- Non-default child merges leave issues open; PR #1156 must carry final closing keywords before implementation and
  owner-requested cross-agent review of the final integrated claim set.
- The final squash loses the child commit graph; hosted PR metadata and the integration table preserve attribution.
- Sequential OpenSpec archives can touch the same current specs; each child archives before staging merge, and #759
  performs the final integrated sync.
- A new legacy caller can enter through any staged child; zero-growth checks run on every child and zero-caller checks
  run before final archive.
- A material `main` change after `F` invalidates the base; reconcile #759 and recut `F`.

## Stop conditions

Stop the sequence if any of the following occurs:

- `F` is not pushed, full-SHA pinned, and reviewed;
- the fetched remote #759 parent differs from `F` immediately before #1159 merge;
- a child PR's base is not the #759 integration branch;
- #1249 does not begin from post-#1146 staged head `A`;
- the fetched remote #759 parent differs from `A` immediately before #1249 merge;
- `main` changes a touched surface after `F`;
- a new production legacy caller appears;
- any migration maps nil mechanically to ACK;
- a lane lacks a durable definition of done or replay contract;
- a fast lane needs an unreviewed raw/no-heartbeat settlement surface;
- a child lacks implementation or archive/spec-sync review;
- #1155 proof is incomplete;
- the final tree still exports or calls `ConsumeWithHeartbeat`;
- any active child OpenSpec change remains unarchived;
- content is committed after the final #759 archive;
- staging `Refs #759` has not been replaced by `Closes #759` before final implementation review;
- any closing keyword is added or changed after implementation/cross-agent review without restarting those reviews;
- undraft, CI, or merge appears as an active OpenSpec task instead of a hosted post-archive checklist step; or
- required CI has a known unfixed flake.

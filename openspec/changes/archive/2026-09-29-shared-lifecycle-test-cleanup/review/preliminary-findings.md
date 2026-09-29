# Preliminary implementation findings

These findings concern uncommitted implementation drafts. They do not represent final review approval.
Final implementation review must verify the correction and its causal test at the final source checkpoint.

1. Root: support initially joined an expired Stop context error into the component's result and only logged the
   explicit abort outcome. That could conceal a component returning nil or the wrong cause when its caller bound
   won. Keep the raw concrete result separate and require it to preserve the exact caller-context cause. Developer
   reports a focused control for nil, wrong and exact results under pre-ended authority; final review is pending.
2. Independent semstreams-reviewer: the central owner initially checked only interface equality with nil, losing
   earlier `require.NotNil` handling of typed-nil pointers. Refuse typed nil at acquisition and prove no lifecycle
   method is invoked. The preliminary verdict was CHANGES REQUESTED for this bounded support finding.
3. Root: the first rule observer called `ctx.Deadline()` on the deliberate nil-Stop probe and expected exactly one
   Stop for every instance, conflicting with the explicit completed-repeat case. Preserve forwarding of contract
   probes and distinguish them from finite terminal attempts. Final review must check the actual observer and proof.

The independent preliminary reviewer found no other support defect in the inspected lexical finalization,
raw-error separation, Start cancellation ordering, base injection cleanup or worker result draining. Tests, real
adoption, mutation evidence, measured budgets and exact cleanup-baseline reconciliation still require final review.

Further independent proof review required two caller-specific controls: a live peer gated on the parent's observed
failure, including post-Stop Start cancellation; and Initialize/Start prerequisite refusal through TestErrorInjection.
Those controls were added and passed focused race verification. The reviewer then found an unbounded pre-Start
signal wait in the mixed-peer fixture: omitting Start could strand worker factories until the package timeout.
Both fixture gates must share one finite deadline and report missing progress while allowing every worker to join.
The final review must verify that containment correction and its missing-Start failure control.

The reviewer accepts combined evidence for the reachable fatal/authority paths. The fatal child specifically proves
lexical finalization after an Initialize assertion failure, before substrate cleanup. Accepted live authority and
post-Stop cancellation are established by separate causal cases and the exact after-Start/before-Stop mutation;
no claim is made that the fatal child alone establishes all three properties.

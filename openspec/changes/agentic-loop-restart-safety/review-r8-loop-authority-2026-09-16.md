# R8 loop-authority review and bounded judgment

## Inventory review

Independent INVENTORY PASS on 2026-09-16 for the bounded AGENT_LOOPS startup inventory at original SHA-256
`f3248bd511b7510d770f057ec60284e20c9a8c0d5765613331a8748d2179a5f4`.
No missing production acquisition owner or implemented admission gate was found.

Two record corrections were required: nine configuration files, not ten, and the truncated http_activity.go
checksum. The reviewer also identified two additional Bucket-only fixtures in the already inventoried helper
family, `processor/agentic-loop/loop_integration_test.go:61` and `:715`; both reach real startup.
Root corrected these records, and the reviewer approved the exact final materialization at
`4cf85988cdaa429000628afa907b6076fa1f3f54f8de79a7e1b5a4acc1358d32`. All 118 pins verify.

Approval-margin arithmetic and raw-config/port reconciliation remain unproven choices, not implementation
permission. Inventory review does not complete whole R8, waive tests or authorize another storage mechanism.

## Bounded approval-margin judgment

Evidence: `design-r8-loop-authority-lowering-2026-09-16.md`, SHA-256
`e4c9d3a907a6dbc5f80a8d7b56d00e2b02287bf35a1549bf3918107535a76a9c`, and its exact accepted inventory.
The judge answered only whether to retain a fixed approval reserve or amend admission to `0 < timeout < observedTTL`.
This is advisory input, not an owner ruling, design approval or implementation permission.

Recommendation: retain the reserve obligation, but ask the owner to define its intended nominal grace before
selecting a number. Confidence is medium. Evidence that the existing path can durably settle after loop authority
expires would change the recommendation.

The reserve establishes only `TTL - timeout >= M`: minimum nominal headroom for timeout processing, not a
guarantee of completion before expiry. The opened sweeper path publishes rejection work before a native consumer
applies it; publication can fail and retry (`processor/agentic-loop/approval_sweeper.go:115`). Structural checks
confirmed that call and the method recording RequestedAt/Timeout (`agentic/state.go:174`).

The current `continuation_unavailable` contract covers absent matching-gate evidence while reconstruction uses
current LoopEntity. Missing loop authority has a separate absence/unresolved/poison disposition. It is not proven
to produce the same durable failure after AGENT_LOOPS expiry (active loop delta, line 534). This distinction means
the stream-evidence absence contract cannot justify removing all authority headroom.

Strongest case for removing the reserve: strict-before-expiry is simple, keeps the accepted 12-hour default and
does not dress an unsupported constant as a safety guarantee. No finite reserve covers an unbounded outage.
It loses the judge's comparison because it permits arbitrarily small headroom, allowing expiry before even an
ordinary sweep and application delay. Both options use framework-owned rules and observed TTL, not a public knob.

Unproven: no measurement or contract bounds sweep delay, queued application, retries, restart duration or clock
discrepancy. Neither five seconds, twelve hours nor any other numerical margin is established by the evidence.
The record supports the reserve's purpose, not its value. No tests or runtime verification were performed.

The question prepared for the owner is what minimum nominal grace should remain between an approval deadline and
loop-state expiry, using private M and `0 < timeout <= observedTTL - M`; alternatively, whether to amend the
requirement and accept expiry before settlement during ordinary processing delay. No choice is inferred here.

The bucket-identity options were outside this judge question. They remain in the architect's bounded handoff.
No code, schema, configuration, bucket policy or research behavior changed during either read-only review.

## Independent pre-owner design review

DESIGN REVIEW PASS for the exact docket `e4c9d3a9…` and advisory record `d859f4c8…`, as an owner-decision
docket only. Neither option selection nor implementation is approved by this review.

N2 is sound when retirement is limited to the loop-side `LoopsBucket` / `loops_bucket` surface. The existing
port declaration becomes its sole identity, and the existing research agreement check observes that declaration
without changing research runtime semantics. Errors must name the retired key and canonical replacement, or the
conflicting component and bucket identities. Migration removes every loop-side occurrence of the retired key,
including default-valued occurrences, and preserves custom selection through the existing `loops` port.
Tools and stage configuration remain unchanged.

M1 is a legitimate owner choice about desired minimum nominal grace, not a request to predict framework
completion latency. Neither a numerical margin nor a settlement guarantee is established. M2 is a genuine
contract amendment accepting arbitrarily small headroom, not conformance-only lowering.

The owner must select identity policy and the margin purpose/value/boundary before a complete implementation
handoff. No tests, runtime changes or new inventory were performed for this review.

## Concrete product-limit recommendation for owner selection

The architect subsequently recommended presenting a maximum approval wait of 12 hours for this release, also
the already approved default, while retaining loop state for 24 hours. This is a new product-limit proposal,
not a margin inferred from current defaults or measured completion latency. It leaves 12 hours of nominal
grace and adds no public knob or machinery. The existing recovery/refusal contracts remain unchanged.

The strongest downside is that a longer finite setting, such as 18 hours, would fail startup. That excludes
longer delayed-review workflows; their use by adopters has not been established. If such windows are required,
the owner should decline the cap. Any accepted migration must name this limitation and must not silently
shorten configured waits.

Both bucket retirement and this concrete lifetime choice were requested from the owner and recorded in issue
comment `5695760323`. They remain pending; earlier R7 test-cost approval does not select either R8 option.

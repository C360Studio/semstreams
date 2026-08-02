# agentic-loop — delta (payload-size-chokepoints)

## ADDED Requirements

### Requirement: A loop's completion result MUST be durably stored or the loop MUST say otherwise

Completion, failure, and cancellation results MUST NOT be silently absent: a failed result
write MUST be retried when transient and MUST mark the loop with a typed
result-not-durable state when permanent, so no loop reports plain completion while its
result is unreadable. A result exceeding the offload threshold MUST be stored in the
content object store with the KV value carrying a reference, a preview, and the size; the
result read path MUST resolve the reference transparently under its existing paging
contract.

#### Scenario: Oversized result is offloaded and readable

- **WHEN** a loop completes with a result larger than the offload threshold
- **THEN** the result body is stored in the content object store, the completion value
  carries the reference and preview, and the result-reading tool returns the full content
  through its normal paging interface

#### Scenario: A result that cannot be stored is a visible state, not a silent gap

- **WHEN** the completion write fails permanently
- **THEN** the loop's state names result-not-durable with the classified cause, and a
  parent or operator reading the loop can distinguish this from both success and absence

#### Scenario: The published terminal event causally reflects persistence

- **WHEN** any terminal path (completion, failure, cancellation) publishes its event
- **THEN** the durable COMPLETE_ record was written FIRST, and on a final persist failure
  the PUBLISHED event itself carries the result-not-durable marker with the classified
  cause — loop, task, and outcome metadata kept, the undurable result body omitted
- **AND** every public reader of the loop (activity/loops projections, the result-reading
  tool consulting the loop entity on an absent record) surfaces the marker as a typed
  state distinct from success, from still-running, and from never-existed

### Requirement: An over-limit request MUST fail the loop loudly, never silently retry

An `agent.request` publish refused at the wire limit MUST terminate the loop with a typed
failure whose reason names the class and whose error names the request size and the
limit — never a silent retry loop, never a wedged loop. This is the SHIPPED bound of this
change (D5 interim loudness).

> **Deferred in this change ([~] task 4.2):** request-lane hydration — bulky historical
> message content riding the wire as content references, hydrated back to IDENTICAL full
> text before the provider call — is the mechanism that LIFTS this ceiling without
> changing what the model sees. It was deliberately not shipped in this slice (it adds a
> content-store dependency and hydration seam to `agentic-model`, a full slice of its
> own); the deferred requirement and scenario are recorded below and MUST return in a
> follow-up change before the interim bound can be called the final request-lane
> contract.

#### Scenario: Over-limit without hydration is loud and terminal

- **WHEN** a request exceeding the wire limit cannot be bounded
- **THEN** the loop fails with a typed reason naming the request size and the limit, and no
  retry re-attempts the identical over-limit publish

#### Scenario: Deep loop crosses the wire limit (DEFERRED — hydration, task 4.2 [~])

- **WHEN** a loop's accumulated context exceeds the offload threshold mid-run
- **THEN** subsequent requests carry references for bulky historical content, the provider
  receives the identical hydrated text, and the loop proceeds
- **NOTE**: not implemented in this change; until the hydration follow-up lands, this
  situation resolves through the loud-terminal scenario above

### Requirement: The tool-result cap is an ingestion bound, not a wire defense

`tool_result_max_bytes` MUST be documented as bounding what a tool may inject into loop
context from external sources; it MUST NOT be represented as the mechanism keeping
requests under the wire limit, and configuring it to unlimited MUST remain safe because
the substrate seam guard backstops the wire.

#### Scenario: Unlimited ingestion meets the seam guard, not silence

- **WHEN** the tool-result cap is configured unlimited and a tool returns content that
  pushes a request over the wire limit
- **THEN** the outcome is the request lane's typed bound (offload or loud failure), never a
  silent drop

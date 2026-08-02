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

### Requirement: The request lane MUST be bounded without changing what the model sees

Accumulated request content exceeding the offload threshold MUST ride the wire as content
references and MUST be hydrated back to the identical full text before the provider call,
so the wire is bounded while model behavior is unchanged. Until hydration bounds a given
request, an over-limit request MUST fail the loop loudly with a typed reason naming the
size and limit — never a silent retry loop.

#### Scenario: Deep loop crosses the wire limit

- **WHEN** a loop's accumulated context exceeds the offload threshold mid-run
- **THEN** subsequent requests carry references for bulky historical content, the provider
  receives the identical hydrated text, and the loop proceeds

#### Scenario: Over-limit without hydration is loud and terminal

- **WHEN** a request exceeding the wire limit cannot be bounded
- **THEN** the loop fails with a typed reason naming the request size and the limit, and no
  retry re-attempts the identical over-limit publish

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

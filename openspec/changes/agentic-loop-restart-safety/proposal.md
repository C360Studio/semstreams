# Change: agentic-loop restart-safe settlement

## Why

Agentic-loop persists a loop entity but retains material execution and correlation state only in process memory.
Several durable-input handlers also turn missing correlation or downstream failure into a normal callback return. A
replacement process can therefore ACK an input whose required durable transition, result, or downstream publication
did not complete, leaving the loop stranded or silently losing the transition.

This is the owner-classified critical beta.163 vertical in #1146. It is blocked by #759 because semantic JetStream
settlement, cancellation-after-join, and exact delivery-owner control must be established before this change can
define safe redelivery behavior.

## Claim scope

This initial proposal claims design and later implementation of #1146 only. Before implementation, the change will:

- inventory every model, tool, signal, approval, rule-verdict, and loop-transition delivery boundary;
- define the happy- and sad-path durable outcome required before each source ACK;
- measure which restart windows close through source redelivery, stable identity, existing durable results, and
  downstream PubAck;
- choose explicit behavior for provider invocation whose result is unknown after replacement; and
- add real-NATS process-replacement failpoints for the admitted vertical.

The owner-ratified sequence is model ambiguity and settlement, loop transition settlement, dispatch
intake/approval, then governance correlation only where the measured flow requires it.

## Holds

- No production implementation begins until #759 supplies the accepted settlement foundation.
- No supervisor, generic state machine, checkpoint bucket, outbox, event-sourced loop, or universal exactly-once
  claim is admitted by this proposal.
- Error propagation does not land separately from the durable authority that makes redelivery safe.
- Additional persistent state requires a named replacement failpoint proving that source redelivery plus committed
  outputs or references is insufficient.
- The inventory and adopter seam are provisional until independent `INVENTORY PASS`; design and task truth follow
  only after that gate and explicit owner acceptance.

## Impact

- Tracking issue: #1146; parent epic: #1147.
- Blocking prerequisite: #759.
- Blocks restart-safe approval/enforcement claims in #1140.
- Expected capability deltas: `agentic-loop` and the settlement/replay capabilities selected after inventory.
- Required verification includes real-NATS process replacement and a serialized relevant agentic E2E tier.


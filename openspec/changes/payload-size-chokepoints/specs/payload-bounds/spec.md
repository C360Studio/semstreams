# payload-bounds — delta (payload-size-chokepoints)

## Purpose

The substrate size contract, in one place: what every framework write, publish, and
request/reply response does when a payload meets the NATS size limit — where the limit comes
from, how refusal is classified, when bulky content offloads to ObjectStore, and how
size-adjacent configuration is classified (ingestion policy vs wire defense) — so a new
developer reads one spec instead of discovering twenty call sites.

## ADDED Requirements

### Requirement: The payload limit MUST be derived from the server, never compiled in

Every framework size check MUST derive its limit from the connected server's advertised
maximum payload at call time. No framework code or configuration MAY carry its own copy of
the wire limit, and no component MAY expose a knob that restates it.

#### Scenario: Operator raises the server limit

- **WHEN** a deployment runs a NATS server with a raised maximum payload
- **THEN** every framework seam guard honors the raised limit with no code or configuration
  change

### Requirement: An oversized write MUST fail loud and permanent at the seam

Every KV write lane and every publish lane MUST refuse a payload exceeding the derived
limit before sending, with a stable classified permanent error naming the byte count, the
limit, the target subject or bucket and key, and a remedy. The raw server oversize error
MUST also classify as permanent wherever it surfaces, so no retry loop treats an
impossible write as transient.

#### Scenario: Oversized KV write refused

- **WHEN** any framework KV write lane receives a value exceeding the derived limit
- **THEN** the write is refused before sending with the classified permanent error naming
  bytes, limit, bucket, key, and remedy

#### Scenario: Oversize is never retried as transient

- **WHEN** the server's oversize error reaches the framework's error classifier by any path
- **THEN** it classifies as permanent and retry machinery does not re-attempt it

### Requirement: An oversized reply MUST answer as a typed error, never as silence

When a request/reply handler's response exceeds the derived limit, the framework MUST send
the standard classified error reply naming the response size and the limit, so the caller
receives a fast typed permanent error rather than a timeout.

#### Scenario: Caller learns "too large" instead of timing out

- **WHEN** a handler produces a reply exceeding the derived limit
- **THEN** the requester receives a classified response-too-large error naming size and
  limit within the normal reply latency
- **AND** no path leaves the requester to infer the failure from a timeout

### Requirement: Size-adjacent configuration MUST be classified by the limit it defends

A configuration knob bounding payload or content size MUST be documented as either an
ingestion bound (policy on what may enter the system — legitimate, component-level,
stays) or a wire-size defense (the substrate's job — no component knob may claim it). No
component MAY introduce a knob whose purpose is defending the wire limit.

#### Scenario: Ingestion bound stays and is named as such

- **WHEN** a tool-facing knob caps content admitted from an untrusted external source
- **THEN** it is documented as an ingestion bound, remains configurable, and its
  documentation states that wire safety is the seam guard's job, not this knob's

#### Scenario: No new wire-defense knobs

- **WHEN** a change proposes a component-level knob whose stated purpose is keeping
  payloads under the wire limit
- **THEN** the change is refused in review; the seam guard and the offload contract are the
  mechanism

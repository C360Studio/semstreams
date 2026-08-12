# request-reply-response-bounds Specification

## Purpose

Define observed request/reply payload refusal, operation-owned continuation for unbounded results, and registered
Store access in place of a separate ObjectStore request API.

## Requirements
### Requirement: Request/reply oversize classification follows the observed publish result

The shared NATS request responder SHALL encode and attempt the operation's success response first. If and only if that
publish returns `nats.ErrMaxPayload`, it SHALL attempt the canonical small ADR-060 response with class `invalid`, code
`response_too_large`, and observed response/max-payload detail. Every other response-publish error SHALL remain
logged under existing transport behavior. A successful response SHALL never be rejected solely by a prior size
prediction.

`natsclient.Client.MaxPayload()` SHALL return the current active connected-server payload limit or a connection error.
It MAY be used to fit an operation-owned encoded page and provide diagnostics. It SHALL NOT expose the connection,
become adopter configuration, or replace the actual publish as the final correctness guard.

#### Scenario: a changed server limit is decided by publish

- **GIVEN** an operation built a page using a previously observed maximum payload
- **AND** the active server limit changes before response publication
- **WHEN** the responder publishes the success response
- **THEN** the actual publish result decides success or `response_too_large`
- **AND** no preflight observation is treated as an exclusive fence

#### Scenario: unrelated response errors are not misclassified

- **GIVEN** success-response publication fails with an error other than `nats.ErrMaxPayload`
- **WHEN** the responder handles that failure
- **THEN** it logs the transport failure under existing behavior
- **AND** it does not claim the response was oversized

### Requirement: Continuation belongs to the unbounded operation

Graph prefix and trajectory readers SHALL each own their typed page, cursor validation, and page truth. Foundation B
SHALL NOT add a generic continuation envelope, response stream, overflow KV bucket, payload-size knob, or second query
carrier. Core NATS request/reply remains the internal carrier.

#### Scenario: paging does not create another transport protocol

- **WHEN** response-bound implementations are inspected
- **THEN** prefix and trajectory expose their own typed continuation
- **AND** no generic response stream, overflow bucket, or shared continuation wrapper exists

### Requirement: Registered Store access replaces the ObjectStore request API

The ObjectStore component SHALL remain the lifecycle owner and registered `StoreProvider`. Internal consumers SHALL
use `StoreRegistry`, `storage.Store`, and optional `storage.StreamableStore.Open`. The ObjectStore default `api` input,
get/store/list request DTOs and handlers, direct responder, NATS API documentation/tests/schema, and dormant
`graph/llm.NATSContentFetcher` SHALL be absent.

ObjectStore construction SHALL reject every input named `api` and every `nats-request` input. It SHALL retain ordinary
`nats` and `jetstream` write inputs. Every declared ordinary input SHALL bind as a write lane independent of its local
port name; local names are flow-graph labels, not operation selectors. Old explicit request/reply configuration MUST
fail startup; no inert input, deprecated path, alias, or compatibility shim may remain.

#### Scenario: an old ObjectStore API config fails loudly

- **GIVEN** an ObjectStore component with an input named `api` or kind `nats-request`
- **WHEN** component construction validates its ports
- **THEN** construction fails before startup
- **AND** no unused subscription or compatibility handler is installed

#### Scenario: native registered-store access remains

- **GIVEN** a running ObjectStore provider registered under its logical instance
- **WHEN** an internal authorized consumer needs stored content
- **THEN** it resolves that instance through `StoreRegistry`
- **AND** it may use `StreamableStore.Open` for a streaming read without a NATS request/reply body

#### Scenario: renamed ordinary inputs remain active

- **GIVEN** an ObjectStore component with ordinary NATS or JetStream inputs whose local names are not `write`
- **WHEN** the component starts
- **THEN** every declared input binds its configured subject as a write lane
- **AND** each distinct JetStream stream/subject binding has a legal, deterministic durable identity independent of
  declaration order and local port name
- **AND** an existing non-colliding durable identity remains unchanged
- **AND** stopping the component tears down every local input binding

#### Scenario: duplicate effective write bindings fail before startup

- **GIVEN** two ordinary Core NATS inputs with the same subject
- **OR** two JetStream inputs with the same stream and subject
- **WHEN** ObjectStore construction plans its bindings
- **THEN** construction rejects the duplicate before any store or subscription I/O
- **AND** the error identifies the declaring ports deterministically

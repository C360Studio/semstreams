# Semantic Boundary Research Note

## Context

This note tracks external research on semantic boundaries, interoperability,
and agent memory as input for SemStreams design.

The immediate trigger was a LinkedIn discussion about semantic agent memory:
RDF 1.2, RDF-star/Turtle reification, OWL, SHACL, provenance, confidence, and
agent memory as a semantic knowledge layer. One useful thread pointed at the
W3C Holon Community Group's Holon Graph Architecture (HGA) with the framing
that RDF 1.2 is not sufficient by itself, but may be part of a larger
architecture.

Primary source for this snapshot:

- https://github.com/w3c-cg/holon/tree/main/architectures/hga

Source status as of 2026-06-10:

- Editor's Draft under the W3C Holon Community Group.
- The table of contents identifies Kurt Cagle as editor and Chloe Shannon as
  LLM-assisted transformer/orchestrator.
- HGA is large and evolving: primer, namespace registry, core holon structure,
  events, provenance, Bayesian/active inference, policy, verifiable
  credentials, Markov blankets, projections, and media vocabularies.

Treat HGA as research signal, not as a dependency, vocabulary source, or
conformance target.

## Why It Matters

HGA is interesting because it does not claim that RDF 1.2 alone solves
interoperability. It uses RDF, SHACL, PROV-O, SKOS, ODRL, verifiable
credentials, and reification inside a broader holonic architecture with
explicit boundaries, event envelopes, payload separation, provenance, and
projection views.

It is also a cautionary example. Terms such as "holon" and "Markov blanket"
may be precise in their source communities, but they create an immediate
onboarding burden for field users. SemStreams should extract the operational
shape behind those terms rather than importing the terms as product or
framework vocabulary.

That posture is close to SemStreams' current design instinct:

- Keep the runtime substrate pragmatic.
- Keep semantic-web machinery at interoperability, validation, and export
  boundaries.
- Do not confuse stored triples with grounded meaning.
- Make boundaries, provenance, claims, projections, and failures inspectable.

## HGA Ideas Worth Tracking

### Boundary-Bearing Units

HGA defines "holons" as whole/part entities with identity, registration status,
payload graph, and boundary declarations. The useful SemStreams idea is not the
word "holon"; it is the idea that a named entity can carry a clear semantic
boundary for what is valid inside it.

SemStreams analogue:

- Entity state plus triples already gives named operational facts.
- `Graphable` entities provide a lightweight way to project domain objects into
  graph state.
- Lifecycle-managed entities and typed artifact entities are already moving
  toward explicit named units with durable state and references.

Research question:

- Do SemStreams resource/entity classes need a more explicit "semantic
  boundary profile" concept for external validation, without importing SHACL
  into the live engine?

### Event Envelope Versus Payload

HGA's event model separates a closed event envelope from a domain-specific
payload. The envelope carries routing, time, provenance, and event type; the
payload is interpreted against the target holon's boundary.

SemStreams analogue:

- Payload registry and `BaseMessage` already separate envelope-ish metadata
  from typed payloads.
- Rules should pass references, not bulky payloads.
- Typed artifact entities already keep large structured content out of triples.

Research question:

- Should SemStreams document a stricter envelope/payload vocabulary for
  external semantic event exchange?

### Rejection And Violation Events

HGA treats invalid commands, unresolved targets, and shape violations as
explicit events that do not mutate the scene graph.

SemStreams analogue:

- Governance, lifecycle, and tool-call rules already prefer auditable outcomes
  over silent failure.
- The rule/component boundary says rules trigger work and components execute
  work; state changes should be explicit.

Research question:

- Do graph mutation failures need a standard rejection/violation event surface
  so downstream products can observe semantic failures consistently?

### Provenance At Multiple Levels

HGA separates envelope-level provenance from payload-level provenance and from
document/process-stamp provenance.

SemStreams analogue:

- `message.Triple` carries `Source`, `Timestamp`, `Confidence`, `Context`,
  `Datatype`, and `ExpiresAt`.
- Agent memory publishes extracted facts as graph mutations.
- The open gap is less "add provenance fields" and more "separate claim,
  evidence, extraction, validation, and confidence semantics."

Research question:

- Should SemStreams define a first-class claim/evidence/profile model before
  expanding RDF/SHACL compatibility?

### Projections As Read-Only Views

HGA projections are read-only envelopes over content. A projection may be a
filtered view, depiction, report, or rendered artifact, but it must not mutate
the source graph.

SemStreams analogue:

- Query gateways, GraphRAG/PathRAG, rule-driven artifacts, and RDF export all
  behave like projections.
- This reinforces the existing boundary: read/query/export surfaces must not
  become hidden mutation paths.

Research question:

- Should gateway surfaces use a shared "projection" vocabulary for ephemeral
  query responses versus persistent artifacts?

### Private Agent State And Observable Surfaces

HGA's Markov-blanket pass separates agent internal state from sensory and
active surfaces. It is explicitly marked at risk, but the boundary idea is
useful: private agent cognition should not be directly readable by external
agents; observation happens through projections and emissions.

SemStreams analogue:

- ADR-036 already distinguishes private agent state from observable state.
- Agentic components communicate through events, results, graph facts, and
  durable references rather than direct inspection of hidden model context.

Research question:

- Should SemStreams' agent-private-state docs reference a lightweight
  input/output surface pattern without adopting the Markov terminology?

## Non-Goals For SemStreams

- Do not adopt HGA as a dependency.
- Do not make SHACL, SPARQL, OWL, or RDF 1.2 reification required inside the
  live SemStreams runtime.
- Do not make Holon terminology user-facing unless it earns its keep.
- Do not make Markov-blanket terminology user-facing unless field users already
  use it in the domain being modeled.
- Do not treat an editor's draft as a stable standard.
- Do not replace SemStreams' KV/watch/stream primitives with triplestore-first
  architecture.

## Possible Follow-Ups

1. Track HGA as part of post-v1 standards/interoperability research.
2. Compare HGA event envelopes with SemStreams payload registry and
   graph-mutation events.
3. Compare HGA projections with SemStreams query gateways, rule-driven
   artifacts, and RDF export.
4. File a focused issue only if one of the research questions becomes a
   concrete pre-v1 interoperability gap.

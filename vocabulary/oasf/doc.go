// Package oasf provides Go constants for the AGNTCY Open Agentic Schema
// Framework (OASF) skills taxonomy.
//
// OASF is the publish-side standard for describing agent capabilities in
// the AGNTCY directory network. Each skill is identified by a uint32
// class ID drawn from a published hierarchical taxonomy:
//
//   - 1-digit category (e.g., 1 = Natural Language Processing)
//   - 3-digit subcategory (e.g., 101 = Natural Language Understanding)
//   - 5-digit specific skill (e.g., 10101 = Contextual Comprehension)
//
// This package surfaces those IDs and their hierarchical names as Go
// constants, paired with lookup helpers. SemStreams' [oasf-generator]
// component uses them to populate the Skill.id and Skill.name fields in
// records published to AGNTCY-conformant directories.
//
// # Standards-at-work, not semweb hell
//
// This package follows the [vocabulary] family pattern (agentic, bfo,
// cco, plus the standards.go IRI roster):
//
//   - Adopts an external vocabulary as first-class Go constants
//   - Keeps internal access patterns plain JSON / Go — no OWL inferencing,
//     no SPARQL, no operator-authored RDF
//   - Sub-package boundary contains the schema dependency; the wider
//     semstreams module does not import oasf except where wire conformance
//     is required
//
// See [ADR-042] for the full rationale and selection methodology.
//
// # Coverage
//
// MVP coverage is 10 top-level categories — see categories.go. Skill IDs
// outside the published taxonomy use the extension range defined in
// extensions.go. Specific subcategories and skills are added as concrete
// internal capabilities surface a need; see Name and ID for the lookup
// helpers callers should use rather than the raw maps.
//
// # External references
//
//   - Spec: https://docs.agntcy.org/oasf/open-agentic-schema-framework/
//   - Taxonomy browser: https://schema.oasf.outshift.com/
//   - Source schemas: https://github.com/agntcy/oasf/tree/main/schema/skills
//
// [oasf-generator]: ../../processor/oasf-generator
// [vocabulary]: ..
// [ADR-042]: ../../docs/adr/042-oasf-taxonomy-adoption.md
package oasf

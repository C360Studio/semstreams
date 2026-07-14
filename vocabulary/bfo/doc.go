// Package bfo provides IRI constants for the Basic Formal Ontology (BFO) 2.0.
//
// BFO is the ISO 21838-2 standard upper-level ontology that provides a domain-neutral
// foundation for building more specific ontologies. It defines the most general categories
// of entities that exist in reality.
//
// # Overview
//
// BFO distinguishes between two fundamental categories:
//
//   - Continuants: Entities that persist through time while maintaining identity (objects, qualities, roles)
//   - Occurrents: Entities that unfold in time (processes, events, temporal regions)
//
// # Key Concepts
//
// ## Continuants
//
// Continuants are entities that continue to exist through time. They include:
//
//   - Independent Continuants: Can exist on their own (material entities, objects, sites)
//   - Specifically Dependent Continuants: Depend on a specific bearer (qualities, roles, dispositions)
//   - Generically Dependent Continuants: Can migrate between bearers (information patterns)
//
// ## Occurrents
//
// Occurrents are entities that happen or unfold in time:
//
//   - Processes: Changes that occur over time
//   - Temporal Regions: Spans of time
//   - Spatiotemporal Regions: Regions of spacetime
//
// # Usage
//
// Use these constants when mapping SemStreams entities to BFO classes for ontology compliance:
//
//	import "github.com/c360studio/semstreams/vocabulary/bfo"
//
//	// Map a physical asset to BFO Object
//	triple := message.Triple{
//	    Subject:   entityID,
//	    Predicate: "rdf.type.class",
//	    Object:    bfo.Object,  // http://purl.obolibrary.org/obo/BFO_0000030
//	}
//
//	// Use BFO relations for participation
//	triple := message.Triple{
//	    Subject:   droneID,
//	    Predicate: "bfo.participates.in",
//	    Object:    missionProcessID,
//	}
//	// Map predicate to: bfo.ParticipatesIn
//
// # Relationship to Other Ontologies
//
// BFO serves as the foundation for many domain ontologies:
//
//   - CCO (Common Core Ontologies): Mid-level ontology built on BFO
//   - OBI (Ontology for Biomedical Investigations): Biomedical domain
//   - IAO (Information Artifact Ontology): Information entities
//   - RO (Relations Ontology): Extended relation vocabulary
//
// # References
//
//   - BFO 2.0 Specification: https://basic-formal-ontology.org/
//   - ISO 21838-2: https://www.iso.org/standard/74572.html
//   - OBO Foundry: http://obofoundry.org/ontology/bfo.html
//   - BFO 2020 Reference: https://github.com/BFO-ontology/BFO-2020
package bfo

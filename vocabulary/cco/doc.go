// Package cco provides IRI constants for the Common Core Ontologies (CCO).
//
// CCO is a mid-level ontology suite built on BFO (Basic Formal Ontology) that provides
// common concepts for information entities, agents, actions, and artifacts. It bridges
// the gap between the abstract categories of BFO and domain-specific ontologies.
//
// # Overview
//
// CCO extends BFO with reusable concepts that appear across many domains:
//
//   - Information Content Entities: Data, documents, specifications, identifiers
//   - Agents: People, organizations, software agents
//   - Acts/Actions: Intentional activities, communication, decision-making
//   - Artifacts: Physical and digital objects created for a purpose
//
// # Key Concepts
//
// ## Information Content Entities (ICE)
//
// ICEs are generically dependent continuants that are about something:
//
//   - Descriptive ICE: Information that describes (names, identifiers, measurements)
//   - Directive ICE: Information that prescribes (plans, specifications, requirements)
//
// ## Agents
//
// Entities capable of bearing roles and performing acts:
//
//   - Person: Human agents
//   - Organization: Collective agents with structure
//   - IntelligentSoftwareAgent: AI/software capable of autonomous action
//
// ## Acts
//
// Processes that are intentionally performed by agents:
//
//   - IntentionalAct: Actions done with purpose
//   - ActOfCommunication: Transmission of information
//   - ActOfDecisionMaking: Choosing between alternatives
//
// # Usage
//
// Use these constants when mapping SemStreams entities to CCO classes:
//
//	import "github.com/c360studio/semstreams/vocabulary/cco"
//
//	// Map a drone to an intelligent software agent
//	triple := message.Triple{
//	    Subject:   droneID,
//	    Predicate: "rdf.type.class",
//	    Object:    cco.IntelligentSoftwareAgent,
//	}
//
//	// Map a mission plan document
//	triple := message.Triple{
//	    Subject:   planID,
//	    Predicate: "rdf.type.class",
//	    Object:    cco.PlanSpecification,
//	}
//
//	// Use CCO relations for information content
//	vocabulary.Register("info.content.about",
//	    vocabulary.WithDescription("What this information content is about"),
//	    vocabulary.WithIRI(cco.IsAbout))
//
// # Relationship to BFO
//
// CCO classes extend BFO categories:
//
//   - InformationContentEntity extends BFO:GenericallyDependentContinuant
//   - Agent extends BFO:Object
//   - Act extends BFO:Process
//   - Artifact extends BFO:MaterialEntity
//
// # Ontology Suite
//
// CCO consists of several modules:
//
//   - Information Entity Ontology (IEO): Documents, data, identifiers
//   - Agent Ontology (AO): People, organizations, software
//   - Event Ontology (EO): Acts, events, occurrences
//   - Artifact Ontology (ArO): Physical and digital artifacts
//   - Geospatial Ontology (GO): Locations, regions, features
//   - Time Ontology (TO): Temporal entities and relations
//
// # References
//
//   - CCO GitHub: https://github.com/CommonCoreOntology/CommonCoreOntologies
//   - CCO Documentation: https://www.nist.gov/programs-projects/common-core-ontologies
//   - OBO Foundry: http://obofoundry.org/
package cco

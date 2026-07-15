package vocabulary

// Standard Vocabulary IRIs
//
// These constants provide commonly used W3C and semantic web standard IRIs.
// Use these in PredicateMetadata.StandardIRI to indicate semantic equivalence
// with established vocabularies.
//
// NOTE: SemStreams uses dotted notation internally (e.g., "semantic.identity.alias")
// for NATS stream query compatibility. These IRIs are for documentation, export,
// and interoperability purposes. Processors can translate between formats if needed.
//
// References:
// - OWL: https://www.w3.org/TR/owl2-overview/
// - SKOS: https://www.w3.org/TR/skos-reference/
// - Dublin Core: https://www.dublincore.org/specifications/dublin-core/dcmi-terms/
// - Schema.org: https://schema.org/
// - PROV-O: https://www.w3.org/TR/prov-o/

// OWL (Web Ontology Language) Standard IRIs
const (
	// OwlSameAs indicates that two URI references refer to the same entity.
	// Used for: AliasTypeIdentity
	// Example: "drone-001" owl:sameAs "c360.platform.test.drone.001"
	OwlSameAs = "http://www.w3.org/2002/07/owl#sameAs"

	// OwlEquivalentClass indicates equivalent classes
	OwlEquivalentClass = "http://www.w3.org/2002/07/owl#equivalentClass"

	// OwlEquivalentProperty indicates equivalent properties
	OwlEquivalentProperty = "http://www.w3.org/2002/07/owl#equivalentProperty"

	// OwlInverseOf indicates that two properties are inverse of each other.
	// Used to link predicates that represent opposite directions of a relationship.
	// Example: skos:broader owl:inverseOf skos:narrower
	OwlInverseOf = "http://www.w3.org/2002/07/owl#inverseOf"

	// OwlSymmetricProperty indicates a property where if A relates to B,
	// then B also relates to A with the same property.
	// Example: skos:related is symmetric (if A is related to B, B is related to A)
	OwlSymmetricProperty = "http://www.w3.org/2002/07/owl#SymmetricProperty"

	// OwlTransitiveProperty indicates a property where if A→B and B→C, then A→C.
	// Example: skos:broaderTransitive
	OwlTransitiveProperty = "http://www.w3.org/2002/07/owl#TransitiveProperty"

	// OwlReflexiveProperty indicates a property that always applies to itself.
	// Example: owl:sameAs is reflexive (everything is sameAs itself)
	OwlReflexiveProperty = "http://www.w3.org/2002/07/owl#ReflexiveProperty"
)

// SKOS (Simple Knowledge Organization System) Standard IRIs
const (
	// SkosPrefLabel provides the preferred lexical label for a resource.
	// Used for: AliasTypeLabel
	// Example: "Alpha Drone" is the preferred display name
	SkosPrefLabel = "http://www.w3.org/2004/02/skos/core#prefLabel"

	// SkosAltLabel provides an alternative lexical label for a resource.
	// Used for: AliasTypeLabel
	// Example: "Drone A", "Alpha", "UAV-001" are alternate labels
	SkosAltLabel = "http://www.w3.org/2004/02/skos/core#altLabel"

	// SkosHiddenLabel provides a label not intended for display but useful for search.
	// Example: Common misspellings, abbreviations
	SkosHiddenLabel = "http://www.w3.org/2004/02/skos/core#hiddenLabel"

	// SkosNotation provides a notation (code or identifier) within a concept scheme.
	// Used for: AliasTypeAlternate
	SkosNotation = "http://www.w3.org/2004/02/skos/core#notation"

	// SkosBroader indicates a hierarchical link to a more general concept.
	// Used for: hierarchy.*.member predicates (entity → container membership)
	// Example: sensor-001 skos:broader temperature-group (sensor is narrower than group)
	SkosBroader = "http://www.w3.org/2004/02/skos/core#broader"

	// SkosNarrower indicates a hierarchical link to a more specific concept.
	// Inverse of SkosBroader.
	// Example: temperature-group skos:narrower sensor-001 (group contains sensor)
	SkosNarrower = "http://www.w3.org/2004/02/skos/core#narrower"

	// SkosRelated indicates an associative (non-hierarchical) link between concepts.
	// Used for: hierarchy.type.sibling predicates (symmetric relationship)
	// Example: sensor-001 skos:related sensor-002 (siblings in same type group)
	SkosRelated = "http://www.w3.org/2004/02/skos/core#related"
)

// RDF Schema Standard IRIs
const (
	// RdfsLabel provides a human-readable name for a resource.
	// Used for: AliasTypeLabel
	RdfsLabel = "http://www.w3.org/2000/01/rdf-schema#label"

	// RdfsComment provides a human-readable description
	RdfsComment = "http://www.w3.org/2000/01/rdf-schema#comment"

	// RdfsSeeAlso indicates a resource that provides additional information
	RdfsSeeAlso = "http://www.w3.org/2000/01/rdf-schema#seeAlso"
)

// Dublin Core Metadata Terms Standard IRIs
const (
	// DcIdentifier provides an unambiguous reference to the resource.
	// Used for: AliasTypeExternal
	// Example: ISBN, DOI, serial number
	DcIdentifier = "http://purl.org/dc/terms/identifier"

	// DcTitle provides the name given to the resource.
	// Used for: AliasTypeLabel
	DcTitle = "http://purl.org/dc/terms/title"

	// DcAlternative provides an alternative name for the resource.
	// Used for: AliasTypeAlternate
	DcAlternative = "http://purl.org/dc/terms/alternative"

	// DcSource indicates a related resource from which the described resource is derived.
	DcSource = "http://purl.org/dc/terms/source"
)

// Dublin Core Dotted Notation Predicates
// These constants provide dotted notation predicates for use in Triples.
// They map semantically to the Dublin Core IRIs above.
const (
	// DCTermsTitle is the dotted notation predicate for resource title.
	// Maps to: DcTitle (http://purl.org/dc/terms/title)
	DCTermsTitle = "dc.terms.title"

	// DCTermsCreator is the dotted notation predicate for resource creator.
	// Maps to: http://purl.org/dc/terms/creator
	DCTermsCreator = "dc.terms.creator"

	// DCTermsIdentifier is the dotted notation predicate for resource identifier.
	// Maps to: DcIdentifier (http://purl.org/dc/terms/identifier)
	DCTermsIdentifier = "dc.terms.identifier"
)

// Schema.org Standard IRIs
const (
	// SchemaName provides the name of the item.
	// Used for: AliasTypeLabel
	SchemaName = "https://schema.org/name"

	// SchemaAlternateName provides an alias for the item.
	// Used for: AliasTypeAlternate
	SchemaAlternateName = "https://schema.org/alternateName"

	// SchemaIdentifier provides a unique identifier for the item.
	// Used for: AliasTypeExternal
	SchemaIdentifier = "https://schema.org/identifier"

	// SchemaSameAs indicates a URL that unambiguously indicates the item's identity.
	// Used for: AliasTypeIdentity
	SchemaSameAs = "https://schema.org/sameAs"
)

// PROV-O (Provenance Ontology) Standard IRIs
// Reference: https://www.w3.org/TR/prov-o/

// PROV-O Namespace
const (
	// ProvNamespace is the base IRI prefix for all PROV-O terms.
	ProvNamespace = "http://www.w3.org/ns/prov#"
)

// PROV-O Core Classes
// The three fundamental types in PROV-O provenance model.
const (
	// ProvEntity is a physical, digital, conceptual, or other kind of thing
	// with some fixed aspects. Entities can be real or imaginary.
	// Examples: a document, a dataset, a concept, a physical object
	ProvEntity = ProvNamespace + "Entity"

	// ProvActivity is something that occurs over a period of time and acts upon
	// or with entities. It may include consuming, processing, transforming,
	// modifying, relocating, using, or generating entities.
	// Examples: a workflow execution, a file edit, a data transformation
	ProvActivity = ProvNamespace + "Activity"

	// ProvAgent is something that bears some form of responsibility for an
	// activity taking place, for the existence of an entity, or for another
	// agent's activity.
	// Examples: a person, an organization, a software agent
	ProvAgent = ProvNamespace + "Agent"
)

// PROV-O Extended Classes
// Additional entity and activity subtypes.
const (
	// ProvPlan is a set of actions or steps intended by one or more agents
	// to achieve some goals.
	// Examples: a mission plan, a workflow definition
	ProvPlan = ProvNamespace + "Plan"

	// ProvCollection is an entity that provides a structure to group other entities.
	// Examples: a dataset, a folder, a fleet
	ProvCollection = ProvNamespace + "Collection"

	// ProvBundle is a named set of provenance descriptions. It is itself an entity
	// so that its provenance can be described.
	// Examples: a provenance graph, a set of related provenance statements
	ProvBundle = ProvNamespace + "Bundle"

	// ProvEmptyCollection is a collection without any members.
	ProvEmptyCollection = ProvNamespace + "EmptyCollection"

	// ProvLocation is a location that is relevant to an entity.
	// Examples: a geographic location, a URL, a file path
	ProvLocation = ProvNamespace + "Location"

	// ProvSoftwareAgent is a running software agent.
	// Examples: a microservice, a daemon process
	ProvSoftwareAgent = ProvNamespace + "SoftwareAgent"

	// ProvPerson is a person agent.
	ProvPerson = ProvNamespace + "Person"

	// ProvOrganization is an organization agent.
	ProvOrganization = ProvNamespace + "Organization"
)

// PROV-O Derivation Relations
// Relations expressing how entities derive from other entities.
const (
	// ProvWasAttributedTo indicates who an entity was attributed to.
	// Domain: Entity, Range: Agent
	ProvWasAttributedTo = ProvNamespace + "wasAttributedTo"

	// ProvWasDerivedFrom indicates a derivation relationship.
	// Domain: Entity, Range: Entity
	// Example: processedData wasDerivedFrom rawData
	ProvWasDerivedFrom = ProvNamespace + "wasDerivedFrom"

	// ProvHadPrimarySource indicates a primary source.
	// A primary source is a specialized derivation for sources.
	// Domain: Entity, Range: Entity
	ProvHadPrimarySource = ProvNamespace + "hadPrimarySource"

	// ProvWasQuotedFrom indicates the entity was quoted from another entity.
	// Domain: Entity, Range: Entity
	ProvWasQuotedFrom = ProvNamespace + "wasQuotedFrom"

	// ProvWasRevisionOf indicates the entity is a revised version of another.
	// Domain: Entity, Range: Entity
	ProvWasRevisionOf = ProvNamespace + "wasRevisionOf"
)

// PROV-O Generation and Usage Relations
// Relations expressing how entities are generated and used by activities.
const (
	// ProvWasGeneratedBy indicates an entity was generated by an activity.
	// Domain: Entity, Range: Activity
	// Example: report wasGeneratedBy analysisActivity
	ProvWasGeneratedBy = ProvNamespace + "wasGeneratedBy"

	// ProvGenerated indicates an activity generated an entity (inverse of wasGeneratedBy).
	// Domain: Activity, Range: Entity
	ProvGenerated = ProvNamespace + "generated"

	// ProvUsed indicates an activity used an entity.
	// Domain: Activity, Range: Entity
	// Example: transformActivity used inputData
	ProvUsed = ProvNamespace + "used"

	// ProvWasInvalidatedBy indicates an entity was invalidated by an activity.
	// Domain: Entity, Range: Activity
	ProvWasInvalidatedBy = ProvNamespace + "wasInvalidatedBy"

	// ProvInvalidated indicates an activity invalidated an entity.
	// Domain: Activity, Range: Entity
	ProvInvalidated = ProvNamespace + "invalidated"
)

// PROV-O Association Relations
// Relations expressing how agents are associated with activities.
const (
	// ProvWasAssociatedWith indicates an activity was associated with an agent.
	// Domain: Activity, Range: Agent
	// Example: missionActivity wasAssociatedWith pilotAgent
	ProvWasAssociatedWith = ProvNamespace + "wasAssociatedWith"

	// ProvActedOnBehalfOf indicates an agent acted on behalf of another agent.
	// Domain: Agent, Range: Agent
	// Example: droneAgent actedOnBehalfOf operatorAgent
	ProvActedOnBehalfOf = ProvNamespace + "actedOnBehalfOf"

	// ProvHadMember indicates membership in a collection.
	// Domain: Collection, Range: Entity
	// Used for: GraphRelContains
	ProvHadMember = ProvNamespace + "hadMember"
)

// PROV-O Influence Relations
// General influence relations between provenance elements.
const (
	// ProvWasInfluencedBy indicates one element was influenced by another.
	// Most other PROV-O relations are specializations of this.
	// Domain: Entity|Activity|Agent, Range: Entity|Activity|Agent
	ProvWasInfluencedBy = ProvNamespace + "wasInfluencedBy"

	// ProvInfluenced indicates one element influenced another (inverse).
	// Domain: Entity|Activity|Agent, Range: Entity|Activity|Agent
	ProvInfluenced = ProvNamespace + "influenced"

	// ProvWasInformedBy indicates an activity was informed by another activity.
	// The first activity used an entity generated by the second.
	// Domain: Activity, Range: Activity
	ProvWasInformedBy = ProvNamespace + "wasInformedBy"

	// ProvWasStartedBy indicates an activity was started by an entity.
	// Domain: Activity, Range: Entity
	ProvWasStartedBy = ProvNamespace + "wasStartedBy"

	// ProvWasEndedBy indicates an activity was ended by an entity.
	// Domain: Activity, Range: Entity
	ProvWasEndedBy = ProvNamespace + "wasEndedBy"

	// ProvHadActivity indicates a qualified relation had an activity.
	// Used in qualified influence patterns.
	ProvHadActivity = ProvNamespace + "hadActivity"
)

// PROV-O Qualified Relations
// Properties used in qualified influence patterns for detailed provenance.
const (
	// ProvQualifiedGeneration links an entity to its qualified generation.
	// Domain: Entity, Range: Generation
	ProvQualifiedGeneration = ProvNamespace + "qualifiedGeneration"

	// ProvQualifiedUsage links an activity to its qualified usage.
	// Domain: Activity, Range: Usage
	ProvQualifiedUsage = ProvNamespace + "qualifiedUsage"

	// ProvQualifiedAssociation links an activity to its qualified association.
	// Domain: Activity, Range: Association
	ProvQualifiedAssociation = ProvNamespace + "qualifiedAssociation"

	// ProvQualifiedDerivation links an entity to its qualified derivation.
	// Domain: Entity, Range: Derivation
	ProvQualifiedDerivation = ProvNamespace + "qualifiedDerivation"

	// ProvQualifiedAttribution links an entity to its qualified attribution.
	// Domain: Entity, Range: Attribution
	ProvQualifiedAttribution = ProvNamespace + "qualifiedAttribution"

	// ProvQualifiedDelegation links an agent to its qualified delegation.
	// Domain: Agent, Range: Delegation
	ProvQualifiedDelegation = ProvNamespace + "qualifiedDelegation"

	// ProvQualifiedInfluence links to a qualified influence.
	ProvQualifiedInfluence = ProvNamespace + "qualifiedInfluence"

	// ProvHadPlan links an association to the plan that was followed.
	// Domain: Association, Range: Plan
	ProvHadPlan = ProvNamespace + "hadPlan"

	// ProvHadRole links a qualified relation to the role played.
	// Domain: Influence, Range: Role
	ProvHadRole = ProvNamespace + "hadRole"
)

// PROV-O Time Properties
// Properties for expressing when activities occurred and entities existed.
const (
	// ProvStartedAtTime indicates when an activity started.
	// Domain: Activity, Range: xsd:dateTime
	ProvStartedAtTime = ProvNamespace + "startedAtTime"

	// ProvEndedAtTime indicates when an activity ended.
	// Domain: Activity, Range: xsd:dateTime
	ProvEndedAtTime = ProvNamespace + "endedAtTime"

	// ProvGeneratedAtTime indicates when an entity was generated.
	// Domain: Entity, Range: xsd:dateTime
	ProvGeneratedAtTime = ProvNamespace + "generatedAtTime"

	// ProvInvalidatedAtTime indicates when an entity was invalidated.
	// Domain: Entity, Range: xsd:dateTime
	ProvInvalidatedAtTime = ProvNamespace + "invalidatedAtTime"

	// ProvAtTime indicates when an instantaneous event occurred.
	// Domain: InstantaneousEvent, Range: xsd:dateTime
	ProvAtTime = ProvNamespace + "atTime"
)

// PROV-O Location Properties
// Properties for expressing where activities occurred.
const (
	// ProvAtLocation indicates where an instantaneous event occurred.
	// Domain: Entity|Activity, Range: Location
	ProvAtLocation = ProvNamespace + "atLocation"
)

// PROV-O Value Properties
// Properties for expressing values.
const (
	// ProvValue provides a direct representation of an entity's value.
	// Domain: Entity, Range: any
	ProvValue = ProvNamespace + "value"
)

// Dublin Core Relations Standard IRIs
const (
	// DcReferences indicates a related resource that is referenced by the described resource
	// Used for: GraphRelReferences
	DcReferences = "http://purl.org/dc/terms/references"

	// DcIsReferencedBy indicates a related resource that references the described resource
	// Inverse of DcReferences
	DcIsReferencedBy = "http://purl.org/dc/terms/isReferencedBy"

	// DcRequires indicates a related resource that is required by the described resource
	// Used for: GraphRelDependsOn
	DcRequires = "http://purl.org/dc/terms/requires"

	// DcIsRequiredBy indicates a related resource that requires the described resource
	// Inverse of DcRequires
	DcIsRequiredBy = "http://purl.org/dc/terms/isRequiredBy"

	// DcReplaces indicates a related resource that is supplanted by the described resource
	// Used for: GraphRelSupersedes
	DcReplaces = "http://purl.org/dc/terms/replaces"

	// DcIsReplacedBy indicates a related resource that supplants the described resource
	// Inverse of DcReplaces
	DcIsReplacedBy = "http://purl.org/dc/terms/isReplacedBy"

	// DcRelation indicates a related resource (generic relationship)
	// Used for: GraphRelRelatedTo
	DcRelation = "http://purl.org/dc/terms/relation"
)

// Schema.org Relationship IRIs
const (
	// SchemaAbout indicates the subject matter of the content
	// Used for: GraphRelDiscusses
	SchemaAbout = "http://schema.org/about"

	// SchemaIsPartOf indicates that this item is part of something else
	SchemaIsPartOf = "http://schema.org/isPartOf"

	// SchemaHasPart indicates that something is part of this item
	// Used for: GraphRelContains
	SchemaHasPart = "http://schema.org/hasPart"
)

// FOAF (Friend of a Friend) Standard IRIs
const (
	// FoafName provides a person's or thing's name
	// Used for: AliasTypeLabel
	FoafName = "http://xmlns.com/foaf/0.1/name"

	// FoafNick provides a short informal nickname
	// Used for: AliasTypeAlternate
	FoafNick = "http://xmlns.com/foaf/0.1/nick"

	// FoafAccountName provides an account name
	// Used for: AliasTypeCommunication
	FoafAccountName = "http://xmlns.com/foaf/0.1/accountName"
)

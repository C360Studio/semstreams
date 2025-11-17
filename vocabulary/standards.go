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
const (
	// ProvWasAttributedTo indicates who an entity was attributed to
	ProvWasAttributedTo = "http://www.w3.org/ns/prov#wasAttributedTo"

	// ProvWasDerivedFrom indicates a derivation relationship
	ProvWasDerivedFrom = "http://www.w3.org/ns/prov#wasDerivedFrom"

	// ProvHadPrimarySource indicates a primary source
	ProvHadPrimarySource = "http://www.w3.org/ns/prov#hadPrimarySource"

	// ProvHadMember indicates membership in a collection
	// Used for: GraphRelContains
	ProvHadMember = "http://www.w3.org/ns/prov#hadMember"
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

// SSN (Semantic Sensor Network Ontology) Standard IRIs
// Useful for IoT and robotics applications
const (
	// SsnHasDeployment indicates where/when a system is deployed
	SsnHasDeployment = "http://www.w3.org/ns/ssn/hasDeployment"

	// SosaObserves indicates what property a sensor observes
	SosaObserves = "http://www.w3.org/ns/sosa/observes"

	// SosaHasSimpleResult provides the simple result value
	SosaHasSimpleResult = "http://www.w3.org/ns/sosa/hasSimpleResult"
)

package llm

// CommunitySummaryData is the data structure for community_summary prompts.
type CommunitySummaryData struct {
	EntityCount    int
	Domains        []DomainGroup // Grouped by the named Domain position of the entity ID
	DominantDomain string        // Most common domain, or "mixed"
	OrgPlatform    string        // Common org.platform if uniform
	Keywords       string
	SampleEntities []EntityParts // Parsed entity samples
}

// SearchAnswerData is the data structure for search_answer prompts.
type SearchAnswerData struct {
	Query       string
	Communities []CommunitySummaryInfo
	Entities    []EntitySample
}

// CommunitySummaryInfo contains community info for search prompts.
type CommunitySummaryInfo struct {
	Summary     string
	EntityCount int
	Keywords    string
}

// EntitySample represents a sample entity for search prompts.
type EntitySample struct {
	ID          string
	Type        string
	Name        string
	Description string // From content store abstract (optional)
}

// EntityDescriptionData is the data structure for entity_description prompts.
type EntityDescriptionData struct {
	ID            string
	Type          string
	Properties    []PropertyInfo
	Relationships []RelationshipInfo
}

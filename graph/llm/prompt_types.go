package llm

// EntityContent contains description fields fetched via NATS from ObjectStore.
// Used to enrich LLM prompts with actual entity titles and descriptions.
type EntityContent struct {
	Title    string // From ContentRole "title"
	Abstract string // From ContentRole "abstract" or description
}

// EntityParts represents parsed 6-part entity ID components.
// Entity IDs follow the canonical pattern: {org}.{platform}.{system}.{domain}.{type}.{instance}
type EntityParts struct {
	Full     string // Complete entity ID
	Org      string // Part 0: Organization
	Platform string // Part 1: Platform
	System   string // Part 2: Source (feed, repo, world, or framework component)
	Domain   string // Part 3: Delegated taxonomy
	Type     string // Part 4: Entity type
	Instance string // Part 5: Instance ID
	Title    string // Entity title from content store (optional)
	Abstract string // Entity abstract from content store (optional)
}

// DomainGroup groups entities by their domain (position 4 of the entity ID).
type DomainGroup struct {
	Domain      string       // e.g., "environmental", "content"
	Count       int          // Total entities in domain
	SystemTypes []SystemType // system.type breakdown
}

// SystemType represents a system.type combination count.
type SystemType struct {
	Name  string // e.g., "sensor.temperature"
	Count int
}

// PropertyInfo represents a property for prompts.
type PropertyInfo struct {
	Predicate string
	Value     string
}

// RelationshipInfo represents a relationship for prompts.
type RelationshipInfo struct {
	Predicate string
	Target    string
}

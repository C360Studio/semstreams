package oasfgenerator

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/c360studio/semstreams/message"
	agentic "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/c360studio/semstreams/vocabulary/oasf"
)

// Mapper converts SemStreams triples to OASF records.
// It maps agent.capability.*, agent.intent.*, and agent.action.* predicates
// to the OASF skills and domains format.
type Mapper struct {
	// defaultVersion is used when no version is specified.
	defaultVersion string

	// defaultAuthors is used when no authors are specified.
	defaultAuthors []string

	// includeExtensions enables SemStreams-specific extensions.
	includeExtensions bool
}

// NewMapper creates a new OASF mapper.
func NewMapper(defaultVersion string, defaultAuthors []string, includeExtensions bool) *Mapper {
	return &Mapper{
		defaultVersion:    defaultVersion,
		defaultAuthors:    defaultAuthors,
		includeExtensions: includeExtensions,
	}
}

// skillBuilder accumulates a single Skill's data during mapping. The
// wire-level OASFSkill.ID + .Name fields are only populated at
// finalizeRecord time, by resolving the source expression (or honoring
// an operator-set OASF class override) through vocabulary/oasf. Keeping
// the source expression on the builder is required because extension
// IDs are derived from it (see oasf.ExtensionID).
type skillBuilder struct {
	skill           *OASFSkill // accumulating wire shape; ID/Name set at finalize
	expression      string     // SemStreams internal expression (drives OASF resolution)
	displayName     string     // operator-supplied human label from CapabilityName
	overrideClassID uint32     // operator-set CapabilityOASFClass (0 = no override)
}

// mappingContext holds the state while mapping triples.
type mappingContext struct {
	record                  *OASFRecord
	skillsByExpression      map[string]*skillBuilder
	domainsByName           map[string]*OASFDomain
	permissionsByExpression map[string][]string
}

// MapTriplesToOASF converts a set of triples for an agent entity into an OASF record.
// The triples should all be about the same agent entity (same subject).
func (m *Mapper) MapTriplesToOASF(agentID string, triples []message.Triple) (*OASFRecord, error) {
	if len(triples) == 0 {
		return nil, fmt.Errorf("no triples provided")
	}

	// Initialize mapping context
	ctx := &mappingContext{
		record:                  NewOASFRecord(extractAgentName(agentID), m.defaultVersion, ""),
		skillsByExpression:      make(map[string]*skillBuilder),
		domainsByName:           make(map[string]*OASFDomain),
		permissionsByExpression: make(map[string][]string),
	}

	// First pass: create skills from expressions and names
	m.extractSkills(ctx, triples)

	// Second pass: apply additional properties
	m.applyTripleProperties(ctx, triples)

	// Finalize the record
	if err := m.finalizeRecord(ctx, agentID); err != nil {
		return nil, err
	}

	return ctx.record, nil
}

// extractSkills creates skill builders from capability expressions and
// names (first pass). The OASF wire-level ID and hierarchical Name are
// resolved later in finalizeRecord — at extraction time we just record
// the source expression and display name.
func (m *Mapper) extractSkills(ctx *mappingContext, triples []message.Triple) {
	for _, triple := range triples {
		switch triple.Predicate {
		case agentic.CapabilityExpression:
			expr := toString(triple.Object)
			tripleCtx := triple.Context
			if tripleCtx == "" {
				tripleCtx = expr
			}
			sb := m.getOrCreateSkill(ctx.skillsByExpression, tripleCtx)
			sb.expression = expr

		case agentic.CapabilityName:
			name := toString(triple.Object)
			tripleCtx := triple.Context
			if tripleCtx == "" {
				tripleCtx = name
			}
			sb := m.getOrCreateSkill(ctx.skillsByExpression, tripleCtx)
			sb.displayName = name
		}
	}
}

// applyTripleProperties applies additional properties from triples (second pass).
func (m *Mapper) applyTripleProperties(ctx *mappingContext, triples []message.Triple) {
	for _, triple := range triples {
		tripleCtx := triple.Context
		if tripleCtx == "" {
			// Use first skill if no context
			for key := range ctx.skillsByExpression {
				tripleCtx = key
				break
			}
		}

		switch triple.Predicate {
		case agentic.CapabilityDescription:
			desc := toString(triple.Object)
			sb := m.findSkillForContext(ctx.skillsByExpression, tripleCtx)
			if sb != nil {
				sb.skill.Description = desc
			}

		case agentic.CapabilityConfidence:
			conf := toFloat64(triple.Object)
			sb := m.findSkillForContext(ctx.skillsByExpression, tripleCtx)
			if sb != nil {
				sb.skill.Confidence = conf
			}

		case agentic.CapabilityOASFClass:
			// Operator override: pin the OASF class ID directly,
			// bypassing expression-based resolution. Zero is treated as
			// "no override" so an absent triple and a zero-valued triple
			// behave identically. A value that fails to coerce to int64
			// (typo'd string, unexpected type) is warn-logged and
			// dropped — keeping no-override semantics for the skill but
			// surfacing the misconfiguration in operator logs instead
			// of silently mapping to zero.
			raw, ok := toInt64(triple.Object)
			if !ok {
				slog.Warn("oasf-generator: CapabilityOASFClass override value did not coerce to int; treating as no-override",
					slog.String("predicate", agentic.CapabilityOASFClass),
					slog.String("subject", triple.Subject),
					slog.String("raw_type", fmt.Sprintf("%T", triple.Object)),
					slog.Any("raw_value", triple.Object))
				continue
			}
			override := uint32(raw)
			sb := m.findSkillForContext(ctx.skillsByExpression, tripleCtx)
			if sb != nil && override != 0 {
				sb.overrideClassID = override
			}

		case agentic.CapabilityPermission:
			perm := toString(triple.Object)
			ctx.permissionsByExpression[tripleCtx] = append(ctx.permissionsByExpression[tripleCtx], perm)

		case agentic.IntentGoal:
			goal := toString(triple.Object)
			if ctx.record.Description == "" {
				ctx.record.Description = goal
			}

		case agentic.IntentType:
			intentType := toString(triple.Object)
			m.getOrCreateDomain(ctx.domainsByName, intentType)

		case agentic.ActionType:
			if m.includeExtensions {
				actionType := toString(triple.Object)
				ctx.record.SetExtension("action_types", appendUnique(
					toStringSlice(ctx.record.Extensions["action_types"]),
					actionType,
				))
			}
		}
	}
}

// finalizeRecord applies final transformations to the record, including
// the OASF taxonomy resolution that turns each skillBuilder's source
// expression into a canonical class ID + hierarchical name pair (or an
// extension ID + semstreams/-prefixed name when the expression has no
// canonical match). Returns an error if any skill's OASF identity
// resolution is structurally invalid (see resolveSkillIdentity).
func (m *Mapper) finalizeRecord(ctx *mappingContext, agentID string) error {
	// Apply permissions to skills
	for expr, perms := range ctx.permissionsByExpression {
		if sb, ok := ctx.skillsByExpression[expr]; ok {
			sb.skill.Permissions = perms
		}
	}

	// Resolve OASF identity and emit each skill.
	for _, sb := range ctx.skillsByExpression {
		if err := resolveSkillIdentity(sb); err != nil {
			return err
		}
		// Preserve the operator's display label if no description is set
		// — OASF has no instance-level display-name field, so the
		// human-readable name lives in description rather than being
		// dropped on the floor.
		if sb.skill.Description == "" && sb.displayName != "" {
			sb.skill.Description = sb.displayName
		}
		ctx.record.AddSkill(*sb.skill)
	}

	// Convert domain map to slice
	for _, domain := range ctx.domainsByName {
		ctx.record.AddDomain(*domain)
	}

	// Add default authors if none specified
	if len(ctx.record.Authors) == 0 {
		ctx.record.Authors = m.defaultAuthors
	}

	// Add SemStreams extensions if enabled
	if m.includeExtensions {
		ctx.record.SetExtension("semstreams_entity_id", agentID)
		ctx.record.SetExtension("source", "semstreams")
	}
	return nil
}

// getOrCreateSkill gets or creates a skillBuilder by context key. The
// builder seeds the source expression with the context key — if a later
// CapabilityExpression triple supplies a different expression for the
// same context, that overwrites the seed.
func (m *Mapper) getOrCreateSkill(skills map[string]*skillBuilder, key string) *skillBuilder {
	if sb, ok := skills[key]; ok {
		return sb
	}
	sb := &skillBuilder{
		skill: &OASFSkill{
			Confidence: 1.0, // Default confidence
		},
		expression: key, // seed; CapabilityExpression triple overwrites
	}
	skills[key] = sb
	return sb
}

// findSkillForContext finds a skillBuilder matching the triple context.
// If no context or no match, returns the first builder or nil.
func (m *Mapper) findSkillForContext(skills map[string]*skillBuilder, context string) *skillBuilder {
	if context != "" {
		if sb, ok := skills[context]; ok {
			return sb
		}
	}
	// Return first builder if any
	for _, sb := range skills {
		return sb
	}
	return nil
}

// resolveSkillIdentity populates the OASF wire-level ID and Name on the
// builder's skill. The resolution precedence is:
//
//  1. Operator override (CapabilityOASFClass triple) — wins outright,
//     used verbatim. The hierarchical name comes from the canonical
//     taxonomy if oasf.Name returns one for the override; for overrides
//     in the extension range, the name is derived from the source
//     expression via ExtensionName.
//  2. Canonical class via oasf.LookupID(expression).
//  3. Extension class via oasf.ExtensionID(expression), with a
//     semstreams/-prefixed hierarchical name.
//
// Returns an error when the operator override is structurally invalid —
// either pointing at a non-covered canonical class (no constant in
// vocabulary/oasf and not in the extension range) or in the extension
// range without a CapabilityExpression to derive a name from. Failing
// at the generator boundary keeps misuse visible at construction time
// instead of as an opaque wire-format rejection downstream.
func resolveSkillIdentity(sb *skillBuilder) error {
	if sb.overrideClassID != 0 {
		sb.skill.ID = sb.overrideClassID
		// Canonical-by-coverage: name from the published taxonomy.
		if name := oasf.Name(sb.overrideClassID); name != "" {
			sb.skill.Name = name
			return nil
		}
		// Non-canonical, non-extension: operator pinned a hypothetical
		// class ID with no vocabulary/oasf constant. Surface loudly.
		if !oasf.IsExtension(sb.overrideClassID) {
			return fmt.Errorf("CapabilityOASFClass override = %d has no covered canonical name and is not in the extension range; "+
				"either add a constant in vocabulary/oasf or drop the override",
				sb.overrideClassID)
		}
		// Extension-range override: hierarchical name comes from the
		// source expression. Without an expression we'd emit an empty
		// Name (Validate catches it, but the error site is here).
		if sb.expression == "" {
			return fmt.Errorf("CapabilityOASFClass override = %d in extension range requires a CapabilityExpression triple "+
				"to derive the hierarchical name",
				sb.overrideClassID)
		}
		sb.skill.Name = oasf.ExtensionName(sb.expression)
		return nil
	}

	if id, ok := oasf.LookupID(sb.expression); ok {
		sb.skill.ID = id
		sb.skill.Name = oasf.Name(id)
		return nil
	}

	sb.skill.ID = oasf.ExtensionID(sb.expression)
	sb.skill.Name = oasf.ExtensionName(sb.expression)
	return nil
}

// getOrCreateDomain gets or creates a domain by name.
func (m *Mapper) getOrCreateDomain(domains map[string]*OASFDomain, name string) *OASFDomain {
	if domain, ok := domains[name]; ok {
		return domain
	}
	domain := &OASFDomain{
		Name: name,
	}
	domains[name] = domain
	return domain
}

// extractAgentName extracts a human-readable name from an entity ID.
// Entity ID format: org.platform.domain.system.type.instance
func extractAgentName(entityID string) string {
	parts := strings.Split(entityID, ".")
	if len(parts) >= 6 {
		// Use type.instance as name (e.g., "agent.architect")
		return parts[4] + "-" + parts[5]
	}
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "-" + parts[len(parts)-1]
	}
	return entityID
}

// toInt64 converts any value to an int64 and reports whether the
// conversion succeeded. Used for CapabilityOASFClass where the triple
// Object may arrive as float64 (JSON numbers), int, int32, int64, uint,
// uint32, uint64, or a numeric string. Callers warn-log on `ok == false`
// so operator typos and shape regressions surface in operations rather
// than landing as silent zero values (zero is the documented
// "no-override" sentinel and must be distinguishable from parse failure).
func toInt64(v any) (int64, bool) {
	switch val := v.(type) {
	case int:
		return int64(val), true
	case int32:
		return int64(val), true
	case int64:
		return val, true
	case uint:
		return int64(val), true
	case uint32:
		return int64(val), true
	case uint64:
		return int64(val), true
	case float32:
		return int64(val), true
	case float64:
		return int64(val), true
	case string:
		i, err := strconv.ParseInt(val, 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}

// toString converts any value to a string.
func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// toFloat64 converts any value to a float64.
func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}

// toStringSlice converts any value to a string slice.
func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, len(val))
		for i, item := range val {
			result[i] = toString(item)
		}
		return result
	default:
		return nil
	}
}

// appendUnique appends a value to a slice if not already present.
func appendUnique(slice []string, value string) []string {
	for _, v := range slice {
		if v == value {
			return slice
		}
	}
	return append(slice, value)
}

// PredicateMapping defines how SemStreams predicates map to OASF fields.
// This table is for documentation and validation purposes.
//
// CapabilityExpression drives both skills[].id and skills[].name via
// the vocabulary/oasf resolver — canonical taxonomy lookup with
// extension fallback. CapabilityName is preserved on skills[].description
// when no explicit description is set (OASF has no instance-level
// display-name field). CapabilityOASFClass is an operator override
// that pins skills[].id to a specific OASF class, bypassing
// CapabilityExpression resolution.
var PredicateMapping = map[string]string{
	// Capability predicates -> Skills
	agentic.CapabilityName:        "skills[].description (fallback)",
	agentic.CapabilityDescription: "skills[].description",
	agentic.CapabilityExpression:  "skills[].{id, name} (via vocabulary/oasf)",
	agentic.CapabilityConfidence:  "skills[].confidence",
	agentic.CapabilityPermission:  "skills[].permissions[]",
	agentic.CapabilityOASFClass:   "skills[].id (operator override)",

	// Intent predicates -> Description and Domains
	agentic.IntentGoal: "description",
	agentic.IntentType: "domains[].name",

	// Action predicates -> Extensions
	agentic.ActionType: "extensions.action_types[]",
}

// SupportedPredicates returns the list of predicates this mapper handles.
func SupportedPredicates() []string {
	predicates := make([]string, 0, len(PredicateMapping))
	for pred := range PredicateMapping {
		predicates = append(predicates, pred)
	}
	return predicates
}

package lifecycle

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/vocabulary"
)

// fieldMeta captures the parsed lifecycle-tag attributes for one
// struct field. The package uses it internally for register-time
// validation and runtime projection + operator-patch enforcement;
// apps don't construct it directly.
type fieldMeta struct {
	// FieldName is the Go struct field name (e.g. "OwnerOrgID").
	FieldName string

	// FieldIndex is the reflect index path to the field (suitable
	// for reflect.Value.FieldByIndex). Cached at parse time so
	// runtime callers (projection, Match comparisons) avoid the
	// FieldByName linear-walk cost per call. The slice form
	// supports embedded-struct paths if the harness ever needs to
	// reach into embedded types; today every Participant field is
	// at top level so the slice always has length 1.
	FieldIndex []int

	// JSONName is the JSON wire-field name (per the field's `json:`
	// tag, falling back to the lowercased Go field name). Used by
	// operator-patch routing because operator patches arrive as JSON
	// keys.
	JSONName string

	// Predicate is the triple predicate that carries this field's
	// value in ENTITY_STATES. Declared via `lifecycle:"...,predicate=foo.bar"`.
	// Empty for ID fields (identity lives in the bucket key, not as
	// a triple) and for fields with no projection mapping.
	Predicate string

	// IsID is true when the field is tagged `lifecycle:"id"`.
	IsID bool

	// IsPhase is true when the field is tagged `lifecycle:"phase"`.
	IsPhase bool

	// IsReference is true when the field is tagged
	// `lifecycle:"reference,predicate=foo.bar"` — the field projects
	// the Object of the named predicate as a scalar entity ID
	// rather than as the field's literal value.
	IsReference bool

	// ReadOnly is true when the field has the `readonly` modifier.
	// Implicitly true for IsID, IsPhase, and IsReference fields
	// (Manager owns transitions, identity, and reference targets).
	// Operator patches against ReadOnly fields are rejected with
	// ErrFieldNotOperatorWritable.
	ReadOnly bool

	// OperatorWritable is true when the field is tagged
	// `lifecycle:"operator_writable"`. Default-deny — fields without
	// this flag are NOT operator-writable.
	OperatorWritable bool

	// Indexable is true when the field is tagged `lifecycle:"indexable"`.
	// v1 ignores this flag; future secondary-index work consumes it
	// when scaling pressure surfaces.
	Indexable bool
}

// structMeta is the parsed lifecycle-tag picture for one registered
// Participant type. Computed once at Register time from the Workflow's
// Schema via reflection; consulted on every projection / UpdateFromOperator
// call.
type structMeta struct {
	// GoType is the reflect.Type registered via Workflow.Schema. Used
	// to allocate fresh instances via reflect.New on every Get.
	GoType reflect.Type

	// IDField is the field tagged `lifecycle:"id"`. Exactly one
	// per struct (validated at parse time).
	IDField *fieldMeta

	// PhaseField is the field tagged `lifecycle:"phase"`. Exactly
	// one per struct.
	PhaseField *fieldMeta

	// FieldsByJSONName is the parsed metadata for every field on
	// the struct, keyed by JSON-wire-field name (NOT Go field name).
	// Operator patches arrive as JSON, so this is the natural lookup.
	FieldsByJSONName map[string]*fieldMeta

	// FieldsByPredicate maps the declared predicate name to its
	// field metadata. Used by the projection layer to populate
	// struct fields from triples on every Get. Fields without
	// `predicate=` declared (e.g. ID fields, untagged metadata)
	// don't appear here.
	FieldsByPredicate map[string]*fieldMeta
}

// OperatorWritableJSONNames returns the JSON field names that are
// tagged operator_writable, in sorted order. Exposed to
// WorkflowDef.OperatorWritableFields so the operator API can advertise
// the patchable surface.
func (sm *structMeta) OperatorWritableJSONNames() []string {
	names := make([]string, 0, len(sm.FieldsByJSONName))
	for jsonName, meta := range sm.FieldsByJSONName {
		if meta.OperatorWritable {
			names = append(names, jsonName)
		}
	}
	// Sort for stable WorkflowDef output (operator dashboards
	// rendering field lists shouldn't see flap on every restart).
	sort.Strings(names)
	return names
}

// OperatorWritablePredicates returns the predicates that the operator
// API may patch via UpdateFromOperator. Sorted for deterministic
// output. Predicate-keyed lookup is what the operator-patch enforcement
// path uses today (post ADR-049 — patches map to triples, not blob
// fields).
func (sm *structMeta) OperatorWritablePredicates() []string {
	preds := make([]string, 0, len(sm.FieldsByPredicate))
	for predicate, meta := range sm.FieldsByPredicate {
		if meta.OperatorWritable {
			preds = append(preds, predicate)
		}
	}
	sort.Strings(preds)
	return preds
}

// parseSchemaType reflects over the given Workflow.Schema type and
// returns the parsed lifecycle-tag metadata. Returns ErrMissingIDField
// / ErrMissingPhaseField when those tags are absent.
//
// The reflect.Type passed in must be a struct (the projection layer
// allocates instances via reflect.New(GoType), which then yields a
// pointer the unmarshal/SetField paths can use).
func parseSchemaType(t reflect.Type) (*structMeta, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("lifecycle: Workflow.Schema must be a struct type, got %v", t.Kind())
	}

	sm := &structMeta{
		GoType:            t,
		FieldsByJSONName:  make(map[string]*fieldMeta, t.NumField()),
		FieldsByPredicate: make(map[string]*fieldMeta, t.NumField()),
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		if err := absorbField(sm, t, field, i); err != nil {
			return nil, err
		}
	}

	if sm.IDField == nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingIDField, t.Name())
	}
	if sm.PhaseField == nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingPhaseField, t.Name())
	}
	if sm.PhaseField.Predicate == "" {
		return nil, fmt.Errorf("lifecycle: phase field %s on struct %s must declare a predicate (lifecycle:\"phase,predicate=foo.phase\")",
			sm.PhaseField.FieldName, t.Name())
	}
	return sm, nil
}

// absorbField parses one struct field's lifecycle + json tags into
// fieldMeta and registers it on sm. Pulled out of parseSchemaType to
// keep that function under revive's function-length budget.
func absorbField(sm *structMeta, t reflect.Type, field reflect.StructField, idx int) error {
	jsonName := jsonNameForField(field)
	meta := &fieldMeta{
		FieldName:  field.Name,
		FieldIndex: []int{idx},
		JSONName:   jsonName,
	}
	if err := parseLifecycleTag(meta, field); err != nil {
		return err
	}
	if meta.Predicate != "" {
		if err := vocabulary.RequireDeclaredPredicate(meta.Predicate); err != nil {
			return fmt.Errorf("lifecycle: field %s declares undeclared or invalid predicate: %w", field.Name, err)
		}
	}
	if err := validateRoleFlags(meta, field); err != nil {
		return err
	}
	if meta.IsID {
		if sm.IDField != nil {
			return fmt.Errorf("lifecycle: multiple fields tagged lifecycle:\"id\" on struct %s (only one allowed)",
				t.Name())
		}
		sm.IDField = meta
	}
	if meta.IsPhase {
		if sm.PhaseField != nil {
			return fmt.Errorf("lifecycle: multiple fields tagged lifecycle:\"phase\" on struct %s (only one allowed)",
				t.Name())
		}
		sm.PhaseField = meta
	}
	if err := validateOperatorWritableShape(meta, field); err != nil {
		return err
	}
	return indexField(sm, t, meta)
}

// parseLifecycleTag walks the `lifecycle:"..."` tag value and sets
// flags / predicate on meta. Unknown values surface as errors so
// typos fail at startup.
func parseLifecycleTag(meta *fieldMeta, field reflect.StructField) error {
	tag, hasTag := field.Tag.Lookup("lifecycle")
	if !hasTag {
		return nil
	}
	for part := range strings.SplitSeq(tag, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if value, ok := strings.CutPrefix(p, "predicate="); ok {
			meta.Predicate = strings.TrimSpace(value)
			continue
		}
		switch p {
		case "id":
			meta.IsID = true
		case "phase":
			meta.IsPhase = true
		case "readonly":
			meta.ReadOnly = true
		case "operator_writable":
			meta.OperatorWritable = true
		case "indexable":
			meta.Indexable = true
		case "reference":
			meta.IsReference = true
		default:
			return fmt.Errorf("lifecycle: field %s has unknown lifecycle tag value %q",
				field.Name, p)
		}
	}
	return nil
}

// validateRoleFlags enforces the cross-flag contradictions caught
// at startup: id/phase + operator_writable, reference + operator_writable.
// Also marks IsID/IsPhase/IsReference fields as implicitly readonly.
func validateRoleFlags(meta *fieldMeta, field reflect.StructField) error {
	if (meta.IsID || meta.IsPhase) && meta.OperatorWritable {
		roleTag := "phase"
		if meta.IsID {
			roleTag = "id"
		}
		return fmt.Errorf("lifecycle: field %s cannot be both lifecycle:%q and lifecycle:%q",
			field.Name, roleTag, "operator_writable")
	}
	if meta.IsReference && meta.OperatorWritable {
		return fmt.Errorf("lifecycle: field %s cannot be both lifecycle:\"reference\" and lifecycle:\"operator_writable\" (reference targets are owned by upstream emitters, not operator-patchable)",
			field.Name)
	}
	if meta.IsID || meta.IsPhase || meta.IsReference {
		meta.ReadOnly = true
	}
	return nil
}

// validateOperatorWritableShape catches wire-vs-tag contradictions
// for operator_writable + reference fields: json:"-" tag means the
// wire can't reach it; missing predicate means projection can't
// route the write.
func validateOperatorWritableShape(meta *fieldMeta, field reflect.StructField) error {
	if meta.OperatorWritable {
		if jsonTag, ok := field.Tag.Lookup("json"); ok {
			if name, _, _ := strings.Cut(jsonTag, ","); name == "-" {
				return fmt.Errorf("lifecycle: field %s tagged operator_writable but json:%q means the wire can't reach it",
					field.Name, "-")
			}
		}
	}
	if meta.OperatorWritable && meta.Predicate == "" {
		return fmt.Errorf("lifecycle: field %s tagged operator_writable must declare a predicate (lifecycle:\"operator_writable,predicate=foo.bar\") — patches need a triple to write to",
			field.Name)
	}
	if meta.IsReference && meta.Predicate == "" {
		return fmt.Errorf("lifecycle: field %s tagged reference must declare a predicate",
			field.Name)
	}
	return nil
}

// indexField inserts meta into both lookup maps and surfaces collisions
// (json-name or predicate aliasing) as startup errors.
func indexField(sm *structMeta, t reflect.Type, meta *fieldMeta) error {
	if existing, dup := sm.FieldsByJSONName[meta.JSONName]; dup {
		return fmt.Errorf("lifecycle: fields %s and %s on struct %s both resolve to JSON name %q — rename one or set distinct json: tags",
			existing.FieldName, meta.FieldName, t.Name(), meta.JSONName)
	}
	sm.FieldsByJSONName[meta.JSONName] = meta
	if meta.Predicate != "" {
		if existing, dup := sm.FieldsByPredicate[meta.Predicate]; dup {
			return fmt.Errorf("lifecycle: fields %s and %s on struct %s both map to predicate %q",
				existing.FieldName, meta.FieldName, t.Name(), meta.Predicate)
		}
		sm.FieldsByPredicate[meta.Predicate] = meta
	}
	return nil
}

// jsonNameForField returns the JSON wire-field name for a struct
// field — the `json:` tag's first comma-separated segment, or the
// lowercased Go field name when no `json:` tag is present (matching
// encoding/json's default).
func jsonNameForField(field reflect.StructField) string {
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return strings.ToLower(field.Name)
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		return strings.ToLower(field.Name)
	}
	return name
}

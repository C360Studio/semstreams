package lifecycle

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// fieldMeta captures the parsed lifecycle-tag attributes for one
// struct field. The package uses it internally for register-time
// validation and runtime operator-patch enforcement; apps don't
// construct it directly.
type fieldMeta struct {
	// FieldName is the Go struct field name (e.g. "OwnerOrgID").
	FieldName string

	// FieldIndex is the reflect index path to the field (suitable
	// for reflect.Value.FieldByIndex). Cached at parse time so
	// runtime callers (setPhaseField, Match comparisons) avoid the
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

	// IsID is true when the field is tagged `lifecycle:"id"`.
	IsID bool

	// IsPhase is true when the field is tagged `lifecycle:"phase"`.
	IsPhase bool

	// ReadOnly is true when the field has the `readonly` modifier.
	// Implicitly true for IsID and IsPhase fields (Manager owns
	// transitions and identity). Operator patches against ReadOnly
	// fields are rejected with ErrFieldNotOperatorWritable.
	ReadOnly bool

	// OperatorWritable is true when the field is tagged
	// `lifecycle:"operator_writable"`. Default-deny — fields without
	// this flag are NOT operator-writable.
	OperatorWritable bool

	// Indexable is true when the field is tagged `lifecycle:"indexable"`.
	// v1 ignores this flag; v2 secondary-index work consumes it
	// when scaling pressure surfaces.
	Indexable bool
}

// structMeta is the parsed lifecycle-tag picture for one registered
// Participant type. Computed once at Register time from a sample
// instance via reflection; consulted on every UpdateFromOperator call
// and on every List operation that uses Match.
type structMeta struct {
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

// parseStructTags reflects over the given struct value and returns
// the parsed lifecycle-tag metadata. Returns ErrMissingIDField /
// ErrMissingPhaseField when those tags are absent.
//
// The reflect.Value passed in must be a struct or pointer to struct.
// Pointers are dereferenced once; nested pointers and interfaces are
// not recursed (Participant implementations are expected to be plain
// data structs, not deeply-nested object graphs — see
// participant.go's interface contract).
func parseStructTags(v reflect.Value) (*structMeta, error) {
	// Dereference one level of pointer if present. Factory functions
	// typically return pointers (e.g. `func() Participant { return
	// &MissionState{} }`), so this is the common case.
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("lifecycle: parseStructTags requires struct or *struct, got %v", v.Kind())
	}

	t := v.Type()
	sm := &structMeta{
		FieldsByJSONName: make(map[string]*fieldMeta, t.NumField()),
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields — Manager.UpdateFromOperator could
		// not patch them anyway (operator patches arrive as JSON
		// keys; encoding/json ignores unexported fields). Including
		// them in FieldsByJSONName would let Match accidentally rely
		// on something the wire can't reach.
		if !field.IsExported() {
			continue
		}

		jsonName := jsonNameForField(field)

		meta := &fieldMeta{
			FieldName:  field.Name,
			FieldIndex: []int{i},
			JSONName:   jsonName,
		}

		tag, hasTag := field.Tag.Lookup("lifecycle")
		if hasTag {
			for part := range strings.SplitSeq(tag, ",") {
				switch strings.TrimSpace(part) {
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
				case "":
					// trailing comma or empty tag — tolerate quietly
				default:
					return nil, fmt.Errorf("lifecycle: field %s has unknown lifecycle tag value %q",
						field.Name, part)
				}
			}
		}

		// IDs and Phases are implicitly readonly (Manager owns
		// them — apps don't operator-patch identity or transitions).
		// Operator-writable + (id|phase) is a contradiction; reject
		// at parse time so the wire-up bug surfaces at startup.
		if (meta.IsID || meta.IsPhase) && meta.OperatorWritable {
			roleTag := "phase"
			if meta.IsID {
				roleTag = "id"
			}
			return nil, fmt.Errorf("lifecycle: field %s cannot be both lifecycle:%q and lifecycle:%q",
				field.Name, roleTag, "operator_writable")
		}
		if meta.IsID || meta.IsPhase {
			meta.ReadOnly = true
		}

		if meta.IsID {
			if sm.IDField != nil {
				return nil, fmt.Errorf("lifecycle: multiple fields tagged lifecycle:\"id\" on struct %s (only one allowed)",
					t.Name())
			}
			sm.IDField = meta
		}
		if meta.IsPhase {
			if sm.PhaseField != nil {
				return nil, fmt.Errorf("lifecycle: multiple fields tagged lifecycle:\"phase\" on struct %s (only one allowed)",
					t.Name())
			}
			sm.PhaseField = meta
		}

		// json:"-" + lifecycle:"operator_writable" is a wire/harness
		// contradiction: the operator-patch JSON can't reach a field
		// json-tagged "-", so claiming it's operator-writable is
		// guaranteed-broken at runtime. The internal Match path can
		// still see the field (it reflects directly, not through
		// json.Unmarshal), but operator-patch routing is the
		// load-bearing reason for the tag — reject at startup.
		if meta.OperatorWritable {
			if jsonTag, ok := field.Tag.Lookup("json"); ok {
				if name, _, _ := strings.Cut(jsonTag, ","); name == "-" {
					return nil, fmt.Errorf("lifecycle: field %s tagged operator_writable but json:%q means the wire can't reach it",
						field.Name, "-")
				}
			}
		}

		// Field-name collision (e.g. Foo + FOO both falling back to
		// "foo", or a tagged json name colliding with another field's
		// fallback) silently aliases two fields to the same operator-
		// patch routing target. Reject at parse time so the wiring bug
		// surfaces at app startup, not at first patch in prod.
		if existing, dup := sm.FieldsByJSONName[jsonName]; dup {
			return nil, fmt.Errorf("lifecycle: fields %s and %s on struct %s both resolve to JSON name %q — rename one or set distinct json: tags",
				existing.FieldName, field.Name, t.Name(), jsonName)
		}
		sm.FieldsByJSONName[jsonName] = meta
	}

	if sm.IDField == nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingIDField, t.Name())
	}
	if sm.PhaseField == nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingPhaseField, t.Name())
	}
	return sm, nil
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
		// `json:",omitempty"` (empty name) → fall back to Go name.
		// `json:"-"` (explicit skip) → use the Go name internally
		// so reflection can still find it for Match purposes; the
		// field won't appear in operator-patch JSON anyway because
		// encoding/json refuses to marshal/unmarshal it.
		return strings.ToLower(field.Name)
	}
	return name
}

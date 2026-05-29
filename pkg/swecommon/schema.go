package swecommon

import (
	"encoding/json"
	"fmt"
)

// Field is a named member of a [DataRecord]. The name is the
// dictionary key in the record's value map (and the column header
// in CSV / position index in binary). Component carries the typed
// schema for the field's values.
type Field struct {
	// Name is the field's identifier within its parent record. Must
	// be unique across the record's fields, non-empty, and (for
	// CSV/text encoding) free of the configured token separator.
	Name string

	// Component is the typed schema for this field's values.
	Component DataComponent
}

// DataRecord is the schema for a heterogeneous record of named
// typed fields (SWE Common §7.6.8). It is the framework's
// observation-result and command-payload model.
type DataRecord struct {
	CommonFields

	// Fields are the record's typed members, in declaration order.
	// Order is wire-significant for CSV and binary encodings.
	Fields []Field
}

// Kind reports [KindRecord].
func (*DataRecord) Kind() ComponentKind { return KindRecord }
func (*DataRecord) sealed()             {}

// FieldByName returns the field with the given name and reports
// whether it was found. O(n) — DataRecords are typically small (a
// handful of fields).
func (r *DataRecord) FieldByName(name string) (Field, bool) {
	if r == nil {
		return Field{}, false
	}
	for _, f := range r.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Validate enforces the structural invariants the encoders rely on:
// non-empty record, non-empty unique field names, non-nil scalar
// component per field. Phase 1 rejects nested DataRecord fields at
// schema-validate time so consumers do not learn about the
// unsupported shape per-row at first encode.
func (r *DataRecord) Validate() error {
	if r == nil {
		return fmt.Errorf("swecommon: DataRecord is nil")
	}
	if len(r.Fields) == 0 {
		return fmt.Errorf("swecommon: DataRecord has no fields")
	}
	seen := make(map[string]struct{}, len(r.Fields))
	for i, f := range r.Fields {
		if f.Name == "" {
			return fmt.Errorf("swecommon: field %d has empty name", i)
		}
		if _, dup := seen[f.Name]; dup {
			return fmt.Errorf("swecommon: duplicate field name %q", f.Name)
		}
		seen[f.Name] = struct{}{}
		if f.Component == nil {
			return fmt.Errorf("swecommon: field %q has nil component", f.Name)
		}
		if _, nested := f.Component.(*DataRecord); nested {
			return fmt.Errorf("swecommon: field %q: nested DataRecord not supported in Phase 1", f.Name)
		}
	}
	return nil
}

// schemaEnvelope is the wire shape of a serialized [DataRecord]
// schema in SWE Common JSON Encoding (22-022). Composite types
// embed a Fields slice; scalar types do not.
type schemaEnvelope struct {
	Type ComponentKind `json:"type"`

	Label      string `json:"label,omitempty"`
	Definition string `json:"definition,omitempty"`
	NilValue   string `json:"nilValue,omitempty"`

	// Quantity / Time fields
	UoMCode        string `json:"uomCode,omitempty"`
	UoMHref        string `json:"uomHref,omitempty"`
	ReferenceFrame string `json:"referenceFrame,omitempty"`

	// Category fields
	CodeSpace string `json:"codeSpace,omitempty"`

	// DataRecord fields
	Fields []fieldEnvelope `json:"fields,omitempty"`
}

// fieldEnvelope is the JSON shape of a DataRecord field — name plus
// the nested schema envelope of the field's component.
type fieldEnvelope struct {
	Name      string         `json:"name"`
	Component schemaEnvelope `json:"-"`
}

// MarshalJSON splats the nested component shape onto the field
// envelope (per OGC 22-022) so a Quantity field reads as
// `{"name":"temperature","type":"Quantity","uomCode":"Cel"}`
// instead of nesting the component under a sub-key.
func (f fieldEnvelope) MarshalJSON() ([]byte, error) {
	type fieldAlias struct {
		Name string `json:"name"`
		schemaEnvelope
	}
	return json.Marshal(fieldAlias{Name: f.Name, schemaEnvelope: f.Component})
}

// UnmarshalJSON is the inverse: read the splatted shape into a
// fieldEnvelope.
func (f *fieldEnvelope) UnmarshalJSON(data []byte) error {
	type fieldAlias struct {
		Name string `json:"name"`
		schemaEnvelope
	}
	var a fieldAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	f.Name = a.Name
	f.Component = a.schemaEnvelope
	return nil
}

// envelopeFromComponent builds the wire envelope for a single
// component. Used by both schema-marshal and the field encoder.
func envelopeFromComponent(c DataComponent) (schemaEnvelope, error) {
	if c == nil {
		return schemaEnvelope{}, fmt.Errorf("swecommon: nil component")
	}
	env := schemaEnvelope{
		Type:       c.Kind(),
		Label:      c.Label(),
		Definition: c.Definition(),
	}
	switch v := c.(type) {
	case Quantity:
		env.NilValue = v.Nil()
		env.UoMCode = v.UoMCode
		env.UoMHref = v.UoMHref
	case Count:
		env.NilValue = v.Nil()
	case Time:
		env.NilValue = v.Nil()
		env.UoMHref = v.UoMHref
		env.ReferenceFrame = v.ReferenceFrame
	case Boolean:
		env.NilValue = v.Nil()
	case Text:
		env.NilValue = v.Nil()
	case Category:
		env.NilValue = v.Nil()
		env.CodeSpace = v.CodeSpace
	case *DataRecord:
		if v == nil {
			return schemaEnvelope{}, fmt.Errorf("swecommon: nil *DataRecord")
		}
		env.NilValue = v.Nil()
		env.Fields = make([]fieldEnvelope, 0, len(v.Fields))
		for _, f := range v.Fields {
			sub, err := envelopeFromComponent(f.Component)
			if err != nil {
				return schemaEnvelope{}, fmt.Errorf("field %q: %w", f.Name, err)
			}
			env.Fields = append(env.Fields, fieldEnvelope{Name: f.Name, Component: sub})
		}
	default:
		return schemaEnvelope{}, fmt.Errorf("swecommon: unknown component kind %q", c.Kind())
	}
	return env, nil
}

// componentFromEnvelope is the inverse — reconstruct a typed
// [DataComponent] from a wire envelope. Used by schema unmarshal
// and the field decoder.
func componentFromEnvelope(env schemaEnvelope) (DataComponent, error) {
	base := CommonFields{
		LabelValue:      env.Label,
		DefinitionValue: env.Definition,
		NilValue:        env.NilValue,
	}
	switch env.Type {
	case KindQuantity:
		return Quantity{CommonFields: base, UoMCode: env.UoMCode, UoMHref: env.UoMHref}, nil
	case KindCount:
		return Count{CommonFields: base}, nil
	case KindTime:
		return Time{CommonFields: base, UoMHref: env.UoMHref, ReferenceFrame: env.ReferenceFrame}, nil
	case KindBoolean:
		return Boolean{CommonFields: base}, nil
	case KindText:
		return Text{CommonFields: base}, nil
	case KindCategory:
		return Category{CommonFields: base, CodeSpace: env.CodeSpace}, nil
	case KindRecord:
		rec := &DataRecord{CommonFields: base, Fields: make([]Field, 0, len(env.Fields))}
		for _, f := range env.Fields {
			sub, err := componentFromEnvelope(f.Component)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", f.Name, err)
			}
			rec.Fields = append(rec.Fields, Field{Name: f.Name, Component: sub})
		}
		return rec, nil
	default:
		return nil, fmt.Errorf("swecommon: unknown component type %q", env.Type)
	}
}

// MarshalSchema serializes a [DataRecord] schema to OGC SWE Common
// JSON Encoding (22-022). Round-trips through [UnmarshalSchema].
//
// This is the metadata document — a datastream advertises its
// observation-result schema by serving this on a `/schema` endpoint
// (or embedding it in the datastream resource). The schema then
// drives the JSON / CSV / binary encoders for the values.
func MarshalSchema(r *DataRecord) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("swecommon: marshal schema: %w", err)
	}
	env, err := envelopeFromComponent(r)
	if err != nil {
		return nil, fmt.Errorf("swecommon: marshal schema: %w", err)
	}
	return json.Marshal(env)
}

// UnmarshalSchema parses a SWE Common JSON Encoding schema document
// into a typed [DataRecord]. The result is structurally validated
// (non-empty record, unique field names).
func UnmarshalSchema(data []byte) (*DataRecord, error) {
	var env schemaEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("swecommon: unmarshal schema: %w", err)
	}
	if env.Type != KindRecord {
		return nil, fmt.Errorf("swecommon: expected DataRecord at root, got %q", env.Type)
	}
	c, err := componentFromEnvelope(env)
	if err != nil {
		return nil, fmt.Errorf("swecommon: unmarshal schema: %w", err)
	}
	rec, ok := c.(*DataRecord)
	if !ok {
		return nil, fmt.Errorf("swecommon: root component is not *DataRecord")
	}
	if err := rec.Validate(); err != nil {
		return nil, fmt.Errorf("swecommon: unmarshal schema: %w", err)
	}
	return rec, nil
}

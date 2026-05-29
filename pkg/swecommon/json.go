package swecommon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// EncodeJSON writes the rows as a SWE Common JSON Encoding (22-022)
// document bound to the given schema. The output shape is a JSON
// array of objects, one per row, with field names matching the
// schema's field declarations.
//
// Per-field value encoding:
//
//   - Quantity → JSON number (float64)
//   - Count    → JSON number (int64)
//   - Time     → JSON string (RFC 3339)
//   - Boolean  → JSON boolean
//   - Text     → JSON string
//   - Category → JSON string
//
// nilValue rule: when a value is Go nil OR exactly equal to the
// schema's nilValue for the field, the output is JSON null.
// Decoders honor the same rule.
func EncodeJSON(w io.Writer, schema *DataRecord, rows []Values) error {
	if err := schema.Validate(); err != nil {
		return fmt.Errorf("swecommon: encode JSON: %w", err)
	}
	enc := json.NewEncoder(w)
	out := make([]map[string]any, 0, len(rows))
	for i, row := range rows {
		obj, err := jsonRow(schema, row)
		if err != nil {
			return fmt.Errorf("swecommon: encode JSON row %d: %w", i, err)
		}
		out = append(out, obj)
	}
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("swecommon: encode JSON: %w", err)
	}
	return nil
}

// MarshalJSONRows is the bytes-returning convenience wrapper around
// [EncodeJSON]. The result has no trailing newline.
func MarshalJSONRows(schema *DataRecord, rows []Values) ([]byte, error) {
	var buf bytes.Buffer
	if err := EncodeJSON(&buf, schema, rows); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func jsonRow(schema *DataRecord, row Values) (map[string]any, error) {
	out := make(map[string]any, len(schema.Fields))
	for _, f := range schema.Fields {
		v := fieldValue(row, f.Name)
		if matchesNilToken(f.Component, v) {
			v = nil
		}
		canon, isNil, err := normalizeValue(f.Component, v)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		if isNil {
			out[f.Name] = nil
			continue
		}
		out[f.Name] = canon
	}
	return out, nil
}

// DecodeJSON reads a SWE Common JSON Encoding document into typed
// row values bound to the schema. Unknown fields in the JSON input
// are ignored — operators evolving the schema can add fields
// without breaking old payloads. Missing fields decode to absent
// keys (NOT to nil values, so callers can distinguish "the
// producer omitted this" from "the producer asserted null").
func DecodeJSON(r io.Reader, schema *DataRecord) ([]Values, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("swecommon: decode JSON: %w", err)
	}
	var raw []map[string]json.RawMessage
	dec := json.NewDecoder(r)
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("swecommon: decode JSON: %w", err)
	}
	out := make([]Values, 0, len(raw))
	for i, rawRow := range raw {
		row, err := jsonDecodeRow(schema, rawRow)
		if err != nil {
			return nil, fmt.Errorf("swecommon: decode JSON row %d: %w", i, err)
		}
		out = append(out, row)
	}
	return out, nil
}

// UnmarshalJSONRows is the bytes-taking convenience wrapper around
// [DecodeJSON].
func UnmarshalJSONRows(data []byte, schema *DataRecord) ([]Values, error) {
	return DecodeJSON(bytes.NewReader(data), schema)
}

func jsonDecodeRow(schema *DataRecord, raw map[string]json.RawMessage) (Values, error) {
	out := make(Values, len(schema.Fields))
	for _, f := range schema.Fields {
		rawVal, present := raw[f.Name]
		if !present {
			// Missing field — leave the key absent.
			continue
		}
		if isJSONNull(rawVal) {
			out[f.Name] = nil
			continue
		}
		v, err := jsonDecodeField(f.Component, rawVal)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		out[f.Name] = v
	}
	return out, nil
}

func jsonDecodeField(c DataComponent, raw json.RawMessage) (any, error) {
	switch c.Kind() {
	case KindQuantity:
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, fmt.Errorf("Quantity: %w", err)
		}
		f, err := n.Float64()
		if err != nil {
			return nil, fmt.Errorf("Quantity: %w", err)
		}
		return f, nil
	case KindCount:
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, fmt.Errorf("Count: %w", err)
		}
		i, err := n.Int64()
		if err != nil {
			return nil, fmt.Errorf("Count: %w", err)
		}
		return i, nil
	case KindTime:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("Time: %w", err)
		}
		return s, nil
	case KindBoolean:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("Boolean: %w", err)
		}
		return b, nil
	case KindText:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("Text: %w", err)
		}
		return s, nil
	case KindCategory:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("Category: %w", err)
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unknown component kind %q", c.Kind())
	}
}

func isJSONNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 4 && string(t) == "null"
}

// nilValueOf returns the nilValue token of a component, or empty.
// Mirrors the embedded CommonFields.Nil() accessor — needed
// because Go's type switch on DataComponent doesn't reach the
// embedded method directly without a per-kind branch.
func nilValueOf(c DataComponent) string {
	switch v := c.(type) {
	case Quantity:
		return v.Nil()
	case Count:
		return v.Nil()
	case Time:
		return v.Nil()
	case Boolean:
		return v.Nil()
	case Text:
		return v.Nil()
	case Category:
		return v.Nil()
	case *DataRecord:
		if v == nil {
			return ""
		}
		return v.Nil()
	default:
		return ""
	}
}

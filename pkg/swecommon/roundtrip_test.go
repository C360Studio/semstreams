package swecommon_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/swecommon"
)

// observationSchema is the canonical worked example: a 3-field
// record covering Time + Quantity (with UoM) + Category. Used by
// every encoding's round-trip table.
func observationSchema() *swecommon.DataRecord {
	return &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "time", Component: swecommon.Time{
				UoMHref: "http://www.opengis.net/def/uom/ISO-8601/0/Gregorian",
			}},
			{Name: "temperature", Component: swecommon.Quantity{
				UoMCode: "Cel",
			}},
			{Name: "status", Component: swecommon.Category{
				CodeSpace: "http://example.com/codes/sensor-status",
			}},
		},
	}
}

// observationRows is the canonical row set: ordinary row,
// nil-result row, boundary-numeric row.
func observationRows() []swecommon.Values {
	return []swecommon.Values{
		{"time": "2026-05-29T13:00:00Z", "temperature": 23.5, "status": "normal"},
		{"time": "2026-05-29T13:01:00Z", "temperature": nil, "status": "warning"},
		{"time": "2026-05-29T13:02:00Z", "temperature": -40.125, "status": "critical"},
	}
}

func TestSchema_RoundTrip(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	bytesOut, err := swecommon.MarshalSchema(schema)
	if err != nil {
		t.Fatalf("MarshalSchema: %v", err)
	}
	t.Logf("schema bytes: %s", bytesOut)
	got, err := swecommon.UnmarshalSchema(bytesOut)
	if err != nil {
		t.Fatalf("UnmarshalSchema: %v", err)
	}
	if len(got.Fields) != len(schema.Fields) {
		t.Fatalf("field count: got %d, want %d", len(got.Fields), len(schema.Fields))
	}
	for i, f := range schema.Fields {
		if got.Fields[i].Name != f.Name {
			t.Errorf("field %d name: got %q, want %q", i, got.Fields[i].Name, f.Name)
		}
		if got.Fields[i].Component.Kind() != f.Component.Kind() {
			t.Errorf("field %d kind: got %q, want %q", i, got.Fields[i].Component.Kind(), f.Component.Kind())
		}
	}
	// Spot-check UoM round-trip on the Quantity.
	q, ok := got.Fields[1].Component.(swecommon.Quantity)
	if !ok {
		t.Fatalf("field 1 component: got %T, want Quantity", got.Fields[1].Component)
	}
	if q.UoMCode != "Cel" {
		t.Errorf("quantity UoMCode: got %q, want %q", q.UoMCode, "Cel")
	}
	// Spot-check CodeSpace round-trip on the Category.
	cat, ok := got.Fields[2].Component.(swecommon.Category)
	if !ok {
		t.Fatalf("field 2 component: got %T, want Category", got.Fields[2].Component)
	}
	if cat.CodeSpace == "" {
		t.Errorf("category CodeSpace lost in round-trip")
	}
}

func TestEncodeJSON_RoundTrip(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	rows := observationRows()
	out, err := swecommon.MarshalJSONRows(schema, rows)
	if err != nil {
		t.Fatalf("MarshalJSONRows: %v", err)
	}
	t.Logf("JSON: %s", out)
	got, err := swecommon.UnmarshalJSONRows(out, schema)
	if err != nil {
		t.Fatalf("UnmarshalJSONRows: %v", err)
	}
	assertRowsEqual(t, rows, got)
}

func TestEncodeText_RoundTrip(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	rows := observationRows()
	enc := swecommon.DefaultTextEncoding()
	out, err := swecommon.MarshalTextRows(schema, rows, enc)
	if err != nil {
		t.Fatalf("MarshalTextRows: %v", err)
	}
	t.Logf("text:\n%s", out)
	got, err := swecommon.UnmarshalTextRows(out, schema, enc)
	if err != nil {
		t.Fatalf("UnmarshalTextRows: %v", err)
	}
	assertRowsEqual(t, rows, got)
}

func TestEncodeText_WithHeader_RoundTrip(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	rows := observationRows()
	enc := swecommon.DefaultTextEncoding()
	enc.EmitHeader = true
	out, err := swecommon.MarshalTextRows(schema, rows, enc)
	if err != nil {
		t.Fatalf("MarshalTextRows: %v", err)
	}
	t.Logf("text with header:\n%s", out)
	if !strings.HasPrefix(string(out), "time,temperature,status") {
		t.Errorf("header missing or wrong: %s", out)
	}
	got, err := swecommon.UnmarshalTextRows(out, schema, enc)
	if err != nil {
		t.Fatalf("UnmarshalTextRows: %v", err)
	}
	assertRowsEqual(t, rows, got)
}

func TestEncodeText_CustomSeparators_RoundTrip(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	rows := observationRows()
	enc := swecommon.TextEncoding{
		TokenSeparator:   "|",
		BlockSeparator:   ";",
		DecimalSeparator: ".",
	}
	out, err := swecommon.MarshalTextRows(schema, rows, enc)
	if err != nil {
		t.Fatalf("MarshalTextRows: %v", err)
	}
	t.Logf("custom-sep text: %s", out)
	got, err := swecommon.UnmarshalTextRows(out, schema, enc)
	if err != nil {
		t.Fatalf("UnmarshalTextRows: %v", err)
	}
	assertRowsEqual(t, rows, got)
}

func TestEncodeBinary_RoundTrip(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	rows := observationRows()
	enc := swecommon.DefaultBinaryEncoding()
	out, err := swecommon.MarshalBinaryRows(schema, rows, enc)
	if err != nil {
		t.Fatalf("MarshalBinaryRows: %v", err)
	}
	t.Logf("binary length: %d", len(out))
	got, err := swecommon.UnmarshalBinaryRows(out, schema, enc)
	if err != nil {
		t.Fatalf("UnmarshalBinaryRows: %v", err)
	}
	assertRowsEqual(t, rows, got)
}

func TestEncodeBinary_LittleEndian_RoundTrip(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	rows := observationRows()
	enc := swecommon.BinaryEncoding{ByteOrder: swecommon.LittleEndian}
	out, err := swecommon.MarshalBinaryRows(schema, rows, enc)
	if err != nil {
		t.Fatalf("MarshalBinaryRows: %v", err)
	}
	got, err := swecommon.UnmarshalBinaryRows(out, schema, enc)
	if err != nil {
		t.Fatalf("UnmarshalBinaryRows: %v", err)
	}
	assertRowsEqual(t, rows, got)
}

// TestEncodeBinary_ByteOrderRefusedCross asserts that little-endian
// bytes decoded with the big-endian setting do NOT round-trip
// cleanly. The assertion is shape-independent: either the decode
// errors, OR the decoded rows differ from the input. (We don't
// fix to one of the two so the test stays robust against schema
// reordering — Time-first happens to produce an error; a
// Quantity-first schema would produce garbage float64s but no
// error.)
func TestEncodeBinary_ByteOrderRefusedCross(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	rows := observationRows()
	out, err := swecommon.MarshalBinaryRows(schema, rows, swecommon.BinaryEncoding{ByteOrder: swecommon.LittleEndian})
	if err != nil {
		t.Fatalf("MarshalBinaryRows: %v", err)
	}
	got, decErr := swecommon.UnmarshalBinaryRows(out, schema, swecommon.DefaultBinaryEncoding())
	if decErr != nil {
		// Decode errored — the byte-order mismatch was caught at
		// the length-prefix / read-bounds layer. Acceptable signal.
		return
	}
	// Decode succeeded but the values must differ from the input.
	if len(got) != len(rows) {
		return // row count mismatch is also acceptable evidence.
	}
	for i := range rows {
		for k, want := range rows[i] {
			if !valuesMatch(want, got[i][k]) {
				return // any differing field is acceptable evidence.
			}
		}
	}
	t.Fatalf("expected byte-order disagreement to fail or produce different values; got identical round-trip: %+v", got)
}

func TestEncodeJSON_NilRoundTrip(t *testing.T) {
	t.Parallel()
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "reading", Component: swecommon.Text{}},
		},
	}
	rows := []swecommon.Values{
		{"reading": "alpha"},
		{"reading": nil},
		{"reading": "beta"},
	}
	out, err := swecommon.MarshalJSONRows(schema, rows)
	if err != nil {
		t.Fatalf("MarshalJSONRows: %v", err)
	}
	t.Logf("JSON: %s", out)
	if !bytes.Contains(out, []byte(`"reading":null`)) {
		t.Errorf("expected JSON null for nil reading, got %s", out)
	}
	got, err := swecommon.UnmarshalJSONRows(out, schema)
	if err != nil {
		t.Fatalf("UnmarshalJSONRows: %v", err)
	}
	if got[1]["reading"] != nil {
		t.Errorf("row 1 reading: got %v, want nil", got[1]["reading"])
	}
}

func TestEncodeJSON_NumericNilStandIn(t *testing.T) {
	t.Parallel()
	// Quantity with a numeric nilValue stand-in. Regression test
	// for B2: matching used to be string-only, so a numeric input
	// equal to the nil token would silently round-trip as a real
	// reading.
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "temp", Component: swecommon.Quantity{
				CommonFields: swecommon.CommonFields{NilValue: "-9999"},
				UoMCode:      "Cel",
			}},
		},
	}
	rows := []swecommon.Values{
		{"temp": 23.5},
		{"temp": float64(-9999)},
		{"temp": "-9999"},
		{"temp": int64(-9999)},
	}
	out, err := swecommon.MarshalJSONRows(schema, rows)
	if err != nil {
		t.Fatalf("MarshalJSONRows: %v", err)
	}
	t.Logf("JSON: %s", out)
	got, err := swecommon.UnmarshalJSONRows(out, schema)
	if err != nil {
		t.Fatalf("UnmarshalJSONRows: %v", err)
	}
	if v := got[0]["temp"]; v != 23.5 {
		t.Errorf("row 0: got %v, want 23.5", v)
	}
	for i := 1; i <= 3; i++ {
		if v := got[i]["temp"]; v != nil {
			t.Errorf("row %d: got %v, want nil (matched nilValue stand-in)", i, v)
		}
	}
}

func TestEncodeJSON_QuantityFromInt(t *testing.T) {
	t.Parallel()
	// Tolerance: a Go int feeding a Quantity field should encode
	// as a float64. Catches regressions in coerceFloat.
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "n", Component: swecommon.Quantity{}},
		},
	}
	rows := []swecommon.Values{{"n": int(42)}}
	out, err := swecommon.MarshalJSONRows(schema, rows)
	if err != nil {
		t.Fatalf("MarshalJSONRows: %v", err)
	}
	t.Logf("JSON: %s", out)
	got, err := swecommon.UnmarshalJSONRows(out, schema)
	if err != nil {
		t.Fatalf("UnmarshalJSONRows: %v", err)
	}
	if v, ok := got[0]["n"].(float64); !ok || v != 42 {
		t.Errorf("row 0 n: got %v (%T), want 42 (float64)", got[0]["n"], got[0]["n"])
	}
}

func TestEncodeJSON_TimeFromTimeTime(t *testing.T) {
	t.Parallel()
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "t", Component: swecommon.Time{}},
		},
	}
	tm := time.Date(2026, 5, 29, 13, 0, 0, 0, time.UTC)
	rows := []swecommon.Values{{"t": tm}}
	out, err := swecommon.MarshalJSONRows(schema, rows)
	if err != nil {
		t.Fatalf("MarshalJSONRows: %v", err)
	}
	if !bytes.Contains(out, []byte("2026-05-29T13:00:00Z")) {
		t.Errorf("expected RFC3339 in output, got %s", out)
	}
}

func TestEncodeBinary_BooleanRoundTrip(t *testing.T) {
	t.Parallel()
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "online", Component: swecommon.Boolean{}},
		},
	}
	rows := []swecommon.Values{{"online": true}, {"online": false}, {"online": nil}}
	out, err := swecommon.MarshalBinaryRows(schema, rows, swecommon.DefaultBinaryEncoding())
	if err != nil {
		t.Fatalf("MarshalBinaryRows: %v", err)
	}
	got, err := swecommon.UnmarshalBinaryRows(out, schema, swecommon.DefaultBinaryEncoding())
	if err != nil {
		t.Fatalf("UnmarshalBinaryRows: %v", err)
	}
	if got[0]["online"] != true {
		t.Errorf("row 0: got %v, want true", got[0]["online"])
	}
	if got[1]["online"] != false {
		t.Errorf("row 1: got %v, want false", got[1]["online"])
	}
	if got[2]["online"] != nil {
		t.Errorf("row 2: got %v, want nil", got[2]["online"])
	}
}

func TestEncodeBinary_CountRoundTrip(t *testing.T) {
	t.Parallel()
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "n", Component: swecommon.Count{}},
		},
	}
	rows := []swecommon.Values{
		{"n": int64(0)},
		{"n": int64(42)},
		{"n": int64(-7)},
		{"n": int64(math.MaxInt64)},
		{"n": int64(math.MinInt64)},
	}
	out, err := swecommon.MarshalBinaryRows(schema, rows, swecommon.DefaultBinaryEncoding())
	if err != nil {
		t.Fatalf("MarshalBinaryRows: %v", err)
	}
	got, err := swecommon.UnmarshalBinaryRows(out, schema, swecommon.DefaultBinaryEncoding())
	if err != nil {
		t.Fatalf("UnmarshalBinaryRows: %v", err)
	}
	for i, row := range rows {
		if got[i]["n"] != row["n"] {
			t.Errorf("row %d: got %v, want %v", i, got[i]["n"], row["n"])
		}
	}
}

func TestSchema_Validate_RejectsBadShapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		schema *swecommon.DataRecord
	}{
		{"nil record", nil},
		{"empty fields", &swecommon.DataRecord{}},
		{"empty field name", &swecommon.DataRecord{
			Fields: []swecommon.Field{{Name: "", Component: swecommon.Text{}}},
		}},
		{"nil component", &swecommon.DataRecord{
			Fields: []swecommon.Field{{Name: "x", Component: nil}},
		}},
		{"duplicate names", &swecommon.DataRecord{
			Fields: []swecommon.Field{
				{Name: "x", Component: swecommon.Text{}},
				{Name: "x", Component: swecommon.Text{}},
			},
		}},
		{"nested DataRecord rejected at schema time", &swecommon.DataRecord{
			Fields: []swecommon.Field{
				{Name: "outer", Component: &swecommon.DataRecord{
					Fields: []swecommon.Field{
						{Name: "inner", Component: swecommon.Text{}},
					},
				}},
			},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.schema.Validate(); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// TestEncodeText_EmptyStringRoundTripsAsNil pins the documented
// behavior: with no nilValue declared, an empty Text input encodes
// as empty token and decodes as Go nil. Operators who need to
// preserve empty-string readings as distinct from "absent" must
// set a non-empty nilValue on the field. Regression test for S2.
func TestEncodeText_EmptyStringRoundTripsAsNil(t *testing.T) {
	t.Parallel()
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "note", Component: swecommon.Text{}},
		},
	}
	rows := []swecommon.Values{
		{"note": "alpha"},
		{"note": ""},
		{"note": "beta"},
	}
	enc := swecommon.DefaultTextEncoding()
	out, err := swecommon.MarshalTextRows(schema, rows, enc)
	if err != nil {
		t.Fatalf("MarshalTextRows: %v", err)
	}
	t.Logf("text:\n%s", out)
	got, err := swecommon.UnmarshalTextRows(out, schema, enc)
	if err != nil {
		t.Fatalf("UnmarshalTextRows: %v", err)
	}
	if v := got[1]["note"]; v != nil {
		t.Errorf("row 1 note: got %v (%T), want nil (Phase 1 contract: empty Text token decodes to Go nil)", v, v)
	}
}

// TestEncodeText_EmptyStringPreservedWithNilToken complements the
// above: when nilValue is non-empty, an empty string input
// preserves its value through the round-trip.
func TestEncodeText_EmptyStringPreservedWithNilToken(t *testing.T) {
	t.Parallel()
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "note", Component: swecommon.Text{
				CommonFields: swecommon.CommonFields{NilValue: "<MISSING>"},
			}},
		},
	}
	rows := []swecommon.Values{
		{"note": "alpha"},
		{"note": ""},
		{"note": nil},
		{"note": "beta"},
	}
	enc := swecommon.DefaultTextEncoding()
	out, err := swecommon.MarshalTextRows(schema, rows, enc)
	if err != nil {
		t.Fatalf("MarshalTextRows: %v", err)
	}
	t.Logf("text:\n%s", out)
	got, err := swecommon.UnmarshalTextRows(out, schema, enc)
	if err != nil {
		t.Fatalf("UnmarshalTextRows: %v", err)
	}
	if v := got[1]["note"]; v != "" {
		t.Errorf("row 1 note: got %v, want empty string (preserved by nilValue token discipline)", v)
	}
	if v := got[2]["note"]; v != nil {
		t.Errorf("row 2 note: got %v, want nil", v)
	}
}

func TestEncodeText_TokenAndBlockSeparatorMustDiffer(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	enc := swecommon.TextEncoding{TokenSeparator: ",", BlockSeparator: ",", DecimalSeparator: "."}
	_, err := swecommon.MarshalTextRows(schema, observationRows(), enc)
	if err == nil {
		t.Error("expected separator-collision error, got nil")
	}
}

func TestEncodeText_DecimalSeparatorMustBeDot(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	enc := swecommon.TextEncoding{TokenSeparator: ";", BlockSeparator: "\n", DecimalSeparator: ","}
	_, err := swecommon.MarshalTextRows(schema, observationRows(), enc)
	if err == nil {
		t.Error("expected decimal-separator error, got nil")
	}
}

func TestUnmarshalSchema_RejectsNonRecordRoot(t *testing.T) {
	t.Parallel()
	// A bare Quantity at the root — legal SWE Common component
	// but not a complete schema for a stream of records.
	bytesIn := []byte(`{"type":"Quantity","uomCode":"Cel"}`)
	_, err := swecommon.UnmarshalSchema(bytesIn)
	if err == nil {
		t.Error("expected non-record-root error, got nil")
	}
}

// assertRowsEqual compares two row slices field-by-field with the
// per-kind tolerance the encoders apply (e.g. numeric round-trip
// through float64). t.Errorf-and-continue so a single bad row
// doesn't hide later mismatches.
func assertRowsEqual(t *testing.T, want, got []swecommon.Values) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("row count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		for k, w := range want[i] {
			g, ok := got[i][k]
			if !ok {
				t.Errorf("row %d field %q: missing from decoded", i, k)
				continue
			}
			if !valuesMatch(w, g) {
				t.Errorf("row %d field %q: got %v (%T), want %v (%T)", i, k, g, g, w, w)
			}
		}
	}
}

func valuesMatch(want, got any) bool {
	if want == nil && got == nil {
		return true
	}
	if want == nil || got == nil {
		return false
	}
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		return ok && math.Abs(w-g) < 1e-9
	case int:
		// Tolerance: int input may decode as float64 (Quantity) or
		// int64 (Count).
		if g, ok := got.(int64); ok {
			return int64(w) == g
		}
		if g, ok := got.(float64); ok {
			return float64(w) == g
		}
		return false
	case time.Time:
		g, ok := got.(string)
		if !ok {
			return false
		}
		// Compare against RFC 3339 nano form.
		return g == w.UTC().Format(time.RFC3339Nano) || g == w.UTC().Format(time.RFC3339)
	default:
		// Use JSON serialization as the equality oracle for
		// strings, bools, and anything else — works because the
		// encoders canonicalize through JSON shapes anyway.
		wj, werr := json.Marshal(want)
		gj, gerr := json.Marshal(got)
		if werr != nil || gerr != nil {
			return false
		}
		return bytes.Equal(wj, gj)
	}
}

// TestDecodeJSON_RefusesNilSchema asserts that the validate-on-
// entry short-circuit catches a nil schema before json.Unmarshal
// reaches it. Helps catch regressions if the validate call is
// ever removed.
func TestDecodeJSON_RefusesNilSchema(t *testing.T) {
	t.Parallel()
	_, err := swecommon.UnmarshalJSONRows([]byte("[]"), nil)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestDecodeJSON_RejectsObjectAtRoot pins the contract: SWE JSON
// row collections are JSON arrays, not objects. An object at the
// root must surface as a decode error so producers don't silently
// truncate to no rows.
func TestDecodeJSON_RejectsObjectAtRoot(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	bytesIn := []byte(`{"time":"2026-05-29T13:00:00Z","temperature":23.5,"status":"normal"}`)
	_, err := swecommon.UnmarshalJSONRows(bytesIn, schema)
	if err == nil {
		t.Error("expected decode error for object-at-root, got nil")
	}
}

// TestDecodeText_RejectsRowWithTooFewTokens pins the column-count
// check: a row whose tokens don't match the schema's field count
// must error.
func TestDecodeText_RejectsRowWithTooFewTokens(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	enc := swecommon.DefaultTextEncoding()
	// Three fields, but the second row carries only two tokens.
	bytesIn := []byte("2026-05-29T13:00:00Z,23.5,normal\n2026-05-29T13:01:00Z,24.0\n")
	_, err := swecommon.UnmarshalTextRows(bytesIn, schema, enc)
	if err == nil {
		t.Error("expected token-count error, got nil")
	}
}

// TestDecodeBinary_TruncatedStreamSurfacesUnexpectedEOF pins the
// streaming contract: a partial record mid-stream must surface
// as io.ErrUnexpectedEOF (or wrap it), not return partial rows
// with garbage values.
func TestDecodeBinary_TruncatedStreamSurfacesUnexpectedEOF(t *testing.T) {
	t.Parallel()
	schema := observationSchema()
	rows := observationRows()
	enc := swecommon.DefaultBinaryEncoding()
	out, err := swecommon.MarshalBinaryRows(schema, rows, enc)
	if err != nil {
		t.Fatalf("MarshalBinaryRows: %v", err)
	}
	if len(out) < 5 {
		t.Fatalf("binary output unexpectedly small: %d", len(out))
	}
	truncated := out[:len(out)-3]
	_, err = swecommon.UnmarshalBinaryRows(truncated, schema, enc)
	if err == nil {
		t.Fatal("expected truncation error, got nil")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("expected io.ErrUnexpectedEOF wrap, got %T: %v", err, err)
	}
}

// TestEncodeText_RejectsFieldNameWithSeparator pins S3 enforcement
// on the schema side.
func TestEncodeText_RejectsFieldNameWithSeparator(t *testing.T) {
	t.Parallel()
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "temperature,celsius", Component: swecommon.Quantity{}},
		},
	}
	_, err := swecommon.MarshalTextRows(schema, []swecommon.Values{{"temperature,celsius": 23.5}}, swecommon.DefaultTextEncoding())
	if err == nil {
		t.Error("expected separator-in-field-name error, got nil")
	}
}

// TestEncodeText_RejectsValueWithSeparator pins S3 enforcement on
// the value-emission seam.
func TestEncodeText_RejectsValueWithSeparator(t *testing.T) {
	t.Parallel()
	schema := &swecommon.DataRecord{
		Fields: []swecommon.Field{
			{Name: "note", Component: swecommon.Text{}},
		},
	}
	_, err := swecommon.MarshalTextRows(schema, []swecommon.Values{{"note": "alpha,beta"}}, swecommon.DefaultTextEncoding())
	if err == nil {
		t.Error("expected separator-in-value error, got nil")
	}
}

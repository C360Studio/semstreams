package swecommon

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// TextEncoding configures the SWE TextEncoding (SWE Common §7.4.3)
// separators. CS API requires the producer/consumer to agree on
// these; the framework defaults match the CSV media type and are
// safe round-trip targets.
//
// Token separator splits values within one record (column
// delimiter). Block separator splits records (row delimiter).
// Decimal separator must be "." to round-trip JSON-compatible
// floats; encoders surface a configuration error otherwise.
type TextEncoding struct {
	// TokenSeparator separates values within a record. Default ",".
	TokenSeparator string

	// BlockSeparator separates records. Default "\n".
	BlockSeparator string

	// DecimalSeparator is the decimal point. Must be ".".
	DecimalSeparator string

	// CollapseWhiteSpace controls whether the decoder trims
	// leading/trailing whitespace from each token. Defaults true.
	CollapseWhiteSpace bool

	// EmitHeader controls whether the encoder emits the field-name
	// header row before the first record. CS API SWE text streams
	// typically omit it (the schema is advertised separately); the
	// framework default is false to match.
	EmitHeader bool
}

// DefaultTextEncoding is the CSV-shaped default used when no
// per-datastream encoding is configured.
func DefaultTextEncoding() TextEncoding {
	return TextEncoding{
		TokenSeparator:     ",",
		BlockSeparator:     "\n",
		DecimalSeparator:   ".",
		CollapseWhiteSpace: true,
		EmitHeader:         false,
	}
}

func (te TextEncoding) resolve() (TextEncoding, error) {
	if te.TokenSeparator == "" {
		te.TokenSeparator = ","
	}
	if te.BlockSeparator == "" {
		te.BlockSeparator = "\n"
	}
	if te.DecimalSeparator == "" {
		te.DecimalSeparator = "."
	}
	if te.DecimalSeparator != "." {
		return te, fmt.Errorf("swecommon: decimal separator %q not supported (use \".\")", te.DecimalSeparator)
	}
	if te.TokenSeparator == te.BlockSeparator {
		return te, fmt.Errorf("swecommon: token and block separators must differ")
	}
	return te, nil
}

// EncodeText writes the rows as SWE TextEncoding bytes per the
// given encoding (separators + optional header row). Field order
// follows the schema's field declaration order.
//
// Nil values are emitted as the schema's nilValue token when set,
// otherwise as the empty string. The empty string is a legal
// "absent value" stand-in for SWE text streams but operators are
// strongly encouraged to set a nilValue for fields where a real
// reading could be empty (Text, Category) so decoders can tell
// "absent" from "empty string reading."
func EncodeText(w io.Writer, schema *DataRecord, rows []Values, enc TextEncoding) error {
	if err := schema.Validate(); err != nil {
		return fmt.Errorf("swecommon: encode text: %w", err)
	}
	resolved, err := enc.resolve()
	if err != nil {
		return err
	}
	if err := validateFieldNamesAgainstSeparators(schema, resolved); err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	if resolved.EmitHeader {
		for i, f := range schema.Fields {
			if i > 0 {
				if _, err := bw.WriteString(resolved.TokenSeparator); err != nil {
					return err
				}
			}
			if _, err := bw.WriteString(f.Name); err != nil {
				return err
			}
		}
		if _, err := bw.WriteString(resolved.BlockSeparator); err != nil {
			return err
		}
	}
	for ri, row := range rows {
		for fi, f := range schema.Fields {
			if fi > 0 {
				if _, err := bw.WriteString(resolved.TokenSeparator); err != nil {
					return err
				}
			}
			token, err := textEncodeField(f.Component, fieldValue(row, f.Name))
			if err != nil {
				return fmt.Errorf("swecommon: encode text row %d field %q: %w", ri, f.Name, err)
			}
			if err := validateTokenAgainstSeparators(token, resolved); err != nil {
				return fmt.Errorf("swecommon: encode text row %d field %q: %w", ri, f.Name, err)
			}
			if _, err := bw.WriteString(token); err != nil {
				return err
			}
		}
		if _, err := bw.WriteString(resolved.BlockSeparator); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// MarshalTextRows is the bytes-returning convenience wrapper around
// [EncodeText].
func MarshalTextRows(schema *DataRecord, rows []Values, enc TextEncoding) ([]byte, error) {
	var buf bytes.Buffer
	if err := EncodeText(&buf, schema, rows, enc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func textEncodeField(c DataComponent, v any) (string, error) {
	if matchesNilToken(c, v) {
		v = nil
	}
	canon, isNil, err := normalizeValue(c, v)
	if err != nil {
		return "", err
	}
	if isNil {
		return nilValueOf(c), nil
	}
	switch c.Kind() {
	case KindQuantity:
		return strconv.FormatFloat(canon.(float64), 'f', -1, 64), nil
	case KindCount:
		return strconv.FormatInt(canon.(int64), 10), nil
	case KindTime, KindText, KindCategory:
		return canon.(string), nil
	case KindBoolean:
		return strconv.FormatBool(canon.(bool)), nil
	default:
		return "", fmt.Errorf("unsupported kind %q", c.Kind())
	}
}

// DecodeText parses SWE TextEncoding bytes back into typed rows
// against the schema. Field count per row must match the schema's
// field count exactly; mismatched rows surface as errors.
//
// Header handling: when enc.EmitHeader is true, the first
// non-empty block is consumed as the header and validated against
// the schema's field names. When false, no header is expected.
func DecodeText(r io.Reader, schema *DataRecord, enc TextEncoding) ([]Values, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("swecommon: decode text: %w", err)
	}
	resolved, err := enc.resolve()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("swecommon: decode text: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	blocks := strings.Split(string(raw), resolved.BlockSeparator)
	// A trailing block separator yields an empty final element —
	// drop it. Interior empty blocks ARE NOT skipped: for a
	// single-field schema an empty block is a single empty-token
	// row (which decodes to nil unless the schema declares a
	// nilValue); for multi-field schemas the token-count check
	// below catches them as malformed rows.
	if len(blocks) > 0 && blocks[len(blocks)-1] == "" {
		blocks = blocks[:len(blocks)-1]
	}
	out := make([]Values, 0, len(blocks))
	headerConsumed := !resolved.EmitHeader
	for bi, block := range blocks {
		if !headerConsumed {
			if err := validateHeader(block, schema, resolved); err != nil {
				return nil, fmt.Errorf("swecommon: decode text header: %w", err)
			}
			headerConsumed = true
			continue
		}
		tokens := strings.Split(block, resolved.TokenSeparator)
		if len(tokens) != len(schema.Fields) {
			return nil, fmt.Errorf("swecommon: decode text block %d: got %d tokens, want %d",
				bi, len(tokens), len(schema.Fields))
		}
		row := make(Values, len(schema.Fields))
		for fi, f := range schema.Fields {
			tok := tokens[fi]
			if resolved.CollapseWhiteSpace {
				tok = strings.TrimSpace(tok)
			}
			v, err := textDecodeField(f.Component, tok)
			if err != nil {
				return nil, fmt.Errorf("swecommon: decode text block %d field %q: %w", bi, f.Name, err)
			}
			row[f.Name] = v
		}
		out = append(out, row)
	}
	return out, nil
}

// UnmarshalTextRows is the bytes-taking convenience wrapper around
// [DecodeText].
func UnmarshalTextRows(data []byte, schema *DataRecord, enc TextEncoding) ([]Values, error) {
	return DecodeText(bytes.NewReader(data), schema, enc)
}

// validateFieldNamesAgainstSeparators rejects schemas whose field
// names contain the configured token or block separators. SWE
// TextEncoding does not specify quoting, so a separator inside a
// field name silently corrupts the header row and breaks the
// downstream decoder's column-count check. Failing fast at encode
// time surfaces the conflict before bad bytes hit the wire.
func validateFieldNamesAgainstSeparators(schema *DataRecord, enc TextEncoding) error {
	for _, f := range schema.Fields {
		if strings.Contains(f.Name, enc.TokenSeparator) {
			return fmt.Errorf("swecommon: encode text: field name %q contains token separator %q", f.Name, enc.TokenSeparator)
		}
		if strings.Contains(f.Name, enc.BlockSeparator) {
			return fmt.Errorf("swecommon: encode text: field name %q contains block separator %q", f.Name, enc.BlockSeparator)
		}
	}
	return nil
}

// validateTokenAgainstSeparators rejects emitted string tokens
// (Text / Category values, Time strings) that contain the
// configured separators. Mirrors the field-name check at the
// value-emission seam — preserves the same fail-loud-at-encode
// discipline.
func validateTokenAgainstSeparators(token string, enc TextEncoding) error {
	if strings.Contains(token, enc.TokenSeparator) {
		return fmt.Errorf("token %q contains token separator %q", token, enc.TokenSeparator)
	}
	if strings.Contains(token, enc.BlockSeparator) {
		return fmt.Errorf("token %q contains block separator %q", token, enc.BlockSeparator)
	}
	return nil
}

func validateHeader(block string, schema *DataRecord, enc TextEncoding) error {
	tokens := strings.Split(block, enc.TokenSeparator)
	if len(tokens) != len(schema.Fields) {
		return fmt.Errorf("header has %d columns, schema has %d fields", len(tokens), len(schema.Fields))
	}
	for i, tok := range tokens {
		if enc.CollapseWhiteSpace {
			tok = strings.TrimSpace(tok)
		}
		if tok != schema.Fields[i].Name {
			return fmt.Errorf("column %d: header %q does not match field name %q", i, tok, schema.Fields[i].Name)
		}
	}
	return nil
}

func textDecodeField(c DataComponent, tok string) (any, error) {
	// Nil-stand-in semantics on decode side:
	//   1. If the schema declares a nilValue token, ONLY that
	//      exact token decodes to Go nil — empty Text/Category
	//      tokens preserve as empty strings, so a producer can
	//      distinguish "no reading" from "empty reading."
	//   2. If no nilValue is declared, an empty token decodes to
	//      Go nil for non-string kinds (no other reading is
	//      possible). For Text/Category an empty token also
	//      decodes to nil — operators wanting to round-trip empty
	//      string readings must declare a nilValue token.
	nilStr := nilValueOf(c)
	if nilStr != "" {
		if tok == nilStr {
			return nil, nil //nolint:nilnil // SWE text nil-stand-in: nil value is the schema-sanctioned answer for a nil-encoded token
		}
		// Fall through — empty tokens for Text/Category are real
		// empty strings when a distinct nilValue is declared.
	} else if tok == "" {
		return nil, nil //nolint:nilnil // SWE text empty token without nilValue: framework treats as nil per Phase 1 contract
	}
	switch c.Kind() {
	case KindQuantity:
		f, err := strconv.ParseFloat(tok, 64)
		if err != nil {
			return nil, fmt.Errorf("Quantity: %w", err)
		}
		return f, nil
	case KindCount:
		i, err := strconv.ParseInt(tok, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("Count: %w", err)
		}
		return i, nil
	case KindTime:
		return tok, nil
	case KindBoolean:
		b, err := strconv.ParseBool(tok)
		if err != nil {
			return nil, fmt.Errorf("Boolean: %w", err)
		}
		return b, nil
	case KindText, KindCategory:
		return tok, nil
	default:
		return nil, fmt.Errorf("unsupported kind %q", c.Kind())
	}
}

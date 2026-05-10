package wire

import (
	"reflect"
	"strings"
	"testing"
)

// TestJSONTags_NoEmptyName guards against the failure mode at
// types.go:knownFieldsOf — a struct field tagged with json:",..."
// (empty name before the comma) would be emitted by encoding/json
// under its Go name (capitalized) but treated as unknown by the
// helpers, causing a round-trip duplication into Extras. Every wire
// struct must use an explicit JSON name; this test enumerates every
// exported struct in the package and rejects the empty-name form.
//
// Add new wire types to typesUnderTagAudit so they're covered.
func TestJSONTags_NoEmptyName(t *testing.T) {
	t.Parallel()
	for _, sample := range typesUnderTagAudit {
		typ := reflect.TypeOf(sample)
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		for i := range typ.NumField() {
			f := typ.Field(i)
			tag := f.Tag.Get("json")
			if tag == "" || tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if name == "" {
				t.Errorf("%s.%s: empty json name before comma (%q) — knownFieldsOf would treat the field as unknown",
					typ.Name(), f.Name, tag)
			}
		}
	}
}

// typesUnderTagAudit lists every wire struct that round-trips through
// the marshalWithExtras / unmarshalWithExtras helpers. Adding a new
// wire struct without adding it here means the JSONTags audit doesn't
// see it.
var typesUnderTagAudit = []any{
	ChatCompletionRequest{},
	StreamOptions{},
	Message{},
	ContentPart{},
	ContentImageURL{},
	ToolCall{},
	Function{},
	Tool{},
	FunctionDefinition{},
	ResponseFormat{},
	JSONSchema{},
	ChatCompletionResponse{},
	Choice{},
	Usage{},
	StreamChunk{},
	EmbeddingsRequest{},
	EmbeddingsResponse{},
	EmbeddingDatum{},
	APIError{},
}

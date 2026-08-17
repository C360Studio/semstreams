// Package jsoncanon provides the repository-owned JSON normalization used by
// configuration equality and provenance digests.
package jsoncanon

import (
	"bytes"
	"encoding/json"
	"io"
)

// Normalize returns an object-key-order- and whitespace-insensitive encoding
// of one JSON value. Empty input normalizes to null. Number source text is
// preserved so large integers are never collapsed through float64.
func Normalize(raw json.RawMessage) ([]byte, bool) {
	if len(raw) == 0 {
		return []byte("null"), true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	return normalized, true
}

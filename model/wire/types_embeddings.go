package wire

import "encoding/json"

// EmbeddingsRequest is the wire-shape Embeddings request body. Used by
// graph/embedding once the embeddings migration chunk lands.
type EmbeddingsRequest struct {
	Model          string                     `json:"model"`
	Input          json.RawMessage            `json:"input"`
	EncodingFormat string                     `json:"encoding_format,omitempty"`
	Dimensions     int                        `json:"dimensions,omitempty"`
	User           string                     `json:"user,omitempty"`
	Extras         map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (r EmbeddingsRequest) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(r, r.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (r *EmbeddingsRequest) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, r, &r.Extras)
}

// EmbeddingsResponse is the embeddings response.
type EmbeddingsResponse struct {
	Object string                     `json:"object,omitempty"`
	Model  string                     `json:"model"`
	Data   []EmbeddingDatum           `json:"data"`
	Usage  *Usage                     `json:"usage,omitempty"`
	Extras map[string]json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler with Extras merge.
func (r EmbeddingsResponse) MarshalJSON() ([]byte, error) {
	return marshalWithExtras(r, r.Extras)
}

// UnmarshalJSON implements json.Unmarshaler with Extras split.
func (r *EmbeddingsResponse) UnmarshalJSON(data []byte) error {
	return unmarshalWithExtras(data, r, &r.Extras)
}

// EmbeddingDatum is one vector returned by the embeddings endpoint.
type EmbeddingDatum struct {
	Object    string    `json:"object,omitempty"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

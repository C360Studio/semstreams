package wire

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// APIError is the decoded error returned when a non-2xx HTTP status
// is received from a ChatCompletion call. The shape matches OpenAI's
// documented `error` object; fields a given provider doesn't set are
// left empty.
//
// Code is a string for OpenAI-style error codes ("rate_limit_exceeded",
// "invalid_api_key") and tolerates numeric values from providers like
// Gemini that overload the field with the HTTP status (e.g. 400).
// Numeric codes are stringified on decode.
type APIError struct {
	StatusCode int    `json:"-"`
	Type       string `json:"type,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	Param      string `json:"param,omitempty"`
	Status     string `json:"status,omitempty"`
}

// Error implements error.
func (e *APIError) Error() string {
	return fmt.Sprintf("wire: HTTP %d: %s (type=%q code=%q)", e.StatusCode, e.Message, e.Type, e.Code)
}

// UnmarshalJSON tolerates numeric or string `code` values. Numeric
// codes are stringified.
func (e *APIError) UnmarshalJSON(data []byte) error {
	var aux struct {
		Type    string          `json:"type"`
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Param   string          `json:"param"`
		Status  string          `json:"status"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	e.Type = aux.Type
	e.Message = aux.Message
	e.Param = aux.Param
	e.Status = aux.Status
	if len(aux.Code) > 0 {
		raw := bytes.TrimSpace(aux.Code)
		if len(raw) > 0 && raw[0] == '"' {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				e.Code = s
			}
		} else {
			e.Code = string(raw)
		}
	}
	return nil
}

// errorEnvelope is the standard `{"error": {...}}` shape OpenAI ships.
type errorEnvelope struct {
	Error APIError `json:"error"`
}

// DecodeError parses an HTTP error body into an *APIError, tolerating
// two known shapes:
//
//   - The standard object shape: {"error": {...}}
//   - Gemini 3.x preview's array-wrapped variant: [{"error": {...}}]
//     (the leading [ is sniffed and the first element peeled before
//     decode)
//
// statusCode is recorded on the returned error for callers that
// branch on it. body may be empty; in that case a synthetic error is
// returned describing the status code.
func DecodeError(statusCode int, body []byte) *APIError {
	trimmed := trimJSONLeading(body)
	if len(trimmed) == 0 {
		return &APIError{
			StatusCode: statusCode,
			Message:    fmt.Sprintf("HTTP %d with empty body", statusCode),
		}
	}

	if trimmed[0] == '[' {
		var arr []errorEnvelope
		if err := json.Unmarshal(trimmed, &arr); err == nil && len(arr) > 0 && arr[0].Error.Message != "" {
			out := arr[0].Error
			out.StatusCode = statusCode
			return &out
		}
	}

	var env errorEnvelope
	if err := json.Unmarshal(trimmed, &env); err == nil && env.Error.Message != "" {
		out := env.Error
		out.StatusCode = statusCode
		return &out
	}

	var bare APIError
	if err := json.Unmarshal(trimmed, &bare); err == nil && bare.Message != "" {
		bare.StatusCode = statusCode
		return &bare
	}

	return &APIError{
		StatusCode: statusCode,
		Message:    fmt.Sprintf("HTTP %d: %s", statusCode, string(body)),
	}
}

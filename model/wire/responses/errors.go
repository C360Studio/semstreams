package responses

import "github.com/c360studio/semstreams/model/wire"

// APIError is the decoded non-2xx error from a Responses call.
// Re-exported from model/wire — the envelope shape is identical to
// ChatCompletion's per ADR-051 ("Same envelope shape; same HTTP
// status semantics" in the structural-delta table). Callers can
// errors.As on either *wire.APIError or *responses.APIError; they
// are the same type.
type APIError = wire.APIError

// DecodeError parses a Responses error body into an APIError.
// Re-exported from model/wire; the body shape matches.
var DecodeError = wire.DecodeError

// Package graph provides query request/response contracts for the graph query system.
// These types are shared between handlers (producers) and clients (consumers)
// to ensure type safety and consistent API contracts.
package graph

import (
	"encoding/json"
	"time"
)

// QueryResponse is the standard SUCCESS envelope for all query responses.
//
// ADR-060: a query reply is EITHER this success body (nil Go error) OR a typed
// *errs.ClassifiedError on the err channel — the in-body Error field was
// removed. A RequestClassified caller branches on the returned err
// (errs.IsInvalid / IsTransient, errors.As → ce.Code); success unmarshals here.
type QueryResponse[T any] struct {
	Data      T         `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

// NewQueryResponse creates a successful response with the given data.
func NewQueryResponse[T any](data T) QueryResponse[T] {
	return QueryResponse[T]{
		Data:      data,
		Timestamp: time.Now(),
	}
}

// Marshalled field names of QueryResponse. This is the SINGLE source for
// "which keys make up the envelope" — UnwrapQueryResponse discriminates on it,
// and a field added to QueryResponse above must be added here or the
// discriminator silently stops recognizing the envelope it describes.
const (
	queryResponseDataKey      = "data"
	queryResponseTimestampKey = "timestamp"
)

// UnwrapQueryResponse removes ONE QueryResponse envelope from a marshalled
// query reply, reporting whether it did.
//
// It returns (payload, true) when raw is a marshalled QueryResponse, and
// (raw, false) — the input, byte-for-byte — when it is not. Failing to be an
// envelope is the ordinary case for the query families that do not use one, so
// it is not an error and is not reported as one.
//
// # Why the caller must not decide this from the subject
//
// The families do not partition by envelope usage. `graph.query.summary` is
// served by graph-query's own handler and returns this envelope, so a
// prefix-gated unwrap keyed on `graph.index.query.` left it double-nested as
// `data.<field>.data.*` — that is gh#762, and it is the observed defect.
//
// The property is more general than that one instance, which is why detection
// rather than a corrected subject list. Query handlers PROXY: graph-query's
// semantic, spatial, similar, temporal, entity and byName handlers forward to a
// downstream subject and return that reply verbatim, so a reply enveloped by
// one component can surface under another family's subject. No reachable proxy
// surfaces an envelope TODAY — this is a soundness property, not a second live
// bug — but it means whether a reply carries the envelope is a property of the
// REPLY, and any subject-keyed rule is one downstream change away from being
// wrong again.
//
// # The discriminator is the CLOSED key set, deliberately
//
// A reply is the envelope only when it has BOTH `data` and `timestamp` and
// every one of its keys is drawn from {data, timestamp}.
//
// Detecting on `data` alone would be the dangerous form: any reply that
// legitimately carries a top-level `data` field would be stripped of a nesting
// level, turning a cosmetic projection defect into silent data loss. Timestamp
// carries no `omitempty`, so a real envelope always has it and the conjunction
// is free. Requiring the set to be CLOSED additionally means a reply bearing
// `data` and `timestamp` ALONGSIDE other fields is not an envelope and is left
// alone.
//
// # What it does NOT promise
//
//   - It does NOT unwrap repeatedly. Exactly one layer is removed, because
//     exactly one is applied by the producer. Re-testing the payload would make
//     the number of layers removed depend on user data, so a reply whose own
//     contents happened to match would be silently flattened.
//   - It does NOT validate the payload or the timestamp's type.
//     Key presence is the whole discriminator.
//   - It does NOT report envelope-borne errors. ADR-060 removed the in-body
//     Error field: a query reply is EITHER this success body OR a classified
//     error on the err channel. A caller looking for an error in here is reading
//     a field that no producer has emitted since that ADR.
func UnwrapQueryResponse(raw []byte) ([]byte, bool) {
	var fields map[string]json.RawMessage
	// A non-object reply (array, scalar, malformed) is not the envelope.
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, false
	}

	data, hasData := fields[queryResponseDataKey]
	if !hasData {
		return raw, false
	}
	if _, hasTimestamp := fields[queryResponseTimestampKey]; !hasTimestamp {
		return raw, false
	}

	// Closed set: any foreign key means this is some other type that merely
	// shares two field names with the envelope.
	for key := range fields {
		switch key {
		case queryResponseDataKey, queryResponseTimestampKey:
		default:
			return raw, false
		}
	}

	return data, true
}

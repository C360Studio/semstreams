// Package graph provides query request/response contracts for the graph query system.
// These types are shared between handlers (producers) and clients (consumers)
// to ensure type safety and consistent API contracts.
package graph

import (
	"encoding/json"
	"errors"
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
	RequestID string    `json:"request_id,omitempty"`
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
	queryResponseRequestIDKey = "request_id"
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
// every one of its keys is drawn from {data, request_id, timestamp}.
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
//   - It does NOT validate the payload, the timestamp's type, or the request_id.
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
		case queryResponseDataKey, queryResponseRequestIDKey, queryResponseTimestampKey:
		default:
			return raw, false
		}
	}

	return data, true
}

// Marshalled field names of a JetStream publish acknowledgement. Same role as
// the envelope key set above: the SINGLE source for "which keys make up an ack".
//
// `stream` and `seq` are always present; `domain` appears in a domain-scoped
// deployment and `duplicate` when the server recognises a repeat.
const (
	publishAckStreamKey    = "stream"
	publishAckSeqKey       = "seq"
	publishAckDomainKey    = "domain"
	publishAckDuplicateKey = "duplicate"
)

// ErrPublishAck reports that a reply body is a JetStream publish
// acknowledgement rather than a query reply.
//
// It means a stream captured the request/reply subject: the request was
// published into a stream and acked, and no responder ever saw it. That is a
// DEPLOYMENT fault — a stream's subject filter overlapping a subject the
// framework answers on — not a malformed reply, and the remedy is to move the
// subject or narrow the stream (gh#810).
var ErrPublishAck = errors.New("graph: reply is a JetStream publish ack — a stream captured the request/reply subject")

// IsPublishAck reports whether raw is a JetStream publish acknowledgement.
//
// # Why this needs its own discriminator
//
// An ack is structurally valid JSON that carries none of the fields a caller
// expects, so decoding it yields the ZERO value of the target type: an empty
// catalog, an empty result set. Empty is indistinguishable from "nothing is
// registered", which is how gh#810 stayed invisible — a deployment whose
// streams covered `tool.>` served `{"stream":"TOOL","seq":1}` to every
// discovery request and reported a zero-tool catalog with no error anywhere.
//
// # Closed key set, for the same reason the envelope uses one
//
// An ack has BOTH `stream` and `seq`, and every key is drawn from
// {stream, seq, domain, duplicate}. Detecting on `stream` alone would be the
// dangerous form: a legitimate reply that happens to carry a top-level `stream`
// field would be rejected outright, trading a silent empty result for a hard
// failure on valid data. The conjunction plus closure keeps that from firing on
// anything but an actual ack.
func IsPublishAck(raw []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	if _, ok := fields[publishAckStreamKey]; !ok {
		return false
	}
	if _, ok := fields[publishAckSeqKey]; !ok {
		return false
	}
	for key := range fields {
		switch key {
		case publishAckStreamKey, publishAckSeqKey, publishAckDomainKey, publishAckDuplicateKey:
		default:
			return false
		}
	}
	return true
}

// DecodeQueryReply is the canonical entry point for a marshalled query reply:
// it refuses a publish ack, then removes at most one QueryResponse envelope.
//
// UnwrapQueryResponse alone cannot express this. Its contract is
// (payload, wasEnvelope) with no error channel, and an ack is simply "not an
// envelope" — so it passes the ack through byte-for-byte and the caller decodes
// it into a zero value. The missing piece was never the unwrapping; it was
// having somewhere to say "this is not a reply at all".
//
// Callers that only need the envelope removed may keep using
// UnwrapQueryResponse. Callers decoding a reply into a typed result should use
// this, so a captured subject surfaces as an error naming the cause rather than
// as an empty result several hops from the fact (gh#785 migrates the remaining
// in-repo shape-knowers onto it).
func DecodeQueryReply(raw []byte) ([]byte, error) {
	if IsPublishAck(raw) {
		return nil, ErrPublishAck
	}
	payload, _ := UnwrapQueryResponse(raw)
	return payload, nil
}

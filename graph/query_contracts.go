// Package graph provides query request/response contracts for the graph query system.
// These types are shared between handlers (producers) and clients (consumers)
// to ensure type safety and consistent API contracts.
package graph

import "time"

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

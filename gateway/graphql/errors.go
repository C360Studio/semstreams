package graphql

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
	"github.com/c360/semstreams/errors"
	"github.com/nats-io/nats.go"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// mapNATSError converts NATS errors to GraphQL errors with appropriate error codes
func mapNATSError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// Map NATS-specific errors
	switch {
	case err == nats.ErrTimeout:
		return &gqlerror.Error{
			Message: "Query timeout - please try again",
			Extensions: map[string]interface{}{
				"code":      "TIMEOUT",
				"operation": operation,
			},
		}

	case err == nats.ErrNoResponders:
		return &gqlerror.Error{
			Message: "Service unavailable - no responders for query",
			Extensions: map[string]interface{}{
				"code":      "SERVICE_UNAVAILABLE",
				"operation": operation,
			},
		}

	case err == nats.ErrConnectionClosed:
		return &gqlerror.Error{
			Message: "Connection closed - please retry",
			Extensions: map[string]interface{}{
				"code":      "CONNECTION_CLOSED",
				"operation": operation,
			},
		}

	case err == context.DeadlineExceeded:
		return &gqlerror.Error{
			Message: "Query timeout exceeded",
			Extensions: map[string]interface{}{
				"code":      "DEADLINE_EXCEEDED",
				"operation": operation,
			},
		}

	case err == context.Canceled:
		return &gqlerror.Error{
			Message: "Query cancelled",
			Extensions: map[string]interface{}{
				"code":      "CANCELLED",
				"operation": operation,
			},
		}
	}

	// Map SemStreams error classifications
	if errors.IsTransient(err) {
		return &gqlerror.Error{
			Message: fmt.Sprintf("Temporary error: %s", err.Error()),
			Extensions: map[string]interface{}{
				"code":      "TRANSIENT_ERROR",
				"operation": operation,
				"retryable": true,
			},
		}
	}

	if errors.IsInvalid(err) {
		return &gqlerror.Error{
			Message: fmt.Sprintf("Invalid input: %s", err.Error()),
			Extensions: map[string]interface{}{
				"code":      "INVALID_INPUT",
				"operation": operation,
			},
		}
	}

	if errors.IsFatal(err) {
		return &gqlerror.Error{
			Message: "Internal server error",
			Extensions: map[string]interface{}{
				"code":      "INTERNAL_ERROR",
				"operation": operation,
			},
		}
	}

	// Generic error
	return &gqlerror.Error{
		Message: fmt.Sprintf("Query failed: %s", err.Error()),
		Extensions: map[string]interface{}{
			"code":      "QUERY_ERROR",
			"operation": operation,
		},
	}
}

// mapJSONError converts JSON unmarshaling errors to GraphQL errors
func mapJSONError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// Check for specific JSON errors
	switch e := err.(type) {
	case *json.SyntaxError:
		return &gqlerror.Error{
			Message: "Invalid response format from service",
			Extensions: map[string]interface{}{
				"code":      "INVALID_RESPONSE",
				"operation": operation,
				"offset":    e.Offset,
			},
		}

	case *json.UnmarshalTypeError:
		return &gqlerror.Error{
			Message: fmt.Sprintf("Invalid response type: expected %s, got %s", e.Type, e.Value),
			Extensions: map[string]interface{}{
				"code":      "INVALID_RESPONSE_TYPE",
				"operation": operation,
				"field":     e.Field,
			},
		}
	}

	// Generic JSON error
	return &gqlerror.Error{
		Message: "Failed to parse service response",
		Extensions: map[string]interface{}{
			"code":      "PARSE_ERROR",
			"operation": operation,
		},
	}
}

// wrapError wraps an error with GraphQL error format, preserving context
func wrapError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// If already a GraphQL error, return as-is
	if _, ok := err.(*gqlerror.Error); ok {
		return err
	}

	// Try NATS error mapping first
	if gqlErr := mapNATSError(err, operation); gqlErr != nil {
		return gqlErr
	}

	// Try JSON error mapping
	if gqlErr := mapJSONError(err, operation); gqlErr != nil {
		return gqlErr
	}

	// Generic wrapping
	return &gqlerror.Error{
		Message: err.Error(),
		Extensions: map[string]interface{}{
			"code":      "UNKNOWN_ERROR",
			"operation": operation,
		},
	}
}

// addErrorToContext adds an error to the GraphQL context for error reporting
func addErrorToContext(ctx context.Context, err error) {
	if err == nil {
		return
	}

	// Get GraphQL error list from context
	errList := graphql.GetErrors(ctx)
	if errList == nil {
		errList = gqlerror.List{}
	}

	// Add error to list
	if gqlErr, ok := err.(*gqlerror.Error); ok {
		errList = append(errList, gqlErr)
	} else {
		errList = append(errList, &gqlerror.Error{
			Message: err.Error(),
		})
	}

	// Store back in context
	graphql.AddError(ctx, errList[len(errList)-1])
}

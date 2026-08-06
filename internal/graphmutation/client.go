package graphmutation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

type requester interface {
	RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error)
}

// TransportOutcome describes what the requester can prove when no valid
// mutation reply exists. Only CommitUnknown permits that the command may have
// reached graph-ingest.
type TransportOutcome string

const (
	// TransportUnavailable proves that no mutation responder accepted the request.
	TransportUnavailable TransportOutcome = "unavailable"
	// TransportDeadline proves the context ended before request delivery.
	TransportDeadline TransportOutcome = "deadline"
	// TransportCommitUnknown reports delivery without a valid mutation reply.
	TransportCommitUnknown TransportOutcome = "commit_unknown"
)

// TransportError is returned only for transport or invalid-reply failures.
// Classified handler failures remain their original typed error.
type TransportError struct {
	Operation Operation
	Outcome   TransportOutcome
	Err       error
}

func (e *TransportError) Error() string {
	if e == nil {
		return "graph mutation transport failed"
	}
	return fmt.Sprintf("graph mutation %s transport failed (%s): %v", e.Operation, e.Outcome, e.Err)
}

func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsCommitUnknown reports whether a mutation may have committed despite the
// missing or invalid reply.
func IsCommitUnknown(err error) bool {
	var transport *TransportError
	return errors.As(err, &transport) && transport.Outcome == TransportCommitUnknown
}

// IsDefinitelyNotCommitted reports transport outcomes that prove no request
// was delivered: no responder, or a context already done before the call.
func IsDefinitelyNotCommitted(err error) bool {
	var transport *TransportError
	return errors.As(err, &transport) &&
		(transport.Outcome == TransportUnavailable || transport.Outcome == TransportDeadline)
}

// Client is the single in-repository adapter for the canonical mutation port.
// Each method makes exactly one request. It never retries an ambiguous delivery.
type Client struct {
	requester requester
	timeout   time.Duration
}

// NewClient binds the canonical mutation protocol to a request/reply client.
func NewClient(requester requester, timeout time.Duration) (*Client, error) {
	if requester == nil {
		return nil, errors.New("graph mutation requester is required")
	}
	if timeout <= 0 {
		return nil, errors.New("graph mutation timeout must be positive")
	}
	return &Client{requester: requester, timeout: timeout}, nil
}

// Create performs one atomic entity birth request.
func (c *Client) Create(ctx context.Context, request graph.CreateEntityRequest) (*graph.CreateEntityResponse, error) {
	var response graph.CreateEntityResponse
	if err := c.request(ctx, CreateEntity, request, &response); err != nil {
		return nil, err
	}
	if response.Outcome != graph.MutationApplied || request.Entity == nil || response.Entity == nil ||
		response.Entity.ID != request.Entity.ID || response.KVRevision == 0 {
		return nil, unknownReply(CreateEntity, errors.New("invalid create response"))
	}
	return &response, nil
}

// Reconcile makes one predicate set equal the supplied desired set.
func (c *Client) Reconcile(
	ctx context.Context,
	request graph.ReconcilePredicatesRequest,
) (*graph.ReconcilePredicatesResponse, error) {
	var response graph.ReconcilePredicatesResponse
	if err := c.request(ctx, ReconcilePredicates, request, &response); err != nil {
		return nil, err
	}
	if (response.Outcome != graph.MutationApplied && response.Outcome != graph.MutationUnchanged) ||
		response.Entity == nil || response.Entity.ID != request.EntityID || response.KVRevision == 0 {
		return nil, unknownReply(ReconcilePredicates, errors.New("invalid reconcile response"))
	}
	return &response, nil
}

// Append appends triples independently per distinct subject.
func (c *Client) Append(ctx context.Context, request graph.AppendTriplesRequest) (*graph.AppendTriplesResponse, error) {
	var response graph.AppendTriplesResponse
	if err := c.request(ctx, AppendTriples, request, &response); err != nil {
		return nil, err
	}
	if err := validateAppendResponse(request, response); err != nil {
		return nil, unknownReply(AppendTriples, err)
	}
	return &response, nil
}

// Delete conditionally deletes one entity at the supplied authority revision.
func (c *Client) Delete(ctx context.Context, request graph.DeleteEntityRequest) (*graph.DeleteEntityResponse, error) {
	var response graph.DeleteEntityResponse
	if err := c.request(ctx, DeleteEntity, request, &response); err != nil {
		return nil, err
	}
	if response.Outcome != graph.MutationApplied || response.EntityID != request.EntityID ||
		response.ExpectedRevision != request.ExpectedRevision {
		return nil, unknownReply(DeleteEntity, errors.New("invalid delete response"))
	}
	return &response, nil
}

func (c *Client) request(ctx context.Context, operation Operation, request any, response any) error {
	if c == nil || c.requester == nil {
		return errors.New("graph mutation client is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return &TransportError{Operation: operation, Outcome: TransportDeadline, Err: err}
	}
	subject, err := ResolveSubject(SubjectFamily, operation)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal graph mutation %s: %w", operation, err)
	}
	reply, err := c.requester.RequestClassified(ctx, subject, payload, c.timeout)
	if err != nil {
		var classified *errs.ClassifiedError
		if errors.As(err, &classified) {
			return fmt.Errorf("graph mutation %s rejected: %w", operation, err)
		}
		if natsclient.IsNoResponders(err) {
			return &TransportError{Operation: operation, Outcome: TransportUnavailable, Err: err}
		}
		return &TransportError{Operation: operation, Outcome: TransportCommitUnknown, Err: err}
	}
	if err := json.Unmarshal(reply, response); err != nil {
		return unknownReply(operation, fmt.Errorf("decode response: %w", err))
	}
	return nil
}

func unknownReply(operation Operation, err error) error {
	return &TransportError{Operation: operation, Outcome: TransportCommitUnknown, Err: err}
}

func validateAppendResponse(request graph.AppendTriplesRequest, response graph.AppendTriplesResponse) error {
	want := make(map[string]struct{})
	for _, triple := range request.Triples {
		want[triple.Subject] = struct{}{}
	}
	if len(want) == 0 || len(response.Results) != len(want) {
		return fmt.Errorf("invalid append response result count")
	}
	seen := make(map[string]struct{}, len(response.Results))
	for _, result := range response.Results {
		if _, requested := want[result.EntityID]; !requested {
			return fmt.Errorf("invalid append response subject %q", result.EntityID)
		}
		if _, duplicate := seen[result.EntityID]; duplicate {
			return fmt.Errorf("duplicate append response subject %q", result.EntityID)
		}
		seen[result.EntityID] = struct{}{}
		if result.Outcome == graph.MutationFailed {
			if result.Error == nil || result.Error.Class == "" || result.Error.Code == "" || result.KVRevision != 0 {
				return fmt.Errorf("invalid append failure for subject %q", result.EntityID)
			}
			continue
		}
		if result.Error != nil {
			return fmt.Errorf("append outcome %q for subject %q must not include an error", result.Outcome, result.EntityID)
		}
		switch result.Outcome {
		case graph.MutationApplied, graph.MutationUnchanged:
			if result.KVRevision == 0 {
				return fmt.Errorf("append outcome %q for subject %q lacks revision", result.Outcome, result.EntityID)
			}
		case graph.MutationEntityNotFound:
			if result.KVRevision != 0 {
				return fmt.Errorf("not-found append result for subject %q has revision", result.EntityID)
			}
		default:
			return fmt.Errorf("invalid append outcome %q for subject %q", result.Outcome, result.EntityID)
		}
	}
	return nil
}

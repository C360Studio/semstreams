package graphingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// setupMutationHandlers registers exactly the four operations exposed by the
// component's required typed mutation-provider port.
func (c *Component) setupMutationHandlers(ctx context.Context) error {
	routes, err := c.canonicalMutationRoutes()
	if err != nil {
		return fmt.Errorf("resolve mutation provider routes: %w", err)
	}

	subjects := make([]string, 0, len(routes))
	for _, route := range routes {
		sub, subscribeErr := c.natsClient.SubscribeForRequests(
			ctx, route.subject, c.meteredCanonicalMutation(route),
		)
		if subscribeErr != nil {
			return fmt.Errorf("subscribe graph mutation %s: %w", route.operation, subscribeErr)
		}
		c.subscriptions = append(c.subscriptions, sub)
		subjects = append(subjects, route.subject)
	}

	c.logger.Info("mutation handlers registered", "subjects", subjects)
	return nil
}

type mutationHandler = func(context.Context, []byte) ([]byte, error)

// meteredMutation records typed rejections without altering the handler verdict.
func (c *Component) meteredMutation(subject string, handler mutationHandler) mutationHandler {
	return func(ctx context.Context, data []byte) ([]byte, error) {
		response, err := handler(ctx, data)
		if err == nil {
			return response, nil
		}
		c.recordPredicateContractRejections(subject, err)
		reason := graph.ErrorCodeInternal
		var classified *errs.ClassifiedError
		if errors.As(err, &classified) && classified.Code != "" {
			reason = classified.Code
		}
		c.recordMutationRejection(subject, reason, err.Error())
		return response, err
	}
}

func (c *Component) recordPredicateContractRejections(lane string, err error) {
	if c.predicateContractRejections == nil {
		return
	}
	var contractErr *graph.EntityPredicateContractError
	if !errors.As(err, &contractErr) {
		return
	}
	reasons := make(map[vocabulary.PredicateValidationReason]struct{}, len(contractErr.Violations))
	for _, violation := range contractErr.Violations {
		reasons[violation.Reason] = struct{}{}
	}
	for reason := range reasons {
		c.predicateContractRejections.WithLabelValues(lane, string(reason)).Inc()
	}
}

const (
	entityStateReasonObjectType = "object_type"
	contractReasonUnknown       = "unknown"
	contractFieldPredicate      = "predicate"
)

func (c *Component) recordEntityStateContractRejection(lane string, err error) {
	if c.entityStateContractRejections == nil {
		return
	}
	field, reason, _, ok := entityStateContractRejectionLabels(err)
	if ok {
		c.entityStateContractRejections.WithLabelValues(lane, field, reason).Inc()
	}
}

func entityStateContractRejectionLabels(err error) (field, reason string, tripleIndex int, ok bool) {
	var contractErr *graph.EntityStateContractError
	if !errors.As(err, &contractErr) {
		return "", "", -1, false
	}
	switch contractErr.Field {
	case graph.EntityStateContractFieldID, graph.EntityStateContractFieldSubject, graph.EntityStateContractFieldReference:
		field = string(contractErr.Field)
	default:
		return "", "", -1, false
	}
	reason = entityIDContractReason(contractErr.Err)
	if reason == contractReasonUnknown && contractErr.Field == graph.EntityStateContractFieldReference {
		reason = entityStateReasonObjectType
	}
	return field, reason, contractErr.TripleIndex, true
}

func entityIDContractReason(err error) string {
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		return contractReasonUnknown
	}
	reason, _ := classified.Detail[semtypes.EntityIDDetailReason].(string)
	switch reason {
	case semtypes.EntityIDReasonEmpty,
		semtypes.EntityIDReasonBytes,
		semtypes.EntityIDReasonArity,
		semtypes.EntityIDReasonEmptySegment,
		semtypes.EntityIDReasonFirstByte,
		semtypes.EntityIDReasonAlphabet:
		return reason
	default:
		return contractReasonUnknown
	}
}

func predicateContractReason(err error) (string, bool) {
	var contractErr *graph.EntityPredicateContractError
	if !errors.As(err, &contractErr) || len(contractErr.Violations) == 0 {
		return "", false
	}
	reason := contractErr.Violations[0].Reason
	switch reason {
	case vocabulary.PredicateReasonEmpty,
		vocabulary.PredicateReasonLength,
		vocabulary.PredicateReasonArity,
		vocabulary.PredicateReasonSegmentEmpty,
		vocabulary.PredicateReasonSegmentLength,
		vocabulary.PredicateReasonSegmentStart,
		vocabulary.PredicateReasonSegmentCharacter,
		vocabulary.PredicateReasonSegmentHyphen:
		return string(reason), true
	default:
		return contractReasonUnknown, true
	}
}

func (c *Component) recordMutationRejection(subject, reason, detail string) {
	if c.mutationRejections != nil {
		c.mutationRejections.WithLabelValues(subject, reason).Inc()
	}
	if c.logger != nil {
		c.logger.Warn("graph mutation rejected",
			slog.String("subject", subject),
			slog.String("reason", reason),
			slog.String("error", detail))
	}
}

func (c *Component) validateTriplePredicates(triples []message.Triple) error {
	for _, triple := range triples {
		if vocabulary.IsValidPredicate(triple.Predicate) {
			continue
		}
		return rejectInvalid(graph.ErrorCodeStructuralInvalid,
			fmt.Errorf("predicate %q is not a valid 3-part predicate (domain.category.property) on entity %q",
				triple.Predicate, triple.Subject))
	}
	return nil
}

// fetchEntityState reads one authority value and its same-entry revision.
func (c *Component) fetchEntityState(ctx context.Context, entityID string) (*graph.EntityState, uint64, error) {
	entry, err := c.entityBucket.Get(ctx, entityID)
	if err != nil {
		return nil, 0, err
	}
	if len(entry.Value) == 0 {
		return nil, 0, natsclient.ErrKVKeyNotFound
	}
	var state graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value, &state); err != nil {
		var contractErr *graph.StateContractError
		if errors.As(err, &contractErr) {
			contractErr.EntityID = entityID
			c.inventoryEntityPoison(ctx, contractErr, entry.Revision)
		}
		return nil, 0, fmt.Errorf("unmarshal entity state: %w", err)
	}
	c.clearEntityPoisonOnValidRead(entityID, entry.Revision)
	return &state, entry.Revision, nil
}

func rejectInvalid(code string, err error) error {
	return errs.ClassifiedCode(errs.ErrorInvalid, code, err)
}

func rejectInvalidDetail(code string, detail map[string]any, err error) error {
	return errs.ClassifiedCodeDetail(errs.ErrorInvalid, code, detail, err)
}

func rejectInternal(err error) error {
	return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, err)
}

func rejectFromError(err error) error {
	var stateErr *graph.StateContractError
	if errors.As(err, &stateErr) {
		return errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired, err)
	}
	if errs.IsInvalid(err) {
		return rejectInvalid(graph.ErrorCodeInvalidRequest, err)
	}
	return rejectInternal(err)
}

func rejectRevisionMismatch(detail map[string]any, err error) error {
	return errs.ClassifiedCodeDetail(errs.ErrorInvalid, graph.ErrorCodeRevisionMismatch, detail, err)
}

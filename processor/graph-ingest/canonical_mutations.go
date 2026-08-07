package graphingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
)

type canonicalMutationRoute struct {
	operation graphmutation.Operation
	subject   string
	handler   mutationHandler
}

func (c *Component) meteredCanonicalMutation(route canonicalMutationRoute) mutationHandler {
	metered := c.meteredMutation(route.subject, route.handler)
	return func(ctx context.Context, data []byte) ([]byte, error) {
		response, err := metered(ctx, data)
		if err != nil {
			outcome := canonicalErrorOutcome(err)
			c.recordCanonicalOutcome(route.operation, outcome)
			var classified *errs.ClassifiedError
			if errors.As(err, &classified) && classified.Code == graph.ErrorCodeRevisionMismatch && c.logger != nil {
				c.logger.Warn("graph mutation revision mismatch",
					slog.String("operation", string(route.operation)),
					slog.Any("entity_id", classified.Detail["entity"]),
					slog.Any("expected_revision", classified.Detail["expected_revision"]))
			}
			return response, err
		}
		if recordErr := c.recordCanonicalResponse(route.operation, response); recordErr != nil {
			c.recordCanonicalOutcome(route.operation, graph.ErrorCodeInternal)
			if c.logger != nil {
				c.logger.Warn("canonical mutation response could not be metered",
					slog.String("operation", string(route.operation)), slog.Any("error", recordErr))
			}
		}
		return response, nil
	}
}

func canonicalErrorOutcome(err error) string {
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		return graph.ErrorCodeInternal
	}
	switch classified.Code {
	case graph.ErrorCodeEntityNotFound:
		return string(graph.MutationEntityNotFound)
	case graph.ErrorCodeEntityExists:
		return string(graph.MutationEntityAlreadyExists)
	case graph.ErrorCodeRevisionMismatch:
		return string(graph.MutationRevisionMismatch)
	case graph.ErrorCodeInvalidRequest, graph.ErrorCodeStructuralInvalid:
		return string(graph.MutationInvalid)
	default:
		return graph.ErrorCodeInternal
	}
}

func (c *Component) recordCanonicalResponse(operation graphmutation.Operation, data []byte) error {
	switch operation {
	case graphmutation.CreateEntity:
		var response graph.CreateEntityResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return err
		}
		c.recordCanonicalOutcome(operation, string(response.Outcome))
	case graphmutation.ReconcilePredicates:
		var response graph.ReconcilePredicatesResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return err
		}
		c.recordCanonicalOutcome(operation, string(response.Outcome))
	case graphmutation.AppendTriples:
		var response graph.AppendTriplesResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return err
		}
		for _, result := range response.Results {
			outcome := string(result.Outcome)
			if result.Outcome == graph.MutationFailed {
				if result.Error == nil {
					return fmt.Errorf("failed append result for %q has no error", result.EntityID)
				}
				outcome = result.Error.Code
			}
			c.recordCanonicalOutcome(operation, outcome)
		}
	case graphmutation.DeleteEntity:
		var response graph.DeleteEntityResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return err
		}
		c.recordCanonicalOutcome(operation, string(response.Outcome))
	default:
		return fmt.Errorf("unknown canonical mutation operation %q", operation)
	}
	return nil
}

func (c *Component) recordCanonicalOutcome(operation graphmutation.Operation, outcome string) {
	if c.mutationOutcomes == nil {
		return
	}
	if outcome == "" {
		outcome = graph.ErrorCodeInternal
	}
	c.mutationOutcomes.WithLabelValues(string(operation), outcome).Inc()
}

func (c *Component) canonicalMutationRoutes() ([]canonicalMutationRoute, error) {
	provider, err := canonicalMutationProvider(c.config.Ports)
	if err != nil {
		return nil, err
	}

	operations := []struct {
		operation graphmutation.Operation
		handler   mutationHandler
	}{
		{graphmutation.CreateEntity, c.handleCanonicalCreate},
		{graphmutation.ReconcilePredicates, c.handleCanonicalReconcile},
		{graphmutation.AppendTriples, c.handleCanonicalAppend},
		{graphmutation.DeleteEntity, c.handleCanonicalDelete},
	}
	routes := make([]canonicalMutationRoute, 0, len(operations))
	for _, operation := range operations {
		subject, resolveErr := graphmutation.ResolveSubject(provider, operation.operation)
		if resolveErr != nil {
			return nil, resolveErr
		}
		routes = append(routes, canonicalMutationRoute{
			operation: operation.operation,
			subject:   subject,
			handler:   operation.handler,
		})
	}
	return routes, nil
}

func canonicalMutationProvider(ports *component.PortConfig) (string, error) {
	if ports == nil {
		return "", errors.New("graph mutation provider port is required")
	}
	var provider *string
	for _, definition := range ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return "", err
		}
		facts, err := port.Facts()
		if err != nil {
			return "", err
		}
		subjects := facts.NATSSubjects()
		contract, hasContract := facts.Interface()
		candidate := (len(subjects) == 1 && strings.HasPrefix(subjects[0], "graph.mutation.")) ||
			(hasContract && contract.Type == graphmutation.InterfaceType)
		if !candidate {
			continue
		}
		if provider != nil {
			return "", errors.New("graph mutation provider port is ambiguous")
		}
		if facts.Kind() != component.PortKindNATSRequest || !port.Required ||
			len(subjects) != 1 || subjects[0] != graphmutation.SubjectFamily || !hasContract ||
			contract.Type != graphmutation.InterfaceType || contract.Version != graphmutation.InterfaceVersion {
			return "", fmt.Errorf(
				"graph mutation provider must be a required %s %s nats-request input on %s",
				graphmutation.InterfaceType, graphmutation.InterfaceVersion, graphmutation.SubjectFamily,
			)
		}
		subject := subjects[0]
		provider = &subject
	}
	if provider == nil {
		return "", errors.New("graph mutation provider port is required")
	}
	return *provider, nil
}

func (c *Component) handleCanonicalCreate(ctx context.Context, data []byte) ([]byte, error) {
	var request graph.CreateEntityRequest
	if err := decodeCanonicalMutation(data, &request, "entity", "triples"); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}
	if request.Entity == nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("entity cannot be nil"))
	}
	if !request.Entity.MessageType.IsValid() {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("entity message_type must be valid"))
	}
	if len(request.Entity.Triples) != 0 {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest,
			errors.New("entity.triples is not admitted; use top-level triples"))
	}
	if request.IndexingProfile != "" && !vocabulary.IsValidIndexingProfile(request.IndexingProfile) {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest,
			fmt.Errorf("invalid indexing_profile %q", request.IndexingProfile))
	}

	entity := request.Entity.Clone()
	entity.Triples = append([]message.Triple(nil), request.Triples...)
	for index := range entity.Triples {
		if entity.Triples[index].Subject != entity.ID {
			return nil, rejectInvalid(graph.ErrorCodeInvalidRequest,
				fmt.Errorf("triple[%d] subject %q does not match entity %q",
					index, entity.Triples[index].Subject, entity.ID))
		}
	}
	if entity.Version == 0 {
		entity.Version = 1
	}
	if entity.UpdatedAt.IsZero() {
		entity.UpdatedAt = time.Now()
	}
	stampExplicitIndexingProfile(entity, request.IndexingProfile)
	c.reconcileIndexingProfile(entity)
	if err := graph.ValidateEntityStateContract(entity); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, err)
	}
	encoded, err := graph.MarshalEntityState(entity)
	if err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, err)
	}
	revision, err := c.entityBucket.Create(ctx, entity.ID, encoded)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyExists) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityExists,
				map[string]any{"entity": entity.ID}, fmt.Errorf("entity already exists: %s", entity.ID))
		}
		return nil, rejectFromError(err)
	}

	c.recordCanonicalEntityCommit(ctx, entity.ID, revision, len(encoded))
	c.updateSuffixIndex(ctx, entity.ID)
	return json.Marshal(graph.CreateEntityResponse{
		Outcome: graph.MutationApplied, Entity: entity.Clone(), KVRevision: revision,
		TraceID: request.TraceID, RequestID: request.RequestID,
	})
}

func (c *Component) handleCanonicalReconcile(ctx context.Context, data []byte) ([]byte, error) {
	var request graph.ReconcilePredicatesRequest
	if err := decodeCanonicalMutation(data, &request, "entity_id", "expected_revision", "predicates", "desired"); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}
	if err := validateEntityID(request.EntityID); err != nil {
		return nil, rejectFromError(err)
	}
	if request.ExpectedRevision == 0 {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("expected_revision must be nonzero"))
	}
	predicates, err := validateCanonicalReconcileRequest(request)
	if err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, err)
	}

	current, revision, err := c.fetchEntityState(ctx, request.EntityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound,
				map[string]any{"entity": request.EntityID}, err)
		}
		return nil, rejectFromError(err)
	}
	if revision != request.ExpectedRevision {
		return nil, rejectRevisionMismatch(
			map[string]any{"entity": request.EntityID, "expected_revision": request.ExpectedRevision, "current_revision": revision},
			fmt.Errorf("revision mismatch: expected %d, current %d", request.ExpectedRevision, revision))
	}

	desired := dedupeReconcileTriples(request.Desired)
	if selectedPredicatesEqual(current.Triples, desired, predicates) {
		return json.Marshal(graph.ReconcilePredicatesResponse{
			Outcome: graph.MutationUnchanged, Entity: current.Clone(), KVRevision: revision,
			TraceID: request.TraceID, RequestID: request.RequestID,
		})
	}

	candidate := current.Clone()
	candidate.Triples = reconcileSelectedPredicates(current.Triples, desired, predicates)
	candidate.Version++
	candidate.UpdatedAt = time.Now()
	encoded, err := graph.MarshalEntityState(candidate)
	if err != nil {
		return nil, rejectFromError(err)
	}
	committedRevision, err := c.entityBucket.Update(ctx, request.EntityID, encoded, request.ExpectedRevision)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			return nil, rejectRevisionMismatch(
				map[string]any{"entity": request.EntityID, "expected_revision": request.ExpectedRevision},
				fmt.Errorf("revision mismatch: concurrent modification of %s", request.EntityID))
		}
		if natsclient.IsKVNotFoundError(err) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound,
				map[string]any{"entity": request.EntityID}, err)
		}
		return nil, rejectFromError(err)
	}

	c.recordCanonicalEntityCommit(ctx, request.EntityID, committedRevision, len(encoded))
	return json.Marshal(graph.ReconcilePredicatesResponse{
		Outcome: graph.MutationApplied, Entity: candidate, KVRevision: committedRevision,
		TraceID: request.TraceID, RequestID: request.RequestID,
	})
}

func (c *Component) handleCanonicalAppend(ctx context.Context, data []byte) ([]byte, error) {
	var request graph.AppendTriplesRequest
	if err := decodeCanonicalMutation(data, &request, "triples"); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}
	if len(request.Triples) == 0 {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("triples cannot be empty"))
	}
	if err := validateCanonicalAppendRequest(request.Triples); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, err)
	}

	result, batchErr := c.addTriplesLane(ctx, request.Triples, dedupLaneAddBatch)
	subjects := distinctSortedSubjects(request.Triples)
	if accountingErr := validateCanonicalAppendAccounting(subjects, result); accountingErr != nil {
		if batchErr != nil {
			return nil, fmt.Errorf("%v: %w", accountingErr, batchErr)
		}
		return nil, rejectInternal(accountingErr)
	}
	response := graph.AppendTriplesResponse{
		Results: make([]graph.AppendSubjectResult, 0, len(subjects)),
		TraceID: request.TraceID, RequestID: request.RequestID,
	}
	for _, subject := range subjects {
		if revision, committed := result.CommittedRevisions[subject]; committed {
			response.Results = append(response.Results, graph.AppendSubjectResult{
				EntityID: subject, Outcome: graph.MutationApplied, KVRevision: revision,
			})
			continue
		}
		if _, absent := result.NotFoundSubjects[subject]; absent {
			response.Results = append(response.Results, graph.AppendSubjectResult{
				EntityID: subject, Outcome: graph.MutationEntityNotFound,
			})
			continue
		}
		if subjectErr, failed := result.SubjectErrors[subject]; failed {
			response.Results = append(response.Results, graph.AppendSubjectResult{
				EntityID: subject, Outcome: graph.MutationFailed, Error: canonicalAppendFailure(subjectErr),
			})
			continue
		}
		if _, unchanged := result.UnchangedSubjects[subject]; !unchanged {
			return nil, rejectInternal(fmt.Errorf("append subject %q lost its accounted outcome", subject))
		}
		// Duplicate-only is explicitly accounted as unchanged. Read only to attach
		// the current revision; readability never proves the operation ran.
		_, revision, readErr := c.fetchEntityState(ctx, subject)
		if readErr != nil {
			if natsclient.IsKVNotFoundError(readErr) {
				response.Results = append(response.Results, graph.AppendSubjectResult{
					EntityID: subject, Outcome: graph.MutationEntityNotFound,
				})
				continue
			}
			response.Results = append(response.Results, graph.AppendSubjectResult{
				EntityID: subject, Outcome: graph.MutationFailed, Error: canonicalAppendFailure(readErr),
			})
			continue
		}
		response.Results = append(response.Results, graph.AppendSubjectResult{
			EntityID: subject, Outcome: graph.MutationUnchanged, KVRevision: revision,
		})
	}
	return json.Marshal(response)
}

func validateCanonicalAppendAccounting(subjects []string, result addTriplesResult) error {
	for _, subject := range subjects {
		outcomes := 0
		if _, ok := result.CommittedRevisions[subject]; ok {
			outcomes++
		}
		if _, ok := result.UnchangedSubjects[subject]; ok {
			outcomes++
		}
		if _, ok := result.NotFoundSubjects[subject]; ok {
			outcomes++
		}
		if _, ok := result.SubjectErrors[subject]; ok {
			outcomes++
		}
		if outcomes != 1 {
			return fmt.Errorf("append subject %q has %d outcomes; want exactly one", subject, outcomes)
		}
	}
	return nil
}

func (c *Component) handleCanonicalDelete(ctx context.Context, data []byte) ([]byte, error) {
	var request graph.DeleteEntityRequest
	if err := decodeCanonicalMutation(data, &request, "entity_id", "expected_revision"); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}
	if err := validateEntityID(request.EntityID); err != nil {
		return nil, rejectFromError(err)
	}
	if request.ExpectedRevision == 0 {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("expected_revision must be nonzero"))
	}
	_, currentRevision, err := c.fetchEntityState(ctx, request.EntityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound,
				map[string]any{"entity": request.EntityID}, fmt.Errorf("entity not found: %s", request.EntityID))
		}
		return nil, rejectFromError(err)
	}
	if currentRevision != request.ExpectedRevision {
		return nil, rejectRevisionMismatch(
			map[string]any{"entity": request.EntityID, "expected_revision": request.ExpectedRevision, "current_revision": currentRevision},
			fmt.Errorf("revision mismatch: expected %d, current %d", request.ExpectedRevision, currentRevision))
	}
	if err := c.deleteEntityAtRevision(ctx, request.EntityID, request.ExpectedRevision); err != nil {
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			return nil, rejectRevisionMismatch(
				map[string]any{"entity": request.EntityID, "expected_revision": request.ExpectedRevision},
				fmt.Errorf("revision mismatch deleting %s at %d", request.EntityID, request.ExpectedRevision))
		}
		if natsclient.IsKVNotFoundError(err) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound,
				map[string]any{"entity": request.EntityID}, fmt.Errorf("entity not found: %s", request.EntityID))
		}
		return nil, rejectInternal(err)
	}
	return json.Marshal(graph.DeleteEntityResponse{
		EntityID: request.EntityID, Outcome: graph.MutationApplied,
		ExpectedRevision: request.ExpectedRevision, TraceID: request.TraceID, RequestID: request.RequestID,
	})
}

func decodeCanonicalMutation(data []byte, target any, requiredFields ...string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not admitted")
		}
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, field := range requiredFields {
		if _, exists := fields[field]; !exists {
			return fmt.Errorf("required field %q is missing", field)
		}
	}
	return nil
}

func canonicalAppendFailure(err error) *graph.MutationFailure {
	classified := rejectFromError(err)
	var typed *errs.ClassifiedError
	if errors.As(classified, &typed) {
		return &graph.MutationFailure{Class: typed.Class.String(), Code: typed.Code}
	}
	return &graph.MutationFailure{Class: errs.ErrorTransient.String(), Code: graph.ErrorCodeInternal}
}

func validateCanonicalReconcileRequest(request graph.ReconcilePredicatesRequest) (map[string]struct{}, error) {
	if len(request.Predicates) == 0 {
		return nil, errors.New("predicates cannot be empty")
	}
	predicates := make(map[string]struct{}, len(request.Predicates))
	for _, predicate := range request.Predicates {
		if !vocabulary.IsValidPredicate(predicate) {
			return nil, fmt.Errorf("predicate %q is structurally invalid", predicate)
		}
		predicates[predicate] = struct{}{}
	}
	for index := range request.Desired {
		triple := request.Desired[index]
		if triple.Subject != request.EntityID {
			return nil, fmt.Errorf("desired triple[%d] subject %q does not match entity %q",
				index, triple.Subject, request.EntityID)
		}
		if _, ok := predicates[triple.Predicate]; !ok {
			return nil, fmt.Errorf("desired triple[%d] predicate %q is outside reconciled set",
				index, triple.Predicate)
		}
	}
	if err := graph.ValidateEntityStateContract(&graph.EntityState{
		ID: request.EntityID, Triples: request.Desired,
	}); err != nil {
		return nil, err
	}
	return predicates, nil
}

func validateCanonicalAppendRequest(triples []message.Triple) error {
	bySubject := make(map[string][]message.Triple)
	for _, triple := range triples {
		bySubject[triple.Subject] = append(bySubject[triple.Subject], triple)
	}
	for subject, group := range bySubject {
		if err := graph.ValidateEntityStateContract(&graph.EntityState{ID: subject, Triples: group}); err != nil {
			return err
		}
	}
	return nil
}

func selectedPredicatesEqual(current, desired []message.Triple, predicates map[string]struct{}) bool {
	counts := make([]fullTripleCount, 0, len(desired))
	currentCount := 0
	for _, triple := range current {
		if _, selected := predicates[triple.Predicate]; selected {
			counts = countsFullTriple(counts, triple)
			currentCount++
		}
	}
	if currentCount != len(desired) {
		return false
	}
	for _, triple := range desired {
		found := -1
		for index := range counts {
			if counts[index].count > 0 && sameReconcileTriple(counts[index].triple, triple) {
				found = index
				break
			}
		}
		if found < 0 {
			return false
		}
		counts[found].count--
	}
	return true
}

type fullTripleCount struct {
	triple message.Triple
	count  int
}

func countsFullTriple(counts []fullTripleCount, triple message.Triple) []fullTripleCount {
	for index := range counts {
		if sameReconcileTriple(counts[index].triple, triple) {
			counts[index].count++
			return counts
		}
	}
	return append(counts, fullTripleCount{triple: triple, count: 1})
}

func sameReconcileTriple(left, right message.Triple) bool {
	if !message.SameAppendTuple(left, right) ||
		left.Confidence != right.Confidence ||
		!left.Timestamp.Equal(right.Timestamp) {
		return false
	}
	if left.ExpiresAt == nil || right.ExpiresAt == nil {
		return left.ExpiresAt == nil && right.ExpiresAt == nil
	}
	return left.ExpiresAt.Equal(*right.ExpiresAt)
}

func dedupeReconcileTriples(incoming []message.Triple) []message.Triple {
	if len(incoming) == 0 {
		return nil
	}
	result := make([]message.Triple, 0, len(incoming))
	for _, candidate := range incoming {
		duplicate := false
		for _, existing := range result {
			if sameReconcileTriple(existing, candidate) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, candidate)
		}
	}
	return result
}

func reconcileSelectedPredicates(current, desired []message.Triple, predicates map[string]struct{}) []message.Triple {
	result := make([]message.Triple, 0, len(current)+len(desired))
	for _, triple := range current {
		if _, selected := predicates[triple.Predicate]; !selected {
			result = append(result, triple)
		}
	}
	return append(result, desired...)
}

func distinctSortedSubjects(triples []message.Triple) []string {
	seen := make(map[string]struct{}, len(triples))
	for _, triple := range triples {
		seen[triple.Subject] = struct{}{}
	}
	subjects := make([]string, 0, len(seen))
	for subject := range seen {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	return subjects
}

func (c *Component) recordCanonicalEntityCommit(ctx context.Context, entityID string, revision uint64, bytesWritten int) {
	c.clearEntityPoisonOnCommit(ctx, entityID, revision)
	c.invalidateEntityCacheEntry(entityID)
	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(bytesWritten))
	c.lastActivity.Store(time.Now())
	c.entitiesUpdated.Inc()
}

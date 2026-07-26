package projection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/ownership"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

const (
	subjectCreateWithTriples = "graph.mutation.entity.create_with_triples"
	subjectUpdateWithTriples = "graph.mutation.entity.update_with_triples"
	subjectAddTriplesBatch   = "graph.mutation.triple.add_batch"
	subjectQueryEntity       = "graph.ingest.query.entity"
)

type mutationRequester interface {
	RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error)
	RequestWithRetryClassified(
		context.Context,
		string,
		[]byte,
		time.Duration,
		natsclient.RetryConfig,
	) ([]byte, error)
}

type contractBinding struct {
	contract        Contract
	primaryModes    map[string]ownership.WriteMode
	birthPredicates map[string]struct{}
	namedGroups     map[string]predicateGroupBinding
	replaceGroups   []predicateGroupBinding
}

type predicateGroupBinding struct {
	name       string
	mode       ownership.WriteMode
	predicates []string
	set        map[string]struct{}
}

// MutationClient is an immutable, concurrency-safe contract-bound graph writer.
type MutationClient struct {
	rpc       mutationRequester
	token     ownership.OwnerToken
	contracts map[string]contractBinding
	timeout   time.Duration
	retry     natsclient.RetryConfig
	retryWait func(context.Context, natsclient.RetryConfig, int) error
}

// BindMutationClient validates and copies the complete contract set before
// registering its ownership and liveness. The returned client never exposes or
// refreshes the minted owner token.
func BindMutationClient(ctx context.Context, cfg MutationClientConfig) (*MutationClient, error) {
	if cfg.NATS == nil {
		return nil, invalidMutationError(MutationOperationBind, errors.New("NATS client is required"))
	}
	if strings.TrimSpace(cfg.Owner) == "" {
		return nil, invalidMutationError(MutationOperationBind, errors.New("owner is required"))
	}
	contracts, err := buildContractIndex(cfg.Contracts)
	if err != nil {
		return nil, invalidMutationError(MutationOperationBind, err)
	}
	if cfg.Heartbeater == nil && contractsRequireHeartbeat(contracts) {
		return nil, invalidMutationError(
			MutationOperationBind,
			errors.New("heartbeater is required for owning projection contracts"),
		)
	}
	if cfg.Registry == nil && contractsRequireRegistration(contracts) {
		return nil, invalidMutationError(MutationOperationBind, errors.New("ownership registry is required"))
	}
	copies := make([]Contract, 0, len(contracts))
	names := make([]string, 0, len(contracts))
	for name := range contracts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		copies = append(copies, cloneContract(contracts[name].contract))
	}

	token, err := BindAndHeartbeat(ctx, cfg.Registry, cfg.Heartbeater, cfg.Owner, copies...)
	if err != nil {
		return nil, newMutationError(MutationOperationBind, err, CommitNotCommitted)
	}
	return &MutationClient{
		rpc:       cfg.NATS,
		token:     token,
		contracts: contracts,
		timeout:   normalizeMutationTimeout(cfg.Timeout),
		retry:     normalizeRetryConfig(cfg.Retry),
		retryWait: waitMutationRetry,
	}, nil
}

func contractsRequireHeartbeat(contracts map[string]contractBinding) bool {
	for _, binding := range contracts {
		if bindingHasOwningPredicates(binding) {
			return true
		}
	}
	return false
}

func contractsRequireRegistration(contracts map[string]contractBinding) bool {
	for _, binding := range contracts {
		if len(binding.contract.Groups) != 0 || len(binding.contract.ForeignEdges) != 0 {
			return true
		}
	}
	return false
}

func bindingHasOwningPredicates(binding contractBinding) bool {
	for _, mode := range binding.primaryModes {
		if mode == ownership.ModeReplaceOwned || mode == ownership.ModeCASTransition {
			return true
		}
	}
	return false
}

func bindingCanCreate(binding contractBinding) bool {
	return len(binding.birthPredicates) != 0 || bindingHasOwningPredicates(binding)
}

func newMutationClient(
	rpc mutationRequester,
	token ownership.OwnerToken,
	contracts []Contract,
	timeout time.Duration,
	retry natsclient.RetryConfig,
) (*MutationClient, error) {
	if rpc == nil {
		return nil, errors.New("mutation requester is required")
	}
	index, err := buildContractIndex(contracts)
	if err != nil {
		return nil, err
	}
	return &MutationClient{
		rpc:       rpc,
		token:     token,
		contracts: index,
		timeout:   normalizeMutationTimeout(timeout),
		retry:     normalizeRetryConfig(retry),
		retryWait: waitMutationRetry,
	}, nil
}

func normalizeMutationTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return natsclient.DefaultRequestTimeout
	}
	return timeout
}

func normalizeRetryConfig(retry natsclient.RetryConfig) natsclient.RetryConfig {
	if retry.MaxRetries < 0 {
		retry.MaxRetries = 0
	}
	if retry.BackoffMultiplier <= 0 {
		retry.BackoffMultiplier = 1
	}
	if retry.InitialBackoff < 0 {
		retry.InitialBackoff = 0
	}
	if retry.MaxBackoff < 0 {
		retry.MaxBackoff = 0
	}
	return retry
}

func buildContractIndex(contracts []Contract) (map[string]contractBinding, error) {
	if len(contracts) == 0 {
		return nil, fmt.Errorf("%w: mutation client has no contracts", ErrInvalidContract)
	}
	copies := make([]Contract, len(contracts))
	for index := range contracts {
		copies[index] = cloneContract(contracts[index])
	}
	if _, err := Derive(validationOwner, copies...); err != nil {
		return nil, err
	}
	index := make(map[string]contractBinding, len(contracts))
	for _, contract := range copies {
		if _, duplicate := index[contract.Name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate mutation contract %q", ErrInvalidContract, contract.Name)
		}
		binding := contractBinding{
			contract:        contract,
			primaryModes:    make(map[string]ownership.WriteMode),
			birthPredicates: make(map[string]struct{}, len(contract.BirthPredicates)),
			namedGroups:     make(map[string]predicateGroupBinding),
		}
		for _, predicate := range contract.BirthPredicates {
			binding.birthPredicates[predicate] = struct{}{}
		}
		for _, group := range contract.Groups {
			groupBinding := predicateGroupBinding{
				name:       group.Name,
				mode:       group.Mode,
				predicates: append([]string(nil), group.Predicates...),
				set:        make(map[string]struct{}, len(group.Predicates)),
			}
			for _, predicate := range group.Predicates {
				binding.primaryModes[predicate] = group.Mode
				groupBinding.set[predicate] = struct{}{}
			}
			sort.Strings(groupBinding.predicates)
			if group.Name != "" {
				binding.namedGroups[group.Name] = groupBinding
			}
			if group.Mode == ownership.ModeReplaceOwned {
				binding.replaceGroups = append(binding.replaceGroups, groupBinding)
			}
		}
		index[contract.Name] = binding
	}
	return index, nil
}

func cloneContract(contract Contract) Contract {
	clone := contract
	clone.Groups = make([]PredicateGroup, len(contract.Groups))
	for index := range contract.Groups {
		clone.Groups[index] = contract.Groups[index]
		clone.Groups[index].Predicates = append([]string(nil), contract.Groups[index].Predicates...)
	}
	clone.ForeignEdges = append([]ForeignEdge(nil), contract.ForeignEdges...)
	clone.BirthPredicates = append([]string(nil), contract.BirthPredicates...)
	return clone
}

func (c *MutationClient) binding(operation MutationOperation, name string) (contractBinding, error) {
	if c == nil {
		return contractBinding{}, invalidMutationError(operation, errors.New("mutation client is nil"))
	}
	binding, ok := c.contracts[name]
	if !ok {
		return contractBinding{}, invalidMutationError(operation, fmt.Errorf("unknown projection contract %q", name))
	}
	return binding, nil
}

type canonicalCreate struct {
	binding    contractBinding
	entity     *graph.EntityState
	triples    []message.Triple
	metadata   MutationMetadata
	ownerToken string
}

func (c *MutationClient) canonicalizeCreate(req CreateMutation) (canonicalCreate, error) {
	const operation = MutationOperationCreate
	binding, err := c.binding(operation, req.Contract)
	if err != nil {
		return canonicalCreate{}, err
	}
	if !bindingCanCreate(binding) {
		return canonicalCreate{}, invalidMutationError(
			operation,
			fmt.Errorf("contract %q has no create-authorized predicates", binding.contract.Name),
		)
	}
	if req.Entity == nil {
		return canonicalCreate{}, invalidMutationError(operation, errors.New("entity is required"))
	}
	if len(req.Entity.Triples) != 0 {
		return canonicalCreate{}, invalidMutationError(
			operation,
			errors.New("entity must not contain triples; use CreateMutation.Triples as the sole birth-fact source"),
		)
	}
	entity := req.Entity.Clone()
	entity.Triples = nil
	if err := validateEntityForContract(binding.contract, entity); err != nil {
		return canonicalCreate{}, invalidMutationError(operation, err)
	}
	triples, metadata, err := canonicalizeTriples(operation, req.Triples, req.Metadata, true)
	if err != nil {
		return canonicalCreate{}, err
	}
	if err := validateCreateTriples(binding, entity, triples); err != nil {
		return canonicalCreate{}, invalidMutationError(operation, err)
	}
	entity.Triples = append([]message.Triple(nil), triples...)
	if err := graph.ValidateEntityStateContract(entity); err != nil {
		return canonicalCreate{}, invalidMutationError(operation, err)
	}
	entity.Triples = nil

	ownerToken := ""
	for _, triple := range triples {
		if triple.Subject != entity.ID {
			continue
		}
		mode := binding.primaryModes[triple.Predicate]
		if mode == ownership.ModeReplaceOwned || mode == ownership.ModeCASTransition {
			ownerToken = c.token.Wire()
			break
		}
	}
	return canonicalCreate{
		binding: binding, entity: entity, triples: triples, metadata: metadata, ownerToken: ownerToken,
	}, nil
}

func validateEntityForContract(contract Contract, entity *graph.EntityState) error {
	if err := semtypes.ValidateEntityID(entity.ID); err != nil {
		return fmt.Errorf("entity ID: %w", err)
	}
	matches, err := semtypes.MatchEntityIDPattern(contract.EntityPattern, entity.ID)
	if err != nil {
		return fmt.Errorf("entity pattern: %w", err)
	}
	if !matches {
		return fmt.Errorf("entity %q is outside contract %q pattern", entity.ID, contract.Name)
	}
	if !entity.MessageType.IsValid() {
		return errors.New("entity message type is required")
	}
	if contract.MessageType != "" && entity.MessageType.Key() != contract.MessageType {
		return fmt.Errorf(
			"entity message type %q does not match contract %q",
			entity.MessageType.Key(),
			contract.MessageType,
		)
	}
	return nil
}

func validateCreateTriples(binding contractBinding, entity *graph.EntityState, triples []message.Triple) error {
	for _, triple := range triples {
		if triple.Subject != entity.ID {
			return fmt.Errorf(
				"create triple subject %q must equal primary entity %q",
				triple.Subject,
				entity.ID,
			)
		}
		if _, birth := binding.birthPredicates[triple.Predicate]; birth {
			continue
		}
		mode := binding.primaryModes[triple.Predicate]
		if mode != ownership.ModeReplaceOwned && mode != ownership.ModeCASTransition {
			return fmt.Errorf("predicate %q is not create-authorized by contract %q",
				triple.Predicate, binding.contract.Name)
		}
	}
	return nil
}

func canonicalizeTriples(
	operation MutationOperation,
	input []message.Triple,
	metadata MutationMetadata,
	required bool,
) ([]message.Triple, MutationMetadata, error) {
	if required && strings.TrimSpace(metadata.RequestID) == "" {
		return nil, MutationMetadata{}, invalidMutationError(operation, errors.New("request ID is required"))
	}
	if required && strings.TrimSpace(metadata.Source) == "" {
		return nil, MutationMetadata{}, invalidMutationError(operation, errors.New("source is required"))
	}
	timestampWasZero := metadata.Timestamp.IsZero()
	if timestampWasZero {
		metadata.Timestamp = time.Now().UTC()
	}

	triples := append([]message.Triple(nil), input...)
	for index := range triples {
		triple := &triples[index]
		if triple.Source != "" && metadata.Source != "" && triple.Source != metadata.Source {
			return nil, MutationMetadata{}, invalidMutationError(
				operation,
				fmt.Errorf("triple[%d] source conflicts with mutation metadata", index),
			)
		}
		if triple.Context != "" && metadata.RequestID != "" && triple.Context != metadata.RequestID {
			return nil, MutationMetadata{}, invalidMutationError(
				operation,
				fmt.Errorf("triple[%d] context conflicts with mutation metadata", index),
			)
		}
		if !triple.Timestamp.IsZero() &&
			!timestampWasZero &&
			!triple.Timestamp.Equal(metadata.Timestamp) {
			return nil, MutationMetadata{}, invalidMutationError(
				operation,
				fmt.Errorf("triple[%d] timestamp conflicts with mutation metadata", index),
			)
		}
		if triple.Source == "" {
			triple.Source = metadata.Source
		}
		if triple.Context == "" {
			triple.Context = metadata.RequestID
		}
		if triple.Timestamp.IsZero() {
			triple.Timestamp = metadata.Timestamp
		}
		if triple.ExpiresAt != nil {
			expiresAt := *triple.ExpiresAt
			triple.ExpiresAt = &expiresAt
		}
	}
	return triples, metadata, nil
}

func invalidMutationError(operation MutationOperation, err error) *MutationError {
	return &MutationError{
		Operation: operation,
		Kind:      MutationInvalid,
		Class:     errs.ErrorInvalid,
		Commit:    CommitNotCommitted,
		Err:       err,
	}
}

func newMutationError(operation MutationOperation, err error, commit CommitState) *MutationError {
	if err == nil {
		return nil
	}
	mapped := &MutationError{
		Operation: operation,
		Kind:      MutationUnavailable,
		Class:     errs.Classify(err),
		Commit:    commit,
		Err:       err,
	}
	if errors.Is(err, ErrInvalidContract) {
		mapped.Kind = MutationInvalid
		mapped.Class = errs.ErrorInvalid
		return mapped
	}
	if errors.Is(err, ownership.ErrOwnershipOverlap) {
		mapped.Kind = MutationConflict
		mapped.Class = errs.ErrorInvalid
		return mapped
	}
	var classified *errs.ClassifiedError
	if errors.As(err, &classified) {
		mapped.Code = classified.Code
		mapped.Class = classified.Class
		if classified.Detail != nil {
			mapped.Detail = make(map[string]any, len(classified.Detail))
			for key, value := range classified.Detail {
				mapped.Detail[key] = value
			}
		}
		switch classified.Code {
		case graph.ErrorCodeInvalidRequest, graph.ErrorCodeStructuralInvalid:
			mapped.Kind = MutationInvalid
		case graph.ErrorCodeEntityNotFound:
			mapped.Kind = MutationNotFound
		case graph.ErrorCodeEntityExists:
			mapped.Kind = MutationConflict
		case graph.ErrorCodeRevisionMismatch:
			mapped.Kind = MutationRevisionConflict
		case graph.ErrorCodeOwnerLeaseStale:
			mapped.Kind = MutationStaleOwnerToken
		case graph.ErrorCodeGraphStateResetRequired:
			mapped.Kind = MutationInternal
		case graph.ErrorCodeInternal:
			if classified.Class == errs.ErrorTransient {
				mapped.Kind = MutationUnavailable
			} else {
				mapped.Kind = MutationInternal
			}
		default:
			switch classified.Class {
			case errs.ErrorInvalid:
				mapped.Kind = MutationInvalid
			case errs.ErrorFatal:
				mapped.Kind = MutationInternal
			default:
				mapped.Kind = MutationUnavailable
			}
		}
		return mapped
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errs.IsTransient(err) {
		mapped.Kind = MutationUnavailable
		return mapped
	}
	if errs.IsFatal(err) {
		mapped.Kind = MutationInternal
	}
	return mapped
}

// CreateWithTriples atomically creates an entity with the complete initial
// facts authorized by the named projection contract.
func (c *MutationClient) CreateWithTriples(
	ctx context.Context,
	req CreateMutation,
) (MutationReceipt, error) {
	canonical, err := c.canonicalizeCreate(req)
	if err != nil {
		return MutationReceipt{Commit: CommitNotCommitted}, err
	}
	wire := graph.CreateEntityWithTriplesRequest{
		Entity:          canonical.entity,
		Triples:         canonical.triples,
		IndexingProfile: canonical.binding.contract.IndexingProfile,
		OwnerToken:      canonical.ownerToken,
		TraceID:         canonical.metadata.TraceID,
		RequestID:       canonical.metadata.RequestID,
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return MutationReceipt{Commit: CommitNotCommitted},
			invalidMutationError(MutationOperationCreate, fmt.Errorf("marshal request: %w", err))
	}

	for attempt := 0; attempt <= c.retry.MaxRetries; attempt++ {
		responseData, requestErr := c.rpc.RequestClassified(
			ctx, subjectCreateWithTriples, data, c.timeout,
		)
		if requestErr == nil {
			var response graph.CreateEntityWithTriplesResponse
			if err := json.Unmarshal(responseData, &response); err != nil {
				return committedUnverified(
					MutationOperationCreate,
					MutationReceipt{Commit: CommitCommitted},
					fmt.Errorf("decode create response: %w", err),
				)
			}
			receipt := MutationReceipt{
				Entity: response.Entity, KVRevision: response.KVRevision,
				Commit: CommitCommitted, Degraded: response.Degraded,
			}
			if !response.Degraded && response.Entity != nil {
				if err := graph.ValidateDecodedEntityState(response.Entity); err == nil &&
					createFactsMatch(canonical, response.Entity) {
					receipt.Entity = response.Entity.Clone()
					receipt.Commit = CommitVerified
					return receipt, nil
				}
			}
			return c.verifyCreate(ctx, canonical, receipt)
		}

		if isClassified(requestErr) {
			var classified *errs.ClassifiedError
			if errors.As(requestErr, &classified) && classified.Code == graph.ErrorCodeEntityExists {
				entity, readErr := c.ReadAuthoritative(ctx, canonical.entity.ID)
				if readErr == nil && createFactsMatch(canonical, entity) {
					return MutationReceipt{Entity: entity, Commit: CommitVerified}, nil
				}
				if readErr == nil {
					return MutationReceipt{Commit: CommitNotCommitted},
						newMutationError(MutationOperationCreate, requestErr, CommitNotCommitted)
				}
			}
			return MutationReceipt{Commit: CommitNotCommitted},
				newMutationError(MutationOperationCreate, requestErr, CommitNotCommitted)
		}

		if ctx.Err() != nil {
			return MutationReceipt{Commit: CommitUnknown},
				commitUnknown(MutationOperationCreate, requestErr)
		}
		entity, readErr := c.ReadAuthoritative(ctx, canonical.entity.ID)
		if readErr == nil {
			if createFactsMatch(canonical, entity) {
				return MutationReceipt{Entity: entity, Commit: CommitVerified}, nil
			}
			return MutationReceipt{Commit: CommitNotCommitted},
				&MutationError{
					Operation: MutationOperationCreate, Kind: MutationConflict,
					Class: errs.ErrorInvalid, Commit: CommitNotCommitted,
					Err: errors.New("authoritative entity conflicts with requested creation"),
				}
		}
		if !isNotFound(readErr) {
			return MutationReceipt{Commit: CommitUnknown},
				commitUnknown(MutationOperationCreate, requestErr)
		}
		if attempt == c.retry.MaxRetries {
			return MutationReceipt{Commit: CommitNotCommitted},
				newMutationError(MutationOperationCreate, requestErr, CommitNotCommitted)
		}
		if err := c.retryWait(ctx, c.retry, attempt); err != nil {
			return MutationReceipt{Commit: CommitNotCommitted},
				newMutationError(MutationOperationCreate, err, CommitNotCommitted)
		}
	}
	panic("unreachable")
}

// ReplaceOwned reconciles one complete replace-owned predicate group on an
// existing entity while preserving facts outside that group.
func (c *MutationClient) ReplaceOwned(
	ctx context.Context,
	req ReplaceOwnedMutation,
) (MutationReceipt, error) {
	_, group, desired, metadata, err := c.canonicalizeReplace(req)
	if err != nil {
		return MutationReceipt{Commit: CommitNotCommitted}, err
	}
	wire := graph.UpdateEntityWithTriplesRequest{
		Entity:        &graph.EntityState{ID: req.EntityID},
		AddTriples:    desired,
		RemoveTriples: append([]string(nil), group.predicates...),
		OwnerToken:    c.token.Wire(),
		TraceID:       metadata.TraceID,
		RequestID:     metadata.RequestID,
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return MutationReceipt{Commit: CommitNotCommitted},
			invalidMutationError(MutationOperationReplaceOwned, fmt.Errorf("marshal request: %w", err))
	}
	responseData, err := c.rpc.RequestWithRetryClassified(
		ctx, subjectUpdateWithTriples, data, c.timeout, c.retry,
	)
	if err != nil {
		if !isClassified(err) {
			return MutationReceipt{Commit: CommitUnknown},
				commitUnknown(MutationOperationReplaceOwned, err)
		}
		return MutationReceipt{Commit: CommitNotCommitted},
			newMutationError(MutationOperationReplaceOwned, err, CommitNotCommitted)
	}
	var response graph.UpdateEntityWithTriplesResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		return committedUnverified(
			MutationOperationReplaceOwned,
			MutationReceipt{Commit: CommitCommitted},
			fmt.Errorf("decode replace response: %w", err),
		)
	}
	receipt := MutationReceipt{
		Entity: response.Entity, KVRevision: response.KVRevision,
		Commit: CommitCommitted, Degraded: response.Degraded,
	}
	if !response.Degraded && response.Entity != nil {
		if err := graph.ValidateDecodedEntityState(response.Entity); err == nil &&
			ownedFactsMatch(group, desired, response.Entity) {
			receipt.Entity = response.Entity.Clone()
			receipt.Commit = CommitVerified
			return receipt, nil
		}
	}
	entity, readErr := c.ReadAuthoritative(ctx, req.EntityID)
	if readErr != nil || !ownedFactsMatch(group, desired, entity) {
		if readErr == nil {
			readErr = errors.New("authoritative owned facts do not match requested replacement")
		}
		return committedUnverified(MutationOperationReplaceOwned, receipt, readErr)
	}
	receipt.Entity = entity
	receipt.Commit = CommitVerified
	return receipt, nil
}

// AppendEvidence appends contract-authorized evidence to an existing entity
// with ambiguity-safe authoritative readback.
func (c *MutationClient) AppendEvidence(
	ctx context.Context,
	req AppendEvidenceMutation,
) (MutationReceipt, error) {
	evidence, metadata, err := c.canonicalizeAppend(req)
	if err != nil {
		return MutationReceipt{Commit: CommitNotCommitted}, err
	}
	wire := graph.AddTriplesBatchRequest{
		Triples: evidence, TraceID: metadata.TraceID, RequestID: metadata.RequestID,
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return MutationReceipt{Commit: CommitNotCommitted},
			invalidMutationError(MutationOperationAppendEvidence, fmt.Errorf("marshal request: %w", err))
	}
	for attempt := 0; attempt <= c.retry.MaxRetries; attempt++ {
		responseData, requestErr := c.rpc.RequestClassified(
			ctx, subjectAddTriplesBatch, data, c.timeout,
		)
		if requestErr == nil {
			var response graph.AddTriplesBatchResponse
			if err := json.Unmarshal(responseData, &response); err != nil {
				return c.verifyAnomalousAppend(
					ctx,
					req.EntityID,
					evidence,
					fmt.Errorf("decode append response: %w", err),
				)
			}
			receipt := MutationReceipt{
				KVRevision: response.KVRevision,
				Commit:     CommitCommitted,
				Degraded:   response.Degraded,
			}
			if response.Degraded {
				return c.verifyCommittedAppend(ctx, evidence, receipt)
			}
			requestedFailure, validFailure, anomaly := classifyAppendResponse(
				response,
				req.EntityID,
				len(evidence),
			)
			if validFailure {
				return c.resolveRequestedAppendFailure(
					ctx,
					req.EntityID,
					requestedFailure,
				)
			}
			if anomaly != nil {
				return c.verifyAnomalousAppend(
					ctx,
					req.EntityID,
					evidence,
					anomaly,
				)
			}
			return receipt, nil
		}
		if isClassified(requestErr) {
			return MutationReceipt{Commit: CommitNotCommitted},
				newMutationError(MutationOperationAppendEvidence, requestErr, CommitNotCommitted)
		}
		if ctx.Err() != nil {
			return MutationReceipt{Commit: CommitUnknown},
				commitUnknown(MutationOperationAppendEvidence, requestErr)
		}
		entity, readErr := c.ReadAuthoritative(ctx, req.EntityID)
		if readErr == nil && appendFactsPresent(evidence, entity) {
			return MutationReceipt{Entity: entity, Commit: CommitVerified}, nil
		}
		if readErr != nil && !isNotFound(readErr) {
			return MutationReceipt{Commit: CommitUnknown},
				commitUnknown(MutationOperationAppendEvidence, requestErr)
		}
		if attempt == c.retry.MaxRetries {
			return MutationReceipt{Commit: CommitNotCommitted},
				newMutationError(MutationOperationAppendEvidence, requestErr, CommitNotCommitted)
		}
		if err := c.retryWait(ctx, c.retry, attempt); err != nil {
			return MutationReceipt{Commit: CommitNotCommitted},
				newMutationError(MutationOperationAppendEvidence, err, CommitNotCommitted)
		}
	}
	panic("unreachable")
}

// ReadAuthoritative returns the current graph-ingest source-of-truth state for
// entityID.
func (c *MutationClient) ReadAuthoritative(ctx context.Context, entityID string) (*graph.EntityState, error) {
	if c == nil || c.rpc == nil {
		return nil, invalidMutationError(MutationOperationReadAuthoritative, errors.New("mutation client is nil"))
	}
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return nil, invalidMutationError(MutationOperationReadAuthoritative, fmt.Errorf("entity ID: %w", err))
	}
	data, err := json.Marshal(struct {
		ID string `json:"id"`
	}{ID: entityID})
	if err != nil {
		return nil, invalidMutationError(MutationOperationReadAuthoritative, err)
	}
	responseData, err := c.rpc.RequestClassified(ctx, subjectQueryEntity, data, c.timeout)
	if err != nil {
		return nil, newMutationError(MutationOperationReadAuthoritative, err, CommitNotCommitted)
	}
	var entity graph.EntityState
	if err := graph.UnmarshalEntityState(responseData, &entity); err != nil {
		return nil, newMutationError(MutationOperationReadAuthoritative, err, CommitNotCommitted)
	}
	if entity.ID != entityID {
		return nil, newMutationError(
			MutationOperationReadAuthoritative,
			fmt.Errorf("query returned entity %q for %q", entity.ID, entityID),
			CommitNotCommitted,
		)
	}
	return entity.Clone(), nil
}

func classifyAppendResponse(
	response graph.AddTriplesBatchResponse,
	entityID string,
	expectedCount int,
) (requestedFailure string, validFailure bool, anomaly error) {
	for subject := range response.FailedSubjects {
		if subject != entityID {
			return "", false, fmt.Errorf(
				"add-batch response reported unrequested failed subject %q",
				subject,
			)
		}
	}
	requestedFailure, failed := response.FailedSubjects[entityID]
	if failed && len(response.FailedSubjects) == 1 && response.WrittenCount == 0 {
		return requestedFailure, true, nil
	}
	if len(response.FailedSubjects) != 0 {
		return "", false, fmt.Errorf(
			"add-batch response combined failed subject with written count %d",
			response.WrittenCount,
		)
	}
	if response.WrittenCount != expectedCount {
		return "", false, fmt.Errorf(
			"add-batch response wrote %d of %d requested triples",
			response.WrittenCount,
			expectedCount,
		)
	}
	return "", false, nil
}

func (c *MutationClient) verifyAnomalousAppend(
	ctx context.Context,
	entityID string,
	evidence []message.Triple,
	anomaly error,
) (MutationReceipt, error) {
	entity, readErr := c.ReadAuthoritative(ctx, entityID)
	if readErr == nil && appendFactsPresent(evidence, entity) {
		return MutationReceipt{Entity: entity, Commit: CommitVerified}, nil
	}
	if readErr == nil || isNotFound(readErr) {
		if readErr != nil {
			anomaly = errors.Join(anomaly, readErr)
		}
		return MutationReceipt{Commit: CommitNotCommitted}, &MutationError{
			Operation: MutationOperationAppendEvidence,
			Kind:      MutationInternal,
			Class:     errs.ErrorFatal,
			Commit:    CommitNotCommitted,
			Err:       anomaly,
		}
	}
	return MutationReceipt{Commit: CommitUnknown},
		commitUnknown(MutationOperationAppendEvidence, errors.Join(anomaly, readErr))
}

func (c *MutationClient) verifyCommittedAppend(
	ctx context.Context,
	evidence []message.Triple,
	receipt MutationReceipt,
) (MutationReceipt, error) {
	entity, readErr := c.ReadAuthoritative(ctx, evidence[0].Subject)
	if readErr == nil && appendFactsPresent(evidence, entity) {
		receipt.Entity = entity
		receipt.Commit = CommitVerified
		return receipt, nil
	}
	if readErr == nil {
		readErr = errors.New("authoritative entity does not contain the committed append evidence")
	}
	return committedUnverified(MutationOperationAppendEvidence, receipt, readErr)
}

func (c *MutationClient) resolveRequestedAppendFailure(
	ctx context.Context,
	entityID string,
	reason string,
) (MutationReceipt, error) {
	entity, readErr := c.ReadAuthoritative(ctx, entityID)
	if isNotFound(readErr) {
		return MutationReceipt{Commit: CommitNotCommitted},
			newMutationError(MutationOperationAppendEvidence, readErr, CommitNotCommitted)
	}
	rejected := fmt.Errorf("append rejected for entity: %s", reason)
	if readErr != nil {
		rejected = errors.Join(rejected, readErr)
	}
	return MutationReceipt{Entity: entity, Commit: CommitNotCommitted}, &MutationError{
		Operation: MutationOperationAppendEvidence,
		Kind:      MutationUnavailable,
		Class:     errs.ErrorTransient,
		Commit:    CommitNotCommitted,
		Err:       rejected,
	}
}

func (c *MutationClient) canonicalizeReplace(
	req ReplaceOwnedMutation,
) (contractBinding, predicateGroupBinding, []message.Triple, MutationMetadata, error) {
	const operation = MutationOperationReplaceOwned
	binding, err := c.binding(operation, req.Contract)
	if err != nil {
		return contractBinding{}, predicateGroupBinding{}, nil, MutationMetadata{}, err
	}
	if err := validateEntityIDForContract(binding.contract, req.EntityID); err != nil {
		return contractBinding{}, predicateGroupBinding{}, nil, MutationMetadata{},
			invalidMutationError(operation, err)
	}
	group, err := selectReplaceGroup(binding, req.Group)
	if err != nil {
		return contractBinding{}, predicateGroupBinding{}, nil, MutationMetadata{},
			invalidMutationError(operation, err)
	}
	desired, metadata, err := canonicalizeTriples(operation, req.Desired, req.Metadata, false)
	if err != nil {
		return contractBinding{}, predicateGroupBinding{}, nil, MutationMetadata{}, err
	}
	for index, triple := range desired {
		if triple.Subject != req.EntityID {
			return contractBinding{}, predicateGroupBinding{}, nil, MutationMetadata{}, invalidMutationError(
				operation, fmt.Errorf("desired triple[%d] subject must equal entity ID", index),
			)
		}
		if _, selected := group.set[triple.Predicate]; !selected {
			return contractBinding{}, predicateGroupBinding{}, nil, MutationMetadata{}, invalidMutationError(
				operation,
				fmt.Errorf("predicate %q is outside selected replace-owned group %q", triple.Predicate, group.name),
			)
		}
	}
	candidate := &graph.EntityState{ID: req.EntityID, Triples: desired}
	if err := graph.ValidateEntityStateContract(candidate); err != nil {
		return contractBinding{}, predicateGroupBinding{}, nil, MutationMetadata{},
			invalidMutationError(operation, err)
	}
	return binding, group, desired, metadata, nil
}

func selectReplaceGroup(binding contractBinding, selector string) (predicateGroupBinding, error) {
	if selector == "" {
		if len(binding.replaceGroups) != 1 {
			return predicateGroupBinding{}, fmt.Errorf(
				"contract %q has %d replace-owned groups; selector must identify exactly one",
				binding.contract.Name,
				len(binding.replaceGroups),
			)
		}
		return binding.replaceGroups[0], nil
	}
	group, found := binding.namedGroups[selector]
	if !found {
		return predicateGroupBinding{}, fmt.Errorf(
			"contract %q has no named predicate group %q",
			binding.contract.Name,
			selector,
		)
	}
	if group.mode != ownership.ModeReplaceOwned {
		return predicateGroupBinding{}, fmt.Errorf(
			"predicate group %q uses mode %q, not replace-owned",
			selector,
			group.mode,
		)
	}
	return group, nil
}

func (c *MutationClient) canonicalizeAppend(
	req AppendEvidenceMutation,
) ([]message.Triple, MutationMetadata, error) {
	const operation = MutationOperationAppendEvidence
	binding, err := c.binding(operation, req.Contract)
	if err != nil {
		return nil, MutationMetadata{}, err
	}
	if err := validateEntityIDForContract(binding.contract, req.EntityID); err != nil {
		return nil, MutationMetadata{}, invalidMutationError(operation, err)
	}
	if len(req.Evidence) == 0 {
		return nil, MutationMetadata{}, invalidMutationError(operation, errors.New("evidence is required"))
	}
	evidence, metadata, err := canonicalizeTriples(operation, req.Evidence, req.Metadata, true)
	if err != nil {
		return nil, MutationMetadata{}, err
	}
	for index, triple := range evidence {
		if triple.Subject != req.EntityID {
			return nil, MutationMetadata{}, invalidMutationError(
				operation, fmt.Errorf("evidence triple[%d] subject must equal entity ID", index),
			)
		}
		if binding.primaryModes[triple.Predicate] != ownership.ModeAppendEvidence {
			return nil, MutationMetadata{}, invalidMutationError(
				operation, fmt.Errorf("predicate %q is not append-evidence", triple.Predicate),
			)
		}
	}
	candidate := &graph.EntityState{ID: req.EntityID, Triples: evidence}
	if err := graph.ValidateEntityStateContract(candidate); err != nil {
		return nil, MutationMetadata{}, invalidMutationError(operation, err)
	}
	return evidence, metadata, nil
}

func validateEntityIDForContract(contract Contract, entityID string) error {
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return fmt.Errorf("entity ID: %w", err)
	}
	matches, err := semtypes.MatchEntityIDPattern(contract.EntityPattern, entityID)
	if err != nil {
		return fmt.Errorf("entity pattern: %w", err)
	}
	if !matches {
		return fmt.Errorf("entity %q is outside contract %q pattern", entityID, contract.Name)
	}
	return nil
}

func (c *MutationClient) verifyCreate(
	ctx context.Context,
	canonical canonicalCreate,
	receipt MutationReceipt,
) (MutationReceipt, error) {
	entity, err := c.ReadAuthoritative(ctx, canonical.entity.ID)
	if err != nil {
		return committedUnverified(MutationOperationCreate, receipt, err)
	}
	if !createFactsMatch(canonical, entity) {
		return committedUnverified(
			MutationOperationCreate,
			receipt,
			errors.New("authoritative entity does not match requested creation"),
		)
	}
	receipt.Entity = entity
	receipt.Commit = CommitVerified
	return receipt, nil
}

func createFactsMatch(canonical canonicalCreate, entity *graph.EntityState) bool {
	if entity == nil ||
		entity.ID != canonical.entity.ID ||
		entity.MessageType != canonical.entity.MessageType {
		return false
	}
	requested := make(map[string][]message.Triple)
	for _, triple := range canonical.triples {
		if triple.Subject == canonical.entity.ID {
			requested[triple.Predicate] = append(requested[triple.Predicate], triple)
		}
	}
	for predicate, triples := range requested {
		stored := triplesForPredicate(entity, predicate)
		if !sameTripleMultiset(triples, stored) {
			return false
		}
	}
	return true
}

func ownedFactsMatch(
	group predicateGroupBinding,
	desired []message.Triple,
	entity *graph.EntityState,
) bool {
	if entity == nil {
		return false
	}
	for _, predicate := range group.predicates {
		want := make([]message.Triple, 0)
		for _, triple := range desired {
			if triple.Predicate == predicate {
				want = append(want, triple)
			}
		}
		if !sameTripleMultiset(want, triplesForPredicate(entity, predicate)) {
			return false
		}
	}
	return true
}

func appendFactsPresent(evidence []message.Triple, entity *graph.EntityState) bool {
	if entity == nil {
		return false
	}
	available := append([]message.Triple(nil), entity.Triples...)
	for _, want := range evidence {
		found := -1
		for index, candidate := range available {
			if sameAppendTuple(want, candidate) {
				found = index
				break
			}
		}
		if found < 0 {
			return false
		}
		available = append(available[:found], available[found+1:]...)
	}
	return true
}

func triplesForPredicate(entity *graph.EntityState, predicate string) []message.Triple {
	var triples []message.Triple
	for _, triple := range entity.Triples {
		if triple.Predicate == predicate {
			triples = append(triples, triple)
		}
	}
	return triples
}

func sameTripleMultiset(left, right []message.Triple) bool {
	if len(left) != len(right) {
		return false
	}
	remaining := append([]message.Triple(nil), right...)
	for _, want := range left {
		found := -1
		for index, candidate := range remaining {
			if sameFullTriple(want, candidate) {
				found = index
				break
			}
		}
		if found < 0 {
			return false
		}
		remaining = append(remaining[:found], remaining[found+1:]...)
	}
	return true
}

func sameAppendTuple(left, right message.Triple) bool {
	return left.Subject == right.Subject &&
		left.Predicate == right.Predicate &&
		left.Datatype == right.Datatype &&
		left.Source == right.Source &&
		left.Context == right.Context &&
		objectsEqual(left.Object, right.Object)
}

func sameFullTriple(left, right message.Triple) bool {
	if left.Subject != right.Subject ||
		left.Predicate != right.Predicate ||
		left.Datatype != right.Datatype ||
		left.Source != right.Source ||
		left.Context != right.Context ||
		left.Confidence != right.Confidence ||
		!left.Timestamp.Equal(right.Timestamp) ||
		!optionalTimesEqual(left.ExpiresAt, right.ExpiresAt) ||
		!objectsEqual(left.Object, right.Object) {
		return false
	}
	return true
}

func optionalTimesEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func objectsEqual(left, right any) bool {
	if reflect.DeepEqual(left, right) {
		return true
	}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func isClassified(err error) bool {
	var classified *errs.ClassifiedError
	return errors.As(err, &classified)
}

func isNotFound(err error) bool {
	var mutationErr *MutationError
	return errors.As(err, &mutationErr) && mutationErr.Kind == MutationNotFound
}

func commitUnknown(operation MutationOperation, err error) *MutationError {
	mapped := newMutationError(operation, err, CommitUnknown)
	mapped.Kind = MutationCommitUnknown
	return mapped
}

func committedUnverified(
	operation MutationOperation,
	receipt MutationReceipt,
	err error,
) (MutationReceipt, error) {
	receipt.Commit = CommitCommitted
	mapped := newMutationError(operation, err, CommitCommitted)
	mapped.Kind = MutationCommittedUnverified
	return receipt, mapped
}

func waitMutationRetry(ctx context.Context, retry natsclient.RetryConfig, retryIndex int) error {
	delay := float64(retry.InitialBackoff) * math.Pow(retry.BackoffMultiplier, float64(retryIndex))
	if retry.MaxBackoff > 0 && delay > float64(retry.MaxBackoff) {
		delay = float64(retry.MaxBackoff)
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(delay))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

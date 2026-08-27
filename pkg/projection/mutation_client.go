package projection

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

type mutationRequester interface {
	RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error)
}

type contractBinding struct {
	contract Contract
	groups   map[string]PredicateGroup
	allowed  map[string]struct{}
}

// MutationClient is an immutable, concurrency-safe graph mutation client.
type MutationClient struct {
	wire      *graphmutation.Client
	reader    graph.ExactEntityReader
	contracts map[string]contractBinding
}

// NewMutationClient validates and copies the complete projection contract set.
func NewMutationClient(cfg MutationClientConfig) (*MutationClient, error) {
	if cfg.NATS == nil {
		return nil, invalidMutationError(MutationOperationCreate, errors.New("NATS client is required"))
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = natsclient.DefaultRequestTimeout
	}
	return newMutationClient(cfg.NATS, cfg.Contracts, timeout)
}

func newMutationClient(
	requester mutationRequester,
	contracts []Contract,
	timeout time.Duration,
) (*MutationClient, error) {
	if isNilRequester(requester) {
		return nil, errors.New("mutation requester is required")
	}
	bindings, err := buildContractIndex(contracts)
	if err != nil {
		return nil, err
	}
	wire, err := graphmutation.NewClient(requester, timeout)
	if err != nil {
		return nil, err
	}
	return &MutationClient{
		wire:      wire,
		reader:    graph.NewExactEntityReader(requester, timeout),
		contracts: bindings,
	}, nil
}

func isNilRequester(requester mutationRequester) bool {
	if requester == nil {
		return true
	}
	value := reflect.ValueOf(requester)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func buildContractIndex(contracts []Contract) (map[string]contractBinding, error) {
	if err := ValidateContracts(contracts); err != nil {
		return nil, err
	}
	index := make(map[string]contractBinding, len(contracts))
	for _, original := range contracts {
		contract := cloneContract(original)
		binding := contractBinding{
			contract: contract,
			groups:   make(map[string]PredicateGroup, len(contract.Groups)),
			allowed:  make(map[string]struct{}),
		}
		for _, predicate := range contract.BirthPredicates {
			binding.allowed[predicate] = struct{}{}
		}
		for _, group := range contract.Groups {
			binding.groups[group.Name] = group
			for _, predicate := range group.Predicates {
				binding.allowed[predicate] = struct{}{}
			}
		}
		index[contract.Name] = binding
	}
	return index, nil
}

func cloneContract(contract Contract) Contract {
	clone := contract
	clone.BirthPredicates = append([]string(nil), contract.BirthPredicates...)
	clone.Groups = make([]PredicateGroup, len(contract.Groups))
	for index, group := range contract.Groups {
		clone.Groups[index] = group
		clone.Groups[index].Predicates = append([]string(nil), group.Predicates...)
	}
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

// Create atomically creates an entity with its complete initial triples.
func (c *MutationClient) Create(ctx context.Context, request CreateMutation) (MutationReceipt, error) {
	binding, err := c.binding(MutationOperationCreate, request.Contract)
	if err != nil {
		return notCommitted(), err
	}
	if request.Entity == nil {
		return notCommitted(), invalidMutationError(MutationOperationCreate, errors.New("entity is required"))
	}
	if len(request.Entity.Triples) != 0 {
		return notCommitted(), invalidMutationError(MutationOperationCreate,
			errors.New("entity.triples must be empty; CreateMutation.Triples is the sole fact source"))
	}
	entity := request.Entity.Clone()
	// O-17: the bound contract is the only spelling of the type a caller needs.
	// A zero stamp is filled from the contract's structured type before
	// validation and before the request is built (no key is parsed); a
	// non-zero stamp that differs is rejected below.
	if entity.MessageType == (message.Type{}) && binding.contract.MessageType.IsValid() {
		entity.MessageType = binding.contract.MessageType
	}
	if err := validateEntity(binding.contract, entity); err != nil {
		return notCommitted(), invalidMutationError(MutationOperationCreate, err)
	}
	triples, metadata, err := canonicalizeTriples(MutationOperationCreate, request.Triples, request.Metadata)
	if err != nil {
		return notCommitted(), err
	}
	for index, triple := range triples {
		if triple.Subject != entity.ID {
			return notCommitted(), invalidMutationError(MutationOperationCreate,
				fmt.Errorf("triple[%d] subject %q does not match entity %q", index, triple.Subject, entity.ID))
		}
		if _, allowed := binding.allowed[triple.Predicate]; !allowed {
			return notCommitted(), invalidMutationError(MutationOperationCreate,
				fmt.Errorf("predicate %q is not declared by contract %q", triple.Predicate, binding.contract.Name))
		}
	}
	response, err := c.wire.Create(ctx, graph.CreateEntityRequest{
		Entity: entity, Triples: triples, IndexingProfile: binding.contract.IndexingProfile,
		TraceID: metadata.TraceID, RequestID: metadata.RequestID,
	})
	if err != nil {
		return mutationFailure(MutationOperationCreate, err)
	}
	if response.Entity.ID != entity.ID {
		return unknownMutation(MutationOperationCreate, errors.New("create response entity does not match request"))
	}
	return MutationReceipt{Entity: response.Entity.Clone(), KVRevision: response.KVRevision, Commit: CommitVerified}, nil
}

// Reconcile replaces one complete reconcile-mode predicate group.
func (c *MutationClient) Reconcile(ctx context.Context, request ReconcileMutation) (MutationReceipt, error) {
	binding, group, desired, metadata, err := c.canonicalizeGroupMutation(
		MutationOperationReconcile, request.Contract, request.Group, request.EntityID, request.Desired, request.Metadata, ModeReconcile,
	)
	if err != nil {
		return notCommitted(), err
	}
	exact, err := c.ReadAuthoritative(ctx, request.EntityID)
	if err != nil {
		return notCommitted(), err
	}
	if err := validateEntity(binding.contract, exact.Entity); err != nil {
		return notCommitted(), invalidMutationError(MutationOperationReconcile, err)
	}
	response, err := c.wire.Reconcile(ctx, graph.ReconcilePredicatesRequest{
		EntityID: request.EntityID, ExpectedRevision: exact.KVRevision,
		Predicates: append([]string(nil), group.Predicates...), Desired: desired,
		TraceID: metadata.TraceID, RequestID: metadata.RequestID,
	})
	if err != nil {
		return mutationFailure(MutationOperationReconcile, err)
	}
	return MutationReceipt{Entity: response.Entity.Clone(), KVRevision: response.KVRevision, Commit: CommitVerified}, nil
}

// Append appends triples in one append-mode predicate group to one entity.
func (c *MutationClient) Append(ctx context.Context, request AppendMutation) (MutationReceipt, error) {
	_, _, triples, metadata, err := c.canonicalizeGroupMutation(
		MutationOperationAppend, request.Contract, request.Group, request.EntityID, request.Triples, request.Metadata, ModeAppend,
	)
	if err != nil {
		return notCommitted(), err
	}
	response, err := c.wire.Append(ctx, graph.AppendTriplesRequest{
		Triples: triples, TraceID: metadata.TraceID, RequestID: metadata.RequestID,
	})
	if err != nil {
		return mutationFailure(MutationOperationAppend, err)
	}
	result := response.Results[0]
	if result.Outcome == graph.MutationFailed {
		classified := classifiedAppendFailure(result.Error)
		return notCommitted(), newMutationError(MutationOperationAppend, classified, CommitNotCommitted)
	}
	switch result.Outcome {
	case graph.MutationApplied, graph.MutationUnchanged:
		return MutationReceipt{KVRevision: result.KVRevision, Commit: CommitVerified}, nil
	case graph.MutationEntityNotFound:
		err := errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEntityNotFound,
			fmt.Errorf("entity not found: %s", request.EntityID))
		return notCommitted(), newMutationError(MutationOperationAppend, err, CommitNotCommitted)
	default:
		return unknownMutation(MutationOperationAppend, fmt.Errorf("unexpected append outcome %q", result.Outcome))
	}
}

// Delete conditionally deletes one entity. Callers supply the exact revision
// they intend to fence; the client does not hide a read or retry.
func (c *MutationClient) Delete(ctx context.Context, request DeleteMutation) (MutationReceipt, error) {
	if request.ExpectedRevision == 0 {
		return notCommitted(), invalidMutationError(MutationOperationDelete, errors.New("expected revision must be nonzero"))
	}
	response, err := c.wire.Delete(ctx, graph.DeleteEntityRequest{
		EntityID: request.EntityID, ExpectedRevision: request.ExpectedRevision,
		TraceID: request.Metadata.TraceID, RequestID: request.Metadata.RequestID,
	})
	if err != nil {
		return mutationFailure(MutationOperationDelete, err)
	}
	return MutationReceipt{KVRevision: response.ExpectedRevision, Commit: CommitVerified}, nil
}

// ReadAuthoritative returns one validated entity and its same-entry revision.
func (c *MutationClient) ReadAuthoritative(ctx context.Context, entityID string) (*graph.ExactEntity, error) {
	if c == nil || c.reader == nil {
		return nil, invalidMutationError(MutationOperationReadAuthoritative, errors.New("mutation client is nil"))
	}
	exact, err := c.reader.ReadExactEntity(ctx, entityID)
	if err != nil {
		_, mapped := mutationFailure(MutationOperationReadAuthoritative, err)
		return nil, mapped
	}
	return &graph.ExactEntity{Entity: exact.Entity.Clone(), KVRevision: exact.KVRevision}, nil
}

func (c *MutationClient) canonicalizeGroupMutation(
	operation MutationOperation,
	contractName string,
	groupName string,
	entityID string,
	input []message.Triple,
	metadata MutationMetadata,
	wantMode WriteMode,
) (contractBinding, PredicateGroup, []message.Triple, MutationMetadata, error) {
	binding, err := c.binding(operation, contractName)
	if err != nil {
		return contractBinding{}, PredicateGroup{}, nil, MutationMetadata{}, err
	}
	group, ok := binding.groups[groupName]
	if !ok || group.Mode != wantMode {
		return contractBinding{}, PredicateGroup{}, nil, MutationMetadata{},
			invalidMutationError(operation, fmt.Errorf("contract %q group %q is not %s mode", contractName, groupName, wantMode))
	}
	matches, err := semtypes.MatchEntityIDPattern(binding.contract.EntityPattern, entityID)
	if err != nil || !matches {
		if err == nil {
			err = fmt.Errorf("entity %q is outside contract %q pattern", entityID, contractName)
		}
		return contractBinding{}, PredicateGroup{}, nil, MutationMetadata{}, invalidMutationError(operation, err)
	}
	triples, metadata, err := canonicalizeTriples(operation, input, metadata)
	if err != nil {
		return contractBinding{}, PredicateGroup{}, nil, MutationMetadata{}, err
	}
	allowed := make(map[string]struct{}, len(group.Predicates))
	for _, predicate := range group.Predicates {
		allowed[predicate] = struct{}{}
	}
	for index, triple := range triples {
		if triple.Subject != entityID {
			return contractBinding{}, PredicateGroup{}, nil, MutationMetadata{},
				invalidMutationError(operation, fmt.Errorf("triple[%d] subject %q does not match entity %q", index, triple.Subject, entityID))
		}
		if _, ok := allowed[triple.Predicate]; !ok {
			return contractBinding{}, PredicateGroup{}, nil, MutationMetadata{},
				invalidMutationError(operation, fmt.Errorf("predicate %q is outside group %q", triple.Predicate, groupName))
		}
	}
	return binding, group, triples, metadata, nil
}

func validateEntity(contract Contract, entity *graph.EntityState) error {
	if entity == nil {
		return errors.New("entity is required")
	}
	if err := semtypes.ValidateEntityID(entity.ID); err != nil {
		return err
	}
	matches, err := semtypes.MatchEntityIDPattern(contract.EntityPattern, entity.ID)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("entity %q is outside contract %q pattern", entity.ID, contract.Name)
	}
	if !entity.MessageType.IsValid() {
		return errors.New("entity message type is required")
	}
	if contract.MessageType.IsValid() && !entity.MessageType.Equal(contract.MessageType) {
		return fmt.Errorf("entity message type %q does not match contract %q message type %q",
			entity.MessageType.Key(), contract.Name, contract.MessageType.Key())
	}
	return nil
}

func canonicalizeTriples(
	operation MutationOperation,
	input []message.Triple,
	metadata MutationMetadata,
) ([]message.Triple, MutationMetadata, error) {
	if operation == MutationOperationCreate || operation == MutationOperationAppend {
		if metadata.RequestID == "" {
			return nil, MutationMetadata{}, invalidMutationError(operation, errors.New("request ID is required"))
		}
		if metadata.Source == "" {
			return nil, MutationMetadata{}, invalidMutationError(operation, errors.New("source is required"))
		}
	}
	if operation == MutationOperationAppend && len(input) == 0 {
		return nil, MutationMetadata{}, invalidMutationError(operation, errors.New("at least one triple is required"))
	}

	triples := append([]message.Triple(nil), input...)
	for index := range triples {
		triple := &triples[index]
		if triple.Source != "" && metadata.Source != "" && triple.Source != metadata.Source {
			return nil, MutationMetadata{}, invalidMutationError(operation,
				fmt.Errorf("triple[%d] source conflicts with mutation metadata", index))
		}
		if triple.Context != "" && metadata.RequestID != "" && triple.Context != metadata.RequestID {
			return nil, MutationMetadata{}, invalidMutationError(operation,
				fmt.Errorf("triple[%d] context conflicts with mutation metadata", index))
		}
		if !triple.Timestamp.IsZero() && !metadata.Timestamp.IsZero() &&
			!triple.Timestamp.Equal(metadata.Timestamp) {
			return nil, MutationMetadata{}, invalidMutationError(operation,
				fmt.Errorf("triple[%d] timestamp conflicts with mutation metadata", index))
		}
		if triple.Source == "" {
			triple.Source = metadata.Source
		}
		if triple.Context == "" {
			triple.Context = metadata.RequestID
		}
		if triple.Timestamp.IsZero() {
			if metadata.Timestamp.IsZero() {
				triple.Timestamp = time.Now().UTC()
			} else {
				triple.Timestamp = metadata.Timestamp
			}
		}
		if triple.ExpiresAt != nil {
			expiresAt := *triple.ExpiresAt
			triple.ExpiresAt = &expiresAt
		}
	}
	return triples, metadata, nil
}

func notCommitted() MutationReceipt {
	return MutationReceipt{Commit: CommitNotCommitted}
}

func invalidMutationError(operation MutationOperation, err error) *MutationError {
	return &MutationError{
		Operation: operation, Kind: MutationInvalid, Class: errs.ErrorInvalid,
		Commit: CommitNotCommitted, Err: err,
	}
}

func mutationFailure(operation MutationOperation, err error) (MutationReceipt, error) {
	if isDefiniteFailure(err) {
		return notCommitted(), newMutationError(operation, err, CommitNotCommitted)
	}
	return unknownMutation(operation, err)
}

func unknownMutation(operation MutationOperation, err error) (MutationReceipt, error) {
	receipt := MutationReceipt{Commit: CommitUnknown}
	return receipt, &MutationError{
		Operation: operation, Kind: MutationCommitUnknown, Class: errs.Classify(err),
		Commit: CommitUnknown, Err: err,
	}
}

func isDefiniteFailure(err error) bool {
	var classified *errs.ClassifiedError
	return errors.As(err, &classified) || graphmutation.IsDefinitelyNotCommitted(err) || natsclient.IsNoResponders(err)
}

func newMutationError(operation MutationOperation, err error, commit CommitState) *MutationError {
	mapped := &MutationError{
		Operation: operation, Kind: MutationUnavailable, Class: errs.Classify(err), Commit: commit, Err: err,
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		if commit == CommitUnknown {
			mapped.Kind = MutationCommitUnknown
		}
		return mapped
	}
	mapped.Code = classified.Code
	mapped.Class = classified.Class
	mapped.Detail = cloneDetail(classified.Detail)
	switch classified.Code {
	case graph.ErrorCodeInvalidRequest, graph.ErrorCodeStructuralInvalid:
		mapped.Kind = MutationInvalid
	case graph.ErrorCodeEntityNotFound:
		mapped.Kind = MutationNotFound
	case graph.ErrorCodeEntityExists:
		mapped.Kind = MutationConflict
	case graph.ErrorCodeRevisionMismatch:
		mapped.Kind = MutationRevisionConflict
	case graph.ErrorCodeGraphStateResetRequired, graph.ErrorCodeInternal:
		mapped.Kind = MutationInternal
	default:
		if classified.Class == errs.ErrorInvalid {
			mapped.Kind = MutationInvalid
		} else if classified.Class == errs.ErrorFatal {
			mapped.Kind = MutationInternal
		}
	}
	return mapped
}

func classifiedAppendFailure(failure *graph.MutationFailure) error {
	class := errs.ErrorTransient
	switch failure.Class {
	case errs.ErrorInvalid.String():
		class = errs.ErrorInvalid
	case errs.ErrorFatal.String():
		class = errs.ErrorFatal
	}
	return errs.ClassifiedCode(class, failure.Code, fmt.Errorf("append failed: %s", failure.Code))
}

func cloneDetail(detail map[string]any) map[string]any {
	if detail == nil {
		return nil
	}
	clone := make(map[string]any, len(detail))
	keys := make([]string, 0, len(detail))
	for key := range detail {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		clone[key] = detail[key]
	}
	return clone
}

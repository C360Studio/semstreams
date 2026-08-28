package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/vocabulary"
)

const (
	graphRoundTripContract  = "e2e.graph-roundtrip"
	graphRoundTripGroup     = "title"
	graphRoundTripSource    = "e2e-graph-roundtrip"
	graphRoundTripTimeout   = 10 * time.Second
	graphQLExactEntityQuery = `query($id: String!) { entity(id: $id) { entity { id triples { subject predicate object } } kvRevision } }`
)

var (
	mutationCreateSubject    = mustGraphMutationSubject(graphmutation.CreateEntity)
	mutationReconcileSubject = mustGraphMutationSubject(graphmutation.ReconcilePredicates)
)

func mustGraphMutationSubject(operation graphmutation.Operation) string {
	subject, err := graphmutation.ResolveSubject(graphmutation.SubjectFamily, operation)
	if err != nil {
		panic(err)
	}
	return subject
}

// GraphRoundTripProbe exercises the public graph write and read seams against a
// running SemStreams stack. Message Logger supplies correlated transport and KV
// evidence; authoritative read-back and GraphQL remain the acceptance surfaces.
type GraphRoundTripProbe struct {
	nats       *client.NATSValidationClient
	msgLogger  *client.MessageLoggerClient
	graphqlURL string
	httpClient *http.Client
	timeout    time.Duration
	// org / platform are the DEPLOYMENT the probe is driving — positions 1-2 of
	// the canary it mints. Since ADR-102 the graph refuses any subject outside
	// its own authority, so a canary whose pair is not the running stack's
	// `platform.org`/`platform.id` is rejected at the boundary rather than
	// written. The caller states which stack it is probing; it is the one fact
	// the probe cannot observe from the outside.
	org      string
	platform string
}

// NewGraphRoundTripProbe builds the shared graph canary used by core and every
// tiered variant. The caller retains ownership of the NATS validation client.
func NewGraphRoundTripProbe(
	nats *client.NATSValidationClient,
	msgLogger *client.MessageLoggerClient,
	graphqlURL, org, platform string,
) *GraphRoundTripProbe {
	return &GraphRoundTripProbe{
		nats:       nats,
		msgLogger:  msgLogger,
		graphqlURL: strings.TrimRight(graphqlURL, "/"),
		httpClient: &http.Client{Timeout: 3 * time.Second},
		timeout:    graphRoundTripTimeout,
		org:        org,
		platform:   platform,
	}
}

// Run creates one uniquely correlated entity, reconciles its selected title, and
// proves authoritative state plus both public GraphQL views converge.
func (p *GraphRoundTripProbe) Run(ctx context.Context, result *Result) error {
	if err := p.validateDependencies(); err != nil {
		return err
	}

	rootTrace := natsclient.NewTraceContext()
	traceCtx := natsclient.ContextWithTrace(ctx, rootTrace)
	runCtx, cancel := context.WithTimeout(traceCtx, p.timeout)
	defer cancel()

	// The canary carries the DEPLOYMENT's own authority. A pair that disagrees
	// with the running stack's config is refused by the graph boundary, which is
	// the intended behaviour — so state the disagreement here rather than let it
	// surface as an opaque "invalid entity ID contract input".
	if p.org == "" || p.platform == "" {
		return fmt.Errorf("graph round-trip probe requires the deployment authority " +
			"(org/platform of the stack under test); a canary minted under any other pair is refused")
	}
	entityID := p.org + "." + p.platform + ".graph.core.canary." + rootTrace.TraceID[:12]
	before := "graph-canary-before-" + rootTrace.TraceID
	after := "graph-canary-after-" + rootTrace.TraceID
	requestPrefix := "graph-canary-" + rootTrace.TraceID
	createRequestID := requestPrefix + "-create"
	replaceRequestID := requestPrefix + "-replace"
	createTrace := rootTrace.NewSpan()
	replaceTrace := rootTrace.NewSpan()
	createCtx := natsclient.ContextWithTrace(runCtx, createTrace)
	replaceCtx := natsclient.ContextWithTrace(runCtx, replaceTrace)

	if err := p.msgLogger.Health(runCtx); err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID,
			fmt.Errorf("message logger is required for graph-roundtrip: %w", err))
	}

	mutationClient, err := p.buildMutationClient(entityID)
	if err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID, fmt.Errorf("bind projection client: %w", err))
	}

	createdAt := time.Now().UTC()
	createReceipt, err := mutationClient.Create(createCtx, projection.CreateMutation{
		Contract: graphRoundTripContract,
		Entity:   newGraphRoundTripEntity(entityID, createdAt),
		Triples: []message.Triple{{
			Subject: entityID, Predicate: vocabulary.DCTermsTitle, Object: before,
		}},
		Metadata: projection.MutationMetadata{
			RequestID: createRequestID,
			TraceID:   rootTrace.TraceID,
			Source:    graphRoundTripSource,
			Timestamp: createdAt,
		},
	})
	if err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID, fmt.Errorf("create entity: %w", err))
	}
	if createReceipt.Commit != projection.CommitVerified {
		return p.withDiagnostics(entityID, rootTrace.TraceID,
			fmt.Errorf("create commit = %q, want %q", createReceipt.Commit, projection.CommitVerified))
	}

	replacedAt := time.Now().UTC()
	replaceReceipt, err := mutationClient.Reconcile(replaceCtx, projection.ReconcileMutation{
		Contract: graphRoundTripContract,
		Group:    graphRoundTripGroup,
		EntityID: entityID,
		Desired: []message.Triple{{
			Subject: entityID, Predicate: vocabulary.DCTermsTitle, Object: after,
		}},
		Metadata: projection.MutationMetadata{
			RequestID: replaceRequestID,
			TraceID:   rootTrace.TraceID,
			Source:    graphRoundTripSource,
			Timestamp: replacedAt,
		},
	})
	if err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID, fmt.Errorf("reconcile selected title: %w", err))
	}
	if replaceReceipt.Commit != projection.CommitVerified {
		return p.withDiagnostics(entityID, rootTrace.TraceID,
			fmt.Errorf("replace commit = %q, want %q", replaceReceipt.Commit, projection.CommitVerified))
	}

	authoritative, err := mutationClient.ReadAuthoritative(runCtx, entityID)
	if err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID, fmt.Errorf("read authoritative entity: %w", err))
	}
	if authoritative.KVRevision == 0 {
		return p.withDiagnostics(entityID, rootTrace.TraceID, errors.New("authoritative read returned zero KV revision"))
	}
	if err := validateTitleReplacement(authoritative.Entity, before, after); err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID, fmt.Errorf("authoritative replacement: %w", err))
	}

	if err := p.waitForGraphQL(runCtx, entityID, before, after); err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID, err)
	}
	traceEntries, err := p.waitForMutationTrace(runCtx, rootTrace.TraceID, map[string]mutationTraceExpectation{
		mutationCreateSubject: {
			EntityID: entityID, RequestID: createRequestID, TraceID: rootTrace.TraceID, SpanID: createTrace.SpanID,
		},
		mutationReconcileSubject: {
			EntityID: entityID, RequestID: replaceRequestID, TraceID: rootTrace.TraceID, SpanID: replaceTrace.SpanID,
		},
	})
	if err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID, err)
	}
	kvEvidence, err := p.msgLogger.QueryKV(runCtx, graph.BucketEntityStates, entityID, 1)
	if err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID, fmt.Errorf("message logger KV evidence: %w", err))
	}
	if err := validateKVEvidence(kvEvidence, entityID, before, after); err != nil {
		return p.withDiagnostics(entityID, rootTrace.TraceID, err)
	}

	if result != nil {
		if result.Details == nil {
			result.Details = make(map[string]any)
		}
		if result.Metrics == nil {
			result.Metrics = make(map[string]any)
		}
		result.Details["graph_roundtrip"] = map[string]any{
			"entity_id":              entityID,
			"trace_id":               rootTrace.TraceID,
			"create_span_id":         createTrace.SpanID,
			"replace_span_id":        replaceTrace.SpanID,
			"create_kv_revision":     createReceipt.KVRevision,
			"replace_kv_revision":    replaceReceipt.KVRevision,
			"mutation_trace_entries": len(traceEntries),
			"graphql_url":            p.graphqlURL,
		}
		result.Metrics["graph_roundtrip_trace_entries"] = len(traceEntries)
	}
	return nil
}

func newGraphRoundTripEntity(entityID string, updatedAt time.Time) *graph.EntityState {
	return &graph.EntityState{
		ID:          entityID,
		MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
		Version:     1,
		UpdatedAt:   updatedAt,
	}
}

func (p *GraphRoundTripProbe) validateDependencies() error {
	switch {
	case p == nil:
		return errors.New("graph-roundtrip probe is nil")
	case p.nats == nil:
		return errors.New("graph-roundtrip requires NATS validation client")
	case p.msgLogger == nil:
		return errors.New("graph-roundtrip requires Message Logger client")
	case p.graphqlURL == "":
		return errors.New("graph-roundtrip requires GraphQL URL")
	case p.httpClient == nil:
		return errors.New("graph-roundtrip requires HTTP client")
	default:
		return nil
	}
}

func (p *GraphRoundTripProbe) buildMutationClient(entityID string) (*projection.MutationClient, error) {
	contract := projection.Contract{
		Name:            graphRoundTripContract,
		MessageType:     message.Type{Domain: "test", Category: "fixture", Version: "v1"},
		EntityPattern:   entityID,
		IndexingProfile: "control",
		Groups: []projection.PredicateGroup{{
			Name: graphRoundTripGroup, Mode: projection.ModeReconcile,
			Predicates: []string{vocabulary.DCTermsTitle},
		}},
	}
	client, err := projection.NewMutationClient(projection.MutationClientConfig{
		NATS: p.nats.Client(), Contracts: []projection.Contract{contract}, Timeout: 3 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (p *GraphRoundTripProbe) waitForGraphQL(ctx context.Context, entityID, before, after string) error {
	return pollGraphRoundTrip(ctx, 100*time.Millisecond, func() error {
		entity, err := p.queryGraphQLEntity(ctx, entityID)
		if err != nil {
			return err
		}
		if err := validateTitleReplacement(entity, before, after); err != nil {
			return fmt.Errorf("GraphQL entity replacement: %w", err)
		}

		newMembers, err := p.queryGraphQLPredicate(ctx, after)
		if err != nil {
			return fmt.Errorf("GraphQL new-value membership: %w", err)
		}
		if countString(newMembers, entityID) != 1 || len(newMembers) != 1 {
			return fmt.Errorf("GraphQL new-value members = %v, want exactly [%s]", newMembers, entityID)
		}

		oldMembers, err := p.queryGraphQLPredicate(ctx, before)
		if err != nil {
			return fmt.Errorf("GraphQL old-value membership: %w", err)
		}
		if countString(oldMembers, entityID) != 0 {
			return fmt.Errorf("GraphQL old-value members still contain %s: %v", entityID, oldMembers)
		}
		return nil
	})
}

func (p *GraphRoundTripProbe) queryGraphQLEntity(ctx context.Context, entityID string) (*graph.EntityState, error) {
	var response struct {
		Data struct {
			Entity *graph.ExactEntity `json:"entity"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	err := p.postGraphQL(ctx, map[string]any{
		"query":     graphQLExactEntityQuery,
		"variables": map[string]any{"id": entityID},
	}, &response)
	if err != nil {
		return nil, err
	}
	if err := responseError(response.Errors); err != nil {
		return nil, err
	}
	if response.Data.Entity == nil {
		return nil, errors.New("GraphQL exact entity response is null")
	}
	if response.Data.Entity.Entity == nil {
		return nil, errors.New("GraphQL exact entity response has no entity")
	}
	if response.Data.Entity.KVRevision == 0 {
		return nil, errors.New("GraphQL exact entity response has zero KV revision")
	}
	return response.Data.Entity.Entity, nil
}

func (p *GraphRoundTripProbe) queryGraphQLPredicate(ctx context.Context, value string) ([]string, error) {
	var response struct {
		Data struct {
			EntitiesByPredicate struct {
				Entities []string `json:"entities"`
			} `json:"entitiesByPredicate"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	err := p.postGraphQL(ctx, map[string]any{
		"query": `query($predicate: String!, $value: String!, $limit: Int) { entitiesByPredicate(predicate: $predicate, value: $value, limit: $limit) }`,
		"variables": map[string]any{
			"predicate": vocabulary.DCTermsTitle,
			"value":     value,
			"limit":     10,
		},
	}, &response)
	if err != nil {
		return nil, err
	}
	if err := responseError(response.Errors); err != nil {
		return nil, err
	}
	return response.Data.EntitiesByPredicate.Entities, nil
}

type graphQLError struct {
	Message string `json:"message"`
}

func responseError(graphQLErrors []graphQLError) error {
	if len(graphQLErrors) == 0 {
		return nil
	}
	messages := make([]string, 0, len(graphQLErrors))
	for _, graphqlErr := range graphQLErrors {
		messages = append(messages, graphqlErr.Message)
	}
	return fmt.Errorf("GraphQL errors: %s", strings.Join(messages, "; "))
}

func (p *GraphRoundTripProbe) postGraphQL(ctx context.Context, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal GraphQL request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.graphqlURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create GraphQL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute GraphQL request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GraphQL status = %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode GraphQL response: %w", err)
	}
	return nil
}

type mutationTraceExpectation struct {
	EntityID  string
	RequestID string
	TraceID   string
	SpanID    string
}

func (p *GraphRoundTripProbe) waitForMutationTrace(
	ctx context.Context,
	traceID string,
	expected map[string]mutationTraceExpectation,
) ([]client.MessageEntry, error) {
	var matched []client.MessageEntry
	_, err := p.msgLogger.WaitForTrace(ctx, traceID, func(entries []client.MessageEntry) error {
		var err error
		matched, err = validateMutationTraceEntries(entries, expected)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("Message Logger trace convergence: %w", err)
	}
	return matched, nil
}

func validateMutationTraceEntries(
	entries []client.MessageEntry,
	expected map[string]mutationTraceExpectation,
) ([]client.MessageEntry, error) {
	matched := make([]client.MessageEntry, 0, len(expected))
	spans := make(map[string]string, len(expected))
	for subject, want := range expected {
		var lastErr error
		found := false
		for _, entry := range entries {
			if entry.Subject != subject {
				continue
			}
			if err := validateMutationTraceEntry(entry, want); err != nil {
				lastErr = err
				continue
			}
			if prior, duplicate := spans[entry.SpanID]; duplicate {
				return nil, fmt.Errorf("%s and %s reused span ID %q", prior, subject, entry.SpanID)
			}
			spans[entry.SpanID] = subject
			matched = append(matched, entry)
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("missing valid %s entry (last observation: %v)", subject, lastErr)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Subject < matched[j].Subject })
	return matched, nil
}

func validateMutationTraceEntry(
	entry client.MessageEntry,
	expected mutationTraceExpectation,
) error {
	if entry.TraceID != expected.TraceID {
		return fmt.Errorf("entry trace_id=%q, want %q", entry.TraceID, expected.TraceID)
	}
	if entry.SpanID == "" {
		return errors.New("entry span_id is empty")
	}
	if entry.SpanID != expected.SpanID {
		return fmt.Errorf("entry span_id=%q, want child span %q", entry.SpanID, expected.SpanID)
	}

	entityID, requestID, payloadTraceID, err := decodeMutationTracePayload(entry)
	if err != nil {
		return err
	}
	if entityID != expected.EntityID {
		return fmt.Errorf("payload entity_id=%q, want %q", entityID, expected.EntityID)
	}
	if requestID != expected.RequestID {
		return fmt.Errorf("payload request_id=%q, want %q", requestID, expected.RequestID)
	}
	if payloadTraceID != expected.TraceID {
		return fmt.Errorf("payload trace_id=%q, want %q", payloadTraceID, expected.TraceID)
	}
	return nil
}

func decodeMutationTracePayload(entry client.MessageEntry) (string, string, string, error) {
	switch entry.Subject {
	case mutationCreateSubject:
		var request graph.CreateEntityRequest
		if err := json.Unmarshal(entry.RawData, &request); err != nil {
			return "", "", "", fmt.Errorf("decode create raw_data: %w", err)
		}
		if request.Entity == nil {
			return "", "", "", errors.New("create raw_data entity is nil")
		}
		return request.Entity.ID, request.RequestID, request.TraceID, nil
	case mutationReconcileSubject:
		var request graph.ReconcilePredicatesRequest
		if err := json.Unmarshal(entry.RawData, &request); err != nil {
			return "", "", "", fmt.Errorf("decode replace raw_data: %w", err)
		}
		return request.EntityID, request.RequestID, request.TraceID, nil
	default:
		return "", "", "", fmt.Errorf("unexpected mutation subject %q", entry.Subject)
	}
}

func pollGraphRoundTrip(ctx context.Context, interval time.Duration, check func() error) error {
	var lastErr error
	for {
		err := check()
		if err == nil {
			return nil
		}
		lastErr = err

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return fmt.Errorf("convergence deadline reached: %w (last observation: %v)", ctx.Err(), lastErr)
		case <-timer.C:
		}
	}
}

func validateTitleReplacement(entity *graph.EntityState, before, after string) error {
	if entity == nil {
		return errors.New("entity is nil")
	}
	beforeCount := 0
	afterCount := 0
	predicateCount := 0
	for _, triple := range entity.Triples {
		if triple.Predicate != vocabulary.DCTermsTitle {
			continue
		}
		predicateCount++
		switch fmt.Sprint(triple.Object) {
		case before:
			beforeCount++
		case after:
			afterCount++
		}
	}
	if predicateCount != 1 || beforeCount != 0 || afterCount != 1 {
		return fmt.Errorf("dc.terms.title total=%d before=%d after=%d, want total=1 before=0 after=1",
			predicateCount, beforeCount, afterCount)
	}
	return nil
}

func validateKVEvidence(evidence *client.KVQueryResult, entityID, before, after string) error {
	if evidence == nil || evidence.Count != 1 || len(evidence.Entries) != 1 {
		return fmt.Errorf("Message Logger ENTITY_STATES evidence count = %v, want 1", evidence)
	}
	entry := evidence.Entries[0]
	if entry.Key != entityID {
		return fmt.Errorf("Message Logger ENTITY_STATES key = %q, want %q", entry.Key, entityID)
	}
	raw, err := json.Marshal(entry.Value)
	if err != nil {
		return fmt.Errorf("marshal Message Logger KV evidence: %w", err)
	}
	var entity graph.EntityState
	if err := graph.UnmarshalEntityState(raw, &entity); err != nil {
		return fmt.Errorf("decode Message Logger KV evidence: %w", err)
	}
	if err := validateTitleReplacement(&entity, before, after); err != nil {
		return fmt.Errorf("Message Logger KV replacement: %w", err)
	}
	return nil
}

func countString(values []string, target string) int {
	count := 0
	for _, value := range values {
		if value == target {
			count++
		}
	}
	return count
}

func (p *GraphRoundTripProbe) withDiagnostics(entityID, traceID string, cause error) error {
	diagnosticCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	parts := []string{"trace_id=" + traceID, "entity_id=" + entityID}
	if trace, err := p.msgLogger.GetEntriesByTrace(diagnosticCtx, traceID); err != nil {
		parts = append(parts, "trace_error="+err.Error())
	} else {
		subjects := make([]string, 0, len(trace.Entries))
		for _, entry := range trace.Entries {
			subjects = append(subjects, entry.Subject)
		}
		sort.Strings(subjects)
		parts = append(parts, fmt.Sprintf("trace_subjects=%v", subjects))
	}
	if kv, err := p.msgLogger.QueryKV(diagnosticCtx, graph.BucketEntityStates, entityID, 1); err != nil {
		parts = append(parts, "kv_error="+err.Error())
	} else {
		encoded, _ := json.Marshal(kv)
		parts = append(parts, "kv="+string(encoded))
	}
	return fmt.Errorf("%w; diagnostics: %s", cause, strings.Join(parts, " "))
}

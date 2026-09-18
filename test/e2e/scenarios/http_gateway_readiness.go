package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
)

// gh#1336 — the community-index readiness precondition of test-http-gateway.
//
// globalSearch is served from a community GENERATION that graph-query's consumer
// cache publishes once per watch, at the initial-enumeration sentinel
// (processor/graph-query/community_cache.go:92-99), and unpublishes when that
// watch is lost (:140-153). Its supervisor then re-opens COMMUNITY_INDEX and
// stages a fresh generation every RecheckInterval — default 5s, and the tiered
// configs do not override it (processor/graph-query/component.go:101-103,
// :710-746). So "no generation published" is reachable at any point in a run,
// not only at cold start.
//
// While none is published, every community path answers with the DECLARED
// retryable transient — errs.ClassifiedCode(ErrorTransient,
// graph.ErrorCodeIndexNotReady, "community index is not ready")
// (processor/graph-query/graphrag.go:120-122); the site comment at :986-988
// states the contract: "Its absence is a transient readiness error so the caller
// can retry or choose another path." The gateway projects it onto the GraphQL
// envelope as errors[0].extensions {class, code}
// (gateway/graph-gateway/component.go:1491-1503), a shape pinned by
// gateway/graph-gateway/query_contract_closure_test.go:83-102.
//
// That envelope IS the readiness signal, on the exact path the stage asserts:
// graph-query publishes no GRAPH_STATUS key and no gauge for its community
// generation (communityPublished/communityUnpublished at
// processor/graph-query/component.go:152-153 are test hooks), and the
// producer-side waits this package already owns — waitForCommunities
// (tiered_semantic.go:65) and stages.awaitProducersCaughtUp
// (stages/entities.go:68) — observe COMMUNITY_INDEX and GRAPH_STATUS, never the
// consumer cache the gateway actually reads through. So the stage waits on the
// signal itself, taking the caller side of the retry contract rather than
// turning one sample of a declared transient into a tier failure.

const (
	// communityIndexReadyWait bounds the wait. The supervisor stages a fresh
	// generation every RecheckInterval (5s by default), so this is six recheck
	// cycles: long enough to cover a lost watch mid-run, short enough that a
	// generation which never publishes is still reported as a finding rather
	// than absorbed into the tier's runtime.
	communityIndexReadyWait = 30 * time.Second

	// communityIndexReadyPoll is the retry cadence. Nothing is gained by
	// probing faster than the gateway can answer; a lost generation cannot come
	// back sooner than the supervisor's next recheck anyway.
	communityIndexReadyPoll = 1 * time.Second
)

// gatewayGraphQLError is one GraphQL error together with the classified
// extensions the gateway attaches to it.
type gatewayGraphQLError struct {
	Message    string `json:"message"`
	Extensions struct {
		Class string `json:"class"`
		Code  string `json:"code"`
	} `json:"extensions"`
}

// isIndexNotReady reports whether this error is the declared, retryable index
// readiness transient — and nothing else.
//
// Both halves are required, and both are production constants rather than
// transcribed strings. An uncoded error, a different code, or the same code
// raised FATAL (a reset, not a bootstrap) is a real failure: it fails the stage
// on the first response, because a wait that swallows it would convert a broken
// deployment into a 30s pause and then a confusing timeout.
func (e gatewayGraphQLError) isIndexNotReady() bool {
	return e.Extensions.Code == graph.ErrorCodeIndexNotReady &&
		e.Extensions.Class == errs.ErrorTransient.String()
}

// gatewayGlobalSearchResponse is the GraphQL envelope test-http-gateway decodes.
type gatewayGlobalSearchResponse struct {
	Data struct {
		GlobalSearch struct {
			Entities []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"entities"`
			Count    int    `json:"count"`
			Strategy string `json:"strategy"`
		} `json:"globalSearch"`
	} `json:"data"`
	Errors []gatewayGraphQLError `json:"errors"`
}

// gatewayGlobalSearchQuery is the stage's controlled probe: the same
// globalSearch("robot warehouse", level:0) document the stage has always sent.
func gatewayGlobalSearchQuery() map[string]any {
	return map[string]any{
		"query": `query($query: String!, $level: Int, $maxCommunities: Int) {
			globalSearch(query: $query, level: $level, maxCommunities: $maxCommunities) {
				entities { id type }
				count
				strategy
			}
		}`,
		"variables": map[string]any{
			"query":          "robot warehouse",
			"level":          0,
			"maxCommunities": 10,
		},
	}
}

// awaitReadyGatewayGlobalSearch issues the stage's globalSearch query and
// returns the first response the community index actually served, retrying ONLY
// while every GraphQL error in the envelope is the declared readiness transient.
//
// The probe and the wait are one operation deliberately: a separate "wait for
// ready, then query" pair reopens the same race between the two calls, since the
// generation can be unpublished in between.
//
// It records what it did on the Result unconditionally — a wait that happened
// and a wait that was not needed are both observations, and a metric that only
// appears when the slow path ran cannot be read as zero.
//
// maxWait bounds ADMISSION, not completion. No request is started once the
// readiness budget is spent — including the case where the polling delay itself
// would cross the deadline, since an admitted request may then occupy the whole
// per-query timeout OUTSIDE the budget and return success from outside it. A
// request admitted while the budget still held is allowed to finish on the
// per-query timeout, which is deliberately longer for legitimate semantic
// synthesis.
func (s *TieredScenario) awaitReadyGatewayGlobalSearch(
	ctx context.Context,
	result *Result,
	maxWait time.Duration,
	poll time.Duration,
) (*gatewayGlobalSearchResponse, error) {
	queryJSON, err := json.Marshal(gatewayGlobalSearchQuery())
	if err != nil {
		return nil, fmt.Errorf("marshal GraphQL gateway query: %w", err)
	}

	httpClient := &http.Client{Timeout: globalSearchClientTimeout(60 * time.Second)}
	startWait := time.Now()
	deadline := startWait.Add(maxWait)

	// attempts counts requests ISSUED; the recorded retry count is every request
	// beyond the first, so a budget that expires during the delay reads as "waited,
	// never retried" rather than claiming a retry that was refused.
	attempts := 0
	var notReady gatewayGraphQLError
	record := func() {
		result.Metrics["graphql_gateway_index_not_ready_retries"] = attempts - 1
		result.Metrics["graphql_gateway_readiness_wait_ms"] = time.Since(startWait).Milliseconds()
	}
	budgetSpent := func() error {
		record()
		return fmt.Errorf(
			"gateway globalSearch never served a ready community index within %s "+
				"(%d attempts over %s; last response: code=%q class=%q message=%q): "+
				"the community generation graph-query answers globalSearch from was "+
				"never published, so this is the index, not the query",
			maxWait, attempts, time.Since(startWait).Round(time.Millisecond),
			notReady.Extensions.Code, notReady.Extensions.Class, notReady.Message)
	}

	for {
		gqlResp, latency, err := s.postGatewayGlobalSearch(ctx, httpClient, queryJSON)
		attempts++
		if err != nil {
			record()
			return nil, err
		}
		result.Metrics["graphql_gateway_latency_ms"] = latency.Milliseconds()

		transient, waiting := readinessTransient(gqlResp.Errors)
		if !waiting {
			record()
			if len(gqlResp.Errors) > 0 {
				return nil, fmt.Errorf("GraphQL gateway search error: %s", gqlResp.Errors[0].Message)
			}
			return gqlResp, nil
		}
		notReady = transient

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, budgetSpent()
		}

		fmt.Printf("[GATEWAY READINESS WAIT] globalSearch reports %q (%s/%s) after %.1fs; retrying\n",
			notReady.Message, notReady.Extensions.Class, notReady.Extensions.Code,
			time.Since(startWait).Seconds())

		// Never sleep past the budget. An unclamped delay that crosses the
		// deadline admits one more request, and that request runs on the
		// per-query timeout (60s, or the longer semantic override) entirely
		// outside the budget — a success there would be reported as success.
		delay := poll
		if delay > remaining {
			delay = remaining
		}

		select {
		case <-ctx.Done():
			record()
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		// The clamped delay lands ON the deadline, so re-check before admitting
		// the next request rather than after it has already run.
		if !time.Now().Before(deadline) {
			return nil, budgetSpent()
		}
	}
}

// readinessTransient reports whether the envelope is nothing but the readiness
// transient, and returns the one to report. It is false for an empty error list
// (that is a served answer, not a wait) and false the moment any error in the
// list is something else — a real error standing beside a readiness transient
// still fails the stage.
func readinessTransient(gqlErrors []gatewayGraphQLError) (gatewayGraphQLError, bool) {
	if len(gqlErrors) == 0 {
		return gatewayGraphQLError{}, false
	}
	for _, e := range gqlErrors {
		if !e.isIndexNotReady() {
			return gatewayGraphQLError{}, false
		}
	}
	return gqlErrors[0], true
}

// postGatewayGlobalSearch performs one gateway request and decodes the envelope.
// A classified handler error arrives as HTTP 200 with a GraphQL errors envelope
// (gateway/graph-gateway/component.go:2210-2226), so a non-200 is a transport or
// gateway fault and is returned as such.
func (s *TieredScenario) postGatewayGlobalSearch(
	ctx context.Context,
	httpClient *http.Client,
	queryJSON []byte,
) (*gatewayGlobalSearchResponse, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", s.config.GraphQLURL, strings.NewReader(string(queryJSON)))
	if err != nil {
		return nil, 0, fmt.Errorf("create GraphQL gateway request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute GraphQL gateway request: %w", err)
	}
	defer resp.Body.Close()
	latency := time.Since(startTime)

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, latency, fmt.Errorf("GraphQL gateway returned status %d and body read failed: %w", resp.StatusCode, readErr)
		}
		return nil, latency, fmt.Errorf("GraphQL gateway returned status %d: %s", resp.StatusCode, body)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, latency, fmt.Errorf("read GraphQL gateway response: %w", err)
	}

	var gqlResp gatewayGlobalSearchResponse
	if err := json.Unmarshal(bodyBytes, &gqlResp); err != nil {
		return nil, latency, fmt.Errorf("decode GraphQL gateway response: %w", err)
	}
	return &gqlResp, latency, nil
}

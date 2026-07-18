package researchsynthesize

import (
	"context"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
)

// llmSynthesizerAdapter wraps a graph/llm.Client into the Synthesizer
// interface. Production wires the OpenAI-compatible client resolved
// from the model registry; tests inject a fake Synthesizer directly
// into the Component and skip this adapter entirely.
type llmSynthesizerAdapter struct {
	client llm.Client
}

func newLLMSynthesizerAdapter(client llm.Client) *llmSynthesizerAdapter {
	return &llmSynthesizerAdapter{client: client}
}

// Synthesize sends one ChatCompletion with deterministic settings
// (temperature 0) so the same prompt produces the same JSON across
// runs.
func (a *llmSynthesizerAdapter) Synthesize(ctx context.Context, systemPrompt, userPrompt string, maxResponseTokens int) (string, string, error) {
	if a == nil || a.client == nil {
		return "", "", errors.New("llm client not configured")
	}
	zero := 0.0
	req := llm.ChatRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    maxResponseTokens,
		Temperature:  &zero,
	}
	resp, err := a.client.ChatCompletion(ctx, req)
	if err != nil {
		return "", "", fmt.Errorf("chat completion: %w", err)
	}
	return resp.Content, resp.FinishReason, nil
}

// natsLoopStore adapts natsclient.KVStore to the LoopStore interface.
// Key conventions:
//
//   - research.request.received.<loopID>     (read; written by research_graph tool, PR 1)
//   - execute.complete.<loopID>       (read; written by execute_subqueries, PR 4)
//   - route.complete.<loopID>         (read; written by route_search, PR 3; nil-ok)
//   - search_result.complete.<loopID> (write; R6 continuation rule watches this
//     per ADR-045 — the terminal trigger key
//     names the PAYLOAD shape, not the producing
//     component)
//   - synthesize.snapshot.<loopID>    (write; non-trigger queryable copy keyed on
//     component name for ops observability)
type natsLoopStore struct {
	kv      *natsclient.KVStore
	decoder *message.Decoder
}

func newNATSLoopStore(kv *natsclient.KVStore, registry *payloadregistry.Registry) *natsLoopStore {
	return &natsLoopStore{
		kv:      kv,
		decoder: message.NewDecoder(registry),
	}
}

// Key helpers — package-private.

func loopStoreKeyIntent(loopID string) string          { return "research.request.received." + loopID }
func loopStoreKeyExecuteComplete(loopID string) string { return "execute.complete." + loopID }
func loopStoreKeyRouteComplete(loopID string) string   { return "route.complete." + loopID }
func loopStoreKeySearchResultComplete(loopID string) string {
	return "search_result.complete." + loopID
}
func loopStoreKeySynthesizeSnapshot(loopID string) string { return "synthesize.snapshot." + loopID }

// GetIntent decodes the research_intent payload from the trigger
// key. errIntentNotFound surfaces "key not found".
func (s *natsLoopStore) GetIntent(ctx context.Context, loopID string) (*research.Intent, error) {
	entry, err := s.kv.Get(ctx, loopStoreKeyIntent(loopID))
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, errIntentNotFound
		}
		return nil, fmt.Errorf("kv get %s: %w", loopStoreKeyIntent(loopID), err)
	}
	decoded, err := s.decoder.Decode(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("decode intent: %w", err)
	}
	intent, ok := decoded.Payload().(*research.Intent)
	if !ok {
		return nil, fmt.Errorf("decoded payload is %T, expected *research.Intent", decoded.Payload())
	}
	return intent, nil
}

// GetExecutionOutput decodes ExecutionOutput from execute.complete.
func (s *natsLoopStore) GetExecutionOutput(ctx context.Context, loopID string) (*research.ExecutionOutput, error) {
	entry, err := s.kv.Get(ctx, loopStoreKeyExecuteComplete(loopID))
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, errExecutionOutputNotFound
		}
		return nil, fmt.Errorf("kv get %s: %w", loopStoreKeyExecuteComplete(loopID), err)
	}
	decoded, err := s.decoder.Decode(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("decode execution output: %w", err)
	}
	out, ok := decoded.Payload().(*research.ExecutionOutput)
	if !ok {
		return nil, fmt.Errorf("decoded payload is %T, expected *research.ExecutionOutput", decoded.Payload())
	}
	return out, nil
}

// GetRouteDecision decodes RouteDecision from route.complete. Returns
// (nil, nil) on key-not-found so the synthesizer can degrade
// gracefully — RouteDecision is observability-grade for DecompTrace,
// not load-bearing for synthesis. Other errors propagate.
func (s *natsLoopStore) GetRouteDecision(ctx context.Context, loopID string) (*research.RouteDecision, error) {
	entry, err := s.kv.Get(ctx, loopStoreKeyRouteComplete(loopID))
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("kv get %s: %w", loopStoreKeyRouteComplete(loopID), err)
	}
	decoded, err := s.decoder.Decode(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("decode route decision: %w", err)
	}
	dec, ok := decoded.Payload().(*research.RouteDecision)
	if !ok {
		return nil, fmt.Errorf("decoded payload is %T, expected *research.RouteDecision", decoded.Payload())
	}
	return dec, nil
}

// PutSearchResult writes the SearchResult envelope at the
// search_result.complete.<loopID> trigger key per ADR-045 R6. The
// terminal trigger names the payload shape, not the producing
// component, so the continuation rule's key_pattern stays stable
// across future synthesizer reshuffles.
func (s *natsLoopStore) PutSearchResult(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeySearchResultComplete(loopID), envelope)
	return err
}

// loopCompletionKeyPrefix mirrors the convention the
// processor/agentic-tools.read_loop_result tool consumes (see its
// completeKeyPrefix). Keeping the constant local rather than
// importing keeps the dependency direction clean (research-graph
// chain → read_loop_result is operator-facing only, not a code
// dependency the chain takes on).
const loopCompletionKeyPrefix = "COMPLETE_"

// PutLoopCompletion writes the SearchResult envelope at the
// COMPLETE_<loopID> key so the parent agent's read_loop_result tool
// can fetch it without a new tool. Co-located with PutSearchResult so
// both AGENT_LOOPS writes that synthesize_answer makes live one place
// and the wire contract stays inspectable.
func (s *natsLoopStore) PutLoopCompletion(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopCompletionKeyPrefix+loopID, envelope)
	return err
}

// PutSnapshot writes the envelope at the stable snapshot key.
func (s *natsLoopStore) PutSnapshot(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeySynthesizeSnapshot(loopID), envelope)
	return err
}

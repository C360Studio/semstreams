package researchroute

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

// llmRouterAdapter wraps a graph/llm.Client into the Router
// interface. Production wires the OpenAI-compatible client resolved
// from the model registry; tests inject a fake Router directly into
// the Component and skip this adapter entirely.
type llmRouterAdapter struct {
	client llm.Client
}

func newLLMRouterAdapter(client llm.Client) *llmRouterAdapter {
	return &llmRouterAdapter{client: client}
}

// Route sends one ChatCompletion with deterministic settings
// (temperature 0) so the same prompt produces the same JSON across
// runs — important for trajectory replay + the structured-emit
// contract. MaxTokens is bounded so a runaway response truncates
// rather than blowing the budget.
func (a *llmRouterAdapter) Route(ctx context.Context, systemPrompt, userPrompt string, maxResponseTokens int) (string, string, error) {
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

// natsLoopStore adapts natsclient.KVStore to the LoopStore
// interface. Mirrors processor/research-graph-classify's adapter —
// same AGENT_LOOPS bucket, same payload-registry decode pattern.
//
// Key conventions:
//
//   - research.requested.<loopID>   (read; written by research_graph tool, PR 1)
//   - classify.complete.<loopID>    (read; written by nl_classify, PR 2)
//   - route.complete.<loopID>       (write; R2 watches this — trigger)
//   - route.snapshot.<loopID>       (write; non-trigger queryable copy)
type natsLoopStore struct {
	kv      *natsclient.KVStore
	decoder *message.Decoder
}

// newNATSLoopStore wires the LoopStore using the supplied KV store
// and the process-wide payload registry. registry must be non-nil —
// without it the upstream Intent + ClassifierOutput envelopes cannot
// be decoded. The factory caller (NewProcessor) is responsible for
// surfacing a nil-registry config error cleanly.
func newNATSLoopStore(kv *natsclient.KVStore, registry *payloadregistry.Registry) *natsLoopStore {
	return &natsLoopStore{
		kv:      kv,
		decoder: message.NewDecoder(registry),
	}
}

// Key helpers — kept package-private; consumers reference the
// conceptual operation, not the literal key shape, so a Phase 2
// reorganisation of trigger naming doesn't ripple across packages.

func loopStoreKeyIntent(loopID string) string           { return "research.requested." + loopID }
func loopStoreKeyClassifyComplete(loopID string) string { return "classify.complete." + loopID }
func loopStoreKeyRouteComplete(loopID string) string    { return "route.complete." + loopID }
func loopStoreKeyRouteSnapshot(loopID string) string    { return "route.snapshot." + loopID }

// GetIntent decodes the research_intent payload from the trigger
// key. errIntentNotFound surfaces "key not found" so the handler can
// log + drop without a retry storm.
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

// GetClassifierOutput decodes the upstream ClassifierOutput from
// the classify.complete trigger key.
func (s *natsLoopStore) GetClassifierOutput(ctx context.Context, loopID string) (*research.ClassifierOutput, error) {
	entry, err := s.kv.Get(ctx, loopStoreKeyClassifyComplete(loopID))
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, errClassifierOutputNotFound
		}
		return nil, fmt.Errorf("kv get %s: %w", loopStoreKeyClassifyComplete(loopID), err)
	}
	decoded, err := s.decoder.Decode(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("decode classifier output: %w", err)
	}
	out, ok := decoded.Payload().(*research.ClassifierOutput)
	if !ok {
		return nil, fmt.Errorf("decoded payload is %T, expected *research.ClassifierOutput", decoded.Payload())
	}
	return out, nil
}

// PutRouteDecision writes the envelope at R2's trigger key.
func (s *natsLoopStore) PutRouteDecision(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeyRouteComplete(loopID), envelope)
	return err
}

// PutSnapshot writes the envelope at the stable snapshot key.
// Best-effort: handler logs + continues on failure rather than
// aborting the chain.
func (s *natsLoopStore) PutSnapshot(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeyRouteSnapshot(loopID), envelope)
	return err
}

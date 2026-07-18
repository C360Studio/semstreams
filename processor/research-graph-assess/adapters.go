package researchassess

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

// llmAssessorAdapter wraps a graph/llm.Client into the Assessor
// interface. Production wires the OpenAI-compatible client resolved
// from the model registry; tests inject a fake Assessor directly
// into the Component and skip this adapter entirely.
type llmAssessorAdapter struct {
	client llm.Client
}

func newLLMAssessorAdapter(client llm.Client) *llmAssessorAdapter {
	return &llmAssessorAdapter{client: client}
}

// Assess sends one ChatCompletion with deterministic settings
// (temperature 0) so the same prompt produces the same JSON across
// runs — important for trajectory replay + the structured-emit
// contract. MaxTokens is bounded so a runaway response truncates
// rather than blowing the budget.
func (a *llmAssessorAdapter) Assess(ctx context.Context, systemPrompt, userPrompt string, maxResponseTokens int) (string, string, error) {
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
// Mirrors processor/research-graph-route's adapter — same
// AGENT_LOOPS bucket, same payload-registry decode pattern.
//
// Key conventions:
//
//   - research.request.received.<loopID>   (read; written by research_graph tool, PR 1)
//   - execute.complete.<loopID>     (read; written by execute_subqueries, PR 4)
//   - assess.complete.<loopID>      (write; R3 watches this — trigger)
//   - assess.snapshot.<loopID>      (write; non-trigger queryable copy)
type natsLoopStore struct {
	kv      *natsclient.KVStore
	decoder *message.Decoder
}

// newNATSLoopStore wires the LoopStore using the supplied KV store
// and the process-wide payload registry. registry must be non-nil —
// without it the upstream Intent + ExecutionOutput envelopes cannot
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

func loopStoreKeyIntent(loopID string) string          { return "research.request.received." + loopID }
func loopStoreKeyExecuteComplete(loopID string) string { return "execute.complete." + loopID }
func loopStoreKeyAssessComplete(loopID string) string  { return "assess.complete." + loopID }
func loopStoreKeyAssessSnapshot(loopID string) string  { return "assess.snapshot." + loopID }

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

// GetExecutionOutput decodes the upstream ExecutionOutput from the
// execute.complete trigger key.
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

// PutAssessmentOutput writes the envelope at R3's trigger key.
func (s *natsLoopStore) PutAssessmentOutput(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeyAssessComplete(loopID), envelope)
	return err
}

// PutSnapshot writes the envelope at the stable snapshot key.
// Best-effort: handler logs + continues on failure rather than
// aborting the chain.
func (s *natsLoopStore) PutSnapshot(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeyAssessSnapshot(loopID), envelope)
	return err
}

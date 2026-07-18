package researchclassify

import (
	"context"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/graph/query"
)

// Classifier is the narrow surface this component consumes from
// graph/query. Production satisfies it with *query.ClassifierChain
// (via classifierChainAdapter so the nil-check + Tier expansion live
// in adapters.go); tests substitute a fake that returns deterministic
// ClassificationResult shapes per scenario.
type Classifier interface {
	ClassifyQuery(ctx context.Context, topic string) *query.ClassificationResult
}

// CandidateRetriever fetches initial candidate entities for a topic.
// Production wraps a NATS request to graph.query.searchGraph; tests
// supply an in-memory fake so the per-variant pathway tests don't
// need a live graph stack.
//
// hints carries the classifier-derived SearchOptions facets (already
// flattened by ClassificationResult.Options) so the implementation
// can pass them through to the downstream search. Returning Degraded
// lets the implementation flag fallback paths (e.g. semantic
// fallback fired) without losing the candidates that came back.
type CandidateRetriever interface {
	FetchCandidates(ctx context.Context, topic string, hints map[string]any, limit int) (CandidateSet, error)
}

// CandidateSet is the retriever's return shape — separate type so
// the interface can evolve (add Strategy field, etc.) without
// changing the retriever signature.
type CandidateSet struct {
	Candidates     []research.Candidate
	Degraded       bool
	DegradedReason string
}

// LoopStore is the AGENT_LOOPS read/write surface this component
// consumes. Production wraps natsclient.KVStore; tests substitute an
// in-memory map.
//
// Three methods so the component can:
//
//   - GetIntent: load the research_intent payload from the
//     research.request.received.<loopID> key written by research_graph (PR 1).
//   - PutClassifierOutput: persist the ClassifierOutput envelope at
//     the classify.complete.<loopID> key — the trigger R1 watches.
//   - PutSnapshot: optionally stash a copy of the classifier output
//     at a stable, non-trigger key (classify.snapshot.<loopID>) so
//     downstream components / human operators can read the full
//     output without racing R1's wildcard watcher. PR 6 may unify
//     this with the completion key once the rule contract is
//     finalised; for PR 2 the snapshot key is a no-op in tests
//     unless explicitly observed.
type LoopStore interface {
	GetIntent(ctx context.Context, loopID string) (*research.Intent, error)
	PutClassifierOutput(ctx context.Context, loopID string, envelope []byte) error
	PutSnapshot(ctx context.Context, loopID string, envelope []byte) error
}

// errIntentNotFound is the sentinel returned by LoopStore.GetIntent
// when no research.request.received.<loopID> key exists. The handler
// surfaces it as a non-retryable error — a missing intent means R0
// fired without the trigger key being written, which is a flow
// configuration bug, not a transient failure.
var errIntentNotFound = fmt.Errorf("research intent not found")

// extractLoopIDFromSubject pulls the loop_id off the NATS subject
// component.nl_classify.<loop_id>. Returns empty string if the
// subject doesn't match the expected pattern; the handler treats an
// empty result as a routing bug.
//
// Format is stable per ADR-045 §Rule chain — R0's publish action
// uses `subject: "component.nl_classify.{loop_id}"`. Any deviation
// is a misconfigured rule, not a parsing problem.
func extractLoopIDFromSubject(subject string) string {
	const prefix = "component.nl_classify."
	if !strings.HasPrefix(subject, prefix) {
		return ""
	}
	return subject[len(prefix):]
}

// classifyAndRetrieve runs the classifier chain on the intent's
// topic, fetches candidates via the retriever, and returns the
// assembled ClassifierOutput. Pure function over the injected
// interfaces — no NATS / KV plumbing leaks in here so the test
// matrix can drive it through fakes.
func classifyAndRetrieve(ctx context.Context, classifier Classifier, retriever CandidateRetriever, intent *research.Intent, maxCandidates int) (*research.ClassifierOutput, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent is nil")
	}
	if classifier == nil {
		return nil, fmt.Errorf("classifier is nil")
	}
	if retriever == nil {
		return nil, fmt.Errorf("retriever is nil")
	}

	result := classifier.ClassifyQuery(ctx, intent.Topic)
	output := &research.ClassifierOutput{
		Topic: intent.Topic,
	}
	if result != nil {
		output.Tier = tierLabel(result.Tier)
		output.Intent = result.Intent
		output.Confidence = result.Confidence
		// Copy the Options map defensively so a downstream caller
		// can't mutate the classifier's internal state by mutating
		// the output.
		if len(result.Options) > 0 {
			output.Hints = make(map[string]any, len(result.Options))
			for k, v := range result.Options {
				output.Hints[k] = v
			}
		}
	}

	candidates, err := retriever.FetchCandidates(ctx, intent.Topic, output.Hints, maxCandidates)
	if err != nil {
		return nil, fmt.Errorf("retrieve candidates: %w", err)
	}
	output.Candidates = candidates.Candidates
	output.Degraded = candidates.Degraded
	output.DegradedReason = candidates.DegradedReason

	if err := output.Validate(); err != nil {
		return nil, fmt.Errorf("assembled output failed validation: %w", err)
	}
	return output, nil
}

// tierLabel renders a ClassifierChain tier number as the string
// taxonomy ClassifierOutput uses. Per chain conventions: 0=keyword,
// 1=embedding/BM25 (the chain doesn't currently distinguish T1/T2 at
// emit time so both surface as "1"), 3=LLM. Phase 2 may surface
// neural as "2" explicitly when the embedding tier splits.
func tierLabel(tier int) string {
	switch tier {
	case 0:
		return "0"
	case 1, 2:
		return "1"
	case 3:
		return "3"
	default:
		return ""
	}
}

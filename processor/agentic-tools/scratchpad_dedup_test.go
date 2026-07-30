package agentictools

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// The scratchpad tool is explicitly append-only — "multiple scratchpad calls
// within one loop simply accumulate" — and it emits on the PRODUCTION add
// lane (AddTriplesBatch), which suppresses any triple whose six-field identity
// tuple is already stored.
//
// Only ScratchID carried the per-call UUID. ScratchText, ScratchCreatedAt and
// ScratchChars carried no per-entry discriminator at all, so two calls with the
// same text — or merely the same CHARACTER COUNT, which is far more likely —
// emitted byte-identical tuples. The second call's entry then landed as an id
// with no text, no created-at and no chars.
//
// Identity is asserted through message.AppendIdentityKey, the same function the
// server lane suppresses by, rather than a re-implementation that could agree
// with the lane today and drift tomorrow.
func TestScratchpadExecutor_RepeatedCallsStayDistinctOnTheAddLane(t *testing.T) {
	tests := []struct {
		name       string
		firstText  string
		secondText string
	}{
		{
			name:       "identical text",
			firstText:  "the same thought twice",
			secondText: "the same thought twice",
		},
		{
			// The nastier case: different text, same length. ScratchChars
			// alone collides, so one call loses its chars triple — and
			// agent.scratch.chars is exactly what external rule matching
			// keys on.
			name:       "different text of identical length",
			firstText:  "aaaa bbbb",
			secondText: "cccc dddd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := &recordingPublisher{}
			e := NewScratchpadExecutor(pub, types.PlatformMeta{Org: "acme", Platform: "ops"})
			ids := []string{"scratch-id-1", "scratch-id-2"}
			idx := 0
			e.SetIDGenerator(func() string {
				out := ids[idx]
				idx++
				return out
			})
			// One frozen clock across both calls. Timestamp is EXCLUDED from
			// add-lane identity, so advancing it would not rescue anything —
			// freezing it keeps the test honest about that.
			e.SetClock(func() time.Time { return time.Date(2026, 5, 12, 9, 15, 0, 0, time.UTC) })

			for _, text := range []string{tt.firstText, tt.secondText} {
				if _, err := e.Execute(context.Background(), agentic.ToolCall{
					ID: "c", Name: ScratchpadToolName, LoopID: "loop",
					Arguments: map[string]any{"text": text},
				}); err != nil {
					t.Fatalf("execute: %v", err)
				}
			}

			if got := len(pub.triples); got != 8 {
				t.Fatalf("emitted triples = %d, want 8 (4 per call)", got)
			}

			// Every emitted triple must be a distinct assertion, or the add
			// lane collapses it and the entry is silently incomplete.
			survivors, suppressed := message.DedupeAppendTriples(nil, pub.triples)
			if suppressed != 0 {
				t.Fatalf("the add lane would suppress %d of 8 emitted triples, leaving an "+
					"incomplete scratchpad entry (survivors=%d)", suppressed, len(survivors))
			}

			// And each call's four triples must form ONE complete, groupable
			// entry. Context is the correlation key documented for exactly
			// this job, so each entry is retrievable by its scratch id.
			entries := map[string]map[string]bool{}
			for _, tr := range pub.triples {
				if tr.Context == "" {
					t.Fatalf("triple %q carries no correlation key, so its entry cannot be "+
						"reconstructed", tr.Predicate)
				}
				if entries[tr.Context] == nil {
					entries[tr.Context] = map[string]bool{}
				}
				entries[tr.Context][tr.Predicate] = true
			}
			if len(entries) != 2 {
				t.Fatalf("distinct scratchpad entries = %d, want 2", len(entries))
			}
			for _, scratchID := range ids {
				group, ok := entries[scratchID]
				if !ok {
					t.Fatalf("no entry correlated to scratch id %q", scratchID)
				}
				for _, predicate := range []string{
					agvocab.ScratchID, agvocab.ScratchText,
					agvocab.ScratchCreatedAt, agvocab.ScratchChars,
				} {
					if !group[predicate] {
						t.Errorf("entry %q is missing %s", scratchID, predicate)
					}
				}
				if len(group) != 4 {
					t.Errorf("entry %q has %d predicates, want 4", scratchID, len(group))
				}
			}
		})
	}
}

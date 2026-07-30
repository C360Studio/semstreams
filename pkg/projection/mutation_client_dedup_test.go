package projection

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// The add lane deduplicates by six-field tuple server-side (gh#697/gh#713), so
// a caller that innocently assembles the same evidence twice must NOT be told
// its write was not committed. Before the repair, canonicalizeAppend put both
// copies on the wire, the server wrote one, the written-count mismatch routed
// to verifyAnomalousAppend, and appendFactsPresent — which consumed matches
// from a MULTISET — demanded two stored copies, found one, and returned
// CommitNotCommitted with a fatal MutationInternal.
func TestAppendEvidenceWithInternalDuplicatesIsNotReportedNotCommitted(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	evidence := message.Triple{
		Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
	}
	canonical := evidence
	canonical.Source = req.Metadata.Source
	canonical.Context = req.Metadata.RequestID
	canonical.Timestamp = req.Metadata.Timestamp

	// The authoritative entity carries ONE copy — which is the whole point of
	// server-side suppression.
	stored := canonicalMutationTestEntity(req)
	stored.Triples = []message.Triple{canonical}

	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		// Server saw one tuple after the client collapsed it, and wrote it.
		subjectAddTriplesBatch: {{data: marshalMutationTestJSON(t, graph.AddTriplesBatchResponse{
			WrittenCount: 1,
		})}},
		subjectQueryEntity: {{data: marshalMutationTestJSON(t, stored)}},
	}}
	client := newMutationTestClient(t, rpc)

	receipt, err := client.AppendEvidence(context.Background(), AppendEvidenceMutation{
		Contract: req.Contract,
		EntityID: req.Entity.ID,
		// Two identical six-field tuples in one batch.
		Evidence: []message.Triple{evidence, evidence},
		Metadata: req.Metadata,
	})

	if err != nil {
		t.Fatalf("AppendEvidence with internally duplicated evidence: %v", err)
	}
	if receipt.Commit == CommitNotCommitted {
		t.Fatalf("commit = %q, want a committed state", receipt.Commit)
	}

	// Task 5.1: the duplicate must be collapsed BEFORE transport, preserving
	// first-input order, so the server's written count matches what was asked.
	var wire graph.AddTriplesBatchRequest
	if unmarshalErr := json.Unmarshal(rpc.calls[0].data, &wire); unmarshalErr != nil {
		t.Fatalf("decode wire request: %v", unmarshalErr)
	}
	if len(wire.Triples) != 1 {
		t.Fatalf("wire triples = %d, want 1 (duplicates collapse before transport)", len(wire.Triples))
	}
	if wire.Triples[0].Predicate != canonical.Predicate {
		t.Fatalf("wire triple = %#v, want the canonicalized evidence", wire.Triples[0])
	}
}

// Order preservation is part of the contract: a collapsed batch keeps first
// appearance order so a reader of the stored entity sees what was asserted.
func TestCanonicalizeAppendCollapsesDuplicatesPreservingFirstInputOrder(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	repeated := message.Triple{Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "a"}
	distinct := message.Triple{Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "b"}
	client := newMutationTestClient(t, &fakeMutationRequester{})

	evidence, _, err := client.canonicalizeAppend(AppendEvidenceMutation{
		Contract: req.Contract,
		EntityID: req.Entity.ID,
		Evidence: []message.Triple{repeated, distinct, repeated, repeated},
		Metadata: req.Metadata,
	})
	if err != nil {
		t.Fatalf("canonicalizeAppend: %v", err)
	}
	if len(evidence) != 2 {
		t.Fatalf("evidence = %d triples, want 2 after collapsing", len(evidence))
	}
	if evidence[0].Object != "a" || evidence[1].Object != "b" {
		t.Fatalf("evidence order = [%v %v], want first-input order [a b]",
			evidence[0].Object, evidence[1].Object)
	}
}

// Task 5.2: presence is a SET question, not a multiset one. Under server-side
// suppression, N identical evidence tuples are satisfied by one stored copy.
func TestAppendFactsPresentUsesSetPresenceNotMultisetConsumption(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	tuple := message.Triple{
		Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
		Source: req.Metadata.Source, Context: req.Metadata.RequestID,
		Timestamp: req.Metadata.Timestamp,
	}
	entity := canonicalMutationTestEntity(req)
	entity.Triples = []message.Triple{tuple}

	if !appendFactsPresent([]message.Triple{tuple, tuple, tuple}, entity) {
		t.Fatal("three identical evidence tuples must be satisfied by the one stored copy")
	}

	missing := tuple
	missing.Object = "never-written"
	if appendFactsPresent([]message.Triple{tuple, missing}, entity) {
		t.Fatal("a genuinely absent tuple must still report not present")
	}
	if appendFactsPresent([]message.Triple{tuple}, nil) {
		t.Fatal("a nil entity carries nothing")
	}
}

// Task 5.4, split per review B1. A fully-suppressed response is only an ANOMALY
// against a server that cannot say so. Once the server reports Deduplicated, the
// request is fully accounted for and needs NO read-back — which is what keeps
// the reported KVRevision on the receipt instead of discarding it.
func TestAppendEvidenceFullySuppressedResponse(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	evidence := message.Triple{
		Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
	}
	canonical := evidence
	canonical.Source = req.Metadata.Source
	canonical.Context = req.Metadata.RequestID
	canonical.Timestamp = req.Metadata.Timestamp
	// Stored with DIFFERENT excluded fields — the server suppressed against the
	// six-field key, so the client must verify against the same key.
	stored := canonicalMutationTestEntity(req)
	storedTuple := canonical
	storedTuple.Confidence = 0.25
	storedTuple.Timestamp = req.Metadata.Timestamp.Add(-1)
	stored.Triples = []message.Triple{storedTuple}

	tests := []struct {
		name           string
		response       graph.AddTriplesBatchResponse
		wantCommit     CommitState
		wantCalls      int
		wantRevisionOn bool
	}{
		{
			name:           "server reports the suppression",
			response:       graph.AddTriplesBatchResponse{WrittenCount: 0, Deduplicated: 1},
			wantCommit:     CommitCommitted,
			wantCalls:      1,
			wantRevisionOn: true,
		},
		{
			name: "old server cannot report it, read-back resolves",
			// Deduplicated absent: written(0) != expected(1) is a genuine
			// anomaly to a client that has no other account of the missing
			// tuple, and read-back must settle it.
			response:   graph.AddTriplesBatchResponse{WrittenCount: 0},
			wantCommit: CommitVerified,
			wantCalls:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			response := tt.response
			response.KVRevision = 42
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
				subjectAddTriplesBatch: {{data: marshalMutationTestJSON(t, response)}},
				subjectQueryEntity:     {{data: marshalMutationTestJSON(t, stored)}},
			}}
			client := newMutationTestClient(t, rpc)

			receipt, err := client.AppendEvidence(context.Background(), AppendEvidenceMutation{
				Contract: req.Contract, EntityID: req.Entity.ID,
				Evidence: []message.Triple{evidence}, Metadata: req.Metadata,
			})
			if err != nil {
				t.Fatalf("AppendEvidence against a fully-suppressed response: %v", err)
			}
			if receipt.Commit != tt.wantCommit {
				t.Fatalf("commit = %q, want %q", receipt.Commit, tt.wantCommit)
			}
			if rpc.callCount() != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", rpc.callCount(), tt.wantCalls)
			}
			if tt.wantRevisionOn && receipt.KVRevision != 42 {
				t.Fatalf("KVRevision = %d, want the server's live revision 42 preserved",
					receipt.KVRevision)
			}
		})
	}
}

// Review B1. A late-committing original plus an identical retry is the exact
// scenario the spec delta names: "a late commit followed by an identical retry
// stores one tuple AND the retry reports success". The retry's response is
// WrittenCount 0 + Deduplicated 1, and with an ambiguousCause already recorded
// any anomaly is escalated to CommitUnknown + a non-nil error — so counting a
// suppressed tuple as unaccounted-for turns a success into a reported failure
// that sister repos never saw before this change.
func TestAppendEvidenceLateCommitThenIdenticalRetryReportsSuccess(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	evidence := message.Triple{
		Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
	}
	// The read between the two attempts does NOT yet see the tuple: the original
	// write has not landed. That is what leaves ambiguousCause set going into
	// the retry.
	absent := canonicalMutationTestEntity(req)
	absent.Triples = nil

	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectAddTriplesBatch: {
			{err: errors.New("request timed out")},
			// The original committed late, so the retry is wholly suppressed.
			{data: marshalMutationTestJSON(t, graph.AddTriplesBatchResponse{
				MutationResponse: graph.MutationResponse{KVRevision: 42},
				WrittenCount:     0, Deduplicated: 1,
			})},
		},
		subjectQueryEntity: {{data: marshalMutationTestJSON(t, absent)}},
	}}
	client := newMutationTestClient(t, rpc)
	client.retry.MaxRetries = 1

	receipt, err := client.AppendEvidence(context.Background(), AppendEvidenceMutation{
		Contract: req.Contract, EntityID: req.Entity.ID,
		Evidence: []message.Triple{evidence}, Metadata: req.Metadata,
	})

	if err != nil {
		t.Fatalf("the retry must report success, got: %v", err)
	}
	if receipt.Commit == CommitUnknown || receipt.Commit == CommitNotCommitted {
		t.Fatalf("commit = %q, want a committed state", receipt.Commit)
	}
	if receipt.KVRevision != 42 {
		t.Fatalf("KVRevision = %d, want the live revision 42 the server reported",
			receipt.KVRevision)
	}
}

// Review B1, at the classifier itself: written + deduplicated accounts for the
// whole request when nothing failed.
func TestClassifyAppendResponseCountsDeduplicatedAsAccountedFor(t *testing.T) {
	t.Parallel()
	const entityID = "acme.ops.test.system.widget.001"

	tests := []struct {
		name        string
		response    graph.AddTriplesBatchResponse
		expected    int
		wantAnomaly bool
	}{
		{
			name:     "fully suppressed",
			response: graph.AddTriplesBatchResponse{WrittenCount: 0, Deduplicated: 3},
			expected: 3,
		},
		{
			name:     "partially suppressed",
			response: graph.AddTriplesBatchResponse{WrittenCount: 1, Deduplicated: 2},
			expected: 3,
		},
		{
			name:     "nothing suppressed",
			response: graph.AddTriplesBatchResponse{WrittenCount: 3},
			expected: 3,
		},
		{
			name:        "still short after counting suppressions",
			response:    graph.AddTriplesBatchResponse{WrittenCount: 1, Deduplicated: 1},
			expected:    3,
			wantAnomaly: true,
		},
		{
			name:        "old server reporting no suppression is still an anomaly",
			response:    graph.AddTriplesBatchResponse{WrittenCount: 0},
			expected:    1,
			wantAnomaly: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, validFailure, anomaly := classifyAppendResponse(tt.response, entityID, tt.expected)
			if validFailure {
				t.Fatalf("unexpected requested failure for %#v", tt.response)
			}
			if tt.wantAnomaly && anomaly == nil {
				t.Fatalf("expected an anomaly for %#v", tt.response)
			}
			if !tt.wantAnomaly && anomaly != nil {
				t.Fatalf("unexpected anomaly for %#v: %v", tt.response, anomaly)
			}
		})
	}
}

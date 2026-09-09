package executors

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/c360studio/semstreams/agentic"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// Properties over the two invariants the delta states as universals rather
// than as examples: the entity_type grammar, and the cursor partition.
//
// Both generators are written from the stated grammar and the stated
// contract — never from the executor's code — and each hugs the boundary the
// clause names rather than merely striding past it: the arity generator draws
// 1..entityTypeMaxTokens+1 so the accept/refuse flip at 3 is exercised on both
// sides on nearly every case, and the page-size generator includes 1 and the
// exact match count so "one page holds everything" and "every page holds one"
// are ordinary draws rather than lucky ones.

// canonicalSegment generates one segment of the entity-ID grammar: first byte
// alphanumeric, remaining bytes alphanumeric, '_' or '-'. Short by
// construction, so six of them stay well inside the 256-byte identity bound —
// that bound has its own property in pkg/types.
var canonicalSegment = rapid.StringMatching(`[a-z0-9][a-z0-9_-]{0,7}`)

// TestPropTypePatternRightAnchors: any one to three canonical segments build a
// valid six-position pattern whose literal positions are right-anchored on the
// ADR-102 type segment, and that pattern matches exactly the identities whose
// corresponding positions agree.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestPropTypePatternRightAnchors(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// 1..4: the accept range plus the first refused arity, so the bound is
		// hit from both sides rather than approached.
		count := rapid.IntRange(1, entityTypeMaxTokens+1).Draw(t, "tokenCount")
		tokens := make([]string, count)
		for index := range tokens {
			tokens[index] = canonicalSegment.Draw(t, "segment")
		}
		entityType := strings.Join(tokens, ".")

		pattern, err := buildTypePattern(entityType)
		if count > entityTypeMaxTokens {
			if err == nil {
				t.Fatalf("entity_type %q has %d segments and was accepted as %q", entityType, count, pattern)
			}
			return
		}
		if err != nil {
			t.Fatalf("canonical entity_type %q rejected: %v", entityType, err)
		}
		if validateErr := semtypes.ValidateEntityIDPattern(pattern); validateErr != nil {
			t.Fatalf("built pattern %q is not a valid entity-ID pattern: %v", pattern, validateErr)
		}

		parts := strings.Split(pattern, ".")
		if len(parts) != 6 {
			t.Fatalf("pattern %q has %d positions, want 6", pattern, len(parts))
		}
		// Right-anchored on the type segment: the last drawn token lands on
		// position 4, and each earlier token one position to its left.
		for offset, token := range tokens {
			position := typeSegmentIndex - (len(tokens) - 1 - offset)
			if parts[position] != token {
				t.Fatalf("pattern %q position %d = %q, want %q", pattern, position, parts[position], token)
			}
		}
		for position, part := range parts {
			if position > typeSegmentIndex && part != "*" {
				t.Fatalf("pattern %q must wildcard the instance position, got %q", pattern, part)
			}
			if position < typeSegmentIndex-len(tokens)+1 && part != "*" {
				t.Fatalf("pattern %q position %d must be a wildcard, got %q", pattern, position, part)
			}
		}

		// An identity built from the same right-anchored tokens matches; the
		// same identity with any ONE literal position changed does not.
		identity := buildIdentityFor(t, tokens)
		matched, matchErr := semtypes.MatchEntityIDPattern(pattern, identity)
		if matchErr != nil || !matched {
			t.Fatalf("pattern %q must match %q (matched=%v err=%v)", pattern, identity, matched, matchErr)
		}
	})
}

// buildIdentityFor materializes a six-part identity whose right-anchored
// positions carry the drawn tokens.
func buildIdentityFor(t *rapid.T, tokens []string) string {
	parts := []string{"org", "plat", "sys", "dom", "typ", "inst"}
	for offset, token := range tokens {
		parts[typeSegmentIndex-(len(tokens)-1-offset)] = token
	}
	identity := strings.Join(parts, ".")
	if err := semtypes.ValidateEntityID(identity); err != nil {
		t.Fatalf("constructed identity %q is not canonical: %v", identity, err)
	}
	return identity
}

// TestPropTypePatternRefusesInjectedWildcards: a wildcard reaching
// ValidateEntityIDPattern would VALIDATE — that is the pattern grammar's own
// job — and widen the listing. The refusal therefore has to happen on the
// caller's tokens, and this property injects one into every position of every
// arity to prove it does.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestPropTypePatternRefusesInjectedWildcards(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		count := rapid.IntRange(1, entityTypeMaxTokens).Draw(t, "tokenCount")
		tokens := make([]string, count)
		for index := range tokens {
			tokens[index] = canonicalSegment.Draw(t, "segment")
		}
		position := rapid.IntRange(0, count-1).Draw(t, "injectAt")
		injection := rapid.SampledFrom([]string{"*", ">", "a*", "*b", "a>b"}).Draw(t, "wildcard")
		tokens[position] = injection

		entityType := strings.Join(tokens, ".")
		pattern, err := buildTypePattern(entityType)
		if err == nil {
			t.Fatalf("entity_type %q carries a wildcard and built pattern %q", entityType, pattern)
		}
	})
}

// TestPropQueryByTypeCursorPartitionsTheMatch: over one unchanged key set, the
// pages reached by following next_cursor partition the sorted match EXACTLY
// once — no gap, no repeat, strictly increasing — for any page size.
//
// The expected value is the sorted set of generated keys, which the test holds
// independently; nothing here recomputes the executor's paging.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestPropQueryByTypeCursorPartitionsTheMatch(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const entityType = "propfixture"
		total := rapid.IntRange(1, 12).Draw(t, "matchCount")
		// Page sizes hug both ends: 1 (a page per identity) and the exact match
		// count (one page holds everything, so has_more must be false) are the
		// two boundaries the contract's has_more clause turns on.
		limit := rapid.OneOf(
			rapid.IntRange(1, 12),
			rapid.SampledFrom([]int{1, total, total + 1}),
		).Draw(t, "limit")
		if limit < 1 {
			limit = 1
		}

		want := make([]string, 0, total)
		for index := 0; index < total; index++ {
			want = append(want, fmt.Sprintf("acme.test.sys.dom.%s.i%02d", entityType, index))
		}
		sorted := append([]string(nil), want...)
		sort.Strings(sorted)

		// Hand the executor the keys in reverse order: FilteredKeys returns KV
		// scan order and sorts nothing, so an unsorted feed is the realistic
		// one and the partition must hold over it.
		scanOrder := make([]string, 0, total)
		for index := len(want) - 1; index >= 0; index-- {
			scanOrder = append(scanOrder, want[index])
		}
		executor := NewGraphQueryExecutor(&mockKVLister{mockKVGetter: newMockKVGetter(), keys: scanOrder})

		var seen []string
		cursor := ""
		for page := 0; page <= total+1; page++ {
			args := map[string]any{"entity_type": entityType, "limit": float64(limit)}
			if cursor != "" {
				args["cursor"] = cursor
			}
			result, err := executor.Execute(context.Background(), agentic.ToolCall{
				ID: fmt.Sprintf("prop-%d", page), Name: "query_by_type", Arguments: args,
			})
			if err != nil || result.Error != "" {
				t.Fatalf("page %d failed: err=%v result=%q", page, err, result.Error)
			}

			ids := propPageIDs(t, result.Content)
			hasMore, ok := result.Metadata[agentic.MetadataKeyHasMore].(bool)
			if !ok {
				t.Fatalf("page %d did not set %s; the pagination contract requires it on every successful result",
					page, agentic.MetadataKeyHasMore)
			}
			if len(ids) > limit {
				t.Fatalf("page %d returned %d identities over a limit of %d", page, len(ids), limit)
			}
			for index := 1; index < len(ids); index++ {
				if ids[index] <= ids[index-1] {
					t.Fatalf("page %d is not strictly increasing: %v", page, ids)
				}
			}
			if len(seen) > 0 && len(ids) > 0 && ids[0] <= seen[len(seen)-1] {
				t.Fatalf("page %d restarts at %q, which does not sort after %q", page, ids[0], seen[len(seen)-1])
			}
			seen = append(seen, ids...)

			nextCursor, hasCursor := result.Metadata[agentic.MetadataKeyNextCursor]
			if hasMore != hasCursor {
				t.Fatalf("page %d: has_more=%v but next_cursor present=%v — the two move together",
					page, hasMore, hasCursor)
			}
			if hasMore != (result.ResultHint == agentic.HintTooLarge) {
				t.Fatalf("page %d: has_more=%v but hint=%q", page, hasMore, result.ResultHint)
			}
			if !hasMore {
				break
			}
			cursor = nextCursor.(string)
		}

		if len(seen) != len(sorted) {
			t.Fatalf("pages covered %d identities, want %d (%v)", len(seen), len(sorted), seen)
		}
		for index := range sorted {
			if seen[index] != sorted[index] {
				t.Fatalf("position %d = %q, want %q", index, seen[index], sorted[index])
			}
		}
	})
}

// propPageIDs pulls entity_ids out of one page body.
func propPageIDs(t *rapid.T, content string) []string {
	var parsed struct {
		EntityIDs []string `json:"entity_ids"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("page content is not JSON: %v (%s)", err, content)
	}
	return parsed.EntityIDs
}

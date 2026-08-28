package graphquery

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
)

func TestExtractEntityType(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"acme.ops.robotics.gcs.drone.001", "drone"},
		{"local.dev.game.board1.quest.3416ee6c", "quest"},
		{"local.dev.model-registry.agent.endpoint.semembed", "endpoint"},
		{"short.id", ""},
		{"a.b.c.d.sensor.x", "sensor"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := extractEntityType(tt.id); got != tt.want {
			t.Errorf("extractEntityType(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestExtractEntityInstance(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"acme.ops.robotics.gcs.drone.001", "001"},
		{"local.dev.game.board1.quest.3416ee6c", "3416ee6c"},
		{"short.id", "short.id"},
		{"a.b.c.d.sensor.temp-001", "temp-001"},
	}
	for _, tt := range tests {
		if got := extractEntityInstance(tt.id); got != tt.want {
			t.Errorf("extractEntityInstance(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestResolveLabel(t *testing.T) {
	t.Run("fixed predicate order advances after unusable first stored match", func(t *testing.T) {
		entityID := "acme.ops.agent.loop.agent.bot1"
		entity := &gtypes.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{Subject: entityID, Predicate: "test.fixture.note", Object: "heuristic must not win"},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "dc", "terms", "title"), Object: ""},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "dc", "terms", "title"), Object: "later title must not win"},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "agent", "identity", "display-name"), Object: 42},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "agent", "capability", "name"), Object: "Routing Specialist"},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "agent", "model", "name"), Object: "later model"},
			},
		}

		if got := resolveLabel(entity); got != "Routing Specialist" {
			t.Errorf("resolveLabel() = %q, want Routing Specialist", got)
		}
	})

	t.Run("dc.terms.title wins", func(t *testing.T) {
		entity := &gtypes.EntityState{
			ID: "acme.ops.robotics.gcs.drone.001",
			Triples: []message.Triple{
				{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: semantictest.Predicate(t, "agent", "identity", "display-name"), Object: "Drone 001"},
				{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: semantictest.Predicate(t, "dc", "terms", "title"), Object: "Alpha Drone"},
			},
		}
		got := resolveLabel(entity)
		if got != "Alpha Drone" {
			t.Errorf("resolveLabel() = %q, want %q", got, "Alpha Drone")
		}
	})

	t.Run("display_name fallback", func(t *testing.T) {
		entity := &gtypes.EntityState{
			ID: "acme.ops.agent.loop.agent.bot1",
			Triples: []message.Triple{
				{Subject: "acme.ops.agent.loop.agent.bot1", Predicate: semantictest.Predicate(t, "agent", "identity", "display-name"), Object: "Scout Bot"},
			},
		}
		got := resolveLabel(entity)
		if got != "Scout Bot" {
			t.Errorf("resolveLabel() = %q, want %q", got, "Scout Bot")
		}
	})

	t.Run("model.name fallback", func(t *testing.T) {
		entity := &gtypes.EntityState{
			ID: "local.dev.model-registry.agent.endpoint.semembed",
			Triples: []message.Triple{
				{Subject: "local.dev.model-registry.agent.endpoint.semembed", Predicate: semantictest.Predicate(t, "agent", "model", "name"), Object: "nomic-embed-text"},
			},
		}
		got := resolveLabel(entity)
		if got != "nomic-embed-text" {
			t.Errorf("resolveLabel() = %q, want %q", got, "nomic-embed-text")
		}
	})

	t.Run("first string triple fallback", func(t *testing.T) {
		entity := &gtypes.EntityState{
			ID: "acme.ops.robotics.gcs.sensor.temp-001",
			Triples: []message.Triple{
				{Subject: "acme.ops.robotics.gcs.sensor.temp-001", Predicate: "robotics.sensor.reading", Object: 72.5},
				{Subject: "acme.ops.robotics.gcs.sensor.temp-001", Predicate: "robotics.sensor.location", Object: "hangar-b"},
			},
		}
		got := resolveLabel(entity)
		if got != "hangar-b" {
			t.Errorf("resolveLabel() = %q, want %q", got, "hangar-b")
		}
	})

	t.Run("skips entity ID references", func(t *testing.T) {
		entity := &gtypes.EntityState{
			ID: "acme.ops.robotics.gcs.sensor.temp-001",
			Triples: []message.Triple{
				{Subject: "acme.ops.robotics.gcs.sensor.temp-001", Predicate: "robotics.sensor.monitored-by", Object: "acme.ops.robotics.gcs.device.042"},
				{Subject: "acme.ops.robotics.gcs.sensor.temp-001", Predicate: "robotics.sensor.zone", Object: "zone-alpha"},
			},
		}
		got := resolveLabel(entity)
		if got != "zone-alpha" {
			t.Errorf("resolveLabel() = %q, want %q (should skip entity ID reference)", got, "zone-alpha")
		}
	})

	t.Run("empty triples returns empty", func(t *testing.T) {
		entity := &gtypes.EntityState{
			ID:      "acme.ops.robotics.gcs.sensor.temp-001",
			Triples: nil,
		}
		got := resolveLabel(entity)
		if got != "" {
			t.Errorf("resolveLabel() = %q, want empty", got)
		}
	})
}

func TestBuildEntityDigests(t *testing.T) {
	entityIDs := []string{
		"local.dev.game.board1.quest.3416ee6c",
		"local.dev.game.board1.agent.675e7820",
		"local.dev.model-registry.agent.endpoint.semembed",
	}
	scores := map[string]float64{
		"local.dev.game.board1.quest.3416ee6c": 0.87,
		"local.dev.game.board1.agent.675e7820": 0.82,
	}
	labels := map[string]string{
		"local.dev.game.board1.quest.3416ee6c":             "Dragon Slayer Quest",
		"local.dev.model-registry.agent.endpoint.semembed": "nomic-embed-text",
	}

	digests := buildEntityDigests(entityIDs, scores, labels)

	if len(digests) != 3 {
		t.Fatalf("len(digests) = %d, want 3", len(digests))
	}

	// First: has label and score
	if digests[0].Type != "quest" {
		t.Errorf("digests[0].Type = %q, want quest", digests[0].Type)
	}
	if digests[0].Label != "Dragon Slayer Quest" {
		t.Errorf("digests[0].Label = %q, want Dragon Slayer Quest", digests[0].Label)
	}
	if digests[0].Relevance != 0.87 {
		t.Errorf("digests[0].Relevance = %v, want 0.87", digests[0].Relevance)
	}

	// Second: no label in map, falls back to instance
	if digests[1].Type != "agent" {
		t.Errorf("digests[1].Type = %q, want agent", digests[1].Type)
	}
	if digests[1].Label != "675e7820" {
		t.Errorf("digests[1].Label = %q, want 675e7820 (instance fallback)", digests[1].Label)
	}

	// Third: has label, no score
	if digests[2].Label != "nomic-embed-text" {
		t.Errorf("digests[2].Label = %q, want nomic-embed-text", digests[2].Label)
	}
	if digests[2].Relevance != 0 {
		t.Errorf("digests[2].Relevance = %v, want 0", digests[2].Relevance)
	}
}

func TestSynthesizeAnswer(t *testing.T) {
	t.Run("empty summaries", func(t *testing.T) {
		got := synthesizeAnswer(nil, 0)
		if got != "" {
			t.Errorf("synthesizeAnswer(nil) = %q, want empty", got)
		}
	})

	t.Run("single community", func(t *testing.T) {
		summaries := []CommunitySummary{
			{
				CommunityID: "c1",
				Summary:     "Active quest instances on board1.",
				Keywords:    []string{"quest-completion", "reward-distribution"},
				MemberCount: 12,
				Relevance:   0.85,
				Entities: []EntityDigest{
					{ID: "local.dev.game.board1.quest.abc", Type: "quest", Label: "Dragon Slayer"},
				},
			},
		}
		got := synthesizeAnswer(summaries, 12)

		if got == "" {
			t.Fatal("synthesizeAnswer returned empty string")
		}
		// Should contain the header
		if !containsAll(got, "12 entities", "1 knowledge cluster") {
			t.Errorf("missing header in: %s", got)
		}
		// Should contain community summary
		if !containsAll(got, "Active quest instances") {
			t.Errorf("missing community summary in: %s", got)
		}
		// Should contain representative entity
		if !containsAll(got, "Dragon Slayer", "quest") {
			t.Errorf("missing representative entity in: %s", got)
		}
		// Should contain keywords
		if !containsAll(got, "quest-completion") {
			t.Errorf("missing keywords in: %s", got)
		}
	})

	t.Run("limits to 5 communities", func(t *testing.T) {
		summaries := make([]CommunitySummary, 8)
		for i := range summaries {
			summaries[i] = CommunitySummary{
				CommunityID: fmt.Sprintf("c%d", i),
				Summary:     fmt.Sprintf("Community %c", 'A'+i),
				MemberCount: 5,
				Relevance:   0.5,
			}
		}
		got := synthesizeAnswer(summaries, 40)

		// Should mention all 8 communities in header
		if !containsAll(got, "8 knowledge cluster") {
			t.Errorf("header should mention all 8 clusters: %s", got)
		}
		// Should only detail first 5
		if containsAll(got, "Community F") {
			t.Errorf("should not include 6th community: %s", got)
		}
	})
}

func TestFilterEntityIDsByType_UsesExtractEntityType(t *testing.T) {
	ids := []string{
		"a.b.c.d.sensor.001",
		"a.b.c.d.drone.002",
		"a.b.c.d.sensor.003",
		"a.b.c.d.mission.004",
	}
	got, fellBack := filterEntityIDsByType(ids, []string{"sensor"})
	if len(got) != 2 {
		t.Errorf("filterEntityIDsByType() returned %d, want 2", len(got))
	}
	if fellBack {
		t.Errorf("filterEntityIDsByType() fellBack=true on a partial match, want false")
	}
}

// TestFilterEntityIDsByType_ZeroFallback covers the graceful-fallback contract
// (#645): the classifier (qwen3-0.6b) invents type names that match no real
// type segment, and hard-zeroing a non-empty semantic result set produces an
// empty answer. The filter must instead preserve the input and signal fellBack.
func TestFilterEntityIDsByType_ZeroFallback(t *testing.T) {
	tests := []struct {
		name        string
		entityIDs   []string
		typeFilters []string
		wantIDs     []string
		wantFellBk  bool
	}{
		{
			// Classifier guessed types that match nothing real → do NOT zero a
			// non-empty semantic result set; return it unfiltered + fellBack.
			name:        "fallback: non-empty input, filters match nothing",
			entityIDs:   []string{"a.b.c.d.sensor.001", "a.b.c.d.drone.002"},
			typeFilters: []string{"Procedure", "equipment process"},
			wantIDs:     []string{"a.b.c.d.sensor.001", "a.b.c.d.drone.002"},
			wantFellBk:  true,
		},
		{
			name:        "partial match: some IDs match → subset, no fallback",
			entityIDs:   []string{"a.b.c.d.sensor.001", "a.b.c.d.drone.002", "a.b.c.d.sensor.003"},
			typeFilters: []string{"sensor"},
			wantIDs:     []string{"a.b.c.d.sensor.001", "a.b.c.d.sensor.003"},
			wantFellBk:  false,
		},
		{
			// Nothing to preserve — an already-empty input is not a fallback.
			name:        "already-empty input with non-empty filters → empty, no fallback",
			entityIDs:   []string{},
			typeFilters: []string{"sensor"},
			wantIDs:     []string{},
			wantFellBk:  false,
		},
		{
			name:        "empty typeFilters → input unchanged, no fallback",
			entityIDs:   []string{"a.b.c.d.sensor.001", "a.b.c.d.drone.002"},
			typeFilters: nil,
			wantIDs:     []string{"a.b.c.d.sensor.001", "a.b.c.d.drone.002"},
			wantFellBk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fellBack := filterEntityIDsByType(tt.entityIDs, tt.typeFilters)
			if fellBack != tt.wantFellBk {
				t.Errorf("fellBack = %v, want %v", fellBack, tt.wantFellBk)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("got %d IDs %v, want %d %v", len(got), got, len(tt.wantIDs), tt.wantIDs)
			}
			for i := range tt.wantIDs {
				if got[i] != tt.wantIDs[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.wantIDs[i])
				}
			}
		})
	}
}

func TestScoreEntityQuery(t *testing.T) {
	makeEntity := func(id string, triples ...message.Triple) *gtypes.EntityState {
		return &gtypes.EntityState{ID: id, Triples: triples}
	}

	t.Run("type match scores highest", func(t *testing.T) {
		entity := makeEntity("a.b.c.d.drone.001")
		score := scoreEntityQuery(entity, []string{"drone"})
		if score != 1.0 {
			t.Errorf("type match should score 1.0, got %v", score)
		}
	})

	t.Run("triple value match scores moderate", func(t *testing.T) {
		entity := makeEntity("a.b.c.d.sensor.001",
			message.Triple{Predicate: "sensor.identity.location", Object: "warehouse"},
		)
		score := scoreEntityQuery(entity, []string{"warehouse"})
		if score != 0.5 {
			t.Errorf("value match should score 0.5, got %v", score)
		}
	})

	t.Run("no match scores zero", func(t *testing.T) {
		entity := makeEntity("a.b.c.d.drone.001",
			message.Triple{Predicate: "drone.measurement.battery", Object: 85.0},
		)
		score := scoreEntityQuery(entity, []string{"warehouse", "logistics"})
		if score != 0 {
			t.Errorf("no match should score 0, got %v", score)
		}
	})

	t.Run("partial match proportional", func(t *testing.T) {
		entity := makeEntity("a.b.c.d.drone.001",
			message.Triple{Predicate: "drone.state.status", Object: "active"},
		)
		// "drone" matches type (1.0), "operations" doesn't match (0)
		// Total: 1.0 / 2 = 0.5
		score := scoreEntityQuery(entity, []string{"drone", "operations"})
		if score != 0.5 {
			t.Errorf("partial match should score 0.5, got %v", score)
		}
	})

	t.Run("multiple matches accumulate", func(t *testing.T) {
		entity := makeEntity("a.b.c.d.drone.001",
			message.Triple{Predicate: "drone.assignment.mission", Object: "survey"},
		)
		// "drone" matches type (1.0), "survey" matches value (0.5)
		// Total: 1.5 / 2 = 0.75
		score := scoreEntityQuery(entity, []string{"drone", "survey"})
		if score != 0.75 {
			t.Errorf("multi-match should score 0.75, got %v", score)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		entity := makeEntity("a.b.c.d.drone.001",
			message.Triple{Predicate: "drone.state.status", Object: "ACTIVE"},
		)
		score := scoreEntityQuery(entity, []string{"drone", "active"})
		if score < 0.5 {
			t.Errorf("case-insensitive match should score >= 0.5, got %v", score)
		}
	})
}

func TestFilterEntitiesByQuery_DropsLowScore(t *testing.T) {
	entities := []*gtypes.EntityState{
		{ID: "a.b.c.d.drone.001", Triples: []message.Triple{
			{Predicate: "drone.assignment.mission", Object: "survey"},
		}},
		{ID: "a.b.c.d.sensor.002", Triples: []message.Triple{
			{Predicate: "sensor.identity.location", Object: "hangar"},
		}},
		{ID: "a.b.c.d.worker.003", Triples: []message.Triple{
			{Predicate: "worker.identity.role", Object: "logistics"},
		}},
	}

	// Query "drone survey" — only the drone entity should match well
	result := filterEntitiesByQuery(entities, "drone survey", 0.3)

	if len(result) != 1 {
		t.Errorf("expected 1 entity above threshold, got %d", len(result))
		for _, e := range result {
			t.Logf("  matched: %s", e.ID)
		}
	}
	if len(result) > 0 && result[0].ID != "a.b.c.d.drone.001" {
		t.Errorf("expected drone entity, got %s", result[0].ID)
	}
}

func TestFilterEntitiesByQuery_SortsByScore(t *testing.T) {
	entities := []*gtypes.EntityState{
		{ID: "a.b.c.d.sensor.001", Triples: []message.Triple{
			{Predicate: "sensor.identity.type", Object: "temperature"},
		}},
		{ID: "a.b.c.d.drone.002", Triples: []message.Triple{
			{Predicate: "drone.equipment.sensor", Object: "temperature"},
		}},
	}

	// Query "drone temperature" — drone should rank higher (type match + value match)
	result := filterEntitiesByQuery(entities, "drone temperature", 0.0)

	if len(result) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(result))
	}
	if result[0].ID != "a.b.c.d.drone.002" {
		t.Errorf("drone should rank first (type match), got %s first", result[0].ID)
	}
}

// containsAll checks that s contains all substrings.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// TestSortAndCapEntityIDsDeterministic pins gh#621: the global-search candidate
// corpus is ordered BEFORE the resource cap is applied, so the retained subset is
// a pure function of the candidate set.
//
// The pre-fix code ranged the candidate map straight into a slice and sliced it
// to MaxTotalEntitiesInSearch. Go randomizes map iteration order, so past the cap
// the same query against an unchanged graph kept a different arbitrary 10,000
// entities on every call — and that partial corpus feeds LLM answer synthesis.
// Repeating the call is the whole test: one run can coincide, fifty cannot.
func TestSortAndCapEntityIDsDeterministic(t *testing.T) {
	const (
		limit      = 25
		candidates = 500
	)
	idSet := make(map[string]bool, candidates)
	for i := 0; i < candidates; i++ {
		idSet[fmt.Sprintf("acme.ops.robotics.gcs.drone.%04d", i)] = true
	}

	want, truncated := sortAndCapEntityIDs(idSet, limit)
	if !truncated {
		t.Fatalf("truncated = false with %d candidates over a limit of %d", candidates, limit)
	}
	if len(want) != limit {
		t.Fatalf("len = %d, want %d", len(want), limit)
	}
	if !sort.StringsAreSorted(want) {
		t.Fatalf("result is not sorted: %v", want)
	}
	// Sorting first means the cap keeps a defined prefix, not an arbitrary one.
	if want[0] != "acme.ops.robotics.gcs.drone.0000" || want[limit-1] != "acme.ops.robotics.gcs.drone.0024" {
		t.Errorf("cap did not retain the lexicographically smallest %d IDs: first=%q last=%q", limit, want[0], want[limit-1])
	}

	for run := 0; run < 50; run++ {
		got, gotTruncated := sortAndCapEntityIDs(idSet, limit)
		if !gotTruncated {
			t.Fatalf("run %d: truncated = false", run)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("run %d returned a different subset of the SAME candidate set (gh#621)\n first got:  %v\n first want: %v", run, got[:3], want[:3])
		}
	}
}

// TestSortAndCapEntityIDsUnderLimit covers the non-truncating paths: the flag must
// stay false so a complete corpus is never reported as partial.
func TestSortAndCapEntityIDsUnderLimit(t *testing.T) {
	idSet := map[string]bool{
		"acme.ops.robotics.gcs.drone.003": true,
		"acme.ops.robotics.gcs.drone.001": true,
		"acme.ops.robotics.gcs.drone.002": true,
	}

	ids, truncated := sortAndCapEntityIDs(idSet, 10)
	if truncated {
		t.Errorf("truncated = true with 3 candidates under a limit of 10")
	}
	want := []string{
		"acme.ops.robotics.gcs.drone.001",
		"acme.ops.robotics.gcs.drone.002",
		"acme.ops.robotics.gcs.drone.003",
	}
	if !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}

	// Exactly at the limit is not truncation.
	if _, atLimit := sortAndCapEntityIDs(idSet, 3); atLimit {
		t.Errorf("truncated = true when the candidate count equals the limit")
	}

	// limit <= 0 means no cap.
	if all, capped := sortAndCapEntityIDs(idSet, 0); capped || len(all) != 3 {
		t.Errorf("limit=0 should not cap: capped=%v len=%d", capped, len(all))
	}

	if empty, capped := sortAndCapEntityIDs(map[string]bool{}, 10); capped || len(empty) != 0 {
		t.Errorf("empty set: capped=%v len=%d", capped, len(empty))
	}
}

// TestGlobalSearchResponseEntitiesTruncatedJSON pins the operator/agent-visible
// half of gh#621: truncation is reported on the wire, not just avoided silently.
// omitempty means a complete corpus stays absent from the payload.
func TestGlobalSearchResponseEntitiesTruncatedJSON(t *testing.T) {
	truncatedJSON, err := json.Marshal(GlobalSearchResponse{EntitiesTruncated: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(truncatedJSON), `"entities_truncated":true`) {
		t.Errorf("truncation flag missing from response payload: %s", truncatedJSON)
	}

	var round GlobalSearchResponse
	if err := json.Unmarshal(truncatedJSON, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !round.EntitiesTruncated {
		t.Errorf("EntitiesTruncated did not survive the round trip")
	}

	completeJSON, err := json.Marshal(GlobalSearchResponse{})
	if err != nil {
		t.Fatalf("marshal complete: %v", err)
	}
	if strings.Contains(string(completeJSON), "entities_truncated") {
		t.Errorf("complete response should omit the flag entirely: %s", completeJSON)
	}
}

// TestCollectDigestTags is the Lever-B extractor contract: multi-valued
// classification tags are collected from an entity's triples (not just the first
// match), deduplicated, and capped — with non-tag/non-string triples ignored.
func TestCollectDigestTags(t *testing.T) {
	const id = "acme.ops.warehouse.dock.document.maint-008"
	tagPred := semantictest.Predicate(t, "content", "classification", "tag")
	otherPred := semantictest.Predicate(t, "content", "classification", "category")

	tagTriple := func(v any) message.Triple {
		return message.Triple{Subject: id, Predicate: tagPred, Object: v}
	}

	t.Run("multi-valued collected in order", func(t *testing.T) {
		e := &gtypes.EntityState{ID: id, Triples: []message.Triple{
			tagTriple("forklift"), tagTriple("battery"), tagTriple("charging"),
		}}
		got := collectDigestTags(e, MaxDigestTags)
		want := []string{"forklift", "battery", "charging"}
		if !slices.Equal(got, want) {
			t.Errorf("collectDigestTags = %v, want %v (all tag triples, first-seen order)", got, want)
		}
	})

	t.Run("duplicates deduped, non-tag and non-string skipped", func(t *testing.T) {
		e := &gtypes.EntityState{ID: id, Triples: []message.Triple{
			{Subject: id, Predicate: otherPred, Object: "operations"}, // wrong predicate
			tagTriple("battery"),
			tagTriple("battery"), // duplicate
			tagTriple(42),        // non-string object
			tagTriple(""),        // empty
			tagTriple("dock"),
		}}
		got := collectDigestTags(e, MaxDigestTags)
		want := []string{"battery", "dock"}
		if !slices.Equal(got, want) {
			t.Errorf("collectDigestTags = %v, want %v", got, want)
		}
	})

	t.Run("capped at max, first-seen order", func(t *testing.T) {
		triples := make([]message.Triple, 0, 10)
		for i := 0; i < 10; i++ {
			triples = append(triples, tagTriple(fmt.Sprintf("t%d", i)))
		}
		e := &gtypes.EntityState{ID: id, Triples: triples}
		got := collectDigestTags(e, MaxDigestTags)
		if len(got) != MaxDigestTags {
			t.Fatalf("collectDigestTags returned %d tags, want cap %d", len(got), MaxDigestTags)
		}
		want := []string{"t0", "t1", "t2", "t3", "t4", "t5"}
		if !slices.Equal(got, want) {
			t.Errorf("collectDigestTags = %v, want first %d %v", got, MaxDigestTags, want)
		}
	})

	t.Run("nil entity and non-positive cap return nil", func(t *testing.T) {
		if got := collectDigestTags(nil, MaxDigestTags); got != nil {
			t.Errorf("collectDigestTags(nil) = %v, want nil", got)
		}
		e := &gtypes.EntityState{ID: id, Triples: []message.Triple{tagTriple("x")}}
		if got := collectDigestTags(e, 0); got != nil {
			t.Errorf("collectDigestTags(e, 0) = %v, want nil", got)
		}
	})
}

// TestSynthesizeAnswer_MirrorsTags is the owner tenet from tasks.md 3.2: the
// template (LLM-absent) floor must render the same tag context the LLM prompt
// shows, so a degraded answer never silently drops theme vocabulary.
func TestSynthesizeAnswer_MirrorsTags(t *testing.T) {
	summaries := []CommunitySummary{
		{
			Summary:     "Emergency procedures.",
			MemberCount: 3,
			Relevance:   1.0,
			Entities: []EntityDigest{
				{ID: "e1", Type: "document", Label: "Evac Plan", Tags: []string{"emergency", "evacuation"}},
				{ID: "e2", Type: "document", Label: "Plain"}, // no tags
			},
		},
	}
	out := synthesizeAnswer(summaries, 3)
	if !strings.Contains(out, "Evac Plan [document] {tags: emergency, evacuation}") {
		t.Errorf("template floor must mirror the tag suffix; got:\n%s", out)
	}
	if !strings.Contains(out, "Plain [document]") || strings.Contains(out, "Plain [document] {tags") {
		t.Errorf("tagless rep must render bare in the template floor; got:\n%s", out)
	}
}

package graphquery

import (
	"fmt"
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
		{"local.dev.agent.model-registry.endpoint.semembed", "endpoint"},
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
			ID: "local.dev.agent.model-registry.endpoint.semembed",
			Triples: []message.Triple{
				{Subject: "local.dev.agent.model-registry.endpoint.semembed", Predicate: semantictest.Predicate(t, "agent", "model", "name"), Object: "nomic-embed-text"},
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
		"local.dev.agent.model-registry.endpoint.semembed",
	}
	scores := map[string]float64{
		"local.dev.game.board1.quest.3416ee6c": 0.87,
		"local.dev.game.board1.agent.675e7820": 0.82,
	}
	labels := map[string]string{
		"local.dev.game.board1.quest.3416ee6c":             "Dragon Slayer Quest",
		"local.dev.agent.model-registry.endpoint.semembed": "nomic-embed-text",
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
	got := filterEntityIDsByType(ids, []string{"sensor"})
	if len(got) != 2 {
		t.Errorf("filterEntityIDsByType() returned %d, want 2", len(got))
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

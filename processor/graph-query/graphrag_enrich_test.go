package graphquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/graph/clustering"
)

// findCommunitiesForEntities walks EVERY level, so it is the one producer that can
// emit two summaries sharing a community ID. Each must carry its own level: the
// summary is later re-resolved via GetCommunity(summary.Level, summary.CommunityID),
// so a dropped stamp resolves silently to level 0 — indistinguishable from correct
// behavior in any level-0-only test, because 0 is the Go zero value.
func TestFindCommunitiesForEntities_StampsEachSummaryWithItsOwnLevel(t *testing.T) {
	const collidingID = "acme.ops.robotics.gcs.drone.001"

	cache := NewCommunityCache(slog.New(slog.NewTextHandler(io.Discard, nil)))
	put := func(level int, members []string) {
		t.Helper()
		b, err := json.Marshal(&clustering.Community{ID: collidingID, Level: level, Members: members})
		if err != nil {
			t.Fatal(err)
		}
		cache.handleUpdate(fmt.Sprintf("%d.%s", level, collidingID), b)
	}
	put(0, []string{"e1"})
	put(1, []string{"e1", "e2"})

	c := &Component{
		communityCache: cache,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	got := c.findCommunitiesForEntities([]string{"e1"})
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2 (one per level sharing the ID)", len(got))
	}

	byLevel := map[int]CommunitySummary{}
	for _, s := range got {
		byLevel[s.Level] = s
	}
	for _, want := range []struct{ level, members int }{{0, 1}, {1, 2}} {
		s, ok := byLevel[want.level]
		if !ok {
			t.Fatalf("no summary carried level %d — levels seen: %v", want.level, byLevel)
		}
		if s.MemberCount != want.members {
			t.Errorf("level %d: MemberCount = %d, want %d", want.level, s.MemberCount, want.members)
		}
	}
}

// Community IDs are seed entity IDs and every level re-derives its partition from
// the same entity set, so the SAME id legitimately names different communities at
// different levels. enrichCommunitySummaries must therefore keep each summary's
// rep entities separate.
//
// Round-6 review found it keyed them by bare community ID, so two same-id summaries
// both received the LAST level's rep entities while each kept its own MemberCount —
// a digest stitched from two different communities, returned to agents on
// community_summaries[].entities[] and fed into the answer prompt. This became
// reachable exactly when the round-5 cache fix stopped collapsing levels.
func TestEnrichCommunitySummaries_CollidingIDsKeepTheirOwnRepEntities(t *testing.T) {
	const collidingID = "acme.ops.robotics.gcs.drone.001"

	cache := NewCommunityCache(slog.New(slog.NewTextHandler(io.Discard, nil)))
	put := func(level int, members, reps []string) {
		t.Helper()
		b, err := json.Marshal(&clustering.Community{
			ID: collidingID, Level: level, Members: members, RepEntities: reps,
		})
		if err != nil {
			t.Fatal(err)
		}
		cache.handleUpdate(fmt.Sprintf("%d.%s", level, collidingID), b)
	}

	// Same ID at two levels, deliberately distinct membership AND rep entities.
	put(0, []string{"e1"}, []string{"rep-level0"})
	put(1, []string{"e1", "e2"}, []string{"rep-level1"})

	// No natsClient: resolveEntityLabels degrades to unlabelled digests, which is
	// fine here — this test is about WHICH community's rep entities each summary
	// gets, not about their labels.
	c := &Component{
		communityCache: cache,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	summaries := []CommunitySummary{
		{CommunityID: collidingID, Level: 0},
		{CommunityID: collidingID, Level: 1},
	}

	got, err := c.enrichCommunitySummaries(context.Background(), summaries)
	if err != nil {
		t.Fatalf("enrichCommunitySummaries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2", len(got))
	}

	// Each summary's MemberCount and Entities must come from the SAME community.
	for _, tc := range []struct {
		level          int
		wantMemberCnt  int
		wantRepEntitiy string
	}{
		{level: 0, wantMemberCnt: 1, wantRepEntitiy: "rep-level0"},
		{level: 1, wantMemberCnt: 2, wantRepEntitiy: "rep-level1"},
	} {
		s := got[tc.level] // summaries were supplied in level order
		if s.Level != tc.level {
			t.Fatalf("summary order changed: got level %d at index %d", s.Level, tc.level)
		}
		if s.MemberCount != tc.wantMemberCnt {
			t.Errorf("level %d: MemberCount = %d, want %d", tc.level, s.MemberCount, tc.wantMemberCnt)
		}
		if len(s.Entities) != 1 {
			t.Fatalf("level %d: got %d entity digests, want 1", tc.level, len(s.Entities))
		}
		if s.Entities[0].ID != tc.wantRepEntitiy {
			t.Errorf("level %d: rep entity = %q, want %q — a summary carrying another level's "+
				"rep entities is a digest stitched from two different communities",
				tc.level, s.Entities[0].ID, tc.wantRepEntitiy)
		}
	}
}

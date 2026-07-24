package scenarios

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph/clustering"
)

// comm is a tiny helper for building synthetic level-0 communities. Members are
// full 6-part EntityIDs whose LAST segment is the source-record id the
// diagnostic matches on.
func comm(id string, level int, members ...string) *clustering.Community {
	return &clustering.Community{ID: id, Level: level, Members: members}
}

// eid renders a 6-part warehouse EntityID whose last segment is the given
// source-record id — mirrors the real ingest shape
// (c360.logistics.maintenance.work.completed.maint-003).
func eid(recordID string) string {
	return "c360.logistics.maintenance.work.completed." + recordID
}

func TestLastSegment(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"c360.logistics.maintenance.work.completed.maint-003", "maint-003"},
		{"c360.logistics.content.document.shipping.doc-shipping-001", "doc-shipping-001"},
		{"c360.logistics.observation.record.high.obs-001", "obs-001"},
		{"no-dots-here", "no-dots-here"},
		{"", ""},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, lastSegment(tt.in), "lastSegment(%q)", tt.in)
	}
}

// TestGradeColocation covers the four required shapes: all-co-located,
// fully-scattered, partial, and expected-entity-not-in-any-community. Each runs
// against a SYNTHETIC []*clustering.Community — no live run required.
func TestGradeColocation(t *testing.T) {
	tests := []struct {
		name  string
		exp   colocationExpectation
		comms []*clustering.Community

		wantExpected    int
		wantFound       int
		wantNotIn       int
		wantDistinct    int
		wantPlurality   string
		wantPlurCount   int
		wantShare       float64
		wantNotInList   []string
		wantMultiMember int
	}{
		{
			name: "all co-located in one community => plurality 1.0",
			exp:  colocationExpectation{id: "q", expected: []string{"maint-001", "maint-008", "obs-002"}},
			comms: []*clustering.Community{
				comm("0.c-A", 0, eid("maint-001"), eid("maint-008"), eid("obs-002"), eid("noise-1")),
			},
			wantExpected:  3,
			wantFound:     3,
			wantNotIn:     0,
			wantDistinct:  1,
			wantPlurality: "0.c-A",
			wantPlurCount: 3,
			wantShare:     1.0,
		},
		{
			name: "fully scattered => each in its own community, plurality 1/3",
			exp:  colocationExpectation{id: "q", expected: []string{"maint-001", "maint-008", "obs-002"}},
			comms: []*clustering.Community{
				comm("0.c-A", 0, eid("maint-001")),
				comm("0.c-B", 0, eid("maint-008")),
				comm("0.c-C", 0, eid("obs-002")),
			},
			wantExpected: 3,
			wantFound:    3,
			wantNotIn:    0,
			wantDistinct: 3,
			// ties break on sorted-first community id
			wantPlurality: "0.c-A",
			wantPlurCount: 1,
			wantShare:     1.0 / 3.0,
		},
		{
			name: "partial: two in one community, one in another => plurality 2/3",
			exp:  colocationExpectation{id: "q", expected: []string{"maint-001", "maint-008", "obs-002"}},
			comms: []*clustering.Community{
				comm("0.c-A", 0, eid("maint-001"), eid("maint-008")),
				comm("0.c-B", 0, eid("obs-002")),
			},
			wantExpected:  3,
			wantFound:     3,
			wantNotIn:     0,
			wantDistinct:  2,
			wantPlurality: "0.c-A",
			wantPlurCount: 2,
			wantShare:     2.0 / 3.0,
		},
		{
			name: "expected entity not in any community => not_in_partition, share over found only",
			exp:  colocationExpectation{id: "q", expected: []string{"maint-001", "maint-008", "ghost-999"}},
			comms: []*clustering.Community{
				comm("0.c-A", 0, eid("maint-001"), eid("maint-008")),
			},
			wantExpected:  3,
			wantFound:     2,
			wantNotIn:     1,
			wantDistinct:  1,
			wantPlurality: "0.c-A",
			wantPlurCount: 2,
			wantShare:     1.0, // 2/2 found are co-located; the ghost is not_in_partition
			wantNotInList: []string{"ghost-999"},
		},
		{
			name: "no expected entity present => found 0, share 0, all not_in_partition",
			exp:  colocationExpectation{id: "q", expected: []string{"ghost-1", "ghost-2"}},
			comms: []*clustering.Community{
				comm("0.c-A", 0, eid("maint-001")),
			},
			wantExpected:  2,
			wantFound:     0,
			wantNotIn:     2,
			wantDistinct:  0,
			wantPlurality: "",
			wantPlurCount: 0,
			wantShare:     0.0,
			wantNotInList: []string{"ghost-1", "ghost-2"},
		},
		{
			name: "level-1 community is ignored (only level-0 partitions co-locate)",
			exp:  colocationExpectation{id: "q", expected: []string{"maint-001", "maint-008"}},
			comms: []*clustering.Community{
				comm("0.c-A", 0, eid("maint-001")),
				// A level-1 rollup holding maint-008 must NOT count as co-location.
				comm("1.c-super", 1, eid("maint-008"), eid("maint-001")),
			},
			wantExpected:  2,
			wantFound:     1,
			wantNotIn:     1,
			wantDistinct:  1,
			wantPlurality: "0.c-A",
			wantPlurCount: 1,
			wantShare:     1.0,
			wantNotInList: []string{"maint-008"},
		},
		{
			name: "exact-segment match: maint-1 must NOT match maint-16",
			exp:  colocationExpectation{id: "q", expected: []string{"maint-1"}},
			comms: []*clustering.Community{
				comm("0.c-A", 0, eid("maint-16")),
			},
			wantExpected:  1,
			wantFound:     0,
			wantNotIn:     1,
			wantDistinct:  0,
			wantPlurality: "",
			wantPlurCount: 0,
			wantShare:     0.0,
			wantNotInList: []string{"maint-1"},
		},
		{
			name: "abnormal multi-membership: entity in two level-0 communities is recorded",
			exp:  colocationExpectation{id: "q", expected: []string{"maint-001"}},
			comms: []*clustering.Community{
				comm("0.c-B", 0, eid("maint-001")),
				comm("0.c-A", 0, eid("maint-001")),
			},
			wantExpected: 1,
			wantFound:    1,
			wantNotIn:    0,
			wantDistinct: 1,
			// deterministic sorted-first assignment => 0.c-A
			wantPlurality:   "0.c-A",
			wantPlurCount:   1,
			wantShare:       1.0,
			wantMultiMember: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := buildLevel0ColocationIndex(tt.comms)
			g := gradeColocation(tt.exp, idx)

			require.Equal(t, tt.wantExpected, g.ExpectedCount, "expected_count")
			require.Equal(t, tt.wantFound, g.FoundCount, "found_count")
			require.Equal(t, tt.wantNotIn, g.NotInPartitionCount, "not_in_partition_count")
			require.Equal(t, tt.wantDistinct, g.DistinctCommunities, "distinct_communities")
			require.Equal(t, tt.wantPlurality, g.PluralityCommunity, "plurality_community_id")
			require.Equal(t, tt.wantPlurCount, g.PluralityCount, "plurality_count")
			require.InDelta(t, tt.wantShare, g.PluralityShare, 1e-9, "plurality_share")
			if tt.wantNotInList != nil {
				require.Equal(t, tt.wantNotInList, g.NotInPartition, "not_in_partition list")
			}
			require.Len(t, g.MultiMembership, tt.wantMultiMember, "multi_membership")
		})
	}
}

// TestGradeColocationMatchesCarryFullEntityID proves the printed member id is the
// full 6-part EntityID (so a fixture/corpus id-mismatch is visible in the live
// run), and that matches are sorted by expected id for stable output.
func TestGradeColocationMatchesCarryFullEntityID(t *testing.T) {
	exp := colocationExpectation{id: "q", expected: []string{"obs-002", "maint-001"}}
	idx := buildLevel0ColocationIndex([]*clustering.Community{
		comm("0.c-A", 0, eid("maint-001"), eid("obs-002")),
	})
	g := gradeColocation(exp, idx)

	require.Len(t, g.Matches, 2)
	// Sorted by expected id: maint-001 before obs-002.
	require.Equal(t, "maint-001", g.Matches[0].Expected)
	require.Equal(t, eid("maint-001"), g.Matches[0].FullEntityID)
	require.Equal(t, "obs-002", g.Matches[1].Expected)
	require.Equal(t, eid("obs-002"), g.Matches[1].FullEntityID)
}

// TestGradeColocationDedupesExpected locks the dedupe guard: a fixture that
// accidentally repeats an expected id must count it ONCE, not inflate
// found/plurality.
func TestGradeColocationDedupesExpected(t *testing.T) {
	exp := colocationExpectation{id: "dup", expected: []string{"maint-003", "maint-003", "maint-009"}}
	comms := []*clustering.Community{
		comm("c1", 0, eid("maint-003"), eid("maint-009")),
	}
	g := gradeColocation(exp, buildLevel0ColocationIndex(comms))
	require.Equal(t, 2, g.FoundCount, "duplicate expected id must not double-count")
	require.Equal(t, 2, g.PluralityCount)
	require.InDelta(t, 1.0, g.PluralityShare, 1e-9)
}

// TestAggregateColocation checks the mean is taken ONLY over queries with found>0
// and that entities_not_in_community totals across all queries.
func TestAggregateColocation(t *testing.T) {
	grades := []colocationGrade{
		{ID: "a", FoundCount: 3, PluralityShare: 1.0, NotInPartitionCount: 0},
		{ID: "b", FoundCount: 2, PluralityShare: 0.5, NotInPartitionCount: 1},
		{ID: "c", FoundCount: 0, PluralityShare: 0.0, NotInPartitionCount: 2}, // excluded from mean
	}
	agg := aggregateColocation(grades, 7)

	require.Equal(t, 3, agg.totalQueries)
	require.Equal(t, 2, agg.includedQueries) // c has found=0, excluded
	// mean over a(1.0) + b(0.5) / 2 = 0.75
	require.InDelta(t, 0.75, agg.mean, 1e-9)
	require.Equal(t, 3, agg.entitiesNotInCommunity) // 0 + 1 + 2
	require.Equal(t, 7, agg.level0Communities)      // partition cardinality carried through
}

// TestAggregateColocationEmpty covers the found=0 case: no query resolved any
// entity => mean 0, included 0, no divide-by-zero.
func TestAggregateColocationEmpty(t *testing.T) {
	grades := []colocationGrade{
		{ID: "a", FoundCount: 0, NotInPartitionCount: 2},
	}
	agg := aggregateColocation(grades, 1)
	require.Equal(t, 0, agg.includedQueries)
	require.InDelta(t, 0.0, agg.mean, 1e-9)
	require.Equal(t, 2, agg.entitiesNotInCommunity)
	require.Equal(t, 1, agg.level0Communities) // trust-guard value survives even with no found entities
}

// TestPartitionColocationExpectationsMirrorThematic locks the B2 fixtures to the
// B0 thematicQuestions by id, so the two tables always line up 1:1 by row.
func TestPartitionColocationExpectationsMirrorThematic(t *testing.T) {
	require.Len(t, partitionColocationExpectations, len(thematicQuestions),
		"co-location fixtures must mirror thematicQuestions 1:1")

	thematicIDs := make(map[string]struct{}, len(thematicQuestions))
	for _, q := range thematicQuestions {
		thematicIDs[q.id] = struct{}{}
	}
	for _, e := range partitionColocationExpectations {
		_, ok := thematicIDs[e.id]
		require.Truef(t, ok, "co-location fixture id %q has no matching thematicQuestions id", e.id)
		require.NotEmpty(t, e.expected, "co-location fixture %q must list expected entities", e.id)
	}
}

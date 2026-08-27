package types

import (
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEntityIDKeyOrderIsSystemBeforeDomain pins the canonical order
// org.platform.system.domain.type.instance (ADR-102 d1): the typed struct
// serializes in that order and parsing assigns every field from its named
// position.
func TestEntityIDKeyOrderIsSystemBeforeDomain(t *testing.T) {
	t.Parallel()

	eid := EntityID{Org: "acme", Platform: "dep1", System: "src", Domain: "git", Type: "commit", Instance: "a1"}
	const want = "acme.dep1.src.git.commit.a1"
	require.Equal(t, want, eid.Key())

	parsed, err := ParseEntityID(want)
	require.NoError(t, err)
	assert.Equal(t, "acme", parsed.Org)
	assert.Equal(t, "dep1", parsed.Platform)
	assert.Equal(t, "src", parsed.System)
	assert.Equal(t, "git", parsed.Domain)
	assert.Equal(t, "commit", parsed.Type)
	assert.Equal(t, "a1", parsed.Instance)
	assert.Equal(t, eid, parsed)
	assert.Equal(t, want, parsed.Key())
}

// TestPrefixLevelsAreNamed pins the prefix-level vocabulary: 2 = deployment,
// 3 = source (the federation triple), 4 = taxonomy, 5 = type.
func TestPrefixLevelsAreNamed(t *testing.T) {
	t.Parallel()

	eid := EntityID{Org: "acme", Platform: "dep1", System: "src", Domain: "git", Type: "commit", Instance: "a1"}
	assert.Equal(t, "acme.dep1", eid.DeploymentPrefix())
	assert.Equal(t, "acme.dep1.src", eid.SourcePrefix())
	assert.Equal(t, "acme.dep1.src.git", eid.TaxonomyPrefix())
	assert.Equal(t, "acme.dep1.src.git.commit", eid.TypePrefix())

	for level, want := range map[int]string{
		2: "acme.dep1",
		3: "acme.dep1.src",
		4: "acme.dep1.src.git",
		5: "acme.dep1.src.git.commit",
	} {
		got, err := eid.PrefixLevel(level)
		require.NoError(t, err, "level %d", level)
		assert.Equal(t, want, got, "level %d", level)
	}
	for _, level := range []int{0, 1, 6, 7} {
		got, err := eid.PrefixLevel(level)
		assert.Empty(t, got, "level %d", level)
		assertEntityIDContractError(t, err, ErrorCodeEntityIDPrefixInvalid, EntityIDReasonArity, nil)
	}

	other := EntityID{Org: "acme", Platform: "dep1", System: "src", Domain: "media", Type: "video", Instance: "v9"}
	assert.True(t, eid.IsSameSource(other))
	assert.False(t, eid.IsSameSource(EntityID{Org: "acme", Platform: "dep1", System: "other", Domain: "git", Type: "commit", Instance: "a1"}))
}

// TestTaxonomyAcrossSourcesIsPatternNotPrefix pins that "every git entity of
// this deployment regardless of source" is an exact-arity wildcard pattern,
// never a prefix.
func TestTaxonomyAcrossSourcesIsPatternNotPrefix(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateEntityIDPattern("acme.dep1.*.git.*.*"))
	// entity-id-audit:classify intentional-malformed "acme.dep1.*.git" line=76 column=32 surface=go-call:ValidateEntityIDPrefix entity_id_prefix_invalid:first_byte a taxonomy across sources is not expressible as a prefix
	err := ValidateEntityIDPrefix("acme.dep1.*.git")
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, ErrorCodeEntityIDPrefixInvalid, classified.Code)
}

// TestMaxAuthorityPairBytesDerivesFromLongestFamily pins that the authority
// pair budget falls out of the framework identity family table (ADR-102
// amends ADR-076 d2) and is never hand-copied: the rule trigger family
// (`rules.graph.trigger.` + 64 hex + two separators = 86 bytes) binds today.
func TestMaxAuthorityPairBytesDerivesFromLongestFamily(t *testing.T) {
	t.Parallel()

	longest := LongestFrameworkIdentityFamily()
	assert.Equal(t, "rule-trigger", longest.Name)
	assert.Equal(t, 86, longest.FixedBytes())
	assert.Equal(t, MaxEntityIDBytes-longest.FixedBytes(), MaxAuthorityPairBytes())
	assert.Equal(t, 170, MaxAuthorityPairBytes())

	digest := strings.Repeat("0", 64)
	id, err := longest.EntityID("acme", "dep1", digest)
	require.NoError(t, err)
	assert.Equal(t, "acme.dep1.rules.graph.trigger."+digest, id)
	assert.Equal(t, len("acme")+len("dep1")+longest.FixedBytes(), len(id))
	for _, bad := range [][3]string{{"", "dep1", digest}, {"acme", "de.p1", digest}, {"acme", "dep1", "short"}, {"acme", "-dep1", digest}} {
		if id, err := longest.EntityID(bad[0], bad[1], bad[2]); err == nil || id != "" {
			t.Fatalf("EntityID(%q, %q, %q) = (%q, %v), want fail closed", bad[0], bad[1], bad[2], id, err)
		}
	}
	for _, family := range FrameworkIdentityFamilies() {
		assert.LessOrEqual(t, family.FixedBytes(), longest.FixedBytes(), family.Name)
		assert.True(t, IsFrameworkEntityDomain(family.Domain), "%s domain %q must be reserved", family.Name, family.Domain)
	}
}

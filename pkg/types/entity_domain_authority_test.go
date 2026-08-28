package types

import (
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertDomainUndelegated(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified), "error %v is not classified", err)
	assert.Equal(t, ErrorCodeEntityIDAuthorityInvalid, classified.Code)
	assert.Equal(t, EntityIDReasonDomainUndelegated, classified.Detail[EntityIDDetailReason])
}

// TestEntityDomainAuthorityMirrorsPredicateAuthority pins the delegated
// entity-domain authority on the vocabulary.PredicateAuthority pattern: an
// undelegated domain with an empty producer is rejected, an exact domain.type
// delegation admits only that type, and the rejection is coded.
func TestEntityDomainAuthorityMirrorsPredicateAuthority(t *testing.T) {
	t.Parallel()

	authority, err := NewEntityDomainAuthority(
		EntityDomainDelegation{Producer: "semsource", Domain: "git"},
		EntityDomainDelegation{Producer: "semdragon", Domain: "game", Type: "quest"},
	)
	require.NoError(t, err)

	assertDomainUndelegated(t, authority.Authorize("", "media", "video"))
	assertDomainUndelegated(t, authority.Authorize("semsource", "media", "video"))
	assertDomainUndelegated(t, authority.Authorize("semdragon", "git", "commit"))

	require.NoError(t, authority.Authorize("semsource", "git", "commit"))
	require.NoError(t, authority.Authorize("semsource", "git", "repo"))
	require.NoError(t, authority.Authorize("semdragon", "game", "quest"))
	assertDomainUndelegated(t, authority.Authorize("semdragon", "game", "board"))

	var none *EntityDomainAuthority
	assertDomainUndelegated(t, none.Authorize("semsource", "git", "commit"))
}

// TestEntityDomainAuthorityReservedPassesForEveryProducer pins the
// framework-reserved set {agent, ops, graph}: reserved domains pass for an
// empty and for an arbitrary producer, with and without an authority.
func TestEntityDomainAuthorityReservedPassesForEveryProducer(t *testing.T) {
	t.Parallel()

	assert.ElementsMatch(t, []string{"agent", "ops", "graph"}, FrameworkEntityDomains())
	empty, err := NewEntityDomainAuthority()
	require.NoError(t, err)
	var none *EntityDomainAuthority
	for _, domain := range FrameworkEntityDomains() {
		assert.True(t, IsFrameworkEntityDomain(domain))
		for _, authority := range []*EntityDomainAuthority{none, empty} {
			require.NoError(t, authority.Authorize("", domain, "execution"), domain)
			require.NoError(t, authority.Authorize("anyone", domain, "anything"), domain)
		}
	}
	assert.False(t, IsFrameworkEntityDomain("gateddag"))
	assert.False(t, IsFrameworkEntityDomain(""))
}

// TestEntityDomainAuthorityPermitsSharedDomains pins the owner ruling of
// 2026-08-28 (superseding O-5): two producers MAY delegate one domain. The
// assertion is positive, not merely "no error" — each producer must actually
// be authorized for the shared taxonomy, which is the capability the retired
// exclusivity rule made unavailable.
func TestEntityDomainAuthorityPermitsSharedDomains(t *testing.T) {
	t.Parallel()

	authority, err := NewEntityDomainAuthority(
		EntityDomainDelegation{Producer: "semsource", Domain: "web"},
		EntityDomainDelegation{Producer: "semdragon", Domain: "web"},
	)
	require.NoError(t, err, "a shared taxonomy is permitted, not a composition rejection")
	require.NoError(t, authority.Authorize("semsource", "web", "page"))
	require.NoError(t, authority.Authorize("semdragon", "web", "doc"))
	require.Error(t, authority.Authorize("semteams", "web", "page"),
		"sharing a domain does not open it to an undelegated producer")
}

// TestEntityDomainAuthorityRejectsReservedAndMalformedDelegations pins the
// composition-time rejections that survive the 2026-08-28 ruling: a delegation
// of a reserved domain, an empty producer, and a non-canonical segment all
// refuse to build.
func TestEntityDomainAuthorityRejectsReservedAndMalformedDelegations(t *testing.T) {
	t.Parallel()

	_, err := NewEntityDomainAuthority(
		EntityDomainDelegation{Producer: "semsource", Domain: "web"},
		EntityDomainDelegation{Producer: "semsource", Domain: "web", Type: "page"},
	)
	require.NoError(t, err, "one producer may hold a domain-wide and a domain.type delegation")

	for _, delegation := range []EntityDomainDelegation{
		{Producer: "product", Domain: "agent"},
		{Producer: "", Domain: "git"},
		{Producer: "product", Domain: ""},
		{Producer: "product", Domain: "-git"},
		{Producer: "product", Domain: "git", Type: "a.b"},
	} {
		_, err := NewEntityDomainAuthority(delegation)
		require.Error(t, err, "%+v", delegation)
	}
}

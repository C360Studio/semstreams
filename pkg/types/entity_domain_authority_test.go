package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFrameworkEntityDomainsIsTheClosedReservedSet pins the framework-reserved
// domain vocabulary {agent, ops, graph} (ADR-102 d4, ruled O-9). The audit's
// domain_unregistered rule consults it beside the registered delegations, so a
// token silently entering or leaving this set changes what the corpus accepts.
// The gated-DAG family re-slots UNDER `agent`; `gateddag` is not itself
// reserved.
func TestFrameworkEntityDomainsIsTheClosedReservedSet(t *testing.T) {
	t.Parallel()

	assert.ElementsMatch(t, []string{"agent", "ops", "graph"}, FrameworkEntityDomains())
	for _, domain := range FrameworkEntityDomains() {
		assert.True(t, IsFrameworkEntityDomain(domain), domain)
	}
	assert.False(t, IsFrameworkEntityDomain("gateddag"))
	assert.False(t, IsFrameworkEntityDomain(""))
	assert.False(t, IsFrameworkEntityDomain("web"))
}

// TestReservedInstanceTokensIsTheClosedContainerSet pins the hierarchy-container
// padding tokens reserved in the instance position until gh606 retires
// containers. graph/inference/hierarchy.go reads it to recognize a container
// entity, and the audit's instance_reserved rule reads it to reject a producer
// that mints one.
func TestReservedInstanceTokensIsTheClosedContainerSet(t *testing.T) {
	t.Parallel()

	assert.ElementsMatch(t, []string{"group", "container", "level"}, ReservedInstanceTokens())
	for _, token := range ReservedInstanceTokens() {
		assert.True(t, IsReservedInstanceToken(token), token)
	}
	assert.False(t, IsReservedInstanceToken("drone"))
	assert.False(t, IsReservedInstanceToken(""))
}

// TestEntityDomainDelegationIsADeclarationNotAPolicy pins what survives the
// owner ruling of 2026-08-28. EntityDomainDelegation is a declaration the
// entity-ID corpus audit AST-scans for its registered set; the
// EntityDomainAuthority/Authorize policy that once consumed it is deleted,
// because two producers sharing one domain is permitted and there was nothing
// left to authorize. The type therefore has no constructor and no validation
// of its own — its only reader is the audit, over source text.
func TestEntityDomainDelegationIsADeclarationNotAPolicy(t *testing.T) {
	t.Parallel()

	shared := []EntityDomainDelegation{
		{Producer: "semsource", Domain: "web"},
		{Producer: "semdragon", Domain: "web"},
		{Producer: "semsource", Domain: "git", Type: "commit"},
	}
	for _, delegation := range shared {
		assert.NotEmpty(t, delegation.Producer)
		assert.False(t, IsFrameworkEntityDomain(delegation.Domain),
			"a product delegates its own taxonomy, never a framework-reserved one")
	}
	assert.Equal(t, shared[0].Domain, shared[1].Domain,
		"two producers may declare one domain: overlap is permitted, not a rejection")
}

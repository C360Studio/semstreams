package composition

import (
	"fmt"
	"sort"
	"strings"

	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// entityDomainsComponent is the composition-level subject an entity-domain
// finding names, following TypeConfigInvalid's use of "config": the finding is
// about the composition, not about one instance.
const entityDomainsComponent = "entity-domains"

// entityDomainOverlaps reports each entity domain that more than one producer
// delegates. Overlap is PERMITTED (owner ruling 2026-08-28, superseding #1095
// O-5): the taxonomy vocabulary is shared, `system` at position 3 keeps the
// IDs distinct, and ADR-099 level 0 is source x taxonomy so the communities
// stay distinct too. The operator composing two products is the one who cannot
// otherwise see it, so it is an observation at warning severity — a boot WARN
// was set aside because it fires on the intended case and trains operators to
// ignore it.
//
// One producer repeating its own domain, or narrowing it with a Type, is not
// overlap. Producers and domains are reported in sorted order so two runs over
// equal inputs marshal to byte-equal JSON.
func entityDomainOverlaps(delegations []semtypes.EntityDomainDelegation) []Finding {
	producersByDomain := map[string]map[string]bool{}
	for _, delegation := range delegations {
		producer := strings.TrimSpace(delegation.Producer)
		domain := strings.TrimSpace(delegation.Domain)
		if producer == "" || domain == "" {
			continue // NewEntityDomainAuthority refuses these; they are not overlap
		}
		if producersByDomain[domain] == nil {
			producersByDomain[domain] = map[string]bool{}
		}
		producersByDomain[domain][producer] = true
	}

	domains := make([]string, 0, len(producersByDomain))
	for domain, producers := range producersByDomain {
		if len(producers) > 1 {
			domains = append(domains, domain)
		}
	}
	sort.Strings(domains)

	findings := make([]Finding, 0, len(domains))
	for _, domain := range domains {
		producers := make([]string, 0, len(producersByDomain[domain]))
		for producer := range producersByDomain[domain] {
			producers = append(producers, producer)
		}
		sort.Strings(producers)
		findings = append(findings, Finding{
			Type:      TypeEntityDomainOverlap,
			Severity:  severityOf(TypeEntityDomainOverlap, nil),
			Component: entityDomainsComponent,
			Message: fmt.Sprintf("entity domain %q is delegated by %d producers: %s",
				domain, len(producers), strings.Join(producers, ", ")),
			Suggestions: []string{
				"Sharing a taxonomy is permitted: position 3 (system) keeps the entity IDs distinct, and ADR-099 level 0 is source x taxonomy so the communities stay distinct",
				fmt.Sprintf("Query one producer's slice with the source prefix, and every producer's with the pattern org.platform.*.%s.*.*", domain),
				"If this overlap was not intended, rename one producer's domain token",
			},
		})
	}
	return findings
}

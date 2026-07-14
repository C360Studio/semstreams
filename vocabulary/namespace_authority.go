package vocabulary

import (
	"fmt"
	"strings"
)

// PredicateNamespace is an exact vocabulary namespace delegation. Category is
// empty for a domain-wide delegation and non-empty for a domain.category
// delegation. Wildcards and property-level namespaces are not supported.
type PredicateNamespace struct {
	Domain   string
	Category string
}

// String returns the exact delegated namespace.
func (n PredicateNamespace) String() string {
	if n.Category == "" {
		return n.Domain
	}
	return n.Domain + "." + n.Category
}

// NamespaceDelegation grants one named producer authority to author otherwise
// undeclared predicates in an exact domain or domain.category namespace.
// Producer identity is supplied by the trusted integration boundary; it is
// never inferred from Triple.Source or a payload MessageType.
type NamespaceDelegation struct {
	Producer  string
	Namespace string
}

// PredicateAuthority is an immutable authoring policy built from explicit
// namespace delegations. It is separate from graph predicate-group ownership:
// passing this check says who may mint a semantic name, not who may mutate a
// particular entity.
type PredicateAuthority struct {
	delegations map[string][]PredicateNamespace
}

// RequireDeclaredPredicate enforces the v1 first-party authoring policy. The
// predicate must be canonical and registered by a trusted composition-root or
// vocabulary package before a configuration, workflow, ownership claim, or
// generated mutation surface may expose it. Runtime graph persistence uses the
// syntax-only final-candidate seam instead; caller-controlled fields never
// manufacture declaration authority.
func RequireDeclaredPredicate(predicate string) error {
	if _, err := ParsePredicate(predicate); err != nil {
		return err
	}
	if GetPredicateMetadata(predicate) == nil {
		return fmt.Errorf("predicate %q is canonical but not declared in the vocabulary registry", predicate)
	}
	return nil
}

// NewPredicateAuthority validates and installs exact namespace delegations.
// An empty delegation list still permits predicates declared in the vocabulary
// registry.
func NewPredicateAuthority(delegations ...NamespaceDelegation) (*PredicateAuthority, error) {
	authority := &PredicateAuthority{delegations: make(map[string][]PredicateNamespace)}
	for _, delegation := range delegations {
		producer := strings.TrimSpace(delegation.Producer)
		if producer == "" {
			return nil, fmt.Errorf("predicate namespace delegation has empty producer")
		}
		namespace, err := ParsePredicateNamespace(delegation.Namespace)
		if err != nil {
			return nil, fmt.Errorf("predicate namespace delegation for producer %q: %w", producer, err)
		}
		authority.delegations[producer] = append(authority.delegations[producer], namespace)
	}
	return authority, nil
}

// ParsePredicateNamespace validates an exact domain or domain.category
// namespace using the same lower-kebab segment grammar and byte bound as stored
// predicates.
func ParsePredicateNamespace(namespace string) (PredicateNamespace, error) {
	segments := strings.Split(namespace, ".")
	if len(segments) != 1 && len(segments) != 2 {
		return PredicateNamespace{}, fmt.Errorf("predicate namespace %q must be an exact domain or domain.category", namespace)
	}
	for i, segment := range segments {
		if err := validatePredicateSegment(namespace, segment, i); err != nil {
			return PredicateNamespace{}, fmt.Errorf("predicate namespace %q is invalid: %w", namespace, err)
		}
	}

	parsed := PredicateNamespace{Domain: segments[0]}
	if len(segments) == 2 {
		parsed.Category = segments[1]
	}
	return parsed, nil
}

// Authorize validates predicate syntax and declaration authority. Registered
// vocabulary predicates are declared for every producer, including an empty
// producer. An unregistered predicate requires a non-empty producer with a
// matching exact domain or domain.category delegation.
func (a *PredicateAuthority) Authorize(producer, predicate string) error {
	parts, err := ParsePredicate(predicate)
	if err != nil {
		return err
	}
	if GetPredicateMetadata(predicate) != nil {
		return nil
	}

	producer = strings.TrimSpace(producer)
	if producer == "" {
		return fmt.Errorf("predicate %q is not registered and anonymous producers cannot use delegated namespaces", predicate)
	}
	if a == nil {
		return fmt.Errorf("predicate %q is not registered and producer %q has no namespace delegation", predicate, producer)
	}
	for _, namespace := range a.delegations[producer] {
		if namespace.Domain == parts.Domain &&
			(namespace.Category == "" || namespace.Category == parts.Category) {
			return nil
		}
	}
	return fmt.Errorf("predicate %q is not registered and producer %q has no matching namespace delegation", predicate, producer)
}

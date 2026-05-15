package export

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/c360studio/semstreams/vocabulary"
)

// defaultPrefixes maps short prefix names to their namespace IRIs.
// Only prefixes that are actually used in output are emitted.
//
// The baseline set ships with the package; vocabulary sub-packages
// (e.g. vocabulary/sosa, vocabulary/swe, vocabulary/oms) extend it
// from init() via Register. Read access is mutex-guarded because
// downstream code may register additional namespaces from init in
// any package import order, and exporters snapshot the map at
// newPrefixMap time.
var (
	defaultPrefixesMu sync.RWMutex
	defaultPrefixes   = map[string]string{
		"rdf":    "http://www.w3.org/1999/02/22-rdf-syntax-ns#",
		"owl":    "http://www.w3.org/2002/07/owl#",
		"skos":   "http://www.w3.org/2004/02/skos/core#",
		"rdfs":   "http://www.w3.org/2000/01/rdf-schema#",
		"dc":     "http://purl.org/dc/terms/",
		"schema": "https://schema.org/",
		"foaf":   "http://xmlns.com/foaf/0.1/",
		"prov":   "http://www.w3.org/ns/prov#",
		"ssn":    "http://www.w3.org/ns/ssn/",
		"sosa":   "http://www.w3.org/ns/sosa/",
		"xsd":    "http://www.w3.org/2001/XMLSchema#",
		"ss":     vocabulary.SemStreamsBase + "/",
	}
)

// Register adds a prefix → namespace binding to the export prefix
// table. It is idempotent: registering the same prefix with the same
// namespace twice is a no-op. Registering the same prefix with a
// different namespace returns an error so collisions surface loudly
// rather than silently shadowing.
//
// Intended use is from a package init() in a vocabulary sub-package
// (e.g. vocabulary/sosa), so registrations land before any exporter
// runs. The function is safe to call at runtime as well, but mid-
// export registrations will only affect prefix maps constructed
// after the call returns.
func Register(prefix, namespace string) error {
	if prefix == "" {
		return fmt.Errorf("vocabulary/export: prefix must not be empty")
	}
	if namespace == "" {
		return fmt.Errorf("vocabulary/export: namespace must not be empty for prefix %q", prefix)
	}
	defaultPrefixesMu.Lock()
	defer defaultPrefixesMu.Unlock()
	if existing, ok := defaultPrefixes[prefix]; ok {
		if existing == namespace {
			return nil
		}
		return fmt.Errorf("vocabulary/export: prefix %q already bound to %q, cannot rebind to %q", prefix, existing, namespace)
	}
	defaultPrefixes[prefix] = namespace
	return nil
}

// snapshotDefaults returns a copy of the current prefix table under
// the read lock. Callers iterate the returned map without holding
// any lock.
func snapshotDefaults() map[string]string {
	defaultPrefixesMu.RLock()
	defer defaultPrefixesMu.RUnlock()
	out := make(map[string]string, len(defaultPrefixes))
	maps.Copy(out, defaultPrefixes)
	return out
}

// prefixMap tracks namespaces and which prefixes are actually used.
type prefixMap struct {
	prefixes map[string]string // prefix -> namespace
	used     map[string]bool   // prefix -> whether it appeared in output
}

// newPrefixMap creates a prefix map initialized with defaults, optionally
// overriding the ss: prefix if a custom base IRI is provided.
func newPrefixMap(base string) *prefixMap {
	defaults := snapshotDefaults()
	pm := &prefixMap{
		prefixes: defaults,
		used:     make(map[string]bool, len(defaults)),
	}
	// Override ss: if custom base
	if base != "" && base != vocabulary.SemStreamsBase {
		pm.prefixes["ss"] = ensureTrailingSlash(base)
	}
	return pm
}

// compact attempts to shorten a full IRI using known prefixes.
// Returns the compacted form (e.g., "owl:sameAs") and true if successful,
// or the original IRI and false if no prefix matches.
func (pm *prefixMap) compact(iri string) (string, bool) {
	// Try each prefix, preferring the longest namespace match
	var bestPrefix string
	var bestNS string
	for prefix, ns := range pm.prefixes {
		if strings.HasPrefix(iri, ns) && len(ns) > len(bestNS) {
			bestPrefix = prefix
			bestNS = ns
		}
	}
	if bestNS == "" {
		return iri, false
	}
	local := iri[len(bestNS):]
	// Only compact if local part is a valid CURIE local name (no slashes)
	if strings.Contains(local, "/") {
		return iri, false
	}
	pm.used[bestPrefix] = true
	return bestPrefix + ":" + local, true
}

// markUsed records a prefix as used without compacting.
func (pm *prefixMap) markUsed(prefix string) {
	pm.used[prefix] = true
}

// usedPrefixes returns the prefixes that were actually used, sorted alphabetically.
func (pm *prefixMap) usedPrefixes() []string {
	result := make([]string, 0, len(pm.used))
	for p := range pm.used {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}

// namespaceFor returns the namespace IRI for a given prefix.
func (pm *prefixMap) namespaceFor(prefix string) string {
	return pm.prefixes[prefix]
}

// usedPrefixMap returns a map of used prefix -> namespace, for JSON-LD @context.
func (pm *prefixMap) usedPrefixMap() map[string]string {
	result := make(map[string]string, len(pm.used))
	for p := range pm.used {
		result[p] = pm.prefixes[p]
	}
	return result
}

func ensureTrailingSlash(s string) string {
	if s == "" {
		return s
	}
	if s[len(s)-1] == '/' || s[len(s)-1] == '#' {
		return s
	}
	return s + "/"
}

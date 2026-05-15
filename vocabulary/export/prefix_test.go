package export

import (
	"testing"
)

func TestPrefixMap_Compact(t *testing.T) {
	pm := newPrefixMap("")

	tests := []struct {
		iri       string
		want      string
		compacted bool
	}{
		{"http://www.w3.org/2002/07/owl#sameAs", "owl:sameAs", true},
		{"http://www.w3.org/2004/02/skos/core#prefLabel", "skos:prefLabel", true},
		{"http://www.w3.org/2000/01/rdf-schema#label", "rdfs:label", true},
		{"http://purl.org/dc/terms/title", "dc:title", true},
		{"https://schema.org/name", "schema:name", true},
		{"http://xmlns.com/foaf/0.1/name", "foaf:name", true},
		{"http://www.w3.org/ns/prov#wasGeneratedBy", "prov:wasGeneratedBy", true},
		{"http://www.w3.org/ns/ssn/hasDeployment", "ssn:hasDeployment", true},
		{"http://www.w3.org/ns/sosa/observes", "sosa:observes", true},
		{"http://www.w3.org/2001/XMLSchema#integer", "xsd:integer", true},
		{"https://semstreams.semanticstream.ing/predicates/robotics/battery/level", "https://semstreams.semanticstream.ing/predicates/robotics/battery/level", false}, // has slashes in local part, not compactable
		{"http://example.org/unknown", "http://example.org/unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.iri, func(t *testing.T) {
			got, ok := pm.compact(tt.iri)
			if ok != tt.compacted {
				t.Errorf("compact(%q): got ok=%v, want %v", tt.iri, ok, tt.compacted)
			}
			if got != tt.want {
				t.Errorf("compact(%q) = %q, want %q", tt.iri, got, tt.want)
			}
		})
	}
}

func TestPrefixMap_UsedTracking(t *testing.T) {
	pm := newPrefixMap("")

	// Initially no prefixes used
	if len(pm.usedPrefixes()) != 0 {
		t.Fatalf("expected 0 used prefixes, got %d", len(pm.usedPrefixes()))
	}

	// Compact a few IRIs
	pm.compact("http://www.w3.org/2002/07/owl#sameAs")
	pm.compact("http://www.w3.org/2001/XMLSchema#integer")

	used := pm.usedPrefixes()
	if len(used) != 2 {
		t.Fatalf("expected 2 used prefixes, got %d: %v", len(used), used)
	}
	// Should be sorted
	if used[0] != "owl" || used[1] != "xsd" {
		t.Errorf("expected [owl xsd], got %v", used)
	}
}

func TestPrefixMap_CustomBase(t *testing.T) {
	pm := newPrefixMap("https://example.org")

	ns := pm.namespaceFor("ss")
	if ns != "https://example.org/" {
		t.Errorf("expected ss: namespace to be %q, got %q", "https://example.org/", ns)
	}
}

func TestPrefixMap_MarkUsed(t *testing.T) {
	pm := newPrefixMap("")
	pm.markUsed("xsd")

	used := pm.usedPrefixes()
	if len(used) != 1 || used[0] != "xsd" {
		t.Errorf("expected [xsd], got %v", used)
	}
}

func TestPrefixMap_UsedPrefixMap(t *testing.T) {
	pm := newPrefixMap("")
	pm.compact("http://www.w3.org/2002/07/owl#sameAs")

	m := pm.usedPrefixMap()
	if len(m) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m))
	}
	if m["owl"] != "http://www.w3.org/2002/07/owl#" {
		t.Errorf("expected owl namespace, got %q", m["owl"])
	}
}

func TestRegister_IdempotentSameNamespace(t *testing.T) {
	// Re-registering an existing default with the same namespace is a no-op.
	if err := Register("sosa", "http://www.w3.org/ns/sosa/"); err != nil {
		t.Fatalf("Register sosa same namespace: %v", err)
	}
}

func TestRegister_RejectsCollision(t *testing.T) {
	err := Register("sosa", "https://example.org/different/")
	if err == nil {
		t.Fatalf("expected error rebinding sosa to a different namespace")
	}
}

func TestRegister_RejectsEmpty(t *testing.T) {
	if err := Register("", "http://example.org/"); err == nil {
		t.Fatalf("expected error for empty prefix")
	}
	if err := Register("foo", ""); err == nil {
		t.Fatalf("expected error for empty namespace")
	}
}

func TestRegister_NewPrefixAppearsInSnapshot(t *testing.T) {
	// Pick a prefix unlikely to collide with the defaults or sub-packages.
	const probe = "test-register-probe"
	const ns = "https://example.org/test-register-probe/"
	if err := Register(probe, ns); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Cleanup is intentional: this is a registry of test fixtures and
	// the next test run would see the leftover. We have no Unregister
	// to keep the API surface narrow; reach into the private map.
	t.Cleanup(func() {
		defaultPrefixesMu.Lock()
		delete(defaultPrefixes, probe)
		defaultPrefixesMu.Unlock()
	})
	pm := newPrefixMap("")
	if got := pm.namespaceFor(probe); got != ns {
		t.Errorf("namespaceFor(%q) = %q, want %q", probe, got, ns)
	}
}

func TestEnsureTrailingSlash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.org", "https://example.org/"},
		{"https://example.org/", "https://example.org/"},
		{"http://www.w3.org/2002/07/owl#", "http://www.w3.org/2002/07/owl#"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ensureTrailingSlash(tt.input)
			if got != tt.want {
				t.Errorf("ensureTrailingSlash(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
